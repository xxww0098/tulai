// Package wizard 决定首次设置向导该不该出现、用哪一个浏览器、账号叫什么。
// 不启动浏览器，也不访问豆包或千问。
package wizard

import (
	"strings"
	"time"

	"xbs/internal/browser"
)

// KeyComplete 写在配置库 settings 表里。值为 CompleteYes 表示向导走完过。
const KeyComplete = "wizard_complete"

// CompleteYes 是向导已完成。
const CompleteYes = "1"

// 登录探测只认这三个结果。in 表示页面上已经有输入框，且没有扫码登录层。
const (
	ProbeIn      = "in"
	ProbeOut     = "out"
	ProbeUnknown = "unknown"
)

// Step 是向导的四步。
type Step int

const (
	StepBrowser Step = iota
	StepPlatform
	StepAccount
	StepDone
)

// Count 是步骤数。
func Count() int { return 4 }

// Index 从 1 开始。
func (s Step) Index() int { return int(s) + 1 }

// Title 是这一步的大标题。
func (s Step) Title() string {
	switch s {
	case StepPlatform:
		return "选择平台"
	case StepAccount:
		return "登录账号"
	case StepDone:
		return "可以开始了"
	default:
		return "准备浏览器"
	}
}

// Account 是判断「还有没有能用的账号」时用的快照。
type Account struct {
	Platform  string
	Name      string
	LoggedIn  bool
	Extension bool
	Health    string
	CoolUntil int64
}

// KeepsClosed 为真时，这个账号说明用户已经能用 tulai，启动时不再摊开向导。
// 登录过（或是扩展会话），并且不是要等人点「恢复」的永久失败。
// 冷却和额度用尽只要写了恢复时间，就算暂时不可用也先留在主界面。
func KeepsClosed(a Account, now time.Time) bool {
	if strings.TrimSpace(a.Platform) == "" {
		return false
	}
	if !a.LoggedIn && !a.Extension {
		return false
	}
	switch strings.TrimSpace(a.Health) {
	case "", "ready":
		return true
	case "cooling", "exhausted":
		return a.CoolUntil != 0
	default:
		return true
	}
}

// ShouldShow 在第一次打开，或已经没有可用账号时为真。
// 有一个还能用的账号就跳过，哪怕完成标记还没写上。
// 标记写过、但账号都没了或不健康，向导会再出现。
func ShouldShow(complete string, accounts []Account, now time.Time) bool {
	for _, account := range accounts {
		if KeepsClosed(account, now) {
			return false
		}
	}
	return true
}

// Found 是本机已经能启动的浏览器，不含还没下载的内置浏览器。
type Found struct {
	Kind string
	Path string
}

// BrowserPlan 是向导第一步自动选中的结果。用户不用自己挑。
type BrowserPlan struct {
	Backend       string
	Kind          string
	Title         string
	Detail        string
	NeedsDownload bool
	CanNext       bool
	Blocked       string
}

// PlanBrowser 按系统挑一个推荐项。
// Windows 优先系统 Edge；其它情况依次是已安装的 Chrome、Edge、Chromium。
// 都没有就准备下载内置浏览器。当前架构没有安装包时，只能请用户自己装浏览器。
func PlanBrowser(goos, goarch string, found []Found, builtinReady, builtinSupported bool) BrowserPlan {
	if goos == "windows" {
		if path, ok := findKind(found, browser.KindEdge); ok {
			return readyPlan(browser.KindEdge, browser.KindEdge, "将使用系统 Edge", path)
		}
	}
	if path, ok := findKind(found, browser.KindChrome); ok {
		return readyPlan(browser.KindChrome, browser.KindChrome, "将使用本机 Chrome", path)
	}
	if path, ok := findKind(found, browser.KindEdge); ok {
		title := "将使用本机 Edge"
		if goos == "windows" {
			title = "将使用系统 Edge"
		}
		return readyPlan(browser.KindEdge, browser.KindEdge, title, path)
	}
	if path, ok := findKind(found, browser.KindChromium); ok {
		return readyPlan(browser.KindChrome, browser.KindChromium, "将使用本机 Chromium", path)
	}
	if builtinReady {
		return readyPlan(browser.KindBuiltin, browser.KindBuiltin, "将使用已下载的内置浏览器", "Chrome for Testing 已在本机，不用再安装")
	}
	if builtinSupported {
		return BrowserPlan{
			Backend:       browser.KindBuiltin,
			Kind:          browser.KindBuiltin,
			Title:         "需要下载内置浏览器",
			Detail:        "没有检测到 Chrome、Edge 或 Chromium。下载内置浏览器后即可继续，不用自己安装。",
			NeedsDownload: true,
		}
	}
	return BrowserPlan{
		Title:   "没有可用的浏览器",
		Blocked: "当前系统没有可下载的内置浏览器。请先安装 Microsoft Edge 或 Google Chrome，然后点重新检测。",
	}
}

func readyPlan(backend, kind, title, detail string) BrowserPlan {
	return BrowserPlan{
		Backend: backend,
		Kind:    kind,
		Title:   title,
		Detail:  detail,
		CanNext: true,
	}
}

func findKind(found []Found, kind string) (string, bool) {
	for _, item := range found {
		if item.Kind == kind && strings.TrimSpace(item.Path) != "" {
			return item.Path, true
		}
	}
	return "", false
}

// SelectedPlatforms 按豆包、千问的顺序返回勾选的平台。
func SelectedPlatforms(doubao, qianwen bool) []string {
	var out []string
	if doubao {
		out = append(out, "豆包")
	}
	if qianwen {
		out = append(out, "千问")
	}
	return out
}

// CanChoosePlatform 至少要选一个平台。
func CanChoosePlatform(doubao, qianwen bool) bool {
	return doubao || qianwen
}

// Queue 是这一轮还要登录的平台。
type Queue struct {
	Left []string
}

// NewQueue 复制平台列表。
func NewQueue(platforms []string) Queue {
	return Queue{Left: append([]string{}, platforms...)}
}

// Current 是正在登录的平台。没有时为空。
func (q Queue) Current() string {
	if len(q.Left) == 0 {
		return ""
	}
	return q.Left[0]
}

// Advance 去掉已经登录的那个平台。
func (q Queue) Advance() Queue {
	if len(q.Left) == 0 {
		return q
	}
	return Queue{Left: append([]string{}, q.Left[1:]...)}
}

// Done 表示这一轮平台都登录过了。
func (q Queue) Done() bool { return len(q.Left) == 0 }

// Poll 是一次登录检测之后向导该做什么。
type Poll int

const (
	PollWait Poll = iota
	PollNext
	PollDone
	PollStop
)

// LoggedIn 只在探测明确返回 in 时为真。加载中和登录墙都继续等。
func LoggedIn(raw string) bool {
	return strings.TrimSpace(raw) == ProbeIn
}

// AfterLogin 在检测到登录后进入下一个平台，或结束这一轮。
func AfterLogin(q Queue, loggedIn bool) (Queue, Poll) {
	if !loggedIn {
		return q, PollWait
	}
	q = q.Advance()
	if q.Done() {
		return q, PollDone
	}
	return q, PollNext
}

// NextAccountName 给新档案一个不重复的名字，从账号1开始。
func NextAccountName(used []string) string {
	taken := map[string]bool{}
	for _, name := range used {
		taken[strings.TrimSpace(name)] = true
	}
	for i := 1; i < 10000; i++ {
		name := "账号" + itoa(i)
		if !taken[name] {
			return name
		}
	}
	return "账号"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
