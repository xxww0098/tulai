package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
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

const pinterestSampleID = "1048072144516339083"
const pinterestThumb = "https://i.pinimg.com/736x/9b/14/70/9b1470ae1bac072eb38353f0484c6628.jpg"
const pinterestOriginal = "https://i.pinimg.com/originals/9b/14/70/9b1470ae1bac072eb38353f0484c6628.jpg"
const pinterestPinHTML = `<html><head>
<meta property="og:url" content="https://www.pinterest.com/pin/canon-camera-mirror-selfie--1048072144516339083/">
<meta property="og:image" content="` + pinterestThumb + `"><meta property="og:title" content="Photo">
</head><body><img src="https://i.pinimg.com/originals/00/11/22/not-the-pin.jpg"></body></html>`
const pinterestBoardHTML = `<html><head><title>Gallery</title></head><body>
<script type="application/json" id="__PWS_INITIAL_PROPS__">{"initialReduxState":{"resources":{
"BoardResource":{"one":{"data":{"id":"123456","url":"/owner/board/","name":"Board","pin_count":2,"section_count":0}}},
"BoardFeedResource":{"[[\"board_id\",\"123456\"]]":{"data":[{"id":"Pin1","type":"pin","images":{"orig":{"url":"https://i.pinimg.com/originals/aa/bb/cc/first.jpg","width":1000}}},{"type":"story","images":{"orig":{"url":"https://i.pinimg.com/originals/99/99/99/advert.jpg"}}}],"nextBookmark":"cursor1"},"[[\"board_id\",\"654321\"]]":{"data":[{"id":"other","type":"pin","images":{"orig":{"url":"https://i.pinimg.com/originals/99/99/99/other.jpg"}}}],"nextBookmark":"-end-"}}}}}</script>
<img src="https://i.pinimg.com/originals/99/99/99/extra.jpg"></body></html>`

func pinterestResponse(raw, body, contentType string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPinterestURLs(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{"https://www.pinterest.com/pin/" + pinterestSampleID + "/", true},
		{"https://in.pinterest.com/pin/canon-camera-mirror-selfie--" + pinterestSampleID + "/", true},
		{"https://ca.pinterest.com/tonii__a/mother-daughter/", true},
		{"https://pinterest.com/owner/board/", true},
		{"https://www.pinterest.fr/pin/" + pinterestSampleID, true},
		{"https://pinterest.co.uk/pin/" + pinterestSampleID, true},
		{"https://pinterest.de/owner/board/", true},
		{"https://evil.pinterest.com/owner/board/", false},
		{"https://pinterest.com.evil.test/pin/" + pinterestSampleID, false},
		{"https://user@www.pinterest.com/pin/" + pinterestSampleID, false},
		{"https://www.pinterest.com:8443/pin/" + pinterestSampleID, false},
		{"https://www.pinterest.com/pin/create/", false},
		{"https://www.pinterest.com/search/pins/", false},
		{"https://www.pinterest.com/owner/board/section/", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isPinterestPage(u) != tc.valid {
			t.Errorf("page %q: got %v", tc.raw, isPinterestPage(u))
		}
	}
}

func TestPinterestPinIdentityAndOriginal(t *testing.T) {
	root, _ := url.Parse("https://www.pinterest.com/pin/" + pinterestSampleID + "/?token=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return pinterestResponse(raw, pinterestPinHTML, "text/html"), nil
	})
	result, err := discoverPinterest(context.Background(), getter, root)
	if err != nil || len(result.Items) != 1 || result.Title != "Photo" || result.Items[0].URL != pinterestOriginal || len(result.Items[0].AlternateURLs) != 1 || result.Items[0].AlternateURLs[0] != pinterestThumb || result.Items[0].Headers["Referer"] != "https://www.pinterest.com/" {
		t.Fatalf("pin=%+v err=%v", result, err)
	}
	for _, body := range []string{
		strings.Replace(pinterestPinHTML, pinterestSampleID, "999999999999999999", 1),
		strings.Replace(pinterestPinHTML, pinterestThumb, "https://i.pinimg.com.evil.test/736x/aa/bb/cc/wrong.jpg", 1),
		strings.Replace(pinterestPinHTML, pinterestThumb, "https://user@i.pinimg.com/736x/aa/bb/cc/wrong.jpg", 1),
		strings.Replace(pinterestPinHTML, pinterestThumb, "https://i.pinimg.com:8443/736x/aa/bb/cc/wrong.jpg", 1),
		`<meta property="og:image" content="` + pinterestThumb + `"><img src="` + pinterestThumb + `">`,
	} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return pinterestResponse(raw, body, "text/html"), nil
		})
		got, err := discoverPinterest(context.Background(), getter, root)
		if err == nil || len(got.Items) != 0 {
			t.Fatalf("unbound pin=%+v err=%v", got, err)
		}
	}
}

