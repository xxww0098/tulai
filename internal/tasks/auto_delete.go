package tasks

import (
	"context"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/strategy"
)

const autoDeleteTimeout = 12 * time.Second

type pendingDeleteChat struct {
	platform string
	convID   string
}

func (e *Engine) resolveConvID(t *Task, sess *cdp.Session) string {
	if t != nil && strings.TrimSpace(t.sentURL) != "" {
		return strings.TrimSpace(t.sentURL)
	}
	if e != nil && e.convIDFn != nil {
		return strings.TrimSpace(e.convIDFn(sess))
	}
	return strings.TrimSpace(strategy.CurrentConversationID(sess))
}

func (e *Engine) cancelPendingDelete(sess *cdp.Session) {
	if e == nil || sess == nil {
		return
	}
	e.mu.Lock()
	delete(e.pendingDeleteChats, sess)
	e.mu.Unlock()
}

func (e *Engine) recordPendingDelete(sess *cdp.Session, platform, convID string) {
	if e == nil || sess == nil || convID == "" {
		return
	}
	e.mu.Lock()
	if e.pendingDeleteChats == nil {
		e.pendingDeleteChats = make(map[*cdp.Session]pendingDeleteChat)
	}
	e.pendingDeleteChats[sess] = pendingDeleteChat{platform: platform, convID: convID}
	e.mu.Unlock()
}

func (e *Engine) popPendingDelete(sess *cdp.Session) (pendingDeleteChat, bool) {
	if e == nil || sess == nil {
		return pendingDeleteChat{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	item, ok := e.pendingDeleteChats[sess]
	if ok {
		delete(e.pendingDeleteChats, sess)
	}
	return item, ok
}

func (e *Engine) deleteConversationBestEffort(ctx context.Context, platform string, sess *cdp.Session, convID string, label string) {
	if e == nil || convID == "" || ctx == nil || ctx.Err() != nil {
		return
	}
	delCtx, cancel := context.WithTimeout(ctx, autoDeleteTimeout)
	defer cancel()
	var err error
	if e.deleteConvFn != nil {
		err = e.deleteConvFn(delCtx, platform, sess, convID)
	} else {
		if sess == nil || (sess.Ctx() != nil && sess.Ctx().Err() != nil) {
			return
		}
		cleaner, ok := strategy.Factory(platform).(strategy.HistoryCleaner)
		if !ok {
			return
		}
		err = cleaner.DeleteConversation(delCtx, sess, convID)
	}
	if err != nil {
		logging.Log.Warning("%s 自动删除%s聊天记录(%s)失败：%v", label, platform, convID, err)
		return
	}
	logging.Log.Info("%s 已自动删除%s聊天记录(%s)", label, platform, convID)
}

// maybeAutoDeleteHistory 严格遵循提交边界：仅在确认保存图片且无异常时删除聊天记录；
// 发送状态未确认、失败或取消时绝不删除，并撤销同会话上挂起的延迟删除。
func (e *Engine) maybeAutoDeleteHistory(ctx context.Context, t *Task, sess *cdp.Session, attemptErr error) {
	if e == nil || t == nil || !e.opts.AutoDeleteHistory || !e.opts.WaitResult {
		return
	}
	succeeded := attemptErr == nil && t.submitted && (ctx == nil || ctx.Err() == nil) && (len(t.Results) > 0 || t.matchedSavedResult)
	if !succeeded {
		e.cancelPendingDelete(sess)
		return
	}
	convID := e.resolveConvID(t, sess)
	if convID == "" {
		return
	}
	// 豆包洗图会话复用期间不中途删除，等会话不再复用或本批结束后统一删除。
	if e.opts.Wash && t.Platform == "豆包" && sess != nil {
		e.mu.Lock()
		chat := e.washChats[sess]
		e.mu.Unlock()
		if chat.url != "" && chat.count > 0 && chat.count < washChatTasks {
			e.recordPendingDelete(sess, t.Platform, convID)
			return
		}
		e.cancelPendingDelete(sess)
	}
	e.deleteConversationBestEffort(ctx, t.Platform, sess, convID, formatTaskLabel(t))
}

func (e *Engine) deletePendingWashChat(ctx context.Context, sess *cdp.Session) {
	if e == nil || !e.opts.AutoDeleteHistory || !e.opts.WaitResult {
		return
	}
	item, ok := e.popPendingDelete(sess)
	if !ok {
		return
	}
	e.deleteConversationBestEffort(ctx, item.platform, sess, item.convID, "[洗图会话结束]")
}

func (e *Engine) flushPendingAutoDelete(ctx context.Context) {
	if e == nil || !e.opts.AutoDeleteHistory || !e.opts.WaitResult || ctx == nil || ctx.Err() != nil {
		return
	}
	e.mu.Lock()
	pending := e.pendingDeleteChats
	e.pendingDeleteChats = nil
	e.mu.Unlock()
	for sess, item := range pending {
		if ctx.Err() != nil {
			return
		}
		e.deleteConversationBestEffort(ctx, item.platform, sess, item.convID, "[批次收尾]")
	}
}

func formatTaskLabel(t *Task) string {
	if t == nil || t.Index <= 0 {
		return "[任务]"
	}
	return "#" + itoa(t.Index)
}
