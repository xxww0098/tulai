package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
)

func TestStructuredPromptKeepsTenImageWashBatch(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
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

func TestStructuredPromptReachesDoubaoBridge(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	image := resultPNG(t, 42)
	seenPrompt := make(chan string, 1)
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdSetPrompt:
			var payload struct {
				Prompt string `json:"prompt"`
			}
			_ = json.Unmarshal(env.Payload, &payload)
			select {
			case seenPrompt <- payload.Prompt:
			default:
			}
			return ackOK(env, nil)
		case extbridge.CmdCaptchaAssist, extbridge.CmdSend:
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"images": []map[string]string{{"url": "https://cdn.example/output.png"}}})
		case extbridge.CmdDownload:
			return chunkAck(env, image, 0)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	e := NewEngine(st, nil, nil, t.TempDir())
	defer e.CloseSessions()
	var last Task
	err := e.Run(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"商品图"}, OutputPath: t.TempDir(),
		WaitResult: true, WaitResultSeconds: 30, Concurrency: 1,
		StructuredPrompt: "背景：浅灰背景",
		OutputSize:       OutputSize{Width: 1200, Height: 960},
	}, func(task Task) { last = task }, func(Progress) {})
	if err != nil || last.Status != StatusSuccess || len(last.Results) != 1 {
		t.Fatalf("task=%+v err=%v", last, err)
	}
	select {
	case prompt := <-seenPrompt:
		if !strings.Contains(prompt, "商品图\n附加要求：\n背景：浅灰背景") || !strings.Contains(prompt, "5:4") || strings.Count(prompt, "背景：浅灰背景") != 1 {
			t.Fatalf("bridge prompt: %q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("bridge did not receive the prompt")
	}
	if filepath.Base(last.Results[0]) != "商品图_01.png" {
		t.Fatalf("combined suffix changed output name: %s", last.Results[0])
	}
}
