package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

const sheinTestProduct = "https://us.shein.com/sample-p-123.html"
const sheinTestImage = "https://img.ltwebstatic.com/v4/j/pi/2025/10/14/07/original.jpg"
const sheinTestImage2 = "https://img.ltwebstatic.com/images3_pi/2025/03/31/4f/other.webp"

func sheinStateFixture(raw string) string {
	return `<html><head><link rel="canonical" href="` + sheinTestProduct + `"></head><body><script>window.gbRawData = ` + raw + `;</script><img src="https://img.ltwebstatic.com/recommended.jpg"></body></html>`
}

func sheinState() string {
	return `{"googleSEO":{},"product":{"goods_id":"123","goods_name":"Example dress","allColorDetailImages":{"123":[{"origin_image":"` + sheinTestImage + `"}],"456":[{"origin_image":"` + sheinTestImage2 + `"}],"999":[{"origin_image":"https://img.ltwebstatic.com/recommendation.jpg"}]},"mainSaleAttribute":{"info":[{"goods_id":"123","attr_value":"Black"},{"goods_id":"456","attr_value":"Red"}]}}}`
}

func sheinResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestSheinProductAndCDNBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{sheinTestProduct, true},
		{"https://de.shein.com/-p-216147362.html", true},
		{"https://m.shein.com/de/Tie-Back-High-Neck-A-Line-Dress-p-11337364.html", true},
		{"https://us.shein.com/sample-p-123.html?ref=any", true},
		{"https://us.shein.com/sample-p-123.html/", false},
		{"https://us.shein.com/sample-p-0.html", false},
		{"https://us.shein.com/sample-p-123.html.evil", false},
		{"https://us.shein.com.evil.test/sample-p-123.html", false},
		{"https://someone@us.shein.com/sample-p-123.html", false},
		{"https://us.shein.com:8443/sample-p-123.html", false},
		{"https://us.shein.com/risk/challenge", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isSheinProduct(u); got != tc.ok {
			t.Errorf("isSheinProduct(%q)=%v", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"http://img.ltwebstatic.com/foo.jpg", "https://img.ltwebstatic.com.evil.test/foo.jpg",
		"https://x@img.ltwebstatic.com/foo.jpg", "https://img.ltwebstatic.com:444/foo.jpg",
		"https://127.0.0.1/foo.jpg", "https://img.ltwebstatic.com/a/../foo.jpg",
	} {
		if _, ok := sheinImageURL(raw); ok {
			t.Errorf("accepted %q", raw)
		}
	}
	if got, ok := sheinImageURL("//img.ltwebstatic.com/foo.jpg#x"); !ok || got != "https://img.ltwebstatic.com/foo.jpg" {
		t.Fatalf("image=%q ok=%v", got, ok)
	}
}

func TestSheinReferenceColorState(t *testing.T) {
	root, _ := url.Parse(sheinTestProduct)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return sheinResponse(raw, sheinStateFixture(sheinState())), nil
	})
	got, err := Discover(context.Background(), getter, root.String(), 1)
	if err != nil || got.Title != "Example dress" || len(got.Items) != 2 {
		t.Fatalf("title=%q items=%+v err=%v", got.Title, got.Items, err)
	}
	if got.Items[0].URL != sheinTestImage || got.Items[0].Metadata["shein_goods_id"] != "123" || got.Items[1].URL != sheinTestImage2 || got.Items[1].Metadata["shein_color"] != "Red" {
		t.Fatalf("items=%+v", got.Items)
	}
	for _, item := range got.Items {
		if !item.PublicOnly || !item.RequireImage || item.Headers["Referer"] != "https://us.shein.com/" {
			t.Fatalf("unsafe item %+v", item)
		}
	}
}

func TestSheinStateIdentityAndPartial(t *testing.T) {
	root, _ := url.Parse(sheinTestProduct)
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"wrong page identity", strings.Replace(sheinStateFixture(sheinState()), sheinTestProduct, "https://us.shein.com/sample-p-999.html", 1), 0},
		{"wrong product identity", sheinStateFixture(strings.Replace(sheinState(), `"goods_id":"123"`, `"goods_id":"999"`, 1)), 0},
		{"wrong selected color", sheinStateFixture(strings.Replace(sheinState(), `"goods_id":"123","attr_value"`, `"goods_id":"999","attr_value"`, 1)), 0},
		{"missing variant", sheinStateFixture(strings.Replace(sheinState(), `"456":[{"origin_image":"`+sheinTestImage2+`"}],`, "", 1)), 1},
		{"untrusted variant CDN", sheinStateFixture(strings.Replace(sheinState(), sheinTestImage2, "https://evil.test/a.jpg", 1)), 1},
		{"assignment inside string", sheinStateFixture(`null`) + `<script>var s='window.gbRawData = ` + sheinState() + `';</script>`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return sheinResponse(raw, tc.body), nil
			})
			got, err := discoverShein(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestSheinJSONLDProductBinding(t *testing.T) {
	root, _ := url.Parse(sheinTestProduct)
	for _, tc := range []struct {
		name, target, variant string
		want                  int
	}{
		{"matching group", sheinTestProduct, "", 2},
		{"other group", "https://us.shein.com/other-p-999.html", "", 0},
		{"missing group URL", "", "", 0},
		{"other variant", sheinTestProduct, "https://us.shein.com/other-p-999.html", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := map[string]any{"@type": "ProductGroup", "url": tc.target, "name": "Dress", "image": []string{sheinTestImage}, "hasVariant": []any{map[string]any{"@type": "Product", "url": tc.variant, "image": sheinTestImage2}}}
			data, _ := json.Marshal(v)
			body := `<html><head><script type="application/ld+json">` + string(data) + `</script></head></html>`
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return sheinResponse(raw, body), nil
			})
			got, err := discoverShein(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestSheinRedirectChallengeAndCancellation(t *testing.T) {
	root, _ := url.Parse(sheinTestProduct)
	for _, target := range []string{"https://us.shein.com/risk/challenge", "https://us.shein.com/other-p-999.html", "https://evil.test/x"} {
		getter := GetterFunc(func(_ context.Context, _ string, _ func(*http.Request)) (*Response, error) {
			return sheinResponse(target, `<html></html>`), nil
		})
		got, err := discoverShein(context.Background(), getter, root)
		if err == nil || len(got.Items) != 0 {
			t.Fatalf("target=%q items=%+v err=%v", target, got.Items, err)
		}
		if strings.Contains(target, "/risk/") && !strings.Contains(err.Error(), "安全验证") {
			t.Fatalf("challenge=%v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(_ context.Context, _ string, _ func(*http.Request)) (*Response, error) {
		t.Fatal("cancelled request reached getter")
		return nil, nil
	})
	if _, err := discoverShein(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled=%v", err)
	}
}

func TestSheinRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == sheinTestProduct {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(sheinStateFixture(sheinState()))))
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
	start, _ := url.Parse(sheinTestProduct)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(sheinTestProduct)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != sheinTestImage {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
}
