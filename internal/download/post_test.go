package download

import (
	"context"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var _ Poster = (*HTTPGetter)(nil)
var _ Poster = (*RenderedSession)(nil)

func TestPostHTTPHeadersBodyAndProxy(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || r.URL.String() != "http://post-target.example/api" || string(body) != `{"page":2}` ||
			r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected POST: method=%s url=%s body=%q headers=%v", r.Method, r.URL, body, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer proxy.Close()
	getter := NewHTTPGetter(proxy.URL)
	result, err := getter.Post(context.Background(), "http://post-target.example/api", []byte(`{"page":2}`), http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(result.Body)
	_ = result.Body.Close()
	if err != nil || string(body) != `{"ok":true}` || result.StatusCode != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("POST response = %q status=%d calls=%d err=%v", body, result.StatusCode, calls.Load(), err)
	}
}

func TestPostRejectsUnsafeInputAndAcceptsBodyLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	getter := NewHTTPGetter("")
	for _, tc := range []struct {
		name, raw string
		body      []byte
		headers   http.Header
	}{
		{"userinfo", strings.Replace(server.URL, "http://", "http://user:pass@", 1), nil, nil},
		{"scheme", "file:///private", nil, nil},
		{"body limit", server.URL, make([]byte, maxPostBodyBytes+1), nil},
		{"cookie", server.URL, nil, http.Header{"Cookie": {"secret=value"}}},
		{"authorization", server.URL, nil, http.Header{"Authorization": {"secret"}}},
		{"origin override", server.URL, nil, http.Header{"Origin": {"https://another.test"}}},
		{"referer override", server.URL, nil, http.Header{"Referer": {"https://another.test"}}},
		{"unsupported type", server.URL, nil, http.Header{"Content-Type": {"text/plain"}}},
		{"duplicate type", server.URL, nil, http.Header{"Content-Type": {"application/json"}, "content-type": {"application/json"}}},
		{"header injection", server.URL, nil, http.Header{"Accept": {"a\r\nCookie: secret"}}},
		{"oversized header", server.URL, nil, http.Header{"Accept": {strings.Repeat("a", 8193)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := getter.Post(context.Background(), tc.raw, tc.body, tc.headers)
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("unsafe requests reached the server: %d", calls.Load())
	}
	resp, err := getter.Post(context.Background(), server.URL, make([]byte, maxPostBodyBytes), http.Header{"Content-Type": {"application/x-www-form-urlencoded; charset=UTF-8"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls.Load() != 1 {
		t.Fatalf("limit-sized request calls = %d", calls.Load())
	}
}

func TestPostRejectsRedirects(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Add(1)
	}))
	defer other.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", other.URL+"/redirected")
				w.WriteHeader(code)
			}))
			defer origin.Close()
			response, err := NewHTTPGetter("").Post(context.Background(), origin.URL, []byte("synthetic secret"), nil)
			if response != nil {
				response.Body.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "重定向") || leaked.Load() != 0 {
				t.Fatalf("redirect status %d: response=%v err=%v leaked=%d", code, response, err, leaked.Load())
			}
		})
	}
}

func TestPostCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		response, err := NewHTTPGetter("").Post(ctx, server.URL, nil, nil)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("POST was not received")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("POST did not stop on cancellation")
	}
}

