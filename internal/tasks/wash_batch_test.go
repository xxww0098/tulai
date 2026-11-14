package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestBoundWashDispatchesTenThenOneAndKeepsSources(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	account := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	var sources []string
	for i := 1; i <= 11; i++ {
		sources = append(sources, fmt.Sprintf("%02d.png", i))
	}
	var sizes []int
	var progress Progress
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: sources,
		Wash: true, Concurrency: 1, WaitResult: true, DailyCap: 100,
	}, func(Task) {}, func(p Progress) { progress = p }, func(_ context.Context, task *Task) error {
		sizes = append(sizes, len(task.Images))
		if len(task.batch) > 0 {
			for i := range task.batch {
				task.batch[i].Results = []string{task.batch[i].Images[0] + ".out"}
			}
		} else {
			task.Results = []string{task.Images[0] + ".out"}
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(sizes, []int{10, 1}) || progress.Done != 11 || progress.Total != 11 {
		t.Fatalf("batch sizes=%v progress=%+v err=%v", sizes, progress, err)
	}
	for i, task := range e.View().Snapshot() {
		if task.Status != StatusSuccess || task.BrowserID != account.ID || len(task.Images) != 1 || task.Images[0] != sources[i] || !reflect.DeepEqual(task.Results, []string{sources[i] + ".out"}) {
			t.Fatalf("task %d lost its source or result: %+v", i+1, task)
		}
	}
	if got := browserByID(t, st, account.ID); got.UsedToday(time.Now()) != 11 {
		t.Fatalf("daily count = %d, want 11", got.UsedToday(time.Now()))
	}
}

func TestBoundWashUsesBothIdleAccountsConcurrently(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	addNamed(t, st, "豆包", "甲")
	addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan Task, 2)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- e.RunObserved(ctx, Options{
			Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: []string{"1.png", "2.png", "3.png", "4.png"},
			Wash: true, Concurrency: 2, WaitResult: true,
		}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
			started <- *task
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			for i := range task.batch {
				task.batch[i].Results = []string{task.batch[i].Images[0] + ".out"}
			}
			return nil
		})
	}()
	var batches []Task
	for len(batches) < 2 {
		select {
		case task := <-started:
			batches = append(batches, task)
		case <-ctx.Done():
			t.Fatal("both accounts did not start before either batch finished")
		}
	}
	if batches[0].BrowserID == batches[1].BrowserID || len(batches[0].Images) != 2 || len(batches[1].Images) != 2 {
		t.Fatalf("expected two independent batches of two: %+v", batches)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, task := range e.View().Snapshot() {
		if task.Status != StatusSuccess || len(task.Results) != 1 || task.Results[0] != task.Images[0]+".out" {
			t.Fatalf("source or result lost: %+v", task)
		}
	}
}

func TestParallelWashBatchBalancesOnlyMatchingWork(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	e := NewEngine(st, nil, nil, "")
	e.opts = Options{Wash: true, WaitResult: true}
	for _, tc := range []struct {
		count int
		want  int
	}{{1, 1}, {2, 1}, {4, 2}, {11, 6}, {20, 10}, {21, 10}} {
		var pending []Task
		for i := 0; i < tc.count; i++ {
			pending = append(pending, Task{Platform: "豆包", Prompt: "洗图", Images: []string{fmt.Sprint(i)}})
		}
		if got := e.parallelWashBatchLimit(pending, 0, pending[0], 10); got != tc.want {
			t.Errorf("%d images: got %d, want %d", tc.count, got, tc.want)
		}
	}
	pending := []Task{
		{Platform: "豆包", Prompt: "甲", Images: []string{"1"}},
		{Platform: "豆包", Prompt: "甲", Images: []string{"2"}},
		{Platform: "豆包", Prompt: "乙", Images: []string{"3"}},
	}
	if got := e.parallelWashBatchLimit(pending, 0, pending[0], 10); got != 10 {
		t.Fatalf("another prompt can fill the second lane; batch limit=%d", got)
	}
}

