package main

import (
	"testing"

	"github.com/xxww0098/realbrowser/fingerprintkernel/profile"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/ui"
)

func TestBrowserServicePersistsAccountFingerprint(t *testing.T) {
	var id int64
	svcs, _, _, data := setupServiceSuite(t, false, func(s *store.Store) {
		row, err := s.AddBrowser("豆包", "设置测试", "")
		if err != nil {
			t.Fatal(err)
		}
		id = row.ID
	})
	dir, err := browser.AccountProfileDir(data, store.Browser{ID: id, Name: "设置测试"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.WriteProfile(dir, profile.FingerprintSpec{Seed: 42}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	prefs := ui.BrowserFingerprintDTO{Lang: "zh-CN", Timezone: "Asia/Shanghai", WindowWidth: 1024, WindowHeight: 768, ColorScheme: "dark"}
	if err := svcs.Browser.SaveAccountFingerprint(ctx, id, prefs); err != nil {
		t.Fatal(err)
	}
	if got, err := svcs.Browser.GetAccountFingerprint(ctx, id); err != nil || got != prefs {
		t.Fatalf("account preferences = %+v, error = %v", got, err)
	}
	bad := prefs
	bad.Timezone = "invalid/timezone"
	if err := svcs.Browser.SaveAccountFingerprint(ctx, id, bad); err == nil {
		t.Fatal("saved invalid timezone")
	}
	if f, err := profile.ReadProfile(dir); err != nil || f.Seed != 42 || f.Timezone != prefs.Timezone {
		t.Fatalf("lost existing profile data: %+v %v", f, err)
	}
	if _, err := svcs.Browser.GetAccountFingerprint(ctx, id+1); err == nil {
		t.Fatal("returned preferences for a missing account")
	}
	svcs.Settings.SetSettingField(ctx, "kernelAutoUpdate", "true")
	if !svcs.Settings.GetSettingsState(ctx).KernelAutoUpdate {
		t.Fatal("auto-update setting was not saved")
	}
}
