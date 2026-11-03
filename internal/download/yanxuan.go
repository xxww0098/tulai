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
	yanxuanIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	yanxuanItemState = regexp.MustCompile(`\bvar\s+JSON_DATA_FROMFTL\s*=\s*\{\s*"item"\s*:\s*`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxYanxuanImages = 10000

func yanxuanProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "/item/detail" ||
		!strings.EqualFold(u.Hostname(), "you.163.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	values := u.Query()["id"]
	if len(values) != 1 || !yanxuanIDPattern.MatchString(values[0]) || strings.Trim(values[0], "0") == "" {
		return ""
	}
	return values[0]
}

func isYanxuanProduct(u *url.URL) bool { return yanxuanProductID(u) != "" }

type yanxuanRedirectGetter struct {
	base Getter
	id   string
}

func (g *yanxuanRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || yanxuanProductID(current) != g.id {
		return nil, errors.New("网易严选商品请求地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || final.Path != current.Path || yanxuanProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("网易严选商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || !sameOrigin(current, next) || yanxuanProductID(next) != g.id {
			return nil, errors.New("网易严选商品跳转地址无效，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("网易严选商品重定向循环，可能需要安全验证")
		}
		current = next
	}
	return nil, errors.New("网易严选商品重定向次数过多")
}

func discoverYanxuanProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := yanxuanProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("网易严选商品地址或下载客户端无效")
	}
	page := *root
	pageGetter := getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("网易严选下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", "you.163.com"
		pageGetter = &yanxuanRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取网易严选商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || yanxuanProductID(final) != id {
		return result, errors.New("网易严选商品已跳转，可能需要登录或安全验证")
	}
	return yanxuanProductPage(ctx, doc, final)
}

func yanxuanImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil ||
		(!strings.EqualFold(u.Hostname(), "yanxuan-item.nosdn.127.net") && !strings.EqualFold(u.Hostname(), "yanxuan.nosdn.127.net")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path == "" || u.Path == "/" {
		return "", false
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String(), true
}

type yanxuanItem struct {
	ID         json.RawMessage `json:"id"`
	Name       string          `json:"name"`
	ItemDetail json.RawMessage `json:"itemDetail"`
	SKUSpecs   []struct {
		Values []struct {
			Image string `json:"picUrl"`
			Value string `json:"value"`
		} `json:"skuSpecValueList"`
	} `json:"skuSpecList"`
}

func yanxuanIDValue(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	if !yanxuanIDPattern.MatchString(text) || strings.Trim(text, "0") == "" {
		return ""
	}
	return text
}

func yanxuanProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := yanxuanProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("网易严选商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var item yanxuanItem
	stateSeen, stateValid := false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || stateValid || n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode || len(child.Data) > maxPageBytes {
				continue
			}
			match := yanxuanItemState.FindStringIndex(child.Data)
			if match == nil {
				continue
			}
			stateSeen = true
			var candidate yanxuanItem
			if err := json.NewDecoder(strings.NewReader(child.Data[match[1]:])).Decode(&candidate); err == nil {
				item, stateValid = candidate, true
				break
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !stateValid {
		if stateSeen {
			return result, errors.New("网易严选商品数据无法解析，图片可能不完整")
		}
		return result, errors.New("网易严选商品数据未返回，可能需要登录或安全验证")
	}
	if yanxuanIDValue(item.ID) != id {
		return result, errors.New("网易严选商品数据与请求的商品不匹配")
	}
	result.Title = item.Name
	seen := make(map[string]bool)
	invalid, limited := 0, false
	add := func(raw, kind, variant string) {
		if ctx.Err() != nil || limited || raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
			return
		}
		image, ok := yanxuanImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxYanxuanImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"yanxuan_item_id": id, "image_kind": kind}}
		if chars := []rune(variant); len(chars) > 256 {
			variant = string(chars[:256])
		}
		if variant != "" {
			item.Metadata["variant"] = variant
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	var detailHTML string
	detailSeen, detailValid := false, false
	decoder := json.NewDecoder(strings.NewReader(string(item.ItemDetail)))
	if token, err := decoder.Token(); err == nil && token == json.Delim('{') {
		detailValid = true
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				detailValid = false
				break
			}
			key, ok := token.(string)
			if !ok {
				detailValid = false
				break
			}
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				detailValid = false
				break
			}
			if strings.HasPrefix(key, "picUrl") {
				var image string
				if string(raw) != "null" && json.Unmarshal(raw, &image) != nil {
					invalid++
				} else {
					add(image, "main", "")
				}
			} else if key == "detailHtml" {
				detailSeen = true
				if json.Unmarshal(raw, &detailHTML) != nil {
					detailValid = false
				}
			}
		}
		if _, err := decoder.Token(); err != nil {
			detailValid = false
		}
	}
	mainCount := len(result.Items)
	for _, spec := range item.SKUSpecs {
		for _, value := range spec.Values {
			add(value.Image, "sku", value.Value)
		}
	}
	if detailValid && detailSeen && detailHTML != "" {
		fragment, err := html.Parse(strings.NewReader(detailHTML))
		if err != nil {
			detailValid = false
		} else {
			walkHTML(fragment, func(n *html.Node) {
				if n.Type == html.ElementNode && n.Data == "img" {
					add(attribute(n, "src"), "description", "")
				}
			})
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if mainCount == 0 {
		problems = append(problems, errors.New("网易严选商品主图未返回"))
	}
	if item.SKUSpecs == nil {
		problems = append(problems, errors.New("网易严选规格数据未返回，规格图片可能不完整"))
	}
	if !detailValid || !detailSeen {
		problems = append(problems, errors.New("网易严选商品详情未完整返回"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("网易严选商品有 %d 张图片地址无效或不受支持", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("网易严选商品图片超过 %d 张，结果不完整", maxYanxuanImages))
	}
	return result, errors.Join(problems...)
}
