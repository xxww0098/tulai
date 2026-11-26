package ui

import (
	"context"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xxww0098/tulai/internal/logging"
)

// ImageEditService exposes batch image processing, darkroom preview, crop box,
// recipe presets, and export IPCs.
type ImageEditService struct {
	app *App
}

// EditFileDTO represents one image file in the editor filmstrip.
type EditFileDTO struct {
	Index         int    `json:"index"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	FileURL       string `json:"fileUrl"`
	Status        string `json:"status"`
	Err           string `json:"err"`
	Output        string `json:"output"`
	OutURL        string `json:"outUrl"`
	Picked        bool   `json:"picked"`
	HasCustomCrop bool   `json:"hasCustomCrop"`
}

// EditChipDTO represents an active parameter summary pill.
type EditChipDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// EditSizePresetDTO represents a width×height preset.
type EditSizePresetDTO struct {
	Label string `json:"label"`
	W     int    `json:"w"`
	H     int    `json:"h"`
}

// EditRatioPresetDTO represents an aspect-ratio crop preset.
type EditRatioPresetDTO struct {
	Ratio string `json:"ratio"`
	Label string `json:"label"`
}

// EditRecipeDTO represents a saved parameter recipe.
type EditRecipeDTO struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

// EditCropBoxDTO represents a crop rectangle in source-image pixel coordinates.
type EditCropBoxDTO struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// ImageEditStateDTO represents the complete image editor snapshot.
type ImageEditStateDTO struct {
	Files           []EditFileDTO        `json:"files"`
	Selected        int                  `json:"selected"`
	PickedCount     int                  `json:"pickedCount"`
	CustomCropCount int                  `json:"customCropCount"`
	HasSelection    bool                 `json:"hasSelection"`
	SelectedName    string               `json:"selectedName"`
	SrcW            int                  `json:"srcW"`
	SrcH            int                  `json:"srcH"`
	OutW            int                  `json:"outW"`
	OutH            int                  `json:"outH"`
	OrigVersion     uint64               `json:"origVersion"`
	OutVersion      uint64               `json:"outVersion"`
	OrigURL         string               `json:"origUrl"`
	PreviewURL      string               `json:"previewUrl"`
	ShowCropUI      bool                 `json:"showCropUi"`
	CropAspect      float64              `json:"cropAspect"`
	CropBox         *EditCropBoxDTO      `json:"cropBox,omitempty"`
	PreviewPending  bool                 `json:"previewPending"`
	Zoom            float64              `json:"zoom"`
	Compare         bool                 `json:"compare"`
	Split           float64              `json:"split"`
	FormatEnabled   bool                 `json:"formatEnabled"`
	CropEnabled     bool                 `json:"cropEnabled"`
	AdjustEnabled   bool                 `json:"adjustEnabled"`
	FormatOpen      bool                 `json:"formatOpen"`
	CropOpen        bool                 `json:"cropOpen"`
	AdjustOpen      bool                 `json:"adjustOpen"`
	Format          string               `json:"format"`
	Quality         string               `json:"quality"`
	UniformCrop     bool                 `json:"uniformCrop"`
	Mode            string               `json:"mode"`
	DimTab          int                  `json:"dimTab"`
	RatioTab        int                  `json:"ratioTab"`
	DimW            string               `json:"dimW"`
	DimH            string               `json:"dimH"`
	CropRatio       string               `json:"cropRatio"`
	Rotate          int                  `json:"rotate"`
	FlipH           bool                 `json:"flipH"`
	FlipV           bool                 `json:"flipV"`
	Brightness      int                  `json:"brightness"`
	Contrast        int                  `json:"contrast"`
	Saturation      int                  `json:"saturation"`
	Blur            float64              `json:"blur"`
	Sharpen         bool                 `json:"sharpen"`
	Grayscale       bool                 `json:"grayscale"`
	OutDir          string               `json:"outDir"`
	Suffix          string               `json:"suffix"`
	Notice          string               `json:"notice"`
	Running         bool                 `json:"running"`
	ProgressDone    int                  `json:"progressDone"`
	ProgressTotal   int                  `json:"progressTotal"`
	ProgressCurrent string               `json:"progressCurrent"`
	OkCount         int                  `json:"okCount"`
	FailCount       int                  `json:"failCount"`
	ResultOpen      bool                 `json:"resultOpen"`
	RecipeOpen      bool                 `json:"recipeOpen"`
	RecipeErr       string               `json:"recipeErr"`
	Chips           []EditChipDTO        `json:"chips"`
	SummaryFormat   string               `json:"summaryFormat"`
	SummaryCrop     string               `json:"summaryCrop"`
	SummaryAdjust   string               `json:"summaryAdjust"`
	SizePresets     []EditSizePresetDTO  `json:"sizePresets"`
	RatioPresets    []EditRatioPresetDTO `json:"ratioPresets"`
	MyRatios        []string             `json:"myRatios"`
	MySizes         []EditSizePresetDTO  `json:"mySizes"`
	Recipes         []EditRecipeDTO      `json:"recipes"`
}

func emptyImageEditState() ImageEditStateDTO {
	return ImageEditStateDTO{
		Selected:     -1,
		Zoom:         1,
		Split:        50,
		Files:        []EditFileDTO{},
		Chips:        []EditChipDTO{},
		SizePresets:  []EditSizePresetDTO{},
		RatioPresets: []EditRatioPresetDTO{},
		MyRatios:     []string{},
		MySizes:      []EditSizePresetDTO{},
		Recipes:      []EditRecipeDTO{},
	}
}

// GetImageEditState returns the current image editor filmstrip, stage preview URLs, crop box, and parameters.
func (s *ImageEditService) GetImageEditState(ctx context.Context) ImageEditStateDTO {
	if s.app == nil {
		return emptyImageEditState()
	}
	out, err := callValue(s.app, ctx, func() ImageEditStateDTO {
		a := s.app
		a.editEnsureMaps()
		st := &a.edit

		customCropCount := 0
		files := make([]EditFileDTO, 0, len(st.files))
		for i, f := range st.files {
			hasCustom := a.editFileHasCustomCrop(f.Path)
			if hasCustom {
				customCropCount++
			}
			files = append(files, EditFileDTO{
				Index:         i,
				Path:          f.Path,
				Name:          f.Name,
				FileURL:       a.mediaURL(f.Path),
				Status:        f.Status,
				Err:           f.Err,
				Output:        f.Output,
				OutURL:        a.mediaURL(f.Output),
				Picked:        st.picks[f.Path],
				HasCustomCrop: hasCustom,
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
		for _, sz := range st.sizes {
			mySizes = append(mySizes, EditSizePresetDTO{W: sz.W, H: sz.H})
		}
		recipes := make([]EditRecipeDTO, 0, len(st.recipes))
		for i, r := range st.recipes {
			recipes = append(recipes, EditRecipeDTO{Index: i, Name: r.Name})
		}
		myRatios := make([]string, len(st.ratios))
		copy(myRatios, st.ratios)

		okN, failN := a.editRunCounts()
		sel := st.selectedFile()
		selName := ""
		var cropBox *EditCropBoxDTO
		if sel != nil {
			selName = sel.Name
			if box, ok := a.editCropBoxFor(sel.Path); ok {
				cropBox = &EditCropBoxDTO{X: box.X, Y: box.Y, W: box.W, H: box.H}
			}
		}
		eff := a.editEffOptions()
		media := a.ensureMedia()
		origURL := ""
		previewURL := ""
		if sel != nil {
			origURL = media.EditOrigURL(st.origVersion)
			if origURL == "" {
				origURL = a.mediaURL(sel.Path)
			}
			previewURL = media.EditPreviewURL(st.outVersion)
		}
		return ImageEditStateDTO{
			Files:           files,
			Selected:        st.selected,
			PickedCount:     len(a.editPickedIndices()),
			CustomCropCount: customCropCount,
			HasSelection:    sel != nil,
			SelectedName:    selName,
			SrcW:            st.srcW,
			SrcH:            st.srcH,
			OutW:            st.outW,
			OutH:            st.outH,
			OrigVersion:     st.origVersion,
			OutVersion:      st.outVersion,
			OrigURL:         origURL,
			PreviewURL:      previewURL,
			ShowCropUI:      a.editShowCropUI(),
			CropAspect:      a.editCropAspect(),
			CropBox:         cropBox,
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
			MyRatios:        myRatios,
			MySizes:         mySizes,
			Recipes:         recipes,
		}
	})
	if err != nil {
		return emptyImageEditState()
	}
	return out
}

// EditPickFiles opens a multi-file picker and adds selected images to the editor filmstrip.
func (s *ImageEditService) EditPickFiles(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	paths, err := s.app.pickFilesAsync(ctx, "打开图片", true, []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "avif"}, "图片")
	if err != nil {
		logging.Log.Errorf("打开图片失败: %v", err)
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return s.app.call(ctx, func() {
		s.app.editAddPaths(paths)
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditPickFolder opens a folder picker and adds images from the chosen directory.
func (s *ImageEditService) EditPickFolder(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	paths, err := s.app.pickDirsAsync(ctx, "选择图片文件夹", false)
	if err != nil {
		logging.Log.Errorf("选择图片文件夹失败: %v", err)
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return s.app.call(ctx, func() {
		s.app.editAddPaths(paths)
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditAddPaths adds dropped or supplied file/folder paths to the editor filmstrip.
func (s *ImageEditService) EditAddPaths(ctx context.Context, paths []string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editAddPaths(paths)
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditOpenPath adds a single image path to the editor filmstrip (if absent) and selects it.
func (s *ImageEditService) EditOpenPath(ctx context.Context, path string) {
	if s.app == nil {
		return
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editAddPaths([]string{path})
		clean := filepath.Clean(path)
		for i, f := range s.app.edit.files {
			if filepath.Clean(f.Path) == clean {
				s.app.editSelect(i)
				break
			}
		}
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditAddFromSources imports the workbench's current source images into the editor.
func (s *ImageEditService) EditAddFromSources(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editAddFromSources()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditSelectIndex selects an image in the filmstrip and refreshes its preview.
func (s *ImageEditService) EditSelectIndex(ctx context.Context, idx int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editSelect(idx)
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditTogglePick toggles multi-selection pick state on a filmstrip item.
func (s *ImageEditService) EditTogglePick(ctx context.Context, idx int, shift, cmd bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editThumbToggle(idx, shift, cmd)
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditClearPicks clears all multi-select picks in the filmstrip.
func (s *ImageEditService) EditClearPicks(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editClearPicks()
		s.app.emit("imageedit")
	})
}

// EditSelectAllPicks marks all files in the editor filmstrip as picked.
func (s *ImageEditService) EditSelectAllPicks(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editSelectAllPicks()
		s.app.emit("imageedit")
	})
}

// EditInvertPicks toggles pick state for every file in the editor filmstrip.
func (s *ImageEditService) EditInvertPicks(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editInvertPicks()
		s.app.emit("imageedit")
	})
}

// EditPickByStatus replaces picks with files matching status ("pending", "ok", "failed", or "customCrop").
func (s *ImageEditService) EditPickByStatus(ctx context.Context, status string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editPickByStatus(status)
		s.app.emit("imageedit")
	})
}

// EditRemovePicked removes all picked files from the editor filmstrip when not running.
func (s *ImageEditService) EditRemovePicked(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editRemovePicked()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditRemoveIndex removes an image from the filmstrip by index.
func (s *ImageEditService) EditRemoveIndex(ctx context.Context, idx int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editRemoveFile(idx)
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditClearFiles clears all images from the editor filmstrip.
func (s *ImageEditService) EditClearFiles(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editClearFiles()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditPickOutDir opens a folder picker for the batch export output directory.
func (s *ImageEditService) EditPickOutDir(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	paths, err := s.app.pickDirsAsync(ctx, "选择输出目录", false)
	if err != nil {
		logging.Log.Errorf("选择输出目录失败: %v", err)
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	return s.app.call(ctx, func() {
		s.app.edit.outDir = paths[0]
		s.app.saveImageEditSettings()
		s.app.emit("imageedit")
	})
}

// EditOpenOutDir opens the image editor output directory in the OS file manager.
func (s *ImageEditService) EditOpenOutDir(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	dir, err := callValue(s.app, ctx, func() string {
		return strings.TrimSpace(s.app.edit.outDir)
	})
	if err != nil || dir == "" {
		return err
	}
	return s.app.openPath(dir)
}

// EditSetField updates an image editor parameter and schedules preview re-rendering.
func (s *ImageEditService) EditSetField(ctx context.Context, field, value string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
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
			st.opts.Rotate = ((st.opts.Rotate-90)%360 + 360) % 360
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
	})
}

// EditSetCropBox commits an interactive crop rectangle (in source-image pixel coordinates).
func (s *ImageEditService) EditSetCropBox(ctx context.Context, x, y, w, h float64) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if w < 8 || h < 8 {
			return
		}
		box := editCropBox{X: x, Y: y, W: w, H: h}
		s.app.editSetCropBox(&box)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditClearCropBox removes the active crop rectangle for the current image or uniform crop.
func (s *ImageEditService) EditClearCropBox(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editSetCropBox(nil)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditSyncCropTo copies the active selected image's crop box to picked or all files as independent per-file overrides.
func (s *ImageEditService) EditSyncCropTo(ctx context.Context, scope string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if s.app.editSyncCropTo(scope) {
			s.app.saveImageEditSettings()
			s.app.editEnsurePreview()
		}
		s.app.emit("imageedit")
	})
}

// EditClearCustomCrops clears per-file crop overrides for "selected", "picked", or "all" files.
func (s *ImageEditService) EditClearCustomCrops(ctx context.Context, scope string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editClearCustomCrops(scope)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditSetFeature enables or disables one parameter section (format, crop, adjust).
func (s *ImageEditService) EditSetFeature(ctx context.Context, id string, on bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editSetFeature(id, on)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditToggleExpanded expands or collapses one parameter accordion card.
func (s *ImageEditService) EditToggleExpanded(ctx context.Context, id string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editToggleExpanded(id)
		s.app.saveImageEditSettings()
		s.app.emit("imageedit")
	})
}

// EditApplyDims applies a resize/crop mode, dimensions, and optional ratio.
func (s *ImageEditService) EditApplyDims(ctx context.Context, mode string, w, h int, ratio string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editApplyDims(mode, w, h, ratio)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditPickRatio toggles or applies an aspect-ratio preset.
func (s *ImageEditService) EditPickRatio(ctx context.Context, ratio string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editPickRatio(ratio)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditSaveMyRatio saves the active ratio to the user's custom ratios list.
func (s *ImageEditService) EditSaveMyRatio(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editSaveMyRatio()
		s.app.emit("imageedit")
	})
}

// EditSaveMySize saves the current width×height to the user's custom sizes list.
func (s *ImageEditService) EditSaveMySize(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editSaveMySize()
		s.app.emit("imageedit")
	})
}

// EditSaveRecipe saves the active parameter set as a named recipe.
func (s *ImageEditService) EditSaveRecipe(ctx context.Context, name string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editSaveRecipe(name)
		s.app.emit("imageedit")
	})
}

// EditApplyRecipe applies a saved recipe by index.
func (s *ImageEditService) EditApplyRecipe(ctx context.Context, idx int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editApplyRecipe(idx)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditDeleteRecipe removes a saved recipe by index.
func (s *ImageEditService) EditDeleteRecipe(ctx context.Context, idx int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editDeleteRecipe(idx)
		s.app.emit("imageedit")
	})
}

// EditClearAll resets all image editor parameters.
func (s *ImageEditService) EditClearAll(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editClearAll()
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditResetFeature resets one parameter section (format, crop, adjust).
func (s *ImageEditService) EditResetFeature(ctx context.Context, id string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editResetFeature(id)
		s.app.saveImageEditSettings()
		s.app.editEnsurePreview()
		s.app.emit("imageedit")
	})
}

// EditZoomStep multiplies the stage zoom by factor (clamped to 0.1..4.0).
func (s *ImageEditService) EditZoomStep(ctx context.Context, factor float64) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editZoomBy(factor, 0, 0)
		s.app.editClampPan()
		s.app.emit("imageedit")
	})
}

// EditResetView resets stage zoom to 100%, centers pan, and disables compare mode.
func (s *ImageEditService) EditResetView(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editResetView()
		s.app.emit("imageedit")
	})
}

// EditRunBatch starts batch exporting all (or multi-selected) images.
func (s *ImageEditService) EditRunBatch(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editRunAll()
		s.app.emit("imageedit")
	})
}

// EditRetryFailed re-runs only images that failed in the previous batch export.
func (s *ImageEditService) EditRetryFailed(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editRetryFailed()
		s.app.emit("imageedit")
	})
}

// EditCancelBatch stops an in-progress batch export before the next queued image starts.
func (s *ImageEditService) EditCancelBatch(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.editRequestCancel()
		s.app.emit("imageedit")
	})
}
