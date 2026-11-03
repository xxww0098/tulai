package download

import (
	"context"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

var unsplashID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

const (
	maxUnsplashPhotos  = 50
	maxUnsplashScrolls = 8
)

type unsplashRoute struct {
	photo  string
	search bool
}

// The reference handles individual photos and /s/photos/ searches.
func unsplashRouteOf(page *url.URL) unsplashRoute {
	if page == nil || page.User != nil || (page.Scheme != "http" && page.Scheme != "https") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) {
		return unsplashRoute{}
	}
	switch strings.ToLower(page.Hostname()) {
	case "unsplash.com", "www.unsplash.com":
	default:
		return unsplashRoute{}
	}
	if strings.Contains(strings.ToLower(page.EscapedPath()), "%2f") || strings.Contains(page.Path, "..") || strings.Contains(page.Path, "\\") {
		return unsplashRoute{}
	}
	if strings.HasPrefix(page.Path, "/s/photos/") {
		term := strings.TrimPrefix(page.Path, "/s/photos/")
		if term != "" && len(term) <= 512 && !strings.Contains(term, "/") {
			return unsplashRoute{search: true}
		}
	}
	if !strings.HasPrefix(page.Path, "/photos/") {
		return unsplashRoute{}
	}
	part := strings.TrimPrefix(page.Path, "/photos/")
	if len(part) < 11 || len(part) > 512 || strings.Contains(part, "/") {
		return unsplashRoute{}
	}
	id := part[len(part)-11:]
	if !unsplashID.MatchString(id) || (len(part) > 11 && part[len(part)-12] != '-') {
		return unsplashRoute{}
	}
	return unsplashRoute{photo: id}
}

func isUnsplashPage(page *url.URL) bool {
	ref := unsplashRouteOf(page)
	return ref.photo != "" || ref.search
}

func unsplashDownloadURL(page *url.URL, raw, id string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || strings.Contains(parsed.Path, "..") || strings.Contains(strings.ToLower(parsed.EscapedPath()), "%2f") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !sameOrigin(page, u) || u.Path != "/photos/"+id+"/download" || u.Scheme != "https" {
		return "", false
	}
	return u.String(), true
}

func unsplashDownloadButton(n *html.Node) bool {
	const marker = "non-sponsored-photo-download-button"
	return n.Type == html.ElementNode && n.Data == "a" &&
		(attribute(n, "data-test") == marker || attribute(n, "data-testid") == marker)
}

func unsplashDownloadItem(page *url.URL, raw, id string) (Item, bool) {
	download, ok := unsplashDownloadURL(page, raw, id)
	if !ok {
		return Item{}, false
	}
	return Item{URL: download, RequireImage: true,
		Headers:  map[string]string{"Referer": page.Scheme + "://" + page.Host + "/"},
		Metadata: map[string]string{"unsplash_photo_id": id}}, true
}

func unsplashPhotoPage(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := unsplashRouteOf(page).photo
	if id == "" || doc == nil {
		return result, errors.New("Unsplash 图片页面无效")
	}
	invalid := false
	var fallbackTitle string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:title") && fallbackTitle == "" {
			fallbackTitle = attribute(n, "content")
		}
		if !unsplashDownloadButton(n) {
			return
		}
		item, ok := unsplashDownloadItem(page, attribute(n, "href"), id)
		if !ok {
			invalid = true
			return
		}
		if len(result.Items) == 0 {
			result.Items = append(result.Items, item)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Title = firstNonEmpty(result.Title, fallbackTitle)
	result.Title = strings.TrimSpace(strings.TrimSuffix(result.Title, "– Free Fashion Image on Unsplash"))
	if len(result.Items) == 0 {
		return result, errors.New("未找到 Unsplash 公开下载按钮，可能需要登录、订阅或安全验证")
	}
	if invalid {
		return result, errors.New("Unsplash 页面包含不匹配的下载地址，结果可能不完整")
	}
	return result, nil
}

