package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/tsx"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getWorkbenchState":       getWorkbenchStateBridge,
		"setWorkbenchField":       setWorkbenchFieldBridge,
		"setPlatformSelected":     setPlatformSelectedBridge,
		"setOutputSize":           setOutputSizeBridge,
		"addWorkbenchSources":     addWorkbenchSourcesBridge,
		"pickWorkbenchFiles":      pickWorkbenchFilesBridge,
		"pickWorkbenchFolder":     pickWorkbenchFolderBridge,
		"removeWorkbenchSource":   removeWorkbenchSourceBridge,
		"clearWorkbenchSources":   clearWorkbenchSourcesBridge,
		"selectWorkbenchSource":   selectWorkbenchSourceBridge,
		"startTasks":              startTasksBridge,
		"stopTasks":               stopTasksBridge,
		"retryTask":               retryTaskBridge,
		"retryAllFailed":          retryAllFailedBridge,
		"openOutputFolder":        openOutputFolderBridge,
		"replaceResultImage":      replaceResultImageBridge,
		"sessionAction":           sessionActionBridge,
		"getPromptPartsState":     getPromptPartsBridge,
		"setPromptPart":           setPromptPartBridge,
		"applyWorkbenchPrompt":    applyWorkbenchPromptBridge,
		"setResultsDoneOpen":      setResultsDoneOpenBridge,
		"setHistoryOpen":          setHistoryOpenBridge,
		"setPreviewPath":          setPreviewPathBridge,
		"setGalleryOpen":          setGalleryOpenBridge,
		"toggleGallerySelect":     toggleGallerySelectBridge,
		"setAllGallerySelect":     setAllGallerySelectBridge,
		"removeGallerySourceItem": removeGallerySourceBridge,
	})
}

type SourceItemDTO struct {
	Index      int
	Path       string
	Name       string
	FileURL    string
	Washed     bool
	Washing    bool
	WashOutput string
	OutputURL  string
	Selected   bool
}

type ResultTileDTO struct {
	Key        string
	TaskIndex  int
	Title      string
	Status     string
	Path       string
	FileURL    string
	SrcURL     string
	Platform   string
	Elapsed    string
	Prompt     string
	Rewritten  bool
	Reason     string
	ActionText string
	CanReplace bool
}

type ResultRowDTO struct {
	Key     string
	Header  bool
	Issue   bool
	Title   string
	Summary string
	Tiles   []ResultTileDTO
}

type LaneJobDTO struct {
	TaskIndex int
	Prompt    string
	SourceURL string
	Status    string
	Elapsed   string
}

type AccountLaneDTO struct {
	ID           int64
	Platform     string
	Title        string
	State        string
	Action       string
	UsedToday    int
	SuccessToday int
	FailedToday  int
	Cap          int
	ReadyInSec   int
	CooldownAt   string
	CoolUntil    int64
	UsagePct     int
	UsageLabel   string
	UsageErr     string
	Job          LaneJobDTO
	HasJob       bool
	Disabled     bool
	IsRunning    bool
	IsLoggedIn   bool
	Extension    bool
	Engine       string
	LastError    string
}

type SessionDTO struct {
	ID        int64
	Title     string
	CountText string
	When      string
	Active    bool
}

type WorkbenchStateDTO struct {
	Mode             string // free / wash / fission
	Page             int
	BatchMode        int // 0 text / 1 image
	Platforms        []string
	Doubao           bool
	Qianwen          bool
	Agy              bool
	ChatGPT          bool
	Prompts          string
	PromptChoice     string
	PromptOptions    []string
	PromptSuffix     string
	UseSuffix        bool
	WashStyle        string
	WashStyleOptions []string
	RemoveWatermark  bool
	OutputPreset     string
	OutputPresets    []string
	OutputMode       string
	OutputRatio      string
	OutputRatios     []string
	OutputWidth      string
	OutputHeight     string
	OutputCrop       bool
	Sources          []SourceItemDTO
	SourcesTotal     int
	SelectedSource   int
	SkipImages       bool
	Running          bool
	RunPage          int
	Progress         float64
	ProgText         string
	CanStart         bool
	StartLabel       string
	StartBlockedHint string
	PlanMissing      []string
	OutputNote       string
	ActiveRows       []ResultRowDTO
	IssueRows        []ResultRowDTO
	DoneTiles        []ResultTileDTO
	DoneCount        int
	DoneTasks        int
	FailedCount      int
	NeedsCount       int
	ActiveCount      int
	TotalCount       int
	ResultsDoneOpen  bool
	HistoryOpen      bool
	Sessions         []SessionDTO
	StageTab         int
	Lanes            []AccountLaneDTO
	LaneCollapsed    bool
	AgyPauseLine     string
	PreviewOn        bool
	PreviewPath      string
	PreviewURL       string
	GalleryOn        bool
	GallerySelected  int
	GalleryAll       bool
	GalleryItems     []SourceItemDTO
	StructuredOpen   bool
	StructuredCount  int
}

