package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/html"
)

var (
	gallery121Path  = regexp.MustCompile(`^/(g|gallery)/([1-9][0-9]{0,19})(?:/([1-9][0-9]{0,5}))?/?$`)
	gallery121Asset = regexp.MustCompile(`^/([0-9]{1,20})/([1-9][0-9]{0,19})/([1-9][0-9]{0,5})\.(?:jpg|jpeg|png|webp)$`)
	gallery121Num   = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
)

type gallery121Address struct {
	site   string
	id     string
	page   int
	reader bool
}

func gallery121AddressOf(u *url.URL) (gallery121Address, bool) {
	var result gallery121Address
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") ||
		path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return result, false
	}
	switch strings.ToLower(u.Hostname()) {
	case "asmhentai.com":
		result.site = "asm"
	case "hentaifox.com":
		result.site = "fox"
	default:
		return result, false
	}
	m := gallery121Path.FindStringSubmatch(u.Path)
	if len(m) != 4 {
		return gallery121Address{}, false
	}
	result.id = m[2]
	if (result.site == "asm" && m[1] == "g") || (result.site == "fox" && m[1] == "gallery") {
		if m[3] != "" {
			return gallery121Address{}, false
		}
		return result, true
	}
	if m[3] == "" {
		return gallery121Address{}, false
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n < 1 {
		return gallery121Address{}, false
	}
	result.page, result.reader = n, true
	return result, true
}

func isGallery121(u *url.URL) bool { _, ok := gallery121AddressOf(u); return ok }
func gallery121Key(u *url.URL) string {
	info, ok := gallery121AddressOf(u)
	if !ok {
		return ""
	}
	return info.site + ":" + info.id
}
func gallery121ReaderURL(page *url.URL, info gallery121Address, index int) *url.URL {
	u := *page
	if info.site == "asm" {
		u.Path = fmt.Sprintf("/gallery/%s/%d/", info.id, index)
	} else {
		u.Path = fmt.Sprintf("/g/%s/%d/", info.id, index)
	}
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return &u
}
func gallery121Same(a, b *url.URL) bool {
	x, okX := gallery121AddressOf(a)
	y, okY := gallery121AddressOf(b)
	return okX && okY && x == y && !(a.Scheme == "https" && b.Scheme != "https")
}
func gallery121Guard(getter Getter, expected *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !gallery121Same(expected, req.URL) {
			return errors.New("图片集页面跳转到其他地址")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("图片集页面重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func gallery121Fetch(ctx context.Context, getter Getter, page *url.URL) (*html.Node, *url.URL, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, gallery121Guard(getter, page), crawlPage{url: page.String(), depth: 1})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || final == nil || !gallery121Same(page, final) {
		return nil, nil, errors.New("图片集页面已跳转或不可访问")
	}
	return doc, final, nil
}
func discoverGallery121(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !isGallery121(root) || getter == nil {
		return result, errors.New("图片集地址或下载客户端无效")
	}
	doc, final, err := gallery121Fetch(ctx, getter, root)
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return result, fmt.Errorf("图片集页面读取失败：HTTP %d，可能需要登录或验证", status.StatusCode)
		}
		return result, errors.New("图片集页面读取失败或跳转，可能需要登录或验证")
	}
	return gallery121Collect(ctx, getter, final, doc)
}
func gallery121HasClass(n *html.Node, name string) bool {
	for _, class := range strings.Fields(attribute(n, "class")) {
		if class == name {
			return true
		}
	}
	return false
}
func gallery121Link(page *url.URL, raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil {
		return nil, false
	}
	u = page.ResolveReference(u)
	if !sameOrigin(page, u) || !isGallery121(u) {
		return nil, false
	}
	return u, true
}
func gallery121GalleryHasFirst(page *url.URL, info gallery121Address, doc *html.Node) bool {
	found := false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		if u, ok := gallery121Link(page, attribute(n, "href")); ok {
			p, _ := gallery121AddressOf(u)
			if p.site == info.site && p.id == info.id && p.reader && p.page == 1 {
				found = true
			}
		}
	})
	return found
}

type gallery121Reader struct {
	image, internalID string
	pages             int
}

