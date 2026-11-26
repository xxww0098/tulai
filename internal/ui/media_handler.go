package ui

import (
	"bytes"
	"container/list"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/xxww0098/tulai/internal/imageedit"
)

// MediaScheme is the custom URL scheme registered with MyGo Protocol.Handle.
const MediaScheme = "tulaimedia"

const (
	mediaMaxTokens      = 16384
	mediaThumbCacheMax  = 64 << 20 // 64 MiB bounded in-memory thumbnail cache
	mediaThumbMaxWidth  = 2048
	mediaThumbPNGCutoff = 300 << 10 // keep transparent PNG thumbs <= 300 KiB; fallback to JPEG otherwise
)

// MediaOrigin returns the platform-aware base origin for the tulaimedia scheme.
// Windows WebView2 only intercepts http://<scheme>.localhost, while macOS WKWebView
// and Linux WebKitGTK intercept <scheme>://localhost natively.
func MediaOrigin() string {
	return mediaOriginForGOOS(runtime.GOOS)
}

func mediaOriginForGOOS(goos string) string {
	if goos == "windows" {
		return "http://" + MediaScheme + ".localhost"
	}
	return MediaScheme + "://localhost"
}

type thumbCacheKey struct {
	path  string
	mtime int64
	width int
}

type thumbCacheEntry struct {
	key         thumbCacheKey
	contentType string
	data        []byte
}

type thumbInflightCall struct {
	done        chan struct{}
	contentType string
	data        []byte
	rawFallback []byte
	err         error
	notFound    bool
}

// MediaHandler serves opaque-token local images (with optional on-the-fly thumbnail
// resizing) and zero-reencode live buffers (CDP drawer frames, image editor previews)
// over MyGo's custom protocol handler.
type MediaHandler struct {
	app    *App
	origin string

	tokenMu     sync.RWMutex
	pathToToken map[string]string
	tokenToPath map[string]string
	tokenOrder  []string

	thumbMu       sync.Mutex
	thumbLRU      *list.List
	thumbIndex    map[thumbCacheKey]*list.Element
	thumbInflight map[thumbCacheKey]*thumbInflightCall
	thumbBytes    int64
	decodeSem     chan struct{}
}

// NewMediaHandler constructs a MediaHandler bound to app.
func NewMediaHandler(app *App) *MediaHandler {
	workers := runtime.GOMAXPROCS(0)
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	return &MediaHandler{
		app:           app,
		origin:        MediaOrigin(),
		pathToToken:   make(map[string]string),
		tokenToPath:   make(map[string]string),
		thumbLRU:      list.New(),
		thumbIndex:    make(map[thumbCacheKey]*list.Element),
		thumbInflight: make(map[thumbCacheKey]*thumbInflightCall),
		decodeSem:     make(chan struct{}, workers),
	}
}

// Origin returns the scheme origin used to build URLs.
func (m *MediaHandler) Origin() string {
	if m == nil || m.origin == "" {
		return MediaOrigin()
	}
	return m.origin
}

// FileURL registers p in the opaque token table and returns a cache-busted media URL:
//
//	<origin>/file?t=<opaqueToken>&v=<mtimeUnixNano>
//
// The frontend may freely append `&w=<px>` for resized thumbnails (< 150 KiB) or omit
// `w` / pass `w=0` for the full-resolution original file.
func (m *MediaHandler) FileURL(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "data:") || strings.HasPrefix(p, MediaScheme+"://") {
		return p
	}
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	p = filepath.Clean(p)
	tok := m.registerToken(p)
	var mtime int64
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		mtime = st.ModTime().UnixNano()
	}
	if mtime > 0 {
		return m.Origin() + "/file?t=" + tok + "&v=" + strconv.FormatInt(mtime, 10)
	}
	return m.Origin() + "/file?t=" + tok
}

// DrawerFrameURL returns the versioned URL for the live CDP browser drawer frame.
func (m *MediaHandler) DrawerFrameURL(ver uint64) string {
	if ver == 0 {
		return ""
	}
	return fmt.Sprintf("%s/drawer/frame?v=%d", m.Origin(), ver)
}

