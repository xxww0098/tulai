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

	"golang.org/x/net/html"
)

var (
	neteaseBlogOwner       = regexp.MustCompile(`^[A-Za-z0-9_@.-]{1,128}$`)
	neteaseBlogPost        = regexp.MustCompile(`^[0-9]{1,64}$`)
	neteaseBlogHostOwner   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	neteaseBlogImageHost   = regexp.MustCompile(`^img[0-9]{0,4}\.ph\.126\.net$`)
	neteaseBlogURLProperty = regexp.MustCompile(`\bblogUrl\s*:\s*`)
)

type neteaseBlogReference struct{ owner, id string }

// The original platform used both a user path and a user subdomain for posts.
func neteaseBlogRef(u *url.URL) neteaseBlogReference {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return neteaseBlogReference{}
	}
	host := strings.ToLower(u.Hostname())
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	var owner, id string
	if host == "blog.163.com" || host == "www.blog.163.com" {
		if len(p) != 4 || p[1] != "blog" || p[2] != "static" {
			return neteaseBlogReference{}
		}
		owner, id = p[0], p[3]
		if !neteaseBlogOwner.MatchString(owner) || owner == "." || owner == ".." {
			return neteaseBlogReference{}
		}
	} else {
		if !strings.HasSuffix(host, ".blog.163.com") || len(p) != 3 || p[0] != "blog" || p[1] != "static" {
			return neteaseBlogReference{}
		}
		owner, id = strings.TrimSuffix(host, ".blog.163.com"), p[2]
		if !neteaseBlogHostOwner.MatchString(owner) {
			return neteaseBlogReference{}
		}
	}
	if !neteaseBlogPost.MatchString(id) {
		return neteaseBlogReference{}
	}
	return neteaseBlogReference{owner: strings.ToLower(owner), id: id}
}
func isNeteaseBlogArticle(u *url.URL) bool { return neteaseBlogRef(u).id != "" }
func neteaseBlogClass(n *html.Node, c string) bool {
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

// Preserve the image URL shown by the archived article; a display URL is not
// evidence that the original-resolution file is available.
func neteaseBlogImage(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Fragment != "" || u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if !neteaseBlogImageHost.MatchString(host) && host != "img.bimg.126.net" {
		return "", false
	}
	return u.String(), true
}

func neteaseBlogArticle(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := neteaseBlogRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ref.id == "" || doc == nil {
		return result, errors.New("网易博客文章地址无效")
	}
	var scripts []string
	var images []string
	active := false
	starts, ends := 0, 0
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if result.Title == "" && n.Data == "title" {
			result.Title = nodeText(n)
		}
		if n.Data == "script" || (n.Data == "textarea" && attribute(n, "name") == "js" && neteaseBlogClass(n.Parent, "nb-init")) {
			var b strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					b.WriteString(c.Data)
				}
			}
			scripts = append(scripts, b.String())
		}
		if neteaseBlogClass(n, "nbw-blog-start") {
			starts++
			active = true
			return
		}
		if neteaseBlogClass(n, "nbw-blog-end") {
			ends++
			active = false
			return
		}
		if active && n.Data == "img" {
			if raw := attribute(n, "src"); raw != "" {
				images = append(images, raw)
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if starts != 1 || ends != 1 || active {
		return result, errors.New("网易博客正文边界无法确认，页面可能已迁移或不可用")
	}
	verified, wrong := false, false
	for _, script := range scripts {
		for _, match := range offer1688CodeMatches(script, neteaseBlogURLProperty) {
			s := strings.TrimSpace(script[match[1]:])
			value, used, ok := offer1688String(s)
			if !ok || !offer1688LiteralEnd(s[used:]) || strings.TrimSuffix(value, "/") != "blog/static/"+ref.id {
				wrong = true
			} else {
				verified = true
			}
		}
	}
	if !verified || wrong {
		return result, errors.New("网易博客正文与文章编号不匹配")
	}
	seen := map[string]bool{}
	bad := false
	for _, raw := range images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		image, ok := neteaseBlogImage(raw)
		if !ok {
			bad = true
			continue
		}
		if seen[image] {
			continue
		}
		seen[image] = true
		if len(result.Items) >= 10000 {
			bad = true
			break
		}
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"netease_blog_post_id": ref.id}}
		if r := imageReferer(page.Scheme+"://"+page.Host+"/", image); r != "" {
			item.Headers = map[string]string{"Referer": r}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, errors.New("网易博客正文没有可确认的图片，旧站可能已停止服务")
	}
	if bad {
		return result, errors.New("网易博客部分图片的来源未确认；已保留正文中可确认的展示图")
	}
	return result, errors.New("网易博客已停止服务；仅保留当前页面可确认的展示图，原始尺寸和其他图床尚未确认")
}

func neteaseBlogGuard(getter Getter, origin *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || !sameOrigin(origin, req.URL) {
			return errors.New("网易博客已阻止跨站跳转")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("网易博客重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func discoverNeteaseBlog(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isNeteaseBlogArticle(root) {
		return result, errors.New("网易博客文章地址或下载客户端无效")
	}
	page := *root
	page.Fragment = ""
	doc, final, _, response, err := fetchCrawlPage(ctx, neteaseBlogGuard(getter, &page), crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return result, fmt.Errorf("网易博客旧站文章不可用（HTTP %d），服务已关停或内容已迁移", status.StatusCode)
		}
		return result, errors.New("网易博客旧站文章不可用，服务已关停或内容已迁移")
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || neteaseBlogRef(final) != neteaseBlogRef(&page) {
		return result, errors.New("网易博客文章发生跳转或已不可用")
	}
	return neteaseBlogArticle(ctx, final, doc)
}
