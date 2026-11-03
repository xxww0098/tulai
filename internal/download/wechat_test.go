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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"

	"golang.org/x/net/html"
)

func TestWechatArticleURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://mp.weixin.qq.com/s/Ah9Bv8Iwk8VGkECE9g_GhQ":         true,
		"https://mp.weixin.qq.com/s?__biz=abc&mid=123&idx=1&sn=xyz": true,
		"https://mp.weixin.qq.com/s?src=11&signature=x":             true,
		"http://mp.weixin.qq.com/s/short":                           true,
		"https://mp.weixin.qq.com/s":                                false,
		"https://mp.weixin.qq.com/s/short/extra":                    false,
		"https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha":          false,
		"https://mp.weixin.qq.com.evil.test/s/short":                false,
		"https://user:pass@mp.weixin.qq.com/s/short":                false,
		"https://mp.weixin.qq.com:444/s/short":                      false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isWechatArticle(u); got != want {
			t.Errorf("isWechatArticle(%q) = %v", raw, got)
		}
	}
}

func TestWechatVerificationURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha?source=1":  true,
		"https://mp.weixin.qq.com:443/mp/wappoc_appmsgcaptcha":       true,
		"https://mp.weixin.qq.com/s/test":                            false,
		"https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha/other":     false,
		"https://evil.test/mp/wappoc_appmsgcaptcha":                  false,
		"https://mp.weixin.qq.com.evil.test/mp/wappoc_appmsgcaptcha": false,
		"https://user@mp.weixin.qq.com/mp/wappoc_appmsgcaptcha":      false,
		"https://mp.weixin.qq.com:444/mp/wappoc_appmsgcaptcha":       false,
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isWechatVerification(u); got != want {
			t.Errorf("isWechatVerification(%q) = %v", raw, got)
		}
	}
}

func TestWechatArticleContentAndPictureList(t *testing.T) {
	page, _ := url.Parse("https://mp.weixin.qq.com/s/example?token=synthetic")
	body := `<html><head><title>页面标题</title></head><body>
<img src="https://mmbiz.qpic.cn/avatar/0"><div id="activity-name">文章标题</div>
<div id="js_content"><img src="data:image/gif;base64,eA==" data-src="https://mmbiz.qpic.cn/mmbiz_jpg/body/640?wx_fmt=jpeg&amp;copy=1"><img data-src="https://mmbiz.qpic.cn/mmbiz_jpg/body/640?wx_fmt=jpeg&amp;copy=1"></div>
<script>
// window.picture_page_info_list = [{cdn_url:'https://mmbiz.qpic.cn/comment/0'}]
var unrelated = [{cdn_url:'https://mmbiz.qpic.cn/other/0'}];
window.picture_page_info_list = [
 {width: 100, height: 200, cdn_url: 'https://mmbiz.qpic.cn/mmbiz_png/one/640?wx_fmt=png\x26amp;x=2', watermark_info: {cdn_url:'https://mmbiz.qpic.cn/watermark/0'}},
 {share_cover:{cdn_url:'https://mmbiz.qpic.cn/share/0'}, "cdn_url":"https:\/\/mmbiz.qpic.cn\/mmbiz_svg\/two\/0?wx_fmt=svg"}
].slice(0,20);
</script></body></html>`
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, err := wechatArticlePage(context.Background(), doc, page)
	if err != nil || !got.IsPage || got.Title != "文章标题" || len(got.Items) != 3 {
		t.Fatalf("got %+v err=%v", got, err)
	}
	for i, want := range []string{"https://mmbiz.qpic.cn/mmbiz_jpg/body/640?wx_fmt=jpeg&copy=1", "https://mmbiz.qpic.cn/mmbiz_png/one/640?wx_fmt=png&x=2", "https://mmbiz.qpic.cn/mmbiz_svg/two/0?wx_fmt=svg"} {
		item := got.Items[i]
		if item.URL != want || !item.RequireImage || item.Headers["Referer"] != "https://mp.weixin.qq.com/" {
			t.Errorf("item %d = %+v, want %s", i, item, want)
		}
	}
}

func TestWechatEncodedListAndCover(t *testing.T) {
	page, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	jsonList := `[{"cdn_url":"https://mmbiz.qpic.cn/mmbiz_jpg/one/0?wx_fmt=jpeg"},{"cdn_url":"https://mmbiz.qpic.cn/mmbiz_jpg/two/0?wx_fmt=jpeg"}]`
	body := `<title>封面</title><script>var cover = "https://mmbiz.qpic.cn/cover/0"; var picturePageInfoList = ` + strconv.Quote(jsonList) + `;</script>`
	doc, _ := html.Parse(strings.NewReader(body))
	got, err := wechatArticlePage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://mmbiz.qpic.cn/cover/0" || !strings.Contains(got.Items[2].URL, "/two/0") {
		t.Fatalf("encoded list = %+v err=%v", got.Items, err)
	}
}

