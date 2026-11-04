package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/camoufox"
)

// RenderedSession owns a separate browser process and its page. Its profile is
// retained so a user can sign in once and use that login in subsequent tasks.
type RenderedSession struct {
	process        *camoufox.Process
	life           context.Context
	tab            context.Context
	target         string
	closeTab       context.CancelFunc
	closeAllocator context.CancelFunc
	releaseProfile func()
	getter         *HTTPGetter
	start          *url.URL
	assistant      *downloadAssistant

	mu        sync.RWMutex
	pageURL   *url.URL
	pageIP    netip.Addr
	docURL    *url.URL
	docIP     netip.Addr
	documents map[string]renderedDocument
	startIP   netip.Addr
	frozen    bool
	expected  string
	closed    sync.Once
	done      chan struct{}
	doneOnce  sync.Once
}

type renderedDocument struct {
	url *url.URL
	ip  netip.Addr
}

// OpenRendered opens rawURL, or a blank page if it is empty, in a dedicated browser.
// ctx bounds startup; the caller must close the returned session.
func OpenRendered(ctx context.Context, profileDir, rawURL, proxy string, headless bool) (*RenderedSession, error) {
	return openRendered(ctx, profileDir, rawURL, proxy, headless, browser.ConfigDir())
}

// OpenRenderedAssisted enables the download button in the dedicated browser.
func OpenRenderedAssisted(ctx context.Context, profileDir, rawURL, proxy string, headless bool, handler DownloadRequest) (*RenderedSession, error) {
	return openRenderedWithAssistant(ctx, profileDir, rawURL, proxy, headless, browser.ConfigDir(), handler)
}

func openRendered(ctx context.Context, profileDir, rawURL, proxy string, headless bool, runtimeDir string) (*RenderedSession, error) {
	return openRenderedWithAssistant(ctx, profileDir, rawURL, proxy, headless, runtimeDir, nil)
}

func openRenderedWithAssistant(ctx context.Context, profileDir, rawURL, proxy string, headless bool, runtimeDir string, handler DownloadRequest) (*RenderedSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if QzoneURLHasCredentials(strings.TrimSpace(rawURL)) {
		return nil, errors.New("QQ 相册网址包含登录令牌，请移除后重试")
	}
	if strings.TrimSpace(proxy) != "" {
		return nil, errors.New("浏览器下载暂不支持代理，无法安全校验代理解析的目标地址")
	}
	var start *url.URL
	var err error
	if strings.TrimSpace(rawURL) != "" {
		start, err = crawlInputURL(rawURL)
		if err != nil || start.User != nil {
			return nil, errors.New("浏览器地址必须是不含账号密码的 HTTP / HTTPS 地址")
		}
	}
	if !filepath.IsAbs(profileDir) {
		return nil, errors.New("下载浏览器目录必须是绝对路径")
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, fmt.Errorf("创建下载浏览器目录: %w", err)
	}
	profileDir, err = filepath.EvalSymlinks(profileDir)
	if err != nil {
		return nil, fmt.Errorf("定位下载浏览器目录: %w", err)
	}
	release, err := acquireRenderedProfile(profileDir)
	if err != nil {
		return nil, err
	}
	options := camoufox.Options{Profile: profileDir, Headless: headless}
	var assistant *downloadAssistant
	if handler != nil {
		var addon string
		assistant, addon, err = startDownloadAssistant(profileDir, handler)
		if err != nil {
			release()
			return nil, fmt.Errorf("准备下载助手: %w", err)
		}
		options.Addons = []string{addon}
	}
	process, err := camoufox.Start(ctx, runtimeDir, options, nil)
	if err != nil {
		assistant.close()
		release()
		return nil, err
	}
	life, closeTab := context.WithCancel(process.Context())
	tab := camoufox.PageContext(life, process, process.Page)
	closeAlloc := func() { process.Close() }
	getter := NewHTTPGetter(proxy)
	getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := getter.client.Transport.(*http.Transport)
	transport.DialContext = safeDownloadDial
	transport.DisableKeepAlives = true
	s := &RenderedSession{
		process: process, life: life, tab: tab, target: process.Page,
		closeTab: closeTab, closeAllocator: closeAlloc, releaseProfile: release,
		getter: getter, start: start, assistant: assistant,
		documents: map[string]renderedDocument{}, done: make(chan struct{}),
	}
	if assistant != nil {
		assistant.mu.Lock()
		assistant.session = s
		assistant.mu.Unlock()
	}
	if start != nil {
		if literal, parseErr := netip.ParseAddr(start.Hostname()); parseErr == nil {
			s.startIP = literal.Unmap()
		}
	}
	unsubscribe := process.Subscribe(func(event camoufox.Event) {
		if event.Event == "closed" {
			s.mu.Lock()
			delete(s.documents, event.Target)
			active := event.Target == s.target
			s.mu.Unlock()
			if active {
				s.markClosed()
			}
			return
		}
		if event.Event != "document" {
			return
		}
		var response struct{ URL, IP string }
		if json.Unmarshal(event.Data, &response) != nil {
			return
		}
		u, parseErr := crawlURL(nil, response.URL)
		ip, ipErr := netip.ParseAddr(strings.Trim(response.IP, "[]"))
		if parseErr != nil {
			u = nil
		}
		if ipErr != nil {
			ip = netip.Addr{}
		}
		s.mu.Lock()
		s.documents[event.Target] = renderedDocument{url: u, ip: ip.Unmap()}
		if event.Target == s.target {
			s.docURL, s.docIP = u, ip.Unmap()
			if !s.frozen && !s.startIP.IsValid() && u != nil && sameOrigin(s.start, u) {
				s.startIP = ip.Unmap()
			}
		}
		s.mu.Unlock()
	})
	context.AfterFunc(tab, unsubscribe)
	context.AfterFunc(process.Context(), s.markClosed)
	target := "about:blank"
	if start != nil {
		target = start.String()
	}
	err = s.run(ctx, camoufox.Navigate(target))
	s.mu.Lock()
	s.frozen = true
	s.mu.Unlock()

	if err != nil {
		s.Close()
		return nil, fmt.Errorf("打开下载浏览器: %w", err)
	}
	return s, nil
}

// OpenTarget changes the target while no capture or download is running.
func (s *RenderedSession) OpenTarget(ctx context.Context, rawURL string) error {
	if QzoneURLHasCredentials(strings.TrimSpace(rawURL)) {
		return errors.New("QQ 相册网址包含登录令牌，请移除后重试")
	}
	start, err := crawlInputURL(rawURL)
	if err != nil || start.User != nil {
		return errors.New("浏览器地址必须是不含账号密码的 HTTP / HTTPS 地址")
	}
	s.mu.Lock()
	s.pageURL, s.pageIP = nil, netip.Addr{}
	s.expected = ""
	s.start, s.docURL, s.docIP, s.startIP, s.frozen = start, nil, netip.Addr{}, netip.Addr{}, false
	if literal, parseErr := netip.ParseAddr(start.Hostname()); parseErr == nil {
		s.startIP = literal.Unmap()
	}
	s.mu.Unlock()
	err = s.run(ctx, page.BringToFront(), camoufox.Navigate(start.String()))
	s.mu.Lock()
	s.frozen = true
	if err != nil {
		s.start, s.docURL, s.docIP, s.startIP = nil, nil, netip.Addr{}, netip.Addr{}
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("打开下载网址: %w", err)
	}
	return nil
}

