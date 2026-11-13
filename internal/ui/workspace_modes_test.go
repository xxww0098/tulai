package ui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
)

func TestWorkspaceModesKeepDraftsAndHistory(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	genImage := filepath.Join(dir, "reference.png")
	washImage := filepath.Join(dir, "wash-one.png")
	washCopy := filepath.Join(dir, "wash-two.png")
	for _, path := range []string{genImage, washImage, washCopy} {
		writeTinyPNG(t, path)
	}

	a := &App{store: st, platformAuto: true}
	a.loadSettings()
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("提示词内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("自由生图草稿")
	a.addLoaded(pageHome, loadSourceFiles([]string{genImage}))
	a.saveSettings()
	tt.Frame()
	if a.tab != pageHome {
		t.Fatal("添加参考图不应切换模式")
	}
	if err := tt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	washLabel, ok := tt.Find("洗图指令")
	if !ok {
		t.Fatal("没有找到洗图指令")
	}
	tt.ClickAt(washLabel.X+60, washLabel.Y+washLabel.H+20)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("批量洗图草稿")
	if a.prompts != "批量洗图草稿" {
		t.Fatalf("无法编辑洗图指令: %q", a.prompts)
	}
	a.addLoaded(pageWash, loadSourceFiles([]string{washImage, washCopy}))
	if len(a.sources) != 2 {
		t.Fatalf("洗图应保留不同路径的同内容图片: %+v", a.sources)
	}
	a.saveSettings()

	if err := tt.Click("自由生图"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "自由生图草稿" || len(a.sources) != 1 || a.sources[0].Path != genImage {
		t.Fatalf("生图草稿丢失: %q %+v", a.prompts, a.sources)
	}
	if err := tt.Click("新建任务"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("自由生图草稿") || tt.HasText("wash-one.png") {
		t.Fatalf("生图历史不正确: %v", tt.Texts())
	}
	if err := tt.Click("恢复"); err != nil {
		t.Fatal(err)
	}

	if err := tt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "批量洗图草稿" || len(a.sources) != 2 {
		t.Fatalf("洗图草稿丢失: %q %+v", a.prompts, a.sources)
	}
	if err := tt.Click("新建任务"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("wash-one.png") || tt.HasText("自由生图草稿") {
		t.Fatalf("洗图历史不正确: %v", tt.Texts())
	}
	if err := tt.Click("恢复"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("图片生成"); err != nil {
		t.Fatal(err)
	}
	if a.tab != pageWash || a.prompts != "批量洗图草稿" || len(a.sources) != 2 {
		t.Fatalf("返回页面时未保留洗图模式: tab=%d prompts=%q sources=%+v", a.tab, a.prompts, a.sources)
	}
	a.saveSettings()

	restored := &App{store: st}
	restored.loadSettings()
	gen, wash := restored.stateOf(pageHome), restored.stateOf(pageWash)
	if restored.tab != pageWash || gen.prompts != "自由生图草稿" || len(gen.sources) != 1 ||
		wash.prompts != "批量洗图草稿" || len(wash.sources) != 2 {
		t.Fatalf("重启后草稿或模式未恢复: tab=%d gen=%+v wash=%+v", restored.tab, gen, wash)
	}
}

func TestWorkspaceModeTabsKeyboardAndLayout(t *testing.T) {
	for _, width := range []int{760, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := &App{platformAuto: true}
			tt := ui.NewTester(a.view, width, 900)
			gen := assertVisible(t, tt, "自由生图", float32(width), 900)
			wash := assertVisible(t, tt, "批量洗图", float32(width), 900)
			history := assertVisible(t, tt, "历史", float32(width), 900)
			if gen.X+gen.W > wash.X || wash.X+wash.W > history.X {
				t.Fatalf("模式和历史按钮重叠: gen=%+v wash=%+v history=%+v", gen, wash, history)
			}
			if err := tt.Click("自由生图"); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyRight)
			if a.tab != pageWash || !tt.HasText("洗图指令") {
				t.Fatalf("键盘无法切换到洗图: tab=%d", a.tab)
			}
			tt.Key(0, ui.KeyLeft)
			if a.tab != pageHome || !tt.HasText("提示词内容") {
				t.Fatalf("键盘无法切回生图: tab=%d", a.tab)
			}
			tt.Key(0, ui.KeyRight)
			a.running = true
			tt.Frame()
			if err := tt.Click("自由生图"); err != nil {
				t.Fatal(err)
			}
			if a.tab != pageHome || !tt.HasText("提示词内容") {
				t.Fatalf("运行中无法切换到另一页: tab=%d", a.tab)
			}
			a.running = false
			tt.Frame()
			if err := tt.Click("图片生成"); err != nil {
				t.Fatal(err)
			}
			tt.Key(0, ui.KeyDown)
			if a.tab != pageDedup {
				t.Fatalf("侧栏键盘导航未进入去重: tab=%d", a.tab)
			}
			tt.Key(0, ui.KeyUp)
			if a.tab != pageHome {
				t.Fatalf("侧栏键盘导航未返回图片生成: tab=%d", a.tab)
			}
		})
	}
}
