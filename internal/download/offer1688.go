package download

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

var offer1688IDPattern = regexp.MustCompile(`^[0-9]{1,24}$`)
var offer1688ListPattern = regexp.MustCompile(`(?:"offerImgList"|'offerImgList'|\bofferImgList)\s*:`)
var offer1688SKUPropsPattern = regexp.MustCompile(`"skuProps"\s*:`)
var offer1688DataPattern = regexp.MustCompile(`\b(?:var|let|const)\s+iDetailData\s*=\s*`)
var offer1688FieldPattern = regexp.MustCompile(`(?:"(imageUrl|detailUrl)"|'(imageUrl|detailUrl)'|\b(imageUrl|detailUrl))\s*:`)
var offer1688DescriptionPattern = regexp.MustCompile(`^(?:var\s+)?(?:desc|content)\s*=\s*`)

func offer1688ID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		return ""
	}
	var id string
	switch strings.ToLower(u.Hostname()) {
	case "detail.1688.com", "m.1688.com":
		for _, prefix := range []string{"/offer/", "/buyer/offerdetail/"} {
			if strings.HasPrefix(u.Path, prefix) && strings.HasSuffix(u.Path, ".html") {
				id = strings.TrimSuffix(strings.TrimPrefix(u.Path, prefix), ".html")
			}
		}
	case "detail.m.1688.com":
		if u.Path == "/page/index.htm" {
			id = u.Query().Get("offerId")
		}
	}
	if !offer1688IDPattern.MatchString(id) {
		return ""
	}
	return id
}

func is1688Offer(u *url.URL) bool { return offer1688ID(u) != "" }

func discover1688Offer(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || offer1688ID(final) != offer1688ID(root) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("1688 商品页面不可用或跳转到其他商品，可能需要登录或安全验证")
	}
	return offer1688Collect(ctx, getter, final, doc)
}

// offer1688Collect handles both fetched HTML and the user's rendered product page.
func offer1688Collect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !is1688Offer(page) || doc == nil {
		return result, errors.New("1688 商品页面或下载客户端无效")
	}
	result, detail, err := offer1688Page(doc, page)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if detail != "" {
		detailURL, parseErr := crawlURL(page, detail)
		if parseErr != nil || !offer1688MediaHost(detailURL) || (page.Scheme == "https" && detailURL.Scheme != "https") {
			err = errors.Join(err, errors.New("1688 商品详情地址无效，结果可能不完整"))
		} else {
			body, requestErr := album1688Request(ctx, getter, detailURL, page)
			if requestErr == nil {
				requestErr = offer1688AddDescription(&result, detailURL, body)
			}
			if requestErr != nil {
				if errors.Is(requestErr, context.Canceled) {
					return result, context.Canceled
				}
				if errors.Is(requestErr, context.DeadlineExceeded) {
					return result, context.DeadlineExceeded
				}
				var transfer *TransferError
				if errors.As(requestErr, &transfer) {
					requestErr = &TransferError{Err: errors.New("商品描述请求失败")}
				}
				err = errors.Join(err, fmt.Errorf("读取 1688 商品描述失败，结果可能不完整: %w", requestErr))
			}
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if len(result.Items) == 0 && err == nil {
		err = errors.New("1688 商品页面没有可识别的图片，可能需要登录或安全验证")
	}
	return result, err
}

func offer1688Page(doc *html.Node, page *url.URL) (Discovery, string, error) {
	result := Discovery{IsPage: true}
	seen := map[string]bool{}
	detail, tfsDetail := "", ""
	description := false
	var problems []error
	add := func(raw string) bool { return offer1688AddImage(&result, seen, page, raw) }
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "img" {
			if gallery, descImage := offer1688Gallery(n); gallery {
				for _, key := range []string{"swipe-lazy-src", "data-lazy-src", "data-src", "src"} {
					if raw := attribute(n, key); possibleImage(raw) {
						if add(raw) {
							description = description || descImage
							break
						}
					}
				}
			}
		}
		if raw := attribute(n, "data-tfs-url"); raw != "" && tfsDetail == "" {
			tfsDetail = raw
		}
		if n.Data != "script" {
			return
		}
		for _, raw := range jsonLDImages(n) {
			add(raw)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, match := range offer1688CodeMatches(text, offer1688ListPattern) {
				var images []json.RawMessage
				tail := text[match[1]:]
				decoder := json.NewDecoder(strings.NewReader(tail))
				if decoder.Decode(&images) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					problems = append(problems, errors.New("无法读取 1688 商品主图列表"))
					continue
				}
				for _, raw := range images {
					var image string
					if json.Unmarshal(raw, &image) != nil {
						var item struct {
							Original string `json:"originalImageURI"`
						}
						if json.Unmarshal(raw, &item) == nil {
							image = item.Original
						}
					}
					if image == "" || !add(image) {
						problems = append(problems, errors.New("1688 商品主图地址无效或不属于受支持站点"))
					}
				}
			}
			for _, match := range offer1688CodeMatches(text, offer1688SKUPropsPattern) {
				var sku any
				tail := text[match[1]:]
				decoder := json.NewDecoder(strings.NewReader(tail))
				if decoder.Decode(&sku) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					problems = append(problems, errors.New("无法读取 1688 商品规格列表"))
					continue
				}
				if err := offer1688SKUImages(sku, 0, add); err != nil {
					problems = append(problems, err)
				}
			}
			locations := offer1688CodeMatches(text, offer1688DataPattern)
			if len(locations) == 0 {
				continue
			}
			object, ok := offer1688Object(text[locations[0][1]:])
			if !ok {
				problems = append(problems, errors.New("无法读取 1688 商品规格和描述地址"))
				continue
			}
			for _, field := range offer1688CodeMatches(object, offer1688FieldPattern) {
				name := firstNonEmpty(offer1688Match(object, field[2], field[3]), offer1688Match(object, field[4], field[5]), offer1688Match(object, field[6], field[7]))
				tail := strings.TrimSpace(object[field[1]:])
				value, used, ok := offer1688String(tail)
				if !ok || !offer1688LiteralEnd(tail[used:]) {
					problems = append(problems, fmt.Errorf("无法读取 1688 商品字段 %s", name))
					continue
				}
				if name == "imageUrl" {
					if value != "" && !add(value) {
						problems = append(problems, errors.New("1688 商品规格图片地址无效或不属于受支持站点"))
					}
				} else if detail == "" {
					detail = value
				}
			}
		}
	})
	if tfsDetail == "" && detail == "" && !description {
		problems = append(problems, errors.New("未发现 1688 商品描述图源，结果可能不完整"))
	}
	return result, firstNonEmpty(tfsDetail, detail), errors.Join(problems...)
}