// BindCurrent selects the tab whose download button was clicked without reloading it.
func (s *RenderedSession) BindCurrent(ctx context.Context, rawURL, marker string) error {
	start, err := crawlInputURL(rawURL)
	if err != nil || start.User != nil || QzoneURLHasCredentials(rawURL) || !downloadRequestMarker.MatchString(marker) {
		return errors.New("当前网址无效或包含登录令牌，请移除后重试")
	}
	if !s.Alive() {
		return errors.New("下载浏览器已关闭，请重新打开")
	}
	var targets struct {
		Infos []struct {
			ID   string `json:"targetId"`
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := s.process.Call(ctx, "", "Target.getTargets", nil, &targets); err != nil {
		return err
	}
	var selected string
	var selectedTab context.Context
	for _, target := range targets.Infos {
		if target.Type != "page" || target.URL != rawURL {
			continue
		}
		tab := camoufox.PageContext(s.life, s.process, target.ID)
		var state struct {
			URL   string
			Match bool
		}
		script := fmt.Sprintf(`({url:location.href,match:Array.from(document.querySelectorAll('[data-tulai-download-helper][data-tulai-download-request]')).some(el=>el.getAttribute('data-tulai-download-request')===%q)})`, marker)
		err := runRendered(ctx, tab, chromedp.Evaluate(script, &state))
		if err == nil && state.URL == rawURL && state.Match {
			if selected != "" {
				return errors.New("无法确定下载标签页，请重试")
			}
			selected, selectedTab = target.ID, tab
		}
	}
	if selected == "" {
		return errors.New("网页已切换或关闭，请在目标页面重新点击下载")
	}
	s.mu.Lock()
	doc := s.documents[selected]
	s.target, s.tab, s.start = selected, selectedTab, start
	s.expected = rawURL
	s.pageURL, s.pageIP = nil, netip.Addr{}
	s.docURL, s.docIP, s.startIP, s.frozen = doc.url, doc.ip, netip.Addr{}, true
	if doc.url != nil && sameOrigin(start, doc.url) {
		s.startIP = doc.ip
	}
	if literal, parseErr := netip.ParseAddr(start.Hostname()); parseErr == nil {
		s.startIP = literal.Unmap()
	}
	s.mu.Unlock()
	return nil
}

// Close stops only the browser process owned by this session and keeps its profile.
func (s *RenderedSession) Close() {
	if s == nil {
		return
	}
	s.markClosed()
	s.closed.Do(func() {
		s.closeTab()
		s.closeAllocator()
		s.getter.client.CloseIdleConnections()
		s.assistant.close()
		s.releaseProfile()
	})
}

func (s *RenderedSession) markClosed() {
	if s != nil && s.done != nil {
		s.doneOnce.Do(func() { close(s.done) })
	}
}

// Alive reports whether the dedicated browser and its captured page are still open.
func (s *RenderedSession) Alive() bool {
	if s == nil || s.process == nil || !s.process.Alive() || s.done == nil {
		return false
	}
	s.mu.RLock()
	tab := s.tab
	s.mu.RUnlock()
	if tab == nil || tab.Err() != nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Done closes when the dedicated browser or its page closes.
func (s *RenderedSession) Done() <-chan struct{} { return s.done }

// PageURL is the URL of the last successfully captured page; credentials are removed.
func (s *RenderedSession) PageURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pageURL == nil {
		return ""
	}
	return pageReferer(s.pageURL)
}

// Getter returns the streaming downloader associated with the captured page.
func (s *RenderedSession) Getter() Getter { return s }

func (s *RenderedSession) run(ctx context.Context, actions ...camoufox.Action) error {
	s.mu.RLock()
	tab := s.tab
	s.mu.RUnlock()
	return runRendered(ctx, tab, actions...)
}

func runRendered(ctx, tab context.Context, actions ...camoufox.Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	op, cancel := context.WithTimeout(tab, 45*time.Second)
	stop := context.AfterFunc(ctx, cancel)
	defer func() { stop(); cancel() }()
	err := camoufox.Run(op, actions...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type renderedSnapshot struct {
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Base       string   `json:"base"`
	HTML       []string `json:"html"`
	Current    []string `json:"current"`
	Background []string `json:"background"`
	ScrollY    float64  `json:"scrollY"`
	Height     float64  `json:"height"`
	Viewport   float64  `json:"viewport"`
	Truncated  bool     `json:"truncated"`
	TooLarge   bool     `json:"tooLarge"`
}

// shortcut: each snapshot examines at most 10,000 elements and 8 MiB of markup;
// add configurable bounds when larger pages must be fully scanned.
const renderedSnapshotScript = `(() => {
  const html = [document.documentElement ? document.documentElement.outerHTML : ""];
  const current = [], background = [], roots = [document];
  let count = 0, truncated = false, total = html[0].length;
  for (let i = 0; i < roots.length && !truncated; i++) {
    const root = roots[i];
    if (i) { const text = root.innerHTML || ""; total += text.length; html.push(text); }
    if (total > 8388608) return {url:location.href, tooLarge:true};
    for (const element of root.querySelectorAll("*")) {
      if (++count > 10000) { truncated = true; break; }
      if (element.shadowRoot) roots.push(element.shadowRoot);
      if (element instanceof HTMLImageElement && element.currentSrc) current.push(element.currentSrc);
      try { const value = getComputedStyle(element).backgroundImage; if (value && value !== "none") background.push(value); } catch (_) {}
    }
  }
  const body = document.body, root = document.documentElement;
  return {url:location.href,title:document.title,base:document.baseURI,html,current,background,
    scrollY:window.scrollY,height:Math.max(root ? root.scrollHeight : 0, body ? body.scrollHeight : 0),
    viewport:window.innerHeight,truncated};
})()`

// shortcut: only the current page and at most eight viewport scrolls are
// examined; add task-configurable crawling when deeper rendered pages are needed.
const maxRenderedScrolls = 8

// Capture reads the currently visible page after the user has completed login.
func (s *RenderedSession) Capture(ctx context.Context) (Discovery, error) {
	result := Discovery{IsPage: true}
	s.mu.Lock()
	s.pageURL = nil
	s.pageIP = netip.Addr{}
	expected := s.expected
	s.mu.Unlock()
	if err := Wait(ctx, 400*time.Millisecond); err != nil {
		return result, err
	}
	seen := map[string]bool{}
	var first *url.URL
	var firstRaw string
	var originalY float64
	var partial error
	defer func() {
		if first != nil && s.tab.Err() == nil {
			restore, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.run(restore, chromedp.Evaluate(fmt.Sprintf(`if (location.href === %q) window.scrollTo(0, %g)`, firstRaw, originalY), nil))
		}
	}()
	for step := 0; step <= maxRenderedScrolls; step++ {
		var snapshot renderedSnapshot
		if err := s.run(ctx, chromedp.Evaluate(renderedSnapshotScript, &snapshot)); err != nil {
			return result, err
		}
		if expected != "" && snapshot.URL != expected {
			return result, errors.New("浏览器页面已变化，请返回目标页面后重新下载")
		}
		page, err := crawlInputURL(snapshot.URL)
		if err != nil || !sameRenderedSite(s.start, page) {
			return result, errors.New("浏览器当前页面已离开原始站点，请返回目标网页后重试")
		}
		if isNeteasePhotoPage(s.start) {
			return discoverNeteasePhoto(ctx, s.Getter(), s.start)
		}
		if googleVerificationPage(page) {
			return result, errors.New("Google 要求安全验证，请在下载浏览器中完成验证后重试")
		}
		if isBingImageDetail(s.start) && !bingDetailSameRequest(s.start, page) {
			return result, errors.New("Bing 单图页面已跳转或搜索条件发生变化")
		}
		if isTwitterProfileMedia(s.start) {
			return result, errors.New("X 媒体页可能需要登录，当前尚未支持；请打开作者主页或单个帖子")
		}
		if isTwitterProfile(s.start) && !twitterSameProfile(s.start, page) {
			return result, errors.New("X 作者主页已跳转，请完成登录后重试")
		}
		if isGoogleImageSearch(s.start) {
			if !googleImagesSameSearch(s.start, page) {
				return result, errors.New("Google 图片搜索已跳转或查询条件发生变化")
			}
		}
		if isAmazonSearch(s.start) && !amazonSearchSamePage(s.start, page) {
			return result, errors.New("亚马逊搜索已跳转或查询条件发生变化")
		}
		if isEbaySearch(s.start) && !ebaySearchSameRequest(s.start, page) {
			return result, errors.New("eBay 搜索已跳转或查询条件发生变化")
		}
		if isTemuSearch(s.start) && !temuSearchSameSearch(s.start, page) {
			return result, errors.New("TEMU 搜索已跳转或查询条件发生变化，请完成验证后重试")
		}
		if (isTemuSearch(s.start) || isTemuHome(s.start)) && temuSearchSite(page) && page.Path == "/bgn_verification.html" {
			return result, errors.New("TEMU 要求安全验证，请在下载浏览器中完成后重试")
		}
		if isWechatArticle(s.start) && isWechatVerification(page) {
			return result, errors.New("公众号要求安全验证，请在下载浏览器中完成验证后重试")
		}
		if isAlibabaProduct(s.start) && !isAlibabaProduct(page) {
			return result, errors.New("阿里巴巴国际站商品页面已跳转，可能需要登录或安全验证")
		}
		if isDouyinProduct(s.start) && !isDouyinProduct(page) {
			return result, errors.New("抖音商品页面已跳转，可能需要登录或安全验证")
		}
		if isYouzanProduct(s.start) && !isYouzanProduct(page) {
			return result, errors.New("有赞商品页面已跳转，可能需要登录或安全验证")
		}
		if isIMDbPage(s.start) && !isIMDbPage(page) {
			return result, errors.New("IMDb 页面已跳转，可能需要登录或安全验证")
		}
		if (isSinaBlogArticle(s.start) || isSinaBlogList(s.start)) && !isSinaBlogArticle(page) && !isSinaBlogList(page) {
			return result, errors.New("新浪博客文章已跳转，可能需要登录或安全验证")
		}
		if isDHgateProduct(s.start) && !isDHgateProduct(page) {
			return result, errors.New("敦煌网商品页面已跳转，可能需要登录或安全验证")
		}
		if isWeidianProduct(s.start) && !isWeidianProduct(page) {
			return result, errors.New("微店商品页面已跳转，可能需要登录或安全验证")
		}
		if isYupooPage(s.start) && !isYupooPage(page) {
			return result, errors.New("又拍页面已跳转，可能需要登录或安全验证")
		}
		if isZOLPage(s.start) && !isZOLPage(page) {
			return result, errors.New("中关村在线图片页已跳转，可能需要登录或安全验证")
		}
		if isAutohomePhoto(s.start) && !isAutohomePhoto(page) {
			return result, errors.New("汽车之家图片页已跳转，可能需要登录或安全验证")
		}
		if isMadeInChinaProduct(s.start) && !isMadeInChinaProduct(page) {
			return result, errors.New("中国制造网商品页面已跳转，可能需要登录或安全验证")
		}
		if isDangdangProduct(s.start) && !isDangdangProduct(page) {
			return result, errors.New("当当商品页面已跳转，可能需要登录或安全验证")
		}
		if isVIPProduct(s.start) && !isVIPProduct(page) {
			return result, errors.New("唯品会商品页面已跳转，可能需要登录或安全验证")
		}
		if isSogouImageSearch(s.start) && !isSogouImageSearch(page) {
			return result, errors.New("搜狗图片页面已跳转，可能需要安全验证")
		}
		if isOzonProduct(s.start) && !isOzonProduct(page) {
			return result, errors.New("Ozon 商品页面已跳转，可能需要登录或安全验证")
		}
		if isSOImageSearch(s.start) && !isSOImageSearch(page) {
			return result, errors.New("360 图片页面已跳转，可能需要安全验证")
		}
		if isZhihuPage(s.start) && !isZhihuPage(page) {
			return result, errors.New("知乎页面已跳转，可能需要登录或安全验证")
		}
		if isUnsplashPage(s.start) && !isUnsplashPage(page) {
			return result, errors.New("Unsplash 页面已跳转，可能需要登录或安全验证")
		}
		if isYanxuanProduct(s.start) && !isYanxuanProduct(page) {
			return result, errors.New("网易严选商品页面已跳转，可能需要登录或安全验证")
		}
		if isJJsHouseProduct(s.start) && !isJJsHouseProduct(page) {
			return result, errors.New("JJ's House 商品页面已跳转，可能需要登录或安全验证")
		}
		if isBaijiahaoPage(s.start) && !isBaijiahaoPage(page) {
			return result, errors.New("百家号页面已跳转，可能需要登录或安全验证")
		}
		if isToutiaoArticle(s.start) && !isToutiaoArticle(page) {
			return result, errors.New("今日头条文章已跳转，可能需要登录或安全验证")
		}
		if (isAicaigouProduct(s.start) || isAicaigouShop(s.start)) && !isAicaigouProduct(page) && !isAicaigouShop(page) {
			return result, errors.New("爱采购页面已跳转，可能需要登录或安全验证")
		}
		if isRajceAlbum(s.start) && !isRajceAlbum(page) {
			return result, errors.New("Rajče 相册已跳转，可能需要登录或安全验证")
		}
		if isRajceProfile(page) {
			return result, rajceProfileUnsupported()
		}
		if isSpoonflowerPage(s.start) && !isSpoonflowerPage(page) {
			return result, errors.New("Spoonflower 页面已跳转，可能需要登录或安全验证")
		}
		if isMogujieProduct(s.start) && !isMogujieProduct(page) {
			return result, errors.New("蘑菇街商品页面已跳转，可能已下架或需要安全验证")
		}
		if isCNUWork(s.start) && !isCNUWork(page) {
			return result, errors.New("CNU 作品页面已跳转，可能需要登录或安全验证")
		}
		if isCNUUser(page) {
			return result, cnuUserUnsupported()
		}
		if isYandePage(s.start) && !isYandePage(page) {
			return result, errors.New("yande.re 页面已跳转，可能需要登录或安全验证")
		}
		if isSuningProduct(s.start) && !isSuningProduct(page) {
			return result, errors.New("苏宁商品页面已跳转，可能需要登录或安全验证")
		}
		if isYiwugouProduct(s.start) && !isYiwugouProduct(page) {
			return result, errors.New("义乌购商品页面已跳转，可能需要登录或安全验证")
		}
		if isCtripAlbum(s.start) && !isCtripAlbum(page) {
			return result, errors.New("携程相册页面已跳转，可能需要登录或安全验证")
		}
		if isGoooodPage(s.start) && !isGoooodPage(page) {
			return result, errors.New("谷德页面已跳转，可能需要登录或安全验证")
		}
		if isFangAlbum(s.start) && !isFangAlbum(page) {
			return result, errors.New("房天下相册已跳转，可能需要登录或安全验证")
		}
		if isArchDailyPage(s.start) && !isArchDailyPage(page) {
			return result, errors.New("ArchDaily 页面已跳转，可能需要登录或安全验证")
		}
		if isPhotoplusPage(s.start) && !isPhotoplusPage(page) {
			return result, errors.New("Photoplus 活动页面已跳转，可能需要登录或安全验证")
		}
		if isWSYProduct(s.start) && !isWSYProduct(page) {
			return result, errors.New("网商园商品页面已跳转，可能需要登录或安全验证")
		}
		if isPexelsPhoto(s.start) && !isPexelsPhoto(page) {
			return result, errors.New("Pexels 图片页面已跳转，可能需要登录或安全验证")
		}
		if isTemuProduct(s.start) && !isTemuProduct(page) {
			return result, errors.New("TEMU 商品页面已跳转，可能需要登录或安全验证")
		}
		if isSheinProduct(s.start) && !isSheinProduct(page) {
			return result, errors.New("SHEIN 商品页面已跳转，可能需要登录或安全验证")
		}
		if isGelbooruPage(s.start) && !isGelbooruPage(page) {
			return result, errors.New("Gelbooru 页面已跳转，可能需要登录或安全验证")
		}
		if isXianyuItem(s.start) && !isXianyuItem(page) {
			return result, errors.New("闲鱼商品页面已跳转，可能需要登录或安全验证")
		}
		if isDewuProduct(s.start) && !isDewuProduct(page) {
			return result, errors.New("得物商品页面已跳转，可能需要登录或安全验证")
		}
		if pailixiangLegacy(s.start) {
			return result, errors.New("拍立享旧版相册暂不支持，请使用当前公开活动链接")
		}
		if isPailixiangPage(s.start) && !isPailixiangPage(page) {
			return result, errors.New("拍立享活动页面已跳转，可能需要登录或安全验证")
		}
		if isWikiArtAuthor(s.start) && !isWikiArtAuthor(page) {
			return result, errors.New("WikiArt 作者页面已跳转，可能需要登录或安全验证")
		}
		if isKuaishouPost(s.start) && !isKuaishouPost(page) {
			return result, errors.New("快手作品页面已跳转，可能需要登录或安全验证")
		}
		if isAlltuuAlbum(s.start) || isAlltuuAlbum(page) {
			return result, alltuuUnsupported()
		}
		if isDouyinContent(s.start) && !isDouyinContent(page) {
			return result, errors.New("抖音作品页面已跳转，可能需要登录或安全验证")
		}
		if isIssuuDocument(s.start) && !isIssuuDocument(page) {
			return result, errors.New("Issuu 文档页面已跳转，可能需要登录或访问授权")
		}
		if isHouzzProject(s.start) && !isHouzzProject(page) {
			return result, errors.New("Houzz 项目页面已跳转，可能需要登录或安全验证")
		}
		if kind, id := mihuashiRef(s.start); kind != "" {
			currentKind, currentID := mihuashiRef(page)
			if kind != currentKind || id != currentID {
				return result, errors.New("米画师页面已跳转，作品内容暂无法确认")
			}
		}
		if isLightInTheBoxProduct(s.start) && !isLightInTheBoxProduct(page) {
			return result, errors.New("兰亭商品页面已跳转，可能需要登录或安全验证")
		}
		if (isOnexPhoto(s.start) || isOnexGallery(s.start)) && !isOnexPhoto(page) && !isOnexGallery(page) {
			return result, errors.New("1x 作品页面已跳转，可能需要登录或安全验证")
		}
		if (isPhotography35Photo(s.start) || isPhotography35Author(s.start)) && !isPhotography35Photo(page) && !isPhotography35Author(page) {
			return result, errors.New("35photo 页面已跳转，可能需要登录或安全验证")
		}
		if isPconlinePhoto(s.start) && !isPconlinePhoto(page) {
			return result, errors.New("摄影部落作品页面已跳转，可能需要登录或安全验证")
		}
		if isImgsrcPage(s.start) && (!isImgsrcPage(page) || imgsrcPageRef(s.start).owner != imgsrcPageRef(page).owner) {
			return result, errors.New("ImgSrc 相册页面已跳转，可能需要访问授权")
		}
		if isMtimePage(s.start) && (mtimePageRef(s.start).kind != mtimePageRef(page).kind || mtimePageRef(s.start).id != mtimePageRef(page).id) {
			return result, errors.New("时光网图片页面已跳转")
		}
		if kind, id := animePicturesRoute(s.start); kind != "" {
			currentKind, currentID := animePicturesRoute(page)
			if kind != currentKind || id != currentID || (kind == "search" && !animePicturesSameSearch(s.start, page, false)) {
				return result, errors.New("Anime-Pictures 页面已跳转或搜索条件已变化")
			}
		}
		if start := faPageRef(s.start); start.kind != "" {
			current := faPageRef(page)
			if (start.kind == "photo" && (current.kind != "photo" || start.id != current.id)) ||
				(start.kind != "photo" && (current.kind == "photo" || !strings.EqualFold(start.author, current.author))) {
				return result, errors.New("Fur Affinity 页面已跳转到其他作品或作者")
			}
		}
		if product, sku := gomeProductRef(s.start); product != "" {
			currentProduct, currentSKU := gomeProductRef(page)
			if product != currentProduct || sku != currentSKU {
				return result, errors.New("国美商品页面已跳转")
			}
		}
		if shop := dianpingPageRef(s.start).shop; shop != "" && shop != dianpingPageRef(page).shop {
			return result, errors.New("大众点评页面已跳转到其他门店")
		}
		if kind, id := nipicRoute(s.start); kind != "" {
			currentKind, currentID := nipicRoute(page)
			if kind != currentKind || id != currentID || (kind == "search" && !nipicSameSearch(s.start, page, false)) {
				return result, errors.New("昵图/汇图页面已跳转或搜索条件已变化")
			}
		}
		if id := qyerAlbumID(s.start); id != "" && qyerAlbumID(page) != id {
			return result, errors.New("穷游相册页面已跳转")
		}
		if isOnlyFansSite(s.start) {
			requested, current := onlyFansProfile(s.start), onlyFansProfile(page)
			if current == "" || (requested != "" && requested != current) {
				return result, errors.New("OnlyFans 请先登录并打开已授权的作者主页")
			}
		}
		if ref := muyingPageRef(s.start); ref.kind != "" {
			current := muyingPageRef(page)
			if ref.kind != current.kind || ref.id != current.id {
				return result, errors.New("母婴天下页面已跳转")
			}
		}
		if kind, id := ymatouRoute(s.start); kind != "" {
			currentKind, currentID := ymatouRoute(page)
			if kind != currentKind || id != currentID || !strings.EqualFold(s.start.Hostname(), page.Hostname()) {
				return result, errors.New("洋码头商品页面已跳转")
			}
		}
		if ref := neteaseBlogRef(s.start); ref.id != "" && ref != neteaseBlogRef(page) {
			return result, errors.New("网易博客文章页面已跳转")
		}
		if kind, id := soukuanRoute(s.start); kind != "" {
			currentKind, currentID := soukuanRoute(page)
			if kind != currentKind || id != currentID || !strings.EqualFold(s.start.Hostname(), page.Hostname()) {
				return result, errors.New("搜款网商品页面已跳转")
			}
		}
		if start := mokoPageRef(s.start); start.kind != "" {
			current := mokoPageRef(page)
			if (start.kind == "post" && (current.kind != "post" || start.id != current.id)) ||
				(start.kind != "post" && (current.owner != start.owner || (start.kind == "category" && (current.kind != "category" || start.category != current.category)))) {
				return result, errors.New("美空页面已跳转到其他作品或作者")
			}
		}
		if id := xiaomiYoupinID(s.start); id != "" && id != xiaomiYoupinID(page) {
			return result, errors.New("小米有品商品页面已跳转")
		}
		if id := zcygovID(s.start); id != "" && (id != zcygovID(page) || !strings.EqualFold(s.start.Hostname(), page.Hostname())) {
			return result, errors.New("政采云商品页面已跳转")
		}
		if id := dribbbleShotID(s.start); id != "" && id != dribbbleShotID(page) {
			return result, errors.New("Dribbble 作品页面已跳转")
		}
		if kind, id := facebookRoute(s.start); kind != "" {
			currentKind, currentID := facebookRoute(page)
			if kind != currentKind || id != currentID {
				return result, errors.New("Facebook 图片页面已跳转，请完成登录后重试")
			}
		}
		if start := tiktokPageRef(s.start); start.kind != "" {
			current := tiktokPageRef(page)
			if current.kind == "" || !strings.EqualFold(start.author, current.author) || start.id != current.id {
				return result, errors.New("TikTok 页面已跳转到其他作者或作品")
			}
		}
		if id := mujiProductID(s.start); id != "" && id != mujiProductID(page) {
			return result, errors.New("无印良品商品页面已跳转")
		}
		if start := ehentaiRoute(s.start); start.kind != "" && (start != ehentaiRoute(page) || !ehentaiSafeQuery(s.start) || !ehentaiSafeQuery(page)) {
			return result, errors.New("E-Hentai 页面已跳转到其他画廊或图片")
		}
		if key := gallery121Key(s.start); key != "" && key != gallery121Key(page) {
			return result, errors.New("图片集页面已跳转到其他作品")
		}
		if isTwitterStatus(s.start) && !twitterSameStatus(s.start, page) {
			return result, errors.New("X 帖子已跳转到其他作者或作品")
		}
		if key := gallery128Key(s.start); key != "" && key != gallery128Key(page) {
			return result, errors.New("图片集页面已跳转到其他作品")
		}
		if isSizuoArticle(s.start) && !sizuoSameArticle(s.start, page) {
			return result, errors.New("四座网页面已跳转到其他作品")
		}
		if isWellcoPage(s.start) && !wellcoSameFetchPage(s.start, page) {
			return result, errors.New("Wellco 页面已跳转到其他商品或分类")
		}
		if is500pxChinaLegacyPage(page) {
			return result, errors.New("请等待 500px 中国站完成页面跳转和加载后重试")
		}
		if _, profile := xiaohongshuProfileID(s.start); profile && strings.EqualFold(page.Hostname(), "www.xiaohongshu.com") && page.Path == "/login" {
			return result, errors.New("小红书主页需要登录，请在下载浏览器中完成登录后重试")
		}
		if qzoneIsGroupAlbum(s.start) && !qzoneIsGroupAlbum(page) {
			return result, errors.New("请在下载浏览器中登录并打开 QQ 群相册后重试")
		}
		if _, groupList := qzoneGroupAlbumListID(s.start); groupList {
			if _, current := qzoneGroupAlbumListID(page); !current && !qzoneIsGroupAlbum(page) {
				return result, errors.New("请在下载浏览器中登录并打开 QQ 群相册后重试")
			}
		}
		if _, personal := qzonePersonalOwner(s.start); personal || qzonePersonalAlbumDetail(s.start) {
			if _, current := qzonePersonalOwner(page); !current {
				if !qzonePersonalAlbumDetail(page) {
					return result, errors.New("请在下载浏览器中登录并打开 QQ 空间相册后重试")
				}
			}
		}
		if first == nil {
			first, firstRaw, originalY = page, snapshot.URL, snapshot.ScrollY
			result.Title = strings.TrimSpace(snapshot.Title)
			s.mu.Lock()
			s.pageURL = page
			if s.docURL != nil && s.docIP.IsValid() && sameOrigin(s.docURL, page) {
				s.pageIP = s.docIP
			} else if literal, err := netip.ParseAddr(page.Hostname()); err == nil {
				s.pageIP = literal.Unmap()
			}
			s.mu.Unlock()
			if isGoogleImageSearch(s.start) {
				return googleImagesCapture(ctx, s, s.start, page)
			}
			if (isTemuSearch(s.start) || isTemuHome(s.start)) && isTemuSearch(page) {
				return temuSearchCapture(ctx, s, s.start, page)
			}
			if albums, matched, err := s.captureQzoneGroupList(ctx, page, snapshot.URL); matched {
				return albums, err
			}
			if album, matched, err := s.captureQzoneGroupAlbum(ctx, page, snapshot.URL); matched {
				return album, err
			}
			if albums, matched, err := s.captureQzonePersonal(ctx, page, snapshot.URL); matched {
				return albums, err
			}
			if profile, matched, err := s.captureXiaohongshuProfile(ctx, page, snapshot.URL); matched {
				return profile, err
			}
			if note, matched, err := s.captureXiaohongshuNote(ctx, page, snapshot.URL); matched {
				return note, err
			}
			if photos, matched, err := s.capturePhotoplus(ctx, page, snapshot.URL); matched {
				return photos, err
			}
			if photos, matched, err := s.capturePailixiang(ctx, page, snapshot.URL); matched {
				return photos, err
			}
			if photos, matched, err := s.captureOnlyFans(ctx, page, snapshot.URL); matched {
				return photos, err
			}
			if isYmatouProduct(page) {
				return ymatouCapture(ctx, s, page)
			}
			if isSoukuanProduct(page) {
				return soukuanCapture(ctx, s, page)
			}
			if isXiaomiYoupinProduct(page) {
				return s.captureXiaomiYoupin(ctx, page, snapshot.URL)
			}
			if isZcygovProduct(page) {
				return zcygovCapture(ctx, s, page)
			}
			if isFacebookPage(page) {
				return facebookCapture(ctx, s, page)
			}
			if isBilibiliSpace(page) {
				return discoverBilibiliSpace(ctx, s.Getter(), page)
			}
			if isPixivArtwork(page) {
				return discoverPixivArtwork(ctx, s.Getter(), page)
			}
			if isPixivUser(page) {
				return discoverPixivUser(ctx, s.Getter(), page)
			}
			if isWeiboContent(page) {
				return discoverWeiboContent(ctx, s.Getter(), page)
			}
			if isDoubanPage(page) {
				return discoverDouban(ctx, s.Getter(), page)
			}
			if isArtstationPage(page) {
				return discoverArtstation(ctx, s.Getter(), page)
			}
			if isWeishangPage(page) {
				return discoverWeishang(ctx, s.Getter(), page)
			}
			if isSinaBlogList(page) {
				return discoverSinaBlogList(ctx, s.Getter(), page)
			}
			if isAicaigouShop(page) {
				return discoverAicaigouShop(ctx, s.Getter(), page)
			}
			if isTuchongPage(page) && tuchongRouteOf(page).post == "" {
				return discoverTuchong(ctx, s.Getter(), page)
			}
			if is1688Offer(page) || isBilibiliPost(page) || isImgurPage(page) || isTaobaoItem(page) ||
				isFlickrPage(page) || isHuabanPage(page) || isWechatArticle(page) || isJDProduct(page) ||
				isAliExpressProduct(page) || isAmazonSearch(page) || isAmazonProduct(page) || isInstagramPost(page) ||
				isPinduoduoProduct(page) || isTuchongPage(page) || isBehanceProject(page) ||
				isBehanceProfile(page) || isZcoolWork(page) || is500pxPage(page) || isAlibabaProduct(page) ||
				isDuitangPage(page) || isPinterestPage(page) || isEbaySearch(page) || isEbayProduct(page) || isDeviantArtWork(page) ||
				isShopeeProduct(page) || isEtsyProduct(page) || isTumblrPost(page) || isYahooJPProduct(page) ||
				isLazadaProduct(page) || isDouyinProduct(page) || isYouzanProduct(page) || isIMDbPage(page) ||
				isSinaBlogArticle(page) || isDHgateProduct(page) || isWeidianProduct(page) || isYupooPage(page) ||
				isZOLPage(page) || isAutohomePhoto(page) || isMadeInChinaProduct(page) || isDangdangProduct(page) ||
				isVIPProduct(page) || isSogouImageSearch(page) || isOzonProduct(page) || isSOImageSearch(page) ||
				isZhihuPage(page) || isUnsplashPage(page) || isYanxuanProduct(page) || isJJsHouseProduct(page) ||
				isBaijiahaoPage(page) || isToutiaoArticle(page) || isAicaigouProduct(page) || isRajceAlbum(page) ||
				isSpoonflowerPage(page) || isMogujieProduct(page) || isCNUWork(page) || isYandePage(page) ||
				isSuningProduct(page) || isYiwugouProduct(page) || isCtripAlbum(page) || isGoooodPage(page) ||
				isFangAlbum(page) || isArchDailyPage(page) || isWSYProduct(page) || isPexelsPhoto(page) ||
				isTemuProduct(page) || isSheinProduct(page) || isGelbooruPage(page) || isXianyuItem(page) ||
				isDewuProduct(page) || isWikiArtAuthor(page) || isKuaishouPost(page) || isDouyinContent(page) ||
				isIssuuDocument(page) || isHouzzProject(page) || isMihuashiPage(page) || isLightInTheBoxProduct(page) ||
				isOnexPhoto(page) || isOnexGallery(page) || isPhotography35Photo(page) || isPhotography35Author(page) ||
				isPconlinePhoto(page) || isImgsrcPage(page) || isMtimePage(page) || isAnimePicturesPage(page) || isFuraffinityPage(page) || isGomeProduct(page) || isDianpingPage(page) || isNipicPage(page) || isQyerAlbum(page) || isMuyingPage(page) || isNeteaseBlogArticle(page) || isMokoPage(page) || isDribbbleShot(page) || isTikTokPage(page) || isMujiProduct(page) || isEhentaiPage(page) || isGallery121(page) || isTwitterStatus(page) || isTwitterProfile(page) || isGallery128Page(page) || isSizuoArticle(page) || isWellcoPage(page) || isBingImageDetail(page) {
				if snapshot.TooLarge || len(snapshot.HTML) == 0 || len(snapshot.HTML[0]) > maxPageBytes {
					return result, errors.New("渲染网页超过 8 MiB 的扫描上限")
				}
				doc, err := html.Parse(strings.NewReader(snapshot.HTML[0]))
				if err != nil {
					return result, err
				}
				switch {
				case isDouyinProduct(page):
					return douyinProductPage(ctx, doc, page)
				case isYouzanProduct(page):
					return youzanCollect(ctx, s.Getter(), doc, page)
				case isIMDbPage(page):
					return imdbCollect(ctx, s.Getter(), page, doc)
				case isSinaBlogArticle(page):
					return sinaBlogArticlePage(ctx, doc, page)
				case isDHgateProduct(page):
					return dhgateProductPage(ctx, doc, page)
				case isWeidianProduct(page):
					return weidianCollect(ctx, s.Getter(), doc, page)
				case isYupooPage(page):
					return yupooCollect(ctx, s.Getter(), page, doc)
				case isZOLPage(page):
					return zolCollect(ctx, s.Getter(), page, doc)
				case isAutohomePhoto(page):
					return autohomeCollect(ctx, s.Getter(), page, doc)
				case isMadeInChinaProduct(page):
					return madeInChinaProductPage(ctx, doc, page)
				case isDangdangProduct(page):
					return dangdangCollect(ctx, s.Getter(), page, doc)
				case isVIPProduct(page):
					return vipCollect(ctx, s.Getter(), page, doc)
				case isSogouImageSearch(page):
					return sogouCollect(ctx, s.Getter(), page, doc)
				case isOzonProduct(page):
					return ozonProductPage(ctx, doc, page)
				case isSOImageSearch(page):
					return soImagesCollect(ctx, s.Getter(), page, doc)
				case isZhihuPage(page):
					return zhihuPage(ctx, doc, page)
				case isUnsplashPage(page):
					if search, matched, err := s.captureUnsplashSearch(ctx, page, snapshot.URL, doc); matched {
						return search, err
					}
					return unsplashCollect(ctx, s.Getter(), page, doc)
				case isYanxuanProduct(page):
					return yanxuanProductPage(ctx, doc, page)
				case isJJsHouseProduct(page):
					return jjshouseProductPage(ctx, doc, page)
				case isBaijiahaoPage(page):
					return baijiahaoCollect(ctx, s.Getter(), page, doc)
				case isToutiaoArticle(page):
					return toutiaoArticlePage(ctx, doc, page)
				case isAicaigouProduct(page):
					return aicaigouProductPage(ctx, doc, page)
				case isRajceAlbum(page):
					return rajceCollect(ctx, s.Getter(), page, doc)
				case isSpoonflowerPage(page):
					return spoonflowerCollect(ctx, s.Getter(), page, doc)
				case isMogujieProduct(page):
					return mogujieProductPage(ctx, doc, page)
				case isCNUWork(page):
					return cnuWorkPage(ctx, doc, page)
				case isYandePage(page):
					return yandeCollect(ctx, s.Getter(), page, doc)
				case isSuningProduct(page):
					return suningCollect(ctx, s.Getter(), page, doc)
				case isYiwugouProduct(page):
					return yiwugouCollect(ctx, s.Getter(), page, doc)
				case isCtripAlbum(page):
					return ctripCollect(ctx, s.Getter(), page, doc)
				case isGoooodPage(page):
					return goooodCollect(ctx, s.Getter(), page, doc)
				case isFangAlbum(page):
					return fangAlbumPage(ctx, doc, page)
				case isArchDailyPage(page):
					return archDailyCollect(ctx, s.Getter(), page, doc)
				case isWSYProduct(page):
					return wsyProductPage(ctx, doc, page)
				case isPexelsPhoto(page):
					return pexelsPhotoPage(ctx, doc, page)
				case isTemuProduct(page):
					return temuProductPage(ctx, doc, page)
				case isSheinProduct(page):
					return sheinCollect(ctx, page, doc)
				case isGelbooruPage(page):
					return gelbooruCollect(ctx, s.Getter(), page, doc)
				case isXianyuItem(page):
					return xianyuItemPage(ctx, doc, page)
				case isDewuProduct(page):
					return dewuProductPage(ctx, doc, page)
				case isWikiArtAuthor(page):
					return wikiArtCollect(ctx, s.Getter(), page, doc)
				case isKuaishouPost(page):
					return kuaishouCollect(ctx, s.Getter(), doc, page)
				case isDouyinContent(page):
					return douyinContentPage(ctx, doc, page)
				case isIssuuDocument(page):
					return issuuRenderedCollect(ctx, s, page, doc)
				case isHouzzProject(page):
					return houzzProjectPage(ctx, doc, page)
				case isMihuashiPage(page):
					return s.captureMihuashi(ctx, doc, page, snapshot.URL)
				case isLightInTheBoxProduct(page):
					return lightInTheBoxProductPage(ctx, doc, page)
				case isOnexPhoto(page):
					return onexPhotoPage(ctx, doc, page)
				case isOnexGallery(page):
					return onexGalleryPage(ctx, doc, page)
				case isPhotography35Photo(page):
					return photography35PhotoPage(ctx, doc, page)
				case isPhotography35Author(page):
					return photography35AuthorCollect(ctx, s.Getter(), page, doc)
				case isPconlinePhoto(page):
					return pconlineCollect(ctx, s.Getter(), page, doc)
				case isImgsrcPage(page):
					return imgsrcRenderedPage(ctx, doc, page)
				case isMtimePage(page):
					return discoverMtime(ctx, s.Getter(), page)
				case isAnimePicturesPage(page):
					return animePicturesCollect(ctx, s.Getter(), page, doc)
				case isFuraffinityPage(page):
					return furaffinityCollect(ctx, s.Getter(), page, doc)
				case isGomeProduct(page):
					return gomeCollect(ctx, s.Getter(), page, doc)
				case isDianpingPage(page):
					return dianpingCollect(ctx, s.Getter(), page, doc)
				case isNipicPage(page):
					return nipicCollect(ctx, s.Getter(), page, doc)
				case isQyerAlbum(page):
					return qyerAlbum(ctx, page, doc)
				case isMuyingPage(page):
					return muyingCollect(ctx, s.Getter(), page, doc)
				case isNeteaseBlogArticle(page):
					return neteaseBlogArticle(ctx, page, doc)
				case isMokoPage(page):
					return mokoCollect(ctx, s.Getter(), page, doc)
				case isDribbbleShot(page):
					return dribbbleShot(ctx, page, doc)
				case isTikTokPage(page):
					return s.captureTikTok(ctx, doc, page, snapshot.URL)
				case isMujiProduct(page):
					return mujiCollect(ctx, s.Getter(), page, doc)
				case isEhentaiPage(page):
					return ehentaiCollect(ctx, s.Getter(), page, doc)
				case isGallery121(page):
					return gallery121Collect(ctx, s.Getter(), page, doc)
				case isTwitterStatus(page):
					return twitterCollect(ctx, twitterCapturePage(s.start, page), doc)
				case isTwitterProfile(page):
					return twitterProfileCapture(ctx, s, s.start, page)
				case isGallery128Page(page):
					return gallery128Collect(ctx, s.Getter(), page, doc)
				case isSizuoArticle(page):
					return sizuoCollect(ctx, s.Getter(), page, doc)
				case isWellcoPage(page):
					return wellcoCollect(ctx, s.Getter(), page, doc)
				case isBingImageDetail(page):
					return bingDetailCollect(ctx, s.Getter(), page, doc)
				case is1688Offer(page):
					return offer1688Collect(ctx, s.Getter(), page, doc)
				case isBilibiliPost(page):
					return bilibiliPostPage(ctx, doc, page)
				case isImgurPage(page):
					return imgurPage(ctx, doc, page)
				case isFlickrPage(page):
					return flickrCollect(ctx, s.Getter(), page, doc)
				case isHuabanPage(page):
					return huabanCollect(ctx, s.Getter(), page, doc)
				case isWechatArticle(page):
					return wechatArticlePage(ctx, doc, page)
				case isJDProduct(page):
					return jdCollect(ctx, s.Getter(), page, doc)
				case isAliExpressProduct(page):
					return aliExpressCollect(ctx, s.Getter(), page, doc)
				case isAmazonSearch(page):
					return amazonSearchCollect(ctx, s.Getter(), page, doc)
				case isAmazonProduct(page):
					return amazonCollect(ctx, s.Getter(), page, doc)
				case isInstagramPost(page):
					return instagramPostPage(ctx, doc, page)
				case isPinduoduoProduct(page):
					return pinduoduoCollect(ctx, s.Getter(), page, doc)
				case isTuchongPage(page):
					return tuchongPostPage(ctx, doc, page)
				case isBehanceProject(page), isBehanceProfile(page):
					return behanceCollect(ctx, s.Getter(), page, doc)
				case isZcoolWork(page):
					return zcoolWorkPage(ctx, doc, page)
				case is500pxPage(page):
					return collect500px(ctx, doc, page)
				case isAlibabaProduct(page):
					return alibabaCollect(ctx, s.Getter(), page, doc)
				case isDuitangPage(page):
					return duitangCollect(ctx, s.Getter(), page, doc)
				case isPinterestPage(page):
					return collectPinterest(ctx, s.Getter(), page, doc)
				case isEbaySearch(page):
					return ebaySearchCollect(ctx, s.Getter(), page, doc)
				case isEbayProduct(page):
					return ebayCollect(ctx, s.Getter(), page, doc)
				case isDeviantArtWork(page):
					return deviantArtWorkPage(ctx, doc, page)
				case isShopeeProduct(page):
					return shopeeCollect(ctx, s.Getter(), page, doc)
				case isEtsyProduct(page):
					return etsyProductPage(ctx, doc, page)
				case isTumblrPost(page):
					return tumblrPostPage(ctx, doc, page)
				case isYahooJPProduct(page):
					return yahooJPProductPage(ctx, doc, page)
				case isLazadaProduct(page):
					return lazadaProductPage(ctx, doc, page)
				default:
					return taobaoCollect(ctx, s.Getter(), page, doc)
				}
			}
		} else if page.String() != first.String() {
			return result, errors.New("采集期间网页地址发生变化，已保留发现的图片")
		}
		if snapshot.TooLarge {
			return result, errors.New("渲染网页超过 8 MiB 的扫描上限")
		}
		if err := addRenderedSnapshot(&result, seen, page, snapshot); err != nil {
			return result, err
		}
		if snapshot.Truncated {
			partial = errors.Join(partial, errors.New("渲染网页超过 10000 个元素的扫描上限"))
		}
		if snapshot.ScrollY+snapshot.Viewport >= snapshot.Height-2 || snapshot.Viewport <= 0 {
			break
		}
		if step == maxRenderedScrolls {
			partial = errors.Join(partial, errors.New("网页滚动超过 8 屏的扫描上限"))
			break
		}
		if err := s.run(ctx, chromedp.Evaluate(`window.scrollBy({top: window.innerHeight, behavior: "instant"})`, nil)); err != nil {
			return result, err
		}
		if err := Wait(ctx, 300*time.Millisecond); err != nil {
			return result, err
		}
	}
	if first == nil {
		return result, errors.New("没有可采集的网页")
	}
	return result, partial
}

func addRenderedSnapshot(result *Discovery, seen map[string]bool, page *url.URL, snapshot renderedSnapshot) error {
	base := page
	if u, err := crawlURL(page, snapshot.Base); err == nil {
		base = u
	}
	add := func(raw string) {
		u, err := crawlURL(base, raw)
		if err != nil || u.User != nil {
			return
		}
		normalized := weiboOriginalURL(page.String(), u.String())
		normalized, alternatives := twitterOriginalURLs(page.String(), normalized)
		if seen[normalized] {
			return
		}
		seen[normalized] = true
		item := Item{URL: normalized, AlternateURLs: alternatives}
		if ref := imageReferer(page.String(), item.URL); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	total := 0
	for _, markup := range snapshot.HTML {
		total += len(markup)
		if total > maxPageBytes {
			return errors.New("渲染网页超过 8 MiB 的扫描上限")
		}
		doc, err := html.Parse(strings.NewReader(markup))
		if err != nil {
			return err
		}
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			for _, raw := range imageCandidates(n) {
				add(raw)
			}
			if n.Data == "a" {
				if u, err := crawlURL(base, attribute(n, "href")); err == nil && imagePath(u.Path) {
					add(u.String())
				}
			}
		})
	}
	for _, raw := range snapshot.Current {
		add(raw)
	}
	for _, value := range snapshot.Background {
		for _, match := range cssURL.FindAllStringSubmatch(value, -1) {
			add(strings.Trim(strings.TrimSpace(match[1]), `"'`))
		}
	}
	return nil
}

func sameRenderedSite(start, current *url.URL) bool {
	if start == nil || current == nil || (start.Scheme == "https" && current.Scheme != "https") {
		return false
	}
	if sameOrigin(start, current) {
		return true
	}
	a, b := start.Hostname(), current.Hostname()
	if start.User == nil && (strings.EqualFold(a, "xhslink.com") || strings.EqualFold(a, "www.xhslink.com") ||
		strings.EqualFold(a, "xhslink.cn") || strings.EqualFold(a, "www.xhslink.cn")) &&
		((start.Scheme == "https" && (start.Port() == "" || start.Port() == "443")) ||
			(start.Scheme == "http" && (start.Port() == "" || start.Port() == "80"))) {
		_, note := xiaohongshuNoteID(current)
		return note
	}
	if strings.EqualFold(a, "500px.me") && is500pxChinaPage(start) && is500pxChinaPage(current) && current.Scheme == "https" &&
		(strings.EqualFold(b, "500px.com.cn") || strings.EqualFold(b, "www.500px.com.cn")) {
		return true
	}
	if isRajceAlbum(start) {
		return current.Scheme == "https" && isRajceAlbum(current) && rajceSameAlbum(start, current)
	}
	if isYiwugouProduct(start) && isYiwugouProduct(current) {
		return current.Scheme == "https" && yiwugouProductID(start) == yiwugouProductID(current)
	}
	if isKuaishouPost(start) && isKuaishouPost(current) {
		return current.Scheme == "https" && kuaishouPostID(start) == kuaishouPostID(current)
	}
	if isDouyinContent(start) && isDouyinContent(current) {
		return current.Scheme == "https" && douyinContentID(start) == douyinContentID(current)
	}
	if ref := archDailyPageRef(start); ref.id != "" && ref == archDailyPageRef(current) &&
		strings.HasSuffix(strings.ToLower(start.Hostname()), "archdaily.cn") &&
		strings.HasSuffix(strings.ToLower(current.Hostname()), "archdaily.com") {
		return current.Scheme == "https"
	}
	pinterestTarget := ""
	switch strings.TrimPrefix(strings.ToLower(a), "www.") {
	case "pinterest.fr":
		pinterestTarget = "fr.pinterest.com"
	case "pinterest.de":
		pinterestTarget = "de.pinterest.com"
	case "pinterest.co.uk":
		pinterestTarget = "uk.pinterest.com"
	}
	if pinterestTarget != "" && strings.EqualFold(b, pinterestTarget) && current.Scheme == "https" &&
		isPinterestPage(start) && pinterestPageRef(start) == pinterestPageRef(current) {
		return true
	}
	if net.ParseIP(a) != nil || net.ParseIP(b) != nil || strings.EqualFold(a, "localhost") || strings.EqualFold(b, "localhost") {
		return false
	}
	x, errX := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(a))
	y, errY := publicsuffix.EffectiveTLDPlusOne(strings.ToLower(b))
	if errX == nil && errY == nil && start.User == nil && current.User == nil && current.Scheme == "https" &&
		((x == "twitter.com" && y == "x.com") || (x == "x.com" && y == "twitter.com")) &&
		(current.Port() == "" || current.Port() == "443") {
		return true
	}
	return errX == nil && errY == nil && x == y
}

type renderedRedirectGuardKey struct{}

// Get streams a resource and applies browser cookies only to the captured origin.
// Redirects are processed individually so credentials never move across origins.
func (s *RenderedSession) Get(ctx context.Context, rawURL string, prepare func(*http.Request)) (*Response, error) {
	if err := s.tab.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	page := s.pageURL
	pageIP := s.pageIP
	startIP := s.startIP
	s.mu.RUnlock()
	if page == nil {
		return nil, errors.New("请先采集浏览器当前网页")
	}
	initial, err := crawlURL(nil, rawURL)
	if err != nil || initial.User != nil {
		return nil, errors.New("图片地址必须是不含账号密码的 HTTP / HTTPS 地址")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.tab, cancel)
	cleanup := func() { stop(); cancel() }
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, initial.String(), nil)
	if err != nil {
		cleanup()
		return nil, err
	}
	if prepare != nil {
		prepare(request)
	}
	request.URL = initial
	for name := range request.Header {
		switch http.CanonicalHeaderKey(name) {
		case "Accept", "Accept-Encoding", "User-Agent", "Referer", "Range", "If-Range":
		default:
			cleanup()
			return nil, fmt.Errorf("浏览器下载不支持自定义请求头 %s", name)
		}
	}
	for redirects := 0; redirects < 10; redirects++ {
		current, err := crawlURL(nil, request.URL.String())
		if err != nil || current.User != nil {
			cleanup()
			return nil, errors.New("图片重定向地址无效")
		}
		if guard, ok := ctx.Value(renderedRedirectGuardKey{}).(func(*url.URL) bool); ok && !guard(current) {
			cleanup()
			return nil, errors.New("浏览器请求的网页地址已改变，可能需要登录或安全验证")
		}
		if public, _ := ctx.Value(publicDownloadKey{}).(bool); public {
			if _, err := publicImageURL(current.String()); err != nil {
				cleanup()
				return nil, err
			}
		}
		var cookies []*network.Cookie
		if sameOrigin(page, current) {
			cookies, err = s.cookies(requestCtx, current)
			if err != nil {
				cleanup()
				return nil, err
			}
		}
		policy := safeDownloadPolicy{}
		if pageIP.IsValid() && pageIP == startIP && sameOrigin(s.start, page) && !isPublicDownloadIP(pageIP) && (sameOrigin(page, current) ||
			(pageIP.IsLoopback() && strings.EqualFold(page.Hostname(), current.Hostname()))) {
			policy.allowedLocal = pageIP
		}
		hopCtx := context.WithValue(requestCtx, safeDownloadPolicyKey{}, policy)
		hopCtx = context.WithValue(hopCtx, publicDownloadKey{}, false)
		response, err := s.getter.Get(hopCtx, current.String(), func(r *http.Request) {
			for name, values := range request.Header {
				name = http.CanonicalHeaderKey(name)
				if name == "Referer" || (!sameOrigin(initial, current) && (name == "Range" || name == "If-Range")) {
					continue
				}
				r.Header[name] = append([]string(nil), values...)
			}
			if ref := imageReferer(page.String(), current.String()); ref != "" {
				r.Header.Set("Referer", ref)
			} else {
				r.Header.Del("Referer")
			}
			for _, cookie := range cookies {
				r.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
			}
		})
		if err != nil {
			cleanup()
			return nil, err
		}
		if response.StatusCode == 301 || response.StatusCode == 302 || response.StatusCode == 303 || response.StatusCode == 307 || response.StatusCode == 308 {
			location := response.Header.Get("Location")
			if location != "" {
				_ = response.Body.Close()
				next, err := crawlURL(current, location)
				if err != nil || next.User != nil {
					cleanup()
					return nil, errors.New("图片重定向地址无效")
				}
				request.URL = next
				continue
			}
		}
		response.Body = &renderedBody{ReadCloser: response.Body, cleanup: cleanup}
		return response, nil
	}
	cleanup()
	return nil, errors.New("图片重定向次数过多")
}

func (s *RenderedSession) cookies(ctx context.Context, target *url.URL) ([]*network.Cookie, error) {
	var cookies []*network.Cookie
	err := s.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		var err error
		cookies, err = network.GetCookies().WithURLs([]string{target.String()}).Do(ec)
		return err
	}))
	if err != nil {
		return nil, err
	}
	filtered := cookies[:0]
	for _, cookie := range cookies {
		// shortcut: partitioned cookies are skipped until their top-level site can be validated.
		if cookie == nil || cookie.PartitionKey != nil || cookie.PartitionKeyOpaque || (cookie.Secure && target.Scheme != "https") ||
			(!cookie.Session && cookie.Expires > 0 && cookie.Expires <= float64(time.Now().Unix())) ||
			!renderedCookieMatches(cookie, target) {
			continue
		}
		filtered = append(filtered, cookie)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return len(filtered[i].Path) > len(filtered[j].Path) })
	return filtered, nil
}

