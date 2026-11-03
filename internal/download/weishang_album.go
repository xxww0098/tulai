package download

import (
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
)

var (
	weishangHost = regexp.MustCompile(`^a[0-9]{18,30}\.(?:szwego\.com|wsxcme\.com)$`)
	weishangID   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	weishangTime = regexp.MustCompile(`^-?[0-9]{1,20}$`)
)

// shortcut: limit anomalously large products until configurable limits are needed.
const (
	maxWeishangImages = 10000
	maxWeishangPages  = 100
)

type weishangRoute struct{ kind, album, item string }

func weishangRouteOf(u *url.URL) weishangRoute {
	if u == nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		!weishangHost.MatchString(strings.ToLower(u.Hostname())) || u.Path != "/static/index.html" {
		return weishangRoute{}
	}
	fragment, _, _ := strings.Cut(strings.TrimPrefix(u.Fragment, "/"), "?")
	parts := strings.Split(fragment, "/")
	if len(parts) == 3 && parts[0] == "theme_detail" && weishangID.MatchString(parts[1]) && weishangID.MatchString(parts[2]) {
		return weishangRoute{kind: parts[0], album: parts[1], item: parts[2]}
	}
	if len(parts) == 2 && (parts[0] == "shop_detail" || parts[0] == "goods_list") && weishangID.MatchString(parts[1]) {
		return weishangRoute{kind: parts[0], album: parts[1]}
	}
	return weishangRoute{}
}

func isWeishangProduct(u *url.URL) bool { return weishangRouteOf(u).kind == "theme_detail" }

func isWeishangPage(u *url.URL) bool { return weishangRouteOf(u).kind != "" }

func discoverWeishang(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	if isWeishangProduct(page) {
		return discoverWeishangProduct(ctx, getter, page)
	}
	route := weishangRouteOf(page)
	if route.kind == "shop_detail" {
		return discoverWeishangAlbum(ctx, getter, page)
	}
	return Discovery{IsPage: true}, errors.New("微商相册分类筛选暂未实现")
}

func weishangImageURL(raw string) (string, bool) {
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") ||
		(!strings.EqualFold(u.Hostname(), "xcimg.szwego.com") && !strings.EqualFold(u.Hostname(), "qnimg.szwego.com")) ||
		strings.Contains(u.Path, "..") || u.Path == "" || u.Path == "/" {
		return "", false
	}
	u.Host = strings.ToLower(u.Hostname())
	return u.String(), true
}

