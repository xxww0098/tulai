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
	goooodArticlePath = regexp.MustCompile(`^/(?:en/)?[^/]+\.htm$`)
	goooodListPath    = regexp.MustCompile(`^/((?:type|category)/[A-Za-z0-9_-]{1,128})(?:/page/([1-9][0-9]{0,4}))?/?$`)
	goooodSizeSuffix  = regexp.MustCompile(`(?i)-[0-9]+x[0-9]+\.jpg$`)
)

const (
	goooodArticleLimit = 1000
	goooodImageLimit   = 10000
)

type goooodListRef struct {
	base, filters string
	page          int
}

func goooodHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	return strings.EqualFold(u.Hostname(), "www.gooood.cn") || strings.EqualFold(u.Hostname(), "gooood.cn")
}

func goooodArticleID(u *url.URL) string {
	if !goooodHost(u) || path.Clean(u.Path) != u.Path || !goooodArticlePath.MatchString(u.Path) {
		return ""
	}
	return u.Path
}

func goooodListID(u *url.URL) goooodListRef {
	if !goooodHost(u) || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return goooodListRef{}
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return goooodListRef{}
	}
	if u.Path == "/search" {
		if len(values["q"]) != 1 || strings.TrimSpace(values.Get("q")) == "" || len(values.Get("q")) > 2048 || len(values["page"]) > 1 {
			return goooodListRef{}
		}
		page := 1
		if raw := values.Get("page"); raw != "" {
			page, err = strconv.Atoi(raw)
			if err != nil || page < 1 || page > 10000 {
				return goooodListRef{}
			}
		}
		values.Del("page")
		return goooodListRef{base: "/search", filters: values.Encode(), page: page}
	}
	match := goooodListPath.FindStringSubmatch(u.Path)
	if len(match) != 3 {
		return goooodListRef{}
	}
	page := 1
	if match[2] != "" {
		page, err = strconv.Atoi(match[2])
		if err != nil || page < 1 || page > 10000 {
			return goooodListRef{}
		}
	}
	return goooodListRef{base: "/" + match[1], filters: values.Encode(), page: page}
}

func isGoooodPage(u *url.URL) bool { return goooodArticleID(u) != "" || goooodListID(u).base != "" }

func goooodClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func goooodAncestor(n *html.Node, class, id string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && ((class != "" && goooodClass(p, class)) || (id != "" && attribute(p, "id") == id)) {
			return true
		}
	}
	return false
}

func goooodImageURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "oss.gooood.cn") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/uploads/") ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", "", false
	}
	u.Fragment = ""
	alternate := u.String()
	if u.RawQuery == "" && goooodSizeSuffix.MatchString(u.Path) {
		u.Path = goooodSizeSuffix.ReplaceAllString(u.Path, ".jpg")
		u.RawPath = ""
	}
	if u.String() == alternate {
		alternate = ""
	}
	return u.String(), alternate, true
}

func discoverGooood(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isGoooodPage(root) {
		return result, errors.New("谷德页面地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("谷德页面不可用或跳转异常")
	}
	if id := goooodArticleID(root); id != "" {
		if goooodArticleID(final) != id {
			return result, errors.New("谷德文章已跳转到其他内容")
		}
	} else if goooodListID(final) != goooodListID(root) {
		return result, errors.New("谷德列表已跳转到其他内容")
	}
	return goooodCollect(ctx, getter, final, doc)
}

func goooodCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || doc == nil || !isGoooodPage(page) {
		return result, errors.New("谷德页面无效")
	}
	if goooodArticleID(page) != "" {
		return goooodArticlePage(ctx, page, doc)
	}
	return goooodList(ctx, getter, page, doc)
}

// goooodArticlePage reads the reference's article and hero image selectors.
// The canonical URL binds those elements to the article the user requested.
func goooodArticlePage(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := goooodArticleID(page)
	if id == "" || doc == nil {
		return result, errors.New("谷德文章页面无效")
	}
	var identity, mismatch, mainPost bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = strings.TrimSpace(strings.SplitN(nodeText(n), "|", 2)[0])
		}
		if n.Data == "article" && attribute(n, "id") == "main-post" {
			mainPost = true
		}
		var raw string
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			raw = attribute(n, "href")
		} else if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			raw = attribute(n, "content")
		}
		if raw != "" {
			u, err := url.Parse(raw)
			if err != nil || goooodArticleID(page.ResolveReference(u)) != id {
				mismatch = true
			} else {
				identity = true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if mismatch || !identity || !mainPost {
		return result, errors.New("谷德页面未能确认当前文章身份或正文")
	}
	seen := map[string]bool{}
	incomplete := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "img" || !goooodAncestor(n, "", "main-post") {
			return
		}
		if !goooodClass(n, "entry-hero-image") && !goooodAncestor(n, "article-image-link", "") {
			return
		}
		image, alternate, ok := goooodImageURL(attribute(n, "src"))
		if !ok {
			incomplete = true
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= goooodImageLimit {
			incomplete = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"gooood_article": id, "gooood_title": result.Title}}
		if alternate != "" {
			item.AlternateURLs = []string{alternate}
		}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("谷德文章没有返回可验证的图片")
	}
	if incomplete {
		return result, errors.New("谷德部分文章图片无法验证或超过采集上限")
	}
	return result, nil
}

