package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// editPalette 是图像编辑工作台的语义色板，直接映射到 workbenchTokens。
type editPalette struct {
	dark                                                               bool
	tok                                                                tokens
	paper, sheet, subtle, quiet                                        ui.Color
	rule, ink, muted, faint                                            ui.Color
	transfer, transferStrong, transferSoft, transferBorder, onTransfer ui.Color
	amber, amberSoft, blue, blueSoft                                   ui.Color
	violet, violetSoft, green, greenSoft                               ui.Color
	monoSoft                                                           ui.Color
	success, fault, shadow                                             ui.Color
}

func editColors(dark bool) editPalette {
	tok := workbenchTokens(dark)
	blueFg, accentSoft, accentBorder := semanticToneColors(dark, tok.accent, tok.accent, 0.12)
	if dark {
		blueFg, accentSoft, accentBorder = semanticToneColors(dark, tok.accent, tok.accent, 0.16)
	}
	amberFg, amberSoft, _ := semanticToneColors(dark, tok.warning, tok.warning, 0.12)
	violetBase := ui.Hex("#6366F1")
	if dark {
		violetBase = ui.Hex("#818CF8")
	}
	violetFg, violetSoft, _ := semanticToneColors(dark, violetBase, violetBase, 0.12)
	greenFg, greenSoft, _ := semanticToneColors(dark, tok.success, tok.success, 0.12)
	return editPalette{
		dark:           dark,
		tok:            tok,
		paper:          tok.paper,
		sheet:          tok.card,
		subtle:         tok.rail,
		quiet:          tok.sunken,
		rule:           tok.border,
		ink:            tok.text,
		muted:          tok.muted,
		faint:          tok.subtle,
		transfer:       tok.accent,
		transferStrong: tok.accent,
		transferSoft:   accentSoft,
		transferBorder: accentBorder,
		onTransfer:     tok.onAccent,
		amber:          amberFg,
		amberSoft:      amberSoft,
		blue:           blueFg,
		blueSoft:       accentSoft,
		violet:         violetFg,
		violetSoft:     violetSoft,
		green:          greenFg,
		greenSoft:      greenSoft,
		monoSoft:       tok.sunkenHi,
		success:        tok.success,
		fault:          tok.danger,
		shadow:         tok.shadow,
	}
}

func (a *App) editBe() editPalette { return editColors(a.look.dark) }

// imageEditPage 是批处理页：左侧功能栈，右侧预览和胶片条。
func (a *App) imageEditPage(c *ui.Context) {
	a.editEnsureMaps()
	if c.Shortcut(ui.Cmd, ui.KeyV) {
		data := c.Services().ReadClipboardData(transfer.FileList, transfer.URIList)
		if paths, err := data.Files(); err == nil && len(paths) > 0 {
			a.editAddPaths(paths)
		}
	}
	be := a.editBe()
	st := &a.edit
	page := ui.Column(c.Key("image-edit-page")).Fill().MinHeight(0).Background(be.paper)
	if paths := page.DroppedFiles(); len(paths) > 0 {
		a.editAddPaths(paths)
	}
	page.Children(func() {
		a.editHeader(c, be)
		if st.notice != "" {
			okN, failN := a.editRunCounts()
			ui.Row(c.Key("edit-notice-bar")).FillWidth().Padding(5, 12).Gap(8).AlignItems(ui.Center).
				Background(be.quiet).BorderWidth(1, 0, 0, 0).BorderColor(be.rule).Children(func() {
				if failN > 0 {
					uiBadge(c, "edit-notice-badge", fmt.Sprintf("失败 %d", failN), "danger", be.tok)
				} else if okN > 0 {
					uiBadge(c, "edit-notice-badge", fmt.Sprintf("完成 %d", okN), "success", be.tok)
				} else {
					ui.Icon(c, iconInfo).FontSize(12).TextColor(be.muted).Shrink(0)
				}
				ui.Text(c, st.notice).FontSize(be.tok.body).TextColor(be.ink).SingleLine().
					Grow(1).MinWidth(0)
				if okN+failN > 0 {
					ink := be.transferStrong
					if failN > 0 {
						ink = be.fault
					}
					link := ui.Text(c, "查看详情").FontSize(be.tok.caption).FontWeight(500).TextColor(ink).Shrink(0).
						Padding(3, 8).Radius(be.tok.radiusS).Background(be.sheet).Border(1, be.rule).
						Cursor(ui.CursorPointer)
					link.OnClick(func() { st.resultOpen = true })
				}
			})
		}
		body := ui.Row(c.Key("edit-body")).Grow(1).MinHeight(0).AlignItems(ui.Stretch).
			BorderWidth(1, 0, 0, 0).BorderColor(be.rule)
		body.Children(func() {
			a.editAside(c, be)
			a.editStage(c, be)
		})
	})
	a.editRecipeModal(c, be)
	a.editResultModal(c, be)
	a.editEnsurePreview()
	a.editEnsureThumbs()
}

func (a *App) editHeader(c *ui.Context, be editPalette) {
	st := &a.edit
	row := ui.Row(c.Key("edit-header")).FillWidth().Padding(7, 14).Gap(10).
		AlignItems(ui.Center).Background(be.sheet)
	row.Children(func() {
		left := ui.Row(c).Grow(1).MinWidth(0).Gap(6).Wrap().AlignItems(ui.Center)
		left.Children(func() {
			list := a.editChips()
			if len(list) == 0 {
				ui.Text(c, "在左侧启用处理步骤后实时预览").FontSize(be.tok.caption).TextColor(be.muted).SingleLine()
				return
			}
			for _, chip := range list {
				a.editChip(c, be, chip)
			}
		})
		actions := ui.Row(c).Shrink(0).Gap(6).AlignItems(ui.Center)
		actions.Children(func() {
			if len(st.files) > 0 {
				ui.Text(c, fmt.Sprintf("共 %d 张", len(st.files))).FontSize(be.tok.caption).
					TextColor(be.muted).FontFeatures("tnum").Shrink(0)
			}
			uiIconTextBtn(c, "edit-save-recipe", "存为配方", "存为配方", iconSave, 26, false, be.tok).
				OnClick(func() { a.editOpenRecipeModal() })
			if st.running {
				// 跑批时主按钮变成「停止」，这是唯一能中止的地方。
				stop := a.editAction(c, be, "停止", iconStop, true, true)
				stop.OnClick(func() { a.editRequestCancel() })
				return
			}
			label := "保存全部"
			if n := len(a.editPickedIndices()); n > 0 {
				label = fmt.Sprintf("处理选中 %d 张", n)
			}
			canRun := len(st.files) > 0
			run := a.editAction(c, be, label, iconDownload, true, canRun)
			if canRun {
				run.OnClick(func() { a.editRunAll() })
			}
		})
	})
}

