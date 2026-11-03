package download

import (
	"context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var jjshouseProductPath = regexp.MustCompile(`^/(?:[a-z]{2}/)?product/[^/]{1,500}-g([0-9]{1,20})/?$`)
var jjshouseCDN = regexp.MustCompile(`^cdn-[0-9]{1,2}\.jjshouse\.com$`)

func jjshouseProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(!strings.EqualFold(u.Hostname(), "www.jjshouse.com") && !strings.EqualFold(u.Hostname(), "jjshouse.com")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	m := jjshouseProductPath.FindStringSubmatch(u.Path)
	if len(m) != 2 || strings.Trim(m[1], "0") == "" {
		return ""
	}
	return m[1]
}

func isJJsHouseProduct(u *url.URL) bool { return jjshouseProductID(u) != "" }

func discoverJJsHouse(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := jjshouseProductID(page)
	if getter == nil || id == "" {
		return result, errors.New("JJ's House 商品地址无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || jjshouseProductID(final) != id || (page.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("JJ's House 商品已跳转，可能需要登录或安全验证")
	}
	return jjshouseProductPage(ctx, doc, final)
}

func jjshouseImage(page *url.URL, raw string) (string, string) {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil ||
		(!jjshouseCDN.MatchString(strings.ToLower(u.Hostname())) && !strings.EqualFold(u.Hostname(), "www.jjshouse.com") && !strings.EqualFold(u.Hostname(), "jjshouse.com")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") ||
		(!strings.HasPrefix(u.Path, "/upimg/l/") && !strings.HasPrefix(u.Path, "/upimg/jjshouse/s1140/")) {
		return "", ""
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	original := u.String()
	if u.RawQuery == "" && u.RawPath == "" && strings.HasPrefix(u.Path, "/upimg/l/") {
		u.Path = strings.Replace(u.Path, "/upimg/l/", "/upimg/jjshouse/s1140/", 1)
	}
	if u.String() == original {
		return original, ""
	}
	return u.String(), original
}

func jjshouseClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

// shortcut: only the selected color is loaded; traverse variants when the site exposes a stable interface.
func jjshouseProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := jjshouseProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("JJ's House 商品页面无效")
	}
	var galleries []*html.Node
	var canonical string
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			canonical = attribute(n, "content")
		}
		if jjshouseClass(n, "base-image") && jjshouseClass(n, "goods-gallery-thumbs__img") {
			galleries = append(galleries, n)
		}
	})
	if canonical != "" {
		u, err := crawlURL(page, canonical)
		if err != nil || jjshouseProductID(u) != id {
			return result, errors.New("JJ's House 页面与请求的商品不匹配")
		}
	}
	seen := map[string]bool{}
	for _, gallery := range galleries {
		walkHTML(gallery, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "img" || ctx.Err() != nil {
				return
			}
			raw := firstNonEmpty(attribute(n, "data-src"), attribute(n, "src"))
			image, alternate := jjshouseImage(page, raw)
			if image == "" || seen[image] {
				return
			}
			seen[image] = true
			item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"jjshouse_product_id": id}}
			if alternate != "" {
				item.AlternateURLs = []string{alternate}
			}
			if referer := imageReferer(page.String(), image); referer != "" {
				item.Headers = map[string]string{"Referer": referer}
			}
			result.Items = append(result.Items, item)
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("JJ's House 商品图片尚未加载，可能需要登录或安全验证")
	}
	return result, errors.New("JJ's House 当前仅采集已选颜色，其他颜色可能不完整")
}
