package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/tasks"
)

const (
	resultTileMin      float32 = 64 / interfaceZoom
	resultTileMax      float32 = 76 / interfaceZoom
	resultTileGap      float32 = 8
	resultMaxCols              = 10
	resultDoneMaxCols          = 8
	resultPreviewDelay         = 200 * time.Millisecond
	resultPreviewMax   float32 = 320 / interfaceZoom
)

// resultTile 是结果区或「已完成」浮层中的一张图，或尚未产图时的一条状态。
type resultTile struct {
	key  string
	task tasks.Task
	path string
}

// resultRow 是虚拟列表的一行：分组标题、缩略图网格行，或单条「待处理」故障行。
type resultRow struct {
	key     string
	header  bool
	issue   bool
	title   string
	summary string
	tiles   []resultTile
}

type resultAccountBucket struct {
	key       string
	title     string
	running   int
	awaitUser int
	tiles     []resultTile
}

type resultSplit struct {
	activeRows []resultRow
	issueRows  []resultRow
	doneTiles  []resultTile
	doneTasks  int
	tileSize   float32
}

func (a *App) resultList() *ui.ListState {
	if a.fissioning() {
		return &a.fissionList
	}
	if a.washing() {
		return &a.washList
	}
	return &a.genList
}

func (a *App) doneResultList() *ui.ListState {
	if a.fissioning() {
		return &a.fissionDoneList
	}
	if a.washing() {
		return &a.washDoneList
	}
	return &a.genDoneList
}

// resultsPane 以「正在运行」和「待处理」为主视觉；已完成的图不常驻主格，收进顶部「已完成」浮层。
func (a *App) resultsPane(c *ui.Context) {
	tok := a.look
	w, _ := c.Size()
	items := tasksForPlatform(a.queueItems(), a.stagePlatform())
	tally := TallyTasks(items)
	split := a.buildResultSplit(w, items)
	a.resultsClock(c, items)

	th := *c.Theme()
	th.TooltipDelay = resultPreviewDelay

	pane := uiCardSurface(ui.Themed(c.Key("results"), &th).Label("出图结果区").FillWidth().Padding(8, 10).Gap(6), tok)
	if homeSplit(w) {
		pane.Grow(1).MinHeight(0)
	}
	pane.Children(func() {
		var doneAnchor ui.Element
		var hasDoneAnchor bool
		a.resultsHead(c, items, tally, split, &doneAnchor, &hasDoneAnchor)

		if hasDoneAnchor && len(split.doneTiles) > 0 {
			a.resultsDonePopover(c, doneAnchor, split)
		}

		if tally.Running > 0 || tally.Done > 0 || tally.Failed > 0 || tally.Needs > 0 || tally.Review > 0 {
			a.resultsProgressBar(c, tally)
		}

		if tally.Needs > 0 {
			warnFg, _, _ := semanticToneColors(tok.dark, tok.text, tok.warning, 0.12)
			ui.Text(c, verificationHint).FontSize(tok.caption).TextColor(warnFg)
		}

		ui.Divider(c).Background(tok.border)

		rows := make([]resultRow, 0, len(split.activeRows)+len(split.issueRows))
		rows = append(rows, split.activeRows...)
		rows = append(rows, split.issueRows...)

		if len(rows) == 0 {
			if split.doneTasks > 0 || len(split.doneTiles) > 0 {
				a.resultsDoneSummary(c, split)
			} else {
				a.resultsEmpty(c)
			}
			return
		}

		st := a.resultList()
		st.Key = func(i int) any {
			if i < 0 || i >= len(rows) {
				return i
			}
			return rows[i].key
		}
		st.Header = func(i int) bool {
			return i >= 0 && i < len(rows) && rows[i].header
		}
		list := ui.List(c.Key("result-list"), st, len(rows), func(i int) {
			if i < 0 || i >= len(rows) {
				return
			}
			row := rows[i]
			switch {
			case row.header:
				a.resultHeader(c, row)
			case row.issue:
				a.resultIssueRow(c, row)
			default:
				a.resultStrip(c, row, split.tileSize)
			}
		}).FillWidth()
		if homeSplit(w) {
			list.Grow(1).MinHeight(0)
		} else {
			list.Height(320)
		}
	})
}

func (a *App) resultsClock(c *ui.Context, items []tasks.Task) {
	now := c.Now()
	var wait time.Duration
	for _, task := range items {
		if !task.Worked() || !isTaskActiveStatus(task.Status) || task.StartedAt.IsZero() {
			continue
		}
		elapsed := now.Sub(task.StartedAt)
		if elapsed < 0 {
			elapsed = 0
		}
		left := time.Second - elapsed%time.Second
		if left <= 0 {
			left = time.Millisecond
		}
		if wait == 0 || left < wait {
			wait = left
		}
	}
	if wait > 0 {
		c.After(wait)
	}
}

