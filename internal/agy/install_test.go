package agy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBinFollowsOfficialInstallerPerSystem(t *testing.T) {
	home := func() (string, error) { return filepath.Join("home", "me"), nil }
	noHome := func() (string, error) { return "", errors.New("no home") }
	env := func(k string) string {
		if k == "LOCALAPPDATA" {
			return filepath.Join("C:", "Users", "me", "AppData", "Local")
		}
		return ""
	}
	none := func(string) string { return "" }

	if got := defaultBin("darwin", home, none); got != filepath.Join("home", "me", ".local", "bin", "agy") {
		t.Fatalf("darwin = %q", got)
	}
	if got := defaultBin("linux", noHome, none); got != filepath.Join(".local", "bin", "agy") {
		t.Fatalf("linux without home = %q", got)
	}
	if got := defaultBin("windows", home, env); got != filepath.Join("C:", "Users", "me", "AppData", "Local", "agy", "bin", "agy.exe") {
		t.Fatalf("windows with LOCALAPPDATA = %q", got)
	}
	if got := defaultBin("windows", home, none); got != filepath.Join("home", "me", "AppData", "Local", "agy", "bin", "agy.exe") {
		t.Fatalf("windows without LOCALAPPDATA = %q", got)
	}
}

func TestLocatePrefersConfiguredThenDefaultThenPath(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	configured := write("custom-agy")
	fallback := write("default-agy")
	onPath := write("path-agy")
	lookup := func(name string) (string, error) {
		if name != "agy" {
			t.Fatalf("looked up %q", name)
		}
		return onPath, nil
	}
	missing := filepath.Join(dir, "missing")

	if got, ok := locate(configured, fallback, "linux", lookup); !ok || got != configured {
		t.Fatalf("configured = %q %v", got, ok)
	}
	if got, ok := locate("", fallback, "linux", lookup); !ok || got != fallback {
		t.Fatalf("blank setting should use the default location: %q %v", got, ok)
	}
	if got, ok := locate(missing, fallback, "linux", lookup); !ok || got != fallback {
		t.Fatalf("a stale setting should fall back to the default location: %q %v", got, ok)
	}
	if got, ok := locate(missing, missing+"2", "linux", lookup); !ok || got != onPath {
		t.Fatalf("PATH should be the last resort: %q %v", got, ok)
	}
	nothing := func(string) (string, error) { return "", errors.New("not found") }
	got, ok := locate("", missing, "linux", nothing)
	if ok || got != missing {
		t.Fatalf("nothing installed should report the first place checked: %q %v", got, ok)
	}
	if _, ok := locate(dir, missing, "linux", nothing); ok {
		t.Fatal("a directory is not an agy binary")
	}
}

func TestLocateLooksForExeOnWindows(t *testing.T) {
	var asked string
	_, _ = locate("", filepath.Join(t.TempDir(), "none"), "windows", func(name string) (string, error) {
		asked = name
		return "", errors.New("not found")
	})
	if asked != "agy.exe" {
		t.Fatalf("windows lookup name = %q", asked)
	}
}

func TestInstallCommandMatchesOfficialScripts(t *testing.T) {
	if got := InstallCommand("darwin"); got != "curl -fsSL https://antigravity.google/cli/install.sh | bash" {
		t.Fatalf("darwin = %q", got)
	}
	if InstallCommand("linux") != InstallCommand("darwin") {
		t.Fatal("linux and macOS share the bash installer")
	}
	got := InstallCommand("windows")
	if got != "irm https://antigravity.google/cli/install.ps1 | iex" || strings.Contains(got, "curl") {
		t.Fatalf("windows = %q", got)
	}
	if InstallShell("windows") != "PowerShell" || InstallShell("darwin") != "终端" {
		t.Fatal("shell names")
	}
}
