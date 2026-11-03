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

	"golang.org/x/net/html"
)

var (
	mokoPostPath     = regexp.MustCompile(`^/post/([1-9][0-9]{0,19})\.html$`)
	mokoListPath     = regexp.MustCompile(`^/post/([A-Za-z0-9_-]{1,80})/list\.html$`)
	mokoCategoryPath = regexp.MustCompile(`^/post/([A-Za-z0-9_-]{1,80})/category/([1-9][0-9]{0,19})/([1-9][0-9]{0,5})\.html$`)
	mokoOwnerPath    = regexp.MustCompile(`^/([A-Za-z0-9_-]{1,80})/$`)
	mokoNumber       = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	mokoImagePath    = regexp.MustCompile(`^/users/[0-9]+/[0-9]+/([1-9][0-9]*)/post/[a-f0-9]+/img2_src_[0-9]+\.jpg$`)
)

type mokoRef struct {
	kind, id, owner, category string
	page                      int
}

func mokoPageRef(u *url.URL) mokoRef {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		(!strings.EqualFold(u.Hostname(), "www.moko.cc") && !strings.EqualFold(u.Hostname(), "moko.cc")) ||
		path.Clean(u.Path) != u.Path || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return mokoRef{}
	}
	if m := mokoPostPath.FindStringSubmatch(u.Path); len(m) == 2 {
		return mokoRef{kind: "post", id: m[1]}
	}
	if m := mokoListPath.FindStringSubmatch(u.Path); len(m) == 2 {
		return mokoRef{kind: "list", owner: strings.ToLower(m[1])}
	}
	if m := mokoCategoryPath.FindStringSubmatch(u.Path); len(m) == 4 {
		page, err := strconv.Atoi(m[3])
		if err != nil || page > 100000 {
			return mokoRef{}
		}
		return mokoRef{kind: "category", owner: strings.ToLower(m[1]), category: m[2], page: page}
	}
	return mokoRef{}
}
func isMokoPage(u *url.URL) bool { return mokoPageRef(u).kind != "" }
func mokoClass(n *html.Node, c string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, s := range strings.Fields(attribute(n, "class")) {
		if s == c {
			return true
		}
	}
	return false
}
func mokoAncestor(n *html.Node, c string) bool {
	for p := n; p != nil; p = p.Parent {
		if mokoClass(p, c) {
			return true
		}
	}
	return false
}
func mokoVisible(n *html.Node) bool {
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
func mokoImage(raw, uid string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.EqualFold(u.Hostname(), "img2.moko.cc") || u.RawQuery != "" || u.Fragment != "" || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	m := mokoImagePath.FindStringSubmatch(u.Path)
	if len(m) != 2 || m[1] != uid {
		return "", false
	}
	return u.String(), true
}

type mokoParsedPost struct {
	result  Discovery
	owner   string
	limited bool
}

func mokoPost(ctx context.Context, page *url.URL, doc *html.Node) (mokoParsedPost, error) {
	parsed := mokoParsedPost{result: Discovery{IsPage: true}}
	ref := mokoPageRef(page)
	if err := ctx.Err(); err != nil {
		return parsed, err
	}
	if ref.kind != "post" || doc == nil {
		return parsed, errors.New("美空作品地址无效")
	}
	var userID, postID, owner string
	var userCount, postCount, ownerCount int
	var contents []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if parsed.result.Title == "" && n.Data == "title" {
			parsed.result.Title = nodeText(n)
		}
		switch attribute(n, "id") {
		case "to_user_id":
			if n.Data == "input" {
				userCount++
				userID = attribute(n, "value")
			}
		case "hidPostGeiLiPostId":
			if n.Data == "input" {
				postCount++
				postID = attribute(n, "value")
			}
		case "workNickName":
			if n.Data == "a" {
				ownerCount++
				u, err := url.Parse(attribute(n, "href"))
				if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
					return
				}
				u = page.ResolveReference(u)
				if !sameOrigin(page, u) {
					return
				}
				if m := mokoOwnerPath.FindStringSubmatch(u.Path); len(m) == 2 {
					owner = strings.ToLower(m[1])
				}
			}
		case "postContentDiv":
			if n.Data == "div" {
				contents = append(contents, n)
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return parsed, err
	}
	if postCount != 1 || postID != ref.id || userCount != 1 || !mokoNumber.MatchString(userID) || ownerCount != 1 || owner == "" || len(contents) != 1 {
		return parsed, errors.New("美空作品或作者身份无法确认")
	}
	parsed.owner = owner
	bad, gated := false, false
	seen := map[string]bool{}
	walkHTML(contents[0], func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if mokoClass(n, "post-limit") && mokoVisible(n) {
			gated = true
			return
		}
		if gated || n.Data != "img" || !mokoVisible(n) || !mokoAncestor(n, "pic") || !mokoAncestor(n, "picBox") {
			return
		}
		raw := attribute(n, "src2")
		if raw == "" {
			bad = true
			return
		}
		image, ok := mokoImage(raw, userID)
		if !ok {
			bad = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"moko_post_id": ref.id, "moko_author": owner}}
		if r := imageReferer(page.Scheme+"://"+page.Host+"/", image); r != "" {
			item.Headers = map[string]string{"Referer": r}
		}
		parsed.result.Items = append(parsed.result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return parsed, err
	}
	if len(parsed.result.Items) == 0 {
		return parsed, errors.New("美空作品未返回可确认的公开图片，可能需要登录或访问授权")
	}
	if gated {
		parsed.limited = true
		return parsed, errors.New("美空部分照片需要登录；已保留页面允许查看的图片")
	}
	if bad {
		parsed.limited = true
		return parsed, errors.New("美空部分图片的来源或作者无法确认；已保留可确认的图片")
	}
	return parsed, errors.New("美空仅采集页面已显示的图片，其他图床和原始尺寸尚未确认")
}

type mokoParsedList struct {
	result                  Discovery
	posts, categories, next []*url.URL
}

func mokoList(ctx context.Context, page *url.URL, doc *html.Node) (mokoParsedList, error) {
	out := mokoParsedList{result: Discovery{IsPage: true}}
	ref := mokoPageRef(page)
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if doc == nil || (ref.kind != "list" && ref.kind != "category") {
		return out, errors.New("美空作者页面无效")
	}
	identified := false
	mismatch := false
	seen := map[string]bool{}
	add := func(dst *[]*url.URL, u *url.URL) {
		k := u.String()
		if !seen[k] {
			seen[k] = true
			*dst = append(*dst, u)
		}
	}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if out.result.Title == "" && n.Data == "title" {
			out.result.Title = nodeText(n)
		}
		if n.Data != "a" {
			return
		}
		raw := attribute(n, "href")
		u, err := url.Parse(raw)
		if err != nil || u.User != nil {
			return
		}
		u = page.ResolveReference(u)
		if attribute(n, "id") == "a_post" {
			r := mokoPageRef(u)
			if !sameOrigin(page, u) || r.kind != "list" || r.owner != ref.owner {
				mismatch = true
			} else {
				identified = true
			}
		}
		if !mokoVisible(n) {
			return
		}
		if !sameOrigin(page, u) || u.RawQuery != "" || u.Fragment != "" {
			return
		}
		other := mokoPageRef(u)
		if other.kind == "post" && mokoClass(n, "coverBg") && mokoAncestor(n, "sBox") {
			add(&out.posts, u)
			return
		}
		if ref.kind == "list" && other.kind == "category" && other.owner == ref.owner && other.page == 1 && mokoAncestor(n, "sBox") && n.Parent != nil && mokoClass(n.Parent, "title") {
			add(&out.categories, u)
			return
		}
		if ref.kind == "category" && other.kind == "category" && other.owner == ref.owner && other.category == ref.category && other.page == ref.page+1 && n.Parent != nil && mokoClass(n.Parent, "page") {
			add(&out.next, u)
		}
	})
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if !identified || mismatch {
		return out, errors.New("美空作者页面身份无法确认")
	}
	if len(out.posts) == 0 && len(out.categories) == 0 && len(out.next) == 0 {
		return out, errors.New("美空作者页面没有可确认的作品")
	}
	return out, nil
}
func mokoGuard(getter Getter, origin *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || !sameOrigin(origin, req.URL) {
			return errors.New("美空已阻止跨站跳转")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("美空重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func mokoFetch(ctx context.Context, getter Getter, page *url.URL, depth int) (*html.Node, *url.URL, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, mokoGuard(getter, page), crawlPage{url: page.String(), depth: depth})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		var s *HTTPStatusError
		if errors.As(err, &s) {
			return nil, nil, fmt.Errorf("美空页面读取失败，HTTP %d；可能需要登录或访问授权", s.StatusCode)
		}
		return nil, nil, errors.New("美空页面读取失败，请检查网络或授权状态")
	}
	if doc == nil || final == nil || !sameOrigin(page, final) || mokoPageRef(page) != mokoPageRef(final) {
		return nil, nil, errors.New("美空页面发生跳转或需要登录、访问授权")
	}
	return doc, final, nil
}
func discoverMoko(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isMokoPage(root) {
		return result, errors.New("美空地址或下载客户端无效")
	}
	page := *root
	page.Fragment = ""
	doc, final, err := mokoFetch(ctx, getter, &page, 1)
	if err != nil {
		return result, err
	}
	return mokoCollect(ctx, getter, final, doc)
}
func mokoCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := mokoPageRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || doc == nil || ref.kind == "" {
		return result, errors.New("美空页面或下载客户端无效")
	}
	if ref.kind == "post" {
		parsed, err := mokoPost(ctx, page, doc)
		return parsed.result, err
	}
	first, err := mokoList(ctx, page, doc)
	if err != nil {
		return result, err
	}
	result.Title = first.result.Title
	queue := append(append(first.posts, first.categories...), first.next...)
	seenPages := map[mokoRef]bool{ref: true}
	seenImages := map[string]bool{}
	bad := false
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		current := queue[0]
		queue = queue[1:]
		itemRef := mokoPageRef(current)
		if seenPages[itemRef] {
			continue
		}
		if len(seenPages) >= maxCrawlPages {
			bad = true
			break
		}
		seenPages[itemRef] = true
		child, final, err := mokoFetch(ctx, getter, current, 2)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			bad = true
			continue
		}
		if itemRef.kind == "post" {
			parsed, err := mokoPost(ctx, final, child)
			if parsed.owner != ref.owner {
				bad = true
				continue
			}
			if err != nil && (parsed.limited || len(parsed.result.Items) == 0) {
				bad = true
			}
			for _, item := range parsed.result.Items {
				if !seenImages[item.URL] {
					seenImages[item.URL] = true
					result.Items = append(result.Items, item)
				}
			}
		} else {
			parsed, err := mokoList(ctx, final, child)
			if err != nil {
				bad = true
				continue
			}
			queue = append(queue, parsed.posts...)
			queue = append(queue, parsed.next...)
		}
	}
	if len(result.Items) == 0 {
		return result, errors.New("美空当前作者页面未返回可确认的图片")
	}
	if bad {
		return result, errors.New("美空部分作品受限、未读取或达到分页上限；已保留可查看的图片")
	}
	return result, errors.New("美空仅采集已加载页面中的作品图片；其他图床和原始尺寸尚未确认")
}
