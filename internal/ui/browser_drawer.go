package ui

import (
	"bytes"
	"context"
	"image"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

type browserDrawerState struct {
	row           store.Browser
	cancel        context.CancelFunc
	done          chan struct{}
	inputs        chan cdp.PageInput
	held          atomic.Bool
	manual        bool
	resuming      bool
	loading       bool
	err           string
	title, url    string
	bmp           *ui.Bitmap
	width, height float64
	imageLocal    ui.Rect
	caret         ui.Rect
	compose       string
	copyText      string
	copyReady     bool
}

func (a *App) openBrowserDrawer(row store.Browser) {
	var previous <-chan struct{}
	if a.browserDrawer != nil {
		previous = a.browserDrawer.done
		a.closeBrowserDrawer()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &browserDrawerState{row: row, cancel: cancel, done: make(chan struct{}), inputs: make(chan cdp.PageInput, 128), loading: true}
	a.browserDrawer = s
	a.mu.Lock()
	a.browserViewingID = row.ID
	if a.loginWatchGen == nil {
		a.loginWatchGen = map[int64]int{}
	}
	a.loginWatchGen[row.ID]++
	a.mu.Unlock()
	a.pushProxy()
	backend, root := a.browserBackend, a.dataRoot
	go func() {
		defer close(s.done)
		defer s.releaseManual(a.engine)
		if previous != nil {
			select {
			case <-previous:
			case <-ctx.Done():
				return
			}
		}
		if a.manager == nil {
			a.browserDrawerUpdate(s, func() { s.loading, s.err = false, "无法启动浏览器" })
			return
		}
		ws, err := a.manager.Start(row, root, backend, accountHomeURL(row), true)
		if err != nil || ctx.Err() != nil {
			if err != nil {
				a.browserDrawerUpdate(s, func() { s.loading, s.err = false, "打开失败："+err.Error() })
			}
			return
		}
		if a.store != nil {
			a.store.SetRunning(row.ID, true)
		}
		connectCtx, stop := context.WithTimeout(ctx, 8*time.Second)
		// 浏览器关闭了最后一页时进程和扩展仍可能在运行；补回平台页，已有会话则直接复用。
		err = cdp.OpenURLBackground(connectCtx, ws, accountHomeURL(row))
		var page *cdp.PageView
		if err == nil {
			page, err = cdp.AttachPageView(connectCtx, ws, accountHomeURL(row))
		}
		stop()
		if err != nil {
			a.browserDrawerUpdate(s, func() { s.loading, s.err = false, "连接失败："+err.Error() })
			return
		}
		defer page.Close()
		a.runBrowserDrawer(ctx, s, page)
	}()
}

func (s *browserDrawerState) releaseManual(engine *tasks.Engine) {
	if s.held.Swap(false) && engine != nil {
		engine.SetAccountManual(s.row.ID, false)
	}
}

func (a *App) closeBrowserDrawer() {
	s := a.browserDrawer
	if s == nil {
		return
	}
	a.browserDrawer = nil
	a.mu.Lock()
	a.browserViewingID = 0
	a.mu.Unlock()
	s.cancel()
	go func() {
		<-s.done
		s.releaseManual(a.engine)
	}()
}

func (a *App) browserDrawerUpdate(s *browserDrawerState, fn func()) {
	apply := func() {
		if a.browserDrawer == s {
			fn()
		}
	}
	if a.window != nil {
		if !a.window.IsDestroyed() {
			a.window.Update(apply)
		}
	} else {
		apply()
	}
}

func (a *App) runBrowserDrawer(ctx context.Context, s *browserDrawerState, page *cdp.PageView) {
	// shortcut: 每秒最多四帧；需要连续动画或更低延迟时改用 CDP screencast。
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	lastProbe := time.Time{}
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case in := <-s.inputs:
			if in.Kind == "resume" {
				s.releaseManual(a.engine)
				a.browserDrawerUpdate(s, func() { s.resuming = false })
				continue
			}
			if ctx.Err() != nil || !s.held.Load() || a.browserDrawerBusy(s.row.ID) {
				continue
			}
			opCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			text, err := page.Input(opCtx, in)
			stop()
			if err != nil {
				a.browserDrawerUpdate(s, func() { s.err = "网页操作失败，请重新连接：" + err.Error() })
				return
			}
			if (in.Kind == "copy" || in.Kind == "cut") && text != "" {
				a.browserDrawerUpdate(s, func() { s.copyText, s.copyReady = text, true })
			}
		case <-tick.C:
			frameCtx, stop := context.WithTimeout(ctx, 4*time.Second)
			frame, err := page.Frame(frameCtx)
			var bmp image.Image
			if err == nil {
				bmp, _, err = image.Decode(bytes.NewReader(frame.JPEG))
			}
			if err != nil {
				stop()
				a.browserDrawerUpdate(s, func() { s.loading, s.err = false, "浏览器连接已断开，请重新连接："+err.Error() })
				return
			}
			if time.Since(lastProbe) >= 2*time.Second {
				lastProbe = time.Now()
				if state, err := page.LoginState(frameCtx); err == nil && state == "in" && a.store != nil {
					a.store.ConfirmLogin(s.row.ID)
					a.clearLoginHold(s.row.ID)
				}
			}
			stop()
			a.browserDrawerUpdate(s, func() {
				if s.bmp == nil {
					s.bmp = ui.NewBitmap(bmp)
				} else {
					s.bmp.Update(bmp)
				}
				s.width, s.height = frame.Width, frame.Height
				s.title, s.url, s.loading = frame.Title, frame.URL, false
			})
		}
	}
}

