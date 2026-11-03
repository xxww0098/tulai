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
	shopeeProductNumber = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	shopeeSlugID        = regexp.MustCompile(`-i\.([1-9][0-9]{0,19})\.([1-9][0-9]{0,19})$`)
	shopeeImageID       = regexp.MustCompile(`^[A-Za-z0-9_-]{8,256}$`)
	shopeeInitialState  = regexp.MustCompile(`(?:(?:window\.)?__STORE__|["']initialState["'])\s*[:=]\s*`)
)

type shopeeRef struct{ region, shop, item string }

// These CDN regions were observed in the respective Shopee home pages.
func shopeeRegion(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	p := u.Port()
	if p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch host {
	case "shopee.sg":
		return "sg"
	case "shopee.com.my":
		return "my"
	case "shopee.co.th":
		return "th"
	case "shopee.co.id":
		return "id"
	case "shopee.vn":
		return "vn"
	case "shopee.ph":
		return "ph"
	case "shopee.tw":
		return "tw"
	case "shopee.com.br":
		return "br"
	}
	return ""
}

func shopeePageRef(u *url.URL) shopeeRef {
	region := shopeeRegion(u)
	if region == "" {
		return shopeeRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var shop, item string
	switch {
	case len(parts) == 3 && parts[0] == "product":
		shop, item = parts[1], parts[2]
	case len(parts) == 4 && parts[0] == "shop" && parts[2] == "item":
		shop, item = parts[1], parts[3]
	case len(parts) == 1:
		if ids := shopeeSlugID.FindStringSubmatch(parts[0]); len(ids) == 3 {
			shop, item = ids[1], ids[2]
		}
	}
	if !shopeeProductNumber.MatchString(shop) || !shopeeProductNumber.MatchString(item) {
		return shopeeRef{}
	}
	return shopeeRef{region: region, shop: shop, item: item}
}

func isShopeeProduct(u *url.URL) bool { return shopeePageRef(u).item != "" }

func discoverShopeeProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := shopeePageRef(root)
	if getter == nil || want.item == "" {
		return result, errors.New("Shopee 商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || shopeePageRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Shopee 商品页面不可用或跳转到其他内容")
	}
	return shopeeCollect(ctx, getter, final, doc)
}

func shopeeMediaURL(ref shopeeRef, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if shopeeImageID.MatchString(raw) {
		return "https://down-" + ref.region + ".img.susercontent.com/file/" + raw, true
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.EqualFold(u.Hostname(), "down-"+ref.region+".img.susercontent.com") || (u.Port() != "" && u.Port() != "443") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") || !strings.HasPrefix(u.Path, "/file/") {
		return "", false
	}
	id := strings.TrimPrefix(u.Path, "/file/")
	if !shopeeImageID.MatchString(id) {
		return "", false
	}
	return u.String(), true
}

func shopeeString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return ""
}

func shopeeMatches(m map[string]any, ref shopeeRef) bool {
	shop := firstNonEmpty(shopeeString(m["shop_id"]), shopeeString(m["shopid"]))
	item := firstNonEmpty(shopeeString(m["item_id"]), shopeeString(m["itemid"]))
	return shop == ref.shop && item == ref.item
}

// shopeeData inspects only objects bound to both requested IDs. It does not
// recurse into arbitrary image fields such as recommendations or shop logos.
func shopeeData(data any, ref shopeeRef) ([]string, string, bool) {
	type entry struct {
		value any
		depth int
	}
	stack := []entry{{data, 0}}
	var images []string
	var title string
	matched := false
	add := func(value any) {
		if s, ok := value.(string); ok && s != "" {
			images = append(images, s)
		}
	}
	addList := func(value any) {
		if list, ok := value.([]any); ok {
			for _, x := range list {
				add(x)
			}
		}
	}
	fields := func(m map[string]any) {
		title = firstNonEmpty(title, shopeeString(m["name"]), shopeeString(m["title"]))
		addList(m["images"])
		if tiers, ok := m["tier_variations"].([]any); ok {
			for _, v := range tiers {
				if tier, ok := v.(map[string]any); ok {
					addList(tier["images"])
				}
			}
		}
		if pi, ok := m["product_images"].(map[string]any); ok {
			add(pi["cover_image"])
			addList(pi["images"])
			addList(pi["long_images"])
			if variants, ok := pi["first_tier_variations"].([]any); ok {
				for _, v := range variants {
					if variant, ok := v.(map[string]any); ok {
						add(variant["image"])
						add(variant["gallery_image"])
					}
				}
			}
			if promotions, ok := pi["promotion_images"].([]any); ok {
				for _, v := range promotions {
					if promotion, ok := v.(map[string]any); ok {
						add(promotion["image"])
					}
				}
			}
			if gallery, ok := pi["gallery_contents"].([]any); ok {
				for _, v := range gallery {
					if row, ok := v.(map[string]any); ok {
						if image, ok := row["image"].(map[string]any); ok {
							add(image["url"])
						}
					}
				}
			}
		}
	}
	for visited := 0; len(stack) > 0 && visited < 100000; visited++ {
		last := len(stack) - 1
		e := stack[last]
		stack = stack[:last]
		if e.depth > 100 {
			continue
		}
		switch m := e.value.(type) {
		case map[string]any:
			if shopeeMatches(m, ref) {
				matched = true
				fields(m)
			}
			if item, ok := m["item"].(map[string]any); ok && shopeeMatches(item, ref) {
				matched = true
				fields(m)
				fields(item)
			}
			for _, v := range m {
				switch v.(type) {
				case map[string]any, []any:
					stack = append(stack, entry{v, e.depth + 1})
				}
			}
		case []any:
			for _, v := range m {
				switch v.(type) {
				case map[string]any, []any:
					stack = append(stack, entry{v, e.depth + 1})
				}
			}
		}
	}
	return images, title, matched
}

