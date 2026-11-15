package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
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
	gen, _ := json.Marshal(a.structuredGen)
	wash, _ := json.Marshal(a.structuredWash)
	a.store.Set("structured_prompt_gen", string(gen))
	a.store.Set("structured_prompt_wash", string(wash))
}

func (a *App) promptPartsFor(page int) *promptParts {
	if !a.structuredReady {
		a.loadPromptParts()
	}
	if page == pageWash {
		return &a.structuredWash
	}
	return &a.structuredGen
}

func (a *App) currentPromptParts() *promptParts {
	return a.promptPartsFor(a.statePage)
}

func (a *App) hasTaskPrompt() bool {
	return len(a.taskPromptList()) > 0 || a.currentPromptParts().text() != ""
}

func (a *App) promptPartsPreview(parts *promptParts) string {
	var text string
	if list := a.taskPromptList(); len(list) > 0 {
		text = list[0]
	}
	if !a.washing() && a.useSuffix && a.promptSuffix != "" {
		text = strings.TrimSpace(text + "\n" + a.promptSuffix)
	}
	if extra := a.promptExtras(a.structuredPage, parts); extra != "" {
		if text == "" {
			text = extra
		} else {
			text = strings.TrimSpace(text + "\n附加要求：\n" + extra)
		}
	}
	if text == "" {
		return "尚未输入提示词或勾选参数"
	}
	return text
}

func (a *App) promptPartsButton(c *ui.Context) {
	uiIconBtn(c, "prompt-parts-btn", "组合参数", "组合提示词参数", iconSliders, 22, a.structuredOpen, false, a.look).
		Disabled(a.running).OnClick(func() {
		a.structuredPage = a.statePage
		a.structuredOpen = true
	})
}

func (a *App) promptPartsModal(c *ui.Context) {
	if !a.structuredOpen {
		return
	}
	tok := a.look
	dangerInk, _, _ := semanticToneColors(tok.dark, tok.text, tok.danger, 0.12)
	parts := a.promptPartsFor(a.structuredPage)
	w, h := c.Size()
	ui.Modal(c, &a.structuredOpen, func() {
		ui.Column(c.Key("prompt-parts")).Width(max(260, min(540, w-40))).Gap(8).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				ui.Text(c, "组合提示词参数").FontSize(tok.title).Bold().TextColor(tok.text)
				ui.Text(c, "勾选后自动附加到每条提示词；内容可以修改。").FontSize(tok.caption).TextColor(tok.muted).Wrap()
			})
			ui.Divider(c).Background(tok.border)
			ui.Scroll(c.Key("prompt-parts-scroll")).FillWidth().MaxHeight(max(160, h-260)).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					for _, row := range parts.rows() {
						row := row
						ui.Column(c.Key("part-" + row.name)).FillWidth().Gap(3).Children(func() {
							ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Checkbox(c, &row.part.Enabled, row.name).Label("勾选" + row.name).Width(74).Disabled(a.running)
								ui.TextInput(c, &row.part.Value).Grow(1).MinWidth(0).Label(row.name + "参数内容").Placeholder("输入要求").Disabled(a.running || !row.part.Enabled)
							})
							styleOverridden := row.name == "风格" && a.structuredPage == pageWash && washStyleInstruction(a.washStyle) != ""
							if styleOverridden {
								ui.Text(c, "当前洗图风格预设优先，此项暂不发送").FontSize(tok.caption).TextColor(tok.muted).Margin(0, 0, 0, 82)
							} else if row.part.Enabled && strings.TrimSpace(row.part.Value) == "" {
								ui.Text(c, row.name+"参数不能为空").FontSize(tok.caption).TextColor(dangerInk).Margin(0, 0, 0, 82)
							}
						})
					}
					ui.Text(c, "组合预览（第 1 条）").FontSize(tok.body).FontWeight(600).TextColor(tok.text)
					ui.Scroll(c.Key("prompt-parts-preview")).FillWidth().MaxHeight(100).Padding(6, 8).
						Radius(tok.radiusS).Background(tok.sunken).Border(1, tok.border).Children(func() {
						ui.Text(c, a.promptPartsPreview(parts)).FontSize(tok.body).TextColor(tok.muted).Wrap()
					})
					ui.Text(c, "如果参数与原提示词冲突，请修改后再发送。平台的比例和批量要求会在发送时另行补充。").FontSize(tok.caption).TextColor(tok.muted).Wrap()
					effective := a.effectivePromptParts(a.structuredPage, parts)
					if err := effective.validate(); err != nil {
						ui.Text(c, err.Error()).FontSize(tok.caption).TextColor(dangerInk).Wrap()
					}
				})
			})
			ui.Divider(c).Background(tok.border)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				uiBtn(ui.Button(c, "清空勾选"), tok).Disabled(a.running).OnClick(func() {
					for _, row := range parts.rows() {
						row.part.Enabled = false
					}
				})
				effective := a.effectivePromptParts(a.structuredPage, parts)
				uiBtn(ui.PrimaryButton(c, "完成"), tok).Disabled(effective.validate() != nil).OnClick(func() {
					a.savePromptParts()
					a.structuredOpen = false
				})
			})
		})
	})
}
