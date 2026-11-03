package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/imageedit"
	"github.com/xxww0098/tulai/internal/logging"
)

// 图片编辑页的设置项。配方、比例和尺寸都放在配置库里。
const (
	editKeyOptions  = "imageedit_options"
	editKeyEnabled  = "imageedit_enabled"
	editKeyExpanded = "imageedit_expanded"
	editKeyOutDir   = "imageedit_outdir"
	editKeySuffix   = "imageedit_suffix"
	editKeyRecipes  = "imageedit_recipes"
	editKeyRatios   = "imageedit_ratios"
	editKeySizes    = "imageedit_sizes"
	editKeyUniform  = "imageedit_uniform_crop"
)

// editMaxFiles 是一次最多处理的数量，和 moza 的图库交棒上限一致。
const editMaxFiles = 5000

// editFeatureSet 是格式、裁剪和画质三张卡片的启停。
// 灰度不是卡片开关，直接看选项里的 grayscale。
type editFeatureSet struct {
	Format bool `json:"format"`
	Crop   bool `json:"crop"`
	Adjust bool `json:"adjust"`
}

// editFile 是胶片条里的一张图。
type editFile struct {
	Path        string
	Name        string
	Status      string // pending / ok / failed
	Err         string
	Output      string
	Thumb       *ui.Bitmap
	ThumbQueued bool
}

// editRecipe 是「我的配方」里的一个。
type editRecipe struct {
	Name    string            `json:"name"`
	Options imageedit.Options `json:"options"`
}

// editSize 是「我的尺寸」里的一档。
type editSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

// editCropBox 是源图像素坐标下的裁切框。
type editCropBox struct {
	X, Y, W, H float64
}

// editCropDrag 是一次裁切框拖拽的起点和最新框。
type editCropDrag struct {
	Mode   string // draw / move / resize
	Corner string
	StartX float64
	StartY float64
	Box    editCropBox
	Latest editCropBox
}

// editChip 是页头「已选功能」的一枚。
type editChip struct {
	ID    string
	Label string
}

// imageEditState 是图片编辑页的全部状态。
type imageEditState struct {
	files    []*editFile
	selected int

	opts        imageedit.Options
	enabled     editFeatureSet
	expanded    map[string]bool
	uniformCrop bool
	fileOps     map[string]imageedit.Options

	mode     string // crop / fit / pad / stretch
	dimTab   int    // 0 尺寸 1 比例
	ratioTab int    // 0 推荐 1 我的
	dimW     int
	dimH     int
	dimWText string
	dimHText string
	locked   bool
	quality  string

	outDir string
	suffix string
	notice string

	recipes []editRecipe
	ratios  []string
	sizes   []editSize

	flowScroll  ui.ScrollState
	stripScroll ui.ScrollState
	stripViewW  float32
	stripRect   ui.Rect

	// 滑杆每帧绑定数值，拖动时由 SliderBase 写入。
	fBrightness, fContrast, fSaturation, fBlur float64

	running         bool
	runCancel       chan struct{} // 关掉它让批次在下一张之前停下
	runLock         bool
	progressDone    int
	progressTotal   int
	progressCurrent string

	// picks 是胶片条上勾选待处理的图片路径；为空时按「全部」处理。
	picks      map[string]bool
	pickAnchor int  // Shift 连选的起点下标
	resultOpen bool // 处理结果弹窗

	recipeOpen bool
	recipeName string
	recipeErr  string

	// 预览与缩放
	orig     *ui.Bitmap
	out      *ui.Bitmap
	origPath string
	srcW     int
	srcH     int
	outW     int
	outH     int

	zoom       float64
	panX, panY float64
	compare    bool
	split      float64
	panning    bool
	dragMode   string // "" / pan / split / crop
	panStartX  float64
	panStartY  float64
	panOriginX float64
	panOriginY float64
	splitStart float64

	cropDrag *editCropDrag
	liveBox  *editCropBox

	previewSig     string
	previewGen     int
	previewPending bool
	previewTimer   *time.Timer

	// 最近一帧的舞台几何。stageRect/imageRect 是窗口坐标，绘制用；
	// imageLocal 是图片相对舞台的位置，指针事件给的是元素内坐标，换算用。
	stageRect  ui.Rect
	imageRect  ui.Rect
	imageLocal ui.Rect
	baseW      float32
	baseH      float32
	previewW   float32
	previewH   float32

	thumbSem chan struct{}
	thumbGen int
}

func (s *imageEditState) selectedFile() *editFile {
	if s.selected < 0 || s.selected >= len(s.files) {
		return nil
	}
	return s.files[s.selected]
}

// editResizeLabels 是「修改方式」四档在摘要里的叫法。
var editResizeLabels = map[string]string{
	"inside":  "等比缩放",
	"fill":    "拉伸",
	"contain": "纯色背景",
}

