package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/chromedp/chromedp"

	extbundle "github.com/xxww0098/tulai/extension"
)

var temuSearchPath = regexp.MustCompile(`^/(?:[a-zA-Z]{2}(?:-[a-zA-Z]{2})?/)?search_result\.html$`)
var temuHomePath = regexp.MustCompile(`^/(?:[a-zA-Z]{2}(?:-[a-zA-Z]{2})?/?)?$`)
var temuSearchGoodsID = regexp.MustCompile(`^[1-9][0-9]{14,19}$`)

// shortcut: cap a single search at 10000 cards; use narrower searches for larger collections.
const maxTemuSearchCards = 10000

func temuSearchSite(page *url.URL) bool {
	if page == nil || page.User != nil || (page.Scheme != "http" && page.Scheme != "https") ||
		(page.Port() != "" && !(page.Scheme == "http" && page.Port() == "80") && !(page.Scheme == "https" && page.Port() == "443")) ||
		page.RawPath != "" {
		return false
	}
	host := strings.ToLower(page.Hostname())
	return host == "www.temu.com" || host == "temu.com"
}

func isTemuSearch(page *url.URL) bool {
	return temuSearchSite(page) && temuSearchPath.MatchString(page.Path)
}

func isTemuHome(page *url.URL) bool {
	return temuSearchSite(page) && temuHomePath.MatchString(page.Path)
}

func temuSearchQuery(page *url.URL) (url.Values, bool) {
	if !isTemuSearch(page) {
		return nil, false
	}
	query, err := url.ParseQuery(page.RawQuery)
	values := query["search_key"]
	if err != nil || len(values) != 1 || len(values[0]) > 1024 || strings.TrimSpace(values[0]) == "" || strings.ContainsAny(values[0], "\x00\r\n") {
		return nil, false
	}
	return query, true
}

func temuSearchTrackingKey(key string) bool {
	return strings.HasPrefix(key, "refer_page_") || strings.HasPrefix(key, "_x_") ||
		key == "search_method" || key == "srch_enter_source" || key == "is_back"
}

func temuSearchSameSearch(start, current *url.URL) bool {
	a, okA := temuSearchQuery(start)
	b, okB := temuSearchQuery(current)
	if !okA || !okB || current.Scheme != "https" || start.Path != current.Path {
		return false
	}
	for key, values := range a {
		if !temuSearchTrackingKey(key) && !slices.Equal(values, b[key]) {
			return false
		}
	}
	for key := range b {
		if !temuSearchTrackingKey(key) && !slices.Equal(a[key], b[key]) {
			return false
		}
	}
	return true
}

func discoverTemuSearch(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if _, ok := temuSearchQuery(page); !ok || getter == nil {
		return result, errors.New("TEMU 搜索地址或查询条件无效")
	}
	return result, errors.New("TEMU 搜索请使用下载浏览器，完成验证并加载商品后再采集")
}

type temuSearchRow struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Image string `json:"image"`
	Link  string `json:"link"`
}

type temuSearchSnapshot struct {
	URL       string          `json:"url"`
	Title     string          `json:"title"`
	Rows      []temuSearchRow `json:"rows"`
	Missing   int             `json:"missing"`
	Truncated bool            `json:"truncated"`
	Blocked   bool            `json:"blocked"`
}

// The selected classes and title attributes are emitted by Temu's search and goods-list scripts.
// Only read visible cards from the current page; no network response interception.
var temuSearchSnapshotScript = "(() => {\n" + extbundle.TemuSearchJS() + "\nreturn tulaiTemuSnapshot(); })()"

