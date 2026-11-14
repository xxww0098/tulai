package tasks

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/heal"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

// prepareAccount 在派发前把任务放到一个还能用的账号上。
// 当前账号被冷却时改派到最少最近使用的下一个。全部要等冷却时返回等待截止时间。
// 没有会自动恢复的账号时 dead 为真，调用方把这一条标失败并继续别的平台。
func (e *Engine) prepareAccount(task *Task, now time.Time) (wait time.Time, dead bool) {
	if e == nil || e.store == nil || task == nil {
		return time.Time{}, false
	}
	rows := e.accountsFor(task.Platform)
	if task.Platform == "ChatGPT" && task.chatGPT != nil && task.chatGPT.Attempted {
		if current := findBrowser(rows, task.BrowserID); current == nil || current.Disabled {
			task.Error = "ChatGPT 原任务的账号已移除或停用，请检查该账号的网页结果"
			return time.Time{}, true
		}
		if e.AccountManual(task.BrowserID) {
			return now.Add(heal.RecheckAfter), false
		}
		return time.Time{}, false
	}
	if len(rows) == 0 {
		return time.Time{}, false
	}
	limit := e.capFor(task.Platform)
	if task.BrowserID <= 0 {
		if id, ok := pickHealthy(e.store, rows, 0, now, limit); ok {
			task.BrowserID = id
		}
	}
	current := findBrowser(rows, task.BrowserID)
	if current != nil && openAccount(*current, now, limit) {
		return time.Time{}, false
	}
	if id, ok := pickHealthy(e.store, rows, task.BrowserID, now, limit); ok {
		task.BrowserID = id
		task.avoidBrowser = 0
		task.Error = ""
		return time.Time{}, false
	}
	for _, b := range rows {
		if b.Disabled {
			return now.Add(heal.RecheckAfter), false
		}
	}
	if current == nil {
		return time.Time{}, false
	}
	until := earliestCool(rows, now)
	if !until.IsZero() && accountsNeedUser(rows, now, limit) {
		if task.Error == "" {
			task.Error = "需要在网页里完成验证"
		}
		return time.Time{}, true
	}
	if task.Platform == agy.Platform && agyAccountsBlocked(rows, now, limit) && !cappedOut(rows, now, limit) {
		e.noteAgyNoAccount(now)
		if until.IsZero() {
			task.Error = ""
			return now.Add(heal.RecheckAfter), false
		}
		return until, false
	}
	if until.IsZero() {
		if e.accountWaitingOnUser(rows, now) {
			return now.Add(heal.RecheckAfter), false
		}
		if task.Error == "" {
			if needsAgyLogin(rows) {
				task.Error = agy.ErrNeedLogin.Error()
			} else if cappedOut(rows, now, limit) {
				task.Error = "今日已达上限"
			} else {
				task.Error = "同平台账号都不可用"
			}
		}
		return time.Time{}, true
	}
	return until, false
}

// maybeRotate 在额度、限流、登录或验证码打到当前账号时改派。
// 已经发出去的任务保留原账号，只更新账号状态，不换号重发。
func (e *Engine) maybeRotate(task *Task, err error, now time.Time) bool {
	kind := classifyFault(err)
	switch kind {
	case faultNone, faultUncertain:
		return false
	}
	if task == nil {
		return false
	}
	if task.submitted {
		id := task.ranOnOr(task.BrowserID)
		e.noteFault(id, kind, err, now)
		e.announceAccount(id, kind)
		return false
	}
	current := task.ranOnOr(task.BrowserID)
	next, ok := pickHealthy(e.store, e.accountsFor(task.Platform), current, now, e.capFor(task.Platform))
	if !ok {
		if kind != faultOffline {
			e.noteFault(current, kind, err, now)
			e.announceAccount(current, kind)
		}
		return false
	}
	e.noteFault(current, kind, err, now)
	e.announceAccount(current, kind)
	name := browserName(e.store, next)
	old := browserName(e.store, current)
	label := faultLabel(kind)
	if errors.Is(err, agy.ErrNeedLogin) {
		label = agy.ErrNeedLogin.Error()
	}
	logging.Log.Warning("#%d「%s」%s，改用「%s」", task.Index, old, label, name)
	task.BrowserID = next
	task.avoidBrowser = 0
	task.submitted = false
	task.sentURL, task.sentWrap = "", ""
	task.chatGPT = nil
	task.rotated = true
	task.pauseNote = ""
	if task.Attempts > 0 {
		task.Attempts--
	}
	task.Status = StatusPending
	task.Error = fmt.Sprintf("已换到「%s」：%s", name, faultLabel(kind))
	return true
}

// rotateOnFailure 在普通失败后把这一条交给同平台的另一个健康账号。
// 不把这错误记成账号故障，只避开刚刚失败的账号。
func (e *Engine) rotateOnFailure(task *Task, now time.Time) bool {
	if e == nil || e.store == nil || task == nil || task.submitted {
		return false
	}
	current := task.ranOnOr(task.BrowserID)
	next, ok := pickHealthy(e.store, e.accountsFor(task.Platform), current, now, e.capFor(task.Platform))
	if !ok || next == current {
		return false
	}
	task.avoidBrowser = current
	task.BrowserID = next
	task.submitted = false
	task.sentURL, task.sentWrap = "", ""
	task.chatGPT = nil
	task.rotated = true
	task.pauseNote = "rotate"
	return true
}

