// Package tasks 批量任务模型与执行引擎。对应原软件 MainForm 的任务队列：
// BuildTaskQueue / ExecuteTasksAsync / StartTasksAsync / StopTasks / UpdateQueueProgress。
package tasks

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Status 任务状态。
type Status string

// 任务状态常量。
const (
	StatusPending   Status = "等待"
	StatusRunning   Status = "执行中"
	StatusRetry     Status = "重试中"
	StatusSuccess   Status = "成功"
	StatusFailed    Status = "失败"
	StatusSkipped   Status = "跳过"
	StatusNeedsUser Status = "待验证"
)

// Task 一条批量任务：一个平台 × 一条提示词。图生图另带一张源图；文生图 Images 为空。
type Task struct {
	Index     int      // 序号（从 1 起）
	Platform  string   // 豆包 / 千问
	BrowserID int64    // 执行用的浏览器（构建后分配）
	Prompt    string   // 提示词（已拼后缀）
	Images    []string // 源图路径；文生图为空，执行时不上传
	Status    Status
	Attempts  int // 已尝试次数
	Error     string
	Results   []string // 已保存结果路径

	submitted          bool // 已确认发送，重试时继续等待原任务
	savedHashes        map[string]bool
	matchedSavedResult bool  // 本轮是否再次获取到本任务已成功落盘的原图
	slot               int64 // 这次派发时占用的浏览器，完成后用来释放并发名额
	ranOn              int64 // 实际打到的账号。轮换后 BrowserID 会改成下一个
	rotated            bool  // 失败后已改派，调度器要把任务放回队列
}

// Options 引擎运行参数（由设置表构造）。
type Options struct {
	Platforms          []string
	Prompts            []string
	PromptSuffix       string
	UseSuffix          bool
	Images             []string
	Concurrency        int // 0 按可用账号自动；正数是同时出图的上限
	RetryCount         int
	SendWait           int // 发送间隔秒
	WaitBeforeSend     int // 发送前等待秒
	WaitResult         bool
	WaitResultSeconds  int
	TaskSwitching      bool
	TaskSwitchingCount int
	StartBrowserIndex  int
	SaveMode           string // 单文件夹汇总 / 按源图片分文件夹
	NamingRule         string // 原文件名 / 时间戳 / 自定义
	FilePrefix         string
	FilePrefixEnabled  bool
	FileSuffix         string
	FileSuffixEnabled  bool
	OutputPath         string
	RemoveWatermark    bool // 另存计算去水印版本，始终保留原图
}

// queueUnit 是队列的一行原料。图生图带一张源图并循环提示词；文生图固定一条提示词且没有源图。
type queueUnit struct {
	image    string
	hasImage bool
	prompt   string
	fixed    bool
}

// BuildQueue 生成批量队列。
//
// 图生图：Images 非空时，每张源图 × 每个平台一个任务，提示词按任务序号顺序循环，可以为空。
// 文生图：Images 为空且 Prompts 含非空白条目时，每条提示词 × 每个平台一个任务，不附带源图。
// 两条都空时返回空队列。
func BuildQueue(opts Options, browsersOf map[string][]int64) []Task {
	var out []Task
	idx := 0
	browserIdx := map[string]int{} // 每平台浏览器轮转游标
	for _, platform := range opts.Platforms {
		if ids := browsersOf[platform]; opts.StartBrowserIndex > 1 && len(ids) > 0 {
			browserIdx[platform] = (opts.StartBrowserIndex - 1) % len(ids)
		}
	}
	var units []queueUnit
	if len(opts.Images) > 0 {
		for _, img := range opts.Images {
			units = append(units, queueUnit{image: img, hasImage: true})
		}
	} else {
		for _, prompt := range opts.Prompts {
			prompt = strings.TrimSpace(prompt)
			if prompt == "" {
				continue
			}
			units = append(units, queueUnit{prompt: prompt, fixed: true})
		}
	}
	for _, unit := range units {
		for _, platform := range opts.Platforms {
			idx++
			prompt := unit.prompt
			if !unit.fixed && len(opts.Prompts) > 0 {
				prompt = opts.Prompts[(idx-1)%len(opts.Prompts)]
			}
			if opts.UseSuffix && opts.PromptSuffix != "" {
				prompt = strings.TrimSpace(prompt + "\n" + opts.PromptSuffix)
			}
			var images []string
			if unit.hasImage {
				images = []string{unit.image}
			}
			t := Task{
				Index:    idx,
				Platform: platform,
				Prompt:   prompt,
				Images:   images,
				Status:   StatusPending,
			}
			// 分配浏览器（按平台轮转）
			if ids := browsersOf[platform]; len(ids) > 0 {
				bi := browserIdx[platform]
				t.BrowserID = ids[bi%len(ids)]
				browserIdx[platform] = bi + 1
			}
			out = append(out, t)
		}
	}
	return out
}

