package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/prompts"
)

func (a *App) promptsPage(c *ui.Context) {
	tok := a.uiTokens(c)
	w, _ := c.Size()
	if a.promptFilter == "" {
		a.promptFilter = "全部"
	}
	allItems := promptOptions(newPromptLabel, a.userPrompts)[1:]
	ui.Column(c).Grow(1).MinHeight(0).Gap(8).Children(func() {
		uiPageHeader(c, "prompts-page-header", iconPen, "提示词库", "管理生图、洗图与通用模板；编辑内置模板时自动另存为个人副本。", tok, func() {
			uiBadge(c, "prompts-total-badge", fmt.Sprintf("共 %d 条", len(allItems)), "neutral", tok)
			if len(a.userPrompts) > 0 {
				uiBadge(c, "prompts-custom-badge", fmt.Sprintf("%d 自定义", len(a.userPrompts)), "accent", tok)
			}
		})
		a.promptFilters(c)
		if w >= 720 {
			tableW := float32(280)
			if w < 900 {
				tableW = 248
			}
			ui.Row(c).Grow(1).MinHeight(0).Gap(8).AlignItems(ui.Stretch).Children(func() {
				a.promptTable(c).Width(tableW).Shrink(0)
				a.promptEditor(c).Grow(1).Basis(0).MinWidth(0)
			})
			return
		}
		ui.Scroll(c.Key("prompt-page-scroll")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				a.promptTable(c).FillWidth().Height(200)
				a.promptEditor(c).FillWidth().MinHeight(340)
			})
		})
	})
}

func (a *App) promptFilters(c *ui.Context) {
	tok := a.uiTokens(c)
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			for _, label := range []string{"全部", "生图", "洗图", "通用"} {
				selected := a.promptFilter == label
				uiPill(c, "prompt-filter-"+label, label, selected, tok).
					Label("筛选" + label).Disabled(a.running).
					OnClick(func() { a.promptFilter = label })
			}
		})
		ui.Spacer(c)
		ui.Text(c, fmt.Sprintf("%d 条", len(a.promptTableItems()))).FontSize(tok.caption).FontFeatures("tnum").TextColor(tok.muted).Shrink(0)
		uiIconBtn(c, "prompt-new", "新建提示词", "新建提示词", iconPlus, 24, true, false, tok).
			Disabled(a.running).OnClick(func() {
			a.promptEditChoice = newPromptLabel
			a.applyPromptChoice()
			if a.promptEditText == "" && a.promptEditName == "" && a.promptFilter != "全部" {
				a.promptEditKind = a.promptFilter
			}
		})
	})
}

func (a *App) promptTableItems() []prompts.Template {
	var items []prompts.Template
	for _, item := range promptOptions(newPromptLabel, a.userPrompts)[1:] {
		if a.promptFilter == "" || a.promptFilter == "全部" || promptKindLabel(item.Kind) == a.promptFilter {
			items = append(items, item)
		}
	}
	return items
}

func (a *App) promptTable(c *ui.Context) ui.Element {
	tok := a.uiTokens(c)
	panel := uiCard(c, "prompt-table", tok).Padding(0).Gap(0).MinHeight(0).Clip()
	panel.Children(func() {
		ui.Row(c).FillWidth().Padding(5, 8).Gap(6).AlignItems(ui.Center).Background(tok.sunken).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
			ui.Text(c, "名称").Grow(1).MinWidth(0).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted)
			ui.Text(c, "类型").Width(46).Shrink(0).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted)
			ui.Text(c, "来源").Width(44).Shrink(0).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted)
		})
		items := a.promptTableItems()
		if len(items) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Gap(3).Padding(12).Children(func() {
				ui.Text(c, "没有此类型的提示词").FontSize(tok.body).TextColor(tok.text)
				ui.Text(c, "选择其他类型，或新建提示词。").FontSize(tok.caption).TextColor(tok.muted)
			})
			return
		}
		ui.Scroll(c.Key("prompt-table-list")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for _, item := range items {
					a.promptTableRow(c, item)
				}
			})
		})
	})
	return panel
}

