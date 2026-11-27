package strategy

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
)

//go:embed js/qwen_history.js
var qwenHistoryJS string

//go:embed js/qwen_intl_history.js
var qwenIntlHistoryJS string

//go:embed js/gemini_history.js
var geminiHistoryJS string

//go:embed js/history_fast.js
var historyFastJS string

const (
	defaultMaxHistoryDelete = 200
	defaultHistoryDelayMin  = 400 * time.Millisecond
	defaultHistoryDelayMax  = 900 * time.Millisecond
	maxConsecutiveDelFails  = 3
	fastWaveSize            = 6
	fastWaveConcurrency     = 3
	fastWaveDelayMin        = 80 * time.Millisecond
	fastWaveDelayMax        = 160 * time.Millisecond
)

// ClearHistoryOptions 控制单次账号历史记录批量清理行为。
type ClearHistoryOptions struct {
	KeepRecent int           // 保留最近 N 条历史对话，0 表示全部清空
	MaxDelete  int           // 单次上限，<=0 默认 200
	DelayMin   time.Duration // 两条之间的最小节流间隔，<=0 默认 400ms
	DelayMax   time.Duration // 两条之间的最大节流间隔，<=0 默认 900ms
	FastMode   bool          // 极速模式：首条 DOM 删除捕获请求模板后并发快删，失败自动回退逐条 DOM
	OnProgress func(deleted int, title string)
}

// ClearHistoryResult 描述批量清理的结果与提前中止原因。
type ClearHistoryResult struct {
	Deleted     int    `json:"deleted"`
	FastDeleted int    `json:"fastDeleted,omitempty"`
	Remaining   int    `json:"remaining"`
	Stopped     string `json:"stopped,omitempty"`
}

// HistoryCleaner 可选策略能力：删除单条会话或批量清理聊天历史记录。
type HistoryCleaner interface {
	DeleteConversation(ctx context.Context, s *cdp.Session, convID string) error
	ClearHistory(ctx context.Context, s *cdp.Session, opts ClearHistoryOptions) (ClearHistoryResult, error)
}

// CurrentConversationID 返回当前页面 URL 对应的会话 ID（豆包/千问为 /chat/<id>，千问国际为 /c/<id>，Gemini 为 /app/<id>）。
func CurrentConversationID(s *cdp.Session) string {
	if s == nil {
		return ""
	}
	if id := chatKey(s); id != "" {
		return id
	}
	raw := strings.Trim(evalString(s, `(() => {
		try {
			const u = new URL(location.href);
			if (/(^|\.)jimeng\.jianying\.com$/i.test(u.hostname)) return u.searchParams.get('workspace') || '';
			const parts = u.pathname.split('/').filter(Boolean);
			const i = parts.indexOf('chat');
			if (i >= 0 && parts[i + 1]) return parts[i + 1];
			const c = parts.indexOf('c');
			if (c >= 0 && parts[c + 1] && !/^(new-chat|new-branch|guest|local)$/i.test(parts[c + 1])) return parts[c + 1];
			const a = parts.indexOf('app');
			if (a >= 0 && parts[a + 1] && !/^(new|images|download|gems|settings|mystuff)$/i.test(parts[a + 1])) return parts[a + 1];
			return '';
		} catch (e) { return ''; }
	})()`), `"`)
	return strings.TrimSpace(raw)
}

