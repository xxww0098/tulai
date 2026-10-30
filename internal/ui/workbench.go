package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

const (
	pageHome = iota
	pageWash
	pageBrowser
	pagePrompts
	pageSettings
)

// DefaultOutputName 是没填输出目录时用的文件夹名。
const DefaultOutputName = "输出结果"

// StartAction 是「开始出图」这一下要做的事。
type StartAction int

const (
	// StartNone 表示现在不能出图：正在跑，或者还没有图片和提示词。
	StartNone StartAction = iota
	// StartGenerate 用已经登录的账号直接出图。
	StartGenerate
	// StartWizard 没有可用账号，按钮改为打开设置向导。
	StartWizard
)

// AccountSnap 是判断平台和向导时用的账号快照。
type AccountSnap struct {
	Platform  string
	LoggedIn  bool
	Extension bool
	Health    string
	CoolUntil int64
}

// WorkbenchInput 是主页默认值和开始按钮需要的输入。
type WorkbenchInput struct {
	Running       bool
	HasImages     bool
	HasPrompts    bool
	AutoPlatform  bool
	WantDoubao    bool
	WantQianwen   bool
	OnlyPlatform  string
	ExtensionMode bool
	Accounts      []AccountSnap
	Now           time.Time
}

// WorkbenchPlan 是这一次开始按钮的决定。
type WorkbenchPlan struct {
	Action    StartAction
	Platforms []string
}

// PlanWorkbench 决定开始按钮该出图、打开向导，还是先不动。
// 自动平台时，用当前方式下已登录且还能恢复的账号；关掉自动后只保留勾选的平台。
func PlanWorkbench(in WorkbenchInput) WorkbenchPlan {
	if in.Running || (!in.HasImages && !in.HasPrompts) {
		return WorkbenchPlan{Action: StartNone}
	}
	only := strings.TrimSpace(in.OnlyPlatform)
	want := func(platform string) bool {
		if only != "" && platform != only {
			return false
		}
		if in.AutoPlatform {
			return platform == "豆包" || platform == "千问"
		}
		switch platform {
		case "豆包":
			return in.WantDoubao
		case "千问":
			return in.WantQianwen
		default:
			return false
		}
	}
	platforms := platformsFromAccounts(in.Accounts, in.ExtensionMode, in.Now, want)
	if len(platforms) == 0 {
		// 这一页已经有账号，只是登录或额度不行。按钮关掉，不要改去开向导。
		if only != "" && hasPlatformAccount(in.Accounts, only) {
			return WorkbenchPlan{Action: StartNone}
		}
		return WorkbenchPlan{Action: StartWizard}
	}
	return WorkbenchPlan{Action: StartGenerate, Platforms: platforms}
}

func hasPlatformAccount(accounts []AccountSnap, platform string) bool {
	for _, account := range accounts {
		if account.Platform == platform {
			return true
		}
	}
	return false
}

// ConcurrencyCaption 是「调度与等待」里并发这一行的说明。
// 自动时写出当前能同时出图的账号数；手动时写出上限旁边的可用账号数。
func ConcurrencyCaption(auto bool, slots int) string {
	if slots < 0 {
		slots = 0
	}
	if auto {
		return fmt.Sprintf("自动（当前 %d 个账号同时出图）", slots)
	}
	return fmt.Sprintf("手动上限（当前 %d 个账号可用）", slots)
}

func (a AccountSnap) carries(extensionMode bool, now time.Time) bool {
	if a.Extension != extensionMode {
		return false
	}
	return wizard.KeepsClosed(wizard.Account{
		Platform:  a.Platform,
		LoggedIn:  a.LoggedIn,
		Extension: a.Extension,
		Health:    a.Health,
		CoolUntil: a.CoolUntil,
	}, now)
}

func platformsFromAccounts(accounts []AccountSnap, extensionMode bool, now time.Time, want func(string) bool) []string {
	has := map[string]bool{}
	for _, account := range accounts {
		if account.carries(extensionMode, now) && want(account.Platform) {
			has[account.Platform] = true
		}
	}
	var out []string
	if has["豆包"] {
		out = append(out, "豆包")
	}
	if has["千问"] {
		out = append(out, "千问")
	}
	return out
}

// DefaultOutputDir 在没填目录时用工作目录下的「输出结果」。
func DefaultOutputDir(explicit, workDir string) string {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit
	}
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		workDir = "."
	}
	return filepath.Join(workDir, DefaultOutputName)
}

