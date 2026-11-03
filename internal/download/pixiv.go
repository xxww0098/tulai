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
	"sort"
	"strconv"
	"strings"
	"time"
)

var pixivArtworkPath = regexp.MustCompile(`^/(?:[a-z]{2}(?:-[a-z]{2})?/)?artworks/([0-9]{1,20})/?$`)
var pixivUserPath = regexp.MustCompile(`^/(?:[a-z]{2}(?:-[a-z]{2})?/)?users/([0-9]{1,20})/?$`)
var pixivID = regexp.MustCompile(`^[0-9]{1,20}$`)

// shortcut: cap works and pages so very large collections cannot run forever;
// make these task settings when larger Pixiv profiles need complete downloads.
const (
	maxPixivWorks  = 1000
	maxPixivPages  = 1000
	maxPixivImages = 10000
)

func pixivHost(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if host := strings.ToLower(u.Hostname()); host != "www.pixiv.net" && host != "pixiv.net" {
		return false
	}
	return u.Port() == "" || (u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443")
}

func pixivValidID(id string) bool {
	return pixivID.MatchString(id) && strings.Trim(id, "0") != ""
}

func pixivArtworkID(u *url.URL) string {
	if !pixivHost(u) {
		return ""
	}
	if match := pixivArtworkPath.FindStringSubmatch(u.Path); len(match) == 2 && pixivValidID(match[1]) {
		return match[1]
	}
	if u.Path == "/member_illust.php" {
		id := u.Query().Get("illust_id")
		if pixivValidID(id) {
			return id
		}
	}
	return ""
}

func isPixivArtwork(u *url.URL) bool { return pixivArtworkID(u) != "" }

func pixivUserID(u *url.URL) string {
	if !pixivHost(u) {
		return ""
	}
	if match := pixivUserPath.FindStringSubmatch(u.Path); len(match) == 2 && pixivValidID(match[1]) {
		return match[1]
	}
	if u.Path == "/member.php" {
		id := u.Query().Get("id")
		if pixivValidID(id) {
			return id
		}
	}
	return ""
}

func isPixivUser(u *url.URL) bool { return pixivUserID(u) != "" }

type pixivPage struct {
	URLs struct {
		Original string `json:"original"`
	} `json:"urls"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type pixivDetail struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	PageCount  int    `json:"pageCount"`
	IllustType int    `json:"illustType"`
}

// pixivAPI uses the page's exact origin so a browser getter only sends that
// page's cookies to the Pixiv API; image requests use a separate origin.
func pixivAPI(ctx context.Context, getter Getter, page *url.URL, apiPath string, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if getter == nil || !pixivHost(page) {
		return errors.New("Pixiv 地址或下载客户端无效")
	}
	endpoint := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: apiPath}
	// The ordinary HTTP client has no browser login state. Pixiv redirects its
	// bare and HTTP origins to www over HTTPS, so use the canonical origin.
	// Every other Getter retains the exact page origin for credential safety.
	if _, ok := getter.(*HTTPGetter); ok {
		endpoint.Scheme, endpoint.Host = "https", "www.pixiv.net"
	}
	resp, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", endpoint.Scheme+"://"+endpoint.Host+"/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &TransferError{Err: err}
	}
	defer resp.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != endpoint.Path {
		return errors.New("Pixiv 接口跳转到其他页面，可能需要登录")
	}
	if resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{URL: final.String(), StatusCode: resp.StatusCode,
			RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	var reader io.Reader = io.LimitReader(resp.Body, maxPageBytes+1)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer compressed.Close()
		reader = compressed
	case "deflate":
		compressed, err := zlib.NewReader(reader)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer compressed.Close()
		reader = compressed
	default:
		return errors.New("不支持 Pixiv 接口的压缩格式")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return &TransferError{Err: err}
	}
	if len(body) > maxPageBytes {
		return errors.New("Pixiv 接口响应超过 8 MiB 上限")
	}
	var envelope struct {
		Error *bool           `json:"error"`
		Body  json.RawMessage `json:"body"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Error == nil || len(envelope.Body) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Body), []byte("null")) {
		return errors.New("Pixiv 接口响应无效，可能需要登录或验证")
	}
	if *envelope.Error {
		return errors.New("Pixiv 未返回作品，可能需要登录或作品不可访问")
	}
	if err := json.Unmarshal(envelope.Body, result); err != nil {
		return errors.New("Pixiv 接口数据格式无效")
	}
	return nil
}

