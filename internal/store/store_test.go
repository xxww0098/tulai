package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAndDefaults(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.Get("send_strategy"); got != "顺序循环" {
		t.Errorf("默认发送策略: %s", got)
	}
	if n := s.GetInt("concurrency"); n != 0 {
		t.Errorf("默认并发: %d，0 表示自动", n)
	}
	if got := s.Get("browserBackend"); got != "ego" {
		t.Errorf("默认控制面: %s", got)
	}
	if got := s.Get("extBridgePort"); got != "18765" {
		t.Errorf("默认扩展端口: %s", got)
	}
	if got := s.Get("extBridgeToken"); got != "dev-token" {
		t.Errorf("默认扩展令牌: %s", got)
	}
	s.Set("browserBackend", "extension")
	if got := s.Get("browserBackend"); got != "extension" {
		t.Errorf("写入控制面: %s", got)
	}
	s.SetInt("concurrency", 4)
	if n := s.GetInt("concurrency"); n != 4 {
		t.Errorf("写入后并发: %d", n)
	}
	b, err := s.AddBrowser("豆包", "测试浏览器", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.Port < 9333 {
		t.Errorf("端口分配: %d", b.Port)
	}
	s.UpdateBrowserHealth(b.ID, HealthExhausted, 0, "未登录")
	if got := s.Browsers()[0]; got.Usable(time.Now()) || got.Health != HealthExhausted || got.LastError != "未登录" {
		t.Fatalf("health: %+v", got)
	}
	s.ResetBrowserHealth(b.ID)
	if !s.Browsers()[0].Usable(time.Now()) {
		t.Fatal("reset should make the account usable")
	}
	ext, err := s.UpsertExtensionAccount("千问", "profile-1", "小号")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.UpsertExtensionAccount("千问", "profile-1", "小号")
	if err != nil || again.ID != ext.ID || again.ExtAccount != "profile-1" {
		t.Fatalf("upsert: %+v %+v err=%v", ext, again, err)
	}
	s.UpdateBrowser(b.ID, true, true)
	bs := s.Browsers()
	if len(bs) != 2 {
		t.Fatalf("浏览器数量: %+v", bs)
	}
	for _, row := range bs {
		if row.ID == b.ID && (!row.IsRunning || !row.IsLoggedIn) {
			t.Errorf("浏览器状态: %+v", row)
		}
	}
}

func TestDeleteByExtensionAccountLeavesLocalRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	local, err := s.AddBrowser("豆包", "账号1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertExtensionAccount("豆包", "profile-a", "扩展甲"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertExtensionAccount("千问", "profile-a", "扩展甲"); err != nil {
		t.Fatal(err)
	}
	s.DeleteByExtensionAccount("")
	s.DeleteByExtensionAccount("  ")
	if rows := s.Browsers(); len(rows) != 3 {
		t.Fatalf("empty account id must not delete rows: %+v", rows)
	}
	s.DeleteByExtensionAccount("profile-a")
	rows := s.Browsers()
	if len(rows) != 1 || rows[0].ID != local.ID || rows[0].ExtAccount != "" {
		t.Fatalf("local account should remain: %+v", rows)
	}
}

func TestAddUseResetsOnNewDay(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, err := s.AddBrowser("豆包", "甲", "")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 10, 28, 15, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)
	s.AddUse(b.ID, yesterday)
	s.AddUse(b.ID, yesterday)
	got := browser(t, s, b.ID)
	if got.UsedToday(today) != 0 || got.UsedToday(yesterday) != 2 {
		t.Fatalf("rollover = today %d yesterday %d", got.UsedToday(today), got.UsedToday(yesterday))
	}
	s.AddUse(b.ID, today)
	got = browser(t, s, b.ID)
	if got.UsedToday(today) != 1 {
		t.Fatalf("today = %d", got.UsedToday(today))
	}
	if s.Get("daily_cap") != "20" || s.Get("risky_words") == "" {
		t.Fatalf("defaults cap=%q words=%q", s.Get("daily_cap"), s.Get("risky_words"))
	}
}

func TestRenameBrowserKeepsProfileDir(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, err := s.AddBrowser("豆包", "账号1", "/data/账号1")
	if err != nil {
		t.Fatal(err)
	}
	s.RenameBrowser(b.ID, "  ")
	s.RenameBrowser(b.ID, "工作室")
	got := browser(t, s, b.ID)
	if got.Name != "工作室" || got.UserDataDir != "/data/账号1" {
		t.Fatalf("renamed = %+v", got)
	}
	if s.Get("proxy_mode") != "system" {
		t.Fatalf("proxy mode = %q", s.Get("proxy_mode"))
	}
}