type historyStepOutcome struct {
	OK        bool   `json:"ok"`
	Done      bool   `json:"done"`
	Deleted   bool   `json:"deleted"`
	NotFound  bool   `json:"notFound"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Remaining int    `json:"remaining"`
	Stopped   string `json:"stopped"`
	Error     string `json:"error"`
}

type historyCollectResult struct {
	OK        bool              `json:"ok"`
	IDs       []string          `json:"ids"`
	KeepIDs   []string          `json:"keepIds"`
	Titles    map[string]string `json:"titles"`
	TotalSeen int               `json:"totalSeen"`
	Stopped   string            `json:"stopped"`
	Error     string            `json:"error"`
}

type fastReplayResult struct {
	OK         bool     `json:"ok"`
	ID         string   `json:"id"`
	StripSign  bool     `json:"stripSign"`
	DeletedIDs []string `json:"deletedIds"`
	FailedID   string   `json:"failedId"`
	Stopped    string   `json:"stopped"`
	Error      string   `json:"error"`
}

type historyDriver struct {
	installFast   func(ctx context.Context) error
	domStep       func(ctx context.Context, skipIDs []string, keepRecent int) (historyStepOutcome, error)
	takeTemplate  func(ctx context.Context) (json.RawMessage, error)
	collectIDs    func(ctx context.Context, skipIDs []string, keepRecent, maxCount int) (historyCollectResult, error)
	replayOne     func(ctx context.Context, tpl json.RawMessage, id string, stripSign bool) (fastReplayResult, error)
	replayWave    func(ctx context.Context, tpl json.RawMessage, ids []string, concurrency int, stripSign bool) (fastReplayResult, error)
	reloadAndWait func(ctx context.Context) error
}

func stoppedErrorFor(stopped, fallback string) error {
	switch stopped {
	case "not_logged_in":
		return errors.New("账号未登录，请先登录后再清理历史")
	case "captcha":
		return errors.New("触发安全验证，已暂停清理历史")
	case "rate_limit":
		return ErrAccountRate
	case "cancelled":
		return context.Canceled
	default:
		if fallback != "" {
			return errors.New(fallback)
		}
		return errors.New("删除历史记录失败")
	}
}

func (o historyStepOutcome) stoppedError() error {
	return stoppedErrorFor(o.Stopped, o.Error)
}

func normalizeClearOptions(opts ClearHistoryOptions) ClearHistoryOptions {
	if opts.KeepRecent < 0 {
		opts.KeepRecent = 0
	}
	if opts.MaxDelete <= 0 {
		opts.MaxDelete = defaultMaxHistoryDelete
	}
	if opts.DelayMin <= 0 {
		opts.DelayMin = defaultHistoryDelayMin
	}
	if opts.DelayMax < opts.DelayMin {
		opts.DelayMax = max(opts.DelayMin, defaultHistoryDelayMax)
	}
	return opts
}

func sleepJitter(ctx context.Context, minD, maxD time.Duration) error {
	wait := minD
	if span := maxD - minD; span > 0 {
		wait += time.Duration(rand.Int64N(int64(span)))
	}
	if wait <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func clearHistoryLoop(
	ctx context.Context,
	s *cdp.Session,
	opts ClearHistoryOptions,
	step func(ctx context.Context, skipIDs []string, keepRecent int) (historyStepOutcome, error),
) (ClearHistoryResult, error) {
	return clearHistoryExec(ctx, s, opts, historyDriver{domStep: step})
}

func clearHistoryExec(
	ctx context.Context,
	s *cdp.Session,
	opts ClearHistoryOptions,
	drv historyDriver,
) (ClearHistoryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return ClearHistoryResult{}, errors.New("缺少浏览器会话")
	}
	opts = normalizeClearOptions(opts)
	if s.Ctx() != nil {
		if state, err := s.LoginState(); err == nil {
			switch state {
			case "out":
				return ClearHistoryResult{Stopped: "not_logged_in"}, errors.New("账号未登录，请先登录后再清理历史")
			case "challenge":
				return ClearHistoryResult{Stopped: "captcha"}, errors.New("账号待安全验证，请先完成验证后再清理历史")
			}
		}
	}

	canFast := opts.FastMode && drv.installFast != nil && drv.takeTemplate != nil &&
		drv.collectIDs != nil && drv.replayOne != nil && drv.replayWave != nil
	if canFast {
		if err := drv.installFast(ctx); err != nil {
			logging.Log.Debugf("极速清理注入失败，回退逐条模式：%v", err)
			canFast = false
		}
	}

	var res ClearHistoryResult
	var skipIDs []string
	consecutiveFails := 0
	fastTried := false

	for res.Deleted < opts.MaxDelete {
		if err := ctx.Err(); err != nil {
			res.Stopped = "cancelled"
			return res, err
		}
		out, err := drv.domStep(ctx, skipIDs, opts.KeepRecent)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				res.Stopped = "cancelled"
				return res, context.Canceled
			}
			return res, err
		}
		res.Remaining = out.Remaining
		if out.Stopped != "" {
			res.Stopped = out.Stopped
			return res, out.stoppedError()
		}
		if out.Done {
			return res, nil
		}
		if !out.OK {
			if out.ID != "" {
				skipIDs = append(skipIDs, out.ID)
			}
			consecutiveFails++
			if consecutiveFails >= maxConsecutiveDelFails {
				return res, out.stoppedError()
			}
			if err := sleepJitter(ctx, opts.DelayMin, opts.DelayMax); err != nil {
				res.Stopped = "cancelled"
				return res, err
			}
			continue
		}
		consecutiveFails = 0
		if out.Deleted {
			res.Deleted++
			if opts.OnProgress != nil {
				opts.OnProgress(res.Deleted, out.Title)
			}
		}
		if res.Deleted >= opts.MaxDelete {
			res.Stopped = "max_reached"
			return res, nil
		}

		// 极速模式：首条 DOM 删除成功后尝试捕获真实接口模板，转入批量并发快删。
		if canFast && !fastTried && out.Deleted {
			fastTried = true
			tpl, tplErr := drv.takeTemplate(ctx)
			if tplErr == nil && isUsableTemplate(tpl) {
				done, fastErr := runFastHistoryBatch(ctx, opts, drv, tpl, skipIDs, &res)
				if fastErr != nil {
					return res, fastErr
				}
				if done {
					return res, nil
				}
				// 回退逐条 DOM 模式前不再重复尝试极速模式。
				continue
			}
			logging.Log.Debugf("极速清理未捕获到可用删除接口模板，自动回退逐条模式")
		}

		if err := sleepJitter(ctx, opts.DelayMin, opts.DelayMax); err != nil {
			res.Stopped = "cancelled"
			return res, err
		}
	}
	res.Stopped = "max_reached"
	return res, nil
}

func isUsableTemplate(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return len(s) > 2 && s != "null"
}

func runFastHistoryBatch(
	ctx context.Context,
	opts ClearHistoryOptions,
	drv historyDriver,
	tpl json.RawMessage,
	skipIDs []string,
	res *ClearHistoryResult,
) (done bool, err error) {
	verifiedOnce := false
	stripSign := false

	for res.Deleted < opts.MaxDelete {
		if ctxErr := ctx.Err(); ctxErr != nil {
			res.Stopped = "cancelled"
			return false, ctxErr
		}
		budget := opts.MaxDelete - res.Deleted
		col, colErr := drv.collectIDs(ctx, skipIDs, opts.KeepRecent, budget)
		if colErr != nil {
			if errors.Is(colErr, context.Canceled) || ctx.Err() != nil {
				res.Stopped = "cancelled"
				return false, context.Canceled
			}
			logging.Log.Debugf("极速清理收集对话 ID 失败，回退逐条模式：%v", colErr)
			return false, nil
		}
		if col.Stopped != "" {
			res.Stopped = col.Stopped
			return false, stoppedErrorFor(col.Stopped, col.Error)
		}
		res.Remaining = col.TotalSeen
		if len(col.IDs) == 0 {
			return true, nil
		}

		roundFastDeleted := 0
		firstReplayedID := col.IDs[0]
		waveTargets := col.IDs
		fallbackToDOM := false

		// 首次回放先单条探测，确认站点接受模板（含自动剥离旧签名重试）。
		if !verifiedOnce {
			probe, probeErr := drv.replayOne(ctx, tpl, firstReplayedID, stripSign)
			if probeErr != nil {
				if errors.Is(probeErr, context.Canceled) || ctx.Err() != nil {
					res.Stopped = "cancelled"
					return false, context.Canceled
				}
				logging.Log.Debugf("极速清理首条探测异常，回退逐条模式：%v", probeErr)
				return false, nil
			}
			if probe.Stopped != "" {
				res.Stopped = probe.Stopped
				return false, stoppedErrorFor(probe.Stopped, probe.Error)
			}
			if !probe.OK {
				logging.Log.Info("极速清理首条探测未通过（%s），自动回退逐条模式", probe.Error)
				return false, nil
			}
			if probe.StripSign {
				stripSign = true
			}
			roundFastDeleted++
			res.Deleted++
			res.FastDeleted++
			if opts.OnProgress != nil {
				opts.OnProgress(res.Deleted, col.Titles[firstReplayedID])
			}
			waveTargets = col.IDs[1:]
		}

		// 分波并发回放（每波 6 条、并发 3，波间短暂错峰防风控）。
		for offset := 0; offset < len(waveTargets) && res.Deleted < opts.MaxDelete; offset += fastWaveSize {
			if ctxErr := ctx.Err(); ctxErr != nil {
				res.Stopped = "cancelled"
				return false, ctxErr
			}
			end := min(offset+fastWaveSize, len(waveTargets))
			if remain := opts.MaxDelete - res.Deleted; end-offset > remain {
				end = offset + remain
			}
			chunk := waveTargets[offset:end]
			w, wErr := drv.replayWave(ctx, tpl, chunk, fastWaveConcurrency, stripSign)
			if wErr != nil {
				if errors.Is(wErr, context.Canceled) || ctx.Err() != nil {
					res.Stopped = "cancelled"
					return false, context.Canceled
				}
				logging.Log.Warning("极速清理批量回放异常，回退逐条模式：%v", wErr)
				fallbackToDOM = true
				break
			}
			for _, id := range w.DeletedIDs {
				roundFastDeleted++
				res.Deleted++
				res.FastDeleted++
				if opts.OnProgress != nil {
					opts.OnProgress(res.Deleted, col.Titles[id])
				}
			}
			if w.Stopped != "" {
				res.Stopped = w.Stopped
				return false, stoppedErrorFor(w.Stopped, w.Error)
			}
			if !w.OK {
				logging.Log.Warning("极速清理回放中断（%s），刷新页面后回退逐条模式", w.Error)
				fallbackToDOM = true
				break
			}
			if end < len(waveTargets) && res.Deleted < opts.MaxDelete {
				if err := sleepJitter(ctx, fastWaveDelayMin, fastWaveDelayMax); err != nil {
					res.Stopped = "cancelled"
					return false, err
				}
			}
		}

		// 刷新页面同步真实服务端状态，并在首轮核对首条是否真的被服务端删除。
		if drv.reloadAndWait != nil {
			if err := drv.reloadAndWait(ctx); err != nil {
				if errors.Is(err, context.Canceled) || ctx.Err() != nil {
					res.Stopped = "cancelled"
					return false, context.Canceled
				}
				logging.Log.Debugf("极速清理刷新页面失败：%v", err)
				return false, nil
			}
		}

		afterCol, afterErr := drv.collectIDs(ctx, skipIDs, opts.KeepRecent, 20)
		if afterErr == nil && afterCol.OK {
			res.Remaining = afterCol.TotalSeen
			stillPresent := slices.Contains(afterCol.IDs, firstReplayedID) || slices.Contains(afterCol.KeepIDs, firstReplayedID)
			if !verifiedOnce && stillPresent {
				res.Deleted -= roundFastDeleted
				res.FastDeleted -= roundFastDeleted
				logging.Log.Warning("极速清理核对发现会话未被服务端移除，已回退逐条模式")
				return false, nil
			}
			verifiedOnce = true
			if fallbackToDOM {
				return false, nil
			}
			if len(afterCol.IDs) == 0 {
				return true, nil
			}
			if res.Deleted >= opts.MaxDelete {
				res.Stopped = "max_reached"
				return true, nil
			}
			continue
		}

		verifiedOnce = true
		if fallbackToDOM {
			return false, nil
		}
		return true, nil
	}
	res.Stopped = "max_reached"
	return true, nil
}

// ---- 极速页面助手桥接 ----

func ensureHistoryFast(ctx context.Context, s *cdp.Session) error {
	if s == nil {
		return errors.New("缺少浏览器会话")
	}
	s.EnsureBase()
	rev := fmt.Sprintf("%x", sha256.Sum256([]byte(historyFastJS)))
	expr := `(() => {
		if (!window.__tulaiHistoryFast || window.__tulaiHistoryFastRev !== ` + fmt.Sprintf("%q", rev) + `) {
			delete window.__tulaiHistoryFast;` + historyFastJS + `
			window.__tulaiHistoryFastRev = ` + fmt.Sprintf("%q", rev) + `;
		}
		window.__tulaiHistoryFast.install();
		return true;
	})()`
	var ok bool
	return s.EvaluateContext(ctx, expr, &ok)
}

func takeFastTemplate(ctx context.Context, s *cdp.Session) (json.RawMessage, error) {
	var raw string
	err := s.EvaluateContext(ctx, `(async () => {
		if (!window.__tulaiHistoryFast) return 'null';
		return JSON.stringify(await window.__tulaiHistoryFast.takeTemplate(500));
	})()`, &raw)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(strings.TrimSpace(raw)), nil
}

func replayFastOne(ctx context.Context, s *cdp.Session, tpl json.RawMessage, id string, stripSign bool) (fastReplayResult, error) {
	if err := ensureHistoryFast(ctx, s); err != nil {
		return fastReplayResult{}, err
	}
	idJSON, err := json.Marshal(id)
	if err != nil {
		return fastReplayResult{}, err
	}
	var raw string
	expr := fmt.Sprintf(`(async () => JSON.stringify(await window.__tulaiHistoryFast.replayOne(%s, %s, %t)))()`, string(tpl), string(idJSON), stripSign)
	if err := s.EvaluateContext(ctx, expr, &raw); err != nil {
		return fastReplayResult{}, err
	}
	var out fastReplayResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return fastReplayResult{}, err
	}
	return out, nil
}

func replayFastWave(ctx context.Context, s *cdp.Session, tpl json.RawMessage, ids []string, concurrency int, stripSign bool) (fastReplayResult, error) {
	if err := ensureHistoryFast(ctx, s); err != nil {
		return fastReplayResult{}, err
	}
	idsJSON, err := json.Marshal(ids)
	if err != nil {
		return fastReplayResult{}, err
	}
	var raw string
	expr := fmt.Sprintf(`(async () => JSON.stringify(await window.__tulaiHistoryFast.replayWave(%s, %s, %d, %t)))()`, string(tpl), string(idsJSON), concurrency, stripSign)
	if err := s.EvaluateContext(ctx, expr, &raw); err != nil {
		return fastReplayResult{}, err
	}
	var out fastReplayResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return fastReplayResult{}, err
	}
	return out, nil
}

func reloadHistoryPage(ctx context.Context, s *cdp.Session, homeURL, platform string) error {
	if s == nil {
		return errors.New("缺少浏览器会话")
	}
	if err := s.Navigate(homeURL); err != nil {
		return err
	}
	WaitHistorySidebarReady(ctx, s, platform)
	return nil
}

// WaitHistorySidebarReady 等待豆包/千问历史对话侧边栏真实数据加载就绪（避免空壳提前判空）。
// 注意：豆包冷启动初始 DOM 会短暂挂载 data-empty-conversation="true"，不可据此提前返回空，
// 必须等到真实对话条目/登录壁出现，或侧边栏挂载满 3.5 秒后才判定为空历史。
func WaitHistorySidebarReady(ctx context.Context, sess *cdp.Session, platform string) {
	if sess == nil {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	readyExpr := `document.readyState !== 'loading' && !!(Array.from(document.querySelectorAll('[data-testid="conversation-list-v2-item"]')).some(function(el){ return !el.closest('[data-testid="project-grouped-section"]'); }) || document.querySelector('button[data-testid="to_login_button"]'))`
	shellExpr := `document.readyState !== 'loading' && !!document.querySelector('[data-testid="chat_list_wrapper"],[data-testid="flow_chat_sidebar"]')`
	if platform == "千问" {
		readyExpr = `document.readyState !== 'loading' && !!(document.querySelector('[data-session-id][data-session-switch-target="normal"]') || Array.from(document.querySelectorAll('button,[role="button"],a')).some(function(el){ return /^(登录|立即登录|登录\/注册|登录千问)$/.test(String(el.innerText||'').trim()); }))`
		shellExpr = `document.readyState !== 'loading' && !!document.querySelector('[data-testid="qianwen-layout-left-panel"],[role="list"].sider-scrollbar')`
	} else if platform == "千问国际" {
		readyExpr = `document.readyState !== 'loading' && !!(document.querySelector('a.chat-item-drag-link[aria-label="chat-item"],a[href*="/c/"]') || document.querySelector('.header-right-auth-button') || Array.from(document.querySelectorAll('button,[role="button"],a')).some(function(el){ return /^(Log in|Sign in|登录|立即登录)$/i.test(String(el.innerText||'').trim()); }))`
		shellExpr = `document.readyState !== 'loading' && !!document.querySelector('.sidebar-user,.user-menu-btn,textarea.message-input-textarea')`
	} else if platform == "Gemini" {
		readyExpr = `document.readyState !== 'loading' && !!(document.querySelector('[data-test-id="conversation"],a[href*="/app/"]') || document.querySelector('[data-test-id="sidenav-error-action-link"],[data-test-id="sign-out-banner"]') || Array.from(document.querySelectorAll('button,[role="button"],a')).some(function(el){ return /^(Sign in|Log in|登录|立即登录)$/i.test(String(el.innerText||'').trim()); }))`
		shellExpr = `document.readyState !== 'loading' && !!document.querySelector('side-navigation-v2,bard-sidenav,rich-textarea .ql-editor')`
	}
	var shellSeen time.Time
	for time.Now().Before(deadline) {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		if sess.EvaluateBool(readyExpr) {
			timer := time.NewTimer(300 * time.Millisecond)
			select {
			case <-ctxDone(ctx):
				timer.Stop()
			case <-timer.C:
			}
			return
		}
		if shellSeen.IsZero() && sess.EvaluateBool(shellExpr) {
			shellSeen = time.Now()
		}
		if !shellSeen.IsZero() && time.Since(shellSeen) >= 3500*time.Millisecond {
			return
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctxDone(ctx):
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func ctxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

// ---- 豆包历史记录删除 ----

func evalDoubaoHistoryRaw(ctx context.Context, s *cdp.Session, body string) (string, error) {
	if s == nil {
		return "", errors.New("缺少浏览器会话")
	}
	s.EnsureBase()
	pageJS := extbundle.DoubaoPageJS()
	rev := fmt.Sprintf("%x", sha256.Sum256([]byte(pageJS)))
	expr := `(async () => {
		if (!window.__tulaiWashPage || window.__tulaiWashPageRev !== ` + fmt.Sprintf("%q", rev) + ` || typeof window.__tulaiWashPage.deleteConversation !== 'function') {` + pageJS + `
			window.__tulaiWashPage = window.TULAIPage;
			window.__tulaiWashPageRev = ` + fmt.Sprintf("%q", rev) + `;
		}
		` + body + `
	})()`
	var raw string
	if err := s.EvaluateContext(ctx, expr, &raw); err != nil {
		return "", err
	}
	return raw, nil
}

func evalDoubaoHistory(ctx context.Context, s *cdp.Session, body string) (historyStepOutcome, error) {
	raw, err := evalDoubaoHistoryRaw(ctx, s, body)
	if err != nil {
		return historyStepOutcome{}, err
	}
	var out historyStepOutcome
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return historyStepOutcome{}, fmt.Errorf("解析豆包删除结果: %w", err)
	}
	return out, nil
}

// DeleteConversation 删除指定豆包对话（convID 为空时删除当前页对话）。
func (Doubao) DeleteConversation(ctx context.Context, s *cdp.Session, convID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	arg, err := json.Marshal(strings.TrimSpace(convID))
	if err != nil {
		return err
	}
	out, err := evalDoubaoHistory(ctx, s, `return JSON.stringify(await window.__tulaiWashPage.deleteConversation(document, `+string(arg)+`, null, 10000));`)
	if err != nil {
		return err
	}
	if !out.OK {
		return out.stoppedError()
	}
	return nil
}

const doubaoThreadListURL = "https://www.doubao.com/chat/thread/list"

type doubaoBatchStepOutcome struct {
	OK           bool   `json:"ok"`
	Done         bool   `json:"done"`
	Unsupported  bool   `json:"unsupported"`
	DeletedCount int    `json:"deletedCount"`
	Remaining    int    `json:"remaining"`
	Stopped      string `json:"stopped"`
	Error        string `json:"error"`
}

func runDoubaoNativeBatchClear(
	ctx context.Context,
	opts ClearHistoryOptions,
	step func(ctx context.Context, keepRecent, maxCount int) (doubaoBatchStepOutcome, error),
) (res ClearHistoryResult, handled bool, err error) {
	opts = normalizeClearOptions(opts)
	for round := 0; round < 20 && res.Deleted < opts.MaxDelete; round++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			res.Stopped = "cancelled"
			return res, true, ctxErr
		}
		budget := opts.MaxDelete - res.Deleted
		out, stepErr := step(ctx, opts.KeepRecent, budget)
		if stepErr != nil {
			if errors.Is(stepErr, context.Canceled) || ctx.Err() != nil {
				res.Stopped = "cancelled"
				return res, true, context.Canceled
			}
			return res, false, nil
		}
		if out.Stopped != "" {
			res.Stopped = out.Stopped
			res.Remaining = out.Remaining
			return res, true, stoppedErrorFor(out.Stopped, out.Error)
		}
		if out.Unsupported || !out.OK {
			if out.Error != "" {
				logging.Log.Info("豆包原生批量删除未启用（%s），自动回退其他模式", out.Error)
			}
			return res, false, nil
		}
		res.Remaining = out.Remaining
		if out.DeletedCount > 0 {
			res.Deleted += out.DeletedCount
			res.FastDeleted += out.DeletedCount
			if opts.OnProgress != nil {
				opts.OnProgress(res.Deleted, "批量删除对话")
			}
		}
		if out.Done || out.DeletedCount == 0 {
			return res, true, nil
		}
		if res.Deleted >= opts.MaxDelete {
			res.Stopped = "max_reached"
			return res, true, nil
		}
		if err := sleepJitter(ctx, 250*time.Millisecond, 500*time.Millisecond); err != nil {
			res.Stopped = "cancelled"
			return res, true, err
		}
	}
	if res.Deleted >= opts.MaxDelete {
		res.Stopped = "max_reached"
	}
	return res, true, nil
}

// ClearHistory 批量清理豆包聊天历史记录：
// FastMode 下优先走豆包官方「管理历史对话」（/chat/thread/list → 批量删除对话）原生批量删除，
// 若入口不可用则自动回退到接口模板快删及侧边栏逐条 DOM 删除。
func (d Doubao) ClearHistory(ctx context.Context, s *cdp.Session, opts ClearHistoryOptions) (ClearHistoryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil {
		return ClearHistoryResult{}, errors.New("缺少浏览器会话")
	}
	opts = normalizeClearOptions(opts)

	var nativeRes ClearHistoryResult
	if opts.FastMode && s.Ctx() != nil {
		if navErr := s.Navigate(doubaoThreadListURL); navErr == nil {
			WaitHistorySidebarReady(ctx, s, d.Platform())
			var handled bool
			var nativeErr error
			nativeRes, handled, nativeErr = runDoubaoNativeBatchClear(ctx, opts, func(stepCtx context.Context, keepRecent, maxCount int) (doubaoBatchStepOutcome, error) {
				raw, err := evalDoubaoHistoryRaw(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiWashPage.batchDeleteOnThreadList(document, %d, %d, null, 15000));`, keepRecent, maxCount))
				if err != nil {
					return doubaoBatchStepOutcome{}, err
				}
				var out doubaoBatchStepOutcome
				if err := json.Unmarshal([]byte(raw), &out); err != nil {
					return doubaoBatchStepOutcome{}, err
				}
				return out, nil
			})
			if handled {
				return nativeRes, nativeErr
			}
			// 回退前切回主对话页并等待侧边栏就绪
			_ = reloadHistoryPage(ctx, s, d.HomeURL(), d.Platform())
		}
	}

	remOpts := opts
	if nativeRes.Deleted > 0 {
		remOpts.MaxDelete = max(1, opts.MaxDelete-nativeRes.Deleted)
		if opts.OnProgress != nil {
			base := nativeRes.Deleted
			orig := opts.OnProgress
			remOpts.OnProgress = func(deleted int, title string) {
				orig(base+deleted, title)
			}
		}
	}
	fallbackRes, err := clearHistoryExec(ctx, s, remOpts, historyDriver{
		installFast: func(stepCtx context.Context) error {
			return ensureHistoryFast(stepCtx, s)
		},
		domStep: func(stepCtx context.Context, skipIDs []string, keepRecent int) (historyStepOutcome, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyStepOutcome{}, err
			}
			return evalDoubaoHistory(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiWashPage.deleteNextHistoryItem(document, %s, %d, null, 10000));`, string(skipJSON), keepRecent))
		},
		takeTemplate: func(stepCtx context.Context) (json.RawMessage, error) {
			return takeFastTemplate(stepCtx, s)
		},
		collectIDs: func(stepCtx context.Context, skipIDs []string, keepRecent, maxCount int) (historyCollectResult, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyCollectResult{}, err
			}
			raw, err := evalDoubaoHistoryRaw(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiWashPage.collectHistoryIds(document, %s, %d, %d));`, string(skipJSON), keepRecent, maxCount))
			if err != nil {
				return historyCollectResult{}, err
			}
			var out historyCollectResult
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				return historyCollectResult{}, err
			}
			return out, nil
		},
		replayOne: func(stepCtx context.Context, tpl json.RawMessage, id string, stripSign bool) (fastReplayResult, error) {
			return replayFastOne(stepCtx, s, tpl, id, stripSign)
		},
		replayWave: func(stepCtx context.Context, tpl json.RawMessage, ids []string, concurrency int, stripSign bool) (fastReplayResult, error) {
			return replayFastWave(stepCtx, s, tpl, ids, concurrency, stripSign)
		},
		reloadAndWait: func(stepCtx context.Context) error {
			return reloadHistoryPage(stepCtx, s, d.HomeURL(), d.Platform())
		},
	})
	fallbackRes.Deleted += nativeRes.Deleted
	fallbackRes.FastDeleted += nativeRes.FastDeleted
	return fallbackRes, err
}

