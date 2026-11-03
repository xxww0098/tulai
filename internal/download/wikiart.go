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
	wikiArtSlug       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
	wikiArtPaintingID = regexp.MustCompile(`^[a-f0-9]{24}$`)
	wikiArtImageHost  = regexp.MustCompile(`^uploads[0-9]{0,2}\.wikiart\.org$`)
)

const wikiArtImageLimit = 10000

type wikiArtRef struct {
	base     string
	page     int
	filtered bool
}

func wikiArtHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return false
	}
	return strings.EqualFold(u.Hostname(), "www.wikiart.org") || strings.EqualFold(u.Hostname(), "wikiart.org")
}

func wikiArtAuthorRef(u *url.URL) wikiArtRef {
	if !wikiArtHost(u) || path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return wikiArtRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || !wikiArtSlug.MatchString(parts[1]) {
		return wikiArtRef{}
	}
	switch parts[0] {
	case "en", "de", "es", "fr", "pt", "ru", "uk", "zh":
	default:
		return wikiArtRef{}
	}
	switch parts[1] {
	case "search", "artists-by-field", "artists-by-art-movement", "popular-artists", "artists", "paintings-by-genre", "artwork-of-the-day", "login", "about", "privacy", "terms":
		return wikiArtRef{}
	}
	ref := wikiArtRef{base: "/" + parts[0] + "/" + parts[1], page: 1, filtered: u.Fragment != ""}
	switch {
	case len(parts) == 2:
	case len(parts) == 3 && parts[2] == "all-works":
	case len(parts) == 4 && parts[2] == "all-works" && parts[3] == "text-list":
	case (len(parts) == 4 || len(parts) == 5) && parts[2] == "mode" && parts[3] == "all-paintings":
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["page"]) > 1 {
			ref.filtered = true
			return ref
		}
		for k, v := range q {
			if len(v) != 1 {
				ref.filtered = true
				continue
			}
			switch k {
			case "json":
				ref.filtered = ref.filtered || v[0] != "2"
			case "layout":
				ref.filtered = ref.filtered || v[0] != "new"
			case "resultType":
				ref.filtered = ref.filtered || v[0] != "masonry"
			case "page":
			default:
				ref.filtered = true
			}
		}
		if len(parts) == 5 {
			if q.Get("page") != "" {
				ref.filtered = true
			}
			ref.page, err = strconv.Atoi(parts[4])
		} else if q.Get("page") != "" {
			ref.page, err = strconv.Atoi(q.Get("page"))
		}
		if err != nil || ref.page < 1 || ref.page > 10000 {
			ref.filtered = true
		}
	default:
		return wikiArtRef{}
	}
	return ref
}

func isWikiArtAuthor(u *url.URL) bool { return wikiArtAuthorRef(u).base != "" }

func wikiArtJSONInput(u *url.URL) bool {
	if wikiArtAuthorRef(u).base == "" {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	q, err := url.ParseQuery(u.RawQuery)
	return err == nil && (len(parts) == 4 || len(parts) == 5) && parts[2] == "mode" && parts[3] == "all-paintings" && len(q["json"]) == 1 && q.Get("json") == "2"
}

func wikiArtImageURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !wikiArtImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || !strings.Contains(u.Path, "/images/") ||
		path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") || strings.HasSuffix(u.Path, "/") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func discoverWikiArt(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := wikiArtAuthorRef(root)
	if getter == nil || want.base == "" {
		return result, errors.New("WikiArt 作者地址或下载客户端无效")
	}
	if want.filtered {
		return result, errors.New("WikiArt 当前筛选或分页条件暂不支持")
	}
	if wikiArtJSONInput(root) {
		return wikiArtPaginate(ctx, getter, root, want)
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	got := wikiArtAuthorRef(final)
	if doc == nil || got.base != want.base || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("WikiArt 作者页面不可用或跳转到其他作者")
	}
	// Preserve a requested starting page if the server redirects to the base URL.
	got.page, got.filtered = want.page, want.filtered
	return wikiArtAuthor(ctx, getter, final, doc, got)
}

func wikiArtCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	ref := wikiArtAuthorRef(page)
	if wikiArtJSONInput(page) {
		return wikiArtPaginate(ctx, getter, page, ref)
	}
	return wikiArtAuthor(ctx, getter, page, doc, ref)
}

type wikiArtPainting struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Year        any    `json:"year"`
	ArtistName  string `json:"artistName"`
	ArtistURL   string `json:"artistUrl"`
	PaintingURL string `json:"paintingUrl"`
	Image       string `json:"image"`
}

type wikiArtData struct {
	Paintings []wikiArtPainting `json:"Paintings"`
	Total     *int              `json:"AllPaintingsCount"`
	PageSize  *int              `json:"PageSize"`
}

func wikiArtURLWithin(page *url.URL, raw, base string, painting bool) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	u = page.ResolveReference(u)
	if !wikiArtHost(u) || u.Scheme != "https" || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || path.Clean(u.Path) != u.Path {
		return false
	}
	if !painting {
		return u.Path == base
	}
	if !strings.HasPrefix(u.Path, base+"/") {
		return false
	}
	name := strings.TrimPrefix(u.Path, base+"/")
	return name != "" && !strings.Contains(name, "/")
}

