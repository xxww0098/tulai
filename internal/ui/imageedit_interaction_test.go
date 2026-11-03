package ui

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/imageedit"
)

// newEditInteractionApp 造一个已加载第一张图的图片编辑页，
// 每张图都带缩略图，避免测试里去碰异步加载。
func newEditInteractionApp(t *testing.T, sizes ...[2]int) *App {
	t.Helper()
	dir := t.TempDir()
	a := &App{tab: pageImageEdit}
	for i, size := range sizes {
		path := filepath.Join(dir, fmt.Sprintf("图%d.png", i+1))
		editTestImage(t, path, size[0], size[1])
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s: %v", path, err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("解码 %s: %v", path, err)
		}
		bmp := ui.NewBitmap(img)
		a.edit.files = append(a.edit.files, &editFile{
			Path: path, Name: filepath.Base(path), Status: "pending", Thumb: bmp,
		})
		if i == 0 {
			a.edit.orig = bmp
			a.edit.out = bmp
			a.edit.outW, a.edit.outH = size[0], size[1]
			a.edit.origPath = path
			a.edit.srcW, a.edit.srcH = size[0], size[1]
		}
	}
	a.edit.selected = 0
	a.editEnsureMaps()
	a.edit.previewSig = a.editPreviewSignature(a.edit.files[0].Path)
	return a
}

// editScreenPoint 把源图像素坐标换成窗口坐标：绘制和指针用的是同一套。
func editScreenPoint(t *testing.T, a *App, srcX, srcY float64) (float32, float32) {
	t.Helper()
	st := &a.edit
	if st.imageRect.W <= 0 || st.srcW <= 0 || st.srcH <= 0 {
		t.Fatalf("预览还没排版: %+v", st.imageRect)
	}
	sx := st.imageRect.W / float32(st.srcW)
	sy := st.imageRect.H / float32(st.srcH)
	return st.imageRect.X + float32(srcX)*sx, st.imageRect.Y + float32(srcY)*sy
}

func editNear(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if got < want-tol || got > want+tol {
		t.Fatalf("%s 应为 %.0f 左右，实际 %.0f", name, want, got)
	}
}

func editCropTestApp(t *testing.T) *App {
	t.Helper()
	a := newEditInteractionApp(t, [2]int{800, 600})
	a.edit.enabled.Crop = true
	a.edit.expanded["crop"] = true
	a.edit.mode = "crop"
	a.edit.uniformCrop = true
	a.edit.previewSig = a.editPreviewSignature(a.edit.files[0].Path)
	return a
}

func TestImageEditCropDrawWithPointer(t *testing.T) {
	a := editCropTestApp(t)
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	x0, y0 := editScreenPoint(t, a, 100, 100)
	x1, y1 := editScreenPoint(t, a, 500, 400)
	tt.Press(x0, y0)
	tt.Move((x0+x1)/2, (y0+y1)/2)
	tt.Move(x1, y1)
	tt.Release(x1, y1)

	o := a.edit.opts
	editNear(t, "裁剪左边", float64(*o.CropLeft), 100, 2)
	editNear(t, "裁剪上边", float64(*o.CropTop), 100, 2)
	editNear(t, "裁剪宽", float64(o.CropWidth), 400, 3)
	editNear(t, "裁剪高", float64(o.CropHeight), 300, 3)
	if a.edit.dimW != o.CropWidth || a.edit.dimH != o.CropHeight {
		t.Fatalf("尺寸输入没跟上: %dx%d", a.edit.dimW, a.edit.dimH)
	}
	if a.edit.liveBox != nil || a.edit.cropDrag != nil {
		t.Fatal("松开后还留着拖动状态")
	}
}

func TestImageEditCropMoveWithPointer(t *testing.T) {
	a := editCropTestApp(t)
	a.editSetCropBox(&editCropBox{X: 100, Y: 100, W: 400, H: 300})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	startX, startY := editScreenPoint(t, a, 300, 250)
	endX, endY := editScreenPoint(t, a, 350, 280)
	tt.Press(startX, startY)
	tt.Move(endX, endY)
	tt.Release(endX, endY)

	o := a.edit.opts
	editNear(t, "移动后的左边", float64(*o.CropLeft), 150, 2)
	editNear(t, "移动后的上边", float64(*o.CropTop), 130, 2)
	editNear(t, "移动后的宽", float64(o.CropWidth), 400, 2)
	editNear(t, "移动后的高", float64(o.CropHeight), 300, 2)
}

