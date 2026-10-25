package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"xbs/internal/cdp"
	"xbs/internal/logging"
	"xbs/internal/store"
	"xbs/internal/strategy"
	"xbs/internal/tasks"
)

// settingsPane 左侧设置区（页签：任务 / 浏览器 / 验证码）。
func (a *App) settingsPane(c *ui.Context) {
	ui.Tabs(c, &a.tab, "任务设置", "浏览器", "验证码")
	switch a.tab {
	case 1:
		a.browserPane(c)
	case 2:
		a.captchaPane(c)
	default:
		a.taskPane(c)
	}
}

// taskPane 保存位置和默认折叠的调度选项。平台和提示词在右侧工作区。
func (a *App) taskPane(c *ui.Context) {
	ui.Scroll(c.Key("task-settings")).Grow(1).MinHeight(0).Children(func() {
		ui.Form(c, func() {
			ui.Text(c, "平台和提示词在右侧，填好即可开始。").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Fieldset(c, "保存", func() {
				ui.Field(c, "输出目录", func() {
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.outputPath).Grow(1).MinWidth(0).Placeholder("留空则保存到输出结果")
						ui.Button(c, "选择…").OnClick(func() { a.pickOutputDir() })
					})
				})
			}).Gap(8).Margin(8, 0, 0, 0)
			ui.Collapsible(c, "更多设置", &a.moreSettings, func() {
				ui.Field(c, "保存方式", func() {
					ui.Select(c, &a.saveMode, []string{"单文件夹汇总", "按源图片分文件夹"})
				})
				ui.Field(c, "命名规则", func() {
					ui.Select(c, &a.namingRule, []string{"原文件名", "时间戳", "序号"})
				})
				ui.Field(c, "文件名前缀", func() {
					ui.Row(c).Gap(6).Children(func() {
						ui.TextInput(c, &a.filePrefix).Grow(1).MinWidth(0)
						ui.Checkbox(c, &a.prefixOn, "启用")
					})
				})
				ui.Field(c, "文件名后缀", func() {
					ui.Row(c).Gap(6).Children(func() {
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
				ui.Text(c, "调度与等待").Bold().Margin(8, 0, 0, 0)
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
			})
		}).FillWidth().Gap(8)
	})
}

// browserPane 浏览器管理。
func (a *App) browserPane(c *ui.Context) {
	ui.Field(c, "网页方式", func() {
		ui.Select(c.Key("browser-backend"), &a.browserBackend, []string{"ego lite", "Chrome"}).Disabled(a.running || a.egoOpening).OnChange(func() {
			a.store.Set("browser_backend", a.browserBackend)
			a.egoMessage = ""
		})
	})
	if a.browserBackend != "Chrome" {
		ui.Column(c).Grow(1).MinHeight(0).Gap(12).Children(func() {
			ui.Row(c).Children(func() { ui.Badge(c, "已启用 ego lite") })
			ui.Text(c, "复用 ego lite 中的豆包、千问网页和现有登录状态，无需创建独立浏览器实例。").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Button(c, "打开豆包网页").Disabled(a.running || a.egoOpening).OnClick(func() { a.openEgoPlatform("豆包") })
			ui.Button(c, "打开千问网页").Disabled(a.running || a.egoOpening).OnClick(func() { a.openEgoPlatform("千问") })
			ui.Text(c, "请保持 ego lite 运行，在网页中登录后返回工作台开始任务。").FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Scroll(c.Key("ego-status")).Grow(1).MinHeight(0).Children(func() {
				message := a.egoMessage
				if message == "" {
					message = "已保存当前方式；连接会在打开网页或开始任务时建立。"
				}
				ui.Text(c, message).FontSize(12)
			})
		})
		return
	}
	ui.Column(c).Grow(1).MinHeight(0).Gap(8).Children(func() {
		ui.Text(c, "浏览器实例").Bold()
		ui.Text(c, "每个实例独立保存登录状态").FontSize(12).TextColor(c.Theme().TextMuted)
		// 新增行
		ui.Row(c).Gap(6).Children(func() {
			ui.Select(c, &a.newBrowserPlat, []string{"豆包", "千问"}).Width(80).Label("浏览器平台")
			ui.TextInput(c, &a.newBrowserName).Grow(1).MinWidth(0).Placeholder("名称（可选）").Label("浏览器名称")
			ui.Button(c, "添加").Disabled(a.running).OnClick(func() { a.addBrowser() })
		})
		if len(a.browserRows) == 0 {
			ui.Text(c, "尚无浏览器，添加后启动并登录平台").Padding(12, 0).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		// 列表
		a.browserList.Key = func(i int) any { return a.browserRows[i].ID }
		a.browserList.Selected = &a.browserSel
		ui.List(c, &a.browserList, len(a.browserRows), func(i int) {
			b := a.browserRows[i]
			ui.Column(c).Padding(10).Gap(5).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, b.Name).Bold().Grow(1).SingleLine()
					state := "未运行"
					if b.IsRunning {
						state = "运行中"
					}
					ui.Badge(c, state)
				})
				ui.Text(c, fmt.Sprintf("%s · 端口 %d", b.Platform, b.Port)).FontSize(12).TextColor(c.Theme().TextMuted)
			})
		}).Grow(1).MinHeight(0).Border(1, c.Theme().Border).Radius(8)
		selected, hasSelection := a.selectedBrowser()
		ui.Row(c).Gap(6).Children(func() {
			ui.Button(c, "启动选中").Disabled(a.running || !hasSelection || selected.IsRunning).OnClick(func() { a.launchBrowser() })
			ui.Button(c, "停止选中").Disabled(a.running || !hasSelection || !selected.IsRunning).OnClick(func() { a.stopBrowser() })
			ui.Button(c, "刷新列表").OnClick(func() { a.refreshBrowsers() })
		})
		ui.Button(c, "删除选中").Disabled(a.running || !hasSelection).TextColor(c.Theme().Danger).OnClick(func() { a.deleteBrowser() })
		ui.Text(c, "首次使用：选中实例 → 启动 → 登录平台").FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

