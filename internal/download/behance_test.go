package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

func behanceTestProject(id string) map[string]any {
	return map[string]any{"id": id, "name": "Project", "modules": []any{
		map[string]any{"type": "image", "sizes": map[string]string{"1400": "https://mir-s3-cdn-cf.behance.net/project_modules/1400/one.jpg", "original": "https://mir-s3-cdn-cf.behance.net/project_modules/source/one.jpg"}},
		map[string]any{"type": "text"},
		map[string]any{"type": "image", "imageSizes": []any{map[string]any{"url": "https://mir-s3-cdn-cf.behance.net/project_modules/disp/two.png", "width": 320}, map[string]any{"url": "https://mir-s3-cdn-cf.behance.net/project_modules/source/two.png", "width": 2000}}},
	}}
}

func behanceTestHTML(key string, value any) string {
	data, _ := json.Marshal(map[string]any{key: value})
	return `<html><head><title>Behance</title><script type="application/json">` + string(data) + `</script></head><body><img src="https://mir-s3-cdn-cf.behance.net/projects/original/recommend.jpg"></body></html>`
}

func behanceTestProfile() string {
	projects := []any{
		map[string]any{"id": 123, "url": "https://www.behance.net/gallery/123/a", "covers": map[string]any{"allAvailable": []any{map[string]any{"url": "https://mir-s3-cdn-cf.behance.net/projects/original/cover-one.jpg", "type": "JPG"}}}},
		map[string]any{"id": 124, "url": "https://www.behance.net/gallery/124/b", "covers": map[string]any{"allAvailable": []any{map[string]any{"url": "https://mir-s3-cdn-cf.behance.net/projects/404/cover-two.jpg", "width": 404}, map[string]any{"url": "https://mir-s3-cdn-cf.behance.net/projects/original/cover-two.jpg", "type": "JPG"}}}},
	}
	return behanceTestHTML("activeSection", map[string]any{"work": map[string]any{"user": map[string]any{"username": "designowner"}, "profileProjects": projects, "hasMore": true}})
}

func behanceTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestBehanceURLs(t *testing.T) {
	for _, tc := range []struct{ raw, project, profile string }{
		{"https://www.behance.net/gallery/123/My-Work", "123", ""},
		{"https://behance.net/gallery/123/", "123", ""},
		{"https://www.behance.net/designowner", "", "designowner"},
		{"https://www.behance.net/search", "", ""},
		{"https://www.behance.net/galleries", "", ""},
		{"https://www.behance.net/gallery/x/slug", "", ""},
		{"https://www.behance.net.evil.test/gallery/123/slug", "", ""},
		{"https://user@www.behance.net/gallery/123/slug", "", ""},
		{"https://www.behance.net:8443/gallery/123/slug", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if p, a := behanceProjectID(u), behanceProfileID(u); p != tc.project || a != tc.profile || isBehanceProject(u) != (p != "") || isBehanceProfile(u) != (a != "") {
			t.Errorf("ref(%q) = %q,%q", tc.raw, p, a)
		}
	}
}

func TestBehanceProjectImages(t *testing.T) {
	root, _ := url.Parse("https://www.behance.net/gallery/123/My-Work?token=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return behanceTestResponse(raw, behanceTestHTML("project", behanceTestProject("123"))), nil
	})
	got, err := discoverBehance(context.Background(), getter, root)
	if err != nil || got.Title != "Project" || len(got.Items) != 2 || got.Items[0].URL != "https://mir-s3-cdn-cf.behance.net/project_modules/source/one.jpg" || !strings.Contains(got.Items[1].URL, "/source/two.png") {
		t.Fatalf("project = %+v, %v", got.Items, err)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://www.behance.net/" {
			t.Fatalf("referer %q", item.Headers["Referer"])
		}
	}
}

