package strategy

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

func TestEgoSendUncertainAfterClick(t *testing.T) {
	for _, stage := range []string{"click-error", "confirmation-read", "confirmation-timeout"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				clickErr := errors.New("click transport disconnected")
				f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{
					"composer":   {{Composer: "#editor"}, {Composer: "#editor", Value: "prompt", Send: "#send"}},
					"bindPrompt": {{}},
				}}
				switch stage {
				case "click-error":
					f.clickErrors = map[string]error{"#send": clickErr}
				case "confirmation-read":
					reads := 0
					f.afterRead = func() {
						reads++
						if reads == 3 {
							delete(f.states, "composer")
						}
					}
				}
				err := egoSend(f, "prompt")
				if !errors.Is(err, ErrSubmissionUncertain) || len(f.clicks) != 1 {
					t.Fatalf("发送动作后的错误必须标记为不确定，且只点击一次: err=%v clicks=%v", err, f.clicks)
				}
				if stage == "click-error" && !errors.Is(err, clickErr) {
					t.Fatalf("原始点击错误必须保留: %v", err)
				}
			})
		})
	}
}

func TestEgoSendFailureBeforeClickRemainsRetryable(t *testing.T) {
	f := &egoFixture{ctx: context.Background(), states: map[string][]egoState{"composer": {{}}}}
	if err := egoSend(f, "prompt"); err == nil || errors.Is(err, ErrSubmissionUncertain) || len(f.clicks) != 0 {
		t.Fatalf("发送前的错误不应误标为已尝试发送: err=%v clicks=%v", err, f.clicks)
	}
}
