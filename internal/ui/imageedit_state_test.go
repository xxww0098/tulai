package ui

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xxww0098/tulai/internal/imageedit"
)

func editTestPath(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录 %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("图片"), 0o644); err != nil {
		t.Fatalf("写 %s: %v", path, err)
	}
	return path
}

func editTestImage(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("编码 %s: %v", path, err)
	}
}

func TestImageEditAddPathsExpandsAndDedupes(t *testing.T) {
	dir := t.TempDir()
	a := editTestPath(t, dir, "a.png")
	b := editTestPath(t, dir, "b.JPG")
	note := editTestPath(t, dir, "说明.txt")
	sub := filepath.Join(dir, "子目录")
	c := editTestPath(t, sub, "c.png")
	d := editTestPath(t, sub, "d.webp")

	app := &App{}
	app.editAddPaths([]string{a, a, b, note, sub})
	got := make([]string, 0, len(app.edit.files))
	for _, f := range app.edit.files {
		got = append(got, f.Path)
	}
	want := []string{a, b, c, d}
	if len(got) != len(want) {
		t.Fatalf("加入的图片不对: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 张是 %q，想要 %q", i, got[i], want[i])
		}
	}
	if app.edit.selected != 0 {
		t.Fatalf("选中项应为第一张，实际 %d", app.edit.selected)
	}

	// 再次加入只补新图，选中项不动。
	e := editTestPath(t, dir, "e.bmp")
	app.editSelect(2)
	app.editAddPaths([]string{e})
	if len(app.edit.files) != 5 || app.edit.files[4].Path != e {
		t.Fatalf("追加入图不对: %d 张", len(app.edit.files))
	}
	if app.edit.selected != 2 {
		t.Fatalf("追加后选中项被挪到 %d", app.edit.selected)
	}
}

func TestImageEditOptionsForMergesPerFileCrop(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()
	app.edit.enabled = editFeatureSet{Crop: true}
	app.edit.opts.CropRatio = "16:9"
	app.edit.opts.Rotate = 90
	app.edit.uniformCrop = false
	app.edit.fileOps["/图/a.png"] = imageedit.Options{
		CropLeft:   imageedit.Int(10),
		CropTop:    imageedit.Int(20),
		CropWidth:  100,
		CropHeight: 50,
	}

	o := app.editOptionsFor("/图/a.png")
	if o.CropWidth != 100 || o.CropHeight != 50 {
		t.Fatalf("单图裁剪框没生效: %dx%d", o.CropWidth, o.CropHeight)
	}
	if o.CropLeft == nil || *o.CropLeft != 10 || o.CropTop == nil || *o.CropTop != 20 {
		t.Fatalf("单图裁剪位置没生效: %v %v", o.CropLeft, o.CropTop)
	}
	if o.CropRatio != "16:9" || o.Rotate != 90 {
		t.Fatalf("公共选项被单图覆盖盖掉了: ratio=%q rotate=%d", o.CropRatio, o.Rotate)
	}
	if other := app.editOptionsFor("/图/b.png"); other.CropWidth != 0 {
		t.Fatalf("别的图不该拿到这张的裁剪框: %dx%d", other.CropWidth, other.CropHeight)
	}

	// 打开「裁剪同一位置」后忽略单图覆盖。
	app.edit.uniformCrop = true
	if o := app.editOptionsFor("/图/a.png"); o.CropWidth != 0 {
		t.Fatalf("同一位置模式仍用了单图框: %dx%d", o.CropWidth, o.CropHeight)
	}

	// 关掉裁剪卡，单图框也不该出力。
	app.edit.enabled = editFeatureSet{}
	if o := app.editOptionsFor("/图/a.png"); o.CropWidth != 0 {
		t.Fatalf("裁剪卡关了还带裁剪框: %dx%d", o.CropWidth, o.CropHeight)
	}
}

