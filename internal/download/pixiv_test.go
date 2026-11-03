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
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type pixivRoundTrip func(*http.Request) (*http.Response, error)

func (fn pixivRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func pixivTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPixivArtworkAndUserRoutes(t *testing.T) {
	for _, tc := range []struct {
		raw, artwork, user string
	}{
		{"https://www.pixiv.net/artworks/100412238", "100412238", ""},
		{"https://www.pixiv.net/en/artworks/100412238/", "100412238", ""},
		{"https://www.pixiv.net/zh-tw/artworks/100412238", "100412238", ""},
		{"https://www.pixiv.net/member_illust.php?mode=medium&illust_id=100412238", "100412238", ""},
		{"https://www.pixiv.net/users/123456", "", "123456"},
		{"https://www.pixiv.net/member.php?id=123456", "", "123456"},
		{"https://pixiv.net/artworks/123", "123", ""},
		{"https://www.pixiv.net.evil.test/artworks/123", "", ""},
		{"https://user@www.pixiv.net/artworks/123", "", ""},
		{"https://www.pixiv.net:8443/artworks/123", "", ""},
		{"https://www.pixiv.net/artworks/0", "", ""},
		{"https://www.pixiv.net/artworks/not-a-number", "", ""},
		{"https://www.pixiv.net/artworks/123/extra", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := pixivArtworkID(u); got != tc.artwork {
			t.Errorf("artwork %q = %q, want %q", tc.raw, got, tc.artwork)
		}
		if got := pixivUserID(u); got != tc.user {
			t.Errorf("user %q = %q, want %q", tc.raw, got, tc.user)
		}
	}
}

func TestPixivArtworkOriginalPagesAndHeaders(t *testing.T) {
	root, _ := url.Parse("https://www.pixiv.net/artworks/123?private=source")
	var paths []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		paths = append(paths, req.URL.Path)
		if req.URL.Host != "www.pixiv.net" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "https://www.pixiv.net/" {
			t.Fatalf("Pixiv API request = %s, headers=%v", req.URL, req.Header)
		}
		if strings.HasSuffix(req.URL.Path, "/pages") {
			return pixivTestResponse(raw, http.StatusOK, `{"error":false,"body":[
              {"urls":{"original":"https://i.pximg.net/img-original/img/a/123_p0.png?sign=ok","regular":"https://i.pximg.net/img-master/small.jpg"},"width":200,"height":300},
              {"urls":{"original":"https://i.pximg.net/img-original/img/b/123_p1.jpg"},"width":400,"height":500}]}`), nil
		}
		return pixivTestResponse(raw, http.StatusOK, `{"error":false,"body":{"id":"123","title":"示例作品","pageCount":2,"illustType":0}}`), nil
	})
	got, err := discoverPixivArtwork(context.Background(), getter, root)
	if err != nil || !got.IsPage || got.Title != "示例作品" || len(got.Items) != 2 || strings.Join(paths, ",") != "/ajax/illust/123/pages,/ajax/illust/123" {
		t.Fatalf("artwork=%+v, paths=%v, err=%v", got, paths, err)
	}
	if got.Items[0].URL != "https://i.pximg.net/img-original/img/a/123_p0.png?sign=ok" || got.Items[1].URL != "https://i.pximg.net/img-original/img/b/123_p1.jpg" {
		t.Fatalf("original pages = %+v", got.Items)
	}
	for i, item := range got.Items {
		if item.Headers["Referer"] != "https://www.pixiv.net/" || item.Headers["Cookie"] != "" || item.Headers["Authorization"] != "" ||
			item.Metadata["pixiv_id"] != "123" || item.Metadata["page"] != fmt.Sprint(i+1) {
			t.Fatalf("page metadata or headers = %+v", item)
		}
	}
}

func TestPixivArtworkKeepsPartialResults(t *testing.T) {
	root, _ := url.Parse("https://www.pixiv.net/artworks/123")
	for _, mode := range []string{"missing", "count", "id", "detail-status", "animation"} {
		t.Run(mode, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				if strings.HasSuffix(raw, "/pages") {
					second := "https://i.pximg.net/img-original/b.jpg"
					if mode == "missing" {
						second = "https://evil.test/private.jpg"
					}
					body := fmt.Sprintf(`{"error":false,"body":[{"urls":{"original":"https://i.pximg.net/img-original/a.jpg"}},{"urls":{"original":%q}}]}`, second)
					return pixivTestResponse(raw, 200, body), nil
				}
				if mode == "detail-status" {
					return pixivTestResponse(raw, 403, `{}`), nil
				}
				id, count := "123", 2
				if mode == "id" {
					id = "456"
				}
				if mode == "count" {
					count = 5
				}
				kind := 0
				if mode == "animation" {
					kind = 2
				}
				return pixivTestResponse(raw, 200, fmt.Sprintf(`{"error":false,"body":{"id":%q,"pageCount":%d,"illustType":%d}}`, id, count, kind)), nil
			})
			got, err := discoverPixivArtwork(context.Background(), getter, root)
			if err == nil || len(got.Items) == 0 || (mode == "missing" && len(got.Items) != 1) {
				t.Fatalf("partial mode=%s result=%+v err=%v", mode, got, err)
			}
		})
	}
}

