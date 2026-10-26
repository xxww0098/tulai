package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"xbs/internal/tasks"
	"xbs/internal/wizard"
)

const (
	pageHome = iota
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
	want := func(platform string) bool {
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
		return WorkbenchPlan{Action: StartWizard}
	}
	return WorkbenchPlan{Action: StartGenerate, Platforms: platforms}
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
	var running, pending, needs, failed, success bool
	for _, status := range statuses {
		switch status {
		case tasks.StatusRunning, tasks.StatusRetry:
			running = true
		case tasks.StatusPending:
			pending = true
		case tasks.StatusNeedsUser:
			needs = true
		case tasks.StatusFailed, tasks.StatusSkipped:
			failed = true
		case tasks.StatusSuccess:
			success = true
		}
	}
	switch {
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
	Platforms []string
	Results   []string
	Retry     []tasks.Task
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
		reason := ""
		for _, task := range group {
			statuses = append(statuses, task.Status)
			if task.Platform != "" && !seen[task.Platform] {
				seen[task.Platform] = true
				card.Platforms = append(card.Platforms, task.Platform)
			}
			card.Results = append(card.Results, task.Results...)
			switch task.Status {
			case tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser:
				card.Retry = append(card.Retry, task)
				if reason == "" {
					reason = task.Error
				}
			}
		}
		card.Status = DisplayStatus(statuses)
		if card.Status == "失败" || card.Status == "待验证" {
			if strings.TrimSpace(reason) != "" {
				card.Reason = HumanReason(reason)
			} else if card.Status == "失败" {
				card.Reason = "出图失败，请重试"
			}
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
	case strings.Contains(s, "验证"):
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
