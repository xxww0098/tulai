package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// WorkbenchService exposes image generation, wash, fission, source queue,
// result gallery, rewash/undo, and structured prompt builder IPCs.
type WorkbenchService struct {
	app *App
}

// SourceItemDTO represents one queued source/reference image in the workbench.
type SourceItemDTO struct {
	Index      int    `json:"index"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	FileURL    string `json:"fileUrl"`
	Washed     bool   `json:"washed"`
	Washing    bool   `json:"washing"`
	WashOutput string `json:"washOutput"`
	OutputURL  string `json:"outputUrl"`
	Selected   bool   `json:"selected"`
}

// ResultTileDTO represents one generated or pending/failed result card.
type ResultTileDTO struct {
	Key         string `json:"key"`
	TaskIndex   int    `json:"taskIndex"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Phase       string `json:"phase,omitempty"`
	Path        string `json:"path"`
	FileURL     string `json:"fileUrl"`
	SrcURL      string `json:"srcUrl"`
	Platform    string `json:"platform"`
	AccountID   int64  `json:"accountId"`
	AccountName string `json:"accountName"`
	Engine      string `json:"engine"`
	Elapsed     string `json:"elapsed"`
	Prompt      string `json:"prompt"`
	Rewritten   bool   `json:"rewritten"`
	Reason      string `json:"reason"`
	QualityNote string `json:"qualityNote,omitempty"`
	ActionText  string `json:"actionText"`
	CanReplace  bool   `json:"canReplace"`
	CanRewash   bool   `json:"canRewash"`
	RewashCount int    `json:"rewashCount"`
}

// ResultRowDTO groups result tiles under a section row.
type ResultRowDTO struct {
	Key     string          `json:"key"`
	Header  bool            `json:"header"`
	Issue   bool            `json:"issue"`
	Title   string          `json:"title"`
	Summary string          `json:"summary"`
	Tiles   []ResultTileDTO `json:"tiles"`
}

// LaneJobDTO describes the task currently running in an account lane.
type LaneJobDTO struct {
	TaskIndex int    `json:"taskIndex"`
	Prompt    string `json:"prompt"`
	SourceURL string `json:"sourceUrl"`
	Status    string `json:"status"`
	Elapsed   string `json:"elapsed"`
}

// AccountLaneDTO describes an inline account worker lane in the workbench.
type AccountLaneDTO struct {
	ID                 int64      `json:"id"`
	Platform           string     `json:"platform"`
	Title              string     `json:"title"`
	State              string     `json:"state"`
	Action             string     `json:"action"`
	UsedToday          int        `json:"usedToday"`
	SuccessToday       int        `json:"successToday"`
	FailedToday        int        `json:"failedToday"`
	Cap                int        `json:"cap"`
	ReadyInSec         int        `json:"readyInSec"`
	CooldownAt         string     `json:"cooldownAt"`
	CoolUntil          int64      `json:"coolUntil"`
	UsagePct           int        `json:"usagePct"`
	UsageLabel         string     `json:"usageLabel"`
	UsageErr           string     `json:"usageErr"`
	Job                LaneJobDTO `json:"job"`
	HasJob             bool       `json:"hasJob"`
	Disabled           bool       `json:"disabled"`
	IsRunning          bool       `json:"isRunning"`
	IsLoggedIn         bool       `json:"isLoggedIn"`
	Pending            bool       `json:"pending"`
	PendingOp          string     `json:"pendingOp"`
	Engine             string     `json:"engine"`
	LastError          string     `json:"lastError"`
	CreditsSynced      bool       `json:"creditsSynced"`
	CreditsRemain      int        `json:"creditsRemain"`
	CreditsTotal       int        `json:"creditsTotal"`
	CreditCost         int        `json:"creditCost"`
	CreditsResetAt     int64      `json:"creditsResetAt"`
	CreditsResetLabel  string     `json:"creditsResetLabel"`
	CreditsCycle       string     `json:"creditsCycle"`
	CreditsCycleLabel  string     `json:"creditsCycleLabel"`
	MemberTier         string     `json:"memberTier"`
	MemberLabel        string     `json:"memberLabel"`
	ConcurrencyLimit   int        `json:"concurrencyLimit"`
	ConcurrencyLabel   string     `json:"concurrencyLabel"`
	WatermarkFree      bool       `json:"watermarkFree"`
	UsagePercent       int        `json:"usagePercent"`
	PlatformLimited    bool       `json:"platformLimited"`
	LimitTips          string     `json:"limitTips"`
	ResetCardCount     int        `json:"resetCardCount"`
	CreditsSyncedAt    int64      `json:"creditsSyncedAt"`
	CreditsSyncedLabel string     `json:"creditsSyncedLabel"`
}

// PromptOptionDTO describes a selectable prompt template in the workbench template menu.
type PromptOptionDTO struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	KindLbl string `json:"kindLbl"`
	Builtin bool   `json:"builtin"`
	Matched bool   `json:"matched"`
}