func pixivOriginal(raw string) (string, bool) {
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "i.pximg.net") ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/img-original/") {
		return "", false
	}
	return u.String(), true
}

func discoverPixivArtwork(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	id := pixivArtworkID(root)
	if id == "" {
		return Discovery{IsPage: true}, errors.New("Pixiv 作品地址无效")
	}
	return pixivArtwork(ctx, getter, root, id)
}

func pixivArtwork(ctx context.Context, getter Getter, origin *url.URL, id string) (Discovery, error) {
	result := Discovery{IsPage: true, Title: "Pixiv " + id}
	if !pixivHost(origin) || !pixivValidID(id) {
		return result, errors.New("Pixiv 作品地址无效")
	}
	basePath := "/ajax/illust/" + id
	var pages []pixivPage
	if err := pixivAPI(ctx, getter, origin, basePath+"/pages", &pages); err != nil {
		return result, fmt.Errorf("读取 Pixiv 作品图片失败: %w", err)
	}
	if len(pages) == 0 {
		return result, errors.New("Pixiv 作品没有返回图片，可能需要登录")
	}
	var problems []error
	if len(pages) > maxPixivPages {
		pages = pages[:maxPixivPages]
		problems = append(problems, fmt.Errorf("Pixiv 作品超过 %d 页，结果可能不完整", maxPixivPages))
	}
	seen := map[string]bool{}
	missing := 0
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return result, errors.Join(append(problems, err)...)
		}
		image, ok := pixivOriginal(page.URLs.Original)
		if !ok {
			missing++
			continue
		}
		if seen[image] {
			continue
		}
		seen[image] = true
		item := Item{URL: image, Headers: map[string]string{"Referer": "https://www.pixiv.net/"},
			Metadata: map[string]string{"pixiv_id": id, "page": strconv.Itoa(i + 1)}}
		if page.Width > 0 {
			item.Metadata["width"] = strconv.Itoa(page.Width)
		}
		if page.Height > 0 {
			item.Metadata["height"] = strconv.Itoa(page.Height)
		}
		result.Items = append(result.Items, item)
	}
	if missing > 0 {
		problems = append(problems, fmt.Errorf("%d 页 Pixiv 图片缺少有效原图地址", missing))
	}
	var detail pixivDetail
	if err := pixivAPI(ctx, getter, origin, basePath, &detail); err != nil {
		return result, errors.Join(append(problems, fmt.Errorf("读取 Pixiv 作品信息失败，结果可能不完整: %w", err))...)
	}
	if detail.ID != id {
		problems = append(problems, errors.New("Pixiv 作品信息的 ID 不匹配"))
	} else if detail.PageCount < 1 {
		problems = append(problems, errors.New("Pixiv 作品没有有效页数，完整性未知"))
	} else {
		if title := strings.TrimSpace(detail.Title); title != "" {
			result.Title = title
		}
		if detail.PageCount != len(pages) {
			problems = append(problems, fmt.Errorf("Pixiv 作品声明 %d 页，实际返回 %d 页", detail.PageCount, len(pages)))
		}
		if detail.IllustType == 2 {
			problems = append(problems, errors.New("Pixiv 动图仅保留静态图片，动画内容未完整下载"))
		}
	}
	return result, errors.Join(problems...)
}

