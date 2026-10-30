package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// noteWorkspacePage 记下当前是生图还是洗图。浏览器、设置这些页不覆盖它。
func (a *App) noteWorkspacePage() {
	if a == nil {
		return
	}
	switch a.tab {
	case pageHome:
		a.workspacePage = store.PageGen
	case pageWash:
		a.workspacePage = store.PageWash
	}
}

func workspacePageKey(page int) string {
	if page == pageWash {
		return store.PageWash
	}
	return store.PageGen
}

func (a *App) sessionID(page int) int64 {
	if page == pageWash {
		return a.washSession
	}
	return a.genSession
}

func (a *App) setSessionID(page int, id int64) {
	if page == pageWash {
		a.washSession = id
		return
	}
	a.genSession = id
}

func promptMeansContent(page int, prompts string) bool {
	p := strings.TrimSpace(prompts)
	if page == pageWash {
		return p != "" && p != DefaultWashPrompt
	}
	return p != ""
}

func (a *App) workspaceHasContent(page int) bool {
	st := a.stateOf(page)
	if len(st.queue) > 0 || len(st.sources) > 0 {
		return true
	}
	return promptMeansContent(page, st.prompts)
}

// workspaceBar 是这一页右上角的历史和新建任务。按钮按内容收缩。
func (a *App) workspaceBar(c *ui.Context) {
	tok := a.look
	ui.Row(c.Key("workspace-bar")).FillWidth().Height(workspaceBarH).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, pageLabel(a.statePage)).FontSize(tok.title).Bold().Grow(1)
		a.historyControl(c)
		ui.Button(c.Key("new-task"), "新建任务").FontSize(tok.caption).Padding(3, 8).Radius(tok.radiusS).Shrink(0).
			Disabled(a.running).OnClick(func() { a.newWorkspace() })
	})
}

func (a *App) historyControl(c *ui.Context) {
	open := a.historyOpen
	btn := ui.Button(c.Key("history"), "历史").FontSize(a.look.caption).Padding(3, 8).Radius(a.look.radiusS).Shrink(0)
	btn.OnClick(func() { a.historyOpen = !open })
}

// historyPanel 在工作区标题和平台页签之间展开。列表占右栏自己的高度，把页签和账号表往下推，不盖住账号行、添加账号或运行日志。
func (a *App) historyPanel(c *ui.Context) {
	if a == nil || !a.historyOpen {
		return
	}
	tok := a.look
	ui.Column(c.Key("history-panel")).Label("历史任务").FillWidth().Shrink(0).Gap(6).Padding(8, 10).
		Radius(tok.radiusM).Background(tok.card).Border(1, tok.border).Children(func() {
		ui.Text(c, "历史任务").FontSize(tok.caption).Bold().TextColor(tok.muted).SingleLine()
		a.historyBody(c)
	})
}

func (a *App) historyBody(c *ui.Context) {
	tok := a.look
	if a.store == nil {
		ui.Text(c, "还没有历史任务").FontSize(tok.caption).TextColor(tok.muted)
		return
	}
	rows, err := a.store.History(workspacePageKey(a.statePage))
	if err != nil {
		ui.Text(c, "读取任务记录失败").FontSize(tok.caption).TextColor(tok.danger)
		return
	}
	if len(rows) == 0 {
		ui.Text(c, "还没有历史任务").FontSize(tok.caption).TextColor(tok.muted)
		return
	}
	ui.Scroll(c.Key("history-scroll")).FillWidth().MaxHeight(168).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for _, row := range rows {
				a.historyRow(c, row)
			}
		})
	})
}

func (a *App) historyRow(c *ui.Context, sess store.Session) {
	tok := a.look
	shown, full := sessionPreview(sess)
	id := sess.ID
	ui.Column(c.Key(id)).FillWidth().Gap(4).Padding(4, 6).Radius(tok.radiusS).Background(tok.sunken).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if when := sessionWhen(sess.Updated); when != "" {
				ui.Text(c, when).FontSize(tok.caption).TextColor(tok.muted).Shrink(0).SingleLine()
			}
			ui.Text(c, sessionCountText(sess)).FontSize(tok.caption).TextColor(tok.muted).Shrink(0).SingleLine()
			ui.Text(c, shown).FontSize(tok.caption).Grow(1).MinWidth(0).SingleLine().Tooltip(full)
		})
		ui.Row(c).Gap(6).Children(func() {
			ui.Button(c.Key("restore"), "恢复").FontSize(tok.caption).Padding(2, 8).Radius(tok.radiusS).Shrink(0).
				Disabled(a.running).OnClick(func() { a.reopenSession(id) })
			ui.Button(c.Key("delete"), "删除").FontSize(tok.caption).Padding(2, 8).Radius(tok.radiusS).Shrink(0).
				OnClick(func() { a.deleteSession(id) })
		})
	})
}

