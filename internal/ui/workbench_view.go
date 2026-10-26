package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"xbs/internal/captcha"
	"xbs/internal/images"
	"xbs/internal/logging"
	"xbs/internal/prompts"
	"xbs/internal/store"
	"xbs/internal/tasks"
)

func (a *App) chrome(c *ui.Context) {
	ui.Column(c).Padding(16, 28, 12, 28).Gap(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "tulai").FontSize(22).Bold()
			subtitle := "豆包 / 千问"
			if v := mygo.App.Version(); v != "" {
				subtitle += " · " + v
			}
			ui.Text(c, subtitle).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine()
			ui.Button(c, "设置向导").Disabled(a.running).OnClick(func() { a.openWizard() })
			ui.Button(c, "检查更新").Disabled(a.updateBusy).OnClick(func() { a.checkForUpdates() })
		})
		ui.Segmented(c, &a.tab, "出图", "浏览器", "提示词", "设置").Disabled(a.running)
	})
}

func (a *App) homePage(c *ui.Context) {
	_, height := c.Size()
	minH := height - 132
	if minH < 560 {
		minH = 560
	}
	gap := float32(16)
	if len(a.queueItems()) > 0 {
		// 有进度时把拖图区压成一条，保证 720 高的窗口里进度和「高级」还在屏内。
		gap = 10
	}
	ui.Scroll(c.Key("home")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().MinHeight(minH).Gap(gap).Padding(8, 0, 12, 0).Children(func() {
			a.dropZone(c)
			a.promptPicker(c)
			a.loginBanner(c)
			a.startBar(c)
			a.progressPane(c)
			ui.Collapsible(c, "高级", &a.advancedOpen, func() {
				a.advancedFields(c)
			})
		})
	})
}

func (a *App) dropZone(c *ui.Context) {
	busy := len(a.queueItems()) > 0
	pad := float32(16)
	if busy {
		pad = 10
	}
	zone := ui.Column(c.Key("drop-zone")).FillWidth().Padding(pad).Gap(12).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(16)
	if !busy {
		zone.Grow(1).MinHeight(280)
	}
	if paths := zone.DroppedFiles(); len(paths) > 0 && !a.running {
		n := a.appendSources(loadSourceFiles(paths))
		if n > 0 {
			logging.Log.Info("添加 %d 张源图", n)
		}
	}
	if zone.FileDragOver() {
		zone.Border(2, c.Theme().Accent)
	}
	empty := len(a.sources) == 0
	if empty && zone.Clicked() && !a.running {
		a.addImages()
	}
	zone.Children(func() {
		if empty {
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Text(c, "把图片拖到这里，或点击选择").FontSize(20).Bold()
				ui.Text(c, "可以一次选多张。不放图片时，用下面的提示词直接出图").FontSize(13).TextColor(c.Theme().TextMuted)
			})
			return
		}
		if busy {
			a.sourceStrip(c)
			return
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("已选 %d 张", len(a.sources))).FontSize(16).Bold().Grow(1)
			ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
			ui.Button(c, "清空").Disabled(a.running).OnClick(func() {
				a.sources = nil
				a.srcSel = -1
			})
		})
		width, _ := c.Size()
		cols := int((width - 80) / 156)
		if cols < 2 {
			cols = 2
		}
		if cols > 6 {
			cols = 6
		}
		ui.Grid(c).Columns(cols).Gap(12).Children(func() {
			for i := range a.sources {
				si := a.sources[i]
				ui.Column(c.Key(si.Path)).Gap(6).Children(func() {
					thumb := float32(120)
					if busy {
						thumb = 72
					}
					if si.Thumb != nil {
						ui.Image(c, si.Thumb).Size(thumb, thumb).Radius(8)
					} else {
						ui.Text(c, "图片").Height(thumb)
					}
					ui.Text(c, filepath.Base(si.Path)).FontSize(12).SingleLine().Tooltip(si.Path)
					ui.Button(c, "移除").Disabled(a.running).OnClick(func() { a.removeSourceAt(i) })
				})
			}
		})
	})
}

