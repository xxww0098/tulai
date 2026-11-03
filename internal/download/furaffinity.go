package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	faPhotoPath   = regexp.MustCompile(`^/view/([0-9]{1,20})/?$`)
	faGalleryPath = regexp.MustCompile(`^/gallery/([A-Za-z0-9_-]{1,64})(?:/([0-9]{1,9}))?/?$`)
	faUserPath    = regexp.MustCompile(`^/user/([A-Za-z0-9_-]{1,64})/?$`)
	faPageNumber  = regexp.MustCompile(`^Page\s*#([0-9]{1,9})$`)
	faDigits      = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// shortcut: visiting each work is necessary to verify its actual Download link.
// Bound both traversals and report when the gallery has more work to inspect.
const (
	faMaxGalleryPages = 100
	faMaxWorks        = 1000
)

type faRef struct {
	kind, id, author string
	page             int
}

func faPageRef(u *url.URL) faRef {
	if u == nil || u.User != nil || !strings.EqualFold(u.Hostname(), "www.furaffinity.net") ||
		!((u.Scheme == "https" && (u.Port() == "" || u.Port() == "443")) ||
			(u.Scheme == "http" && (u.Port() == "" || u.Port() == "80"))) || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return faRef{}
	}
	if m := faPhotoPath.FindStringSubmatch(u.Path); m != nil {
		return faRef{kind: "photo", id: m[1]}
	}
	if m := faGalleryPath.FindStringSubmatch(u.Path); m != nil {
		p := 1
		if m[2] != "" {
			p, _ = strconv.Atoi(m[2])
		}
		if p < 1 {
			return faRef{}
		}
		return faRef{kind: "gallery", author: m[1], page: p}
	}
	if m := faUserPath.FindStringSubmatch(u.Path); m != nil {
		return faRef{kind: "user", author: m[1]}
	}
	return faRef{}
}

func isFuraffinityPage(u *url.URL) bool { return faPageRef(u).kind != "" }

func faSameRef(a, b faRef) bool {
	return a.kind != "" && a.kind == b.kind && a.id == b.id && a.page == b.page && strings.EqualFold(a.author, b.author)
}

func faInsideID(n *html.Node, id string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && attribute(p, "id") == id {
			return true
		}
	}
	return false
}

func faInsideClass(n *html.Node, class string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && faHasClass(p, class) {
			return true
		}
	}
	return false
}

func faHasClass(n *html.Node, class string) bool {
	for _, s := range strings.Fields(attribute(n, "class")) {
		if s == class {
			return true
		}
	}
	return false
}

func faLink(page *url.URL, raw string) (*url.URL, error) {
	if raw == "" || len(raw) > 8192 {
		return nil, errors.New("Fur Affinity 链接无效")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("Fur Affinity 链接无效")
	}
	return crawlURL(page, raw)
}

func faCanonical(doc *html.Node, page *url.URL, expected faRef) bool {
	count := 0
	valid := true
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "meta" || !strings.EqualFold(attribute(n, "property"), "og:url") {
			return
		}
		count++
		u, err := faLink(page, attribute(n, "content"))
		if err != nil || !sameOrigin(page, u) || u.RawQuery != "" {
			valid = false
			return
		}
		r := faPageRef(u)
		// Gallery canonical URLs omit the current pagination index.
		if expected.kind == "gallery" {
			if r.page != 1 && r.page != expected.page {
				valid = false
			}
			r.page = expected.page
		}
		if !faSameRef(expected, r) {
			valid = false
		}
	})
	return count == 1 && valid
}

func faAsset(page *url.URL, raw, author string) (string, bool) {
	u, err := faLink(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "d.furaffinity.net") ||
		(u.Port() != "" && u.Port() != "443") || strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "art" || !strings.EqualFold(parts[1], author) {
		return "", false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
	default:
		return "", false
	}
	return u.String(), true
}

