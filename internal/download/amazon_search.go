package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var amazonSearchSize = regexp.MustCompile(`(_(?:AC_)?(?:UL|UY|UX|SL|SX|SY|SS))(?:218|320)([_\.])`)

// Only site-provided search result links are followed; user filters stay fixed.
func isAmazonSearch(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https") && amazonMarket(u.Hostname()) != "" && u.Path == "/s"
}

func amazonSearchValid(u *url.URL) bool {
	if !isAmazonSearch(u) || u.User != nil ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.RawPath != "" || len(u.RawQuery) > 8192 {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["page"]) > 1 {
		return false
	}
	if _, ok := amazonSearchPage(u); !ok {
		return false
	}
	for _, key := range []string{"k", "i", "rh", "bbn", "node"} {
		if strings.TrimSpace(query.Get(key)) != "" {
			return true
		}
	}
	return false
}

func amazonSearchPage(u *url.URL) (int, bool) {
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["page"]) > 1 {
		return 0, false
	}
	if !q.Has("page") {
		return 1, true
	}
	raw := q.Get("page")
	page, err := strconv.Atoi(raw)
	return page, err == nil && page >= 1 && page <= 1000000 && strconv.Itoa(page) == raw
}

// These fields identify Amazon's navigation session, rather than result filters.
func amazonSearchFilters(u *url.URL) string {
	q := u.Query()
	for _, key := range []string{"page", "qid", "ref", "xpid", "crid", "sprefix", "dib", "dib_tag"} {
		q.Del(key)
	}
	return q.Encode()
}

func amazonSearchSameFilter(a, b *url.URL) bool {
	return amazonSearchValid(a) && amazonSearchValid(b) && amazonMarket(a.Hostname()) == amazonMarket(b.Hostname()) &&
		b.Scheme == "https" && amazonSearchFilters(a) == amazonSearchFilters(b)
}

func amazonSearchSamePage(a, b *url.URL) bool {
	if !amazonSearchSameFilter(a, b) {
		return false
	}
	first, _ := amazonSearchPage(a)
	last, _ := amazonSearchPage(b)
	return first == last
}