// sourceStrip 在已有出图进度时把已选图片收成一行，避免把进度挤出窗口。
func (a *App) sourceStrip(c *ui.Context) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("已选 %d 张", len(a.sources))).FontSize(15).Bold()
		ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(48).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				for i := range a.sources {
					si := a.sources[i]
					ui.Row(c.Key(si.Path)).Gap(6).AlignItems(ui.Center).Children(func() {
						if si.Thumb != nil {
							ui.Image(c, si.Thumb).Size(40, 40).Radius(6)
						}
						ui.Text(c, filepath.Base(si.Path)).FontSize(12).SingleLine().Tooltip(si.Path)
						ui.Button(c, "移除").Disabled(a.running).OnClick(func() { a.removeSourceAt(i) })
					})
				}
			})
		})
		ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
		ui.Button(c, "清空").Disabled(a.running).OnClick(func() {
			a.sources = nil
			a.srcSel = -1
		})
	})
}

func (a *App) promptPicker(c *ui.Context) {
	labels := prompts.Labels(a.userPrompts)
	if !containsStr(labels, a.promptChoice) {
		a.promptChoice = prompts.LabelFor(a.promptPick, a.userPrompts)
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "提示词").FontSize(15).Bold()
			ui.Select(c.Key("prompt-template"), &a.promptChoice, labels).Grow(1).MinWidth(0).Disabled(a.running).OnChange(func() {
				a.applyPromptChoice()
			})
			ui.Button(c, "编辑提示词").Disabled(a.running).OnClick(func() { a.tab = pagePrompts })
		})
		if a.promptPick == "" {
			h := float32(72)
			if len(a.queueItems()) > 0 {
				h = 48
			}
			ui.TextArea(c, &a.prompts).Height(h).FillWidth().Label("提示词内容").Placeholder("不放图片时，每行一条。选了模板就整段发送")
		} else {
			preview := oneLine(a.prompts)
			rs := []rune(preview)
			if len(rs) > 48 {
				preview = string(rs[:48]) + "…"
			}
			if preview != "" {
				ui.Text(c, preview).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(oneLine(a.prompts))
			}
		}
		if a.promptNote != "" {
			ui.Text(c, a.promptNote).FontSize(12).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *App) loginBanner(c *ui.Context) {
	text, id, ok := "", int64(0), false
	if a.engine != nil {
		text, id, ok = a.engine.Attention()
	}
	if !ok || text == "" {
		return
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Padding(10, 12).
		Background(c.Theme().Surface).Border(1, c.Theme().Warning).Radius(10).Children(func() {
		ui.Text(c, text).FontSize(14).TextColor(c.Theme().Warning).Grow(1).MinWidth(0).SingleLine().Tooltip(text)
		ui.Button(c, "去处理").OnClick(func() { a.openAttention(id) })
	})
}

func (a *App) openAttention(id int64) {
	a.tab = pageBrowser
	a.refreshBrowsers()
	for i, row := range a.browserRows {
		if row.ID == id {
			a.browserSel = i
			break
		}
	}
	var row store.Browser
	if a.store != nil {
		for _, b := range a.store.Browsers() {
			if b.ID == id {
				row = b
				break
			}
		}
	}
	if row.ID == 0 || a.engine == nil {
		return
	}
	go func(b store.Browser) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = a.engine.FocusAccount(ctx, b)
	}(row)
}

func (a *App) startBar(c *ui.Context) {
	plan := a.workbenchPlan()
	hint := a.startHint(plan)
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		if a.running {
			ui.Button(c, "停止").Height(56).FontSize(18).FillWidth().OnClick(func() { a.stopTasks() })
		} else {
			ui.PrimaryButton(c, "开始出图").Height(56).FontSize(18).FillWidth().
				Disabled(plan.Action == StartNone).OnClick(func() { a.startTasks() })
		}
		ui.Text(c, hint).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(hint)
		if a.running || a.progress > 0 {
			ui.Progress(c, a.progress).FillWidth().Height(6)
		}
	})
}

func (a *App) startHint(plan WorkbenchPlan) string {
	items := a.queueItems()
	if !a.running && hasNeedsUser(items) {
		return taskResultSummary(items)
	}
	if a.running {
		if a.progText != "" {
			return a.progText
		}
		return "正在出图"
	}
	if a.progText != "" && plan.Action != StartWizard {
		return a.progText
	}
	switch plan.Action {
	case StartWizard:
		return "还没有可用账号，点「开始出图」会先打开设置向导"
	case StartNone:
		return "拖入图片，或写下提示词。不放图片也可以只按提示词出图"
	default:
		return "将使用" + strings.Join(plan.Platforms, "、") + "，账号自动轮换 · 保存到 " + a.resolvedOutputDir()
	}
}