func renderedCookieMatches(cookie *network.Cookie, target *url.URL) bool {
	host, domain := strings.ToLower(target.Hostname()), strings.ToLower(cookie.Domain)
	if strings.HasPrefix(domain, ".") {
		domain = strings.TrimPrefix(domain, ".")
		if host != domain && !strings.HasSuffix(host, "."+domain) {
			return false
		}
	} else if host != domain {
		return false
	}
	path := target.EscapedPath()
	if path == "" {
		path = "/"
	}
	cookiePath := cookie.Path
	if cookiePath == "" {
		cookiePath = "/"
	}
	return path == cookiePath || (strings.HasPrefix(path, cookiePath) &&
		(strings.HasSuffix(cookiePath, "/") || (len(path) > len(cookiePath) && path[len(cookiePath)] == '/')))
}

type safeDownloadPolicy struct{ allowedLocal netip.Addr }
type safeDownloadPolicyKey struct{}

var nonPublicDownloadPrefixes = func() []netip.Prefix {
	var prefixes []netip.Prefix
	for _, raw := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "5f00::/16",
	} {
		prefixes = append(prefixes, netip.MustParsePrefix(raw))
	}
	return prefixes
}()

func isPublicDownloadIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range nonPublicDownloadPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func safeDownloadDial(ctx context.Context, networkName, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	return safeDownloadDialWith(ctx, networkName, address, lookupTemuImageIPs, dialer.DialContext)
}

