package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/prompts"
)

func (a *App) promptsPage(c *ui.Context) {
	w, _ := c.Size()
	tok := a.uiTokens(c)
	if a.promptFilter == "" {
		a.promptFilter = "全部"
	}
	ui.Column(c).Grow(1).MinHeight(0).Gap(0).Background(tok.card).Children(func() {
		if w >= 720 {
			tableW := float32(268)
			if w < 900 {
				tableW = 232
			}
			ui.Row(c).Grow(1).MinHeight(0).Gap(0).AlignItems(ui.Stretch).Children(func() {
				a.promptTable(c).Width(tableW).Shrink(0)
				a.promptEditor(c).Grow(1).Basis(0).MinWidth(0)
			})
			return
		}
		ui.Scroll(c.Key("prompt-page-scroll")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(0).Children(func() {
				a.promptTable(c).FillWidth().Height(220)
				a.promptEditor(c).FillWidth().MinHeight(340)
			})
		})
	})
}

func (a *App) promptFilters(c *ui.Context) {
	tok := a.uiTokens(c)
	ui.Row(c).FillWidth().Padding(8, 10).Gap(4).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
			for _, label := range []string{"全部", "生图", "洗图", "通用"} {
				selected := a.promptFilter == label
				uiPill(c, "prompt-filter-"+label, label, selected, tok).
					Padding(0, 7).Label("筛选" + label).Disabled(a.running).
					OnClick(func() { a.promptFilter = label })
			}
		})
		ui.Spacer(c)
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
	w, _ := c.Size()
	allItems := promptOptions(newPromptLabel, a.userPrompts)[1:]
	items := a.promptTableItems()
	panel := ui.Column(c.Key("prompt-table")).Padding(0).Gap(0).MinHeight(0).
		Background(tok.card).BorderColor(tok.border).Clip()
	if w >= 720 {
		panel.BorderWidth(0, 1, 0, 0)
	} else {
		panel.BorderWidth(0, 0, 1, 0)
	}
	panel.Children(func() {
		a.promptFilters(c)
		ui.Row(c).FillWidth().Padding(5, 10).Gap(6).AlignItems(ui.Center).Background(tok.paper).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
			ui.Text(c, "名称").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Shrink(0)
			ui.Text(c, fmt.Sprintf("%d/%d", len(items), len(allItems))).
				FontSize(tok.caption).FontFeatures("tnum").TextColor(tok.subtle).Grow(1).MinWidth(0)
			ui.Text(c, "来源").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Width(30).TextAlign(ui.End).Shrink(0)
			ui.Text(c, "类型").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Width(36).TextAlign(ui.End).Shrink(0)
		})
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
	row := ui.ButtonBase(c.Key("prompt-row-"+item.ID)).FillWidth().MinHeight(30).
		Padding(6, 10).Gap(6).AlignItems(ui.Center).Label("编辑 "+item.Label).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border.Alpha(0.55)).Disabled(a.running)
	if selected {
		row.Background(tok.accent.Alpha(0.08))
	} else if row.Hovered() {
		row.Background(tok.paper)
	}
	if !a.running {
		row.Cursor(ui.CursorPointer).OnClick(func() {
			a.promptEditChoice = item.Label
			a.applyPromptChoice()
		})
	}
	row.Children(func() {
		name := ui.Text(c, item.Label).Grow(1).MinWidth(0).FontSize(tok.body).SingleLine().Tooltip(item.Label)
		if selected {
			name.FontWeight(600).TextColor(tok.accent)
		} else {
			name.TextColor(tok.text)
		}
		if item.Builtin {
			ui.Text(c, "内置").FontSize(tok.caption).TextColor(tok.muted).Shrink(0)
		}
		kind := promptKindLabel(item.Kind)
		kindTone := "neutral"
		if kind == "生图" {
			kindTone = "accent"
		} else if kind == "洗图" {
			kindTone = "warning"
		}
		uiBadge(c, "prompt-kind-"+item.ID, kind, kindTone, tok)
	})
}

func (a *App) promptEditor(c *ui.Context) ui.Element {
	tok := a.uiTokens(c)
	if !containsStr([]string{"生图", "洗图", "通用"}, a.promptEditKind) {
		a.promptEditKind = "通用"
	}
	panel := ui.Column(c.Key("prompt-editor")).MinHeight(0).Gap(8).Padding(12, 16).Background(tok.card)
	panel.Children(func() {
		title := "新建提示词"
		if a.promptEditPick != "" {
			title = "编辑提示词"
		}
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			uiSectionTitle(c, title, "", tok)
			if prompts.IsBuiltin(a.promptEditPick) {
				uiBadge(c, "prompt-builtin-badge", "内置 · 修改后另存为副本", "neutral", tok)
			}
			ui.Spacer(c)
			if prompts.IsBuiltin(a.promptEditPick) {
				uiIconBtn(c, "prompt-restore", "恢复内置", "恢复内置默认正文", iconRotateCcw, 24, false, true, tok).
					Disabled(a.running).OnClick(func() { a.restoreBuiltinPrompt() })
			} else if a.promptEditPick != "" {
				uiIconBtn(c, "prompt-delete", "删除提示词", "删除此提示词", iconTrash, 24, false, true, tok).
					Disabled(a.running).OnClick(func() { a.deleteUserPrompt() })
			}
			uiPrimaryBtn(c, "prompt-save", "保存提示词", a.running, tok).
				Height(26).Padding(0, 12).OnClick(func() { a.saveCurrentPrompt() })
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "名称").FontSize(tok.caption).TextColor(tok.muted).Shrink(0)
			ui.TextInput(c.Key("prompt-name"), &a.promptEditName).Height(26).Grow(1).MinWidth(0).Label("提示词名称").
				Placeholder("留空时取正文第一行").Disabled(a.running)
			ui.Text(c, "适用类型").FontSize(tok.caption).TextColor(tok.muted).Shrink(0)
			motionSelect(c.Key("prompt-kind"), &a.promptEditKind, []string{"生图", "洗图", "通用"}).
				Height(26).Width(92).Shrink(0).Label("提示词类型").Disabled(a.running)
		})
		uiPromptArea(ui.TextArea(c.Key("prompt-body"), &a.promptEditText).Grow(1).MinHeight(140).FillWidth().
			Label("提示词内容").Placeholder("输入提示词内容").Disabled(a.running), tok)
		if a.promptEditNote != "" {
			noteFg := tok.muted
			if strings.Contains(a.promptEditNote, "已") {
				noteFg, _, _ = semanticToneColors(tok.dark, tok.text, tok.success, 0.14)
			}
			ui.Text(c, a.promptEditNote).FontSize(tok.caption).TextColor(noteFg)
		}
	})
	return panel
}
