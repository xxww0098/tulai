package tasks

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

func TestAgySuccessHarvest(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	home := filepath.Join(root, "acc1")
	addAgyAccount(t, st, "acc1", home, "ok")
	writeAgyJPEG(t, filepath.Join(home, "sample.jpg"))
	out := filepath.Join(root, "out")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := eng.Run(ctx, agyOpts(out, "一只猫", false, nil), nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusSuccess || len(items[0].Results) != 1 {
		t.Fatalf("%+v", items)
	}
	got := items[0].Results[0]
	if !strings.Contains(got, filepath.Join("生图", time.Now().Format("2006-01-02"))) || !strings.HasSuffix(got, ".jpg") {
		t.Fatalf("result %s", got)
	}
}

func TestAgyTaskFinishKicksUsage(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	home := filepath.Join(root, "acc1")
	row := addAgyAccount(t, st, "acc1", home, "ok")
	writeAgyJPEG(t, filepath.Join(home, "sample.jpg"))
	var mu sync.Mutex
	var ids []int64
	eng.SetAgyUsageKick(func(id int64) {
		mu.Lock()
		ids = append(ids, id)
		mu.Unlock()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 1 || ids[0] != row.ID {
		t.Fatalf("kicks %v want %d", ids, row.ID)
	}
}

func TestAgyWashConvertsPNG(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	home := filepath.Join(root, "acc1")
	addAgyAccount(t, st, "acc1", home, "ok")
	writeAgyJPEG(t, filepath.Join(home, "sample.jpg"))
	src := filepath.Join(root, "apple.jpg")
	writeAgyJPEG(t, src)
	out := filepath.Join(root, "out")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	opts := agyOpts(out, "把苹果改成绿色", true, []string{src})
	if err := eng.Run(ctx, opts, nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusSuccess || len(items[0].Results) != 1 {
		t.Fatalf("%+v", items)
	}
	got := items[0].Results[0]
	if !strings.Contains(got, filepath.Join("洗图", time.Now().Format("2006-01-02"))) || filepath.Base(got) != "apple_01.png" {
		t.Fatalf("result %s", got)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatal("洗图结果不是 PNG")
	}
}

func TestAgyQuotaSwitchesAccount(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	first := filepath.Join(root, "acc1")
	second := filepath.Join(root, "acc2")
	addAgyAccount(t, st, "acc1", first, "quota")
	addAgyAccount(t, st, "acc2", second, "ok")
	writeAgyJPEG(t, filepath.Join(second, "sample.jpg"))
	out := filepath.Join(root, "out")
	from := len(logging.Log.Entries())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := eng.Run(ctx, agyOpts(out, "一只猫", false, nil), nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusSuccess {
		t.Fatalf("%+v", items)
	}
	rows := st.Browsers()
	var exhausted, ok bool
	for _, row := range rows {
		if row.Name == "acc1" {
			exhausted = row.Health == store.HealthExhausted && strings.Contains(row.LastError, "额度") && row.IsLoggedIn
		}
		if row.Name == "acc2" && row.Health == store.HealthReady {
			ok = true
		}
	}
	if !exhausted || !ok {
		t.Fatalf("accounts %+v", rows)
	}
	log := logSince(from)
	if !strings.Contains(log, "#1「acc1」额度已用尽，改用「acc2」") {
		t.Fatalf("换号日志: %s", log)
	}
	if strings.Contains(log, "已换到") {
		t.Fatalf("换号不应再写第二句: %s", log)
	}
}

func TestAgyAllQuotaPauses(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	addAgyAccount(t, st, "acc1", filepath.Join(root, "acc1"), "quota")
	addAgyAccount(t, st, "acc2", filepath.Join(root, "acc2"), "quota")
	before := pauseCount()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for pauseCount() <= before && time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
	}
	if pauseCount() <= before {
		cancel()
		<-done
		t.Fatal("没有写出全部额度不足")
	}
	if eng.View() != nil {
		for _, item := range eng.View().Snapshot() {
			if item.Status == StatusFailed {
				t.Fatalf("不应标失败: %+v", item)
			}
		}
	}
	cancel()
	<-done
	now := time.Now()
	y, m, d := now.Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, now.Location()).Unix()
	for _, row := range st.Browsers() {
		if row.Platform != agy.Platform {
			continue
		}
		if row.Health != store.HealthExhausted || !strings.Contains(row.LastError, "额度") || row.CoolUntil != midnight {
			t.Fatalf("account %+v want cool %d", row, midnight)
		}
	}
}

func TestAgyUnauthenticatedNeedsLogin(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	home := filepath.Join(root, "acc1")
	addAgyAccount(t, st, "acc1", home, "unauth")
	from := len(logging.Log.Entries())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress)
	}()
	text := waitHeld(t, eng, from, "没有可用账号：Antigravity需要重新登录")
	if strings.Contains(text, "没有正面证据") || strings.Contains(text, "失败") || strings.Contains(text, "等待账号额度恢复") || strings.Contains(text, "已换到") {
		cancel()
		<-done
		t.Fatalf("log = %s", text)
	}
	if agy.LoggedIn(home) {
		cancel()
		<-done
		t.Fatal("凭证应被删掉")
	}
	for _, row := range st.Browsers() {
		if row.Name == "acc1" && (row.IsLoggedIn || !strings.Contains(row.LastError, "需登录")) {
			cancel()
			<-done
			t.Fatalf("account %+v", row)
		}
	}
	cancel()
	waitRun(t, done)
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusSkipped || items[0].Error != "已停止" {
		t.Fatalf("停下后应是已停止: %+v", items)
	}
}

func TestAgyMissingTokenNeedsLogin(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	home := filepath.Join(root, "acc1")
	row := addAgyAccount(t, st, "acc1", home, "ok")
	agy.ForgetToken(home)
	st.UpdateBrowser(row.ID, false, true)
	from := len(logging.Log.Entries())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress)
	}()
	text := waitHeld(t, eng, from, "没有可用账号：Antigravity需要重新登录")
	if strings.Contains(text, "没有正面证据") || strings.Contains(text, "失败") || strings.Contains(text, "已换到") {
		cancel()
		<-done
		t.Fatalf("log = %s", text)
	}
	if agy.LoggedIn(home) {
		cancel()
		<-done
		t.Fatal("凭证应被删掉")
	}
	for _, got := range st.Browsers() {
		if got.ID == row.ID && (got.IsLoggedIn || !strings.Contains(got.LastError, "需登录")) {
			cancel()
			<-done
			t.Fatalf("account %+v", got)
		}
	}
	cancel()
	waitRun(t, done)
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusSkipped || items[0].Error != "已停止" {
		t.Fatalf("停下后应是已停止: %+v", items)
	}
}

