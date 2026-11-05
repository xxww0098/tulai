// Package wizard 决定首次设置向导该不该出现、用哪一个浏览器、账号叫什么。
// 不启动浏览器和 agy，也不访问豆包、千问或 Antigravity。
package wizard

import (
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
)

// KeyComplete 记录向导已完成，或已识别出旧版用户。
const KeyComplete = "wizard_complete"

// KeyShown 记录首次启动时已经展示过向导。
const KeyShown = "wizard_shown"

// CompleteYes 表示已经完成首次设置或是旧版用户。
const CompleteYes = "1"

// 登录探测结果。in 表示已经登录。页面上还有「登录」或扫码层时是 out，即使聊天框还在。
const (
	ProbeIn      = "in"
	ProbeOut     = "out"
	ProbeUnknown = "unknown"
	ProbeRegion  = "region"
)

// Step 是向导的步骤。第一步总是选平台，之后按平台走不同的路，见 Flow。
type Step int

const (
	StepPlatform Step = iota
	StepBrowser
	StepInstall
	StepAccount
	StepDone
)

// Title 是这一步的大标题。
func (s Step) Title() string {
	switch s {
	case StepBrowser:
		return "准备浏览器"
	case StepInstall:
		return "安装 Antigravity"
	case StepAccount:
		return "登录账号"
	case StepDone:
		return "可以开始了"
	default:
		return "选择平台"
	}
}

// Label 是左侧步骤栏里的短名。
func (s Step) Label() string {
	switch s {
	case StepBrowser:
		return "浏览器"
	case StepInstall:
		return "安装"
	case StepAccount:
		return "登录"
	case StepDone:
		return "完成"
	default:
		return "平台"
	}
}

// Flow 是选定平台之后要走的全部步骤，按先后排列。
// 豆包和千问在网页里登录，要先备好浏览器。Antigravity 走命令行，要先装好 agy，不需要浏览器。
func Flow(platform string) []Step {
	if platform == agy.Platform {
		return []Step{StepPlatform, StepInstall, StepAccount, StepDone}
	}
	return []Step{StepPlatform, StepBrowser, StepAccount, StepDone}
}

// Next 是 s 在这个平台的流程里的下一步。s 是最后一步或不在流程里时 ok 为假。
func Next(platform string, s Step) (Step, bool) {
	flow := Flow(platform)
	for i, step := range flow {
		if step == s && i+1 < len(flow) {
			return flow[i+1], true
		}
	}
	return s, false
}

// Prev 是 s 在这个平台的流程里的上一步。s 是第一步或不在流程里时 ok 为假。
func Prev(platform string, s Step) (Step, bool) {
	flow := Flow(platform)
	for i, step := range flow {
		if step == s && i > 0 {
			return flow[i-1], true
		}
	}
	return s, false
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
// 必须已经登录。只连上的扩展会话不算。不是要等人点「恢复」的永久失败。
// 冷却和额度用尽只要写了恢复时间，就算暂时不可用也先留在主界面。
func KeepsClosed(a Account, now time.Time) bool {
	if strings.TrimSpace(a.Platform) == "" || !a.LoggedIn {
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

// ShouldShow 只在尚未完成设置且没有旧账号时为真。
func ShouldShow(complete string, accounts []Account) bool {
	if complete == CompleteYes {
		return false
	}
	for _, account := range accounts {
		if strings.TrimSpace(account.Platform) != "" {
			return false
		}
	}
	return true
}

// Found 是已经安装的 Camoufox 运行环境。
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

// PlanBrowser only offers Camoufox. The official browser and launcher are installed together.
func PlanBrowser(goos, goarch string, found []Found, ready, supported bool) BrowserPlan {
	if ready {
		return readyPlan(browser.KindCamoufox, browser.KindCamoufox, "Camoufox 已就绪", "每个账号使用独立的登录资料和固定指纹")
	}
	if supported {
		return BrowserPlan{Backend: browser.KindCamoufox, Kind: browser.KindCamoufox, Title: "安装 Camoufox", Detail: "首次使用需要下载浏览器和运行环境。安装完成后即可登录服务商账号。", NeedsDownload: true}
	}
	return BrowserPlan{Title: "当前架构不支持 Camoufox", Blocked: "Windows 请使用 x64 版本；macOS 支持 Intel 和 Apple Silicon。"}
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

// Platforms 是第一步能选的平台，按显示顺序排列。
func Platforms() []string {
	return []string{"豆包", "千问", agy.Platform, "ChatGPT"}
}

// Valid 报告 name 是不是第一步能选的平台。
func Valid(name string) bool {
	for _, p := range Platforms() {
		if p == name {
			return true
		}
	}
	return false
}

// DefaultChoice 是第一步一开始选中的平台：已勾选的平台里排最前的，一个都没有就是豆包。
func DefaultChoice(selected []string) string {
	for _, p := range Platforms() {
		if hasName(selected, p) {
			return p
		}
	}
	return Platforms()[0]
}

// MergeSelection 是选定平台后写回配置库的勾选列表。
// 第一次设置只留下选的这一个。以后重开向导只是再加一个平台，已有的勾选不动。
func MergeSelection(selected []string, chosen string, firstRun bool) []string {
	var out []string
	for _, p := range Platforms() {
		if p == chosen || (!firstRun && hasName(selected, p)) {
			out = append(out, p)
		}
	}
	return out
}

func hasName(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
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

// NextInstanceName 按平台取最小的未占用编号。豆包和千问各自从 1 起，
// 所以已有豆包时，千问叫千问1、千问2，而不是浏览器3、浏览器4。
func NextInstanceName(platform string, used []string) string {
	prefix := strings.TrimSpace(platform)
	if prefix == "" {
		prefix = "账号"
	}
	taken := map[int]bool{}
	for _, name := range used {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		n := 0
		for _, r := range name[len(prefix):] {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		if n > 0 {
			taken[n] = true
		}
	}
	for i := 1; i < 10000; i++ {
		if !taken[i] {
			return prefix + itoa(i)
		}
	}
	return prefix
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
