package ui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	fileren "github.com/xxww0098/tulai/internal/rename"
)

type renameItem struct {
	path  string
	draft string
}

type renameRule struct {
	Find, With    string
	template      int
	count, offset float64
	text          string
}

type renameState struct {
	ready, busy bool
	done        <-chan struct{}
	mode        int
	items       []renameItem
	list        ui.ListState
	rules       []renameRule
	includeExt  bool
	insertKind  string
	position    string
	index       float64
	text        string
	prefix      string
	suffix      string
	numFormat   string
	numStart    float64
	numWidth    float64
	numPrefix   string
	numSuffix   string
	sortBy      string
	aiPrompt    string
	aiSettings  bool
	editVersion int
	undo        []fileren.Change
	note, err   string
}

func (s *renameState) init() {
	if s.ready {
		return
	}
	s.ready = true
	s.rules = []renameRule{{count: 1, offset: 1}}
	s.insertKind, s.position = "文本", "末尾"
	s.index, s.numStart = 1, 1
	s.numFormat, s.sortBy = "阿拉伯数字", "名称升序"
}

func (a *App) renameOptions() fileren.Options {
	s := &a.fileRename
	s.init()
	opts := fileren.Options{
		IncludeExtension: s.includeExt,
		InsertKind:       s.insertKind, Text: s.text, Index: int(s.index), Prefix: s.prefix, Suffix: s.suffix,
		Numbering: fileren.Numbering{Start: int64(s.numStart), Width: int(s.numWidth), Format: s.numFormat, Prefix: s.numPrefix, Suffix: s.numSuffix},
	}
	for _, rule := range s.rules {
		opts.Replacements = append(opts.Replacements, fileren.Replacement{
			Find: rule.Find, With: rule.With, Template: rule.template,
			Count: int(rule.count), Offset: int(rule.offset), Text: rule.text,
		})
	}
	switch s.position {
	case "开头":
		opts.Position = 0
	case "指定位置":
		opts.Position = 1
	default:
		opts.Position = 2
	}
	switch s.mode {
	case 1, 4:
		opts.Mode = fileren.Manual
		for _, item := range s.items {
			opts.Names = append(opts.Names, item.draft)
		}
	case 2:
		opts.Mode = fileren.Insert
	case 3:
		opts.Mode = fileren.Number
	}
	return opts
}

func (a *App) renamePlan() ([]fileren.Change, error) {
	paths := make([]string, len(a.fileRename.items))
	for i, item := range a.fileRename.items {
		paths[i] = item.path
	}
	changes, err := fileren.Preview(paths, a.renameOptions())
	if err == nil {
		err = fileren.Check(changes)
	}
	return changes, err
}

type renamePalette struct {
	tok                                         tokens
	frame, rule, field, text, muted, line, blue ui.Color
}

func renameColors(dark bool) renamePalette {
	tok := workbenchTokens(dark)
	return renamePalette{
		tok:   tok,
		frame: tok.paper,
		rule:  tok.card,
		field: tok.sunken,
		text:  tok.text,
		muted: tok.muted,
		line:  tok.border,
		blue:  tok.accent,
	}
}

