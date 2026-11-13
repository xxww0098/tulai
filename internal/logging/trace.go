package logging

import (
	"io"
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
	Done     int
	Total    int
	Images   int
	Bytes    int
	Error    string
	Msg      string
}

var (
	traceMu      sync.Mutex
	traceWriteMu sync.Mutex
	traceOut     io.Writer
	traceRotate  *rotator
	traceFailed  bool // guarded by traceWriteMu
	runID        string
)

// Open 把结构化日志按天切片写到数据目录 logs/tulai.YYYYMMDD.log，并同时打到 stderr。
func Open(dir string) error {
	return open(dir, os.Stderr)
}

// 文件是主要输出；无控制台的图形程序可能没有可写的 stderr。
type traceWriter struct {
	file   io.Writer
	stderr io.Writer
}

func (w traceWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	if w.stderr != nil {
		_, _ = w.stderr.Write(p)
	}
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func open(dir string, stderr io.Writer) error {
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
	traceWriteMu.Lock()
	defer traceWriteMu.Unlock()
	traceMu.Lock()
	previous := traceRotate
	traceOut = traceWriter{file: w, stderr: stderr}
	traceRotate = w
	traceFailed = false
	traceMu.Unlock()
	if previous != nil && previous != w {
		_ = previous.Close()
	}
	return nil
}

// Close 关闭当前日志文件。主要给测试和需要干净退出的调用方使用。
func Close() error {
	traceWriteMu.Lock()
	defer traceWriteMu.Unlock()
	traceMu.Lock()
	r := traceRotate
	traceOut = nil
	traceRotate = nil
	traceFailed = false
	traceMu.Unlock()
	if r == nil {
		return nil
	}
	return r.Close()
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
	runID = strings.TrimSpace(id)
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
	writeLine(level, line)
	if line.Msg != "" {
		Log.addRaw(level, line.Msg)
	}
}

// writeLine 是文件输出的唯一出口：格式化、加锁、整行写入，避免并发日志互相截断。
func writeLine(level Level, line Line) {
	if line.RunID == "" {
		line.RunID = Run()
	}
	text := DefaultFormat.line(level, line, time.Now())
	traceWriteMu.Lock()
	traceMu.Lock()
	out := traceOut
	traceMu.Unlock()
	if out == nil {
		traceWriteMu.Unlock()
		return
	}
	_, err := io.WriteString(out, text)
	notify := err != nil && !traceFailed
	traceFailed = err != nil
	traceWriteMu.Unlock()
	if notify {
		// Do not recurse into the failing file writer; surface the problem in
		// the in-memory/UI log once until a later write succeeds.
		Log.addRaw(Error, "写入日志文件失败："+err.Error())
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
	writeLine(level, boundLine(Line{Stage: "log", Msg: msg}))
}
