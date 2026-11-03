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
	instagramCodePattern  = regexp.MustCompile(`^[A-Za-z0-9_-]{5,30}$`)
	instagramOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9._]{1,30}$`)
	instagramCDNPattern   = regexp.MustCompile(`^(?:scontent(?:-[a-z0-9-]+)?\.cdninstagram\.com|scontent(?:-[a-z0-9-]+)?\.fbcdn\.net|instagram\.[a-z0-9-]+\.(?:fna\.)?fbcdn\.net)$`)
	instagramMediaPattern = regexp.MustCompile(`(?:["']shortcode_media["']|\bshortcode_media)\s*:\s*`)
)

type instagramMedia struct {
	Shortcode string `json:"shortcode"`
	Code      string `json:"code"`
	Type      string `json:"__typename"`
	IsVideo   *bool  `json:"is_video"`
	MediaType int    `json:"media_type"`
	URL       string `json:"display_url"`
	Source    string `json:"display_src"`
	Resources []struct {
		URL    string `json:"src"`
		Width  int    `json:"config_width"`
		Height int    `json:"config_height"`
	} `json:"display_resources"`
	Versions struct {
		Candidates []struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"candidates"`
	} `json:"image_versions2"`
	Sidecar *struct {
		Count    *int `json:"count"`
		PageInfo struct {
			HasNext *bool `json:"has_next_page"`
		} `json:"page_info"`
		Edges []struct {
			Node instagramMedia `json:"node"`
		} `json:"edges"`
	} `json:"edge_sidecar_to_children"`
}

func instagramPostID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "instagram.com", "www.instagram.com", "m.instagram.com":
	default:
		return ""
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 3 && instagramOwnerPattern.MatchString(parts[0]) {
		parts = parts[1:]
	}
	if len(parts) != 2 || (parts[0] != "p" && parts[0] != "reel") || !instagramCodePattern.MatchString(parts[1]) {
		return ""
	}
	return parts[1]
}

func isInstagramPost(u *url.URL) bool { return instagramPostID(u) != "" }

func discoverInstagramPost(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := instagramPostID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("Instagram 帖子页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || instagramPostID(final) != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Instagram 帖子页面不可用或跳转到了其他页面，可能需要登录或完成验证")
	}
	return instagramPostPage(ctx, doc, final)
}

// instagramPostPage reads only the matching post, without calling private APIs.
func instagramPostPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := instagramPostID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || doc == nil {
		return result, errors.New("Instagram 帖子页面无效")
	}
	var media *instagramMedia
	var cover, canonical string
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" {
			switch strings.ToLower(attribute(n, "property")) {
			case "og:image":
				cover = firstNonEmpty(cover, attribute(n, "content"))
			case "og:url":
				canonical = firstNonEmpty(canonical, attribute(n, "content"))
			}
		}
		if n.Data != "script" || media != nil {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, loc := range offer1688CodeMatches(text, instagramMediaPattern) {
				var candidate instagramMedia
				if json.NewDecoder(strings.NewReader(text[loc[1]:])).Decode(&candidate) == nil && firstNonEmpty(candidate.Shortcode, candidate.Code) == id {
					media = &candidate
					return
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if media == nil {
		canonicalURL, err := crawlURL(page, canonical)
		if err == nil && instagramPostID(canonicalURL) == id {
			if image, ok := instagramImageURL(page, cover); ok {
				result.Items = append(result.Items, instagramItem(page, image))
			}
		}
		return result, errors.New("Instagram 未提供完整帖子数据，可能需要登录或加载动态内容")
	}
	var problems []error
	seen := make(map[string]bool)
	add := func(m instagramMedia) {
		if m.IsVideo != nil && *m.IsVideo || m.Type == "GraphVideo" || m.MediaType == 2 {
			return
		}
		if m.Type != "GraphImage" && m.MediaType != 1 && (m.IsVideo == nil || *m.IsVideo) {
			problems = append(problems, errors.New("Instagram 媒体类型不明确，结果可能不完整"))
			return
		}
		image, ok := instagramBestImage(page, m)
		if !ok {
			problems = append(problems, errors.New("Instagram 图片地址不可用或不属于受支持图床"))
			return
		}
		if !seen[image.String()] {
			seen[image.String()] = true
			result.Items = append(result.Items, instagramItem(page, image))
		}
	}
	if media.Type == "GraphSidecar" || media.MediaType == 8 || media.Sidecar != nil {
		if media.Sidecar == nil {
			problems = append(problems, errors.New("Instagram 轮播图片列表未加载，结果可能不完整"))
		} else {
			edges := media.Sidecar.Edges
			// shortcut: limit one post to 500 media entries; raise for larger supported carousels.
			if len(edges) > 500 {
				edges = edges[:500]
				problems = append(problems, errors.New("Instagram 轮播超过 500 个媒体的扫描上限"))
			}
			for _, edge := range edges {
				if err := ctx.Err(); err != nil {
					return result, err
				}
				add(edge.Node)
			}
			count, next := media.Sidecar.Count, media.Sidecar.PageInfo.HasNext
			if (count != nil && (*count < 0 || *count != len(media.Sidecar.Edges))) || (next != nil && *next) || (count == nil && next == nil) {
				problems = append(problems, errors.New("Instagram 轮播没有确认全部媒体，结果可能不完整"))
			}
		}
	} else {
		add(*media)
	}
	if len(result.Items) == 0 && len(problems) == 0 {
		problems = append(problems, errors.New("Instagram 帖子没有可识别的图片"))
	}
	return result, errors.Join(problems...)
}

func instagramBestImage(page *url.URL, media instagramMedia) (*url.URL, bool) {
	for _, raw := range []string{media.URL, media.Source} {
		if image, ok := instagramImageURL(page, raw); ok {
			return image, true
		}
	}
	var best *url.URL
	var size int64
	consider := func(raw string, width, height int) {
		if width < 1 || height < 1 || width > 100000 || height > 100000 {
			return
		}
		image, ok := instagramImageURL(page, raw)
		area := int64(width) * int64(height)
		if ok && (best == nil || area > size) {
			best, size = image, area
		}
	}
	for _, resource := range media.Resources {
		consider(resource.URL, resource.Width, resource.Height)
	}
	for _, candidate := range media.Versions.Candidates {
		consider(candidate.URL, candidate.Width, candidate.Height)
	}
	return best, best != nil
}

func instagramImageURL(page *url.URL, raw string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Path == "" || path.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "//") {
		return nil, false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || !instagramCDNPattern.MatchString(strings.ToLower(u.Hostname())) || !imagePath(u.Path) {
		return nil, false
	}
	return u, true
}

func instagramItem(page, image *url.URL) Item {
	item := Item{URL: image.String()}
	if ref := imageReferer(page.String(), item.URL); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	return item
}
