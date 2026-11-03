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
	"unicode"

	"golang.org/x/net/html"
)

var (
	animePicturesID      = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	animePicturesHeading = regexp.MustCompile(`№\s*([1-9][0-9]{0,18})\s*$`)
)

// animePicturesRoute recognizes only the two routes used by the reference
// script. Old /pictures/view_post/ links and other site pages are not inferred.
func animePicturesRoute(u *url.URL) (string, string) {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		!strings.EqualFold(u.Hostname(), "anime-pictures.net") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) || u.RawPath != "" {
		return "", ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", ""
	}
	if strings.HasPrefix(u.Path, "/posts/") {
		id := strings.TrimPrefix(u.Path, "/posts/")
		if animePicturesID.MatchString(id) {
			return "post", id
		}
		return "", ""
	}
	if u.Path != "/posts" || len(q["search_tag"]) != 1 || len(q["page"]) > 1 {
		return "", ""
	}
	if raw := q.Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 10000000 || strconv.Itoa(n) != raw {
			return "", ""
		}
	}
	return "search", ""
}

func isAnimePicturesPage(u *url.URL) bool {
	kind, _ := animePicturesRoute(u)
	return kind != ""
}

// animePicturesDownload only accepts a link actually present in the post. The
// numeric filename prefix binds it to the post; it never constructs originals
// from IDs, hashes, or preview URLs.
func animePicturesDownload(page *url.URL, raw, id string) string {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" ||
		(u.Port() != "" && u.Port() != "443") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "anime-pictures.net" && host != "api.anime-pictures.net" {
		return ""
	}
	const prefix = "/pictures/download_image/"
	if !strings.HasPrefix(u.Path, prefix) {
		return ""
	}
	name := strings.TrimPrefix(u.Path, prefix)
	if !strings.HasPrefix(name, id+"-") || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return ""
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp":
	default:
		return ""
	}
	return u.String()
}

func discoverAnimePictures(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isAnimePicturesPage(root) {
		return result, errors.New("Anime-Pictures 页面地址或下载客户端无效")
	}
	if root.Scheme != "https" {
		return result, errors.New("Anime-Pictures 请使用 HTTPS 页面地址")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	kind, id := animePicturesRoute(root)
	finalKind, finalID := animePicturesRoute(final)
	if doc == nil || !sameOrigin(root, final) || kind != finalKind || id != finalID ||
		(kind == "search" && !animePicturesSameSearch(root, final, true)) {
		return result, errors.New("Anime-Pictures 页面已跳转或内容无法确认")
	}
	return animePicturesCollect(ctx, getter, final, doc)
}

func animePicturesPost(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	kind, id := animePicturesRoute(page)
	if kind != "post" || page.Scheme != "https" || doc == nil {
		return result, errors.New("Anime-Pictures 作品页面无效")
	}
	var heading, mismatch bool
	var original string
	var conflicting bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "h1" {
			if m := animePicturesHeading.FindStringSubmatch(nodeText(n)); m != nil {
				heading = true
				mismatch = mismatch || m[1] != id
			}
		}
		if n.Data != "a" || !animePicturesVisible(n) {
			return
		}
		link := animePicturesDownload(page, attribute(n, "href"), id)
		if link == "" {
			return
		}
		if original != "" && original != link {
			conflicting = true
		}
		original = link
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !heading || mismatch {
		return result, errors.New("Anime-Pictures 页面与请求作品不匹配或尚未加载")
	}
	if original == "" || conflicting {
		return result, errors.New("Anime-Pictures 未提供可确认的作品下载链接，可能需要登录或原图不可下载")
	}
	item := Item{URL: original, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"animepictures_post_id": id}}
	if ref := imageReferer(page.String(), original); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	result.Items = append(result.Items, item)
	return result, nil
}

// animePicturesSameSearch permits only the default sort parameters which were
// observed on the site's own next-page link, when the input omitted them.
func animePicturesSameSearch(a, b *url.URL, samePage bool) bool {
	if a == nil || b == nil {
		return false
	}
	qa, qb := a.Query(), b.Query()
	if samePage && firstNonEmpty(qa.Get("page"), "0") != firstNonEmpty(qb.Get("page"), "0") {
		return false
	}
	qa.Del("page")
	qb.Del("page")
	for key, value := range map[string]string{"ldate": "0", "order_by": "date"} {
		if _, ok := qa[key]; !ok && len(qb[key]) == 1 && qb.Get(key) == value {
			qb.Del(key)
		}
	}
	return qa.Encode() == qb.Encode()
}

