package tasks

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"xbs/internal/captcha"
	"xbs/internal/cdp"
	"xbs/internal/images"
	"xbs/internal/strategy"
)

type resumeStrategy struct {
	strategy.Strategy
	calls   []string
	failure string
	err     error
	wait    func() ([]strategy.Result, error)
}

func (s *resumeStrategy) step(stage string) error {
	s.calls = append(s.calls, stage)
	if stage == s.failure {
		s.failure = ""
		return s.err
	}
	return nil
}

func (s *resumeStrategy) CleanComposer(*cdp.Session) error { return s.step("clean") }
func (s *resumeStrategy) UploadFiles(*cdp.Session, []string) error {
	return s.step("upload")
}
func (s *resumeStrategy) MarkTask(*cdp.Session) error { return s.step("mark") }
func (s *resumeStrategy) SendPrompt(*cdp.Session, string) error {
	return s.step("send")
}
func (s *resumeStrategy) WaitForResult(*cdp.Session, time.Duration) ([]strategy.Result, error) {
	s.calls = append(s.calls, "wait")
	return s.wait()
}

func TestSubmitTaskResumesOnlyAfterConfirmedSend(t *testing.T) {
	for _, stage := range []string{"clean", "upload", "mark", "send"} {
		t.Run(stage, func(t *testing.T) {
			failure := errors.New("temporary " + stage + " failure")
			strat := &resumeStrategy{failure: stage, err: failure}
			e := NewEngine(nil, nil, nil, "")
			task := &Task{Images: []string{"source.png"}}
			if err := e.submitTask(task, strat, nil); !errors.Is(err, failure) || task.submitted {
				t.Fatalf("failed submission must remain retryable: submitted=%v err=%v", task.submitted, err)
			}
			strat.calls = nil
			for range 2 {
				if err := e.submitTask(task, strat, nil); err != nil {
					t.Fatal(err)
				}
			}
			if !task.submitted || strings.Join(strat.calls, ",") != "clean,upload,mark,send" {
				t.Fatalf("confirmed send must execute once: submitted=%v calls=%v", task.submitted, strat.calls)
			}
			if err := e.submitTask(&Task{Images: task.Images}, strat, nil); err != nil || strings.Join(strat.calls, ",") != "clean,upload,mark,send,clean,upload,mark,send" {
				t.Fatalf("a different task must still submit: calls=%v err=%v", strat.calls, err)
			}
		})
	}
}

func TestResultRetryContinuesOriginalTask(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "wait-failure", true: "partial-download"}[partial], func(t *testing.T) {
			data := resultPNG(t, 81)
			output := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				e := NewEngine(nil, nil, nil, "")
				e.opts = Options{OutputPath: output, WaitResultSeconds: 10, RetryCount: 1}
				strat := &resumeStrategy{}
				waits := 0
				strat.wait = func() ([]strategy.Result, error) {
					waits++
					results := []strategy.Result{{Src: "download", Data: data}}
					if waits == 1 {
						if !partial {
							results = nil
						}
						return results, errors.New("temporary result failure")
					}
					return results, nil
				}
				got := e.execute(context.Background(), Task{Images: []string{"source.png"}}, func(ctx context.Context, task *Task) error {
					if err := e.submitTask(task, strat, nil); err != nil {
						return err
					}
					return e.waitAndSave(ctx, task, strat, nil)
				})
				if got.Status != StatusSuccess || got.Attempts != 2 || len(got.Results) != 1 || strings.Join(strat.calls, ",") != "clean,upload,mark,send,wait,wait" {
					t.Fatalf("retry must reuse the original task boundary: task=%+v calls=%v", got, strat.calls)
				}
				if saved, err := os.ReadFile(got.Results[0]); err != nil || !bytes.Equal(saved, data) {
					t.Fatalf("completed image must remain intact: %v", err)
				}
				if files, err := os.ReadDir(output); err != nil || len(files) != 1 {
					t.Fatalf("retried partial must not create duplicate files: files=%v err=%v", files, err)
				}
			})
		})
	}
}

