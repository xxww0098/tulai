package ui

import (
	"context"
	"math"
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/tsx"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getImageEditState":   getImageEditStateBridge,
		"editPickFiles":       editPickFilesBridge,
		"editPickFolder":      editPickFolderBridge,
		"editAddPaths":        editAddPathsBridge,
		"editAddFromSources":  editAddFromSourcesBridge,
		"editSelectIndex":     editSelectIndexBridge,
		"editTogglePick":      editTogglePickBridge,
		"editClearPicks":      editClearPicksBridge,
		"editRemoveIndex":     editRemoveIndexBridge,
		"editClearFiles":      editClearFilesBridge,
		"editPickOutDir":      editPickOutDirBridge,
		"editOpenOutDir":      editOpenOutDirBridge,
		"editSetField":        editSetFieldBridge,
		"editSetFeature":      editSetFeatureBridge,
		"editToggleExpanded":  editToggleExpandedBridge,
		"editApplyDims":       editApplyDimsBridge,
		"editPickRatio":       editPickRatioBridge,
		"editSaveMyRatio":     editSaveMyRatioBridge,
		"editSaveMySize":      editSaveMySizeBridge,
		"editSaveRecipe":      editSaveRecipeBridge,
		"editApplyRecipe":     editApplyRecipeBridge,
		"editDeleteRecipe":    editDeleteRecipeBridge,
		"editClearAll":        editClearAllBridge,
		"editResetFeature":    editResetFeatureBridge,
		"editZoomStep":        editZoomStepBridge,
		"editResetView":       editResetViewBridge,
		"editRunBatch":        editRunBatchBridge,
		"editRetryFailed":     editRetryFailedBridge,
		"editCancelBatch":     editCancelBatchBridge,
	})
}

type EditFileDTO struct {
	Index   int
	Path    string
	Name    string
	FileURL string
	Status  string
	Err     string
	Output  string
	OutURL  string
	Picked  bool
}

type EditChipDTO struct {
	ID    string
	Label string
}

type EditSizePresetDTO struct {
	Label string
	W     int
	H     int
}

type EditRatioPresetDTO struct {
	Ratio string
	Label string
}

type EditRecipeDTO struct {
	Index int
	Name  string
}

type ImageEditStateDTO struct {
	Files           []EditFileDTO
	Selected        int
	PickedCount     int
	HasSelection    bool
	SelectedName    string
	SrcW            int
	SrcH            int
	OutW            int
	OutH            int
	PreviewPending  bool
	Zoom            float64
	Compare         bool
	Split           float64
	FormatEnabled   bool
	CropEnabled     bool
	AdjustEnabled   bool
	FormatOpen      bool
	CropOpen        bool
	AdjustOpen      bool
	Format          string
	Quality         string
	UniformCrop     bool
	Mode            string
	DimTab          int
	RatioTab        int
	DimW            string
	DimH            string
	CropRatio       string
	Rotate          int
	FlipH           bool
	FlipV           bool
	Brightness      int
	Contrast        int
	Saturation      int
	Blur            float64
	Sharpen         bool
	Grayscale       bool
	OutDir          string
	Suffix          string
	Notice          string
	Running         bool
	ProgressDone    int
	ProgressTotal   int
	ProgressCurrent string
	OkCount         int
	FailCount       int
	ResultOpen      bool
	RecipeOpen      bool
	RecipeErr       string
	Chips           []EditChipDTO
	SummaryFormat   string
	SummaryCrop     string
	SummaryAdjust   string
	SizePresets     []EditSizePresetDTO
	RatioPresets    []EditRatioPresetDTO
	MyRatios        []string
	MySizes         []EditSizePresetDTO
	Recipes         []EditRecipeDTO
}

