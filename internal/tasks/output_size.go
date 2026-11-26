package tasks

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/xxww0098/tulai/internal/imageedit"
)

// OutputSize 为豆包、千问结果指定比例或像素。零值保留原尺寸。
type OutputSize struct {
	Enabled bool
	Ratio   string
	Width   int
	Height  int
	Crop    bool
}

func supportsOutputSize(platform string) bool {
	return platform == "豆包" || platform == "千问" || platform == "千问国际"
}

func (o OutputSize) ratio() (int, int, error) {
	ratio := strings.TrimSpace(strings.ReplaceAll(o.Ratio, "：", ":"))
	if o.Width == 0 && o.Height == 0 && ratio == "" {
		if o.Enabled {
			return 0, 0, fmt.Errorf("请填写比例或宽高像素")
		}
		return 0, 0, nil
	}
	w, h := o.Width, o.Height
	if ratio != "" {
		if w != 0 || h != 0 {
			return 0, 0, fmt.Errorf("比例和像素尺寸只能选择一种")
		}
		parts := strings.Split(ratio, ":")
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("比例格式应为 5:4")
		}
		var err error
		w, err = strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return 0, 0, fmt.Errorf("比例格式应为 5:4")
		}
		h, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return 0, 0, fmt.Errorf("比例格式应为 5:4")
		}
	}
	if w <= 0 || h <= 0 || w > 65535 || h > 65535 {
		return 0, 0, fmt.Errorf("宽高须为正整数，单边不超过 65535，总像素不超过 5000 万")
	}
	a, b := w, h
	for b != 0 {
		a, b = b, a%b
	}
	rw, rh := w/a, h/a
	if (ratio == "" && w > imageedit.MaxPixels/h) || (ratio != "" && rw > imageedit.MaxPixels/rh) {
		return 0, 0, fmt.Errorf("总像素不超过 5000 万")
	}
	return rw, rh, nil
}

func (o OutputSize) Validate() error {
	_, _, err := o.ratio()
	return err
}

// NativeRatios 是豆包图像模式「比例」菜单提供的选项（2026-11 校准）。
var NativeRatios = []string{"9:16", "2:3", "3:4", "1:1", "4:3", "3:2", "16:9"}

// NativeRatio 返回应改用平台原生「比例」控件的设置（如 "4:3"）。
// 折算比例命中原生菜单时返回该选项；未启用、非受支持平台、比例非法或不在菜单里都返回空。
func (o OutputSize) NativeRatio(platform string) string {
	if !supportsOutputSize(platform) {
		return ""
	}
	w, h, err := o.ratio()
	if err != nil || w == 0 {
		return ""
	}
	want := fmt.Sprintf("%d:%d", w, h)
	for _, native := range NativeRatios {
		if native == want {
			return native
		}
	}
	return ""
}

// ClosestNativeRatio 按像素宽高返回最接近的原生「比例」菜单项（9:16 ~ 16:9），
// 供千问等不支持「自动比例」的平台自动贴合参考图/原图画幅，减少留白与黑边。
func ClosestNativeRatio(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	target := float64(w) / float64(h)
	best := ""
	bestDist := math.Inf(1)
	for _, native := range NativeRatios {
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

// AppendRatioLine 是提示词里的比例兜底说明，豆包原生比例不可用时使用。
func AppendRatioLine(prompt string, w, h int) string {
	return strings.TrimSpace(prompt + fmt.Sprintf("\n请让每张输出图片的画面宽高比为 %d:%d，完整呈现主体。", w, h))
}

// ParseRatioLine 把 "4:3" 这类比例折成宽高整数。
func ParseRatioLine(ratio string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(strings.ReplaceAll(ratio, "：", ":")), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("比例格式应为 5:4")
	}
	w, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("比例格式应为 5:4")
	}
	h, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("比例格式应为 5:4")
	}
	return w, h, nil
}

func (o OutputSize) prompt(platform, prompt string) string {
	if !supportsOutputSize(platform) {
		return prompt
	}
	w, h, err := o.ratio()
	if err != nil || w == 0 {
		return prompt
	}
	return AppendRatioLine(prompt, w, h)
}

