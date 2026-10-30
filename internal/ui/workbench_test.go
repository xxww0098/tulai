package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestPlanWorkbenchPicksLoggedInPlatforms(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.Local)
	accounts := []AccountSnap{
		{Platform: "豆包", LoggedIn: true, Health: store.HealthReady},
		{Platform: "千问", LoggedIn: true, Health: store.HealthExhausted},
		{Platform: "豆包", LoggedIn: true, Health: store.HealthCooling, CoolUntil: now.Add(time.Minute).Unix()},
	}
	got := PlanWorkbench(WorkbenchInput{
		HasImages: true, AutoPlatform: true, Accounts: accounts, Now: now,
	})
	if got.Action != StartGenerate || len(got.Platforms) != 1 || got.Platforms[0] != "豆包" {
		t.Fatalf("auto pick = %+v", got)
	}

	both := PlanWorkbench(WorkbenchInput{
		HasPrompts: true, AutoPlatform: true, Now: now,
		Accounts: []AccountSnap{
			{Platform: "千问", LoggedIn: true, Health: store.HealthReady},
			{Platform: "豆包", Extension: true, Health: store.HealthReady},
			{Platform: "豆包", LoggedIn: true, Health: store.HealthReady},
		},
	})
	if both.Action != StartGenerate || len(both.Platforms) != 2 || both.Platforms[0] != "豆包" || both.Platforms[1] != "千问" {
		t.Fatalf("both platforms = %+v", both)
	}

	manual := PlanWorkbench(WorkbenchInput{
		HasImages: true, WantQianwen: true, Now: now,
		Accounts: []AccountSnap{{Platform: "豆包", LoggedIn: true, Health: store.HealthReady}},
	})
	if manual.Action != StartWizard || manual.Platforms != nil {
		t.Fatalf("manual mismatch = %+v", manual)
	}

	ext := PlanWorkbench(WorkbenchInput{
		HasPrompts: true, AutoPlatform: true, ExtensionMode: true, Now: now,
		Accounts: []AccountSnap{
			{Platform: "豆包", LoggedIn: true, Health: store.HealthReady},
			{Platform: "千问", Extension: true, LoggedIn: true, Health: store.HealthReady},
		},
	})
	if ext.Action != StartGenerate || len(ext.Platforms) != 1 || ext.Platforms[0] != "千问" {
		t.Fatalf("extension pick = %+v", ext)
	}
	for _, platform := range []string{"豆包", "千问"} {
		onlyConnected := PlanWorkbench(WorkbenchInput{
			HasPrompts: true, AutoPlatform: true, ExtensionMode: true, Now: now,
			Accounts: []AccountSnap{{Platform: platform, Extension: true, Health: store.HealthReady}},
		})
		if onlyConnected.Action != StartWizard {
			t.Fatalf("%s extension without login = %+v", platform, onlyConnected)
		}
	}
}

func TestPlanWorkbenchStaysOnTheOpenTab(t *testing.T) {
	now := time.Now()
	accounts := []AccountSnap{
		{Platform: "豆包", LoggedIn: true, Health: store.HealthReady},
		{Platform: "千问", LoggedIn: true, Health: store.HealthReady},
	}
	got := PlanWorkbench(WorkbenchInput{
		HasPrompts: true, AutoPlatform: true, OnlyPlatform: "千问", Accounts: accounts, Now: now,
	})
	if got.Action != StartGenerate || len(got.Platforms) != 1 || got.Platforms[0] != "千问" {
		t.Fatalf("%+v", got)
	}
}

