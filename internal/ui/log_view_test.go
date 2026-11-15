package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
)

func TestFloatingLogFiltersAndClearsThePanel(t *testing.T) {
	previous := logging.Log
	logging.Log = logging.New(32)
	t.Cleanup(func() { logging.Log = previous })
	logging.Log.Info("任务开始 token=panel-secret")
	logging.Emit(logging.Info, logging.Line{
		Platform: "豆包", Account: "工作", Stage: "submit", Msg: "已提交", Elapsed: 1500 * time.Millisecond,
	})
	logging.Log.Success("图片已保存")
	logging.Log.Warning("等待重试")
	logging.Log.Errorf("启动失败")
	logging.Log.Debugf("调试内容")

	a := &App{logOpen: true}
	tt := ui.NewTester(func(c *ui.Context) {
		a.look = applyWorkbenchTheme(c)
		a.logFloat(c)
	}, 1100, 720)
	tt.Advance(80 * time.Millisecond)

	for _, text := range []string{"运行日志", "实时接收", "暂停跟随", "导出", "复制", "清空", "关闭", "全部 6", "调试 1", "信息 3", "豆包/工作", "1.5s"} {
		if !tt.HasText(text) {
			t.Fatalf("missing %q in %v", text, tt.Texts())
		}
	}
	if tt.HasText("panel-secret") {
		t.Fatal("panel showed a token")
	}
	if dir := os.Getenv("TULAI_LOG_SHOT"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "log-panel.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !tt.HasText("[omitted]") || !tt.HasText("调试内容") || !tt.HasText("DBG") {
		t.Fatalf("expected redacted text and debug row, got %v", tt.Texts())
	}
	if err := tt.Click("调试 1"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if tt.HasText("任务开始") || !tt.HasText("调试内容") {
		t.Fatalf("debug filter: %v", tt.Texts())
	}
	if err := tt.Click("全部 6"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tt.Clipboard(), "panel-secret") || !strings.Contains(tt.Clipboard(), "[omitted]") {
		t.Fatalf("clipboard %q", tt.Clipboard())
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if len(logging.Log.Entries()) != 0 || tt.HasText("调试内容") {
		t.Fatalf("clear left rows %d %v", len(logging.Log.Entries()), tt.Texts())
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(40 * time.Millisecond)
	if _, ok := tt.Find("运行日志区"); ok {
		t.Fatal("close left the panel up")
	}
	logging.Log.Info("重新打开")
	a.logOpen = true
	tt.Frame()
	if !tt.HasText("重新打开") {
		t.Fatalf("reopen missed the new line: %v", tt.Texts())
	}
}
