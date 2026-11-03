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
	etsyListingPath = regexp.MustCompile(`^/(?:[A-Za-z]{2,3}(?:-[A-Za-z]{2,3})?/)?listing/([0-9]{6,20})(?:/[^/]+)?/?$`)
	etsyShopHost    = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.etsy\.com$`)
)

// shortcut: bound anomalously large galleries until the limit is configurable.
const maxEtsyImages = 10000

func etsyListingID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "etsy.com" && !etsyShopHost.MatchString(host) {
		return ""
	}
	match := etsyListingPath.FindStringSubmatch(u.Path)
	if len(match) != 2 || strings.Trim(match[1], "0") == "" {
		return ""
	}
	return match[1]
}

func isEtsyProduct(u *url.URL) bool { return etsyListingID(u) != "" }

func discoverEtsyProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := etsyListingID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("Etsy 商品地址或下载客户端无效")
	}
	page := *root
	if _, ok := getter.(*HTTPGetter); ok {
		page.Scheme = "https"
		page.Host = strings.ToLower(page.Hostname())
		if page.Host == "etsy.com" {
			page.Host = "www.etsy.com"
		}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取 Etsy 商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || etsyListingID(final) != id {
		return result, errors.New("Etsy 商品已跳转，可能需要登录或安全验证")
	}
	return etsyProductPage(ctx, doc, final)
}

func etsyImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "i.etsystatic.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path == "" || u.Path == "/" {
		return "", false
	}
	u.Scheme, u.Host = "https", "i.etsystatic.com"
	return u.String(), true
}

func etsyHasClass(n *html.Node, class string) bool {
	for _, name := range strings.Fields(attribute(n, "class")) {
		if name == class {
			return true
		}
	}
	return false
}

// etsyProductPage reads the current listing gallery, not recommendation carousels.
func etsyProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := etsyListingID(page)
	if doc == nil || id == "" {
		return result, errors.New("Etsy 商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var galleries, paneLists []*html.Node
	var ogURL, ogTitle string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
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
		if etsyHasClass(n, "listing-page-image-carousel-component") {
			galleries = append(galleries, n)
		} else if n.Data == "ul" && etsyHasClass(n, "carousel-pane-list") {
			paneLists = append(paneLists, n)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ogURL != "" {
		canonical, err := crawlURL(page, ogURL)
		if err != nil || etsyListingID(canonical) != id {
			return result, errors.New("Etsy 页面与请求的商品不匹配")
		}
	}
	if ogTitle != "" {
		result.Title = ogTitle
	}
	if len(galleries) == 0 {
		galleries = paneLists
	}
	seen := make(map[string]bool)
	invalid, preview, limited := 0, 0, false
	for _, gallery := range galleries {
		count := 0
		walkHTML(gallery, func(n *html.Node) {
			if ctx.Err() != nil || limited || n.Type != html.ElementNode || n.Data != "img" || !etsyHasClass(n, "carousel-image") {
				return
			}
			count++
			zoom := attribute(n, "data-src-zoom-image")
			image, ok := etsyImageURL(page, zoom)
			if !ok {
				for _, name := range []string{"data-src", "src"} {
					if image, ok = etsyImageURL(page, attribute(n, name)); ok {
						preview++
						break
					}
				}
			}
			if !ok {
				invalid++
				return
			}
			if seen[image] {
				return
			}
			if len(result.Items) >= maxEtsyImages {
				limited = true
				return
			}
			seen[image] = true
			item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"etsy_listing_id": id, "image_kind": "main"}}
			if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
				item.Headers = map[string]string{"Referer": ref}
			}
			result.Items = append(result.Items, item)
		})
		// A page may contain recommendation galleries; use the first listing gallery only.
		if count > 0 {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("Etsy 商品图库未返回图片，可能需要登录或安全验证"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("Etsy 商品图库有 %d 张图片地址无效或不受支持", invalid))
	}
	if preview > 0 {
		problems = append(problems, fmt.Errorf("Etsy 商品图库有 %d 张图片只有预览地址", preview))
	}
	if limited {
		problems = append(problems, fmt.Errorf("Etsy 商品图库超过 %d 张，结果不完整", maxEtsyImages))
	}
	return result, errors.Join(problems...)
}
