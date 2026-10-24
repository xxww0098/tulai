package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