func (a *App) resultsHead(c *ui.Context, items []tasks.Task, tally TaskTally, split resultSplit, doneAnchor *ui.Element, hasDoneAnchor *bool) {
	tok := a.look
	platform := a.stagePlatform()
	failed := FailedTasks(items)

	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				if tally.Running > 0 {
					waitingDot(c)
				}
				ui.Text(c, "出图进度").FontSize(tok.title).Bold().TextColor(tok.text).Shrink(0)
				platformIcon(c, platform, 12, tok)
				ui.Text(c, "当前服务商："+platform).FontSize(tok.caption).TextColor(tok.muted).MinWidth(0).SingleLine()
			})
			if len(items) > 0 {
				line := FormatProgressTally(
					tally.Done, tally.Running, tally.Queued,
					tally.Failed, tally.Needs, tally.Review, tally.Stopped,
					a.runningOn(platform), a.liveParallel(),
				)
				ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).FontFeatures("tnum").SingleLine()
			}
		})

		ui.Row(c.Key("head-actions")).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
			if len(failed) > 0 {
				uiIconTextBtn(c, "retry-failed", "全部重试失败项", "全部重试失败项", iconRotate, 24, false, tok).
					Disabled(a.running).OnClick(func() { a.retryTasks(failed) })
			}
			if split.doneTasks > 0 || len(split.doneTiles) > 0 {
				count := split.doneTasks
				if count == 0 {
					count = len(split.doneTiles)
				}
				label := fmt.Sprintf("已完成 %d", count)
				btn := uiIconTextBtn(c, "done-toggle", label, "查看已完成结果", iconCheck, 24, a.resultsDoneOpen, tok).
					OnClick(func() {
						a.resultsDoneOpen = !a.resultsDoneOpen
					})
				*doneAnchor = btn
				*hasDoneAnchor = true
			}
			if len(items) > 0 || split.doneTasks > 0 || len(split.doneTiles) > 0 {
				uiIconTextBtn(c, "open-out", "打开输出目录", "打开输出目录", iconFolderOpen, 24, false, tok).
					OnClick(func() { a.openOutputFolder() })
			}
		})
	})
}

func (a *App) resultsProgressBar(c *ui.Context, tally TaskTally) {
	tok := a.look
	total := tally.Done + tally.Running + tally.Queued + tally.Failed + tally.Needs + tally.Review + tally.Stopped
	if total <= 0 {
		return
	}
	finished := tally.Done + tally.Failed + tally.Stopped
	fraction := float64(finished) / float64(total)
	if fraction == 0 && tally.Running > 0 {
		fraction = 0.04
	}
	fill := tok.accent
	if tally.Done > 0 && tally.Running == 0 && tally.Queued == 0 && tally.Failed == 0 && tally.Needs == 0 {
		fill = tok.success
	}
	quotaBar(c.Key("progress"), "bar", fraction, fill, tok.sunkenHi)
}

func (a *App) resultsEmpty(c *ui.Context) {
	tok := a.look
	text := "出图结果会出现在这里"
	if a.washing() {
		text = "洗图结果会出现在这里"
	} else if a.fissioning() {
		text = "4 张裂变结果会出现在这里"
	}
	ui.Row(c.Key("stage-empty")).Padding(8, 10).Radius(tok.radiusS).Background(tok.sunken).
		Border(1, tok.border).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconImages).FontSize(15).TextColor(tok.muted)
		ui.Text(c, text).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
	})
}

func (a *App) resultsDoneSummary(c *ui.Context, split resultSplit) {
	tok := a.look
	count := split.doneTasks
	if count == 0 {
		count = len(split.doneTiles)
	}
	sub := "结果已收纳到右上角「已完成」，按需查看或悬停预览大图"
	if len(split.doneTiles) > count {
		sub = fmt.Sprintf("共输出 %d 张结果图，已收纳到右上角「已完成」", len(split.doneTiles))
	}
	badgeFg, badgeBg, badgeBorder := semanticToneColors(tok.dark, tok.text, tok.success, 0.14)
	ui.Column(c.Key("done-summary")).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		ui.Column(c.Key("done-card")).
			Background(tok.sunken).
			Border(1, tok.border).
			Radius(tok.radiusL).
			Padding(16, 20).
			Gap(8).
			AlignItems(ui.Center).
			Children(func() {
				ui.Box(c.Key("badge")).
					Size(32, 32).Radius(16).
					Background(badgeBg).
					Border(1, badgeBorder).
					Justify(ui.Center).AlignItems(ui.Center).
					Children(func() {
						ui.Icon(c, iconCheck).FontSize(15).TextColor(badgeFg)
					})
				ui.Text(c, fmt.Sprintf("本轮已完成 %d 项", count)).FontSize(tok.title).Bold().
					TextColor(tok.text).FontFeatures("tnum")
				ui.Text(c, sub).FontSize(tok.caption).TextColor(tok.muted).FontFeatures("tnum")
				ui.Row(c.Key("cta")).Padding(4, 0, 0, 0).Gap(8).Children(func() {
					uiIconTextBtn(c, "view-done", "查看已完成", "查看已完成结果", iconCheck, 28, true, tok).
						OnClick(func() { a.resultsDoneOpen = true })
					uiIconTextBtn(c, "open-folder", "打开输出目录", "打开输出目录", iconFolderOpen, 28, false, tok).
						OnClick(func() { a.openOutputFolder() })
				})
			})
	})
}

