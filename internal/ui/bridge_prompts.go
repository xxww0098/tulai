package ui

import (
	"strings"

	"github.com/nsgo-dev/nsgo/tsx"

	"github.com/xxww0098/tulai/internal/prompts"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getPromptsState":      getPromptsStateBridge,
		"selectPrompt":         selectPromptBridge,
		"savePromptEdits":      savePromptEditsBridge,
		"createNewPrompt":      createNewPromptBridge,
		"restoreBuiltinPrompt": restoreBuiltinPromptBridge,
		"deleteUserPrompt":     deleteUserPromptBridge,
		"setPromptFilter":      setPromptFilterBridge,
		"setPromptSuffix":      setPromptSuffixBridge,
	})
}

type PromptTemplateDTO struct {
	ID      string
	Name    string
	Label   string
	Kind    string
	KindLbl string
	Text    string
	Builtin bool
}

type PromptsStateDTO struct {
	Items         []PromptTemplateDTO
	SelectedID    string
	SelectedName  string
	SelectedKind  string
	SelectedText  string
	SelectedIsBln bool
	Filter        string
	Note          string
	Suffix        string
	UseSuffix     bool
}

func getPromptsStateBridge() PromptsStateDTO {
	a := getApp()
	if a == nil {
		return PromptsStateDTO{}
	}
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
		selectPromptBridge(items[0].ID)
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
}

func selectPromptBridge(id string) {
	a := getApp()
	if a == nil {
		return
	}
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
	a.emit("prompts")
}

func savePromptEditsBridge(name, kindLbl, draft string) {
	a := getApp()
	if a == nil {
		return
	}
	a.promptEditName = name
	a.promptEditKind = kindLbl
	a.promptEditDraft = draft
	if err := a.commitPromptEdits(); err != "" {
		a.promptEditNote = err
	}
	a.emit("prompts", "workbench")
}

func createNewPromptBridge(name, kindLbl, body string) {
	a := getApp()
	if a == nil {
		return
	}
	a.promptDraftName = name
	a.promptDraftKind = kindLbl
	a.promptEditDraft = body
	a.saveCurrentPrompt()
	a.emit("prompts", "workbench")
}

func restoreBuiltinPromptBridge() {
	if a := getApp(); a != nil {
		a.restoreBuiltinPrompt()
		a.emit("prompts")
	}
}

func deleteUserPromptBridge() {
	if a := getApp(); a != nil {
		a.deleteUserPrompt()
		a.emit("prompts", "workbench")
	}
}

func setPromptFilterBridge(q string) {
	if a := getApp(); a != nil {
		a.promptFilter = q
		a.emit("prompts")
	}
}

func setPromptSuffixBridge(suffix string, use bool) {
	a := getApp()
	if a == nil {
		return
	}
	a.promptSuffix = suffix
	a.useSuffix = use
	a.persistPromptLibrary()
	a.emit("prompts", "workbench")
}
