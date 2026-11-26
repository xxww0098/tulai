package download

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadBookmarkFoldersValidAndUnique(t *testing.T) {
	if len(DownloadBookmarkFolders) == 0 {
		t.Fatal("DownloadBookmarkFolders is empty")
	}
	seenNames := make(map[string]string)
	seenURLs := make(map[string]string)
	for _, folder := range DownloadBookmarkFolders {
		if strings.TrimSpace(folder.Title) == "" {
			t.Fatal("bookmark folder has blank title")
		}
		if len(folder.Items) == 0 {
			t.Fatalf("bookmark folder %q has no items", folder.Title)
		}
		for _, item := range folder.Items {
			if strings.TrimSpace(item.Name) == "" {
				t.Fatalf("folder %q contains blank item name", folder.Title)
			}
			u, err := url.Parse(item.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
				t.Fatalf("invalid bookmark URL %q for %q: %v", item.URL, item.Name, err)
			}
			if prev, dup := seenNames[item.Name]; dup {
				t.Fatalf("duplicate bookmark name %q (%s and %s)", item.Name, prev, item.URL)
			}
			norm := normalizeBookmarkURL(item.URL)
			if prev, dup := seenURLs[norm]; dup {
				t.Fatalf("duplicate bookmark URL %q (%s and %s)", item.URL, prev, item.Name)
			}
			seenNames[item.Name] = item.URL
			seenURLs[norm] = item.Name
		}
	}
}

func TestEnsureDownloadBrowserBookmarksSeedMergeAndIdempotent(t *testing.T) {
	profile := t.TempDir()
	if err := EnsureDownloadBrowserBookmarks(profile); err != nil {
		t.Fatalf("initial EnsureDownloadBrowserBookmarks failed: %v", err)
	}

	defaultDir := filepath.Join(profile, "Default")
	bmData, err := os.ReadFile(filepath.Join(defaultDir, "Bookmarks"))
	if err != nil {
		t.Fatalf("read Bookmarks: %v", err)
	}
	var doc struct {
		Version int `json:"version"`
		Roots   struct {
			BookmarkBar struct {
				Children []struct {
					Name     string `json:"name"`
					Type     string `json:"type"`
					Children []struct {
						Name string `json:"name"`
						Type string `json:"type"`
						URL  string `json:"url"`
					} `json:"children"`
				} `json:"children"`
			} `json:"bookmark_bar"`
		} `json:"roots"`
	}
	if err := json.Unmarshal(bmData, &doc); err != nil {
		t.Fatalf("unmarshal Bookmarks: %v", err)
	}
	if doc.Version != 1 || len(doc.Roots.BookmarkBar.Children) != len(DownloadBookmarkFolders) {
		t.Fatalf("unexpected Bookmarks structure: version=%d folders=%d", doc.Version, len(doc.Roots.BookmarkBar.Children))
	}

	prefsData, err := os.ReadFile(filepath.Join(defaultDir, "Preferences"))
	if err != nil {
		t.Fatalf("read Preferences: %v", err)
	}
	var prefs map[string]any
	if err := json.Unmarshal(prefsData, &prefs); err != nil {
		t.Fatalf("unmarshal Preferences: %v", err)
	}
	barPref, _ := prefs["bookmark_bar"].(map[string]any)
	if show, _ := barPref["show_on_all_tabs"].(bool); !show {
		t.Fatalf("expected bookmark_bar.show_on_all_tabs=true, got %+v", prefs["bookmark_bar"])
	}

	// Second call is idempotent.
	if err := EnsureDownloadBrowserBookmarks(profile); err != nil {
		t.Fatalf("second EnsureDownloadBrowserBookmarks failed: %v", err)
	}
	bmData2, err := os.ReadFile(filepath.Join(defaultDir, "Bookmarks"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bmData2) != string(bmData) {
		t.Fatal("expected second EnsureDownloadBrowserBookmarks call to leave Bookmarks unchanged")
	}

	// Merging into an existing profile preserves user bookmarks and manual bar preference.
	profile2 := t.TempDir()
	def2 := filepath.Join(profile2, "Default")
	if err := os.MkdirAll(def2, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(def2, "Preferences"), map[string]any{
		"bookmark_bar": map[string]any{"show_on_all_tabs": false},
		"intl":         map[string]any{"selected_languages": "zh-CN"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(def2, "Bookmarks"), map[string]any{
		"checksum": "stale",
		"version":  1,
		"roots": map[string]any{
			"bookmark_bar": map[string]any{
				"id":   "1",
				"name": "书签栏",
				"type": "folder",
				"children": []any{
					map[string]any{"id": "10", "name": "My Site", "type": "url", "url": "https://example.com/custom"},
					map[string]any{
						"id":   "11",
						"name": "摄影作品",
						"type": "folder",
						"children": []any{
							map[string]any{"id": "12", "name": "Pixiv", "type": "url", "url": "https://www.pixiv.net/"},
						},
					},
				},
			},
			"other":  map[string]any{"id": "2", "name": "其他书签", "type": "folder", "children": []any{}},
			"synced": map[string]any{"id": "3", "name": "移动设备书签", "type": "folder", "children": []any{}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDownloadBrowserBookmarks(profile2); err != nil {
		t.Fatalf("merge EnsureDownloadBrowserBookmarks failed: %v", err)
	}

	var merged map[string]any
	mergedBytes, err := os.ReadFile(filepath.Join(def2, "Bookmarks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mergedBytes, &merged); err != nil {
		t.Fatal(err)
	}
	if cs, _ := merged["checksum"].(string); cs != "" {
		t.Fatalf("expected cleared checksum after merge, got %q", cs)
	}
	urls := make(map[string]bool)
	maxID := 0
	scanBookmarkTree(merged["roots"].(map[string]any)["bookmark_bar"], &maxID, urls)
	if !urls["https://example.com/custom"] || !urls["https://www.pixiv.net"] || !urls["https://www.temu.com"] {
		t.Fatalf("merged bookmarks missing expected URLs: custom=%v pixiv=%v temu=%v",
			urls["https://example.com/custom"], urls["https://www.pixiv.net"], urls["https://www.temu.com"])
	}

	prefs2Bytes, err := os.ReadFile(filepath.Join(def2, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	var prefs2 map[string]any
	if err := json.Unmarshal(prefs2Bytes, &prefs2); err != nil {
		t.Fatal(err)
	}
	if show, _ := prefs2["bookmark_bar"].(map[string]any)["show_on_all_tabs"].(bool); show {
		t.Fatal("expected existing manual bookmark_bar.show_on_all_tabs=false to be preserved")
	}
}
