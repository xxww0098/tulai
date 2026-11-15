package logging

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 面板上的级别短码。成功并进「信息」，行上仍标 OK。
func LevelCode(level Level) string {
	switch level {
	case Debug:
		return "DBG"
	case Warning:
		return "WRN"
	case Error:
		return "ERR"
	case Success:
		return "OK"
	default:
		return "INF"
	}
}

// SourceOf 是这一行从哪来。没有平台和账号时归到应用。
func SourceOf(line Line) string {
	who := strings.TrimSpace(line.Platform)
	if account := strings.TrimSpace(line.Account); account != "" {
		if who != "" {
			who += "/"
		}
		who += account
	}
	if who == "" {
		return "应用"
	}
	return who
}

func entryDetail(line Line) string {
	parts := make([]string, 0, 4)
	if stage := strings.TrimSpace(line.Stage); stage != "" && stage != "log" {
		parts = append(parts, stage)
	}
	if errText := strings.TrimSpace(line.Error); errText != "" {
		parts = append(parts, errText)
	}
	if line.TaskID != 0 {
		parts = append(parts, "task_id="+strconv.Itoa(line.TaskID))
	}
	if run := strings.TrimSpace(line.RunID); run != "" {
		parts = append(parts, "run_id="+run)
	}
	return strings.Join(parts, " · ")
}

var (
	bearerSecret = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
	cookieHeader = regexp.MustCompile(`(?i)\bCookie\s*:\s*[^\r\n]+`)
	namedSecret  = regexp.MustCompile(`(?i)(["']?\b(?:token|password|secret|authorization|api[_-]?key|access[_-]?key|cookie)\b["']?\s*[:=]\s*["']?)[^\s"',;}]{4,}`)
)

// Redact 抹掉复制、导出和面板上能直接读到的凭据。磁盘日志保持原样。
// Cookie 头整段换掉，避免分号后面的下一枚 cookie 留下来。
func Redact(text string) string {
	if text == "" {
		return ""
	}
	text = cookieHeader.ReplaceAllString(text, "Cookie: [omitted]")
	text = bearerSecret.ReplaceAllString(text, "$1 [omitted]")
	return namedSecret.ReplaceAllString(text, "$1[omitted]")
}

// View 是面板上的筛选。Level 为 -1 时不过滤级别。Source 空着表示全部来源。
type View struct {
	Level  Level
	Query  string
	Source string
}

// AllowLevel 报告这一级是否留在当前筛选里。成功算信息。
func (v View) AllowLevel(level Level) bool {
	if v.Level < 0 {
		return true
	}
	if v.Level == Info && level == Success {
		return true
	}
	return level == v.Level
}

// Match 同时看级别、来源和搜索词。
func (v View) Match(entry Entry) bool {
	if !v.AllowLevel(entry.Level) {
		return false
	}
	if v.Source != "" && entry.Source != v.Source {
		return false
	}
	query := strings.ToLower(strings.TrimSpace(v.Query))
	if query == "" {
		return true
	}
	blob := strings.ToLower(strings.Join([]string{
		LevelCode(entry.Level), entry.Source, entry.Text, entry.Detail,
	}, " "))
	return strings.Contains(blob, query)
}

// Counts 是缓冲里各级的条数。信息含成功。全部是总和。
type Counts struct {
	All, Debug, Info, Warn, Error int
}

func CountLevels(entries []Entry) Counts {
	var c Counts
	for _, entry := range entries {
		c.All++
		switch entry.Level {
		case Debug:
			c.Debug++
		case Warning:
			c.Warn++
		case Error:
			c.Error++
		default:
			c.Info++
		}
	}
	return c
}

// Sources 按出现顺序列出去重来源。
func Sources(entries []Entry) []string {
	seen := map[string]bool{}
	var out []string
	for _, entry := range entries {
		if entry.Source == "" || seen[entry.Source] {
			continue
		}
		seen[entry.Source] = true
		out = append(out, entry.Source)
	}
	return out
}

// Filter 返回当前视图要画的行，顺序不变。
func Filter(entries []Entry, view View) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if view.Match(entry) {
			out = append(out, entry)
		}
	}
	return out
}

// CopyLine 是复制和导出用的一行，已经脱敏。
func CopyLine(entry Entry) string {
	parts := []string{
		entry.Time.Local().Format("2006-01-02 15:04:05.000"),
		LevelCode(entry.Level),
		"[" + entry.Source + "]",
		entry.Text,
	}
	if entry.Detail != "" {
		parts = append(parts, entry.Detail)
	}
	if entry.Elapsed > 0 {
		parts = append(parts, entry.Elapsed.Round(time.Millisecond).String())
	}
	return strings.Join(parts, " ")
}

// ExportText 把当前筛选结果按时间从旧到新拼成文件内容。
func ExportText(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries {
		b.WriteString(CopyLine(entry))
		b.WriteByte('\n')
	}
	return b.String()
}

// Window 是虚拟列表这一帧要实例化的半开区间，以及上下占位高度。
// 占位按每行真实高度累加，展开详情不会把后面的行挤错位。
type Window struct {
	Start, End  int
	Top, Bottom float32
}

// VisibleWindow 用每行自己的高度计算可见区间。scroll 和 view 是像素。
func VisibleWindow(heights []float32, view, scroll float32) Window {
	n := len(heights)
	if n == 0 {
		return Window{}
	}
	var total float32
	for _, h := range heights {
		if h < 1 {
			h = 1
		}
		total += h
	}
	if view < 1 {
		view = 160
	}
	maxScroll := total - view
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	var acc float32
	start := 0
	for i, h := range heights {
		if h < 1 {
			h = 1
		}
		if acc+h > scroll {
			start = i
			break
		}
		acc += h
		start = i + 1
	}
	if start > n {
		start = n
	}
	top := acc
	end := start
	shown := float32(0)
	overscan := float32(0)
	for end < n && shown < view+overscan {
		h := heights[end]
		if h < 1 {
			h = 1
		}
		shown += h
		end++
		if overscan == 0 && shown >= view {
			overscan = h
		}
	}
	var bottom float32
	for i := end; i < n; i++ {
		h := heights[i]
		if h < 1 {
			h = 1
		}
		bottom += h
	}
	return Window{Start: start, End: end, Top: top, Bottom: bottom}
}

// FrameKick 把同一阵日志收成一次界面唤醒。Done 之后下一条才能再醒。
type FrameKick struct {
	mu   sync.Mutex
	hot  bool
	wake int
}

// Kick 在尚未醒着时调用 wake 一次。
func (k *FrameKick) Kick(wake func()) {
	if k == nil {
		if wake != nil {
			wake()
		}
		return
	}
	k.mu.Lock()
	if k.hot {
		k.mu.Unlock()
		return
	}
	k.hot = true
	k.wake++
	k.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// Done 表示这一帧已经画完，下一批日志可以再醒一次。
func (k *FrameKick) Done() {
	if k == nil {
		return
	}
	k.mu.Lock()
	k.hot = false
	k.mu.Unlock()
}

// Wakes 是 Kick 真正叫醒的次数。
func (k *FrameKick) Wakes() int {
	if k == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.wake
}
