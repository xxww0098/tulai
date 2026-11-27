package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

// 浏览器账号页和洗图工作台的账号卡用同一条规则：到期的额度、冷却错误只是历史。
func TestBrowserRowsHideExpiredQuotaError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.AddBrowser("豆包", "豆包1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(b.ID, false, true)
	a := NewApp(st, nil, nil, "")
	SetTestApp(a)
	defer SetTestApp(nil)

	const quota = "账号额度已用尽: E_QUOTA: 今日图片生成免费额度已用完。"
	expired := time.Now().Add(-time.Minute).Unix()
	st.UpdateBrowserHealth(b.ID, store.HealthExhausted, expired, quota)
	// 界面可能还在用换日之前读到的行，这里直接放一份旧快照。
	a.browserRows = []store.Browser{{
		ID: b.ID, Platform: "豆包", Name: "豆包1", IsLoggedIn: true,
		Health: store.HealthExhausted, CoolUntil: expired, LastError: quota,
	}}
	rows := getBrowserStateBridge().Rows
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].LastError != "" || rows[0].Health != "就绪" {
		t.Fatalf("expired quota should be hidden: %+v", rows[0])
	}

	// 还在冷却的账号照旧显示原因和状态。
	future := time.Now().Add(time.Hour).Unix()
	st.UpdateBrowserHealth(b.ID, store.HealthCooling, future, "请求过于频繁")
	a.refreshBrowsers()
	if got := getBrowserStateBridge().Rows[0]; got.LastError != "请求过于频繁" || got.Health != "冷却中" {
		t.Fatalf("active cooldown should stay: %+v", got)
	}

	// 生图降级为纯文本的账号显示「生图受限」危险态并保留具体原因。
	const degradedErr = "账号生图能力受限（已降级纯文本）（flow.agent.shell）"
	st.UpdateBrowserHealth(b.ID, store.HealthExhausted, future, degradedErr)
	a.refreshBrowsers()
	if got := getBrowserStateBridge().Rows[0]; got.Health != "生图受限" || got.HealthTone != "danger" || got.LastError != degradedErr {
		t.Fatalf("degraded account should display 生图受限: %+v", got)
	}
}
