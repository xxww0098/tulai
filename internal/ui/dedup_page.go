package ui

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

// 去重力度只有三档，滑块和位数都收起来：多数人不需要知道汉明距离是什么。
const (
	dedupStrict = iota
	dedupStandard
	dedupLoose
)

// dedupLevelDistance 是每一档对应的指纹差异位数上限。
var dedupLevelDistance = [3]int{4, 8, 16}

// dedupDefaultLevel 是默认档位。
const dedupDefaultLevel = dedupStandard

// 去重页的设置键。和生图、洗图一样存在 settings 表里，重启还在。
const (
	dedupKeyLevel     = "dedup_level"
	dedupKeyOutDir    = "dedup_out_dir"
	dedupKeyFlatColor = "dedup_flat_color"
)

// dedupState 是去重页的全部状态。这一页不跟生图、洗图共用图片和提示词。
type dedupState struct {
	items  []string // 待去重的图片路径，按加入顺序
	level  int      // 去重力度：严格 / 标准 / 宽松
	outDir string   // 空表示用「输出目录/去重/日期」
	more   bool     // 「更多设置」折叠着还是展开
	// flatColorOK 关掉平坦图配色护栏：纯白底和纯黑底也算重复。
	flatColorOK bool
	busy        bool
	scanned     int
	total       int
	report      dedup.Report
	note        string
	err         string
}

// dedupThreshold 当前档位对应的汉明距离上限。
func (a *App) dedupThreshold() int {
	level := clampInt(a.dedup.level, 0, len(dedupLevelDistance)-1)
	return dedupLevelDistance[level]
}

// dedupOptions 把界面上的选择收成一次去重的口径。
func (a *App) dedupOptions() dedup.Options {
	return dedup.Options{MaxDistance: a.dedupThreshold(), IgnoreFlatColor: a.dedup.flatColorOK}
}

// dedupLevelHint 是一句话解释这一档在做什么。
func dedupLevelHint(level int) string {
	switch clampInt(level, 0, len(dedupLevelDistance)-1) {
	case dedupStrict:
		return "只去几乎一样的图：同一张图重存、缩放、换了编码。"
	case dedupLoose:
		return "更松：轻微裁剪、换了角标的也算同一张，同款不同图也可能并到一组。"
	default:
		return "默认。同一张图重存、缩放、轻微裁剪都算重复。"
	}
}

// loadDedupSettings 读上一次的选择。没存过就留在 App 初始化时的默认值。
func (a *App) loadDedupSettings(s *store.Store) {
	if s == nil {
		return
	}
	if s.Get(dedupKeyLevel) != "" {
		a.dedup.level = clampInt(s.GetInt(dedupKeyLevel), 0, len(dedupLevelDistance)-1)
	}
	a.dedup.outDir = strings.TrimSpace(s.Get(dedupKeyOutDir))
	a.dedup.flatColorOK = s.GetBool(dedupKeyFlatColor)
}

// saveDedupSettings 把选择写回配置库。
func (a *App) saveDedupSettings(s *store.Store) {
	if s == nil {
		return
	}
	s.SetInt(dedupKeyLevel, clampInt(a.dedup.level, 0, len(dedupLevelDistance)-1))
	s.Set(dedupKeyOutDir, strings.TrimSpace(a.dedup.outDir))
	s.SetInt(dedupKeyFlatColor, boolInt(a.dedup.flatColorOK))
}

// dedupOutDir 是这次去重写到哪儿。没手选就落在设置的输出目录下。
func (a *App) dedupOutDir() string {
	if dir := strings.TrimSpace(a.dedup.outDir); dir != "" {
		return dir
	}
	return filepath.Join(a.resolvedOutputDir(), "去重", time.Now().Format("2006-01-02"))
}

// dedupPage 是去重页：拖图、选力度、开始、看结果。
// 整页都收拖动：拖到空白处和拖到收图卡上效果一样。
func (a *App) dedupPage(c *ui.Context) {
	page := ui.Scroll(c.Key("dedup-page")).Grow(1).MinHeight(0)
	if paths := page.DroppedFiles(); len(paths) > 0 {
		a.dedupIngest(paths)
	}
	page.Children(func() {
		ui.Column(c).FillWidth().Gap(10).Padding(0, 0, 10, 0).Children(func() {
			a.dedupPickCard(c)
			a.dedupGoCard(c)
			a.dedupResultCard(c)
		})
	})
}

