package download

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func fiveHundredTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

const fiveHundredTestPhoto = `<html><head>
<meta property="og:url" content="https://500px.com/photo/1120003408/landscape">
<meta property="og:image" content="https://drscdn.500px.org/photo/1120003408/m=600/v2?sig=synthetic">
<meta property="og:title" content="Landscape">
</head><body>
<img fetchpriority="high" data-src="https://drscdn.500px.org/photo/9999999999/m=2048/v2?sig=synthetic">
<img fetchpriority="high" data-src="https://drscdn.500px.org/photo/1120003408/m=2048/v2?sig=synthetic">
<img src="https://drscdn.500px.org/photo/8888888888/m=600/v2?sig=synthetic">
</body></html>`

const fiveHundredTestGallery = `<html><head>
<meta property="og:url" content="https://500px.com/gallery/Gallery123">
<meta property="og:image" content="https://drscdn.500px.org/photo/999999/m=600/v2?sig=synthetic">
</head><body>
<div data-track-id="card_body" data-track-content-id="PhotoAAA" data-track-content-type="1"><img data-src="https://drscdn.500px.org/photo/111111/m=1024/v2?sig=synthetic"><img src="https://drscdn.500px.org/user/123/avatar.jpg"></div>
<div data-track-id="card_body" data-track-content-id="VideoAAA" data-track-content-type="2"><img data-src="https://drscdn.500px.org/photo/222222/m=1024/v2?sig=synthetic"></div>
<div data-track-id="card_body" data-track-content-id="PhotoBBB" data-track-content-type="1"><img data-src="https://drscdn.500px.org/photo/333333/m=1024/v2?sig=synthetic"></div>
<img data-src="https://drscdn.500px.org/photo/888888/m=1024/v2?sig=synthetic">
</body></html>`

func Test500pxURLs(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		gallery bool
	}{
		{"https://500px.com/photo/1120003408/landscape", "1120003408", false},
		{"https://www.500px.com/photo/7GwIdBU8L19", "7GwIdBU8L19", false},
		{"https://500px.com/gallery/Gallery123", "Gallery123", true},
		{"https://500px.com/gallery/Gallery123/extra", "", false},
		{"https://500px.com/user/PhotoAAA", "", false},
		{"https://500px.com.evil.test/photo/1120003408", "", false},
		{"https://user@500px.com/photo/1120003408", "", false},
		{"https://500px.com:8443/photo/1120003408", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		ref := fiveHundredPageRef(u)
		if ref.id != tc.id || ref.gallery != tc.gallery || is500pxPage(u) != (tc.id != "") {
			t.Errorf("ref(%q) = %+v", tc.raw, ref)
		}
	}
}

func Test500pxBoundPhoto(t *testing.T) {
	root, _ := url.Parse("https://500px.com/photo/1120003408/landscape?token=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return fiveHundredTestResponse(raw, fiveHundredTestPhoto), nil
	})
	got, err := discover500px(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 || got.Title != "Landscape" || !strings.Contains(got.Items[0].URL, "/photo/1120003408/m=2048/") || got.Items[0].Headers["Referer"] != "https://500px.com/" {
		t.Fatalf("photo = %+v, %v", got.Items, err)
	}
}

