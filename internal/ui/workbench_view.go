package ui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

const (
	settingsOutput = iota
	settingsDownload
	settingsAccount
	settingsAgy
	settingsRules
	settingsAdvanced
	settingsUpdate
)

func (a *App) settingsPage(c *ui.Context) {
	a.persistAgySettings()
	if a.proxyURL != a.proxySaved {
		a.proxySaved = a.proxyURL
		a.persistProxy()
	}
	ui.Scroll(c.Key("settings-page")).Grow(1).MinHeight(0).Children(func() {
		// Scroll 是盒子，Justify 不生效；用撑满的行把限宽的内容列居中。
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(720).Gap(12).Padding(4, 0, 20, 0).Children(func() {
				ui.Text(c, "设置").FontSize(22).Bold()
				ui.Text(c, "在这里管理保存路径、并发和生成规则。").FontSize(13).TextColor(c.Theme().TextMuted)
				ui.Tabs(c.Key("settings-tabs"), &a.settingsTab, "输出", "下载", "账号与并发", "Antigravity", "生成规则", "验证码 / 高级", "更新").FillWidth().Wrap()
				switch a.settingsTab {
				case settingsOutput:
					a.settingsCard(c, "输出", "生图和洗图按日期放进各自的文件夹。", func() { a.outputSettings(c) })
				case settingsDownload:
					a.settingsCard(c, "下载", "下载并发、重试、限速和网页图片筛选。", func() { a.downloadSettings(c) })
				case settingsAccount:
					a.settingsCard(c, "账号与并发", "用哪些平台、同时跑几个账号、每天的上限。", func() { a.accountSettings(c) })
				case settingsAgy:
					a.settingsCard(c, "Antigravity", "agy 路径、账号目录、模型和单任务超时。一次只跑一个账号。", func() { a.agySettings(c) })
				case settingsRules:
					a.settingsCard(c, "生成规则", "发送节奏、审核改写、等待和去水印。", func() { a.ruleSettings(c) })
				case settingsAdvanced:
					a.settingsCard(c, "验证码 / 高级", "遇到验证时怎么处理，以及浏览器走的代理。", func() {
						a.captchaFields(c)
						ui.Text(c, "网络").Bold().Margin(8, 0, 0, 0)
						ui.Text(c, "Camoufox 安装使用此代理；账号可在浏览器页设置独立代理。").FontSize(12).TextColor(c.Theme().TextMuted)
						a.proxyFields(c)
					})
				case settingsUpdate:
					a.settingsCard(c, "更新", "检查新版本，连不上 GitHub 时走镜像。", func() { a.updateSettings(c) })
				}
			})
		})
	})
}

func (a *App) settingsCard(c *ui.Context, title, blurb string, body func()) {
	tok := a.look
	ui.Column(c).FillWidth().Gap(8).Padding(12, 14).Radius(tok.radiusL).
		Background(tok.card).Border(1, tok.border).Children(func() {
		ui.Text(c, title).FontSize(tok.title).Bold()
		if blurb != "" {
			ui.Text(c, blurb).FontSize(tok.caption).TextColor(tok.muted)
		}
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

func (a *App) outputSettings(c *ui.Context) {
	ui.Form(c, func() {
		ui.Field(c, "输出目录", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.outputPath).Grow(1).MinWidth(0).Placeholder("留空则保存到输出结果")
				ui.Button(c, "选择…").Disabled(a.running).OnClick(func() { a.pickOutputDir() })
			})
		})
		root := a.resolvedOutputDir()
		gen := tasks.Options{OutputPath: root}.ResultDir(time.Now())
		wash := tasks.Options{OutputPath: root, Wash: true}.ResultDir(time.Now())
		a.outputPathLine(c, "生图保存到 "+gen, gen)
		a.outputPathLine(c, "洗图保存到 "+wash, wash)
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c, &a.genAutoNumber, "生图自动编号").Disabled(a.running)
			ui.Checkbox(c, &a.washAutoNumber, "洗图自动编号").Disabled(a.running)
		})
		if a.genAutoNumber || a.washAutoNumber {
			ui.Field(c, "序号前缀", func() {
				ui.TextInput(c, &a.numPrefix).Grow(1).MinWidth(0).Placeholder("留空则只存序号，如 001.png").Disabled(a.running)
			})
			ui.Field(c, "编号设置", func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.NumberInput(c, &a.numStart, 0, 9_000_000_000_000_000, 1).Label("编号起始值").Disabled(a.running)
					ui.NumberInput(c, &a.numWidth, 0, 16, 1).Label("补零位数").Disabled(a.running)
				})
			})
			ui.Text(c, "开启自动编号后，文件按「前缀+序号」命名（如图001.png），同一天同前缀接着排号，起始值只在没有更大的号时生效，不覆盖已有文件。").FontSize(12).TextColor(c.Theme().TextMuted)
		} else {
			ui.Text(c, "同一天的结果放进同一个日期文件夹，文件从 _01 起编，不覆盖已有文件。").FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if a.outputNote != "" {
			ui.Text(c, a.outputNote).FontSize(12).TextColor(c.Theme().Danger)
		}
	}).FillWidth().Gap(8)
}

