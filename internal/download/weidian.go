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
	"sort"
	"strings"

	"golang.org/x/net/html"
)

var (
	weidianProductHost = regexp.MustCompile(`^shop[0-9]{1,20}\.v\.weidian\.com$`)
	weidianIDPattern   = regexp.MustCompile(`^[0-9]{6,20}$`)
	weidianImageSuffix = regexp.MustCompile(`(?i)\.(?:jpg|jpeg|png)\.webp$`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxWeidianImages = 10000

func weidianProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "/item.html" ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "weidian.com" && !weidianProductHost.MatchString(host) {
		return ""
	}
	var id string
	found := false
	for name, values := range u.Query() {
		if strings.EqualFold(name, "itemid") {
			if found || len(values) != 1 {
				return ""
			}
			id, found = values[0], true
		}
	}
	if !weidianIDPattern.MatchString(id) || strings.Trim(id, "0") == "" {
		return ""
	}
	return id
}

func isWeidianProduct(u *url.URL) bool { return weidianProductID(u) != "" }

func weidianAnonymousGetter(getter Getter) Getter {
	var source *HTTPGetter
	switch g := getter.(type) {
	case *HTTPGetter:
		source = g
	case *RenderedSession:
		source = g.getter
	}
	if source == nil || source.client == nil {
		return nil
	}
	clone, client := *source, *source.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil
	clone.client = &client
	return &clone
}

// Only follow public HTTPS redirects to a recognized page for the same item.
type weidianRedirectGetter struct {
	base Getter
	id   string
}

func (g *weidianRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || weidianProductID(current) != g.id {
		return nil, errors.New("微店商品请求地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || final.Path != current.Path || weidianProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("微店商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || weidianProductID(next) != g.id {
			return nil, errors.New("微店商品跳转地址无效或商品不匹配")
		}
		current = next
	}
	return nil, errors.New("微店商品重定向次数过多")
}

func discoverWeidianProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := weidianProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("微店商品地址或下载客户端无效")
	}
	page := *root
	pageGetter := getter
	validatedRedirects := false
	if _, ok := getter.(*HTTPGetter); ok {
		anon := weidianAnonymousGetter(getter)
		if anon == nil {
			return result, errors.New("微店下载客户端无效")
		}
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter, validatedRedirects = &weidianRedirectGetter{base: anon, id: id}, true
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取微店商品失败: %w", err)
	}
	if doc == nil || final == nil || weidianProductID(final) != id ||
		(!sameOrigin(&page, final) && !validatedRedirects) {
		return result, errors.New("微店商品已跳转，可能需要登录或安全验证")
	}
	return weidianCollect(ctx, getter, doc, final)
}

type weidianVariant struct {
	Image string `json:"img"`
	Value string `json:"attr_value"`
	Title string `json:"title"`
}

type weidianState struct {
	Status struct {
		Code *int `json:"code"`
	} `json:"status"`
	Result struct {
		DefaultModel struct {
			Item struct {
				ID     json.RawMessage `json:"item_id"`
				Name   string          `json:"item_name"`
				Images []string        `json:"imgs"`
			} `json:"item_info"`
			SKU *struct {
				Attributes []struct {
					Values []weidianVariant `json:"attr_values"`
				} `json:"attr_list"`
				Values map[string]weidianVariant `json:"sku"`
			} `json:"sku_properties"`
		} `json:"default_model"`
	} `json:"result"`
}

func weidianIDValue(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	if !weidianIDPattern.MatchString(text) || strings.Trim(text, "0") == "" {
		return ""
	}
	return text
}

func weidianHasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func weidianImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "si.geilicdn.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path == "" || u.Path == "/" {
		return "", false
	}
	u.Scheme, u.Host = "https", "si.geilicdn.com"
	return u.String(), true
}

func weidianFullImage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return raw
	}
	for key, values := range query {
		if (key != "w" && key != "h" && key != "cp") || len(values) != 1 || values[0] == "" || strings.Trim(values[0], "0123456789") != "" {
			return raw
		}
	}
	path := u.Path
	if weidianImageSuffix.MatchString(path) {
		path = path[:len(path)-len(".webp")]
	}
	if path != u.Path || u.RawQuery != "" {
		u.Path, u.RawPath, u.RawQuery, u.ForceQuery = path, "", "", false
	}
	return u.String()
}

type weidianDetailImage struct {
	Type int    `json:"type"`
	URL  string `json:"url"`
}

