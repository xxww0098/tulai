package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func galleryTestApp(t *testing.T, count int) (*App, []string) {
	t.Helper()
	dir := t.TempDir()
	sources := make([]sourceImage, count)
	paths := make([]string, count)
	for i := range sources {
		path := filepath.Join(dir, fmt.Sprintf("%02d-gallery.png", i+1))
		writeTinyPNG(t, path)
		paths[i] = path
		sources[i] = sourceImage{Path: path, Hash: fmt.Sprintf("gallery-%d", i+1)}
	}
	return &App{
		engine:       tasks.NewEngine(nil, nil, nil, ""),
		tab:          pageWash,
		statePage:    pageWash,
		platformAuto: true,
		sources:      sources,
	}, paths
}

func TestGalleryOpensAndRemovesSelectedImages(t *testing.T) {
	a, paths := galleryTestApp(t, 6)
	tt := ui.NewTester(a.view, 1280, 900)

	if err := tt.Click("图库"); err != nil {
		t.Fatal(err)
	}
	if !a.galleryOn || !tt.HasText("原图图库") || !tt.HasText("共 6 张") {
		t.Fatalf("gallery did not open: on=%v texts=%v", a.galleryOn, tt.Texts())
	}
	for _, path := range paths {
		if !tt.HasText(filepath.Base(path)) {
			t.Fatalf("gallery is missing %s: %v", filepath.Base(path), tt.Texts())
		}
	}
	if err := tt.Click("选择 02 02-gallery.png"); err != nil {
		t.Fatal(err)
	}
	if !a.gallerySelected[paths[1]] || !tt.HasText("已选 1 张") {
		t.Fatalf("selection did not update: selected=%v texts=%v", a.gallerySelected, tt.Texts())
	}
	if err := tt.Click("删除已选 1 张"); err != nil {
		t.Fatal(err)
	}
	if !a.galleryOn || len(a.sources) != 5 || a.sources[1].Path != paths[2] {
		t.Fatalf("batch remove state: open=%v sources=%+v", a.galleryOn, a.sources)
	}
	if err := tt.Click("移除 04-gallery.png"); err != nil {
		t.Fatal(err)
	}
	if len(a.sources) != 4 || !a.galleryOn {
		t.Fatalf("single remove state: open=%v sources=%+v", a.galleryOn, a.sources)
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if a.galleryOn {
		t.Fatal("gallery did not close")
	}
}

func TestGallerySelectAllAndDeleteKeepsWindowOpen(t *testing.T) {
	a, _ := galleryTestApp(t, 3)
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("图库"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	if !a.galleryAllSelected() || !tt.HasText("已选 3 张") {
		t.Fatalf("select all did not update: selected=%v texts=%v", a.gallerySelected, tt.Texts())
	}
	if err := tt.Click("删除已选 3 张"); err != nil {
		t.Fatal(err)
	}
	if !a.galleryOn || len(a.sources) != 0 || !tt.HasText("拖入图片或文件夹") {
		t.Fatalf("empty gallery state: open=%v sources=%+v texts=%v", a.galleryOn, a.sources, tt.Texts())
	}
}

func TestGalleryClearWashedKeepsPendingImagesAndFiles(t *testing.T) {
	a, paths := galleryTestApp(t, 4)
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a.store = st
	output := filepath.Join(t.TempDir(), "washed-result.png")
	writeTinyPNG(t, output)
	a.sources[0].Washed, a.sources[0].WashOutput = true, output
	a.sources[2].Washed, a.sources[2].WashOutput = true, output
	a.srcSel = 3
	a.queue = []tasks.Task{
		{Index: 1, Status: tasks.StatusSuccess, Images: []string{paths[0]}, Results: []string{output}},
		{Index: 2, Status: tasks.StatusFailed, Images: []string{paths[1]}},
	}
	a.saveSettings()

	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("图库"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"选择 01 01-gallery.png", "选择 04 04-gallery.png"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("清除已洗"); err != nil {
		t.Fatal(err)
	}
	if !a.galleryOn || len(a.sources) != 2 || a.sources[0].Path != paths[1] || a.sources[1].Path != paths[3] {
		t.Fatalf("clear washed changed the wrong images: open=%v sources=%+v", a.galleryOn, a.sources)
	}
	if a.srcSel != 1 || len(a.gallerySelected) != 1 || !a.gallerySelected[paths[3]] || !tt.HasText("已选 1 张") {
		t.Fatalf("remaining selection was not preserved: srcSel=%d selected=%v", a.srcSel, a.gallerySelected)
	}
	if len(a.queue) != 2 || a.queue[0].Results[0] != output {
		t.Fatalf("task history changed: %+v", a.queue)
	}
	for _, path := range append(paths, output) {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("image file was removed: %s: %v", path, err)
		}
	}
	if got := a.washPendingPaths(); len(got) != 2 || got[0] != paths[1] || got[1] != paths[3] {
		t.Fatalf("pending images changed: %v", got)
	}

	// Normal application shutdown saves the remaining image list and history.
	a.saveSettings()
	restored := &App{store: st, engine: tasks.NewEngine(st, nil, nil, "")}
	restored.loadSettings()
	if len(restored.sources) != 2 || restored.sources[0].Path != paths[1] || restored.sources[1].Path != paths[3] {
		t.Fatalf("cleared images returned after restart: %+v", restored.sources)
	}
	if washed, pending := restored.washCounts(); washed != 0 || pending != 2 || len(restored.queue) != 2 {
		t.Fatalf("restart lost pending images or task history: washed=%d pending=%d queue=%+v", washed, pending, restored.queue)
	}
}

func TestGalleryClearWashedHandlesEmptyAndRunning(t *testing.T) {
	a, paths := galleryTestApp(t, 2)
	for i := range a.sources {
		a.sources[i].Washed = true
	}
	a.skipImages = true
	a.srcSel = 1
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("图库"); err != nil {
		t.Fatal(err)
	}
	a.running = true
	tt.Frame()
	button, ok := tt.Find("清除已洗")
	if !ok {
		t.Fatal("clear washed button not found")
	}
	tt.ClickAt(button.X+button.W/2, button.Y+button.H/2)
	if len(a.sources) != 2 {
		t.Fatalf("running task allowed clearing: %+v", a.sources)
	}
	a.clearWashedGallerySources()
	if len(a.sources) != 2 {
		t.Fatalf("running task bypassed the clear guard: %+v", a.sources)
	}
	a.running = false
	tt.Frame()
	if err := tt.Click("清除已洗"); err != nil {
		t.Fatal(err)
	}
	if !a.galleryOn || len(a.sources) != 0 || a.srcSel != -1 || a.skipImages || !tt.HasText("拖入图片或文件夹") {
		t.Fatalf("all-washed empty state: open=%v sources=%+v srcSel=%d skip=%v", a.galleryOn, a.sources, a.srcSel, a.skipImages)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("original file was removed: %s: %v", path, err)
		}
	}
}

func TestGalleryClearWashedButtonFitsAndIsWashOnly(t *testing.T) {
	for _, width := range []int{320, 760, 1280} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			a, paths := galleryTestApp(t, 2)
			a.sources[0].Washed = true
			a.galleryOn, a.galleryTitle = true, "原图"
			tt := ui.NewTester(a.view, width, 900)
			button, ok := tt.Find("清除已洗")
			if !ok || button.X < 0 || button.X+button.W > float32(width) {
				t.Fatalf("clear button outside window: found=%v bounds=%+v", ok, button)
			}
			if err := tt.Click("清除已洗"); err != nil {
				t.Fatal(err)
			}
			if len(a.sources) != 1 || a.sources[0].Path != paths[1] {
				t.Fatalf("button did not clear washed image: %+v", a.sources)
			}
		})
	}
	a, _ := galleryTestApp(t, 1)
	a.tab, a.statePage = pageHome, pageHome
	a.galleryOn, a.galleryTitle = true, "参考图"
	tt := ui.NewTester(a.view, 1280, 900)
	if tt.HasText("清除已洗") {
		t.Fatal("clear washed button appeared in the reference image gallery")
	}
}

