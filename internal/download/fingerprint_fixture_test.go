package download

import (
	"bytes"
	"context"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/xxww0098/tulai/internal/fingerprinttest"
)

func dedicatedFixtureData(t *testing.T) string {
	t.Helper()
	if os.Getenv("TULAI_FINGERPRINT_TEST_DATA") == "" {
		t.Skip("set TULAI_FINGERPRINT_TEST_DATA to an isolated Fingerprint runtime")
	}
	return fingerprinttest.ConfigDir(t)
}

func openDedicatedFixture(t *testing.T, dataDir, profile, target string, handler DownloadRequest) (*RenderedSession, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), fingerprinttest.StartTimeout)
	defer cancel()
	return openRenderedWithAssistant(ctx, profile, target, "", true, dataDir, handler)
}

func renderedFixturePNGResponse(request *http.Request, picture []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}},
		Request: request, Body: io.NopCloser(bytes.NewReader(picture))}
}

func assertSavedRenderedPNG(t *testing.T, ctx context.Context, session *RenderedSession, item Item, picture []byte) {
	t.Helper()
	dir := t.TempDir()
	item.Filename = "sample.png"
	if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(dir, item.Filename))
	if err != nil || !bytes.Equal(saved, picture) {
		t.Fatalf("saved PNG differs from the response: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(saved))
	if err != nil || decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 {
		t.Fatalf("saved PNG could not be decoded: %v", err)
	}
}

// fixtureBrowser maps browser traffic to one local fixture, including TLS CONNECT tunnels.
func fixtureBrowser(t *testing.T, server *httptest.Server) (context.Context, context.CancelFunc, func()) {
	t.Helper()
	local, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	tlsHost := local.Host
	if local.Scheme == "http" {
		tlsServer := httptest.NewTLSServer(server.Config.Handler)
		t.Cleanup(tlsServer.Close)
		tlsURL, _ := url.Parse(tlsServer.URL)
		tlsHost = tlsURL.Host
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			upstream, err := net.Dial("tcp", tlsHost)
			if err != nil {
				http.Error(w, err.Error(), 502)
				return
			}
			client, buffer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				upstream.Close()
				return
			}
			_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			_ = buffer.Flush()
			go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(upstream, buffer) }()
			go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(client, upstream) }()
			return
		}
		req := r.Clone(r.Context())
		req.URL.Scheme = local.Scheme
		req.URL.Host = local.Host
		req.RequestURI = ""
		transport := server.Client().Transport
		resp, err := transport.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		defer resp.Body.Close()
		for k, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)
	return fingerprinttest.StartWithProxy(t, proxy.URL)
}