func (a *App) renamePage(c *ui.Context) {
	s := &a.fileRename
	s.init()
	colors := renameColors(a.look.dark)
	tok := colors.tok
	if !s.busy && c.Shortcut(ui.Cmd, ui.KeyV) {
		data := c.Services().ReadClipboardData(transfer.FileList, transfer.URIList)
		if paths, err := data.Files(); err == nil && len(paths) > 0 {
			a.renameIngest(paths)
		}
	}
	changes, planErr := a.renamePlan()
	changed := 0
	for _, change := range changes {
		if change.From != change.To {
			changed++
		}
	}
	page := ui.Column(c.Key("rename-page")).Fill().MinHeight(0).Background(colors.rule)
	if dropped := page.DroppedFiles(); len(dropped) > 0 {
		a.renameIngest(dropped)
	}
	page.Children(func() {
		ui.Row(c.Key("rename-top")).FillWidth().Padding(8, 14).Gap(10).AlignItems(ui.Center).
			Background(colors.rule).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
			a.renameTabs(c, colors)
			ui.Spacer(c)
			uiBadge(c, "rename-total-badge", fmt.Sprintf("%d 项", len(s.items)), "neutral", tok)
			if changed > 0 {
				uiBadge(c, "rename-changed-badge", fmt.Sprintf("%d 待改", changed), "accent", tok)
			}
			if planErr != nil && len(s.items) > 0 {
				uiBadge(c, "rename-conflict-badge", "存在冲突", "danger", tok)
			}
		})

		w, _ := c.Size()
		split := w >= 820 && s.mode != 4
		body := ui.Column(c.Key("rename-workspace")).Grow(1).MinHeight(0).FillWidth().Padding(0).Gap(0)
		body.Children(func() {
			if split {
				sideW := float32(312)
				if w >= 1040 {
					sideW = 340
				}
				ui.Row(c.Key("rename-split")).Grow(1).MinHeight(0).FillWidth().Gap(0).AlignItems(ui.Stretch).Children(func() {
					ruleCard := ui.Column(c.Key("rename-rules-card")).Width(sideW).Shrink(0).MinHeight(0).
						Padding(12, 14).Gap(8).Background(colors.rule).
						BorderWidth(0, 1, 0, 0).BorderColor(colors.line)
					ruleCard.Children(func() {
						uiSectionTitle(c, renameModeTitle(s.mode), renameModeMeta(s), tok)
						a.renameRules(c, colors, true)
					})
					a.renameList(c, colors, changes, planErr).Grow(1).Basis(0).MinWidth(0)
				})
			} else {
				if s.mode == 4 {
					ui.Row(c.Key("rename-manual-hint")).FillWidth().MinHeight(26).Padding(6, 14).Gap(6).
						AlignItems(ui.Center).Background(tok.paper).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
						ui.Icon(c, iconInfo).FontSize(12).TextColor(colors.blue).Shrink(0)
						ui.Text(c, "提示：粘贴多行文本（如 Excel 复制的列），将自动向下批量替换").
							FontSize(tok.caption).TextColor(colors.muted).Grow(1).MinWidth(0).SingleLine()
					})
				} else {
					ruleCard := ui.Column(c.Key("rename-rules-card")).Shrink(0).FillWidth().
						Padding(10, 14).Gap(6).Background(colors.rule).
						BorderWidth(0, 0, 1, 0).BorderColor(colors.line)
					ruleCard.Children(func() {
						a.renameRules(c, colors, false)
					})
				}
				a.renameList(c, colors, changes, planErr).Grow(1).MinHeight(120).FillWidth()
			}

			message := s.err
			if message == "" && planErr != nil && len(s.items) > 0 {
				message = planErr.Error()
			}
			if message != "" {
				services := c.Services()
				fg, bg, border := semanticToneColors(tok.dark, tok.text, tok.danger, 0.10)
				ui.Row(c.Key("rename-error-bar")).FillWidth().Padding(6, 14).Gap(6).AlignItems(ui.Center).
					Background(bg).BorderWidth(1, 0, 0, 0).BorderColor(border).Children(func() {
					ui.Icon(c, iconAlert).FontSize(13).TextColor(fg).Shrink(0)
					ui.Scroll(c).Grow(1).MinWidth(0).MaxHeight(72).Children(func() {
						ui.Text(c, message).FillWidth().FontSize(tok.caption).TextColor(fg)
					})
					uiMiniBtn(ui.Button(c, "复制错误"), tok).OnClick(func() { services.WriteClipboardData(transfer.TextData(message)) })
				})
			}
		})

		a.renameToolbar(c, colors, changes, changed, planErr)
	})
}

func renameModeTitle(mode int) string {
	switch mode {
	case 1:
		return "AI 智能重命名"
	case 2:
		return "插入内容规则"
	case 3:
		return "自动编号规则"
	case 4:
		return "手动编辑"
	default:
		return "查找与替换规则"
	}
}

func renameModeMeta(s *renameState) string {
	switch s.mode {
	case 0:
		return fmt.Sprintf("%d 条规则", len(s.rules))
	case 2:
		return s.insertKind + " · " + s.position
	case 3:
		return s.numFormat
	default:
		return ""
	}
}

func (a *App) renameTabs(c *ui.Context, colors renamePalette) {
	s := &a.fileRename
	tok := colors.tok
	if tok.body == 0 {
		tok = workbenchTokens(false)
	}
	labels := []string{"查找替换", "智能重命名", "插入内容", "自动编号", "手动编辑"}
	icons := []*ui.SVG{iconSearch, iconSparkles, iconLayers, iconListOrdered, iconPen}
	tabs := ui.TabsBase(c.Key("rename-modes"), &s.mode, len(labels))
	bar := tabs.List.Row().FillWidth().MaxWidth(540).Shrink(1).Height(34).Padding(3).Radius(tok.radiusS).
		Background(colors.field).AlignItems(ui.Stretch)
	position := bar.AnimateTo("selected", float32(s.mode), motionEaseOut)
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		width := (r.W - 6) / float32(len(labels))
		p.Fill(ui.Rect{X: r.X + 3 + width*position, Y: r.Y + 3, W: width, H: r.H - 6}, colors.rule, tok.radiusS-1)
	})
	bar.Children(func() {
		for i, label := range labels {
			tab := tabs.Tab(i).Grow(1).Basis(0).MinWidth(0).Height(28).Gap(4).Center().Radius(tok.radiusS - 1).
				Label(label).Disabled(s.busy).TextColor(colors.muted)
			if i == s.mode {
				tab.TextColor(colors.text)
			}
			if !s.busy {
				tab.Cursor(ui.CursorPointer)
			}
			tab.Children(func() {
				ui.Icon(c, icons[i]).FontSize(12).Shrink(0)
				text := ui.Text(c, label).FontSize(tok.body).SingleLine()
				if i == s.mode {
					text.FontWeight(600)
				}
			})
		}
	})
}

