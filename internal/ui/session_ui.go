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
	case pageFission:
		a.workspacePage = store.PageFission
	}
}

func workspacePageKey(page int) string {
	if page == pageFission {
		return store.PageFission
	}
	if page == pageWash {
		return store.PageWash
	}
	return store.PageGen
}

func (a *App) sessionID(page int) int64 {
	if page == pageFission {
		return a.fissionSession
	}
	if page == pageWash {
		return a.washSession
	}
	return a.genSession
}

func (a *App) setSessionID(page int, id int64) {
	if page == pageFission {
		a.fissionSession = id
		return
	}
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
	if page == pageFission {
		return p != "" && p != DefaultFissionPrompt
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

func (a *App) historyControl(c *ui.Context) {
	open := a.historyOpen
	uiIconBtn(c, "history", "历史", "历史任务", iconClock, 24, open, false, a.look).
		OnClick(func() { a.historyOpen = !open })
}

// historyPanel 在工作区标题和平台页签之间展开。列表占右栏自己的高度，把页签和账号表往下推，不盖住账号行、添加账号或运行日志。
func (a *App) historyPanel(c *ui.Context) {
	if a == nil || !a.historyOpen {
		return
	}
	tok := a.look
	ui.Column(c.Key("history-panel")).Label("历史任务").FillWidth().Shrink(0).Gap(4).Padding(6, 8).
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
	ui.Scroll(c.Key("history-scroll")).FillWidth().MaxHeight(132).Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
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
	ui.Column(c.Key(id)).FillWidth().Gap(3).Padding(4, 6).Radius(tok.radiusS).Background(tok.sunken).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			if when := sessionWhen(sess.Updated); when != "" {
				ui.Text(c, when).FontSize(tok.caption).TextColor(tok.muted).Shrink(0).SingleLine()
			}
			ui.Text(c, sessionCountText(sess)).FontSize(tok.caption).TextColor(tok.muted).Shrink(0).SingleLine()
			ui.Text(c, shown).FontSize(tok.caption).Grow(1).MinWidth(0).SingleLine().Tooltip(full)
		})
		ui.Row(c).Gap(5).Children(func() {
			ui.Button(c.Key("restore"), "恢复").FontSize(tok.caption).Padding(1, 7).Radius(tok.radiusS).Shrink(0).
				Disabled(a.running).OnClick(func() { a.reopenSession(id) })
			ui.Button(c.Key("delete"), "删除").FontSize(tok.caption).Padding(1, 7).Radius(tok.radiusS).Shrink(0).
				OnClick(func() { a.deleteSession(id) })
		})
	})
}

func sessionPreview(sess store.Session) (shown, full string) {
	var raw string
	if sess.Page == store.PageWash || sess.Page == store.PageFission {
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
		if !a.persistWorkspace(page) {
			return
		}
		if id := a.sessionID(page); id > 0 {
			if err := a.store.ArchiveSession(id); err != nil {
				logging.Log.Errorf("收起当前任务失败: %v", err)
				return
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
		} else if page == pageFission {
			a.prompts = DefaultFissionPrompt
		} else {
			a.prompts = ""
		}
	})
	if page == pageHome {
		a.freeformPrompts = ""
		a.promptPick = ""
		a.promptChoice = prompts.LabelCustom
		a.promptNote = ""
		a.batchMode = batchText
	}
	a.dropStamps(page)
	if page == pageWash {
		a.washList = ui.ListState{}
	} else if page == pageFission {
		a.fissionList = ui.ListState{}
	} else {
		a.genList = ui.ListState{}
	}
}

