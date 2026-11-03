package download

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	vipDesktopPath = regexp.MustCompile(`^/detail-([0-9]{1,24})-([0-9]{1,24})\.html$`)
	vipMobilePath  = regexp.MustCompile(`^/product-([0-9]{1,24})-([0-9]{1,24})\.html$`)
	vipThumbSuffix = regexp.MustCompile(`_(?:[0-9]{1,5}x[0-9]{1,5}_[0-9]{1,3}|[0-9]{1,5}_[0-9]{1,5})\.jpg$`)
)

type vipProduct struct{ brand, goods string }

// The reference converts the old mobile path to the corresponding desktop path.
func vipProductOf(page *url.URL) vipProduct {
	if page == nil || page.User != nil || (page.Scheme != "http" && page.Scheme != "https") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) ||
		strings.Contains(page.EscapedPath(), "%") {
		return vipProduct{}
	}
	var match []string
	switch strings.ToLower(page.Hostname()) {
	case "detail.vip.com":
		match = vipDesktopPath.FindStringSubmatch(page.Path)
	case "m.vip.com":
		match = vipMobilePath.FindStringSubmatch(page.Path)
	}
	if len(match) != 3 || strings.Trim(match[1], "0") == "" || strings.Trim(match[2], "0") == "" {
		return vipProduct{}
	}
	return vipProduct{brand: match[1], goods: match[2]}
}

func isVIPProduct(page *url.URL) bool { return vipProductOf(page).goods != "" }

func discoverVIPProduct(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := vipProductOf(page)
	if getter == nil || id.goods == "" {
		return result, errors.New("唯品会商品地址或下载客户端无效")
	}
	// The reference removes the query and fetches the desktop page even for a mobile URL.
	target := &url.URL{Scheme: page.Scheme, Host: "detail.vip.com", Path: "/detail-" + id.brand + "-" + id.goods + ".html"}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: target.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || vipProductOf(final) != id || (target.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("唯品会商品页面不可用或跳转到其他商品，可能需要登录或安全验证")
	}
	return vipCollect(ctx, getter, final, doc)
}

func vipImageURL(page *url.URL, raw string, original bool) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || strings.Contains(parsed.Path, "..") ||
		strings.Contains(strings.ToLower(parsed.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(parsed.EscapedPath()), "%5c") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "a.vpimg1.com", "a.vpimg2.com", "a.vpimg3.com", "a.vpimg4.com":
	case "a.appsimg.com":
		if !strings.HasPrefix(u.Path, "/upload/merchandise/") {
			return "", false
		}
	case "h2.appsimg.com":
		// This CDN uses the first path component as its origin; never let a
		// page supply another proxied host.
		if !strings.HasPrefix(u.Path, "/a.appsimg.com/upload/merchandise/") {
			return "", false
		}
	default:
		return "", false
	}
	if !strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	u.Scheme, u.Host = "https", host
	// Reference replaces the thumbnail suffix with .jpg; only rewrite the
	// verified size suffix in a filename, not underscores in arbitrary paths.
	if original && u.RawQuery == "" {
		base := u.Path[strings.LastIndex(u.Path, "/")+1:]
		if loc := vipThumbSuffix.FindStringIndex(base); loc != nil {
			prefix := strings.LastIndex(u.Path, "/") + 1
			u.Path = u.Path[:prefix+loc[0]] + ".jpg"
			u.RawPath = ""
		}
	}
	return u.String(), true
}

func vipImageKind(n *html.Node) string {
	for current := n; current != nil; current = current.Parent {
		for _, class := range strings.Fields(attribute(current, "class")) {
			switch class {
			case "color-list":
				return "sku"
			case "dc-img-detail":
				return "detail"
			case "pic-slider-items", "pic-sliderwrap":
				return "main"
			}
		}
	}
	return ""
}

// vipCollect accepts either fetched HTML or the user's currently rendered DOM.
func vipCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := vipProductOf(page)
	if getter == nil || id.goods == "" || doc == nil {
		return result, errors.New("唯品会商品页面或下载客户端无效")
	}
	seen := make(map[string]bool)
	var main, detail, invalid, limited, unmodifiedThumbnail bool
	var fallbackTitle string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && fallbackTitle == "" {
			fallbackTitle = nodeText(n)
		}
		for _, class := range strings.Fields(attribute(n, "class")) {
			if class == "pib-title-detail" && result.Title == "" {
				result.Title = firstNonEmpty(attribute(n, "title"), nodeText(n))
			}
		}
		if n.Data != "img" {
			return
		}
		kind := vipImageKind(n)
		if kind == "" {
			return
		}
		keys := []string{"data-original"}
		if kind == "detail" {
			keys = append(keys, "data-src", "src")
		}
		for _, key := range keys {
			raw := attribute(n, key)
			if raw == "" || !possibleImage(raw) {
				continue
			}
			image, ok := vipImageURL(page, raw, kind != "detail")
			if !ok {
				invalid = true
				continue
			}
			u, _ := url.Parse(image)
			if kind != "detail" && u.RawQuery != "" && vipThumbSuffix.MatchString(u.Path[strings.LastIndex(u.Path, "/")+1:]) {
				unmodifiedThumbnail = true
			}
			if kind == "main" {
				main = true
			} else if kind == "detail" {
				detail = true
			}
			if !seen[image] {
				if len(result.Items) >= 10000 {
					limited = true
					return
				}
				seen[image] = true
				metadata := map[string]string{"vip_brand_id": id.brand, "vip_goods_id": id.goods, "image_kind": kind}
				if alt := attribute(n, "alt"); alt != "" {
					metadata["title"] = alt
				}
				item := Item{URL: image, RequireImage: true, Metadata: metadata}
				if ref := imageReferer(page.String(), image); ref != "" {
					item.Headers = map[string]string{"Referer": ref}
				}
				result.Items = append(result.Items, item)
			}
			return
		}
	})
	if result.Title == "" {
		result.Title = fallbackTitle
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var issues []error
	if !main {
		issues = append(issues, errors.New("未找到唯品会商品主图，页面可能尚未加载或需要登录、安全验证"))
	}
	if !detail {
		issues = append(issues, errors.New("未读取唯品会商品详情图，结果可能不完整"))
	}
	if invalid {
		issues = append(issues, errors.New("唯品会商品包含不受支持的图片地址，结果可能不完整"))
	}
	if unmodifiedThumbnail {
		issues = append(issues, errors.New("部分唯品会缩略图包含查询参数，未改写为原图"))
	}
	if limited {
		issues = append(issues, errors.New("唯品会商品图片超过 10000 张上限，结果不完整"))
	}
	return result, errors.Join(issues...)
}
