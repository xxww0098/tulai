package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/xxww0098/tulai/internal/camoufox"
)

const wellcoTestPage = "https://wellco.ca/products/example"
const wellcoTestImage = "https://d2j6dbq0eux0bg.cloudfront.net/images/105295557/products/123/456.jpg"

func wellcoProductFixture(page, image string) string {
	return fmt.Sprintf(`<html><head><meta property="og:url" content="%s"><script type="application/ld+json">{"@type":"Product","name":"Example","image":[{"url":%q,"contentUrl":%q}]}</script></head></html>`, page, page, image)
}

func TestWellcoProductAndIdentity(t *testing.T) {
	fixture := wellcoProductFixture(wellcoTestPage, wellcoTestImage)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw != wellcoTestPage {
			return nil, errors.New("unexpected page")
		}
		return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(fixture))}, nil
	})
	got, err := Discover(context.Background(), getter, wellcoTestPage, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != wellcoTestImage || !got.Items[0].PublicOnly ||
		got.Items[0].Metadata["wellco_product_id"] != "123" || got.Items[0].Headers["Referer"] != "https://wellco.ca/" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	for _, bad := range []string{
		strings.Replace(fixture, `content="https://wellco.ca/products/example"`, `content="https://wellco.ca/products/other"`, 1),
		strings.Replace(fixture, `"url":"https://wellco.ca/products/example"`, `"url":"https://wellco.ca/products/other"`, 1),
		strings.Replace(fixture, "d2j6dbq0eux0bg.cloudfront.net", "evil.example", 1),
		strings.Replace(fixture, "/images/105295557/", "/images/111111/", 1),
	} {
		fixture = bad
		got, err = Discover(context.Background(), getter, wellcoTestPage, 1)
		if err == nil || len(got.Items) != 0 {
			t.Fatalf("accepted mismatched product: %+v err=%v", got, err)
		}
	}
}

func TestWellcoQueryIdentity(t *testing.T) {
	start, _ := url.Parse("https://wellco.ca/products/scarves?offset=60&category=shirts")
	for _, tc := range []struct {
		page string
		want bool
	}{
		{"https://wellco.ca/products/scarves?category=shirts&offset=60", true},
		{"https://wellco.ca/products/scarves?offset=60", false},
		{"https://wellco.ca/products/scarves?offset=60&category=pants", false},
		{"https://wellco.ca/products/scarves?offset=0&category=shirts", false},
		{"https://wellco.ca/products/scarves?offset=60&offset=120&category=shirts", false},
		{"https://wellco.ca/products/scarves?offset=60&category=shirts#different", false},
	} {
		page, err := url.Parse(tc.page)
		if err != nil || wellcoSameFetchPage(start, page) != tc.want {
			t.Errorf("same page %q: got %v, want %v, error %v", tc.page, wellcoSameFetchPage(start, page), tc.want, err)
		}
	}
}

func TestWellcoCategoryPreservesPartial(t *testing.T) {
	const category = "https://wellco.ca/products/scarves"
	const second = "https://wellco.ca/products/second"
	body := `<html><head><meta property="og:url" content="` + category + `"><meta property="og:title" content="Scarves"></head><body>` +
		`<a class="grid-product__image" data-product-id="123" href="` + wellcoTestPage + `">first</a>` +
		`<a class="grid-product__image" data-product-id="123" href="` + wellcoTestPage + `">duplicate</a>` +
		`<a class="grid-product__image" data-product-id="999" href="` + second + `">second</a>` +
		`<a class="grid-product__image" data-product-id="456" href="https://evil.example/products/no">wrong host</a>` +
		`<a data-page-number="2" href="javascript:;">next</a></body></html>`
	firstDone := make(chan struct{})
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		switch raw {
		case category:
			return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
		case wellcoTestPage:
			defer close(firstDone)
			return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(wellcoProductFixture(raw, wellcoTestImage)))}, nil
		case second:
			<-firstDone
			return &Response{StatusCode: 429, FinalURL: raw, Body: io.NopCloser(strings.NewReader("limited"))}, nil
		default:
			return nil, errors.New("unexpected page")
		}
	})
	got, err := Discover(context.Background(), getter, category, 1)
	if err == nil || !strings.Contains(err.Error(), "429") || len(got.Items) != 1 || got.Items[0].Metadata["album_id"] != "123" || got.Items[0].Metadata["album_title"] != "Example" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page, _ := url.Parse(category)
	if _, err := discoverWellco(ctx, getter, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestWellcoCategoryStopsAfterRateLimit(t *testing.T) {
	const category = "https://wellco.ca/products/scarves"
	var body strings.Builder
	fmt.Fprintf(&body, `<html><head><meta property="og:url" content="%s"></head><body>`, category)
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&body, `<a class="grid-product__image" data-product-id="%d" href="https://wellco.ca/products/item%d">product</a>`, i, i)
	}
	body.WriteString(`</body></html>`)
	var productRequests atomic.Int32
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == category {
			return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body.String()))}, nil
		}
		productRequests.Add(1)
		return &Response{StatusCode: 429, FinalURL: raw, Body: io.NopCloser(strings.NewReader("limited"))}, nil
	})
	got, err := Discover(context.Background(), getter, category, 1)
	if err == nil || !strings.Contains(err.Error(), "429") || len(got.Items) != 0 || productRequests.Load() > 3 {
		t.Fatalf("images=%d requests=%d err=%v", len(got.Items), productRequests.Load(), err)
	}
}