// captchaPane 验证码设置。
func (a *App) captchaPane(c *ui.Context) {
	ui.Scroll(c.Key("captcha-settings")).Grow(1).MinHeight(0).Children(func() {
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
		})
	})
}

// bottomView 队列表格 + 日志。
func (a *App) bottomView(c *ui.Context) {
	// 队列快照
	snap := a.engine.View()
	var items []tasks.Task
	if snap != nil {
		items = snap.Snapshot()
	} else {
		items = a.queue
	}
	ui.Column(c).Grow(1).MinHeight(0).Padding(14).Gap(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "任务队列").Bold()
			ui.Badge(c, fmt.Sprintf("%d 项", len(items)))
		})
		if len(items) == 0 {
			empty := "写下提示词并开始后，每条的状态会显示在这里"
			if a.batchMode == batchImage {
				empty = "添加源图并开始后，每张图片的状态会显示在这里"
			}
			ui.Column(c).Grow(1).Center().Gap(6).Children(func() {
				ui.Text(c, "任务会显示在这里").Bold()
				ui.Text(c, empty).FontSize(12).TextColor(c.Theme().TextMuted)
			})
			return
		}
		cols := []ui.TableColumn{
			{ID: "idx", Title: "#", Width: 36},
			{ID: "plat", Title: "平台", Width: 56},
			{ID: "status", Title: "状态", Width: 68},
			{ID: "img", Title: "源图", MinWidth: 76},
			{ID: "prompt", Title: "提示词", MinWidth: 76},
			{ID: "try", Title: "次数", Width: 48},
			{ID: "result", Title: "结果", Width: 56},
			{ID: "err", Title: "说明", MinWidth: 100},
		}
		a.queueTbl.Key = func(i int) any { return items[i].Index }
		ui.Table(c, &a.queueTbl, cols, len(items), func(row, col int) {
			t := items[row]
			text := ""
			color := c.Theme().Text
			switch cols[col].ID {
			case "idx":
				text = fmt.Sprint(t.Index)
			case "plat":
				text = t.Platform
			case "prompt":
				text = t.Prompt
			case "img":
				if len(t.Images) > 0 {
					text = baseName(t.Images[0])
				}
			case "status":
				text = string(t.Status)
				switch t.Status {
				case tasks.StatusFailed:
					color = c.Theme().Danger
				case tasks.StatusSuccess:
					color = c.Theme().Success
				case tasks.StatusNeedsUser:
					color = c.Theme().Warning
				}
			case "try":
				text = fmt.Sprint(t.Attempts)
			case "result":
				text = fmt.Sprintf("%d 张", len(t.Results))
			case "err":
				text = t.Error
			}
			ui.Text(c, text).FontSize(12).TextColor(color).SingleLine().Tooltip(text)
		}).Grow(1).MinHeight(0)
	})
	ui.Column(c).Grow(0.65).MinHeight(0).Padding(14).Gap(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() {
		ui.Text(c, "运行日志").Bold()
		if len(a.logs) == 0 {
			ui.Text(c, "操作与任务进展会记录在这里").FontSize(12).TextColor(c.Theme().TextMuted)
			return
		}
		a.logList.Key = nil // 追加式，无稳定行键
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
			ui.Row(c).Gap(8).Padding(3, 0).Children(func() {
				ui.Text(c, e.Time.Format("15:04:05")).FontSize(11).TextColor(c.Theme().TextMuted)
				ui.Text(c, e.Level.String()).FontSize(12).TextColor(color)
				ui.Text(c, e.Text).FontSize(12).Grow(1).MinWidth(0).SingleLine().Tooltip(e.Text)
			})
		}).Grow(1).MinHeight(0)
	})
}

