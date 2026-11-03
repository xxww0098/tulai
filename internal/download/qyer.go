package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	qyerID        = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	qyerImagePath = regexp.MustCompile(`^/album/user/[0-9]+/[0-9]+/[A-Za-z0-9_-]+/index/180180$`)
)

// Historical album documents use this exact host and numeric album path.
func qyerAlbumID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.EqualFold(u.Hostname(), "photo.qyer.com") || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return ""
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) != 2 || p[0] != "album" || !qyerID.MatchString(p[1]) {
		return ""
	}
	return p[1]
}
func isQyerAlbum(u *url.URL) bool { return qyerAlbumID(u) != "" }

// The reference selects the site's 180px gallery thumbnails and requests the
// same path at w1080. It does not prove that this variant is the original.
func qyerImage(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || u.RawQuery != "" || u.Fragment != "" || !qyerImagePath.MatchString(u.Path) {
		return "", false
	}
	if !strings.EqualFold(u.Hostname(), "pic.qyer.com") && !strings.EqualFold(u.Hostname(), "pic1.qyer.com") {
		return "", false
	}
	u.Path = strings.TrimSuffix(u.Path, "/index/180180") + "/index/w1080"
	u.RawPath = ""
	return u.String(), true
}
func qyerClasses(n *html.Node, name string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == name {
			return true
		}
	}
	return false
}
func qyerPhotoID(page *url.URL, n *html.Node, album string) string {
	if n == nil || n.Data != "a" || !qyerClasses(n, "img-link") {
		return ""
	}
	id := attribute(n, "data-id")
	if !qyerID.MatchString(id) {
		return ""
	}
	raw, err := url.Parse(attribute(n, "href"))
	if err != nil || raw.User != nil || raw.RawQuery != "" || raw.Fragment != "" {
		return ""
	}
	u := page.ResolveReference(raw)
	if u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.EqualFold(u.Hostname(), "photo.qyer.com") || u.Path != "/"+id+"/album/"+album {
		return ""
	}
	return id
}

// Only photos linked inside the current album's visible photo list are used.
// The historical page has no confirmed next-page contract; don't synthesize it.
func qyerAlbum(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	album := qyerAlbumID(page)
	if album == "" || doc == nil {
		return result, errors.New("穷游相册页面无效")
	}
	var lists []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if result.Title == "" && qyerClasses(n, "albumName") {
			result.Title = nodeText(n)
		}
		if n.Data == "div" && attribute(n, "id") == "photoList" {
			lists = append(lists, n)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(lists) != 1 || attribute(lists[0], "data-albumid") != album || !qyerClasses(lists[0], "photo-list") {
		return result, errors.New("穷游相册身份无法确认，可能需要登录或访问授权")
	}
	seen := map[string]bool{}
	invalid := false
	walkHTML(lists[0], func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "img" {
			return
		}
		id := qyerPhotoID(page, n.Parent, album)
		image, ok := qyerImage(attribute(n, "src"))
		if id == "" || !ok {
			invalid = true
			return
		}
		if seen[id] {
			invalid = true
			return
		}
		if len(result.Items) >= 10000 {
			invalid = true
			return
		}
		seen[id] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"qyer_album_id": album, "qyer_photo_id": id}}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("穷游相册未返回可确认的图片，可能需要登录或访问授权")
	}
	if invalid {
		return result, errors.New("穷游相册有无法确认的图片，已保留部分结果；其他分页和原始尺寸尚未确认")
	}
	return result, errors.New("穷游仅采集页面当前加载的图片；其他分页和原始尺寸尚未确认")
}

func discoverQyerAlbum(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isQyerAlbum(root) {
		return result, errors.New("穷游相册地址或下载客户端无效")
	}
	page := *root
	page.Fragment = ""
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return result, fmt.Errorf("穷游相册读取失败，HTTP %d；可能需要登录或访问授权", status.StatusCode)
		}
		return result, errors.New("穷游相册读取失败，请检查网络或授权状态")
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || qyerAlbumID(final) != qyerAlbumID(root) || (page.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("穷游相册发生跳转或需要登录、访问授权")
	}
	return qyerAlbum(ctx, final, doc)
}
