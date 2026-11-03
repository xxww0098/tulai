package download

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func filterPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestImageFilterValidation(t *testing.T) {
	for _, f := range []ImageFilter{
		{MinBytes: -1}, {MaxBytes: -1}, {MinWidth: -1}, {MaxWidth: -1}, {MinHeight: -1}, {MaxHeight: -1},
		{MinBytes: 2, MaxBytes: 1}, {MinWidth: 2, MaxWidth: 1}, {MinHeight: 2, MaxHeight: 1},
	} {
		if f.Validate() == nil {
			t.Errorf("invalid filter accepted: %+v", f)
		}
	}
	for _, f := range []ImageFilter{{}, {MinBytes: 3}, {MaxWidth: 4}, {MinBytes: 3, MaxBytes: 3, MinWidth: 4, MaxWidth: 4}} {
		if err := f.Validate(); err != nil {
			t.Errorf("valid filter rejected: %+v, %v", f, err)
		}
	}
	var requests atomic.Int32
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		requests.Add(1)
		return nil, errors.New("invalid filter should not reach the network")
	})
	err := New(Options{Getter: getter, Retries: 2}).Download(context.Background(),
		Job{Directory: t.TempDir(), ImageFilter: ImageFilter{MinWidth: 2, MaxWidth: 1}}, Item{URL: "https://example.test/a.png"}, nil)
	if err == nil || requests.Load() != 0 {
		t.Fatalf("invalid filter: requests=%d, err=%v", requests.Load(), err)
	}
}

func TestImageFilterInclusiveBoundsAndCleanup(t *testing.T) {
	payload := filterPNG(t, 4, 3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	size := int64(len(payload))
	for _, tc := range []struct {
		name     string
		filter   ImageFilter
		accepted bool
	}{
		{"exact bounds", ImageFilter{MinBytes: size, MaxBytes: size, MinWidth: 4, MaxWidth: 4, MinHeight: 3, MaxHeight: 3}, true},
		{"below min bytes", ImageFilter{MinBytes: size + 1}, false},
		{"above max bytes", ImageFilter{MaxBytes: size - 1}, false},
		{"below min width", ImageFilter{MinWidth: 5}, false},
		{"above max width", ImageFilter{MaxWidth: 3}, false},
		{"below min height", ImageFilter{MinHeight: 4}, false},
		{"above max height", ImageFilter{MaxHeight: 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			archive := NewMemoryArchive()
			dl := New(Options{Getter: NewHTTPGetter(""), Archive: archive})
			item := Item{URL: srv.URL + "/image.png", RequireImage: true, ArchiveKey: "key"}
			var progress Progress
			err := dl.Download(context.Background(), Job{Directory: dir, ImageFilter: tc.filter}, item, func(p Progress) { progress = p })
			path := filepath.Join(dir, "image.png")
			if tc.accepted {
				got, readErr := os.ReadFile(path)
				if err != nil || readErr != nil || !bytes.Equal(got, payload) || !archive.Has("key") {
					t.Fatalf("accepted result err=%v, read=%v, archive=%v", err, readErr, archive.Has("key"))
				}
			} else {
				if !errors.Is(err, ErrFiltered) || archive.Has("key") || progress.Stage != "skip" || !errors.Is(progress.Err, ErrFiltered) {
					t.Fatalf("filtered result err=%v, archive=%v, progress=%+v", err, archive.Has("key"), progress)
				}
				if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
					t.Fatalf("filtered destination exists: %v", statErr)
				}
			}
			for _, suffix := range []string{".part", ".part.json"} {
				if _, statErr := os.Stat(path + suffix); !os.IsNotExist(statErr) {
					t.Fatalf("partial %s not removed: %v", suffix, statErr)
				}
			}
		})
	}
}

