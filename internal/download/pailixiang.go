package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

var (
	pailixiangAlbumPath  = regexp.MustCompile(`^/album/(a[0-9]{1,20})/?$`)
	pailixiangLegacyPath = regexp.MustCompile(`^/album_[A-Za-z0-9]{1,30}\.html$`)
	pailixiangInternalID = regexp.MustCompile(`^[0-9]{1,30}$`)
	pailixiangPhotoID    = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// shortcut: respect the site's per-batch cap and make partial results explicit;
// unverified groups and transfer-backed images need separate support.
const (
	pailixiangMaxImages  = 100
	pailixiangMaxScrolls = 4
	pailixiangMaxBody    = 4 << 20
	pailixiangMaxBodies  = 8
	pailixiangMaxPost    = 64 << 10
)

func pailixiangAlbumID(page *url.URL) string {
	if page == nil || page.User != nil || (page.Scheme != "https" && page.Scheme != "http") ||
		!strings.EqualFold(page.Hostname(), "live.pailixiang.com") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) ||
		strings.Contains(strings.ToLower(page.EscapedPath()), "%2f") {
		return ""
	}
	match := pailixiangAlbumPath.FindStringSubmatch(page.Path)
	if match == nil {
		return ""
	}
	return match[1]
}

func pailixiangLegacy(page *url.URL) bool {
	return page != nil && page.User == nil && (page.Scheme == "http" || page.Scheme == "https") &&
		strings.EqualFold(page.Hostname(), "www.pailixiang.com") &&
		(page.Port() == "" || (page.Scheme == "https" && page.Port() == "443") || (page.Scheme == "http" && page.Port() == "80")) &&
		!strings.Contains(strings.ToLower(page.EscapedPath()), "%2f") && pailixiangLegacyPath.MatchString(page.Path)
}

func isPailixiangPage(page *url.URL) bool {
	return pailixiangAlbumID(page) != "" || pailixiangLegacy(page)
}

func discoverPailixiang(ctx context.Context, _ Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if pailixiangLegacy(page) {
		return result, errors.New("拍立享旧版相册暂不支持，请使用当前公开活动链接")
	}
	if pailixiangAlbumID(page) == "" {
		return result, errors.New("拍立享活动地址无效")
	}
	return result, errors.New("拍立享公开相册需要使用下载浏览器采集")
}

type pailixiangEntity struct {
	ID                  string `json:"ID"`
	Code                string `json:"Code"`
	Name                string `json:"Name"`
	EnterValidateType   *int   `json:"EnterValidateType"`
	LockType            *int   `json:"LockType"`
	IsOnlyByFindMe      *bool  `json:"IsOnlyByFindMe"`
	IsPhotoDownload     *bool  `json:"IsPhotoDownload"`
	IsPayDownload       *bool  `json:"IsPayDownload"`
	IsInternalLink      *bool  `json:"IsInternalLink"`
	PhotoDownloadMaxQty *int   `json:"PhotoDownloadMaxQty"`
}

type pailixiangView struct {
	Entity     pailixiangEntity `json:"Entity"`
	ResCode    *int             `json:"ResCode"`
	IsNw       *bool            `json:"IsNw"`
	IsStart    *bool            `json:"IsStart"`
	PhotoGroup json.RawMessage  `json:"PhotoGroup"`
	NavList    []struct {
		ID string `json:"ID"`
	} `json:"NavList"`
}

type pailixiangPhoto struct {
	ID                   string `json:"ID"`
	Name                 string `json:"Name"`
	FileName             string `json:"FileName"`
	DownloadImageURL     string `json:"DownloadImageUrl"`
	IsTransfer           *bool  `json:"IsTransfer"`
	IsGetTransferFileURL *bool  `json:"IsGetTransferFileUrl"`
}

type pailixiangResponse[T any] struct {
	Code       *int `json:"Code"`
	Data       T    `json:"Data"`
	TotalCount *int `json:"TotalCount"`
}