// unsplashSearchLinks collects the reference's /photos/ links without following
// links to unrelated origins or retaining tracking parameters from cards.
func unsplashSearchLinks(page *url.URL, doc *html.Node, seen map[string]bool, links []*url.URL) ([]*url.URL, bool) {
	limited := false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		raw := attribute(n, "href")
		if raw == "" || len(raw) > 8192 {
			return
		}
		u, err := crawlURL(page, raw)
		if err != nil || u.User != nil || !sameOrigin(page, u) {
			return
		}
		id := unsplashRouteOf(u).photo
		if id == "" || seen[id] {
			return
		}
		if len(links) >= maxUnsplashPhotos {
			limited = true
			return
		}
		seen[id] = true
		u.RawQuery, u.ForceQuery = "", false
		links = append(links, u)
	})
	return links, limited
}

// Current search cards expose the same official button as each photo page.
// Accept a button only for a photo link already found in this search.
func unsplashSearchButtons(page *url.URL, doc *html.Node, seen map[string]bool, buttons map[string]Item) {
	walkHTML(doc, func(n *html.Node) {
		if !unsplashDownloadButton(n) {
			return
		}
		raw := attribute(n, "href")
		if len(raw) == 0 || len(raw) > 8192 {
			return
		}
		u, err := crawlURL(page, raw)
		if err != nil || u.User != nil || !sameOrigin(page, u) {
			return
		}
		parts := strings.Split(u.Path, "/")
		if len(parts) != 4 || parts[1] != "photos" || parts[3] != "download" || !unsplashID.MatchString(parts[2]) || !seen[parts[2]] {
			return
		}
		if item, ok := unsplashDownloadItem(page, raw, parts[2]); ok {
			buttons[parts[2]] = item
		}
	})
}

func discoverUnsplash(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := unsplashRouteOf(page)
	if getter == nil || (ref.photo == "" && !ref.search) {
		return result, errors.New("Unsplash 页面或下载客户端无效")
	}
	target := *page
	target.Scheme = "https"
	if strings.EqualFold(page.Hostname(), "www.unsplash.com") {
		target.Host = "unsplash.com"
	} else if page.Port() == "80" {
		target.Host = page.Hostname()
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: target.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("读取 Unsplash 页面失败，可能需要安全验证：%w", err)
	}
	if doc == nil || !sameOrigin(&target, final) || unsplashRouteOf(final) != ref ||
		(ref.search && (final.Path != target.Path || final.RawQuery != target.RawQuery)) {
		return result, errors.New("Unsplash 页面已跳转，可能需要登录或安全验证")
	}
	return unsplashCollect(ctx, getter, final, doc)
}

// unsplashCollect handles fetched HTML and a rendered document. Search results
// are limited to links already present in the supplied document.
func unsplashCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := unsplashRouteOf(page)
	if getter == nil || doc == nil || (ref.photo == "" && !ref.search) {
		return result, errors.New("Unsplash 页面或下载客户端无效")
	}
	if ref.photo != "" {
		return unsplashPhotoPage(ctx, page, doc)
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
	})
	seen := map[string]bool{}
	links, limited := unsplashSearchLinks(page, doc, seen, nil)
	buttons := map[string]Item{}
	unsplashSearchButtons(page, doc, seen, buttons)
	var failures int
	for _, photo := range links {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if item, ok := buttons[unsplashRouteOf(photo).photo]; ok {
			result.Items = append(result.Items, item)
			continue
		}
		childDoc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: photo.String(), referer: page.Scheme + "://" + page.Host + "/", depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil || childDoc == nil || !sameOrigin(page, final) || unsplashRouteOf(final).photo != unsplashRouteOf(photo).photo {
			failures++
			continue
		}
		one, err := unsplashPhotoPage(ctx, final, childDoc)
		result.Items = append(result.Items, one.Items...)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil {
			failures++
		}
	}
	var issues []error
	if limited {
		issues = append(issues, fmt.Errorf("Unsplash 搜索超过 %d 张图片上限", maxUnsplashPhotos))
	}
	if failures != 0 {
		issues = append(issues, fmt.Errorf("%d 张 Unsplash 图片未能读取下载按钮，可能需要登录、订阅或验证", failures))
	}
	if len(links) == 0 {
		issues = append(issues, errors.New("未找到 Unsplash 搜索结果，页面可能尚未加载或需要安全验证"))
	}
	issues = append(issues, errors.New("Unsplash 搜索仅包含已加载的结果，可能不完整"))
	return result, errors.Join(issues...)
}

