package download

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const fiveHundredChinaLegacyID = "07c6ad7035384d2389da2a8e4d0f3535"
const fiveHundredChinaModernID = "F0gltfbWAC"
const fiveHundredChinaImage = "https://cdn-resize-prod-cn.500px.me/photo/83619245/0.jpg?size=synthetic"

func chinaResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func Test500pxChinaURLs(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		id     string
		legacy bool
	}{
		{"https://500px.com.cn/community/photo-details/" + fiveHundredChinaLegacyID, fiveHundredChinaLegacyID, true},
		{"https://500px.me/community/photo-details/" + fiveHundredChinaLegacyID, fiveHundredChinaLegacyID, true},
		{"https://500px.com.cn/photo-details/" + fiveHundredChinaLegacyID, fiveHundredChinaLegacyID, true},
		{"https://www.500px.com.cn/photo/" + fiveHundredChinaModernID + "/landscape", fiveHundredChinaModernID, false},
		{"https://500px.com.cn/gallery/Gallery123", "", false},
		{"https://500px.com.cn/community/set/123456/details", "", false},
		{"https://500px.com.cn/photo/1234/slug/extra", "", false},
		{"https://user@500px.com.cn/photo/1234", "", false},
		{"https://500px.com.cn.evil.test/photo/1234", "", false},
		{"https://500px.com.cn:8443/photo/1234", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := fiveHundredChinaPageRef(u)
		if got.id != tc.id || got.legacy != tc.legacy || is500pxChinaPhoto(u) != (tc.id != "") {
			t.Errorf("page %q: %+v", tc.raw, got)
		}
	}
}

func Test500pxChinaLegacyPage(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		legacy bool
	}{
		{"https://500px.com.cn/community/photo-details/" + fiveHundredChinaLegacyID, true},
		{"https://500px.me/photo-details/" + fiveHundredChinaLegacyID, true},
		{"https://500px.com.cn/community/set/7421f55871e647eca42e9416ebc5d6a5/details", true},
		{"https://500px.com.cn/photo/" + fiveHundredChinaModernID, false},
		{"https://500px.com.cn/gallery/DrmHy1VGTF", false},
		{"https://evil.test/community/photo-details/" + fiveHundredChinaLegacyID, false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if is500pxChinaLegacyPage(u) != tc.legacy {
			t.Errorf("legacy page %q", tc.raw)
		}
	}
}

func Test500pxChinaLegacyAndModernPhoto(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		path := "/photo/" + fiveHundredChinaModernID
		if legacy {
			path = "/community/photo-details/" + fiveHundredChinaLegacyID
		}
		root, _ := url.Parse("https://500px.com.cn" + path + "?token=private-sentinel&legacyId=wrong#private")
		calls := 0
		var device string
		getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
			calls++
			u, err := url.Parse(raw)
			if err != nil || u.Scheme != "https" || u.Host != "api-neo.500px.com.cn" || u.Path != "/graphql" || strings.Contains(raw, "private-sentinel") {
				t.Fatalf("bad API request: %q, %v", raw, err)
			}
			q := u.Query()
			if len(q) != 3 {
				t.Fatalf("unexpected API parameters: %v", q)
			}
			req, _ := http.NewRequest("GET", raw, nil)
			req.Header.Set("Cookie", "synthetic")
			req.Header.Set("Authorization", "synthetic")
			req.Header.Set("Referer", root.String())
			prepare(req)
			if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "" {
				t.Fatalf("credentials leaked: %v", req.Header)
			}
			if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(req.Header.Get("x-500px-device-id")) {
				t.Fatal("missing random UUID")
			}
			if device != "" && device != req.Header.Get("x-500px-device-id") {
				t.Fatal("device changed within request")
			}
			device = req.Header.Get("x-500px-device-id")
			var variables map[string]string
			if err := json.Unmarshal([]byte(q.Get("variables")), &variables); err != nil {
				t.Fatal(err)
			}
			switch q.Get("operationName") {
			case "resolveLegacyPhotoOrVideo":
				if !legacy || variables["legacyId"] != fiveHundredChinaLegacyID || q.Get("query") != fiveHundredChinaResolve {
					t.Fatal("wrong legacy request")
				}
				return chinaResponse(raw, `{"data":{"photo":{"id":"`+fiveHundredChinaModernID+`"},"video":null}}`), nil
			case "getPhotoById":
				if variables["id"] != fiveHundredChinaModernID || q.Get("query") != fiveHundredChinaPhoto {
					t.Fatal("wrong photo request")
				}
				return chinaResponse(raw, `{"data":{"getPhotoById":{"id":"`+fiveHundredChinaModernID+`","title":"Photo","urls":{"size_4k":"`+fiveHundredChinaImage+`"}}}}`), nil
			default:
				t.Fatal("unknown operation")
			}
			return nil, nil
		})
		result, err := discover500pxChinaPhoto(context.Background(), getter, root)
		wantCalls := 1
		if legacy {
			wantCalls = 2
		}
		if err != nil || calls != wantCalls || result.Title != "Photo" || len(result.Items) != 1 || result.Items[0].URL != fiveHundredChinaImage || result.Items[0].Headers["Referer"] != "https://500px.com.cn/" {
			t.Fatalf("legacy=%v calls=%d result=%+v error=%v", legacy, calls, result, err)
		}
	}
}