func hasNeedsUser(items []tasks.Task) bool {
	for _, task := range items {
		if task.Status == tasks.StatusNeedsUser {
			return true
		}
	}
	return false
}

func (a *App) progressPane(c *ui.Context) {
	items := a.queueItems()
	cards := ProgressCards(items)
	if len(cards) == 0 {
		return
	}
	failed := FailedTasks(items)
	needsUser := false
	for _, card := range cards {
		if card.Status == "待验证" {
			needsUser = true
			break
		}
	}
	ui.Column(c).FillWidth().Padding(14).Gap(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(16).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "出图进度").FontSize(16).Bold().Grow(1)
			if len(failed) > 0 {
				ui.Button(c, "全部重试失败项").Disabled(a.running).OnClick(func() { a.retryTasks(failed) })
			}
		})
		if needsUser {
			ui.Text(c, verificationHint).FontSize(12).TextColor(c.Theme().Warning)
		}
		for _, card := range cards {
			a.progressCard(c, card)
		}
	})
}

func (a *App) progressCard(c *ui.Context, card ProgressCard) {
	ui.Row(c.Key("card:"+card.Key)).Gap(12).AlignItems(ui.Start).Padding(8, 4).Children(func() {
		thumb := a.thumbFor(card.Source, 160)
		if thumb != nil {
			ui.Image(c, thumb).Size(72, 72).Radius(8)
		} else if card.Source != "" {
			ui.Text(c, "图片").Width(72).Height(72)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, card.Title).Bold().Grow(1).MinWidth(0).SingleLine().Tooltip(card.Title)
				ui.Badge(c, card.Status)
				if (card.Status == "失败" || card.Status == "待验证") && len(card.Retry) > 0 {
					ui.Button(c, "重试").Disabled(a.running).OnClick(func() { a.retryTasks(card.Retry) })
				}
			})
			if len(card.Platforms) > 0 {
				ui.Text(c, strings.Join(card.Platforms, "、")).FontSize(12).TextColor(c.Theme().TextMuted)
			}
			if card.Reason != "" {
				color := c.Theme().Danger
				if card.Status == "待验证" {
					color = c.Theme().Warning
				}
				ui.Text(c, card.Reason).FontSize(13).TextColor(color).SingleLine().Tooltip(card.Reason)
			}
			if len(card.Results) > 0 {
				ui.Row(c).Gap(8).Children(func() {
					n := len(card.Results)
					if n > 4 {
						n = 4
					}
					for _, path := range card.Results[:n] {
						if bmp := a.thumbFor(path, 160); bmp != nil {
							ui.Image(c.Key(path), bmp).Size(64, 64).Radius(6)
						}
					}
				})
			}
		})
	})
}

