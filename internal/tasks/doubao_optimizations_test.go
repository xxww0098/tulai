package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestWashChatReuseAllowsMultiImageBatch(t *testing.T) {
	e := &Engine{}
	sess := &cdp.Session{}
	const u = "https://www.doubao.com/chat/batch-reuse"
	e.recordWashChat(sess, u, 4)
	chat := e.washChats[sess]
	if !chat.reusableFor(u, 1, 6) {
		t.Fatal("4 + 6 <= 10 should allow multi-image chat reuse")
	}
	if chat.reusableFor(u, 1, 7) {
		t.Fatal("4 + 7 > 10 must not reuse the chat")
	}
	if chat.reusableFor(u, 2, 2) {
		t.Fatal("retries must not reuse the chat")
	}
}

func TestPartialVerifiedWashBatchAutoRequeuesMissingOnce(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()

	sources := []string{"01.png", "02.png", "03.png", "04.png"}
	var dispatches [][]string
	var progress Progress
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: sources,
		Wash: true, Concurrency: 1, WaitResult: true, DailyCap: 100,
	}, func(Task) {}, func(p Progress) { progress = p }, func(_ context.Context, task *Task) error {
		dispatches = append(dispatches, slices.Clone(task.Images))
		if len(task.batch) == 4 {
			// 首次批次只返回 1, 3, 4，缺少第 2 张（02.png）
			task.submitted = true
			for _, idx := range []int{0, 2, 3} {
				out := task.batch[idx].Images[0] + ".out"
				task.batch[idx].Results = []string{out}
				task.Results = append(task.Results, out)
			}
			return errors.New("本批发送 4 张，已按源图关联保存 3 张，缺少 1 张结果，请重试未完成源图")
		}
		// 自动单独补跑 02.png 成功
		task.Results = []string{task.Images[0] + ".out"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dispatches) != 2 || len(dispatches[0]) != 4 || !slices.Equal(dispatches[1], []string{"02.png"}) {
		t.Fatalf("expected initial batch of 4 then solo requeue of 02.png, got %v", dispatches)
	}
	if progress.Done != 4 || progress.Total != 4 {
		t.Fatalf("progress mismatch: %+v", progress)
	}
	for _, item := range e.View().Snapshot() {
		if item.Status != StatusSuccess || len(item.Results) != 1 {
			t.Fatalf("item should succeed after auto-requeue: %+v", item)
		}
	}
}

func TestManualHighConcurrencyScalesAboveDefaultTwo(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := range 4 {
		row, err := st.AddBrowser("豆包", fmt.Sprintf("号%d", i+1), "")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetBrowserProxy(row.ID, fmt.Sprintf("http://10.0.0.%d:8080", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	got := overlapStarted(t, st, Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png", "c.png", "d.png", "e.png"}, Concurrency: 4,
	})
	if len(got) != 4 {
		t.Fatalf("手动上限 4 且有 4 个账号时应同时跑 4 条，实际 %v", got)
	}
}

func TestSharedExitKeyIdentifiesSameProxyAccounts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, _ := st.AddBrowser("豆包", "甲", "")
	b, _ := st.AddBrowser("豆包", "乙", "")
	c, _ := st.AddBrowser("豆包", "丙", "")
	_ = st.SetBrowserProxy(a.ID, "http://10.0.0.1:8080")
	_ = st.SetBrowserProxy(b.ID, "http://10.0.0.1:8080")
	_ = st.SetBrowserProxy(c.ID, "http://10.0.0.2:8080")
	e := NewEngine(st, nil, nil, "")
	ka, kb, kc := e.sharedExitKey("豆包", a.ID), e.sharedExitKey("豆包", b.ID), e.sharedExitKey("豆包", c.ID)
	if ka == "" || ka != kb {
		t.Fatalf("accounts sharing proxy must share exit key: a=%q b=%q", ka, kb)
	}
	if kc != "" {
		t.Fatalf("account with unique proxy must not stagger: c=%q", kc)
	}
}

func TestSourceSignatureUsesPerceptualHashAndClearsOnClose(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			v := uint8(30 + (x*190)/256)
			if y > 128 {
				v = 255 - v
			}
			img.SetNRGBA(x, y, color.NRGBA{R: v, G: v / 2, B: 255 - v, A: 255})
		}
	}
	path := filepath.Join(t.TempDir(), "src.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, nil, nil, "")
	sig := e.sourceSignature(path)
	if sig == nil || sig.Flat || sig.Hash == 0 || sig.Pixels != "" {
		t.Fatalf("expected lightweight perceptual signature without pixel SHA256, got %+v", sig)
	}
	e.cacheSourceSignature("virtual-path", img)
	cached := e.sourceSignature("virtual-path")
	if cached == nil || cached.Hash != sig.Hash {
		t.Fatalf("cacheSourceSignature must avoid disk read and match hash: %+v vs %+v", cached, sig)
	}
	e.CloseSessions()
	if _, ok := e.srcSigs.Load(path); ok {
		t.Fatal("CloseSessions must clear srcSigs cache")
	}
}

