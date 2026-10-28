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

// Entry 一条日志。
type Entry struct {
	Time  time.Time
	Level Level
	Text  string
}

// Log 全局日志实例。
var Log = New(600)

// Logger 线程安全的环形日志，支持一个订阅回调（UI 刷新用）。
type Logger struct {
	mu     sync.Mutex
	buf    []Entry
	max    int
	sink   func(Entry)
	onSink sync.Once
}

// New 创建容量 max 的日志器。
func New(max int) *Logger { return &Logger{buf: make([]Entry, 0, max), max: max} }

// SetSink 设置订阅回调（只允许设置一次，在 UI 就绪时调用）。
func (l *Logger) SetSink(fn func(Entry)) {
	l.mu.Lock()
	l.sink = fn
	l.mu.Unlock()
}

func (l *Logger) add(level Level, msg string) {
	e := Entry{Time: time.Now(), Level: level, Text: msg}
	l.mu.Lock()
	if len(l.buf) >= l.max {
		l.buf = l.buf[1:]
	}
	l.buf = append(l.buf, e)
	sink := l.sink
	l.mu.Unlock()
	if sink != nil {
		sink(e)
	}
}

// Entries 返回日志快照。
func (l *Logger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.buf))
	copy(out, l.buf)
	return out
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
