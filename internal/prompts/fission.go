package prompts

import (
	"fmt"
	"strings"
)

// DefaultFissionPrompt 是四个裂变方向共用的可编辑要求。
const DefaultFissionPrompt = `以附件母图为唯一视觉来源，先识别主体、关键造型、主题与最有辨识度的元素，再进行原创性重构。保留能让人认出母图的核心特征，不要照搬原图的整体布局或细节。画面完整、主体清晰，细节自然，不添加无关文字、水印或拼图。`

var fissionDirections = [...]string{
	"方向一（高调左上偏心特写）：将母图中最具辨识度的主体大幅放大并紧贴画面左上角做大胆不对称裁切，右下方留出大面积明亮纯白或浅暖米色负空间，柔和顺光与细腻特写质感。",
	"方向二（冷调低地平线远景）：将主体大幅缩小置于画面最下方四分之一处并融入开阔户外或建筑远景，上方四分之三留出大片明亮冷蓝天空与水平云层空间，广角自然漫射光。",
	"方向三（正俯视平铺构成）：正90度垂直俯视平铺（Flat-lay），打破单一主体，将母图核心元素拆解重组为多个大小错落的独立单元均匀散布于整个画幅的几何平面网格中，高饱和撞色扁平插画语言。",
	"方向四（暗调右侧斜仰）：通体深炭黑或午夜蓝高对比暗背景，低角度斜仰视角，主体偏画面右侧呈倾斜动势并带强金色侧逆光轮廓高光，厚重画布肌理与大面积深暗负空间。",
}

var fissionTextDirections = [...]string{
	"方向一（高调左上偏心特写）：将核心主题主体大幅放大并紧贴画面左上角做大胆不对称裁切，右下方留出大面积明亮纯白或浅暖米色负空间，柔和顺光与细腻特写质感。",
	"方向二（冷调低地平线远景）：将核心主题主体大幅缩小置于画面最下方四分之一处并融入开阔户外或建筑远景，上方四分之三留出大片明亮冷蓝天空与水平云层空间，广角自然漫射光。",
	"方向三（正俯视平铺构成）：正90度垂直俯视平铺（Flat-lay），打破单一构图，将核心主题元素拆解重组为多个大小错落的独立单元均匀散布于整个画幅的几何平面网格中，高饱和撞色扁平插画语言。",
	"方向四（暗调右侧斜仰）：通体深炭黑或午夜蓝高对比暗背景，低角度斜仰视角，主体偏画面右侧呈倾斜动势并带强金色侧逆光轮廓高光，厚重画布肌理与大面积深暗负空间。",
}

// FissionPrompts 为同一张母图构造四条独立、可重试的提示词。
func FissionPrompts(custom string) []string {
	custom = strings.TrimSpace(custom)
	if custom == "" {
		custom = DefaultFissionPrompt
	}
	out := make([]string, len(fissionDirections))
	for i, direction := range fissionDirections {
		out[i] = fmt.Sprintf("%s\n\n%s\n\n这是同一母图的第 %d/4 个独立变体。只生成一张完整图片，不能拼成四宫格；与其他变体在构图、视角、色彩和场景上明显不同，目标是四张结果两两相似度不超过 60%%。始终以附件母图为来源，保留其可辨识的核心主体和特征。", custom, direction, i+1)
	}
	return out
}

// FissionTextPrompts 为「跳过参考图直接按提示词裂变」构造四条独立提示词。
// 留空或仍为依赖附件母图的默认文案时返回 nil；用户输入恰好四行非空方向时逐行对应四个变体，
// 否则把主题要求展开到四个差异化空间/色调方向。
func FissionTextPrompts(custom string) []string {
	custom = strings.TrimSpace(custom)
	if custom == "" || SameText(custom, DefaultFissionPrompt) {
		return nil
	}
	var lines []string
	for _, raw := range strings.Split(custom, "\n") {
		if line := strings.TrimSpace(raw); line != "" {
			lines = append(lines, line)
		}
	}
	out := make([]string, len(fissionTextDirections))
	if len(lines) == len(fissionTextDirections) {
		for i, line := range lines {
			out[i] = fmt.Sprintf("%s\n\n这是本组裂变的第 %d/4 个独立变体。只生成一张完整图片，不能拼成四宫格，不添加无关文字或水印；与其他变体在构图、视角、色彩和场景上明显不同，目标是四张结果两两相似度不超过 60%%。", line, i+1)
		}
		return out
	}
	for i, direction := range fissionTextDirections {
		out[i] = fmt.Sprintf("%s\n\n%s\n\n这是同一主题的第 %d/4 个独立变体。只生成一张完整图片，不能拼成四宫格，不添加无关文字或水印；与其他变体在构图、视角、色彩和场景上明显不同，目标是四张结果两两相似度不超过 60%%。", custom, direction, i+1)
	}
	return out
}