// WorkbenchStateDTO represents the complete workbench page snapshot.
type WorkbenchStateDTO struct {
	Mode             string            `json:"mode"` // free / wash / fission
	Page             int               `json:"page"`
	BatchMode        int               `json:"batchMode"` // 0 text / 1 image
	Platform         string            `json:"platform"`
	Platforms        []string          `json:"platforms"`
	Doubao           bool              `json:"doubao"`
	Qianwen          bool              `json:"qianwen"`
	QwenIntl         bool              `json:"qwenIntl"`
	Gemini           bool              `json:"gemini"`
	Prompts          string            `json:"prompts"`
	PromptChoice     string            `json:"promptChoice"`
	PromptOptions    []string          `json:"promptOptions"`
	PromptTemplates  []PromptOptionDTO `json:"promptTemplates"`
	PromptSuffix     string           `json:"promptSuffix"`
	UseSuffix        bool             `json:"useSuffix"`
	WashStyle        string           `json:"washStyle"`
	WashStyleOptions []string         `json:"washStyleOptions"`
	OutputPreset     string           `json:"outputPreset"`
	OutputPresets    []string         `json:"outputPresets"`
	OutputMode       string           `json:"outputMode"`
	OutputRatio      string           `json:"outputRatio"`
	OutputRatios     []string         `json:"outputRatios"`
	Sources          []SourceItemDTO  `json:"sources"`
	SourcesTotal     int              `json:"sourcesTotal"`
	WashedCount      int              `json:"washedCount"`
	SelectedSource   int              `json:"selectedSource"`
	SkipImages       bool             `json:"skipImages"`
	Running          bool             `json:"running"`
	RunPage          int              `json:"runPage"`
	Progress         float64          `json:"progress"`
	ProgText         string           `json:"progText"`
	CanStart         bool             `json:"canStart"`
	StartLabel       string           `json:"startLabel"`
	PlanMissing      []string         `json:"planMissing"`
	OutputNote       string           `json:"outputNote"`
	ActiveRows       []ResultRowDTO   `json:"activeRows"`
	IssueRows        []ResultRowDTO   `json:"issueRows"`
	PendingTiles     []ResultTileDTO  `json:"pendingTiles"`
	DoneTiles        []ResultTileDTO  `json:"doneTiles"`
	PendingCount     int              `json:"pendingCount"`
	DoneCount        int              `json:"doneCount"`
	DoneTasks        int              `json:"doneTasks"`
	FailedCount      int              `json:"failedCount"`
	NeedsCount       int              `json:"needsCount"`
	ActiveCount      int              `json:"activeCount"`
	TotalCount       int              `json:"totalCount"`
	ResultsDoneOpen  bool             `json:"resultsDoneOpen"`
	StageTab         int              `json:"stageTab"`
	Lanes            []AccountLaneDTO `json:"lanes"`
	LaneCollapsed    bool             `json:"laneCollapsed"`
	ConcurrencyAuto  bool             `json:"concurrencyAuto"`
	ConcurrencyMax   int              `json:"concurrencyMax"`
	PreviewOn        bool             `json:"previewOn"`
	PreviewPath      string           `json:"previewPath"`
	PreviewURL       string           `json:"previewUrl"`
	GalleryOn        bool             `json:"galleryOn"`
	GallerySelected  int              `json:"gallerySelected"`
	GalleryAll       bool             `json:"galleryAll"`
	GalleryItems     []SourceItemDTO  `json:"galleryItems"`
	StructuredOpen   bool             `json:"structuredOpen"`
	StructuredCount  int              `json:"structuredCount"`
	CanUndoRewash    bool             `json:"canUndoRewash"`
	RewashUndoCount  int              `json:"rewashUndoCount"`
	RepeatCount      int              `json:"repeatCount"`
	ImagePromptMode  string           `json:"imagePromptMode"`
	PlannedTasks     int              `json:"plannedTasks"`
	PromptUnitCount  int              `json:"promptUnitCount"`
	TemplateLocked   bool             `json:"templateLocked"`
}

// PromptPartItemDTO represents one structured prompt dimension entry.
type PromptPartItemDTO struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Value   string `json:"value"`
}

// PromptPartsStateDTO represents the structured prompt builder dialog state.
type PromptPartsStateDTO struct {
	Open  bool                `json:"open"`
	Page  int                 `json:"page"`
	Items []PromptPartItemDTO `json:"items"`
}

func emptyWorkbenchState() WorkbenchStateDTO {
	return WorkbenchStateDTO{
		Platform:         "豆包",
		Platforms:        []string{},
		PromptOptions:    []string{},
		PromptTemplates:  []PromptOptionDTO{},
		WashStyleOptions: []string{},
		OutputPresets:    []string{},
		OutputRatios:     []string{},
		Sources:          []SourceItemDTO{},
		PlanMissing:      []string{},
		ActiveRows:       []ResultRowDTO{},
		IssueRows:        []ResultRowDTO{},
		PendingTiles:     []ResultTileDTO{},
		DoneTiles:        []ResultTileDTO{},
		Lanes:            []AccountLaneDTO{},
		ConcurrencyMax:   tasks.PlatformInFlight,
		GalleryItems:     []SourceItemDTO{},
		RepeatCount:      1,
		ImagePromptMode:  "unified",
	}
}

