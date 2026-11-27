package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

type doubaoImageProbeStatus string

const (
	doubaoProbeOK        doubaoImageProbeStatus = "ok"
	doubaoProbeDegraded  doubaoImageProbeStatus = "degraded"
	doubaoProbeLoggedOut doubaoImageProbeStatus = "logged_out"
	doubaoProbeQuota     doubaoImageProbeStatus = "quota"
	doubaoProbeRate      doubaoImageProbeStatus = "rate"
	doubaoProbeCaptcha   doubaoImageProbeStatus = "captcha"
	doubaoProbeFailed    doubaoImageProbeStatus = "failed"
)

type doubaoImageProbeOutcome struct {
	Status  doubaoImageProbeStatus
	Message string
	Images  int
	Route   string
}

const (
	doubaoImageProbePrompt = "画一只坐在木质窗台上的橘猫，简约水彩插画风格"
	doubaoImageProbeWait   = 75 * time.Second
	doubaoImageProbeGap    = 1500 * time.Millisecond
)

func nextLocalMidnightUI(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
}

// probeDoubaoImageCapability 手动触发豆包账号生图能力检测（独立于日常自动同步）。
// 逐个账号在豆包「图像生成」模式下发送 1 条极简文生图请求，实测服务端是否正常路由至 Agent-Creation，
// 或被静默降级至纯文本 flow.agent.shell / 发消息被踢登出，并在检测后尽量清理临时测试对话。
func (a *App) probeDoubaoImageCapability(ids []int64) error {
	if a.running {
		return errors.New("任务运行中，请先停止任务后再检测生图能力")
	}
	if a.probeImageCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "probing_image") {
		return errors.New("正在检测豆包生图能力，请等待完成或先停止")
	}
	if a.syncQwenCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing_status") {
		return errors.New("正在同步账号状态，请等待完成后再检测生图能力")
	}
	if a.syncNamesCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing") {
		return errors.New("正在同步账号用户名，请等待完成后再检测生图能力")
	}
	if a.clearHistoryCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "clearing") {
		return errors.New("正在清理聊天历史，请等待完成后再检测生图能力")
	}

	var requested []store.Browser
	seen := map[int64]bool{}
	if len(ids) > 0 {
		for _, id := range ids {
			if id <= 0 || seen[id] {
				continue
			}
			if row, ok := a.browserByID(id); ok && row.Platform == "豆包" {
				seen[id] = true
				requested = append(requested, row)
			}
		}
	} else {
		for _, row := range a.browserRows {
			if row.Platform == "豆包" && !seen[row.ID] {
				seen[row.ID] = true
				requested = append(requested, row)
			}
		}
	}
	if len(requested) == 0 {
		return errors.New("当前没有可检测生图能力的豆包账号")
	}
	var targets []store.Browser
	for _, row := range requested {
		if a.clearHistoryAllowed(row) {
			targets = append(targets, row)
		}
	}
	if len(targets) == 0 {
		return errors.New("所选豆包账号当前正在操作中，请稍后再试")
	}
	if a.probeDoubaoImageFn == nil && a.manager == nil {
		return errors.New("没有受管浏览器管理器，无法检测豆包生图能力")
	}
	if a.browserPending == nil {
		a.browserPending = map[int64]browserOperation{}
	}

	batch := &browserBatchRun{
		kind:    "probing_image",
		total:   len(targets),
		skipped: len(requested) - len(targets),
	}
	for _, row := range targets {
		a.browserPending[row.ID] = browserOperation{kind: "probing_image", batch: batch}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a.browserBatch = batch
	a.probeImageCancel = cancel
	a.emit("browsers", "workbench")

	go func() {
		defer func() {
			cancel()
			a.postUI(func() {
				if a.browserBatch == batch {
					a.browserBatch = nil
				}
				a.probeImageCancel = nil
				for _, row := range targets {
					if op, ok := a.browserPending[row.ID]; ok && op.batch == batch {
						delete(a.browserPending, row.ID)
					}
				}
				a.refreshBrowsers()
				a.emit("browsers", "workbench", "shell")
			})
		}()

		var totalOK atomic.Int64
		var totalDegraded atomic.Int64
		var totalFailed atomic.Int64
		var singleMu sync.Mutex
		var singleOutcome doubaoImageProbeOutcome
		var singleName string

		for i, row := range targets {
			if runCtx.Err() != nil {
				a.postUI(func() {
					a.finishBrowserOperation(row.ID, context.Canceled)
					a.emit("browsers", "workbench")
				})
				continue
			}
			if i > 0 && a.probeDoubaoImageFn == nil {
				timer := time.NewTimer(doubaoImageProbeGap)
				select {
				case <-runCtx.Done():
					timer.Stop()
					a.postUI(func() {
						a.finishBrowserOperation(row.ID, context.Canceled)
						a.emit("browsers", "workbench")
					})
					continue
				case <-timer.C:
				}
			}

			name := accountLabel(row)
			label := fmt.Sprintf("%s/%s", row.Platform, name)
			a.postUI(func() {
				batch.current = label
				a.emit("browsers", "workbench")
			})

			outcome := a.probeOneDoubaoImageCapability(runCtx, row)
			if runCtx.Err() != nil {
				logging.Log.Info("账号[%s] 生图能力检测已停止", label)
				a.postUI(func() {
					a.finishBrowserOperation(row.ID, context.Canceled)
					a.emit("browsers", "workbench")
				})
				continue
			}

			singleMu.Lock()
			singleOutcome = outcome
			singleName = name
			singleMu.Unlock()

			now := time.Now()
			switch outcome.Status {
			case doubaoProbeOK:
				totalOK.Add(1)
				a.syncUI(func() {
					if a.store != nil {
						a.store.ResetBrowserHealth(row.ID)
					}
					if cur, ok := a.browserByID(row.ID); ok && (!cur.IsLoggedIn || accountNeedsLogin(cur)) {
						a.confirmLogin(row.ID, false)
					}
					batch.updated++
					a.finishBrowserOperation(row.ID, nil)
					a.emit("browsers", "workbench", "shell")
				})
				logging.Log.Success("账号[%s] 生图能力正常（测试出图 %d 张）", label, max(outcome.Images, 1))

			case doubaoProbeDegraded:
				totalDegraded.Add(1)
				msg := strings.TrimSpace(outcome.Message)
				if msg == "" {
					msg = strategy.ErrAccountImageDegraded.Error()
				}
				until := nextLocalMidnightUI(now).Unix()
				a.syncUI(func() {
					if a.store != nil {
						a.store.UpdateBrowserHealth(row.ID, store.HealthExhausted, until, msg)
					}
					batch.degraded++
					a.finishBrowserOperation(row.ID, nil)
					a.emit("browsers", "workbench", "shell")
				})
				logging.Log.Warning("账号[%s] 生图能力受限（已降级纯文本），当日暂停使用：%s", label, msg)

			case doubaoProbeLoggedOut:
				totalFailed.Add(1)
				msg := strings.TrimSpace(outcome.Message)
				if msg == "" {
					msg = "未登录"
				}
				a.syncUI(func() {
					if a.store != nil {
						a.store.UpdateBrowserHealth(row.ID, store.HealthExhausted, 0, msg)
						a.store.UpdateBrowser(row.ID, row.IsRunning, false)
					}
					a.finishBrowserOperation(row.ID, errors.New(msg))
					a.emit("browsers", "workbench", "shell")
				})
				logging.Log.Warning("账号[%s] 生图能力检测发现登录态失效：%s", label, msg)

			case doubaoProbeQuota:
				totalFailed.Add(1)
				msg := strings.TrimSpace(outcome.Message)
				if msg == "" {
					msg = strategy.ErrAccountQuota.Error()
				}
				until := nextLocalMidnightUI(now).Unix()
				a.syncUI(func() {
					if a.store != nil {
						a.store.UpdateBrowserHealth(row.ID, store.HealthExhausted, until, msg)
						a.store.MarkBrowserPlatformLimited(row.ID, msg, until, now)
					}
					a.finishBrowserOperation(row.ID, errors.New(msg))
					a.emit("browsers", "workbench", "shell")
				})
				logging.Log.Warning("账号[%s] 生图能力检测触发额度上限：%s", label, msg)

			case doubaoProbeRate:
				totalFailed.Add(1)
				msg := strings.TrimSpace(outcome.Message)
				if msg == "" {
					msg = strategy.ErrAccountRate.Error()
				}
				until := now.Add(15 * time.Minute).Unix()
				a.syncUI(func() {
					if a.store != nil {
						a.store.UpdateBrowserHealth(row.ID, store.HealthCooling, until, msg)
						a.store.MarkBrowserPlatformLimited(row.ID, msg, until, now)
					}
					a.finishBrowserOperation(row.ID, errors.New(msg))
					a.emit("browsers", "workbench", "shell")
				})
				logging.Log.Warning("账号[%s] 生图能力检测触发频控冷却：%s", label, msg)

			case doubaoProbeCaptcha:
				totalFailed.Add(1)
				msg := strings.TrimSpace(outcome.Message)
				if msg == "" {
					msg = "需要安全验证"
				}
				until := now.Add(10 * time.Minute).Unix()
				a.syncUI(func() {
					if a.store != nil {
						a.store.UpdateBrowserHealth(row.ID, store.HealthCooling, until, msg)
					}
					a.finishBrowserOperation(row.ID, errors.New(msg))
					a.emit("browsers", "workbench", "shell")
				})
				logging.Log.Warning("账号[%s] 生图能力检测触发安全验证：%s", label, msg)

			default:
				totalFailed.Add(1)
				msg := strings.TrimSpace(outcome.Message)
				if msg == "" {
					msg = "检测失败，请稍后重试"
				}
				a.syncUI(func() {
					a.finishBrowserOperation(row.ID, errors.New(msg))
					a.emit("browsers", "workbench", "shell")
				})
				logging.Log.Warning("账号[%s] 生图能力检测异常：%s", label, msg)
			}
		}

		if runCtx.Err() == nil {
			okN := totalOK.Load()
			degN := totalDegraded.Load()
			failN := totalFailed.Load()
			logging.Log.Info("豆包生图能力检测结束：共 %d 个，正常 %d · 受限 %d · 异常 %d", len(targets), okN, degN, failN)
			singleMu.Lock()
			so := singleOutcome
			sn := singleName
			singleMu.Unlock()
			a.postUI(func() {
				if len(targets) == 1 {
					switch so.Status {
					case doubaoProbeOK:
						a.pushToast("success", fmt.Sprintf("「%s」生图能力正常（已测试出图并清理临时对话）", sn))
					case doubaoProbeDegraded:
						a.pushToast("warning", fmt.Sprintf("「%s」生图能力受限（已降级纯文本），已标记停用", sn))
					case doubaoProbeLoggedOut:
						a.pushToast("error", fmt.Sprintf("「%s」%s", sn, so.Message))
					case doubaoProbeQuota:
						a.pushToast("warning", fmt.Sprintf("「%s」今日生图额度已用尽", sn))
					case doubaoProbeRate:
						a.pushToast("warning", fmt.Sprintf("「%s」请求过于频繁，已进入冷却", sn))
					case doubaoProbeCaptcha:
						a.pushToast("warning", fmt.Sprintf("「%s」触发安全验证，请打开账号处理", sn))
					default:
						a.pushToast("error", fmt.Sprintf("「%s」生图能力检测失败：%s", sn, so.Message))
					}
					return
				}
				summary := fmt.Sprintf("豆包生图能力检测完成：正常 %d · 受限 %d · 异常 %d", okN, degN, failN)
				switch {
				case degN > 0 || failN > 0:
					if okN == 0 && degN == 0 {
						a.pushToast("error", summary)
					} else {
						a.pushToast("warning", summary)
					}
				default:
					a.pushToast("success", summary)
				}
			})
		}
	}()
	return nil
}