// ---- 千问历史记录删除 ----

func evalQianwenHistoryRaw(ctx context.Context, s *cdp.Session, body string) (string, error) {
	if s == nil {
		return "", errors.New("缺少浏览器会话")
	}
	s.EnsureBase()
	rev := fmt.Sprintf("%x", sha256.Sum256([]byte(qwenHistoryJS)))
	expr := `(async () => {
		if (!window.__tulaiQwenHistory || window.__tulaiQwenHistoryRev !== ` + fmt.Sprintf("%q", rev) + `) {
			delete window.__tulaiQwenHistory;` + qwenHistoryJS + `
			window.__tulaiQwenHistoryRev = ` + fmt.Sprintf("%q", rev) + `;
		}
		` + body + `
	})()`
	var raw string
	if err := s.EvaluateContext(ctx, expr, &raw); err != nil {
		return "", err
	}
	return raw, nil
}

func evalQianwenHistory(ctx context.Context, s *cdp.Session, body string) (historyStepOutcome, error) {
	raw, err := evalQianwenHistoryRaw(ctx, s, body)
	if err != nil {
		return historyStepOutcome{}, err
	}
	var out historyStepOutcome
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return historyStepOutcome{}, fmt.Errorf("解析千问删除结果: %w", err)
	}
	return out, nil
}

