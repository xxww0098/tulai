package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	"golang.org/x/net/html"
)

const qyerTestPage = "https://photo.qyer.com/album/107442"
const qyerTestThumb = "https://pic.qyer.com/album/user/359/52/QkxcRxgDZA/index/180180"
const qyerTestLarge = "https://pic.qyer.com/album/user/359/52/QkxcRxgDZA/index/w1080"

func qyerTestFixture(album, photos string) string {
	return `<html><head><title>相册</title></head><body><span class="albumName">旅行相册</span><div id="photoList" class="photo-list" data-loaded="1" data-page="1" data-albumid="` + album + `"><ul class="photo-lists">` + photos + `</ul></div><img src="https://evil.test/not-album.jpg"></body></html>`
}
func qyerTestPhoto(album, id, src string) string {
	return `<li><a href="//photo.qyer.com/` + id + `/album/` + album + `" class="img-link" data-id="` + id + `"><img src="` + src + `"></a></li>`
}
func qyerDoc(t *testing.T, s string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func qyerResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}
}

func TestQyerAlbumReference(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{qyerTestPage, "107442"}, {"http://photo.qyer.com/album/107442/", "107442"},
		{"https://photo.qyer.com/album/u851873", ""}, {"https://photo.qyer.com/3595244/album/107442", ""},
		{"https://photo.qyer.com/album/107442/other", ""}, {"https://photo.qyer.com/album/107442%2Fother", ""},
		{"https://photo.qyer.com/album/../107442", ""}, {"https://evil.test/album/107442", ""},
		{"https://photo.qyer.com.evil.test/album/107442", ""}, {"https://user@photo.qyer.com/album/107442", ""},
		{"https://photo.qyer.com:8443/album/107442", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := qyerAlbumID(u); got != tc.id {
			t.Errorf("%s id=%q", tc.raw, got)
		}
	}
}

func TestQyerVisibleAlbum(t *testing.T) {
	page, _ := url.Parse(qyerTestPage + "?token=synthetic-secret")
	body := qyerTestFixture("107442", qyerTestPhoto("107442", "3595244", qyerTestThumb)+qyerTestPhoto("107442", "3595243", "https://pic1.qyer.com/album/user/359/52/QkxcRxgDYw/index/180180"))
	got, err := qyerAlbum(context.Background(), page, qyerDoc(t, body))
	if err == nil || !strings.Contains(err.Error(), "当前加载") || len(got.Items) != 2 || got.Items[0].URL != qyerTestLarge || got.Title != "旅行相册" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["qyer_album_id"] != "107442" || strings.Contains(fmt.Sprint(item.Headers), "synthetic-secret") {
			t.Fatalf("unsafe result %+v", item)
		}
	}
}

