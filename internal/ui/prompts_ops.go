package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/tasks"
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
		return []string{a.fissionPrompt()}
	}
	if a.statePage == pageWash {
		return []string{a.washPrompt()}
	}
	if strings.TrimSpace(a.promptPick) != "" {
		return []string{strings.TrimSpace(a.prompts)}
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

func (a *App) applyPromptChoice() {
	page := a.statePage
	if a.running && a.runPage == page {
		return
	}
	choice := a.genPromptChoice
	if page == pageWash {
		choice = a.washPromptChoice
	}
	options := workspacePromptOptions(prompts.KindGen, a.userPrompts)
	if page == pageWash {
		options = workspacePromptOptions(prompts.KindWash, a.userPrompts)
	}
	id := promptOptionID(choice, options)
	if id == "" {
		if page == pageWash {
			a.prompts = a.defaultWashPrompt()
			a.washPromptChoice = choosePromptLabel
		} else {
			a.promptPick = ""
			a.promptChoice = ""
			a.genPromptChoice = choosePromptLabel
			a.prompts = a.freeformPrompts
		}
		return
	}
	tpl, ok := resolvePromptTemplate(id, a.userPrompts)
	if !ok {
		return
	}
	if page == pageWash {
		a.prompts = tpl.Body
		return
	}
	if a.promptPick == "" {
		a.freeformPrompts = a.prompts
	}
	a.promptPick = tpl.ID
	a.promptChoice = tpl.ID
	a.prompts = tpl.Body
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
	if a.promptPick == u.ID {
		a.promptChoice = u.ID
		a.genPromptChoice = promptOptionLabel(u.ID, workspacePromptOptions(prompts.KindGen, a.userPrompts))
		if a.statePage == pageHome {
			a.prompts = u.Body
		} else if gen, ok := a.parked[pageHome]; ok {
			gen.prompts = u.Body
			a.parked[pageHome] = gen
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
	name := tpl.Label + "（自定义）"
	if tpl.Label == "" {
		name = "自定义提示词"
	}
	u := prompts.UserPrompt{
		ID:    prompts.NextUserID(a.userPrompts),
		Label: name,
		Kind:  tpl.Kind,
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
	a.userPrompts = append(a.userPrompts[:idx], a.userPrompts[idx+1:]...)
	if a.promptPick == id {
		a.promptPick = ""
		a.promptChoice = ""
		a.genPromptChoice = choosePromptLabel
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

func (a *App) bridgeStatusLine(st tasks.BridgeStatus) (string, string, bool) {
	if st.Listening {
		return fmt.Sprintf("本地扩展桥运行中 · %s", st.Addr), "success", true
	}
	return "本地扩展桥未启动", "neutral", false
}