// EnsureOutputDir 创建输出目录。
func EnsureOutputDir(path string) (string, error) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// DisplayStatus 把一批任务收成出图页上的四个状态。待验证单独保留。
func DisplayStatus(statuses []tasks.Status) string {
	var running, pending, needs, failed, success, awaiting bool
	for _, status := range statuses {
		switch status {
		case tasks.StatusRunning, tasks.StatusRetry:
			running = true
		case tasks.StatusPending:
			pending = true
		case tasks.StatusAwaitUser:
			awaiting = true
		case tasks.StatusNeedsUser:
			needs = true
		case tasks.StatusFailed, tasks.StatusSkipped:
			failed = true
		case tasks.StatusSuccess:
			success = true
		}
	}
	switch {
	case awaiting:
		return "需要人工验证"
	case running:
		return "生成中"
	case pending:
		return "排队中"
	case needs:
		return "待验证"
	case failed:
		return "失败"
	case success:
		return "完成"
	default:
		return "排队中"
	}
}

// ProgressCard 是一张图（或一条文生图提示词）的进度。
type ProgressCard struct {
	Key       string
	Title     string
	Source    string
	Status    string
	Reason    string
	Action    string
	Rewrite   string
	Platforms []string
	Sites     []CardSite
	Shots     []CardShot
	Retry     []tasks.Task
	Moved     string // 已改由豆包1
	MoveWhy   string // 改派前的失败原因
}

// CardShot 是结果格里的一张图，以及产出它的那一单。
type CardShot struct {
	Path      string
	Task      int
	BrowserID int64
	Platform  string
}

// CardSite 是这张卡上的一个平台账号。
type CardSite struct {
	Platform  string
	BrowserID int64
}

const (
	stepPending = iota
	stepCurrent
	stepDone
)

// flowStep 是向导三步里的一步：做到哪了，卡片展不展开。
type flowStep struct {
	State int
	Open  bool
}

// flowLayout 是选图、写提示词、出图三张卡。
type flowLayout struct {
	Images flowStep
	Prompt flowStep
	Run    flowStep
}

// LayoutFlow 决定每一步是当前、已完成还是还没到，以及展开还是收成摘要。
// 已经做完的收起。正在挑图（有图、还没写提示词、也没在跑）时选图保持展开。
// 已经有出图进度时收起上面两步，把进度留在窗口里。pin 为 1 或 2 表示人点开了那一步。
func LayoutFlow(hasImages, hasPrompts, skipImages, hasQueue bool, pin int) flowLayout {
	var f flowLayout
	imagesDone := hasImages || skipImages
	arranging := hasImages && !hasPrompts && !skipImages && !hasQueue && pin != 2
	switch {
	case pin == 1:
		f.Images.Open = true
	case !hasQueue && (!imagesDone || arranging):
		f.Images.Open = true
	}
	switch {
	case f.Images.Open && !skipImages:
		f.Images.State = stepCurrent
	case imagesDone:
		f.Images.State = stepDone
	default:
		f.Images.State = stepCurrent
	}

	switch {
	case hasPrompts:
		f.Prompt.State = stepDone
	case imagesDone && !f.Images.Open:
		f.Prompt.State = stepCurrent
	default:
		f.Prompt.State = stepPending
	}
	switch {
	case pin == 2:
		f.Prompt.Open = true
	case hasQueue:
		f.Prompt.Open = false
	case hasPrompts && imagesDone && !f.Images.Open:
		f.Prompt.Open = false
	default:
		f.Prompt.Open = true
	}

	if (hasImages || hasPrompts) && !f.Images.Open && f.Prompt.State == stepDone {
		f.Run.State = stepCurrent
	} else {
		f.Run.State = stepPending
	}
	f.Run.Open = true
	return f
}

// ProgressTally 按卡片数完成、生成中、排队、失败和待验证。
func ProgressTally(cards []ProgressCard) (done, running, queued, failed, needs int) {
	for _, card := range cards {
		switch card.Status {
		case "完成":
			done++
		case "生成中":
			running++
		case "排队中":
			queued++
		case "需要人工验证":
			running++
		case "失败":
			failed++
		case "待验证":
			needs++
		}
	}
	return done, running, queued, failed, needs
}

