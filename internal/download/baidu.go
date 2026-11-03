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
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const baiduPageSize = 30

func isBaiduImageSearch(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Hostname(), "image.baidu.com") || u.Path != "/search/index" {
		return false
	}
	if p := u.Port(); p != "" && !((u.Scheme == "https" && p == "443") || (u.Scheme == "http" && p == "80")) {
		return false
	}
	q := u.Query()
	return strings.TrimSpace(firstNonEmpty(q.Get("word"), q.Get("queryWord"))) != "" && !q.Has("queryImageUrl") && !q.Has("querySign")
}

func discoverBaiduImages(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	word := strings.TrimSpace(firstNonEmpty(root.Query().Get("word"), root.Query().Get("queryWord")))
	result := Discovery{IsPage: true, Title: word + " - 百度图片"}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return result, err
	}
	seed := *root
	seed.Scheme, seed.Host, seed.User = "https", "image.baidu.com", nil
	home := &url.URL{Scheme: "https", Host: "image.baidu.com", Path: "/"}
	_, err = baiduRequest(ctx, getter, home, &seed, jar, false)
	if err != nil {
		return result, err
	}

	query := seed.Query()
	query.Set("tn", "resultjson_com")
	query.Set("ipn", "rj")
	if query.Get("ct") == "" {
		query.Set("ct", "201326592")
	}
	query.Set("fp", "result")
	query.Set("ie", "utf-8")
	query.Set("oe", "utf-8")
	query.Set("word", word)
	query.Set("queryWord", word)
	query.Set("rn", strconv.Itoa(baiduPageSize))
	query.Del("callback")
	start, _ := strconv.Atoi(query.Get("pn"))
	if start < 0 {
		start = 0
	}
	if start > int(^uint(0)>>1)-maxCrawlPages*baiduPageSize {
		return result, errors.New("百度图片搜索起始位置过大")
	}
	seen := map[string]bool{}
	missing := 0
	total, hasTotal := 0, false
	finish := func(err error) (Discovery, error) {
		if missing > 0 {
			err = errors.Join(err, fmt.Errorf("百度图片有 %d 条结果缺少可用图片地址", missing))
		}
		return result, err
	}
	for page := 0; page < maxCrawlPages; page++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		offset := start + page*baiduPageSize
		query.Set("pn", strconv.Itoa(offset))
		api := &url.URL{Scheme: "https", Host: "image.baidu.com", Path: "/search/acjson", RawQuery: query.Encode()}
		body, err := baiduRequest(ctx, getter, api, &seed, jar, true)
		if err != nil {
			return finish(err)
		}
		var response struct {
			Data       json.RawMessage `json:"data"`
			AntiFlag   json.RawMessage `json:"antiFlag"`
			DisplayNum json.RawMessage `json:"displayNum"`
		}
		if json.Unmarshal(body, &response) != nil || string(response.AntiFlag) == "1" || string(response.AntiFlag) == `"1"` || len(response.Data) == 0 || bytes.Equal(response.Data, []byte("null")) {
			return finish(errors.New("百度图片搜索接口未返回结果，可能需要验证"))
		}
		var rows []json.RawMessage
		if json.Unmarshal(response.Data, &rows) != nil {
			return finish(errors.New("百度图片搜索结果格式无效"))
		}
		if !hasTotal {
			total, hasTotal = baiduDisplayNum(response.DisplayNum)
		}
		nonempty, added := 0, 0
		for _, raw := range rows {
			var row map[string]json.RawMessage
			if err := json.Unmarshal(raw, &row); err != nil {
				nonempty++
				missing++
				continue
			}
			if len(row) == 0 {
				continue
			}
			nonempty++
			image := baiduImageURL(row)
			if image == "" {
				missing++
				continue
			}
			if seen[image] {
				continue
			}
			seen[image] = true
			added++
			from := baiduDecodeURL(baiduString(row["fromURL"]))
			if from == "" {
				from = pageReferer(&seed)
			}
			item := Item{URL: image, PublicOnly: true}
			if ref := imageReferer(from, image); ref != "" {
				item.Headers = map[string]string{"Referer": ref}
			}
			result.Items = append(result.Items, item)
		}
		if nonempty == 0 || (hasTotal && offset/baiduPageSize >= total/baiduPageSize) {
			return finish(nil)
		}
		if added == 0 {
			return finish(errors.New("百度图片分页没有返回新图片，结果可能不完整"))
		}
	}
	return finish(fmt.Errorf("百度图片已达到 %d 页（最多 %d 条）的抓取上限，结果可能不完整", maxCrawlPages, maxCrawlPages*baiduPageSize))
}

