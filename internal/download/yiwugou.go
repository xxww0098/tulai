package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	yiwugouPath    = regexp.MustCompile(`^/product/detail/([0-9]{1,24})\.html$`)
	yiwugouDigits  = regexp.MustCompile(`^[0-9]{1,24}$`)
	yiwugouState   = regexp.MustCompile(`\bwindow\s*\.\s*__INITIAL_STATE__\s*=`)
	yiwugouImage1  = regexp.MustCompile(`^/i[0-9]{3}/`)
	yiwugouResize  = regexp.MustCompile(`^image/resize,[A-Za-z0-9_,.]+$`)
	yiwugouNumeric = regexp.MustCompile(`^[0-9]{1,32}$`)
)

const maxYiwugouImages = 10000

func yiwugouProductID(page *url.URL) string {
	if page == nil || page.User != nil || (page.Scheme != "http" && page.Scheme != "https") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) ||
		strings.Contains(page.EscapedPath(), "%") {
		return ""
	}
	switch strings.ToLower(page.Hostname()) {
	case "yiwugo.com", "www.yiwugo.com", "wap.yiwugo.com", "yiwugou.com", "www.yiwugou.com":
	default:
		return ""
	}
	match := yiwugouPath.FindStringSubmatch(page.Path)
	if len(match) != 2 || strings.Trim(match[1], "0") == "" {
		return ""
	}
	return match[1]
}

func isYiwugouProduct(page *url.URL) bool { return yiwugouProductID(page) != "" }

func yiwugouStateID(raw json.RawMessage) string {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		if json.Unmarshal(raw, &value) != nil {
			return ""
		}
	}
	if !yiwugouDigits.MatchString(value) {
		return ""
	}
	return value
}

// yiwugouImageURL accepts only image locations observed in official products.
// Known resize/cache parameters can be dropped; unknown signatures are kept.
func yiwugouImageURL(page *url.URL, raw string) (string, bool, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false, false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || initial.User != nil || strings.Contains(initial.Path, "..") || strings.Contains(initial.Path, "\\") ||
		strings.Contains(strings.ToLower(initial.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(initial.EscapedPath()), "%5c") {
		return "", false, false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return "", false, false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "cbu01.alicdn.com":
		if !strings.HasPrefix(u.Path, "/img/ibank/") {
			return "", false, false
		}
	case "img1.yiwugo.com":
		if !yiwugouImage1.MatchString(u.Path) {
			return "", false, false
		}
	case "ywgimg.yiwugo.com":
		if !strings.HasPrefix(u.Path, "/product/") {
			return "", false, false
		}
	default:
		return "", false, false
	}
	u.Scheme, u.Host = "https", host
	kept := false
	if u.RawQuery != "" {
		values, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "", false, false
		}
		known := strings.HasSuffix(strings.ToLower(u.Path), ".jpg")
		for key, value := range values {
			if len(value) != 1 {
				known = false
				continue
			}
			switch key {
			case "__r__":
				known = known && yiwugouNumeric.MatchString(value[0])
			case "x-oss-process":
				known = known && yiwugouResize.MatchString(value[0])
			default:
				known = false
			}
		}
		if known {
			u.RawQuery, u.ForceQuery = "", false
		} else {
			kept = true
		}
	}
	return u.String(), true, kept
}

type yiwugouPhoto struct {
	GoodsID  json.RawMessage `json:"goodsid"`
	Picture  string          `json:"picture"`
	Picture1 string          `json:"picture1"`
	Picture2 string          `json:"picture2"`
	Picture3 string          `json:"picture3"`
	Picture4 string          `json:"picture4"`
}

type yiwugouVariant struct {
	ProductID         json.RawMessage `json:"productId"`
	Picture           string          `json:"picture"`
	SpecificationName string          `json:"specificationName"`
	PropertyValues    string          `json:"propertyValues"`
}

type yiwugouData struct {
	C struct {
		ProductID json.RawMessage `json:"productId"`
		Detail    struct {
			ID              json.RawMessage `json:"id"`
			ProductDetailVO struct {
				ID       json.RawMessage `json:"id"`
				Title    string          `json:"title"`
				Picture  string          `json:"picture"`
				Picture1 string          `json:"picture1"`
				Picture2 string          `json:"picture2"`
				Picture3 string          `json:"picture3"`
				Picture4 string          `json:"picture4"`
				Detail   json.RawMessage `json:"detaill"`
			} `json:"productDetailVO"`
			Photos     json.RawMessage `json:"sdiProductsPicList"`
			SKUs       json.RawMessage `json:"productPropertySkuBoList"`
			Dimensions json.RawMessage `json:"productPropertyDimensionTwoList"`
		} `json:"detail"`
	} `json:"c"`
}

