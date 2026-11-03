package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
)

var (
	onlyFansSlug   = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	onlyFansNumber = regexp.MustCompile(`^[0-9]{1,20}$`)
	onlyFansCDN    = regexp.MustCompile(`^cdn[0-9]{0,3}\.onlyfans\.com$`)
)

// shortcut: only responses produced by eight scrolls in the site's own page
// are observed; the cap protects memory and large profiles remain partial.
const (
	onlyFansMaxScrolls = 8
	onlyFansMaxBodies  = 100
	onlyFansMaxBody    = 4 << 20
	onlyFansMaxImages  = 1000
)

func isOnlyFansSite(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		!((u.Scheme == "https" && (u.Port() == "" || u.Port() == "443")) || (u.Scheme == "http" && (u.Port() == "" || u.Port() == "80"))) {
		return false
	}
	return strings.EqualFold(u.Hostname(), "onlyfans.com") || strings.EqualFold(u.Hostname(), "www.onlyfans.com")
}

func onlyFansProfile(u *url.URL) string {
	if !isOnlyFansSite(u) || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "onlyfans.com") ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") {
		return ""
	}
	name := strings.Trim(u.Path, "/")
	if !onlyFansSlug.MatchString(name) {
		return ""
	}
	switch strings.ToLower(name) {
	case "", "login", "logout", "register", "signup", "home", "my", "me", "messages", "notifications", "subscriptions", "settings", "search", "posts", "api2", "users", "privacy", "terms", "help", "about", "explore", "feed":
		return ""
	}
	return strings.ToLower(name)
}

func discoverOnlyFans(ctx context.Context, _ Getter, u *url.URL) (Discovery, error) {
	if err := ctx.Err(); err != nil {
		return Discovery{IsPage: true}, err
	}
	if !isOnlyFansSite(u) {
		return Discovery{IsPage: true}, errors.New("OnlyFans 网页地址无效")
	}
	return Discovery{IsPage: true}, errors.New("OnlyFans 图片请在下载浏览器中自行登录并打开已授权的作者主页后采集")
}

type onlyFansResponse struct {
	kind, user string
	requestID  network.RequestID
	failed     bool
}

// The page itself performs the requests. Never construct a signed API request.
// A profile response establishes the numeric author ID before post bodies are read.
func onlyFansAPI(raw string, origin *url.URL, profile string) (kind, user string) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || !sameOrigin(origin, u) ||
		!isOnlyFansSite(u) || u.Scheme != "https" || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(u.Path, "\\") {
		return "", ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 4 && parts[0] == "api2" && parts[1] == "v2" && parts[2] == "users" && strings.EqualFold(parts[3], profile) {
		return "profile", profile
	}
	if len(parts) == 5 && parts[0] == "api2" && parts[1] == "v2" && parts[2] == "users" &&
		parts[4] == "posts" && onlyFansNumber.MatchString(parts[3]) && u.RawQuery != "" {
		return "posts", parts[3]
	}
	return "", ""
}

func onlyFansIdentity(body []byte, profile string) (string, error) {
	var user struct {
		ID       json.Number `json:"id"`
		Username string      `json:"username"`
	}
	if len(body) == 0 || len(body) > onlyFansMaxBody || json.Unmarshal(body, &user) != nil ||
		!onlyFansNumber.MatchString(user.ID.String()) || !strings.EqualFold(user.Username, profile) {
		return "", errors.New("OnlyFans 作者身份无法确认")
	}
	return user.ID.String(), nil
}

func onlyFansImage(raw string) (string, bool) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") || u.Fragment != "" ||
		!onlyFansCDN.MatchString(strings.ToLower(u.Hostname())) || !strings.HasPrefix(u.Path, "/files/") ||
		strings.Contains(u.Path, "\\") || strings.Contains(u.Path, "..") || strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") {
		return "", false
	}
	return u.String(), true
}

