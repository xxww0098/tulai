package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func baiduTestResponse(raw string, status int, body string, headers http.Header) *Response {
	if headers == nil {
		headers = http.Header{}
	}
	return &Response{StatusCode: status, FinalURL: raw, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}

func baiduTestPage(offset, count int) string {
	rows := make([]map[string]any, 0, count+1)
	for i := 0; i < count; i++ {
		rows = append(rows, map[string]any{"objURL": fmt.Sprintf("https://cdn.example.test/%d.jpg", offset+i)})
	}
	rows = append(rows, map[string]any{})
	body, _ := json.Marshal(map[string]any{"data": rows, "listNum": 10000})
	return string(body)
}

func TestBaiduSearchPagesOriginalsAndCookies(t *testing.T) {
	var calls []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		prepare(req)
		if req.URL.Scheme != "https" || req.URL.Host != "image.baidu.com" {
			t.Fatalf("unexpected search host %s", req.URL.Redacted())
		}
		calls = append(calls, req.URL.Path+"?pn="+req.URL.Query().Get("pn"))
		if req.URL.Path == "/" {
			if req.Header.Get("Cookie") != "" {
				t.Fatal("seed request sent cookies")
			}
			return baiduTestResponse(raw, 200, "seed", http.Header{"Set-Cookie": {"BAIDUID=test; Domain=.baidu.com; Path=/; Secure", "foreign=wrong; Domain=evil.test; Path=/"}}), nil
		}
		if req.URL.Path != "/search/acjson" || !strings.Contains(req.Header.Get("Cookie"), "BAIDUID=test") || strings.Contains(req.Header.Get("Cookie"), "foreign=") {
			t.Fatalf("API path or cookie = %s / %q", req.URL.Path, req.Header.Get("Cookie"))
		}
		if req.URL.Query().Get("word") != "郁金香" || req.URL.Query().Get("rn") != "30" || req.URL.Query().Get("tn") != "resultjson_com" {
			t.Fatalf("query = %v", req.URL.Query())
		}
		if req.URL.Query().Get("pn") == "0" {
			var data struct {
				Data       []map[string]any `json:"data"`
				DisplayNum int              `json:"displayNum"`
			}
			if err := json.Unmarshal([]byte(baiduTestPage(0, 30)), &data); err != nil {
				t.Fatal(err)
			}
			data.Data[0] = map[string]any{"objURL": "ipprf_z2C$qAzdH3FAzdH3Fkwt17_z&e3Bv54AzdH3Fpjfp_z&e3B3r2"}
			data.Data[1] = map[string]any{"objURL": "https://baidu.com/test.jpg"}
			data.DisplayNum = 33
			body, _ := json.Marshal(data)
			return baiduTestResponse(raw, 200, string(body), http.Header{"Set-Cookie": {"APISID=next; Domain=image.baidu.com; Path=/; Secure"}}), nil
		}
		if !strings.Contains(req.Header.Get("Cookie"), "APISID=next") {
			t.Fatalf("updated cookie missing: %q", req.Header.Get("Cookie"))
		}
		return baiduTestResponse(raw, 200, `{"data":[{"replaceUrl":[{"ObjURL":"https://images.example/original.jpg"}],"fromURL":"https://source.example/post?secret=1"},{"middleURL":"https://images.example/medium.jpg"},{"objURL":null},{}]}`, nil), nil
	})
	result, err := Discover(context.Background(), getter, "https://image.baidu.com/search/index?word=%E9%83%81%E9%87%91%E9%A6%99&token=private", 1)
	if err == nil || !strings.Contains(err.Error(), "1 条结果缺少") || !result.IsPage || len(result.Items) != 31 {
		t.Fatalf("result items=%d, err=%v", len(result.Items), err)
	}
	if got := strings.Join(calls, ","); got != "/?pn=,/search/acjson?pn=0,/search/acjson?pn=30" {
		t.Fatalf("calls = %s", got)
	}
	if result.Items[0].URL != "https://baidu.com/test.jpg" || result.Items[29].URL != "https://images.example/original.jpg" {
		t.Fatalf("original images = %+v / %+v", result.Items[0], result.Items[29])
	}
	if ref := result.Items[29].Headers["Referer"]; ref != "https://source.example/" {
		t.Fatalf("cross-origin referer = %q", ref)
	}
	for _, item := range result.Items {
		if item.Headers["Cookie"] != "" || strings.Contains(item.Headers["Referer"], "token=private") {
			t.Fatalf("credentials leaked to image request: %+v", item)
		}
	}
}

func TestBaiduSearchTruncationAndPartialFailure(t *testing.T) {
	for _, failAt := range []int{-1, 1} {
		t.Run(strconv.Itoa(failAt), func(t *testing.T) {
			pages := 0
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Path == "/" {
					return baiduTestResponse(raw, 200, "seed", nil), nil
				}
				page := pages
				pages++
				if page == failAt {
					return baiduTestResponse(raw, http.StatusServiceUnavailable, "", nil), nil
				}
				return baiduTestResponse(raw, 200, baiduTestPage(page*baiduPageSize, baiduPageSize), nil), nil
			})
			got, err := Discover(context.Background(), getter, "https://image.baidu.com/search/index?word=test", 1)
			if failAt < 0 {
				if err == nil || !strings.Contains(err.Error(), "抓取上限") || pages != maxCrawlPages || len(got.Items) != maxCrawlPages*baiduPageSize {
					t.Fatalf("limit: %d pages, %d items, %v", pages, len(got.Items), err)
				}
			} else {
				var status *HTTPStatusError
				if !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable || pages != 2 || len(got.Items) != 30 {
					t.Fatalf("partial: %d pages, %d items, %v", pages, len(got.Items), err)
				}
			}
		})
	}
}

