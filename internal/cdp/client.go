// Package cdp 封装 chromedp，对上层提供与原软件 CdpClient 等价的能力：
// 标签页管理、DOM 查询、JS 执行、文件上传、截图、状态探测与 Go↔页面桥。
package cdp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	cdprotoCDP "github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	cdt "github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Client 对应一个已启动的浏览器实例（远程分配器）。
type Client struct {
	alloc    context.Context
	allocCan context.CancelFunc
	wsURL    string
}

// NewClient 连接到浏览器级 WS 地址。
func NewClient(wsURL string) (*Client, error) {
	alloc, cancel := chromedp.NewRemoteAllocator(context.Background(), wsURL)
	c := &Client{alloc: alloc, allocCan: cancel, wsURL: wsURL}
	return c, nil
}

// quietCDP 丢掉 cdproto 尚未识别的 IPAddressSpace=Loopback。其余错误仍打到标准日志。
func quietCDP(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "IPAddressSpace") || strings.Contains(msg, "Loopback") {
		return
	}
	log.Printf("%s", msg)
}

// Close 释放分配器。
func (c *Client) Close() { c.allocCan() }

// WSURL 返回浏览器 WS 地址。
func (c *Client) WSURL() string { return c.wsURL }

// Session 一个页面标签页会话，含注入脚本与桥接处理。
type Session struct {
	client       *Client
	ego          *egoTransport
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	hooks        map[string]func(data string) string // 桥命令处理表
	onLog        func(string)                        // console 日志回调
	closed       bool
	newDoc       []string // 新文档注入脚本
	navigateHook string
	hide         atomic.Bool
	parkOnce     sync.Once
	parkMu       sync.Mutex
}

// NewSession 在浏览器上新建标签页并打开 url。
// NewBackgroundSession 复用已经打开的站点标签，没有才在后台开一页。不把窗口拉到前面。
func (c *Client) NewBackgroundSession(rawURL string, onLog func(string)) (*Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, c.wsURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(hostOf(rawURL))
	var ids []string
	for _, item := range targets {
		if item.Type != "page" || item.ID == "" {
			continue
		}
		if host != "" && strings.Contains(strings.ToLower(item.URL), host) {
			ids = append(ids, item.ID)
		}
	}
	var id string
	if len(ids) > 0 {
		id = ids[len(ids)-1]
		for _, extra := range ids[:len(ids)-1] {
			_ = conn.call(ctx, "", "Target.closeTarget", map[string]any{"targetId": extra}, nil)
		}
	} else {
		back := context.WithValue(ctx, stayBackKey{}, true)
		id, err = conn.createPage(back, rawURL)
		if err != nil {
			return nil, err
		}
	}
	// 启动参数已经把窗口放在屏幕外。这里再 setWindowBounds 会把窗口激活到 tulai 前面。
	tab, tabCancel := chromedp.NewContext(c.alloc, chromedp.WithTargetID(targetID(id)), chromedp.WithErrorf(quietCDP))
	return c.finishSession(tab, tabCancel, "", onLog)
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func targetID(id string) cdt.ID { return cdt.ID(id) }

func (c *Client) NewSession(rawURL string, onLog func(string)) (*Session, error) {
	ctx, cancel := chromedp.NewContext(c.alloc, chromedp.WithErrorf(quietCDP))
	return c.finishSession(ctx, cancel, rawURL, onLog)
}

func (c *Client) finishSession(ctx context.Context, cancel context.CancelFunc, rawURL string, onLog func(string)) (*Session, error) {
	s := &Session{client: c, ctx: ctx, cancel: cancel, hooks: map[string]func(data string) string{}, onLog: onLog}
	// 桥事件监听：页面 → Go
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventBindingCalled:
			if e.Name == "tulaiBridge" {
				go s.dispatchBridge(e.Payload)
			}
		case *runtime.EventConsoleAPICalled:
			if onLog != nil && len(e.Args) > 0 {
				var parts []string
				for _, a := range e.Args {
					parts = append(parts, a.Value.String())
				}
				onLog(strings.Join(parts, " "))
			}
		}
	})
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("建立标签页会话: %w", err)
	}
	// 注册桥绑定（对当前与后续文档生效需两者都做）
	if err := s.exec(func(ec context.Context) error {
		return runtime.AddBinding("tulaiBridge").Do(ec)
	}); err != nil {
		cancel()
		return nil, err
	}
	_ = s.InjectBase() // 基础运行时（幂等）
	if rawURL == "" {
		s.quietBackground()
	}
	if rawURL != "" {
		if err := chromedp.Run(ctx, chromedp.Navigate(rawURL), chromedp.WaitReady("body", chromedp.ByQuery)); err != nil {
			// 页面可能加载慢，导航错误不致命
			_ = err
		}
		s.EnsureBase() // 导航后新文档需重注入
	}
	return s, nil
}

