package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestWashImageActionsAccessibleAndUsable(t *testing.T) {
	for _, tc := range []struct {
		width int
		dark  bool
	}{{1280, false}, {760, true}} {
		t.Run(fmt.Sprintf("width-%d-dark-%t", tc.width, tc.dark), func(t *testing.T) {
			a, _ := galleryTestApp(t, 2)
			originalSources := append([]sourceImage(nil), a.sources...)
			tt := ui.NewTesterWith(a.view, tc.width, 900, ui.TesterOptions{Dark: tc.dark})
			labels := []string{"图库", "添加图片", "选文件夹", "清空"}
			var previous ui.Rect
			for i, label := range labels {
				info, ok := tt.AccessByRole(ui.RoleButton, label)
				if !ok || info.Disabled || !info.Focusable || info.Description != label {
					t.Fatalf("%s: accessible button = %+v, found=%v", label, info, ok)
				}
				r := assertVisible(t, tt, label, float32(tc.width), 900)
				if i > 0 && previous.X+previous.W > r.X {
					t.Fatalf("image actions overlap: %s at %+v after %+v", label, r, previous)
				}
				previous = r
			}
			// The label is also shown when the pointer rests on the icon.
			r, _ := tt.Find("添加图片")
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Advance(time.Second)
			matches := 0
			for _, text := range tt.Texts() {
				if text == "添加图片" {
					matches++
				}
			}
			if matches < 2 {
				t.Fatalf("add-image tooltip did not appear: %v", tt.Texts())
			}

			a.running = true
			tt.Frame()
			for _, label := range labels {
				info, ok := tt.AccessByRole(ui.RoleButton, label)
				if !ok || !info.Disabled {
					t.Fatalf("running: %s should be disabled, got %+v", label, info)
				}
			}
			r, _ = tt.Find("清空")
			tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
			if len(a.sources) != 2 {
				t.Fatal("disabled clear button removed images")
			}

			a.running = false
			tt.Frame()
			if err := tt.Click("清空"); err != nil {
				t.Fatal(err)
			}
			if len(a.sources) != 0 {
				t.Fatal("clear button did not remove images")
			}
			for _, label := range []string{"图库", "清空"} {
				if _, ok := tt.AccessByRole(ui.RoleButton, label); ok {
					t.Fatalf("%s should be hidden when there are no images", label)
				}
			}
			for _, label := range []string{"添加图片", "选文件夹"} {
				if _, ok := tt.AccessByRole(ui.RoleButton, label); !ok {
					t.Fatalf("%s should remain available without images", label)
				}
			}
			a.sources = originalSources
			tt.Frame()
			if err := tt.Click("图库"); err != nil {
				t.Fatal(err)
			}
			if !a.galleryOn {
				t.Fatal("gallery button did not work after images returned")
			}
		})
	}
}
