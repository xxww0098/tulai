package cdp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestEgoWindowsPrefersExecutable(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ego-browser.exe", "ego-browser.cmd"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("PATHEXT", ".CMD;.EXE")
	path, err := egoExecutable()
	if err != nil || filepath.Ext(path) != ".exe" {
		t.Fatalf("expected native exe, got %q, %v", path, err)
	}
}

func TestEgoWindowsBatchLauncherPreservesArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "中文 路径 & ! %XBS_EGO_SENTINEL%")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "ego-browser.cmd")
	if err := os.WriteFile(launcher, []byte("@echo off\r\n@\"%XBS_EGO_TEST_BINARY%\" -test.run=TestEgoWindowsArgRecorder -- %*\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "args.json")
	t.Setenv("XBS_EGO_SENTINEL", "must-not-expand")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entry := egoImportEntry(filepath.Join(dir, "bridge.mjs"))
	cmd, err := egoPlatformCommand(ctx, launcher, entry)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(cmd.Env, "XBS_EGO_TEST_BINARY="+binary, "XBS_EGO_TEST_RECORDER=1", "XBS_EGO_TEST_OUTPUT="+output)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("batch launcher failed: %v: %s", err, data)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil || !reflect.DeepEqual(args, []string{"nodejs", "-e", entry}) {
		t.Fatalf("arguments changed: %q, %v", data, err)
	}
}

func TestEgoWindowsArgRecorder(t *testing.T) {
	if os.Getenv("XBS_EGO_TEST_RECORDER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			data, err := json.Marshal(os.Args[i+1:])
			if err != nil || os.WriteFile(os.Getenv("XBS_EGO_TEST_OUTPUT"), data, 0o600) != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(1)
}
