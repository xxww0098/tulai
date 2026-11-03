package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

var (
	lazadaProductID = regexp.MustCompile(`-i([1-9][0-9]{0,19})(?:-s[1-9][0-9]{0,19})?\.html$`)
	lazadaID        = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	lazadaState     = regexp.MustCompile(`\b(?:var|let|const)\s+__moduleData__\s*=\s*`)
	lazadaHash      = regexp.MustCompile(`^([a-fA-F0-9]{32})\.(?:jpe?g|png|gif|webp)$`)
)

type lazadaRef struct{ region, id string }
type lazadaGalleryImage struct {
	Type string `json:"type"`
	Src  string `json:"src"`
}
type lazadaModule struct {
	Data struct {
		Root struct {
			Fields struct {
				PrimaryKey struct {
					ItemID json.RawMessage `json:"itemId"`
				} `json:"primaryKey"`
				Product struct {
					Title   string `json:"title"`
					Desc    string `json:"desc"`
					PageURL string `json:"pageUrl"`
				} `json:"product"`
				PageURL       string                          `json:"pageUrl"`
				SkuGalleries  map[string][]lazadaGalleryImage `json:"skuGalleries"`
				ProductOption struct {
					Options []struct {
						Image string `json:"image"`
					} `json:"options"`
					SkuBase struct {
						Properties []struct {
							Values []struct {
								Image      string `json:"image"`
								HoverImage string `json:"hoverImage"`
							} `json:"values"`
						} `json:"properties"`
						Skus []struct {
							ItemID json.RawMessage `json:"itemId"`
							SkuID  json.RawMessage `json:"skuId"`
						} `json:"skus"`
					} `json:"skuBase"`
				} `json:"productOption"`
				SkuInfos map[string]struct {
					ItemID json.RawMessage `json:"itemId"`
					Image  string          `json:"image"`
				} `json:"skuInfos"`
			} `json:"fields"`
		} `json:"root"`
	} `json:"data"`
}

func lazadaRegion(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	port := u.Port()
	if port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch host {
	case "lazada.sg":
		return "sg"
	case "lazada.com.my":
		return "my"
	case "lazada.co.th":
		return "th"
	case "lazada.co.id":
		return "id"
	case "lazada.vn":
		return "vn"
	case "lazada.com.ph":
		return "ph"
	}
	return ""
}

func lazadaPageRef(u *url.URL) lazadaRef {
	region := lazadaRegion(u)
	if region == "" {
		return lazadaRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "products" {
		return lazadaRef{}
	}
	match := lazadaProductID.FindStringSubmatch(parts[1])
	if len(match) != 2 {
		return lazadaRef{}
	}
	return lazadaRef{region: region, id: match[1]}
}

func isLazadaProduct(u *url.URL) bool { return lazadaPageRef(u).id != "" }

func discoverLazadaProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := lazadaPageRef(root)
	if getter == nil || want.id == "" {
		return result, errors.New("Lazada 商品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || lazadaPageRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Lazada 商品页面不可用或跳转到其他商品")
	}
	return lazadaProductPage(ctx, doc, final)
}

func lazadaRawID(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil && lazadaID.MatchString(text) {
		return text
	}
	text = string(raw)
	if lazadaID.MatchString(text) {
		return text
	}
	return ""
}

func lazadaImageURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", "", false
	}
	switch strings.ToLower(u.Hostname()) {
	case "laz-img-sg.alicdn.com", "laz-img-cdn.alicdn.com", "img.lazcdn.com", "lzd-img-global.slatic.net",
		"sg-live-01.slatic.net", "sg-test-11.slatic.net", "my-live-01.slatic.net", "my-test-11.slatic.net",
		"th-live-01.slatic.net", "th-live.slatic.net", "th-test-11.slatic.net", "id-live-01.slatic.net", "id-live.slatic.net", "id-test-11.slatic.net",
		"vn-live-01.slatic.net", "vn-test-11.slatic.net", "ph-live-01.slatic.net", "ph-test-11.slatic.net":
	default:
		return "", "", false
	}
	if !(strings.HasPrefix(u.Path, "/p/") || strings.HasPrefix(u.Path, "/g/p/") || strings.HasPrefix(u.Path, "/original/")) {
		return "", "", false
	}
	u.Fragment = ""
	key := u.String()
	if m := lazadaHash.FindStringSubmatch(path.Base(u.Path)); len(m) == 2 {
		key = "hash:" + strings.ToLower(m[1])
	}
	return u.String(), key, true
}

