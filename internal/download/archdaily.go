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

var (
	archDailyNumericID    = regexp.MustCompile(`^[1-9][0-9]{0,14}$`)
	archDailyMediaSlug    = regexp.MustCompile(`(?i)^([a-f0-9]{24})-[^/]+$`)
	archDailyHex          = regexp.MustCompile(`(?i)^[a-f0-9]{4}$`)
	archDailyCategoryPath = regexp.MustCompile(`^/search/projects/categories/([a-z0-9-]{1,100})/?$`)
)

const (
	archDailyArticleLimit = 1000
	archDailyImageLimit   = 10000
	archDailyPageSize     = 18
)

type archDailyRef struct{ id, media string }
type archDailyCategoryRef struct {
	slug string
	page int
}

func archDailyHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "www.archdaily.com", "archdaily.com", "www.archdaily.cn", "archdaily.cn":
		return true
	}
	return false
}

func archDailyPageRef(u *url.URL) archDailyRef {
	if !archDailyHost(u) || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return archDailyRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && (parts[0] == "zh" || parts[0] == "cn" || parts[0] == "es" || parts[0] == "pt" || parts[0] == "en") {
		parts = parts[1:]
	}
	if (len(parts) != 2 && len(parts) != 3) || !archDailyNumericID.MatchString(parts[0]) || parts[1] == "" || len(parts[1]) > 250 {
		return archDailyRef{}
	}
	ref := archDailyRef{id: parts[0]}
	if len(parts) == 3 {
		match := archDailyMediaSlug.FindStringSubmatch(parts[2])
		if len(match) != 2 {
			return archDailyRef{}
		}
		ref.media = strings.ToLower(match[1])
	}
	return ref
}

func archDailyCategoryID(u *url.URL) archDailyCategoryRef {
	if !archDailyHost(u) || !(strings.EqualFold(u.Hostname(), "www.archdaily.com") || strings.EqualFold(u.Hostname(), "archdaily.com")) {
		return archDailyCategoryRef{}
	}
	match := archDailyCategoryPath.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return archDailyCategoryRef{}
	}
	ref := archDailyCategoryRef{slug: match[1], page: 1}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["page"]) > 1 {
		ref.page = 0
		return ref
	}
	for key := range q {
		if key != "page" {
			ref.page = 0
			return ref
		}
	}
	if raw, exists := q["page"]; exists {
		if len(raw) != 1 || raw[0] == "" {
			ref.page = 0
			return ref
		}
		ref.page, err = strconv.Atoi(raw[0])
		if err != nil || ref.page < 1 || ref.page > 10000 {
			ref.page = 0
		}
	}
	return ref
}

func isArchDailyPage(u *url.URL) bool {
	return archDailyPageRef(u).id != "" || archDailyCategoryID(u).slug != ""
}

// archDailyImageURL checks that the CDN path contains the same media ID as
// the gallery entry, and accepts only sizes actually supplied by the site.
func archDailyImageURL(raw, media string, large bool) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "images.adsttc.com") ||
		(u.Port() != "" && u.Port() != "443") || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 10 || parts[0] != "media" || parts[1] != "images" || parts[9] == "" {
		return "", false
	}
	for _, part := range parts[2:8] {
		if !archDailyHex.MatchString(part) {
			return "", false
		}
	}
	if !strings.EqualFold(strings.Join(parts[2:8], ""), media) {
		return "", false
	}
	if large {
		if parts[8] != "large_jpg" {
			return "", false
		}
	} else {
		switch parts[8] {
		case "large_jpg", "slideshow", "medium_jpg", "newsletter", "thumb_jpg":
		default:
			return "", false
		}
	}
	u.Fragment = ""
	return u.String(), true
}

func discoverArchDaily(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isArchDailyPage(root) {
		return result, errors.New("ArchDaily 地址或下载客户端无效")
	}
	if category := archDailyCategoryID(root); category.slug != "" {
		return archDailyCategory(ctx, getter, category)
	}
	want := archDailyPageRef(root)
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || archDailyPageRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("ArchDaily 页面不可用或跳转到其他作品")
	}
	return archDailyCollect(ctx, getter, final, doc)
}

func archDailyCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isArchDailyPage(page) {
		return result, errors.New("ArchDaily 页面无效")
	}
	if category := archDailyCategoryID(page); category.slug != "" {
		return archDailyCategory(ctx, getter, category)
	}
	ref := archDailyPageRef(page)
	if doc == nil {
		return result, errors.New("ArchDaily 页面无效")
	}
	if ref.media != "" {
		return archDailyGallery(ctx, page, doc)
	}
	return archDailyArticle(ctx, getter, page, doc)
}

