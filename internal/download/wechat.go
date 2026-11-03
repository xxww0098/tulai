package download

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
)

var wechatShortID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var wechatPictureList = regexp.MustCompile(`\b(?:var\s+picturePageInfoList|window\s*\.\s*picture_page_info_list)\s*=\s*`)
var wechatCover = regexp.MustCompile(`\bvar\s+cover\s*=\s*`)
var wechatTitle = regexp.MustCompile(`\bvar\s+msg_title\s*=\s*`)
var wechatSlice = regexp.MustCompile(`^\.slice\s*\(\s*0\s*,\s*20\s*\)`)

// shortcut: 微信文章最多读取 10000 张图片，超限时返回部分结果。
const maxWechatImages = 10000

func isWechatArticle(u *url.URL) bool {
	if u == nil || u.User != nil || !strings.EqualFold(u.Hostname(), "mp.weixin.qq.com") || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	if u.Path == "/s" {
		q := u.Query()
		return q.Get("__biz") != "" || q.Get("src") != "" || q.Get("signature") != "" || q.Get("sn") != "" || q.Get("tempkey") != ""
	}
	return strings.HasPrefix(u.Path, "/s/") && wechatShortID.MatchString(strings.TrimPrefix(u.Path, "/s/"))
}

func isWechatVerification(u *url.URL) bool {
	if u == nil || u.User != nil || !strings.EqualFold(u.Hostname(), "mp.weixin.qq.com") || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	return u.Path == "/mp/wappoc_appmsgcaptcha"
}