func TestImageEditCropResizeHandle(t *testing.T) {
	a := editCropTestApp(t)
	a.editSetCropBox(&editCropBox{X: 100, Y: 100, W: 400, H: 300})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	// 右边中间的把手：拖到源图 x=600 处。
	startX, startY := editScreenPoint(t, a, 500, 250)
	endX, endY := editScreenPoint(t, a, 600, 250)
	tt.Press(startX, startY)
	tt.Move(endX, endY)
	tt.Release(endX, endY)

	o := a.edit.opts
	editNear(t, "拉伸后的左边", float64(*o.CropLeft), 100, 3)
	editNear(t, "拉伸后的宽", float64(o.CropWidth), 500, 4)
	editNear(t, "拉伸后的高", float64(o.CropHeight), 300, 3)
}

func TestImageEditCropArrowKeys(t *testing.T) {
	a := editCropTestApp(t)
	a.editSetCropBox(&editCropBox{X: 100, Y: 100, W: 400, H: 300})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	// 先点一下舞台把焦点给它。
	x, y := editScreenPoint(t, a, 300, 250)
	tt.ClickAt(x, y)

	tt.Key(0, ui.KeyRight)
	editNear(t, "右移一格", float64(*a.edit.opts.CropLeft), 101, 1)
	tt.Key(ui.Shift, ui.KeyRight)
	editNear(t, "Shift 右移十格", float64(*a.edit.opts.CropLeft), 111, 1)
	tt.Key(0, ui.KeyDown)
	editNear(t, "下移一格", float64(*a.edit.opts.CropTop), 101, 1)
}

func TestImageEditWheelZoomAndDoubleClickReset(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{1600, 1200})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	x, y := editScreenPoint(t, a, 800, 600)
	tt.Wheel(x, y, 0, -10)
	if a.edit.zoom <= 1 {
		t.Fatalf("滚轮没放大: %.2f", a.edit.zoom)
	}
	tt.ClickAt(x, y)
	tt.ClickAt(x, y)
	if a.edit.zoom != 1 || a.edit.panX != 0 || a.edit.panY != 0 {
		t.Fatalf("双击没回到适合窗口: zoom=%.2f pan=%.0f,%.0f", a.edit.zoom, a.edit.panX, a.edit.panY)
	}

	// HUD 上的缩放按钮和百分比：预览带输入处理，按钮不能被它挡住。
	base := a.edit.baseW
	if err := tt.Click("1:1"); err != nil {
		t.Fatal(err)
	}
	editNear(t, "1:1 缩放", a.edit.zoom, float64(1600/base), 0.02)
	atOneToOne := a.edit.zoom

	if err := tt.Click("−"); err != nil {
		t.Fatal(err)
	}
	editNear(t, "缩小一档", a.edit.zoom, atOneToOne/1.2, 0.02)
	if err := tt.Click("+"); err != nil {
		t.Fatal(err)
	}
	editNear(t, "放大一档", a.edit.zoom, atOneToOne, 0.02)

	if err := tt.Click(fmt.Sprintf("%.0f%%", a.edit.zoom*100)); err != nil {
		t.Fatal(err)
	}
	if a.edit.zoom != 1 {
		t.Fatalf("点百分比应回到适合窗口，实际 %.2f", a.edit.zoom)
	}
}

func TestImageEditCompareToggleAndSplitDrag(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{800, 600})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	if err := tt.Click("对比"); err != nil {
		t.Fatal(err)
	}
	if !a.edit.compare {
		t.Fatal("对比没打开")
	}

	img := a.edit.imageRect
	sx := img.X + img.W*0.5
	sy := img.Y + img.H*0.5
	tt.Press(sx, sy)
	tt.Move(img.X+img.W*0.25, sy)
	tt.Release(img.X+img.W*0.25, sy)
	editNear(t, "分割位置", a.edit.split, 25, 1)
}

func TestImageEditArrowNavigatesSelection(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{400, 300}, [2]int{400, 300}, [2]int{400, 300})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	x, y := editScreenPoint(t, a, 200, 150)
	tt.ClickAt(x, y)
	tt.Key(0, ui.KeyRight)
	if a.edit.selected != 1 {
		t.Fatalf("右方向键应选第二张，实际 %d", a.edit.selected)
	}
	tt.Key(0, ui.KeyLeft)
	if a.edit.selected != 0 {
		t.Fatalf("左方向键应回第一张，实际 %d", a.edit.selected)
	}
}