// EditOrigURL returns the versioned URL for the image editor's selected source image.
func (m *MediaHandler) EditOrigURL(ver uint64) string {
	if ver == 0 {
		return ""
	}
	return fmt.Sprintf("%s/edit/orig?v=%d", m.Origin(), ver)
}

// EditPreviewURL returns the versioned URL for the image editor's processed JPEG preview.
func (m *MediaHandler) EditPreviewURL(ver uint64) string {
	if ver == 0 {
		return ""
	}
	return fmt.Sprintf("%s/edit/preview?v=%d", m.Origin(), ver)
}

// BrandIconURL returns the URL for the embedded application icon PNG.
func (m *MediaHandler) BrandIconURL() string {
	return m.Origin() + "/brand-icon"
}

func (m *MediaHandler) registerToken(cleanPath string) string {
	m.tokenMu.RLock()
	if tok, ok := m.pathToToken[cleanPath]; ok {
		m.tokenMu.RUnlock()
		return tok
	}
	m.tokenMu.RUnlock()

	var raw [16]byte
	_, _ = rand.Read(raw[:])
	tok := hex.EncodeToString(raw[:])

	m.tokenMu.Lock()
	defer m.tokenMu.Unlock()
	if existing, ok := m.pathToToken[cleanPath]; ok {
		return existing
	}
	if len(m.tokenOrder) >= mediaMaxTokens {
		evict := m.tokenOrder[0]
		m.tokenOrder = m.tokenOrder[1:]
		if oldPath, ok := m.tokenToPath[evict]; ok {
			delete(m.tokenToPath, evict)
			delete(m.pathToToken, oldPath)
		}
	}
	m.pathToToken[cleanPath] = tok
	m.tokenToPath[tok] = cleanPath
	m.tokenOrder = append(m.tokenOrder, tok)
	return tok
}

func (m *MediaHandler) lookupToken(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	m.tokenMu.RLock()
	p, ok := m.tokenToPath[tok]
	m.tokenMu.RUnlock()
	return p, ok
}

