package download

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestImageDownloadUsesActualFormatAndPreservesExistingFile(t *testing.T) {
	png := filterPNG(t, 2, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()

	t.Run("uses detected extension", func(t *testing.T) {
		dir := t.TempDir()
		err := New(Options{Getter: NewHTTPGetter(""), PathLock: NewPathLocker()}).Download(context.Background(), Job{Directory: dir}, Item{URL: server.URL + "/photo.jpg", Filename: "photo.jpg", RequireImage: true}, nil)
		data, readErr := os.ReadFile(filepath.Join(dir, "photo.png"))
		if err != nil || readErr != nil || !bytes.Equal(data, png) {
			t.Fatalf("detected image: err=%v, read=%v", err, readErr)
		}
		if _, err := os.Stat(filepath.Join(dir, "photo.jpg")); !os.IsNotExist(err) {
			t.Fatalf("mismatched extension exists: %v", err)
		}
	})
	t.Run("does not overwrite detected destination", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "photo.png")
		if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := New(Options{Getter: NewHTTPGetter(""), PathLock: NewPathLocker()}).Download(context.Background(), Job{Directory: dir}, Item{URL: server.URL + "/photo.jpg", Filename: "photo.jpg", RequireImage: true}, nil)
		data, readErr := os.ReadFile(path)
		if !errors.Is(err, ErrSkipped) || readErr != nil || string(data) != "existing" {
			t.Fatalf("existing image changed: err=%v, data=%q, read=%v", err, data, readErr)
		}
	})
}

func TestImageDownloadsWithSameBaseSharePathLock(t *testing.T) {
	png := filterPNG(t, 2, 2)
	entered := make(chan string, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.URL.Path
		if r.URL.Path == "/first" {
			<-release
		}
		_, _ = w.Write(png)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	dir := t.TempDir()
	dl := New(Options{Getter: NewHTTPGetter(""), PathLock: NewPathLocker()})
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		first <- dl.Download(context.Background(), Job{Directory: dir}, Item{URL: server.URL + "/first", Filename: "photo.jpg", RequireImage: true}, nil)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first download did not start")
	}
	go func() {
		second <- dl.Download(context.Background(), Job{Directory: dir}, Item{URL: server.URL + "/second", Filename: "photo.png", RequireImage: true}, nil)
	}()
	select {
	case path := <-entered:
		t.Fatalf("same image base downloaded concurrently: %s", path)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first download: %v", err)
	}
	if err := <-second; !errors.Is(err, ErrSkipped) {
		t.Fatalf("second download should preserve existing image: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "photo.png"))
	if err != nil || !bytes.Equal(data, png) {
		t.Fatalf("final image changed: %v", err)
	}
}

func TestImageDownloadRejectsHTMLWithoutReplacingExistingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("<html>需要登录</html>"))
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := New(Options{Getter: NewHTTPGetter("")}).Download(context.Background(), Job{Directory: dir, Overwrite: true}, Item{URL: server.URL, Filename: "photo.jpg", RequireImage: true}, nil)
	if err == nil {
		t.Fatal("错误网页被当成图片")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("原文件被更改: %q, %v", data, err)
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Fatalf("无效半成品未清理: %v", err)
	}
}

func TestImageExtensionDetectsNonstandardFormats(t *testing.T) {
	for _, tc := range []struct {
		data string
		want string
	}{
		{"II*\x00", ".tiff"},
		{"\x00\x00\x00\x18ftypavif", ".avif"},
		{"\x00\x00\x00\x18ftypheic", ".heic"},
		{"<?xml version=\"1.0\"?><svg></svg>", ".svg"},
		{"<html>not an image</html>", ""},
	} {
		if got := imageExtension([]byte(tc.data)); got != tc.want {
			t.Errorf("imageExtension(%q) = %q, want %q", tc.data, got, tc.want)
		}
	}
}