func (a *App) promptTableRow(c *ui.Context, item prompts.Template) {
	tok := a.uiTokens(c)
	selected := a.promptEditPick == item.ID
	row := ui.ButtonBase(c.Key("prompt-row-"+item.ID)).FillWidth().MinHeight(28).
		Padding(4, 8).Gap(6).AlignItems(ui.Center).Label("编辑 "+item.Label).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Disabled(a.running)
	if selected {
		row.Background(tok.accent.Alpha(0.10))
	} else if row.Hovered() {
		row.Background(tok.sunken)
	}
	if !a.running {
		row.Cursor(ui.CursorPointer).OnClick(func() {
			a.promptEditChoice = item.Label
			a.applyPromptChoice()
		})
	}
	row.Children(func() {
		bar := ui.Box(c).Width(2).Height(16).Radius(1).Shrink(0)
		if selected {
			bar.Background(tok.accent)
		}
		name := ui.Text(c, item.Label).Grow(1).MinWidth(0).FontSize(tok.body).SingleLine().Tooltip(item.Label)
		if selected {
			name.FontWeight(600).TextColor(tok.accent)
		} else {
			name.TextColor(tok.text)
		}
		kind := promptKindLabel(item.Kind)
		kindTone := "neutral"
		if kind == "生图" {
			kindTone = "accent"
		} else if kind == "洗图" {
			kindTone = "warning"
		}
		ui.Box(c).Width(46).Shrink(0).AlignItems(ui.Start).Children(func() {
			uiBadge(c, "prompt-kind-"+item.ID, kind, kindTone, tok)
		})
		source, srcTone := "我的", "accent"
		if item.Builtin {
			source, srcTone = "内置", "neutral"
		}
		ui.Box(c).Width(44).Shrink(0).AlignItems(ui.Start).Children(func() {
			uiBadge(c, "prompt-src-"+item.ID, source, srcTone, tok)
		})
	})
}

func (a *App) promptEditor(c *ui.Context) ui.Element {
	tok := a.uiTokens(c)
	if !containsStr([]string{"生图", "洗图", "通用"}, a.promptEditKind) {
		a.promptEditKind = "通用"
	}
	panel := uiCard(c, "prompt-editor", tok).MinHeight(0).Gap(8).Padding(10, 12)
	panel.Children(func() {
		title := "新建提示词"
		if a.promptEditPick != "" {
			title = "编辑提示词"
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			uiSectionTitle(c, title, "", tok)
			ui.Box(c).Grow(1).MinWidth(0)
			if prompts.IsBuiltin(a.promptEditPick) {
				uiBadge(c, "prompt-builtin-badge", "内置 · 修改后另存为副本", "neutral", tok)
			}
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, "名称").FontSize(tok.caption).TextColor(tok.muted)
				ui.TextInput(c.Key("prompt-name"), &a.promptEditName).Height(24).FillWidth().Label("提示词名称").
					Placeholder("留空时取正文第一行").Disabled(a.running)
			})
			ui.Column(c).Width(100).Shrink(0).Gap(3).Children(func() {
				ui.Text(c, "适用类型").FontSize(tok.caption).TextColor(tok.muted)
				motionSelect(c.Key("prompt-kind"), &a.promptEditKind, []string{"生图", "洗图", "通用"}).
					Height(24).FillWidth().Label("提示词类型").Disabled(a.running)
			})
		})
		ui.Text(c, "正文").FontSize(tok.caption).TextColor(tok.muted)
		ui.TextArea(c.Key("prompt-body"), &a.promptEditText).Grow(1).MinHeight(120).FillWidth().
			Label("提示词内容").Placeholder("输入提示词内容").Disabled(a.running)
		if a.promptEditNote != "" {
			noteFg := tok.muted
			if strings.Contains(a.promptEditNote, "已") {
				noteFg, _, _ = semanticToneColors(tok.dark, tok.text, tok.success, 0.14)
			}
			ui.Text(c, a.promptEditNote).FontSize(tok.caption).TextColor(noteFg)
		}
		ui.Row(c).FillWidth().Gap(6).Wrap().Padding(6, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tok.border).AlignItems(ui.Center).Children(func() {
			uiBtn(ui.PrimaryButton(c, "保存提示词"), tok).Disabled(a.running).OnClick(func() { a.saveCurrentPrompt() })
			if prompts.IsBuiltin(a.promptEditPick) {
				uiIconBtn(c, "prompt-restore", "恢复内置", "恢复内置默认正文", iconRotateCcw, 24, false, false, tok).
					Disabled(a.running).OnClick(func() { a.restoreBuiltinPrompt() })
			} else if a.promptEditPick != "" {
				uiIconBtn(c, "prompt-delete", "删除提示词", "删除此提示词", iconTrash, 24, false, false, tok).
					Disabled(a.running).OnClick(func() { a.deleteUserPrompt() })
			}
		})
	})
	return panel
}
