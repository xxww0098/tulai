package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

var alibabaProductPath = regexp.MustCompile(`^/product-detail/(?:[A-Za-z0-9_-]*_)?([0-9]{5,24})\.html$`)
var alibabaMobileProductPath = regexp.MustCompile(`^/product/([0-9]{5,24})/[A-Za-z0-9_-]+\.html$`)
var alibabaGlobalData = regexp.MustCompile(`"globalData"\s*:`)
var alibabaDescription = regexp.MustCompile(`(?:"productHtmlDescription"|'productHtmlDescription'|\bproductHtmlDescription)\s*:`)

func alibabaProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "alibaba.com", "www.alibaba.com", "m.alibaba.com":
	default:
		return ""
	}
	match := alibabaProductPath.FindStringSubmatch(u.Path)
	if len(match) == 0 && host == "m.alibaba.com" {
		match = alibabaMobileProductPath.FindStringSubmatch(u.Path)
	}
	if len(match) != 2 || strings.Trim(match[1], "0") == "" || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return ""
	}
	return match[1]
}

func isAlibabaProduct(u *url.URL) bool { return alibabaProductID(u) != "" }

func discoverAlibabaProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isAlibabaProduct(root) {
		return result, errors.New("阿里巴巴国际站商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || alibabaProductID(final) != alibabaProductID(root) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("阿里巴巴国际站商品页面不可用或跳转到其他商品，可能需要登录或安全验证")
	}
	return alibabaCollect(ctx, getter, final, doc)
}

// alibabaCollect also accepts the DOM captured after the user completes verification.
func alibabaCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isAlibabaProduct(page) || doc == nil {
		return result, errors.New("阿里巴巴国际站商品页面或下载客户端无效")
	}
	seen := map[string]bool{}
	var main, desc, descPayload, skuData, invalid, malformed, limited, verification bool
	add := func(raw string, kind int) bool {
		u, err := crawlURL(page, raw)
		if err != nil || u.User != nil ||
			(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) ||
			strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
			return false
		}
		host := strings.ToLower(u.Hostname())
		if host != "alicdn.com" && !(strings.HasSuffix(host, ".alicdn.com") && len(host) > len(".alicdn.com")) {
			return false
		}
		if u.Scheme == "http" {
			u.Scheme, u.Host = "https", u.Hostname()
		}
		// The reference changes .jpg_ to .jpg#, so the CDN receives the original path.
		if kind&1 != 0 {
			if at := strings.Index(u.Path[strings.LastIndex(u.Path, "/")+1:], ".jpg_"); at >= 0 {
				at += strings.LastIndex(u.Path, "/") + 1
				u.Path, u.RawPath, u.RawQuery, u.ForceQuery = u.Path[:at+4], "", "", false
			}
		}
		if kind&1 != 0 {
			main = true
		}
		if kind&2 != 0 {
			desc = true
		}
		if !seen[u.String()] {
			// shortcut: 图片最多收集 10000 张，超过时保留已有结果并提示。
			if len(result.Items) >= 10000 {
				limited = true
				return true
			}
			seen[u.String()] = true
			// Ali CDN may negotiate AVIF for a .jpg URL if the downloader advertises it.
			item := Item{URL: u.String(), Headers: map[string]string{"Accept": "image/jpeg,image/png,image/gif,*/*;q=0.5"}}
			if ref := imageReferer(page.String(), item.URL); ref != "" {
				item.Headers["Referer"] = ref
			}
			result.Items = append(result.Items, item)
		}
		return true
	}
	addNode := func(n *html.Node, kind int) {
		for _, key := range []string{"data-original", "data-src", "data-lazy-src", "src"} {
			raw := attribute(n, key)
			if !possibleImage(raw) {
				continue
			}
			if add(raw, kind) {
				return
			}
			invalid = true
		}
	}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:title") && attribute(n, "content") != "" {
			result.Title = attribute(n, "content")
		}
		if n.Data == "img" {
			if kind := alibabaImageKind(n); kind != 0 {
				addNode(n, kind)
			}
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			if strings.Contains(text, "_____tmd_____") || strings.Contains(text, "bixi.alicdn.com/punish/") || strings.Contains(text, "cnsec.alibaba.com/query.htm?smApp=aisn-detail") {
				verification = true
			}
			for _, match := range offer1688CodeMatches(text, alibabaGlobalData) {
				tail := text[match[1]:]
				var value any
				decoder := json.NewDecoder(strings.NewReader(tail))
				if decoder.Decode(&value) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					malformed = true
					continue
				}
				found, ok := alibabaSKUImages(ctx, value, 0, func(raw string) {
					if !add(raw, 0) {
						invalid = true
					}
				})
				skuData = skuData || found
				malformed = malformed || !ok
			}
			for _, match := range offer1688CodeMatches(text, alibabaDescription) {
				tail := strings.TrimSpace(text[match[1]:])
				value, used, ok := offer1688String(tail)
				if !ok || !offer1688LiteralEnd(tail[used:]) {
					malformed = true
					continue
				}
				descPayload = true
				fragment, err := html.Parse(strings.NewReader(value))
				if err != nil {
					malformed = true
					continue
				}
				walkHTML(fragment, func(n *html.Node) {
					if ctx.Err() == nil && n.Type == html.ElementNode && n.Data == "img" {
						addNode(n, 2)
					}
				})
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var issues []error
	if verification {
		issues = append(issues, errors.New("阿里巴巴国际站要求安全验证，请在浏览器中完成验证"))
	}
	if !main {
		issues = append(issues, errors.New("未找到阿里巴巴国际站商品主图，可能需要登录或安全验证"))
	}
	if !skuData {
		issues = append(issues, errors.New("未读取阿里巴巴国际站商品规格数据，结果可能不完整"))
	}
	if !desc {
		issues = append(issues, errors.New("未读取阿里巴巴国际站商品详情图，结果可能不完整"))
	} else if !descPayload {
		issues = append(issues, errors.New("未读取阿里巴巴国际站商品详情接口数据，结果可能不完整"))
	}
	if invalid {
		issues = append(issues, errors.New("部分阿里巴巴国际站图片地址无效或不属于受支持的图片站点"))
	}
	if malformed {
		issues = append(issues, errors.New("部分阿里巴巴国际站商品数据无法解析，结果可能不完整"))
	}
	if limited {
		issues = append(issues, errors.New("阿里巴巴国际站图片超过采集上限，结果可能不完整"))
	}
	return result, errors.Join(issues...)
}

