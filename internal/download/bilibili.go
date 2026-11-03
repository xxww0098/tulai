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

var bilibiliOpusPath = regexp.MustCompile(`^/opus/([0-9]{1,20})/?$`)
var bilibiliDynamicPath = regexp.MustCompile(`^/([0-9]{1,20})/?$`)
var bilibiliSpacePath = regexp.MustCompile(`^/([0-9]{1,20})(?:/(upload/opus|dynamic|article|album))?/?$`)

// shortcut: 单个空间最多读取 1000 条动态；需要更大空间时调整上限。
const maxBilibiliSpacePosts = 1000

type bilibiliState struct {
	Detail json.RawMessage `json:"detail"`
}

type bilibiliDetail struct {
	ID    string `json:"id_str"`
	Basic struct {
		Title string `json:"title"`
	} `json:"basic"`
	Modules json.RawMessage `json:"modules"`
}

func bilibiliPostID(u *url.URL) string {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return ""
	}
	var match []string
	switch strings.ToLower(u.Hostname()) {
	case "www.bilibili.com", "bilibili.com", "m.bilibili.com":
		match = bilibiliOpusPath.FindStringSubmatch(u.Path)
	case "t.bilibili.com":
		match = bilibiliDynamicPath.FindStringSubmatch(u.Path)
	}
	if len(match) != 2 || strings.Trim(match[1], "0") == "" {
		return ""
	}
	return match[1]
}

func isBilibiliPost(u *url.URL) bool { return bilibiliPostID(u) != "" }

func bilibiliSpaceParts(u *url.URL) (string, string) {
	if u == nil || u.User != nil || !strings.EqualFold(u.Hostname(), "space.bilibili.com") ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return "", ""
	}
	match := bilibiliSpacePath.FindStringSubmatch(u.Path)
	if len(match) != 3 || strings.Trim(match[1], "0") == "" {
		return "", ""
	}
	return match[1], match[2]
}

func isBilibiliSpace(u *url.URL) bool {
	id, _ := bilibiliSpaceParts(u)
	return id != ""
}

func bilibiliPublicGetter(getter Getter) Getter {
	return GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return getter.Get(ctx, raw, func(r *http.Request) {
			if prepare != nil {
				prepare(r)
			}
			r.Header.Del("Cookie")
			r.Header.Del("Authorization")
		})
	})
}

