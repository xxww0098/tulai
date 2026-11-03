package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

var (
	amazonASIN              = regexp.MustCompile(`(?i)^[a-z0-9]{10}$`)
	amazonColorImagesMarker = regexp.MustCompile(`(?:"colorImages"|'colorImages')\s*:`)
	amazonInitialMarker     = regexp.MustCompile(`(?:"initial"|'initial'|\binitial)\s*:`)
	amazonParentMarker      = regexp.MustCompile(`(?:"parentAsin"|'parentAsin')\s*:`)
	amazonParseJSONCall     = regexp.MustCompile(`(?:jQuery\.parseJSON|A\.\$\.parseJSON|JSON\.parse)\s*\(\s*`)
)

// shortcut: bound the number of product images; larger pages return partial results.
const maxAmazonImages = 10000

func amazonMarket(host string) string {
	host = strings.ToLower(host)
	if strings.HasPrefix(host, "www.") {
		host = strings.TrimPrefix(host, "www.")
	} else if strings.HasPrefix(host, "m.") {
		host = strings.TrimPrefix(host, "m.")
	}
	switch host {
	case "amazon.com", "amazon.co.uk", "amazon.de", "amazon.fr", "amazon.es", "amazon.it", "amazon.co.jp",
		"amazon.com.br", "amazon.ca", "amazon.in", "amazon.com.au", "amazon.com.mx", "amazon.nl",
		"amazon.com.be", "amazon.pl", "amazon.se", "amazon.com.tr", "amazon.ae", "amazon.sa", "amazon.eg":
		return host
	}
	return ""
}

func amazonProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || amazonMarket(u.Hostname()) == "" ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	if len(parts) >= 2 && parts[0] == "dp" {
		id = parts[1]
	} else if len(parts) >= 3 && parts[1] == "dp" {
		id = parts[2]
	} else if len(parts) >= 3 && parts[0] == "gp" && parts[1] == "product" {
		id = parts[2]
	}
	if !amazonASIN.MatchString(id) {
		return ""
	}
	return strings.ToUpper(id)
}

func isAmazonProduct(u *url.URL) bool { return amazonProductID(u) != "" }

func discoverAmazonProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := amazonProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("亚马逊商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取亚马逊商品页失败: %w", err)
	}
	if doc == nil || final == nil || amazonProductID(final) != id || amazonMarket(root.Hostname()) != amazonMarket(final.Hostname()) ||
		(root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("亚马逊商品页已跳转，可能需要登录或安全验证")
	}
	return amazonCollect(ctx, getter, final, doc)
}

