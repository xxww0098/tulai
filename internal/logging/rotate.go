package logging

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

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
	day := r.now().Format(dayLayout)
	if r.f == nil || day != r.day {
		if err := r.switchTo(day); err != nil {
			return 0, err
		}
	}
	return r.f.Write(p)
}

// Close 释放当前切片，关闭后再次写入会重新打开当天文件。
func (r *rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
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
