package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

// agyWizardRig 是一份配置库已经打开的 App。HOME 指到临时目录，agy 的查找换成可控的：
// 默认找不到，install 之后就指向一个真实存在的文件。Antigravity 这条路不许起浏览器。
type agyWizardRig struct {
	app       *App
	store     *store.Store
	missing   string
	installed string
}

func newAgyWizardRig(t *testing.T) *agyWizardRig {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &agyWizardRig{store: st, missing: filepath.Join(t.TempDir(), "no-agy")}
	r.app = &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		dataRoot: t.TempDir(), splitSz: 380, sourceHeight: 210,
	}
	r.app.loadSettings()
	r.app.agyHomeRoot = filepath.Join(t.TempDir(), "agy-homes")
	r.app.agyBin = r.missing
	r.app.agyLocate = func(string) (string, bool) {
		if r.installed != "" {
			return r.installed, true
		}
		return r.missing, false
	}
	r.app.wizardLaunch = func(store.Browser) (string, error) {
		t.Error("the Antigravity flow must not start a browser")
		return "", nil
	}
	return r
}

// install 放一个真实存在的 agy 文件，之后的查找都能找到它。
func (r *agyWizardRig) install(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agy")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.installed = path
	return path
}

func clickEach(t *testing.T, tt *ui.Tester, labels ...string) {
	t.Helper()
	for _, label := range labels {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
}

// openAgyLogin 用真实的点击走到 Antigravity 的登录步骤：选平台，装好 agy，点两次下一步。
// 走完后把 agy 路径指回不存在的文件。授权不会真的起命令行，StartLogin 立刻报「找不到 agy」，
// 账号行和账号目录照常建好。
func openAgyLogin(t *testing.T, r *agyWizardRig) *ui.Tester {
	t.Helper()
	r.install(t)
	r.app.applyWizardGate()
	tt := ui.NewTester(r.app.view, 1280, 900)
	clickEach(t, tt, agy.Platform, "下一步", "下一步")
	if r.app.wizardStep != wizard.StepAccount || r.app.agyChooser == nil {
		t.Fatalf("step=%v chooser=%v, want the login step", r.app.wizardStep, r.app.agyChooser)
	}
	r.app.agyBin = r.missing
	return tt
}

func TestWizardAntigravityGuidesInstallBeforeLogin(t *testing.T) {
	r := newAgyWizardRig(t)
	a := r.app
	a.applyWizardGate()
	if !a.wizardOn || a.wizardStep != wizard.StepPlatform {
		t.Fatal("a new config should open on the platform step")
	}
	tt := ui.NewTester(a.view, 1280, 900)

	// 左栏跟着选择走：Antigravity 要先装命令行、不用浏览器，豆包相反。
	clickEach(t, tt, agy.Platform)
	if a.wizardChoice != agy.Platform {
		t.Fatalf("choice = %q", a.wizardChoice)
	}
	if _, ok := tt.Find("浏览器"); ok {
		t.Fatal("the rail still lists a browser step for Antigravity")
	}
	if _, ok := tt.Find("安装"); !ok {
		t.Fatal("the rail has no install step for Antigravity")
	}
	clickEach(t, tt, "豆包")
	if _, ok := tt.Find("安装"); ok {
		t.Fatal("the rail lists an install step for doubao")
	}
	if _, ok := tt.Find("浏览器"); !ok {
		t.Fatal("the rail lost the browser step for doubao")
	}

	clickEach(t, tt, agy.Platform, "下一步")
	if a.wizardStep != wizard.StepInstall {
		t.Fatalf("Antigravity should go to the install step, got %v", a.wizardStep)
	}
	cmd := agy.InstallCommand(runtime.GOOS)
	for _, label := range []string{"安装 Antigravity", "还没有找到 agy", "未安装", "复制命令", "打开官方下载页", "我装在别处…", "重新检测"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if !tt.HasText(cmd) {
		t.Fatalf("install command %q is not shown: %v", cmd, tt.Texts())
	}

	clickEach(t, tt, "下一步")
	if a.wizardStep != wizard.StepInstall {
		t.Fatal("next advanced although agy is not installed")
	}
	clickEach(t, tt, "复制命令")
	if got := tt.Clipboard(); got != cmd {
		t.Fatalf("clipboard = %q, want %q", got, cmd)
	}
	if !tt.HasText("已复制") {
		t.Fatal("no copy confirmation")
	}
	clickEach(t, tt, "打开官方下载页")
	if urls := tt.OpenedURLs(); len(urls) != 1 || urls[0] != agy.InstallPage {
		t.Fatalf("opened %v, want [%s]", urls, agy.InstallPage)
	}

	// 用户去终端装好之后重新检测：改用找到的位置，命令卡片收起来，下一步亮起。
	path := r.install(t)
	clickEach(t, tt, "重新检测")
	if !a.wizardAgy.found || a.agyBin != path {
		t.Fatalf("found=%v agyBin=%q, want %q", a.wizardAgy.found, a.agyBin, path)
	}
	if !tt.HasText("已找到 agy") || !tt.HasText(path) || tt.HasText(cmd) {
		t.Fatalf("install step did not switch to the found state: %v", tt.Texts())
	}
	clickEach(t, tt, "下一步")
	if a.wizardStep != wizard.StepAccount || a.agyChooser == nil {
		t.Fatalf("step=%v chooser=%v", a.wizardStep, a.agyChooser)
	}
	for _, label := range []string{"登录账号", "Google 登录（OAuth）", "导入本地 Antigravity CLI 账号"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if got := r.store.Get("selected_platforms"); got != agy.Platform {
		t.Fatalf("selected_platforms = %q, want only %q", got, agy.Platform)
	}
}

func TestWizardAntigravityLoginFinishesOnThatPlatform(t *testing.T) {
	for _, tc := range []struct {
		name   string
		finish func(t *testing.T, r *agyWizardRig, tt *ui.Tester)
	}{
		{"import", func(t *testing.T, r *agyWizardRig, tt *ui.Tester) {
			home := filepath.Join(t.TempDir(), "acc1")
			writeAgyHome(t, home, "a@b.c")
			r.app.finishAgyImport(agy.ImportResult{Home: home, Name: "acc1", Email: "a@b.c"}, nil)
		}},
		{"oauth", func(t *testing.T, r *agyWizardRig, tt *ui.Tester) {
			clickEach(t, tt, "agy-oauth-go")
			rows := r.store.Browsers()
			if len(rows) != 1 || rows[0].IsLoggedIn || r.app.agyLogin == nil {
				t.Fatalf("oauth did not leave a pending account: %+v", rows)
			}
			// 授权完成后命令行写下凭证，登录监视随后调用 finishAgyLogin。
			writeAgyHome(t, rows[0].UserDataDir, "a@b.c")
			r.app.finishAgyLogin(rows[0].ID, rows[0].Name)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newAgyWizardRig(t)
			a := r.app
			tt := openAgyLogin(t, r)
			assertVisible(t, tt, "Google 登录（OAuth）", 1280, 900)

			tc.finish(t, r, tt)
			tt.Frame()
			tt.Advance(motionUI + 40*time.Millisecond)
			if a.wizardStep != wizard.StepDone || a.agyChooser != nil || a.agyLogin != nil || a.wizardAgy.created != 0 {
				t.Fatalf("step=%v chooser=%v login=%v created=%d", a.wizardStep, a.agyChooser, a.agyLogin, a.wizardAgy.created)
			}
			rows := r.store.Browsers()
			if len(rows) != 1 || rows[0].Platform != agy.Platform || !rows[0].IsLoggedIn {
				t.Fatalf("accounts: %+v", rows)
			}
			assertVisible(t, tt, "可以开始了", 1280, 900)
			if !tt.HasText("Antigravity · " + rows[0].Name) {
				t.Fatalf("done page does not list the account: %v", tt.Texts())
			}

			// 第一次设置只留选的这一个平台，出图页进来就停在它上面。
			clickEach(t, tt, "进入主页")
			if a.wizardOn || r.store.Get(wizard.KeyComplete) != wizard.CompleteYes {
				t.Fatal("finish did not leave the wizard or save the flag")
			}
			if got := r.store.Get("selected_platforms"); got != agy.Platform {
				t.Fatalf("selected_platforms = %q, want only %q", got, agy.Platform)
			}
			if a.platDoubao || a.platQianwen || !a.platAntigravity {
				t.Fatalf("doubao=%v qianwen=%v agy=%v", a.platDoubao, a.platQianwen, a.platAntigravity)
			}
			tt.Frame()
			if got := a.stagePlatform(); got != agy.Platform {
				t.Fatalf("workbench opened on %q", got)
			}
		})
	}
}

func TestWizardAntigravityBackFromLoginKeepsOnlyLoggedInAccounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		loggedIn bool
	}{
		{"unfinished authorization drops the new account", false},
		{"finished authorization keeps it", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newAgyWizardRig(t)
			a := r.app
			tt := openAgyLogin(t, r)
			clickEach(t, tt, "agy-oauth-go")
			rows := r.store.Browsers()
			if len(rows) != 1 || rows[0].Platform != agy.Platform || a.wizardAgy.created != rows[0].ID {
				t.Fatalf("pending account: created=%d rows=%+v", a.wizardAgy.created, rows)
			}
			dir := rows[0].UserDataDir
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("account directory missing before back: %v", err)
			}
			if tc.loggedIn {
				writeAgyHome(t, dir, "a@b.c")
			}

			clickEach(t, tt, "上一步")
			if a.wizardStep != wizard.StepInstall || a.agyLogin != nil || a.agyChooser != nil || a.wizardAgy.created != 0 {
				t.Fatalf("step=%v login=%v chooser=%v created=%d", a.wizardStep, a.agyLogin, a.agyChooser, a.wizardAgy.created)
			}
			left := r.store.Browsers()
			_, statErr := os.Stat(dir)
			if tc.loggedIn {
				if len(left) != 1 || statErr != nil {
					t.Fatalf("a logged-in account was removed: rows=%+v stat=%v", left, statErr)
				}
				return
			}
			if len(left) != 0 || !os.IsNotExist(statErr) {
				t.Fatalf("unfinished account kept: rows=%+v stat=%v", left, statErr)
			}
		})
	}
}