func TestPixivAPIRejectsRedirectsInvalidResponseAndCancellation(t *testing.T) {
	root, _ := url.Parse("https://www.pixiv.net/artworks/123")
	for _, mode := range []string{"redirect", "status", "login", "html", "null", "oversize", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				body, status, final := `{"error":false,"body":[]}`, http.StatusOK, raw
				switch mode {
				case "redirect":
					final = "https://attacker.test/data"
				case "status":
					status = http.StatusForbidden
				case "login":
					body = `{"error":true,"body":null}`
				case "html":
					body = `<html>login</html>`
				case "null":
					body = `{"error":false,"body":null}`
				case "oversize":
					body = strings.Repeat("x", maxPageBytes+1)
				case "cancel":
					cancel()
				}
				response := pixivTestResponse(raw, status, body)
				response.FinalURL = final
				return response, nil
			})
			got, err := discoverPixivArtwork(ctx, getter, root)
			if err == nil || len(got.Items) != 0 || calls != 1 || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s result=%+v calls=%d err=%v", mode, got, calls, err)
			}
		})
	}
}

func TestPixivOriginalOnlyOfficialCDN(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://i.pximg.net/img-original/img/a.png?sign=kept", true},
		{"http://i.pximg.net/img-original/img/a.png", false},
		{"https://i.pximg.net/img-master/img/a.jpg", false},
		{"https://i.pximg.net.evil.test/img-original/img/a.png", false},
		{"https://user@i.pximg.net/img-original/img/a.png", false},
		{"https://i.pximg.net:8443/img-original/img/a.png", false},
		{"http://127.0.0.1/img-original/a.png", false},
	} {
		_, got := pixivOriginal(tc.raw)
		if got != tc.want {
			t.Errorf("original %q = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestPixivCanonicalOriginForHTTPGetter(t *testing.T) {
	for _, raw := range []string{"http://pixiv.net/artworks/123", "https://pixiv.net/artworks/123", "http://www.pixiv.net/artworks/123"} {
		root, _ := url.Parse(raw)
		getter := NewHTTPGetter("")
		calls := 0
		getter.client.Transport = pixivRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Scheme != "https" || r.URL.Host != "www.pixiv.net" || r.Header.Get("Cookie") != "" {
				t.Fatalf("canonical API request: %s, headers=%v", r.URL, r.Header)
			}
			body := `{"error":false,"body":{"id":"123","pageCount":1}}`
			if strings.HasSuffix(r.URL.Path, "/pages") {
				body = `{"error":false,"body":[{"urls":{"original":"https://i.pximg.net/img-original/a.jpg"}}]}`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		got, err := discoverPixivArtwork(context.Background(), getter, root)
		if err != nil || len(got.Items) != 1 || calls != 2 {
			t.Fatalf("root=%s got=%+v calls=%d err=%v", raw, got, calls, err)
		}
	}
}

func TestPixivUserWorksAndPartialResults(t *testing.T) {
	root, _ := url.Parse("https://www.pixiv.net/users/42?private=not-forwarded")
	for _, mode := range []string{"complete", "invalid", "failure", "empty", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var paths []string
			getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				req, _ := http.NewRequest("GET", raw, nil)
				prepare(req)
				if req.URL.Host != "www.pixiv.net" || req.URL.RawQuery != "" || req.Header.Get("Cookie") != "" {
					t.Fatalf("unexpected request: %s, %v", req.URL, req.Header)
				}
				paths = append(paths, req.URL.Path)
				if strings.HasSuffix(req.URL.Path, "/profile/all") {
					body := `{"error":false,"body":{"illusts":{"2":null,"1":null},"manga":{"2":null}}}`
					if mode == "invalid" {
						body = `{"error":false,"body":{"illusts":{"2":null,"invalid":null},"manga":[ ]}}`
					} else if mode == "empty" {
						body = `{"error":false,"body":{"illusts":{},"manga":null}}`
					}
					return pixivTestResponse(raw, 200, body), nil
				}
				id := strings.Split(req.URL.Path, "/")[3]
				if mode == "failure" && id == "1" {
					return pixivTestResponse(raw, 403, `{}`), nil
				}
				if mode == "cancel" && id == "1" {
					cancel()
				}
				if strings.HasSuffix(req.URL.Path, "/pages") {
					return pixivTestResponse(raw, 200, fmt.Sprintf(`{"error":false,"body":[{"urls":{"original":"https://i.pximg.net/img-original/%s.jpg"}}]}`, id)), nil
				}
				return pixivTestResponse(raw, 200, fmt.Sprintf(`{"error":false,"body":{"id":%q,"title":%q,"pageCount":1}}`, id, "work "+id)), nil
			})
			got, err := discoverPixivUser(ctx, getter, root)
			if mode == "complete" {
				if err != nil || len(got.Items) != 2 || got.Items[0].Metadata["album_id"] != "2" || got.Items[0].Metadata["album_title"] != "work 2" || len(paths) != 5 {
					t.Fatalf("complete got=%+v paths=%v err=%v", got, paths, err)
				}
			} else if err == nil || (mode == "empty" && len(got.Items) != 0) || (mode != "empty" && len(got.Items) != 1) || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s got=%+v paths=%v err=%v", mode, got, paths, err)
			}
		})
	}
}

