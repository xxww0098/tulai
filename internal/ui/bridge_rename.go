package ui

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/tsx"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getRenameState":    getRenameStateBridge,
		"renamePickFiles":   renamePickFilesBridge,
		"renamePickFolder":  renamePickFolderBridge,
		"renameIngestPaths": renameIngestPathsBridge,
		"renameClear":       renameClearBridge,
		"renameRemoveAt":    renameRemoveAtBridge,
		"renameSetField":    renameSetFieldBridge,
		"renameSetRule":     renameSetRuleBridge,
		"renameAddRule":     renameAddRuleBridge,
		"renameRemoveRule":  renameRemoveRuleBridge,
		"renameSetDraft":    renameSetDraftBridge,
		"renamePasteLines":  renamePasteLinesBridge,
		"renameMoveRow":     renameMoveRowBridge,
		"renameSuggestAI":   renameSuggestAIBridge,
		"renameSaveAIModel": renameSaveAIModelBridge,
		"renameApply":       renameApplyBridge,
		"renameUndo":        renameUndoBridge,
	})
}

type RenameRuleDTO struct {
	Index    int
	Find     string
	With     string
	Template int
	Count    int
	Offset   int
	Text     string
}

type RenameRowDTO struct {
	Index   int
	Path    string
	OldName string
	NewName string
	Draft   string
	Changed bool
}

type RenameStateDTO struct {
	Mode          int
	Busy          bool
	IncludeExt    bool
	InsertKind    string
	Position      string
	Index         int
	Text          string
	Prefix        string
	Suffix        string
	NumFormat     string
	NumStart      int
	NumWidth      int
	NumPrefix     string
	NumSuffix     string
	SortBy        string
	AIPrompt      string
	AISettings    bool
	AIURL         string
	AIKey         string
	AIModel       string
	Rules         []RenameRuleDTO
	Rows          []RenameRowDTO
	ChangedCount  int
	CanUndo       bool
	PlanErr       string
	Note          string
	Err           string
	TemplateNames []string
}

var renameTemplateLabels = []string{
	"普通查找",
	"前 N 位",
	"后 N 位",
	"从第 X 位开始的 N 位",
	"某字符之后的内容",
	"某字符之前的内容",
	"某字符之后的 N 位",
	"某字符之前的 N 位",
}

func getRenameStateBridge() RenameStateDTO {
	a := getApp()
	if a == nil {
		return RenameStateDTO{TemplateNames: renameTemplateLabels}
	}
	s := &a.fileRename
	s.init()

	changes, planErr := a.renamePlan()
	planErrStr := ""
	if planErr != nil {
		planErrStr = planErr.Error()
	}

	rules := make([]RenameRuleDTO, 0, len(s.rules))
	for i, r := range s.rules {
		rules = append(rules, RenameRuleDTO{
			Index:    i,
			Find:     r.Find,
			With:     r.With,
			Template: r.Template,
			Count:    r.Count,
			Offset:   r.Offset,
			Text:     r.Text,
		})
	}

	rows := make([]RenameRowDTO, 0, len(s.items))
	changedCount := 0
	for i, item := range s.items {
		oldName := filepath.Base(item.path)
		newName := oldName
		changed := false
		if i < len(changes) {
			newName = filepath.Base(changes[i].To)
			changed = changes[i].From != changes[i].To
		}
		if changed {
			changedCount++
		}
		rows = append(rows, RenameRowDTO{
			Index:   i,
			Path:    item.path,
			OldName: oldName,
			NewName: newName,
			Draft:   item.draft,
			Changed: changed,
		})
	}

	return RenameStateDTO{
		Mode:          s.mode,
		Busy:          s.busy,
		IncludeExt:    s.includeExt,
		InsertKind:    s.insertKind,
		Position:      s.position,
		Index:         int(s.index),
		Text:          s.text,
		Prefix:        s.prefix,
		Suffix:        s.suffix,
		NumFormat:     s.numFormat,
		NumStart:      int(s.numStart),
		NumWidth:      int(s.numWidth),
		NumPrefix:     s.numPrefix,
		NumSuffix:     s.numSuffix,
		SortBy:        s.sortBy,
		AIPrompt:      s.aiPrompt,
		AISettings:    s.aiSettings,
		AIURL:         a.customURL,
		AIKey:         a.customKey,
		AIModel:       a.customModel,
		Rules:         rules,
		Rows:          rows,
		ChangedCount:  changedCount,
		CanUndo:       len(s.undo) > 0,
		PlanErr:       planErrStr,
		Note:          s.note,
		Err:           s.err,
		TemplateNames: renameTemplateLabels,
	}
}

func renamePickFilesBridge(ctx context.Context) error {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return nil
	}
	paths, err := a.pickFilesAsync(ctx, "选择要重命名的文件", true, nil, "所有文件")
	if err != nil || len(paths) == 0 {
		return err
	}
	return a.onScope(ctx, func() {
		a.renameIngest(paths)
		a.emit("rename")
	})
}

