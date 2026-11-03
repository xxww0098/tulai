package download

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type alternateBody struct {
	io.Reader
	closed bool
}

func (b *alternateBody) Close() error { b.closed = true; return nil }

func alternateResponse(raw string, status int, body io.ReadCloser, headers http.Header) *Response {
	if headers == nil {
		headers = http.Header{}
	}
	return &Response{StatusCode: status, Header: headers, FinalURL: raw, Body: body}
}

func TestAlternate404FallbackPreservesIdentity(t *testing.T) {
	primary := "https://cdn.example/image?format=jpg&name=orig"
	alt := "https://cdn.example/image?format=png&name=orig"
	png := filterPNG(t, 3, 2)
	miss := &alternateBody{Reader: strings.NewReader("missing")}
	var requests []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		requests = append(requests, raw)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		prepare(req)
		if req.Header.Get("Authorization") != "Bearer same-origin" || req.Header.Get("Range") != "" {
			t.Fatalf("headers = %v", req.Header)
		}
		if raw == primary {
			return alternateResponse(raw, http.StatusNotFound, miss, nil), nil
		}
		if raw != alt || !miss.closed {
			t.Fatalf("alternate = %q; 404 closed = %v", raw, miss.closed)
		}
		return alternateResponse(raw, http.StatusOK, io.NopCloser(bytes.NewReader(png)), nil), nil
	})
	dir := t.TempDir()
	archive := NewMemoryArchive()
	item := Item{URL: primary, AlternateURLs: []string{alt}, Filename: "image", RequireImage: true,
		ArchiveKey: "key", Headers: map[string]string{"Authorization": "Bearer same-origin"}}
	var progress []Progress
	err := New(Options{Getter: getter, Archive: archive}).Download(context.Background(), Job{Directory: dir}, item, func(p Progress) { progress = append(progress, p) })
	data, readErr := os.ReadFile(filepath.Join(dir, "image.png"))
	if err != nil || readErr != nil || !bytes.Equal(data, png) || !archive.Has("key") || !reflect.DeepEqual(requests, []string{primary, alt}) {
		t.Fatalf("download = %v, read = %v, archive = %v, requests = %v", err, readErr, archive.Has("key"), requests)
	}
	for _, p := range progress {
		if p.URL != primary {
			t.Fatalf("progress changed logical URL: %+v", p)
		}
	}
}

func TestAlternateAllNotFoundClosesBodies(t *testing.T) {
	primary, alt := "https://cdn.example/image?format=jpg", "https://cdn.example/image?format=png"
	var bodies []*alternateBody
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := &alternateBody{Reader: strings.NewReader("not found")}
		bodies = append(bodies, body)
		return alternateResponse(raw, http.StatusNotFound, body, nil), nil
	})
	err := New(Options{Getter: getter, Retries: 2}).Download(context.Background(), Job{Directory: t.TempDir()}, Item{URL: primary, AlternateURLs: []string{alt}}, nil)
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusNotFound || status.URL != alt || len(bodies) != 2 || !bodies[0].closed || !bodies[1].closed {
		t.Fatalf("not found = %v, bodies = %+v", err, bodies)
	}
}

func TestAlternatesRejectUnsafeAndNon404(t *testing.T) {
	primary := "https://cdn.example/image?format=jpg"
	for _, raw := range []string{
		"https://other.example/image", "https://cdn.example.evil.test/image", "http://cdn.example/image",
		"https://user:pass@cdn.example/image", "https://cdn.example:8443/image", "/relative", "https://cdn.example/image#fragment",
	} {
		t.Run(raw, func(t *testing.T) {
			calls := 0
			getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
				calls++
				return nil, errors.New("must not request an unsafe alternate")
			})
			err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: t.TempDir()}, Item{URL: primary, AlternateURLs: []string{raw}}, nil)
			var local *LocalError
			if !errors.As(err, &local) || calls != 0 {
				t.Fatalf("unsafe alternate %q: err=%v, calls=%d", raw, err, calls)
			}
		})
	}
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			body := &alternateBody{Reader: strings.NewReader("rejected")}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				return alternateResponse(raw, status, body, nil), nil
			})
			err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: t.TempDir()}, Item{URL: primary, AlternateURLs: []string{"https://cdn.example/image?format=png"}}, nil)
			var result *HTTPStatusError
			if !errors.As(err, &result) || result.StatusCode != status || calls != 1 || !body.closed {
				t.Fatalf("status %d: err=%v, calls=%d, closed=%v", status, err, calls, body.closed)
			}
		})
	}
	for _, cancelOn404 := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		body := &alternateBody{Reader: strings.NewReader("missing")}
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			calls++
			if cancelOn404 {
				cancel()
				return alternateResponse(raw, http.StatusNotFound, body, nil), nil
			}
			return nil, errors.New("network failure")
		})
		err := New(Options{Getter: getter}).Download(ctx, Job{Directory: t.TempDir()}, Item{URL: primary, AlternateURLs: []string{"https://cdn.example/image?format=png"}}, nil)
		if calls != 1 || (cancelOn404 && (!errors.Is(err, context.Canceled) || !body.closed)) || (!cancelOn404 && err == nil) {
			t.Fatalf("canceled=%v err=%v calls=%d closed=%v", cancelOn404, err, calls, body.closed)
		}
		cancel()
	}
}