// discoverBilibiliPost reads the site's public JSON state; it never executes page scripts.
func discoverBilibiliPost(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := bilibiliPostID(root)
	if id == "" {
		return result, errors.New("哔哩哔哩图文地址无效")
	}
	page, _ := url.Parse("https://www.bilibili.com/opus/" + id)
	doc, final, _, response, err := fetchCrawlPage(ctx, bilibiliPublicGetter(getter), crawlPage{url: page.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("读取哔哩哔哩图文失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || final == nil || final.User != nil || !sameOrigin(page, final) || strings.TrimSuffix(final.Path, "/") != page.Path {
		return result, errors.New("哔哩哔哩图文不可用或跳转到了其他页面")
	}
	return bilibiliPostPage(ctx, doc, final)
}

func bilibiliPostPage(ctx context.Context, doc *html.Node, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	id := bilibiliPostID(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if id == "" || doc == nil {
		return result, errors.New("哔哩哔哩图文页面无效")
	}
	var detail bilibiliDetail
	var found bool
	walkHTML(doc, func(n *html.Node) {
		if found || n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for {
				i := strings.Index(text, "window.__INITIAL_STATE__")
				if i < 0 {
					break
				}
				text = strings.TrimSpace(text[i+len("window.__INITIAL_STATE__"):])
				if !strings.HasPrefix(text, "=") {
					continue
				}
				text = strings.TrimSpace(text[1:])
				var candidate bilibiliState
				var candidateDetail bilibiliDetail
				if json.NewDecoder(strings.NewReader(text)).Decode(&candidate) == nil && json.Unmarshal(candidate.Detail, &candidateDetail) == nil && candidateDetail.ID == id && bytes.HasPrefix(bytes.TrimSpace(candidateDetail.Modules), []byte("[")) {
					detail, found = candidateDetail, true
					return
				}
			}
		}
	})
	if !found {
		return result, errors.New("未读取到该哔哩哔哩图文状态，可能需要登录或验证")
	}
	result.Title = strings.TrimSpace(detail.Basic.Title)
	seen := make(map[string]bool)
	var invalid int
	decoder := json.NewDecoder(bytes.NewReader(detail.Modules))
	err := bilibiliCollectPictures(ctx, decoder, nil, 0, func(raw string) {
		if !bilibiliAddImage(&result, seen, page, raw) {
			invalid++
		}
	})
	if err != nil {
		return result, fmt.Errorf("解析哔哩哔哩图文图片失败: %w", err)
	}
	if invalid > 0 {
		return result, fmt.Errorf("%d 张哔哩哔哩图片地址无效，结果可能不完整", invalid)
	}
	return result, nil
}

func bilibiliAddImage(result *Discovery, seen map[string]bool, source *url.URL, raw string) bool {
	u, err := crawlURL(source, raw)
	if err != nil || u.User != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".hdslb.com") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return false
	}
	if u.Scheme == "http" {
		u.Scheme, u.Host = "https", u.Hostname()
	}
	if !seen[u.String()] {
		seen[u.String()] = true
		item := Item{URL: u.String()}
		if referer := imageReferer(source.String(), item.URL); referer != "" {
			item.Headers = map[string]string{"Referer": referer}
		}
		result.Items = append(result.Items, item)
	}
	return true
}

func bilibiliAPI(ctx context.Context, getter Getter, endpoint *url.URL, target any) error {
	response, err := bilibiliPublicGetter(getter).Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", "https://space.bilibili.com/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &TransferError{Err: err}
	}
	defer response.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) {
		return errors.New("哔哩哔哩接口跳转到了其他站点")
	}
	if response.StatusCode != http.StatusOK {
		return &HTTPStatusError{URL: final.String(), StatusCode: response.StatusCode,
			RetryAfter: ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return &TransferError{Err: err}
	}
	if len(raw) > maxPageBytes {
		return errors.New("哔哩哔哩接口响应超过 8 MiB")
	}
	if length := contentLength(response); length >= 0 && length != int64(len(raw)) {
		return &IntegrityError{Err: errors.New("哔哩哔哩接口响应长度不符")}
	}
	var input io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		reader, err := gzip.NewReader(input)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer reader.Close()
		input = reader
	case "deflate":
		reader, err := zlib.NewReader(input)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer reader.Close()
		input = reader
	default:
		return errors.New("哔哩哔哩接口使用了不支持的压缩格式")
	}
	data, err := io.ReadAll(io.LimitReader(input, maxPageBytes+1))
	if err != nil {
		return &TransferError{Err: err}
	}
	if len(data) > maxPageBytes {
		return errors.New("哔哩哔哩接口响应超过 8 MiB")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("哔哩哔哩接口响应不是有效的 JSON")
	}
	return nil
}

