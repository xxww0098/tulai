package download

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func rajceTestDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func rajceTestGetter() Getter {
	return GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected request")
	})
}

func rajceTestPage(album string) string {
	return `<title>fallback</title><script>var settings = {"album_name":"Trip","album_id":123,"album_url":{"path":"/alice/album/` + album + `"},"album_storage":"https://img44.rajce.idnes.cz/d44/123_abcd/","media_count":3}; var storage = "https://img44.rajce.idnes.cz/d44/123_abcd/";</script>
<script type="application/ld+json">[{"@type":"ImageObject","contentUrl":"https://img44.rajce.idnes.cz/d44/123_abcd/images/a.jpg","description":"Picture A"},{"@type":"ImageObject","contentUrl":"https://img44.rajce.idnes.cz/d44/123_abcd/images/b.png","name":"Picture B"},{"@type":"VideoObject","contentUrl":"https://img44.rajce.idnes.cz/d44/123_abcd/video/c.mp4"},{"@type":"MediaGallery","url":"https://www.rajce.idnes.cz/alice/album/` + album + `"}]</script>`
}

func TestRajceRoutesAndImageScope(t *testing.T) {
	for _, tc := range []struct {
		raw, owner, album, medium string
		legacy                    bool
	}{
		{"https://www.rajce.idnes.cz/alice/album/summer-trip", "alice", "summer-trip", "", false},
		{"https://rajce.net/alice/album/summer-trip/12345", "alice", "summer-trip", "12345", false},
		{"https://alice.rajce.idnes.cz/Summer_Trip/", "alice", "Summer_Trip", "", true},
		{"http://alice.rajce.net/Summer_Trip/", "alice", "Summer_Trip", "", true},
	} {
		u, _ := url.Parse(tc.raw)
		got := rajceAlbum(u)
		if got.owner != tc.owner || got.album != tc.album || got.medium != tc.medium || got.legacy != tc.legacy || !isRajceAlbum(u) {
			t.Errorf("route %q=%+v", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"https://alice.rajce.idnes.cz/", "https://www.rajce.idnes.cz/alice/album/",
		"https://www.rajce.idnes.cz/album/private-token", "https://www.rajce.idnes.cz/alice/album/summer/not-a-number",
		"https://user@www.rajce.idnes.cz/alice/album/summer", "https://www.rajce.idnes.cz:444/alice/album/summer",
		"https://www.rajce.idnes.cz.evil.invalid/alice/album/summer", "https://127.0.0.1/alice/album/summer",
		"https://alice.rajce.idnes.cz/a%2Fb", "https://alice.rajce.idnes.cz/a/../b",
	} {
		u, _ := url.Parse(raw)
		if isRajceAlbum(u) {
			t.Errorf("accepted route %q", raw)
		}
	}
	storage, ok := rajceStorage("https://img44.rajce.idnes.cz/d44/123_abcd/")
	if !ok {
		t.Fatal("valid storage rejected")
	}
	for _, tc := range []struct{ raw, want string }{
		{"https://img44.rajce.idnes.cz/d44/123_abcd/images/a.jpg?ver=2", "https://img44.rajce.idnes.cz/d44/123_abcd/images/a.jpg?ver=2"},
		{"images/a%20b.jpg", "https://img44.rajce.idnes.cz/d44/123_abcd/images/a%20b.jpg"},
		{"https://img45.rajce.idnes.cz/d44/123_abcd/images/a.jpg", ""},
		{"https://img44.rajce.idnes.cz/d44/other/images/a.jpg", ""},
		{"https://img44.rajce.idnes.cz/d44/123_abcd/thumb/a.jpg", ""},
		{"https://img44.rajce.idnes.cz:444/d44/123_abcd/images/a.jpg", ""},
		{"https://user@img44.rajce.idnes.cz/d44/123_abcd/images/a.jpg", ""},
		{"https://evil.invalid/d44/123_abcd/images/a.jpg", ""},
		{"https://img44.rajce.idnes.cz/d44/123_abcd/images/%2fsecret.jpg", ""},
		{"../other/images/a.jpg", ""},
	} {
		got, ok := rajceImageURL(storage, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %q=%q %v", tc.raw, got, ok)
		}
	}
	for _, raw := range []string{
		"https://evil.invalid/d44/", "https://img44.rajce.idnes.cz.evil.invalid/d44/", "https://user@img44.rajce.idnes.cz/d44/",
		"https://img44.rajce.idnes.cz:444/d44/", "https://img44.rajce.idnes.cz/d44/?token=1",
	} {
		if _, ok := rajceStorage(raw); ok {
			t.Errorf("accepted storage %q", raw)
		}
	}
}

func TestRajceProfileRoutes(t *testing.T) {
	for _, raw := range []string{"https://alice.rajce.idnes.cz/", "https://alice.rajce.net/?page=1", "https://imglover.rajce.idnes.cz/"} {
		page, _ := url.Parse(raw)
		if !isRajceProfile(page) || isRajceAlbum(page) {
			t.Errorf("profile not recognized %q", raw)
		}
	}
	for _, raw := range []string{
		"https://www.rajce.idnes.cz/", "https://alba.rajce.idnes.cz/", "https://img44.rajce.idnes.cz/",
		"https://download.rajce.idnes.cz/", "https://alice.rajce.idnes.cz/album/", "https://alice.rajce.idnes.cz:444/",
		"https://alice.rajce.idnes.cz.evil.invalid/", "https://user@alice.rajce.idnes.cz/",
	} {
		page, _ := url.Parse(raw)
		if isRajceProfile(page) {
			t.Errorf("accepted profile %q", raw)
		}
	}
	if err := rajceProfileUnsupported(); err == nil || !strings.Contains(err.Error(), "单个相册") {
		t.Errorf("profile warning=%v", err)
	}
}

func TestRajceAlbumJSONLDAndVideo(t *testing.T) {
	page, _ := url.Parse("https://www.rajce.idnes.cz/alice/album/trip")
	got, err := rajceCollect(context.Background(), rajceTestGetter(), page, rajceTestDoc(t, rajceTestPage("trip")))
	if err == nil || len(got.Items) != 2 || got.Title != "Trip" || !strings.Contains(err.Error(), "1 个视频") {
		t.Fatalf("album=%+v err=%v", got, err)
	}
	for i, expected := range []struct{ path, title string }{{"a.jpg", "Picture A"}, {"b.png", "Picture B"}} {
		item := got.Items[i]
		if item.URL != "https://img44.rajce.idnes.cz/d44/123_abcd/images/"+expected.path || item.Metadata["title"] != expected.title || item.Metadata["album_id"] != "123" || item.Headers["Referer"] != "https://www.rajce.idnes.cz/" || !item.RequireImage {
			t.Errorf("item=%+v", item)
		}
	}
}

func TestRajceLegacyAndPartial(t *testing.T) {
	page, _ := url.Parse("https://alice.rajce.idnes.cz/trip/")
	doc := rajceTestDoc(t, `<title>old</title><script>var storage = "https://img44.rajce.idnes.cz/d44/123_abcd/"; var photos = [{"fileName":"a b.jpg","desc":"old caption"},{"fileName":"../bad.jpg"}];</script>`)
	got, err := rajceCollect(context.Background(), rajceTestGetter(), page, doc)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != "https://img44.rajce.idnes.cz/d44/123_abcd/images/a%20b.jpg" || got.Items[0].Metadata["title"] != "old caption" || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("legacy=%+v err=%v", got, err)
	}
	canonical, _ := url.Parse("https://www.rajce.idnes.cz/alice/album/trip")
	for _, content := range []string{
		strings.Replace(rajceTestPage("trip"), `"media_count":3`, `"media_count":20`, 1),
		strings.Replace(rajceTestPage("trip"), `https://img44.rajce.idnes.cz/d44/123_abcd/images/a.jpg`, `https://evil.invalid/a.jpg`, 1),
	} {
		got, err := rajceCollect(context.Background(), rajceTestGetter(), canonical, rajceTestDoc(t, content))
		if err == nil || len(got.Items) == 0 || !strings.Contains(err.Error(), "不完整") {
			t.Errorf("partial=%+v err=%v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rajceCollect(ctx, rajceTestGetter(), page, doc); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled=%v", err)
	}
}

func TestRajceIdentityAndStorageMismatch(t *testing.T) {
	page, _ := url.Parse("https://www.rajce.idnes.cz/alice/album/trip")
	for _, source := range []string{
		strings.Replace(rajceTestPage("trip"), `"path":"/alice/album/trip"`, `"path":"/mallory/album/trip"`, 1),
		strings.Replace(rajceTestPage("trip"), `"url":"https://www.rajce.idnes.cz/alice/album/trip"`, `"url":"https://www.rajce.idnes.cz/alice/album/other"`, 1),
		strings.Replace(rajceTestPage("trip"), `var storage = "https://img44.rajce.idnes.cz/d44/123_abcd/"`, `var storage = "https://img45.rajce.idnes.cz/d45/other/"`, 1),
	} {
		got, err := rajceCollect(context.Background(), rajceTestGetter(), page, rajceTestDoc(t, source))
		if err == nil || len(got.Items) != 0 {
			t.Errorf("mismatch=%+v err=%v", got, err)
		}
	}
}

func TestRajceRedirectIdentity(t *testing.T) {
	page, _ := url.Parse("https://alice.rajce.net/Old_Trip/")
	for _, tc := range []struct {
		final string
		ok    bool
	}{
		{"https://www.rajce.idnes.cz/alice/album/trip", true},
		{"https://www.rajce.idnes.cz/mallory/album/trip", false},
		{"https://www.rajce.idnes.cz/login", false},
		{"http://www.rajce.idnes.cz/alice/album/trip", false},
	} {
		getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: tc.final, Body: io.NopCloser(strings.NewReader(rajceTestPage("trip")))}, nil
		})
		got, err := discoverRajceAlbum(context.Background(), getter, page)
		if tc.ok && (len(got.Items) != 2 || err == nil || !strings.Contains(err.Error(), "视频")) {
			t.Errorf("valid redirect=%+v err=%v", got, err)
		}
		if !tc.ok && (len(got.Items) != 0 || err == nil) {
			t.Errorf("invalid redirect=%+v err=%v", got, err)
		}
	}
}

func TestRajceRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	png := filterPNG(t, 1, 1)
	var imageRequests, badRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "img44.rajce.idnes.cz" {
			imageRequests.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "https://www.rajce.idnes.cz/" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host == "alice.rajce.idnes.cz" {
			http.Redirect(w, r, "https://www.rajce.idnes.cz/alice/album/trip", http.StatusFound)
			return
		}
		if r.Host != "www.rajce.idnes.cz" {
			badRequests.Add(1)
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/alice/album/trip", "/alice/album/other":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/", Secure: true})
			name := strings.TrimPrefix(r.URL.Path, "/alice/album/")
			body := strings.Replace(rajceTestPage(name), "/images/a.jpg", "/images/a.png", 1)
			_, _ = io.WriteString(w, body)
		case "/mallory/album/trip":
			body := strings.ReplaceAll(rajceTestPage("trip"), "/alice/album/trip", "/mallory/album/trip")
			_, _ = io.WriteString(w, body)
		case "/login":
			_, _ = io.WriteString(w, `<img src="https://img44.rajce.idnes.cz/login.png">`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://alice.rajce.idnes.cz/Old_Trip/")
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec -- synthetic local TLS server
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "www.rajce.idnes.cz:443" && addr != "img44.rajce.idnes.cz:443" {
			badRequests.Add(1)
			return nil, errors.New("blocked test destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	getter.client.Transport = transport
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String())); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "视频") {
		t.Fatalf("capture=%+v err=%v", got, err)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("download image: %v", err)
	}
	if imageRequests.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("image requests=%d bad=%d", imageRequests.Load(), badRequests.Load())
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.rajce.idnes.cz/alice/album/other")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "视频") {
		t.Fatalf("current album=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.rajce.idnes.cz/mallory/album/trip")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("different owner=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.rajce.idnes.cz/login")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 0 || badRequests.Load() != 0 {
		t.Fatalf("login=%+v bad=%d err=%v", got, badRequests.Load(), err)
	}
}
