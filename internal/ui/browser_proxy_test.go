package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestAccountProxyValidationAndIsolation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, name := range []string{"工作号", "备用号"} {
		if _, err := st.AddBrowser("豆包", name, ""); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{store: st, tab: pageBrowser, browserSel: 0, browserBackend: browser.KindCamoufox}
	tt := ui.NewTester(a.view, 1280, 900)
	for _, tc := range []struct{ input, note, saved string }{
		{"not a proxy", "代理地址无效，请填写协议、主机和端口", ""},
		{"socks5://user:pass@127.0.0.1:1080", "SOCKS5 认证不受支持，请改用 HTTP 代理", ""},
		{"http://127.0.0.1:7890", "已保存，下次打开此账号生效", "http://127.0.0.1:7890"},
	} {
		a.accountProxy = tc.input
		tt.Frame()
		if err := tt.Click("保存账号代理"); err != nil {
			t.Fatal(err)
		}
		rows := st.Browsers()
		if rows[0].Proxy != tc.saved || rows[1].Proxy != "" || !tt.HasText(tc.note) {
			t.Fatalf("input=%q rows=%+v note=%q", tc.input, rows, a.accountProxyNote)
		}
	}
	if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		saveFrame(t, dir, "camoufox-accounts-light", tt)
		tt.SetDark(true)
		tt.Advance(450 * time.Millisecond)
		saveFrame(t, dir, "camoufox-accounts-dark", tt)
	}
	a.browserSel = 1
	tt.Frame()
	if a.accountProxy != "" || a.accountProxyNote != "" {
		t.Fatal("switching accounts retained another account's proxy")
	}
	a.browserSel = 0
	tt.Frame()
	if a.accountProxy != "http://127.0.0.1:7890" {
		t.Fatal("saved proxy was not restored")
	}
	a.accountProxy = ""
	tt.Frame()
	if err := tt.Click("保存账号代理"); err != nil || st.Browsers()[0].Proxy != "" {
		t.Fatalf("clearing proxy: %v", err)
	}
	a.running = true
	a.accountProxy = "http://127.0.0.1:8888"
	tt.Frame()
	_ = tt.Click("保存账号代理")
	if st.Browsers()[0].Proxy != "" {
		t.Fatal("proxy changed while tasks were running")
	}
	for range 10 {
		if _, err := st.AddBrowser("豆包", "其他账号", ""); err != nil {
			t.Fatal(err)
		}
	}
	a.running = false
	tt.SetSize(1000, 720)
	tt.Frame()
	assertVisible(t, tt, "当前账号代理", 1000, 720)
	assertVisible(t, tt, "保存账号代理", 1000, 720)
	assertVisible(t, tt, "删除当前", 1000, 720)
}
