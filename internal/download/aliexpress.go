package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	aliProductIDPattern = regexp.MustCompile(`^[0-9]{1,24}$`)
	aliLocalePattern    = regexp.MustCompile(`^(?:www|m|[a-z]{2}|m\.[a-z]{2})\.aliexpress\.(?:com|us)$`)
	aliCDNPattern       = regexp.MustCompile(`^(?:ae[0-9]{2}\.alicdn\.com|ae-pic-a[0-9]+\.aliexpress-media\.com)$`)
	aliResizePattern    = regexp.MustCompile(`(?i)^(.+\.(?:jpe?g|png|gif|webp|avif))_(?:[0-9]+x[0-9]+(?:q[0-9]+)?\.(?:jpe?g|png|webp|avif)(?:_\.avif)?|\.webp|\.avif)$`)
	aliParamsPattern    = regexp.MustCompile(`\b(?:window\s*\.\s*)?runParams\s*=\s*`)
	aliDataPattern      = regexp.MustCompile(`(?:["']data["']|\bdata)\s*:\s*`)
	aliDescPattern      = regexp.MustCompile(`(?:["'](?:descriptionUrl|descriptionURL|pcDescUrl)["']|\b(?:descriptionUrl|descriptionURL|pcDescUrl))\s*:\s*`)
	aliDescPathPattern  = regexp.MustCompile(`^/product/description/pc/(?:[A-Za-z0-9_-]+/)*desc\.htm$`)
)

type aliProductData struct {
	Action struct {
		ProductID json.RawMessage `json:"productId"`
	} `json:"actionModule"`
	Title struct {
		Subject string `json:"subject"`
	} `json:"titleModule"`
	Images struct {
		URLs []string `json:"imagePathList"`
	} `json:"imageModule"`
	SKU struct {
		Properties []struct {
			Values []struct {
				Image string `json:"skuPropertyImagePath"`
			} `json:"skuPropertyValues"`
		} `json:"productSKUPropertyList"`
	} `json:"skuModule"`
	Description struct {
		URL string `json:"descriptionUrl"`
	} `json:"descriptionModule"`
}

func aliExpressProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "aliexpress.com" && host != "aliexpress.us" && !aliLocalePattern.MatchString(host) {
		return ""
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || (parts[0] != "item" && parts[0] != "i") || !strings.HasSuffix(parts[1], ".html") {
		return ""
	}
	id := strings.TrimSuffix(parts[1], ".html")
	if !aliProductIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func isAliExpressProduct(u *url.URL) bool { return aliExpressProductID(u) != "" }

func discoverAliExpressProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	want := aliExpressProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || want == "" {
		return result, errors.New("速卖通商品页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || aliExpressProductID(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("速卖通商品页面不可用或跳转到了其他商品，可能需要登录或完成验证")
	}
	return aliExpressCollect(ctx, getter, final, doc)
}

// aliExpressCollect shares product extraction with the browser capture flow.
func aliExpressCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := aliExpressProductID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" || doc == nil {
		return result, errors.New("速卖通商品页面或下载客户端无效")
	}
	seen := make(map[string]bool)
	var problems []error
	invalid := 0
	mainCount, detailCount := 0, 0
	var desc string
	blocked := false
	add := func(raw string, detail bool) {
		if raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
			return
		}
		image, key, ok := aliImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		if !seen[key] {
			seen[key] = true
			item := Item{URL: image}
			if ref := imageReferer(page.String(), image); ref != "" {
				item.Headers = map[string]string{"Referer": ref}
			}
			result.Items = append(result.Items, item)
		}
		if detail {
			detailCount++
		} else {
			mainCount++
		}
	}
	setDesc := func(raw string) {
		if raw == "" || desc != "" {
			return
		}
		if u := aliDescriptionURL(page, raw, id); u != nil {
			desc = u.String()
		}
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if attribute(n, "id") == "nc_1_wrapper" || strings.Contains(attribute(n, "class"), "captcha-tips") {
			blocked = true
		}
		if n.Data == "img" {
			main, detail := aliImageContext(n)
			if main || detail {
				add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "data-lazyload"), attribute(n, "data-original"), attribute(n, "src")), detail)
			}
		}
		if n.Data == "iframe" || n.Data == "a" {
			setDesc(firstNonEmpty(attribute(n, "src"), attribute(n, "data-src"), attribute(n, "href")))
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			if strings.Contains(text, "_____tmd_____") || strings.Contains(text, "Please slide to verify") {
				blocked = true
			}
			for _, match := range offer1688CodeMatches(text, aliDescPattern) {
				tail := strings.TrimSpace(text[match[1]:])
				if value, used, ok := offer1688String(tail); ok && offer1688LiteralEnd(tail[used:]) {
					setDesc(value)
				}
			}
			for _, match := range offer1688CodeMatches(text, aliParamsPattern) {
				object, ok := offer1688Object(text[match[1]:])
				if !ok {
					continue
				}
				for _, loc := range offer1688CodeMatches(object, aliDataPattern) {
					var data aliProductData
					if json.NewDecoder(strings.NewReader(object[loc[1]:])).Decode(&data) != nil || aliRawID(data.Action.ProductID) != id {
						continue
					}
					result.Title = firstNonEmpty(data.Title.Subject, result.Title)
					for _, image := range data.Images.URLs {
						add(image, false)
					}
					for _, property := range data.SKU.Properties {
						for _, value := range property.Values {
							add(value.Image, false)
						}
					}
					setDesc(data.Description.URL)
					break
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if blocked && mainCount == 0 && detailCount == 0 {
		return result, errors.New("速卖通要求登录或完成安全验证，请在下载浏览器中处理")
	}
	if desc != "" {
		if err := aliDescription(ctx, getter, page, desc, id, add); err != nil {
			problems = append(problems, fmt.Errorf("读取速卖通商品详情失败，结果可能不完整: %w", err))
		} else if detailCount == 0 {
			problems = append(problems, errors.New("速卖通商品详情没有可识别的图片，结果可能不完整"))
		}
	} else if detailCount == 0 {
		problems = append(problems, errors.New("速卖通商品详情未加载，结果可能不完整"))
	}
	if mainCount == 0 {
		problems = append(problems, errors.New("速卖通商品主图未加载，可能需要登录或完成验证"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 个速卖通商品图片地址无效或不属于受支持图床", invalid))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, errors.Join(problems...)
}

func aliRawID(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		value = string(raw)
	}
	if !aliProductIDPattern.MatchString(value) {
		return ""
	}
	return value
}

func aliImageContext(n *html.Node) (bool, bool) {
	for depth := 0; n != nil && depth < 128; depth++ {
		for _, class := range strings.Fields(attribute(n, "class")) {
			if strings.HasPrefix(class, "description--product-description--") {
				return false, true
			}
			if strings.HasPrefix(class, "slider--img--") || strings.HasPrefix(class, "sku--wrap--") || class == "images-view-item" || class == "gallery_Gallery__picList__1gsooe" {
				return true, false
			}
		}
		n = n.Parent
	}
	return false, false
}

func aliImageURL(page *url.URL, raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Path != "" && (path.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "//"))) {
		return "", "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || !aliCDNPattern.MatchString(strings.ToLower(u.Hostname())) {
		return "", "", false
	}
	for {
		match := aliResizePattern.FindStringSubmatch(u.Path)
		if match == nil || match[1] == u.Path {
			break
		}
		u.Path, u.RawPath, u.RawQuery = match[1], "", ""
	}
	if !imagePath(u.Path) {
		return "", "", false
	}
	key := u.String()
	if strings.HasPrefix(u.Path, "/kf/") {
		key = u.Path
	}
	return u.String(), key, true
}

func aliDescriptionURL(page *url.URL, raw, id string) *url.URL {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Path == "" || path.Clean(parsed.Path) != parsed.Path {
		return nil
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || !aliDescPathPattern.MatchString(u.Path) {
		return nil
	}
	switch strings.ToLower(u.Hostname()) {
	case "aeproductsourcesite.alicdn.com", "pdp.aliexpress-media.com":
	default:
		return nil
	}
	ids := u.Query()["productId"]
	if len(ids) != 1 || ids[0] != id {
		return nil
	}
	return u
}

func aliDescription(ctx context.Context, getter Getter, page *url.URL, raw, id string, add func(string, bool)) error {
	target := aliDescriptionURL(page, raw, id)
	if target == nil {
		return errors.New("速卖通详情地址无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: target.String(), referer: page.Scheme + "://" + page.Host + "/", depth: 2})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("速卖通详情请求失败")
	}
	if doc == nil || final == nil || aliDescriptionURL(page, final.String(), id) == nil || !sameOrigin(target, final) || final.Path != target.Path {
		return errors.New("速卖通详情跳转到了其他地址")
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "data-lazyload"), attribute(n, "data-original"), attribute(n, "src")), true)
		}
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
