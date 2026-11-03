package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	madeInChinaShopHost     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.en\.made-in-china\.com$`)
	madeInChinaShopPath     = regexp.MustCompile(`^/product/([A-Za-z0-9]{8,32})/[^/]{1,500}\.html$`)
	madeInChinaWWWPath      = regexp.MustCompile(`^/product/[^/]{1,500}-([A-Za-z0-9]{8,32})\.html$`)
	errMadeInChinaChallenge = errors.New("中国制造网需要完成安全验证")
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxMadeInChinaImages = 10000

func madeInChinaProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		strings.Contains(u.Path, "..") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	var match []string
	switch {
	case madeInChinaShopHost.MatchString(host):
		match = madeInChinaShopPath.FindStringSubmatch(u.Path)
	case host == "www.made-in-china.com" || host == "made-in-china.com":
		match = madeInChinaWWWPath.FindStringSubmatch(u.Path)
	}
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func isMadeInChinaProduct(u *url.URL) bool { return madeInChinaProductID(u) != "" }

type madeInChinaRedirectGetter struct {
	base Getter
	id   string
}

func (g *madeInChinaRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || madeInChinaProductID(current) != g.id {
		return nil, errors.New("中国制造网商品请求地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || final.Path != current.Path || madeInChinaProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("中国制造网商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err == nil && next.User == nil && next.Scheme == "https" && strings.EqualFold(next.Hostname(), "captcha.made-in-china.com") &&
			(next.Port() == "" || next.Port() == "443") && next.Path == "/verification.html" {
			return nil, errMadeInChinaChallenge
		}
		if err != nil || next.Scheme != "https" || madeInChinaProductID(next) != g.id {
			return nil, errors.New("中国制造网商品跳转地址无效，可能需要安全验证")
		}
		current = next
	}
	return nil, errors.New("中国制造网商品重定向次数过多")
}

func discoverMadeInChinaProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := madeInChinaProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("中国制造网商品地址或下载客户端无效")
	}
	page := *root
	pageGetter := getter
	validatedRedirects := false
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("中国制造网下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter, validatedRedirects = &madeInChinaRedirectGetter{base: &clone, id: id}, true
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取中国制造网商品失败: %w", err)
	}
	if doc == nil || final == nil || madeInChinaProductID(final) != id || (!sameOrigin(&page, final) && !validatedRedirects) {
		return result, errors.New("中国制造网商品已跳转，可能需要登录或安全验证")
	}
	return madeInChinaProductPage(ctx, doc, final)
}

func madeInChinaHasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func madeInChinaImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "image.made-in-china.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path == "" || u.Path == "/" {
		return "", false
	}
	u.Scheme, u.Host = "https", "image.made-in-china.com"
	return u.String(), true
}

func madeInChinaProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := madeInChinaProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("中国制造网商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var galleries, descriptions []*html.Node
	var ogURL, ogTitle string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "h1" && madeInChinaHasClass(n, "sr-proMainInfo-baseInfoH1") {
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
		if madeInChinaHasClass(n, "swiper-wrapper") {
			for parent := n.Parent; parent != nil; parent = parent.Parent {
				if madeInChinaHasClass(parent, "sr-proMainInfo-slide") || madeInChinaHasClass(parent, "J-pic-list-container") {
					galleries = append(galleries, n)
					break
				}
			}
		}
		if madeInChinaHasClass(n, "sr-layout-content") && madeInChinaHasClass(n, "detail-desc") {
			descriptions = append(descriptions, n)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ogURL != "" {
		u, err := crawlURL(page, ogURL)
		if err != nil || madeInChinaProductID(u) != id {
			return result, errors.New("中国制造网页面与请求的商品不匹配")
		}
	}
	if result.Title == "" && ogTitle != "" {
		result.Title = ogTitle
	}
	seen := make(map[string]bool)
	invalid, preview, limited := 0, 0, false
	add := func(raw, kind, fallback string) {
		if ctx.Err() != nil || limited || raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
			return
		}
		image, ok := madeInChinaImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxMadeInChinaImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"madeinchina_product_id": id, "image_kind": kind}}
		if candidate, valid := madeInChinaImageURL(page, fallback); valid && candidate != image {
			item.AlternateURLs = []string{candidate}
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for _, gallery := range galleries {
		count := 0
		for slide := gallery.FirstChild; slide != nil; slide = slide.NextSibling {
			if slide.Type != html.ElementNode || slide.Data != "div" || attribute(slide, "fsrc") == "" {
				continue
			}
			count++
			fallback := ""
			walkHTML(slide, func(n *html.Node) {
				if fallback == "" && n.Type == html.ElementNode && n.Data == "img" {
					fallback = firstNonEmpty(attribute(n, "data-src"), attribute(n, "src"))
				}
			})
			add(attribute(slide, "fsrc"), "main", fallback)
		}
		if count > 0 {
			break
		}
	}
	mainCount := len(result.Items)
	for _, description := range descriptions {
		walkHTML(description, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "img" {
				return
			}
			raw := attribute(n, "data-original")
			if raw == "" {
				candidate := firstNonEmpty(attribute(n, "data-src"), attribute(n, "src"))
				if _, ok := madeInChinaImageURL(page, candidate); ok {
					raw = candidate
					preview++
				}
			}
			add(raw, "description", "")
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if mainCount == 0 {
		problems = append(problems, errors.New("中国制造网商品图库未返回图片，可能需要登录或安全验证"))
	}
	if len(descriptions) == 0 {
		problems = append(problems, errors.New("中国制造网商品描述区未返回，详情图片可能不完整"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("中国制造网商品有 %d 张图片地址无效或不受支持", invalid))
	}
	if preview > 0 {
		problems = append(problems, fmt.Errorf("中国制造网商品有 %d 张详情图片只有预览地址", preview))
	}
	if limited {
		problems = append(problems, fmt.Errorf("中国制造网商品图片超过 %d 张，结果不完整", maxMadeInChinaImages))
	}
	return result, errors.Join(problems...)
}
