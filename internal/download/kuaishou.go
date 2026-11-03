package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	kuaishouIDPattern    = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	kuaishouStateStart   = regexp.MustCompile(`\bwindow\s*\.\s*INIT_STATE\s*=\s*`)
	kuaishouAtlasCDN     = regexp.MustCompile(`^p[0-9]{1,3}\.a\.yximgs\.com$`)
	kuaishouAtlasPicture = regexp.MustCompile(`^/ufile/atlas/[A-Za-z0-9_=-]{1,300}\.(?:jpg|jpeg|png|webp)$`)
)

// shortcut: bound anomalously large albums until the limit is configurable.
const maxKuaishouImages = 10000

func kuaishouPostID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return ""
	}
	var prefix string
	switch strings.ToLower(u.Hostname()) {
	case "www.kuaishou.com":
		prefix = "/short-video/"
	case "m.gifshow.com":
		prefix = "/fw/photo/"
	default:
		return ""
	}
	id := strings.TrimPrefix(u.Path, prefix)
	if id == u.Path || !kuaishouIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func isKuaishouPost(u *url.URL) bool { return kuaishouPostID(u) != "" }

func kuaishouMobileURL(id string) *url.URL {
	return &url.URL{Scheme: "https", Host: "m.gifshow.com", Path: "/fw/photo/" + id}
}

type kuaishouRedirectGetter struct {
	base Getter
	id   string
}

func (g *kuaishouRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	want := kuaishouMobileURL(g.id)
	if err != nil || !sameOrigin(current, want) || kuaishouPostID(current) != g.id {
		return nil, errors.New("快手分享地址无效")
	}
	for i := 0; i < 6; i++ {
		resp, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, current.String()))
		if err != nil || !sameOrigin(want, final) || kuaishouPostID(final) != g.id {
			resp.Body.Close()
			return nil, errors.New("快手作品响应地址无效")
		}
		if resp.StatusCode != 301 && resp.StatusCode != 302 && resp.StatusCode != 303 && resp.StatusCode != 307 && resp.StatusCode != 308 {
			return resp, nil
		}
		location := resp.Header.Get("Location")
		resp.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || !sameOrigin(want, next) || kuaishouPostID(next) != g.id {
			return nil, errors.New("快手作品已跳转，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("快手作品重定向循环")
		}
		current = next
	}
	return nil, errors.New("快手作品重定向次数过多")
}

// discoverKuaishouPost obtains only the fixed public mobile share page.
func discoverKuaishouPost(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := kuaishouPostID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || getter == nil {
		return result, errors.New("快手作品地址或下载客户端无效")
	}
	return kuaishouFetchMobile(ctx, getter, id)
}

func kuaishouFetchMobile(ctx context.Context, getter Getter, id string) (Discovery, error) {
	result := Discovery{IsPage: true}
	page := kuaishouMobileURL(id)
	pageGetter := getter
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("快手下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		pageGetter = &kuaishouRedirectGetter{base: &clone, id: id}
	}
	doc, final, _, resp, err := fetchCrawlPage(ctx, pageGetter, crawlPage{url: page.String(), depth: 1})
	if resp != nil {
		resp.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		var status *HTTPStatusError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return result, fmt.Errorf("快手作品可能需要登录或安全验证: %w", err)
			case http.StatusTooManyRequests:
				return result, fmt.Errorf("快手请求受到限流，请稍后重试: %w", err)
			}
		}
		return result, fmt.Errorf("读取快手作品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(page, final) || kuaishouPostID(final) != id {
		return result, errors.New("快手分享页未加载或已跳转，可能需要登录或安全验证")
	}
	return kuaishouMobilePage(ctx, doc, final)
}

func kuaishouHasChallenge(doc *html.Node) bool {
	var challenge bool
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		for _, class := range strings.Fields(attribute(n, "class")) {
			if strings.HasPrefix(class, "slider-bg-tip") {
				challenge = true
			}
		}
		if n.Data == "title" && strings.Contains(nodeText(n), "安全验证") {
			challenge = true
		}
	})
	return challenge
}

