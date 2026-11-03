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

type tuchongRoundTrip func(*http.Request) (*http.Response, error)

func (fn tuchongRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func tuchongTestResponse(raw string, status int, contentType, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestTuchongRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, site, post string }{
		{"https://tuchong.com/123/456/", "123", "456"},
		{"https://www.tuchong.com/123/", "123", ""},
		{"https://tuchong.com/123/456/other", "", ""},
		{"https://tuchong.com/0/456/", "", ""},
		{"https://example.tuchong.com/123/456/", "", ""},
		{"https://tuchong.com.evil.test/123/456/", "", ""},
		{"https://user@tuchong.com/123/456/", "", ""},
		{"https://tuchong.com:8443/123/456/", "", ""},
	} {
		u, e := url.Parse(tc.raw)
		if e != nil {
			t.Fatal(e)
		}
		got := tuchongRouteOf(u)
		if got.site != tc.site || got.post != tc.post {
			t.Errorf("route %q=%+v", tc.raw, got)
		}
	}
	page, _ := url.Parse("https://tuchong.com/123/456/")
	for _, tc := range []struct{ raw, want string }{
		{"https://photo.tuchong.com/123/t/456.jpg?keep=1", "https://photo.tuchong.com/123/f/456.jpg?keep=1"},
		{"http://photo.tuchong.com/123/l/456.jpg", "https://photo.tuchong.com/123/f/456.jpg"},
		{"https://photo.tuchong.com/123/f/456.jpg", "https://photo.tuchong.com/123/f/456.jpg"},
		{"https://photo.tuchong.com.evil.test/123/f/456.jpg", ""},
		{"https://user@photo.tuchong.com/123/f/456.jpg", ""},
		{"https://photo.tuchong.com:8443/123/f/456.jpg", ""},
		{"https://photo.tuchong.com/123/../../private", ""},
		{"https://photo.tuchong.com/123/f/not-id.jpg", ""},
	} {
		got, ok := tuchongImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %q=%q %t", tc.raw, got, ok)
		}
	}
}

