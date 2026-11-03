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

var (
	onexPhotoPath = regexp.MustCompile(`^/photo/([1-9][0-9]{0,19})/?$`)
	onexNumericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
)

func onexHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "1x.com", "www.1x.com", "gallery.1x.com":
		return true
	}
	return false
}
func onexPhotoID(u *url.URL) string {
	if !onexHost(u) || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return ""
	}
	m := onexPhotoPath.FindStringSubmatch(u.Path)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}
func isOnexPhoto(u *url.URL) bool { return onexPhotoID(u) != "" }

func isOnexGallery(u *url.URL) bool {
	if !onexHost(u) || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return false
	}
	switch strings.TrimSuffix(u.Path, "/") {
	case "/photos", "/photos/latest/published":
		return true
	}
	return false
}

func onexImageURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "1x.com") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/images/user/") ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		return "", false
	}
	return u.String(), true
}

func discoverOnex(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := onexPhotoID(root)
	if getter == nil || (id == "" && !isOnexGallery(root)) {
		return result, errors.New("1x 作品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("1x 作品页面不可用或跳转到其他作品")
	}
	if id == "" {
		if !isOnexGallery(final) || strings.TrimSuffix(final.Path, "/") != strings.TrimSuffix(root.Path, "/") {
			return result, errors.New("1x 图库页面不可用或发生跳转")
		}
		return onexGalleryPage(ctx, doc, final)
	}
	if onexPhotoID(final) != id {
		return result, errors.New("1x 作品页面不可用或跳转到其他作品")
	}
	return onexPhotoPage(ctx, doc, final)
}

func onexAncestorID(n *html.Node, want string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && attribute(p, "id") == want {
			return true
		}
	}
	return false
}

// Script 1 selects img-{photoID}. Also require the matching container and
// og:url so that recommendations, avatars, and other works are not returned.
func onexPhotoPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := onexPhotoID(page)
	if doc == nil || id == "" {
		return result, errors.New("1x 作品页面无效")
	}
	var identity, mismatch, invalid bool
	var image string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "meta" {
			switch strings.ToLower(attribute(n, "property")) {
			case "og:url":
				raw := attribute(n, "content")
				u, err := url.Parse(raw)
				if raw == "" || err != nil || onexPhotoID(page.ResolveReference(u)) != id {
					mismatch = true
				} else {
					identity = true
				}
			case "og:title":
				if result.Title == "" {
					result.Title = attribute(n, "content")
				}
			}
		}
		if n.Data != "img" || attribute(n, "id") != "img-"+id || !onexAncestorID(n, "imgcontainer-"+id) {
			return
		}
		u, ok := onexImageURL(attribute(n, "src"))
		if !ok {
			invalid = true
			return
		}
		if image != "" && image != u {
			invalid = true
			return
		}
		image = u
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !identity || mismatch {
		return result, errors.New("1x 页面未能确认当前作品编号")
	}
	if invalid {
		return result, errors.New("1x 展示图地址不唯一或无法验证")
	}
	if image == "" {
		return result, errors.New("1x 未提供可确认的公开展示图，页面可能需要登录或访问授权")
	}
	item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"onex_photo_id": id}}
	// Send no page query to the image host, even when the two share an origin.
	item.Headers = map[string]string{"Referer": page.Scheme + "://" + page.Host + "/"}
	result.Items = append(result.Items, item)
	return result, errors.New("1x 当前返回页面展示图，分辨率可能低于原图")
}

// Only the gallery entries already rendered under photos-feed are collected.
// The site's stateful pagination may require login and is not inferred here.
func onexGalleryPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || !isOnexGallery(page) {
		return result, errors.New("1x 图库页面无效")
	}
	var ids []string
	images := make(map[string]string)
	conflicts := make(map[string]bool)
	invalid := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "img" || !strings.HasPrefix(attribute(n, "id"), "img-") || !onexAncestorID(n, "photos-feed") {
			return
		}
		id := strings.TrimPrefix(attribute(n, "id"), "img-")
		if !onexNumericID.MatchString(id) || !onexAncestorID(n, "imgcontainer-"+id) {
			return
		}
		image, ok := onexImageURL(attribute(n, "src"))
		if !ok {
			invalid = true
			return
		}
		if previous, exists := images[id]; exists {
			if previous != image {
				conflicts[id] = true
			}
			return
		}
		if len(ids) >= 10000 {
			invalid = true
			return
		}
		ids = append(ids, id)
		images[id] = image
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for _, id := range ids {
		if conflicts[id] {
			invalid = true
			continue
		}
		result.Items = append(result.Items, Item{URL: images[id], RequireImage: true, PublicOnly: true,
			Headers:  map[string]string{"Referer": page.Scheme + "://" + page.Host + "/"},
			Metadata: map[string]string{"onex_photo_id": id}})
	}
	if len(result.Items) == 0 {
		return result, errors.New("1x 图库尚未加载作品，请在下载浏览器中打开页面")
	}
	if invalid {
		return result, errors.New("1x 部分图库图片无法验证；仅采集了当前加载的作品")
	}
	return result, errors.New("1x 仅采集了当前加载的图库作品，其他页面尚未读取")
}