func (a *App) buildResultRows(windowW float32) ([]resultRow, float32) {
	return a.buildResultRowsAt(windowW, time.Now())
}

func (a *App) buildResultRowsAt(windowW float32, _ time.Time) ([]resultRow, float32) {
	items := tasksForPlatform(a.queueItems(), a.stagePlatform())
	split := a.buildResultSplit(windowW, items)
	rows := make([]resultRow, 0, len(split.activeRows)+len(split.issueRows))
	rows = append(rows, split.activeRows...)
	rows = append(rows, split.issueRows...)
	return rows, split.tileSize
}

func (a *App) buildResultSplit(windowW float32, items []tasks.Task) resultSplit {
	cols, tileW := resultLayoutForStage(a.homeStageW(windowW))
	activeRows, issueRows, doneTiles, doneTasks := a.classifyResultRows(items, cols)
	return resultSplit{
		activeRows: activeRows,
		issueRows:  issueRows,
		doneTiles:  doneTiles,
		doneTasks:  doneTasks,
		tileSize:   tileW,
	}
}

func (a *App) classifyResultRows(items []tasks.Task, cols int) (activeRows, issueRows []resultRow, doneTiles []resultTile, doneTasks int) {
	if cols < 1 {
		cols = 1
	}

	browsers := a.configuredBrowsers()
	byID := make(map[int64]string, len(browsers))
	for _, b := range browsers {
		title := accountLabel(b)
		if b.Platform == agy.Platform && strings.TrimSpace(b.UserDataDir) != "" {
			if email := strings.TrimSpace(agy.Email(b.UserDataDir)); email != "" {
				title = email
			}
		}
		byID[b.ID] = title
	}

	var order []string
	buckets := map[string]*resultAccountBucket{}
	var issueTiles []resultTile

	for pos, task := range items {
		switch {
		case isTaskActiveStatus(task.Status):
			key, title := a.resultAccountGroup(task, byID)
			b := buckets[key]
			if b == nil {
				b = &resultAccountBucket{key: key, title: title}
				buckets[key] = b
				order = append(order, key)
			}
			b.running++
			if task.Status == tasks.StatusAwaitUser {
				b.awaitUser++
			}
			b.tiles = append(b.tiles, taskActiveTiles(task, pos)...)

		case task.Status == tasks.StatusSuccess:
			doneTasks++
			doneTiles = append(doneTiles, taskDoneTiles(task, pos)...)

		case task.Status == tasks.StatusFailed || task.Status == tasks.StatusNeedsUser || task.Status == tasks.StatusNeedsReview || (task.Status == tasks.StatusSkipped && task.Worked()):
			issueTiles = append(issueTiles, taskIssueTile(task, pos))
		}
	}

	if len(buckets) > 0 {
		ordered := make([]string, 0, len(buckets))
		added := make(map[string]bool, len(buckets))
		for _, b := range browsers {
			key := fmt.Sprintf("acct:%d", b.ID)
			if buckets[key] != nil && !added[key] {
				ordered = append(ordered, key)
				added[key] = true
			}
		}
		for _, key := range order {
			if !added[key] {
				ordered = append(ordered, key)
				added[key] = true
			}
		}
		for _, key := range ordered {
			b := buckets[key]
			summary := fmt.Sprintf("进行中 %d", b.running)
			if b.awaitUser > 0 {
				summary += fmt.Sprintf(" · 需要人工验证 %d", b.awaitUser)
			}
			activeRows = append(activeRows, resultRow{
				key:     "h:" + key,
				header:  true,
				title:   b.title,
				summary: summary,
			})
			for i := 0; i < len(b.tiles); i += cols {
				end := min(i+cols, len(b.tiles))
				activeRows = append(activeRows, resultRow{
					key:   fmt.Sprintf("r:%s:%d", key, i),
					tiles: b.tiles[i:end],
				})
			}
		}
	}

	if len(issueTiles) > 0 {
		issueRows = append(issueRows, resultRow{
			key:     "h:issues",
			header:  true,
			title:   "待处理",
			summary: fmt.Sprintf("%d 项", len(issueTiles)),
		})
		for _, tile := range issueTiles {
			issueRows = append(issueRows, resultRow{
				key:   "i:" + tile.key,
				issue: true,
				tiles: []resultTile{tile},
			})
		}
	}

	return activeRows, issueRows, doneTiles, doneTasks
}

