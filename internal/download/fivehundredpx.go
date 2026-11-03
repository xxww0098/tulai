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
	fiveHundredIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{4,40}$`)
	fiveHundredPhotoPattern = regexp.MustCompile(`(?:["']getPhotoById["']|\bgetPhotoById)\s*:\s*`)
	fiveHundredNumber       = regexp.MustCompile(`^[0-9]{1,24}$`)
)

type fiveHundredRef struct {
	id      string
	gallery bool
}

type fiveHundredPhoto struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URLs  struct {
		Size600  string `json:"size_600"`
		Size1024 string `json:"size_1024"`
		Size2048 string `json:"size_2048"`
		Size4K   string `json:"size_4k"`
	} `json:"urls"`
}

func fiveHundredPageRef(u *url.URL) fiveHundredRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fiveHundredRef{}
	}
	switch strings.ToLower(u.Hostname()) {
	case "500px.com", "www.500px.com", "web.500px.com", "500px.com.cn", "www.500px.com.cn":
	default:
		return fiveHundredRef{}
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return fiveHundredRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || len(parts) > 3 || !fiveHundredIDPattern.MatchString(parts[1]) {
		return fiveHundredRef{}
	}
	switch parts[0] {
	case "photo":
		return fiveHundredRef{id: parts[1]}
	case "gallery":
		if len(parts) == 2 {
			return fiveHundredRef{id: parts[1], gallery: true}
		}
	}
	return fiveHundredRef{}
}

func is500pxPage(u *url.URL) bool { return fiveHundredPageRef(u).id != "" }

func discover500px(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	want := fiveHundredPageRef(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || want.id == "" {
		return result, errors.New("500px 页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || fiveHundredPageRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("500px 页面不可用或跳转到了其他作品或相册")
	}
	return collect500px(ctx, doc, final)
}

// collect500px uses the current page's known image data without private API calls.
func collect500px(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := fiveHundredPageRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ref.id == "" || doc == nil {
		return result, errors.New("500px 页面无效")
	}
	var canonical, cover string
	var photo *fiveHundredPhoto
	var high []string
	var cards []string
	seenCards := make(map[*html.Node]bool)
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
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
			case "og:title":
				result.Title = firstNonEmpty(attribute(n, "content"), result.Title)
			}
		}
		if n.Data == "img" {
			raw := firstNonEmpty(attribute(n, "data-src"), attribute(n, "src"))
			if !ref.gallery && strings.EqualFold(attribute(n, "fetchpriority"), "high") {
				high = append(high, raw)
			}
			if ref.gallery {
				if card := fiveHundredCard(n); card != nil && !seenCards[card] {
					if _, _, ok := fiveHundredImageURL(page, raw); ok {
						seenCards[card] = true
						cards = append(cards, raw)
					}
				}
			}
		}
		if n.Data != "script" || ref.gallery || photo != nil {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, loc := range offer1688CodeMatches(text, fiveHundredPhotoPattern) {
				var candidate fiveHundredPhoto
				if json.NewDecoder(strings.NewReader(text[loc[1]:])).Decode(&candidate) == nil && candidate.ID == ref.id {
					photo = &candidate
					return
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	seen := make(map[string]bool)
	add := func(image *url.URL) {
		if seen[image.String()] {
			return
		}
		seen[image.String()] = true
		item := Item{URL: image.String()}
		if referer := imageReferer(page.String(), item.URL); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	if !ref.gallery && photo != nil {
		result.Title = firstNonEmpty(photo.Title, result.Title)
		for _, raw := range []string{photo.URLs.Size4K, photo.URLs.Size2048, photo.URLs.Size1024, photo.URLs.Size600} {
			if image, asset, ok := fiveHundredImageURL(page, raw); ok && (!fiveHundredNumber.MatchString(ref.id) || asset == ref.id) {
				add(image)
				return result, nil
			}
		}
		return result, errors.New("500px 作品图片地址无效或不属于当前作品")
	}
	canonicalURL, err := crawlURL(page, canonical)
	if err != nil || fiveHundredPageRef(canonicalURL) != ref {
		return result, errors.New("500px 作品或相册数据尚未加载，可能需要登录")
	}
	if ref.gallery {
		for _, raw := range cards {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if image, _, ok := fiveHundredImageURL(page, raw); ok {
				add(image)
			}
		}
		return result, errors.New("500px 相册按需加载，当前只包含已加载的照片，结果可能不完整")
	}
	image, asset, ok := fiveHundredImageURL(page, cover)
	if !ok || (fiveHundredNumber.MatchString(ref.id) && asset != ref.id) {
		return result, errors.New("500px 作品图片尚未加载，可能需要登录")
	}
	for _, raw := range high {
		if large, largeAsset, ok := fiveHundredImageURL(page, raw); ok && largeAsset == asset {
			image = large
			break
		}
	}
	add(image)
	return result, errors.New("500px 仅取得当前页面可见尺寸，原图和更高尺寸未确认")
}

func fiveHundredCard(n *html.Node) *html.Node {
	for depth := 0; n != nil && depth < 128; depth++ {
		if attribute(n, "data-track-id") == "card_body" && attribute(n, "data-track-content-type") == "1" && fiveHundredIDPattern.MatchString(attribute(n, "data-track-content-id")) {
			return n
		}
		n = n.Parent
	}
	return nil
}

func fiveHundredImageURL(page *url.URL, raw string) (*url.URL, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Path == "" || path.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "//") {
		return nil, "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
		return nil, "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	switch strings.ToLower(u.Hostname()) {
	case "drscdn.500px.org", "cdn-resize-prod-cn.500px.me":
		if len(parts) >= 3 && parts[0] == "photo" {
			id = parts[1]
		}
	case "pcdn.500px.net", "ppcdn.500px.org":
		if len(parts) >= 3 {
			id = parts[0]
		}
	}
	if !fiveHundredNumber.MatchString(id) {
		return nil, "", false
	}
	return u, id, true
}
