package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	yupooOwner = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)
	yupooID    = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// The reference supports both seller subdomains and the older /photos/<owner> form.
type yupooRoute struct{ owner, kind, id string }

func yupooRouteOf(page *url.URL) yupooRoute {
	if page == nil || page.User != nil || (page.Scheme != "https" && page.Scheme != "http") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) || strings.Contains(page.EscapedPath(), "%") {
		return yupooRoute{}
	}
	host := strings.ToLower(page.Hostname())
	parts := strings.Split(strings.Trim(page.Path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		parts = nil
	}
	var owner string
	if host == "x.yupoo.com" {
		if len(parts) < 2 || parts[0] != "photos" || !yupooOwner.MatchString(parts[1]) {
			return yupooRoute{}
		}
		owner, parts = strings.ToLower(parts[1]), parts[2:]
	} else if strings.HasSuffix(host, ".x.yupoo.com") {
		owner = strings.TrimSuffix(host, ".x.yupoo.com")
		if !yupooOwner.MatchString(owner) || strings.Contains(owner, ".") {
			return yupooRoute{}
		}
	} else {
		return yupooRoute{}
	}
	switch {
	case len(parts) == 0:
		return yupooRoute{owner: owner, kind: "home"}
	case len(parts) == 1 && parts[0] == "albums":
		return yupooRoute{owner: owner, kind: "home"}
	case len(parts) == 2 && parts[0] == "albums" && yupooID.MatchString(parts[1]):
		return yupooRoute{owner: owner, kind: "album", id: parts[1]}
	case len(parts) == 2 && (parts[0] == "categories" || parts[0] == "collections") && yupooID.MatchString(parts[1]):
		return yupooRoute{owner: owner, kind: parts[0], id: parts[1]}
	case len(parts) == 2 && parts[0] == "search" && parts[1] == "album":
		return yupooRoute{owner: owner, kind: "search"}
	}
	return yupooRoute{}
}

func isYupooPage(page *url.URL) bool { return yupooRouteOf(page).owner != "" }

func yupooImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || strings.Contains(parsed.Path, "..") || strings.Contains(parsed.EscapedPath(), "%2f") || strings.Contains(parsed.EscapedPath(), "%2F") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "photo.yupoo.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	// The reference normalizes the page to HTTPS; do not invent image variants.
	u.Scheme, u.Host = "https", "photo.yupoo.com"
	return u.String(), true
}

func yupooHasClass(n *html.Node, name string) bool {
	for _, class := range strings.Fields(attribute(n, "class")) {
		if class == name {
			return true
		}
	}
	return false
}

func yupooAlbumLink(n *html.Node) bool {
	if n.Type != html.ElementNode || n.Data != "a" {
		return false
	}
	for _, class := range strings.Fields(attribute(n, "class")) {
		if strings.HasPrefix(class, "album__main") {
			return true
		}
		for i := 1; i <= 6; i++ {
			if class == fmt.Sprintf("album%d__main", i) {
				return true
			}
		}
	}
	return false
}

func yupooPageNumber(page *url.URL) (int, bool) {
	values, err := url.ParseQuery(page.RawQuery)
	if err != nil || len(values["page"]) > 1 {
		return 0, false
	}
	if len(values["page"]) == 0 || values.Get("page") == "" {
		return 1, true
	}
	n, err := strconv.Atoi(values.Get("page"))
	return n, err == nil && n > 0 && n <= 1000000
}

func yupooEquivalentPageQuery(a, b *url.URL) bool {
	x, errA := url.ParseQuery(a.RawQuery)
	y, errB := url.ParseQuery(b.RawQuery)
	if errA != nil || errB != nil {
		return false
	}
	delete(x, "page")
	delete(y, "page")
	return x.Encode() == y.Encode()
}

func yupooCompatiblePagerQuery(a, b *url.URL) bool {
	x, errA := url.ParseQuery(a.RawQuery)
	y, errB := url.ParseQuery(b.RawQuery)
	if errA != nil || errB != nil {
		return false
	}
	delete(y, "page")
	for key, values := range y {
		if (url.Values{key: x[key]}).Encode() != (url.Values{key: values}).Encode() {
			return false
		}
	}
	return true
}

// A page link keeps a validated same-owner path and uses only its page number
// to change the current query.
func yupooPaginationLink(page *url.URL, raw string) (*url.URL, bool) {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !sameOrigin(page, u) || yupooRouteOf(u) != yupooRouteOf(page) || !yupooCompatiblePagerQuery(page, u) {
		return nil, false
	}
	current, ok := yupooPageNumber(page)
	if !ok {
		return nil, false
	}
	next, ok := yupooPageNumber(u)
	if !ok || next <= current {
		return nil, false
	}
	out := *page
	out.Path, out.RawPath = u.Path, u.RawPath
	values := out.Query()
	values.Set("page", strconv.Itoa(next))
	out.RawQuery, out.ForceQuery = values.Encode(), false
	return &out, true
}

