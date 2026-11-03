package download

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Discovery 区分网页和直链；网页的 Items 已去重并带有来源页 Referer。
type Discovery struct {
	IsPage bool
	Title  string
	Items  []Item
	// Response 仅用于非网页根地址；调用方负责关闭 Body。
	Response *Response
}

// shortcut: 最多遍历 100 页、每页 8 MiB；需要整站大规模抓取时改为可配置。
const (
	maxCrawlPages = 100
	maxPageBytes  = 8 << 20
)

type crawlPage struct {
	url, referer string
	depth        int
}

// Discover 从网页发现图片；非网页交由现有直链下载器处理。
// 子页面失败时返回已发现的图片和错误，调用方可以提示部分结果。
func Discover(ctx context.Context, getter Getter, rawURL string, depth int) (Discovery, error) {
	var result Discovery
	if QzoneURLHasCredentials(strings.TrimSpace(rawURL)) {
		return result, &LocalError{Err: errors.New("QQ 相册网址包含登录令牌，请移除后重试")}
	}
	if getter == nil {
		return result, &LocalError{Err: errors.New("没有配置下载客户端")}
	}
	root, err := crawlInputURL(rawURL)
	if err != nil {
		return result, &LocalError{Err: err}
	}
	if _, personal := qzonePersonalOwner(root); personal {
		return Discovery{IsPage: true}, errors.New("QQ 空间相册请使用下载浏览器登录后采集")
	}
	if qzonePersonalAlbumDetail(root) {
		return Discovery{IsPage: true}, errors.New("QQ 空间单相册详情暂不支持，请打开相册列表后重试")
	}
	if _, groupList := qzoneGroupAlbumListID(root); groupList {
		return Discovery{IsPage: true}, errors.New("QQ 群相册请使用下载浏览器登录后采集")
	}
	var found Discovery
	var searchErr error
	switch {
	case isBaiduImageSearch(root):
		found, searchErr = discoverBaiduImages(ctx, getter, root)
	case isBingImageDetail(root):
		return discoverBingDetail(ctx, getter, root)
	case isBingImageSearch(root):
		found, searchErr = discoverBingImages(ctx, getter, root)
	case isTiebaThread(root):
		return discoverTiebaThread(ctx, getter, root)
	case is1688AlbumList(root):
		found, searchErr = discover1688Albums(ctx, getter, root)
	case album1688DetailID(root) != "":
		found, searchErr = discover1688Album(ctx, getter, root)
	case is1688Offer(root):
		return discover1688Offer(ctx, getter, root)
	case isBilibiliPost(root):
		return discoverBilibiliPost(ctx, getter, root)
	case isBilibiliSpace(root):
		return discoverBilibiliSpace(ctx, getter, root)
	case isImgurPage(root):
		return discoverImgurPage(ctx, getter, root)
	case isTaobaoItem(root):
		return discoverTaobaoItem(ctx, getter, root)
	case isPixivArtwork(root):
		return discoverPixivArtwork(ctx, getter, root)
	case isPixivUser(root):
		return discoverPixivUser(ctx, getter, root)
	case isFlickrPage(root):
		return discoverFlickr(ctx, getter, root)
	case isWeiboContent(root):
		return discoverWeiboContent(ctx, getter, root)
	case isDoubanPage(root):
		return discoverDouban(ctx, getter, root)
	case isHuabanPage(root):
		return discoverHuaban(ctx, getter, root)
	case isWechatArticle(root):
		return discoverWechatArticle(ctx, getter, root)
	case isArtstationPage(root):
		return discoverArtstation(ctx, getter, root)
	case isJDProduct(root):
		return discoverJDProduct(ctx, getter, root)
	case isAliExpressProduct(root):
		return discoverAliExpressProduct(ctx, getter, root)
	case isAmazonSearch(root):
		return discoverAmazonSearch(ctx, getter, root)
	case isAmazonProduct(root):
		return discoverAmazonProduct(ctx, getter, root)
	case isInstagramPost(root):
		return discoverInstagramPost(ctx, getter, root)
	case isPinduoduoProduct(root):
		return discoverPinduoduoProduct(ctx, getter, root)
	case isTuchongPage(root):
		return discoverTuchong(ctx, getter, root)
	case isBehanceProject(root), isBehanceProfile(root):
		return discoverBehance(ctx, getter, root)
	case isZcoolWork(root):
		return discoverZcoolWork(ctx, getter, root)
	case is500pxChinaPage(root):
		return discover500pxChina(ctx, getter, root)
	case is500pxPage(root):
		return discover500px(ctx, getter, root)
	case isAlibabaProduct(root):
		return discoverAlibabaProduct(ctx, getter, root)
	case isDuitangPage(root):
		return discoverDuitang(ctx, getter, root)
	case isWeishangPage(root):
		return discoverWeishang(ctx, getter, root)
	case isPinterestPage(root):
		return discoverPinterest(ctx, getter, root)
	case isEbaySearch(root):
		return discoverEbaySearch(ctx, getter, root)
	case isEbayProduct(root):
		return discoverEbayProduct(ctx, getter, root)
	case isDeviantArtWork(root):
		return discoverDeviantArtWork(ctx, getter, root)
	case isShopeeProduct(root):
		return discoverShopeeProduct(ctx, getter, root)
	case isEtsyProduct(root):
		return discoverEtsyProduct(ctx, getter, root)
	case isTumblrPost(root):
		return discoverTumblrPost(ctx, getter, root)
	case isYahooJPProduct(root):
		return discoverYahooJPProduct(ctx, getter, root)
	case isLazadaProduct(root):
		return discoverLazadaProduct(ctx, getter, root)
	case isDouyinProduct(root):
		return discoverDouyinProduct(ctx, getter, root)
	case isYouzanProduct(root):
		return discoverYouzanProduct(ctx, getter, root)
	case isIMDbPage(root):
		return discoverIMDb(ctx, getter, root)
	case isSinaBlogArticle(root):
		return discoverSinaBlogArticle(ctx, getter, root)
	case isSinaBlogList(root):
		return discoverSinaBlogList(ctx, getter, root)
	case isDHgateProduct(root):
		return discoverDHgateProduct(ctx, getter, root)
	case isWeidianProduct(root):
		return discoverWeidianProduct(ctx, getter, root)
	case isYupooPage(root):
		return discoverYupoo(ctx, getter, root)
	case isZOLPage(root):
		return discoverZOL(ctx, getter, root)
	case isAutohomePhoto(root):
		return discoverAutohomePhoto(ctx, getter, root)
	case isMadeInChinaProduct(root):
		return discoverMadeInChinaProduct(ctx, getter, root)
	case isDangdangProduct(root):
		return discoverDangdangProduct(ctx, getter, root)
	case isVIPProduct(root):
		return discoverVIPProduct(ctx, getter, root)
	case isSogouImageSearch(root):
		return discoverSogouImages(ctx, getter, root)
	case isOzonProduct(root):
		return discoverOzonProduct(ctx, getter, root)
	case isSOImageSearch(root):
		return discoverSOImages(ctx, getter, root)
	case isZhihuPage(root):
		return discoverZhihu(ctx, getter, root)
	case isUnsplashPage(root):
		return discoverUnsplash(ctx, getter, root)
	case isYanxuanProduct(root):
		return discoverYanxuanProduct(ctx, getter, root)
	case isJJsHouseProduct(root):
		return discoverJJsHouse(ctx, getter, root)
	case isBaijiahaoPage(root):
		return discoverBaijiahao(ctx, getter, root)
	case isToutiaoArticle(root):
		return discoverToutiaoArticle(ctx, getter, root)
	case isAicaigouProduct(root):
		return discoverAicaigouProduct(ctx, getter, root)
	case isAicaigouShop(root):
		return discoverAicaigouShop(ctx, getter, root)
	case isRajceAlbum(root):
		return discoverRajceAlbum(ctx, getter, root)
	case isRajceProfile(root):
		return Discovery{IsPage: true}, rajceProfileUnsupported()
	case isSpoonflowerPage(root):
		return discoverSpoonflower(ctx, getter, root)
	case isMogujieProduct(root):
		return discoverMogujieProduct(ctx, getter, root)
	case isCNUWork(root):
		return discoverCNUWork(ctx, getter, root)
	case isCNUUser(root):
		return Discovery{IsPage: true}, cnuUserUnsupported()
	case isYandePage(root):
		return discoverYande(ctx, getter, root)
	case isSuningProduct(root):
		return discoverSuningProduct(ctx, getter, root)
	case isYiwugouProduct(root):
		return discoverYiwugouProduct(ctx, getter, root)
	case isCtripAlbum(root):
		return discoverCtripAlbum(ctx, getter, root)
	case isGoooodPage(root):
		return discoverGooood(ctx, getter, root)
	case isFangAlbum(root):
		return discoverFangAlbum(ctx, getter, root)
	case isArchDailyPage(root):
		return discoverArchDaily(ctx, getter, root)
	case isPhotoplusPage(root):
		return discoverPhotoplus(ctx, getter, root)
	case isWSYProduct(root):
		return discoverWSYProduct(ctx, getter, root)
	case isPexelsPhoto(root):
		return discoverPexels(ctx, getter, root)
	case isTemuSearch(root):
		return discoverTemuSearch(ctx, getter, root)
	case isTemuProduct(root):
		return discoverTemuProduct(ctx, getter, root)
	case isSheinProduct(root):
		return discoverShein(ctx, getter, root)
	case isGelbooruPage(root):
		return discoverGelbooru(ctx, getter, root)
	case isXianyuItem(root):
		return discoverXianyu(ctx, getter, root)
	case isDewuProduct(root):
		return discoverDewuProduct(ctx, getter, root)
	case isPailixiangPage(root):
		return discoverPailixiang(ctx, getter, root)
	case isWikiArtAuthor(root):
		return discoverWikiArt(ctx, getter, root)
	case isKuaishouPost(root):
		return discoverKuaishouPost(ctx, getter, root)
	case isAlltuuAlbum(root):
		return Discovery{IsPage: true}, alltuuUnsupported()
	case isDouyinContent(root):
		return discoverDouyinContent(ctx, getter, root)
	case isIssuuDocument(root):
		return discoverIssuu(ctx, getter, root)
	case isHouzzProject(root):
		return discoverHouzzProject(ctx, getter, root)
	case isMihuashiPage(root):
		return discoverMihuashi(ctx, getter, root)
	case isLightInTheBoxProduct(root):
		return discoverLightInTheBoxProduct(ctx, getter, root)
	case isOnexPhoto(root) || isOnexGallery(root):
		return discoverOnex(ctx, getter, root)
	case isPhotography35Photo(root):
		return discoverPhotography35Photo(ctx, getter, root)
	case isPhotography35Author(root):
		return discoverPhotography35Author(ctx, getter, root)
	case isPconlinePhoto(root):
		return discoverPconlinePhoto(ctx, getter, root)
	case isImgsrcPage(root):
		return discoverImgsrc(ctx, getter, root)
	case isMtimePage(root):
		return discoverMtime(ctx, getter, root)
	case isAnimePicturesPage(root):
		return discoverAnimePictures(ctx, getter, root)
	case isFuraffinityPage(root):
		return discoverFuraffinity(ctx, getter, root)
	case isGomeProduct(root):
		return discoverGome(ctx, getter, root)
	case isDianpingPage(root):
		return discoverDianping(ctx, getter, root)
	case isNipicPage(root):
		return discoverNipic(ctx, getter, root)
	case isQyerAlbum(root):
		return discoverQyerAlbum(ctx, getter, root)
	case isOnlyFansSite(root):
		return discoverOnlyFans(ctx, getter, root)
	case isMuyingPage(root):
		return discoverMuying(ctx, getter, root)
	case isYmatouProduct(root):
		return discoverYmatou(ctx, getter, root)
	case isNeteaseBlogArticle(root):
		return discoverNeteaseBlog(ctx, getter, root)
	case isSoukuanProduct(root):
		return discoverSoukuan(ctx, getter, root)
	case isMokoPage(root):
		return discoverMoko(ctx, getter, root)
	case isXiaomiYoupinProduct(root):
		return discoverXiaomiYoupin(ctx, getter, root)
	case isZcygovProduct(root):
		return discoverZcygov(ctx, getter, root)
	case isNeteasePhotoPage(root):
		return discoverNeteasePhoto(ctx, getter, root)
	case isDribbbleShot(root):
		return discoverDribbble(ctx, getter, root)
	case isFacebookPage(root):
		return discoverFacebook(ctx, getter, root)
	case isTikTokPage(root):
		return discoverTikTok(ctx, getter, root)
	case isMujiProduct(root):
		return discoverMuji(ctx, getter, root)
	case isEhentaiPage(root):
		return discoverEhentai(ctx, getter, root)
	case isGallery121(root):
		return discoverGallery121(ctx, getter, root)
	case isTwitterStatus(root):
		return discoverTwitterStatus(ctx, getter, root)
	case isTwitterProfileMedia(root):
		return Discovery{IsPage: true}, errors.New("X 媒体页可能需要登录，当前尚未支持；请打开作者主页或单个帖子")
	case isTwitterProfile(root):
		return discoverTwitterProfile(ctx, getter, root)
	case isGallery128Page(root):
		return discoverGallery128(ctx, getter, root)
	case isSizuoArticle(root):
		return discoverSizuo(ctx, getter, root)
	case isWellcoPage(root):
		return discoverWellco(ctx, getter, root)
	case isGoogleImageSearch(root):
		return discoverGoogleImages(ctx, getter, root)
	case qzoneIsGroupAlbum(root):
		return Discovery{IsPage: true}, errors.New("QQ 群相册请使用下载浏览器登录后采集")
	default:
		return discoverWebsite(ctx, getter, root, depth)
	}
	if searchErr == nil || len(found.Items) > 0 || ctx.Err() != nil {
		return found, searchErr
	}
	fallback, fallbackErr := discoverWebsite(ctx, getter, root, depth)
	if fallback.Response != nil {
		fallback.Response.Body.Close()
		fallback.Response = nil
	}
	fallback.IsPage = true
	return fallback, errors.Join(searchErr, fallbackErr)
}

