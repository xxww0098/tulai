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
	wsyIDPattern    = regexp.MustCompile(`^[0-9]{1,20}$`)
	wsyConfigMarker = regexp.MustCompile(`\bwindow\.g_config\s*=`)
	wsyConfigID     = regexp.MustCompile(`\bwindow\.g_config\s*=\s*\{\s*id\s*:\s*([0-9]+)`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxWSYImages = 10000

func wsyProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "/item.htm" ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	if h := strings.ToLower(u.Hostname()); h != "www.wsy.com" && h != "wsy.com" {
		return ""
	}
	values := u.Query()["id"]
	if len(values) != 1 || !wsyIDPattern.MatchString(values[0]) || strings.Trim(values[0], "0") == "" {
		return ""
	}
	return values[0]
}
func isWSYProduct(u *url.URL) bool { return wsyProductID(u) != "" }

type wsyRedirectGetter struct {
	base Getter
	id   string
}

func (g *wsyRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || wsyProductID(current) != g.id {
		return nil, errors.New("网商园商品地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || wsyProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("网商园商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || !sameOrigin(current, next) || wsyProductID(next) != g.id {
			return nil, errors.New("网商园商品已跳转，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("网商园商品重定向循环")
		}
		current = next
	}
	return nil, errors.New("网商园商品重定向次数过多")
}

func discoverWSYProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := wsyProductID(root)
	if getter == nil || id == "" {
		return result, errors.New("网商园商品地址或下载客户端无效")
	}
	page, pageGetter := *root, getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("网商园下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter = &wsyRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取网商园商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || wsyProductID(final) != id {
		return result, errors.New("网商园商品已跳转，可能需要登录或安全验证")
	}
	return wsyProductPage(ctx, doc, final)
}

func wsyImageURL(page *url.URL, raw string, sku bool) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "img.alicdn.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) || u.Path == "" || u.Path == "/" {
		return "", false
	}
	if sku && u.RawQuery == "" {
		if i := strings.Index(u.Path, ".jpg_"); i >= 0 {
			u.Path, u.RawPath = u.Path[:i+4], ""
		}
	}
	u.Scheme, u.Host = "https", "img.alicdn.com"
	return u.String(), true
}

func wsyHasClass(n *html.Node, class string) bool {
	for _, x := range strings.Fields(attribute(n, "class")) {
		if x == class {
			return true
		}
	}
	return false
}

type wsyImage struct{ raw, kind, variant string }

func wsyProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := wsyProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("网商园商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var main, sku, detail []wsyImage
	var stateBad, stateIDFound, wrongID, challenge, skuKnown bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" {
			result.Title = strings.TrimSpace(nodeText(n))
			if strings.Contains(result.Title, "安全验证") {
				challenge = true
			}
		}
		if n.Data == "script" {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type != html.TextNode || len(c.Data) > maxPageBytes {
					continue
				}
				if wsyConfigMarker.MatchString(c.Data) {
					m := wsyConfigID.FindStringSubmatch(c.Data)
					if len(m) != 2 {
						stateBad = true
					} else {
						stateIDFound = true
						if m[1] != id {
							wrongID = true
						}
					}
				}
			}
		}
		if n.Data == "a" && n.Parent != nil && wsyHasClass(n.Parent, "item-mb") {
			if name := strings.TrimSpace(nodeText(n)); name != "" {
				result.Title = name
			}
		}
		if n.Data == "a" {
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Type == html.ElementNode && p.Data == "ol" && wsyHasClass(p, "J_UlThumb") && wsyHasClass(p, "tb-thumb") && wsyHasClass(p, "tb-clearfix") && !wsyHasClass(p, "tb-video") {
					main = append(main, wsyImage{raw: attribute(n, "href"), kind: "main"})
					break
				}
			}
		}
		if n.Data == "div" && attribute(n, "data-img") != "" {
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Type == html.ElementNode && attribute(p, "id") == "J_TSaleProp" {
					skuKnown = true
					sku = append(sku, wsyImage{raw: attribute(n, "data-img"), kind: "variant", variant: attribute(n, "title")})
					break
				}
			}
		}
		if n.Data == "textarea" && wsyHasClass(n, "J_DivItemDesc2s") {
			var text strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					text.WriteString(c.Data)
				}
			}
			fragment, err := html.Parse(strings.NewReader(text.String()))
			if err != nil {
				stateBad = true
			} else {
				walkHTML(fragment, func(x *html.Node) {
					if x.Type == html.ElementNode && x.Data == "img" {
						detail = append(detail, wsyImage{raw: firstNonEmpty(attribute(x, "data-original"), attribute(x, "src")), kind: "description"})
					}
				})
			}
		}
		if n.Data == "img" && attribute(n, "data-original") != "" {
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Type == html.ElementNode && attribute(p, "id") == "J_DivItemDesc" {
					detail = append(detail, wsyImage{raw: attribute(n, "data-original"), kind: "description"})
					break
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if challenge {
		return result, errors.New("网商园需要完成安全验证")
	}
	if wrongID {
		return result, errors.New("网商园页面与请求的商品不匹配")
	}
	if !stateIDFound {
		return result, errors.New("网商园页面缺少可验证的商品标识，无法确认图片归属")
	}
	seen := make(map[string]bool)
	var invalid, limited, signed, detailFound bool
	add := func(v wsyImage) {
		if ctx.Err() != nil || limited || v.raw == "" || strings.HasPrefix(strings.ToLower(v.raw), "data:") {
			return
		}
		u, _ := url.Parse(v.raw)
		if v.kind == "variant" && u != nil && u.RawQuery != "" && strings.Contains(u.Path, ".jpg_") {
			signed = true
		}
		image, ok := wsyImageURL(page, v.raw, v.kind == "variant")
		if !ok {
			invalid = true
			return
		}
		if v.kind == "description" {
			detailFound = true
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxWSYImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"wsy_item_id": id, "image_kind": v.kind}}
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
	if stateBad {
		problems = append(problems, errors.New("网商园商品信息无法完整解析"))
	}
	if invalid {
		problems = append(problems, errors.New("网商园包含未验证来源的图片，已跳过"))
	}
	if mainCount == 0 {
		problems = append(problems, errors.New("网商园主图未加载，可能需要在浏览器打开商品"))
	}
	if !skuKnown {
		problems = append(problems, errors.New("网商园规格图未加载，结果可能不完整"))
	}
	if !detailFound {
		problems = append(problems, errors.New("网商园详情图未加载，结果可能不完整"))
	}
	if signed {
		problems = append(problems, errors.New("网商园带参数的规格缩略图保留原地址，原图未验证"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("网商园商品超过 %d 张图片，结果不完整", maxWSYImages))
	}
	return result, errors.Join(problems...)
}
