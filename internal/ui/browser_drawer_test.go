package ui

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestBrowserDrawerKeepsInputReadOnlyUntilTakenOver(t *testing.T) {
	e := tasks.NewEngine(nil, nil, nil, "")
	s := &browserDrawerState{
		row: store.Browser{ID: 1}, inputs: make(chan cdp.PageInput, 8),
		width: 1200, height: 800, imageLocal: ui.Rect{X: 10, Y: 20, W: 600, H: 400},
	}
	a := &App{engine: e}
	ev := ui.InputEvent{Kind: ui.InputPointerDown, X: 310, Y: 220, Clicks: 1}
	if a.browserDrawerInput(nil, s, ev) || len(s.inputs) != 0 {
		t.Fatal("preview forwarded a click")
	}
	s.manual = true
	if !a.browserDrawerInput(nil, s, ev) {
		t.Fatal("manual click ignored")
	}
	in := <-s.inputs
	if in.X != 600 || in.Y != 400 || in.Kind != "mousePressed" || in.Buttons != 1 {
		t.Fatalf("scaled click = %+v", in)
	}
	if !a.browserDrawerInput(nil, s, ui.InputEvent{Kind: ui.InputText, Text: "你好，豆包"}) {
		t.Fatal("Chinese input ignored")
	}
	if in = <-s.inputs; in.Kind != "text" || in.Text != "你好，豆包" {
		t.Fatalf("text = %+v", in)
	}
}

func TestBrowserDrawerFitsAndReleasesAccountOnClose(t *testing.T) {
	for _, width := range []int{900, 1280} {
		e := tasks.NewEngine(nil, nil, nil, "")
		ctx, cancel := context.WithCancel(context.Background())
		s := &browserDrawerState{row: store.Browser{ID: 1, Name: "豆包1"}, cancel: cancel, done: make(chan struct{}), inputs: make(chan cdp.PageInput, 8)}
		a := &App{engine: e, browserDrawer: s}
		tt := ui.NewTester(a.view, width, 900)
		if err := tt.Click("接管操作"); err != nil {
			t.Fatal(err)
		}
		if !e.AccountManual(1) || !s.manual {
			t.Fatal("takeover did not reserve the account")
		}
		if !tt.HasText("恢复自动") {
			t.Fatalf("manual action missing: %v", tt.Texts())
		}
		close(s.done)
		if err := tt.Click("收起浏览器"); err != nil {
			t.Fatal(err)
		}
		if a.browserDrawer != nil || ctx.Err() == nil {
			t.Fatal("drawer did not detach")
		}
		// 关闭只释放接管，浏览器进程不在这里停止。
		s.releaseManual(e)
		if e.AccountManual(1) {
			t.Fatal("closed drawer kept the account paused")
		}
	}
}

func TestBrowserDrawerKeyMapping(t *testing.T) {
	if name, code := browserKey(ui.KeyBackspace); name != "Backspace" || code != 8 {
		t.Fatalf("backspace = %q %d", name, code)
	}
	if name, code := browserKey(ui.KeyA); name != "a" || code != 65 {
		t.Fatalf("letter = %q %d", name, code)
	}
	if got := browserModifiers(ui.Ctrl | ui.Super | ui.Shift); got != 14 {
		t.Fatalf("modifiers = %d", got)
	}
}