// KeepOffscreen 出图期间把窗口留在屏幕外，并把焦点还给 tulai。
// 登录调用过 BringToFront / setWindowBounds 显示窗口时，这里才解除占用。
func (s *Session) KeepOffscreen() {
	if s == nil || s.ego != nil {
		return
	}
	if s.client != nil {
		releaseFront(s.client.wsURL)
	}
	s.armPark()
}

// StayBack 在导航、点击、输入之后再送一次屏幕外。登录占用前台时不动。
func (s *Session) StayBack() {
	if s == nil || !s.hide.Load() {
		return
	}
	s.parkNow()
}

func (s *Session) armPark() {
	if s == nil || s.ego != nil || s.ctx == nil || s.client == nil {
		return
	}
	s.hide.Store(true)
	s.parkOnce.Do(func() { go s.parkLoop() })
	s.parkNow()
}

func (s *Session) parkLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.parkNow()
		}
	}
}

func (s *Session) parkNow() {
	if s == nil || s.ctx == nil || s.ego != nil || s.client == nil || !s.hide.Load() {
		return
	}
	if frontHeld(s.client.wsURL) {
		return
	}
	if !s.parkMu.TryLock() {
		return
	}
	defer s.parkMu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, s.client.wsURL)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.parkFirst(ctx)
}

// Close 关闭标签页会话。
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	if s.ego != nil {
		s.cancel()
		s.ego.close()
		return
	}
	chromedp.Run(s.ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return page.Close().Do(ec)
	}))
	s.cancel()
}

// Ctx 暴露内部 ctx（供引擎做带超时操作）。
func (s *Session) Ctx() context.Context { return s.ctx }

// exec 在目标上执行底层协议命令。
func (s *Session) exec(fn func(ec context.Context) error) error {
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()
	if s.ego != nil {
		return fn(cdprotoCDP.WithExecutor(ctx, s.ego))
	}
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return fn(ec)
	}))
}

// Handle 注册桥命令处理函数（页面 JS: tulaiBridge({cmd, id, data})）。
func (s *Session) Handle(cmd string, fn func(data string) string) {
	s.mu.Lock()
	s.hooks[cmd] = fn
	s.mu.Unlock()
}

// SetNavigateInjection 设置每次导航后自动执行的脚本。
func (s *Session) SetNavigateInjection(js string) {
	s.mu.Lock()
	s.navigateHook = js
	s.mu.Unlock()
	_ = s.exec(func(ec context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(js).Do(ec)
		return err
	})
}

// dispatchBridge 处理页面发来的桥消息并回执。
func (s *Session) dispatchBridge(payload string) {
	// payload: {"cmd":"...","id":1,"data":"..."}
	var msg struct {
		Cmd  string `json:"cmd"`
		ID   int64  `json:"id"`
		Data string `json:"data"`
	}
	if err := jsonUnmarshal([]byte(payload), &msg); err != nil {
		return
	}
	s.mu.Lock()
	fn := s.hooks[msg.Cmd]
	s.mu.Unlock()
	var result string
	if msg.Cmd == "mouseClick" {
		result = s.bridgeMouseClick(msg.Data)
	} else if msg.Cmd == "keyEnter" {
		result = s.bridgeKeyEnter()
	} else if fn != nil {
		result = fn(msg.Data)
	} else {
		result = `{"success":false,"error":"未注册的命令: ` + msg.Cmd + `"}`
	}
	ans, _ := jsonMarshal(map[string]any{"id": msg.ID, "result": result})
	s.Evaluate(fmt.Sprintf(`window.__tulaiResolve && window.__tulaiResolve(%d, %q)`, msg.ID, ans))
}

// Evaluate 执行 JS 表达式（不 await），返回 JSON 值。
func (s *Session) Evaluate(expr string) (string, error) {
	return s.EvaluateWithOpts(expr, false)
}

// EvaluateAsync 执行 JS 并等待 Promise。
func (s *Session) EvaluateAsync(expr string) (string, error) {
	return s.EvaluateWithOpts(expr, true)
}