func (a *App) editChip(c *ui.Context, be editPalette, chip editChip) {
	row := ui.Row(c.Key("chip-"+chip.ID+chip.Label)).Height(22).Padding(0, 4, 0, 8).Gap(4).
		Radius(be.tok.radiusS).Background(be.transferSoft).
		AlignItems(ui.Center).Shrink(0)
	row.Children(func() {
		ui.Text(c, chip.Label).FontSize(11).FontWeight(500).TextColor(be.transferStrong).SingleLine()
		close := ui.Box(c).Size(14, 14).Center().Radius(3).Cursor(ui.CursorPointer)
		close.Draw(func(p *ui.Painter, r ui.Rect) {
			if close.Hovered() {
				p.Fill(r, be.transferStrong.Alpha(0.16), 3)
			}
		})
		close.OnClick(func() { a.editDisableChip(chip.ID) })
		close.Children(func() {
			ui.Icon(c, iconX).FontSize(9).TextColor(be.transferStrong)
		})
	})
}

func (a *App) editDisableChip(id string) {
	switch id {
	case "gray":
		a.edit.opts.Grayscale = false
	case "resize", "rotate":
		a.editSetFeature("crop", false)
	default:
		a.editSetFeature(id, false)
	}
}

func (a *App) editAside(c *ui.Context, be editPalette) {
	st := &a.edit
	aside := ui.Column(c.Key("edit-ops")).Width(260).FillHeight().MinHeight(0).
		Padding(0).Gap(0).Background(be.sheet).BorderWidth(0, 1, 0, 0).BorderColor(be.rule)
	aside.Children(func() {
		head := ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Padding(8, 12).
			BorderWidth(0, 0, 1, 0).BorderColor(be.rule)
		head.Children(func() {
			ui.Text(c, "处理流程").FontSize(be.tok.title).FontWeight(600).TextColor(be.ink).Grow(1)
			onCount := 0
			if st.enabled.Format {
				onCount++
			}
			if st.enabled.Crop {
				onCount++
			}
			if st.enabled.Adjust {
				onCount++
			}
			if st.opts.Grayscale {
				onCount++
			}
			if onCount > 0 {
				uiBadge(c, "edit-on-count", fmt.Sprintf("%d 项生效", onCount), "accent", be.tok)
			} else {
				ui.Text(c, "未启用功能").FontSize(be.tok.caption).TextColor(be.muted)
			}
			clear := ui.Text(c, "清空").FontSize(be.tok.caption).TextColor(be.muted).
				Padding(2, 4).Radius(be.tok.radiusS).Cursor(ui.CursorPointer)
			if onCount > 0 {
				clear.Draw(func(p *ui.Painter, r ui.Rect) {
					if clear.Hovered() {
						p.Fill(r, be.quiet, be.tok.radiusS)
					}
				})
				clear.OnClick(func() { a.editClearAll() })
				clear.Tooltip("清空全部功能")
			} else {
				clear.Opacity(0.45)
			}
		})

		flow := ui.Scroll(c.Key("edit-flow")).Grow(1).MinHeight(0).TrackScroll(&st.flowScroll)
		flow.Children(func() {
			ui.Column(c).FillWidth().Gap(0).Children(func() {
				a.editRecipeCard(c, be)
				a.editFeatureCard(c, be, "format", "格式转换", "JPEG / PNG / WebP / AVIF",
					iconFileOutput, be.blue, be.blueSoft)
				a.editFeatureCard(c, be, "crop", "裁剪 / 旋转 / 改尺寸", "尺寸·比例·翻转",
					iconCrop, be.violet, be.violetSoft)
				a.editFeatureCard(c, be, "adjust", "画质调整", "亮度·对比·饱和·锐化",
					iconSliders, be.green, be.greenSoft)
				a.editGrayCard(c, be)
			})
		})
		a.editOutputCard(c, be)
	})
}

// editFeatureCard 是一段可折叠的功能区块，采用通栏 1px 底部分隔线，避免窄栏内卡片套卡片。
func (a *App) editFeatureCard(c *ui.Context, be editPalette, id, title, hint string, icon *ui.SVG, tint, tintSoft ui.Color) {
	st := &a.edit
	on, open := false, st.expanded[id]
	switch id {
	case "format":
		on = st.enabled.Format
	case "crop":
		on = st.enabled.Crop
	case "adjust":
		on = st.enabled.Adjust
	}
	card := ui.Column(c.Key("edit-op-"+id)).FillWidth().Radius(0).
		BorderWidth(0, 0, 1, 0).BorderColor(be.rule).Background(be.sheet)
	card.Children(func() {
		a.editCardHead(c, be, id, title, editSummary(id, st.opts, hint), icon, tint, tintSoft, on, open)
		if open {
			body := ui.Column(c.Key("edit-body-"+id)).FillWidth().Padding(8, 12, 10, 12).Gap(6).
				Background(be.paper).BorderWidth(1, 0, 0, 0).BorderColor(be.rule)
			body.Children(func() {
				switch id {
				case "format":
					a.editFormatBody(c, be)
				case "crop":
					a.editCropBody(c, be)
				case "adjust":
					a.editAdjustBody(c, be)
				}
			})
		}
	})
}

