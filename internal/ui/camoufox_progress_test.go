package ui

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestCamoufoxProgressTracksSpeedAndResetsPerStage(t *testing.T) {
	a := &App{camoufoxBusy: true}
	start := time.Unix(1_700_000_000, 0)
	a.applyCamoufoxProgressAt(start, camoufox.Progress{Stage: camoufox.StageBrowser, Text: "下载中", Done: 1 << 20, Total: 100 << 20})
	a.applyCamoufoxProgressAt(start.Add(time.Second), camoufox.Progress{Stage: camoufox.StageBrowser, Text: "下载中", Done: 5 << 20, Total: 100 << 20})
	if a.camoufoxSpeed != 4<<20 {
		t.Fatalf("speed = %d, want %d", a.camoufoxSpeed, 4<<20)
	}
	if a.camoufoxPercent < 0.049 || a.camoufoxPercent > 0.051 {
		t.Fatalf("percent = %v", a.camoufoxPercent)
	}
	a.applyCamoufoxProgressAt(start.Add(2*time.Second), camoufox.Progress{Stage: camoufox.StagePackages, Text: "正在安装启动库"})
	if a.camoufoxPercent != -1 || a.camoufoxSpeed != 0 || a.camoufoxDone != 0 {
		t.Fatalf("new stage kept old numbers: %+v", a)
	}
}

func TestCamoufoxSpeedDropsWhenBytesStop(t *testing.T) {
	a := &App{camoufoxBusy: true, camoufoxSpeed: 100, camoufoxAt: time.Unix(1_700_000_000, 0)}
	a.tickCamoufoxProgress(a.camoufoxAt.Add(3 * time.Second))
	if a.camoufoxSpeed != 0 {
		t.Fatalf("stalled speed = %d", a.camoufoxSpeed)
	}
}

func TestCamoufoxProgressShowsPercentSpeedAndSize(t *testing.T) {
	a := &App{
		splitSz: 380, sourceHeight: 210, wizardOn: true, wizardStep: wizard.StepBrowser, wizardChoice: "豆包",
		camoufoxBusy: true, camoufoxStage: camoufox.StageBrowser,
		camoufoxStatus: "正在下载 Camoufox 内核和指纹数据，首次安装可能需要数分钟…",
		camoufoxDone:   42 << 20, camoufoxTotal: 100 << 20, camoufoxSpeed: 3670016,
	}
	a.camoufoxPercent = float64(a.camoufoxDone) / float64(a.camoufoxTotal)
	a.wizardPlan = wizard.BrowserPlan{Title: "安装 Camoufox", NeedsDownload: true}
	tt := ui.NewTester(a.view, 1000, 720)
	for _, want := range []string{"42%", "3.50 MB/s", "已下载", "42.0 MB / 100 MB", "剩余"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
}

func TestCamoufoxProgressWithoutTotalStillShowsBytes(t *testing.T) {
	a := &App{
		splitSz: 380, sourceHeight: 210, wizardOn: true, wizardStep: wizard.StepBrowser, wizardChoice: "豆包",
		camoufoxBusy: true, camoufoxStage: camoufox.StageBrowser, camoufoxPercent: -1,
		camoufoxStatus: "正在下载 Camoufox 内核…", camoufoxDone: 12 << 20, camoufoxSpeed: 1 << 20,
	}
	a.wizardPlan = wizard.BrowserPlan{Title: "安装 Camoufox", NeedsDownload: true}
	tt := ui.NewTester(a.view, 1000, 720)
	if tt.HasText("%") {
		t.Fatalf("shows a percent without a total: %v", tt.Texts())
	}
	for _, want := range []string{"已下载 12.0 MB", "1.00 MB/s"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
}