func amazonSearchGetter(getter Getter, page *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !amazonSearchSamePage(page, req.URL) {
			return errors.New("亚马逊搜索页已跳转，可能需要登录或验证")
		}
		if previous != nil {
			if err := previous(req, via); err != nil {
				return err
			}
		}
		if !amazonSearchSamePage(page, req.URL) {
			return errors.New("亚马逊搜索页已跳转，可能需要登录或验证")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}

func amazonSearchFetch(ctx context.Context, getter Getter, page *url.URL) (*html.Node, *url.URL, error) {
	switch g := getter.(type) {
	case *HTTPGetter:
		if g == nil || g.client == nil {
			return nil, nil, errors.New("亚马逊搜索下载客户端无效")
		}
		getter = amazonSearchGetter(g, page)
	case *RenderedSession:
		if g == nil {
			return nil, nil, errors.New("亚马逊浏览器会话无效")
		}
		ctx = context.WithValue(ctx, renderedRedirectGuardKey{}, func(next *url.URL) bool { return amazonSearchSamePage(page, next) })
	default:
		return nil, nil, errors.New("亚马逊搜索需要支持安全重定向的下载客户端")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || !amazonSearchSamePage(page, final) {
		return nil, nil, errors.New("亚马逊搜索页已跳转，可能需要登录或验证")
	}
	return doc, final, nil
}

func discoverAmazonSearch(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !amazonSearchValid(root) {
		return result, errors.New("亚马逊搜索地址或下载客户端无效")
	}
	requested := *root
	if requested.Scheme == "http" {
		requested.Scheme, requested.Host = "https", requested.Hostname()
	}
	doc, page, err := amazonSearchFetch(ctx, getter, &requested)
	if err != nil {
		return result, fmt.Errorf("读取亚马逊搜索页失败: %w", err)
	}
	return amazonSearchCollect(ctx, getter, page, doc)
}

func amazonSearchClass(n *html.Node, class string) bool {
	for _, part := range strings.Fields(attribute(n, "class")) {
		if part == class {
			return true
		}
	}
	return false
}

// Amazon hides its duplicate image link from accessibility, while displaying it visually.
func amazonSearchVisible(n *html.Node) bool {
	var imageLink *html.Node
	if n != nil && n.Data == "img" {
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Data == "a" {
				imageLink = p
				break
			}
		}
	}
	for depth, p := 0, n; p != nil; depth, p = depth+1, p.Parent {
		if depth >= 128 {
			return false
		}
		if p.Type != html.ElementNode {
			continue
		}
		if p.Data == "script" || p.Data == "style" || p.Data == "template" || p.Data == "noscript" {
			return false
		}
		for _, a := range p.Attr {
			key, value := strings.ToLower(a.Key), strings.ToLower(strings.TrimSpace(a.Val))
			if key == "hidden" || (key == "aria-hidden" && value == "true" && !(n.Data == "img" && (p == n || p == imageLink))) {
				return false
			}
			if p == n && n.Data == "img" && (key == "width" || key == "height") && googleImagesCSSZero(value) {
				return false
			}
			if key == "style" {
				for _, rule := range strings.Split(value, ";") {
					k, v, ok := strings.Cut(rule, ":")
					k = strings.TrimSpace(k)
					v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
					if ok && ((k == "display" && v == "none") || (k == "visibility" && (v == "hidden" || v == "collapse")) ||
						(k == "opacity" && googleImagesCSSZero(v)) || ((k == "width" || k == "height") && googleImagesCSSZero(strings.TrimSuffix(v, "px")))) {
						return false
					}
				}
			}
		}
	}
	return true
}

func amazonSearchCard(page *url.URL, card *html.Node) (Item, bool) {
	id := strings.ToUpper(attribute(card, "data-asin"))
	if !amazonASIN.MatchString(id) || !amazonSearchVisible(card) {
		return Item{}, false
	}
	var image, title, alt string
	walkHTML(card, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "h2" && title == "" && amazonSearchVisible(n) {
			title = firstNonEmpty(attribute(n, "aria-label"), nodeText(n))
		}
		if image != "" || n.Data != "img" || !amazonSearchClass(n, "s-image") || !amazonSearchVisible(n) {
			return
		}
		// The image must be linked directly to this card's ASIN, not to a swatch or recommendation.
		for p := n.Parent; p != nil && p != card; p = p.Parent {
			if p.Data != "a" {
				continue
			}
			link, err := crawlURL(page, attribute(p, "href"))
			if err == nil && link.User == nil && amazonMarket(link.Hostname()) == amazonMarket(page.Hostname()) && amazonProductID(link) == id {
				image, _ = amazonImageURL(page, attribute(n, "src"))
			}
			break
		}
		alt = attribute(n, "alt")
	})
	if image == "" {
		return Item{}, false
	}
	title = strings.TrimSpace(firstNonEmpty(title, alt))
	if title == "" {
		title = id
	}
	item := Item{URL: image, RequireImage: true, PublicOnly: true,
		Metadata: map[string]string{"album_id": id, "album_title": title, "amazon_asin": id}}
	if u, err := url.Parse(image); err == nil && u.RawQuery == "" {
		u.Path = amazonSearchSize.ReplaceAllString(u.Path, `${1}1500${2}`)
		if u.String() != image {
			item.URL, item.AlternateURLs = u.String(), []string{image}
		}
	}
	if ref := imageReferer(page.String(), item.URL); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	return item, true
}

