package download

import (
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
	suningProductPath = regexp.MustCompile(`^/([0-9]{10})/([0-9]{1,18})\.html$`)
	suningSKUCode     = regexp.MustCompile(`^[0-9]{18}$`)
	suningImageHost   = regexp.MustCompile(`^imgservice[0-9]{0,2}\.suning\.cn$`)
	suningImageResize = regexp.MustCompile(`(?i)(\.(?:jpe?g|png|webp))_[0-9]+w_[0-9]+h_4e$`)
)

const suningSKULimit = 200

type suningRef struct{ vendor, item string }

func suningNumber(s string) string {
	if s == "" {
		return ""
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return strings.TrimLeft(s, "0")
}

func suningProductRef(u *url.URL) suningRef {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "product.suning.com") {
		return suningRef{}
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return suningRef{}
	}
	match := suningProductPath.FindStringSubmatch(u.Path)
	if len(match) != 3 || suningNumber(match[2]) == "" {
		return suningRef{}
	}
	return suningRef{vendor: match[1], item: suningNumber(match[2])}
}

func isSuningProduct(u *url.URL) bool { return suningProductRef(u).item != "" }

func discoverSuningProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := suningProductRef(root)
	if getter == nil || want.item == "" {
		return result, errors.New("苏宁商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || suningProductRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("苏宁商品页面不可用或跳转到其他商品")
	}
	return suningCollect(ctx, getter, final, doc)
}

func suningImageURL(raw string, removeResize bool) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case suningImageHost.MatchString(host) && strings.HasPrefix(u.Path, "/uimg1/b2c/image/"):
	case host == "uimgproxy.suning.cn" && strings.HasPrefix(u.Path, "/uimg1/sop/commodity/"):
	default:
		return "", false
	}
	if removeResize && suningImageHost.MatchString(host) {
		u.Path = suningImageResize.ReplaceAllString(u.Path, "$1")
		u.RawPath = ""
	}
	u.Fragment = ""
	return u.String(), true
}

func suningHasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func suningAncestor(n *html.Node, class, id string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && ((class != "" && suningHasClass(p, class)) || (id != "" && attribute(p, "id") == id)) {
			return true
		}
	}
	return false
}

func suningSKUBatch(ctx context.Context, getter Getter, page *url.URL, ref suningRef, codes []string) (map[string]string, error) {
	images := make(map[string]string)
	if len(codes) == 0 || len(codes) > 20 {
		return images, errors.New("苏宁规格图请求数量无效")
	}
	want := map[string]bool{}
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		if !suningSKUCode.MatchString(code) || want[code] {
			return images, errors.New("苏宁规格编号无效")
		}
		want[code] = true
		parts = append(parts, ref.vendor+"-"+code)
	}
	endpoint := "https://product.suning.com/pds-web/ajax/batchMainPicutures/" + strings.Join(parts, "_") + "/clusterPicCallBack.html"
	response, err := getter.Get(ctx, endpoint, func(r *http.Request) {
		r.Header.Set("Accept", "application/json, text/javascript, */*;q=0.8")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", page.Scheme+"://"+page.Host+"/")
	})
	if err != nil {
		return images, err
	}
	if response == nil || response.Body == nil {
		return images, errors.New("苏宁规格接口没有返回数据")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.FinalURL != endpoint {
		return images, errors.New("苏宁规格接口不可用或发生跳转")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return images, err
	}
	if len(body) > maxPageBytes {
		return images, errors.New("苏宁规格数据超过大小限制")
	}
	text := strings.TrimSpace(string(body))
	text = strings.TrimSuffix(text, ";")
	const callback = "clusterPicCallBack("
	if !strings.HasPrefix(text, callback) || !strings.HasSuffix(text, ")") {
		return images, errors.New("苏宁规格接口响应格式无效")
	}
	var data struct {
		List []struct {
			Code     string `json:"cmmdtyCode"`
			Supplier string `json:"supplierCode"`
			Pictures []struct {
				Domain string `json:"domain"`
				Path   string `json:"pictureUrl"`
			} `json:"mainPictureList"`
		} `json:"mainPictureResponseList"`
	}
	if err := json.Unmarshal([]byte(text[len(callback):len(text)-1]), &data); err != nil {
		return images, errors.New("苏宁规格数据不是有效的 JSON")
	}
	incomplete := false
	seen := map[string]bool{}
	for _, entry := range data.List {
		if !want[entry.Code] || entry.Supplier != ref.vendor || seen[entry.Code] {
			incomplete = true
			continue
		}
		seen[entry.Code] = true
		if len(entry.Pictures) == 0 {
			incomplete = true
			continue
		}
		image, ok := suningImageURL(entry.Pictures[0].Domain+entry.Pictures[0].Path, true)
		if !ok {
			incomplete = true
			continue
		}
		images[entry.Code] = image
	}
	if len(images) != len(codes) || incomplete {
		return images, errors.New("苏宁部分规格图未返回或无法验证")
	}
	return images, nil
}

