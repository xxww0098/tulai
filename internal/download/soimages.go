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

	"golang.org/x/net/html"
)

var soImagesSession = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type soImagesResult struct {
	ID    json.RawMessage `json:"id"`
	Title json.RawMessage `json:"title"`
	Image json.RawMessage `json:"img"`
	Link  json.RawMessage `json:"link"`
}

type soImagesData struct {
	End       bool              `json:"end"`
	LastIndex json.RawMessage   `json:"lastindex"`
	PS        json.RawMessage   `json:"ps"`
	PC        json.RawMessage   `json:"pc"`
	SID       string            `json:"sid"`
	RAS       json.RawMessage   `json:"ras"`
	CN        json.RawMessage   `json:"cn"`
	GN        json.RawMessage   `json:"gn"`
	KN        json.RawMessage   `json:"kn"`
	Cuben     json.RawMessage   `json:"cuben"`
	Pornn     json.RawMessage   `json:"pornn"`
	Manun     json.RawMessage   `json:"manun"`
	Adstar    json.RawMessage   `json:"adstar"`
	List      []json.RawMessage `json:"list"`
}

func soImagesString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func isSOImageSearch(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), "image.so.com") || u.Path != "/i" {
		return false
	}
	if p := u.Port(); p != "" && !(u.Scheme == "https" && p == "443") && !(u.Scheme == "http" && p == "80") {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q["q"]) != 1 || strings.TrimSpace(q.Get("q")) == "" || len(q.Get("q")) > 2048 {
		return false
	}
	return q.Get("tab") != "chacha"
}