func yiwugouArray(raw json.RawMessage) ([]json.RawMessage, bool, bool) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return nil, false, true
	}
	if value[0] != '[' {
		return nil, true, false
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil, true, false
	}
	return entries, true, true
}

func discoverYiwugouProduct(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := yiwugouProductID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("义乌购商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取义乌购商品失败: %w", err)
	}
	if doc == nil || final == nil || yiwugouProductID(final) != id || (page.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("义乌购商品已跳转，可能需要登录或安全验证")
	}
	return yiwugouCollect(ctx, getter, final, doc)
}

func yiwugouHasClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func yiwugouDOMKind(n *html.Node) string {
	var main, sku, detail, product, picture bool
	for p := n; p != nil; p = p.Parent {
		product = product || yiwugouHasClass(p, "product-detail-page")
		picture = picture || yiwugouHasClass(p, "picture-viewer")
		main = main || yiwugouHasClass(p, "swiper-wrapper")
		sku = sku || yiwugouHasClass(p, "specification-box") || (yiwugouHasClass(p, "sales") && yiwugouHasClass(p, "product-item"))
		detail = detail || yiwugouHasClass(p, "tab-detail")
	}
	if !product {
		return ""
	}
	if detail {
		return "detail"
	}
	if sku {
		return "sku"
	}
	if main && picture {
		return "main"
	}
	return ""
}

