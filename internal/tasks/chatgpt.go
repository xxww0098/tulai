package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/chatgpt"
	"github.com/xxww0098/tulai/internal/strategy"
)

func (e *Engine) attemptChatGPT(ctx context.Context, t *Task) error {
	if t.chatGPT == nil {
		t.chatGPT = &chatgpt.Generation{}
	}
	g := t.chatGPT
	if !g.Attempted {
		t.noUse = true
	}
	if g.Attempted && g.ConversationID == "" && !g.Submitted {
		return chatGPTError(chatgpt.ErrSubmissionUncertain)
	}
	t.ranOn = t.BrowserID
	strat := &strategy.ChatGPT{}
	sess, err := e.session(ctx, chatgpt.Platform, t.BrowserID, strat)
	if err != nil {
		return err
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(sess.Ctx(), cancel)
	defer func() {
		stop()
		cancel()
		if sess.Ctx().Err() != nil {
			e.forgetSession(sess)
		}
	}()
	client, err := chatgpt.New(work, sess)
	if err != nil {
		return chatGPTError(err)
	}
	if !g.Attempted {
		prompt := t.Prompt
		if strings.TrimSpace(prompt) == "" && e.opts.Wash {
			prompt = "按洗图要求处理参考图，保留主体"
		}
		err = client.Start(work, g, prompt, e.opts.ChatGPTModel, t.Images)
		t.noUse = !g.Attempted
		t.submitted = g.Submitted
		if g.Submitted && t.sentAt.IsZero() {
			t.sentAt = time.Now()
		}
		if g.ConversationID != "" {
			t.sentURL = chatgpt.HomeURL + "c/" + g.ConversationID
		}
		if err != nil {
			return chatGPTError(err)
		}
	}
	if !e.opts.WaitResult {
		return nil
	}
	urls, err := client.Results(work, g)
	if err != nil {
		return chatGPTError(err)
	}
	for _, raw := range urls {
		data, err := sess.LoadResourceContext(work, raw)
		if err != nil {
			if work.Err() != nil {
				return work.Err()
			}
			return errors.New("ChatGPT 图片下载失败，请重试获取原任务结果")
		}
		saved, err := e.saveResults(work, t, strat, sess, []strategy.Result{{Data: data, Src: "chatgpt"}})
		t.Results = append(t.Results, saved...)
		if err != nil {
			return err
		}
	}
	if len(t.Results) == 0 {
		return fmt.Errorf("ChatGPT 没有返回可保存的生成图")
	}
	return nil
}

func chatGPTError(err error) error {
	switch {
	case errors.Is(err, chatgpt.ErrSubmissionUncertain):
		return errors.Join(strategy.ErrSubmissionUncertain, err)
	case errors.Is(err, chatgpt.ErrVerification):
		return errors.Join(captcha.ErrVerificationRequired, err)
	case errors.Is(err, chatgpt.ErrQuota):
		return errors.Join(strategy.ErrAccountQuota, err)
	case errors.Is(err, chatgpt.ErrRate):
		return errors.Join(strategy.ErrAccountRate, err)
	default:
		return err
	}
}
