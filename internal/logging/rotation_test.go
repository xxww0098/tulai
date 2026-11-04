package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRotatorSlicesByDay(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 3, 23, 59, 59, 0, time.Local)
	r, err := newRotator(dir, "tulai", 7, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write([]byte("day3 行\n")); err != nil {
		t.Fatal(err)
	}
	// 跨过午夜后再写，应该切到新一天的文件。
	now = now.Add(3 * time.Second)
	if _, err := r.Write([]byte("day4 行\n")); err != nil {
		t.Fatal(err)
	}
	day3, err := os.ReadFile(filepath.Join(dir, "tulai.20261103.log"))
	if err != nil {
		t.Fatal(err)
	}
	day4, err := os.ReadFile(filepath.Join(dir, "tulai.20261104.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(day3) != "day3 行\n" || string(day4) != "day4 行\n" {
		t.Fatalf("两天的日志串了: %q %q", day3, day4)
	}
	if got := r.currentPath(); got != filepath.Join(dir, "tulai.20261104.log") {
		t.Fatalf("当前文件该是新一天: %s", got)
	}
}

func TestRotatorPrunesOldSlicesOnly(t *testing.T) {
	dir := t.TempDir()
	files := map[string]bool{
		"tulai.20261020.log": false, // 14 天前，过期
		"tulai.20261028.log": true,  // 6 天前，保留窗口内
		"tulai.20261103.log": true,  // 今天
		"tulai.log":          true,  // 旧格式，不动
		"tulai.log.1":        true,  // 旧格式，不动
		"other.20260101.log": true,  // 别的文件，不动
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	if _, err := newRotator(dir, "tulai", 7, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		_, err := os.Stat(filepath.Join(dir, name))
		if want && err != nil {
			t.Fatalf("该保留的文件被删了: %s", name)
		}
		if !want && err == nil {
			t.Fatalf("过期的切片没清掉: %s", name)
		}
	}
}

func TestOpenWritesToTodaysSlice(t *testing.T) {
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	Log.Info("按天切片")
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("当天切片里应该有日志")
	}
}
