//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testHelperSource = `package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var buildTag = "v1"

func rawCommandLine() string {
	ptr := syscall.GetCommandLine()
	if ptr == nil {
		return ""
	}
	slice := unsafe.Slice(ptr, 32768)
	n := 0
	for n < len(slice) && slice[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(slice[:n])
}

func main() {
	if len(os.Args) >= 4 && os.Args[1] == "hold" {
		readyFile := os.Args[2]
		holdMs, _ := strconv.Atoi(os.Args[3])
		if err := os.WriteFile(readyFile, []byte("holding\n"), 0o644); err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Duration(holdMs) * time.Millisecond)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(3)
	}
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, "/S") {
			cfgPath := filepath.Join(filepath.Dir(exe), "setup.cfg")
			raw, err := os.ReadFile(cfgPath)
			if err != nil {
				os.Exit(4)
			}
			lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
			if len(lines) < 2 {
				os.Exit(5)
			}
			argsOut := strings.TrimSpace(lines[0])
			payloadPath := strings.TrimSpace(lines[1])
			cmdLine := rawCommandLine()
			_ = os.WriteFile(argsOut, []byte(cmdLine), 0o644)
			idx := strings.Index(strings.ToUpper(cmdLine), "/D=")
			if idx < 0 {
				os.Exit(6)
			}
			destDir := strings.TrimSpace(cmdLine[idx+3:])
			if destDir == "" {
				os.Exit(6)
			}
			payload, err := os.ReadFile(payloadPath)
			if err != nil {
				os.Exit(7)
			}
			if err := os.WriteFile(filepath.Join(destDir, "tulai.exe"), payload, 0o755); err != nil {
				os.Exit(8)
			}
			return
		}
	}
	if buildTag == "crash" {
		os.Exit(1)
	}
	_ = os.WriteFile(exe+".started", []byte("ok\n"), 0o644)
	wd, _ := os.Getwd()
	_ = os.WriteFile(exe+".ran", []byte(buildTag+"\n"+wd), 0o644)
}
`

func buildDetachedTestExe(t *testing.T, srcDir, outPath, tag string) {
	t.Helper()
	srcFile := filepath.Join(srcDir, "main.go")
	if _, err := os.Stat(srcFile); os.IsNotExist(err) {
		if err := os.WriteFile(srcFile, []byte(testHelperSource), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ldflags := "-H=windowsgui -X main.buildTag=" + tag
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", outPath, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper %s: %v\n%s", tag, err, out)
	}
}

func startHeldProcess(t *testing.T, exe string, hold time.Duration) *exec.Cmd {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "hold.ready")
	cmd := exec.Command(exe, "hold", ready, fmt.Sprintf("%d", hold.Milliseconds()))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start holder %s: %v", exe, err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return cmd
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("holder %s did not signal readiness", exe)
	return nil
}

func scriptDirFromArgs(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-File" {
			return filepath.Dir(args[i+1])
		}
	}
	return ""
}

func waitForFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
	return nil
}

func waitForRemoved(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	if path == "" {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("path was not cleaned up: %s", path)
}

func TestWindowsDetachedPortableReplaceAndRelaunch(t *testing.T) {
	if testing.Short() {
		// Still fast enough when go build cache is warm; run unconditionally on Windows.
	}
	srcDir := t.TempDir()
	root := t.TempDir()
	destDir := filepath.Join(root, "图来 (Portable) 目录")
	dlDir := filepath.Join(root, "downloads")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	destExe := filepath.Join(destDir, "tulai.exe")
	sourceExe := filepath.Join(dlDir, "tulai-new.exe")
	buildDetachedTestExe(t, srcDir, destExe, "v1")
	buildDetachedTestExe(t, srcDir, sourceExe, "v2")

	holder := startHeldProcess(t, destExe, 500*time.Millisecond)
	waitCh := make(chan error, 1)
	go func() { waitCh <- holder.Wait() }()

	out, err := Install(Options{
		Mode:       ModeWindowsExe,
		Executable: destExe,
		AssetPath:  sourceExe,
		Version:    "0.2.0",
		PID:        holder.Process.Pid,
	})
	if err != nil {
		t.Fatal(err)
	}
	scriptDir := scriptDirFromArgs(out.HelperArgs)
	t.Cleanup(func() {
		if scriptDir != "" {
			_ = os.RemoveAll(scriptDir)
		}
	})
	if err := StartDetached(out.Helper, out.HelperArgs...); err != nil {
		t.Fatal(err)
	}
	<-waitCh

	ran := string(waitForFile(t, destExe+".ran", 20*time.Second))
	lines := strings.Split(strings.ReplaceAll(ran, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "v2" || !strings.EqualFold(filepath.Clean(lines[1]), filepath.Clean(destDir)) {
		t.Fatalf("relaunched binary wrote %q, want v2 in %s", ran, destDir)
	}
	waitForRemoved(t, destExe+".previous", 10*time.Second)
	waitForRemoved(t, scriptDir, 10*time.Second)
}

func TestWindowsDetachedRelaunchOnly(t *testing.T) {
	srcDir := t.TempDir()
	root := t.TempDir()
	holdDir := filepath.Join(root, "old")
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(holdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldExe := filepath.Join(holdDir, "old.exe")
	newExe := filepath.Join(appDir, "tulai.exe")
	buildDetachedTestExe(t, srcDir, oldExe, "v1")
	buildDetachedTestExe(t, srcDir, newExe, "v2")

	holder := startHeldProcess(t, oldExe, 400*time.Millisecond)
	waitCh := make(chan error, 1)
	go func() { waitCh <- holder.Wait() }()

	out, err := PrepareWindowsRelaunch(newExe, holder.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	scriptDir := scriptDirFromArgs(out.HelperArgs)
	t.Cleanup(func() {
		if scriptDir != "" {
			_ = os.RemoveAll(scriptDir)
		}
	})
	if err := StartDetached(out.Helper, out.HelperArgs...); err != nil {
		t.Fatal(err)
	}
	// While holder is alive, newExe must not have started yet.
	time.Sleep(150 * time.Millisecond)
	if _, err := os.Stat(newExe + ".ran"); err == nil {
		t.Fatal("relaunched before the old process exited")
	}
	<-waitCh

	ran := string(waitForFile(t, newExe+".ran", 20*time.Second))
	lines := strings.Split(strings.ReplaceAll(ran, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "v2" || !strings.EqualFold(filepath.Clean(lines[1]), filepath.Clean(appDir)) {
		t.Fatalf("relaunched binary wrote %q, want v2 in %s", ran, appDir)
	}
	waitForRemoved(t, scriptDir, 10*time.Second)
}

func TestWindowsDetachedRollbackOnCrash(t *testing.T) {
	srcDir := t.TempDir()
	root := t.TempDir()
	destDir := filepath.Join(root, "app")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	destExe := filepath.Join(destDir, "tulai.exe")
	crashExe := filepath.Join(root, "crash-new.exe")
	buildDetachedTestExe(t, srcDir, destExe, "v1")
	buildDetachedTestExe(t, srcDir, crashExe, "crash")

	v1Bytes, err := os.ReadFile(destExe)
	if err != nil {
		t.Fatal(err)
	}

	holder := startHeldProcess(t, destExe, 400*time.Millisecond)
	waitCh := make(chan error, 1)
	go func() { waitCh <- holder.Wait() }()

	out, err := Install(Options{
		Mode:       ModeWindowsExe,
		Executable: destExe,
		AssetPath:  crashExe,
		Version:    "0.2.0",
		PID:        holder.Process.Pid,
	})
	if err != nil {
		t.Fatal(err)
	}
	scriptDir := scriptDirFromArgs(out.HelperArgs)
	t.Cleanup(func() {
		if scriptDir != "" {
			_ = os.RemoveAll(scriptDir)
		}
	})
	if err := StartDetached(out.Helper, out.HelperArgs...); err != nil {
		t.Fatal(err)
	}
	<-waitCh

	ran := string(waitForFile(t, destExe+".ran", 25*time.Second))
	lines := strings.Split(strings.ReplaceAll(ran, "\r\n", "\n"), "\n")
	if len(lines) < 1 || lines[0] != "v1" {
		t.Fatalf("expected rolled back v1 to run, got %q", ran)
	}
	gotBytes, err := os.ReadFile(destExe)
	if err != nil || string(gotBytes) != string(v1Bytes) {
		t.Fatalf("destExe was not restored to v1 bytes (err=%v)", err)
	}
	waitForRemoved(t, scriptDir, 10*time.Second)
}

func TestWindowsDetachedSetupInstaller(t *testing.T) {
	srcDir := t.TempDir()
	root := t.TempDir()
	installDir := filepath.Join(root, "Program Files Test", "tulai")
	setupDir := filepath.Join(root, "setup")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(setupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	destExe := filepath.Join(installDir, "tulai.exe")
	payloadExe := filepath.Join(setupDir, "payload-v2.exe")
	installerExe := filepath.Join(setupDir, "tulai.Setup.exe")
	argsOut := filepath.Join(setupDir, "installer.args")

	buildDetachedTestExe(t, srcDir, destExe, "v1")
	buildDetachedTestExe(t, srcDir, payloadExe, "v2")
	buildDetachedTestExe(t, srcDir, installerExe, "installer")
	if err := os.WriteFile(filepath.Join(setupDir, "setup.cfg"), []byte(argsOut+"\n"+payloadExe+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	holder := startHeldProcess(t, destExe, 400*time.Millisecond)
	waitCh := make(chan error, 1)
	go func() { waitCh <- holder.Wait() }()

	out, err := Install(Options{
		Mode:       ModeWindowsSetup,
		Executable: destExe,
		AssetPath:  installerExe,
		Version:    "0.2.0",
		PID:        holder.Process.Pid,
	})
	if err != nil {
		t.Fatal(err)
	}
	scriptDir := scriptDirFromArgs(out.HelperArgs)
	t.Cleanup(func() {
		if scriptDir != "" {
			_ = os.RemoveAll(scriptDir)
		}
	})
	if err := StartDetached(out.Helper, out.HelperArgs...); err != nil {
		t.Fatal(err)
	}
	<-waitCh

	ran := string(waitForFile(t, destExe+".ran", 25*time.Second))
	lines := strings.Split(strings.ReplaceAll(ran, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "v2" || !strings.EqualFold(filepath.Clean(lines[1]), filepath.Clean(installDir)) {
		t.Fatalf("relaunched binary wrote %q, want v2 in %s", ran, installDir)
	}
	recordedArgs := string(waitForFile(t, argsOut, 10*time.Second))
	wantD := "/D=" + installDir
	if !strings.Contains(recordedArgs, "/S") || !strings.Contains(recordedArgs, wantD) {
		t.Fatalf("installer args = %q, want /S and %q", recordedArgs, wantD)
	}
	waitForRemoved(t, scriptDir, 10*time.Second)
}
