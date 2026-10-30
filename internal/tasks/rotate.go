package tasks

import (
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
	if len(rows) == 0 {
		return time.Time{}, false
	}
	limit := e.capNow()
	if task.BrowserID <= 0 {
		if id, ok := pickHealthy(rows, 0, now, limit); ok {
			task.BrowserID = id
		}
	}
	current := findBrowser(rows, task.BrowserID)
	if current == nil || openAccount(*current, now, limit) {
		return time.Time{}, false
	}
	if id, ok := pickHealthy(rows, task.BrowserID, now, limit); ok {
		task.BrowserID = id
		task.Error = ""
		return time.Time{}, false
	}
	until := earliestCool(rows, now)
	if !until.IsZero() && accountsNeedUser(rows, now, limit) {
		if task.Error == "" {
			task.Error = "需要在网页里完成验证"
		}
		return time.Time{}, true
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
// 已经发出去的验证码和登录失败不换号重发。发送没确认时由 rotateUncertain 另换一次。
func (e *Engine) maybeRotate(task *Task, err error, now time.Time) bool {
	kind := classifyFault(err)
	switch kind {
	case faultNone, faultUncertain:
		return false
	}
	if task == nil {
		return false
	}
	if (kind == faultCaptcha || kind == faultAuth) && task.submitted {
		id := task.ranOnOr(task.BrowserID)
		e.noteFault(id, kind, err, now)
		e.announceAccount(id, kind)
		return false
	}
	current := task.ranOnOr(task.BrowserID)
	next, ok := pickHealthy(e.accountsFor(task.Platform), current, now, e.capNow())
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
	logging.Log.Warning("#%d 账号 %s，改用「%s」", task.Index, faultLabel(kind), name)
	task.BrowserID = next
	task.submitted = false
	task.sentURL, task.sentWrap = "", ""
	task.rotated = true
	if task.Attempts > 0 {
		task.Attempts--
	}
	task.Status = StatusPending
	task.Error = fmt.Sprintf("已换到「%s」：%s", name, faultLabel(kind))
	return true
}

// rotateUncertain 在发送没被页面接住时，换一个同平台的空闲账号再试一次。
// 同一个账号不再发：点击已经发生，重发可能变成两条。没有别的账号就只让这一条失败。
func (e *Engine) rotateUncertain(task *Task, now time.Time) bool {
	if e == nil || task == nil || task.submitted || task.altTried {
		return false
	}
	current := task.ranOnOr(task.BrowserID)
	next, ok := pickHealthy(e.accountsFor(task.Platform), current, now, e.capNow())
	if !ok || next == current {
		return false
	}
	name := browserName(e.store, next)
	logging.Log.Warning("#%d 发送未确认，改用「%s」", task.Index, name)
	task.Reassigned = name
	task.avoidSend = current
	task.BrowserID = next
	task.submitted = false
	task.sentURL, task.sentWrap = "", ""
	task.rotated = true
	task.altTried = true
	if task.Attempts > 0 {
		task.Attempts--
	}
	task.Status = StatusPending
	task.Error = fmt.Sprintf("发送状态未确认，等待「%s」空闲后再试", name)
	return true
}

func (e *Engine) hasOtherHealthy(platform string, avoid int64, now time.Time) bool {
	_, ok := pickHealthy(e.accountsFor(platform), avoid, now, e.capNow())
	return ok
}

func (e *Engine) accountsFor(platform string) []store.Browser {
	if e == nil || e.store == nil {
		return nil
	}
	extension := e.useExtension(platform)
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

func openAccount(b store.Browser, now time.Time, cap int) bool {
	if !b.Usable(now) {
		return false
	}
	return cap <= 0 || b.UsedToday(now) < cap
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

func pickHealthy(rows []store.Browser, avoid int64, now time.Time, cap int) (int64, bool) {
	var best *store.Browser
	for i := range rows {
		b := &rows[i]
		if b.ID == avoid || !openAccount(*b, now, cap) {
			continue
		}
		if best == nil || b.LastUsed < best.LastUsed || (b.LastUsed == best.LastUsed && b.ID < best.ID) {
			best = b
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
	seen := map[string]struct{}{}
	for _, b := range rows {
		if !openAccount(b, now, cap) {
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
		if b.Usable(now) || b.CoolUntil <= 0 {
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
