package ui

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRenameFieldFloatingLabel(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		t.Run(map[bool]string{false: "animated", true: "reduced motion"}[reduce], func(t *testing.T) {
			var prefix, suffix string
			tt := ui.NewTester(func(c *ui.Context) {
				colors := renameColors(false)
				ui.Column(c).Fill().Gap(8).Children(func() {
					renameField(c, colors, "prefix", &prefix, "序号前缀", "序号前面字符", false).FillWidth()
					renameField(c, colors, "suffix", &suffix, "序号后缀", "序号后面字符", false).FillWidth()
				})
			}, 400, 160)
			tt.SetPreferences(ui.Preferences{ReduceMotion: reduce, TextScale: 1})
			labelY := func() float32 {
				t.Helper()
				r, ok := tt.Find("序号前面字符")
				if !ok {
					t.Fatal("label is missing")
				}
				return r.Y
			}
			idle := labelY()
			if err := tt.Click("序号前面字符"); err != nil {
				t.Fatal(err)
			}
			if !tt.Focused("序号前缀") {
				t.Fatal("input did not receive focus")
			}
			if reduce {
				if labelY() >= idle-5 {
					t.Fatal("reduced-motion label did not move immediately")
				}
			} else {
				tt.Advance(motionFast / 3)
				if middle := labelY(); middle >= idle || middle <= idle-10 {
					t.Fatalf("label did not animate between positions: idle=%v middle=%v", idle, middle)
				}
			}
			tt.Advance(motionFast)
			floating := labelY()
			if floating >= idle-5 {
				t.Fatalf("focused label did not move up: idle=%v focused=%v", idle, floating)
			}
			tt.Type("IMG_")
			tt.Key(0, ui.KeyTab)
			tt.Advance(motionFast)
			if !tt.Focused("序号后缀") || prefix != "IMG_" || labelY() != floating {
				t.Fatalf("filled field did not keep its label after blur: prefix=%q, y=%v", prefix, labelY())
			}
			if err := tt.Click("序号前缀"); err != nil {
				t.Fatal(err)
			}
			tt.Key(ui.Cmd, ui.KeyA)
			tt.Key(0, ui.KeyBackspace)
			tt.Key(0, ui.KeyTab)
			tt.Advance(motionFast)
			if prefix != "" || labelY() != idle {
				t.Fatalf("empty field did not restore its label: prefix=%q, y=%v want=%v", prefix, labelY(), idle)
			}
		})
	}
}
