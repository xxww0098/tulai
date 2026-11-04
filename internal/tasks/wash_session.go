package tasks

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/strategy"
)

const washChatTasks = 10

type washChat struct {
	url   string
	count int
}

type extensionWashChat struct {
	sessionID string
	tabID     int
	url       string
	count     int
}

func (e *Engine) clearExtensionWash(account int64) {
	e.mu.Lock()
	delete(e.extWashChats, account)
	e.mu.Unlock()
}

func (e *Engine) prepareExtensionWash(ctx context.Context, sess *extbridge.Session, t *Task) (extensionWashChat, error) {
	e.mu.Lock()
	prev := e.extWashChats[t.BrowserID]
	e.mu.Unlock()
	taskID := strconv.Itoa(t.Index)
	if t.Attempts <= 1 && prev.sessionID == sess.ID() && prev.count > 0 && prev.count+max(1, len(t.Images)) <= washChatTasks {
		ack, err := extCall(ctx, sess, extbridge.CmdWashChat, taskID, "", map[string]any{"mode": "check", "expected": prev.url}, 10*time.Second, "豆包")
		if err != nil {
			e.clearExtensionWash(t.BrowserID)
			return extensionWashChat{}, err
		}
		if ack.Reusable && ack.Conversation == prev.url && ack.TabID == prev.tabID {
			return prev, nil
		}
	}
	e.clearExtensionWash(t.BrowserID)
	ready, err := extCall(ctx, sess, extbridge.CmdWashChat, taskID, "", map[string]any{"mode": "ready"}, 10*time.Second, "豆包")
	if err != nil && extbridge.CodeOf(err) != extbridge.ENoTab {
		return extensionWashChat{}, err
	}
	if err == nil && ready.Ready && ready.TabID > 0 {
		return extensionWashChat{sessionID: sess.ID(), tabID: ready.TabID}, nil
	}
	fresh, err := extCall(ctx, sess, extbridge.CmdWashChat, taskID, "", map[string]any{"mode": "fresh"}, 45*time.Second, "豆包")
	if err != nil {
		return extensionWashChat{}, err
	}
	wait, cancel := context.WithTimeout(ctx, extHelloWait)
	defer cancel()
	for {
		ready, err = extCall(wait, sess, extbridge.CmdWashChat, taskID, "", map[string]any{"mode": "ready"}, 3*time.Second, "豆包")
		if err == nil && ready.Ready && fresh.TabID > 0 && ready.TabID == fresh.TabID {
			return extensionWashChat{sessionID: sess.ID(), tabID: fresh.TabID}, nil
		}
		if err != nil && extbridge.CodeOf(err) != extbridge.ENoTab {
			return extensionWashChat{}, err
		}
		select {
		case <-wait.Done():
			return extensionWashChat{}, fmt.Errorf("等待豆包新对话就绪: %w", wait.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (e *Engine) finishExtensionWash(t *Task, chat extensionWashChat, out QianwenRun, success bool) {
	u, err := url.Parse(out.Conversation)
	validURL := err == nil && u.Scheme == "https" && (u.Hostname() == "doubao.com" || strings.HasSuffix(u.Hostname(), ".doubao.com")) && u.User == nil && u.RawQuery == "" && u.Fragment == "" && len(strings.Split(strings.Trim(u.Path, "/"), "/")) == 2 && strings.HasPrefix(u.Path, "/chat/")
	if !success || chat.sessionID == "" || chat.tabID <= 0 || chat.tabID != out.TabID || !validURL {
		e.clearExtensionWash(t.BrowserID)
		return
	}
	if chat.url != out.Conversation {
		chat.url, chat.count = out.Conversation, 0
	}
	n := max(1, len(t.Images))
	chat.count += n
	e.mu.Lock()
	if e.extWashChats == nil {
		e.extWashChats = make(map[int64]extensionWashChat)
	}
	e.extWashChats[t.BrowserID] = chat
	e.mu.Unlock()
	tag := sha256.Sum256([]byte(chat.url))
	if n > 1 {
		logging.Log.Info("#%d 豆包洗图会话 %x：第 %d-%d/%d 张，一次发送 %d 张", t.Index, tag[:6], chat.count-n+1, chat.count, washChatTasks, n)
	} else {
		logging.Log.Info("#%d 豆包洗图会话 %x：第 %d/%d 张", t.Index, tag[:6], chat.count, washChatTasks)
	}
}

func (chat washChat) reusable(url string, attempts int) bool {
	return url != "" && chat.url == url && chat.count > 0 && chat.count < washChatTasks && attempts <= 1
}

func (e *Engine) reuseWashChat(t *Task, sess *cdp.Session) bool {
	if t == nil || sess == nil || !e.opts.Wash || !e.opts.WaitResult || t.Platform != "豆包" || len(t.Images) != 1 || strings.TrimSpace(t.Prompt) == "" || sess.IsEgo() {
		return false
	}
	e.mu.Lock()
	chat := e.washChats[sess]
	e.mu.Unlock()
	if t.Attempts <= 1 && chat.count > 0 && chat.count < washChatTasks &&
		chat.reusable(strategy.ReusableDoubaoChat(sess), t.Attempts) && strategy.PrepareDoubaoTurns(sess) == nil {
		return true
	}
	e.mu.Lock()
	delete(e.washChats, sess)
	e.mu.Unlock()
	return false
}

func (e *Engine) finishWashChat(sess *cdp.Session, success bool) {
	url := ""
	if success {
		url = strategy.ReusableDoubaoChat(sess)
	}
	e.recordWashChat(sess, url)
}

func (e *Engine) recordWashChat(sess *cdp.Session, url string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if url == "" {
		delete(e.washChats, sess)
		return
	}
	if e.washChats == nil {
		e.washChats = make(map[*cdp.Session]washChat)
	}
	chat := e.washChats[sess]
	if chat.url != url {
		chat = washChat{url: url}
	}
	chat.count++
	e.washChats[sess] = chat
}
