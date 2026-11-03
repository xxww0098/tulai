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
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

var (
	flickrNumberPattern = regexp.MustCompile(`^[0-9]{1,24}$`)
	flickrOwnerPattern  = regexp.MustCompile(`^[A-Za-z0-9@_-]{1,100}$`)
	flickrHostPattern   = regexp.MustCompile(`^(?:live|farm[0-9]+|c[0-9]+)\.staticflickr\.com$`)
	flickrExportPattern = regexp.MustCompile(`\bmodelExport\s*:\s*`)
	flickrKeyPattern    = regexp.MustCompile(`\bYUI_config\s*\.\s*flickr\s*\.\s*api\s*\.\s*site_key\s*=\s*["']([a-fA-F0-9]{32})["']`)
)

// Flickr's original and large sizes have distinct secrets; only use URLs supplied by Flickr.
var flickrSizes = []string{"o", "6k", "5k", "4k", "3k", "k", "h", "f", "l", "c", "z", "m", "w", "n", "s", "t", "q", "sq"}

type flickrPageRef struct {
	id    string
	album bool
}

type flickrModel struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	IsVideo   bool   `json:"isVideo"`
	MediaType string `json:"mediaType"`
	Sizes     struct {
		Data map[string]struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		} `json:"data"`
	} `json:"sizes"`
}

type flickrSet struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	PhotoPageList *struct {
		Data struct {
			Items []struct {
				Data flickrModel `json:"data"`
			} `json:"_data"`
			TotalItems *int `json:"totalItems"`
			FetchedEnd bool `json:"fetchedEnd"`
		} `json:"data"`
	} `json:"photoPageList"`
}

type flickrExport struct {
	Main map[string][]struct {
		Data json.RawMessage `json:"data"`
	} `json:"main"`
}

type flickrFound struct {
	id      string
	item    Item
	rank    int
	removed bool
}

type flickrCollection struct {
	items []flickrFound
	index map[string]int
}

func flickrRef(u *url.URL) flickrPageRef {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return flickrPageRef{}
	}
	switch strings.ToLower(u.Hostname()) {
	case "flickr.com", "www.flickr.com", "m.flickr.com":
	default:
		return flickrPageRef{}
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		return flickrPageRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "photos" || !flickrOwnerPattern.MatchString(parts[1]) {
		return flickrPageRef{}
	}
	if parts[2] == "albums" || parts[2] == "sets" {
		if len(parts) != 4 || !flickrNumberPattern.MatchString(parts[3]) {
			return flickrPageRef{}
		}
		return flickrPageRef{id: parts[3], album: true}
	}
	if !flickrNumberPattern.MatchString(parts[2]) {
		return flickrPageRef{}
	}
	if len(parts) > 3 && !(len(parts) == 5 && parts[3] == "in") && !(len(parts) <= 5 && parts[3] == "sizes") {
		return flickrPageRef{}
	}
	return flickrPageRef{id: parts[2]}
}

func isFlickrPage(u *url.URL) bool { return flickrRef(u).id != "" }

