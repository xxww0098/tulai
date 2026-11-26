//go:build windows

package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func temporaryRegistration(t *testing.T) (registry.Key, string) {
	t.Helper()
	path := fmt.Sprintf(`Software\tulai-version-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	key, existed, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		key.Close()
		t.Fatal("temporary registration already exists")
	}
	t.Cleanup(func() {
		key.Close()
		if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
			t.Errorf("delete temporary registration: %v", err)
		}
	})
	return key, path
}

func TestSyncWindowsInstalledVersion(t *testing.T) {
	for _, tc := range []struct {
		name           string
		location       string
		executable     string
		initialVersion string
		newVersion     string
		wantVersion    string
		wantErr        bool
	}{
		{name: "matching install", location: "same", executable: "tulai.exe", initialVersion: "0.1.22", newVersion: "0.1.23", wantVersion: "0.1.23"},
		{name: "case and trailing separator", location: "same-upper", executable: "TULAI.EXE", initialVersion: "0.1.22", newVersion: "0.1.23", wantVersion: "0.1.23"},
		{name: "missing display version", location: "same", executable: "tulai.exe", newVersion: "0.1.23", wantVersion: "0.1.23"},
		{name: "other install", location: "other", executable: "tulai.exe", initialVersion: "0.1.22", newVersion: "0.1.23", wantVersion: "0.1.22"},
		{name: "missing install location", executable: "tulai.exe", initialVersion: "0.1.22", newVersion: "0.1.23", wantVersion: "0.1.22"},
		{name: "relative install location", location: "relative", executable: "tulai.exe", initialVersion: "0.1.22", newVersion: "0.1.23", wantVersion: "0.1.22"},
		{name: "renamed portable", location: "same", executable: "portable.exe", initialVersion: "0.1.22", newVersion: "0.1.23", wantVersion: "0.1.22"},
		{name: "invalid version", location: "same", executable: "tulai.exe", initialVersion: "0.1.22", newVersion: "bad", wantVersion: "0.1.22", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, path := temporaryRegistration(t)
			dir := t.TempDir()
			var location string
			switch tc.location {
			case "same":
				location = dir
			case "same-upper":
				location = strings.ToUpper(dir) + string(os.PathSeparator)
			case "other":
				location = t.TempDir()
			case "relative":
				location = `relative\tulai`
			}
			if location != "" {
				if err := key.SetStringValue("InstallLocation", location); err != nil {
					t.Fatal(err)
				}
			}
			if tc.initialVersion != "" {
				if err := key.SetStringValue("DisplayVersion", tc.initialVersion); err != nil {
					t.Fatal(err)
				}
			}
			err := syncWindowsInstalledVersion(registry.CURRENT_USER, path, filepath.Join(dir, tc.executable), tc.newVersion)
			if (err != nil) != tc.wantErr {
				t.Fatalf("sync error = %v, want error %v", err, tc.wantErr)
			}
			got, _, err := key.GetStringValue("DisplayVersion")
			if err != nil || got != tc.wantVersion {
				t.Fatalf("DisplayVersion = %q, %v; want %q", got, err, tc.wantVersion)
			}
		})
	}
}

func TestSyncWindowsInstalledVersionDoesNotCreateRegistration(t *testing.T) {
	root, _ := temporaryRegistration(t)
	executable := filepath.Join(t.TempDir(), "tulai.exe")
	if err := syncWindowsInstalledVersion(root, "missing", executable, "0.1.23"); err != nil {
		t.Fatal(err)
	}
	key, err := registry.OpenKey(root, "missing", registry.QUERY_VALUE)
	if err == nil {
		key.Close()
		t.Fatal("sync created an installer registration")
	}
	if !errors.Is(err, registry.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestWindowsUninstallKeyMatchesAppIdentifier(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Identifier string `json:"identifier"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	want := `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + config.Identifier
	if config.Identifier == "" || windowsUninstallKey != want {
		t.Fatalf("uninstall key = %q, want %q", windowsUninstallKey, want)
	}
}
