// Package prompts 是桌面端的提示词库：内置模板和用户另存的副本。
package prompts

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	// LabelCustom 是不套模板、按行拆分的那一项。
	LabelCustom       = "每行一条"
	IDWash            = "wash"
	IDDoubaoWash      = "doubao_wash"
	KindGen           = "gen"
	KindWash          = "wash"
	DefaultWashPrompt = `请分别参考每张输入图重新绘制，并为每张原图生成一张独立图片。
保留原图的主体、主题、构图、整体风格、主要色彩和清晰可辨的文字与标志，不改变关键特征；未指定输出比例时参考原图比例。
在保留主要内容的前提下，适度调整线条、纹理、光影和局部细节，使画面自然清晰，与原图有适当区别。
不要新增无关元素、文字或水印，不要猜测无法辨认的文字；避免主体变形、重复、模糊或拼图。直接输出图片，不要输出文字说明。`
	DoubaoWashPrompt = `每张图单独按以下要求生成一张图。反推图生文要求：以数字油画作为画面主体，全文将该对象称作主体，中文详尽描述主体表面全部图案细节：1、可以识别的IP形象、知名人物、地名、景点、建筑直接写出对应专有名称；2、完整描述画面主题、配色并标注具体十六进制色值，细致拆解构图布局，重点突出油画艺术风格，包含画布肌理、颜料笔触、厚涂堆叠质感，可指定印象派、写实油画、复古做旧油画等画风；3、识别参考图内所有非图案类封面标题、装饰标语、书籍文字，全部剔除，主体画面不保留任何额外文字，不生成标题、书名、宣传文案；4、写明背景属性，明确是纯色背景，标注背景具体颜色；5、输出文本字数不少于100字。
若参考图画面裁切残缺、内容不完整，则依据参考图主体图案元素补全缺失部分，重构生成完整主体图案。
基于反推得到的完整描述进行文生图生成，画面比例4:5，重绘强度0.7，完整平铺数字油画，油画画面边缘直接紧贴图片画布四边，完全填满整个画幅，零留白、零空隙、无外围背景区域，不出现画框，不出现边框，完整展示油画全部画面内容，无裁切缺失；消除画面多余杂散阴影，仅保留画布本身柔和环境光影；浓郁油画质感，可见真实画布纹理，油画颜料笔触清晰，色彩层次厚重，油画画布颗粒肌理，色彩过渡带有油画混色效果。
反向提示词：不要数码平涂感，商业数字油画平铺实拍，光影柔和均匀，细节丰富，8K高清，光滑塑料质感，卡通渲染，3D渲染，畸形，扭曲，水印，书名，封面标题，宣传标语，多余文字，书籍排版，模糊，低画质，噪点，AI畸变，照片写实无笔触，插画线条感，线条轮廓，黑白素描，四周白边，画面留白，边缘空隙，多余阴影，浓重投影，边框，画框，外框，画布外围背景，四周空余区域，油画缩小居中，四周留有空间，画面裁切，局部残缺`
)

// Template 是一条可选提示词。Builtin 为真时正文不可改写，编辑后应另存。
type Template struct {
	ID      string
	Label   string
	Body    string
	Kind    string
	Builtin bool
	From    string
}

// UserPrompt 是用户保存的提示词。From 指向被改过的内置 id，可空。
type UserPrompt struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Body  string `json:"body"`
	Kind  string `json:"kind,omitempty"`
	From  string `json:"from,omitempty"`
}

// Builtins 按界面顺序返回内置模板。
func Builtins() []Template {
	return []Template{
		{ID: IDWash, Label: "默认洗图", Body: DefaultWashPrompt, Kind: KindWash, Builtin: true},
		{ID: IDDoubaoWash, Label: "豆包洗图", Body: DoubaoWashPrompt, Kind: KindWash, Builtin: true},
	}
}

// Builtin 按 id 取内置模板。
func Builtin(id string) (Template, bool) {
	for _, item := range Builtins() {
		if item.ID == id {
			return item, true
		}
	}
	return Template{}, false
}

// IsBuiltin 报告 id 是不是内置模板。
func IsBuiltin(id string) bool {
	_, ok := Builtin(id)
	return ok
}

