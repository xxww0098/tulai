package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func album1688Response(raw string, code int, body string, header http.Header) *Response {
	if header == nil {
		header = http.Header{"Content-Type": {"text/html; charset=utf-8"}}
	}
	return &Response{StatusCode: code, FinalURL: raw, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func album1688Page(start, count, total int) string {
	albums := make([]map[string]any, 0, count)
	for i := start; i < start+count; i++ {
		albums = append(albums, map[string]any{"id": i, "title": fmt.Sprintf("相册%d", i), "count": i + 10})
	}
	data, _ := json.Marshal(map[string]any{"dataList": map[string]any{"albums": albums, "total": total}})
	return string(data)
}

func Test1688AlbumsPaginationDetailsAndPrivacy(t *testing.T) {
	var pages []string
	var details int
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Cookie", "session=private")
		req.Header.Set("Authorization", "Bearer private")
		prepare(req)
		switch {
		case req.URL.Host == "shop.1688.com" && req.URL.Path == "/page/albumlist.htm":
			return album1688Response(raw, 200, `<title>店铺相册</title><script>var site = {member_id: "b2b-123"};</script>`, nil), nil
		case req.URL.Host == "picman.1688.com" && req.URL.Path == "/open/ajax/RecommendAlbumDetailList.json":
			if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "https://shop.1688.com/" {
				t.Fatalf("API credentials/referrer: %v", req.Header)
			}
			q := req.URL.Query()
			if q.Get("memberId") != "b2b-123" || q.Get("hasAlbums") != "true" {
				t.Fatalf("API query = %v", q)
			}
			pages = append(pages, q.Get("start")+"-"+q.Get("end"))
			if q.Get("start") == "1" && q.Get("end") == "20" && q.Get("jsResponseDataName") == "albumList1" {
				return album1688Response(raw, 200, "albumList1("+album1688Page(1, 20, 22)+");", nil), nil
			}
			if q.Get("start") != "21" || q.Get("end") != "40" || q.Get("jsResponseDataName") != "albumList21" {
				t.Fatalf("second API query = %v", q)
			}
			return album1688Response(raw, 200, `{"dataList":{"total":"22","albums":[{"id":"21","title":"最后相册","count":8},{"title":"缺少编号"}]}}`, nil), nil
		case req.URL.Host == "shop.1688.com" && strings.HasPrefix(req.URL.Path, "/app/albumDetail_"):
			details++
			id := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/app/albumDetail_"), ".htm")
			if id == "1" {
				return album1688Response(raw, 200, `<base href="https://cdn.example.test/images/"><img src="/outside.jpg"><div class="selected img-wrapper"><img src="first.jpg?x=1"><img src="second.jpg"></div>`, nil), nil
			}
			if id == "2" || id == "3" {
				return album1688Response(raw, 200, `<div class="img-wrapper"><img src="/shared.jpg"></div>`, nil), nil
			}
			return album1688Response(raw, 200, fmt.Sprintf(`<div class="img-wrapper"><img src="../images/%s.jpg"></div>`, id), nil), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.Redacted())
			return nil, errors.New("unexpected request")
		}
	})
	got, err := Discover(context.Background(), getter, "https://shop.1688.com/page/albumlist.htm?token=private", 1)
	if err == nil || !strings.Contains(err.Error(), "缺少有效 ID") || !got.IsPage || got.Title != "店铺相册" || len(got.Items) != 20 || details != 21 || strings.Join(pages, ",") != "1-20,21-40" {
		t.Fatalf("result=%+v, details=%d, pages=%v, err=%v", got, details, pages, err)
	}
	first := got.Items[0]
	if first.URL != "https://cdn.example.test/images/first.jpg?x=1" || first.Headers["Referer"] != "https://shop.1688.com/" || first.Metadata["album_id"] != "1" || first.Metadata["album_title"] != "相册1" || first.Metadata["album_count"] != "11" {
		t.Fatalf("first image = %+v", first)
	}
	if got.Items[1].URL != "https://shop.1688.com/shared.jpg" || got.Items[1].Headers["Referer"] != "https://shop.1688.com/app/albumDetail_2.htm" {
		t.Fatalf("relative image = %+v", got.Items[1])
	}
	for _, item := range got.Items {
		if item.Headers["Cookie"] != "" || strings.Contains(item.Headers["Referer"], "token=private") {
			t.Fatalf("image request leaked credentials: %+v", item)
		}
	}
}