func discoverWebsite(ctx context.Context, getter Getter, root *url.URL, depth int) (Discovery, error) {
	var result Discovery
	if depth < 1 {
		depth = 1
	}
	queue := []crawlPage{{url: root.String(), depth: 1}}
	pages := map[string]bool{root.String(): true}
	images := map[string]bool{}
	var origin *url.URL
	var failures []error
	limited := false

	addImage := func(raw, referer string) {
		raw = weiboOriginalURL(referer, raw)
		raw, alternatives := twitterOriginalURLs(referer, raw)
		if images[raw] {
			return
		}
		images[raw] = true
		item := Item{URL: raw, AlternateURLs: alternatives}
		if ref := imageReferer(referer, raw); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		page := queue[0]
		queue = queue[1:]
		doc, final, isImage, response, err := fetchCrawlPage(ctx, getter, page)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if page.depth == 1 {
				return result, err
			}
			label, _ := crawlURL(nil, page.url)
			label.User, label.RawQuery = nil, ""
			failures = append(failures, fmt.Errorf("扫描 %s: %w", label.String(), err))
			continue
		}
		if page.depth == 1 {
			if doc == nil {
				result.Response = response
				return result, nil
			}
			result.IsPage = true
			origin = final
			if googleBlockedPage(root, final, doc) {
				return result, errors.New("Google 要求启用 JavaScript 或完成安全验证，请在下载浏览器中重试")
			}
		} else if doc != nil && !sameOrigin(origin, final) {
			continue
		}
		if doc == nil {
			if isImage {
				addImage(final.String(), page.referer)
			}
			continue
		}
		pages[final.String()] = true
		base := documentBase(doc, final)
		referer := pageReferer(final)
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if result.Title == "" && page.depth == 1 && n.Data == "title" {
				result.Title = nodeText(n)
			}
			for _, candidate := range imageCandidates(n) {
				if u, err := crawlURL(base, candidate); err == nil {
					addImage(u.String(), referer)
				}
			}
			switch n.Data {
			case "a", "link", "area", "frame", "iframe":
			default:
				return
			}
			follow := n.Data != "link" || pageLink(n)
			if n.Data == "frame" || n.Data == "iframe" {
				hasSource := false
				for _, attr := range n.Attr {
					hasSource = hasSource || strings.Contains(attr.Key, "src")
				}
				if !hasSource {
					return
				}
			}
			for _, candidate := range imageURLAttributes(n) {
				if u, err := crawlURL(base, candidate); err == nil {
					addImage(u.String(), referer)
				}
			}
			for _, attr := range n.Attr {
				value := strings.TrimSpace(attr.Val)
				if attr.Key != "href" && attr.Key != "src" &&
					!(strings.Contains(attr.Key, "href") && strings.Contains(value, "/") && !strings.ContainsAny(value, " \t\r\n")) {
					continue
				}
				u, err := crawlURL(base, value)
				if err != nil || u.User != nil {
					continue
				}
				if imagePath(u.Path) {
					addImage(u.String(), referer)
					continue
				}
				if !follow || page.depth >= depth || !sameOrigin(origin, u) || pages[u.String()] {
					continue
				}
				if len(pages) >= maxCrawlPages {
					limited = true
					continue
				}
				pages[u.String()] = true
				queue = append(queue, crawlPage{url: u.String(), referer: referer, depth: page.depth + 1})
			}
		})
	}
	if limited {
		failures = append(failures, fmt.Errorf("网页超过 %d 页的扫描上限", maxCrawlPages))
	}
	return result, errors.Join(failures...)
}