func TestResultRetryErrorsBeatSavedPartial(t *testing.T) {
	for _, failure := range []error{captcha.ErrVerificationRequired, context.Canceled, errors.Join(captcha.ErrVerificationRequired, context.Canceled)} {
		t.Run(failure.Error(), func(t *testing.T) {
			data := resultPNG(t, 82)
			output := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				e := NewEngine(nil, nil, nil, "")
				e.opts = Options{OutputPath: output, WaitResultSeconds: 10, RetryCount: 3}
				strat := &resumeStrategy{}
				waits := 0
				strat.wait = func() ([]strategy.Result, error) {
					waits++
					if waits == 1 {
						return []strategy.Result{{Src: "download", Data: data}}, errors.New("temporary download failure")
					}
					return []strategy.Result{{Src: "download", Data: data}}, failure
				}
				got := e.execute(context.Background(), Task{Images: []string{"source.png"}}, func(ctx context.Context, task *Task) error {
					if err := e.submitTask(task, strat, nil); err != nil {
						return err
					}
					return e.waitAndSave(ctx, task, strat, nil)
				})
				wantStatus := StatusNeedsUser
				if errors.Is(failure, context.Canceled) {
					wantStatus = StatusSkipped
				}
				if got.Status != wantStatus || got.Attempts != 2 || len(got.Results) != 1 || strings.Join(strat.calls, ",") != "clean,upload,mark,send,wait,wait" {
					t.Fatalf("terminal errors must stop retries and retain partials: task=%+v calls=%v", got, strat.calls)
				}
			})
		})
	}
}

func TestResultRetryEmptyRoundDoesNotCompleteSavedPartial(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts.WaitResultSeconds = 1
		strat := &resumeStrategy{wait: func() ([]strategy.Result, error) { return nil, nil }}
		task := &Task{Results: []string{"already-saved.png"}}
		if err := e.waitAndSave(context.Background(), task, strat, nil); err == nil {
			t.Fatal("an empty result round must not complete a previous partial")
		}
	})
}

func TestResultRetryRejectsUnrelatedCandidates(t *testing.T) {
	for _, kind := range []string{"source-echo", "other-task-duplicate", "failed-fetch", "invalid-candidate"} {
		t.Run(kind, func(t *testing.T) {
			partial, unrelated := resultPNG(t, 83), resultPNG(t, 84)
			output := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				e := NewEngine(nil, nil, nil, "")
				e.opts = Options{OutputPath: output, WaitResultSeconds: 1}
				task := &Task{Images: []string{"source.png"}}
				failure := errors.New("temporary download failure")
				strat := &resultSequenceStrategy{rounds: [][]strategy.Result{{{Data: partial}}}, waitErr: failure}
				if err := e.waitAndSave(context.Background(), task, strat, nil); !errors.Is(err, failure) || len(task.Results) != 1 {
					t.Fatalf("setup must retain a saved partial: task=%+v err=%v", task, err)
				}
				candidate := strategy.Result{Data: unrelated}
				switch kind {
				case "source-echo":
					e.srcHashes = map[string]bool{images.SHA256(unrelated): true}
				case "other-task-duplicate":
					saved, err := e.saveResults(context.Background(), &Task{Images: []string{"other.png"}}, strat, nil, []strategy.Result{candidate})
					if err != nil || len(saved) != 1 {
						t.Fatalf("setup must save a different task's result: %v, %v", saved, err)
					}
				case "failed-fetch":
					candidate = strategy.Result{URL: "unavailable"}
				case "invalid-candidate":
					candidate = strategy.Result{URL: "invalid"}
					strat.data = map[string][]byte{"invalid": []byte("not an image")}
				}
				strat.rounds, strat.waitErr = [][]strategy.Result{{candidate}}, nil
				if err := e.waitAndSave(context.Background(), task, strat, nil); err == nil || len(task.Results) != 1 || task.matchedSavedResult {
					t.Fatalf("unrelated candidates must not complete an old partial: task=%+v err=%v", task, err)
				}
				if saved, err := os.ReadFile(task.Results[0]); err != nil || !bytes.Equal(saved, partial) {
					t.Fatalf("original partial must remain intact: %v", err)
				}
			})
		})
	}
}

func TestResultRetryAfterDiskFailureDoesNotResend(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "results")
	data := resultPNG(t, 85)
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputPath: blocked, WaitResultSeconds: 10, RetryCount: 1}
		strat := &resumeStrategy{wait: func() ([]strategy.Result, error) {
			return []strategy.Result{{Data: data}}, nil
		}}
		got := e.execute(context.Background(), Task{Images: []string{"source.png"}}, func(ctx context.Context, task *Task) error {
			if task.Attempts == 2 {
				e.opts.OutputPath = output
			}
			if err := e.submitTask(task, strat, nil); err != nil {
				return err
			}
			return e.waitAndSave(ctx, task, strat, nil)
		})
		if got.Status != StatusSuccess || got.Attempts != 2 || len(got.Results) != 1 || strings.Join(strat.calls, ",") != "clean,upload,mark,send,wait,wait" {
			t.Fatalf("保存失败只能重取结果，不得重复发送: task=%+v calls=%v", got, strat.calls)
		}
		if saved, err := os.ReadFile(got.Results[0]); err != nil || !bytes.Equal(saved, data) {
			t.Fatalf("恢复后须保存原生成结果: %v", err)
		}
	})
}
