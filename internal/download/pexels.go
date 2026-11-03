package download

import (
	"context"
	"errors"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var pexelsPhotoPath = regexp.MustCompile(`^/photo/(?:[A-Za-z0-9-]+-)?([1-9][0-9]{0,14})/?$`)

func pexelsPhotoID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(!strings.EqualFold(u.Hostname(), "www.pexels.com") && !strings.EqualFold(u.Hostname(), "pexels.com")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		strings.Contains(u.EscapedPath(), "%") {
		return ""
	}
	match := pexelsPhotoPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

// shortcut: only individual photos are parsed until collection and search formats are verified.
func isPexelsPhoto(u *url.URL) bool { return pexelsPhotoID(u) != "" }

func pexelsImage(raw, id string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "images.pexels.com") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/photos/"+id+"/") ||
		path.Clean(u.Path) != u.Path || !imagePath(u.Path) {
		return nil, false
	}
	u.Fragment = ""
	return u, true
}

func discoverPexels(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isPexelsPhoto(root) {
		return result, errors.New("Pexels 图片地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || pexelsPhotoID(final) != pexelsPhotoID(root) || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Pexels 图片页面已跳转，可能需要登录或安全验证")
	}
	return pexelsPhotoPage(ctx, doc, final)
}

func pexelsPhotoPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := pexelsPhotoID(page)
	if doc == nil || id == "" {
		return result, errors.New("Pexels 图片页面无效")
	}
	var preview, official *url.URL
	mismatch := false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		var identity string
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			identity = attribute(n, "href")
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			identity = attribute(n, "content")
		}
		if identity != "" {
			u, err := url.Parse(identity)
			if err != nil || pexelsPhotoID(page.ResolveReference(u)) != id {
				mismatch = true
			}
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:image") {
			if u, ok := pexelsImage(attribute(n, "content"), id); ok && preview == nil {
				preview = u
			}
		}
		if n.Data == "a" {
			if u, ok := pexelsImage(attribute(n, "href"), id); ok && u.Query().Get("dl") != "" && official == nil {
				official = u
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if mismatch {
		return result, errors.New("Pexels 图片页面与请求的作品不匹配")
	}
	if official == nil && preview == nil {
		return result, errors.New("Pexels 未返回匹配的图片，可能需要登录或安全验证")
	}
	item := Item{RequireImage: true, PublicOnly: true, Metadata: map[string]string{"pexels_photo_id": id}}
	if official != nil {
		item.URL = official.String()
	} else {
		original := *preview
		params, err := url.ParseQuery(original.RawQuery)
		known := err == nil
		for key, values := range params {
			if len(values) != 1 {
				known = false
			}
			switch key {
			case "auto", "dpr", "h", "w", "fit", "crop", "cs":
			default:
				known = false
			}
		}
		if known && original.RawQuery != "" {
			original.RawQuery, original.ForceQuery = "", false
			item.AlternateURLs = []string{preview.String()}
		}
		item.URL = original.String()
	}
	if ref := imageReferer(page.String(), item.URL); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	result.Items = append(result.Items, item)
	return result, nil
}