func renamePickFolderBridge(ctx context.Context, recursive bool) error {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return nil
	}
	dirs, err := a.pickDirsAsync(ctx, "选择包含文件的文件夹", true)
	if err != nil || len(dirs) == 0 {
		return err
	}
	paths := dirs
	failed := 0
	if recursive {
		paths, failed = renameReadFolders(dirs)
	}
	return a.onScope(ctx, func() {
		a.renameIngest(paths)
		if failed > 0 {
			a.fileRename.err = strconv.Itoa(failed) + " 个项目读取失败或包含待恢复的暂存目录，已跳过"
		}
		a.emit("rename")
	})
}

func renameIngestPathsBridge(paths []string) {
	if a := getApp(); a != nil {
		a.renameIngest(paths)
		a.emit("rename")
	}
}

func renameClearBridge() {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return
	}
	a.fileRename.items = nil
	a.fileRename.undo = nil
	a.fileRename.err = ""
	a.fileRename.note = ""
	a.emit("rename")
}

func renameRemoveAtBridge(index int) {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return
	}
	if index >= 0 && index < len(a.fileRename.items) {
		a.fileRename.items = append(a.fileRename.items[:index], a.fileRename.items[index+1:]...)
		a.emit("rename")
	}
}

func renameSetFieldBridge(field, value string) {
	a := getApp()
	if a == nil {
		return
	}
	s := &a.fileRename
	s.init()
	switch field {
	case "mode":
		s.mode = clampInt(parseIntDefault(value, 0), 0, 4)
	case "includeExt":
		s.includeExt = value == "true" || value == "1"
	case "insertKind":
		s.insertKind = value
	case "position":
		s.position = value
	case "index":
		s.index = float64(max(1, parseIntDefault(value, 1)))
	case "text":
		s.text = value
	case "prefix":
		s.prefix = value
	case "suffix":
		s.suffix = value
	case "numFormat":
		s.numFormat = value
	case "numStart":
		s.numStart = float64(max(0, parseIntDefault(value, 1)))
	case "numWidth":
		s.numWidth = float64(clampInt(parseIntDefault(value, 0), 0, 12))
	case "numPrefix":
		s.numPrefix = value
	case "numSuffix":
		s.numSuffix = value
	case "sortBy":
		s.sortBy = value
		a.renameSort()
	case "aiPrompt":
		s.aiPrompt = value
	case "aiSettings":
		s.aiSettings = value == "true" || value == "1"
	case "aiURL":
		a.customURL = strings.TrimSpace(value)
	case "aiKey":
		a.customKey = strings.TrimSpace(value)
	case "aiModel":
		a.customModel = strings.TrimSpace(value)
	}
	a.emit("rename")
}

func renameSetRuleBridge(index int, find, with, text string, template, count, offset int) {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return
	}
	s := &a.fileRename
	s.init()
	if index >= 0 && index < len(s.rules) {
		s.rules[index] = renameRule{
			Find:     find,
			With:     with,
			Template: clampInt(template, 0, len(renameTemplateLabels)-1),
			Count:    max(1, count),
			Offset:   max(1, offset),
			Text:     text,
		}
		a.emit("rename")
	}
}

func renameAddRuleBridge() {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return
	}
	a.fileRename.init()
	a.fileRename.rules = append(a.fileRename.rules, renameRule{Count: 1, Offset: 1})
	a.emit("rename")
}

func renameRemoveRuleBridge(index int) {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return
	}
	s := &a.fileRename
	if len(s.rules) > 1 && index >= 0 && index < len(s.rules) {
		s.rules = append(s.rules[:index], s.rules[index+1:]...)
		a.emit("rename")
	}
}

func renameSetDraftBridge(index int, draft string) {
	a := getApp()
	if a == nil || a.fileRename.busy {
		return
	}
	if index >= 0 && index < len(a.fileRename.items) {
		a.fileRename.items[index].draft = draft
		a.emit("rename")
	}
}

func renamePasteLinesBridge(start int, text string) {
	if a := getApp(); a != nil {
		a.renamePasteAt(start, text)
		a.emit("rename")
	}
}

func renameMoveRowBridge(from, to int) {
	if a := getApp(); a != nil {
		a.renameReorder([]int{from}, to)
		a.emit("rename")
	}
}

func renameSuggestAIBridge() {
	if a := getApp(); a != nil {
		a.renameSuggest()
	}
}

func renameSaveAIModelBridge(url, key, model string) {
	if a := getApp(); a != nil {
		a.customURL = strings.TrimSpace(url)
		a.customKey = strings.TrimSpace(key)
		a.customModel = strings.TrimSpace(model)
		a.saveRenameModel()
		a.emit("rename")
	}
}

func renameApplyBridge() {
	a := getApp()
	if a == nil {
		return
	}
	changes, err := a.renamePlan()
	if err != nil {
		a.fileRename.err = err.Error()
		a.emit("rename")
		return
	}
	a.renameApply(changes, false)
}

func renameUndoBridge() {
	a := getApp()
	if a == nil || len(a.fileRename.undo) == 0 {
		return
	}
	a.renameApply(a.fileRename.undo, true)
}