func pailixiangDetail(body []byte, code string) (pailixiangView, int, error) {
	var result pailixiangResponse[pailixiangView]
	if len(body) > pailixiangMaxBody || json.Unmarshal(body, &result) != nil || result.Code == nil || *result.Code != 0 {
		return pailixiangView{}, 0, errors.New("拍立享活动详情不可用")
	}
	v, e := result.Data, result.Data.Entity
	if e.Code != code || !pailixiangInternalID.MatchString(e.ID) {
		return pailixiangView{}, 0, errors.New("拍立享活动编号不匹配")
	}
	if v.ResCode == nil || *v.ResCode != 0 || v.IsNw == nil || *v.IsNw || v.IsStart == nil || !*v.IsStart ||
		e.EnterValidateType == nil || *e.EnterValidateType != 0 || e.LockType == nil || *e.LockType != 0 ||
		e.IsOnlyByFindMe == nil || *e.IsOnlyByFindMe || e.IsPhotoDownload == nil || !*e.IsPhotoDownload ||
		e.IsPayDownload == nil || *e.IsPayDownload || e.IsInternalLink == nil || *e.IsInternalLink ||
		e.PhotoDownloadMaxQty == nil || *e.PhotoDownloadMaxQty < 0 {
		return pailixiangView{}, 0, errors.New("拍立享活动未公开允许下载，或需要密码、验证、付费或内部链接")
	}
	if string(v.PhotoGroup) != "null" || len(v.NavList) == 0 || v.NavList[0].ID != "photoLive" {
		return pailixiangView{}, 0, errors.New("拍立享分组或默认栏目暂不支持，无法确认全部照片权限")
	}
	limit := *e.PhotoDownloadMaxQty
	if limit == 0 || limit > pailixiangMaxImages {
		limit = pailixiangMaxImages
	}
	return v, limit, nil
}

func pailixiangImage(raw, code string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "img1.pailixiang.com") ||
		(u.Port() != "" && u.Port() != "443") || u.Fragment != "" || strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || !strings.HasPrefix(u.Path, "/album/"+code+"/") || strings.HasSuffix(u.Path, "/") {
		return "", false
	}
	return u.String(), true
}

type pailixiangRequest struct {
	kind, album  string
	start, count int
}

// Decode only the site's own anonymous POST body to bind responses to the
// requested activity and the unfiltered, non-paid list. Signatures are ignored.
func pailixiangResponseURL(raw, kind string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "mapi.pailixiang.com") ||
		(u.Port() != "" && u.Port() != "443") || u.Fragment != "" {
		return false
	}
	return (kind == "view" && u.Path == "/plx/WapAbm/AlbumGetView") || (kind == "list" && u.Path == "/plx/WapAbm/AlbumSearchPhoto")
}

func pailixiangRequestFor(raw, method string, entries []*network.PostDataEntry, code string) (pailixiangRequest, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "mapi.pailixiang.com") ||
		(u.Port() != "" && u.Port() != "443") || u.Fragment != "" || method != "POST" || len(entries) == 0 || len(entries) > 4 {
		return pailixiangRequest{}, false
	}
	var body []byte
	for _, entry := range entries {
		if entry == nil || len(entry.Bytes) > 2*pailixiangMaxPost {
			return pailixiangRequest{}, false
		}
		part, err := base64.StdEncoding.DecodeString(entry.Bytes)
		if err != nil || len(body)+len(part) > pailixiangMaxPost {
			return pailixiangRequest{}, false
		}
		body = append(body, part...)
	}
	switch u.Path {
	case "/plx/WapAbm/AlbumGetView":
		var v struct {
			ID string          `json:"ID"`
			Nw json.RawMessage `json:"Nw"`
		}
		if json.Unmarshal(body, &v) != nil || v.ID != strings.TrimPrefix(code, "a") || (len(v.Nw) > 0 && string(v.Nw) != "null" && string(v.Nw) != `""` && string(v.Nw) != "false") {
			return pailixiangRequest{}, false
		}
		return pailixiangRequest{kind: "view"}, true
	case "/plx/WapAbm/AlbumSearchPhoto":
		var v struct {
			AlbumID       string  `json:"AlbumID"`
			GroupID       *string `json:"GroupID"`
			SearchType    *int    `json:"SearchType"`
			IsPayDownload *bool   `json:"IsPayDownload"`
			IsNw          *bool   `json:"IsNw"`
			StartIndex    *int    `json:"StartIndex"`
			SearchCount   *int    `json:"SearchCount"`
			SearchText    string  `json:"SearchText"`
		}
		if json.Unmarshal(body, &v) != nil || !pailixiangInternalID.MatchString(v.AlbumID) || v.GroupID == nil || *v.GroupID != "" ||
			v.SearchType == nil || *v.SearchType != 0 || v.IsPayDownload == nil || *v.IsPayDownload || v.IsNw == nil || *v.IsNw ||
			v.StartIndex == nil || *v.StartIndex < 1 || *v.StartIndex > pailixiangMaxImages || v.SearchCount == nil ||
			*v.SearchCount < 1 || *v.SearchCount > pailixiangMaxImages || v.SearchText != "" {
			return pailixiangRequest{}, false
		}
		return pailixiangRequest{kind: "list", album: v.AlbumID, start: *v.StartIndex, count: *v.SearchCount}, true
	}
	return pailixiangRequest{}, false
}

