package download

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
)

// This optional test uses a copied runtime and an entirely new browser profile.
// It never reads or changes the user's browser profile or application config.
func TestYmatouPublicCamoufoxCapture(t *testing.T) {
	runtimeRoot := os.Getenv("TULAI_YMATOU_CAMOUFOX_ROOT")
	if runtimeRoot == "" {
		t.Skip("set TULAI_YMATOU_CAMOUFOX_ROOT to a copied temporary runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	process, err := camoufox.Start(ctx, runtimeRoot, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(process.Context())
	tab := camoufox.PageContext(life, process, process.Page)
	getter := NewHTTPGetter("")
	getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	getter.client.Transport.(*http.Transport).DialContext = safeDownloadDial
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: process.Close, releaseProfile: func() {}, getter: getter, start: ymatouTestURL(t, ymatouTestPage)}
	defer session.Close()
	if err := session.run(ctx, camoufox.Navigate(ymatouTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 9 || session.PageURL() != ymatouTestPage {
		t.Fatalf("public Camoufox capture=%d page=%s err=%v", len(got.Items), session.PageURL(), err)
	}
	if os.Getenv("TULAI_YMATOU_BROWSER_DOWNLOAD") == "1" {
		if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
			t.Fatalf("Camoufox getter public image download: %v", err)
		}
	}
}