func TestSameExitStaggerAndRiskBackoff(t *testing.T) {
	if sameExitStaggerFor("豆包") != sameExitStagger {
		t.Fatalf("豆包 stagger = %v, want %v", sameExitStaggerFor("豆包"), sameExitStagger)
	}
	if sameExitStaggerFor("千问") <= sameExitStagger {
		t.Fatalf("千问 stagger = %v, want > %v", sameExitStaggerFor("千问"), sameExitStagger)
	}
	if !taskHitExitRisk(Task{Status: StatusNeedsUser}) {
		t.Fatal("StatusNeedsUser must trigger shared-exit risk backoff")
	}
	if !taskHitExitRisk(Task{Status: StatusFailed, Error: strategy.ErrAccountRate.Error()}) {
		t.Fatal("rate limit error must trigger shared-exit risk backoff")
	}
	if taskHitExitRisk(Task{Status: StatusFailed, Error: "普通网络超时"}) {
		t.Fatal("ordinary non-risk error should not trigger shared-exit risk backoff")
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, _ := st.AddBrowser("千问", "千问1", "")
	b, _ := st.AddBrowser("千问", "千问2", "")
	c, _ := st.AddBrowser("千问", "千问3", "")
	_ = st.SetBrowserProxy(c.ID, "http://10.0.0.9:8080")

	oldCool := sameExitRiskCooldown
	sameExitRiskCooldown = 250 * time.Millisecond
	t.Cleanup(func() { sameExitRiskCooldown = oldCool })

	e := NewEngine(st, nil, nil, "")
	e.opts.SendWait = 1
	sched := &slotSched{notify: make(chan struct{})}
	e.activeSched = sched

	e.noteExitRisk("千问", a.ID)
	now := time.Now()
	keyA := e.sharedExitKey("千问", a.ID)
	keyB := e.sharedExitKey("千问", b.ID)
	keyC := e.sharedExitKey("千问", c.ID)
	if keyA == "" || keyA != keyB || keyC != "" {
		t.Fatalf("unexpected shared exit keys: a=%q b=%q c=%q", keyA, keyB, keyC)
	}
	if until := sched.exitWait(keyB, now); until.IsZero() || until.Sub(now) < 100*time.Millisecond {
		t.Fatalf("sibling account on same exit did not receive backoff: until=%v", until)
	}
}

func TestPrepareInMemoryCandidatesParallelPreservesOrderAndErrors(t *testing.T) {
	makePNG := func(seed uint8) []byte {
		const side = 320
		img := image.NewNRGBA(image.Rect(0, 0, side, side))
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				h := uint32(x*2654435761) ^ uint32(y*2246822519) ^ (uint32(seed) * 3266489917)
				img.SetNRGBA(x, y, color.NRGBA{
					R: uint8(h),
					G: uint8(h >> 8),
					B: uint8(h >> 16),
					A: 255,
				})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	e := &Engine{}
	results := []strategy.Result{
		{Data: makePNG(10)},
		{URL: "https://example.invalid/needs-cdp.png"},
		{Data: []byte("corrupt-not-an-image")},
		{Data: makePNG(40)},
		{Data: makePNG(90)},
	}
	batched := make([][]byte, len(results))
	batched[3] = makePNG(150) // batched overrides inline data

	cands, errs, handled := e.prepareInMemoryCandidates(t.Context(), results, batched)
	if !handled[0] || handled[1] || !handled[2] || !handled[3] || !handled[4] {
		t.Fatalf("unexpected handled mask: %v", handled)
	}
	if errs[0] != nil || cands[0] == nil || errs[3] != nil || cands[3] == nil || errs[4] != nil || cands[4] == nil {
		t.Fatalf("valid candidates failed: errs=%v", errs)
	}
	if errs[2] == nil || cands[2] != nil {
		t.Fatalf("expected corrupt payload to fail: cand=%v err=%v", cands[2], errs[2])
	}
	if cands[0].hash == "" || cands[3].hash == "" || cands[4].hash == "" ||
		cands[0].hash == cands[3].hash || cands[3].hash == cands[4].hash {
		t.Fatalf("expected distinct valid hashes, got %q %q %q", cands[0].hash, cands[3].hash, cands[4].hash)
	}
}
