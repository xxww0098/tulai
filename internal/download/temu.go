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

var temuProductPath = regexp.MustCompile(`^/(?:[a-zA-Z]{2}(?:-[a-zA-Z]{2})?/)?[^/]{0,500}-g-([0-9]{15,20})\.html$`)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxTemuImages = 10000

func temuProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || strings.Contains(u.Path, "..") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	if h != "www.temu.com" && h != "temu.com" {
		return ""
	}
	m := temuProductPath.FindStringSubmatch(u.Path)
	if len(m) != 2 || strings.Trim(m[1], "0") == "" {
		return ""
	}
	for _, key := range []string{"goods_id", "goodsId"} {
		values := u.Query()[key]
		if len(values) > 1 || (len(values) == 1 && values[0] != m[1]) {
			return ""
		}
	}
	return m[1]
}
func isTemuProduct(u *url.URL) bool { return temuProductID(u) != "" }

type temuRedirectGetter struct {
	base Getter
	id   string
}

func (g *temuRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || temuProductID(current) != g.id {
		return nil, errors.New("TEMU 商品地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || temuProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("TEMU 商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || !sameOrigin(current, next) || temuProductID(next) != g.id {
			return nil, errors.New("TEMU 商品已跳转，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("TEMU 商品重定向循环")
		}
		current = next
	}
	return nil, errors.New("TEMU 商品重定向次数过多")
}

func discoverTemuProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := temuProductID(root)
	if getter == nil || id == "" {
		return result, errors.New("TEMU 商品地址或下载客户端无效")
	}
	page, pageGetter := *root, getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("TEMU 下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", "www.temu.com"
		pageGetter = &temuRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取 TEMU 商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || temuProductID(final) != id {
		return result, errors.New("TEMU 商品已跳转，可能需要登录或安全验证")
	}
	return temuProductPage(ctx, doc, final)
}

func temuImageURL(page *url.URL, raw string) (string, string, bool) {
	if len(raw) > 16384 {
		return "", "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || (u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) || u.Path == "" || u.Path == "/" {
		return "", "", false
	}
	h := strings.ToLower(u.Hostname())
	if !temuImageHost(h) {
		return "", "", false
	}
	u.Scheme, u.Host = "https", h
	alternate := ""
	if strings.HasPrefix(u.RawQuery, "imageView2/") && !strings.Contains(u.RawQuery, "&") {
		alternate = u.String()
		u.RawQuery = ""
	}
	return u.String(), alternate, true
}

func temuImageHost(host string) bool {
	return host == "img.kwcdn.com" || host == "img-eu.kwcdn.com"
}

type temuImage struct{ raw, kind, variant string }

func temuProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := temuProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("TEMU 商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var ogURL, ogTitle string
	var ogConflict bool
	var main, sku, detail []temuImage
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = strings.TrimSpace(nodeText(n))
		}
		if n.Data == "meta" {
			switch strings.ToLower(firstNonEmpty(attribute(n, "property"), attribute(n, "name"))) {
			case "og:url":
				value := attribute(n, "content")
				if ogURL != "" && ogURL != value {
					ogConflict = true
				}
				ogURL = value
			case "og:title":
				ogTitle = attribute(n, "content")
			}
		}
		if n.Data != "img" {
			return
		}
		var kind string
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Type != html.ElementNode {
				continue
			}
			classes := strings.Fields(attribute(p, "class"))
			if len(classes) == 0 {
				continue
			}
			for _, class := range classes {
				switch class {
				case "_2ftcdy74", "_2AOclWz7", "_2YJlRFSG":
					kind = "main"
				case "_2bzGqXzH":
					kind = "variant"
				case "_2jCYETGt":
					kind = "description"
				}
				if kind != "" {
					break
				}
			}
			if kind != "" {
				break
			}
		}
		switch kind {
		case "main":
			main = append(main, temuImage{raw: firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), kind: kind})
		case "variant":
			present := false
			for _, a := range n.Attr {
				if a.Key == "alt" {
					present = true
					break
				}
			}
			if present {
				sku = append(sku, temuImage{raw: attribute(n, "src"), kind: kind, variant: attribute(n, "alt")})
			}
		case "description":
			detail = append(detail, temuImage{raw: firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), kind: kind})
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ogConflict || ogURL == "" {
		return result, errors.New("TEMU 页面缺少可验证的商品标识，无法确认图片归属")
	}
	og, err := crawlURL(page, ogURL)
	if err != nil || og.User != nil || temuProductID(og) != id {
		return result, errors.New("TEMU 页面与请求的商品不匹配")
	}
	if ogTitle != "" {
		result.Title = ogTitle
	}
	seen := make(map[string]bool)
	var invalid, limited bool
	add := func(v temuImage) {
		if ctx.Err() != nil || limited || v.raw == "" || strings.HasPrefix(strings.ToLower(v.raw), "data:") {
			return
		}
		image, alternate, ok := temuImageURL(page, v.raw)
		if !ok {
			invalid = true
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxTemuImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"temu_goods_id": id, "image_kind": v.kind}}
		if alternate != "" {
			item.AlternateURLs = []string{alternate}
		}
		if chars := []rune(v.variant); len(chars) > 256 {
			v.variant = string(chars[:256])
		}
		if v.variant != "" {
			item.Metadata["variant"] = v.variant
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for _, v := range main {
		add(v)
	}
	mainCount := len(result.Items)
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
	if invalid {
		problems = append(problems, errors.New("TEMU 包含未验证来源的图片，已跳过"))
	}
	if mainCount == 0 {
		problems = append(problems, errors.New("TEMU 主图未加载，可能需要登录或安全验证"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("TEMU 商品超过 %d 张图片，结果不完整", maxTemuImages))
	}
	return result, errors.Join(problems...)
}