func archDailyAncestor(n *html.Node, id string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && attribute(p, "id") == id {
			return true
		}
	}
	return false
}

func archDailyIdentity(doc *html.Node, page *url.URL, want archDailyRef) (string, bool) {
	var title string
	var seen, mismatch bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && title == "" {
			title = strings.TrimSpace(nodeText(n))
		}
		var raw string
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			raw = attribute(n, "href")
		} else if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:url") {
			raw = attribute(n, "content")
		}
		if raw == "" {
			return
		}
		u, err := url.Parse(raw)
		if err != nil || archDailyPageRef(page.ResolveReference(u)) != want {
			mismatch = true
		} else {
			seen = true
		}
	})
	return title, seen && !mismatch
}

func archDailyItem(page *url.URL, image, id, media, alt string) Item {
	item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"archdaily_article_id": id, "archdaily_media_id": media, "archdaily_alt": alt}}
	if ref := imageReferer(page.String(), image); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	return item
}

func archDailyArticle(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	ref := archDailyPageRef(page)
	result := Discovery{IsPage: true}
	title, identity := archDailyIdentity(doc, page, ref)
	result.Title = title
	if !identity {
		return result, errors.New("ArchDaily 页面未能确认当前文章身份")
	}
	var gallery *url.URL
	var seenPost bool
	seen := map[string]bool{}
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "article" && attribute(n, "id") == "single-content" {
			seenPost = true
		}
		if n.Data != "img" || !archDailyAncestor(n, "single-content") {
			return
		}
		u, err := crawlURL(page, attribute(n, "longdesc"))
		media := archDailyPageRef(u)
		if err != nil || media.id != ref.id || media.media == "" || u.RawQuery != "" || !sameOrigin(page, u) {
			return
		}
		if gallery == nil {
			gallery = u
		}
		if picID := strings.ToLower(attribute(n, "data-nr-picture-id")); picID != media.media {
			return
		}
		raw := firstNonEmpty(attribute(n, "data-src"), attribute(n, "src"))
		image, ok := archDailyImageURL(raw, media.media, false)
		if ok && !seen[media.media] {
			seen[media.media] = true
			result.Items = append(result.Items, archDailyItem(page, image, ref.id, media.media, attribute(n, "alt")))
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !seenPost || gallery == nil {
		return result, errors.New("ArchDaily 未返回可验证的文章图集入口")
	}
	found, err := discoverArchDaily(ctx, getter, gallery)
	if err != nil {
		if len(found.Items) > 0 {
			found.Title = title
			return found, err
		}
		return result, fmt.Errorf("ArchDaily 图集未能完整读取，已保留文章预览: %w", err)
	}
	found.Title = title
	return found, nil
}

func archDailyGallery(ctx context.Context, page *url.URL, doc *html.Node) (Discovery, error) {
	ref := archDailyPageRef(page)
	result := Discovery{IsPage: true}
	title, identity := archDailyIdentity(doc, page, ref)
	result.Title = title
	if !identity {
		return result, errors.New("ArchDaily 未能确认图集身份")
	}
	var blob string
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && attribute(n, "id") == "gallery-items" && blob == "" {
			blob = attribute(n, "data-images")
		}
	})
	var images []struct {
		Link string `json:"link"`
		URL  string `json:"url_large"`
		Alt  string `json:"image_alt"`
		Type string `json:"type"`
	}
	if blob == "" || json.Unmarshal([]byte(blob), &images) != nil {
		return result, errors.New("ArchDaily 未返回可读取的图集数据")
	}
	var selected, incomplete bool
	seen := map[string]bool{}
	for _, image := range images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if image.Type != "MediaPicture" && image.Type != "MediaPlan" {
			continue
		}
		match := archDailyMediaSlug.FindStringSubmatch(image.Link)
		if len(match) != 2 {
			incomplete = true
			continue
		}
		media := strings.ToLower(match[1])
		selected = selected || media == ref.media
		raw, ok := archDailyImageURL(image.URL, media, true)
		if !ok {
			incomplete = true
			continue
		}
		if seen[media] {
			continue
		}
		if len(result.Items) >= archDailyImageLimit {
			incomplete = true
			break
		}
		seen[media] = true
		result.Items = append(result.Items, archDailyItem(page, raw, ref.id, media, image.Alt))
	}
	if !selected {
		return Discovery{IsPage: true, Title: title}, errors.New("ArchDaily 图集不包含请求的图片")
	}
	if len(result.Items) == 0 {
		return result, errors.New("ArchDaily 图集未返回可信的大图")
	}
	if incomplete {
		return result, errors.New("ArchDaily 部分图集图片无法验证或超过上限")
	}
	return result, nil
}