// RebuildRetry 把失败、跳过或待验证的任务收成一批新的等待任务。
// 提示词保持原样（后缀已经拼过），不沿用上次的账号，方便重新挑一个健康账号。
func RebuildRetry(items []Task) []Task {
	var out []Task
	for _, task := range items {
		switch task.Status {
		case StatusFailed, StatusSkipped, StatusNeedsUser:
		default:
			continue
		}
		if strings.TrimSpace(task.Platform) == "" {
			continue
		}
		if len(task.Images) == 0 && strings.TrimSpace(task.Prompt) == "" {
			continue
		}
		next := Task{
			Index:    len(out) + 1,
			Platform: task.Platform,
			Prompt:   task.Prompt,
			Status:   StatusPending,
		}
		if len(task.Images) > 0 {
			next.Images = append([]string(nil), task.Images...)
		}
		out = append(out, next)
	}
	return out
}

// resultSource 是命名和分文件夹用的路径。文生图没有源图，用提示词第一行造一个文件名。
func resultSource(t *Task) string {
	if t != nil && len(t.Images) > 0 {
		return t.Images[0]
	}
	prompt := ""
	if t != nil {
		prompt = t.Prompt
	}
	return promptFileName(prompt)
}

// promptFileName 把提示词收成可用的文件名。只取第一行，去掉路径非法字符，过长则截断。
func promptFileName(prompt string) string {
	line := strings.TrimSpace(prompt)
	if i := strings.IndexAny(line, "\r\n"); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	var b strings.Builder
	for _, r := range line {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			if r < 32 {
				b.WriteByte('_')
			} else {
				b.WriteRune(r)
			}
		}
	}
	name := strings.Trim(b.String(), " .")
	if rs := []rune(name); len(rs) > 40 {
		name = strings.Trim(string(rs[:40]), " .")
	}
	if name == "" || name == "." || name == ".." {
		name = "文生图"
	}
	return name + ".png"
}

// NameFor 结果文件名：按命名规则 + 前后缀。
func (o Options) NameFor(sourcePath string, seq, total int) string {
	base := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	switch o.NamingRule {
	case "时间戳":
		base = timestamp()
	case "序号":
		base = fmt.Sprintf("%03d", seq)
	}
	if o.FilePrefixEnabled && o.FilePrefix != "" {
		base = o.FilePrefix + base
	}
	if o.FileSuffixEnabled && o.FileSuffix != "" {
		base = base + o.FileSuffix
	}
	if total > 1 {
		base = fmt.Sprintf("%s_%02d", base, seq)
	}
	return base + ".png"
}

// ResultDir 结果目录：单文件夹汇总 / 按源图片分文件夹。
func (o Options) ResultDir(sourcePath string) string {
	if o.SaveMode == "按源图片分文件夹" {
		base := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
		return filepath.Join(o.OutputPath, base)
	}
	return o.OutputPath
}

func timestamp() string { return time.Now().Format("20060102_150405") }

// QueueView 队列快照（UI 读取）。
type QueueView struct {
	mu    sync.Mutex
	items []Task
}

// NewQueueView 创建队列视图。
func NewQueueView(items []Task) *QueueView {
	cp := make([]Task, len(items))
	copy(cp, items)
	return &QueueView{items: cp}
}

// Snapshot 返回队列快照。
func (q *QueueView) Snapshot() []Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Task, len(q.items))
	copy(out, q.items)
	return out
}

// Update 更新指定任务（按 Index）。
func (q *QueueView) Update(t Task) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.items {
		if q.items[i].Index == t.Index {
			q.items[i] = t
			return
		}
	}
}