// editCardHead 画卡头：展开按钮 + 开关。配方卡单独画。
func (a *App) editCardHead(c *ui.Context, be editPalette, id, title, summary string, icon *ui.SVG, tint, tintSoft ui.Color, on, open bool) {
	head := ui.Row(c).FillWidth().Gap(5).Padding(6, 12, 6, 8).AlignItems(ui.Center)
	head.Children(func() {
		toggle := ui.Row(c).Grow(1).MinWidth(0).Gap(7).Padding(3, 5).Radius(be.tok.radiusS).
			AlignItems(ui.Center).Cursor(ui.CursorPointer)
		toggle.Draw(func(p *ui.Painter, r ui.Rect) {
			if toggle.Hovered() {
				p.Fill(r, be.quiet, be.tok.radiusS)
			}
		})
		toggle.OnClick(func() { a.editToggleExpanded(id) })
		toggle.Children(func() {
			icoBg, icoInk := be.quiet, be.muted
			if on {
				icoBg, icoInk = tintSoft, tint
			}
			box := ui.Box(c).Size(22, 22).Center().Radius(be.tok.radiusS).Background(icoBg).Shrink(0)
			box.Children(func() { ui.Icon(c, icon).FontSize(12).TextColor(icoInk) })
			text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
			text.Children(func() {
				titleRow := ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center)
				titleRow.Children(func() {
					b := ui.Text(c, title).FontSize(12).FontWeight(500).SingleLine()
					if on {
						b.TextColor(be.ink)
						ui.Box(c).Size(5, 5).Radius(3).Background(tint).Shrink(0)
					} else {
						b.TextColor(be.ink.Alpha(0.86))
					}
				})
				ui.Text(c, summary).FontSize(10.5).SingleLine().TextColor(be.muted)
			})
			caret := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(be.muted).Shrink(0)
			if open {
				caret.Rotate(180)
			}
		})
		enabled := on
		a.editSwitch(c, be, enabled, func() {
			a.editSetFeature(id, !enabled)
		})
	})
}

func (a *App) editRecipeCard(c *ui.Context, be editPalette) {
	st := &a.edit
	open := st.expanded["recipe"]
	card := ui.Column(c.Key("edit-op-recipe")).FillWidth().Radius(0).
		BorderWidth(0, 0, 1, 0).BorderColor(be.rule).Background(be.sheet)
	card.Children(func() {
		toggle := ui.Row(c).FillWidth().Gap(7).Padding(8, 12).Radius(0).AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		toggle.Draw(func(p *ui.Painter, r ui.Rect) {
			if toggle.Hovered() {
				p.Fill(r, be.quiet, 0)
			}
		})
		toggle.OnClick(func() { a.editToggleExpanded("recipe") })
		toggle.Children(func() {
			box := ui.Box(c).Size(22, 22).Center().Radius(be.tok.radiusS).Background(be.amberSoft).Shrink(0)
			box.Children(func() { ui.Icon(c, iconSparkles).FontSize(12).TextColor(be.amber) })
			text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
			text.Children(func() {
				ui.Text(c, "我的配方").FontSize(12).FontWeight(500).TextColor(be.ink).SingleLine()
				sub := "调好功能后可存为配方"
				if len(st.recipes) > 0 {
					sub = fmt.Sprintf("%d 个已存配方", len(st.recipes))
				}
				ui.Text(c, sub).FontSize(10.5).TextColor(be.muted).FontFeatures("tnum").SingleLine()
			})
			caret := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(be.muted).Shrink(0)
			if open {
				caret.Rotate(180)
			}
		})
		if open {
			body := ui.Column(c).FillWidth().Padding(6, 12, 8, 12).Gap(2).
				Background(be.paper).BorderWidth(1, 0, 0, 0).BorderColor(be.rule)
			body.Children(func() {
				if len(st.recipes) == 0 {
					ui.Text(c, "暂无配方——调好功能后点顶部「存为配方」").FontSize(10.5).
						TextColor(be.muted).Wrap()
					return
				}
				for i, recipe := range st.recipes {
					index := i
					row := ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center)
					row.Children(func() {
						pick := ui.Text(c, recipe.Name).FontSize(11.5).TextColor(be.ink).Grow(1).
							MinWidth(0).SingleLine().Padding(6, 8).Radius(be.tok.radiusS).Cursor(ui.CursorPointer)
						pick.Draw(func(p *ui.Painter, r ui.Rect) {
							if pick.Hovered() {
								p.Fill(r, be.quiet, be.tok.radiusS)
							}
						})
						pick.OnClick(func() { a.editApplyRecipe(index) })
						del := ui.Box(c).Size(20, 20).Center().Radius(be.tok.radiusS).Cursor(ui.CursorPointer)
						del.Draw(func(p *ui.Painter, r ui.Rect) {
							if del.Hovered() {
								p.Fill(r, be.quiet, be.tok.radiusS)
							}
						})
						del.OnClick(func() { a.editDeleteRecipe(index) })
						del.Children(func() {
							ui.Icon(c, iconTrash).FontSize(12).TextColor(be.muted)
						})
					})
				}
			})
		}
	})
}

func (a *App) editGrayCard(c *ui.Context, be editPalette) {
	st := &a.edit
	on := st.opts.Grayscale
	card := ui.Column(c.Key("edit-op-gray")).FillWidth().Radius(0).
		BorderWidth(0, 0, 1, 0).BorderColor(be.rule).Background(be.sheet)
	card.Children(func() {
		head := ui.Row(c).FillWidth().Gap(5).Padding(6, 12, 6, 8).AlignItems(ui.Center)
		head.Children(func() {
			toggle := ui.Row(c).Grow(1).MinWidth(0).Gap(7).Padding(3, 5).Radius(be.tok.radiusS).
				AlignItems(ui.Center).Cursor(ui.CursorPointer)
			toggle.Draw(func(p *ui.Painter, r ui.Rect) {
				if toggle.Hovered() {
					p.Fill(r, be.quiet, be.tok.radiusS)
				}
			})
			toggle.OnClick(func() { st.opts.Grayscale = !st.opts.Grayscale })
			toggle.Children(func() {
				bg, ink := be.quiet, be.muted
				if on {
					bg, ink = be.monoSoft, be.ink
				}
				box := ui.Box(c).Size(22, 22).Center().Radius(be.tok.radiusS).Background(bg).Shrink(0)
				box.Children(func() { ui.Icon(c, iconContrast).FontSize(12).TextColor(ink) })
				text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
				text.Children(func() {
					titleRow := ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center)
					titleRow.Children(func() {
						title := ui.Text(c, "灰度").FontSize(12).FontWeight(500).SingleLine()
						if on {
							title.TextColor(be.ink)
							ui.Box(c).Size(5, 5).Radius(3).Background(be.transfer).Shrink(0)
						} else {
							title.TextColor(be.ink.Alpha(0.86))
						}
					})
					ui.Text(c, "转为黑白图像").FontSize(10.5).TextColor(be.muted).SingleLine()
				})
			})
			a.editSwitch(c, be, on, func() { st.opts.Grayscale = !on })
		})
	})
}

