package logging

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestRedactHidesCredentials(t *testing.T) {
	in := "Authorization: Bearer abcdefghijklmnop cookie=sessionsecret token=\"abcd1234\" ok"
	got := Redact(in)
	if strings.Contains(got, "abcdefghijklmnop") || strings.Contains(got, "sessionsecret") || strings.Contains(got, "abcd1234") {
		t.Fatalf("secret leaked: %s", got)
	}
	if !strings.Contains(got, "[omitted]") || !strings.Contains(got, "ok") {
		t.Fatalf("redact changed the surrounding text: %s", got)
	}
	header := Redact("Cookie: session=secretone; auth=secrettwo")
	if strings.Contains(header, "secretone") || strings.Contains(header, "secrettwo") {
		t.Fatalf("cookie header left a later pair: %s", header)
	}
	if header != "Cookie: [omitted]" {
		t.Fatalf("cookie header = %s", header)
	}
}

func TestPanelFilterCountsAndCopy(t *testing.T) {
	rows := []Entry{
		{Seq: 1, Time: time.Date(2026, 1, 2, 3, 4, 5, 6e6, time.Local), Level: Debug, Source: "应用", Text: "probe"},
		{Seq: 2, Time: time.Date(2026, 1, 2, 3, 4, 6, 0, time.Local), Level: Info, Source: "豆包/工作", Text: "已提交", Elapsed: 1500 * time.Millisecond},
		{Seq: 3, Time: time.Date(2026, 1, 2, 3, 4, 7, 0, time.Local), Level: Success, Source: "豆包/工作", Text: "已保存"},
		{Seq: 4, Time: time.Date(2026, 1, 2, 3, 4, 8, 0, time.Local), Level: Warning, Source: "千问", Text: "重试"},
		{Seq: 5, Time: time.Date(2026, 1, 2, 3, 4, 9, 0, time.Local), Level: Error, Source: "豆包/工作", Text: "失败", Detail: "task_id=4"},
	}
	counts := CountLevels(rows)
	if counts.All != 5 || counts.Debug != 1 || counts.Info != 2 || counts.Warn != 1 || counts.Error != 1 {
		t.Fatalf("counts %+v", counts)
	}
	if got := Sources(rows); len(got) != 3 || got[0] != "应用" || got[2] != "千问" {
		t.Fatalf("sources %v", got)
	}
	view := View{Level: Info, Query: "保存", Source: "豆包/工作"}
	got := Filter(rows, view)
	if len(got) != 1 || got[0].Seq != 3 {
		t.Fatalf("filter %+v", got)
	}
	line := CopyLine(rows[1])
	if !strings.Contains(line, "INF") || !strings.Contains(line, "[豆包/工作]") || !strings.Contains(line, "1.5s") {
		t.Fatalf("copy line %s", line)
	}
	if !strings.Contains(ExportText(got), "OK") {
		t.Fatalf("export %s", ExportText(got))
	}
}

func TestVisibleSpanAndFrameKick(t *testing.T) {
	heights := make([]float32, 100)
	for i := range heights {
		heights[i] = 22
	}
	win := VisibleWindow(heights, 80, 44)
	if win.Start != 2 || win.Top != 44 {
		t.Fatalf("uniform window %+v", win)
	}
	if empty := VisibleWindow(nil, 80, 0); empty.Start != 0 || empty.End != 0 {
		t.Fatalf("empty window %+v", empty)
	}
	mixed := []float32{40, 22, 22}
	scrolled := VisibleWindow(mixed, 22, 40)
	if scrolled.Start != 1 || scrolled.Top != 40 {
		t.Fatalf("expanded first row must push the next row to 40px, got %+v", scrolled)
	}
	var kick FrameKick
	n := 0
	for range 5 {
		kick.Kick(func() { n++ })
	}
	if n != 1 || kick.Wakes() != 1 {
		t.Fatalf("wakes %d %d", n, kick.Wakes())
	}
	kick.Done()
	kick.Kick(func() { n++ })
	if n != 2 {
		t.Fatalf("second wake %d", n)
	}
}

func TestSubscribeCloseReopenAndClearKeepsFile(t *testing.T) {
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Close() })
	logger := New(4)
	var got []string
	cancel := logger.Subscribe(func(entry Entry) { got = append(got, entry.Text) })
	logger.Info("第一条 token=supersecret")
	cancel()
	logger.Info("关闭后")
	if len(got) != 1 || strings.Contains(got[0], "supersecret") || !strings.Contains(got[0], "[omitted]") {
		t.Fatalf("while subscribed %+v", got)
	}
	again := logger.Subscribe(func(entry Entry) { got = append(got, entry.Text) })
	defer again()
	logger.Info("重新打开")
	if len(got) != 2 || got[1] != "重新打开" {
		t.Fatalf("after reopen %+v", got)
	}
	logger.Clear()
	if len(logger.Entries()) != 0 {
		t.Fatal("clear left panel rows")
	}
	Log.Info("文件还在 cookie=keepthisvalue")
	path := Path()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "文件还在") {
		t.Fatalf("log file was cleared: %s", data)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestRingDropsOldest(t *testing.T) {
	logger := New(2)
	logger.Info("a")
	logger.Info("b")
	logger.Info("c")
	rows := logger.Entries()
	if len(rows) != 2 || rows[0].Text != "b" || rows[1].Text != "c" {
		t.Fatalf("ring %+v", rows)
	}
}
