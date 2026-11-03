package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const ebaySearchTestURL = "https://www.ebay.com/sch/i.html?_nkw=camera&_sacat=625"

func ebaySearchTestURLParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestEbaySearchRouteAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{ebaySearchTestURL, true},
		{"https://www.ebay.fr/sch/625/i.html?_nkw=camera", true},
		{"https://cafr.ebay.ca/sch/i.html?_nkw=camera", true},
		{"https://www.ebay.com/sch/i.html?_nkw=camera&_nkw=other", true},
		{"https://www.ebay.com/itm/123456789012", false},
		{"https://ebay.com.evil.test/sch/i.html?_nkw=camera", false},
		{"https://evil.ebay.com/sch/i.html?_nkw=camera", false},
		{"https://user@www.ebay.com/sch/i.html?_nkw=camera", false},
		{"https://www.ebay.com:8443/sch/i.html?_nkw=camera", false},
		{"https://www.ebay.com/sch/seller/m.html", false},
	} {
		if got := isEbaySearch(ebaySearchTestURLParse(t, tc.raw)); got != tc.want {
			t.Errorf("route %s = %v, want %v", tc.raw, got, tc.want)
		}
	}
	start := ebaySearchTestURLParse(t, ebaySearchTestURL)
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{ebaySearchTestURL + "&_trksid=synthetic", true},
		{"https://ebay.com/sch/i.html?_sacat=625&_nkw=camera&_pgn=1", true},
		{"https://www.ebay.com/sch/i.html?_nkw=phone&_sacat=625", false},
		{"https://www.ebay.com/sch/i.html?_nkw=camera&_sacat=626", false},
		{"https://www.ebay.com/sch/i.html?_nkw=camera", false},
		{"https://www.ebay.com/sch/i.html?_nkw=camera&_sacat=625&_sop=12", false},
		{"https://www.ebay.com/sch/i.html?_nkw=camera&_sacat=625&_pgn=2", false},
		{"https://www.ebay.com/sch/i.html?_nkw=camera&_sacat=625&_pgn=1&_pgn=2", false},
		{"http://www.ebay.com/sch/i.html?_nkw=camera&_sacat=625", false},
		{"https://www.ebay.fr/sch/i.html?_nkw=camera&_sacat=625", false},
	} {
		if got := ebaySearchSameRequest(start, ebaySearchTestURLParse(t, tc.raw)); got != tc.want {
			t.Errorf("same request %s = %v, want %v", tc.raw, got, tc.want)
		}
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"/sch/i.html?_nkw=camera&_sacat=625&_pgn=2", true},
		{"/sch/i.html?_nkw=camera&_sacat=625&_pgn=3", false},
		{"/sch/i.html?_nkw=phone&_sacat=625&_pgn=2", false},
		{"https://evil.test/sch/i.html?_nkw=camera&_sacat=625&_pgn=2", false},
		{"/sch/i.html?_nkw=camera&_pgn=2", false},
		{"/sch/i.html?_nkw=camera&_sacat=625&_pgn=2&_pgn=3", false},
	} {
		_, err := ebaySearchNext(start, tc.raw)
		if (err == nil) != tc.want {
			t.Errorf("next %s = %v, want valid %v", tc.raw, err, tc.want)
		}
	}
}

func ebaySearchCard(id, title, image string) string {
	return fmt.Sprintf(`<li class="s-item"><a class="s-item__link" href="https://www.ebay.com/itm/example/%s"><div class="s-item__title"><span>%s</span></div></a><div class="image-treatment"><img src="%s" alt="%s"></div></li>`, id, title, image, title)
}

func ebaySearchFixture(next string, cards ...string) string {
	return `<html><head><title>Camera results</title></head><body><img src="https://i.ebayimg.com/images/g/Logo/s-l500.jpg"><div class="srp-river-results"><ul>` + strings.Join(cards, "") + `</ul></div>` + next + `</body></html>`
}

func ebaySearchGetter(fn ebayRoundTrip) *HTTPGetter {
	getter := NewHTTPGetter("")
	getter.client.Transport = fn
	return getter
}

func ebaySearchHTTPResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Request: r, Body: io.NopCloser(strings.NewReader(body))}
}