func TestImageEditDeleteKeyRemovesSelected(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{400, 300}, [2]int{400, 300})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	x, y := editScreenPoint(t, a, 200, 150)
	tt.ClickAt(x, y)
	tt.Key(0, ui.KeyDelete)
	if len(a.edit.files) != 1 || a.edit.selected != 0 {
		t.Fatalf("删除后应剩一张并选中它: %d 张，选中 %d", len(a.edit.files), a.edit.selected)
	}
}

func TestImageEditDropFilesAddsImages(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.png")
	second := filepath.Join(dir, "b.png")
	note := filepath.Join(dir, "说明.txt")
	editTestImage(t, first, 40, 30)
	editTestImage(t, second, 40, 30)
	if err := os.WriteFile(note, []byte("不是图片"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &App{tab: pageImageEdit}
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)
	if !tt.DropFiles(800, 400, first, second, note) {
		t.Fatal("拖到空白舞台的文件没人接")
	}
	if len(a.edit.files) != 2 || a.edit.selected != 0 {
		t.Fatalf("拖入后应有 2 张并选中第一张: %d 张，选中 %d", len(a.edit.files), a.edit.selected)
	}
	if a.edit.files[0].Name != "a.png" {
		t.Fatalf("第一张应是 a.png，实际 %q", a.edit.files[0].Name)
	}
}

// editWaitRun 等一轮批量处理跑完。
func editWaitRun(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		a.flushUIUpdates()
		if !a.edit.running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("批量处理没有在 20 秒内结束")
}

func TestImageEditBackgroundUpdateWaitsForUIFrame(t *testing.T) {
	a := &App{tab: pageImageEdit}
	done := make(chan struct{})
	go func() {
		a.postUI(func() { a.edit.notice = "后台结果" })
		close(done)
	}()
	<-done
	if a.edit.notice != "" {
		t.Fatal("后台线程直接修改了界面状态")
	}
	ui.NewTester(a.view, 1280, 860)
	if a.edit.notice != "后台结果" {
		t.Fatalf("界面帧没有应用结果: %q", a.edit.notice)
	}
}

func TestImageEditRunAllWritesOutputs(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{60, 40}, [2]int{40, 60})
	srcDir := filepath.Dir(a.edit.files[0].Path)
	outDir := filepath.Join(srcDir, "out")
	a.edit.outDir = outDir
	a.edit.suffix = "_编辑"
	a.edit.opts.Format = "png"
	a.edit.enabled.Format = true

	a.editRunAll()
	editWaitRun(t, a)
	for _, name := range []string{"图1_编辑.png", "图2_编辑.png"} {
		info, err := os.Stat(filepath.Join(outDir, name))
		if err != nil || info.Size() == 0 {
			t.Fatalf("输出 %s 不对: %v", name, err)
		}
	}
	for _, f := range a.edit.files {
		if f.Status != "ok" || f.Output == "" {
			t.Fatalf("文件状态不对: %+v", f)
		}
	}
	if !strings.Contains(a.edit.notice, "完成 2 个") {
		t.Fatalf("完成提示不对: %q", a.edit.notice)
	}
}

func TestImageEditCancelLeavesRestPending(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{40, 30}, [2]int{40, 30}, [2]int{40, 30})
	outDir := filepath.Join(t.TempDir(), "out")
	paths := make([]string, len(a.edit.files))
	opts := make([]imageedit.Options, len(a.edit.files))
	for i, f := range a.edit.files {
		paths[i] = f.Path
	}
	cancel := make(chan struct{})
	close(cancel)
	a.edit.running = true
	a.editBatchRun([]int{0, 1, 2}, paths, opts, outDir, "_x", cancel)
	a.flushUIUpdates()

	for _, f := range a.edit.files {
		if f.Status != "pending" || f.Output != "" {
			t.Fatalf("停止后不该处理任何一张: %+v", f)
		}
	}
	if a.edit.running {
		t.Fatal("停止后还在 running")
	}
	if !strings.Contains(a.edit.notice, "已停止") || !strings.Contains(a.edit.notice, "3 张没处理") {
		t.Fatalf("停止提示不对: %q", a.edit.notice)
	}
	if entries, err := os.ReadDir(outDir); err == nil && len(entries) > 0 {
		t.Fatalf("停止后不该写出文件: %d 个", len(entries))
	}
}

