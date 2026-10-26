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
	if n := s.GetInt("concurrency"); n != 1 {
		t.Errorf("默认并发: %d", n)
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
