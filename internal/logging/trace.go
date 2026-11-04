package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	traceMu     sync.Mutex
	traceLog    *slog.Logger
	traceRotate *rotator
	runID       string
)

// Open 把结构化日志按天切片写到数据目录 logs/tulai.YYYYMMDD.log，并同时打到 stderr。
func Open(dir string) error {
	if dir == "" {
		dir = "."
	}
	dir = filepath.Join(dir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	w, err := newRotator(dir, "tulai", keepDays, time.Now)
	if err != nil {
		return err
	}
	h := slog.NewTextHandler(io.MultiWriter(os.Stderr, w), &slog.HandlerOptions{Level: slog.LevelDebug})
	traceMu.Lock()
	traceLog = slog.New(h)
	traceRotate = w
	traceMu.Unlock()
	return nil
}

// Path 是当前日志文件。还没打开时为空。
func Path() string {
	traceMu.Lock()
	r := traceRotate
	traceMu.Unlock()
	if r == nil {
		return ""
	}
	return r.currentPath()
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
	Emit(Info, line)
}

// Emit 和 Event 一样带上平台和账号，界面上的颜色跟 level 走。
func Emit(level Level, line Line) {
	if line.RunID == "" {
		line.RunID = Run()
	}
	if line.Stage == "" {
		line.Stage = "log"
	}
	slogLevel := slog.LevelInfo
	if level == Debug {
		slogLevel = slog.LevelDebug
	}
	writeSlog(slogLevel, line)
	if line.Msg != "" {
		Log.addRaw(level, line.Msg)
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

type boundAccount struct {
	Platform string
	Account  string
	Port     int
	TaskID   int
}

var bounds sync.Map

func goid() uint64 {
	var buf [48]byte
	n := runtime.Stack(buf[:], false)
	var id uint64
	for _, c := range buf[:n] {
		if c < '0' || c > '9' {
			if id > 0 {
				break
			}
			continue
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

// Bind 让当前 goroutine 之后的日志带上平台和账号。返回的函数结束这段绑定。
func Bind(platform, account string, port, taskID int) func() {
	id := goid()
	prev, had := bounds.Load(id)
	bounds.Store(id, boundAccount{Platform: platform, Account: account, Port: port, TaskID: taskID})
	return func() {
		if had {
			bounds.Store(id, prev)
			return
		}
		bounds.Delete(id)
	}
}

func boundLine(line Line) Line {
	v, ok := bounds.Load(goid())
	if !ok {
		return line
	}
	b := v.(boundAccount)
	if line.Platform == "" {
		line.Platform = b.Platform
	}
	if line.Account == "" {
		line.Account = b.Account
	}
	if line.Port == 0 {
		line.Port = b.Port
	}
	if line.TaskID == 0 {
		line.TaskID = b.TaskID
	}
	return line
}

func (l *Logger) add(level Level, msg string) {
	l.addRaw(level, msg)
	slogLevel := slog.LevelInfo
	if level == Debug {
		slogLevel = slog.LevelDebug
	}
	writeSlog(slogLevel, boundLine(Line{Stage: "log", Msg: msg}))
}

// 日志按天切片：每天一个 tulai.YYYYMMDD.log，跨天写入时自动切换，过期的旧片自动清理。
const (
	dayLayout = "20060102"
	keepDays  = 7
)

type rotator struct {
	mu   sync.Mutex
	dir  string
	stem string
	keep int
	now  func() time.Time
	f    *os.File
	day  string
}

func newRotator(dir, stem string, keep int, now func() time.Time) (*rotator, error) {
	if keep < 1 {
		keep = 1
	}
	if now == nil {
		now = time.Now
	}
	r := &rotator{dir: dir, stem: stem, keep: keep, now: now}
	if err := r.switchTo(now().Format(dayLayout)); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotator) path(day string) string {
	return filepath.Join(r.dir, r.stem+"."+day+".log")
}

// Write 跨天时先切到新一天的文件再写。
func (r *rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if day := r.now().Format(dayLayout); day != r.day {
		if err := r.switchTo(day); err != nil {
			return 0, err
		}
	}
	return r.f.Write(p)
}

// currentPath 是最近写入的日志文件。
func (r *rotator) currentPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.path(r.day)
}

// switchTo 打开指定日期的切片；成功后关闭旧文件并清理过期切片。
func (r *rotator) switchTo(day string) error {
	f, err := os.OpenFile(r.path(day), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if r.f != nil {
		_ = r.f.Close()
	}
	r.f = f
	r.day = day
	r.prune(day)
	return nil
}

// prune 删除保留窗口之外的旧切片；文件名对不上日期格式的一律不动。
func (r *rotator) prune(today string) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return
	}
	cut, err := time.ParseInLocation(dayLayout, today, time.Local)
	if err != nil {
		return
	}
	cut = cut.AddDate(0, 0, -(r.keep - 1)) // 保留今天在内共 keep 天
	prefix := r.stem + "."
	suffix := ".log"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		dayText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
		day, err := time.ParseInLocation(dayLayout, dayText, time.Local)
		if err != nil {
			continue
		}
		if day.Before(cut) {
			_ = os.Remove(filepath.Join(r.dir, name))
		}
	}
}