func (a *App) browserDrawerBusy(id int64) bool {
	return a.engine != nil && a.engine.AccountBusy(id) && !a.engine.AccountAwaitingUser(id)
}

func (a *App) browserDrawerView(c *ui.Context) {
	s := a.browserDrawer
	if s == nil {
		return
	}
	if s.copyReady {
		c.WriteClipboard(s.copyText)
		s.copyText, s.copyReady = "", false
	}
	tok := a.look
	w, _ := c.Size()
	drawerW := min(w, max(float32(680), w*0.68))
	busy := a.browserDrawerBusy(s.row.ID)
	manual := s.manual && !busy && s.err == "" && !s.loading
	ui.Overlay(c, func() {
		back := ui.Row(c.Key("browser-drawer-backdrop")).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, 0.22)).Modal()
		if back.OverlayShortcut(0, ui.KeyEscape) {
			a.closeBrowserDrawer()
		}
		back.Children(func() {
			ui.Box(c.Key("dismiss-browser-drawer")).Grow(1).FillHeight().OnClick(a.closeBrowserDrawer)
			ui.Column(c.Key("browser-drawer")).Width(drawerW).FillHeight().Background(tok.card).
				BorderWidth(0, 0, 0, 1).BorderColor(tok.border).Shadow(-6, 0, 24, 0, ui.RGBA(0, 0, 0, 0.12)).
				Role(ui.RoleDialog).Label("浏览器 · " + s.row.Name).Transition(motionTravel(12, 0)).Children(func() {
				ui.Row(c).FillWidth().Gap(8).Padding(14, 16).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
						ui.Text(c, s.row.Name).FontSize(tok.title).Bold().SingleLine()
						label := "仅查看"
						if s.manual {
							label = "手动操作 · 该账号已暂停自动出图"
							if busy {
								label = "等待当前任务完成，再进入手动操作"
							}
						}
						ui.Text(c, label).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
					})
					label := "接管操作"
					if s.manual {
						label = "恢复自动"
					}
					if s.resuming {
						label = "正在恢复…"
					}
					ui.Button(c, label).FontSize(tok.caption).Disabled(s.loading || s.resuming || s.err != "").OnClick(func() {
						if s.manual {
							s.manual, s.resuming = false, true
							a.browserDrawerSend(s, cdp.PageInput{Kind: "resume"})
						} else {
							if a.engine != nil {
								a.engine.SetAccountManual(s.row.ID, true)
							}
							s.held.Store(true)
							s.manual = true
						}
					})
					headless := a.manager != nil && a.manager.IsHeadless(s.row.ID)
					standalone := ui.Button(c, "独立窗口").FontSize(tok.caption).Disabled(s.loading || headless)
					if headless {
						standalone.Tooltip("后台运行时请在此接管操作；需要独立窗口可先停止账号再启动")
					}
					standalone.OnClick(func() {
						go func() {
							if err := cdp.BringToFront(context.Background(), loginEndpoint(s.row)); err != nil {
								a.browserDrawerUpdate(s, func() { s.err = "无法唤起浏览器：" + err.Error() })
							}
						}()
					})
					ui.Button(c, "收起").FontSize(tok.caption).Label("收起浏览器").OnClick(a.closeBrowserDrawer)
				})
				ui.Row(c).FillWidth().Gap(8).Padding(8, 16).Background(tok.sunken).AlignItems(ui.Center).Children(func() {
					ui.Text(c, s.url).Grow(1).MinWidth(0).SingleLine().FontSize(tok.caption).TextColor(tok.muted).Tooltip(s.url)
					ui.Button(c, "刷新网页").FontSize(tok.caption).Disabled(!manual).OnClick(func() { a.browserDrawerSend(s, cdp.PageInput{Kind: "reload"}) })
				})
				if s.err != "" {
					ui.Row(c).FillWidth().Gap(10).Padding(10, 16).AlignItems(ui.Center).Children(func() {
						ui.Text(c, s.err).Grow(1).MinWidth(0).FontSize(tok.caption).TextColor(tok.danger)
						ui.Button(c, "重新连接").OnClick(func() { a.openBrowserDrawer(s.row) })
					})
				}
				stage := ui.Box(c.Key("browser-page")).FillWidth().Grow(1).MinHeight(0).Clip().Focusable().
					Label("当前浏览器网页").TextCaret(s.caret).InputMethod(true).Background(tok.sunken)
				stage.Draw(func(p *ui.Painter, r ui.Rect) {
					if s.bmp == nil {
						return
					}
					dst := editContainRect(s.bmp, r)
					s.imageLocal = ui.Rect{X: dst.X - r.X, Y: dst.Y - r.Y, W: dst.W, H: dst.H}
					p.ImageRegion(s.bmp, ui.Rect{}, dst, ui.ImageOptions{})
				})
				stage.HandleInput(func(ev ui.InputEvent) bool { return a.browserDrawerInput(c, s, ev) })
				if s.loading {
					stage.Children(func() {
						ui.Column(c).Fill().Center().Gap(12).Children(func() {
							ui.Spinner(c).Size(24, 24)
							ui.Text(c, "正在连接浏览器…").TextColor(tok.muted)
						})
					})
				}
				footer := "查看当前网页；接管后可以登录、输入和处理验证。"
				if manual {
					footer = "手动操作中；恢复自动或收起后，该账号继续接收任务。"
				}
				if s.compose != "" {
					footer = s.compose
				}
				ui.Text(c, footer).FontSize(tok.caption).TextColor(tok.muted).Padding(10, 16).FillWidth()
			})
		})
	})
}

