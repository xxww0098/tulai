package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	gomeProductPath = regexp.MustCompile(`^/([A-Za-z0-9]{1,32})-([A-Za-z0-9]{1,32})\.html$`)
	gomeCDN         = regexp.MustCompile(`^gfs[0-9]{0,2}\.gomein\.net\.cn$`)
	gomeInfo        = regexp.MustCompile(`(?s)\bvar\s+prdInfo\s*=\s*\{(.*?)\}`)
	gomeInfoProduct = regexp.MustCompile(`\bprdId\s*:\s*["']([^"']+)["']`)
	gomeInfoSKU     = regexp.MustCompile(`\bsku\s*:\s*["']([^"']+)["']`)
	gomeInfoDesc    = regexp.MustCompile(`\bhtmlHref\s*:\s*["']([^"']+)["']`)
)

func gomeProductRef(u *url.URL) (string, string) {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Hostname(), "item.gome.com.cn") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) || u.RawPath != "" {
		return "", ""
	}
	m := gomeProductPath.FindStringSubmatch(u.Path)
	if len(m) != 3 {
		return "", ""
	}
	return m[1], m[2]
}

func isGomeProduct(u *url.URL) bool { id, _ := gomeProductRef(u); return id != "" }

func gomeImageURL(page *url.URL, raw string) string {
	u, err := crawlURL(page, strings.TrimSpace(raw))
	if err != nil || u.User != nil || !gomeCDN.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "80" && u.Port() != "443") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return ""
	}
	// The image service supports HTTPS even for historic protocol-relative URLs.
	u.Scheme, u.Host, u.Fragment = "https", strings.ToLower(u.Hostname()), ""
	return u.String()
}

func gomeWithin(n *html.Node, attr, want string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		value := attribute(p, attr)
		if attr == "id" && value == want {
			return true
		}
		if attr == "class" {
			for _, part := range strings.Fields(value) {
				if part == want {
					return true
				}
			}
		}
	}
	return false
}

func discoverGome(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	product, sku := gomeProductRef(root)
	if getter == nil || product == "" {
		return result, errors.New("国美商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	gotProduct, gotSKU := gomeProductRef(final)
	if doc == nil || gotProduct != product || gotSKU != sku || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("国美商品页面已跳转或内容无法确认")
	}
	return gomeCollect(ctx, getter, final, doc)
}

// shortcut: preserve available product images when the separate description page cannot be read.
func gomeCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	product, sku := gomeProductRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || product == "" || doc == nil {
		return result, errors.New("国美商品页面无效")
	}
	var info, desc string
	var mismatch bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "script" {
			m := gomeInfo.FindStringSubmatch(nodeText(n))
			if len(m) != 2 {
				return
			}
			if info != "" {
				mismatch = true
				return
			}
			info = m[1]
		}
	})
	prod, variant := gomeInfoProduct.FindStringSubmatch(info), gomeInfoSKU.FindStringSubmatch(info)
	if mismatch || len(prod) != 2 || len(variant) != 2 || prod[1] != product || variant[1] != sku {
		return result, errors.New("国美商品编号无法确认")
	}
	if m := gomeInfoDesc.FindStringSubmatch(info); len(m) == 2 {
		desc = m[1]
	}
	seen := map[string]bool{}
	limitReached := false
	add := func(raw, kind, label string) {
		image := gomeImageURL(page, raw)
		if image == "" {
			return
		}
		var alternate string
		if kind == "sku" && strings.HasSuffix(image, "_60.jpg") {
			alternate, image = image, strings.TrimSuffix(image, "_60.jpg")+".jpg"
		}
		if seen[image] {
			return
		}
		// shortcut: cap a product at 10,000 images until a configurable batch limit is available.
		if len(result.Items) >= 10000 {
			limitReached = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{
			"gome_product_id": product, "gome_sku_id": sku, "image_kind": kind, "title": label,
		}}
		if alternate != "" {
			item.AlternateURLs = []string{alternate}
		}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "img" || !gomeWithin(n, "id", "gm-main-info") {
			return
		}
		switch {
		case gomeWithin(n, "class", "pic-small"):
			add(attribute(n, "rpic"), "main", attribute(n, "alt"))
		case gomeWithin(n, "id", "salesProperty") && gomeWithin(n, "class", "prd-img-item"):
			add(attribute(n, "gome-src"), "sku", attribute(n, "alt"))
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("国美页面未提供可识别的商品图片")
	}
	if limitReached {
		return result, errors.New("国美商品图片达到采集上限")
	}
	if desc == "" {
		return result, errors.New("国美详情页面未提供，图片可能不完整")
	}
	link, err := crawlURL(page, desc)
	if err != nil || link.User != nil || !strings.EqualFold(link.Hostname(), "desc.gome.com.cn") ||
		link.Port() != "" || !strings.HasPrefix(link.Path, "/html/prodhtml/goodsDesc/descHtml/") ||
		path.Base(link.Path) != sku+".html" || path.Clean(link.Path) != link.Path {
		return result, errors.New("国美详情页面地址或商品编号无法确认")
	}
	descGetter := getter
	if source, ok := getter.(*HTTPGetter); ok && source != nil && source.client != nil {
		clone, client := *source, *source.client
		client.Jar = nil
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		clone.client = &client
		descGetter = &clone
	}
	resp, err := descGetter.Get(ctx, link.String(), nil)
	if err != nil {
		return result, fmt.Errorf("国美详情图片读取失败，已保留主图及规格图: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return result, errors.New("国美详情页面没有响应，已保留主图及规格图")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || firstNonEmpty(resp.FinalURL, link.String()) != link.String() {
		return result, errors.New("国美详情页面不可用或发生跳转，已保留主图及规格图")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil || len(body) > maxPageBytes {
		return result, errors.New("国美详情页面读取失败或超过大小限制，已保留主图及规格图")
	}
	detail, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return result, errors.New("国美详情页面无效，已保留主图及规格图")
	}
	before := len(result.Items)
	walkHTML(detail, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			add(firstNonEmpty(attribute(n, "gome-src"), attribute(n, "src")), "detail", "")
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if limitReached {
		return result, errors.New("国美商品图片达到采集上限")
	}
	if len(result.Items) == before {
		return result, errors.New("国美详情页面未返回图片，已保留主图及规格图")
	}
	return result, nil
}