func TestBaiduSearchDisplayNumPagination(t *testing.T) {
	for _, tc := range []struct {
		name           string
		total          int
		display        any
		start          int
		partialFirst   bool
		wantPages      int
		wantImageCount int
	}{
		{"zero", 0, 0, 0, false, 1, 0},
		{"less than one page", 5, 5, 0, false, 1, 5},
		{"exactly one page", 30, 30, 0, false, 2, 30},
		{"two pages", 31, 31, 0, false, 2, 31},
		{"string exact boundary", 60, "60", 0, false, 3, 60},
		{"missing count", 31, nil, 0, false, 3, 31},
		{"invalid count", 31, "unknown", 0, false, 3, 31},
		{"partial page", 31, 31, 0, true, 2, 3},
		{"nonzero start", 91, 91, 60, false, 2, 31},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var offsets []int
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Path == "/" {
					return baiduTestResponse(raw, 200, "seed", nil), nil
				}
				offset, _ := strconv.Atoi(u.Query().Get("pn"))
				offsets = append(offsets, offset)
				count := min(baiduPageSize, max(0, tc.total-offset))
				if tc.partialFirst && offset == 0 {
					count = 2
				}
				var body map[string]any
				if err := json.Unmarshal([]byte(baiduTestPage(offset, count)), &body); err != nil {
					t.Fatal(err)
				}
				if tc.display != nil {
					body["displayNum"] = tc.display
				}
				encoded, _ := json.Marshal(body)
				return baiduTestResponse(raw, 200, string(encoded), nil), nil
			})
			raw := fmt.Sprintf("https://image.baidu.com/search/index?word=test&pn=%d", tc.start)
			got, err := Discover(context.Background(), getter, raw, 1)
			if err != nil || len(got.Items) != tc.wantImageCount || len(offsets) != tc.wantPages {
				t.Fatalf("offsets=%v, images=%d, err=%v", offsets, len(got.Items), err)
			}
			for i, offset := range offsets {
				if offset != tc.start+i*baiduPageSize {
					t.Errorf("offset %d = %d", i, offset)
				}
			}
		})
	}
}

type cancelCrawlBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelCrawlBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

func TestBaiduSearchCancellationPreservesPartial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Path == "/" {
			return baiduTestResponse(raw, 200, "seed", nil), nil
		}
		resp := baiduTestResponse(raw, 200, baiduTestPage(0, 30), nil)
		resp.Body = &cancelCrawlBody{ReadCloser: resp.Body, cancel: cancel}
		return resp, nil
	})
	got, err := Discover(ctx, getter, "https://image.baidu.com/search/index?word=test", 1)
	if !errors.Is(err, context.Canceled) || len(got.Items) != 30 {
		t.Fatalf("canceled result %d items, %v", len(got.Items), err)
	}
}

func TestBaiduSearchFallbackAndCookieBoundary(t *testing.T) {
	requests := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		requests++
		if req.URL.Path == "/search/acjson" {
			return baiduTestResponse(raw, 200, `{"antiFlag":1,"data":[]}`, nil), nil
		}
		return baiduTestResponse(raw, 200, `<html><img src="https://cdn.example.test/fallback.jpg"></html>`, http.Header{"Content-Type": {"text/html"}}), nil
	})
	got, err := Discover(context.Background(), getter, "https://image.baidu.com/search/index?word=test", 1)
	if err == nil || !strings.Contains(err.Error(), "可能需要验证") || requests != 3 || len(got.Items) != 1 || got.Items[0].URL != "https://cdn.example.test/fallback.jpg" {
		t.Fatalf("fallback: requests=%d, result=%+v, err=%v", requests, got, err)
	}
	jar, _ := cookiejar.New(nil)
	api, _ := url.Parse("https://image.baidu.com/search/acjson")
	jar.SetCookies(api, []*http.Cookie{{Name: "BAIDUID", Value: "private", Path: "/", Secure: true}})
	malicious := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://evil.test/search/acjson", nil)
		prepare(req)
		if req.Header.Get("Cookie") != "" {
			t.Fatal("cookie sent outside the exact search host")
		}
		return baiduTestResponse(req.URL.String(), 200, "{}", nil), nil
	})
	if _, err := baiduRequest(context.Background(), malicious, api, api, jar, true); err == nil {
		t.Fatal("cross-origin response accepted")
	}
}

func TestBaiduSearchRecognizesOnlyKeywordPages(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://image.baidu.com/search/index?word=cat":                                                 true,
		"https://image.baidu.com/search/index?queryWord=cat":                                            true,
		"https://image.baidu.com/search/index?word=cat&queryImageUrl=https%3A%2F%2Fexample.com%2Fa.jpg": false,
		"https://image.baidu.com/search/acjson?word=cat":                                                false,
		"https://image.baidu.com.evil.test/search/index?word=cat":                                       false,
		"https://image.baidu.com:8443/search/index?word=cat":                                            false,
	} {
		u, _ := url.Parse(raw)
		if got := isBaiduImageSearch(u); got != want {
			t.Errorf("isBaiduImageSearch(%s) = %v", raw, got)
		}
	}
}
