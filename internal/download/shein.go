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
	sheinHostPattern = regexp.MustCompile(`^(?:(?:m|[a-z]{2,3})\.)?shein\.com$`)
	sheinProductPath = regexp.MustCompile(`(?i)-p-([1-9][0-9]{0,19})\.html$`)
	sheinNumericID   = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	sheinStateAssign = regexp.MustCompile(`\bwindow\s*\.\s*gbRawData\s*=`)
)

// shortcut: bound unusually large product galleries until the limit is configurable.
const sheinImageLimit = 10000

func sheinHost(u *url.URL) bool {
	return u != nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") &&
		(u.Port() == "" || (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")) &&
		sheinHostPattern.MatchString(strings.ToLower(u.Hostname()))
}

func sheinProductID(u *url.URL) string {
	if !sheinHost(u) || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return ""
	}
	m := sheinProductPath.FindStringSubmatch(u.Path)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func isSheinProduct(u *url.URL) bool { return sheinProductID(u) != "" }

func isSheinChallenge(u *url.URL) bool {
	return sheinHost(u) && (u.Path == "/risk/challenge" || u.Path == "/risk/action/limit")
}

func sheinImageURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "img.ltwebstatic.com") ||
		(u.Port() != "" && u.Port() != "443") || u.Path == "" || u.Path == "/" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func discoverShein(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isSheinProduct(root) {
		return result, errors.New("SHEIN 商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if isSheinChallenge(final) {
		return result, errors.New("SHEIN 要求安全验证，请在下载浏览器中完成验证后重试")
	}
	if doc == nil || sheinProductID(final) != sheinProductID(root) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("SHEIN 商品页面不可用或跳转到其他商品")
	}
	return sheinCollect(ctx, final, doc)
}

func sheinString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

func sheinType(v any, want string) bool {
	switch x := v.(type) {
	case string:
		return x == want
	case []any:
		for _, v := range x {
			if sheinType(v, want) {
				return true
			}
		}
	}
	return false
}

type sheinCollector struct {
	ctx           context.Context
	page          *url.URL
	id            string
	result        Discovery
	seen          map[string]bool
	partial       bool
	stateComplete bool
}

func (c *sheinCollector) add(raw, id, color string) {
	if raw == "" {
		return
	}
	image, ok := sheinImageURL(raw)
	if !ok {
		c.partial = true
		return
	}
	if c.seen[image] {
		return
	}
	if len(c.result.Items) >= sheinImageLimit {
		c.partial = true
		return
	}
	c.seen[image] = true
	item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"shein_goods_id": id, "shein_color": color}}
	if ref := imageReferer(c.page.String(), image); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	c.result.Items = append(c.result.Items, item)
}

func (c *sheinCollector) imageValue(v any, id, color string, depth int) {
	if depth > 8 || c.ctx.Err() != nil {
		c.partial = true
		return
	}
	switch x := v.(type) {
	case string:
		c.add(x, id, color)
	case []any:
		for _, entry := range x {
			c.imageValue(entry, id, color, depth+1)
		}
	case map[string]any:
		raw := sheinString(x["contentUrl"])
		if raw == "" {
			raw = sheinString(x["url"])
		}
		if raw != "" {
			c.add(raw, id, color)
		}
	}
}

// JSON-LD must identify the requested product by its URL, not by an SKU or
// a productGroupID: those identifiers have different meanings on SHEIN.
func (c *sheinCollector) jsonLD(v any, depth int) {
	if depth > 24 || c.ctx.Err() != nil {
		c.partial = true
		return
	}
	switch x := v.(type) {
	case []any:
		for _, n := range x {
			c.jsonLD(n, depth+1)
		}
	case map[string]any:
		if graph, ok := x["@graph"]; ok {
			c.jsonLD(graph, depth+1)
		}
		if !sheinType(x["@type"], "ProductGroup") && !sheinType(x["@type"], "Product") {
			return
		}
		rawURL := sheinString(x["url"])
		if rawURL == "" {
			return
		}
		u, err := url.Parse(rawURL)
		if err != nil || u == nil || sheinProductID(c.page.ResolveReference(u)) != c.id {
			return
		}
		if c.result.Title == "" {
			c.result.Title = sheinString(x["name"])
		}
		c.imageValue(x["image"], c.id, sheinString(x["color"]), 0)
		// hasVariant is contained in this identified ProductGroup; its images
		// belong to that product even when individual size variants omit a URL.
		if sheinType(x["@type"], "ProductGroup") {
			if variants, ok := x["hasVariant"].([]any); ok {
				for _, entry := range variants {
					if variant, ok := entry.(map[string]any); ok && sheinType(variant["@type"], "Product") {
						if raw := sheinString(variant["url"]); raw != "" {
							u, err := url.Parse(raw)
							if err != nil || sheinProductID(c.page.ResolveReference(u)) != c.id {
								c.partial = true
								continue
							}
						}
						c.imageValue(variant["image"], c.id, sheinString(variant["color"]), 0)
					}
				}
			}
		}
	}
}

