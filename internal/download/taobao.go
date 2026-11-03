package download

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

var taobaoItemIDPattern = regexp.MustCompile(`^[0-9]{1,24}$`)
var taobaoDescField = regexp.MustCompile(`(?i)(?:"(httpsDescUrl|descUrl)"|'(httpsDescUrl|descUrl)'|\b(httpsDescUrl|descUrl))\s*:`)
var taobaoPropertyPics = regexp.MustCompile(`(?:"propertyPics"|'propertyPics'|\bpropertyPics)\s*:`)
var taobaoSKUBase = regexp.MustCompile(`(?:"skuBase"|'skuBase'|\bskuBase)\s*:`)

func taobaoItemID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return ""
	}
	var id string
	switch strings.ToLower(u.Hostname()) {
	case "item.taobao.com", "detail.taobao.com", "detail.tmall.com", "detail.tmall.hk", "detail.m.tmall.com":
		if u.Path != "/item.htm" && u.Path != "/item_o.htm" {
			return ""
		}
		values := u.Query()["id"]
		if len(values) == 1 {
			id = values[0]
		}
	case "new.m.taobao.com":
		if u.Path == "/detail.htm" && len(u.Query()["id"]) == 1 {
			id = u.Query().Get("id")
		}
	case "h5.m.taobao.com":
		if u.Path == "/awp/core/detail.htm" && len(u.Query()["id"]) == 1 {
			id = u.Query().Get("id")
		}
	case "world.taobao.com":
		if strings.HasPrefix(u.Path, "/item/") && strings.HasSuffix(u.Path, ".htm") {
			id = strings.TrimSuffix(strings.TrimPrefix(u.Path, "/item/"), ".htm")
		}
	}
	if !taobaoItemIDPattern.MatchString(id) || strings.Trim(id, "0") == "" {
		return ""
	}
	return id
}

func isTaobaoItem(u *url.URL) bool { return taobaoItemID(u) != "" }