// A work is collected only when its identity, author, displayed image and
// explicit Download action all agree. Other artwork thumbnails are ignored.
func furaffinityPhotoPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	out := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	ref := faPageRef(page)
	if doc == nil || ref.kind != "photo" || !faCanonical(doc, page, ref) {
		return out, errors.New("Fur Affinity 作品身份无法确认，可能需要登录或验证")
	}
	var author, title string
	var full, src, download string
	var imgCount, downloadCount int
	invalid := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "img" && attribute(n, "id") == "submissionImg" {
			imgCount++
			if !faInsideClass(n, "submission-image") {
				invalid = true
			}
			full, src = attribute(n, "data-fullview-src"), attribute(n, "src")
		}
		if n.Data == "a" && faInsideID(n, "submission-options") && strings.EqualFold(strings.TrimSpace(nodeText(n)), "Download") {
			downloadCount++
			if !faInsideClass(n, "submission-content") {
				invalid = true
			}
			download = attribute(n, "href")
		}
		if n.Data == "a" && faInsideClass(n, "submission-description-artist") {
			u, err := faLink(page, attribute(n, "href"))
			if err != nil {
				invalid = true
				return
			}
			r := faPageRef(u)
			if r.kind != "user" || !sameOrigin(page, u) || (author != "" && !strings.EqualFold(author, r.author)) {
				invalid = true
				return
			}
			author = r.author
		}
		if n.Data == "h2" && faInsideClass(n, "submission-title") && faInsideClass(n, "submission-description-artist") {
			title = strings.TrimSpace(nodeText(n))
		}
	})
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if invalid || imgCount != 1 || downloadCount != 1 || author == "" {
		return out, errors.New("Fur Affinity 作品没有可确认的图片下载入口，可能需要登录或验证")
	}
	asset, ok := faAsset(page, download, author)
	if !ok {
		return out, errors.New("Fur Affinity 图片下载地址与作品作者不匹配")
	}
	displayed, displayedOK := faAsset(page, full, author)
	if !displayedOK {
		displayed, displayedOK = faAsset(page, src, author)
	}
	if !displayedOK || displayed != asset {
		return out, errors.New("Fur Affinity 图片与下载入口不一致")
	}
	out.Title = title
	out.Items = append(out.Items, Item{URL: asset, Headers: map[string]string{"Referer": "https://www.furaffinity.net/"}, RequireImage: true, PublicOnly: true,
		Metadata: map[string]string{"furaffinity_submission_id": ref.id, "furaffinity_author": author}})
	return out, nil
}

func faFetch(ctx context.Context, getter Getter, page *url.URL) (*html.Node, *url.URL, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || !sameOrigin(page, final) || !faSameRef(faPageRef(page), faPageRef(final)) {
		return nil, nil, errors.New("Fur Affinity 页面已跳转，可能需要登录或验证")
	}
	return doc, final, nil
}

func discoverFuraffinity(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	out := Discovery{IsPage: true}
	if getter == nil || !isFuraffinityPage(page) {
		return out, errors.New("Fur Affinity 页面地址或下载客户端无效")
	}
	// The public site permanently redirects these legacy URLs to HTTPS.
	// Upgrade before the request so any user-supplied query stays encrypted.
	if page.Scheme == "http" {
		upgraded := *page
		upgraded.Scheme = "https"
		upgraded.Host = upgraded.Hostname()
		page = &upgraded
	}
	doc, final, err := faFetch(ctx, getter, page)
	if err != nil {
		return out, err
	}
	return furaffinityCollect(ctx, getter, final, doc)
}