// discoverPixivUser reads the publicly observed profile index, then resolves
// each artwork using the same original-page endpoint as an individual work.
func discoverPixivUser(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	id := pixivUserID(root)
	result := Discovery{IsPage: true, Title: "Pixiv 用户 " + id}
	if id == "" {
		return result, errors.New("Pixiv 用户地址无效")
	}
	var profile struct {
		Illusts json.RawMessage `json:"illusts"`
		Manga   json.RawMessage `json:"manga"`
	}
	if err := pixivAPI(ctx, getter, root, "/ajax/user/"+id+"/profile/all", &profile); err != nil {
		return result, fmt.Errorf("读取 Pixiv 用户作品列表失败: %w", err)
	}
	ids, listErr := pixivProfileIDs(profile.Illusts, profile.Manga)
	var problems []error
	if listErr != nil {
		problems = append(problems, listErr)
	}
	if len(ids) == 0 {
		return result, errors.Join(append(problems, errors.New("Pixiv 用户未返回作品，可能为空或需要登录"))...)
	}
	if len(ids) > maxPixivWorks {
		ids = ids[:maxPixivWorks]
		problems = append(problems, fmt.Errorf("Pixiv 用户作品超过 %d 个，结果不完整", maxPixivWorks))
	}
	seen := make(map[string]bool)
	failed := 0
	var firstErr error
	finish := func(extra error) (Discovery, error) {
		all := append([]error(nil), problems...)
		if failed > 0 {
			all = append(all, fmt.Errorf("%d 个 Pixiv 作品读取失败或不完整: %w", failed, firstErr))
		}
		if len(result.Items) == 0 && extra == nil {
			all = append(all, errors.New("Pixiv 用户作品未返回可下载的原图"))
		}
		return result, errors.Join(append(all, extra)...)
	}
	for _, workID := range ids {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		work, err := pixivArtwork(ctx, getter, root, workID)
		for _, item := range work.Items {
			if seen[item.URL] {
				continue
			}
			if len(result.Items) >= maxPixivImages {
				return finish(fmt.Errorf("Pixiv 用户图片超过 %d 张，结果不完整", maxPixivImages))
			}
			seen[item.URL] = true
			item.Metadata["album_id"] = workID
			item.Metadata["album_title"] = work.Title
			result.Items = append(result.Items, item)
		}
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("作品 %s: %w", workID, err)
			}
			if ctx.Err() != nil {
				return finish(ctx.Err())
			}
		}
	}
	return finish(nil)
}

// Pixiv returns maps keyed by work ID, with null or [] for an empty category.
func pixivProfileIDs(categories ...json.RawMessage) ([]string, error) {
	seen := make(map[string]bool)
	var problems []error
	for _, category := range categories {
		raw := bytes.TrimSpace(category)
		if len(raw) == 0 {
			problems = append(problems, errors.New("Pixiv 用户作品列表缺少分类，结果可能不完整"))
			continue
		}
		if bytes.Equal(raw, []byte("null")) {
			continue
		}
		if raw[0] == '[' {
			var empty []json.RawMessage
			if json.Unmarshal(raw, &empty) == nil && len(empty) == 0 {
				continue
			}
			problems = append(problems, errors.New("Pixiv 用户作品列表分类格式无效"))
			continue
		}
		var works map[string]json.RawMessage
		if err := json.Unmarshal(raw, &works); err != nil || works == nil {
			problems = append(problems, errors.New("Pixiv 用户作品列表分类格式无效"))
			continue
		}
		invalid := 0
		for id := range works {
			if !pixivValidID(id) {
				invalid++
				continue
			}
			seen[id] = true
		}
		if invalid > 0 {
			problems = append(problems, fmt.Errorf("Pixiv 用户作品列表含 %d 个无效作品 ID", invalid))
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := strings.TrimLeft(ids[i], "0"), strings.TrimLeft(ids[j], "0")
		if len(left) != len(right) {
			return len(left) > len(right)
		}
		if left != right {
			return left > right
		}
		return ids[i] > ids[j]
	})
	return ids, errors.Join(problems...)
}
