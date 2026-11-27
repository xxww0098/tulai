package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

const outputCapsLiveProbeCooldown = 15 * time.Minute

func (a *App) probeOutputCaps(ctx context.Context, platform, endpoint string) (store.ProviderOutputCaps, error) {
	a.mu.Lock()
	fn := a.outputCapsProbe
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, platform, endpoint)
	}
	return cdp.ProbeOutputCaps(ctx, platform, endpoint)
}

func (a *App) applyOutputCaps(platform string, raw store.ProviderOutputCaps) (store.ProviderOutputCaps, bool) {
	if a == nil || !store.OutputCapsSupportedPlatform(platform) {
		return store.ProviderOutputCaps{}, false
	}
	if raw.SyncedAt <= 0 {
		raw.SyncedAt = time.Now().Unix()
	}
	caps, ok := store.NormalizeProviderOutputCaps(platform, raw)
	if !ok {
		return store.ProviderOutputCaps{}, false
	}
	if a.outputCaps == nil {
		a.outputCaps = map[string]store.ProviderOutputCaps{}
	}
	a.outputCaps[platform] = caps
	if a.store != nil {
		if err := a.store.SaveOutputCaps(platform, caps); err != nil {
			logging.Log.Warning("保存%s网页输出规格缓存失败: %v", platform, err)
		}
	}
	prev := *a.outputPrefsPtr(platform)
	cur := a.outputPrefsFor(platform)
	if *cur != prev {
		savePlatformOutputPrefs(a.store, platform, *cur, caps)
	}
	a.emit("workbench")
	return caps, true
}

func (a *App) outputCapsStale(platform string, now time.Time) bool {
	caps := a.outputCapsFor(platform)
	if caps.SyncedAt <= 0 {
		return true
	}
	return now.Sub(time.Unix(caps.SyncedAt, 0)) >= outputCapsLiveProbeCooldown
}

func (a *App) pickOutputCapsProbeAccount(platform string) (store.Browser, bool) {
	rows := a.configuredBrowsers()
	var runningLoggedIn, runningAny, loggedInEnabled, anyEnabled *store.Browser
	for i := range rows {
		row := &rows[i]
		if row.Platform != platform {
			continue
		}
		isRunning := a.manager != nil && a.manager.IsRunning(row.ID)
		if isRunning && row.IsLoggedIn && runningLoggedIn == nil {
			runningLoggedIn = row
		}
		if isRunning && runningAny == nil {
			runningAny = row
		}
		if !row.Disabled && row.IsLoggedIn && loggedInEnabled == nil {
			loggedInEnabled = row
		}
		if !row.Disabled && anyEnabled == nil {
			anyEnabled = row
		}
	}
	switch {
	case runningLoggedIn != nil:
		return *runningLoggedIn, true
	case runningAny != nil:
		return *runningAny, true
	case loggedInEnabled != nil:
		return *loggedInEnabled, true
	case anyEnabled != nil:
		return *anyEnabled, true
	default:
		return store.Browser{}, false
	}
}

func formatOutputCapsSummary(caps store.ProviderOutputCaps) string {
	parts := make([]string, 0, 3)
	if caps.SupportsAuto && caps.AutoLabel != "" {
		parts = append(parts, fmt.Sprintf("%s + %d 种比例", caps.AutoLabel, len(caps.Ratios)))
	} else {
		parts = append(parts, fmt.Sprintf("%d 种比例", len(caps.Ratios)))
	}
	if len(caps.Resolutions) > 0 {
		parts = append(parts, fmt.Sprintf("%d 档分辨率（%s）", len(caps.Resolutions), strings.Join(caps.Resolutions, "/")))
	}
	return strings.Join(parts, " · ")
}

