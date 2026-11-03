package captcha

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufoxtest"
)

func TestSolveDisabled(t *testing.T) {
	t.Setenv("TULAI_BAXIA_SOLVE", "0")
	if err := SolveQianwenSlider(context.Background()); err != errBaxiaDisabled {
		t.Fatalf("err=%v", err)
	}
}

func TestSliderEndpointUsesExplicitAccount(t *testing.T) {
	t.Setenv("TULAI_CAMOUFOX_ACCOUNT", "9333")
	got := sliderEndpoints()
	if len(got) != 1 || got[0] != "camoufox://9333" {
		t.Fatal(got)
	}
}

func TestTrustedDragPassesPuzzleSlider(t *testing.T) {
	if testing.Short() {
		t.Skip("需要本机 Chrome")
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
	endpoint := startDebugCamoufox(t, server.URL)
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

func startDebugCamoufox(t *testing.T, pageURL string) string {
	t.Helper()
	_, endpoint := camoufoxtest.Start(t, pageURL)
	return endpoint
}