func wikiArtPage(ctx context.Context, getter Getter, ref wikiArtRef, page int) (wikiArtData, error) {
	endpoint := "https://www.wikiart.org" + ref.base + "/mode/all-paintings?json=2&layout=new&page=" + strconv.Itoa(page) + "&resultType=masonry"
	var data wikiArtData
	response, err := getter.Get(ctx, endpoint, func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
	})
	if err != nil {
		return data, err
	}
	if response == nil || response.Body == nil {
		return data, errors.New("WikiArt 作品接口没有响应")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || firstNonEmpty(response.FinalURL, endpoint) != endpoint {
		return data, fmt.Errorf("WikiArt 第 %d 页不可用或发生跳转", page)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return data, err
	}
	if len(body) > maxPageBytes {
		return data, errors.New("WikiArt 作品数据超过大小限制")
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil || data.Total == nil || data.PageSize == nil || *data.Total < 0 || *data.PageSize < 1 || *data.PageSize > 2000 || len(data.Paintings) > 2000 {
		return data, errors.New("WikiArt 作品数据无效")
	}
	return data, nil
}

// shortcut: cap pages and images until larger author collections can be configured.
func wikiArtAuthor(ctx context.Context, getter Getter, page *url.URL, doc *html.Node, ref wikiArtRef) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || doc == nil || ref.base == "" {
		return result, errors.New("WikiArt 作者页面无效")
	}
	if ref.filtered {
		return result, errors.New("WikiArt 当前筛选或分页条件暂不支持")
	}
	var canonical, mismatch bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			if u, err := url.Parse(attribute(n, "href")); err == nil && wikiArtAuthorRef(page.ResolveReference(u)).base == ref.base {
				canonical = true
			} else {
				mismatch = true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !canonical || mismatch {
		return result, errors.New("WikiArt 作者页面身份无法确认")
	}
	return wikiArtPaginate(ctx, getter, page, ref)
}

func wikiArtPaginate(ctx context.Context, getter Getter, page *url.URL, ref wikiArtRef) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || page == nil || ref.base == "" {
		return result, errors.New("WikiArt 作者页面无效")
	}
	if ref.filtered {
		return result, errors.New("WikiArt 当前筛选或分页条件暂不支持")
	}
	seenIDs, seenURLs := map[string]bool{}, map[string]bool{}
	expected, fetched := -1, 0
	incomplete := false
	for count, p := 0, ref.page; count < maxCrawlPages; count, p = count+1, p+1 {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		data, err := wikiArtPage(ctx, getter, ref, p)
		if err != nil {
			return result, fmt.Errorf("WikiArt 作品分页读取失败: %w", err)
		}
		if expected < 0 {
			expected = *data.Total
		} else if expected != *data.Total {
			incomplete = true
			if *data.Total > expected {
				expected = *data.Total
			}
		}
		if len(data.Paintings) == 0 {
			if fetched == 0 {
				return result, errors.New("WikiArt 未返回该作者的作品")
			}
			if ref.page == 1 && fetched < expected {
				return result, errors.New("WikiArt 作品分页提前结束，图片不完整")
			}
			if incomplete || ref.page > 1 {
				return result, errors.New("WikiArt 作品可能不完整")
			}
			return result, nil
		}
		fresh := 0
		for _, painting := range data.Paintings {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if !wikiArtPaintingID.MatchString(painting.ID) || !wikiArtURLWithin(page, painting.ArtistURL, ref.base, false) || !wikiArtURLWithin(page, painting.PaintingURL, ref.base, true) {
				incomplete = true
				continue
			}
			if seenIDs[painting.ID] {
				incomplete = true
				continue
			}
			seenIDs[painting.ID] = true
			fresh++
			image, ok := wikiArtImageURL(painting.Image)
			if !ok {
				incomplete = true
				continue
			}
			if seenURLs[image] {
				continue
			}
			if len(result.Items) >= wikiArtImageLimit {
				return result, errors.New("WikiArt 作品达到图片采集上限")
			}
			seenURLs[image] = true
			if result.Title == "" {
				result.Title = painting.ArtistName
			}
			year := ""
			switch v := painting.Year.(type) {
			case string:
				year = v
			case json.Number:
				year = v.String()
			}
			item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"wikiart_artist": painting.ArtistName, "wikiart_painting_id": painting.ID, "wikiart_title": painting.Title, "wikiart_year": year}}
			if referer := imageReferer(page.String(), image); referer != "" {
				item.Headers = map[string]string{"Referer": referer}
			}
			result.Items = append(result.Items, item)
		}
		fetched += len(data.Paintings)
		if ref.page == 1 && fetched > expected {
			incomplete = true
		}
		if fresh == 0 {
			return result, errors.New("WikiArt 作品分页重复或作者不匹配，图片可能不完整")
		}
		if ref.page == 1 && fetched >= expected {
			if incomplete {
				return result, errors.New("WikiArt 部分作品身份或图片地址无法验证")
			}
			return result, nil
		}
		if ref.page > 1 && len(data.Paintings) < *data.PageSize {
			return result, errors.New("WikiArt 已从指定页开始，未包含更早作品")
		}
	}
	return result, errors.New("WikiArt 作品分页达到采集上限，图片可能不完整")
}