func offer1688Match(s string, start, end int) string {
	if start < 0 {
		return ""
	}
	return s[start:end]
}

// shortcut: 商品容器最多回溯 128 层；实际页面超过此深度时调整。
func offer1688Gallery(n *html.Node) (bool, bool) {
	for depth := 0; n.Parent != nil && depth < 128; depth++ {
		n = n.Parent
		if id := attribute(n, "id"); id == "desc-lazyload-container" {
			return true, true
		} else if id == "J_Detail_ImageSlides" {
			return true, false
		}
		classes := strings.Fields(attribute(n, "class"))
		var nav, tabs, clear bool
		for _, class := range classes {
			if strings.HasPrefix(class, "detail-gallery-turn-wrapper") || strings.HasPrefix(class, "od-gallery-turn-outter-wrapper") {
				return true, false
			}
			nav = nav || class == "nav"
			tabs = tabs || class == "nav-tabs"
			clear = clear || class == "fd-clr"
		}
		if nav && tabs && clear {
			return true, false
		}
	}
	return false, false
}

func offer1688SKUImages(value any, depth int, add func(string) bool) error {
	if depth > 64 {
		return errors.New("1688 商品规格列表嵌套过深")
	}
	var problems []error
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			if err := offer1688SKUImages(child, depth+1, add); err != nil {
				problems = append(problems, err)
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "imageUrl" {
				if v[key] == nil {
					continue
				}
				if raw, ok := v[key].(string); !ok || (raw != "" && !add(raw)) {
					problems = append(problems, errors.New("1688 商品规格图片地址无效或不属于受支持站点"))
				}
			} else if err := offer1688SKUImages(v[key], depth+1, add); err != nil {
				problems = append(problems, err)
			}
		}
	}
	return errors.Join(problems...)
}

func offer1688MediaHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"alicdn.com", "1688.com", "tbcdn.cn", "taobaocdn.com", "tmall.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func offer1688AddImage(result *Discovery, seen map[string]bool, page *url.URL, raw string) bool {
	u, err := crawlURL(page, raw)
	if err != nil || !offer1688MediaHost(u) {
		return false
	}
	if seen[u.String()] {
		return true
	}
	seen[u.String()] = true
	item := Item{URL: u.String()}
	if referer := imageReferer(page.String(), item.URL); referer != "" {
		item.Headers = map[string]string{"Referer": referer}
	}
	result.Items = append(result.Items, item)
	return true
}