func TestTuchongPostImages(t *testing.T) {
	root, _ := url.Parse("https://tuchong.com/123/456/?private=source")
	body := `<html><head><title>作品标题</title></head><body><script>window.post={"post_id":"456","site_id":"123"};</script><article class="post-content"><img class="multi-photo-image" src="https://photo.tuchong.com/123/t/1.jpg?key=1"><img src="https://photo.tuchong.com/123/l/2.jpg"><img src="https://photo.tuchong.com/123/f/2.jpg"></article><img src="https://photo.tuchong.com/123/f/999.jpg"></body></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tuchongTestResponse(raw, 200, "text/html", body), nil
	})
	got, err := discoverTuchong(context.Background(), getter, root)
	if err != nil || got.Title != "作品标题" || len(got.Items) != 2 || got.Items[0].URL != "https://photo.tuchong.com/123/f/1.jpg?key=1" || got.Items[1].URL != "https://photo.tuchong.com/123/f/2.jpg" {
		t.Fatalf("post=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://tuchong.com/" || item.Headers["Cookie"] != "" || !item.RequireImage || item.Metadata["tuchong_post_id"] != "456" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestTuchongPostPartialAndSafety(t *testing.T) {
	root, _ := url.Parse("https://tuchong.com/123/456/")
	for _, mode := range []string{"invalid", "wrong-post", "wrong-site", "redirect", "empty", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				state := `{"post_id":"456","site_id":"123"}`
				if mode == "wrong-post" {
					state = `{"post_id":"999","site_id":"123"}`
				}
				if mode == "wrong-site" {
					state = `{"post_id":"456","site_id":"999"}`
				}
				images := `<img src="https://photo.tuchong.com/123/f/1.jpg">`
				if mode == "invalid" {
					images += `<img src="https://evil.test/123/f/2.jpg">`
				}
				if mode == "empty" {
					images = ""
				}
				if mode == "cancel" {
					cancel()
				}
				body := `<html><script>window.post=` + state + `;</script><article class="post-content">` + images + `</article></html>`
				r := tuchongTestResponse(raw, 200, "text/html", body)
				if mode == "redirect" {
					r.FinalURL = "https://attacker.test/123/456/"
				}
				return r, nil
			})
			got, err := discoverTuchong(ctx, getter, root)
			want := 0
			if mode == "invalid" {
				want = 1
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s items=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestTuchongAuthorPaginationAndPartial(t *testing.T) {
	root, _ := url.Parse("https://tuchong.com/123/")
	for _, mode := range []string{"complete", "detail-fail", "no-cursor", "repeated", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls []string
			getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				req, _ := http.NewRequest("GET", raw, nil)
				prepare(req)
				calls = append(calls, req.URL.Path+"?"+req.URL.RawQuery)
				if req.URL.Host != "tuchong.com" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
					t.Fatalf("unsafe %s %v", req.URL, req.Header)
				}
				if req.URL.Path == "/rest/2/sites/123/posts" {
					if req.URL.Query().Get("page") == "1" {
						before := `12345`
						if mode == "no-cursor" {
							before = `null`
						}
						return tuchongTestResponse(raw, 200, "application/json", fmt.Sprintf(`{"result":"SUCCESS","post_list":[{"post_id":"1"},{"post_id":"2"}],"more":true,"before_timestamp":%s}`, before)), nil
					}
					if req.URL.Query().Get("before_timestamp") != "12345" {
						t.Fatalf("lost pagination cursor: %s", req.URL)
					}
					if mode == "repeated" {
						return tuchongTestResponse(raw, 200, "application/json", `{"result":"SUCCESS","post_list":[{"post_id":"1"}],"more":true,"before_timestamp":12345}`), nil
					}
					return tuchongTestResponse(raw, 200, "application/json", `{"result":"SUCCESS","post_list":[{"post_id":"3"}],"more":false,"before_timestamp":12345}`), nil
				}
				parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
				id := parts[len(parts)-1]
				if id == "2" && mode == "detail-fail" {
					return tuchongTestResponse(raw, 404, "text/html", `not found`), nil
				}
				if id == "2" && mode == "cancel" {
					cancel()
				}
				body := fmt.Sprintf(`<html><title>work %s</title><script>window.post={"post_id":%q,"site_id":"123"};</script><article class="post-content"><img src="https://photo.tuchong.com/123/l/%s.jpg"></article></html>`, id, id, id)
				return tuchongTestResponse(raw, 200, "text/html", body), nil
			})
			got, err := discoverTuchong(ctx, getter, root)
			if mode == "complete" {
				if err != nil || len(got.Items) != 3 || got.Items[0].Metadata["album_id"] != "1" || got.Items[0].Metadata["album_title"] != "work 1" {
					t.Fatalf("got=%+v calls=%v err=%v", got, calls, err)
				}
			} else {
				want := 2
				if mode == "cancel" {
					want = 1
				}
				if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
					t.Fatalf("mode=%s got=%d calls=%v err=%v", mode, len(got.Items), calls, err)
				}
			}
		})
	}
}

func TestTuchongAuthorLimitReportsPartial(t *testing.T) {
	root, _ := url.Parse("https://tuchong.com/123/")
	var posts []string
	for i := 1; i <= maxTuchongPosts+1; i++ {
		posts = append(posts, fmt.Sprintf(`{"post_id":"%d"}`, i))
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Path == "/rest/2/sites/123/posts" {
			return tuchongTestResponse(raw, 200, "application/json", `{"result":"SUCCESS","post_list":[`+strings.Join(posts, ",")+`],"more":false}`), nil
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		id := parts[len(parts)-1]
		body := fmt.Sprintf(`<html><article class="post-content"><img src="https://photo.tuchong.com/123/f/%s.jpg"></article></html>`, id)
		return tuchongTestResponse(raw, 200, "text/html", body), nil
	})
	got, err := discoverTuchong(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "1000") || len(got.Items) != maxTuchongPosts {
		t.Fatalf("limit images=%d err=%v", len(got.Items), err)
	}
}

func TestTuchongRenderedCapture(t *testing.T) {
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
	const postURL = "https://tuchong.com/123/456/"
	const authorURL = "https://tuchong.com/123/"
	const imageURL = "https://photo.tuchong.com/123/f/1.jpg"
	const post = `<html><title>synthetic work</title><script>window.post={"post_id":"456","site_id":"123"};</script><article class="post-content"><img class="multi-photo-image" src="https://photo.tuchong.com/123/l/1.jpg"></article><img src="https://photo.tuchong.com/123/f/999.jpg"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && (paused.Request.URL == postURL || paused.Request.URL == authorURL) {
				body := post
				if paused.Request.URL == authorURL {
					body = `<html><title>synthetic author</title></html>`
				}
				action = fetch.FulfillRequest(paused.RequestID, 200).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
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
	session.start, _ = url.Parse(postURL)
	if err := session.run(ctx, camoufox.Navigate(postURL)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://tuchong.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || session.PageURL() != postURL || len(got.Items) != 1 || got.Items[0].URL != imageURL {
		t.Fatalf("post=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	type capturedRequest struct{ host, cookie, authorization string }
	var mu sync.Mutex
	var requests []capturedRequest
	session.getter.client.Transport = tuchongRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, capturedRequest{r.URL.Host, r.Header.Get("Cookie"), r.Header.Get("Authorization")})
		mu.Unlock()
		body, contentType := "image", "image/jpeg"
		if r.URL.Path == "/rest/2/sites/123/posts" {
			body, contentType = `{"result":"SUCCESS","post_list":[{"post_id":"456"}],"before_timestamp":12345,"more":false}`, "application/json"
		} else if r.URL.Host == "tuchong.com" {
			body, contentType = post, "text/html"
		}
		h := make(http.Header)
		h.Set("Content-Type", contentType)
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if err := session.run(ctx, camoufox.Navigate(authorURL)); err != nil {
		t.Fatal(err)
	}
	author, err := session.Capture(ctx)
	if err != nil || session.PageURL() != authorURL || len(author.Items) != 1 || author.Items[0].URL != imageURL {
		t.Fatalf("author=%+v page=%s err=%v", author, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, imageURL, func(r *http.Request) { r.Header.Set("Referer", author.Items[0].Headers["Referer"]) })
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 || requests[0].host != "tuchong.com" || requests[1].host != "tuchong.com" ||
		!strings.Contains(requests[0].cookie, "synthetic_session=example") || !strings.Contains(requests[1].cookie, "synthetic_session=example") ||
		requests[2].host != "photo.tuchong.com" || requests[2].cookie != "" || requests[2].authorization != "" {
		t.Fatalf("incorrect synthetic credential scope %+v", requests)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestTuchongLivePost(t *testing.T) {
	if os.Getenv("TULAI_TUCHONG_LIVE") != "1" {
		t.Skip("set TULAI_TUCHONG_LIVE=1 for public Tuchong checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root, _ := url.Parse("https://tuchong.com/16368360/63715405/")
	getter := NewHTTPGetter("")
	got, err := discoverTuchong(ctx, getter, root)
	if err != nil || len(got.Items) < 3 {
		t.Fatalf("post images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	err = New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	entries, readErr := os.ReadDir(dir)
	if err != nil || readErr != nil || len(entries) != 1 {
		t.Fatalf("download err=%v entries=%v read=%v", err, entries, readErr)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("download empty: %v %v", info, err)
	}
	t.Logf("public post images=%d", len(got.Items))
}

func TestTuchongLiveAuthor(t *testing.T) {
	if os.Getenv("TULAI_TUCHONG_LIVE") != "1" {
		t.Skip("set TULAI_TUCHONG_LIVE=1 for public Tuchong checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root, _ := url.Parse("https://tuchong.com/16368360/")
	got, err := discoverTuchong(ctx, NewHTTPGetter(""), root)
	if err != nil || len(got.Items) < 8 {
		t.Fatalf("author images=%d err=%v", len(got.Items), err)
	}
	t.Logf("public author images=%d", len(got.Items))
}

func TestTuchongLiveFeedPagination(t *testing.T) {
	if os.Getenv("TULAI_TUCHONG_LIVE") != "1" {
		t.Skip("set TULAI_TUCHONG_LIVE=1 for public Tuchong checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	origin, _ := url.Parse("https://tuchong.com/")
	getter := NewHTTPGetter("")
	first, err := tuchongFeed(ctx, getter, origin, "1107603", 1, "0")
	if err != nil || first.More == nil || !*first.More || len(first.PostList) == 0 {
		t.Fatalf("first page=%d more=%v err=%v", len(first.PostList), first.More, err)
	}
	second, err := tuchongFeed(ctx, getter, origin, "1107603", 2, tuchongRawID(first.Before))
	if err != nil || len(second.PostList) == 0 {
		t.Fatalf("second page=%d err=%v", len(second.PostList), err)
	}
	seen := map[string]bool{}
	for _, p := range first.PostList {
		seen[tuchongRawID(p.PostID)] = true
	}
	for _, p := range second.PostList {
		if seen[tuchongRawID(p.PostID)] {
			t.Fatal("public pages overlap")
		}
	}
	t.Logf("public author pages=%d,%d", len(first.PostList), len(second.PostList))
}
