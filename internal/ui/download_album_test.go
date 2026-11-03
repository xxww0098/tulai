package ui

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xxww0098/tulai/internal/download"
)

func TestDownloadPageKeepsAlbumsSeparate(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data.Bytes())
	}))
	defer server.Close()
	a, _ := newDownloadApp(t)
	dir := t.TempDir()
	task := newDownloadTask(1, server.URL+"/albumlist", dir)
	found := download.Discovery{IsPage: true, Title: "店铺相册", Items: []download.Item{
		{URL: server.URL + "/one.png", Metadata: map[string]string{"album_id": "1", "album_title": "../夏装"}},
		{URL: server.URL + "/two.png", Metadata: map[string]string{"album_id": "2", "album_title": "../夏装"}},
	}}
	err := a.downloadPageImages(context.Background(), task, download.New(download.Options{Getter: download.NewHTTPGetter("")}), download.Job{Directory: dir}, found, "", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := os.ReadDir(task.Path)
	if err != nil || len(folders) != 2 {
		t.Fatalf("album folders = %v, %v", folders, err)
	}
	for _, folder := range folders {
		if !folder.IsDir() {
			t.Fatalf("album path is not a directory: %s", folder.Name())
		}
		files, err := os.ReadDir(filepath.Join(task.Path, folder.Name()))
		if err != nil || len(files) != 1 || files[0].IsDir() {
			t.Fatalf("album %s = %v, %v", folder.Name(), files, err)
		}
	}
}
