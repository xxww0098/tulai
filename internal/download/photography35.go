package download

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	photography35PhotoPath = regexp.MustCompile(`^/(?:([A-Za-z0-9_-]{1,64})/)?photo_([0-9]{1,20})/?$`)
	photography35Slug      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	photography35Digits    = regexp.MustCompile(`^[0-9]{1,20}$`)
	photography35Data      = regexp.MustCompile(`\bphotoData\s*=`)
	photography35Adult     = regexp.MustCompile(`\badultText\s*=\s*(true|false)\b`)
	photography35NextUser  = regexp.MustCompile(`\bnextListMoreOp\s*=\s*["']&user_id=([0-9]{1,20})["']`)
	photography35NextID    = regexp.MustCompile(`\bshowNextListId\s*=\s*([0-9]{1,20})`)
)

// shortcut: keep author traversal bounded; series are expanded only on their
// individual pages, so a portfolio returns one image per listed work.
const (
	photography35MaxPages  = 100
	photography35MaxImages = 10000
)

type photography35Ref struct{ author, photo string }

func photography35Host(page *url.URL) bool {
	return page != nil && page.User == nil && page.Scheme == "https" && strings.EqualFold(page.Hostname(), "35photo.pro") &&
		(page.Port() == "" || page.Port() == "443") && !strings.Contains(strings.ToLower(page.EscapedPath()), "%2f") &&
		!strings.Contains(page.Path, "\\") && !strings.Contains(page.Path, "..")
}
func photography35PhotoRef(page *url.URL) photography35Ref {
	if !photography35Host(page) {
		return photography35Ref{}
	}
	match := photography35PhotoPath.FindStringSubmatch(page.Path)
	if match == nil {
		return photography35Ref{}
	}
	return photography35Ref{author: match[1], photo: match[2]}
}
func isPhotography35Photo(page *url.URL) bool { return photography35PhotoRef(page).photo != "" }
func photography35Author(page *url.URL) string {
	if !photography35Host(page) || isPhotography35Photo(page) {
		return ""
	}
	path := strings.Trim(page.Path, "/")
	if !photography35Slug.MatchString(path) || strings.Contains(path, "/") {
		return ""
	}
	switch strings.ToLower(path) {
	case "photo", "photos", "photographers", "rating", "genre", "items", "ps", "reg", "login", "search", "about", "help", "contact", "contacts", "awards", "albums", "album", "news", "feed", "tag", "tags", "top", "new", "best", "series", "community", "communities":
		return ""
	}
	return path
}
func isPhotography35Author(page *url.URL) bool { return photography35Author(page) != "" }

func discoverPhotography35Photo(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := photography35PhotoRef(page)
	if getter == nil || ref.photo == "" {
		return result, errors.New("35photo 作品地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("读取 35photo 作品失败：%w", err)
	}
	finalRef := photography35PhotoRef(final)
	if doc == nil || !sameOrigin(page, final) || finalRef.photo != ref.photo || (ref.author != "" && !strings.EqualFold(finalRef.author, ref.author)) {
		return result, errors.New("35photo 作品已跳转，可能需要登录或验证")
	}
	return photography35PhotoPage(ctx, doc, final)
}

func photography35Asset(raw, kind, id string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 || !photography35Digits.MatchString(id) {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || !photography35Host(u) || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	switch kind {
	case "main", "series":
		base := "photos_main"
		if kind == "series" {
			base = "photos_series"
		}
		if len(parts) != 3 || parts[0] != base || !photography35Digits.MatchString(parts[1]) || parts[2] != id+".jpg" {
			return "", false
		}
	case "display":
		if len(parts) != 4 || parts[0] != "photos_temp" || parts[1] != "sizes" || !photography35Digits.MatchString(parts[2]) || parts[3] != id+"_1500n.jpg" {
			return "", false
		}
	case "series-display":
		if len(parts) != 3 || parts[0] != "photos_series_prev" || !photography35Digits.MatchString(parts[1]) || parts[2] != id+"_1000n.jpg" {
			return "", false
		}
	default:
		return "", false
	}
	return u.String(), true
}

type photography35PhotoData struct {
	PhotoID   json.Number `json:"photo_id"`
	UserID    json.Number `json:"user_id"`
	UserLogin string      `json:"user_login"`
	UserName  string      `json:"user_name"`
	Name      string      `json:"photo_name"`
	Link      string      `json:"link"`
	Src       string      `json:"src"`
	Mobile    string      `json:"src-mobile"`
	BuyAllow  *int        `json:"buyAllow"`
	Series    []struct {
		ID     json.Number `json:"id"`
		Src    string      `json:"src"`
		Mobile string      `json:"src-mobile"`
	} `json:"series"`
}

func photography35HasClass(n *html.Node, want string) bool {
	for _, v := range strings.Fields(attribute(n, "class")) {
		if v == want {
			return true
		}
	}
	return false
}
func photography35Inside(n *html.Node, id string) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && attribute(p, "id") == id {
			return true
		}
	}
	return false
}