func shopeeDecodeJSON(raw string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// shopeeCollect is shared by the regular crawler and the user's rendered page.
func shopeeCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := shopeePageRef(page)
	if getter == nil || doc == nil || ref.item == "" {
		return result, errors.New("Shopee 商品页面无效")
	}
	seen := map[string]bool{}
	add := func(raw string) {
		if image, ok := shopeeMediaURL(ref, raw); ok && !seen[image] {
			seen[image] = true
			item := Item{URL: image}
			if r := imageReferer(page.String(), image); r != "" {
				item.Headers = map[string]string{"Referer": r}
			}
			result.Items = append(result.Items, item)
		}
	}
	var canonical, cover string
	var sourceMatched bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" {
			switch strings.ToLower(firstNonEmpty(attribute(n, "property"), attribute(n, "name"))) {
			case "og:url":
				canonical = firstNonEmpty(canonical, attribute(n, "content"))
			case "og:image":
				cover = firstNonEmpty(cover, attribute(n, "content"))
			case "og:title":
				result.Title = firstNonEmpty(attribute(n, "content"), result.Title)
			}
		}
		if n.Data != "script" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode {
				continue
			}
			text := c.Data
			if strings.EqualFold(attribute(n, "type"), "application/json") {
				if value, err := shopeeDecodeJSON(text); err == nil {
					imgs, title, ok := shopeeData(value, ref)
					if ok {
						sourceMatched = true
						result.Title = firstNonEmpty(title, result.Title)
						for _, img := range imgs {
							add(img)
						}
					}
				}
			}
			for _, loc := range offer1688CodeMatches(text, shopeeInitialState) {
				if value, err := shopeeDecodeJSON(text[loc[1]:]); err == nil {
					imgs, title, ok := shopeeData(value, ref)
					if ok {
						sourceMatched = true
						result.Title = firstNonEmpty(title, result.Title)
						for _, img := range imgs {
							add(img)
						}
					}
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if sourceMatched && len(result.Items) > 0 {
		return result, nil
	}
	if cu, err := crawlURL(page, canonical); err == nil && shopeePageRef(cu) == ref {
		add(cover)
	}
	data, err := shopeeFetchProduct(ctx, getter, page, ref)
	if err != nil {
		return result, err
	}
	images, title, matched := shopeeData(data, ref)
	if !matched {
		return result, errors.New("Shopee 接口商品身份与请求不符，结果可能不完整")
	}
	result.Title = firstNonEmpty(title, result.Title)
	for _, image := range images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		add(image)
	}
	if len(result.Items) == 0 {
		return result, errors.New("Shopee 未返回可信商品图片，可能需要登录或验证")
	}
	return result, nil
}

func shopeeFetchProduct(ctx context.Context, getter Getter, page *url.URL, ref shopeeRef) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	api := *page
	api.User = nil
	api.Path = "/api/v4/pdp/get_pc"
	api.RawPath = ""
	api.Fragment = ""
	api.ForceQuery = false
	api.RawQuery = url.Values{"shop_id": {ref.shop}, "item_id": {ref.item}}.Encode()
	endpoint := api.String()
	if original, ok := getter.(*HTTPGetter); ok {
		clone := *original
		client := *original.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		clone.client = &client
		getter = &clone
	}
	resp, err := getter.Get(ctx, endpoint, func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Shopee 商品接口不可用，结果可能不完整")
	}
	if resp == nil || resp.Body == nil {
		return nil, errors.New("Shopee 商品接口未返回数据")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.FinalURL != endpoint {
		return nil, fmt.Errorf("Shopee 商品接口不可用（HTTP %d），可能需要登录或安全验证", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPageBytes {
		return nil, errors.New("Shopee 商品接口响应超过大小上限")
	}
	data, err := shopeeDecodeJSON(string(body))
	if err != nil {
		return nil, errors.New("Shopee 商品接口返回格式无效")
	}
	return data, nil
}