func (a *App) buildWorkbenchState() WorkbenchStateDTO {
	if a == nil {
		return emptyWorkbenchState()
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
	accounts := a.configuredBrowsers()
	activeRows, issueRows, doneTiles, doneTasks := a.classifyResultRowsWithBrowsers(items, 4, accounts)
	acctTitleByID := make(map[int64]string, len(accounts))
	acctEngineByID := make(map[int64]string, len(accounts))
	for _, b := range accounts {
		acctTitleByID[b.ID] = accountLabel(b)
		if eng := strings.TrimSpace(b.Engine); eng != "" {
			acctEngineByID[b.ID] = eng
		} else {
			acctEngineByID[b.ID] = browser.KindFingerprint
		}
	}

	now := time.Now()
	urlCache := make(map[string]string, 32)
	mediaURLCached := func(p string) string {
		if p == "" {
			return ""
		}
		if u, ok := urlCache[p]; ok {
			return u
		}
		u := a.mediaURL(p)
		urlCache[p] = u
		return u
	}

	mapTile := func(t resultTile) ResultTileDTO {
		reason, act, _ := tileFailure(t.task)
		src := taskSource(t.task)
		acctName := strings.TrimSpace(t.task.AccountName)
		if acctName == "" && t.task.BrowserID > 0 {
			acctName = acctTitleByID[t.task.BrowserID]
		}
		engine := acctEngineByID[t.task.BrowserID]
		if engine == "" {
			engine = browser.KindFingerprint
		}
		rewashCount := 0
		if a.statePage == pageWash && src != "" && a.rewashCounts != nil {
			rewashCount = a.rewashCounts[filepath.Clean(src)]
		}
		phase := ""
		if isTaskActiveStatus(t.task.Status) {
			phase = string(t.task.Phase)
		}
		return ResultTileDTO{
			Key:         t.key,
			TaskIndex:   t.task.Index,
			Title:       tileTitle(t),
			Status:      tileStatus(t.task),
			Phase:       phase,
			Path:        t.path,
			FileURL:     mediaURLCached(t.path),
			SrcURL:      mediaURLCached(src),
			Platform:    t.task.Platform,
			AccountID:   t.task.BrowserID,
			AccountName: acctName,
			Engine:      engine,
			Elapsed:     taskElapsedText(t.task, now),
			Prompt:      t.task.Prompt,
			Rewritten:   t.task.Rewritten,
			Reason:      reason,
			QualityNote: t.task.QualityNote,
			ActionText:  act,
			CanReplace:  !a.running && src != "" && t.task.Status != tasks.StatusRunning,
			CanRewash:   a.statePage == pageWash && !a.running && t.task.Status == tasks.StatusSuccess && (src != "" || t.path != ""),
			RewashCount: rewashCount,
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
	pendingTiles := make([]ResultTileDTO, 0)
	for pos, task := range items {
		if task.Status == tasks.StatusPending || task.Status == "" {
			pendingTiles = append(pendingTiles, mapTile(resultTile{
				key:  fmt.Sprintf("q-%d-%d", task.Index, pos),
				task: task,
			}))
		}
	}

	srcLimit := min(len(a.sources), 24)
	srcs := make([]SourceItemDTO, 0, srcLimit)
	for i := 0; i < srcLimit; i++ {
		s := a.sources[i]
		srcs = append(srcs, SourceItemDTO{
			Index:      i,
			Path:       s.Path,
			Name:       filepath.Base(s.Path),
			FileURL:    mediaURLCached(s.Path),
			Washed:     s.Washed,
			Washing:    s.Washing,
			WashOutput: s.WashOutput,
			OutputURL:  mediaURLCached(s.WashOutput),
			Selected:   i == a.srcSel,
		})
	}
	washedCount := 0
	for _, s := range a.sources {
		if s.Washed {
			washedCount++
		}
	}

	kind := a.workbenchModeKind()
	curChoice := a.currentModePromptChoice()
	selectable, promptTemplates := workbenchPromptItems(kind, a.userPrompts)
	opts := workspacePromptOptions(kind, a.userPrompts)
	optLabels := promptLabels(opts)
	if optLabels == nil {
		optLabels = []string{}
	}
	if curChoice == "" || (curChoice != choosePromptLabel && promptOptionID(curChoice, selectable) == "") {
		curChoice = choosePromptLabel
	}

	selectedPlatform := a.selectedPlatform()
	outputPrefs := a.outputPrefsFor(selectedPlatform)
	presetLabels := outputPresetLabelsFor(selectedPlatform)

	plan := a.workbenchPlan(accounts)
	canStart := !a.renameUpdateActive() && len(a.browserPending) == 0 && plan.Action == StartGenerate
	_, plannedTasks := a.plannedTaskCount()
	promptUnitCount := len(a.taskPromptList())
	templateLocked := a.statePage == pageHome && a.batchMode == batchText && strings.TrimSpace(a.promptPick) != "" && strings.Contains(strings.TrimSpace(a.prompts), "\n")

	startLabel := "开始文生图"
	switch {
	case a.running:
		startLabel = "停止任务"
	case a.washing():
		startLabel = "开始洗图"
	case a.fissioning():
		startLabel = "开始裂变"
	case a.batchMode == batchImage:
		if canStart && plannedTasks > 0 {
			startLabel = fmt.Sprintf("开始图生图 (%d)", plannedTasks)
		} else {
			startLabel = "开始图生图"
		}
	default:
		if canStart && plannedTasks > 0 {
			startLabel = fmt.Sprintf("开始文生图 (%d)", plannedTasks)
		} else {
			startLabel = "开始文生图"
		}
	}

	board := a.accountBoard(accounts)
	rowByID := make(map[int64]store.Browser, len(accounts))
	for _, b := range accounts {
		if a.manager != nil {
			b.IsRunning = a.manager.IsRunning(b.ID)
		}
		rowByID[b.ID] = b
	}
	lanes := make([]AccountLaneDTO, 0, len(board.Lanes))
	for _, ln := range board.Lanes {
		row := rowByID[ln.ID]
		op, pending := a.browserPending[ln.ID]
		pendingOp := ""
		if pending {
			pendingOp = op.opKind()
		}
		quotaView := buildAccountQuotaView(row, ln.Cap, now)
		dto := AccountLaneDTO{
			ID:                 ln.ID,
			Platform:           ln.Platform,
			Title:              laneAccountTitle(ln),
			State:              string(ln.State),
			Action:             ln.Action,
			UsedToday:          ln.UsedToday,
			SuccessToday:       ln.SuccessToday,
			FailedToday:        ln.FailedToday,
			Cap:                ln.Cap,
			CoolUntil:          ln.CoolUntil,
			UsagePct:           quotaView.UsagePct,
			UsageLabel:         quotaView.UsageLabel,
			Disabled:           ln.Disabled || row.Disabled,
			IsRunning:          row.IsRunning,
			IsLoggedIn:         row.IsLoggedIn,
			Pending:            pending,
			PendingOp:          pendingOp,
			Engine:             row.Engine,
			LastError:          row.LastError,
			CreditsSynced:      quotaView.CreditsSynced,
			CreditsRemain:      quotaView.CreditsRemain,
			CreditsTotal:       quotaView.CreditsTotal,
			CreditCost:         quotaView.CreditCost,
			CreditsResetAt:     quotaView.CreditsResetAt,
			CreditsResetLabel:  quotaView.CreditsResetLabel,
			CreditsCycle:       quotaView.CreditsCycle,
			CreditsCycleLabel:  quotaView.CreditsCycleLabel,
			MemberTier:         quotaView.MemberTier,
			MemberLabel:        quotaView.MemberLabel,
			ConcurrencyLimit:   quotaView.ConcurrencyLimit,
			ConcurrencyLabel:   quotaView.ConcurrencyLabel,
			WatermarkFree:      quotaView.WatermarkFree,
			UsagePercent:       quotaView.UsagePercent,
			PlatformLimited:    quotaView.PlatformLimited,
			LimitTips:          quotaView.LimitTips,
			ResetCardCount:     quotaView.ResetCardCount,
			CreditsSyncedAt:    quotaView.CreditsSyncedAt,
			CreditsSyncedLabel: quotaView.CreditsSyncedLabel,
		}
		if row.Expired(now) {
			dto.LastError = ""
		}
		if ln.NextReadyAt.After(now) {
			dto.ReadyInSec = int(ln.NextReadyAt.Sub(now).Seconds()) + 1
		}
		if ln.CoolUntil > now.Unix() {
			dto.CooldownAt = time.Unix(ln.CoolUntil, 0).Local().Format("15:04")
		}
		if ln.Job != nil {
			dto.HasJob = true
			dto.Job = LaneJobDTO{
				TaskIndex: ln.Job.Index,
				Prompt:    ln.Job.Prompt,
				SourceURL: mediaURLCached(ln.Job.Image),
				Status:    string(ln.State),
				Elapsed:   formatElapsedDuration(now.Sub(ln.Job.Started), false),
			}
		}
		lanes = append(lanes, dto)
	}

	galleryItems := make([]SourceItemDTO, 0)
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
				FileURL:  mediaURLCached(s.Path),
				Washed:   s.Washed,
				Washing:  s.Washing,
				Selected: sel,
			})
		}
	}

	parts := a.currentPromptParts()
	structuredCount := 0
	for _, r := range parts.rows() {
		if r.part.Enabled {
			structuredCount++
		}
	}

	planMissing := a.missingBrowserNames(accounts)
	if planMissing == nil {
		planMissing = []string{}
	}
	planPlatforms := plan.Platforms
	if planPlatforms == nil {
		planPlatforms = []string{}
	}

	rewashUndoCount := 0
	canUndoRewash := a.statePage == pageWash && !a.running && a.rewashUndo != nil && a.rewashUndo.page == pageWash && a.rewashUndo.count > 0
	if canUndoRewash {
		rewashUndoCount = a.rewashUndo.count
	}

	return WorkbenchStateDTO{
		Mode:             mode,
		Page:             a.statePage,
		BatchMode:        a.batchMode,
		Platform:         selectedPlatform,
		Platforms:        planPlatforms,
		Doubao:           a.platDoubao,
		Qianwen:          a.platQianwen,
		QwenIntl:         a.platQwenIntl,
		Gemini:           a.platGemini,
		Prompts:          a.prompts,
		PromptChoice:     curChoice,
		PromptOptions:    optLabels,
		PromptTemplates:  promptTemplates,
		PromptSuffix:     a.promptSuffix,
		UseSuffix:        a.useSuffix,
		WashStyle:        a.washStyle,
		WashStyleOptions: washStyleLabels(),
		OutputPreset:     outputPrefs.preset,
		OutputPresets:    presetLabels,
		OutputMode:       outputPrefs.mode,
		OutputRatio:      outputPrefs.ratio,
		OutputRatios:     outputRatiosFor(selectedPlatform),
		Sources:          srcs,
		SourcesTotal:     len(a.sources),
		WashedCount:      washedCount,
		SelectedSource:   a.srcSel,
		SkipImages:       a.skipImages,
		Running:          a.running,
		RunPage:          a.runPage,
		Progress:         a.progress,
		ProgText:         a.progText,
		CanStart:         canStart,
		StartLabel:       startLabel,
		PlanMissing:      planMissing,
		OutputNote:       a.outputNote,
		ActiveRows:       mapRows(activeRows),
		IssueRows:        mapRows(issueRows),
		PendingTiles:     pendingTiles,
		DoneTiles:        mappedDone,
		PendingCount:     len(pendingTiles),
		DoneCount:        len(mappedDone),
		DoneTasks:        doneTasks,
		FailedCount:      tally.Failed,
		NeedsCount:       tally.Needs,
		ActiveCount:      tally.Running,
		TotalCount:       len(items),
		ResultsDoneOpen:  a.resultsDoneOpen,
		StageTab:         a.stageTab,
		Lanes:            lanes,
		LaneCollapsed:    a.laneCollapsed,
		ConcurrencyAuto:  a.concurrencyAutoFor(selectedPlatform),
		ConcurrencyMax:   a.concurrencyMaxFor(selectedPlatform),
		PreviewOn:        a.previewOn,
		PreviewPath:      a.previewPath,
		PreviewURL:       mediaURLCached(a.previewPath),
		GalleryOn:        a.galleryOn,
		GallerySelected:  gallerySelCount,
		GalleryAll:       len(a.sources) > 0 && gallerySelCount == len(a.sources),
		GalleryItems:     galleryItems,
		StructuredOpen:   a.structuredOpen,
		StructuredCount:  structuredCount,
		CanUndoRewash:    canUndoRewash,
		RewashUndoCount:  rewashUndoCount,
		RepeatCount:      a.currentRepeatCount(),
		ImagePromptMode:  normalizeImagePromptMode(a.imagePromptMode),
		PlannedTasks:     plannedTasks,
		PromptUnitCount:  promptUnitCount,
		TemplateLocked:   templateLocked,
	}
}

