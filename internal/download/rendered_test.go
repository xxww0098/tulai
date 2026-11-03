package download

import (
	"context"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net"
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

	"github.com/chromedp/cdproto/network"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func TestRenderedSiteBoundary(t *testing.T) {
	for _, tc := range []struct {
		start, current string
		want           bool
	}{
		{"https://www.example.com/gallery", "https://m.example.com/post", true},
		{"https://example.com/", "http://example.com/", false},
		{"https://example.com/", "https://example.com.evil.test/", false},
		{"https://alice.github.io/", "https://bob.github.io/", false},
		{"http://127.0.0.1:8000/", "http://127.0.0.1:8001/", false},
		{"http://localhost:8000/", "http://localhost:8000/post", true},
		{"https://xhslink.com/o/example", "https://www.xiaohongshu.com/explore/676a35670000000013002578?xsec_token=test", true},
		{"http://xhslink.cn/o/example", "https://www.xiaohongshu.com/discovery/item/676a35670000000013002578", true},
		{"https://xhslink.com/o/example", "https://www.xiaohongshu.com/user/profile/676a35670000000013002578", false},
		{"https://xhslink.com/o/example", "https://xiaohongshu.com.evil.test/explore/676a35670000000013002578", false},
		{"https://xhslink.com:8443/o/example", "https://www.xiaohongshu.com/explore/676a35670000000013002578", false},
		{"https://twitter.com/user/status/1", "https://x.com/user/status/1", true},
		{"https://x.com/user/status/1", "https://mobile.twitter.com/user/status/1", true},
		{"https://twitter.com/user/status/1", "https://x.com.evil.test/user/status/1", false},
		{"https://twitter.com/user/status/1", "https://x.com:8443/user/status/1", false},
		{"https://500px.me/community/photo-details/abcd1234", "https://www.500px.com.cn/photo/AbCd12", true},
		{"https://500px.me/community/set/abcd1234", "https://500px.com.cn/gallery/AbCd12", true},
		{"https://500px.me/community/photo-details/abcd1234", "https://500px.com.cn.evil.test/photo/AbCd12", false},
		{"https://500px.me/community/photo-details/abcd1234", "https://500px.com.cn:8443/photo/AbCd12", false},
		{"https://www.pinterest.fr/pin/123456/", "https://fr.pinterest.com/pin/123456/", true},
		{"https://pinterest.de/owner/board/", "https://de.pinterest.com/owner/board/", true},
		{"https://pinterest.co.uk/pin/123456/", "https://uk.pinterest.com/pin/123456/", true},
		{"https://pinterest.fr/pin/123456/", "https://fr.pinterest.com/pin/654321/", false},
		{"https://pinterest.fr/pin/123456/", "https://fr.pinterest.com:8443/pin/123456/", false},
		{"https://pinterest.fr/pin/123456/", "https://fr.pinterest.com.evil.test/pin/123456/", false},
		{"https://alice.rajce.net/old-album", "https://www.rajce.idnes.cz/alice/album/new-album", true},
		{"https://alice.rajce.net/old-album", "https://www.rajce.idnes.cz/bob/album/new-album", false},
		{"https://alice.rajce.idnes.cz/old-album", "https://www.rajce.idnes.cz/bob/album/new-album", false},
		{"https://www.rajce.net/alice/album/first", "https://www.rajce.idnes.cz/alice/album/second", false},
		{"https://alice.rajce.net/old-album", "https://www.rajce.idnes.cz:8443/alice/album/new-album", false},
		{"https://www.yiwugou.com/product/detail/123.html", "https://www.yiwugo.com/product/detail/123.html", true},
		{"https://www.yiwugou.com/product/detail/123.html", "https://www.yiwugo.com/product/detail/124.html", false},
		{"https://www.yiwugou.com/product/detail/123.html", "https://www.yiwugo.com:8443/product/detail/123.html", false},
		{"https://www.yiwugou.com/product/detail/123.html", "https://www.yiwugo.com.evil.test/product/detail/123.html", false},
		{"https://www.archdaily.cn/cn/123/example", "https://www.archdaily.com/zh/123/example", true},
		{"https://www.archdaily.cn/cn/123/example", "https://www.archdaily.com/zh/124/example", false},
		{"https://www.archdaily.cn/cn/123/example", "https://www.archdaily.com:8443/zh/123/example", false},
		{"https://www.archdaily.cn/cn/123/example", "https://www.archdaily.com.evil.test/zh/123/example", false},
		{"https://www.kuaishou.com/short-video/3xabc", "https://m.gifshow.com/fw/photo/3xabc", true},
		{"https://www.kuaishou.com/short-video/3xabc", "https://m.gifshow.com/fw/photo/3xdef", false},
		{"https://www.kuaishou.com/short-video/3xabc", "https://m.gifshow.com:8443/fw/photo/3xabc", false},
		{"https://www.kuaishou.com/short-video/3xabc", "https://m.gifshow.com.evil.test/fw/photo/3xabc", false},
		{"https://www.douyin.com/note/7291234567890123456", "https://www.iesdouyin.com/share/note/7291234567890123456/", true},
		{"https://www.douyin.com/note/7291234567890123456", "https://www.iesdouyin.com/share/note/7291234567890123457/", false},
		{"https://www.douyin.com/note/7291234567890123456", "https://www.iesdouyin.com:8443/share/note/7291234567890123456/", false},
	} {
		start, _ := url.Parse(tc.start)
		current, _ := url.Parse(tc.current)
		if got := sameRenderedSite(start, current); got != tc.want {
			t.Errorf("sameRenderedSite(%q, %q) = %v, want %v", tc.start, tc.current, got, tc.want)
		}
	}
}

func TestGoogleVerificationPage(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://www.google.com/sorry/index", true},
		{"https://www.google.co.jp/sorry", true},
		{"https://www.google.com/search?q=images", false},
		{"https://google.com.evil.test/sorry/index", false},
	} {
		page, _ := url.Parse(tc.raw)
		if got := googleVerificationPage(page); got != tc.want {
			t.Errorf("googleVerificationPage(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestRenderedSnapshotNormalizesWeiboBeforeDeduplication(t *testing.T) {
	markup := `<img src="https://wx1.sinaimg.cn/orj360/a.jpg?x=1"><img src="https://wx1.sinaimg.cn/large/a.jpg?x=1">`
	for _, tc := range []struct {
		page, want string
		count      int
	}{
		{"https://weibo.com/post?token=private", "https://wx1.sinaimg.cn/large/a.jpg?x=1", 1},
		{"https://example.test/post", "https://wx1.sinaimg.cn/orj360/a.jpg?x=1", 2},
	} {
		page, _ := url.Parse(tc.page)
		var result Discovery
		err := addRenderedSnapshot(&result, map[string]bool{}, page, renderedSnapshot{Base: tc.page, HTML: []string{markup}})
		if err != nil || len(result.Items) != tc.count || result.Items[0].URL != tc.want {
			t.Fatalf("%s: items=%+v, err=%v", tc.page, result.Items, err)
		}
		if strings.Contains(result.Items[0].Headers["Referer"], "token=private") {
			t.Fatalf("cross-origin Referer leaked query: %+v", result.Items[0])
		}
	}
}

func TestRenderedCookieScope(t *testing.T) {
	for _, tc := range []struct {
		domain, path, target string
		want                 bool
	}{
		{"example.com", "/private", "https://example.com/private/a", true},
		{"example.com", "/private", "https://example.com/private2", false},
		{"example.com", "/", "https://sub.example.com/a", false},
		{".example.com", "/", "https://sub.example.com/a", true},
		{".example.com", "/", "https://example.com.evil.test/a", false},
	} {
		u, _ := url.Parse(tc.target)
		if got := renderedCookieMatches(&network.Cookie{Domain: tc.domain, Path: tc.path}, u); got != tc.want {
			t.Errorf("cookie %q %q for %q = %v, want %v", tc.domain, tc.path, tc.target, got, tc.want)
		}
	}
}

func TestRenderedProfileLockIsExclusiveAndPreservesFile(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, ".tulai-rendered.lock")
	if err := os.WriteFile(name, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err := acquireRenderedProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireRenderedProfile(dir); err == nil {
		second()
		t.Fatal("second session took the locked profile")
	}
	release()
	again, err := acquireRenderedProfile(dir)
	if err != nil {
		t.Fatalf("released lock could not be acquired: %v", err)
	}
	again()
	if data, err := os.ReadFile(name); err != nil || string(data) != "existing" {
		t.Fatalf("lock file was changed or removed: %q, %v", data, err)
	}
}

func TestOpenRenderedRejectsInvalidInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenRendered(ctx, t.TempDir(), "https://example.test", "", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled startup = %v", err)
	}
	if _, err := OpenRendered(context.Background(), "relative", "https://example.test", "", true); err == nil {
		t.Fatal("relative profile accepted")
	}
	if _, err := OpenRendered(context.Background(), t.TempDir(), "file:///tmp/private", "", true); err == nil {
		t.Fatal("non-HTTP page accepted")
	}
	if _, err := OpenRendered(context.Background(), t.TempDir(), "https://user:pass@example.test", "", true); err == nil {
		t.Fatal("URL credentials accepted")
	}
	if _, err := OpenRendered(context.Background(), t.TempDir(), "https://example.test", "http://127.0.0.1:7890", true); err == nil {
		t.Fatal("unsafe proxy accepted")
	}
}

func TestSafeDownloadDial(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"93.184.216.34", true}, {"2001:4860:4860::8888", true},
		{"127.0.0.1", false}, {"10.0.0.1", false}, {"169.254.169.254", false},
		{"100.64.0.1", false}, {"::ffff:127.0.0.1", false}, {"::1", false},
		{"fc00::1", false}, {"fe80::1", false}, {"64:ff9b::7f00:1", false},
	} {
		if got := isPublicDownloadIP(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("isPublicDownloadIP(%s) = %v", tc.addr, got)
		}
	}
	public, local := netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")
	answers := []netip.Addr{local, public}
	lookup := func(context.Context, string, string) ([]netip.Addr, error) { return answers, nil }
	var dialed []string
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		a, b := net.Pipe()
		_ = b.Close()
		return a, nil
	}
	conn, err := safeDownloadDialWith(context.Background(), "tcp", "image.example:443", lookup, dial)
	if err != nil || len(dialed) != 1 || dialed[0] != "93.184.216.34:443" {
		t.Fatalf("public hop: dialed=%v err=%v", dialed, err)
	}
	_ = conn.Close()
	dialed, answers = nil, []netip.Addr{local}
	if _, err := safeDownloadDialWith(context.Background(), "tcp", "image.example:443", lookup, dial); err == nil || len(dialed) != 0 {
		t.Fatalf("DNS rebind reached loopback: dialed=%v err=%v", dialed, err)
	}
	ctx := context.WithValue(context.Background(), safeDownloadPolicyKey{}, safeDownloadPolicy{allowedLocal: local})
	conn, err = safeDownloadDialWith(ctx, "tcp", "localhost:8080", lookup, dial)
	if err != nil || len(dialed) != 1 || dialed[0] != "127.0.0.1:8080" {
		t.Fatalf("explicit local hop: dialed=%v err=%v", dialed, err)
	}
	_ = conn.Close()
	answers, dialed = []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	if _, err := safeDownloadDialWith(ctx, "tcp", "localhost:8080", lookup, dial); err == nil || len(dialed) != 0 {
		t.Fatalf("different private address accepted: dialed=%v err=%v", dialed, err)
	}
}

