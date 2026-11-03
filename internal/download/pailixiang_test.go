package download

import (
	"context"
	"encoding/base64"
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

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestPailixiangRouteAndImage(t *testing.T) {
	for _, test := range []struct {
		raw, code string
		legacy    bool
	}{
		{"https://live.pailixiang.com/album/a12956069967", "a12956069967", false},
		{"https://live.pailixiang.com/album/a123/", "a123", false},
		{"https://www.pailixiang.com/album_ia123.html", "", true},
		{"https://live.pailixiang.com.evil.test/album/a123", "", false},
		{"https://x@live.pailixiang.com/album/a123", "", false},
		{"https://live.pailixiang.com:444/album/a123", "", false},
		{"https://live.pailixiang.com/album/a123/other", "", false},
		{"https://live.pailixiang.com/album/a%2f123", "", false},
	} {
		u, err := url.Parse(test.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := pailixiangAlbumID(u); got != test.code {
			t.Errorf("%s = %q", test.raw, got)
		}
		if got := pailixiangLegacy(u); got != test.legacy {
			t.Errorf("%s legacy = %v", test.raw, got)
		}
	}
	for _, test := range []struct {
		raw string
		ok  bool
	}{
		{"https://img1.pailixiang.com/album/a123/1.jpg?signature=public", true},
		{"https://img1.pailixiang.com:443/album/a123/1.jpg", true},
		{"https://img1.pailixiang.com/album/a124/1.jpg", false},
		{"https://phnw1.pailixiang.com/album/a123/1.jpg", false},
		{"https://img1.pailixiang.com.evil.test/album/a123/1.jpg", false},
		{"https://img1.pailixiang.com@evil.test/album/a123/1.jpg", false},
		{"https://img1.pailixiang.com:444/album/a123/1.jpg", false},
		{"http://img1.pailixiang.com/album/a123/1.jpg", false},
		{"https://img1.pailixiang.com/album/a123/%2e%2e/1.jpg", false},
	} {
		_, got := pailixiangImage(test.raw, "a123")
		if got != test.ok {
			t.Errorf("image accepted=%v for %q", got, test.raw)
		}
	}
}

func pailixiangTestDetail() map[string]any {
	return map[string]any{"Code": 0, "Data": map[string]any{
		"Entity":  map[string]any{"ID": "789", "Code": "a123", "Name": "Test album", "EnterValidateType": 0, "LockType": 0, "IsOnlyByFindMe": false, "IsPhotoDownload": true, "IsPayDownload": false, "IsInternalLink": false, "PhotoDownloadMaxQty": 3},
		"ResCode": 0, "IsNw": false, "IsStart": true, "PhotoGroup": nil, "NavList": []any{map[string]any{"ID": "photoLive"}},
	}}
}
func pailixiangTestBody(x any) []byte { b, _ := json.Marshal(x); return b }
func pailixiangPostEntry(x any) []*network.PostDataEntry {
	return []*network.PostDataEntry{{Bytes: base64.StdEncoding.EncodeToString(pailixiangTestBody(x))}}
}

func TestPailixiangPermissionsAndRequests(t *testing.T) {
	view := pailixiangTestDetail()
	if _, max, err := pailixiangDetail(pailixiangTestBody(view), "a123"); err != nil || max != 3 {
		t.Fatalf("valid view: max=%d err=%v", max, err)
	}
	for _, test := range []struct {
		field string
		value any
	}{
		{"EnterValidateType", 1}, {"LockType", 1}, {"IsOnlyByFindMe", true}, {"IsPhotoDownload", false}, {"IsPayDownload", true}, {"IsInternalLink", true}, {"PhotoDownloadMaxQty", nil}, {"Code", "a124"},
	} {
		t.Run(test.field, func(t *testing.T) {
			v := pailixiangTestDetail()
			v["Data"].(map[string]any)["Entity"].(map[string]any)[test.field] = test.value
			if _, _, err := pailixiangDetail(pailixiangTestBody(v), "a123"); err == nil {
				t.Fatal("restricted view accepted")
			}
		})
	}
	for _, test := range []struct {
		field string
		value any
	}{{"ResCode", 1}, {"IsNw", true}, {"IsStart", false}, {"PhotoGroup", map[string]any{}}, {"NavList", []any{map[string]any{"ID": "timeline"}}}} {
		t.Run(test.field, func(t *testing.T) {
			v := pailixiangTestDetail()
			v["Data"].(map[string]any)[test.field] = test.value
			if _, _, err := pailixiangDetail(pailixiangTestBody(v), "a123"); err == nil {
				t.Fatal("restricted view accepted")
			}
		})
	}
	viewURL := "https://mapi.pailixiang.com/plx/WapAbm/AlbumGetView?ak=synthetic"
	if _, ok := pailixiangRequestFor(viewURL, "POST", pailixiangPostEntry(map[string]any{"ID": "123"}), "a123"); !ok {
		t.Fatal("valid view request rejected")
	}
	if _, ok := pailixiangRequestFor(viewURL, "POST", pailixiangPostEntry(map[string]any{"ID": "123", "Nw": "secret"}), "a123"); ok {
		t.Fatal("internal link request accepted")
	}
	list := map[string]any{"AlbumID": "789", "GroupID": "", "SearchType": 0, "IsPayDownload": false, "IsNw": false, "StartIndex": 1, "SearchCount": 80}
	listURL := "https://mapi.pailixiang.com/plx/WapAbm/AlbumSearchPhoto?ak=synthetic"
	if req, ok := pailixiangRequestFor(listURL, "POST", pailixiangPostEntry(list), "a123"); !ok || req.album != "789" {
		t.Fatalf("valid list request rejected: %+v", req)
	}
	for _, test := range []struct {
		field string
		value any
	}{{"GroupID", "secret"}, {"SearchType", 1}, {"IsPayDownload", true}, {"IsNw", true}, {"StartIndex", 101}, {"SearchCount", 101}, {"SearchText", "private"}} {
		clone := mapsClone(list)
		clone[test.field] = test.value
		if _, ok := pailixiangRequestFor(listURL, "POST", pailixiangPostEntry(clone), "a123"); ok {
			t.Errorf("accepted %s", test.field)
		}
	}
	if pailixiangResponseURL("https://evil.test/plx/WapAbm/AlbumSearchPhoto", "list") {
		t.Fatal("accepted off-site redirect")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	page, _ := url.Parse("https://live.pailixiang.com/album/a123")
	if _, err := discoverPailixiang(ctx, nil, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
func mapsClone(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestPailixiangRenderedAnonymous(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	var mode, leaks, pages, redirected atomic.Int32
	var started atomic.Bool
	viewStarted := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if started.Load() && r.Header.Get("Cookie") != "" {
			leaks.Add(1)
		}
		if r.Host == "mapi.pailixiang.com" {
			w.Header().Set("Access-Control-Allow-Origin", "https://live.pailixiang.com")
			w.Header().Set("Access-Control-Allow-Headers", "content-type")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			if r.Method == "OPTIONS" {
				return
			}
		}
		switch r.URL.Path {
		case "/album/a123":
			if mode.Load() == 3 {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<html><style>.tall{height:1800px}</style><ul><li class="nav_item active">照片直播</li><li class="nav_item">热门</li></ul><div class="tall"></div><script>
const post=(path,data)=>fetch('https://mapi.pailixiang.com/plx/WapAbm/'+path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});
post('AlbumGetView',{ID:'123'}); const list=(n)=>post('AlbumSearchPhoto',{AlbumID:'789',GroupID:'',SearchType:0,IsPayDownload:false,IsNw:false,StartIndex:n,SearchCount:2});list(1);let done=false;onscroll=()=>{if(!done){done=true;list(3)}};
</script></html>`)
		case "/plx/WapAbm/AlbumGetView":
			if mode.Load() == 5 {
				select {
				case viewStarted <- struct{}{}:
				default:
				}
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
				return
			}
			if mode.Load() == 6 {
				http.Redirect(w, r, "/plx/WapAbm/redirected", http.StatusTemporaryRedirect)
				return
			}
			v := pailixiangTestDetail()
			if mode.Load() == 1 {
				v["Data"].(map[string]any)["Entity"].(map[string]any)["EnterValidateType"] = 1
			}
			_ = json.NewEncoder(w).Encode(v)
		case "/plx/WapAbm/AlbumSearchPhoto":
			pages.Add(1)
			var req struct {
				StartIndex int `json:"StartIndex"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if mode.Load() == 4 && req.StartIndex > 1 {
				http.Error(w, "failure", 503)
				return
			}
			ids := []int{1, 2}
			if req.StartIndex > 1 {
				ids = []int{3}
			}
			rows := make([]any, 0, len(ids))
			for _, id := range ids {
				code := "a123"
				if mode.Load() == 2 && id == 2 {
					code = "a124"
				}
				rows = append(rows, map[string]any{"ID": fmt.Sprintf("%032d", id), "Name": "image", "DownloadImageUrl": fmt.Sprintf("https://img1.pailixiang.com/album/%s/%d.jpg?sign=synthetic", code, id), "IsTransfer": false, "IsGetTransferFileUrl": false})
			}
			total := 3
			if req.StartIndex > 1 {
				total = 0
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Code": 0, "Data": rows, "TotalCount": total})
		case "/plx/WapAbm/redirected":
			redirected.Add(1)
			http.Error(w, "unexpected redirect", http.StatusBadRequest)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://live.pailixiang.com/album/a123")
	s := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := s.run(ctx, camoufox.Navigate(page.String()), chromedp.Sleep(500*time.Millisecond), chromedp.Evaluate(`document.cookie='session=synthetic; Secure; Path=/'`, nil)); err != nil {
		t.Fatal(err)
	}
	started.Store(true)
	got, handled, err := s.capturePailixiang(ctx, page, page.String())
	if !handled || err != nil || len(got.Items) != 3 || got.Title != "Test album" {
		t.Fatalf("capture handled=%v count=%d title=%q err=%v", handled, len(got.Items), got.Title, err)
	}
	routed, routeErr := s.Capture(ctx)
	if routeErr != nil || len(routed.Items) != 3 || routed.Title != "Test album" {
		t.Fatalf("Capture route: count=%d title=%q err=%v", len(routed.Items), routed.Title, routeErr)
	}
	if leaks.Load() != 0 || pages.Load() < 3 {
		t.Fatalf("cookie leaks=%d pages=%d", leaks.Load(), pages.Load())
	}
	var location, cookie string
	if err := s.run(ctx, chromedp.Location(&location), chromedp.Evaluate(`document.cookie`, &cookie)); err != nil || location != page.String() || !strings.Contains(cookie, "synthetic") {
		t.Fatalf("original tab changed: %q err=%v", location, err)
	}
	if err := s.run(ctx, chromedp.Evaluate(`document.querySelectorAll('li.nav_item')[0].classList.remove('active');document.querySelectorAll('li.nav_item')[1].classList.add('active')`, nil)); err != nil {
		t.Fatal(err)
	}
	if found, matched, err := s.capturePailixiang(ctx, page, page.String()); !matched || err == nil || len(found.Items) != 0 {
		t.Fatalf("selected other tab: matched=%v count=%d err=%v", matched, len(found.Items), err)
	}
	if err := s.run(ctx, chromedp.Evaluate(`document.querySelectorAll('li.nav_item')[1].classList.remove('active');document.querySelectorAll('li.nav_item')[0].classList.add('active')`, nil)); err != nil {
		t.Fatal(err)
	}
	mode.Store(1)
	got, handled, err = s.capturePailixiang(ctx, page, page.String())
	if !handled || err == nil || len(got.Items) != 0 {
		t.Fatalf("password: handled=%v count=%d err=%v", handled, len(got.Items), err)
	}
	mode.Store(2)
	got, handled, err = s.capturePailixiang(ctx, page, page.String())
	if !handled || err == nil || len(got.Items) != 2 {
		t.Fatalf("other album: handled=%v count=%d err=%v", handled, len(got.Items), err)
	}
	mode.Store(4)
	got, handled, err = s.capturePailixiang(ctx, page, page.String())
	if !handled || err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "活动数据未能读取") {
		t.Fatalf("HTTP error: handled=%v count=%d err=%v", handled, len(got.Items), err)
	}
	mode.Store(3)
	got, handled, err = s.capturePailixiang(ctx, page, page.String())
	if !handled || err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect: handled=%v count=%d err=%v", handled, len(got.Items), err)
	}
	mode.Store(6)
	got, handled, err = s.capturePailixiang(ctx, page, page.String())
	if !handled || err == nil || len(got.Items) != 0 || redirected.Load() != 0 {
		t.Fatalf("API redirect: handled=%v count=%d redirects=%d err=%v", handled, len(got.Items), redirected.Load(), err)
	}
	mode.Store(5)
	pending, stopPending := context.WithCancel(ctx)
	finished := make(chan error, 1)
	go func() { _, _, err := s.capturePailixiang(pending, page, page.String()); finished <- err }()
	select {
	case <-viewStarted:
	case <-time.After(8 * time.Second):
		stopPending()
		t.Fatal("waiting request did not start")
	}
	stopPending()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel while waiting: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not interrupt wait")
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if _, matched, err := s.capturePailixiang(stopped, page, page.String()); !matched || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: matched=%v err=%v", matched, err)
	}
	if leaks.Load() != 0 {
		t.Fatalf("cookie leaked=%d", leaks.Load())
	}
}

func TestPailixiangPublicLive(t *testing.T) {
	if os.Getenv("TULAI_PAILIXIANG_LIVE") != "1" {
		t.Skip("set TULAI_PAILIXIANG_LIVE=1 for public network validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	raw := "https://live.pailixiang.com/album/a12956069967"
	s, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), raw, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page, _ := url.Parse(raw)
	if err := s.run(ctx, chromedp.Sleep(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !isPailixiangPage(page) {
		t.Fatal("invalid public page")
	}
	got, warning := s.Capture(ctx)
	if len(got.Items) == 0 {
		t.Fatalf("public capture count=%d err=%v", len(got.Items), warning)
	}
	t.Logf("public capture: %d photos warning=%v", len(got.Items), warning)
	if err := New(Options{Getter: s}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		if !strings.Contains(err.Error(), "网页图片不能访问其他内网地址") {
			t.Fatalf("browser image download failed: %T", err)
		}
		t.Log("browser image download blocked by private/fake-IP network policy")
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
			t.Fatalf("public image download failed: %T", err)
		}
		t.Log("public client image download succeeded")
	} else {
		t.Log("browser image download succeeded")
	}
}
