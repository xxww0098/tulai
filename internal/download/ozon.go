package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	ozonProductPath = regexp.MustCompile(`^/product/(?:[^/]*-)?([0-9]{6,20})/?$`)
	ozonCDNHost     = regexp.MustCompile(`^(?:ir(?:-[0-9]{1,2})?|cdn[1-9][0-9]?)\.ozone\.ru$`)
	ozonCDNPath     = regexp.MustCompile(`^/s3/multimedia(?:-[A-Za-z0-9]+){0,3}/[^/]+(?:/[^/]+)*$`)
	errOzonRedirect = errors.New("Ozon 商品页面重定向循环，可能需要在浏览器中完成验证")
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxOzonImages = 10000

func ozonProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(!strings.EqualFold(u.Hostname(), "ozon.ru") && !strings.EqualFold(u.Hostname(), "www.ozon.ru")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		strings.Contains(u.Path, "..") {
		return ""
	}
	match := ozonProductPath.FindStringSubmatch(u.Path)
	if len(match) != 2 || strings.Trim(match[1], "0") == "" {
		return ""
	}
	return match[1]
}

func isOzonProduct(u *url.URL) bool { return ozonProductID(u) != "" }

type ozonRedirectGetter struct {
	base Getter
	id   string
}

func (g *ozonRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || ozonProductID(current) != g.id {
		return nil, errors.New("Ozon 商品请求地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || final.Path != current.Path || ozonProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("Ozon 商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || ozonProductID(next) != g.id {
			return nil, errors.New("Ozon 商品跳转地址无效，可能需要安全验证")
		}
		if sameOrigin(current, next) && current.Path == next.Path {
			return nil, errOzonRedirect
		}
		current = next
	}
	return nil, errOzonRedirect
}

func discoverOzonProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := ozonProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("Ozon 商品地址或下载客户端无效")
	}
	page := *root
	pageGetter := getter
	validatedRedirects := false
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("Ozon 下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter, validatedRedirects = &ozonRedirectGetter{base: &clone, id: id}, true
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取 Ozon 商品失败: %w", err)
	}
	if doc == nil || final == nil || ozonProductID(final) != id || (!sameOrigin(&page, final) && !validatedRedirects) {
		return result, errors.New("Ozon 商品已跳转，可能需要登录或安全验证")
	}
	return ozonProductPage(ctx, doc, final)
}

func ozonImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !ozonCDNHost.MatchString(strings.ToLower(u.Hostname())) || !ozonCDNPath.MatchString(u.Path) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return "", false
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String(), true
}

func ozonFullImage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery != "" {
		return raw
	}
	path := strings.Replace(u.Path, "/c100/wc140/", "/", 1)
	path = strings.Replace(path, "/wc50/", "/", 1)
	if strings.HasSuffix(strings.ToLower(path), ".jpg.webp") {
		path = path[:len(path)-len(".webp")]
	}
	if path != u.Path {
		u.Path, u.RawPath = path, ""
	}
	return u.String()
}

type ozonVariant struct {
	Data struct {
		Picture        string `json:"picture"`
		SearchableText string `json:"searchableText"`
	} `json:"data"`
}

type ozonAspects struct {
	Aspects []struct {
		Variants []ozonVariant `json:"variants"`
	} `json:"aspects"`
}

func ozonProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := ozonProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("Ozon 商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var gallery *html.Node
	var states []string
	var ogURL string
	var richContent bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "h1" {
			for p := n.Parent; p != nil; p = p.Parent {
				if attribute(p, "data-widget") == "webProductHeading" {
					result.Title = nodeText(n)
					break
				}
			}
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			ogURL = attribute(n, "content")
		}
		if gallery == nil && attribute(n, "data-widget") == "webGallery" {
			gallery = n
		}
		if n.Data == "div" && strings.HasPrefix(attribute(n, "id"), "state-webAspects") {
			states = append(states, attribute(n, "data-state"))
		}
		switch attribute(n, "data-widget") {
		case "webRichContent", "webProductDescription":
			richContent = true
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ogURL != "" {
		u, err := crawlURL(page, ogURL)
		if err != nil || ozonProductID(u) != id {
			return result, errors.New("Ozon 页面与请求的商品不匹配")
		}
	}
	seen := make(map[string]bool)
	invalid, limited := 0, false
	add := func(raw, kind, variant string) bool {
		if ctx.Err() != nil || limited || raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
			return false
		}
		original, ok := ozonImageURL(page, raw)
		if !ok {
			invalid++
			return false
		}
		full := ozonFullImage(original)
		if seen[full] {
			return true
		}
		if len(result.Items) >= maxOzonImages {
			limited = true
			return false
		}
		seen[full] = true
		item := Item{URL: full, RequireImage: true, Metadata: map[string]string{"ozon_product_id": id, "image_kind": kind}}
		if len(variant) > 512 {
			variant = variant[:512]
		}
		if variant != "" {
			item.Metadata["variant"] = variant
		}
		if full != original {
			item.AlternateURLs = []string{original}
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", full); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
		return true
	}
	var fallback []string
	if gallery != nil {
		walkHTML(gallery, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "img" {
				return
			}
			raw := firstNonEmpty(attribute(n, "src"), attribute(n, "data-src"))
			if strings.Contains(raw, "/wc50/") {
				add(raw, "main", "")
			} else if raw != "" && !strings.HasPrefix(strings.ToLower(raw), "data:") {
				fallback = append(fallback, raw)
			}
		})
	}
	mainCount := len(result.Items)
	if mainCount == 0 {
		for _, raw := range fallback {
			add(raw, "main", "")
		}
		mainCount = len(result.Items)
	}
	stateValid, stateInvalid := false, false
	for _, raw := range states {
		var state ozonAspects
		if len(raw) > maxPageBytes || json.Unmarshal([]byte(raw), &state) != nil || state.Aspects == nil {
			stateInvalid = true
			continue
		}
		stateValid = true
		for _, aspect := range state.Aspects {
			for _, variant := range aspect.Variants {
				add(variant.Data.Picture, "sku", variant.Data.SearchableText)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if mainCount == 0 {
		problems = append(problems, errors.New("Ozon 商品图库未返回图片，可能需要登录或安全验证"))
	}
	if !stateValid || stateInvalid {
		problems = append(problems, errors.New("Ozon 规格图片数据未完整返回"))
	}
	if richContent {
		problems = append(problems, errors.New("Ozon 商品详情富文本图片尚未确认完整"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("Ozon 商品有 %d 张图片地址无效或不受支持", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("Ozon 商品图片超过 %d 张，结果不完整", maxOzonImages))
	}
	return result, errors.Join(problems...)
}
