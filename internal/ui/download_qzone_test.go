package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/download"
	"github.com/xxww0098/tulai/internal/store"
)

func TestDownloadQzoneCredentialsAreNotPersistedOrQueued(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager()}
	a.loadSettings()
	sensitive := "https://h5.qzone.qq.com/album/123/groupphoto?g_tk=synthetic-secret&keep=1#/view?qzonetoken=synthetic-token"
	a.dlURLs = "  " + sensitive + "  \nhttps://example.test/image.jpg?token=public"
	a.saveSettings()
	saved := st.Get("download_urls")
	if strings.Contains(saved, "synthetic-secret") || strings.Contains(saved, "synthetic-token") || !strings.Contains(saved, "keep=1") || !strings.Contains(saved, "example.test/image.jpg?token=public") {
		t.Fatalf("unexpected stored URLs: %q", saved)
	}
	if !strings.Contains(a.dlURLs, "synthetic-secret") {
		t.Fatal("saving settings unexpectedly changed the input field")
	}
	if added, invalid := a.addDownloadURLs(sensitive); added != 0 || invalid != 1 || len(a.dlTasks) != 0 {
		t.Fatalf("sensitive task queued: added=%d invalid=%d tasks=%d", added, invalid, len(a.dlTasks))
	}
	a.dlURLs = sensitive
	a.submitDownloadComposer()
	if !strings.Contains(a.dlComposerErr, "令牌") || len(a.dlTasks) != 0 {
		t.Fatalf("composer did not reject credentials: %q", a.dlComposerErr)
	}
	a.openDownloadBrowser()
	if !strings.Contains(a.dlBrowserErr, "令牌") {
		t.Fatalf("browser did not reject credentials: %q", a.dlBrowserErr)
	}
	_, _, err = a.enqueueSpreadsheetImages([]download.SpreadsheetImage{{URL: sensitive}}, []bool{true}, false)
	if err == nil || len(a.dlTasks) != 0 {
		t.Fatalf("spreadsheet queued sensitive URL: err=%v tasks=%d", err, len(a.dlTasks))
	}
}