func TestPostRenderedOriginCookiesAndRedirects(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var mu sync.Mutex
	var gotBody, gotCookie, gotOrigin, gotReferer, publicCookie string
	var otherCalls atomic.Int32
	var apiCalls atomic.Int32
	blockStarted := make(chan struct{}, 2)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherCalls.Add(1)
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shop":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/api"})
			_, _ = io.WriteString(w, "<html><title>synthetic shop</title></html>")
		case "/api/items":
			apiCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			gotBody, gotCookie, gotOrigin, gotReferer = string(body), r.Header.Get("Cookie"), r.Header.Get("Origin"), r.Header.Get("Referer")
			mu.Unlock()
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/api/redirect":
			w.Header().Set("Location", other.URL+"/received")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/api/block":
			blockStarted <- struct{}{}
			<-r.Context().Done()
		case "/api/stream":
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/public":
			mu.Lock()
			publicCookie = r.Header.Get("Cookie")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL+"/shop", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	resp, err := session.Post(ctx, server.URL+"/api/items", []byte(`{"page":1}`), http.Header{"Content-Type": {"application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	mu.Lock()
	observedBody, observedCookie, observedOrigin, observedReferer := gotBody, gotCookie, gotOrigin, gotReferer
	mu.Unlock()
	if err != nil || string(body) != `{"ok":true}` || observedBody != `{"page":1}` || !strings.Contains(observedCookie, "session=synthetic") || observedOrigin != server.URL || observedReferer != server.URL+"/shop" {
		t.Fatalf("same-origin response=%s body=%q cookie=%q origin=%q referer=%q err=%v", body, observedBody, observedCookie, observedOrigin, observedReferer, err)
	}
	if response, err := session.Post(ctx, other.URL+"/api/items", []byte("synthetic"), nil); err == nil || response != nil {
		t.Fatalf("cross-origin POST = %+v, %v", response, err)
	}
	if response, err := session.Post(ctx, server.URL+"/api/redirect", []byte("synthetic"), nil); err == nil || response != nil || otherCalls.Load() != 0 {
		t.Fatalf("redirect POST = %+v, %v; other calls=%d", response, err, otherCalls.Load())
	}
	if response, err := session.Post(ctx, server.URL+"/api/items", nil, http.Header{"Cookie": {"forged=synthetic"}}); err == nil || response != nil {
		t.Fatalf("forged cookie POST = %+v, %v", response, err)
	}
	public, err := session.Post(ctx, server.URL+"/public", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	public.Body.Close()
	mu.Lock()
	observedPublicCookie := publicCookie
	mu.Unlock()
	if strings.Contains(observedPublicCookie, "session=synthetic") {
		t.Fatalf("path-restricted cookie leaked: %q", observedPublicCookie)
	}
	// Simulate a public page address resolving to loopback for a subsequent request.
	session.mu.Lock()
	oldPageIP, oldStartIP := session.pageIP, session.startIP
	session.pageIP, session.startIP = netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("93.184.216.34")
	session.mu.Unlock()
	beforeCalls := apiCalls.Load()
	response, rebindErr := session.Post(ctx, server.URL+"/api/items", nil, nil)
	session.mu.Lock()
	session.pageIP, session.startIP = oldPageIP, oldStartIP
	session.mu.Unlock()
	if response != nil {
		response.Body.Close()
	}
	if rebindErr == nil || apiCalls.Load() != beforeCalls {
		t.Fatalf("unapproved local POST: response=%v err=%v calls=%d", response, rebindErr, apiCalls.Load()-beforeCalls)
	}
	runBlocked := func(requestCtx context.Context) <-chan error {
		done := make(chan error, 1)
		go func() {
			response, err := session.Post(requestCtx, server.URL+"/api/block", nil, nil)
			if response != nil {
				response.Body.Close()
			}
			done <- err
		}()
		select {
		case <-blockStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("browser POST was not received")
		}
		return done
	}
	postCtx, cancelPost := context.WithCancel(ctx)
	finished := runBlocked(postCtx)
	cancelPost()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("browser POST cancellation = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("browser POST did not stop after cancellation")
	}
	if err := session.run(ctx, camoufox.Navigate(other.URL)); err != nil {
		t.Fatal(err)
	}
	if response, err := session.Post(ctx, server.URL+"/api/items", nil, nil); err == nil || response != nil {
		t.Fatalf("stale captured origin POST = %+v, %v", response, err)
	}
	if err := session.run(ctx, camoufox.Navigate(server.URL+"/shop")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	stream, err := session.Post(context.Background(), server.URL+"/api/stream", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	streamDone := make(chan error, 1)
	go func() {
		_, readErr := io.ReadAll(stream.Body)
		stream.Body.Close()
		streamDone <- readErr
	}()
	session.Close()
	select {
	case err := <-streamDone:
		if err == nil {
			t.Fatal("browser POST succeeded after session close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("browser POST did not stop when session closed")
	}
}

func TestPostRenderedRejectsCrossOriginBeforeBrowser(t *testing.T) {
	page, _ := url.Parse("https://shop.example.test/index.html")
	tab, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &RenderedSession{tab: tab, pageURL: page, getter: NewHTTPGetter("")}
	for _, target := range []string{"https://other.example.test/api", "https://shop.example.test:444/api", "http://shop.example.test/api", "https://user@shop.example.test/api"} {
		response, err := session.Post(context.Background(), target, nil, nil)
		if err == nil || response != nil {
			t.Errorf("unsafe target %s accepted: %+v %v", target, response, err)
		}
	}
}

func TestPostHeaderSnapshot(t *testing.T) {
	headers := http.Header{"Content-Type": {"application/json"}}
	clean, err := postHeaders(headers)
	if err != nil {
		t.Fatal(err)
	}
	headers["Content-Type"][0] = "mutated"
	if clean.Get("Content-Type") != "application/json" {
		t.Fatalf("validated headers changed: %v", clean)
	}
}
