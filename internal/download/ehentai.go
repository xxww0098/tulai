package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	ehentaiGalleryPath = regexp.MustCompile(`^/g/([1-9][0-9]{0,19})/([0-9a-fA-F]{10})/?$`)
	ehentaiPhotoPath   = regexp.MustCompile(`^/s/([0-9a-fA-F]{10})/([1-9][0-9]{0,19})-([1-9][0-9]{0,6})/?$`)
	ehentaiLength      = regexp.MustCompile(`^([0-9]+)\s+pages?$`)
	ehentaiRetryToken  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

// The reference follows gallery thumbnails to viewer pages. Bounds and a
// modest delay prevent a large gallery from making unbounded site requests.
const (
	maxEhentaiGalleryPages = 100
	maxEhentaiImages       = 1000
)

type ehentaiRef struct {
	kind, gallery, token string
	page                 int
}

func ehentaiRoute(u *url.URL) ehentaiRef {
	if u == nil || len(u.String()) > 8192 || u.User != nil || u.RawPath != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Hostname(), "e-hentai.org") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ehentaiRef{}
	}
	if m := ehentaiGalleryPath.FindStringSubmatch(u.Path); m != nil {
		p := 0
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["p"]) > 1 {
			p = -1
		} else if raw := q.Get("p"); raw != "" {
			p, err = strconv.Atoi(raw)
			if err != nil || p < 0 || p > 1000000 || strconv.Itoa(p) != raw {
				p = -1
			}
		}
		return ehentaiRef{kind: "gallery", gallery: m[1], token: strings.ToLower(m[2]), page: p}
	}
	if m := ehentaiPhotoPath.FindStringSubmatch(u.Path); m != nil {
		p, err := strconv.Atoi(m[3])
		if err == nil {
			return ehentaiRef{kind: "photo", gallery: m[2], token: strings.ToLower(m[1]), page: p}
		}
	}
	return ehentaiRef{}
}

func isEhentaiPage(u *url.URL) bool { return ehentaiRoute(u).kind != "" }

// A user may provide documented site parameters. None is manufactured for
// subsequent page requests or passed to an image host.
func ehentaiSafeQuery(u *url.URL) bool {
	ref := ehentaiRoute(u)
	if ref.kind == "" || ref.page < 0 {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range q {
		if len(values) != 1 || values[0] == "" {
			return false
		}
		switch {
		case ref.kind == "gallery" && key == "p":
		case ref.kind == "gallery" && key == "hc" && values[0] == "1":
		case ref.kind == "gallery" && key == "nw" && (values[0] == "session" || values[0] == "always"):
		case ref.kind == "photo" && key == "nl" && ehentaiRetryToken.MatchString(values[0]):
		default:
			return false
		}
	}
	return true
}

// Linked requests may use the site's pagination and comment-display options;
// do not propagate a warning choice or retry token from a found link.
func ehentaiSafeLinkedQuery(u *url.URL) bool {
	if !ehentaiSafeQuery(u) {
		return false
	}
	q := u.Query()
	for key := range q {
		if ehentaiRoute(u).kind != "gallery" || (key != "p" && key != "hc") {
			return false
		}
	}
	return true
}

func ehentaiSamePage(a, b *url.URL) bool {
	r := ehentaiRoute(a)
	return r.kind != "" && r == ehentaiRoute(b) && sameOrigin(a, b) && ehentaiSafeQuery(a) && ehentaiSafeQuery(b)
}

// Only an actual viewer <img> URL is accepted. Non-standard H@H ports are
// intentionally rejected by the existing public-download safety policy.
func ehentaiImage(page *url.URL, raw string) string {
	if len(raw) > 8192 {
		return ""
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		!strings.HasSuffix(strings.ToLower(u.Hostname()), ".hath.network") ||
		strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "//") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) == 0 || (parts[0] != "h" && parts[0] != "om") ||
		(parts[0] == "h" && (len(parts) < 4 || !strings.HasPrefix(parts[2], "keystamp="))) ||
		(parts[0] == "om" && len(parts) < 7) {
		return ""
	}
	// Recent site scripts may initially place a placeholder source here. Do
	// not request it or manufacture a replacement URL.
	if parts[0] == "om" && len(parts) >= 5 && parts[3] == "x" && parts[4] == "0" {
		return ""
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif":
	default:
		return ""
	}
	return u.String()
}

func ehentaiHasClass(n *html.Node, name string) bool {
	for _, s := range strings.Fields(attribute(n, "class")) {
		if s == name {
			return true
		}
	}
	return false
}

func ehentaiWithin(n *html.Node, id string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if attribute(p, "id") == id {
			return true
		}
	}
	return false
}