func weidianDetails(ctx context.Context, getter Getter, id string) ([]weidianDetailImage, error) {
	anon := weidianAnonymousGetter(getter)
	if anon == nil {
		return nil, errors.New("无法匿名读取微店商品详情")
	}
	param, err := json.Marshal(struct {
		ID        string `json:"vItemId"`
		Translate bool   `json:"needTranslate"`
	}{ID: id, Translate: true})
	if err != nil || !weidianIDPattern.MatchString(id) {
		return nil, errors.New("微店商品编号无效")
	}
	target := &url.URL{Scheme: "https", Host: "thor.weidian.com", Path: "/detail/getDetailDesc/1.0", RawQuery: url.Values{"param": {string(param)}}.Encode()}
	response, err := anon.Get(ctx, target.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
	})
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	defer response.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, target.String()))
	if err != nil || !sameOrigin(target, final) || final.Path != target.Path || final.RawQuery != target.RawQuery {
		return nil, errors.New("微店详情响应地址不匹配")
	}
	if response.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: target.String(), StatusCode: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	if len(raw) > maxPageBytes {
		return nil, errors.New("微店商品详情超过 8 MiB")
	}
	var data struct {
		Status struct {
			Code *int `json:"code"`
		} `json:"status"`
		Result struct {
			ItemDetail *struct {
				Content *[]weidianDetailImage `json:"desc_content"`
			} `json:"item_detail"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || data.Status.Code == nil || *data.Status.Code != 0 ||
		data.Result.ItemDetail == nil || data.Result.ItemDetail.Content == nil {
		return nil, errors.New("微店商品详情未返回有效图片数据")
	}
	return *data.Result.ItemDetail.Content, nil
}

func weidianProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	return weidianCollect(ctx, nil, doc, page)
}

func weidianCollect(ctx context.Context, getter Getter, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := weidianProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("微店商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var state weidianState
	stateSeen, stateValid, mismatch := false, false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || stateValid || n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		raw := attribute(n, "data-obj")
		if raw == "" || len(raw) > maxPageBytes {
			return
		}
		stateSeen = true
		var candidate weidianState
		if json.Unmarshal([]byte(raw), &candidate) != nil || candidate.Status.Code == nil || *candidate.Status.Code != 0 {
			return
		}
		if weidianIDValue(candidate.Result.DefaultModel.Item.ID) != id {
			mismatch = true
			return
		}
		state, stateValid = candidate, true
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if mismatch && !stateValid {
		return result, errors.New("微店商品数据与请求的商品不匹配")
	}
	result.Title = state.Result.DefaultModel.Item.Name
	seen := make(map[string]bool)
	invalid, limited, mainFound := 0, false, false
	add := func(raw, kind, variant string) {
		if ctx.Err() != nil || limited || raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
			return
		}
		original, ok := weidianImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		full := weidianFullImage(original)
		if kind == "main" {
			mainFound = true
		}
		if seen[full] {
			return
		}
		if len(result.Items) >= maxWeidianImages {
			limited = true
			return
		}
		seen[full] = true
		item := Item{URL: full, RequireImage: true, Metadata: map[string]string{"weidian_item_id": id, "image_kind": kind}}
		if len(variant) > 512 {
			variant = variant[:512]
		}
		if variant != "" {
			item.Metadata["variant"] = variant
		}
		if full != original {
			item.AlternateURLs = []string{original}
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", full); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for _, raw := range state.Result.DefaultModel.Item.Images {
		add(raw, "main", "")
	}
	if sku := state.Result.DefaultModel.SKU; sku != nil {
		for _, group := range sku.Attributes {
			for _, variant := range group.Values {
				add(variant.Image, "sku", variant.Value)
			}
		}
		keys := make([]string, 0, len(sku.Values))
		for key := range sku.Values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			variant := sku.Values[key]
			add(variant.Image, "sku", variant.Title)
		}
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "img" {
			return
		}
		if weidianHasClass(n, "item-img") {
			add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), "main", "")
		} else if weidianHasClass(n, "d-image") && n.Parent != nil && weidianHasClass(n.Parent, "d-content") {
			add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), "description", "")
		}
	})
	var problems []error
	if stateSeen && !stateValid {
		problems = append(problems, errors.New("微店商品数据无法解析，图片可能不完整"))
	}
	if !stateValid || state.Result.DefaultModel.SKU == nil {
		problems = append(problems, errors.New("微店规格数据未返回，规格图片可能不完整"))
	}
	if stateValid {
		images, err := weidianDetails(ctx, getter, id)
		if err != nil {
			problems = append(problems, fmt.Errorf("读取微店商品详情失败，详情图片可能不完整: %w", err))
		} else {
			for _, image := range images {
				if image.Type == 2 || image.Type == 10000 {
					add(image.URL, "description", "")
				} else if image.URL != "" {
					problems = append(problems, errors.New("微店商品详情包含未识别的图片元素"))
				}
			}
		}
	} else {
		problems = append(problems, errors.New("未确认微店商品编号，详情图片可能不完整"))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !mainFound {
		problems = append(problems, errors.New("微店商品未返回主图，可能需要登录或安全验证"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("微店商品有 %d 张图片地址无效或不受支持", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("微店商品图片超过 %d 张，结果不完整", maxWeidianImages))
	}
	return result, errors.Join(problems...)
}
