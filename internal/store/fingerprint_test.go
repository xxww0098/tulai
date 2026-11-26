package store

import (
	"path/filepath"
	"testing"
)

func TestManagedFingerprintAccountAndProxy(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if s.Get("browser_backend") != EngineFingerprint {
		t.Fatalf("unexpected default browser_backend=%q", s.Get("browser_backend"))
	}

	root := t.TempDir()
	fpDir := filepath.Join(root, "Fingerprint", "account-fp")
	fp, err := s.AddManagedBrowser("千问", "主力账号", fpDir, EngineFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if fp.Engine != EngineFingerprint || fp.UserDataDir != fpDir {
		t.Fatalf("unexpected managed browser: %+v", fp)
	}
	if _, err := s.AddManagedBrowser("未知平台", "bad", fpDir, EngineFingerprint); err == nil {
		t.Fatal("unsupported platform accepted Fingerprint engine")
	}
	if _, err := s.AddManagedBrowser("豆包", "bad-engine", fpDir, "OtherEngine"); err == nil {
		t.Fatal("unsupported engine accepted")
	}

	if err := s.SetBrowserProxy(fp.ID, "http://user:pass@127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBrowserProxy(999, ""); err == nil {
		t.Fatal("unknown account accepted proxy")
	}
	for _, b := range s.Browsers() {
		if b.ID == fp.ID && b.Proxy != "http://user:pass@127.0.0.1:8080" {
			t.Fatalf("proxy not saved: %+v", b)
		}
	}
}