func temuSearchCollect(ctx context.Context, page *url.URL, snapshot temuSearchSnapshot) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	current, err := crawlInputURL(snapshot.URL)
	if err != nil || !temuSearchSameSearch(page, current) {
		return result, errors.New("TEMU 搜索页面已跳转或查询条件发生变化")
	}
	if snapshot.Blocked {
		return result, errors.New("TEMU 要求安全验证，请在下载浏览器中完成后重试")
	}
	result.Title = strings.TrimSpace(snapshot.Title)
	seen := make(map[string]bool)
	invalid := false
	for _, row := range snapshot.Rows {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		link, linkErr := crawlURL(page, row.Link)
		if !temuSearchGoodsID.MatchString(row.ID) || linkErr != nil || link.Scheme != "https" || temuProductID(link) != row.ID {
			invalid = true
			continue
		}
		if seen[row.ID] {
			continue
		}
		image, alt, ok := temuImageURL(page, row.Image)
		if !ok {
			invalid = true
			continue
		}
		if len(result.Items) >= maxTemuSearchCards {
			snapshot.Truncated = true
			break
		}
		seen[row.ID] = true
		title := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, row.Title))
		if title == "" {
			title = row.ID
		}
		if runes := []rune(title); len(runes) > 256 {
			title = string(runes[:256])
		}
		item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{
			"temu_goods_id": row.ID, "album_id": row.ID, "album_title": title, "image_kind": "main",
		}}
		if alt != "" {
			item.AlternateURLs = []string{alt}
		}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	if len(result.Items) == 0 {
		return result, errors.New("没有找到已加载的 TEMU 搜索商品；请完成验证或等待页面加载")
	}
	var issues []error
	if snapshot.Missing > 0 || invalid {
		issues = append(issues, errors.New("部分 TEMU 商品图片无法确认来源"))
	}
	if snapshot.Truncated {
		issues = append(issues, fmt.Errorf("TEMU 搜索超过 %d 个商品的采集上限", maxTemuSearchCards))
	}
	return result, errors.Join(issues...)
}

func temuSearchCapture(ctx context.Context, s *RenderedSession, start, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	_, pageOK := temuSearchQuery(page)
	if !pageOK || page.Scheme != "https" ||
		(isTemuSearch(start) && !temuSearchSameSearch(start, page)) ||
		(!isTemuSearch(start) && !isTemuHome(start)) {
		return result, errors.New("TEMU 搜索页面已跳转或查询条件发生变化")
	}
	var snapshot temuSearchSnapshot
	if err := s.run(ctx, chromedp.Evaluate(temuSearchSnapshotScript, &snapshot)); err != nil {
		return result, err
	}
	var current string
	if err := s.run(ctx, chromedp.Location(&current)); err != nil {
		return result, err
	}
	if current != snapshot.URL {
		return result, errors.New("采集期间 TEMU 搜索页面发生变化")
	}
	var cached struct {
		Value    string `json:"value"`
		TooLarge bool   `json:"tooLarge"`
	}
	if err := s.run(ctx, chromedp.Evaluate(`(() => {
  const value = document.querySelector('[data-tulai-download-helper]')?.getAttribute('data-tulai-temu-snapshot') || '';
  return {value: value.length <= 8 * 1024 * 1024 ? value : '', tooLarge: value.length > 8 * 1024 * 1024};
})()`, &cached)); err != nil {
		return result, err
	}
	if cached.TooLarge {
		return result, errors.New("TEMU 商品信息过多，请缩小搜索范围后下载")
	}
	if cached.Value != "" {
		var prior temuSearchSnapshot
		if err := json.Unmarshal([]byte(cached.Value), &prior); err != nil {
			return result, errors.New("TEMU 商品列表无效，请重新点击下载")
		}
		priorPage, err := crawlInputURL(prior.URL)
		if err != nil || !temuSearchSameSearch(page, priorPage) {
			return result, errors.New("TEMU 搜索条件已改变，请重新点击下载")
		}
		snapshot.Rows = append(prior.Rows, snapshot.Rows...)
		snapshot.Truncated = snapshot.Truncated || prior.Truncated
	}
	return temuSearchCollect(ctx, page, snapshot)
}