func TestBoundWashKeepsFullBatchWhenConcurrencyUnavailable(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint("disabled=", disabled), func(t *testing.T) {
			st := openRotateStore(t)
			st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
			st.Set(extbridge.SettingExtPort, freePort(t))
			addNamed(t, st, "豆包", "甲")
			second := addNamed(t, st, "豆包", "乙")
			concurrency := 1
			if disabled {
				concurrency = 2
				if err := st.SetBrowserDisabled(second.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			e := NewEngine(st, nil, nil, "")
			defer e.CloseSessions()
			var sizes []int
			err := e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: []string{"1", "2", "3", "4"}, Wash: true, WaitResult: true, Concurrency: concurrency}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
				sizes = append(sizes, len(task.Images))
				for i := range task.batch {
					task.batch[i].Results = []string{"ok"}
				}
				return nil
			})
			if err != nil || !reflect.DeepEqual(sizes, []int{4}) {
				t.Fatalf("sizes=%v err=%v", sizes, err)
			}
		})
	}
}

func TestWashBatchRespectsRemainingDailyAndSwitchLimits(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	account := addNamed(t, st, "豆包", "甲")
	if err := st.RecordUse(account.ID, time.Now(), 5, 0, 0); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, nil, nil, "")
	e.opts = Options{Wash: true, WaitResult: true, DailyCap: 8}
	pending := make([]Task, 6)
	for i := range pending {
		pending[i] = Task{Index: i + 1, Platform: "豆包", BrowserID: account.ID, Images: []string{fmt.Sprint(i)}, Prompt: "洗图"}
	}
	batch, indices := e.bundleWashTasks(pending, 0, pending[0], 10, time.Now())
	if len(batch.Images) != 3 || !reflect.DeepEqual(indices, []int{0, 1, 2}) {
		t.Fatalf("daily cap batch=%v indices=%v", batch.Images, indices)
	}
	batch, _ = e.bundleWashTasks(pending, 0, pending[0], 2, time.Now())
	if len(batch.Images) != 2 {
		t.Fatalf("switch limit batch=%v", batch.Images)
	}
}

func TestUnsentWashBatchDoesNotUseDailyLimit(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	account := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	err := e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Prompts: []string{"洗图"}, Wash: true, WaitResult: true, Concurrency: 1}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		task.noUse = true
		return errors.New("附件未确认")
	})
	b := browserByID(t, st, account.ID)
	_, failed := b.ResultsToday(time.Now())
	if err != nil || b.UsedToday(time.Now()) != 0 || failed != 2 {
		t.Fatalf("unsent batch consumed account limit: used=%d failed=%d err=%v", b.UsedToday(time.Now()), failed, err)
	}
}

func TestSaveWashBatchKeepsMappingAndRejectsMissingResults(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: dir}
	data := [][]byte{resultPNG(t, 81), resultPNG(t, 82)}
	results := []strategy.Result{{Data: data[0], WatermarkRemoved: true}, {Data: data[1], WatermarkRemoved: true}}

	// 无关联或非法关联时，缺失结果不得分配给任何源图。
	unverified := Task{Index: 5, Platform: "豆包", batch: []Task{{Images: []string{"/a/first.png"}}, {Images: []string{"/b/second.png"}}}}
	if err := e.saveWashBatch(context.Background(), &unverified, results[:1], nil); err == nil || len(unverified.Results) != 0 {
		t.Fatalf("unverified missing result must not be assigned: %v %v", unverified.Results, err)
	}

	// 已验证部分结果（1/2 对应第 2 张）：直接保存到第 2 张源图，不写待核对，仅第 1 张失败。
	partialDir := t.TempDir()
	ePartial := NewEngine(nil, nil, nil, "")
	ePartial.opts = Options{Wash: true, OutputPath: partialDir}
	partial := Task{Index: 6, Platform: "豆包", submitted: true, batch: []Task{{Index: 1, Images: []string{"/a/first.png"}}, {Index: 2, Images: []string{"/b/second.png"}}}}
	err := ePartial.saveExtensionWash(context.Background(), &partial, results[1:2], []int{2}, nil)
	if err == nil || !strings.Contains(err.Error(), "已按源图关联保存") || strings.Contains(err.Error(), "待核对") {
		t.Fatalf("unexpected partial wash error: %v", err)
	}
	partial.Status, partial.Error = StatusFailed, err.Error()
	members := batchMembers(partial)
	if members[0].Status != StatusFailed || len(members[0].Results) != 0 {
		t.Fatalf("missing member should remain failed: %+v", members[0])
	}
	if members[1].Status != StatusSuccess || len(members[1].Results) != 1 || filepath.Base(members[1].Results[0]) != "second_01.png" {
		t.Fatalf("matched partial member should succeed: %+v", members[1])
	}
	if got, readErr := os.ReadFile(members[1].Results[0]); readErr != nil || !bytes.Equal(got, data[1]) {
		t.Fatalf("wrong partial data: %v", readErr)
	}
	if files, globErr := filepath.Glob(filepath.Join(partialDir, "待核对", "*", "*", "*.png")); globErr != nil || len(files) != 0 {
		t.Fatalf("verified partial outputs must not be quarantined: %v err=%v", files, globErr)
	}

	// 完整批次：两张都按源图关联保存且返回 nil。
	batch := Task{Platform: "豆包", batch: []Task{{Images: []string{"/a/first.png"}}, {Images: []string{"/b/second.png"}}}}
	if err := e.saveWashBatch(context.Background(), &batch, results, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	for i, item := range batch.batch {
		if len(item.Results) != 1 || filepath.Base(item.Results[0]) != []string{"first_01.png", "second_01.png"}[i] {
			t.Fatalf("wrong path for image %d: %v", i, item.Results)
		}
		if got, err := os.ReadFile(item.Results[0]); err != nil || !bytes.Equal(got, data[i]) {
			t.Fatalf("wrong data for image %d: %v", i, err)
		}
	}
}

