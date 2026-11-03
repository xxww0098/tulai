package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/html"
)

var (
	wellcoPath  = regexp.MustCompile(`^/products/[A-Za-z0-9][A-Za-z0-9_-]{0,199}/?$`)
	wellcoImage = regexp.MustCompile(`^/images/105295557/products/([1-9][0-9]{0,19})/[A-Za-z0-9_-]+\.(?:jpg|jpeg|png|webp)$`)
	wellcoID    = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
)

func isWellcoPage(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!wellcoPath.MatchString(u.Path) || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") ||
		path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "wellco.ca" && host != "www.wellco.ca" {
		return false
	}
	switch strings.ToLower(strings.Trim(strings.TrimPrefix(u.Path, "/products/"), "/")) {
	case "account", "cart", "search":
		return false
	}
	return true
}

func wellcoSamePage(a, b *url.URL) bool {
	return isWellcoPage(a) && isWellcoPage(b) && strings.TrimSuffix(a.Path, "/") == strings.TrimSuffix(b.Path, "/") &&
		!(a.Scheme == "https" && b.Scheme != "https")
}

func wellcoOffset(u *url.URL) (int, bool) {
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["offset"]) > 1 {
		return 0, false
	}
	if len(q["offset"]) == 0 {
		return 0, true
	}
	raw := q.Get("offset")
	offset, err := strconv.Atoi(raw)
	return offset, err == nil && offset >= 0 && offset <= 6000000 && offset%60 == 0 && strconv.Itoa(offset) == raw
}

func wellcoSameFetchPage(a, b *url.URL) bool {
	if !wellcoSamePage(a, b) || a.Fragment != "" || b.Fragment != "" {
		return false
	}
	x, okX := wellcoOffset(a)
	y, okY := wellcoOffset(b)
	if !okX || !okY || x != y {
		return false
	}
	qa, qb := a.Query(), b.Query()
	qa.Del("offset")
	qb.Del("offset")
	return qa.Encode() == qb.Encode()
}

func wellcoGetter(getter Getter, expected *url.URL) Getter {
	g, ok := getter.(*HTTPGetter)
	if !ok || g == nil || g.client == nil {
		return getter
	}
	copyGetter, client := *g, *g.client
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || !wellcoSameFetchPage(expected, req.URL) {
			return errors.New("Wellco 页面跳转到其他地址")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return errors.New("Wellco 页面重定向次数过多")
		}
		return nil
	}
	copyGetter.client = &client
	return &copyGetter
}

func wellcoFetch(ctx context.Context, getter Getter, page *url.URL) (*html.Node, *url.URL, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, wellcoGetter(getter, page), crawlPage{url: page.String(), depth: 1})
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, nil, err
	}
	if doc == nil || final == nil || !wellcoSameFetchPage(page, final) {
		return nil, nil, errors.New("Wellco 页面已跳转")
	}
	return doc, final, nil
}

func discoverWellco(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isWellcoPage(page) {
		return result, errors.New("Wellco 页面地址无效")
	}
	if page.Fragment != "" {
		return result, errors.New("Wellco 暂不支持带片段的商品或分类地址")
	}
	doc, final, err := wellcoFetch(ctx, getter, page)
	if err != nil {
		return result, fmt.Errorf("Wellco 页面读取失败: %w", err)
	}
	return wellcoCollect(ctx, getter, final, doc)
}

type wellcoLDProduct struct {
	Type  string `json:"@type"`
	Name  string `json:"name"`
	Image []struct {
		Page string `json:"url"`
		URL  string `json:"contentUrl"`
	} `json:"image"`
}

func wellcoImageURL(raw string) (string, string) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "d2j6dbq0eux0bg.cloudfront.net") ||
		(u.Port() != "" && u.Port() != "443") || u.RawPath != "" || u.Fragment != "" {
		return "", ""
	}
	m := wellcoImage.FindStringSubmatch(u.Path)
	if len(m) != 2 {
		return "", ""
	}
	return u.String(), m[1]
}

func wellcoProduct(doc *html.Node, page *url.URL, expectedID string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	var product wellcoLDProduct
	count := 0
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "script" || !strings.EqualFold(attribute(n, "type"), "application/ld+json") {
			return
		}
		var candidate wellcoLDProduct
		if json.Unmarshal([]byte(nodeText(n)), &candidate) == nil && candidate.Type == "Product" {
			product = candidate
			count++
		}
	})
	if count == 0 {
		return result, false, nil
	}
	if count != 1 || len(product.Image) == 0 {
		return result, true, errors.New("Wellco 商品图片数据不完整")
	}
	result.Title = strings.TrimSpace(product.Name)
	seen := map[string]bool{}
	incomplete := false
	productID := expectedID
	for _, image := range product.Image {
		owner, err := url.Parse(image.Page)
		img, id := wellcoImageURL(image.URL)
		if err != nil || !wellcoSamePage(page, owner) || img == "" || (productID != "" && productID != id) {
			incomplete = true
			continue
		}
		productID = id
		if seen[img] {
			continue
		}
		seen[img] = true
		item := Item{URL: img, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"wellco_product_id": id}}
		if ref := imageReferer(page.String(), img); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, true, errors.New("Wellco 商品没有可确认的图片")
	}
	if incomplete {
		return result, true, errors.New("Wellco 部分商品图片无法确认身份")
	}
	return result, true, nil
}

type wellcoProductLink struct {
	page *url.URL
	id   string
}

type wellcoListing struct {
	title   string
	links   []wellcoProductLink
	current int
	last    int
}