func baiduDisplayNum(raw json.RawMessage) (int, bool) {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, `"`) {
		if json.Unmarshal(raw, &value) != nil {
			return 0, false
		}
	}
	total, err := strconv.Atoi(strings.TrimSpace(value))
	return total, err == nil && total >= 0
}

func baiduRequest(ctx context.Context, getter Getter, target, seed *url.URL, jar *cookiejar.Jar, readBody bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := getter.Get(ctx, target.String(), func(r *http.Request) {
		r.Header.Set("Accept-Encoding", "identity")
		if readBody {
			r.Header.Set("Accept", "application/json,text/javascript,*/*;q=0.8")
			r.Header.Set("Referer", pageReferer(seed))
			if r.URL.Scheme == "https" && r.URL.Host == "image.baidu.com" && r.URL.Path == "/search/acjson" {
				for _, cookie := range jar.Cookies(r.URL) {
					r.AddCookie(cookie)
				}
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
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, target.String()))
	if err != nil || final.Scheme != "https" || final.Host != "image.baidu.com" {
		return nil, errors.New("百度图片搜索被重定向到其他站点")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: final.String(), StatusCode: resp.StatusCode,
			RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	jar.SetCookies(final, (&http.Response{Header: resp.Header}).Cookies())
	if !readBody {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	if len(raw) > maxPageBytes {
		return nil, errors.New("百度图片结果超过 8 MiB 的扫描上限")
	}
	if length := contentLength(resp); length >= 0 && length != int64(len(raw)) {
		return nil, &IntegrityError{Err: errors.New("百度图片结果长度不符")}
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
		return nil, errors.New("百度图片结果使用了不支持的压缩格式")
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if err != nil {
		return nil, &TransferError{Err: err}
	}
	if len(decoded) > maxPageBytes {
		return nil, errors.New("百度图片结果超过 8 MiB 的扫描上限")
	}
	return decoded, nil
}

func baiduImageURL(row map[string]json.RawMessage) string {
	if image := baiduDecodeURL(baiduString(row["objURL"])); image != "" {
		return image
	}
	var replacements []map[string]json.RawMessage
	if json.Unmarshal(row["replaceUrl"], &replacements) == nil {
		for _, replacement := range replacements {
			for _, key := range []string{"ObjURL", "ObjUrl"} {
				if image := baiduDecodeURL(baiduString(replacement[key])); image != "" {
					return image
				}
			}
		}
	}
	for _, key := range []string{"hoverURL", "largeTnImageUrl", "middleURL", "thumbURL"} {
		if image := baiduDecodeURL(baiduString(row[key])); image != "" {
			return image
		}
	}
	return ""
}

func baiduString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

var baiduURLReplacer = func() *strings.Replacer {
	pairs := []string{"_z2C$q", ":", "_z&e3B", ".", "AzdH3F", "/"}
	encoded, plain := "wkv1ju2it3hs4g5rq6fp7eo8dn9cm0bla", "abcdefghijklmnopqrstuvw1234567890"
	for i := range encoded {
		pairs = append(pairs, encoded[i:i+1], plain[i:i+1])
	}
	return strings.NewReplacer(pairs...)
}()

func baiduDecodeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if u, err := crawlURL(nil, raw); err == nil {
		return u.String()
	}
	if u, err := crawlURL(nil, baiduURLReplacer.Replace(raw)); err == nil {
		return u.String()
	}
	return ""
}