func getImageEditStateBridge() ImageEditStateDTO {
	a := getApp()
	if a == nil {
		return ImageEditStateDTO{Zoom: 1, Split: 50}
	}
	a.editEnsureMaps()
	st := &a.edit

	files := make([]EditFileDTO, 0, len(st.files))
	for i, f := range st.files {
		files = append(files, EditFileDTO{
			Index:   i,
			Path:    f.Path,
			Name:    f.Name,
			FileURL: fileURL(f.Path),
			Status:  f.Status,
			Err:     f.Err,
			Output:  f.Output,
			OutURL:  fileURL(f.Output),
			Picked:  st.picks[f.Path],
		})
	}
	chips := make([]EditChipDTO, 0)
	for _, c := range a.editChips() {
		chips = append(chips, EditChipDTO{ID: c.ID, Label: c.Label})
	}
	sizePresets := make([]EditSizePresetDTO, 0, len(editSizePresets))
	for _, p := range editSizePresets {
		sizePresets = append(sizePresets, EditSizePresetDTO{Label: p.Label, W: p.W, H: p.H})
	}
	ratioPresets := make([]EditRatioPresetDTO, 0, len(editRatioPresets))
	for _, p := range editRatioPresets {
		ratioPresets = append(ratioPresets, EditRatioPresetDTO{Ratio: p.Ratio, Label: p.Label})
	}
	mySizes := make([]EditSizePresetDTO, 0, len(st.sizes))
	for _, s := range st.sizes {
		mySizes = append(mySizes, EditSizePresetDTO{W: s.W, H: s.H})
	}
	recipes := make([]EditRecipeDTO, 0, len(st.recipes))
	for i, r := range st.recipes {
		recipes = append(recipes, EditRecipeDTO{Index: i, Name: r.Name})
	}
	okN, failN := a.editRunCounts()
	sel := st.selectedFile()
	selName := ""
	if sel != nil {
		selName = sel.Name
	}
	eff := a.editEffOptions()
	return ImageEditStateDTO{
		Files:           files,
		Selected:        st.selected,
		PickedCount:     len(a.editPickedIndices()),
		HasSelection:    sel != nil,
		SelectedName:    selName,
		SrcW:            st.srcW,
		SrcH:            st.srcH,
		OutW:            st.outW,
		OutH:            st.outH,
		PreviewPending:  st.previewPending,
		Zoom:            st.zoom,
		Compare:         st.compare,
		Split:           st.split,
		FormatEnabled:   st.enabled.Format,
		CropEnabled:     st.enabled.Crop,
		AdjustEnabled:   st.enabled.Adjust,
		FormatOpen:      st.expanded["format"],
		CropOpen:        st.expanded["crop"],
		AdjustOpen:      st.expanded["adjust"],
		Format:          st.opts.Format,
		Quality:         st.quality,
		UniformCrop:     st.uniformCrop,
		Mode:            st.mode,
		DimTab:          st.dimTab,
		RatioTab:        st.ratioTab,
		DimW:            st.dimWText,
		DimH:            st.dimHText,
		CropRatio:       st.opts.CropRatio,
		Rotate:          st.opts.Rotate,
		FlipH:           st.opts.FlipHorizontal,
		FlipV:           st.opts.FlipVertical,
		Brightness:      st.opts.Brightness,
		Contrast:        st.opts.Contrast,
		Saturation:      st.opts.Saturation,
		Blur:            st.opts.Blur,
		Sharpen:         st.opts.Sharpen,
		Grayscale:       st.opts.Grayscale,
		OutDir:          st.outDir,
		Suffix:          st.suffix,
		Notice:          st.notice,
		Running:         st.running,
		ProgressDone:    st.progressDone,
		ProgressTotal:   st.progressTotal,
		ProgressCurrent: st.progressCurrent,
		OkCount:         okN,
		FailCount:       failN,
		ResultOpen:      st.resultOpen,
		RecipeOpen:      st.recipeOpen,
		RecipeErr:       st.recipeErr,
		Chips:           chips,
		SummaryFormat:   editSummary("format", eff, "转格式 / 压缩品质"),
		SummaryCrop:     editSummary("crop", eff, "裁剪 / 改尺寸 / 旋转"),
		SummaryAdjust:   editSummary("adjust", eff, "亮度 / 对比 / 饱和 / 锐化"),
		SizePresets:     sizePresets,
		RatioPresets:    ratioPresets,
		MyRatios:        append([]string(nil), st.ratios...),
		MySizes:         mySizes,
		Recipes:         recipes,
	}
}