func Test1688AlbumResponseDecoding(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"json", `{"dataList":{"albums":[]}}`, true},
		{"jsonp", `albumList1({"dataList":{"albums":[]}});`, true},
		{"assignment", `var albumList1 = {"dataList":{"albums":[]}};`, true},
		{"wrong callback", `other({"dataList":{"albums":[]}});`, false},
		{"extra script", `albumList1({"dataList":{"albums":[]}});danger()`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var target map[string]any
			err := album1688Decode([]byte(tc.body), "albumList1", &target)
			if (err == nil) != tc.valid {
				t.Fatalf("decode err=%v", err)
			}
		})
	}
}

func Test1688AlbumsCancellationAndLimit(t *testing.T) {
	for _, cancelAt := range []int{2, -1} {
		t.Run(strconv.Itoa(cancelAt), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			details, pages := 0, 0
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				switch {
				case u.Path == "/page/albumlist.htm":
					return album1688Response(raw, 200, `<script>member_id: 'b2b-1'</script>`, nil), nil
				case u.Host == "picman.1688.com":
					pages++
					start, _ := strconv.Atoi(u.Query().Get("start"))
					return album1688Response(raw, 200, album1688Page(start, 20, 10000), nil), nil
				case strings.HasPrefix(u.Path, "/app/albumDetail_"):
					details++
					if details == cancelAt {
						cancel()
						return nil, ctx.Err()
					}
					return album1688Response(raw, 200, fmt.Sprintf(`<div class="img-wrapper"><img src="/%d.jpg"></div>`, details), nil), nil
				}
				return nil, errors.New("unexpected request")
			})
			got, err := Discover(ctx, getter, "https://shop.1688.com/page/albumlist.htm", 1)
			if cancelAt > 0 {
				if !errors.Is(err, context.Canceled) || len(got.Items) != 1 || details != 2 {
					t.Fatalf("canceled: items=%d, details=%d, err=%v", len(got.Items), details, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "扫描上限") || len(got.Items) != max1688Albums || details != max1688Albums || pages != max1688Albums/album1688PageSize+1 {
				t.Fatalf("limit: items=%d, details=%d, pages=%d, err=%v", len(got.Items), details, pages, err)
			}
		})
	}
}

func Test1688AlbumsLaterPageFailurePreservesResults(t *testing.T) {
	pages := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		switch {
		case u.Path == "/page/albumlist.htm":
			return album1688Response(raw, 200, `<script>member_id: "b2b-1"</script>`, nil), nil
		case u.Host == "picman.1688.com":
			pages++
			if pages == 2 {
				return album1688Response(raw, http.StatusServiceUnavailable, "", nil), nil
			}
			return album1688Response(raw, 200, album1688Page(1, 20, 40), nil), nil
		default:
			id := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/app/albumDetail_"), ".htm")
			return album1688Response(raw, 200, fmt.Sprintf(`<div class="img-wrapper"><img src="/%s.jpg"></div>`, id), nil), nil
		}
	})
	got, err := Discover(context.Background(), getter, "https://shop.1688.com/page/albumlist.htm", 1)
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable || pages != 2 || len(got.Items) != 20 {
		t.Fatalf("partial: items=%d, pages=%d, err=%v", len(got.Items), pages, err)
	}
}

