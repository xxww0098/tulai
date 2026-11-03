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
	youzanAliasPattern = regexp.MustCompile(`^[A-Za-z0-9]{6,40}$`)
	youzanMobileHost   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}\.m\.youzan\.com$`)
	youzanCashierHost  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}\.youzan\.com$`)
	youzanImageHost    = regexp.MustCompile(`^img(?:[0-9]{1,2})?\.yzcdn\.cn$`)
	youzanImageSuffix  = regexp.MustCompile(`!(?:middle|large|[0-9]{1,5}x[0-9]{1,5})(?:\.[A-Za-z0-9]{2,5})?$`)
	youzanGlobal       = regexp.MustCompile(`\bwindow\s*\.\s*_global\s*=\s*`)
)

// shortcut: bound anomalously large product pages until the limit is configurable.
const maxYouzanImages = 10000

func youzanProductID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return ""
	}
	host, path := strings.ToLower(u.Hostname()), u.Path
	var alias string
	switch {
	case host == "detail.youzan.com" && path == "/show/goods",
		host == "h5.youzan.com" && path == "/v2/showcase/goods",
		youzanCashierHost.MatchString(host) && path == "/pay/wscgoods_order":
		values := u.Query()["alias"]
		if len(values) == 1 {
			alias = values[0]
		}
	case host == "h5.youzan.com" && strings.HasPrefix(path, "/v2/goods/"):
		alias = strings.TrimPrefix(path, "/v2/goods/")
	case (host == "h5.youzan.com" || youzanMobileHost.MatchString(host)) && strings.HasPrefix(path, "/wscgoods/detail/"):
		alias = strings.TrimPrefix(path, "/wscgoods/detail/")
	}
	if !youzanAliasPattern.MatchString(alias) {
		return ""
	}
	return alias
}

func isYouzanProduct(u *url.URL) bool { return youzanProductID(u) != "" }

func discoverYouzanProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	alias := youzanProductID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || alias == "" {
		return result, errors.New("有赞商品地址或下载客户端无效")
	}
	page := *root
	if _, ok := getter.(*HTTPGetter); ok {
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
	}
	pageGetter := getter
	if anonymous := youzanPublicDetailGetter(getter); anonymous != nil {
		if _, ok := getter.(*HTTPGetter); ok {
			pageGetter = &youzanRedirectGetter{base: anonymous, alias: alias}
		}
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取有赞商品失败: %w", err)
	}
	if doc == nil || final == nil || final.Scheme != "https" || youzanProductID(final) != alias {
		return result, errors.New("有赞商品已跳转，可能需要登录或安全验证")
	}
	if !sameOrigin(&page, final) {
		if _, ok := getter.(*HTTPGetter); !ok {
			return result, errors.New("有赞商品跨源跳转，请在下载浏览器中打开商品后重试")
		}
	}
	return youzanCollect(ctx, getter, doc, final)
}

type youzanSKUProp struct {
	Values []struct {
		Name   string `json:"name"`
		ImgURL string `json:"imgUrl"`
	} `json:"v"`
}

type youzanState struct {
	Alias     string `json:"alias"`
	GoodsData struct {
		Goods struct {
			Alias    string `json:"alias"`
			Title    string `json:"title"`
			Pictures []struct {
				URL string `json:"url"`
			} `json:"pictures"`
		} `json:"goods"`
		SKUInfo *struct {
			Props []youzanSKUProp `json:"props"`
		} `json:"skuInfo"`
	} `json:"goodsData"`
}

func youzanReadState(ctx context.Context, doc *html.Node) (youzanState, bool, bool) {
	var state youzanState
	seen, valid := false, false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || valid || n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode || len(c.Data) > maxPageBytes {
				continue
			}
			match := youzanGlobal.FindStringIndex(c.Data)
			if match == nil {
				continue
			}
			seen = true
			var candidate youzanState
			if err := json.NewDecoder(strings.NewReader(c.Data[match[1]:])).Decode(&candidate); err == nil {
				state = candidate
				valid = true
				break
			}
		}
	})
	return state, seen, valid
}

func youzanStateMatches(state youzanState, alias string) bool {
	return (state.Alias == alias || state.GoodsData.Goods.Alias == alias) &&
		(state.Alias == "" || state.Alias == alias) &&
		(state.GoodsData.Goods.Alias == "" || state.GoodsData.Goods.Alias == alias)
}