// fetchCrawlPage 只读取网页；二进制直链不会在发现阶段读完整内容。
func fetchCrawlPage(ctx context.Context, getter Getter, page crawlPage) (*html.Node, *url.URL, bool, *Response, error) {
	resp, err := getter.Get(ctx, page.url, func(r *http.Request) {
		r.Header.Set("Accept", "text/html,application/xhtml+xml,image/*,*/*;q=0.8")
		r.Header.Set("Accept-Encoding", "identity")
		if page.referer != "" {
			r.Header.Set("Referer", page.referer)
		}
	})
	if err != nil {
		return nil, nil, false, nil, &TransferError{Err: err}
	}
	body := resp.Body
	retained := false
	defer func() {
		if !retained {
			body.Close()
		}
	}()
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, page.url))
	if err != nil {
		return nil, nil, false, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, final, false, nil, &HTTPStatusError{URL: final.String(), StatusCode: resp.StatusCode,
			RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)
	mediaType = strings.ToLower(mediaType)
	probe := &crawlProbe{reader: body, recording: true}
	var reader io.Reader = probe
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, final, false, nil, &TransferError{Err: err}
		}
		defer gz.Close()
		reader = gz
	case "deflate":
		zr, err := zlib.NewReader(reader)
		if err != nil {
			return nil, final, false, nil, &TransferError{Err: err}
		}
		defer zr.Close()
		reader = zr
	default:
		return nil, final, false, nil, &TransferError{Err: errors.New("不支持网页压缩格式")}
	}
	buffer := bufio.NewReader(reader)
	if mediaType != "text/html" && mediaType != "application/xhtml+xml" {
		prefix, peekErr := buffer.Peek(512)
		if peekErr != nil && peekErr != io.EOF {
			return nil, final, false, nil, &TransferError{Err: peekErr}
		}
		detected := http.DetectContentType(bytes.TrimPrefix(prefix, []byte("\xef\xbb\xbf")))
		if !strings.HasPrefix(detected, "text/html") && !htmlFragment(prefix) {
			if page.depth == 1 {
				resp.Body = struct {
					io.Reader
					io.Closer
				}{io.MultiReader(bytes.NewReader(probe.bytes), body), body}
				retained = true
				return nil, final, strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(detected, "image/"), resp, nil
			}
			return nil, final, strings.HasPrefix(mediaType, "image/") || strings.HasPrefix(detected, "image/"), nil, nil
		}
	}
	probe.recording = false
	content, err := io.ReadAll(io.LimitReader(buffer, maxPageBytes+1))
	if err != nil {
		return nil, final, false, nil, &TransferError{Err: err}
	}
	if len(content) > maxPageBytes {
		return nil, final, false, nil, &LocalError{Err: errors.New("网页超过 8 MiB 的扫描上限")}
	}
	var decoded io.Reader = bytes.NewReader(content)
	_, _, certain := charset.DetermineEncoding(content, contentType)
	if certain || !utf8.Valid(content) {
		decoded, err = charset.NewReader(decoded, contentType)
		if err != nil {
			return nil, final, false, nil, err
		}
	}
	doc, err := html.Parse(decoded)
	if err != nil {
		return nil, final, false, nil, err
	}
	return doc, final, false, nil, nil
}