// dedupPickCard 收图片：拖进来、点着选、或选整个文件夹。
func (a *App) dedupPickCard(c *ui.Context) {
	tok := a.look
	card := a.stepCard(c, "dedup-pick", stepCurrent)
	if paths := card.DroppedFiles(); len(paths) > 0 {
		a.dedupIngest(paths)
	}
	busy := a.dedup.busy
	if card.FileDragOver() && !busy {
		card.Border(2, tok.accent).Background(tok.accent.Alpha(0.08))
	}
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "图片").FontSize(tok.title).Bold().Shrink(0)
			if n := len(a.dedup.items); n > 0 {
				ui.Text(c, fmt.Sprintf("已选 %d 张", n)).FontSize(tok.caption).TextColor(tok.muted).
					Shrink(0).FontFeatures("tnum")
			}
			ui.Row(c).Grow(1).MinWidth(0)
			a.dedupQuiet(c, "添加图片", busy, func() { a.dedupAdd() })
			a.dedupQuiet(c, "选文件夹", busy, func() { a.dedupAddFolder() })
			if len(a.dedup.items) > 0 {
				a.dedupQuiet(c, "清空", busy, func() { a.dedupClear() })
			}
		})
		if len(a.dedup.items) == 0 {
			a.dedupDropZone(c, busy)
			return
		}
		a.dedupGrid(c)
	})
}

func (a *App) dedupQuiet(c *ui.Context, label string, busy bool, click func()) {
	b := a.quietButton(c, label).OnClick(click)
	if busy {
		b.Disabled(true)
	}
}

// dedupDropZone 是空列表时的虚线框，点一下和拖进来效果一样。
func (a *App) dedupDropZone(c *ui.Context, busy bool) {
	tok := a.look
	zone := ui.Column(c.Key("dedup-drop")).FillWidth().Height(120).Radius(tok.radiusM).
		Border(1.5, tok.accent.Alpha(0.45)).BorderStyle(ui.BorderDashed).
		Background(tok.sunken).Center().Gap(4)
	if !busy {
		zone.Cursor(ui.CursorPointer)
		if zone.Hovered() {
			zone.Border(1.5, tok.accent).Background(tok.accent.Alpha(0.08))
		}
		hoverPressPose(zone)
		if zone.Clicked() {
			a.dedupAdd()
		}
	}
	zone.Children(func() {
		ui.Text(c, "把图片拖到这里").FontSize(tok.body).Bold()
		ui.Text(c, "也可以点一下选图，或选整个文件夹").FontSize(tok.caption).TextColor(tok.muted)
	})
}

// dedupGrid 显示已经收进来的图。太多时只画前几张，剩下的用一张计数格带过。
// 用自动换行的行，宽度交给布局算：面板里再套一层网格会因为宽度未定把卡片撑到满屏。
func (a *App) dedupGrid(c *ui.Context) {
	tok := a.look
	const shown = 40
	ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
		for i := range a.dedup.items {
			if i >= shown {
				break
			}
			path := a.dedup.items[i]
			tile := ui.Box(c.Key("dedup-thumb-"+path)).Size(72, 72).
				Radius(tok.radiusS).Background(tok.sunken).Clip().
				Label(filepath.Base(path)).Tooltip(path)
			tile.Children(func() {
				if bmp := a.cachedThumb(path); bmp != nil {
					ui.Image(c, bmp).Fill().Fit(ui.Cover).Radius(tok.radiusS)
				}
			})
		}
		if extra := len(a.dedup.items) - shown; extra > 0 {
			ui.Box(c.Key("dedup-thumb-more")).Size(72, 72).Radius(tok.radiusS).
				Background(tok.sunken).Center().Children(func() {
				ui.Text(c, fmt.Sprintf("+%d", extra)).FontSize(tok.body).TextColor(tok.muted)
			})
		}
	})
}

