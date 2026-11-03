package ui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func newDownloadApp(t *testing.T) (*App, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &App{store: st, manager: browser.NewManager()}
	a.loadSettings()
	a.tab = pageDownload
	return a, st
}

// 空态要和 Moza 一样：四个页签、右上角两个圆钮、工具条和中间那块空态。
func TestDownloadPageEmptyState(t *testing.T) {
	a, _ := newDownloadApp(t)
	tt := ui.NewTester(a.view, 1280, 800)
	for _, label := range []string{
		"正在下载", "已完成", "回收站", "下载记录", "无任务",
		"全部开始", "全部暂停", "按站点分组", "清理",
		"暂无下载任务", "新建任务",
	} {
		assertVisible(t, tt, label, 1280, 800)
	}
	if !tt.HasText("点击右上角 + 新建任务，或直接拖入链接") {
		t.Fatalf("空态副标题缺失: %v", tt.Texts())
	}
	if !tt.HasText("暂无下载任务") {
		t.Fatal("空态标题缺失")
	}

	// 页签、搜索和新建任务都要能点。
	if err := tt.Click("已完成"); err != nil {
		t.Fatal(err)
	}
	if a.dlFilter != dlFilterDone {
		t.Fatalf("点页签后 filter = %d", a.dlFilter)
	}
	tt.Frame()
	if !tt.HasText("暂无下载任务") {
		t.Fatalf("已完成页签空态缺失: %v", tt.Texts())
	}
	if err := tt.Click("正在下载"); err != nil {
		t.Fatal(err)
	}
	if a.dlFilter != dlFilterOpen {
		t.Fatalf("回到正在下载失败: %d", a.dlFilter)
	}
	if err := tt.Click("搜索任务"); err != nil {
		t.Fatal(err)
	}
	if !a.dlSearchOn {
		t.Fatal("搜索按钮没打开输入框")
	}
	tt.Frame()
	if !tt.HasText("清除并关闭搜索") {
		t.Fatalf("搜索输入框没出现: %v", tt.Texts())
	}
	a.dlSearchOn = false
	tt.Frame()
	if err := tt.Click("新建任务"); err != nil {
		t.Fatal(err)
	}
	if !a.dlComposerOn {
		t.Fatal("空态的新建任务没打开弹窗")
	}
}

// 新建任务弹窗收下链接后，队列真的把文件存下来，列表按页签筛出来。
func TestDownloadComposerDownloads(t *testing.T) {
	payload := []byte("downloaded-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(payload)
	}))
	defer srv.Close()

	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	a.dlComposerOn = true
	a.dlURLs = srv.URL + "/photo.jpg"
	a.outputPath = t.TempDir()

	tt := ui.NewTester(a.view, 1280, 800)
	for _, label := range []string{"新建任务", "链接", "保存目录", "立即下载"} {
		assertVisible(t, tt, label, 1280, 800)
	}
	if !tt.HasText("1 个有效链接") {
		t.Fatalf("弹窗没有写有效链接数: %v", tt.Texts())
	}

	// 测试里没有窗口，提交后当场把队列跑完。
	a.submitDownloadComposer()
	if a.dlComposerOn {
		t.Fatal("提交后弹窗没关")
	}
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone {
		t.Fatalf("任务状态 = %+v", a.dlTasks)
	}
	data, err := os.ReadFile(a.dlTasks[0].Path)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("落盘内容不符: %q, %v", data, err)
	}
	if filepath.Dir(a.dlTasks[0].Path) != a.dlDir {
		t.Fatalf("文件没有存到选择的目录: %s", a.dlTasks[0].Path)
	}

	a.dlFilter = dlFilterDone
	tt.Frame()
	for _, want := range []string{"1 个任务", "已完成", "100%"} {
		if !tt.HasText(want) {
			t.Fatalf("已完成页签缺少 %q: %v", want, tt.Texts())
		}
	}

	a.dlSearch = "不存在的关键词"
	tt.Frame()
	if !tt.HasText("没有匹配的任务") {
		t.Fatalf("搜索无结果时没有空态: %v", tt.Texts())
	}

	a.dlSearch = ""
	a.dlFilter = dlFilterOpen
	tt.Frame()
	if !tt.HasText("暂无下载任务") {
		t.Fatalf("已完成的任务不该留在正在下载: %v", tt.Texts())
	}
}