func editPickFilesBridge() {
	if a := getApp(); a != nil {
		a.editPickFiles()
	}
}

func editPickFolderBridge() {
	if a := getApp(); a != nil {
		a.editPickFolder()
	}
}

func editAddPathsBridge(paths []string) {
	if a := getApp(); a != nil {
		a.editAddPaths(paths)
		a.emit("imageedit")
	}
}

func editAddFromSourcesBridge() {
	if a := getApp(); a != nil {
		a.editAddFromSources()
		a.emit("imageedit")
	}
}

func editSelectIndexBridge(idx int) {
	if a := getApp(); a != nil {
		a.editSelect(idx)
		a.emit("imageedit")
	}
}

func editTogglePickBridge(idx int, shift, cmd bool) {
	if a := getApp(); a != nil {
		a.editThumbToggle(idx, shift, cmd)
		a.emit("imageedit")
	}
}

func editClearPicksBridge() {
	if a := getApp(); a != nil {
		a.editClearPicks()
		a.emit("imageedit")
	}
}

func editRemoveIndexBridge(idx int) {
	if a := getApp(); a != nil {
		a.editRemoveFile(idx)
		a.emit("imageedit")
	}
}

func editClearFilesBridge() {
	if a := getApp(); a != nil {
		a.editClearFiles()
		a.emit("imageedit")
	}
}

func editPickOutDirBridge() {
	if a := getApp(); a != nil {
		a.editPickOutDir(nil)
	}
}

func editOpenOutDirBridge() {
	if a := getApp(); a != nil {
		a.editOpenOutDir()
	}
}

func editSetFieldBridge(field, value string) {
	a := getApp()
	if a == nil {
		return
	}
	a.editEnsureMaps()
	st := &a.edit
	on := value == "true" || value == "1"
	switch field {
	case "format":
		a.editMarkUsed("format")
		st.opts.Format = value
	case "quality":
		a.editMarkUsed("format")
		st.quality = value
		if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && v >= 1 && v <= 100 {
			st.opts.JPEGQuality = v
		} else {
			st.opts.JPEGQuality = 0
		}
	case "uniformCrop":
		st.uniformCrop = on
	case "mode":
		a.editMarkUsed("crop")
		a.editApplyDims(value, st.dimW, st.dimH, st.opts.CropRatio)
	case "dimTab":
		st.dimTab = clampInt(parseIntDefault(value, 0), 0, 1)
	case "ratioTab":
		st.ratioTab = clampInt(parseIntDefault(value, 0), 0, 1)
	case "dimW":
		st.dimWText = value
		if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && v >= 0 {
			st.dimW = v
			a.editApplyDims(st.mode, st.dimW, st.dimH, "")
		}
	case "dimH":
		st.dimHText = value
		if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && v >= 0 {
			st.dimH = v
			a.editApplyDims(st.mode, st.dimW, st.dimH, "")
		}
	case "rotateLeft":
		a.editMarkUsed("crop")
		st.opts.Rotate = ((st.opts.Rotate - 90) % 360 + 360) % 360
	case "rotateRight":
		a.editMarkUsed("crop")
		st.opts.Rotate = (st.opts.Rotate + 90) % 360
	case "flipH":
		a.editMarkUsed("crop")
		st.opts.FlipHorizontal = !st.opts.FlipHorizontal
	case "flipV":
		a.editMarkUsed("crop")
		st.opts.FlipVertical = !st.opts.FlipVertical
	case "brightness":
		a.editMarkUsed("adjust")
		if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			st.opts.Brightness = clampInt(v, -100, 100)
		}
	case "contrast":
		a.editMarkUsed("adjust")
		if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			st.opts.Contrast = clampInt(v, -100, 100)
		}
	case "saturation":
		a.editMarkUsed("adjust")
		if v, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			st.opts.Saturation = clampInt(v, -100, 100)
		}
	case "blur":
		a.editMarkUsed("adjust")
		if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			st.opts.Blur = math.Max(0, math.Min(20, v))
		}
	case "sharpen":
		a.editMarkUsed("adjust")
		st.opts.Sharpen = on
	case "grayscale":
		st.opts.Grayscale = on
	case "compare":
		st.compare = on
	case "split":
		if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			st.split = math.Max(5, math.Min(95, v))
		}
	case "outDir":
		st.outDir = strings.TrimSpace(value)
	case "suffix":
		st.suffix = value
	case "resultOpen":
		st.resultOpen = on
	case "recipeOpen":
		st.recipeOpen = on
		st.recipeErr = ""
	}
	a.saveImageEditSettings()
	a.editEnsurePreview()
	a.emit("imageedit")
}