func (a *App) outputPathLine(c *ui.Context, line, dir string) {
	text := ui.Text(c, line).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(line).Cursor(ui.CursorPointer)
	if text.Clicked() {
		a.openResultFolder(dir)
	}
}

// downloadSettings 是结果图直接下载的通道。浏览器页面里 fetch 拿不到时才会走它。
func (a *App) downloadSettings(c *ui.Context) {
	ui.Form(c, func() {
		ui.Field(c, "每个站点同时下载", func() {
			ui.NumberInput(c, &a.downloadConc, 1, 8, 1).Disabled(a.running)
		})
		ui.Text(c, "同一个站点同时下几张结果图。调大更快，也更容易被限流。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "失败重试", func() {
			ui.NumberInput(c, &a.downloadRetries, 0, 10, 1).Disabled(a.running)
		})
		ui.Text(c, "网络中断或服务器 5xx 时自动重试，服务器给了等待时间就按它的来。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "限速 (KB/s)", func() {
			ui.NumberInput(c, &a.downloadRateKB, 0, 102400, 64).Disabled(a.running)
		})
		ui.Text(c, "0 表示不限制。只在本机直接下载时生效，页面内取回的图不受影响。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, "网页图片筛选").Bold().Margin(8, 0, 0, 0)
		ui.Text(c, "只作用于下载页的新任务；0 表示不限。").FontSize(12).TextColor(c.Theme().TextMuted)
		for _, row := range []struct {
			label, minLabel, maxLabel string
			min, max                  *float64
		}{
			{"文件大小 (KB)", "最小文件大小", "最大文件大小", &a.dlMinKB, &a.dlMaxKB},
			{"图片宽度 (px)", "最小图片宽度", "最大图片宽度", &a.dlMinWidth, &a.dlMaxWidth},
			{"图片高度 (px)", "最小图片高度", "最大图片高度", &a.dlMinHeight, &a.dlMaxHeight},
		} {
			ui.Field(c, row.label, func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.NumberInput(c, row.min, 0, 10000000, 1).Label(row.minLabel)
					ui.Text(c, "至").TextColor(c.Theme().TextMuted)
					ui.NumberInput(c, row.max, 0, 10000000, 1).Label(row.maxLabel)
				})
			})
		}
		if err := a.downloadImageFilter().Validate(); err != nil {
			ui.Text(c, err.Error()).FontSize(12).TextColor(c.Theme().Danger)
		}
	}).FillWidth().Gap(8)
}

