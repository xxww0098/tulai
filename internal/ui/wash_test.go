package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestWashPageKeepsItsOwnImagesPromptAndQueue(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), prompts: "画一只猫"}
	a.sources = []sourceImage{{Path: "/gen/a.png"}}
	a.queue = []tasks.Task{{Index: 7}}

	a.tab = pageWash
	a.syncPage()
	if len(a.sources) != 0 || len(a.queue) != 0 {
		t.Fatalf("wash page sees 生图 state: sources=%v queue=%v", a.sources, a.queue)
	}
	if got := a.taskPromptList(); len(got) != 1 || got[0] != DefaultWashPrompt {
		t.Fatalf("wash prompts = %q", got)
	}
	a.sources = []sourceImage{{Path: "/wash/b.png"}}
	a.prompts = "只留主体"

	a.tab = pagePrompts
	a.syncPage()
	if a.prompts != "画一只猫" || len(a.sources) != 1 || a.sources[0].Path != "/gen/a.png" || a.queue[0].Index != 7 {
		t.Fatalf("生图 state lost: prompts=%q sources=%v queue=%v", a.prompts, a.sources, a.queue)
	}
	if w := a.stateOf(pageWash); w.prompts != "只留主体" || w.sources[0].Path != "/wash/b.png" {
		t.Fatalf("parked wash state = %+v", w)
	}
}

func TestAsyncAddLandsOnThePageThatAsked(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, "")}
	a.tab = pageWash
	a.syncPage()
	a.tab = pageHome
	a.syncPage()
	a.addLoaded(pageWash, []sourceImage{{Path: "/x/1.png", Hash: "h1"}})
	if len(a.sources) != 0 {
		t.Fatalf("生图 got the wash image: %v", a.sources)
	}
	if w := a.stateOf(pageWash); len(w.sources) != 1 {
		t.Fatalf("wash sources = %v", w.sources)
	}
}

func TestWashImportKeepsIdenticalFilesAtDifferentPaths(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.png"), filepath.Join(dir, "second.png")
	data := tinyPNG(t)
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loaded := loadSourceFiles([]string{first, second, first})
	a := &App{statePage: pageHome}
	a.addLoaded(pageWash, loaded)
	a.addLoaded(pageWash, loaded)
	got := a.stateOf(pageWash).sources
	if len(got) != 2 || got[0].Path != first || got[1].Path != second {
		t.Fatalf("wash must keep each source path exactly once: %+v", got)
	}
	if len(a.sources) != 0 {
		t.Fatalf("async wash import changed the other page: %+v", a.sources)
	}
}

func TestWashPromptAndImagesPersist(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	img := filepath.Join(dir, "poster.png")
	if err := os.WriteFile(img, tinyPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &App{store: st, engine: tasks.NewEngine(st, nil, nil, "")}
	a.loadSettings()
	if a.stateOf(pageWash).prompts != DefaultWashPrompt {
		t.Fatalf("default wash prompt = %q", a.stateOf(pageWash).prompts)
	}
	a.prompts = "生图提示词"
	a.tab = pageWash
	a.syncPage()
	a.prompts = "去掉文字，白底"
	a.sources = []sourceImage{{Path: img}}
	a.saveSettings()

	b := &App{store: st, engine: tasks.NewEngine(st, nil, nil, "")}
	b.loadSettings()
	if b.tab != pageWash || b.prompts != "去掉文字，白底" || len(b.sources) != 1 || b.sources[0].Path != img {
		t.Fatalf("洗图没有回到上次页面: tab=%d prompts=%q sources=%v", b.tab, b.prompts, b.sources)
	}
	gen := b.stateOf(pageHome)
	if gen.prompts != "生图提示词" || len(gen.sources) != 0 {
		t.Fatalf("生图 reloaded wrong: %q %v", gen.prompts, gen.sources)
	}
}

func TestWashDefaultUpgradeKeepsCustomPrompts(t *testing.T) {
	const oldDefault = "提取图中的商品主体，去掉所有文字、水印和背景装饰，纯白背景，保持主体形状、颜色和细节完全不变"
	for _, tc := range []struct {
		name, saved, want string
	}{
		{"empty", "", DefaultWashPrompt},
		{"old default", oldDefault, DefaultWashPrompt},
		{"edited old default", oldDefault + "，保留标签", oldDefault + "，保留标签"},
		{"custom", "我的洗图指令", "我的洗图指令"},
	} {
		for _, active := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/active=%t", tc.name, active), func(t *testing.T) {
				st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				st.Set("wash_prompt", tc.saved)
				if active {
					if _, err := st.SaveActive(store.Session{Page: store.PageWash, Prompts: tc.saved}, []store.SessionTask{{Prompt: tc.saved}}); err != nil {
						t.Fatal(err)
					}
				}
				a := &App{store: st}
				a.loadSettings()
				if got := a.stateOf(pageWash).prompts; got != tc.want {
					t.Fatalf("wash prompt = %q, want %q", got, tc.want)
				}
				if active && (len(a.stateOf(pageWash).queue) != 1 || a.stateOf(pageWash).queue[0].Prompt != tc.saved) {
					t.Fatal("upgrading the default changed a saved task")
				}
			})
		}
	}
}

