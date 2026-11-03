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

const album1688PageSize = 20

// shortcut: 最多读取 1000 个相册；需要更大店铺时调整上限。
const max1688Albums = 1000

var album1688Member = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_$])["']?member_id["']?\s*:\s*(?:"([^"\\\r\n]{1,256})"|'([^'\\\r\n]{1,256})')`)
var album1688ID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func is1688AlbumList(u *url.URL) bool {
	return is1688Shop(u) && u.Path == "/page/albumlist.htm"
}

func is1688Shop(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".1688.com") {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, ".1688.com"), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

func album1688DetailID(u *url.URL) string {
	if !is1688Shop(u) || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	for _, prefix := range []string{"/app/albumDetail_", "/page/albumdetail_"} {
		if strings.HasPrefix(u.Path, prefix) && strings.HasSuffix(u.Path, ".htm") {
			id := strings.TrimSuffix(strings.TrimPrefix(u.Path, prefix), ".htm")
			if album1688ID.MatchString(id) {
				return id
			}
		}
	}
	return ""
}

func discover1688Album(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	id := album1688DetailID(root)
	result := Discovery{IsPage: true, Title: "相册 " + id}
	detail := *root
	detail.Path, detail.RawPath = "/app/albumDetail_"+id+".htm", ""
	query := detail.Query()
	query.Del("imageNum")
	detail.RawQuery = query.Encode()
	images, err := album1688Detail(ctx, getter, &detail, root, id)
	seen := map[string]bool{}
	for _, image := range images {
		if seen[image] {
			continue
		}
		seen[image] = true
		item := Item{URL: image, Metadata: map[string]string{"album_id": id}}
		if ref := imageReferer(pageReferer(&detail), image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		result.Items = append(result.Items, item)
	}
	return result, err
}

func discover1688Albums(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !album1688SameSource(root, final) {
		return result, errors.New("1688 相册页面不可用或跳转到了其他站点")
	}
	member := ""
	walkHTML(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if member != "" || (n.Type != html.TextNode && n.Type != html.CommentNode) {
			return
		}
		if m := album1688Member.FindStringSubmatch(n.Data); len(m) == 3 {
			member = strings.TrimSpace(firstNonEmpty(m[1], m[2]))
		}
	})
	if member == "" || strings.ContainsAny(member, "\x00\t\r\n") {
		return result, errors.New("1688 相册页面没有可用的 member_id，可能需要登录")
	}
	origin := *final
	origin.User = nil
	origin.Path, origin.RawPath, origin.RawQuery, origin.Fragment, origin.ForceQuery = "", "", "", "", false
	seenAlbums, seenImages := map[string]bool{}, map[string]bool{}
	var failures []error
	partial, unknown := 0, 0
	finish := func(err error) (Discovery, error) {
		if partial > 0 {
			failures = append(failures, fmt.Errorf("%d 个相册的图片数量与列表不一致，结果可能不完整", partial))
		}
		if unknown > 0 {
			failures = append(failures, fmt.Errorf("%d 个相册未提供图片数量，完整性未知", unknown))
		}
		return result, errors.Join(append(failures, err)...)
	}
	for page := 0; page < maxCrawlPages; page++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		start, end := page*album1688PageSize+1, (page+1)*album1688PageSize
		callback := "albumList" + strconv.Itoa(start)
		query := url.Values{"memberId": {member}, "jsResponseDataName": {callback}, "hasAlbums": {"true"}, "start": {strconv.Itoa(start)}, "end": {strconv.Itoa(end)}}
		api := &url.URL{Scheme: "https", Host: "picman.1688.com", Path: "/open/ajax/RecommendAlbumDetailList.json", RawQuery: query.Encode()}
		body, err := album1688Request(ctx, getter, api, final)
		if err != nil {
			return finish(fmt.Errorf("读取 1688 相册列表失败: %w", err))
		}
		var data struct {
			DataList *struct {
				Albums json.RawMessage `json:"albums"`
				Total  json.RawMessage `json:"total"`
			} `json:"dataList"`
			Total json.RawMessage `json:"total"`
		}
		if err := album1688Decode(body, callback, &data); err != nil || data.DataList == nil || len(data.DataList.Albums) == 0 || bytes.Equal(data.DataList.Albums, []byte("null")) {
			return finish(errors.New("1688 相册接口响应无效，可能需要登录"))
		}
		var albums []json.RawMessage
		if json.Unmarshal(data.DataList.Albums, &albums) != nil {
			return finish(errors.New("1688 相册列表格式无效"))
		}
		total, hasTotal := album1688Number(data.DataList.Total)
		if !hasTotal {
			total, hasTotal = album1688Number(data.Total)
		}
		if len(albums) == 0 {
			if hasTotal && total >= start {
				return finish(errors.New("1688 相册列表提前结束，结果可能不完整"))
			}
			return finish(nil)
		}
		newAlbums := 0
		for i, raw := range albums {
			if err := ctx.Err(); err != nil {
				return finish(err)
			}
			var album map[string]json.RawMessage
			if json.Unmarshal(raw, &album) != nil {
				failures = append(failures, fmt.Errorf("第 %d 条 1688 相册格式无效", start+i))
				continue
			}
			id := album1688Value(album["id"])
			if !album1688ID.MatchString(id) {
				failures = append(failures, fmt.Errorf("第 %d 条 1688 相册缺少有效 ID", start+i))
				continue
			}
			if seenAlbums[id] {
				continue
			}
			if len(seenAlbums) >= max1688Albums {
				return finish(fmt.Errorf("1688 相册已达到 %d 个详情页的扫描上限，结果可能不完整", max1688Albums))
			}
			seenAlbums[id] = true
			newAlbums++
			detail := origin
			detail.Path = "/app/albumDetail_" + id + ".htm"
			count, hasCount := album1688Number(album["count"])
			if hasCount && count == 0 {
				continue
			}
			images, detailErr := album1688Detail(ctx, getter, &detail, final, id)
			for _, image := range images {
				if seenImages[image] {
					continue
				}
				seenImages[image] = true
				item := Item{URL: image, Metadata: map[string]string{"album_id": id}}
				if title := album1688Value(album["title"]); title != "" {
					item.Metadata["album_title"] = title
				}
				if count := album1688Value(album["count"]); count != "" {
					item.Metadata["album_count"] = count
				}
				if ref := imageReferer(detail.String(), image); ref != "" {
					item.Headers = map[string]string{"Referer": ref}
				}
				result.Items = append(result.Items, item)
			}
			if detailErr != nil {
				if ctx.Err() != nil {
					return finish(ctx.Err())
				}
				failures = append(failures, fmt.Errorf("读取相册 %s 失败: %w", id, detailErr))
			}
			if !hasCount {
				unknown++
			} else if len(images) != count {
				partial++
			}
		}
		if hasTotal && end >= total {
			if total < start-1+len(albums) {
				return finish(errors.New("1688 相册总数与返回的列表不一致"))
			}
			return finish(nil)
		}
		if newAlbums == 0 {
			return finish(errors.New("1688 相册分页没有返回新相册，结果可能不完整"))
		}
	}
	return finish(fmt.Errorf("1688 相册已达到 %d 页的扫描上限，结果可能不完整", maxCrawlPages))
}

func album1688SameSource(root, final *url.URL) bool {
	return sameOrigin(root, final) || (root != nil && final != nil && root.Scheme == "http" && final.Scheme == "https" && strings.EqualFold(root.Host, final.Host))
}

// shortcut: 每个相册最多读取 1000 个图片页；需要更大相册时调整上限。
const max1688AlbumImages = 1000

func album1688Detail(ctx context.Context, getter Getter, detail, source *url.URL, id string) ([]string, error) {
	var images []string
	page := *detail
	photoPath := "/page/albumdetail_" + id + ".htm"
	seenPages := map[string]bool{}
	for number := 1; number <= max1688AlbumImages; number++ {
		if err := ctx.Err(); err != nil {
			return images, err
		}
		if seenPages[page.String()] {
			return images, errors.New("相册图片分页重复，结果可能不完整")
		}
		seenPages[page.String()] = true
		doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: page.String(), referer: pageReferer(source), depth: 2})
		if response != nil {
			response.Body.Close()
		}
		if err != nil {
			return images, err
		}
		if doc == nil || final.User != nil || !sameOrigin(detail, final) || (final.Path != detail.Path && final.Path != photoPath) {
			return images, errors.New("相册详情不可用或跳转到了其他页面")
		}
		image, next, found := "", "", false
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode {
				return
			}
			for _, class := range strings.Fields(attribute(n, "class")) {
				switch class {
				case "img-wrapper":
					walkHTML(n, func(child *html.Node) {
						if !found && child.Type == html.ElementNode && child.Data == "img" {
							image, found = attribute(child, "src"), true
						}
					})
				case "big-img-field":
					walkHTML(n, func(child *html.Node) {
						if child.Type != html.ElementNode || child.Data != "a" {
							return
						}
						classes := " " + strings.Join(strings.Fields(attribute(child, "class")), " ") + " "
						if strings.Contains(classes, " right ") && !strings.Contains(classes, " right-disable ") {
							next = attribute(child, "href")
						}
					})
				}
			}
		})
		u, err := crawlURL(documentBase(doc, final), image)
		if !found || err != nil || u.User != nil {
			return images, errors.New("相册详情没有可用的图片")
		}
		images = append(images, u.String())
		if next == "" {
			return images, nil
		}
		u, err = crawlURL(final, next)
		if err != nil || u.User != nil || !sameOrigin(detail, u) || (u.Path != detail.Path && u.Path != photoPath) {
			return images, errors.New("相册下一张图片地址无效或不属于当前相册")
		}
		values := u.Query()["imageNum"]
		if len(values) != 1 || values[0] != strconv.Itoa(number+1) {
			return images, errors.New("相册图片分页顺序无效，结果可能不完整")
		}
		u.RawQuery = url.Values{"imageNum": {values[0]}}.Encode()
		page = *u
	}
	return images, fmt.Errorf("相册已达到 %d 张图片的扫描上限，结果可能不完整", max1688AlbumImages)
}

func album1688Request(ctx context.Context, getter Getter, api, source *url.URL) ([]byte, error) {
	resp, err := getter.Get(ctx, api.String(), func(r *http.Request) {
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Del("Referer")
		r.Header.Set("Accept", "application/json,text/javascript,*/*;q=0.8")
		r.Header.Set("Accept-Encoding", "identity")
		if sameOrigin(api, r.URL) {
			if ref := imageReferer(pageReferer(source), r.URL.String()); ref != "" {
				r.Header.Set("Referer", ref)
			}
		}
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &TransferError{Err: err}
	}
	defer resp.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, api.String()))
	if err != nil || !sameOrigin(api, final) {
		return nil, errors.New("1688 相册接口跳转到了其他站点")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: final.String(), StatusCode: resp.StatusCode,
			RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	if len(raw) > maxPageBytes {
		return nil, errors.New("1688 相册响应超过 8 MiB")
	}
	if length := contentLength(resp); length >= 0 && length != int64(len(raw)) {
		return nil, &IntegrityError{Err: errors.New("1688 相册响应长度不符")}
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, &TransferError{Err: err}
		}
		defer gz.Close()
		reader = gz
	case "deflate":
		zr, err := zlib.NewReader(reader)
		if err != nil {
			return nil, &TransferError{Err: err}
		}
		defer zr.Close()
		reader = zr
	default:
		return nil, errors.New("1688 相册响应使用了不支持的压缩格式")
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	if len(decoded) > maxPageBytes {
		return nil, errors.New("1688 相册响应超过 8 MiB")
	}
	return decoded, nil
}

func album1688Decode(body []byte, callback string, target any) error {
	data := bytes.TrimSpace(body)
	if json.Valid(data) {
		return json.Unmarshal(data, target)
	}
	data = bytes.TrimSpace(bytes.TrimSuffix(data, []byte(";")))
	if bytes.HasPrefix(data, []byte("var ")) {
		data = bytes.TrimSpace(data[len("var "):])
	}
	if !bytes.HasPrefix(data, []byte(callback)) {
		return errors.New("未知的相册响应包装")
	}
	data = bytes.TrimSpace(data[len(callback):])
	if len(data) >= 2 && data[0] == '(' && data[len(data)-1] == ')' {
		data = bytes.TrimSpace(data[1 : len(data)-1])
	} else if len(data) >= 1 && data[0] == '=' {
		data = bytes.TrimSpace(data[1:])
	} else {
		return errors.New("未知的相册响应包装")
	}
	return json.Unmarshal(data, target)
}

func album1688Value(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	value = strings.TrimSpace(string(raw))
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		return value
	}
	return ""
}

func album1688Number(raw json.RawMessage) (int, bool) {
	value, err := strconv.Atoi(album1688Value(raw))
	return value, err == nil && value >= 0
}
