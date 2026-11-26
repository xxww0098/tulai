package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/xxww0098/tulai/internal/prompts"
)

type promptPart struct {
	Enabled bool   `json:"enabled"`
	Value   string `json:"value"`
}

type promptParts struct {
	Subject    promptPart `json:"subject"`
	Background promptPart `json:"background"`
	Style      promptPart `json:"style"`
	Light      promptPart `json:"light"`
	Layout     promptPart `json:"layout"`
	Text       promptPart `json:"text"`
	Other      promptPart `json:"other"`
}

type promptPartRow struct {
	name string
	part *promptPart
}

func defaultPromptParts() promptParts {
	return promptParts{
		Subject:    promptPart{Value: "主体突出，关键特征清晰可辨"},
		Background: promptPart{Value: "纯白背景"},
		Style:      promptPart{Value: "写实摄影"},
		Light:      promptPart{Value: "光线柔和均匀"},
		Layout:     promptPart{Value: "主体居中，完整展示"},
		Text:       promptPart{Value: "不新增文字、标志或水印"},
	}
}

func (p *promptParts) rows() []promptPartRow {
	return []promptPartRow{
		{"主体", &p.Subject}, {"背景", &p.Background}, {"风格", &p.Style},
		{"光线", &p.Light}, {"构图", &p.Layout}, {"文字", &p.Text}, {"其他", &p.Other},
	}
}

func (p *promptParts) validate() error {
	for _, row := range p.rows() {
		if row.part.Enabled && strings.TrimSpace(row.part.Value) == "" {
			return fmt.Errorf("请填写已勾选的%s参数", row.name)
		}
	}
	return nil
}