type crawlProbe struct {
	reader    io.Reader
	bytes     []byte
	recording bool
}

func (p *crawlProbe) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	if p.recording {
		p.bytes = append(p.bytes, b[:n]...)
		if len(p.bytes) > maxPageBytes {
			return n, errors.New("网页探测超过 8 MiB 的扫描上限")
		}
	}
	return n, err
}

func htmlFragment(prefix []byte) bool {
	text := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(string(prefix), "\xef\xbb\xbf")))
	for _, tag := range []string{"<img", "<picture", "<source"} {
		if strings.HasPrefix(text, tag) && len(text) > len(tag) && strings.ContainsAny(text[len(tag):len(tag)+1], " \t\n\r\f/>") {
			return true
		}
	}
	return false
}

func crawlURL(base *url.URL, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("空地址")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("只支持 HTTP / HTTPS 地址")
	}
	u.Fragment = ""
	return u, nil
}

// Some sites use the fragment as the actual page route rather than an anchor.
func crawlInputURL(raw string) (*url.URL, error) {
	u, err := crawlURL(nil, raw)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && isWeishangPage(parsed) {
		u.Fragment, u.RawFragment = parsed.Fragment, parsed.RawFragment
	}
	return u, nil
}

func qzoneIsGroupAlbum(u *url.URL) bool {
	_, _, ok := qzoneGroupAlbumIDs(u)
	return ok
}

func sameOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return port(a) == port(b)
}

func pageReferer(u *url.URL) string {
	copy := *u
	copy.User = nil
	return copy.String()
}

func imageReferer(source, target string) string {
	from, err := crawlURL(nil, source)
	if err != nil {
		return ""
	}
	to, err := crawlURL(nil, target)
	if err != nil || (from.Scheme == "https" && to.Scheme == "http") {
		return ""
	}
	from.User = nil
	if !sameOrigin(from, to) {
		from.Path, from.RawPath, from.RawQuery, from.ForceQuery = "/", "", "", false
	}
	return from.String()
}

func documentBase(doc *html.Node, fallback *url.URL) *url.URL {
	base := fallback
	found := false
	walkHTML(doc, func(n *html.Node) {
		if found || n.Type != html.ElementNode || n.Data != "base" {
			return
		}
		if u, err := crawlURL(fallback, attribute(n, "href")); err == nil {
			base, found = u, true
		}
	})
	return base
}

func walkHTML(root *html.Node, visit func(*html.Node)) {
	for n := root; n != nil; {
		visit(n)
		if n.FirstChild != nil {
			n = n.FirstChild
			continue
		}
		for n != root && n.NextSibling == nil {
			n = n.Parent
		}
		if n == root {
			break
		}
		n = n.NextSibling
	}
}