func TestWechatScriptTitle(t *testing.T) {
	page, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	doc, _ := html.Parse(strings.NewReader(`<script>var msg_title = '书\x26amp;画';</script><div id="js_content"><img data-src="https://mmbiz.qpic.cn/body/640"></div>`))
	got, err := wechatArticlePage(context.Background(), doc, page)
	if err != nil || got.Title != "书&画" {
		t.Fatalf("title=%q err=%v", got.Title, err)
	}
}

func TestWechatPartialAndUnsafe(t *testing.T) {
	page, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	for _, body := range []string{
		`<div id="js_content"><img data-src="https://mmbiz.qpic.cn/good/0"><img data-src="https://example.test/image.jpg"></div>`,
		`<div id="js_content"><img data-src="https://mmbiz.qpic.cn/good/0"><img data-src="https://mmbiz.qpic.cn.evil.test/image.jpg"></div>`,
		`<div id="js_content"><img data-src="https://mmbiz.qpic.cn/good/0"><img data-src="https://user:pass@mmbiz.qpic.cn/image.jpg"></div>`,
		`<div id="js_content"><img data-src="https://mmbiz.qpic.cn/good/0"><img data-src="https://mmbiz.qpic.cn:444/image.jpg"></div>`,
		`<script>window.picture_page_info_list = [{cdn_url:'https://mmbiz.qpic.cn/good/0'}, {cdn_url: fetch('https://evil.test')}];</script>`,
		`<div id="js_content"><img data-src="https://mmbiz.qpic.cn/good/0"></div><script>window.picture_page_info_list = unknownData();</script>`,
		`<script>var picturePageInfoList = "[{\"cdn_url\":\"https://mmbiz.qpic.cn/good/0\"}]"; window.picture_page_info_list = unknownData();</script>`,
	} {
		doc, _ := html.Parse(strings.NewReader(body))
		got, err := wechatArticlePage(context.Background(), doc, page)
		if err == nil || len(got.Items) != 1 || got.Items[0].URL != "https://mmbiz.qpic.cn/good/0" {
			t.Errorf("partial = %+v err=%v for %s", got.Items, err, body)
		}
	}
}

func TestWechatLoginRedirectAndCancellation(t *testing.T) {
	page, _ := url.Parse("https://mp.weixin.qq.com/s/example")
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: "https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha", Body: io.NopCloser(strings.NewReader(`<title>安全验证</title>`))}, nil
	})
	got, err := discoverWechatArticle(context.Background(), getter, page)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "验证") {
		t.Fatalf("captcha = %+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverWechatArticle(ctx, getter, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
}

func TestWechatListLiteralBoundaries(t *testing.T) {
	for _, text := range []string{
		`[{cdn_url: 'https://mmbiz.qpic.cn/one/0'}].concat(other)`,
		`[{cdn_url: 'https://mmbiz.qpic.cn/one/0'}].slice(0,100)`,
		`[{cdn_url: 'https://mmbiz.qpic.cn/one/0' + calculate()}]`,
		`[{watermark_info:{cdn_url:'https://mmbiz.qpic.cn/other/0'}}]`,
	} {
		if _, err := wechatImagesLiteral(text); err == nil {
			t.Errorf("accepted %s", text)
		}
	}
}

func TestWechatRenderedCaptureAndVerification(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>placeholder</html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const article = "https://mp.weixin.qq.com/s/example"
	const captcha = "https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha?source=synthetic"
	const articleBody = `<html><title>文章</title><div id="js_content"><img data-src="https://mmbiz.qpic.cn/body/640"></div><script>window.picture_page_info_list = [{cdn_url:'https://mmbiz.qpic.cn/list/0'}].slice(0,20);</script><img src="https://mmbiz.qpic.cn/avatar/0"></html>`
	const captchaBody = `<html><title>安全验证</title><img src="https://mmbiz.qpic.cn/captcha/0"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && (paused.Request.URL == article || paused.Request.URL == captcha) {
				body := articleBody
				if paused.Request.URL == captcha {
					body = captchaBody
				}
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case responses <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	session.start, _ = url.Parse(article)
	if err := session.run(ctx, camoufox.Navigate(article)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || got.Title != "文章" || len(got.Items) != 2 || got.Items[0].URL != "https://mmbiz.qpic.cn/body/640" || got.Items[1].URL != "https://mmbiz.qpic.cn/list/0" {
		t.Fatalf("rendered article=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate(captcha)); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "验证") || len(got.Items) != 0 {
		t.Fatalf("rendered captcha=%+v err=%v", got, err)
	}
	select {
	case err := <-responses:
		t.Fatal(err)
	default:
	}
}
