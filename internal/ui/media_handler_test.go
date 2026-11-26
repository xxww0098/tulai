package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func makeTestPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMediaOriginPlatformAware(t *testing.T) {
	if got := mediaOriginForGOOS("windows"); got != "http://tulaimedia.localhost" {
		t.Fatalf("windows origin = %q", got)
	}
	if got := mediaOriginForGOOS("darwin"); got != "tulaimedia://localhost" {
		t.Fatalf("darwin origin = %q", got)
	}
	if got := mediaOriginForGOOS("linux"); got != "tulaimedia://localhost" {
		t.Fatalf("linux origin = %q", got)
	}
}

func TestMediaHandlerOpaqueTokensThumbnailsAndCacheBusting(t *testing.T) {
	dir := t.TempDir()
	opaquePath := filepath.Join(dir, "opaque.png")
	if err := os.WriteFile(opaquePath, makeTestPNG(t, 400, 200, color.NRGBA{R: 30, G: 120, B: 220, A: 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	alphaPath := filepath.Join(dir, "alpha.png")
	if err := os.WriteFile(alphaPath, makeTestPNG(t, 200, 100, color.NRGBA{R: 220, G: 60, B: 80, A: 128}), 0o644); err != nil {
		t.Fatal(err)
	}
	txtPath := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(txtPath, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(nil, nil, nil, dir)
	h := app.ensureMedia()

	// Unknown token -> 404
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/file?t=doesnotexist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown token status = %d, want 404", rec.Code)
	}

	// Non-image extension -> 403 even if token registered
	txtURL := h.FileURL(txtPath)
	parsedTxt, _ := url.Parse(txtURL)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, parsedTxt.RequestURI(), nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-image status = %d, want 403", rec.Code)
	}

	// Full-res image via opaque token (no raw path in URL)
	rawURL := h.FileURL(opaquePath)
	if strings.Contains(rawURL, "opaque.png") {
		t.Fatalf("media URL leaked filesystem path: %s", rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u.RequestURI(), nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("full-res status=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS Access-Control-Allow-Origin=*, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	recOpt := httptest.NewRecorder()
	h.ServeHTTP(recOpt, httptest.NewRequest(http.MethodOptions, u.RequestURI(), nil))
	if recOpt.Code != http.StatusNoContent || recOpt.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("OPTIONS preflight status=%d acao=%q", recOpt.Code, recOpt.Header().Get("Access-Control-Allow-Origin"))
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("expected immutable Cache-Control, got %q", rec.Header().Get("Cache-Control"))
	}

	// Resized opaque thumbnail (&w=100 appended freely by frontend) -> JPEG <= 100px wide
	recThumb := httptest.NewRecorder()
	h.ServeHTTP(recThumb, httptest.NewRequest(http.MethodGet, u.RequestURI()+"&w=100", nil))
	if recThumb.Code != http.StatusOK || recThumb.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("opaque thumb status=%d ct=%q", recThumb.Code, recThumb.Header().Get("Content-Type"))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(recThumb.Body.Bytes()))
	if err != nil || cfg.Width != 100 || cfg.Height != 50 {
		t.Fatalf("thumb dims = %dx%d err=%v, want 100x50", cfg.Width, cfg.Height, err)
	}

	// Second request hits LRU cache
	recThumb2 := httptest.NewRecorder()
	h.ServeHTTP(recThumb2, httptest.NewRequest(http.MethodGet, u.RequestURI()+"&w=100", nil))
	if recThumb2.Code != http.StatusOK || !bytes.Equal(recThumb.Body.Bytes(), recThumb2.Body.Bytes()) {
		t.Fatalf("cached thumb mismatch: status=%d", recThumb2.Code)
	}

	// Alpha PNG thumbnail stays PNG when small
	alphaURL, _ := url.Parse(h.FileURL(alphaPath))
	recAlpha := httptest.NewRecorder()
	h.ServeHTTP(recAlpha, httptest.NewRequest(http.MethodGet, alphaURL.RequestURI()+"&w=80", nil))
	if recAlpha.Code != http.StatusOK || recAlpha.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("alpha thumb status=%d ct=%q", recAlpha.Code, recAlpha.Header().Get("Content-Type"))
	}

	// In-place file overwrite (replaceResultImage) changes v= mtime query param
	vBefore := u.Query().Get("v")
	future := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(opaquePath, makeTestPNG(t, 300, 300, color.NRGBA{R: 10, G: 200, B: 50, A: 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(opaquePath, future, future); err != nil {
		t.Fatal(err)
	}
	uAfter, _ := url.Parse(h.FileURL(opaquePath))
	if uAfter.Query().Get("v") == vBefore {
		t.Fatalf("mtime cache-buster did not change after overwrite: %q", vBefore)
	}
}

func TestMediaHandlerDynamicEndpoints(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "edit.png")
	rawPNG := makeTestPNG(t, 64, 64, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	if err := os.WriteFile(imgPath, rawPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(nil, nil, nil, dir)
	h := app.ensureMedia()

	// Drawer frame before data -> 404; after data -> 200 JPEG no-store
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/drawer/frame?v=1", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty drawer frame status = %d, want 404", rec.Code)
	}
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	app.setDrawerFrame(fakeJPEG)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/drawer/frame?v=2", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || rec.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(rec.Body.Bytes(), fakeJPEG) {
		t.Fatalf("drawer frame response unexpected: status=%d ct=%q cc=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Cache-Control"))
	}

	// Edit orig & preview endpoints
	app.setEditOrigPath(imgPath)
	recOrig := httptest.NewRecorder()
	h.ServeHTTP(recOrig, httptest.NewRequest(http.MethodGet, "/edit/orig?v=1", nil))
	if recOrig.Code != http.StatusOK || !bytes.Equal(recOrig.Body.Bytes(), rawPNG) {
		t.Fatalf("edit orig status=%d len=%d", recOrig.Code, recOrig.Body.Len())
	}

	app.setEditPreviewJPEG(fakeJPEG)
	recPrev := httptest.NewRecorder()
	h.ServeHTTP(recPrev, httptest.NewRequest(http.MethodGet, "/edit/preview?v=1", nil))
	if recPrev.Code != http.StatusOK || recPrev.Header().Get("Content-Type") != "image/jpeg" || recPrev.Header().Get("Cache-Control") != "no-store" || !bytes.Equal(recPrev.Body.Bytes(), fakeJPEG) {
		t.Fatalf("edit preview unexpected: status=%d ct=%q cc=%q", recPrev.Code, recPrev.Header().Get("Content-Type"), recPrev.Header().Get("Cache-Control"))
	}

	// Brand icon endpoint
	SetAppIcon(rawPNG)
	recIcon := httptest.NewRecorder()
	h.ServeHTTP(recIcon, httptest.NewRequest(http.MethodGet, "/brand-icon", nil))
	if recIcon.Code != http.StatusOK || !bytes.Equal(recIcon.Body.Bytes(), rawPNG) {
		t.Fatalf("brand icon status=%d", recIcon.Code)
	}
}

func TestMediaHandlerConcurrentThumbnailSingleflight(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "concurrent.png")
	if err := os.WriteFile(imgPath, makeTestPNG(t, 480, 240, color.NRGBA{R: 40, G: 130, B: 210, A: 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp(nil, nil, nil, dir)
	h := app.ensureMedia()
	u, err := url.Parse(h.FileURL(imgPath))
	if err != nil {
		t.Fatal(err)
	}
	reqURI := u.RequestURI() + "&w=120"

	const workers = 16
	results := make([][]byte, workers)
	codes := make([]int, workers)
	cts := make([]string, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, reqURI, nil))
			codes[idx] = rec.Code
			cts[idx] = rec.Header().Get("Content-Type")
			results[idx] = append([]byte(nil), rec.Body.Bytes()...)
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < workers; i++ {
		if codes[i] != http.StatusOK || cts[i] != "image/jpeg" {
			t.Fatalf("worker %d: status=%d ct=%q", i, codes[i], cts[i])
		}
		if !bytes.Equal(results[i], results[0]) {
			t.Fatalf("worker %d returned different thumbnail bytes", i)
		}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(results[0]))
	if err != nil || cfg.Width != 120 || cfg.Height != 60 {
		t.Fatalf("singleflight thumb dims = %dx%d err=%v, want 120x60", cfg.Width, cfg.Height, err)
	}
}