func discoverTaobaoItem(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isTaobaoItem(root) {
		return result, errors.New("淘宝商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || taobaoItemID(final) != taobaoItemID(root) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("淘宝商品页面不可用或跳转到其他商品，可能需要登录或安全验证")
	}
	return taobaoCollect(ctx, getter, final, doc)
}

const (
	taobaoMain = 1 << iota
	taobaoSKU
	taobaoDesc
)

// taobaoCollect handles fetched HTML and the user's rendered product page.
func taobaoCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isTaobaoItem(page) || doc == nil {
		return result, errors.New("淘宝商品页面或下载客户端无效")
	}
	seen := map[string]bool{}
	var found int
	var issues []error
	var descURL, secureDesc string
	add := func(raw string, kind int) bool {
		if !taobaoAddImage(&result, seen, page, raw) {
			return false
		}
		found |= kind
		return true
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
		kind := taobaoImageKind(n)
		if kind != 0 {
			if n.Data == "img" {
				for _, key := range []string{"data-original", "data-src", "data-lazy-src", "data-ks-lazyload", "src"} {
					if raw := attribute(n, key); possibleImage(raw) {
						if add(raw, kind) {
							break
						}
						issues = append(issues, errors.New("淘宝商品图片地址无效或不属于受支持站点"))
					}
				}
			}
			for _, match := range cssURL.FindAllStringSubmatch(attribute(n, "style"), -1) {
				if raw := strings.Trim(strings.TrimSpace(match[1]), `"'`); raw != "" && !add(raw, kind) {
					issues = append(issues, errors.New("淘宝商品背景图片地址无效或不属于受支持站点"))
				}
			}
		}
		if n.Data != "script" {
			return
		}
		for _, raw := range jsonLDImages(n) {
			if !add(raw, taobaoMain) {
				issues = append(issues, errors.New("淘宝商品主图地址无效或不属于受支持站点"))
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, match := range offer1688CodeMatches(text, taobaoDescField) {
				name := firstNonEmpty(offer1688Match(text, match[2], match[3]), offer1688Match(text, match[4], match[5]), offer1688Match(text, match[6], match[7]))
				tail := strings.TrimSpace(text[match[1]:])
				value, used, ok := offer1688String(tail)
				if !ok || !offer1688LiteralEnd(tail[used:]) || value == "" {
					continue
				}
				if strings.EqualFold(name, "httpsDescUrl") {
					secureDesc = value
				} else if descURL == "" {
					descURL = value
				}
			}
			for _, match := range offer1688CodeMatches(text, taobaoPropertyPics) {
				var pics any
				tail := text[match[1]:]
				decoder := json.NewDecoder(strings.NewReader(tail))
				if decoder.Decode(&pics) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					issues = append(issues, errors.New("无法读取淘宝商品规格图片"))
					continue
				}
				if err := taobaoPropertyImages(pics, 0, func(raw string) bool { return add(raw, taobaoSKU) }); err != nil {
					issues = append(issues, err)
				}
			}
			for _, match := range offer1688CodeMatches(text, taobaoSKUBase) {
				var sku struct {
					Props []struct {
						Values []struct {
							Image string `json:"image"`
						} `json:"values"`
					} `json:"props"`
				}
				tail := text[match[1]:]
				decoder := json.NewDecoder(strings.NewReader(tail))
				if decoder.Decode(&sku) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					issues = append(issues, errors.New("无法读取淘宝商品规格列表"))
					continue
				}
				for _, prop := range sku.Props {
					for _, value := range prop.Values {
						if value.Image != "" && !add(value.Image, taobaoSKU) {
							issues = append(issues, errors.New("淘宝商品规格图片地址无效或不属于受支持站点"))
						}
					}
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	desc := firstNonEmpty(secureDesc, descURL)
	if desc != "" {
		url, err := crawlURL(page, desc)
		if err != nil || !taobaoDescriptionHost(url) {
			issues = append(issues, errors.New("淘宝商品详情地址无效，结果可能不完整"))
		} else {
			if url.Scheme == "http" {
				url.Scheme, url.Host = "https", url.Hostname()
			}
			body, requestErr := album1688Request(ctx, getter, url, page)
			if requestErr == nil {
				requestErr = taobaoDescription(body, url, func(raw string) bool { return add(raw, taobaoDesc) })
			}
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if requestErr != nil {
				issues = append(issues, errors.New("读取淘宝商品详情失败，结果可能不完整"))
			}
		}
	}
	if found&taobaoMain == 0 {
		issues = append(issues, errors.New("未找到淘宝商品主图，可能需要登录或安全验证"))
	}
	if found&taobaoDesc == 0 {
		issues = append(issues, errors.New("未找到淘宝商品详情图，结果可能不完整"))
	}
	return result, errors.Join(issues...)
}

// shortcut: 页面节点最多回溯 128 层；遇到更深的商品页面时调整上限。
func taobaoImageKind(n *html.Node) int {
	var kind int
	for depth := 0; n != nil && depth < 128; n, depth = n.Parent, depth+1 {
		if n.Type != html.ElementNode {
			continue
		}
		switch attribute(n, "id") {
		case "J_UlThumb", "J_ThumbNav", "J_ImgBooth":
			kind |= taobaoMain
		case "J_Desc", "J_DivItemDesc", "descinfo":
			kind |= taobaoDesc
		}
		for _, attr := range n.Attr {
			if attr.Val == "tb-key-sku" {
				kind |= taobaoSKU
			}
		}
		for _, class := range strings.Fields(attribute(n, "class")) {
			switch {
			case class == "img-list", class == "thumbPic", class == "img-box", strings.Contains(class, "thumbnailPic--"):
				kind |= taobaoMain
			case class == "tb-sku", class == "item-sku", class == "J_TSaleProp", class == "skuItem", class == "sku-img-list", strings.HasPrefix(class, "SkuContent--valueItem--"), strings.Contains(class, "skuWrapper"):
				kind |= taobaoSKU
			case class == "detail-box", class == "descV8-richtext", class == "descV8-container", class == "descV6-container":
				kind |= taobaoDesc
			}
		}
	}
	return kind
}

func taobaoMediaHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"alicdn.com", "taobaocdn.com", "tbcdn.cn"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func taobaoDescriptionHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "dsc.taobaocdn.com", "desc.alicdn.com", "osdsc.alicdn.com", "dscnew.taobao.com":
		return true
	}
	return false
}

func taobaoAddImage(result *Discovery, seen map[string]bool, page *url.URL, raw string) bool {
	u, err := crawlURL(page, raw)
	if err != nil || !taobaoMediaHost(u) || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return false
	}
	if u.Scheme == "http" {
		u.Scheme, u.Host = "https", u.Hostname()
	}
	lower := strings.ToLower(u.Path)
	nameStart := strings.LastIndex(lower, "/") + 1
	for _, ext := range []string{".jpg_", ".jpeg_", ".png_", ".gif_"} {
		if pos := strings.Index(lower[nameStart:], ext); pos >= 0 {
			at := nameStart + pos
			u.Path, u.RawPath = u.Path[:at+len(ext)-1], ""
			break
		}
	}
	if !seen[u.String()] {
		seen[u.String()] = true
		item := Item{URL: u.String()}
		if ref := imageReferer(page.String(), item.URL); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	return true
}

// shortcut: 规格图片对象最多遍历 64 层；遇到更深的实际结构时调整上限。
func taobaoPropertyImages(value any, depth int, add func(string) bool) error {
	if depth > 64 {
		return errors.New("淘宝商品规格图片嵌套过深")
	}
	var issues []error
	switch v := value.(type) {
	case string:
		if v != "" && !add(v) {
			issues = append(issues, errors.New("淘宝商品规格图片地址无效或不属于受支持站点"))
		}
	case []any:
		for _, child := range v {
			issues = append(issues, taobaoPropertyImages(child, depth+1, add))
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			issues = append(issues, taobaoPropertyImages(v[key], depth+1, add))
		}
	}
	return errors.Join(issues...)
}

func taobaoDescription(body []byte, source *url.URL, add func(string) bool) error {
	if strings.Contains(strings.ToLower(source.Path), "lang^gbk") {
		reader, err := charset.NewReaderLabel("gbk", bytes.NewReader(body))
		if err != nil {
			return err
		}
		body, err = io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
		if err != nil || len(body) > maxPageBytes {
			return errors.New("淘宝商品描述超过大小上限或编码无效")
		}
	}
	text := strings.TrimSpace(string(body))
	if loc := offer1688DescriptionPattern.FindStringIndex(text); loc != nil {
		tail := strings.TrimSpace(text[loc[1]:])
		value, used, ok := offer1688String(tail)
		if !ok || !offer1688LiteralEnd(tail[used:]) {
			return errors.New("淘宝商品描述格式无效")
		}
		text = value
	} else if !strings.HasPrefix(text, "<") {
		return errors.New("淘宝商品描述格式不支持")
	}
	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return err
	}
	var issues []error
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "img" {
			return
		}
		for _, raw := range imageCandidates(n) {
			if !add(raw) {
				issues = append(issues, errors.New("淘宝商品详情图片地址无效或不属于受支持站点"))
			}
		}
	})
	return errors.Join(issues...)
}
