package download

import (
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

const ctripAlbumAPI = "https://m.ctrip.com/restapi/soa2/20036/json/getPoiAlbum"

var (
	ctripAlbumPath = regexp.MustCompile(`(?i)^/photos/sight/[a-z0-9_-]+/(r?)([1-9][0-9]{0,15})\.html$`)
	ctripImageHost = regexp.MustCompile(`^dimg[0-9]{2}\.c-ctrip\.com$`)
)

func ctripAlbumID(u *url.URL) (int64, bool) {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!strings.EqualFold(u.Hostname(), "you.ctrip.com") || strings.Contains(u.EscapedPath(), "%") ||
		(u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return 0, false
	}
	m := ctripAlbumPath.FindStringSubmatch(u.Path)
	if len(m) != 3 {
		return 0, false
	}
	id, err := strconv.ParseInt(m[2], 10, 64)
	return id, err == nil && id > 0 && strings.EqualFold(m[1], "r")
}

// shortcut: only business-ID sight albums are parsed until other album routes are verified.
func isCtripAlbum(u *url.URL) bool {
	id, business := ctripAlbumID(u)
	return id > 0 && business
}

type ctripPhoto struct {
	Type      int `json:"type"`
	PageIndex int `json:"pageIndex"`
	ImageInfo struct {
		ID  int64  `json:"imageId"`
		URL string `json:"imageUrl"`
	} `json:"imageInfo"`
}

type ctripState struct {
	PhotoList []ctripPhoto `json:"photoList"`
	Param     struct {
		BusinessID int64  `json:"businessId"`
		PageIndex  int    `json:"pageIndex"`
		PageSize   int    `json:"pageSize"`
		FilterID   string `json:"filterId"`
	} `json:"photoListParam"`
	Count     int    `json:"photoCount"`
	ViewState string `json:"viewState"`
}

func ctripImage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !ctripImageHost.MatchString(strings.ToLower(u.Hostname())) ||
		(u.Port() != "" && u.Port() != "443") || !strings.HasPrefix(u.Path, "/images/") || strings.Contains(u.Path, "..") {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func discoverCtripAlbum(ctx context.Context, getter Getter, root *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if getter == nil || !isCtripAlbum(root) {
		return result, errors.New("携程相册地址或下载客户端无效")
	}
	if network, ok := getter.(*HTTPGetter); ok && network != nil {
		// The public HTML endpoint serves its lightweight user agent without a browser session.
		copyGetter := *network
		copyGetter.userAgent = "Mozilla/5.0"
		getter = &copyGetter
	}
	doc, final, _, response, err := fetchCrawlPage(ctx, getter, crawlPage{url: root.String(), depth: 1})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		return result, err
	}
	if doc == nil || !isCtripAlbum(final) || !strings.EqualFold(root.Hostname(), final.Hostname()) ||
		(root.Scheme == "https" && final.Scheme != "https") || root.Path != final.Path || root.Query().Encode() != final.Query().Encode() {
		return result, errors.New("携程相册页面已跳转，可能需要登录或验证")
	}
	return ctripCollect(ctx, getter, final, doc)
}

// shortcut: inspect at most 100 album pages until larger searches can be configured.
func ctripCollect(ctx context.Context, getter Getter, page *url.URL, doc *html.Node) (Discovery, error) {
	result := Discovery{IsPage: true}
	id, business := ctripAlbumID(page)
	if getter == nil || doc == nil || id == 0 || !business {
		return result, errors.New("携程相册页面无效")
	}
	var state ctripState
	found, duplicate := false, false
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "title" && result.Title == "" {
			result.Title = nodeText(n)
		}
		if n.Data != "script" || attribute(n, "id") != "__NEXT_DATA__" {
			return
		}
		if found {
			duplicate = true
			return
		}
		found = true
		var data struct {
			Props struct {
				PageProps struct {
					InitialState ctripState `json:"initialState"`
				} `json:"pageProps"`
			} `json:"props"`
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode && json.Unmarshal([]byte(child.Data), &data) == nil {
				state = data.Props.PageProps.InitialState
				break
			}
		}
	})
	if !found || duplicate || state.Param.BusinessID != id || state.Param.PageIndex != 1 || state.Param.PageSize < 1 || state.Param.PageSize > 100 ||
		state.Param.FilterID != "online" || state.Count < 0 || state.PhotoList == nil || len(state.PhotoList) > state.Param.PageSize ||
		(state.Count == 0 && len(state.PhotoList) != 0) || len(state.ViewState) > 2048 {
		return result, errors.New("携程未返回匹配的相册数据，可能需要登录或验证")
	}
	seen := map[int64]bool{}
	missing := 0
	finish := func(err error) (Discovery, error) {
		if missing > 0 {
			err = errors.Join(err, fmt.Errorf("携程相册有 %d 条图片无法确认来源", missing))
		}
		return result, err
	}
	appendPage := func(rows []ctripPhoto, expected int) int {
		added := 0
		for _, row := range rows {
			if row.Type != 1 || row.PageIndex != expected || row.ImageInfo.ID <= 0 {
				missing++
				continue
			}
			image := ctripImage(row.ImageInfo.URL)
			if image == "" {
				missing++
				continue
			}
			if seen[row.ImageInfo.ID] {
				continue
			}
			seen[row.ImageInfo.ID] = true
			added++
			item := Item{URL: image, RequireImage: true, PublicOnly: true, Metadata: map[string]string{"ctrip_image_id": strconv.FormatInt(row.ImageInfo.ID, 10)}}
			if ref := imageReferer(page.String(), image); ref != "" {
				item.Headers = map[string]string{"Referer": ref}
			}
			result.Items = append(result.Items, item)
		}
		return added
	}
	appendPage(state.PhotoList, 1)
	if len(state.PhotoList) < min(state.Count, state.Param.PageSize) {
		return finish(errors.New("携程相册首页图片不足，结果可能不完整"))
	}
	if state.Count <= state.Param.PageSize {
		return finish(nil)
	}
	if state.ViewState == "" {
		return finish(errors.New("携程相册缺少分页信息，结果可能不完整"))
	}
	var poster Poster
	if session, ok := getter.(*RenderedSession); ok {
		// The public cross-origin API gets no browser cookies and keeps the session's safe dialer.
		if session.tab == nil || session.getter == nil {
			return finish(errors.New("携程浏览器会话不可用，结果可能不完整"))
		}
		requestCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(session.tab, cancel)
		defer func() { stop(); cancel() }()
		ctx = requestCtx
		poster = session.getter
	} else {
		poster, _ = getter.(Poster)
	}
	if poster == nil {
		return finish(errors.New("携程相册需要分页请求，结果可能不完整"))
	}
	for pageNo := 2; (pageNo-1)*state.Param.PageSize < state.Count; pageNo++ {
		if err := ctx.Err(); err != nil {
			return finish(err)
		}
		if pageNo > maxCrawlPages {
			return finish(errors.New("携程相册达到 100 页上限，结果可能不完整"))
		}
		request := map[string]any{"businessId": id, "pageIndex": pageNo, "pageSize": state.Param.PageSize,
			"filterId": "online", "viewState": state.ViewState, "head": map[string]string{"syscode": "999"}}
		body, err := json.Marshal(request)
		if err != nil {
			return finish(err)
		}
		response, err := poster.Post(ctx, ctripAlbumAPI, body, http.Header{"Content-Type": {"application/json"}})
		if err != nil {
			return finish(fmt.Errorf("读取携程相册下一页失败: %w", err))
		}
		if response == nil || response.Body == nil {
			return finish(errors.New("携程相册分页未返回数据"))
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxPageBytes+1))
		response.Body.Close()
		if readErr != nil || len(data) > maxPageBytes || response.StatusCode != http.StatusOK || response.FinalURL != ctripAlbumAPI {
			return finish(errors.New("携程相册分页不可用，结果可能不完整"))
		}
		var next struct {
			Result    *int         `json:"result"`
			ViewState string       `json:"viewState"`
			List      []ctripPhoto `json:"albumInfoList"`
		}
		if json.Unmarshal(data, &next) != nil || next.Result == nil || *next.Result != 0 || next.List == nil ||
			len(next.List) > state.Param.PageSize || len(next.ViewState) > 2048 {
			return finish(errors.New("携程相册分页数据无效，结果可能不完整"))
		}
		added := appendPage(next.List, pageNo)
		if len(next.List) < min(state.Param.PageSize, state.Count-(pageNo-1)*state.Param.PageSize) || added == 0 {
			return finish(errors.New("携程相册分页结果不足，结果可能不完整"))
		}
		if next.ViewState != "" {
			state.ViewState = next.ViewState
		}
	}
	return finish(nil)
}
