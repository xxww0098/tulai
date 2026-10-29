package logging

import (
	"os"
	"strings"
	"testing"
)

func TestVisibleTailSkipsDebugFlood(t *testing.T) {
	entries := []Entry{
		{Level: Info, Text: "任务开始"},
		{Level: Info, Text: "#1 已提交"},
	}
	for i := 0; i < 8; i++ {
		entries = append(entries, Entry{Level: Debug, Text: "检测 images=0"})
	}
	entries = append(entries, Entry{Level: Info, Text: "#2 发送未确认"})
	got := VisibleTail(entries, 6)
	if len(got) != 3 || got[0].Text != "任务开始" || got[1].Text != "#1 已提交" || got[2].Text != "#2 发送未确认" {
		t.Fatalf("调试行不应该把运行日志冲空: %+v", got)
	}
}

func TestBindPutsPlatformOnEveryLine(t *testing.T) {
	if err := Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	stop := Bind("千问", "千问1", 9338, 1)
	Log.Warning("#1 第 1 次尝试失败: 出图超时，没有可保存的图片")
	stop()
	Log.Info("启动完成")
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "platform=千问") || !strings.Contains(text, "account=千问1") || !strings.Contains(text, "port=9338") || !strings.Contains(text, "task_id=1") {
		t.Fatalf("失败日志没有账号: %s", text)
	}
	if strings.Count(text, "account=千问1") != 1 {
		t.Fatalf("绑定结束后的日志不该再带账号: %s", text)
	}
}