func (a *App) editOutputCard(c *ui.Context, be editPalette) {
	st := &a.edit
	card := ui.Column(c.Key("edit-op-output")).FillWidth().Radius(0).
		BorderWidth(1, 0, 0, 0).BorderColor(be.rule).Background(be.paper)
	card.Children(func() {
		head := ui.Row(c).FillWidth().Gap(7).Padding(8, 12, 6, 12).AlignItems(ui.Center)
		head.Children(func() {
			box := ui.Box(c).Size(22, 22).Center().Radius(be.tok.radiusS).Background(be.quiet).Shrink(0)
			box.Children(func() { ui.Icon(c, iconFolderOutput).FontSize(12).TextColor(be.muted) })
			text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
			text.Children(func() {
				ui.Text(c, "输出").FontSize(12).FontWeight(500).TextColor(be.ink).SingleLine()
				sub := "目录与命名"
				if strings.TrimSpace(st.outDir) != "" {
					sub = filepath.Base(st.outDir)
				}
				ui.Text(c, sub).FontSize(10.5).TextColor(be.muted).SingleLine()
			})
		})
		body := ui.Column(c).FillWidth().Padding(0, 12, 10, 12).Gap(6)
		body.Children(func() {
			dir := ui.Row(c).FillWidth().Height(28).Padding(0, 8).Gap(7).Radius(be.tok.radiusS).
				Border(1, be.rule).Background(be.quiet).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			dir.Draw(func(p *ui.Painter, r ui.Rect) {
				if dir.Hovered() {
					p.Stroke(r, be.tok.borderStrong, be.tok.radiusS, 1)
				}
			})
			dir.OnClick(func() { a.editPickOutDir(nil) })
			dir.Children(func() {
				ui.Icon(c, iconFolderOpen).FontSize(13).TextColor(be.muted).Shrink(0)
				label := st.outDir
				if strings.TrimSpace(label) == "" {
					label = "选择输出目录"
				}
				ui.Text(c, label).FontSize(11.5).TextColor(be.ink).SingleLine().Grow(1).MinWidth(0)
			})
			ui.Text(c, "文件名后缀").FontSize(11).TextColor(be.muted)
			suffix := ui.TextInput(c.Key("edit-suffix"), &st.suffix).Height(28).FillWidth().
				Radius(be.tok.radiusS).Background(be.quiet).Border(1, be.rule).Padding(0, 8).
				TextColor(be.ink).Label("文件名后缀").Placeholder("如 _edit（可留空）")
			suffix.FontSize(12)
			ui.Text(c, "输出为 原文件名+后缀.扩展名；同名文件自动追加序号").
				FontSize(10.5).TextColor(be.muted).Wrap()
		})
	})
}

// ---- 格式 ----

func (a *App) editFormatBody(c *ui.Context, be editPalette) {
	st := &a.edit
	ui.Text(c, "输出格式").FontSize(11).TextColor(be.muted)
	values := []string{"original", "jpeg", "png", "webp", "avif"}
	if st.opts.Format == "" {
		st.opts.Format = "original"
	}
	sel := ui.SelectBase(c.Key("edit-format"), &st.opts.Format)
	tr := sel.Trigger
	tr.FillWidth().Height(28).Radius(be.tok.radiusS).Border(1, be.rule).Background(be.quiet).Cursor(ui.CursorPointer)
	tr.Children(func() {
		inner := ui.Row(c).FillWidth().Height(26).Padding(0, 8).AlignItems(ui.Center).Gap(6)
		inner.Children(func() {
			ui.Text(c, editFormatLabel(st.opts.Format)).FontSize(12).TextColor(be.ink).Grow(1).SingleLine()
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(be.muted).Shrink(0)
		})
	})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(be.tok.radiusM).Background(be.tok.lift).Border(1, be.tok.borderStrong).
			Shadow(0, 8, 22, 0, be.shadow)
		panel.Children(func() {
			for _, v := range values {
				value := v
				item := sel.Item(value).Padding(6, 8).Radius(be.tok.radiusS).Cursor(ui.CursorPointer)
				if item.Highlighted() {
					item.Background(be.transferSoft)
				}
				item.Children(func() {
					text := ui.Text(c, editFormatLabel(value)).FontSize(12)
					if value == st.opts.Format {
						text.TextColor(be.transferStrong).FontWeight(500)
					} else {
						text.TextColor(be.ink)
					}
				})
			}
		})
	})
	if tr.Changed() {
		a.editMarkUsed("format")
	}
	label := "压缩品质"
	if st.opts.JPEGQuality > 0 {
		label = fmt.Sprintf("压缩品质 · %d", st.opts.JPEGQuality)
	}
	ui.Text(c, label).FontSize(11).TextColor(be.muted).FontFeatures("tnum")
	quality := ui.TextInput(c.Key("edit-quality"), &st.quality).Height(28).FillWidth().
		Radius(be.tok.radiusS).Background(be.quiet).Border(1, be.rule).Padding(0, 8).
		TextColor(be.ink).Label("压缩品质").Placeholder("默认 90")
	quality.FontSize(12)
	if quality.Changed() {
		if n, err := strconv.Atoi(strings.TrimSpace(st.quality)); err == nil && n > 0 {
			st.opts.JPEGQuality = clampInt(n, 1, 100)
			a.editMarkUsed("format")
		} else if strings.TrimSpace(st.quality) == "" {
			st.opts.JPEGQuality = 0
		}
	}
	ui.Text(c, "品质作用于 JPEG / WebP / AVIF 等有损格式").FontSize(10.5).TextColor(be.muted).Wrap()
}

func editFormatLabel(v string) string {
	switch v {
	case "jpeg":
		return "JPEG"
	case "png":
		return "PNG"
	case "webp":
		return "WebP"
	case "avif":
		return "AVIF"
	default:
		return "保持原格式"
	}
}

// ---- 画质调整 ----