type alternateCancelReader struct {
	data   []byte
	cancel context.CancelFunc
}

func (r *alternateCancelReader) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	r.cancel()
	return n, nil
}

func (*alternateCancelReader) Close() error { return nil }

func TestAlternateResumeUsesSelectedSource(t *testing.T) {
	primary, alt := "https://cdn.example/photo?format=jpg", "https://cdn.example/photo?format=png"
	full, prefix := "new-image-content", "new-image"
	ctx, cancel := context.WithCancel(context.Background())
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == primary {
			return alternateResponse(raw, http.StatusNotFound, io.NopCloser(strings.NewReader("")), nil), nil
		}
		return alternateResponse(raw, http.StatusOK, &alternateCancelReader{data: []byte(prefix), cancel: cancel},
			http.Header{"Etag": {`"v1"`}, "Content-Length": {fmt.Sprint(len(full))}}), nil
	})
	dir := t.TempDir()
	item := Item{URL: primary, AlternateURLs: []string{alt}, Filename: "photo.bin"}
	err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first transfer = %v", err)
	}
	part := filepath.Join(dir, "photo.bin.part")
	var meta PartialMeta
	metadata, readErr := os.ReadFile(part + ".json")
	if readErr != nil || json.Unmarshal(metadata, &meta) != nil || meta.URL != primary || meta.SourceURL != alt || meta.FinalURL != alt {
		t.Fatalf("saved metadata = %+v, read = %v", meta, readErr)
	}
	partial, readErr := os.ReadFile(part)
	if readErr != nil || string(partial) != prefix {
		t.Fatalf("partial = %q, %v", partial, readErr)
	}
	calls := 0
	resume := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if raw != alt {
			t.Fatalf("resume selected %q", raw)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if req.Header.Get("Range") != fmt.Sprintf("bytes=%d-", len(prefix)) || req.Header.Get("If-Range") != `"v1"` {
			t.Fatalf("resume headers = %v", req.Header)
		}
		headers := http.Header{"Etag": {`"v1"`}, "Content-Range": {fmt.Sprintf("bytes %d-%d/%d", len(prefix), len(full)-1, len(full))}}
		return alternateResponse(raw, http.StatusPartialContent, io.NopCloser(strings.NewReader(full[len(prefix):])), headers), nil
	})
	err = New(Options{Getter: resume}).Download(context.Background(), Job{Directory: dir}, item, nil)
	saved, readErr := os.ReadFile(filepath.Join(dir, "photo.bin"))
	if err != nil || readErr != nil || string(saved) != full || calls != 1 {
		t.Fatalf("resume = %q, err=%v, read=%v, calls=%d", saved, err, readErr, calls)
	}
}

func TestAlternateResumeBeforeFirstByteKeepsSelectedSource(t *testing.T) {
	primary, alt := "https://cdn.example/photo?format=jpg", "https://cdn.example/photo?format=png"
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == primary {
			return alternateResponse(raw, http.StatusNotFound, io.NopCloser(strings.NewReader("")), nil), nil
		}
		cancel()
		return alternateResponse(raw, http.StatusOK, io.NopCloser(strings.NewReader("fresh")), nil), nil
	})
	item := Item{URL: primary, AlternateURLs: []string{alt}, Filename: "photo.bin"}
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("first transfer = %v", err)
	}
	part := filepath.Join(dir, "photo.bin.part")
	if info, err := os.Stat(part); err != nil || info.Size() != 0 {
		t.Fatalf("empty partial = %v, %v", info, err)
	}
	var calls []string
	resume := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if raw != alt || req.Header.Get("Range") != "" {
			t.Fatalf("zero-byte resume url=%q, headers=%v", raw, req.Header)
		}
		return alternateResponse(raw, http.StatusOK, io.NopCloser(strings.NewReader("fresh")), nil), nil
	})
	err := New(Options{Getter: resume}).Download(context.Background(), Job{Directory: dir}, item, nil)
	got, readErr := os.ReadFile(filepath.Join(dir, "photo.bin"))
	if err != nil || readErr != nil || string(got) != "fresh" || !reflect.DeepEqual(calls, []string{alt}) {
		t.Fatalf("zero-byte resume=%q, err=%v, read=%v, calls=%v", got, err, readErr, calls)
	}
}