func youzanHasSKUState(state youzanState) bool {
	if state.GoodsData.SKUInfo == nil || state.GoodsData.SKUInfo.Props == nil {
		return false
	}
	for _, prop := range state.GoodsData.SKUInfo.Props {
		if prop.Values == nil {
			return false
		}
	}
	return true
}

func youzanHasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func youzanImageURL(page *url.URL, raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !youzanImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) ||
		u.Path == "" || u.Path == "/" {
		return "", false
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String(), true
}

func youzanFullImage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery != "" {
		return raw
	}
	path := youzanImageSuffix.ReplaceAllString(u.Path, "")
	if path != u.Path {
		u.Path, u.RawPath = path, ""
	}
	return u.String()
}

// The public desktop detail page is fetched anonymously and never follows redirects.
func youzanPublicDetailGetter(getter Getter) Getter {
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
	// Do not send cookies from an HTTP client's jar to the public detail endpoint.
	client.Jar = nil
	clone.client = &client
	return &clone
}

// Follow only HTTPS redirects to another recognized URL for the same product.
type youzanRedirectGetter struct {
	base  Getter
	alias string
}

func (g *youzanRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || youzanProductID(current) != g.alias {
		return nil, errors.New("有赞商品请求地址无效")
	}
	for i := 0; i < 6; i++ {
		response, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || final.Path != current.Path || youzanProductID(final) != g.alias {
			response.Body.Close()
			return nil, errors.New("有赞商品响应地址无效")
		}
		if response.StatusCode != 301 && response.StatusCode != 302 && response.StatusCode != 303 && response.StatusCode != 307 && response.StatusCode != 308 {
			return response, nil
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || next.Scheme != "https" || youzanProductID(next) != g.alias {
			return nil, errors.New("有赞商品跳转地址无效或商品不匹配")
		}
		current = next
	}
	return nil, errors.New("有赞商品重定向次数过多")
}

func youzanProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	return youzanCollect(ctx, nil, doc, page)
}

