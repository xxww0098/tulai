package download

import (
	"context"
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

var photoplusPath = regexp.MustCompile(`^/live/pc/([0-9]{1,20})/?$`)

// shortcut: bound automatic scrolling and captured images; larger events require
// configurable limits and explicit partial-result reporting.
const (
	photoplusMaxScrolls = 8
	photoplusMaxImages  = 1000
	photoplusMaxBodies  = 16
	photoplusMaxBody    = 4 << 20
)

func photoplusActivityID(page *url.URL) string {
	if page == nil || page.User != nil || (page.Scheme != "https" && page.Scheme != "http") ||
		!strings.EqualFold(page.Hostname(), "live.photoplus.cn") ||
		(page.Port() != "" && !(page.Scheme == "https" && page.Port() == "443") && !(page.Scheme == "http" && page.Port() == "80")) ||
		strings.Contains(strings.ToLower(page.EscapedPath()), "%2f") {
		return ""
	}
	match := photoplusPath.FindStringSubmatch(page.Path)
	if match == nil {
		return ""
	}
	return match[1]
}

func isPhotoplusPage(page *url.URL) bool { return photoplusActivityID(page) != "" }

// The public image API is signed by the current site frontend. Do not guess the
// signature or use a user's authenticated session for this public-only path.
func discoverPhotoplus(ctx context.Context, _ Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !isPhotoplusPage(page) {
		return result, errors.New("Photoplus 活动地址无效")
	}
	return result, errors.New("Photoplus 公开活动需要使用下载浏览器采集")
}

type photoplusPermissions struct {
	Activity json.Number `json:"activity_no"`
	Enable   *uint64     `json:"enable"`
	IsSell   *uint64     `json:"is_sell"`
	Name     string      `json:"name"`
	Serial   *struct {
		Type *int `json:"type"`
	} `json:"serial_code"`
}

type photoplusPhoto struct {
	Activity json.Number `json:"activity_no"`
	ID       json.Number `json:"id"`
	Enable   *uint64     `json:"enable"`
	IsSell   *uint64     `json:"is_sell"`
	Status   *int        `json:"status"`
	Name     string      `json:"pic_name"`
	Origin   string      `json:"origin_img"`
}

type photoplusList struct {
	Total  *int             `json:"pics_total"`
	Photos []photoplusPhoto `json:"pics_array"`
}

type photoplusEnvelope[T any] struct {
	Code    int  `json:"code"`
	Success bool `json:"success"`
	Result  T    `json:"result"`
}

// These bits come from the current first-party frontend's hasDownOrigin,
// hasForbiddenDownload, hasPay and tabAllShow checks. Reject paid cases and
// events without the public all-images tab instead of guessing access rights.
func photoplusAllowed(enable, sell *uint64) bool {
	return enable != nil && sell != nil && (*enable&(1048576|8388608)) == 0 && (*sell&(131072|1048576)) == 0
}

func photoplusDetail(body []byte, id string) (photoplusPermissions, error) {
	var envelope photoplusEnvelope[photoplusPermissions]
	if len(body) > photoplusMaxBody || json.Unmarshal(body, &envelope) != nil || !envelope.Success || envelope.Code != 1 || envelope.Result.Activity.String() != id {
		return photoplusPermissions{}, errors.New("Photoplus 活动详情不可用或活动编号不匹配")
	}
	if envelope.Result.Serial == nil || envelope.Result.Serial.Type == nil || *envelope.Result.Serial.Type != 0 {
		return photoplusPermissions{}, errors.New("Photoplus 活动需要密码、登记或其他访问条件")
	}
	if !photoplusAllowed(envelope.Result.Enable, envelope.Result.IsSell) {
		return photoplusPermissions{}, errors.New("Photoplus 活动未公开允许查看全部图片、下载原图，或需要付费")
	}
	return envelope.Result, nil
}

func photoplusListBody(body []byte) (photoplusList, error) {
	var envelope photoplusEnvelope[photoplusList]
	if len(body) > photoplusMaxBody || json.Unmarshal(body, &envelope) != nil || !envelope.Success || envelope.Code != 1 || envelope.Result.Total == nil || *envelope.Result.Total < 0 || len(envelope.Result.Photos) > photoplusMaxImages {
		return photoplusList{}, errors.New("Photoplus 图片列表不可用或格式无法识别")
	}
	return envelope.Result, nil
}

func photoplusImage(raw, id string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "pb.plusx.cn") ||
		(u.Port() != "" && u.Port() != "443") || u.Fragment != "" || strings.Contains(u.Path, "..") ||
		strings.Contains(u.Path, "\\") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") ||
		(!strings.HasPrefix(u.Path, "/plus/immedpic/"+id+"/") && !strings.HasPrefix(u.Path, "/plus/immediate/"+id+"/")) || strings.HasSuffix(u.Path, "/") {
		return "", false
	}
	return u.String(), true
}

