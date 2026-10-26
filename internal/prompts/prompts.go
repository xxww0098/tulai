// Package prompts 是桌面端的提示词库：三条内置模板，加上用户另存的副本。
// 内置正文只去掉开头的 BOM 和空白，其余原样保留，方便恢复。
package prompts

import (
	"embed"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

//go:embed builtin/*.txt
var builtinFS embed.FS

const (
	// LabelCustom 是不套模板、按行拆分的那一项。
	LabelCustom = "每行一条"
	IDOil       = "oil"
	IDArt       = "art"
	IDCircle    = "circle"
)

// Template 是一条可选提示词。Builtin 为真时正文不可改写，编辑后应另存。
type Template struct {
	ID      string
	Label   string
	Body    string
	Builtin bool
	From    string
}

// UserPrompt 是用户保存的提示词。From 指向被改过的内置 id，可空。
type UserPrompt struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Body  string `json:"body"`
	From  string `json:"from,omitempty"`
}

type builtinSpec struct {
	id, label, file string
}

var builtinOrder = []builtinSpec{
	{IDOil, "数字油画", "builtin/digital-oil.txt"},
	{IDArt, "数字画", "builtin/digital-art.txt"},
	{IDCircle, "圆形印花", "builtin/circle-print.txt"},
}

// Builtins 按界面顺序返回内置模板。
func Builtins() []Template {
	out := make([]Template, 0, len(builtinOrder))
	for _, spec := range builtinOrder {
		out = append(out, Template{
			ID: spec.id, Label: spec.label, Body: mustBuiltin(spec.file), Builtin: true,
		})
	}
	return out
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

func mustBuiltin(name string) string {
	raw, err := builtinFS.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return normalizeBuiltin(string(raw))
}

// normalizeBuiltin 只去掉开头的 BOM 和空白。
func normalizeBuiltin(s string) string {
	s = strings.TrimPrefix(s, "\uFEFF")
	return strings.TrimLeftFunc(s, unicode.IsSpace)
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
		out = append(out, Template{ID: item.ID, Label: label, Body: item.Body, From: item.From})
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