func renameField(c *ui.Context, colors renamePalette, key string, value *string, label, placeholder string, busy bool) ui.Element {
	tok := colors.tok
	if tok.radiusS == 0 {
		tok = workbenchTokens(false)
	}
	field := ui.Column(c.Key(key)).Height(34).Padding(0, 8).Radius(tok.radiusS).
		Background(colors.field).Border(1, colors.line)
	focused := field.FocusWithin()
	target := float32(0)
	if focused || *value != "" {
		target = 1
	}
	position := field.AnimateTo("label", target, motionEaseFast)
	surface := colors.field
	labelColor := colors.muted
	if focused {
		surface = colors.rule
		labelColor = colors.blue
		field.Background(surface).Border(1, colors.blue)
	} else if field.Hovered() && !busy {
		field.BorderColor(tok.borderStrong)
	}
	field.Children(func() {
		ui.TextInput(c.Key("input"), value).Grow(1).MinHeight(0).FillWidth().Padding(11*target, 0, 0, 0).Radius(0).
			Background(surface).BorderWidth(0).FontSize(12).TextColor(colors.text).
			Label(label).Placeholder("").Disabled(busy)
		ui.Text(c, placeholder).Absolute().Top(9).Left(8).Right(8).FontSize(12).TextColor(labelColor).SingleLine().
			Translate(0, -7*position).TransformOrigin(0, 0).Scale(1-position/4, 1-position/4).IgnoreInput(true)
	})
	return field
}

func (a *App) renameTemplateSelect(c *ui.Context, colors renamePalette, rule *renameRule) {
	tok := colors.tok
	labels := []string{"普通查找", "前 N 位", "后 N 位", "从第 X 位开始的 N 位", "某字符之后的内容", "某字符之前的内容", "某字符之后的 N 位", "某字符之前的 N 位"}
	sel := ui.SelectBase(c.Key("rename-template"), &rule.template)
	tr := sel.Trigger.Size(28, 34).Center().Radius(tok.radiusS).Background(colors.field).Border(1, colors.line).
		Label("选择查找模板").Tooltip("选择查找模板").TextColor(colors.muted).Disabled(a.fileRename.busy)
	if tr.Hovered() && !a.fileRename.busy {
		tr.Background(tok.sunkenHi).BorderColor(tok.borderStrong).TextColor(colors.text)
	}
	tr.Children(func() {
		ui.Icon(c, iconChevronDown).FontSize(13)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Width(204).Padding(4).Radius(tok.radiusM).Background(colors.rule).Border(1, colors.line).
			Shadow(0, 6, 20, 0, tok.lift).Transition(motionPopup()).Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				for i, label := range labels {
					item := sel.Item(i).FillWidth().Padding(5, 8).Radius(tok.radiusS).Label(label)
					if item.Highlighted() {
						item.Background(tok.accent).TextColor(tok.onAccent)
					} else if i == rule.template {
						item.Background(colors.field).TextColor(colors.text)
					} else {
						item.TextColor(colors.text)
					}
					item.Children(func() { ui.Text(c, label).FontSize(tok.body).SingleLine() })
				}
			})
		})
	})
}

func (a *App) renameTemplateFields(c *ui.Context, colors renamePalette, rule *renameRule) {
	ui.Row(c).Grow(1).MinWidth(0).Height(34).Padding(0, 6).Gap(4).Radius(colors.tok.radiusS).
		Background(colors.field).Border(1, colors.line).AlignItems(ui.Center).Children(func() {
		word := func(text string) { ui.Text(c, text).FontSize(11).TextColor(colors.text).Shrink(0) }
		number := func(key, label string, value *float64) {
			ui.NumberInput(c.Key(key), value, 0, 9999, 1).Width(92).MinWidth(60).Shrink(1).Label(label).Disabled(a.fileRename.busy)
		}
		character := func() {
			ui.TextInput(c.Key("template-text"), &rule.text).Width(78).MinWidth(44).Label("指定字符").Placeholder("指定字符").Disabled(a.fileRename.busy)
		}
		switch rule.template {
		case 1, 2:
			if rule.template == 1 {
				word("前")
			} else {
				word("后")
			}
			number("template-count", "字符位数", &rule.count)
			word("位")
		case 3:
			word("从第")
			number("template-offset", "起始位置", &rule.offset)
			word("位后")
			number("template-count", "字符位数", &rule.count)
			word("位")
		case 4, 5, 6, 7:
			character()
			if rule.template == 4 || rule.template == 6 {
				word("之后")
			} else {
				word("之前")
			}
			if rule.template == 6 || rule.template == 7 {
				number("template-count", "字符位数", &rule.count)
				word("位")
			} else {
				word("的内容")
			}
		}
	})
}

