package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCamoufoxMigrationPreservesAccountsAndOldProfiles(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := t.TempDir()
	marker := filepath.Join(old, "Cookies")
	if err := os.WriteFile(marker, []byte("old-login"), 0600); err != nil {
		t.Fatal(err)
	}
	local, err := s.AddBrowser("豆包", "work", old)
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateBrowser(local.ID, true, true)
	external, err := s.SaveLoggedInExtension("千问", "external-profile", "external")
	if err != nil {
		t.Fatal(err)
	}
	agy, err := s.AddBrowser("Antigravity", "agy", old)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := s.PrepareCamoufoxProfiles(root); err != nil {
		t.Fatal(err)
	}
	rows := s.Browsers()
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	for _, b := range rows {
		switch b.ID {
		case local.ID:
			if b.UserDataDir == old || b.IsRunning || b.IsLoggedIn || b.Name != local.Name || b.Port != local.Port {
				t.Fatalf("bad migration: %+v", b)
			}
			if filepath.Dir(b.UserDataDir) != filepath.Join(root, "Camoufox") {
				t.Fatal(b.UserDataDir)
			}
		case external.ID:
			if b.ExtAccount != external.ExtAccount || !b.IsLoggedIn {
				t.Fatal(b)
			}
		case agy.ID:
			if b.UserDataDir != old {
				t.Fatal(b)
			}
		}
	}
	var legacy string
	if err := s.db.QueryRow("SELECT legacy_user_data_dir FROM browsers WHERE id=?", local.ID).Scan(&legacy); err != nil || legacy != old {
		t.Fatalf("legacy=%s %v", legacy, err)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "old-login" {
		t.Fatal("old data changed", err)
	}
	s.UpdateBrowser(local.ID, false, true)
	if err := s.SetBrowserProxy(local.ID, "http://user:pass@127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBrowserProxy(external.ID, ""); err == nil {
		t.Fatal("external account accepted local proxy")
	}
	if err := s.SetBrowserProxy(999, ""); err == nil {
		t.Fatal("unknown account accepted proxy")
	}
	if err := s.PrepareCamoufoxProfiles(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, b := range s.Browsers() {
		if b.ID == local.ID && (!b.IsLoggedIn || b.Proxy != "http://user:pass@127.0.0.1:8080" || filepath.Dir(b.UserDataDir) != filepath.Join(root, "Camoufox")) {
			t.Fatalf("migration repeated: %+v", b)
		}
	}
	if s.Get("browser_backend") != "Camoufox" || s.Get("browserBackend") != "camoufox" {
		t.Fatal("backend migration missing")
	}
}
