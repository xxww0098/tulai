package download

import (
	"context"
	"encoding/base64"
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
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type artstationRoundTrip func(*http.Request) (*http.Response, error)

func (fn artstationRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func artstationTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestArtstationRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, kind, id, user, album string }{
		{"https://www.artstation.com/artwork/5XOvVA", "project", "5XOvVA", "", ""},
		{"https://xebelion.artstation.com/projects/kNKX4d", "project", "kNKX4d", "", ""},
		{"https://www.artstation.com/artist_name", "portfolio", "", "artist_name", "all"},
		{"https://www.artstation.com/artist_name/albums/123", "portfolio", "", "artist_name", "123"},
		{"https://artist-name.artstation.com/albums/123", "portfolio", "", "artist-name", "123"},
		{"https://artist-name.artstation.com/projects", "portfolio", "", "artist-name", "all"},
		{"https://artist-name.artstation.com/", "portfolio", "", "artist-name", "all"},
		{"https://www.artstation.com/artwork", "", "", "", ""},
		{"https://www.artstation.com/search", "", "", "", ""},
		{"https://magazine.artstation.com/something", "", "", "", ""},
		{"https://artstation.com.evil.test/artwork/123", "", "", "", ""},
		{"https://user@www.artstation.com/artwork/123", "", "", "", ""},
		{"https://www.artstation.com:8443/artwork/123", "", "", "", ""},
		{"https://www.artstation.com/artist/albums/0", "", "", "", ""},
		{"https://www.artstation.com/artist/collections/123", "", "", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := artstationRouteOf(u)
		if got.kind != tc.kind || got.project != tc.id || got.user != tc.user || got.album != tc.album {
			t.Errorf("route %q = %+v", tc.raw, got)
		}
	}
}

func TestArtstationProjectAssets(t *testing.T) {
	root, _ := url.Parse("https://www.artstation.com/artwork/Ab12")
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
		prepare(req)
		if req.URL.String() != "https://www.artstation.com/projects/Ab12.json" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Fatalf("project request: %s headers=%v", req.URL, req.Header)
		}
		return artstationTestResponse(raw, 200, `{"hash_id":"Ab12","title":"Work","assets":[
{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/images/1/large/one.jpg?token=kept"},
{"has_image":false,"image_url":"https://not-image.invalid/video"},
{"has_image":true,"image_url":"https://cdnb1.artstation.com/p/assets/images/images/2/large/two.png"}]}`), nil
	})
	got, err := discoverArtstation(context.Background(), getter, root)
	if err != nil || got.Title != "Work" || len(got.Items) != 2 || !strings.HasSuffix(got.Items[0].URL, "one.jpg?token=kept") || got.Items[1].Metadata["page"] != "3" {
		t.Fatalf("project=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Headers["Referer"] != "https://www.artstation.com/" || item.Headers["Cookie"] != "" || item.Metadata["artstation_project"] != "Ab12" {
			t.Fatalf("asset=%+v", item)
		}
	}
}

func TestArtstationImageRestriction(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"https://cdna.artstation.com/p/assets/images/images/1/large/a.jpg", true},
		{"https://cdn0.artstation.com/p/assets/images/images/1/large/a.jpg", true},
		{"https://cdnb1.artstation.com/p/assets/images/images/1/large/a.jpg", true},
		{"http://cdna.artstation.com/p/assets/images/images/1/large/a.jpg", false},
		{"https://cdna.artstation.com.evil.test/p/assets/images/images/1/large/a.jpg", false},
		{"https://user@cdna.artstation.com/p/assets/images/images/1/large/a.jpg", false},
		{"https://cdna.artstation.com:8443/p/assets/images/images/1/large/a.jpg", false},
		{"https://cdna.artstation.com/redirect?next=bad", false},
	} {
		_, ok := artstationImage(tc.raw)
		if ok != tc.ok {
			t.Errorf("image %q = %t, want %t", tc.raw, ok, tc.ok)
		}
	}
}

