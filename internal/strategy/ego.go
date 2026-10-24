package strategy

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xbs/internal/captcha"
	"xbs/internal/logging"
)

//go:embed js/ego.js
var egoJS string

// The narrow SDK surface also lets fixtures exercise the flow without a browser.
type egoPage interface {
	Ctx() context.Context
	EgoEvaluate(string, any) error
	EgoClick(string) error
	EgoFill(string, string) error
	EgoPress(string) error
	EgoUpload([]string, string, []string) error
	EgoDownload(string, time.Duration) ([]byte, error)
}

type egoControl struct {
	Selector string `json:"selector"`
	Key      string `json:"key"`
}

type egoState struct {
	Summary    string       `json:"summary"`
	Composer   string       `json:"composer"`
	Value      string       `json:"value"`
	Send       string       `json:"send"`
	Open       []string     `json:"open"`
	MenuLabel  string       `json:"menuLabel"`
	Ready      bool         `json:"ready"`
	Busy       bool         `json:"busy"`
	Downloads  []egoControl `json:"downloads"`
	Images     []egoControl `json:"images"`
	Close      string       `json:"close"`
	Preview    bool         `json:"preview"`
	Diagnostic []string     `json:"diagnostic"`
}

func egoEvaluate(s egoPage, action string, args any, out any) error {
	if err := captcha.CheckVerificationRequired(s); err != nil {
		return err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	if out == nil {
		out = &egoState{}
	}
	err = s.EgoEvaluate(fmt.Sprintf("/* xbs ego:%s */ (%s)(%q,%s)", action, egoJS, action, data), out)
	if ctxErr := s.Ctx().Err(); ctxErr != nil {
		return ctxErr
	}
	return errors.Join(err, captcha.CheckVerificationRequired(s))
}

func egoAction(s egoPage, action func() error) error {
	if err := captcha.CheckVerificationRequired(s); err != nil {
		return err
	}
	err := action()
	if ctxErr := s.Ctx().Err(); ctxErr != nil {
		return ctxErr
	}
	return errors.Join(err, captcha.CheckVerificationRequired(s))
}

func logEgoDiagnostic(stage string, lines []string) {
	for _, line := range lines {
		logging.Log.Debugf("Ego %s %s", stage, line)
	}
}

func logEgoResultState(stage string, state egoState) {
	for _, line := range state.Diagnostic {
		if line != state.Summary {
			logging.Log.Debugf("Ego %s %s", stage, line)
		}
	}
	if state.Summary != "" {
		logging.Log.Debugf("Ego %s %s", stage, state.Summary)
	}
}

func egoPause(s egoPage, deadline time.Time) error {
	if err := s.Ctx().Err(); err != nil {
		return err
	}
	wait := min(300*time.Millisecond, time.Until(deadline))
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-s.Ctx().Done():
		return s.Ctx().Err()
	case <-timer.C:
		return nil
	}
}

