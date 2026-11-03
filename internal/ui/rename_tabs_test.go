package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestRenameTabsCapsuleMotion(t *testing.T) {
	for _, reduce := range []bool{false, true} {
		t.Run(map[bool]string{false: "animated", true: "reduced motion"}[reduce], func(t *testing.T) {
			a := &App{}
			tt := ui.NewTester(func(c *ui.Context) { a.renameTabs(c, renameColors(false)) }, 500, 42)
			tt.SetPreferences(ui.Preferences{ReduceMotion: reduce, TextScale: 1})
			pill := tt.Image().RGBAAt(50, 9)
			if pill == tt.Image().RGBAAt(450, 9) {
				t.Fatal("selected capsule is not visible")
			}
			center := func() int {
				sum, count := 0, 0
				img := tt.Image()
				for x := 0; x < 500; x++ {
					if img.RGBAAt(x, 9) == pill {
						sum += x
						count++
					}
				}
				if count == 0 {
					t.Fatal("selected capsule disappeared")
				}
				return sum / count
			}
			if err := tt.Click("手动编辑"); err != nil {
				t.Fatal(err)
			}
			if reduce {
				if x := center(); x < 430 {
					t.Fatalf("reduced-motion capsule did not move immediately: %d", x)
				}
			} else {
				tt.Advance(60 * time.Millisecond)
				if x := center(); x <= 100 || x >= 400 {
					t.Fatalf("capsule did not slide between tabs: %d", x)
				}
			}
			if err := tt.Click("智能重命名"); err != nil {
				t.Fatal(err)
			}
			tt.Advance(motionUI)
			if x := center(); a.fileRename.mode != 1 || x < 130 || x > 170 {
				t.Fatalf("capsule did not reach the newly selected tab: mode %d, center %d", a.fileRename.mode, x)
			}
			tt.Key(0, ui.KeyRight)
			tt.Advance(motionUI)
			if x := center(); a.fileRename.mode != 2 || x < 230 || x > 270 {
				t.Fatalf("keyboard selection did not move capsule: mode %d, center %d", a.fileRename.mode, x)
			}
		})
	}
}