func TestArtstationProjectErrorsPreserveImages(t *testing.T) {
	root, _ := url.Parse("https://www.artstation.com/artwork/Ab12")
	for _, mode := range []string{"missing-flag", "bad-domain", "bad-project", "empty", "403", "redirect", "challenge", "oversize", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				status, final := 200, raw
				body := `{"hash_id":"Ab12","assets":[{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/one.jpg"},{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/two.jpg"}]}`
				switch mode {
				case "missing-flag":
					body = `{"hash_id":"Ab12","assets":[{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/one.jpg"},{}]}`
				case "bad-domain":
					body = `{"hash_id":"Ab12","assets":[{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/one.jpg"},{"has_image":true,"image_url":"https://evil.test/file"}]}`
				case "bad-project":
					body = `{"hash_id":"Wrong","assets":[]}`
				case "empty":
					body = `{"hash_id":"Ab12","assets":null}`
				case "403":
					status = 403
				case "redirect":
					final = "https://attacker.test/project.json"
				case "challenge":
					body = `<html>challenge</html>`
				case "oversize":
					body = strings.Repeat("x", maxPageBytes+1)
				case "cancel":
					cancel()
				}
				r := artstationTestResponse(raw, status, body)
				r.FinalURL = final
				return r, nil
			})
			got, err := discoverArtstation(ctx, getter, root)
			want := 0
			if mode == "missing-flag" || mode == "bad-domain" {
				want = 1
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestArtstationPortfolioPaginationAndPartial(t *testing.T) {
	root, _ := url.Parse("https://www.artstation.com/artist/albums/12")
	for _, mode := range []string{"complete", "project-404", "forbidden", "cancel", "repeated", "missing-total"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				req, _ := http.NewRequest("GET", raw, nil)
				prepare(req)
				calls = append(calls, req.URL.Path+"?"+req.URL.RawQuery)
				if req.URL.Scheme != "https" || req.URL.Host != "www.artstation.com" || req.Header.Get("Cookie") != "" {
					t.Fatalf("unsafe API request: %s", req.URL)
				}
				if req.URL.Path == "/users/artist/projects.json" {
					if req.URL.Query().Get("album_id") != "12" {
						t.Fatalf("album query: %s", req.URL)
					}
					if mode == "missing-total" {
						return artstationTestResponse(raw, 200, `{"data":[]}`), nil
					}
					if req.URL.Query().Get("page") == "1" || mode == "repeated" {
						return artstationTestResponse(raw, 200, `{"data":[{"hash_id":"Ab1"},{"hash_id":"Ab2"}],"total_count":3}`), nil
					}
					return artstationTestResponse(raw, 200, `{"data":[{"hash_id":"Ab3"}],"total_count":3}`), nil
				}
				id := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/projects/"), ".json")
				if id == "Ab2" {
					if mode == "project-404" {
						return artstationTestResponse(raw, 404, `{}`), nil
					}
					if mode == "forbidden" {
						return artstationTestResponse(raw, 403, `{}`), nil
					}
					if mode == "cancel" {
						cancel()
					}
				}
				return artstationTestResponse(raw, 200, fmt.Sprintf(`{"hash_id":%q,"title":%q,"assets":[{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/%s.jpg"}]}`, id, "Work "+id, id)), nil
			})
			got, err := discoverArtstation(ctx, getter, root)
			if mode == "complete" {
				if err != nil || len(got.Items) != 3 || got.Items[0].Metadata["album_id"] != "Ab1" || got.Items[0].Metadata["album_title"] != "Work Ab1" {
					t.Fatalf("got=%+v calls=%v err=%v", got, calls, err)
				}
			} else {
				want := 1
				switch mode {
				case "project-404", "repeated":
					want = 2
				case "missing-total":
					want = 0
				}
				if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
					t.Fatalf("mode=%s got=%d calls=%v err=%v", mode, len(got.Items), calls, err)
				}
				if mode == "forbidden" && len(calls) != 3 {
					t.Fatalf("forbidden did not stop: %v", calls)
				}
			}
		})
	}
}

func TestArtstationPortfolioLimitReportsPartial(t *testing.T) {
	root, _ := url.Parse("https://www.artstation.com/artist/albums/12")
	var entries []string
	for i := 1; i <= maxArtstationProjects+1; i++ {
		entries = append(entries, fmt.Sprintf(`{"hash_id":"A%d"}`, i))
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Path == "/users/artist/projects.json" {
			return artstationTestResponse(raw, 200, fmt.Sprintf(`{"data":[%s],"total_count":%d}`, strings.Join(entries, ","), len(entries))), nil
		}
		id := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/projects/"), ".json")
		return artstationTestResponse(raw, 200, fmt.Sprintf(`{"hash_id":%q,"assets":[{"has_image":true,"image_url":"https://cdna.artstation.com/p/assets/images/%s.jpg"}]}`, id, id)), nil
	})
	got, err := discoverArtstation(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "1000") || len(got.Items) != maxArtstationProjects {
		t.Fatalf("project limit images=%d err=%v", len(got.Items), err)
	}
}

