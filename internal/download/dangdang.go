package download

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
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
	dangdangProductPath = regexp.MustCompile(`^/([1-9][0-9]{4,19})\.html$`)
	dangdangState       = regexp.MustCompile(`\b(?:var|let|const)\s+prodSpuInfo\s*=\s*`)
	dangdangImageHost   = regexp.MustCompile(`^img(?:3m[0-9]|[0-9]{1,2})\.ddimg\.cn$`)
	dangdangDigits      = regexp.MustCompile(`^[0-9]{1,20}$`)
	dangdangDescribe    = regexp.MustCompile(`^[0-9:,]{1,256}$`)
	dangdangCategory    = regexp.MustCompile(`^[0-9.]{1,100}$`)
)

type dangdangProduct struct {
	ProductID    string `json:"productId"`
	ProductName  string `json:"productName"`
	Template     string `json:"template"`
	DescribeMap  string `json:"describeMap"`
	ShopID       string `json:"shopId"`
	CategoryPath string `json:"categoryPath"`
}

func dangdangProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "product.dangdang.com") {
		return ""
	}
	if port := u.Port(); port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return ""
	}
	m := dangdangProductPath.FindStringSubmatch(u.Path)
	if len(m) != 2 {
		return ""
	}
	return m[1]
}

func isDangdangProduct(u *url.URL) bool { return dangdangProductID(u) != "" }

func discoverDangdangProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := dangdangProductID(root)
	if getter == nil || id == "" {
		return result, errors.New("当当商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || dangdangProductID(final) != id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("当当商品页面不可用或跳转到其他商品")
	}
	return dangdangCollect(ctx, getter, final, doc)
}

func dangdangImageURL(raw string, sku bool) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !dangdangImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	u.Scheme = "https"
	if u.Port() == "80" {
		u.Host = u.Hostname()
	}
	if sku {
		u.Path = strings.Replace(u.Path, "_b_", "_u_", 1)
	}
	if strings.HasSuffix(strings.ToLower(u.Path), ".jpg_.webp") {
		u.Path = u.Path[:len(u.Path)-len("_.webp")]
	}
	u.RawPath = ""
	u.Fragment = ""
	return u.String(), true
}

func dangdangAncestor(n *html.Node, key, value string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type != html.ElementNode {
			continue
		}
		if key == "tag" && p.Data == value {
			return true
		}
		if key == "id" && attribute(p, "id") == value {
			return true
		}
		if key == "class" {
			for _, class := range strings.Fields(attribute(p, "class")) {
				if class == value {
					return true
				}
			}
		}
	}
	return false
}

func dangdangDetailEndpoint(page *url.URL, product dangdangProduct) (*url.URL, error) {
	if page == nil || product.ProductID != dangdangProductID(page) || product.Template != "publish" ||
		!dangdangDigits.MatchString(product.ShopID) || !dangdangDescribe.MatchString(product.DescribeMap) || !dangdangCategory.MatchString(product.CategoryPath) {
		return nil, errors.New("当当商品详情参数缺失或无法校验")
	}
	endpoint := *page
	endpoint.Path = "/index.php"
	endpoint.RawPath, endpoint.Fragment = "", ""
	query := url.Values{"r": {"callback/detail"}, "productId": {product.ProductID}, "templateType": {product.Template},
		"describeMap": {product.DescribeMap}, "shopId": {product.ShopID}, "categoryPath": {product.CategoryPath}}
	endpoint.RawQuery = query.Encode()
	return &endpoint, nil
}

func dangdangDetail(ctx context.Context, getter Getter, page *url.URL, product dangdangProduct) (string, error) {
	endpoint, err := dangdangDetailEndpoint(page, product)
	if err != nil {
		return "", err
	}
	resp, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", page.Scheme+"://"+page.Host+page.Path)
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("当当详情请求失败：HTTP %d", resp.StatusCode)
	}
	final, err := url.Parse(firstNonEmpty(resp.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path || final.RawQuery != endpoint.RawQuery {
		return "", errors.New("当当详情接口发生了不安全的跳转")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxPageBytes {
		return "", errors.New("当当详情响应超过大小上限")
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		decoded, err := gzip.NewReader(reader)
		if err != nil {
			return "", err
		}
		defer decoded.Close()
		reader = decoded
	case "deflate":
		decoded, err := zlib.NewReader(reader)
		if err != nil {
			return "", err
		}
		defer decoded.Close()
		reader = decoded
	default:
		return "", errors.New("当当详情响应使用了不支持的压缩格式")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if err != nil || len(body) > maxPageBytes {
		return "", errors.New("当当详情响应无法读取或超过大小上限")
	}
	var result struct {
		ErrMsg string `json:"errMsg"`
		Data   struct {
			HTML string `json:"html"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil || result.ErrMsg != "" {
		return "", errors.New("当当详情响应无效")
	}
	return result.Data.HTML, nil
}

// dangdangCollect shares the same product and description extraction with
// direct HTTP and the isolated browser session's Getter.
func dangdangCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := dangdangProductID(page)
	if doc == nil || getter == nil || id == "" {
		return result, errors.New("当当商品页面无效")
	}
	var product *dangdangProduct
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" || product != nil {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode {
				continue
			}
			for _, loc := range offer1688CodeMatches(c.Data, dangdangState) {
				var candidate dangdangProduct
				if json.NewDecoder(strings.NewReader(c.Data[loc[1]:])).Decode(&candidate) == nil && candidate.ProductID == id {
					product = &candidate
					break
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if product == nil {
		return result, errors.New("当当未返回可确认属于该商品的数据")
	}
	result.Title = firstNonEmpty(product.ProductName, result.Title)
	seen := map[string]bool{}
	incomplete := false
	add := func(raw, kind, variant string) {
		if strings.TrimSpace(raw) == "" {
			return
		}
		image, ok := dangdangImageURL(raw, kind == "sku")
		if !ok {
			incomplete = true
			return
		}
		if seen[image] {
			return
		}
		seen[image] = true
		metadata := map[string]string{"dangdang_product_id": id, "image_kind": kind}
		if variant != "" {
			metadata["variant"] = variant
		}
		item := Item{URL: image, RequireImage: true, Metadata: metadata}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "a" && dangdangAncestor(n, "id", "main-img-slider") {
			add(attribute(n, "data-imghref"), "main", "")
		}
		if n.Data == "img" && dangdangAncestor(n, "class", "list_c") {
			variant := ""
			if n.Parent != nil {
				variant = attribute(n.Parent, "title")
			}
			add(attribute(n, "src"), "sku", variant)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	description, err := dangdangDetail(ctx, getter, page, *product)
	if err != nil {
		return result, errors.Join(errors.New("当当详情加载失败，已保留部分图片"), err)
	}
	if strings.TrimSpace(description) != "" {
		fragment, parseErr := html.Parse(strings.NewReader(description))
		if parseErr != nil {
			return result, errors.Join(errors.New("当当详情解析失败，已保留部分图片"), parseErr)
		}
		walkHTML(fragment, func(n *html.Node) {
			if ctx.Err() == nil && n.Type == html.ElementNode && n.Data == "img" && !dangdangAncestor(n, "tag", "a") {
				add(firstNonEmpty(attribute(n, "data-original"), attribute(n, "src")), "description", "")
			}
		})
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("当当未返回可信商品图片")
	}
	if incomplete {
		return result, errors.New("当当部分图片地址无效，结果可能不完整")
	}
	return result, nil
}
