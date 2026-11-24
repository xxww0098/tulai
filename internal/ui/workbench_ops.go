package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

const (
	DefaultWashPrompt    = prompts.DefaultWashPrompt
	DefaultFissionPrompt = prompts.DefaultFissionPrompt
	verificationHint     = "在浏览器中完成验证后重新开始任务"
)

// ---- Output size & wash styles ----

const (
	outputOriginal = "原比例"
	outputRatio    = "宽高比"
	outputPixels   = "固定尺寸"
)

const washStyleOriginal = "保持原风格"

type washStylePreset struct {
	label       string
	instruction string
}

var washStyles = []washStylePreset{
	{washStyleOriginal, ""},
	{"像素风格", "把选中的整幅图重新画成低分辨率的二维游戏像素画：以约 48×60 的逻辑像素组织主体和背景，再用最近邻方式放大到目标尺寸。画面各处都要有清楚可见的方形色块，主体轮廓、曲线和镶嵌线都呈粗方块阶梯，不能保留平滑矢量曲线。色块紧密无缝、内部纯色，不能给每个像素画边框、留缝或加圆点高光。去除原图的细格线、填色数字、印刷网点、珠粒与纸面纹理；不要在高清原图上叠加网格，也不要做成拼豆或钻石画。保留主体主要形状和颜色，图案内部必要的深色轮廓可以由方块组成。"},
	{"水彩插画", "使用透明水彩叠色、柔和晕染和轻微纸面肌理，呈现二维水彩插画。"},
	{"油画风格", "使用可见的油画笔触、丰富的色彩层次和适度画布肌理，呈现二维油画。"},
	{"动漫插画", "使用清晰线条、概括的色块和柔和赛璐璐光影，呈现二维动漫插画。"},
	{"扁平插画", "使用简洁轮廓、平整色块和少量阴影，呈现二维扁平插画。"},
	{"国风水墨", "使用水墨笔触、墨色浓淡和适度留白，呈现二维中国风水墨画。"},
	{"黑白线稿", "转换为纯黑白线稿：白底、黑色轮廓和必要细节线条，不使用任何彩色、灰度阴影或彩色填充，适合印刷和上色。"},
	{"复古版画", "使用版画式刻线、有限色彩和轻微复古纸感，呈现二维复古版画。"},
}