// ---- 浏览器操作 ----

// openSelectedPages 从工作台打开已选平台。ego lite 直接开网页；Chrome 切到浏览器页。
func (a *App) openSelectedPages() {
	if a.running || a.egoOpening {
		return
	}
	if a.browserBackend == "Chrome" {
		a.tab = 1
		return
	}
	var names []string
	if a.platDoubao {
		names = append(names, "豆包")
	}
	if a.platQianwen {
		names = append(names, "千问")
	}
	if len(names) == 0 {
		a.egoMessage = "请先勾选豆包或千问"
		return
	}
	a.egoOpening = true
	a.egoMessage = "正在通过 ego lite 打开网页…"
	window := a.window
	go func() {
		var failed string
		var openErr error
		var opened []string
		for _, name := range names {
			platform := strategy.Factory(name)
			if platform == nil {
				continue
			}
			if err := cdp.OpenEgoURL(platform.HomeURL()); err != nil {
				if openErr == nil {
					openErr = err
					failed = name
				}
				continue
			}
			opened = append(opened, name)
		}
		if window == nil {
			return
		}
		window.Update(func() {
			a.egoOpening = false
			switch {
			case openErr != nil && len(opened) == 0:
				a.egoMessage = fmt.Sprintf("打开%s失败：%v", failed, openErr)
				logging.Log.Errorf("在 ego lite 打开%s失败: %v", failed, openErr)
			case openErr != nil:
				a.egoMessage = fmt.Sprintf("已打开%s；打开%s失败：%v", strings.Join(opened, "、"), failed, openErr)
				logging.Log.Errorf("在 ego lite 打开%s失败: %v", failed, openErr)
			default:
				a.egoMessage = fmt.Sprintf("已在 ego lite 打开%s网页，请在网页中确认登录状态。", strings.Join(opened, "、"))
				logging.Log.Success("已在 ego lite 打开%s网页", strings.Join(opened, "、"))
			}
		})
	}()
}