func TestPlatformCardsDoNotMixAccounts(t *testing.T) {
	a := &App{
		stageTab: 1,
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包2", Platform: "豆包"},
			{ID: 4, Name: "浏览器4", Platform: "千问"},
		},
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Images: []string{"a.png"}, Prompt: "同一句"},
			{Index: 2, Platform: "千问", BrowserID: 4, Status: tasks.StatusRunning, Images: []string{"a.png"}, Prompt: "同一句"},
		},
	}
	cards := a.platformCards()
	if len(cards) != 1 || len(cards[0].Sites) != 1 || cards[0].Sites[0].Platform != "千问" || cards[0].Sites[0].BrowserID != 4 {
		t.Fatalf("%+v", cards)
	}
	site := cards[0].Sites[0]
	if got := shotBadge(site.Platform, a.accountName(site.BrowserID)); got != "浏览器4" {
		t.Fatalf("account badge = %q", got)
	}
}

func TestPlanWorkbenchStartButton(t *testing.T) {
	now := time.Now()
	ready := []AccountSnap{{Platform: "豆包", LoggedIn: true, Health: store.HealthReady}}
	cases := []struct {
		name string
		in   WorkbenchInput
		want StartAction
	}{
		{"running", WorkbenchInput{Running: true, HasImages: true, AutoPlatform: true, Accounts: ready, Now: now}, StartNone},
		{"empty", WorkbenchInput{AutoPlatform: true, Accounts: ready, Now: now}, StartNone},
		{"prompt only", WorkbenchInput{HasPrompts: true, AutoPlatform: true, Accounts: ready, Now: now}, StartGenerate},
		{"image only", WorkbenchInput{HasImages: true, AutoPlatform: true, Accounts: ready, Now: now}, StartGenerate},
		{"no account", WorkbenchInput{HasImages: true, HasPrompts: true, AutoPlatform: true, Now: now}, StartWizard},
		{"signed out", WorkbenchInput{HasPrompts: true, AutoPlatform: true, Now: now, Accounts: []AccountSnap{{Platform: "豆包", Health: store.HealthReady}}}, StartWizard},
	}
	for _, tc := range cases {
		got := PlanWorkbench(tc.in)
		if got.Action != tc.want {
			t.Errorf("%s action = %v, want %v (%+v)", tc.name, got.Action, tc.want, got)
		}
	}
}

func TestDefaultOutputDir(t *testing.T) {
	if got := DefaultOutputDir("  /custom/out ", "/data"); got != "/custom/out" {
		t.Fatalf("explicit = %q", got)
	}
	if got := DefaultOutputDir("", "/data"); got != filepath.Join("/data", DefaultOutputName) {
		t.Fatalf("default = %q", got)
	}
	if got := DefaultOutputDir("  ", ""); got != filepath.Join(".", DefaultOutputName) {
		t.Fatalf("blank work dir = %q", got)
	}
	dir := filepath.Join(t.TempDir(), "nested")
	got, err := EnsureOutputDir(DefaultOutputDir("", dir))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() || got != filepath.Join(dir, DefaultOutputName) {
		t.Fatalf("created %q: %v", got, err)
	}
}

func TestConcurrencyCaption(t *testing.T) {
	if got := ConcurrencyCaption(true, 3); got != "自动（当前 3 个账号同时出图）" {
		t.Fatalf("auto caption = %q", got)
	}
	if got := ConcurrencyCaption(false, 3); got != "手动上限（当前 3 个账号可用）" {
		t.Fatalf("manual caption = %q", got)
	}
	if got := ConcurrencyCaption(true, 0); got != "自动（当前 0 个账号同时出图）" {
		t.Fatalf("empty caption = %q", got)
	}
}