type PromptPartItemDTO struct {
	Key     string
	Name    string
	Enabled bool
	Value   string
}

type PromptPartsStateDTO struct {
	Open  bool
	Page  int
	Items []PromptPartItemDTO
}

func getWorkbenchStateBridge() WorkbenchStateDTO {
	a := getApp()
	if a == nil {
		return WorkbenchStateDTO{}
	}
	a.syncPage()
	mode := "free"
	switch a.statePage {
	case pageWash:
		mode = "wash"
	case pageFission:
		mode = "fission"
	}
	items := a.currentQueueItems()
	tally := TallyTasks(items)
	activeRows, issueRows, doneTiles, doneTasks := a.classifyResultRows(items, 4)

	mapTile := func(t resultTile) ResultTileDTO {
		reason, act, _ := tileFailure(t.task)
		src := taskSource(t.task)
		return ResultTileDTO{
			Key:        t.key,
			TaskIndex:  t.task.Index,
			Title:      tileTitle(t),
			Status:     tileStatus(t.task),
			Path:       t.path,
			FileURL:    fileURL(t.path),
			SrcURL:     fileURL(src),
			Platform:   t.task.Platform,
			Elapsed:    taskElapsedText(t.task, time.Now()),
			Prompt:     t.task.Prompt,
			Rewritten:  t.task.Rewritten,
			Reason:     reason,
			ActionText: act,
			CanReplace: !a.running && src != "" && t.task.Status != tasks.StatusRunning,
		}
	}
	mapRows := func(in []resultRow) []ResultRowDTO {
		out := make([]ResultRowDTO, 0, len(in))
		for _, r := range in {
			tiles := make([]ResultTileDTO, 0, len(r.tiles))
			for _, t := range r.tiles {
				tiles = append(tiles, mapTile(t))
			}
			out = append(out, ResultRowDTO{
				Key:     r.key,
				Header:  r.header,
				Issue:   r.issue,
				Title:   r.title,
				Summary: r.summary,
				Tiles:   tiles,
			})
		}
		return out
	}
	mappedDone := make([]ResultTileDTO, 0, len(doneTiles))
	for _, t := range doneTiles {
		mappedDone = append(mappedDone, mapTile(t))
	}

	srcLimit := min(len(a.sources), 6)
	srcs := make([]SourceItemDTO, 0, srcLimit)
	for i := 0; i < srcLimit; i++ {
		s := a.sources[i]
		srcs = append(srcs, SourceItemDTO{
			Index:      i,
			Path:       s.Path,
			Name:       filepath.Base(s.Path),
			FileURL:    fileURL(s.Path),
			Washed:     s.Washed,
			Washing:    s.Washing,
			WashOutput: s.WashOutput,
			OutputURL:  fileURL(s.WashOutput),
			Selected:   i == a.srcSel,
		})
	}

	// Prompt options for dropdown
	kind := prompts.KindGen
	curChoice := a.genPromptChoice
	if a.statePage == pageWash {
		kind = prompts.KindWash
		curChoice = a.washPromptChoice
	}
	opts := workspacePromptOptions(kind, a.userPrompts)
	optLabels := promptLabels(opts)
	if curChoice == "" {
		curChoice = choosePromptLabel
	}

	presetLabels := make([]string, 0, len(doubaoOutputPresets))
	for _, p := range doubaoOutputPresets {
		presetLabels = append(presetLabels, p.label)
	}

	accounts := a.configuredBrowsers()
	plan := a.workbenchPlan(accounts)
	startLabel := "开始生成"
	switch {
	case a.running:
		startLabel = "停止任务"
	case a.washing():
		startLabel = "开始洗图"
	case a.fissioning():
		startLabel = "开始裂变"
	}

	// Sessions list (only queried while history dialog is open)
	var sessList []SessionDTO
	if a.historyOpen && a.store != nil {
		curSess := a.sessionID(a.statePage)
		history, _ := a.store.History(workspacePageKey(a.statePage))
		for _, s := range history {
			title := strings.TrimSpace(shortLine(oneLine(s.Prompts), 36))
			if title == "" {
				title = "未命名草稿"
			}
			when := ""
			if s.Updated > 0 {
				when = time.Unix(s.Updated, 0).Local().Format("01-02 15:04")
			}
			sessList = append(sessList, SessionDTO{
				ID:        s.ID,
				Title:     title,
				CountText: fmt.Sprintf("%d 任务", s.Tasks),
				When:      when,
				Active:    s.ID == curSess,
			})
		}
	}

	// Account lanes
	board := a.accountBoard(accounts)
	now := time.Now()
	rowByID := make(map[int64]store.Browser, len(accounts))
	for _, b := range accounts {
		if a.manager != nil && b.ExtAccount == "" {
			b.IsRunning = a.manager.IsRunning(b.ID)
		}
		rowByID[b.ID] = b
	}
	lanes := make([]AccountLaneDTO, 0, len(board.Lanes))
	for _, ln := range board.Lanes {
		row := rowByID[ln.ID]
		dto := AccountLaneDTO{
			ID:           ln.ID,
			Platform:     ln.Platform,
			Title:        laneAccountTitle(ln),
			State:        string(ln.State),
			Action:       ln.Action,
			UsedToday:    ln.UsedToday,
			SuccessToday: ln.SuccessToday,
			FailedToday:  ln.FailedToday,
			Cap:          ln.Cap,
			CoolUntil:    ln.CoolUntil,
			Disabled:     ln.Disabled || row.Disabled,
			IsRunning:    row.IsRunning,
			IsLoggedIn:   row.IsLoggedIn,
			Extension:    ln.Extension || row.ExtAccount != "",
			Engine:       row.Engine,
			LastError:    row.LastError,
		}
		if ln.NextReadyAt.After(now) {
			dto.ReadyInSec = int(ln.NextReadyAt.Sub(now).Seconds()) + 1
		}
		if ln.CoolUntil > now.Unix() {
			dto.CooldownAt = time.Unix(ln.CoolUntil, 0).Local().Format("15:04")
		}
		if ln.Platform == agy.Platform {
			uv := a.agyUsageView(ln.ID)
			dto.UsageLabel, dto.UsagePct, dto.UsageErr = formatAgyUsageView(uv)
		}
		if ln.Job != nil {
			dto.HasJob = true
			dto.Job = LaneJobDTO{
				TaskIndex: ln.Job.Index,
				Prompt:    ln.Job.Prompt,
				SourceURL: fileURL(ln.Job.Image),
				Status:    string(ln.State),
				Elapsed:   formatElapsedDuration(now.Sub(ln.Job.Started), false),
			}
		}
		lanes = append(lanes, dto)
	}

	// Gallery items (only built while gallery modal is open)
	var galleryItems []SourceItemDTO
	gallerySelCount := 0
	if a.galleryOn {
		galleryItems = make([]SourceItemDTO, 0, len(a.sources))
		for i, s := range a.sources {
			sel := a.gallerySelected != nil && a.gallerySelected[s.Path]
			if sel {
				gallerySelCount++
			}
			galleryItems = append(galleryItems, SourceItemDTO{
				Index:    i,
				Path:     s.Path,
				Name:     filepath.Base(s.Path),
				FileURL:  fileURL(s.Path),
				Washed:   s.Washed,
				Washing:  s.Washing,
				Selected: sel,
			})
		}
	}

	// Count enabled structured prompt parts
	parts := a.currentPromptParts()
	structuredCount := 0
	for _, r := range parts.rows() {
		if r.part.Enabled {
			structuredCount++
		}
	}

	planMissing := a.missingBrowserNames(accounts)
	blockedHint := ""
	if plan.Action != StartGenerate && !a.running {
		if len(planMissing) > 0 {
			blockedHint = "缺少账号：" + strings.Join(planMissing, "、")
		} else if len(a.taskPromptList()) == 0 && len(a.batchImages()) == 0 {
			blockedHint = "请先填写提示词或添加参考图"
		} else {
			blockedHint = "暂无可用的已登录账号，请在右侧账号区登录或新建"
		}
	}

	return WorkbenchStateDTO{
		Mode:             mode,
		Page:             a.statePage,
		BatchMode:        a.batchMode,
		Platforms:        plan.Platforms,
		Doubao:           a.platDoubao,
		Qianwen:          a.platQianwen,
		Agy:              a.platAntigravity,
		ChatGPT:          a.platChatGPT,
		Prompts:          a.prompts,
		PromptChoice:     curChoice,
		PromptOptions:    optLabels,
		PromptSuffix:     a.promptSuffix,
		UseSuffix:        a.useSuffix,
		WashStyle:        a.washStyle,
		WashStyleOptions: washStyleLabels(),
		RemoveWatermark:  a.removeWatermark,
		OutputPreset:     a.outputSizePreset,
		OutputPresets:    presetLabels,
		OutputMode:       a.outputSizeMode,
		OutputRatio:      a.outputRatio,
		OutputRatios:     doubaoOutputRatios,
		OutputWidth:      a.outputWidth,
		OutputHeight:     a.outputHeight,
		OutputCrop:       a.outputCrop,
		Sources:          srcs,
		SourcesTotal:     len(a.sources),
		SelectedSource:   a.srcSel,
		SkipImages:       a.skipImages,
		Running:          a.running,
		RunPage:          a.runPage,
		Progress:         a.progress,
		ProgText:         a.progText,
		CanStart:         !a.renameUpdateActive() && len(a.browserPending) == 0 && plan.Action == StartGenerate,
		StartLabel:       startLabel,
		StartBlockedHint: blockedHint,
		PlanMissing:      planMissing,
		OutputNote:       a.outputNote,
		ActiveRows:       mapRows(activeRows),
		IssueRows:        mapRows(issueRows),
		DoneTiles:        mappedDone,
		DoneCount:        len(mappedDone),
		DoneTasks:        doneTasks,
		FailedCount:      tally.Failed,
		NeedsCount:       tally.Needs,
		ActiveCount:      tally.Running,
		TotalCount:       len(items),
		ResultsDoneOpen:  a.resultsDoneOpen,
		HistoryOpen:      a.historyOpen,
		Sessions:         sessList,
		StageTab:         a.stageTab,
		Lanes:            lanes,
		LaneCollapsed:    a.laneCollapsed,
		AgyPauseLine:     a.agyPauseLine(now),
		PreviewOn:        a.previewOn,
		PreviewPath:      a.previewPath,
		PreviewURL:       fileURL(a.previewPath),
		GalleryOn:        a.galleryOn,
		GallerySelected:  gallerySelCount,
		GalleryAll:       len(a.sources) > 0 && gallerySelCount == len(a.sources),
		GalleryItems:     galleryItems,
		StructuredOpen:   a.structuredOpen,
		StructuredCount:  structuredCount,
	}
}

