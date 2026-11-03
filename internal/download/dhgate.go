package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	dhgateProductPath = regexp.MustCompile(`^/(?:store/)?product/[^/]+/([1-9][0-9]{4,19})\.html$`)
	dhgateFlightPush  = regexp.MustCompile(`\bself\.__next_f\.push\(\s*`)
)

func dhgateProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "www.dhgate.com", "dhgate.com", "de.dhgate.com", "it.dhgate.com", "pt.dhgate.com", "fr.dhgate.com", "es.dhgate.com",
		"ar.dhgate.com", "kr.dhgate.com", "ru.dhgate.com", "tr.dhgate.com", "nl.dhgate.com", "pl.dhgate.com", "se.dhgate.com", "jp.dhgate.com", "ie.dhgate.com":
	default:
		return ""
	}
	match := dhgateProductPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func isDHgateProduct(u *url.URL) bool { return dhgateProductID(u) != "" }

func discoverDHgateProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := dhgateProductID(root)
	if getter == nil || id == "" {
		return result, errors.New("敦煌网商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || dhgateProductID(final) != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("敦煌网商品页面不可用或跳转到其他商品")
	}
	return dhgateProductPage(ctx, doc, final)
}

func dhgateImageURL(raw string) (string, bool) {
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
	case "img4.dhresource.com", "www.dhresource.com":
	default:
		return "", false
	}
	if !(strings.HasPrefix(u.Path, "/webp/") || strings.HasPrefix(u.Path, "/0x0/")) {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func dhgateValueID(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case json.Number:
		return string(value)
	}
	return ""
}

func dhgateDescAncestor(n *html.Node, needDetails bool) bool {
	desc, details := false, false
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode {
			continue
		}
		if attribute(p, "id") == "descWrap" {
			desc = true
		}
		for _, class := range strings.Fields(attribute(p, "class")) {
			if class == "product-details" {
				details = true
			}
		}
	}
	return desc && (!needDetails || details)
}

// dhgateProductPage decodes the site's Next Flight data as JSON without
// executing scripts. Gallery and SKU data must share the requested itemCode.
func dhgateProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := dhgateProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("敦煌网商品页面无效")
	}
	var flight strings.Builder
	var canonical, cover, ogType string
	var descFound, detailsFound bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if attribute(n, "id") == "descWrap" {
			descFound = true
		}
		if dhgateDescAncestor(n, false) {
			for _, class := range strings.Fields(attribute(n, "class")) {
				if class == "product-details" {
					detailsFound = true
				}
			}
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" {
			switch strings.ToLower(attribute(n, "property")) {
			case "og:url":
				canonical = firstNonEmpty(canonical, attribute(n, "content"))
			case "og:image":
				cover = firstNonEmpty(cover, attribute(n, "content"))
			case "og:type":
				ogType = firstNonEmpty(ogType, attribute(n, "content"))
			}
		}
		if n.Data != "script" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode {
				continue
			}
			for _, loc := range offer1688CodeMatches(c.Data, dhgateFlightPush) {
				var args []json.RawMessage
				if json.NewDecoder(strings.NewReader(c.Data[loc[1]:])).Decode(&args) != nil || len(args) < 2 {
					continue
				}
				var channel int
				var payload string
				if json.Unmarshal(args[0], &channel) == nil && channel == 1 && json.Unmarshal(args[1], &payload) == nil && flight.Len()+len(payload) <= maxPageBytes {
					flight.WriteString(payload)
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	seen := map[string]bool{}
	incomplete := false
	add := func(raw, kind, variant string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		image, ok := dhgateImageURL(raw)
		if !ok {
			incomplete = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		metadata := map[string]string{"dhgate_item_code": id, "image_kind": kind}
		if variant != "" {
			metadata["variant"] = variant
		}
		item := Item{URL: image, RequireImage: true, Metadata: metadata}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	found := false
	var visit func(any, int)
	visit = func(value any, depth int) {
		if found || ctx.Err() != nil || depth > 64 {
			return
		}
		switch node := value.(type) {
		case map[string]any:
			item, _ := node["prodItem"].(map[string]any)
			gallery, _ := node["prodImg"].(map[string]any)
			if dhgateValueID(node["itemCode"]) == id && dhgateValueID(item["itemCode"]) == id && gallery != nil {
				found = true
				if title, ok := item["productName"].(string); ok {
					result.Title = firstNonEmpty(title, result.Title)
				}
				if images, ok := gallery["oriImgList"].([]any); ok {
					for _, value := range images {
						if image, ok := value.(string); ok {
							add(image, "main", "")
						}
					}
				} else {
					incomplete = true
				}
				sku, _ := node["prodSku"].(map[string]any)
				if groups, ok := sku["itemAttrList"].([]any); ok {
					for _, value := range groups {
						group, _ := value.(map[string]any)
						values, _ := group["itemAttrvalList"].([]any)
						for _, value := range values {
							variant, _ := value.(map[string]any)
							image, _ := variant["pic0x0Url"].(string)
							name, _ := variant["attrValName"].(string)
							add(image, "sku", name)
						}
					}
				} else {
					incomplete = true
				}
				return
			}
			for _, child := range node {
				visit(child, depth+1)
			}
		case []any:
			for _, child := range node {
				visit(child, depth+1)
			}
		}
	}
	for _, line := range strings.Split(flight.String(), "\n") {
		_, data, ok := strings.Cut(line, ":")
		if !ok || !strings.Contains(data, `"oriImgList"`) {
			continue
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		if decoder.Decode(&value) == nil {
			visit(value, 0)
		}
		if found || ctx.Err() != nil {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if found {
		walkHTML(doc, func(n *html.Node) {
			if ctx.Err() == nil && n.Type == html.ElementNode && n.Data == "img" && dhgateDescAncestor(n, detailsFound) {
				add(attribute(n, "src"), "description", "")
			}
		})
		if len(result.Items) == 0 {
			return result, errors.New("敦煌网未返回可信商品图片")
		}
		if !descFound || incomplete {
			return result, errors.New("敦煌网详情或部分图片尚未加载，结果可能不完整")
		}
		return result, nil
	}
	if strings.EqualFold(ogType, "product") {
		if u, err := crawlURL(page, canonical); err == nil && dhgateProductID(u) == id {
			add(cover, "main", "")
		}
	}
	if len(result.Items) > 0 {
		return result, errors.New("敦煌网只取得商品封面，规格或详情图片可能不完整")
	}
	return result, errors.New("敦煌网商品数据尚未加载，可能需要登录或安全验证")
}