func (a *App) renameRules(c *ui.Context, colors renamePalette, compact bool) {
	s := &a.fileRename
	tok := colors.tok
	switch s.mode {
	case 0:
		scroll := ui.Scroll(c.Key("rename-replace-scroll")).FillWidth()
		if compact {
			scroll.Grow(1).MinHeight(0)
		} else {
			scroll.MaxHeight(132).MinHeight(36)
		}
		scroll.Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for i := range s.rules {
					i := i
					find := func() {
						ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(4).AlignItems(ui.Center).Children(func() {
							if s.rules[i].template == 0 {
								renameField(c, colors, "find", &s.rules[i].Find, fmt.Sprintf("查找 %d", i+1), "查找", s.busy).Grow(1).MinWidth(0)
							} else {
								a.renameTemplateFields(c, colors, &s.rules[i])
							}
							a.renameTemplateSelect(c, colors, &s.rules[i])
						})
					}
					remove := func() {
						if len(s.rules) > 1 {
							uiMiniBtn(ui.Button(c.Key("remove"), "移除"), tok).Label(fmt.Sprintf("移除替换规则 %d", i+1)).
								Disabled(s.busy).OnClick(func() { s.rules = append(s.rules[:i], s.rules[i+1:]...) })
						}
					}
					ruleBox := ui.Column(c.Key(fmt.Sprintf("rename-rule-%d", i))).FillWidth().Gap(5)
					if i < len(s.rules)-1 {
						ruleBox.Padding(0, 0, 8, 0).BorderWidth(0, 0, 1, 0).BorderColor(colors.line)
					}
					w, _ := c.Size()
					if compact || w < 1000 && s.rules[i].template != 0 || w < 720 {
						ruleBox.Children(func() {
							if len(s.rules) > 1 {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
									ui.Text(c, fmt.Sprintf("规则 #%d", i+1)).FontSize(tok.caption).FontWeight(600).
										TextColor(colors.muted).FontFeatures("tnum").Grow(1)
									remove()
								})
							}
							ui.Row(c).FillWidth().Children(func() { find() })
							renameField(c, colors, "with", &s.rules[i].With, fmt.Sprintf("替换为 %d", i+1), "替换成", s.busy).FillWidth()
						})
					} else {
						ruleBox.Children(func() {
							ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
								find()
								ui.Icon(c, iconArrowRight).FontSize(13).TextColor(colors.muted).Margin(0, 2).Shrink(0)
								renameField(c, colors, "with", &s.rules[i].With, fmt.Sprintf("替换为 %d", i+1), "替换成", s.busy).Grow(1).Basis(0).MinWidth(0)
								remove()
							})
						})
					}
				}
			})
		})
		ui.Row(c).FillWidth().Gap(6).Padding(4, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(colors.line).
			AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c, &s.includeExt, "替换文件扩展名").Disabled(s.busy)
			ui.Spacer(c)
			uiMiniBtn(ui.Button(c, "+ 添加替换规则"), tok).Label("添加替换规则").Disabled(s.busy).
				OnClick(func() { s.rules = append(s.rules, renameRule{count: 1, offset: 1}) })
		})
	case 1:
		scroll := ui.Scroll(c.Key("rename-ai-scroll")).FillWidth().MinHeight(0)
		if compact {
			scroll.Grow(1)
		} else {
			scroll.MaxHeight(180)
		}
		scroll.Children(func() {
			ui.Column(c).FillWidth().Gap(6).Children(func() {
				ui.TextArea(c.Key("rename-ai-prompt"), &s.aiPrompt).Height(68).FillWidth().Label("智能重命名要求").
					Placeholder("用一句话描述重命名规则，例如：中文转英文 / 加序号").Disabled(s.busy)
				ui.Row(c).FillWidth().Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					uiMiniBtn(ui.Button(c, "模型设置"), tok).Disabled(s.busy).OnClick(func() { s.aiSettings = !s.aiSettings })
					ui.Text(c, "与验证码自定义模型共用").FontSize(tok.caption).TextColor(colors.muted).Grow(1).MinWidth(0).SingleLine()
					uiBtn(ui.PrimaryButton(c, "生成名称建议"), tok).
						Disabled(s.busy || len(s.items) == 0 || strings.TrimSpace(s.aiPrompt) == "").
						OnClick(a.renameSuggest)
				})
				if s.aiSettings {
					ui.Column(c).FillWidth().Gap(5).Padding(6, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(colors.line).Children(func() {
						ui.TextInput(c.Key("rename-ai-url"), &a.customURL).FillWidth().Label("模型 API 地址").Placeholder("模型 API 地址").Disabled(s.busy)
						ui.TextInput(c.Key("rename-ai-key"), &a.customKey).FillWidth().Label("模型 API Key").Placeholder("API Key").Password().Disabled(s.busy)
						ui.Row(c).FillWidth().Gap(6).Children(func() {
							ui.TextInput(c.Key("rename-ai-model"), &a.customModel).Grow(1).MinWidth(0).Label("模型名称").Placeholder("模型名称").Disabled(s.busy)
							uiBtn(ui.Button(c, "保存模型"), tok).Disabled(s.busy).OnClick(a.saveRenameModel)
						})
					})
				}
				ui.Text(c, "文件名会发送给模型，文件内容不会发送。").FontSize(tok.caption).TextColor(colors.muted)
			})
		})
	case 2:
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "内容").FontSize(tok.caption).FontWeight(600).TextColor(colors.muted).Margin(0, 4, 0, 0)
				for _, choice := range []string{"文本", "序号", "文件信息"} {
					selected := s.insertKind == choice || choice == "文件信息" && s.insertKind != "文本" && s.insertKind != "序号"
					renameChoice(c, colors, "kind-"+choice, choice, selected, s.busy, func() {
						if choice == "文件信息" {
							s.insertKind = "创建时间"
						} else {
							s.insertKind = choice
						}
					})
				}
			})
			ui.Row(c).FillWidth().Gap(4).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "位置").FontSize(tok.caption).FontWeight(600).TextColor(colors.muted).Margin(0, 4, 0, 0)
				for _, choice := range []string{"开头", "指定位置", "末尾"} {
					renameChoice(c, colors, "pos-"+choice, choice, s.position == choice, s.busy, func() { s.position = choice })
				}
				if s.position == "指定位置" {
					ui.NumberInput(c.Key("rename-position-index"), &s.index, 0, 9999, 1).Width(68).Label("位置").Disabled(s.busy)
				}
			})
			ui.Box(c).FillWidth().Height(1).Background(colors.line)
			switch s.insertKind {
			case "文本":
				renameField(c, colors, "rename-text", &s.text, "插入文本", "固定文本", s.busy).FillWidth()
			case "序号":
				a.renameNumberFields(c, colors, compact)
			default:
				if compact {
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						motionSelect(c.Key("rename-insert-kind"), &s.insertKind, []string{"创建时间", "修改时间", "大小", "图片宽度", "图片高度", "照片拍摄时间"}).FillWidth().Label("文件信息类型").Disabled(s.busy)
						ui.Row(c).FillWidth().Gap(6).Children(func() {
							renameField(c, colors, "rename-prefix", &s.prefix, "信息前缀", "信息前缀", s.busy).Grow(1).Basis(0).MinWidth(0)
							renameField(c, colors, "rename-suffix", &s.suffix, "信息后缀", "信息后缀", s.busy).Grow(1).Basis(0).MinWidth(0)
						})
					})
				} else {
					ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						renameField(c, colors, "rename-prefix", &s.prefix, "信息前缀", "信息前缀", s.busy).Grow(1).Basis(0).MinWidth(0)
						motionSelect(c.Key("rename-insert-kind"), &s.insertKind, []string{"创建时间", "修改时间", "大小", "图片宽度", "图片高度", "照片拍摄时间"}).Width(124).Label("文件信息类型").Disabled(s.busy)
						renameField(c, colors, "rename-suffix", &s.suffix, "信息后缀", "信息后缀", s.busy).Grow(1).Basis(0).MinWidth(0)
					})
				}
			}
		})
	case 3:
		a.renameNumberFields(c, colors, compact)
	}
}