func TestConcurrencyDefaultsToAuto(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st}
	a.loadSettings()
	if !a.concurrencyAuto || a.taskConcurrency() != 0 {
		t.Fatal("concurrency should default to auto")
	}
	if got := ConcurrencyCaption(a.concurrencyAuto, a.parallelSlots()); got != "自动（当前 0 个账号同时出图）" {
		t.Fatalf("caption = %q", got)
	}
	if _, err := st.UpsertExtensionAccount("千问", "profile-a", "甲"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertExtensionAccount("豆包", "profile-a", "甲"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertExtensionAccount("千问", "profile-b", "乙"); err != nil {
		t.Fatal(err)
	}
	st.Set(extbridge.SettingBrowserBackend, "extension")
	a.platformAuto = true
	if slots := a.parallelSlots(); slots != 1 {
		t.Fatalf("doubao tab only counts doubao: %d", slots)
	}
	a.stageTab = 1
	if slots := a.parallelSlots(); slots != 2 {
		t.Fatalf("qianwen tab counts its own accounts: %d", slots)
	}
	a.concurrencyAuto = false
	a.concurrency = 4
	a.saveSettings()
	again := &App{store: st}
	again.loadSettings()
	if again.concurrencyAuto || again.taskConcurrency() != 4 {
		t.Fatalf("manual cap was not restored: auto=%v n=%v", again.concurrencyAuto, again.taskConcurrency())
	}
}

func TestPlatformAutoDefaultsOn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st}
	a.loadSettings()
	if !a.platformAuto {
		t.Fatal("platform auto should default on")
	}
	if int(a.dailyCap) != tasks.DefaultDailyCap || a.riskyWords != tasks.RiskySetting() {
		t.Fatalf("safe defaults cap=%v words=%q", a.dailyCap, a.riskyWords)
	}
	a.platformAuto = false
	a.dailyCap = 12
	a.riskyWords = "血腥,武器"
	a.saveSettings()
	again := &App{store: st}
	again.loadSettings()
	if again.platformAuto {
		t.Fatal("turned-off platform auto was not restored")
	}
	if int(again.dailyCap) != 12 || again.riskyWords != "血腥,武器" {
		t.Fatalf("cap/words not restored: %v %q", again.dailyCap, again.riskyWords)
	}
}

