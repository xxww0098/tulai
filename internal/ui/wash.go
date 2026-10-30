package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/tasks"
)

// DefaultWashPrompt 是洗图的内置指令。用户改过就存在 wash_prompt 里。
const DefaultWashPrompt = "提取图中的商品主体，去掉所有文字、水印和背景装饰，纯白背景，保持主体形状、颜色和细节完全不变"

// pageState 是生图和洗图各自的一份：图片、提示词、进度和右栏平台。
// 当前页的那份放在 App 字段里，另一页的收在 parked，换页时对调。
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
	if tab == pageWash {
		return pageWash
	}
	return pageHome
}

// syncPage 让 App 字段里装着当前导航页那一份状态。
// 去重页有自己的图片列表和阈值，不借用生图、洗图那一份。
func (a *App) syncPage() {
	if a == nil || a.tab == pageDedup {
		return
	}
	a.noteWorkspacePage()
	want := statePageFor(a.tab)
	if a.statePage == want {
		return
	}
	a.historyOpen = false
	cur := a.capturePage()
	a.restorePage(a.parked)
	a.parked = cur
	a.statePage = want
}

// stateOf 读某一页的状态，不切换当前页。
func (a *App) stateOf(page int) pageState {
	if a.statePage == page {
		return a.capturePage()
	}
	return a.parked
}

// onPage 在 page 那一份状态上改。异步选图回来时用户可能已经换了页。
func (a *App) onPage(page int, fn func()) {
	if a.statePage == page {
		fn()
		return
	}
	cur := a.capturePage()
	a.restorePage(a.parked)
	fn()
	a.parked = a.capturePage()
	a.restorePage(cur)
}

func (a *App) washing() bool { return a.statePage == pageWash }

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
	a.onPage(page, func() {
		if n := a.appendSources(loaded); n > 0 {
			logging.Log.Info("%s添加 %d 张图片", pageLabel(page), n)
		}
	})
}

func pageLabel(page int) string {
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
	a.stepCard(c, "wash-prompt", stepDone).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "洗图指令").FontSize(tok.title).Bold().Grow(1).MinWidth(0)
			a.washReset(c)
		})
		ui.TextArea(c, &a.prompts).Height(84).FillWidth().Label("洗图指令").Placeholder(DefaultWashPrompt).Disabled(a.running)
		ui.Text(c, "每张图单独发一次，结果存成「原文件名_01.png」").FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip("每张图单独发一次，结果存成「原文件名_01.png」")
	})
}

// washReset 是标题行右侧的次要按钮，尺寸跟选图卡的按钮对齐，不跟「开始洗图」抢主色。
func (a *App) washReset(c *ui.Context) {
	tok := a.look
	ui.Button(c, "恢复默认").FontSize(tok.caption).Padding(3, 8).Radius(tok.radiusS).Shrink(0).
		Disabled(a.running || a.washPrompt() == DefaultWashPrompt).OnClick(func() {
		a.prompts = DefaultWashPrompt
	})
}