func TestAgyLoginSwitchesAccount(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	first := filepath.Join(root, "acc1")
	second := filepath.Join(root, "acc2")
	addAgyAccount(t, st, "acc1", first, "unauth")
	addAgyAccount(t, st, "acc2", second, "ok")
	writeAgyJPEG(t, filepath.Join(second, "sample.jpg"))
	from := len(logging.Log.Entries())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusSuccess || len(items[0].Results) != 1 {
		t.Fatalf("%+v", items)
	}
	log := logSince(from)
	if !strings.Contains(log, "#1「acc1」需登录，改用「acc2」") {
		t.Fatalf("换号日志: %s", log)
	}
	if strings.Contains(log, "已换到") || strings.Contains(log, "没有正面证据") || strings.Contains(log, "失败") {
		t.Fatalf("log = %s", log)
	}
	for _, row := range st.Browsers() {
		if row.Name == "acc1" && (row.IsLoggedIn || !strings.Contains(row.LastError, "需登录")) {
			t.Fatalf("account %+v", row)
		}
	}
}

func TestAgyQuotaThenLoginHolds(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	addAgyAccount(t, st, "acc1", filepath.Join(root, "acc1"), "quota")
	addAgyAccount(t, st, "acc2", filepath.Join(root, "acc2"), "quota")
	addAgyAccount(t, st, "acc3", filepath.Join(root, "acc3"), "unauth")
	from := len(logging.Log.Entries())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress)
	}()
	text := waitHeld(t, eng, from, "没有可用账号：Antigravity额度不足，Antigravity需要重新登录")
	for _, line := range []string{"#1「acc1」额度已用尽，改用「acc2」", "#1「acc2」额度已用尽，改用「acc3」"} {
		if !strings.Contains(text, line) {
			cancel()
			<-done
			t.Fatalf("缺少 %s\n%s", line, text)
		}
	}
	if strings.Contains(text, "已换到") || strings.Contains(text, "没有正面证据") || strings.Contains(text, "失败") || strings.Contains(text, "全部账号额度不足") {
		cancel()
		<-done
		t.Fatalf("log = %s", text)
	}
	cancel()
	waitRun(t, done)
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusSkipped || items[0].Error != "已停止" {
		t.Fatalf("停下后应是已停止: %+v", items)
	}
}

