package download

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDiscoverImagesAndDepth(t *testing.T) {
	var outsideHits atomic.Int32
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outsideHits.Add(1)
		fmt.Fprint(w, "<img src='/outside.jpg'>")
	}))
	defer outside.Close()
	var childHits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/gallery", http.StatusFound)
		case "/gallery":
			fmt.Fprintf(w, `<html><head><base href="/assets/"><title>猫 的相册</title>
				<meta property="og:image" content="/share.jpg"></head><body>
				<img src="/placeholder.jpg" data-src="/lazy.jpg">
				<img src="/small.jpg" srcset="/small.jpg 320w, /large.jpg 1280w">
				<div style="background-image:url('bg.png')"></div>
				<picture><source srcset="/small.webp 1x, /large.webp 2x"><img src="/fallback.jpg"></picture>
				<img src="/lazy.jpg#again"><img src="data:image/png;base64,aA==">
				<img data-src="data:image/png;base64,aA==" src="/valid.jpg">
				<a href="/full.jpg#image">original</a><a href="/child#section">next</a>
				<a href="%s/foreign">foreign</a><a href="javascript:void(0)">bad</a></body></html>`, outside.URL)
		case "/child":
			childHits.Add(1)
			if r.Header.Get("Referer") != srv.URL+"/gallery" {
				t.Errorf("child referer = %q", r.Header.Get("Referer"))
			}
			fmt.Fprint(w, `<img src="/child.jpg"><img src="/share.jpg"><a href="/third">more</a>`)
		case "/third":
			fmt.Fprint(w, `<img src="/third.jpg">`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	page, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL+"/start", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !page.IsPage || page.Title != "猫 的相册" || childHits.Load() != 0 {
		t.Fatalf("depth 1 = %+v, child hits = %d", page, childHits.Load())
	}
	want := []string{"/share.jpg", "/lazy.jpg", "/large.jpg", "/assets/bg.png", "/large.webp", "/fallback.jpg", "/valid.jpg", "/full.jpg"}
	if len(page.Items) != len(want) {
		t.Fatalf("depth 1 images = %+v", page.Items)
	}
	for i, item := range page.Items {
		if item.URL != srv.URL+want[i] || item.Headers["Referer"] != srv.URL+"/gallery" {
			t.Errorf("item %d = %+v, want %s", i, item, want[i])
		}
	}

	page, err = Discover(context.Background(), NewHTTPGetter(""), srv.URL+"/start", 2)
	if err != nil {
		t.Fatal(err)
	}
	if childHits.Load() != 1 || outsideHits.Load() != 0 || len(page.Items) != len(want)+1 {
		t.Fatalf("depth 2 images = %+v, child/outside hits = %d/%d", page.Items, childHits.Load(), outsideHits.Load())
	}
	last := page.Items[len(page.Items)-1]
	if last.URL != srv.URL+"/child.jpg" || last.Headers["Referer"] != srv.URL+"/child" {
		t.Fatalf("child image = %+v", last)
	}
}

func TestDiscoverDirectAndMislabelledHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		if r.URL.Path == "/html" {
			fmt.Fprint(w, "<!doctype html><title>Page</title><img src='/photo.jpg'>")
			return
		}
		if r.URL.Path == "/fragment" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "\xef\xbb\xbf<img src='/photo.jpg'>")
			return
		}
		fmt.Fprint(w, "image bytes")
	}))
	defer srv.Close()
	getter := NewHTTPGetter("")
	direct, err := Discover(context.Background(), getter, srv.URL+"/photo", 1)
	if err != nil || direct.IsPage || len(direct.Items) != 0 || direct.Response == nil {
		t.Fatalf("direct = %+v, err = %v", direct, err)
	}
	direct.Response.Body.Close()
	page, err := Discover(context.Background(), getter, srv.URL+"/html", 1)
	if err != nil || !page.IsPage || page.Title != "Page" || len(page.Items) != 1 || page.Items[0].URL != srv.URL+"/photo.jpg" {
		t.Fatalf("mislabelled page = %+v, err = %v", page, err)
	}
	fragment, err := Discover(context.Background(), getter, srv.URL+"/fragment", 1)
	if err != nil || !fragment.IsPage || len(fragment.Items) != 1 || fragment.Items[0].URL != srv.URL+"/photo.jpg" {
		t.Fatalf("mislabelled HTML fragment = %+v, err = %v", fragment, err)
	}
}

func TestDiscoverPartialErrorAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<img src="/ok.jpg"><a href="/missing?secret=1">missing</a>`)
	}))
	defer srv.Close()
	getter := NewHTTPGetter("")
	page, err := Discover(context.Background(), getter, srv.URL, 2)
	var status *HTTPStatusError
	if !page.IsPage || len(page.Items) != 1 || !errors.As(err, &status) || status.StatusCode != 404 || strings.Contains(err.Error(), "secret=1") {
		t.Fatalf("partial = %+v, err = %v", page, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Discover(ctx, getter, srv.URL, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
	_, err = Discover(context.Background(), getter, srv.URL+"/missing", 1)
	if !errors.As(err, &status) || status.StatusCode != 404 {
		t.Fatalf("root err = %v", err)
	}
}

func TestDiscoverCancellationDuringPageRead(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var closed atomic.Int32
	ready := make(chan struct{})
	base := NewHTTPGetter("")
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		resp, err := base.Get(ctx, raw, prepare)
		if err == nil {
			resp.Body = &trackedCrawlBody{ReadCloser: resp.Body, closed: &closed}
			close(ready)
		}
		return resp, err
	})
	result := make(chan error, 1)
	go func() {
		_, err := Discover(ctx, getter, srv.URL, 1)
		result <- err
	}()
	<-started
	<-ready
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read err = %v", err)
	}
	if closed.Load() != 1 {
		t.Fatalf("canceled response closed %d times", closed.Load())
	}
}

type trackedCrawlBody struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (b *trackedCrawlBody) Close() error {
	b.closed.Add(1)
	return b.ReadCloser.Close()
}

func TestDiscoverPreservesSingleUseResponse(t *testing.T) {
	want := bytes.Repeat([]byte("image data"), 2000)
	var calls, closed atomic.Int32
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		if calls.Add(1) > 1 {
			return nil, errors.New("single-use URL was requested twice")
		}
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"image/jpeg"}},
			Body: &trackedCrawlBody{ReadCloser: io.NopCloser(bytes.NewReader(want)), closed: &closed}}, nil
	})
	page, err := Discover(context.Background(), getter, "https://example.test/single-use", 1)
	if err != nil || page.IsPage || page.Response == nil || closed.Load() != 0 {
		t.Fatalf("direct = %+v, closed = %d, err = %v", page, closed.Load(), err)
	}
	got, err := io.ReadAll(page.Response.Body)
	if err != nil || !bytes.Equal(got, want) || calls.Load() != 1 {
		t.Fatalf("read direct: got %d bytes, calls %d, err %v", len(got), calls.Load(), err)
	}
	page.Response.Body.Close()
	if closed.Load() != 1 {
		t.Fatalf("direct response closed %d times", closed.Load())
	}
}

func TestDiscoverPreservesCompressedDirectResponse(t *testing.T) {
	var original bytes.Buffer
	w := gzip.NewWriter(&original)
	w.Write(bytes.Repeat([]byte("compressed image bytes"), 500))
	w.Close()
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"image/jpeg"}, "Content-Encoding": {"gzip"}},
			Body: io.NopCloser(bytes.NewReader(original.Bytes()))}, nil
	})
	page, err := Discover(context.Background(), getter, "https://example.test/image", 1)
	if err != nil || page.IsPage || page.Response == nil {
		t.Fatalf("compressed response = %+v, %v", page, err)
	}
	defer page.Response.Body.Close()
	got, err := io.ReadAll(page.Response.Body)
	if err != nil || !bytes.Equal(got, original.Bytes()) {
		t.Fatalf("compressed bytes changed: got %d bytes, err %v", len(got), err)
	}
}

func TestDiscoverClosesPageAndChildResponses(t *testing.T) {
	var closed atomic.Int32
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		var contentType, body string
		switch raw {
		case "https://example.test/":
			contentType, body = "text/html", `<a href="/page">page</a><a href="/image">image</a>`
		case "https://example.test/page":
			contentType, body = "text/html", `<img src="/ok.jpg">`
		default:
			contentType, body = "image/jpeg", "image"
		}
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}},
			Body: &trackedCrawlBody{ReadCloser: io.NopCloser(strings.NewReader(body)), closed: &closed}}, nil
	})
	page, err := Discover(context.Background(), getter, "https://example.test/", 2)
	if err != nil || !page.IsPage || page.Response != nil || len(page.Items) != 2 || closed.Load() != 3 {
		t.Fatalf("page = %+v, closed = %d, err = %v", page, closed.Load(), err)
	}
}

func TestDiscoverCompressedHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/deflate" {
			w.Header().Set("Content-Encoding", "deflate")
			writer := zlib.NewWriter(w)
			fmt.Fprint(writer, `<title>相册</title><img src="/cat.jpg">`)
			writer.Close()
		} else {
			w.Header().Set("Content-Encoding", "gzip")
			writer := gzip.NewWriter(w)
			fmt.Fprint(writer, `<title>相册</title><img src="/cat.jpg">`)
			writer.Close()
		}
	}))
	defer srv.Close()
	for _, path := range []string{"/gzip", "/deflate"} {
		page, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL+path, 1)
		if err != nil || page.Title != "相册" || len(page.Items) != 1 || page.Items[0].URL != srv.URL+"/cat.jpg" {
			t.Fatalf("%s page = %+v, err = %v", path, page, err)
		}
	}
}

func TestDiscoverFollowsImageButNotPageRedirectAcrossOrigins(t *testing.T) {
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/picture" {
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "image")
		} else {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<img src="/outside.jpg">`)
		}
	}))
	defer outside.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/image">image</a><a href="/page">page</a>`)
		case "/image":
			http.Redirect(w, r, outside.URL+"/picture", http.StatusFound)
		case "/page":
			http.Redirect(w, r, outside.URL+"/page", http.StatusFound)
		}
	}))
	defer srv.Close()
	page, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL, 2)
	if err != nil || len(page.Items) != 1 || page.Items[0].URL != outside.URL+"/picture" || page.Items[0].Headers["Referer"] != srv.URL+"/" {
		t.Fatalf("redirect page = %+v, err = %v", page, err)
	}
}

