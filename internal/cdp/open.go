package cdp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrDebugUnavailable 表示调试端口暂时连不上。浏览器若已带着登录地址启动，页面可以留着。
var ErrDebugUnavailable = errors.New("连不上调试端口")

// stayBackKey 为真时导航不把窗口拉到前面。出图留在屏幕外；登录要把窗口拿回屏幕。
type stayBackKey struct{}

func stayBack(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	back, _ := ctx.Value(stayBackKey{}).(bool)
	return back
}

// OpenURL 把已经打开的可见页面导航到 url，并确认地址离开了 about:blank。
// 控制连接退出后标签页仍由浏览器保留。已经在目标站点上时只把窗口拉到前面，不重新加载。
func OpenURL(wsURL, rawURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return OpenURLContext(ctx, wsURL, rawURL)
}

// OpenURLBackground 同 OpenURLContext，但不把窗口拉到前面。
func OpenURLBackground(ctx context.Context, endpoint, rawURL string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
	}
	return OpenURLContext(context.WithValue(ctx, stayBackKey{}, true), endpoint, rawURL)
}

// OpenURLContext 同 OpenURL，使用调用方的超时。
func OpenURLContext(ctx context.Context, endpoint, rawURL string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || blankLike(rawURL) {
		return errors.New("登录页地址是空的")
	}
	delays := []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, 1000 * time.Millisecond, 1500 * time.Millisecond}
	var last error
	for _, wait := range delays {
		if err := sleepCtx(ctx, wait); err != nil {
			if errors.Is(last, ErrDebugUnavailable) {
				return ErrDebugUnavailable
			}
			return err
		}
		err := navigateVisible(ctx, endpoint, rawURL)
		if err == nil {
			return nil
		}
		last = err
	}
	if errors.Is(last, ErrDebugUnavailable) {
		return ErrDebugUnavailable
	}
	if last == nil {
		last = errors.New("窗口仍停在空白页")
	}
	return last
}

// VisibleURL 返回当前可见页面的地址。没有可见页时用第一个页面。
func VisibleURL(ctx context.Context, endpoint string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	hits, err := conn.pageHits(ctx)
	if err != nil {
		return "", err
	}
	return visibleHref(hits), nil
}

// PageSummary 读取可见页的标题和开头正文，用来核对是不是空白壳。
func PageSummary(ctx context.Context, endpoint string) (title, text string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return "", "", err
	}
	defer conn.Close()
	hits, err := conn.pageHits(ctx)
	if err != nil {
		return "", "", err
	}
	id := ""
	for _, hit := range hits {
		if hit.Vis == "visible" && hit.ID != "" {
			id = hit.ID
			break
		}
	}
	if id == "" && len(hits) > 0 {
		id = hits[0].ID
	}
	if id == "" {
		return "", "", errors.New("没有页面")
	}
	var snap struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := conn.EvalJSON(ctx, id, `({title:document.title, text:String((document.body&&document.body.innerText)||'').slice(0,240)})`, &snap); err != nil {
		return "", "", err
	}
	return snap.Title, snap.Text, nil
}

