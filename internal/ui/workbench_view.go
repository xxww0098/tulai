package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func (a *App) promptsPage(c *ui.Context) {
	w, _ := c.Size()
	if homeSplit(w) {
		ui.Row(c).Grow(1).MinHeight(0).Gap(20).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(300).Shrink(1).MinWidth(240).Gap(12).Children(func() {
				a.promptsHead(c)
				a.promptControls(c, true)
				a.promptNoteLine(c)
			})
			ui.Column(c).Grow(1).MinWidth(0).MaxWidth(640).MinHeight(0).Children(func() {
				ui.TextArea(c, &a.prompts).Grow(1).MinHeight(220).FillWidth().Label("提示词内容").Placeholder("每行一条。选了模板后，整段一起发送")
			})
		})
		return
	}
	ui.Column(c).Fill().MaxWidth(720).Gap(14).Children(func() {
		a.promptsHead(c)
		a.promptRow(c)
		ui.TextArea(c, &a.prompts).Grow(1).MinHeight(220).Label("提示词内容").Placeholder("每行一条。选了模板后，整段一起发送")
		a.promptNoteLine(c)
	})
}

func (a *App) promptsHead(c *ui.Context) {
	ui.Text(c, "提示词").FontSize(22).Bold()
	ui.Text(c, "内置模板和保存过的提示词。出图页会记住上次的选择。").FontSize(13).TextColor(c.Theme().TextMuted)
}

func (a *App) promptNoteLine(c *ui.Context) {
	if a.promptNote == "" {
		return
	}
	ui.Text(c, a.promptNote).FontSize(12).TextColor(c.Theme().TextMuted)
}

func (a *App) settingsPage(c *ui.Context) {
	w, _ := c.Size()
	ui.Scroll(c.Key("settings-page")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).MaxWidth(920).Gap(16).Padding(4, 0, 20, 0).Children(func() {
			ui.Text(c, "设置").FontSize(22).Bold()
			ui.Text(c, "验证码在这里调。平台和输出文件夹在出图页的「更多设置」里。").FontSize(13).TextColor(c.Theme().TextMuted)
			cards := func() {
				a.settingsCard(c, "更新", "检查新版本，连不上 GitHub 时走镜像。", func() { a.updateSettings(c) })
				a.settingsCard(c, "验证码", "出图遇到验证时怎么处理。", func() { a.captchaFields(c) })
				a.settingsCard(c, "网络", "浏览器和内置浏览器下载走同一条代理。", func() { a.proxyFields(c) })
			}
			if homeSplit(w) {
				ui.Row(c).Gap(16).AlignItems(ui.Start).Children(cards)
				return
			}
			ui.Column(c).FillWidth().Gap(16).Children(cards)
		})
	})
}

func (a *App) settingsCard(c *ui.Context, title, blurb string, body func()) {
	tok := a.look
	ui.Column(c).Grow(1).MinWidth(280).Gap(10).Padding(14, 16).Radius(tok.radiusL).
		Background(tok.card).Border(1, tok.border).Children(func() {
		ui.Text(c, title).FontSize(tok.title).Bold()
		ui.Text(c, blurb).FontSize(tok.caption).TextColor(tok.muted)
		body()
	})
}

type captchaOpt struct {
	id, label, hint string
}

var captchaOpts = []captchaOpt{
	{"free", "免费离线识别", "在本机识别九宫格和拼图，不联网。"},
	{"custom", "自定义视觉模型", "把截图发给兼容 OpenAI 的视觉模型。"},
	{"ikunocr", "ikunocr 接码平台", "交给 ikunocr 接码平台处理。"},
}

func captchaLabel(id string) string {
	for _, opt := range captchaOpts {
		if opt.id == id {
			return opt.label
		}
	}
	if id == "" {
		return captchaOpts[0].label
	}
	return id
}

func captchaID(label string) string {
	for _, opt := range captchaOpts {
		if opt.label == label {
			return opt.id
		}
	}
	return "free"
}

func captchaHint(id string) string {
	for _, opt := range captchaOpts {
		if opt.id == id {
			return opt.hint
		}
	}
	return ""
}

func captchaLabels() []string {
	out := make([]string, len(captchaOpts))
	for i, opt := range captchaOpts {
		out[i] = opt.label
	}
	return out
}

