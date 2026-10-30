package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestRailIconsHaveTooltipsAndActions(t *testing.T) {
	mygo.App.SetVersion("0.0.0-test")
	t.Cleanup(func() { mygo.App.SetVersion("") })

	a := &App{platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(80 * time.Millisecond)

	steps := []struct {
		tip  string
		prep func()
		done func()
	}{
		{
			tip:  "回到生图",
			prep: func() { a.tab = pageSettings; tt.Frame() },
			done: func() {
				if a.tab != pageHome {
					t.Fatalf("logo tab=%d", a.tab)
				}
			},
		},
		{
			tip:  "生图",
			prep: func() { a.tab = pageSettings; tt.Frame() },
			done: func() {
				if a.tab != pageHome {
					t.Fatalf("生图 tab=%d", a.tab)
				}
			},
		},
		{
			tip:  "洗图",
			prep: func() { a.tab = pageHome; tt.Frame() },
			done: func() {
				if a.tab != pageWash {
					t.Fatalf("洗图 tab=%d", a.tab)
				}
			},
		},
		{
			tip:  "浏览器",
			prep: func() { a.tab = pageHome; tt.Frame() },
			done: func() {
				if a.tab != pageBrowser {
					t.Fatalf("浏览器 tab=%d", a.tab)
				}
			},
		},
		{
			tip:  "提示词",
			prep: func() { a.tab = pageHome; tt.Frame() },
			done: func() {
				if a.tab != pagePrompts {
					t.Fatalf("提示词 tab=%d", a.tab)
				}
			},
		},
		{
			tip:  "设置",
			prep: func() { a.tab = pageHome; tt.Frame() },
			done: func() {
				if a.tab != pageSettings {
					t.Fatalf("设置 tab=%d", a.tab)
				}
			},
		},
		{
			tip:  "设置向导",
			prep: func() { a.tab = pageHome; a.wizardOn = false; tt.Frame() },
			done: func() {
				if !a.wizardOn {
					t.Fatal("设置向导没有打开")
				}
				a.wizardOn = false
				tt.Frame()
			},
		},
		{
			tip:  "检查更新",
			prep: func() { a.wizardOn = false; tt.Frame() },
			done: func() {
				if a.updateChecks != 1 {
					t.Fatalf("检查更新没有执行: %d", a.updateChecks)
				}
			},
		},
	}
	for _, step := range steps {
		step.prep()
		before := countExact(tt, step.tip)
		r, ok := tt.Find(step.tip)
		if !ok {
			t.Fatalf("找不到 %q", step.tip)
		}
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Advance(700 * time.Millisecond)
		if got := countExact(tt, step.tip); got != before+1 {
			t.Fatalf("%s 悬停没有出现提示: before=%d after=%d", step.tip, before, got)
		}
		if err := tt.Click(step.tip); err != nil {
			t.Fatal(err)
		}
		step.done()
	}
}

func countExact(tt *ui.Tester, s string) int {
	n := 0
	for _, text := range tt.Texts() {
		if text == s {
			n++
		}
	}
	return n
}