func (a *App) promptsPage(c *ui.Context) {
	ui.Column(c).Fill().Gap(14).Padding(16, 8).Children(func() {
		ui.Text(c, "提示词").FontSize(22).Bold()
		ui.Text(c, "内置模板和保存过的提示词。出图页会记住上次的选择。").FontSize(13).TextColor(c.Theme().TextMuted)
		a.promptRow(c)
		ui.TextArea(c, &a.prompts).Grow(1).MinHeight(220).Label("提示词内容").Placeholder("每行一条。选了模板后，整段一起发送")
		if a.promptNote != "" {
			ui.Text(c, a.promptNote).FontSize(12).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *App) settingsPage(c *ui.Context) {
	ui.Scroll(c.Key("settings-page")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).MaxWidth(760).FillWidth().Gap(16).Padding(16, 8, 24, 8).Children(func() {
			ui.Text(c, "设置").FontSize(22).Bold()
			ui.Text(c, "验证码在这里调。平台、并发和输出文件夹在出图页的「高级」里。").FontSize(13).TextColor(c.Theme().TextMuted)
			a.captchaFields(c)
		})
	})
}

func (a *App) captchaFields(c *ui.Context) {
	ui.Form(c, func() {
		ui.Checkbox(c, &a.captchaEnabled, "自动求解验证码")
		ui.Field(c, "求解后端", func() {
			ui.Select(c, &a.captchaBackend, []string{"free", "custom", "ikunocr"})
		})
		ui.Text(c, "free=纯CV离线（九宫格拖拽/拼图）；custom=视觉大模型；ikunocr=接码平台").FontSize(11)
		ui.Fieldset(c, "custom 视觉大模型（OpenAI 兼容）", func() {
			ui.Field(c, "API 地址", func() {
				ui.TextInput(c, &a.customURL)
			})
			ui.Field(c, "API Key", func() {
				ui.TextInput(c, &a.customKey).Password()
			})
			ui.Field(c, "模型", func() {
				ui.TextInput(c, &a.customModel)
			})
		})
		ui.Fieldset(c, "ikunocr 接码平台", func() {
			ui.Field(c, "API Key", func() {
				ui.TextInput(c, &a.ikunocrKey).Password()
			})
		})
	}).FillWidth().Gap(12)
}

// advancedFields 是出图页「高级」里的平台、轮换、输出和单次任务选项。
func (a *App) advancedFields(c *ui.Context) {
	ui.Form(c, func() {
		ui.Text(c, "运行日志").FontSize(15).Bold()
		a.autoLogPane(c)
		ui.Field(c, "平台", func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Switch(c, &a.platformAuto).Disabled(a.running)
				ui.Text(c, "自动选择平台")
			})
		})
		ui.Text(c, "用已登录的账号。额度用尽、限流或验证码时，自动换到同平台下一个健康账号。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(16).Children(func() {
			ui.Checkbox(c, &a.platDoubao, "豆包").Disabled(a.running || a.platformAuto)
			ui.Checkbox(c, &a.platQianwen, "千问").Disabled(a.running || a.platformAuto)
		})
		ui.Text(c, a.platformLine()).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "输出目录", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.outputPath).Grow(1).MinWidth(0).Placeholder("留空则保存到输出结果")
				ui.Button(c, "选择…").Disabled(a.running).OnClick(func() { a.pickOutputDir() })
				ui.Button(c, "打开输出文件夹").OnClick(func() { a.openOutputFolder() })
			})
		})
		resolved := a.resolvedOutputDir()
		ui.Text(c, "图片会保存到 "+resolved).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(resolved)
		if a.outputNote != "" {
			ui.Text(c, a.outputNote).FontSize(12).TextColor(c.Theme().Danger)
		}
		ui.Field(c, "保存方式", func() {
			ui.Select(c, &a.saveMode, []string{"单文件夹汇总", "按源图片分文件夹"})
		})
		ui.Field(c, "命名规则", func() {
			ui.Select(c, &a.namingRule, []string{"原文件名", "时间戳", "序号"})
		})
		ui.Field(c, "文件名前缀", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.filePrefix).Grow(1).MinWidth(0)
				ui.Checkbox(c, &a.prefixOn, "启用")
			})
		})
		ui.Field(c, "文件名后缀", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.fileSuffix).Grow(1).MinWidth(0)
				ui.Checkbox(c, &a.suffixOn, "启用")
			})
		})
		ui.Field(c, "后缀", func() {
			ui.TextInput(c, &a.promptSuffix)
		})
		ui.Checkbox(c, &a.useSuffix, "附加提示词后缀")
		ui.Checkbox(c, &a.removeWatermark, "去水印（实验）").Disabled(a.running)
		ui.Text(c, "已适配豆包/千问方图。另存修复 PNG，保留原图；仅匹配已支持水印，复杂背景可能有痕迹。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, "调度与等待").Bold().Margin(12, 0, 4, 0)
		ui.Field(c, "发送策略", func() {
			ui.Select(c, &a.sendStrategy, []string{"顺序循环", "随机循环"})
		})
		ui.Field(c, "并发数", func() {
			ui.NumberInput(c, &a.concurrency, 1, 16, 1)
		})
		ui.Field(c, "失败重试", func() {
			ui.NumberInput(c, &a.retryCount, 0, 20, 1)
		})
		ui.Field(c, "发送间隔(秒)", func() {
			ui.NumberInput(c, &a.sendWait, 0, 600, 1)
		})
		ui.Field(c, "发送前等待(秒)", func() {
			ui.NumberInput(c, &a.waitBefore, 0, 60, 1)
		})
		ui.Field(c, "等待结果", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Switch(c, &a.waitResult)
				if a.waitResult {
					ui.NumberInput(c, &a.waitResultS, 10, 1800, 10)
				}
			})
		})
		ui.Field(c, "定时切换浏览器", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Switch(c, &a.taskSwitch)
				if a.taskSwitch {
					ui.NumberInput(c, &a.switchCount, 1, 9999, 1)
				}
			})
		})
		ui.Field(c, "起始浏览器序号", func() {
			ui.NumberInput(c, &a.startBrowserIdx, 1, 999, 1)
		})
		ui.Collapsible(c, "详细日志", &a.logsOpen, func() {
			a.logPane(c)
		})
	}).FillWidth().Gap(12)
}