func discoverWechatArticle(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isWechatArticle(root) {
		return result, errors.New("公众号文章地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !isWechatArticle(final) || !strings.EqualFold(root.Hostname(), final.Hostname()) ||
		(root.Scheme == "https" && final.Scheme != "https") || root.Path != final.Path {
		return result, errors.New("公众号文章已跳转或要求安全验证，无法确认当前文章")
	}
	// The old long-link form has stable identifiers even when tracking fields change.
	for _, key := range []string{"__biz", "mid", "idx", "sn"} {
		if root.Query().Get(key) != "" && root.Query().Get(key) != final.Query().Get(key) {
			return result, errors.New("公众号文章编号发生变化，无法确认当前文章")
		}
	}
	return wechatArticlePage(ctx, doc, final)
}

// wechatArticlePage handles the same DOM in a fetched or user-rendered page.
// Script data is parsed as literals and is never executed.
func wechatArticlePage(ctx context.Context, doc *xhtml.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || !isWechatArticle(page) {
		return result, errors.New("公众号文章页面无效")
	}
	seen := map[string]bool{}
	var problems []error
	badImage, limit, invalidList := false, false, false
	add := func(raw string) {
		if limit || strings.TrimSpace(raw) == "" {
			return
		}
		u, err := crawlURL(page, raw)
		if err != nil || u.User != nil || (!domainOrSubdomain(u.Hostname(), "qpic.cn") && !domainOrSubdomain(u.Hostname(), "qlogo.cn")) ||
			(u.Port() != "" && !((u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80"))) {
			badImage = true
			return
		}
		if u.Scheme == "http" {
			u.Scheme, u.Host = "https", u.Hostname()
		}
		key := u.String()
		if seen[key] {
			return
		}
		if len(seen) >= maxWechatImages {
			limit = true
			return
		}
		seen[key] = true
		item := Item{URL: key, RequireImage: true}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", key); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	xhtmlTitle := ""
	walkHTML(doc, func(n *xhtml.Node) {
		if ctx.Err() != nil || limit || n.Type != xhtml.ElementNode {
			return
		}
		if n.Data == "title" && xhtmlTitle == "" {
			xhtmlTitle = nodeText(n)
		}
		if attribute(n, "id") == "activity-name" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "img" && wechatInContent(n) {
			raw := attribute(n, "data-src")
			if raw == "" || !possibleImage(raw) {
				raw = attribute(n, "src")
			}
			if raw != "" && possibleImage(raw) {
				add(raw)
			}
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != xhtml.TextNode {
				continue
			}
			text := child.Data
			if result.Title == "" {
				for _, match := range offer1688CodeMatches(text, wechatTitle) {
					tail := strings.TrimSpace(text[match[1]:])
					if value, used, ok := wechatString(tail); ok && offer1688LiteralEnd(tail[used:]) {
						result.Title = strings.TrimSpace(html.UnescapeString(value))
						break
					}
				}
			}
			for _, match := range offer1688CodeMatches(text, wechatCover) {
				tail := strings.TrimSpace(text[match[1]:])
				value, used, ok := wechatString(tail)
				if ok && offer1688LiteralEnd(tail[used:]) && value != "" {
					add(html.UnescapeString(value))
				}
			}
			for _, match := range offer1688CodeMatches(text, wechatPictureList) {
				tail := strings.TrimSpace(text[match[1]:])
				isString := false
				if value, used, ok := wechatString(tail); ok {
					if !offer1688LiteralEnd(tail[used:]) {
						invalidList = true
						continue
					}
					if value == "" {
						continue
					}
					tail, isString = value, true
				}
				images, parseErr := wechatImagesLiteral(tail)
				unescaped := false
				if parseErr != nil && isString {
					decodedImages, decodedErr := wechatImagesLiteral(html.UnescapeString(tail))
					if decodedErr == nil || len(decodedImages) > len(images) {
						images, parseErr, unescaped = decodedImages, decodedErr, true
					}
				}
				for _, image := range images {
					if !unescaped {
						image = html.UnescapeString(image)
					}
					add(image)
				}
				if parseErr != nil {
					invalidList = true
				}
			}
		}
	})
	result.Title = firstNonEmpty(result.Title, xhtmlTitle)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if limit {
		problems = append(problems, errors.New("公众号图片数量达到上限，结果可能不完整"))
	}
	if badImage {
		problems = append(problems, errors.New("公众号正文含未支持的图片来源，结果可能不完整"))
	}
	if invalidList {
		problems = append(problems, errors.New("公众号图片列表格式无法识别，结果可能不完整"))
	}
	if len(result.Items) == 0 && len(problems) == 0 {
		problems = append(problems, errors.New("未发现公众号文章图片，页面可能需要验证或使用了未支持的格式"))
	}
	return result, errors.Join(problems...)
}

func wechatInContent(n *xhtml.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == xhtml.ElementNode && attribute(p, "id") == "js_content" {
			return true
		}
	}
	return false
}

// wechatString accepts quoted string literals and common JS hex escapes.
func wechatString(s string) (string, int, bool) {
	if len(s) < 2 || (s[0] != '\'' && s[0] != '"') {
		return "", 0, false
	}
	quote := s[0]
	var encoded strings.Builder
	encoded.WriteByte('"')
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == quote {
			encoded.WriteByte('"')
			var result string
			if json.Unmarshal([]byte(encoded.String()), &result) != nil {
				return "", 0, false
			}
			return result, i + 1, true
		}
		if c == '\\' {
			if i+1 >= len(s) {
				return "", 0, false
			}
			i++
			switch s[i] {
			case '\'':
				encoded.WriteByte('\'')
			case 'x':
				if i+2 >= len(s) || !wechatHex(s[i+1]) || !wechatHex(s[i+2]) {
					return "", 0, false
				}
				encoded.WriteString(`\u00`)
				encoded.WriteString(s[i+1 : i+3])
				i += 2
			default:
				encoded.WriteByte('\\')
				encoded.WriteByte(s[i])
			}
		} else if c == '"' {
			encoded.WriteString(`\"`)
		} else {
			encoded.WriteByte(c)
		}
	}
	return "", 0, false
}

func wechatHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// wechatImagesLiteral accepts only an array of object literals, extracting a
// top-level cdn_url from each object and ignoring nested share/watermark data.
func wechatImagesLiteral(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != '[' {
		return nil, errors.New("公众号图片列表不是数组")
	}
	var images []string
	for i := 1; i < len(s); {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == ']' {
			rest := strings.TrimSpace(s[i+1:])
			if strings.HasPrefix(rest, ".slice") {
				end := wechatSlice.FindStringIndex(rest)
				if end == nil {
					return images, errors.New("公众号图片列表含未知表达式")
				}
				rest = strings.TrimSpace(rest[end[1]:])
			}
			if !offer1688LiteralEnd(rest) {
				return images, errors.New("公众号图片列表含未知表达式")
			}
			return images, nil
		}
		if s[i] != '{' {
			return images, errors.New("公众号图片列表含未知格式")
		}
		object, ok := offer1688Object(s[i:])
		if !ok {
			return images, errors.New("公众号图片列表不完整")
		}
		image, ok := wechatObjectURL(object)
		if !ok {
			return images, errors.New("公众号图片列表缺少可识别地址")
		}
		images = append(images, image)
		i += len(object)
	}
	return images, errors.New("公众号图片列表不完整")
}

func wechatObjectURL(s string) (string, bool) {
	depth, arrays, parens := 0, 0, 0
	expectKey := false
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if c == '/' && i+1 < len(s) && (s[i+1] == '/' || s[i+1] == '*') {
			if s[i+1] == '/' {
				end := strings.IndexByte(s[i+2:], '\n')
				if end < 0 {
					return "", false
				}
				i += end + 3
			} else {
				end := strings.Index(s[i+2:], "*/")
				if end < 0 {
					return "", false
				}
				i += end + 4
			}
			continue
		}
		if c == '\'' || c == '"' {
			value, used, ok := wechatString(s[i:])
			if !ok {
				return "", false
			}
			if depth == 1 && arrays == 0 && parens == 0 && expectKey {
				if value == "cdn_url" {
					return wechatFieldValue(s[i+used:])
				}
				expectKey = false
			}
			i += used
			continue
		}
		if depth == 1 && arrays == 0 && parens == 0 && expectKey && (c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			start := i
			i++
			for i < len(s) && (s[i] == '_' || s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9') {
				i++
			}
			if s[start:i] == "cdn_url" {
				return wechatFieldValue(s[i:])
			}
			expectKey = false
			continue
		}
		switch c {
		case '{':
			depth++
			if depth == 1 {
				expectKey = true
			}
		case '}':
			depth--
		case '[':
			arrays++
		case ']':
			arrays--
		case '(':
			parens++
		case ')':
			parens--
		case ',':
			if depth == 1 && arrays == 0 && parens == 0 {
				expectKey = true
			}
		}
		if depth < 0 || arrays < 0 || parens < 0 || depth+arrays+parens > 64 {
			return "", false
		}
		i++
	}
	return "", false
}

func wechatFieldValue(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s[0] != ':' {
		return "", false
	}
	s = strings.TrimSpace(s[1:])
	value, used, ok := wechatString(s)
	if !ok || !offer1688LiteralEnd(s[used:]) || value == "" {
		return "", false
	}
	return value, true
}