func Test500pxStructuredPhoto(t *testing.T) {
	root, _ := url.Parse("https://500px.com/photo/PhotoAAAA")
	body := `<script type="application/json">{"data":{"getPhotoById":{"id":"PhotoAAAA","title":"Photo","urls":{"size_600":"https://drscdn.500px.org/photo/111111/m=600/v2?sig=synthetic","size_4k":"https://drscdn.500px.org/photo/111111/m=4096/v2?sig=synthetic"}}}}</script>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return fiveHundredTestResponse(raw, body), nil
	})
	got, err := discover500px(context.Background(), getter, root)
	if err != nil || len(got.Items) != 1 || !strings.Contains(got.Items[0].URL, "/m=4096/") {
		t.Fatalf("structured = %+v, %v", got.Items, err)
	}
	body = strings.Replace(body, `"id":"PhotoAAAA"`, `"id":"OtherBBBB"`, 1)
	got, err = discover500px(context.Background(), getter, root)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong ID = %+v, %v", got.Items, err)
	}
}

func Test500pxGalleryLoadedCardsOnly(t *testing.T) {
	root, _ := url.Parse("https://500px.com/gallery/Gallery123")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return fiveHundredTestResponse(raw, fiveHundredTestGallery), nil
	})
	got, err := discover500px(context.Background(), getter, root)
	if err == nil || len(got.Items) != 2 || !strings.Contains(got.Items[0].URL, "/photo/111111/") || !strings.Contains(got.Items[1].URL, "/photo/333333/") || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("gallery = %+v, %v", got.Items, err)
	}
}

func Test500pxIdentityAndCDNBoundary(t *testing.T) {
	root, _ := url.Parse("https://500px.com/photo/1120003408/landscape")
	for _, tc := range []string{
		"https://drscdn.500px.org.evil.test/photo/1120003408/m=600/v2",
		"https://user@drscdn.500px.org/photo/1120003408/m=600/v2",
		"https://drscdn.500px.org:8443/photo/1120003408/m=600/v2",
		"http://drscdn.500px.org/photo/1120003408/m=600/v2",
		"https://127.0.0.1/photo/1120003408/m=600/v2",
		"https://drscdn.500px.org/user/1120003408/avatar.jpg",
		"https://drscdn.500px.org/../photo/1120003408/m=600/v2",
	} {
		if _, _, ok := fiveHundredImageURL(root, tc); ok {
			t.Errorf("unsafe image accepted: %q", tc)
		}
	}
	for _, body := range []string{
		strings.Replace(fiveHundredTestPhoto, `https://500px.com/photo/1120003408/landscape`, `https://500px.com/photo/9999999999/other`, 1),
		strings.Replace(fiveHundredTestPhoto, `/photo/1120003408/m=600/`, `/photo/9999999999/m=600/`, 1),
		`<meta property="og:image" content="https://drscdn.500px.org/photo/1120003408/m=600/v2">`,
	} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return fiveHundredTestResponse(raw, body), nil
		})
		got, err := discover500px(context.Background(), getter, root)
		if err == nil || len(got.Items) != 0 {
			t.Fatalf("unbound photo = %+v, %v", got.Items, err)
		}
	}
}

func Test500pxCancellationRedirectAndSize(t *testing.T) {
	root, _ := url.Parse("https://500px.com/photo/1120003408/landscape")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discover500px(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	for _, kind := range []string{"redirect", "oversize", "shell"} {
		t.Run(kind, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				resp := fiveHundredTestResponse(raw, fiveHundredTestPhoto)
				switch kind {
				case "redirect":
					resp.FinalURL = "https://evil.test/photo/1120003408/landscape"
				case "oversize":
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxPageBytes+1)))
				case "shell":
					resp = fiveHundredTestResponse(raw, "<html><div id='root'></div></html>")
				}
				return resp, nil
			})
			got, err := discover500px(context.Background(), getter, root)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("unsafe response = %+v, %v", got.Items, err)
			}
		})
	}
}

func Test500pxRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://500px.com/photo/1120003408/landscape?token=synthetic"
	var requests atomic.Int32
	session.getter.client.Transport = fiveHundredTestTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected HTTP request")
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(fiveHundredTestPhoto)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case responses <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(target)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://500px.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 1 || requests.Load() != 0 || !strings.Contains(got.Items[0].URL, "/photo/1120003408/m=2048/") {
		t.Fatalf("capture = %+v, requests %d, err %v", got.Items, requests.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

type fiveHundredTestTransport func(*http.Request) (*http.Response, error)

func (f fiveHundredTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
