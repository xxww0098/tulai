package ui

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestExportAllSaaSPages(t *testing.T) {
	dir := os.Getenv("TULAI_ALL_SHOTS")
	if dir == "" {
		t.Skip("set TULAI_ALL_SHOTS to write all SaaS screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	prevLog := logging.Log
	logging.Log = logging.New(64)
	t.Cleanup(func() { logging.Log = prevLog })
	logging.Log.Info("调度引擎就绪 token=secret-demo")
	logging.Emit(logging.Info, logging.Line{
		Platform: "豆包", Account: "豆包主力", Stage: "submit", Msg: "已提交生图任务 #1", Elapsed: 1500 * time.Millisecond,
	})
	logging.Log.Success("任务 #2 图片已保存到 图片/输出结果")
	logging.Log.Warning("千问运营触发风控冷却，等待 45s 自动重试")
	logging.Log.Errorf("ChatGPT 备用连接超时，已切换到下一可用账号")
	logging.Log.Debugf("pool snapshot: ready=5 cooling=1 running=3")

	tmp := t.TempDir()
	pics := filepath.Join(tmp, "pics")
	dedupPics := filepath.Join(tmp, "dedup-pics")
	renameDir := filepath.Join(tmp, "rename-files")
	for _, p := range []string{pics, dedupPics, renameDir} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	land := filepath.Join(pics, "商品主图.png")
	face := filepath.Join(pics, "模特人像.png")
	still := filepath.Join(pics, "陶瓷花瓶.png")
	outImg := filepath.Join(pics, "成片输出.png")
	writeShot(t, land, 640, 360, func(x, y, w, h int) color.NRGBA {
		if (x-w/2)*(x-w/2)+(y-h/3)*(y-h/3) < 48*48 {
			return color.NRGBA{R: 250, G: 200, B: 60, A: 255}
		}
		if y > h*2/3 {
			return color.NRGBA{R: 40, G: 140, B: 70, A: 255}
		}
		return color.NRGBA{R: 70, G: 150, B: uint8(70 + 140*y/h), A: 255}
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
	writeShot(t, outImg, 512, 512, func(x, y, w, h int) color.NRGBA {
		dx, dy := x-w/2, y-h/2
		if dx*dx+dy*dy < 120*120 {
			return color.NRGBA{R: 255, G: 120, B: 80, A: 255}
		}
		return color.NRGBA{R: 20, G: 30, B: 50, A: 255}
	})

	dupHi := filepath.Join(dedupPics, "海报-高清.png")
	dupLo := filepath.Join(dedupPics, "海报-缩略.png")
	dupOdd := filepath.Join(dedupPics, "独立素材.png")
	writeShot(t, dupHi, 640, 480, func(x, y, w, h int) color.NRGBA {
		if x > w*2/3 && y < h/3 {
			return color.NRGBA{R: 28, G: 38, B: 64, A: 255}
		}
		return color.NRGBA{
			R: uint8(x * 255 / (w - 1)),
			G: uint8(y * 255 / (h - 1)),
			B: 180,
			A: 255,
		}
	})
	writeShot(t, dupLo, 480, 360, func(x, y, w, h int) color.NRGBA {
		if x > w*2/3 && y < h/3 {
			return color.NRGBA{R: 28, G: 38, B: 64, A: 255}
		}
		return color.NRGBA{
			R: uint8(x * 255 / (w - 1)),
			G: uint8(y * 255 / (h - 1)),
			B: 180,
			A: 255,
		}
	})
	writeShot(t, dupOdd, 320, 240, func(x, y, w, h int) color.NRGBA {
		if x < w/3 && y > h*2/3 {
			return color.NRGBA{R: 245, G: 190, B: 66, A: 255}
		}
		return color.NRGBA{
			R: uint8(255 - x*200/(w-1)),
			G: uint8(90 + y*140/(h-1)),
			B: 210,
			A: 255,
		}
	})

	var renamePaths []string
	for _, name := range []string{"SKU_001_白底主图.png", "SKU_002_场景图.png", "SKU_003_细节图.png", "SKU_004_包装图.png"} {
		p := filepath.Join(renameDir, name)
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		renamePaths = append(renamePaths, p)
	}

	agyHome := filepath.Join(tmp, "agy-acc1")
	writeAgyHome(t, agyHome, "studio@example.com")
	today := time.Now().Format("2006-01-02")
	const shotOut = "图片/输出结果"

	multiTenantBrowsers := func() []store.Browser {
		cool := loggedLane(3, "豆包验证", "豆包")
		cool.Health = store.HealthCooling
		cool.LastError = "需要人工验证"
		cool.CoolUntil = time.Now().Add(45 * time.Second).Unix()
		cool.DayCount = 19

		qwNeed := loggedLane(5, "千问待登录", "千问")
		qwNeed.IsLoggedIn = false
		qwNeed.DayCount = 4

		doubaoMain := loggedLane(1, "豆包主力", "豆包")
		doubaoMain.IsRunning = true
		doubaoMain.Proxy = "http://127.0.0.1:7890"
		doubaoMain.DayCount = 14

		doubaoBackup := loggedLane(2, "豆包备用", "豆包")
		doubaoBackup.DayCount = 6

		qwMain := loggedLane(4, "千问运营", "千问")
		qwMain.IsRunning = true
		qwMain.DayCount = 9

		agyAcc := store.Browser{
			ID: 6, Name: "agy-设计", Platform: agy.Platform, UserDataDir: agyHome,
			IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 3,
		}

		gptMain := loggedLane(7, "GPT-主号", "ChatGPT")
		gptMain.Proxy = "socks5://127.0.0.1:1080"
		gptMain.DayCount = 5

		gptBack := loggedLane(8, "GPT-备用", "ChatGPT")
		gptBack.Health = store.HealthExhausted
		gptBack.LastError = "今日额度已用尽"
		gptBack.DayCount = 20

		return []store.Browser{doubaoMain, doubaoBackup, cool, qwMain, qwNeed, agyAcc, gptMain, gptBack}
	}

	multiTenantUsage := func() map[int64]agyUsageSlot {
		return map[int64]agyUsageSlot{
			6: {view: agy.UsageView{
				State: agy.UsageOK,
				At:    time.Now(),
				Usage: agy.GeminiUsage{
					FiveHour: agy.UsageWindow{Remaining: 0.72, Reset: time.Now().Add(2 * time.Hour)},
					Weekly:   agy.UsageWindow{Remaining: 0.46, Reset: time.Now().Add(48 * time.Hour)},
				},
			}},
		}
	}

	buildHome := func(stageTab int) *App {
		a := &App{
			tab:             pageHome,
			statePage:       pageHome,
			runPage:         pageHome,
			stageTab:        stageTab,
			stagePicked:     true,
			platformAuto:    true,
			concurrencyAuto: true,
			running:         true,
			dailyCap:        20,
			outputPath:      shotOut,
			prompts:         "极简北欧陶瓷花瓶，柔和自然光\n城市夜景霓虹雨后倒影，电影感构图",
			browserRows:     multiTenantBrowsers(),
			agyUsage:        multiTenantUsage(),
			sources:         []sourceImage{{Path: land, Hash: "land"}, {Path: face, Hash: "face"}},
			queue: []tasks.Task{
				{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Images: []string{land}, Prompt: "极简北欧陶瓷花瓶，柔和自然光"},
				{Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusSuccess, Images: []string{face}, Prompt: "保留构图换成胶片色调", Results: []string{outImg}},
				{Index: 3, Platform: "豆包", BrowserID: 3, Status: tasks.StatusNeedsUser, Images: []string{still}, Prompt: "电商白底精修", Error: "触发人工验证"},
				{Index: 4, Platform: "豆包", Status: tasks.StatusPending, Prompt: "城市夜景霓虹雨后倒影"},
				{Index: 5, Platform: agy.Platform, BrowserID: 6, Status: tasks.StatusRunning, Prompt: "Studio lighting product shot"},
				{Index: 6, Platform: agy.Platform, BrowserID: 6, Status: tasks.StatusSuccess, Prompt: "Minimalist desk accessory", Results: []string{outImg}},
				{Index: 7, Platform: "ChatGPT", BrowserID: 7, Status: tasks.StatusSuccess, Prompt: "Editorial perfume bottle on slate", Results: []string{outImg}},
			},
		}
		a.preloadThumbs([]string{land, face, still, outImg})
		return a
	}

	buildWash := func() *App {
		a := &App{
			tab:             pageWash,
			statePage:       pageWash,
			runPage:         pageWash,
			stageTab:        0,
			stagePicked:     true,
			platformAuto:    true,
			concurrencyAuto: true,
			running:         true,
			dailyCap:        20,
			outputPath:      shotOut,
			prompts:         DefaultWashPrompt,
			browserRows:     multiTenantBrowsers(),
			agyUsage:        multiTenantUsage(),
			sources: []sourceImage{
				{Path: land, Hash: "land", Washing: true},
				{Path: face, Hash: "face", Washed: true, WashOutput: outImg},
				{Path: still, Hash: "still"},
			},
			queue: []tasks.Task{
				{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Images: []string{land}, Prompt: DefaultWashPrompt},
				{Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusSuccess, Images: []string{face}, Prompt: DefaultWashPrompt, Results: []string{outImg}},
				{Index: 3, Platform: "豆包", BrowserID: 3, Status: tasks.StatusFailed, Images: []string{still}, Prompt: DefaultWashPrompt, Error: "账号冷却中"},
			},
		}
		a.preloadThumbs([]string{land, face, still, outImg})
		return a
	}

	buildFission := func() *App {
		a := &App{
			tab:             pageFission,
			statePage:       pageFission,
			runPage:         pageFission,
			stageTab:        0,
			stagePicked:     true,
			platformAuto:    true,
			concurrencyAuto: true,
			running:         true,
			dailyCap:        20,
			outputPath:      shotOut,
			prompts:         DefaultFissionPrompt,
			browserRows:     multiTenantBrowsers(),
			agyUsage:        multiTenantUsage(),
			sources:         []sourceImage{{Path: still, Hash: "still"}},
			queue: []tasks.Task{
				{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess, Images: []string{still}, Prompt: "近景特写，突出纹理细节", Results: []string{outImg}},
				{Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusRunning, Images: []string{still}, Prompt: "远景展示完整场景氛围"},
				{Index: 3, Platform: "豆包", BrowserID: 3, Status: tasks.StatusNeedsReview, Images: []string{still}, Prompt: "俯视角度展示轮廓布局", Results: []string{outImg}, Error: "裂变相似度过高 (0.94)"},
				{Index: 4, Platform: "豆包", Status: tasks.StatusPending, Images: []string{still}, Prompt: "侧视 45 度立体构图"},
			},
		}
		a.preloadThumbs([]string{still, outImg})
		return a
	}

	buildDedup := func() *App {
		a := &App{
			engine:      tasks.NewEngine(nil, nil, nil, ""),
			tab:         pageDedup,
			outputPath:  shotOut,
			browserRows: multiTenantBrowsers(),
		}
		a.dedup.level = dedupDefaultLevel
		a.dedup.outDir = filepath.Join(tmp, "dedup-out")
		a.dedupIngest([]string{dedupPics})
		a.dedupStart()
		waitDedup(t, a)
		a.engine = nil
		a.thumbs = map[string]*ui.Bitmap{}
		for _, p := range []string{dupHi, dupLo, dupOdd} {
			if img, err := images.CoverThumb(p, thumbPx); err == nil && img != nil {
				a.thumbs[p] = ui.NewBitmap(img)
			}
		}
		return a
	}

	buildBrowser := func() *App {
		var rows []store.Browser
		for _, b := range multiTenantBrowsers() {
			if b.Platform != agy.Platform {
				rows = append(rows, b)
			}
		}
		a := &App{
			tab:            pageBrowser,
			platformAuto:   true,
			camoufoxReady:  true,
			detectedNote:   "已检测到内置 Camoufox",
			camoufoxStatus: "Camoufox 已就绪",
			newBrowserPlat: "豆包",
			dailyCap:       20,
			extToken:       "tulai-bridge-demo-token-2026",
			extPort:        "39281",
			browserSel:     0,
			browserRows:    rows,
		}
		a.browserChecked.Add(1)
		return a
	}

	buildPrompts := func() *App {
		a := &App{
			tab:         pagePrompts,
			browserRows: multiTenantBrowsers(),
			userPrompts: []prompts.UserPrompt{
				{ID: "u-ecom", Label: "电商白底精修", Kind: prompts.KindWash, Body: "保留商品主体结构与材质纹理，纯白无影棚背景，去除贴纸水印。"},
				{ID: "u-editorial", Label: "杂志质感静物", Kind: prompts.KindGen, Body: "柔焦晨光穿过百叶窗，石膏纹理台面，35mm 胶片颗粒质感。"},
			},
			promptEditChoice: "电商白底精修",
		}
		a.applyPromptChoice()
		return a
	}

	buildDownload := func() *App {
		return &App{
			tab:          pageDownload,
			dlSelectedID: 1,
			dlDir:        shotOut,
			browserRows:  multiTenantBrowsers(),
			dlTasks: []*downloadTask{
				{ID: 1, URL: "https://cdn.example.com/posts/2026/11/hero-01.jpg", Name: "hero-01.jpg", Site: "cdn.example.com", Dir: shotOut, Phase: dlActive, Written: 3_400_000, Total: 8_100_000, Speed: 3_100_000},
				{ID: 2, URL: "https://cdn.example.com/posts/2026/11/hero-02.jpg", Name: "hero-02.jpg", Site: "cdn.example.com", Dir: shotOut, Phase: dlQueued, Total: 4_200_000},
				{ID: 3, URL: "https://pbs.example.org/media/portrait.jpg", Name: "portrait.jpg", Site: "pbs.example.org", Dir: shotOut, Phase: dlPaused, Written: 900_000, Total: 5_000_000},
				{ID: 4, URL: "https://pbs.example.org/media/street.jpg", Name: "street.jpg", Site: "pbs.example.org", Dir: shotOut, Phase: dlDone, Written: 2_000_000, Total: 2_000_000},
				{ID: 5, URL: "https://img.example.net/a/flower.jpg", Name: "flower.jpg", Site: "img.example.net", Dir: shotOut, Phase: dlFailed, Err: "服务器返回 403"},
			},
		}
	}

	buildRename := func() *App {
		a := &App{
			tab:         pageRename,
			browserRows: multiTenantBrowsers(),
		}
		a.fileRename.init()
		a.renameIngest(renamePaths)
		a.fileRename.rules = []renameRule{{Find: "SKU_", With: "2026_主图_", count: 1, offset: 1}}
		return a
	}

	buildImageEdit := func() *App {
		a := &App{
			tab:         pageImageEdit,
			browserRows: multiTenantBrowsers(),
		}
		paths := []string{land, face, still}
		for i, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			bmp := ui.NewBitmap(img)
			a.edit.files = append(a.edit.files, &editFile{
				Path:   p,
				Name:   filepath.Base(p),
				Status: "pending",
				Thumb:  bmp,
			})
			if i == 0 {
				a.edit.orig = bmp
				a.edit.out = bmp
				a.edit.srcW, a.edit.srcH = img.Bounds().Dx(), img.Bounds().Dy()
				a.edit.outW, a.edit.outH = 640, 360
				a.edit.origPath = p
			}
		}
		a.edit.selected = 0
		a.edit.locked = true
		a.edit.mode = "crop"
		a.edit.split = 50
		a.edit.zoom = 1
		a.editEnsureMaps()
		a.edit.enabled.Crop = true
		a.edit.expanded["crop"] = true
		a.edit.opts.CropWidth, a.edit.opts.CropHeight = 640, 360
		a.edit.dimW, a.edit.dimH = 640, 360
		a.edit.dimWText, a.edit.dimHText = "640", "360"
		a.edit.enabled.Adjust = true
		a.edit.expanded["adjust"] = true
		a.edit.opts.Brightness = 12
		a.edit.opts.Contrast = 8
		a.edit.previewSig = a.editPreviewSignature(paths[0])
		return a
	}

	buildSettings := func(subTab int) *App {
		a := settingsShot(shotOut)
		a.settingsTab = subTab
		a.browserRows = multiTenantBrowsers()
		return a
	}

	buildWizard := func() *App {
		return &App{
			wizardOn:     true,
			wizardFirst:  true,
			wizardStep:   wizard.StepPlatform,
			wizardChoice: "豆包",
			wizardPlan: wizard.BrowserPlan{
				Backend: browser.KindCamoufox,
				Kind:    browser.KindCamoufox,
				Title:   "Camoufox 已就绪",
				Detail:  "内置指纹浏览器已准备就绪",
				CanNext: true,
			},
			wizardPlanFn: func() wizard.BrowserPlan {
				return wizard.BrowserPlan{Backend: browser.KindCamoufox, Kind: browser.KindCamoufox, Title: "Camoufox 已就绪", CanNext: true}
			},
			agyLocate: func(string) (string, bool) { return "/usr/local/bin/agy", true },
		}
	}

	buildLog := func() *App {
		a := buildHome(0)
		a.logOpen = true
		return a
	}

	render := func(name string, w, h int, dark bool, build func() *App) {
		t.Helper()
		a := build()
		view := func(c *ui.Context) {
			theme := *c.Theme()
			theme.Dark = dark
			c.SetTheme(&theme)
			a.view(c)
		}
		tt := ui.NewTester(view, w, h)
		tt.SetDark(dark)
		tt.Advance(450 * time.Millisecond)
		saveFrame(t, dir, name, tt)
	}

	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		render("home-1280x800-"+mode, 1280, 800, dark, func() *App { return buildHome(0) })
		render("home-agy-1280x800-"+mode, 1280, 800, dark, func() *App { return buildHome(2) })
		render("home-chatgpt-1280x800-"+mode, 1280, 800, dark, func() *App { return buildHome(3) })
		render("wash-1280x800-"+mode, 1280, 800, dark, buildWash)
		render("fission-1280x800-"+mode, 1280, 800, dark, buildFission)
		render("dedup-1280x800-"+mode, 1280, 800, dark, buildDedup)
		render("browser-1280x800-"+mode, 1280, 800, dark, buildBrowser)
		render("prompts-1280x800-"+mode, 1280, 800, dark, buildPrompts)
		render("download-1280x800-"+mode, 1280, 800, dark, buildDownload)
		render("rename-1280x800-"+mode, 1280, 800, dark, buildRename)
		render("imageedit-1280x800-"+mode, 1280, 800, dark, buildImageEdit)
		render("settings-1280x800-"+mode, 1280, 800, dark, func() *App { return buildSettings(settingsOutput) })
		render("settings-advanced-1280x800-"+mode, 1280, 800, dark, func() *App { return buildSettings(settingsAdvanced) })
		render("wizard-1280x800-"+mode, 1280, 800, dark, buildWizard)
		render("log-1280x800-"+mode, 1280, 800, dark, buildLog)

		for _, compact := range []struct {
			name  string
			build func() *App
		}{
			{"home", func() *App { return buildHome(0) }},
			{"browser", buildBrowser},
			{"download", buildDownload},
			{"settings", func() *App { return buildSettings(settingsOutput) }},
		} {
			render(fmt.Sprintf("%s-1024x700-%s", compact.name, mode), 1024, 700, dark, compact.build)
		}
	}
}
