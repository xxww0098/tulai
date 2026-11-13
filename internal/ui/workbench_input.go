package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
)

// workColumns 左边是这一页的输入与开始卡片，右边是账号队列和出图结果；运行日志已移至顶部弹出。
func (a *App) workColumns(c *ui.Context, input func()) {
	w, _ := c.Size()
	if homeSplit(w) {
		ui.Column(c).Grow(1).MinHeight(0).Gap(6).Children(func() {
			a.loginBanner(c)
			ui.Row(c).Grow(1).MinHeight(0).Gap(homeGap).AlignItems(ui.Stretch).Children(func() {
				a.workSide(c, input)
				a.homeStage(c)
			})
		})
	} else {
		ui.Scroll(c.Key("work-narrow")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Padding(2, 0, 4, 0).Children(func() {
				a.loginBanner(c)
				a.platformPicker(c)
				input()
				a.runCard(c)
				a.platformStage(c)
			})
		})
	}
	a.previewModal(c)
	a.galleryModal(c)
	a.promptPartsModal(c)
}

// workSide 输入在上并撑满可用高度，开始卡片固定贴底。
func (a *App) workSide(c *ui.Context, input func()) {
	ui.Column(c.Key("work-side")).Width(composerW).Shrink(0).FillHeight().MinHeight(0).Gap(6).Children(func() {
		ui.Scroll(c.Key("work-side-input")).FillWidth().Grow(1).MinHeight(0).
			TrackScroll(&a.workInputScroll).Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(input)
		})
		a.runCard(c)
	})
}

func (a *App) genInput(c *ui.Context) {
	a.promptFocus(c)
	a.refImageCard(c)
}

func (a *App) washInput(c *ui.Context) {
	a.washImageCard(c)
	a.washPromptCard(c)
}

// promptFocus 是生图的主输入：直接写、粘贴，或从 txt 载入。
func (a *App) promptFocus(c *ui.Context) {
	tok := a.look
	_, winH := c.Size()
	inputHeight := float32(148)
	if winH < 720 {
		inputHeight = 116
	}
	card := a.stepCard(c, "prompt-focus", stepCurrent)
	if paths := card.DroppedFiles(); len(paths) > 0 {
		a.ingestPromptFiles(paths)
	}
	card.Children(func() {
		n := len(prompts.TaskPrompts(a.promptPick, a.prompts))
		if n == 0 && a.hasTaskPrompt() {
			n = 1
		}
		hint := "每行一条；导入段落会合并"
		if a.promptPick != "" {
			hint = "当前模板整段发送"
		}
		ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "提示词").FontSize(tok.title).Bold().Shrink(0)
			ui.Box(c).Shrink(0).Tooltip(hint).Children(func() {
				ui.Icon(c, iconInfo).FontSize(12).TextColor(tok.muted)
			})
			ui.Spacer(c)
			if n > 0 {
				ui.Row(c).Height(16).Padding(0, 5).Radius(999).Center().Background(tok.sunken).Shrink(0).Children(func() {
					ui.Text(c, fmt.Sprintf("%d 条", n)).FontSize(tok.caption).TextColor(tok.muted).
						SingleLine().FontFeatures("tnum")
				})
			}
		})
		ui.TextArea(c, &a.prompts).Height(inputHeight).FillWidth().Label("提示词内容").
			Placeholder("输入提示词，每行一条；也可拖入 txt").Disabled(a.running)
		if a.promptPick != "" {
			line := "模板「" + a.promptChoice + "」整段一起发送"
			ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(line)
		}
		if a.promptNote != "" {
			ui.Text(c, a.promptNote).FontSize(tok.caption).TextColor(tok.muted)
		}
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			a.workspacePromptSelect(c)
			ui.Spacer(c)
			if a.promptPick != "" {
				uiIconBtn(c, "prompt-by-line", "按行发送", "切回按行发送", iconListOrdered, 22, false, false, tok).
					Disabled(a.running).OnClick(func() { a.useLinePrompts() })
			}
			a.promptPartsButton(c)
		})
	})
}

