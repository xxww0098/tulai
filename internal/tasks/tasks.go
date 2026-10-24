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

// Task 一条批量任务：一个平台 × 一组源图 × 一条提示词。
type Task struct {
	Index     int      // 序号（从 1 起）
	Platform  string   // 豆包 / 千问
	BrowserID int64    // 执行用的浏览器（构建后分配）
	Prompt    string   // 提示词（已拼后缀）
	Images    []string // 源图路径
	Status    Status
	Attempts  int // 已尝试次数
	Error     string
	Results   []string // 已保存结果路径

	submitted          bool // 已确认发送，重试时继续等待原任务
	savedHashes        map[string]bool
	matchedSavedResult bool // 本轮是否再次获取到本任务已成功落盘的原图
}

// Options 引擎运行参数（由设置表构造）。
type Options struct {
	Platforms          []string
	Prompts            []string
	PromptSuffix       string
	UseSuffix          bool
	Images             []string
	Concurrency        int
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

// BuildQueue 顺序循环策略：每张源图一个任务，提示词按顺序循环使用。
func BuildQueue(opts Options, browsersOf map[string][]int64) []Task {
	var out []Task
	idx := 0
	browserIdx := map[string]int{} // 每平台浏览器轮转游标
	for _, platform := range opts.Platforms {
		if ids := browsersOf[platform]; opts.StartBrowserIndex > 1 && len(ids) > 0 {
			browserIdx[platform] = (opts.StartBrowserIndex - 1) % len(ids)
		}
	}
	for _, img := range opts.Images {
		for _, platform := range opts.Platforms {
			idx++
			prompt := ""
			if len(opts.Prompts) > 0 {
				prompt = opts.Prompts[(idx-1)%len(opts.Prompts)]
			}
			if opts.UseSuffix && opts.PromptSuffix != "" {
				prompt = strings.TrimSpace(prompt + "\n" + opts.PromptSuffix)
			}
			t := Task{
				Index:    idx,
				Platform: platform,
				Prompt:   prompt,
				Images:   []string{img},
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
