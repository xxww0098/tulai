package fingerprint

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/realbrowser/browserkernel"
)

func TestManagedFingerprintAppliesEditedPreferences(t *testing.T) {
	dir := os.Getenv("TULAI_FINGERPRINT_TEST_DATA")
	if dir == "" {
		t.Skip("requires a dedicated test runtime")
	}
	if _, ok := InstalledFingerprint(dir); !ok {
		t.Fatal("incomplete test runtime")
	}
	profile := t.TempDir()
	settings := browserkernel.FingerprintSpec{Lang: "en-GB", Timezone: "Europe/London", WindowWidth: 640, WindowHeight: 480, ColorScheme: browserkernel.ColorSchemeDark}
	if err := browserkernel.WriteProfile(profile, settings); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEBUG", "pw:browser")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var log bytes.Buffer
	p, err := Start(ctx, dir, Options{Profile: profile, Headless: true, DirectNetwork: true}, &log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	var display struct {
		Language, Timezone string
		Dark               bool
	}
	err = Run(PageContext(ctx, p, p.Page), Evaluate(`({language: navigator.language, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, dark: matchMedia('(prefers-color-scheme: dark)').matches})`, &display))
	if err != nil || display.Language != settings.Lang || display.Timezone != settings.Timezone || !display.Dark {
		t.Errorf("page preferences = %+v, error = %v", display, err)
	}
	p.Close()
	args := log.String()
	for _, arg := range []string{"--user-data-dir=" + profile, "--timezone=Europe/London", "--lang=en-GB", "--window-size=640,480"} {
		if !strings.Contains(args, arg) {
			t.Errorf("launch missing %q", arg)
		}
	}
	if runtime.GOOS == "darwin" && !strings.Contains(args, "--use-mock-keychain") {
		t.Fatal("missing mock keychain in actual Chromium launch")
	}
}