func (a *App) accountSettings(c *ui.Context) {
	tok := a.look
	boundBridge := a.store != nil && a.store.Get("browserBackend") == "camoufox_extension"
	ui.Form(c, func() {
		ui.Text(c, "额度用尽、限流或验证码时，自动换到同平台下一个健康账号。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Checkbox(c, &boundBridge, "绑定账号使用 Firefox 页面桥").Disabled(a.running || a.store == nil).OnChange(func() {
			if a.store == nil {
				return
			}
			mode := "camoufox"
			if boundBridge {
				mode = "camoufox_extension"
			}
			a.store.Set("browserBackend", mode)
		})
		ui.Text(c, "使用已有账号的指纹和登录状态，页面桥随任务启动。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Checkbox(c, &a.platformAuto, "自动使用已登录的平台").Disabled(a.running)
		if !a.platformAuto {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				a.platformCheck(c, "豆包", &a.platDoubao)
				a.platformCheck(c, "千问", &a.platQianwen)
				a.platformCheck(c, agy.Platform, &a.platAntigravity)
				a.platformCheck(c, "ChatGPT", &a.platChatGPT)
			})
		}
		ui.Text(c, a.platformLine()).FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Text(c, fmt.Sprintf("豆包、千问、ChatGPT 每个平台同时最多 %d 张。Antigravity 一次只跑一个账号。点开始时只跑当前这一边的账号。", tasks.PlatformInFlight)).FontSize(12).TextColor(tok.muted)
		ui.Field(c, "ChatGPT 模型", func() {
			motionSelect(c, &a.chatGPTModel, []string{"gpt-image-2", "auto"}).Disabled(a.running)
		})
		ui.Field(c, "每日上限", func() {
			ui.NumberInput(c, &a.dailyCap, 1, 200, 1).Disabled(a.running)
		})
		ui.Text(c, "每个账号每天最多出这么多次，到了就换下一个账号。").FontSize(12).TextColor(c.Theme().TextMuted)
	}).FillWidth().Gap(8)
}

func (a *App) platformCheck(c *ui.Context, name string, on *bool) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		platformIcon(c, name, 16, a.look)
		ui.Checkbox(c, on, name).Disabled(a.running).OnChange(func() { a.platformAuto = false })
	})
}

func (a *App) ruleSettings(c *ui.Context) {
	ui.Form(c, func() {
		ui.Field(c, "后缀", func() {
			ui.TextInput(c, &a.promptSuffix)
		})
		ui.Checkbox(c, &a.useSuffix, "附加提示词后缀")
		ui.Checkbox(c, &a.removeWatermark, "去水印（实验）").Disabled(a.running)
		ui.Text(c, "豆包自动去水印后保存；勾选后千问方图另存修复 PNG。未匹配时保留原图，复杂背景可能有痕迹。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "发送策略", func() {
			motionSelect(c, &a.sendStrategy, []string{"顺序循环", "随机循环"}).Disabled(a.running)
		})
		ui.Field(c, "失败重试", func() {
			ui.NumberInput(c, &a.retryCount, 0, 20, 1).Disabled(a.running)
		})
		ui.Field(c, "发送间隔(秒)", func() {
			ui.NumberInput(c, &a.sendWait, 0, 600, 1).Disabled(a.running)
		})
		ui.Text(c, "实际等待会在这个秒数上下浮动，避免每次都卡在同一拍。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "审核改写词", func() {
			ui.TextInput(c, &a.riskyWords).Placeholder("逗号分隔").Disabled(a.running)
		})
		ui.Text(c, "审核不通过时去掉这些词，并加上温和说法，最多自动重试 2 次。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "发送前等待(秒)", func() {
			ui.NumberInput(c, &a.waitBefore, 0, 60, 1).Disabled(a.running)
		})
		ui.Field(c, "等待结果", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Switch(c, &a.waitResult).Disabled(a.running)
				if a.waitResult {
					ui.NumberInput(c, &a.waitResultS, 10, 1800, 10).Disabled(a.running)
				}
			})
		})
		ui.Field(c, "定时切换浏览器", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.Switch(c, &a.taskSwitch).Disabled(a.running)
				if a.taskSwitch {
					ui.NumberInput(c, &a.switchCount, 1, 9999, 1).Disabled(a.running)
					ui.Text(c, "个任务").FontSize(12).TextColor(c.Theme().TextMuted)
				}
			})
		})
		ui.Text(c, "达到任务数后优先切换到其他空闲账号；没有其他空闲账号时继续使用当前账号。").FontSize(12).TextColor(c.Theme().TextMuted)
		ui.Field(c, "起始浏览器序号", func() {
			ui.NumberInput(c, &a.startBrowserIdx, 1, 999, 1).Disabled(a.running)
		})
	}).FillWidth().Gap(8)
}

