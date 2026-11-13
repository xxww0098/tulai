package logging

import (
	"strconv"
	"strings"
	"time"
)

// Format 是 txt 日志的排版样式。各段分开拼装，空字段整段省略，
// 不再出现 task_id=0、port=0、error="" 这类噪声。
type Format struct {
	TimeLayout string
	TimeZone   *time.Location
	Separator  string
}

// DefaultFormat 是写入日志文件和 stderr 的样式。
var DefaultFormat = Format{
	TimeLayout: "2006-01-02 15:04:05.000",
	Separator:  "  ",
}

// line 把一条结构化日志排成一行纯文本：
//
//	时间  级别  [平台/账号:端口 · 阶段]  中文句子  (耗时 · 数量 · 大小 · 错误)  task_id=N run_id=...
func (f Format) line(level Level, line Line, at time.Time) string {
	if f.TimeLayout == "" {
		f.TimeLayout = "2006-01-02 15:04:05.000"
	}
	if f.Separator == "" {
		f.Separator = "  "
	}
	if f.TimeZone != nil {
		at = at.In(f.TimeZone)
	}
	parts := []string{at.Format(f.TimeLayout), level.String()}
	for _, part := range []string{formatScope(line), formatMessage(line.Msg), formatDetails(line), formatTrailer(line)} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, f.Separator) + "\n"
}

// formatScope 是每行的模块块：谁（平台/账号/端口）在哪个阶段（stage）做事。
// stage=log 是普通界面日志的默认值，显示出来只会添乱。
func formatScope(line Line) string {
	scope := make([]string, 0, 2)
	if line.Platform != "" || line.Account != "" {
		who := line.Platform
		if line.Account != "" {
			if who != "" {
				who += "/"
			}
			who += line.Account
		}
		if line.Port > 0 {
			who += ":" + strconv.Itoa(line.Port)
		}
		scope = append(scope, who)
	} else if line.Port > 0 {
		scope = append(scope, "port="+strconv.Itoa(line.Port))
	}
	if stage := cleanToken(line.Stage); stage != "" && stage != "log" {
		scope = append(scope, stage)
	}
	if len(scope) == 0 {
		return ""
	}
	return "[" + strings.Join(scope, " · ") + "]"
}

func formatMessage(msg string) string { return cleanText(msg) }

// formatDetails 是括号里的运行详情，按耗时、张数、大小、错误排列。
func formatDetails(line Line) string {
	details := make([]string, 0, 5)
	if line.Elapsed > 0 {
		details = append(details, line.Elapsed.Round(time.Millisecond).String())
	}
	if line.Total > 0 {
		details = append(details, "进度 "+strconv.Itoa(line.Done)+"/"+strconv.Itoa(line.Total))
	}
	if line.Images > 0 {
		details = append(details, strconv.Itoa(line.Images)+" 张")
	}
	if line.Bytes > 0 {
		details = append(details, formatBytes(line.Bytes))
	}
	if errText := cleanText(line.Error); errText != "" {
		details = append(details, "错误: "+errText)
	}
	if len(details) == 0 {
		return ""
	}
	return "(" + strings.Join(details, " · ") + ")"
}

// formatTrailer 保留 task_id 和 run_id 的 grep 写法，排在行尾不打断阅读。
func formatTrailer(line Line) string {
	fields := make([]string, 0, 2)
	if line.TaskID != 0 {
		fields = append(fields, "task_id="+strconv.Itoa(line.TaskID))
	}
	if run := cleanToken(line.RunID); run != "" {
		fields = append(fields, "run_id="+run)
	}
	return strings.Join(fields, " ")
}

// cleanText 把多行诊断压回一行，保证一条日志只占一个物理行。
func cleanText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n", " ⏎ ")
	text = strings.ReplaceAll(text, "\t", " ")
	return strings.TrimSpace(text)
}

// cleanToken 保证 stage、run_id 这类字段不会带出空格或换行。
func cleanToken(text string) string {
	return strings.Join(strings.Fields(text), "_")
}

func formatBytes(n int) string {
	const unit = 1024
	if n < unit {
		return strconv.Itoa(n) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(n)
	index := -1
	for value >= unit && index < len(units)-1 {
		value /= unit
		index++
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + units[index]
}
