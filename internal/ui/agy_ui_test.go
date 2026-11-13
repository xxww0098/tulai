package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestAntigravityAddChooser(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "database", "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store:        st,
		platformAuto: true,
		stageTab:     2,
		stagePicked:  true,
		agyBin:       filepath.Join(dir, "missing-agy"),
	}
	tt := ui.NewTester(a.view, 1024, 700)
	tt.Advance(200 * time.Millisecond)
	if err := tt.Click("+ 添加账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, text := range []string{"Google 登录（OAuth）", "导入本地 Antigravity CLI 账号", "登录", "导入", "选择文件夹…"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q", text)
		}
	}
	if a.agyChooser == nil {
		t.Fatal("chooser state missing")
	}
	if !a.agyChooser.canImport {
		if !strings.Contains(a.agyChooser.hint, "已检查") || !strings.Contains(a.agyChooser.hint, "~/.gemini/antigravity-cli") {
			t.Fatalf("hint %q", a.agyChooser.hint)
		}
		if !strings.HasSuffix(filepath.ToSlash(a.agyChooser.home), "/.gemini/antigravity-cli") {
			t.Fatalf("home %q", a.agyChooser.home)
		}
		lead, checked := splitCheckedHint(a.agyChooser.hint)
		if lead == "" || checked == "" || !strings.Contains(checked, "~/.gemini/antigravity-cli") {
			t.Fatalf("hint %q", a.agyChooser.hint)
		}
		if !tt.HasText(lead) || !tt.HasText(checked) {
			t.Fatal("chooser did not show the checked folder on its own line")
		}
		assertCheckedPathOneLine(t, tt, checked)
	}
	panel, ok := tt.Find("agy-add-chooser")
	if !ok {
		t.Fatal("chooser missing")
	}
	for _, label := range []string{"agy-oauth-go", "agy-import-go", "agy-pick-folder"} {
		btn, found := tt.Find(label)
		if !found {
			t.Fatalf("%s missing", label)
		}
		if btn.W >= panel.W-24 {
			t.Fatalf("%s is full width: button %v panel %v", label, btn, panel)
		}
		if btn.X < panel.X+panel.W*0.45 {
			t.Fatalf("%s is not on the right: button %v panel %v", label, btn, panel)
		}
	}
	if err := tt.Click("登录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("授权码") || !tt.HasText("找不到 agy") {
		t.Fatal("登录没有打开授权面板")
	}
	if tt.HasText("导入本地 Antigravity CLI 账号") {
		t.Fatal("授权面板打开后选择方式还在")
	}
}