// GetWorkbenchState returns the current workbench state snapshot.
func (s *WorkbenchService) GetWorkbenchState(ctx context.Context) WorkbenchStateDTO {
	if s.app == nil {
		return emptyWorkbenchState()
	}
	out, err := callValue(s.app, ctx, func() WorkbenchStateDTO {
		return s.app.buildWorkbenchState()
	})
	if err != nil {
		return emptyWorkbenchState()
	}
	return out
}

// SetWorkbenchField updates a workbench field (mode, batchMode, prompts, washStyle, etc.).
func (s *WorkbenchService) SetWorkbenchField(ctx context.Context, field, value string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
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
			if a.running && a.runPage == a.statePage {
				break
			}
			nextMode := batchText
			if value == "image" || value == "1" {
				nextMode = batchImage
			}
			a.switchBatchMode(nextMode, false)
		case "repeatCount":
			if a.running && a.runPage == a.statePage {
				break
			}
			n := clampRepeat(parseIntDefault(value, 1))
			if a.batchMode == batchImage {
				a.imageRepeat = n
				if a.store != nil {
					a.store.SetInt(platformSettingKey(a.selectedPlatform(), "free_image_repeat"), n)
				}
			} else {
				a.textRepeat = n
				if a.store != nil {
					a.store.SetInt(platformSettingKey(a.selectedPlatform(), "free_text_repeat"), n)
				}
			}
		case "imagePromptMode":
			if a.running && a.runPage == a.statePage {
				break
			}
			a.imagePromptMode = normalizeImagePromptMode(value)
			if a.store != nil {
				a.store.Set(platformSettingKey(a.selectedPlatform(), "image_prompt_mode"), a.imagePromptMode)
			}
		case "detachTemplate":
			if !(a.running && a.runPage == a.statePage) {
				a.detachActiveFreeTemplate()
			}
		case "prompts":
			if !(a.running && a.runPage == a.statePage) {
				a.prompts = value
				if a.statePage == pageHome {
					if a.batchMode == batchImage {
						a.imgPrompts = value
						if a.imgPromptPick == "" {
							a.imgFreeformPrompts = value
						} else {
							a.clearEditedPromptChoice(a.statePage, value)
						}
					} else {
						a.textPrompts = value
						if a.promptPick == "" {
							a.freeformPrompts = value
						} else {
							a.clearEditedPromptChoice(a.statePage, value)
						}
					}
				} else {
					a.clearEditedPromptChoice(a.statePage, value)
				}
			}
		case "resetWashPrompt":
			if !(a.running && a.runPage == pageWash) {
				a.prompts = a.defaultWashPrompt()
				a.washPromptChoice = choosePromptLabel
			}
		case "resetFissionPrompt":
			if !(a.running && a.runPage == pageFission) {
				a.skipImages = false
				a.prompts = DefaultFissionPrompt
				a.fissionPromptChoice = choosePromptLabel
				if a.store != nil {
					a.store.SetInt(platformSettingKey(a.selectedPlatform(), "fission_skip_images"), 0)
				}
			}
		case "washStyle":
			a.washStyle = value
			a.saveWashStyle()
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
			if a.running && a.runPage == a.statePage {
				break
			}
			on := value == "true" || value == "1"
			a.skipImages = on
			if a.statePage == pageFission {
				if on && prompts.SameText(a.prompts, DefaultFissionPrompt) {
					a.prompts = ""
					a.fissionPromptChoice = choosePromptLabel
				} else if !on && strings.TrimSpace(a.prompts) == "" {
					a.prompts = DefaultFissionPrompt
					a.fissionPromptChoice = choosePromptLabel
				}
				if a.store != nil {
					a.store.SetInt(platformSettingKey(a.selectedPlatform(), "fission_skip_images"), boolInt(on))
				}
			}
		}
		a.emit("workbench", "shell")
	})
}