func TestAlternateEmptyPartialDiscardedAfterNotFound(t *testing.T) {
	primary, alt := "https://cdn.example/photo?format=jpg", "https://cdn.example/photo?format=png"
	dir := t.TempDir()
	part := filepath.Join(dir, "photo.bin.part")
	if err := os.WriteFile(part, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(part, PartialMeta{URL: primary, SourceURL: alt, Total: -1}); err != nil {
		t.Fatal(err)
	}
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		if raw == alt {
			return alternateResponse(raw, http.StatusNotFound, io.NopCloser(strings.NewReader("")), nil), nil
		}
		if _, err := os.Stat(part); !os.IsNotExist(err) {
			t.Fatalf("empty partial of expired source still exists: %v", err)
		}
		return alternateResponse(raw, http.StatusOK, io.NopCloser(strings.NewReader("fresh")), nil), nil
	})
	err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: dir}, Item{URL: primary, AlternateURLs: []string{alt}, Filename: "photo.bin"}, nil)
	got, readErr := os.ReadFile(filepath.Join(dir, "photo.bin"))
	if err != nil || readErr != nil || string(got) != "fresh" || !reflect.DeepEqual(calls, []string{alt, primary}) {
		t.Fatalf("expired empty partial=%q, err=%v, read=%v, calls=%v", got, err, readErr, calls)
	}
}

func TestAlternateInvalidatedPartialNeverMixesSources(t *testing.T) {
	primary, alt := "https://cdn.example/photo?format=jpg", "https://cdn.example/photo?format=png"
	dir := t.TempDir()
	part := filepath.Join(dir, "photo.bin.part")
	if err := os.WriteFile(part, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(part, PartialMeta{URL: primary, SourceURL: alt, FinalURL: alt, ETag: `"v1"`, Total: 10}); err != nil {
		t.Fatal(err)
	}
	var calls []string
	miss := &alternateBody{Reader: strings.NewReader("missing")}
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if raw == alt {
			if req.Header.Get("Range") != "bytes=3-" {
				t.Fatalf("saved source not resumed: %v", req.Header)
			}
			return alternateResponse(raw, http.StatusNotFound, miss, nil), nil
		}
		if raw != primary || req.Header.Get("Range") != "" || req.Header.Get("If-Range") != "" || !miss.closed {
			t.Fatalf("replacement request %q: %v, previous closed=%v", raw, req.Header, miss.closed)
		}
		return alternateResponse(raw, http.StatusOK, io.NopCloser(strings.NewReader("fresh")), nil), nil
	})
	err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: dir}, Item{URL: primary, AlternateURLs: []string{alt}, Filename: "photo.bin"}, nil)
	got, readErr := os.ReadFile(filepath.Join(dir, "photo.bin"))
	if err != nil || readErr != nil || string(got) != "fresh" || !reflect.DeepEqual(calls, []string{alt, primary}) {
		t.Fatalf("result=%q, err=%v, read=%v, calls=%v", got, err, readErr, calls)
	}
}

func TestAlternateExpiredSourceContinuesThroughRemainingCandidates(t *testing.T) {
	primary := "https://cdn.example/photo?format=jpg"
	first := "https://cdn.example/photo?format=png"
	second := "https://cdn.example/photo?format=webp"
	dir := t.TempDir()
	part := filepath.Join(dir, "photo.bin.part")
	if err := os.WriteFile(part, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(part, PartialMeta{URL: primary, SourceURL: first, ETag: `"v1"`, Total: 10}); err != nil {
		t.Fatal(err)
	}
	var calls []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if raw == first {
			if req.Header.Get("Range") != "bytes=3-" {
				t.Fatalf("first request headers = %v", req.Header)
			}
		} else if req.Header.Get("Range") != "" {
			t.Fatalf("another source inherited Range: %v", req.Header)
		}
		if raw == second {
			return alternateResponse(raw, http.StatusOK, io.NopCloser(strings.NewReader("fresh")), nil), nil
		}
		return alternateResponse(raw, http.StatusNotFound, io.NopCloser(strings.NewReader("missing")), nil), nil
	})
	err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: dir}, Item{
		URL: primary, AlternateURLs: []string{first, second}, Filename: "photo.bin",
	}, nil)
	got, readErr := os.ReadFile(filepath.Join(dir, "photo.bin"))
	if err != nil || readErr != nil || string(got) != "fresh" || !reflect.DeepEqual(calls, []string{first, primary, second}) {
		t.Fatalf("result=%q, err=%v, read=%v, calls=%v", got, err, readErr, calls)
	}
}