func TestPinterestRegionalRedirect(t *testing.T) {
	root, _ := url.Parse("https://www.pinterest.fr/pin/" + pinterestSampleID + "/")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := pinterestResponse(raw, pinterestPinHTML, "text/html")
		resp.FinalURL = "https://fr.pinterest.com/pin/" + pinterestSampleID + "/"
		return resp, nil
	})
	got, err := discoverPinterest(context.Background(), getter, root)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != pinterestOriginal {
		t.Fatalf("regional pin=%+v err=%v", got, err)
	}
}

func TestPinterestBoardPaginationAndIdentity(t *testing.T) {
	root, _ := url.Parse("https://www.pinterest.com/owner/board/?private=sentinel")
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Path == "/owner/board/" {
			return pinterestResponse(raw, pinterestBoardHTML, "text/html"), nil
		}
		if u.Host != "www.pinterest.com" || u.Path != "/resource/BoardFeedResource/get/" || strings.Contains(raw, "private%3Dsentinel") || strings.Contains(raw, "sentinel") {
			t.Fatalf("unsafe API url %q", raw)
		}
		q := u.Query()
		if q.Get("source_url") != "/owner/board/" {
			t.Fatalf("source %q", q.Get("source_url"))
		}
		var payload struct {
			Options struct {
				BoardID   string   `json:"board_id"`
				BoardURL  string   `json:"board_url"`
				Bookmarks []string `json:"bookmarks"`
			} `json:"options"`
		}
		if err := json.Unmarshal([]byte(q.Get("data")), &payload); err != nil || payload.Options.BoardID != "123456" || payload.Options.BoardURL != "/owner/board/" || len(payload.Options.Bookmarks) != 1 || payload.Options.Bookmarks[0] != "cursor1" {
			t.Fatalf("payload=%+v err=%v", payload, err)
		}
		req, _ := http.NewRequest("GET", raw, nil)
		prepare(req)
		if req.Header.Get("Accept") != "application/json" {
			t.Fatalf("header %v", req.Header)
		}
		return pinterestResponse(raw, `{"resource_response":{"data":[{"id":"Pin2","type":"pin","images":{"orig":{"url":"https://i.pinimg.com/originals/dd/ee/ff/second.jpg"}}}],"bookmark":"-end-"}}`, "application/json"), nil
	})
	got, err := discoverPinterest(context.Background(), getter, root)
	if err != nil || calls != 2 || got.Title != "Board" || len(got.Items) != 2 || !strings.Contains(got.Items[0].URL, "first.jpg") || !strings.Contains(got.Items[1].URL, "second.jpg") {
		t.Fatalf("board=%+v calls=%d err=%v", got, calls, err)
	}
}