func TestImageFilterOverwritePreservesOriginalAndReleasesArchive(t *testing.T) {
	payload := filterPNG(t, 4, 3)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	archive := NewMemoryArchive()
	dl := New(Options{Getter: NewHTTPGetter(""), Archive: archive})
	item := Item{URL: srv.URL + "/image.png", RequireImage: true, ArchiveKey: "key"}
	err := dl.Download(context.Background(), Job{Directory: dir, ImageFilter: ImageFilter{MinWidth: 5}}, item, nil)
	if !errors.Is(err, ErrFiltered) || archive.Has("key") {
		t.Fatalf("filtered archive reservation: err=%v, archive=%v", err, archive.Has("key"))
	}
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = dl.Download(context.Background(), Job{Directory: dir, Overwrite: true, ImageFilter: ImageFilter{MinWidth: 5}}, item, nil)
	original, readErr := os.ReadFile(path)
	if !errors.Is(err, ErrFiltered) || readErr != nil || string(original) != "original" || archive.Has("key") {
		t.Fatalf("filtered overwrite err=%v, data=%q, archive=%v", err, original, archive.Has("key"))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := dl.Download(context.Background(), Job{Directory: dir}, item, nil); err != nil || !archive.Has("key") || hits.Load() != 3 {
		t.Fatalf("retry after filter: err=%v, archive=%v, requests=%d", err, archive.Has("key"), hits.Load())
	}
}

func TestImageFilterUnreadableDimensionsDoesNotOverwrite(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("\x00\x00\x00\x18ftypavifnot-supported"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "image.avif")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	dl := New(Options{Getter: NewHTTPGetter(""), Retries: 2})
	err := dl.Download(context.Background(), Job{Directory: dir, Overwrite: true, ImageFilter: ImageFilter{MinWidth: 1}}, Item{URL: srv.URL + "/image.avif", RequireImage: true}, nil)
	data, readErr := os.ReadFile(path)
	if err == nil || errors.Is(err, ErrFiltered) || !strings.Contains(err.Error(), "无法读取图片尺寸") || readErr != nil || string(data) != "original" || hits.Load() != 1 {
		t.Fatalf("unknown dimensions: err=%v, data=%q, requests=%d", err, data, hits.Load())
	}
	for _, suffix := range []string{".part", ".part.json"} {
		if _, statErr := os.Stat(path + suffix); !os.IsNotExist(statErr) {
			t.Fatalf("partial %s not removed: %v", suffix, statErr)
		}
	}
}

func TestRunBatchMarksFilteredAsSkipped(t *testing.T) {
	small, large := filterPNG(t, 1, 1), filterPNG(t, 4, 3)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/small.png" {
			_, _ = w.Write(small)
		} else {
			_, _ = w.Write(large)
		}
	}))
	defer srv.Close()
	items := []Item{{URL: srv.URL + "/small.png"}, {URL: srv.URL + "/large.png"}}
	results := RunBatch(context.Background(), New(Options{Getter: NewHTTPGetter("")}),
		Job{Directory: t.TempDir(), ImageFilter: ImageFilter{MinWidth: 2}}, items, 2, nil)
	if len(results) != 2 || !results[0].Skipped || !errors.Is(results[0].Err, ErrFiltered) || results[1].Skipped || results[1].Err != nil {
		t.Fatalf("batch results = %+v", results)
	}
}

type filterCancelBody struct {
	data   []byte
	cancel context.CancelFunc
}

func (b *filterCancelBody) Read(p []byte) (int, error) {
	n := copy(p, b.data)
	b.cancel()
	return n, io.EOF
}

func (b *filterCancelBody) Close() error { return nil }

func TestImageFilterCancellationDoesNotReplaceOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.png")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	data := filterPNG(t, 4, 3)
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: http.StatusOK, Header: http.Header{}, FinalURL: raw, Body: &filterCancelBody{data: data, cancel: cancel}}, nil
	})
	err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir, Overwrite: true, ImageFilter: ImageFilter{MinWidth: 2}}, Item{URL: "https://example.test/image.png"}, nil)
	got, readErr := os.ReadFile(path)
	if !errors.Is(err, context.Canceled) || readErr != nil || string(got) != "original" {
		t.Fatalf("canceled filter: err=%v, old file=%q, read=%v", err, got, readErr)
	}
}