func gallery121Image(page *url.URL, info gallery121Address, internalID, raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	u = page.ResolveReference(u)
	host := strings.ToLower(u.Hostname())
	trusted := (info.site == "asm" && host == "images.asmhentai.com") ||
		(info.site == "fox" && (host == "i.hentaifox.com" || host == "i2.hentaifox.com" || host == "i3.hentaifox.com"))
	if u.Scheme != "https" || !trusted || (u.Port() != "" && u.Port() != "443") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || u.Fragment != "" {
		return "", false
	}
	m := gallery121Asset.FindStringSubmatch(u.Path)
	if len(m) != 4 || m[2] != internalID || m[3] != strconv.Itoa(info.page) {
		return "", false
	}
	return u.String(), true
}
func gallery121ParseReader(page *url.URL, doc *html.Node) (gallery121Reader, error) {
	var result gallery121Reader
	info, ok := gallery121AddressOf(page)
	if !ok || !info.reader || doc == nil {
		return result, errors.New("图片集页面无效")
	}
	var pageValue, internalID string
	var bad, back bool
	var media []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "input" {
			switch attribute(n, "id") {
			case "pages":
				v := attribute(n, "value")
				if pageValue != "" && pageValue != v {
					bad = true
				}
				pageValue = v
			case "gallery_id":
				v := attribute(n, "value")
				if internalID != "" && internalID != v {
					bad = true
				}
				internalID = v
			}
		}
		if n.Data == "a" {
			if u, ok := gallery121Link(page, attribute(n, "href")); ok {
				p, _ := gallery121AddressOf(u)
				if p.site == info.site && p.id == info.id && !p.reader {
					back = true
				}
			}
		}
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			if u, ok := gallery121Link(page, attribute(n, "href")); !ok || !gallery121Same(page, u) {
				bad = true
			}
		}
		imageID := "fimg"
		if info.site == "fox" {
			imageID = "gimg"
		}
		if n.Data == "img" && attribute(n, "id") == imageID && gallery121HasClass(n, fmt.Sprintf("image_%d", info.page)) {
			media = append(media, n)
		}
	})
	pages, err := strconv.Atoi(pageValue)
	if bad || !back || !gallery121Num.MatchString(internalID) || err != nil || pages < info.page || pages > 100000 || len(media) != 1 || (info.site == "asm" && info.id != internalID) {
		return result, errors.New("图片集页码或作品身份无法确认")
	}
	var image string
	for _, raw := range []string{attribute(media[0], "data-src"), attribute(media[0], "src")} {
		if raw == "" {
			continue
		}
		if candidate, valid := gallery121Image(page, info, internalID, raw); valid {
			if image != "" && image != candidate {
				return result, errors.New("图片集页面包含不同图片地址")
			}
			image = candidate
		}
	}
	if image == "" {
		return result, errors.New("图片集页面未提供可确认的图片")
	}
	return gallery121Reader{image: image, internalID: internalID, pages: pages}, nil
}

// The two services expose distinct gallery and reader paths. Only the image
// URL actually present on each reader page is used; no image URL is invented.
func gallery121Collect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	info, ok := gallery121AddressOf(page)
	if !ok || getter == nil || doc == nil {
		return result, errors.New("图片集页面无效")
	}
	result.Title = "图片集 " + info.id
	firstPage, firstDoc := page, doc
	if !info.reader {
		if !gallery121GalleryHasFirst(page, info, doc) {
			return result, errors.New("图片集入口未提供可确认的第一页，可能需要登录或验证")
		}
		var err error
		firstDoc, firstPage, err = gallery121Fetch(ctx, getter, gallery121ReaderURL(page, info, 1))
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, errors.New("图片集第一页不可访问，可能需要登录或验证")
		}
	}
	firstInfo, _ := gallery121AddressOf(firstPage)
	first, err := gallery121ParseReader(firstPage, firstDoc)
	if err != nil {
		return result, err
	}
	limit := first.pages
	if limit > maxCrawlPages {
		limit = maxCrawlPages
	}
	type parsedPage struct {
		reader gallery121Reader
		err    error
	}
	pages := make([]parsedPage, limit+1)
	if firstInfo.page <= limit {
		pages[firstInfo.page] = parsedPage{reader: first}
	}
	jobs := make(chan int, limit)
	for n := 1; n <= limit; n++ {
		if n != firstInfo.page {
			jobs <- n
		}
	}
	close(jobs)
	var wg sync.WaitGroup
	var stop atomic.Bool
	var blocked atomic.Int32
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil || stop.Load() {
					return
				}
				d, p, err := gallery121Fetch(ctx, getter, gallery121ReaderURL(firstPage, info, index))
				if err != nil {
					pages[index].err = err
					var status *HTTPStatusError
					if errors.As(err, &status) && (status.StatusCode == http.StatusForbidden || status.StatusCode == http.StatusTooManyRequests) {
						blocked.CompareAndSwap(0, int32(status.StatusCode))
						stop.Store(true)
					}
					continue
				}
				parsed, err := gallery121ParseReader(p, d)
				if err == nil && (parsed.internalID != first.internalID || parsed.pages != first.pages) {
					err = errors.New("图片集分页身份或总数变化")
				}
				pages[index] = parsedPage{reader: parsed, err: err}
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	add := func(index int, reader gallery121Reader) {
		if reader.image == "" || seen[reader.image] {
			return
		}
		seen[reader.image] = true
		item := Item{URL: reader.image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"gallery_site": info.site, "gallery_id": info.id, "gallery_page": strconv.Itoa(index)}}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", reader.image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	missing := 0
	for i := 1; i <= limit; i++ {
		if pages[i].err != nil || pages[i].reader.image == "" {
			missing++
			continue
		}
		add(i, pages[i].reader)
	}
	if firstInfo.page > limit {
		add(firstInfo.page, first)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if status := blocked.Load(); status != 0 {
		return result, fmt.Errorf("图片集已保留 %d 张；遇到 HTTP %d，已停止后续分页，%d 页未能确认", len(result.Items), status, missing)
	}
	if missing > 0 || first.pages > limit {
		return result, fmt.Errorf("图片集已保留 %d 张；%d 页未能确认，最多读取前 %d 页（总页数 %d）", len(result.Items), missing, limit, first.pages)
	}
	return result, nil
}
