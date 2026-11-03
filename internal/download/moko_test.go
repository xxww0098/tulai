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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"golang.org/x/net/html"
)

const mokoTestPost = "https://www.moko.cc/post/1052776.html"
const mokoTestList = "https://www.moko.cc/post/aaronsky/list.html"
const mokoTestCategory1 = "https://www.moko.cc/post/aaronsky/category/7641/1.html"
const mokoTestCategory2 = "https://www.moko.cc/post/aaronsky/category/7641/2.html"
const mokoTestImage = "http://img2.moko.cc/users/0/16/4813/post/b5/img2_src_9803620.jpg"

func mokoTestPic(src string) string {
	return `<div class="pic dBd"><p class="picBox"><img src2="` + src + `"></p></div>`
}
func mokoPostFixture(id, owner, uid, body string) string {
	return `<html><head><title>作品</title></head><body><input id="to_user_id" value="` + uid + `"><a id="workNickName" href="/` + owner + `/" title="作者">作者</a><div id="postContentDiv" class="article">` + body + `</div><input id="hidPostGeiLiPostId" value="` + id + `"><img src2="http://img2.moko.cc/users/0/16/4813/post/b5/img2_src_1.jpg"></body></html>`
}
func mokoTestListFixture(body string) string {
	return `<html><head><title>作者作品</title></head><body><a id="a_post" href="/post/aaronsky/list.html">展示</a>` + body + `</body></html>`
}
func mokoDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	n, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func mokoTestResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}
}

type mokoTestTransport func(*http.Request) (*http.Response, error)