// editSizePresets 是尺寸 tab 的推荐列表。w=-1 表示原尺寸。
var editSizePresets = []struct {
	Label string
	W, H  int
}{
	{"自由调整", 0, 0},
	{"原尺寸", -1, -1},
	{"公众号首图 900×383", 900, 383},
	{"公众号次图 200×200", 200, 200},
	{"朋友圈封面 1080×1080", 1080, 1080},
	{"电脑壁纸 1920×1080", 1920, 1080},
	{"Logo设计 500×500", 500, 500},
	{"方形主图 800×800", 800, 800},
	{"竖版主图 800×1200", 800, 1200},
	{"拼多多店铺首页 750×1000", 750, 1000},
	{"标准1寸 295×413", 295, 413},
	{"标准2寸 413×626", 413, 626},
	{"二代身份证 358×441", 358, 441},
}

// editRatioPresets 是比例 tab 的推荐列表。
var editRatioPresets = []struct {
	Ratio string
	Label string
}{
	{"1:1", "正方形"},
	{"4:3", "标准屏幕（横）"},
	{"3:4", "标准屏幕（竖）"},
	{"3:2", "单反相机（横）"},
	{"2:3", "单反相机（竖）"},
	{"16:9", "视频横屏"},
	{"9:16", "视频竖屏"},
	{"5:3", "宽屏相册"},
	{"18:9", "全面屏（横）"},
	{"9:18", "全面屏（竖）"},
}

// ---- 设置 ----

func (a *App) loadImageEditSettings() {
	s := a.store
	st := &a.edit
	st.enabled = editFeatureSet{}
	st.expanded = map[string]bool{}
	st.fileOps = map[string]imageedit.Options{}
	st.thumbSem = make(chan struct{}, 4)
	st.zoom = 1
	st.panX, st.panY = 0, 0
	st.split = 50
	st.locked = true
	st.mode = "crop"
	st.selected = -1

	if raw := strings.TrimSpace(s.Get(editKeyOptions)); raw != "" {
		_ = json.Unmarshal([]byte(raw), &st.opts)
	}
	enabledRaw := strings.TrimSpace(s.Get(editKeyEnabled))
	if enabledRaw != "" {
		_ = json.Unmarshal([]byte(enabledRaw), &st.enabled)
	} else {
		st.enabled = editEnabledFromOptions(st.opts)
	}
	if raw := strings.TrimSpace(s.Get(editKeyExpanded)); raw != "" {
		_ = json.Unmarshal([]byte(raw), &st.expanded)
	} else {
		st.expanded = editDefaultExpanded(st.enabled)
	}
	st.uniformCrop = s.Get(editKeyUniform) != "0"
	st.outDir = strings.TrimSpace(s.Get(editKeyOutDir))
	st.suffix = s.Get(editKeySuffix)
	if raw := strings.TrimSpace(s.Get(editKeyRecipes)); raw != "" {
		_ = json.Unmarshal([]byte(raw), &st.recipes)
	}
	if raw := strings.TrimSpace(s.Get(editKeyRatios)); raw != "" {
		_ = json.Unmarshal([]byte(raw), &st.ratios)
	}
	if raw := strings.TrimSpace(s.Get(editKeySizes)); raw != "" {
		_ = json.Unmarshal([]byte(raw), &st.sizes)
	}
	st.mode = editModeFromOptions(st.opts)
	if st.opts.CropWidth > 0 || st.opts.CropHeight > 0 {
		st.dimW, st.dimH = st.opts.CropWidth, st.opts.CropHeight
	} else {
		st.dimW, st.dimH = st.opts.ResizeWidth, st.opts.ResizeHeight
	}
	st.dimWText = editDimText(st.dimW)
	st.dimHText = editDimText(st.dimH)
	st.quality = editQualityText(st.opts.JPEGQuality)
}

// editEnsureMaps 给没走设置加载的测试和旧存档补上容器。
func (a *App) editEnsureMaps() {
	st := &a.edit
	if st.expanded == nil {
		st.expanded = map[string]bool{}
	}
	if st.fileOps == nil {
		st.fileOps = map[string]imageedit.Options{}
	}
	if st.picks == nil {
		st.picks = map[string]bool{}
	}
	if st.thumbSem == nil {
		st.thumbSem = make(chan struct{}, 4)
	}
	if st.zoom <= 0 {
		st.zoom = 1
	}
	if st.split <= 0 {
		st.split = 50
	}
}

