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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	start := time.Now()
	if err := eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("需登录应马上结束，用了 %s", time.Since(start))
	}
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Status != StatusFailed || items[0].Error != "需登录" {
		t.Fatalf("%+v", items)
	}
	if agy.LoggedIn(home) {
		t.Fatal("凭证应被删掉")
	}
	for _, row := range st.Browsers() {
		if row.Name == "acc1" && row.IsLoggedIn {
			t.Fatal("账号仍显示已登录")
		}
	}
}

func TestAgyMissingTokenNeedsLogin(t *testing.T) {
	eng, st, root := newAgyEngine(t)
	home := filepath.Join(root, "acc1")
	row := addAgyAccount(t, st, "acc1", home, "ok")
	agy.ForgetToken(home)
	st.UpdateBrowser(row.ID, false, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := eng.Run(ctx, agyOpts(filepath.Join(root, "out"), "一只猫", false, nil), nil, nopProgress); err != nil {
		t.Fatal(err)
	}
	items := eng.View().Snapshot()
	if len(items) != 1 || items[0].Error != "需登录" {
		t.Fatalf("%+v", items)
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

func pauseCount() int {
	n := 0
	for _, entry := range logging.Log.Entries() {
		if strings.Contains(entry.Text, "全部账号额度不足，明天 00:00 自动恢复") {
			n++
		}
	}
	return n
}