func renameChoice(c *ui.Context, colors renamePalette, key, label string, selected, busy bool, click func()) {
	uiPill(c, key, label, selected, colors.tok).Disabled(busy).OnClick(click)
}

func (a *App) renameNumberFields(c *ui.Context, colors renamePalette, compact bool) {
	s := &a.fileRename
	tok := colors.tok
	w, _ := c.Size()
	controls := func() {
		ui.Row(c).Gap(6).Padding(6, 8).Radius(tok.radiusS).Background(colors.field).Border(1, colors.line).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(68).Gap(2).Children(func() {
				ui.Text(c, "开始序号").FontSize(tok.caption).TextColor(colors.muted)
				ui.NumberInput(c.Key("rename-number-start"), &s.numStart, 0, 9_000_000_000_000_000, 1).FillWidth().Label("编号起始值").Disabled(s.busy)
			})
			ui.Column(c).Grow(1).Basis(0).MinWidth(82).Gap(2).Children(func() {
				ui.Text(c, "序号类型").FontSize(tok.caption).TextColor(colors.muted)
				motionSelect(c.Key("rename-number-format"), &s.numFormat, []string{"阿拉伯数字", "中文小写", "中文大写", "英文小写", "英文大写"}).FillWidth().Label("编号格式").Disabled(s.busy)
			})
			if s.numFormat == "阿拉伯数字" {
				ui.Column(c).Width(62).Shrink(0).Gap(2).Children(func() {
					ui.Text(c, "固定位数").FontSize(tok.caption).TextColor(colors.muted)
					ui.NumberInput(c.Key("rename-number-width"), &s.numWidth, 0, 16, 1).FillWidth().Label("补零位数").Disabled(s.busy)
				})
			}
		})
	}
	if compact || w < 760 {
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				renameField(c, colors, "rename-number-prefix", &s.numPrefix, "序号前缀", "序号前面字符", s.busy).Grow(1).Basis(0).MinWidth(0)
				renameField(c, colors, "rename-number-suffix", &s.numSuffix, "序号后缀", "序号后面字符", s.busy).Grow(1).Basis(0).MinWidth(0)
			})
			controls()
		})
		return
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		renameField(c, colors, "rename-number-prefix", &s.numPrefix, "序号前缀", "序号前面字符", s.busy).Grow(1).Basis(0).MinWidth(0)
		controls()
		renameField(c, colors, "rename-number-suffix", &s.numSuffix, "序号后缀", "序号后面字符", s.busy).Grow(1).Basis(0).MinWidth(0)
	})
}

func renameTool(c *ui.Context, colors renamePalette, key, label string, icon *ui.SVG, disabled bool, click func()) {
	uiIconBtn(c, key, label, label, icon, 24, false, true, colors.tok).Disabled(disabled).OnClick(click)
}

