package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestDeleteBrowserRemovesOnlyItsOwnProfile(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	local, err := st.AddBrowser("豆包", "账号1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(local.ID, false, true)
	dir := browser.ProfileDir(root, browser.KindFingerprint, fmt.Sprintf("account-%d", local.ID))
	if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Local State"), []byte("profile"), 0o644); err != nil {
		t.Fatal(err)
	}
	other, err := st.AddBrowser("千问", "账号2", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(other.ID, false, true)
	otherDir := browser.ProfileDir(root, browser.KindFingerprint, fmt.Sprintf("account-%d", other.ID))
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := &App{
		store: st, manager: browser.NewManager(), dataRoot: root,
		browserBackend: browser.KindFingerprint, browserSel: 0,
	}
	a.refreshBrowsers()
	for i, row := range a.browserRows {
		if row.ID == local.ID {
			a.browserSel = i
		}
	}
	a.deleteBrowser()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("deleted account profile still exists: %v", err)
	}
	if _, err := os.Stat(otherDir); err != nil {
		t.Fatal("the other account profile was removed")
	}
	left := st.Browsers()
	if len(left) != 1 || left[0].ID != other.ID {
		t.Fatalf("unexpected remaining accounts: %+v", left)
	}
}