func (a *App) saveImageEditSettings() {
	if a.store == nil {
		return
	}
	s := a.store
	st := &a.edit
	s.Set(editKeyOptions, editJSON(st.opts))
	s.Set(editKeyEnabled, editJSON(st.enabled))
	s.Set(editKeyExpanded, editJSON(st.expanded))
	s.Set(editKeyOutDir, strings.TrimSpace(st.outDir))
	s.Set(editKeySuffix, st.suffix)
	s.Set(editKeyRecipes, editJSON(st.recipes))
	s.Set(editKeyRatios, editJSON(st.ratios))
	s.Set(editKeySizes, editJSON(st.sizes))
	if st.uniformCrop {
		s.Set(editKeyUniform, "1")
	} else {
		s.Set(editKeyUniform, "0")
	}
}

func editJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func editDimText(v int) string {
	if v <= 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func editQualityText(v int) string {
	if v <= 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func editDefaultExpanded(enabled editFeatureSet) map[string]bool {
	return map[string]bool{
		"format": enabled.Format,
		"crop":   enabled.Crop,
		"adjust": enabled.Adjust,
	}
}

// editFeatureConfigured 判断某组功能在选项里有没有非默认值。
func editFeatureConfigured(id string, o imageedit.Options) bool {
	switch id {
	case "format":
		return (o.Format != "" && o.Format != "original") || o.JPEGQuality != 0
	case "crop":
		return o.CropWidth > 0 || o.CropHeight > 0 || strings.TrimSpace(o.CropRatio) != "" ||
			o.ResizeWidth > 0 || o.ResizeHeight > 0 || o.ResizeMode != "" ||
			o.Rotate != 0 || o.FlipHorizontal || o.FlipVertical
	case "adjust":
		return o.Brightness != 0 || o.Contrast != 0 || o.Saturation != 0 || o.Blur > 0 || o.Sharpen
	}
	return false
}

func editEnabledFromOptions(o imageedit.Options) editFeatureSet {
	return editFeatureSet{
		Format: editFeatureConfigured("format", o),
		Crop:   editFeatureConfigured("crop", o),
		Adjust: editFeatureConfigured("adjust", o),
	}
}

func editModeFromOptions(o imageedit.Options) string {
	switch {
	case strings.TrimSpace(o.CropRatio) != "" || o.CropWidth > 0 || o.CropHeight > 0:
		return "crop"
	case o.ResizeMode == "contain":
		return "pad"
	case o.ResizeMode == "fill":
		return "stretch"
	case o.ResizeWidth > 0 || o.ResizeHeight > 0:
		return "fit"
	default:
		return "crop"
	}
}

// editEffOptions 是真正会送进引擎的选项：关掉的卡片保留配置但不出力。
func (a *App) editEffOptions() imageedit.Options {
	st := &a.edit
	o := st.opts
	if !st.enabled.Format {
		o.Format = ""
		o.JPEGQuality = 0
	}
	if !st.enabled.Crop {
		o.CropWidth, o.CropHeight, o.CropRatio = 0, 0, ""
		o.CropLeft, o.CropTop = nil, nil
		o.ResizeWidth, o.ResizeHeight, o.ResizeMode = 0, 0, ""
		o.Rotate, o.FlipHorizontal, o.FlipVertical = 0, false, false
	}
	if !st.enabled.Adjust {
		o.Brightness, o.Contrast, o.Saturation = 0, 0, 0
		o.Blur, o.Sharpen = 0, false
	}
	return o
}

// editOptionsFor 合并每张图自己的裁剪位置。
func (a *App) editOptionsFor(path string) imageedit.Options {
	o := a.editEffOptions()
	if !a.edit.enabled.Crop || a.edit.uniformCrop {
		return o
	}
	ov, ok := a.edit.fileOps[path]
	if !ok {
		return o
	}
	// 只覆盖这一张图真正设过的字段，其余沿用公共配置。
	if ov.CropWidth > 0 {
		o.CropWidth = ov.CropWidth
	}
	if ov.CropHeight > 0 {
		o.CropHeight = ov.CropHeight
	}
	if strings.TrimSpace(ov.CropRatio) != "" {
		o.CropRatio = ov.CropRatio
	}
	if ov.CropLeft != nil {
		o.CropLeft = ov.CropLeft
	}
	if ov.CropTop != nil {
		o.CropTop = ov.CropTop
	}
	return o
}

// editChips 由生效选项推导页头的功能标签。
func (a *App) editChips() []editChip {
	o := a.editEffOptions()
	var out []editChip
	if o.Format != "" && o.Format != "original" {
		out = append(out, editChip{"format", "格式·" + strings.ToUpper(o.Format)})
	} else if o.JPEGQuality > 0 {
		out = append(out, editChip{"format", fmt.Sprintf("品质·%d", o.JPEGQuality)})
	}
	if strings.TrimSpace(o.CropRatio) != "" {
		out = append(out, editChip{"crop", "裁剪·" + o.CropRatio})
	} else if o.CropWidth > 0 && o.CropHeight > 0 {
		out = append(out, editChip{"crop", fmt.Sprintf("裁剪·%d×%d", o.CropWidth, o.CropHeight)})
	}
	if o.ResizeWidth > 0 || o.ResizeHeight > 0 {
		mode := "缩放"
		switch o.ResizeMode {
		case "contain":
			mode = "纯色背景"
		case "fill":
			mode = "拉伸"
		}
		w, h := "auto", "auto"
		if o.ResizeWidth > 0 {
			w = strconv.Itoa(o.ResizeWidth)
		}
		if o.ResizeHeight > 0 {
			h = strconv.Itoa(o.ResizeHeight)
		}
		out = append(out, editChip{"resize", mode + "·" + w + "×" + h})
	}
	if o.Rotate != 0 || o.FlipHorizontal || o.FlipVertical {
		out = append(out, editChip{"rotate", "旋转/翻转"})
	}
	var adj []string
	if o.Brightness != 0 {
		adj = append(adj, fmt.Sprintf("亮度%+d", o.Brightness))
	}
	if o.Contrast != 0 {
		adj = append(adj, fmt.Sprintf("对比%+d", o.Contrast))
	}
	if o.Saturation != 0 {
		adj = append(adj, fmt.Sprintf("饱和%+d", o.Saturation))
	}
	if o.Blur > 0 {
		adj = append(adj, "模糊"+strconv.FormatFloat(o.Blur, 'f', -1, 64))
	}
	if o.Sharpen {
		adj = append(adj, "锐化")
	}
	if len(adj) > 0 {
		out = append(out, editChip{"adjust", strings.Join(adj, "·")})
	}
	if o.Grayscale {
		out = append(out, editChip{"gray", "灰度"})
	}
	return out
}

// editSummary 是卡片头上的单行参数摘要。
func editSummary(id string, o imageedit.Options, hint string) string {
	switch id {
	case "format":
		var parts []string
		if o.Format != "" && o.Format != "original" {
			parts = append(parts, "转 "+strings.ToUpper(o.Format))
		}
		if o.JPEGQuality > 0 {
			parts = append(parts, fmt.Sprintf("品质 %d", o.JPEGQuality))
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
	case "crop":
		var parts []string
		if strings.TrimSpace(o.CropRatio) != "" {
			parts = append(parts, "比例 "+o.CropRatio)
		}
		if o.CropWidth > 0 && o.CropHeight > 0 {
			parts = append(parts, fmt.Sprintf("裁剪 %d×%d", o.CropWidth, o.CropHeight))
		}
		if o.ResizeWidth > 0 || o.ResizeHeight > 0 {
			mode := editResizeLabels[o.ResizeMode]
			if mode == "" {
				mode = "缩放"
			}
			w, h := "auto", "auto"
			if o.ResizeWidth > 0 {
				w = strconv.Itoa(o.ResizeWidth)
			}
			if o.ResizeHeight > 0 {
				h = strconv.Itoa(o.ResizeHeight)
			}
			parts = append(parts, mode+" "+w+"×"+h)
		}
		if o.Rotate != 0 {
			parts = append(parts, fmt.Sprintf("旋转%d°", o.Rotate))
		}
		if o.FlipHorizontal {
			parts = append(parts, "水平翻转")
		}
		if o.FlipVertical {
			parts = append(parts, "垂直翻转")
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
	case "adjust":
		var parts []string
		if o.Brightness != 0 {
			parts = append(parts, fmt.Sprintf("亮度 %+d", o.Brightness))
		}
		if o.Contrast != 0 {
			parts = append(parts, fmt.Sprintf("对比 %+d", o.Contrast))
		}
		if o.Saturation != 0 {
			parts = append(parts, fmt.Sprintf("饱和 %+d", o.Saturation))
		}
		if o.Blur > 0 {
			parts = append(parts, "模糊 "+strconv.FormatFloat(o.Blur, 'f', -1, 64))
		}
		if o.Sharpen {
			parts = append(parts, "锐化")
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
	}
	return hint
}

// ---- 交互状态 ----

func (a *App) editMarkUsed(id string) {
	switch id {
	case "format":
		a.edit.enabled.Format = true
	case "crop":
		a.edit.enabled.Crop = true
	case "adjust":
		a.edit.enabled.Adjust = true
	}
}

func (a *App) editSetFeature(id string, on bool) {
	switch id {
	case "format":
		a.edit.enabled.Format = on
	case "crop":
		a.edit.enabled.Crop = on
	case "adjust":
		a.edit.enabled.Adjust = on
	}
	a.edit.expanded[id] = on
}

func (a *App) editToggleExpanded(id string) {
	if a.edit.expanded == nil {
		a.edit.expanded = map[string]bool{}
	}
	a.edit.expanded[id] = !a.edit.expanded[id]
}

func (a *App) editClearAll() {
	st := &a.edit
	st.opts = imageedit.Options{}
	st.enabled = editFeatureSet{}
	st.fileOps = map[string]imageedit.Options{}
	st.dimW, st.dimH = 0, 0
	st.dimWText, st.dimHText = "", ""
	st.quality = ""
	st.mode = "crop"
	st.liveBox = nil
}

func (a *App) editResetFeature(id string) {
	a.editSetFeature(id, false)
	st := &a.edit
	switch id {
	case "format":
		st.opts.Format = ""
		st.opts.JPEGQuality = 0
	case "crop":
		st.opts.CropWidth, st.opts.CropHeight, st.opts.CropRatio = 0, 0, ""
		st.opts.CropLeft, st.opts.CropTop = nil, nil
		st.opts.ResizeWidth, st.opts.ResizeHeight, st.opts.ResizeMode = 0, 0, ""
		st.opts.Rotate, st.opts.FlipHorizontal, st.opts.FlipVertical = 0, false, false
		st.fileOps = map[string]imageedit.Options{}
		st.dimW, st.dimH = 0, 0
		st.dimWText, st.dimHText = "", ""
		st.mode = "crop"
		st.liveBox = nil
	case "adjust":
		st.opts.Brightness, st.opts.Contrast, st.opts.Saturation = 0, 0, 0
		st.opts.Blur, st.opts.Sharpen = 0, false
	}
}

// editApplyDims 写入目标尺寸或比例。四种修改方式互斥。
func (a *App) editApplyDims(mode string, w, h int, ratio string) {
	st := &a.edit
	a.editMarkUsed("crop")
	st.mode = mode
	st.dimW, st.dimH = w, h
	st.dimWText, st.dimHText = editDimText(w), editDimText(h)
	st.opts.CropWidth, st.opts.CropHeight, st.opts.CropRatio = 0, 0, ""
	st.opts.ResizeWidth, st.opts.ResizeHeight, st.opts.ResizeMode = 0, 0, ""
	switch mode {
	case "crop":
		if ratio != "" {
			st.opts.CropRatio = ratio
		} else {
			st.opts.CropWidth, st.opts.CropHeight = w, h
		}
	case "pad":
		st.opts.ResizeMode = "contain"
		st.opts.ResizeWidth, st.opts.ResizeHeight = w, h
	case "stretch":
		st.opts.ResizeMode = "fill"
		st.opts.ResizeWidth, st.opts.ResizeHeight = w, h
	default:
		st.opts.ResizeMode = "inside"
		st.opts.ResizeWidth, st.opts.ResizeHeight = w, h
	}
}

func (a *App) editPickRatio(ratio string) {
	if a.edit.opts.CropRatio == ratio {
		a.edit.opts.CropRatio = ""
		return
	}
	a.editApplyDims("crop", 0, 0, ratio)
}

func (a *App) editSaveMyRatio() {
	st := &a.edit
	ratio := st.opts.CropRatio
	if ratio == "" && st.dimW > 0 && st.dimH > 0 {
		ratio = fmt.Sprintf("%d:%d", st.dimW, st.dimH)
	}
	if ratio == "" {
		st.notice = "先输入尺寸或选一个比例"
		return
	}
	for _, r := range st.ratios {
		if r == ratio {
			return
		}
	}
	st.ratios = append(st.ratios, ratio)
	st.notice = "已存比例 " + ratio
	a.saveImageEditSettings()
}

func (a *App) editSaveMySize() {
	st := &a.edit
	if st.dimW <= 0 || st.dimH <= 0 {
		st.notice = "先输入宽和高"
		return
	}
	for _, s := range st.sizes {
		if s.W == st.dimW && s.H == st.dimH {
			return
		}
	}
	st.sizes = append(st.sizes, editSize{W: st.dimW, H: st.dimH})
	st.notice = fmt.Sprintf("已存尺寸 %d×%d", st.dimW, st.dimH)
	a.saveImageEditSettings()
}

func (a *App) editSaveRecipe(name string) {
	name = strings.TrimSpace(name)
	st := &a.edit
	if name == "" {
		st.recipeErr = "请输入配方名称"
		return
	}
	if len(a.editChips()) == 0 {
		st.recipeErr = "先启用至少一个功能再存配方"
		return
	}
	recipe := editRecipe{Name: name, Options: a.editEffOptions()}
	replaced := false
	for i := range st.recipes {
		if st.recipes[i].Name == name {
			st.recipes[i] = recipe
			replaced = true
			break
		}
	}
	if !replaced {
		st.recipes = append(st.recipes, recipe)
	}
	st.recipeOpen = false
	st.recipeName = ""
	st.recipeErr = ""
	st.notice = fmt.Sprintf("已存配方「%s」", name)
	a.saveImageEditSettings()
}

func (a *App) editApplyRecipe(i int) {
	st := &a.edit
	if i < 0 || i >= len(st.recipes) {
		return
	}
	st.opts = st.recipes[i].Options
	st.enabled = editEnabledFromOptions(st.opts)
	st.expanded = editDefaultExpanded(st.enabled)
	st.fileOps = map[string]imageedit.Options{}
	st.mode = editModeFromOptions(st.opts)
	if st.opts.CropWidth > 0 || st.opts.CropHeight > 0 {
		st.dimW, st.dimH = st.opts.CropWidth, st.opts.CropHeight
	} else {
		st.dimW, st.dimH = st.opts.ResizeWidth, st.opts.ResizeHeight
	}
	st.dimWText, st.dimHText = editDimText(st.dimW), editDimText(st.dimH)
	st.quality = editQualityText(st.opts.JPEGQuality)
	st.liveBox = nil
	st.notice = fmt.Sprintf("已应用配方「%s」", st.recipes[i].Name)
}

func (a *App) editDeleteRecipe(i int) {
	if i < 0 || i >= len(a.edit.recipes) {
		return
	}
	a.edit.recipes = append(a.edit.recipes[:i], a.edit.recipes[i+1:]...)
	a.saveImageEditSettings()
}

// ---- 裁切框 ----

func (a *App) editCropAspect() float64 {
	parts := strings.Split(a.edit.opts.CropRatio, ":")
	if len(parts) != 2 {
		return 0
	}
	rw, err1 := strconv.ParseFloat(parts[0], 64)
	rh, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || rw <= 0 || rh <= 0 {
		return 0
	}
	return rw / rh
}

func (a *App) editCropBoxFor(path string) (editCropBox, bool) {
	st := &a.edit
	o := a.editOptionsFor(path)
	w, h := o.CropWidth, o.CropHeight
	if w <= 0 || h <= 0 || st.srcW <= 0 || st.srcH <= 0 {
		return editCropBox{}, false
	}
	if o.CropLeft != nil && o.CropTop != nil && *o.CropLeft >= 0 && *o.CropTop >= 0 {
		return editCropBox{X: float64(*o.CropLeft), Y: float64(*o.CropTop), W: float64(w), H: float64(h)}, true
	}
	return editCropBox{
		X: math.Max(0, float64(st.srcW-w)/2),
		Y: math.Max(0, float64(st.srcH-h)/2),
		W: math.Min(float64(w), float64(st.srcW)),
		H: math.Min(float64(h), float64(st.srcH)),
	}, true
}

func (a *App) editSetCropBox(box *editCropBox) {
	st := &a.edit
	a.editMarkUsed("crop")
	path := ""
	if f := st.selectedFile(); f != nil {
		path = f.Path
	}
	if box == nil {
		if st.uniformCrop {
			st.opts.CropLeft, st.opts.CropTop = nil, nil
			st.opts.CropWidth, st.opts.CropHeight = 0, 0
			st.opts.CropRatio = ""
		} else if path != "" {
			delete(st.fileOps, path)
		}
		return
	}
	x := math.Round(math.Max(0, box.X))
	y := math.Round(math.Max(0, box.Y))
	w := math.Round(math.Max(8, box.W))
	h := math.Round(math.Max(8, box.H))
	if st.srcW > 0 && x+w > float64(st.srcW) {
		w = float64(st.srcW) - x
	}
	if st.srcH > 0 && y+h > float64(st.srcH) {
		h = float64(st.srcH) - y
	}
	o := imageedit.Options{
		CropLeft:   imageedit.Int(int(x)),
		CropTop:    imageedit.Int(int(y)),
		CropWidth:  int(w),
		CropHeight: int(h),
	}
	if st.uniformCrop {
		st.opts.CropLeft, st.opts.CropTop = o.CropLeft, o.CropTop
		st.opts.CropWidth, st.opts.CropHeight = o.CropWidth, o.CropHeight
		st.dimW, st.dimH = o.CropWidth, o.CropHeight
		st.dimWText, st.dimHText = editDimText(st.dimW), editDimText(st.dimH)
		return
	}
	if path != "" {
		st.fileOps[path] = o
	}
}

// ---- 文件 ----

// editIsImage 按扩展名判断能不能编辑。
func editIsImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".avif":
		return true
	}
	return false
}

// editExpandPaths 把文件夹换成里面的图片，不进子文件夹，跳过点开头的文件。
func editExpandPaths(paths []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		var names []string
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if editIsImage(entry.Name()) {
				names = append(names, filepath.Join(p, entry.Name()))
			}
		}
		sort.Strings(names)
		out = append(out, names...)
	}
	return out
}

func (a *App) editAddPaths(paths []string) {
	st := &a.edit
	seen := make(map[string]bool, len(st.files))
	for _, f := range st.files {
		seen[f.Path] = true
	}
	added := 0
	for _, p := range editExpandPaths(paths) {
		if !editIsImage(p) || seen[p] || len(st.files) >= editMaxFiles {
			continue
		}
		seen[p] = true
		st.files = append(st.files, &editFile{
			Path:   p,
			Name:   filepath.Base(p),
			Status: "pending",
		})
		added++
	}
	if added == 0 {
		return
	}
	if st.selected < 0 || st.selected >= len(st.files) {
		st.selected = len(st.files) - added
	}
	st.notice = ""
	a.editEnsureThumbs()
}

func (a *App) editAddFromSources() {
	var paths []string
	for _, src := range a.sources {
		paths = append(paths, src.Path)
	}
	if len(paths) == 0 {
		return
	}
	a.editAddPaths(paths)
}

func (a *App) editRemoveFile(i int) {
	st := &a.edit
	if st.running || i < 0 || i >= len(st.files) {
		return
	}
	path := st.files[i].Path
	delete(st.fileOps, path)
	delete(st.picks, path)
	st.files = append(st.files[:i], st.files[i+1:]...)
	switch {
	case len(st.files) == 0:
		st.selected = -1
	case st.selected > i:
		st.selected--
	case st.selected >= len(st.files):
		st.selected = len(st.files) - 1
	}
	if f := st.selectedFile(); f != nil && f.Path != st.origPath {
		a.editResetView()
	}
}

func (a *App) editClearFiles() {
	if a.edit.running {
		return
	}
	a.edit.files = nil
	a.edit.selected = -1
	a.edit.picks = map[string]bool{}
	a.edit.pickAnchor = 0
	a.edit.fileOps = map[string]imageedit.Options{}
	a.edit.notice = ""
	a.editResetView()
}

func (a *App) editSelect(i int) {
	st := &a.edit
	if i < 0 || i >= len(st.files) || st.selected == i {
		return
	}
	st.selected = i
	a.editResetView()
}

// editThumbClick 处理胶片条上的点击：普通点选预览，Cmd 切换勾选，
// Shift 从上一张连选。勾选只影响「保存」处理哪些图，不改变预览。
func (a *App) editThumbClick(i int, mods ui.Modifiers) {
	st := &a.edit
	if i < 0 || i >= len(st.files) {
		return
	}
	if st.picks == nil {
		st.picks = map[string]bool{}
	}
	path := st.files[i].Path
	switch {
	case mods&ui.Shift != 0 && st.pickAnchor >= 0 && st.pickAnchor < len(st.files):
		lo, hi := st.pickAnchor, i
		if lo > hi {
			lo, hi = hi, lo
		}
		for j := lo; j <= hi; j++ {
			st.picks[st.files[j].Path] = true
		}
	case mods&ui.Cmd != 0:
		if st.picks[path] {
			delete(st.picks, path)
		} else {
			st.picks[path] = true
		}
		st.pickAnchor = i
	default:
		st.pickAnchor = i
	}
	a.editSelect(i)
	a.editScrollSelected()
}

// editPickedIndices 返回勾选的图片下标，按胶片条顺序。
func (a *App) editPickedIndices() []int {
	st := &a.edit
	if len(st.picks) == 0 {
		return nil
	}
	out := make([]int, 0, len(st.picks))
	for i, f := range st.files {
		if st.picks[f.Path] {
			out = append(out, i)
		}
	}
	return out
}

// editTargetIndices 是这次「保存」要处理的图：勾过就只处理勾选的。
func (a *App) editTargetIndices() []int {
	if picked := a.editPickedIndices(); len(picked) > 0 {
		return picked
	}
	out := make([]int, len(a.edit.files))
	for i := range out {
		out[i] = i
	}
	return out
}

// editFailedIndices 是上一轮失败、可以单独重试的图。
func (a *App) editFailedIndices() []int {
	var out []int
	for i, f := range a.edit.files {
		if f.Status == "failed" {
			out = append(out, i)
		}
	}
	return out
}

// editRunCounts 数上一轮的结果，用于结果弹窗和提示行。
func (a *App) editRunCounts() (okN, failN int) {
	for _, f := range a.edit.files {
		switch f.Status {
		case "ok":
			okN++
		case "failed":
			failN++
		}
	}
	return okN, failN
}

func (a *App) editClearPicks() {
	a.edit.picks = map[string]bool{}
}

func (a *App) editResetView() {
	st := &a.edit
	st.zoom = 1
	st.panX, st.panY = 0, 0
	st.compare = false
	st.liveBox = nil
	st.cropDrag = nil
}

// ---- 预览 ----

func (a *App) editPreviewSignature(path string) string {
	o := a.editOptionsFor(path)
	return path + "\x00" + editJSON(o)
}

// editEnsurePreview 在选图或选项变化后安排一次原图和处理后预览。
// 处理后的预览延迟 180ms，连续拖滑杆时只跑最后一次。
func (a *App) editEnsurePreview() {
	st := &a.edit
	f := st.selectedFile()
	if f == nil {
		if st.previewSig != "" {
			st.previewSig = ""
			st.previewGen++
			st.previewPending = false
			st.orig, st.out = nil, nil
			st.origPath = ""
			st.srcW, st.srcH, st.outW, st.outH = 0, 0, 0, 0
			if st.previewTimer != nil {
				st.previewTimer.Stop()
				st.previewTimer = nil
			}
		}
		return
	}
	sig := a.editPreviewSignature(f.Path)
	if sig == st.previewSig {
		return
	}
	st.previewSig = sig
	st.previewGen++
	gen := st.previewGen
	st.previewPending = true
	path := f.Path
	opts := a.editOptionsFor(path)
	if st.origPath != path {
		st.origPath = path
		go a.editLoadOriginal(path)
	}
	if st.previewTimer != nil {
		st.previewTimer.Stop()
	}
	st.previewTimer = time.AfterFunc(180*time.Millisecond, func() {
		a.editRenderPreview(path, opts, gen)
	})
}

func (a *App) editLoadOriginal(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	img, err := imageedit.Load(data)
	if err != nil {
		return
	}
	a.postUI(func() {
		st := &a.edit
		if st.origPath != path {
			return
		}
		st.orig = ui.NewBitmap(img)
		st.srcW, st.srcH = img.Bounds().Dx(), img.Bounds().Dy()
	})
}

func (a *App) editRenderPreview(path string, opts imageedit.Options, gen int) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.postUI(func() {
			if a.edit.previewGen == gen {
				a.edit.previewPending = false
			}
		})
		return
	}
	out, err := imageedit.Preview(context.Background(), data, opts, 1600)
	if err != nil {
		logging.Log.Debugf("图片预览失败: %v", err)
		a.postUI(func() {
			st := &a.edit
			if st.previewGen != gen {
				return
			}
			st.out = nil
			st.previewPending = false
		})
		return
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		a.postUI(func() {
			if a.edit.previewGen == gen {
				a.edit.previewPending = false
			}
		})
		return
	}
	a.postUI(func() {
		st := &a.edit
		if st.previewGen != gen {
			return
		}
		st.out = ui.NewBitmap(img)
		st.outW, st.outH = img.Bounds().Dx(), img.Bounds().Dy()
		st.previewPending = false
	})
}