// dedupGoCard 是力度、输出和开始。出力度的说明跟按钮在同一张卡上，看一眼就知道会怎么判。
func (a *App) dedupGoCard(c *ui.Context) {
	tok := a.look
	th := c.Theme()
	busy := a.dedup.busy
	card := a.stepCard(c, "dedup-go-card", stepDone)
	card.Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "去重力度").FontSize(tok.body).Shrink(0)
			ui.Tabs(c, &a.dedup.level, "严格", "标准", "宽松").Disabled(busy).Shrink(0)
			ui.Row(c).Grow(1).MinWidth(0)
			a.dedupGoButton(c, th, busy)
		})
		ui.Text(c, dedupLevelHint(a.dedup.level)).FontSize(tok.caption).TextColor(tok.muted)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			paintFolder(c, tok.muted)
			ui.Text(c, "存到").FontSize(tok.caption).TextColor(tok.muted).Shrink(0)
			out := a.dedupOutDir()
			ui.Text(c, out).FontSize(tok.caption).TextColor(tok.accent).Grow(1).MinWidth(0).
				SingleLine().Tooltip(out)
			a.dedupQuiet(c, "改", busy, func() { a.dedupPickOut() })
		})
		ui.Collapsible(c, "更多设置", &a.dedup.more, func() {
			ui.Checkbox(c, &a.dedup.flatColorOK, "纯色底也算重复").Disabled(busy)
			flatHint := "纯白底和纯黑底的主图默认不互判重复。"
			if a.dedup.flatColorOK {
				flatHint = "已关掉护栏：纯色底图之间也会判重复，误判会变多。"
			}
			ui.Text(c, flatHint).FontSize(tok.caption).TextColor(tok.muted)
		})
		if busy {
			a.dedupBar(c)
		}
		if lead := a.dedupLead(); lead != "" {
			ui.Text(c, lead).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(lead)
		}
	})
}

// dedupGoButton 是这一页唯一的主动作。
func (a *App) dedupGoButton(c *ui.Context, th *ui.Theme, busy bool) {
	tok := a.look
	label := "开始去重"
	if busy {
		label = "去重中"
	}
	disabled := busy || len(a.dedup.items) == 0
	b := ui.Button(c.Key("dedup-go"), label).Padding(8, 16).Radius(th.Radius).
		Shrink(0).AlignSelf(ui.Center).FontSize(tok.body)
	bg, fg := th.Accent, th.AccentText
	switch {
	case disabled:
		bg, fg = th.Surface, th.TextMuted
	case b.Pressed():
		bg = th.AccentPressed
	case b.Hovered():
		bg = th.AccentHover
	}
	b.Background(bg).TextColor(fg).Disabled(disabled)
	if !disabled {
		hoverPressPose(b)
		b.Cursor(ui.CursorPointer).OnClick(func() { a.dedupStart() })
	}
}

// dedupLead 是开始按钮下面那一行小字。跑完的汇总放在结果卡里，这里只说进度和提示。
func (a *App) dedupLead() string {
	switch {
	case a.dedup.busy:
		return fmt.Sprintf("正在处理 %d / %d", a.dedup.scanned, a.dedup.total)
	case a.dedup.err != "":
		return a.dedup.err
	case len(a.dedup.report.Failed) > 0:
		return fmt.Sprintf("%d 张解不开，已跳过", len(a.dedup.report.Failed))
	}
	return a.dedup.note
}

// dedupBar 是确定进度条：算指纹时按张数走。
func (a *App) dedupBar(c *ui.Context) {
	tok := a.look
	frac := float32(0)
	if a.dedup.total > 0 {
		frac = float32(a.dedup.scanned) / float32(a.dedup.total)
	}
	if frac > 1 {
		frac = 1
	}
	if frac < 0 {
		frac = 0
	}
	ui.Box(c.Key("dedup-bar")).FillWidth().Height(6).Radius(3).Background(tok.sunken).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if frac <= 0 {
				return
			}
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * frac, H: r.H}, tok.accent, 3)
		})
}

