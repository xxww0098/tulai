package ui

import (
	"github.com/xxww0098/tulai/internal/prompts"

	"github.com/egoist/mygo/ui"
)

func (a *App) taskPromptList() []string {
	return prompts.TaskPrompts(a.promptPick, a.prompts)
}

func (a *App) promptRow(c *ui.Context) {
	a.promptControls(c, false)
}

func (a *App) promptControls(c *ui.Context, stacked bool) {
	labels := prompts.Labels(a.userPrompts)
	if !containsStr(labels, a.promptChoice) {
		a.promptChoice = prompts.LabelFor(a.promptPick, a.userPrompts)
	}
	if stacked {
		motionSelect(c.Key("prompt-template"), &a.promptChoice, labels).FillWidth().OnChange(func() {
			a.applyPromptChoice()
		})
		ui.Row(c).Gap(8).Wrap().Children(func() { a.promptButtons(c) })
		return
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		motionSelect(c.Key("prompt-template"), &a.promptChoice, labels).Grow(1).MinWidth(0).OnChange(func() {
			a.applyPromptChoice()
		})
		a.promptButtons(c)
	})
}

func (a *App) promptButtons(c *ui.Context) {
	ui.Button(c, "保存提示词").Disabled(a.running).OnClick(func() { a.saveCurrentPrompt() })
	if prompts.IsBuiltin(a.promptPick) {
		ui.Button(c, "恢复内置").Disabled(a.running).OnClick(func() { a.restoreBuiltinPrompt() })
	}
	if a.promptPick != "" && !prompts.IsBuiltin(a.promptPick) {
		ui.Button(c, "删除提示词").Disabled(a.running).OnClick(func() { a.deleteUserPrompt() })
	}
}

func (a *App) applyPromptChoice() {
	next := prompts.IDForLabel(a.promptChoice, a.userPrompts)
	if next == a.promptPick {
		return
	}
	note := a.commitPromptEdits()
	if a.promptPick == "" {
		a.freeformPrompts = a.prompts
	}
	a.promptPick = next
	a.promptNote = note
	if next == "" {
		a.prompts = a.freeformPrompts
		a.persistPromptLibrary()
		return
	}
	if body, ok := prompts.Body(next, a.userPrompts); ok {
		a.prompts = body
	}
	a.persistPromptLibrary()
}

// commitPromptEdits 在离开一条模板前，把改过的内置另存，把改过的用户提示词写回。
func (a *App) commitPromptEdits() string {
	if a.promptPick == "" || !prompts.ValidBody(a.prompts) {
		return ""
	}
	if prompts.IsBuiltin(a.promptPick) {
		if prompts.BuiltinUnchanged(a.promptPick, a.prompts) {
			return ""
		}
		a.addUserCopy(a.promptPick, a.prompts)
		return "改过的内置已另存为我的提示词"
	}
	for i := range a.userPrompts {
		if a.userPrompts[i].ID == a.promptPick {
			a.userPrompts[i].Body = a.prompts
		}
	}
	return ""
}

func (a *App) saveCurrentPrompt() {
	if !prompts.ValidBody(a.prompts) {
		a.promptNote = "提示词是空的，没有保存"
		return
	}
	if prompts.IsBuiltin(a.promptPick) {
		if prompts.BuiltinUnchanged(a.promptPick, a.prompts) {
			a.promptNote = "与内置相同，不用另存"
			return
		}
		item := a.addUserCopy(a.promptPick, a.prompts)
		a.promptPick = item.ID
		a.promptChoice = prompts.LabelFor(item.ID, a.userPrompts)
		a.promptNote = "已另存为我的提示词，内置原文还在"
		a.persistPromptLibrary()
		return
	}
	if a.promptPick == "" {
		item := a.addUserCopy("", a.prompts)
		a.promptPick = item.ID
		a.promptChoice = prompts.LabelFor(item.ID, a.userPrompts)
		a.freeformPrompts = a.prompts
		a.promptNote = "已保存为我的提示词"
		a.persistPromptLibrary()
		return
	}
	for i := range a.userPrompts {
		if a.userPrompts[i].ID == a.promptPick {
			a.userPrompts[i].Body = a.prompts
		}
	}
	a.promptNote = "已更新我的提示词"
	a.persistPromptLibrary()
}

func (a *App) restoreBuiltinPrompt() {
	if !prompts.IsBuiltin(a.promptPick) {
		return
	}
	body, ok := prompts.Body(a.promptPick, nil)
	if !ok {
		return
	}
	a.prompts = body
	a.promptNote = "已恢复内置提示词"
	a.persistPromptLibrary()
}

func (a *App) deleteUserPrompt() {
	if a.promptPick == "" || prompts.IsBuiltin(a.promptPick) {
		return
	}
	kept := make([]prompts.UserPrompt, 0, len(a.userPrompts))
	for _, item := range a.userPrompts {
		if item.ID != a.promptPick {
			kept = append(kept, item)
		}
	}
	a.userPrompts = kept
	a.promptPick = ""
	a.promptChoice = prompts.LabelCustom
	a.prompts = a.freeformPrompts
	a.promptNote = "已删除我的提示词"
	a.persistPromptLibrary()
}

func (a *App) addUserCopy(from, body string) prompts.UserPrompt {
	base := prompts.LabelFromBody(body)
	if item, ok := prompts.Builtin(from); ok {
		base = item.Label
	}
	item := prompts.UserPrompt{
		ID:    prompts.NextUserID(a.userPrompts),
		Label: prompts.UniqueLabel(base, a.userPrompts),
		Body:  body,
		From:  from,
	}
	a.userPrompts = append(a.userPrompts, item)
	return item
}

func (a *App) persistPromptLibrary() {
	if a.store == nil {
		return
	}
	a.store.Set("prompts", a.prompts)
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
