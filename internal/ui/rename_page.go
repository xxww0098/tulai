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
	frame, rule, field, text, muted, line, blue ui.Color
}

func renameColors(dark bool) renamePalette {
	if dark {
		return renamePalette{ui.Hex("#303133"), ui.Hex("#282828"), ui.Hex("#383838"), ui.Hex("#F1F1F1"), ui.Hex("#AAAAAA"), ui.Hex("#555555"), ui.Hex("#A4C7F4")}
	}
	return renamePalette{ui.Hex("#F4F4F4"), ui.Hex("#FAFBFB"), ui.Hex("#E9E9E9"), ui.Hex("#222222"), ui.Hex("#777777"), ui.Hex("#D5D5D5"), ui.Hex("#3F51B5")}
}

func (a *App) renamePage(c *ui.Context) {
	s := &a.fileRename
	s.init()
	colors := renameColors(a.look.dark)
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
	page := ui.Column(c.Key("rename-page")).Fill().MinHeight(0).Background(colors.frame)
	if dropped := page.DroppedFiles(); len(dropped) > 0 {
		a.renameIngest(dropped)
	}
	page.Children(func() {
		ui.Column(c.Key("rename-tab-bar")).FillWidth().Padding(10, 12, 8, 12).Background(colors.frame).Children(func() {
			a.renameTabs(c, colors)
		})
		a.renameRules(c, colors)
		a.renameList(c, colors, changes)
		message := s.err
		if message == "" && planErr != nil && len(s.items) > 0 {
			message = planErr.Error()
		}
		if message != "" {
			services := c.Services()
			ui.Row(c).FillWidth().Padding(4, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Scroll(c).Grow(1).MinWidth(0).MaxHeight(100).Children(func() {
					ui.Text(c, message).FillWidth().FontSize(12).TextColor(a.look.danger)
				})
				ui.Button(c, "复制错误").FontSize(12).OnClick(func() { services.WriteClipboardData(transfer.TextData(message)) })
			})
		}
		a.renameToolbar(c, colors, changes, changed, planErr)
	})
}

func (a *App) renameTabs(c *ui.Context, colors renamePalette) {
	s := &a.fileRename
	labels := []string{"查找替换", "智能重命名", "插入内容", "自动编号", "手动编辑"}
	icons := []*ui.SVG{iconSearch, iconSparkles, iconLayers, iconListOrdered, iconPen}
	tabs := ui.TabsBase(c.Key("rename-modes"), &s.mode, len(labels))
	bar := tabs.List.Row().FillWidth().Height(42).Padding(3).Radius(21).Background(colors.field).AlignItems(ui.Stretch)
	position := bar.AnimateTo("selected", float32(s.mode), motionEaseOut)
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		width := (r.W - 6) / float32(len(labels))
		p.Fill(ui.Rect{X: r.X + 3 + width*position, Y: r.Y + 3, W: width, H: r.H - 6}, colors.rule, 18)
	})
	bar.Children(func() {
		for i, label := range labels {
			tab := tabs.Tab(i).Grow(1).Basis(0).MinWidth(0).Height(36).Gap(6).Center().Radius(18).
				Label(label).Disabled(s.busy).TextColor(colors.text.Alpha(0.7))
			if i == s.mode {
				tab.TextColor(colors.text)
			}
			tab.Children(func() {
				ui.Icon(c, icons[i]).FontSize(15).Shrink(0)
				text := ui.Text(c, label).FontSize(13).SingleLine()
				if i == s.mode {
					text.Bold()
				}
			})
		}
	})
}

func renameField(c *ui.Context, colors renamePalette, key string, value *string, label, placeholder string, busy bool) ui.Element {
	field := ui.Column(c.Key(key)).Height(50).Padding(0, 11).Radius(3).
		Background(colors.field).BorderWidth(0, 0, 1, 0).BorderColor(colors.muted)
	focused := field.FocusWithin()
	target := float32(0)
	if focused || *value != "" {
		target = 1
	}
	position := field.AnimateTo("label", target, motionEaseFast)
	labelColor := colors.muted
	if focused {
		labelColor = colors.blue
		field.BorderWidth(0, 0, 2, 0).BorderColor(colors.blue)
	}
	field.Children(func() {
		ui.TextInput(c.Key("input"), value).Grow(1).MinHeight(0).FillWidth().Padding(14*target, 0, 0, 0).Radius(0).
			Background(colors.field).BorderWidth(0).FontSize(14).TextColor(colors.text).
			Label(label).Placeholder("").Disabled(busy)
		ui.Text(c, placeholder).Absolute().Top(14).Left(11).Right(11).FontSize(14).TextColor(labelColor).SingleLine().
			Translate(0, -10*position).TransformOrigin(0, 0).Scale(1-2*position/7, 1-2*position/7).IgnoreInput(true)
	})
	return field
}