var doubaoOutputRatios = []string{"1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "2:3"}

type outputPresetSpec struct {
	label  string
	ratio  string
	width  int
	height int
}

var doubaoOutputPresets = []outputPresetSpec{
	{label: "原比例"},
	{label: "1:1（正方形）", ratio: "1:1"},
	{label: "4:3（标准横）", ratio: "4:3"},
	{label: "3:4（标准竖）", ratio: "3:4"},
	{label: "16:9（宽屏）", ratio: "16:9"},
	{label: "9:16（竖屏）", ratio: "9:16"},
	{label: "1024×1024", width: 1024, height: 1024},
	{label: "2048×2048", width: 2048, height: 2048},
	{label: "自定义尺寸"},
}

func washStyleLabels() []string {
	labels := make([]string, 0, len(washStyles))
	for _, style := range washStyles {
		labels = append(labels, style.label)
	}
	return labels
}

func washStyleInstruction(label string) string {
	for _, style := range washStyles {
		if style.label == label && style.instruction != "" {
			return "本次洗图风格：" + style.label + "。" + style.instruction +
				"只对选中的单幅主图应用此风格，封面拼版的其他格和分隔线一律不要；风格转换优先于先前的其他风格要求，包括保留原画风、笔触和纹理。尽量保留所选单幅内主体的数量、位置与关键特征；彩色风格保留画面原有背景的颜色和明暗，不将深色背景自动替换为白色，白底线稿按风格要求处理。只输出平面图案。画面四角、边缘及内部不得新增签名、花押、作者印章、版权符号、水印或类似字母的手写笔迹；去除原有署名和水印后，用周围图案与纹理自然补全，不添加新文字或实物载体。"
		}
	}
	return ""
}

func (a *App) loadWashStyle() {
	if a.store == nil {
		a.washStyle = washStyleOriginal
		return
	}
	a.washStyle = pickStr(a.store.Get("wash_style"), washStyleOriginal, washStyleLabels())
}

func (a *App) saveWashStyle() {
	if a.store == nil {
		return
	}
	a.store.Set("wash_style", a.washStyle)
}

func (a *App) loadOutputSize() {
	if a.store == nil {
		a.outputSizeMode = outputOriginal
		a.outputSizePreset = "原比例"
		return
	}
	s := a.store
	a.outputSizeMode = pickStr(s.Get("doubao_output_size_mode"), outputOriginal, []string{outputOriginal, outputRatio, outputPixels})
	a.outputRatio = s.Get("doubao_output_ratio")
	a.outputWidth = s.Get("doubao_output_width")
	a.outputHeight = s.Get("doubao_output_height")
	a.outputCrop = s.GetBool("doubao_output_crop")
	a.syncOutputSizePreset()
}

func (a *App) saveOutputSize() {
	if a.store == nil {
		return
	}
	s := a.store
	s.Set("doubao_output_size_mode", a.outputSizeMode)
	s.Set("doubao_output_ratio", a.outputRatio)
	s.Set("doubao_output_width", a.outputWidth)
	s.Set("doubao_output_height", a.outputHeight)
	s.SetInt("doubao_output_crop", boolInt(a.outputCrop))
}

func (a *App) syncOutputSizePreset() {
	switch a.outputSizeMode {
	case outputRatio:
		for _, p := range doubaoOutputPresets {
			if p.ratio != "" && p.ratio == a.outputRatio {
				a.outputSizePreset = p.label
				return
			}
		}
		a.outputSizePreset = "自定义尺寸"
	case outputPixels:
		w, _ := strconv.Atoi(strings.TrimSpace(a.outputWidth))
		h, _ := strconv.Atoi(strings.TrimSpace(a.outputHeight))
		for _, p := range doubaoOutputPresets {
			if p.width > 0 && p.width == w && p.height == h {
				a.outputSizePreset = p.label
				return
			}
		}
		a.outputSizePreset = "自定义尺寸"
	default:
		a.outputSizePreset = "原比例"
	}
}

func (a *App) applyOutputSizePreset(label string) {
	a.outputSizePreset = label
	for _, p := range doubaoOutputPresets {
		if p.label != label {
			continue
		}
		switch {
		case p.ratio != "":
			a.outputSizeMode = outputRatio
			a.outputRatio = p.ratio
		case p.width > 0 && p.height > 0:
			a.outputSizeMode = outputPixels
			a.outputWidth = strconv.Itoa(p.width)
			a.outputHeight = strconv.Itoa(p.height)
		case label == "自定义尺寸":
			if a.outputSizeMode == outputOriginal {
				a.outputSizeMode = outputPixels
			}
		default:
			a.outputSizeMode = outputOriginal
		}
		return
	}
}

func (a *App) doubaoOutputSize() tasks.OutputSize {
	w, _ := strconv.Atoi(strings.TrimSpace(a.outputWidth))
	h, _ := strconv.Atoi(strings.TrimSpace(a.outputHeight))
	switch a.outputSizeMode {
	case outputRatio:
		return tasks.OutputSize{Ratio: strings.TrimSpace(a.outputRatio), Crop: a.outputCrop}
	case outputPixels:
		return tasks.OutputSize{Width: w, Height: h, Crop: a.outputCrop}
	default:
		return tasks.OutputSize{}
	}
}

// ---- Page switching & session management ----

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

func (a *App) syncPage() {
	if !isWorkPage(a.tab) || a.statePage == a.tab {
		return
	}
	if a.parked == nil {
		a.parked = map[int]pageState{}
	}
	a.parked[a.statePage] = a.capturePage()
	next, ok := a.parked[a.tab]
	if !ok {
		next = pageState{srcSel: -1}
		if a.tab == pageWash {
			next.prompts = a.defaultWashPrompt()
		} else if a.tab == pageFission {
			next.prompts = DefaultFissionPrompt
		}
	}
	a.statePage = a.tab
	a.restorePage(next)
}

func (a *App) stateOf(page int) pageState {
	a.syncPage()
	if page == a.statePage {
		return a.capturePage()
	}
	if s, ok := a.parked[page]; ok {
		return s
	}
	s := pageState{srcSel: -1}
	if page == pageWash {
		s.prompts = a.defaultWashPrompt()
	} else if page == pageFission {
		s.prompts = DefaultFissionPrompt
	}
	return s
}

func (a *App) onPage(page int, fn func()) {
	if fn == nil {
		return
	}
	a.syncPage()
	if page == a.statePage {
		fn()
		return
	}
	prev := a.statePage
	if a.parked == nil {
		a.parked = map[int]pageState{}
	}
	a.parked[prev] = a.capturePage()
	cur, ok := a.parked[page]
	if !ok {
		cur = pageState{srcSel: -1}
		if page == pageWash {
			cur.prompts = a.defaultWashPrompt()
		} else if page == pageFission {
			cur.prompts = DefaultFissionPrompt
		}
	}
	a.statePage = page
	a.restorePage(cur)
	fn()
	a.parked[page] = a.capturePage()
	a.statePage = prev
	a.restorePage(a.parked[prev])
}

func isWorkPage(tab int) bool {
	return tab == pageHome || tab == pageWash || tab == pageFission
}

func (a *App) washing() bool    { return a.statePage == pageWash }
func (a *App) fissioning() bool { return a.statePage == pageFission }

func pageLabel(page int) string {
	switch page {
	case pageWash:
		return "批量洗图"
	case pageFission:
		return "裂变图片"
	default:
		return "自由生图"
	}
}

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

func promptMeansContent(page int, rawPrompts string) bool {
	p := strings.TrimSpace(rawPrompts)
	if page == pageWash {
		return p != "" && p != DefaultWashPrompt && p != prompts.DoubaoWashPrompt
	}
	if page == pageFission {
		return p != "" && p != DefaultFissionPrompt
	}
	return p != ""
}

func (a *App) workspaceHasContent(page int) bool {
	st := a.stateOf(page)
	return len(st.sources) > 0 || promptMeansContent(page, st.prompts) || len(st.queue) > 0
}

func (a *App) persistWorkspace(page int) bool {
	if a == nil || a.store == nil {
		return false
	}
	if a.persistDeferral > 0 {
		if a.dirtyWorkspace == nil {
			a.dirtyWorkspace = map[int]struct{}{}
		}
		a.dirtyWorkspace[page] = struct{}{}
		return true
	}
	return a.writeWorkspace(page)
}

func (a *App) writeDirtyWorkspaces() {
	if a == nil || a.store == nil || len(a.dirtyWorkspace) == 0 {
		return
	}
	pages := make([]int, 0, len(a.dirtyWorkspace))
	for p := range a.dirtyWorkspace {
		pages = append(pages, p)
	}
	clear(a.dirtyWorkspace)
	for _, p := range pages {
		a.writeWorkspace(p)
	}
}

func (a *App) writeWorkspace(page int) bool {
	a.workspaceWrites++
	st := a.stateOf(page)
	sess := store.Session{
		ID:       a.sessionID(page),
		Page:     workspacePageKey(page),
		Prompts:  st.prompts,
		Sources:  a.washSourcesForSave(page, st),
		Progress: st.progress,
		ProgText: st.progText,
		Updated:  time.Now().Unix(),
	}
	items := a.tasksForSession(page, a.tasksToSave(page))
	id, err := a.store.SaveActive(sess, items)
	if err != nil {
		logging.Log.Warning("保存工作区失败: %v", err)
		return false
	}
	a.setSessionID(page, id)
	return true
}

func (a *App) washSourcesForSave(page int, st pageState) []store.SessionSource {
	if page != pageWash {
		out := make([]store.SessionSource, 0, len(st.sources))
		for _, si := range st.sources {
			out = append(out, store.SessionSource{Path: si.Path})
		}
		return out
	}
	out := make([]store.SessionSource, 0, len(st.sources))
	for _, si := range st.sources {
		out = append(out, store.SessionSource{
			Path:   si.Path,
			Washed: si.Washed,
			Output: si.WashOutput,
		})
	}
	return out
}

func (a *App) restoreWorkspace(page int) {
	if a == nil || a.store == nil {
		return
	}
	sess, items, ok, err := a.store.ActiveSession(workspacePageKey(page))
	if err != nil || !ok {
		return
	}
	a.applySession(page, sess, items, false)
}

func (a *App) applySession(page int, sess store.Session, items []store.SessionTask, resetPick bool) {
	a.setSessionID(page, sess.ID)
	if page == pageWash {
		a.onPage(page, func() { a.applyWashSession(sess, items) })
		return
	}
	q := queueFromSession(items)
	if page == pageHome {
		if resetPick {
			a.promptPick = ""
			a.promptChoice = ""
			a.genPromptChoice = choosePromptLabel
		}
	}
	a.onPage(page, func() {
		a.prompts = sess.Prompts
		if page == pageFission && strings.TrimSpace(a.prompts) == "" {
			a.prompts = DefaultFissionPrompt
		}
		if len(sess.Sources) > 0 {
			a.sources = make([]sourceImage, 0, len(sess.Sources))
			for _, src := range sess.Sources {
				if data, err := os.ReadFile(src.Path); err == nil {
					a.sources = append(a.sources, sourceImage{Path: src.Path, Hash: images.SHA256(data)})
				} else {
					a.sources = append(a.sources, sourceImage{Path: src.Path})
				}
			}
		}
		a.srcSel = -1
		a.queue = q
		a.progress = sess.Progress
		a.progText = sess.ProgText
	})
}

func (a *App) applyWashSession(sess store.Session, items []store.SessionTask) {
	a.prompts = sess.Prompts
	if strings.TrimSpace(a.prompts) == "" {
		a.prompts = a.defaultWashPrompt()
	}
	a.washPromptChoice = choosePromptLabel
	if len(sess.Sources) > 0 {
		a.sources = make([]sourceImage, 0, len(sess.Sources))
		for _, src := range sess.Sources {
			a.sources = append(a.sources, sourceImage{
				Path:       src.Path,
				Washed:     src.Washed,
				WashOutput: src.Output,
			})
		}
	}
	a.srcSel = -1
	a.queue = queueFromSession(items)
	a.applyWashQueue(a.queue)
	a.progress = sess.Progress
	a.progText = sess.ProgText
}

func (a *App) newWorkspace() {
	if a.running {
		return
	}
	page := a.statePage
	if a.store != nil && a.workspaceHasContent(page) {
		a.persistWorkspace(page)
		if err := a.store.ArchiveSession(a.sessionID(page)); err != nil {
			logging.Log.Warning("归档当前工作区失败: %v", err)
			return
		}
	}
	a.clearWorkspace(page)
	a.historyOpen = false
	a.emit("workbench")
}

func (a *App) clearWorkspace(page int) {
	a.setSessionID(page, 0)
	a.onPage(page, func() {
		a.sources = nil
		a.srcSel = -1
		a.queue = nil
		a.progress = 0
		a.progText = ""
		a.skipImages = false
		switch page {
		case pageWash:
			a.prompts = a.defaultWashPrompt()
			a.washPromptChoice = choosePromptLabel
		case pageFission:
			a.prompts = DefaultFissionPrompt
		default:
			a.prompts = ""
			a.freeformPrompts = ""
			a.promptPick = ""
			a.promptChoice = ""
			a.genPromptChoice = choosePromptLabel
		}
	})
	a.dropStamps(page)
}

func (a *App) reopenSession(id int64) {
	if a.running || a.store == nil || id <= 0 {
		return
	}
	page := a.statePage
	if id == a.sessionID(page) {
		a.persistWorkspace(page)
		a.historyOpen = false
		return
	}
	sess, items, err := a.store.Session(id)
	if err != nil {
		logging.Log.Warning("读取历史会话失败: %v", err)
		return
	}
	if !a.persistWorkspace(page) {
		return
	}
	if err := a.store.ActivateSession(id); err != nil {
		logging.Log.Warning("切换历史会话失败: %v", err)
		return
	}
	a.applySession(page, sess, items, true)
	a.historyOpen = false
}

func (a *App) deleteSession(id int64) {
	if a.store == nil || id <= 0 {
		return
	}
	if err := a.store.DeleteSession(id); err != nil {
		logging.Log.Warning("删除历史会话失败: %v", err)
	}
}

func (a *App) tasksToSave(page int) []tasks.Task {
	if a.running && a.runPage == page && a.engine != nil {
		if view := a.engine.View(); view != nil {
			items := view.Snapshot()
			if page == pageFission && len(a.fissionRetryBase) > 0 {
				return mergeFissionRetry(a.fissionRetryBase, items)
			}
			return items
		}
	}
	return a.stateOf(page).queue
}

func (a *App) tasksForSession(page int, items []tasks.Task) []store.SessionTask {
	out := make([]store.SessionTask, 0, len(items))
	for ord, t := range items {
		outputs := joinPaths(t.Results)
		source := taskSource(t)
		key := stampKey(page, ord, string(t.Status), source, outputs)
		finishedAt := a.stampAt(key)
		acct := a.sessionAccountName(t)
		out = append(out, store.SessionTask{
			Ord:          ord,
			Platform:     t.Platform,
			AccountID:    t.BrowserID,
			AccountName:  acct,
			Prompt:       t.Prompt,
			Source:       source,
			Status:       string(t.Status),
			Outputs:      outputs,
			Error:        t.Error,
			ElapsedMS:    t.Elapsed.Milliseconds(),
			At:           finishedAt,
			Undispatched: t.Undispatched,
		})
	}
	a.rememberStamps(page, out)
	return out
}

func (a *App) sessionAccountName(task tasks.Task) string {
	if name := strings.TrimSpace(task.AccountName); name != "" {
		return name
	}
	if task.BrowserID > 0 {
		for _, b := range a.configuredBrowsers() {
			if b.ID == task.BrowserID {
				return accountLabel(b)
			}
		}
	}
	return ""
}

func queueFromSession(items []store.SessionTask) []tasks.Task {
	out := make([]tasks.Task, 0, len(items))
	for i, item := range items {
		t := tasks.Task{
			Index:        i + 1,
			Platform:     item.Platform,
			BrowserID:    item.AccountID,
			AccountName:  item.AccountName,
			Prompt:       item.Prompt,
			Status:       tasks.Status(item.Status),
			Results:      splitLines(item.Outputs),
			Error:        item.Error,
			Elapsed:      time.Duration(item.ElapsedMS) * time.Millisecond,
			Undispatched: item.Undispatched,
		}
		if item.Source != "" {
			t.Images = []string{item.Source}
		}
		out = append(out, t)
	}
	return out
}

func stampKey(page, ord int, status, source, outputs string) string {
	return fmt.Sprintf("%d:%d:%s:%s:%s", page, ord, status, source, outputs)
}

func (a *App) stampAt(key string) int64 {
	if a.stamps != nil {
		if ts, ok := a.stamps[key]; ok {
			return ts
		}
	}
	return time.Now().Unix()
}

func (a *App) rememberStamps(page int, items []store.SessionTask) {
	if a.stamps == nil {
		a.stamps = map[string]int64{}
	}
	for _, it := range items {
		key := stampKey(page, it.Ord, it.Status, it.Source, it.Outputs)
		a.stamps[key] = it.At
	}
}

func (a *App) dropStamps(page int) {
	prefix := fmt.Sprintf("%d:", page)
	for k := range a.stamps {
		if strings.HasPrefix(k, prefix) {
			delete(a.stamps, k)
		}
	}
}

// ---- Wash & Fission helpers ----

func (a *App) defaultWashPrompt() string {
	if a.selectedPlatform() == "豆包" {
		return prompts.DoubaoWashPrompt
	}
	return DefaultWashPrompt
}

func (a *App) washPrompt() string {
	p := strings.TrimSpace(a.prompts)
	if p == "" {
		return a.defaultWashPrompt()
	}
	return p
}

func (a *App) fissionPrompt() string {
	p := strings.TrimSpace(a.prompts)
	if p == "" {
		return DefaultFissionPrompt
	}
	return p
}

func (a *App) washSoloPaths() []string {
	st := a.stateOf(pageWash)
	lastModeration := make(map[string]bool, len(st.queue))
	for _, item := range st.queue {
		if len(item.Images) == 1 {
			lastModeration[filepath.Clean(item.Images[0])] = tasks.ClassifyFailure(item.Status, item.Error).Class == tasks.FailModeration
		}
	}
	var paths []string
	for _, si := range st.sources {
		if !si.Washed && si.Path != "" && lastModeration[filepath.Clean(si.Path)] {
			paths = append(paths, si.Path)
		}
	}
	return paths
}

func (a *App) washPendingPaths() []string {
	var paths []string
	for _, si := range a.sources {
		if !si.Washed && si.Path != "" {
			paths = append(paths, si.Path)
		}
	}
	return paths
}

func (a *App) beginWashRun() {
	for i := range a.sources {
		if !a.sources[i].Washed {
			a.sources[i].Washing = true
		}
	}
}

func (a *App) applyWashTask(task tasks.Task) {
	src := taskSource(task)
	if src == "" {
		return
	}
	for i := range a.sources {
		if filepath.Clean(a.sources[i].Path) != filepath.Clean(src) {
			continue
		}
		switch {
		case isTaskActiveStatus(task.Status):
			a.sources[i].Washing = true
		case task.Status == tasks.StatusSuccess:
			a.sources[i].Washing = false
			a.sources[i].Washed = true
			if len(task.Results) > 0 {
				a.sources[i].WashOutput = task.Results[0]
			}
		default:
			a.sources[i].Washing = false
		}
	}
}

func (a *App) applyWashQueue(queue []tasks.Task) {
	byPath := map[string]tasks.Task{}
	for _, t := range queue {
		if src := taskSource(t); src != "" {
			byPath[filepath.Clean(src)] = t
		}
	}
	for i := range a.sources {
		t, ok := byPath[filepath.Clean(a.sources[i].Path)]
		if !ok {
			a.sources[i].Washing = false
			continue
		}
		a.applyWashTask(t)
	}
}

func mergeFissionRetry(base, retry []tasks.Task) []tasks.Task {
	out := append([]tasks.Task(nil), base...)
	for _, next := range retry {
		for i := range out {
			if out[i].Platform == next.Platform && out[i].Prompt == next.Prompt {
				next.Index = out[i].Index
				out[i] = next
				break
			}
		}
	}
	return out
}

func sameFissionSource(a, b string) bool {
	return filepath.Clean(strings.TrimSpace(a)) == filepath.Clean(strings.TrimSpace(b))
}

func (a *App) promptExtras(page int, parts *promptParts) string {
	effective := a.effectivePromptParts(page, parts)
	extra := strings.TrimSpace(effective.text())
	if page == pageWash {
		style := washStyleInstruction(a.washStyle)
		if style != "" && extra != "" {
			return style + "\n" + extra
		}
		if style != "" {
			return style
		}
	}
	return extra
}

func (a *App) effectivePromptParts(page int, parts *promptParts) promptParts {
	if parts == nil {
		return promptParts{}
	}
	out := *parts
	if page == pageWash && a.washStyle != "" && a.washStyle != washStyleOriginal {
		// 特定洗图风格优先于冲突条目时保留用户明确开启项。
		_ = pageWash
	}
	return out
}

// ---- Platform & Quota helpers ----

func (a *App) selectedPlatform() string {
	switch {
	case a.platDoubao:
		return "豆包"
	case a.platQianwen:
		return "千问"
	case a.platAntigravity:
		return agy.Platform
	case a.platChatGPT:
		return "ChatGPT"
	default:
		return "豆包"
	}
}

func (a *App) setSinglePlatform(platform string) bool {
	switch platform {
	case "豆包", "千问", agy.Platform, "ChatGPT":
		previousDefault := a.defaultWashPrompt()
		a.platDoubao = platform == "豆包"
		a.platQianwen = platform == "千问"
		a.platAntigravity = platform == agy.Platform
		a.platChatGPT = platform == "ChatGPT"
		a.platformAuto = false
		// Switch only untouched defaults; custom requirements belong to the user.
		if a.statePage == pageWash {
			if prompts.SameText(a.prompts, previousDefault) {
				a.prompts = a.defaultWashPrompt()
			}
		} else if wash, ok := a.parked[pageWash]; ok && prompts.SameText(wash.prompts, previousDefault) {
			wash.prompts = a.defaultWashPrompt()
			a.parked[pageWash] = wash
		}
		return true
	default:
		return false
	}
}

func (a *App) selectedPlatforms() []string {
	return []string{a.selectedPlatform()}
}

func (a *App) activePlatforms() []string {
	return a.selectedPlatforms()
}

func (a *App) toggleStage(platform string) {
	if a.running {
		return
	}
	a.setSinglePlatform(platform)
}

func (a *App) stageEnabled(platform string) bool {
	switch platform {
	case "豆包":
		return a.platDoubao
	case "千问":
		return a.platQianwen
	case agy.Platform:
		return a.platAntigravity
	case "ChatGPT":
		return a.platChatGPT
	}
	return false
}

func (a *App) shownDailyCap() int {
	return clampInt(int(a.dailyCap), 0, 200)
}

func (a *App) shownDailyCapFor(platform string) int {
	switch platform {
	case "千问":
		if a.dailyCapQianwenSet {
			return clampInt(int(a.dailyCapQianwen), 0, 200)
		}
	case agy.Platform:
		if a.dailyCapAgySet {
			return clampInt(int(a.dailyCapAgy), 0, 200)
		}
	case "ChatGPT":
		if a.dailyCapChatGPTSet {
			return clampInt(int(a.dailyCapChatGPT), 0, 200)
		}
	}
	return a.shownDailyCap()
}

func (a *App) dailyCaps() map[string]int {
	return map[string]int{
		"豆包":         a.shownDailyCap(),
		"千问":         a.shownDailyCapFor("千问"),
		agy.Platform: a.shownDailyCapFor(agy.Platform),
		"ChatGPT":    a.shownDailyCapFor("ChatGPT"),
	}
}

func (a *App) taskConcurrency() int {
	if a.concurrencyAuto {
		return 0
	}
	return 1
}

// ---- Workbench task execution ----

func (a *App) workbenchPlan(rows []store.Browser) WorkbenchPlan {
	promptList := a.taskPromptList()
	var sources []string
	if a.statePage == pageWash {
		sources = a.washPendingPaths()
	} else if a.statePage != pageHome || a.batchMode == batchImage {
		sources = a.batchImages()
	}
	var snaps []AccountSnap
	for _, b := range rows {
		snaps = append(snaps, AccountSnap{
			Platform:  b.Platform,
			LoggedIn:  b.IsLoggedIn,
			Extension: b.ExtAccount != "",
			Disabled:  b.Disabled,
			Health:    b.Health,
			CoolUntil: b.CoolUntil,
		})
	}
	return PlanWorkbench(WorkbenchInput{
		Running:         a.running,
		HasImages:       len(sources) > 0,
		HasPrompts:      len(promptList) > 0,
		AutoPlatform:    false,
		OnlyPlatform:    a.selectedPlatform(),
		WantDoubao:      a.platDoubao,
		WantQianwen:     a.platQianwen,
		WantAntigravity: a.platAntigravity,
		WantChatGPT:     a.platChatGPT,
		ExtensionMode:   a.extensionTasks(),
		Accounts:        snaps,
		Now:             time.Now(),
	})
}

func (a *App) startTasks() {
	if a.renameUpdateActive() || len(a.browserPending) > 0 {
		return
	}
	plan := a.workbenchPlan(a.configuredBrowsers())
	if plan.Action != StartGenerate {
		return
	}
	paths := a.batchImages()
	if a.washing() {
		paths = a.washPendingPaths()
	} else if a.statePage == pageHome && a.batchMode == batchText {
		paths = nil
	}
	a.runQueue(plan.Platforms, a.taskPromptList(), paths, nil)
}

func (a *App) retryTasks(items []tasks.Task) {
	if a.running || len(items) == 0 || a.engine == nil {
		return
	}
	if a.fissioning() {
		if len(a.sources) != 1 {
			a.progText = "请先添加该结果对应的母图"
			return
		}
		for _, task := range items {
			if len(task.Images) != 1 || !sameFissionSource(task.Images[0], a.sources[0].Path) {
				a.progText = "请切换到该结果对应的母图再重新生成"
				return
			}
		}
	}
	var platforms []string
	seenPlat := map[string]bool{}
	var promptList []string
	for _, t := range items {
		if !seenPlat[t.Platform] {
			seenPlat[t.Platform] = true
			platforms = append(platforms, t.Platform)
		}
		promptList = append(promptList, t.Prompt)
	}
	var paths []string
	if a.statePage == pageFission && len(a.sources) == 1 {
		paths = []string{a.sources[0].Path}
	}
	a.runQueue(platforms, promptList, paths, items)
}

func (a *App) runQueue(platforms, promptList, paths []string, retry []tasks.Task) {
	if a.engine == nil || a.running || a.renameUpdateActive() || len(a.browserPending) > 0 {
		return
	}
	if retry == nil && !a.fissioning() {
		effective := a.effectivePromptParts(a.statePage, a.currentPromptParts())
		if err := effective.validate(); err != nil {
			a.progText = err.Error()
			logging.Log.Errorf("%s", a.progText)
			return
		}
	}
	for _, platform := range platforms {
		if platform == "豆包" {
			if err := a.doubaoOutputSize().Validate(); err != nil {
				a.progText = "豆包输出尺寸：" + err.Error()
				logging.Log.Errorf("%s", a.progText)
				return
			}
			break
		}
	}
	if !a.extensionTasks() {
		rows := a.configuredBrowsers()
		for _, platform := range platforms {
			if platform != "豆包" && platform != "千问" && platform != "ChatGPT" {
				continue
			}
			kinds := browser.AccountEngines(rows, platform, a.browserBackend)
			ready := false
			for _, kind := range kinds {
				if _, ready = browser.InstalledRuntime(browser.ConfigDir(), kind); ready {
					break
				}
			}
			if !ready {
				if !slices.Contains(kinds, browser.CanonicalBackend(a.browserBackend)) {
					a.progText = "请在「浏览器」页选择「" + kinds[0] + " 账号环境」并安装，再返回重试"
					return
				}
				a.refreshDetected()
				if !a.camoufoxBusy && browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, a.browserBackend) {
					a.camoufoxStatus = "请先安装 " + browser.CanonicalBackend(a.browserBackend) + "，再返回任务页重试"
				}
				a.tab = pageBrowser
				a.emit("shell", "browsers")
				return
			}
		}
	}
	dir, err := EnsureOutputDir(DefaultOutputDir(a.outputPath, mustWd()))
	if err != nil {
		a.progText = HumanReason(err.Error())
		logging.Log.Errorf("创建输出文件夹失败: %v", err)
		return
	}
	if strings.TrimSpace(a.outputPath) == "" {
		a.outputPath = dir
	}
	a.saveSettings()
	opts := a.taskOptions(platforms, promptList, paths)
	opts.OutputPath = dir
	page := a.statePage
	previousView := a.engine.View()
	previous := a.stateOf(page)
	a.fissionRetryBase = nil
	if page == pageFission && retry != nil {
		a.fissionRetryBase = append([]tasks.Task(nil), previous.queue...)
	}
	previousRunPage, previousUseRunView := a.runPage, a.useRunView
	a.runPage = page
	a.running = true
	a.useRunView = false
	a.resultsDoneOpen = false
	if page == pageWash {
		a.onPage(page, func() { a.beginWashRun() })
	}
	if retry != nil {
		pending := tasks.RebuildRetry(retry)
		if page == pageFission {
			pending = mergeFissionRetry(previous.queue, pending)
		}
		a.onPage(page, func() {
			a.queue = pending
			a.progress = 0
			if len(pending) > 0 {
				a.progText = fmt.Sprintf("0 / %d", len(pending))
			} else {
				a.progText = ""
			}
		})
	} else {
		a.progress = 0
		a.progText = ""
	}
	if !a.persistWorkspace(page) {
		a.onPage(page, func() {
			a.queue, a.progress = previous.queue, previous.progress
			a.progText = "保存任务记录失败，未开始任务，请检查配置数据库"
		})
		a.running = false
		a.fissionRetryBase = nil
		a.runPage, a.useRunView = previousRunPage, previousUseRunView
		return
	}
	a.engine.SetSolver(captcha.Build(a.captchaConfig()))
	a.emit("workbench", "shell")
	go func() {
		onTask := func(task tasks.Task) {
			a.postUI(func() {
				a.useRunView = true
				if page == pageWash {
					a.onPage(page, func() { a.applyWashTask(task) })
				}
				a.persistWorkspace(page)
				a.emit("workbench")
			})
		}
		onProgress := func(p tasks.Progress) {
			a.postUI(func() {
				a.useRunView = true
				a.onPage(page, func() {
					if p.Total > 0 {
						a.progress = float64(p.Done) / float64(p.Total)
					}
					a.progText = fmt.Sprintf("%d / %d", p.Done, p.Total)
				})
				a.persistWorkspace(page)
				a.emit("workbench")
			})
		}
		var runErr error
		if retry != nil {
			runErr = a.engine.Retry(context.Background(), opts, retry, onTask, onProgress)
		} else {
			runErr = a.engine.Run(context.Background(), opts, onTask, onProgress)
		}
		var checked []tasks.Task
		if page == pageFission {
			if view := a.engine.View(); view != nil && view != previousView {
				checked = view.Snapshot()
				if retry != nil {
					checked = mergeFissionRetry(previous.queue, checked)
				}
				checked = tasks.ReviewFissionResults(checked, retry)
			}
		}
		a.postUI(func() {
			a.running = false
			a.useRunView = false
			a.fissionRetryBase = nil
			if a.browserDrawer != nil && a.manager != nil && !a.manager.IsRunning(a.browserDrawer.row.ID) {
				a.closeBrowserDrawer()
			}
			if a.manager != nil {
				for i := range a.browserRows {
					if a.browserRows[i].ExtAccount == "" {
						a.browserRows[i].IsRunning = a.manager.IsRunning(a.browserRows[i].ID)
					}
				}
			}
			a.onPage(page, func() {
				if page == pageFission && checked != nil {
					a.queue = checked
				} else if view := a.engine.View(); view != nil && view != previousView {
					a.queue = view.Snapshot()
				} else {
					a.queue, a.progress = previous.queue, previous.progress
				}
				if page == pageWash {
					a.applyWashQueue(a.queue)
				}
				if runErr != nil {
					a.progText = HumanReason(runErr.Error())
					logging.Log.Errorf("%s任务终止: %v", pageLabel(page), runErr)
				} else {
					a.progText = taskResultSummary(a.queue)
				}
			})
			a.persistWorkspace(page)
			a.maybeInstallPending()
			a.emit("workbench", "shell", "browsers")
		})
	}()
}

