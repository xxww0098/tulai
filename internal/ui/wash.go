package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/tasks"
)

// DefaultWashPrompt 是洗图的内置指令。用户改过就存在 wash_prompt 里。
const DefaultWashPrompt = prompts.DefaultWashPrompt

// pageState 保存一个工作区的图片、提示词、进度和右栏平台。
type pageState struct {
	sources     []sourceImage
	srcSel      int
	prompts     string
	queue       []tasks.Task
	progress    float64
	progText    string
	stageTab    int
	stagePicked bool
	flowPin     int
	skipImages  bool
}

func (a *App) capturePage() pageState {
	return pageState{
		sources: a.sources, srcSel: a.srcSel, prompts: a.prompts, queue: a.queue,
		progress: a.progress, progText: a.progText, stageTab: a.stageTab, stagePicked: a.stagePicked,
		flowPin: a.flowPin, skipImages: a.skipImages,
	}
}

func (a *App) restorePage(s pageState) {
	a.sources, a.srcSel, a.prompts, a.queue = s.sources, s.srcSel, s.prompts, s.queue
	a.progress, a.progText, a.stageTab, a.stagePicked = s.progress, s.progText, s.stageTab, s.stagePicked
	a.flowPin, a.skipImages = s.flowPin, s.skipImages
}

// statePageFor 是这个导航页用哪一份状态。浏览器、提示词和设置跟着生图。
func statePageFor(tab int) int {
	if tab == pageWash || tab == pageFission {
		return tab
	}
	return pageHome
}

// syncPage 让 App 字段里装着当前导航页那一份状态。
// 去重页有自己的图片列表和阈值，不借用生图、洗图那一份。
func (a *App) syncPage() {
	if a == nil || a.tab == pageDedup || a.tab == pageImageEdit {
		return
	}
	a.noteWorkspacePage()
	want := statePageFor(a.tab)
	if a.statePage == want {
		return
	}
	// 换页后左栏输入区从顶部开始，不沿用另一页的滚动位置。
	a.workInputScroll.Y = 0
	a.historyOpen = false
	if a.parked == nil {
		a.parked = make(map[int]pageState)
	}
	a.parked[a.statePage] = a.capturePage()
	a.restorePage(a.stateOf(want))
	a.statePage = want
}

// stateOf 读某一页的状态，不切换当前页。
func (a *App) stateOf(page int) pageState {
	if a.statePage == page {
		return a.capturePage()
	}
	if st, ok := a.parked[page]; ok {
		return st
	}
	if page == pageFission {
		return pageState{srcSel: -1, prompts: DefaultFissionPrompt}
	}
	return pageState{srcSel: -1}
}

// onPage 在 page 那一份状态上改。异步选图回来时用户可能已经换了页。
func (a *App) onPage(page int, fn func()) {
	if a.statePage == page {
		fn()
		return
	}
	cur := a.capturePage()
	a.restorePage(a.stateOf(page))
	fn()
	if a.parked == nil {
		a.parked = make(map[int]pageState)
	}
	a.parked[page] = a.capturePage()
	a.restorePage(cur)
}

func (a *App) washing() bool    { return a.statePage == pageWash }
func (a *App) fissioning() bool { return a.statePage == pageFission }

// washCounts 是当前洗图工作区的原图进度。失败和停止的图仍算未洗，下次继续时重试。
func (a *App) washCounts() (washed, pending int) {
	if a == nil {
		return 0, 0
	}
	for _, src := range a.sources {
		if src.Washed {
			washed++
		} else {
			pending++
		}
	}
	return washed, pending
}

// washPendingPaths 只提交还没有成功保存结果的原图。
func (a *App) washPendingPaths() []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.sources))
	for _, src := range a.sources {
		if !src.Washed {
			out = append(out, src.Path)
		}
	}
	return out
}

// washSoloPaths 返回上次因内容审核失败的未洗源图路径，继续洗图时单独发送，避免再整批连坐。
func (a *App) washSoloPaths() []string {
	if a == nil {
		return nil
	}
	st := a.stateOf(pageWash)
	lastModeration := make(map[string]bool, len(st.queue))
	for _, task := range st.queue {
		if len(task.Images) != 1 {
			continue
		}
		lastModeration[filepath.Clean(task.Images[0])] = tasks.ClassifyFailure(task.Status, task.Error).Class == tasks.FailModeration
	}
	var out []string
	for _, src := range st.sources {
		if !src.Washed && lastModeration[filepath.Clean(src.Path)] {
			out = append(out, src.Path)
		}
	}
	return out
}

func (a *App) beginWashRun() {
	for i := range a.sources {
		a.sources[i].Washing = false
	}
}

// applyWashTask 把引擎回调落到对应原图上。已洗标记只升不降，失败不会让历史成功记录倒回去。
func (a *App) applyWashTask(t tasks.Task) {
	if a == nil || len(t.Images) == 0 {
		return
	}
	path := filepath.Clean(t.Images[0])
	for i := range a.sources {
		src := &a.sources[i]
		if filepath.Clean(src.Path) != path {
			continue
		}
		switch t.Status {
		case tasks.StatusRunning, tasks.StatusRetry, tasks.StatusAwaitUser:
			src.Washing = true
		case tasks.StatusSuccess:
			src.Washing = false
			if len(t.Results) > 0 {
				src.Washed = true
				src.WashOutput = t.Results[0]
			}
		default:
			src.Washing = false
		}
		return
	}
}

