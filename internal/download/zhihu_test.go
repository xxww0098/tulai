package download

import (
	"context"
	"encoding/base64"
	"errors"
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
)

func TestZhihuArticleAndAnswer(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if strings.HasPrefix(u.Path, "/p/") {
			return tiebaTestResponse(raw, http.StatusOK, `<meta property="og:url" content="https://zhuanlan.zhihu.com/p/123"><img data-original="https://pic1.zhimg.com/avatar.jpg"><div class="Post-RichText"><img data-original="https://pic1.zhimg.com/article.jpg"><img data-original="https://pic1.zhimg.com/article.jpg"></div>`), nil
		}
		return tiebaTestResponse(raw, http.StatusOK, `<div class="AnswerItem" data-zop='{"itemId":456,"type":"answer"}'><div class="RichContent-inner"><img data-original="https://pic1.zhimg.com/answer.jpg"></div></div><div class="AnswerItem" data-zop='{"itemId":789,"type":"answer"}'><div class="RichContent-inner"><img data-original="https://pic1.zhimg.com/other.jpg"></div></div>`), nil
	})
	for _, tc := range []struct{ page, image string }{
		{"https://zhuanlan.zhihu.com/p/123", "https://pic1.zhimg.com/article.jpg"},
		{"https://www.zhihu.com/question/123/answer/456", "https://pic1.zhimg.com/answer.jpg"},
	} {
		got, err := Discover(context.Background(), getter, tc.page, 1)
		if err != nil || len(got.Items) != 1 || got.Items[0].URL != tc.image || !got.Items[0].RequireImage {
			t.Fatalf("%s: images=%+v err=%v", tc.page, got.Items, err)
		}
	}
}

func TestZhihuIdentityAndMissingBody(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, http.StatusOK, `<meta property="og:url" content="https://zhuanlan.zhihu.com/p/999"><div class="Post-RichText"><img data-original="https://pic1.zhimg.com/wrong.jpg"></div>`), nil
	})
	got, err := Discover(context.Background(), getter, "https://zhuanlan.zhihu.com/p/123", 1)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("wrong identity: %+v %v", got.Items, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, http.StatusOK, `<img data-original="https://pic1.zhimg.com/avatar.jpg">`), nil
	})
	got, err = Discover(context.Background(), getter, "https://www.zhihu.com/question/123/answer/456", 1)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "正文") {
		t.Fatalf("missing answer: %+v %v", got.Items, err)
	}
}

func TestZhihuRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
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
	const target = "https://zhuanlan.zhihu.com/p/123"
	const html = `<meta property="og:url" content="https://zhuanlan.zhihu.com/p/123"><div class="Post-RichText"><img data-original="https://pic1.zhimg.com/article.jpg"></div><img data-original="https://pic1.zhimg.com/avatar.jpg">`
	var requests atomic.Int32
	session.getter.client.Transport = instagramTestTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("unexpected HTTP request")
	})
	errorsCh := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(html)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case errorsCh <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(target)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://pic1.zhimg.com/article.jpg" || requests.Load() != 0 {
		t.Fatalf("capture=%+v requests=%d err=%v", got.Items, requests.Load(), err)
	}
	select {
	case err := <-errorsCh:
		t.Fatal(err)
	default:
	}
}
