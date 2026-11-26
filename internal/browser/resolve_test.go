package browser

import (
	"slices"
	"testing"

	"github.com/xxww0098/tulai/internal/store"
)

func TestManagedEngineDefaultsToFingerprint(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if DefaultBackend(goos) != KindFingerprint {
			t.Fatal(goos)
		}
		for _, value := range []string{"", "unknown", "other", "Fingerprint"} {
			if NormalizeStoredBackend(goos, value) != KindFingerprint || CanonicalBackend(value) != KindFingerprint || ChoiceLabel(value) != KindFingerprint {
				t.Fatal(value)
			}
		}
		wantAMD64 := goos == "windows" || goos == "linux"
		wantARM64 := goos == "darwin"
		if RuntimeSupported(goos, "amd64", KindFingerprint) != wantAMD64 || RuntimeSupported(goos, "arm64", KindFingerprint) != wantARM64 {
			t.Fatalf("unexpected Fingerprint platform support on %s", goos)
		}
	}
	if _, ok := InstalledRuntime(t.TempDir(), KindFingerprint); ok {
		t.Fatal("empty directory looks installed for Fingerprint")
	}
	if status, exe := FormatDetected(nil); status != "Fingerprint 尚未安装" || exe != "" {
		t.Fatalf("FormatDetected(nil) = %q, %q", status, exe)
	}
}

func TestAccountEnginesAlwaysReturnsFingerprint(t *testing.T) {
	rows := []store.Browser{
		{Platform: "豆包", Engine: KindFingerprint},
		{Platform: "豆包", Disabled: true},
		{Platform: "千问"},
	}
	if got := AccountEngines(rows, "豆包", KindFingerprint); !slices.Equal(got, []string{KindFingerprint}) {
		t.Fatal(got)
	}
	if got := AccountEngines(nil, "千问", ""); !slices.Equal(got, []string{KindFingerprint}) {
		t.Fatal(got)
	}
}