// shortcut: scan at most 100 site-provided pages; static HTML cannot confirm external CSS or image decoding.
func amazonSearchCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !amazonSearchValid(page) || doc == nil {
		return result, errors.New("亚马逊搜索页面无效")
	}
	seenPages := map[int]bool{}
	seenItems := map[string]bool{}
	var warnings []error
	missing := false
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		current, _ := amazonSearchPage(page)
		if seenPages[current] {
			return result, errors.Join(append(warnings, errors.New("亚马逊搜索分页重复，结果可能不完整"))...)
		}
		seenPages[current] = true
		var next string
		var conflictingNext, nav, blocked, noResults, searchLayout, limited bool
		before := len(result.Items)
		walkHTML(doc, func(n *html.Node) {
			if ctx.Err() != nil {
				return
			}
			if n.Type == html.TextNode && n.Parent != nil && n.Parent.Data != "script" && n.Parent.Data != "style" {
				text := strings.ToLower(n.Data)
				blocked = blocked || strings.Contains(text, "enter the characters you see below") || strings.Contains(text, "sorry, we just need to make sure")
				noResults = noResults || strings.Contains(text, "no results for") || strings.Contains(text, "no results found") || strings.Contains(text, "aucun résultat pour")
			}
			if n.Type != html.ElementNode {
				return
			}
			if n.Data == "title" && result.Title == "" {
				result.Title = nodeText(n)
			}
			if n.Data == "form" && (strings.Contains(strings.ToLower(attribute(n, "action")), "validatecaptcha") || attribute(n, "id") == "auth-captcha-form") {
				blocked = true
			}
			if amazonSearchClass(n, "s-main-slot") || attribute(n, "data-component-type") == "s-search-result" {
				searchLayout = true
			}
			if amazonSearchClass(n, "s-pagination-next") && amazonSearchVisible(n) {
				nav = true
				if n.Data == "a" && !amazonSearchClass(n, "s-pagination-disabled") && attribute(n, "aria-disabled") != "true" {
					href := attribute(n, "href")
					if next != "" && next != href {
						conflictingNext = true
					}
					next = href
				}
			}
			if n.Data != "div" || attribute(n, "data-component-type") != "s-search-result" || !amazonSearchVisible(n) {
				return
			}
			item, ok := amazonSearchCard(page, n)
			if !ok {
				missing = true
				return
			}
			key := item.Metadata["amazon_asin"] + "\x00" + item.URL
			if !seenItems[key] {
				if len(result.Items) >= maxAmazonImages {
					limited = true
					return
				}
				seenItems[key] = true
				result.Items = append(result.Items, item)
			}
		})
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		if blocked {
			result.Items = result.Items[:before]
			return result, errors.Join(append(warnings, errors.New("亚马逊搜索要求验证，结果可能不完整"))...)
		}
		if limited {
			return result, errors.Join(append(warnings, errors.New("亚马逊搜索图片达到采集上限，结果可能不完整"))...)
		}
		if !searchLayout && !noResults {
			return result, errors.Join(append(warnings, errors.New("亚马逊搜索未返回可识别的结果页，结果可能不完整"))...)
		}
		if count > 0 && len(result.Items) == before && !noResults {
			return result, errors.Join(append(warnings, errors.New("亚马逊分页没有返回新的商品图片，结果可能不完整"))...)
		}
		if conflictingNext {
			return result, errors.Join(append(warnings, errors.New("亚马逊下一页地址不唯一，结果可能不完整"))...)
		}
		if next == "" {
			if missing {
				warnings = append(warnings, errors.New("亚马逊部分商品缺少可确认的主图或链接"))
			}
			if !nav && !noResults {
				warnings = append(warnings, errors.New("亚马逊搜索分页信息不完整"))
			}
			return result, errors.Join(warnings...)
		}
		nextPage, err := crawlURL(page, next)
		if err != nil || !amazonSearchSameFilter(page, nextPage) {
			return result, errors.Join(append(warnings, errors.New("亚马逊下一页搜索条件发生变化，结果可能不完整"))...)
		}
		nextNumber, valid := amazonSearchPage(nextPage)
		if !valid || nextNumber != current+1 {
			return result, errors.Join(append(warnings, errors.New("亚马逊下一页页码无效，结果可能不完整"))...)
		}
		doc, page, err = amazonSearchFetch(ctx, getter, nextPage)
		if err != nil {
			return result, errors.Join(append(warnings, fmt.Errorf("读取亚马逊下一页失败，已保留当前结果: %w", err))...)
		}
	}
	return result, errors.Join(append(warnings, errors.New("亚马逊搜索达到 100 页上限，结果可能不完整"))...)
}
