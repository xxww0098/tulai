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
	douyinContentIDPattern = regexp.MustCompile(`^[1-9][0-9]{16,21}$`)
	douyinRouterStart      = regexp.MustCompile(`\bwindow\s*\.\s*_ROUTER_DATA\s*=\s*`)
	douyinPaceStart        = regexp.MustCompile(`\bself\s*\.\s*__pace_f\s*\.\s*push\s*\(\s*`)
	douyinAwemeStart       = regexp.MustCompile(`"aweme"\s*:\s*`)
	douyinImageHost        = regexp.MustCompile(`^p[0-9]{1,3}(?:-[a-z0-9-]+)?\.douyinpic\.com$`)
)

// shortcut: bound anomalously large albums until the limit is configurable.
const maxDouyinContentImages = 10000

func douyinContentID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return ""
	}
	var prefix string
	switch strings.ToLower(u.Hostname()) {
	case "www.douyin.com":
		for _, p := range []string{"/note/", "/video/"} {
			if strings.HasPrefix(u.Path, p) {
				prefix = p
				break
			}
		}
	case "www.iesdouyin.com":
		for _, p := range []string{"/share/note/", "/share/video/"} {
			if strings.HasPrefix(u.Path, p) {
				prefix = p
				break
			}
		}
	}
	if prefix == "" {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(u.Path, prefix), "/")
	if !douyinContentIDPattern.MatchString(id) {
		return ""
	}
	return id
}

func isDouyinContent(u *url.URL) bool { return douyinContentID(u) != "" }

type douyinContentRedirectGetter struct {
	base Getter
	id   string
}

func (g *douyinContentRedirectGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	current, err := crawlURL(nil, raw)
	if err != nil || current.Scheme != "https" || douyinContentID(current) != g.id {
		return nil, errors.New("抖音作品地址无效")
	}
	for i := 0; i < 6; i++ {
		resp, err := g.base.Get(ctx, current.String(), prepare)
		if err != nil {
			return nil, err
		}
		final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, current.String()))
		if err != nil || !sameOrigin(current, final) || douyinContentID(final) != g.id {
			resp.Body.Close()
			return nil, errors.New("抖音作品响应地址无效")
		}
		if resp.StatusCode != 301 && resp.StatusCode != 302 && resp.StatusCode != 303 && resp.StatusCode != 307 && resp.StatusCode != 308 {
			return resp, nil
		}
		location := resp.Header.Get("Location")
		resp.Body.Close()
		next, err := crawlURL(current, location)
		if err != nil || !sameOrigin(current, next) || douyinContentID(next) != g.id {
			return nil, errors.New("抖音作品已跳转，可能需要登录或安全验证")
		}
		if next.String() == current.String() {
			return nil, errors.New("抖音作品重定向循环")
		}
		current = next
	}
	return nil, errors.New("抖音作品重定向次数过多")
}

func discoverDouyinContent(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := douyinContentID(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || getter == nil {
		return result, errors.New("抖音作品地址或下载客户端无效")
	}
	page, pageGetter := *root, getter
	page.RawQuery, page.Fragment = "", ""
	if source, ok := getter.(*HTTPGetter); ok {
		if source == nil || source.client == nil {
			return result, errors.New("抖音下载客户端无效")
		}
		clone, client := *source, *source.client
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		client.Jar = nil
		clone.client = &client
		page.Scheme, page.Host = "https", strings.ToLower(page.Hostname())
		pageGetter = &douyinContentRedirectGetter{base: &clone, id: id}
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
				return result, fmt.Errorf("抖音作品可能需要登录或安全验证: %w", err)
			case http.StatusTooManyRequests:
				return result, fmt.Errorf("抖音请求受到限流，请稍后重试: %w", err)
			}
		}
		return result, fmt.Errorf("读取抖音作品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || douyinContentID(final) != id {
		return result, errors.New("抖音作品页面未加载或已跳转，可能需要登录或安全验证")
	}
	return douyinContentPage(ctx, doc, final)
}

type douyinContentWork struct {
	ID     json.Number       `json:"aweme_id"`
	WebID  json.Number       `json:"awemeId"`
	Desc   string            `json:"desc"`
	Images []json.RawMessage `json:"images"`
	Status struct {
		Private      *int  `json:"private_status"`
		PartSee      *int  `json:"part_see"`
		IsDeleted    *bool `json:"is_delete"`
		IsProhibited *bool `json:"is_prohibited"`
	} `json:"status"`
}

