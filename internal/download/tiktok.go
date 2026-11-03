package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

var (
	tiktokUsername = regexp.MustCompile(`^[A-Za-z0-9._]{1,30}$`)
	tiktokNumber   = regexp.MustCompile(`^[0-9]{1,25}$`)
	tiktokNoState  = errors.New("未读取到 TikTok 当前帖子的图片数据，可能需要登录或验证")
)

// shortcut: bound a single carousel while preserving a partial-result warning.
const tiktokMaxImages = 100

type tiktokRef struct{ author, id, kind string }

func tiktokPageRef(u *url.URL) tiktokRef {
	if u == nil || u.User != nil ||
		!((u.Scheme == "https" && (u.Port() == "" || u.Port() == "443")) || (u.Scheme == "http" && (u.Port() == "" || u.Port() == "80"))) ||
		(!strings.EqualFold(u.Hostname(), "tiktok.com") && !strings.EqualFold(u.Hostname(), "www.tiktok.com")) ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") {
		return tiktokRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "@") || !tiktokUsername.MatchString(strings.TrimPrefix(parts[0], "@")) {
		return tiktokRef{}
	}
	ref := tiktokRef{author: strings.TrimPrefix(parts[0], "@"), kind: "profile"}
	if len(parts) == 1 {
		return ref
	}
	if len(parts) != 3 || (parts[1] != "video" && parts[1] != "photo") || !tiktokNumber.MatchString(parts[2]) || strings.Trim(parts[2], "0") == "" {
		return tiktokRef{}
	}
	ref.kind, ref.id = parts[1], parts[2]
	return ref
}

func isTikTokPage(u *url.URL) bool { return tiktokPageRef(u).kind != "" }

func tiktokAsset(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" ||
		strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".tiktokcdn-us.com") && !strings.HasSuffix(host, ".tiktokcdn.com") {
		return "", false
	}
	if !strings.Contains(u.Path, "-i-photomode-") {
		return "", false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		return "", false
	}
	return u.String(), true
}

type tiktokItem struct {
	ID                string                     `json:"id"`
	Description       string                     `json:"desc"`
	Secret            *bool                      `json:"secret"`
	ForFriend         *bool                      `json:"forFriend"`
	PrivateItem       *bool                      `json:"privateItem"`
	Prohibited        *bool                      `json:"isProhibited"`
	Reviewing         *bool                      `json:"isReviewing"`
	ShareEnabled      *bool                      `json:"shareEnabled"`
	TakeDown          *int                       `json:"takeDown"`
	Ad                *bool                      `json:"isAd"`
	ItemMute          *bool                      `json:"itemMute"`
	ContentClassified *bool                      `json:"isContentClassified"`
	SubVideoMeta      json.RawMessage            `json:"subVideoMeta"`
	WarnInfo          []json.RawMessage          `json:"warnInfo"`
	ItemControl       map[string]json.RawMessage `json:"item_control"`
	Author            struct {
		UniqueID        string `json:"uniqueId"`
		PrivateAccount  *bool  `json:"privateAccount"`
		Secret          *bool  `json:"secret"`
		DownloadSetting *int   `json:"downloadSetting"`
	} `json:"author"`
	ImagePost *struct {
		Images []struct {
			ImageURL struct {
				URLs []string `json:"urlList"`
			} `json:"imageURL"`
		} `json:"images"`
	} `json:"imagePost"`
}

type tiktokState struct {
	Scope struct {
		Detail struct {
			StatusCode *int `json:"statusCode"`
			ItemInfo   struct {
				Item tiktokItem `json:"itemStruct"`
			} `json:"itemInfo"`
		} `json:"webapp.video-detail"`
	} `json:"__DEFAULT_SCOPE__"`
}

// The website's download menu requires a public author with downloadSetting 0.
// Additional explicit post flags must also confirm that the post is public.
func tiktokAllowed(item tiktokItem) bool {
	if item.Author.PrivateAccount == nil || *item.Author.PrivateAccount || item.Author.Secret == nil || *item.Author.Secret ||
		item.Author.DownloadSetting == nil || *item.Author.DownloadSetting != 0 ||
		item.Secret == nil || *item.Secret || item.ForFriend == nil || *item.ForFriend || item.PrivateItem == nil || *item.PrivateItem ||
		item.Prohibited == nil || *item.Prohibited || item.Reviewing == nil || *item.Reviewing || item.ShareEnabled == nil || !*item.ShareEnabled ||
		item.TakeDown == nil || *item.TakeDown != 0 || (item.Ad != nil && *item.Ad) || (item.ItemMute != nil && *item.ItemMute) ||
		(item.ContentClassified != nil && *item.ContentClassified) || len(item.WarnInfo) != 0 {
		return false
	}
	if len(item.SubVideoMeta) != 0 && strings.TrimSpace(string(item.SubVideoMeta)) != "null" {
		return false
	}
	for _, key := range []string{"can_download", "canDownload"} {
		if raw, ok := item.ItemControl[key]; ok {
			var allowed bool
			if json.Unmarshal(raw, &allowed) != nil || !allowed {
				return false
			}
		}
	}
	return true
}