// Gallery pages expose thumbnail work IDs, not original URLs. Check every
// accessible work page separately and retain successfully verified results.
func furaffinityCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	out := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	ref := faPageRef(page)
	if doc == nil || getter == nil || ref.kind == "" {
		return out, errors.New("Fur Affinity 页面地址或下载客户端无效")
	}
	if ref.kind == "photo" {
		return furaffinityPhotoPage(ctx, doc, page)
	}
	if !faCanonical(doc, page, ref) {
		return out, errors.New("Fur Affinity 作者身份无法确认，可能需要登录或验证")
	}
	if ref.kind == "user" {
		gallery := ""
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "a" {
				return
			}
			u, err := faLink(page, attribute(n, "href"))
			if err != nil || !sameOrigin(page, u) || u.RawQuery != "" {
				return
			}
			r := faPageRef(u)
			if r.kind == "gallery" && r.page == 1 && strings.EqualFold(r.author, ref.author) {
				gallery = u.String()
			}
		})
		if gallery == "" {
			return out, errors.New("Fur Affinity 作者页没有可确认的画廊链接")
		}
		var err error
		page, _ = url.Parse(gallery)
		doc, page, err = faFetch(ctx, getter, page)
		if err != nil {
			return out, err
		}
		ref = faPageRef(page)
	}
	seen := make(map[string]bool)
	attempts := 0
	var incomplete bool
	for pages := 0; pages < faMaxGalleryPages; pages++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if !faCanonical(doc, page, ref) {
			return out, errors.New("Fur Affinity 画廊身份无法确认，结果可能不完整")
		}
		ids, next, malformed := faGalleryItems(doc, page, ref)
		incomplete = incomplete || malformed
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if attempts >= faMaxWorks {
				return out, fmt.Errorf("Fur Affinity 画廊达到 %d 个作品上限，结果不完整", faMaxWorks)
			}
			attempts++
			work := &url.URL{Scheme: "https", Host: page.Host, Path: "/view/" + id + "/"}
			detail, final, err := faFetch(ctx, getter, work)
			if err != nil {
				if ctx.Err() != nil {
					return out, ctx.Err()
				}
				incomplete = true
				continue
			}
			item, err := furaffinityPhotoPage(ctx, detail, final)
			if err != nil || len(item.Items) != 1 || !strings.EqualFold(item.Items[0].Metadata["furaffinity_author"], ref.author) {
				if ctx.Err() != nil {
					return out, ctx.Err()
				}
				incomplete = true
				continue
			}
			out.Items = append(out.Items, item.Items...)
		}
		if next == nil {
			if incomplete {
				return out, errors.New("部分 Fur Affinity 作品无法确认或访问，结果不完整")
			}
			return out, nil
		}
		if pages+1 == faMaxGalleryPages {
			return out, fmt.Errorf("Fur Affinity 画廊达到 %d 页上限，结果不完整", faMaxGalleryPages)
		}
		var err error
		doc, page, err = faFetch(ctx, getter, next)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			return out, fmt.Errorf("读取 Fur Affinity 下一页失败，结果不完整：%w", err)
		}
		ref = faPageRef(page)
	}
	return out, errors.New("Fur Affinity 画廊结果不完整")
}

func faGalleryItems(doc *html.Node, page *url.URL, ref faRef) (ids []string, next *url.URL, invalid bool) {
	sections, navs := 0, 0
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "section" && attribute(n, "id") == "gallery-gallery" {
			sections++
		}
		if faHasClass(n, "navigation-page-name") {
			navs++
			m := faPageNumber.FindStringSubmatch(strings.TrimSpace(nodeText(n)))
			if m == nil || m[1] != strconv.Itoa(ref.page) {
				invalid = true
			}
		}
		if n.Data == "figure" && faInsideID(n, "gallery-gallery") && faHasClass(n, "t-image") {
			id := strings.TrimPrefix(attribute(n, "id"), "sid-")
			if attribute(n, "id") != "sid-"+id || !faDigits.MatchString(id) {
				invalid = true
				return
			}
			linked, authorOK := false, false
			walkHTML(n, func(child *html.Node) {
				if child.Type != html.ElementNode {
					return
				}
				if child.Data == "a" {
					u, err := faLink(page, attribute(child, "href"))
					if err == nil && sameOrigin(page, u) && u.RawQuery == "" && faPageRef(u).kind == "photo" && faPageRef(u).id == id {
						linked = true
					}
				}
				if child.Data == "img" {
					for _, tag := range strings.Fields(attribute(child, "data-tags")) {
						if strings.EqualFold(tag, "u_"+ref.author) {
							authorOK = true
						}
					}
				}
			})
			if linked && authorOK {
				ids = append(ids, id)
			} else {
				invalid = true
			}
		}
		if n.Data == "form" && strings.EqualFold(attribute(n, "method"), "get") && faInsideClass(n, "gallery-navigation") {
			isNext := false
			walkHTML(n, func(child *html.Node) {
				if child.Type == html.ElementNode && child.Data == "button" && strings.EqualFold(strings.TrimSpace(nodeText(child)), "Next") {
					isNext = true
				}
			})
			if !isNext {
				return
			}
			u, err := faLink(page, attribute(n, "action"))
			if err != nil || !sameOrigin(page, u) || u.RawQuery != "" {
				invalid = true
				return
			}
			r := faPageRef(u)
			if r.kind != "gallery" || !strings.EqualFold(r.author, ref.author) || r.page != ref.page+1 {
				invalid = true
				return
			}
			if next != nil && next.String() != u.String() {
				invalid = true
				next = nil
				return
			}
			next = u
		}
	})
	if sections != 1 || navs == 0 {
		invalid = true
	}
	return ids, next, invalid
}