func (a *App) autoLogPane(c *ui.Context) {
	var notes []string
	if a.engine != nil {
		notes = a.engine.AutoNotes()
	}
	if len(notes) == 0 {
		ui.Text(c, "自动处理账号和浏览器的记录会显示在这里").FontSize(12).TextColor(c.Theme().TextMuted)
		return
	}
	start := 0
	if len(notes) > 6 {
		start = len(notes) - 6
	}
	for _, line := range notes[start:] {
		ui.Text(c, line).FontSize(12).SingleLine().Tooltip(line)
	}
}

func (a *App) logPane(c *ui.Context) {
	if len(a.logs) == 0 {
		ui.Text(c, "操作与任务进展会记录在这里").FontSize(12).TextColor(c.Theme().TextMuted)
		return
	}
	a.logList.Key = nil
	a.logList.FollowEnd = true
	ui.List(c, &a.logList, len(a.logs), func(i int) {
		e := a.logs[i]
		color := c.Theme().TextMuted
		switch e.Level {
		case logging.Error:
			color = c.Theme().Danger
		case logging.Warning:
			color = c.Theme().Warning
		case logging.Success:
			color = c.Theme().Success
		}
		ui.Row(c).Gap(10).Padding(5, 2).Children(func() {
			ui.Text(c, e.Time.Format("15:04:05")).FontSize(11).TextColor(c.Theme().TextMuted)
			ui.Text(c, e.Level.String()).FontSize(12).TextColor(color)
			ui.Text(c, e.Text).FontSize(12).Grow(1).MinWidth(0).SingleLine().Tooltip(e.Text)
		})
	}).Height(180)
}

func (a *App) platformLine() string {
	names := a.carryingPlatforms(true)
	if a.platformAuto {
		if len(names) == 0 {
			return "当前还没有已登录的账号"
		}
		return "当前将使用：" + strings.Join(names, "、")
	}
	if !a.platDoubao && !a.platQianwen {
		return "请勾选豆包或千问"
	}
	return "按勾选的平台出图，仍会在健康账号之间自动轮换"
}

func (a *App) carryingPlatforms(auto bool) []string {
	in := a.workbenchInput()
	in.HasImages = true
	in.AutoPlatform = auto
	in.Running = false
	return PlanWorkbench(in).Platforms
}

func (a *App) workbenchInput() WorkbenchInput {
	return WorkbenchInput{
		Running:       a.running,
		HasImages:     len(a.sources) > 0,
		HasPrompts:    len(a.taskPromptList()) > 0,
		AutoPlatform:  a.platformAuto,
		WantDoubao:    a.platDoubao,
		WantQianwen:   a.platQianwen,
		ExtensionMode: a.extensionTasks(),
		Accounts:      a.accountSnaps(),
		Now:           time.Now(),
	}
}

func (a *App) workbenchPlan() WorkbenchPlan {
	return PlanWorkbench(a.workbenchInput())
}

func (a *App) accountSnaps() []AccountSnap {
	rows := a.configuredBrowsers()
	out := make([]AccountSnap, 0, len(rows))
	for _, row := range rows {
		out = append(out, AccountSnap{
			Platform:  row.Platform,
			LoggedIn:  row.IsLoggedIn,
			Extension: row.ExtAccount != "",
			Health:    row.Health,
			CoolUntil: row.CoolUntil,
		})
	}
	return out
}

func (a *App) resolvedOutputDir() string {
	return DefaultOutputDir(a.outputPath, mustWd())
}

func (a *App) openOutputFolder() {
	dir, err := EnsureOutputDir(a.resolvedOutputDir())
	if err != nil {
		a.outputNote = "无法创建输出文件夹"
		logging.Log.Errorf("创建输出文件夹失败: %v", err)
		return
	}
	if err := mygo.Shell.OpenPath(dir); err != nil {
		a.outputNote = "无法打开输出文件夹"
		logging.Log.Errorf("打开输出文件夹失败: %v", err)
		return
	}
	a.outputNote = ""
}

func (a *App) queueItems() []tasks.Task {
	if a.engine != nil {
		if view := a.engine.View(); view != nil {
			return view.Snapshot()
		}
	}
	return a.queue
}

