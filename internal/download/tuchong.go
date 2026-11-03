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
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	tuchongNumericID = regexp.MustCompile(`^[0-9]{1,20}$`)
	tuchongImagePath = regexp.MustCompile(`^/([0-9]{1,20})/(?:t|l|f)/([0-9]{1,20})\.(jpg|jpeg|png|webp)$`)
	tuchongPostState = regexp.MustCompile(`\bwindow\s*\.\s*post\s*=`)
)

// shortcut: limit author crawls while reporting incomplete results.
const (
	maxTuchongPages  = 100
	maxTuchongPosts  = 1000
	maxTuchongImages = 10000
)

type tuchongRoute struct{ site, post string }

func tuchongValidID(id string) bool {
	return tuchongNumericID.MatchString(id) && strings.Trim(id, "0") != ""
}

func tuchongRouteOf(u *url.URL) tuchongRoute {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		(!strings.EqualFold(u.Hostname(), "tuchong.com") && !strings.EqualFold(u.Hostname(), "www.tuchong.com")) ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return tuchongRoute{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 1 && tuchongValidID(parts[0]) {
		return tuchongRoute{site: parts[0]}
	}
	if len(parts) == 2 && tuchongValidID(parts[0]) && tuchongValidID(parts[1]) {
		return tuchongRoute{site: parts[0], post: parts[1]}
	}
	return tuchongRoute{}
}

func isTuchongPage(u *url.URL) bool { return tuchongRouteOf(u).site != "" }

func tuchongImageURL(page *url.URL, raw string) (string, bool) {
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || !strings.EqualFold(u.Hostname(), "photo.tuchong.com") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return "", false
	}
	m := tuchongImagePath.FindStringSubmatch(strings.ToLower(u.Path))
	if len(m) != 4 || !tuchongValidID(m[1]) || !tuchongValidID(m[2]) {
		return "", false
	}
	u.Scheme, u.Host = "https", "photo.tuchong.com"
	u.Path, u.RawPath = "/"+m[1]+"/f/"+m[2]+"."+m[3], ""
	return u.String(), true
}

func tuchongRawID(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return "!invalid"
}

func discoverTuchong(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	route := tuchongRouteOf(root)
	if route.site == "" || getter == nil {
		return Discovery{IsPage: true}, errors.New("图虫作品或作者地址无效")
	}
	if route.post != "" {
		return tuchongPost(ctx, getter, root, route)
	}
	return tuchongAuthor(ctx, getter, root, route.site)
}

func tuchongPost(ctx context.Context, getter Getter, root *url.URL, route tuchongRoute) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	requestPage := *root
	if _, ok := getter.(*HTTPGetter); ok {
		requestPage.Scheme, requestPage.Host = "https", "tuchong.com"
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: requestPage.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("读取图虫作品失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&requestPage, final) || tuchongRouteOf(final) != route {
		return result, errors.New("图虫作品跳转到其他页面，可能需要登录或验证")
	}
	return tuchongPostPage(ctx, doc, final)
}

func tuchongPostPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	route := tuchongRouteOf(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if route.post == "" || doc == nil {
		return result, errors.New("图虫作品页面无效")
	}
	var mismatch bool
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			for _, at := range offer1688CodeMatches(child.Data, tuchongPostState) {
				tail := strings.TrimSpace(child.Data[at[1]:])
				decoder := json.NewDecoder(strings.NewReader(tail))
				var state struct {
					PostID json.RawMessage `json:"post_id"`
					SiteID json.RawMessage `json:"site_id"`
				}
				if decoder.Decode(&state) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					continue
				}
				if id := tuchongRawID(state.PostID); id != "" && id != route.post {
					mismatch = true
				}
				if id := tuchongRawID(state.SiteID); id != "" && id != route.site {
					mismatch = true
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if mismatch {
		return result, errors.New("图虫页面作品 ID 与请求不匹配")
	}
	seen := make(map[string]bool)
	invalid := 0
	limited := false
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || limited || n.Type != html.ElementNode || n.Data != "img" {
			return
		}
		content := false
		for parent := n; parent != nil; parent = parent.Parent {
			for _, name := range strings.Fields(attribute(parent, "class")) {
				if name == "post-content" || name == "multi-photo-image" {
					content = true
				}
			}
		}
		if !content {
			return
		}
		image, ok := tuchongImageURL(page, attribute(n, "src"))
		if !ok {
			invalid++
			return
		}
		if seen[image] {
			return
		}
		if len(result.Items) >= maxTuchongImages {
			limited = true
			return
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"tuchong_site_id": route.site, "tuchong_post_id": route.post}}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 张图虫作品图片地址无效，结果可能不完整", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("图虫作品超过 %d 张图片，结果不完整", maxTuchongImages))
	}
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("图虫作品未返回图片，可能需要登录或验证"))
	}
	return result, errors.Join(problems...)
}

type tuchongPosts struct {
	PostList []struct {
		PostID json.RawMessage `json:"post_id"`
	} `json:"post_list"`
	More   *bool           `json:"more"`
	Before json.RawMessage `json:"before_timestamp"`
	Result string          `json:"result"`
}