func TestRenderedBrowserIntegration(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	var crossCookie, redirectCookie, sourceCookie, privateCookie, rangeHeader, ifRangeHeader, crossReferer string
	var mu sync.Mutex
	var privateHits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.URL.Path == "/redirected" {
			redirectCookie = r.Header.Get("Cookie")
		} else {
			crossCookie, crossReferer = r.Header.Get("Cookie"), r.Header.Get("Referer")
		}
		mu.Unlock()
		_, _ = io.WriteString(w, "other")
	}))
	defer other.Close()
	streamStarted := make(chan struct{}, 1)
	var source *httptest.Server
	source = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/", HttpOnly: true, MaxAge: 3600})
			http.SetCookie(w, &http.Cookie{Name: "private", Value: "synthetic", Path: "/private", HttpOnly: true, MaxAge: 3600})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<!doctype html><title>动态相册</title><script>
setTimeout(() => {const img=document.createElement('img');img.src='/dynamic.png';document.body.appendChild(img);
const host=document.createElement('div');document.body.appendChild(host);host.attachShadow({mode:'open'}).innerHTML='<img src="/shadow.png">';
document.body.style.backgroundImage="url('/background.png')";}, 20);</script>`)
		case "/private/image":
			privateHits.Add(1)
			mu.Lock()
			sourceCookie, privateCookie = r.Header.Get("Cookie"), r.Header.Get("Cookie")
			rangeHeader, ifRangeHeader = r.Header.Get("Range"), r.Header.Get("If-Range")
			mu.Unlock()
			w.Header().Set("Content-Range", "bytes 2-4/5")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "345")
		case "/public/image":
			mu.Lock()
			privateCookie = r.Header.Get("Cookie")
			mu.Unlock()
			_, _ = io.WriteString(w, "public")
		case "/redirect":
			http.Redirect(w, r, other.URL+"/redirected", http.StatusFound)
		case "/public-redirect-local":
			http.Redirect(w, r, source.URL+"/private/image", http.StatusFound)
		case "/stream":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "x")
			w.(http.Flusher).Flush()
			streamStarted <- struct{}{}
			<-r.Context().Done()
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.WriteString(w, "image")
		}
	}))
	defer source.Close()
	profile := filepath.Join(t.TempDir(), "profile")
	session, err := OpenRendered(ctx, profile, source.URL+"/?token=private", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := OpenRendered(ctx, profile, source.URL, "", true); err == nil {
		t.Fatal("second browser acquired an active profile")
	}
	page, err := session.Capture(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "动态相册" || session.PageURL() != source.URL+"/?token=private" {
		t.Fatalf("captured page=%+v, url=%q", page, session.PageURL())
	}
	found := map[string]bool{}
	for _, item := range page.Items {
		found[item.URL] = true
	}
	for _, path := range []string{"/dynamic.png", "/shadow.png", "/background.png"} {
		if !found[source.URL+path] {
			t.Errorf("rendered image %s missing in %+v", path, page.Items)
		}
	}
	get := session.Getter()
	resp, err := get.Get(ctx, source.URL+"/private/image", func(r *http.Request) {
		r.Header.Set("Range", "bytes=2-")
		r.Header.Set("If-Range", `"validator"`)
	})
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	mu.Lock()
	gotSourceCookie, gotPrivateCookie, gotRange, gotIfRange := sourceCookie, privateCookie, rangeHeader, ifRangeHeader
	mu.Unlock()
	if readErr != nil || string(body) != "345" || resp.StatusCode != http.StatusPartialContent ||
		!strings.Contains(gotSourceCookie, "session=synthetic") || !strings.Contains(gotPrivateCookie, "private=synthetic") ||
		gotRange != "bytes=2-" || gotIfRange != `"validator"` {
		t.Fatalf("same-origin response=%q status=%d cookies=%q range=%q if-range=%q err=%v", body, resp.StatusCode, gotSourceCookie, gotRange, gotIfRange, readErr)
	}
	resp, err = get.Get(ctx, source.URL+"/public/image", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	mu.Lock()
	gotPrivateCookie = privateCookie
	mu.Unlock()
	if strings.Contains(gotPrivateCookie, "private=synthetic") {
		t.Fatalf("path-restricted cookie leaked: %q", gotPrivateCookie)
	}
	resp, err = get.Get(ctx, other.URL+"/other", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	mu.Lock()
	gotCrossCookie, gotCrossReferer := crossCookie, crossReferer
	mu.Unlock()
	if gotCrossCookie != "" || gotCrossReferer != source.URL+"/" {
		t.Fatalf("cross-origin cookie=%q referer=%q", gotCrossCookie, gotCrossReferer)
	}
	resp, err = get.Get(ctx, source.URL+"/redirect", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	mu.Lock()
	gotRedirectCookie := redirectCookie
	mu.Unlock()
	if gotRedirectCookie != "" {
		t.Fatalf("redirect leaked cookie: %q", gotRedirectCookie)
	}

	// A page that was actually served by a public host cannot direct downloads
	// to loopback, either directly or by redirecting a public image URL.
	session.mu.Lock()
	savedPage, savedIP := session.pageURL, session.pageIP
	session.pageURL, _ = url.Parse("https://attacker.example/post")
	session.pageIP = netip.MustParseAddr("93.184.216.34")
	session.mu.Unlock()
	before := privateHits.Load()
	if got, err := get.Get(ctx, source.URL+"/private/image", nil); err == nil {
		_ = got.Body.Close()
		t.Fatal("public page accessed loopback directly")
	}
	sourceAddr, _ := url.Parse(source.URL)
	transport := session.getter.client.Transport.(*http.Transport)
	oldDial := transport.DialContext
	var publicDials atomic.Int32
	transport.DialContext = func(ctx context.Context, networkName, address string) (net.Conn, error) {
		lookup := func(ctx context.Context, networkName, host string) ([]netip.Addr, error) {
			if host == "public.test" {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}
			return net.DefaultResolver.LookupNetIP(ctx, networkName, host)
		}
		dial := func(ctx context.Context, networkName, approved string) (net.Conn, error) {
			if strings.HasPrefix(approved, "93.184.216.34:") {
				publicDials.Add(1)
				return (&net.Dialer{}).DialContext(ctx, networkName, sourceAddr.Host)
			}
			return nil, fmt.Errorf("unexpected destination %s", approved)
		}
		return safeDownloadDialWith(ctx, networkName, address, lookup, dial)
	}
	redirectURL := "http://public.test:" + sourceAddr.Port() + "/public-redirect-local"
	got, redirectErr := get.Get(ctx, redirectURL, nil)
	transport.DialContext = oldDial
	if redirectErr == nil {
		_ = got.Body.Close()
		t.Fatal("public page redirected to loopback")
	}
	if publicDials.Load() != 1 || privateHits.Load() != before {
		t.Fatalf("redirect reached private service: public dials=%d private hits=%d", publicDials.Load(), privateHits.Load()-before)
	}
	// Even if the same public hostname later resolves to loopback, it does not
	// inherit the permission granted to a page that was local at startup.
	session.mu.Lock()
	savedStart, savedStartIP := session.start, session.startIP
	session.start, _ = url.Parse("http://public.test:" + sourceAddr.Port() + "/post")
	session.startIP = netip.MustParseAddr("93.184.216.34")
	session.pageURL = session.start
	session.pageIP = netip.MustParseAddr("127.0.0.1")
	session.mu.Unlock()
	var rebindingDials atomic.Int32
	transport.DialContext = func(ctx context.Context, networkName, address string) (net.Conn, error) {
		lookup := func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		dial := func(ctx context.Context, networkName, address string) (net.Conn, error) {
			rebindingDials.Add(1)
			return (&net.Dialer{}).DialContext(ctx, networkName, address)
		}
		return safeDownloadDialWith(ctx, networkName, address, lookup, dial)
	}
	got, rebindErr := get.Get(ctx, "http://public.test:"+sourceAddr.Port()+"/private/image", nil)
	transport.DialContext = oldDial
	if rebindErr == nil {
		_ = got.Body.Close()
		t.Fatal("public page accessed loopback after DNS rebinding")
	}
	if rebindingDials.Load() != 0 || privateHits.Load() != before {
		t.Fatal("DNS rebinding reached private service")
	}
	session.mu.Lock()
	session.start, session.startIP = savedStart, savedStartIP
	session.pageURL, session.pageIP = savedPage, savedIP
	session.mu.Unlock()

	if err := session.run(ctx, camoufox.Navigate(other.URL)); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Capture(ctx); err == nil || session.PageURL() != "" {
		t.Fatalf("captured a different local site: %v, %q", err, session.PageURL())
	}
	if err := session.run(ctx, camoufox.Navigate(source.URL)); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	canceledCapture, cancelCapture := context.WithCancel(ctx)
	cancelCapture()
	if _, err := session.Capture(canceledCapture); !errors.Is(err, context.Canceled) || session.PageURL() != "" {
		t.Fatalf("canceled capture left a usable previous page: %v, %q", err, session.PageURL())
	}
	if _, err := session.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	independent, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "independent"), source.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close()
	downloadCtx, cancelDownload := context.WithCancel(ctx)
	resp, err = get.Get(downloadCtx, source.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-streamStarted
	cancelDone := make(chan error, 1)
	go func() { _, err := io.ReadAll(resp.Body); cancelDone <- err }()
	cancelDownload()
	select {
	case err := <-cancelDone:
		if err == nil {
			t.Fatal("canceling the task did not interrupt the transfer")
		}
	case <-ctx.Done():
		t.Fatal("transfer remained blocked after task cancellation")
	}
	_ = resp.Body.Close()
	resp, err = get.Get(ctx, source.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-streamStarted
	readDone := make(chan error, 1)
	go func() { _, err := io.ReadAll(resp.Body); readDone <- err }()
	session.Close()
	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("closing browser did not cancel active transfer")
		}
	case <-ctx.Done():
		t.Fatal("active transfer remained blocked after browser close")
	}
	_ = resp.Body.Close()
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("browser profile was removed: %v", err)
	}
	if _, err := independent.Capture(ctx); err != nil {
		t.Fatalf("closing one browser affected another: %v", err)
	}
	reopened, err := OpenRendered(ctx, profile, source.URL+"/public/image", "", true)
	if err != nil {
		t.Fatalf("reopening profile failed: %v", err)
	}
	defer reopened.Close()
	if _, err := reopened.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	resp, err = reopened.Get(ctx, source.URL+"/private/image", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	mu.Lock()
	gotSourceCookie = sourceCookie
	mu.Unlock()
	if !strings.Contains(gotSourceCookie, "session=synthetic") {
		t.Fatalf("persistent sign-in cookie was not retained: %q", gotSourceCookie)
	}
	if err := reopened.run(ctx, chromedp.ActionFunc(func(ec context.Context) error { return cdppage.Close().Do(ec) })); err != nil {
		t.Fatal(err)
	}
	manualCtx, manualCancel := context.WithTimeout(ctx, 5*time.Second)
	defer manualCancel()
	if _, err := reopened.Capture(manualCtx); err == nil {
		t.Fatal("manual browser close was not detected")
	}
	reopened.Close()
	again, err := OpenRendered(ctx, profile, source.URL+"/public/image", "", true)
	if err != nil {
		t.Fatalf("reopening profile after a manual close failed: %v", err)
	}
	again.Close()
}

func TestRenderedLocalHostnameIntegration(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<img src="/image.png">`)
	}))
	defer source.Close()
	serverURL, _ := url.Parse(source.URL)
	localURL := "http://localhost:" + serverURL.Port()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), localURL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	response, err := session.Get(ctx, localURL+"/image.png", nil)
	if err != nil {
		t.Fatalf("local hostname image: %v", err)
	}
	_ = response.Body.Close()
}
