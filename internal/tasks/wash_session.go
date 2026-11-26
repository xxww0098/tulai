package tasks

import (
	"strings"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/strategy"
)

const washChatTasks = 10

type washChat struct {
	url   string
	count int
}

func (chat washChat) reusable(url string, attempts int) bool {
	return chat.reusableFor(url, attempts, 1)
}

func (chat washChat) reusableFor(url string, attempts, n int) bool {
	return url != "" && chat.url == url && chat.count > 0 && chat.count+max(1, n) <= washChatTasks && attempts <= 1
}

func (e *Engine) reuseWashChat(t *Task, sess *cdp.Session) bool {
	if t == nil || sess == nil || !e.opts.Wash || !e.opts.WaitResult || t.Platform != "豆包" || len(t.Images) == 0 || strings.TrimSpace(t.Prompt) == "" {
		return false
	}
	e.mu.Lock()
	chat := e.washChats[sess]
	e.mu.Unlock()
	if t.Attempts <= 1 && chat.count > 0 && chat.count+len(t.Images) <= washChatTasks &&
		chat.reusableFor(strategy.ReusableDoubaoChat(sess), t.Attempts, len(t.Images)) && strategy.PrepareDoubaoTurns(sess) == nil {
		return true
	}
	e.mu.Lock()
	delete(e.washChats, sess)
	e.mu.Unlock()
	return false
}

func (e *Engine) finishWashChat(sess *cdp.Session, count int, success bool) {
	url := ""
	if success {
		url = strategy.ReusableDoubaoChat(sess)
	}
	e.recordWashChat(sess, url, count)
}

func (e *Engine) recordWashChat(sess *cdp.Session, url string, count int) {
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
	chat.count += count
	e.washChats[sess] = chat
}