func TestBehanceProfilePartialAndRestricted(t *testing.T) {
	root, _ := url.Parse("https://www.behance.net/designowner")
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Path == "/designowner" {
			return behanceTestResponse(raw, behanceTestProfile()), nil
		}
		calls = append(calls, u.Path)
		if strings.Contains(u.Path, "/123/") {
			return behanceTestResponse(raw, behanceTestHTML("project", behanceTestProject("123"))), nil
		}
		resp := behanceTestResponse(raw, "<html>blocked</html>")
		resp.StatusCode = 403
		return resp, nil
	})
	got, err := discoverBehance(context.Background(), getter, root)
	if err == nil || len(got.Items) != 3 || len(calls) != 2 || !strings.Contains(got.Items[2].URL, "/projects/original/cover-two.jpg") || !strings.Contains(err.Error(), "未加载") || !strings.Contains(err.Error(), "封面") {
		t.Fatalf("profile = %+v, calls %v, err %v", got.Items, calls, err)
	}
	calls = nil
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Path == "/designowner" {
			return behanceTestResponse(raw, behanceTestProfile()), nil
		}
		calls = append(calls, u.Path)
		resp := behanceTestResponse(raw, "<html>blocked</html>")
		resp.StatusCode = 403
		return resp, nil
	})
	got, err = discoverBehance(context.Background(), getter, root)
	if err == nil || len(got.Items) != 2 || len(calls) != 1 {
		t.Fatalf("blocked profile = %+v, calls %v, err %v", got.Items, calls, err)
	}
}

func TestBehanceBoundPreviewAndSafety(t *testing.T) {
	root, _ := url.Parse("https://www.behance.net/gallery/123/Work")
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"bound cover", `<meta property="og:url" content="https://www.behance.net/gallery/123/Work"><meta property="og:image" content="https://mir-s3-cdn-cf.behance.net/projects/original/cover.jpg">`, 1},
		{"unbound cover", `<meta property="og:image" content="https://mir-s3-cdn-cf.behance.net/projects/original/cover.jpg">`, 0},
		{"wrong project", behanceTestHTML("project", behanceTestProject("124")), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return behanceTestResponse(raw, tc.body), nil
			})
			got, err := discoverBehance(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("cover = %+v, %v", got.Items, err)
			}
		})
	}
	for _, raw := range []string{
		"https://mir-s3-cdn-cf.behance.net.evil.test/project_modules/source/x.jpg", "https://user@mir-s3-cdn-cf.behance.net/project_modules/source/x.jpg",
		"https://mir-s3-cdn-cf.behance.net:8443/project_modules/source/x.jpg", "http://mir-s3-cdn-cf.behance.net/project_modules/source/x.jpg",
		"https://127.0.0.1/project_modules/source/x.jpg", "https://mir-s3-cdn-cf.behance.net/avatar/x.jpg", "https://mir-s3-cdn-cf.behance.net/../project_modules/source/x.jpg",
	} {
		if _, ok := behanceImageURL(root, raw); ok {
			t.Errorf("unsafe image accepted: %s", raw)
		}
	}
}

func TestBehanceCancellationRedirectAndSize(t *testing.T) {
	root, _ := url.Parse("https://www.behance.net/gallery/123/Work")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverBehance(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	for _, kind := range []string{"redirect", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				resp := behanceTestResponse(raw, behanceTestHTML("project", behanceTestProject("123")))
				if kind == "redirect" {
					resp.FinalURL = "https://evil.test/gallery/123/Work"
				} else {
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxPageBytes+1)))
				}
				return resp, nil
			})
			got, err := discoverBehance(context.Background(), getter, root)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("unsafe response = %+v, %v", got.Items, err)
			}
		})
	}
}

func TestBehanceRenderedCapture(t *testing.T) {
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
	const target = "https://www.behance.net/designowner?token=synthetic"
	profile := behanceTestProfile()
	profile = strings.Replace(profile, `"hasMore":true`, `"hasMore":false`, 1)
	var calls atomic.Int32
	session.getter.client.Transport = behanceTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.behance.net" || !strings.HasPrefix(r.URL.Path, "/gallery/") || !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
			return nil, errors.New("unexpected project request or missing same-origin session")
		}
		calls.Add(1)
		id := behanceProjectID(r.URL)
		body := behanceTestHTML("project", behanceTestProject(id))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(profile)))
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.behance.net/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || calls.Load() != 2 {
		t.Fatalf("capture = %+v, calls %d, err %v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

type behanceTestTransport func(*http.Request) (*http.Response, error)

func (f behanceTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBehanceLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_BEHANCE_LIVE") != "1" {
		t.Skip("set TULAI_BEHANCE_LIVE=1 to check a public Behance profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, "https://www.behance.net/tlinh2002", 1)
	if len(got.Items) == 0 {
		t.Fatalf("public profile returned no images: %v", err)
	}
	item := got.Items[0]
	item.RequireImage = true
	if downloadErr := New(Options{Getter: getter}).Download(ctx, Job{Directory: t.TempDir()}, item, nil); downloadErr != nil {
		t.Fatal(fmt.Errorf("download public Behance image: %w", downloadErr))
	}
	t.Logf("public profile: %d images, one download; partial error: %v", len(got.Items), err)
}