// DeleteConversation 删除指定千问会话（convID 为空时删除当前页会话）。
func (Qianwen) DeleteConversation(ctx context.Context, s *cdp.Session, convID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	convID = strings.TrimSpace(convID)
	arg, err := json.Marshal(convID)
	if err != nil {
		return err
	}
	out, err := evalQianwenHistory(ctx, s, `return JSON.stringify(await window.__tulaiQwenHistory.deleteConversation(document, `+string(arg)+`, null, 10000));`)
	if err != nil {
		return err
	}
	if !out.OK && out.NotFound && convID != "" {
		// 虚拟列表当前视口未命中时，先切回目标会话再试一次。
		FocusQwenChat(s, convID, "")
		out, err = evalQianwenHistory(ctx, s, `return JSON.stringify(await window.__tulaiQwenHistory.deleteConversation(document, `+string(arg)+`, null, 10000));`)
		if err != nil {
			return err
		}
	}
	if !out.OK {
		return out.stoppedError()
	}
	return nil
}

// ClearHistory 批量清理千问聊天历史记录（适配 react-window 虚拟列表；FastMode 时启用模板捕获并发快删）。
func (q Qianwen) ClearHistory(ctx context.Context, s *cdp.Session, opts ClearHistoryOptions) (ClearHistoryResult, error) {
	return clearHistoryExec(ctx, s, opts, historyDriver{
		installFast: func(stepCtx context.Context) error {
			return ensureHistoryFast(stepCtx, s)
		},
		domStep: func(stepCtx context.Context, skipIDs []string, keepRecent int) (historyStepOutcome, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyStepOutcome{}, err
			}
			return evalQianwenHistory(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiQwenHistory.deleteNextHistoryItem(document, %s, %d, null, 10000));`, string(skipJSON), keepRecent))
		},
		takeTemplate: func(stepCtx context.Context) (json.RawMessage, error) {
			return takeFastTemplate(stepCtx, s)
		},
		collectIDs: func(stepCtx context.Context, skipIDs []string, keepRecent, maxCount int) (historyCollectResult, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyCollectResult{}, err
			}
			raw, err := evalQianwenHistoryRaw(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiQwenHistory.collectHistoryIds(document, %s, %d, %d));`, string(skipJSON), keepRecent, maxCount))
			if err != nil {
				return historyCollectResult{}, err
			}
			var out historyCollectResult
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				return historyCollectResult{}, err
			}
			return out, nil
		},
		replayOne: func(stepCtx context.Context, tpl json.RawMessage, id string, stripSign bool) (fastReplayResult, error) {
			return replayFastOne(stepCtx, s, tpl, id, stripSign)
		},
		replayWave: func(stepCtx context.Context, tpl json.RawMessage, ids []string, concurrency int, stripSign bool) (fastReplayResult, error) {
			return replayFastWave(stepCtx, s, tpl, ids, concurrency, stripSign)
		},
		reloadAndWait: func(stepCtx context.Context) error {
			return reloadHistoryPage(stepCtx, s, q.HomeURL(), q.Platform())
		},
	})
}

