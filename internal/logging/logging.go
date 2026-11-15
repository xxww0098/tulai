// Package logging 提供带级别的运行日志：UI 订阅展示，引擎与各模块写入。
package logging

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Level 日志级别，对应原软件 LogHelper 的 Info/Success/Warning/Debug。
type Level int

const (
	Info Level = iota
	Success
	Warning
	Error
	Debug
)

func (l Level) String() string {
	switch l {
	case Info:
		return "信息"
	case Success:
		return "成功"
	case Warning:
		return "警告"
	case Error:
		return "错误"
	case Debug:
		return "调试"
	}
	return "?"
}

// Entry 一条界面日志。正文和详情在入环时已脱敏；磁盘上的原始行不改。
type Entry struct {
	Seq     int
	Time    time.Time
	Level   Level
	Text    string
	Source  string
	Detail  string
	Elapsed time.Duration
}

// Log 全局日志实例。
var Log = New(8000)

// Logger 线程安全的环形日志。sink 给界面醒一帧；subs 是面板自己的订阅，关闭时卸掉。
type Logger struct {
	mu     sync.Mutex
	buf    []Entry
	max    int
	seq    int
	sink   func(Entry)
	subs   map[int]func(Entry)
	subID  int
	onSink sync.Once
}

// New 创建容量 max 的日志器。
func New(max int) *Logger { return &Logger{buf: make([]Entry, 0, max), max: max} }

// SetSink 设置界面醒帧回调。后一次替换前一次。
func (l *Logger) SetSink(fn func(Entry)) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.sink = fn
	l.mu.Unlock()
}

// Subscribe 登记一个订阅。返回的函数卸掉它；卸掉之后新日志不再送给这次订阅，再订一次又会收到。
func (l *Logger) Subscribe(fn func(Entry)) (cancel func()) {
	if l == nil || fn == nil {
		return func() {}
	}
	l.mu.Lock()
	if l.subs == nil {
		l.subs = map[int]func(Entry){}
	}
	l.subID++
	id := l.subID
	l.subs[id] = fn
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		delete(l.subs, id)
		l.mu.Unlock()
	}
}

// Clear 只丢掉内存里的面板缓冲。不删日志文件，也不动任务。
func (l *Logger) Clear() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.buf = l.buf[:0]
	l.mu.Unlock()
}

// Entries 返回日志快照。
func (l *Logger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.buf))
	copy(out, l.buf)
	return out
}

// VisibleTail 是界面上要留下的最近几条。调试行会在出图时刷满尾部，不能把它们算进这几条。
func VisibleTail(entries []Entry, limit int) []Entry {
	if limit < 1 {
		limit = 1
	}
	kept := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Level == Debug {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) > limit {
		kept = kept[len(kept)-limit:]
	}
	return kept
}

// Info 记录普通信息。
func (l *Logger) Info(format string, a ...any) { l.add(Info, fmt.Sprintf(format, a...)) }

// Success 记录成功信息。
func (l *Logger) Success(format string, a ...any) { l.add(Success, fmt.Sprintf(format, a...)) }

// Warning 记录警告。
func (l *Logger) Warning(format string, a ...any) { l.add(Warning, fmt.Sprintf(format, a...)) }

// Errorf 记录错误。
func (l *Logger) Errorf(format string, a ...any) { l.add(Error, fmt.Sprintf(format, a...)) }

// Debugf 记录调试信息。
func (l *Logger) Debugf(format string, a ...any) { l.add(Debug, fmt.Sprintf(format, a...)) }

// Join 预告词列表按行拆分并去空行。
func Join(parts ...string) string { return strings.Join(parts, " ") }