// Read only the current post's bounded JSON data. Scripts are never executed.
func tiktokPhotoPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := tiktokPageRef(page)
	if doc == nil || ref.id == "" {
		return result, errors.New("TikTok 图文地址无效")
	}
	var state tiktokState
	found, ambiguous := false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "script" || attribute(n, "id") != "__UNIVERSAL_DATA_FOR_REHYDRATION__" {
			return
		}
		if found {
			ambiguous = true
			return
		}
		found = true
		var raw strings.Builder
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				if raw.Len()+len(child.Data) > maxPageBytes {
					ambiguous = true
					return
				}
				raw.WriteString(child.Data)
			}
		}
		if json.Unmarshal([]byte(raw.String()), &state) != nil {
			ambiguous = true
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !found || ambiguous || state.Scope.Detail.StatusCode == nil {
		return result, tiktokNoState
	}
	if *state.Scope.Detail.StatusCode != 0 {
		return result, errors.New("TikTok 帖子不可访问，可能需要登录或验证")
	}
	item := state.Scope.Detail.ItemInfo.Item
	if item.ID != ref.id || !strings.EqualFold(item.Author.UniqueID, ref.author) {
		return result, errors.New("TikTok 帖子或作者身份不匹配")
	}
	if !tiktokAllowed(item) {
		return result, errors.New("TikTok 作者未允许下载，或帖子的访问权限无法确认")
	}
	if item.ImagePost == nil {
		return result, errors.New("TikTok 当前帖子不是可采集的图文帖子")
	}
	if len(item.ImagePost.Images) == 0 {
		return result, errors.New("TikTok 图文帖子没有可确认的图片")
	}
	images := item.ImagePost.Images
	partial := false
	if len(images) > tiktokMaxImages {
		images = images[:tiktokMaxImages]
		partial = true
	}
	result.Title = strings.TrimSpace(item.Description)
	seen := make(map[string]bool)
	for _, img := range images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		chosen := ""
		for _, raw := range img.ImageURL.URLs {
			if asset, ok := tiktokAsset(raw); ok {
				chosen = asset
				break
			}
		}
		if chosen == "" {
			partial = true
			continue
		}
		if seen[chosen] {
			continue
		}
		seen[chosen] = true
		result.Items = append(result.Items, Item{URL: chosen, Headers: map[string]string{"Referer": "https://www.tiktok.com/"}, RequireImage: true, PublicOnly: true,
			Metadata: map[string]string{"tiktok_author": item.Author.UniqueID, "tiktok_post_id": item.ID}})
	}
	if partial {
		return result, errors.New("部分 TikTok 图片地址无法确认或达到上限，结果不完整")
	}
	return result, nil
}

func discoverTikTok(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := tiktokPageRef(page)
	if getter == nil || ref.kind == "" {
		return result, errors.New("TikTok 页面或下载客户端无效")
	}
	if ref.kind == "profile" {
		return result, errors.New("TikTok 作者主页请使用下载浏览器采集公开图文")
	}
	// TikTok's current public /photo route may contain only a shell. The
	// first-party /video route exposes the same photo item; never read video URLs.
	target := &url.URL{Scheme: "https", Host: "www.tiktok.com", Path: "/@" + ref.author + "/video/" + ref.id}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: target.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("TikTok 页面不可访问，可能需要登录或验证")
	}
	finalRef := tiktokPageRef(final)
	if doc == nil || !sameOrigin(target, final) || finalRef.kind != "video" || finalRef.id != ref.id || !strings.EqualFold(finalRef.author, ref.author) {
		return result, errors.New("TikTok 页面已跳转到其他帖子，或需要登录或验证")
	}
	return tiktokPhotoPage(ctx, doc, final)
}