func TestPinterestBoardRetainsPartialAndRejectsWrongBoard(t *testing.T) {
	root, _ := url.Parse("https://www.pinterest.com/owner/board/")
	for _, tc := range []struct {
		name, body  string
		code, count int
	}{
		{"http403", pinterestBoardHTML, 403, 1},
		{"wrong-board", strings.Replace(pinterestBoardHTML, `"url":"/owner/board/"`, `"url":"/other/board/"`, 1), 200, 0},
		{"missing-feed", strings.Replace(pinterestBoardHTML, `[[\"board_id\",\"123456\"]]`, `[[\"board_id\",\"000000\"]]`, 1), 200, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Path == "/owner/board/" {
					return pinterestResponse(raw, tc.body, "text/html"), nil
				}
				resp := pinterestResponse(raw, "{}", "application/json")
				resp.StatusCode = tc.code
				return resp, nil
			})
			got, err := discoverPinterest(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}

func TestPinterestCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse("https://www.pinterest.com/pin/" + pinterestSampleID + "/")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverPinterest(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := pinterestResponse(raw, pinterestPinHTML, "text/html")
		resp.FinalURL = "https://evil.test/pin/" + pinterestSampleID + "/"
		return resp, nil
	})
	got, err := discoverPinterest(context.Background(), getter, root)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect=%+v err=%v", got, err)
	}
}

func TestPinterestLivePinDownload(t *testing.T) {
	if os.Getenv("TULAI_PINTEREST_LIVE") != "1" {
		t.Skip("set TULAI_PINTEREST_LIVE=1 to test a public Pinterest Pin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse("https://www.pinterest.com/pin/" + pinterestSampleID + "/")
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, root.String(), 1)
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("pin items=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "pin.jpg"
	item.RequireImage = true
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	f, err := os.Open(filepath.Join(dir, item.Filename))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, format, err := image.DecodeConfig(f)
	if err != nil || format != "jpeg" || im.Width == 0 || im.Height == 0 {
		t.Fatalf("image=%+v format=%q err=%v", im, format, err)
	}
	t.Logf("public pin decoded=%dx%d %s", im.Width, im.Height, format)
}

func TestPinterestLiveBoardPartial(t *testing.T) {
	if os.Getenv("TULAI_PINTEREST_LIVE") != "1" {
		t.Skip("set TULAI_PINTEREST_LIVE=1 to test a public Pinterest board")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse("https://www.pinterest.com/tonii__a/mother-daughter/")
	got, err := Discover(ctx, NewHTTPGetter(""), root.String(), 1)
	if len(got.Items) < 20 {
		t.Fatalf("board items=%d err=%v", len(got.Items), err)
	}
	t.Logf("public board pins=%d err=%v", len(got.Items), err)
}

func TestPinterestBookmarkLoopKeepsFirstPage(t *testing.T) {
	root, _ := url.Parse("https://www.pinterest.com/owner/board/")
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		u, _ := url.Parse(raw)
		if u.Path == "/owner/board/" {
			return pinterestResponse(raw, pinterestBoardHTML, "text/html"), nil
		}
		return pinterestResponse(raw, `{"resource_response":{"data":[{"id":"Pin2","type":"pin","images":{"orig":{"url":"https://i.pinimg.com/originals/dd/ee/ff/second.jpg"}}}],"bookmark":"cursor1"}}`, "application/json"), nil
	})
	got, err := discoverPinterest(context.Background(), getter, root)
	if err == nil || len(got.Items) != 2 || calls != 2 {
		t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
	}
}

type pinterestRoundTripper func(*http.Request) (*http.Response, error)

func (fn pinterestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestPinterestRenderedBoardCapture(t *testing.T) {
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
	const target = "https://www.pinterest.com/owner/board/?token=synthetic"
	var requests, syntheticCookie atomic.Int32
	session.getter.client.Transport = pinterestRoundTripper(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.URL.Host != "www.pinterest.com" || r.URL.Path != "/resource/BoardFeedResource/get/" {
			return nil, errors.New("unexpected request")
		}
		for _, c := range r.Cookies() {
			if c.Name == "synthetic_session" && c.Value == "example" {
				syntheticCookie.Add(1)
			}
		}
		return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(pinterestBoardHTML)))
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.pinterest.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 1 || !strings.Contains(got.Items[0].URL, "first.jpg") || requests.Load() != 1 || syntheticCookie.Load() != 1 {
		t.Fatalf("capture items=%+v requests=%d synthetic-cookie=%d err=%v", got.Items, requests.Load(), syntheticCookie.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}