// kuaishouCollect preserves the current browser page and uses a fixed public
// share URL for PC works, so RenderedSession does not send PC cookies there.
func kuaishouCollect(ctx context.Context, getter Getter, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := kuaishouPostID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || doc == nil || getter == nil {
		return result, errors.New("快手作品页面无效")
	}
	if kuaishouHasChallenge(doc) {
		return result, errors.New("快手需要完成安全验证")
	}
	if strings.EqualFold(page.Hostname(), "m.gifshow.com") {
		return kuaishouMobilePage(ctx, doc, page)
	}
	return kuaishouFetchMobile(ctx, getter, id)
}

type kuaishouStateEntry struct {
	Result int `json:"result"`
	Photo  struct {
		ID        string `json:"photoId"`
		ShareInfo string `json:"share_info"`
		PhotoType string `json:"photoType"`
		Status    *int   `json:"photoStatus"`
		Caption   string `json:"caption"`
	} `json:"photo"`
	Atlas struct {
		CDN  []string          `json:"cdn"`
		List []json.RawMessage `json:"list"`
	} `json:"atlas"`
}

func kuaishouMobilePage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := kuaishouPostID(page)
	if doc == nil || id == "" || !strings.EqualFold(page.Hostname(), "m.gifshow.com") {
		return result, errors.New("快手分享页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if kuaishouHasChallenge(doc) {
		return result, errors.New("快手需要完成安全验证")
	}
	var entries []kuaishouStateEntry
	var malformed bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode || len(c.Data) > maxPageBytes {
				continue
			}
			match := kuaishouStateStart.FindStringIndex(c.Data)
			if match == nil {
				continue
			}
			var root map[string]json.RawMessage
			if err := json.NewDecoder(strings.NewReader(c.Data[match[1]:])).Decode(&root); err != nil {
				malformed = true
				continue
			}
			for _, raw := range root {
				var entry kuaishouStateEntry
				if err := json.Unmarshal(raw, &entry); err != nil {
					continue
				}
				share, err := url.ParseQuery(entry.Photo.ShareInfo)
				if err == nil && len(share["photoId"]) == 1 && share.Get("photoId") == id {
					entries = append(entries, entry)
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(entries) != 1 || malformed {
		return result, errors.New("快手分享页缺少唯一匹配的作品信息，可能需要登录或安全验证")
	}
	entry := entries[0]
	if entry.Result != 1 || entry.Photo.ID == "" || entry.Photo.Status == nil || *entry.Photo.Status != 0 {
		return result, errors.New("快手作品不可用或未公开")
	}
	if entry.Photo.PhotoType == "VIDEO" {
		return result, errors.New("快手当前作品是视频，没有可采集的图集图片")
	}
	if entry.Photo.PhotoType != "HORIZONTAL_ATLAS" {
		return result, errors.New("快手当前作品类型尚未验证，无法确认图集")
	}
	var host string
	for _, candidate := range entry.Atlas.CDN {
		candidate = strings.ToLower(candidate)
		if kuaishouAtlasCDN.MatchString(candidate) {
			host = candidate
			break
		}
	}
	if host == "" || len(entry.Atlas.List) == 0 {
		return result, errors.New("快手图集图片未加载，可能需要登录或安全验证")
	}
	result.Title = strings.TrimSpace(entry.Photo.Caption)
	seen := make(map[string]bool)
	var invalid, limited bool
	for _, raw := range entry.Atlas.List {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var path string
		if err := json.Unmarshal(raw, &path); err != nil || !kuaishouAtlasPicture.MatchString(path) {
			invalid = true
			continue
		}
		image := "https://" + host + path
		if seen[image] {
			continue
		}
		if len(result.Items) >= maxKuaishouImages {
			limited = true
			break
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"kuaishou_photo_id": id, "image_kind": "atlas"}}
		if ref := imageReferer("https://m.gifshow.com/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	var problems []error
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("快手图集中没有可验证的图片"))
	} else {
		problems = append(problems, errors.New("快手分享页的图片可能经压缩，未确认原图"))
	}
	if invalid {
		problems = append(problems, errors.New("快手图集包含未验证的图片地址，已跳过"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("快手图集超过 %d 张图片，结果不完整", maxKuaishouImages))
	}
	return result, errors.Join(problems...)
}
