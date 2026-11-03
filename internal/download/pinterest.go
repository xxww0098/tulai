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
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	pinterestIDPattern   = regexp.MustCompile(`^[0-9]{5,30}$`)
	pinterestNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,100}$`)
	pinterestSlugPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)
	pinterestSizePattern = regexp.MustCompile(`^[0-9]{2,4}x[0-9]{0,4}$`)
)

type pinterestRef struct{ pin, owner, board string }
type pinterestImage struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
}
type pinterestPin struct {
	ID     string                    `json:"id"`
	Type   string                    `json:"type"`
	Images map[string]pinterestImage `json:"images"`
}
type pinterestResourceEntry struct {
	Data         json.RawMessage `json:"data"`
	NextBookmark string          `json:"nextBookmark"`
}
type pinterestBoard struct {
	ID           string `json:"id"`
	URL          string `json:"url"`
	Name         string `json:"name"`
	PinCount     *int   `json:"pin_count"`
	SectionCount int    `json:"section_count"`
}
type pinterestState struct {
	InitialReduxState struct {
		Pins      map[string]pinterestPin `json:"pins"`
		Resources struct {
			Boards map[string]pinterestResourceEntry `json:"BoardResource"`
			Feeds  map[string]pinterestResourceEntry `json:"BoardFeedResource"`
		} `json:"resources"`
	} `json:"initialReduxState"`
}

func pinterestHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return false
	}
	p := u.Port()
	if p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	switch h {
	case "pinterest.com", "www.pinterest.com", "pinterest.fr", "www.pinterest.fr", "pinterest.de", "www.pinterest.de", "pinterest.co.uk", "www.pinterest.co.uk":
		return true
	}
	if !strings.HasSuffix(h, ".pinterest.com") {
		return false
	}
	prefix := strings.TrimSuffix(h, ".pinterest.com")
	return len(prefix) == 2 && prefix[0] >= 'a' && prefix[0] <= 'z' && prefix[1] >= 'a' && prefix[1] <= 'z'
}

func pinterestPageRef(u *url.URL) pinterestRef {
	if !pinterestHost(u) {
		return pinterestRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return pinterestRef{}
	}
	if parts[0] == "pin" {
		id := parts[1]
		if pos := strings.LastIndex(id, "--"); pos >= 0 {
			id = id[pos+2:]
		}
		if pinterestIDPattern.MatchString(id) {
			return pinterestRef{pin: id}
		}
		return pinterestRef{}
	}
	if !pinterestNamePattern.MatchString(parts[0]) || !pinterestSlugPattern.MatchString(parts[1]) {
		return pinterestRef{}
	}
	switch strings.ToLower(parts[0]) {
	case "about", "business", "explore", "ideas", "login", "logout", "messages", "notifications", "pin", "resource", "search", "settings", "signup":
		return pinterestRef{}
	}
	return pinterestRef{owner: strings.ToLower(parts[0]), board: strings.ToLower(parts[1])}
}

func isPinterestPage(u *url.URL) bool {
	ref := pinterestPageRef(u)
	return ref.pin != "" || ref.board != ""
}

func discoverPinterest(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	want := pinterestPageRef(root)
	if getter == nil || (want.pin == "" && want.board == "") {
		return result, errors.New("Pinterest 页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || pinterestPageRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Pinterest 页面不可用或跳转到了其他内容")
	}
	return collectPinterest(ctx, getter, final, doc)
}

func pinterestImageURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "i.pinimg.com") || (u.Port() != "" && u.Port() != "443") || u.Path == "" || path.Clean(u.Path) != u.Path || strings.Contains(u.Path, "//") {
		return nil, false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 2 || (parts[0] != "originals" && !pinterestSizePattern.MatchString(parts[0])) {
		return nil, false
	}
	return u, true
}

func pinterestItem(page *url.URL, raw string) (Item, bool) {
	image, ok := pinterestImageURL(raw)
	if !ok {
		return Item{}, false
	}
	item := Item{URL: image.String()}
	parts := strings.Split(strings.TrimPrefix(image.Path, "/"), "/")
	if parts[0] != "originals" && image.RawQuery == "" {
		original := *image
		parts[0] = "originals"
		original.Path = "/" + strings.Join(parts, "/")
		original.RawPath = ""
		item.URL = original.String()
		item.AlternateURLs = []string{image.String()}
	}
	if ref := imageReferer(page.String(), item.URL); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	return item, true
}

func pinterestPinItem(page *url.URL, pin pinterestPin) (Item, bool) {
	if image, ok := pin.Images["orig"]; ok {
		if item, valid := pinterestItem(page, image.URL); valid {
			return item, true
		}
	}
	var best pinterestImage
	for _, image := range pin.Images {
		if _, valid := pinterestImageURL(image.URL); valid && (best.URL == "" || image.Width > best.Width) {
			best = image
		}
	}
	return pinterestItem(page, best.URL)
}

// collectPinterest only uses a pin's own canonical metadata or a board's own
// feed; recommendations, avatars and other page images are never enumerated.
func collectPinterest(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref := pinterestPageRef(page)
	if doc == nil || getter == nil || (ref.pin == "" && ref.board == "") {
		return result, errors.New("Pinterest 页面无效")
	}
	var canonical, cover string
	var state *pinterestState
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
		if n.Data == "script" && attribute(n, "id") == "__PWS_INITIAL_PROPS__" && state == nil {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					var parsed pinterestState
					if json.Unmarshal([]byte(c.Data), &parsed) == nil {
						state = &parsed
					}
					break
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if ref.pin != "" {
		if state != nil {
			if pin, ok := state.InitialReduxState.Pins[ref.pin]; ok && pin.ID == ref.pin {
				if item, valid := pinterestPinItem(page, pin); valid {
					result.Items = append(result.Items, item)
					return result, nil
				}
			}
		}
		cu, err := crawlURL(page, canonical)
		if err != nil || pinterestPageRef(cu).pin != ref.pin {
			return result, errors.New("Pinterest Pin 数据尚未加载，可能需要登录或验证")
		}
		if item, ok := pinterestItem(page, cover); ok {
			result.Items = append(result.Items, item)
			return result, nil
		}
		return result, errors.New("Pinterest Pin 未返回可信图片")
	}
	if state == nil {
		return result, errors.New("Pinterest 画板数据尚未加载，可能需要登录或验证")
	}
	var board pinterestBoard
	for _, entry := range state.InitialReduxState.Resources.Boards {
		var candidate pinterestBoard
		if json.Unmarshal(entry.Data, &candidate) != nil || !pinterestIDPattern.MatchString(candidate.ID) {
			continue
		}
		u, err := crawlURL(page, candidate.URL)
		if err == nil && pinterestPageRef(u) == ref {
			board = candidate
			break
		}
	}
	if board.ID == "" {
		return result, errors.New("Pinterest 画板身份无法确认，可能需要登录或验证")
	}
	result.Title = firstNonEmpty(board.Name, result.Title)
	keys := make([]string, 0, len(state.InitialReduxState.Resources.Feeds))
	for k := range state.InitialReduxState.Resources.Feeds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var feed pinterestResourceEntry
	found := false
	for _, key := range keys {
		if pinterestFeedBoardID(key) == board.ID {
			feed = state.InitialReduxState.Resources.Feeds[key]
			found = true
			break
		}
	}
	if !found {
		return result, errors.New("Pinterest 画板图片尚未加载，可能需要登录或验证")
	}
	var first []pinterestPin
	if json.Unmarshal(feed.Data, &first) != nil {
		return result, errors.New("Pinterest 画板数据格式无效")
	}
	seen := map[string]bool{}
	add := func(pins []pinterestPin) {
		for _, pin := range pins {
			if pin.Type != "pin" {
				continue
			}
			if item, ok := pinterestPinItem(page, pin); ok && !seen[item.URL] {
				seen[item.URL] = true
				result.Items = append(result.Items, item)
			}
		}
	}
	add(first)
	var problems []error
	if board.SectionCount > 0 {
		problems = append(problems, errors.New("Pinterest 画板含分区，分区图片可能不完整"))
	}
	bookmark := feed.NextBookmark
	if bookmark == "" && board.PinCount != nil && *board.PinCount > len(result.Items) {
		problems = append(problems, errors.New("Pinterest 画板分页信息缺失，结果不完整"))
	}
	seenBookmarks := map[string]bool{}
	for pages := 0; bookmark != "" && bookmark != "-end-"; pages++ {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(problems, err)...)
		}
		if pages >= maxCrawlPages || len(result.Items) >= 10000 {
			return result, errors.Join(append(problems, errors.New("Pinterest 画板超过采集上限，结果不完整"))...)
		}
		if len(bookmark) > 4096 || seenBookmarks[bookmark] {
			return result, errors.Join(append(problems, errors.New("Pinterest 画板分页游标无效，结果不完整"))...)
		}
		seenBookmarks[bookmark] = true
		pins, next, err := pinterestNextPage(ctx, getter, page, board, bookmark)
		if err != nil {
			return result, errors.Join(append(problems, err)...)
		}
		add(pins)
		if next == "" {
			return result, errors.Join(append(problems, errors.New("Pinterest 画板后续分页信息缺失，结果不完整"))...)
		}
		bookmark = next
	}
	if board.PinCount != nil && *board.PinCount > len(result.Items) {
		problems = append(problems, errors.New("Pinterest 画板部分图片未能取得"))
	}
	return result, errors.Join(problems...)
}

func pinterestFeedBoardID(key string) string {
	var pairs [][]json.RawMessage
	if json.Unmarshal([]byte(key), &pairs) != nil {
		return ""
	}
	for _, pair := range pairs {
		if len(pair) != 2 {
			continue
		}
		var name, id string
		if json.Unmarshal(pair[0], &name) == nil && name == "board_id" && json.Unmarshal(pair[1], &id) == nil {
			return id
		}
	}
	return ""
}

func pinterestNextPage(ctx context.Context, getter Getter, page *url.URL, board pinterestBoard, bookmark string) ([]pinterestPin, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	ref := pinterestPageRef(page)
	boardPath := "/" + ref.owner + "/" + ref.board + "/"
	payload := map[string]any{"options": map[string]any{"access": []any{}, "board_id": board.ID, "board_url": boardPath, "field_set_key": "react_grid_pin", "filter_section_pins": true, "layout": "default", "page_size": 25, "bookmarks": []string{bookmark}}, "context": map[string]any{}}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}
	api := *page
	api.User = nil
	api.Path = "/resource/BoardFeedResource/get/"
	api.RawPath = ""
	api.Fragment = ""
	api.ForceQuery = false
	api.RawQuery = url.Values{"source_url": {boardPath}, "data": {string(data)}, "_": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}.Encode()
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
			return nil, "", ctx.Err()
		}
		return nil, "", errors.New("Pinterest 画板后续页不可用，结果不完整")
	}
	if resp == nil || resp.Body == nil {
		return nil, "", errors.New("Pinterest 画板后续页未返回数据，结果不完整")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.FinalURL != endpoint {
		return nil, "", fmt.Errorf("Pinterest 画板后续页不可用（HTTP %d），可能需要登录或验证", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) > maxPageBytes {
		return nil, "", errors.New("Pinterest 画板后续页超过大小上限")
	}
	var envelope struct {
		ResourceResponse struct {
			Data     json.RawMessage `json:"data"`
			Bookmark string          `json:"bookmark"`
		} `json:"resource_response"`
		Resource struct {
			Options struct {
				Bookmarks []string `json:"bookmarks"`
			} `json:"options"`
		} `json:"resource"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil, "", errors.New("Pinterest 画板后续页格式无效")
	}
	var pins []pinterestPin
	if len(envelope.ResourceResponse.Data) == 0 || json.Unmarshal(envelope.ResourceResponse.Data, &pins) != nil {
		return nil, "", errors.New("Pinterest 画板后续页没有图片数据")
	}
	next := envelope.ResourceResponse.Bookmark
	if next == "" && len(envelope.Resource.Options.Bookmarks) > 0 {
		next = envelope.Resource.Options.Bookmarks[0]
	}
	return pins, next, nil
}