func (a *App) reopenSession(id int64) {
	if a == nil || a.running || a.store == nil || id <= 0 {
		return
	}
	page := a.statePage
	cur := a.sessionID(page)
	if cur == id {
		if a.persistWorkspace(page) {
			a.historyOpen = false
		}
		return
	}
	sess, items, err := a.store.Session(id)
	if err != nil {
		logging.Log.Errorf("读取任务记录失败: %v", err)
		return
	}
	if sess.Page != workspacePageKey(page) {
		logging.Log.Errorf("读取任务记录失败: 页不对")
		return
	}
	if a.workspaceHasContent(page) {
		if !a.persistWorkspace(page) {
			return
		}
	} else if cur > 0 {
		if err := a.store.DeleteSession(cur); err != nil {
			logging.Log.Errorf("删除历史记录失败: %v", err)
			return
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
	} else if page == pageFission {
		a.fissionList = ui.ListState{}
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
	if id == a.genSession || id == a.washSession || id == a.fissionSession {
		return
	}
	if err := a.store.DeleteSession(id); err != nil {
		logging.Log.Errorf("删除历史记录失败: %v", err)
		return
	}
	logging.Log.Info("已删除历史记录，图片文件仍保留")
}

func (a *App) persistWorkspace(page int) bool {
	if a == nil || a.store == nil {
		return true
	}
	st := a.stateOf(page)
	items := a.tasksToSave(page)
	id := a.sessionID(page)
	if id == 0 && len(items) == 0 && len(st.sources) == 0 && !promptMeansContent(page, st.prompts) {
		return true
	}
	saved, err := a.store.SaveActive(store.Session{
		ID:       id,
		Page:     workspacePageKey(page),
		Prompts:  st.prompts,
		Images:   joinPaths(sourcePaths(st.sources)),
		Progress: st.progress,
		ProgText: st.progText,
		Sources:  a.washSourcesForSave(page, st),
	}, a.tasksForSession(page, items))
	if err != nil {
		logging.Log.Errorf("保存任务记录失败: %v", err)
		return false
	}
	a.setSessionID(page, saved)
	return true
}

// washSourcesForSave 只给洗图会话保存原图进度。生图参考图不参与已洗/未洗记录。
func (a *App) washSourcesForSave(page int, st pageState) []store.SessionSource {
	if page != pageWash {
		return nil
	}
	out := make([]store.SessionSource, 0, len(st.sources))
	for _, src := range st.sources {
		out = append(out, store.SessionSource{
			Path:   src.Path,
			Washed: src.Washed,
			Output: src.WashOutput,
		})
	}
	return out
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
		} else if page == pageFission && strings.TrimSpace(a.prompts) == "" {
			a.prompts = DefaultFissionPrompt
		}
		a.sources = sourcesFromRaw(sess.Images)
		if page == pageFission && len(a.sources) > 1 {
			a.sources = a.sources[:1]
		}
		if len(a.sources) == 0 {
			a.srcSel = -1
		} else if a.srcSel < 0 || a.srcSel >= len(a.sources) {
			a.srcSel = 0
		}
		a.queue = queueFromSession(items)
		if page == pageWash {
			a.applyWashSession(sess, items)
		}
		a.progress = sess.Progress
		a.progText = sess.ProgText
		if tally := TallyTasks(a.queue); tally.Review > 0 {
			a.progress = float64(tally.Done+tally.Failed+tally.Needs+tally.Stopped) / float64(len(a.queue))
			a.progText = taskResultSummary(a.queue)
		}
	})
	if page == pageHome && (resetPick || a.promptPick == "") {
		a.freeformPrompts = sess.Prompts
	}
	if resetPick && page == pageHome {
		a.promptPick = ""
		a.promptChoice = prompts.LabelCustom
		a.promptNote = ""
	}
	a.rememberStamps(page, items)
}