func Test1688AlbumsEmptyPageBeforeTotalPreservesResults(t *testing.T) {
	pages := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		switch {
		case u.Path == "/page/albumlist.htm":
			return album1688Response(raw, 200, `<script>member_id: "b2b-1"</script>`, nil), nil
		case u.Host == "picman.1688.com":
			pages++
			if pages == 2 {
				return album1688Response(raw, 200, `{"total":40,"dataList":{"albums":[]}}`, nil), nil
			}
			return album1688Response(raw, 200, album1688Page(1, 20, 40), nil), nil
		default:
			id := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/app/albumDetail_"), ".htm")
			return album1688Response(raw, 200, fmt.Sprintf(`<div class="img-wrapper"><img src="/%s.jpg"></div>`, id), nil), nil
		}
	})
	got, err := Discover(context.Background(), getter, "https://shop.1688.com/page/albumlist.htm", 1)
	if err == nil || !strings.Contains(err.Error(), "提前结束") || len(got.Items) != 20 || pages != 2 {
		t.Fatalf("partial: items=%d pages=%d err=%v", len(got.Items), pages, err)
	}
}

func Test1688AlbumFallbackAndDomainSafety(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://shop.1688.com/page/albumlist.htm":           true,
		"https://a.shop.1688.com/page/albumlist.htm":         true,
		"https://1688.com/page/albumlist.htm":                false,
		"https://shop.1688.com.evil.test/page/albumlist.htm": false,
		"https://shop.1688.com@evil.test/page/albumlist.htm": false,
		"https://evil1688.com/page/albumlist.htm":            false,
		"https://shop..1688.com/page/albumlist.htm":          false,
		"https://shop.1688.com/page/offerlist.htm":           false,
	} {
		u, _ := url.Parse(raw)
		if got := is1688AlbumList(u); got != want {
			t.Errorf("is1688AlbumList(%s) = %v", raw, got)
		}
	}
	requests := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		requests++
		return album1688Response(raw, 200, `<img src="/fallback.jpg">`, nil), nil
	})
	got, err := Discover(context.Background(), getter, "https://shop.1688.com/page/albumlist.htm", 1)
	if err == nil || !strings.Contains(err.Error(), "member_id") || requests != 2 || len(got.Items) != 1 || got.Items[0].URL != "https://shop.1688.com/fallback.jpg" {
		t.Fatalf("fallback: requests=%d, result=%+v, err=%v", requests, got, err)
	}
}

func Test1688DirectAlbumStartsAtFirstImage(t *testing.T) {
	for _, raw := range []string{
		"https://shop.1688.com/app/albumDetail_42.htm?imageNum=3",
		"https://shop.1688.com/page/albumdetail_42.htm?imageNum=3",
	} {
		var requests []string
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			requests = append(requests, raw)
			u, _ := url.Parse(raw)
			if len(requests) == 1 {
				if u.Path != "/app/albumDetail_42.htm" || u.Query().Has("imageNum") {
					t.Fatalf("first page = %s", raw)
				}
				return album1688Response(raw, 200, `<div class="big-img-field"><div class="img-wrapper"><img src="/one.jpg"></div><a class="right" href="/page/albumdetail_42.htm?imageNum=2">下一张</a></div>`, nil), nil
			}
			return album1688Response(raw, 200, `<div class="big-img-field"><div class="img-wrapper"><img src="/two.jpg"></div></div>`, nil), nil
		})
		got, err := Discover(context.Background(), getter, raw, 1)
		if err != nil || len(requests) != 2 || len(got.Items) != 2 || got.Items[0].URL != "https://shop.1688.com/one.jpg" || got.Items[1].URL != "https://shop.1688.com/two.jpg" {
			t.Fatalf("direct album = %+v, requests=%v, err=%v", got.Items, requests, err)
		}
	}
}

func Test1688DirectAlbumRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{
		"https://shop.1688.com.evil.test/app/albumDetail_42.htm",
		"https://user@shop.1688.com/app/albumDetail_42.htm",
		"https://shop.1688.com/app/albumDetail_../42.htm",
		"https://shop.1688.com/page/offerlist.htm",
	} {
		u, _ := url.Parse(raw)
		if id := album1688DetailID(u); id != "" {
			t.Errorf("unexpected album id %q for %s", id, raw)
		}
	}
}