func TestImageEditCropBoxClampsAndRoutes(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()
	app.edit.srcW, app.edit.srcH = 1000, 500
	app.edit.files = []*editFile{{Path: "/图/a.png"}}
	app.edit.selected = 0
	app.edit.uniformCrop = true

	app.editSetCropBox(&editCropBox{X: -5, Y: 480, W: 300, H: 100})
	if app.edit.opts.CropLeft == nil || *app.edit.opts.CropLeft != 0 {
		t.Fatalf("左边越界没夹住: %v", app.edit.opts.CropLeft)
	}
	if got := app.edit.opts.CropWidth; got != 300 {
		t.Fatalf("宽度应为 300，实际 %d", got)
	}
	if got := app.edit.opts.CropHeight; got != 20 {
		t.Fatalf("底边越界应裁到 20，实际 %d", got)
	}
	if app.edit.dimW != 300 || app.edit.dimH != 20 {
		t.Fatalf("尺寸输入没同步: %dx%d", app.edit.dimW, app.edit.dimH)
	}

	app.edit.uniformCrop = false
	app.editSetCropBox(&editCropBox{X: 10, Y: 20, W: 100, H: 50})
	if app.edit.opts.CropWidth != 300 || app.edit.opts.CropHeight != 20 {
		t.Fatalf("单图模式改动了公共裁剪: %dx%d", app.edit.opts.CropWidth, app.edit.opts.CropHeight)
	}
	ov, ok := app.edit.fileOps["/图/a.png"]
	if !ok || ov.CropWidth != 100 || ov.CropHeight != 50 {
		t.Fatalf("单图裁剪没存下来: %+v", ov)
	}

	app.editSetCropBox(nil)
	if _, ok := app.edit.fileOps["/图/a.png"]; ok {
		t.Fatal("清空单图裁剪后还留着覆盖")
	}
}

func TestImageEditRatioAndModes(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()
	app.editApplyDims("crop", 0, 0, "16:9")
	if app.edit.opts.CropRatio != "16:9" || app.edit.mode != "crop" || !app.edit.enabled.Crop {
		t.Fatalf("选比例没写进裁剪: %+v", app.edit.opts)
	}
	app.editPickRatio("16:9")
	if app.edit.opts.CropRatio != "" {
		t.Fatalf("再点一次应解除比例，实际 %q", app.edit.opts.CropRatio)
	}

	app.editApplyDims("fit", 800, 600, "")
	if app.edit.opts.ResizeMode != "inside" || app.edit.opts.ResizeWidth != 800 || app.edit.opts.ResizeHeight != 600 {
		t.Fatalf("等比缩放选项不对: %+v", app.edit.opts)
	}
	app.editApplyDims("pad", 640, 480, "")
	if app.edit.opts.ResizeMode != "contain" {
		t.Fatalf("纯色背景应是 contain，实际 %q", app.edit.opts.ResizeMode)
	}
	app.editApplyDims("stretch", 640, 480, "")
	if app.edit.opts.ResizeMode != "fill" {
		t.Fatalf("拉伸应是 fill，实际 %q", app.edit.opts.ResizeMode)
	}
	if app.edit.opts.CropRatio != "" || app.edit.opts.CropWidth != 0 {
		t.Fatalf("换模式没清掉裁剪: %+v", app.edit.opts)
	}
}

func TestImageEditSaveMySizeAndRatio(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()
	app.edit.dimW, app.edit.dimH = 320, 240
	app.editSaveMySize()
	if len(app.edit.sizes) != 1 || app.edit.sizes[0] != (editSize{W: 320, H: 240}) {
		t.Fatalf("尺寸没存下: %+v", app.edit.sizes)
	}
	if !strings.Contains(app.edit.notice, "320×240") {
		t.Fatalf("存尺寸提示不对: %q", app.edit.notice)
	}
	app.editSaveMySize()
	if len(app.edit.sizes) != 1 {
		t.Fatalf("重复尺寸又存了一遍: %+v", app.edit.sizes)
	}
	app.edit.dimW, app.edit.dimH = 0, 0
	app.edit.notice = ""
	app.editSaveMySize()
	if app.edit.notice == "" {
		t.Fatal("没尺寸时应提示")
	}

	app.edit.dimW, app.edit.dimH = 4, 3
	app.edit.opts.CropRatio = ""
	app.editSaveMyRatio()
	if len(app.edit.ratios) != 1 || app.edit.ratios[0] != "4:3" {
		t.Fatalf("比例没存下: %+v", app.edit.ratios)
	}
	app.edit.opts.CropRatio = "4:3"
	app.edit.notice = ""
	app.editSaveMyRatio()
	if len(app.edit.ratios) != 1 {
		t.Fatalf("重复比例又存了一遍: %+v", app.edit.ratios)
	}
}