func TestEbaySearchPaginationAndFallback(t *testing.T) {
	first := ebaySearchFixture(`<a class="pagination__next" href="/sch/i.html?_nkw=camera&amp;_sacat=625&amp;_pgn=2&amp;_trksid=tracking">Next</a>`,
		ebaySearchCard("123456789012", "Camera", "https://i.ebayimg.com/images/g/A/s-l500.jpg"),
		ebaySearchCard("123456789013", "Lens", "https://i.ebayimg.com/images/g/B/s-l500.webp?x=1"),
		`<li class="s-item" hidden><a class="s-item__link" href="/itm/123456789019"></a><div class="image-treatment"><img src="https://i.ebayimg.com/images/g/Hidden/s-l500.jpg"></div></li>`,
		`<li class="s-item"><a class="s-item__link" href="https://evil.test/itm/123456789018"></a><div class="image-treatment"><img src="https://i.ebayimg.com/images/g/Other/s-l500.jpg"></div></li>`)
	second := ebaySearchFixture(`<a class="pagination__next" aria-disabled="true">Next</a>`,
		ebaySearchCard("123456789013", "Lens repeated", "https://i.ebayimg.com/images/g/B/s-l500.webp?x=1"),
		ebaySearchCard("123456789014", "Flash", "https://i.ebayimg.com/images/g/C/s-l500.jpg"))
	calls := 0
	getter := ebaySearchGetter(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Hostname() != "www.ebay.com" || r.URL.Query().Get("_nkw") != "camera" || r.URL.Query().Get("_sacat") != "625" || r.Header.Get("Cookie") != "" {
			t.Fatalf("unsafe page request: %v %v", r.URL, r.Header)
		}
		if r.URL.Query().Get("_pgn") == "2" {
			return ebaySearchHTTPResponse(r, 200, second), nil
		}
		return ebaySearchHTTPResponse(r, 200, first), nil
	})
	result, err := discoverEbaySearch(context.Background(), getter, ebaySearchTestURLParse(t, ebaySearchTestURL))
	if err != nil || calls != 2 || result.Title != "Camera results" || len(result.Items) != 3 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
	}
	item := result.Items[0]
	if item.URL != "https://i.ebayimg.com/images/g/A/s-l1600.jpg" || len(item.AlternateURLs) != 1 || item.AlternateURLs[0] != "https://i.ebayimg.com/images/g/A/s-l500.jpg" ||
		item.Metadata["album_title"] != "Camera" || item.Metadata["album_id"] != "123456789012" || item.Headers["Referer"] != "https://www.ebay.com/" || !item.RequireImage || !item.PublicOnly {
		t.Fatalf("product item=%+v", item)
	}
	var pngBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	var requests []string
	imageGetter := ebaySearchGetter(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		if strings.Contains(r.URL.Path, "-l1600.") {
			return ebaySearchHTTPResponse(r, http.StatusNotFound, "not found"), nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Request: r, Body: io.NopCloser(bytes.NewReader(pngBytes.Bytes()))}, nil
	})
	item.Filename = "camera.png"
	dir := t.TempDir()
	if err := New(Options{Getter: imageGetter}).Download(context.Background(), Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[1] != item.AlternateURLs[0] {
		t.Fatalf("fallback requests=%v", requests)
	}
	if info, err := os.Stat(filepath.Join(dir, item.Filename)); err != nil || info.Size() == 0 {
		t.Fatalf("download=%v err=%v", info, err)
	}
}

func TestEbaySearchStopsAndKeepsResults(t *testing.T) {
	card := ebaySearchCard("123456789012", "Camera", "https://i.ebayimg.com/images/g/A/s-l500.jpg")
	for _, mode := range []string{"wrong-query", "other-host", "wrong-page", "redirect-query", "redirect-host", "limited", "forbidden", "blocked", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			getter := ebaySearchGetter(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					next := "/sch/i.html?_nkw=camera&amp;_sacat=625&amp;_pgn=2"
					switch mode {
					case "wrong-query":
						next = "/sch/i.html?_nkw=phone&amp;_sacat=625&amp;_pgn=2"
					case "other-host":
						next = "https://evil.test/sch/i.html?_nkw=camera&amp;_sacat=625&amp;_pgn=2"
					case "wrong-page":
						next = "/sch/i.html?_nkw=camera&amp;_sacat=625&amp;_pgn=3"
					}
					return ebaySearchHTTPResponse(r, 200, ebaySearchFixture(`<a class="pagination__next" href="`+next+`">Next</a>`, card)), nil
				}
				if calls > 2 {
					t.Fatal("unexpected extra request")
				}
				switch mode {
				case "redirect-query", "redirect-host":
					target := "/sch/i.html?_nkw=phone&_sacat=625&_pgn=2"
					if mode == "redirect-host" {
						target = "https://evil.test/sch/i.html?_nkw=camera&_sacat=625&_pgn=2"
					}
					response := ebaySearchHTTPResponse(r, http.StatusFound, "")
					response.Header.Set("Location", target)
					return response, nil
				case "limited":
					return ebaySearchHTTPResponse(r, http.StatusTooManyRequests, ""), nil
				case "forbidden":
					return ebaySearchHTTPResponse(r, http.StatusForbidden, ""), nil
				case "blocked":
					return ebaySearchHTTPResponse(r, 200, `<body>Verify yourself to continue</body>`), nil
				case "canceled":
					cancel()
					return nil, context.Canceled
				}
				t.Fatal("request after invalid next link")
				return nil, nil
			})
			result, err := discoverEbaySearch(ctx, getter, ebaySearchTestURLParse(t, ebaySearchTestURL))
			if err == nil || len(result.Items) != 1 || calls > 2 {
				t.Fatalf("result=%+v calls=%d err=%v", result, calls, err)
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}

func TestEbaySearchRejectsRecommendationsAndMissingImage(t *testing.T) {
	page := ebaySearchTestURLParse(t, ebaySearchTestURL)
	getter := ebaySearchGetter(func(r *http.Request) (*http.Response, error) { return nil, errors.New("unexpected fetch") })
	for _, tc := range []struct {
		name string
		body string
		err  bool
	}{
		{"empty", `<p>No exact matches found</p>` + ebaySearchFixture("", ebaySearchCard("123456789012", "Suggestion", "https://i.ebayimg.com/images/g/A/s-l500.jpg")), false},
		{"blocked", `<p>Pardon our interruption</p>` + ebaySearchFixture("", ebaySearchCard("123456789012", "Suggestion", "https://i.ebayimg.com/images/g/A/s-l500.jpg")), true},
		{"missing", ebaySearchFixture("", ebaySearchCard("123456789012", "Broken", "https://evil.test/image.jpg")), true},
		{"missing-grid", `<html><body>unrecognized</body></html>`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			result, err := ebaySearchCollect(context.Background(), getter, page, doc)
			if len(result.Items) != 0 || (err != nil) != tc.err {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