func nopProgress(Progress) {}

func newAgyEngine(t *testing.T) (*Engine, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "database", "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewEngine(st, nil, nil, root), st, root
}

func addAgyAccount(t *testing.T, st *store.Store, name, home, mode string) store.Browser {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	row, err := st.AddBrowser(agy.Platform, name, home)
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	if err := os.MkdirAll(filepath.Dir(agy.TokenPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agy.TokenPath(home), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "fixture.txt"), []byte(mode), 0o644); err != nil {
		t.Fatal(err)
	}
	row.IsLoggedIn = true
	row.UserDataDir = home
	return row
}

func agyOpts(out, prompt string, wash bool, images []string) Options {
	opts := Options{
		Platforms:     []string{agy.Platform},
		Prompts:       []string{prompt},
		OutputPath:    out,
		AgyBin:        agyBinPath(),
		AgyModel:      agy.DefaultModel,
		AgyTimeoutSec: 60,
		SendWait:      0,
		RetryCount:    1,
		Wash:          wash,
		Images:        images,
	}
	return opts
}

func agyBinPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	path := filepath.Join(filepath.Dir(file), "..", "agy", "testdata", "agy.sh")
	_ = os.Chmod(path, 0o755)
	return path
}

func writeAgyJPEG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 13), uint8(y * 7), uint8((x + y) * 3), 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
}

func logSince(n int) string {
	entries := logging.Log.Entries()
	if n < 0 || n > len(entries) {
		n = 0
	}
	var b strings.Builder
	for _, entry := range entries[n:] {
		b.WriteString(entry.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func waitHeld(t *testing.T, eng *Engine, from int, want string) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		text = logSince(from)
		view := eng.View()
		if view == nil || !strings.Contains(text, want) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		items := view.Snapshot()
		if len(items) == 1 && items[0].Status == StatusPending {
			return text
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("没有停在 %q\n%s", want, text)
	return text
}

func waitRun(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("停止后没有结束")
	}
}

func pauseCount() int {
	n := 0
	for _, entry := range logging.Log.Entries() {
		if strings.Contains(entry.Text, "全部账号额度不足，明天 00:00 自动恢复") {
			n++
		}
	}
	return n
}

func TestAgyGenPassesReferenceImage(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	home := filepath.Join(root, "acc1")
	addAgyAccount(t, st, "acc1", home, "ok")
	writeAgyJPEG(t, filepath.Join(home, "sample.jpg"))
	src := filepath.Join(root, "ref.jpg")
	writeAgyJPEG(t, src)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "改成赛博朋克", false, []string{src}), nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(filepath.Join(home, "last-prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "edit the reference image") {
		t.Fatalf("生图带参考图时没有交给 agy: %s", prompt)
	}
	if _, err := os.Stat(filepath.Join(home, "staged-ref")); err != nil {
		t.Fatal("参考图没有放进工作目录")
	}
}