// Reference script 134 uses allColorDetailImages and mainSaleAttribute.info.
// Require both in one product object, and bind the selected goods ID before
// accepting any related color. Never infer a product from generic page images.
func (c *sheinCollector) productState(v any, identity bool, depth int) {
	if depth > 32 || c.ctx.Err() != nil {
		c.partial = true
		return
	}
	switch x := v.(type) {
	case []any:
		for _, n := range x {
			c.productState(n, identity, depth+1)
		}
	case map[string]any:
		images, imagesOK := x["allColorDetailImages"].(map[string]any)
		sale, saleOK := x["mainSaleAttribute"].(map[string]any)
		if imagesOK && saleOK {
			id := sheinString(x["goods_id"])
			if id != "" && id != c.id {
				return
			}
			if id == "" && !identity {
				return
			}
			info, ok := sale["info"].([]any)
			if !ok {
				return
			}
			type colorEntry struct{ id, name string }
			colors := make([]colorEntry, 0, len(info))
			known := make(map[string]string)
			badInfo := false
			for _, entry := range info {
				if m, ok := entry.(map[string]any); ok {
					k, name := sheinString(m["goods_id"]), sheinString(m["attr_value"])
					if !sheinNumericID.MatchString(k) {
						badInfo = true
					} else if previous, exists := known[k]; exists {
						badInfo = badInfo || previous != name
					} else {
						colors = append(colors, colorEntry{k, name})
						known[k] = name
					}
				} else {
					badInfo = true
				}
			}
			if _, ok := known[c.id]; !ok {
				return
			}
			if _, ok := images[c.id]; !ok {
				return
			}
			if c.result.Title == "" {
				c.result.Title = sheinString(x["goods_name"])
			}
			complete := !badInfo
			for _, color := range colors {
				list, ok := images[color.id].([]any)
				if !ok || len(list) == 0 {
					complete = false
					continue
				}
				for _, entry := range list {
					m, ok := entry.(map[string]any)
					if !ok || sheinString(m["origin_image"]) == "" {
						complete = false
						continue
					}
					c.add(sheinString(m["origin_image"]), color.id, color.name)
				}
			}
			c.stateComplete = c.stateComplete || complete
			if !complete {
				c.partial = true
			}
			return
		}
		for _, child := range x {
			c.productState(child, identity, depth+1)
		}
	}
}

func sheinCollect(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := sheinProductID(page)
	if id == "" || doc == nil {
		return result, errors.New("SHEIN 商品页面无效")
	}
	c := &sheinCollector{ctx: ctx, page: page, id: id, result: result, seen: make(map[string]bool)}
	var identity, mismatch bool
	var scripts []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "script" {
			scripts = append(scripts, n)
		}
		var raw string
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			raw = attribute(n, "href")
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			raw = attribute(n, "content")
		}
		if raw == "" {
			return
		}
		u, err := url.Parse(raw)
		if err != nil || sheinProductID(page.ResolveReference(u)) != id {
			mismatch = true
		} else {
			identity = true
		}
	})
	if mismatch {
		return result, errors.New("SHEIN 商品页面编号不匹配")
	}
	for _, n := range scripts {
		if err := ctx.Err(); err != nil {
			return c.result, err
		}
		text := nodeText(n)
		if strings.EqualFold(strings.TrimSpace(attribute(n, "type")), "application/ld+json") {
			var v any
			dec := json.NewDecoder(strings.NewReader(text))
			dec.UseNumber()
			if dec.Decode(&v) == nil {
				c.jsonLD(v, 0)
			}
			continue
		}
		for _, match := range offer1688CodeMatches(text, sheinStateAssign) {
			var v any
			dec := json.NewDecoder(strings.NewReader(text[match[1]:]))
			dec.UseNumber()
			if dec.Decode(&v) == nil {
				c.productState(v, identity, 0)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return c.result, err
	}
	if len(c.result.Items) == 0 {
		return c.result, errors.New("SHEIN 未提供可确认归属的商品图片；页面可能需要登录或安全验证")
	}
	if c.partial || !c.stateComplete {
		return c.result, errors.New("SHEIN 仅采集到部分商品图片；其他颜色或详情可能不完整")
	}
	return c.result, nil
}