func (a *App) editAdjustBody(c *ui.Context, be editPalette) {
	st := &a.edit
	st.fBrightness = float64(st.opts.Brightness)
	st.fContrast = float64(st.opts.Contrast)
	st.fSaturation = float64(st.opts.Saturation)
	st.fBlur = st.opts.Blur

	if v, changed := a.editSlider(c, be, "edit-bright", "亮度", &st.fBrightness, -100, 100, 1); changed {
		st.opts.Brightness = int(math.Round(v))
		a.editMarkUsed("adjust")
	}
	if v, changed := a.editSlider(c, be, "edit-contrast", "对比度", &st.fContrast, -100, 100, 1); changed {
		st.opts.Contrast = int(math.Round(v))
		a.editMarkUsed("adjust")
	}
	if v, changed := a.editSlider(c, be, "edit-sat", "饱和度", &st.fSaturation, -100, 100, 1); changed {
		st.opts.Saturation = int(math.Round(v))
		a.editMarkUsed("adjust")
	}
	if v, changed := a.editSlider(c, be, "edit-blur", "模糊", &st.fBlur, 0, 20, 0.1); changed {
		if v < 0.3 {
			v = 0
		}
		st.opts.Blur = math.Round(v*10) / 10
		a.editMarkUsed("adjust")
	}
	row := ui.Row(c).FillWidth().AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, "锐化").FontSize(11.5).TextColor(be.muted).Grow(1)
		a.editSwitch(c, be, st.opts.Sharpen, func() {
			st.opts.Sharpen = !st.opts.Sharpen
			a.editMarkUsed("adjust")
		})
	})
	a.editSmallButton(c, be, "重置画质调整", func() { a.editResetFeature("adjust") })
}

func (a *App) editSlider(c *ui.Context, be editPalette, key, label string, value *float64, lo, hi, step float64) (float64, bool) {
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	var changed bool
	row.Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(be.muted).Width(44).Shrink(0)
		sl := ui.SliderBase(c.Key(key), value, lo, hi).Step(step).Grow(1).Height(18).MinWidth(0)
		sl.Draw(func(p *ui.Painter, r ui.Rect) {
			trackY := r.Y + r.H/2 - 2
			p.Fill(ui.Rect{X: r.X, Y: trackY, W: r.W, H: 4}, be.tok.borderStrong, 2)
			frac := float32((*value - lo) / (hi - lo))
			frac = float32(math.Min(1, math.Max(0, float64(frac))))
			if frac > 0 {
				p.Fill(ui.Rect{X: r.X, Y: trackY, W: r.W * frac, H: 4}, be.transfer, 2)
			}
			k := float32(12)
			p.Fill(ui.Rect{X: r.X + r.W*frac - k/2, Y: r.Y + r.H/2 - k/2, W: k, H: k}, be.sheet, k/2)
			p.Stroke(ui.Rect{X: r.X + r.W*frac - k/2, Y: r.Y + r.H/2 - k/2, W: k, H: k}, be.transfer, k/2, 1.5)
		})
		if sl.Changed() {
			changed = true
		}
		if sl.DoubleClicked() {
			*value = 0
			changed = true
		}
		v := *value
		text := strconv.FormatFloat(v, 'f', -1, 64)
		if lo < 0 && v > 0 {
			text = "+" + text
		}
		ui.Text(c, text).FontSize(11).TextColor(be.ink).Width(34).TextAlign(ui.End).FontFeatures("tnum")
	})
	return *value, changed
}

// ---- 裁剪 / 旋转 / 改尺寸 ----

func (a *App) editCropBody(c *ui.Context, be editPalette) {
	st := &a.edit
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Text(c, "修改方式").FontSize(11).TextColor(be.muted)
		btns := ui.Row(c).FillWidth().Gap(4)
		btns.Children(func() {
			for _, m := range []struct{ mode, label, tip string }{
				{"crop", "裁剪", "裁剪到目标尺寸"},
				{"fit", "等比缩放", "等比缩放到目标尺寸内，不放大、不变形"},
				{"pad", "纯色背景", "填充透明或纯色背景，不裁剪图片"},
				{"stretch", "拉伸", "不裁剪图片，拉伸到目标尺寸"},
			} {
				mode := m.mode
				box := ui.Box(c.Key("edit-mode-" + mode)).Grow(1).Basis(0).Height(24).Center().
					Radius(be.tok.radiusS).BorderWidth(1).Cursor(ui.CursorPointer).Tooltip(m.tip)
				if st.mode == mode {
					box.Background(be.transferSoft).BorderColor(be.transferBorder)
				} else {
					box.Background(be.quiet).BorderColor(be.rule)
				}
				box.Draw(func(p *ui.Painter, r ui.Rect) {
					if st.mode != mode && box.Hovered() {
						p.Fill(r, be.monoSoft, be.tok.radiusS)
					}
				})
				box.OnClick(func() {
					ratio := ""
					if mode == "crop" {
						ratio = st.opts.CropRatio
					}
					a.editApplyDims(mode, st.dimW, st.dimH, ratio)
				})
				box.Children(func() {
					text := ui.Text(c, m.label).FontSize(11).SingleLine()
					if st.mode == mode {
						text.TextColor(be.transferStrong).FontWeight(500)
					} else {
						text.TextColor(be.muted)
					}
				})
			}
		})
	})

	if st.mode == "crop" {
		a.editSegTabs(c, be, &st.dimTab, []string{"尺寸", "比例"})
	}
	if st.mode != "crop" || st.dimTab == 0 {
		a.editSizeControls(c, be)
	} else {
		a.editRatioControls(c, be)
	}

	if st.mode == "crop" {
		row := ui.Row(c).FillWidth().AlignItems(ui.Center)
		row.Tooltip("开启将应用当前图片裁剪位置到其它图片，关闭则支持每张不同位置")
		row.Children(func() {
			ui.Text(c, "裁剪同一位置").FontSize(11.5).TextColor(be.muted).Grow(1)
			a.editSwitch(c, be, st.uniformCrop, func() { st.uniformCrop = !st.uniformCrop })
		})
	}

	rotate := ui.Row(c).FillWidth().Justify(ui.SpaceAround).Padding(8, 0, 0, 0).
		BorderWidth(1, 0, 0, 0).BorderColor(be.rule)
	rotate.Children(func() {
		a.editRotateButton(c, be, iconRotateCcw, "左旋 90°", false, func() {
			st.opts.Rotate = (st.opts.Rotate + 270) % 360
			a.editMarkUsed("crop")
		})
		a.editRotateButton(c, be, iconRotateCw, "右旋 90°", false, func() {
			st.opts.Rotate = (st.opts.Rotate + 90) % 360
			a.editMarkUsed("crop")
		})
		a.editRotateButton(c, be, iconFlipH, "水平翻转", st.opts.FlipHorizontal, func() {
			st.opts.FlipHorizontal = !st.opts.FlipHorizontal
			a.editMarkUsed("crop")
		})
		a.editRotateButton(c, be, iconFlipV, "垂直翻转", st.opts.FlipVertical, func() {
			st.opts.FlipVertical = !st.opts.FlipVertical
			a.editMarkUsed("crop")
		})
	})

	a.editSmallButton(c, be, "清空本功能", func() { a.editResetFeature("crop") })
}

