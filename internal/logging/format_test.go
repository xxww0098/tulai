package logging

import (
	"strings"
	"testing"
	"time"
)

func TestFormatLineKeepsOnlyPresentFields(t *testing.T) {
	at := time.Date(2026, 11, 13, 16, 40, 12, 345_000_000, time.Local)
	got := DefaultFormat.line(Info, Line{Msg: "启动完成"}, at)
	want := "2026-11-13 16:40:12.345  信息  启动完成\n"
	if got != want {
		t.Fatalf("普通日志混进了空字段:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(got, "task_id=0") || strings.Contains(got, "error=\"\"") {
		t.Fatalf("空字段没有省略: %q", got)
	}
}

func TestFormatLineGroupsModuleAndDetails(t *testing.T) {
	at := time.Date(2026, 11, 13, 16, 40, 12, 345_000_000, time.Local)
	line := Line{
		RunID: "20261113T164000", TaskID: 3,
		Platform: "千问", Account: "千问1", Port: 9338, Stage: "submit",
		Elapsed: 1234 * time.Millisecond, Images: 4, Bytes: 10 * 1024,
		Msg: "#3 发送完成", Error: "网络抖动\n已重试",
	}
	got := DefaultFormat.line(Success, line, at)
	for _, part := range []string{
		"2026-11-13 16:40:12.345  成功",
		"[千问/千问1:9338 · submit]",
		"#3 发送完成",
		"(1.234s · 4 张 · 10.0 KiB · 错误: 网络抖动 ⏎ 已重试)",
		"task_id=3 run_id=20261113T164000",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("缺少 %q: %s", part, got)
		}
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("一条日志应只占一个物理行: %q", got)
	}
}

func TestFormatLineMarksDebugAndStageLog(t *testing.T) {
	at := time.Date(2026, 11, 13, 16, 40, 12, 345_000_000, time.Local)
	got := DefaultFormat.line(Debug, Line{Stage: "log", Msg: "检测 images=0"}, at)
	if !strings.Contains(got, "  调试  ") || strings.Contains(got, "[log]") {
		t.Fatalf("调试级别或默认阶段排错了: %s", got)
	}
}

func TestFormatLineShowsTaskProgress(t *testing.T) {
	at := time.Date(2026, 11, 13, 16, 40, 12, 0, time.Local)
	got := DefaultFormat.line(Info, Line{Done: 3, Total: 20, Elapsed: time.Second, Msg: "完成"}, at)
	if !strings.Contains(got, "(1s · 进度 3/20)") {
		t.Fatalf("缺少耗时或进度: %s", got)
	}
}
