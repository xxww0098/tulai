package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestExportWorkbenchShots(t *testing.T) {
	dir := os.Getenv("TULAI_SHOTS")
	if dir == "" {
		t.Skip("set TULAI_SHOTS to write workbench frames")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pics := filepath.Join(t.TempDir(), "pics")
	if err := os.MkdirAll(pics, 0o755); err != nil {
		t.Fatal(err)
	}
	land := filepath.Join(pics, "风景.png")
	face := filepath.Join(pics, "人像.png")
	still := filepath.Join(pics, "静物.png")
	out := filepath.Join(pics, "猫.png")
	writeShot(t, land, 640, 360, func(x, y, w, h int) color.NRGBA {
		sky := uint8(70 + 140*y/h)
		if (x-w/2)*(x-w/2)+(y-h/3)*(y-h/3) < 48*48 {
			return color.NRGBA{R: 250, G: 200, B: 60, A: 255}
		}
		if y > h*2/3 {
			return color.NRGBA{R: 40, G: 140, B: 70, A: 255}
		}
		return color.NRGBA{R: 70, G: 150, B: sky, A: 255}
	})
	writeShot(t, face, 360, 640, func(x, y, w, h int) color.NRGBA {
		dx, dy := x-w/2, y-h/3
		if dx*dx+dy*dy < 70*70 {
			return color.NRGBA{R: 230, G: 170, B: 140, A: 255}
		}
		return color.NRGBA{R: 40, G: 55, B: 90, A: 255}
	})
	writeShot(t, still, 480, 480, func(x, y, w, h int) color.NRGBA {
		dx, dy := x-w/2, y-h/2+20
		if dx*dx+dy*dy < 90*90 {
			return color.NRGBA{R: 150, G: 70, B: 170, A: 255}
		}
		return color.NRGBA{R: 245, G: 236, B: 220, A: 255}
	})
	writeShot(t, out, 512, 512, func(x, y, w, h int) color.NRGBA {
		dx, dy := x-w/2, y-h/2
		if dx*dx+dy*dy < 120*120 {
			return color.NRGBA{R: 255, G: 120, B: 80, A: 255}
		}
		return color.NRGBA{R: 20, G: 30, B: 50, A: 255}
	})

	save := func(name string, a *App) {
		t.Helper()
		tt := ui.NewTester(a.view, 1280, 900)
		tt.Advance(400 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

	const shotOut = "图片/输出结果"
	save("after-empty", &App{platformAuto: true, outputPath: shotOut})
	save("gen-prompts", &App{
		platformAuto: true, outputPath: shotOut,
		prompts: "一只坐在窗边的猫\n傍晚的街景，暖光\n一束白花",
	})
	save("settings", settingsShot(shotOut))
	var twelve []store.Browser
	today := time.Now().Format("2006-01-02")
	for i := 1; i <= 12; i++ {
		b := store.Browser{ID: int64(i), Name: fmt.Sprintf("豆包%02d", i), Platform: "豆包", IsLoggedIn: true,
			Health: store.HealthReady, DayKey: today, DayCount: (i * 7) % 21}
		if i == 5 {
			b.IsLoggedIn, b.Health, b.LastError = false, store.HealthExhausted, "未登录"
		}
		twelve = append(twelve, b)
	}
	busy := []tasks.Task{
		{Index: 1, Platform: "豆包", BrowserID: 1, Prompt: "提取图中的商品主体，去掉所有文字、水印和背景装饰", Images: []string{land}, Status: tasks.StatusRunning},
		{Index: 2, Platform: "豆包", BrowserID: 2, Prompt: "提取图中的商品主体", Images: []string{face}, Status: tasks.StatusRunning},
		{Index: 3, Platform: "豆包", Images: []string{still}, Status: tasks.StatusPending},
	}
	save("accounts-12-gen", &App{platformAuto: true, concurrencyAuto: true, outputPath: shotOut, browserRows: twelve, prompts: "一只猫", queue: busy, running: true})
	save("accounts-12-wash", &App{platformAuto: true, concurrencyAuto: true, outputPath: shotOut, browserRows: twelve, tab: pageWash, statePage: pageWash, runPage: pageWash,
		sources: []sourceImage{{Path: land, Hash: "land"}, {Path: face, Hash: "face"}, {Path: still, Hash: "still"}}, queue: busy, running: true})
	save("wash-empty", &App{platformAuto: true, outputPath: shotOut, tab: pageWash, statePage: pageWash})
	washShot := &App{
		platformAuto: true, outputPath: shotOut, tab: pageWash, statePage: pageWash, flowPin: 1, srcSel: -1,
		sources: []sourceImage{{Path: land, Hash: "land"}, {Path: face, Hash: "face"}, {Path: still, Hash: "still"}},
	}
	washShot.preloadThumbs([]string{land, face, still})
	save("wash", washShot)

	picked := &App{
		platformAuto: true,
		outputPath:   shotOut,
		sources: []sourceImage{
			{Path: land, Hash: "land"},
			{Path: face, Hash: "face"},
		},
	}
	picked.preloadThumbs([]string{land, face})
	save("after-selected", picked)

	run := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		running:         true,
		platDoubao:      true,
		outputPath:      shotOut,
		prompts:         "水彩，保留构图",
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包主力", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady},
			{ID: 2, Name: "千问备用", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady},
		},
		sources: []sourceImage{
			{Path: land, Hash: "land"},
			{Path: face, Hash: "face"},
		},
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusPending, Images: []string{land}, Prompt: "水彩"},
			{Index: 2, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Images: []string{face}, Prompt: "水彩"},
			{Index: 3, Platform: "千问", BrowserID: 2, Status: tasks.StatusFailed, Images: []string{still}, Prompt: "水彩", Error: "账号额度已用尽"},
			{Index: 4, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess, Prompt: "文生图一只猫", Results: []string{out}},
		},
	}
	run.preloadThumbs([]string{land, face, still, out})
	for i := 1; i <= 30; i++ {
		logging.Log.Info("#%d 豆包「豆包主力」第 %d 行日志", i, i)
	}
	save("after-run", run)

	saveTall := func(name string, a *App) {
		t.Helper()
		tt := ui.NewTester(a.view, 1280, 1040)
		tt.Advance(450 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	today = time.Now().Format("2006-01-02")
	idle := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		outputPath:      shotOut,
		prompts:         "水彩，保留构图",
		sources:         []sourceImage{{Path: land, Hash: "land"}, {Path: face, Hash: "face"}},
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包主力", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 3},
			{ID: 2, Name: "千问备用", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 1},
		},
	}
	idle.preloadThumbs([]string{land, face})
	saveTall("lanes-idle", idle)

	running := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		running:         true,
		outputPath:      shotOut,
		prompts:         "水彩，保留构图",
		sources:         []sourceImage{{Path: land, Hash: "land"}, {Path: face, Hash: "face"}, {Path: still, Hash: "still"}},
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包主力", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 4},
			{ID: 2, Name: "豆包小号", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 2},
			{ID: 3, Name: "千问备用", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 6},
		},
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Images: []string{land}, Prompt: "水彩，保留构图，傍晚的光"},
			{Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusRunning, Images: []string{face}, Prompt: "人像改成胶片色"},
			{Index: 3, Platform: "千问", BrowserID: 3, Status: tasks.StatusRunning, Images: []string{still}, Prompt: "静物换成暖光"},
		},
	}
	running.preloadThumbs([]string{land, face, still, out})
	saveTall("lanes-running", running)

	blocked := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		outputPath:      shotOut,
		dailyCap:        20,
		prompts:         "一只猫",
		sources:         []sourceImage{{Path: land, Hash: "land"}},
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包没登录", Platform: "豆包", Health: store.HealthExhausted, LastError: "未登录"},
			{ID: 2, Name: "千问今天满了", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 20},
		},
	}
	blocked.preloadThumbs([]string{land})
	saveTall("lanes-blocked", blocked)

	saveAt := func(name string, width, height int, app *App) {
		t.Helper()
		tt := ui.NewTester(app.view, width, height)
		tt.Advance(450 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	extra := []struct{ file, prompt string }{
		{"街景.png", "街边的傍晚"},
		{"夜色.png", "深蓝的夜色"},
		{"花.png", "一束白花"},
	}
	var extraSrc []sourceImage
	var extraQueue []tasks.Task
	for i, item := range extra {
		path := filepath.Join(pics, item.file)
		writeShot(t, path, 320, 320, func(x, y, w, h int) color.NRGBA {
			return color.NRGBA{R: uint8(40 + i*60), G: uint8(80 + x%80), B: uint8(120 + y%80), A: 255}
		})
		extraSrc = append(extraSrc, sourceImage{Path: path})
		extraQueue = append(extraQueue, tasks.Task{
			Index: 4 + i, Platform: "豆包", Status: tasks.StatusPending,
			Images: []string{path}, Prompt: item.prompt,
		})
	}
	six := &App{
		platformAuto: running.platformAuto, concurrencyAuto: running.concurrencyAuto,
		running: true, outputPath: running.outputPath, prompts: running.prompts,
		browserRows: running.browserRows,
		sources:     append(append([]sourceImage{}, running.sources...), extraSrc...),
		queue:       append(append([]tasks.Task{}, running.queue...), extraQueue...),
	}
	six.preloadThumbs(extraPaths(six.sources))
	saveAt("lanes-running-1024", 1024, 1100, running)
	saveAt("lanes-running-6", 1280, 1200, six)
	saveAt("lanes-running-6-1024", 1024, 1400, six)

	for _, sz := range [][2]int{{1280, 800}, {1024, 700}} {
		tag := fmt.Sprintf("%dx%d", sz[0], sz[1])
		saveAt("home-idle-"+tag, sz[0], sz[1], idle)
		saveAt("home-running-"+tag, sz[0], sz[1], six)
		saveAt("home-blocked-"+tag, sz[0], sz[1], blocked)
		// 列表状态不能跨两个 Tester，每个尺寸用一份新的页面状态。
		saveAt("browser-"+tag, sz[0], sz[1], &App{
			tab: pageBrowser, platformAuto: true, browserChoice: "Chrome", newBrowserPlat: "豆包",
			detectedNote: "已检测到 Chrome", cftStatus: "内置浏览器未下载",
			browserRows: []store.Browser{
				{ID: 1, Name: "豆包主力", Platform: "豆包", Port: 9333, IsLoggedIn: true, Health: store.HealthReady},
				{ID: 2, Name: "千问备用", Platform: "千问", Port: 9334, IsLoggedIn: true, Health: store.HealthReady},
			},
		})
		saveAt("settings-"+tag, sz[0], sz[1], settingsShot(shotOut))
	}
	agyHome := filepath.Join(t.TempDir(), "acc1")
	writeAgyHome(t, agyHome, "li@gmail.com")
	y, m, d := time.Now().Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, time.Local).Unix()
	agyAccount := func(exhausted bool) []store.Browser {
		row := store.Browser{
			ID: 1, Name: "acc1", Platform: "Antigravity", UserDataDir: agyHome,
			IsLoggedIn: true, Health: store.HealthReady,
		}
		if exhausted {
			row.Health = store.HealthExhausted
			row.LastError = "额度不足"
			row.CoolUntil = midnight
		}
		return []store.Browser{row}
	}
	agyBase := func(exhausted bool) *App {
		return &App{
			platformAuto: true, stageTab: 2, stagePicked: true, outputPath: shotOut,
			prompts:     "一只坐在窗边的猫",
			browserRows: agyAccount(exhausted),
		}
	}
	saveAt("antigravity-gen", 1024, 700, agyBase(false))
	washAgy := agyBase(false)
	washAgy.tab, washAgy.statePage = pageWash, pageWash
	washAgy.sources = []sourceImage{{Path: land, Hash: "land"}}
	washAgy.preloadThumbs([]string{land})
	saveAt("antigravity-wash", 1024, 700, washAgy)
	saveAt("antigravity-wash-1280x800", 1280, 800, washAgy)
	saveAt("wash-1280x800", 1280, 800, washShot)
	loginAgy := agyBase(false)
	loginAgy.agyLogin = &agyLoginState{url: "https://accounts.google.com/o/oauth2/auth?client_id=agy", name: "acc1"}
	saveAt("antigravity-login", 1024, 700, loginAgy)
	saveAt("antigravity-exhausted", 1024, 700, agyBase(true))
	chooser := agyBase(false)
	chooser.agyChooser = &agyChooserState{hint: agy.MsgNotFound}
	saveAt("antigravity-add-chooser", 1024, 700, chooser)
	detected := agyBase(false)
	detected.agyChooser = &agyChooserState{
		hint: "已检测到 li@gmail.com", email: "li@gmail.com", home: agyHome, canImport: true,
	}
	saveAt("antigravity-import-detected", 1024, 700, detected)
	var washManySrc []sourceImage
	var washManyPaths []string
	for i, path := range []string{land, face, still, filepath.Join(pics, "街景.png"), filepath.Join(pics, "夜色.png"), filepath.Join(pics, "花.png")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		washManySrc = append(washManySrc, sourceImage{Path: path, Hash: fmt.Sprintf("w%d", i)})
		washManyPaths = append(washManyPaths, path)
	}
	washMany := &App{
		platformAuto: true, outputPath: shotOut, tab: pageWash, statePage: pageWash,
		sources: washManySrc,
	}
	washMany.preloadThumbs(washManyPaths)
	saveAt("wash-many-1024x700", 1024, 700, washMany)
	shotStore, err := store.Open(filepath.Join(t.TempDir(), "shots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer shotStore.Close()
	if row, err := shotStore.AddBrowser("豆包", "豆包主力", ""); err != nil {
		t.Fatal(err)
	} else {
		shotStore.UpdateBrowser(row.ID, false, true)
	}
	seedHistory := func(page, prompts, images string) {
		t.Helper()
		id, err := shotStore.SaveActive(store.Session{
			Page: page, Prompts: prompts, Images: images,
		}, []store.SessionTask{{
			Ord: 1, Prompt: prompts, Source: images, Platform: "豆包",
			Status: "成功", Outputs: out, At: time.Now().Unix(),
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := shotStore.ArchiveSession(id); err != nil {
			t.Fatal(err)
		}
	}
	seedHistory(store.PageGen, "傍晚窗边的一只猫", land)
	seedHistory(store.PageWash, "去掉水印", land)
	twoRows := []store.Browser{loggedLane(1, "豆包主力", "豆包"), loggedLane(2, "豆包小号", "豆包")}
	oneAcc := func() *App {
		return &App{
			platformAuto: true, concurrencyAuto: true, outputPath: shotOut,
			browserRows: []store.Browser{loggedLane(1, "豆包主力", "豆包")},
		}
	}
	twoAcc := func() *App {
		a := &App{
			platformAuto: true, concurrencyAuto: true, outputPath: shotOut,
			prompts:     "水彩，保留构图\n人像改成胶片色",
			browserRows: append([]store.Browser{}, twoRows...),
			sources:     []sourceImage{{Path: land}, {Path: face}},
			queue: []tasks.Task{
				{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Prompt: "水彩，保留构图", Images: []string{land}},
				{Index: 2, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess, Prompt: "水彩，保留构图", Results: []string{out}},
				{Index: 3, Platform: "豆包", BrowserID: 2, Status: tasks.StatusFailed, Prompt: "人像改成胶片色", Images: []string{face}, Error: "出图失败"},
			},
		}
		a.preloadThumbs([]string{land, face, out})
		return a
	}
	dozenAcc := func() *App {
		a := &App{
			platformAuto: true, concurrencyAuto: true, outputPath: shotOut, prompts: "一只猫",
			browserRows: append([]store.Browser{}, twelve...),
			queue: []tasks.Task{
				{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess, Prompt: "一只猫", Results: []string{out}},
				{Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusRunning, Prompt: "街边的傍晚", Images: []string{land}},
				{Index: 3, Platform: "豆包", BrowserID: 3, Status: tasks.StatusPending, Prompt: "一束白花", Images: []string{still}},
			},
		}
		a.preloadThumbs([]string{out, land, still})
		return a
	}
	missingAcc := func() *App {
		a := &App{
			platformAuto: true, concurrencyAuto: true, outputPath: shotOut, prompts: "一只猫",
			browserRows: []store.Browser{loggedLane(1, "豆包主力", "豆包")},
			queue: []tasks.Task{{
				Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess,
				Prompt: "一只猫", Results: []string{filepath.Join(pics, "不存在.png"), out},
			}},
		}
		a.preloadThumbs([]string{out})
		return a
	}
	asWash := func(a *App) *App {
		a.tab, a.statePage = pageWash, pageWash
		if a.prompts == "" {
			a.prompts = DefaultWashPrompt
		}
		return a
	}
	historyAcc := func(wash bool) *App {
		a := oneAcc()
		a.store = shotStore
		a.historyOpen = true
		a.prompts = "当前提示词"
		if wash {
			a.tab, a.statePage = pageWash, pageWash
			a.prompts = "白底"
			a.sources = []sourceImage{{Path: land}}
			a.preloadThumbs([]string{land})
		}
		return a
	}
	for _, sz := range [][2]int{{1280, 800}, {1024, 700}} {
		tag := fmt.Sprintf("%dx%d", sz[0], sz[1])
		saveAt("right-1acc-empty-"+tag, sz[0], sz[1], oneAcc())
		saveAt("right-2acc-results-"+tag, sz[0], sz[1], twoAcc())
		saveAt("right-12acc-results-"+tag, sz[0], sz[1], dozenAcc())
		saveAt("history-open-"+tag, sz[0], sz[1], historyAcc(false))
		saveAt("missing-file-"+tag, sz[0], sz[1], missingAcc())
		w2 := twoAcc()
		w2.prompts = "白底，去掉水印"
		w2.sources = []sourceImage{{Path: land}, {Path: face}, {Path: still}}
		w2.queue = []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Prompt: "白底，去掉水印", Images: []string{land}},
			{Index: 2, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess, Prompt: "白底，去掉水印", Images: []string{face}, Results: []string{out}},
			{Index: 3, Platform: "豆包", BrowserID: 2, Status: tasks.StatusFailed, Prompt: "白底，去掉水印", Images: []string{still}, Error: "出图失败"},
		}
		w2.preloadThumbs([]string{land, face, still, out})
		w12 := dozenAcc()
		w12.prompts = "白底，去掉水印"
		w12.sources = []sourceImage{{Path: land}, {Path: still}}
		w12.queue = []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess, Prompt: "白底，去掉水印", Images: []string{land}, Results: []string{out}},
			{Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusRunning, Prompt: "白底，去掉水印", Images: []string{still}},
		}
		w12.preloadThumbs([]string{land, still, out})
		wm := missingAcc()
		wm.prompts = "白底，去掉水印"
		wm.sources = []sourceImage{{Path: land}}
		saveAt("wash-right-1acc-empty-"+tag, sz[0], sz[1], asWash(oneAcc()))
		saveAt("wash-right-2acc-results-"+tag, sz[0], sz[1], asWash(w2))
		saveAt("wash-right-12acc-results-"+tag, sz[0], sz[1], asWash(w12))
		saveAt("wash-history-open-"+tag, sz[0], sz[1], historyAcc(true))
		saveAt("wash-missing-file-"+tag, sz[0], sz[1], asWash(wm))
	}

	tip := ui.NewTester(idle.view, 1280, 800)
	tip.Advance(450 * time.Millisecond)
	r, ok := tip.Find("检查更新")
	if !ok {
		t.Fatal("nav icon missing")
	}
	tip.Move(r.X+r.W/2, r.Y+r.H/2)
	tip.Advance(700 * time.Millisecond)
	f, err := os.Create(filepath.Join(dir, "nav-tooltip-1280x800.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tip.Image()); err != nil {
		t.Fatal(err)
	}
}

func writeAgyHome(t *testing.T, home, email string) {
	t.Helper()
	dir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := "noise\nOAuth: authenticated successfully as old@example.com\nOAuth: authenticated successfully as " + email + "\n"
	if err := os.WriteFile(filepath.Join(dir, "cli.log"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "antigravity-oauth-token"), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}

func settingsShot(output string) *App {
	return &App{
		tab:             pageSettings,
		platformAuto:    true,
		concurrencyAuto: true,
		autoCheck:       true,
		captchaEnabled:  true,
		captchaBackend:  "free",
		outputPath:      output,
		dailyCap:        20,
		sendStrategy:    "顺序循环",
	}
}

func extraPaths(sources []sourceImage) []string {
	out := make([]string, len(sources))
	for i := range sources {
		out[i] = sources[i].Path
	}
	return out
}

func writeShot(t *testing.T, path string, w, h int, px func(x, y, w, h int) color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, px(x, y, w, h))
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