func (a *App) batchImages() []string {
	if len(a.sources) == 0 {
		return nil
	}
	paths := make([]string, 0, len(a.sources))
	for _, si := range a.sources {
		paths = append(paths, si.Path)
	}
	return paths
}

func (a *App) addLoaded(page int, loaded []sourceImage) {
	if len(loaded) == 0 {
		return
	}
	a.onPage(page, func() {
		if page == pageFission {
			a.sources = loaded[:1]
			a.srcSel = 0
		} else {
			a.appendSources(loaded, page == pageWash)
		}
	})
	a.persistWorkspace(page)
	a.emit("workbench")
}

func taskResultSummary(queue []tasks.Task) string {
	tally := TallyTasks(queue)
	summary := fmt.Sprintf("成功 %d · 失败 %d · 待验证 %d", tally.Done, tally.Failed, tally.Needs)
	if tally.Review > 0 {
		summary += fmt.Sprintf(" · 需核对 %d", tally.Review)
	}
	return summary + fmt.Sprintf(" · 未完成 %d", tally.Incomplete())
}

// ---- Result grouping, lanes & status helpers ----

type resultTile struct {
	key  string
	task tasks.Task
	path string
}

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

func (a *App) currentQueueItems() []tasks.Task {
	if a.running && a.runPage == a.statePage && a.useRunView && a.engine != nil {
		if view := a.engine.View(); view != nil {
			items := view.Snapshot()
			if a.statePage == pageFission && len(a.fissionRetryBase) > 0 {
				return mergeFissionRetry(a.fissionRetryBase, items)
			}
			return items
		}
	}
	return a.queue
}