func TestExpandImagePathsReadsFolder(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.jpg", "a.png", "note.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	got := expandImagePaths([]string{dir})
	if len(got) != 2 || filepath.Base(got[0]) != "a.png" || filepath.Base(got[1]) != "b.jpg" {
		t.Fatalf("expanded = %v", got)
	}
}

func TestWashManyImagesKeepsPromptAndWholeRows(t *testing.T) {
	for _, tc := range []struct {
		n, height, visibleRows int
	}{
		{3, 700, 1}, {6, 700, 1}, {9, 700, 1},
		{9, 760, 1}, {9, 800, 2},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.n, tc.height), func(t *testing.T) {
			n := tc.n
			sources := make([]sourceImage, n)
			for i := range sources {
				sources[i] = sourceImage{Path: fmt.Sprintf("/wash/t%d.png", i+1)}
			}
			a := &App{
				platformAuto: true,
				tab:          pageWash,
				statePage:    pageWash,
				sources:      sources,
			}
			tt := ui.NewTester(a.view, 1024, tc.height)
			tt.Advance(200 * time.Millisecond)
			for _, label := range []string{"洗图指令", "开始洗图", "运行日志"} {
				r, ok := tt.Find(label)
				if !ok || r.Y < 0 || r.Y+r.H > float32(tc.height) || r.H < 8 {
					t.Fatalf("%s not fully visible: ok=%v rect=%v", label, ok, r)
				}
			}
			logCard, ok := tt.Find("运行日志区")
			if !ok || logCard.H < 140 || logCard.Y < 0 || logCard.Y+logCard.H > float32(tc.height+1) {
				t.Fatalf("运行日志 card %+v", logCard)
			}
			prompt, _ := tt.Find("洗图指令")
			start, _ := tt.Find("开始洗图")
			if prompt.Y >= start.Y {
				t.Fatalf("指令应在开始之上: prompt %v start %v", prompt, start)
			}
			rows := map[int]float32{}
			for i := 1; i <= n; i++ {
				name := fmt.Sprintf("t%d.png", i)
				r, found := tt.Find(name)
				row := (i - 1) / washThumbCols
				if row < tc.visibleRows {
					if !found || r.H < washThumbSize-4 {
						t.Fatalf("row %d thumb %s clipped: ok=%v %+v", row, name, found, r)
					}
					if prev, ok := rows[row]; ok && (r.Y < prev-1 || r.Y > prev+1) {
						t.Fatalf("row %d not aligned: %s y=%v prev=%v", row, name, r.Y, prev)
					}
					rows[row] = r.Y
					continue
				}
				if found && r.H > 4 && r.H < washThumbSize-4 {
					t.Fatalf("thumb %s cut mid-row: %+v", name, r)
				}
				if found && r.H >= washThumbSize-4 {
					t.Fatalf("thumb %s should stay in the scroll, not an extra full row: %+v", name, r)
				}
			}
			if y0, y1 := rows[0], rows[1]; n > washThumbCols && len(rows) > 1 && y1 < y0+washThumbSize-1 {
				t.Fatalf("rows overlap %v %v", y0, y1)
			}
		})
	}
}

func TestWashPageShowsFolderPickerAndNoTextOnlyLink(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), platformAuto: true, tab: pageWash}
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"洗图指令", "选文件夹", "开始洗图", "恢复默认", "添加图片"} {
		assertVisible(t, tt, label, 1280, 840)
	}
	if tt.HasText("不使用图片，只用提示词") {
		t.Fatal("wash page offers prompt-only runs")
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