type onlyFansPost struct {
	ID     json.Number `json:"id"`
	Author struct {
		ID json.Number `json:"id"`
	} `json:"author"`
	IsOpened     *bool `json:"isOpened"`
	CanViewMedia *bool `json:"canViewMedia"`
	MediaCount   *int  `json:"mediaCount"`
	Media        []struct {
		ID      json.Number `json:"id"`
		Type    string      `json:"type"`
		CanView *bool       `json:"canView"`
		Files   struct {
			Full struct {
				URL string `json:"url"`
			} `json:"full"`
		} `json:"files"`
	} `json:"media"`
}

type onlyFansPosts struct {
	List    []json.RawMessage `json:"list"`
	HasMore *bool             `json:"hasMore"`
}

// Reference script 116 takes media[].files.full.url for photo items. Additional
// visibility checks avoid treating a locked preview as an authorized original.
func onlyFansPostImages(body []byte, authorID, profile string) (items []Item, partial bool, err error) {
	var data onlyFansPosts
	if len(body) == 0 || len(body) > onlyFansMaxBody || !onlyFansNumber.MatchString(authorID) ||
		json.Unmarshal(body, &data) != nil || data.List == nil {
		return nil, true, errors.New("OnlyFans 帖子响应无法识别")
	}
	if len(data.List) > onlyFansMaxImages {
		data.List = data.List[:onlyFansMaxImages]
		partial = true
	}
	for _, raw := range data.List {
		var post onlyFansPost
		if json.Unmarshal(raw, &post) != nil {
			partial = true
			continue
		}
		if len(post.Media) == 0 {
			if (post.MediaCount != nil && *post.MediaCount != 0) || (post.CanViewMedia != nil && !*post.CanViewMedia) {
				partial = true
			}
			continue
		}
		if post.Author.ID.String() != authorID || !onlyFansNumber.MatchString(post.ID.String()) {
			partial = true
			continue
		}
		for _, media := range post.Media {
			if media.Type != "photo" {
				if media.Type == "" {
					partial = true
				}
				continue
			}
			if post.IsOpened == nil || !*post.IsOpened || post.CanViewMedia == nil || !*post.CanViewMedia || media.CanView == nil || !*media.CanView || !onlyFansNumber.MatchString(media.ID.String()) {
				partial = true
				continue
			}
			image, ok := onlyFansImage(media.Files.Full.URL)
			if !ok {
				partial = true
				continue
			}
			items = append(items, Item{URL: image, RequireImage: true, PublicOnly: true,
				Headers:  map[string]string{"Referer": "https://onlyfans.com/"},
				Metadata: map[string]string{"onlyfans_author": profile, "onlyfans_post_id": post.ID.String(), "onlyfans_media_id": media.ID.String()}})
			if len(items) >= onlyFansMaxImages {
				return items, true, nil
			}
		}
	}
	return items, partial, nil
}

