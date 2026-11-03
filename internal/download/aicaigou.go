package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	aicaigouIDPattern     = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
	aicaigouShopIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	aicaigouState         = regexp.MustCompile(`\bwindow\.data\s*=\s*`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const (
	maxAicaigouImages = 10000
	maxAicaigouPages  = 100
)

func aicaigouProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "b2b.baidu.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	var id string
	switch {
	case u.Path == "/land":
		values := u.Query()["id"]
		if len(values) != 1 || len(values[0]) != 34 || !strings.HasSuffix(values[0], "10") {
			return ""
		}
		id = values[0][:32]
	case strings.HasPrefix(u.Path, "/land/"):
		id = strings.TrimPrefix(u.Path, "/land/")
		if len(u.Query()["id"]) != 0 {
			return ""
		}
	}
	if !aicaigouIDPattern.MatchString(id) {
		return ""
	}
	return strings.ToLower(id)
}

func isAicaigouProduct(u *url.URL) bool { return aicaigouProductID(u) != "" }

type aicaigouRedirectGetter struct {
	base Getter
	id   string
}

func (g *aicaigouRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || aicaigouProductID(current) != g.id {
		return nil, errors.New("爱采购商品请求地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || aicaigouProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("爱采购商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || !sameOrigin(current, next) || aicaigouProductID(next) != g.id {
			return nil, errors.New("爱采购商品跳转地址无效，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("爱采购商品重定向循环")
		}
		current = next
	}
	return nil, errors.New("爱采购商品重定向次数过多")
}

func discoverAicaigouProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := aicaigouProductID(root)
	if getter == nil || id == "" {
		return result, errors.New("爱采购商品地址或下载客户端无效")
	}
	page, pageGetter := *root, getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("爱采购下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", "b2b.baidu.com"
		pageGetter = &aicaigouRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取爱采购商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || aicaigouProductID(final) != id {
		return result, errors.New("爱采购商品已跳转，可能需要登录或安全验证")
	}
	return aicaigouProductPage(ctx, doc, final)
}

func aicaigouImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || (u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) || u.Path == "" || u.Path == "/" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "b2b-material.cdn.bcebos.com", "b2b-openapi-attachment.cdn.bcebos.com", "ss0.baidu.com", "ss2.baidu.com", "t10.baidu.com", "t11.baidu.com", "t12.baidu.com":
	case "bj.bcebos.com":
		if !strings.HasPrefix(u.Path, "/b2b-openapi-attachment/") {
			return "", false
		}
	default:
		return "", false
	}
	u.Scheme, u.Host = "https", host
	return u.String(), true
}

func aicaigouHasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

type aicaigouItemData struct {
	ID       string          `json:"id"`
	ShopID   json.RawMessage `json:"xzhid"`
	FullName string          `json:"fullName"`
	PicList  json.RawMessage `json:"picList"`
	Detail   json.RawMessage `json:"detail_v2"`
	SKUList  json.RawMessage `json:"skuList"`
}

func aicaigouProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := aicaigouProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("爱采购商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var item aicaigouItemData
	var stateFound, stateValid, challenge bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" {
			title := strings.TrimSpace(nodeText(n))
			if title == "百度安全验证" {
				challenge = true
			}
			if result.Title == "" {
				result.Title = title
			}
		}
		if n.Data != "script" || stateValid {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode || len(child.Data) > maxPageBytes {
				continue
			}
			m := aicaigouState.FindStringIndex(child.Data)
			if m == nil {
				continue
			}
			stateFound = true
			var state struct {
				Item *aicaigouItemData `json:"item"`
			}
			if json.NewDecoder(strings.NewReader(child.Data[m[1]:])).Decode(&state) == nil && state.Item != nil {
				item, stateValid = *state.Item, true
				break
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if challenge {
		return result, errors.New("爱采购需要完成百度安全验证")
	}
	if stateValid && !strings.EqualFold(item.ID, id) {
		return result, errors.New("爱采购页面与请求的商品不匹配")
	}
	if item.FullName != "" {
		result.Title = item.FullName
	}
	seen := make(map[string]bool)
	var problems []error
	var rejected, limited bool
	add := func(raw, kind string) {
		if ctx.Err() != nil || limited || raw == "" {
			return
		}
		image, ok := aicaigouImageURL(page, raw)
		if !ok {
			rejected = true
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxAicaigouImages {
			limited = true
			return
		}
		seen[image] = true
		found := Item{URL: image, RequireImage: true, Metadata: map[string]string{"aicaigou_product_id": id, "image_kind": kind}}
		if shopID := aicaigouDecimalID(item.ShopID); shopID != "" {
			found.Metadata["aicaigou_shop_id"] = shopID
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			found.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, found)
	}
	if stateValid {
		var pics []string
		if len(item.PicList) == 0 || json.Unmarshal(item.PicList, &pics) != nil {
			problems = append(problems, errors.New("爱采购商品主图数据不完整"))
		} else {
			if len(pics) == 0 {
				problems = append(problems, errors.New("爱采购商品主图未返回，图片可能不完整"))
			}
			for _, raw := range pics {
				add(raw, "main")
			}
		}
		var sku []struct {
			Image string `json:"image"`
		}
		if len(item.SKUList) == 0 || json.Unmarshal(item.SKUList, &sku) != nil {
			problems = append(problems, errors.New("爱采购商品规格图数据不完整"))
		} else {
			for _, v := range sku {
				add(v.Image, "variant")
			}
		}
		var detail string
		if len(item.Detail) == 0 || json.Unmarshal(item.Detail, &detail) != nil {
			problems = append(problems, errors.New("爱采购商品详情数据不完整"))
		} else if detail != "" {
			fragment, err := html.Parse(strings.NewReader(detail))
			if err != nil {
				problems = append(problems, fmt.Errorf("爱采购详情解析失败: %w", err))
			} else {
				walkHTML(fragment, func(n *html.Node) {
					if n.Type == html.ElementNode && n.Data == "img" {
						add(attribute(n, "src"), "description")
					}
				})
			}
		}
	} else {
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "img" {
				return
			}
			for p := n.Parent; p != nil; p = p.Parent {
				if aicaigouHasClass(p, "rich-text") {
					add(attribute(n, "src"), "description")
					return
				}
				if aicaigouHasClass(p, "img-big") && aicaigouHasClass(p, "pcui-img") {
					add(attribute(n, "src"), "main")
					return
				}
			}
		})
		if stateFound {
			problems = append(problems, errors.New("爱采购商品数据无法解析，图片可能不完整"))
		} else {
			problems = append(problems, errors.New("爱采购商品数据未返回，图片可能不完整"))
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if rejected {
		problems = append(problems, errors.New("爱采购包含未验证来源的图片，已跳过"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("爱采购商品超过 %d 张图片，结果不完整", maxAicaigouImages))
	}
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("未找到爱采购商品图片"))
	}
	return result, errors.Join(problems...)
}

func aicaigouDecimalID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) != nil {
		id = string(raw)
	}
	if !aicaigouShopIDPattern.MatchString(id) || strings.Trim(id, "0") == "" {
		return ""
	}
	return id
}

func aicaigouShopID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "b2b.baidu.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	var id string
	switch {
	case u.Path == "/shop":
		values := u.Query()["xzhid"]
		if len(values) != 1 {
			return ""
		}
		id = values[0]
	case strings.HasPrefix(u.Path, "/shop/"):
		id = strings.TrimPrefix(u.Path, "/shop/")
		values := u.Query()["xzhid"]
		if len(values) > 1 || (len(values) == 1 && values[0] != id) {
			return ""
		}
	}
	if !aicaigouShopIDPattern.MatchString(id) || strings.Trim(id, "0") == "" {
		return ""
	}
	if types := u.Query()["tpath"]; len(types) > 1 || (len(types) == 1 && types[0] != "prod" && types[0] != "index") {
		return ""
	}
	return id
}

func isAicaigouShop(u *url.URL) bool { return aicaigouShopID(u) != "" }

type aicaigouShopProduct struct {
	ID     string          `json:"id"`
	ShopID json.RawMessage `json:"xzhid"`
	URL    string          `json:"jUrl"`
}

type aicaigouShopList struct {
	ShopID   json.RawMessage       `json:"xzhId"`
	Name     string                `json:"entName"`
	Total    *int                  `json:"dispNum"`
	Size     *int                  `json:"size"`
	Products []aicaigouShopProduct `json:"productList"`
}

func aicaigouShopPage(ctx context.Context, getter Getter, page *url.URL, id string) (*aicaigouShopList, error) {
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("读取爱采购店铺失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(page, final) || aicaigouShopID(final) != id || final.Path != page.Path || final.RawQuery != page.RawQuery {
		return nil, errors.New("爱采购店铺页面已跳转，可能需要登录或安全验证")
	}
	var list aicaigouShopList
	valid, challenge := false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && strings.TrimSpace(nodeText(n)) == "百度安全验证" {
			challenge = true
		}
		if n.Data != "script" || valid {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode || len(child.Data) > maxPageBytes {
				continue
			}
			match := aicaigouState.FindStringIndex(child.Data)
			if match == nil {
				continue
			}
			if json.NewDecoder(strings.NewReader(child.Data[match[1]:])).Decode(&list) == nil {
				valid = true
				break
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if challenge {
		return nil, errors.New("爱采购需要完成百度安全验证")
	}
	if !valid || aicaigouDecimalID(list.ShopID) != id || list.Total == nil || list.Size == nil || *list.Total < 0 || *list.Size != 30 || list.Products == nil {
		return nil, errors.New("爱采购店铺商品列表或分页数据不完整")
	}
	return &list, nil
}

func aicaigouShopAPI(ctx context.Context, getter Getter, page *url.URL, id string, index int) (*aicaigouShopList, error) {
	endpoint := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/land/entProdAjax", RawQuery: url.Values{"xzhid": {id}, "p": {strconv.Itoa(index)}, "s": {"30"}}.Encode()}
	response, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) { r.Header.Set("Accept", "application/json") })
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	defer response.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, endpoint.String()))
	if err != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path || final.RawQuery != endpoint.RawQuery {
		return nil, errors.New("爱采购店铺分页接口已跳转")
	}
	if response.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: endpoint.String(), StatusCode: response.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	if len(body) > maxPageBytes {
		return nil, errors.New("爱采购店铺分页超过大小上限")
	}
	var data struct {
		Status *int              `json:"status"`
		Data   *aicaigouShopList `json:"data"`
	}
	if json.Unmarshal(body, &data) != nil || data.Status == nil || *data.Status != 0 || data.Data == nil || data.Data.Total == nil || *data.Data.Total < 0 || data.Data.Products == nil {
		return nil, errors.New("爱采购店铺分页数据无效，可能需要登录或安全验证")
	}
	return data.Data, nil
}

