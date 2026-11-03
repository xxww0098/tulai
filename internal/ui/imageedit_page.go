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

// editPalette 是 moza 批处理页的取色。深浅色各一套。
type editPalette struct {
	dark                                                               bool
	paper, sheet, subtle, quiet                                        ui.Color
	rule, ink, muted, faint                                            ui.Color
	transfer, transferStrong, transferSoft, transferBorder, onTransfer ui.Color
	amber, amberSoft, blue, blueSoft                                   ui.Color
	violet, violetSoft, green, greenSoft                               ui.Color
	monoSoft                                                           ui.Color
	success, fault, shadow                                             ui.Color
}

func editColors(dark bool) editPalette {
	if dark {
		return editPalette{
			dark:           true,
			paper:          ui.Hex("#161618"),
			sheet:          ui.Hex("#1e1e22"),
			subtle:         ui.Hex("#26262b"),
			quiet:          ui.Hex("#232328"),
			rule:           ui.Hex("#333338"),
			ink:            ui.Hex("#ececef"),
			muted:          ui.Hex("#b5b5bb"),
			faint:          ui.Hex("#8e8e95"),
			transfer:       ui.Hex("#4a90f5"),
			transferStrong: ui.Hex("#93c5fd"),
			transferSoft:   ui.Hex("#4a90f5").Mix(ui.Hex("#1e1e22"), 0.84),
			transferBorder: ui.Hex("#4a90f5").Mix(ui.Hex("#1e1e22"), 0.66),
			onTransfer:     ui.Hex("#0b1220"),
			amber:          ui.Hex("#fbbf24"),
			amberSoft:      ui.Hex("#fbbf24").Mix(ui.Hex("#1e1e22"), 0.84),
			blue:           ui.Hex("#60a5fa"),
			blueSoft:       ui.Hex("#60a5fa").Mix(ui.Hex("#1e1e22"), 0.82),
			violet:         ui.Hex("#a78bfa"),
			violetSoft:     ui.Hex("#a78bfa").Mix(ui.Hex("#1e1e22"), 0.84),
			green:          ui.Hex("#4ade80"),
			greenSoft:      ui.Hex("#4ade80").Mix(ui.Hex("#1e1e22"), 0.84),
			monoSoft:       ui.Hex("#333338"),
			success:        ui.Hex("#4ade80"),
			fault:          ui.Hex("#f87171"),
			shadow:         ui.RGBA(0, 0, 0, 0.36),
		}
	}
	return editPalette{
		paper:          ui.Hex("#e4e3e3"),
		sheet:          ui.Hex("#ffffff"),
		subtle:         ui.Hex("#f3f6fb"),
		quiet:          ui.Hex("#f7f9fc"),
		rule:           ui.Hex("#dce3f0"),
		ink:            ui.Hex("#1a2332"),
		muted:          ui.Hex("#4b556f"),
		faint:          ui.Hex("#64708a"),
		transfer:       ui.Hex("#3b82f6"),
		transferStrong: ui.Hex("#2563eb"),
		transferSoft:   ui.Hex("#e8f0fe"),
		transferBorder: ui.Hex("#bfd3fc"),
		onTransfer:     ui.Hex("#ffffff"),
		amber:          ui.Hex("#f59e0b"),
		amberSoft:      ui.Hex("#fff4df"),
		blue:           ui.Hex("#3b82f6"),
		blueSoft:       ui.Hex("#e0ebff"),
		violet:         ui.Hex("#8b5cf6"),
		violetSoft:     ui.Hex("#efe9ff"),
		green:          ui.Hex("#22c55e"),
		greenSoft:      ui.Hex("#e4f8ec"),
		monoSoft:       ui.Hex("#dce3f0"),
		success:        ui.Hex("#22c55e"),
		fault:          ui.Hex("#ef4444"),
		shadow:         ui.RGBA(42, 58, 96, 0.06),
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
			// moza 的提示只有浅灰文字，不套底色；跑过一轮后右边给个「查看详情」。
			okN, failN := a.editRunCounts()
			ui.Row(c).FillWidth().Padding(0, 24).AlignItems(ui.Center).Children(func() {
				ui.Text(c, st.notice).FontSize(12).TextColor(be.muted).SingleLine().
					Grow(1).MinWidth(0).Padding(6, 10)
				if okN+failN > 0 {
					ink := be.transfer
					if failN > 0 {
						ink = be.fault
					}
					link := ui.Text(c, "查看详情").FontSize(12).TextColor(ink).Shrink(0).
						Padding(6, 10).Cursor(ui.CursorPointer)
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
	row := ui.Row(c.Key("edit-header")).FillWidth().Padding(14, 24, 12, 24).Gap(16).AlignItems(ui.Center)
	row.Children(func() {
		left := ui.Row(c).Grow(1).MinWidth(0).Gap(14).AlignItems(ui.Center)
		left.Children(func() {
			ui.Text(c, "批处理").FontSize(15).FontWeight(500).TextColor(be.ink).Shrink(0)
			chips := ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).MinWidth(0)
			chips.Children(func() {
				list := a.editChips()
				if len(list) == 0 {
					ui.Text(c, "未选功能").FontSize(12).TextColor(be.faint)
					return
				}
				for _, chip := range list {
					a.editChip(c, be, chip)
				}
			})
		})
		actions := ui.Row(c).Shrink(0).Gap(8).AlignItems(ui.Center)
		actions.Children(func() {
			save := a.editAction(c, be, "存为配方", iconSave, false, true)
			save.OnClick(func() { a.editOpenRecipeModal() })
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
	row := ui.Row(c.Key("chip-"+chip.ID+chip.Label)).Height(22).Padding(0, 4, 0, 10).Gap(4).
		Radius(11).Background(be.transferSoft).AlignItems(ui.Center).Shrink(0)
	row.Children(func() {
		ui.Text(c, chip.Label).FontSize(11.5).FontWeight(500).TextColor(be.transferStrong).SingleLine()
		close := ui.Box(c).Size(16, 16).Center().Radius(8).Cursor(ui.CursorPointer)
		close.Draw(func(p *ui.Painter, r ui.Rect) {
			if close.Hovered() {
				p.Fill(r, be.transferStrong.Alpha(0.18), 8)
			}
		})
		close.OnClick(func() { a.editDisableChip(chip.ID) })
		close.Children(func() {
			ui.Icon(c, iconX).FontSize(10).TextColor(be.transferStrong)
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
	aside := ui.Column(c.Key("edit-ops")).Width(320).FillHeight().MinHeight(0).
		Padding(12).Gap(8).Background(be.subtle).BorderWidth(0, 1, 0, 0).BorderColor(be.rule)
	aside.Children(func() {
		head := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(0, 4, 2, 4)
		head.Children(func() {
			ui.Text(c, "处理流程").FontSize(12).FontWeight(500).TextColor(be.faint).Grow(1)
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
			count := "未启用功能"
			if onCount > 0 {
				count = fmt.Sprintf("%d 项生效", onCount)
			}
			ui.Text(c, count).FontSize(10.5).TextColor(be.faint).FontFeatures("tnum")
			clear := ui.Text(c, "清空").FontSize(10.5).TextColor(be.faint).Cursor(ui.CursorPointer)
			if onCount > 0 {
				clear.OnClick(func() { a.editClearAll() })
				clear.Tooltip("清空全部功能")
			} else {
				clear.Opacity(0.45)
			}
		})

		flow := ui.Scroll(c.Key("edit-flow")).Grow(1).MinHeight(0).TrackScroll(&st.flowScroll)
		flow.Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
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

// editFeatureCard 是一张带开关的功能卡。停用平躺，启用或展开浮起。
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
	card := ui.Column(c.Key("edit-op-" + id)).FillWidth().Radius(12).
		BorderWidth(1).BorderColor(be.rule).Background(be.sheet)
	if !on && !open {
		card.BorderColor(ui.Color{}).Background(ui.Color{})
	} else {
		card.Shadow(0, 1, 2, 0, be.shadow)
	}
	card.Children(func() {
		a.editCardHead(c, be, id, title, editSummary(id, st.opts, hint), icon, tint, tintSoft, on, open)
		if open {
			body := ui.Column(c.Key("edit-body-"+id)).FillWidth().Padding(8, 10, 12, 10).Gap(8).
				BorderWidth(1, 0, 0, 0).BorderColor(be.rule)
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
	head := ui.Row(c).FillWidth().Gap(6).Padding(5, 10, 5, 5).AlignItems(ui.Center)
	head.Children(func() {
		toggle := ui.Row(c).Grow(1).MinWidth(0).Gap(9).Padding(3, 4).Radius(9).
			AlignItems(ui.Center).Cursor(ui.CursorPointer)
		toggle.Draw(func(p *ui.Painter, r ui.Rect) {
			if toggle.Hovered() {
				p.Fill(r, be.quiet, 9)
			}
		})
		toggle.OnClick(func() { a.editToggleExpanded(id) })
		toggle.Children(func() {
			icoBg, icoInk := be.quiet, be.faint
			if on {
				icoBg, icoInk = tintSoft, tint
			}
			box := ui.Box(c).Size(26, 26).Center().Radius(8).Background(icoBg).Shrink(0)
			box.Children(func() { ui.Icon(c, icon).FontSize(14).TextColor(icoInk) })
			text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
			text.Children(func() {
				b := ui.Text(c, title).FontSize(12.5).FontWeight(500).SingleLine()
				if on {
					b.TextColor(be.ink)
				} else {
					b.TextColor(be.muted)
				}
				em := ui.Text(c, summary).FontSize(10.5).SingleLine()
				if on {
					em.TextColor(be.muted)
				} else {
					em.TextColor(be.faint)
				}
			})
			caret := ui.Icon(c, iconChevronDown).FontSize(13).TextColor(be.faint).Shrink(0)
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
	card := ui.Column(c.Key("edit-op-recipe")).FillWidth().Radius(12).
		BorderWidth(1).BorderColor(be.rule).Background(be.sheet)
	if !open {
		card.BorderColor(ui.Color{}).Background(ui.Color{})
	} else {
		card.Shadow(0, 1, 2, 0, be.shadow)
	}
	card.Children(func() {
		toggle := ui.Row(c).FillWidth().Gap(9).Padding(8, 10, 8, 8).Radius(9).AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		toggle.Draw(func(p *ui.Painter, r ui.Rect) {
			if toggle.Hovered() {
				p.Fill(r, be.quiet, 9)
			}
		})
		toggle.OnClick(func() { a.editToggleExpanded("recipe") })
		toggle.Children(func() {
			box := ui.Box(c).Size(26, 26).Center().Radius(8).Background(be.amberSoft).Shrink(0)
			box.Children(func() { ui.Icon(c, iconSparkles).FontSize(14).TextColor(be.amber) })
			text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
			text.Children(func() {
				ui.Text(c, "我的配方").FontSize(12.5).FontWeight(500).TextColor(be.ink).SingleLine()
				sub := "调好功能后可存为配方"
				if len(st.recipes) > 0 {
					sub = fmt.Sprintf("%d 个已存配方", len(st.recipes))
				}
				ui.Text(c, sub).FontSize(10.5).TextColor(be.faint).SingleLine()
			})
			caret := ui.Icon(c, iconChevronDown).FontSize(13).TextColor(be.faint).Shrink(0)
			if open {
				caret.Rotate(180)
			}
		})
		if open {
			body := ui.Column(c).FillWidth().Padding(8, 10, 12, 10).Gap(2).
				BorderWidth(1, 0, 0, 0).BorderColor(be.rule)
			body.Children(func() {
				if len(st.recipes) == 0 {
					ui.Text(c, "暂无配方——调好功能后点顶部「存为配方」").FontSize(10.5).
						TextColor(be.faint).Wrap()
					return
				}
				for i, recipe := range st.recipes {
					index := i
					row := ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center)
					row.Children(func() {
						pick := ui.Text(c, recipe.Name).FontSize(11.5).TextColor(be.ink).Grow(1).
							MinWidth(0).SingleLine().Padding(6, 8).Radius(6).Cursor(ui.CursorPointer)
						pick.Draw(func(p *ui.Painter, r ui.Rect) {
							if pick.Hovered() {
								p.Fill(r, be.quiet, 6)
							}
						})
						pick.OnClick(func() { a.editApplyRecipe(index) })
						del := ui.Box(c).Size(20, 20).Center().Radius(6).Cursor(ui.CursorPointer)
						del.Draw(func(p *ui.Painter, r ui.Rect) {
							if del.Hovered() {
								p.Fill(r, be.quiet, 6)
							}
						})
						del.OnClick(func() { a.editDeleteRecipe(index) })
						del.Children(func() {
							ui.Icon(c, iconTrash).FontSize(12).TextColor(be.faint)
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
	card := ui.Column(c.Key("edit-op-gray")).FillWidth().Radius(12).
		BorderWidth(1).BorderColor(be.rule).Background(be.sheet)
	if !on {
		card.BorderColor(ui.Color{}).Background(ui.Color{})
	} else {
		card.Shadow(0, 1, 2, 0, be.shadow)
	}
	card.Children(func() {
		head := ui.Row(c).FillWidth().Gap(6).Padding(5, 10, 5, 5).AlignItems(ui.Center)
		head.Children(func() {
			toggle := ui.Row(c).Grow(1).MinWidth(0).Gap(9).Padding(3, 4).Radius(9).
				AlignItems(ui.Center).Cursor(ui.CursorPointer)
			toggle.Draw(func(p *ui.Painter, r ui.Rect) {
				if toggle.Hovered() {
					p.Fill(r, be.quiet, 9)
				}
			})
			toggle.OnClick(func() { st.opts.Grayscale = !st.opts.Grayscale })
			toggle.Children(func() {
				bg, ink := be.quiet, be.faint
				if on {
					bg, ink = be.monoSoft, be.ink
				}
				box := ui.Box(c).Size(26, 26).Center().Radius(8).Background(bg).Shrink(0)
				box.Children(func() { ui.Icon(c, iconContrast).FontSize(14).TextColor(ink) })
				text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
				text.Children(func() {
					title := ui.Text(c, "灰度").FontSize(12.5).FontWeight(500).SingleLine()
					if on {
						title.TextColor(be.ink)
					} else {
						title.TextColor(be.muted)
					}
					ui.Text(c, "转为黑白图像").FontSize(10.5).TextColor(be.faint).SingleLine()
				})
			})
			a.editSwitch(c, be, on, func() { st.opts.Grayscale = !on })
		})
	})
}

func (a *App) editOutputCard(c *ui.Context, be editPalette) {
	st := &a.edit
	card := ui.Column(c.Key("edit-op-output")).FillWidth().Radius(12).
		BorderWidth(1).BorderColor(be.rule).Background(be.sheet).Shadow(0, 1, 2, 0, be.shadow)
	card.Children(func() {
		head := ui.Row(c).FillWidth().Gap(9).Padding(8, 10, 5, 9).AlignItems(ui.Center)
		head.Children(func() {
			box := ui.Box(c).Size(26, 26).Center().Radius(8).Background(be.quiet).Shrink(0)
			box.Children(func() { ui.Icon(c, iconFolderOutput).FontSize(14).TextColor(be.faint) })
			text := ui.Column(c).Grow(1).MinWidth(0).Gap(1)
			text.Children(func() {
				ui.Text(c, "输出").FontSize(12.5).FontWeight(500).TextColor(be.ink).SingleLine()
				sub := "目录与命名"
				if strings.TrimSpace(st.outDir) != "" {
					sub = filepath.Base(st.outDir)
				}
				ui.Text(c, sub).FontSize(10.5).TextColor(be.faint).SingleLine()
			})
		})
		body := ui.Column(c).FillWidth().Padding(8, 10, 12, 10).Gap(8)
		body.Children(func() {
			dir := ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(7).Radius(8).
				Border(1, be.rule).Background(be.quiet).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			dir.OnClick(func() { a.editPickOutDir(nil) })
			dir.Children(func() {
				ui.Icon(c, iconFolderOpen).FontSize(13).TextColor(be.faint).Shrink(0)
				label := st.outDir
				if strings.TrimSpace(label) == "" {
					label = "选择输出目录"
				}
				ui.Text(c, label).FontSize(11.5).TextColor(be.ink).SingleLine().Grow(1).MinWidth(0)
			})
			ui.Text(c, "文件名后缀").FontSize(11.5).TextColor(be.muted)
			suffix := ui.TextInput(c.Key("edit-suffix"), &st.suffix).Height(28).FillWidth().
				Placeholder("如 _edit（可留空）")
			suffix.FontSize(12)
			ui.Text(c, "输出为 原文件名+后缀.扩展名；同名文件自动追加序号").
				FontSize(10.5).TextColor(be.faint).Wrap()
		})
	})
}

// ---- 格式 ----

func (a *App) editFormatBody(c *ui.Context, be editPalette) {
	st := &a.edit
	ui.Text(c, "输出格式").FontSize(11.5).TextColor(be.muted)
	values := []string{"original", "jpeg", "png", "webp", "avif"}
	if st.opts.Format == "" {
		st.opts.Format = "original"
	}
	sel := ui.SelectBase(c.Key("edit-format"), &st.opts.Format)
	tr := sel.Trigger
	tr.FillWidth().Height(28).Radius(6).Border(1, be.rule).Background(be.sheet).Cursor(ui.CursorPointer)
	tr.Children(func() {
		inner := ui.Row(c).FillWidth().Height(26).Padding(0, 10).AlignItems(ui.Center).Gap(6)
		inner.Children(func() {
			ui.Text(c, editFormatLabel(st.opts.Format)).FontSize(12).TextColor(be.ink).Grow(1).SingleLine()
			ui.Icon(c, iconChevronDown).FontSize(12).TextColor(be.faint).Shrink(0)
		})
	})
	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(8).Background(be.sheet).Border(1, be.rule).Shadow(0, 6, 20, 0, be.shadow)
		panel.Children(func() {
			for _, v := range values {
				value := v
				item := sel.Item(value).Padding(6, 10).Radius(6).Cursor(ui.CursorPointer)
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
	ui.Text(c, label).FontSize(11.5).TextColor(be.muted)
	quality := ui.TextInput(c.Key("edit-quality"), &st.quality).Height(28).FillWidth().
		Placeholder("默认 90")
	quality.FontSize(12)
	if quality.Changed() {
		if n, err := strconv.Atoi(strings.TrimSpace(st.quality)); err == nil && n > 0 {
			st.opts.JPEGQuality = clampInt(n, 1, 100)
			a.editMarkUsed("format")
		} else if strings.TrimSpace(st.quality) == "" {
			st.opts.JPEGQuality = 0
		}
	}
	ui.Text(c, "品质作用于 JPEG / WebP / AVIF 等有损格式").FontSize(10.5).TextColor(be.faint).Wrap()
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
			p.Fill(ui.Rect{X: r.X, Y: trackY, W: r.W, H: 4}, be.rule, 2)
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
	modeRow := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center)
	modeRow.Children(func() {
		ui.Text(c, "修改方式").FontSize(12).TextColor(be.muted).Shrink(0)
		btns := ui.Row(c).Grow(1).Gap(4)
		btns.Children(func() {
			for _, m := range []struct{ mode, label, tip string }{
				{"crop", "裁剪", "裁剪到目标尺寸"},
				{"fit", "等比缩放", "等比缩放到目标尺寸内，不放大、不变形"},
				{"pad", "纯色背景", "填充透明或纯色背景，不裁剪图片"},
				{"stretch", "拉伸", "不裁剪图片，拉伸到目标尺寸"},
			} {
				mode := m.mode
				box := ui.Box(c.Key("edit-mode-" + mode)).Grow(1).Basis(0).Height(26).Center().
					Radius(13).Cursor(ui.CursorPointer).Tooltip(m.tip)
				if st.mode == mode {
					box.Background(be.transferSoft)
				} else {
					box.Background(be.quiet)
				}
				box.Draw(func(p *ui.Painter, r ui.Rect) {
					if st.mode != mode && box.Hovered() {
						p.Fill(r, be.rule.Alpha(0.55), 13)
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
					text := ui.Text(c, m.label).FontSize(11.5).SingleLine()
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
	box := ui.Box(c.Key("edit-rot-"+tip)).Size(30, 30).Center().Radius(7).
		Cursor(ui.CursorPointer).Tooltip(tip)
	if on {
		box.Background(be.transferSoft)
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		if !on && box.Hovered() {
			p.Fill(r, be.quiet, 7)
		}
	})
	box.OnClick(fn)
	box.Children(func() {
		ink := be.muted
		if on {
			ink = be.transferStrong
		}
		ui.Icon(c, icon).FontSize(16).TextColor(ink)
	})
}

// editSegTabs 是带下划线的两个小页签。
func (a *App) editSegTabs(c *ui.Context, be editPalette, value *int, labels []string) {
	row := ui.Row(c).FillWidth().Padding(0, 10).Gap(0).
		BorderWidth(0, 0, 1, 0).BorderColor(be.rule)
	row.Children(func() {
		for i, label := range labels {
			index := i
			tab := ui.Box(c.Key("edit-seg-"+label)).Padding(6, 10, 8, 10).
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
					text.TextColor(be.ink).FontWeight(500)
				} else {
					text.TextColor(be.muted)
				}
			})
		}
	})
}

func (a *App) editMiniTabs(c *ui.Context, be editPalette, value *int, labels []string, key string) {
	row := ui.Row(c).Gap(0).Shrink(0)
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
					text.TextColor(be.ink).FontWeight(500)
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
					Padding(7, 10).Radius(7).Cursor(ui.CursorPointer).AlignItems(ui.Center)
				if on {
					row.Background(be.transferSoft)
				}
				row.Draw(func(painter *ui.Painter, r ui.Rect) {
					if !on && row.Hovered() {
						painter.Fill(r, be.quiet, 7)
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
					text := ui.Text(c, label).FontSize(12.5).SingleLine()
					if on {
						text.TextColor(be.transferStrong).FontWeight(500)
					} else {
						text.TextColor(be.ink)
					}
				})
			}
			if st.ratioTab == 1 && len(st.sizes) == 0 {
				ui.Text(c, "暂无我的尺寸——输好宽高后点下方保存").FontSize(10.5).TextColor(be.faint).Wrap()
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
		w := ui.TextInput(c.Key("edit-dim-w"), &st.dimWText).Height(30).Grow(1).Basis(0).
			MinWidth(0).TextAlign(ui.Center).Placeholder("宽")
		w.FontSize(12)
		if w.Changed() {
			a.editOnDimW(editAtoi(st.dimWText))
		}
		link := ui.Box(c).Size(22, 22).Center().Radius(5).Cursor(ui.CursorPointer).
			Tooltip(map[bool]string{true: "锁定比例", false: "自由比例"}[st.locked])
		if st.locked {
			link.Background(be.transferSoft)
		}
		link.OnClick(func() { st.locked = !st.locked })
		link.Children(func() {
			ink := be.faint
			if st.locked {
				ink = be.transfer
			}
			ui.Icon(c, iconLink).FontSize(13).TextColor(ink)
		})
		h := ui.TextInput(c.Key("edit-dim-h"), &st.dimHText).Height(30).Grow(1).Basis(0).
			MinWidth(0).TextAlign(ui.Center).Placeholder("高")
		h.FontSize(12)
		if h.Changed() {
			a.editOnDimH(editAtoi(st.dimHText))
		}
		ui.Text(c, "像素").FontSize(11).TextColor(be.faint).Shrink(0)
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
					ui.Text(c, "暂无我的比例——输好尺寸后点下方保存").FontSize(10.5).TextColor(be.faint).Wrap()
				}
			}
		})
	})
	if w, h, ok := a.editResolvedRatio(); ok {
		ui.Text(c, fmt.Sprintf("当前图片 %d×%d", w, h)).FontSize(10.5).TextColor(be.faint)
	}
	a.editSmallButton(c, be, "存为我的比例", func() { a.editSaveMyRatio() })
}

func (a *App) editRatioRow(c *ui.Context, be editPalette, ratio, label string) {
	st := &a.edit
	on := st.opts.CropRatio == ratio
	row := ui.Row(c.Key("edit-ratio-"+ratio)).FillWidth().Gap(8).Padding(7, 10).Radius(7).
		Cursor(ui.CursorPointer).AlignItems(ui.Baseline)
	if on {
		row.Background(be.transferSoft)
	}
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		if !on && row.Hovered() {
			p.Fill(r, be.quiet, 7)
		}
	})
	row.OnClick(func() { a.editPickRatio(ratio) })
	row.Children(func() {
		text := ui.Text(c, ratio).FontSize(12.5).SingleLine()
		if on {
			text.TextColor(be.transferStrong).FontWeight(500)
		} else {
			text.TextColor(be.ink)
		}
		if label != "" {
			ui.Text(c, label).FontSize(10.5).TextColor(be.faint).SingleLine()
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
	btn := ui.Button(c, label).FontSize(11.5).Padding(5, 10).Radius(7).Shrink(0).
		Background(be.quiet).TextColor(be.muted)
	hoverPressPose(btn)
	btn.OnClick(fn)
}

func (a *App) editAction(c *ui.Context, be editPalette, label string, icon *ui.SVG, primary, enabled bool) ui.Element {
	// moza 的 quiet 按钮没有底色，文字用 75% 前景色；primary 用传送色。
	ink := be.ink.Alpha(0.75)
	el := ui.Row(c).Height(28).Padding(0, 10).Gap(4).Radius(8).AlignItems(ui.Center).Shrink(0)
	if primary {
		el.Background(be.transfer).Shadow(0, 1, 2, 0, be.shadow)
		ink = be.onTransfer
	}
	if enabled {
		el.Cursor(ui.CursorPointer)
	} else {
		// moza 的按钮禁用时只降到半透明，不换颜色。
		el.Opacity(0.5)
	}
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		if !enabled || !el.Hovered() {
			return
		}
		if primary {
			p.Fill(r, ui.RGBA(0, 0, 0, 0.08), 8)
		} else {
			p.Fill(r, be.quiet, 8)
		}
	})
	el.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(14).TextColor(ink)
		}
		ui.Text(c, label).FontSize(12.8).FontWeight(500).TextColor(ink).SingleLine()
	})
	return el
}

// editSwitch 是 30×17 的小开关，和 moza 的尺寸一致。
func (a *App) editSwitch(c *ui.Context, be editPalette, on bool, fn func()) ui.Element {
	sw := ui.Box(c).Size(30, 17).Radius(9).Shrink(0).Cursor(ui.CursorPointer)
	if on {
		sw.Background(be.transfer)
	} else {
		sw.Background(be.rule)
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
		ui.Column(c.Key("edit-recipe-modal")).Width(380).Gap(12).Padding(4).
			Background(be.sheet).Radius(12).Children(func() {
			ui.Text(c, "存为配方").FontSize(16).FontWeight(500).TextColor(be.ink)
			ui.Text(c, "保存当前生效的功能，之后一键套用。").FontSize(12).TextColor(be.muted)
			ui.Text(c, "配方名称").FontSize(11.5).TextColor(be.muted)
			name := ui.TextInput(c.Key("edit-recipe-name"), &st.recipeName).Height(30).FillWidth()
			name.FontSize(13)
			if st.recipeErr != "" {
				ui.Text(c, st.recipeErr).FontSize(11.5).TextColor(be.fault)
			}
			row := ui.Row(c).FillWidth().Justify(ui.End).Gap(8)
			row.Children(func() {
				cancel := ui.Button(c, "取消").FontSize(12).Padding(6, 12).Radius(7).
					Background(be.quiet).TextColor(be.ink)
				cancel.OnClick(func() { st.recipeOpen = false })
				save := ui.Button(c, "保存").FontSize(12).Padding(6, 12).Radius(7).
					Background(be.transfer).TextColor(be.onTransfer)
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
		col := ui.Column(c.Key("edit-result-modal")).Width(440).Gap(10).Padding(4).
			Background(be.sheet).Radius(12)
		col.Children(func() {
			ui.Text(c, "处理结果").FontSize(16).FontWeight(500).TextColor(be.ink)
			summary := fmt.Sprintf("完成 %d 个", okN)
			if failN > 0 {
				summary += fmt.Sprintf(" · 失败 %d 个", failN)
			}
			if dir := strings.TrimSpace(st.outDir); dir != "" {
				summary += " → " + dir
			}
			ui.Text(c, summary).FontSize(11.5).TextColor(be.muted).SingleLine()
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
					open := ui.Button(c, "打开输出目录").FontSize(12).Padding(6, 12).Radius(7).
						Background(be.quiet).TextColor(be.ink)
					open.OnClick(func() { a.editOpenOutDir() })
				}
				if failN > 0 {
					retry := ui.Button(c, fmt.Sprintf("重试失败 %d 张", failN)).FontSize(12).Padding(6, 12).Radius(7).
						Background(be.transfer).TextColor(be.onTransfer)
					retry.OnClick(func() {
						st.resultOpen = false
						a.editRetryFailed()
					})
				}
				close := ui.Button(c, "关闭").FontSize(12).Padding(6, 12).Radius(7).
					Background(be.quiet).TextColor(be.ink)
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