func (e *Engine) hasOtherHealthy(platform string, avoid int64, now time.Time) bool {
	_, ok := pickHealthy(e.store, e.accountsFor(platform), avoid, now, e.capFor(platform))
	return ok
}

func (e *Engine) accountsFor(platform string) []store.Browser {
	if e == nil || e.store == nil {
		return nil
	}
	extension := e.useExtension(platform) && !e.useBoundExtension(platform)
	var out []store.Browser
	for _, b := range e.store.Browsers() {
		if b.Platform != platform {
			continue
		}
		if extension && b.ExtAccount == "" {
			continue
		}
		if !extension && b.ExtAccount != "" {
			continue
		}
		if e.AccountManual(b.ID) {
			// 只改调度快照，用户账号的健康状态和登录信息仍保留在库里。
			b.Health, b.CoolUntil = store.HealthCooling, time.Now().Add(2*time.Second).Unix()
		}
		out = append(out, b)
	}
	return out
}

func (t *Task) ranOnOr(fallback int64) int64 {
	if t != nil && t.ranOn > 0 {
		return t.ranOn
	}
	return fallback
}

// agyAccountsBlocked 是这个平台一个能用的号都没有。每日上限另走失败，不在这里。
func agyAccountsBlocked(rows []store.Browser, now time.Time, cap int) bool {
	if len(rows) == 0 {
		return false
	}
	for _, b := range rows {
		if b.Platform != agy.Platform || openAccount(b, now, cap) {
			return false
		}
	}
	return true
}

func openAccount(b store.Browser, now time.Time, cap int) bool {
	return openAccountCaps(b, now, FlatCap(cap))
}

// openAccountCaps 按账号所属供应商的上限判断。上限为 0 或负数时视为不限制。
func openAccountCaps(b store.Browser, now time.Time, caps CapFor) bool {
	if b.Disabled || !b.Usable(now) {
		return false
	}
	capN := 0
	if caps != nil {
		capN = caps(b.Platform)
	}
	return capN <= 0 || b.UsedToday(now) < capN
}

func cappedOut(rows []store.Browser, now time.Time, cap int) bool {
	hit := false
	for _, b := range rows {
		if openAccount(b, now, cap) {
			return false
		}
		if b.Usable(now) && cap > 0 && b.UsedToday(now) >= cap {
			hit = true
		}
	}
	return hit
}

func pickHealthy(st *store.Store, rows []store.Browser, avoid int64, now time.Time, cap int) (int64, bool) {
	return pickHealthyCaps(st, rows, avoid, now, FlatCap(cap))
}

func pickHealthyCaps(st *store.Store, rows []store.Browser, avoid int64, now time.Time, caps CapFor) (int64, bool) {
	var best *store.Browser
	bestRemaining := -1.0
	for i := range rows {
		b := &rows[i]
		if b.ID == avoid || !openAccountCaps(*b, now, caps) {
			continue
		}
		remaining := -1.0
		if b.Platform == agy.Platform {
			remaining = agyRemaining(st, b.ID, now)
		}
		if best == nil || remaining > bestRemaining || (remaining == bestRemaining && (b.LastUsed < best.LastUsed || (b.LastUsed == best.LastUsed && b.ID < best.ID))) {
			best = b
			bestRemaining = remaining
		}
	}
	if best == nil {
		return 0, false
	}
	return best.ID, true
}

// ParallelSlots 是这些账号里现在能同时出图的名额。
// 同一个扩展配置文件只算一个：扩展每次命令都把标签页拉到前台，一个窗口不能两单一起跑。
func ParallelSlots(rows []store.Browser, now time.Time, cap int) int {
	return ParallelSlotsCaps(rows, now, FlatCap(cap))
}

// ParallelSlotsCaps 按账号所属供应商的上限统计可用名额。
func ParallelSlotsCaps(rows []store.Browser, now time.Time, caps CapFor) int {
	seen := map[string]struct{}{}
	for _, b := range rows {
		if !openAccountCaps(b, now, caps) {
			continue
		}
		key := strconv.FormatInt(b.ID, 10)
		if b.ExtAccount != "" {
			key = "ext:" + b.ExtAccount
		}
		seen[key] = struct{}{}
	}
	return len(seen)
}

func earliestCool(rows []store.Browser, now time.Time) time.Time {
	var earliest time.Time
	for _, b := range rows {
		if b.Disabled || b.Usable(now) || b.CoolUntil <= 0 {
			continue
		}
		until := time.Unix(b.CoolUntil, 0)
		if earliest.IsZero() || until.Before(earliest) {
			earliest = until
		}
	}
	return earliest
}

func findBrowser(rows []store.Browser, id int64) *store.Browser {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}

func browserName(st *store.Store, id int64) string {
	if st == nil {
		return fmt.Sprintf("%d", id)
	}
	for _, b := range st.Browsers() {
		if b.ID == id {
			if b.Name != "" {
				return b.Name
			}
			break
		}
	}
	return fmt.Sprintf("%d", id)
}