type wellcoOutcome struct {
	product Discovery
	err     error
}

func wellcoProducts(ctx context.Context, getter Getter, links []wellcoProductLink) ([]wellcoOutcome, int) {
	results := make([]wellcoOutcome, len(links))
	jobs := make(chan int, len(links))
	for i := range links {
		jobs <- i
	}
	close(jobs)
	var limited atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil || limited.Load() != 0 {
					return
				}
				link := links[i]
				doc, final, err := wellcoFetch(ctx, getter, link.page)
				var status *HTTPStatusError
				if errors.As(err, &status) && (status.StatusCode == 403 || status.StatusCode == 429) {
					limited.CompareAndSwap(0, int32(status.StatusCode))
				}
				if err != nil {
					results[i].err = err
					continue
				}
				product, ok, err := wellcoProduct(doc, final, link.id)
				if !ok {
					err = errors.New("Wellco 商品页面缺少数据")
				}
				results[i] = wellcoOutcome{product, err}
			}
		}()
	}
	wg.Wait()
	return results, int(limited.Load())
}

func wellcoListingPage(page *url.URL, doc *html.Node) (wellcoListing, error) {
	listing := wellcoListing{current: 1, last: 1}
	var canonical, title string
	var badCanonical bool
	seen := map[string]bool{}
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			value := attribute(n, "content")
			if canonical != "" && canonical != value {
				badCanonical = true
			}
			canonical = value
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:title") {
			title = attribute(n, "content")
		}
		if n.Data != "a" {
			return
		}
		classes := strings.Fields(attribute(n, "class"))
		if number, err := strconv.Atoi(attribute(n, "data-page-number")); err == nil && number > 0 && slices.Contains(classes, "pager__number") {
			if slices.Contains(classes, "pager__number--current") {
				listing.current = number
			}
			if number > listing.last {
				listing.last = number
			}
		}
		if !slices.Contains(classes, "grid-product__image") {
			return
		}
		id := attribute(n, "data-product-id")
		if !wellcoID.MatchString(id) || seen[id] {
			return
		}
		u, err := url.Parse(attribute(n, "href"))
		if err != nil || u.User != nil {
			return
		}
		u = page.ResolveReference(u)
		if u.Scheme != "https" || !isWellcoPage(u) || u.RawQuery != "" || u.Fragment != "" || wellcoSamePage(page, u) {
			return
		}
		seen[id] = true
		listing.links = append(listing.links, wellcoProductLink{page: u, id: id})
	})
	canonicalURL, err := url.Parse(canonical)
	if err != nil || badCanonical || canonical == "" || !wellcoSamePage(page, canonicalURL) {
		return listing, errors.New("Wellco 页面身份无法确认")
	}
	listing.title = title
	return listing, nil
}

// shortcut: stop at 100 listing pages or 1000 products; enlarge only after validating larger categories.
func wellcoCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isWellcoPage(page) || doc == nil {
		return result, errors.New("Wellco 页面无效")
	}
	listing, err := wellcoListingPage(page, doc)
	if err != nil {
		return result, err
	}
	if product, ok, err := wellcoProduct(doc, page, ""); ok {
		return product, err
	}
	if len(listing.links) == 0 {
		return result, errors.New("Wellco 分类页没有可确认的商品")
	}
	result.Title = listing.title
	const maxProducts = 1000
	seen := map[string]bool{}
	incomplete := false
	pages, missing, accessLimit := 0, 0, 0
	offset, offsetOK := wellcoOffset(page)
	for {
		pages++
		if !offsetOK || listing.current != offset/60+1 {
			incomplete = true
			break
		}
		var links []wellcoProductLink
		for _, link := range listing.links {
			if seen[link.id] {
				continue
			}
			if len(seen) >= maxProducts {
				incomplete = true
				break
			}
			seen[link.id] = true
			links = append(links, link)
		}
		results, limited := wellcoProducts(ctx, getter, links)
		if err := ctx.Err(); err != nil {
			return result, err
		}
		for i, entry := range results {
			if entry.err != nil || len(entry.product.Items) == 0 {
				missing++
			}
			for _, item := range entry.product.Items {
				item.Metadata["album_id"] = links[i].id
				item.Metadata["album_title"] = entry.product.Title
				result.Items = append(result.Items, item)
			}
		}
		if limited != 0 {
			accessLimit = limited
			incomplete = true
			break
		}
		if listing.current >= listing.last {
			break
		}
		if incomplete || pages >= maxCrawlPages || len(listing.links) != 60 {
			incomplete = true
			break
		}
		next := *page
		query := next.Query()
		offset += 60
		query.Set("offset", strconv.Itoa(offset))
		next.RawQuery, next.Fragment = query.Encode(), ""
		nextDoc, final, err := wellcoFetch(ctx, getter, &next)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			var status *HTTPStatusError
			if errors.As(err, &status) && (status.StatusCode == 403 || status.StatusCode == 429) {
				accessLimit = status.StatusCode
			}
			incomplete = true
			break
		}
		listing, err = wellcoListingPage(final, nextDoc)
		if err != nil || len(listing.links) == 0 {
			incomplete = true
			break
		}
	}
	if accessLimit != 0 {
		return result, fmt.Errorf("Wellco 遇到 HTTP %d，已保留 %d 张图片，%d 个商品不完整", accessLimit, len(result.Items), missing)
	}
	if missing > 0 || incomplete {
		return result, fmt.Errorf("Wellco 已读取 %d 个分类页，%d 个商品不完整；部分结果尚未确认", pages, missing)
	}
	return result, nil
}
