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

var (
	ebayItemPath  = regexp.MustCompile(`^/itm/(?:[^/]+/)?([0-9]{9,15})/?$`)
	ebayThumbSize = regexp.MustCompile(`-l500\.(jpg|jpeg|png|webp)$`)
)

// shortcut: bound anomalously large listings until the limit is configurable.
const maxEbayImages = 10000

var ebayMarkets = map[string]bool{
	"ebay.com": true, "ebay.co.uk": true, "ebay.de": true, "ebay.ca": true,
	"ebay.com.au": true, "ebay.fr": true, "ebay.it": true, "ebay.es": true,
	"ebay.at": true, "ebay.ch": true, "ebay.ie": true, "ebay.nl": true,
	"ebay.be": true, "ebay.pl": true, "ebay.com.hk": true, "ebay.com.my": true,
	"ebay.ph": true, "ebay.com.sg": true, "ebay.com.tw": true,
}

func ebayItemID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.HasPrefix(host, "www.") {
		host = strings.TrimPrefix(host, "www.")
	}
	if host == "cafr.ebay.ca" {
		host = "ebay.ca"
	}
	if host == "benl.ebay.be" || host == "befr.ebay.be" {
		host = "ebay.be"
	}
	if !ebayMarkets[host] {
		return ""
	}
	match := ebayItemPath.FindStringSubmatch(u.Path)
	if len(match) != 2 || strings.Trim(match[1], "0") == "" {
		return ""
	}
	return match[1]
}

func isEbayProduct(u *url.URL) bool { return ebayItemID(u) != "" }

func discoverEbayProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := ebayItemID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("eBay 商品地址或下载客户端无效")
	}
	page := *root
	if _, ok := getter.(*HTTPGetter); ok {
		host := strings.ToLower(page.Hostname())
		if ebayMarkets[host] {
			host = "www." + host
		}
		page.Scheme, page.Host = "https", host
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取 eBay 商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || ebayItemID(final) != id {
		return result, errors.New("eBay 商品已跳转，可能需要登录或安全验证")
	}
	return ebayCollect(ctx, getter, final, doc)
}

func ebayImageURL(page *url.URL, raw string) (string, bool) {
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "i.ebayimg.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		(!strings.HasPrefix(u.Path, "/images/") && !strings.HasPrefix(u.Path, "/thumbs/images/")) {
		return "", false
	}
	u.Scheme, u.Host = "https", "i.ebayimg.com"
	return u.String(), true
}

func ebayDescriptionURL(page *url.URL, raw, item string) (*url.URL, error) {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(!strings.EqualFold(u.Hostname(), "itm.ebaydesc.com") && !strings.EqualFold(u.Hostname(), "vi.vipr.ebaydesc.com")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path != "/itmdesc/"+item {
		return nil, errors.New("eBay 描述页面地址无效或商品不匹配")
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u, nil
}

func ebayHasClass(n *html.Node, class string) bool {
	for _, name := range strings.Fields(attribute(n, "class")) {
		if name == class {
			return true
		}
	}
	return false
}

func ebayCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := ebayItemID(page)
	if getter == nil || doc == nil || id == "" {
		return result, errors.New("eBay 商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var containers, zooms []*html.Node
	var descRaw string
	var descSeen, hasVariants bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "h1" && ebayHasClass(n, "x-item-title__mainTitle") {
			result.Title = nodeText(n)
		}
		if n.Data == "div" && ebayHasClass(n, "ux-image-carousel-container") {
			containers = append(containers, n)
		}
		if n.Data == "div" && ebayHasClass(n, "ux-image-carousel") && ebayHasClass(n, "zoom") && ebayHasClass(n, "img-transition-medium") {
			zooms = append(zooms, n)
		}
		for _, class := range strings.Fields(attribute(n, "class")) {
			if class == "x-msku" || strings.HasPrefix(class, "x-msku__") {
				hasVariants = true
			}
		}
		if n.Data == "iframe" && attribute(n, "id") == "desc_ifr" {
			descSeen = true
			if descRaw == "" {
				descRaw = firstNonEmpty(attribute(n, "src"), attribute(n, "data-src"))
			}
		} else if n.Data == "a" && strings.Contains(attribute(n, "href"), "ebaydesc.com/itmdesc/") && descRaw == "" {
			descSeen, descRaw = true, attribute(n, "href")
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	seen := make(map[string]bool)
	invalid, limited := 0, false
	add := func(raw, kind string, upgrade bool) {
		if ctx.Err() != nil || limited {
			return
		}
		original, ok := ebayImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		image := original
		if upgrade {
			u, _ := url.Parse(original)
			u.Path = ebayThumbSize.ReplaceAllString(u.Path, `-l1600.$1`)
			u.RawPath = ""
			image = u.String()
		}
		if seen[image] {
			return
		}
		if len(result.Items) == maxEbayImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"ebay_item_id": id, "image_kind": kind}}
		if image != original {
			item.AlternateURLs = []string{original}
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	addGallery := func(n *html.Node) {
		var first string
		for _, name := range []string{"data-zoom-src", "data-src", "src"} {
			raw := attribute(n, name)
			if raw == "" {
				continue
			}
			if first == "" {
				first = raw
			}
			if _, ok := ebayImageURL(page, raw); ok {
				add(raw, "main", true)
				return
			}
		}
		if first != "" {
			add(first, "main", true)
		}
	}
	mainStart := len(result.Items)
	for _, container := range containers {
		before := len(result.Items)
		walkHTML(container, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "img" {
				return
			}
			inside := false
			for parent := n.Parent; parent != nil && parent != container; parent = parent.Parent {
				if ebayHasClass(parent, "ux-image-carousel-item") {
					inside = true
					break
				}
			}
			if inside {
				addGallery(n)
			}
		})
		if len(result.Items) > before {
			break
		}
	}
	if len(result.Items) == mainStart && len(zooms) >= 2 {
		walkHTML(zooms[1], func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" {
				add(attribute(n, "data-src"), "main", true)
			}
		})
	}
	var problems []error
	if len(result.Items) == mainStart {
		problems = append(problems, errors.New("eBay 商品画廊未返回图片"))
	}
	if hasVariants {
		problems = append(problems, errors.New("eBay 只采集当前画廊，其他变体图片可能不完整"))
	}
	if descRaw == "" {
		if descSeen {
			problems = append(problems, errors.New("eBay 商品描述尚未加载，详情图可能不完整"))
		}
	} else {
		desc, err := ebayDescriptionURL(page, descRaw, id)
		if err != nil {
			problems = append(problems, err)
		} else {
			if err := ebayDescription(ctx, getter, page, desc, id, add); err != nil {
				problems = append(problems, err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 张 eBay 图片地址无效或不受支持", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("eBay 商品超过 %d 张图片，结果不完整", maxEbayImages))
	}
	return result, errors.Join(problems...)
}

func ebayDescription(ctx context.Context, getter Getter, page, desc *url.URL, id string, add func(string, string, bool)) error {
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: desc.String(), referer: page.Scheme + "://" + page.Host + "/", depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("读取 eBay 商品描述失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(desc, final) {
		return errors.New("eBay 商品描述已跳转，详情图可能不完整")
	}
	if _, err := ebayDescriptionURL(page, final.String(), id); err != nil {
		return err
	}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() == nil && n.Type == html.ElementNode && n.Data == "img" {
			raw := attribute(n, "src")
			if raw != "" {
				add(raw, "detail", false)
			}
		}
	})
	return ctx.Err()
}