// EvaluateWithOpts 控制 awaitPromise 的 JS 执行，返回 JSON 序列化结果。
func (s *Session) EvaluateWithOpts(expr string, await bool) (string, error) {
	var out string
	err := s.exec(func(ec context.Context) error {
		v, exc, err := runtime.Evaluate(expr).
			WithAwaitPromise(await).
			WithReturnByValue(true).
			Do(ec)
		if err != nil {
			return err
		}
		if exc != nil {
			return errors.New("JS异常: " + exc.Text)
		}
		if v == nil {
			out = "null"
			return nil
		}
		if v.Value != nil {
			out = v.Value.String()
			return nil
		}
		out = v.Type.String()
		return nil
	})
	return out, err
}

// EvaluateBool 执行 JS 返回布尔。
func (s *Session) EvaluateBool(expr string) bool {
	out, err := s.Evaluate(expr)
	if err != nil {
		return false
	}
	var b bool
	if jsonUnmarshal([]byte(out), &b) == nil {
		return b
	}
	return false
}

// EvaluateInt 执行 JS 返回整数。
func (s *Session) EvaluateInt(expr string) int {
	out, err := s.Evaluate(expr)
	if err != nil {
		return -1
	}
	var n int
	if jsonUnmarshal([]byte(out), &n) == nil {
		return n
	}
	return -1
}

// Navigate 导航并等待 body。
func (s *Session) Navigate(url string) error {
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()
	if s.ego != nil {
		return s.ego.request(ctx, "goto", map[string]any{"url": url}, nil)
	}
	err := chromedp.Run(ctx, chromedp.Navigate(url))
	s.StayBack()
	return err
}

// RenewPage 为旧版注入迁移建立干净页面；ego lite 保留旧页及浏览器登录态。
func (s *Session) RenewPage() error {
	if s.ego != nil {
		ctx, cancel := context.WithTimeout(s.ctx, 75*time.Second)
		defer cancel()
		select {
		case egoInit <- struct{}{}:
			defer func() { <-egoInit }()
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := s.ego.request(ctx, "renew", nil, nil); err != nil {
			return err
		}
	} else {
		out, err := s.Evaluate(`location.href`)
		if err != nil {
			return err
		}
		var url string
		if err := ParseJSValue(out, &url); err != nil {
			return err
		}
		if err := s.Navigate(url); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.newDoc = nil
	s.navigateHook = ""
	s.mu.Unlock()
	return s.InjectBase()
}

// WaitForSelectorText 等待选择器出现文本，返回是否找到。
func (s *Session) WaitForSelector(sel string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	if s.ego != nil {
		return s.ego.request(ctx, "wait", map[string]any{"selector": sel, "timeout": timeout.Milliseconds()}, nil)
	}
	return chromedp.Run(ctx, chromedp.WaitVisible(sel, chromedp.ByQuery))
}

// Click 通过浏览器鼠标协议单击元素。
func (s *Session) Click(sel string) error {
	out, err := s.EvaluateAsync(fmt.Sprintf(`tulaiClick(%q)`, sel))
	if err != nil {
		return err
	}
	if out != "true" {
		return fmt.Errorf("目标无法点击")
	}
	return nil
}

// emulateFocus 让后台页以为自己有焦点，输入和点击才能进到豆包。
// 不用 activateTarget / bringToFront，那些会把窗口拉到前面。
func (s *Session) emulateFocus() {
	if s == nil || s.ego != nil {
		return
	}
	_ = s.exec(func(ctx context.Context) error {
		return emulation.SetFocusEmulationEnabled(true).Do(ctx)
	})
}

// quietBackground 出图页不抢前台：模拟焦点，拒绝下载栏，并把窗口送出屏幕。
func (s *Session) quietBackground() {
	s.emulateFocus()
	if s == nil || s.ego != nil {
		return
	}
	_ = s.exec(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Browser == nil {
			return nil
		}
		return cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorDeny).Do(cdprotoCDP.WithExecutor(ctx, c.Browser))
	})
	s.armPark()
}

// KeepOnePage 关掉同一站点多出来的标签，只留当前这一张。
// 每单新开一页又不关，窗口里会堆上好几张豆包。关的是别的标签，不激活窗口。
func (s *Session) KeepOnePage(rawURL string) {
	if s == nil || s.ego != nil {
		return
	}
	host := strings.ToLower(hostOf(rawURL))
	if host == "" {
		return
	}
	_ = s.exec(func(ctx context.Context) error {
		c := chromedp.FromContext(ctx)
		if c == nil || c.Browser == nil || c.Target == nil {
			return nil
		}
		infos, err := cdt.GetTargets().Do(cdprotoCDP.WithExecutor(ctx, c.Browser))
		if err != nil {
			return err
		}
		for _, info := range infos {
			if info.Type != "page" || info.TargetID == "" || info.TargetID == c.Target.TargetID {
				continue
			}
			if !strings.Contains(strings.ToLower(info.URL), host) {
				continue
			}
			_ = cdt.CloseTarget(info.TargetID).Do(cdprotoCDP.WithExecutor(ctx, c.Browser))
		}
		return nil
	})
}

