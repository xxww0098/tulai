package download

import (
	"bytes"
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
	cnuDigits       = regexp.MustCompile(`^[0-9]{1,24}$`)
	cnuPathSegments = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
)

const maxCNUImages = 10000

func cnuPageID(page *url.URL, kind string) string {
	if page == nil || page.User != nil || (page.Scheme != "http" && page.Scheme != "https") ||
		(page.Port() != "" && !(page.Scheme == "http" && page.Port() == "80") && !(page.Scheme == "https" && page.Port() == "443")) ||
		strings.Contains(page.EscapedPath(), "%") {
		return ""
	}
	switch strings.ToLower(page.Hostname()) {
	case "cnu.cc", "www.cnu.cc":
	default:
		return ""
	}
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != kind || !cnuDigits.MatchString(parts[1]) || strings.Trim(parts[1], "0") == "" {
		return ""
	}
	return parts[1]
}

func isCNUWork(page *url.URL) bool { return cnuPageID(page, "works") != "" }
func isCNUUser(page *url.URL) bool { return cnuPageID(page, "users") != "" }

func cnuUserUnsupported() error {
	return errors.New("CNU 作者页作品列表尚未完整支持，请打开单个作品后采集")
}

// cnuImageURL accepts only site-supplied relative image paths and never lets the
// page choose a host, query, userinfo, or parent path.
func cnuImageURL(raw string, legacy bool) (string, bool) {
	if len(raw) == 0 || len(raw) > 4096 || raw != strings.TrimSpace(raw) || !cnuPathSegments.MatchString(raw) {
		return "", false
	}
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return "", false
		}
	}
	switch strings.ToLower(path.Ext(raw)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
	default:
		return "", false
	}
	if legacy {
		// The site's own script uses this legacy forum path for height="auto".
		return "http://img.cnu.cc/forum/" + raw, true
	}
	return "http://imgoss.cnu.cc/" + raw + "?x-oss-process=style/content", true
}

func discoverCNUWork(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := cnuPageID(page, "works")
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("CNU 作品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		if page.Scheme == "https" {
			return result, fmt.Errorf("读取 CNU HTTPS 页面失败；公开作品可尝试站点 HTTP 地址: %w", err)
		}
		return result, fmt.Errorf("读取 CNU 作品失败: %w", err)
	}
	if doc == nil || final == nil || cnuPageID(final, "works") != id || (page.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("CNU 作品已跳转，可能需要登录或安全验证")
	}
	return cnuWorkPage(ctx, doc, final)
}

// cnuWorkPage reads the current work's bounded imgs_json array without running
// site JavaScript or scanning page thumbnails, avatars, and recommendations.
func cnuWorkPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := cnuPageID(page, "works")
	if id == "" || doc == nil {
		return result, errors.New("CNU 作品页面无效")
	}
	var payload string
	var found, malformed bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "div" || attribute(n, "id") != "imgs_json" {
			return
		}
		if found {
			malformed = true
			return
		}
		found = true
		var text strings.Builder
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode || text.Len()+len(child.Data) > maxPageBytes {
				malformed = true
				return
			}
			text.WriteString(child.Data)
		}
		payload = text.String()
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !found || malformed {
		return result, errors.New("未能读取 CNU 作品图片数据，可能需要登录或安全验证")
	}
	trimmed := bytes.TrimSpace([]byte(payload))
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return result, errors.New("CNU 作品图片数据格式无效")
	}
	var entries []json.RawMessage
	if json.Unmarshal(trimmed, &entries) != nil {
		return result, errors.New("CNU 作品图片数据格式无效")
	}
	seen := map[string]bool{}
	invalid, limited, legacy := false, false, false
	for _, raw := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var entry struct {
			Img    string          `json:"img"`
			Height json.RawMessage `json:"height"`
		}
		if json.Unmarshal(raw, &entry) != nil || entry.Img == "" {
			invalid = true
			continue
		}
		var height string
		_ = json.Unmarshal(entry.Height, &height)
		old := height == "auto"
		image, ok := cnuImageURL(entry.Img, old)
		if !ok {
			invalid = true
			continue
		}
		legacy = legacy || old
		if seen[image] {
			continue
		}
		if len(result.Items) >= maxCNUImages {
			limited = true
			break
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"cnu_work_id": id}}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	var issues []error
	if len(entries) == 0 {
		issues = append(issues, errors.New("CNU 作品未返回图片，可能需要登录或等待加载"))
	}
	if invalid {
		issues = append(issues, errors.New("部分 CNU 图片路径无效，结果可能不完整"))
	}
	if limited {
		issues = append(issues, fmt.Errorf("CNU 作品超过 %d 张图片上限", maxCNUImages))
	}
	if legacy {
		issues = append(issues, errors.New("CNU 包含旧版图片地址，该分支尚未在线验证"))
	}
	return result, errors.Join(issues...)
}
