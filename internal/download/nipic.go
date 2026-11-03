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
	nipicPostPath   = regexp.MustCompile(`^/show/([1-9][0-9]{0,18})\.html$`)
	huituPostPath   = regexp.MustCompile(`^/photo/show/([0-9]{8})/([0-9]{12})\.html$`)
	errNipicPreview = errors.New("昵图/汇图结果为网页预览图，素材源文件请在网站获取")
)

// nipicRoute includes confirmed but unsupported paths so they cannot fall
// through to generic page scanning and accidentally collect recommendations.
func nipicRoute(u *url.URL) (string, string) {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) || u.RawPath != "" {
		return "", ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "www.nipic.com":
		if m := nipicPostPath.FindStringSubmatch(u.Path); m != nil {
			return "nipic", m[1]
		}
	case "www.huitu.com":
		if m := huituPostPath.FindStringSubmatch(u.Path); m != nil {
			return "huitu", m[1] + m[2]
		}
		if strings.HasPrefix(u.Path, "/photo/vcg/detail/") || strings.HasPrefix(u.Path, "/design/show/") {
			return "unsupported", ""
		}
	case "soso.nipic.com":
		if strings.HasPrefix(u.Path, "/q_") && strings.HasSuffix(u.Path, ".html") {
			return "unsupported", ""
		}
		if u.Path != "" && u.Path != "/" {
			return "", ""
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["q"]) > 1 || len(q["page"]) > 1 {
			return "", ""
		}
		if raw := q.Get("page"); raw != "" {
			page, err := strconv.Atoi(raw)
			if err != nil || page < 1 || page > 10000000 || strconv.Itoa(page) != raw {
				return "", ""
			}
		}
		return "search", ""
	}
	return "", ""
}

func isNipicPage(u *url.URL) bool {
	kind, _ := nipicRoute(u)
	return kind != ""
}

func nipicClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func nipicWithin(n *html.Node, key, value string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if (key == "id" && attribute(p, "id") == value) || (key == "class" && nipicClass(p, value)) {
			return true
		}
	}
	return false
}

func nipicPreviewURL(page *url.URL, raw, kind, id string) string {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") {
		return ""
	}
	ext := strings.ToLower(path.Ext(u.Path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp":
	default:
		return ""
	}
	switch kind {
	case "nipic":
		if !strings.EqualFold(u.Hostname(), "pic.nximg.cn") || !strings.HasPrefix(u.Path, "/file/") {
			return ""
		}
	case "huitu":
		if len(id) != 20 || !strings.EqualFold(u.Hostname(), "pic.huitu.com") || !strings.HasPrefix(u.Path, "/res/"+id[:8]+"/") {
			return ""
		}
		parts := strings.Split(strings.TrimSuffix(path.Base(u.Path), ext), "_")
		if len(parts) != 3 || parts[0] == "" || parts[1] != id || parts[2] != "1" {
			return ""
		}
	default:
		return ""
	}
	return u.String()
}

