package tasks

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/xxww0098/tulai/internal/dedup"
)

// washQualityReport 是洗图本地轻量质检结果（纯像素统计，无外部依赖，单图耗时 <15ms）。
type washQualityReport struct {
	LetterboxSides      []string // 疑似画幅留白/黑边方向：top/bottom/left/right
	NearIdenticalSource bool     // 感知哈希与源图极近，疑似未改写直接回显
	SourceDistance      int      // 与源图 pHash 汉明距离（仅 NearIdenticalSource 为真时有意义）
}

// OK 返回是否未发现明显质量风险。
func (r washQualityReport) OK() bool {
	return len(r.LetterboxSides) == 0 && !r.NearIdenticalSource
}

// Summary 返回给人看的简短质检提示；正常时返回空串。
func (r washQualityReport) Summary() string {
	var parts []string
	if len(r.LetterboxSides) > 0 {
		parts = append(parts, "边缘疑似留白/黑边("+strings.Join(r.LetterboxSides, "/")+")")
	}
	if r.NearIdenticalSource {
		parts = append(parts, fmt.Sprintf("画面与源图几乎一致(pHash差%d位)", r.SourceDistance))
	}
	return strings.Join(parts, "；")
}

// inspectWashQuality 对已解码的洗图结果做本地轻量质检：
//  1. 检查左右或上下对称留白/黑边（跳过四边全白的白底商品图，以及极小/纯色测试图）；
//  2. 若提供非平坦源图指纹，检查 pHash 汉明距离是否 <= 2（疑似原图直出未洗动）。
func inspectWashQuality(decoded image.Image, sourceSig *dedup.Signature) washQualityReport {
	var rep washQualityReport
	if decoded == nil {
		return rep
	}
	b := decoded.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 256 || h < 256 {
		return rep
	}

	// 内核区域必须有一定纹理或与边缘有明显亮度差，避免纯色/测试图误报。
	inner := image.Rect(b.Min.X+w/6, b.Min.Y+h/6, b.Max.X-w/6, b.Max.Y-h/6)
	innerMean, innerStd := rectLumaStats(decoded, inner, 4)
	if innerStd >= 8 {
		bandX := max(6, w*4/100)
		bandY := max(6, h*4/100)
		left := stripUniformBar(decoded, image.Rect(b.Min.X, b.Min.Y+bandY, b.Min.X+bandX, b.Max.Y-bandY), innerMean, innerStd)
		right := stripUniformBar(decoded, image.Rect(b.Max.X-bandX, b.Min.Y+bandY, b.Max.X, b.Max.Y-bandY), innerMean, innerStd)
		top := stripUniformBar(decoded, image.Rect(b.Min.X+bandX, b.Min.Y, b.Max.X-bandX, b.Min.Y+bandY), innerMean, innerStd)
		bottom := stripUniformBar(decoded, image.Rect(b.Min.X+bandX, b.Max.Y-bandY, b.Max.X-bandX, b.Max.Y), innerMean, innerStd)

		// 白底商品图四边都是纯白，属于正常主体留白，不视为画幅黑/白边。
		allWhite := left == "white" && right == "white" && top == "white" && bottom == "white"
		if !allWhite {
			if left != "" && right != "" && left == right {
				rep.LetterboxSides = append(rep.LetterboxSides, "left", "right")
			}
			if top != "" && bottom != "" && top == bottom {
				rep.LetterboxSides = append(rep.LetterboxSides, "top", "bottom")
			}
		}
	}

	if sourceSig != nil && !sourceSig.Flat {
		outSig := dedup.PerceptualHashImage(decoded)
		if !outSig.Flat {
			if dist := dedup.Hamming(outSig.Hash, sourceSig.Hash); dist <= 2 {
				rep.NearIdenticalSource = true
				rep.SourceDistance = dist
			}
		}
	}
	return rep
}

func stripUniformBar(img image.Image, rect image.Rectangle, innerMean, innerStd float64) string {
	mean, std := rectLumaStats(img, rect, 2)
	if std > 5.5 {
		return ""
	}
	if mean >= 245 && (math.Abs(mean-innerMean) >= 25 || innerStd >= 20) {
		return "white"
	}
	if mean <= 12 && (math.Abs(mean-innerMean) >= 25 || innerStd >= 20) {
		return "black"
	}
	return ""
}

func rectLumaStats(img image.Image, rect image.Rectangle, step int) (mean, std float64) {
	if step < 1 {
		step = 1
	}
	rect = rect.Intersect(img.Bounds())
	if rect.Empty() {
		return 0, 0
	}
	var sum, sumSq float64
	var n float64
	switch src := img.(type) {
	case *image.NRGBA:
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			row := (y - src.Rect.Min.Y) * src.Stride
			for x := rect.Min.X; x < rect.Max.X; x += step {
				i := row + (x-src.Rect.Min.X)*4
				a := float64(src.Pix[i+3])
				var luma float64
				if a == 255 {
					luma = 0.299*float64(src.Pix[i]) + 0.587*float64(src.Pix[i+1]) + 0.114*float64(src.Pix[i+2])
				} else if a > 0 {
					luma = (0.299*float64(src.Pix[i]) + 0.587*float64(src.Pix[i+1]) + 0.114*float64(src.Pix[i+2])) * (a / 255.0)
				}
				sum += luma
				sumSq += luma * luma
				n++
			}
		}
	case *image.RGBA:
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			row := (y - src.Rect.Min.Y) * src.Stride
			for x := rect.Min.X; x < rect.Max.X; x += step {
				i := row + (x-src.Rect.Min.X)*4
				luma := 0.299*float64(src.Pix[i]) + 0.587*float64(src.Pix[i+1]) + 0.114*float64(src.Pix[i+2])
				sum += luma
				sumSq += luma * luma
				n++
			}
		}
	case *image.YCbCr:
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			for x := rect.Min.X; x < rect.Max.X; x += step {
				yi := src.YOffset(x, y)
				ci := src.COffset(x, y)
				r, g, b := color.YCbCrToRGB(src.Y[yi], src.Cb[ci], src.Cr[ci])
				luma := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
				sum += luma
				sumSq += luma * luma
				n++
			}
		}
	default:
		for y := rect.Min.Y; y < rect.Max.Y; y += step {
			for x := rect.Min.X; x < rect.Max.X; x += step {
				r, g, b, _ := img.At(x, y).RGBA()
				luma := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 257.0
				sum += luma
				sumSq += luma * luma
				n++
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	mean = sum / n
	variance := sumSq/n - mean*mean
	if variance < 0 {
		variance = 0
	}
	return mean, math.Sqrt(variance)
}
