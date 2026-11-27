package ui

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestProbeDoubaoImageCapabilityMarksDegradedAndRecoversHealthy(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d1, err := st.AddBrowser("豆包", "用户923101", filepath.Join(t.TempDir(), "d1"))
	if err != nil {
		t.Fatal(err)
	}
	d2, err := st.AddBrowser("豆包", "正常豆包", filepath.Join(t.TempDir(), "d2"))
	if err != nil {
		t.Fatal(err)
	}
	d3, err := st.AddBrowser("豆包", "温总1", filepath.Join(t.TempDir(), "d3"))
	if err != nil {
		t.Fatal(err)
	}
	q1, err := st.AddBrowser("千问", "千问号", filepath.Join(t.TempDir(), "q1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(d1.ID)
	st.ConfirmLogin(d2.ID)
	st.ConfirmLogin(d3.ID)
	st.ConfirmLogin(q1.ID)
	// 先把 d2 置为旧的「生图受限」，验证手动检测通过后能自动恢复为「就绪」。
	st.UpdateBrowserHealth(d2.ID, store.HealthExhausted, time.Now().Add(4*time.Hour).Unix(), strategy.ErrAccountImageDegraded.Error())

	app := NewApp(st, nil, nil, "")
	app.StartActor()
	defer app.StopActor()

	var probed []int64
	var mu sync.Mutex
	app.probeDoubaoImageFn = func(_ context.Context, b store.Browser) doubaoImageProbeOutcome {
		mu.Lock()
		probed = append(probed, b.ID)
		mu.Unlock()
		switch b.ID {
		case d1.ID:
			return doubaoImageProbeOutcome{
				Status:  doubaoProbeDegraded,
				Message: strategy.ErrAccountImageDegraded.Error() + "（flow.agent.shell）",
				Route:   "flow.agent.shell",
			}
		case d2.ID:
			return doubaoImageProbeOutcome{
				Status:  doubaoProbeOK,
				Message: "生图能力正常",
				Images:  2,
				Route:   "flow.agent.creation",
			}
		case d3.ID:
			return doubaoImageProbeOutcome{
				Status:  doubaoProbeLoggedOut,
				Message: "发送消息后登录态失效（被服务端退出登录），请重新登录",
			}
		default:
			t.Errorf("非豆包账号不应进入豆包生图能力检测: %+v", b)
			return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "unexpected"}
		}
	}

	svc := &BrowserService{app: app}
	// 传入包含千问的混合 ID，服务端应仅过滤出豆包账号并逐个检测。
	if err := svc.ProbeDoubaoImageCapability(context.Background(), []int64{d1.ID, d2.ID, d3.ID, q1.ID}); err != nil {
		t.Fatalf("ProbeDoubaoImageCapability failed: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := svc.GetBrowserState(context.Background())
		if !state.BatchActive {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	state := svc.GetBrowserState(context.Background())
	if state.BatchActive {
		t.Fatal("expected probe batch to finish")
	}

	mu.Lock()
	gotProbed := append([]int64(nil), probed...)
	mu.Unlock()
	if len(gotProbed) != 3 || gotProbed[0] != d1.ID || gotProbed[1] != d2.ID || gotProbed[2] != d3.ID {
		t.Fatalf("unexpected probed order/ids: %v", gotProbed)
	}

	rowByID := map[int64]BrowserRowDTO{}
	for _, r := range state.Rows {
		rowByID[r.ID] = r
	}
	if r := rowByID[d1.ID]; r.Health != "生图受限" || r.HealthTone != "danger" || !strings.Contains(r.LastError, "flow.agent.shell") {
		t.Fatalf("d1 should be marked 生图受限: %+v", r)
	}
	if r := rowByID[d2.ID]; (r.Health != "就绪" && r.Health != "运行中") || r.HealthTone != "success" || r.LastError != "" {
		t.Fatalf("d2 should recover to healthy after OK probe: %+v", r)
	}
	if r := rowByID[d3.ID]; r.IsLoggedIn || r.Health != "未登录" || !strings.Contains(r.LastError, "退出登录") {
		t.Fatalf("d3 should be marked 未登录 after logout kick: %+v", r)
	}
}

func TestProbeDoubaoImageCapabilitySupportsCancelAndMutualExclusion(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d1, _ := st.AddBrowser("豆包", "豆包1", filepath.Join(t.TempDir(), "d1"))
	d2, _ := st.AddBrowser("豆包", "豆包2", filepath.Join(t.TempDir(), "d2"))
	st.ConfirmLogin(d1.ID)
	st.ConfirmLogin(d2.ID)

	app := NewApp(st, nil, nil, "")
	app.StartActor()
	defer app.StopActor()

	entered := make(chan struct{})
	app.probeDoubaoImageFn = func(ctx context.Context, _ store.Browser) doubaoImageProbeOutcome {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return doubaoImageProbeOutcome{Status: doubaoProbeFailed, Message: "已停止"}
	}

	svc := &BrowserService{app: app}
	if err := svc.ProbeDoubaoImageCapability(context.Background(), []int64{d1.ID, d2.ID}); err != nil {
		t.Fatalf("start probe: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start")
	}

	// 检测进行中，状态同步与历史清理必须互斥拒绝。
	if err := svc.SyncAccountStatus(context.Background(), []int64{d1.ID}); err == nil || !strings.Contains(err.Error(), "检测豆包生图能力") {
		t.Fatalf("expected SyncAccountStatus mutual exclusion error, got: %v", err)
	}
	if err := svc.ClearBrowserHistory(context.Background(), []int64{d1.ID}, 0, true); err == nil || !strings.Contains(err.Error(), "检测豆包生图能力") {
		t.Fatalf("expected ClearBrowserHistory mutual exclusion error, got: %v", err)
	}
	if st := svc.GetBrowserState(context.Background()); !st.BatchActive || st.BatchKind != "probing_image" || !strings.Contains(st.BatchText, "正在检测豆包生图能力") {
		t.Fatalf("unexpected active probe batch state: %+v", st)
	}

	svc.StopProbeDoubaoImageCapability(context.Background())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !svc.GetBrowserState(context.Background()).BatchActive {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("expected probe batch to stop promptly after cancel")
}
