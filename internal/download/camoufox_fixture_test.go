package download

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/xxww0098/tulai/internal/camoufoxtest"
)

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
	return camoufoxtest.StartWithProxy(t, proxy.URL)
}