// InsertText 选中编辑器全文后用浏览器真实输入替换，触发框架可信输入事件。
func (s *Session) InsertText(sel, text string) error {
	s.emulateFocus()
	_, err := s.Evaluate(fmt.Sprintf(`(()=>{
const e=document.querySelector(%q);
if(!e)throw new Error('找不到输入框');
if(e.matches(':disabled')||e.readOnly||e.closest('[aria-disabled="true"],[aria-readonly="true"]'))throw new Error('输入框已禁用或只读');
if(e instanceof HTMLInputElement&&!['text','search','email','tel','url','password','number'].includes(e.type))throw new Error('元素不可编辑');
if(e instanceof HTMLInputElement||e instanceof HTMLTextAreaElement){
  e.focus();e.select();
}else if(e.isContentEditable){
  e.focus();const r=document.createRange();r.selectNodeContents(e);const selection=window.getSelection();selection.removeAllRanges();selection.addRange(r);
}else throw new Error('元素不可编辑');
if(document.activeElement!==e&&!e.contains(document.activeElement))throw new Error('无法聚焦输入框');
return true;
})()`, sel))
	if err != nil {
		return err
	}
	err = s.exec(func(ec context.Context) error { return input.InsertText(text).Do(ec) })
	s.StayBack()
	return err
}

// SetUploadFilesNth 给第 n 个 input[type=file] 设置文件，由浏览器发送输入事件。
// 实现：JS 先给目标 input 打标记，浏览器上传后只清除标记，避免重复 change。
func (s *Session) SetUploadFilesNth(n int, files []string) error {
	if _, err := s.Evaluate(fmt.Sprintf(`tulaiMarkFileInput(%d)`, n)); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()
	var err error
	if s.ego != nil {
		paths := make([]string, len(files))
		for i, file := range files {
			paths[i], err = filepath.Abs(file)
			if err != nil {
				return err
			}
		}
		err = s.ego.request(ctx, "upload", map[string]any{"selector": `input[type=file][data-tulai-upload="1"]`, "files": paths}, nil)
	} else {
		err = chromedp.Run(ctx, chromedp.SetUploadFiles(`input[type=file][data-tulai-upload="1"]`, files))
	}
	if err == nil {
		_, _ = s.Evaluate(`tulaiUnmarkFileInput()`)
	}
	return err
}

// CaptureScreenshotRect 截取页面坐标区域（CSS 像素），返回 PNG 字节。
func (s *Session) CaptureScreenshotRect(x, y, w, h float64) ([]byte, error) {
	var pngData []byte
	err := s.exec(func(ec context.Context) error {
		v, err := page.CaptureScreenshot().
			WithClip(&page.Viewport{X: x, Y: y, Width: w, Height: h, Scale: 1}).
			WithFormat(page.CaptureScreenshotFormatPng).
			Do(ec)
		if err == nil {
			pngData = v
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return pngData, nil
}

// CaptureElementScreenshot 截取选择器元素区域。
func (s *Session) CaptureElementScreenshot(sel string) ([]byte, error) {
	if s.ego != nil {
		out, err := s.Evaluate(fmt.Sprintf(`(()=>{const e=document.querySelector(%q);if(!e)throw new Error('元素不存在');e.scrollIntoView();const r=e.getBoundingClientRect();return {x:r.x+scrollX,y:r.y+scrollY,w:r.width,h:r.height}})()`, sel))
		if err != nil {
			return nil, err
		}
		var rect struct{ X, Y, W, H float64 }
		if err := ParseJSValue(out, &rect); err != nil {
			return nil, err
		}
		return s.CaptureScreenshotRect(rect.X, rect.Y, rect.W, rect.H)
	}
	var buf []byte
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()
	err := chromedp.Run(ctx, chromedp.Screenshot(sel, &buf, chromedp.NodeVisible, chromedp.ByQuery))
	return buf, err
}

// HasText 判断页面文本是否匹配正则。
func (s *Session) HasText(pattern string) bool {
	out, err := s.Evaluate(fmt.Sprintf(`new RegExp(%q).test(document.body.innerText)`, pattern))
	if err != nil {
		return false
	}
	var b bool
	jsonUnmarshal([]byte(out), &b)
	return b
}

// KeepAlive 保活：周期性执行轻量调用，防止标签页被回收。
func (s *Session) KeepAlive(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = s.Evaluate(`1`)
			}
		}
	}()
}
