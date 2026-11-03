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

var weiboDigits = regexp.MustCompile(`^[0-9]{1,24}$`)
var weiboPostKey = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
var weiboPID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var weiboCallback = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_.$]{0,120}$`)

// shortcut: 单个微博任务有页数、相册数、帖子数和图片条数上限；更大任务需要调整上限。
const (
	maxWeiboPages  = 100
	maxWeiboAlbums = 100
	maxWeiboPosts  = 1000
	maxWeiboRows   = 10000
)

var errWeiboRowLimit = errors.New("微博图片数量达到上限，结果可能不完整")

type weiboTarget struct {
	kind, id, uid, name, album, albumType string
}

func weiboContentTarget(u *url.URL) weiboTarget {
	var target weiboTarget
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return target
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch strings.ToLower(u.Hostname()) {
	case "weibo.com", "www.weibo.com":
		if len(parts) == 3 && parts[0] == "u" && weiboDigits.MatchString(parts[1]) && parts[2] == "album" {
			return weiboTarget{kind: "albums", uid: parts[1]}
		}
		if len(parts) == 2 && weiboDigits.MatchString(parts[0]) && parts[1] == "album" {
			return weiboTarget{kind: "albums", uid: parts[0]}
		}
		if len(parts) == 2 && (parts[0] == "u" || parts[0] == "n") && u.Query().Get("tabtype") == "album" {
			if parts[0] == "u" && weiboDigits.MatchString(parts[1]) {
				return weiboTarget{kind: "albums", uid: parts[1]}
			}
			if parts[0] == "n" && parts[1] != "" && len(parts[1]) <= 128 && !strings.ContainsAny(parts[1], "\x00\t\r\n") {
				return weiboTarget{kind: "albums", name: parts[1]}
			}
		}
		if len(parts) == 1 && weiboDigits.MatchString(parts[0]) && u.Query().Get("tabtype") == "album" {
			return weiboTarget{kind: "albums", uid: parts[0]}
		}
		if len(parts) == 2 && (parts[0] == "detail" || weiboDigits.MatchString(parts[0])) && weiboPostKey.MatchString(parts[1]) {
			return weiboTarget{kind: "post", id: parts[1]}
		}
	case "m.weibo.cn":
		if len(parts) == 2 && (parts[0] == "status" || parts[0] == "detail") && weiboPostKey.MatchString(parts[1]) {
			return weiboTarget{kind: "post", id: parts[1]}
		}
	case "photo.weibo.com":
		if len(parts) == 2 && weiboDigits.MatchString(parts[0]) && parts[1] == "albums" {
			return weiboTarget{kind: "old-albums", uid: parts[0]}
		}
		if len(parts) == 5 && weiboDigits.MatchString(parts[0]) && parts[1] == "albums" && parts[2] == "detail" && parts[3] == "album_id" && weiboDigits.MatchString(parts[4]) {
			typeValue := u.Query().Get("type")
			if typeValue != "" && !weiboDigits.MatchString(typeValue) {
				return target
			}
			return weiboTarget{kind: "old-album", uid: parts[0], album: parts[4], albumType: typeValue}
		}
	}
	return target
}

func isWeiboContent(u *url.URL) bool { return weiboContentTarget(u).kind != "" }

// discoverWeiboContent can use a public getter or the user's same-origin rendered getter.
func discoverWeiboContent(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	target := weiboContentTarget(root)
	if getter == nil || target.kind == "" {
		return result, errors.New("微博地址或下载客户端无效")
	}
	c := weiboCollector{ctx: ctx, getter: getter, source: root, result: result, seen: map[string]bool{}}
	var err error
	switch target.kind {
	case "post":
		if strings.EqualFold(root.Hostname(), "m.weibo.cn") {
			err = errors.New("移动版微博的图片接口尚未验证，请使用电脑版微博帖子地址")
		} else {
			err = c.post(target.id)
		}
	case "albums":
		if target.uid == "" {
			target.uid, err = c.userID(target.name)
		}
		if err == nil {
			err = c.albums(target.uid)
		}
	case "old-albums":
		err = c.oldAlbums(target.uid, "")
	case "old-album":
		if target.albumType == "" {
			err = c.oldAlbums(target.uid, target.album)
		} else {
			err = c.oldAlbum(target.uid, target.album, target.albumType, -1)
		}
	}
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	return c.result, errors.Join(append(c.problems, err)...)
}

type weiboCollector struct {
	ctx      context.Context
	getter   Getter
	source   *url.URL
	result   Discovery
	seen     map[string]bool
	posts    int
	rows     int
	problems []error
}

func (c *weiboCollector) nextRow() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if c.rows >= maxWeiboRows {
		return errWeiboRowLimit
	}
	c.rows++
	return nil
}

func (c *weiboCollector) api(path string, query url.Values, target any) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	endpoint := *c.source
	endpoint.User, endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = nil, path, "", query.Encode(), ""
	response, err := c.getter.Get(c.ctx, endpoint.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json,text/javascript,*/*;q=0.8")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", endpoint.Scheme+"://"+endpoint.Host+"/")
	})
	if err != nil {
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		return &TransferError{Err: errors.New("微博接口请求失败")}
	}
	defer response.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(response.FinalURL, endpoint.String()))
	if err != nil || final.User != nil || !sameOrigin(&endpoint, final) {
		return errors.New("微博接口跳转到其他站点，可能需要登录或验证")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("微博接口可能需要登录或验证: %w", &HTTPStatusError{URL: endpoint.Scheme + "://" + endpoint.Host + endpoint.Path, StatusCode: response.StatusCode, RetryAfter: ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())})
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		return &TransferError{Err: errors.New("读取微博接口响应失败")}
	}
	if len(raw) > maxPageBytes {
		return errors.New("微博接口响应超过 8 MiB")
	}
	if length := contentLength(response); length >= 0 && length != int64(len(raw)) {
		return &IntegrityError{Err: errors.New("微博接口响应长度不符")}
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer gz.Close()
		reader = gz
	case "deflate":
		zr, err := zlib.NewReader(reader)
		if err != nil {
			return &TransferError{Err: err}
		}
		defer zr.Close()
		reader = zr
	default:
		return errors.New("微博接口使用了不支持的压缩格式")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if err != nil || len(data) > maxPageBytes {
		return errors.New("微博接口响应无法读取或超过 8 MiB")
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := weiboDecode(data, target); err != nil {
		return errors.New("微博接口响应无效，可能需要登录或验证")
	}
	return nil
}

func weiboDecode(body []byte, target any) error {
	data := bytes.TrimSpace(body)
	if !json.Valid(data) {
		data = bytes.TrimSpace(bytes.TrimSuffix(data, []byte(";")))
		if bytes.HasPrefix(data, []byte("var ")) {
			data = bytes.TrimSpace(data[4:])
		}
		if at := bytes.IndexByte(data, '='); at >= 0 && weiboCallback.Match(bytes.TrimSpace(data[:at])) {
			data = bytes.TrimSpace(data[at+1:])
		} else if at := bytes.IndexByte(data, '('); at >= 0 && weiboCallback.Match(bytes.TrimSpace(data[:at])) && len(data) > at+1 && data[len(data)-1] == ')' {
			data = bytes.TrimSpace(data[at+1 : len(data)-1])
		} else {
			return errors.New("invalid JSONP wrapper")
		}
	}
	if len(data) == 0 || (data[0] != '{' && data[0] != '[') {
		return errors.New("invalid response type")
	}
	return json.Unmarshal(data, target)
}

func (c *weiboCollector) add(raw string) bool {
	u, err := crawlURL(c.source, raw)
	if err != nil || u.User != nil || !domainOrSubdomain(u.Hostname(), "sinaimg.cn") ||
		(u.Port() != "" && !((u.Scheme == "http" && u.Port() == "80") || (u.Scheme == "https" && u.Port() == "443"))) {
		return false
	}
	if u.Scheme == "http" {
		u.Scheme, u.Host = "https", u.Hostname()
	}
	image := weiboOriginalURL(c.source.String(), u.String())
	if !c.seen[image] {
		c.seen[image] = true
		item := Item{URL: image}
		if ref := imageReferer(c.source.Scheme+"://"+c.source.Host+"/", image); ref != "" {
			item.Headers = map[string]string{"Referer": ref}
		}
		c.result.Items = append(c.result.Items, item)
	}
	return true
}

type weiboPicture struct {
	URL      string `json:"url"`
	Original struct {
		URL string `json:"url"`
	} `json:"original"`
	Largest struct {
		URL string `json:"url"`
	} `json:"largest"`
	Large struct {
		URL string `json:"url"`
	} `json:"large"`
	BMiddle struct {
		URL string `json:"url"`
	} `json:"bmiddle"`
	Thumbnail struct {
		URL string `json:"url"`
	} `json:"thumbnail"`
}

func (p weiboPicture) url() string {
	return firstNonEmpty(p.Original.URL, p.Largest.URL, p.Large.URL, p.URL, p.BMiddle.URL, p.Thumbnail.URL)
}

type weiboStatus struct {
	ID              json.RawMessage         `json:"id"`
	IDStr           string                  `json:"idstr"`
	MID             string                  `json:"mid"`
	MblogID         string                  `json:"mblogid"`
	Bid             string                  `json:"bid"`
	Text            string                  `json:"text_raw"`
	PicIDs          []string                `json:"pic_ids"`
	PicInfos        map[string]weiboPicture `json:"pic_infos"`
	Pics            []weiboPicture          `json:"pics"`
	RetweetedStatus *weiboStatus            `json:"retweeted_status"`
	MixMediaInfo    *struct {
		Items []struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		} `json:"items"`
	} `json:"mix_media_info"`
}

func weiboNumber(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		value = string(raw)
	}
	return value
}

func (c *weiboCollector) post(id string) error {
	if !weiboPostKey.MatchString(id) || c.posts >= maxWeiboPosts {
		return errors.New("微博帖子编号无效或已达到扫描上限")
	}
	c.posts++
	var raw json.RawMessage
	if err := c.api("/ajax/statuses/show", url.Values{"id": {id}}, &raw); err != nil {
		return err
	}
	var wrapper struct {
		OK   *int            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &wrapper) == nil && wrapper.OK != nil {
		if *wrapper.OK != 1 || len(wrapper.Data) == 0 {
			return errors.New("微博帖子不可用，可能需要登录或验证")
		}
		raw = wrapper.Data
	}
	var post weiboStatus
	if json.Unmarshal(raw, &post) != nil {
		return errors.New("微博帖子数据格式无效")
	}
	if id != post.IDStr && id != post.MID && id != post.MblogID && id != post.Bid && id != weiboNumber(post.ID) {
		return errors.New("微博帖子编号与请求不一致")
	}
	if c.result.Title == "" {
		c.result.Title = strings.TrimSpace(post.Text)
	}
	return c.statusImages(post, 0)
}

func (c *weiboCollector) statusImages(post weiboStatus, depth int) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if depth > 2 {
		return errors.New("转发微博嵌套过深，结果可能不完整")
	}
	var failures []error
	ids := append([]string(nil), post.PicIDs...)
	if len(ids) == 0 {
		for id := range post.PicInfos {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}
	for _, id := range ids {
		if err := c.nextRow(); err != nil {
			return errors.Join(errors.Join(failures...), err)
		}
		pic, ok := post.PicInfos[id]
		if !ok || !c.add(pic.url()) {
			failures = append(failures, errors.New("微博图片地址缺失或不属于受支持的图床"))
		}
	}
	for _, pic := range post.Pics {
		if err := c.nextRow(); err != nil {
			return errors.Join(errors.Join(failures...), err)
		}
		if !c.add(pic.url()) {
			failures = append(failures, errors.New("微博图片地址缺失或不属于受支持的图床"))
		}
	}
	if post.MixMediaInfo != nil {
		for _, item := range post.MixMediaInfo.Items {
			if item.Type != "pic" && item.Type != "image" {
				continue
			}
			if err := c.nextRow(); err != nil {
				return errors.Join(errors.Join(failures...), err)
			}
			var pic weiboPicture
			var nested struct {
				PicInfo weiboPicture `json:"pic_info"`
			}
			if json.Unmarshal(item.Data, &pic) != nil || (pic.url() == "" && json.Unmarshal(item.Data, &nested) != nil) {
				failures = append(failures, errors.New("微博混合媒体中的图片格式无效"))
				continue
			}
			if raw := firstNonEmpty(pic.url(), nested.PicInfo.url()); !c.add(raw) {
				failures = append(failures, errors.New("微博混合媒体中的图片地址无效"))
			}
		}
	}
	if post.RetweetedStatus != nil {
		failures = append(failures, c.statusImages(*post.RetweetedStatus, depth+1))
	}
	return errors.Join(failures...)
}

func (c *weiboCollector) userID(_ string) (string, error) {
	return "", errors.New("无法从微博昵称确定相册用户编号，请使用数字用户主页地址")
}

type weiboAlbumResponse struct {
	OK   *int `json:"ok"`
	Data *struct {
		AlbumList json.RawMessage `json:"album_list"`
		List      json.RawMessage `json:"list"`
		SinceID   json.RawMessage `json:"since_id"`
	} `json:"data"`
}

// The legacy and current APIs both return untrusted identifiers. Pass them as
// encoded query values only after validating their size and character set.
var weiboContainer = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,256}$`)
var weiboPhotoName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,200}$`)

func weiboList(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, errors.New("微博接口缺少图片列表，结果可能不完整")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, errors.New("微博图片列表格式无效")
	}
	return rows, nil
}

func weiboCursor(raw json.RawMessage, hasRows bool) (string, error) {
	if len(raw) == 0 {
		if hasRows {
			return "", errors.New("微博接口缺少下一页标记，结果可能不完整")
		}
		return "", nil
	}
	s := weiboNumber(raw)
	if s == "" || s == "0" || s == "null" {
		return "", nil
	}
	if !weiboContainer.MatchString(s) {
		return "", errors.New("微博分页标记无效，结果可能不完整")
	}
	return s, nil
}

func (c *weiboCollector) albumRows(rows []json.RawMessage) error {
	var failures []error
	for _, raw := range rows {
		if err := c.nextRow(); err != nil {
			return errors.Join(errors.Join(failures...), err)
		}
		var row struct {
			PID string          `json:"pid"`
			MID json.RawMessage `json:"mid"`
		}
		if json.Unmarshal(raw, &row) != nil {
			failures = append(failures, errors.New("微博相册图片数据无效"))
			continue
		}
		mid := weiboNumber(row.MID)
		if mid != "" && mid != "0" && mid != "null" {
			if err := c.post(mid); err != nil {
				if errors.Is(err, errWeiboRowLimit) || c.ctx.Err() != nil {
					return errors.Join(errors.Join(failures...), err)
				}
				failures = append(failures, err)
			}
			continue
		}
		if !weiboPID.MatchString(row.PID) || !c.add("https://wx3.sinaimg.cn/mw2000/"+row.PID+".jpg") {
			failures = append(failures, errors.New("微博相册图片编号无效"))
		}
	}
	return errors.Join(failures...)
}

func (c *weiboCollector) albums(uid string) error {
	if !weiboDigits.MatchString(uid) {
		return errors.New("微博用户编号无效")
	}
	seenCursors := map[string]bool{"0": true}
	seenAlbums := map[string]bool{}
	cursor := "0"
	for page := 0; page < maxWeiboPages; page++ {
		query := url.Values{"uid": {uid}, "sinceid": {cursor}}
		if page == 0 {
			query.Set("has_album", "true")
		}
		var response weiboAlbumResponse
		if err := c.api("/ajax/profile/getImageWall", query, &response); err != nil {
			return err
		}
		if response.Data == nil || (response.OK != nil && *response.OK != 1) {
			return errors.New("微博图片墙不可用，可能需要登录或验证")
		}
		if page == 0 {
			albums, err := weiboList(response.Data.AlbumList)
			if err != nil {
				c.problems = append(c.problems, errors.New("微博相册列表缺失，结果可能不完整"))
			}
			for _, raw := range albums {
				if err := c.ctx.Err(); err != nil {
					return err
				}
				var album struct {
					Container string `json:"containerid"`
				}
				if json.Unmarshal(raw, &album) != nil || !weiboContainer.MatchString(album.Container) {
					c.problems = append(c.problems, errors.New("微博相册编号无效"))
					continue
				}
				if seenAlbums[album.Container] {
					continue
				}
				if len(seenAlbums) >= maxWeiboAlbums {
					c.problems = append(c.problems, errors.New("微博相册数量达到上限，结果可能不完整"))
					break
				}
				seenAlbums[album.Container] = true
				if err := c.album(album.Container); err != nil {
					if errors.Is(err, errWeiboRowLimit) || c.ctx.Err() != nil {
						return err
					}
					c.problems = append(c.problems, err)
				}
			}
		}
		rows, err := weiboList(response.Data.List)
		if err != nil {
			return err
		}
		if err = c.albumRows(rows); err != nil {
			if errors.Is(err, errWeiboRowLimit) || c.ctx.Err() != nil {
				return err
			}
			c.problems = append(c.problems, err)
		}
		next, err := weiboCursor(response.Data.SinceID, len(rows) > 0)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if len(rows) == 0 || seenCursors[next] {
			return errors.New("微博图片墙分页停滞，结果可能不完整")
		}
		seenCursors[next] = true
		cursor = next
	}
	return errors.New("微博图片墙页数达到上限，结果可能不完整")
}

func (c *weiboCollector) album(container string) error {
	cursor := "0"
	seen := map[string]bool{cursor: true}
	for page := 0; page < maxWeiboPages; page++ {
		var response weiboAlbumResponse
		if err := c.api("/ajax/profile/getAlbumDetail", url.Values{"containerid": {container}, "since_id": {cursor}}, &response); err != nil {
			return err
		}
		if response.Data == nil || (response.OK != nil && *response.OK != 1) {
			return errors.New("微博相册不可用，可能需要登录或验证")
		}
		rows, err := weiboList(response.Data.List)
		if err != nil {
			return err
		}
		if err = c.albumRows(rows); err != nil {
			if errors.Is(err, errWeiboRowLimit) || c.ctx.Err() != nil {
				return err
			}
			c.problems = append(c.problems, err)
		}
		next, err := weiboCursor(response.Data.SinceID, len(rows) > 0)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if len(rows) == 0 || seen[next] {
			return errors.New("微博相册分页停滞，结果可能不完整")
		}
		seen[next] = true
		cursor = next
	}
	return errors.New("微博相册页数达到上限，结果可能不完整")
}

func (c *weiboCollector) oldAlbums(uid, wanted string) error {
	var response struct {
		Data *struct {
			AlbumList json.RawMessage `json:"album_list"`
		} `json:"data"`
	}
	if err := c.api("/albums/get_all", url.Values{"uid": {uid}, "count": {"9999"}}, &response); err != nil {
		return err
	}
	if response.Data == nil {
		return errors.New("旧版微博相册不可用，可能需要登录或验证")
	}
	rows, err := weiboList(response.Data.AlbumList)
	if err != nil {
		return err
	}
	if len(rows) > maxWeiboAlbums && wanted == "" {
		c.problems = append(c.problems, errors.New("微博相册数量达到上限，结果可能不完整"))
		rows = rows[:maxWeiboAlbums]
	}
	found := false
	for _, raw := range rows {
		if err := c.ctx.Err(); err != nil {
			return err
		}
		var album struct {
			ID    json.RawMessage `json:"album_id"`
			Type  json.RawMessage `json:"type"`
			Count struct {
				Photos json.RawMessage `json:"photos"`
			} `json:"count"`
		}
		if json.Unmarshal(raw, &album) != nil {
			c.problems = append(c.problems, errors.New("旧版微博相册数据无效"))
			continue
		}
		id := weiboNumber(album.ID)
		if wanted != "" && id != wanted {
			continue
		}
		found = true
		typ := weiboNumber(album.Type)
		if !weiboDigits.MatchString(id) || !weiboDigits.MatchString(typ) {
			c.problems = append(c.problems, errors.New("旧版微博相册编号或类型无效"))
			continue
		}
		expected := -1
		if n, err := strconv.Atoi(weiboNumber(album.Count.Photos)); err == nil && n >= 0 {
			expected = n
		}
		if err := c.oldAlbum(uid, id, typ, expected); err != nil {
			if errors.Is(err, errWeiboRowLimit) || c.ctx.Err() != nil {
				return err
			}
			c.problems = append(c.problems, err)
		}
		if wanted != "" {
			break
		}
	}
	if wanted != "" && !found {
		return errors.New("没有找到指定的旧版微博相册")
	}
	return nil
}

func (c *weiboCollector) oldAlbum(uid, album, typ string, expected int) error {
	count := 0
	seen := map[string]bool{}
	for page := 1; page <= maxWeiboPages; page++ {
		var response struct {
			Data *struct {
				Photos json.RawMessage `json:"photo_list"`
				Total  json.RawMessage `json:"total"`
			} `json:"data"`
			Total json.RawMessage `json:"total"`
		}
		query := url.Values{"uid": {uid}, "album_id": {album}, "count": {"30"}, "page": {strconv.Itoa(page)}, "type": {typ}}
		if err := c.api("/photos/get_all", query, &response); err != nil {
			return err
		}
		if response.Data == nil {
			return errors.New("旧版微博相册不可用，可能需要登录或验证")
		}
		rows, err := weiboList(response.Data.Photos)
		if err != nil {
			return err
		}
		if n, err := strconv.Atoi(firstNonEmpty(weiboNumber(response.Data.Total), weiboNumber(response.Total))); err == nil && n >= 0 {
			expected = n
		}
		newCount := 0
		for _, raw := range rows {
			if err := c.nextRow(); err != nil {
				return err
			}
			var photo struct {
				Host string `json:"pic_host"`
				Name string `json:"pic_name"`
			}
			if json.Unmarshal(raw, &photo) != nil || !weiboPhotoName.MatchString(photo.Name) {
				c.problems = append(c.problems, errors.New("旧版微博图片数据无效"))
				continue
			}
			host := photo.Host
			if strings.HasPrefix(host, "//") {
				host = "https:" + host
			}
			if !strings.Contains(host, "://") {
				host = "https://" + host
			}
			u, err := url.Parse(host)
			if err != nil || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || !c.add(strings.TrimSuffix(host, "/")+"/large/"+photo.Name) {
				c.problems = append(c.problems, errors.New("旧版微博图片地址无效"))
				continue
			}
			key := host + "/" + photo.Name
			if !seen[key] {
				seen[key] = true
				newCount++
				count++
			}
		}
		if len(rows) == 0 {
			if expected >= 0 && count < expected {
				return errors.New("旧版微博相册提前结束，结果可能不完整")
			}
			return nil
		}
		if expected >= 0 && count >= expected {
			return nil
		}
		if newCount == 0 {
			return errors.New("旧版微博相册分页停滞，结果可能不完整")
		}
	}
	return errors.New("旧版微博相册页数达到上限，结果可能不完整")
}