// RunSummary 是出图步骤左侧的一行。账号数写在面板上，这里只说这一批有几张图。
func RunSummary(images int, prompts bool) string {
	switch {
	case images > 0:
		return fmt.Sprintf("%d 张图", images)
	case prompts:
		return "按提示词"
	default:
		return "先添加图片，或写一条提示词"
	}
}

// FormatProgressTally 是一个平台的进度。并发上限写在同一行，不再另起一条「同时出图」。
func FormatProgressTally(done, running, queued, failed, needs int) string {
	parts := []string{
		fmt.Sprintf("完成 %d", done),
		fmt.Sprintf("进行中 %d", running),
		fmt.Sprintf("排队 %d", queued),
		fmt.Sprintf("失败 %d", failed),
	}
	if needs > 0 {
		parts = append(parts, fmt.Sprintf("待验证 %d", needs))
	}
	parts = append(parts, fmt.Sprintf("%d/%d 并发", running, tasks.PlatformInFlight))
	return strings.Join(parts, " · ")
}

// ProgressCards 按源图合并多平台任务。没有源图时按提示词合并。
func ProgressCards(items []tasks.Task) []ProgressCard {
	var order []string
	groups := map[string][]tasks.Task{}
	for _, task := range items {
		key := cardKey(task)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], task)
	}
	out := make([]ProgressCard, 0, len(order))
	for _, key := range order {
		group := groups[key]
		card := ProgressCard{
			Key:    key,
			Title:  cardTitle(group[0]),
			Source: cardSource(group[0]),
		}
		statuses := make([]tasks.Status, 0, len(group))
		seen := map[string]bool{}
		var failed tasks.Task
		var haveFailed bool
		for _, task := range group {
			statuses = append(statuses, task.Status)
			if task.Platform != "" && !seen[task.Platform] {
				seen[task.Platform] = true
				card.Platforms = append(card.Platforms, task.Platform)
			}
			card.addSite(task.Platform, task.BrowserID)
			for _, path := range task.Results {
				card.Shots = append(card.Shots, CardShot{
					Path: path, Task: task.Index, BrowserID: task.BrowserID, Platform: task.Platform,
				})
			}
			if task.Rewritten && card.Rewrite == "" {
				card.Rewrite = shortLine(task.Prompt, 36)
			}
			if task.Reassigned != "" && card.Moved == "" {
				card.Moved = "已改由" + task.Reassigned
			}
			if task.ReassignWhy != "" && card.MoveWhy == "" {
				card.MoveWhy = HumanReason(task.ReassignWhy)
			}
			switch task.Status {
			case tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser:
				card.Retry = append(card.Retry, task)
				if !haveFailed {
					failed = task
					haveFailed = true
				}
			}
		}
		card.Status = DisplayStatus(statuses)
		// 还在出或还在排队时，不把已经落下的图当成完成结果。
		// 排队的还没分到账号，不把构建队列时占的浏览器写上去。
		var waiting string
		if card.Status == "生成中" || card.Status == "排队中" {
			card.Shots = nil
		}
		if card.Status == "排队中" {
			card.Sites = nil
			for _, task := range group {
				if task.Status == tasks.StatusPending && task.Error != "" {
					waiting = task.Error
					break
				}
			}
		}
		if card.Status == "排队中" && waiting != "" {
			card.Reason = waiting
			if !strings.Contains(waiting, "等待") {
				card.Reason = HumanReason(waiting)
			}
		} else if card.Status == "需要人工验证" {
			card.Reason = "请在弹出的窗口里完成验证"
		} else if card.Status == "待验证" {
			hint := tasks.ClassifyFailure(tasks.StatusNeedsUser, failed.Error)
			card.Reason = hint.Sentence
			card.Action = hint.Button
		} else if card.Status == "失败" {
			hint := tasks.ClassifyFailure(failed.Status, failed.Error)
			card.Reason = hint.Sentence
			card.Action = hint.Button
		}
		out = append(out, card)
	}
	return out
}

// FailedTasks 是「全部重试失败项」要点的任务。待验证不算失败。
func FailedTasks(items []tasks.Task) []tasks.Task {
	var out []tasks.Task
	for _, task := range items {
		if task.Status == tasks.StatusFailed || task.Status == tasks.StatusSkipped {
			out = append(out, task)
		}
	}
	return out
}