func (a *App) proxyFields(c *ui.Context) {
	if a.proxyURL != a.proxySaved {
		a.proxySaved = a.proxyURL
		a.persistProxy()
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		a.proxyModeButton(c, "跟随系统", false)
		a.proxyModeButton(c, "自定义代理", true)
	})
	if !a.proxyCustom {
		ui.Text(c, "未填写时使用 HTTPS_PROXY、ALL_PROXY 或 HTTP_PROXY。").FontSize(12).TextColor(c.Theme().TextMuted)
		return
	}
	ui.TextInput(c, &a.proxyURL).Width(280).Placeholder("http://127.0.0.1:7890")
	raw := strings.TrimSpace(a.proxyURL)
	if raw != "" {
		if _, ok := browser.NormalizeProxy(raw); !ok {
			ui.Text(c, "格式应为 http://主机:端口 或 socks5://主机:端口").FontSize(12).TextColor(c.Theme().Warning)
		}
	} else {
		ui.Text(c, "例如 socks5://127.0.0.1:1080。改完后重新打开浏览器。").FontSize(12).TextColor(c.Theme().TextMuted)
	}
}

func (a *App) proxyModeButton(c *ui.Context, label string, custom bool) {
	on := a.proxyCustom == custom
	b := ui.Button(c, label)
	if on {
		b.Border(1, c.Theme().Accent).TextColor(c.Theme().Accent)
	}
	b.OnClick(func() {
		if a.proxyCustom == custom {
			return
		}
		a.proxyCustom = custom
		a.proxySaved = a.proxyURL
		a.persistProxy()
	})
}

func (a *App) captchaFields(c *ui.Context) {
	if label := captchaLabel(a.captchaBackend); a.captchaChoice != label {
		a.captchaChoice = label
	}
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		ui.Checkbox(c, &a.captchaEnabled, "自动求解验证码")
		ui.Field(c, "求解方式", func() {
			motionSelect(c, &a.captchaChoice, captchaLabels()).FillWidth().OnChange(func() {
				a.captchaBackend = captchaID(a.captchaChoice)
			})
		})
		if hint := captchaHint(a.captchaBackend); hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		switch a.captchaBackend {
		case "custom":
			ui.Field(c, "API 地址", func() {
				ui.TextInput(c, &a.customURL)
			})
			ui.Field(c, "API Key", func() {
				ui.TextInput(c, &a.customKey).Password()
			})
			ui.Field(c, "模型", func() {
				ui.TextInput(c, &a.customModel)
			})
		case "ikunocr":
			ui.Field(c, "API Key", func() {
				ui.TextInput(c, &a.ikunocrKey).Password()
			})
		}
	})
}

// advancedFields 是出图页里收起来的「更多设置」。平台勾选、运行日志和并发不在这里。
func (a *App) advancedFields(c *ui.Context) {
	if a.platformAuto {
		a.platDoubao, a.platQianwen = true, true
		a.platformAuto = false
	}
	ui.Form(c, func() {
		ui.Text(c, "用已登录的账号。额度用尽、限流或验证码时，自动换到同平台下一个健康账号。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(16).Children(func() {
			ui.Checkbox(c, &a.platDoubao, "豆包").Disabled(a.running).OnChange(func() { a.platformAuto = false })
			ui.Checkbox(c, &a.platQianwen, "千问").Disabled(a.running).OnChange(func() { a.platformAuto = false })
		})
		ui.Text(c, a.platformLine()).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "输出目录", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.outputPath).Grow(1).MinWidth(0).Placeholder("留空则保存到输出结果")
				ui.Button(c, "选择…").Disabled(a.running).OnClick(func() { a.pickOutputDir() })
			})
		})
		resolved := a.resolvedOutputDir()
		ui.Text(c, "图片会保存到 "+resolved).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(resolved)
		if a.outputNote != "" {
			ui.Text(c, a.outputNote).FontSize(12).TextColor(c.Theme().Danger)
		}
		ui.Field(c, "保存方式", func() {
			motionSelect(c, &a.saveMode, []string{"单文件夹汇总", "按源图片分文件夹"})
		})
		ui.Field(c, "命名规则", func() {
			motionSelect(c, &a.namingRule, []string{"原文件名", "时间戳", "序号"})
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
			motionSelect(c, &a.sendStrategy, []string{"顺序循环", "随机循环"})
		})
		ui.Text(c, "在豆包或千问页点开始，只跑这一边的账号，各同时出 2 张。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "失败重试", func() {
			ui.NumberInput(c, &a.retryCount, 0, 20, 1)
		})
		ui.Field(c, "发送间隔(秒)", func() {
			ui.NumberInput(c, &a.sendWait, 0, 600, 1)
		})
		ui.Text(c, "实际等待会在这个秒数上下浮动，避免每次都卡在同一拍。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "每日上限", func() {
			ui.NumberInput(c, &a.dailyCap, 1, 200, 1)
		})
		ui.Text(c, "每个账号每天最多出这么多次，到了就换下一个账号。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "审核改写词", func() {
			ui.TextInput(c, &a.riskyWords).Placeholder("逗号分隔")
		})
		ui.Text(c, "审核不通过时去掉这些词，并加上温和说法，最多自动重试 2 次。").FontSize(12).TextColor(c.Theme().TextMuted)
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
	for i, line := range notes[start:] {
		ui.Text(c.Key(noteLine{start + i}), line).FontSize(12).SingleLine().Tooltip(line).Transition(motionItem())
	}
}

