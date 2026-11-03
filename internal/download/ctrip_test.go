package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type ctripFixtureGetter struct {
	t     *testing.T
	page  string
	posts int
}

func (g *ctripFixtureGetter) Get(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
	return tiebaTestResponse(raw, http.StatusOK, g.page), nil
}

func (g *ctripFixtureGetter) Post(_ context.Context, raw string, body []byte, headers http.Header) (*Response, error) {
	g.posts++
	var request map[string]any
	if raw != ctripAlbumAPI || json.Unmarshal(body, &request) != nil || request["businessId"] != float64(123) ||
		request["pageIndex"] != float64(2) || request["viewState"] != "cursor" || headers.Get("Content-Type") != "application/json" {
		g.t.Fatalf("unexpected pagination request: %s %s", raw, body)
	}
	return tiebaTestResponse(raw, http.StatusOK, `{"result":0,"viewState":"cursor2","albumInfoList":[{"type":1,"pageIndex":2,"imageInfo":{"imageId":3,"imageUrl":"https://dimg04.c-ctrip.com/images/three.jpg"}}]}`), nil
}

func ctripTestHTML(id int, list string) string {
	return fmt.Sprintf(`<title>Album</title><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"initialState":{"photoListParam":{"businessId":%d,"pageIndex":1,"pageSize":2,"filterId":"online"},"photoCount":3,"viewState":"cursor","photoList":[%s]}}}}</script>`, id, list)
}

const ctripTestList = `{"type":1,"pageIndex":1,"imageInfo":{"imageId":1,"imageUrl":"https://dimg04.c-ctrip.com/images/one.jpg"}},{"type":1,"pageIndex":1,"imageInfo":{"imageId":2,"imageUrl":"https://dimg04.c-ctrip.com/images/two.jpg"}}`

func TestCtripAlbumPaginationAndIdentity(t *testing.T) {
	getter := &ctripFixtureGetter{t: t, page: ctripTestHTML(123, ctripTestList)}
	result, err := Discover(context.Background(), getter, "https://you.ctrip.com/photos/sight/city2/r123.html", 1)
	if err != nil || getter.posts != 1 || len(result.Items) != 3 || result.Items[2].Metadata["ctrip_image_id"] != "3" {
		t.Fatalf("posts=%d items=%+v err=%v", getter.posts, result.Items, err)
	}
	getter.page = ctripTestHTML(124, ctripTestList)
	result, err = Discover(context.Background(), getter, "https://you.ctrip.com/photos/sight/city2/r123.html", 1)
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("accepted wrong album: %+v %v", result.Items, err)
	}
}

func TestCtripRejectsForeignImagesAndUnsafeRoutes(t *testing.T) {
	if ctripImage("https://dimg04.c-ctrip.com.evil.test/images/a.jpg") != "" || ctripImage("https://dimg04.c-ctrip.com/images/../a.jpg") != "" {
		t.Fatal("accepted a foreign image")
	}
	for _, raw := range []string{"https://you.ctrip.com:444/photos/sight/city2/r123.html", "https://you.ctrip.com/photos/sight/city2/123.html", "https://you.ctrip.com.evil.test/photos/sight/city2/r123.html"} {
		u, _ := url.Parse(raw)
		if isCtripAlbum(u) {
			t.Fatalf("accepted %s", raw)
		}
	}
	getter := &ctripFixtureGetter{t: t, page: ctripTestHTML(123, strings.Replace(ctripTestList, "dimg04.c-ctrip.com/images/one", "evil.test/images/one", 1))}
	result, err := Discover(context.Background(), getter, "https://you.ctrip.com/photos/sight/city2/r123.html", 1)
	if err == nil || len(result.Items) != 2 {
		t.Fatalf("result=%+v err=%v", result.Items, err)
	}
}

func TestCtripRequiresPosterForRemainingPages(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, http.StatusOK, ctripTestHTML(123, ctripTestList)), nil
	})
	result, err := Discover(context.Background(), getter, "https://you.ctrip.com/photos/sight/city2/r123.html", 1)
	if err == nil || len(result.Items) != 2 || !strings.Contains(err.Error(), "分页") {
		t.Fatalf("%+v %v", result.Items, err)
	}
}

func TestCtripLive(t *testing.T) {
	if os.Getenv("TULAI_CTRIP_LIVE") != "1" {
		t.Skip("set TULAI_CTRIP_LIVE=1 for a public album")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	result, err := Discover(ctx, getter, "https://you.ctrip.com/photos/sight/shanghai2/r43803.html", 1)
	if err != nil || len(result.Items) < 40 {
		t.Fatalf("%d images: %v", len(result.Items), err)
	}
	item := result.Items[0]
	item.Filename = "ctrip"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal(errors.New("album image was not saved"))
	}
	f, err := os.Open(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || img.Width < 1 || img.Height < 1 {
		t.Fatalf("image: %v", err)
	}
	t.Logf("public album: %d unique images, first %dx%d %s", len(result.Items), img.Width, img.Height, format)
}

func TestCtripRenderedPaginationWithoutCrossOriginCookies(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	var calls, leaked atomic.Int32
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":0,"viewState":"cursor2","albumInfoList":[{"type":1,"pageIndex":2,"imageInfo":{"imageId":3,"imageUrl":"https://dimg04.c-ctrip.com/images/three.jpg"}}]}`)
	}))
	defer api.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/"})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, ctripTestHTML(123, ctripTestList))
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("http://you.ctrip.com/photos/sight/city2/r123.html")
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = api.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "m.ctrip.com:443" {
			return nil, errors.New("unexpected cross-origin target")
		}
		return (&net.Dialer{}).DialContext(ctx, network, api.Listener.Addr().String())
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
	result, err := session.Capture(ctx)
	if err != nil || len(result.Items) != 3 || calls.Load() != 1 || leaked.Load() != 0 {
		t.Fatalf("captured=%d API calls=%d leaked=%d err=%v", len(result.Items), calls.Load(), leaked.Load(), err)
	}
}
