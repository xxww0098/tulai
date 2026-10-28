package captcha

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
)

func TestSolveDisabled(t *testing.T) {
	t.Setenv("TULAI_BAXIA_SOLVE", "0")
	t.Setenv("TULAI_CDP_URL", "http://127.0.0.1:1")
	if err := SolveQianwenSlider(context.Background()); err != errBaxiaDisabled {
		t.Fatalf("err=%v", err)
	}
}

func TestSliderEndpointsPreferExplicitURL(t *testing.T) {
	t.Setenv("TULAI_CDP_URL", "http://127.0.0.1:9")
	t.Setenv("TULAI_CDP_PORT", "9222")
	got := sliderEndpoints()
	if len(got) != 1 || got[0] != "http://127.0.0.1:9" {
		t.Fatalf("endpoints=%v", got)
	}
}

func TestDevtoolsActivePortEndpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("9333\n/devtools/browser/abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := devtoolsEndpoint(dir); got != "ws://127.0.0.1:9333/devtools/browser/abc" {
		t.Fatalf("endpoint %q", got)
	}
}

func TestTrustedDragPassesPuzzleSlider(t *testing.T) {
	if testing.Short() {
		t.Skip("需要本机 Chrome")
	}
	chrome, err := browser.FindChromePath()
	if err != nil {
		t.Skip(err)
	}
	bg, piece := puzzleFixtures()
	mux := http.NewServeMux()
	mux.HandleFunc("/bg.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, bg)
	})
	mux.HandleFunc("/piece.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, piece)
	})
	mux.HandleFunc("/frame.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(puzzleFrameHTML(160)))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body style="margin:0">
<iframe id="baxia-dialog-content" src="/frame.html" style="position:absolute;left:24px;top:36px;width:320px;height:240px;border:0"></iframe>
</body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	endpoint := startDebugChrome(t, chrome, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := SolveQianwenSliderAt(ctx, endpoint); err != nil {
		t.Fatal(err)
	}
}

func puzzleFixtures() (*image.NRGBA, *image.NRGBA) {
	bg := image.NewNRGBA(image.Rect(0, 0, 280, 120))
	fill(bg, color.NRGBA{R: 210, G: 210, B: 210, A: 255})
	fillRect(bg, 160, 40, 40, 40, color.NRGBA{R: 30, G: 30, B: 30, A: 255})
	piece := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	fill(piece, color.NRGBA{R: 180, G: 180, B: 180, A: 255})
	return bg, piece
}

func puzzleFrameHTML(gap int) string {
	return fmt.Sprintf(`<!doctype html><html><body style="margin:0">
<div id="aliyunCaptcha-window-popup" style="position:relative;width:280px;background:#fff">
  <div style="position:relative;width:280px;height:120px">
    <img id="aliyunCaptcha-img" src="/bg.png" width="280" height="120" draggable="false" style="width:280px;height:120px;display:block">
    <img id="aliyunCaptcha-puzzle" src="/piece.png" width="40" height="40" draggable="false" style="position:absolute;left:0;top:40px;width:40px;height:40px">
  </div>
  <div id="aliyunCaptcha-sliding-body" style="position:relative;width:280px;height:40px;margin-top:8px;background:#eee">
    <span id="aliyunCaptcha-sliding-text">Please drag the slider to verify</span>
    <div id="aliyunCaptcha-sliding-slider" style="position:absolute;left:0;top:0;width:40px;height:40px;background:#333"></div>
  </div>
</div>
<script>
const slider = document.getElementById('aliyunCaptcha-sliding-slider')
const piece = document.getElementById('aliyunCaptcha-puzzle')
const gap = %d
let drag = null
slider.addEventListener('mousedown', (e) => {
  window.__down = {trusted: e.isTrusted, x: e.clientX}
  if (!e.isTrusted) return
  drag = {x: e.clientX, left: slider.offsetLeft}
})
window.addEventListener('mousemove', (e) => {
  if (!drag || !e.isTrusted) return
  const left = Math.max(0, drag.left + (e.clientX - drag.x))
  slider.style.left = left + 'px'
  piece.style.left = left + 'px'
})
window.addEventListener('mouseup', (e) => {
  if (!drag) return
  const left = piece.offsetLeft
  window.__up = {trusted: e.isTrusted, left: left}
  drag = null
  if (e.isTrusted && Math.abs(left - gap) <= 8) {
    document.getElementById('aliyunCaptcha-window-popup').remove()
    window.__passed = left
  }
})
</script>
</body></html>`, gap)
}

func startDebugChrome(t *testing.T, chrome, pageURL string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(chrome,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--force-device-scale-factor=1",
		"--window-size=1280,800",
		"--remote-allow-origins=*",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir="+t.TempDir(),
		"--remote-debugging-port="+strconv.Itoa(port),
		pageURL,
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(endpoint + "/json/version")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return endpoint
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Chrome 调试端口没有起来")
	return ""
}
