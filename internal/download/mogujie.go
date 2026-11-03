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
	mogujieIDPattern   = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
	mogujieCDNPattern  = regexp.MustCompile(`^s[0-9]{1,2}\.mogucdn\.com$`)
	mogujieDetailState = regexp.MustCompile(`\bvar\s+detailInfo\s*=\s*\{`)
	mogujieStateID     = regexp.MustCompile(`(?m)^\s*itemid\s*:\s*`)
	mogujieStateMain   = regexp.MustCompile(`(?m)^\s*main\s*:\s*`)
	mogujieStateSKU    = regexp.MustCompile(`(?m)^\s*attribute\s*:\s*`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxMogujieImages = 10000

func mogujieProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "shop.mogu.com" && host != "shop.mogujie.com" {
		return ""
	}
	id := strings.TrimPrefix(u.Path, "/detail/")
	if id == u.Path || !mogujieIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func isMogujieProduct(u *url.URL) bool { return mogujieProductID(u) != "" }

type mogujieRedirectGetter struct {
	base Getter
	id   string
}

func (g *mogujieRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || mogujieProductID(current) != g.id {
		return nil, errors.New("蘑菇街商品请求地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || mogujieProductID(final) != g.id {
			response.Body.Close()
			return nil, errors.New("蘑菇街商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || !sameOrigin(current, next) || mogujieProductID(next) != g.id {
			return nil, errors.New("蘑菇街商品已跳转，可能已下架或需要安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("蘑菇街商品重定向循环")
		}
		current = next
	}
	return nil, errors.New("蘑菇街商品重定向次数过多")
}

func discoverMogujieProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	id := mogujieProductID(root)
	if getter == nil || id == "" {
		return result, errors.New("蘑菇街商品地址或下载客户端无效")
	}
	page, pageGetter := *root, getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("蘑菇街下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter = &mogujieRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取蘑菇街商品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || mogujieProductID(final) != id {
		return result, errors.New("蘑菇街商品已跳转，可能已下架或需要安全验证")
	}
	return mogujieProductPage(ctx, doc, final)
}

func mogujieImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !mogujieCDNPattern.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) || u.Path == "" || u.Path == "/" {
		return "", false
	}
	// The site's .jpg_ thumbnail suffix follows the original JPEG path.
	if u.RawQuery == "" {
		if i := strings.Index(u.Path, ".jpg_"); i >= 0 {
			u.Path, u.RawPath = u.Path[:i+4], ""
		}
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String(), true
}

func mogujieHasClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

type mogujieImage struct{ raw, kind, variant string }

type mogujieSKU struct {
	ItemID   string `json:"itemId"`
	Image    string `json:"image"`
	Original string `json:"imageOrg"`
	Style    string `json:"style"`
}

func mogujieProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := mogujieProductID(page)
	if doc == nil || id == "" {
		return result, errors.New("蘑菇街商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var gallery, sku, detail []mogujieImage
	var big string
	var stateFound, stateInvalid, skuKnown, challenge bool
	var skuInvalid int
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" {
			result.Title = strings.TrimSpace(nodeText(n))
			if strings.Contains(result.Title, "安全验证") || strings.Contains(result.Title, "访问验证") {
				challenge = true
			}
		}
		if n.Data == "script" && !stateFound {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.TextNode || len(child.Data) > maxPageBytes {
					continue
				}
				match := mogujieDetailState.FindStringIndex(child.Data)
				if match == nil {
					continue
				}
				stateFound = true
				state := child.Data[match[1]:]
				var stateID string
				idMatch := mogujieStateID.FindStringIndex(state)
				if idMatch == nil || json.NewDecoder(strings.NewReader(state[idMatch[1]:])).Decode(&stateID) != nil {
					stateInvalid = true
					break
				}
				if stateID != id {
					stateInvalid = true
					skuInvalid = -1
					break
				}
				if mainMatch := mogujieStateMain.FindStringIndex(state); mainMatch != nil {
					var main struct {
						TopImages []string `json:"topImages"`
					}
					if json.NewDecoder(strings.NewReader(state[mainMatch[1]:])).Decode(&main) != nil {
						stateInvalid = true
					} else {
						for _, raw := range main.TopImages {
							gallery = append(gallery, mogujieImage{raw: raw, kind: "main"})
						}
					}
				}
				if skuMatch := mogujieStateSKU.FindStringIndex(state); skuMatch != nil {
					var entries []mogujieSKU
					if json.NewDecoder(strings.NewReader(state[skuMatch[1]:])).Decode(&entries) != nil {
						stateInvalid = true
					} else {
						skuKnown = true
						for _, entry := range entries {
							if entry.ItemID != "" && entry.ItemID != id {
								skuInvalid++
								continue
							}
							sku = append(sku, mogujieImage{raw: firstNonEmpty(entry.Original, entry.Image), kind: "variant", variant: entry.Style})
						}
					}
				}
				break
			}
		}
		if n.Data != "img" {
			return
		}
		if attribute(n, "id") == "J_BigImg" {
			big = firstNonEmpty(attribute(n, "data-main"), attribute(n, "src"))
		}
		var small, skuSection, graphic, oldDetail, imageBox bool
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Type != html.ElementNode {
				continue
			}
			switch attribute(p, "id") {
			case "J_SmallImgs":
				small = true
			case "J_GoodsSku":
				skuSection = true
			case "J_ModuleGraphic":
				graphic = true
			case "J_ModuleDetail":
				oldDetail = true
			}
			if mogujieHasClass(p, "img-box") {
				imageBox = true
			}
		}
		switch {
		case small:
			gallery = append(gallery, mogujieImage{raw: attribute(n, "src"), kind: "main"})
		case skuSection:
			skuKnown = true
			sku = append(sku, mogujieImage{raw: attribute(n, "src"), kind: "variant", variant: attribute(n.Parent, "title")})
		case graphic:
			detail = append(detail, mogujieImage{raw: firstNonEmpty(attribute(n, "data-original"), attribute(n, "src")), kind: "description"})
		case oldDetail && imageBox && attribute(n, "d-src") != "":
			detail = append(detail, mogujieImage{raw: attribute(n, "d-src"), kind: "description"})
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if challenge {
		return result, errors.New("蘑菇街需要完成安全验证")
	}
	if skuInvalid == -1 {
		return result, errors.New("蘑菇街页面与请求的商品不匹配")
	}
	seen := make(map[string]bool)
	var invalid, limited, signedThumbnail, detailFound bool
	add := func(image mogujieImage) {
		if ctx.Err() != nil || limited || image.raw == "" || strings.HasPrefix(strings.ToLower(image.raw), "data:") {
			return
		}
		parsed, _ := url.Parse(image.raw)
		if parsed != nil && parsed.RawQuery != "" && strings.Contains(parsed.Path, ".jpg_") {
			signedThumbnail = true
		}
		resolved, ok := mogujieImageURL(page, image.raw)
		if !ok {
			invalid = true
			return
		}
		if image.kind == "description" {
			detailFound = true
		}
		if seen[resolved] {
			return
		}
		if len(result.Items) >= maxMogujieImages {
			limited = true
			return
		}
		seen[resolved] = true
		item := Item{URL: resolved, RequireImage: true, Metadata: map[string]string{"mogujie_item_id": id, "image_kind": image.kind}}
		if len([]rune(image.variant)) > 256 {
			image.variant = string([]rune(image.variant)[:256])
		}
		if image.variant != "" {
			item.Metadata["variant"] = image.variant
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", resolved); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	for _, value := range gallery {
		add(value)
	}
	if len(result.Items) == 0 && big != "" {
		add(mogujieImage{raw: big, kind: "main"})
	}
	mainCount := len(result.Items)
	for _, value := range sku {
		add(value)
	}
	for _, value := range detail {
		add(value)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if stateInvalid {
		problems = append(problems, errors.New("蘑菇街商品数据无法完整解析"))
	}
	if skuInvalid > 0 {
		problems = append(problems, errors.New("蘑菇街规格数据与商品不匹配"))
	}
	if invalid {
		problems = append(problems, errors.New("蘑菇街包含未验证来源的图片，已跳过"))
	}
	if mainCount == 0 {
		problems = append(problems, errors.New("蘑菇街主图未返回，可能需要在浏览器打开商品"))
	}
	if !skuKnown {
		problems = append(problems, errors.New("蘑菇街规格图未完整加载，结果可能不完整"))
	}
	if !detailFound {
		problems = append(problems, errors.New("蘑菇街详情图未返回，结果可能不完整"))
	}
	if signedThumbnail {
		problems = append(problems, errors.New("蘑菇街带参数的缩略图保留原地址，原图未验证"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("蘑菇街商品超过 %d 张图片，结果不完整", maxMogujieImages))
	}
	return result, errors.Join(problems...)
}