func (a *App) browserDrawerSend(s *browserDrawerState, in cdp.PageInput) bool {
	if in.Kind == "mouseMoved" && len(s.inputs) > 16 {
		return true
	}
	select {
	case s.inputs <- in:
		return true
	default:
		s.err = "网页操作积压，请收起后重新连接"
		return false
	}
}

func (a *App) browserDrawerInput(c *ui.Context, s *browserDrawerState, ev ui.InputEvent) bool {
	if !s.manual || s.loading || s.err != "" || a.browserDrawerBusy(s.row.ID) {
		return false
	}
	in := cdp.PageInput{Modifiers: browserModifiers(ev.Mods)}
	switch ev.Kind {
	case ui.InputPointerDown, ui.InputPointerUp, ui.InputPointerMove, ui.InputScroll:
		r := s.imageLocal
		if r.W <= 0 || r.H <= 0 || s.width <= 0 || s.height <= 0 {
			return false
		}
		outside := ev.X < r.X || ev.Y < r.Y || ev.X >= r.X+r.W || ev.Y >= r.Y+r.H
		if outside && ev.Kind != ui.InputPointerUp && ev.Kind != ui.InputPointerMove {
			return false
		}
		in.X = math.Max(0, math.Min(s.width-1, float64(ev.X-r.X)/float64(r.W)*s.width))
		in.Y = math.Max(0, math.Min(s.height-1, float64(ev.Y-r.Y)/float64(r.H)*s.height))
		in.Button, in.Buttons = "left", 1
		if ev.Button == 1 {
			in.Button, in.Buttons = "right", 2
		} else if ev.Button == 2 {
			in.Button, in.Buttons = "middle", 4
		}
		switch ev.Kind {
		case ui.InputPointerDown:
			in.Kind, in.Clicks = "mousePressed", ev.Clicks
			s.caret = ui.Rect{X: ev.X, Y: ev.Y, W: 1, H: 20}
		case ui.InputPointerUp:
			in.Kind, in.Clicks, in.Buttons = "mouseReleased", ev.Clicks, 0
		case ui.InputPointerMove:
			in.Kind = "mouseMoved"
			if ev.Button < 0 {
				in.Button, in.Buttons = "none", 0
			}
		case ui.InputScroll:
			in.Kind, in.Button, in.Buttons = "mouseWheel", "none", 0
			in.DX, in.DY = float64(ev.DX)*s.width/float64(r.W), float64(ev.DY)*s.height/float64(r.H)
		}
	case ui.InputText:
		in.Kind, in.Text = "text", ev.Text
		s.compose = ""
	case ui.InputCompose:
		s.compose = ev.Text
		return true
	case ui.InputCommand:
		in.Kind = ev.Text
		if ev.Text == "paste" || ev.Text == "pasteAndMatchStyle" {
			in.Kind, in.Text = "text", c.ReadClipboard()
		} else if ev.Text != "copy" && ev.Text != "cut" && ev.Text != "selectAll" && ev.Text != "undo" && ev.Text != "redo" {
			return false
		}
	case ui.InputKeyDown, ui.InputKeyUp:
		if ev.Key == ui.KeyEscape {
			return false
		}
		if ev.Key == ui.KeyV && ev.Mods&ui.Cmd != 0 {
			if ev.Kind == ui.InputKeyDown {
				// 在本机同步读取粘贴内容，再发给后台网页，避免剪贴板已恢复后才处理快捷键。
				return a.browserDrawerSend(s, cdp.PageInput{Kind: "text", Text: c.ReadClipboard()})
			}
			return true
		}
		in.Key, in.VirtualKey = browserKey(ev.Key)
		if in.Key == "" {
			return false
		}
		// 让系统输入法处理可打印按键，文字由后续 InputText 统一提交。
		if ev.Mods&(ui.Ctrl|ui.Super|ui.Alt) == 0 && (len(in.Key) == 1 || ev.Key == ui.KeySpace) {
			return false
		}
		in.Kind = "keyDown"
		if ev.Kind == ui.InputKeyUp {
			in.Kind = "keyUp"
		}
	default:
		return false
	}
	return a.browserDrawerSend(s, in)
}