func nipicPost(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	kind, id := nipicRoute(page)
	if page == nil || page.Scheme != "https" || doc == nil || (kind != "nipic" && kind != "huitu") {
		return result, errors.New("昵图/汇图作品页面无效")
	}
	var canonical, workID, preview, ogImage string
	var idLabel bool
	conflict := false
	set := func(dst *string, val string) {
		if val == "" {
			return
		}
		if *dst != "" && *dst != val {
			conflict = true
		}
		*dst = val
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "h1" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if kind == "nipic" {
			if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
				set(&canonical, attribute(n, "href"))
			}
			if n.Data == "input" && attribute(n, "id") == "worksId" {
				set(&workID, attribute(n, "value"))
			}
			if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:image") {
				set(&ogImage, nipicPreviewURL(page, attribute(n, "content"), kind, id))
			}
			if n.Data == "img" && attribute(n, "id") == "J_worksImg" && nipicClass(n, "works-img") && nipicWithin(n, "id", "J_detailMain") {
				set(&preview, nipicPreviewURL(page, attribute(n, "src"), kind, id))
				if result.Title == "" {
					result.Title = attribute(n, "alt")
				}
			}
		} else {
			if n.Data == "label" && nipicWithin(n, "class", "pic-info-box") {
				label := strings.TrimSpace(nodeText(n))
				if strings.HasPrefix(label, "编号：") {
					if strings.TrimSpace(strings.TrimPrefix(label, "编号：")) == id {
						idLabel = true
					} else {
						conflict = true
					}
				}
			}
			if n.Data == "img" && nipicClass(n, "m-img") && nipicWithin(n, "class", "m-img-wrap") {
				set(&preview, nipicPreviewURL(page, attribute(n, "src"), kind, id))
				if result.Title == "" {
					result.Title = attribute(n, "alt")
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if kind == "nipic" {
		canonicalURL, err := crawlURL(page, canonical)
		cKind, cID := nipicRoute(canonicalURL)
		if err != nil || !sameOrigin(page, canonicalURL) || cKind != "nipic" || cID != id || workID != id || ogImage == "" || preview != ogImage {
			return result, errors.New("昵图作品身份或预览图无法确认")
		}
	} else if !idLabel {
		return result, errors.New("汇图作品编号无法确认")
	}
	if conflict || preview == "" {
		return result, errors.New("昵图/汇图未提供可确认的预览图")
	}
	item := Item{URL: preview, RequireImage: true, PublicOnly: true, Metadata: map[string]string{
		"nipic_work_id": id, "image_kind": "preview", "source_site": kind,
	}}
	if ref := imageReferer(page.String(), preview); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	result.Items = append(result.Items, item)
	return result, errNipicPreview
}

func nipicSameSearch(a, b *url.URL, samePage bool) bool {
	if a == nil || b == nil {
		return false
	}
	qa, qb := a.Query(), b.Query()
	if samePage && firstNonEmpty(qa.Get("page"), "1") != firstNonEmpty(qb.Get("page"), "1") {
		return false
	}
	qa.Del("page")
	qb.Del("page")
	return qa.Encode() == qb.Encode()
}

func nipicFetchPage(ctx context.Context, getter Getter, target *url.URL, referer string) (*html.Node, *url.URL, error) {
	doc, final, _, resp, err := fetchCrawlPage(ctx, getter, crawlPage{url: target.String(), referer: referer, depth: 1})
	if resp != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, nil, err
	}
	kind, id := nipicRoute(target)
	finalKind, finalID := nipicRoute(final)
	if doc == nil || !sameOrigin(target, final) || kind != finalKind || id != finalID ||
		(kind == "search" && !nipicSameSearch(target, final, true)) {
		return nil, nil, errors.New("昵图/汇图页面已跳转或内容无法确认")
	}
	return doc, final, nil
}

func discoverNipic(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	kind, _ := nipicRoute(root)
	if kind == "" || getter == nil {
		return result, errors.New("昵图/汇图地址或下载客户端无效")
	}
	if kind == "unsupported" {
		return result, errors.New("此昵图/汇图页面尚未支持，无法确认预览内容")
	}
	if root.Scheme != "https" {
		return result, errors.New("昵图/汇图请使用 HTTPS 页面地址")
	}
	doc, final, err := nipicFetchPage(ctx, getter, root, "")
	if err != nil {
		return result, err
	}
	return nipicCollect(ctx, getter, final, doc)
}

type nipicSearchPage struct {
	posts       []*url.URL
	next        *url.URL
	unsupported int
}

func nipicExtractSearch(page *url.URL, doc *html.Node) (nipicSearchPage, error) {
	var result nipicSearchPage
	kind, _ := nipicRoute(page)
	if kind != "search" || page.Scheme != "https" || doc == nil {
		return result, errors.New("昵图搜索页面无效")
	}
	current, _ := strconv.Atoi(firstNonEmpty(page.Query().Get("page"), "1"))
	seen := map[string]bool{}
	foundGrid, duplicateGrid, invalidNext := false, false, false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "ul" && attribute(n, "id") == "img-list-outer" {
			if foundGrid {
				duplicateGrid = true
			}
			foundGrid = true
		}
		if n.Data != "a" {
			return
		}
		u, err := crawlURL(page, attribute(n, "href"))
		if err != nil || u.User != nil || u.Scheme != "https" {
			return
		}
		if nipicClass(n, "search-works-thumb") && nipicWithin(n, "class", "new-search-works-item") && nipicWithin(n, "id", "img-list-outer") {
			k, id := nipicRoute(u)
			if k == "unsupported" {
				result.unsupported++
			} else if (k == "nipic" || k == "huitu") && !seen[k+id] {
				seen[k+id] = true
				result.posts = append(result.posts, u)
			} else if k == "" {
				result.unsupported++
			}
		}
		if !nipicWithin(n, "class", "common-page-box") || !sameOrigin(page, u) {
			return
		}
		if k, _ := nipicRoute(u); k != "search" {
			return
		}
		candidate, err := strconv.Atoi(u.Query().Get("page"))
		if err != nil || candidate <= current {
			return
		}
		if candidate == current+1 {
			if !nipicSameSearch(page, u, false) {
				invalidNext = true
			} else {
				result.next = u
			}
		}
	})
	if !foundGrid {
		return result, errors.New("昵图未发现可确认的搜索结果列表，可能没有结果或页面尚未加载")
	}
	if duplicateGrid {
		return nipicSearchPage{}, errors.New("昵图搜索页面结构异常，结果可能不完整")
	}
	if invalidNext {
		return result, errors.New("昵图搜索页面结构或分页条件异常，结果可能不完整")
	}
	return result, nil
}

