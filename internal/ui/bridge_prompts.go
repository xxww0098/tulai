package ui

import (
	"context"
	"strings"

	"github.com/xxww0098/tulai/internal/prompts"
)

// PromptsService exposes prompt template library management IPCs.
type PromptsService struct {
	app *App
}

// PromptTemplateDTO represents a built-in or user-defined prompt template.
type PromptTemplateDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	KindLbl string `json:"kindLbl"`
	Text    string `json:"text"`
	Builtin bool   `json:"builtin"`
}

// PromptsStateDTO represents the prompt library page snapshot.
type PromptsStateDTO struct {
	Items         []PromptTemplateDTO `json:"items"`
	SelectedID    string              `json:"selectedId"`
	SelectedName  string              `json:"selectedName"`
	SelectedKind  string              `json:"selectedKind"`
	SelectedText  string              `json:"selectedText"`
	SelectedIsBln bool                `json:"selectedIsBln"`
	Filter        string              `json:"filter"`
	Note          string              `json:"note"`
	Suffix        string              `json:"suffix"`
	UseSuffix     bool                `json:"useSuffix"`
}

func (a *App) selectPromptInternal(id string) {
	a.promptEditNote = ""
	a.promptEditPick = id
	if tpl, ok := resolvePromptTemplate(id, a.userPrompts); ok {
		a.promptEditName = tpl.Label
		a.promptEditKind = promptKindLabel(tpl.Kind)
		a.promptEditDraft = tpl.Body
	} else {
		a.promptEditName = ""
		a.promptEditKind = "通用"
		a.promptEditDraft = ""
	}
}

// GetPromptsState returns the filtered prompt template library and current editor state.
func (s *PromptsService) GetPromptsState(ctx context.Context) PromptsStateDTO {
	if s.app == nil {
		return PromptsStateDTO{Items: []PromptTemplateDTO{}}
	}
	out, err := callValue(s.app, ctx, func() PromptsStateDTO {
		a := s.app
		all := prompts.Catalog(a.userPrompts)
		filter := strings.ToLower(strings.TrimSpace(a.promptFilter))
		items := make([]PromptTemplateDTO, 0, len(all))
		for _, tpl := range all {
			if tpl.ID == "" {
				continue
			}
			kl := promptKindLabel(tpl.Kind)
			if filter != "" && !strings.Contains(strings.ToLower(tpl.Label+" "+tpl.Body+" "+kl), filter) {
				continue
			}
			items = append(items, PromptTemplateDTO{
				ID:      tpl.ID,
				Name:    tpl.Label,
				Label:   tpl.Label,
				Kind:    tpl.Kind,
				KindLbl: kl,
				Text:    tpl.Body,
				Builtin: tpl.Builtin,
			})
		}
		if a.promptEditPick == "" && len(items) > 0 {
			a.selectPromptInternal(items[0].ID)
		}
		return PromptsStateDTO{
			Items:         items,
			SelectedID:    a.promptEditPick,
			SelectedName:  a.promptEditName,
			SelectedKind:  a.promptEditKind,
			SelectedText:  a.promptEditDraft,
			SelectedIsBln: isBuiltinPrompt(a.promptEditPick),
			Filter:        a.promptFilter,
			Note:          a.promptEditNote,
			Suffix:        a.promptSuffix,
			UseSuffix:     a.useSuffix,
		}
	})
	if err != nil {
		return PromptsStateDTO{Items: []PromptTemplateDTO{}}
	}
	return out
}

// SelectPrompt selects a template in the prompt library editor.
func (s *PromptsService) SelectPrompt(ctx context.Context, id string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.selectPromptInternal(id)
		s.app.emit("prompts")
	})
}

// SavePromptEdits persists edits to the currently selected prompt template.
func (s *PromptsService) SavePromptEdits(ctx context.Context, name, kindLbl, draft string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		a.promptEditName = name
		a.promptEditKind = kindLbl
		a.promptEditDraft = draft
		if err := a.commitPromptEdits(); err != "" {
			a.promptEditNote = err
		}
		a.emit("prompts", "workbench")
	})
}

// CreateNewPrompt adds a new user-defined prompt template.
func (s *PromptsService) CreateNewPrompt(ctx context.Context, name, kindLbl, body string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		a.promptDraftName = name
		a.promptDraftKind = kindLbl
		a.promptEditDraft = body
		a.saveCurrentPrompt()
		a.emit("prompts", "workbench")
	})
}

// RestoreBuiltinPrompt restores the selected built-in template back to its default text.
func (s *PromptsService) RestoreBuiltinPrompt(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.restoreBuiltinPrompt()
		s.app.emit("prompts")
	})
}

// DeleteUserPrompt deletes the currently selected user-defined prompt template.
func (s *PromptsService) DeleteUserPrompt(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.deleteUserPrompt()
		s.app.emit("prompts", "workbench")
	})
}

// SetPromptFilter updates the search query in the prompt library.
func (s *PromptsService) SetPromptFilter(ctx context.Context, q string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.promptFilter = q
		s.app.emit("prompts")
	})
}

// SetPromptSuffix updates and persists the global prompt suffix and enable flag.
func (s *PromptsService) SetPromptSuffix(ctx context.Context, suffix string, use bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		a.promptSuffix = suffix
		a.useSuffix = use
		a.persistPromptLibrary()
		a.emit("prompts", "workbench")
	})
}

// ---- Test bridge wrappers ----

func getPromptsStateBridge() PromptsStateDTO {
	return (&PromptsService{app: getApp()}).GetPromptsState(context.Background())
}

func createNewPromptBridge(name, kindLbl, body string) {
	(&PromptsService{app: getApp()}).CreateNewPrompt(context.Background(), name, kindLbl, body)
}