func nodeText(n *html.Node) string {
	var parts []string
	walkHTML(n, func(child *html.Node) {
		if child.Type == html.TextNode {
			parts = append(parts, child.Data)
		}
	})
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}

func attribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func pageLink(n *html.Node) bool {
	page := false
	for _, rel := range strings.Fields(strings.ToLower(attribute(n, "rel"))) {
		switch rel {
		case "stylesheet", "preload", "modulepreload", "preconnect", "dns-prefetch", "prefetch":
			return false
		case "next", "prev", "alternate":
			page = true
		}
	}
	if value := attribute(n, "type"); value != "" {
		mediaType, _, err := mime.ParseMediaType(value)
		if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
			return false
		}
	}
	return page
}

var cssURL = regexp.MustCompile(`(?i)url\(\s*([^)]*?)\s*\)`)

func imageCandidates(n *html.Node) []string {
	var images []string
	if n.Data == "img" {
		for _, key := range []string{"data-original", "data-origin-src", "data-src", "data-lazyload", "data-lazy-src", "data-img", "data-url", "data-href", "bigimgsrc", "real_src", "lazy-src-load"} {
			if v := attribute(n, key); possibleImage(v) {
				images = append(images, v)
				break
			}
		}
		if len(images) == 0 {
			images = append(images, imageURLAttributes(n)...)
			if v := largestSrcset(firstNonEmpty(attribute(n, "data-srcset"), attribute(n, "srcset"))); v != "" {
				images = append(images, v)
			}
		}
		if len(images) == 0 {
			if v := attribute(n, "src"); v != "" {
				images = append(images, v)
			}
		}
	} else if n.Data == "object" {
		images = append(images, imageURLAttributes(n)...)
	} else if n.Data == "source" && n.Parent != nil && n.Parent.Data == "picture" {
		if v := largestSrcset(attribute(n, "srcset")); v != "" {
			images = append(images, v)
		}
	} else if n.Data == "meta" {
		name := strings.ToLower(firstNonEmpty(attribute(n, "property"), attribute(n, "name")))
		if name == "og:image" || name == "og:image:url" || name == "twitter:image" || name == "twitter:image:src" {
			images = append(images, attribute(n, "content"))
		}
	} else if n.Data == "link" && (attribute(n, "rel") == "image_src" || (attribute(n, "rel") == "preload" && attribute(n, "as") == "image")) {
		images = append(images, attribute(n, "href"))
	} else if n.Data == "script" {
		images = append(images, jsonLDImages(n)...)
	}
	style := attribute(n, "style")
	if strings.Contains(strings.ToLower(style), "background") {
		for _, match := range cssURL.FindAllStringSubmatch(style, -1) {
			images = append(images, strings.Trim(strings.TrimSpace(match[1]), `"'`))
		}
	}
	return images
}

