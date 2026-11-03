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
	lightInTheBoxPath = regexp.MustCompile(`^/(?:en/)?p/[^/]{1,500}_p([1-9][0-9]{0,19})\.html$`)
	lightInTheBoxH1ID = regexp.MustCompile(`(?:^|\s)#([1-9][0-9]{0,19})\s*$`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxLightInTheBoxImages = 10000

func lightInTheBoxProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "www.lightinthebox.com" && host != "lightinthebox.com" {
		return ""
	}
	match := lightInTheBoxPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func isLightInTheBoxProduct(u *url.URL) bool { return lightInTheBoxProductID(u) != "" }

type lightInTheBoxRedirectGetter struct {
	base Getter
	id   string
}

func (g *lightInTheBoxRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || lightInTheBoxProductID(current) != g.id {
		return nil, errors.New("兰亭商品地址无效")
	}
	for i := 0; i < 6; i++ {
		resp, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || lightInTheBoxProductID(final) != g.id {
			resp.Body.Close()
			return nil, errors.New("兰亭商品响应地址无效")
		}
		if resp.StatusCode != 301 && resp.StatusCode != 302 && resp.StatusCode != 303 && resp.StatusCode != 307 && resp.StatusCode != 308 {
			return resp, nil
		}
		location := resp.Header.Get("Location")
		resp.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || !sameOrigin(current, next) || lightInTheBoxProductID(next) != g.id {
			return nil, errors.New("兰亭商品已跳转，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("兰亭商品重定向循环")
		}
		current = next
	}
	return nil, errors.New("兰亭商品重定向次数过多")
}

func discoverLightInTheBoxProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := lightInTheBoxProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || getter == nil {
		return result, errors.New("兰亭商品地址或下载客户端无效")
	}
	page, pageGetter := *root, getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("兰亭下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter = &lightInTheBoxRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, resp, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if resp != nil {
		resp.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		var status *HTTPStatusError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return result, fmt.Errorf("兰亭商品可能需要登录或安全验证: %w", err)
			case http.StatusTooManyRequests:
				return result, fmt.Errorf("兰亭请求受到限流，请稍后重试: %w", err)
			}
		}
		return result, fmt.Errorf("读取兰亭商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || lightInTheBoxProductID(final) != id {
		return result, errors.New("兰亭商品页面未加载或已跳转，可能需要登录或安全验证")
	}
	return lightInTheBoxProductPage(ctx, doc, final)
}

func lightInTheBoxImageURL(page *url.URL, raw string, sku bool) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || initial.User != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "litb-cgis.rightinthebox.com") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/images/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	if sku && u.RawQuery == "" {
		u.Path = strings.Replace(u.Path, "/images/170x170/", "/images/640x640/", 1)
		u.RawPath = ""
	}
	u.Host, u.Fragment = "litb-cgis.rightinthebox.com", ""
	return u.String(), true
}

func lightInTheBoxContainer(n *html.Node, name string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode {
			continue
		}
		if attribute(p, "id") == name {
			return true
		}
		for _, class := range strings.Fields(attribute(p, "class")) {
			if class == name {
				return true
			}
		}
	}
	return false
}

type lightInTheBoxImage struct{ raw, kind, variant, alt string }

func lightInTheBoxProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := lightInTheBoxProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("兰亭商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var h1Count int
	var title string
	var challenge bool
	var main, sku, detail []lightInTheBoxImage
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" {
			text := strings.ToLower(nodeText(n))
			if strings.Contains(text, "just a moment") || strings.Contains(text, "security check") || strings.Contains(text, "安全验证") {
				challenge = true
			}
		}
		if n.Data == "h1" {
			text := strings.TrimSpace(nodeText(n))
			if m := lightInTheBoxH1ID.FindStringSubmatch(text); len(m) == 2 {
				h1Count++
				if m[1] == id {
					title = strings.TrimSpace(text[:len(text)-len(m[0])])
				}
			}
		}
		if n.Data != "img" {
			return
		}
		switch {
		case lightInTheBoxContainer(n, "viewport"):
			main = append(main, lightInTheBoxImage{raw: attribute(n, "data-normal"), kind: "main", alt: attribute(n, "alt")})
		case lightInTheBoxContainer(n, "attr-item-container"):
			variant := ""
			if n.Parent != nil {
				variant = attribute(n.Parent, "data-content")
			}
			sku = append(sku, lightInTheBoxImage{raw: attribute(n, "src"), kind: "variant", variant: variant})
		case lightInTheBoxContainer(n, "new-desc-photo"):
			detail = append(detail, lightInTheBoxImage{raw: attribute(n, "src"), kind: "description"})
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if challenge {
		return result, errors.New("兰亭商品需要完成安全验证")
	}
	if h1Count != 1 || title == "" {
		return result, errors.New("兰亭页面缺少匹配的商品标识，无法确认图片归属")
	}
	result.Title = title
	seen := make(map[string]bool)
	var invalid, limited, hasMain, hasSKU bool
	add := func(v lightInTheBoxImage) {
		if ctx.Err() != nil || limited || v.raw == "" {
			return
		}
		if v.kind == "main" && strings.Join(strings.Fields(v.alt), " ") != title {
			invalid = true
			return
		}
		image, ok := lightInTheBoxImageURL(page, v.raw, v.kind == "variant")
		if !ok {
			invalid = true
			return
		}
		if v.kind == "main" {
			hasMain = true
		}
		if v.kind == "variant" {
			hasSKU = true
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxLightInTheBoxImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"lightinthebox_product_id": id, "image_kind": v.kind}}
		if v.kind == "variant" {
			if original, ok := lightInTheBoxImageURL(page, v.raw, false); ok && original != image {
				item.AlternateURLs = []string{original}
			}
		}
		if chars := []rune(v.variant); len(chars) > 256 {
			v.variant = string(chars[:256])
		}
		if v.variant != "" {
			item.Metadata["variant"] = v.variant
		}
		if ref := imageReferer("https://"+page.Hostname()+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for _, v := range main {
		add(v)
	}
	for _, v := range sku {
		add(v)
	}
	for _, v := range detail {
		add(v)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if !hasMain {
		problems = append(problems, errors.New("兰亭主图未加载或无法确认归属"))
	}
	if !hasSKU {
		problems = append(problems, errors.New("兰亭规格图未完整提供"))
	}
	problems = append(problems, errors.New("兰亭详情接口尚未验证，详情图可能不完整"))
	if invalid {
		problems = append(problems, errors.New("兰亭包含未验证来源或归属的图片，已跳过"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("兰亭商品超过 %d 张图片，结果不完整", maxLightInTheBoxImages))
	}
	return result, errors.Join(problems...)
}
