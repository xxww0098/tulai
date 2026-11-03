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

var imgurIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{5,8}$`)
var imgurPostDataPattern = regexp.MustCompile(`\bwindow\s*\.\s*postDataJSON\s*=\s*`)
var imgurLegacyPattern = regexp.MustCompile(`\bwidgetFactory\s*\.\s*mergeConfig\s*\(\s*['"]gallery['"]\s*,\s*`)

type imgurMedia struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	MIMEType string `json:"mime_type"`
	Hash     string `json:"hash"`
	Ext      string `json:"ext"`
}

type imgurPost struct {
	ID         string       `json:"id"`
	Title      string       `json:"title"`
	ImageCount *int         `json:"image_count"`
	Media      []imgurMedia `json:"media"`
	Images     []imgurMedia `json:"images"`
}

func imgurPageID(u *url.URL) (string, bool) {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "imgur.com", "www.imgur.com", "m.imgur.com":
	default:
		return "", false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	var album bool
	switch {
	case len(parts) == 1:
		id = parts[0]
		switch strings.ToLower(id) {
		case "about", "account", "explore", "gallery", "images", "login", "logout", "popular", "privacy", "random", "register", "search", "settings", "signin", "signup", "support", "terms", "topics", "upload":
			return "", false
		}
	case len(parts) == 2 && (parts[0] == "a" || parts[0] == "gallery"):
		id, album = parts[1], true
		if i := strings.LastIndexByte(id, '-'); i >= 0 {
			id = id[i+1:]
		}
	}
	if !imgurIDPattern.MatchString(id) {
		return "", false
	}
	return id, album
}

func isImgurPage(u *url.URL) bool {
	id, _ := imgurPageID(u)
	return id != ""
}

// discoverImgurPage reads only the current public post, without executing page scripts.
func discoverImgurPage(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
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
	wantID, album := imgurPageID(root)
	gotID, _ := imgurPageID(final)
	if doc == nil || wantID == "" || gotID != wantID || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Imgur 页面不可用或跳转到了其他帖子")
	}
	return imgurPostPage(ctx, doc, final, wantID, album)
}

// imgurPage shares structured post extraction with the rendered browser flow.
func imgurPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	id, album := imgurPageID(page)
	if id == "" || doc == nil {
		return Discovery{IsPage: true}, errors.New("Imgur 帖子页面无效")
	}
	return imgurPostPage(ctx, doc, page, id, album)
}

func imgurPostPage(ctx context.Context, doc *html.Node, page *url.URL, wantID string, album bool) (Discovery, error) {
	result := Discovery{IsPage: true}
	var post *imgurPost
	var cover string
	var parseErr error
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
				if cover == "" {
					cover = attribute(n, "content")
				}
			case "og:title":
				result.Title = firstNonEmpty(attribute(n, "content"), result.Title)
			}
		}
		if n.Data != "script" || post != nil {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, match := range offer1688CodeMatches(text, imgurPostDataPattern) {
				tail := strings.TrimSpace(text[match[1]:])
				value, used, ok := offer1688String(tail)
				if !ok || !offer1688LiteralEnd(tail[used:]) {
					parseErr = errors.Join(parseErr, errors.New("Imgur 帖子数据格式无效"))
					continue
				}
				var candidate imgurPost
				if json.Unmarshal([]byte(value), &candidate) != nil || candidate.ID != wantID || candidate.Media == nil {
					parseErr = errors.Join(parseErr, errors.New("Imgur 帖子数据缺失或不属于当前帖子"))
					continue
				}
				post = &candidate
				return
			}
			for _, match := range offer1688CodeMatches(text, imgurLegacyPattern) {
				var candidate imgurPost
				decoder := json.NewDecoder(strings.NewReader(text[match[1]:]))
				if decoder.Decode(&candidate) != nil || candidate.Images == nil || (candidate.ID != "" && candidate.ID != wantID) {
					parseErr = errors.Join(parseErr, errors.New("Imgur 旧版相册数据无效"))
					continue
				}
				post = &candidate
				return
			}
		}
	})
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if post == nil {
		if image, ok := imgurImageURL(page, cover); ok && imagePath(image.Path) {
			base := path.Base(image.Path)
			matches := strings.TrimSuffix(base, path.Ext(base)) == wantID
			if album || matches {
				result.Items = append(result.Items, imgurItem(page, image))
			}
			if !album && matches {
				return result, parseErr
			}
		}
		return result, errors.Join(parseErr, errors.New("Imgur 未提供完整帖子数据，结果可能不完整"))
	}
	if post.Title != "" {
		result.Title = post.Title
	}
	media := post.Media
	if media == nil {
		media = post.Images
	}
	seen := map[string]bool{}
	missing := 0
	for _, image := range media {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		kind, mime := strings.ToLower(image.Type), strings.ToLower(image.MIMEType)
		if kind == "video" || strings.HasPrefix(mime, "video/") {
			continue
		}
		raw := image.URL
		if raw == "" && imgurIDPattern.MatchString(image.Hash) && imagePath("x"+image.Ext) {
			raw = "https://i.imgur.com/" + image.Hash + image.Ext
		}
		u, ok := imgurImageURL(page, raw)
		if !ok || (mime != "" && !strings.HasPrefix(mime, "image/")) || (mime == "" && !imagePath(u.Path)) {
			missing++
			continue
		}
		if seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		result.Items = append(result.Items, imgurItem(page, u))
	}
	if missing > 0 {
		parseErr = errors.Join(parseErr, fmt.Errorf("%d 张 Imgur 图片地址无效或不属于 Imgur 图床", missing))
	}
	if post.ImageCount == nil || *post.ImageCount < 0 || *post.ImageCount != len(media) {
		parseErr = errors.Join(parseErr, errors.New("Imgur 未返回完整的图片数量或媒体列表，结果可能不完整"))
	}
	if len(result.Items) == 0 && parseErr == nil {
		parseErr = errors.New("Imgur 帖子中没有可下载的图片")
	}
	return result, parseErr
}

func imgurImageURL(page *url.URL, raw string) (*url.URL, bool) {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "i.imgur.com") || u.Path == "" || u.Path == "/" || (u.Port() != "" && u.Port() != "443") {
		return nil, false
	}
	return u, true
}

func imgurItem(page, image *url.URL) Item {
	item := Item{URL: image.String()}
	if referer := imageReferer(page.String(), item.URL); referer != "" {
		item.Headers = map[string]string{"Referer": referer}
	}
	return item
}
