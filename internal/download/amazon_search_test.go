package download

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

const amazonSearchFixture = `<html><head><title>Coffee - Amazon</title></head><body>
<div class="s-main-slot">
  <div data-component-type="s-search-result" data-asin="B012345678">
    <a href="/dp/B012345678" aria-hidden="true"><img class="s-image" aria-hidden="true" src="https://m.media-amazon.com/images/I/main._AC_UL320_.jpg" alt="short"></a>
    <h2 aria-label="Full coffee mug title"></h2>
  </div>
  <div data-component-type="s-search-result" data-asin="B012345679" hidden><a href="/dp/B012345679"><img class="s-image" src="https://m.media-amazon.com/images/I/hidden._AC_UL320_.jpg"></a></div>
  <div data-component-type="s-search-result" data-asin="B012345670" aria-hidden="true"><a href="/dp/B012345670"><img class="s-image" src="https://m.media-amazon.com/images/I/aria._AC_UL320_.jpg"></a></div>
  <template><div data-component-type="s-search-result" data-asin="B012345671"><a href="/dp/B012345671"><img class="s-image" src="https://m.media-amazon.com/images/I/template.jpg"></a></div></template>
  <div data-component-type="s-search-result" data-asin="B012345672"><a href="/dp/B012345673"><img class="s-image" src="https://m.media-amazon.com/images/I/unrelated.jpg"></a></div>
</div><span class="s-pagination-next s-pagination-disabled">next</span></body></html>`

func amazonSearchParse(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func amazonSearchURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAmazonSearchRouteAndFilters(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"https://www.amazon.com/s?k=coffee+cup", true},
		{"https://www.amazon.fr/s?i=kitchen&rh=p_89%3Atest", true},
		{"https://www.amazon.com/s?k=coffee&page=2", true},
		{"https://www.amazon.com/s?k=coffee&page=0", false},
		{"https://www.amazon.com/s?k=coffee&page=2&page=3", false},
		{"https://user@www.amazon.com/s?k=coffee", false},
		{"https://www.amazon.com.evil.test/s?k=coffee", false},
		{"https://www.amazon.com:8443/s?k=coffee", false},
		{"https://www.amazon.com/dp/B012345678", false},
		{"https://www.amazon.com/s", false},
	} {
		if got := amazonSearchValid(amazonSearchURL(t, tc.raw)); got != tc.ok {
			t.Errorf("valid %q = %v", tc.raw, got)
		}
	}
	for _, raw := range []string{"https://www.amazon.com/s", "https://www.amazon.com/s?k=coffee&page=2&page=3", "https://www.amazon.com:8443/s?k=coffee"} {
		if !isAmazonSearch(amazonSearchURL(t, raw)) {
			t.Errorf("invalid query bypassed Amazon search route: %q", raw)
		}
	}
	root := amazonSearchURL(t, "https://www.amazon.fr/s?k=mug&i=kitchen&rh=p_89%3Atest&s=price-asc-rank")
	for _, tc := range []struct {
		raw      string
		filterOK bool
		pageOK   bool
	}{
		{"https://www.amazon.fr/s?k=mug&i=kitchen&rh=p_89%3Atest&s=price-asc-rank&page=2&qid=123&ref=sr_pg_1&xpid=abc", true, false},
		{"https://www.amazon.fr/s?k=mug&i=kitchen&rh=p_89%3Atest&s=price-asc-rank&ref=sr_pg_1", true, true},
		{"https://www.amazon.fr/s?k=other&i=kitchen&rh=p_89%3Atest&s=price-asc-rank&page=2", false, false},
		{"https://www.amazon.fr/s?k=mug&i=kitchen&rh=p_89%3Aother&s=price-asc-rank&page=2", false, false},
		{"https://www.amazon.fr/s?k=mug&i=kitchen&s=price-asc-rank&page=2", false, false},
		{"https://www.amazon.fr/s?k=mug&i=kitchen&rh=p_89%3Atest&s=price-asc-rank&low-price=10&page=2", false, false},
		{"https://www.amazon.com/s?k=mug&i=kitchen&rh=p_89%3Atest&s=price-asc-rank", false, false},
		{"http://www.amazon.fr/s?k=mug&i=kitchen&rh=p_89%3Atest&s=price-asc-rank", false, false},
	} {
		u := amazonSearchURL(t, tc.raw)
		if got := amazonSearchSameFilter(root, u); got != tc.filterOK {
			t.Errorf("same filter %q = %v", tc.raw, got)
		}
		if got := amazonSearchSamePage(root, u); got != tc.pageOK {
			t.Errorf("same page %q = %v", tc.raw, got)
		}
	}
}

func TestAmazonSearchCardsAndHiddenContent(t *testing.T) {
	page := amazonSearchURL(t, "https://www.amazon.com/s?k=coffee")
	got, err := amazonSearchCollect(context.Background(), GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("last page must not trigger a request")
		return nil, nil
	}), page, amazonSearchParse(t, amazonSearchFixture))
	if err == nil || len(got.Items) != 1 {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	item := got.Items[0]
	if item.URL != "https://m.media-amazon.com/images/I/main._AC_UL1500_.jpg" || len(item.AlternateURLs) != 1 ||
		item.AlternateURLs[0] != "https://m.media-amazon.com/images/I/main._AC_UL320_.jpg" ||
		item.Metadata["album_id"] != "B012345678" || item.Metadata["album_title"] != "Full coffee mug title" ||
		!item.PublicOnly || !item.RequireImage || item.Headers["Referer"] != "https://www.amazon.com/" {
		t.Fatalf("item=%+v", item)
	}
}

