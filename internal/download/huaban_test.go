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

func huabanTestPin(id, boardID, key, mime string) map[string]any {
	pin := map[string]any{"pin_id": id, "file": map[string]string{"key": key, "type": mime}}
	if boardID != "" {
		pin["board_id"] = boardID
	}
	return pin
}

func huabanTestHTML(kind string, data any) string {
	body, _ := json.Marshal(data)
	return `<html><head><title>fallback</title><script>app.page["` + kind + `"] = ` + string(body) + `;</script></head><body><img src="https://unrelated.invalid/recommendation.jpg"></body></html>`
}

func huabanTestBoard(id string, count int, pins ...map[string]any) map[string]any {
	return map[string]any{"board_id": id, "title": "test board", "pin_count": count, "pins": pins}
}

func huabanTestResponse(raw, body, contentType string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestHuabanPageURLs(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		board   bool
	}{
		{"https://huaban.com/pins/5412219633", "5412219633", false},
		{"https://www.huaban.com/boards/17324249/", "17324249", true},
		{"https://huaban.com/boards/123?token=private", "123", true},
		{"https://huaban.com/boards/", "", false},
		{"https://huaban.com/boards/123/edit", "", false},
		{"https://huaban.com.evil.test/boards/123", "", false},
		{"https://user@huaban.com/boards/123", "", false},
		{"https://huaban.com:8443/boards/123", "", false},
		{"https://api.huaban.com/boards/123/pins", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := huabanPageRef(u)
		if got.id != tc.id || got.board != tc.board || isHuabanPage(u) != (tc.id != "") {
			t.Errorf("ref(%q) = %+v", tc.raw, got)
		}
	}
}

func TestHuabanSingleOriginalAndCover(t *testing.T) {
	root, _ := url.Parse("https://huaban.com/pins/123?token=private")
	key := "ac37e90c8cccd0accffa651ea52a82be7eaa680344949-hR0DBh"
	for _, tc := range []struct {
		name, body string
		count      int
		partial    bool
	}{
		{"pin", huabanTestHTML("pin", huabanTestPin("123", "", key, "image/jpeg")), 1, false},
		{"url state", huabanTestHTML("$url", map[string]any{"pin": huabanTestPin("123", "", key, "image/png")}), 1, false},
		{"wrong id", huabanTestHTML("pin", huabanTestPin("124", "", key, "image/jpeg")), 0, true},
		{"video", huabanTestHTML("pin", huabanTestPin("123", "", key, "video/mp4")), 0, true},
		{"unsafe key", huabanTestHTML("pin", huabanTestPin("123", "", "../secret", "image/jpeg")), 0, true},
		{"cover", `<meta property="og:image" content="https://gd-hbimg.huaban.com/` + key + `_fw658webp/format/webp">`, 1, true},
		{"forged cover", `<meta property="og:image" content="https://hbimg.huaban.com.evil.test/` + key + `">`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return huabanTestResponse(raw, tc.body, "text/html"), nil
			})
			got, err := discoverHuaban(context.Background(), getter, root)
			if len(got.Items) != tc.count || (err != nil) != tc.partial {
				t.Fatalf("got %+v, err %v", got.Items, err)
			}
			if len(got.Items) > 0 && (got.Items[0].URL != "https://hbimg.huaban.com/"+key || got.Items[0].Headers["Referer"] != "https://huaban.com/") {
				t.Fatalf("unsafe image or referer: %+v", got.Items[0])
			}
		})
	}
}

func TestHuabanBoardPagination(t *testing.T) {
	root, _ := url.Parse("https://huaban.com/boards/123?token=private")
	board := huabanTestBoard("123", 5,
		huabanTestPin("500", "123", "key-one", "image/jpeg"),
		huabanTestPin("490", "123", "key-two", "image/png"))
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() != "api.huaban.com" {
			return huabanTestResponse(raw, huabanTestHTML("board", board), "text/html"), nil
		}
		r, _ := http.NewRequest("GET", raw, nil)
		prepare(r)
		if u.Path != "/boards/123/pins" || u.Query().Get("limit") != "20" || r.Header.Get("Referer") != "https://huaban.com/" {
			t.Fatal("unexpected API request")
		}
		calls = append(calls, u.Query().Get("max"))
		data := map[string]any{"pins": []any{
			huabanTestPin("480", "123", "key-three", "image/jpeg"),
			huabanTestPin("470", "123", "key-three", "image/jpeg"),
			huabanTestPin("460", "123", "not-image", "video/mp4"),
		}}
		body, _ := json.Marshal(data)
		return huabanTestResponse(raw, string(body), "application/json"), nil
	})
	got, err := discoverHuaban(context.Background(), getter, root)
	if err != nil || strings.Join(calls, ",") != "490" || len(got.Items) != 3 || got.Items[2].URL != "https://hbimg.huaban.com/key-three" || got.Title != "test board" {
		t.Fatalf("board = %+v, calls %v, err %v", got.Items, calls, err)
	}
}