// captureOnlyFans observes a second tab in the user's existing browser context.
// It never exports cookies or synthesizes API signatures. The original tab is
// checked before and during capture and is never navigated by the collector.
func (s *RenderedSession) captureOnlyFans(ctx context.Context, page *url.URL, observedURL string) (Discovery, bool, error) {
	result := Discovery{IsPage: true}
	if !isOnlyFansSite(page) {
		return result, false, nil
	}
	profile := onlyFansProfile(page)
	if profile == "" {
		return result, true, errors.New("OnlyFans 请打开已授权的作者主页，当前页面暂不支持")
	}
	checkCurrent := func() error {
		var raw string
		if err := s.run(ctx, chromedp.Location(&raw)); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("OnlyFans 原页面不可用")
		}
		current, err := url.Parse(raw)
		if err != nil || raw != observedURL || !sameOrigin(page, current) || onlyFansProfile(current) != profile {
			return errors.New("OnlyFans 原页面已更改，已保留采集结果")
		}
		return nil
	}
	if err := checkCurrent(); err != nil {
		return result, true, err
	}
	child, closeChild, err := camoufox.NewPage(ctx, s.tab, false)
	if err != nil {
		return result, true, errors.New("无法创建 OnlyFans 采集标签")
	}
	defer closeChild()
	stopOnCancel := context.AfterFunc(ctx, closeChild)
	defer stopOnCancel()
	run := func(actions ...camoufox.Action) error {
		op, cancel := context.WithTimeout(child, 30*time.Second)
		stop := context.AfterFunc(ctx, cancel)
		defer func() { stop(); cancel() }()
		if err := camoufox.Run(op, actions...); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		return nil
	}
	events := make(chan onlyFansResponse, onlyFansMaxBodies)
	var mu sync.Mutex
	pending := make(map[network.RequestID]onlyFansResponse)
	ready := make(map[network.RequestID]bool)
	var count atomic.Int32
	var overflow atomic.Bool
	send := func(r onlyFansResponse) {
		select {
		case events <- r:
		case <-child.Done():
		default:
			overflow.Store(true)
		}
	}
	camoufox.ListenTarget(child, func(event any) {
		switch e := event.(type) {
		case *fetch.EventRequestPaused:
			allowed := false
			if e.Request != nil {
				target, parseErr := url.Parse(e.Request.URL)
				allowed = e.Request.Method == "GET" && parseErr == nil && sameOrigin(page, target) && onlyFansProfile(target) == profile
			}
			go func() {
				var action camoufox.Action = fetch.FailRequest(e.RequestID, network.ErrorReasonBlockedByClient)
				if allowed {
					action = fetch.ContinueRequest(e.RequestID)
				}
				_ = camoufox.Run(child, action)
			}()
		case *network.EventRequestWillBeSent:
			if e.RedirectResponse != nil {
				mu.Lock()
				request, ok := pending[e.RequestID]
				delete(pending, e.RequestID)
				delete(ready, e.RequestID)
				mu.Unlock()
				if ok {
					request.failed = true
					send(request)
				}
				return
			}
			if e.Request == nil || e.Request.Method != "GET" {
				return
			}
			kind, user := onlyFansAPI(e.Request.URL, page, profile)
			if kind == "" {
				return
			}
			if count.Add(1) > onlyFansMaxBodies {
				overflow.Store(true)
				return
			}
			mu.Lock()
			pending[e.RequestID] = onlyFansResponse{kind: kind, user: user, requestID: e.RequestID}
			mu.Unlock()
		case *network.EventResponseReceived:
			mu.Lock()
			request, ok := pending[e.RequestID]
			if !ok {
				mu.Unlock()
				return
			}
			kind, user := "", ""
			if e.Response != nil {
				kind, user = onlyFansAPI(e.Response.URL, page, profile)
			}
			if e.Response == nil || e.Response.Status != 200 || e.Response.FromServiceWorker || kind != request.kind || user != request.user {
				delete(pending, e.RequestID)
				mu.Unlock()
				request.failed = true
				send(request)
				return
			}
			ready[e.RequestID] = true
			mu.Unlock()
		case *network.EventLoadingFinished:
			mu.Lock()
			request, ok := pending[e.RequestID]
			isReady := ready[e.RequestID]
			delete(pending, e.RequestID)
			delete(ready, e.RequestID)
			mu.Unlock()
			if ok {
				request.failed = !isReady
				send(request)
			}
		case *network.EventLoadingFailed:
			mu.Lock()
			request, ok := pending[e.RequestID]
			delete(pending, e.RequestID)
			delete(ready, e.RequestID)
			mu.Unlock()
			if ok {
				request.failed = true
				send(request)
			}
		}
	})
	initTimeout := time.AfterFunc(30*time.Second, closeChild)
	err = camoufox.Run(child, network.Enable().WithMaxResourceBufferSize(onlyFansMaxBody).WithMaxTotalBufferSize(4*onlyFansMaxBody).WithMaxPostDataSize(1),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{ResourceType: network.ResourceTypeDocument}}))
	initTimeout.Stop()
	if err != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("无法监听 OnlyFans 授权页面")
	}
	if err := run(camoufox.Navigate(page.String())); err != nil {
		if ctx.Err() != nil {
			return result, true, ctx.Err()
		}
		return result, true, errors.New("OnlyFans 作者主页未加载，请在下载浏览器中完成登录或验证")
	}
	var childURL string
	if err := run(chromedp.Location(&childURL)); err != nil {
		return result, true, errors.New("无法读取 OnlyFans 页面")
	}
	current, parseErr := url.Parse(childURL)
	if parseErr != nil || !sameOrigin(page, current) || onlyFansProfile(current) != profile {
		return result, true, errors.New("OnlyFans 页面发生跳转，请完成登录或验证")
	}
	var authorID string
	var incomplete, receivedPosts bool
	var waiting []onlyFansResponse
	seen := make(map[string]bool)
	read := func(request onlyFansResponse) ([]byte, error) {
		var body []byte
		err := run(chromedp.ActionFunc(func(c context.Context) error {
			var e error
			body, e = network.GetResponseBody(request.requestID).Do(c)
			return e
		}))
		if err != nil || len(body) > onlyFansMaxBody {
			return nil, errors.New("OnlyFans 页面响应不可用")
		}
		return body, nil
	}
	processPosts := func(request onlyFansResponse) {
		if request.user != authorID {
			return
		}
		if request.failed {
			incomplete = true
			return
		}
		body, err := read(request)
		if err != nil {
			incomplete = true
			return
		}
		receivedPosts = true
		items, partial, err := onlyFansPostImages(body, authorID, profile)
		if partial || err != nil {
			incomplete = true
		}
		for _, item := range items {
			key := item.Metadata["onlyfans_post_id"] + ":" + item.Metadata["onlyfans_media_id"]
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(result.Items) >= onlyFansMaxImages {
				incomplete = true
				break
			}
			result.Items = append(result.Items, item)
		}
	}
	process := func(request onlyFansResponse) {
		if request.kind == "posts" {
			if authorID == "" {
				waiting = append(waiting, request)
				return
			}
			processPosts(request)
			return
		}
		if request.failed {
			incomplete = true
			return
		}
		body, err := read(request)
		if err != nil {
			incomplete = true
			return
		}
		id, err := onlyFansIdentity(body, profile)
		if err != nil {
			incomplete = true
			return
		}
		if authorID != "" && authorID != id {
			incomplete = true
			return
		}
		authorID = id
		for _, pending := range waiting {
			processPosts(pending)
		}
		waiting = nil
	}
	drain := func() error {
		idle := time.NewTimer(500 * time.Millisecond)
		defer idle.Stop()
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-child.Done():
				return errors.New("OnlyFans 采集标签已关闭")
			case request := <-events:
				process(request)
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(500 * time.Millisecond)
			case <-idle.C:
				return nil
			case <-deadline.C:
				return nil
			}
		}
	}
	for step := 0; step <= onlyFansMaxScrolls; step++ {
		if err := drain(); err != nil {
			return result, true, err
		}
		if err := checkCurrent(); err != nil {
			return result, true, err
		}
		if len(result.Items) >= onlyFansMaxImages {
			incomplete = true
			break
		}
		if step == onlyFansMaxScrolls {
			break
		}
		if err := run(chromedp.Evaluate(`window.scrollBy({top:window.innerHeight,behavior:"instant"})`, nil)); err != nil {
			if ctx.Err() != nil {
				return result, true, ctx.Err()
			}
			incomplete = true
			break
		}
	}
	if overflow.Load() || len(waiting) > 0 {
		incomplete = true
	}
	if authorID == "" {
		return result, true, errors.New("无法确认 OnlyFans 作者身份，请确认已登录并可访问该主页")
	}
	if !receivedPosts {
		return result, true, errors.New("未读取到 OnlyFans 作品列表，请确认已登录并可访问该主页")
	}
	warning := errors.New("OnlyFans 仅采集本次滚动加载的可见图片，结果可能不完整")
	if incomplete {
		warning = errors.Join(warning, errors.New("部分作品被锁定、可见性未知或无法读取"))
	}
	return result, true, warning
}