// Read the site's JSON assignment as data and verify its identity against the
// page URL and displayed photo nodes. JavaScript is never evaluated.
func photography35PhotoPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := photography35PhotoRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || ref.photo == "" {
		return result, errors.New("35photo 作品页面无效")
	}
	var found, malformed, blocked bool
	var state photography35PhotoData
	shown := make(map[string]map[string]bool)
	mainShown := make(map[string]bool)
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "script" {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.TextNode {
					continue
				}
				text := child.Data
				for _, m := range offer1688CodeMatches(text, photography35Adult) {
					if text[m[2]:m[3]] == "true" {
						blocked = true
					}
				}
				for _, m := range offer1688CodeMatches(text, photography35Data) {
					if found {
						malformed = true
						continue
					}
					tail := strings.TrimSpace(text[m[1]:])
					dec := json.NewDecoder(strings.NewReader(tail))
					var value photography35PhotoData
					if dec.Decode(&value) != nil || !offer1688LiteralEnd(tail[int(dec.InputOffset()):]) {
						malformed = true
						continue
					}
					found = true
					state = value
				}
			}
		}
		if n.Data != "img" || !photography35HasClass(n, "mainPhoto") || !photography35Inside(n, "zonePhotoInScroll") {
			return
		}
		for _, key := range []string{"src", "data-src"} {
			raw := attribute(n, key)
			if raw == "" {
				continue
			}
			if attribute(n, "id") == "mainPhoto" {
				mainShown[raw] = true
			}
			sid := attribute(n, "photo-ids")
			if photography35Digits.MatchString(sid) {
				if shown[sid] == nil {
					shown[sid] = make(map[string]bool)
				}
				shown[sid][raw] = true
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if blocked {
		return result, errors.New("35photo 作品要求额外访问验证")
	}
	if !found || malformed || state.PhotoID.String() != ref.photo || !photography35Digits.MatchString(state.UserID.String()) || !photography35Slug.MatchString(state.UserLogin) {
		return result, errors.New("35photo 作品与作者信息无法确认")
	}
	if ref.author != "" && !strings.EqualFold(ref.author, state.UserLogin) {
		return result, errors.New("35photo 作品作者不匹配")
	}
	link, err := url.Parse(state.Link)
	linked := photography35PhotoRef(link)
	if err != nil || !sameOrigin(page, link) || linked.photo != ref.photo || (linked.author != "" && !strings.EqualFold(linked.author, state.UserLogin)) {
		return result, errors.New("35photo 作品链接不匹配")
	}
	result.Title = state.Name
	allowFull := state.BuyAllow != nil && *state.BuyAllow == 0
	var issues []error
	if !allowFull {
		issues = append(issues, errors.New("35photo 未确认原图访问条件，仅采集页面展示图"))
	}
	seen := make(map[string]bool)
	add := func(raw, kind, id string) bool {
		image, ok := photography35Asset(raw, kind, id)
		if !ok {
			return false
		}
		if seen[image] {
			return true
		}
		seen[image] = true
		result.Items = append(result.Items, Item{URL: image, RequireImage: true, PublicOnly: true, Headers: map[string]string{"Referer": "https://35photo.pro/"}, Metadata: map[string]string{"title": state.Name, "35photo_photo_id": ref.photo, "35photo_author_id": state.UserID.String(), "35photo_author": state.UserLogin}})
		return true
	}
	if len(state.Series) == 0 {
		if !mainShown[state.Mobile] {
			return result, errors.New("35photo 作品主图未在当前页面展示")
		}
		if allowFull {
			if !add(state.Src, "main", ref.photo) {
				issues = append(issues, errors.New("35photo 作品主图地址无法确认"))
			}
		} else if !add(state.Mobile, "display", ref.photo) {
			issues = append(issues, errors.New("35photo 展示图地址无法确认"))
		}
	} else {
		if len(state.Series) > photography35MaxImages {
			return result, errors.New("35photo 系列图片超过扫描上限")
		}
		for _, series := range state.Series {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			sid := series.ID.String()
			if !photography35Digits.MatchString(sid) || !shown[sid][series.Mobile] {
				issues = append(issues, errors.New("部分 35photo 系列图片与页面不匹配"))
				continue
			}
			if allowFull {
				if !add(series.Src, "series", sid) {
					issues = append(issues, errors.New("部分 35photo 系列图片地址无法确认"))
				}
			} else if !add(series.Mobile, "series-display", sid) {
				issues = append(issues, errors.New("部分 35photo 系列展示图地址无法确认"))
			}
		}
	}
	if len(result.Items) == 0 {
		issues = append(issues, errors.New("未找到可确认的 35photo 作品图片"))
	}
	return result, errors.Join(issues...)
}

func discoverPhotography35Author(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	author := photography35Author(page)
	if getter == nil || author == "" {
		return result, errors.New("35photo 作者地址或下载客户端无效")
	}
	if page.RawQuery != "" {
		return result, errors.New("35photo 作者筛选页暂不支持，请使用作者主页")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("读取 35photo 作者页失败：%w", err)
	}
	if doc == nil || !sameOrigin(page, final) || !strings.EqualFold(photography35Author(final), author) {
		return result, errors.New("35photo 作者页已跳转，可能需要登录或验证")
	}
	return photography35AuthorCollect(ctx, getter, final, doc)
}

type photography35Card struct{ id, src, title string }

func photography35Cards(doc *html.Node, page *url.URL, author string, fragment bool) ([]photography35Card, int) {
	var cards []photography35Card
	bad := 0
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" || !photography35HasClass(n, "openPhoto") {
			return
		}
		if !fragment {
			inGallery := false
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Type == html.ElementNode && photography35HasClass(p, "profileTape") {
					inGallery = true
					break
				}
			}
			if !inGallery {
				return
			}
		}
		var photoNode *html.Node
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Type == html.ElementNode && (attribute(p, "itemtype") == "https://schema.org/Photograph" || attribute(p, "itemtype") == "http://schema.org/Photograph") {
				photoNode = p
				break
			}
		}
		if photoNode == nil {
			bad++
			return
		}
		id := attribute(n, "photo-id")
		href, err := crawlURL(page, attribute(n, "href"))
		ref := photography35PhotoRef(href)
		if err != nil || !sameOrigin(page, href) || ref.photo != id || !strings.EqualFold(ref.author, author) {
			bad++
			return
		}
		src, ok := photography35Asset(attribute(n, "href-mobile"), "display", id)
		if !ok {
			bad++
			return
		}
		title := attribute(n, "title")
		if title == "" {
			walkHTML(photoNode, func(x *html.Node) {
				if title == "" && x.Type == html.ElementNode && x.Data == "meta" && attribute(x, "itemprop") == "name" {
					title = attribute(x, "content")
				}
			})
		}
		cards = append(cards, photography35Card{id: id, src: src, title: title})
	})
	return cards, bad
}