func TestImageEditRunsOnlyPicked(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{60, 40}, [2]int{40, 60}, [2]int{50, 50})
	outDir := filepath.Join(t.TempDir(), "out")
	a.edit.outDir = outDir
	a.edit.opts.Format = "png"
	a.edit.enabled.Format = true
	a.edit.picks = map[string]bool{a.edit.files[1].Path: true}

	if got := a.editTargetIndices(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("处理目标不对: %v", got)
	}
	a.editRunAll()
	editWaitRun(t, a)

	if got := a.edit.files[1].Status; got != "ok" {
		t.Fatalf("勾选的图应处理成功，实际 %q", got)
	}
	for _, i := range []int{0, 2} {
		if a.edit.files[i].Status != "pending" || a.edit.files[i].Output != "" {
			t.Fatalf("没勾的第 %d 张也被处理了: %+v", i, a.edit.files[i])
		}
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("只该写出一张，实际 %d 个", len(entries))
	}
}

func TestImageEditFailedFilesShowReasonAndRetry(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{40, 30}, [2]int{40, 30})
	outDir := filepath.Join(t.TempDir(), "out")
	a.edit.outDir = outDir
	a.edit.opts.Format = "png"
	a.edit.enabled.Format = true
	broken := a.edit.files[1].Path
	if err := os.WriteFile(broken, []byte("这不是图片"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.editRunAll()
	editWaitRun(t, a)
	if a.edit.files[0].Status != "ok" {
		t.Fatalf("好图应成功: %+v", a.edit.files[0])
	}
	bad := a.edit.files[1]
	if bad.Status != "failed" || bad.Err == "" {
		t.Fatalf("坏图应失败并带上原因: %+v", bad)
	}
	if !strings.Contains(a.edit.notice, "失败 1 个") {
		t.Fatalf("提示没报失败数: %q", a.edit.notice)
	}

	// 提示行右边的「查看详情」能打开结果弹窗。
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)
	if err := tt.Click("查看详情"); err != nil {
		t.Fatal(err)
	}
	if !a.edit.resultOpen {
		t.Fatal("点查看详情没打开结果弹窗")
	}
	a.edit.resultOpen = false

	// 修好之后只重跑失败的那张。
	if got := a.editFailedIndices(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("失败清单不对: %v", got)
	}
	editTestImage(t, broken, 40, 30)
	a.editRetryFailed()
	editWaitRun(t, a)
	if a.edit.files[1].Status != "ok" || a.edit.files[1].Err != "" {
		t.Fatalf("重试后应成功: %+v", a.edit.files[1])
	}
}

func TestImageEditThumbModifierClickPicks(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{400, 300}, [2]int{400, 300}, [2]int{400, 300})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	st := &a.edit
	if st.stripRect.W <= 0 {
		t.Fatal("缩略图条还没排版")
	}
	y := st.stripRect.Y + st.stripRect.H/2
	at := func(i int) float32 {
		return st.stripRect.X + float32(i)*72 + 32 - st.stripScroll.X
	}

	tt.ClickAtWith(ui.Cmd, at(0), y)
	if !st.picks[st.files[0].Path] {
		t.Fatal("Cmd 点击没勾上第一张")
	}
	// 普通点击只换预览，不该清掉已经勾好的。
	tt.ClickAtWith(0, at(1), y)
	if st.selected != 1 {
		t.Fatalf("普通点击应选中第二张，实际 %d", st.selected)
	}
	if len(st.picks) != 1 {
		t.Fatalf("普通点击把勾选清掉了: %v", st.picks)
	}
	// Shift 连选从第二张到第三张。
	tt.ClickAtWith(ui.Shift, at(2), y)
	if len(st.picks) != 3 {
		t.Fatalf("Shift 连选不对: %v", st.picks)
	}
	if err := tt.Click("清空选择"); err != nil {
		t.Fatal(err)
	}
	if len(st.picks) != 0 {
		t.Fatalf("清空选择后还留着勾选: %v", st.picks)
	}
}

func TestImageEditThumbnailClickSelects(t *testing.T) {
	a := newEditInteractionApp(t, [2]int{400, 300}, [2]int{400, 300}, [2]int{400, 300})
	tt := ui.NewTester(a.view, 1280, 860)
	tt.Advance(20 * time.Millisecond)

	st := &a.edit
	if st.stripRect.W <= 0 {
		t.Fatal("缩略图条还没排版")
	}
	x := st.stripRect.X + 72 + 32 - st.stripScroll.X
	y := st.stripRect.Y + st.stripRect.H/2
	tt.ClickAt(x, y)
	if a.edit.selected != 1 {
		t.Fatalf("点第二张缩略图应选中它，实际 %d", a.edit.selected)
	}
}