func imageURLAttributes(n *html.Node) []string {
	var images []string
	for _, a := range n.Attr {
		if a.Key == "src" || a.Key == "srcset" || a.Key == "data-srcset" {
			continue
		}
		value := strings.TrimSpace(a.Val)
		if !strings.Contains(value, "/") || !possibleImage(value) {
			continue
		}
		u, err := url.Parse(value)
		if err == nil {
			switch strings.ToLower(path.Ext(u.Path)) {
			case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tif", ".tiff", ".webp":
				images = append(images, value)
			}
		}
	}
	return images
}

// shortcut: JSON-LD 只遍历 64 层；遇到更深的实际页面时再调整上限。
const maxJSONLDDepth = 64

func jsonLDImages(n *html.Node) []string {
	mediaType, _, err := mime.ParseMediaType(attribute(n, "type"))
	if err != nil || !strings.EqualFold(mediaType, "application/ld+json") {
		return nil
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(text.String()))
	decoder.UseNumber()
	var data any
	if decoder.Decode(&data) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil
	}
	var images []string
	var addImage func(any, int)
	addImage = func(value any, depth int) {
		if depth > maxJSONLDDepth {
			return
		}
		switch v := value.(type) {
		case string:
			v = strings.TrimSpace(v)
			if possibleImage(v) {
				images = append(images, v)
			}
		case []any:
			for _, item := range v {
				addImage(item, depth+1)
			}
		case map[string]any:
			before := len(images)
			addImage(v["contentUrl"], depth+1)
			if len(images) == before {
				addImage(v["url"], depth+1)
			}
		}
	}
	var visit func(any, int)
	visit = func(value any, depth int) {
		if depth > maxJSONLDDepth {
			return
		}
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				visit(item, depth+1)
			}
		case map[string]any:
			if jsonLDType(v["@type"], "Product") || jsonLDType(v["@type"], "ProductGroup") {
				addImage(v["image"], depth+1)
			}
			if jsonLDType(v["@type"], "ImageObject") {
				addImage(v, depth)
			}
			keys := make([]string, 0, len(v))
			for key := range v {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				visit(v[key], depth+1)
			}
		}
	}
	visit(data, 0)
	return images
}

func jsonLDType(value any, want string) bool {
	switch v := value.(type) {
	case string:
		if i := strings.LastIndexAny(v, "/#:"); i >= 0 {
			v = v[i+1:]
		}
		return v == want
	case []any:
		for _, item := range v {
			if name, ok := item.(string); ok && jsonLDType(name, want) {
				return true
			}
		}
	}
	return false
}

func largestSrcset(srcset string) string {
	best := ""
	var score float64
	for len(srcset) > 0 {
		srcset = strings.TrimLeft(srcset, " \t\n\r\f,")
		if srcset == "" {
			break
		}
		end := strings.IndexAny(srcset, " \t\n\r\f")
		if end < 0 {
			end = len(srcset)
		}
		raw, rest := srcset[:end], srcset[end:]
		desc := ""
		if strings.HasSuffix(raw, ",") {
			raw = strings.TrimRight(raw, ",")
		} else if comma := strings.IndexByte(rest, ','); comma >= 0 {
			desc, rest = rest[:comma], rest[comma+1:]
		} else {
			desc, rest = rest, ""
		}
		srcset = rest
		fields := strings.Fields(desc)
		if len(fields) > 1 || !possibleImage(raw) {
			continue
		}
		n := 1.0
		if len(fields) == 1 {
			d := fields[0]
			if !strings.HasSuffix(d, "w") && !strings.HasSuffix(d, "x") {
				continue
			}
			var err error
			n, err = strconv.ParseFloat(d[:len(d)-1], 64)
			if err != nil || n <= 0 {
				continue
			}
		}
		if best == "" || n > score {
			best, score = raw, n
		}
	}
	return best
}

func possibleImage(raw string) bool {
	if raw == "" || strings.HasPrefix(raw, "#") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "" || strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

func imagePath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp", ".tif", ".tiff", ".svg", ".heic", ".heif":
		return true
	}
	return false
}