func (a *App) renameTemplateSelect(c *ui.Context, colors renamePalette, rule *renameRule) {
	labels := []string{"普通查找", "前 N 位", "后 N 位", "从第 X 位开始的 N 位", "某字符之后的内容", "某字符之前的内容", "某字符之后的 N 位", "某字符之前的 N 位"}
	sel := ui.SelectBase(c.Key("rename-template"), &rule.template)
	sel.Trigger.Size(32, 40).Center().Label("选择查找模板").TextColor(colors.muted).Disabled(a.fileRename.busy).Children(func() {
		ui.Icon(c, iconChevronDown).FontSize(15)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Width(220).Padding(4).Radius(4).Background(colors.rule).Border(1, colors.line).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for i, label := range labels {
					item := sel.Item(i).FillWidth().Padding(6, 8).Radius(3).Label(label)
					if item.Highlighted() {
						item.Background(colors.field)
					}
					item.Children(func() { ui.Text(c, label).FontSize(12).TextColor(colors.text).SingleLine() })
				}
			})
		})
	})
}

func (a *App) renameTemplateFields(c *ui.Context, colors renamePalette, rule *renameRule) {
	ui.Row(c).Grow(1).MinWidth(0).Padding(0, 8).Gap(4).AlignItems(ui.Center).Children(func() {
		word := func(text string) { ui.Text(c, text).FontSize(12).TextColor(colors.text).Shrink(0) }
		number := func(key, label string, value *float64) {
			ui.NumberInput(c.Key(key), value, 0, 9999, 1).Width(150).Shrink(0).Label(label).Disabled(a.fileRename.busy)
		}
		character := func() {
			ui.TextInput(c.Key("template-text"), &rule.text).Width(100).MinWidth(50).Label("指定字符").Placeholder("指定字符").Disabled(a.fileRename.busy)
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
			word("位后的")
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

func (a *App) renameRules(c *ui.Context, colors renamePalette) {
	s := &a.fileRename
	if s.mode == 4 {
		ui.Row(c.Key("rename-manual-hint")).FillWidth().Height(28).Padding(0, 12).AlignItems(ui.Center).
			Justify(ui.Center).Background(colors.rule).Children(func() {
			ui.Text(c, "提示：粘贴多行文本（如 Excel 复制的列），将自动向下批量替换").FontSize(12).TextColor(colors.muted).SingleLine()
		})
		return
	}
	panel := ui.Column(c.Key("rename-rules")).FillWidth().Grow(1).Basis(0).MinHeight(160).
		Padding(14, 20, 10, 20).Background(colors.rule)
	panel.Children(func() {
		switch s.mode {
		case 0:
			ui.Scroll(c.Key("rename-replace-scroll")).FillWidth().Grow(1).MinHeight(48).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					for i := range s.rules {
						i := i
						find := func() {
							ui.Row(c).Grow(1).Basis(0).MinWidth(0).Height(50).AlignItems(ui.Center).Background(colors.field).
								BorderWidth(0, 0, 1, 0).BorderColor(colors.muted).Children(func() {
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
								ui.Button(c.Key("remove"), "移除").Label(fmt.Sprintf("移除替换规则 %d", i+1)).Disabled(s.busy).OnClick(func() { s.rules = append(s.rules[:i], s.rules[i+1:]...) })
							}
						}
						w, _ := c.Size()
						if w < 1000 && s.rules[i].template != 0 {
							ui.Column(c.Key(fmt.Sprintf("rename-rule-%d", i))).FillWidth().Gap(6).Children(func() {
								ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() { find(); remove() })
								renameField(c, colors, "with", &s.rules[i].With, fmt.Sprintf("替换为 %d", i+1), "替换成", s.busy).FillWidth()
							})
						} else {
							ui.Row(c.Key(fmt.Sprintf("rename-rule-%d", i))).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
								find()
								ui.Icon(c, iconArrowRight).FontSize(18).TextColor(colors.muted).Margin(0, 10).Shrink(0)
								renameField(c, colors, "with", &s.rules[i].With, fmt.Sprintf("替换为 %d", i+1), "替换成", s.busy).Grow(1).Basis(0).MinWidth(0)
								remove()
							})
						}
					}
				})
			})
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Checkbox(c, &s.includeExt, "替换文件扩展名").Disabled(s.busy)
				ui.Spacer(c)
				ui.Button(c, "+  添加替换规则").Label("添加替换规则").FontSize(12).Radius(3).Disabled(s.busy).
					OnClick(func() { s.rules = append(s.rules, renameRule{count: 1, offset: 1}) })
			})
		case 1:
			ui.Scroll(c.Key("rename-ai-scroll")).Grow(1).MinHeight(0).FillWidth().Children(func() {
				ui.Column(c).FillWidth().MinHeight(190).Center().Children(func() {
					w, _ := c.Size()
					width := max(float32(240), min(float32(620), w-railWidth-100))
					ui.Column(c).Width(width).MaxWidth(width).Padding(12, 14).Gap(6).Radius(16).
						Background(colors.rule).Border(1, colors.line).Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.08)).Children(func() {
						ui.TextArea(c.Key("rename-ai-prompt"), &s.aiPrompt).Height(64).FillWidth().Label("智能重命名要求").
							Placeholder("用一句话描述重命名规则，例如：中文转英文 / 加序号").Disabled(s.busy)
						ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Button(c, "模型设置").FontSize(12).Disabled(s.busy).OnClick(func() { s.aiSettings = !s.aiSettings })
							ui.Text(c, "与验证码自定义模型共用").FontSize(12).TextColor(colors.muted).Grow(1).MinWidth(0).SingleLine()
							ui.Button(c, "生成名称建议").Disabled(s.busy || len(s.items) == 0 || strings.TrimSpace(s.aiPrompt) == "").OnClick(a.renameSuggest)
						})
						if s.aiSettings {
							ui.TextInput(c.Key("rename-ai-url"), &a.customURL).FillWidth().Label("模型 API 地址").Placeholder("模型 API 地址").Disabled(s.busy)
							ui.TextInput(c.Key("rename-ai-key"), &a.customKey).FillWidth().Label("模型 API Key").Placeholder("API Key").Password().Disabled(s.busy)
							ui.Row(c).FillWidth().Gap(8).Children(func() {
								ui.TextInput(c.Key("rename-ai-model"), &a.customModel).Grow(1).MinWidth(0).Label("模型名称").Placeholder("模型名称").Disabled(s.busy)
								ui.Button(c, "保存模型").Disabled(s.busy).OnClick(a.saveRenameModel)
							})
						}
					})
					ui.Text(c, "文件名会发送给模型，文件内容不会发送。").FontSize(12).TextColor(colors.muted).Margin(6, 0, 0, 0)
				})
			})
		case 2:
			ui.Row(c).FillWidth().Gap(5).Wrap().AlignItems(ui.Center).Children(func() {
				ui.Text(c, "内容：").FontSize(13).TextColor(colors.text)
				for _, choice := range []string{"文本", "序号", "文件信息"} {
					selected := s.insertKind == choice || choice == "文件信息" && s.insertKind != "文本" && s.insertKind != "序号"
					renameChoice(c, colors, choice, selected, s.busy, func() {
						if choice == "文件信息" {
							s.insertKind = "创建时间"
						} else {
							s.insertKind = choice
						}
					})
				}
				ui.Spacer(c)
				ui.Text(c, "位置：").FontSize(13).TextColor(colors.text)
				for _, choice := range []string{"开头", "指定位置", "末尾"} {
					renameChoice(c, colors, choice, s.position == choice, s.busy, func() { s.position = choice })
				}
				if s.position == "指定位置" {
					ui.NumberInput(c.Key("rename-position-index"), &s.index, 0, 9999, 1).Width(78).Label("位置").Disabled(s.busy)
				}
			})
			ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
				switch s.insertKind {
				case "文本":
					renameField(c, colors, "rename-text", &s.text, "插入文本", "固定文本", s.busy).FillWidth()
				case "序号":
					a.renameNumberFields(c, colors)
				default:
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						renameField(c, colors, "rename-prefix", &s.prefix, "信息前缀", "信息前缀", s.busy).Grow(1).Basis(0).MinWidth(0)
						motionSelect(c.Key("rename-insert-kind"), &s.insertKind, []string{"创建时间", "修改时间", "大小", "图片宽度", "图片高度", "照片拍摄时间"}).Width(140).Label("文件信息类型").Disabled(s.busy)
						renameField(c, colors, "rename-suffix", &s.suffix, "信息后缀", "信息后缀", s.busy).Grow(1).Basis(0).MinWidth(0)
					})
				}
			})
		case 3:
			ui.Column(c).Grow(1).FillWidth().Center().Children(func() { a.renameNumberFields(c, colors) })
		}
	})
}

