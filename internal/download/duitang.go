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
	duitangID     = regexp.MustCompile(`^[0-9]{1,20}$`)
	duitangCursor = regexp.MustCompile(`^\[[0-9]+(?:,[0-9]+)*\]$`)
	duitangCount  = regexp.MustCompile(`([0-9]+)\s*张图片`)
)

// shortcut: cap album pagination; expose these limits if larger albums need it.
const (
	maxDuitangPages  = 100
	maxDuitangImages = 10000
)

type duitangRoute struct{ kind, id string }

func duitangRouteOf(u *url.URL) duitangRoute {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(!strings.EqualFold(u.Hostname(), "www.duitang.com") && !strings.EqualFold(u.Hostname(), "duitang.com")) ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return duitangRoute{}
	}
	kind := strings.Trim(u.Path, "/")
	if kind != "album" && kind != "blog" {
		return duitangRoute{}
	}
	values := u.Query()["id"]
	if len(values) != 1 || !duitangID.MatchString(values[0]) || strings.Trim(values[0], "0") == "" {
		return duitangRoute{}
	}
	return duitangRoute{kind: kind, id: values[0]}
}

func isDuitangPage(u *url.URL) bool { return duitangRouteOf(u).id != "" }

func duitangImageURL(page *url.URL, raw string) (string, bool) {
	initial, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || strings.Contains(initial.Path, "..") {
		return "", false
	}
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil ||
		(!strings.EqualFold(u.Hostname(), "a-ho-img.dtstatic.com") && !strings.EqualFold(u.Hostname(), "c-ssl.duitang.com")) ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) ||
		!strings.HasPrefix(u.Path, "/uploads/") {
		return "", false
	}
	u.Scheme, u.Host = "https", strings.ToLower(u.Hostname())
	return u.String(), true
}

func duitangItem(page *url.URL, raw, post, album string) (Item, bool) {
	image, ok := duitangImageURL(page, raw)
	if !ok {
		return Item{}, false
	}
	item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"duitang_post_id": post}}
	if album != "" {
		item.Metadata["album_id"] = album
	}
	if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	return item, true
}

func discoverDuitang(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	route := duitangRouteOf(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || route.id == "" {
		return result, errors.New("堆糖作品或专辑地址无效")
	}
	page := *root
	if _, ok := getter.(*HTTPGetter); ok {
		page.Scheme, page.Host = "https", "www.duitang.com"
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("读取堆糖页面失败: %w", err)
	}
	if doc == nil || final == nil || !sameOrigin(&page, final) || duitangRouteOf(final) != route {
		return result, errors.New("堆糖页面已跳转，可能需要登录或验证")
	}
	return duitangCollect(ctx, getter, final, doc)
}

func duitangCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	route := duitangRouteOf(page)
	if route.id == "" || doc == nil || getter == nil {
		return result, errors.New("堆糖页面或下载客户端无效")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if route.kind == "blog" {
		return duitangPost(ctx, page, doc, route.id)
	}
	return duitangAlbum(ctx, getter, page, doc, route.id)
}

func duitangPost(ctx context.Context, page *url.URL, doc *html.Node, id string) (Discovery, error) {
	result := Discovery{IsPage: true}
	invalid := 0
	limited := false
	seen := make(map[string]bool)
	walkHTML(doc, func(n *html.Node) {
		if ctx.Err() != nil || limited || n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "a" || !duitangHasClass(n, "vieworg") {
			return
		}
		inside := false
		for p := n.Parent; p != nil; p = p.Parent {
			if attribute(p, "id") == "detailcnt" || attribute(p, "id") == "detailinfo" {
				inside = true
				break
			}
		}
		if !inside {
			return
		}
		item, ok := duitangItem(page, attribute(n, "href"), id, "")
		if !ok {
			invalid++
		} else if !seen[item.URL] {
			if len(result.Items) >= maxDuitangImages {
				limited = true
				return
			}
			seen[item.URL] = true
			result.Items = append(result.Items, item)
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var problems []error
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 张堆糖作品图片地址无效", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("堆糖作品超过 %d 张图片，结果不完整", maxDuitangImages))
	}
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("堆糖作品未返回原图，可能需要登录或验证"))
	}
	return result, errors.Join(problems...)
}

func duitangHasClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

type duitangAlbumResponse struct {
	Status *int `json:"status"`
	Data   struct {
		More    *int   `json:"more"`
		After   string `json:"after"`
		Objects []struct {
			ID    json.RawMessage `json:"id"`
			Photo struct {
				Path string `json:"path"`
			} `json:"photo"`
		} `json:"object_list"`
	} `json:"data"`
}

func duitangAlbumJSON(ctx context.Context, getter Getter, page *url.URL, id, after string) (duitangAlbumResponse, error) {
	var result duitangAlbumResponse
	endpoint := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/napi/vienna/blog/by_album/"}
	query := url.Values{"album_id": {id}, "limit": {"24"}, "after_id": {after}}
	endpoint.RawQuery = query.Encode()
	resp, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", endpoint.Scheme+"://"+endpoint.Host+"/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, &TransferError{Err: err}
	}
	defer resp.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path ||
		len(final.Query()["album_id"]) != 1 || len(final.Query()["after_id"]) != 1 || len(final.Query()["limit"]) != 1 ||
		final.Query().Get("album_id") != id || final.Query().Get("after_id") != after || final.Query().Get("limit") != "24" {
		return result, errors.New("堆糖专辑接口跳转到其他页面，可能需要登录")
	}
	if resp.StatusCode != http.StatusOK {
		return result, &HTTPStatusError{URL: final.String(), StatusCode: resp.StatusCode, RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	var reader io.Reader = io.LimitReader(resp.Body, maxPageBytes+1)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return result, &TransferError{Err: err}
		}
		defer compressed.Close()
		reader = compressed
	case "deflate":
		compressed, err := zlib.NewReader(reader)
		if err != nil {
			return result, &TransferError{Err: err}
		}
		defer compressed.Close()
		reader = compressed
	default:
		return result, errors.New("不支持堆糖接口的压缩格式")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, &TransferError{Err: err}
	}
	if len(body) > maxPageBytes {
		return result, errors.New("堆糖专辑接口超过 8 MiB 上限")
	}
	if json.Unmarshal(body, &result) != nil || result.Status == nil || *result.Status != 1 || result.Data.More == nil || result.Data.Objects == nil {
		return result, errors.New("堆糖专辑接口响应无效，可能需要登录或验证")
	}
	if *result.Data.More != 0 && *result.Data.More != 1 {
		return result, errors.New("堆糖专辑分页状态无效")
	}
	return result, nil
}