func amazonImageURL(page *url.URL, raw string) (string, bool) {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "m.media-amazon.com", "images-na.ssl-images-amazon.com", "images-eu.ssl-images-amazon.com", "images-fe.ssl-images-amazon.com", "images-cn.ssl-images-amazon.com", "images-amazon.com", "ecx.images-amazon.com":
	default:
		return "", false
	}
	if (u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.HasPrefix(u.Path, "/images/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	u.Scheme, u.Host = "https", host
	return u.String(), true
}

type amazonImage struct {
	HiRes   string `json:"hiRes"`
	Large   string `json:"large"`
	Variant string `json:"variant"`
}

func amazonWithin(n *html.Node, ids ...string) bool {
	for parent := n; parent != nil; parent = parent.Parent {
		if parent.Type != html.ElementNode {
			continue
		}
		id := attribute(parent, "id")
		for _, target := range ids {
			if id == target {
				return true
			}
		}
	}
	return false
}

func amazonDetailNode(n *html.Node) bool {
	// A+ comparison tables advertise other products, not this item's details.
	for parent := n; parent != nil; parent = parent.Parent {
		id := strings.ToLower(attribute(parent, "id"))
		class := strings.ToLower(attribute(parent, "class"))
		if strings.Contains(id, "comparison-table") || strings.Contains(id, "comparison_table") ||
			strings.Contains(class, "comparison-table") || strings.Contains(class, "comparison_table") {
			return false
		}
	}
	if amazonWithin(n, "aplus_feature_div", "aplus3p_feature_div", "productDescription", "productDescription_feature_div", "product-description") {
		return true
	}
	for parent := n; parent != nil; parent = parent.Parent {
		for _, class := range strings.Fields(attribute(parent, "class")) {
			if class == "content-grid-row-wrapper" || class == "apm-spacing" || class == "aplus-3p-fixed-width" {
				return true
			}
		}
	}
	return false
}

func amazonProductScript(n *html.Node) bool {
	return amazonWithin(n, "imageBlock_feature_div", "mediaBlock_feature_div", "mediaBlockVariations_feature_div", "imageBlock", "imageBlockContainer")
}

// amazonCollect parses the public HTML/JSON literals only; it never executes scripts.
func amazonCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := amazonProductID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" || doc == nil {
		return result, errors.New("亚马逊商品页面或下载客户端无效")
	}
	var title string
	var asinMismatch, parentMatches bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "input" && strings.EqualFold(attribute(n, "id"), "ASIN") {
			if value := strings.ToUpper(strings.TrimSpace(attribute(n, "value"))); value != "" && value != id {
				asinMismatch = true
			}
		}
		if n.Data == "title" && title == "" {
			title = nodeText(n)
		}
		if attribute(n, "id") == "productTitle" {
			result.Title = nodeText(n)
		}
		if n.Data == "script" && amazonWithin(n, "softlinesTwister_feature_div", "twister", "mediaBlockVariations_feature_div") {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.TextNode {
					continue
				}
				for _, at := range offer1688CodeMatches(child.Data, amazonParentMarker) {
					value, _, ok := wechatString(strings.TrimSpace(child.Data[at[1]:]))
					if ok && strings.EqualFold(value, id) {
						parentMatches = true
					}
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if asinMismatch && !parentMatches {
		return result, errors.New("亚马逊页面商品 ID 与请求不匹配")
	}
	if result.Title == "" {
		result.Title = title
	}
	seen := make(map[string]bool)
	var problems []error
	mainCount, detailCount, invalid := 0, 0, 0
	limited, colorFound, colorParsed := false, false, false
	add := func(raw, kind, variant string) {
		if ctx.Err() != nil || raw == "" {
			return
		}
		image, ok := amazonImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		if seen[image] {
			if kind == "main" {
				mainCount++
			}
			if kind == "detail" {
				detailCount++
			}
			return
		}
		if len(result.Items) >= maxAmazonImages {
			limited = true
			return
		}
		seen[image] = true
		metadata := map[string]string{"amazon_asin": id, "image_kind": kind}
		if variant != "" {
			metadata["variant"] = variant
		}
		item := Item{URL: image, RequireImage: true, Metadata: metadata}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
		if kind == "main" {
			mainCount++
		}
		if kind == "detail" {
			detailCount++
		}
	}
	addImages := func(images []amazonImage, kind, color string) {
		for _, image := range images {
			if ctx.Err() != nil || limited {
				break
			}
			raw := strings.TrimSpace(image.HiRes)
			if raw == "" {
				raw = strings.TrimSpace(image.Large)
			}
			if raw == "" {
				continue
			}
			variant := color
			if image.Variant != "" {
				if variant != "" {
					variant += "/"
				}
				variant += image.Variant
			}
			add(raw, kind, variant)
		}
	}
	addGroups := func(groups map[string]json.RawMessage) bool {
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		found := false
		for _, key := range keys {
			var images []amazonImage
			if json.Unmarshal(groups[key], &images) != nil {
				continue
			}
			found = true
			kind, color := "sku", key
			if key == "initial" {
				kind, color = "main", ""
			}
			addImages(images, kind, color)
		}
		return found
	}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || limited || n.Type != html.ElementNode {
			return
		}
		if n.Data == "script" && amazonProductScript(n) {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.TextNode {
					continue
				}
				text := child.Data
				// The public variation payload is JSON inside a quoted parseJSON
				// argument. Decode that literal; do not evaluate the surrounding JS.
				for _, call := range offer1688CodeMatches(text, amazonParseJSONCall) {
					literal, _, ok := wechatString(strings.TrimSpace(text[call[1]:]))
					if !ok || !strings.Contains(literal, `"colorImages"`) {
						continue
					}
					var payload struct {
						ColorImages map[string]json.RawMessage `json:"colorImages"`
					}
					if json.Unmarshal([]byte(literal), &payload) != nil || payload.ColorImages == nil {
						continue
					}
					colorFound = true
					colorParsed = addGroups(payload.ColorImages) || colorParsed
				}
				for _, at := range offer1688CodeMatches(text, amazonColorImagesMarker) {
					colorFound = true
					object, ok := offer1688Object(text[at[1]:])
					if !ok {
						continue
					}
					var groups map[string]json.RawMessage
					if json.Unmarshal([]byte(object), &groups) == nil && groups != nil {
						colorParsed = addGroups(groups) || colorParsed
						continue
					}
					for _, marker := range offer1688CodeMatches(object, amazonInitialMarker) {
						rest := strings.TrimSpace(object[marker[1]:])
						var payload string
						for _, prefix := range []string{"A.$.parseJSON", "jQuery.parseJSON", "JSON.parse"} {
							if strings.HasPrefix(rest, prefix) {
								tail := strings.TrimSpace(strings.TrimPrefix(rest, prefix))
								if strings.HasPrefix(tail, "(") {
									payload, _, _ = wechatString(strings.TrimSpace(tail[1:]))
								}
								break
							}
						}
						var images []amazonImage
						if payload != "" && json.Unmarshal([]byte(payload), &images) == nil {
							colorParsed = true
							addImages(images, "main", "")
						} else if strings.HasPrefix(rest, "[") && json.NewDecoder(strings.NewReader(rest)).Decode(&images) == nil {
							colorParsed = true
							addImages(images, "main", "")
						}
					}
				}
			}
		}
		if n.Data == "img" && (attribute(n, "id") == "landingImage" || amazonWithin(n, "imgTagWrapperId")) {
			raw := firstNonEmpty(attribute(n, "data-old-hires"), attribute(n, "data-a-hires"), attribute(n, "src"))
			add(raw, "main", "")
		}
		if n.Data == "img" && amazonDetailNode(n) {
			raw := firstNonEmpty(attribute(n, "data-a-hires"), attribute(n, "data-src"), attribute(n, "src"))
			add(raw, "detail", "")
		}
		if amazonDetailNode(n) {
			for _, match := range cssURL.FindAllStringSubmatch(attribute(n, "style"), -1) {
				if len(match) > 1 {
					add(strings.Trim(strings.TrimSpace(match[1]), `"'`), "detail", "")
				}
			}
		}
	})
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if limited {
		problems = append(problems, fmt.Errorf("亚马逊商品超过 %d 张图片，结果不完整", maxAmazonImages))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 张亚马逊图片地址无效或不属于已知图床", invalid))
	}
	if colorFound && !colorParsed {
		problems = append(problems, errors.New("亚马逊商品图片列表格式未识别，规格图片可能不完整"))
	}
	if mainCount == 0 {
		problems = append(problems, errors.New("亚马逊商品未找到主图，可能需要登录或安全验证"))
	}
	if detailCount == 0 {
		problems = append(problems, errors.New("亚马逊商品未找到详情图片，可能没有详情图或页面加载不完整"))
	}
	return result, errors.Join(problems...)
}