// applyWashQueue 在停止或整批结束后按最终队列补齐进度，避免漏掉最后一个回调。
func (a *App) applyWashQueue(items []tasks.Task) {
	for i := range a.sources {
		a.sources[i].Washing = false
	}
	for _, task := range items {
		if task.Status != tasks.StatusSuccess || len(task.Images) != 1 || len(task.Results) == 0 {
			continue
		}
		a.applyWashTask(task)
	}
}

// ownsRun 是正在跑或刚跑完的那一批属于当前页。
func (a *App) ownsRun() bool { return a.runPage == a.statePage }

func (a *App) washPrompt() string {
	if p := strings.TrimSpace(a.prompts); p != "" {
		return p
	}
	return DefaultWashPrompt
}

// expandImagePaths 把文件夹换成里面的图片（不进子文件夹，按文件名排）。
func expandImagePaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		var names []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(entry.Name())) {
			case ".png", ".jpg", ".jpeg", ".webp", ".gif":
				names = append(names, filepath.Join(p, entry.Name()))
			}
		}
		sort.Strings(names)
		out = append(out, names...)
	}
	return out
}

// addFolder 选一个或几个文件夹，把里面的图片加进当前页。
func (a *App) addFolder() {
	window := a.window
	page := a.statePage
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
		loaded := loadSourceFiles(paths)
		window.Update(func() { a.addLoaded(page, loaded) })
	}()
}

func (a *App) addLoaded(page int, loaded []sourceImage) {
	if page == pageFission {
		if a.running || len(loaded) == 0 {
			return
		}
		st := a.stateOf(page)
		replacing := len(st.sources) != 1 || !sameFissionSource(st.sources[0].Path, loaded[0].Path)
		if replacing && len(st.queue) > 0 && a.store != nil {
			if !a.persistWorkspace(page) {
				return
			}
			if id := a.sessionID(page); id > 0 {
				if err := a.store.ArchiveSession(id); err != nil {
					logging.Log.Errorf("收起裂变任务失败: %v", err)
					return
				}
				a.setSessionID(page, 0)
			}
		}
	}
	a.onPage(page, func() {
		if page == pageFission {
			if len(loaded) > 0 {
				if len(a.sources) != 1 || !sameFissionSource(a.sources[0].Path, loaded[0].Path) {
					a.queue = nil
					a.progress = 0
					a.progText = ""
				}
				a.sources = []sourceImage{loaded[0]}
				a.srcSel = 0
			}
			return
		}
		if n := a.appendSources(loaded, page == pageWash); n > 0 {
			logging.Log.Info("%s添加 %d 张图片", pageLabel(page), n)
		}
	})
}

func pageLabel(page int) string {
	if page == pageFission {
		return "裂变图片"
	}
	if page == pageWash {
		return "洗图"
	}
	return "生图"
}

func (a *App) washPage(c *ui.Context) {
	a.workColumns(c, func() { a.washInput(c) })
}

// washPromptCard 是洗图指令。默认是内置的一段，改过的会保存。
func (a *App) washPromptCard(c *ui.Context) {
	tok := a.look
	_, winH := c.Size()
	promptHeight := float32(90)
	if winH < 720 {
		promptHeight = 68
	}
	hint := "按「原文件名_01.png」保存"
	batch := a.store != nil && a.store.Get("browserBackend") == "camoufox_extension" && a.waitResult && a.stagePlatform() == "豆包"
	switch {
	case a.washAutoNumber && batch:
		hint = "每批最多 10 张 · 自动编号保存"
	case a.washAutoNumber:
		hint = "按「前缀+序号」自动编号保存"
	case batch:
		hint = "每批最多 10 张 · 按源文件名保存"
	}
	a.stepCard(c, "wash-prompt", stepDone).Children(func() {
		ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "洗图指令").FontSize(tok.title).Bold().Shrink(0)
			ui.Spacer(c)
			a.washReset(c)
		})
		ui.TextArea(c, &a.prompts).Height(promptHeight).MinHeight(promptHeight).FillWidth().Label("洗图指令").Placeholder(DefaultWashPrompt).Disabled(a.running)
		ui.Text(c, hint).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(hint)
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			a.washPromptSelect(c)
			ui.Spacer(c)
			a.promptPartsButton(c)
		})
	})
}

// washReset 是标题行右侧的次要图标按钮，保留 Label 与 Tooltip。
func (a *App) washReset(c *ui.Context) {
	tok := a.look
	uiIconBtn(c, "wash-reset", "恢复默认", "恢复默认指令", iconRotateCcw, 22, false, false, tok).
		Disabled(a.running || a.washPrompt() == DefaultWashPrompt).OnClick(func() {
		a.prompts = DefaultWashPrompt
	})
}
