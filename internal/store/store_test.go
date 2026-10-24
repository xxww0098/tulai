package store

import (
	"path/filepath"
	"testing"
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
	s.UpdateBrowser(b.ID, true, true)
	bs := s.Browsers()
	if len(bs) != 1 || !bs[0].IsRunning || !bs[0].IsLoggedIn {
		t.Errorf("浏览器状态: %+v", bs)
	}
}
