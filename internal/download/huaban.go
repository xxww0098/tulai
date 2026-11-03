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
	huabanIDPattern    = regexp.MustCompile(`^[0-9]{1,24}$`)
	huabanKeyPattern   = regexp.MustCompile(`^[A-Za-z0-9-]{1,160}$`)
	huabanImagePattern = regexp.MustCompile(`^/([A-Za-z0-9-]{1,160})(?:_fw[0-9]+(?:webp)?)?(?:/format/webp)?$`)
	huabanStatePattern = regexp.MustCompile(`\bapp\s*\.\s*page\s*\[\s*["'](board|pin|\$url)["']\s*\]\s*=\s*`)
)

type huabanRef struct {
	id    string
	board bool
}

type huabanPin struct {
	ID      json.RawMessage `json:"pin_id"`
	BoardID json.RawMessage `json:"board_id"`
	Text    string          `json:"raw_text"`
	File    struct {
		Key  string `json:"key"`
		Type string `json:"type"`
	} `json:"file"`
}

type huabanBoard struct {
	ID       json.RawMessage `json:"board_id"`
	Title    string          `json:"title"`
	PinCount json.RawMessage `json:"pin_count"`
	Pins     []huabanPin     `json:"pins"`
}

type huabanPageData struct {
	Pin   *huabanPin   `json:"pin"`
	Board *huabanBoard `json:"board"`
	Pins  []huabanPin  `json:"pins"`
}

func huabanPageRef(u *url.URL) huabanRef {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return huabanRef{}
	}
	switch strings.ToLower(u.Hostname()) {
	case "huaban.com", "www.huaban.com":
	default:
		return huabanRef{}
	}
	if p := u.Port(); p != "" && !(u.Scheme == "http" && p == "80") && !(u.Scheme == "https" && p == "443") {
		return huabanRef{}
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || !huabanIDPattern.MatchString(parts[1]) {
		return huabanRef{}
	}
	switch parts[0] {
	case "pins":
		return huabanRef{id: parts[1]}
	case "boards":
		return huabanRef{id: parts[1], board: true}
	}
	return huabanRef{}
}

func isHuabanPage(u *url.URL) bool { return huabanPageRef(u).id != "" }