func Test1688AlbumRejectsUnsafeIDsAndRedirects(t *testing.T) {
	for _, scenario := range []string{"invalid id", "root redirect", "api redirect", "detail redirect"} {
		t.Run(scenario, func(t *testing.T) {
			var detailCalls int
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				switch {
				case u.Path == "/page/albumlist.htm":
					response := album1688Response(raw, 200, `<script>member_id: "b2b-1"</script>`, nil)
					if scenario == "root redirect" {
						response.FinalURL = "https://evil.test/page"
					}
					return response, nil
				case u.Host == "picman.1688.com":
					id := "1"
					if scenario == "invalid id" {
						id = "../escape"
					}
					response := album1688Response(raw, 200, fmt.Sprintf(`{"dataList":{"total":1,"albums":[{"id":%q}]}}`, id), nil)
					if scenario == "api redirect" {
						response.FinalURL = "https://evil.test/data"
					}
					return response, nil
				case strings.HasPrefix(u.Path, "/app/albumDetail_"):
					detailCalls++
					response := album1688Response(raw, 200, `<div class="img-wrapper"><img src="/x.jpg"></div>`, nil)
					if scenario == "detail redirect" {
						response.FinalURL = "https://evil.test/page"
					}
					return response, nil
				}
				return nil, errors.New("unexpected request")
			})
			got, err := Discover(context.Background(), getter, "https://shop.1688.com/page/albumlist.htm", 1)
			if err == nil || len(got.Items) != 0 || (scenario != "detail redirect" && detailCalls != 0) || (scenario == "detail redirect" && detailCalls != 1) {
				t.Fatalf("unsafe result=%+v, details=%d, err=%v", got.Items, detailCalls, err)
			}
		})
	}
}

func Test1688AlbumReportsUnknownOrIncompleteCount(t *testing.T) {
	for _, tc := range []struct {
		name, count string
		wantErr     string
	}{
		{"single", `,"count":1`, ""},
		{"multiple", `,"count":3`, "图片数量"},
		{"missing", ``, "完整性未知"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				switch {
				case u.Path == "/page/albumlist.htm":
					return album1688Response(raw, 200, `<script>member_id: "b2b-1"</script>`, nil), nil
				case u.Host == "picman.1688.com":
					return album1688Response(raw, 200, fmt.Sprintf(`{"dataList":{"total":1,"albums":[{"id":1%s}]}}`, tc.count), nil), nil
				default:
					return album1688Response(raw, 200, `<div class="img-wrapper"><img src="/first.jpg"><img src="/second.jpg"></div>`, nil), nil
				}
			})
			got, err := Discover(context.Background(), getter, "https://shop.1688.com/page/albumlist.htm", 1)
			if len(got.Items) != 1 || got.Items[0].URL != "https://shop.1688.com/first.jpg" || (tc.wantErr == "" && err != nil) || (tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr))) {
				t.Fatalf("items=%+v, err=%v", got.Items, err)
			}
		})
	}
}