func (a *App) editEnsureThumbs() {
	st := &a.edit
	if st.thumbSem == nil {
		st.thumbSem = make(chan struct{}, 4)
	}
	for _, f := range st.files {
		if f.Thumb != nil || f.ThumbQueued {
			continue
		}
		f.ThumbQueued = true
		path := f.Path
		go func() {
			st.thumbSem <- struct{}{}
			defer func() { <-st.thumbSem }()
			data, err := os.ReadFile(path)
			if err != nil {
				return
			}
			img, err := imageedit.Thumbnail(data, 128)
			if err != nil {
				return
			}
			a.postUI(func() {
				for _, x := range a.edit.files {
					if x.Path == path && x.Thumb == nil {
						x.Thumb = ui.NewBitmap(img)
					}
				}
			})
		}()
	}
}

// editZoomBy 以舞台中心为原点、光标处为锚点缩放。
func (a *App) editZoomBy(factor, cx, cy float64) {
	st := &a.edit
	z := math.Min(4, math.Max(0.1, st.zoom*factor))
	f := z / st.zoom
	st.panX = st.panX + (cx-st.panX)*(1-f)
	st.panY = st.panY + (cy-st.panY)*(1-f)
	st.zoom = z
}

func (a *App) editClampPan() {
	st := &a.edit
	if st.baseW <= 0 || st.baseH <= 0 || st.previewW <= 0 || st.previewH <= 0 {
		return
	}
	maxX := float64(st.previewW)/2 + float64(st.baseW)*st.zoom/2 - 40
	maxY := float64(st.previewH)/2 + float64(st.baseH)*st.zoom/2 - 40
	maxX = math.Max(0, maxX)
	maxY = math.Max(0, maxY)
	st.panX = math.Min(math.Max(st.panX, -maxX), maxX)
	st.panY = math.Min(math.Max(st.panY, -maxY), maxY)
}

// editShowCropUI 是裁切框该不该出现。
func (a *App) editShowCropUI() bool {
	st := &a.edit
	return st.enabled.Crop && st.expanded["crop"] && st.mode == "crop" && st.orig != nil && st.srcW > 0
}
