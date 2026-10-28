package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestDeleteBrowserRemovesProfileAndExtensionPair(t *testing.T) {
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
	dir := browser.ProfileDir(root, browser.KindEdge, local.Name)
	if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Local State"), []byte("profile"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertExtensionAccount("豆包", "profile-a", "扩展甲"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertExtensionAccount("千问", "profile-a", "扩展甲"); err != nil {
		t.Fatal(err)
	}
	other, err := st.AddBrowser("千问", "账号2", "")
	if err != nil {
		t.Fatal(err)
	}
	otherDir := browser.ProfileDir(root, browser.KindEdge, other.Name)
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := &App{
		store: st, manager: browser.NewManager(), dataRoot: root,
		browserBackend: browser.KindEdge, browserSel: 0,
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
		t.Fatal(err)
	}
	rows := st.Browsers()
	if len(rows) != 3 {
		t.Fatalf("stopped browser cannot read the extension id, pair rows stay until a live delete: %+v", rows)
	}

	a.refreshBrowsers()
	for i, row := range a.browserRows {
		if row.ExtAccount == "profile-a" {
			a.browserSel = i
			break
		}
	}
	a.deleteBrowser()
	left := st.Browsers()
	if len(left) != 1 || left[0].ID != other.ID {
		t.Fatalf("deleting one extension row should drop both platforms: %+v", left)
	}
	if _, err := os.Stat(otherDir); err != nil {
		t.Fatal("the other account profile was removed")
	}
}