type pailixiangMessage struct {
	request pailixiangRequest
	body    []byte
	failed  bool
}

// Use an empty browser context to let the official frontend issue its own
// requests. This does not read or copy the user's browser profile or cookies.
func (s *RenderedSession) capturePailixiang(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	if pailixiangLegacy(page) {
		return result, true, errors.New("拍立享旧版相册暂不支持，请使用当前公开活动链接")
	}
	code := pailixiangAlbumID(page)
	if code == "" {
		return result, false, nil
	}
	if err := ctx.Err(); err != nil {
		return result, true, err
	}
	observed, err := url.Parse(observedURL)
	if err != nil || pailixiangAlbumID(observed) != code || !sameOrigin(page, observed) {
		return result, true, errors.New("拍立享活动页面已更改")
	}
	if observed.RawQuery != "" || observed.Fragment != "" {
		return result, true, errors.New("拍立享带参数、内部链接或筛选页面暂不支持，请使用不带参数的公开活动链接")
	}
	checkCurrent := func() error {
		var snapshot struct {
			URL   string `json:"url"`
			First bool   `json:"first"`
		}
		err := s.run(ctx, chromedp.Evaluate(`(() => {
  const nav = Array.from(document.querySelectorAll('li.nav_item'));
  return {url:location.href,first:nav.length > 0 && nav[0].classList.contains('active')};
})()`, &snapshot))
		if err != nil {
			return err
		}
		if snapshot.URL != observedURL {
			return errors.New("采集期间拍立享页面已更改")
		}
		if !snapshot.First {
			return errors.New("拍立享仅支持照片直播栏目，请先返回公开活动照片列表")
		}
		return nil
	}
	if err := checkCurrent(); err != nil {
		return result, true, err
	}
	child, closeChild, err := camoufox.NewPage(ctx, s.tab, true)
	if err != nil {
		return result, true, err
	}
	defer closeChild()
	stop := context.AfterFunc(ctx, closeChild)
	defer stop()
	messages := make(chan pailixiangMessage, pailixiangMaxBodies)
	var mu sync.Mutex
	pending := make(map[network.RequestID]pailixiangRequest)
	var counted atomic.Int32
	var overflow atomic.Bool
	send := func(m pailixiangMessage) {
		select {
		case messages <- m:
		case <-child.Done():
		default:
			overflow.Store(true)
		}
	}
	camoufox.ListenTarget(child, func(event any) {
		switch e := event.(type) {
		case *fetch.EventRequestPaused:
			u, err := url.Parse(e.Request.URL)
			allowed := false
			if err == nil && e.RedirectedRequestID == "" {
				if e.ResourceType == network.ResourceTypeDocument {
					allowed = u.Scheme == "https" && pailixiangAlbumID(u) == code && u.RawQuery == "" && u.Fragment == ""
				} else {
					allowed = (e.Request.Method == "POST" || e.Request.Method == "OPTIONS") && (pailixiangResponseURL(e.Request.URL, "view") || pailixiangResponseURL(e.Request.URL, "list"))
				}
			}
			go func() {
				var action chromedp.Action = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient)
				if allowed {
					action = fetch.ContinueRequest(e.RequestID)
				}
				_ = camoufox.Run(child, action)
			}()
		case *network.EventRequestWillBeSent:
			if e.RedirectResponse != nil {
				mu.Lock()
				prior, exists := pending[e.RequestID]
				delete(pending, e.RequestID)
				mu.Unlock()
				if exists {
					send(pailixiangMessage{request: prior, failed: true})
				}
				return
			}
			if e.Request == nil {
				return
			}
			req, ok := pailixiangRequestFor(e.Request.URL, e.Request.Method, e.Request.PostDataEntries, code)
			if !ok {
				return
			}
			if counted.Add(1) > pailixiangMaxBodies {
				overflow.Store(true)
				return
			}
			mu.Lock()
			pending[e.RequestID] = req
			mu.Unlock()
		case *network.EventResponseReceived:
			mu.Lock()
			req, ok := pending[e.RequestID]
			mu.Unlock()
			if !ok {
				return
			}
			if e.Response == nil || !pailixiangResponseURL(e.Response.URL, req.kind) || e.Response.Status != 200 || e.Response.FromServiceWorker {
				mu.Lock()
				delete(pending, e.RequestID)
				mu.Unlock()
				send(pailixiangMessage{request: req, failed: true})
			}
		case *network.EventLoadingFinished:
			mu.Lock()
			req, ok := pending[e.RequestID]
			delete(pending, e.RequestID)
			mu.Unlock()
			if !ok {
				return
			}
			go func(id network.RequestID) {
				var body []byte
				err := camoufox.Run(child, chromedp.ActionFunc(func(c context.Context) error { var x error; body, x = network.GetResponseBody(id).Do(c); return x }))
				if err != nil || len(body) > pailixiangMaxBody {
					send(pailixiangMessage{request: req, failed: true})
					return
				}
				send(pailixiangMessage{request: req, body: body})
			}(e.RequestID)
		case *network.EventLoadingFailed:
			mu.Lock()
			req, ok := pending[e.RequestID]
			delete(pending, e.RequestID)
			mu.Unlock()
			if ok {
				send(pailixiangMessage{request: req, failed: true})
			}
		}
	})
	timer := time.AfterFunc(30*time.Second, closeChild)
	initErr := camoufox.Run(child, network.Enable().WithMaxResourceBufferSize(pailixiangMaxBody).WithMaxTotalBufferSize(2*pailixiangMaxBody).WithMaxPostDataSize(pailixiangMaxPost),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{ResourceType: network.ResourceTypeDocument}, {ResourceType: network.ResourceTypeXHR}, {ResourceType: network.ResourceTypeFetch}}))
	timer.Stop()
	if initErr != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("无法创建拍立享公开采集标签")
	}
	run := func(actions ...camoufox.Action) error {
		op, cancel := context.WithTimeout(child, 30*time.Second)
		defer cancel()
		err := camoufox.Run(op, actions...)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	target := "https://live.pailixiang.com/album/" + code
	if err := run(camoufox.Navigate(target)); err != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("无法打开拍立享公开相册，可能需要登录或验证")
	}
	var view *pailixiangView
	var viewErr error
	var limit int
	var lists []pailixiangMessage
	failures := 0
	consume := func() {
		for {
			select {
			case m := <-messages:
				if m.failed {
					failures++
					continue
				}
				if m.request.kind == "view" {
					v, n, err := pailixiangDetail(m.body, code)
					if err != nil {
						viewErr = err
					} else {
						view = &v
						limit = n
					}
				} else {
					lists = append(lists, m)
				}
			default:
				return
			}
		}
	}
	for attempt := 0; attempt < 30 && (view == nil || len(lists) == 0); attempt++ {
		consume()
		if viewErr != nil {
			return result, true, viewErr
		}
		if view != nil && len(lists) > 0 {
			break
		}
		if err := Wait(ctx, 300*time.Millisecond); err != nil {
			return result, true, err
		}
	}
	consume()
	if viewErr != nil {
		return result, true, viewErr
	}
	if view == nil || len(lists) == 0 {
		return result, true, errors.New("拍立享未返回公开照片列表，可能需要验证或选择分组")
	}
	result.Title = view.Entity.Name
	seen := make(map[string]bool)
	total := -1
	bad := 0
	processed := 0
	process := func() {
		for processed < len(lists) {
			m := lists[processed]
			processed++
			if m.request.album != view.Entity.ID {
				bad++
				continue
			}
			var data pailixiangResponse[[]pailixiangPhoto]
			if json.Unmarshal(m.body, &data) != nil || data.Code == nil || *data.Code != 0 || data.TotalCount == nil || *data.TotalCount < 0 || len(data.Data) > m.request.count {
				failures++
				continue
			}
			// The frontend uses TotalCount from the first page only; later
			// responses can report zero even when more pictures exist.
			if m.request.start == 1 {
				if total >= 0 && total != *data.TotalCount {
					failures++
				}
				total = *data.TotalCount
			}
			for _, photo := range data.Data {
				if !pailixiangPhotoID.MatchString(photo.ID) || photo.IsTransfer == nil || *photo.IsTransfer || photo.IsGetTransferFileURL == nil || *photo.IsGetTransferFileURL {
					bad++
					continue
				}
				image, ok := pailixiangImage(photo.DownloadImageURL, code)
				if !ok {
					bad++
					continue
				}
				if seen[photo.ID] {
					continue
				}
				seen[photo.ID] = true
				if len(result.Items) >= limit {
					continue
				}
				result.Items = append(result.Items, Item{URL: image, RequireImage: true, PublicOnly: true, Headers: map[string]string{"Referer": "https://live.pailixiang.com/"}, Metadata: map[string]string{"title": firstNonEmpty(photo.Name, photo.FileName), "pailixiang_album_id": code, "pailixiang_photo_id": photo.ID}})
			}
		}
	}
	process()
	for step := 0; step < pailixiangMaxScrolls && len(result.Items) < limit && len(result.Items) < total; step++ {
		if err := checkCurrent(); err != nil {
			return result, true, err
		}
		var current string
		if err := run(chromedp.Location(&current)); err != nil {
			return result, true, err
		}
		u, err := url.Parse(current)
		if err != nil || pailixiangAlbumID(u) != code || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" {
			return result, true, errors.New("拍立享活动已跳转，结果可能不完整")
		}
		if err := run(chromedp.Evaluate(`window.scrollTo(0,document.documentElement.scrollHeight)`, nil)); err != nil {
			return result, true, errors.New("拍立享相册滚动失败，结果可能不完整")
		}
		if err := Wait(ctx, 1200*time.Millisecond); err != nil {
			return result, true, err
		}
		consume()
		if viewErr != nil {
			return Discovery{IsPage: true}, true, viewErr
		}
		process()
	}
	consume()
	if viewErr != nil {
		return Discovery{IsPage: true}, true, viewErr
	}
	process()
	if err := checkCurrent(); err != nil {
		return result, true, err
	}
	var issues []error
	if bad > 0 {
		issues = append(issues, fmt.Errorf("%d 条拍立享照片的相册、传输方式或来源未能确认", bad))
	}
	if failures > 0 || overflow.Load() {
		issues = append(issues, errors.New("部分拍立享活动数据未能读取"))
	}
	if total < 0 || len(result.Items) < total {
		issues = append(issues, fmt.Errorf("拍立享已采集 %d 张，列表共 %d 张；单次最多 %d 张，结果可能不完整", len(result.Items), total, limit))
	}
	return result, true, errors.Join(issues...)
}
