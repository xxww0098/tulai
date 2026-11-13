package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func browserTableTestApp(t *testing.T, count int) *App {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for i := range count {
		platform, name := "豆包", fmt.Sprintf("商品素材 %02d", i+1)
		if i%2 == 1 {
			platform = "千问"
		}
		if i == 0 {
			name = "主力工作号"
		}
		if i == 2 {
			name = "品牌素材制作团队的备用账号 · Brand Studio"
		}
		row, err := st.AddBrowser(platform, name, "")
		if err != nil {
			t.Fatal(err)
		}
		st.UpdateBrowser(row.ID, i == 1, i != 3)
		if i%3 == 0 {
			if err := st.SetBrowserProxy(row.ID, fmt.Sprintf("http://192.0.2.%d:7890", i+10)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &App{store: st, tab: pageBrowser, browserSel: 0, browserBackend: browser.KindCamoufox,
		detectedNote: "Camoufox 已就绪", camoufoxStatus: "Camoufox 已就绪", camoufoxReady: true}
}

func TestBrowserTableRowActionsTargetTheClickedEnvironment(t *testing.T) {
	a := browserTableTestApp(t, 3)
	var launched int64
	a.loginLaunch = func(b store.Browser) error { launched = b.ID; return nil }
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("启动 品牌素材制作团队的备用账号 · Brand Studio"); err != nil {
		t.Fatal(err)
	}
	selected, ok := a.selectedBrowser()
	if !ok || launched != selected.ID || selected.Name != "品牌素材制作团队的备用账号 · Brand Studio" {
		t.Fatalf("wrong launch target: launched=%d selected=%+v", launched, selected)
	}
	if err := tt.Click("停止 商品素材 02"); err != nil {
		t.Fatal(err)
	}
	if a.store.Browsers()[1].IsRunning {
		t.Fatal("row stop left the clicked environment running")
	}
	a.running = true
	tt.Frame()
	launched = 0
	if err := tt.Click("启动 主力工作号"); err != nil {
		t.Fatal(err)
	}
	if launched != 0 {
		t.Fatal("row launch was enabled while tasks were running")
	}
	a.running = false
	a.dataRoot = t.TempDir()
	fresh, err := a.store.AddBrowser("豆包", "新环境", "")
	if err != nil {
		t.Fatal(err)
	}
	a.markFreshLogin(fresh.ID)
	a.store.SetRunning(fresh.ID, true)
	following, err := a.store.AddBrowser("千问", "后续环境", "")
	if err != nil {
		t.Fatal(err)
	}
	a.store.UpdateBrowser(following.ID, false, true)
	tt.Frame()
	if err := tt.Click("停止 新环境"); err != nil {
		t.Fatal(err)
	}
	for _, b := range a.store.Browsers() {
		if b.ID == fresh.ID {
			t.Fatal("stopping a fresh unlogged environment left it in the list")
		}
	}
	external, err := a.store.SaveLoggedInExtension("豆包", "external-fixture", "扩展账号")
	if err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("扩展会话") || !tt.HasText("由扩展管理") {
		t.Fatal("legacy extension account was presented as a local environment")
	}
	if err := tt.Click("停止 扩展账号"); err != nil {
		t.Fatal(err)
	}
	for _, b := range a.store.Browsers() {
		if b.ID == external.ID && !b.IsRunning {
			t.Fatal("extension account was stopped as a local environment")
		}
	}
}

func TestBrowserTableLayoutAndThemes(t *testing.T) {
	for _, width := range []float32{1280, 1000, 760} {
		t.Run(fmt.Sprintf("width=%g", width), func(t *testing.T) {
			a := browserTableTestApp(t, 12)
			tt := ui.NewTester(a.view, int(width), 720)
			for _, dark := range []bool{false, true} {
				tt.SetDark(dark)
				tt.Advance(400 * time.Millisecond)
				for _, label := range []string{"全选", "批量启动", "批量停止", "清空", "选择环境 主力工作号", "环境名称", "服务商", "环境状态", "操作", "启动 主力工作号", "停止 商品素材 02", "当前账号代理", "保存账号代理", "删除当前"} {
					assertVisible(t, tt, label, width, 720)
				}
				name := assertVisible(t, tt, "主力工作号", width, 720)
				start := assertVisible(t, tt, "启动 主力工作号", width, 720)
				if start.X <= name.X || start.Y > name.Y+40 || start.Y < name.Y-16 {
					t.Fatalf("launch left its environment row: name=%+v start=%+v", name, start)
				}
				if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					saveFrame(t, dir, fmt.Sprintf("browser-table-%g-dark-%t", width, dark), tt)
				}
			}
		})
	}
}

func TestBrowserTableRefreshesDailyUsageAtMidnight(t *testing.T) {
	a := browserTableTestApp(t, 1)
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	b := a.store.Browsers()[0]
	if err := a.store.RecordUse(b.ID, now, 2, 1, 1); err != nil {
		t.Fatal(err)
	}
	a.store.UpdateBrowserHealth(b.ID, store.HealthExhausted, midnight.Unix(), "今日额度已用尽")
	tt := ui.NewTester(a.view, 1280, 840)
	if !tt.HasText("2 / 不限") || !tt.HasText("已用尽") {
		t.Fatalf("before midnight: %v", tt.Texts())
	}
	tt.Advance(midnight.Sub(tt.Now()) - time.Second)
	if !tt.HasText("2 / 不限") || !tt.HasText("已用尽") {
		t.Fatalf("just before midnight: %v", tt.Texts())
	}
	tt.Advance(time.Second)
	if !tt.HasText("0 / 不限") || !tt.HasText("未启动") || tt.HasText("今日额度已用尽") {
		t.Fatalf("at midnight: %v", tt.Texts())
	}
}

func TestBrowserTableKeyboardSelection(t *testing.T) {
	a := browserTableTestApp(t, 3)
	tt := ui.NewTester(a.view, 1280, 720)
	if err := tt.Click("001"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key  ui.Key
		want int
	}{{ui.KeyDown, 1}, {ui.KeyEnd, 2}, {ui.KeyHome, 0}} {
		tt.Key(0, tc.key)
		if a.browserSel != tc.want {
			t.Fatalf("key %v selected row %d, want %d", tc.key, a.browserSel, tc.want)
		}
	}
	tt.Key(ui.Shift, ui.KeyEnd)
	if a.browserChecked.Len() != 3 {
		t.Fatal("shift navigation did not select the environment range")
	}
}

func TestBrowserProxySummaryHidesCredentials(t *testing.T) {
	for _, raw := range []string{"http://private-user:private-secret@192.0.2.10:8000", "socks5://127.0.0.1:1080", "", "invalid"} {
		host, protocol := browserProxySummary(raw)
		if host == "" || protocol == "" || strings.Contains(host+protocol, "private-") {
			t.Fatalf("unsafe proxy summary for %q: %q %q", raw, host, protocol)
		}
	}
}

func TestBrowserTableEmptyAndScaledStates(t *testing.T) {
	for _, count := range []int{0, 3} {
		t.Run(fmt.Sprintf("count=%d", count), func(t *testing.T) {
			a := browserTableTestApp(t, count)
			tt := ui.NewTester(a.view, 1000, 720)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.25})
			tt.Advance(400 * time.Millisecond)
			for _, label := range []string{"添加环境", "刷新列表"} {
				assertVisible(t, tt, label, 1000, 720)
			}
			if count == 0 {
				assertVisible(t, tt, "添加第一个浏览器环境", 1000, 720)
				for _, label := range []string{"全选", "批量启动", "批量停止", "清空", "删除当前", "选择环境以配置代理"} {
					if tt.HasText(label) {
						t.Fatalf("empty state shows unavailable action %q", label)
					}
				}
			} else {
				for _, label := range []string{"全选", "批量启动", "批量停止", "清空", "删除当前"} {
					assertVisible(t, tt, label, 1000, 720)
				}
				assertVisible(t, tt, "启动 主力工作号", 1000, 720)
				assertVisible(t, tt, "保存账号代理", 1000, 720)
			}
			if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				saveFrame(t, dir, fmt.Sprintf("browser-table-scaled-count-%d", count), tt)
			}
		})
	}
}
