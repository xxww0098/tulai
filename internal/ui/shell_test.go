package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/logging"
)

func TestTopbarLogPopoverOpensFromTopRightAcrossPages(t *testing.T) {
	prev := logging.Log
	logging.Log = logging.New(16)
	t.Cleanup(func() { logging.Log = prev })
	logging.Log.Info("SaaS 顶栏日志测试")

	a := &App{platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(120 * time.Millisecond)

	logBtn := assertVisible(t, tt, "运行日志", 1280, 800)
	if logBtn.X <= 640 || logBtn.Y+logBtn.H > topbarH+2 {
		t.Fatalf("运行日志 button not anchored in top-right topbar: %v", logBtn)
	}

	// 未展开日志时不应在工作台左栏占高度。
	if _, open := tt.Find("运行日志区"); open {
		t.Fatal("log popover should be closed initially")
	}

	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(150 * time.Millisecond)
	if err := tt.Click("运行日志"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(150 * time.Millisecond)

	pop := assertVisible(t, tt, "运行日志区", 1280, 800)
	assertVisible(t, tt, "SaaS 顶栏日志测试", 1280, 800)
	if pop.X+pop.W > 1280 || pop.Y < topbarH {
		t.Fatalf("log popover outside expected top-right bounds: %v", pop)
	}
	if err := tt.Click("运行日志"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(120 * time.Millisecond)
	if _, open := tt.Find("运行日志区"); open {
		t.Fatal("clicking 运行日志 again should close popover")
	}
}

func TestSidebarCollapseExpandsStageWidth(t *testing.T) {
	a := &App{platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(120 * time.Millisecond)

	stageExpanded := assertVisible(t, tt, "账号表格", 1280, 800)
	if err := tt.Click("收起侧栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(120 * time.Millisecond)
	stageCollapsed := assertVisible(t, tt, "账号表格", 1280, 800)
	if stageCollapsed.W <= stageExpanded.W+50 {
		t.Fatalf("collapsing sidebar did not widen stage: before=%v after=%v", stageExpanded, stageCollapsed)
	}
	if err := tt.Click("展开侧栏"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(120 * time.Millisecond)
	stageAgain := assertVisible(t, tt, "账号表格", 1280, 800)
	if stageAgain.W != stageExpanded.W {
		t.Fatalf("re-expanding sidebar did not restore stage width: before=%v after=%v", stageExpanded, stageAgain)
	}
}
