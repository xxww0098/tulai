package download

import (
	"bytes"
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
	pinduoduoID        = regexp.MustCompile(`^[0-9]{1,20}$`)
	pinduoduoRawData   = regexp.MustCompile(`\bwindow\s*\.\s*rawData\s*=`)
	pinduoduoImageHost = regexp.MustCompile(`^(?:img|[a-z0-9-]+img)\.pddpic\.com$|^(?:t[0-9]+img|omsproductionimg)\.yangkeduo\.com$`)
)

// shortcut: bound large or malformed image lists; preserve collected results.
const maxPinduoduoImages = 10000

func pinduoduoProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "mobile.yangkeduo.com") || u.Path != "/goods.html" ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	values := u.Query()["goods_id"]
	if len(values) != 1 || !pinduoduoID.MatchString(values[0]) || strings.Trim(values[0], "0") == "" {
		return ""
	}
	return values[0]
}

func isPinduoduoProduct(u *url.URL) bool { return pinduoduoProductID(u) != "" }

func discoverPinduoduoProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := pinduoduoProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || id == "" {
		return result, errors.New("拼多多商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取拼多多商品页失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(root, final) || pinduoduoProductID(final) != id {
		return result, errors.New("拼多多商品页已跳转，可能需要登录或安全验证")
	}
	return pinduoduoCollect(ctx, getter, final, doc)
}

func pinduoduoImageURL(page *url.URL, raw string) (string, bool) {
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !pinduoduoImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		!strings.HasPrefix(u.Path, "/") || strings.Contains(u.Path, "..") {
		return "", false
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String(), true
}

type pinduoduoGalleryItem struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type pinduoduoSKU struct {
	ThumbURL string          `json:"thumbUrl"`
	GoodsID  json.RawMessage `json:"goodsId"`
	Specs    []struct {
		Key   string `json:"spec_key"`
		Value string `json:"spec_value"`
	} `json:"specs"`
}

func pinduoduoRawID(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return "!invalid"
}

// pinduoduoCollect reads only the literal window.rawData for the requested
// product. It does not execute scripts or make authenticated API calls.
func pinduoduoCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := pinduoduoProductID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || getter == nil || doc == nil {
		return result, errors.New("拼多多商品页面或下载客户端无效")
	}
	var init map[string]json.RawMessage
	found := false
	walkHTML(doc, func(n *html.Node) {
		if found || ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			for _, at := range offer1688CodeMatches(child.Data, pinduoduoRawData) {
				tail := strings.TrimSpace(child.Data[at[1]:])
				decoder := json.NewDecoder(strings.NewReader(tail))
				var state struct {
					Store struct {
						InitDataObj map[string]json.RawMessage `json:"initDataObj"`
					} `json:"store"`
				}
				if decoder.Decode(&state) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) || state.Store.InitDataObj == nil {
					continue
				}
				init, found = state.Store.InitDataObj, true
				return
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !found {
		return result, errors.New("拼多多商品数据不可用，可能需要登录或安全验证")
	}
	var needLogin bool
	_ = json.Unmarshal(init["needLogin"], &needLogin)
	var goods map[string]json.RawMessage
	if raw := init["goods"]; len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if json.Unmarshal(raw, &goods) != nil || goods == nil {
			return result, errors.New("拼多多商品数据格式无效")
		}
	}
	containers := []map[string]json.RawMessage{init}
	if goods != nil {
		containers = append(containers, goods)
	}
	for _, container := range containers {
		for _, key := range []string{"goodsId", "goodsID", "goods_id"} {
			if candidate := pinduoduoRawID(container[key]); candidate != "" && candidate != id {
				return result, errors.New("拼多多页面商品 ID 与请求不匹配")
			}
		}
	}
	for _, container := range containers {
		for _, key := range []string{"goodsName", "goods_name"} {
			var name string
			if json.Unmarshal(container[key], &name) == nil && strings.TrimSpace(name) != "" {
				result.Title = strings.TrimSpace(name)
				break
			}
		}
		if result.Title != "" {
			break
		}
	}
	var problems []error
	seen := map[string]bool{}
	mainCount, detailCount, invalid := 0, 0, 0
	limited, haveSKUs := false, false
	add := func(raw, kind, specs string) {
		if ctx.Err() != nil || strings.TrimSpace(raw) == "" {
			return
		}
		image, ok := pinduoduoImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		if kind == "main" {
			mainCount++
		}
		if kind == "detail" {
			detailCount++
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxPinduoduoImages {
			limited = true
			return
		}
		seen[image] = true
		metadata := map[string]string{"pinduoduo_goods_id": id, "image_kind": kind}
		if specs != "" {
			metadata["variant"] = specs
		}
		item := Item{URL: image, RequireImage: true, Metadata: metadata}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for _, container := range containers {
		if ctx.Err() != nil || limited {
			break
		}
		for _, field := range []string{"viewImageData", "banner", "gallery", "top_gallery"} {
			raw, ok := container[field]
			if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				continue
			}
			var entries []json.RawMessage
			if json.Unmarshal(raw, &entries) != nil {
				problems = append(problems, fmt.Errorf("拼多多主图字段 %s 格式无效", field))
				continue
			}
			for _, entry := range entries {
				if ctx.Err() != nil || limited {
					break
				}
				var image string
				if json.Unmarshal(entry, &image) != nil {
					var obj pinduoduoGalleryItem
					if json.Unmarshal(entry, &obj) == nil {
						image = obj.URL
					}
				}
				if image == "" {
					invalid++
					continue
				}
				add(image, "main", "")
			}
		}
		if raw, ok := container["skus"]; ok {
			haveSKUs = true
			var skus []pinduoduoSKU
			if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, &skus) != nil {
				problems = append(problems, errors.New("拼多多规格列表格式无效"))
			} else {
				for _, sku := range skus {
					if ctx.Err() != nil || limited {
						break
					}
					if candidate := pinduoduoRawID(sku.GoodsID); candidate != "" && candidate != id {
						invalid++
						continue
					}
					var specs []string
					for _, spec := range sku.Specs {
						if spec.Key != "" || spec.Value != "" {
							specs = append(specs, spec.Key+"["+spec.Value+"]")
						}
					}
					add(sku.ThumbURL, "sku", strings.Join(specs, "_"))
				}
			}
		}
		if raw, ok := container["detailGallery"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var detail []pinduoduoGalleryItem
			if json.Unmarshal(raw, &detail) != nil {
				problems = append(problems, errors.New("拼多多详情图片列表格式无效"))
			} else {
				for _, item := range detail {
					if ctx.Err() != nil || limited {
						break
					}
					if item.URL == "" {
						invalid++
						continue
					}
					add(item.URL, "detail", "")
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, errors.Join(append(problems, err)...)
	}
	if limited {
		problems = append(problems, fmt.Errorf("拼多多商品超过 %d 张图片，结果不完整", maxPinduoduoImages))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 条拼多多图片或规格信息无效，结果可能不完整", invalid))
	}
	if needLogin {
		problems = append(problems, errors.New("拼多多要求登录，商品图片可能不完整"))
	}
	if mainCount == 0 {
		problems = append(problems, errors.New("拼多多未返回商品主图，可能需要登录或验证"))
	}
	if detailCount == 0 {
		problems = append(problems, errors.New("拼多多未返回详情图，可能没有或需要登录"))
	}
	if !haveSKUs {
		problems = append(problems, errors.New("拼多多未返回规格列表，规格图片完整性未知"))
	}
	return result, errors.Join(problems...)
}