// SetPlatformSelected switches the active single-selected provider platform (豆包 / 千问).
func (s *WorkbenchService) SetPlatformSelected(ctx context.Context, platform string, on bool) {
	if s.app == nil {
		return
	}
	_ = on
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		prevPlatform := a.selectedPlatform()
		prevPage := a.statePage
		if !a.setSinglePlatform(platform) {
			return
		}
		if a.store != nil {
			if prevPlatform != a.selectedPlatform() {
				a.persistWorkspaceFor(prevPlatform, prevPage)
			}
			a.store.SetPlatforms(a.selectedPlatforms())
			a.store.SetInt("platform_auto", 0)
		}
		a.emit("workbench", "shell")
	})
}

// SetOutputSize updates the web-native output aspect ratio preset.
// Changes are ignored while tasks are running.
func (s *WorkbenchService) SetOutputSize(ctx context.Context, preset, mode, ratio string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		a.activeOutputPrefs().updateFor(a.selectedPlatform(), preset, mode, ratio)
		a.saveOutputSize()
		a.emit("workbench")
	})
}

// PickPromptTextFile opens a native file picker to import batch prompts from .txt or .md files into text-to-image mode.
func (s *WorkbenchService) PickPromptTextFile(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	platform, err := callValue(s.app, ctx, func() string {
		s.app.syncPage()
		return s.app.selectedPlatform()
	})
	if err != nil {
		return err
	}
	paths, err := s.app.pickFilesAsync(ctx, "导入提示词文本", true, []string{"txt", "md"}, "文本文件")
	if err != nil {
		logging.Log.Errorf("选择提示词文本文件失败: %v", err)
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	imported, count, impErr := readPromptTextFiles(paths)
	return s.app.call(ctx, func() {
		if impErr != nil {
			s.app.pushToast("error", impErr.Error())
			return
		}
		s.app.applyImportedPrompts(platform, imported, count)
	})
}

// AddWorkbenchSources ingests dropped or supplied file/folder paths into the active workbench queue.
func (s *WorkbenchService) AddWorkbenchSources(ctx context.Context, paths []string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.ingestSources(paths)
	})
}

// PickWorkbenchFiles opens a native file picker and adds selected source images.
func (s *WorkbenchService) PickWorkbenchFiles(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	slot, err := callValue(s.app, ctx, func() workspaceSlot {
		s.app.syncPage()
		return workspaceSlot{platform: s.app.selectedPlatform(), page: s.app.statePage}
	})
	if err != nil {
		return err
	}
	paths, err := s.app.pickFilesAsync(ctx, "选择源图片", slot.page != pageFission, []string{"png", "jpg", "jpeg", "webp", "gif"}, "图片")
	if err != nil {
		logging.Log.Errorf("选择源图片失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		loaded := loadSourceFiles(paths)
		_ = s.app.call(ctx, func() { s.app.addLoadedFor(slot.platform, slot.page, loaded) })
	}
	return nil
}

// PickWorkbenchFolder opens a native folder picker and imports images from the chosen folders.
func (s *WorkbenchService) PickWorkbenchFolder(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	slot, err := callValue(s.app, ctx, func() workspaceSlot {
		s.app.syncPage()
		return workspaceSlot{platform: s.app.selectedPlatform(), page: s.app.statePage}
	})
	if err != nil {
		return err
	}
	paths, err := s.app.pickDirsAsync(ctx, "选择图片文件夹", slot.page != pageFission)
	if err != nil {
		logging.Log.Errorf("选择图片文件夹失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		loaded := loadSourceFiles(paths)
		_ = s.app.call(ctx, func() { s.app.addLoadedFor(slot.platform, slot.page, loaded) })
	}
	return nil
}

func (a *App) removeWorkbenchSourceInternal(idx int) {
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

// RemoveWorkbenchSource removes a queued source image by index.
func (s *WorkbenchService) RemoveWorkbenchSource(ctx context.Context, idx int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.removeWorkbenchSourceInternal(idx)
	})
}

// ClearWorkbenchSources clears all queued source images.
func (s *WorkbenchService) ClearWorkbenchSources(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		a.sources = nil
		a.srcSel = -1
		a.persistWorkspace(a.statePage)
		a.emit("workbench")
	})
}

// ClearWashedWorkbenchSources removes already-washed images from the source queue.
func (s *WorkbenchService) ClearWashedWorkbenchSources(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		kept := make([]sourceImage, 0, len(a.sources))
		removed := 0
		newSel := -1
		for i, src := range a.sources {
			if src.Washed {
				removed++
				if a.gallerySelected != nil {
					delete(a.gallerySelected, src.Path)
				}
				continue
			}
			if i == a.srcSel {
				newSel = len(kept)
			}
			kept = append(kept, src)
		}
		if removed == 0 {
			return
		}
		if len(kept) == 0 {
			kept = nil
		}
		a.sources = kept
		a.srcSel = newSel
		a.persistWorkspace(a.statePage)
		a.pushToast("success", fmt.Sprintf("已清空 %d 张已洗图片", removed))
		a.emit("workbench")
	})
}

// SelectWorkbenchSource highlights a source image by index.
func (s *WorkbenchService) SelectWorkbenchSource(ctx context.Context, idx int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if idx >= 0 && idx < len(a.sources) {
			a.srcSel = idx
		} else {
			a.srcSel = -1
		}
		a.emit("workbench")
	})
}

// StartTasks starts or stops the active workbench run.
func (s *WorkbenchService) StartTasks(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			a.stopTasks()
			a.emit("workbench", "shell")
			return
		}
		a.startTasks()
	})
}

// StopTasks stops the currently running workbench batch.
func (s *WorkbenchService) StopTasks(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.stopTasks()
		s.app.emit("workbench", "shell")
	})
}

