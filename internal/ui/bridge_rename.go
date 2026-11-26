package ui

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
)

// RenameService exposes batch file renaming, rule planning, AI suggestions, and undo IPCs.
type RenameService struct {
	app *App
}

// RenameRuleDTO represents one search/replace rule entry.
type RenameRuleDTO struct {
	Index    int    `json:"index"`
	Find     string `json:"find"`
	With     string `json:"with"`
	Template int    `json:"template"`
	Count    int    `json:"count"`
	Offset   int    `json:"offset"`
	Text     string `json:"text"`
}

// RenameRowDTO represents one file row and its live planned target name.
type RenameRowDTO struct {
	Index   int    `json:"index"`
	Path    string `json:"path"`
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
	Draft   string `json:"draft"`
	Changed bool   `json:"changed"`
}

// RenameStateDTO represents the full batch rename page snapshot.
type RenameStateDTO struct {
	Mode          int             `json:"mode"`
	Busy          bool            `json:"busy"`
	IncludeExt    bool            `json:"includeExt"`
	InsertKind    string          `json:"insertKind"`
	Position      string          `json:"position"`
	Index         int             `json:"index"`
	Text          string          `json:"text"`
	Prefix        string          `json:"prefix"`
	Suffix        string          `json:"suffix"`
	NumFormat     string          `json:"numFormat"`
	NumStart      int             `json:"numStart"`
	NumWidth      int             `json:"numWidth"`
	NumPrefix     string          `json:"numPrefix"`
	NumSuffix     string          `json:"numSuffix"`
	SortBy        string          `json:"sortBy"`
	AIPrompt      string          `json:"aiPrompt"`
	AISettings    bool            `json:"aiSettings"`
	AIURL         string          `json:"aiUrl"`
	AIKey         string          `json:"aiKey"`
	AIModel       string          `json:"aiModel"`
	Rules         []RenameRuleDTO `json:"rules"`
	Rows          []RenameRowDTO  `json:"rows"`
	ChangedCount  int             `json:"changedCount"`
	CanUndo       bool            `json:"canUndo"`
	PlanErr       string          `json:"planErr"`
	Note          string          `json:"note"`
	Err           string          `json:"err"`
	TemplateNames []string        `json:"templateNames"`
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

func emptyRenameState() RenameStateDTO {
	return RenameStateDTO{
		Rules:         []RenameRuleDTO{},
		Rows:          []RenameRowDTO{},
		TemplateNames: append([]string(nil), renameTemplateLabels...),
	}
}

// GetRenameState returns the current batch rename items, rules, and planned preview.
func (s *RenameService) GetRenameState(ctx context.Context) RenameStateDTO {
	if s.app == nil {
		return emptyRenameState()
	}
	out, err := callValue(s.app, ctx, func() RenameStateDTO {
		a := s.app
		st := &a.fileRename
		st.init()

		changes, planErr := a.renamePlan()
		planErrStr := ""
		if planErr != nil {
			planErrStr = planErr.Error()
		}

		rules := make([]RenameRuleDTO, 0, len(st.rules))
		for i, r := range st.rules {
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

		rows := make([]RenameRowDTO, 0, len(st.items))
		changedCount := 0
		for i, item := range st.items {
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
			Mode:          st.mode,
			Busy:          st.busy,
			IncludeExt:    st.includeExt,
			InsertKind:    st.insertKind,
			Position:      st.position,
			Index:         int(st.index),
			Text:          st.text,
			Prefix:        st.prefix,
			Suffix:        st.suffix,
			NumFormat:     st.numFormat,
			NumStart:      int(st.numStart),
			NumWidth:      int(st.numWidth),
			NumPrefix:     st.numPrefix,
			NumSuffix:     st.numSuffix,
			SortBy:        st.sortBy,
			AIPrompt:      st.aiPrompt,
			AISettings:    st.aiSettings,
			AIURL:         a.customURL,
			AIKey:         a.customKey,
			AIModel:       a.customModel,
			Rules:         rules,
			Rows:          rows,
			ChangedCount:  changedCount,
			CanUndo:       len(st.undo) > 0,
			PlanErr:       planErrStr,
			Note:          st.note,
			Err:           st.err,
			TemplateNames: append([]string(nil), renameTemplateLabels...),
		}
	})
	if err != nil {
		return emptyRenameState()
	}
	return out
}

// RenamePickFiles opens a multi-file picker and adds selected files to the rename list.
func (s *RenameService) RenamePickFiles(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	busy, err := callValue(s.app, ctx, func() bool { return s.app.fileRename.busy })
	if err != nil || busy {
		return err
	}
	paths, err := s.app.pickFilesAsync(ctx, "选择要重命名的文件", true, nil, "所有文件")
	if err != nil || len(paths) == 0 {
		return err
	}
	return s.app.call(ctx, func() {
		s.app.renameIngest(paths)
		s.app.emit("rename")
	})
}

// RenamePickFolder opens a folder picker and adds files from selected folders.
func (s *RenameService) RenamePickFolder(ctx context.Context, recursive bool) error {
	if s.app == nil {
		return nil
	}
	busy, err := callValue(s.app, ctx, func() bool { return s.app.fileRename.busy })
	if err != nil || busy {
		return err
	}
	dirs, err := s.app.pickDirsAsync(ctx, "选择包含文件的文件夹", true)
	if err != nil || len(dirs) == 0 {
		return err
	}
	paths := dirs
	failed := 0
	if recursive {
		paths, failed = renameReadFolders(dirs)
	}
	return s.app.call(ctx, func() {
		s.app.renameIngest(paths)
		if failed > 0 {
			s.app.fileRename.err = strconv.Itoa(failed) + " 个项目读取失败或包含待恢复的暂存目录，已跳过"
		}
		s.app.emit("rename")
	})
}

// RenameIngestPaths adds dropped or supplied paths to the rename list.
func (s *RenameService) RenameIngestPaths(ctx context.Context, paths []string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.renameIngest(paths)
		s.app.emit("rename")
	})
}

