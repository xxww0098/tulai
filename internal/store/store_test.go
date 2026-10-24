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