// ---- 千问国际历史记录删除 ----

func evalQwenIntlHistoryRaw(ctx context.Context, s *cdp.Session, body string) (string, error) {
	if s == nil {
		return "", errors.New("缺少浏览器会话")
	}
	s.EnsureBase()
	rev := fmt.Sprintf("%x", sha256.Sum256([]byte(qwenIntlHistoryJS)))
	expr := `(async () => {
		if (!window.__tulaiQwenIntlHistory || window.__tulaiQwenIntlHistoryRev !== ` + fmt.Sprintf("%q", rev) + `) {
			delete window.__tulaiQwenIntlHistory;` + qwenIntlHistoryJS + `
			window.__tulaiQwenIntlHistoryRev = ` + fmt.Sprintf("%q", rev) + `;
		}
		` + body + `
	})()`
	var raw string
	if err := s.EvaluateContext(ctx, expr, &raw); err != nil {
		return "", err
	}
	return raw, nil
}

func evalQwenIntlHistory(ctx context.Context, s *cdp.Session, body string) (historyStepOutcome, error) {
	raw, err := evalQwenIntlHistoryRaw(ctx, s, body)
	if err != nil {
		return historyStepOutcome{}, err
	}
	var out historyStepOutcome
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return historyStepOutcome{}, fmt.Errorf("解析千问国际删除结果: %w", err)
	}
	return out, nil
}