func (a *App) renameToolbar(c *ui.Context, colors renamePalette, changes []fileren.Change, changed int, planErr error) {
	s := &a.fileRename
	tok := colors.tok
	services := c.Services()
	ui.Row(c.Key("rename-toolbar")).FillWidth().MinHeight(38).Padding(6, 14).Gap(5).Wrap().AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(colors.line).Background(tok.paper).Children(func() {
		uiIconTextBtn(c, "rename-import-folder", "读取文件夹内容", "读取文件夹及其子目录内所有文件", iconFolderOpen, 24, false, tok).
			Disabled(s.busy).OnClick(func() { a.renamePick(true, true) })
		uiIconTextBtn(c, "rename-import-files", "添加文件", "选择一个或多个文件", iconFilePlus, 24, false, tok).
			Disabled(s.busy).OnClick(func() { a.renamePick(false, false) })
		renameTool(c, colors, "rename-import-dir", "添加文件夹", iconFolder, s.busy, func() { a.renamePick(true, false) })
		renameTool(c, colors, "rename-clear", "清空列表", iconTrash, s.busy || len(s.items) == 0, func() { s.items, s.undo, s.note, s.err = nil, nil, "", "" })
		if s.mode == 4 {
			renameTool(c, colors, "rename-copy", "复制原名称", iconCopy, s.busy || len(s.items) == 0, func() {
				names := make([]string, len(s.items))
				for i, item := range s.items {
					names[i] = filepath.Base(item.path)
				}
				services.WriteClipboardData(transfer.TextData(strings.Join(names, "\n")))
				s.note = "已复制原名称。"
			})
		}
		motionSelect(c.Key("rename-sort"), &s.sortBy, []string{"名称升序", "名称降序", "大小升序", "大小降序", "修改时间升序", "修改时间降序", "创建时间升序", "创建时间降序"}).
			Height(24).Width(116).Label("文件排序").Disabled(s.busy).OnChange(a.renameSort)
		count := fmt.Sprintf("已选 %d 个文件", len(s.items))
		ui.Text(c, count).FontSize(tok.caption).TextColor(colors.muted).SingleLine().FontFeatures("tnum").Tooltip(count)
		if s.note != "" {
			ui.Text(c, s.note).FontSize(tok.caption).TextColor(tok.success).SingleLine().Tooltip(s.note)
		}
		ui.Spacer(c)
		if len(s.undo) > 0 {
			uiIconBtn(c, "rename-undo", "撤销上次重命名", "撤销上次重命名", iconRotateCcw, 24, false, true, tok).
				Disabled(s.busy || a.renameUpdateActive()).OnClick(func() { a.renameApply(s.undo, true) })
		}
		uiPrimaryBtn(c, "rename-apply", "执行重命名", s.busy || a.renameUpdateActive() || planErr != nil || changed == 0, tok).
			Height(26).Padding(0, 12).OnClick(func() { a.renameApply(changes, false) })
	})
}