type unsplashBrowserSnapshot struct {
	URL      string `json:"url"`
	HTML     string `json:"html"`
	TooLarge bool   `json:"tooLarge"`
}

const unsplashBrowserSnapshotScript = `(() => {
  const text = document.documentElement ? document.documentElement.outerHTML : "";
  if (text.length > 8388608) return {url:location.href,tooLarge:true};
  return {url:location.href,html:text};
})()`

// captureUnsplashSearch uses a separate browser tab so loading more results and
// opening photo pages does not navigate or scroll the user's current tab.
func (s *RenderedSession) captureUnsplashSearch(ctx context.Context, page *url.URL, observedURL string, initialDoc *html.Node) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	if !unsplashRouteOf(page).search {
		return result, false, nil
	}
	if err := ctx.Err(); err != nil {
		return result, true, err
	}
	if initialDoc == nil {
		return result, true, errors.New("Unsplash 搜索页面未加载")
	}
	checkCurrent := func() error {
		var raw string
		if err := s.run(ctx, chromedp.Location(&raw)); err != nil {
			return err
		}
		if raw != observedURL {
			return errors.New("采集期间 Unsplash 搜索页面已更改")
		}
		return nil
	}
	if err := checkCurrent(); err != nil {
		return result, true, err
	}
	walkHTML(initialDoc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
	})
	seen := map[string]bool{}
	links, limited := unsplashSearchLinks(page, initialDoc, seen, nil)
	buttons := map[string]Item{}
	unsplashSearchButtons(page, initialDoc, seen, buttons)
	child, closeChild, err := camoufox.NewPage(ctx, s.tab, false)
	if err != nil {
		return result, true, err
	}
	defer closeChild()
	run := func(ctx context.Context, actions ...camoufox.Action) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		op, cancel := context.WithTimeout(child, 30*time.Second)
		stop := context.AfterFunc(ctx, cancel)
		defer func() { stop(); cancel() }()
		err := camoufox.Run(op, actions...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	var expected atomic.Value
	expected.Store("")
	camoufox.ListenTarget(child, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		u, err := crawlURL(nil, paused.Request.URL)
		allowed := err == nil && u.User == nil && sameOrigin(page, u)
		want := expected.Load().(string)
		if allowed && want == "" {
			allowed = unsplashRouteOf(u).search && u.Path == page.Path && u.RawQuery == page.RawQuery
		} else if allowed {
			allowed = unsplashRouteOf(u).photo == want
		}
		go func() {
			var action chromedp.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if allowed {
				action = fetch.ContinueRequest(paused.RequestID)
			}
			_ = camoufox.Run(child, action)
		}()
	})
	stopInit := context.AfterFunc(ctx, closeChild)
	timer := time.AfterFunc(30*time.Second, closeChild)
	initErr := camoufox.Run(child, fetch.Enable().WithPatterns([]*fetch.RequestPattern{{ResourceType: network.ResourceTypeDocument}}))
	stopInit()
	timer.Stop()
	if initErr != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("无法创建 Unsplash 搜索采集标签")
	}
	if err := run(ctx, camoufox.Navigate(page.String())); err != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		// The existing document may contain useful results even if a new tab is blocked.
		found, partial := unsplashCollect(ctx, s.Getter(), page, initialDoc)
		return found, true, errors.Join(errors.New("Unsplash 搜索页面未能在采集标签中加载"), partial)
	}
	read := func() (unsplashBrowserSnapshot, *html.Node, error) {
		var snapshot unsplashBrowserSnapshot
		if err := run(ctx, chromedp.Evaluate(unsplashBrowserSnapshotScript, &snapshot)); err != nil {
			return snapshot, nil, err
		}
		if snapshot.TooLarge || len(snapshot.HTML) > maxPageBytes {
			return snapshot, nil, errors.New("Unsplash 页面超过 8 MiB 上限")
		}
		doc, err := html.Parse(strings.NewReader(snapshot.HTML))
		return snapshot, doc, err
	}
	var issues []error
	issues = append(issues, errors.New("Unsplash 搜索最多采集 50 张照片并滚动 8 屏，结果可能不完整"))
	for step := 0; step <= maxUnsplashScrolls; step++ {
		if err := checkCurrent(); err != nil {
			return result, true, errors.Join(err, errors.Join(issues...))
		}
		snapshot, doc, err := read()
		if currentErr := checkCurrent(); currentErr != nil {
			return result, true, errors.Join(currentErr, errors.Join(issues...))
		}
		current, parseErr := crawlURL(nil, snapshot.URL)
		if err != nil || parseErr != nil || !sameOrigin(page, current) || !unsplashRouteOf(current).search || current.Path != page.Path || current.RawQuery != page.RawQuery {
			issues = append(issues, errors.New("Unsplash 搜索页面无法读取或已跳转"))
			break
		}
		var hitLimit bool
		links, hitLimit = unsplashSearchLinks(page, doc, seen, links)
		unsplashSearchButtons(page, doc, seen, buttons)
		limited = limited || hitLimit
		if step == maxUnsplashScrolls || len(links) >= maxUnsplashPhotos {
			break
		}
		// This is the reference's visible Load more button. Scroll as well for
		// pages that append results when the sentinel enters the viewport.
		if err := run(ctx, chromedp.Evaluate(`(() => {
  const button = Array.from(document.querySelectorAll("button")).find(x => x.textContent.trim() === "Load more" && !x.disabled);
  if (button) button.click();
  window.scrollBy({top:window.innerHeight,behavior:"instant"});
})()`, nil)); err != nil {
			return result, true, err
		}
		if err := Wait(ctx, 400*time.Millisecond); err != nil {
			return result, true, err
		}
	}
	if limited {
		issues = append(issues, fmt.Errorf("Unsplash 搜索超过 %d 张图片上限", maxUnsplashPhotos))
	}
	if len(links) == 0 {
		issues = append(issues, errors.New("未找到 Unsplash 搜索结果，页面可能尚未加载或需要安全验证"))
	}
	failures := 0
	for _, photo := range links {
		if err := ctx.Err(); err != nil {
			return result, true, err
		}
		if err := checkCurrent(); err != nil {
			return result, true, errors.Join(err, errors.Join(issues...))
		}
		id := unsplashRouteOf(photo).photo
		if item, ok := buttons[id]; ok {
			result.Items = append(result.Items, item)
			continue
		}
		expected.Store(id)
		if err := run(ctx, camoufox.Navigate(photo.String())); err != nil {
			if ctx.Err() != nil {
				return result, true, ctx.Err()
			}
			failures++
			continue
		}
		snapshot, doc, err := read()
		if currentErr := checkCurrent(); currentErr != nil {
			return result, true, errors.Join(currentErr, errors.Join(issues...))
		}
		current, parseErr := crawlURL(nil, snapshot.URL)
		if err != nil || parseErr != nil || !sameOrigin(page, current) || unsplashRouteOf(current).photo != id {
			failures++
			continue
		}
		one, err := unsplashPhotoPage(ctx, current, doc)
		result.Items = append(result.Items, one.Items...)
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		if err != nil {
			failures++
		}
	}
	if failures != 0 {
		issues = append(issues, fmt.Errorf("%d 张 Unsplash 图片未能读取下载按钮，可能需要登录、订阅或验证", failures))
	}
	return result, true, errors.Join(issues...)
}
