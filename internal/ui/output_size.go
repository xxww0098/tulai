package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/tasks"
)

const (
	outputOriginal = "保持原尺寸"
	outputRatio    = "指定比例"
	outputPixels   = "指定像素"
)

func (a *App) loadOutputSize() {
	s := a.store
	a.outputSizeMode = pickStr(s.Get("doubao_output_size_mode"), outputOriginal, []string{outputOriginal, outputRatio, outputPixels})
	a.outputRatio = s.Get("doubao_output_ratio")
	if a.outputRatio == "" {
		a.outputRatio = "5:4"
	}
	a.outputWidth = s.Get("doubao_output_width")
	a.outputHeight = s.Get("doubao_output_height")
	if a.outputWidth == "" {
		a.outputWidth = "1200"
	}
	if a.outputHeight == "" {
		a.outputHeight = "960"
	}
	a.outputCrop = s.GetBool("doubao_output_crop")
}

func (a *App) saveOutputSize() {
	s := a.store
	s.Set("doubao_output_size_mode", a.outputSizeMode)
	s.Set("doubao_output_ratio", a.outputRatio)
	s.Set("doubao_output_width", a.outputWidth)
	s.Set("doubao_output_height", a.outputHeight)
	s.SetInt("doubao_output_crop", boolInt(a.outputCrop))
}

func (a *App) doubaoOutputSize() tasks.OutputSize {
	result := tasks.OutputSize{Crop: a.outputCrop}
	switch a.outputSizeMode {
	case outputRatio:
		result.Enabled = true
		result.Ratio = a.outputRatio
	case outputPixels:
		result.Enabled = true
		result.Width, _ = strconv.Atoi(strings.TrimSpace(a.outputWidth))
		result.Height, _ = strconv.Atoi(strings.TrimSpace(a.outputHeight))
	}
	return result
}

func (a *App) outputSizeSelect(c *ui.Context) {
	if a.outputSizeMode == "" {
		a.outputSizeMode = outputOriginal
	}
	ui.Text(c, "尺寸").FontSize(a.look.caption).TextColor(a.look.muted).Shrink(0)
	motionSelect(c, &a.outputSizeMode, []string{outputOriginal, outputRatio, outputPixels}).
		Width(126).Label("豆包输出尺寸").Disabled(a.running).OnChange(func() {
		if a.outputRatio == "" {
			a.outputRatio = "5:4"
		}
		if a.outputWidth == "" {
			a.outputWidth = "1200"
		}
		if a.outputHeight == "" {
			a.outputHeight = "960"
		}
	})
}

func (a *App) outputSizeFields(c *ui.Context) {
	if a.outputSizeMode == outputOriginal {
		return
	}
	if a.outputSizePreset == "" {
		a.outputSizePreset = "常用"
	}
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		presets := []string{"常用", "1:1", "4:3", "3:4", "5:4", "4:5", "16:9", "9:16"}
		presetLabel := "常用比例"
		if a.outputSizeMode == outputRatio {
			ui.Text(c, "比例").FontSize(a.look.caption).Shrink(0)
			ui.TextInput(c, &a.outputRatio).Width(66).Label("画面比例").Placeholder("5:4").Disabled(a.running)
		} else {
			presets = []string{"常用", "1024×1024", "1200×1200", "1200×960", "1080×1350", "1920×1080", "1080×1920", "2048×2048"}
			presetLabel = "常用像素"
			ui.TextInput(c, &a.outputWidth).Width(66).Label("输出宽度").Disabled(a.running)
			ui.Text(c, "×").Shrink(0)
			ui.TextInput(c, &a.outputHeight).Width(66).Label("输出高度").Disabled(a.running)
		}
		motionSelect(c.Key("output-size-presets"), &a.outputSizePreset, presets).Width(100).Label(presetLabel).Disabled(a.running).OnChange(func() {
			if a.outputSizePreset != "常用" {
				if a.outputSizeMode == outputRatio {
					a.outputRatio = a.outputSizePreset
				} else {
					a.outputWidth, a.outputHeight, _ = strings.Cut(a.outputSizePreset, "×")
				}
			}
			a.outputSizePreset = "常用"
		})
		ui.Checkbox(c, &a.outputCrop, "裁剪").Label("居中裁剪铺满画面").Disabled(a.running).
			Tooltip("默认等比缩放并补白；勾选后可能裁掉图片边缘。豆包生成时会尝试匹配比例，保存时再统一尺寸。")
	})
	if err := a.doubaoOutputSize().Validate(); err != nil {
		ui.Text(c, fmt.Sprintf("输出尺寸：%s", strings.TrimSpace(err.Error()))).FontSize(12).TextColor(c.Theme().Danger).Wrap()
	}
}
