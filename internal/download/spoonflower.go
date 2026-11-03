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
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

const spoonflowerAPI = "https://api-gateway.spoonflower.com/alpenrose/design/"

var (
	spoonflowerID         = regexp.MustCompile(`^[1-9][0-9]{0,14}$`)
	spoonflowerDesignPath = regexp.MustCompile(`^/en/(?:fabric|wallpaper)/([1-9][0-9]{0,14})(?:-[^/]+)?/?$`)
	spoonflowerProfile    = regexp.MustCompile(`^/profiles/[A-Za-z0-9_-]{1,128}/?$`)
)

func spoonflowerHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	return strings.EqualFold(u.Hostname(), "www.spoonflower.com") || strings.EqualFold(u.Hostname(), "spoonflower.com")
}

func spoonflowerDesignID(u *url.URL) string {
	if !spoonflowerHost(u) {
		return ""
	}
	match := spoonflowerDesignPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func isSpoonflowerShop(u *url.URL) bool {
	return spoonflowerHost(u) && (u.Path == "/en/shop" || u.Path == "/en/shop/" || spoonflowerProfile.MatchString(u.Path))
}

func isSpoonflowerPage(u *url.URL) bool { return spoonflowerDesignID(u) != "" || isSpoonflowerShop(u) }

// spoonflowerPublicGetter avoids following an HTTP redirect or using a cookie
// jar when fetching the site's public, cross-origin design endpoint.
func spoonflowerPublicGetter(getter Getter) Getter {
	if original, ok := getter.(*HTTPGetter); ok {
		copyGetter := *original
		copyClient := *original.client
		copyClient.Jar = nil
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		copyGetter.client = &copyClient
		return &copyGetter
	}
	return getter
}

func spoonflowerImageURL(raw, host, id, kind string) (string, bool) {
	if !spoonflowerID.MatchString(id) {
		return "", false
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), host) ||
		(u.Port() != "" && u.Port() != "443") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") ||
		!strings.HasPrefix(u.Path, "/c/"+id+"/"+kind+"/") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func spoonflowerLegacyThumbnail(id string) string {
	n, _ := strconv.ParseInt(id, 10, 64)
	return fmt.Sprintf("https://s3.amazonaws.com/spoonflower/public/design_thumbnails/%04d/%d/desktop_ms.jpg", n/10000, n%10000)
}

// spoonflowerLegacyAvailable checks the reference's historical thumbnail before
// choosing it: newer public designs can return 403 even while the API has a preview.
func spoonflowerLegacyAvailable(ctx context.Context, getter Getter, image string) bool {
	response, err := spoonflowerPublicGetter(getter).Get(ctx, image, func(r *http.Request) {
		r.Header.Set("Accept", "image/*")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Range", "bytes=0-511")
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Del("Referer")
	})
	if err != nil || response == nil || response.Body == nil {
		return false
	}
	defer response.Body.Close()
	if (response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent) || response.FinalURL != image {
		return false
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 512))
	return err == nil && strings.HasPrefix(http.DetectContentType(data), "image/")
}

func discoverSpoonflower(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isSpoonflowerPage(root) {
		return result, errors.New("Spoonflower 地址或下载客户端无效")
	}
	if spoonflowerDesignID(root) != "" {
		return spoonflowerCollect(ctx, getter, root, nil)
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !isSpoonflowerShop(final) || !sameOrigin(root, final) || root.Path != final.Path || root.Query().Encode() != final.Query().Encode() || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Spoonflower 商店不可用或跳转到其他页面")
	}
	return spoonflowerCollect(ctx, getter, final, doc)
}

func spoonflowerCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isSpoonflowerPage(page) {
		return result, errors.New("Spoonflower 页面无效")
	}
	if id := spoonflowerDesignID(page); id != "" {
		return spoonflowerDesign(ctx, getter, page, id)
	}
	return spoonflowerShopPage(ctx, page, doc)
}

func spoonflowerDesign(ctx context.Context, getter Getter, page *url.URL, id string) (Discovery, error) {
	result := Discovery{IsPage: true}
	endpoint := spoonflowerAPI + id
	response, err := spoonflowerPublicGetter(getter).Get(ctx, endpoint, func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Del("Referer")
	})
	if err != nil {
		return result, err
	}
	if response == nil || response.Body == nil {
		return result, errors.New("Spoonflower 设计接口没有返回数据")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.FinalURL != endpoint {
		return result, errors.New("Spoonflower 设计接口不可用或发生跳转")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return result, err
	}
	if len(body) > maxPageBytes {
		return result, errors.New("Spoonflower 设计数据超过大小限制")
	}
	var data struct {
		Data struct {
			ID           json.Number `json:"id"`
			Name         string      `json:"name"`
			ForSale      *bool       `json:"for_sale"`
			ThumbnailURL string      `json:"thumbnail_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil || data.Data.ID.String() != id || data.Data.ForSale == nil || !*data.Data.ForSale {
		return result, errors.New("Spoonflower 未返回匹配的公开设计")
	}
	result.Title = data.Data.Name
	preview, valid := spoonflowerImageURL(data.Data.ThumbnailURL, "garden.spoonflower.com", id, "i")
	legacy := spoonflowerLegacyThumbnail(id)
	useLegacy := spoonflowerLegacyAvailable(ctx, getter, legacy)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	image := legacy
	if !useLegacy {
		if !valid {
			return result, errors.New("Spoonflower 未返回可验证的设计预览")
		}
		image = preview
	}
	item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"spoonflower_design_id": id}}
	if referer := imageReferer(page.String(), image); referer != "" {
		item.Headers = map[string]string{"Referer": referer}
	}
	result.Items = append(result.Items, item)
	if !useLegacy {
		return result, errors.New("Spoonflower 的旧版缩略图不可用，已使用公开设计预览")
	}
	return result, nil
}

// The shop result image and product link must carry the same design ID. The
// current site's pagination has not been fully verified, so only this page is
// returned and callers are told the result may be incomplete.
func spoonflowerShopPage(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if doc == nil || !isSpoonflowerShop(page) {
		return result, errors.New("Spoonflower 商店页面不可用")
	}
	seen := map[string]bool{}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "a" {
			return
		}
		link, err := url.Parse(attribute(n, "href"))
		if err != nil {
			return
		}
		id := spoonflowerDesignID(page.ResolveReference(link))
		if id == "" || seen[id] {
			return
		}
		walkHTML(n, func(img *html.Node) {
			if seen[id] || img.Type != html.ElementNode || img.Data != "img" {
				return
			}
			image, ok := spoonflowerImageURL(attribute(img, "src"), "img.spoonflower.com", id, "p")
			if !ok {
				return
			}
			seen[id] = true
			item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"spoonflower_design_id": id}}
			if referer := imageReferer(page.String(), image); referer != "" {
				item.Headers = map[string]string{"Referer": referer}
			}
			result.Items = append(result.Items, item)
		})
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 {
		return result, errors.New("Spoonflower 商店未返回可确认身份的设计卡片，可能需要在浏览器中验证")
	}
	return result, errors.New("Spoonflower 仅采集了当前页可确认的设计预览，分页可能不完整")
}