func (a *App) stopProbeDoubaoImageCapability() {
	if a.probeImageCancel != nil {
		a.probeImageCancel()
		logging.Log.Info("已请求停止豆包生图能力检测")
	}
}

func (a *App) probeOneDoubaoImageCapability(ctx context.Context, b store.Browser) doubaoImageProbeOutcome {
	if a.probeDoubaoImageFn != nil {
		return a.probeDoubaoImageFn(ctx, b)
	}
	if a.manager == nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "受管浏览器管理器未初始化"}
	}
	doubao := &strategy.Doubao{}
	a.pushProxy()
	wasRunning := a.manager.IsRunning(b.ID)
	ws, err := a.manager.StartView(ctx, b, a.dataRoot, doubao.HomeURL())
	if err != nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: fmt.Sprintf("打开受管浏览器失败: %v", err)}
	}
	if !wasRunning {
		defer func() {
			a.manager.Stop(b.ID)
			if a.store != nil {
				a.store.SetRunning(b.ID, a.manager.IsRunning(b.ID))
			}
		}()
	}

	// 发送前先快速确认登录态/地区/验证码，避免未登录账号无意义发消息。
	if pre := checkDoubaoProbeLogin(ctx, ws, 15*time.Second); pre != nil {
		return *pre
	}

	client, err := cdp.NewClient(ws)
	if err != nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: fmt.Sprintf("连接受管浏览器失败: %v", err)}
	}
	defer client.Close()

	sess, err := client.NewBackgroundSession(doubao.HomeURL(), nil)
	if err != nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: fmt.Sprintf("打开豆包页面失败: %v", err)}
	}
	stopWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			sess.Close()
		case <-stopWatch:
		}
	}()
	defer func() {
		close(stopWatch)
		sess.Close()
	}()

	if err := doubao.InstallHook(sess); err != nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: fmt.Sprintf("注入检测脚本失败: %v", err)}
	}
	if err := strategy.FreshChat(sess); err != nil {
		return classifyDoubaoProbeError(ctx, ws, sess, err)
	}
	if err := doubao.CleanComposer(sess); err != nil {
		return classifyDoubaoProbeError(ctx, ws, sess, err)
	}
	if err := doubao.MarkTask(sess); err != nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: fmt.Sprintf("初始化检测会话失败: %v", err)}
	}
	if err := doubao.SendPrompt(sess, doubaoImageProbePrompt); err != nil {
		// 稍停片刻核查是否像强踢登出一样在发送瞬间被重定向至 from_logout=1。
		if kicked := detectDoubaoPostSendLogout(ctx, ws, sess, 2*time.Second); kicked != nil {
			return *kicked
		}
		return classifyDoubaoProbeError(ctx, ws, sess, err)
	}

	// 检测结束前尽力删除本次临时探测对话，避免在账号侧栏留垃圾会话。
	loggedOut := false
	defer func() {
		if loggedOut || ctx.Err() != nil {
			return
		}
		convID := strategy.CurrentConversationID(sess)
		if convID == "" || strings.HasPrefix(strings.ToLower(convID), "local_") {
			return
		}
		delCtx, delCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer delCancel()
		if delErr := doubao.DeleteConversation(delCtx, sess, convID); delErr != nil {
			logging.Log.Debugf("清理豆包生图检测临时对话 %s 失败: %v", convID, delErr)
		}
	}()

	// 复查发送后 2.5s 内是否触发服务端强制踢登出（?from_logout=1）。
	if kicked := detectDoubaoPostSendLogout(ctx, ws, sess, 2500*time.Millisecond); kicked != nil {
		loggedOut = true
		return *kicked
	}

	results, waitErr := doubao.WaitForResult(sess, doubaoImageProbeWait)
	if waitErr == nil && len(results) > 0 {
		return doubaoImageProbeOutcome{
			Status:  doubaoProbeOK,
			Message: "生图能力正常",
			Images:  len(results),
			Route:   "flow.agent.creation",
		}
	}
	outcome := classifyDoubaoProbeError(ctx, ws, sess, waitErr)
	if outcome.Status == doubaoProbeLoggedOut {
		loggedOut = true
	}
	return outcome
}