func (a *App) renameList(c *ui.Context, colors renamePalette, changes []fileren.Change, planErr error) ui.Element {
	s := &a.fileRename
	tok := colors.tok
	panel := ui.Column(c.Key("rename-list-panel")).MinHeight(120).FillWidth().Background(colors.rule).Clip()
	panel.Children(func() {
		if len(s.items) == 0 {
			ui.Column(c.Key("rename-empty")).Grow(1).FillWidth().Center().Gap(6).Padding(16).Children(func() {
				ui.Row(c).Size(40, 40).Radius(tok.radiusL).Center().Background(tok.paper).Children(func() {
					ui.Icon(c, iconFolderOpen).FontSize(18).TextColor(colors.muted)
				})
				ui.Text(c, "拖放 / 粘贴文件（夹）到这里").FontSize(tok.title).FontWeight(600).TextColor(colors.text)
				ui.Text(c, "支持拖动调整顺序，或使用底部「添加文件」「读取文件夹内容」导入").FontSize(tok.caption).TextColor(colors.muted)
			})
			return
		}
		targetCounts := make(map[string]int, len(changes))
		for _, ch := range changes {
			targetCounts[strings.ToLower(ch.To)]++
		}
		ui.Row(c.Key("rename-table-head")).FillWidth().Height(26).Padding(0, 10).Gap(6).AlignItems(ui.Center).
			Background(tok.paper).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
			ui.Text(c, "#").Width(24).FontSize(tok.caption).FontWeight(600).TextColor(colors.muted).FontFeatures("tnum").Shrink(0)
			ui.Box(c).Width(13).Shrink(0)
			ui.Text(c, "原名称").Grow(1).Basis(0).MinWidth(0).FontSize(tok.caption).FontWeight(600).TextColor(colors.muted).SingleLine()
			ui.Text(c, "状态").Width(48).FontSize(tok.caption).FontWeight(600).TextColor(colors.muted).Shrink(0)
			newLabel := "预览新名称"
			if s.mode == 1 || s.mode == 4 {
				newLabel = "新名称（可编辑）"
			}
			ui.Text(c, newLabel).Grow(1).Basis(0).MinWidth(0).FontSize(tok.caption).FontWeight(600).TextColor(colors.muted).SingleLine()
			ui.Box(c).Width(24).Shrink(0)
		})
		s.list.Key = func(i int) any { return s.items[i].path }
		s.list.Label = func(i int) string { return filepath.Base(s.items[i].path) }
		if !s.busy {
			s.list.Reorder = a.renameReorder
		} else {
			s.list.Reorder = nil
		}
		ui.List(c.Key("rename-list"), &s.list, len(s.items), func(i int) {
			if i >= len(s.items) {
				return
			}
			item := &s.items[i]
			changedRow := i < len(changes) && changes[i].From != changes[i].To
			conflictRow := false
			if i < len(changes) {
				ch := changes[i]
				if ch.To == "" || filepath.Dir(ch.To) != filepath.Dir(ch.From) ||
					(changedRow && targetCounts[strings.ToLower(ch.To)] > 1) ||
					(planErr != nil && changedRow && strings.Contains(planErr.Error(), ch.To)) {
					conflictRow = true
				}
			}
			row := ui.Row(c.Key(item.path)).FillWidth().Height(30).Padding(0, 10).Gap(6).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(colors.line)
			if row.Hovered() {
				row.Background(colors.field)
			}
			row.Children(func() {
				ui.Text(c, fmt.Sprintf("%02d", i+1)).Width(24).FontSize(tok.caption).TextColor(tok.subtle).
					FontFeatures("tnum").SingleLine().Shrink(0)
				icon := iconFile
				if info, err := os.Lstat(item.path); err == nil && info.IsDir() {
					icon = iconFolder
				}
				ui.Icon(c, icon).FontSize(13).TextColor(colors.muted).Shrink(0)
				ui.Text(c, filepath.Base(item.path)).Grow(1).Basis(0).MinWidth(0).FontSize(11.5).TextColor(colors.text).SingleLine().Tooltip(item.path)
				ui.Row(c).Width(48).Shrink(0).AlignItems(ui.Center).Children(func() {
					switch {
					case conflictRow:
						uiBadge(c, "status", "冲突", "danger", tok)
					case changedRow:
						uiBadge(c, "status", "待改", "accent", tok)
					default:
						uiBadge(c, "status", "不变", "neutral", tok)
					}
				})
				if s.mode == 1 || s.mode == 4 {
					in := ui.TextInput(c.Key(fmt.Sprintf("rename-draft-%d", s.editVersion)), &item.draft).
						Grow(1).Basis(0).MinWidth(0).Height(22).Padding(0, 6).Radius(tok.radiusS).
						Background(colors.field).Border(1, colors.line).FontSize(11.5).TextColor(colors.text).
						Label("新名称 " + filepath.Base(item.path)).Disabled(s.busy).
						OnPaste(func(text string) bool { return a.renamePasteAt(i, text) })
					if in.Focused() {
						in.Background(colors.rule).BorderColor(colors.blue)
					}
				} else {
					name := "—"
					if i < len(changes) {
						name = filepath.Base(changes[i].To)
					}
					t := ui.Text(c, name).Grow(1).Basis(0).MinWidth(0).FontSize(11.5).SingleLine().Tooltip(name)
					switch {
					case conflictRow:
						t.TextColor(tok.danger).FontWeight(500)
					case changedRow:
						t.TextColor(colors.text).FontWeight(500)
					default:
						t.TextColor(colors.muted)
					}
				}
				renameTool(c, colors, "remove", "移除 "+filepath.Base(item.path), iconX, s.busy, func() {
					s.items = append(s.items[:i], s.items[i+1:]...)
					s.undo = nil
				})
			})
		}).Grow(1).MinHeight(0).FillWidth()
	})
	return panel
}

func (a *App) renameIngest(paths []string) {
	s := &a.fileRename
	s.init()
	if s.busy {
		return
	}
	known := make(map[string]bool, len(s.items))
	for _, item := range s.items {
		known[item.path] = true
	}
	added := 0
	for _, path := range paths {
		path, err := filepath.Abs(path)
		if err != nil || known[path] {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		known[path] = true
		s.items = append(s.items, renameItem{path: path, draft: filepath.Base(path)})
		added++
	}
	if added > 0 {
		s.undo = nil
		s.err = ""
		s.note = ""
		a.renameSort()
	}
}

func (a *App) renamePick(directory, recursive bool) {
	if a.fileRename.busy {
		return
	}
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: "选择要重命名的文件或文件夹", Directory: directory, Multiple: true})
		if err != nil || len(paths) == 0 {
			return
		}
		failed := 0
		if recursive {
			paths, failed = renameReadFolders(paths)
		}
		apply := func() {
			a.renameIngest(paths)
			if failed > 0 {
				a.fileRename.err = fmt.Sprintf("%d 个项目读取失败或包含待恢复的暂存目录，已跳过", failed)
			}
		}
		if window != nil {
			window.Update(apply)
		} else {
			apply()
		}
	}()
}

func renameReadFolders(dirs []string) ([]string, int) {
	var files []string
	failed := 0
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				failed++
				return nil
			}
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".tulai-rename-") {
				if _, err := os.Lstat(filepath.Join(path, "恢复说明.txt")); err == nil {
					failed++
					return filepath.SkipDir
				}
			}
			if !entry.IsDir() {
				files = append(files, path)
			}
			return nil
		})
	}
	return files, failed
}