func TestUnverifiedWashAssociationsAreQuarantined(t *testing.T) {
	for _, indices := range [][]int{nil, {1, 1}, {0, 2}, {1, 3}} {
		t.Run(fmt.Sprint(indices), func(t *testing.T) {
			e := NewEngine(nil, nil, nil, "")
			e.opts = Options{Wash: true, OutputPath: t.TempDir()}
			task := Task{Index: 7, Platform: "豆包", batch: []Task{{Images: []string{"/a/first.png"}}, {Images: []string{"/b/second.png"}}}}
			results := []strategy.Result{{Data: resultPNG(t, 87), WatermarkRemoved: true}, {Data: resultPNG(t, 88), WatermarkRemoved: true}}
			if err := e.saveExtensionWash(context.Background(), &task, results, indices, nil); err == nil || len(task.Results) != 0 {
				t.Fatalf("unverified association saved against a source: results=%v err=%v", task.Results, err)
			}
			files, err := filepath.Glob(filepath.Join(e.opts.OutputPath, "待核对", "洗图", "*", "*.png"))
			if err != nil || len(files) != 2 {
				t.Fatalf("unverified outputs were not preserved: %v err=%v", files, err)
			}
		})
	}
}

func TestSentWashBatchDoesNotResendAfterFailure(t *testing.T) {
	e := &Engine{opts: Options{RetryCount: 3}}
	task := Task{batch: []Task{{}, {}}, Images: []string{"a", "b"}}
	calls := 0
	got := e.execute(context.Background(), task, func(_ context.Context, t *Task) error {
		calls++
		t.submitted = true
		return errors.New("结果数量不足")
	})
	if calls != 1 || got.Status != StatusFailed {
		t.Fatalf("submitted batch retried: calls=%d task=%+v", calls, got)
	}
}

func TestSentWashBatchMarksQuotaWithoutResending(t *testing.T) {
	st := openRotateStore(t)
	account := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	task := Task{BrowserID: account.ID, Platform: "豆包", batch: []Task{{}, {}}, Images: []string{"a", "b"}}
	calls := 0
	got := e.execute(context.Background(), task, func(_ context.Context, task *Task) error {
		calls++
		task.submitted = true
		return strategy.ErrAccountQuota
	})
	if calls != 1 || got.Status != StatusFailed || browserByID(t, st, account.ID).Health != store.HealthExhausted {
		t.Fatalf("quota should stop the submitted batch and exhaust the account: calls=%d task=%+v", calls, got)
	}
}