func TestAlternateRemovedFromCandidatesIsDiscarded(t *testing.T) {
	primary, removed := "https://cdn.example/photo?format=jpg", "https://cdn.example/photo?format=webp"
	dir := t.TempDir()
	part := filepath.Join(dir, "photo.bin.part")
	if err := os.WriteFile(part, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(part, PartialMeta{URL: primary, SourceURL: removed, ETag: `"v1"`, Total: 10}); err != nil {
		t.Fatal(err)
	}
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if raw != primary || req.Header.Get("Range") != "" {
			t.Fatalf("removed source was resumed: url=%q headers=%v", raw, req.Header)
		}
		return alternateResponse(raw, http.StatusOK, io.NopCloser(strings.NewReader("fresh")), nil), nil
	})
	err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: dir}, Item{URL: primary, AlternateURLs: []string{"https://cdn.example/photo?format=png"}, Filename: "photo.bin"}, nil)
	got, readErr := os.ReadFile(filepath.Join(dir, "photo.bin"))
	if err != nil || readErr != nil || string(got) != "fresh" {
		t.Fatalf("result=%q, err=%v, read=%v", got, err, readErr)
	}
}

func TestAlternateChangedRedirectRestarts(t *testing.T) {
	primary, alt := "https://cdn.example/photo?format=jpg", "https://cdn.example/photo?format=png"
	dir := t.TempDir()
	part := filepath.Join(dir, "photo.bin.part")
	if err := os.WriteFile(part, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(part, PartialMeta{URL: primary, SourceURL: alt, FinalURL: "https://cdn.example/old", ETag: `"same"`, Total: 8}); err != nil {
		t.Fatal(err)
	}
	var ranges []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		if raw != alt {
			t.Fatalf("expected selected alternate, got %q", raw)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		ranges = append(ranges, req.Header.Get("Range"))
		if len(ranges) == 1 {
			headers := http.Header{"Etag": {`"same"`}, "Content-Range": {"bytes 3-7/8"}}
			response := alternateResponse(raw, http.StatusPartialContent, io.NopCloser(strings.NewReader("wrong")), headers)
			response.FinalURL = "https://cdn.example/new"
			return response, nil
		}
		response := alternateResponse(raw, http.StatusOK, io.NopCloser(strings.NewReader("fresh")), nil)
		response.FinalURL = "https://cdn.example/new"
		return response, nil
	})
	err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: dir}, Item{URL: primary, AlternateURLs: []string{alt}, Filename: "photo.bin"}, nil)
	got, readErr := os.ReadFile(filepath.Join(dir, "photo.bin"))
	if err != nil || readErr != nil || string(got) != "fresh" || !reflect.DeepEqual(ranges, []string{"bytes=3-", ""}) {
		t.Fatalf("result=%q, err=%v, read=%v, ranges=%v", got, err, readErr, ranges)
	}
}

func TestAlternateFilterAndExistingFileStaySafe(t *testing.T) {
	primary, alt := "https://cdn.example/image?format=jpg", "https://cdn.example/image?format=png"
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	png := filterPNG(t, 4, 3)
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		if raw == primary {
			return alternateResponse(raw, http.StatusNotFound, io.NopCloser(strings.NewReader("")), nil), nil
		}
		return alternateResponse(raw, http.StatusOK, io.NopCloser(bytes.NewReader(png)), nil), nil
	})
	item := Item{URL: primary, AlternateURLs: []string{alt}, Filename: "photo", RequireImage: true}
	dl := New(Options{Getter: getter})
	if err := dl.Download(context.Background(), Job{Directory: dir}, item, nil); !errors.Is(err, ErrSkipped) || calls != 0 {
		t.Fatalf("existing file: err=%v, calls=%d", err, calls)
	}
	err := dl.Download(context.Background(), Job{Directory: dir, Overwrite: true, ImageFilter: ImageFilter{MinWidth: 5}}, item, nil)
	got, readErr := os.ReadFile(path)
	if !errors.Is(err, ErrFiltered) || readErr != nil || string(got) != "original" || calls != 2 {
		t.Fatalf("filtered overwrite: err=%v, data=%q, read=%v, calls=%d", err, got, readErr, calls)
	}
}

func TestAlternateRedirectDoesNotForwardCredentials(t *testing.T) {
	var cookie, auth string
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, auth = r.Header.Get("Cookie"), r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "image")
	}))
	defer foreign.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/primary" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, foreign.URL+"/image", http.StatusFound)
	}))
	defer source.Close()
	item := Item{URL: source.URL + "/primary", AlternateURLs: []string{source.URL + "/alternate"}, Filename: "image.bin",
		Headers: map[string]string{"Cookie": "session=synthetic", "Authorization": "Bearer synthetic"}}
	err := New(Options{Getter: NewHTTPGetter("")}).Download(context.Background(), Job{Directory: t.TempDir()}, item, nil)
	if err != nil || cookie != "" || auth != "" {
		t.Fatalf("redirect err=%v cookie=%q auth=%q", err, cookie, auth)
	}
}