func (a *App) resultAccountGroup(task tasks.Task, byID map[int64]string) (key, title string) {
	if task.BrowserID > 0 {
		title = strings.TrimSpace(byID[task.BrowserID])
		if title == "" {
			title = strings.TrimSpace(task.AccountName)
		}
		if title == "" {
			if plat := strings.TrimSpace(task.Platform); plat != "" {
				title = fmt.Sprintf("%s账号%d", plat, task.BrowserID)
			} else {
				title = fmt.Sprintf("账号%d", task.BrowserID)
			}
		}
		return fmt.Sprintf("acct:%d", task.BrowserID), title
	}
	if name := strings.TrimSpace(task.AccountName); name != "" {
		return "acct-name:" + name, name
	}
	plat := strings.TrimSpace(task.Platform)
	if plat == "" && a != nil {
		plat = strings.TrimSpace(a.stagePlatform())
	}
	if plat == "" {
		return "plat:", "工作账号"
	}
	return "plat:" + plat, plat + "账号"
}

func taskActiveTiles(task tasks.Task, pos int) []resultTile {
	path := ""
	if len(task.Results) > 0 {
		path = task.Results[0]
	}
	return []resultTile{{
		key:  fmt.Sprintf("a-%d-%d-%s", task.Index, pos, task.Status),
		task: task,
		path: path,
	}}
}

func taskIssueTile(task tasks.Task, pos int) resultTile {
	path := ""
	if len(task.Results) > 0 {
		path = task.Results[0]
	}
	return resultTile{
		key:  fmt.Sprintf("e-%d-%d-%s", task.Index, pos, task.Status),
		task: task,
		path: path,
	}
}

func taskDoneTiles(task tasks.Task, pos int) []resultTile {
	if len(task.Results) == 0 {
		return []resultTile{{
			key:  fmt.Sprintf("d-%d-%d", task.Index, pos),
			task: task,
		}}
	}
	tiles := make([]resultTile, 0, len(task.Results))
	for i, path := range task.Results {
		tiles = append(tiles, resultTile{
			key:  fmt.Sprintf("p-%d-%d-%d", task.Index, pos, i),
			task: task,
			path: path,
		})
	}
	return tiles
}

func resultGridLayout(inner float32, maxCols int) (cols int, tile float32) {
	if maxCols < 1 {
		maxCols = 1
	}
	if inner < resultTileMin {
		inner = resultTileMin
	}
	cols = int((inner + resultTileGap) / (resultTileMin + resultTileGap))
	if cols < 1 {
		cols = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	tile = (inner - resultTileGap*float32(cols-1)) / float32(cols)
	if tile > resultTileMax {
		tile = resultTileMax
	}
	return cols, tile
}

// resultLayout 按默认窗口宽度排紧凑网格列。单张不拉成整栏宽。
func resultLayout(windowW float32) (cols int, tile float32) {
	return resultLayoutForStage(homeStageWDefault(windowW))
}

func resultLayoutForStage(stageW float32) (cols int, tile float32) {
	inner := max(resultTileMin, stageW-24)
	return resultGridLayout(inner, resultMaxCols)
}

func (a *App) resultHeader(c *ui.Context, row resultRow) {
	tok := a.look
	title := row.title
	if title == "" {
		title = "未命名"
	}
	accLabel := "账号分组" + title
	if row.key == "h:issues" {
		accLabel = "分组待处理"
	}
	ui.Row(c.Key(row.key)).Label(accLabel).FillWidth().Padding(4, 8).Gap(8).
		Radius(tok.radiusS).Background(tok.sunken).Border(1, tok.border).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(tok.body).FontWeight(600).TextColor(tok.text).SingleLine().MinWidth(0).Grow(1)
		if row.summary != "" {
			uiBadge(c, "hdr-sum", row.summary, "neutral", tok)
		}
	})
}

func (a *App) resultStrip(c *ui.Context, row resultRow, tileW float32) {
	ui.Row(c.Key(row.key)).Padding(0, 0, 8, 0).Gap(resultTileGap).AlignItems(ui.Start).Children(func() {
		for _, tile := range row.tiles {
			a.resultRunningTile(c, tile, tileW)
		}
	})
}

