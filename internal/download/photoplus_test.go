package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestPhotoplusRoutesAndOrigins(t *testing.T) {
	for _, test := range []struct {
		raw, id string
	}{
		{"https://live.photoplus.cn/live/pc/123/#/live", "123"},
		{"http://live.photoplus.cn:80/live/pc/123", "123"},
		{"https://live.photoplus.cn/live/pc/1234", "1234"},
		{"https://live.photoplus.cn.evil.test/live/pc/123", ""},
		{"https://x@live.photoplus.cn/live/pc/123", ""},
		{"https://live.photoplus.cn:444/live/pc/123", ""},
		{"https://live.photoplus.cn/live/pc/123/other", ""},
		{"https://live.photoplus.cn/live/pc/%2f123", ""},
	} {
		parsed, err := url.Parse(test.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := photoplusActivityID(parsed); got != test.id {
			t.Errorf("route %q = %q; want %q", test.raw, got, test.id)
		}
	}
	for _, test := range []struct {
		raw  string
		good bool
	}{
		{"//pb.plusx.cn/plus/immedpic/123/a.jpg~watermark?sign=public", true},
		{"https://pb.plusx.cn:443/plus/immedpic/123/a.jpg", true},
		{"https://pb.plusx.cn/plus/immediate/123/a.jpg", true},
		{"http://pb.plusx.cn/plus/immedpic/123/a.jpg", false},
		{"https://pb.plusx.cn.evil.test/plus/immedpic/123/a.jpg", false},
		{"https://pb.plusx.cn@evil.test/plus/immedpic/123/a.jpg", false},
		{"https://pb.plusx.cn:444/plus/immedpic/123/a.jpg", false},
		{"https://pb.plusx.cn/plus/immedpic/124/a.jpg", false},
		{"https://pb.plusx.cn/plus/immedpic/123/%2e%2e/a.jpg", false},
		{"https://pb.plusx.cn/plus/immedpic/123/x%2fy.jpg", false},
	} {
		_, got := photoplusImage(test.raw, "123")
		if got != test.good {
			t.Errorf("image %q accepted=%v", test.raw, got)
		}
	}
	for _, test := range []struct {
		raw, kind string
	}{
		{"https://live.photoplus.cn/live/detail?activityNo=123&_s=synthetic", "detail"},
		{"https://live.photoplus.cn/pic/list?activityNo=123&page=2", "list"},
		{"https://live.photoplus.cn/pic/list?activityNo=124", ""},
		{"https://live.photoplus.cn/pic/list?activityNo=123&activityNo=124", ""},
		{"https://live.photoplus.cn:444/pic/list?activityNo=123", ""},
		{"https://live.photoplus.cn.evil.test/pic/list?activityNo=123", ""},
	} {
		if got := photoplusRequest(test.raw, "123"); got != test.kind {
			t.Errorf("request %q = %q; want %q", test.raw, got, test.kind)
		}
	}
}

func TestPhotoplusPermissionsAndList(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		good bool
	}{
		{"allowed", `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":0,"serial_code":{"type":0}}}`, true},
		{"different activity", `{"code":1,"success":true,"result":{"activity_no":124,"enable":0,"is_sell":0,"serial_code":{"type":0}}}`, false},
		{"hidden original", `{"code":1,"success":true,"result":{"activity_no":123,"enable":1048576,"is_sell":0,"serial_code":{"type":0}}}`, false},
		{"download denied", `{"code":1,"success":true,"result":{"activity_no":123,"enable":8388608,"is_sell":0,"serial_code":{"type":0}}}`, false},
		{"paid", `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":131072,"serial_code":{"type":0}}}`, false},
		{"all images hidden", `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":1048576,"serial_code":{"type":0}}}`, false},
		{"missing permissions", `{"code":1,"success":true,"result":{"activity_no":123}}`, false},
		{"password", `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":0,"serial_code":{"type":1}}}`, false},
		{"registration", `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":0,"serial_code":{"type":4}}}`, false},
		{"missing serial code", `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":0}}`, false},
		{"needs login", `{"code":0,"success":false}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := photoplusDetail([]byte(test.body), "123")
			if (err == nil) != test.good {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := photoplusListBody([]byte(`{"code":1,"success":true,"result":{"pics_array":[]}}`)); err == nil {
		t.Fatal("missing total was accepted")
	}
	if _, err := photoplusListBody([]byte(`{"code":1,"success":true,"result":{"pics_total":0,"pics_array":[]}}`)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page, _ := url.Parse("https://live.photoplus.cn/live/pc/123")
	if _, err := discoverPhotoplus(ctx, nil, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
}

func TestPhotoplusRenderedAnonymousPagination(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	var leak atomic.Int32
	var pages atomic.Int32
	var mode atomic.Int32
	var testStarted atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "live.photoplus.cn" {
			http.Error(w, "bad host", http.StatusBadRequest)
			return
		}
		if testStarted.Load() && r.Header.Get("Cookie") != "" {
			leak.Add(1)
		}
		switch r.URL.Path {
		case "/live/pc/123/":
			if mode.Load() == 3 {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<html><title>Public Event</title><style>.photo-content{height:100px;overflow:auto}.tall{height:1000px}</style><ul class="nav-bottom__ul"><li class="active">全部</li><li>分类</li></ul><div class="photo-live"><div class="photo-content"><div class="tall"></div></div></div><script>
fetch('/live/detail?activityNo=123'); fetch('/pic/list?activityNo=123&page=1');
let next=false;document.querySelector('.photo-content').onscroll=()=>{if(!next){next=true;fetch('/pic/list?activityNo=123&page=2')}};
</script></html>`)
		case "/live/detail":
			w.Header().Set("Content-Type", "application/json")
			if mode.Load() == 1 {
				fmt.Fprint(w, `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":0,"serial_code":{"type":1}}}`)
				return
			}
			fmt.Fprint(w, `{"code":1,"success":true,"result":{"activity_no":123,"enable":0,"is_sell":0,"name":"Public Event","serial_code":{"type":0}}}`)
		case "/pic/list":
			pages.Add(1)
			if mode.Load() == 4 && r.URL.Query().Get("page") == "2" {
				http.Error(w, "temporary failure", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			ids := []int{1, 2}
			if r.URL.Query().Get("page") == "2" {
				ids = []int{3}
			}
			photos := make([]map[string]any, 0, len(ids))
			for _, id := range ids {
				activity := 123
				if mode.Load() == 2 && id == 2 {
					activity = 124
				}
				photos = append(photos, map[string]any{"activity_no": activity, "id": id, "enable": 0, "is_sell": 0, "status": 5, "pic_name": "test", "origin_img": fmt.Sprintf("//pb.plusx.cn/plus/immedpic/123/%d.jpg?sign=public", id)})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "success": true, "result": map[string]any{"pics_total": 3, "pics_array": photos}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://live.photoplus.cn/live/pc/123/")
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String()+"#/live"), chromedp.Sleep(500*time.Millisecond), chromedp.Evaluate(`document.cookie='session=synthetic; Secure; Path=/'`, nil)); err != nil {
		t.Fatal(err)
	}
	testStarted.Store(true)
	got, handled, err := session.capturePhotoplus(ctx, page, page.String()+"#/live")
	if !handled || err != nil || len(got.Items) != 3 || got.Title != "Public Event" {
		t.Fatalf("capture: handled=%v items=%d title=%q err=%v", handled, len(got.Items), got.Title, err)
	}
	routed, routeErr := session.Capture(ctx)
	if routeErr != nil || len(routed.Items) != 3 || routed.Title != "Public Event" {
		t.Fatalf("Capture route: items=%d title=%q err=%v", len(routed.Items), routed.Title, routeErr)
	}
	if leak.Load() != 0 || pages.Load() < 3 {
		t.Fatalf("credentials leaked=%d page requests=%d", leak.Load(), pages.Load())
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://live.photoplus.cn/" || item.Metadata["photoplus_activity_id"] != "123" {
			t.Fatalf("unsafe item: %+v", item)
		}
	}
	var location, cookie string
	if err := session.run(ctx, chromedp.Location(&location), chromedp.Evaluate(`document.cookie`, &cookie)); err != nil || location != page.String()+"#/live" || !strings.Contains(cookie, "session=synthetic") {
		t.Fatalf("original tab changed: location=%q cookie=%q err=%v", location, cookie, err)
	}
	if err := session.run(ctx, chromedp.Evaluate(`document.querySelector('.nav-bottom__ul li.active').className=''; document.querySelectorAll('.nav-bottom__ul li')[1].className='active'`, nil)); err != nil {
		t.Fatal(err)
	}
	if got, handled, err := session.capturePhotoplus(ctx, page, page.String()+"#/live"); !handled || err == nil || len(got.Items) != 0 {
		t.Fatalf("selected category: handled=%v items=%d err=%v", handled, len(got.Items), err)
	}
	if err := session.run(ctx, chromedp.Evaluate(`document.querySelectorAll('.nav-bottom__ul li')[1].className=''; document.querySelector('.nav-bottom__ul li').className='active'`, nil)); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	got, handled, err = session.capturePhotoplus(ctx, page, page.String()+"#/live")
	if !handled || err == nil || len(got.Items) != 0 {
		t.Fatalf("password-protected activity: handled=%v items=%d err=%v", handled, len(got.Items), err)
	}
	mode.Store(2)
	got, handled, err = session.capturePhotoplus(ctx, page, page.String()+"#/live")
	if !handled || err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "结果可能不完整") {
		t.Fatalf("partial activity mismatch: handled=%v items=%d err=%v", handled, len(got.Items), err)
	}
	mode.Store(4)
	got, handled, err = session.capturePhotoplus(ctx, page, page.String()+"#/live")
	if !handled || err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "活动数据未能读取") {
		t.Fatalf("partial HTTP failure: handled=%v items=%d err=%v", handled, len(got.Items), err)
	}
	mode.Store(3)
	got, handled, err = session.capturePhotoplus(ctx, page, page.String()+"#/live")
	if !handled || err == nil || len(got.Items) != 0 {
		t.Fatalf("login redirect: handled=%v items=%d err=%v", handled, len(got.Items), err)
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	_, handled, err = session.capturePhotoplus(stopped, page, page.String()+"#/live")
	if !handled || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: handled=%v err=%v", handled, err)
	}
	_, handled, err = session.capturePhotoplus(ctx, page, page.String()+"#/category/other")
	if !handled || err == nil {
		t.Fatalf("unknown category: handled=%v err=%v", handled, err)
	}
	if leak.Load() != 0 {
		t.Fatalf("credentials leaked on later requests: %d", leak.Load())
	}
}