func TestDiscoverReferrerPrivacy(t *testing.T) {
	out := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer out.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<img src="/same.jpg"><img src="%s/other.jpg">`, out.URL)
	}))
	defer srv.Close()
	page, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL+"/album?token=secret", 1)
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("page = %+v, err = %v", page, err)
	}
	if got := page.Items[0].Headers["Referer"]; got != srv.URL+"/album?token=secret" {
		t.Fatalf("same-origin referer = %q", got)
	}
	if got := page.Items[1].Headers["Referer"]; got != srv.URL+"/" {
		t.Fatalf("cross-origin referer = %q", got)
	}
	if got := imageReferer("https://user:pass@example.test/album?token=secret", "http://cdn.test/photo.jpg"); got != "" {
		t.Fatalf("HTTPS to HTTP referer = %q", got)
	}
	if got := imageReferer("https://user:pass@example.test/album?token=secret", "https://cdn.test/photo.jpg"); got != "https://example.test/" {
		t.Fatalf("HTTPS cross-origin referer = %q", got)
	}
}

func TestDiscoverBoundsPagesAndPageSize(t *testing.T) {
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		body := `<img src="/ok.jpg">`
		if strings.HasSuffix(raw, "/big") {
			body = strings.Repeat(" ", maxPageBytes+1)
		} else if strings.HasSuffix(raw, "/") {
			for i := 0; i < maxCrawlPages+1; i++ {
				body += fmt.Sprintf(`<a href="/page%d">page</a>`, i)
			}
		}
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	page, err := Discover(context.Background(), getter, "https://example.test/", 2)
	if !page.IsPage || len(page.Items) != 1 || err == nil || !strings.Contains(err.Error(), "扫描上限") {
		t.Fatalf("page limit: %+v, %v", page, err)
	}
	_, err = Discover(context.Background(), getter, "https://example.test/big", 1)
	if err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("size limit: %v", err)
	}
}

func TestLargestSrcsetIgnoresDataURLs(t *testing.T) {
	if got := largestSrcset("data:image/png;base64,aA== 1x, /large.jpg 2x"); got != "/large.jpg" {
		t.Fatalf("srcset = %q", got)
	}
	if got := largestSrcset("data:image/png;base64,aA== 1x"); got != "" {
		t.Fatalf("data srcset = %q", got)
	}
}

func TestDiscoverJSONLDImages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<script type="application/ld+json; charset=utf-8">{
			"@context": "https://schema.org", "@graph": [
				{"@type":"Product", "url":"/product-page", "image":["/main.jpg",
					{"@type":"ImageObject", "contentUrl":"../original.jpg", "url":"/preview-page"},
					{"url":"https://cdn.example.test/third.jpg"}]},
				{"@type":["Thing", "https://schema.org/ProductGroup"], "image":{"url":"/group.jpg"},
					"hasVariant":[{"@type":"schema:Product", "image":"/variant.jpg"}]},
				{"@type":"https://schema.org/ImageObject", "contentUrl":"/standalone.jpg"},
				{"@type":"Article", "url":"/not-an-image"}
			]}
		</script><img src="/main.jpg">
		<script type="application/json">{"@type":"Product","image":"/wrong-type.jpg"}</script>
		<script type="application/ld+json">{"@type":"Product","image":"/invalid.jpg"} garbage</script>`)
		fmt.Fprintf(w, `<script type="application/ld+json">%s{"@type":"Product","image":"/too-deep.jpg"}%s</script>`,
			strings.Repeat(`{"nested":`, maxJSONLDDepth+1), strings.Repeat(`}`, maxJSONLDDepth+1))
	}))
	defer srv.Close()
	page, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL+"/products/item?token=secret", 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		srv.URL + "/main.jpg",
		srv.URL + "/original.jpg",
		"https://cdn.example.test/third.jpg",
		srv.URL + "/group.jpg",
		srv.URL + "/variant.jpg",
		srv.URL + "/standalone.jpg",
	}
	if len(page.Items) != len(want) {
		t.Fatalf("JSON-LD images = %+v", page.Items)
	}
	for i, item := range page.Items {
		if item.URL != want[i] {
			t.Errorf("item %d URL = %q, want %q", i, item.URL, want[i])
		}
		referer := srv.URL + "/products/item?token=secret"
		if i == 2 {
			referer = srv.URL + "/"
		}
		if item.Headers["Referer"] != referer {
			t.Errorf("item %d referer = %q, want %q", i, item.Headers["Referer"], referer)
		}
	}
}