// suningCollect reads only the reference's main, SKU, and detail containers.
// The public batch endpoint is JSONP but is parsed as data and never executed.
func suningCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := suningProductRef(page)
	if doc == nil || getter == nil || ref.item == "" {
		return result, errors.New("苏宁商品页面无效")
	}
	var canonical, itemID bool
	var mismatch bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			u, err := url.Parse(attribute(n, "href"))
			if err != nil || suningProductRef(page.ResolveReference(u)) != ref {
				mismatch = true
			} else {
				canonical = true
			}
		}
		if n.Data == "input" {
			switch attribute(n, "id") {
			case "ga_itemDataBean_itemID":
				if suningNumber(attribute(n, "value")) != ref.item {
					mismatch = true
				} else {
					itemID = true
				}
			case "curPartNumber":
				if suningNumber(attribute(n, "value")) != ref.item {
					mismatch = true
				}
			case "ga_itemDataBean_description_name":
				result.Title = attribute(n, "value")
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if mismatch || !canonical || !itemID {
		return result, errors.New("苏宁页面未能确认当前商品身份")
	}
	var mainImages, detailImages []string
	var partialErrors []error
	var skuCodes []string
	skuSet := map[string]bool{}
	skuImages := map[string]string{}
	incomplete := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "li" && suningHasClass(n, "clr-item") {
			code := attribute(n, "sku")
			if !suningSKUCode.MatchString(code) {
				incomplete = true
				return
			}
			if !skuSet[code] {
				skuSet[code] = true
				skuCodes = append(skuCodes, code)
			}
			walkHTML(n, func(img *html.Node) {
				if img.Type != html.ElementNode || img.Data != "img" || attribute(img, "src") == "" {
					return
				}
				if image, ok := suningImageURL(attribute(img, "src"), true); ok {
					skuImages[code] = image
				}
			})
		}
		if n.Data == "img" && suningAncestor(n, "imgzoom-thumb-main", "") {
			if image, ok := suningImageURL(attribute(n, "src-large"), true); ok {
				mainImages = append(mainImages, image)
			} else {
				incomplete = true
			}
		}
		if n.Data == "img" && suningAncestor(n, "", "productDetail") && attribute(n, "src2") != "" {
			if image, ok := suningImageURL(attribute(n, "src2"), false); ok {
				detailImages = append(detailImages, image)
			} else {
				incomplete = true
			}
		}
	})
	seen := map[string]bool{}
	add := func(image, section, sku string) {
		if seen[image] {
			return
		}
		seen[image] = true
		metadata := map[string]string{"suning_product_id": ref.item, "suning_vendor_id": ref.vendor, "suning_section": section}
		if sku != "" {
			metadata["suning_sku"] = sku
		}
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: metadata}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	for _, image := range mainImages {
		add(image, "主图", "")
	}
	for _, code := range skuCodes {
		if image := skuImages[code]; image != "" {
			add(image, "规格图", code)
		}
	}
	for _, image := range detailImages {
		add(image, "描述图", "")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var missing []string
	for _, code := range skuCodes {
		if skuImages[code] == "" {
			missing = append(missing, code)
		}
	}
	if len(missing) > suningSKULimit {
		missing = missing[:suningSKULimit]
		incomplete = true
	}
	for start := 0; start < len(missing); start += 20 {
		end := start + 20
		if end > len(missing) {
			end = len(missing)
		}
		images, err := suningSKUBatch(ctx, getter, page, ref, missing[start:end])
		for _, code := range missing[start:end] {
			if image := images[code]; image != "" {
				add(image, "规格图", code)
			}
		}
		if err != nil {
			incomplete = true
			partialErrors = append(partialErrors, fmt.Errorf("苏宁规格图请求失败: %w", err))
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(mainImages) == 0 || len(detailImages) == 0 {
		incomplete = true
	}
	if len(result.Items) == 0 {
		return result, errors.Join(append([]error{errors.New("苏宁未返回可确认属于该商品的图片")}, partialErrors...)...)
	}
	if incomplete {
		return result, errors.Join(append([]error{errors.New("苏宁部分主图、规格图或详情图未能采集，结果可能不完整")}, partialErrors...)...)
	}
	return result, nil
}
