package browser

import (
	"slices"
	"testing"

	"github.com/xxww0098/tulai/internal/store"
)

func TestManagedEnginesPreserveLegacyDefaults(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if DefaultBackend(goos) != KindCamoufox {
			t.Fatal(goos)
		}
		if NormalizeStoredBackend(goos, KindChromeCDP) != KindChromeCDP || ChoiceLabel(KindChromeCDP) != KindChromeCDP {
			t.Fatal("explicit managed Chrome choice was lost")
		}
		for _, old := range []string{"", "Chrome", "Edge", "Builtin", "chromium", "ego lite", "Camoufox"} {
			if NormalizeStoredBackend(goos, old) != KindCamoufox || CanonicalBackend(old) != KindCamoufox || ChoiceLabel(old) != KindCamoufox {
				t.Fatal(old)
			}
		}
	}
	for _, pair := range [][2]string{{"windows", "amd64"}, {"darwin", "amd64"}, {"darwin", "arm64"}} {
		if !CamoufoxSupported(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
	if CamoufoxSupported("windows", "arm64") {
		t.Fatal("upstream does not ship Windows ARM64")
	}
	for _, os := range []string{"windows", "darwin", "linux"} {
		if !RuntimeSupported(os, "amd64", KindChromeCDP) || RuntimeSupported(os, "arm64", KindChromeCDP) != (os == "darwin") {
			t.Fatalf("unexpected Chrome for Testing platform support on %s", os)
		}
	}
	if _, ok := InstalledCamoufox(t.TempDir()); ok {
		t.Fatal("empty directory looks installed")
	}
}

func TestAccountEnginesUsesStoredAccountsBeforeNewAccountDefault(t *testing.T) {
	rows := []store.Browser{
		{Platform: "豆包", Engine: KindChromeCDP},
		{Platform: "豆包", Disabled: true},
		{Platform: "豆包", ExtAccount: "legacy"},
		{Platform: "千问"},
	}
	if got := AccountEngines(rows, "豆包", KindCamoufox); !slices.Equal(got, []string{KindChromeCDP}) {
		t.Fatal(got)
	}
	rows = append(rows, store.Browser{Platform: "豆包", IsLoggedIn: true})
	if got := AccountEngines(rows, "豆包", KindCamoufox); !slices.Equal(got, []string{KindChromeCDP, KindCamoufox}) {
		t.Fatal(got)
	}
	if got := AccountEngines(rows, "ChatGPT", KindChromeCDP); !slices.Equal(got, []string{KindChromeCDP}) {
		t.Fatal(got)
	}
}