// goooodList follows only the site's explicit next links and a bounded number
// of article links; each article is independently checked before extraction.
// shortcut: cap pages, articles, and images until larger collections can be configured.
func goooodList(ctx context.Context, getter Getter, first *url.URL, firstDoc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	page, doc := first, firstDoc
	seenArticles := map[string]bool{}
	seenImages := map[string]bool{}
	var warnings []error
	articleFailed := false
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		ref := goooodListID(page)
		if ref.base == "" || doc == nil {
			return result, errors.Join(append(warnings, errors.New("谷德列表页面无效"))...)
		}
		var articles []*url.URL
		var next string
		var sawPagination, expectedMore, invalid, limited bool
		walkHTML(doc, func(n *html.Node) {
			if ctx.Err() != nil || n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" && result.Title == "" {
				result.Title = strings.TrimSpace(strings.SplitN(nodeText(n), "|", 2)[0])
			}
			if n.Data == "nav" && goooodClass(n, "pagination") {
				sawPagination = true
				if max, err := strconv.Atoi(attribute(n, "data-total-pages")); err == nil && max > ref.page {
					expectedMore = true
				}
			}
			if n.Data == "input" && goooodAncestor(n, "pagination", "") {
				if max, err := strconv.Atoi(attribute(n, "max")); err == nil && max > ref.page {
					expectedMore = true
				}
			}
			if n.Data != "a" {
				return
			}
			if goooodAncestor(n, "pagination", "") && (attribute(n, "aria-label") == "下一页" || strings.EqualFold(attribute(n, "rel"), "next")) {
				next = attribute(n, "href")
			}
			if !goooodClass(n, "result-image") && !goooodClass(n, "cover-link") && !goooodClass(n, "article-cover-link") {
				return
			}
			u, err := crawlURL(page, attribute(n, "href"))
			id := goooodArticleID(u)
			if err != nil || id == "" || !sameOrigin(page, u) {
				invalid = true
				return
			}
			if !seenArticles[id] {
				if len(seenArticles) >= goooodArticleLimit {
					limited = true
					return
				}
				seenArticles[id] = true
				articles = append(articles, u)
			}
		})
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		if invalid {
			warnings = append(warnings, errors.New("谷德部分列表文章地址无法验证"))
		}
		for _, article := range articles {
			found, err := discoverGooood(ctx, getter, article)
			if ctx.Err() != nil {
				return result, errors.Join(append(warnings, ctx.Err())...)
			}
			if err != nil && !articleFailed {
				warnings = append(warnings, fmt.Errorf("谷德部分文章读取失败，结果可能不完整: %w", err))
				articleFailed = true
			}
			for _, item := range found.Items {
				if seenImages[item.URL] {
					continue
				}
				if len(result.Items) >= goooodImageLimit {
					return result, errors.Join(append(warnings, errors.New("谷德列表图片达到采集上限"))...)
				}
				seenImages[item.URL] = true
				result.Items = append(result.Items, item)
			}
		}
		if limited {
			return result, errors.Join(append(warnings, errors.New("谷德列表文章达到采集上限"))...)
		}
		if next == "" {
			if len(result.Items) == 0 {
				return result, errors.Join(append(warnings, errors.New("谷德列表没有返回可采集的文章图片"))...)
			}
			if !sawPagination || expectedMore {
				warnings = append(warnings, errors.New("谷德列表分页信息不完整"))
			}
			return result, errors.Join(warnings...)
		}
		nextPage, err := crawlURL(page, next)
		wanted := ref
		wanted.page++
		if err != nil || !sameOrigin(page, nextPage) || goooodListID(nextPage) != wanted {
			return result, errors.Join(append(warnings, errors.New("谷德列表下一页地址无效"))...)
		}
		nextDoc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: nextPage.String(), depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		if nextDoc == nil || !sameOrigin(nextPage, final) || goooodListID(final) != wanted {
			return result, errors.Join(append(warnings, errors.New("谷德列表翻页后跳转异常"))...)
		}
		page, doc = final, nextDoc
	}
	return result, errors.Join(append(warnings, errors.New("谷德列表达到 100 页上限"))...)
}