// 页签筛选、按站点分组和清理都按 Moza 的规则走。
func TestDownloadFiltersAndPrune(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlTasks = []*downloadTask{
		{ID: 1, URL: "https://a.example/1.jpg", Site: "a.example", Name: "1.jpg", Phase: dlActive},
		{ID: 2, URL: "https://b.example/2.jpg", Site: "b.example", Name: "2.jpg", Phase: dlPaused},
		{ID: 3, URL: "https://a.example/3.jpg", Site: "a.example", Name: "3.jpg", Phase: dlDone},
		{ID: 4, URL: "https://c.example/4.jpg", Site: "c.example", Name: "4.jpg", Phase: dlFailed},
		{ID: 5, URL: "https://d.example/5.jpg", Site: "d.example", Name: "5.jpg", Phase: dlCanceled},
	}
	a.dlFilter = dlFilterOpen
	if got := len(a.downloadView()); got != 3 {
		t.Fatalf("正在下载应有 3 条（含暂停和失败），got %d", got)
	}
	a.dlFilter = dlFilterDone
	if got := a.downloadView(); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("已完成 = %+v", got)
	}
	a.dlFilter = dlFilterCanceled
	if got := a.downloadView(); len(got) != 1 || got[0].ID != 5 {
		t.Fatalf("回收站 = %+v", got)
	}
	a.dlFilter = dlFilterAll
	if got := len(a.downloadView()); got != 5 {
		t.Fatalf("下载记录 = %d", got)
	}

	groups := groupBySite(a.downloadView())
	if len(groups) != 4 || groups[0].Label != "a.example" || len(groups[0].Tasks) != 2 {
		t.Fatalf("分组 = %+v", groups)
	}

	if n := a.pruneDownloads(); n != 2 {
		t.Fatalf("清理应移掉已完成和失败两条，got %d", n)
	}
	if len(a.dlTasks) != 3 {
		t.Fatalf("清理后剩 %d 条", len(a.dlTasks))
	}
	if n := a.clearDownloadTrash(); n != 1 {
		t.Fatalf("清空回收站 = %d", n)
	}
	if len(a.dlTasks) != 2 {
		t.Fatalf("清空回收站后剩 %d 条", len(a.dlTasks))
	}
}

// 悬停任务行才露出操作按钮，点移除会把记录删掉。
func TestDownloadRowActionsOnHover(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlFilter = dlFilterDone
	a.dlTasks = []*downloadTask{{
		ID: 1, URL: "https://a.example/1.jpg", Site: "a.example", Name: "1.jpg",
		Dir: t.TempDir(), Phase: dlDone, Written: 10, Total: 10,
	}}
	tt := ui.NewTester(a.view, 1280, 800)
	r, ok := tt.Find("a.example")
	if !ok {
		t.Fatalf("没找到任务行: %v", tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	for _, label := range []string{"重新下载", "打开目录", "移除"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("悬停后缺少操作 %q: %v", label, tt.Texts())
		}
	}
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}
	if len(a.dlTasks) != 0 {
		t.Fatalf("移除后还剩 %d 条", len(a.dlTasks))
	}
}

// 上一次没跑完的任务，重开以后按暂停恢复，不会自己偷偷开始。
func TestDownloadRecordsRestorePaused(t *testing.T) {
	a, st := newDownloadApp(t)
	a.dlTasks = []*downloadTask{
		{ID: 7, URL: "https://a.example/1.jpg", Site: "a.example", Name: "1.jpg", Phase: dlActive, Written: 10, Total: 100},
		{ID: 8, URL: "https://b.example/2.jpg", Site: "b.example", Name: "2.jpg", Phase: dlDone, Written: 50, Total: 50},
	}
	a.saveDownloadRecords()

	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if len(restored.dlTasks) != 2 {
		t.Fatalf("恢复 %d 条", len(restored.dlTasks))
	}
	if restored.dlTasks[0].Phase != dlPaused || restored.dlTasks[1].Phase != dlDone {
		t.Fatalf("状态 = %v / %v", restored.dlTasks[0].Phase, restored.dlTasks[1].Phase)
	}
	if restored.dlNextID != 8 {
		t.Fatalf("下一个编号 = %d", restored.dlNextID)
	}
}

// 表头和任务行的数值列必须右对齐到同一条线。
func TestDownloadColumnsAlign(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlTasks = []*downloadTask{{
		ID: 1, URL: "https://a.example/1.jpg", Site: "a.example", Name: "1.jpg",
		Phase: dlActive, Total: 10 << 20, Written: 42 * (10 << 20) / 100, Speed: 3 << 20,
	}}
	tt := ui.NewTester(a.view, 1280, 800)
	for _, pair := range [][2]string{
		{"大小", "10.0 MB"},
		{"进度", "42%"},
		{"速度", "3.00 MB/s"},
		{"剩余时间", "1秒"},
	} {
		h, ok := tt.Find(pair[0])
		if !ok {
			t.Fatalf("没找到表头 %q: %v", pair[0], tt.Texts())
		}
		v, ok := tt.Find(pair[1])
		if !ok {
			t.Fatalf("没找到 %q: %v", pair[1], tt.Texts())
		}
		hr, vr := h.X+h.W, v.X+v.W
		if d := hr - vr; d < -1 || d > 1 {
			t.Fatalf("%s/%s 右边缘差 %.1fpx（表头 %.1f，值 %.1f）", pair[0], pair[1], d, hr, vr)
		}
	}
}

func TestParseDownloadURLs(t *testing.T) {
	valid, invalid := parseDownloadURLsSplit("https://a.example/1.jpg\n\n  https://b.example/2.png  \nftp://c.example/3.jpg\nhttps://a.example/1.jpg\nnot a url")
	if len(valid) != 2 || valid[0] != "https://a.example/1.jpg" || valid[1] != "https://b.example/2.png" {
		t.Fatalf("valid = %v", valid)
	}
	if invalid != 2 {
		t.Fatalf("invalid = %d", invalid)
	}
}

func TestDownloadFormatting(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		999:        "999 B",
		1536:       "1.50 KB",
		10 * 1024:  "10.0 KB",
		250 * 1024: "250 KB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Fatalf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
	if got := formatETA(65); got != "1分5秒" {
		t.Fatalf("formatETA(65) = %q", got)
	}
	if got := shortenPath("/Users/me/Pictures/输出结果/下载", 12); got != "…/输出结果/下载" {
		t.Fatalf("shortenPath = %q", got)
	}
}