// dedupResultCard 跑完才出现：一句汇总、一个打开按钮、前几组明细。
func (a *App) dedupResultCard(c *ui.Context) {
	rep := a.dedup.report
	if rep.Scanned == 0 {
		return
	}
	tok := a.look
	card := a.stepCard(c, "dedup-result", stepDone)
	card.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "结果").FontSize(tok.title).Bold().Shrink(0)
			ui.Text(c, fmt.Sprintf("%d 组重复，%d 张没复制", len(rep.Groups), rep.Duplicates)).
				FontSize(tok.caption).TextColor(tok.muted).Grow(1).MinWidth(0).SingleLine()
			a.quietButton(c, "打开文件夹").OnClick(func() { a.openResultFolder(rep.OutDir) })
		})
		if len(rep.Groups) == 0 {
			ui.Text(c, "没有重复的图片，全部复制过去了。").FontSize(tok.body).TextColor(tok.muted)
			return
		}
		const maxGroups = 10
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for i, g := range rep.Groups {
				if i >= maxGroups {
					break
				}
				a.dedupGroupRow(c, i, g)
			}
		})
		if extra := len(rep.Groups) - maxGroups; extra > 0 {
			ui.Text(c, fmt.Sprintf("还有 %d 组，见输出目录里的 %s", extra, dedup.ReportName)).
				FontSize(tok.caption).TextColor(tok.muted).SingleLine()
		}
	})
}

func (a *App) dedupGroupRow(c *ui.Context, index int, g dedup.Group) {
	tok := a.look
	sigs := a.dedup.report.Signatures
	if g.Keep < 0 || g.Keep >= len(sigs) {
		return
	}
	row := ui.Row(c.Key(fmt.Sprintf("dedup-group-%d", index))).FillWidth().Gap(8).
		AlignItems(ui.Center).Padding(6).Radius(tok.radiusS).Background(tok.paper)
	row.Children(func() {
		ui.Text(c, fmt.Sprintf("#%d", index+1)).FontSize(tok.caption).TextColor(tok.muted).
			Shrink(0).FontFeatures("tnum")
		a.dedupThumbBox(c, sigs[g.Keep].Path, 40)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Clip().Children(func() {
			keep := "保留 " + filepath.Base(sigs[g.Keep].Path)
			ui.Text(c, keep).FontSize(tok.caption).MinWidth(0).SingleLine().Tooltip(sigs[g.Keep].Path)
			var dups []string
			for _, d := range g.Dups {
				if d >= 0 && d < len(sigs) {
					dups = append(dups, filepath.Base(sigs[d].Path))
				}
			}
			line := "重复 " + strings.Join(dups, "、")
			ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).MinWidth(0).SingleLine().Tooltip(line)
		})
	})
}

func (a *App) dedupThumbBox(c *ui.Context, path string, size float32) {
	tok := a.look
	box := ui.Box(c.Key("dedup-mini-"+path)).Size(size, size).Radius(6).Shrink(0).
		Background(tok.sunken).Clip()
	if bmp := a.cachedThumb(path); bmp != nil {
		box.Children(func() {
			ui.Image(c, bmp).Fill().Fit(ui.Cover).Radius(6)
		})
	}
}

// dedupClear 清空这一批，连同上一轮的结果。
func (a *App) dedupClear() {
	if a.dedup.busy {
		return
	}
	a.dedup.items = nil
	a.dedup.report = dedup.Report{}
	a.dedup.scanned, a.dedup.total = 0, 0
	a.dedup.note, a.dedup.err = "", ""
}

// openableImage 只读文件头判一下是不是能解开的图。
// 收图时不整份读进来：一次拖几百张会被磁盘拖住，真正的解码留到开始去重时的后台任务。
func openableImage(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	_, _, err = image.DecodeConfig(f)
	return err == nil
}

// humanSize 把字节数写成界面上看得懂的档位。
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB"} {
		value /= unit
		if value < unit || suffix == "GB" {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%d B", n)
}

