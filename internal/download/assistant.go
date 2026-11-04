package download

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
)

// DownloadRequest handles a user-initiated request from the download assistant.
type DownloadRequest func(context.Context, *RenderedSession, string, string) error

var downloadRequestMarker = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

type downloadAssistant struct {
	server  *http.Server
	token   string
	handler DownloadRequest
	mu      sync.RWMutex
	session *RenderedSession
}

func startDownloadAssistant(profileDir string, handler DownloadRequest) (*downloadAssistant, string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, "", err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	assistant := &downloadAssistant{token: hex.EncodeToString(secret[:]), handler: handler}
	endpoint := "http://" + listener.Addr().String() + "/download"
	addon, err := extbundle.InstallDownloadAddon(profileDir, endpoint, assistant.token)
	if err != nil {
		listener.Close()
		return nil, "", err
	}
	assistant.server = &http.Server{Handler: http.HandlerFunc(assistant.serveHTTP), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = assistant.server.Serve(listener) }()
	return assistant, addon, nil
}

func (a *downloadAssistant) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/download" ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Tulai-Token")), []byte(a.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		URL    string `json:"url"`
		Marker string `json:"marker"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if decoder.Decode(&body) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	raw := strings.TrimSpace(body.URL)
	u, err := crawlInputURL(raw)
	if err != nil || u.User != nil || QzoneURLHasCredentials(raw) || !downloadRequestMarker.MatchString(body.Marker) {
		a.reply(w, errors.New("当前网址无效或包含登录令牌，请移除后重试"))
		return
	}
	a.mu.RLock()
	session := a.session
	a.mu.RUnlock()
	if session == nil {
		a.reply(w, errors.New("下载浏览器正在启动，请稍后重试"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	a.reply(w, a.handler(ctx, session, raw, body.Marker))
}

func (a *downloadAssistant) reply(w http.ResponseWriter, err error) {
	message := "已加入 tulai 下载列表"
	if err != nil {
		message = err.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": err == nil, "message": message})
}

func (a *downloadAssistant) close() {
	if a != nil {
		_ = a.server.Close()
	}
}