func photoplusRequest(raw, id string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "live.photoplus.cn") || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" {
		return ""
	}
	q := u.Query()
	if len(q["activityNo"]) != 1 || q.Get("activityNo") != id {
		return ""
	}
	switch u.Path {
	case "/live/detail":
		return "detail"
	case "/pic/list":
		return "list"
	default:
		return ""
	}
}

type photoplusResponse struct {
	kind string
	body []byte
	fail bool
}

// The site itself produces signed requests in a new empty browser context. The
// user's tab, profile cookies and login state are neither read nor modified.
func (s *RenderedSession) capturePhotoplus(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	id := photoplusActivityID(page)
	if id == "" {
		return result, false, nil
	}
	if err := ctx.Err(); err != nil {
		return result, true, err
	}
	observed, err := url.Parse(observedURL)
	if err != nil || photoplusActivityID(observed) != id || !sameOrigin(page, observed) {
		return result, true, errors.New("Photoplus 活动页面已更改")
	}
	if observed.Fragment != "" && observed.Fragment != "/live" && observed.Fragment != "/live/" && observed.Fragment != "/" {
		return result, true, errors.New("暂不支持 Photoplus 分类、搜索或单图页面，请打开活动全部图片")
	}
	checkCurrent := func() error {
		var current struct {
			URL        string `json:"url"`
			HasPhotos  bool   `json:"hasPhotos"`
			Restricted bool   `json:"restricted"`
		}
		// A category is page state, not a URL fragment. Inspect only the visible
		// menu; do not turn a locked category into a request for all pictures.
		if err := s.run(ctx, chromedp.Evaluate(`(() => {
  const visible = n => n.getClientRects().length > 0 && getComputedStyle(n).display !== 'none';
  const lists = Array.from(document.querySelectorAll('ul.nav-bottom__ul')).filter(visible);
  const selected = lists.flatMap(n => Array.from(n.querySelectorAll('li.active')).filter(visible));
  const restricted = lists.some(n => n.querySelector('li') && !n.querySelector('li.active')) ||
    selected.some(n => !['全部','all'].includes((n.textContent || '').trim().toLowerCase()));
  return {url:location.href,hasPhotos:!!document.querySelector('.photo-live .photo-content'),restricted};
})()`, &current)); err != nil {
			return err
		}
		if current.URL != observedURL {
			return errors.New("采集期间 Photoplus 页面已更改")
		}
		if !current.HasPhotos || current.Restricted {
			return errors.New("Photoplus 仅支持当前选择全部图片的公开活动；分类、筛选或锁定相册暂不支持")
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
	stopOnCancel := context.AfterFunc(ctx, closeChild)
	defer stopOnCancel()
	responses := make(chan photoplusResponse, photoplusMaxBodies)
	var mu sync.Mutex
	pending := make(map[network.RequestID]string)
	var responseCount atomic.Int32
	var overflow atomic.Bool
	send := func(response photoplusResponse) {
		select {
		case responses <- response:
		case <-child.Done():
		default:
			overflow.Store(true)
		}
	}
	camoufox.ListenTarget(child, func(event any) {
		switch e := event.(type) {
		case *fetch.EventRequestPaused:
			u, parseErr := url.Parse(e.Request.URL)
			allowed := parseErr == nil && u.Scheme == "https" && photoplusActivityID(u) == id
			go func() {
				var action chromedp.Action = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient)
				if allowed {
					action = fetch.ContinueRequest(e.RequestID)
				}
				_ = camoufox.Run(child, action)
			}()
		case *network.EventResponseReceived:
			if e.Response == nil {
				return
			}
			kind := photoplusRequest(e.Response.URL, id)
			if kind == "" {
				return
			}
			if responseCount.Add(1) > photoplusMaxBodies {
				overflow.Store(true)
				return
			}
			if e.Response.Status != 200 || e.Response.FromServiceWorker {
				send(photoplusResponse{kind: kind, fail: true})
				return
			}
			mu.Lock()
			pending[e.RequestID] = kind
			mu.Unlock()
		case *network.EventLoadingFinished:
			mu.Lock()
			kind, ok := pending[e.RequestID]
			delete(pending, e.RequestID)
			mu.Unlock()
			if !ok {
				return
			}
			go func(requestID network.RequestID) {
				var body []byte
				err := camoufox.Run(child, chromedp.ActionFunc(func(c context.Context) error {
					var getErr error
					body, getErr = network.GetResponseBody(requestID).Do(c)
					return getErr
				}))
				if err != nil || len(body) > photoplusMaxBody {
					send(photoplusResponse{kind: kind, fail: true})
					return
				}
				send(photoplusResponse{kind: kind, body: body})
			}(e.RequestID)
		case *network.EventLoadingFailed:
			mu.Lock()
			kind, ok := pending[e.RequestID]
			delete(pending, e.RequestID)
			mu.Unlock()
			if ok {
				send(photoplusResponse{kind: kind, fail: true})
			}
		}
	})
	initTimeout := time.AfterFunc(30*time.Second, closeChild)
	initErr := camoufox.Run(child,
		network.Enable().WithMaxResourceBufferSize(photoplusMaxBody).WithMaxTotalBufferSize(2*photoplusMaxBody),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{ResourceType: network.ResourceTypeDocument}}))
	initTimeout.Stop()
	if initErr != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("无法创建 Photoplus 公开采集标签")
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
	// Do not copy query or fragment credentials into the anonymous context.
	target := "https://live.photoplus.cn/live/pc/" + id + "/#/live"
	if err := run(camoufox.Navigate(target)); err != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("无法打开 Photoplus 公开活动，可能需要登录、密码或安全验证")
	}
	var detail *photoplusPermissions
	var detailErr error
	var lists []photoplusList
	var failures int
	consume := func() {
		for {
			select {
			case response := <-responses:
				if response.fail {
					failures++
					continue
				}
				if response.kind == "detail" {
					value, err := photoplusDetail(response.body, id)
					if err != nil {
						detailErr = err
					} else {
						detail = &value
					}
				} else {
					value, err := photoplusListBody(response.body)
					if err != nil {
						failures++
					} else {
						lists = append(lists, value)
					}
				}
			default:
				return
			}
		}
	}
	// Allow the SPA time to request its initial detail and picture list.
	for attempt := 0; attempt < 30 && (detail == nil || len(lists) == 0); attempt++ {
		consume()
		if detailErr != nil {
			return result, true, detailErr
		}
		if detail != nil && len(lists) > 0 {
			break
		}
		if err := Wait(ctx, 300*time.Millisecond); err != nil {
			return result, true, err
		}
	}
	consume()
	if detailErr != nil {
		return result, true, detailErr
	}
	if detail == nil || len(lists) == 0 {
		return result, true, errors.New("Photoplus 未返回公开活动图片，可能需要登录、密码或安全验证")
	}
	result.Title = detail.Name
	seen := make(map[string]bool)
	total := -1
	bad := 0
	processed := 0
	process := func() {
		for processed < len(lists) {
			list := lists[processed]
			processed++
			if total >= 0 && total != *list.Total {
				failures++
			}
			total = *list.Total
			for _, photo := range list.Photos {
				if photo.Activity.String() != id || photo.ID.String() == "" || photo.Status == nil || *photo.Status != 5 || !photoplusAllowed(photo.Enable, photo.IsSell) {
					bad++
					continue
				}
				image, ok := photoplusImage(photo.Origin, id)
				if !ok {
					bad++
					continue
				}
				key := photo.ID.String()
				if seen[key] {
					continue
				}
				seen[key] = true
				if len(result.Items) >= photoplusMaxImages {
					continue
				}
				result.Items = append(result.Items, Item{URL: image, RequireImage: true, PublicOnly: true,
					Headers:  map[string]string{"Referer": "https://live.photoplus.cn/"},
					Metadata: map[string]string{"title": photo.Name, "photoplus_activity_id": id, "photoplus_image_id": key}})
			}
		}
	}
	process()
	for step := 0; step < photoplusMaxScrolls && len(result.Items) < total && len(result.Items) < photoplusMaxImages; step++ {
		if err := checkCurrent(); err != nil {
			return result, true, err
		}
		var current string
		if err := run(chromedp.Location(&current)); err != nil {
			return result, true, err
		}
		currentURL, parseErr := url.Parse(current)
		if parseErr != nil || photoplusActivityID(currentURL) != id || currentURL.Scheme != "https" || (currentURL.Fragment != "" && currentURL.Fragment != "/live" && currentURL.Fragment != "/live/") {
			return result, true, errors.New("Photoplus 活动已跳转，结果可能不完整")
		}
		if err := run(chromedp.Evaluate(`(() => { const c = document.querySelector('.photo-live .photo-content'); if (c) c.scrollTop = c.scrollHeight; })()`, nil)); err != nil {
			return result, true, errors.New("Photoplus 活动滚动失败，结果可能不完整")
		}
		if err := Wait(ctx, 1200*time.Millisecond); err != nil {
			return result, true, err
		}
		consume()
		if detailErr != nil {
			return Discovery{IsPage: true}, true, detailErr
		}
		process()
	}
	consume()
	if detailErr != nil {
		return Discovery{IsPage: true}, true, detailErr
	}
	process()
	if err := checkCurrent(); err != nil {
		return result, true, err
	}
	var issues []error
	if bad > 0 {
		issues = append(issues, fmt.Errorf("%d 条 Photoplus 图片权限、编号或来源未能确认", bad))
	}
	if failures > 0 || overflow.Load() {
		issues = append(issues, errors.New("部分 Photoplus 活动数据未能读取"))
	}
	if total < 0 || len(result.Items) < total {
		issues = append(issues, fmt.Errorf("Photoplus 已采集 %d 张，活动列表共 %d 张；最多滚动 %d 次、采集 %d 张，结果可能不完整", len(result.Items), total, photoplusMaxScrolls, photoplusMaxImages))
	}
	return result, true, errors.Join(issues...)
}