func (a *App) refImageCard(c *ui.Context) {
	a.imageCard(c, "ref-images", "参考图", false)
}

func (a *App) washImageCard(c *ui.Context) {
	a.imageCard(c, "wash-images", "原图", true)
}

// imageCard 收图片。洗图时它是主输入；生图里参考图是次要的。
func (a *App) imageCard(c *ui.Context, key, title string, primary bool) {
	tok := a.look
	state := stepDone
	if primary {
		state = stepCurrent
	}
	card := a.stepCard(c, key, state).Label(title + "输入区")
	if paths := card.DroppedFiles(); len(paths) > 0 {
		a.ingestSources(paths)
	}
	if card.FileDragOver() && !a.running {
		card.Border(2, tok.accent).Background(tok.accent.Alpha(0.08))
	}
	card.Children(func() {
		ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(tok.title).Bold().Shrink(0)
			if n := len(a.sources); n > 0 {
				ui.Row(c).Height(16).Padding(0, 5).Radius(999).Center().Background(tok.sunken).Shrink(0).Children(func() {
					ui.Text(c, fmt.Sprintf("%d 张", n)).FontSize(tok.caption).TextColor(tok.muted).
						SingleLine().FontFeatures("tnum")
				})
			} else if !primary {
				ui.Text(c, "可选").FontSize(tok.caption).TextColor(tok.muted).Shrink(0)
			}
			ui.Spacer(c)
			a.imageActions(c, title, true)
		})
		if primary && a.washing() && len(a.sources) > 0 {
			washed, pending := a.washCounts()
			ui.Text(c, fmt.Sprintf("已洗 %d · 待洗 %d", washed, pending)).
				FontSize(tok.caption).TextColor(tok.muted).SingleLine().FontFeatures("tnum")
		}
		if len(a.sources) == 0 {
			a.compactDrop(c, primary)
			return
		}
		if primary && a.washing() {
			a.washSourceGrid(c)
			return
		}
		a.thumbStrip(c)
	})
}

func (a *App) compactDrop(c *ui.Context, primary bool) {
	tok := a.look
	h := float32(44)
	hint := ""
	if primary {
		h = 68
		hint = "支持多张或整文件夹"
		if a.fissioning() {
			hint = "选 1 张母图生成 4 张不同角度"
		}
	}
	zone := ui.Column(c.Key("drop-zone")).Label("拖入或点击添加图片").FillWidth().Height(h).Radius(tok.radiusM).
		Border(1.5, tok.accent.Alpha(0.42)).BorderStyle(ui.BorderDashed).
		Background(tok.sunken).Center().Gap(2).Cursor(ui.CursorPointer)
	if !primary {
		zone.Tooltip("可选，不放图也可纯提示词生成")
	}
	if zone.Hovered() && !a.running {
		zone.Border(1.5, tok.accent).Background(tok.accent.Alpha(0.08))
	}
	hoverPressPose(zone)
	if zone.Clicked() && !a.running {
		a.addImages()
	}
	zone.Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconImagePlus).FontSize(13).TextColor(tok.accent)
			ui.Text(c, "拖入或点击添加图片").FontSize(tok.body).FontWeight(500)
		})
		if hint != "" {
			ui.Text(c, hint).FontSize(tok.caption).TextColor(tok.muted)
		}
	})
}

func (a *App) thumbStrip(c *ui.Context) {
	ui.ScrollHorizontal(c).FillWidth().Height(34).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			for i := range a.sources {
				a.miniThumb(c, i)
			}
		})
	})
}