// yiwugouCollect handles fetched HTML and the user's rendered product DOM.
// It ignores unrelated state fields, including authentication values.
func yiwugouCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := yiwugouProductID(page)
	if getter == nil || id == "" || doc == nil {
		return result, errors.New("义乌购商品页面或下载客户端无效")
	}
	var state yiwugouData
	var found, malformed bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			for _, match := range offer1688CodeMatches(child.Data, yiwugouState) {
				if found {
					malformed = true
					continue
				}
				tail := strings.TrimSpace(child.Data[match[1]:])
				decoder := json.NewDecoder(strings.NewReader(tail))
				var value yiwugouData
				if decoder.Decode(&value) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					malformed = true
					continue
				}
				state, found = value, true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !found || malformed {
		return result, errors.New("无法读取义乌购商品数据，请等待页面加载或打开电脑端商品页")
	}
	product := state.C.Detail
	if yiwugouStateID(state.C.ProductID) != id || yiwugouStateID(product.ID) != id || yiwugouStateID(product.ProductDetailVO.ID) != id {
		return result, errors.New("义乌购商品数据与当前商品 ID 不匹配")
	}
	if product.ProductDetailVO.Title != "" {
		result.Title = product.ProductDetailVO.Title
	}
	seen := map[string]int{}
	var main, detail, sku, invalid, limited, retained bool
	add := func(raw, kind, title string) bool {
		if raw == "" || ctx.Err() != nil {
			return false
		}
		image, ok, kept := yiwugouImageURL(page, raw)
		if !ok {
			return false
		}
		retained = retained || kept
		switch kind {
		case "main":
			main = true
		case "sku":
			sku = true
		case "detail":
			detail = true
		}
		if index, exists := seen[image]; exists {
			item := &result.Items[index]
			kinds := strings.Split(item.Metadata["yiwugou_image_kinds"], ",")
			already := false
			for _, existing := range kinds {
				already = already || existing == kind
			}
			if !already {
				item.Metadata["yiwugou_image_kinds"] += "," + kind
			}
			if kind == "sku" && title != "" && item.Metadata["yiwugou_sku_title"] == "" {
				item.Metadata["yiwugou_sku_title"] = title
			}
			return true
		}
		if len(result.Items) >= maxYiwugouImages {
			limited = true
			return true
		}
		seen[image] = len(result.Items)
		metadata := map[string]string{"yiwugou_product_id": id, "image_kind": kind, "yiwugou_image_kinds": kind}
		if title != "" {
			metadata["title"] = title
			if kind == "sku" {
				metadata["yiwugou_sku_title"] = title
			}
		}
		item := Item{URL: image, RequireImage: true, Metadata: metadata, Headers: map[string]string{"Accept": "image/jpeg,image/png,image/gif,*/*;q=0.5"}}
		if ref := imageReferer(page.String(), image); ref != "" {
			item.Headers["Referer"] = ref
		}
		result.Items = append(result.Items, item)
		return true
	}
	vo := product.ProductDetailVO
	cover := firstNonEmpty(vo.Picture, vo.Picture4, vo.Picture3, vo.Picture2, vo.Picture1)
	if cover != "" && !add(cover, "main", "") {
		invalid = true
	}
	photos, photosPresent, photosValid := yiwugouArray(product.Photos)
	invalid = invalid || !photosValid
	for _, entry := range photos {
		var photo yiwugouPhoto
		if json.Unmarshal(entry, &photo) != nil {
			invalid = true
			continue
		}
		if yiwugouStateID(photo.GoodsID) != id {
			invalid = true
			continue
		}
		raw := firstNonEmpty(photo.Picture, photo.Picture4, photo.Picture3, photo.Picture2, photo.Picture1)
		if raw != "" && !add(raw, "main", "") {
			invalid = true
		}
	}
	skus, skusPresent, skusValid := yiwugouArray(product.SKUs)
	dimensions, dimensionsPresent, dimensionsValid := yiwugouArray(product.Dimensions)
	invalid = invalid || !skusValid || !dimensionsValid
	for _, group := range [][]json.RawMessage{skus, dimensions} {
		for _, entry := range group {
			var variant yiwugouVariant
			if json.Unmarshal(entry, &variant) != nil {
				invalid = true
				continue
			}
			if variant.Picture == "" {
				continue
			}
			if yiwugouStateID(variant.ProductID) != id || !add(variant.Picture, "sku", firstNonEmpty(variant.SpecificationName, variant.PropertyValues)) {
				invalid = true
			}
		}
	}
	var detailHTML string
	if len(vo.Detail) > 0 && string(vo.Detail) != "null" && json.Unmarshal(vo.Detail, &detailHTML) != nil {
		invalid = true
	}
	if len(detailHTML) > maxPageBytes {
		invalid = true
	} else if detailHTML != "" {
		fragment, err := html.Parse(strings.NewReader(detailHTML))
		if err != nil {
			invalid = true
		} else {
			walkHTML(fragment, func(n *html.Node) {
				if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "img" {
					return
				}
				for _, key := range []string{"data-original", "data-src", "src"} {
					raw := attribute(n, key)
					if raw == "" || !possibleImage(raw) {
						continue
					}
					if add(raw, "detail", "") {
						return
					}
					invalid = true
				}
			})
		}
	}
	// The site's reference selectors are constrained to the product container;
	// use lazy-loaded attributes before generic placeholder src values.
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "img" {
			return
		}
		kind := yiwugouDOMKind(n)
		if kind == "" {
			return
		}
		for _, key := range []string{"data-original", "data-src", "src"} {
			raw := attribute(n, key)
			if raw == "" || !possibleImage(raw) {
				continue
			}
			if add(raw, kind, attribute(n, "alt")) {
				return
			}
			if !strings.Contains(raw, "static.yiwugo.com/yiwugou/ywgimages/yiwugo_default.jpg") {
				invalid = true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var issues []error
	if !main {
		issues = append(issues, errors.New("未找到义乌购商品主图，可能需要登录或安全验证"))
	}
	if !photosPresent {
		issues = append(issues, errors.New("未读取义乌购商品主图列表，结果可能不完整"))
	}
	if !detail {
		issues = append(issues, errors.New("未读取义乌购商品详情图，结果可能不完整"))
	}
	if !sku && !skusPresent && !dimensionsPresent {
		issues = append(issues, errors.New("未读取义乌购商品规格数据，结果可能不完整"))
	}
	if invalid {
		issues = append(issues, errors.New("部分义乌购商品图片或所属商品 ID 无效，结果可能不完整"))
	}
	if retained {
		issues = append(issues, errors.New("部分义乌购图片包含未知参数，已保留，原图尺寸无法确认"))
	}
	if limited {
		issues = append(issues, fmt.Errorf("义乌购商品超过 %d 张图片上限", maxYiwugouImages))
	}
	return result, errors.Join(issues...)
}