// nativeRatioMatchTolerance 允许平台原生比例出图因扩散模型 32/64 像素网格对齐产生的轻微宽高比偏差
// （例如千问 3:4 原生图 1760×2368 偏差约 0.9%、1140×1472 偏差约 3.2%、16:9 原生图 1664×928 偏差约 0.9%），
// 避免在「仅指定原生比例、未指定固定像素」时给原生满幅成图人为补上白边。
// 相邻原生比例间距 >= 11.8%，不会误放行错误画幅。
const nativeRatioMatchTolerance = 0.04

func isNativeRatio(rw, rh int) bool {
	want := fmt.Sprintf("%d:%d", rw, rh)
	for _, native := range NativeRatios {
		if native == want {
			return true
		}
	}
	return false
}

func (o OutputSize) plan(cfg image.Config) (w, h, rw, rh int, need bool, err error) {
	rw, rh, err = o.ratio()
	if err != nil || rw == 0 {
		return 0, 0, 0, 0, false, err
	}
	w, h = o.Width, o.Height
	if w == 0 {
		if cfg.Width >= 256 && cfg.Height >= 256 && isNativeRatio(rw, rh) {
			actual := float64(cfg.Width) / float64(cfg.Height)
			target := float64(rw) / float64(rh)
			if math.Abs(math.Log(actual/target)) <= nativeRatioMatchTolerance {
				return cfg.Width, cfg.Height, rw, rh, false, nil
			}
		}
		var k int
		if o.Crop {
			k = min(cfg.Width/rw, cfg.Height/rh)
			if k == 0 {
				return 0, 0, 0, 0, false, fmt.Errorf("原图尺寸过小，无法按该比例裁剪")
			}
		} else {
			k = max((cfg.Width+rw-1)/rw, (cfg.Height+rh-1)/rh)
		}
		if k > 65535/rw || k > 65535/rh || int64(k)*int64(rw)*int64(k)*int64(rh) > imageedit.MaxPixels {
			return 0, 0, 0, 0, false, fmt.Errorf("按比例处理后的图片超过尺寸上限")
		}
		w, h = rw*k, rh*k
	}
	if cfg.Width == w && cfg.Height == h {
		return w, h, rw, rh, false, nil
	}
	return w, h, rw, rh, true, nil
}

func (o OutputSize) editOptions(w, h, rw, rh int, format string) imageedit.Options {
	opts := imageedit.Options{ResizeWidth: w, ResizeHeight: h, ResizeMode: "contain", ResizeUpscale: true}
	if o.Crop {
		opts.CropRatio = fmt.Sprintf("%d:%d", rw, rh)
		opts.ResizeMode = "fill"
	}
	if format != "" {
		opts.Format = format
	}
	return opts
}

func (o OutputSize) process(ctx context.Context, data []byte, cfg image.Config) ([]byte, error) {
	w, h, rw, rh, need, err := o.plan(cfg)
	if err != nil || !need {
		return data, err
	}
	result, err := imageedit.Process(ctx, data, o.editOptions(w, h, rw, rh, ""))
	if err != nil {
		return nil, err
	}
	if result.Width != w || result.Height != h {
		return nil, fmt.Errorf("实际尺寸 %dx%d 与目标尺寸 %dx%d 不符", result.Width, result.Height, w, h)
	}
	return result.Data, nil
}

// processImage 直接对已解码的像素做比例/尺寸调整并按 targetFormat 编码，省去中间 PNG 编码与再解码。
func (o OutputSize) processImage(ctx context.Context, decoded image.Image, targetFormat string, cfg image.Config) ([]byte, bool, error) {
	w, h, rw, rh, need, err := o.plan(cfg)
	if err != nil || !need {
		return nil, false, err
	}
	result, err := imageedit.ProcessImage(ctx, decoded, targetFormat, o.editOptions(w, h, rw, rh, targetFormat))
	if err != nil {
		return nil, false, err
	}
	if result.Width != w || result.Height != h {
		return nil, false, fmt.Errorf("实际尺寸 %dx%d 与目标尺寸 %dx%d 不符", result.Width, result.Height, w, h)
	}
	return result.Data, true, nil
}
