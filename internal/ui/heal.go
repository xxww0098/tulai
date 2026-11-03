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
		if a.window == nil {
			return
		}
		a.window.Update(func() {})
	})
}

func (a *App) probeAccount(ctx context.Context, b store.Browser) (string, error) {
	if b.ExtAccount != "" || b.Port <= 0 || !debugPortOpen(b.Port) {
		return "unknown", nil
	}
	return cdp.ProbeLogin(ctx, camoufox.Endpoint(b.Port))
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
	a.mu.Lock()
	viewing := a.browserViewingID == b.ID
	a.mu.Unlock()
	if viewing {
		return nil
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
	if b.Platform != "千问" || b.Port <= 0 {
		return errors.New("没有滑块处理")
	}
	return captcha.SolveQianwenSliderAt(ctx, camoufox.Endpoint(b.Port))
}

func debugPortOpen(port int) bool {
	_, err := camoufox.Find(camoufox.Endpoint(port))
	return err == nil
}