// ServeHTTP handles tulaimedia custom scheme requests.
func (m *MediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Allow cross-origin canvas/WebGL texture uploads across MyGo WebView origins
	// (WKWebView custom scheme, Windows http://tulaimedia.localhost, Vite dev server).
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/file":
		m.serveFile(w, r)
	case "/drawer/frame":
		m.serveDrawerFrame(w, r)
	case "/edit/orig":
		m.serveEditOrig(w, r)
	case "/edit/preview":
		m.serveEditPreview(w, r)
	case "/brand-icon":
		m.serveBrandIcon(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (m *MediaHandler) serveFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tok := strings.TrimSpace(q.Get("t"))
	absPath, ok := m.lookupToken(tok)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(absPath))
	if !isAllowedMediaExt(ext) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	st, err := os.Stat(absPath)
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	mtime := st.ModTime().UnixNano()
	vParam := strings.TrimSpace(q.Get("v"))
	cacheHeader := "private, max-age=60"
	if vParam != "" && vParam == strconv.FormatInt(mtime, 10) {
		cacheHeader = "private, max-age=31536000, immutable"
	}

	maxW, _ := strconv.Atoi(strings.TrimSpace(q.Get("w")))
	if maxW > mediaThumbMaxWidth {
		maxW = mediaThumbMaxWidth
	}
	if maxW > 0 && ext != ".svg" && ext != ".ico" {
		m.serveThumbnail(w, r, absPath, mtime, maxW, cacheHeader)
		return
	}

	// Full-resolution serving: transcode BMP if needed; serve standard web formats directly.
	if ext == ".bmp" {
		data, err := os.ReadFile(absPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		img, err := imageedit.Load(data)
		if err != nil {
			http.Error(w, "decode failed", http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			http.Error(w, "encode failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", cacheHeader)
		http.ServeContent(w, r, st.Name()+".png", st.ModTime(), bytes.NewReader(buf.Bytes()))
		return
	}

	f, err := os.Open(absPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	if ct := mediaContentType(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", cacheHeader)
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func (m *MediaHandler) serveThumbnail(w http.ResponseWriter, r *http.Request, absPath string, mtime int64, maxW int, cacheHeader string) {
	key := thumbCacheKey{path: absPath, mtime: mtime, width: maxW}
	if ct, data, ok := m.getThumbCache(key); ok {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", cacheHeader)
		http.ServeContent(w, r, filepath.Base(absPath), time.Unix(0, mtime), bytes.NewReader(data))
		return
	}

	// Singleflight: coalesce concurrent requests for the exact same thumbnail.
	m.thumbMu.Lock()
	if el, ok := m.thumbIndex[key]; ok {
		m.thumbLRU.MoveToFront(el)
		entry := el.Value.(*thumbCacheEntry)
		ct, data := entry.contentType, entry.data
		m.thumbMu.Unlock()
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", cacheHeader)
		http.ServeContent(w, r, filepath.Base(absPath), time.Unix(0, mtime), bytes.NewReader(data))
		return
	}
	if call, ok := m.thumbInflight[key]; ok {
		m.thumbMu.Unlock()
		select {
		case <-call.done:
		case <-r.Context().Done():
			return
		}
		m.writeThumbResult(w, r, absPath, mtime, cacheHeader, call)
		return
	}
	call := &thumbInflightCall{done: make(chan struct{})}
	m.thumbInflight[key] = call
	m.thumbMu.Unlock()

	defer func() {
		m.thumbMu.Lock()
		if m.thumbInflight[key] == call {
			delete(m.thumbInflight, key)
		}
		m.thumbMu.Unlock()
		close(call.done)
	}()

	select {
	case m.decodeSem <- struct{}{}:
		defer func() { <-m.decodeSem }()
	case <-r.Context().Done():
		call.err = r.Context().Err()
		return
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		call.notFound = true
		http.NotFound(w, r)
		return
	}
	src, err := imageedit.DecodeImage(raw)
	if err != nil {
		call.rawFallback = raw
		m.writeThumbResult(w, r, absPath, mtime, cacheHeader, call)
		return
	}

	ct, encoded, err := encodeThumbnail(src, maxW)
	if err != nil {
		call.err = err
		http.Error(w, "thumbnail encode failed", http.StatusInternalServerError)
		return
	}
	call.contentType = ct
	call.data = encoded
	m.putThumbCache(key, ct, encoded)
	m.writeThumbResult(w, r, absPath, mtime, cacheHeader, call)
}

func (m *MediaHandler) writeThumbResult(w http.ResponseWriter, r *http.Request, absPath string, mtime int64, cacheHeader string, call *thumbInflightCall) {
	if call.notFound {
		http.NotFound(w, r)
		return
	}
	if len(call.rawFallback) > 0 {
		ext := strings.ToLower(filepath.Ext(absPath))
		if ct := mediaContentType(ext); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Cache-Control", cacheHeader)
		http.ServeContent(w, r, filepath.Base(absPath), time.Unix(0, mtime), bytes.NewReader(call.rawFallback))
		return
	}
	if call.err != nil || len(call.data) == 0 {
		http.Error(w, "thumbnail encode failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", call.contentType)
	w.Header().Set("Cache-Control", cacheHeader)
	http.ServeContent(w, r, filepath.Base(absPath), time.Unix(0, mtime), bytes.NewReader(call.data))
}

func encodeThumbnail(src image.Image, maxW int) (string, []byte, error) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	var img image.Image = src
	if sw > maxW && maxW > 0 {
		dw := maxW
		dh := max(1, int(float64(sh)*float64(dw)/float64(sw)+0.5))
		dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
		xdraw.BiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
		img = dst
	}

	if hasTranslucentPixels(img) {
		var pngBuf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := enc.Encode(&pngBuf, img); err == nil && pngBuf.Len() <= mediaThumbPNGCutoff {
			return "image/png", pngBuf.Bytes(), nil
		}
		// Composite onto white before JPEG encoding so transparent regions don't turn black.
		img = flattenAlphaWhite(img)
	}

	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, img, &jpeg.Options{Quality: 82}); err != nil {
		return "", nil, err
	}
	return "image/jpeg", jpgBuf.Bytes(), nil
}

func hasTranslucentPixels(img image.Image) bool {
	if op, ok := img.(interface{ Opaque() bool }); ok && op.Opaque() {
		return false
	}
	switch typed := img.(type) {
	case *image.NRGBA:
		pix := typed.Pix
		step := 4
		if len(pix) > 40000 {
			step = 16
		}
		for i := 3; i < len(pix); i += step {
			if pix[i] < 250 {
				return true
			}
		}
		return false
	case *image.RGBA:
		pix := typed.Pix
		step := 4
		if len(pix) > 40000 {
			step = 16
		}
		for i := 3; i < len(pix); i += step {
			if pix[i] < 250 {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func flattenAlphaWhite(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	xdraw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, xdraw.Src)
	xdraw.Draw(dst, dst.Bounds(), img, b.Min, xdraw.Over)
	return dst
}

func (m *MediaHandler) getThumbCache(key thumbCacheKey) (string, []byte, bool) {
	m.thumbMu.Lock()
	defer m.thumbMu.Unlock()
	el, ok := m.thumbIndex[key]
	if !ok {
		return "", nil, false
	}
	m.thumbLRU.MoveToFront(el)
	entry := el.Value.(*thumbCacheEntry)
	return entry.contentType, entry.data, true
}

func (m *MediaHandler) putThumbCache(key thumbCacheKey, contentType string, data []byte) {
	size := int64(len(data))
	if size == 0 || size > mediaThumbCacheMax/4 {
		return
	}
	m.thumbMu.Lock()
	defer m.thumbMu.Unlock()
	if el, ok := m.thumbIndex[key]; ok {
		entry := el.Value.(*thumbCacheEntry)
		m.thumbBytes -= int64(len(entry.data))
		entry.contentType = contentType
		entry.data = data
		m.thumbBytes += size
		m.thumbLRU.MoveToFront(el)
	} else {
		el := m.thumbLRU.PushFront(&thumbCacheEntry{key: key, contentType: contentType, data: data})
		m.thumbIndex[key] = el
		m.thumbBytes += size
	}
	for m.thumbBytes > mediaThumbCacheMax && m.thumbLRU.Len() > 0 {
		back := m.thumbLRU.Back()
		entry := back.Value.(*thumbCacheEntry)
		m.thumbBytes -= int64(len(entry.data))
		delete(m.thumbIndex, entry.key)
		m.thumbLRU.Remove(back)
	}
}

func (m *MediaHandler) serveDrawerFrame(w http.ResponseWriter, r *http.Request) {
	if m.app == nil {
		http.NotFound(w, r)
		return
	}
	data := m.app.currentDrawerFrame()
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (m *MediaHandler) serveEditOrig(w http.ResponseWriter, r *http.Request) {
	if m.app == nil {
		http.NotFound(w, r)
		return
	}
	p := m.app.currentEditOrigPath()
	if p == "" {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(p))
	if !isAllowedMediaExt(ext) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if ext == ".bmp" {
		raw, err := os.ReadFile(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		img, err := imageedit.Load(raw)
		if err != nil {
			http.Error(w, "decode failed", http.StatusInternalServerError)
			return
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			http.Error(w, "encode failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(buf.Bytes())
		}
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	if ct := mediaContentType(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func (m *MediaHandler) serveEditPreview(w http.ResponseWriter, r *http.Request) {
	if m.app == nil {
		http.NotFound(w, r)
		return
	}
	data := m.app.currentEditPreviewJPEG()
	if len(data) == 0 {
		m.serveEditOrig(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (m *MediaHandler) serveBrandIcon(w http.ResponseWriter, r *http.Request) {
	data := getAppIconData()
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func isAllowedMediaExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".avif", ".svg", ".ico":
		return true
	default:
		return false
	}
}

func mediaContentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".avif":
		return "image/avif"
	case ".bmp":
		return "image/bmp"
	case ".ico":
		return "image/x-icon"
	default:
		return ""
	}
}