func safeDownloadDialWith(ctx context.Context, networkName, address string,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (networkName != "tcp" && networkName != "tcp4" && networkName != "tcp6") {
		return nil, errors.New("下载目标地址无效")
	}
	ips, err := lookup(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("解析下载目标失败: %w", err)
	}
	policy, _ := ctx.Value(safeDownloadPolicyKey{}).(safeDownloadPolicy)
	var last error
	for _, rawIP := range ips {
		ip := rawIP.Unmap()
		if !ip.IsValid() || (networkName == "tcp4" && !ip.Is4()) || (networkName == "tcp6" && !ip.Is6()) ||
			(!isPublicDownloadIP(ip) && (!policy.allowedLocal.IsValid() || ip != policy.allowedLocal.Unmap())) {
			continue
		}
		conn, err := dial(ctx, networkName, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last != nil {
		return nil, last
	}
	return nil, errors.New("出于安全限制，网页图片不能访问其他内网地址")
}

type renderedBody struct {
	io.ReadCloser
	cleanup func()
	once    sync.Once
	err     error
}

func (b *renderedBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close(); b.cleanup() })
	return b.err
}

func renderedLockFile(profileDir string) (*os.File, error) {
	name := filepath.Join(profileDir, ".tulai-rendered.lock")
	if info, err := os.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("下载浏览器锁文件不是普通文件")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开下载浏览器锁: %w", err)
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("下载浏览器锁文件不是普通文件")
	}
	return f, nil
}