// dedupIngest 把拖进来或选进来的路径收成待去重列表。文件夹会展开成里面的图片。
func (a *App) dedupIngest(paths []string) {
	if a.dedup.busy || len(paths) == 0 {
		return
	}
	have := make(map[string]bool, len(a.dedup.items))
	for _, path := range a.dedup.items {
		have[path] = true
	}
	added := 0
	for _, path := range expandImagePaths(paths) {
		if have[path] || !openableImage(path) {
			continue
		}
		a.dedup.items = append(a.dedup.items, path)
		have[path] = true
		added++
	}
	if added > 0 {
		a.dedup.note = fmt.Sprintf("新加 %d 张", added)
		a.dedup.err = ""
		// 上一轮的报告对应的是上一批图，加了新图就不再成立。
		a.dedup.report = dedup.Report{}
		a.dedup.scanned, a.dedup.total = 0, 0
		return
	}
	a.dedup.note = "没有新的图片"
}

// dedupAdd 文件对话框选图。
func (a *App) dedupAdd() {
	if a.dedup.busy {
		return
	}
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   window,
			Title:    "选择要去重的图片",
			Multiple: true,
			Filters:  []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "webp", "gif"}}},
		})
		if err != nil {
			logging.Log.Errorf("选择图片失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		apply := func() { a.dedupIngest(paths) }
		if window != nil {
			window.Update(apply)
			return
		}
		apply()
	}()
}

// dedupAddFolder 选文件夹，把里面的图片加进来（不进子文件夹）。
func (a *App) dedupAddFolder() {
	if a.dedup.busy {
		return
	}
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: window, Title: "选择图片文件夹", Directory: true, Multiple: true,
		})
		if err != nil {
			logging.Log.Errorf("选择文件夹失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		apply := func() { a.dedupIngest(paths) }
		if window != nil {
			window.Update(apply)
			return
		}
		apply()
	}()
}

// dedupPickOut 手选输出文件夹。
func (a *App) dedupPickOut() {
	if a.dedup.busy {
		return
	}
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: window, Title: "选择去重结果文件夹", Directory: true,
		})
		if err != nil {
			logging.Log.Errorf("选择输出文件夹失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		apply := func() { a.dedup.outDir = paths[0] }
		if window != nil {
			window.Update(apply)
			return
		}
		apply()
	}()
}

// dedupStart 起一个后台任务：算指纹、分组、复制留下来的图、写报告。
// 这一页不占引擎，生图和洗图可以同时跑。
func (a *App) dedupStart() {
	if a.dedup.busy || len(a.dedup.items) == 0 {
		return
	}
	paths := append([]string(nil), a.dedup.items...)
	out := a.dedupOutDir()
	opts := a.dedupOptions()
	a.dedup.busy = true
	a.dedup.err = ""
	a.dedup.note = ""
	a.dedup.report = dedup.Report{}
	a.dedup.scanned = 0
	a.dedup.total = len(paths)
	logging.Log.Info("开始去重：%d 张，阈值 %d，纯色底也算重复=%v，输出 %s",
		len(paths), opts.MaxDistance, opts.IgnoreFlatColor, out)
	// 没有窗口时（测试）当场跑完，不另起 goroutine：这一页的状态只在界面线程上改。
	window := a.window
	if window == nil {
		rep, err := dedup.Run(paths, out, opts, nil)
		a.finishDedup(rep, err)
		return
	}
	go func() {
		rep, err := dedup.Run(paths, out, opts, func(done, total int) {
			window.Update(func() {
				a.dedup.scanned = done
				a.dedup.total = total
			})
		})
		window.Update(func() { a.finishDedup(rep, err) })
	}()
}

// finishDedup 收尾。只在界面线程上调。
func (a *App) finishDedup(rep dedup.Report, err error) {
	a.dedup.busy = false
	a.dedup.report = rep
	a.dedup.scanned = rep.Scanned
	a.dedup.total = rep.Scanned
	if err != nil {
		a.dedup.err = "去重失败：" + err.Error()
		logging.Log.Errorf("去重失败: %v", err)
		return
	}
	logging.Log.Info("去重完成：%d 组，重复 %d 张，留下 %d 张", len(rep.Groups), rep.Duplicates, rep.Unique)
}
