package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
)

type browserOperation struct {
	stopping bool
	batch    *browserBatchRun
}

type browserBatchRun struct {
	stopping                     bool
	total, done, failed, skipped int
}

func (b *browserBatchRun) text() string {
	verb := "启动"
	if b.stopping {
		verb = "停止"
	}
	state := "正在批量" + verb
	if b.done == b.total {
		state = "批量" + verb + "完成"
	}
	return fmt.Sprintf("%s：%d/%d，失败 %d，跳过 %d", state, b.done, b.total, b.failed, b.skipped)
}

func (a *App) browserOperationAllowed(b store.Browser, stopping bool) bool {
	if a.running || b.ID <= 0 || b.ExtAccount != "" || b.IsRunning != stopping {
		return false
	}
	if _, pending := a.browserPending[b.ID]; pending {
		return false
	}
	return a.engine == nil || !a.engine.ActiveAccounts()[b.ID]
}

func (a *App) browserBatchTargets(stopping bool) []store.Browser {
	var rows []store.Browser
	for _, b := range a.browserRows {
		if a.browserChecked.Has(b.ID) && a.browserOperationAllowed(b, stopping) {
			rows = append(rows, b)
		}
	}
	return rows
}

func (a *App) runBrowserBatch(stopping bool) {
	if a.running || len(a.browserPending) > 0 {
		return
	}
	rows := a.browserBatchTargets(stopping)
	if len(rows) == 0 {
		return
	}
	batch := &browserBatchRun{stopping: stopping, total: len(rows), skipped: a.browserChecked.Len() - len(rows)}
	a.browserBatch = batch
	// 停止未登录的新环境会删除行，整批必须使用环境 ID 对应的快照。
	for _, b := range rows {
		if stopping {
			a.stopBrowserInstance(b, batch)
		} else {
			a.launchBrowserInstance(b, batch)
		}
	}
}

func (a *App) beginBrowserOperation(b store.Browser, stopping bool, batch *browserBatchRun) bool {
	if !a.browserOperationAllowed(b, stopping) {
		if batch != nil {
			batch.total--
			batch.skipped++
		}
		return false
	}
	if a.browserPending == nil {
		a.browserPending = map[int64]browserOperation{}
	}
	a.browserPending[b.ID] = browserOperation{stopping: stopping, batch: batch}
	return true
}

func (a *App) finishBrowserOperation(id int64, err error) {
	op, ok := a.browserPending[id]
	if !ok {
		return
	}
	delete(a.browserPending, id)
	if op.batch != nil {
		op.batch.done++
		if err != nil {
			op.batch.failed++
		}
	}
	a.refreshBrowsers()
}

func (a *App) pruneBrowserSelection() {
	present := make(map[int64]bool, len(a.browserRows))
	for _, b := range a.browserRows {
		present[b.ID] = true
	}
	for id := range a.browserChecked.All() {
		if !present[id] {
			a.browserChecked.Remove(id)
		}
	}
}

func (a *App) browserBatchToolbar(c *ui.Context) {
	chosen := a.browserChecked.Len()
	all := chosen > 0 && chosen == len(a.browserRows)
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		check := ui.Checkbox(c, &all, "全选").Disabled(len(a.browserRows) == 0)
		if chosen > 0 && chosen < len(a.browserRows) {
			check.Mixed()
			size := c.Theme().Space(4)
			fill, mark := c.Theme().Accent, c.Theme().AccentText
			check.DrawOver(func(p *ui.Painter, r ui.Rect) {
				y := r.Y + (r.H-size)/2
				p.Fill(ui.Rect{X: r.X, Y: y, W: size, H: size}, fill, 4)
				p.Fill(ui.Rect{X: r.X + 4, Y: y + size/2 - 1, W: size - 8, H: 2}, mark, 1)
			})
		}
		if check.Changed() {
			a.browserChecked.Clear()
			if all {
				for _, b := range a.browserRows {
					a.browserChecked.Add(b.ID)
				}
			}
		}
		ui.Text(c, fmt.Sprintf("已选 %d 个", a.browserChecked.Len())).FontSize(12).TextColor(c.Theme().TextMuted).Width(78)
		busy := a.running || len(a.browserPending) > 0
		ui.Button(c, "批量启动").FontSize(12).Shrink(0).Disabled(busy || len(a.browserBatchTargets(false)) == 0).OnClick(func() { a.runBrowserBatch(false) })
		ui.Button(c, "批量停止").FontSize(12).Shrink(0).Disabled(busy || len(a.browserBatchTargets(true)) == 0).OnClick(func() { a.runBrowserBatch(true) })
		ui.Button(c, "清空").FontSize(12).Shrink(0).Disabled(a.browserChecked.Len() == 0).OnClick(func() { a.browserChecked.Clear() })
		if a.browserBatch != nil {
			ink := c.Theme().TextMuted
			if a.browserBatch.failed > 0 {
				ink = c.Theme().Danger
			}
			ui.Text(c, a.browserBatch.text()).FontSize(12).TextColor(ink).Grow(1).MinWidth(0).SingleLine().Tooltip(a.browserBatch.text())
		}
	})
}
