package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestLoginBannerAndAutoLog(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "账号2", "")
	if err != nil {
		t.Fatal(err)
	}
	e := tasks.NewEngine(st, nil, nil, "")
	a := &App{store: st, engine: e, platformAuto: true}
	e.RaiseAttention("账号2 需要重新扫码登录", row.ID)
	e.RecordAuto("账号1 浏览器崩溃，已自动重启")
	a.queue = []tasks.Task{{Platform: "豆包", Status: tasks.StatusPending, Images: []string{"风景.png"}}}
	a.sources = []sourceImage{{Path: "风景.png"}}
	const width, height = 1280, 900
	tt := ui.NewTester(a.view, width, height)
	assertVisible(t, tt, "账号2 需要重新扫码登录", width, height)
	assertVisible(t, tt, "去处理", width, height)
	if err := tt.Click("高级"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(250 * time.Millisecond)
	scrollIntoView(t, tt, "运行日志", width, height)
	scrollIntoView(t, tt, "账号1 浏览器崩溃，已自动重启", width, height)
	scrollIntoView(t, tt, "去处理", width, height)
	if err := tt.Click("去处理"); err != nil {
		t.Fatal(err)
	}
	if a.tab != pageBrowser || a.browserSel != 0 {
		t.Fatalf("去处理 should open the account: tab=%d sel=%d", a.tab, a.browserSel)
	}
}

func scrollIntoView(t *testing.T, tt *ui.Tester, label string, width, height float32) {
	t.Helper()
	inView := func() bool {
		r, ok := tt.Find(label)
		return ok && r.W > 0 && r.H > 0 && r.X >= 0 && r.Y >= 0 && r.X+r.W <= width && r.Y+r.H <= height
	}
	if inView() {
		return
	}
	for _, dy := range []float32{-240, 240} {
		for i := 0; i < 8; i++ {
			tt.Scroll(width/2, height/2, 0, dy)
			if inView() {
				return
			}
		}
	}
	r, ok := tt.Find(label)
	t.Fatalf("%q is outside the window: %+v (found=%v)", label, r, ok)
}