func TestUnmatchedWashResultsArePreservedWithoutSourceNames(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: t.TempDir()}
	task := Task{Index: 7, Platform: "豆包", Images: []string{"/a/product.png", "/b/other.png"}}
	data := resultPNG(t, 88)
	results := []strategy.Result{{Data: data, WatermarkRemoved: true}, {Data: data, WatermarkRemoved: true}}
	count, dir, err := e.saveUnmatchedWash(context.Background(), &task, results)
	if err != nil || count != 2 {
		t.Fatalf("preserved=%d dir=%s err=%v", count, dir, err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 || files[0].Name() == "product_01.png" || files[1].Name() == "other_01.png" {
		t.Fatalf("unmatched files=%v err=%v", files, err)
	}
}

func TestSingleWashWithExtraResultDoesNotMislabelIt(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: t.TempDir()}
	task := Task{Index: 11, Platform: "豆包", Images: []string{"/input/phone.png"}}
	results := []strategy.Result{{Data: resultPNG(t, 83), WatermarkRemoved: true}, {Data: resultPNG(t, 84), WatermarkRemoved: true}}
	if err := e.saveExtensionWash(context.Background(), &task, results, nil, nil); err == nil || len(task.Results) != 0 {
		t.Fatalf("extra result must not be assigned to source: results=%v err=%v", task.Results, err)
	}
	files, err := filepath.Glob(filepath.Join(e.opts.OutputPath, "待核对", "洗图", "*", "*.png"))
	if err != nil || len(files) != 2 {
		t.Fatalf("unmatched outputs not preserved: %v err=%v", files, err)
	}
}

func TestSingleWashDoesNotSendWithStaleAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "phone.png")
	if err := os.WriteFile(path, resultPNG(t, 85), 0o600); err != nil {
		t.Fatal(err)
	}
	sends := 0
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload:
			return ackOK(env, map[string]any{"attachments": []string{"stale.png", "phone.png"}})
		case extbridge.CmdSend:
			sends++
			return ackOK(env, nil)
		default:
			return ackOK(env, nil)
		}
	})
	out, err := RunDoubaoExtension(context.Background(), sess, QianwenJob{TaskID: "11", Images: []string{path}, Prompt: "洗图", VerifyAttachments: true})
	if err == nil || out.Submitted || out.sendAttempted || sends != 0 {
		t.Fatalf("stale attachment must block send: out=%+v sends=%d err=%v", out, sends, err)
	}
}

func TestWashSendUncertainStillCountsAsAttempted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phone.png")
	if err := os.WriteFile(path, resultPNG(t, 86), 0o600); err != nil {
		t.Fatal(err)
	}
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload:
			return ackOK(env, map[string]any{"attachments": []string{"phone.png"}})
		case extbridge.CmdSend:
			return ackFail(env, extbridge.ESendUncertain, "发送状态未确认")
		default:
			return ackOK(env, nil)
		}
	})
	out, err := RunDoubaoExtension(context.Background(), sess, QianwenJob{TaskID: "11", Images: []string{path}, Prompt: "洗图", VerifyAttachments: true})
	if err == nil || !out.sendAttempted || out.Submitted {
		t.Fatalf("uncertain send must count as attempted: out=%+v err=%v", out, err)
	}
}

func TestBatchUploadChecksActualAttachmentOrder(t *testing.T) {
	for _, reorder := range []bool{false, true} {
		t.Run(fmt.Sprint(reorder), func(t *testing.T) {
			var names []string
			sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
				var p struct {
					Name   string `json:"fileName"`
					Finish bool   `json:"finish"`
				}
				_ = json.Unmarshal(env.Payload, &p)
				if p.Finish {
					names = append(names, p.Name)
				}
				actual := append([]string(nil), names...)
				if reorder && len(actual) == 2 {
					actual[0], actual[1] = actual[1], actual[0]
				}
				return ackOK(env, map[string]any{"attachments": actual})
			})
			dir := t.TempDir()
			paths := []string{filepath.Join(dir, "a", "same.png"), filepath.Join(dir, "b", "same.png")}
			for _, path := range paths {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := extUpload(context.Background(), sess, QianwenJob{TaskID: "1", IdempotencyPrefix: "batch", Images: paths}, "豆包")
			if (err != nil) != reorder || !reflect.DeepEqual(names, []string{"01-same.png", "02-same.png"}) {
				t.Fatalf("order=%v names=%v err=%v", reorder, names, err)
			}
		})
	}
}

func TestWashUploadNamesDistinguishBatchesAndSameNamedFiles(t *testing.T) {
	job := QianwenJob{IdempotencyPrefix: "run-1:1", Images: []string{"/a/same.png", "/b/same.png"}, VerifyAttachments: true}
	first, second := extUploadName(job, 0), extUploadName(job, 1)
	job.IdempotencyPrefix = "run-2:1"
	if first == second || first == extUploadName(job, 0) || filepath.Ext(first) != ".png" {
		t.Fatalf("uploads are not uniquely named: %q %q", first, second)
	}
}