func TestGalleryAcceptsDropsWhilePopulated(t *testing.T) {
	a, paths := galleryTestApp(t, 3)
	extraDir := t.TempDir()
	extra := filepath.Join(extraDir, "添加后保持打开.png")
	writeTinyPNG(t, extra)
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("图库"); err != nil {
		t.Fatal(err)
	}
	panel, ok := tt.Find("原图图库")
	if !ok {
		t.Fatal("gallery panel not found")
	}
	if !tt.DropFiles(panel.X+panel.W/2, panel.Y+panel.H/2, extra) {
		t.Fatal("populated gallery did not accept dropped files")
	}
	if !a.galleryOn || len(a.sources) != 4 || a.sources[3].Path != extra {
		t.Fatalf("gallery drop state: open=%v sources=%+v", a.galleryOn, a.sources)
	}
	if !tt.HasText("共 4 张") || !tt.HasText(filepath.Base(extra)) {
		t.Fatalf("gallery did not refresh after drop: %v", tt.Texts())
	}
	for _, path := range paths[:2] {
		if !tt.HasText(filepath.Base(path)) {
			t.Fatalf("drop lost existing image %s", filepath.Base(path))
		}
	}
}

func TestGalleryLeftDragSelectsAcrossImages(t *testing.T) {
	a, paths := galleryTestApp(t, 6)
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("图库"); err != nil {
		t.Fatal(err)
	}
	first, ok := tt.Find("拖动选择 " + filepath.Base(paths[0]))
	if !ok {
		t.Fatal("first gallery image not found")
	}
	third, ok := tt.Find("拖动选择 " + filepath.Base(paths[2]))
	if !ok {
		t.Fatal("third gallery image not found")
	}
	startX, startY := first.X+first.W/2, first.Y+first.H/2
	endX, endY := third.X+third.W/2, third.Y+third.H/2
	tt.Press(startX, startY)
	tt.Move((startX+endX)/2, (startY+endY)/2)
	tt.Move(endX, endY)
	tt.Release(endX, endY)
	tt.Frame()

	for i, path := range paths {
		if selected := a.gallerySelected[path]; selected != (i < 3) {
			t.Fatalf("image %d selected=%v, want %v: %+v", i+1, selected, i < 3, a.gallerySelected)
		}
	}
	if !tt.HasText("已选 3 张") || !a.galleryOn {
		t.Fatalf("drag selection did not update gallery: open=%v texts=%v", a.galleryOn, tt.Texts())
	}
}

func TestGalleryLayoutUsesWideResponsiveGrid(t *testing.T) {
	for _, test := range []struct {
		windowW float32
		cols    int
		minTile float32
		maxTile float32
	}{
		{1280, 6, 119, 121},
		{760, 6, 108, 110},
		{320, 2, 120, 122},
	} {
		cols, tile := galleryLayout(galleryPanelWidth(test.windowW))
		if cols != test.cols || tile < test.minTile || tile > test.maxTile {
			t.Errorf("window %.0f: cols=%d tile=%.1f, want cols=%d tile %.1f–%.1f",
				test.windowW, cols, tile, test.cols, test.minTile, test.maxTile)
		}
	}
}
