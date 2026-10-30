package agy

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectAtLoggedInAndMissing(t *testing.T) {
	home := t.TempDir()
	miss := DetectAt(home)
	if miss.CanImport() || miss.HasToken || miss.Hint != MsgNotFound {
		t.Fatalf("missing %+v", miss)
	}
	writeImportSource(t, home, []byte("token-bytes"))
	got := DetectAt(home)
	if !got.HasToken || !got.CanImport() || got.Email != "later@gmail.com" || got.Hint != "已检测到 later@gmail.com" {
		t.Fatalf("detected %+v", got)
	}
	if got.Keychain {
		t.Fatal("有凭证文件时不应走钥匙串")
	}
}

func TestNormalizeHomeAcceptsThreeFolders(t *testing.T) {
	home := t.TempDir()
	writeImportSource(t, home, []byte("token-bytes"))
	want, err := absClean(home)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		home,
		filepath.Join(home, ".gemini"),
		filepath.Join(home, ".gemini", "antigravity-cli"),
	}
	for _, path := range paths {
		got, err := NormalizeHome(path)
		if err != nil || got != want {
			t.Fatalf("%s -> %s %v", path, got, err)
		}
	}
	if _, err := NormalizeHome(t.TempDir()); err == nil {
		t.Fatal("无关目录不应通过")
	}
	if err := os.Remove(TokenPath(home)); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeHome(home); err == nil || !strings.Contains(err.Error(), MsgNoToken) {
		t.Fatalf("没有凭证: %v", err)
	}
}

func TestCopyAccountFilesOnlyAllowed(t *testing.T) {
	src := t.TempDir()
	token := []byte("token-bytes-not-for-logs")
	writeImportSource(t, src, token)
	dst := t.TempDir()
	if err := copyAccountFiles(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(TokenPath(dst))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, token) {
		t.Fatal("凭证内容不一致")
	}
	info, err := os.Stat(TokenPath(dst))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("凭证权限 %o", info.Mode().Perm())
	}
	for _, name := range []string{"installation_id", "jetski_state.pbtxt"} {
		if _, err := os.Stat(filepath.Join(cliDir(dst), name)); err != nil {
			t.Fatal(name, err)
		}
	}
	cfg, err := os.ReadFile(filepath.Join(dst, ".gemini", "config", "sub", "a.json"))
	if err != nil || string(cfg) != "cfg" {
		t.Fatalf("config %q %v", cfg, err)
	}
	var banned []string
	err = filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dst, path)
		base := filepath.Base(path)
		switch base {
		case "cli.log", "brain", "conversations", "cache":
			banned = append(banned, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(banned) != 0 {
		t.Fatalf("复制了不该带上的文件 %v", banned)
	}
}

func TestImportRejectsDuplicateAndInsideRoot(t *testing.T) {
	script := agyScript(t)
	src := t.TempDir()
	writeImportSource(t, src, []byte("token-bytes"))
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Import(ctx, ImportRequest{
		SourceHome: src, AccountRoot: root, Bin: script,
		ExistingEmails: []string{"LATER@gmail.com"}, Timeout: 3 * time.Second,
	})
	if err != ErrAlreadyImported {
		t.Fatalf("duplicate %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("重复导入仍创建了目录 %v", namesOf(entries))
	}

	inside := filepath.Join(root, "acc9")
	writeImportSource(t, inside, []byte("token-bytes"))
	_, err = Import(ctx, ImportRequest{
		SourceHome: inside, AccountRoot: root, Bin: script, Timeout: 3 * time.Second,
	})
	if err != ErrAlreadyImported {
		t.Fatalf("inside %v", err)
	}
	entries, err = os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "acc9" {
		t.Fatalf("账号根被改动 %v", namesOf(entries))
	}
}

func TestImportVerifyFailureRemovesNewHome(t *testing.T) {
	script := agyScript(t)
	src := t.TempDir()
	writeImportSource(t, src, []byte("token-bytes"))
	root := t.TempDir()
	t.Setenv("AGY_FIXTURE", "fail")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Import(ctx, ImportRequest{
		SourceHome: src, AccountRoot: root, Bin: script, Model: DefaultModel, Timeout: 3 * time.Second,
	})
	if err == nil || err.Error() != "验证没有通过" {
		t.Fatalf("%v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("验证失败后目录还在 %v", namesOf(entries))
	}
	if LoggedIn(src) {
		info, statErr := os.Stat(TokenPath(src))
		if statErr != nil || info.Mode().Perm() == 0 {
			t.Fatal(statErr)
		}
	} else {
		t.Fatal("失败不应改动来源账号")
	}
}

func TestImportVerifySuccess(t *testing.T) {
	script := agyScript(t)
	src := t.TempDir()
	token := []byte("token-bytes")
	writeImportSource(t, src, token)
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := Import(ctx, ImportRequest{
		SourceHome: src, AccountRoot: root, Bin: script, Model: "gemini-3.8-flash", Timeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "acc1" || res.Email != "later@gmail.com" {
		t.Fatalf("%+v", res)
	}
	got, err := os.ReadFile(TokenPath(res.Home))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, token) {
		t.Fatal("导入后凭证内容不一致")
	}
	info, err := os.Stat(TokenPath(res.Home))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("权限 %o", info.Mode().Perm())
	}
	prompt, err := os.ReadFile(filepath.Join(res.Home, "last-prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(prompt)) != verifyPrompt {
		t.Fatalf("prompt %q", prompt)
	}
	if _, err := os.Stat(filepath.Join(cliDir(res.Home), "cli.log")); err == nil {
		t.Fatal("不应复制日志")
	}
}

func TestKeychainProbeDoesNotRevealSecret(t *testing.T) {
	got := keychainProbeArgs()
	want := []string{"find-generic-password", "-s", "antigravity"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%q", got)
	}
	for _, arg := range got {
		if arg == "-w" || arg == "-g" || strings.HasPrefix(arg, "-w") || strings.HasPrefix(arg, "-g") {
			t.Fatal(arg)
		}
	}
}

func writeImportSource(t *testing.T, home string, token []byte) {
	t.Helper()
	dir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(filepath.Join(dir, "brain", "c1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "config", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		filepath.Join(dir, "antigravity-oauth-token"):             token,
		filepath.Join(dir, "installation_id"):                     []byte("install-1"),
		filepath.Join(dir, "jetski_state.pbtxt"):                  []byte("state"),
		filepath.Join(dir, "cli.log"):                             []byte("noise\nOAuth: authenticated successfully as old@gmail.com\nOAuth: authenticated successfully as later@gmail.com\n"),
		filepath.Join(dir, "brain", "c1", "x.jpg"):                []byte("img"),
		filepath.Join(dir, "conversations", "c.json"):             []byte("conv"),
		filepath.Join(dir, "cache", "c.bin"):                      []byte("cache"),
		filepath.Join(home, ".gemini", "config", "sub", "a.json"): []byte("cfg"),
	}
	for path, body := range files {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func namesOf(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, entry := range entries {
		out[i] = entry.Name()
	}
	return out
}
