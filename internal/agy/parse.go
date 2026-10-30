package agy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	quotaWord = regexp.MustCompile(`(?i)resource_exhausted|\bquota\b|rate[ _-]?limit`)
	code429   = regexp.MustCompile(`(^|[^0-9])429([^0-9]|$)`)
	resetText = regexp.MustCompile(`(?i)"(?:retryDelay|retry_delay|resetTime|reset_time)"\s*:\s*"([^"]+)"`)
	resetNum  = regexp.MustCompile(`(?i)"(?:retryDelay|retry_delay)"\s*:\s*(\d+)`)
	authURL   = regexp.MustCompile(`https://[^\s]+`)
)

// Stream 是一轮 stream-json 里和收图有关的字段。
type Stream struct {
	ConversationID string
	Status         string
	Response       string
	Quota          bool
	Until          time.Time
	Auth           bool
	Failed         bool
	// Denied 表示过程里出现了读文件权限被拒绝。
	Denied bool
}

// Feed 吃一行标准输出或标准错误。成功结果里即使出现额度字样也不当失败。
func (s *Stream) Feed(line string, stderr bool, now time.Time) {
	if s == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if permissionDenied(line) {
		s.Denied = true
	}
	if strings.Contains(line, "Authentication required") || strings.Contains(line, "paste the authorization code") {
		s.Auth = true
		return
	}
	var ev struct {
		Event          string `json:"event"`
		ConversationID string `json:"conversation_id"`
		Result         struct {
			Status   string `json:"status"`
			Response string `json:"response"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		if stderr && quotaText(line) {
			s.Quota = true
			if until := parseReset(line, now); !until.IsZero() {
				s.Until = until
			}
		}
		return
	}
	switch ev.Event {
	case "init":
		if ev.ConversationID != "" {
			s.ConversationID = ev.ConversationID
		}
	case "error":
		if quotaText(line) {
			s.Quota = true
			if until := parseReset(line, now); !until.IsZero() {
				s.Until = until
			}
		}
	case "result":
		status := strings.ToUpper(strings.TrimSpace(ev.Result.Status))
		s.Status = status
		if status == "SUCCESS" {
			s.Response = ev.Result.Response
			return
		}
		s.Failed = true
		if quotaText(line) {
			s.Quota = true
			if until := parseReset(line, now); !until.IsZero() {
				s.Until = until
			}
		}
	}
}

func quotaText(s string) bool {
	return quotaWord.MatchString(s) || code429.MatchString(s)
}

func permissionDenied(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "denied permission") || strings.Contains(low, "permission denied")
}

func parseReset(raw string, now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	if m := resetText.FindStringSubmatch(raw); len(m) == 2 {
		if t, err := time.Parse(time.RFC3339, m[1]); err == nil {
			return t
		}
		if d, err := time.ParseDuration(m[1]); err == nil && d > 0 {
			return now.Add(d)
		}
	}
	if m := resetNum.FindStringSubmatch(raw); len(m) == 2 {
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 {
			return now.Add(time.Duration(n) * time.Second)
		}
	}
	return time.Time{}
}

// AuthURL 从登录提示里取出要打开的链接。
func AuthURL(text string) string {
	m := authURL.FindString(text)
	return strings.TrimRight(m, ".,)")
}

// CollectImage 优先用结果里的路径；否则在这次对话目录里找任务开始后出现的图片。
// .tempmediaStorage 里的文件不算。
func CollectImage(home, conversationID, response string, started time.Time) (string, error) {
	response = strings.TrimSpace(response)
	if response != "" {
		if i := strings.IndexAny(response, "\r\n"); i >= 0 {
			response = strings.TrimSpace(response[:i])
		}
		if path, ok := cleanBrainPath(home, response); ok {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, nil
			}
		}
	}
	if conversationID == "" {
		return "", errNoImage
	}
	dir := BrainDir(home, conversationID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", errNoImage
	}
	var best string
	var bestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, ".tempmediaStorage") || !imageName(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !started.IsZero() && info.ModTime().Before(started.Add(-2*time.Second)) {
			continue
		}
		if best == "" || info.ModTime().After(bestTime) {
			best = filepath.Join(dir, name)
			bestTime = info.ModTime()
		}
	}
	if best == "" {
		return "", errNoImage
	}
	return best, nil
}

var (
	errNoImage = errString("没有找到生成的图片")
	errDenied  = errString("agy 拒绝读取参考图")
)

type errString string

func (e errString) Error() string { return string(e) }

func imageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func cleanBrainPath(home, path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	brain, err := filepath.Abs(filepath.Join(cliDir(home), "brain"))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(brain, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if strings.Contains(rel, ".tempmediaStorage") {
		return "", false
	}
	return abs, true
}