// captureTikTok keeps the user's page open while a same-session child reads the
// official alternate route. It never navigates the user's page or downloads video.
func (s *RenderedSession) captureTikTok(ctx context.Context, doc *html.Node, page *url.URL, observedURL string) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := tiktokPageRef(page)
	if ref.kind == "profile" {
		return s.captureTikTokProfile(ctx, doc, page, observedURL)
	}
	result, err := tiktokPhotoPage(ctx, doc, page)
	if !errors.Is(err, tiktokNoState) || ref.kind != "photo" {
		return result, err
	}
	if page.Scheme != "https" {
		return result, errors.New("请等待 TikTok 的 HTTPS 页面加载后重试")
	}
	checkOriginal := func() error {
		var raw string
		if err := s.run(ctx, chromedp.Location(&raw)); err != nil {
			return err
		}
		current, err := crawlURL(nil, raw)
		currentRef := tiktokPageRef(current)
		if err != nil || raw != observedURL || !sameOrigin(page, current) || currentRef.kind != "photo" ||
			currentRef.id != ref.id || !strings.EqualFold(currentRef.author, ref.author) {
			return errors.New("采集期间 TikTok 原页面已更改")
		}
		return nil
	}
	if err := checkOriginal(); err != nil {
		return result, err
	}
	return s.captureTikTokPhotoTarget(ctx, page, checkOriginal)
}

// shortcut: profile scans stop after 100 scrolls, 1000 posts or 10000 images;
// the site can load further posts and does not expose a verified end marker.
const (
	tiktokProfileMaxScrolls = 100
	tiktokProfileMaxPosts   = 1000
	tiktokProfileMaxPhotos  = 1000
	tiktokProfileMaxImages  = 10000
	tiktokProfileMaxCards   = 2000
)

// Check the site's own user-detail state before trusting the rendered post list.
func tiktokCheckProfile(ctx context.Context, doc *html.Node, page *url.URL) error {
	ref := tiktokPageRef(page)
	if err := ctx.Err(); err != nil {
		return err
	}
	if doc == nil || ref.kind != "profile" {
		return errors.New("TikTok 作者主页无效")
	}
	var state struct {
		Scope struct {
			Detail struct {
				StatusCode *int `json:"statusCode"`
				UserInfo   struct {
					User struct {
						UniqueID        string `json:"uniqueId"`
						PrivateAccount  *bool  `json:"privateAccount"`
						Secret          *bool  `json:"secret"`
						DownloadSetting *int   `json:"downloadSetting"`
					} `json:"user"`
				} `json:"userInfo"`
			} `json:"webapp.user-detail"`
		} `json:"__DEFAULT_SCOPE__"`
	}
	found, invalid := false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "script" || attribute(n, "id") != "__UNIVERSAL_DATA_FOR_REHYDRATION__" {
			return
		}
		if found {
			invalid = true
			return
		}
		found = true
		var raw strings.Builder
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				if raw.Len()+len(child.Data) > maxPageBytes {
					invalid = true
					return
				}
				raw.WriteString(child.Data)
			}
		}
		if json.Unmarshal([]byte(raw.String()), &state) != nil {
			invalid = true
		}
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	detail := state.Scope.Detail
	if !found || invalid || detail.StatusCode == nil {
		return errors.New("未读取到 TikTok 作者主页数据，可能需要登录或验证")
	}
	if *detail.StatusCode != 0 {
		return errors.New("TikTok 作者主页不可访问，可能需要登录或验证")
	}
	user := detail.UserInfo.User
	if !strings.EqualFold(user.UniqueID, ref.author) {
		return errors.New("TikTok 作者主页身份不匹配")
	}
	if user.PrivateAccount == nil || *user.PrivateAccount || user.Secret == nil || *user.Secret || user.DownloadSetting == nil || *user.DownloadSetting != 0 {
		return errors.New("TikTok 作者未允许下载，或主页访问权限无法确认")
	}
	return nil
}

// Only use a post link supplied by a card on the current author's site.
func tiktokProfileLink(page *url.URL, raw string) (*url.URL, tiktokRef, bool) {
	if len(raw) == 0 || len(raw) > 4096 {
		return nil, tiktokRef{}, false
	}
	u, err := crawlURL(page, raw)
	ref, author := tiktokPageRef(u), tiktokPageRef(page)
	if err != nil || author.kind != "profile" || u.User != nil || !sameOrigin(page, u) || (ref.kind != "photo" && ref.kind != "video") || !strings.EqualFold(ref.author, author.author) {
		return nil, tiktokRef{}, false
	}
	// Tracking queries and fragments are never needed to identify these posts.
	return &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/@" + ref.author + "/" + ref.kind + "/" + ref.id}, ref, true
}