func cardKey(task tasks.Task) string {
	if len(task.Images) > 0 && strings.TrimSpace(task.Images[0]) != "" {
		return "img:" + task.Images[0]
	}
	return "prompt:" + task.Prompt
}

func cardSource(task tasks.Task) string {
	if len(task.Images) > 0 {
		return task.Images[0]
	}
	return ""
}

func (card *ProgressCard) addSite(platform string, id int64) {
	if platform == "" && id == 0 {
		return
	}
	for _, site := range card.Sites {
		if site.Platform == platform && site.BrowserID == id {
			return
		}
	}
	card.Sites = append(card.Sites, CardSite{Platform: platform, BrowserID: id})
}

func cardTitle(task tasks.Task) string {
	if len(task.Images) > 0 && task.Images[0] != "" {
		return filepath.Base(task.Images[0])
	}
	line := strings.TrimSpace(task.Prompt)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	rs := []rune(line)
	if len(rs) > 24 {
		line = string(rs[:24]) + "…"
	}
	if line == "" {
		return "文生图"
	}
	return line
}

// HumanReason 把任务错误收成一行中文。已经是短中文的原样留下。
func HumanReason(raw string) string {
	s := oneLine(raw)
	if s == "" {
		return "出图失败，请重试"
	}
	lower := strings.ToLower(s)
	switch {
	case strings.Contains(s, "已停止"):
		return "已停止"
	case strings.Contains(s, "出图超时"):
		return "出图超时"
	case strings.Contains(s, "等待生成结果超时") || strings.Contains(s, "交还浏览器超时"):
		return "等太久了，没有拿到新图片"
	case strings.Contains(s, "没有配置浏览器"):
		return "这个平台还没有可用账号"
	case strings.Contains(s, "任务队列为空"):
		return "请先添加图片或提示词"
	case strings.Contains(s, "任务已在执行中"):
		return "正在出图，请稍候"
	case strings.Contains(s, "发送状态未确认"):
		return "发送结果不确定，请看网页后再试"
	case strings.Contains(s, "额度") || strings.Contains(lower, "quota"):
		return "账号额度已用完"
	case strings.Contains(s, "频繁") || strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many"):
		return "请求太频繁，请稍后再试"
	case strings.Contains(s, "未登录") || strings.Contains(s, "请先登录") || strings.Contains(s, "登录已失效") || strings.Contains(s, "登录状态"):
		return "账号未登录，请先登录"
	case strings.Contains(s, "浏览器崩溃"):
		if strings.Contains(s, "仍失败") || strings.Contains(s, "自动重启") {
			return "浏览器崩溃，自动重启后仍失败"
		}
		return "浏览器崩溃了"
	case strings.Contains(s, "没有进展") || strings.Contains(s, "页面卡住"):
		return "页面长时间没有进展"
	case strings.Contains(lower, "target closed") || strings.Contains(s, "连接已断开") || strings.Contains(lower, "websocket: close"):
		return "浏览器断开了"
	case strings.Contains(s, "滑块") || strings.Contains(s, "安全验证") || strings.Contains(s, "验证"):
		return "需要在网页里完成验证"
	case strings.Contains(s, "扩展") && (strings.Contains(s, "未连接") || strings.Contains(s, "没连")):
		return "扩展没有连上"
	case strings.Contains(s, "上传"):
		return "图片上传失败"
	case strings.Contains(s, "下载") || strings.Contains(s, "校验失败"):
		return "结果图片保存失败"
	case strings.Contains(s, "同平台账号都不可用") || strings.Contains(s, "账号不可用"):
		return "账号暂时不可用"
	case strings.Contains(s, "启动浏览器") || strings.Contains(s, "未找到"):
		return "浏览器没有打开"
	case strings.Contains(s, "网络") || strings.Contains(lower, "connection") || strings.Contains(lower, "websocket"):
		return "网络中断，请重试"
	}
	if mostlyHan(s) {
		rs := []rune(s)
		if len(rs) > 36 {
			return string(rs[:36]) + "…"
		}
		return s
	}
	return "出图失败，请重试"
}

func shortLine(s string, n int) string {
	s = oneLine(s)
	rs := []rune(s)
	if n > 0 && len(rs) > n {
		return string(rs[:n]) + "…"
	}
	return s
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func mostlyHan(s string) bool {
	han, letters := 0, 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.IsLetter(r):
			letters++
		}
	}
	return han > 0 && han >= letters
}
