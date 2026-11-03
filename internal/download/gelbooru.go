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
	gelbooruID        = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	gelbooruDigest    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	gelbooruImageHost = regexp.MustCompile(`^img[0-9]{1,2}\.gelbooru\.com$`)
)

func gelbooruRoute(u *url.URL) (string, string) {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Hostname(), "gelbooru.com") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) || u.Path != "/index.php" {
		return "", ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["page"]) != 1 || q.Get("page") != "post" || len(q["s"]) != 1 {
		return "", ""
	}
	switch q.Get("s") {
	case "view":
		if len(q["id"]) == 1 && gelbooruID.MatchString(q.Get("id")) {
			return "view", q.Get("id")
		}
	case "list":
		if len(q["tags"]) <= 1 && len(q["pid"]) <= 1 {
			if raw := q.Get("pid"); raw != "" {
				if n, err := strconv.Atoi(raw); err != nil || n < 0 || n > 10000000 {
					return "", ""
				}
			}
			return "list", ""
		}
	}
	return "", ""
}

func isGelbooruPage(u *url.URL) bool { kind, _ := gelbooruRoute(u); return kind != "" }

func gelbooruOriginal(raw, digest string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !gelbooruImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || strings.Contains(u.Path, "..") || !gelbooruDigest.MatchString(digest) {
		return ""
	}
	clean := path.Clean("/" + strings.TrimLeft(u.Path, "/"))
	parts := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	if len(parts) != 4 || parts[0] != "images" || parts[1] != digest[:2] || parts[2] != digest[2:4] ||
		strings.TrimSuffix(parts[3], path.Ext(parts[3])) != digest || !imagePath(clean) {
		return ""
	}
	u.Path, u.RawPath, u.Fragment = clean, "", ""
	return u.String()
}

func discoverGelbooru(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isGelbooruPage(root) {
		return result, errors.New("Gelbooru 地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !isGelbooruPage(final) || (root.Scheme == "https" && final.Scheme != "https") || root.Query().Encode() != final.Query().Encode() {
		return result, errors.New("Gelbooru 页面已跳转，可能需要登录或安全验证")
	}
	return gelbooruCollect(ctx, getter, final, doc)
}

func gelbooruPostPage(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	kind, id := gelbooruRoute(page)
	if doc == nil || kind != "view" {
		return result, errors.New("Gelbooru 图片页面无效")
	}
	var digest, original string
	found, conflict := false, false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "section" && gelbooruHasClass(n, "image-container") {
			if found {
				conflict = true
				return
			}
			found = true
			if attribute(n, "data-id") != id {
				conflict = true
			}
			digest = attribute(n, "data-md5")
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:image") {
			original = attribute(n, "content")
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !found || conflict {
		return result, errors.New("Gelbooru 页面与请求图片不匹配")
	}
	image := gelbooruOriginal(original, digest)
	if image == "" {
		return result, errors.New("Gelbooru 未返回可确认的原图")
	}
	item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"gelbooru_post_id": id}}
	if ref := imageReferer(page.String(), image); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	result.Items = append(result.Items, item)
	return result, nil
}

func gelbooruHasClass(n *html.Node, class string) bool {
	for _, item := range strings.Fields(attribute(n, "class")) {
		if item == class {
			return true
		}
	}
	return false
}

// shortcut: inspect at most 100 pages and 1000 posts until larger searches are supported.
func gelbooruCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	kind, _ := gelbooruRoute(page)
	if kind == "view" {
		return gelbooruPostPage(ctx, page, doc)
	}
	result := Discovery{IsPage: true}
	if getter == nil || doc == nil || kind != "list" {
		return result, errors.New("Gelbooru 搜索页面无效")
	}
	seen := map[string]bool{}
	failures, pages := 0, 0
	finish := func(err error) (Discovery, error) {
		if failures > 0 {
			err = errors.Join(err, fmt.Errorf("Gelbooru 有 %d 篇图片页无法读取", failures))
		}
		return result, err
	}
	for ; pages < maxCrawlPages; pages++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		var posts []*url.URL
		var next *url.URL
		var unusableNext bool
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" && result.Title == "" {
				result.Title = nodeText(n)
			}
			if n.Data != "a" {
				return
			}
			u, err := crawlURL(page, attribute(n, "href"))
			if err != nil || !sameOrigin(page, u) {
				return
			}
			if k, id := gelbooruRoute(u); k == "view" && attribute(n, "id") == "p"+id && !seen[id] {
				for p := n.Parent; p != nil; p = p.Parent {
					if p.Type == html.ElementNode && p.Data == "article" && gelbooruHasClass(p, "thumbnail-preview") {
						seen[id] = true
						posts = append(posts, u)
						break
					}
				}
			}
			if k, _ := gelbooruRoute(u); k == "list" {
				var inPaginator bool
				for p := n.Parent; p != nil; p = p.Parent {
					if attribute(p, "id") == "paginator" {
						inPaginator = true
						break
					}
				}
				if !inPaginator {
					return
				}
				currentQ, candidateQ := page.Query(), u.Query()
				current, _ := strconv.Atoi(currentQ.Get("pid"))
				candidate, e := strconv.Atoi(candidateQ.Get("pid"))
				currentQ.Del("pid")
				candidateQ.Del("pid")
				if e != nil || candidate <= current {
					return
				}
				if currentQ.Encode() != candidateQ.Encode() {
					unusableNext = true
					return
				}
				if next == nil {
					next = u
				} else {
					prior, _ := strconv.Atoi(next.Query().Get("pid"))
					if candidate < prior {
						next = u
					}
				}
			}
		})
		if len(posts) == 0 && pages == 0 {
			return finish(errors.New("Gelbooru 未返回可确认的搜索结果"))
		}
		for _, post := range posts {
			if len(result.Items)+failures >= 1000 {
				return finish(errors.New("Gelbooru 达到 1000 篇图片上限，结果可能不完整"))
			}
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			subdoc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: post.String(), referer: pageReferer(page), depth: 1})
			if response != nil {
				response.Body.Close()
			}
			_, id := gelbooruRoute(post)
			_, finalID := gelbooruRoute(final)
			if err != nil || subdoc == nil || !sameOrigin(post, final) || id != finalID {
				failures++
				continue
			}
			found, err := gelbooruPostPage(ctx, final, subdoc)
			if err != nil || len(found.Items) != 1 {
				failures++
				continue
			}
			result.Items = append(result.Items, found.Items...)
		}
		if next == nil {
			if unusableNext {
				return finish(errors.New("Gelbooru 下一页筛选条件不匹配，结果可能不完整"))
			}
			return finish(nil)
		}
		if pages == maxCrawlPages-1 {
			return finish(errors.New("Gelbooru 达到 100 页上限，结果可能不完整"))
		}
		doc2, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: next.String(), referer: pageReferer(page), depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if err != nil || doc2 == nil || !sameOrigin(next, final) || next.Query().Encode() != final.Query().Encode() {
			return finish(errors.New("Gelbooru 下一页不可用，结果可能不完整"))
		}
		page, doc = final, doc2
	}
	return finish(nil)
}