func TestAgyImportSuccessClosesChooser(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	home := filepath.Join(dir, "acc1")
	writeAgyHome(t, home, "a@b.c")
	a := &App{
		store: st, platformAuto: true, stageTab: 2, stagePicked: true,
		agyChooser: &agyChooserState{busy: true, canImport: true, home: home, email: "a@b.c"},
	}
	a.finishAgyImport(agy.ImportResult{}, errors.New("验证没通过"))
	if a.agyChooser == nil || a.agyChooser.err == "" || !a.agyChooser.retry {
		t.Fatal("失败时应留着选择器")
	}
	before := len(logging.Log.Entries())
	a.finishAgyImport(agy.ImportResult{Home: home, Name: "acc1", Email: "a@b.c"}, nil)
	if a.agyChooser != nil {
		t.Fatal("导入成功后选择器还开着")
	}
	entries := logging.Log.Entries()
	if len(entries) <= before || !strings.Contains(entries[len(entries)-1].Text, "a@b.c") || !strings.Contains(entries[len(entries)-1].Text, agy.MsgImported) {
		t.Fatalf("日志 = %+v", entries[before:])
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(200 * time.Millisecond)
	if tt.HasText("导入本地 Antigravity CLI 账号") {
		t.Fatal("选择器还在界面上")
	}
	if !tt.HasText("a@b.c") {
		t.Fatalf("新账号应以邮箱显示: %v", tt.Texts())
	}
	if tt.HasText("邮箱") {
		t.Fatal("Antigravity 不应再单独显示邮箱列")
	}
}

func TestAntigravityCheckedPathDoesNotWrap(t *testing.T) {
	const path = "~/.gemini/antigravity-cli"
	long := "~/" + strings.Repeat("very-long-directory/", 12) + ".gemini/antigravity-cli"
	for _, checkedPath := range []string{path, long} {
		hint := agy.MsgNotFound + "（已检查 " + checkedPath + "）"
		a := &App{
			platformAuto: true, stageTab: 2, stagePicked: true,
			agyChooser: &agyChooserState{hint: hint, home: checkedPath},
			browserRows: []store.Browser{{
				ID: 1, Name: "acc1", Platform: agy.Platform, IsLoggedIn: true, Health: store.HealthReady,
			}},
		}
		tt := ui.NewTester(a.view, 1024, 700)
		tt.Advance(120 * time.Millisecond)
		lead, checked := splitCheckedHint(hint)
		if lead != agy.MsgNotFound || checked != "已检查 "+checkedPath {
			t.Fatalf("split %q -> %q %q", hint, lead, checked)
		}
		if !tt.HasText(lead) {
			t.Fatal("lead missing")
		}
		assertCheckedPathOneLine(t, tt, checked)
	}
}

func assertCheckedPathOneLine(t *testing.T, tt *ui.Tester, checked string) {
	t.Helper()
	path, ok := tt.Find(checked)
	if !ok {
		t.Fatalf("checked path missing: %q", checked)
	}
	title, ok := tt.Find("导入本地 Antigravity CLI 账号")
	if !ok {
		t.Fatal("chooser title missing")
	}
	lead, ok := tt.Find(agy.MsgNotFound)
	if !ok {
		t.Fatal("not-found line missing")
	}
	panel, ok := tt.Find("agy-add-chooser")
	if !ok {
		t.Fatal("chooser missing")
	}
	if path.H > title.H+1 {
		t.Fatalf("checked path wrapped: path %v title %v", path, title)
	}
	if path.Y+1 < lead.Y+lead.H || rectsOverlap(path, lead) {
		t.Fatalf("checked path is not its own line under the message: path %v lead %v", path, lead)
	}
	if path.X < panel.X-1 || path.X+path.W > panel.X+panel.W+1 {
		t.Fatalf("checked path leaves the chooser: path %v panel %v", path, panel)
	}
	for _, text := range tt.Texts() {
		if strings.Contains(text, "antigravity-cli") && text != checked {
			t.Fatalf("path split into %q", text)
		}
	}
}

func TestAntigravityTabShowsAccountAndLogin(t *testing.T) {
	home := filepath.Join(t.TempDir(), "acc1")
	writeAgyHome(t, home, "li@gmail.com")
	now := time.Now()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()).Unix()
	const link = "https://accounts.google.com/o/oauth2/auth?client_id=agy"
	launched := make(chan store.Browser, 1)
	navigated := make(chan string, 1)
	a := &App{
		platformAuto: true,
		stageTab:     2,
		stagePicked:  true,
		prompts:      "一只猫",
		browserRows: []store.Browser{{
			ID: 1, Name: "acc1", Platform: agy.Platform, UserDataDir: home,
			IsLoggedIn: true, Health: store.HealthExhausted, LastError: "额度不足", CoolUntil: midnight,
		}},
		agyLogin:     &agyLoginState{id: 1, home: home, url: link, name: "acc1"},
		wizardLaunch: func(row store.Browser) (string, error) { launched <- row; return "camoufox://9333", nil },
		wizardOpen: func(ws, rawURL string) error {
			if ws != "camoufox://9333" {
				return errors.New("wrong account endpoint")
			}
			navigated <- rawURL
			return nil
		},
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
	select {
	case row := <-launched:
		if row.ID != 1 || row.UserDataDir != filepath.Join(home, "oauth-browser") {
			t.Fatal("OAuth did not use the account's dedicated profile")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("account browser was not started")
	}
	select {
	case rawURL := <-navigated:
		if rawURL != link {
			t.Fatal("wrong OAuth URL")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OAuth page was not opened")
	}
	if len(tt.OpenedURLs()) != 0 {
		t.Fatal("OAuth opened an external browser")
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

func TestAntigravityAddAccountStaysInAccountTable(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		for _, width := range []int{900, 1024, 1280} {
			t.Run(fmt.Sprintf("%s-%d", pageLabel(page), width), func(t *testing.T) {
				a := &App{platformAuto: true, tab: page, statePage: page, stageTab: 2, stagePicked: true, prompts: "一只猫"}
				if page == pageWash {
					a.prompts = DefaultWashPrompt
				}
				tester := ui.NewTester(a.view, width, 900)
				check := func() (ui.Rect, ui.Rect) {
					t.Helper()
					tester.Frame()
					tester.Advance(200 * time.Millisecond)
					table := assertVisible(t, tester, "账号表格", float32(width), 900)
					add := assertVisible(t, tester, "+ 添加账号", float32(width), 900)
					if countExact(tester, "+ 添加账号") != 1 {
						t.Fatal("添加账号入口重复")
					}
					if add.X < table.X+table.W/2 || add.X+add.W > table.X+table.W || add.X+add.W < table.X+table.W-32 || add.Y < table.Y || add.Y+add.H > table.Y+52 {
						t.Fatalf("添加账号没有固定在表格右上角: table=%v add=%v", table, add)
					}
					return table, add
				}
				check()
				a.agyChooser = &agyChooserState{hint: agy.MsgKeychain, canImport: true}
				table, add := check()
				panel := assertVisible(t, tester, "agy-add-chooser", float32(width), 900)
				if panel.Y < add.Y+add.H || panel.X < table.X || panel.X+panel.W > table.X+table.W || panel.Y+panel.H > table.Y+table.H {
					t.Fatalf("登录/导入选择器不在账号表格内: table=%v panel=%v", table, panel)
				}
				a.agyChooser = nil
				a.agyLogin = &agyLoginState{url: "https://accounts.google.com/o/oauth2/auth?client_id=agy"}
				table, add = check()
				title := assertVisible(t, tester, "登录 Antigravity", float32(width), 900)
				submit := assertVisible(t, tester, "提交", float32(width), 900)
				if title.Y < add.Y+add.H || submit.Y+submit.H > table.Y+table.H {
					t.Fatal("授权表单不在账号表格内")
				}
			})
		}
	}
}

func TestAntigravityTableKeepsQuotaAndRunningTaskReadable(t *testing.T) {
	for _, size := range [][2]int{{900, 700}, {1280, 800}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			now := time.Now()
			a := &App{platformAuto: true, stageTab: 2, stagePicked: true, prompts: "一只猫", agyUsage: map[int64]agyUsageSlot{}}
			usage := agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: 0.7, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: 0.6, Reset: now.Add(48 * time.Hour)}}
			for i := 1; i <= 8; i++ {
				name := fmt.Sprintf("账号%d", i)
				home := t.TempDir()
				writeAgyHome(t, home, fmt.Sprintf("account%d@example.com", i))
				a.browserRows = append(a.browserRows, store.Browser{ID: int64(i), Name: name, Platform: agy.Platform, UserDataDir: home, IsLoggedIn: true, Health: store.HealthReady})
				a.agyUsage[int64(i)] = agyUsageSlot{view: agy.UsageView{State: agy.UsageOK, At: now, Usage: usage}}
			}
			a.queue = []tasks.Task{{Index: 1, Platform: agy.Platform, BrowserID: 1, Status: tasks.StatusRunning, Prompt: "正在处理的插画"}}
			tester := ui.NewTester(a.view, size[0], size[1])
			tester.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			tester.Advance(300 * time.Millisecond)
			row := assertVisible(t, tester, "车道账号1", float32(size[0]), float32(size[1]))
			if row.H < 34*interfaceZoom-1 || row.H > 42*interfaceZoom+1 {
				t.Fatalf("账号行不适合双行信息: %v", row)
			}
			list := assertVisible(t, tester, "账号列表", float32(size[0]), float32(size[1]))
			if list.H > 3*row.H+1 {
				t.Fatal("账号列表挤占结果区")
			}
			bar := assertVisible(t, tester, agy.WindowTip(agy.Window5h, usage.FiveHour.Remaining, usage.FiveHour.Reset, now), float32(size[0]), float32(size[1]))
			if bar.W < 40 {
				t.Fatalf("用量条过窄: %v", bar)
			}
			task := assertVisible(t, tester, "正在处理的插画", float32(size[0]), float32(size[1]))
			if float32(size[0])/interfaceZoom < 1150 && (task.Y < row.Y+14*interfaceZoom || task.Y+task.H > row.Y+row.H) {
				t.Fatal("窄窗口的当前任务没有放在账号下方")
			}
		})
	}
}
