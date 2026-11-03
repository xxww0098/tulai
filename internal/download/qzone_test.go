package download

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestQzoneURLCredentials(t *testing.T) {
	tests := []struct{ input, want string }{
		{"https://h5.qzone.qq.com/groupphoto/inqq/photo/12/a/groupphoto?_wv=3&g_tk=123&qzonetoken=secret&x=1", "https://h5.qzone.qq.com/groupphoto/inqq/photo/12/a/groupphoto?_wv=3&x=1"},
		{"https://user.qzone.qq.com/12#/photo?G%5FTK=123&tab=album&QZONETOKEN=secret", "https://user.qzone.qq.com/12#/photo?tab=album"},
		{"https://user.qzone.qq.com/12#g_tk%3Dsecret", "https://user.qzone.qq.com/12"},
		{"https://user.qzone.qq.com/12#safe%3D1%26g_tk%3Dsecret", "https://user.qzone.qq.com/12#safe%3D1"},
		{"https://user.qzone.qq.com/12#g_tk%253Dsecret", "https://user.qzone.qq.com/12"},
		{"https://user.qzone.qq.com/12?g%252525255Ftk=secret", "https://user.qzone.qq.com/12"},
		{"https://user.qzone.qq.com/12#access%255Ftoken%253Dsecret", "https://user.qzone.qq.com/12"},
		{"https://user.qzone.qq.com/12?g_tk%3Dsecret&safe=1", "https://user.qzone.qq.com/12?safe=1"},
		{"https://i.qq.com/?redirect=https%3A%2F%2Fh5.qzone.qq.com%2F%3Fg_tk%3D123&safe=1", "https://i.qq.com/?safe=1"},
		{"https://i.qq.com/?redirect=https%3A%2F%2Fh5.qzone.qq.com%2F%23qzonetoken%3Dsecret&safe=1", "https://i.qq.com/?safe=1"},
		{"https://name:secret@h5.qzone.qq.com/?safe=1#skey=secret", "https://h5.qzone.qq.com/?safe=1"},
		{"https://h5.qzone.qq.com/?a=1&&b=2#normal", "https://h5.qzone.qq.com/?a=1&&b=2#normal"},
		{"https://m.qpic.cn/psc?g_tk=signed", "https://m.qpic.cn/psc?g_tk=signed"},
		{"https://r.photo.store.qq.com/a?qzonetoken=signed", "https://r.photo.store.qq.com/a?qzonetoken=signed"},
		{"https://other.example/?g_tk=value", "https://other.example/?g_tk=value"},
	}
	for _, tt := range tests {
		got := RedactQzoneCredentials(tt.input)
		if got != tt.want || QzoneURLHasCredentials(tt.input) != (tt.input != tt.want) {
			t.Errorf("redact %q = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestQzoneGroupAlbumIDs(t *testing.T) {
	for _, raw := range []string{
		"https://h5.qzone.qq.com/groupphoto/inqq/photo/123/album_1",
		"https://h5.qzone.qq.com/groupphoto/inqq/photo/123/album_1/groupphoto?_wv=3",
	} {
		u, _ := url.Parse(raw)
		group, album, ok := qzoneGroupAlbumIDs(u)
		if !ok || group != "123" || album != "album_1" {
			t.Errorf("group album ID for %q = %q %q %v", raw, group, album, ok)
		}
	}
	for _, raw := range []string{
		"https://evil.example/groupphoto/inqq/photo/123/album_1",
		"https://h5.qzone.qq.com:444/groupphoto/inqq/photo/123/album_1",
		"https://user@h5.qzone.qq.com/groupphoto/inqq/photo/123/album_1",
		"http://h5.qzone.qq.com/groupphoto/inqq/photo/123/album_1",
		"https://h5.qzone.qq.com/groupphoto/inqq/photo/123/album%5f1",
		"https://h5.qzone.qq.com/groupphoto/inqq/photo/123/album_1/other",
		"https://h5.qzone.qq.com/groupphoto/inqq/photo/xx/album_1",
	} {
		u, _ := url.Parse(raw)
		if _, _, ok := qzoneGroupAlbumIDs(u); ok {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestQzoneGroupListID(t *testing.T) {
	for _, raw := range []string{"https://h5.qzone.qq.com/groupphoto/inqq/album/123", "https://h5.qzone.qq.com/groupphoto/inqq/album/123/groupphoto"} {
		page, _ := url.Parse(raw)
		if got, ok := qzoneGroupAlbumListID(page); !ok || got != "123" {
			t.Errorf("group list %q = %q %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://evil.example/groupphoto/inqq/album/123", "https://h5.qzone.qq.com:444/groupphoto/inqq/album/123", "https://h5.qzone.qq.com/groupphoto/inqq/album/123/other", "https://h5.qzone.qq.com/groupphoto/inqq/album/abc"} {
		page, _ := url.Parse(raw)
		if _, ok := qzoneGroupAlbumListID(page); ok {
			t.Errorf("unexpected group list %q", raw)
		}
	}
}

func TestQzoneImageBoundaries(t *testing.T) {
	tests := []struct{ input, output string }{
		{"http://m.qpic.cn/psc?signed=1", "https://m.qpic.cn/psc?signed=1"},
		{"//r.photo.store.qq.com/psc/one", "https://r.photo.store.qq.com/psc/one"},
		{"https://photo.store.qq.com/psc/one?sig=abc", "https://photo.store.qq.com/psc/one?sig=abc"},
		{"https://m.qpic.cn/psc/one?token=image-signature", "https://m.qpic.cn/psc/one?token=image-signature"},
		{"https://user@m.qpic.cn/a", ""},
		{"https://m.qpic.cn:444/a", ""},
		{"https://m.qpic.cn.evil.test/a", ""},
		{"https://127.0.0.1/a", ""},
		{"https://m.qpic.cn/a?g_tk=secret", ""},
		{"https://m.qpic.cn/a#qzonetoken=secret", ""},
	}
	for _, tt := range tests {
		got, ok := qzoneImage(tt.input)
		if got != tt.output || ok != (tt.output != "") {
			t.Errorf("image %q = %q, %v; want %q", tt.input, got, ok, tt.output)
		}
	}
}

type qzonePosterFunc func(context.Context, string, []byte, http.Header) (*Response, error)

func (f qzonePosterFunc) Post(ctx context.Context, target string, body []byte, headers http.Header) (*Response, error) {
	return f(ctx, target, body, headers)
}

func qzoneTestResponse(status int, body string) *Response {
	return &Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestQzoneGroupAlbumPagination(t *testing.T) {
	identity := qzoneIdentity{GTk: "112233", Token: "synthetic-token", Uin: "10086"}
	calls := 0
	poster := qzonePosterFunc(func(ctx context.Context, raw string, data []byte, headers http.Header) (*Response, error) {
		calls++
		u, err := url.Parse(raw)
		if err != nil || u.Host != "h5.qzone.qq.com" || u.Path != "/groupphoto/inqq" || u.Query().Get("g_tk") != identity.GTk || u.Query().Get("qzonetoken") != identity.Token {
			t.Errorf("unexpected POST target: %s", raw)
		}
		v, err := url.ParseQuery(string(data))
		if err != nil || v.Get("qunid") != "123" || v.Get("albumid") != "album_1" || v.Get("uin") != "10086" || v.Get("cmd") != "qunGetPhotoList" || headers.Get("Content-Type") != "application/x-www-form-urlencoded; charset=utf-8" {
			t.Errorf("unexpected POST form: %v %v", v, err)
		}
		if calls == 1 {
			if v.Get("refresh_type") != "1" || v.Get("attach_info") != "" {
				t.Errorf("initial page = %v", v)
			}
			return qzoneTestResponse(200, `{"ret":0,"data":{"albuminfo":{"albumid":"album_1","name":"群相册"},"photolist":[{"photourl":{"0":{"url":"http://m.qpic.cn/psc/one?sig=signed"},"1":{"url":"https://m.qpic.cn/psc/small"}}},{"videoflag":1,"photourl":{}}],"hasmore":1,"attach_info":{"start_count":36}}}`), nil
		}
		if v.Get("refresh_type") != "2" || v.Get("attach_info.start_count") != "36" {
			t.Errorf("next page = %v", v)
		}
		return qzoneTestResponse(200, `{"ret":0,"data":{"photolist":[{"photourl":[{"url":"https://r.photo.store.qq.com/psc/two"}]},{"photourl":[{"url":"https://m.qpic.cn/psc/one?sig=signed"}]}],"hasmore":0}}`), nil
	})
	got, err := collectQzoneGroupAlbum(context.Background(), poster, "123", "album_1", identity, func() error { return nil })
	if err != nil || calls != 2 || got.Title != "群相册" || len(got.Items) != 2 {
		t.Fatalf("pages = %+v; calls=%d err=%v", got, calls, err)
	}
	if got.Items[0].URL != "https://m.qpic.cn/psc/one?sig=signed" || got.Items[1].URL != "https://r.photo.store.qq.com/psc/two" {
		t.Fatalf("images = %+v", got.Items)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Headers["Referer"] != "https://h5.qzone.qq.com/" || strings.Contains(fmt.Sprint(item), identity.Token) || strings.Contains(item.URL, identity.GTk) {
			t.Fatalf("unsafe image = %+v", item)
		}
	}
}

func TestQzoneGroupAlbumPartialAndErrors(t *testing.T) {
	identity := qzoneIdentity{GTk: "112233", Token: "synthetic-token", Uin: "10086"}
	first := `{"ret":0,"data":{"albuminfo":{"albumid":"a","name":"示例"},"photolist":[{"photourl":[{"url":"https://m.qpic.cn/one"}]}],"hasmore":1,"attach_info":"next"}}`
	for _, tc := range []struct {
		name   string
		second func(context.Context, string) (*Response, error)
		check  func(error) bool
	}{
		{"network", func(_ context.Context, target string) (*Response, error) { return nil, fmt.Errorf("failed %s", target) }, func(err error) bool { return strings.Contains(err.Error(), "请求失败") }},
		{"status", func(_ context.Context, _ string) (*Response, error) { return qzoneTestResponse(403, ""), nil }, func(err error) bool {
			var status *HTTPStatusError
			return errors.As(err, &status) && status.URL == qzoneEndpoint && status.StatusCode == 403
		}},
		{"login", func(_ context.Context, _ string) (*Response, error) {
			return qzoneTestResponse(200, `{"ret":-3000}`), nil
		}, func(err error) bool { return strings.Contains(err.Error(), "登录") }},
		{"repeat", func(_ context.Context, _ string) (*Response, error) { return qzoneTestResponse(200, first), nil }, func(err error) bool { return strings.Contains(err.Error(), "游标重复") }},
		{"album mismatch", func(_ context.Context, _ string) (*Response, error) {
			return qzoneTestResponse(200, `{"ret":0,"data":{"albuminfo":{"albumid":"other"},"photolist":[],"hasmore":0}}`), nil
		}, func(err error) bool { return strings.Contains(err.Error(), "不匹配") }},
		{"missing pagination", func(_ context.Context, _ string) (*Response, error) {
			return qzoneTestResponse(200, `{"ret":0,"data":{"photolist":[]}}`), nil
		}, func(err error) bool { return strings.Contains(err.Error(), "分页状态未知") }},
		{"unsupported image", func(_ context.Context, _ string) (*Response, error) {
			return qzoneTestResponse(200, `{"ret":0,"data":{"photolist":[{"photourl":[{"url":"https://unknown.example/a"},{"url":"https://m.qpic.cn/small"}]}],"hasmore":0}}`), nil
		}, func(err error) bool { return strings.Contains(err.Error(), "不完整") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			poster := qzonePosterFunc(func(ctx context.Context, target string, _ []byte, _ http.Header) (*Response, error) {
				calls++
				if calls == 1 {
					return qzoneTestResponse(200, first), nil
				}
				return tc.second(ctx, target)
			})
			got, err := collectQzoneGroupAlbum(context.Background(), poster, "123", "a", identity, func() error { return nil })
			if err == nil || !tc.check(err) || len(got.Items) == 0 || strings.Contains(err.Error(), identity.Token) || strings.Contains(err.Error(), identity.GTk) {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	poster := qzonePosterFunc(func(_ context.Context, _ string, _ []byte, _ http.Header) (*Response, error) {
		calls++
		if calls == 1 {
			return qzoneTestResponse(200, first), nil
		}
		cancel()
		return nil, errors.New("ignored")
	})
	got, err := collectQzoneGroupAlbum(ctx, poster, "123", "a", identity, func() error { return nil })
	if !errors.Is(err, context.Canceled) || len(got.Items) != 1 {
		t.Fatalf("cancel result=%+v err=%v", got, err)
	}
}

func TestQzoneGroupListPagination(t *testing.T) {
	identity := qzoneIdentity{GTk: "112233", Token: "synthetic-token", Uin: "10086"}
	var listCalls, photoCalls int
	poster := qzonePosterFunc(func(ctx context.Context, raw string, data []byte, headers http.Header) (*Response, error) {
		u, err := url.Parse(raw)
		form, formErr := url.ParseQuery(string(data))
		if err != nil || formErr != nil || u.Host != "h5.qzone.qq.com" || u.Path != "/groupphoto/inqq" || u.Query().Get("g_tk") != identity.GTk || u.Query().Get("qzonetoken") != identity.Token || form.Get("qunid") != "123" || form.Get("uin") != identity.Uin {
			t.Errorf("unexpected group request")
		}
		switch form.Get("cmd") {
		case "qunGetAlbumList,qunGetMainPage":
			listCalls++
			if listCalls == 1 {
				return qzoneTestResponse(200, `{"ret":0,"data":{"qunGetAlbumList":{"all_album_list_data":[{"singlefeed":{"5":{"albumid":"a1","albumname":"First","albumnum":1,"qunid":"123"}}}],"hasmore":1,"attach_info":{"start_count":1}},"qunGetMainPage":{"album_count":2}}}`), nil
			}
			if form.Get("refresh_type") != "2" || form.Get("attach_info.start_count") != "1" {
				t.Errorf("group list pagination = %v", form)
			}
			return qzoneTestResponse(200, `{"ret":0,"data":{"qunGetAlbumList":{"all_album_list_data":[{"singlefeed":[null,null,null,null,null,{"albumid":"a2","albumname":"Second","albumnum":1}]},{"singlefeed":{"5":{"albumid":"a1","albumnum":1}}}],"hasmore":0},"qunGetMainPage":{}}}`), nil
		case "qunGetPhotoList":
			photoCalls++
			id := form.Get("albumid")
			if id != "a1" && id != "a2" {
				t.Errorf("unexpected album %q", id)
			}
			return qzoneTestResponse(200, fmt.Sprintf(`{"ret":0,"data":{"albuminfo":{"albumid":%q},"photolist":[{"photourl":[{"url":"https://m.qpic.cn/%s.png"}]}],"hasmore":0}}`, id, id)), nil
		default:
			return nil, errors.New("unexpected command")
		}
	})
	got, err := collectQzoneGroupList(context.Background(), poster, "123", identity, func() error { return nil })
	if err != nil || listCalls != 2 || photoCalls != 2 || len(got.Items) != 2 || got.Items[0].Metadata["album_id"] != "a1" || got.Items[1].Metadata["album_title"] != "Second" {
		t.Fatalf("group list result=%+v list=%d photo=%d err=%v", got, listCalls, photoCalls, err)
	}
}

func TestQzoneGroupListPartialAndCredentials(t *testing.T) {
	identity := qzoneIdentity{GTk: "112233", Token: "synthetic-token", Uin: "10086"}
	for _, tc := range []struct {
		name   string
		second func(context.Context, context.CancelFunc, string) (*Response, error)
		check  func(error) bool
	}{
		{"network", func(_ context.Context, _ context.CancelFunc, target string) (*Response, error) {
			return nil, fmt.Errorf("failed %s", target)
		}, func(err error) bool { return strings.Contains(err.Error(), "请求失败") }},
		{"canceled", func(ctx context.Context, cancel context.CancelFunc, _ string) (*Response, error) {
			cancel()
			return nil, ctx.Err()
		}, func(err error) bool { return errors.Is(err, context.Canceled) }},
		{"missing data", func(_ context.Context, _ context.CancelFunc, _ string) (*Response, error) {
			return qzoneTestResponse(200, `{"ret":0,"data":{"qunGetAlbumList":{}}}`), nil
		}, func(err error) bool { return strings.Contains(err.Error(), "不完整") }},
		{"login", func(_ context.Context, _ context.CancelFunc, _ string) (*Response, error) {
			return qzoneTestResponse(200, `{"ret":-3000}`), nil
		}, func(err error) bool { return errors.Is(err, errQzoneGroupLogin) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lists := 0
			poster := qzonePosterFunc(func(ctx context.Context, raw string, data []byte, _ http.Header) (*Response, error) {
				form, _ := url.ParseQuery(string(data))
				if form.Get("cmd") == "qunGetPhotoList" {
					return qzoneTestResponse(200, `{"ret":0,"data":{"albuminfo":{"albumid":"a1"},"photolist":[{"photourl":[{"url":"https://m.qpic.cn/one.png"}]}],"hasmore":0}}`), nil
				}
				lists++
				if lists == 1 {
					return qzoneTestResponse(200, `{"ret":0,"data":{"qunGetAlbumList":{"all_album_list_data":[{"singlefeed":{"5":{"albumid":"a1","albumnum":1}}}],"hasmore":1,"attach_info":"next"}}}`), nil
				}
				return tc.second(ctx, cancel, raw)
			})
			got, err := collectQzoneGroupList(ctx, poster, "123", identity, func() error { return nil })
			if err == nil || !tc.check(err) || len(got.Items) != 1 || strings.Contains(err.Error(), identity.Token) || strings.Contains(err.Error(), identity.GTk) {
				t.Fatalf("partial group list result=%+v err=%v", got, err)
			}
		})
	}
}

func TestQzoneGroupListRequestLimit(t *testing.T) {
	identity := qzoneIdentity{GTk: "112233", Uin: "10086"}
	var calls, pages int
	poster := qzonePosterFunc(func(ctx context.Context, target string, body []byte, headers http.Header) (*Response, error) {
		calls++
		form, _ := url.ParseQuery(string(body))
		if form.Get("cmd") == "qunGetPhotoList" {
			return qzoneTestResponse(200, fmt.Sprintf(`{"ret":0,"data":{"albuminfo":{"albumid":%q},"photolist":[],"hasmore":0}}`, form.Get("albumid"))), nil
		}
		pages++
		entries := make([]string, 10)
		for i := range entries {
			entries[i] = fmt.Sprintf(`{"singlefeed":{"5":{"albumid":"a%d_%d","albumnum":1}}}`, pages, i)
		}
		return qzoneTestResponse(200, fmt.Sprintf(`{"ret":0,"data":{"qunGetAlbumList":{"all_album_list_data":[%s],"hasmore":1,"attach_info":"cursor_%d"}}}`, strings.Join(entries, ","), pages)), nil
	})
	_, err := collectQzoneGroupList(context.Background(), poster, "123", identity, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "500 次请求上限") || calls != 500 {
		t.Fatalf("request limit: calls=%d err=%v", calls, err)
	}
}

func TestQzoneRenderedGroupAlbum(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	const secret = "synthetic-cookie-value"
	const token = "synthetic-qzonetoken"
	const pagePath = "/groupphoto/inqq/photo/123/album_1/groupphoto"
	const listPath = "/groupphoto/inqq/album/123/groupphoto"
	var hash uint32 = 5381
	for _, c := range secret {
		hash += (hash << 5) + uint32(c)
	}
	wantGTK := strconv.FormatUint(uint64(hash&0x7fffffff), 10)
	png := filterPNG(t, 1, 1)
	var posts, downloads, badRequests atomic.Int32
	var redirect atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "m.qpic.cn" && r.URL.Path == "/one.png" {
			downloads.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "https://h5.qzone.qq.com/" || r.URL.Query().Get("sig") != "synthetic-image-signature" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host != "h5.qzone.qq.com" {
			badRequests.Add(1)
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == pagePath || r.URL.Path == listPath) {
			http.SetCookie(w, &http.Cookie{Name: "p_skey", Value: secret, Path: "/", Secure: true})
			http.SetCookie(w, &http.Cookie{Name: "uin", Value: "o0000010086", Path: "/", Secure: true})
			albumID := "album_1"
			if r.URL.Query().Has("mismatch") {
				albumID = "other"
			}
			code := 0
			if r.URL.Query().Has("login") {
				code = -3000
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprintf(w, `<html><head><title>群相册</title></head><body><script>window.params={qunid:"123",albumid:%q,loginUin:"10086"};window.syncData={code:%d,loginUin:"10086"};window.shine0callback=%q;</script></body></html>`, albumID, code, token)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/groupphoto/inqq" {
			http.NotFound(w, r)
			return
		}
		posts.Add(1)
		cookie, cookieErr := r.Cookie("p_skey")
		ref := r.Header.Get("Referer")
		if cookieErr != nil || cookie.Value != secret || r.URL.Query().Get("g_tk") != wantGTK || r.URL.Query().Get("qzonetoken") != token || r.Header.Get("Origin") != "https://h5.qzone.qq.com" || (!strings.HasPrefix(ref, "https://h5.qzone.qq.com"+pagePath) && !strings.HasPrefix(ref, "https://h5.qzone.qq.com"+listPath)) {
			badRequests.Add(1)
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("qunid") != "123" || r.PostForm.Get("uin") != "10086" {
			badRequests.Add(1)
		}
		if redirect.Load() {
			http.Redirect(w, r, "https://outside.invalid/steal", http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("cmd") == "qunGetAlbumList,qunGetMainPage" {
			_, _ = io.WriteString(w, `{"ret":0,"data":{"qunGetAlbumList":{"all_album_list_data":[{"singlefeed":{"5":{"albumid":"album_1","albumname":"群相册","albumnum":2,"qunid":"123"}}}],"hasmore":0},"qunGetMainPage":{"album_count":1}}}`)
			return
		}
		if r.PostForm.Get("albumid") != "album_1" {
			badRequests.Add(1)
		}
		if r.PostForm.Get("refresh_type") == "1" {
			_, _ = io.WriteString(w, `{"ret":0,"data":{"albuminfo":{"albumid":"album_1","name":"群相册"},"photolist":[{"photourl":[{"url":"https://m.qpic.cn/one.png?sig=synthetic-image-signature"}]}],"hasmore":1,"attach_info":"start_count=1"}}`)
			return
		}
		if r.PostForm.Get("refresh_type") != "2" || r.PostForm.Get("attach_info") != "start_count=1" {
			badRequests.Add(1)
		}
		_, _ = io.WriteString(w, `{"ret":0,"data":{"photolist":[{"photourl":[{"url":"https://m.qpic.cn/two.png"}]}],"hasmore":0}}`)
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://h5.qzone.qq.com" + pagePath)
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	// Only this synthetic test maps the two documented hosts to the local TLS server.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "h5.qzone.qq.com:443" && addr != "m.qpic.cn:443" {
			badRequests.Add(1)
			return nil, errors.New("blocked test destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	getter.client.Transport = transport
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String())); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || posts.Load() != 2 || got.Title != "群相册" {
		t.Fatalf("capture=%+v posts=%d err=%v", got, posts.Load(), err)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if downloads.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("download requests=%d unexpected=%d", downloads.Load(), badRequests.Load())
	}
	redirect.Store(true)
	_, err = session.Capture(ctx)
	if err == nil || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), wantGTK) || badRequests.Load() != 0 {
		t.Fatalf("redirect err=%v unexpected=%d", err, badRequests.Load())
	}
	before := posts.Load()
	for _, suffix := range []string{"?mismatch=1", "?login=1"} {
		if err := session.run(ctx, camoufox.Navigate(page.String()+suffix)); err != nil {
			t.Fatal(err)
		}
		result, err := session.Capture(ctx)
		if err == nil || len(result.Items) != 0 || posts.Load() != before {
			t.Fatalf("%s result=%+v err=%v posts=%d", suffix, result, err, posts.Load()-before)
		}
	}
	redirect.Store(false)
	listPage, _ := url.Parse("https://h5.qzone.qq.com" + listPath)
	session.start = listPage
	if err := session.run(ctx, camoufox.Navigate(listPage.String())); err != nil {
		t.Fatal(err)
	}
	before = posts.Load()
	result, err := session.Capture(ctx)
	if err != nil || len(result.Items) != 2 || result.Items[0].Metadata["album_id"] != "album_1" || posts.Load()-before != 3 || badRequests.Load() != 0 {
		t.Fatalf("group list capture=%+v err=%v posts=%d unexpected=%d", result, err, posts.Load()-before, badRequests.Load())
	}
}

func TestQzonePersonalOwnerAndJSONP(t *testing.T) {
	for _, raw := range []string{"https://user.qzone.qq.com/123", "https://user.qzone.qq.com/123/photo/"} {
		page, _ := url.Parse(raw)
		if owner, ok := qzonePersonalOwner(page); !ok || owner != "123" {
			t.Errorf("owner %q = %q %v", raw, owner, ok)
		}
	}
	for _, raw := range []string{"https://other.qzone.qq.com/123", "https://user.qzone.qq.com:444/123", "https://user@user.qzone.qq.com/123", "https://user.qzone.qq.com/123/photo/456", "https://user.qzone.qq.com/%31%32%33"} {
		page, _ := url.Parse(raw)
		if _, ok := qzonePersonalOwner(page); ok {
			t.Errorf("unexpected owner for %q", raw)
		}
	}
	detail, _ := url.Parse("https://user.qzone.qq.com/123/photo/456")
	if !qzonePersonalAlbumDetail(detail) {
		t.Fatal("personal album detail was not recognized")
	}
	for _, body := range []string{"viewer_Callback({\"code\":0});evil()", "other_Callback({\"code\":0});", "viewer_Callback({});\n<script>"} {
		if _, err := qzonePersonalJSON([]byte(body), "viewer_Callback"); err == nil {
			t.Errorf("accepted invalid JSONP %q", body)
		}
	}
}

func TestQzonePersonalResponseIdentity(t *testing.T) {
	for _, tc := range []struct {
		data string
		want bool
	}{
		{`{"hostUin":"123","albumid":"a1"}`, true},
		{`{"hostuin":123,"topicId":"a1"}`, true},
		{`{"photos":[]}`, true},
		{`{"hostUin":"999","albumid":"a1"}`, false},
		{`{"hostUin":"123","albumid":"a2"}`, false},
		{`{"hostUin":"123","topicId":"a2"}`, false},
		{`[]`, false},
	} {
		if got := qzonePersonalResponseMatches(json.RawMessage(tc.data), "123", "a1"); got != tc.want {
			t.Errorf("response binding %s = %v, want %v", tc.data, got, tc.want)
		}
	}
}

func TestQzonePersonalAlbumPagination(t *testing.T) {
	identity := qzonePersonalIdentity{uin: "9988", gtk: "123456"}
	var listCalls, photoCalls, floatCalls int
	get := func(ctx context.Context, path string, v url.Values) ([]byte, error) {
		switch path {
		case "/proxy/domain/route.store.qq.com/GetRoute":
			if v.Get("UIN") != "123" {
				t.Errorf("route target = %v", v)
			}
			return []byte(`photoDomainNameCallback({"default":"domain_4","domain_4":{"p":"photo.qzone.qq.com","s":"photo.qzone.qq.com"}})`), nil
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_album_v3":
			listCalls++
			if v.Get("hostUin") != "123" || v.Get("uin") != identity.uin || v.Get("g_tk") != identity.gtk {
				t.Errorf("list target = %v", v)
			}
			if listCalls == 1 {
				return []byte(`shine0_Callback({"code":0,"data":{"albumsInUser":2,"nextPageStart":1,"nextPageStartModeSort":1,"mode":2,"ordertype":2,"albumListModeSort":[{"id":"a1","name":"First","allowAccess":1,"total":2}]}});`), nil
			}
			if v.Get("pageStart") != "1" || v.Get("pageNum") != "40" || v.Get("mode") != "2" || v.Get("sortOrder") != "2" {
				t.Errorf("list pagination = %v", v)
			}
			return []byte(`shine0_Callback({"code":0,"data":{"albumsInUser":2,"nextPageStart":0,"nextPageStartModeSort":0,"albumListModeClass":[{"albumList":[{"id":"a2","name":"Second","allowAccess":true,"total":1}]}]}});`), nil
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_photo_v2":
			photoCalls++
			if v.Get("hostUin") != "123" || (v.Get("albumid") != "a1" && v.Get("albumid") != "a2") {
				t.Errorf("album binding = %v", v)
			}
			return []byte(`_Callback({"code":0,"data":{"photoList":[{"lloc":"start_1"}]}});`), nil
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/cgi_floatview_photo_list_v2":
			floatCalls++
			if v.Get("hostUin") != "123" || v.Get("uin") != identity.uin || v.Get("g_tk") != identity.gtk {
				t.Errorf("photo identity = %v", v)
			}
			if v.Get("topicId") == "a1" && v.Get("picKey") == "start_1" {
				return []byte(`viewer_Callback({"code":0,"data":{"last":0,"photos":[{"raw":"http://m.qpic.cn/one.png?sig=one","origin":"https://m.qpic.cn/small","picKey":"next_1","shootTime":"123","is_video":false}]}});`), nil
			}
			if v.Get("topicId") == "a1" && v.Get("picKey") == "next_1" {
				if v.Get("offset") != "1" || v.Get("shootTime") != "123" || v.Get("prevNum") != "0" || v.Get("postNum") != "20" {
					t.Errorf("photo pagination = %v", v)
				}
				return []byte(`viewer_Callback({"code":0,"data":{"last":1,"photos":[{"origin_url":"https://m.qpic.cn/two.png","is_video":0}]}});`), nil
			}
			if v.Get("topicId") == "a2" {
				return []byte(`viewer_Callback({"code":0,"data":{"last":1,"photos":[{"url":"https://m.qpic.cn/one.png?sig=one","is_video":0}]}});`), nil
			}
			t.Errorf("unexpected photo request = %v", v)
			return nil, errors.New("unexpected")
		default:
			return nil, errors.New("unexpected endpoint")
		}
	}
	got, err := collectQzonePersonal(context.Background(), "123", identity, get)
	if err != nil || listCalls != 2 || photoCalls != 2 || floatCalls != 3 || len(got.Items) != 3 {
		t.Fatalf("result=%+v list=%d first=%d float=%d err=%v", got, listCalls, photoCalls, floatCalls, err)
	}
	if got.Items[0].URL != "https://m.qpic.cn/one.png?sig=one" || got.Items[1].URL != "https://m.qpic.cn/two.png" || got.Items[2].Metadata["album_id"] != "a2" || got.Items[2].Metadata["album_title"] != "Second" {
		t.Fatalf("images=%+v", got.Items)
	}
}

func TestQzonePersonalPartialPaginationAndCancellation(t *testing.T) {
	identity := qzonePersonalIdentity{uin: "9988", gtk: "123456"}
	ctx, cancel := context.WithCancel(context.Background())
	var floatCalls int
	get := func(ctx context.Context, path string, v url.Values) ([]byte, error) {
		switch path {
		case "/proxy/domain/route.store.qq.com/GetRoute":
			return []byte(`photoDomainNameCallback({"default":"domain_4","domain_4":{"p":"photo.qzone.qq.com","s":"photo.qzone.qq.com"}})`), nil
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_album_v3":
			return []byte(`_Callback({"code":0,"data":{"albumsInUser":1,"nextPageStart":0,"albumListModeSort":[{"id":"a1","name":"First","allowAccess":1,"total":2}]}});`), nil
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_photo_v2":
			return []byte(`_Callback({"code":0,"data":{"photoList":[{"lloc":"start"}]}});`), nil
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/cgi_floatview_photo_list_v2":
			floatCalls++
			if floatCalls > 1 {
				cancel()
				return nil, ctx.Err()
			}
			return []byte(`viewer_Callback({"code":0,"data":{"last":0,"photos":[{"raw":"https://m.qpic.cn/one","picKey":"next","shootTime":"1"}]}});`), nil
		}
		return nil, errors.New("unexpected endpoint")
	}
	got, err := collectQzonePersonal(ctx, "123", identity, get)
	if !errors.Is(err, context.Canceled) || len(got.Items) != 1 {
		t.Fatalf("cancel result=%+v err=%v", got, err)
	}
}

func TestQzoneRenderedPersonalAlbum(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	const secret = "synthetic-personal-skey"
	const pagePath = "/123/photo/"
	var hash uint32 = 5381
	for _, b := range []byte(secret) {
		hash += (hash << 5) + uint32(b)
	}
	gtk := strconv.FormatUint(uint64(hash&0x7fffffff), 10)
	png := filterPNG(t, 1, 1)
	var mode, requests, imageRequests, badRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "m.qpic.cn" {
			imageRequests.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "https://user.qzone.qq.com/" || r.URL.Query().Get("sig") != "image-signature" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host != "user.qzone.qq.com" {
			badRequests.Add(1)
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == pagePath {
			http.SetCookie(w, &http.Cookie{Name: "skey", Value: secret, Path: "/", Secure: true})
			http.SetCookie(w, &http.Cookie{Name: "uin", Value: "o0000009988", Path: "/", Secure: true})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<html><head><title>合成空间</title></head><body></body></html>`)
			return
		}
		requests.Add(1)
		cookie, err := r.Cookie("skey")
		if err != nil || cookie.Value != secret || r.Method != http.MethodGet || r.Header.Get("Referer") != "https://user.qzone.qq.com"+pagePath {
			badRequests.Add(1)
		}
		if mode.Load() == 1 {
			http.Redirect(w, r, "https://outside.invalid/leak?g_tk="+gtk, http.StatusTemporaryRedirect)
			return
		}
		if mode.Load() == 2 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/proxy/domain/route.store.qq.com/GetRoute":
			if r.URL.Query().Get("UIN") != "123" {
				badRequests.Add(1)
			}
			_, _ = io.WriteString(w, `photoDomainNameCallback({"default":"domain_4","domain_4":{"p":"photo.qzone.qq.com","s":"photo.qzone.qq.com"}})`)
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_album_v3":
			if r.URL.Query().Get("uin") != "9988" || r.URL.Query().Get("hostUin") != "123" || r.URL.Query().Get("g_tk") != gtk {
				badRequests.Add(1)
			}
			_, _ = io.WriteString(w, `shine0_Callback({"code":0,"data":{"albumsInUser":1,"nextPageStart":0,"albumListModeSort":[{"id":"album_1","name":"合成相册","allowAccess":1,"total":1}]}});`)
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/fcg_list_photo_v2":
			if r.URL.Query().Get("hostUin") != "123" || r.URL.Query().Get("albumid") != "album_1" {
				badRequests.Add(1)
			}
			_, _ = io.WriteString(w, `_Callback({"code":0,"data":{"photoList":[{"lloc":"pic_1"}]}});`)
		case "/proxy/domain/photo.qzone.qq.com/fcgi-bin/cgi_floatview_photo_list_v2":
			if r.URL.Query().Get("hostUin") != "123" || r.URL.Query().Get("uin") != "9988" || r.URL.Query().Get("topicId") != "album_1" || r.URL.Query().Get("picKey") != "pic_1" || r.URL.Query().Get("g_tk") != gtk {
				badRequests.Add(1)
			}
			_, _ = io.WriteString(w, `viewer_Callback({"code":0,"data":{"last":1,"photos":[{"raw":"https://m.qpic.cn/one.png?sig=image-signature"}]}});`)
		default:
			badRequests.Add(1)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://user.qzone.qq.com" + pagePath)
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	// Only the isolated test maps these documented hosts to the local TLS server.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "user.qzone.qq.com:443" && addr != "m.qpic.cn:443" {
			badRequests.Add(1)
			return nil, errors.New("blocked test destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	getter.client.Transport = transport
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String())); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	session.pageURL = page
	session.mu.Unlock()
	got, matched, err := session.captureQzonePersonal(ctx, page, page.String())
	if err != nil || !matched || len(got.Items) != 1 || requests.Load() != 4 || got.Items[0].Metadata["album_id"] != "album_1" {
		t.Fatalf("personal capture=%+v matched=%v requests=%d err=%v", got, matched, requests.Load(), err)
	}
	routed, err := session.Capture(ctx)
	if err != nil || len(routed.Items) != 1 || routed.Items[0].URL != got.Items[0].URL || requests.Load() != 8 {
		t.Fatalf("routed personal capture=%+v requests=%d err=%v", routed, requests.Load(), err)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("image download: %v", err)
	}
	if imageRequests.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("image requests=%d unexpected=%d", imageRequests.Load(), badRequests.Load())
	}
	mode.Store(1)
	_, _, err = session.captureQzonePersonal(ctx, page, page.String())
	if err == nil || !strings.Contains(err.Error(), "重定向") || strings.Contains(err.Error(), gtk) || strings.Contains(err.Error(), secret) || badRequests.Load() != 0 {
		t.Fatalf("redirect err=%v unexpected=%d", err, badRequests.Load())
	}
	mode.Store(2)
	_, _, err = session.captureQzonePersonal(ctx, page, page.String())
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusForbidden || strings.Contains(status.URL, "?") || strings.Contains(status.URL, gtk) {
		t.Fatalf("HTTP status err=%v status=%+v", err, status)
	}
	before := requests.Load()
	if err := session.run(ctx, chromedp.Evaluate(`document.cookie="uin=o0000007777; Path=/; Secure"`, nil)); err != nil {
		t.Fatal(err)
	}
	_, err = session.qzonePersonalGet(ctx, page, page.String(), "/proxy/domain/route.store.qq.com/GetRoute", url.Values{"UIN": {"123"}}, qzonePersonalIdentity{uin: "9988", gtk: gtk})
	if err == nil || requests.Load() != before || strings.Contains(err.Error(), gtk) || strings.Contains(err.Error(), secret) {
		t.Fatalf("identity change err=%v requests=%d", err, requests.Load()-before)
	}
	_, err = session.qzonePersonalGet(ctx, page, page.String(), "/proxy/domain/outside.invalid/steal", nil, qzonePersonalIdentity{uin: "7777", gtk: gtk})
	if err == nil || requests.Load() != before || badRequests.Load() != 0 {
		t.Fatalf("outside proxy err=%v requests=%d unexpected=%d", err, requests.Load()-before, badRequests.Load())
	}
}