func TestImageEditRecipeRoundTrip(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()
	app.edit.opts.Format = "webp"
	app.edit.opts.Brightness = 12
	app.edit.enabled = editFeatureSet{Format: true, Adjust: true}

	app.editSaveRecipe("电商")
	if len(app.edit.recipes) != 1 || app.edit.recipes[0].Name != "电商" {
		t.Fatalf("配方没存下: %+v", app.edit.recipes)
	}
	app.editClearAll()
	if len(app.editChips()) != 0 {
		t.Fatalf("清空后还有生效项: %+v", app.editChips())
	}
	app.editApplyRecipe(0)
	if app.edit.opts.Format != "webp" || app.edit.opts.Brightness != 12 {
		t.Fatalf("配方没恢复: %+v", app.edit.opts)
	}
	if !app.edit.enabled.Format || !app.edit.enabled.Adjust || app.edit.enabled.Crop {
		t.Fatalf("配方开关不对: %+v", app.edit.enabled)
	}
	if len(app.editChips()) != 2 {
		t.Fatalf("配方应用后的标签不对: %+v", app.editChips())
	}

	app.edit.recipeErr = ""
	app.editSaveRecipe("  ")
	if app.edit.recipeErr == "" {
		t.Fatal("空配方名应提示")
	}
	app.editClearAll()
	app.edit.recipeErr = ""
	app.editSaveRecipe("空配方")
	if app.edit.recipeErr == "" {
		t.Fatal("没有功能时应拒绝存配方")
	}
}