// RetryTask retries a single task by its task index.
func (s *WorkbenchService) RetryTask(ctx context.Context, taskIndex int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		for _, t := range a.queue {
			if t.Index == taskIndex {
				a.retryTasks([]tasks.Task{t})
				return
			}
		}
	})
}

// RetryAllFailed retries all failed, needs-user, needs-review, or worked-skipped tasks.
func (s *WorkbenchService) RetryAllFailed(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		var failed []tasks.Task
		for _, t := range a.queue {
			if t.Status == tasks.StatusFailed || t.Status == tasks.StatusNeedsUser || t.Status == tasks.StatusNeedsReview || (t.Status == tasks.StatusSkipped && t.Worked()) {
				failed = append(failed, t)
			}
		}
		a.retryTasks(failed)
	})
}

// OpenOutputFolder ensures the resolved output folder exists and opens it in the OS file manager.
func (s *WorkbenchService) OpenOutputFolder(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	rawDir, err := callValue(s.app, ctx, func() string { return s.app.resolvedOutputDir() })
	if err != nil {
		return err
	}
	dir, err := EnsureOutputDir(rawDir)
	if err != nil {
		_ = s.app.call(ctx, func() {
			s.app.outputNote = "无法创建输出文件夹"
			s.app.emit("workbench")
		})
		return err
	}
	return s.app.openPath(dir)
}

// ReplaceResultImage prompts the user for a local image and replaces a task result in place.
func (s *WorkbenchService) ReplaceResultImage(ctx context.Context, taskIndex int) error {
	if s.app == nil {
		return nil
	}
	running, err := callValue(s.app, ctx, func() bool { return s.app.running })
	if err != nil || running {
		return err
	}
	paths, err := s.app.pickFilesAsync(ctx, "选择替换图片", false, []string{"png", "jpg", "jpeg", "webp"}, "图片")
	if err != nil || len(paths) == 0 {
		return err
	}
	return s.app.call(ctx, func() {
		if repErr := s.app.replaceCardImage(taskIndex, paths[0]); repErr != nil {
			s.app.pushToast("error", repErr.Error())
		} else {
			s.app.pushToast("success", "已替换结果图")
		}
	})
}

// GetPromptPartsState returns the structured prompt builder items.
func (s *WorkbenchService) GetPromptPartsState(ctx context.Context) PromptPartsStateDTO {
	if s.app == nil {
		return PromptPartsStateDTO{Items: []PromptPartItemDTO{}}
	}
	out, err := callValue(s.app, ctx, func() PromptPartsStateDTO {
		a := s.app
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
	})
	if err != nil {
		return PromptPartsStateDTO{Items: []PromptPartItemDTO{}}
	}
	return out
}

// SetPromptPart updates one structured prompt dimension and persists it.
func (s *WorkbenchService) SetPromptPart(ctx context.Context, key string, enabled bool, value string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
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
	})
}

// ApplyWorkbenchPrompt applies a saved prompt template by label.
func (s *WorkbenchService) ApplyWorkbenchPrompt(ctx context.Context, label string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.applyWorkbenchPrompt(label)
		s.app.emit("workbench")
	})
}

// SetResultsDoneOpen expands or collapses the completed wash results section.
func (s *WorkbenchService) SetResultsDoneOpen(ctx context.Context, open bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.resultsDoneOpen = open
		s.app.emit("workbench")
	})
}

// SetPreviewPath opens the full-size lightbox preview for path, or closes it when empty.
func (s *WorkbenchService) SetPreviewPath(ctx context.Context, path string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		path = strings.TrimSpace(path)
		s.app.previewPath = path
		s.app.previewOn = path != ""
		s.app.emit("workbench")
	})
}

// SetGalleryOpen opens or closes the source gallery modal.
func (s *WorkbenchService) SetGalleryOpen(ctx context.Context, open bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.galleryOn = open
		s.app.emit("workbench")
	})
}

// ToggleGallerySelect selects or deselects one source path inside the gallery modal.
func (s *WorkbenchService) ToggleGallerySelect(ctx context.Context, path string, on bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.gallerySelected == nil {
			a.gallerySelected = map[string]bool{}
		}
		if on {
			a.gallerySelected[path] = true
		} else {
			delete(a.gallerySelected, path)
		}
		a.emit("workbench")
	})
}

// SetAllGallerySelect selects or clears all items in the gallery modal.
func (s *WorkbenchService) SetAllGallerySelect(ctx context.Context, on bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.gallerySelected == nil {
			a.gallerySelected = map[string]bool{}
		}
		clear(a.gallerySelected)
		if on {
			for _, src := range a.sources {
				a.gallerySelected[src.Path] = true
			}
		}
		a.emit("workbench")
	})
}

// RemoveGallerySourceItem removes a source image by its filesystem path from the gallery modal.
func (s *WorkbenchService) RemoveGallerySourceItem(ctx context.Context, path string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		for i, src := range a.sources {
			if src.Path == path {
				a.removeWorkbenchSourceInternal(i)
				return
			}
		}
	})
}

func cloneTasksForSnapshot(in []tasks.Task) []tasks.Task {
	out := make([]tasks.Task, len(in))
	for i, t := range in {
		t.Images = append([]string(nil), t.Images...)
		t.Results = append([]string(nil), t.Results...)
		out[i] = t
	}
	return out
}

