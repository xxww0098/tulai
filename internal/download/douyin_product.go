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

var (
	douyinProductID         = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	douyinProductBackground = regexp.MustCompile(`(?i)(?:^|;)\s*background-image\s*:\s*url\(\s*(?:"([^"]*)"|'([^']*)'|([^'"\)\s]*))\s*\)`)
)

func douyinProductRef(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "haohuo.jinritemai.com") || u.Path != "/ecommerce/trade/detail/index.html" {
		return ""
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return ""
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["id"]) != 1 || !douyinProductID.MatchString(query.Get("id")) {
		return ""
	}
	id := query.Get("id")
	for _, productID := range query["product_id"] {
		if productID != id {
			return ""
		}
	}
	return id
}

func isDouyinProduct(u *url.URL) bool { return douyinProductRef(u) != "" }

func discoverDouyinProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := douyinProductRef(root)
	if getter == nil || id == "" {
		return result, errors.New("抖音商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || douyinProductRef(final) != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("抖音商品页面不可用或跳转到其他商品")
	}
	return douyinProductPage(ctx, doc, final)
}

func douyinProductImageURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "p3-aio.ecombdimg.com", "p6-aio.ecombdimg.com", "p9-aio.ecombdimg.com":
	default:
		return "", false
	}
	if !strings.HasPrefix(u.Path, "/obj/") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func douyinProductHasClass(n *html.Node, class string) bool {
	for _, name := range strings.Fields(attribute(n, "class")) {
		if name == class {
			return true
		}
	}
	return false
}

func douyinProductInClass(n *html.Node, class string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && douyinProductHasClass(p, class) {
			return true
		}
	}
	return false
}

// The current product frontend uses these specific rendered elements for the
// requested product's gallery and description. Other page images are ignored.
func douyinProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := douyinProductRef(page)
	if doc == nil || id == "" {
		return result, errors.New("抖音商品页面无效")
	}
	seen := map[string]bool{}
	add := func(raw string) {
		image, ok := douyinProductImageURL(raw)
		if !ok || seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"douyin_product_id": id}}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	blocked := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if douyinProductHasClass(n, "login-page") || douyinProductHasClass(n, "captcha_verify_bar") ||
			(douyinProductHasClass(n, "err-reason-text") && strings.Contains(nodeText(n), "安全风险")) {
			if !strings.Contains(strings.ReplaceAll(strings.ToLower(attribute(n, "style")), " ", ""), "display:none") {
				blocked = true
			}
		}
		if douyinProductHasClass(n, "title-info__text") {
			result.Title = firstNonEmpty(result.Title, nodeText(n))
		}
		if douyinProductHasClass(n, "head-figure__media-view__content") {
			if m := douyinProductBackground.FindStringSubmatch(attribute(n, "style")); len(m) == 4 {
				add(firstNonEmpty(m[1], m[2], m[3]))
			}
		}
		if n.Data == "img" && douyinProductInClass(n, "product-big-img-list") {
			add(attribute(n, "src"))
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if blocked {
		result.Items = nil
		return result, errors.New("抖音商品页面要求登录或安全验证，请在下载浏览器中完成后重试")
	}
	if len(result.Items) == 0 {
		return result, errors.New("抖音商品图片尚未加载，请在下载浏览器中打开商品页")
	}
	return result, errors.New("已取得页面中加载的商品图片，规格图或未加载的详情图可能不完整")
}