func TestPixivUserWorkLimitReportsPartial(t *testing.T) {
	root, _ := url.Parse("https://www.pixiv.net/users/42")
	works := map[string]any{}
	for i := 1; i <= maxPixivWorks+1; i++ {
		works[fmt.Sprint(i)] = nil
	}
	profile, _ := json.Marshal(map[string]any{"error": false, "body": map[string]any{"illusts": works, "manga": []any{}}})
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if strings.HasSuffix(u.Path, "/profile/all") {
			return pixivTestResponse(raw, 200, string(profile)), nil
		}
		id := strings.Split(u.Path, "/")[3]
		if strings.HasSuffix(u.Path, "/pages") {
			return pixivTestResponse(raw, 200, fmt.Sprintf(`{"error":false,"body":[{"urls":{"original":"https://i.pximg.net/img-original/%s.jpg"}}]}`, id)), nil
		}
		return pixivTestResponse(raw, 200, fmt.Sprintf(`{"error":false,"body":{"id":%q,"pageCount":1}}`, id)), nil
	})
	got, err := discoverPixivUser(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "1000") || len(got.Items) != maxPixivWorks || got.Items[0].Metadata["pixiv_id"] != "1001" {
		t.Fatalf("limit images=%d first=%+v err=%v", len(got.Items), got.Items[0], err)
	}
}

func TestPixivRenderedCaptureCookieScope(t *testing.T) {
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
	const target = "https://www.pixiv.net/artworks/123"
	const imageURL = "https://i.pximg.net/img-original/test/123_p0.png"
	const page = `<html><title>synthetic Pixiv</title><img src="https://example.invalid/recommendation.jpg"></html>`
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
					WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.pixiv.net/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	type capturedRequest struct{ host, cookie, authorization string }
	var mu sync.Mutex
	var requests []capturedRequest
	session.getter.client.Transport = pixivRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, capturedRequest{r.URL.Host, r.Header.Get("Cookie"), r.Header.Get("Authorization")})
		mu.Unlock()
		body := "image"
		if strings.HasSuffix(r.URL.Path, "/pages") {
			body = `{"error":false,"body":[{"urls":{"original":"` + imageURL + `"}}]}`
		} else if r.URL.Path == "/ajax/illust/123" {
			body = `{"error":false,"body":{"id":"123","title":"synthetic work","pageCount":1}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != imageURL || session.PageURL() != target {
		t.Fatalf("capture=%+v url=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) {
		r.Header.Set("Referer", got.Items[0].Headers["Referer"])
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 || requests[0].host != "www.pixiv.net" || requests[1].host != "www.pixiv.net" ||
		!strings.Contains(requests[0].cookie, "synthetic_session=example") || !strings.Contains(requests[1].cookie, "synthetic_session=example") ||
		requests[2].host != "i.pximg.net" || requests[2].cookie != "" || requests[2].authorization != "" {
		t.Fatalf("incorrect synthetic credential scope: %+v", requests)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestPixivArtworkLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_PIXIV_LIVE") != "1" {
		t.Skip("set TULAI_PIXIV_LIVE=1 to verify a public Pixiv artwork")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse("https://www.pixiv.net/artworks/141260856")
	getter := NewHTTPGetter("")
	got, err := discoverPixivArtwork(ctx, getter, root)
	if err != nil || len(got.Items) != 4 {
		t.Fatalf("public artwork: images=%d, err=%v", len(got.Items), err)
	}
	dir := t.TempDir()
	item := got.Items[0]
	item.Filename, item.RequireImage = "image", true
	err = New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	entries, readErr := os.ReadDir(dir)
	if err != nil || readErr != nil || len(entries) != 1 {
		t.Fatalf("public image download: %v, entries=%v, read=%v", err, entries, readErr)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("downloaded image is empty: info=%v, err=%v", info, err)
	}
}
