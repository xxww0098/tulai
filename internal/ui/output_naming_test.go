package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestOutputAutoNumberSettingsPersistAndReachTasks(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager(), platformAuto: true}
	a.loadSettings()
	if a.genAutoNumber || a.washAutoNumber || a.numStart != 1 || a.numWidth != 3 {
		t.Fatalf("自动编号默认应关闭，起始 1、补零 3 位: %v %v %v %v", a.genAutoNumber, a.washAutoNumber, a.numStart, a.numWidth)
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("输出"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "生图自动编号", 1280, 900)
	assertVisible(t, tt, "洗图自动编号", 1280, 900)
	if tt.HasText("序号前缀") || tt.HasText("编号起始值") {
		t.Fatal("未勾选时不应显示编号字段")
	}
	if !tt.HasText("文件从 _01 起编") {
		t.Fatal("默认说明应保留")
	}
	if err := tt.Click("生图自动编号"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "序号前缀", 1280, 900)
	assertVisible(t, tt, "编号起始值", 1280, 900)
	assertVisible(t, tt, "补零位数", 1280, 900)
	if !tt.HasText("图001.png") {
		t.Fatal("勾选后应显示自动编号示例")
	}
	if err := tt.Click("序号前缀"); err != nil {
		t.Fatal(err)
	}
	tt.Type("图")
	gen := a.numberingFor(pageHome)
	if !gen.Enabled || gen.Prefix != "图" || gen.Start != 1 || gen.Width != 3 {
		t.Fatalf("生图自动编号设置: %+v", gen)
	}
	if wash := a.numberingFor(pageWash); wash.Enabled || wash.Prefix != "图" {
		t.Fatalf("洗图未勾选应只共用参数: %+v", wash)
	}
	a.saveSettings()
	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if !restored.genAutoNumber || restored.washAutoNumber || restored.numPrefix != "图" || restored.numStart != 1 || restored.numWidth != 3 {
		t.Fatalf("自动编号设置未持久化: %+v", restored.numberingFor(pageHome))
	}
}

func TestWashHintFollowsAutoNumber(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager(), platformAuto: true}
	a.loadSettings()
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("原文件名_01.png") && !tt.HasText("按源文件名保存") {
		t.Fatal("默认洗图提示应说明按源文件名保存")
	}
	a.washAutoNumber = true
	tt.Frame()
	if !tt.HasText("自动编号保存") {
		t.Fatal("勾选洗图自动编号后提示应切换")
	}
}
