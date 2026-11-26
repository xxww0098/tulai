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
	if got := s.Get("browser_backend"); got != EngineFingerprint {
		t.Errorf("默认引擎: %s", got)
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
	s.UpdateBrowser(b.ID, true, true)
	bs := s.Browsers()
	if len(bs) != 1 {
		t.Fatalf("浏览器数量: %+v", bs)
	}
	if !bs[0].IsRunning || !bs[0].IsLoggedIn {
		t.Errorf("浏览器状态: %+v", bs[0])
	}
}

func TestOpenCleansLegacyExtensionRowsAndSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.AddManagedBrowser("豆包", "账号1", "", EngineFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO browsers(platform, name, port, user_data_dir, is_running, is_logged_in, ext_account) VALUES('千问','扩展旧号',9399,'',1,1,'legacy-ext')`); err != nil {
		t.Fatal(err)
	}
	s.Set("browserBackend", "extension")
	s.Set("browser_backend", "Camoufox")
	s.Set("extBridgePort", "18765")
	s.Set("extBridgeToken", "old-token")
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows := s.Browsers()
	if len(rows) != 1 || rows[0].ID != local.ID {
		t.Fatalf("legacy extension rows should be cleaned on open: %+v", rows)
	}
	if got := s.Get("browser_backend"); got != EngineFingerprint {
		t.Fatalf("browser_backend = %q, want %q", got, EngineFingerprint)
	}
	for _, key := range []string{"browserBackend", "extBridgePort", "extBridgeToken"} {
		if got := s.Get(key); got != "" {
			t.Fatalf("legacy setting %s = %q, want empty", key, got)
		}
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
	if s.Get("daily_cap") != "0" || s.Get("daily_cap_qianwen") != "100" || s.Get("risky_words") == "" {
		t.Fatalf("defaults cap=%q words=%q", s.Get("daily_cap"), s.Get("risky_words"))
	}
}

func TestOpenRewritesLegacyBrowserNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBrowser("千问", "浏览器3", "/data/qw3"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBrowser("千问", "浏览器4", "/data/qw4"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBrowser("豆包", "豆包1", "/data/db1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBrowser("豆包", "浏览器1", "/data/dbx"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddBrowser("千问", "工作室", "/data/studio"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got := map[string]string{}
	for _, row := range s.Browsers() {
		got[row.UserDataDir] = row.Name
	}
	if got["/data/qw3"] != "千问1" || got["/data/qw4"] != "千问2" || got["/data/db1"] != "豆包1" || got["/data/dbx"] != "豆包2" || got["/data/studio"] != "工作室" {
		t.Fatalf("%v", got)
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
	quota, err := s.AddBrowser("千问", "千问额度", "")
	if err != nil {
		t.Fatal(err)
	}
	s.UpdateBrowser(quota.ID, false, true)
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
	dropped := s.DropUnloggedLocal(confirmedOut)
	if len(dropped) != 2 {
		t.Fatalf("confirmed-out locals = %+v", dropped)
	}
	for _, row := range dropped {
		if row.IsLoggedIn || (row.Platform != "豆包" && row.Platform != "千问") {
			t.Fatalf("dropped = %+v", row)
		}
	}
	for _, row := range s.Browsers() {
		if !row.IsLoggedIn {
			t.Fatalf("unlogged row kept: %+v", row)
		}
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

// 换日之后，昨天记下的额度用尽已经到期：读到就恢复，库里也一起清干净。
func TestExpiredQuotaRecoversOnRead(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, err := s.AddBrowser("豆包", "豆包1", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.UpdateBrowserHealth(b.ID, HealthExhausted, now.Add(-time.Minute).Unix(), "账号额度已用尽: E_QUOTA")
	got := browser(t, s, b.ID)
	if got.Health != HealthReady || got.CoolUntil != 0 || got.LastError != "" || !got.Usable(now) {
		t.Fatalf("expired quota should recover on read: %+v", got)
	}
	var health, last string
	var coolUntil int64
	if err := s.db.QueryRow(`SELECT health, cool_until, last_error FROM browsers WHERE id=?`, b.ID).Scan(&health, &coolUntil, &last); err != nil {
		t.Fatal(err)
	}
	if health != HealthReady || coolUntil != 0 || last != "" {
		t.Fatalf("recovery not saved: health=%q cool_until=%d last_error=%q", health, coolUntil, last)
	}
	// 还没到恢复时间的照旧挡住，错误提示留着。
	until := now.Add(time.Hour).Unix()
	s.UpdateBrowserHealth(b.ID, HealthExhausted, until, "账号额度已用尽: E_QUOTA")
	if held := browser(t, s, b.ID); held.Health != HealthExhausted || held.CoolUntil != until || held.LastError == "" || held.Usable(now) {
		t.Fatalf("active quota hold should stay: %+v", held)
	}
	// 没写恢复时间的挂起要等用户点恢复，读的时候不动。
	s.UpdateBrowserHealth(b.ID, HealthExhausted, 0, "未登录")
	if held := browser(t, s, b.ID); held.Health != HealthExhausted || held.CoolUntil != 0 || held.LastError != "未登录" {
		t.Fatalf("hold without recovery time should stay: %+v", held)
	}
}
