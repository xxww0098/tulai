package ui

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

func TestProviderSpecificOutputCapsDefaultsLiveSyncAndResolution(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, err := st.AddBrowser("即梦", "即梦1", filepath.Join(dir, "jm1")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBrowser("Gemini", "Gem1", filepath.Join(dir, "gem1")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBrowser("千问国际", "Intl1", filepath.Join(dir, "qi1")); err != nil {
		t.Fatal(err)
	}

	a := NewApp(st, nil, nil, dir)
	a.StartActor()
	t.Cleanup(func() { a.StopActor() })
	svc := &WorkbenchService{app: a}
	ctx := context.Background()

	// 1. Default per-provider webpage capabilities (即梦 exposes 智能比例 + 21:9 + 1.5K/2K/4K resolution)
	svc.SetPlatformSelected(ctx, "即梦", true)
	wbJM := svc.GetWorkbenchState(ctx)
	if wbJM.OutputPreset != "智能比例" || !wbJM.OutputSupportsAuto || wbJM.OutputAutoLabel != "智能比例" {
		t.Fatalf("unexpected Jimeng default ratio state: preset=%q supportsAuto=%v autoLabel=%q", wbJM.OutputPreset, wbJM.OutputSupportsAuto, wbJM.OutputAutoLabel)
	}
	if !slices.Contains(wbJM.OutputRatios, "21:9") || wbJM.OutputResolution != "2K" || !slices.Equal(wbJM.OutputResolutions, []string{"1.5K", "2K", "4K"}) {
		t.Fatalf("unexpected Jimeng default options: ratios=%v res=%q resolutions=%v", wbJM.OutputRatios, wbJM.OutputResolution, wbJM.OutputResolutions)
	}
	if wbJM.OutputCapsSource != "default" {
		t.Fatalf("expected default caps source before sync, got %q", wbJM.OutputCapsSource)
	}

	// Switch Jimeng resolution to 1.5K and ratio to 21:9
	svc.SetOutputSize(ctx, "21:9（超宽屏）", "", "")
	svc.SetOutputResolution(ctx, "1.5K")
	wbJMAfter := svc.GetWorkbenchState(ctx)
	if wbJMAfter.OutputRatio != "21:9" || wbJMAfter.OutputResolution != "1.5K" {
		t.Fatalf("Jimeng selection failed: ratio=%q res=%q", wbJMAfter.OutputRatio, wbJMAfter.OutputResolution)
	}
	optsJM := a.taskOptions([]string{"即梦"}, []string{"画一只猫"}, nil)
	if optsJM.OutputSize.Ratio != "21:9" || optsJM.OutputSize.Resolution != "1.5K" {
		t.Fatalf("Jimeng taskOptions OutputSize mismatch: %+v", optsJM.OutputSize)
	}

	// 2. Live sync updates webpage capabilities and reconciles removed options cleanly.
	a.syncOutputCapsFn = func(_ context.Context, platform string, _ store.Browser) (store.ProviderOutputCaps, error) {
		switch platform {
		case "即梦":
			return store.ProviderOutputCaps{
				Platform:          "即梦",
				SyncedAt:          time.Now().Unix(),
				Model:             "Seedream 4.7",
				SupportsAuto:      true,
				AutoValue:         "智能",
				AutoLabel:         "智能比例",
				Ratios:            []string{"16:9", "4:3", "1:1", "3:4", "9:16"},
				Resolutions:       []string{"2K", "4K"},
				DefaultResolution: "2K",
			}, nil
		case "Gemini":
			return store.ProviderOutputCaps{
				Platform:     "Gemini",
				SyncedAt:     time.Now().Unix(),
				SupportsAuto: true,
				AutoValue:    "",
				AutoLabel:    "默认比例",
				Ratios:       []string{"1:1", "4:3", "3:4", "16:9", "9:16", "3:2", "21:9"},
			}, nil
		case "千问国际":
			return store.ProviderOutputCaps{
				Platform:     "千问国际",
				SyncedAt:     time.Now().Unix(),
				SupportsAuto: true,
				AutoValue:    "auto",
				AutoLabel:    "Auto",
				Ratios:       []string{"1:1", "16:9", "3:4"},
			}, nil
		default:
			return store.DefaultProviderOutputCaps(platform), nil
		}
	}

	if err := svc.SyncOutputCaps(ctx, "即梦"); err != nil {
		t.Fatalf("SyncOutputCaps(即梦) failed: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		wb := svc.GetWorkbenchState(ctx)
		if wb.OutputCapsSource == "live" && !wb.OutputCapsSyncing {
			// Previously selected 21:9 & 1.5K are no longer in live caps -> must reconcile cleanly to 智能比例 & 2K!
			if wb.OutputPreset != "智能比例" || wb.OutputRatio != "" || wb.OutputResolution != "2K" {
				t.Fatalf("expected reconciliation after live sync, got preset=%q ratio=%q res=%q", wb.OutputPreset, wb.OutputRatio, wb.OutputResolution)
			}
			if !slices.Equal(wb.OutputResolutions, []string{"2K", "4K"}) {
				t.Fatalf("unexpected live Jimeng resolutions: %v", wb.OutputResolutions)
			}
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if wb := svc.GetWorkbenchState(ctx); wb.OutputCapsSource != "live" {
		t.Fatal("SyncOutputCaps(即梦) did not complete in time")
	}

	// 3. Gemini live sync unlocks newly supported webpage ratio 3:2 and persists across App reload!
	svc.SetPlatformSelected(ctx, "Gemini", true)
	if err := svc.SyncOutputCaps(ctx, "Gemini"); err != nil {
		t.Fatalf("SyncOutputCaps(Gemini) failed: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		wb := svc.GetWorkbenchState(ctx)
		if wb.OutputCapsSource == "live" && !wb.OutputCapsSyncing {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	svc.SetOutputSize(ctx, "3:2（相机横）", "", "")
	wbGem := svc.GetWorkbenchState(ctx)
	if wbGem.OutputRatio != "3:2" || !slices.Contains(wbGem.OutputRatios, "21:9") {
		t.Fatalf("Gemini failed to adopt live-probed ratios: ratio=%q ratios=%v", wbGem.OutputRatio, wbGem.OutputRatios)
	}
	optsGem := a.taskOptions([]string{"Gemini"}, []string{"cat"}, nil)
	if err := optsGem.OutputSize.ValidateFor("Gemini"); err != nil {
		t.Fatalf("Gemini live-probed 3:2 failed validation: %v", err)
	}

	// Reload from SQLite: cached live caps & selected 3:2 survive restart!
	reloaded := NewApp(st, nil, nil, dir)
	if got := reloaded.outputSizeFor("Gemini"); got.Ratio != "3:2" || !slices.Contains(got.AllowedRatios, "3:2") {
		t.Fatalf("reloaded Gemini output size lost live caps: %+v", got)
	}

	// 4. 千问国际 live sync dynamically enables webpage Auto option!
	svc.SetPlatformSelected(ctx, "千问国际", true)
	if err := svc.SyncOutputCaps(ctx, "千问国际"); err != nil {
		t.Fatalf("SyncOutputCaps(千问国际) failed: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		wb := svc.GetWorkbenchState(ctx)
		if wb.OutputCapsSource == "live" && !wb.OutputCapsSyncing {
			if !wb.OutputSupportsAuto || wb.OutputAutoLabel != "Auto" || wb.OutputPresets[0] != "Auto" {
				t.Fatalf("expected live QwenIntl Auto option, got supportsAuto=%v autoLabel=%q presets=%v", wb.OutputSupportsAuto, wb.OutputAutoLabel, wb.OutputPresets)
			}
			break
		}
		time.Sleep(15 * time.Millisecond)
	}

	// 5. Missing account returns actionable error.
	if err := svc.SyncOutputCaps(ctx, "豆包"); err == nil {
		t.Fatal("expected SyncOutputCaps(豆包) to fail cleanly when no Doubao account is configured")
	}
}
