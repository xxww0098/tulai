package ui

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/tasks"
)

const DefaultFissionPrompt = prompts.DefaultFissionPrompt

func (a *App) fissionPage(c *ui.Context) {
	a.workColumns(c, func() {
		a.imageCard(c, "fission-image", "母图", true)
		a.fissionPromptCard(c)
	})
}

func (a *App) fissionPromptCard(c *ui.Context) {
	tok := a.look
	_, h := c.Size()
	height := float32(90)
	if h < 720 {
		height = 68
	}
	hint := "近景、远景、俯视、侧视各生成一张；完成后检查两两 pHash 相似度，不合格可重新生成。相似度仅衡量画面结构，主体来源及语义差异仍需人工确认。"
	a.stepCard(c, "fission-prompt", stepDone).Children(func() {
		ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "裂变指令").FontSize(tok.title).Bold().Shrink(0)
			ui.Box(c).Shrink(0).Tooltip(hint).Children(func() {
				ui.Icon(c, iconInfo).FontSize(12).TextColor(tok.muted)
			})
			ui.Spacer(c)
			uiIconBtn(c, "fission-reset", "恢复默认", "恢复默认指令", iconRotateCcw, 22, false, false, tok).
				Disabled(a.running || a.prompts == DefaultFissionPrompt).OnClick(func() { a.prompts = DefaultFissionPrompt })
		})
		ui.TextArea(c, &a.prompts).Height(height).MinHeight(height).FillWidth().Label("裂变指令").
			Placeholder(DefaultFissionPrompt).Disabled(a.running)
		ui.Text(c, "4 视角生图 · 自动 pHash 校验").FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(hint)
	})
}

// mergeFissionRetry 保留组内其他结果，按独立方向提示词替换重试结果。
func mergeFissionRetry(base, retry []tasks.Task) []tasks.Task {
	out := append([]tasks.Task(nil), base...)
	for _, next := range retry {
		for i := range out {
			if out[i].Platform == next.Platform && out[i].Prompt == next.Prompt {
				next.Index = out[i].Index
				out[i] = next
				break
			}
		}
	}
	return out
}

// 重试相似度未达标的图片应发起新生成，不沿用上次的提交上下文。
func freshFissionRetries(items []tasks.Task) []tasks.Task {
	out := make([]tasks.Task, 0, len(items))
	for _, task := range items {
		if task.Status == tasks.StatusNeedsReview && strings.HasPrefix(task.Error, "裂变相似度") {
			task = tasks.Task{Index: task.Index, Platform: task.Platform, Prompt: task.Prompt,
				Images: append([]string(nil), task.Images...), Status: task.Status, Error: task.Error}
		}
		out = append(out, task)
	}
	return out
}

func sameFissionSource(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}