func TestOnlyLoggedInAccountsAreKept(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var confirmedOut []int64
	for _, platform := range []string{"豆包", "千问"} {
		junk, err := s.AddBrowser(platform, platform+"未登录", "")
		if err != nil {
			t.Fatal(err)
		}
		confirmedOut = append(confirmedOut, junk.ID)
		stale, err := s.AddBrowser(platform, platform+"过期", "")
		if err != nil {
			t.Fatal(err)
		}
		saved, err := s.AddBrowser(platform, platform+"已登录", "")
		if err != nil {
			t.Fatal(err)
		}
		s.UpdateBrowser(saved.ID, false, true)
		if _, err := s.UpsertExtensionAccount(platform, "bare-"+platform, ""); err != nil {
			t.Fatal(err)
		}
		ext, err := s.SaveLoggedInExtension(platform, "live-"+platform, "")
		if err != nil {
			t.Fatal(err)
		}
		if !ext.IsLoggedIn || ext.Health != HealthReady || ext.Platform != platform {
			t.Fatalf("saved extension = %+v", ext)
		}
		s.UpdateBrowserHealth(stale.ID, HealthExhausted, 0, "未登录")
		if !s.ConfirmLogin(stale.ID) {
			t.Fatal("confirm")
		}
		got := browser(t, s, stale.ID)
		if !got.IsLoggedIn || !got.Usable(time.Now()) || got.Health != HealthReady || got.LastError != "" {
			t.Fatalf("%s login did not clear 未登录: %+v", platform, got)
		}
		if browser(t, s, junk.ID).IsLoggedIn || browser(t, s, saved.ID).Name == "" {
			t.Fatalf("setup junk=%+v saved=%+v", browser(t, s, junk.ID), browser(t, s, saved.ID))
		}
	}
	quota, err := s.SaveLoggedInExtension("千问", "quota", "扩展千问")
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour).Unix()
	s.UpdateBrowserHealth(quota.ID, HealthExhausted, until, "额度已用尽")
	if s.ConfirmLogin(quota.ID) {
		t.Fatal("quota hold is not a login")
	}
	held := browser(t, s, quota.ID)
	if !held.IsLoggedIn || held.LastError != "额度已用尽" || held.CoolUntil != until {
		t.Fatalf("quota should stay: %+v", held)
	}
	if dropped := s.DropUnloggedLocal(nil); len(dropped) != 0 {
		t.Fatalf("stale flag deleted %+v", dropped)
	}
	var bare int
	for _, row := range s.Browsers() {
		if row.ExtAccount == "bare-豆包" || row.ExtAccount == "bare-千问" {
			if row.IsLoggedIn {
				t.Fatalf("bare extension marked logged in: %+v", row)
			}
			bare++
		}
	}
	if bare != 2 {
		t.Fatalf("unconfirmed extension rows = %d %+v", bare, s.Browsers())
	}
	dropped := s.DropUnloggedLocal(confirmedOut)
	if len(dropped) != 2 {
		t.Fatalf("confirmed-out locals = %+v", dropped)
	}
	for _, row := range dropped {
		if row.IsLoggedIn || (row.Platform != "豆包" && row.Platform != "千问") {
			t.Fatalf("dropped = %+v", row)
		}
	}
	bare = 0
	for _, row := range s.Browsers() {
		if row.ExtAccount == "bare-豆包" || row.ExtAccount == "bare-千问" {
			bare++
		}
	}
	if bare != 2 {
		t.Fatalf("confirmed-out local drop removed extensions: %+v", s.Browsers())
	}
	s.DropExtensionAccount("豆包", "bare-豆包")
	s.DropExtensionAccount("千问", "bare-千问")
	for _, row := range s.Browsers() {
		if !row.IsLoggedIn {
			t.Fatalf("unlogged row kept: %+v", row)
		}
		if row.ExtAccount != "" && row.Platform != "豆包" && row.Platform != "千问" {
			t.Fatalf("extension platform = %+v", row)
		}
	}
	var doubaoExt, qianwenExt bool
	for _, row := range s.Browsers() {
		if row.ExtAccount == "live-豆包" {
			doubaoExt = true
		}
		if row.ExtAccount == "live-千问" {
			qianwenExt = true
		}
		if row.ExtAccount == "bare-豆包" || row.ExtAccount == "bare-千问" {
			t.Fatalf("connected-only extension kept: %+v", row)
		}
	}
	if !doubaoExt || !qianwenExt {
		t.Fatalf("logged-in extension rows = %+v", s.Browsers())
	}
}

func browser(t *testing.T, s *Store, id int64) Browser {
	t.Helper()
	for _, b := range s.Browsers() {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("missing browser %d", id)
	return Browser{}
}
