package ui

import (
	"context"
	"errors"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

func (a *App) wireHealing() {
	if a == nil || a.engine == nil {
		return
	}
	a.engine.SetLoginProbe(a.probeAccount)
	a.engine.SetSlider(a.solveSlider)
	a.engine.SetHealNotify(func() {
		a.emit("workbench", "browsers")
	})
}

func (a *App) probeAccount(ctx context.Context, b store.Browser) (string, error) {
	if b.ExtAccount != "" || !accountWindowOpen(b) {
		return "unknown", nil
	}
	return cdp.ProbeLogin(ctx, browser.AccountEndpoint(b))
}

// ensureAccountPage 在应用内打开后台账号，或显示用户手动启动的登录窗口。
func (a *App) ensureAccountPage(ctx context.Context, b store.Browser) error {
	if a != nil && a.accountOpen != nil {
		err := a.accountOpen(b)
		if err == nil {
			a.watchLogin(b, loginEndpoint(b))
		}
		return err
	}
	if a == nil || b.ExtAccount != "" || a.manager == nil {
		return nil
	}
	if a.manager.IsHeadless(b.ID) && !a.browserDrawerBusy(b.ID) {
		// 无头页面里豆包验证码图片可能显示不出来、刷新也不变；登录和验证改在有窗口的浏览器里做。
		// 资料目录不变，登录状态保留；下一批任务会照常切回后台浏览器。
		a.stopHeadlessForWindow(b.ID)
	} else {
		a.mu.Lock()
		viewing := a.browserViewingID == b.ID
		a.mu.Unlock()
		if viewing {
			return nil
		}
	}
	page := accountHomeURL(b)
	ws, err := a.manager.Start(b, a.dataRoot, a.browserBackend, page, false)
	if errors.Is(err, browser.ErrDebugPort) {
		a.watchLogin(b, loginEndpoint(b))
		return nil
	}
	if err != nil {
		logging.Log.Errorf("%s 打开登录页失败：%v", accountLabel(b), err)
		return err
	}
	if a.manager.IsHeadless(b.ID) {
		a.postUI(func() { a.openBrowserDrawer(b) })
		return nil
	}
	if ws != "" {
		// 去处理要马上把已经开着的窗口拿到前台，不要等后面的导航重试。
		showCtx, showCancel := context.WithTimeout(ctx, 4*time.Second)
		_ = cdp.BringToFront(showCtx, ws)
		showCancel()
	}
	if page == "" || ws == "" {
		a.watchLogin(b, ws)
		return nil
	}
	if navErr := cdp.OpenURLContext(ctx, ws, page); navErr != nil {
		if errors.Is(navErr, cdp.ErrDebugUnavailable) {
			logging.Log.Warning("%s 调试端口还没连上，已打开登录页，请在网页里登录", accountLabel(b))
			a.watchLogin(b, ws)
			return nil
		}
		logging.Log.Errorf("%s 打开登录页失败：%v", accountLabel(b), navErr)
		return navErr
	}
	a.watchLogin(b, ws)
	return nil
}

// stopHeadlessForWindow 先收起正在看这个账号的面板，再关掉后台浏览器，避免面板报「连接已断开」。
func (a *App) stopHeadlessForWindow(id int64) {
	closed := make(chan (<-chan struct{}), 1)
	a.postUI(func() {
		if s := a.browserDrawer; s != nil && s.row.ID == id {
			a.closeBrowserDrawer()
			closed <- s.done
			return
		}
		closed <- nil
	})
	select {
	case done := <-closed:
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
			}
		}
	case <-time.After(3 * time.Second):
	}
	a.manager.Stop(id)
}

func accountLabel(b store.Browser) string {
	if b.Name != "" {
		return b.Name
	}
	if b.Platform != "" {
		return b.Platform
	}
	return "账号"
}

func (a *App) solveSlider(ctx context.Context, b store.Browser) error {
	if b.Engine == browser.KindChromeCDP {
		return captcha.ErrVerificationRequired
	}
	if b.Platform != "千问" || b.Port <= 0 {
		return errors.New("没有滑块处理")
	}
	return captcha.SolveQianwenSliderAt(ctx, camoufox.Endpoint(b.Port))
}

func accountWindowOpen(b store.Browser) bool {
	if b.Port <= 0 {
		return false
	}
	_, err := camoufox.Find(browser.AccountEndpoint(b))
	return err == nil
}
