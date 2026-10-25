package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductVersionMatchesConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Version != ProductVersion {
		t.Fatalf("mygo.json version = %q, ProductVersion = %q", cfg.Version, ProductVersion)
	}
}

func TestExtensionHelloVersionMatchesManifest(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "extension", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version == "" {
		t.Fatal("extension manifest has no version")
	}
	src, err := os.ReadFile(filepath.Join(root, "extension", "protocol.js"))
	if err != nil {
		t.Fatal(err)
	}
	needle := "version: '" + manifest.Version + "'"
	if !strings.Contains(string(src), needle) {
		t.Fatalf("extension/protocol.js hello payload has no %s", needle)
	}
}

func TestPackagedDataDir(t *testing.T) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		goos       string
		executable string
		packaged   bool
	}{
		{"darwin", "/Applications/xbs.app/Contents/MacOS/xbs", true},
		{"darwin", "/Applications/XBS.APP/Contents/MacOS/xbs", true},
		{"darwin", "/tmp/go-build123/b001/exe/xbs", false},
		{"darwin", "/tmp/xbs", false},
		{"darwin", "/tmp/xbs/Contents/MacOS/xbs", false},
		{"darwin", "/Applications/xbs.app/Contents/Resources/xbs", false},
		{"windows", `C:\Program Files\xbs\xbs.exe`, true},
		{"windows", `D:\Portable\xbs.exe`, true},
		{"windows", `C:\Users\user\Temp\go-build\exe\xbs.exe`, true},
		{"linux", "/usr/local/bin/xbs", false},
	} {
		t.Run(tc.goos+"/"+tc.executable, func(t *testing.T) {
			got, err := packagedDataDir(tc.goos, tc.executable)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.packaged {
				want = filepath.Join(configDir, "xbs")
			}
			if got != want {
				t.Fatalf("data directory = %q, want %q", got, want)
			}
		})
	}
	if after, err := os.Getwd(); err != nil || after != wd {
		t.Fatalf("resolving a data directory changed the working directory: %q, %v", after, err)
	}
}
