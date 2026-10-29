package logging

import "testing"

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