// shortcut: DOM 最多向上检查 128 层；极深页面需要调整这个上限。
func alibabaImageKind(n *html.Node) int {
	var kind int
	for depth := 0; n != nil && depth < 128; n, depth = n.Parent, depth+1 {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "div" && strings.HasPrefix(attribute(n, "class"), "id-overflow-hidden id-h-full") {
			kind |= 1
		}
		if n.Data == "div" && attribute(n, "id") == "J-rich-text-description" {
			kind |= 2
		}
	}
	return kind
}

// shortcut: 商品数据最多遍历 64 层；极深数据保留已有结果并提示。
func alibabaSKUImages(ctx context.Context, value any, depth int, add func(string)) (found, ok bool) {
	if ctx.Err() != nil || depth > 64 {
		return false, false
	}
	switch v := value.(type) {
	case []any:
		ok = true
		for _, child := range v {
			subFound, subOK := alibabaSKUImages(ctx, child, depth+1, add)
			found, ok = found || subFound, ok && subOK
		}
		return found, ok
	case map[string]any:
		ok = true
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := v[key]
			if key == "sku" {
				found = true
				if child != nil {
					sku, isObject := child.(map[string]any)
					attrs, hasAttrs := sku["skuAttrs"]
					if !isObject || !hasAttrs || !alibabaSKUAttrs(ctx, attrs, add) {
						ok = false
					}
				}
			}
			subFound, subOK := alibabaSKUImages(ctx, child, depth+1, add)
			found, ok = found || subFound, ok && subOK
		}
		return found, ok
	default:
		return false, true
	}
}

func alibabaSKUAttrs(ctx context.Context, value any, add func(string)) bool {
	if value == nil {
		return true
	}
	attrs, ok := value.([]any)
	if !ok {
		return false
	}
	valid := true
	for _, attr := range attrs {
		if ctx.Err() != nil {
			return false
		}
		object, ok := attr.(map[string]any)
		if !ok {
			valid = false
			continue
		}
		values, ok := object["values"].([]any)
		if !ok {
			valid = false
			continue
		}
		for _, value := range values {
			if ctx.Err() != nil {
				return false
			}
			object, ok := value.(map[string]any)
			if !ok {
				valid = false
				continue
			}
			if image, exists := object["originImage"]; exists && image != nil {
				raw, ok := image.(string)
				if !ok {
					valid = false
					continue
				}
				if raw != "" {
					add(raw)
				}
			}
		}
	}
	return valid
}