func (a *App) resultRunningTile(c *ui.Context, tile resultTile, tileW float32) {
	tok := a.look
	task := tile.task
	title := tileTitle(tile)
	reduce := c.Preferences().ReduceMotion
	borderCol := tok.border
	barCol := tok.accent
	if task.Status == tasks.StatusAwaitUser {
		borderCol = tok.warning
		barCol = tok.warning
	}

	ui.Column(c.Key(tile.key)).Width(tileW).Shrink(0).Gap(4).Label("结果卡" + title).Children(func() {
		shot := ui.Box(c.Key("shot")).
			Size(tileW, tileW).
			Radius(tok.radiusS).
			Background(tok.sunkenHi).
			Border(1, borderCol).
			Clip().
			Shrink(0).
			Justify(ui.Center).
			AlignItems(ui.Center).
			Label("结果图").
			DrawOver(func(p *ui.Painter, r ui.Rect) {
				if r.W <= 2 || r.H < 4 {
					return
				}
				bar := ui.Rect{X: r.X + 1, Y: r.Y + r.H - 4, W: r.W - 2, H: 3}
				paintGenBar(p, bar, barCol, reduce)
			}).
			Children(func() {
				thumbPath := tile.path
				if !filePresent(thumbPath) {
					if src := taskSource(task); filePresent(src) {
						thumbPath = src
					} else {
						thumbPath = ""
					}
				}
				switch {
				case thumbPath != "":
					if bmp := a.cachedThumb(thumbPath); bmp != nil {
						ui.Image(c, bmp).Size(tileW, tileW).Fit(ui.Cover).Radius(tok.radiusS)
						return
					}
					promptMarkBox(c, tok, tileW)
				case task.Status == tasks.StatusAwaitUser:
					ui.Text(c, "待验证").FontSize(10).Bold().TextColor(tok.warning)
				default:
					promptMarkBox(c, tok, tileW)
				}
			})

		a.attachTileHoldPreview(c, shot, tile)

		metaText := taskElapsedText(task, c.Now())
		metaCol := tok.muted
		if task.Status == tasks.StatusAwaitUser {
			metaText = "需要人工验证"
			metaCol = tok.warning
		}
		ui.Text(c.Key("elapsed"), metaText).
			Width(tileW).
			FontSize(tok.caption).
			FontFeatures("tnum").
			TextColor(metaCol).
			SingleLine()
	})
}

func (a *App) resultIssueRow(c *ui.Context, row resultRow) {
	if len(row.tiles) == 0 {
		return
	}
	tok := a.look
	tile := row.tiles[0]
	task := tile.task
	title := tileTitle(tile)
	reason, action, retry := tileFailure(task)
	statusText := tileStatus(task)
	account := a.accountName(task.BrowserID)

	thumbPath := tile.path
	if !filePresent(thumbPath) {
		if src := taskSource(task); filePresent(src) {
			thumbPath = src
		} else {
			thumbPath = ""
		}
	}

	ui.Row(c.Key(row.key)).FillWidth().Padding(0, 0, 6, 0).Children(func() {
		ui.Row(c.Key("issue-box")).
			FillWidth().
			Background(tok.sunken).
			Border(1, tok.border).
			Radius(tok.radiusM).
			Padding(8, 10).
			Gap(10).
			AlignItems(ui.Center).
			Label("结果卡" + title).
			Children(func() {
				thumb := ui.ButtonBase(c.Key("shot")).
					Size(36, 36).
					Radius(tok.radiusS).
					Background(tok.sunkenHi).
					Border(1, tok.border).
					Clip().
					Shrink(0).
					Justify(ui.Center).
					AlignItems(ui.Center).
					Label("结果图").
					OnClick(func() {
						if filePresent(tile.path) {
							a.openResult(tile.path)
						}
					}).
					Children(func() {
						if thumbPath != "" {
							if bmp := a.cachedThumb(thumbPath); bmp != nil {
								ui.Image(c, bmp).Size(36, 36).Fit(ui.Cover)
								return
							}
						}
						ui.Icon(c, iconImages).FontSize(14).TextColor(tok.muted)
					})
				a.attachTileHoldPreview(c, thumb, tile)

				ui.Column(c.Key("info")).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Row(c.Key("top")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						statusPill(c, statusText)
						ui.Text(c, title).FontSize(tok.body).FontWeight(500).TextColor(tok.text).SingleLine().MinWidth(0).Grow(1)
						if account != "" {
							ui.Text(c, account).FontSize(tok.caption).TextColor(tok.muted).FontFeatures("tnum").SingleLine().Shrink(0)
						}
					})
					reasonCol := tok.muted
					if task.Status == tasks.StatusFailed {
						reasonCol, _, _ = semanticToneColors(tok.dark, tok.text, tok.danger, 0.12)
					} else if task.Status == tasks.StatusNeedsUser || task.Status == tasks.StatusNeedsReview {
						reasonCol, _, _ = semanticToneColors(tok.dark, tok.text, tok.warning, 0.12)
					}
					if reason == "" {
						reason = statusText
					}
					ui.Text(c.Key("reason"), reason).FontSize(tok.caption).TextColor(reasonCol).SingleLine()
					if task.Rewritten {
						ui.Text(c.Key("rewrite"), "已改写："+shortLine(task.Prompt, 36)).
							FontSize(tok.caption).TextColor(tok.muted).SingleLine()
					}
				})

				ui.Row(c.Key("ops")).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
					if tile.path != "" && filePresent(tile.path) {
						a.revealButton(c, tile.path)
					}
					if action != "" && len(retry) > 0 {
						card := ProgressCard{Action: action, Retry: retry}
						uiMiniBtn(ui.Button(c.Key("retry"), action), tok).
							Disabled(a.running).OnClick(func() { a.failAction(card) })
					}
				})
			})
	})
}