// This opt-in check uses only a public event and an empty temporary profile.
func TestPhotoplusPublicLive(t *testing.T) {
	if os.Getenv("TULAI_PHOTOPLUS_LIVE") != "1" {
		t.Skip("set TULAI_PHOTOPLUS_LIVE=1 for public network validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	raw := "https://live.photoplus.cn/live/pc/15768755/#/live"
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), raw, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var observed string
	if err := session.run(ctx, chromedp.Location(&observed)); err != nil {
		t.Fatal(err)
	}
	page, err := crawlInputURL(observed)
	if err != nil {
		t.Fatal(err)
	}
	if !isPhotoplusPage(page) {
		t.Fatal("public event redirected away from activity")
	}
	got, warning := session.Capture(ctx)
	if len(got.Items) == 0 {
		t.Fatalf("public capture: items=%d warning=%v", len(got.Items), warning)
	}
	t.Logf("public capture: %d pictures; warning=%v", len(got.Items), warning)
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		if !strings.Contains(err.Error(), "网页图片不能访问其他内网地址") {
			t.Fatalf("browser image download failed: %T", err)
		}
		t.Log("browser image download blocked by private/fake-IP network policy")
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
			t.Fatalf("public client image download failed: %T", err)
		}
		t.Log("public client image download succeeded")
	} else {
		t.Log("browser image download succeeded")
	}
}