func (a *App) editRotateButton(c *ui.Context, be editPalette, icon *ui.SVG, tip string, on bool, fn func()) {
	box := ui.Box(c.Key("edit-rot-"+tip)).Size(30, 30).Center().Radius(be.tok.radiusS).
		BorderWidth(1).Cursor(ui.CursorPointer).Tooltip(tip)
	if on {
		box.Background(be.transferSoft).BorderColor(be.transferBorder)
	} else {
		box.BorderColor(be.rule)
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		if !on && box.Hovered() {
			p.Fill(r, be.quiet, be.tok.radiusS)
		}
	})
	box.OnClick(fn)
	box.Children(func() {
		ink := be.muted
		if on {
			ink = be.transferStrong
		}
		ui.Icon(c, icon).FontSize(15).TextColor(ink)
	})
}

// editSegTabs 是带下划线的两个小页签。
func (a *App) editSegTabs(c *ui.Context, be editPalette, value *int, labels []string) {
	row := ui.Row(c).FillWidth().Padding(0, 6).Gap(0).
		BorderWidth(0, 0, 1, 0).BorderColor(be.rule)
	row.Children(func() {
		for i, label := range labels {
			index := i
			tab := ui.Box(c.Key("edit-seg-"+label)).Padding(6, 10, 7, 10).
				Cursor(ui.CursorPointer)
			tab.Draw(func(p *ui.Painter, r ui.Rect) {
				if *value == index {
					p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 2, W: r.W, H: 2}, be.transfer, 0)
				}
			})
			tab.OnClick(func() { *value = index })
			tab.Children(func() {
				text := ui.Text(c, label).FontSize(12)
				if *value == index {
					text.TextColor(be.ink).FontWeight(600)
				} else {
					text.TextColor(be.muted)
				}
			})
		}
	})
}

func (a *App) editMiniTabs(c *ui.Context, be editPalette, value *int, labels []string, key string) {
	row := ui.Row(c).Gap(2).Shrink(0)
	row.Children(func() {
		for i, label := range labels {
			index := i
			tab := ui.Box(c.Key(fmt.Sprintf("%s-%d", key, i))).Padding(3, 8, 5, 8).
				BorderWidth(0, 0, 2, 0).BorderColor(ui.Color{}).Cursor(ui.CursorPointer)
			if *value == index {
				tab.BorderColor(be.transfer)
			}
			tab.OnClick(func() { *value = index })
			tab.Children(func() {
				text := ui.Text(c, label).FontSize(11)
				if *value == index {
					text.TextColor(be.ink).FontWeight(600)
				} else {
					text.TextColor(be.muted)
				}
			})
		}
	})
}

func (a *App) editSizeControls(c *ui.Context, be editPalette) {
	st := &a.edit
	a.editMiniTabs(c, be, &st.ratioTab, []string{"推荐", "我的"}, "edit-size-src")
	list := ui.Scroll(c.Key("edit-size-list")).MaxHeight(168)
	list.Children(func() {
		col := ui.Column(c).FillWidth().Gap(1)
		col.Children(func() {
			type sizeRow struct {
				Label string
				W, H  int
			}
			var rows []sizeRow
			if st.ratioTab == 1 {
				rows = append(rows, sizeRow{"自由调整", 0, 0}, sizeRow{"原尺寸", -1, -1})
				for _, s := range st.sizes {
					rows = append(rows, sizeRow{fmt.Sprintf("%d×%d", s.W, s.H), s.W, s.H})
				}
			} else {
				for _, p := range editSizePresets {
					rows = append(rows, sizeRow{p.Label, p.W, p.H})
				}
			}
			for _, p := range rows {
				label := p.Label
				on := p.W > 0 && st.dimW == p.W && st.dimH == p.H &&
					(st.opts.CropWidth == p.W || st.opts.ResizeWidth == p.W)
				row := ui.Row(c.Key(fmt.Sprintf("edit-size-%s-%d", p.Label, p.W))).FillWidth().
					Padding(6, 8).Radius(be.tok.radiusS).Cursor(ui.CursorPointer).AlignItems(ui.Center)
				if on {
					row.Background(be.transferSoft)
				}
				row.Draw(func(painter *ui.Painter, r ui.Rect) {
					if !on && row.Hovered() {
						painter.Fill(r, be.quiet, be.tok.radiusS)
					}
				})
				row.OnClick(func() {
					if p.W < 0 {
						a.editApplyDims(st.mode, 0, 0, "")
						return
					}
					if p.W == 0 {
						return
					}
					a.editApplyDims(st.mode, p.W, p.H, "")
				})
				row.Children(func() {
					text := ui.Text(c, label).FontSize(12).FontFeatures("tnum").SingleLine()
					if on {
						text.TextColor(be.transferStrong).FontWeight(500)
					} else {
						text.TextColor(be.ink)
					}
				})
			}
			if st.ratioTab == 1 && len(st.sizes) == 0 {
				ui.Text(c, "暂无我的尺寸——输好宽高后点下方保存").FontSize(10.5).TextColor(be.muted).Wrap()
			}
		})
	})

	a.editDimRow(c, be)
	a.editSmallButton(c, be, "存为我的尺寸", func() { a.editSaveMySize() })
}