func renameChoice(c *ui.Context, colors renamePalette, label string, selected, busy bool, click func()) {
	b := ui.Button(c, label).FontSize(12).Padding(5, 9).Radius(2).Border(1, colors.line).Disabled(busy)
	if selected {
		b.Background(colors.blue.Alpha(0.1)).TextColor(colors.blue)
	} else {
		b.Background(colors.rule).TextColor(colors.muted)
	}
	b.OnClick(click)
}

func (a *App) renameNumberFields(c *ui.Context, colors renamePalette) {
	s := &a.fileRename
	ui.Row(c).FillWidth().Gap(18).AlignItems(ui.Center).Children(func() {
		renameField(c, colors, "rename-number-prefix", &s.numPrefix, "序号前缀", "序号前面字符", s.busy).Grow(1).Basis(0).MinWidth(0)
		ui.Column(c).Width(160).Shrink(0).Padding(8).Gap(6).Radius(3).Border(1, colors.line).Children(func() {
			ui.Text(c, "开始序号").FontSize(11).TextColor(colors.muted)
			ui.NumberInput(c.Key("rename-number-start"), &s.numStart, 0, 9_000_000_000_000_000, 1).FillWidth().Label("编号起始值").Disabled(s.busy)
			ui.Text(c, "序号类型").FontSize(11).TextColor(colors.muted)
			motionSelect(c.Key("rename-number-format"), &s.numFormat, []string{"阿拉伯数字", "中文小写", "中文大写", "英文小写", "英文大写"}).FillWidth().Label("编号格式").Disabled(s.busy)
			if s.numFormat == "阿拉伯数字" {
				ui.Text(c, "固定位数").FontSize(11).TextColor(colors.muted)
				ui.NumberInput(c.Key("rename-number-width"), &s.numWidth, 0, 16, 1).FillWidth().Label("补零位数").Disabled(s.busy)
			}
		})
		renameField(c, colors, "rename-number-suffix", &s.numSuffix, "序号后缀", "序号后面字符", s.busy).Grow(1).Basis(0).MinWidth(0)
	})
}