func Test500pxChinaIdentityAndPartial(t *testing.T) {
	root, _ := url.Parse("https://500px.com.cn/photo/" + fiveHundredChinaModernID)
	for _, tc := range []struct {
		name, body string
		count      int
		wantError  bool
	}{
		{"different-id", `{"data":{"getPhotoById":{"id":"OtherId","urls":{"size_4k":"` + fiveHundredChinaImage + `"}}}}`, 0, true},
		{"unsafe-cdn", `{"data":{"getPhotoById":{"id":"` + fiveHundredChinaModernID + `","urls":{"size_4k":"https://cdn-resize-prod-cn.500px.me.evil.test/photo/83619245/0.jpg"}}}}`, 0, true},
		{"credentials", `{"data":{"getPhotoById":{"id":"` + fiveHundredChinaModernID + `","urls":{"size_4k":"https://user@cdn-resize-prod-cn.500px.me/photo/83619245/0.jpg"}}}}`, 0, true},
		{"port", `{"data":{"getPhotoById":{"id":"` + fiveHundredChinaModernID + `","urls":{"size_4k":"https://cdn-resize-prod-cn.500px.me:8443/photo/83619245/0.jpg"}}}}`, 0, true},
		{"bad-json", `not json`, 0, true},
		{"unavailable", `{"errors":[{"message":"no access"}],"data":null}`, 0, true},
		{"partial", `{"errors":[{"message":"partial"}],"data":{"getPhotoById":{"id":"` + fiveHundredChinaModernID + `","urls":{"size_600":"` + fiveHundredChinaImage + `"}}}}`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return chinaResponse(raw, tc.body), nil
			})
			got, err := discover500pxChinaPhoto(context.Background(), getter, root)
			if len(got.Items) != tc.count || (err != nil) != tc.wantError {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func Test500pxChinaCancellationRedirectAndSize(t *testing.T) {
	root, _ := url.Parse("https://500px.com.cn/photo/" + fiveHundredChinaModernID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discover500pxChinaPhoto(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	for _, kind := range []string{"redirect", "oversize", "changed-query"} {
		t.Run(kind, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				resp := chinaResponse(raw, `{"data":{}}`)
				switch kind {
				case "redirect":
					resp.FinalURL = "https://evil.test/graphql"
				case "changed-query":
					resp.FinalURL = raw + "&extra=1"
				case "oversize":
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", fiveHundredChinaMaxJSON+1)))
				}
				return resp, nil
			})
			if got, err := discover500pxChinaPhoto(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}

type chinaRoundTripper func(*http.Request) (*http.Response, error)

func (fn chinaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func Test500pxChinaHTTPGetterDoesNotForwardCookiesOrRedirect(t *testing.T) {
	root, _ := url.Parse("https://500px.com.cn/photo/" + fiveHundredChinaModernID)
	getter := NewHTTPGetter("")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	api, _ := url.Parse(fiveHundredChinaAPI)
	jar.SetCookies(api, []*http.Cookie{{Name: "private", Value: "synthetic", Path: "/"}})
	getter.client.Jar = jar
	calls := 0
	getter.client.Transport = chinaRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "api-neo.500px.com.cn" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Fatalf("unsafe request: %v %v", req.URL, req.Header)
		}
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://evil.test/graphql"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	got, err := discover500pxChinaPhoto(context.Background(), getter, root)
	if err == nil || len(got.Items) != 0 || calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", got, err, calls)
	}
	if getter.client.Jar != jar {
		t.Fatal("original client was modified")
	}
}

func Test500pxChinaLiveDownload(t *testing.T) {
	if os.Getenv("TULAI_500PXCN_LIVE") != "1" {
		t.Skip("set TULAI_500PXCN_LIVE=1 to verify a public Chinese 500px photo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	root, _ := url.Parse("https://500px.com.cn/community/photo-details/" + fiveHundredChinaLegacyID)
	getter := NewHTTPGetter("")
	result, err := Discover(ctx, getter, root.String(), 1)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("discover items=%d err=%v", len(result.Items), err)
	}
	item := result.Items[0]
	item.Filename = "photo.jpg"
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
	img, format, err := image.DecodeConfig(f)
	if err != nil || format != "jpeg" || img.Width == 0 || img.Height == 0 {
		t.Fatalf("image=%+v format=%s err=%v", img, format, err)
	}
	t.Logf("public photo title=%s decoded=%dx%d format=%s", result.Title, img.Width, img.Height, format)
}

func Test500pxChinaGalleryURLs(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		legacy  bool
	}{
		{"https://500px.com.cn/community/set/7421f55871e647eca42e9416ebc5d6a5/details", "7421f55871e647eca42e9416ebc5d6a5", true},
		{"https://500px.me/community/set/7421f55871e647eca42e9416ebc5d6a5", "7421f55871e647eca42e9416ebc5d6a5", true},
		{"https://500px.com.cn/gallery/DrmHy1VGTF", "DrmHy1VGTF", false},
		{"https://500px.com.cn/community/set/7421f55871e647eca42e9416ebc5d6a5/photos", "", false},
		{"https://500px.com.cn/gallery/DrmHy1VGTF/other", "", false},
		{"https://user@500px.com.cn/gallery/DrmHy1VGTF", "", false},
		{"https://evil.test/gallery/DrmHy1VGTF", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		ref := fiveHundredChinaGalleryRef(u)
		if ref.id != tc.id || ref.legacy != tc.legacy || is500pxChinaGallery(u) != (tc.id != "") || is500pxChinaLegacyPage(u) != tc.legacy {
			t.Errorf("ref(%q)=%+v", tc.raw, ref)
		}
	}
}

func Test500pxChinaGalleryPaginationGroupsAndPartial(t *testing.T) {
	root, _ := url.Parse("https://500px.com.cn/community/set/7421f55871e647eca42e9416ebc5d6a5/details?token=private-sentinel")
	var operations []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Host != "api-neo.500px.com.cn" || strings.Contains(raw, "private-sentinel") {
			t.Fatalf("unsafe URL %s", raw)
		}
		q := u.Query()
		operation := q.Get("operationName")
		operations = append(operations, operation)
		var vars map[string]any
		if err := json.Unmarshal([]byte(q.Get("variables")), &vars); err != nil {
			t.Fatal(err)
		}
		switch operation {
		case "resolveLegacyGallery":
			if vars["legacyId"] != "7421f55871e647eca42e9416ebc5d6a5" || q.Get("query") != fiveHundredChinaResolveGallery {
				t.Fatal("invalid legacy query")
			}
			return chinaResponse(raw, `{"data":{"target":{"id":"DrmHy1VGTF"}}}`), nil
		case "GetGalleryById":
			if vars["id"] != "DrmHy1VGTF" || q.Get("query") != fiveHundredChinaGallery {
				t.Fatal("invalid metadata query")
			}
			return chinaResponse(raw, `{"data":{"getGalleryById":{"id":"DrmHy1VGTF","name":"Gallery"}}}`), nil
		case "PageGalleryItems":
			if vars["galleryId"] != "DrmHy1VGTF" || vars["first"] != float64(20) || q.Get("query") != fiveHundredChinaGalleryItems {
				t.Fatal("invalid gallery query")
			}
			if vars["after"] == nil {
				return chinaResponse(raw, `{"data":{"pageGalleryItems":{"edges":[{"node":{"__typename":"Photo","id":"Photo0001","urls":{"size_2048":"https://cdn-resize-prod-cn.500px.me/photo/1234/0.jpg?size=synthetic"}}},{"node":{"__typename":"PhotoGroup","id":"Group001"}},{"node":{"__typename":"Video"}}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor1"}}}}`), nil
			}
			if vars["after"] != "cursor1" {
				t.Fatal("wrong pagination cursor")
			}
			return chinaResponse(raw, `{"data":{"pageGalleryItems":{"edges":[{"node":{"__typename":"Photo","id":"Photo0002","urls":{"size_600":"https://cdn-resize-prod-cn.500px.me/photo/2345/0.jpg?size=synthetic"}}}],"pageInfo":{"hasNextPage":false,"endCursor":"cursor2"}}}}`), nil
		case "getPhotosByGroupId":
			if vars["groupId"] != "Group001" || q.Get("query") != fiveHundredChinaGroupPhotos {
				t.Fatal("invalid group query")
			}
			return chinaResponse(raw, `{"data":{"getPhotosByGroupId":[{"id":"Photo0003","urls":{"size_4k":"https://cdn-resize-prod-cn.500px.me/photo/3456/0.jpg?size=synthetic"}},{"id":"Photo0004","urls":{"size_4k":"https://evil.test/photo/4567/0.jpg"}}]}}`), nil
		default:
			t.Fatalf("unexpected operation %s", operation)
		}
		return nil, nil
	})
	got, err := discover500pxChina(context.Background(), getter, root)
	if !is500pxChinaPage(root) || err == nil || len(got.Items) != 3 || got.Title != "Gallery" || strings.Join(operations, ",") != "resolveLegacyGallery,GetGalleryById,PageGalleryItems,getPhotosByGroupId,PageGalleryItems" {
		t.Fatalf("result=%+v err=%v operations=%v", got, err, operations)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://500px.com.cn/" {
			t.Fatalf("wrong referer %q", item.Headers["Referer"])
		}
	}
}

func Test500pxChinaGalleryRetainsItemsOnLaterFailure(t *testing.T) {
	root, _ := url.Parse("https://500px.com.cn/gallery/DrmHy1VGTF")
	for _, failure := range []string{"next-page", "cursor-loop", "group", "missing-page-info"} {
		t.Run(failure, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				q := u.Query()
				var vars map[string]any
				_ = json.Unmarshal([]byte(q.Get("variables")), &vars)
				switch q.Get("operationName") {
				case "GetGalleryById":
					return chinaResponse(raw, `{"data":{"getGalleryById":{"id":"DrmHy1VGTF","name":"Gallery"}}}`), nil
				case "PageGalleryItems":
					if failure == "missing-page-info" {
						return chinaResponse(raw, `{"data":{"pageGalleryItems":{"edges":[{"node":{"__typename":"Photo","id":"Photo0001","urls":{"size_600":"`+fiveHundredChinaImage+`"}}}]}}}`), nil
					}
					if vars["after"] != nil && failure == "next-page" {
						return nil, errors.New("network failure")
					}
					return chinaResponse(raw, `{"data":{"pageGalleryItems":{"edges":[{"node":{"__typename":"Photo","id":"Photo0001","urls":{"size_600":"`+fiveHundredChinaImage+`"}}},{"node":{"__typename":"PhotoGroup","id":"Group001"}}],"pageInfo":{"hasNextPage":true,"endCursor":"same-cursor"}}}}`), nil
				case "getPhotosByGroupId":
					if failure == "group" {
						return nil, errors.New("group failed")
					}
					return chinaResponse(raw, `{"data":{"getPhotosByGroupId":[]}}`), nil
				}
				t.Fatal("unknown request")
				return nil, nil
			})
			got, err := discover500pxChina(context.Background(), getter, root)
			if err == nil || len(got.Items) != 1 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func Test500pxChinaLiveGallery(t *testing.T) {
	if os.Getenv("TULAI_500PXCN_LIVE") != "1" {
		t.Skip("set TULAI_500PXCN_LIVE=1 to verify a public Chinese 500px gallery")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse("https://500px.com.cn/community/set/7421f55871e647eca42e9416ebc5d6a5/details")
	result, err := Discover(ctx, NewHTTPGetter(""), root.String(), 1)
	if err != nil || len(result.Items) < 41 {
		t.Fatalf("discover items=%d err=%v", len(result.Items), err)
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	t.Logf("public gallery title=%s collected=%d images", result.Title, len(result.Items))
}