func offer1688AddDescription(result *Discovery, source *url.URL, body []byte) error {
	if strings.Contains(strings.ToLower(source.Path), "lang^gbk") {
		reader, err := charset.NewReaderLabel("gbk", bytes.NewReader(body))
		if err != nil {
			return err
		}
		body, err = io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
		if err != nil {
			return err
		}
		if len(body) > maxPageBytes {
			return errors.New("商品描述超过 8 MiB")
		}
	}
	text := strings.TrimSpace(string(body))
	if loc := offer1688DescriptionPattern.FindStringIndex(text); loc != nil {
		tail := strings.TrimSpace(text[loc[1]:])
		value, used, ok := offer1688String(tail)
		if !ok || !offer1688LiteralEnd(tail[used:]) {
			return errors.New("商品描述格式无效")
		}
		text = value
	} else if !strings.HasPrefix(text, "<") {
		return errors.New("不支持的商品描述格式")
	}
	doc, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, item := range result.Items {
		seen[item.URL] = true
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, raw := range imageCandidates(n) {
				offer1688AddImage(result, seen, source, raw)
			}
		}
	})
	return nil
}

func offer1688LiteralEnd(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || strings.ContainsRune(",}];", rune(s[0]))
}

// offer1688CodeMatches ignores keys inside JavaScript strings and comments.
func offer1688CodeMatches(s string, pattern *regexp.Regexp) [][]int {
	matches := pattern.FindAllStringSubmatchIndex(s, -1)
	var result [][]int
	var state byte
	for i, at := 0, 0; i < len(matches); i++ {
		match := matches[i]
		for at < match[0] {
			c := s[at]
			switch state {
			case 0:
				if c == '"' || c == '\'' || c == '`' {
					state = c
				} else if c == '/' && at+1 < len(s) && s[at+1] == '/' {
					state, at = 'l', at+1
				} else if c == '/' && at+1 < len(s) && s[at+1] == '*' {
					state, at = 'b', at+1
				}
			case 'l':
				if c == '\n' || c == '\r' {
					state = 0
				}
			case 'b':
				if c == '*' && at+1 < len(s) && s[at+1] == '/' {
					state, at = 0, at+1
				}
			default:
				if c == '\\' && at+1 < len(s) {
					at++
				} else if c == state {
					state = 0
				}
			}
			at++
		}
		if state == 0 {
			result = append(result, match)
		}
	}
	return result
}

// offer1688Object bounds scanning to the iDetailData object, without evaluating JavaScript.
func offer1688Object(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '{' {
		return "", false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'', '`':
			quote := s[i]
			i++
			for i < len(s) && s[i] != quote {
				if s[i] == '\\' {
					i++
				}
				i++
			}
		case '/':
			if i+1 < len(s) && s[i+1] == '/' {
				if end := strings.IndexByte(s[i+2:], '\n'); end >= 0 {
					i += end + 2
				} else {
					return "", false
				}
			} else if i+1 < len(s) && s[i+1] == '*' {
				if end := strings.Index(s[i+2:], "*/"); end >= 0 {
					i += end + 3
				} else {
					return "", false
				}
			}
		case '{':
			depth++
			if depth > 64 {
				return "", false
			}
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1], true
			}
		}
	}
	return "", false
}

// offer1688String reads a quoted JavaScript string without executing its contents.
func offer1688String(s string) (string, int, bool) {
	if len(s) < 2 || (s[0] != '\'' && s[0] != '"') {
		return "", 0, false
	}
	quote := s[0]
	var quoted strings.Builder
	quoted.WriteByte('"')
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == quote {
			quoted.WriteByte('"')
			var result string
			if json.Unmarshal([]byte(quoted.String()), &result) == nil {
				return result, i + 1, true
			}
			return "", 0, false
		}
		if c == '\\' {
			if i+1 >= len(s) {
				return "", 0, false
			}
			i++
			if s[i] == '\'' {
				quoted.WriteByte('\'')
			} else {
				quoted.WriteByte('\\')
				quoted.WriteByte(s[i])
			}
		} else if c == '"' {
			quoted.WriteString(`\"`)
		} else {
			quoted.WriteByte(c)
		}
	}
	return "", 0, false
}