// Ignore links that the markup explicitly hides. Stylesheet-based visibility
// and authorization are still determined by the site when the link is fetched.
func animePicturesVisible(n *html.Node) bool {
	for p := n; p != nil; p = p.Parent {
		for _, a := range p.Attr {
			key, value := strings.ToLower(a.Key), strings.ToLower(strings.TrimSpace(a.Val))
			if key == "hidden" || (key == "aria-hidden" && value == "true") {
				return false
			}
			if key == "style" {
				for _, rule := range strings.Split(value, ";") {
					prop, val, ok := strings.Cut(rule, ":")
					val = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "!important"))
					if ok && ((strings.TrimSpace(prop) == "display" && val == "none") ||
						(strings.TrimSpace(prop) == "visibility" && (val == "hidden" || val == "collapse"))) {
						return false
					}
				}
			}
		}
	}
	return true
}

func animePicturesHasClass(n *html.Node, name string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == name {
			return true
		}
	}
	return false
}

// Current public HTML was not directly available during implementation. These
// known search-grid classes are limited to avoid collecting footer/recommendations.
func animePicturesSearchThumbnail(n *html.Node) bool {
	inGrid, hasImage := false, false
	for p := n.Parent; p != nil; p = p.Parent {
		if animePicturesHasClass(p, "last-stars") {
			return false
		}
		if animePicturesHasClass(p, "central-block") || animePicturesHasClass(p, "images-block") || animePicturesHasClass(p, "image_body") {
			inGrid = true
		}
	}
	if !inGrid || !animePicturesVisible(n) {
		return false
	}
	walkHTML(n, func(child *html.Node) {
		if child.Type == html.ElementNode && child.Data == "img" {
			hasImage = true
		}
	})
	return hasImage
}

// shortcut: limit a search to 100 pages and 1000 posts until configurable limits exist.
func animePicturesCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	kind, _ := animePicturesRoute(page)
	if kind == "post" {
		return animePicturesPost(ctx, page, doc)
	}
	result := Discovery{IsPage: true}
	if kind != "search" || page.Scheme != "https" || getter == nil || doc == nil {
		return result, errors.New("Anime-Pictures 搜索页面无效")
	}
	seen := map[string]bool{}
	failures := 0
	finish := func(err error) (Discovery, error) {
		if failures > 0 {
			err = errors.Join(err, fmt.Errorf("Anime-Pictures 有 %d 篇作品无法确认或下载", failures))
		}
		return result, err
	}
	for pages := 0; pages < maxCrawlPages; pages++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		var posts []*url.URL
		var next *url.URL
		unusableNext := false
		current, _ := strconv.Atoi(page.Query().Get("page"))
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
			if err != nil || u.User != nil || !sameOrigin(page, u) {
				return
			}
			if k, id := animePicturesRoute(u); k == "post" && !seen[id] && animePicturesSearchThumbnail(n) {
				seen[id] = true
				posts = append(posts, u)
			}
			if k, _ := animePicturesRoute(u); k != "search" {
				return
			}
			candidate, err := strconv.Atoi(u.Query().Get("page"))
			if err != nil || candidate <= current {
				return
			}
			if candidate != current+1 || !animePicturesSameSearch(page, u, false) {
				unusableNext = true
				return
			}
			next = u
		})
		if len(posts) == 0 {
			return finish(errors.New("Anime-Pictures 未发现可确认的搜索作品，可能没有结果或页面尚未加载"))
		}
		for _, post := range posts {
			if len(result.Items)+failures >= 1000 {
				return finish(errors.New("Anime-Pictures 达到 1000 篇作品上限，结果可能不完整"))
			}
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			subdoc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: post.String(), referer: pageReferer(page), depth: 1})
			if response != nil {
				response.Body.Close()
			}
			_, id := animePicturesRoute(post)
			_, finalID := animePicturesRoute(final)
			if err != nil || subdoc == nil || !sameOrigin(post, final) || id != finalID {
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				failures++
				continue
			}
			found, err := animePicturesPost(ctx, final, subdoc)
			if err != nil || len(found.Items) != 1 {
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				failures++
				continue
			}
			result.Items = append(result.Items, found.Items...)
		}
		if next == nil {
			if unusableNext {
				return finish(errors.New("Anime-Pictures 下一页条件不匹配，结果可能不完整"))
			}
			return finish(nil)
		}
		if pages == maxCrawlPages-1 {
			return finish(errors.New("Anime-Pictures 达到 100 页上限，结果可能不完整"))
		}
		doc2, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: next.String(), referer: pageReferer(page), depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if ctx.Err() != nil {
			return finish(ctx.Err())
		}
		finalKind, _ := animePicturesRoute(final)
		if err != nil || doc2 == nil || !sameOrigin(next, final) || finalKind != "search" || !animePicturesSameSearch(next, final, true) {
			return finish(errors.New("Anime-Pictures 下一页不可用，结果可能不完整"))
		}
		page, doc = final, doc2
	}
	return finish(nil)
}
