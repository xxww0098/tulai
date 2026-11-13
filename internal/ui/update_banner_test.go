package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/update"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestUpdateBannerAndSkip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store: st, engine: tasks.NewEngine(st, nil, nil, ""),
		updateNotice: "0.2.0", updateNotes: "修复了下载中断",
	}
	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"发现新版本 0.2.0", "查看更新详情", "稍后"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if err := tt.Click("查看更新详情"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"查看更新说明", "立即更新", "跳过此版本"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if err := tt.Click("查看更新说明"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "修复了下载中断", 1280, 900)
	if err := tt.Click("跳过此版本"); err != nil {
		t.Fatal(err)
	}
	if a.updateNotice != "" || a.updateDetailsOpen || a.updateNotesOpen {
		t.Fatal("banner stayed after skip")
	}
	if update.Skipped(st) != "0.2.0" {
		t.Fatalf("skip stored %q", update.Skipped(st))
	}
	st.Close()
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if update.Skipped(st) != "0.2.0" {
		t.Fatalf("skip did not survive reopen: %q", update.Skipped(st))
	}
}

func TestUpdateBannerLayoutAndDownloadState(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		wizard, dark  bool
	}{
		{1280, 840, false, false},
		{760, 720, false, true},
		{760, 720, true, false},
	} {
		t.Run(fmt.Sprintf("width=%d-wizard=%t-dark=%t", tc.width, tc.wizard, tc.dark), func(t *testing.T) {
			var lines []string
			for i := 1; i <= 14; i++ {
				lines = append(lines, fmt.Sprintf("更新说明内容 %02d", i))
			}
			notes := strings.Join(lines, "\n")
			a := &App{tab: pageBrowser, wizardOn: tc.wizard, updateNotice: "0.2.0", updateNotes: notes,
				updateDownloading: true, updateFraction: 0.45, updateProgressText: "正在下载 tulai-0.2.0"}
			tt := ui.NewTester(a.view, tc.width, tc.height)
			tt.SetDark(tc.dark)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1.25})
			tt.Advance(300 * time.Millisecond)
			for _, label := range []string{"发现新版本 0.2.0", "查看更新详情", "隐藏更新提示", "正在下载 tulai-0.2.0", "45%"} {
				assertVisible(t, tt, label, float32(tc.width), float32(tc.height))
			}
			panel := assertVisible(t, tt, "更新提示", float32(tc.width), float32(tc.height))
			bottom := float32(12)
			if tc.wizard {
				bottom = 58
			}
			if panel.W > 276*interfaceZoom+1 || math.Abs(float64(panel.X+panel.W-(float32(tc.width)-12*interfaceZoom))) > 1 || math.Abs(float64(panel.Y+panel.H-(float32(tc.height)-bottom*interfaceZoom))) > 1 {
				t.Fatalf("update notice is not in the bottom-right corner: %+v", panel)
			}
			if tt.HasText(lines[0]) {
				t.Fatal("release notes should start collapsed")
			}
			if tt.HasText("稍后") || tt.HasText("跳过此版本") || tt.HasText("立即更新") {
				t.Fatal("unavailable update actions should be hidden while downloading")
			}
			if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				saveFrame(t, dir, fmt.Sprintf("update-%d-wizard-%t-dark-%t", tc.width, tc.wizard, tc.dark), tt)
			}
			if err := tt.Click("查看更新详情"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("查看更新说明"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText(lines[0]) {
				t.Fatal("release notes were not revealed")
			}
			notesRect := assertVisible(t, tt, "更新说明", float32(tc.width), float32(tc.height))
			if notesRect.H > 120 {
				t.Fatalf("release notes grew beyond their viewport: %+v", notesRect)
			}
			if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
				saveFrame(t, dir, fmt.Sprintf("update-expanded-%d-wizard-%t-dark-%t", tc.width, tc.wizard, tc.dark), tt)
			}
			before, _ := tt.Find(lines[len(lines)-1])
			tt.Scroll(notesRect.X+notesRect.W/2, notesRect.Y+notesRect.H/2, 0, 200)
			after, _ := tt.Find(lines[len(lines)-1])
			if after.Y >= before.Y {
				t.Fatalf("release notes did not scroll: viewport=%+v before=%+v after=%+v", notesRect, before, after)
			}
			if err := tt.Click("收起更新说明"); err != nil {
				t.Fatal(err)
			}
			if a.updateNotesOpen {
				t.Fatal("release notes did not collapse")
			}
			a.updateFraction, a.updateProgressText = 1, "正在安装…"
			tt.Frame()
			if !tt.HasText("正在安装…") || tt.HasText("100%") {
				t.Fatal("installation should not show a download percentage")
			}
			a.updateDownloading = false
			a.updateSigned = &mygo.Update{}
			a.updateErr = "下载失败，请重试"
			tt.Frame()
			for _, label := range []string{"稍后", "跳过此版本", "立即更新", "更新包由 MyGo 验证开发者签名。", "下载失败，请重试"} {
				assertVisible(t, tt, label, float32(tc.width), float32(tc.height))
			}
			if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
				saveFrame(t, dir, fmt.Sprintf("update-ready-%d-wizard-%t-dark-%t", tc.width, tc.wizard, tc.dark), tt)
			}
		})
	}
}