func ehentaiVisible(n *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		for _, a := range p.Attr {
			key, val := strings.ToLower(a.Key), strings.ToLower(strings.TrimSpace(a.Val))
			if key == "hidden" || (key == "aria-hidden" && val == "true") {
				return false
			}
			if key == "style" {
				for _, rule := range strings.Split(val, ";") {
					k, v, ok := strings.Cut(rule, ":")
					v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
					if ok && ((strings.TrimSpace(k) == "display" && v == "none") || (strings.TrimSpace(k) == "visibility" && (v == "hidden" || v == "collapse"))) {
						return false
					}
				}
			}
		}
	}
	return true
}

func ehentaiBlocked(doc *html.Node) bool {
	blocked := false
	walkHTML(doc, func(n *html.Node) {
		if blocked || n.Type != html.ElementNode || !ehentaiVisible(n) {
			return
		}
		if n.Data == "input" && strings.EqualFold(attribute(n, "type"), "password") {
			blocked = true
			return
		}
		if n.Data == "h1" || n.Data == "h2" || n.Data == "h3" {
			if strings.EqualFold(strings.TrimSpace(nodeText(n)), "Content Warning") {
				blocked = true
			}
		}
	})
	return blocked
}

func ehentaiPhoto(page *url.URL, doc *html.Node) (Item, error) {
	ref := ehentaiRoute(page)
	if ref.kind != "photo" || page.Scheme != "https" || !ehentaiSafeQuery(page) || doc == nil {
		return Item{}, errors.New("E-Hentai 单图页面无效")
	}
	var image string
	count := 0
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" && attribute(n, "id") == "img" && ehentaiWithin(n, "i3") && ehentaiVisible(n) {
			count++
			image = ehentaiImage(page, attribute(n, "src"))
		}
	})
	if count != 1 || image == "" {
		return Item{}, errors.New("E-Hentai 图片未加载或图片地址无法确认")
	}
	item := Item{URL: image, Filename: fmt.Sprintf("ehentai-%s-%06d", ref.gallery, ref.page), RequireImage: true, PublicOnly: true,
		Metadata: map[string]string{"ehentai_gallery_id": ref.gallery, "ehentai_page": strconv.Itoa(ref.page), "image_kind": "displayed", "album_id": ref.gallery, "album_title": "E-Hentai " + ref.gallery}}
	if referer := imageReferer(page.String(), image); referer != "" {
		item.Headers = map[string]string{"Referer": referer}
	}
	return item, nil
}

func discoverEhentai(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isEhentaiPage(page) || !ehentaiSafeQuery(page) {
		return result, errors.New("E-Hentai 页面地址无效或包含未确认的参数")
	}
	if page.Scheme == "http" {
		upgraded := *page
		upgraded.Scheme, upgraded.Host = "https", "e-hentai.org"
		page = &upgraded
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, fmt.Errorf("读取 E-Hentai 页面失败: %w", err)
	}
	if doc == nil || !ehentaiSamePage(page, final) {
		return result, errors.New("E-Hentai 页面已跳转，可能需要登录或验证")
	}
	return ehentaiCollect(ctx, getter, final, doc)
}

// Return the site-provided viewer URLs and the next gallery page only. Every
// linked page must remain on the same HTTPS origin and in the same gallery.
func ehentaiGalleryLinks(page *url.URL, doc *html.Node) (photos []*url.URL, next *url.URL, expected int, incomplete bool) {
	ref := ehentaiRoute(page)
	grids, navs := 0, 0
	invalidNext := false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if attribute(n, "id") == "gdt" {
			grids++
		}
		if n.Data == "table" && ehentaiHasClass(n, "ptt") {
			navs++
		}
		if n.Data == "td" && ehentaiHasClass(n, "gdt1") && strings.TrimSpace(nodeText(n)) == "Length:" {
			for p := n.NextSibling; p != nil; p = p.NextSibling {
				if p.Type != html.ElementNode {
					continue
				}
				if p.Data == "td" && ehentaiHasClass(p, "gdt2") {
					m := ehentaiLength.FindStringSubmatch(strings.ReplaceAll(strings.TrimSpace(nodeText(p)), ",", ""))
					if m != nil {
						expected, _ = strconv.Atoi(m[1])
					}
				}
				break
			}
		}
		if n.Data != "a" || !ehentaiVisible(n) {
			return
		}
		if ehentaiWithin(n, "gdt") {
			u, err := crawlURL(page, attribute(n, "href"))
			r := ehentaiRoute(u)
			if err != nil || !sameOrigin(page, u) || r.kind != "photo" || r.gallery != ref.gallery || !ehentaiSafeLinkedQuery(u) {
				incomplete = true
				return
			}
			photos = append(photos, u)
			return
		}
		inNav := false
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Data == "table" && ehentaiHasClass(p, "ptt") {
				inNav = true
				break
			}
		}
		if !inNav {
			return
		}
		label := strings.TrimSpace(nodeText(n))
		if label != ">" && label != "›" && !strings.EqualFold(label, "next") {
			return
		}
		raw := attribute(n, "href")
		if raw == "" || raw == "#" {
			return
		}
		u, err := crawlURL(page, raw)
		r := ehentaiRoute(u)
		if err != nil || !sameOrigin(page, u) || !ehentaiSafeLinkedQuery(u) || r.kind != "gallery" || r.gallery != ref.gallery || r.token != ref.token || r.page != ref.page+1 {
			invalidNext = true
			return
		}
		if next != nil && !ehentaiSamePage(next, u) {
			invalidNext = true
			return
		}
		next = u
	})
	if grids != 1 {
		return nil, nil, expected, true
	}
	if navs > 2 || invalidNext {
		incomplete = true
	}
	if navs == 0 && expected == 0 {
		incomplete = true
	}
	sort.SliceStable(photos, func(i, j int) bool { return ehentaiRoute(photos[i]).page < ehentaiRoute(photos[j]).page })
	return photos, next, expected, incomplete
}

func ehentaiCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := ehentaiRoute(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || ref.kind == "" || page.Scheme != "https" || !ehentaiSafeQuery(page) || doc == nil {
		return result, errors.New("E-Hentai 页面或下载客户端无效")
	}
	if ehentaiBlocked(doc) {
		return result, errors.New("E-Hentai 页面需要登录或显示访问提示")
	}
	result.Title = "E-Hentai " + ref.gallery
	qualityWarning := errors.New("E-Hentai 图片使用页面提供的显示版本，原始尺寸未确认")
	if ref.kind == "photo" {
		item, err := ehentaiPhoto(page, doc)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
		return result, qualityWarning
	}
	seen := map[int]string{}
	var expected int
	var partial error
	if ref.page > 0 {
		partial = errors.Join(partial, errors.New("E-Hentai 从非首页开始，结果可能不完整"))
	}
	requests := 0
	for pages := 0; pages < maxEhentaiGalleryPages; pages++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(partial, err)
		}
		if ehentaiBlocked(doc) {
			return result, errors.Join(partial, errors.New("E-Hentai 页面需要登录或显示访问提示"))
		}
		photos, next, count, incomplete := ehentaiGalleryLinks(page, doc)
		if incomplete {
			partial = errors.Join(partial, errors.New("E-Hentai 页面链接或分页无法确认，结果可能不完整"))
		}
		if count > 0 {
			if expected != 0 && expected != count {
				partial = errors.Join(partial, errors.New("E-Hentai 图片总数发生变化，结果可能不完整"))
			}
			expected = count
		}
		if len(photos) == 0 {
			return result, errors.Join(partial, errors.New("E-Hentai 未找到画廊图片，可能需要登录或验证"))
		}
		for _, photo := range photos {
			p := ehentaiRoute(photo)
			if old, ok := seen[p.page]; ok {
				if old != p.token {
					partial = errors.Join(partial, errors.New("E-Hentai 图片标识冲突，结果可能不完整"))
				}
				continue
			}
			if len(seen) >= maxEhentaiImages {
				return result, errors.Join(partial, errors.New("E-Hentai 达到图片数量上限，结果不完整"))
			}
			seen[p.page] = p.token
			if requests > 0 {
				if err := Wait(ctx, time.Second); err != nil {
					return result, errors.Join(partial, err)
				}
			}
			requests++
			viewer, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: photo.String(), referer: page.String(), depth: 2})
			if response != nil {
				response.Body.Close()
			}
			if err != nil {
				return result, errors.Join(partial, fmt.Errorf("读取 E-Hentai 图片页面失败: %w", err))
			}
			if viewer == nil || !ehentaiSamePage(photo, final) || ehentaiBlocked(viewer) {
				return result, errors.Join(partial, errors.New("E-Hentai 图片页面已跳转或需要验证"))
			}
			item, err := ehentaiPhoto(final, viewer)
			if err != nil {
				return result, errors.Join(partial, err)
			}
			result.Items = append(result.Items, item)
		}
		if next == nil {
			complete := expected > 0 && len(seen) == expected
			if complete {
				for i := 1; i <= expected; i++ {
					if _, ok := seen[i]; !ok {
						complete = false
						break
					}
				}
			}
			if !complete {
				partial = errors.Join(partial, errors.New("E-Hentai 无法确认图片总数，结果可能不完整"))
			}
			return result, errors.Join(partial, qualityWarning)
		}
		if pages+1 == maxEhentaiGalleryPages {
			return result, errors.Join(partial, errors.New("E-Hentai 达到分页上限，结果不完整"))
		}
		if err := Wait(ctx, time.Second); err != nil {
			return result, errors.Join(partial, err)
		}
		paged, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: next.String(), referer: page.String(), depth: 2})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			return result, errors.Join(partial, fmt.Errorf("读取 E-Hentai 下一页失败: %w", err))
		}
		if paged == nil || !ehentaiSamePage(next, final) {
			return result, errors.Join(partial, errors.New("E-Hentai 下一页已跳转或需要验证"))
		}
		page, doc = final, paged
	}
	return result, errors.Join(partial, errors.New("E-Hentai 分页未完成"))
}
