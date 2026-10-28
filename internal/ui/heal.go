package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
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
	if b.ExtAccount != "" || b.Port <= 0 {
		return nil
	}
	return cdp.BringToFront(ctx, fmt.Sprintf("http://127.0.0.1:%d", b.Port))
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