func (a *App) renameSort() {
	s := &a.fileRename
	collator := collate.New(language.Chinese, collate.Numeric, collate.IgnoreCase)
	type metadata struct {
		size              int64
		modified, created int64
	}
	infos := make(map[string]metadata, len(s.items))
	for _, item := range s.items {
		info, err := os.Lstat(item.path)
		if err != nil {
			continue
		}
		m := metadata{size: info.Size(), modified: info.ModTime().UnixNano()}
		if created, err := fileren.Created(item.path); err == nil {
			m.created = created.UnixNano()
		}
		infos[item.path] = m
	}
	desc := strings.HasSuffix(s.sortBy, "降序")
	sort.SliceStable(s.items, func(i, j int) bool {
		left, right := s.items[i], s.items[j]
		cmp := 0
		switch {
		case strings.HasPrefix(s.sortBy, "大小"):
			cmp = compareInt64(infos[left.path].size, infos[right.path].size)
		case strings.HasPrefix(s.sortBy, "修改时间"):
			cmp = compareInt64(infos[left.path].modified, infos[right.path].modified)
		case strings.HasPrefix(s.sortBy, "创建时间"):
			cmp = compareInt64(infos[left.path].created, infos[right.path].created)
		}
		if cmp == 0 {
			cmp = collator.CompareString(filepath.Base(left.path), filepath.Base(right.path))
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (a *App) renameReorder(rows []int, to int) {
	s := &a.fileRename
	if s.busy {
		return
	}
	selected := make(map[int]bool, len(rows))
	var moving []renameItem
	for _, i := range rows {
		if i >= 0 && i < len(s.items) {
			selected[i] = true
			moving = append(moving, s.items[i])
		}
	}
	var rest []renameItem
	at := 0
	for i, item := range s.items {
		if i == to {
			at = len(rest)
		}
		if !selected[i] {
			rest = append(rest, item)
		}
	}
	if to >= len(s.items) {
		at = len(rest)
	}
	s.items = append(append(append([]renameItem{}, rest[:at]...), moving...), rest[at:]...)
}

func (a *App) renamePasteAt(start int, text string) bool {
	s := &a.fileRename
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if !strings.Contains(text, "\n") {
		return false
	}
	lines := strings.Split(text, "\n")
	if len(lines) > len(s.items)-start {
		s.err = fmt.Sprintf("从当前行起只能粘贴 %d 行名称", len(s.items)-start)
		return true
	}
	for i, line := range lines {
		s.items[start+i].draft = line
	}
	s.editVersion++
	s.err = ""
	return true
}

func (a *App) renameSuggest() {
	s := &a.fileRename
	if s.busy || len(s.items) == 0 {
		return
	}
	names := make([]string, len(s.items))
	for i, item := range s.items {
		names[i] = filepath.Base(item.path)
	}
	s.busy, s.err, s.note = true, "", "正在生成名称建议…"
	window, prompt := a.window, s.aiPrompt
	endpoint, key, model := a.customURL, a.customKey, a.customModel
	go func() {
		result, err := fileren.Suggest(context.Background(), endpoint, key, model, prompt, names)
		apply := func() {
			s.busy = false
			if window != nil {
				defer a.maybeInstallPending()
			}
			if err != nil {
				s.err, s.note = err.Error(), ""
				return
			}
			for i := range s.items {
				s.items[i].draft = result[i]
			}
			s.note = "名称建议已生成，可在列表中修改。"
		}
		if window != nil {
			window.Update(apply)
		} else {
			apply()
		}
	}()
}

func (a *App) saveRenameModel() {
	if a.store != nil {
		a.store.Set("captcha_custom_url", a.customURL)
		a.store.Set("captcha_custom_key", a.customKey)
		a.store.Set("captcha_custom_model", a.customModel)
	}
	a.fileRename.aiSettings = false
	a.fileRename.note = "模型设置已保存。"
}

func (a *App) renameApply(changes []fileren.Change, undo bool) {
	s := &a.fileRename
	if s.busy || a.renameUpdateActive() || len(changes) == 0 {
		return
	}
	changes = append([]fileren.Change(nil), changes...)
	if !undo {
		for i := range changes {
			if changes[i].From == changes[i].To {
				continue
			}
			info, err := os.Lstat(changes[i].From)
			if err != nil {
				s.err = fmt.Sprintf("读取 %s：%v", changes[i].From, err)
				return
			}
			changes[i].Expected = info
		}
	}
	s.busy, s.err, s.note = true, "", "正在重命名…"
	finish := func(err error) {
		s.busy = false
		s.done = nil
		if err != nil {
			s.err, s.note = err.Error(), ""
			return
		}
		byPath := make(map[string]string, len(changes))
		var reverse []fileren.Change
		for _, change := range changes {
			byPath[change.From] = change.To
			if change.From != change.To {
				reverse = append(reverse, fileren.Change{From: change.To, To: change.From, Expected: change.Expected})
			}
		}
		for i := range s.items {
			if target, ok := byPath[s.items[i].path]; ok {
				s.items[i].path, s.items[i].draft = target, filepath.Base(target)
			}
		}
		if undo {
			s.undo = nil
			s.note = "已撤销上次重命名。"
		} else {
			s.undo = reverse
			s.note = fmt.Sprintf("已重命名 %d 项。", len(reverse))
		}
		if a.window != nil {
			a.maybeInstallPending()
		}
	}
	if a.window == nil {
		finish(fileren.Apply(changes))
		return
	}
	window := a.window
	done := make(chan struct{})
	s.done = done
	go func() {
		err := fileren.Apply(changes)
		close(done)
		window.Update(func() { finish(err) })
	}()
}

func (a *App) renameUpdateActive() bool {
	return a.updateDownloading || a.updatePending && a.updateBusy && !a.updateDeferred
}

func (a *App) waitRename() {
	if done := a.fileRename.done; done != nil {
		<-done
	}
}