func renameTool(c *ui.Context, colors renamePalette, key, label string, icon *ui.SVG, disabled bool, click func()) {
	b := ui.ButtonBase(c.Key(key)).Size(34, 34).Radius(3).Center().Label(label).Tooltip(label).Disabled(disabled)
	if !disabled {
		b.Cursor(ui.CursorPointer).OnClick(click)
	}
	if b.Hovered() && !disabled {
		b.Background(colors.line.Alpha(0.4))
	}
	b.TextColor(colors.muted).Children(func() { ui.Icon(c, icon).FontSize(17) })
}

func (a *App) renameToolbar(c *ui.Context, colors renamePalette, changes []fileren.Change, changed int, planErr error) {
	s := &a.fileRename
	services := c.Services()
	ui.Row(c.Key("rename-toolbar")).FillWidth().MinHeight(42).Padding(4, 8).Gap(5).Wrap().AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(colors.line).Background(colors.frame).Children(func() {
		renameTool(c, colors, "rename-import-folder", "读取文件夹内容", iconFolderOpen, s.busy, func() { a.renamePick(true, true) })
		renameTool(c, colors, "rename-import-files", "添加文件", iconFilePlus, s.busy, func() { a.renamePick(false, false) })
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
			Width(142).Label("文件排序").Disabled(s.busy).BorderStyle(ui.BorderDashed).OnChange(a.renameSort)
		count := fmt.Sprintf("已选 %d 个文件", len(s.items))
		ui.Text(c, count).FontSize(12).TextColor(colors.muted).SingleLine().Tooltip(count)
		if s.note != "" {
			ui.Text(c, s.note).FontSize(12).TextColor(colors.muted).SingleLine().Tooltip(s.note)
		}
		ui.Spacer(c)
		if len(s.undo) > 0 {
			ui.Button(c, "撤销").Label("撤销上次重命名").Disabled(s.busy || a.renameUpdateActive()).OnClick(func() { a.renameApply(s.undo, true) })
		}
		buttonText := ui.Hex("#FFFFFF")
		if a.look.dark {
			buttonText = ui.Hex("#171717")
		}
		ui.Button(c.Key("rename-apply"), "执行重命名").FontSize(13).Padding(7, 14).Radius(4).
			Background(colors.blue).TextColor(buttonText).Disabled(s.busy || a.renameUpdateActive() || planErr != nil || changed == 0).
			OnClick(func() { a.renameApply(changes, false) })
	})
}