func TestUpdateNoticeDoesNotMoveOrBlockPage(t *testing.T) {
	for _, inWizard := range []bool{false, true} {
		t.Run(fmt.Sprintf("wizard=%t", inWizard), func(t *testing.T) {
			a := &App{tab: pageBrowser, detectedNote: "ready", wizardOn: inWizard,
				wizardStep: wizard.StepPlatform, wizardChoice: "豆包", platDoubao: true}
			tt := ui.NewTester(a.view, 1000, 720)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			heading := "Camoufox 账号环境"
			if inWizard {
				heading = "选择平台"
			}
			before := assertVisible(t, tt, heading, 1000, 720)
			a.updateNotice = "0.2.0"
			tt.Frame()
			after := assertVisible(t, tt, heading, 1000, 720)
			if before != after {
				t.Fatalf("update notice moved page content: before=%+v after=%+v", before, after)
			}
			if inWizard {
				if err := tt.Click("下一步"); err != nil {
					t.Fatal(err)
				}
				if a.wizardStep == wizard.StepPlatform {
					t.Fatal("update notice blocked the wizard footer")
				}
			} else {
				if err := tt.Click("提示词"); err != nil {
					t.Fatal(err)
				}
				if a.tab != pagePrompts {
					t.Fatal("update notice blocked navigation")
				}
			}
		})
	}
}

func TestUpdateNoticeCanHideAndRestoreDownload(t *testing.T) {
	for _, inWizard := range []bool{false, true} {
		t.Run(fmt.Sprintf("wizard=%t", inWizard), func(t *testing.T) {
			a := &App{wizardOn: inWizard, updateNotice: "0.2.0", updateDownloading: true,
				updateBusy: true, updateFraction: 0.45, updateProgressText: "正在下载"}
			tt := ui.NewTester(a.view, 1000, 720)
			if err := tt.Click("隐藏更新提示"); err != nil {
				t.Fatal(err)
			}
			if !a.updateHidden || !a.updateDownloading || tt.HasText("发现新版本 0.2.0") {
				t.Fatal("hiding the notice interrupted or failed to hide the download")
			}
			if err := tt.Click("显示更新进度"); err != nil {
				t.Fatal(err)
			}
			if a.updateHidden || !tt.HasText("45%") {
				t.Fatal("download progress did not return")
			}
			if err := tt.Click("隐藏更新提示"); err != nil {
				t.Fatal(err)
			}
			a.updateDownloading = false
			a.updateErr = "下载失败，请重试"
			tt.Frame()
			if a.updateHidden || !tt.HasText(a.updateErr) {
				t.Fatal("failure did not restore the hidden notice")
			}
		})
	}
}

func TestUpdateNoticeStaysBehindBrowserDrawer(t *testing.T) {
	done := make(chan struct{})
	close(done)
	a := &App{updateNotice: "0.2.0", browserDrawer: &browserDrawerState{
		row: store.Browser{Name: "测试账号"}, cancel: func() {}, done: done, loading: true,
	}}
	tt := ui.NewTester(a.view, 1000, 720)
	_ = tt.Click("查看更新详情")
	if a.updateDetailsOpen {
		t.Fatal("update notice appeared above the browser drawer")
	}
	if err := tt.Click("收起浏览器"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("查看更新详情"); err != nil {
		t.Fatal(err)
	}
	if !a.updateDetailsOpen {
		t.Fatal("update notice did not become available after closing the drawer")
	}
}

func TestUpdateDefersWhileTasksRun(t *testing.T) {
	a := &App{
		engine:  tasks.NewEngine(nil, nil, nil, ""),
		running: true, updateNotice: "0.2.0",
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("查看更新详情"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("立即更新"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.updatePending || !a.updateDeferred {
		t.Fatalf("pending=%v deferred=%v", a.updatePending, a.updateDeferred)
	}
	if !tt.HasText(update.DeferMessage) {
		t.Fatal("banner did not say the update waits for idle")
	}
	if update.NextStep(a.tasksRunning(), a.updatePending, false) != update.StepDefer {
		t.Fatal("next step would install during a run")
	}
	a.running = false
	if update.NextStep(a.tasksRunning(), a.updatePending, false) != update.StepDownload {
		t.Fatal("idle pending update did not move to download")
	}
}

func TestUpdateWaitsForOtherWork(t *testing.T) {
	for name, start := range map[string]func(*App){
		"rename":   func(a *App) { a.fileRename.busy = true },
		"editor":   func(a *App) { a.edit.running = true },
		"dedup":    func(a *App) { a.dedup.busy = true },
		"browser":  func(a *App) { a.camoufoxBusy = true },
		"download": func(a *App) { a.downloadsRunning = 1 },
		"queued":   func(a *App) { a.dlTasks = []*downloadTask{{Phase: dlQueued}} },
	} {
		t.Run(name, func(t *testing.T) {
			a := &App{updateNotice: "0.2.0"}
			start(a)
			a.applyUpdate()
			if !a.updatePending || !a.updateDeferred {
				t.Fatal("update did not wait for active work")
			}
		})
	}
}

func TestUpdateBlocksNewDownloads(t *testing.T) {
	task := &downloadTask{Phase: dlQueued}
	a := &App{updateBusy: true, updatePending: true, dlTasks: []*downloadTask{task}}
	a.pumpDownloads()
	if task.Phase != dlQueued || a.downloadsRunning != 0 {
		t.Fatal("a download started while the update was installing")
	}
}

func TestSettingsShowVersionAndCheck(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), tab: pageSettings, settingsTab: settingsUpdate, autoCheck: true}
	tt := ui.NewTester(a.view, 1280, 900)
	scrollIntoView(t, tt, "当前版本 "+currentVersion(), 1280, 900)
	for _, label := range []string{"当前版本 " + currentVersion(), "自动检查更新", "检查更新"} {
		assertVisible(t, tt, label, 1280, 900)
	}
}