// RenameClear empties the rename list and undo history.
func (s *RenameService) RenameClear(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if s.app.fileRename.busy {
			return
		}
		s.app.fileRename.items = nil
		s.app.fileRename.undo = nil
		s.app.fileRename.err = ""
		s.app.fileRename.note = ""
		s.app.emit("rename")
	})
}

// RenameRemoveAt removes a file row by index.
func (s *RenameService) RenameRemoveAt(ctx context.Context, index int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if s.app.fileRename.busy {
			return
		}
		if index >= 0 && index < len(s.app.fileRename.items) {
			s.app.fileRename.items = append(s.app.fileRename.items[:index], s.app.fileRename.items[index+1:]...)
			s.app.emit("rename")
		}
	})
}

// RenameSetField updates a rename parameter or AI rename configuration field.
func (s *RenameService) RenameSetField(ctx context.Context, field, value string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		st := &a.fileRename
		st.init()
		switch field {
		case "mode":
			st.mode = clampInt(parseIntDefault(value, 0), 0, 4)
		case "includeExt":
			st.includeExt = value == "true" || value == "1"
		case "insertKind":
			st.insertKind = value
		case "position":
			st.position = value
		case "index":
			st.index = float64(max(1, parseIntDefault(value, 1)))
		case "text":
			st.text = value
		case "prefix":
			st.prefix = value
		case "suffix":
			st.suffix = value
		case "numFormat":
			st.numFormat = value
		case "numStart":
			st.numStart = float64(max(0, parseIntDefault(value, 1)))
		case "numWidth":
			st.numWidth = float64(clampInt(parseIntDefault(value, 0), 0, 12))
		case "numPrefix":
			st.numPrefix = value
		case "numSuffix":
			st.numSuffix = value
		case "sortBy":
			st.sortBy = value
			a.renameSort()
		case "aiPrompt":
			st.aiPrompt = value
		case "aiSettings":
			st.aiSettings = value == "true" || value == "1"
		case "aiURL", "aiUrl":
			a.customURL = strings.TrimSpace(value)
		case "aiKey":
			a.customKey = strings.TrimSpace(value)
		case "aiModel":
			a.customModel = strings.TrimSpace(value)
		}
		a.emit("rename")
	})
}

// RenameSetRule updates one search/replace rule entry by index.
func (s *RenameService) RenameSetRule(ctx context.Context, index int, find, with, text string, template, count, offset int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		st := &s.app.fileRename
		if st.busy {
			return
		}
		st.init()
		if index >= 0 && index < len(st.rules) {
			st.rules[index] = renameRule{
				Find:     find,
				With:     with,
				Template: clampInt(template, 0, len(renameTemplateLabels)-1),
				Count:    max(0, count),
				Offset:   max(0, offset),
				Text:     text,
			}
			s.app.emit("rename")
		}
	})
}

// RenameAddRule appends a new blank search/replace rule.
func (s *RenameService) RenameAddRule(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if s.app.fileRename.busy {
			return
		}
		s.app.fileRename.init()
		s.app.fileRename.rules = append(s.app.fileRename.rules, renameRule{Count: 1, Offset: 0})
		s.app.emit("rename")
	})
}

// RenameRemoveRule deletes a search/replace rule by index (leaving at least one rule).
func (s *RenameService) RenameRemoveRule(ctx context.Context, index int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		st := &s.app.fileRename
		if st.busy {
			return
		}
		if len(st.rules) > 1 && index >= 0 && index < len(st.rules) {
			st.rules = append(st.rules[:index], st.rules[index+1:]...)
			s.app.emit("rename")
		}
	})
}

// RenameSetDraft updates the manual target filename draft for one row.
func (s *RenameService) RenameSetDraft(ctx context.Context, index int, draft string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if s.app.fileRename.busy {
			return
		}
		if index >= 0 && index < len(s.app.fileRename.items) {
			s.app.fileRename.items[index].draft = draft
			s.app.emit("rename")
		}
	})
}

// RenamePasteLines pastes newline-delimited names starting at the given row index.
func (s *RenameService) RenamePasteLines(ctx context.Context, start int, text string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.renamePasteAt(start, text)
		s.app.emit("rename")
	})
}

// RenameMoveRow reorders a file row from one index to another.
func (s *RenameService) RenameMoveRow(ctx context.Context, from, to int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.renameReorder([]int{from}, to)
		s.app.emit("rename")
	})
}

// RenameSuggestAI requests AI-generated target filenames in the background.
func (s *RenameService) RenameSuggestAI(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.renameSuggest()
	})
}

// RenameSaveAIModel persists the custom AI model endpoint, API key, and model ID.
func (s *RenameService) RenameSaveAIModel(ctx context.Context, url, key, model string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.customURL = strings.TrimSpace(url)
		s.app.customKey = strings.TrimSpace(key)
		s.app.customModel = strings.TrimSpace(model)
		s.app.saveRenameModel()
		s.app.emit("rename")
	})
}

// RenameApply validates and executes the planned batch rename operation.
func (s *RenameService) RenameApply(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		changes, err := s.app.renamePlan()
		if err != nil {
			s.app.fileRename.err = err.Error()
			s.app.emit("rename")
			return
		}
		s.app.renameApply(changes, false)
	})
}

// RenameUndo reverts the last applied batch rename operation.
func (s *RenameService) RenameUndo(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if len(s.app.fileRename.undo) == 0 {
			return
		}
		s.app.renameApply(s.app.fileRename.undo, true)
	})
}
