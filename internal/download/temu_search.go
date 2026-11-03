package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/chromedp/chromedp"
)

var temuSearchPath = regexp.MustCompile(`^/(?:[a-zA-Z]{2}(?:-[a-zA-Z]{2})?/)?search_result\.html$`)
var temuHomePath = regexp.MustCompile(`^/(?:[a-zA-Z]{2}(?:-[a-zA-Z]{2})?/?)?$`)
var temuSearchGoodsID = regexp.MustCompile(`^[1-9][0-9]{14,19}$`)

// shortcut: capture at most 1000 loaded search cards; raise the limit only after validating larger lists.
const maxTemuSearchCards = 1000

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
		key == "search_method" || key == "srch_enter_source"
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

// The selected classes and title ID are emitted by Temu's search and goods-list scripts.
// Only read already-loaded, visibly rendered cards; no network response interception.
const temuSearchSnapshotScript = `(() => {
  const out = {url: location.href, title: document.title.slice(0, 1024), rows: [], missing: 0, truncated: false, blocked: false};
  const visible = (node, image) => {
    const rect = node.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0) return false;
    let n = node, depth = 0;
    for (; n && depth < 100; n = n.parentElement, depth++) {
      if (n.hidden || (n.getAttribute('aria-hidden') === 'true' && !(image && n === node))) return false;
      const style = getComputedStyle(n);
      if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse' || Number(style.opacity) === 0) return false;
    }
    return !n;
  };
  for (const node of document.querySelectorAll('form, [role="dialog"], iframe')) {
    if (visible(node, false) && /security verification|captcha|verify you are human/i.test((node.innerText || '') + ' ' + (node.getAttribute('title') || ''))) {
      out.blocked = true; return out;
    }
  }
  const cards = document.querySelectorAll('.js-search-goodsList .EKDT7a3v');
  if (cards.length > 10000) out.truncated = true;
  for (let i = 0; i < cards.length && i < 10000; i++) {
    const card = cards[i];
    if (!visible(card, false)) continue;
    const names = [...card.querySelectorAll('[id^="goodName-"]')].filter(n => n.closest('.EKDT7a3v') === card && visible(n, false));
    if (names.length !== 1) { out.missing++; continue; }
    const name = names[0], id = name.id.slice('goodName-'.length);
    if (!/^[1-9][0-9]{14,19}$/.test(id)) { out.missing++; continue; }
    const links = [...card.querySelectorAll('a[href]')].filter(n => n.closest('.EKDT7a3v') === card)
      .map(n => n.href).filter(raw => {
        try { const u = new URL(raw); return /(^|\.)temu\.com$/i.test(u.hostname) && u.pathname.endsWith('-g-' + id + '.html'); }
        catch (_) { return false; }
      });
    if (links.length === 0) { out.missing++; continue; }
    const images = [...new Set([...card.querySelectorAll('img.goods-img-external, .goods-img-external img')]
      .filter(n => n.closest('.EKDT7a3v') === card && n.complete && n.naturalWidth > 0 && visible(n, true))
      .map(n => n.currentSrc || n.src).filter(Boolean))];
    if (images.length !== 1) { out.missing++; continue; }
    if (out.rows.length >= 1000) { out.truncated = true; break; }
    out.rows.push({id, title: (name.getAttribute('title') || name.textContent || '').trim().slice(0, 1024), image: images[0], link: links[0]});
  }
  return out;
})()`

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
	issues = append(issues, errors.New("TEMU 搜索仅采集当前已加载的商品；请手动加载更多后再次采集"))
	if snapshot.Missing > 0 || invalid {
		issues = append(issues, errors.New("部分 TEMU 商品图片未加载或无法确认来源"))
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
	return temuSearchCollect(ctx, page, snapshot)
}