func douyinContentImageURL(raw string) (string, bool) {
	if len(raw) > 16384 {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Scheme != "https" || !douyinImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || u.Path == "" || u.Path == "/" || strings.Contains(u.Path, "..") {
		return "", false
	}
	u.Fragment = ""
	return u.String(), true
}

func douyinContentPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := douyinContentID(page)
	if doc == nil || id == "" {
		return result, errors.New("抖音作品页面无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var candidates []douyinContentWork
	var stream strings.Builder
	var challenge, malformed bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		style := strings.ReplaceAll(strings.ToLower(attribute(n, "style")), " ", "")
		hidden := strings.Contains(style, "display:none") || attribute(n, "aria-hidden") == "true"
		if !hidden {
			if attribute(n, "id") == "web-login-container" {
				challenge = true
			}
			for _, class := range strings.Fields(attribute(n, "class")) {
				if strings.HasPrefix(class, "captcha_verify_bar--title") {
					challenge = true
				}
			}
		}
		if n.Data != "script" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.TextNode || len(c.Data) > maxPageBytes {
				continue
			}
			if m := douyinRouterStart.FindStringIndex(c.Data); m != nil {
				var state struct {
					LoaderData map[string]struct {
						VideoInfoRes struct {
							Items []douyinContentWork `json:"item_list"`
						} `json:"videoInfoRes"`
					} `json:"loaderData"`
				}
				if err := json.NewDecoder(strings.NewReader(c.Data[m[1]:])).Decode(&state); err != nil {
					malformed = true
				} else {
					for _, key := range []string{"video_(id)/page", "note_(id)/page"} {
						for _, work := range state.LoaderData[key].VideoInfoRes.Items {
							if work.ID.String() == id {
								candidates = append(candidates, work)
							}
						}
					}
				}
			}
			for _, m := range douyinPaceStart.FindAllStringIndex(c.Data, -1) {
				var args []json.RawMessage
				if err := json.NewDecoder(strings.NewReader(c.Data[m[1]:])).Decode(&args); err != nil {
					malformed = true
					continue
				}
				if len(args) != 2 || string(args[0]) != "1" {
					continue
				}
				var payload string
				if err := json.Unmarshal(args[1], &payload); err != nil || stream.Len()+len(payload) > maxPageBytes {
					malformed = true
				} else {
					stream.WriteString(payload)
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if challenge {
		return result, errors.New("抖音需要登录或完成安全验证")
	}
	text := stream.String()
	for _, m := range douyinAwemeStart.FindAllStringIndex(text, -1) {
		var envelope struct {
			Detail douyinContentWork `json:"detail"`
		}
		if json.NewDecoder(strings.NewReader(text[m[1]:])).Decode(&envelope) == nil && envelope.Detail.WebID.String() == id {
			candidates = append(candidates, envelope.Detail)
		}
	}
	if len(candidates) != 1 {
		return result, errors.New("抖音作品数据未加载或无法确认归属，可能需要登录或安全验证")
	}
	work := candidates[0]
	if (work.Status.Private != nil && *work.Status.Private != 0) || (work.Status.PartSee != nil && *work.Status.PartSee != 0) ||
		(work.Status.IsDeleted != nil && *work.Status.IsDeleted) || (work.Status.IsProhibited != nil && *work.Status.IsProhibited) {
		return result, errors.New("抖音作品不可用或未公开")
	}
	if len(work.Images) == 0 {
		return result, errors.New("抖音作品没有已加载的图集图片，可能是视频或需要登录")
	}
	result.Title = strings.TrimSpace(work.Desc)
	seen := make(map[string]bool)
	var invalid, limited, live bool
	for _, raw := range work.Images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var img struct {
			URLs    []string        `json:"url_list"`
			WebURLs []string        `json:"urlList"`
			Video   json.RawMessage `json:"video"`
		}
		if err := json.Unmarshal(raw, &img); err != nil {
			invalid = true
			continue
		}
		urls := img.URLs
		if len(urls) == 0 {
			urls = img.WebURLs
		}
		if len(img.Video) > 0 && string(img.Video) != "null" {
			live = true
		}
		if len(urls) == 0 {
			invalid = true
			continue
		}
		image, ok := douyinContentImageURL(urls[len(urls)-1])
		if !ok {
			invalid = true
			continue
		}
		if seen[image] {
			continue
		}
		if len(result.Items) >= maxDouyinContentImages {
			limited = true
			break
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"douyin_aweme_id": id, "image_kind": "atlas"}}
		if ref := imageReferer("https://"+page.Hostname()+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	var problems []error
	if len(result.Items) == 0 || invalid || malformed {
		problems = append(problems, errors.New("抖音图集部分图片未加载或来源未验证，结果可能不完整"))
	}
	if live {
		problems = append(problems, errors.New("抖音实况图仅采集静态图片，动态部分未实现"))
	}
	if limited {
		problems = append(problems, fmt.Errorf("抖音图集超过 %d 张图片，结果不完整", maxDouyinContentImages))
	}
	return result, errors.Join(problems...)
}