func photography35AuthorInfo(doc *html.Node, page *url.URL) (name, user, cursor string, err error) {
	author := photography35Author(page)
	var canonical, person bool
	var users, cursors []string
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "link" && strings.EqualFold(attribute(n, "rel"), "canonical") {
			u, e := crawlURL(page, attribute(n, "href"))
			if e == nil && sameOrigin(page, u) && strings.EqualFold(photography35Author(u), author) {
				canonical = true
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
				var p struct {
					Type string `json:"@type"`
					URL  string `json:"url"`
					Name string `json:"name"`
				}
				if json.Unmarshal([]byte(text), &p) == nil && p.Type == "Person" {
					u, e := crawlURL(page, p.URL)
					if e == nil && sameOrigin(page, u) && strings.EqualFold(photography35Author(u), author) {
						person = true
						name = p.Name
					}
				}
			}
			for _, m := range offer1688CodeMatches(text, photography35NextUser) {
				users = append(users, text[m[2]:m[3]])
			}
			for _, m := range offer1688CodeMatches(text, photography35NextID) {
				cursors = append(cursors, text[m[2]:m[3]])
			}
		}
	})
	if !canonical || !person || len(users) != 1 || len(cursors) != 1 || users[0] == "0" {
		return "", "", "", errors.New("35photo 作者身份或分页信息无法确认")
	}
	return name, users[0], cursors[0], nil
}

type photography35PageResponse struct {
	Page   string      `json:"page"`
	LastID json.Number `json:"lastId"`
	Data   string      `json:"data"`
}

