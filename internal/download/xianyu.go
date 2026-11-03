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

var xianyuNumericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func xianyuHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80") {
		return false
	}
	return strings.EqualFold(u.Hostname(), "www.goofish.com") || strings.EqualFold(u.Hostname(), "goofish.com")
}

func xianyuItemID(u *url.URL) string {
	if !xianyuHost(u) || u.Path != "/item" {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["id"]) != 1 || !xianyuNumericID.MatchString(q.Get("id")) {
		return ""
	}
	return q.Get("id")
}

func isXianyuItem(u *url.URL) bool { return xianyuItemID(u) != "" }

func isXianyuLogin(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return false
	}
	return (xianyuHost(u) && u.Path == "/login") || (strings.EqualFold(u.Hostname(), "passport.goofish.com") && u.Path == "/mini_login.htm")
}

func discoverXianyu(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := xianyuItemID(root)
	if getter == nil || id == "" {
		return result, errors.New("闲鱼商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if isXianyuLogin(final) {
		return result, errors.New("闲鱼要求登录，请在下载浏览器中完成登录后重试")
	}
	if doc == nil || xianyuItemID(final) != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("闲鱼商品页面不可用或跳转到其他商品")
	}
	return xianyuItemPage(ctx, doc, final)
}

func xianyuClassPrefix(n *html.Node, prefix string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, class := range strings.Fields(attribute(n, "class")) {
		if strings.HasPrefix(class, prefix) {
			return true
		}
	}
	return false
}

func xianyuInClass(n, root *html.Node, prefix string) bool {
	for p := n.Parent; p != nil && p != root; p = p.Parent {
		if xianyuClassPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func xianyuImageURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "img.alicdn.com") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/bao/uploaded/") ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") || u.Path == "/bao/uploaded/" {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

// The public frontend builds these links from itemDetailDO.trackParams.itemId.
// They bind the product gallery to the current URL even when the page is a SPA.
func xianyuLinkID(page *url.URL, raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u = page.ResolveReference(u)
	if !xianyuHost(u) || (page.Scheme == "https" && u.Scheme != "https") {
		return ""
	}
	switch u.Path {
	case "/im", "/create-order", "/create-order-yhb":
	default:
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["itemId"]) != 1 || !xianyuNumericID.MatchString(q.Get("itemId")) {
		return ""
	}
	return q.Get("itemId")
}

// Reference script 144 reads the product description and Ant Image preview
// group. The frontend maps this group from itemDO.imageInfos[].url. Only use
// it inside a product container with an independently matching item ID.
func xianyuItemPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := xianyuItemID(page)
	if doc == nil || id == "" {
		return result, errors.New("闲鱼商品页面无效")
	}
	var containers []*html.Node
	var blocked bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if xianyuClassPrefix(n, "item-main-container--") {
			containers = append(containers, n)
		}
		if xianyuClassPrefix(n, "error-container--") {
			t := nodeText(n)
			if strings.Contains(t, "非法访问") || strings.Contains(t, "安全验证") || strings.Contains(t, "请先登录") {
				blocked = true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if blocked {
		return result, errors.New("闲鱼页面要求登录或安全验证，请在下载浏览器中完成后重试")
	}
	if len(containers) != 1 {
		return result, errors.New("闲鱼商品尚未加载或页面要求登录，请在下载浏览器中打开商品页")
	}
	container := containers[0]
	var found, mismatch, rejected bool
	walkHTML(container, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "a" {
			if linkID := xianyuLinkID(page, attribute(n, "href")); linkID != "" {
				if linkID == id {
					found = true
				} else {
					mismatch = true
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if mismatch || !found {
		return result, errors.New("闲鱼页面中的商品编号无法确认，无法安全采集图片")
	}
	seen := make(map[string]bool)
	walkHTML(container, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if result.Title == "" && xianyuClassPrefix(n, "desc--") && xianyuInClass(n, container, "main--") {
			if title := []rune(strings.TrimSpace(nodeText(n))); len(title) > 0 {
				if len(title) > 160 {
					title = title[:160]
				}
				result.Title = string(title)
			}
		}
		if n.Data != "img" || !xianyuClassPrefix(n, "ant-image-img") || !xianyuInClass(n, container, "item-main-window-carousel--") {
			return
		}
		raw := attribute(n, "src")
		if raw == "" {
			return
		}
		image, ok := xianyuImageURL(raw)
		if !ok {
			rejected = true
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= 1000 {
			rejected = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"xianyu_item_id": id}}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("闲鱼尚未提供可确认归属的商品图片，请在下载浏览器中打开商品页")
	}
	if rejected {
		return result, errors.New("闲鱼部分商品图片地址未通过校验，图片可能不完整")
	}
	return result, errors.New("已取得当前商品预览图片；站点可能提供压缩图，未加载的图片可能不完整")
}