func editSetFeatureBridge(id string, on bool) {
	if a := getApp(); a != nil {
		a.editSetFeature(id, on)
		a.saveImageEditSettings()
		a.editEnsurePreview()
		a.emit("imageedit")
	}
}

func editToggleExpandedBridge(id string) {
	if a := getApp(); a != nil {
		a.editToggleExpanded(id)
		a.saveImageEditSettings()
		a.emit("imageedit")
	}
}

func editApplyDimsBridge(mode string, w, h int, ratio string) {
	if a := getApp(); a != nil {
		a.editApplyDims(mode, w, h, ratio)
		a.saveImageEditSettings()
		a.editEnsurePreview()
		a.emit("imageedit")
	}
}

func editPickRatioBridge(ratio string) {
	if a := getApp(); a != nil {
		a.editPickRatio(ratio)
		a.saveImageEditSettings()
		a.editEnsurePreview()
		a.emit("imageedit")
	}
}

func editSaveMyRatioBridge() {
	if a := getApp(); a != nil {
		a.editSaveMyRatio()
		a.emit("imageedit")
	}
}

func editSaveMySizeBridge() {
	if a := getApp(); a != nil {
		a.editSaveMySize()
		a.emit("imageedit")
	}
}

func editSaveRecipeBridge(name string) {
	if a := getApp(); a != nil {
		a.editSaveRecipe(name)
		a.emit("imageedit")
	}
}

func editApplyRecipeBridge(idx int) {
	if a := getApp(); a != nil {
		a.editApplyRecipe(idx)
		a.saveImageEditSettings()
		a.editEnsurePreview()
		a.emit("imageedit")
	}
}

func editDeleteRecipeBridge(idx int) {
	if a := getApp(); a != nil {
		a.editDeleteRecipe(idx)
		a.emit("imageedit")
	}
}

func editClearAllBridge() {
	if a := getApp(); a != nil {
		a.editClearAll()
		a.saveImageEditSettings()
		a.editEnsurePreview()
		a.emit("imageedit")
	}
}

func editResetFeatureBridge(id string) {
	if a := getApp(); a != nil {
		a.editResetFeature(id)
		a.saveImageEditSettings()
		a.editEnsurePreview()
		a.emit("imageedit")
	}
}

func editZoomStepBridge(factor float64) {
	if a := getApp(); a != nil {
		a.editZoomBy(factor, 0, 0)
		a.editClampPan()
		a.emit("imageedit")
	}
}

func editResetViewBridge() {
	if a := getApp(); a != nil {
		a.editResetView()
		a.emit("imageedit")
	}
}

func editRunBatchBridge() {
	if a := getApp(); a != nil {
		a.editRunAll()
		a.emit("imageedit")
	}
}

func editRetryFailedBridge() {
	if a := getApp(); a != nil {
		a.editRetryFailed()
		a.emit("imageedit")
	}
}

func editCancelBatchBridge() {
	if a := getApp(); a != nil {
		a.editRequestCancel()
		a.emit("imageedit")
	}
}

var _ = context.Background