// DeleteConversation 删除指定千问国际会话（convID 为空时删除当前页会话）。
func (QwenIntl) DeleteConversation(ctx context.Context, s *cdp.Session, convID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	convID = strings.TrimSpace(convID)
	arg, err := json.Marshal(convID)
	if err != nil {
		return err
	}
	out, err := evalQwenIntlHistory(ctx, s, `return JSON.stringify(await window.__tulaiQwenIntlHistory.deleteConversation(document, `+string(arg)+`, null));`)
	if err != nil {
		return err
	}
	if !out.OK {
		return out.stoppedError()
	}
	return nil
}

// ClearHistory 批量清理千问国际聊天历史记录（FastMode 下启用同源分页与分波并发快删）。
func (q QwenIntl) ClearHistory(ctx context.Context, s *cdp.Session, opts ClearHistoryOptions) (ClearHistoryResult, error) {
	return clearHistoryExec(ctx, s, opts, historyDriver{
		installFast: func(stepCtx context.Context) error {
			return nil
		},
		domStep: func(stepCtx context.Context, skipIDs []string, keepRecent int) (historyStepOutcome, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyStepOutcome{}, err
			}
			return evalQwenIntlHistory(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiQwenIntlHistory.deleteNextHistoryItem(document, %s, %d, null));`, string(skipJSON), keepRecent))
		},
		takeTemplate: func(stepCtx context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{"mode":"qwen_intl_api"}`), nil
		},
		collectIDs: func(stepCtx context.Context, skipIDs []string, keepRecent, maxCount int) (historyCollectResult, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyCollectResult{}, err
			}
			raw, err := evalQwenIntlHistoryRaw(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiQwenIntlHistory.collectHistoryIds(document, %s, %d, %d));`, string(skipJSON), keepRecent, maxCount))
			if err != nil {
				return historyCollectResult{}, err
			}
			var out historyCollectResult
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				return historyCollectResult{}, err
			}
			return out, nil
		},
		replayOne: func(stepCtx context.Context, _ json.RawMessage, id string, _ bool) (fastReplayResult, error) {
			idJSON, err := json.Marshal(id)
			if err != nil {
				return fastReplayResult{}, err
			}
			raw, err := evalQwenIntlHistoryRaw(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiQwenIntlHistory.deleteConversation(document, %s, null));`, string(idJSON)))
			if err != nil {
				return fastReplayResult{}, err
			}
			var step historyStepOutcome
			if err := json.Unmarshal([]byte(raw), &step); err != nil {
				return fastReplayResult{}, err
			}
			return fastReplayResult{OK: step.OK, ID: id, Stopped: step.Stopped, Error: step.Error}, nil
		},
		replayWave: func(stepCtx context.Context, _ json.RawMessage, ids []string, concurrency int, _ bool) (fastReplayResult, error) {
			idsJSON, err := json.Marshal(ids)
			if err != nil {
				return fastReplayResult{}, err
			}
			raw, err := evalQwenIntlHistoryRaw(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiQwenIntlHistory.deleteWave(document, %s, %d));`, string(idsJSON), concurrency))
			if err != nil {
				return fastReplayResult{}, err
			}
			var out fastReplayResult
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				return fastReplayResult{}, err
			}
			return out, nil
		},
		reloadAndWait: func(stepCtx context.Context) error {
			return reloadHistoryPage(stepCtx, s, q.HomeURL(), q.Platform())
		},
	})
}

// ---- Gemini 历史记录删除 ----

func evalGeminiHistoryRaw(ctx context.Context, s *cdp.Session, body string) (string, error) {
	if s == nil {
		return "", errors.New("缺少浏览器会话")
	}
	s.EnsureBase()
	rev := fmt.Sprintf("%x", sha256.Sum256([]byte(geminiHistoryJS)))
	expr := `(async () => {
		if (!window.__tulaiGeminiHistory || window.__tulaiGeminiHistoryRev !== ` + fmt.Sprintf("%q", rev) + `) {
			delete window.__tulaiGeminiHistory;` + geminiHistoryJS + `
			window.__tulaiGeminiHistoryRev = ` + fmt.Sprintf("%q", rev) + `;
		}
		` + body + `
	})()`
	var raw string
	if err := s.EvaluateContext(ctx, expr, &raw); err != nil {
		return "", err
	}
	return raw, nil
}

func evalGeminiHistory(ctx context.Context, s *cdp.Session, body string) (historyStepOutcome, error) {
	raw, err := evalGeminiHistoryRaw(ctx, s, body)
	if err != nil {
		return historyStepOutcome{}, err
	}
	var out historyStepOutcome
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return historyStepOutcome{}, fmt.Errorf("解析 Gemini 删除结果: %w", err)
	}
	return out, nil
}

// DeleteConversation 删除指定 Gemini 对话（convID 为空时删除当前页对话）。
func (Gemini) DeleteConversation(ctx context.Context, s *cdp.Session, convID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	convID = strings.TrimSpace(convID)
	arg, err := json.Marshal(convID)
	if err != nil {
		return err
	}
	out, err := evalGeminiHistory(ctx, s, `return JSON.stringify(await window.__tulaiGeminiHistory.deleteConversation(document, `+string(arg)+`, null, 10000));`)
	if err != nil {
		return err
	}
	if !out.OK && out.NotFound && convID != "" {
		FocusQwenChatFor(s, "Gemini", convID, "")
		out, err = evalGeminiHistory(ctx, s, `return JSON.stringify(await window.__tulaiGeminiHistory.deleteConversation(document, `+string(arg)+`, null, 10000));`)
		if err != nil {
			return err
		}
	}
	if !out.OK {
		return out.stoppedError()
	}
	return nil
}

// ClearHistory 批量清理 Gemini 聊天历史记录（FastMode 下尝试捕获删除模板并自动回退逐条侧边栏 DOM 删除）。
func (g Gemini) ClearHistory(ctx context.Context, s *cdp.Session, opts ClearHistoryOptions) (ClearHistoryResult, error) {
	return clearHistoryExec(ctx, s, opts, historyDriver{
		installFast: func(stepCtx context.Context) error {
			return ensureHistoryFast(stepCtx, s)
		},
		domStep: func(stepCtx context.Context, skipIDs []string, keepRecent int) (historyStepOutcome, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyStepOutcome{}, err
			}
			return evalGeminiHistory(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiGeminiHistory.deleteNextHistoryItem(document, %s, %d, null, 10000));`, string(skipJSON), keepRecent))
		},
		takeTemplate: func(stepCtx context.Context) (json.RawMessage, error) {
			return takeFastTemplate(stepCtx, s)
		},
		collectIDs: func(stepCtx context.Context, skipIDs []string, keepRecent, maxCount int) (historyCollectResult, error) {
			skipJSON, err := json.Marshal(skipIDs)
			if err != nil {
				return historyCollectResult{}, err
			}
			raw, err := evalGeminiHistoryRaw(stepCtx, s, fmt.Sprintf(`return JSON.stringify(await window.__tulaiGeminiHistory.collectHistoryIds(document, %s, %d, %d));`, string(skipJSON), keepRecent, maxCount))
			if err != nil {
				return historyCollectResult{}, err
			}
			var out historyCollectResult
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				return historyCollectResult{}, err
			}
			return out, nil
		},
		replayOne: func(stepCtx context.Context, tpl json.RawMessage, id string, stripSign bool) (fastReplayResult, error) {
			return replayFastOne(stepCtx, s, tpl, id, stripSign)
		},
		replayWave: func(stepCtx context.Context, tpl json.RawMessage, ids []string, concurrency int, stripSign bool) (fastReplayResult, error) {
			return replayFastWave(stepCtx, s, tpl, ids, concurrency, stripSign)
		},
		reloadAndWait: func(stepCtx context.Context) error {
			return reloadHistoryPage(stepCtx, s, g.HomeURL(), g.Platform())
		},
	})
}