func TestAmazonSearchPaginationStopsOnRateLimit(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			root := amazonSearchURL(t, "https://www.amazon.com/s?k=coffee&rh=p_89%3Atest")
			first := strings.Replace(amazonSearchFixture, `<span class="s-pagination-next s-pagination-disabled">next</span>`, `<a class="s-pagination-next" href="/s?k=coffee&amp;rh=p_89%3Atest&amp;page=2&amp;qid=111">next</a>`, 1)
			calls := []string{}
			getter := NewHTTPGetter("")
			getter.client.Transport = amazonSearchTransport(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.String())
				if len(calls) == 1 {
					return amazonSearchHTTPResponse(r, 200, first), nil
				}
				return amazonSearchHTTPResponse(r, status, "limited"), nil
			})
			got, err := discoverAmazonSearch(context.Background(), getter, root)
			if err == nil || !strings.Contains(err.Error(), strconv.Itoa(status)) || len(calls) != 2 || len(got.Items) != 1 {
				t.Fatalf("items=%+v calls=%v err=%v", got.Items, calls, err)
			}
		})
	}
}

func TestAmazonSearchRejectsChangedNextQuery(t *testing.T) {
	page := amazonSearchURL(t, "https://www.amazon.fr/s?k=tasse&rh=p_89%3Atest")
	doc := amazonSearchParse(t, strings.Replace(amazonSearchFixture, `<span class="s-pagination-next s-pagination-disabled">next</span>`, `<a class="s-pagination-next" href="/s?k=tasse&amp;rh=p_89%3Aother&amp;page=2">next</a>`, 1))
	got, err := amazonSearchCollect(context.Background(), GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("changed filter triggered a request")
		return nil, nil
	}), page, doc)
	if err == nil || !strings.Contains(err.Error(), "搜索条件") || len(got.Items) != 1 {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
}

type amazonSearchTransport func(*http.Request) (*http.Response, error)

func (f amazonSearchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func amazonSearchHTTPResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Request: r, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAmazonSearchRejectsUnknownGetter(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unsafe getter must not be used")
		return nil, nil
	})
	_, err := discoverAmazonSearch(context.Background(), getter, amazonSearchURL(t, "https://www.amazon.com/s?k=coffee"))
	if err == nil || !strings.Contains(err.Error(), "安全重定向") {
		t.Fatalf("unknown getter err=%v", err)
	}
}

func TestAmazonSearchStopsUnsafeRedirectBeforeRequest(t *testing.T) {
	var calls []string
	g := NewHTTPGetter("")
	g.client.Transport = amazonSearchTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://www.amazon.com/s?k=other"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	_, err := discoverAmazonSearch(context.Background(), g, amazonSearchURL(t, "https://www.amazon.com/s?k=coffee"))
	if err == nil || len(calls) != 1 {
		t.Fatalf("redirect calls=%v err=%v", calls, err)
	}
}

func TestAmazonSearchCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := discoverAmazonSearch(ctx, GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request was sent")
		return nil, nil
	}), amazonSearchURL(t, "https://www.amazon.com/s?k=coffee"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestAmazonSearchBlockedPageDoesNotAddStaleCards(t *testing.T) {
	page := amazonSearchURL(t, "https://www.amazon.com/s?k=coffee")
	doc := amazonSearchParse(t, strings.Replace(amazonSearchFixture, `</body>`, `<form action="/errors/validateCaptcha"></form></body>`, 1))
	got, err := amazonSearchCollect(context.Background(), GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("verification page triggered a request")
		return nil, nil
	}), page, doc)
	if err == nil || !strings.Contains(err.Error(), "验证") || len(got.Items) != 0 {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
}

func TestAmazonSearchCapturedPublicPages(t *testing.T) {
	firstPath, secondPath := os.Getenv("TULAI_AMAZON_SEARCH_FIXTURE"), os.Getenv("TULAI_AMAZON_SEARCH_FIXTURE2")
	if firstPath == "" || secondPath == "" {
		t.Skip("set public captured HTML fixture paths to verify offline")
	}
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	page := amazonSearchURL(t, "https://www.amazon.com/s?k=coffee+mug")
	requests := 0
	getter := NewHTTPGetter("")
	getter.client.Transport = amazonSearchTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		number, valid := amazonSearchPage(r.URL)
		if !valid || number != requests+1 || !amazonSearchSameFilter(page, r.URL) {
			t.Fatalf("unexpected public pagination url %q", r.URL)
		}
		if requests == 1 {
			return amazonSearchHTTPResponse(r, 200, string(second)), nil
		}
		return amazonSearchHTTPResponse(r, 429, "limited"), nil
	})
	doc := amazonSearchParse(t, string(first))
	var cards, visibleCards, images, visibleImages, matched int
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "div" && attribute(n, "data-component-type") == "s-search-result" {
			cards++
			if amazonSearchVisible(n) {
				visibleCards++
			}
			if _, ok := amazonSearchCard(page, n); ok {
				matched++
			}
		}
		if n.Data == "img" && amazonSearchClass(n, "s-image") {
			images++
			if amazonSearchVisible(n) {
				visibleImages++
			}
		}
	})
	t.Logf("public fixture cards=%d visible=%d images=%d visible=%d matched=%d", cards, visibleCards, images, visibleImages, matched)
	got, err := amazonSearchCollect(context.Background(), getter, page, doc)
	if err == nil || len(got.Items) < 40 || requests != 2 {
		t.Fatalf("public capture items=%d requests=%d err=%v", len(got.Items), requests, err)
	}
	t.Logf("public pages: %d product images, %d subsequent requests (second simulated as rate-limited); err=%v", len(got.Items), requests, err)
}