func soImagesInt(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 || len(raw) > 20 {
		return 0, false
	}
	value := string(raw)
	if strings.HasPrefix(value, `"`) {
		if json.Unmarshal(raw, &value) != nil {
			return 0, false
		}
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n >= 0 && n <= 1000000
}

func discoverSOImages(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || !isSOImageSearch(root) {
		return result, errors.New("360 图片搜索地址或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !isSOImageSearch(final) || final.Query().Get("q") != root.Query().Get("q") || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("360 图片搜索页不可用或跳转到其他搜索")
	}
	return soImagesCollect(ctx, getter, final, doc)
}

func soImagesInit(doc *html.Node) (soImagesData, bool) {
	var data soImagesData
	found := false
	walkHTML(doc, func(n *html.Node) {
		if found || n.Type != html.ElementNode || n.Data != "script" || attribute(n, "id") != "initData" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode && json.Unmarshal([]byte(c.Data), &data) == nil && data.List != nil {
				found = true
				return
			}
		}
	})
	return data, found
}

func soImagesFilters(root *url.URL) url.Values {
	query := url.Values{"q": {root.Query().Get("q")}, "pd": {"1"}, "pn": {"60"}}
	for _, key := range []string{"qtag", "correct", "tab", "i", "cmg", "ie", "src", "keep", "id", "grpmd5", "query_tag", "zoom", "width", "height", "color", "t", "box", "bz_parent", "bz_child", "bz_height", "bz_width", "bz_color", "bz_type", "pic_type", "pic_style", "pic_effect", "pf_style", "pf_type", "emote_type"} {
		values := root.Query()[key]
		if len(values) == 1 && len(values[0]) <= 1024 {
			query.Set(key, values[0])
		}
	}
	return query
}

func soImagesRequest(ctx context.Context, getter Getter, endpoint, page *url.URL, callback string) (soImagesData, error) {
	var data soImagesData
	resp, err := getter.Get(ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", page.Scheme+"://"+page.Host+"/")
	})
	if err != nil {
		return data, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return data, fmt.Errorf("360 图片分页请求失败：HTTP %d", resp.StatusCode)
	}
	final, err := url.Parse(firstNonEmpty(resp.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(endpoint, final) || final.Path != "/j" ||
		final.Query().Get("q") != endpoint.Query().Get("q") || final.Query().Get("sn") != endpoint.Query().Get("sn") || final.Query().Get("callback") != callback {
		return data, errors.New("360 图片分页发生了不安全的跳转")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if err != nil || len(raw) > maxPageBytes {
		return data, errors.New("360 图片分页无法读取或超过大小上限")
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		decoded, err := gzip.NewReader(reader)
		if err != nil {
			return data, err
		}
		defer decoded.Close()
		reader = decoded
	case "deflate":
		decoded, err := zlib.NewReader(reader)
		if err != nil {
			return data, err
		}
		defer decoded.Close()
		reader = decoded
	default:
		return data, errors.New("360 图片分页使用了不支持的压缩格式")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if err != nil || len(body) > maxPageBytes {
		return data, errors.New("360 图片分页无法解压或超过大小上限")
	}
	text := strings.TrimSpace(string(body))
	prefix := callback + "("
	if !strings.HasPrefix(text, prefix) {
		return data, errors.New("360 图片分页格式无效或需要验证")
	}
	payload := text[len(prefix):]
	decoder := json.NewDecoder(strings.NewReader(payload))
	if decoder.Decode(&data) != nil || data.List == nil {
		return data, errors.New("360 图片分页数据无效")
	}
	tail := strings.TrimSpace(payload[decoder.InputOffset():])
	if tail != ")" && tail != ");" {
		return data, errors.New("360 图片分页包含无效内容")
	}
	return data, nil
}

// soImagesCollect parses the search page's public initial data, then fetches
// bounded JSONP pages as JSON without executing the callback.
func soImagesCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if doc == nil || getter == nil || !isSOImageSearch(page) {
		return result, errors.New("360 图片搜索页面无效")
	}
	result.Title = strings.TrimSpace(page.Query().Get("q")) + " - 360 图片"
	current, ok := soImagesInit(doc)
	if !ok {
		return result, errors.New("360 图片搜索未返回可识别的数据，可能需要验证")
	}
	query := soImagesFilters(page)
	seen := map[string]bool{}
	missing := 0
	finish := func(err error) (Discovery, error) {
		if missing > 0 {
			err = errors.Join(err, fmt.Errorf("360 图片有 %d 条结果缺少安全的原图地址", missing))
		}
		return result, err
	}
	previous := -1
	if value := page.Query().Get("sn"); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n >= 0 && n <= 1000000 {
			previous = n
		}
	}
	for count := 0; count < maxCrawlPages; count++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		added := 0
		for _, raw := range current.List {
			var image soImagesResult
			if json.Unmarshal(raw, &image) != nil {
				missing++
				continue
			}
			u, err := publicImageURL(soImagesString(image.Image))
			if err != nil {
				missing++
				continue
			}
			if seen[u.String()] {
				continue
			}
			seen[u.String()] = true
			added++
			item := Item{URL: u.String(), RequireImage: true, PublicOnly: true, Metadata: map[string]string{"search_title": soImagesString(image.Title), "search_id": soImagesString(image.ID)}}
			if source, err := publicImageURL(soImagesString(image.Link)); err == nil {
				if ref := imageReferer(source.Scheme+"://"+source.Host+"/", item.URL); ref != "" {
					item.Headers = map[string]string{"Referer": ref}
				}
			}
			result.Items = append(result.Items, item)
		}
		if current.End || len(current.List) == 0 {
			return finish(nil)
		}
		if count > 0 && added == 0 {
			return finish(errors.New("360 图片分页未返回新图片，结果可能不完整"))
		}
		if count+1 >= maxCrawlPages {
			return finish(fmt.Errorf("360 图片已达到 %d 页扫描上限，结果可能不完整", maxCrawlPages))
		}
		next, valid := soImagesInt(current.LastIndex)
		if !valid || next <= previous {
			return finish(errors.New("360 图片分页位置无效或重复，结果可能不完整"))
		}
		previous = next
		ps, _ := soImagesInt(current.PS)
		pc, _ := soImagesInt(current.PC)
		query.Set("sn", strconv.Itoa(next))
		query.Set("ps", strconv.Itoa(ps))
		query.Set("pc", strconv.Itoa(pc))
		if soImagesSession.MatchString(current.SID) {
			query.Set("sid", current.SID)
		} else {
			query.Del("sid")
		}
		for key, raw := range map[string]json.RawMessage{"ras": current.RAS, "cn": current.CN, "gn": current.GN, "kn": current.KN, "cuben": current.Cuben, "pornn": current.Pornn, "manun": current.Manun, "adstar": current.Adstar} {
			if value, ok := soImagesInt(raw); ok {
				query.Set(key, strconv.Itoa(value))
			} else {
				query.Del(key)
			}
		}
		callback := fmt.Sprintf("jQuery130_%d", count+1)
		query.Set("callback", callback)
		endpoint := &url.URL{Scheme: page.Scheme, Host: page.Host, Path: "/j", RawQuery: query.Encode()}
		var err error
		current, err = soImagesRequest(ctx, getter, endpoint, page, callback)
		if err != nil {
			return finish(errors.Join(errors.New("360 图片分页失败，已保留部分结果"), err))
		}
	}
	return finish(fmt.Errorf("360 图片已达到 %d 页扫描上限，结果可能不完整", maxCrawlPages))
}
