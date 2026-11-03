package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestExistingAccountsDefaultToEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE browsers (id INTEGER PRIMARY KEY, platform TEXT NOT NULL, name TEXT NOT NULL,
		port INTEGER NOT NULL, user_data_dir TEXT, is_running INTEGER DEFAULT 0, is_logged_in INTEGER DEFAULT 0);
		INSERT INTO browsers(id, platform, name, port, is_logged_in) VALUES(1, '豆包', '主力', 9333, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Browsers(); len(got) != 1 || got[0].Disabled || !got[0].IsLoggedIn {
		t.Fatalf("旧账号升级后状态错误: %+v", got)
	}
}

func TestAccountDisabledPersistsWithoutChangingHealth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AddBrowser("豆包", "主力", "/data/primary")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddBrowser("豆包", "备用", "/data/other")
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateBrowser(b.ID, true, true)
	s.UpdateBrowserHealth(b.ID, HealthCooling, 123, "等待恢复")
	if err := s.SetBrowserDisabled(b.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := browser(t, s, b.ID)
	if !got.Disabled || !got.IsLoggedIn || got.Health != HealthCooling || got.CoolUntil != 123 || got.LastError != "等待恢复" {
		t.Fatalf("保存后账号状态变化: %+v", got)
	}
	if browser(t, s, other.ID).Disabled {
		t.Fatal("其他账号受到影响")
	}
	if err := s.SetBrowserDisabled(b.ID, false); err != nil || browser(t, s, b.ID).Disabled {
		t.Fatalf("重新启用失败: %v", err)
	}
	if err := s.SetBrowserDisabled(9999, true); err == nil {
		t.Fatal("不存在的账号应返回错误")
	}
}