func (a *App) imageActions(c *ui.Context, title string, _ bool) {
	tok := a.look
	action := func(label string, icon *ui.SVG, click func()) {
		uiIconBtn(c, "image-action-"+label, label, label, icon, 22, false, false, tok).
			Disabled(a.running).OnClick(click)
	}
	if len(a.sources) > 0 {
		action("图库", iconImages, func() { a.openImageGallery(title) })
	}
	action("添加图片", iconImagePlus, a.addImages)
	if a.washing() {
		action("选文件夹", iconFolderOpen, a.addFolder)
	}
	if len(a.sources) > 0 {
		action("清空", iconTrash, a.clearSources)
	}
}

// quietButton 是输入区右侧的小按钮，不跟「开始」抢一整行。
func (a *App) quietButton(c *ui.Context, label string) ui.Element {
	return ui.Button(c, label).FontSize(a.look.caption).Padding(2, 6).Radius(a.look.radiusS).
		Shrink(0).Disabled(a.running)
}

func (a *App) runCard(c *ui.Context) {
	tok := a.look
	plan := a.workbenchPlan()
	card := a.stepCard(c, "step-run", stepPending).Gap(4)
	card.Children(func() {
		target := "本次发送至：" + a.stagePlatform()
		if a.running {
			target = "当前查看：" + a.stagePlatform()
		}
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, target).FontSize(tok.caption).Bold().TextColor(tok.accent).Grow(1).MinWidth(0).SingleLine().Tooltip(target)
			if a.stagePlatform() == "豆包" {
				a.outputSizeSelect(c)
			}
		})
		if a.stagePlatform() == "豆包" {
			a.outputSizeFields(c)
		}
		if a.washing() {
			a.washStyleSelect(c)
		}
		board := filterBoard(a.accountBoard(c.Now()), a.stagePlatform())
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Clip().Children(func() {
				summary := a.stepLead(board)
				ui.Text(c, summary).FontSize(tok.body).MinWidth(0).SingleLine().Tooltip(summary)
				if note := a.runNote(); note != "" {
					ui.Text(c.Key("prog"), note).FontSize(tok.caption).TextColor(tok.muted).MinWidth(0).SingleLine().Tooltip(note)
				}
				a.outputHint(c, tok)
			})
			a.runButton(c, plan)
		})
	})
}

// outputHint 只读一行。点它去设置，不在工作页改路径。
func (a *App) outputHint(c *ui.Context, tok tokens) {
	path := a.resultFolder()
	line := "保存到 " + path
	open := func() { a.settingsTab, a.tab = settingsOutput, pageSettings }
	row := ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Cursor(ui.CursorPointer)
	if row.Clicked() {
		open()
	}
	row.Children(func() {
		paintFolder(c, tok.accent)
		text := ui.Text(c, line).FontSize(tok.caption).TextColor(tok.accent).MinWidth(0).SingleLine().
			Cursor(ui.CursorPointer).Tooltip("在设置中修改")
		if text.Clicked() {
			open()
		}
	})
}

func (a *App) useLinePrompts() {
	if a.running {
		return
	}
	a.promptPick = ""
	a.promptChoice = prompts.LabelCustom
	a.freeformPrompts = a.prompts
	a.promptNote = ""
	a.persistPromptLibrary()
}

func (a *App) loadPromptText(raw string) {
	a.promptPick = ""
	a.promptChoice = prompts.LabelCustom
	a.prompts = prompts.ImportText(raw)
	a.freeformPrompts = a.prompts
	n := len(prompts.TaskPrompts("", a.prompts))
	if n == 0 {
		a.promptNote = "文件里没有提示词"
	} else {
		a.promptNote = fmt.Sprintf("已载入 %d 条提示词", n)
	}
	a.persistPromptLibrary()
}

func (a *App) ingestPromptFiles(paths []string) {
	if a.running || len(paths) == 0 {
		return
	}
	text := readPromptFiles(paths)
	if strings.TrimSpace(text) == "" {
		return
	}
	a.loadPromptText(text)
}

func readPromptFiles(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		if strings.ToLower(filepath.Ext(p)) != ".txt" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			logging.Log.Errorf("读取提示词文件失败: %v", err)
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.Write(data)
	}
	return b.String()
}