func discoverFlickr(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	want := flickrRef(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || want.id == "" {
		return result, errors.New("Flickr 页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || flickrRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("Flickr 页面不可用或跳转到了其他照片或相册")
	}
	return flickrCollect(ctx, getter, final, doc)
}

// flickrCollect works with fetched HTML and the current rendered browser document.
func flickrCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := flickrRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || ref.id == "" || doc == nil {
		return result, errors.New("Flickr 页面或下载客户端无效")
	}
	var export *flickrExport
	var key, cover string
	var parseErr error
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data == "meta" && strings.EqualFold(attribute(n, "property"), "og:image") {
			cover = firstNonEmpty(cover, attribute(n, "content"))
		}
		if n.Data == "img" && n.Parent != nil && attribute(n.Parent, "id") == "allsizes-photo" {
			cover = firstNonEmpty(cover, attribute(n, "src"))
		}
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			if key == "" {
				for _, loc := range offer1688CodeMatches(text, flickrKeyPattern) {
					key = text[loc[2]:loc[3]]
					break
				}
			}
			if export != nil {
				continue
			}
			for _, loc := range offer1688CodeMatches(text, flickrExportPattern) {
				var candidate flickrExport
				if json.NewDecoder(strings.NewReader(text[loc[1]:])).Decode(&candidate) != nil || candidate.Main == nil {
					parseErr = errors.Join(parseErr, errors.New("无法读取 Flickr 照片数据"))
					continue
				}
				export = &candidate
				break
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	found := flickrCollection{index: make(map[string]int)}
	if !ref.album {
		matched := false
		if export != nil {
			for _, entry := range export.Main["photo-models"] {
				var photo flickrModel
				if json.Unmarshal(entry.Data, &photo) == nil && photo.ID == ref.id {
					matched = true
					result.Title = firstNonEmpty(photo.Title, result.Title)
					parseErr = errors.Join(parseErr, found.addModel(page, photo))
					break
				}
			}
		}
		if !matched || len(found.items) == 0 {
			found.add(page, ref.id, cover, len(flickrSizes))
			parseErr = errors.Join(parseErr, errors.New("Flickr 未提供完整照片尺寸数据，结果可能不完整"))
		}
		result.Items = found.result()
		return result, parseErr
	}
	var set *flickrSet
	if export != nil {
		for _, entry := range export.Main["set-models"] {
			var candidate flickrSet
			if json.Unmarshal(entry.Data, &candidate) == nil && candidate.ID == ref.id && candidate.PhotoPageList != nil {
				set = &candidate
				break
			}
		}
	}
	if set == nil {
		return result, errors.Join(parseErr, errors.New("Flickr 相册数据不可用，可能需要登录或安全验证"))
	}
	result.Title = firstNonEmpty(set.Title, result.Title)
	list := set.PhotoPageList.Data
	var modelErr error
	for _, photo := range list.Items {
		if err := ctx.Err(); err != nil {
			result.Items = found.result()
			return result, err
		}
		modelErr = errors.Join(modelErr, found.addModel(page, photo.Data))
	}
	if key == "" || page.Scheme != "https" {
		parseErr = errors.Join(parseErr, modelErr)
		if !list.FetchedEnd || list.TotalItems == nil || *list.TotalItems != len(list.Items) {
			parseErr = errors.Join(parseErr, errors.New("Flickr 未提供相册分页所需的数据，结果可能不完整"))
		}
	} else if apiErr := flickrAlbumPages(ctx, getter, page, ref.id, key, &found); apiErr != nil {
		parseErr = errors.Join(parseErr, modelErr, apiErr)
	}
	result.Items = found.result()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, parseErr
}

func (f *flickrCollection) addModel(page *url.URL, photo flickrModel) error {
	if photo.IsVideo || strings.EqualFold(photo.MediaType, "video") {
		f.remove(photo.ID)
		return nil
	}
	if !flickrNumberPattern.MatchString(photo.ID) {
		return errors.New("Flickr 照片编号无效")
	}
	invalid := false
	for rank, size := range flickrSizes {
		if entry, ok := photo.Sizes.Data[size]; ok && entry.Data.URL != "" {
			if f.add(page, photo.ID, entry.Data.URL, rank) {
				if invalid {
					return errors.New("Flickr 照片的部分尺寸地址无效")
				}
				return nil
			}
			invalid = true
		}
	}
	return errors.New("Flickr 未提供可用的照片地址")
}

func (f *flickrCollection) add(page *url.URL, id, raw string, rank int) bool {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || !flickrHostPattern.MatchString(strings.ToLower(u.Hostname())) || !strings.HasPrefix(path.Base(u.Path), id+"_") || !imagePath(u.Path) {
		return false
	}
	item := Item{URL: u.String()}
	if referer := imageReferer(page.String(), item.URL); referer != "" {
		item.Headers = map[string]string{"Referer": referer}
	}
	if index, ok := f.index[id]; ok {
		if f.items[index].removed || rank < f.items[index].rank {
			f.items[index] = flickrFound{id: id, item: item, rank: rank}
		}
	} else {
		f.index[id] = len(f.items)
		f.items = append(f.items, flickrFound{id: id, item: item, rank: rank})
	}
	return true
}

func (f *flickrCollection) remove(id string) {
	if i, ok := f.index[id]; ok {
		f.items[i].removed = true
	}
}

func (f *flickrCollection) result() []Item {
	var items []Item
	for _, found := range f.items {
		if !found.removed {
			items = append(items, found.item)
		}
	}
	return items
}

// shortcut: Flickr albums are capped at 100 API pages; raise this for albums above 5,000 media.
func flickrAlbumPages(ctx context.Context, getter Getter, page *url.URL, albumID, key string, found *flickrCollection) error {
	seen := make(map[string]bool)
	pages, total := 1, -1
	var problems []error
	for number := 1; number <= pages && number <= maxCrawlPages; number++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		query := url.Values{
			"method": {"flickr.photosets.getPhotos"}, "api_key": {key}, "photoset_id": {albumID},
			"extras": {"url_o,url_6k,url_5k,url_4k,url_3k,url_k,url_h,url_f,url_l,url_c,url_z,url_m,url_w,url_n,url_s,url_t,url_q,url_sq,media"},
			"format": {"json"}, "nojsoncallback": {"1"}, "page": {strconv.Itoa(number)}, "per_page": {"50"},
		}
		api := &url.URL{Scheme: "https", Host: page.Host, Path: "/services/rest/", RawQuery: query.Encode()}
		body, err := flickrRequest(ctx, getter, api, page)
		if err != nil {
			return errors.Join(append(problems, fmt.Errorf("读取 Flickr 相册第 %d 页失败，结果可能不完整: %w", number, err))...)
		}
		var response struct {
			Stat     string `json:"stat"`
			Photoset struct {
				ID    string            `json:"id"`
				Title string            `json:"title"`
				Page  json.RawMessage   `json:"page"`
				Pages json.RawMessage   `json:"pages"`
				Total json.RawMessage   `json:"total"`
				Photo []json.RawMessage `json:"photo"`
			} `json:"photoset"`
		}
		if json.Unmarshal(body, &response) != nil || response.Stat != "ok" || response.Photoset.ID != albumID {
			return errors.Join(append(problems, errors.New("Flickr 相册接口返回无效数据或需要登录，结果可能不完整"))...)
		}
		current, ok1 := flickrInt(response.Photoset.Page)
		pageCount, ok2 := flickrInt(response.Photoset.Pages)
		count, ok3 := flickrInt(response.Photoset.Total)
		if !ok1 || !ok2 || !ok3 || current != number || pageCount < 1 || count < 0 || (number > 1 && (pageCount != pages || count != total)) {
			return errors.Join(append(problems, errors.New("Flickr 相册分页数据不一致，结果可能不完整"))...)
		}
		pages, total = pageCount, count
		for _, raw := range response.Photoset.Photo {
			if err := ctx.Err(); err != nil {
				return err
			}
			var photo map[string]json.RawMessage
			if json.Unmarshal(raw, &photo) != nil {
				problems = append(problems, errors.New("Flickr 照片数据无效"))
				continue
			}
			var id, media string
			_ = json.Unmarshal(photo["id"], &id)
			_ = json.Unmarshal(photo["media"], &media)
			if !flickrNumberPattern.MatchString(id) || seen[id] {
				problems = append(problems, errors.New("Flickr 相册照片编号无效或重复"))
				continue
			}
			seen[id] = true
			if strings.EqualFold(media, "video") {
				found.remove(id)
				continue
			}
			valid, invalid := false, false
			for rank, size := range flickrSizes {
				var rawURL string
				if json.Unmarshal(photo["url_"+size], &rawURL) != nil || rawURL == "" {
					continue
				}
				if found.add(page, id, rawURL, rank) {
					valid = true
					break
				}
				invalid = true
			}
			if !valid || invalid {
				problems = append(problems, errors.New("Flickr 相册照片的部分地址无效或不可用"))
			}
		}
		if len(response.Photoset.Photo) == 0 && number < pages {
			return errors.Join(append(problems, errors.New("Flickr 相册分页提前结束，结果可能不完整"))...)
		}
	}
	if pages > maxCrawlPages || len(seen) != total {
		problems = append(problems, errors.New("Flickr 相册未返回全部媒体，结果可能不完整"))
	} else {
		for _, item := range found.items {
			if !seen[item.id] {
				found.remove(item.id)
			}
		}
	}
	return errors.Join(problems...)
}

func flickrInt(raw json.RawMessage) (int, bool) {
	var value int
	if json.Unmarshal(raw, &value) == nil {
		return value, true
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return 0, false
	}
	value, err := strconv.Atoi(text)
	return value, err == nil
}

func flickrRequest(ctx context.Context, getter Getter, api, source *url.URL) ([]byte, error) {
	resp, err := getter.Get(ctx, api.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json,*/*;q=0.8")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", source.Scheme+"://"+source.Host+"/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &TransferError{Err: errors.New("Flickr 接口请求失败")}
	}
	defer resp.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, api.String()))
	if err != nil || final.User != nil || !sameOrigin(api, final) || (final.Path != "/services/rest" && final.Path != "/services/rest/") || final.Query().Get("method") != api.Query().Get("method") || final.Query().Get("photoset_id") != api.Query().Get("photoset_id") || final.Query().Get("api_key") != api.Query().Get("api_key") || final.Query().Get("page") != api.Query().Get("page") {
		return nil, errors.New("Flickr 接口跳转到了其他地址")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: api.Scheme + "://" + api.Host + api.Path, StatusCode: resp.StatusCode, RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, &TransferError{Err: errors.New("Flickr 接口响应读取失败")}
	}
	if len(raw) > maxPageBytes {
		return nil, errors.New("Flickr 接口响应超过 8 MiB")
	}
	if length := contentLength(resp); length >= 0 && length != int64(len(raw)) {
		return nil, &IntegrityError{Err: errors.New("Flickr 接口响应长度不符")}
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, errors.New("Flickr 接口压缩数据无效")
		}
		defer gz.Close()
		reader = gz
	case "deflate":
		zr, err := zlib.NewReader(reader)
		if err != nil {
			return nil, errors.New("Flickr 接口压缩数据无效")
		}
		defer zr.Close()
		reader = zr
	default:
		return nil, errors.New("Flickr 接口使用了不支持的压缩格式")
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if err != nil || len(decoded) > maxPageBytes {
		return nil, errors.New("Flickr 接口响应无效或超过 8 MiB")
	}
	return decoded, nil
}