// shortcut: limit search to 100 pages and 1000 works until configurable limits exist.
func nipicCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	kind, _ := nipicRoute(page)
	if kind == "nipic" || kind == "huitu" {
		return nipicPost(ctx, page, doc)
	}
	result := Discovery{IsPage: true}
	if kind == "unsupported" {
		return result, errors.New("此昵图/汇图页面尚未支持，无法确认预览内容")
	}
	if kind != "search" || getter == nil || page.Scheme != "https" || doc == nil {
		return result, errors.New("昵图搜索页面无效")
	}
	seen := map[string]bool{}
	failed, unsupported := 0, 0
	finish := func(err error) (Discovery, error) {
		if len(result.Items) > 0 {
			err = errors.Join(err, errNipicPreview)
		}
		if failed > 0 {
			err = errors.Join(err, fmt.Errorf("昵图有 %d 篇作品无法读取，结果可能不完整", failed))
		}
		if unsupported > 0 {
			err = errors.Join(err, fmt.Errorf("昵图有 %d 个搜索结果的页面类型尚未支持", unsupported))
		}
		return result, err
	}
	for pages := 0; pages < maxCrawlPages; pages++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		var title string
		walkHTML(doc, func(n *html.Node) {
			if title == "" && n.Type == html.ElementNode && n.Data == "title" {
				title = nodeText(n)
			}
		})
		if result.Title == "" {
			result.Title = title
		}
		list, listErr := nipicExtractSearch(page, doc)
		unsupported += list.unsupported
		for _, post := range list.posts {
			k, id := nipicRoute(post)
			if seen[k+id] {
				continue
			}
			seen[k+id] = true
			if len(result.Items)+failed >= 1000 {
				return finish(errors.New("昵图达到 1000 篇作品上限，结果可能不完整"))
			}
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			subdoc, final, err := nipicFetchPage(ctx, getter, post, pageReferer(page))
			if err != nil {
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				failed++
				continue
			}
			found, err := nipicPost(ctx, final, subdoc)
			if !errors.Is(err, errNipicPreview) || len(found.Items) != 1 {
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				failed++
				continue
			}
			result.Items = append(result.Items, found.Items...)
		}
		if listErr != nil {
			return finish(listErr)
		}
		if list.next == nil {
			if len(result.Items) == 0 && failed == 0 && unsupported == 0 {
				return finish(errors.New("昵图未发现可确认的搜索作品"))
			}
			return finish(nil)
		}
		if pages == maxCrawlPages-1 {
			return finish(errors.New("昵图达到 100 页上限，结果可能不完整"))
		}
		doc2, final, err := nipicFetchPage(ctx, getter, list.next, pageReferer(page))
		if err != nil {
			if ctx.Err() != nil {
				return finish(ctx.Err())
			}
			return finish(errors.New("昵图下一页不可用，结果可能不完整"))
		}
		page, doc = final, doc2
	}
	return finish(nil)
}
