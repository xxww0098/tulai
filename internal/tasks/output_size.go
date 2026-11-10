package tasks

import (
	"context"
	"fmt"
	"image"
	"strconv"
	"strings"

	"github.com/xxww0098/tulai/internal/imageedit"
)

// OutputSize 为豆包结果指定比例或像素。零值保留原尺寸。
type OutputSize struct {
	Enabled bool
	Ratio   string
	Width   int
	Height  int
	Crop    bool
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

// NativeRatio 返回应改用豆包原生「比例」控件的设置（如 "4:3"）。
// 折算比例命中原生菜单时返回该选项；未启用、非豆包、比例非法或不在菜单里都返回空。
func (o OutputSize) NativeRatio(platform string) string {
	if platform != "豆包" {
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
	if platform != "豆包" {
		return prompt
	}
	w, h, err := o.ratio()
	if err != nil || w == 0 {
		return prompt
	}
	return AppendRatioLine(prompt, w, h)
}

func (o OutputSize) process(ctx context.Context, data []byte, cfg image.Config) ([]byte, error) {
	rw, rh, err := o.ratio()
	if err != nil || rw == 0 {
		return data, err
	}
	w, h := o.Width, o.Height
	if w == 0 {
		var k int
		if o.Crop {
			k = min(cfg.Width/rw, cfg.Height/rh)
			if k == 0 {
				return nil, fmt.Errorf("原图尺寸过小，无法按该比例裁剪")
			}
		} else {
			k = max((cfg.Width+rw-1)/rw, (cfg.Height+rh-1)/rh)
		}
		if k > 65535/rw || k > 65535/rh || int64(k)*int64(rw)*int64(k)*int64(rh) > imageedit.MaxPixels {
			return nil, fmt.Errorf("按比例处理后的图片超过尺寸上限")
		}
		w, h = rw*k, rh*k
	}
	if cfg.Width == w && cfg.Height == h {
		return data, nil
	}
	opts := imageedit.Options{ResizeWidth: w, ResizeHeight: h, ResizeMode: "contain", ResizeUpscale: true}
	if o.Crop {
		opts.CropRatio = fmt.Sprintf("%d:%d", rw, rh)
		opts.ResizeMode = "fill"
	}
	result, err := imageedit.Process(ctx, data, opts)
	if err != nil {
		return nil, err
	}
	if result.Width != w || result.Height != h {
		return nil, fmt.Errorf("实际尺寸 %dx%d 与目标尺寸 %dx%d 不符", result.Width, result.Height, w, h)
	}
	return result.Data, nil
}