func tuchongFeed(ctx context.Context, getter Getter, origin *url.URL, site string, page int, before string) (tuchongPosts, error) {
	var out tuchongPosts
	if err := ctx.Err(); err != nil {
		return out, err
	}
	query := url.Values{"count": {"20"}, "page": {strconv.Itoa(page)}, "before_timestamp": {before}}
	endpoint := &url.URL{Scheme: origin.Scheme, Host: origin.Host, Path: "/rest/2/sites/" + site + "/posts", RawQuery: query.Encode()}
	response, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", origin.Scheme+"://"+origin.Host+"/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, &TransferError{Err: err}
	}
	defer response.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path {
		return out, errors.New("图虫列表跳转到其他页面，可能需要登录或验证")
	}
	if response.StatusCode != http.StatusOK {
		return out, &HTTPStatusError{URL: final.String(), StatusCode: response.StatusCode, RetryAfter: ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	var reader io.Reader = io.LimitReader(response.Body, maxPageBytes+1)
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		dec, err := gzip.NewReader(reader)
		if err != nil {
			return out, &TransferError{Err: err}
		}
		defer dec.Close()
		reader = dec
	case "deflate":
		dec, err := zlib.NewReader(reader)
		if err != nil {
			return out, &TransferError{Err: err}
		}
		defer dec.Close()
		reader = dec
	default:
		return out, errors.New("不支持图虫列表压缩格式")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	if err != nil {
		return out, &TransferError{Err: err}
	}
	if len(data) > maxPageBytes {
		return out, errors.New("图虫列表超过 8 MiB 上限")
	}
	if json.Unmarshal(data, &out) != nil || out.Result != "SUCCESS" || out.PostList == nil || out.More == nil {
		return out, errors.New("图虫列表响应无效，可能需要登录或验证")
	}
	return out, nil
}

func tuchongAuthor(ctx context.Context, getter Getter, root *url.URL, site string) (Discovery, error) {
	result := Discovery{IsPage: true, Title: "图虫 " + site}
	if !tuchongValidID(site) {
		return result, errors.New("图虫作者 ID 无效")
	}
	origin := &url.URL{Scheme: root.Scheme, Host: root.Host}
	if _, ok := getter.(*HTTPGetter); ok {
		origin.Scheme, origin.Host = "https", "tuchong.com"
	}
	seenPosts, seenImages := map[string]bool{}, map[string]bool{}
	var problems []error
	failed := 0
	var firstErr error
	finish := func(extra error) (Discovery, error) {
		all := append([]error(nil), problems...)
		if failed > 0 {
			all = append(all, fmt.Errorf("%d 个图虫作品读取失败或不完整: %w", failed, firstErr))
		}
		if len(result.Items) == 0 && extra == nil {
			all = append(all, errors.New("图虫作者未返回可下载的作品图片"))
		}
		return result, errors.Join(append(all, extra)...)
	}
	before := "0"
	for number := 1; number <= maxTuchongPages; number++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		feed, err := tuchongFeed(ctx, getter, origin, site, number, before)
		if err != nil {
			return finish(fmt.Errorf("读取图虫作者列表失败: %w", err))
		}
		nextBefore := before
		if number == 1 && *feed.More {
			nextBefore = tuchongRawID(feed.Before)
		}
		newPosts := 0
		for _, row := range feed.PostList {
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			id := tuchongRawID(row.PostID)
			if !tuchongValidID(id) {
				problems = append(problems, errors.New("图虫作者列表含无效作品 ID"))
				continue
			}
			if seenPosts[id] {
				continue
			}
			if len(seenPosts) >= maxTuchongPosts {
				return finish(fmt.Errorf("图虫作者超过 %d 个作品，结果不完整", maxTuchongPosts))
			}
			seenPosts[id] = true
			newPosts++
			page := &url.URL{Scheme: origin.Scheme, Host: origin.Host, Path: "/" + site + "/" + id + "/"}
			work, err := tuchongPost(ctx, getter, page, tuchongRoute{site: site, post: id})
			for _, item := range work.Items {
				if seenImages[item.URL] {
					continue
				}
				if len(result.Items) >= maxTuchongImages {
					return finish(fmt.Errorf("图虫作者超过 %d 张图片，结果不完整", maxTuchongImages))
				}
				seenImages[item.URL] = true
				item.Metadata["album_id"], item.Metadata["album_title"] = id, work.Title
				result.Items = append(result.Items, item)
			}
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("作品 %s: %w", id, err)
				}
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				var status *HTTPStatusError
				if errors.As(err, &status) && (status.StatusCode == 401 || status.StatusCode == 403 || status.StatusCode == 429) {
					return finish(err)
				}
			}
		}
		if !*feed.More {
			return finish(nil)
		}
		if !tuchongValidID(nextBefore) {
			return finish(errors.New("图虫分页缺少有效游标，结果不完整"))
		}
		before = nextBefore
		if newPosts == 0 {
			return finish(errors.New("图虫作者分页为空或重复，结果不完整"))
		}
	}
	return finish(fmt.Errorf("图虫作者超过 %d 页，结果不完整", maxTuchongPages))
}
