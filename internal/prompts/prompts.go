// Package prompts 是桌面端的提示词库：内置模板和用户另存的副本。
package prompts

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	// LabelCustom 是不套模板、按行拆分的那一项。
	LabelCustom  = "每行一条"
	IDDoubaoWash = "doubao_wash"
	// Retired IDs are kept so existing workspace drafts can be restored safely.
	IDWash            = "wash"
	IDDoubaoPixelWash = "doubao_pixel_wash"
	KindGen           = "gen"
	KindWash          = "wash"
	DefaultWashPrompt = `请分别参考每张输入图重新绘制，并为每张原图生成一张独立图片。
保留原图的主体、主题、构图、整体风格、主要色彩和清晰可辨的文字与标志，不改变关键特征；未指定输出比例时参考原图比例。
在保留主要内容的前提下，适度调整线条、纹理、光影和局部细节，使画面自然清晰，与原图有适当区别。
不要新增无关元素、文字或水印，不要猜测无法辨认的文字；避免主体变形、重复、模糊或拼图。直接输出图片，不要输出文字说明。`
	// LegacyDoubaoWashPrompt is kept only to upgrade an unchanged active draft.
	LegacyDoubaoWashPrompt = `每张图单独按以下要求生成一张图。反推图生文要求：以数字油画作为画面主体，全文将该对象称作主体，中文详尽描述主体表面全部图案细节：1、可以识别的IP形象、知名人物、地名、景点、建筑直接写出对应专有名称；2、完整描述画面主题、配色并标注具体十六进制色值，细致拆解构图布局，重点突出油画艺术风格，包含画布肌理、颜料笔触、厚涂堆叠质感，可指定印象派、写实油画、复古做旧油画等画风；3、识别参考图内所有非图案类封面标题、装饰标语、书籍文字，全部剔除，主体画面不保留任何额外文字，不生成标题、书名、宣传文案；4、写明背景属性，明确是纯色背景，标注背景具体颜色；5、输出文本字数不少于100字。
若参考图画面裁切残缺、内容不完整，则依据参考图主体图案元素补全缺失部分，重构生成完整主体图案。
基于反推得到的完整描述进行文生图生成，画面比例4:5，重绘强度0.7，完整平铺数字油画，油画画面边缘直接紧贴图片画布四边，完全填满整个画幅，零留白、零空隙、无外围背景区域，不出现画框，不出现边框，完整展示油画全部画面内容，无裁切缺失；消除画面多余杂散阴影，仅保留画布本身柔和环境光影；浓郁油画质感，可见真实画布纹理，油画颜料笔触清晰，色彩层次厚重，油画画布颗粒肌理，色彩过渡带有油画混色效果。
反向提示词：不要数码平涂感，商业数字油画平铺实拍，光影柔和均匀，细节丰富，8K高清，光滑塑料质感，卡通渲染，3D渲染，畸形，扭曲，水印，书名，封面标题，宣传标语，多余文字，书籍排版，模糊，低画质，噪点，AI畸变，照片写实无笔触，插画线条感，线条轮廓，黑白素描，四周白边，画面留白，边缘空隙，多余阴影，浓重投影，边框，画框，外框，画布外围背景，四周空余区域，油画缩小居中，四周留有空间，画面裁切，局部残缺`
	// PreviousDoubaoWashPrompt upgrades only an unchanged active draft, not historical tasks.
	PreviousDoubaoWashPrompt = `请分别从每张参考图中还原商品上的主图图案。每张参考图只生成并返回一张对应成品，不提供多个版本或额外配图；严格按附件顺序输出，不混用其他图片的内容。

提取主图：找出商品、书封、画布、衣物或包装上最主要、最清晰的印刷图案或画作，只还原图案本身。去掉实物载体和展示场景，包括书本、画框、墙面、桌面、手、画笔、颜料、尺寸线、产品标签、水印、标题及宣传文字。删除叠加在图案上的圆形数字色卡、放大镜示意、贴纸和宣传角标，并根据周围图案补全被遮住的部分；图案内部原有的景物和背景须保留。

选择单幅：如果参考图用九宫格、拼版或封面缩略图展示多个独立款式，只选择最大、最清晰或最醒目的一幅；同等明显时选左上第一幅。只把选中的那一格还原为完整图案，成品不得出现其他款式、相邻画面、分隔线或色卡。若多个元素共同构成同一幅图案，则保留它们原有的组合与布局。

还原平面：将图案摆正为正面、平整的二维数字画面，校正拍摄透视、倾斜和载体弯曲，去除展示造成的折痕、遮挡、反光和投影。保留画作本身的线条、笔触、纹理、像素或点阵等艺术特征，不要生成实物照片、立体模型或画布外的背景。

忠实重建：尽量保持原图主体、数量、朝向、构图、颜色与可辨细节，以及原有的油画、水彩、插画、像素画等风格，不替换主体或套用统一风格。边缘被裁切或遮挡时，依据可见内容自然补全，避免凭空添加新主题；只保留确属图案且清晰可辨的文字或标志，不猜写模糊文字。

输出完整、清晰、可直接使用的平面图案，保留选中图案内部原有的背景，不用包装、桌面或墙面的颜色替换画作背景。画面四边直接延伸到图案或其自身背景，不保留或生成外围留白、画框、边框、包边、色条、描边或水印，不添加任何新文字。未指定比例时尽量保持原图案比例；如指定了比例，优先完整呈现主体，通过自然延展图案背景适配画幅，不拉伸或裁掉主体。直接输出图片，不要输出分析或文字说明。`
	// ReviewedDoubaoWashPrompt upgrades the previous unchanged built-in draft.
	ReviewedDoubaoWashPrompt = PreviousDoubaoWashPrompt + `

执行顺序：先从每张参考图中确定一幅主图，再只对选中的那一幅进行还原或风格转换。书封、包装或展示图中由格线、标题、留白分开的多幅画，即使都属于同一主题，也按独立款式处理；同等明显时只选左上第一格。不要把其他格的主体、拼版、分隔线或空格带入成品。保持的是所选单幅内部主体的数量、位置和关键特征，而不是整张封面的排列。所选单幅内部可见的背景也是画面内容：彩色风格保留其主要颜色和明暗，不要把原有黑色或深色背景自动变成白色；用户明确要求白底线稿时按该要求处理。

无署名检查：输出的四角、边缘和画面内部都不得出现新增的作者签名、花押、手写涂鸦、版权符号、印章或水印。若参考图上叠有签名、作者印章或水印，请移除，并用周围原有的颜色、纹理和图案自然补全，避免残留类似字母的笔迹。确属主图内容的清晰文字与标志可按原要求保留，但不保留署名或水印。`
	// PixelReviewDoubaoWashPrompt preserves the previous unchanged built-in draft.
	PixelReviewDoubaoWashPrompt = ReviewedDoubaoWashPrompt + `

填色图处理：若参考图是数字填色、圆点马赛克或未完成的涂色图，先去掉每个格子里的数字、字母和填色辅助线，把未填色区域根据可见画面的主体、配色和相邻内容自然补全；不要保留白色数字格、圆形色点、色号表或未完成的底稿。看不清的部分只能合理推测，不要添加无关主体。

拼版与边缘最终检查：多个独立缩略图只选一幅，优先左上第一幅；只放大重绘这一幅，不输出其他动物、画面、分隔线或拼版。去除实物画框、外沿包边和画面外黑边，向四边延展选中图案本身的背景；画面内部属于图案的线条和纹理应保留。`
	// FrameReviewDoubaoWashPrompt upgrades the previous unchanged built-in draft.
	FrameReviewDoubaoWashPrompt = PixelReviewDoubaoWashPrompt + `

外框排除：如果主图是彩色玻璃或镶嵌画，删除最外围完整环绕画面的矩形框、装饰框条和深色包边，不要把它们当作画作主体保留；内部用于描绘主体和景物的镶嵌分隔线可以保留。将主图内容自然延伸到画幅四边，不留画框形状的空白。`
	DoubaoWashPrompt = FrameReviewDoubaoWashPrompt + `

镶嵌画去窗框：不要绘制玻璃窗本身的外轮廓、左右边柱、拱形窗框或底部框条；只重绘窗内图案，画作中的天空、海浪或其他背景直接延伸至画面四边。内部描绘主体的彩色玻璃分隔线仍可保留，但不得形成包围整幅画的框架。`
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
