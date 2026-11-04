package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/prompts"
)

func (a *App) promptsPage(c *ui.Context) {
	w, _ := c.Size()
	if a.promptFilter == "" {
		a.promptFilter = "全部"
	}
	ui.Column(c).Grow(1).MinHeight(0).Gap(14).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, "提示词").FontSize(22).Bold()
				ui.Text(c, "按用途管理模板，在图片生成页的对应模式中快速选择。通用模板两边都可使用。").FontSize(12).TextColor(a.look.muted).SingleLine()
			})
			ui.PrimaryButton(c, "新建提示词").Shrink(0).Disabled(a.running).OnClick(func() {
				a.promptEditChoice = newPromptLabel
				a.applyPromptChoice()
				if a.promptEditText == "" && a.promptEditName == "" && a.promptFilter != "全部" {
					a.promptEditKind = a.promptFilter
				}
			})
		})
		a.promptFilters(c)
		if homeSplit(w) {
			ui.Row(c).Grow(1).MinHeight(0).Gap(14).AlignItems(ui.Stretch).Children(func() {
				a.promptTable(c).Grow(1).Basis(0).MinWidth(0)
				a.promptEditor(c).Grow(1).Basis(0).MinWidth(0)
			})
			return
		}
		ui.Scroll(c.Key("prompt-page-scroll")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(14).Children(func() {
				a.promptTable(c).FillWidth().Height(250)
				a.promptEditor(c).FillWidth().Height(430)
			})
		})
	})
}

func (a *App) promptFilters(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		for _, label := range []string{"全部", "生图", "洗图", "通用"} {
			selected := a.promptFilter == label
			b := ui.Button(c, label).Label("筛选"+label).FontSize(12).Padding(5, 12).Disabled(a.running)
			if selected {
				b.Background(a.look.accent.Alpha(0.12)).TextColor(a.look.accent).Border(1, a.look.accent.Alpha(0.35))
			}
			b.OnClick(func() { a.promptFilter = label })
		}
		ui.Spacer(c)
		ui.Text(c, fmt.Sprintf("%d 条", len(a.promptTableItems()))).FontSize(12).TextColor(a.look.muted).Shrink(0)
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
	tok := a.look
	panel := ui.Column(c.Key("prompt-table")).MinHeight(0).Radius(tok.radiusL).
		Background(tok.card).Border(1, tok.border).Clip()
	panel.Children(func() {
		ui.Row(c).FillWidth().Padding(11, 14).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
			ui.Text(c, "名称").Grow(1).MinWidth(0).FontSize(12).TextColor(tok.muted)
			ui.Text(c, "类型").Width(58).Shrink(0).FontSize(12).TextColor(tok.muted)
			ui.Text(c, "来源").Width(40).Shrink(0).FontSize(12).TextColor(tok.muted)
		})
		items := a.promptTableItems()
		if len(items) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Gap(4).Children(func() {
				ui.Text(c, "没有此类型的提示词").FontSize(13).TextColor(tok.text)
				ui.Text(c, "选择其他类型，或新建提示词。").FontSize(12).TextColor(tok.muted)
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
	tok := a.look
	selected := a.promptEditPick == item.ID
	row := ui.ButtonBase(c.Key("prompt-row-"+item.ID)).FillWidth().MinHeight(44).
		Padding(8, 14).Gap(8).AlignItems(ui.Center).Label("编辑 "+item.Label).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Disabled(a.running)
	if selected {
		row.Background(tok.accent.Alpha(0.09))
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
		name := ui.Text(c, item.Label).Grow(1).MinWidth(0).FontSize(13).SingleLine().Tooltip(item.Label)
		if selected {
			name.Bold().TextColor(tok.accent)
		}
		kind := promptKindLabel(item.Kind)
		color := tok.muted
		if kind == "生图" {
			color = tok.accent
		} else if kind == "洗图" {
			color = tok.text
		}
		ui.Text(c, kind).Width(58).Shrink(0).FontSize(12).TextColor(color)
		source := "我的"
		if item.Builtin {
			source = "内置"
		}
		ui.Text(c, source).Width(40).Shrink(0).FontSize(12).TextColor(tok.muted)
	})
}

func (a *App) promptEditor(c *ui.Context) ui.Element {
	tok := a.look
	if !containsStr([]string{"生图", "洗图", "通用"}, a.promptEditKind) {
		a.promptEditKind = "通用"
	}
	panel := ui.Column(c.Key("prompt-editor")).MinHeight(0).Gap(12).Padding(16).
		Radius(tok.radiusL).Background(tok.card).Border(1, tok.border)
	panel.Children(func() {
		title := "新建提示词"
		if a.promptEditPick != "" {
			title = "编辑提示词"
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(15).Bold().Grow(1)
			if prompts.IsBuiltin(a.promptEditPick) {
				ui.Text(c, "内置 · 修改后另存为副本").FontSize(12).TextColor(tok.muted)
			}
		})
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
				ui.Text(c, "名称").FontSize(12).TextColor(tok.muted)
				ui.TextInput(c.Key("prompt-name"), &a.promptEditName).FillWidth().Label("提示词名称").
					Placeholder("留空时取正文第一行").Disabled(a.running)
			})
			ui.Column(c).Width(132).Shrink(0).Gap(5).Children(func() {
				ui.Text(c, "适用类型").FontSize(12).TextColor(tok.muted)
				motionSelect(c.Key("prompt-kind"), &a.promptEditKind, []string{"生图", "洗图", "通用"}).
					FillWidth().Label("提示词类型").Disabled(a.running)
			})
		})
		ui.Text(c, "正文").FontSize(12).TextColor(tok.muted)
		ui.TextArea(c.Key("prompt-body"), &a.promptEditText).Grow(1).MinHeight(140).FillWidth().
			Label("提示词内容").Placeholder("输入提示词内容").Disabled(a.running)
		if a.promptEditNote != "" {
			ui.Text(c, a.promptEditNote).FontSize(12).TextColor(tok.muted)
		}
		ui.Row(c).FillWidth().Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
			ui.PrimaryButton(c, "保存提示词").Disabled(a.running).OnClick(func() { a.saveCurrentPrompt() })
			if prompts.IsBuiltin(a.promptEditPick) {
				ui.Button(c, "恢复内置").Disabled(a.running).OnClick(func() { a.restoreBuiltinPrompt() })
			} else if a.promptEditPick != "" {
				ui.Button(c, "删除提示词").Disabled(a.running).OnClick(func() { a.deleteUserPrompt() })
			}
		})
	})
	return panel
}