func (a *App) queueItems() []tasks.Task {
	return a.currentQueueItems()
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
			activeRows = append(activeRows, resultRow{key: "h:" + key, header: true, title: b.title, summary: summary})
			for i := 0; i < len(b.tiles); i += cols {
				end := min(i+cols, len(b.tiles))
				activeRows = append(activeRows, resultRow{key: fmt.Sprintf("r:%s:%d", key, i), tiles: b.tiles[i:end]})
			}
		}
	}
	if len(issueTiles) > 0 {
		issueRows = append(issueRows, resultRow{key: "h:issues", header: true, title: "待处理", summary: fmt.Sprintf("%d 项", len(issueTiles))})
		for _, tile := range issueTiles {
			issueRows = append(issueRows, resultRow{key: "i:" + tile.key, issue: true, tiles: []resultTile{tile}})
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
	return []resultTile{{key: fmt.Sprintf("a-%d-%d-%s", task.Index, pos, task.Status), task: task, path: path}}
}

func taskIssueTile(task tasks.Task, pos int) resultTile {
	path := ""
	if len(task.Results) > 0 {
		path = task.Results[0]
	}
	return resultTile{key: fmt.Sprintf("e-%d-%d-%s", task.Index, pos, task.Status), task: task, path: path}
}

func taskDoneTiles(task tasks.Task, pos int) []resultTile {
	if len(task.Results) == 0 {
		return []resultTile{{key: fmt.Sprintf("d-%d-%d", task.Index, pos), task: task}}
	}
	tiles := make([]resultTile, 0, len(task.Results))
	for i, path := range task.Results {
		tiles = append(tiles, resultTile{key: fmt.Sprintf("p-%d-%d-%d", task.Index, pos, i), task: task, path: path})
	}
	return tiles
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

func (a *App) accountBoard(rows []store.Browser) tasks.Board {
	if a == nil {
		return tasks.Board{}
	}
	caps := tasks.CapsFrom(a.dailyCaps(), tasks.DefaultDailyCap)
	var board tasks.Board
	if a.engine != nil {
		board = a.engine.AccountBoardCaps(time.Now(), caps)
	} else {
		board = tasks.BoardFromCaps(a.laneBrowsers(), a.queue, caps, time.Now())
	}
	return a.attachAgyLanes(board, rows)
}

func (a *App) laneBrowsers() []store.Browser {
	rows := a.configuredBrowsers()
	if a.extensionTasks() {
		return rows
	}
	out := make([]store.Browser, 0, len(rows))
	for _, b := range rows {
		if b.ExtAccount == "" {
			out = append(out, b)
		}
	}
	return out
}

func laneAccountTitle(ln tasks.AccountLane) string {
	if ln.Platform == agy.Platform && strings.TrimSpace(ln.Email) != "" {
		return strings.TrimSpace(ln.Email)
	}
	if name := strings.TrimSpace(ln.Name); name != "" {
		return name
	}
	return ln.Platform
}

func (a *App) replaceCardImage(taskIndex int, replacementPath string) error {
	replacementPath = strings.TrimSpace(replacementPath)
	if replacementPath == "" || a.running {
		return nil
	}
	items := a.queue
	for i := range items {
		if items[i].Index != taskIndex {
			continue
		}
		src := taskSource(items[i])
		if src == "" {
			continue
		}
		info, err := os.Stat(replacementPath)
		if err != nil || info.IsDir() {
			return fmt.Errorf("找不到替换图片")
		}
		dst := src
		if len(items[i].Results) > 0 && filePresent(items[i].Results[0]) {
			dst = items[i].Results[0]
		} else {
			outDir, err := EnsureOutputDir(DefaultOutputDir(a.outputPath, mustWd()))
			if err != nil {
				return err
			}
			dst = filepath.Join(outDir, filepath.Base(src))
		}
		data, err := os.ReadFile(replacementPath)
		if err != nil {
			return err
		}
		if !images.IsValidImageBytes(data) {
			return fmt.Errorf("所选文件不是有效图片")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
		items[i].Results = []string{dst}
		items[i].Status = tasks.StatusSuccess
		items[i].Error = ""
		if a.statePage == pageWash {
			a.applyWashTask(items[i])
		}
		a.persistWorkspace(a.statePage)
		a.emit("workbench")
		return nil
	}
	return nil
}