func setWorkbenchFieldBridge(field, value string) {
	a := getApp()
	if a == nil {
		return
	}
	switch field {
	case "mode":
		switch value {
		case "wash":
			a.tab = pageWash
		case "fission":
			a.tab = pageFission
		default:
			a.tab = pageHome
		}
		a.syncPage()
		a.noteWorkspacePage()
	case "batchMode":
		if value == "image" {
			a.batchMode = batchImage
		} else {
			a.batchMode = batchText
		}
	case "prompts":
		if !(a.running && a.runPage == a.statePage) {
			a.prompts = value
			if a.statePage == pageHome && a.promptPick == "" {
				a.freeformPrompts = value
			}
		}
	case "resetWashPrompt":
		a.prompts = DefaultWashPrompt
	case "resetFissionPrompt":
		a.prompts = DefaultFissionPrompt
	case "washStyle":
		a.washStyle = value
		a.saveWashStyle()
	case "removeWatermark":
		a.removeWatermark = value == "true" || value == "1"
	case "useSuffix":
		a.useSuffix = value == "true" || value == "1"
	case "promptSuffix":
		a.promptSuffix = value
	case "stageTab":
		if v := parseIntDefault(value, 0); v >= 0 {
			a.stageTab = v
			a.stagePicked = true
		}
	case "laneCollapsed":
		a.laneCollapsed = value == "true" || value == "1"
	case "structuredOpen":
		a.structuredOpen = value == "true" || value == "1"
	case "skipImages":
		a.skipImages = value == "true" || value == "1"
	}
	a.emit("workbench", "shell")
}