// RequeueDoneTasks sends completed tasks back to pending for a second wash pass, pushing an undo snapshot.
func (s *WorkbenchService) RequeueDoneTasks(ctx context.Context, taskIndices []int) {
	if s.app == nil || len(taskIndices) == 0 {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		a.syncPage()
		if a.statePage != pageWash {
			return
		}
		wanted := make(map[int]bool, len(taskIndices))
		for _, idx := range taskIndices {
			wanted[idx] = true
		}
		count := 0
		for _, t := range a.queue {
			if wanted[t.Index] && t.Status == tasks.StatusSuccess && (taskSource(t) != "" || len(t.Results) > 0) {
				count++
			}
		}
		if count == 0 {
			return
		}

		sourcesSnap := make([]sourceImage, len(a.sources))
		copy(sourcesSnap, a.sources)
		countsSnap := make(map[string]int, len(a.rewashCounts))
		for k, v := range a.rewashCounts {
			countsSnap[k] = v
		}
		a.rewashUndo = &rewashSnapshot{
			page:         a.statePage,
			sources:      sourcesSnap,
			queue:        cloneTasksForSnapshot(a.queue),
			progress:     a.progress,
			progText:     a.progText,
			count:        count,
			rewashCounts: countsSnap,
		}
		if a.rewashCounts == nil {
			a.rewashCounts = map[string]int{}
		}

		for i := range a.queue {
			t := &a.queue[i]
			if !wanted[t.Index] || t.Status != tasks.StatusSuccess || (taskSource(*t) == "" && len(t.Results) == 0) {
				continue
			}
			origSrc := taskSource(*t)
			src := origSrc
			if src == "" || !filePresent(src) {
				picked := ""
				for _, r := range t.Results {
					if r = strings.TrimSpace(r); r != "" && filePresent(r) {
						picked = r
						break
					}
				}
				if picked != "" {
					src = picked
				} else if src == "" && len(t.Results) > 0 {
					src = strings.TrimSpace(t.Results[0])
				} else {
					src = origSrc
				}
			}

			t.Status = tasks.StatusPending
			t.Results = nil
			t.Error = ""
			t.QualityNote = ""
			t.Elapsed = 0
			t.StartedAt = time.Time{}
			t.AccountName = ""
			t.BrowserID = 0
			t.Undispatched = false
			t.Reassigned = ""
			t.ReassignWhy = ""
			if src != "" {
				t.Images = []string{src}
				clean := filepath.Clean(src)
				a.rewashCounts[clean]++
				found := false
				for j := range a.sources {
					if filepath.Clean(a.sources[j].Path) == clean {
						a.sources[j].Washed = false
						a.sources[j].Washing = false
						a.sources[j].WashOutput = ""
						found = true
					}
				}
				if !found && filePresent(src) {
					a.sources = append(a.sources, sourceImage{Path: src})
				}
			}
		}

		tally := TallyTasks(a.queue)
		a.progress = float64(tally.Done) / float64(max(1, len(a.queue)))
		a.progText = taskResultSummary(a.queue)
		a.persistWorkspace(a.statePage)
		a.pushToast("info", fmt.Sprintf("已将 %d 张图片放回待洗队列，准备二次清洗", count))
		a.emit("workbench", "shell")
	})
}

// UndoRequeueTasks restores the workspace queue snapshot from before the last RequeueDoneTasks.
func (s *WorkbenchService) UndoRequeueTasks(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		a.syncPage()
		if a.statePage != pageWash || a.rewashUndo == nil || a.rewashUndo.page != pageWash {
			return
		}
		snap := a.rewashUndo
		a.sources = make([]sourceImage, len(snap.sources))
		copy(a.sources, snap.sources)
		a.queue = cloneTasksForSnapshot(snap.queue)
		a.progress = snap.progress
		a.progText = snap.progText
		a.rewashCounts = make(map[string]int, len(snap.rewashCounts))
		for k, v := range snap.rewashCounts {
			a.rewashCounts[k] = v
		}
		a.rewashUndo = nil
		a.persistWorkspace(a.statePage)
		a.pushToast("info", "已撤销二次清洗放回")
		a.emit("workbench", "shell")
	})
}

// ClearWorkbenchResults clears the current workbench page's task queue and result display
// without deleting generated image files on disk.
func (s *WorkbenchService) ClearWorkbenchResults(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		a.syncPage()
		clearedTasks := len(a.queue)
		doneTasks := 0
		clearedImages := 0
		clearedPaths := make(map[string]struct{})
		for _, t := range a.queue {
			if t.Status == tasks.StatusSuccess {
				doneTasks++
			}
			if src := taskSource(t); src != "" && a.rewashCounts != nil {
				delete(a.rewashCounts, filepath.Clean(src))
			}
			for _, res := range t.Results {
				if p := strings.TrimSpace(res); p != "" {
					clearedPaths[p] = struct{}{}
					clearedImages++
				}
			}
		}
		hadUndo := a.rewashUndo != nil && a.rewashUndo.page == a.statePage
		hadProgress := a.progress != 0 || strings.TrimSpace(a.progText) != ""
		if clearedTasks == 0 && !hadUndo && !hadProgress {
			return
		}
		a.queue = nil
		a.progress = 0
		a.progText = ""
		a.resultsDoneOpen = false
		if a.statePage == pageFission {
			a.fissionRetryBase = nil
		}
		if hadUndo {
			a.rewashUndo = nil
		}
		if a.statePage == pageWash {
			for i := range a.sources {
				a.sources[i].Washing = false
			}
		}
		if a.previewOn && a.previewPath != "" {
			if _, ok := clearedPaths[strings.TrimSpace(a.previewPath)]; ok {
				a.previewOn = false
				a.previewPath = ""
			}
		}
		a.persistWorkspace(a.statePage)
		switch {
		case clearedImages > 0 && clearedTasks == doneTasks:
			a.pushToast("success", fmt.Sprintf("已清空 %d 张结果（本地文件已保留）", clearedImages))
		case clearedImages > 0:
			a.pushToast("success", fmt.Sprintf("已清空全部结果（含 %d 张成图，本地文件已保留）", clearedImages))
		case clearedTasks > 0:
			a.pushToast("success", fmt.Sprintf("已清空 %d 项任务记录", clearedTasks))
		default:
			a.pushToast("success", "已清空结果列表")
		}
		a.emit("workbench", "shell")
	})
}

func trashFolderLabel() string {
	if runtime.GOOS == "darwin" {
		return "废纸篓"
	}
	return "回收站"
}

func findQueueResultTarget(queue []tasks.Task, taskIndex int, path string) (queueIdx int, resultIdx int, verifiedPath string) {
	wantPath := strings.TrimSpace(path)
	cleanWant := ""
	if wantPath != "" {
		cleanWant = filepath.Clean(wantPath)
	}
	for qi, t := range queue {
		if t.Index != taskIndex {
			continue
		}
		if cleanWant == "" {
			if len(t.Results) == 1 {
				if r := strings.TrimSpace(t.Results[0]); r != "" {
					return qi, 0, r
				}
			}
			return qi, -1, ""
		}
		for ri, raw := range t.Results {
			if r := strings.TrimSpace(raw); r != "" && filepath.Clean(r) == cleanWant {
				return qi, ri, r
			}
		}
	}
	if cleanWant != "" {
		for qi, t := range queue {
			for ri, raw := range t.Results {
				if r := strings.TrimSpace(raw); r != "" && filepath.Clean(r) == cleanWant {
					return qi, ri, r
				}
			}
		}
	}
	return -1, -1, ""
}

