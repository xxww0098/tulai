package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
)

// WizardKeyComplete 记录向导已完成，或已识别出旧版用户。
const WizardKeyComplete = "wizard_complete"

// WizardCompleteYes 表示已经完成首次设置或是旧版用户。
const WizardCompleteYes = "1"

const (
	wizardKeyShown    = "wizard_shown"
	wizardProbeIn     = "in"
	wizardProbeRegion = "region"
)

type wizardStep int

const (
	wizardStepPlatform wizardStep = iota
	wizardStepBrowser
	wizardStepAccount
	wizardStepDone
)

func wizardSteps() []wizardStep {
	return []wizardStep{wizardStepPlatform, wizardStepBrowser, wizardStepAccount, wizardStepDone}
}

type wizardAccount struct {
	Platform  string
	Name      string
	Engine    string
	LoggedIn  bool
	Health    string
	CoolUntil int64
}

func wizardKeepsClosed(a wizardAccount, now time.Time) bool {
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

func wizardShouldShow(complete string, accounts []wizardAccount) bool {
	if complete == WizardCompleteYes {
		return false
	}
	for _, account := range accounts {
		if strings.TrimSpace(account.Platform) != "" {
			return false
		}
	}
	return true
}

type wizardBrowserPlan struct {
	Backend       string
	Kind          string
	Title         string
	Detail        string
	NeedsDownload bool
	CanNext       bool
	Blocked       string
}

func planAccountBrowser(engine string, ready, supported bool) wizardBrowserPlan {
	kind := browser.CanonicalBackend(engine)
	if ready {
		return wizardBrowserPlan{
			Backend: kind,
			Kind:    kind,
			Title:   kind + " 已就绪",
			Detail:  "tulai 通过私有 CDP 管道控制 fingerprint-chromium，每个账号独立保存登录资料和固定指纹种子",
			CanNext: true,
		}
	}
	if supported {
		return wizardBrowserPlan{
			Backend:       kind,
			Kind:          kind,
			Title:         "安装 " + kind,
			Detail:        "首次使用需要下载 Fingerprint 浏览器和运行环境。安装完成后即可登录服务商账号。",
			NeedsDownload: true,
		}
	}
	return wizardBrowserPlan{
		Backend: kind,
		Kind:    kind,
		Title:   "当前架构不支持 " + kind,
		Blocked: "Fingerprint (fingerprint-chromium) 目前支持 Windows x64、macOS Apple Silicon 和 Linux x64。",
	}
}

var wizardSupportedPlatforms = []string{"豆包", "千问", "千问国际", "Gemini"}

func wizardValidPlatform(name string) bool {
	for _, p := range wizardSupportedPlatforms {
		if p == name {
			return true
		}
	}
	return false
}

func wizardDefaultChoice(selected []string) string {
	for _, p := range wizardSupportedPlatforms {
		for _, item := range selected {
			if item == p {
				return p
			}
		}
	}
	return wizardSupportedPlatforms[0]
}

type wizardQueue struct {
	Left []string
}

func newWizardQueue(platforms []string) wizardQueue {
	return wizardQueue{Left: append([]string{}, platforms...)}
}

func (q wizardQueue) Current() string {
	if len(q.Left) == 0 {
		return ""
	}
	return q.Left[0]
}

func (q wizardQueue) Advance() wizardQueue {
	if len(q.Left) == 0 {
		return q
	}
	return wizardQueue{Left: append([]string{}, q.Left[1:]...)}
}

func (q wizardQueue) Done() bool { return len(q.Left) == 0 }

type wizardPoll int

const (
	wizardPollWait wizardPoll = iota
	wizardPollNext
	wizardPollDone
	wizardPollStop
)

func wizardLoggedIn(raw string) bool {
	return strings.TrimSpace(raw) == wizardProbeIn
}

func wizardAfterLogin(q wizardQueue, loggedIn bool) (wizardQueue, wizardPoll) {
	if !loggedIn {
		return q, wizardPollWait
	}
	q = q.Advance()
	if q.Done() {
		return q, wizardPollDone
	}
	return q, wizardPollNext
}

// nextInstanceName 按平台取最小的未占用编号。豆包和千问各自从 1 起。
func nextInstanceName(platform string, used []string) string {
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
			return prefix + strconv.Itoa(i)
		}
	}
	return prefix
}

// isDefaultPlatformName 报告名称是否仍是系统自动生成的占位名（如「豆包1」「千问2」）。
func isDefaultPlatformName(platform, name string) bool {
	prefix := strings.TrimSpace(platform)
	if prefix == "" {
		return false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(name), prefix)
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// uniqueAccountName 清洗抓取到的账号用户名，并在同平台存在重名时追加序号后缀。
func uniqueAccountName(candidate string, used []string) string {
	base := cdp.SanitizeAccountUsername(candidate)
	if base == "" {
		return ""
	}
	taken := func(name string) bool {
		for _, item := range used {
			if strings.EqualFold(strings.TrimSpace(item), name) {
				return true
			}
		}
		return false
	}
	if !taken(base) {
		return base
	}
	for i := 2; i < 1000; i++ {
		alt := fmt.Sprintf("%s (%d)", base, i)
		if !taken(alt) {
			return alt
		}
	}
	return base
}
