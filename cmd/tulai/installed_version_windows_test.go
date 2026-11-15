//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// A previous release installs the new files using its own updater. Verify the
// new executable repairs the old version entry on its first command/startup.
func TestStartupRepairsInstalledVersion(t *testing.T) {
	const childEnv = "TULAI_TEST_STARTUP_REGISTRY_ROOT"
	if path := os.Getenv(childEnv); path != "" {
		root, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		override := windows.NewLazySystemDLL("advapi32.dll").NewProc("RegOverridePredefKey")
		rawPredefined := uint32(registry.CURRENT_USER)
		predefined := uintptr(int64(int32(rawPredefined)))
		if code, _, _ := override.Call(predefined, uintptr(root)); code != 0 {
			t.Fatal(syscall.Errno(code))
		}
		defer override.Call(predefined, 0)
		args := os.Args
		defer func() { os.Args = args }()
		os.Args = []string{"tulai.exe", "version"}
		main()
		return
	}

	rootPath := fmt.Sprintf(`Software\tulai-startup-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	root, existed, err := registry.CreateKey(registry.CURRENT_USER, rootPath, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		root.Close()
		t.Fatal("temporary registry root already exists")
	}
	root.Close()
	const uninstallPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\dev.tulai.app`
	t.Cleanup(func() {
		for _, suffix := range []string{
			uninstallPath,
			`Software\Microsoft\Windows\CurrentVersion\Uninstall`,
			`Software\Microsoft\Windows\CurrentVersion`,
			`Software\Microsoft\Windows`,
			`Software\Microsoft`,
			`Software`,
			"",
		} {
			path := rootPath
			if suffix != "" {
				path += `\` + suffix
			}
			if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil && err != registry.ErrNotExist {
				t.Errorf("delete temporary registry key %q: %v", path, err)
			}
		}
	})

	installDir := t.TempDir()
	entry, _, err := registry.CreateKey(registry.CURRENT_USER, rootPath+`\`+uninstallPath, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	if err := entry.SetStringValue("InstallLocation", installDir); err != nil {
		entry.Close()
		t.Fatal(err)
	}
	if err := entry.SetStringValue("DisplayVersion", "0.1.22"); err != nil {
		entry.Close()
		t.Fatal(err)
	}
	entry.Close()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	binary := filepath.Join(installDir, "tulai.exe")
	target, err := os.Create(binary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		target.Close()
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestStartupRepairsInstalledVersion$")
	cmd.Env = append(os.Environ(), childEnv+"="+rootPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("startup helper: %v\n%s", err, out)
	}
	entry, err = registry.OpenKey(registry.CURRENT_USER, rootPath+`\`+uninstallPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	version, _, err := entry.GetStringValue("DisplayVersion")
	if err != nil || version != ProductVersion {
		t.Fatalf("startup DisplayVersion = %q, %v; want %q", version, err, ProductVersion)
	}
}
