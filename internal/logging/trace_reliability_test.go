package logging

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type unavailableWriter struct{}

func (unavailableWriter) Write([]byte) (int, error) {
	return 0, errors.New("standard error unavailable")
}

func TestConcurrentLinesReachFileWhenStderrFails(t *testing.T) {
	if err := open(t.TempDir(), unavailableWriter{}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	SetRun("stderr-unavailable")
	t.Cleanup(func() { SetRun("") })
	const perAccount = 40
	var wg sync.WaitGroup
	for account := 1; account <= 2; account++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perAccount; i++ {
				Emit(Warning, Line{
					Platform: "豆包", Account: fmt.Sprintf("账号%d", account), TaskID: account,
					Stage: "fail", Elapsed: time.Second, Done: i + 1, Total: perAccount,
					Msg: fmt.Sprintf("账号%d条目%d", account, i), Error: "模拟下载错误",
				})
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2*perAccount {
		t.Fatalf("并发日志缺失或拆行: got %d, want %d", len(lines), 2*perAccount)
	}
	for account := 1; account <= 2; account++ {
		for i := 0; i < perAccount; i++ {
			marker := fmt.Sprintf("账号%d条目%d", account, i)
			found := false
			for _, line := range lines {
				if strings.Contains(line, marker+" ") {
					found = strings.Contains(line, fmt.Sprintf("[豆包/账号%d · fail]", account)) &&
						strings.Contains(line, fmt.Sprintf("进度 %d/%d", i+1, perAccount)) &&
						strings.Contains(line, "1s") && strings.Contains(line, "错误: 模拟下载错误")
					break
				}
			}
			if !found {
				t.Errorf("日志缺少或损坏: %s", marker)
			}
		}
	}
}

func TestTraceWriterStillAttemptsStderrWhenFileFails(t *testing.T) {
	var stderr bytes.Buffer
	w := traceWriter{file: unavailableWriter{}, stderr: &stderr}
	if _, err := w.Write([]byte("diagnostic\n")); err == nil || stderr.String() != "diagnostic\n" {
		t.Fatalf("file error=%v stderr=%q", err, stderr.String())
	}
}

func TestFileWriteFailureIsReportedAndResetsAfterRecovery(t *testing.T) {
	if err := open(t.TempDir(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	defer Close()
	path := Path()
	dir := filepath.Dir(path)
	breakFile := func() {
		t.Helper()
		if err := traceRotate.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(dir); err != nil {
			t.Fatal(err)
		}
	}
	before := len(Log.Entries())
	breakFile()
	Log.Info("第一条")
	Log.Info("第二条")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Log.Info("恢复写入")
	breakFile()
	Log.Info("再次失败")
	notices := 0
	for _, entry := range Log.Entries()[before:] {
		if entry.Level == Error && strings.Contains(entry.Text, "写入日志文件失败") {
			notices++
		}
	}
	if notices != 2 {
		t.Fatalf("每次连续故障应只提示一次，恢复后可再次提示: got %d", notices)
	}
}
