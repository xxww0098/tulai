package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	dewuIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	// This is the inline product-data call observed in the public H5 page.
	dewuStateStart = regexp.MustCompile(`__remixContext\s*\.\s*r\s*\(\s*["']routes/page\.\$pageName["']\s*,\s*["']simpleDetail["']\s*,\s*`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxDewuImages = 10000

func dewuProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "fast.dewu.com", "m.dewu.com", "cdn-m.dewu.com", "www.dewu.com", "dewu.com":
	default:
		return ""
	}
	if u.Path != "/page/productDetail" && u.Path != "/product-detail.html" {
		return ""
	}
	ids := u.Query()["spuId"]
	if len(ids) != 1 || !dewuIDPattern.MatchString(ids[0]) {
		return ""
	}
	return ids[0]
}

func isDewuProduct(u *url.URL) bool { return dewuProductID(u) != "" }

type dewuRedirectGetter struct {
	base Getter
	id   string
}

func (g *dewuRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || dewuProductID(current) != g.id {
		return nil, errors.New("得物商品地址无效")
	}
	for i := 0; i < 6; i++ {
		resp, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || dewuProductID(final) != g.id {
			resp.Body.Close()
			return nil, errors.New("得物商品响应地址无效")
		}
		if resp.StatusCode != 301 && resp.StatusCode != 302 && resp.StatusCode != 303 && resp.StatusCode != 307 && resp.StatusCode != 308 {
			return resp, nil
		}
		location := resp.Header.Get("Location")
		resp.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || !sameOrigin(current, next) || dewuProductID(next) != g.id {
			return nil, errors.New("得物商品已跳转，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("得物商品重定向循环")
		}
		current = next
	}
	return nil, errors.New("得物商品重定向次数过多")
}

func discoverDewuProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := dewuProductID(root)
	if getter == nil || id == "" {
		return result, errors.New("得物商品地址或下载客户端无效")
	}
	page, pageGetter := *root, getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("得物下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter = &dewuRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		var status *HTTPStatusError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return result, fmt.Errorf("得物商品可能需要登录或安全验证: %w", err)
			case http.StatusTooManyRequests:
				return result, fmt.Errorf("得物请求受到限流，请稍后重试: %w", err)
			}
		}
		return result, fmt.Errorf("读取得物商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || dewuProductID(final) != id {
		return result, errors.New("得物商品已跳转，可能需要登录或安全验证")
	}
	return dewuProductPage(ctx, doc, final)
}

func dewuImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || initial.User != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.EqualFold(u.Hostname(), "cdn.poizon.com") ||
		(u.Port() != "" && u.Port() != "443") || u.Path == "" || u.Path == "/" {
		return "", false
	}
	u.Host, u.Fragment = "cdn.poizon.com", ""
	return u.String(), true
}

type dewuImage struct {
	URL string `json:"url"`
}

type dewuState struct {
	ModelSequence json.RawMessage `json:"modelSequence"`
	Detail        struct {
		SPUID  json.Number `json:"spuId"`
		Title  string      `json:"title"`
		IsShow *int        `json:"isShow"`
	} `json:"detail"`
	Image           json.RawMessage `json:"image"`
	ImageAndTextMap json.RawMessage `json:"imageAndTextMap"`
	SaleProperties  json.RawMessage `json:"saleProperties"`
}

func dewuProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := dewuProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("得物商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var states []dewuState
	var malformed, challenge bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if attribute(n, "id") == "dw-slide" || (n.Data == "title" && strings.Contains(nodeText(n), "安全验证")) {
			challenge = true
		}
		if n.Data != "script" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode || len(c.Data) > maxPageBytes {
				continue
			}
			match := dewuStateStart.FindStringIndex(c.Data)
			if match == nil {
				continue
			}
			var state dewuState
			if err := json.NewDecoder(strings.NewReader(c.Data[match[1]:])).Decode(&state); err != nil || len(state.ModelSequence) == 0 {
				malformed = true
				continue
			}
			states = append(states, state)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if challenge {
		return result, errors.New("得物需要完成安全验证")
	}
	if len(states) == 0 {
		return result, errors.New("得物商品数据未加载，可能需要登录或安全验证；PC 商品页暂未验证")
	}
	for _, state := range states {
		if !dewuIDPattern.MatchString(state.Detail.SPUID.String()) || state.Detail.SPUID.String() != id {
			return result, errors.New("得物页面缺少匹配的商品标识，无法确认图片归属")
		}
		if state.Detail.IsShow != nil && *state.Detail.IsShow == 0 {
			return result, errors.New("得物商品当前不可见")
		}
	}
	seen := make(map[string]bool)
	var invalid, limited, badMain, badSKU, badDesc, hasMain, descProvided bool
	add := func(raw, kind, variant string) {
		if ctx.Err() != nil || limited || raw == "" {
			return
		}
		image, ok := dewuImageURL(page, raw)
		if !ok {
			invalid = true
			return
		}
		if kind == "main" {
			hasMain = true
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxDewuImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"dewu_spu_id": id, "image_kind": kind}}
		if chars := []rune(variant); len(chars) > 256 {
			variant = string(chars[:256])
		}
		if variant != "" {
			item.Metadata["variant"] = variant
		}
		if ref := imageReferer("https://"+page.Hostname()+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for _, state := range states {
		if result.Title == "" {
			result.Title = state.Detail.Title
		}
		var main struct {
			SPUImage struct {
				Images []json.RawMessage `json:"images"`
			} `json:"spuImage"`
		}
		if len(state.Image) == 0 || json.Unmarshal(state.Image, &main) != nil {
			badMain = true
		} else {
			for _, raw := range main.SPUImage.Images {
				var image dewuImage
				if err := json.Unmarshal(raw, &image); err != nil {
					badMain = true
					continue
				}
				add(image.URL, "main", "")
			}
		}
		var descriptions struct {
			Spread []json.RawMessage `json:"spuShowSpread"`
		}
		if len(state.ImageAndTextMap) == 0 || json.Unmarshal(state.ImageAndTextMap, &descriptions) != nil {
			badDesc = true
		} else {
			for _, raw := range descriptions.Spread {
				var section struct {
					Type   string            `json:"type"`
					Images []json.RawMessage `json:"images"`
				}
				if err := json.Unmarshal(raw, &section); err != nil {
					badDesc = true
					continue
				}
				if section.Type != "image" {
					badDesc = true
					continue
				}
				descProvided = true
				for _, rawImage := range section.Images {
					var image dewuImage
					if err := json.Unmarshal(rawImage, &image); err != nil {
						badDesc = true
						continue
					}
					add(image.URL, "description", "")
				}
			}
		}
		var properties struct {
			List []json.RawMessage `json:"list"`
		}
		if len(state.SaleProperties) == 0 || json.Unmarshal(state.SaleProperties, &properties) != nil {
			badSKU = true
		} else {
			for _, raw := range properties.List {
				var prop struct {
					URL       string `json:"levelImageUrl"`
					Value     string `json:"value"`
					ShowValue *int   `json:"showValue"`
				}
				if err := json.Unmarshal(raw, &prop); err != nil {
					badSKU = true
					continue
				}
				if prop.URL != "" && (prop.ShowValue == nil || *prop.ShowValue != 1) {
					badSKU = true
					continue
				}
				add(prop.URL, "variant", prop.Value)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if malformed || badMain || !hasMain {
		problems = append(problems, errors.New("得物主图数据不完整或未加载"))
	}
	if badSKU {
		problems = append(problems, errors.New("得物规格图数据不完整或包含不可确认的选项"))
	}
	if badDesc || !descProvided {
		problems = append(problems, errors.New("得物详情图未完整提供，结果可能不完整"))
	}
	if invalid {
		problems = append(problems, errors.New("得物包含未验证来源的图片，已跳过"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("得物商品超过 %d 张图片，结果不完整", maxDewuImages))
	}
	return result, errors.Join(problems...)
}
