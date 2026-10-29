package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Line 是一条可按 task_id 追查的结构化日志。
type Line struct {
	RunID    string
	TaskID   int
	Platform string
	Account  string
	Port     int
	Stage    string
	Elapsed  time.Duration
	Images   int
	Bytes    int
	Error    string
	Msg      string
}

var (
	traceMu   sync.Mutex
	traceLog  *slog.Logger
	tracePath string
	runID     string
)

// Open 把结构化日志写到数据目录 logs/tulai.log，并同时打到 stderr。
func Open(dir string) error {
	if dir == "" {
		dir = "."
	}
	dir = filepath.Join(dir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "tulai.log")
	w, err := newRotator(path, 2<<20, 3)
	if err != nil {
		return err
	}
	h := slog.NewTextHandler(io.MultiWriter(os.Stderr, w), &slog.HandlerOptions{Level: slog.LevelDebug})
	traceMu.Lock()
	traceLog = slog.New(h)
	tracePath = path
	traceMu.Unlock()
	return nil
}

// Path 是当前日志文件。还没打开时为空。
func Path() string {
	traceMu.Lock()
	defer traceMu.Unlock()
	return tracePath
}

// SetRun 记下这一批的 run_id。空则按时间生成。
func SetRun(id string) {
	traceMu.Lock()
	runID = id
	traceMu.Unlock()
}

// Run 返回当前 run_id。
func Run() string {
	traceMu.Lock()
	defer traceMu.Unlock()
	if runID == "" {
		runID = time.Now().Format("20060102T150405")
	}
	return runID
}

// Event 写一条带齐字段的日志，并把中文句子放进界面上的运行日志。
func Event(line Line) {
	if line.RunID == "" {
		line.RunID = Run()
	}
	if line.Stage == "" {
		line.Stage = "log"
	}
	writeSlog(slog.LevelInfo, line)
	if line.Msg != "" {
		Log.addRaw(Info, line.Msg)
	}
}

func writeSlog(level slog.Level, line Line) {
	traceMu.Lock()
	lg := traceLog
	traceMu.Unlock()
	if lg == nil {
		return
	}
	lg.Log(context.Background(), level, line.Msg, line.attrs()...)
}

func (line Line) attrs() []any {
	errText := line.Error
	return []any{
		"run_id", line.RunID,
		"task_id", line.TaskID,
		"platform", line.Platform,
		"account", line.Account,
		"port", line.Port,
		"stage", line.Stage,
		"elapsed_ms", line.Elapsed.Milliseconds(),
		"images", line.Images,
		"bytes", line.Bytes,
		"error", errText,
	}
}

func (l *Logger) addRaw(level Level, msg string) {
	if l == nil {
		return
	}
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

func (l *Logger) add(level Level, msg string) {
	l.addRaw(level, msg)
	slogLevel := slog.LevelInfo
	if level == Debug {
		slogLevel = slog.LevelDebug
	}
	writeSlog(slogLevel, Line{Stage: "log", Msg: msg})
}

type rotator struct {
	mu   sync.Mutex
	path string
	max  int64
	keep int
	f    *os.File
	n    int64
}

func newRotator(path string, max int64, keep int) (*rotator, error) {
	if keep < 1 {
		keep = 1
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &rotator{path: path, max: max, keep: keep, f: f, n: info.Size()}, nil
}

func (r *rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.n+int64(len(p)) > r.max {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.n += int64(n)
	return n, err
}

func (r *rotator) rotate() error {
	if err := r.f.Close(); err != nil {
		return err
	}
	for i := r.keep - 1; i >= 1; i-- {
		_ = os.Rename(r.backup(i), r.backup(i+1))
	}
	if err := os.Rename(r.path, r.backup(1)); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	r.f = f
	r.n = 0
	return nil
}

func (r *rotator) backup(i int) string {
	return fmt.Sprintf("%s.%d", r.path, i)
}
