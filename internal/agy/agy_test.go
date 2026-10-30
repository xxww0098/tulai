package agy

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEmailLastMatch(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "OAuth: authenticated successfully as first@gmail.com\nnoise\nOAuth: authenticated successfully as later@gmail.com\n"
	if err := os.WriteFile(LogPath(home), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Email(home); got != "later@gmail.com" {
		t.Fatalf("email = %q", got)
	}
	if LoggedIn(home) {
		t.Fatal("没有凭证文件时不应算已登录")
	}
	if err := os.WriteFile(TokenPath(home), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	if !LoggedIn(home) {
		t.Fatal("凭证文件存在时应算已登录")
	}
	ForgetToken(home)
	if LoggedIn(home) {
		t.Fatal("删除凭证后不应再算已登录")
	}
}

func TestPauseMessage(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 4, 0, 0, time.Local)
	if got := PauseMessage(time.Time{}, now); got != "全部账号额度不足，明天 00:00 自动恢复" {
		t.Fatalf("zero: %s", got)
	}
	midnight := time.Date(2026, 10, 31, 0, 0, 0, 0, time.Local)
	if got := PauseMessage(midnight, now); got != "全部账号额度不足，明天 00:00 自动恢复" {
		t.Fatalf("midnight: %s", got)
	}
	later := time.Date(2026, 10, 30, 18, 30, 0, 0, time.Local)
	if got := PauseMessage(later, now); got != "全部账号额度不足，2026-10-30 18:30 自动恢复" {
		t.Fatalf("later: %s", got)
	}
}

func TestInstruction(t *testing.T) {
	edit := Instruction("make the apple green, keep composition", "/tmp/agyt/in.jpg")
	if !strings.Contains(edit, "edit the reference image /tmp/agyt/in.jpg") || !strings.Contains(edit, "make the apple green, keep composition") {
		t.Fatalf("edit: %s", edit)
	}
	gen := Instruction("一只猫", "")
	if strings.Contains(gen, "reference image") || !strings.Contains(gen, "一只猫") {
		t.Fatalf("gen: %s", gen)
	}
	if strings.Contains(gen, "--dangerously-skip-permissions") || strings.Contains(edit, "--dangerously-skip-permissions") {
		t.Fatal("提示词不应要求跳过权限")
	}
}

func TestCollectAndQuota(t *testing.T) {
	home := t.TempDir()
	started := time.Now().Add(-time.Second)
	dir := BrainDir(home, "conv-1")
	if err := os.MkdirAll(filepath.Join(dir, ".tempmediaStorage"), 0o755); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(dir, ".tempmediaStorage", "junk.jpg")
	if err := os.WriteFile(junk, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(dir, "cat_1.jpg")
	if err := os.WriteFile(img, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := CollectImage(home, "conv-1", img+"\n", started)
	if err != nil || got != img {
		t.Fatalf("path harvest %q %v", got, err)
	}
	got, err = CollectImage(home, "conv-1", "", started)
	if err != nil || got != img {
		t.Fatalf("fallback %q %v", got, err)
	}
	if _, err := CollectImage(home, "missing", "", started); err == nil || err.Error() != "没有找到生成的图片" {
		t.Fatalf("missing: %v", err)
	}

	var stream Stream
	now := time.Now()
	stream.Feed(`{"event":"result","result":{"status":"SUCCESS","response":"quota in the prompt"}}`, false, now)
	if stream.Quota || stream.Status != "SUCCESS" {
		t.Fatalf("success echoed quota: %+v", stream)
	}
	stream = Stream{}
	stream.Feed(`{"event":"error","error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota","retryDelay":"3600s"}}`, false, now)
	if !stream.Quota || stream.Until.IsZero() {
		t.Fatalf("quota: %+v", stream)
	}
	if stream.Until.Sub(now.Add(time.Hour)).Abs() > 2*time.Second {
		t.Fatalf("reset %s", stream.Until)
	}
	stream = Stream{}
	stream.Feed("rate limit on stderr", true, now)
	if !stream.Quota {
		t.Fatal("stderr rate limit")
	}
	if quotaText("image 4291 is fine") {
		t.Fatal("429 不能匹配更长的数字")
	}
	if !quotaText("HTTP 429 too many") {
		t.Fatal("429")
	}
	stream = Stream{}
	stream.Feed("user denied permission for read_file(/workspace/agy-test/poster.png)", false, now)
	if !stream.Denied {
		t.Fatal("读文件权限被拒绝应记下来")
	}
}

func TestNextAccountDir(t *testing.T) {
	root := t.TempDir()
	dir1, name1, err := NextAccountDir(root)
	if err != nil || name1 != "acc1" || dir1 != filepath.Join(root, "acc1") {
		t.Fatalf("%s %s %v", dir1, name1, err)
	}
	_, name2, err := NextAccountDir(root)
	if err != nil || name2 != "acc2" {
		t.Fatalf("%s %v", name2, err)
	}
}

func TestRunFakeAgy(t *testing.T) {
	script := agyScript(t)
	t.Run("success", func(t *testing.T) {
		home := t.TempDir()
		writeToken(t, home)
		sample := filepath.Join(home, "sample.jpg")
		if err := os.WriteFile(sample, []byte("not-really"), 0o644); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		path, err := Run(ctx, Request{Bin: script, Home: home, Model: DefaultModel, Prompt: Instruction("一只猫", "")})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(path, filepath.Join(".gemini", "antigravity-cli", "brain", "conv-test")) {
			t.Fatalf("path %s", path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("quota", func(t *testing.T) {
		home := t.TempDir()
		writeToken(t, home)
		writeFixture(t, home, "quota")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := Run(ctx, Request{Bin: script, Home: home, Prompt: "x"})
		var quota *QuotaError
		if !errorAs(err, &quota) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("unauth", func(t *testing.T) {
		home := t.TempDir()
		writeToken(t, home)
		writeFixture(t, home, "unauth")
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := Run(ctx, Request{Bin: script, Home: home, Prompt: "x"})
		if err != ErrNeedLogin {
			t.Fatalf("%v", err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatalf("未登录应马上结束，用了 %s", time.Since(start))
		}
		if LoggedIn(home) {
			t.Fatal("失效凭证应被删掉")
		}
	})
	t.Run("no token", func(t *testing.T) {
		home := t.TempDir()
		_, err := Run(context.Background(), Request{Bin: script, Home: home, Prompt: "x"})
		if err != ErrNeedLogin {
			t.Fatalf("%v", err)
		}
	})
	t.Run("reference inside cwd", func(t *testing.T) {
		home := t.TempDir()
		writeToken(t, home)
		outside := t.TempDir()
		ref := filepath.Join(outside, "poster.png")
		payload := []byte("reference-bytes-poster")
		if err := os.WriteFile(ref, payload, 0o644); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := Run(ctx, Request{
			Bin:    script,
			Home:   home,
			Model:  DefaultModel,
			Prompt: Instruction("把海报洗一下", ref),
			Ref:    ref,
		})
		if err != nil {
			t.Fatal(err)
		}
		cwdRaw, err := os.ReadFile(filepath.Join(home, "last-cwd.txt"))
		if err != nil {
			t.Fatal(err)
		}
		promptRaw, err := os.ReadFile(filepath.Join(home, "last-prompt.txt"))
		if err != nil {
			t.Fatal(err)
		}
		prompt := string(promptRaw)
		got := instructionRef(prompt)
		cwd := strings.TrimSpace(string(cwdRaw))
		if got == "" || got == ref {
			t.Fatalf("reference path %q prompt %s", got, prompt)
		}
		rel, err := filepath.Rel(cwd, got)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("ref %s is outside cwd %s (%v)", got, cwd, err)
		}
		if strings.Contains(prompt, ref) || strings.Contains(prompt, outside) {
			t.Fatalf("prompt still points outside cwd: %s", prompt)
		}
		staged, err := os.ReadFile(filepath.Join(home, "staged-ref"))
		if err != nil {
			t.Fatal(err)
		}
		if string(staged) != string(payload) {
			t.Fatalf("staged %q", staged)
		}
	})
	t.Run("permission denied", func(t *testing.T) {
		home := t.TempDir()
		writeToken(t, home)
		writeFixture(t, home, "deny")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := Run(ctx, Request{Bin: script, Home: home, Prompt: "x"})
		if err == nil || err.Error() != "agy 拒绝读取参考图" {
			t.Fatalf("%v", err)
		}
	})
}

func instructionRef(prompt string) string {
	const marker = "edit the reference image "
	i := strings.Index(prompt, marker)
	if i < 0 {
		return ""
	}
	rest := prompt[i+len(marker):]
	j := strings.Index(rest, ": ")
	if j <= 0 {
		return ""
	}
	return rest[:j]
}

func agyScript(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("找不到测试文件")
	}
	path := filepath.Join(filepath.Dir(file), "testdata", "agy.sh")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeToken(t *testing.T, home string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(TokenPath(home)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TokenPath(home), []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFixture(t *testing.T, home, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "fixture.txt"), []byte(mode), 0o644); err != nil {
		t.Fatal(err)
	}
}

func errorAs(err error, target **QuotaError) bool {
	if err == nil {
		return false
	}
	q, ok := err.(*QuotaError)
	if !ok {
		return false
	}
	*target = q
	return true
}