func TestWellcoPaginationKeepsResultsOnRateLimit(t *testing.T) {
	const category = "https://wellco.ca/products/scarves"
	var body strings.Builder
	fmt.Fprintf(&body, `<html><head><meta property="og:url" content="%s"></head><body>`, category)
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&body, `<a class="grid-product__image" data-product-id="%d" href="https://wellco.ca/products/item%d">product</a>`, i, i)
	}
	body.WriteString(`<a class="pager__number pager__number--current" data-page-number="1"></a><a class="pager__number" data-page-number="2"></a></body></html>`)
	var laterRequests atomic.Int32
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		switch raw {
		case category:
			return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body.String()))}, nil
		case category + "?offset=60":
			laterRequests.Add(1)
			return &Response{StatusCode: 429, FinalURL: raw, Body: io.NopCloser(strings.NewReader("limited"))}, nil
		}
		id := strings.TrimPrefix(raw, "https://wellco.ca/products/item")
		image := "https://d2j6dbq0eux0bg.cloudfront.net/images/105295557/products/" + id + "/1.jpg"
		return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(wellcoProductFixture(raw, image)))}, nil
	})
	got, err := Discover(context.Background(), getter, category, 1)
	if err == nil || !strings.Contains(err.Error(), "429") || len(got.Items) != 60 || laterRequests.Load() != 1 {
		t.Fatalf("images=%d pagination requests=%d err=%v", len(got.Items), laterRequests.Load(), err)
	}
}

func TestWellcoPublicProduct(t *testing.T) {
	if os.Getenv("TULAI_WELLCO_LIVE") != "1" {
		t.Skip("set TULAI_WELLCO_LIVE=1 for the public product check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, "https://wellco.ca/products/bl070-151-23", 1)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("images=%d err=%v", len(got.Items), err)
	}
	dir := t.TempDir()
	item := got.Items[0]
	item.Filename = "wellco"
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("downloaded files=%d err=%v", len(files), err)
	}
	f, err := os.Open(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	conf, format, err := image.DecodeConfig(f)
	if err != nil || format != "jpeg" || conf.Width != 1000 || conf.Height != 1300 {
		t.Fatalf("image=%+v format=%q err=%v", conf, format, err)
	}
}

func TestWellcoPublicCategoryFixture(t *testing.T) {
	dir := os.Getenv("TULAI_WELLCO_FIXTURES")
	if dir == "" {
		t.Skip("set TULAI_WELLCO_FIXTURES to the saved public pages")
	}
	category, err := os.ReadFile(filepath.Join(dir, "category.html"))
	if err != nil {
		t.Fatal(err)
	}
	product, err := os.ReadFile(filepath.Join(dir, "product.html"))
	if err != nil {
		t.Fatal(err)
	}
	category2, err := os.ReadFile(filepath.Join(dir, "category2.html"))
	if err != nil {
		t.Fatal(err)
	}
	const categoryURL = "https://wellco.ca/products/woven-scarves-170419009"
	const productURL = "https://wellco.ca/products/bl070-151-23"
	const product2URL = "https://wellco.ca/products/bl070-157-mv"
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		var body string
		switch raw {
		case categoryURL:
			body = string(category)
		case categoryURL + "?offset=60":
			body = string(category2)
		case productURL:
			body = string(product)
		case product2URL:
			body = wellcoProductFixture(product2URL, "https://d2j6dbq0eux0bg.cloudfront.net/images/105295557/products/676058883/1.jpg")
		default:
			body = "<html></html>"
		}
		return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := Discover(context.Background(), getter, categoryURL, 1)
	if err == nil || len(got.Items) != 3 || got.Items[0].Metadata["album_id"] != "861304022" || got.Items[2].Metadata["album_id"] != "676058883" {
		t.Fatalf("category images=%d err=%v", len(got.Items), err)
	}
}

func TestWellcoPublicCategory(t *testing.T) {
	if os.Getenv("TULAI_WELLCO_LIVE_CATEGORY") != "1" {
		t.Skip("set TULAI_WELLCO_LIVE_CATEGORY=1 for a public category check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "https://wellco.ca/products/woven-scarves-170419009", 1)
	if len(got.Items) < 2 {
		t.Fatalf("category images=%d err=%v", len(got.Items), err)
	}
	products := map[string]bool{}
	for _, item := range got.Items {
		products[item.Metadata["wellco_product_id"]] = true
	}
	if err != nil {
		t.Logf("public category is partial: %d products, %d images, %v", len(products), len(got.Items), err)
		return
	}
	t.Logf("public category: %d products, %d images", len(products), len(got.Items))
}

func TestWellcoRenderedCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_WELLCO_BROWSER_RUNTIME")
	if runtime == "" {
		t.Skip("set TULAI_WELLCO_BROWSER_RUNTIME to an isolated Camoufox runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	proc, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(proc.Context())
	tab := camoufox.PageContext(life, proc, proc.Page)
	start, _ := url.Parse(wellcoTestPage)
	s := &RenderedSession{process: proc, tab: tab, closeTab: closeTab, closeAllocator: func() { proc.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: start}
	defer s.Close()
	other := "https://wellco.ca/products/other"
	camoufox.ListenTarget(tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if paused.ResourceType == network.ResourceTypeDocument && (paused.Request.URL == wellcoTestPage || paused.Request.URL == other) {
				body := wellcoProductFixture(paused.Request.URL, wellcoTestImage)
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			}
			_ = camoufox.Run(tab, action)
		}()
	})
	if err := s.run(ctx, fetch.Enable(), camoufox.Navigate(wellcoTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != wellcoTestImage {
		t.Fatalf("capture images=%d err=%v", len(got.Items), err)
	}
	if err := s.run(ctx, camoufox.Navigate(other)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Capture(ctx); err == nil || len(got.Items) != 0 {
		t.Fatalf("accepted another product: images=%d err=%v", len(got.Items), err)
	}
}