func TestDisplayStatusAndProgressCards(t *testing.T) {
	if got := DisplayStatus([]tasks.Status{tasks.StatusPending, tasks.StatusSuccess}); got != "排队中" {
		t.Fatalf("pending = %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusRunning, tasks.StatusFailed}); got != "生成中" {
		t.Fatalf("running = %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusRetry}); got != "生成中" {
		t.Fatalf("retry = %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusSuccess}); got != "完成" {
		t.Fatalf("success = %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusFailed, tasks.StatusSuccess}); got != "失败" {
		t.Fatalf("failed = %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusRunning, tasks.StatusAwaitUser}); got != "需要人工验证" {
		t.Fatalf("验证码等待要盖过生成中: %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusNeedsUser}); got != "待验证" {
		t.Fatalf("needs user = %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusSkipped}); got != "已停止" {
		t.Fatalf("stopped = %s", got)
	}
	if got := DisplayStatus([]tasks.Status{tasks.StatusFailed, tasks.StatusSkipped}); got != "失败" {
		t.Fatalf("failed beats stopped: %s", got)
	}

	cards := ProgressCards([]tasks.Task{
		{Platform: "豆包", Images: []string{"风景.png"}, Prompt: "甲", Status: tasks.StatusSuccess, Results: []string{"out/a.png"}},
		{Platform: "千问", Images: []string{"风景.png"}, Prompt: "甲", Status: tasks.StatusFailed, Error: "账号额度已用尽"},
		{Platform: "豆包", Prompt: "画一只猫", Status: tasks.StatusPending},
	})
	if len(cards) != 2 {
		t.Fatalf("cards = %+v", cards)
	}
	if cards[0].Title != "风景.png" || cards[0].Status != "失败" || cards[0].Reason != "账号额度已用完" || cards[0].Action != "换账号重试" || len(cards[0].Retry) != 1 || cards[0].Retry[0].Platform != "千问" {
		t.Fatalf("image card = %+v", cards[0])
	}
	if len(cards[0].Shots) != 1 || cards[0].Shots[0].Path != "out/a.png" {
		t.Fatalf("results = %+v", cards[0].Shots)
	}
	if got := RunSummary(3, false); got != "3 张图" {
		t.Fatal(got)
	}
	running := ProgressCards([]tasks.Task{
		{Index: 1, Platform: "豆包", BrowserID: 1, Images: []string{"风景.png"}, Status: tasks.StatusRunning, Results: []string{"out.png"}},
		{Index: 4, Platform: "豆包", BrowserID: 1, Images: []string{"风景.png"}, Status: tasks.StatusSuccess, Results: []string{"out.png"}},
		{Index: 2, Platform: "豆包", BrowserID: 2, Images: []string{"人像.png"}, Status: tasks.StatusRunning},
		{Index: 3, Platform: "千问", BrowserID: 3, Images: []string{"静物.png"}, Status: tasks.StatusRunning},
	})
	done, gen, queued, failedN, needs := ProgressTally(running)
	if len(running) != 3 || done != 0 || gen != 3 || queued != 0 || failedN != 0 || needs != 0 || len(running[0].Shots) != 0 {
		t.Fatalf("running tally cards=%d done=%d gen=%d shots=%d", len(running), done, gen, len(running[0].Shots))
	}
	if got := LaneHeader(3, done, len(running)); got != "3 个账号同时出图 · 完成 0 / 共 3" {
		t.Fatal(got)
	}
	if got := FormatProgressTally(done, gen, queued, failedN, needs, 0, 3); got != "完成 0 · 进行中 3 · 排队 0 · 失败 0 · 3/3 并发" {
		t.Fatal(got)
	}
	finished := ProgressCards([]tasks.Task{
		{Index: 1, Platform: "豆包", BrowserID: 1, Images: []string{"风景.png"}, Status: tasks.StatusSuccess, Results: []string{"out.png"}},
	})
	if finished[0].Status != "完成" || len(finished[0].Shots) != 1 {
		t.Fatalf("finished card %+v", finished[0])
	}
	if cards[1].Status != "排队中" || cards[1].Title != "画一只猫" || cards[1].Reason != "" {
		t.Fatalf("text card = %+v", cards[1])
	}
	waiting := ProgressCards([]tasks.Task{
		{Platform: "豆包", Prompt: "油画风格", Status: tasks.StatusPending, Error: "发送状态未确认，等待「豆包1」空闲后再试"},
	})
	if waiting[0].Status != "排队中" || waiting[0].Reason != "发送状态未确认，等待「豆包1」空闲后再试" {
		t.Fatalf("queued retry card = %+v", waiting[0])
	}
	switched := ProgressCards([]tasks.Task{{
		Index: 2, Platform: "豆包", BrowserID: 1, Prompt: "雪山下的小木屋，油画风格",
		Status: tasks.StatusSuccess, Results: []string{"out.png"},
		Reassigned:  "豆包1",
		ReassignWhy: "发送: 发送状态未确认，请检查网页后再开始任务: 平台未接受消息",
	}})
	if switched[0].Moved != "已改由豆包1" || switched[0].MoveWhy != "发送结果不确定，请看网页后再试" {
		t.Fatalf("reassign note = %+v", switched[0])
	}
	failedSend := ProgressCards([]tasks.Task{
		{Platform: "豆包", Prompt: "油画风格", Status: tasks.StatusFailed, Error: "发送状态未确认，请检查网页后再开始任务: 平台未接受消息"},
	})
	if failedSend[0].Status != "失败" || failedSend[0].Reason == "" {
		t.Fatalf("failed send card = %+v", failedSend[0])
	}
	failed := FailedTasks([]tasks.Task{
		{Status: tasks.StatusSuccess},
		{Status: tasks.StatusFailed},
		{Status: tasks.StatusNeedsUser},
		{Status: tasks.StatusSkipped},
	})
	if len(failed) != 2 || failed[0].Status != tasks.StatusFailed || failed[1].Status != tasks.StatusSkipped {
		t.Fatalf("failed = %+v", failed)
	}
}

func TestHumanReason(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "出图失败，请重试"},
		{"等待生成结果超时：未得到有效的新图片", "等太久了，没有拿到新图片"},
		{"平台[千问]没有配置浏览器，请先在浏览器管理中添加", "这个平台还没有可用账号"},
		{"任务队列为空：请添加提示词，或添加源图片", "请先添加图片或提示词"},
		{"任务已在执行中", "正在出图，请稍候"},
		{"账号额度已用尽", "账号额度已用完"},
		{"quota exceeded for today", "账号额度已用完"},
		{"请求过于频繁", "请求太频繁，请稍后再试"},
		{"rate limit", "请求太频繁，请稍后再试"},
		{"千问页面未登录，请在扩展打开的网页登录后再开始任务", "账号未登录，请先登录"},
		{"需人工验证：请在原网页手动完成验证后再开始任务", "需要在网页里完成验证"},
		{"发送状态未确认，请检查网页后再开始任务", "发送结果不确定，请看网页后再试"},
		{"网页下载结果 #1 校验失败: 文件为空或图片格式不支持", "结果图片保存失败"},
		{"上传: 连接被拒绝", "图片上传失败"},
		{"扩展账号未连接", "扩展没有连上"},
		{"已停止", "已停止"},
		{"出图超时", "出图超时"},
		{"出图超时，没有可保存的图片", "出图超时"},
		{"同平台账号都不可用", "账号暂时不可用"},
		{"启动浏览器[工作]: 未找到 Google Chrome，请先安装", "浏览器没有打开"},
		{"websocket: connection refused", "网络中断，请重试"},
		{"websocket: close 1006 (abnormal)", "浏览器断开了"},
		{"target closed", "浏览器断开了"},
		{"浏览器崩溃，自动重启后仍失败", "浏览器崩溃，自动重启后仍失败"},
		{"浏览器崩溃了", "浏览器崩溃了"},
		{"页面长时间没有进展", "页面长时间没有进展"},
		{"需要安全验证", "需要在网页里完成验证"},
		{"something broke in the page", "出图失败，请重试"},
		{"源图是空文件", "源图是空文件"},
	}
	for _, tc := range cases {
		if got := HumanReason(tc.in); got != tc.want {
			t.Errorf("HumanReason(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	long := "这是一段已经是中文但长得放不进一行的说明需要被收短以免把进度卡片撑乱掉后面还有很多字"
	got := HumanReason(long)
	if len([]rune(got)) > 37 || !mostlyHan(got) {
		t.Fatalf("long reason = %q", got)
	}
}

func TestResultCardShowsEveryShot(t *testing.T) {
	four := &App{queue: []tasks.Task{{
		Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusSuccess,
		Images: []string{"小屋.png"}, Prompt: "雪山下的小屋",
		Results: []string{"a.png", "b.png", "c.png", "d.png"},
	}}}
	tt := ui.NewTester(four.view, 1280, 900)
	n := 0
	for _, text := range tt.Texts() {
		if text == "结果图" {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("4 张结果应都出现在卡片上，看到 %d 个，文本 %v", n, tt.Texts())
	}
	if tt.HasText("+1") {
		t.Fatal("4 张不该收成 +1")
	}
	many := &App{queue: []tasks.Task{{
		Index: 3, Platform: "豆包", Status: tasks.StatusSuccess, Prompt: "一组",
		Results: []string{"1.png", "2.png", "3.png", "4.png", "5.png", "6.png", "7.png"},
	}}}
	more := ui.NewTester(many.view, 1280, 900)
	n = 0
	for _, text := range more.Texts() {
		if text == "结果图" {
			n++
		}
	}
	if n != 7 {
		t.Fatalf("7 张结果应都出现，看到 %d 个，文本 %v", n, more.Texts())
	}
	if more.HasText("+2") {
		t.Fatal("结果不再收成 +N")
	}
}

func TestTimeoutAndStoppedShareOneTally(t *testing.T) {
	queue := []tasks.Task{
		{Index: 1, Platform: "豆包", Status: tasks.StatusFailed, Error: "出图超时", Images: []string{"a.png"}, Prompt: "甲"},
		{Index: 2, Platform: "豆包", Status: tasks.StatusSkipped, Error: "已停止", Images: []string{"b.png"}, Prompt: "乙"},
	}
	tally := TallyTasks(queue)
	if tally.Failed != 1 || tally.Stopped != 1 || tally.Incomplete() != 1 {
		t.Fatalf("tally %+v", tally)
	}
	summary := taskResultSummary(queue)
	header := FormatProgressTally(tally.Done, tally.Running, tally.Queued, tally.Failed, tally.Needs, tally.Stopped, 0)
	if summary != "成功 0 · 失败 1 · 待验证 0 · 未完成 1" || header != "完成 0 · 进行中 0 · 排队 0 · 失败 1 · 未完成 1" {
		t.Fatalf("summary %q header %q", summary, header)
	}
	a := &App{
		platformAuto: true,
		engine:       tasks.NewEngine(nil, nil, nil, ""),
		queue:        queue,
		progText:     summary,
		stagePicked:  true,
	}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(200 * time.Millisecond)
	for _, label := range []string{summary, header, "出图超时，请重试", "已停止"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q in %v", label, tt.Texts())
		}
	}
	if tt.HasText("失败 2") || tt.HasText("出图失败，请重试") {
		t.Fatalf("已停止被算成失败: %v", tt.Texts())
	}
}

func TestFailureCardShowsReasonCategory(t *testing.T) {
	a := &App{
		platformAuto: true,
		engine:       tasks.NewEngine(nil, nil, nil, ""),
		stagePicked:  true,
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", Status: tasks.StatusFailed, Error: "出图超时", Images: []string{"a.png"}, Prompt: "甲"},
			{Index: 2, Platform: "豆包", Status: tasks.StatusFailed, Error: "额度不足", Images: []string{"b.png"}, Prompt: "乙"},
			{Index: 3, Platform: "豆包", Status: tasks.StatusFailed, Error: "需登录", Images: []string{"c.png"}, Prompt: "丙"},
			{Index: 4, Platform: "豆包", Status: tasks.StatusSkipped, Error: "已停止", Images: []string{"d.png"}, Prompt: "丁"},
		},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(200 * time.Millisecond)
	for _, label := range []string{"出图超时，请重试", "额度不足，请重试", "需登录，请重试", "已停止"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q in %v", label, tt.Texts())
		}
	}
	if tt.HasText("出图失败，请重试") {
		t.Fatalf("类别被收成通用失败: %v", tt.Texts())
	}
}

func TestFailCardShowsOneAction(t *testing.T) {
	a := &App{queue: []tasks.Task{
		{Platform: "豆包", Status: tasks.StatusFailed, Prompt: "一张温和的插画。画面温和、健康，适合公开展示", Error: "审核不通过: 内容不符合规范", Rewritten: true},
		{Platform: "千问", Status: tasks.StatusFailed, Images: []string{"风景.png"}, Prompt: "甲", Error: "登录已失效"},
		{Platform: "豆包", Status: tasks.StatusFailed, Images: []string{"静物.png"}, Prompt: "乙", Error: "上传: 没就绪"},
	}}
	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"这条内容没通过审核", "改提示词重试", "已改写：", "图片上传失败", "换图"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q", label)
		}
	}
	if err := tt.Click("千问"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"登录失效了，请重新登录", "去重登"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q", label)
		}
	}
}

func TestLayoutFlow(t *testing.T) {
	empty := LayoutFlow(false, false, false, false, 0)
	if empty.Images.State != stepCurrent || !empty.Images.Open || !empty.Prompt.Open || empty.Run.State != stepPending {
		t.Fatalf("empty %+v", empty)
	}
	picking := LayoutFlow(true, false, false, false, 0)
	if !picking.Images.Open || picking.Images.State != stepCurrent || !picking.Prompt.Open {
		t.Fatalf("picking %+v", picking)
	}
	ready := LayoutFlow(true, true, false, false, 0)
	if ready.Images.Open || ready.Images.State != stepDone || ready.Prompt.Open || ready.Prompt.State != stepDone || ready.Run.State != stepCurrent {
		t.Fatalf("ready %+v", ready)
	}
	queued := LayoutFlow(true, true, false, true, 0)
	if queued.Images.Open || queued.Prompt.Open || queued.Run.State != stepCurrent {
		t.Fatalf("queued %+v", queued)
	}
	text := LayoutFlow(false, true, false, false, 0)
	if !text.Images.Open || text.Prompt.State != stepDone || !text.Prompt.Open {
		t.Fatalf("text %+v", text)
	}
	skip := LayoutFlow(false, false, true, false, 0)
	if skip.Images.Open || skip.Images.State != stepDone || !skip.Prompt.Open || skip.Prompt.State != stepCurrent {
		t.Fatalf("skip %+v", skip)
	}
	if got := FormatProgressTally(1, 1, 1, 1, 0, 0, 1); got != "完成 1 · 进行中 1 · 排队 1 · 失败 1 · 1/1 并发" {
		t.Fatalf("tally %q", got)
	}
	if got := FormatProgressTally(0, 0, 0, 0, 1, 0, 0); got != "完成 0 · 进行中 0 · 排队 0 · 失败 0 · 待验证 1" {
		t.Fatalf("needs tally %q", got)
	}
}

func TestDropZoneAddsAndRemovesImages(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.png")
	second := filepath.Join(dir, "two.png")
	writeTinyPNG(t, first)
	writeColorPNG(t, second)
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 840)
	r := assertVisible(t, tt, "拖入或点击添加图片", 1280, 840)
	if !tt.DropFiles(r.X+10, r.Y+10, first, second, filepath.Join(dir, "note.txt")) {
		t.Fatal("drop zone did not take the files")
	}
	tt.Frame()
	if len(a.sources) != 2 {
		t.Fatalf("sources = %+v", a.sources)
	}
	assertVisible(t, tt, "已选 2 张", 1280, 840)
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}
	if len(a.sources) != 1 {
		t.Fatalf("after remove = %d", len(a.sources))
	}
}

func TestDropTxtLoadsLineAndParagraphPrompts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompts.txt")
	if err := os.WriteFile(path, []byte("甲\n\n乙\n丙\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 840)
	r := assertVisible(t, tt, "每行一条，空行隔开的段落收成一条", 1280, 840)
	if !tt.DropFiles(r.X+8, r.Y+4, path, filepath.Join(dir, "skip.png")) {
		t.Fatal("提示词卡片没有接住文件")
	}
	tt.Frame()
	if a.prompts != "甲\n乙 丙" || a.promptPick != "" || a.promptChoice != prompts.LabelCustom {
		t.Fatalf("prompts=%q pick=%q choice=%q", a.prompts, a.promptPick, a.promptChoice)
	}
	if !tt.HasText("已载入 2 条提示词") {
		t.Fatal("没有载入提示")
	}
}

func writeColorPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 200, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestPlanUsesStageTabEvenWhenUnchecked(t *testing.T) {
	plan := PlanWorkbench(WorkbenchInput{
		HasPrompts: true, WantDoubao: true, WantQianwen: true, OnlyPlatform: "Antigravity",
		Accounts: []AccountSnap{{Platform: "Antigravity", LoggedIn: true, Health: "ready"}},
		Now:      time.Now(),
	})
	if plan.Action != StartGenerate || len(plan.Platforms) != 1 || plan.Platforms[0] != "Antigravity" {
		t.Fatalf("plan %+v", plan)
	}
}