func Test1688AlbumFollowsPhotoPagesAndKeepsPartialResults(t *testing.T) {
	for _, mode := range []string{"complete", "failure", "cancel", "external", "other-album", "repeat"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var pages []string
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				switch {
				case u.Path == "/page/albumlist.htm":
					return album1688Response(raw, 200, `<script>member_id: "b2b-1"</script>`, nil), nil
				case u.Host == "picman.1688.com":
					return album1688Response(raw, 200, `{"dataList":{"total":1,"albums":[{"id":42,"title":"album","count":3}]}}`, nil), nil
				}
				n := u.Query().Get("imageNum")
				if n == "" {
					n = "1"
				}
				pages = append(pages, n)
				if mode == "failure" && n == "2" {
					return album1688Response(raw, 503, "", nil), nil
				}
				if mode == "cancel" && n == "2" {
					cancel()
					return nil, ctx.Err()
				}
				next := ""
				if n != "3" {
					nextNum, _ := strconv.Atoi(n)
					href := fmt.Sprintf("/page/albumdetail_42.htm?imageNum=%d&discard=private", nextNum+1)
					switch mode {
					case "external":
						href = "https://evil.test/page/albumdetail_42.htm?imageNum=2"
					case "other-album":
						href = "/page/albumdetail_43.htm?imageNum=2"
					case "repeat":
						href = "/page/albumdetail_42.htm?imageNum=1"
					}
					next = fmt.Sprintf(`<a class="right" href="%s">next</a>`, href)
				}
				if u.Query().Get("discard") != "" {
					t.Fatal("unexpected query parameter forwarded to next photo")
				}
				response := album1688Response(raw, 200, fmt.Sprintf(`<div class="big-img-field">%s<div class="img-wrapper"><img src="/photo%s.jpg"></div></div>`, next, n), nil)
				if n == "1" {
					response.FinalURL = "https://shop.1688.com/page/albumdetail_42.htm"
				}
				return response, nil
			})
			got, err := Discover(ctx, getter, "https://shop.1688.com/page/albumlist.htm", 1)
			if mode == "complete" {
				if err != nil || len(got.Items) != 3 || strings.Join(pages, ",") != "1,2,3" {
					t.Fatalf("complete: images=%+v pages=%v err=%v", got.Items, pages, err)
				}
				return
			}
			if err == nil || len(got.Items) != 1 || got.Items[0].URL != "https://shop.1688.com/photo1.jpg" {
				t.Fatalf("partial: images=%+v pages=%v err=%v", got.Items, pages, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			if mode == "failure" {
				var status *HTTPStatusError
				if !errors.As(err, &status) || status.StatusCode != 503 {
					t.Fatalf("failure status: %v", err)
				}
			}
			if (mode == "external" || mode == "other-album" || mode == "repeat") && len(pages) != 1 {
				t.Fatalf("unsafe pagination followed: %v", pages)
			}
		})
	}
}

func Test1688AlbumLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_1688_LIVE") != "1" {
		t.Skip("set TULAI_1688_LIVE=1 to check a public 1688 album")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	found, err := Discover(ctx, NewHTTPGetter(""), "https://rorolifestyle.1688.com/page/albumdetail_405009015.htm?imageNum=3", 1)
	unique := map[string]bool{}
	for _, image := range found.Items {
		unique[image.URL] = true
	}
	if err != nil || len(found.Items) != 7 || len(unique) != 7 {
		t.Fatalf("public album: %d images, %d unique, %v", len(found.Items), len(unique), err)
	}
	dir := t.TempDir()
	first := found.Items[0]
	first.Filename, first.RequireImage = "image.jpg", true
	err = New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, first, nil)
	if stat, statErr := os.Stat(filepath.Join(dir, "image.jpg")); err != nil || statErr != nil || stat.Size() == 0 {
		t.Fatalf("public album image download: %v, stat=%v", err, statErr)
	}
}

func Test1688AlbumPhotoLimitAndEmptyAlbum(t *testing.T) {
	detail, _ := url.Parse("https://shop.1688.com/app/albumDetail_42.htm")
	source, _ := url.Parse("https://shop.1688.com/page/albumlist.htm")
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		u, _ := url.Parse(raw)
		switch {
		case u.Path == "/page/albumlist.htm":
			return album1688Response(raw, 200, `<script>member_id: "b2b-1"</script>`, nil), nil
		case u.Host == "picman.1688.com":
			return album1688Response(raw, 200, `{"dataList":{"total":1,"albums":[{"id":42,"count":0}]}}`, nil), nil
		}
		body := fmt.Sprintf(`<div class="big-img-field"><a class="right" href="/page/albumdetail_42.htm?imageNum=%d"></a><div class="img-wrapper"><img src="/%d.jpg"></div></div>`, calls+1, calls)
		return album1688Response(raw, 200, body, nil), nil
	})
	got, err := Discover(context.Background(), getter, source.String(), 1)
	if err != nil || len(got.Items) != 0 || calls != 2 {
		t.Fatalf("empty album: items=%v calls=%d err=%v", got.Items, calls, err)
	}
	calls = 0
	images, err := album1688Detail(context.Background(), getter, detail, source, "42")
	if err == nil || !strings.Contains(err.Error(), "扫描上限") || len(images) != max1688AlbumImages || calls != max1688AlbumImages {
		t.Fatalf("photo limit: images=%d calls=%d err=%v", len(images), calls, err)
	}
}