func (a *App) donePopoverMetrics(c *ui.Context, tileCount int) (popW, popH float32, cols int, tileSize float32) {
	winW, winH := c.Size()
	maxPopW := min(float32(680/interfaceZoom), max(float32(300), winW-40))
	innerW := max(resultTileMin, maxPopW-32)
	maxCols, tSize := resultGridLayout(innerW, resultDoneMaxCols)

	usedCols := maxCols
	if tileCount > 0 && tileCount < usedCols {
		usedCols = tileCount
	}
	if usedCols < 1 {
		usedCols = 1
	}

	gridW := float32(usedCols)*tSize + float32(usedCols-1)*resultTileGap
	popW = max(min(float32(340), maxPopW), min(maxPopW, gridW+32))
	cols, tileSize = resultGridLayout(max(resultTileMin, popW-32), resultDoneMaxCols)

	rowCount := 1
	if tileCount > 0 {
		rowCount = (tileCount + cols - 1) / cols
	}
	rowH := tileSize + 24 + 8
	contentH := 44 + float32(rowCount)*rowH + 24
	maxPopH := max(float32(220), winH-110)
	popH = min(maxPopH, max(float32(150), contentH))
	return popW, popH, cols, tileSize
}

func buildDoneResultRows(tiles []resultTile, cols int) []resultRow {
	if cols < 1 {
		cols = 1
	}
	rows := make([]resultRow, 0, (len(tiles)+cols-1)/cols)
	for start := 0; start < len(tiles); start += cols {
		end := min(start+cols, len(tiles))
		rows = append(rows, resultRow{
			key:   fmt.Sprintf("done-row:%d", start),
			tiles: tiles[start:end],
		})
	}
	return rows
}

func (a *App) resultsDonePopover(c *ui.Context, anchor ui.Element, split resultSplit) {
	tok := a.look
	ui.PopoverBase(c.Key("done-popover"), anchor, &a.resultsDoneOpen, func(panel ui.Element) {
		popW, popH, cols, tileSize := a.donePopoverMetrics(c, len(split.doneTiles))
		rows := buildDoneResultRows(split.doneTiles, cols)

		panel.AttachTo(anchor, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(6, 0, 0, 0).
			Width(popW).
			Height(popH).
			Background(tok.card).
			Border(1, tok.border).
			Radius(tok.radiusL).
			Shadow(0, 8, 22, 0, tok.lift).
			Padding(12).
			Gap(8).
			Label("已完成结果弹窗")
		if !c.Preferences().ReduceMotion {
			panel.Transition(motionPopup())
		}

		panel.Children(func() {
			ui.Row(c.Key("pop-head")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Column(c.Key("pop-title")).Grow(1).MinWidth(0).Gap(1).Children(func() {
					ui.Text(c, "已完成结果").FontSize(tok.title).Bold().TextColor(tok.text)
					sub := fmt.Sprintf("共 %d 张结果图 · 悬停可预览大图", len(split.doneTiles))
					if split.doneTasks > 0 && split.doneTasks != len(split.doneTiles) {
						sub = fmt.Sprintf("%d 项任务 · %d 张结果图 · 悬停可预览大图", split.doneTasks, len(split.doneTiles))
					}
					ui.Text(c, sub).FontSize(tok.caption).TextColor(tok.muted).FontFeatures("tnum").SingleLine()
				})
				uiIconTextBtn(c, "pop-open", "打开输出目录", "打开输出目录", iconFolderOpen, 24, false, tok).
					OnClick(func() { a.openOutputFolder() })
				uiIconBtn(c, "pop-close", "关闭已完成面板", "关闭", iconX, 24, false, true, tok).
					OnClick(func() { a.resultsDoneOpen = false })
			})
			ui.Divider(c).Background(tok.border)

			st := a.doneResultList()
			st.Key = func(i int) any {
				if i < 0 || i >= len(rows) {
					return i
				}
				return rows[i].key
			}
			ui.List(c.Key("done-list"), st, len(rows), func(i int) {
				if i < 0 || i >= len(rows) {
					return
				}
				row := rows[i]
				ui.Row(c.Key(row.key)).Padding(0, 0, 8, 0).Gap(resultTileGap).AlignItems(ui.Start).Children(func() {
					for _, tile := range row.tiles {
						a.resultDoneTile(c, tile, tileSize)
					}
				})
			}).Grow(1).FillWidth()
		})
	})
}