func TestQyerImageAndAlbumBoundaries(t *testing.T) {
	for _, raw := range []string{
		"https://evil.test/album/user/359/52/id/index/180180",
		"https://pic.qyer.com.evil.test/album/user/359/52/id/index/180180",
		"https://user@pic.qyer.com/album/user/359/52/id/index/180180",
		"https://pic.qyer.com:8443/album/user/359/52/id/index/180180",
		"http://pic.qyer.com/album/user/359/52/id/index/180180",
		"https://pic.qyer.com/album/user/359/52/id/index/180180?token=secret",
		"https://pic.qyer.com/album/user/359/52/id/index/w1080",
		"https://pic.qyer.com/album/user/359/../id/index/180180",
	} {
		if image, ok := qyerImage(raw); ok {
			t.Errorf("unsafe image accepted %q", image)
		}
	}
	page, _ := url.Parse(qyerTestPage)
	good := qyerTestPhoto("107442", "3595244", qyerTestThumb)
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"wrong album", qyerTestFixture("109022", good), 0},
		{"missing identity", strings.Replace(qyerTestFixture("107442", good), `data-albumid="107442"`, ``, 1), 0},
		{"other album link", qyerTestFixture("107442", qyerTestPhoto("109022", "3595244", qyerTestThumb)), 0},
		{"other photo id", qyerTestFixture("107442", strings.Replace(good, `data-id="3595244"`, `data-id="1"`, 1)), 0},
		{"invalid mixed image", qyerTestFixture("107442", good+qyerTestPhoto("107442", "2", "https://evil.test/album/user/359/52/id/index/180180")), 1},
		{"duplicate image", qyerTestFixture("107442", good+good), 1},
		{"login page", `<html><title>请登录</title></html>`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := qyerAlbum(context.Background(), page, qyerDoc(t, tc.body))
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestQyerStaticErrors(t *testing.T) {
	page, _ := url.Parse(qyerTestPage)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request sent")
		return nil, nil
	})
	if _, err := discoverQyerAlbum(ctx, getter, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		r := qyerResponse(raw, "Forbidden")
		r.StatusCode = 403
		return r, nil
	})
	got, err := discoverQyerAlbum(context.Background(), getter, page)
	if len(got.Items) != 0 || err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("status result=%+v err=%v", got, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		r := qyerResponse(raw, qyerTestFixture("107442", qyerTestPhoto("107442", "3595244", qyerTestThumb)))
		r.FinalURL = "https://photo.qyer.com/album/109022"
		return r, nil
	})
	got, err = discoverQyerAlbum(context.Background(), getter, page)
	if len(got.Items) != 0 || err == nil {
		t.Fatalf("redirect result=%+v err=%v", got, err)
	}
}

func TestQyerArchivedPages(t *testing.T) {
	dir := os.Getenv("TULAI_QYER_ARCHIVE_DIR")
	if dir == "" {
		t.Skip("set TULAI_QYER_ARCHIVE_DIR for public historical HTML")
	}
	for _, sample := range []struct {
		id    string
		count int
	}{{"107442", 114}, {"107838", 127}, {"109022", 27}} {
		b, err := os.ReadFile(filepath.Join(dir, "qyer-"+sample.id+".html"))
		if err != nil {
			t.Fatal(err)
		}
		page, _ := url.Parse("http://photo.qyer.com/album/" + sample.id)
		got, err := qyerAlbum(context.Background(), page, qyerDoc(t, string(b)))
		if err == nil || len(got.Items) != sample.count {
			t.Fatalf("album=%s images=%d err=%v", sample.id, len(got.Items), err)
		}
		t.Logf("historical album %s: %d gallery images; no confirmed pagination", sample.id, len(got.Items))
	}
}

func TestQyerPublicImageLive(t *testing.T) {
	if os.Getenv("TULAI_QYER_LIVE") != "1" {
		t.Skip("set TULAI_QYER_LIVE=1 to download one historically public image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	page, _ := url.Parse(qyerTestPage)
	got, err := qyerAlbum(ctx, page, qyerDoc(t, qyerTestFixture("107442", qyerTestPhoto("107442", "3595244", qyerTestThumb))))
	if err == nil || len(got.Items) != 1 {
		t.Fatalf("fixture parse=%+v err=%v", got.Items, err)
	}
	got.Items[0].Filename = "qyer-public"
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, got.Items[0], nil); err != nil {
		t.Fatalf("public historical image download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Size() == 0 {
		t.Fatalf("info err=%v", err)
	}
	t.Logf("historically public w1080 image downloaded: %d bytes; current album page not tested", info.Size())
}

func TestQyerRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for an isolated browser")
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
	body := qyerTestFixture("107442", qyerTestPhoto("107442", "3595244", qyerTestThumb)+qyerTestPhoto("107442", "3595243", "https://pic1.qyer.com/album/user/359/52/QkxcRxgDYw/index/180180"))
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == qyerTestPage {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if e := camoufox.Run(session.tab, action); e != nil {
				select {
				case responses <- e:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(qyerTestPage)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(qyerTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case e := <-responses:
		t.Fatalf("intercept: %v", e)
	default:
	}
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != qyerTestLarge {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
}