func duitangAlbum(ctx context.Context, getter Getter, page *url.URL, doc *html.Node, id string) (Discovery, error) {
	result := Discovery{IsPage: true}
	declared := -1
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if duitangHasClass(n, "album-count") {
			if match := duitangCount.FindStringSubmatch(nodeText(n)); len(match) == 2 {
				if count, err := strconv.Atoi(match[1]); err == nil {
					declared = count
				}
			}
		}
	})
	seenImages, seenIDs, seenCursors := map[string]bool{}, map[string]bool{}, map[string]bool{"0": true}
	after := "0"
	invalid := 0
	var problems []error
	for pageIndex := 0; pageIndex < maxDuitangPages; pageIndex++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		response, err := duitangAlbumJSON(ctx, getter, page, id, after)
		if err != nil {
			problems = append(problems, err)
			break
		}
		for _, object := range response.Data.Objects {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			var post string
			if json.Unmarshal(object.ID, &post) != nil {
				post = string(bytes.TrimSpace(object.ID))
			}
			if !duitangID.MatchString(post) || strings.Trim(post, "0") == "" {
				invalid++
				continue
			}
			seenIDs[post] = true
			item, ok := duitangItem(page, object.Photo.Path, post, id)
			if !ok {
				invalid++
				continue
			}
			if seenImages[item.URL] {
				continue
			}
			if len(result.Items) >= maxDuitangImages {
				problems = append(problems, fmt.Errorf("堆糖专辑超过 %d 张图片，结果不完整", maxDuitangImages))
				return result, errors.Join(problems...)
			}
			seenImages[item.URL] = true
			result.Items = append(result.Items, item)
		}
		if *response.Data.More == 0 {
			break
		}
		next := response.Data.After
		if len(next) > 200 || !duitangCursor.MatchString(next) || seenCursors[next] || len(response.Data.Objects) == 0 {
			problems = append(problems, errors.New("堆糖专辑分页游标无效或重复，结果不完整"))
			break
		}
		if pageIndex == maxDuitangPages-1 {
			problems = append(problems, fmt.Errorf("堆糖专辑超过 %d 页，结果不完整", maxDuitangPages))
			break
		}
		seenCursors[next], after = true, next
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 个堆糖专辑条目无效，结果可能不完整", invalid))
	}
	if declared > len(seenIDs) {
		problems = append(problems, fmt.Errorf("堆糖页面显示 %d 张图片，接口返回 %d 个条目，结果可能不完整", declared, len(seenIDs)))
	}
	if len(result.Items) == 0 && declared != 0 {
		problems = append(problems, errors.New("堆糖专辑未返回图片，可能需要登录或验证"))
	}
	return result, errors.Join(problems...)
}