type noteLine struct{ i int }

func (a *App) logPane(c *ui.Context) {
	if len(a.logs) == 0 {
		ui.Text(c, "操作与任务进展会记录在这里").FontSize(12).TextColor(c.Theme().TextMuted)
		return
	}
	a.logList.Key = func(i int) any { return i }
	a.logList.FollowEnd = true
	a.logList.FollowAnimation = &motionEaseOut
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
	if a != nil {
		in.OnlyPlatform = a.stagePlatform()
	}
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
	in := a.workbenchInput()
	if a != nil {
		in.OnlyPlatform = a.stagePlatform()
	}
	return PlanWorkbench(in)
}

func (a *App) taskConcurrency() int {
	if a.concurrencyAuto {
		return 0
	}
	return clampInt(int(a.concurrency), 1, 16)
}

func (a *App) shownDailyCap() int {
	n := 0
	if a != nil {
		n = int(a.dailyCap)
	}
	if n <= 0 {
		return tasks.DefaultDailyCap
	}
	if n > 200 {
		return 200
	}
	return n
}

// parallelSlots 是当前勾选方式下能同时出图的账号数，和调度用的名额一致。
func (a *App) parallelSlots() int {
	now := time.Now()
	extension := a.extensionTasks()
	stage := ""
	if a != nil {
		stage = a.stagePlatform()
	}
	wantDoubao := a.platformAuto || a.platDoubao
	wantQianwen := a.platformAuto || a.platQianwen
	var rows []store.Browser
	for _, b := range a.configuredBrowsers() {
		if stage != "" && b.Platform != stage {
			continue
		}
		switch b.Platform {
		case "豆包":
			if !wantDoubao {
				continue
			}
		case "千问":
			if !wantQianwen {
				continue
			}
		default:
			continue
		}
		if extension != (b.ExtAccount != "") {
			continue
		}
		rows = append(rows, b)
	}
	return tasks.ParallelSlots(rows, now, a.shownDailyCap())
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
	// 引擎只有一份队列。另一页跑过的批次不显示在这一页，这一页看自己收好的那份。
	if a.engine != nil && a.ownsRun() {
		if view := a.engine.View(); view != nil {
			return view.Snapshot()
		}
	}
	return a.queue
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
	page := a.statePage
	if page == pageWash {
		opts.Wash = true
		opts.UseSuffix = false
		opts.SaveMode = "单文件夹汇总"
	}
	// 上一批属于别的页时，把它的进度收回那一页再接手引擎。
	if a.runPage != page && a.engine.View() != nil {
		snap := a.engine.View().Snapshot()
		a.onPage(a.runPage, func() { a.queue = snap })
	}
	a.runPage = page
	a.running = true
	a.progress = 0
	a.progText = ""
	a.engine.SetSolver(captcha.Build(a.captchaConfig()))
	go func() {
		onTask := func(tasks.Task) { a.syncUI(func() {}) }
		onProgress := func(p tasks.Progress) {
			a.syncUI(func() {
				a.onPage(page, func() {
					if p.Total > 0 {
						a.progress = float64(p.Done) / float64(p.Total)
					}
					a.progText = fmt.Sprintf("%d / %d", p.Done, p.Total)
				})
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
			a.onPage(page, func() {
				if view := a.engine.View(); view != nil {
					a.queue = view.Snapshot()
				}
				if runErr != nil {
					a.progText = HumanReason(runErr.Error())
					logging.Log.Errorf("%s任务终止: %v", pageLabel(page), runErr)
				} else {
					a.progText = taskResultSummary(a.queue)
				}
			})
			a.maybeInstallPending()
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
