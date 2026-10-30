package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

func TestAntigravityTabShowsAccountAndLogin(t *testing.T) {
	home := filepath.Join(t.TempDir(), "acc1")
	writeAgyHome(t, home, "li@gmail.com")
	now := time.Now()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()).Unix()
	const link = "https://accounts.google.com/o/oauth2/auth?client_id=agy"
	a := &App{
		platformAuto: true,
		stageTab:     2,
		stagePicked:  true,
		prompts:      "一只猫",
		browserRows: []store.Browser{{
			ID: 1, Name: "acc1", Platform: agy.Platform, UserDataDir: home,
			IsLoggedIn: true, Health: store.HealthExhausted, LastError: "额度不足", CoolUntil: midnight,
		}},
		agyLogin: &agyLoginState{url: link, name: "acc1"},
	}
	tt := ui.NewTester(a.view, 1024, 700)
	tt.Advance(300 * time.Millisecond)
	for _, text := range []string{"li@gmail.com", "额度不足", link, "授权码", "提交", "全部账号额度不足，明天 00:00 自动恢复"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q", text)
		}
	}
	if err := tt.Click("打开浏览器"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(50 * time.Millisecond)
	opened := tt.OpenedURLs()
	if len(opened) == 0 || opened[0] != link {
		t.Fatalf("opened %#v", opened)
	}
	if err := tt.Click("复制链接"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != link {
		t.Fatalf("clipboard %q", tt.Clipboard())
	}
	if !tt.HasText("已复制链接") {
		t.Fatal("复制后没有提示")
	}
	const code = "s3cret-code"
	if err := tt.Click("授权码"); err != nil {
		t.Fatal(err)
	}
	tt.Type(code)
	tt.Frame()
	if err := tt.Click("提交"); err != nil {
		t.Fatal(err)
	}
	for _, entry := range logging.Log.Entries() {
		if strings.Contains(entry.Text, code) {
			t.Fatal("日志里出现了授权码")
		}
	}
	buf, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "cli.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buf), code) {
		t.Fatal("账号日志里出现了授权码")
	}
}