// lazadaProductPage only reads the requested product's server-rendered module;
// scripts are decoded as JSON and never executed.
func lazadaProductPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := lazadaPageRef(page)
	if doc == nil || ref.id == "" {
		return result, errors.New("Lazada 商品页面无效")
	}
	var module *lazadaModule
	var structured []struct {
		Type  string          `json:"@type"`
		URL   string          `json:"url"`
		Name  string          `json:"name"`
		Image json.RawMessage `json:"image"`
	}
	var canonical, cover string
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
			if strings.EqualFold(attribute(n, "type"), "application/ld+json") {
				var product struct {
					Type  string          `json:"@type"`
					URL   string          `json:"url"`
					Name  string          `json:"name"`
					Image json.RawMessage `json:"image"`
				}
				if json.Unmarshal([]byte(text), &product) == nil && strings.EqualFold(product.Type, "Product") {
					structured = append(structured, product)
				}
			}
			if module == nil {
				for _, loc := range offer1688CodeMatches(text, lazadaState) {
					var candidate lazadaModule
					if json.NewDecoder(strings.NewReader(text[loc[1]:])).Decode(&candidate) == nil && lazadaRawID(candidate.Data.Root.Fields.PrimaryKey.ItemID) == ref.id {
						module = &candidate
						break
					}
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	seen := map[string]bool{}
	add := func(raw string) {
		image, key, ok := lazadaImageURL(raw)
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		item := Item{URL: image, RequireImage: true}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	if module != nil {
		fields := module.Data.Root.Fields
		result.Title = firstNonEmpty(fields.Product.Title, result.Title)
		keys := make([]string, 0, len(fields.SkuGalleries))
		for k := range fields.SkuGalleries {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			for _, image := range fields.SkuGalleries[key] {
				if image.Type == "" || strings.EqualFold(image.Type, "img") {
					add(image.Src)
				}
			}
		}
		for _, option := range fields.ProductOption.Options {
			add(option.Image)
		}
		for _, property := range fields.ProductOption.SkuBase.Properties {
			for _, value := range property.Values {
				add(value.Image)
				add(value.HoverImage)
			}
		}
		for _, info := range fields.SkuInfos {
			if lazadaRawID(info.ItemID) == ref.id {
				add(info.Image)
			}
		}
		if desc := strings.TrimSpace(fields.Product.Desc); desc != "" {
			if parsed, err := html.Parse(strings.NewReader(desc)); err == nil {
				walkHTML(parsed, func(n *html.Node) {
					if n.Type == html.ElementNode && n.Data == "img" {
						add(firstNonEmpty(attribute(n, "data-src"), attribute(n, "src")))
					}
				})
			}
		}
		if len(result.Items) == 0 {
			return result, errors.New("Lazada 未返回可信商品图片")
		}
		if fields.PageURL != "" || fields.Product.PageURL != "" {
			return result, errors.New("Lazada 商品详情需要额外加载，图片可能不完整")
		}
		return result, nil
	}
	for _, product := range structured {
		u, err := crawlURL(page, product.URL)
		if err != nil || lazadaPageRef(u) != ref {
			continue
		}
		var images []string
		if json.Unmarshal(product.Image, &images) != nil {
			var image string
			if json.Unmarshal(product.Image, &image) == nil {
				images = []string{image}
			}
		}
		for _, image := range images {
			add(image)
		}
		if len(result.Items) > 0 {
			result.Title = firstNonEmpty(product.Name, result.Title)
			return result, errors.New("Lazada 仅取得商品图库，规格或详情图片可能不完整")
		}
	}
	if u, err := crawlURL(page, canonical); err == nil && lazadaPageRef(u) == ref {
		add(cover)
	}
	if len(result.Items) > 0 {
		return result, errors.New("Lazada 仅取得商品封面，图片可能不完整")
	}
	return result, errors.New("Lazada 商品数据尚未加载，可能需要登录或验证")
}
