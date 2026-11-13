package ui

import (
	"strings"

	"github.com/xxww0098/tulai/internal/prompts"

	"github.com/egoist/mygo/ui"
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

const (
	newPromptLabel      = "新建提示词"
	choosePromptLabel   = "选择提示词"
	linePromptID        = "__line_prompts__"
	detachedPromptID    = "__workspace_prompt__"
	detachedPromptLabel = "当前提示词（整段发送）"
)

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

func promptLabels(options []prompts.Template) []string {
	labels := make([]string, len(options))
	for i, option := range options {
		labels[i] = option.Label
	}
	return labels
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

func promptOptionLabel(id string, options []prompts.Template) string {
	for _, option := range options {
		if option.ID == id {
			return option.Label
		}
	}
	return options[0].Label
}

func (a *App) taskPromptList() []string {
	if a.fissioning() {
		return prompts.FissionPrompts(a.prompts)
	}
	if a.washing() {
		return []string{a.washPrompt()}
	}
	return prompts.TaskPrompts(a.promptPick, a.prompts)
}

func (a *App) applyPromptChoice() {
	next := promptOptionID(a.promptEditChoice, promptOptions(newPromptLabel, a.userPrompts))
	if next == a.promptEditPick {
		return
	}
	note := a.commitPromptEdits()
	if a.promptEditPick == "" {
		a.promptEditDraft = a.promptEditText
		a.promptDraftName = a.promptEditName
		a.promptDraftKind = a.promptEditKind
	}
	a.promptEditPick = next
	a.promptEditNote = note
	if next == "" {
		a.promptEditText = a.promptEditDraft
		a.promptEditName = a.promptDraftName
		a.promptEditKind = a.promptDraftKind
		a.persistPromptLibrary()
		return
	}
	if body, ok := prompts.Body(next, a.userPrompts); ok {
		a.promptEditText = body
	}
	if item, ok := prompts.Builtin(next); ok {
		a.promptEditName, a.promptEditKind = item.Label, promptKindLabel(item.Kind)
	} else {
		for _, item := range a.userPrompts {
			if item.ID == next {
				a.promptEditName, a.promptEditKind = item.Label, promptKindLabel(item.Kind)
				break
			}
		}
	}
	a.persistPromptLibrary()
}

// commitPromptEdits 在离开一条模板前，把改过的内置另存，把改过的用户提示词写回。
func (a *App) commitPromptEdits() string {
	if a.promptEditPick == "" || !prompts.ValidBody(a.promptEditText) {
		return ""
	}
	if prompts.IsBuiltin(a.promptEditPick) {
		item, _ := prompts.Builtin(a.promptEditPick)
		if prompts.BuiltinUnchanged(a.promptEditPick, a.promptEditText) && a.promptEditName == item.Label && promptKind(a.promptEditKind) == item.Kind {
			return ""
		}
		a.addUserCopy(a.promptEditPick, a.promptEditText)
		return "改过的内置已另存为我的提示词"
	}
	for i := range a.userPrompts {
		if a.userPrompts[i].ID == a.promptEditPick {
			a.updateUserPrompt(i)
		}
	}
	return ""
}

func (a *App) saveCurrentPrompt() {
	if !prompts.ValidBody(a.promptEditText) {
		a.promptEditNote = "提示词是空的，没有保存"
		return
	}
	if prompts.IsBuiltin(a.promptEditPick) {
		builtin, _ := prompts.Builtin(a.promptEditPick)
		if prompts.BuiltinUnchanged(a.promptEditPick, a.promptEditText) && a.promptEditName == builtin.Label && promptKind(a.promptEditKind) == builtin.Kind {
			a.promptEditNote = "与内置相同，不用另存"
			return
		}
		item := a.addUserCopy(a.promptEditPick, a.promptEditText)
		a.promptEditPick = item.ID
		a.promptEditName = item.Label
		a.promptEditChoice = promptOptionLabel(item.ID, promptOptions(newPromptLabel, a.userPrompts))
		a.promptEditNote = "已另存为我的提示词，内置原文还在"
		a.persistPromptLibrary()
		return
	}
	if a.promptEditPick == "" {
		item := a.addUserCopy("", a.promptEditText)
		a.promptEditPick = item.ID
		a.promptEditName = item.Label
		a.promptEditChoice = promptOptionLabel(item.ID, promptOptions(newPromptLabel, a.userPrompts))
		a.promptEditDraft = ""
		a.promptDraftName = ""
		a.promptDraftKind = ""
		a.promptEditNote = "已保存为我的提示词"
		a.persistPromptLibrary()
		return
	}
	for i := range a.userPrompts {
		if a.userPrompts[i].ID == a.promptEditPick {
			a.updateUserPrompt(i)
			a.promptEditName = a.userPrompts[i].Label
			a.promptEditChoice = promptOptionLabel(a.promptEditPick, promptOptions(newPromptLabel, a.userPrompts))
		}
	}
	a.promptEditNote = "已更新我的提示词"
	a.persistPromptLibrary()
}

func (a *App) restoreBuiltinPrompt() {
	if !prompts.IsBuiltin(a.promptEditPick) {
		return
	}
	item, ok := prompts.Builtin(a.promptEditPick)
	if !ok {
		return
	}
	a.promptEditText = item.Body
	a.promptEditName = item.Label
	a.promptEditKind = promptKindLabel(item.Kind)
	a.promptEditNote = "已恢复内置提示词"
	a.persistPromptLibrary()
}

func (a *App) deleteUserPrompt() {
	if a.promptEditPick == "" || prompts.IsBuiltin(a.promptEditPick) {
		return
	}
	kept := make([]prompts.UserPrompt, 0, len(a.userPrompts))
	for _, item := range a.userPrompts {
		if item.ID != a.promptEditPick {
			kept = append(kept, item)
		}
	}
	a.userPrompts = kept
	if a.promptPick == a.promptEditPick {
		a.promptPick = detachedPromptID
		a.promptChoice = a.workspacePromptLabel()
		a.promptNote = "所选模板已删除，当前提示词仍按整段发送"
	}
	a.promptEditPick = ""
	a.promptEditChoice = newPromptLabel
	a.promptEditText = a.promptEditDraft
	a.promptEditName = a.promptDraftName
	a.promptEditKind = a.promptDraftKind
	a.promptEditNote = "已删除我的提示词"
	a.persistPromptLibrary()
}

func (a *App) workspacePromptLabel() string {
	options := promptOptions(prompts.LabelCustom, a.userPrompts)
	if a.promptPick == detachedPromptID {
		options = promptOptions(prompts.LabelCustom, a.userPrompts, prompts.Template{ID: detachedPromptID, Label: detachedPromptLabel})
	}
	return promptOptionLabel(a.promptPick, options)
}

func (a *App) workspacePromptSelect(c *ui.Context) {
	options := workspacePromptOptions(prompts.KindGen, a.userPrompts, prompts.Template{ID: linePromptID, Label: prompts.LabelCustom})
	labels := promptLabels(options)
	if !containsStr(labels, a.genPromptChoice) {
		a.genPromptChoice = choosePromptLabel
	}
	motionSelect(c.Key("gen-prompt-template"), &a.genPromptChoice, labels).Grow(1).MinWidth(0).
		Label("选择生图提示词").Disabled(a.running).OnChange(func() {
		next := promptOptionID(a.genPromptChoice, options)
		if next == linePromptID && a.promptPick != "" {
			a.promptPick = ""
			a.prompts = a.freeformPrompts
		} else if body, ok := prompts.Body(next, a.userPrompts); ok {
			if a.promptPick == "" {
				a.freeformPrompts = a.prompts
			}
			a.promptPick = next
			a.prompts = body
		}
		a.promptChoice = a.workspacePromptLabel()
		a.genPromptChoice = choosePromptLabel
		a.promptNote = ""
		a.persistPromptLibrary()
	})
}

func (a *App) washPromptSelect(c *ui.Context) {
	options := workspacePromptOptions(prompts.KindWash, a.userPrompts)
	labels := promptLabels(options)
	id := promptOptionID(a.washPromptChoice, options)
	if body, ok := prompts.Body(id, a.userPrompts); !ok || body != a.prompts {
		a.washPromptChoice = choosePromptLabel
		for _, option := range options {
			if option.ID != "" && option.Body == a.prompts {
				a.washPromptChoice = option.Label
				break
			}
		}
	}
	motionSelect(c.Key("wash-prompt-template"), &a.washPromptChoice, labels).Grow(1).MinWidth(0).
		Label("选择洗图提示词").Disabled(a.running).OnChange(func() {
		id := promptOptionID(a.washPromptChoice, options)
		if body, ok := prompts.Body(id, a.userPrompts); ok {
			a.prompts = body
		}
	})
}

func (a *App) addUserCopy(from, body string) prompts.UserPrompt {
	base := strings.TrimSpace(a.promptEditName)
	if base == "" {
		base = prompts.LabelFromBody(body)
	}
	item := prompts.UserPrompt{
		ID:    prompts.NextUserID(a.userPrompts),
		Label: prompts.UniqueLabel(base, a.userPrompts),
		Body:  body,
		Kind:  promptKind(a.promptEditKind),
		From:  from,
	}
	a.userPrompts = append(a.userPrompts, item)
	return item
}

func (a *App) updateUserPrompt(i int) {
	item := &a.userPrompts[i]
	name := strings.TrimSpace(a.promptEditName)
	if name == "" {
		name = prompts.LabelFromBody(a.promptEditText)
	}
	if name != item.Label {
		others := make([]prompts.UserPrompt, 0, len(a.userPrompts)-1)
		others = append(others, a.userPrompts[:i]...)
		others = append(others, a.userPrompts[i+1:]...)
		item.Label = prompts.UniqueLabel(name, others)
	}
	item.Body = a.promptEditText
	item.Kind = promptKind(a.promptEditKind)
	if a.promptPick == item.ID {
		a.promptChoice = a.workspacePromptLabel()
	}
}

func (a *App) persistPromptLibrary() {
	if a.store == nil {
		return
	}
	a.store.Set("prompts", a.stateOf(pageHome).prompts)
	a.store.Set("prompts_freeform", a.freeformPrompts)
	a.store.Set("user_prompts", prompts.FormatUser(a.userPrompts))
	a.store.Set("prompt_pick", a.promptPick)
}

func (a *App) bridgeStatusLine() (string, string, bool) {
	if a.engine == nil {
		return "千问未连接 · 豆包未连接", "令牌和端口要与扩展弹出页一致。掉线后会自动重连。", false
	}
	st := a.engine.BridgeStatus()
	if st.Err != "" {
		return st.Err, "", true
	}
	qianwen, doubao := "未连接", "未连接"
	if st.Qianwen {
		qianwen = "已连接"
	}
	if st.Doubao {
		doubao = "已连接"
	}
	line := "千问" + qianwen + " · 豆包" + doubao
	if st.Listening && st.Addr != "" {
		line = st.Addr + " · " + line
	}
	if st.Detail != "" {
		return line, st.Detail, true
	}
	return line, "掉线后扩展会自动重连，不用重启 tulai。", false
}