func (f mokoTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMokoReferences(t *testing.T) {
	for _, tc := range []struct {
		raw, kind, id, owner, category string
		page                           int
	}{
		{mokoTestPost, "post", "1052776", "", "", 0}, {mokoTestList, "list", "", "aaronsky", "", 0}, {mokoTestCategory1, "category", "", "aaronsky", "7641", 1},
		{"http://moko.cc/post/1.html", "post", "1", "", "", 0}, {"https://www.moko.cc/post/Aaron/category/23/2.html", "category", "", "aaron", "23", 2},
		{"https://evil.test/post/1052776.html", "", "", "", "", 0}, {"https://www.moko.cc.evil.test/post/1.html", "", "", "", "", 0},
		{"https://user@www.moko.cc/post/1.html", "", "", "", "", 0}, {"https://www.moko.cc:8443/post/1.html", "", "", "", "", 0},
		{"https://www.moko.cc/post/../post/1.html", "", "", "", "", 0}, {"https://www.moko.cc/post/0.html", "", "", "", "", 0},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		r := mokoPageRef(u)
		if r.kind != tc.kind || r.id != tc.id || r.owner != tc.owner || r.category != tc.category || r.page != tc.page {
			t.Errorf("%q ref=%+v", tc.raw, r)
		}
	}
}
func TestMokoPostRestrictionAndIdentity(t *testing.T) {
	page, _ := url.Parse(mokoTestPost + "?token=synthetic-secret")
	another := "http://img2.moko.cc/users/0/16/4813/post/8a/img2_src_9803621.jpg"
	body := mokoPostFixture("1052776", "aaronsky", "4813", mokoTestPic(mokoTestImage)+`<div style="display:none">`+mokoTestPic(another)+`</div><div class="post-limit">游客仅能观看部分</div>`+mokoTestPic(another))
	got, err := mokoPost(context.Background(), page, mokoDoc(t, body))
	if err == nil || !strings.Contains(err.Error(), "登录") || len(got.result.Items) != 1 || got.result.Items[0].URL != mokoTestImage || got.owner != "aaronsky" {
		t.Fatalf("post=%+v err=%v", got, err)
	}
	item := got.result.Items[0]
	if !item.RequireImage || !item.PublicOnly || item.Metadata["moko_post_id"] != "1052776" || strings.Contains(fmt.Sprint(item.Headers), "synthetic-secret") {
		t.Fatalf("unsafe item %+v", item)
	}
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"wrong post", mokoPostFixture("1", "aaronsky", "4813", mokoTestPic(mokoTestImage)), 0},
		{"wrong user image", mokoPostFixture("1052776", "aaronsky", "123", mokoTestPic(mokoTestImage)), 0},
		{"other host", mokoPostFixture("1052776", "aaronsky", "4813", mokoTestPic("http://evil.test/users/0/16/4813/post/b5/img2_src_9803620.jpg")), 0},
		{"missing identity", strings.Replace(mokoPostFixture("1052776", "aaronsky", "4813", mokoTestPic(mokoTestImage)), `id="workNickName"`, `id="other"`, 1), 0},
		{"hidden login gate", mokoPostFixture("1052776", "aaronsky", "4813", `<div class="post-limit" hidden>登录</div>`+mokoTestPic(mokoTestImage)), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, e := mokoPost(context.Background(), page, mokoDoc(t, tc.body))
			if e == nil || len(result.result.Items) != tc.count {
				t.Fatalf("items=%+v err=%v", result.result.Items, e)
			}
		})
	}
}
func TestMokoImageRestrictions(t *testing.T) {
	for _, raw := range []string{"http://img2.moko.cc.evil.test/users/0/16/4813/post/b5/img2_src_9803620.jpg", "http://user@img2.moko.cc/users/0/16/4813/post/b5/img2_src_9803620.jpg", "http://img2.moko.cc:8080/users/0/16/4813/post/b5/img2_src_9803620.jpg", "http://img2.moko.cc/users/0/16/9999/post/b5/img2_src_9803620.jpg", "http://img2.moko.cc/users/0/16/4813/post/b5/img2_src_9803620.jpg?token=secret", "http://127.0.0.1/a.jpg"} {
		if got, ok := mokoImage(raw, "4813"); ok {
			t.Errorf("unsafe image accepted %q", got)
		}
	}
}
func TestMokoListPaginationAndPartialResults(t *testing.T) {
	root, _ := url.Parse(mokoTestList)
	first := mokoTestListFixture(`<div class="sBox"><p class="title"><a href="/post/aaronsky/category/7641/1.html">分类</a></p><a class="coverBg" href="/post/1052776.html">作品</a><a class="coverBg" href="https://evil.test/post/99.html">外站</a></div>`)
	category1 := mokoTestListFixture(`<div class="sBox"><a class="coverBg" href="/post/1052776.html">重复</a><a class="coverBg" href="/post/1048210.html">作品2</a><p class="page"><a href="/post/aaronsky/category/7641/2.html">下一页</a></p></div>`)
	category2 := mokoTestListFixture(`<div class="sBox"><a class="coverBg" href="/post/1054755.html">作品3</a></div>`)
	seen := []string{}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		seen = append(seen, raw)
		switch raw {
		case mokoTestList:
			return mokoTestResponse(raw, first), nil
		case mokoTestCategory1:
			return mokoTestResponse(raw, category1), nil
		case mokoTestCategory2:
			return mokoTestResponse(raw, category2), nil
		case mokoTestPost:
			return mokoTestResponse(raw, mokoPostFixture("1052776", "aaronsky", "4813", mokoTestPic(mokoTestImage))), nil
		case "https://www.moko.cc/post/1048210.html":
			return nil, errors.New("network failure")
		case "https://www.moko.cc/post/1054755.html":
			return mokoTestResponse(raw, mokoPostFixture("1054755", "aaronsky", "4813", mokoTestPic("http://img2.moko.cc/users/0/16/4813/post/8a/img2_src_9803621.jpg"))), nil
		default:
			t.Fatalf("unexpected request %s", raw)
			return nil, nil
		}
	})
	got, err := discoverMoko(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "部分作品") || len(got.Items) != 2 || len(seen) != 6 {
		t.Fatalf("items=%+v requests=%v err=%v", got.Items, seen, err)
	}
}
func TestMokoOwnerMismatchAndRedirect(t *testing.T) {
	root, _ := url.Parse(mokoTestList)
	list := mokoTestListFixture(`<div class="sBox"><a class="coverBg" href="/post/1052776.html">作品</a></div>`)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == mokoTestList {
			return mokoTestResponse(raw, list), nil
		}
		return mokoTestResponse(raw, mokoPostFixture("1052776", "other", "4813", mokoTestPic(mokoTestImage))), nil
	})
	if got, err := discoverMoko(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong owner items=%+v err=%v", got.Items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverMoko(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	client := NewHTTPGetter("")
	var external atomic.Bool
	client.client.Transport = mokoTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "www.moko.cc" {
			external.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/path"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if got, err := discoverMoko(context.Background(), client, root); err == nil || len(got.Items) != 0 || external.Load() {
		t.Fatalf("external visited=%v items=%+v err=%v", external.Load(), got.Items, err)
	}
}
func TestMokoArchivedPages(t *testing.T) {
	dir := os.Getenv("TULAI_MOKO_ARCHIVE_DIR")
	if dir == "" {
		t.Skip("set TULAI_MOKO_ARCHIVE_DIR for historical public pages")
	}
	for _, sample := range []struct{ file, id, owner string }{{"moko-post-1052776.html", "1052776", "aaronsky"}, {"moko-1000028.html", "1000028", "888888"}, {"moko-1000029.html", "1000029", "andywc"}} {
		b, err := os.ReadFile(filepath.Join(dir, sample.file))
		if err != nil {
			t.Fatal(err)
		}
		page, _ := url.Parse("http://www.moko.cc/post/" + sample.id + ".html")
		got, err := mokoPost(context.Background(), page, mokoDoc(t, string(b)))
		if err == nil || !got.limited || len(got.result.Items) != 6 || got.owner != sample.owner {
			t.Fatalf("historical %s owner=%s count=%d err=%v", sample.id, got.owner, len(got.result.Items), err)
		}
		t.Logf("historical post %s: 6 exposed images, visitor limit respected", sample.id)
	}
	for _, sample := range []struct {
		file, raw               string
		posts, categories, next int
	}{{"moko-aaronsky.html", "http://www.moko.cc/post/aaronsky/list.html", 8, 3, 0}, {"moko-aaronsky-category-1.html", "http://www.moko.cc/post/aaronsky/category/7641/1.html", 28, 0, 1}} {
		b, err := os.ReadFile(filepath.Join(dir, sample.file))
		if err != nil {
			t.Fatal(err)
		}
		page, _ := url.Parse(sample.raw)
		got, err := mokoList(context.Background(), page, mokoDoc(t, string(b)))
		if err != nil || len(got.posts) != sample.posts || len(got.categories) != sample.categories || len(got.next) != sample.next {
			t.Fatalf("historical list %s posts=%d categories=%d next=%d err=%v", sample.file, len(got.posts), len(got.categories), len(got.next), err)
		}
		t.Logf("historical %s: %d posts, %d categories, %d next", sample.file, len(got.posts), len(got.categories), len(got.next))
	}
}
func TestMokoPublicLive(t *testing.T) {
	if os.Getenv("TULAI_MOKO_LIVE") != "1" {
		t.Skip("set TULAI_MOKO_LIVE=1 for a public old work")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "http://www.moko.cc/post/1052776.html", 1)
	if err == nil || len(got.Items) != 0 {
		t.Skipf("site state changed; images=%d err=%v", len(got.Items), err)
	}
	t.Logf("current work unavailable: %v", err)
}
func TestMokoRenderedCapture(t *testing.T) {
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
	body := mokoPostFixture("1052776", "aaronsky", "4813", mokoTestPic(mokoTestImage)+`<div class="post-limit">游客仅能观看部分</div>`+mokoTestPic("http://img2.moko.cc/users/0/16/4813/post/8a/img2_src_9803621.jpg"))
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == mokoTestPost {
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
	start, _ := url.Parse(mokoTestPost)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(mokoTestPost)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case e := <-responses:
		t.Fatalf("intercept: %v", e)
	default:
	}
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != mokoTestImage {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
}