func (a *App) resultDoneTile(c *ui.Context, tile resultTile, tileW float32) {
	tok := a.look
	path := tile.path
	title := tileTitle(tile)
	present := path != "" && filePresent(path)

	ui.Column(c.Key(tile.key)).Width(tileW).Shrink(0).Gap(4).Label("结果卡" + title).Children(func() {
		var shot ui.Element
		if path != "" {
			btn := ui.ButtonBase(c.Key("shot")).
				Size(tileW, tileW).
				Radius(tok.radiusS).
				Background(tok.sunken).
				Border(1, tok.border).
				Clip().
				Shrink(0).
				Label("结果图")
			if present {
				btn.Cursor(ui.CursorPointer)
				p := path
				btn.OnClick(func() { a.openResult(p) })
			}
			if btn.Hovered() {
				btn.Border(1, tok.borderStrong)
			}
			if btn.FocusVisible() {
				btn.Border(1, tok.accent)
			}
			shot = hoverPressPose(btn)
		} else {
			shot = ui.Box(c.Key("shot")).
				Size(tileW, tileW).
				Radius(tok.radiusS).
				Background(tok.sunken).
				Border(1, tok.border).
				Clip().
				Shrink(0).
				Label("结果图")
		}

		shot.Children(func() {
			switch {
			case present:
				if bmp := a.cachedThumb(path); bmp != nil {
					ui.Image(c, bmp).Size(tileW, tileW).Fit(ui.Cover).Radius(tok.radiusS)
				}
			case path != "":
				ui.Column(c).Size(tileW, tileW).Center().Padding(4).Children(func() {
					ui.Text(c, "文件已移除").FontSize(tok.caption).TextColor(tok.muted).SingleLine()
				})
			default:
				src := taskSource(tile.task)
				if filePresent(src) {
					if bmp := a.cachedThumb(src); bmp != nil {
						ui.Image(c, bmp).Size(tileW, tileW).Fit(ui.Cover).Radius(tok.radiusS)
						return
					}
				}
				promptMarkBox(c, tok, tileW)
			}
		})

		a.attachTileHoldPreview(c, shot, tile)

		ui.Row(c.Key("name")).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(tok.caption).SingleLine().MinWidth(0).Grow(1)
			if path != "" && present {
				a.revealButton(c, path)
			}
		})
	})
}

// revealButton 是文件名旁边的文件夹图标，按内容收缩，不拉成缩略图那么宽。
func (a *App) revealButton(c *ui.Context, path string) {
	tok := a.look
	b := ui.ButtonBase(c.Key("folder")).Size(20, 20).Radius(5).Shrink(0).Center().
		Label("在文件夹中显示").Tooltip("在文件夹中显示").Cursor(ui.CursorPointer)
	ink := tok.muted
	switch {
	case b.Pressed():
		b.Background(tok.text.Alpha(0.12))
		ink = tok.text
	case b.Hovered() || b.FocusVisible():
		b.Background(tok.sunkenHi)
		ink = tok.text
	}
	if b.FocusVisible() {
		b.Border(1, tok.accent)
	}
	b.OnClick(func() { a.revealResult(path) })
	b.Children(func() {
		ui.Icon(c, iconFolder).FontSize(12).TextColor(ink)
	})
}

func (a *App) attachTileHoldPreview(c *ui.Context, anchor ui.Element, tile resultTile) {
	tok := a.look
	title := tileTitle(tile)
	anchor.Description(title + "，悬停可查看大图")

	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		previewPath := tile.path
		if !filePresent(previewPath) {
			if src := taskSource(tile.task); filePresent(src) {
				previewPath = src
			}
		}

		var bmp *ui.Bitmap
		if filePresent(previewPath) {
			bmp = a.cachedPreview(previewPath)
			if bmp == nil {
				bmp = a.cachedThumb(previewPath)
			}
		}

		winW, winH := c.Size()
		maxImgW := min(resultPreviewMax, max(float32(160), winW-140))
		maxImgH := min(resultPreviewMax, max(float32(160), winH-130))

		imgW := float32(220)
		imgH := float32(160)
		if bmp != nil {
			imgW, imgH = fitPreviewSize(bmp, maxImgW, maxImgH)
		}
		cardW := max(float32(200), imgW+20)

		tip.AttachTo(anchor, ui.AnchorStart, ui.AnchorEnd).
			Margin(0, 10, 0, 10).
			Width(cardW).
			Background(tok.card).
			Border(1, tok.border).
			Radius(tok.radiusL).
			Shadow(0, 8, 22, 0, tok.lift).
			Padding(10).
			Gap(8).
			Label("大图预览").
			Children(func() {
				ui.Box(c.Key("frame")).
					Width(cardW-20).
					Height(imgH).
					Radius(tok.radiusM).
					Clip().
					Background(tok.sunken).
					Border(1, tok.border).
					Justify(ui.Center).
					AlignItems(ui.Center).
					Children(func() {
						if bmp != nil {
							ui.Image(c, bmp).Size(imgW, imgH).Fit(ui.Contain)
							return
						}
						ui.Column(c.Key("wait")).
							Justify(ui.Center).AlignItems(ui.Center).Gap(4).
							Children(func() {
								ui.Icon(c, iconImages).FontSize(22).TextColor(tok.muted)
								waitLabel := "等待生成预览图"
								if tile.task.Status == tasks.StatusSuccess && tile.path != "" {
									waitLabel = "文件已移除"
								}
								ui.Text(c, waitLabel).FontSize(tok.caption).TextColor(tok.muted)
							})
					})

				ui.Column(c.Key("meta")).FillWidth().Gap(3).Children(func() {
					ui.Row(c.Key("head")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						statusPill(c, tileStatus(tile.task))
						ui.Text(c, title).Grow(1).MinWidth(0).FontSize(tok.body).Bold().TextColor(tok.text).SingleLine()
					})

					var subParts []string
					if acc := a.accountName(tile.task.BrowserID); acc != "" {
						subParts = append(subParts, acc)
					}
					if elapsed := taskElapsedText(tile.task, c.Now()); elapsed != "" {
						subParts = append(subParts, elapsed)
					}
					if src := taskSource(tile.task); src != "" && tile.path != "" && src != tile.path {
						subParts = append(subParts, "原图 "+filepath.Base(src))
					}
					if len(subParts) > 0 {
						ui.Text(c.Key("sub"), strings.Join(subParts, " · ")).
							FontSize(tok.caption).FontFeatures("tnum").TextColor(tok.muted).SingleLine()
					}

					if reason, _, _ := tileFailure(tile.task); reason != "" {
						ui.Text(c.Key("err"), reason).
							FontSize(tok.caption).TextColor(tok.danger).MaxLines(2)
					} else if tile.task.Rewritten {
						ui.Text(c.Key("rw"), "已改写："+shortLine(tile.task.Prompt, 48)).
							FontSize(tok.caption).TextColor(tok.muted).MaxLines(2)
					}
				})
			})
	})
}