func photography35NextPage(ctx context.Context, getter Getter, page *url.URL, user, cursor string) (photography35PageResponse, error) {
	var result photography35PageResponse
	api := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/show_block.php", RawQuery: url.Values{"type": {"getNextPageData"}, "page": {"photoUser"}, "lastId": {cursor}, "user_id": {user}}.Encode()}
	resp, err := getter.Get(ctx, api.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", "https://35photo.pro/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, &TransferError{Err: err}
	}
	defer resp.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, api.String()))
	if err != nil || !sameOrigin(api, final) || final.Path != api.Path || final.RawQuery != api.RawQuery {
		return result, errors.New("35photo 作者分页接口发生跳转")
	}
	if resp.StatusCode != http.StatusOK {
		return result, &HTTPStatusError{URL: api.Scheme + "://" + api.Host + api.Path, StatusCode: resp.StatusCode, RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, &TransferError{Err: err}
	}
	if len(raw) > maxPageBytes {
		return result, errors.New("35photo 分页响应超过 8 MiB")
	}
	if length := contentLength(resp); length >= 0 && length != int64(len(raw)) {
		return result, &IntegrityError{Err: errors.New("35photo 分页响应长度不符")}
	}
	var input io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		z, e := gzip.NewReader(input)
		if e != nil {
			return result, &TransferError{Err: e}
		}
		defer z.Close()
		input = z
	case "deflate":
		z, e := zlib.NewReader(input)
		if e != nil {
			return result, &TransferError{Err: e}
		}
		defer z.Close()
		input = z
	default:
		return result, errors.New("35photo 分页响应压缩格式不支持")
	}
	body, err := io.ReadAll(io.LimitReader(input, maxPageBytes+1))
	if err != nil {
		return result, &TransferError{Err: err}
	}
	if len(body) > maxPageBytes {
		return result, errors.New("35photo 分页内容超过 8 MiB")
	}
	if json.Unmarshal(body, &result) != nil || result.Page != "photoUser" || !photography35Digits.MatchString(result.LastID.String()) {
		return result, errors.New("35photo 作者分页数据无法识别")
	}
	return result, nil
}

func photography35AuthorCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	author := photography35Author(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || doc == nil || author == "" || page.RawQuery != "" {
		return result, errors.New("35photo 作者页或筛选条件不受支持")
	}
	name, user, cursor, err := photography35AuthorInfo(doc, page)
	if err != nil {
		return result, err
	}
	result.Title = name
	seen := make(map[string]bool)
	var issues []error
	limited := false
	appendCards := func(cards []photography35Card, bad int) {
		if bad > 0 {
			issues = append(issues, errors.New("部分 35photo 作品与作者不匹配"))
		}
		for _, card := range cards {
			if seen[card.id] {
				continue
			}
			seen[card.id] = true
			if len(result.Items) >= photography35MaxImages {
				limited = true
				continue
			}
			result.Items = append(result.Items, Item{URL: card.src, RequireImage: true, PublicOnly: true, Headers: map[string]string{"Referer": "https://35photo.pro/"}, Metadata: map[string]string{"title": card.title, "35photo_photo_id": card.id, "35photo_author_id": user, "35photo_author": author}})
		}
	}
	cards, bad := photography35Cards(doc, page, author, false)
	appendCards(cards, bad)
	if cursor != "0" && (len(cards) == 0 || cards[len(cards)-1].id != cursor) {
		issues = append(issues, errors.New("35photo 作者首页分页游标不匹配"))
		cursor = "0"
	}
	visited := make(map[string]bool)
	for i := 0; cursor != "0" && i < photography35MaxPages; i++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if visited[cursor] {
			issues = append(issues, errors.New("35photo 作者分页游标重复"))
			break
		}
		visited[cursor] = true
		if len(result.Items) >= photography35MaxImages {
			issues = append(issues, fmt.Errorf("35photo 作者作品超过 %d 张上限", photography35MaxImages))
			break
		}
		next, err := photography35NextPage(ctx, getter, page, user, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			issues = append(issues, fmt.Errorf("35photo 作者分页未读取：%w", err))
			break
		}
		fragment, err := html.Parse(strings.NewReader(next.Data))
		if err != nil {
			issues = append(issues, errors.New("35photo 作者分页内容无法读取"))
			break
		}
		cards, bad = photography35Cards(fragment, page, author, true)
		appendCards(cards, bad)
		nextID := next.LastID.String()
		if nextID != "0" && (len(cards) == 0 || cards[len(cards)-1].id != nextID) {
			issues = append(issues, errors.New("35photo 作者分页游标不匹配"))
			break
		}
		cursor = nextID
	}
	if limited {
		issues = append(issues, fmt.Errorf("35photo 作者作品超过 %d 张上限", photography35MaxImages))
	}
	if cursor != "0" && len(visited) >= photography35MaxPages {
		issues = append(issues, fmt.Errorf("35photo 作者作品超过 %d 页上限", photography35MaxPages))
	}
	if len(result.Items) == 0 {
		issues = append(issues, errors.New("未找到可确认属于该作者的作品"))
	}
	issues = append(issues, errors.New("35photo 作者列表每个作品仅采集封面；系列作品中的其他照片需打开作品页"))
	return result, errors.Join(issues...)
}