func (a *App) renameList(c *ui.Context, colors renamePalette, changes []fileren.Change) {
	s := &a.fileRename
	panel := ui.Column(c.Key("rename-list-panel")).Grow(1).Basis(0).MinHeight(100).FillWidth().
		Background(colors.frame).BorderWidth(1, 0, 0, 0).BorderColor(colors.line)
	panel.Children(func() {
		if len(s.items) == 0 {
			ui.Column(c.Key("rename-empty")).Grow(1).FillWidth().Center().Gap(6).Children(func() {
				ui.Icon(c, iconFolderOpen).FontSize(34).TextColor(colors.muted)
				ui.Text(c, "拖放 / 粘贴文件（夹）到这里").FontSize(13).TextColor(colors.muted)
			})
			return
		}
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
			ui.Row(c.Key(item.path)).FillWidth().Height(36).Padding(0, 8).Gap(6).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(colors.line).Children(func() {
				icon := iconFile
				if info, err := os.Lstat(item.path); err == nil && info.IsDir() {
					icon = iconFolder
				}
				ui.Icon(c, icon).FontSize(16).TextColor(colors.muted).Shrink(0)
				ui.Text(c, filepath.Base(item.path)).Grow(1).Basis(0).MinWidth(0).FontSize(13).TextColor(colors.text).SingleLine().Tooltip(item.path)
				mark := ui.Box(c).Size(6, 6).Radius(3).Shrink(0)
				if i < len(changes) && changes[i].From != changes[i].To {
					mark.Background(ui.Hex("#FF6D00"))
				}
				if s.mode == 1 || s.mode == 4 {
					ui.TextInput(c.Key(fmt.Sprintf("rename-draft-%d", s.editVersion)), &item.draft).Grow(1).Basis(0).MinWidth(0).Height(32).Padding(4, 0).
						Radius(0).Background(colors.frame).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).FontSize(13).
						Label("新名称 " + filepath.Base(item.path)).Disabled(s.busy).OnPaste(func(text string) bool { return a.renamePasteAt(i, text) })
				} else {
					name := "—"
					if i < len(changes) {
						name = filepath.Base(changes[i].To)
					}
					ui.Text(c, name).Grow(1).Basis(0).MinWidth(0).Height(32).Padding(7, 0).FontSize(13).
						TextColor(colors.muted).BorderWidth(0, 0, 1, 0).BorderColor(colors.line).SingleLine().Tooltip(name)
				}
				renameTool(c, colors, "remove", "移除 "+filepath.Base(item.path), iconX, s.busy, func() {
					s.items = append(s.items[:i], s.items[i+1:]...)
					s.undo = nil
				})
			})
		}).Grow(1).MinHeight(0).FillWidth()
	})
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