func (a *App) openEgoPlatform(name string) {
	platform := strategy.Factory(name)
	if platform == nil {
		logging.Log.Errorf("无法识别平台: %s", name)
		return
	}
	a.egoOpening = true
	a.egoMessage = fmt.Sprintf("正在通过 ego lite 打开%s网页…", name)
	window := a.window
	go func() {
		err := cdp.OpenEgoURL(platform.HomeURL())
		window.Update(func() { a.reportEgoOpen(name, err) })
	}()
}

func (a *App) reportEgoOpen(name string, err error) {
	a.egoOpening = false
	if err != nil {
		a.egoMessage = fmt.Sprintf("打开%s失败：%v", name, err)
		logging.Log.Errorf("在 ego lite 打开%s失败: %v", name, err)
		return
	}
	a.egoMessage = fmt.Sprintf("已在 ego lite 打开%s网页，请在网页中确认登录状态。", name)
	logging.Log.Success("已在 ego lite 打开%s网页", name)
}

func (a *App) refreshBrowsers() {
	a.mu.Lock()
	a.browserRows = a.store.Browsers()
	for i := range a.browserRows {
		a.browserRows[i].IsRunning = a.manager.IsRunning(a.browserRows[i].ID)
	}
	a.mu.Unlock()
}

func (a *App) addBrowser() {
	name := a.newBrowserName
	if name == "" {
		name = fmt.Sprintf("浏览器%d", len(a.browserRows)+1)
	}
	if _, err := a.store.AddBrowser(a.newBrowserPlat, name, ""); err != nil {
		logging.Log.Errorf("添加浏览器失败: %v", err)
		return
	}
	logging.Log.Info("已添加浏览器 %s（%s）", name, a.newBrowserPlat)
	a.refreshBrowsers()
}

func (a *App) selectedBrowser() (store.Browser, bool) {
	if a.browserSel < 0 || a.browserSel >= len(a.browserRows) {
		return store.Browser{}, false
	}
	return a.browserRows[a.browserSel], true
}

func (a *App) launchBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	platform := strategy.Factory(b.Platform)
	if platform == nil {
		logging.Log.Errorf("无法识别浏览器平台: %s", b.Platform)
		return
	}
	go func() {
		ws, err := a.manager.Start(b, a.dataRoot, false)
		if err != nil {
			logging.Log.Errorf("启动失败: %v", err)
			return
		}
		err = cdp.OpenURL(ws, platform.HomeURL())
		a.store.UpdateBrowser(b.ID, true, b.IsLoggedIn)
		if err != nil {
			logging.Log.Errorf("浏览器[%s]已启动，但打开%s失败: %v", b.Name, b.Platform, err)
		} else {
			logging.Log.Success("浏览器[%s]已打开%s，可在浏览器中登录账号", b.Name, b.Platform)
		}
		a.window.Update(func() { a.refreshBrowsers() })
	}()
}

func (a *App) stopBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	a.manager.Stop(b.ID)
	a.store.UpdateBrowser(b.ID, false, false)
	logging.Log.Info("浏览器[%s] 已停止", b.Name)
	a.refreshBrowsers()
}

func (a *App) deleteBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	a.manager.Stop(b.ID)
	a.store.DeleteBrowser(b.ID)
	logging.Log.Info("浏览器[%s] 已删除", b.Name)
	a.browserSel = -1
	a.refreshBrowsers()
}

func (a *App) pickOutputDir() {
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    window,
			Title:     "选择输出目录",
			Directory: true,
		})
		if err != nil {
			logging.Log.Errorf("选择输出目录失败: %v", err)
			return
		}
		if len(paths) > 0 {
			window.Update(func() { a.outputPath = paths[0] })
		}
	}()
}

// ---- 小工具 ----

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func pickStr(v, def string, allowed []string) string {
	if containsStr(allowed, v) {
		return v
	}
	return def
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
