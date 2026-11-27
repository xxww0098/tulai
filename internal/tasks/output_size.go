package tasks

import (
	"fmt"
	"image"
	"math"
	"os"
	"strconv"
	"strings"
)

// OutputSize 为豆包、千问、千问国际与 Gemini 指定网页端原生比例。
// 零值表示保持默认/原比例；尺寸仅在发图前通过网页端点击切换，绝不写入提示词，也不在生图或保存过程中二次缩放/裁剪。
type OutputSize struct {
	Ratio string
}

// NativeRatios 是豆包、千问与千问国际网页端原生「比例」菜单共同支持的选项（2026-11 校准）。
var NativeRatios = []string{"9:16", "2:3", "3:4", "1:1", "4:3", "3:2", "16:9"}

// GeminiNativeRatios 是 Gemini 网页端「制作图片 → 宽高比」原生菜单支持的 5 个选项。
var GeminiNativeRatios = []string{"9:16", "3:4", "1:1", "4:3", "16:9"}

// NativeRatiosFor 返回指定平台网页端原生支持的输出比例列表。
func NativeRatiosFor(platform string) []string {
	if platform == "Gemini" {
		return GeminiNativeRatios
	}
	return NativeRatios
}

func supportsOutputSize(platform string) bool {
	return platform == "豆包" || platform == "千问" || platform == "千问国际" || platform == "Gemini"
}

func normalizeNativeRatioIn(raw string, allowed []string) string {
	ratio := strings.TrimSpace(strings.ReplaceAll(raw, "：", ":"))
	if ratio == "" {
		return ""
	}
	parts := strings.Split(ratio, ":")
	if len(parts) != 2 {
		return ""
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || w <= 0 {
		return ""
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || h <= 0 {
		return ""
	}
	want := fmt.Sprintf("%d:%d", w, h)
	for _, native := range allowed {
		if native == want {
			return native
		}
	}
	return ""
}

func normalizeNativeRatio(raw string) string {
	return normalizeNativeRatioIn(raw, NativeRatios)
}

// IsNativeRatio 报告比例字符串是否命中网页端原生「比例」菜单项。
func IsNativeRatio(raw string) bool {
	return normalizeNativeRatio(raw) != ""
}

// Validate 校验输出比例是否为空（自动/原比例）或命中网页端原生比例。
func (o OutputSize) Validate() error {
	return o.ValidateFor("")
}

// ValidateFor 按指定平台校验输出比例是否为空（自动/原比例）或命中该平台网页端原生比例。
func (o OutputSize) ValidateFor(platform string) error {
	raw := strings.TrimSpace(o.Ratio)
	if raw == "" {
		return nil
	}
	allowed := NativeRatiosFor(platform)
	if normalizeNativeRatioIn(raw, allowed) == "" {
		return fmt.Errorf("仅支持网页端原生比例（%s）", strings.Join(allowed, " / "))
	}
	return nil
}

// NativeRatio 返回应通过网页端原生「比例」控件点击设置的选项（如 "4:3"）。
// 未指定比例、非受支持平台或比例不在该平台网页菜单内时返回空。
func (o OutputSize) NativeRatio(platform string) string {
	if !supportsOutputSize(platform) {
		return ""
	}
	return normalizeNativeRatioIn(o.Ratio, NativeRatiosFor(platform))
}

// ClosestNativeRatio 按像素宽高返回最接近的原生「比例」菜单项（9:16 ~ 16:9），
// 供千问等不支持「自动比例」的平台自动贴合参考图/原图画幅，减少留白与黑边。
func ClosestNativeRatio(w, h int) string {
	return ClosestNativeRatioFor("", w, h)
}

// ClosestNativeRatioFor 按像素宽高返回指定平台最接近的原生「比例」菜单项。
func ClosestNativeRatioFor(platform string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	target := float64(w) / float64(h)
	best := ""
	bestDist := math.Inf(1)
	for _, native := range NativeRatiosFor(platform) {
		nw, nh, err := ParseRatioLine(native)
		if err != nil || nw <= 0 || nh <= 0 {
			continue
		}
		dist := math.Abs(math.Log(target / (float64(nw) / float64(nh))))
		if dist < bestDist {
			bestDist = dist
			best = native
		}
	}
	return best
}

func closestImageNativeRatio(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return ""
	}
	return ClosestNativeRatio(cfg.Width, cfg.Height)
}

// ParseRatioLine 把 "4:3" 这类比例折成宽高整数。
func ParseRatioLine(ratio string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(strings.ReplaceAll(ratio, "：", ":")), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("比例格式应为 4:3")
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || w <= 0 {
		return 0, 0, fmt.Errorf("比例格式应为 4:3")
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || h <= 0 {
		return 0, 0, fmt.Errorf("比例格式应为 4:3")
	}
	return w, h, nil
}
