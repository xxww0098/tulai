package browser

import "testing"

func TestOnlyCamoufoxIsSelectable(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		if DefaultBackend(goos) != KindCamoufox {
			t.Fatal(goos)
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
	if _, ok := InstalledCamoufox(t.TempDir()); ok {
		t.Fatal("empty directory looks installed")
	}
}