func (p *promptParts) text() string {
	var lines []string
	for _, row := range p.rows() {
		if value := strings.TrimSpace(row.part.Value); row.part.Enabled && value != "" {
			lines = append(lines, row.name+"："+value)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (a *App) loadPromptParts() {
	a.structuredGen, a.structuredWash = defaultPromptParts(), defaultPromptParts()
	if a.store != nil {
		for _, item := range []struct {
			key string
			dst *promptParts
		}{{"structured_prompt_gen", &a.structuredGen}, {"structured_prompt_wash", &a.structuredWash}} {
			if raw := a.store.Get(item.key); raw != "" {
				candidate := defaultPromptParts()
				if json.Unmarshal([]byte(raw), &candidate) == nil {
					*item.dst = candidate
				}
			}
		}
	}
	a.structuredReady = true
}

func (a *App) savePromptParts() {
	if a.store == nil {
		return
	}
	if !a.structuredReady {
		a.loadPromptParts()
	}
	for _, item := range []struct {
		key string
		src promptParts
	}{{"structured_prompt_gen", a.structuredGen}, {"structured_prompt_wash", a.structuredWash}} {
		if raw, err := json.Marshal(item.src); err == nil {
			a.store.Set(item.key, string(raw))
		}
	}
}

func (a *App) currentPromptParts() *promptParts {
	if !a.structuredReady {
		a.loadPromptParts()
	}
	if a.statePage == pageWash {
		return &a.structuredWash
	}
	return &a.structuredGen
}

func (a *App) promptPartsForPage(page int) *promptParts {
	if !a.structuredReady {
		a.loadPromptParts()
	}
	if page == pageWash {
		return &a.structuredWash
	}
	return &a.structuredGen
}

const (
	newPromptLabel      = "新建提示词"
	choosePromptLabel   = "选择提示词"
	linePromptID        = "__line_prompts__"
	detachedPromptID    = "__workspace_prompt__"
	detachedPromptLabel = "当前提示词（整段发送）"
)

func promptKindLabel(kind string) string {
	switch kind {
	case prompts.KindGen:
		return "生图"
	case prompts.KindWash:
		return "洗图"
	case prompts.KindFission:
		return "裂变"
	default:
		return "通用"
	}
}

func promptKind(label string) string {
	switch label {
	case "生图":
		return prompts.KindGen
	case "洗图":
		return prompts.KindWash
	case "裂变":
		return prompts.KindFission
	default:
		return ""
	}
}

func promptOptions(first string, users []prompts.UserPrompt, extra ...prompts.Template) []prompts.Template {
	options := prompts.Catalog(users)
	options[0].Label = first
	options = append(options, extra...)
	used := make(map[string]bool, len(options))
	for i := range options {
		label := options[i].Label
		for used[label] {
			label += "（" + options[i].ID + "）"
		}
		options[i].Label = label
		used[label] = true
	}
	return options
}

func workspacePromptOptions(kind string, users []prompts.UserPrompt, extra ...prompts.Template) []prompts.Template {
	options := promptOptions(choosePromptLabel, users, extra...)
	filtered := options[:0]
	for _, option := range options {
		category := promptKind(promptKindLabel(option.Kind))
		if category == "" || category == kind {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func promptOptionID(label string, options []prompts.Template) string {
	for _, option := range options {
		if option.Label == label {
			return option.ID
		}
	}
	return ""
}

func promptLabels(options []prompts.Template) []string {
	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = option.Label
	}
	return labels
}

func promptOptionLabel(id string, options []prompts.Template) string {
	for _, option := range options {
		if option.ID == id {
			return option.Label
		}
	}
	return ""
}

func (a *App) taskPromptList() []string {
	if a.statePage == pageFission {
		if a.skipImages {
			return prompts.FissionTextPrompts(a.prompts)
		}
		return prompts.FissionPrompts(a.fissionPrompt())
	}
	if a.statePage == pageWash {
		return []string{a.washPrompt()}
	}
	if a.statePage == pageHome && a.batchMode == batchImage {
		if normalizeImagePromptMode(a.imagePromptMode) == "per_line" && strings.TrimSpace(a.imgPromptPick) == "" {
			return splitLines(a.prompts)
		}
		if p := strings.TrimSpace(a.prompts); p != "" {
			return []string{p}
		}
		return nil
	}
	if strings.TrimSpace(a.promptPick) != "" {
		if p := strings.TrimSpace(a.prompts); p != "" {
			return []string{p}
		}
		return nil
	}
	return splitLines(a.prompts)
}

func resolvePromptTemplate(id string, users []prompts.UserPrompt) (prompts.Template, bool) {
	if id == "" {
		return prompts.Template{}, false
	}
	for _, tpl := range prompts.Catalog(users) {
		if tpl.ID == id {
			return tpl, true
		}
	}
	return prompts.Template{}, false
}

func isBuiltinPrompt(id string) bool {
	_, ok := prompts.Builtin(id)
	return ok
}

func (a *App) workbenchModeKind() string {
	switch a.statePage {
	case pageWash:
		return prompts.KindWash
	case pageFission:
		return prompts.KindFission
	default:
		return prompts.KindGen
	}
}

func (a *App) currentModePromptChoice() string {
	switch a.statePage {
	case pageWash:
		return a.washPromptChoice
	case pageFission:
		return a.fissionPromptChoice
	default:
		if a.batchMode == batchImage {
			return a.imgGenPromptChoice
		}
		return a.genPromptChoice
	}
}

func (a *App) resolvedPromptChoiceFor(pick string) string {
	if strings.TrimSpace(pick) == "" {
		return choosePromptLabel
	}
	label := promptOptionLabel(pick, promptOptions(choosePromptLabel, a.userPrompts))
	if label == "" {
		return choosePromptLabel
	}
	return label
}

func (a *App) resolvedGenPromptChoice() string {
	label := a.resolvedPromptChoiceFor(a.promptPick)
	if label == choosePromptLabel {
		a.promptPick = ""
		a.promptChoice = ""
	}
	return label
}

func workbenchPromptItems(kind string, users []prompts.UserPrompt) ([]prompts.Template, []PromptOptionDTO) {
	all := promptOptions(choosePromptLabel, users)
	matched := make([]prompts.Template, 0, len(all))
	other := make([]prompts.Template, 0, len(all))
	for _, tpl := range all[1:] {
		kl := promptKindLabel(tpl.Kind)
		cat := promptKind(kl)
		if cat == "" || cat == kind {
			matched = append(matched, tpl)
		} else if !tpl.Builtin {
			other = append(other, tpl)
		}
	}
	selectable := make([]prompts.Template, 0, len(matched)+len(other))
	dtos := make([]PromptOptionDTO, 0, len(matched)+len(other))
	appendGroup := func(items []prompts.Template, isMatched bool) {
		for _, tpl := range items {
			selectable = append(selectable, tpl)
			dtos = append(dtos, PromptOptionDTO{
				ID:      tpl.ID,
				Label:   tpl.Label,
				KindLbl: promptKindLabel(tpl.Kind),
				Builtin: tpl.Builtin,
				Matched: isMatched,
			})
		}
	}
	appendGroup(matched, true)
	appendGroup(other, false)
	return selectable, dtos
}

func (a *App) clearEditedPromptChoice(page int, value string) {
	if page == pageHome {
		if a.batchMode == batchImage {
			if a.imgPromptPick == "" && (a.imgGenPromptChoice == "" || a.imgGenPromptChoice == choosePromptLabel) {
				return
			}
			if strings.TrimSpace(value) == "" {
				a.imgPromptPick = ""
				a.imgGenPromptChoice = choosePromptLabel
				a.imgFreeformPrompts = ""
				return
			}
			tpl, ok := resolvePromptTemplate(a.imgPromptPick, a.userPrompts)
			if !ok || !prompts.SameText(value, tpl.Body) {
				a.imgPromptPick = ""
				a.imgGenPromptChoice = choosePromptLabel
				a.imgFreeformPrompts = value
			}
			return
		}
		if a.promptPick == "" && (a.genPromptChoice == "" || a.genPromptChoice == choosePromptLabel) {
			return
		}
		if strings.TrimSpace(value) == "" {
			a.promptPick = ""
			a.promptChoice = ""
			a.genPromptChoice = choosePromptLabel
			a.freeformPrompts = ""
			return
		}
		tpl, ok := resolvePromptTemplate(a.promptPick, a.userPrompts)
		if !ok || !prompts.SameText(value, tpl.Body) {
			a.promptPick = ""
			a.promptChoice = ""
			a.genPromptChoice = choosePromptLabel
			a.freeformPrompts = value
		}
		return
	}
	var choice *string
	switch page {
	case pageWash:
		choice = &a.washPromptChoice
	case pageFission:
		choice = &a.fissionPromptChoice
	default:
		return
	}
	if *choice == "" || *choice == choosePromptLabel {
		return
	}
	all := promptOptions(choosePromptLabel, a.userPrompts)
	id := promptOptionID(*choice, all)
	if id == "" {
		*choice = choosePromptLabel
		return
	}
	tpl, ok := resolvePromptTemplate(id, a.userPrompts)
	if !ok || !prompts.SameText(value, tpl.Body) {
		*choice = choosePromptLabel
	}
}

func (a *App) detachActiveFreeTemplate() {
	if a.statePage != pageHome {
		return
	}
	if a.batchMode == batchImage {
		a.imgPromptPick = ""
		a.imgGenPromptChoice = choosePromptLabel
		a.imgFreeformPrompts = a.prompts
		a.imgPrompts = a.prompts
		return
	}
	a.promptPick = ""
	a.promptChoice = ""
	a.genPromptChoice = choosePromptLabel
	a.freeformPrompts = a.prompts
	a.textPrompts = a.prompts
}

func (a *App) applyWorkbenchPrompt(label string) {
	page := a.statePage
	if a.running && a.runPage == page {
		return
	}
	selectable, _ := workbenchPromptItems(a.workbenchModeKind(), a.userPrompts)
	if label == "" || label == choosePromptLabel {
		switch page {
		case pageWash:
			a.washPromptChoice = choosePromptLabel
			a.prompts = a.defaultWashPrompt()
		case pageFission:
			a.fissionPromptChoice = choosePromptLabel
			if a.skipImages {
				a.prompts = ""
			} else {
				a.prompts = DefaultFissionPrompt
			}
		default:
			if a.batchMode == batchImage {
				a.imgPromptPick = ""
				a.imgGenPromptChoice = choosePromptLabel
				a.prompts = a.imgFreeformPrompts
				a.imgPrompts = a.prompts
			} else {
				a.promptPick = ""
				a.promptChoice = ""
				a.genPromptChoice = choosePromptLabel
				a.prompts = a.freeformPrompts
				a.textPrompts = a.prompts
			}
		}
		return
	}
	id := promptOptionID(label, selectable)
	if id == "" {
		return
	}
	tpl, ok := resolvePromptTemplate(id, a.userPrompts)
	if !ok {
		return
	}
	switch page {
	case pageWash:
		a.washPromptChoice = label
		a.prompts = tpl.Body
	case pageFission:
		a.fissionPromptChoice = label
		a.prompts = tpl.Body
	default:
		if a.batchMode == batchImage {
			if a.imgPromptPick == "" {
				a.imgFreeformPrompts = a.prompts
			}
			a.imgPromptPick = tpl.ID
			a.imgGenPromptChoice = label
			a.prompts = tpl.Body
			a.imgPrompts = tpl.Body
		} else {
			if a.promptPick == "" {
				a.freeformPrompts = a.prompts
			}
			a.promptPick = tpl.ID
			a.promptChoice = tpl.ID
			a.genPromptChoice = label
			a.prompts = tpl.Body
			a.textPrompts = tpl.Body
		}
	}
}

func (a *App) applyPromptChoice() {
	a.applyWorkbenchPrompt(a.currentModePromptChoice())
}

func (a *App) commitPromptEdits() string {
	a.promptEditNote = ""
	if a.promptEditPick == "" {
		return ""
	}
	body := strings.TrimSpace(a.promptEditDraft)
	if body == "" {
		return "提示词内容不能为空"
	}
	if isBuiltinPrompt(a.promptEditPick) {
		u := a.addUserCopy(a.promptEditPick, body)
		a.promptEditPick = u.ID
		a.promptEditName = u.Label
		a.promptEditKind = promptKindLabel(u.Kind)
		a.promptEditDraft = u.Body
		a.promptEditNote = "已另存为「" + u.Label + "」"
		a.syncWorkspacePrompt(u)
		a.persistPromptLibrary()
		return ""
	}
	for i := range a.userPrompts {
		if a.userPrompts[i].ID == a.promptEditPick {
			a.updateUserPrompt(i)
			a.syncWorkspacePrompt(a.userPrompts[i])
			a.persistPromptLibrary()
			return ""
		}
	}
	return ""
}

func (a *App) syncWorkspacePrompt(u prompts.UserPrompt) {
	label := promptOptionLabel(u.ID, promptOptions(choosePromptLabel, a.userPrompts))
	if a.promptPick == u.ID {
		a.promptChoice = u.ID
		a.genPromptChoice = label
		a.textPrompts = u.Body
		if a.batchMode == batchText {
			a.setHomePrompts(u.Body)
		}
	}
	if a.imgPromptPick == u.ID {
		a.imgGenPromptChoice = label
		a.imgPrompts = u.Body
		if a.batchMode == batchImage {
			a.setHomePrompts(u.Body)
		}
	}
	for plat, ws := range a.platformSpaces {
		if plat == a.selectedPlatform() {
			continue
		}
		changed := false
		if ws.promptPick == u.ID {
			ws.promptChoice = u.ID
			ws.genPromptChoice = label
			ws.textPrompts = u.Body
			if ws.batchMode == batchText {
				if gen, ok := ws.pages[pageHome]; ok {
					gen.prompts = u.Body
					ws.pages[pageHome] = gen
				}
			}
			changed = true
		}
		if ws.imgPromptPick == u.ID {
			ws.imgGenPromptChoice = label
			ws.imgPrompts = u.Body
			if ws.batchMode == batchImage {
				if gen, ok := ws.pages[pageHome]; ok {
					gen.prompts = u.Body
					ws.pages[pageHome] = gen
				}
			}
			changed = true
		}
		if changed {
			a.platformSpaces[plat] = ws
		}
	}
}

func (a *App) saveCurrentPrompt() {
	body := strings.TrimSpace(a.promptEditDraft)
	if body == "" {
		a.promptEditNote = "请先填写提示词内容"
		return
	}
	name := strings.TrimSpace(a.promptDraftName)
	if name == "" {
		name = "自定义提示词"
	}
	kind := promptKind(a.promptDraftKind)
	u := prompts.UserPrompt{
		ID:    prompts.NextUserID(a.userPrompts),
		Label: name,
		Kind:  kind,
		Body:  body,
	}
	a.userPrompts = append(a.userPrompts, u)
	a.promptEditPick = u.ID
	a.promptEditName = u.Label
	a.promptEditKind = promptKindLabel(u.Kind)
	a.promptEditNote = "已保存「" + u.Label + "」"
	a.persistPromptLibrary()
}

func (a *App) addUserCopy(from, body string) prompts.UserPrompt {
	tpl, _ := resolvePromptTemplate(from, a.userPrompts)
	baseName := strings.TrimSpace(a.promptEditName)
	if baseName == "" || baseName == tpl.Label {
		if tpl.Label != "" {
			baseName = tpl.Label + "（自定义）"
		} else {
			baseName = "自定义提示词"
		}
	}
	name := prompts.UniqueLabel(baseName, a.userPrompts)
	kind := tpl.Kind
	if editedKind := strings.TrimSpace(a.promptEditKind); editedKind != "" {
		kind = promptKind(editedKind)
	}
	u := prompts.UserPrompt{
		ID:    prompts.NextUserID(a.userPrompts),
		Label: name,
		Kind:  kind,
		Body:  body,
		From:  from,
	}
	a.userPrompts = append(a.userPrompts, u)
	return u
}

func (a *App) updateUserPrompt(i int) {
	u := &a.userPrompts[i]
	if name := strings.TrimSpace(a.promptEditName); name != "" {
		u.Label = name
	}
	u.Kind = promptKind(a.promptEditKind)
	u.Body = strings.TrimSpace(a.promptEditDraft)
	a.promptEditNote = "已保存改动"
}

func (a *App) restoreBuiltinPrompt() {
	if !isBuiltinPrompt(a.promptEditPick) {
		return
	}
	tpl, ok := prompts.Builtin(a.promptEditPick)
	if !ok {
		return
	}
	a.promptEditDraft = tpl.Body
	a.promptEditName = tpl.Label
	a.promptEditKind = promptKindLabel(tpl.Kind)
	a.promptEditNote = "已恢复默认模板"
}

func (a *App) deleteUserPrompt() {
	if a.promptEditPick == "" || isBuiltinPrompt(a.promptEditPick) {
		return
	}
	id := a.promptEditPick
	idx := slices.IndexFunc(a.userPrompts, func(u prompts.UserPrompt) bool { return u.ID == id })
	if idx < 0 {
		return
	}
	deletedLabel := promptOptionLabel(id, promptOptions(choosePromptLabel, a.userPrompts))
	a.userPrompts = append(a.userPrompts[:idx], a.userPrompts[idx+1:]...)
	if a.promptPick == id {
		a.promptPick = ""
		a.promptChoice = ""
		a.genPromptChoice = choosePromptLabel
	}
	if a.imgPromptPick == id {
		a.imgPromptPick = ""
		a.imgGenPromptChoice = choosePromptLabel
	}
	if deletedLabel != "" {
		if a.washPromptChoice == deletedLabel {
			a.washPromptChoice = choosePromptLabel
		}
		if a.fissionPromptChoice == deletedLabel {
			a.fissionPromptChoice = choosePromptLabel
		}
	}
	for plat, ws := range a.platformSpaces {
		changed := false
		if ws.promptPick == id {
			ws.promptPick = ""
			ws.promptChoice = ""
			ws.genPromptChoice = choosePromptLabel
			changed = true
		}
		if ws.imgPromptPick == id {
			ws.imgPromptPick = ""
			ws.imgGenPromptChoice = choosePromptLabel
			changed = true
		}
		if deletedLabel != "" && ws.washPromptChoice == deletedLabel {
			ws.washPromptChoice = choosePromptLabel
			changed = true
		}
		if deletedLabel != "" && ws.fissionPromptChoice == deletedLabel {
			ws.fissionPromptChoice = choosePromptLabel
			changed = true
		}
		if changed {
			a.platformSpaces[plat] = ws
		}
	}
	a.promptEditPick = ""
	a.promptEditDraft = ""
	a.promptEditName = ""
	a.promptEditNote = "已删除提示词"
	a.persistPromptLibrary()
}

func (a *App) persistPromptLibrary() {
	if a == nil || a.store == nil {
		return
	}
	a.store.Set("user_prompts", prompts.FormatUser(a.userPrompts))
	a.store.Set("prompt_pick", a.promptPick)
	a.store.Set("prompts_freeform", a.freeformPrompts)
	a.store.Set("prompt_suffix", a.promptSuffix)
	a.store.SetInt("use_suffix", boolInt(a.useSuffix))
}