func TestHuabanBoardEmptyPageAndPartialFailure(t *testing.T) {
	root, _ := url.Parse("https://huaban.com/boards/123")
	for _, status := range []int{http.StatusOK, http.StatusMethodNotAllowed} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls []string
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Hostname() != "api.huaban.com" {
					return huabanTestResponse(raw, `<html><title>board</title></html>`, "text/html"), nil
				}
				calls = append(calls, u.Query().Get("max"))
				if len(calls) == 1 {
					body, _ := json.Marshal(map[string]any{"pins": []any{huabanTestPin("500", "123", "key-one", "image/jpeg")}})
					return huabanTestResponse(raw, string(body), "application/json"), nil
				}
				resp := huabanTestResponse(raw, `{"pins":[]}`, "application/json")
				resp.StatusCode = status
				return resp, nil
			})
			got, err := discoverHuaban(context.Background(), getter, root)
			if len(got.Items) != 1 || strings.Join(calls, ",") != ",500" || (err != nil) != (status != http.StatusOK) {
				t.Fatalf("partial = %+v, calls %v, err %v", got.Items, calls, err)
			}
		})
	}
}

func TestHuabanPartialInvalidKeyAndCursorCycle(t *testing.T) {
	root, _ := url.Parse("https://huaban.com/boards/123")
	var calls int
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() != "api.huaban.com" {
			return huabanTestResponse(raw, huabanTestHTML("board", huabanTestBoard("123", 3, huabanTestPin("500", "123", "key-one", "image/jpeg"))), "text/html"), nil
		}
		calls++
		data := map[string]any{"pins": []any{huabanTestPin("490", "123", "../bad", "image/jpeg"), huabanTestPin("480", "123", "key-two", "image/png")}}
		body, _ := json.Marshal(data)
		return huabanTestResponse(raw, string(body), "application/json"), nil
	})
	got, err := discoverHuaban(context.Background(), getter, root)
	if err == nil || len(got.Items) != 2 || calls != 1 {
		t.Fatalf("invalid key result = %+v, calls %d, err %v", got.Items, calls, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() != "api.huaban.com" {
			return huabanTestResponse(raw, huabanTestHTML("board", huabanTestBoard("123", 3, huabanTestPin("500", "123", "key-one", "image/jpeg"))), "text/html"), nil
		}
		data := map[string]any{"pins": []any{huabanTestPin("500", "123", "key-one", "image/jpeg")}}
		body, _ := json.Marshal(data)
		return huabanTestResponse(raw, string(body), "application/json"), nil
	})
	got, err = discoverHuaban(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 || !strings.Contains(err.Error(), "游标") {
		t.Fatalf("repeated cursor = %+v, err %v", got.Items, err)
	}
}

func TestHuabanCancellationRedirectAndBounds(t *testing.T) {
	root, _ := url.Parse("https://huaban.com/boards/123")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverHuaban(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	for _, kind := range []string{"page redirect", "api redirect", "wrong board", "toolarge", "length", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Hostname() != "api.huaban.com" {
					resp := huabanTestResponse(raw, huabanTestHTML("board", huabanTestBoard("123", 2, huabanTestPin("500", "123", "key-one", "image/jpeg"))), "text/html")
					if kind == "page redirect" {
						resp.FinalURL = "https://evil.test/boards/123"
					}
					return resp, nil
				}
				body := `{"board":{"board_id":"999"},"pins":[]}`
				resp := huabanTestResponse(raw, body, "application/json")
				switch kind {
				case "api redirect":
					resp.FinalURL = "https://evil.test/boards/123/pins?limit=20&max=500"
				case "toolarge":
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxPageBytes+1)))
				case "length":
					resp.Header.Set("Content-Length", "999")
				case "cancel":
					cancel()
					return nil, context.Canceled
				}
				return resp, nil
			})
			got, err := discoverHuaban(ctx, getter, root)
			want := 1
			if kind == "page redirect" {
				want = 0
			}
			if err == nil || len(got.Items) != want {
				t.Fatalf("result = %+v, err %v", got.Items, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel = %v", err)
			}
		})
	}
}

func TestHuabanRenderedCapture(t *testing.T) {
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
	const target = "https://huaban.com/boards/123?token=synthetic"
	page := huabanTestHTML("board", huabanTestBoard("123", 2, huabanTestPin("500", "123", "key-one", "image/jpeg")))
	var apiCalls atomic.Int32
	session.getter.client.Transport = huabanTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.huaban.com" || r.URL.Query().Get("max") != "500" || r.Header.Get("Cookie") != "" {
			return nil, errors.New("unexpected API request or cookie leak")
		}
		apiCalls.Add(1)
		body, _ := json.Marshal(map[string]any{"pins": []any{huabanTestPin("490", "123", "key-two", "image/png")}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Request: r, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://huaban.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || apiCalls.Load() != 1 || got.Items[0].URL != "https://hbimg.huaban.com/key-one" || got.Items[1].URL != "https://hbimg.huaban.com/key-two" {
		t.Fatalf("capture = %+v, API calls %d, err %v", got.Items, apiCalls.Load(), err)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://huaban.com/" {
			t.Fatalf("referer = %q", item.Headers["Referer"])
		}
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

type huabanTestTransport func(*http.Request) (*http.Response, error)

func (f huabanTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