func sessionPreview(sess store.Session) (shown, full string) {
	var raw string
	if sess.Page == store.PageWash {
		if img := sessionFirstLine(sess.Images); img != "" {
			raw = filepath.Base(img)
		}
	}
	if raw == "" {
		raw = sessionFirstLine(sess.Prompts)
	}
	if raw == "" {
		if img := sessionFirstLine(sess.Images); img != "" {
			raw = filepath.Base(img)
		}
	}
	if raw == "" {
		raw = "空任务"
	}
	return trimRunes(raw, 24), raw
}

func sessionCountText(sess store.Session) string {
	if sess.Tasks > 0 {
		return fmt.Sprintf("%d 条", sess.Tasks)
	}
	if n := len(splitLines(sess.Images)); n > 0 {
		return fmt.Sprintf("%d 张", n)
	}
	if strings.TrimSpace(sess.Prompts) != "" {
		return "1 条"
	}
	return "0 条"
}

func sessionWhen(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).Format("01-02 15:04")
}

func sessionFirstLine(raw string) string {
	lines := splitLines(raw)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func trimRunes(s string, n int) string {
	rs := []rune(s)
	if n <= 0 || len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// newWorkspace 收起当前会话并清空这一页。正在跑时不能新建。
func (a *App) newWorkspace() {
	if a == nil || a.running {
		return
	}
	page := a.statePage
	if a.store != nil && a.workspaceHasContent(page) {
		a.persistWorkspace(page)
		if id := a.sessionID(page); id > 0 {
			if err := a.store.ArchiveSession(id); err != nil {
				logging.Log.Errorf("收起当前任务失败: %v", err)
			}
			a.setSessionID(page, 0)
		}
	}
	a.clearWorkspace(page)
	a.historyOpen = false
	if a.store != nil {
		a.saveSettings()
	}
	logging.Log.Info("已新建%s任务", pageLabel(page))
}

func (a *App) clearWorkspace(page int) {
	a.onPage(page, func() {
		a.sources = nil
		a.srcSel = -1
		a.queue = nil
		a.progress = 0
		a.progText = ""
		a.flowPin = 0
		a.skipImages = false
		if page == pageWash {
			a.prompts = DefaultWashPrompt
		} else {
			a.prompts = ""
		}
	})
	if page != pageWash {
		a.freeformPrompts = ""
		a.promptPick = ""
		a.promptChoice = prompts.LabelCustom
		a.promptNote = ""
		a.batchMode = batchText
	}
	a.dropStamps(page)
	if page == pageWash {
		a.washList = ui.ListState{}
	} else {
		a.genList = ui.ListState{}
	}
}

func (a *App) reopenSession(id int64) {
	if a == nil || a.running || a.store == nil || id <= 0 {
		return
	}
	page := a.statePage
	sess, items, err := a.store.Session(id)
	if err != nil {
		logging.Log.Errorf("读取任务记录失败: %v", err)
		return
	}
	if sess.Page != workspacePageKey(page) {
		logging.Log.Errorf("读取任务记录失败: 页不对")
		return
	}
	cur := a.sessionID(page)
	if cur != id {
		if a.workspaceHasContent(page) {
			a.persistWorkspace(page)
			cur = a.sessionID(page)
			if cur > 0 && cur != id {
				if err := a.store.ArchiveSession(cur); err != nil {
					logging.Log.Errorf("收起当前任务失败: %v", err)
					return
				}
			}
		} else if cur > 0 && cur != id {
			if err := a.store.DeleteSession(cur); err != nil {
				logging.Log.Errorf("删除历史记录失败: %v", err)
				return
			}
		}
	}
	if err := a.store.ActivateSession(id); err != nil {
		logging.Log.Errorf("读取任务记录失败: %v", err)
		return
	}
	a.applySession(page, sess, items, true)
	a.setSessionID(page, id)
	a.historyOpen = false
	if page == pageWash {
		a.washList = ui.ListState{}
	} else {
		a.genList = ui.ListState{}
	}
	a.saveSettings()
	logging.Log.Info("已恢复历史任务")
}

func (a *App) deleteSession(id int64) {
	if a == nil || a.store == nil || id <= 0 {
		return
	}
	if id == a.genSession || id == a.washSession {
		return
	}
	if err := a.store.DeleteSession(id); err != nil {
		logging.Log.Errorf("删除历史记录失败: %v", err)
		return
	}
	logging.Log.Info("已删除历史记录，图片文件仍保留")
}

func (a *App) persistWorkspace(page int) {
	if a == nil || a.store == nil {
		return
	}
	st := a.stateOf(page)
	items := a.tasksToSave(page)
	id := a.sessionID(page)
	if id == 0 && len(items) == 0 && len(st.sources) == 0 && !promptMeansContent(page, st.prompts) {
		return
	}
	saved, err := a.store.SaveActive(store.Session{
		ID:       id,
		Page:     workspacePageKey(page),
		Prompts:  st.prompts,
		Images:   joinPaths(sourcePaths(st.sources)),
		Progress: st.progress,
		ProgText: st.progText,
	}, a.tasksForSession(page, items))
	if err != nil {
		logging.Log.Errorf("保存任务记录失败: %v", err)
		return
	}
	a.setSessionID(page, saved)
}

func (a *App) restoreWorkspace(page int) {
	if a == nil || a.store == nil {
		return
	}
	sess, items, ok, err := a.store.ActiveSession(workspacePageKey(page))
	if err != nil {
		logging.Log.Errorf("读取任务记录失败: %v", err)
		return
	}
	if !ok {
		return
	}
	a.applySession(page, sess, items, false)
	a.setSessionID(page, sess.ID)
}

func (a *App) applySession(page int, sess store.Session, items []store.SessionTask, resetPick bool) {
	a.onPage(page, func() {
		a.prompts = sess.Prompts
		if page == pageWash && strings.TrimSpace(a.prompts) == "" {
			a.prompts = DefaultWashPrompt
		}
		a.sources = sourcesFromRaw(sess.Images)
		if len(a.sources) == 0 {
			a.srcSel = -1
		} else if a.srcSel < 0 || a.srcSel >= len(a.sources) {
			a.srcSel = 0
		}
		a.queue = queueFromSession(items)
		a.progress = sess.Progress
		a.progText = sess.ProgText
	})
	if page != pageWash && (resetPick || a.promptPick == "") {
		a.freeformPrompts = sess.Prompts
	}
	if resetPick && page != pageWash {
		a.promptPick = ""
		a.promptChoice = prompts.LabelCustom
		a.promptNote = ""
	}
	a.rememberStamps(page, items)
}

func (a *App) tasksToSave(page int) []tasks.Task {
	if a.useRunView && a.running && a.runPage == page && a.engine != nil {
		if view := a.engine.View(); view != nil {
			return view.Snapshot()
		}
	}
	return a.stateOf(page).queue
}

func (a *App) tasksForSession(page int, items []tasks.Task) []store.SessionTask {
	out := make([]store.SessionTask, 0, len(items))
	for i, task := range items {
		ord := i + 1
		source := ""
		if len(task.Images) > 0 {
			source = task.Images[0]
		}
		outputs := strings.Join(task.Results, "\n")
		status := string(task.Status)
		name := ""
		if task.BrowserID > 0 {
			name = a.accountName(task.BrowserID)
		}
		out = append(out, store.SessionTask{
			Ord:         ord,
			Prompt:      task.Prompt,
			Source:      source,
			Platform:    task.Platform,
			AccountID:   task.BrowserID,
			AccountName: name,
			Status:      status,
			Outputs:     outputs,
			Error:       task.Error,
			At:          a.stampAt(stampKey(page, ord, status, source, outputs)),
		})
	}
	return out
}

func queueFromSession(items []store.SessionTask) []tasks.Task {
	out := make([]tasks.Task, 0, len(items))
	for _, item := range items {
		task := tasks.Task{
			Index:     item.Ord,
			Platform:  item.Platform,
			BrowserID: item.AccountID,
			Prompt:    item.Prompt,
			Status:    tasks.Status(item.Status),
			Error:     item.Error,
			Results:   splitLines(item.Outputs),
		}
		if task.Index <= 0 {
			task.Index = len(out) + 1
		}
		if strings.TrimSpace(item.Source) != "" {
			task.Images = []string{item.Source}
		}
		out = append(out, task)
	}
	return out
}

func sourcesFromRaw(raw string) []sourceImage {
	var out []sourceImage
	for _, p := range splitLines(raw) {
		data, err := os.ReadFile(p)
		if err != nil || !images.IsValidImageBytes(data) {
			continue
		}
		out = append(out, sourceImage{Path: p, Hash: images.SHA256(data)})
	}
	return out
}

func stampKey(page, ord int, status, source, outputs string) string {
	return fmt.Sprintf("%d|%d|%s|%s|%s", page, ord, status, source, outputs)
}

func (a *App) stampAt(key string) int64 {
	if a.stamps == nil {
		a.stamps = map[string]int64{}
	}
	if at := a.stamps[key]; at > 0 {
		return at
	}
	at := time.Now().Unix()
	a.stamps[key] = at
	return at
}

func (a *App) rememberStamps(page int, items []store.SessionTask) {
	a.dropStamps(page)
	if a.stamps == nil {
		a.stamps = map[string]int64{}
	}
	for _, item := range items {
		if item.At <= 0 {
			continue
		}
		a.stamps[stampKey(page, item.Ord, item.Status, item.Source, item.Outputs)] = item.At
	}
}

func (a *App) dropStamps(page int) {
	prefix := strconv.Itoa(page) + "|"
	for key := range a.stamps {
		if strings.HasPrefix(key, prefix) {
			delete(a.stamps, key)
		}
	}
}
