package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

var (
	dribbbleID        = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	dribbbleImagePath = regexp.MustCompile(`^/userupload/[1-9][0-9]{0,19}/file/[A-Za-z0-9_-]+\.(?:jpg|jpeg|png|webp|gif)$`)
	dribbbleResize    = regexp.MustCompile(`^[1-9][0-9]{0,5}x[0-9]{0,6}$`)
)

func dribbbleShotID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		(!strings.EqualFold(u.Hostname(), "dribbble.com") && !strings.EqualFold(u.Hostname(), "www.dribbble.com")) ||
		path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") {
		return ""
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) != 2 || p[0] != "shots" || len(p[1]) > 256 || !utf8.ValidString(p[1]) {
		return ""
	}
	id, slug, hasSlug := strings.Cut(p[1], "-")
	if !dribbbleID.MatchString(id) || (hasSlug && slug == "") {
		return ""
	}
	for _, r := range slug {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ""
		}
	}
	return id
}
func isDribbbleShot(u *url.URL) bool { return dribbbleShotID(u) != "" }
func dribbbleClass(n *html.Node, name string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, v := range strings.Fields(attribute(n, "class")) {
		if v == name {
			return true
		}
	}
	return false
}
func dribbbleVisible(n *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		if p.Type != html.ElementNode {
			continue
		}
		for _, a := range p.Attr {
			if strings.EqualFold(a.Key, "hidden") {
				return false
			}
		}
		style := strings.ToLower(strings.Join(strings.Fields(attribute(p, "style")), ""))
		if strings.EqualFold(attribute(p, "aria-hidden"), "true") || strings.Contains(style, "display:none") || strings.Contains(style, "visibility:hidden") {
			return false
		}
	}
	return true
}
func dribbbleImage(raw string) (*url.URL, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return nil, false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" ||
		!strings.EqualFold(u.Hostname(), "cdn.dribbble.com") || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || !dribbbleImagePath.MatchString(u.Path) {
		return nil, false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, false
	}
	for key, v := range q {
		if len(v) != 1 {
			return nil, false
		}
		switch key {
		case "resize":
			if !dribbbleResize.MatchString(v[0]) {
				return nil, false
			}
		case "vertical":
			if v[0] != "center" {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return u, true
}
func dribbbleIdentity(ctx context.Context, page *url.URL, doc *html.Node) (string, bool) {
	id := dribbbleShotID(page)
	var found, bad bool
	var title string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		var raw string
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			raw = attribute(n, "href")
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			raw = attribute(n, "content")
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:title") && title == "" {
			title = attribute(n, "content")
		}
		if raw == "" {
			return
		}
		u, err := url.Parse(raw)
		if err != nil || dribbbleShotID(page.ResolveReference(u)) != id {
			bad = true
		} else {
			found = true
		}
	})
	return title, found && !bad
}
func dribbbleMediaImage(n, root *html.Node) bool {
	if dribbbleClass(n, "v-img") && dribbbleClass(n, "content-block") {
		return true
	}
	for p := n.Parent; p != nil && p != root; p = p.Parent {
		if dribbbleClass(p, "media-content") {
			return true
		}
	}
	return false
}

// A preload is treated as the larger image only when its exact CDN path is
// already present in the verified shot's media element. OG images can point to
// a different upload and are not sufficient to establish that association.
func dribbbleShot(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := dribbbleShotID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || doc == nil {
		return result, errors.New("Dribbble 作品页面无效")
	}
	title, identity := dribbbleIdentity(ctx, page, doc)
	result.Title = title
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !identity {
		return result, errors.New("Dribbble 作品身份无法确认，可能需要登录或验证")
	}
	var containers []*html.Node
	preloads := map[string]string{}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "div" && dribbbleClass(n, "js-media-container") && attribute(n, "data-shot-id") == id {
			containers = append(containers, n)
		}
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "preload") && strings.EqualFold(attribute(n, "as"), "image") {
			if u, ok := dribbbleImage(attribute(n, "href")); ok && u.RawQuery == "" {
				preloads[strings.ToLower(u.Hostname())+u.Path] = u.String()
			}
		}
	})
	if len(containers) != 1 {
		return result, errors.New("Dribbble 作品图片区域无法确认")
	}
	// Some shot pages render the Vue media as a sibling of the section that
	// carries the shot ID. Only use the observed, directly adjacent structure.
	roots := []*html.Node{containers[0]}
	section := containers[0].Parent
	if section != nil && section.Type == html.ElementNode && section.Data == "section" && attribute(section, "data-screenshot_id") == id &&
		section.Parent != nil && dribbbleClass(section.Parent, "shot-content-container") {
		for n := section.Parent.FirstChild; n != nil; n = n.NextSibling {
			if n.Type == html.ElementNode && n.Data == "div" && attribute(n, "id") == "ssr-app" && dribbbleClass(n, "shot-page-container") {
				roots = append(roots, n)
			}
		}
	}
	seen := map[string]bool{}
	partial := false
	for _, root := range roots {
		walkHTML(root, func(n *html.Node) {
			if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "img" || !dribbbleVisible(n) || !dribbbleMediaImage(n, root) {
				return
			}
			var chosen *url.URL
			for _, raw := range []string{attribute(n, "data-src"), attribute(n, "src")} {
				if raw == "" {
					continue
				}
				u, ok := dribbbleImage(raw)
				if !ok {
					continue
				}
				if chosen == nil {
					chosen = u
				} else if !strings.EqualFold(chosen.Hostname(), u.Hostname()) || chosen.Path != u.Path {
					partial = true
				}
			}
			if chosen == nil {
				partial = true
				return
			}
			key := strings.ToLower(chosen.Hostname()) + chosen.Path
			if seen[key] {
				return
			}
			seen[key] = true
			if len(result.Items) >= 1000 {
				partial = true
				return
			}
			image := chosen.String()
			if original, ok := preloads[key]; ok {
				image = original
			} else {
				partial = true
			}
			// The CDN uses Accept negotiation: advertising WebP can silently
			// transcode an explicitly provided PNG or JPEG into another format.
			item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"dribbble_shot_id": id}, Headers: map[string]string{"Accept": "*/*"}}
			if r := imageReferer(page.Scheme+"://"+page.Host+"/", image); r != "" {
				item.Headers["Referer"] = r
			}
			result.Items = append(result.Items, item)
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("Dribbble 作品未返回可确认的图片，可能需要登录或验证")
	}
	if partial {
		return result, errors.New("Dribbble 已保留可确认的作品图片；部分图片或原始尺寸尚未确认")
	}
	return result, nil
}
func dribbbleGuard(getter Getter, origin *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	id := dribbbleShotID(origin)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || req.URL.Scheme != "https" || dribbbleShotID(req.URL) != id {
			return errors.New("Dribbble 已阻止跨作品或不安全跳转")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("Dribbble 重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func discoverDribbble(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := dribbbleShotID(root)
	if getter == nil || id == "" {
		return result, errors.New("Dribbble 作品地址或下载客户端无效")
	}
	page := *root
	page.Fragment = ""
	doc, final, _, response, err := fetchCrawlPage(ctx, dribbbleGuard(getter, &page), crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return result, fmt.Errorf("Dribbble 页面读取失败，HTTP %d；可能需要登录或验证", status.StatusCode)
		}
		return result, errors.New("Dribbble 页面读取失败，请检查网络或授权状态")
	}
	if doc == nil || final == nil || dribbbleShotID(final) != id || (page.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Dribbble 作品发生跳转或需要登录、验证")
	}
	return dribbbleShot(ctx, final, doc)
}
