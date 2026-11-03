package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var muyingID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type muyingRef struct {
	kind, id string
	page     int
}

// Only the two URL shapes found in the reference site's script are handled.
func muyingPageRef(u *url.URL) muyingRef {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.EqualFold(u.Hostname(), "wybbej.mytx.cn") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return muyingRef{}
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return muyingRef{}
	}
	var kind, key string
	switch u.Path {
	case "/ProductDetails.aspx":
		kind, key = "product", "productId"
	case "/SubCategory.aspx":
		kind, key = "category", "categoryId"
	default:
		return muyingRef{}
	}
	if len(q[key]) != 1 || !muyingID.MatchString(q.Get(key)) {
		return muyingRef{}
	}
	page := 1
	if v, exists := q["pageindex"]; exists {
		if kind != "category" || len(v) != 1 {
			return muyingRef{}
		}
		page, err = strconv.Atoi(v[0])
		if err != nil || page < 1 || page > 100000 || strconv.Itoa(page) != v[0] {
			return muyingRef{}
		}
	}
	return muyingRef{kind: kind, id: q.Get(key), page: page}
}
func isMuyingPage(u *url.URL) bool { return muyingPageRef(u).kind != "" }
func muyingHasClass(n *html.Node, c string) bool {
	if n == nil || n.Type != html.ElementNode {
		return false
	}
	for _, s := range strings.Fields(attribute(n, "class")) {
		if s == c {
			return true
		}
	}
	return false
}
func muyingAncestor(n *html.Node, c string) bool {
	for p := n; p != nil; p = p.Parent {
		if muyingHasClass(p, c) {
			return true
		}
	}
	return false
}

// No historic product page is available to establish external CDN hosts. Accept
// only the original, same-origin image URLs that the page itself supplies.
func muyingImage(page *url.URL, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" ||
		(u.Path != "" && path.Clean(u.Path) != u.Path) || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	u = page.ResolveReference(u)
	if u.User != nil || !sameOrigin(page, u) || path.Clean(u.Path) != u.Path || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".avif":
	default:
		return "", false
	}
	return u.String(), true
}

func muyingProduct(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := muyingPageRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ref.kind != "product" || doc == nil {
		return result, errors.New("母婴天下商品页无效")
	}
	seen := map[string]bool{}
	bad := false
	detailStarted, detailEnded := false, false
	add := func(raw, kind string) {
		if raw == "" {
			return
		}
		image, ok := muyingImage(page, raw)
		if !ok {
			bad = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"muying_product_id": ref.id, "muying_image_kind": kind}}
		if referer := imageReferer(page.Scheme+"://"+page.Host+"/", image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "name"), "description") && result.Title == "" {
			result.Title = attribute(n, "content")
		}
		if muyingHasClass(n, "product_detail") {
			detailStarted = true
		}
		if detailStarted && muyingHasClass(n, "quanbupji") {
			detailEnded = true
		}
		if n.Data == "a" && muyingHasClass(n, "cloud-zoom-gallery") {
			add(attribute(n, "href"), "main")
		}
		if n.Data == "img" && !detailEnded && muyingAncestor(n, "product_detail") {
			add(attribute(n, "src"), "detail")
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("母婴天下商品未返回可确认的同源图片，可能需要登录或访问授权")
	}
	if bad {
		return result, errors.New("母婴天下有未确认图床或图片格式，已保留同源图片")
	}
	return result, errors.New("母婴天下可能还有其他图片类型或图床，已保留当前可确认的图片")
}

// An HTTPGetter can be copied with a per-request same-origin redirect guard.
func muyingGuard(getter Getter, origin *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || !sameOrigin(origin, req.URL) {
			return errors.New("母婴天下已阻止跨站跳转")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("母婴天下重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}
func muyingFetch(ctx context.Context, getter Getter, page *url.URL, depth int) (*html.Node, *url.URL, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, muyingGuard(getter, page), crawlPage{url: page.String(), depth: depth})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		var status *HTTPStatusError
		if errors.As(err, &status) {
			return nil, nil, fmt.Errorf("母婴天下页面读取失败，HTTP %d；可能需要登录或访问授权", status.StatusCode)
		}
		return nil, nil, errors.New("母婴天下页面读取失败，请检查网络或授权状态")
	}
	if doc == nil || final == nil || !sameOrigin(page, final) || muyingPageRef(page) != muyingPageRef(final) {
		return nil, nil, errors.New("母婴天下页面发生跳转或需要登录、访问授权")
	}
	return doc, final, nil
}
func discoverMuying(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isMuyingPage(root) {
		return result, errors.New("母婴天下地址或下载客户端无效")
	}
	page := *root
	page.Fragment = ""
	doc, final, err := muyingFetch(ctx, getter, &page, 1)
	if err != nil {
		return result, err
	}
	return muyingCollect(ctx, getter, final, doc)
}

// Follow only the current category's actual product links, using the caller's
// authorized same-origin Getter; no page numbers or product IDs are invented.
func muyingCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := muyingPageRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || doc == nil || ref.kind == "" {
		return result, errors.New("母婴天下页面或下载客户端无效")
	}
	if ref.kind == "product" {
		return muyingProduct(ctx, page, doc)
	}
	var links []*url.URL
	seen := map[string]bool{}
	foundList, bad := false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if result.Title == "" && n.Data == "title" {
			result.Title = nodeText(n)
		}
		if muyingHasClass(n, "category_pro_list") {
			foundList = true
		}
		if n.Data != "a" || !muyingAncestor(n, "category_pro_list") {
			return
		}
		raw := attribute(n, "href")
		if raw == "" {
			return
		}
		u, err := url.Parse(raw)
		if err != nil || u.User != nil {
			bad = true
			return
		}
		u = page.ResolveReference(u)
		u.Fragment = ""
		if !sameOrigin(page, u) || muyingPageRef(u).kind != "product" {
			return
		}
		key := muyingPageRef(u).id
		if !seen[key] {
			seen[key] = true
			links = append(links, u)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !foundList || len(links) == 0 {
		return result, errors.New("母婴天下分类未返回可确认的商品，可能需要登录或访问授权")
	}
	seenImages := map[string]bool{}
	for i, u := range links {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if i >= maxCrawlPages-1 {
			bad = true
			break
		}
		product, final, err := muyingFetch(ctx, getter, u, 2)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			bad = true
			continue
		}
		got, err := muyingProduct(ctx, final, product)
		if err != nil && len(got.Items) == 0 {
			bad = true
			continue
		}
		for _, item := range got.Items {
			if !seenImages[item.URL] {
				seenImages[item.URL] = true
				result.Items = append(result.Items, item)
			}
		}
	}
	if len(result.Items) == 0 {
		return result, errors.New("母婴天下当前分类没有可确认的同源商品图片")
	}
	if bad {
		return result, errors.New("母婴天下部分商品未能读取；已保留当前分类可确认的图片，其他分页尚未确认")
	}
	return result, errors.New("母婴天下仅采集当前分类页的商品；其他分页和图床尚未确认")
}
