package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestProductVersionMatchesConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version string `json:"version"`
		Updates struct {
			GitHub    string `json:"github"`
			PublicKey string `json:"publicKey"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Version != ProductVersion {
		t.Fatalf("mygo.json version = %q, ProductVersion = %q", cfg.Version, ProductVersion)
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Updates.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || cfg.Updates.GitHub != "xxww0098/tulai" {
		t.Fatalf("invalid MyGo update configuration: %+v", cfg.Updates)
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
		{"darwin", "/Applications/tulai.app/Contents/MacOS/tulai", true},
		{"darwin", "/Applications/TULAI.APP/Contents/MacOS/tulai", true},
		{"darwin", "/tmp/go-build123/b001/exe/tulai", true},
		{"darwin", "/tmp/tulai", true},
		{"darwin", "/tmp/tulai/Contents/MacOS/tulai", true},
		{"darwin", "/Applications/tulai.app/Contents/Resources/tulai", true},
		{"windows", `C:\Program Files\tulai\tulai.exe`, true},
		{"windows", `D:\Portable\tulai.exe`, true},
		{"windows", `C:\Users\user\Temp\go-build\exe\tulai.exe`, true},
		{"linux", "/usr/local/bin/tulai", false},
	} {
		t.Run(tc.goos+"/"+tc.executable, func(t *testing.T) {
			got, err := packagedDataDir(tc.goos, tc.executable)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.packaged {
				want = filepath.Join(configDir, "tulai")
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

func TestDevelopmentAndInstalledShareData(t *testing.T) {
	for _, tc := range []struct {
		goos       string
		devExe     string
		installExe string
	}{
		{"windows", `C:\Users\user\Temp\go-build\exe\tulai.exe`, `C:\Program Files\tulai\tulai.exe`},
		{"darwin", "/tmp/go-build/exe/tulai", "/Applications/tulai.app/Contents/MacOS/tulai"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			configHome := t.TempDir()
			t.Setenv("APPDATA", configHome)
			t.Setenv("XDG_CONFIG_HOME", configHome)
			t.Setenv("HOME", configHome)

			devDir, err := packagedDataDir(tc.goos, tc.devExe)
			if err != nil {
				t.Fatal(err)
			}
			installedDir, err := packagedDataDir(tc.goos, tc.installExe)
			if err != nil {
				t.Fatal(err)
			}
			if devDir == "" || devDir != installedDir || devDir != browser.ConfigDir() {
				t.Fatalf("data roots differ: dev=%q installed=%q browser runtime=%q", devDir, installedDir, browser.ConfigDir())
			}

			installedDB, err := store.Open(filepath.Join(installedDir, "database", "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			profile := browser.ProfileDir(filepath.Join(installedDir, "BrowserData"), browser.KindCamoufox, "account-test")
			account, err := installedDB.AddBrowser("豆包", "共享账号", profile)
			if err != nil {
				installedDB.Close()
				t.Fatal(err)
			}
			if err := installedDB.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(profile, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(profile, "profile-marker"), []byte("saved"), 0o600); err != nil {
				t.Fatal(err)
			}

			devDB, err := store.Open(filepath.Join(devDir, "database", "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer devDB.Close()
			accounts := devDB.Browsers()
			if len(accounts) != 1 || accounts[0].ID != account.ID || accounts[0].UserDataDir != profile {
				t.Fatalf("development build did not see the installed account: %+v", accounts)
			}
			if data, err := os.ReadFile(filepath.Join(accounts[0].UserDataDir, "profile-marker")); err != nil || string(data) != "saved" {
				t.Fatalf("development build did not see the account profile: %q, %v", data, err)
			}
		})
	}
}
