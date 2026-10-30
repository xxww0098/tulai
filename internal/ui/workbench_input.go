package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
)

// workColumns 左边是这一页的输入、开始和运行日志，右边是平台、账号和出图结果。
func (a *App) workColumns(c *ui.Context, input func()) {
	w, _ := c.Size()
	if homeSplit(w) {
		ui.Column(c).Grow(1).MinHeight(0).Gap(10).Children(func() {
			a.loginBanner(c)
			ui.Row(c).Grow(1).MinHeight(0).Gap(homeGap).AlignItems(ui.Stretch).Children(func() {
				a.workSide(c, input)
				a.homeStage(c)
			})
		})
	} else {
		ui.Scroll(c.Key("work-narrow")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Padding(4, 0, 8, 0).Children(func() {
				a.loginBanner(c)
				input()
				a.runCard(c)
				a.platformStage(c)
				a.runLog(c, false)
			})
		})
	}
	a.previewModal(c)
}

// workSide 输入在上，内容多时自己滚动；开始卡片始终露出，运行日志撑满左栏剩下的高度。
func (a *App) workSide(c *ui.Context, input func()) {
	_, winH := c.Size()
	ui.Column(c.Key("work-side")).Width(composerW).Shrink(0).Height(winH-48).Gap(10).Padding(0, 0, 8, 0).Children(func() {
		ui.Scroll(c.Key("work-side-input")).FillWidth().Shrink(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(input)
		})
		a.runCard(c)
		a.runLog(c, true)
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
	card := a.stepCard(c, "prompt-focus", stepCurrent)
	if paths := card.DroppedFiles(); len(paths) > 0 {
		a.ingestPromptFiles(paths)
	}
	card.Children(func() {
		n := len(prompts.TaskPrompts(a.promptPick, a.prompts))
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "提示词").FontSize(tok.title).Bold().Shrink(0)
			hint := "每行一条，空行隔开的段落收成一条"
			if a.promptPick != "" {
				hint = "当前模板整段发送"
			}
			ui.Text(c, hint).FontSize(tok.caption).TextColor(tok.muted).Grow(1).MinWidth(0).SingleLine()
			if n > 0 {
				ui.Text(c, fmt.Sprintf("%d 条", n)).FontSize(tok.caption).TextColor(tok.muted).Shrink(0).
					FontFeatures("tnum")
			}
		})
		ui.TextArea(c, &a.prompts).Height(168).FillWidth().Label("提示词内容").
			Placeholder("输入或粘贴。也可以把 txt 拖到这里").Disabled(a.running)
		if a.promptPick != "" {
			line := "模板「" + a.promptChoice + "」整段一起发送"
			ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(line)
		}
		if a.promptNote != "" {
			ui.Text(c, a.promptNote).FontSize(tok.caption).TextColor(tok.muted)
		}
		ui.Row(c).FillWidth().Gap(6).Justify(ui.End).Children(func() {
			if a.promptPick != "" {
				a.quietButton(c, "按行发送").OnClick(func() { a.useLinePrompts() })
			}
			a.quietButton(c, "从文件载入").OnClick(func() { a.loadPromptFile() })
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
	card := a.stepCard(c, key, state)
	if paths := card.DroppedFiles(); len(paths) > 0 {
		a.ingestSources(paths)
	}
	if card.FileDragOver() && !a.running {
		card.Border(2, tok.accent).Background(tok.accent.Alpha(0.08))
	}
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(tok.title).Bold().Shrink(0)
			if !primary {
				ui.Text(c, "可选").FontSize(tok.caption).TextColor(tok.muted).Shrink(0)
			}
			if n := len(a.sources); n > 0 {
				ui.Text(c, fmt.Sprintf("已选 %d 张", n)).FontSize(tok.caption).TextColor(tok.muted).
					Shrink(0).FontFeatures("tnum")
			}
			ui.Row(c).Grow(1).MinWidth(0)
			a.imageActions(c)
		})
		if len(a.sources) == 0 {
			a.compactDrop(c, primary)
			return
		}
		if primary {
			a.sourceGrid(c)
			return
		}
		a.thumbStrip(c)
	})
}

func (a *App) compactDrop(c *ui.Context, primary bool) {
	tok := a.look
	h := float32(72)
	hint := "不放图片也可以只写提示词"
	if primary {
		h = 132
		hint = "可以一次选多张，或选整个文件夹"
	}
	zone := ui.Column(c.Key("drop-zone")).FillWidth().Height(h).Radius(tok.radiusM).
		Border(1.5, tok.accent.Alpha(0.45)).BorderStyle(ui.BorderDashed).
		Background(tok.sunken).Center().Gap(4).Cursor(ui.CursorPointer)
	if zone.Hovered() && !a.running {
		zone.Border(1.5, tok.accent).Background(tok.accent.Alpha(0.08))
	}
	hoverPressPose(zone)
	if zone.Clicked() && !a.running {
		a.addImages()
	}
	zone.Children(func() {
		ui.Text(c, "拖入或点击添加图片").FontSize(tok.body).Bold()
		ui.Text(c, hint).FontSize(tok.caption).TextColor(tok.muted)
	})
}

func (a *App) thumbStrip(c *ui.Context) {
	ui.ScrollHorizontal(c).FillWidth().Height(48).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			for i := range a.sources {
				a.miniThumb(c, i)
			}
		})
	})
}

func (a *App) imageActions(c *ui.Context) {
	a.quietButton(c, "添加图片").OnClick(func() { a.addImages() })
	if a.washing() {
		a.quietButton(c, "选文件夹").OnClick(func() { a.addFolder() })
	}
	if len(a.sources) > 0 {
		a.quietButton(c, "清空").OnClick(func() { a.clearSources() })
	}
}

// quietButton 是输入区右侧的小按钮，不跟「开始」抢一整行。
func (a *App) quietButton(c *ui.Context, label string) ui.Element {
	return ui.Button(c, label).FontSize(a.look.caption).Padding(3, 8).Radius(a.look.radiusS).
		Shrink(0).Disabled(a.running)
}

func (a *App) runCard(c *ui.Context) {
	tok := a.look
	plan := a.workbenchPlan()
	card := a.stepCard(c, "step-run", stepPending)
	card.Children(func() {
		if !a.running && (plan.Action == StartWizard || a.loggedOutCallout()) {
			a.accountCallout(c)
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
	open := func() { a.tab = pageSettings }
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
	a.commitPromptEdits()
	a.promptPick = ""
	a.promptChoice = prompts.LabelCustom
	a.freeformPrompts = a.prompts
	a.promptNote = ""
	a.persistPromptLibrary()
}

func (a *App) loadPromptText(raw string) {
	a.commitPromptEdits()
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

func (a *App) loadPromptFile() {
	if a.running {
		return
	}
	window := a.window
	page := a.statePage
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: window, Title: "载入提示词", Multiple: true,
			Filters: []mygo.FileFilter{{Name: "文本", Extensions: []string{"txt"}}},
		})
		if err != nil {
			logging.Log.Errorf("选择提示词文件失败: %v", err)
			return
		}
		text := readPromptFiles(paths)
		if strings.TrimSpace(text) == "" {
			return
		}
		apply := func() {
			a.onPage(page, func() { a.loadPromptText(text) })
		}
		if window != nil {
			window.Update(apply)
			return
		}
		apply()
	}()
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