func (a *App) platformLine() string {
	names := a.carryingPlatforms(true)
	if a.platformAuto {
		if len(names) == 0 {
			return "当前还没有已登录的账号"
		}
		return "当前将使用：" + strings.Join(names, "、")
	}
	if !a.platDoubao && !a.platQianwen && !a.platAntigravity && !a.platChatGPT {
		return "请勾选豆包、千问、Antigravity 或 ChatGPT"
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
		Running:         a.running,
		HasImages:       len(a.sources) > 0,
		HasPrompts:      a.hasTaskPrompt(),
		AutoPlatform:    a.platformAuto,
		WantDoubao:      a.platDoubao,
		WantQianwen:     a.platQianwen,
		WantAntigravity: a.platAntigravity,
		WantChatGPT:     a.platChatGPT,
		ExtensionMode:   a.extensionTasks(),
		Accounts:        a.accountSnaps(),
		Now:             time.Now(),
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
	return 1
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
	wantDoubao := a.platformAuto || a.platDoubao || stage == "豆包"
	wantQianwen := a.platformAuto || a.platQianwen || stage == "千问"
	wantAgy := a.platformAuto || a.platAntigravity || stage == agy.Platform
	wantChatGPT := a.platformAuto || a.platChatGPT || stage == "ChatGPT"
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
			if extension != (b.ExtAccount != "") {
				continue
			}
		case "千问":
			if !wantQianwen {
				continue
			}
			if extension != (b.ExtAccount != "") {
				continue
			}
		case agy.Platform:
			if !wantAgy {
				continue
			}
		case "ChatGPT":
			if !wantChatGPT || b.ExtAccount != "" {
				continue
			}
		default:
			continue
		}
		rows = append(rows, b)
	}
	n := tasks.ParallelSlots(rows, now, a.shownDailyCap())
	if stage == agy.Platform && n > 1 {
		return 1
	}
	return n
}

func (a *App) accountSnaps() []AccountSnap {
	rows := a.configuredBrowsers()
	out := make([]AccountSnap, 0, len(rows))
	for _, row := range rows {
		out = append(out, AccountSnap{
			Platform:  row.Platform,
			LoggedIn:  row.IsLoggedIn,
			Extension: row.ExtAccount != "",
			Disabled:  row.Disabled,
			Health:    row.Health,
			CoolUntil: row.CoolUntil,
		})
	}
	return out
}

func (a *App) resolvedOutputDir() string {
	return DefaultOutputDir(a.outputPath, mustWd())
}

// resultFolder 是当前页今天的结果目录。生图和洗图各看各的。
func (a *App) resultFolder() string {
	return tasks.Options{OutputPath: a.resolvedOutputDir(), Wash: a.washing()}.ResultDir(time.Now())
}

func (a *App) openOutputFolder() {
	a.openResultFolder(a.resultFolder())
}

