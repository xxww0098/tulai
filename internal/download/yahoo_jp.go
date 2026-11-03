package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var yahooJPProductPath = regexp.MustCompile(`^/([A-Za-z0-9][A-Za-z0-9_-]{0,100})/([A-Za-z0-9][A-Za-z0-9_.-]{0,199})\.html$`)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxYahooJPImages = 10000

func yahooJPProductID(u *url.URL) (string, string) {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "store.shopping.yahoo.co.jp") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return "", ""
	}
	m := yahooJPProductPath.FindStringSubmatch(u.Path)
	if len(m) != 3 || strings.Contains(m[2], "..") || strings.EqualFold(m[2], "search") {
		return "", ""
	}
	return strings.ToLower(m[1]), strings.ToLower(m[2])
}

func isYahooJPProduct(u *url.URL) bool {
	shop, code := yahooJPProductID(u)
	return shop != "" && code != ""
}

func discoverYahooJPProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	shop, code := yahooJPProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || shop == "" || code == "" {
		return result, errors.New("日本雅虎商品地址或下载客户端无效")
	}
	page := *root
	if _, ok := getter.(*HTTPGetter); ok {
		page.Scheme, page.Host = "https", "store.shopping.yahoo.co.jp"
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取日本雅虎商品失败: %w", err)
	}
	finalShop, finalCode := yahooJPProductID(final)
	if doc == nil || final == nil || !sameOrigin(&page, final) || finalShop != shop || finalCode != code {
		return result, errors.New("日本雅虎商品已跳转，可能需要登录或安全验证")
	}
	return yahooJPProductPage(ctx, doc, final)
}

func yahooJPHasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func yahooJPImageURL(page *url.URL, shop, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "item-shopping.c.yimg.jp":
		parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "i" || len(parts[1]) != 1 || parts[2] == "" {
			return "", false
		}
	case "shopping.c.yimg.jp":
		if !strings.HasPrefix(strings.ToLower(u.Path), "/lib/"+shop+"/") {
			return "", false
		}
	case "shopping.geocities.jp":
		if !strings.HasPrefix(strings.ToLower(u.Path), "/"+shop+"/") {
			return "", false
		}
	default:
		return "", false
	}
	u.Scheme, u.Host = "https", host
	return u.String(), true
}

// Yahoo's f size preserves the image's aspect ratio in the verified examples.
func yahooJPLargeImage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() != "item-shopping.c.yimg.jp" || u.RawQuery != "" || len(u.Path) < 6 ||
		!strings.HasPrefix(u.Path, "/i/") || u.Path[4] != '/' || !strings.ContainsRune("degijklnopq", rune(u.Path[3])) {
		return raw
	}
	u.Path = "/i/f/" + u.Path[5:]
	u.RawPath = ""
	return u.String()
}

func yahooJPDifferentProduct(page *url.URL, shop, code string, n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode || p.Data != "a" {
			continue
		}
		u, err := crawlURL(page, attribute(p, "href"))
		if err != nil || !strings.EqualFold(u.Hostname(), "store.shopping.yahoo.co.jp") {
			return false
		}
		// Some links omit .html; recognize either spelling without following it.
		if !strings.HasSuffix(strings.ToLower(u.Path), ".html") {
			u.Path += ".html"
		}
		otherShop, otherCode := yahooJPProductID(u)
		return otherShop != "" && (otherShop != shop || otherCode != code)
	}
	return false
}

func yahooJPProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	shop, code := yahooJPProductID(page)
	if doc == nil || shop == "" || code == "" {
		return result, errors.New("日本雅虎商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var main *html.Node
	var descriptions []*html.Node
	var ogURL, ogTitle string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" {
			switch strings.ToLower(firstNonEmpty(attribute(n, "property"), attribute(n, "name"))) {
			case "og:url":
				ogURL = attribute(n, "content")
			case "og:title":
				ogTitle = attribute(n, "content")
			}
		}
		id := attribute(n, "id")
		if id == "itmbasic" && main == nil {
			main = n
		}
		if id == "anchor-itemInfo" || id == "caption" || id == "CentItemAdditional" ||
			yahooJPHasClass(n, "CentItemAdditional") || yahooJPHasClass(n, "ProductExplanation") || id == "acMdUsrPrv" {
			descriptions = append(descriptions, n)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ogURL != "" {
		u, err := crawlURL(page, ogURL)
		otherShop, otherCode := yahooJPProductID(u)
		if err != nil || otherShop != shop || otherCode != code {
			return result, errors.New("日本雅虎页面与请求的商品不匹配")
		}
	}
	if ogTitle != "" {
		result.Title = ogTitle
	}
	seen := make(map[string]bool)
	invalid, limited := 0, false
	add := func(raw, kind string) {
		if ctx.Err() != nil || limited || raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
			return
		}
		original, ok := yahooJPImageURL(page, shop, raw)
		if !ok {
			invalid++
			return
		}
		large := yahooJPLargeImage(original)
		if seen[large] {
			return
		}
		if len(result.Items) >= maxYahooJPImages {
			limited = true
			return
		}
		seen[large] = true
		item := Item{URL: large, RequireImage: true, Metadata: map[string]string{"yahoo_jp_shop": shop, "yahoo_jp_item": code, "image_kind": kind}}
		if large != original {
			item.AlternateURLs = []string{original}
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", large); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	addImages := func(root *html.Node, kind string) {
		walkHTML(root, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" && (kind != "description" || !yahooJPDifferentProduct(page, shop, code, n)) {
				add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), kind)
			}
		})
	}
	// A static HTML response can represent noscript contents as text; parse it as markup only.
	addMarkup := func(n *html.Node, kind string) {
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode || len(child.Data) > maxPageBytes {
				continue
			}
			fragment, err := html.Parse(strings.NewReader(child.Data))
			if err != nil {
				invalid++
				continue
			}
			addImages(fragment, kind)
		}
	}
	mainStart := len(result.Items)
	var list *html.Node
	if main != nil {
		walkHTML(main, func(n *html.Node) {
			if list == nil && n.Type == html.ElementNode && yahooJPHasClass(n, "splide__list") {
				list = n
			}
		})
	}
	if list != nil {
		addImages(list, "main")
		walkHTML(list, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "noscript" {
				addMarkup(n, "main")
			}
		})
	}
	if len(result.Items) == mainStart {
		walkHTML(doc, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" &&
				((yahooJPHasClass(n, "BasicImage") && yahooJPHasClass(n, "ImageGallery_image")) || attribute(n, "data-imagegallery-url") != "") {
				add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), "main")
			}
		})
	}
	mainCount := len(result.Items)
	for _, description := range descriptions {
		addImages(description, "description")
		walkHTML(description, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "script" && attribute(n, "type") == "text/template" && attribute(n, "data-testid") == "freeSpaceData" {
				addMarkup(n, "description")
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if mainCount == 0 {
		problems = append(problems, errors.New("日本雅虎商品图库未返回图片，可能需要登录或安全验证"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("日本雅虎商品有 %d 张图片地址无效或不受支持", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("日本雅虎商品图片超过 %d 张，结果不完整", maxYahooJPImages))
	}
	return result, errors.Join(problems...)
}
