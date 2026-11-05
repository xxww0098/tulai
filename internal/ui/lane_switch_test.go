package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestConcurrencySwitchInAccountHeader(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		for _, width := range []int{760, 1024, 1280} {
			t.Run(fmt.Sprintf("%s-%d", pageLabel(page), width), func(t *testing.T) {
				a := &App{tab: page, statePage: page, platformAuto: true, concurrencyAuto: true,
					browserRows: []store.Browser{loggedLane(1, "豆包1", "豆包")}}
				tt := ui.NewTester(a.view, width, 900)
				tt.Advance(400 * time.Millisecond)
				header := assertVisible(t, tt, "1 个账号空闲", float32(width), 900)
				label := assertVisible(t, tt, "并发", float32(width), 900)
				switchRect := assertVisible(t, tt, "启用并发", float32(width), 900)
				add := assertVisible(t, tt, "+ 添加账号", float32(width), 900)
				tableHead := assertVisible(t, tt, "今日", float32(width), 900)
				if header.X+header.W > label.X || label.X+label.W > switchRect.X ||
					switchRect.X+switchRect.W > add.X || switchRect.Y+switchRect.H > tableHead.Y {
					t.Fatalf("并发开关应位于账号表格标题和添加账号之间: header=%+v label=%+v switch=%+v add=%+v table=%+v", header, label, switchRect, add, tableHead)
				}
			})
		}
	}
}

func TestConcurrencySwitchPersistsAndIsShared(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, platform := range []string{"豆包", "千问", "Antigravity"} {
		if _, err := st.AddBrowser(platform, platform+"账号", ""); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{store: st, platformAuto: true, concurrencyAuto: true, stagePicked: true}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(400 * time.Millisecond)
	if err := tt.Click("启用并发"); err != nil {
		t.Fatal(err)
	}
	if a.concurrencyAuto || st.GetInt("concurrency") != 1 || a.taskOptions(nil, nil, nil).Concurrency != 1 {
		t.Fatal("关闭并发后应立即保存并使用串行模式")
	}
	reloaded := &App{store: st}
	reloaded.loadSettings()
	if reloaded.concurrencyAuto {
		t.Fatal("关闭并发的状态没有恢复")
	}
	if err := tt.Click("千问"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	if err := tt.Click("启用并发"); err != nil {
		t.Fatal(err)
	}
	if !a.concurrencyAuto || st.GetInt("concurrency") != 0 || a.taskOptions(nil, nil, nil).Concurrency != 0 {
		t.Fatal("在另一服务商启用并发后应保存自动并发模式")
	}
	if err := tt.Click("Antigravity"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	if err := tt.Click("启用并发"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeySpace)
	if !a.concurrencyAuto || st.GetInt("concurrency") != 0 {
		t.Fatal("应能使用键盘切换并发设置")
	}
	if err := tt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	assertVisible(t, tt, "启用并发", 1280, 900)
	a.running = true
	tt.Frame()
	if err := tt.Click("启用并发"); err != nil {
		t.Fatal(err)
	}
	if !a.concurrencyAuto || st.GetInt("concurrency") != 0 {
		t.Fatal("运行期间不应更改并发设置")
	}
}

func TestLaneSwitchChangesAvailability(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, name := range []string{"豆包1", "豆包2"} {
		b, err := st.AddBrowser("豆包", name, "")
		if err != nil {
			t.Fatal(err)
		}
		st.UpdateBrowser(b.ID, true, true)
	}
	a := &App{store: st, engine: tasks.NewEngine(st, nil, nil, ""), platformAuto: true, concurrencyAuto: true, prompts: "一只猫"}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(400 * time.Millisecond)
	switchRect, ok := tt.Find("启用账号 豆包1")
	nameRect, nameOK := tt.Find("豆包1")
	if !ok || !nameOK || switchRect.X+switchRect.W > nameRect.X {
		t.Fatalf("开关没有放在账号名称左侧: switch=%+v name=%+v", switchRect, nameRect)
	}
	if err := tt.Click("启用账号 豆包1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !st.Browsers()[0].Disabled || a.parallelSlots() != 1 || !tt.HasText("已禁用") || !tt.HasText("1 个账号空闲") {
		t.Fatalf("关闭第一个账号后: rows=%+v slots=%d texts=%v", st.Browsers(), a.parallelSlots(), tt.Texts())
	}
	if err := tt.Click("启用账号 豆包2"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.parallelSlots() != 0 || a.workbenchPlan().Action != StartNone || !tt.HasText("暂无可用账号") {
		t.Fatalf("全部关闭后: slots=%d plan=%+v", a.parallelSlots(), a.workbenchPlan())
	}
	tt.Advance(400 * time.Millisecond)
	if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "lanes-disabled.png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if st.Browsers()[1].Disabled || a.parallelSlots() != 1 || a.workbenchPlan().Action != StartGenerate {
		t.Fatalf("重新启用后: rows=%+v plan=%+v", st.Browsers(), a.workbenchPlan())
	}
}

func TestDisabledAgyLaneStaysDisabledWhenLoggedOut(t *testing.T) {
	a := &App{stageTab: 2, browserRows: []store.Browser{{
		ID: 1, Platform: "Antigravity", Name: "账号", UserDataDir: t.TempDir(), Disabled: true,
	}}}
	lane := a.accountBoard(time.Now()).Lanes[0]
	if lane.State != tasks.LaneDisabled || lane.Action != "" {
		t.Fatalf("禁用的 Antigravity 账号: %+v", lane)
	}
	a.browserRows[0].Disabled = false
	if lane := a.accountBoard(time.Now()).Lanes[0]; lane.State != tasks.LaneLogin {
		t.Fatalf("重新启用后应显示登录状态: %+v", lane)
	}
}

func TestLaneSwitchOnlyDisablesSelectedExtensionAccount(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browserBackend", "extension")
	first, err := st.UpsertExtensionAccount("豆包", "profile", "豆包扩展")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.UpsertExtensionAccount("千问", "profile", "千问扩展")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st, platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(400 * time.Millisecond)
	if err := tt.Click("启用账号 豆包扩展"); err != nil {
		t.Fatal(err)
	}
	for _, row := range st.Browsers() {
		if row.ID == first.ID && !row.Disabled || row.ID == second.ID && row.Disabled {
			t.Fatalf("扩展账号状态错误: %+v", row)
		}
	}
}

func TestLaneSwitchLeavesRoomForAccountAt1024(t *testing.T) {
	a := &App{platformAuto: true, browserRows: []store.Browser{loggedLane(1, "豆包主力", "豆包")}}
	tt := ui.NewTester(a.view, 1024, 700)
	tt.Advance(400 * time.Millisecond)
	name, ok := tt.Find("豆包主力")
	if !ok || name.W < 48 {
		t.Fatalf("账号名称被开关挤掉: %+v", name)
	}
}