// syncOutputCaps 从当前已登录或运行中的供应商网页实时读取输出比例与分辨率并更新缓存。
func (a *App) syncOutputCaps(platform string) error {
	if a == nil {
		return nil
	}
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = a.selectedPlatform()
	}
	if !store.OutputCapsSupportedPlatform(platform) {
		return fmt.Errorf("不支持同步%s的网页输出规格", platform)
	}
	if a.running {
		return errors.New("任务运行中，请在任务完成后同步网页输出规格")
	}
	if a.outputCapsSyncing == nil {
		a.outputCapsSyncing = map[string]bool{}
	}
	if a.outputCapsSyncing[platform] {
		return nil
	}
	account, ok := a.pickOutputCapsProbeAccount(platform)
	if !ok {
		msg := fmt.Sprintf("请先在「浏览器」页添加并登录一个%s账号后再同步网页输出规格", platform)
		a.pushToast("warning", msg)
		return errors.New(msg)
	}
	if a.syncOutputCapsFn == nil && a.manager == nil {
		return errors.New("没有受管浏览器管理器，无法同步网页输出规格")
	}

	a.outputCapsSyncing[platform] = true
	a.emit("workbench")

	go func(plat string, row store.Browser) {
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()

		var caps store.ProviderOutputCaps
		var err error
		if a.syncOutputCapsFn != nil {
			caps, err = a.syncOutputCapsFn(ctx, plat, row)
		} else {
			caps, err = a.syncOneOutputCaps(ctx, plat, row)
		}

		a.postUI(func() {
			delete(a.outputCapsSyncing, plat)
			if err != nil {
				logging.Log.Warning("同步%s网页输出规格失败：%v", plat, err)
				a.pushToast("error", fmt.Sprintf("同步%s网页输出规格失败：%v", plat, err))
				a.emit("workbench")
				return
			}
			applied, ok := a.applyOutputCaps(plat, caps)
			if !ok {
				a.pushToast("error", fmt.Sprintf("未能从%s网页读取到有效输出规格", plat))
				a.emit("workbench")
				return
			}
			summary := formatOutputCapsSummary(applied)
			logging.Log.Success("已同步%s网页输出规格：%s", plat, summary)
			a.pushToast("success", fmt.Sprintf("已同步%s网页输出规格：%s", plat, summary))
		})
	}(platform, account)
	return nil
}

func (a *App) syncOneOutputCaps(ctx context.Context, platform string, row store.Browser) (store.ProviderOutputCaps, error) {
	if a.manager != nil && a.manager.IsRunning(row.ID) {
		if ep := loginEndpoint(row); ep != "" {
			if caps, err := a.probeOutputCaps(ctx, platform, ep); err == nil {
				return caps, nil
			}
		}
	}
	ws, _, cleanup, err := a.openStatusProbeBrowser(ctx, row)
	if err != nil {
		return store.ProviderOutputCaps{}, err
	}
	defer cleanup()
	return a.pollOutputCaps(ctx, platform, ws)
}

func (a *App) pollOutputCaps(ctx context.Context, platform, endpoint string) (store.ProviderOutputCaps, error) {
	deadline := time.Now().Add(25 * time.Second)
	outCount := 0
	var lastErr error
	for {
		if ctx.Err() != nil {
			return store.ProviderOutputCaps{}, ctx.Err()
		}
		loginCtx, loginCancel := context.WithTimeout(ctx, min(6*time.Second, max(time.Second, time.Until(deadline))))
		state, err := cdp.ProbeLogin(loginCtx, endpoint)
		loginCancel()
		if err == nil {
			state = cdp.NormalizeLoginState(state)
			switch state {
			case "out":
				outCount++
				if outCount >= 2 {
					return store.ProviderOutputCaps{}, errors.New("账号未登录，请先打开登录")
				}
			case cdp.RegionBlocked:
				return store.ProviderOutputCaps{}, errors.New("当前地区受限，请检查代理设置")
			case "challenge":
				return store.ProviderOutputCaps{}, errors.New("触发安全验证，请先打开账号处理验证")
			case "in", "unknown":
				if state == "in" {
					outCount = 0
				}
				probeCtx, probeCancel := context.WithTimeout(ctx, min(10*time.Second, max(time.Second, time.Until(deadline))))
				caps, pErr := a.probeOutputCaps(probeCtx, platform, endpoint)
				probeCancel()
				if pErr == nil {
					return caps, nil
				}
				lastErr = pErr
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if lastErr != nil {
				return store.ProviderOutputCaps{}, lastErr
			}
			return store.ProviderOutputCaps{}, fmt.Errorf("读取%s网页输出规格超时，请稍后重试", platform)
		}
		timer := time.NewTimer(min(500*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return store.ProviderOutputCaps{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *App) piggybackOutputCaps(ctx context.Context, platform, endpoint string) {
	if a == nil || endpoint == "" || !store.OutputCapsSupportedPlatform(platform) {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	caps, err := a.probeOutputCaps(probeCtx, platform, endpoint)
	if err != nil {
		return
	}
	a.postUI(func() {
		a.applyOutputCaps(platform, caps)
	})
}