func discoverHuaban(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	want := huabanPageRef(root)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || want.id == "" {
		return result, errors.New("花瓣页面或下载客户端无效")
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || huabanPageRef(final) != want || (root.Scheme == "https" && final.Scheme != "https") {
		return result, errors.New("花瓣页面不可用或跳转到了其他采集或画板")
	}
	return huabanCollect(ctx, getter, final, doc)
}

// huabanCollect shares structured data parsing with the browser capture flow.
func huabanCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	ref := huabanPageRef(page)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if getter == nil || ref.id == "" || doc == nil {
		return result, errors.New("花瓣页面或下载客户端无效")
	}
	var board *huabanBoard
	var pin *huabanPin
	var cover string
	var stateErr error
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
		if n.Data != "script" {
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != html.TextNode {
				continue
			}
			text := child.Data
			for _, loc := range offer1688CodeMatches(text, huabanStatePattern) {
				name := text[loc[2]:loc[3]]
				tail := text[loc[1]:]
				var raw json.RawMessage
				decoder := json.NewDecoder(strings.NewReader(tail))
				if decoder.Decode(&raw) != nil || !offer1688LiteralEnd(tail[int(decoder.InputOffset()):]) {
					stateErr = errors.Join(stateErr, errors.New("花瓣页面数据格式无效"))
					continue
				}
				switch name {
				case "pin":
					var value huabanPin
					if json.Unmarshal(raw, &value) == nil && huabanNumber(value.ID) == ref.id {
						pin = &value
					}
				case "board":
					var value huabanBoard
					if json.Unmarshal(raw, &value) == nil && huabanNumber(value.ID) == ref.id {
						board = &value
					}
				case "$url":
					var value huabanPageData
					if json.Unmarshal(raw, &value) == nil {
						if value.Pin != nil && huabanNumber(value.Pin.ID) == ref.id {
							pin = value.Pin
						}
						if value.Board != nil && huabanNumber(value.Board.ID) == ref.id {
							board = value.Board
						}
					}
				}
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !ref.board {
		if pin != nil {
			result.Title = firstNonEmpty(pin.Text, result.Title)
			item, err := huabanPinItem(page, *pin)
			if err == nil && item.URL != "" {
				result.Items = append(result.Items, item)
				return result, stateErr
			}
			stateErr = errors.Join(stateErr, err)
			if err == nil && item.URL == "" {
				return result, errors.Join(stateErr, errors.New("花瓣采集不是图片"))
			}
		}
		if key := huabanCoverKey(page, cover); key != "" {
			item := huabanImageItem(page, key)
			result.Items = append(result.Items, item)
		}
		return result, errors.Join(stateErr, errors.New("花瓣未提供完整采集数据，可能需要登录或完成验证"))
	}
	seen := make(map[string]bool)
	images := make(map[string]bool)
	cursor := ""
	count := -1
	var invalid int
	if board != nil {
		result.Title = firstNonEmpty(board.Title, result.Title)
		count = huabanCount(board.PinCount)
		cursor, invalid = huabanAddPins(ctx, page, ref.id, board.Pins, seen, images, &result)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if count >= 0 && len(seen) > count {
		stateErr = errors.Join(stateErr, errors.New("花瓣画板图片数量不一致"))
		count = -1
	}
	if count < 0 || len(seen) < count {
		if err := huabanBoardPages(ctx, getter, page, ref.id, cursor, count, seen, images, &result, &invalid); err != nil {
			stateErr = errors.Join(stateErr, err)
		}
	}
	if invalid > 0 {
		stateErr = errors.Join(stateErr, fmt.Errorf("%d 个花瓣采集的数据无效或不可用", invalid))
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(result.Items) == 0 && board == nil && stateErr == nil {
		stateErr = errors.New("花瓣画板数据不可用，可能需要登录或完成验证")
	}
	return result, stateErr
}

func huabanNumber(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		s = string(raw)
	}
	if !huabanIDPattern.MatchString(s) {
		return ""
	}
	return s
}

func huabanCount(raw json.RawMessage) int {
	s := huabanNumber(raw)
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

func huabanImageItem(page *url.URL, key string) Item {
	item := Item{URL: "https://hbimg.huaban.com/" + key}
	if ref := imageReferer(page.String(), item.URL); ref != "" {
		item.Headers = map[string]string{"Referer": ref}
	}
	return item
}

func huabanPinItem(page *url.URL, pin huabanPin) (Item, error) {
	if pin.File.Type != "" && !strings.HasPrefix(strings.ToLower(pin.File.Type), "image/") {
		return Item{}, nil
	}
	if !huabanKeyPattern.MatchString(pin.File.Key) {
		return Item{}, errors.New("花瓣采集图片地址无效")
	}
	return huabanImageItem(page, pin.File.Key), nil
}

func huabanCoverKey(page *url.URL, raw string) string {
	u, err := crawlURL(page, raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "hbimg.huaban.com", "gd-hbimg.huaban.com", "hbimg.huabanimg.com":
	default:
		return ""
	}
	match := huabanImagePattern.FindStringSubmatch(u.Path)
	if match == nil {
		return ""
	}
	return match[1]
}

func huabanAddPins(ctx context.Context, page *url.URL, boardID string, pins []huabanPin, seen, images map[string]bool, result *Discovery) (string, int) {
	cursor := ""
	invalid := 0
	for _, pin := range pins {
		if ctx.Err() != nil {
			break
		}
		id := huabanNumber(pin.ID)
		if id == "" || seen[id] {
			invalid++
			continue
		}
		seen[id] = true
		cursor = id
		if len(pin.BoardID) > 0 && huabanNumber(pin.BoardID) != boardID {
			invalid++
			continue
		}
		item, err := huabanPinItem(page, pin)
		if err != nil {
			invalid++
		} else if item.URL != "" && !images[item.URL] {
			images[item.URL] = true
			result.Items = append(result.Items, item)
		}
	}
	return cursor, invalid
}

// shortcut: at most 100 pages of 20 pins; increase this for larger boards.
func huabanBoardPages(ctx context.Context, getter Getter, page *url.URL, boardID, cursor string, count int, seen, images map[string]bool, result *Discovery, invalid *int) error {
	for pageNum := 0; pageNum < maxCrawlPages; pageNum++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		query := url.Values{"limit": {"20"}}
		if cursor != "" {
			query.Set("max", cursor)
		}
		api := &url.URL{Scheme: "https", Host: "api.huaban.com", Path: "/boards/" + boardID + "/pins", RawQuery: query.Encode()}
		body, err := huabanRequest(ctx, getter, api, page)
		if err != nil {
			return fmt.Errorf("读取花瓣画板分页失败，结果可能不完整: %w", err)
		}
		var data huabanPageData
		if json.Unmarshal(body, &data) != nil {
			return errors.New("花瓣画板接口返回无效数据，可能需要登录或完成验证")
		}
		if data.Board != nil && len(data.Board.ID) > 0 && huabanNumber(data.Board.ID) != boardID {
			return errors.New("花瓣画板接口返回了其他画板的数据")
		}
		pins := data.Pins
		if pins == nil && data.Board != nil {
			pins = data.Board.Pins
		}
		if pins == nil {
			return errors.New("花瓣画板接口没有返回采集列表，结果可能不完整")
		}
		if data.Board != nil && count < 0 {
			count = huabanCount(data.Board.PinCount)
		}
		if len(pins) == 0 {
			if count >= 0 && len(seen) < count {
				return errors.New("花瓣画板分页提前结束，结果可能不完整")
			}
			if len(seen) == 0 && data.Board == nil {
				return errors.New("花瓣画板没有可验证的数据，可能需要登录或完成验证")
			}
			return nil
		}
		before := len(seen)
		next, bad := huabanAddPins(ctx, page, boardID, pins, seen, images, result)
		*invalid += bad
		if next == "" || next == cursor || len(seen) == before || huabanNumber(pins[len(pins)-1].ID) != next {
			return errors.New("花瓣画板分页游标或采集数据无效，结果可能不完整")
		}
		cursor = next
		if count >= 0 && len(seen) == count {
			return nil
		}
		if count >= 0 && len(seen) > count {
			return errors.New("花瓣画板图片数量不一致，结果可能不完整")
		}
	}
	return errors.New("花瓣画板已达到 100 页扫描上限，结果可能不完整")
}

func huabanRequest(ctx context.Context, getter Getter, api, source *url.URL) ([]byte, error) {
	resp, err := getter.Get(ctx, api.String(), func(r *http.Request) {
		r.Header.Set("Accept", "application/json,*/*;q=0.8")
		r.Header.Set("Accept-Encoding", "identity")
		r.Header.Set("Referer", source.Scheme+"://"+source.Host+"/")
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &TransferError{Err: errors.New("花瓣接口请求失败")}
	}
	defer resp.Body.Close()
	final, err := crawlURL(nil, firstNonEmpty(resp.FinalURL, api.String()))
	if err != nil || final.User != nil || !sameOrigin(api, final) || final.Path != api.Path || final.Query().Get("max") != api.Query().Get("max") || final.Query().Get("limit") != "20" {
		return nil, errors.New("花瓣画板接口跳转到了其他地址")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{URL: api.Scheme + "://" + api.Host + api.Path, StatusCode: resp.StatusCode, RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, &TransferError{Err: errors.New("花瓣接口响应读取失败")}
	}
	if len(raw) > maxPageBytes {
		return nil, errors.New("花瓣接口响应超过 8 MiB")
	}
	if length := contentLength(resp); length >= 0 && length != int64(len(raw)) {
		return nil, &IntegrityError{Err: errors.New("花瓣接口响应长度不符")}
	}
	var reader io.Reader = bytes.NewReader(raw)
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, errors.New("花瓣接口压缩数据无效")
		}
		defer gz.Close()
		reader = gz
	case "deflate":
		zr, err := zlib.NewReader(reader)
		if err != nil {
			return nil, errors.New("花瓣接口压缩数据无效")
		}
		defer zr.Close()
		reader = zr
	default:
		return nil, errors.New("花瓣接口使用了不支持的压缩格式")
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxPageBytes+1))
	if err != nil || len(decoded) > maxPageBytes {
		return nil, errors.New("花瓣接口响应无效或超过 8 MiB")
	}
	return decoded, nil
}