func discoverBilibiliSpace(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	uid, kind := bilibiliSpaceParts(root)
	result := Discovery{IsPage: true, Title: "哔哩哔哩 " + uid}
	if uid == "" {
		return result, errors.New("哔哩哔哩空间地址无效")
	}
	if kind == "" {
		article := *root
		article.Path, article.RawPath, article.RawQuery, article.Fragment = "/"+uid+"/article", "", "", ""
		first, firstErr := discoverBilibiliSpace(ctx, getter, &article)
		result.Items = append(result.Items, first.Items...)
		if ctx.Err() != nil {
			return result, errors.Join(firstErr, ctx.Err())
		}
		album := article
		album.Path = "/" + uid + "/album"
		second, secondErr := discoverBilibiliSpace(ctx, getter, &album)
		seen := make(map[string]bool, len(result.Items))
		for _, item := range result.Items {
			seen[item.URL] = true
		}
		for _, item := range second.Items {
			if !seen[item.URL] {
				seen[item.URL] = true
				result.Items = append(result.Items, item)
			}
		}
		return result, errors.Join(firstErr, secondErr)
	}
	seenPosts, seenImages := map[string]bool{}, map[string]bool{}
	var failures []error
	if kind == "dynamic" {
		failures = append(failures, errors.New("已扫描图文动态，其他动态类型可能不完整"))
	}
	finish := func(err error) (Discovery, error) { return result, errors.Join(append(failures, err)...) }
	offset := ""
	seenOffsets := map[string]bool{"": true}
	for page := 0; page < maxCrawlPages; page++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if kind == "album" {
			endpoint := &url.URL{Scheme: "https", Host: "api.bilibili.com", Path: "/x/dynamic/feed/draw/doc_list"}
			endpoint.RawQuery = url.Values{"uid": {uid}, "page_num": {strconv.Itoa(page)}, "page_size": {"30"}}.Encode()
			var payload struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    *struct {
					Items   json.RawMessage `json:"items"`
					HasMore *bool           `json:"has_more"`
				} `json:"data"`
			}
			if err := bilibiliAPI(ctx, getter, endpoint, &payload); err != nil {
				return finish(fmt.Errorf("读取哔哩哔哩相册第 %d 页失败: %w", page+1, err))
			}
			if payload.Code != 0 || payload.Data == nil || len(payload.Data.Items) == 0 {
				return finish(fmt.Errorf("哔哩哔哩相册接口返回 %d，可能需要登录或验证", payload.Code))
			}
			var posts []struct {
				ID       string `json:"dyn_id"`
				Count    *int   `json:"count"`
				Pictures []struct {
					URL string `json:"img_src"`
				} `json:"pictures"`
			}
			if err := json.Unmarshal(payload.Data.Items, &posts); err != nil {
				return finish(errors.New("哔哩哔哩相册列表格式无效"))
			}
			if len(posts) == 0 {
				if payload.Data.HasMore != nil && *payload.Data.HasMore {
					return finish(errors.New("哔哩哔哩相册分页提前结束，结果可能不完整"))
				}
				return finish(nil)
			}
			added := 0
			for _, post := range posts {
				if err := ctx.Err(); err != nil {
					return finish(err)
				}
				postURL, _ := url.Parse("https://t.bilibili.com/" + post.ID)
				if bilibiliPostID(postURL) == "" {
					failures = append(failures, errors.New("哔哩哔哩相册含无效动态编号"))
					continue
				}
				if seenPosts[post.ID] {
					continue
				}
				if len(seenPosts) >= maxBilibiliSpacePosts {
					return finish(fmt.Errorf("哔哩哔哩空间已达到 %d 条动态上限，结果可能不完整", maxBilibiliSpacePosts))
				}
				seenPosts[post.ID], added = true, added+1
				if post.Count != nil && *post.Count != len(post.Pictures) {
					failures = append(failures, fmt.Errorf("动态 %s 的图片数量不一致", post.ID))
				}
				for _, picture := range post.Pictures {
					if !bilibiliAddImage(&result, seenImages, postURL, picture.URL) {
						failures = append(failures, fmt.Errorf("动态 %s 的图片地址无效", post.ID))
					}
				}
			}
			if payload.Data.HasMore != nil && !*payload.Data.HasMore {
				return finish(nil)
			}
			if added == 0 {
				return finish(errors.New("哔哩哔哩相册分页重复，结果可能不完整"))
			}
			continue
		}

		endpoint := &url.URL{Scheme: "https", Host: "api.bilibili.com", Path: "/x/polymer/web-dynamic/v1/opus/feed/space"}
		query := url.Values{"host_mid": {uid}}
		if offset != "" {
			query.Set("offset", offset)
		}
		endpoint.RawQuery = query.Encode()
		var payload struct {
			Code int `json:"code"`
			Data *struct {
				Items []struct {
					ID   string `json:"opus_id"`
					Jump string `json:"jump_url"`
				} `json:"items"`
				Offset  string `json:"offset"`
				HasMore *bool  `json:"has_more"`
			} `json:"data"`
		}
		if err := bilibiliAPI(ctx, getter, endpoint, &payload); err != nil {
			return finish(fmt.Errorf("读取哔哩哔哩图文列表失败: %w", err))
		}
		if payload.Code != 0 || payload.Data == nil || payload.Data.Items == nil || payload.Data.HasMore == nil {
			return finish(fmt.Errorf("哔哩哔哩图文列表返回 %d，可能需要登录或验证", payload.Code))
		}
		added := 0
		for _, post := range payload.Data.Items {
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			postURL, _ := url.Parse("https://www.bilibili.com/opus/" + post.ID)
			if bilibiliPostID(postURL) == "" {
				failures = append(failures, errors.New("哔哩哔哩图文含无效编号"))
				continue
			}
			if post.Jump != "" {
				jump, err := crawlURL(endpoint, post.Jump)
				if err != nil || bilibiliPostID(jump) != post.ID {
					failures = append(failures, fmt.Errorf("哔哩哔哩图文 %s 跳转地址无效", post.ID))
					continue
				}
			}
			if seenPosts[post.ID] {
				continue
			}
			if len(seenPosts) >= maxBilibiliSpacePosts {
				return finish(fmt.Errorf("哔哩哔哩空间已达到 %d 条动态上限，结果可能不完整", maxBilibiliSpacePosts))
			}
			seenPosts[post.ID], added = true, added+1
			found, err := discoverBilibiliPost(ctx, getter, postURL)
			for _, item := range found.Items {
				if !seenImages[item.URL] {
					seenImages[item.URL] = true
					result.Items = append(result.Items, item)
				}
			}
			if err != nil {
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				failures = append(failures, fmt.Errorf("读取哔哩哔哩图文 %s 失败: %w", post.ID, err))
			}
		}
		if !*payload.Data.HasMore {
			return finish(nil)
		}
		if added == 0 || payload.Data.Offset == "" || len(payload.Data.Offset) > 256 || seenOffsets[payload.Data.Offset] {
			return finish(errors.New("哔哩哔哩图文分页重复或缺少游标，结果可能不完整"))
		}
		offset = payload.Data.Offset
		seenOffsets[offset] = true
	}
	return finish(fmt.Errorf("哔哩哔哩空间已达到 %d 页上限，结果可能不完整", maxCrawlPages))
}

// shortcut: 图片状态最多遍历 64 层；遇到更深的实际结构时调整上限。
func bilibiliCollectPictures(ctx context.Context, decoder *json.Decoder, path []string, depth int, add func(string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 64 {
		return errors.New("图片状态层级超过 64 层")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return errors.New("无效的图片状态键")
			}
			if name == "pics" && len(path) > 0 && (path[len(path)-1] == "pic" || path[len(path)-1] == "album") {
				if err := bilibiliPictureArray(ctx, decoder, add); err != nil {
					return err
				}
			} else if err := bilibiliCollectPictures(ctx, decoder, append(path, name), depth+1, add); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	case json.Delim('['):
		for decoder.More() {
			if err := bilibiliCollectPictures(ctx, decoder, path, depth+1, add); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
	}
	return err
}

func bilibiliPictureArray(ctx context.Context, decoder *json.Decoder, add func(string)) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return nil
	}
	if token != json.Delim('[') {
		return errors.New("图片列表格式无效")
	}
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var image struct {
			URL string `json:"url"`
		}
		if err := decoder.Decode(&image); err != nil {
			return err
		}
		add(image.URL)
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim(']') {
		return io.ErrUnexpectedEOF
	}
	return nil
}
