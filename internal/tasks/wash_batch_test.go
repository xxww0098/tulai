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
	batch := Task{Platform: "豆包", batch: []Task{{Images: []string{"/a/first.png"}}, {Images: []string{"/b/second.png"}}}}
	data := [][]byte{resultPNG(t, 81), resultPNG(t, 82)}
	results := []strategy.Result{{Data: data[0], WatermarkRemoved: true}, {Data: data[1], WatermarkRemoved: true}}
	if err := e.saveWashBatch(context.Background(), &batch, results[:1], []int{1}); err == nil || len(batch.Results) != 0 {
		t.Fatalf("missing result must not be assigned: %v %v", batch.Results, err)
	}
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