// DeleteWorkbenchResult removes one result tile (or issue task) from the active workbench queue,
// optionally moving its verified local result file to the OS trash/recycle bin.
func (s *WorkbenchService) DeleteWorkbenchResult(ctx context.Context, taskIndex int, path string, trashFile bool) error {
	if s.app == nil {
		return nil
	}
	type deleteTarget struct {
		found        bool
		platform     string
		page         int
		verifiedPath string
	}
	target, err := callValue(s.app, ctx, func() deleteTarget {
		a := s.app
		if a.running {
			return deleteTarget{}
		}
		a.syncPage()
		qi, _, verifiedPath := findQueueResultTarget(a.queue, taskIndex, path)
		if qi < 0 {
			return deleteTarget{}
		}
		if trashFile && verifiedPath == "" {
			return deleteTarget{}
		}
		return deleteTarget{
			found:        true,
			platform:     a.selectedPlatform(),
			page:         a.statePage,
			verifiedPath: verifiedPath,
		}
	})
	if err != nil || !target.found {
		return err
	}

	fileMissing := false
	if trashFile && target.verifiedPath != "" {
		if _, statErr := os.Stat(target.verifiedPath); statErr != nil {
			if os.IsNotExist(statErr) {
				fileMissing = true
			} else {
				_ = s.app.call(ctx, func() {
					s.app.pushToast("error", fmt.Sprintf("删除图片失败：%v", statErr))
					s.app.emit("shell")
				})
				return statErr
			}
		} else if trashErr := s.app.trashPath(target.verifiedPath); trashErr != nil && !os.IsNotExist(trashErr) {
			_ = s.app.call(ctx, func() {
				s.app.pushToast("error", fmt.Sprintf("移至%s失败：%v", trashFolderLabel(), trashErr))
				s.app.emit("shell")
			})
			return trashErr
		}
	}

	return s.app.call(ctx, func() {
		a := s.app
		if a.running {
			return
		}
		removed := false
		matchedPath := target.verifiedPath
		a.onWorkspace(target.platform, target.page, func() {
			qi, ri, verified := findQueueResultTarget(a.queue, taskIndex, target.verifiedPath)
			if qi < 0 {
				return
			}
			if verified != "" {
				matchedPath = verified
			}
			removedTask := a.queue[qi]
			cleanMatched := ""
			if matchedPath != "" {
				cleanMatched = filepath.Clean(matchedPath)
			}
			if ri >= 0 && len(a.queue[qi].Results) > 1 {
				nextResults := make([]string, 0, len(a.queue[qi].Results)-1)
				nextResults = append(nextResults, a.queue[qi].Results[:ri]...)
				nextResults = append(nextResults, a.queue[qi].Results[ri+1:]...)
				a.queue[qi].Results = nextResults
				if target.page == pageWash && cleanMatched != "" {
					for j := range a.sources {
						if a.sources[j].WashOutput != "" && filepath.Clean(a.sources[j].WashOutput) == cleanMatched {
							a.sources[j].WashOutput = nextResults[0]
						}
					}
				}
			} else {
				a.queue = append(a.queue[:qi], a.queue[qi+1:]...)
				if target.page == pageWash {
					src := taskSource(removedTask)
					cleanSrc := ""
					if src != "" {
						cleanSrc = filepath.Clean(src)
					}
					if cleanSrc != "" && a.rewashCounts != nil {
						stillReferenced := false
						for _, t := range a.queue {
							if s := taskSource(t); s != "" && filepath.Clean(s) == cleanSrc {
								stillReferenced = true
								break
							}
						}
						if !stillReferenced {
							delete(a.rewashCounts, cleanSrc)
						}
					}
					for j := range a.sources {
						matchOut := cleanMatched != "" && a.sources[j].WashOutput != "" && filepath.Clean(a.sources[j].WashOutput) == cleanMatched
						matchSrc := cleanSrc != "" && filepath.Clean(a.sources[j].Path) == cleanSrc
						if matchOut || matchSrc {
							a.sources[j].Washing = false
							if trashFile {
								a.sources[j].Washed = false
								a.sources[j].WashOutput = ""
							}
						}
					}
				}
			}

			if len(a.queue) == 0 {
				a.progress = 0
				a.progText = ""
				a.resultsDoneOpen = false
				if target.page == pageFission {
					a.fissionRetryBase = nil
				}
			} else {
				tally := TallyTasks(a.queue)
				a.progress = float64(tally.Done) / float64(max(1, len(a.queue)))
				a.progText = taskResultSummary(a.queue)
			}
			if a.rewashUndo != nil && a.rewashUndo.page == target.page {
				a.rewashUndo = nil
			}
			removed = true
		})
		if !removed {
			return
		}
		if a.previewOn && matchedPath != "" && strings.TrimSpace(a.previewPath) != "" && filepath.Clean(a.previewPath) == filepath.Clean(matchedPath) {
			a.previewOn = false
			a.previewPath = ""
		}
		a.persistWorkspaceFor(target.platform, target.page)
		switch {
		case trashFile && fileMissing:
			a.pushToast("info", "本地文件已不存在，已从结果列表移除")
		case trashFile:
			a.pushToast("success", fmt.Sprintf("已将 %s 移至%s", filepath.Base(matchedPath), trashFolderLabel()))
		default:
			a.pushToast("info", "已从结果列表移除（本地文件已保留）")
		}
		a.emit("workbench", "shell")
	})
}

func parseIntDefault(s string, def int) int {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err == nil {
		return n
	}
	return def
}

// ---- Test bridge wrappers ----

func getWorkbenchStateBridge() WorkbenchStateDTO {
	return (&WorkbenchService{app: getApp()}).GetWorkbenchState(context.Background())
}

func setWorkbenchFieldBridge(field, value string) {
	(&WorkbenchService{app: getApp()}).SetWorkbenchField(context.Background(), field, value)
}

func setPlatformSelectedBridge(platform string, on bool) {
	(&WorkbenchService{app: getApp()}).SetPlatformSelected(context.Background(), platform, on)
}

func clearWashedWorkbenchSourcesBridge() {
	(&WorkbenchService{app: getApp()}).ClearWashedWorkbenchSources(context.Background())
}

func setGalleryOpenBridge(open bool) {
	(&WorkbenchService{app: getApp()}).SetGalleryOpen(context.Background(), open)
}

func requeueDoneTasksBridge(taskIndices []int) {
	(&WorkbenchService{app: getApp()}).RequeueDoneTasks(context.Background(), taskIndices)
}

func undoRequeueTasksBridge() {
	(&WorkbenchService{app: getApp()}).UndoRequeueTasks(context.Background())
}

func clearWorkbenchResultsBridge() {
	(&WorkbenchService{app: getApp()}).ClearWorkbenchResults(context.Background())
}
