package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	doubanCommunityPhotoPath = regexp.MustCompile(`^/photos/(album|photo)/([0-9]{1,20})/?$`)
	doubanMoviePhotoPath     = regexp.MustCompile(`^/photos/photo/([0-9]{1,20})/?$`)
	doubanMovieAlbumPath     = regexp.MustCompile(`^/(subject|celebrity)/([0-9]{1,20})/photos/?$`)
	doubanPersonagePath      = regexp.MustCompile(`^/personage/([0-9]{1,20})/(photos|photo/([0-9]{1,20}))/?$`)
	doubanImageHost          = regexp.MustCompile(`^img[0-9]*\.(?:doubanio|douban)\.com$`)
	doubanImagePath          = regexp.MustCompile(`^/view/photo/(thumb|lthumb|m|s|l|r|xl|large|photo|raw)/public/([A-Za-z0-9_.-]+)$`)
	doubanPhotoFilename      = regexp.MustCompile(`^p([0-9]{1,20})\.[A-Za-z0-9]+$`)
)

// shortcut: bound large albums while reporting incomplete results.
const (
	maxDoubanPages  = 500
	maxDoubanImages = 10000
)

type doubanRoute struct {
	kind, id, detail string
}

func doubanRouteOf(u *url.URL) doubanRoute {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return doubanRoute{}
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "douban.com", "www.douban.com":
		if m := doubanCommunityPhotoPath.FindStringSubmatch(u.Path); len(m) == 3 && strings.Trim(m[2], "0") != "" {
			return doubanRoute{kind: m[1], id: m[2]}
		}
		if m := doubanPersonagePath.FindStringSubmatch(u.Path); len(m) == 4 && strings.Trim(m[1], "0") != "" && (m[3] == "" || strings.Trim(m[3], "0") != "") {
			if m[2] == "photos" {
				return doubanRoute{kind: "personage-album", id: m[1]}
			}
			return doubanRoute{kind: "personage-photo", id: m[1], detail: m[3]}
		}
	case "movie.douban.com":
		if m := doubanMoviePhotoPath.FindStringSubmatch(u.Path); len(m) == 2 && strings.Trim(m[1], "0") != "" {
			return doubanRoute{kind: "movie-photo", id: m[1]}
		}
		if m := doubanMovieAlbumPath.FindStringSubmatch(u.Path); len(m) == 3 && strings.Trim(m[2], "0") != "" {
			return doubanRoute{kind: "movie-" + m[1], id: m[2]}
		}
	}
	return doubanRoute{}
}

func isDoubanPage(u *url.URL) bool { return doubanRouteOf(u).kind != "" }

func (route doubanRoute) singlePhoto() bool {
	return route.kind == "photo" || route.kind == "movie-photo" || route.kind == "personage-photo"
}

// The reference uses /photo/raw/ for photo pages. The CDN may redirect it to
// a smaller /photo/l/ image, especially when an original is not accessible.
func doubanOriginal(base *url.URL, raw string) (string, string, bool) {
	u, err := crawlURL(base, raw)
	if err != nil || u.User != nil || !doubanImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return "", "", false
	}
	m := doubanImagePath.FindStringSubmatch(u.Path)
	if len(m) != 3 || strings.Contains(m[2], "..") {
		return "", "", false
	}
	u.Scheme = "https"
	u.Host = strings.ToLower(u.Hostname())
	u.Path = "/view/photo/raw/public/" + m[2]
	u.RawPath = ""
	photoID := ""
	if id := doubanPhotoFilename.FindStringSubmatch(m[2]); len(id) == 2 {
		photoID = id[1]
	}
	return u.String(), photoID, true
}

func doubanClass(node *html.Node, want string) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	for _, name := range strings.Fields(attribute(node, "class")) {
		if name == want {
			return true
		}
	}
	return false
}

func doubanNodeText(node *html.Node) string {
	var text strings.Builder
	walkHTML(node, func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
	})
	return strings.TrimSpace(text.String())
}