// applyWashSession 把持久化进度和旧版任务记录合并到原图上。
// 旧存档没有 session_sources 时，成功且已保存结果的任务仍然能恢复成「已洗」。
func (a *App) applyWashSession(sess store.Session, items []store.SessionTask) {
	states := make(map[string]store.SessionSource, len(sess.Sources))
	for _, src := range sess.Sources {
		if strings.TrimSpace(src.Path) == "" {
			continue
		}
		states[filepath.Clean(src.Path)] = src
	}
	for _, item := range items {
		if item.Status != string(tasks.StatusSuccess) || strings.TrimSpace(item.Source) == "" {
			continue
		}
		outputs := splitLines(item.Outputs)
		if len(outputs) == 0 {
			continue
		}
		states[filepath.Clean(item.Source)] = store.SessionSource{
			Path: item.Source, Washed: true, Output: outputs[0],
		}
	}
	for i := range a.sources {
		src := &a.sources[i]
		state, ok := states[filepath.Clean(src.Path)]
		src.Washed = ok && state.Washed
		src.WashOutput = ""
		if src.Washed {
			src.WashOutput = state.Output
		}
		src.Washing = false
	}
}

func (a *App) tasksToSave(page int) []tasks.Task {
	if a.useRunView && a.running && a.runPage == page && a.engine != nil {
		if view := a.engine.View(); view != nil {
			items := view.Snapshot()
			if page == pageFission && a.fissionRetryBase != nil {
				items = mergeFissionRetry(a.fissionRetryBase, items)
			}
			if page == pageFission {
				// 进程中断时，尚未执行最终相似度检查的结果不能被恢复为成功。
				for i := range items {
					if items[i].Status == tasks.StatusSuccess {
						items[i].Status = tasks.StatusNeedsReview
						items[i].Error = "裂变相似度尚未完成检查，请核对后重新生成"
					}
				}
			}
			return items
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
		name := a.sessionAccountName(task)
		elapsed := task.Elapsed
		if elapsed <= 0 && !task.StartedAt.IsZero() && (task.Status == tasks.StatusRunning || task.Status == tasks.StatusRetry || task.Status == tasks.StatusAwaitUser) {
			elapsed = time.Since(task.StartedAt)
			if elapsed <= 0 {
				elapsed = time.Millisecond
			}
		}
		out = append(out, store.SessionTask{
			Ord:          ord,
			Prompt:       task.Prompt,
			Source:       source,
			Platform:     task.Platform,
			AccountID:    task.BrowserID,
			AccountName:  name,
			Status:       status,
			Outputs:      outputs,
			Error:        task.Error,
			At:           a.stampAt(stampKey(page, ord, status, source, outputs)),
			ElapsedMS:    elapsed.Milliseconds(),
			Undispatched: task.Undispatched,
		})
	}
	return out
}

func (a *App) sessionAccountName(task tasks.Task) string {
	if task.BrowserID > 0 {
		for _, b := range a.browserRows {
			if b.ID == task.BrowserID && b.Name != "" {
				return b.Name
			}
		}
		if a.store != nil {
			for _, b := range a.store.Browsers() {
				if b.ID == task.BrowserID && b.Name != "" {
					return b.Name
				}
			}
		}
	}
	if name := strings.TrimSpace(task.AccountName); name != "" {
		return name
	}
	if task.BrowserID > 0 {
		return a.accountName(task.BrowserID)
	}
	return ""
}

func queueFromSession(items []store.SessionTask) []tasks.Task {
	out := make([]tasks.Task, 0, len(items))
	for _, item := range items {
		task := tasks.Task{
			Index:        item.Ord,
			Platform:     item.Platform,
			BrowserID:    item.AccountID,
			AccountName:  item.AccountName,
			Prompt:       item.Prompt,
			Status:       tasks.Status(item.Status),
			Error:        item.Error,
			Results:      splitLines(item.Outputs),
			Elapsed:      time.Duration(item.ElapsedMS) * time.Millisecond,
			Undispatched: item.Undispatched,
		}
		// 历史记录没有提交上下文，活动态不能当作仍在执行或直接续发。
		switch task.Status {
		case tasks.StatusSuccess, tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser, tasks.StatusNeedsReview:
		default:
			task.Status = tasks.StatusNeedsReview
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