type tiktokProfileSnapshot struct {
	URL, State, Title, Status string
	Links                     []string
	Truncated, AtBottom       bool
}

// The user-post-item-list and user-post-item markers are from the live profile.
// Read a bounded number of card links; never read a video's media address.
const tiktokProfileScript = `(() => {
  const visible = e => {
    const r=e.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    for (let n=e; n && n.nodeType===1; n=n.parentElement) {
      const s=getComputedStyle(n);
      if (n.hidden || n.getAttribute('aria-hidden') === 'true' || s.display === 'none' || s.visibility !== 'visible' || Number(s.opacity) === 0) return false;
    }
    return true;
  };
  const body = (document.body?.innerText || '').slice(0,100000);
  const challenges = [...document.querySelectorAll('[id*="captcha" i], [class*="captcha" i], iframe[src*="captcha" i]')];
  if (challenges.some(visible) || /verify (you are|to continue)|security check|unusual activity|complete the security/i.test(body)) return {url:location.href,status:'verify'};
  const dialogs = [...document.querySelectorAll('[role="dialog"]')].filter(visible);
  if (dialogs.some(e => /log in to|sign in to/i.test(e.innerText || '')) || /log in to (continue|view|see|watch)/i.test(body)) return {url:location.href,status:'login'};
  if (/this account is private/i.test(body)) return {url:location.href,status:'private'};
  const roots = document.querySelectorAll('[data-e2e="user-post-item-list"]');
  if (roots.length !== 1 || !roots[0].closest('[data-e2e="user-page"]')) return {url:location.href,status:'loading'};
  const nodes = document.querySelectorAll('script#__UNIVERSAL_DATA_FOR_REHYDRATION__');
  const state = %t && nodes.length === 1 ? (nodes[0].textContent || '') : '';
  const cards = roots[0].querySelectorAll('[data-e2e="user-post-item"]');
  const links = [];
  for (let i=0; i<cards.length && i<%d; i++) {
    const a=cards[i].querySelector('a[href]');
    const r=cards[i].getBoundingClientRect();
    if (!a || !visible(cards[i]) || !visible(a) || r.bottom <= 0 || r.top >= innerHeight || r.right <= 0 || r.left >= innerWidth) continue;
    const href=a && a.getAttribute('href');
    if (typeof href === 'string' && href.length <= 4096) links.push(href);
  }
  const scroller=document.scrollingElement;
  const atBottom=!scroller || scroller.scrollTop+scroller.clientHeight >= scroller.scrollHeight-2;
  return {url:location.href,status:'ok',title:(document.title||'').slice(0,512),state:state.length > 8388608 ? '' : state,links,truncated:cards.length > %d,atBottom};
})()`

