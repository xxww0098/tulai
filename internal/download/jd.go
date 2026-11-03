package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	jdIDPattern       = regexp.MustCompile(`^[0-9]{1,24}$`)
	jdTokenPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{6,128}$`)
	jdImageHost       = regexp.MustCompile(`^(?:img[0-9]{1,3}|img|m)\.360buyimg\.com$`)
	jdJFSPath         = regexp.MustCompile(`^jfs/[A-Za-z0-9_./-]+\.(?:jpe?g|png|gif|webp|avif|bmp)$`)
	jdMobilePattern   = regexp.MustCompile(`\bwindow\s*\.\s*_itemOnly\s*=\s*\(\s*`)
	jdPagePattern     = regexp.MustCompile(`\b(?:var|let|const)\s+pageConfig\s*=\s*`)
	jdProductPattern  = regexp.MustCompile(`(?:["']product["']|\bproduct)\s*:\s*`)
	jdSKUPattern      = regexp.MustCompile(`\bskuid\s*:\s*([0-9]{1,24})\s*[,}]`)
	jdBackgroundImage = regexp.MustCompile(`(?i)background(?:-image)?\s*:[^;{}]*?url\(\s*["']?([^"')\s]+)`)
)

type jdRef struct {
	id     string
	mobile bool
}

type jdMobileData struct {
	Item struct {
		SKU   string   `json:"skuId"`
		Name  string   `json:"skuName"`
		Image []string `json:"image"`
		Color []struct {
			Image string `json:"imagePath"`
		} `json:"newColorSize"`
	} `json:"item"`
}

func jdProductRef(u *url.URL) jdRef {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return jdRef{}
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "item.jd.com", "item.jd.hk", "i-item.jd.com", "item.m.jd.com", "item.yiyaojd.com", "item.jkcsjd.com":
	default:
		return jdRef{}
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		return jdRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	if len(parts) == 1 && strings.HasSuffix(parts[0], ".html") {
		id = strings.TrimSuffix(parts[0], ".html")
		if !jdIDPattern.MatchString(id) {
			return jdRef{}
		}
	} else if len(parts) == 2 && parts[0] == "product" && strings.HasSuffix(parts[1], ".html") {
		id = strings.TrimSuffix(parts[1], ".html")
		if !jdIDPattern.MatchString(id) && !(host == "item.jd.com" && jdTokenPattern.MatchString(id)) {
			return jdRef{}
		}
	} else {
		return jdRef{}
	}
	return jdRef{id: id, mobile: host == "item.m.jd.com"}
}

func isJDProduct(u *url.URL) bool { return jdProductRef(u).id != "" }

func discoverJDProduct(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	want := jdProductRef(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || want.id == "" {
		return result, errors.New("京东商品页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	got := jdProductRef(final)
	if doc == nil || got.id != want.id || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("京东商品页面不可用或跳转到了其他商品")
	}
	return jdCollect(ctx, getter, final, doc)
}

// jdCollect shares product and detail extraction with the browser capture flow.
func jdCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := jdProductRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || ref.id == "" || doc == nil {
		return result, errors.New("京东商品页面或下载客户端无效")
	}
	seen := make(map[string]bool)
	var sku string
	if jdIDPattern.MatchString(ref.id) {
		sku = ref.id
	}
	var problems []error
	mainCount, detailCount := 0, 0
	add := func(raw string, thumbnail bool, detail bool) {
		if raw == "" {
			return
		}
		image, identity, ok := jdImageURL(page, raw, thumbnail)
		if !ok {
			problems = append(problems, errors.New("京东商品图片地址无效或不属于京东图床"))
			return
		}
		if seen[identity] {
			if detail {
				detailCount++
			}
			return
		}
		seen[identity] = true
		item := Item{URL: image}
		if referer := imageReferer(page.String(), image); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
		if detail {
			detailCount++
		} else {
			mainCount++
		}
	}
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		main, detail := jdImageContext(n)
		if n.Data == "img" && (main || detail) {
			raw := firstNonEmpty(attribute(n, "data-url"), attribute(n, "data-lazyload"), attribute(n, "data-original"), attribute(n, "data-src"), attribute(n, "src"))
			add(raw, main, detail)
		}
		if detail {
			jdDetailNode(n, add)
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, match := range offer1688CodeMatches(text, jdMobilePattern) {
				var data jdMobileData
				decoder := json.NewDecoder(strings.NewReader(text[match[1]:]))
				if decoder.Decode(&data) != nil || !jdIDPattern.MatchString(data.Item.SKU) || (jdIDPattern.MatchString(ref.id) && data.Item.SKU != ref.id) {
					problems = append(problems, errors.New("京东手机商品数据无效或不属于当前商品"))
					continue
				}
				sku = data.Item.SKU
				result.Title = firstNonEmpty(data.Item.Name, result.Title)
				for _, image := range data.Item.Image {
					add(image, true, false)
				}
				for _, variant := range data.Item.Color {
					add(variant.Image, true, false)
				}
			}
			if value := jdConfigSKU(text); value != "" {
				if jdIDPattern.MatchString(ref.id) && value != ref.id {
					problems = append(problems, errors.New("京东商品编号与页面不一致"))
				} else {
					sku = value
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if sku != "" {
		before := detailCount
		if err := jdDetail(ctx, getter, page, sku, add); err != nil {
			problems = append(problems, fmt.Errorf("读取京东商品详情失败，结果可能不完整: %w", err))
		} else if detailCount == before && detailCount == 0 {
			problems = append(problems, errors.New("京东商品详情没有可识别的图片，可能需要登录或安全验证"))
		}
	} else if detailCount == 0 {
		problems = append(problems, errors.New("京东商品详情未加载，结果可能不完整"))
	}
	if mainCount == 0 {
		problems = append(problems, errors.New("京东商品主图未加载，可能需要登录或安全验证"))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, errors.Join(problems...)
}

func jdConfigSKU(text string) string {
	for _, match := range offer1688CodeMatches(text, jdPagePattern) {
		object, ok := offer1688Object(text[match[1]:])
		if !ok {
			continue
		}
		for _, product := range offer1688CodeMatches(object, jdProductPattern) {
			section, ok := offer1688Object(object[product[1]:])
			if !ok {
				continue
			}
			for _, loc := range offer1688CodeMatches(section, jdSKUPattern) {
				return section[loc[2]:loc[3]]
			}
		}
	}
	return ""
}

func jdImageContext(n *html.Node) (bool, bool) {
	for depth := 0; n != nil && depth < 128; depth++ {
		id := attribute(n, "id")
		if id == "J-detail-content" || id == "detail-main" || id == "IntroductionModule" {
			return false, true
		}
		for _, class := range strings.Fields(attribute(n, "class")) {
			if class == "spec-items" || class == "specification-item-sku" || class == "image-carousel-track" {
				return true, false
			}
		}
		if attribute(n, "data-sku") != "" {
			return true, false
		}
		n = n.Parent
	}
	return false, false
}

func jdDetailNode(n *html.Node, add func(string, bool, bool)) {
	if value := attribute(n, "style"); value != "" {
		jdBackgrounds(value, add)
	}
	if n.Data == "style" {
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				jdBackgrounds(child.Data, add)
			}
		}
	}
	if attribute(n, "id") == "zbViewWeChatMiniImages" {
		for _, value := range strings.Split(attribute(n, "value"), ",") {
			value = strings.TrimSpace(value)
			if strings.HasPrefix(value, "/sku/jfs/") {
				add("https://img10.360buyimg.com"+value, false, true)
			} else if value != "" {
				add(value, false, true)
			}
		}
	}
}

func jdBackgrounds(css string, add func(string, bool, bool)) {
	for _, match := range jdBackgroundImage.FindAllStringSubmatch(css, -1) {
		add(match[1], false, true)
	}
}

func jdImageURL(page *url.URL, raw string, thumbnail bool) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "jfs/") {
		if !jdValidJFS(raw) {
			return "", "", false
		}
		return "https://img10.360buyimg.com/n5/s1440x1440_" + raw, raw, true
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Path != "" && (path.Clean(parsed.Path) != parsed.Path || strings.Contains(parsed.Path, "//"))) {
		return "", "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || !jdImageHost.MatchString(strings.ToLower(u.Hostname())) {
		return "", "", false
	}
	index := strings.Index(u.Path, "jfs/")
	if index < 1 || (u.Path[index-1] != '/' && u.Path[index-1] != '_') || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", "", false
	}
	jfs := strings.TrimSuffix(u.Path[index:], ".dpg")
	if !jdValidJFS(jfs) {
		return "", "", false
	}
	if thumbnail {
		return "https://img10.360buyimg.com/n5/s1440x1440_" + jfs, jfs, true
	}
	if strings.HasSuffix(u.Path, ".dpg") {
		u.Path = strings.TrimSuffix(u.Path, ".dpg")
		u.RawPath = ""
	}
	return u.String(), jfs, true
}

func jdValidJFS(raw string) bool {
	return len(raw) <= 1024 && jdJFSPath.MatchString(strings.ToLower(raw)) && path.Clean(raw) == raw && !strings.Contains(raw, "//")
}

func jdDetail(ctx context.Context, getter Getter, page *url.URL, sku string, add func(string, bool, bool)) error {
	if !jdIDPattern.MatchString(sku) {
		return errors.New("京东商品编号无效")
	}
	target := "https://in.m.jd.com/product/graphext/" + sku + ".html"
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: target, referer: page.Scheme + "://" + page.Host + "/", depth: 2})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return err
	}
	if doc == nil || final == nil || final.User != nil || final.Scheme != "https" || !strings.EqualFold(final.Hostname(), "in.m.jd.com") || (final.Port() != "" && final.Port() != "443") || final.Path != "/product/graphext/"+sku+".html" {
		return errors.New("京东商品详情跳转到了其他地址")
	}
	found := false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		_, detail := jdImageContext(n)
		if !detail {
			return
		}
		found = true
		if n.Data == "img" {
			raw := firstNonEmpty(attribute(n, "data-lazyload"), attribute(n, "data-original"), attribute(n, "data-src"), attribute(n, "src"))
			add(raw, false, true)
		}
		jdDetailNode(n, add)
	})
	if err := ctx.Err(); err != nil {
		return err
	}
	if !found {
		return errors.New("京东商品介绍没有加载，可能需要登录或安全验证")
	}
	return nil
}