func discoverAicaigouShop(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := aicaigouShopID(root)
	if getter == nil || id == "" {
		return result, errors.New("爱采购店铺地址或下载客户端无效")
	}
	pageGetter := getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("爱采购下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		pageGetter = &clone
	} else if root.Scheme != "https" {
		return result, errors.New("请在下载浏览器打开 HTTPS 爱采购店铺")
	}
	page := &url.URL{Scheme: "https", Host: "b2b.baidu.com", Path: "/shop", RawQuery: url.Values{"xzhid": {id}, "tpath": {"prod"}}.Encode()}
	list, err := aicaigouShopPage(ctx, pageGetter, page, id)
	if err != nil {
		return result, err
	}
	result.Title = list.Name
	seenProducts := make(map[string]bool)
	var problems []error
	var firstProductError error
	productErrors, invalid := 0, 0
	finish := func(extra error) (Discovery, error) {
		if extra != nil {
			problems = append(problems, extra)
		}
		if invalid > 0 {
			problems = append(problems, fmt.Errorf("%d 个爱采购商品无效、重复或归属不匹配，结果不完整", invalid))
		}
		if productErrors > 0 {
			problems = append(problems, fmt.Errorf("%d 个爱采购商品读取失败或不完整: %w", productErrors, firstProductError))
		}
		if len(result.Items) == 0 && extra == nil {
			problems = append(problems, errors.New("未找到爱采购店铺商品图片"))
		}
		return result, errors.Join(problems...)
	}
	for index := 1; index <= maxAicaigouPages; index++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if index > 1 {
			list, err = aicaigouShopAPI(ctx, pageGetter, page, id, index)
			if err != nil {
				problems = append(problems, fmt.Errorf("爱采购店铺第 %d 页读取失败: %w", index, err))
				break
			}
		}
		if len(list.Products) > 30 {
			problems = append(problems, fmt.Errorf("爱采购店铺第 %d 页商品数量异常，结果不完整", index))
			list.Products = list.Products[:30]
		}
		newProducts := 0
		for _, product := range list.Products {
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			productURL, parseErr := crawlURL(page, product.URL)
			productID := aicaigouProductID(productURL)
			if parseErr != nil || productID == "" || !sameOrigin(page, productURL) || !strings.EqualFold(productID, product.ID) || aicaigouDecimalID(product.ShopID) != id {
				invalid++
				continue
			}
			if seenProducts[productID] {
				invalid++
				continue
			}
			seenProducts[productID], newProducts = true, newProducts+1
			found, fetchErr := discoverAicaigouProduct(ctx, pageGetter, productURL)
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			if fetchErr != nil {
				productErrors++
				if firstProductError == nil {
					firstProductError = fetchErr
				}
			}
			albumTitle := firstNonEmpty(found.Title, "商品 "+productID)
			for _, item := range found.Items {
				if item.Metadata["aicaigou_shop_id"] != id {
					invalid++
					continue
				}
				if len(result.Items) >= maxAicaigouImages {
					return finish(fmt.Errorf("爱采购店铺超过 %d 张图片，结果不完整", maxAicaigouImages))
				}
				item.Metadata["album_id"] = productID
				item.Metadata["album_title"] = albumTitle
				result.Items = append(result.Items, item)
			}
			var status *HTTPStatusError
			var transfer *TransferError
			if errors.As(fetchErr, &transfer) || (errors.As(fetchErr, &status) && (status.StatusCode == 403 || status.StatusCode == 429)) {
				return finish(fmt.Errorf("爱采购店铺商品读取中断: %w", fetchErr))
			}
		}
		expected := *list.Total - (index-1)*30
		if expected > 30 {
			expected = 30
		}
		if expected > len(list.Products) {
			problems = append(problems, fmt.Errorf("爱采购店铺第 %d 页商品数量不足，结果不完整", index))
		}
		if *list.Total <= index*30 {
			break
		}
		if len(list.Products) == 0 || newProducts == 0 {
			problems = append(problems, errors.New("爱采购店铺分页为空或重复，结果不完整"))
			break
		}
		if index == maxAicaigouPages {
			problems = append(problems, fmt.Errorf("爱采购店铺超过 %d 页，结果不完整", maxAicaigouPages))
		}
	}
	return finish(nil)
}