func fitPreviewSize(bmp *ui.Bitmap, maxW, maxH float32) (w, h float32) {
	bw, bh := bmp.Size()
	if bw <= 0 || bh <= 0 {
		return min(maxW, 200), min(maxH, 200)
	}
	scale := min(maxW/float32(bw), maxH/float32(bh))
	return max(32, float32(bw)*scale), max(32, float32(bh)*scale)
}

func isTaskActiveStatus(status tasks.Status) bool {
	return status == tasks.StatusRunning || status == tasks.StatusRetry || status == tasks.StatusAwaitUser
}

func taskElapsedText(task tasks.Task, now time.Time) string {
	if isTaskActiveStatus(task.Status) {
		if task.StartedAt.IsZero() {
			return "耗时 0秒"
		}
		d := now.Sub(task.StartedAt)
		if d < 0 {
			d = 0
		}
		return "耗时 " + formatElapsedDuration(d, false)
	}
	if task.Elapsed > 0 {
		return "耗时 " + formatElapsedDuration(task.Elapsed, true)
	}
	return "耗时 --"
}

func formatElapsedDuration(d time.Duration, round bool) string {
	if d < 0 {
		d = 0
	}
	var sec int
	if round {
		sec = int((d + time.Second/2) / time.Second)
		if sec < 1 {
			sec = 1
		}
	} else {
		sec = int(d / time.Second)
	}
	if sec < 60 {
		return fmt.Sprintf("%d秒", sec)
	}
	return fmt.Sprintf("%d分%02d秒", sec/60, sec%60)
}

func taskSource(task tasks.Task) string {
	if len(task.Images) == 0 {
		return ""
	}
	return strings.TrimSpace(task.Images[0])
}

func tileTitle(tile resultTile) string {
	if tile.path != "" {
		return filepath.Base(tile.path)
	}
	if src := taskSource(tile.task); src != "" {
		return filepath.Base(src)
	}
	if line := oneLine(tile.task.Prompt); line != "" {
		return shortLine(line, 28)
	}
	return "未命名"
}

func tileStatus(task tasks.Task) string {
	switch task.Status {
	case tasks.StatusRunning, tasks.StatusRetry:
		return "进行中"
	case tasks.StatusSuccess:
		return "完成"
	case tasks.StatusFailed:
		return "失败"
	case tasks.StatusSkipped:
		return "已停止"
	case tasks.StatusPending:
		return "排队中"
	case tasks.StatusNeedsUser:
		return "待验证"
	case tasks.StatusAwaitUser:
		return "需要人工验证"
	default:
		if task.Status == "" {
			return "排队中"
		}
		return string(task.Status)
	}
}

func tileFailure(task tasks.Task) (reason, action string, retry []tasks.Task) {
	switch task.Status {
	case tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser, tasks.StatusNeedsReview:
		hint := tasks.ClassifyFailure(task.Status, task.Error)
		label := hint.Button
		if label == "" {
			label = "重试"
		}
		return hint.Sentence, label, []tasks.Task{task}
	case tasks.StatusAwaitUser:
		return "请在弹出的窗口里完成验证", "", nil
	default:
		return "", "", nil
	}
}

func filePresent(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (a *App) revealResult(path string) {
	dir := strings.TrimSpace(filepath.Dir(path))
	if dir == "" || dir == "." {
		a.outputNote = "无法在文件夹中显示"
		logging.Log.Errorf("无法在文件夹中显示")
		return
	}
	if err := mygo.Shell.OpenPath(dir); err != nil {
		a.outputNote = "无法在文件夹中显示"
		logging.Log.Errorf("无法在文件夹中显示: %v", err)
	}
}