func browserModifiers(mods ui.Modifiers) int64 {
	var out int64
	for _, pair := range []struct {
		ui  ui.Modifiers
		cdp int64
	}{{ui.Alt, 1}, {ui.Ctrl, 2}, {ui.Super, 4}, {ui.Shift, 8}} {
		if mods&pair.ui != 0 {
			out |= pair.cdp
		}
	}
	return out
}

func browserKey(key ui.Key) (string, int) {
	if key >= ui.KeyA && key <= ui.KeyZ {
		return strings.ToLower(string(rune('A' + key - ui.KeyA))), int('A' + key - ui.KeyA)
	}
	if key >= ui.Key0 && key <= ui.Key9 {
		return string(rune('0' + key - ui.Key0)), int('0' + key - ui.Key0)
	}
	keys := map[ui.Key]struct {
		name string
		code int
	}{
		ui.KeyEnter: {"Enter", 13}, ui.KeyBackspace: {"Backspace", 8}, ui.KeyTab: {"Tab", 9},
		ui.KeyDelete: {"Delete", 46}, ui.KeyHome: {"Home", 36}, ui.KeyEnd: {"End", 35},
		ui.KeyLeft: {"ArrowLeft", 37}, ui.KeyUp: {"ArrowUp", 38}, ui.KeyRight: {"ArrowRight", 39}, ui.KeyDown: {"ArrowDown", 40},
		ui.KeyPageUp: {"PageUp", 33}, ui.KeyPageDown: {"PageDown", 34}, ui.KeySpace: {" ", 32},
	}
	entry := keys[key]
	return entry.name, entry.code
}

func (a *App) laneBrowserButton(c *ui.Context, ln tasks.AccountLane) {
	if ln.Extension || (ln.Platform != "豆包" && ln.Platform != "千问") {
		return
	}
	ui.Button(c.Key("view-browser"), "查看").FontSize(a.look.caption).Padding(2, 7).
		Label("查看浏览器 " + ln.Name).Tooltip("查看浏览器").OnClick(func() {
		if a.store == nil {
			return
		}
		for _, row := range a.store.Browsers() {
			if row.ID == ln.ID {
				a.openBrowserDrawer(row)
				return
			}
		}
	})
}
