package imageedit

import (
	"strings"
	"testing"
)

func TestNormalizeFormatsAndExtension(t *testing.T) {
	o, err := Normalize(Options{Format: "jpg", CropWidth: 640})
	if err != nil {
		t.Fatal(err)
	}
	if o.Format != "jpeg" {
		t.Fatalf("format = %q, want jpeg", o.Format)
	}
	if got := OutputExtension(o, "png"); got != "jpg" {
		t.Fatalf("extension = %q, want jpg", got)
	}
	if !Enabled(Options{Format: "jpg", CropWidth: 640}) {
		t.Fatal("expected enabled")
	}

	disabled, err := Normalize(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Format != "original" {
		t.Fatalf("format = %q, want original", disabled.Format)
	}
	if Enabled(Options{}) {
		t.Fatal("empty options should not be enabled")
	}
	if got := OutputExtension(disabled, ".jpeg"); got != "jpg" {
		t.Fatalf("extension = %q, want jpg", got)
	}
	if got := OutputExtension(disabled, "webp"); got != "webp" {
		t.Fatalf("extension = %q, want webp", got)
	}
}

func TestNormalizeRejectsInvalidValues(t *testing.T) {
	cases := []Options{
		{Format: "bmp"},
		{CropWidth: -1},
		{CropPosition: "middle"},
		{Rotate: 45},
		{JPEGQuality: 101},
		{Brightness: 200},
		{Contrast: -101},
		{Saturation: 101},
		{Blur: 0.1},
		{Blur: 60},
		{CropLeft: Int(1)},
	}
	for _, o := range cases {
		if _, err := Normalize(o); err == nil {
			t.Fatalf("Normalize(%+v) should fail", o)
		}
	}
}

func TestNormalizeCropRatioAndModes(t *testing.T) {
	o, err := Normalize(Options{CropRatio: "16.0:9.0", ResizeMode: "contain", JPEGQuality: 75})
	if err != nil {
		t.Fatal(err)
	}
	if o.CropRatio != "16:9" {
		t.Fatalf("ratio = %q", o.CropRatio)
	}
	if o.ResizeMode != "contain" {
		t.Fatalf("mode = %q", o.ResizeMode)
	}
	if o.JPEGQuality != 75 {
		t.Fatalf("quality = %d", o.JPEGQuality)
	}
	if _, err := Normalize(Options{CropRatio: "16x9"}); err == nil || !strings.Contains(err.Error(), "crop_ratio") {
		t.Fatalf("ratio error = %v", err)
	}
}

func TestEnabledIgnoresDefaultQuality(t *testing.T) {
	if Enabled(Options{JPEGQuality: 90}) {
		t.Fatal("quality 90 is the default and should not enable")
	}
	if !Enabled(Options{JPEGQuality: 80}) {
		t.Fatal("quality 80 should enable")
	}
	if !Enabled(Options{CropLeft: Int(0), CropTop: Int(0)}) {
		t.Fatal("explicit crop offset should enable")
	}
}

func TestContainedSize(t *testing.T) {
	cases := []struct{ w, h, mw, mh, wantW, wantH int }{
		{8, 4, 3, 3, 3, 2},
		{8, 4, 4, 0, 4, 2},
		{8, 4, 20, 20, 8, 4},
	}
	for _, c := range cases {
		w, h := containedSize(c.w, c.h, c.mw, c.mh)
		if w != c.wantW || h != c.wantH {
			t.Errorf("containedSize(%d,%d,%d,%d) = %dx%d, want %dx%d", c.w, c.h, c.mw, c.mh, w, h, c.wantW, c.wantH)
		}
	}
}