func (a *App) thumbFor(path string, px int) *ui.Bitmap {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	for _, si := range a.sources {
		if si.Path == path && si.Thumb != nil {
			return si.Thumb
		}
	}
	if a.thumbs == nil {
		a.thumbs = map[string]*ui.Bitmap{}
	}
	if bmp, ok := a.thumbs[path]; ok {
		return bmp
	}
	img, err := images.Thumb(path, px)
	if err != nil || img == nil {
		a.thumbs[path] = nil
		return nil
	}
	bmp := ui.NewBitmap(img)
	a.thumbs[path] = bmp
	return bmp
}

func loadSourceFiles(paths []string) []sourceImage {
	var loaded []sourceImage
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil || !images.IsValidImageBytes(data) {
			continue
		}
		thumb, _ := images.Thumb(p, 200)
		var bmp *ui.Bitmap
		if thumb != nil {
			bmp = ui.NewBitmap(thumb)
		}
		loaded = append(loaded, sourceImage{Path: p, Thumb: bmp, Hash: images.SHA256(data)})
	}
	return loaded
}

func (a *App) removeSourceAt(i int) {
	if a.running || i < 0 || i >= len(a.sources) {
		return
	}
	a.sources = append(a.sources[:i], a.sources[i+1:]...)
	if a.srcSel >= len(a.sources) {
		a.srcSel = len(a.sources) - 1
	}
}

func (a *App) canStart() bool {
	return a.workbenchPlan().Action == StartGenerate
}

func (a *App) startTasks() {
	plan := a.workbenchPlan()
	switch plan.Action {
	case StartWizard:
		a.progText = ""
		a.openWizard()
		return
	case StartNone:
		return
	}
	a.runQueue(plan.Platforms, a.taskPromptList(), a.batchImages(), nil)
}

func (a *App) retryTasks(items []tasks.Task) {
	if a.running || len(items) == 0 || a.engine == nil {
		return
	}
	in := a.workbenchInput()
	in.Running = false
	in.HasImages = true
	in.AutoPlatform = true
	if PlanWorkbench(in).Action != StartGenerate {
		a.progText = ""
		a.openWizard()
		return
	}
	platforms := map[string]bool{}
	for _, task := range items {
		if task.Platform != "" {
			platforms[task.Platform] = true
		}
	}
	var names []string
	for _, name := range []string{"豆包", "千问"} {
		if platforms[name] {
			names = append(names, name)
		}
	}
	a.runQueue(names, nil, nil, items)
}

func (a *App) runQueue(platforms, promptList, paths []string, retry []tasks.Task) {
	if a.engine == nil || a.running {
		return
	}
	dir, err := EnsureOutputDir(DefaultOutputDir(a.outputPath, mustWd()))
	if err != nil {
		a.progText = HumanReason(err.Error())
		logging.Log.Errorf("创建输出文件夹失败: %v", err)
		return
	}
	if strings.TrimSpace(a.outputPath) == "" {
		a.outputPath = dir
	}
	a.saveSettings()
	opts := a.taskOptions(platforms, promptList, paths)
	opts.OutputPath = dir
	a.running = true
	a.progress = 0
	a.progText = ""
	a.engine.SetSolver(captcha.Build(a.captchaConfig()))
	go func() {
		onTask := func(tasks.Task) { a.syncUI(func() {}) }
		onProgress := func(p tasks.Progress) {
			a.syncUI(func() {
				if p.Total > 0 {
					a.progress = float64(p.Done) / float64(p.Total)
				}
				a.progText = fmt.Sprintf("%d / %d", p.Done, p.Total)
			})
		}
		var runErr error
		if retry != nil {
			runErr = a.engine.Retry(context.Background(), opts, retry, onTask, onProgress)
		} else {
			runErr = a.engine.Run(context.Background(), opts, onTask, onProgress)
		}
		a.syncUI(func() {
			a.running = false
			if runErr != nil {
				a.progText = HumanReason(runErr.Error())
				logging.Log.Errorf("任务终止: %v", runErr)
			} else if view := a.engine.View(); view != nil {
				a.progText = taskResultSummary(view.Snapshot())
			}
		})
	}()
}

func (a *App) batchImages() []string {
	if len(a.sources) == 0 {
		return nil
	}
	paths := make([]string, 0, len(a.sources))
	for _, si := range a.sources {
		paths = append(paths, si.Path)
	}
	return paths
}