func (a *App) openResultFolder(dir string) {
	dir, err := EnsureOutputDir(dir)
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
	// 引擎只有一份队列。正在跑的这一页看引擎；停下来以后看这一页自己收好的那份。
	// 新建任务清掉的是这一页的队列，不能再把上一批的引擎快照画出来。
	if a.useRunView && a.running && a.engine != nil && a.ownsRun() {
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
	return !a.renameUpdateActive() && len(a.browserPending) == 0 && a.workbenchPlan().Action == StartGenerate
}

func (a *App) startTasks() {
	if a.renameUpdateActive() || len(a.browserPending) > 0 {
		return
	}
	plan := a.workbenchPlan()
	if plan.Action != StartGenerate {
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
		a.progText = "没有可用账号，请先登录后重试"
		return
	}
	platforms := map[string]bool{}
	for _, task := range items {
		if task.Platform != "" {
			platforms[task.Platform] = true
		}
	}
	var names []string
	for _, name := range wizard.Platforms() {
		if platforms[name] {
			names = append(names, name)
		}
	}
	a.runQueue(names, nil, nil, items)
}

func (a *App) runQueue(platforms, promptList, paths []string, retry []tasks.Task) {
	if a.engine == nil || a.running || a.renameUpdateActive() || len(a.browserPending) > 0 {
		return
	}
	if retry == nil {
		if err := a.currentPromptParts().validate(); err != nil {
			a.progText = err.Error()
			logging.Log.Errorf("%s", a.progText)
			return
		}
	}
	for _, platform := range platforms {
		if platform == "豆包" {
			if err := a.doubaoOutputSize().Validate(); err != nil {
				a.progText = "豆包输出尺寸：" + err.Error()
				logging.Log.Errorf("%s", a.progText)
				return
			}
			break
		}
	}
	if !a.extensionTasks() {
		for _, platform := range platforms {
			if platform != "豆包" && platform != "千问" && platform != "ChatGPT" {
				continue
			}
			if _, ready := browser.InstalledCamoufox(browser.ConfigDir()); !ready {
				a.refreshDetected()
				if !a.camoufoxBusy && browser.CamoufoxSupported(runtime.GOOS, runtime.GOARCH) {
					a.camoufoxStatus = "请先安装 Camoufox，再返回任务页重试"
				}
				a.tab = pageBrowser
				return
			}
			break
		}
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
	opts.Numbering = a.numberingFor(page)
	if page == pageWash {
		opts.Wash = true
		opts.UseSuffix = false
	}
	previousView := a.engine.View()
	previous := a.stateOf(page)
	previousRunPage, previousUseRunView := a.runPage, a.useRunView
	a.runPage = page
	a.running = true
	a.useRunView = false
	if retry != nil {
		pending := tasks.RebuildRetry(retry)
		a.onPage(page, func() {
			a.queue = pending
			a.progress = 0
			if len(pending) > 0 {
				a.progText = fmt.Sprintf("0 / %d", len(pending))
			} else {
				a.progText = ""
			}
		})
	} else {
		a.progress = 0
		a.progText = ""
	}
	if !a.persistWorkspace(page) {
		a.onPage(page, func() {
			a.queue, a.progress = previous.queue, previous.progress
			a.progText = "保存任务记录失败，未开始任务，请检查配置数据库"
		})
		a.running = false
		a.runPage, a.useRunView = previousRunPage, previousUseRunView
		return
	}
	a.engine.SetSolver(captcha.Build(a.captchaConfig()))
	go func() {
		onTask := func(tasks.Task) {
			a.postUI(func() {
				a.useRunView = true
				a.persistWorkspace(page)
			})
		}
		onProgress := func(p tasks.Progress) {
			a.postUI(func() {
				a.useRunView = true
				a.onPage(page, func() {
					if p.Total > 0 {
						a.progress = float64(p.Done) / float64(p.Total)
					}
					a.progText = fmt.Sprintf("%d / %d", p.Done, p.Total)
				})
				a.persistWorkspace(page)
			})
		}
		var runErr error
		if retry != nil {
			runErr = a.engine.Retry(context.Background(), opts, retry, onTask, onProgress)
		} else {
			runErr = a.engine.Run(context.Background(), opts, onTask, onProgress)
		}
		a.postUI(func() {
			a.running = false
			a.useRunView = false
			if a.browserDrawer != nil && a.manager != nil && !a.manager.IsRunning(a.browserDrawer.row.ID) {
				a.closeBrowserDrawer()
			}
			if a.manager != nil {
				for i := range a.browserRows {
					if a.browserRows[i].ExtAccount == "" {
						a.browserRows[i].IsRunning = a.manager.IsRunning(a.browserRows[i].ID)
					}
				}
			}
			a.onPage(page, func() {
				if view := a.engine.View(); view != nil && view != previousView {
					a.queue = view.Snapshot()
				} else {
					a.queue, a.progress = previous.queue, previous.progress
				}
				if runErr != nil {
					a.progText = HumanReason(runErr.Error())
					logging.Log.Errorf("%s任务终止: %v", pageLabel(page), runErr)
				} else {
					a.progText = taskResultSummary(a.queue)
				}
			})
			a.persistWorkspace(page)
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
