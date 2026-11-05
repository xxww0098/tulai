package agy

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

type commandRecording struct {
	Args  []string
	Input string
}

func TestWindowsBackgroundCommandPreservesUnicodeAndPipes(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "中文 用户 & 目录 [%]!")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "agy helper.exe")
	if err := os.WriteFile(helper, data, 0o700); err != nil {
		t.Fatal(err)
	}
	want := []string{"中文 & 空格", "%PATH% ! [x]", "https://example.invalid/?sig=a%2Fb"}
	args := append([]string{"-test.run=^TestWindowsBackgroundCommandRecorder$", "--"}, want...)
	cmd := exec.Command(helper, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TULAI_AGY_COMMAND_RECORDER=1")
	cmd.Stdin = strings.NewReader("中文管道输入\n")
	attr := &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	cmd.SysProcAttr = attr
	configureCommand(cmd)
	if cmd.SysProcAttr != attr || !attr.HideWindow || attr.CreationFlags&0x08000000 == 0 || attr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Fatal("background command lost its Windows creation flags")
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper failed: %v", err)
	}
	var got commandRecording
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Args, want) || got.Input != "中文管道输入\n" {
		t.Fatalf("arguments or pipes changed: %+v", got)
	}
}

func TestWindowsBackgroundCommandRecorder(t *testing.T) {
	if os.Getenv("TULAI_AGY_COMMAND_RECORDER") != "1" {
		t.Skip("subprocess helper")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	if err := json.NewEncoder(os.Stdout).Encode(commandRecording{Args: os.Args[3:], Input: string(data)}); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