// ParseUser 解析配置库里的用户提示词。坏数据当成没有。
func ParseUser(raw string) []UserPrompt {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var items []UserPrompt
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	var out []UserPrompt
	for _, item := range items {
		item.ID = strings.TrimSpace(item.ID)
		item.Label = strings.TrimSpace(item.Label)
		if item.ID == "" || item.Label == "" || strings.TrimSpace(item.Body) == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

// FormatUser 把用户提示词写成配置库字符串。
func FormatUser(items []UserPrompt) string {
	if len(items) == 0 {
		return ""
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(raw)
}

// NormalizePick 丢掉已经不存在的选中项。空字符串表示「每行一条」。
func NormalizePick(id string, users []UserPrompt) string {
	id = strings.TrimSpace(id)
	if id == "" || IsBuiltin(id) {
		return id
	}
	for _, item := range users {
		if item.ID == id {
			return id
		}
	}
	return ""
}

// Catalog 是下拉里的全部条目，顺序与界面一致。
func Catalog(users []UserPrompt) []Template {
	out := []Template{{ID: "", Label: LabelCustom}}
	out = append(out, Builtins()...)
	used := map[string]bool{}
	for _, item := range out {
		used[item.Label] = true
	}
	for _, item := range users {
		label := item.Label
		if used[label] {
			label = item.Label + "（" + item.ID + "）"
		}
		used[label] = true
		out = append(out, Template{ID: item.ID, Label: label, Body: item.Body, Kind: item.Kind, From: item.From})
	}
	return out
}

// Labels 是下拉框的显示名。
func Labels(users []UserPrompt) []string {
	cat := Catalog(users)
	out := make([]string, len(cat))
	for i, item := range cat {
		out[i] = item.Label
	}
	return out
}

// LabelFor 是选中 id 对应的下拉显示名。
func LabelFor(id string, users []UserPrompt) string {
	id = NormalizePick(id, users)
	for _, item := range Catalog(users) {
		if item.ID == id {
			return item.Label
		}
	}
	return LabelCustom
}

// IDForLabel 把下拉显示名映回 id。未知名字按「每行一条」。
func IDForLabel(label string, users []UserPrompt) string {
	for _, item := range Catalog(users) {
		if item.Label == label {
			return item.ID
		}
	}
	return ""
}

// Body 返回选中模板的原文。自定义模式没有模板正文。
func Body(id string, users []UserPrompt) (string, bool) {
	if id == "" {
		return "", false
	}
	if item, ok := Builtin(id); ok {
		return item.Body, true
	}
	for _, item := range users {
		if item.ID == id {
			return item.Body, true
		}
	}
	return "", false
}

// SameText 比较提示词是否同一份，忽略首尾空白。
func SameText(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// NextUserID 分配一个还没用过的用户 id。
func NextUserID(users []UserPrompt) string {
	used := map[string]bool{}
	for _, item := range users {
		used[item.ID] = true
	}
	for n := 1; ; n++ {
		id := "u" + itoa(n)
		if !used[id] {
			return id
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// UniqueLabel 在已有名字后面加「副本」直到不重复。
func UniqueLabel(base string, users []UserPrompt) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "我的提示词"
	}
	used := map[string]bool{LabelCustom: true}
	for _, item := range Builtins() {
		used[item.Label] = true
	}
	for _, item := range users {
		used[item.Label] = true
	}
	if !used[base] {
		return base
	}
	for n := 1; ; n++ {
		label := base
		if n == 1 {
			label = base + " 副本"
		} else {
			label = base + " 副本" + itoa(n)
		}
		if !used[label] {
			return label
		}
	}
}

// LabelFromBody 用正文第一行的前 12 个字做名字。
func LabelFromBody(body string) string {
	line := strings.TrimSpace(body)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	rs := []rune(line)
	if len(rs) > 12 {
		line = string(rs[:12])
	}
	if strings.TrimSpace(line) == "" {
		return "我的提示词"
	}
	return line
}

// ImportText 把 txt 收成「每行一条」。
// 空行隔开、且其中有一段超过一行时，按空行分段，段内换行并成一条。
// 否则丢掉空行，每行一条。这样两种常见写法都进得了 TaskPrompts。
func ImportText(raw string) string {
	raw = strings.TrimPrefix(raw, "\uFEFF")
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	var blocks [][]string
	var cur []string
	blank := false
	sawBlank := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = nil
				blank = true
			}
			continue
		}
		if blank && len(blocks) > 0 {
			sawBlank = true
		}
		blank = false
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	var out []string
	if sawBlank {
		for _, block := range blocks {
			out = append(out, strings.Join(block, " "))
		}
	} else {
		for _, block := range blocks {
			out = append(out, block...)
		}
	}
	return strings.Join(out, "\n")
}

// TaskPrompts 是这次要送出去的提示词。
// 选了模板时，整段正文是一条（换行留在正文里，不按行拆开）。
// 「每行一条」才按非空行拆开。
func TaskPrompts(pick, text string) []string {
	if pick != "" {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{strings.TrimSpace(text)}
	}
	return splitLines(text)
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Describe 是工作区状态行里提示词数量的说法。
func Describe(pick, text string, users []UserPrompt) string {
	n := len(TaskPrompts(pick, text))
	if pick == "" {
		return itoa(n) + " 条提示词"
	}
	return LabelFor(pick, users)
}

// BuiltinUnchanged 报告内置正文没有被用户文本改掉。测试和保存都用它。
func BuiltinUnchanged(id, text string) bool {
	item, ok := Builtin(id)
	if !ok {
		return false
	}
	return SameText(item.Body, text)
}

// ValidBody 报告去掉空白后还有字。
func ValidBody(s string) bool {
	return strings.TrimSpace(s) != "" && utf8.ValidString(s)
}