func TestImageEditClaimOutputAvoidsOverwrite(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	claimed := map[string]bool{}

	first := editClaimOutput(dir, "cat", ".png", claimed, &mu)
	if first != filepath.Join(dir, "cat.png") {
		t.Fatalf("第一个输出名不对: %s", first)
	}
	if err := os.WriteFile(first, []byte("已存在"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := editClaimOutput(dir, "cat", ".png", claimed, &mu)
	if second != filepath.Join(dir, "cat-1.png") {
		t.Fatalf("重名应追加序号: %s", second)
	}
	third := editClaimOutput(dir, "cat", ".png", claimed, &mu)
	if third != filepath.Join(dir, "cat-2.png") {
		t.Fatalf("第三次应到 -2: %s", third)
	}

	// 盘上已有同名文件但本批没占过，也要让开。
	exists := filepath.Join(dir, "dog.png")
	if err := os.WriteFile(exists, []byte("旧图"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := editClaimOutput(dir, "dog", ".png", claimed, &mu); got != filepath.Join(dir, "dog-1.png") {
		t.Fatalf("已有文件被覆盖成 %s", got)
	}
}

func TestImageEditProcessOneWritesSuffixedFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "cat.png")
	editTestImage(t, src, 24, 16)
	outDir := filepath.Join(dir, "out")

	res := editProcessOne(src, imageedit.Options{Format: "png"}, outDir, "_edit", map[string]bool{}, &sync.Mutex{}, 0)
	if res.err != nil {
		t.Fatalf("处理失败: %v", res.err)
	}
	if res.output != filepath.Join(outDir, "cat_edit.png") {
		t.Fatalf("输出名不对: %s", res.output)
	}
	if info, err := os.Stat(res.output); err != nil || info.Size() == 0 {
		t.Fatalf("输出文件没写成: %v", err)
	}

	again := editProcessOne(src, imageedit.Options{Format: "png"}, outDir, "_edit", map[string]bool{}, &sync.Mutex{}, 0)
	if again.err != nil {
		t.Fatalf("第二次处理失败: %v", again.err)
	}
	if again.output != filepath.Join(outDir, "cat_edit-1.png") {
		t.Fatalf("第二次输出名不对: %s", again.output)
	}
}

func TestImageEditPickHelpers(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()

	// Empty file list stays safe.
	app.editSelectAllPicks()
	if len(app.edit.picks) != 0 || app.edit.pickAnchor != 0 {
		t.Fatalf("空列表全选结果异常: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}

	app.edit.files = []*editFile{
		{Path: "/图/a.png", Name: "a.png", Status: "pending"},
		{Path: "/图/b.png", Name: "b.png", Status: "ok"},
		{Path: "/图/c.png", Name: "c.png", Status: "failed"},
		{Path: "/图/d.png", Name: "d.png", Status: "pending"},
	}
	app.edit.fileOps["/图/d.png"] = imageedit.Options{CropWidth: 100, CropHeight: 100}
	app.edit.pickAnchor = 2

	app.editSelectAllPicks()
	if len(app.edit.picks) != 4 || app.edit.pickAnchor != 0 {
		t.Fatalf("全选失败: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}

	app.editInvertPicks()
	if len(app.edit.picks) != 0 || app.edit.pickAnchor != 0 {
		t.Fatalf("全选后反选应清空: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}

	app.edit.picks["/图/a.png"] = true
	app.edit.picks["/图/c.png"] = true
	app.edit.picks["/图/stale.png"] = true
	app.editInvertPicks()
	if len(app.edit.picks) != 2 || !app.edit.picks["/图/b.png"] || !app.edit.picks["/图/d.png"] {
		t.Fatalf("反选结果不对: %v", app.edit.picks)
	}
	if app.edit.pickAnchor != 1 {
		t.Fatalf("反选后锚点应为 1，实际 %d", app.edit.pickAnchor)
	}

	app.editPickByStatus("ok")
	if len(app.edit.picks) != 1 || !app.edit.picks["/图/b.png"] || app.edit.pickAnchor != 1 {
		t.Fatalf("按 ok 勾选不对: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}

	app.editPickByStatus("failed")
	if len(app.edit.picks) != 1 || !app.edit.picks["/图/c.png"] || app.edit.pickAnchor != 2 {
		t.Fatalf("按 failed 勾选不对: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}

	app.editPickByStatus("pending")
	if len(app.edit.picks) != 2 || !app.edit.picks["/图/a.png"] || !app.edit.picks["/图/d.png"] || app.edit.pickAnchor != 0 {
		t.Fatalf("按 pending 勾选不对: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}

	app.editPickByStatus("customCrop")
	if len(app.edit.picks) != 1 || !app.edit.picks["/图/d.png"] || app.edit.pickAnchor != 3 {
		t.Fatalf("按 customCrop 勾选不对: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}

	app.editPickByStatus("unknown")
	if len(app.edit.picks) != 0 || app.edit.pickAnchor != 0 {
		t.Fatalf("未知状态应清空勾选: picks=%v anchor=%d", app.edit.picks, app.edit.pickAnchor)
	}
}

func TestImageEditRemovePicked(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()
	app.edit.files = []*editFile{
		{Path: "/图/a.png", Name: "a.png", Status: "pending"},
		{Path: "/图/b.png", Name: "b.png", Status: "pending"},
		{Path: "/图/c.png", Name: "c.png", Status: "pending"},
		{Path: "/图/d.png", Name: "d.png", Status: "pending"},
	}
	app.edit.fileOps["/图/a.png"] = imageedit.Options{CropWidth: 80, CropHeight: 80}
	app.edit.fileOps["/图/c.png"] = imageedit.Options{CropWidth: 120, CropHeight: 120}
	app.edit.picks["/图/a.png"] = true
	app.edit.selected = 2
	app.edit.zoom = 2.5

	// Guard while running.
	app.edit.running = true
	app.editRemovePicked()
	if len(app.edit.files) != 4 || !app.edit.picks["/图/a.png"] {
		t.Fatalf("运行中不应删除已勾选文件: len=%d picks=%v", len(app.edit.files), app.edit.picks)
	}
	app.edit.running = false

	// Removing file before selected keeps selected pointing to c.png without resetting view.
	app.editRemovePicked()
	if len(app.edit.files) != 3 || app.edit.selected != 1 || app.edit.selectedFile().Path != "/图/c.png" {
		t.Fatalf("删前项后选中不对: len=%d selected=%d", len(app.edit.files), app.edit.selected)
	}
	if _, ok := app.edit.fileOps["/图/a.png"]; ok {
		t.Fatal("已删除文件的 fileOps 应被清理")
	}
	if _, ok := app.edit.fileOps["/图/c.png"]; !ok {
		t.Fatal("未删除文件的 fileOps 不应被清理")
	}
	if len(app.edit.picks) != 0 {
		t.Fatalf("删除后 picks 应清空: %v", app.edit.picks)
	}
	if app.edit.zoom != 2.5 {
		t.Fatalf("选中文件未变时不应重置视图: zoom=%v", app.edit.zoom)
	}

	// Removing trailing files including selected clamps index and resets view.
	app.edit.picks["/图/c.png"] = true
	app.edit.picks["/图/d.png"] = true
	app.editRemovePicked()
	if len(app.edit.files) != 1 || app.edit.selected != 0 || app.edit.selectedFile().Path != "/图/b.png" {
		t.Fatalf("删尾项后选中不对: len=%d selected=%d", len(app.edit.files), app.edit.selected)
	}
	if app.edit.zoom != 1 {
		t.Fatalf("选中文件变化后应重置视图: zoom=%v", app.edit.zoom)
	}

	// Removing last remaining file sets selected = -1.
	app.edit.zoom = 1.8
	app.edit.picks["/图/b.png"] = true
	app.editRemovePicked()
	if len(app.edit.files) != 0 || app.edit.selected != -1 {
		t.Fatalf("删空后 selected 应为 -1: len=%d selected=%d", len(app.edit.files), app.edit.selected)
	}
	if app.edit.zoom != 1 {
		t.Fatalf("删空后应重置视图: zoom=%v", app.edit.zoom)
	}
}

func TestImageEditSyncCropToAndClearCustomCrops(t *testing.T) {
	app := &App{}
	app.editEnsureMaps()
	app.edit.srcW, app.edit.srcH = 800, 600
	app.edit.selected = -1

	// No selected file -> notice & false.
	if app.editSyncCropTo("all") {
		t.Fatal("无选中图时不应同步成功")
	}
	if app.edit.notice != "请先为当前图片设置裁剪区域" {
		t.Fatalf("无选中图提示不对: %q", app.edit.notice)
	}

	app.edit.files = []*editFile{
		{Path: "/图/a.png", Name: "a.png", Status: "pending"},
		{Path: "/图/b.png", Name: "b.png", Status: "pending"},
		{Path: "/图/c.png", Name: "c.png", Status: "pending"},
	}
	app.edit.selected = 0
	app.edit.notice = ""

	// No crop box -> notice & false.
	if app.editSyncCropTo("picked") {
		t.Fatal("无裁剪框时不应同步成功")
	}
	if app.edit.notice != "请先为当前图片设置裁剪区域" {
		t.Fatalf("无裁剪框提示不对: %q", app.edit.notice)
	}

	// Uniform crop mode with centered crop box and CropRatio -> sync to picked (none picked -> selected).
	app.edit.enabled.Crop = true
	app.edit.uniformCrop = true
	app.edit.opts.CropWidth = 200
	app.edit.opts.CropHeight = 100
	app.edit.opts.CropRatio = "2:1"
	if !app.editSyncCropTo("picked") {
		t.Fatal("未勾选时同步到 picked 应回退到当前选中图")
	}
	if app.edit.uniformCrop {
		t.Fatal("同步后应关闭 uniformCrop")
	}
	if ov, ok := app.edit.fileOps["/图/a.png"]; !ok || ov.CropWidth != 200 || ov.CropHeight != 100 ||
		ov.CropLeft == nil || *ov.CropLeft != 300 || ov.CropTop == nil || *ov.CropTop != 250 || ov.CropRatio != "2:1" {
		t.Fatalf("同步到当前图内容不对: %+v", ov)
	}
	if len(app.edit.fileOps) != 1 {
		t.Fatalf("未勾选时不应写入其他图: %+v", app.edit.fileOps)
	}

	// Sync to picked subset only.
	app.edit.fileOps["/图/a.png"] = imageedit.Options{
		CropLeft:   imageedit.Int(10),
		CropTop:    imageedit.Int(20),
		CropWidth:  300,
		CropHeight: 200,
		CropRatio:  "3:2",
	}
	app.edit.picks["/图/b.png"] = true
	if !app.editSyncCropTo("picked") {
		t.Fatal("同步到已选应成功")
	}
	if _, ok := app.edit.fileOps["/图/c.png"]; ok {
		t.Fatal("未勾选的 c.png 不应被同步")
	}
	ovB := app.edit.fileOps["/图/b.png"]
	if ovB.CropWidth != 300 || ovB.CropHeight != 200 || ovB.CropLeft == nil || *ovB.CropLeft != 10 || ovB.CropTop == nil || *ovB.CropTop != 20 || ovB.CropRatio != "3:2" {
		t.Fatalf("同步到已选结果不对: %+v", ovB)
	}

	// Sync to all and verify independent CropLeft/CropTop pointers.
	if !app.editSyncCropTo("all") {
		t.Fatal("同步到全部应成功")
	}
	if len(app.edit.fileOps) != 3 {
		t.Fatalf("同步到全部应覆盖 3 张图: %d", len(app.edit.fileOps))
	}
	if app.edit.notice == "" || !strings.Contains(app.edit.notice, "3") {
		t.Fatalf("同步提示应包含数量: %q", app.edit.notice)
	}
	ovA := app.edit.fileOps["/图/a.png"]
	ovB = app.edit.fileOps["/图/b.png"]
	ovC := app.edit.fileOps["/图/c.png"]
	if ovA.CropLeft == ovB.CropLeft || ovB.CropLeft == ovC.CropLeft || ovA.CropTop == ovB.CropTop || ovB.CropTop == ovC.CropTop {
		t.Fatal("每张图必须持有独立的 CropLeft/CropTop 指针副本")
	}
	*ovA.CropLeft = 99
	if *ovB.CropLeft != 10 || *ovC.CropLeft != 10 {
		t.Fatalf("修改单图指针影响了其他图: b=%d c=%d", *ovB.CropLeft, *ovC.CropLeft)
	}

	// Clear custom crops by scope: selected, picked, all.
	app.edit.selected = 0
	app.editClearCustomCrops("selected")
	if _, ok := app.edit.fileOps["/图/a.png"]; ok {
		t.Fatal("清除 selected 失败")
	}
	if len(app.edit.fileOps) != 2 {
		t.Fatalf("清除 selected 不应影响其他图: %d", len(app.edit.fileOps))
	}

	app.edit.picks = map[string]bool{"/图/b.png": true}
	app.editClearCustomCrops("picked")
	if _, ok := app.edit.fileOps["/图/b.png"]; ok {
		t.Fatal("清除 picked 失败")
	}
	if _, ok := app.edit.fileOps["/图/c.png"]; !ok {
		t.Fatal("清除 picked 不应删未勾选图")
	}

	app.editClearCustomCrops("all")
	if len(app.edit.fileOps) != 0 {
		t.Fatalf("清除 all 后仍留有覆盖: %+v", app.edit.fileOps)
	}
}

func TestImageEditServiceBatchDTOAndMethods(t *testing.T) {
	ctx := context.Background()

	// Nil app safety for all new service methods.
	nilSvc := &ImageEditService{}
	nilSvc.EditSelectAllPicks(ctx)
	nilSvc.EditInvertPicks(ctx)
	nilSvc.EditPickByStatus(ctx, "ok")
	nilSvc.EditRemovePicked(ctx)
	nilSvc.EditSyncCropTo(ctx, "all")
	nilSvc.EditClearCustomCrops(ctx, "all")

	dir := t.TempDir()
	p1 := filepath.Join(dir, "1.png")
	p2 := filepath.Join(dir, "2.png")
	p3 := filepath.Join(dir, "3.png")
	p4 := filepath.Join(dir, "4.png")
	p5 := filepath.Join(dir, "5.png")
	p6 := filepath.Join(dir, "6.png")
	for _, p := range []string{p1, p2, p3, p4, p5, p6} {
		editTestImage(t, p, 120, 80)
	}

	app := &App{}
	app.editEnsureMaps()
	app.edit.srcW, app.edit.srcH = 120, 80
	app.edit.files = []*editFile{
		{Path: p1, Name: "1.png", Status: "pending"},
		{Path: p2, Name: "2.png", Status: "ok"},
		{Path: p3, Name: "3.png", Status: "failed"},
		{Path: p4, Name: "4.png", Status: "pending"},
		{Path: p5, Name: "5.png", Status: "pending"},
		{Path: p6, Name: "6.png", Status: "pending"},
	}
	app.edit.selected = 0
	// Cover every HasCustomCrop trigger plus whitespace-only CropRatio & orphan fileOps entry.
	app.edit.fileOps[p1] = imageedit.Options{CropWidth: 40}
	app.edit.fileOps[p2] = imageedit.Options{CropHeight: 40}
	app.edit.fileOps[p3] = imageedit.Options{CropLeft: imageedit.Int(5)}
	app.edit.fileOps[p4] = imageedit.Options{CropTop: imageedit.Int(5)}
	app.edit.fileOps[p5] = imageedit.Options{CropRatio: "16:9"}
	app.edit.fileOps[p6] = imageedit.Options{CropRatio: "   "}
	app.edit.fileOps[filepath.Join(dir, "orphan.png")] = imageedit.Options{CropWidth: 50, CropHeight: 50}

	svc := &ImageEditService{app: app}
	dto := svc.GetImageEditState(ctx)
	if dto.CustomCropCount != 5 {
		t.Fatalf("CustomCropCount 应为 5，实际 %d", dto.CustomCropCount)
	}
	for i := 0; i < 5; i++ {
		if !dto.Files[i].HasCustomCrop {
			t.Fatalf("第 %d 张图应为 HasCustomCrop=true", i)
		}
	}
	if dto.Files[5].HasCustomCrop {
		t.Fatal("空白 CropRatio 不应算作 HasCustomCrop")
	}

	// Verify camelCase JSON tags on ImageEditStateDTO and EditFileDTO.
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	if !strings.Contains(string(raw), `"customCropCount":5`) || !strings.Contains(string(raw), `"hasCustomCrop":true`) {
		t.Fatalf("缺少预期的 camelCase 字段: %s", string(raw))
	}

	// Service IPC round-trips.
	svc.EditSelectAllPicks(ctx)
	if got := svc.GetImageEditState(ctx).PickedCount; got != 6 {
		t.Fatalf("EditSelectAllPicks 后 PickedCount=%d, want 6", got)
	}
	svc.EditInvertPicks(ctx)
	if got := svc.GetImageEditState(ctx).PickedCount; got != 0 {
		t.Fatalf("EditInvertPicks 后 PickedCount=%d, want 0", got)
	}
	svc.EditPickByStatus(ctx, "failed")
	st := svc.GetImageEditState(ctx)
	if st.PickedCount != 1 || !st.Files[2].Picked {
		t.Fatalf("EditPickByStatus(failed) 不对: pickedCount=%d files[2].picked=%v", st.PickedCount, st.Files[2].Picked)
	}

	svc.EditClearCustomCrops(ctx, "all")
	if got := svc.GetImageEditState(ctx).CustomCropCount; got != 0 {
		t.Fatalf("EditClearCustomCrops(all) 后 CustomCropCount=%d, want 0", got)
	}

	svc.EditSetCropBox(ctx, 10, 10, 60, 40)
	svc.EditSyncCropTo(ctx, "all")
	st = svc.GetImageEditState(ctx)
	if st.CustomCropCount != 6 || st.UniformCrop {
		t.Fatalf("EditSyncCropTo(all) 后状态不对: customCropCount=%d uniformCrop=%v", st.CustomCropCount, st.UniformCrop)
	}

	svc.EditPickByStatus(ctx, "failed")
	svc.EditRemovePicked(ctx)
	st = svc.GetImageEditState(ctx)
	if len(st.Files) != 5 || st.CustomCropCount != 5 {
		t.Fatalf("EditRemovePicked 后数量不对: files=%d customCropCount=%d", len(st.Files), st.CustomCropCount)
	}
}
