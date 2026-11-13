package prompts

import (
	"fmt"
	"strings"
)

// DefaultFissionPrompt 是四个裂变方向共用的可编辑要求。
const DefaultFissionPrompt = `以附件母图为唯一视觉来源，先识别主体、关键造型、主题与最有辨识度的元素，再进行原创性重构。保留能让人认出母图的核心特征，不要照搬原图的整体布局或细节。画面完整、主体清晰，细节自然，不添加无关文字、水印或拼图。`

var fissionDirections = [...]string{
	"方向一：重构为近景特写。放大母图中最有辨识度的主体，采用不对称裁切和对角线构图，背景简洁，使用明亮暖色与柔和光线。",
	"方向二：重构为远景场景。缩小主体并置于全新环境，以水平分层和大面积背景组织画面，采用冷色调与自然漫射光。",
	"方向三：改变观察角度和视觉语言。以俯视视角重新安排主体和源图元素，采用几何化、平面化的图形构成与高对比配色。",
	"方向四：改变叙事和画面节奏。采用侧视或低角度，将主体与重新组合的母图元素错落排列，使用深色背景、局部高光和丰富肌理。",
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