func egoUpload(s egoPage, files []string) error {
	var state egoState
	if err := egoEvaluate(s, "uploadStart", nil, &state); err != nil {
		return err
	}
	if state.Summary != "" {
		logging.Log.Debugf("Ego 上传计划 %s", state.Summary)
	}
	if len(state.Open) == 0 && state.MenuLabel == "" {
		logEgoDiagnostic("上传入口", state.Diagnostic)
		return fmt.Errorf("未找到当前输入区上传入口")
	}
	if err := egoAction(s, func() error { return s.EgoUpload(state.Open, state.MenuLabel, files) }); err != nil {
		if egoEvaluate(s, "upload", len(files), &state) == nil {
			logEgoDiagnostic("上传", state.Diagnostic)
		}
		return fmt.Errorf("官方文件选择器上传: %w", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := s.Ctx().Err(); err != nil {
			return err
		}
		if err := egoEvaluate(s, "upload", len(files), &state); err != nil {
			return err
		}
		if state.Ready {
			return nil
		}
		if err := egoPause(s, deadline); err != nil {
			return err
		}
	}
	logEgoDiagnostic("上传", state.Diagnostic)
	return fmt.Errorf("未确认当前输入区源图附件已就绪")
}

func egoSend(s egoPage, prompt string) (sendErr error) {
	var state egoState
	if err := egoEvaluate(s, "composer", nil, &state); err != nil {
		return err
	}
	if state.Composer == "" {
		return fmt.Errorf("未找到可写入提示词的输入框")
	}
	if err := egoAction(s, func() error { return s.EgoFill(state.Composer, prompt) }); err != nil {
		return fmt.Errorf("官方输入提示词: %w", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := s.Ctx().Err(); err != nil {
			return err
		}
		if err := egoEvaluate(s, "composer", nil, &state); err != nil {
			return err
		}
		normalize := func(text string) string { return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n")) }
		if normalize(state.Value) != normalize(prompt) {
			logEgoDiagnostic("输入区", state.Diagnostic)
			return fmt.Errorf("输入区内容与本次提示词不一致，未发送")
		}
		if state.Send != "" {
			break
		}
		if !time.Now().Before(deadline) {
			logEgoDiagnostic("发送入口", state.Diagnostic)
			return fmt.Errorf("未找到已就绪的发送按钮")
		}
		if err := egoPause(s, deadline); err != nil {
			return err
		}
	}
	if err := egoEvaluate(s, "bindPrompt", prompt, nil); err != nil {
		return err
	}
	defer func() {
		if sendErr != nil {
			sendErr = fmt.Errorf("%w: %w", ErrSubmissionUncertain, sendErr)
		}
	}()
	if err := egoAction(s, func() error { return s.EgoClick(state.Send) }); err != nil {
		return fmt.Errorf("官方点击发送: %w", err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if err := s.Ctx().Err(); err != nil {
			return err
		}
		if err := egoEvaluate(s, "composer", nil, &state); err != nil {
			return err
		}
		if state.Composer != "" && strings.TrimSpace(state.Value) == "" {
			return nil
		}
		if err := egoPause(s, deadline); err != nil {
			return err
		}
	}
	logEgoDiagnostic("发送确认", state.Diagnostic)
	return fmt.Errorf("平台未接受消息：发送后输入仍未清空")
}

func egoClean(s egoPage) error {
	var state struct {
		Remove         []string `json:"remove"`
		PromotionClose string   `json:"promotionClose"`
		Diagnostic     []string `json:"diagnostic"`
		Preview        bool     `json:"preview"`
	}
	if err := egoEvaluate(s, "clean", nil, &state); err != nil {
		return err
	}
	if state.Preview {
		if err := egoClosePreview(s, time.Now().Add(5*time.Second)); err != nil {
			return err
		}
		if err := egoEvaluate(s, "clean", nil, &state); err != nil {
			return err
		}
	}
	if state.PromotionClose != "" {
		logEgoDiagnostic("客户端推广", state.Diagnostic)
		if err := egoAction(s, func() error { return s.EgoClick(state.PromotionClose) }); err != nil {
			return fmt.Errorf("关闭客户端推广: %w", err)
		}
		if err := egoEvaluate(s, "clean", nil, &state); err != nil {
			return err
		}
	}
	for _, selector := range state.Remove {
		if err := egoAction(s, func() error { return s.EgoClick(selector) }); err != nil {
			return fmt.Errorf("清理当前附件: %w", err)
		}
	}
	return nil
}

func egoClosePreview(s egoPage, deadline time.Time) error {
	if err := egoAction(s, func() error { return s.EgoPress("Escape") }); err != nil {
		return fmt.Errorf("关闭图像预览: %w", err)
	}
	var state egoState
	clickedClose := false
	if limit := time.Now().Add(5 * time.Second); limit.Before(deadline) {
		deadline = limit
	}
	for time.Now().Before(deadline) {
		if err := egoEvaluate(s, "preview", nil, &state); err != nil {
			return err
		}
		if !state.Preview {
			return nil
		}
		if state.Close != "" && !clickedClose {
			if err := egoAction(s, func() error { return s.EgoClick(state.Close) }); err != nil {
				return fmt.Errorf("关闭图像预览: %w", err)
			}
			clickedClose = true
		}
		if err := egoPause(s, deadline); err != nil {
			return err
		}
	}
	logEgoDiagnostic("关闭预览", state.Diagnostic)
	return fmt.Errorf("图像预览尚未关闭")
}

func egoWaitForResult(s egoPage, timeout time.Duration) (results []Result, resultErr error) {
	defer func() {
		if err := s.Ctx().Err(); err != nil {
			results, resultErr = nil, err
		} else if errors.Is(resultErr, context.Canceled) {
			results = nil
		}
	}()
	deadline := time.Now().Add(timeout)
	var state egoState
	// 下载失败可能留下预览，重试前先关闭以便重新定位原任务图片。
	if err := egoEvaluate(s, "preview", nil, &state); err != nil {
		return nil, err
	}
	if state.Preview {
		if err := egoClosePreview(s, deadline); err != nil {
			return nil, err
		}
	}
	completed := make(map[string]bool)
	var knownImages []egoControl
	imageIndexes := make(map[string]int)
	preview := ""
	lastSummary := ""
	for time.Now().Before(deadline) {
		if err := s.Ctx().Err(); err != nil {
			return nil, err
		}
		if err := egoEvaluate(s, "results", map[string]any{"preview": preview}, &state); err != nil {
			return results, err
		}
		if state.Summary != lastSummary {
			logEgoResultState("结果阶段", state)
			lastSummary = state.Summary
		}
		for _, img := range state.Images {
			if index, ok := imageIndexes[img.Key]; ok {
				knownImages[index] = img
			} else {
				imageIndexes[img.Key] = len(knownImages)
				knownImages = append(knownImages, img)
			}
		}
		if !state.Busy {
			downloads := state.Downloads
			// Without a proven button-to-image association, multiple images must
			// each be opened in the preview instead of sharing one inline button.
			if preview == "" && len(knownImages) > 1 {
				downloads = nil
			}
			inlineDownload := preview == "" && len(downloads) > 0
			for _, download := range downloads {
				key := download.Key
				if preview != "" {
					key = preview
				}
				if completed[key] {
					continue
				}
				var data []byte
				err := egoAction(s, func() (err error) {
					data, err = s.EgoDownload(download.Selector, time.Until(deadline))
					return err
				})
				if err != nil {
					if data != nil && !errors.Is(err, context.Canceled) && s.Ctx().Err() == nil {
						results = append(results, Result{Src: "download", TS: time.Now().UnixMilli(), Data: data})
					}
					return results, fmt.Errorf("官方网页下载: %w", err)
				}
				if err := s.Ctx().Err(); err != nil {
					return nil, err
				}
				if data == nil {
					data = []byte{}
				}
				results = append(results, Result{Src: "download", TS: time.Now().UnixMilli(), Data: data})
				completed[key] = true
			}
			if preview != "" && completed[preview] {
				if err := egoClosePreview(s, deadline); err != nil {
					return results, err
				}
				preview = ""
				remaining := false
				for _, img := range knownImages {
					remaining = remaining || !completed[img.Key]
				}
				if !remaining {
					return results, nil
				}
			}
			if inlineDownload && len(results) > 0 {
				return results, nil
			}
			if preview == "" {
				for _, img := range knownImages {
					if completed[img.Key] {
						continue
					}
					if err := egoAction(s, func() error { return s.EgoClick(img.Selector) }); err != nil {
						return results, fmt.Errorf("打开本轮生成图预览: %w", err)
					}
					preview = img.Key
					break
				}
			}
		}
		if err := egoPause(s, deadline); err != nil {
			return nil, err
		}
	}
	logEgoResultState("结果等待", state)
	return results, fmt.Errorf("等待本轮助手生成图下载入口超时")
}