// CapturePNG 截当前可见页面。
func CapturePNG(ctx context.Context, endpoint string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	hits, err := conn.pageHits(ctx)
	if err != nil {
		return nil, err
	}
	id := ""
	for _, hit := range hits {
		if hit.Vis == "visible" && hit.ID != "" {
			id = hit.ID
			break
		}
	}
	if id == "" && len(hits) > 0 {
		id = hits[0].ID
	}
	if id == "" {
		return nil, errors.New("没有可截图的页面")
	}
	session, err := conn.session(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = conn.call(ctx, session, "Page.enable", map[string]any{}, nil)
	var out struct {
		Data string `json:"data"`
	}
	if err := conn.call(ctx, session, "Page.captureScreenshot", map[string]any{"format": "png"}, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Data)
}

type pageHit struct {
	ID  string
	URL string
	Vis string
}

func navigateVisible(ctx context.Context, endpoint, want string) error {
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrDebugUnavailable, err)
	}
	defer conn.Close()
	hits, err := conn.pageHits(ctx)
	if err != nil {
		return err
	}
	id, needNav := chooseLoginPage(hits, want)
	if id != "" && !needNav {
		if err := conn.activate(ctx, id); err != nil {
			return err
		}
		conn.closeBlankPages(ctx, id)
		return nil
	}
	if id == "" {
		id, err = conn.createPage(ctx, want)
		if err != nil {
			return err
		}
	} else if err := conn.navigate(ctx, id, want); err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		hits, err = conn.pageHits(ctx)
		if err == nil {
			for _, hit := range hits {
				if hit.ID == id && landed(hit.URL, want) {
					if err := conn.activate(ctx, id); err != nil {
						return err
					}
					conn.closeBlankPages(ctx, id)
					return nil
				}
			}
			// 起始页被浏览器换成了另一张已经到达站点的标签。
			if landedID, nav := chooseLoginPage(hits, want); landedID != "" && !nav {
				if err := conn.activate(ctx, landedID); err != nil {
					return err
				}
				conn.closeBlankPages(ctx, landedID)
				return nil
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		if err := sleepCtx(ctx, 150*time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("窗口仍停在空白页")
}

func chooseLoginPage(pages []pageHit, want string) (id string, navigate bool) {
	var visibleLanded, anyLanded, visibleBlank, anyBlank, visibleOther string
	for _, page := range pages {
		if page.ID == "" {
			continue
		}
		if landed(page.URL, want) {
			if anyLanded == "" {
				anyLanded = page.ID
			}
			if page.Vis == "visible" && visibleLanded == "" {
				visibleLanded = page.ID
			}
			continue
		}
		if blankLike(page.URL) {
			if anyBlank == "" {
				anyBlank = page.ID
			}
			if page.Vis == "visible" && visibleBlank == "" {
				visibleBlank = page.ID
			}
			continue
		}
		if page.Vis == "visible" && visibleOther == "" {
			visibleOther = page.ID
		}
	}
	switch {
	case visibleLanded != "":
		return visibleLanded, false
	case anyLanded != "":
		return anyLanded, false
	case visibleBlank != "":
		return visibleBlank, true
	case anyBlank != "":
		return anyBlank, true
	case visibleOther != "":
		return visibleOther, true
	default:
		return "", true
	}
}

func visibleHref(pages []pageHit) string {
	for _, page := range pages {
		if page.Vis == "visible" && strings.TrimSpace(page.URL) != "" {
			return page.URL
		}
	}
	for _, page := range pages {
		if strings.TrimSpace(page.URL) != "" {
			return page.URL
		}
	}
	return ""
}

func blankLike(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case u == "", u == "about:blank", strings.HasPrefix(u, "about:blank"):
		return true
	case strings.HasPrefix(u, "chrome://"), strings.HasPrefix(u, "edge://"), strings.HasPrefix(u, "devtools://"), strings.HasPrefix(u, "chrome-error://"):
		return true
	case strings.Contains(u, "ntp.msn.com"):
		return true
	default:
		return false
	}
}

func landed(current, want string) bool {
	if blankLike(current) {
		return false
	}
	cu, err1 := url.Parse(current)
	wu, err2 := url.Parse(want)
	if err1 != nil || err2 != nil {
		return false
	}
	if cu.Scheme != "http" && cu.Scheme != "https" {
		return false
	}
	return sameSite(cu.Hostname(), wu.Hostname())
}

func sameSite(a, b string) bool {
	a = strings.TrimPrefix(strings.ToLower(a), "www.")
	b = strings.TrimPrefix(strings.ToLower(b), "www.")
	if a == "" || b == "" {
		return false
	}
	if a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a) {
		return true
	}
	// 千问的登录地址写的是 tongyi.com，站点会 301 到 qianwen.com。
	return loginSite(a) != "" && loginSite(a) == loginSite(b)
}

func loginSite(host string) string {
	switch {
	case host == "tongyi.com" || strings.HasSuffix(host, ".tongyi.com") ||
		host == "qianwen.com" || strings.HasSuffix(host, ".qianwen.com") ||
		host == "qwen.ai" || strings.HasSuffix(host, ".qwen.ai"):
		return "qianwen"
	default:
		return ""
	}
}