func checkDoubaoProbeLogin(ctx context.Context, ws string, wait time.Duration) *doubaoImageProbeOutcome {
	deadline := time.Now().Add(wait)
	outCount := 0
	for {
		if ctx.Err() != nil {
			return &doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "已停止"}
		}
		probeCtx, cancel := context.WithTimeout(ctx, min(5*time.Second, max(time.Second, time.Until(deadline))))
		state, err := cdp.ProbeLogin(probeCtx, ws)
		cancel()
		if err == nil {
			switch cdp.NormalizeLoginState(state) {
			case "in":
				return nil
			case "out":
				outCount++
				if outCount >= 2 {
					return &doubaoImageProbeOutcome{Status: doubaoProbeLoggedOut, Message: "账号未登录，请先打开登录"}
				}
			case cdp.RegionBlocked:
				return &doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "当前地区受限，请检查代理设置"}
			case "challenge":
				return &doubaoImageProbeOutcome{Status: doubaoProbeCaptcha, Message: "触发安全验证，请先打开账号处理验证"}
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return &doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "检查豆包登录态超时，请稍后重试"}
		}
		timer := time.NewTimer(min(400*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return &doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "已停止"}
		case <-timer.C:
		}
	}
}

func detectDoubaoPostSendLogout(ctx context.Context, ws string, sess *cdp.Session, delay time.Duration) *doubaoImageProbeOutcome {
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "已停止"}
		case <-timer.C:
		}
	}
	if sess != nil {
		if href, err := sess.Evaluate(`String(location.href || '')`); err == nil && strings.Contains(strings.ToLower(href), "from_logout=1") {
			return &doubaoImageProbeOutcome{Status: doubaoProbeLoggedOut, Message: "发送消息后登录态失效（被服务端退出登录），请重新登录"}
		}
	}
	if ws != "" {
		probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		state, err := cdp.ProbeLogin(probeCtx, ws)
		cancel()
		if err == nil && cdp.NormalizeLoginState(state) == "out" {
			return &doubaoImageProbeOutcome{Status: doubaoProbeLoggedOut, Message: "发送消息后登录态失效（被服务端退出登录），请重新登录"}
		}
	}
	return nil
}