func (a *App) editDimRow(c *ui.Context, be editPalette) {
	st := &a.edit
	row := ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Margin(8, 0, 0, 0)
	row.Children(func() {
		w := ui.TextInput(c.Key("edit-dim-w"), &st.dimWText).Height(28).Grow(1).Basis(0).
			MinWidth(0).TextAlign(ui.Center).Radius(be.tok.radiusS).Background(be.quiet).
			Border(1, be.rule).TextColor(be.ink).Label("宽度").Placeholder("宽")
		w.FontSize(12).FontFeatures("tnum")
		if w.Changed() {
			a.editOnDimW(editAtoi(st.dimWText))
		}
		link := ui.Box(c).Size(22, 22).Center().Radius(be.tok.radiusS).Cursor(ui.CursorPointer).
			Tooltip(map[bool]string{true: "锁定比例", false: "自由比例"}[st.locked])
		if st.locked {
			link.Background(be.transferSoft).Border(1, be.transferBorder)
		}
		link.OnClick(func() { st.locked = !st.locked })
		link.Children(func() {
			ink := be.muted
			if st.locked {
				ink = be.transfer
			}
			ui.Icon(c, iconLink).FontSize(13).TextColor(ink)
		})
		h := ui.TextInput(c.Key("edit-dim-h"), &st.dimHText).Height(28).Grow(1).Basis(0).
			MinWidth(0).TextAlign(ui.Center).Radius(be.tok.radiusS).Background(be.quiet).
			Border(1, be.rule).TextColor(be.ink).Label("高度").Placeholder("高")
		h.FontSize(12).FontFeatures("tnum")
		if h.Changed() {
			a.editOnDimH(editAtoi(st.dimHText))
		}
		ui.Text(c, "像素").FontSize(11).TextColor(be.muted).Shrink(0)
	})
}

func (a *App) editRatioControls(c *ui.Context, be editPalette) {
	st := &a.edit
	a.editMiniTabs(c, be, &st.ratioTab, []string{"推荐", "我的"}, "edit-ratio-src")
	list := ui.Scroll(c.Key("edit-ratio-list")).MaxHeight(168)
	list.Children(func() {
		col := ui.Column(c).FillWidth().Gap(1)
		col.Children(func() {
			if st.ratioTab == 0 {
				for _, preset := range editRatioPresets {
					a.editRatioRow(c, be, preset.Ratio, preset.Label)
				}
			} else {
				for _, ratio := range st.ratios {
					a.editRatioRow(c, be, ratio, "")
				}
				if len(st.ratios) == 0 {
					ui.Text(c, "暂无我的比例——输好尺寸后点下方保存").FontSize(10.5).TextColor(be.muted).Wrap()
				}
			}
		})
	})
	if w, h, ok := a.editResolvedRatio(); ok {
		ui.Text(c, fmt.Sprintf("当前图片 %d×%d", w, h)).FontSize(10.5).TextColor(be.muted).FontFeatures("tnum")
	}
	a.editSmallButton(c, be, "存为我的比例", func() { a.editSaveMyRatio() })
}

func (a *App) editRatioRow(c *ui.Context, be editPalette, ratio, label string) {
	st := &a.edit
	on := st.opts.CropRatio == ratio
	row := ui.Row(c.Key("edit-ratio-"+ratio)).FillWidth().Gap(8).Padding(6, 8).Radius(be.tok.radiusS).
		Cursor(ui.CursorPointer).AlignItems(ui.Baseline)
	if on {
		row.Background(be.transferSoft)
	}
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		if !on && row.Hovered() {
			p.Fill(r, be.quiet, be.tok.radiusS)
		}
	})
	row.OnClick(func() { a.editPickRatio(ratio) })
	row.Children(func() {
		text := ui.Text(c, ratio).FontSize(12).FontFeatures("tnum").SingleLine()
		if on {
			text.TextColor(be.transferStrong).FontWeight(500)
		} else {
			text.TextColor(be.ink)
		}
		if label != "" {
			ui.Text(c, label).FontSize(10.5).TextColor(be.muted).SingleLine()
		}
	})
}

func (a *App) editResolvedRatio() (int, int, bool) {
	st := &a.edit
	if st.opts.CropRatio == "" || st.srcW <= 0 || st.srcH <= 0 {
		return 0, 0, false
	}
	parts := strings.Split(st.opts.CropRatio, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	rw, err1 := strconv.ParseFloat(parts[0], 64)
	rh, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || rw <= 0 || rh <= 0 {
		return 0, 0, false
	}
	r := rw / rh
	if float64(st.srcW)/float64(st.srcH) > r {
		return int(math.Round(float64(st.srcH) * r)), st.srcH, true
	}
	return st.srcW, int(math.Round(float64(st.srcW) / r)), true
}

// ---- 小部件 ----

func (a *App) editSmallButton(c *ui.Context, be editPalette, label string, fn func()) {
	btn := ui.Button(c, label).FontSize(11).Padding(4, 10).Radius(be.tok.radiusS).Shrink(0).
		Background(be.quiet).Border(1, be.rule).TextColor(be.ink)
	hoverPressPose(btn)
	btn.OnClick(fn)
}

func (a *App) editAction(c *ui.Context, be editPalette, label string, icon *ui.SVG, primary, enabled bool) ui.Element {
	ink := be.ink
	el := ui.Row(c).Height(26).Padding(0, 10).Gap(5).Radius(be.tok.radiusS).AlignItems(ui.Center).Shrink(0)
	if primary {
		bg := be.transfer
		if label == "停止" {
			bg = be.fault
		}
		el.Background(bg).Shadow(0, 1, 2, 0, be.shadow)
		ink = be.onTransfer
	} else {
		el.Background(be.sheet).Border(1, be.rule)
	}
	if enabled {
		el.Cursor(ui.CursorPointer)
	} else {
		el.Opacity(0.45)
	}
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		if !enabled || !el.Hovered() {
			return
		}
		if primary {
			p.Fill(r, ui.RGBA(0, 0, 0, 0.1), be.tok.radiusS)
		} else {
			p.Fill(r, be.quiet, be.tok.radiusS)
		}
	})
	el.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(13).TextColor(ink)
		}
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(ink).FontFeatures("tnum").SingleLine()
	})
	return el
}

// editSwitch 是 28×16 的紧凑开关。
func (a *App) editSwitch(c *ui.Context, be editPalette, on bool, fn func()) ui.Element {
	sw := ui.Box(c).Size(28, 16).Radius(8).Shrink(0).Cursor(ui.CursorPointer)
	if on {
		sw.Background(be.transfer)
	} else {
		sw.Background(be.tok.borderStrong)
	}
	sw.Draw(func(p *ui.Painter, r ui.Rect) {
		pad := float32(2)
		knob := r.H - pad*2
		x := r.X + pad
		if on {
			x = r.X + r.W - pad - knob
		}
		p.Fill(ui.Rect{X: x, Y: r.Y + pad, W: knob, H: knob}, be.sheet, knob/2)
	})
	sw.OnClick(fn)
	return sw
}

