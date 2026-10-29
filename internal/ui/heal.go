package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
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
	a.engine.SetLoginFocus(a.focusAccount)
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
	return cdp.ProbeLogin(ctx, fmt.Sprintf("http://127.0.0.1:%d", b.Port))
}

func (a *App) focusAccount(ctx context.Context, b store.Browser) error {
	return a.ensureAccountPage(ctx, b)
}

// ensureAccountPage 打开这个账号的登录页。调试端口已经在听时复用窗口；
// 页面还停在 about:blank 或欢迎页时导航过去。已经在站点上则只把窗口拉到前面。
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
	return captcha.SolveQianwenSliderAt(ctx, fmt.Sprintf("http://127.0.0.1:%d", b.Port))
}

func debugPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