func (c *SliderConn) pageHits(ctx context.Context) ([]pageHit, error) {
	targets, err := c.targets(ctx)
	if err != nil {
		return nil, err
	}
	var hits []pageHit
	for _, item := range targets {
		if item.Type != "page" || item.ID == "" {
			continue
		}
		hit := pageHit{ID: item.ID, URL: item.URL}
		var snap struct {
			Href string `json:"href"`
			Vis  string `json:"vis"`
		}
		if err := c.EvalJSON(ctx, item.ID, `({href:location.href, vis:document.visibilityState})`, &snap); err == nil {
			if snap.Href != "" {
				hit.URL = snap.Href
			}
			hit.Vis = snap.Vis
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func (c *SliderConn) navigate(ctx context.Context, id, rawURL string) error {
	session, err := c.session(ctx, id)
	if err != nil {
		return err
	}
	var result struct {
		ErrorText string `json:"errorText"`
	}
	if err := c.call(ctx, session, "Page.navigate", map[string]any{"url": rawURL}, &result); err != nil {
		return err
	}
	if result.ErrorText != "" {
		return errors.New(result.ErrorText)
	}
	return c.activate(ctx, id)
}

// pageTarget 是 Target.createTarget 的参数。后台页不能 newWindow，否则窗口会被拉到前面。
func pageTarget(rawURL string, background bool) map[string]any {
	return map[string]any{
		"url":        rawURL,
		"background": background,
		"newWindow":  false,
	}
}

func (c *SliderConn) createPage(ctx context.Context, rawURL string) (string, error) {
	var out struct {
		TargetID string `json:"targetId"`
	}
	// background=true 时 Chrome 不激活新标签。newWindow 会另开一扇到前台的窗口，出图不用。
	if err := c.call(ctx, "", "Target.createTarget", pageTarget(rawURL, stayBack(ctx)), &out); err != nil {
		return "", err
	}
	if out.TargetID == "" {
		return "", errors.New("没有新页面")
	}
	return out.TargetID, nil
}

func (c *SliderConn) activate(ctx context.Context, id string) error {
	if stayBack(ctx) {
		return nil
	}
	// 只有登录才把窗口拿回屏幕并激活。出图路径不进这里。
	_ = c.moveWindow(ctx, id, true)
	if err := c.Activate(ctx, id); err != nil {
		return err
	}
	session, err := c.session(ctx, id)
	if err != nil {
		return err
	}
	_ = c.call(ctx, session, "Page.bringToFront", map[string]any{}, nil)
	return nil
}

// moveWindow 把浏览器窗口放到屏幕外，或在登录时挪回可见区域。
// setWindowBounds 会把窗口激活到前台。出图挪走之后把焦点还给 tulai。
// 窗口管理器把负坐标夹回左上角时再最小化，并用固定视口保住布局尺寸。
func (c *SliderConn) moveWindow(ctx context.Context, targetID string, show bool) error {
	if show && c != nil {
		holdFront(c.endpoint)
	}
	changed, err := c.placeWindow(ctx, targetID, show)
	if err != nil {
		return err
	}
	if !show && changed && (c == nil || !frontHeld(c.endpoint)) {
		focusCaller()
	}
	return nil
}

func (c *SliderConn) placeWindow(ctx context.Context, targetID string, show bool) (bool, error) {
	if c == nil || targetID == "" {
		return false, nil
	}
	var win windowBounds
	if err := c.call(ctx, "", "Browser.getWindowForTarget", map[string]any{"targetId": targetID}, &win); err != nil {
		return false, err
	}
	// 已经最小化就别先恢复。parkSteps 对非 normal 会先写 windowState=normal，那一下又把窗口拉回屏幕。
	if !show && win.Bounds.WindowState == "minimized" {
		return false, nil
	}
	if show {
		bounds := map[string]any{"windowState": "normal", "left": 80, "top": 48, "width": 1280, "height": 900}
		return true, c.call(ctx, "", "Browser.setWindowBounds", map[string]any{"windowId": win.WindowID, "bounds": bounds}, nil)
	}
	steps := parkSteps(win.Bounds.WindowState, win.Bounds.Left, win.Bounds.Top)
	for _, bounds := range steps {
		if err := c.call(ctx, "", "Browser.setWindowBounds", map[string]any{"windowId": win.WindowID, "bounds": bounds}, nil); err != nil {
			return true, err
		}
	}
	if err := c.call(ctx, "", "Browser.getWindowForTarget", map[string]any{"targetId": targetID}, &win); err != nil {
		return len(steps) > 0, nil
	}
	minimized := false
	if needsMinimize(win.Bounds.WindowState, win.Bounds.Left, win.Bounds.Top) {
		minimized = true
		if err := c.call(ctx, "", "Browser.setWindowBounds", map[string]any{
			"windowId": win.WindowID,
			"bounds":   map[string]any{"windowState": "minimized"},
		}, nil); err != nil {
			return true, err
		}
		_ = c.pinLayout(ctx, targetID)
	}
	return len(steps) > 0 || minimized, nil
}

type windowBounds struct {
	WindowID int `json:"windowId"`
	Bounds   struct {
		Left        int    `json:"left"`
		Top         int    `json:"top"`
		WindowState string `json:"windowState"`
	} `json:"bounds"`
}

// needsMinimize 是负坐标被夹回屏幕之后。已经最小化或已经在屏外就不用再改。
func needsMinimize(state string, left, top int) bool {
	if state == "minimized" {
		return false
	}
	return left > -2000 || top > -2000
}

func (c *SliderConn) pinLayout(ctx context.Context, targetID string) error {
	session, err := c.session(ctx, targetID)
	if err != nil {
		return err
	}
	return c.call(ctx, session, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": 1280, "height": 900, "deviceScaleFactor": 1, "mobile": false,
	}, nil)
}

// ParkBrowser 在出图冷启动连上调试端口后把窗口送出屏幕。登录占用前台时不动。
func ParkBrowser(ctx context.Context, endpoint string) {
	if ctx == nil || frontHeld(endpoint) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.parkFirst(ctx)
}

func (c *SliderConn) parkFirst(ctx context.Context) error {
	targets, err := c.targets(ctx)
	if err != nil {
		return err
	}
	for _, item := range targets {
		if item.Type == "page" && item.ID != "" {
			return c.moveWindow(ctx, item.ID, false)
		}
	}
	return nil
}

var frontHold sync.Map

func frontKey(endpoint string) string {
	s := strings.TrimSpace(endpoint)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}
	return u.Host
}

func holdFront(endpoint string) {
	if key := frontKey(endpoint); key != "" {
		frontHold.Store(key, struct{}{})
	}
}

func releaseFront(endpoint string) {
	if key := frontKey(endpoint); key != "" {
		frontHold.Delete(key)
	}
}

// ReleaseLoginFront 在登录窗口关掉之后调用。
// 同一端口接着出图时，才能再把窗口放到屏幕外。
func ReleaseLoginFront(endpoint string) { releaseFront(endpoint) }

func frontHeld(endpoint string) bool {
	key := frontKey(endpoint)
	if key == "" {
		return false
	}
	_, ok := frontHold.Load(key)
	return ok
}

// focusCaller 把焦点还给本进程的窗口。出图只在 Linux X11 上做；没有 xdotool 就跳过。
func focusCaller() {
	if runtime.GOOS != "linux" || os.Getenv("DISPLAY") == "" {
		return
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return
	}
	_ = exec.Command("xdotool", "search", "--pid", strconv.Itoa(os.Getpid()), "windowactivate").Run()
}

// parkSteps 是出图时把窗口留在屏幕外的 setWindowBounds。
// 已经在屏幕外就什么都不发：再写一次 bounds 会把窗口激活到前台。
// Chrome 收到 windowState=normal 时，最大化或最小化的窗口只走 Restore()，
// 坐标被丢掉，Restore 还会把窗口拉到 tulai 前面。所以先单独恢复，再只写坐标。
func parkSteps(state string, left, top int) []map[string]any {
	if state == "normal" && left <= -2000 && top <= -2000 {
		return nil
	}
	var steps []map[string]any
	if state != "" && state != "normal" {
		steps = append(steps, map[string]any{"windowState": "normal"})
	}
	steps = append(steps, map[string]any{"left": -2400, "top": -2400, "width": 1280, "height": 900})
	return steps
}

func (c *SliderConn) closeBlankPages(ctx context.Context, keep string) {
	hits, err := c.pageHits(ctx)
	if err != nil {
		return
	}
	for _, hit := range hits {
		if hit.ID == "" || hit.ID == keep || !blankLike(hit.URL) {
			continue
		}
		_ = c.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": hit.ID}, nil)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