func archDailyCategoryPage(ctx context.Context, getter Getter, ref archDailyCategoryRef) ([]*url.URL, int, error) {
	endpoint := "https://www.archdaily.com/search/api/v1/us/projects/categories/" + ref.slug + "?page=" + strconv.Itoa(ref.page)
	response, err := getter.Get(ctx, endpoint, func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
	})
	if err != nil {
		return nil, 0, err
	}
	if response == nil || response.Body == nil {
		return nil, 0, errors.New("ArchDaily 分类接口没有返回数据")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.FinalURL != endpoint {
		return nil, 0, errors.New("ArchDaily 分类接口不可用或发生跳转")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > maxPageBytes {
		return nil, 0, errors.New("ArchDaily 分类数据超过大小限制")
	}
	var data struct {
		Total   int `json:"total_docs"`
		Results []struct {
			ID  string `json:"document_id"`
			URL string `json:"url"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &data); err != nil || data.Total < 0 || data.Results == nil {
		return nil, 0, errors.New("ArchDaily 分类数据无效")
	}
	var articles []*url.URL
	invalid := false
	for _, entry := range data.Results {
		u, err := url.Parse(entry.URL)
		item := archDailyPageRef(u)
		if err != nil || item.id != entry.ID || item.id == "" || item.media != "" || !strings.EqualFold(u.Hostname(), "www.archdaily.com") || u.Scheme != "https" {
			invalid = true
			continue
		}
		u.RawQuery, u.Fragment = "", ""
		articles = append(articles, u)
	}
	if invalid {
		return articles, data.Total, errors.New("ArchDaily 分类包含无法验证的作品地址")
	}
	return articles, data.Total, nil
}

// archDailyCategory follows the public category API's 18-item pages. Limits
// avoid unbounded traversal and return already collected images on failure.
// shortcut: cap pages, articles, and images until larger categories can be configured.
func archDailyCategory(ctx context.Context, getter Getter, ref archDailyCategoryRef) (Discovery, error) {
	result := Discovery{IsPage: true}
	if ref.slug == "" || ref.page == 0 {
		return result, errors.New("ArchDaily 分类筛选或页码暂不支持")
	}
	seenArticles, seenImages := map[string]bool{}, map[string]bool{}
	var warnings []error
	articleFailed := false
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(warnings, err)...)
		}
		articles, total, err := archDailyCategoryPage(ctx, getter, ref)
		if err != nil {
			if len(articles) == 0 {
				return result, errors.Join(append(warnings, err)...)
			}
			warnings = append(warnings, err)
		}
		if len(articles) == 0 {
			if total > (ref.page-1)*archDailyPageSize {
				return result, errors.Join(append(warnings, errors.New("ArchDaily 分类分页未返回预期结果"))...)
			}
			break
		}
		for _, article := range articles {
			id := archDailyPageRef(article).id
			if seenArticles[id] {
				continue
			}
			if len(seenArticles) >= archDailyArticleLimit {
				return result, errors.Join(append(warnings, errors.New("ArchDaily 分类达到文章采集上限"))...)
			}
			seenArticles[id] = true
			found, err := discoverArchDaily(ctx, getter, article)
			if ctx.Err() != nil {
				return result, errors.Join(append(warnings, ctx.Err())...)
			}
			if err != nil && !articleFailed {
				warnings = append(warnings, fmt.Errorf("ArchDaily 部分作品读取失败: %w", err))
				articleFailed = true
			}
			for _, item := range found.Items {
				if seenImages[item.URL] {
					continue
				}
				if len(result.Items) >= archDailyImageLimit {
					return result, errors.Join(append(warnings, errors.New("ArchDaily 分类达到图片采集上限"))...)
				}
				seenImages[item.URL] = true
				result.Items = append(result.Items, item)
			}
		}
		if ref.page*archDailyPageSize >= total {
			break
		}
		ref.page++
		if count == maxCrawlPages-1 {
			warnings = append(warnings, errors.New("ArchDaily 分类达到分页采集上限"))
		}
	}
	if len(result.Items) == 0 {
		return result, errors.Join(append(warnings, errors.New("ArchDaily 分类未返回可采集的图片"))...)
	}
	return result, errors.Join(warnings...)
}
