package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/chatgpt"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestChatGPTRetryKeepsOriginalAccountAndResult(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	owner, _ := st.AddBrowser(chatgpt.Platform, "原账号", t.TempDir())
	other, _ := st.AddBrowser(chatgpt.Platform, "空闲账号", t.TempDir())
	st.ConfirmLogin(owner.ID)
	st.ConfirmLogin(other.ID)
	st.UpdateBrowserHealth(owner.ID, store.HealthExhausted, time.Now().Add(time.Hour).Unix(), "额度已用尽")
	g := &chatgpt.Generation{Attempted: true, Submitted: true, ConversationID: "original-conversation"}
	failed := Task{Platform: chatgpt.Platform, BrowserID: owner.ID, Prompt: "猫", Status: StatusFailed, submitted: true,
		chatGPT: g, Results: []string{"already-saved.png"}, savedHashes: map[string]bool{"hash": true}}
	e := NewEngine(st, nil, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	called := 0
	err = e.retry(ctx, Options{Platforms: []string{chatgpt.Platform}}, []Task{failed}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		called++
		if task.BrowserID != owner.ID || !task.submitted || task.chatGPT != g || !task.noUse || len(task.Results) != 1 || !task.savedHashes["hash"] {
			t.Fatalf("retry lost its original generation: %+v", task)
		}
		return nil
	})
	if err != nil || called != 1 {
		t.Fatalf("called=%d error=%v", called, err)
	}
	if st.Browsers()[0].DayCount != 0 {
		t.Fatal("result polling consumed another daily attempt")
	}
	if len(e.laneRows()) != 2 {
		t.Fatal("ChatGPT accounts missing from account board")
	}
}

func TestChatGPTUncertainAndAcceptedTasksNeverRotate(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first, _ := st.AddBrowser(chatgpt.Platform, "first", "")
	second, _ := st.AddBrowser(chatgpt.Platform, "second", "")
	st.ConfirmLogin(first.ID)
	st.ConfirmLogin(second.ID)
	e := NewEngine(st, nil, nil, "")
	e.opts.RetryCount = 2
	called := 0
	got := e.execute(context.Background(), Task{Platform: chatgpt.Platform, BrowserID: first.ID}, func(_ context.Context, task *Task) error {
		called++
		task.chatGPT = &chatgpt.Generation{Attempted: true}
		return chatGPTError(chatgpt.ErrSubmissionUncertain)
	})
	if called != 1 || got.Status != StatusFailed || got.BrowserID != first.ID || got.rotated {
		t.Fatalf("uncertain task resent: %+v calls=%d", got, called)
	}
	accepted := Task{Platform: chatgpt.Platform, BrowserID: first.ID, submitted: true}
	if e.maybeRotate(&accepted, strategy.ErrAccountQuota, time.Now()) || accepted.BrowserID != first.ID {
		t.Fatal("accepted task changed account")
	}
}

func TestChatGPTAccountErrorsUseExistingScheduler(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind faultKind
	}{
		{chatgpt.ErrNeedLogin, faultAuth}, {chatgpt.ErrQuota, faultQuota}, {chatgpt.ErrRate, faultRate},
		{chatgpt.ErrVerification, faultCaptcha}, {chatgpt.ErrSubmissionUncertain, faultUncertain},
	} {
		got := chatGPTError(tc.err)
		if classifyFault(got) != tc.kind || !errors.Is(got, tc.err) {
			t.Fatalf("%v -> %v", tc.err, got)
		}
	}
	if !errors.Is(chatGPTError(chatgpt.ErrVerification), captcha.ErrVerificationRequired) {
		t.Fatal("human verification path missing")
	}
}
