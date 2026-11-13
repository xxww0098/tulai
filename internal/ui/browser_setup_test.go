package ui

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestBrowserSetupFollowsInstallationState(t *testing.T) {
	if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, width := range []int{760, 1000, 1280} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/dark=%t", width, dark), func(t *testing.T) {
				a := browserTableTestApp(t, 0)
				a.camoufoxReady = false
				a.camoufoxStatus = "安装后即可添加服务商账号"
				tt := ui.NewTester(a.view, width, 720)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.25})
				tt.SetDark(dark)
				tt.Advance(400 * time.Millisecond)
				for _, label := range []string{"Camoufox 账号环境", "安装 Camoufox 浏览器", "安装 Camoufox"} {
					assertVisible(t, tt, label, float32(width), 720)
				}
				for _, label := range []string{"添加环境", "全选", "删除当前", "使用下载环境"} {
					if tt.HasText(label) {
						t.Fatalf("before installation shows unavailable action %q", label)
					}
				}
				if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
					saveFrame(t, dir, fmt.Sprintf("browser-install-%d-dark-%t", width, dark), tt)
				}

				a.camoufoxBusy = true
				a.camoufoxStatus = "正在下载浏览器…"
				a.camoufoxPercent = 0.42
				tt.Frame()
				assertVisible(t, tt, "正在下载浏览器…", float32(width), 720)
				assertVisible(t, tt, "正在安装…", float32(width), 720)
				if action, ok := tt.AccessByRole(ui.RoleButton, "正在安装…"); !ok || !action.Disabled {
					t.Fatalf("installation action is not disabled while busy: %+v", action)
				}
				if err := tt.Click("正在安装…"); err != nil {
					t.Fatal(err)
				}
				if !a.camoufoxBusy {
					t.Fatal("installation action changed while already busy")
				}

				a.camoufoxBusy = false
				a.camoufoxStatus = "安装失败：网络不可用"
				tt.Frame()
				assertVisible(t, tt, a.camoufoxStatus, float32(width), 720)
				assertVisible(t, tt, "重试安装", float32(width), 720)

				a.camoufoxReady = true
				a.camoufoxStatus = "Camoufox 已就绪"
				tt.Frame()
				for _, label := range []string{"添加环境", "添加第一个浏览器环境", "使用下载环境"} {
					assertVisible(t, tt, label, float32(width), 720)
				}
				if tt.HasText("重试安装") || tt.HasText("全选") {
					t.Fatal("installed empty state retained installation or batch controls")
				}
				if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
					saveFrame(t, dir, fmt.Sprintf("browser-ready-empty-%d-dark-%t", width, dark), tt)
				}
			})
		}
	}
}

func TestMissingBrowserKeepsSavedAccountManagement(t *testing.T) {
	a := browserTableTestApp(t, 1)
	a.camoufoxReady = false
	a.camoufoxStatus = "安装后即可添加服务商账号"
	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"安装 Camoufox", "主力工作号", "配置 主力工作号", "删除当前"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if err := tt.Click("配置 主力工作号"); err != nil {
		t.Fatal(err)
	}
	if selected, ok := a.selectedBrowser(); !ok || selected.Name != "主力工作号" {
		t.Fatalf("saved account is no longer selectable: %+v", selected)
	}
}
