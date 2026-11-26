package tasks

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestStructuredPromptKeepsTenImageWashBatch(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	var sources []string
	for i := 1; i <= 11; i++ {
		sources = append(sources, fmt.Sprintf("%02d.png", i))
	}
	addition := "背景：浅灰背景\n构图：主体居中"
	var sizes []int
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"清理背景"}, Images: sources,
		Wash: true, Concurrency: 1, WaitResult: true, DailyCap: 100, StructuredPrompt: addition,
	}, nil, func(Progress) {}, func(_ context.Context, task *Task) error {
		sizes = append(sizes, len(task.Images))
		if task.Prompt != "清理背景\n附加要求：\n"+addition {
			t.Errorf("unexpected batch prompt: %q", task.Prompt)
		}
		if len(task.batch) > 0 {
			for i := range task.batch {
				task.batch[i].Results = []string{task.batch[i].Images[0] + ".out"}
			}
		} else {
			task.Results = []string{task.Images[0] + ".out"}
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(sizes, []int{10, 1}) {
		t.Fatalf("batch sizes=%v err=%v", sizes, err)
	}
	for i, task := range e.View().Snapshot() {
		if len(task.Images) != 1 || task.Images[0] != sources[i] || len(task.Results) != 1 || task.Results[0] != sources[i]+".out" || strings.Count(task.Prompt, addition) != 1 {
			t.Fatalf("task lost association or combination: %+v", task)
		}
	}
}