func youzanCollect(ctx context.Context, getter Getter, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	alias := youzanProductID(page)
	if doc == nil || alias == "" {
		return result, errors.New("有赞商品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	seen := make(map[string]bool)
	invalid, limited, mainFound := 0, false, false
	add := func(raw, kind, variant string) {
		if ctx.Err() != nil || limited || raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
			return
		}
		original, ok := youzanImageURL(page, raw)
		if !ok {
			invalid++
			return
		}
		full := youzanFullImage(original)
		if kind == "main" {
			mainFound = true
		}
		key := full
		if u, err := url.Parse(full); err == nil && u.RawQuery == "" && strings.HasPrefix(u.Path, "/upload_files/") {
			u.Host = "img.yzcdn.cn"
			key = u.String()
		}
		if seen[key] {
			return
		}
		if len(result.Items) >= maxYouzanImages {
			limited = true
			return
		}
		seen[key] = true
		item := Item{URL: full, RequireImage: true, Metadata: map[string]string{"youzan_alias": alias, "image_kind": kind}}
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
	state, stateSeen, stateValid := youzanReadState(ctx, doc)
	var title, ogURL string
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && title == "" {
			title = nodeText(n)
		}
		if n.Data == "h1" && youzanHasClass(n, "goods-title") {
			title = nodeText(n)
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			ogURL = attribute(n, "content")
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ogURL != "" {
		u, err := crawlURL(page, ogURL)
		if err != nil || youzanProductID(u) != alias {
			return result, errors.New("有赞页面与请求的商品不匹配")
		}
	}
	if stateValid && !youzanStateMatches(state, alias) {
		return result, errors.New("有赞商品数据与请求的商品不匹配")
	}
	if state.GoodsData.Goods.Title != "" {
		title = state.GoodsData.Goods.Title
	}
	result.Title = title
	for _, picture := range state.GoodsData.Goods.Pictures {
		add(picture.URL, "main", "")
	}
	addSKU := func(value youzanState) {
		if value.GoodsData.SKUInfo != nil {
			for _, prop := range value.GoodsData.SKUInfo.Props {
				for _, variant := range prop.Values {
					add(variant.ImgURL, "sku", variant.Name)
				}
			}
		}
	}
	addSKU(state)
	var desktopGallery, desktopDetails []*html.Node
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if youzanHasClass(n, "swiper-pagination-list") {
			desktopGallery = append(desktopGallery, n)
		}
		if youzanHasClass(n, "goods-detail") && youzanHasClass(n, "js-goods-detail") {
			desktopDetails = append(desktopDetails, n)
		}
		if n.Data == "img" && youzanHasClass(n, "tee-image") && youzanHasClass(n, "image-swipe-item") {
			add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), "main", "")
		}
		if n.Data == "img" && (youzanHasClass(n, "js-richtext-lazy-img") || youzanHasClass(n, "cap-image-ad__image") ||
			(n.Parent != nil && youzanHasClass(n.Parent, "lazy-component__image"))) {
			add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), "description", "")
		}
	})
	for _, gallery := range desktopGallery {
		walkHTML(gallery, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" {
				add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), "main", "")
			}
		})
		break
	}
	for _, description := range desktopDetails {
		walkHTML(description, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" {
				add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")), "description", "")
			}
		})
	}
	var problems []error
	if stateSeen && !stateValid {
		problems = append(problems, errors.New("有赞商品状态无法解析，图片可能不完整"))
	}
	if strings.EqualFold(page.Hostname(), "detail.youzan.com") && !youzanHasSKUState(state) {
		anon := youzanPublicDetailGetter(getter)
		if anon == nil {
			problems = append(problems, errors.New("无法读取有赞规格数据，规格图片可能不完整"))
		} else {
			target := &url.URL{Scheme: "https", Host: "h5.youzan.com", Path: "/v2/goods/" + alias}
			mobile, final, _, response, err := fetchCrawlPage(ctx, &youzanRedirectGetter{base: anon, alias: alias}, crawlPage{url: target.String(), depth: 1})
			if response != nil {
				response.Body.Close()
			}
			if err == nil && (mobile == nil || final == nil || final.Scheme != "https" || youzanProductID(final) != alias) {
				err = errors.New("公开移动商品地址不匹配")
			}
			if err == nil {
				mobileState, _, valid := youzanReadState(ctx, mobile)
				if !valid || !youzanStateMatches(mobileState, alias) || !youzanHasSKUState(mobileState) {
					err = errors.New("公开移动商品未返回匹配的规格数据")
				} else {
					addSKU(mobileState)
				}
			}
			if err != nil {
				problems = append(problems, fmt.Errorf("读取有赞规格数据失败，规格图片可能不完整: %w", err))
			}
		}
	} else if !strings.EqualFold(page.Hostname(), "detail.youzan.com") {
		if !stateValid || !youzanHasSKUState(state) {
			problems = append(problems, errors.New("有赞规格数据未返回，规格图片可能不完整"))
		}
		anon := youzanPublicDetailGetter(getter)
		if anon == nil {
			problems = append(problems, errors.New("无法读取有赞公开详情，描述图片可能不完整"))
		} else {
			target := &url.URL{Scheme: "https", Host: "detail.youzan.com", Path: "/show/goods", RawQuery: url.Values{"alias": {alias}}.Encode()}
			secondary, final, _, response, err := fetchCrawlPage(ctx, anon, crawlPage{url: target.String(), depth: 1})
			if response != nil {
				response.Body.Close()
			}
			if err == nil && (final == nil || !sameOrigin(target, final) || youzanProductID(final) != alias) {
				err = errors.New("公开详情的商品地址不匹配")
			}
			if err == nil && secondary != nil {
				var found bool
				var secondaryOG string
				walkHTML(secondary, func(n *html.Node) {
					if n.Type != html.ElementNode {
						return
					}
					if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
						secondaryOG = attribute(n, "content")
					}
				})
				if secondaryOG != "" {
					u, parseErr := crawlURL(target, secondaryOG)
					if parseErr != nil || youzanProductID(u) != alias {
						err = errors.New("公开详情的商品内容不匹配")
					}
				}
				if err == nil {
					walkHTML(secondary, func(n *html.Node) {
						if n.Type == html.ElementNode && youzanHasClass(n, "goods-detail") && youzanHasClass(n, "js-goods-detail") {
							found = true
							walkHTML(n, func(img *html.Node) {
								if img.Type == html.ElementNode && img.Data == "img" {
									add(firstNonEmpty(attribute(img, "data-src"), attribute(img, "src")), "description", "")
								}
							})
						}
					})
					if !found {
						err = errors.New("公开详情未返回商品描述")
					}
				}
			} else if err == nil {
				err = errors.New("公开详情未返回商品页面")
			}
			if err != nil {
				problems = append(problems, fmt.Errorf("读取有赞公开详情失败，描述图片可能不完整: %w", err))
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !mainFound {
		problems = append(problems, errors.New("有赞商品未返回主图，可能需要登录或安全验证"))
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("有赞商品有 %d 张图片地址无效或不受支持", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("有赞商品图片超过 %d 张，结果不完整", maxYouzanImages))
	}
	return result, errors.Join(problems...)
}