func setPlatformSelectedBridge(platform string, _ bool) {
	a := getApp()
	if a == nil || a.running {
		return
	}
	if !a.setSinglePlatform(platform) {
		return
	}
	if a.store != nil {
		a.store.SetPlatforms(a.selectedPlatforms())
		a.store.SetInt("platform_auto", 0)
	}
}

func setOutputSizeBridge(preset, mode, ratio, width, height string, crop bool) {
	a := getApp()
	if a == nil {
		return
	}
	if preset != "" && preset != a.outputSizePreset {
		a.applyOutputSizePreset(preset)
	} else {
		if mode != "" {
			a.outputSizeMode = mode
		}
		a.outputRatio = ratio
		a.outputWidth = width
		a.outputHeight = height
		a.outputCrop = crop
		a.syncOutputSizePreset()
	}
	a.saveOutputSize()
	a.emit("workbench")
}

func addWorkbenchSourcesBridge(paths []string) {
	a := getApp()
	if a == nil {
		return
	}
	a.ingestSources(paths)
}

func pickWorkbenchFilesBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	page := a.statePage
	paths, err := a.pickFilesAsync(ctx, "选择源图片", page != pageFission, []string{"png", "jpg", "jpeg", "webp", "gif"}, "图片")
	if err != nil {
		logging.Log.Errorf("选择源图片失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		loaded := loadSourceFiles(paths)
		_ = a.onScope(ctx, func() { a.addLoaded(page, loaded) })
	}
	return nil
}

func pickWorkbenchFolderBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	page := a.statePage
	paths, err := a.pickDirsAsync(ctx, "选择图片文件夹", false)
	if err != nil {
		logging.Log.Errorf("选择图片文件夹失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		loaded := loadSourceFiles(paths)
		_ = a.onScope(ctx, func() { a.addLoaded(page, loaded) })
	}
	return nil
}

func removeWorkbenchSourceBridge(idx int) {
	a := getApp()
	if a == nil || a.running {
		return
	}
	if idx >= 0 && idx < len(a.sources) {
		a.sources = append(a.sources[:idx], a.sources[idx+1:]...)
		if a.srcSel == idx {
			a.srcSel = -1
		} else if a.srcSel > idx {
			a.srcSel--
		}
		a.persistWorkspace(a.statePage)
		a.emit("workbench")
	}
}

func clearWorkbenchSourcesBridge() {
	a := getApp()
	if a == nil || a.running {
		return
	}
	a.sources = nil
	a.srcSel = -1
	a.persistWorkspace(a.statePage)
	a.emit("workbench")
}

func selectWorkbenchSourceBridge(idx int) {
	a := getApp()
	if a == nil {
		return
	}
	if idx >= 0 && idx < len(a.sources) {
		a.srcSel = idx
	} else {
		a.srcSel = -1
	}
	a.emit("workbench")
}

func startTasksBridge() {
	a := getApp()
	if a == nil {
		return
	}
	if a.running {
		a.stopTasks()
		a.emit("workbench", "shell")
		return
	}
	a.startTasks()
}

func stopTasksBridge() {
	a := getApp()
	if a != nil {
		a.stopTasks()
		a.emit("workbench", "shell")
	}
}

func retryTaskBridge(taskIndex int) {
	a := getApp()
	if a == nil || a.running {
		return
	}
	for _, t := range a.queue {
		if t.Index == taskIndex {
			a.retryTasks([]tasks.Task{t})
			return
		}
	}
}

func retryAllFailedBridge() {
	a := getApp()
	if a == nil || a.running {
		return
	}
	var failed []tasks.Task
	for _, t := range a.queue {
		if t.Status == tasks.StatusFailed || t.Status == tasks.StatusNeedsUser || t.Status == tasks.StatusNeedsReview || (t.Status == tasks.StatusSkipped && t.Worked()) {
			failed = append(failed, t)
		}
	}
	a.retryTasks(failed)
}

func openOutputFolderBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	dir, err := EnsureOutputDir(a.resolvedOutputDir())
	if err != nil {
		a.outputNote = "无法创建输出文件夹"
		a.emit("workbench")
		return err
	}
	return a.openPath(dir)
}