func yupooSameSiteLink(page *url.URL, raw string) (*url.URL, yupooRoute, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return nil, yupooRoute{}, false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !sameOrigin(page, u) {
		return nil, yupooRoute{}, false
	}
	route := yupooRouteOf(u)
	if route.owner == "" || route.owner != yupooRouteOf(page).owner {
		return nil, yupooRoute{}, false
	}
	if _, ok := yupooPageNumber(u); !ok {
		return nil, yupooRoute{}, false
	}
	return u, route, true
}

func discoverYupoo(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isYupooPage(root) {
		return result, errors.New("又拍相册地址无效")
	}
	page := *root
	if page.Scheme == "http" {
		page.Scheme, page.Host = "https", page.Hostname()
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		var status *HTTPStatusError
		if errors.As(err, &status) && status.StatusCode == 567 {
			return result, errors.Join(errors.New("又拍安全服务拦截了网页，请在下载浏览器中完成验证后重试"), status)
		}
		return result, fmt.Errorf("读取又拍网页失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || yupooRouteOf(final) != yupooRouteOf(&page) || !yupooEquivalentPageQuery(&page, final) {
		return result, errors.New("又拍网页已跳转，可能需要登录、密码或安全验证")
	}
	want, validWant := yupooPageNumber(&page)
	got, validGot := yupooPageNumber(final)
	if !validWant || !validGot || want != got {
		return result, errors.New("又拍分页发生变化，可能需要登录或验证")
	}
	return yupooCollect(ctx, getter, final, doc)
}

type yupooQueueEntry struct {
	page *url.URL
	doc  *html.Node
}

// shortcut: bound page traversal; report a partial result when the limit is hit.
const (
	maxYupooPages  = 500
	maxYupooAlbums = 500
	maxYupooImages = 10000
)

// yupooCollect works on a normal fetch or an already unlocked rendered page.
func yupooCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	root := yupooRouteOf(page)
	if root.owner == "" || doc == nil || getter == nil {
		return result, errors.New("又拍网页或下载客户端无效")
	}
	if _, ok := yupooPageNumber(page); !ok {
		return result, errors.New("又拍分页参数无效")
	}
	queue := []yupooQueueEntry{{page: page, doc: doc}}
	seenPages := map[string]bool{page.String(): true}
	seenAlbums, seenImages := map[string]bool{}, map[string]bool{}
	if root.kind == "album" {
		seenAlbums[root.id] = true
	}
	var partial error
	warnings := map[string]bool{}
	warn := func(message string) {
		if !warnings[message] {
			warnings[message] = true
			partial = errors.Join(partial, errors.New(message))
		}
	}
	enqueue := func(next *url.URL) {
		key := next.String()
		if seenPages[key] {
			return
		}
		if len(seenPages) >= maxYupooPages {
			warn("又拍网页达到 500 页上限，采集可能不完整")
			return
		}
		seenPages[key] = true
		queue = append(queue, yupooQueueEntry{page: next})
	}
	for index := 0; index < len(queue); index++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(partial, err)
		}
		entry := queue[index]
		current := entry.page
		if entry.doc == nil {
			loaded, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: current.String(), referer: page.Scheme + "://" + page.Host + "/", depth: 2})
			if response != nil {
				response.Body.Close()
			}
			if ctx.Err() != nil {
				return result, errors.Join(partial, ctx.Err())
			}
			if err != nil || loaded == nil || final == nil || !sameOrigin(current, final) || yupooRouteOf(final) != yupooRouteOf(current) || !yupooEquivalentPageQuery(current, final) {
				var status *HTTPStatusError
				if errors.As(err, &status) && status.StatusCode == 567 {
					warn("部分又拍页面被安全服务拦截，采集可能不完整")
				} else {
					warn("部分又拍页面读取失败或已跳转，采集可能不完整")
				}
				continue
			}
			want, wantOK := yupooPageNumber(current)
			got, gotOK := yupooPageNumber(final)
			if !wantOK || !gotOK || want != got {
				warn("部分又拍分页发生变化，采集可能不完整")
				continue
			}
			entry.doc, current = loaded, final
		}
		route := yupooRouteOf(current)
		var title, htmlTitle string
		var verification, password, videos bool
		walkHTML(entry.doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			if yupooHasClass(n, "showalbumheader__gallerytitle") && title == "" {
				title = strings.TrimSpace(nodeText(n))
			}
			if n.Data == "title" && htmlTitle == "" {
				htmlTitle = strings.TrimSpace(nodeText(n))
				lower := strings.ToLower(htmlTitle)
				verification = strings.Contains(lower, "access restricted") || strings.Contains(htmlTitle, "请求已被拦截")
			}
			if n.Data == "input" && strings.EqualFold(attribute(n, "type"), "password") {
				password = true
			}
		})
		if title == "" {
			title = htmlTitle
		}
		if result.Title == "" {
			result.Title = title
		}
		if verification {
			warn("又拍网页要求安全验证，采集可能不完整")
			continue
		}
		if password {
			warn("又拍网页需要密码，请在下载浏览器中解锁，采集可能不完整")
		}
		photoTitles := map[*html.Node]string{}
		pageImages := 0
		walkHTML(entry.doc, func(n *html.Node) {
			if ctx.Err() != nil || n.Type != html.ElementNode {
				return
			}
			if route.kind == "album" && n.Data == "img" && attribute(n, "data-origin-src") != "" {
				mediaType := strings.ToLower(attribute(n, "data-type"))
				if n.Parent != nil && strings.EqualFold(attribute(n.Parent, "data-type"), "video") || mediaType == "video" {
					videos = true
					return
				}
				if albumID := attribute(n, "data-album-id"); albumID != "" && albumID != route.id {
					warn("部分又拍图片不属于当前相册，采集可能不完整")
					return
				}
				image, ok := yupooImageURL(current, attribute(n, "data-origin-src"))
				if !ok {
					warn("部分又拍图片来源未获确认，采集可能不完整")
					return
				}
				pageImages++
				key := route.id + "\x00" + image
				if !seenImages[key] {
					if len(result.Items) >= maxYupooImages {
						warn("又拍图片达到 10000 张上限，采集可能不完整")
						return
					}
					seenImages[key] = true
					metadata := map[string]string{"album_id": route.id, "album_title": title}
					for p := n.Parent; p != nil; p = p.Parent {
						if yupooHasClass(p, "showalbum__children") {
							photoTitle, exists := photoTitles[p]
							if !exists {
								walkHTML(p, func(child *html.Node) {
									if child.Type == html.ElementNode && child.Data == "h3" && photoTitle == "" {
										photoTitle = strings.TrimSpace(firstNonEmpty(attribute(child, "title"), nodeText(child)))
									}
								})
								photoTitles[p] = photoTitle
							}
							metadata["title"] = photoTitle
							break
						}
					}
					result.Items = append(result.Items, Item{URL: image, RequireImage: true, Headers: map[string]string{"Referer": current.Scheme + "://" + current.Host + "/"}, Metadata: metadata})
				}
				return
			}
			if n.Data != "a" {
				return
			}
			raw := attribute(n, "href")
			if next, ok := yupooPaginationLink(current, raw); ok {
				enqueue(next)
				return
			}
			if next, nextRoute, ok := yupooSameSiteLink(current, raw); ok && nextRoute == route {
				currentPage, currentOK := yupooPageNumber(current)
				nextPage, nextOK := yupooPageNumber(next)
				if currentOK && nextOK && nextPage > currentPage {
					warn("部分又拍分页参数不一致，采集可能不完整")
				}
			}
			if route.kind != "album" && yupooAlbumLink(n) {
				next, nextRoute, ok := yupooSameSiteLink(current, raw)
				if !ok || nextRoute.kind != "album" {
					warn("部分又拍相册链接来源或格式不受支持")
					return
				}
				if !seenAlbums[nextRoute.id] {
					if len(seenAlbums) >= maxYupooAlbums {
						warn("又拍相册达到 500 个上限，采集可能不完整")
						return
					}
					seenAlbums[nextRoute.id] = true
					enqueue(next)
				}
			}
			if route.kind == "home" {
				insideCategory := false
				for p := n.Parent; p != nil; p = p.Parent {
					for _, class := range strings.Fields(attribute(p, "class")) {
						insideCategory = insideCategory || strings.HasPrefix(class, "showheader__category")
					}
				}
				if insideCategory {
					if next, nextRoute, ok := yupooSameSiteLink(current, raw); ok && nextRoute.kind == "categories" {
						enqueue(next)
					}
				}
			}
		})
		if videos {
			warn("又拍相册包含视频，当前只采集图片")
		}
		if route.kind == "album" && pageImages == 0 && !videos && !password {
			warn("部分又拍相册未返回原图，可能需要密码、登录或验证")
		}
	}
	if err := ctx.Err(); err != nil {
		return result, errors.Join(partial, err)
	}
	if len(result.Items) == 0 && partial == nil {
		return result, errors.New("又拍页面未发现原图，可能需要密码、登录或验证")
	}
	return result, partial
}