func TestDoubaoBatchPromptDistinguishesReusedBatches(t *testing.T) {
	job := QianwenJob{IdempotencyPrefix: "run:1", Prompt: "分别洗图", Images: []string{"/a/same.png", "/b/same.png"}, VerifyAttachments: true}
	first := doubaoBatchPrompt(job)
	oldName := extUploadName(job, 0)
	job.IdempotencyPrefix = "run:2"
	second := doubaoBatchPrompt(job)
	if first == second || strings.Contains(second, oldName) || !strings.HasPrefix(second, job.Prompt) {
		t.Fatalf("reused batch prompt retained old source: %q", second)
	}
	for i := range job.Images {
		if !strings.Contains(second, extUploadName(job, i)) {
			t.Fatalf("batch prompt omitted source %d: %q", i, second)
		}
	}
}

func TestBatchBridgeUploadsOnceAndSavesTenMatchingResults(t *testing.T) {
	dir := t.TempDir()
	var paths, names []string
	var replies []map[string]any
	data := map[string][]byte{}
	for i := 0; i < 10; i++ {
		path := filepath.Join(dir, fmt.Sprintf("source-%02d.png", i))
		if err := os.WriteFile(path, resultPNG(t, byte(i+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		url := fmt.Sprintf("https://cdn.example/raw-%02d.png", i)
		replies = append(replies, map[string]any{"url": url, "rawUrl": url, "sourceIndex": i + 1})
		data[url] = resultPNG(t, byte(i+50))
	}
	for i, j := 0, len(replies)-1; i < j; i, j = i+1, j-1 {
		replies[i], replies[j] = replies[j], replies[i]
	}
	sends := 0
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		if name == extbridge.CmdSetPrompt || name == extbridge.CmdSend || name == extbridge.CmdWaitResult {
			var p struct {
				Prompt string `json:"prompt"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			for _, uploaded := range names {
				if !strings.Contains(p.Prompt, uploaded) {
					t.Errorf("%s prompt omitted current attachment %q", name, uploaded)
				}
			}
		}
		switch name {
		case extbridge.CmdUpload:
			var p struct {
				Name   string `json:"fileName"`
				Finish bool   `json:"finish"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			if p.Finish {
				names = append(names, p.Name)
			}
			return ackOK(env, map[string]any{"attachments": names})
		case extbridge.CmdSend:
			sends++
			return ackOK(env, nil)
		case extbridge.CmdCaptchaAssist, extbridge.CmdSetPrompt:
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			var p struct {
				Expected []string `json:"expectedAttachments"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			if !reflect.DeepEqual(p.Expected, names) {
				t.Errorf("wait must identify the actual uploads: got %v want %v", p.Expected, names)
			}
			return ackOK(env, map[string]any{"images": replies})
		case extbridge.CmdDownload:
			var p struct {
				URL    string `json:"url"`
				Offset int    `json:"offset"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			return chunkAck(env, data[p.URL], p.Offset)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	out, err := RunDoubaoExtension(context.Background(), sess, QianwenJob{TaskID: "1", IdempotencyPrefix: "batch", Images: paths, Prompt: "分别洗图", Wait: time.Second, VerifyAttachments: true})
	if err != nil || !out.Submitted || sends != 1 || len(out.Results) != 10 {
		t.Fatalf("send=%d results=%d err=%v", sends, len(out.Results), err)
	}
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: filepath.Join(dir, "out")}
	task := Task{Platform: "豆包"}
	for _, path := range paths {
		task.batch = append(task.batch, Task{Images: []string{path}})
	}
	if err := e.saveWashBatch(context.Background(), &task, out.Results, out.sourceIndices); err != nil {
		t.Fatal(err)
	}
	for i, item := range task.batch {
		got, err := os.ReadFile(item.Results[0])
		if err != nil || !bytes.Equal(got, data[fmt.Sprintf("https://cdn.example/raw-%02d.png", i)]) {
			t.Fatalf("source %d got the wrong output: %v", i, err)
		}
	}
}

func TestSentWashBatchPreservesUnmatchedAndUncertainOutcome(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: t.TempDir(), RetryCount: 2}
	img := resultPNG(t, 91)

	unmatchedTask := Task{
		Index:    1,
		Platform: "豆包",
		Images:   []string{"/in/a.png", "/in/b.png"},
		batch:    []Task{{Images: []string{"/in/a.png"}}, {Images: []string{"/in/b.png"}}},
	}
	gotUnmatched := e.execute(context.Background(), unmatchedTask, func(ctx context.Context, task *Task) error {
		task.submitted = true
		return e.saveExtensionWash(ctx, task, []strategy.Result{{Data: img, WatermarkRemoved: true}}, nil, nil)
	})
	if gotUnmatched.Status != StatusFailed || strings.Contains(gotUnmatched.Error, "发送状态未确认") || !strings.Contains(gotUnmatched.Error, "未匹配结果保存在 ") {
		t.Fatalf("unmatched outcome overwritten: %+v", gotUnmatched)
	}
	if hint := ClassifyFailure(gotUnmatched.Status, gotUnmatched.Error); hint.Class != FailUnmatched || hint.Button != "查看待核对" {
		t.Fatalf("unexpected unmatched classification: %+v (err=%q)", hint, gotUnmatched.Error)
	}

	uncertainTask := Task{
		Index:    2,
		Platform: "豆包",
		Images:   []string{"/in/c.png", "/in/d.png"},
		batch:    []Task{{Images: []string{"/in/c.png"}}, {Images: []string{"/in/d.png"}}},
	}
	gotUncertain := e.execute(context.Background(), uncertainTask, func(_ context.Context, task *Task) error {
		task.submitted = true
		return errors.New("E_NO_TAB: Could not establish connection. Receiving end does not exist.")
	})
	if gotUncertain.Status != StatusFailed || !strings.Contains(gotUncertain.Error, "发送状态未确认") {
		t.Fatalf("uncertain post-send failure missing confirmation hint: %+v", gotUncertain)
	}
	if hint := ClassifyFailure(gotUncertain.Status, gotUncertain.Error); hint.Button != "核对后重试" {
		t.Fatalf("unexpected uncertain classification: %+v (err=%q)", hint, gotUncertain.Error)
	}

	modTask := Task{
		Index:    3,
		Platform: "豆包",
		Images:   []string{"/in/e.png", "/in/f.png"},
		batch:    []Task{{Images: []string{"/in/e.png"}}, {Images: []string{"/in/f.png"}}},
	}
	gotMod := e.execute(context.Background(), modTask, func(_ context.Context, task *Task) error {
		task.submitted = true
		return mapExtErr(&extbridge.ProtoError{Code: extbridge.EModeration, Message: "审核不通过: 违规内容"}, extAck{}, "豆包")
	})
	if gotMod.Status != StatusFailed || strings.Contains(gotMod.Error, "发送状态未确认") {
		t.Fatalf("moderation failure mislabeled: %+v", gotMod)
	}
	if hint := ClassifyFailure(gotMod.Status, gotMod.Error); hint.Class != FailModeration || hint.Button != "改提示词重试" {
		t.Fatalf("unexpected moderation classification: %+v (err=%q)", hint, gotMod.Error)
	}
}

func TestPartialVerifiedWashBatchSavesMatchedWithoutQuarantine(t *testing.T) {
	dir := t.TempDir()
	inDir := filepath.Join(dir, "in")
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(inDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(inDir, fmt.Sprintf("src-%d.png", i+1))
		if err := os.WriteFile(p, resultPNG(t, byte(i+10)), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	data := map[string][]byte{
		"https://cdn.example/raw-3.png": resultPNG(t, 93),
		"https://cdn.example/raw-1.png": resultPNG(t, 91),
	}
	var names []string
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload:
			var p struct {
				Name   string `json:"fileName"`
				Finish bool   `json:"finish"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			if p.Finish {
				names = append(names, p.Name)
			}
			return ackOK(env, map[string]any{"attachments": names})
		case extbridge.CmdSetPrompt, extbridge.CmdSend, extbridge.CmdCaptchaAssist:
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{
				"conversation": "https://www.doubao.com/chat/partial-1",
				"images": []map[string]any{
					{"url": "https://cdn.example/raw-3.png", "rawUrl": "https://cdn.example/raw-3.png", "sourceIndex": 3},
					{"url": "https://cdn.example/raw-1.png", "rawUrl": "https://cdn.example/raw-1.png", "sourceIndex": 1},
				},
			})
		case extbridge.CmdDownload:
			var p struct {
				URL    string `json:"url"`
				Offset int    `json:"offset"`
			}
			_ = json.Unmarshal(env.Payload, &p)
			return chunkAck(env, data[p.URL], p.Offset)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})

	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, WaitResult: true, OutputPath: outDir, RetryCount: 1}
	batch := Task{
		Index:    1,
		Platform: "豆包",
		Prompt:   "分别洗图",
		Images:   append([]string(nil), paths...),
		batch: []Task{
			{Index: 1, Platform: "豆包", Prompt: "分别洗图", Images: []string{paths[0]}},
			{Index: 2, Platform: "豆包", Prompt: "分别洗图", Images: []string{paths[1]}},
			{Index: 3, Platform: "豆包", Prompt: "分别洗图", Images: []string{paths[2]}},
		},
	}
	done := e.execute(context.Background(), batch, func(ctx context.Context, task *Task) error {
		out, err := RunDoubaoExtension(ctx, sess, QianwenJob{
			TaskID:            "1",
			IdempotencyPrefix: "partial",
			Images:            task.Images,
			Prompt:            task.Prompt,
			Wait:              time.Second,
			VerifyAttachments: true,
		})
		task.submitted = task.submitted || out.Submitted || out.sendAttempted
		return e.saveExtensionWash(ctx, task, out.Results, out.sourceIndices, err)
	})
	if done.Status != StatusFailed || !strings.Contains(done.Error, "已按源图关联保存") || strings.Contains(done.Error, "发送状态未确认") {
		t.Fatalf("unexpected batch result: %+v", done)
	}
	members := batchMembers(done)
	if len(members) != 3 {
		t.Fatalf("members len = %d", len(members))
	}
	if members[0].Status != StatusSuccess || len(members[0].Results) != 1 || filepath.Base(members[0].Results[0]) != "src-1_01.png" {
		t.Fatalf("first member should succeed: %+v", members[0])
	}
	if members[1].Status != StatusFailed || len(members[1].Results) != 0 {
		t.Fatalf("missing second member should fail: %+v", members[1])
	}
	if hint := ClassifyFailure(members[1].Status, members[1].Error); hint.Class != FailUnknown || !strings.Contains(hint.Sentence, "少返回") {
		t.Fatalf("unexpected partial failure hint: %+v (err=%q)", hint, members[1].Error)
	}
	if members[2].Status != StatusSuccess || len(members[2].Results) != 1 || filepath.Base(members[2].Results[0]) != "src-3_01.png" {
		t.Fatalf("third member should succeed: %+v", members[2])
	}
	if _, err := os.Stat(filepath.Join(outDir, "待核对")); !os.IsNotExist(err) {
		t.Fatalf("待核对 directory should not be created for verified partial saves, stat err=%v", err)
	}
}

func TestModerationBatchSplitsAndIsolatesGuiltyImage(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()

	sources := []string{"01.png", "02.png", "bad.png", "04.png", "05.png", "06.png"}
	var dispatches []int
	var progress Progress
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: sources,
		Wash: true, Concurrency: 1, WaitResult: true, DailyCap: 100,
	}, func(Task) {}, func(p Progress) { progress = p }, func(_ context.Context, task *Task) error {
		if !task.Rewritten {
			dispatches = append(dispatches, len(task.Images))
		}
		for _, img := range task.Images {
			if img == "bad.png" {
				task.submitted = true
				return mapExtErr(&extbridge.ProtoError{Code: extbridge.EModeration, Message: "审核不通过"}, extAck{}, "豆包")
			}
		}
		if len(task.batch) > 0 {
			for i := range task.batch {
				out := task.batch[i].Images[0] + ".out"
				task.batch[i].Results = []string{out}
				task.Results = append(task.Results, out)
			}
		} else {
			task.Results = []string{task.Images[0] + ".out"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dispatches, []int{6, 3, 1, 1, 1, 3}) {
		t.Fatalf("expected binary split batches [6 3 1 1 1 3], got %v", dispatches)
	}
	if progress.Done != 6 || progress.Total != 6 {
		t.Fatalf("unexpected progress: %+v", progress)
	}
	snap := e.View().Snapshot()
	for i, item := range snap {
		if sources[i] == "bad.png" {
			if item.Status != StatusFailed || ClassifyFailure(item.Status, item.Error).Class != FailModeration {
				t.Fatalf("guilty image #%d should fail with moderation: %+v", i+1, item)
			}
			continue
		}
		if item.Status != StatusSuccess || !reflect.DeepEqual(item.Results, []string{sources[i] + ".out"}) {
			t.Fatalf("healthy image #%d poisoned by batch moderation: %+v", i+1, item)
		}
	}
}

func TestPromptWideModerationStopsSplittingAfterThreeSoloFails(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()

	sources := []string{"01.png", "02.png", "03.png", "04.png", "05.png", "06.png"}
	var dispatches []int
	var progress Progress
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"整批都过不了审"}, Images: sources,
		Wash: true, Concurrency: 1, WaitResult: true, DailyCap: 100,
	}, func(Task) {}, func(p Progress) { progress = p }, func(_ context.Context, task *Task) error {
		if !task.Rewritten {
			dispatches = append(dispatches, len(task.Images))
		}
		task.submitted = true
		return mapExtErr(&extbridge.ProtoError{Code: extbridge.EModeration, Message: "审核不通过"}, extAck{}, "豆包")
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dispatches, []int{6, 3, 1, 1, 1, 3}) {
		t.Fatalf("should stop splitting after 3 solo moderation fails without saves, got %v", dispatches)
	}
	if progress.Done != 6 || progress.Total != 6 {
		t.Fatalf("unexpected progress: %+v", progress)
	}
	for i, item := range e.View().Snapshot() {
		if item.Status != StatusFailed || ClassifyFailure(item.Status, item.Error).Class != FailModeration {
			t.Fatalf("item #%d should end with moderation failure: %+v", i+1, item)
		}
	}
}

func TestSoloImagesAndRebuildRetryPreventRebundling(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()

	var batches [][]string
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"洗图"},
		Images:     []string{"/dir/a.png", "/dir/bad.png", "/dir/b.png", "/dir/c.png"},
		SoloImages: []string{"/dir/../dir/bad.png"},
		Wash:       true, Concurrency: 1, WaitResult: true, DailyCap: 100,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		batches = append(batches, append([]string(nil), task.Images...))
		if len(task.batch) > 0 {
			for i := range task.batch {
				out := task.batch[i].Images[0] + ".out"
				task.batch[i].Results = []string{out}
				task.Results = append(task.Results, out)
			}
		} else {
			task.Results = []string{task.Images[0] + ".out"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantBatches := [][]string{
		{"/dir/a.png", "/dir/b.png", "/dir/c.png"},
		{"/dir/bad.png"},
	}
	if !reflect.DeepEqual(batches, wantBatches) {
		t.Fatalf("SoloImages bundles mismatch: got %v want %v", batches, wantBatches)
	}

	retryItems := []Task{
		{Index: 1, Platform: "豆包", Prompt: "洗图", Images: []string{"/dir/ok.png"}, Status: StatusSuccess},
		{Index: 2, Platform: "豆包", Prompt: "洗图", Images: []string{"/dir/bad.png"}, Status: StatusFailed, Error: "E_MODERATION: 审核不通过"},
		{Index: 3, Platform: "豆包", Prompt: "洗图", Images: []string{"/dir/t1.png"}, Status: StatusFailed, Error: "出图超时"},
		{Index: 4, Platform: "豆包", Prompt: "洗图", Images: []string{"/dir/t2.png"}, Status: StatusSkipped, Error: "已停止"},
	}
	rebuilt := RebuildRetry(retryItems)
	if len(rebuilt) != 3 {
		t.Fatalf("rebuilt len = %d", len(rebuilt))
	}
	if rebuilt[0].maxBatch != 1 || rebuilt[1].maxBatch != 0 || rebuilt[2].maxBatch != 0 {
		t.Fatalf("unexpected maxBatch on rebuilt items: [%d %d %d]", rebuilt[0].maxBatch, rebuilt[1].maxBatch, rebuilt[2].maxBatch)
	}

	var retryBatches [][]string
	err = e.retry(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"洗图"},
		Wash: true, Concurrency: 1, WaitResult: true, DailyCap: 100,
	}, retryItems, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		retryBatches = append(retryBatches, append([]string(nil), task.Images...))
		if len(task.batch) > 0 {
			for i := range task.batch {
				out := task.batch[i].Images[0] + ".out"
				task.batch[i].Results = []string{out}
				task.Results = append(task.Results, out)
			}
		} else {
			task.Results = []string{task.Images[0] + ".out"}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRetryBatches := [][]string{
		{"/dir/bad.png"},
		{"/dir/t1.png", "/dir/t2.png"},
	}
	if !reflect.DeepEqual(retryBatches, wantRetryBatches) {
		t.Fatalf("RebuildRetry bundles mismatch: got %v want %v", retryBatches, wantRetryBatches)
	}
}