func TestWizardAntigravityReopenAddsPlatformAndLandsOnIt(t *testing.T) {
	r := newAgyWizardRig(t)
	a := r.app
	st := r.store
	for _, name := range []string{"豆包1", "豆包2"} {
		row, err := st.AddBrowser("豆包", name, "")
		if err != nil {
			t.Fatal(err)
		}
		st.UpdateBrowser(row.ID, false, true)
	}
	home := filepath.Join(t.TempDir(), "acc1")
	writeAgyHome(t, home, "a@b.c")
	row, err := st.AddBrowser(agy.Platform, "acc1", home)
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	r.install(t)
	a.refreshBrowsers()
	a.applyWizardGate()
	if a.wizardOn {
		t.Fatal("healthy accounts should skip the wizard")
	}
	tt := ui.NewTester(a.view, 1280, 900)

	clickEach(t, tt, "设置向导")
	if !a.wizardOn || a.wizardFirst || a.wizardStep != wizard.StepPlatform {
		t.Fatalf("reopen: on=%v first=%v step=%v", a.wizardOn, a.wizardFirst, a.wizardStep)
	}
	clickEach(t, tt, agy.Platform, "下一步")
	if a.wizardStep != wizard.StepInstall || !a.wizardAgy.found {
		t.Fatalf("step=%v found=%v", a.wizardStep, a.wizardAgy.found)
	}

	// 已经有能用的 Antigravity 账号：不再要求登录，直接到完成页。
	clickEach(t, tt, "下一步")
	if a.wizardStep != wizard.StepDone || a.agyChooser != nil {
		t.Fatalf("step=%v chooser=%v", a.wizardStep, a.agyChooser)
	}
	tt.Advance(motionUI + 40*time.Millisecond)
	if !tt.HasText("Antigravity · acc1") {
		t.Fatalf("done page does not list the existing account: %v", tt.Texts())
	}

	// 重开向导只是再加一个平台，已有的勾选不动。豆包的账号更多，出图页仍要停在刚加的平台。
	clickEach(t, tt, "进入主页")
	if got := st.Get("selected_platforms"); got != "豆包,"+agy.Platform {
		t.Fatalf("selected_platforms = %q", got)
	}
	tt.Frame()
	if got := a.stagePlatform(); got != agy.Platform {
		t.Fatalf("workbench opened on %q", got)
	}
}
