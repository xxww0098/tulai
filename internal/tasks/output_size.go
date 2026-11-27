package tasks

import (
	"fmt"
	"image"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/xxww0098/tulai/internal/store"
)

// OutputSize 为豆包、千问、千问国际、Gemini 与即梦指定网页端原生比例与可选分辨率。
// Ratio 为空表示走该供应商网页的自动/智能/默认比例；尺寸仅在发图前通过网页端点击切换，
// 绝不写入提示词，也不在生图或保存过程中二次缩放/裁剪。
// AllowedRatios 与 AllowedResolutions 由供应商实时网页探测缓存注入，为空时回退该平台内置原生默认列表。
type OutputSize struct {
	Ratio              string
	Resolution         string
	AutoValue          string
	AllowedRatios      []string
	AllowedResolutions []string
}

// NativeRatios 是通用网页端原生「比例」菜单的默认兜底列表（2026-11 校准）。
var NativeRatios = []string{"9:16", "2:3", "3:4", "1:1", "4:3", "3:2", "16:9"}

// GeminiNativeRatios 是 Gemini 网页端「制作图片 → 宽高比」默认原生菜单支持的 5 个选项。
var GeminiNativeRatios = []string{"9:16", "3:4", "1:1", "4:3", "16:9"}

// JimengNativeRatios 是即梦网页端默认原生「画面比例」支持的 8 个选项（含 21:9）。
var JimengNativeRatios = []string{"21:9", "16:9", "3:2", "4:3", "1:1", "3:4", "2:3", "9:16"}

// NativeRatiosFor 返回指定平台网页端内置默认支持的输出比例列表。
func NativeRatiosFor(platform string) []string {
	switch strings.TrimSpace(platform) {
	case "Gemini":
		return GeminiNativeRatios
	case "即梦":
		return JimengNativeRatios
	default:
		return NativeRatios
	}
}

// NativeResolutionsFor 返回指定平台网页端内置默认支持的分辨率/清晰度列表。
func NativeResolutionsFor(platform string) []string {
	return store.DefaultProviderOutputCaps(platform).Resolutions
}

func supportsOutputSize(platform string) bool {
	return store.OutputCapsSupportedPlatform(platform)
}

func (o OutputSize) allowedRatiosFor(platform string) []string {
	if len(o.AllowedRatios) > 0 {
		return o.AllowedRatios
	}
	return NativeRatiosFor(platform)
}

func (o OutputSize) allowedResolutionsFor(platform string) []string {
	if len(o.AllowedResolutions) > 0 {
		return o.AllowedResolutions
	}
	return NativeResolutionsFor(platform)
}

func normalizeNativeRatioIn(raw string, allowed []string) string {
	want := store.CanonicalRatioToken(raw)
	if want == "" {
		return ""
	}
	for _, native := range allowed {
		if store.CanonicalRatioToken(native) == want {
			return want
		}
	}
	return ""
}

func normalizeNativeResolutionIn(raw string, allowed []string) string {
	want := store.CanonicalResolutionToken(raw)
	if want == "" {
		return ""
	}
	for _, native := range allowed {
		norm := store.CanonicalResolutionToken(native)
		if norm != "" && strings.EqualFold(norm, want) {
			return norm
		}
	}
	return ""
}

func normalizeNativeRatio(raw string) string {
	return normalizeNativeRatioIn(raw, NativeRatios)
}

// IsNativeRatio 报告比例字符串是否命中默认网页端原生「比例」菜单项。
func IsNativeRatio(raw string) bool {
	return normalizeNativeRatio(raw) != ""
}

// Validate 校验输出比例是否为空（自动/默认）或命中网页端原生比例。
func (o OutputSize) Validate() error {
	return o.ValidateFor("")
}

// ValidateFor 按指定平台（及其动态探测到的支持列表）校验输出比例与分辨率。
func (o OutputSize) ValidateFor(platform string) error {
	raw := strings.TrimSpace(o.Ratio)
	if raw != "" {
		allowed := o.allowedRatiosFor(platform)
		if normalizeNativeRatioIn(raw, allowed) == "" {
			return fmt.Errorf("仅支持当前网页原生比例（%s）", strings.Join(allowed, " / "))
		}
	}
	rawRes := strings.TrimSpace(o.Resolution)
	if rawRes != "" {
		allowedRes := o.allowedResolutionsFor(platform)
		if len(allowedRes) == 0 || normalizeNativeResolutionIn(rawRes, allowedRes) == "" {
			if len(allowedRes) == 0 {
				return fmt.Errorf("当前网页不支持单独设置分辨率 %s", rawRes)
			}
			return fmt.Errorf("仅支持当前网页原生分辨率（%s）", strings.Join(allowedRes, " / "))
		}
	}
	return nil
}

// NativeRatio 返回应通过网页端原生「比例」控件点击设置的选项（如 "4:3"、"21:9"）。
// 未指定比例、非受支持平台或比例不在该平台网页菜单内时返回空。
func (o OutputSize) NativeRatio(platform string) string {
	if !supportsOutputSize(platform) {
		return ""
	}
	return normalizeNativeRatioIn(o.Ratio, o.allowedRatiosFor(platform))
}

// NativeResolution 返回应通过网页端原生分辨率控件设置的选项（如即梦 "2K" / "4K"）。
// 未指定或当前网页不支持时返回空。
func (o OutputSize) NativeResolution(platform string) string {
	if !supportsOutputSize(platform) {
		return ""
	}
	return normalizeNativeResolutionIn(o.Resolution, o.allowedResolutionsFor(platform))
}

// ClosestNativeRatio 按像素宽高返回最接近的原生「比例」菜单项（9:16 ~ 16:9），
// 供千问等不支持「自动比例」的平台在零值兜底时自动贴合参考图/原图画幅。
func ClosestNativeRatio(w, h int) string {
	return ClosestNativeRatioFor("", w, h)
}

// ClosestNativeRatioFor 按像素宽高返回指定平台最接近的原生「比例」菜单项。
func ClosestNativeRatioFor(platform string, w, h int) string {
	return closestRatioIn(NativeRatiosFor(platform), w, h)
}

func closestRatioIn(allowed []string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	target := float64(w) / float64(h)
	best := ""
	bestDist := math.Inf(1)
	for _, native := range allowed {
		nw, nh, err := ParseRatioLine(native)
		if err != nil || nw <= 0 || nh <= 0 {
			continue
		}
		dist := math.Abs(math.Log(target / (float64(nw) / float64(nh))))
		if dist < bestDist {
			bestDist = dist
			best = store.CanonicalRatioToken(native)
		}
	}
	return best
}

func closestImageNativeRatio(path string) string {
	return closestImageNativeRatioFor("", path, nil)
}

func closestImageNativeRatioFor(platform, path string, allowed []string) string {
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
	if len(allowed) == 0 {
		allowed = NativeRatiosFor(platform)
	}
	return closestRatioIn(allowed, cfg.Width, cfg.Height)
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