func (s *RenderedSession) captureTikTokProfile(ctx context.Context, doc *html.Node, page *url.URL, observedURL string) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := tiktokPageRef(page)
	if page == nil || page.Scheme != "https" || ref.kind != "profile" {
		return result, errors.New("请等待 TikTok 作者主页的 HTTPS 页面加载后重试")
	}
	if err := tiktokCheckProfile(ctx, doc, page); err != nil {
		return result, err
	}
	checkOriginal := func() error {
		var raw string
		if err := s.run(ctx, chromedp.Location(&raw)); err != nil {
			return err
		}
		current, err := crawlURL(nil, raw)
		currentRef := tiktokPageRef(current)
		if err != nil || raw != observedURL || !sameOrigin(page, current) || currentRef.kind != "profile" || !strings.EqualFold(currentRef.author, ref.author) {
			return errors.New("采集期间 TikTok 原页面已更改")
		}
		return nil
	}
	if err := checkOriginal(); err != nil {
		return result, err
	}
	child, closeChild, err := camoufox.NewPage(ctx, s.tab, false)
	if err != nil {
		return result, errors.New("无法创建 TikTok 作者主页采集标签")
	}
	defer closeChild()
	stopOnCancel := context.AfterFunc(ctx, closeChild)
	defer stopOnCancel()
	run := func(actions ...camoufox.Action) error {
		op, cancel := context.WithTimeout(child, 45*time.Second)
		stop := context.AfterFunc(ctx, cancel)
		defer func() { stop(); cancel() }()
		err := camoufox.Run(op, actions...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	target := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/@" + ref.author}
	var visited atomic.Bool
	camoufox.ListenTarget(child, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		allowed := false
		if paused.Request != nil && paused.Request.Method == "GET" && paused.ResourceType == network.ResourceTypeDocument {
			request, err := crawlURL(nil, paused.Request.URL)
			allowed = err == nil && request.User == nil && sameOrigin(target, request) && request.Path == target.Path &&
				request.RawQuery == "" && request.Fragment == "" && visited.CompareAndSwap(false, true)
		}
		go func() {
			var action camoufox.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if allowed {
				action = fetch.ContinueRequest(paused.RequestID)
			}
			_ = camoufox.Run(child, action)
		}()
	})
	if err := run(fetch.Enable().WithPatterns([]*fetch.RequestPattern{{ResourceType: network.ResourceTypeDocument}})); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("无法设置 TikTok 作者主页采集标签的访问限制")
	}
	if err := run(camoufox.Navigate(target.String())); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("TikTok 作者主页未加载，可能需要登录或验证")
	}
	var photos []*url.URL
	seenPosts, seenPhotos := make(map[string]bool), make(map[string]bool)
	profileChecked, invalid, truncated, unchanged := false, false, false, 0
	for step := 0; step <= tiktokProfileMaxScrolls; step++ {
		if err := checkOriginal(); err != nil {
			return result, err
		}
		var snapshot tiktokProfileSnapshot
		for attempt := 0; attempt < 12; attempt++ {
			script := fmt.Sprintf(tiktokProfileScript, !profileChecked, tiktokProfileMaxCards, tiktokProfileMaxCards)
			if err := run(chromedp.Evaluate(script, &snapshot)); err != nil {
				return result, err
			}
			if snapshot.URL != target.String() {
				return result, errors.New("TikTok 作者主页采集标签已跳转，可能需要登录或验证")
			}
			if snapshot.Status != "loading" {
				break
			}
			if err := Wait(ctx, 500*time.Millisecond); err != nil {
				return result, err
			}
		}
		switch snapshot.Status {
		case "verify":
			return result, errors.New("TikTok 要求安全验证，请在下载浏览器中完成验证后重试")
		case "login":
			return result, errors.New("TikTok 作者主页需要登录，请在下载浏览器中登录后重试")
		case "private":
			return result, errors.New("TikTok 作者主页不公开，无法确认图文下载权限")
		case "ok":
		default:
			return result, errors.New("未加载 TikTok 作者作品列表，可能需要登录或验证")
		}
		if !profileChecked {
			n := &html.Node{Type: html.DocumentNode}
			script := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "id", Val: "__UNIVERSAL_DATA_FOR_REHYDRATION__"}}}
			script.AppendChild(&html.Node{Type: html.TextNode, Data: snapshot.State})
			n.AppendChild(script)
			if err := tiktokCheckProfile(ctx, n, target); err != nil {
				return result, err
			}
			profileChecked = true
			result.Title = strings.TrimSpace(snapshot.Title)
		}
		before := len(seenPosts)
		truncated = truncated || snapshot.Truncated
		for _, raw := range snapshot.Links {
			u, post, ok := tiktokProfileLink(target, raw)
			if !ok {
				invalid = true
				continue
			}
			if !seenPosts[post.id] {
				if len(seenPosts) >= tiktokProfileMaxPosts {
					truncated = true
					break
				}
				seenPosts[post.id] = true
			}
			if post.kind == "photo" && !seenPhotos[post.id] {
				if len(photos) >= tiktokProfileMaxPhotos {
					truncated = true
					break
				}
				seenPhotos[post.id] = true
				photos = append(photos, u)
			}
		}
		if snapshot.AtBottom && len(seenPosts) == before {
			unchanged++
		} else {
			unchanged = 0
		}
		if truncated || unchanged >= 4 || step == tiktokProfileMaxScrolls {
			break
		}
		if err := run(chromedp.Evaluate(`window.scrollBy(0, Math.max(200, window.innerHeight * 0.8))`, nil)); err != nil {
			return result, err
		}
		if err := Wait(ctx, 750*time.Millisecond); err != nil {
			return result, err
		}
	}
	closeChild()
	partial := errors.New("TikTok 作者主页仅采集已加载的图文，分页及其他形式的图文可能遗漏，结果不完整")
	if invalid {
		partial = errors.Join(partial, errors.New("部分作品链接与当前作者或站点不匹配"))
	}
	if truncated {
		partial = errors.Join(partial, errors.New("TikTok 作者作品超过采集上限"))
	}
	if len(photos) == 0 {
		return result, errors.Join(partial, errors.New("未找到当前作者可确认的图文帖子"))
	}
	seenImages := make(map[string]bool)
	for _, photo := range photos {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := checkOriginal(); err != nil {
			return result, err
		}
		found, err := s.captureTikTokPhotoTarget(ctx, photo, checkOriginal)
		for _, item := range found.Items {
			if seenImages[item.URL] {
				continue
			}
			if len(result.Items) >= tiktokProfileMaxImages {
				return result, errors.Join(partial, errors.New("TikTok 作者图片超过采集上限"))
			}
			seenImages[item.URL] = true
			result.Items = append(result.Items, item)
		}
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, errors.Join(partial, err)
		}
	}
	return result, partial
}