// discoverDouban accepts both the ordinary Getter and a captured browser's
// same-origin Getter; it never reads or attaches browser credentials itself.
func discoverDouban(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	route := doubanRouteOf(root)
	if route.kind == "" || getter == nil {
		return result, errors.New("豆瓣相册或照片地址无效")
	}
	page := *root
	// Browsers supply their captured final origin. The plain HTTP client has no
	// browser login state and can safely avoid the bare-domain/HTTP redirect.
	if _, ok := getter.(*HTTPGetter); ok {
		page.Scheme = "https"
		if strings.EqualFold(page.Hostname(), "douban.com") {
			page.Host = "www.douban.com"
		} else {
			page.Host = strings.ToLower(page.Hostname())
		}
	}
	page.User, page.Fragment = nil, ""
	initialPage := page
	origin := &initialPage
	visited := make(map[string]bool)
	seen := make(map[string]bool)
	var problems []error
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(problems, err)...)
		}
		if count >= maxDoubanPages {
			return result, errors.Join(append(problems, fmt.Errorf("豆瓣相册超过 %d 页，结果不完整", maxDoubanPages))...)
		}
		if visited[page.String()] {
			return result, errors.Join(append(problems, errors.New("豆瓣相册分页重复，结果可能不完整"))...)
		}
		visited[page.String()] = true
		ref := page.Scheme + "://" + page.Host + "/"
		doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), referer: ref, depth: 1})
		if response != nil {
			response.Body.Close()
		}
		if ctx.Err() != nil {
			return result, errors.Join(append(problems, ctx.Err())...)
		}
		if err != nil {
			return result, errors.Join(append(problems, fmt.Errorf("读取豆瓣页面失败，结果可能不完整: %w", err))...)
		}
		if doc == nil || final == nil || final.User != nil || !sameOrigin(origin, final) || doubanRouteOf(final) != route {
			return result, errors.Join(append(problems, errors.New("豆瓣页面跳转到其他地址，可能需要登录或安全验证"))...)
		}
		found, next, invalid := doubanCollectPage(ctx, doc, final, route)
		if result.Title == "" {
			result.Title = found.Title
		}
		if invalid > 0 {
			problems = append(problems, fmt.Errorf("%d 张豆瓣图片地址无效，结果可能不完整", invalid))
		}
		for _, item := range found.Items {
			if seen[item.URL] {
				continue
			}
			if len(result.Items) >= maxDoubanImages {
				return result, errors.Join(append(problems, fmt.Errorf("豆瓣相册超过 %d 张图片，结果不完整", maxDoubanImages))...)
			}
			seen[item.URL] = true
			item.Metadata["album_id"] = route.id
			item.Metadata["album_title"] = result.Title
			result.Items = append(result.Items, item)
		}
		if ctx.Err() != nil {
			return result, errors.Join(append(problems, ctx.Err())...)
		}
		if len(found.Items) == 0 {
			return result, errors.Join(append(problems, errors.New("豆瓣页面未返回可识别的图片，可能为空或需要登录、安全验证"))...)
		}
		if route.singlePhoto() || next == "" {
			return result, errors.Join(problems...)
		}
		nextPage, err := crawlURL(final, next)
		if err != nil || nextPage.User != nil || !sameOrigin(origin, nextPage) || doubanRouteOf(nextPage) != route {
			return result, errors.Join(append(problems, errors.New("豆瓣相册下一页地址无效，结果不完整"))...)
		}
		page = *nextPage
	}
}

func doubanCollectPage(ctx context.Context, doc *html.Node, page *url.URL, route doubanRoute) (Discovery, string, int) {
	result := Discovery{IsPage: true}
	var next string
	var titles []string
	var candidates []string
	var containers []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "h1" || n.Data == "title" {
			if title := doubanNodeText(n); title != "" {
				titles = append(titles, title)
			}
		}
		if !route.singlePhoto() && n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "next") {
			next = attribute(n, "href")
		}
		if route.singlePhoto() {
			if n.Data == "a" && doubanClass(n, "photo-zoom") {
				candidates = append(candidates, attribute(n, "href"))
			}
			if doubanClass(n, "mainphoto") || doubanClass(n, "pic-viewer") {
				containers = append(containers, n)
			}
			if n.Data == "img" && doubanClass(n, "media") {
				candidates = append(candidates, attribute(n, "src"))
			}
		} else if doubanClass(n, "photolst") || doubanClass(n, "poster-col4") || doubanClass(n, "pics") {
			containers = append(containers, n)
		}
	})
	if len(titles) > 0 {
		result.Title = titles[0]
	}
	for _, container := range containers {
		walkHTML(container, func(n *html.Node) {
			if ctx.Err() == nil && n.Type == html.ElementNode && n.Data == "img" {
				raw := attribute(n, "src")
				if data := attribute(n, "data-src"); data != "" {
					raw = data
				}
				candidates = append(candidates, raw)
			}
		})
	}
	seen := make(map[string]bool)
	invalid := 0
	for _, raw := range candidates {
		if ctx.Err() != nil {
			break
		}
		original, id, ok := doubanOriginal(page, raw)
		if !ok {
			invalid++
			continue
		}
		if route.singlePhoto() && id != "" {
			want := route.id
			if route.detail != "" {
				want = route.detail
			}
			if id != want {
				invalid++
				continue
			}
		}
		if seen[original] {
			continue
		}
		seen[original] = true
		item := Item{URL: original, RequireImage: true,
			Headers:  map[string]string{"Referer": page.Scheme + "://" + page.Host + "/"},
			Metadata: map[string]string{"douban_quality": "raw_candidate"}}
		if id != "" {
			item.Metadata["photo_id"] = id
		}
		result.Items = append(result.Items, item)
		if route.singlePhoto() {
			break
		}
	}
	return result, next, invalid
}
