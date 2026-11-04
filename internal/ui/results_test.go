package ui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/tasks"
)

func TestWashResultRowsKeepSourcesAcrossColumns(t *testing.T) {
	a := &App{statePage: pageWash, queue: []tasks.Task{
		{Index: 1, Platform: "豆包", Images: []string{"/a/源图1.jpg"}, Results: []string{"1a.png", "1b.png"}},
		{Index: 2, Platform: "豆包", Images: []string{"/b/源图2.jpg"}},
		{Index: 3, Platform: "豆包", Images: []string{"/c/源图3.jpg"}, Results: []string{"3.png"}},
		{Index: 4, Platform: "豆包", Images: []string{"/d/源图4.jpg"}, Results: []string{"4.png"}},
		{Index: 5, Platform: "豆包", Images: []string{"/e/源图5.jpg"}, Results: []string{"5.png"}},
	}}
	wantSources := []string{"/a/源图1.jpg", "/a/源图1.jpg", "/b/源图2.jpg", "/c/源图3.jpg", "/d/源图4.jpg", "/e/源图5.jpg"}
	wantPaths := []string{"1a.png", "1b.png", "", "3.png", "4.png", "5.png"}
	for _, width := range []float32{320, 1024, 1280} {
		rows, _ := a.buildResultRows(width)
		cols, _ := resultLayout(width)
		if len(rows) != (len(wantPaths)+cols-1)/cols {
			t.Fatalf("width %.0f: got %d rows for %d columns", width, len(rows), cols)
		}
		n := 0
		for _, row := range rows {
			if row.header || len(row.tiles) > cols {
				t.Fatalf("width %.0f: unexpected row %+v", width, row)
			}
			for _, tile := range row.tiles {
				if taskSource(tile.task) != wantSources[n] || tile.path != wantPaths[n] {
					t.Fatalf("width %.0f: tile %d = %q / %q", width, n, taskSource(tile.task), tile.path)
				}
				n++
			}
		}
		if n != len(wantPaths) {
			t.Fatalf("width %.0f: got %d tiles", width, n)
		}
	}
}

func TestWashResultsRenderAcrossAndWrap(t *testing.T) {
	queue := make([]tasks.Task, 6)
	for i := range queue {
		queue[i] = tasks.Task{
			Index: i + 1, Platform: "豆包", Status: tasks.StatusSuccess,
			Images:  []string{fmt.Sprintf("源图%d.jpg", i+1)},
			Results: []string{fmt.Sprintf("结果%d.png", i+1)},
		}
	}
	a := &App{tab: pageWash, statePage: pageWash, queue: queue}
	tt := ui.NewTester(a.view, 1280, 1200)
	first, ok := tt.Find("结果卡结果1.png")
	if !ok {
		t.Fatal("first result missing")
	}
	second, ok := tt.Find("结果卡结果2.png")
	if !ok || second.X <= first.X || second.Y != first.Y {
		t.Fatalf("second result should be beside first: first=%v second=%v", first, second)
	}
	sixth, ok := tt.Find("结果卡结果6.png")
	if !ok || sixth.Y <= first.Y || sixth.X != first.X {
		t.Fatalf("sixth result should wrap: first=%v sixth=%v", first, sixth)
	}
	for i := range queue {
		if !tt.HasText(fmt.Sprintf("源图%d.jpg", i+1)) {
			t.Fatalf("source label %d missing", i+1)
		}
	}
}