// discoverWeishangProduct only requests the current tenant's same-origin API.
// A browser Getter can supply its login state without forwarding it to the CDN.
func discoverWeishangProduct(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	route := weishangRouteOf(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || route.item == "" {
		return result, errors.New("微商相册商品地址无效")
	}
	endpoint := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/commodity/view"}
	endpoint.RawQuery = url.Values{"targetAlbumId": {route.album}, "itemId": {route.item}}.Encode()
	resp, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", page.Scheme+"://"+page.Host+"/")
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
		len(final.Query()["targetAlbumId"]) != 1 || len(final.Query()["itemId"]) != 1 ||
		final.Query().Get("targetAlbumId") != route.album || final.Query().Get("itemId") != route.item {
		return result, errors.New("微商相册接口跳转到其他页面，可能需要登录")
	}
	if resp.StatusCode != http.StatusOK {
		return result, &HTTPStatusError{URL: final.String(), StatusCode: resp.StatusCode, RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	body, err := readWeishangBody(ctx, resp)
	if err != nil {
		return result, err
	}
	var data struct {
		Errcode *int `json:"errcode"`
		Result  struct {
			Commodity *struct {
				Title     string            `json:"title"`
				Images    []json.RawMessage `json:"imgsSrc"`
				ThemeType *int              `json:"themeType"`
				VideoURL  string            `json:"videoURL"`
				VideoUrl  string            `json:"videoUrl"`
			} `json:"commodity"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &data) != nil || data.Errcode == nil {
		return result, errors.New("微商相册商品数据无效，可能需要登录或验证")
	}
	if *data.Errcode != 0 {
		if *data.Errcode == 9 {
			return result, errors.New("微商相册要求登录或授权后查看")
		}
		return result, fmt.Errorf("微商相册未返回商品（错误码 %d）", *data.Errcode)
	}
	if data.Result.Commodity == nil || data.Result.Commodity.Images == nil {
		return result, errors.New("微商相册商品未返回图片数据，可能需要登录或授权")
	}
	commodity := data.Result.Commodity
	result.Title = strings.TrimSpace(commodity.Title)
	seen := make(map[string]bool)
	invalid := 0
	limited := false
	for _, value := range commodity.Images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		var raw string
		if json.Unmarshal(value, &raw) != nil {
			invalid++
			continue
		}
		image, ok := weishangImageURL(raw)
		if !ok {
			invalid++
			continue
		}
		if seen[image] {
			continue
		}
		if len(result.Items) == maxWeishangImages {
			limited = true
			break
		}
		seen[image] = true
		item := Item{URL: image, RequireImage: true, Metadata: map[string]string{"weishang_album_id": route.album, "weishang_item_id": route.item}}
		if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	var problems []error
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 张微商相册图片地址不可识别，结果可能不完整", invalid))
	}
	if limited {
		problems = append(problems, fmt.Errorf("微商相册商品超过 %d 张图片，结果不完整", maxWeishangImages))
	}
	if commodity.VideoURL != "" || commodity.VideoUrl != "" || (commodity.ThemeType != nil && (*commodity.ThemeType == 1 || *commodity.ThemeType == 4)) {
		problems = append(problems, errors.New("微商相册商品包含视频，当前仅采集图片"))
	}
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("微商相册商品未返回图片"))
	}
	return result, errors.Join(problems...)
}

func readWeishangBody(ctx context.Context, resp *Response) ([]byte, error) {
	var reader io.Reader = io.LimitReader(resp.Body, maxPageBytes+1)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return nil, &TransferError{Err: err}
		}
		defer compressed.Close()
		reader = compressed
	case "deflate":
		compressed, err := zlib.NewReader(reader)
		if err != nil {
			return nil, &TransferError{Err: err}
		}
		defer compressed.Close()
		reader = compressed
	default:
		return nil, errors.New("不支持微商相册接口的压缩格式")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	if len(body) > maxPageBytes {
		return nil, errors.New("微商相册接口超过 8 MiB 上限")
	}
	return body, nil
}

type weishangAlbumPage struct {
	Errcode *int `json:"errcode"`
	Result  *struct {
		Items      []json.RawMessage `json:"items"`
		Pagination *struct {
			More      json.RawMessage `json:"isLoadMore"`
			Timestamp json.RawMessage `json:"pageTimestamp"`
		} `json:"pagination"`
		TargetAlbum struct {
			Name        string          `json:"name"`
			Attention   json.RawMessage `json:"attentionVerify"`
			LinkType    *int            `json:"shopLinkType"`
			LinkExpired *int            `json:"shopLinkExpireStatus"`
			URLClose    *int            `json:"urlClose"`
			VipMessage  string          `json:"linkVipMsg"`
		} `json:"targetAlbum"`
		Album struct {
			Tourist json.RawMessage `json:"isTourist"`
		} `json:"album"`
	} `json:"result"`
}

type weishangAlbumItem struct {
	ID        string            `json:"goods_id"`
	Images    []json.RawMessage `json:"imgsSrc"`
	ThemeType *int              `json:"themeType"`
	Time      json.RawMessage   `json:"time_stamp"`
	VideoURL  string            `json:"videoURL"`
	VideoUrl  string            `json:"videoUrl"`
}

func weishangBool(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return false, false
	}
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	var number int
	if json.Unmarshal(raw, &number) == nil && (number == 0 || number == 1) {
		return number == 1, true
	}
	return false, false
}

func weishangTimestamp(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		value = strings.TrimSpace(string(raw))
	}
	if !weishangTime.MatchString(value) || strings.Trim(value, "-0") == "" {
		return ""
	}
	return value
}

func discoverWeishangAlbum(ctx context.Context, getter Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	route := weishangRouteOf(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	poster, ok := getter.(Poster)
	if !ok || route.kind != "shop_detail" {
		return result, errors.New("微商相册需要支持同源 POST 的下载客户端")
	}
	if _, query, found := strings.Cut(page.Fragment, "?"); found && query != "" {
		return result, errors.New("微商相册带参数的分类或搜索筛选暂未实现")
	}
	endpoint := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/album/personal/all"}
	seenImages, seenCursors := make(map[string]bool), make(map[string]bool)
	var problems []error
	invalid, videos := 0, false
	var timestamp string
	for pageIndex := 0; pageIndex < maxWeishangPages; pageIndex++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		query := url.Values{"albumId": {route.album}, "startDate": {""}, "endDate": {""}, "auditPassword": {""}, "requestDataType": {""}}
		if timestamp != "" {
			query.Set("slipType", "1")
			query.Set("timestamp", timestamp)
		}
		endpoint.RawQuery = query.Encode()
		response, err := poster.Post(ctx, endpoint.String(), nil, http.Header{"Accept": {"application/json"}})
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			problems = append(problems, &TransferError{Err: err})
			break
		}
		final, finalErr := crawlURL(nil, firstNonEmpty(response.FinalURL, endpoint.String()))
		if finalErr != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path || final.RawQuery != endpoint.RawQuery {
			response.Body.Close()
			problems = append(problems, errors.New("微商相册接口跳转到其他页面"))
			break
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			problems = append(problems, &HTTPStatusError{URL: final.String(), StatusCode: response.StatusCode, RetryAfter: ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())})
			break
		}
		body, readErr := readWeishangBody(ctx, response)
		response.Body.Close()
		if readErr != nil {
			problems = append(problems, readErr)
			break
		}
		var data weishangAlbumPage
		if json.Unmarshal(body, &data) != nil || data.Errcode == nil {
			problems = append(problems, errors.New("微商相册数据无效，可能需要登录或验证"))
			break
		}
		if *data.Errcode != 0 {
			if *data.Errcode == 9 {
				problems = append(problems, errors.New("微商相册要求登录或授权后查看"))
			} else {
				problems = append(problems, fmt.Errorf("微商相册未返回列表（错误码 %d）", *data.Errcode))
			}
			break
		}
		if data.Result == nil || data.Result.Items == nil {
			problems = append(problems, errors.New("微商相册列表或分页数据无效"))
			break
		}
		target := data.Result.TargetAlbum
		attention, _ := weishangBool(target.Attention)
		tourist, touristKnown := weishangBool(data.Result.Album.Tourist)
		if (attention && (!touristKnown || tourist)) || (target.LinkType != nil && *target.LinkType == 2 && target.LinkExpired != nil && *target.LinkExpired == 0) || (target.URLClose != nil && *target.URLClose == 1) || target.VipMessage != "" {
			problems = append(problems, errors.New("微商相册需要登录或授权后查看"))
			break
		}
		if result.Title == "" {
			result.Title = target.Name
		}
		var more, moreKnown bool
		if data.Result.Pagination != nil {
			more, moreKnown = weishangBool(data.Result.Pagination.More)
		}
		if !moreKnown {
			problems = append(problems, errors.New("微商相册缺少分页状态，结果可能不完整"))
		}
		var lastTimestamp string
		for _, value := range data.Result.Items {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			var item weishangAlbumItem
			if json.Unmarshal(value, &item) != nil {
				invalid++
				continue
			}
			if current := weishangTimestamp(item.Time); current != "" {
				lastTimestamp = current
			}
			itemHasVideo := item.VideoURL != "" || item.VideoUrl != "" || (item.ThemeType != nil && (*item.ThemeType == 1 || *item.ThemeType == 4))
			if itemHasVideo {
				videos = true
			}
			if !weishangID.MatchString(item.ID) {
				invalid++
				continue
			}
			if len(item.Images) == 0 && !itemHasVideo {
				invalid++
			}
			for _, value := range item.Images {
				var raw string
				if json.Unmarshal(value, &raw) != nil {
					invalid++
					continue
				}
				image, ok := weishangImageURL(raw)
				if !ok {
					invalid++
					continue
				}
				if seenImages[image] {
					continue
				}
				if len(result.Items) == maxWeishangImages {
					problems = append(problems, fmt.Errorf("微商相册超过 %d 张图片，结果不完整", maxWeishangImages))
					return result, errors.Join(problems...)
				}
				seenImages[image] = true
				picture := Item{URL: image, RequireImage: true, Metadata: map[string]string{"weishang_album_id": route.album, "weishang_item_id": item.ID}}
				if ref := imageReferer(page.Scheme+"://"+page.Host+"/", image); ref != "" {
					picture.Headers = map[string]string{"Referer": ref}
				}
				result.Items = append(result.Items, picture)
			}
		}
		if !moreKnown || !more {
			break
		}
		next := weishangTimestamp(data.Result.Pagination.Timestamp)
		if next == "" {
			next = lastTimestamp
		}
		if next == "" || seenCursors[next] || len(data.Result.Items) == 0 {
			problems = append(problems, errors.New("微商相册分页游标无效或重复，结果不完整"))
			break
		}
		if pageIndex == maxWeishangPages-1 {
			problems = append(problems, fmt.Errorf("微商相册超过 %d 页，结果不完整", maxWeishangPages))
			break
		}
		seenCursors[next], timestamp = true, next
	}
	if invalid > 0 {
		problems = append(problems, fmt.Errorf("%d 个微商相册条目或图片地址无效，结果可能不完整", invalid))
	}
	if videos {
		problems = append(problems, errors.New("微商相册包含视频，当前仅采集图片"))
	}
	if len(result.Items) == 0 {
		problems = append(problems, errors.New("微商相册未返回图片"))
	}
	return result, errors.Join(problems...)
}