func TestArtstationRenderedSubdomainCookieScope(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html><title>synthetic start</title></html>`)
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://artist-name.artstation.com/projects/Ab12"
	const imageURL = "https://cdna.artstation.com/p/assets/images/images/1/large/one.jpg"
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, 200).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><title>synthetic project</title></html>`)))
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
	session.start, _ = url.Parse(target)
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://artist-name.artstation.com/").WithDomain(".artstation.com").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var syntheticCookies []*network.Cookie
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		var err error
		syntheticCookies, err = network.GetCookies().WithURLs([]string{"https://www.artstation.com/"}).Do(ec)
		return err
	})); err != nil {
		t.Fatal(err)
	}
	foundSynthetic := false
	for _, cookie := range syntheticCookies {
		if cookie.Name == "synthetic_session" && cookie.Value == "example" {
			foundSynthetic = true
		}
	}
	if !foundSynthetic {
		t.Fatal("synthetic parent-domain cookie was not available to www.artstation.com")
	}
	type capturedRequest struct{ host, cookie, authorization string }
	var mu sync.Mutex
	var requests []capturedRequest
	session.getter.client.Transport = artstationRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, capturedRequest{r.URL.Host, r.Header.Get("Cookie"), r.Header.Get("Authorization")})
		mu.Unlock()
		body := "image"
		if r.URL.Host == "www.artstation.com" {
			body = `{"hash_id":"Ab12","title":"synthetic work","assets":[{"has_image":true,"image_url":"` + imageURL + `"}]}`
		}
		h := make(http.Header)
		h.Set("Content-Type", "application/json")
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != imageURL || session.PageURL() != target {
		t.Fatalf("capture=%+v url=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) { r.Header.Set("Referer", got.Items[0].Headers["Referer"]) })
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || requests[0].host != "www.artstation.com" || requests[0].cookie != "" || requests[0].authorization != "" ||
		requests[1].host != "cdna.artstation.com" || requests[1].cookie != "" || requests[1].authorization != "" {
		t.Fatalf("cross-origin synthetic credentials leaked: %+v", requests)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestArtstationPublicFeedAndImage(t *testing.T) {
	if os.Getenv("TULAI_ARTSTATION_LIVE") != "1" {
		t.Skip("set TULAI_ARTSTATION_LIVE=1 for public feed and CDN checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	var list artstationList
	if err := artstationJSON(ctx, getter, "/projects.json", url.Values{"page": {"1"}}, &list); err != nil || len(list.Data) == 0 || list.TotalCount == nil || *list.TotalCount < len(list.Data) {
		t.Fatalf("public feed count=%d err=%v", len(list.Data), err)
	}
	const publicImage = "https://cdna.artstation.com/p/assets/images/images/005/339/220/large/arnald-andujar-who-stole-the-candle-wax.jpg?1490307454"
	image, ok := artstationImage(publicImage)
	if !ok {
		t.Fatal("public CDN address rejected")
	}
	item := Item{URL: image, Filename: "image", RequireImage: true, Headers: map[string]string{"Referer": "https://www.artstation.com/"}}
	dir := t.TempDir()
	err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	entries, readErr := os.ReadDir(dir)
	if err != nil || readErr != nil || len(entries) != 1 {
		t.Fatalf("public image err=%v entries=%v read=%v", err, entries, readErr)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("public image empty: %v %v", info, err)
	}
}

func TestArtstationLiveProject(t *testing.T) {
	if os.Getenv("TULAI_ARTSTATION_LIVE_PROJECT") != "1" {
		t.Skip("set TULAI_ARTSTATION_LIVE_PROJECT=1 for public project endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, _ := url.Parse("https://www.artstation.com/artwork/5XOvVA")
	got, err := discoverArtstation(ctx, NewHTTPGetter(""), root)
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("public project images=%d err=%v", len(got.Items), err)
	}
}