// captureTikTokPhotoTarget loads one site-provided photo reference without
// navigating the original tab or requesting media resources in the child.
func (s *RenderedSession) captureTikTokPhotoTarget(ctx context.Context, page *url.URL, checkOriginal func() error) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := tiktokPageRef(page)
	child, closeChild, err := camoufox.NewPage(ctx, s.tab, false)
	if err != nil {
		return result, errors.New("无法创建 TikTok 采集标签")
	}
	defer closeChild()
	stopOnCancel := context.AfterFunc(ctx, closeChild)
	defer stopOnCancel()
	run := func(actions ...camoufox.Action) error {
		op, cancel := context.WithTimeout(child, 45*time.Second)
		stop := context.AfterFunc(ctx, cancel)
		defer func() { stop(); cancel() }()
		err := camoufox.Run(op, actions...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	target := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/@" + ref.author + "/video/" + ref.id}
	var visited atomic.Bool
	camoufox.ListenTarget(child, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		allowed := false
		if paused.Request != nil && paused.Request.Method == "GET" && paused.ResourceType == network.ResourceTypeDocument {
			request, err := crawlURL(nil, paused.Request.URL)
			allowed = err == nil && request.User == nil && sameOrigin(target, request) && request.Path == target.Path &&
				request.RawQuery == "" && request.Fragment == "" && visited.CompareAndSwap(false, true)
		}
		go func() {
			var action camoufox.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if allowed {
				action = fetch.ContinueRequest(paused.RequestID)
			}
			_ = camoufox.Run(child, action)
		}()
	})
	if err := run(fetch.Enable()); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("无法设置 TikTok 采集标签的访问限制")
	}
	if err := run(camoufox.Navigate(target.String())); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("TikTok 帖子未加载，可能需要登录或验证")
	}
	var snapshot struct {
		URL, State string
		Count      int
		TooLarge   bool
	}
	const stateScript = `(() => {
	  const nodes = document.querySelectorAll('script#__UNIVERSAL_DATA_FOR_REHYDRATION__');
	  const text = nodes.length === 1 ? (nodes[0].textContent || '') : '';
	  return {url:location.href,count:nodes.length,tooLarge:text.length > 8388608,state:text.length > 8388608 ? '' : text};
	})()`
	if err := run(chromedp.Evaluate(stateScript, &snapshot)); err != nil {
		return result, err
	}
	current, err := crawlURL(nil, snapshot.URL)
	currentRef := tiktokPageRef(current)
	if err != nil || !sameOrigin(target, current) || currentRef.kind != "video" || currentRef.id != ref.id ||
		!strings.EqualFold(currentRef.author, ref.author) || current.RawQuery != "" || current.Fragment != "" {
		return result, errors.New("TikTok 采集标签已跳转，可能需要登录或验证")
	}
	if snapshot.Count != 1 || snapshot.TooLarge || len(snapshot.State) > maxPageBytes {
		return result, tiktokNoState
	}
	parsed := &html.Node{Type: html.DocumentNode}
	script := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "id", Val: "__UNIVERSAL_DATA_FOR_REHYDRATION__"}}}
	script.AppendChild(&html.Node{Type: html.TextNode, Data: snapshot.State})
	parsed.AppendChild(script)
	result, err = tiktokPhotoPage(ctx, parsed, current)
	var childURL string
	if childErr := run(chromedp.Location(&childURL)); childErr != nil {
		return Discovery{IsPage: true}, childErr
	}
	if childURL != snapshot.URL {
		return Discovery{IsPage: true}, errors.New("TikTok 采集标签已更改，可能需要登录或验证")
	}
	if originalErr := checkOriginal(); originalErr != nil {
		return Discovery{IsPage: true}, originalErr
	}
	return result, err
}
