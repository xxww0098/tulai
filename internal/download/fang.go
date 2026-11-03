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
	fangHost      = regexp.MustCompile(`^[a-z0-9-]+\.newhouse\.fang\.com$`)
	fangAlbumPath = regexp.MustCompile(`^/loupan/([1-9][0-9]{0,19})/photo/(?:list_[0-9]+\.htm)?$`)
)

func fangAlbumID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		!fangHost.MatchString(strings.ToLower(u.Hostname())) || strings.Contains(u.EscapedPath(), "%") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	m := fangAlbumPath.FindStringSubmatch(u.Path)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

// shortcut: only new-house galleries are parsed until other Fang templates are verified.
func isFangAlbum(u *url.URL) bool { return fangAlbumID(u) != "" }

func discoverFangAlbum(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isFangAlbum(root) {
		return result, errors.New("房天下相册地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || fangAlbumID(final) != fangAlbumID(root) || !strings.EqualFold(root.Hostname(), final.Hostname()) ||
		(root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("房天下相册已跳转到其他内容")
	}
	return fangAlbumPage(ctx, doc, final)
}

// shortcut: cap a single property's gallery at 10000 images until larger galleries are supported.
func fangAlbumPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := fangAlbumID(page)
	if doc == nil || id == "" {
		return result, errors.New("房天下相册页面无效")
	}
	var viewer *html.Node
	identity, multiple := false, false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "a" && attribute(n, "id") == "xfptxq_B03_016" {
			if u, err := crawlURL(page, attribute(n, "href")); err == nil && fangAlbumID(u) == id &&
				strings.EqualFold(page.Hostname(), u.Hostname()) && (page.Scheme != "https" || u.Scheme == "https") {
				identity = true
			}
		}
		if n.Data == "div" && attribute(n, "id") == "album-viewer" {
			if viewer != nil {
				multiple = true
			}
			viewer = n
		}
	})
	if !identity || viewer == nil || multiple {
		return result, errors.New("未能确认房天下相册及楼盘编号，可能需要验证")
	}
	seen := map[string]bool{}
	invalid, limited := false, false
	walkHTML(viewer, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "img" {
			return
		}
		match := false
		for _, class := range strings.Fields(attribute(n, "class")) {
			if class == "bigImg" {
				match = true
				break
			}
		}
		if !match {
			return
		}
		image, err := crawlURL(page, attribute(n, "src"))
		if err != nil || image.User != nil ||
			(image.Port() != "" && !(image.Scheme == "https" && image.Port() == "443") && !(image.Scheme == "http" && image.Port() == "80")) ||
			path.Clean(image.Path) != image.Path || !imagePath(image.Path) {
			invalid = true
			return
		}
		host := strings.ToLower(image.Hostname())
		if !(host == "loupanw.soufunimg.com" && strings.HasPrefix(image.Path, "/viewimage/housepic/")) &&
			!(host == "imgwcs3.soufunimg.com" && strings.HasPrefix(image.Path, "/viewimage/house/")) {
			invalid = true
			return
		}
		image.Scheme, image.Host, image.Fragment = "https", host, ""
		if seen[image.String()] {
			return
		}
		if len(result.Items) == 10000 {
			limited = true
			return
		}
		seen[image.String()] = true
		item := Item{URL: image.String(), RequireImage: true, PublicOnly: true, Metadata: map[string]string{"fang_property_id": id, "title": attribute(n, "alt")}}
		if ref := imageReferer(page.String(), item.URL); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var issues []error
	if len(result.Items) == 0 {
		issues = append(issues, errors.New("房天下相册没有返回可确认的图片"))
	}
	if invalid {
		issues = append(issues, errors.New("房天下部分图片来源未确认，结果可能不完整"))
	}
	if limited {
		issues = append(issues, errors.New("房天下相册超过 10000 张图片上限"))
	}
	return result, errors.Join(issues...)
}