func classifyDoubaoProbeError(ctx context.Context, ws string, sess *cdp.Session, err error) doubaoImageProbeOutcome {
	if ctx.Err() != nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "已停止"}
	}
	if errors.Is(err, strategy.ErrAccountImageDegraded) {
		return doubaoImageProbeOutcome{Status: doubaoProbeDegraded, Message: err.Error(), Route: "flow.agent.shell"}
	}
	if errors.Is(err, strategy.ErrAccountQuota) {
		return doubaoImageProbeOutcome{Status: doubaoProbeQuota, Message: err.Error()}
	}
	if errors.Is(err, strategy.ErrAccountRate) {
		return doubaoImageProbeOutcome{Status: doubaoProbeRate, Message: err.Error()}
	}
	if errors.Is(err, captcha.ErrVisible) {
		return doubaoImageProbeOutcome{Status: doubaoProbeCaptcha, Message: "触发安全验证，请先打开账号处理验证"}
	}
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "E_NOT_LOGGED_IN") || strings.Contains(msg, "未登录") || strings.Contains(msg, "登录已失效") {
			return doubaoImageProbeOutcome{Status: doubaoProbeLoggedOut, Message: "账号未登录或登录已失效，请重新登录"}
		}
		if strings.Contains(msg, "flow.agent.shell") || strings.Contains(msg, "生图能力受限") || strings.Contains(msg, "已降级纯文本") {
			return doubaoImageProbeOutcome{Status: doubaoProbeDegraded, Message: msg, Route: "flow.agent.shell"}
		}
	}
	if kicked := detectDoubaoPostSendLogout(ctx, ws, sess, 0); kicked != nil {
		return *kicked
	}
	if err == nil {
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "检测未返回测试图片"}
	}
	return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: err.Error()}
}
