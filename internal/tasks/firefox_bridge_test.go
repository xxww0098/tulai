package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
)

func TestBoundFirefoxBridgeUsesExistingAccounts(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	if _, err := st.UpsertExtensionAccount("豆包", "external", "外部"); err != nil {
		t.Fatal(err)
	}
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	e := NewEngine(st, nil, nil, "")
	if !e.useExtension("豆包") || !e.useBoundExtension("豆包") {
		t.Fatal("bound extension mode is not active")
	}
	ids := e.browsersOfPlatforms()["豆包"]
	if len(ids) != 2 || ids[0] != first.ID || ids[1] != second.ID {
		t.Fatalf("bound account ids=%v", ids)
	}
	if key := e.extensionAccountKey(first.ID); key != "tulai-bound-"+itoa(int(first.ID)) {
		t.Fatalf("bound key=%q", key)
	}
	e.applyExtensionLogin("豆包", e.extensionAccountKey(first.ID), "甲", true)
	if len(st.Browsers()) != 3 {
		t.Fatal("bound login must not create another account")
	}
	if len(e.accountsFor("豆包")) != 2 || len(e.laneRows()) != 2 {
		t.Fatal("scheduler must use the bound rows")
	}
}

func TestBoundFirefoxWashChatGroupsAndAccountIsolation(t *testing.T) {
	type fakePage struct {
		mu           sync.Mutex
		conversation string
		groups       int
		fresh        int
		tab          int
	}
	newPage := func(tab int) (*fakePage, *extbridge.Session) {
		page := &fakePage{tab: tab}
		sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
			page.mu.Lock()
			defer page.mu.Unlock()
			var body struct{ Mode, Expected string }
			_ = json.Unmarshal(env.Payload, &body)
			switch body.Mode {
			case "ready":
				return ackOK(env, map[string]any{"ready": page.conversation == "", "tabId": page.tab})
			case "fresh":
				page.conversation = ""
				page.fresh++
				return ackOK(env, map[string]any{"tabId": page.tab})
			case "check":
				return ackOK(env, map[string]any{"reusable": page.conversation == body.Expected, "conversation": page.conversation, "tabId": page.tab})
			default:
				return ackFail(env, extbridge.EProtocol, "unexpected mode")
			}
		})
		return page, sess
	}
	first, sess := newPage(7)
	second, other := newPage(8)
	e := &Engine{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstURL := ""
	for i := 1; i <= 12; i++ {
		page, session, account := first, sess, int64(1)
		if i == 12 {
			page, session, account = second, other, 2
		}
		task := &Task{Index: i, BrowserID: account, Attempts: 1}
		chat, err := e.prepareExtensionWash(ctx, session, task)
		if err != nil {
			t.Fatal(err)
		}
		page.mu.Lock()
		if page.conversation == "" {
			page.groups++
			page.conversation = fmt.Sprintf("https://www.doubao.com/chat/account-%d-group-%d", account, page.groups)
		}
		current, fresh, tab := page.conversation, page.fresh, page.tab
		page.mu.Unlock()
		e.finishExtensionWash(task, chat, QianwenRun{Conversation: current, TabID: tab}, true)
		if i == 1 {
			firstURL = current
		}
		if i <= 10 && (current != firstURL || e.extWashChats[1].count != i) {
			t.Fatalf("task %d did not reuse first group", i)
		}
		if i == 11 && (current == firstURL || e.extWashChats[1].count != 1 || fresh != 1) {
			t.Fatal("task 11 must start a new group")
		}
	}
	if e.extWashChats[2].count != 1 || e.extWashChats[1].count != 1 || e.extWashChats[1].url == e.extWashChats[2].url {
		t.Fatal("accounts must keep separate conversation state")
	}
	e.finishExtensionWash(&Task{BrowserID: 1}, e.extWashChats[1], QianwenRun{}, false)
	if _, ok := e.extWashChats[1]; ok {
		t.Fatal("failed task must discard the conversation")
	}
}

func TestBoundFirefoxWaitsForPageBeforeSending(t *testing.T) {
	calls := 0
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		calls++
		name, _ := env.Name()
		if name != extbridge.CmdCaptchaAssist {
			return ackFail(env, extbridge.EProtocol, "unexpected command")
		}
		if calls < 3 {
			return ackFail(env, extbridge.ENoTab, "content script not ready")
		}
		return ackOK(env, nil)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := waitBoundPage(ctx, sess, "1", "豆包"); err != nil || calls != 3 {
		t.Fatalf("wait err=%v, calls=%d", err, calls)
	}
}

func TestConcurrentBoundAccountsShareBridgeListener(t *testing.T) {
	st := openTaskStore(t)
	st.Set(extbridge.SettingExtPort, freePort(t))
	st.Set(extbridge.SettingExtToken, "test-token")
	e := NewEngine(st, nil, nil, "")
	t.Cleanup(func() { e.CloseSessions() })
	type result struct {
		srv *extbridge.Server
		err error
	}
	const accounts = 24
	start := make(chan struct{})
	results := make(chan result, accounts)
	for i := 0; i < accounts; i++ {
		go func() {
			<-start
			srv, err := e.extensionServer()
			results <- result{srv, err}
		}()
	}
	close(start)
	var first *extbridge.Server
	for i := 0; i < accounts; i++ {
		got := <-results
		if got.err != nil || got.srv == nil || (first != nil && got.srv != first) {
			t.Fatalf("bridge listener=%v err=%v", got.srv, got.err)
		}
		first = got.srv
	}
}