// ---- 配方弹窗 ----

func (a *App) editOpenRecipeModal() {
	st := &a.edit
	st.recipeOpen = true
	st.recipeName = fmt.Sprintf("配方 %d", len(st.recipes)+1)
	st.recipeErr = ""
}

func (a *App) editRecipeModal(c *ui.Context, be editPalette) {
	st := &a.edit
	if !st.recipeOpen {
		return
	}
	ui.Modal(c, &st.recipeOpen, func() {
		ui.Column(c.Key("edit-recipe-modal")).Width(380).Gap(12).Padding(16).
			Background(be.tok.lift).Border(1, be.tok.borderStrong).Radius(be.tok.radiusL).
			Shadow(0, 10, 28, 0, be.shadow).Children(func() {
			ui.Text(c, "存为配方").FontSize(be.tok.display).FontWeight(600).TextColor(be.ink)
			ui.Text(c, "保存当前生效的功能，之后一键套用。").FontSize(12).TextColor(be.muted)
			ui.Text(c, "配方名称").FontSize(11.5).TextColor(be.muted)
			name := ui.TextInput(c.Key("edit-recipe-name"), &st.recipeName).Height(30).FillWidth().
				Radius(be.tok.radiusS).Background(be.quiet).Border(1, be.rule).Padding(0, 8).
				TextColor(be.ink).Label("配方名称")
			name.FontSize(13)
			if st.recipeErr != "" {
				ui.Text(c, st.recipeErr).FontSize(11.5).TextColor(be.fault)
			}
			row := ui.Row(c).FillWidth().Justify(ui.End).Gap(8)
			row.Children(func() {
				cancel := ui.Button(c, "取消").FontSize(12).Padding(6, 12).Radius(be.tok.radiusS).
					Background(be.sheet).Border(1, be.rule).TextColor(be.ink)
				hoverPressPose(cancel)
				cancel.OnClick(func() { st.recipeOpen = false })
				save := ui.Button(c, "保存").FontSize(12).Padding(6, 12).Radius(be.tok.radiusS).
					Background(be.transfer).TextColor(be.onTransfer)
				hoverPressPose(save)
				save.OnClick(func() { a.editSaveRecipe(st.recipeName) })
			})
		})
	})
}

// editResultModal 列出这一轮的结果；失败项带上原因，可以只重跑它们。
func (a *App) editResultModal(c *ui.Context, be editPalette) {
	st := &a.edit
	if !st.resultOpen {
		return
	}
	okN, failN := a.editRunCounts()
	ui.Modal(c, &st.resultOpen, func() {
		col := ui.Column(c.Key("edit-result-modal")).Width(440).Gap(10).Padding(16).
			Background(be.tok.lift).Border(1, be.tok.borderStrong).Radius(be.tok.radiusL).
			Shadow(0, 10, 28, 0, be.shadow)
		col.Children(func() {
			ui.Text(c, "处理结果").FontSize(be.tok.display).FontWeight(600).TextColor(be.ink)
			summary := fmt.Sprintf("完成 %d 个", okN)
			if failN > 0 {
				summary += fmt.Sprintf(" · 失败 %d 个", failN)
			}
			if dir := strings.TrimSpace(st.outDir); dir != "" {
				summary += " → " + dir
			}
			ui.Text(c, summary).FontSize(11.5).TextColor(be.muted).FontFeatures("tnum").SingleLine()
			if failN > 0 {
				height := float32(math.Min(220, float64(failN)*38+4))
				list := ui.Scroll(c.Key("edit-result-list")).FillWidth().Height(height)
				list.Children(func() {
					for _, f := range st.files {
						if f.Status != "failed" {
							continue
						}
						row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Padding(4, 0)
						row.Children(func() {
							ui.Icon(c, iconAlert).FontSize(13).TextColor(be.fault)
							ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
								ui.Text(c, f.Name).FontSize(12).TextColor(be.ink).SingleLine()
								ui.Text(c, f.Err).FontSize(11).TextColor(be.muted)
							})
						})
					}
				})
			}
			footer := ui.Row(c).FillWidth().Justify(ui.End).Gap(8).AlignItems(ui.Center)
			footer.Children(func() {
				if strings.TrimSpace(st.outDir) != "" {
					open := ui.Button(c, "打开输出目录").FontSize(12).Padding(6, 12).Radius(be.tok.radiusS).
						Background(be.sheet).Border(1, be.rule).TextColor(be.ink)
					hoverPressPose(open)
					open.OnClick(func() { a.editOpenOutDir() })
				}
				if failN > 0 {
					retry := ui.Button(c, fmt.Sprintf("重试失败 %d 张", failN)).FontSize(12).Padding(6, 12).Radius(be.tok.radiusS).
						Background(be.transfer).TextColor(be.onTransfer)
					hoverPressPose(retry)
					retry.OnClick(func() {
						st.resultOpen = false
						a.editRetryFailed()
					})
				}
				close := ui.Button(c, "关闭").FontSize(12).Padding(6, 12).Radius(be.tok.radiusS).
					Background(be.sheet).Border(1, be.rule).TextColor(be.ink)
				hoverPressPose(close)
				close.OnClick(func() { st.resultOpen = false })
			})
		})
	})
}

func editAtoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// editOnDimW/H 对应 moza 的两个尺寸输入，锁比时联动另一轴。
func (a *App) editOnDimW(v int) {
	st := &a.edit
	v = clampInt(v, 0, 65535)
	h := st.dimH
	if st.locked && st.srcW > 0 && st.srcH > 0 && v > 0 {
		h = int(math.Round(float64(v) * float64(st.srcH) / float64(st.srcW)))
	}
	a.editApplyDims(st.mode, v, h, "")
}

func (a *App) editOnDimH(v int) {
	st := &a.edit
	v = clampInt(v, 0, 65535)
	w := st.dimW
	if st.locked && st.srcW > 0 && st.srcH > 0 && v > 0 {
		w = int(math.Round(float64(v) * float64(st.srcW) / float64(st.srcH)))
	}
	a.editApplyDims(st.mode, w, v, "")
}