func replaceResultImageBridge(ctx context.Context, taskIndex int) error {
	a := getApp()
	if a == nil || a.running {
		return nil
	}
	paths, err := a.pickFilesAsync(ctx, "选择替换图片", false, []string{"png", "jpg", "jpeg", "webp"}, "图片")
	if err != nil || len(paths) == 0 {
		return err
	}
	return a.onScope(ctx, func() {
		if repErr := a.replaceCardImage(taskIndex, paths[0]); repErr != nil {
			a.pushToast("error", repErr.Error())
		} else {
			a.pushToast("success", "已替换结果图")
		}
	})
}

func sessionActionBridge(action string, id int64) {
	a := getApp()
	if a == nil {
		return
	}
	switch action {
	case "new":
		a.newWorkspace()
	case "reopen":
		a.reopenSession(id)
	case "delete":
		a.deleteSession(id)
	}
	a.emit("workbench")
}

func getPromptPartsBridge() PromptPartsStateDTO {
	a := getApp()
	if a == nil {
		return PromptPartsStateDTO{}
	}
	parts := a.currentPromptParts()
	rows := parts.rows()
	items := make([]PromptPartItemDTO, 0, len(rows))
	keys := []string{"subject", "background", "style", "light", "layout", "text", "other"}
	for i, r := range rows {
		items = append(items, PromptPartItemDTO{
			Key:     keys[i],
			Name:    r.name,
			Enabled: r.part.Enabled,
			Value:   r.part.Value,
		})
	}
	return PromptPartsStateDTO{
		Open:  a.structuredOpen,
		Page:  a.statePage,
		Items: items,
	}
}

func setPromptPartBridge(key string, enabled bool, value string) {
	a := getApp()
	if a == nil {
		return
	}
	parts := a.currentPromptParts()
	switch key {
	case "subject":
		parts.Subject = promptPart{Enabled: enabled, Value: value}
	case "background":
		parts.Background = promptPart{Enabled: enabled, Value: value}
	case "style":
		parts.Style = promptPart{Enabled: enabled, Value: value}
	case "light":
		parts.Light = promptPart{Enabled: enabled, Value: value}
	case "layout":
		parts.Layout = promptPart{Enabled: enabled, Value: value}
	case "text":
		parts.Text = promptPart{Enabled: enabled, Value: value}
	case "other":
		parts.Other = promptPart{Enabled: enabled, Value: value}
	}
	a.savePromptParts()
	a.emit("workbench")
}

func applyWorkbenchPromptBridge(label string) {
	a := getApp()
	if a == nil {
		return
	}
	if a.statePage == pageWash {
		a.washPromptChoice = label
	} else {
		a.genPromptChoice = label
	}
	a.applyPromptChoice()
	a.emit("workbench")
}

func setResultsDoneOpenBridge(open bool) {
	a := getApp()
	if a != nil {
		a.resultsDoneOpen = open
		a.emit("workbench")
	}
}

func setHistoryOpenBridge(open bool) {
	a := getApp()
	if a != nil {
		a.historyOpen = open
		a.emit("workbench")
	}
}

func setPreviewPathBridge(path string) {
	a := getApp()
	if a != nil {
		path = strings.TrimSpace(path)
		a.previewPath = path
		a.previewOn = path != ""
		a.emit("workbench")
	}
}

func setGalleryOpenBridge(open bool) {
	a := getApp()
	if a != nil {
		a.galleryOn = open
		a.emit("workbench")
	}
}

func toggleGallerySelectBridge(path string, on bool) {
	a := getApp()
	if a == nil {
		return
	}
	if a.gallerySelected == nil {
		a.gallerySelected = map[string]bool{}
	}
	if on {
		a.gallerySelected[path] = true
	} else {
		delete(a.gallerySelected, path)
	}
	a.emit("workbench")
}

func setAllGallerySelectBridge(on bool) {
	a := getApp()
	if a == nil {
		return
	}
	if a.gallerySelected == nil {
		a.gallerySelected = map[string]bool{}
	}
	clear(a.gallerySelected)
	if on {
		for _, s := range a.sources {
			a.gallerySelected[s.Path] = true
		}
	}
	a.emit("workbench")
}

func removeGallerySourceBridge(path string) {
	a := getApp()
	if a == nil || a.running {
		return
	}
	for i, s := range a.sources {
		if s.Path == path {
			removeWorkbenchSourceBridge(i)
			return
		}
	}
}

func parseIntDefault(s string, def int) int {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err == nil {
		return n
	}
	return def
}
