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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

const xianyuTestItem = "https://www.goofish.com/item?id=123"
const xianyuTestImage1 = "https://img.alicdn.com/bao/uploaded/i1/first-fleamarket.jpg_Q90.jpg_.webp"
const xianyuTestImage2 = "https://img.alicdn.com/bao/uploaded/i2/second-fleamarket.jpg_Q90.jpg_.webp"

func xianyuFixture(itemID, image1, image2 string) string {
	return `<html><body><div class="item-container--hash"><div class="item-main-container--hash"><div class="item-main-window--hash"><div class="item-main-window-carousel--hash"><div class="carousel-container--hash"><div class="ant-image"><img class="ant-image-img css-hash" src="` + image1 + `" style="display:none"></div><div class="ant-image"><img class="ant-image-img css-hash" src="` + image2 + `" style="display:none"></div></div></div></div><div><div class="main--hash"><span class="desc--hash">二手相机描述</span></div><a href="/im?itemId=` + itemID + `&amp;peerUserId=456">聊一聊</a></div></div><div class="item-feeds-container--hash"><img class="ant-image-img css-hash" src="https://img.alicdn.com/bao/uploaded/recommended.jpg"></div></div></body></html>`
}

func xianyuResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestXianyuItemAndImageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{xianyuTestItem, true},
		{"https://www.goofish.com/item?categoryId=126862528&id=1002001404411", true},
		{"https://goofish.com/item?id=123", true},
		{"https://www.goofish.com/item?id=0", false},
		{"https://www.goofish.com/item?id=123&id=456", false},
		{"https://www.goofish.com/item?id=abc", false},
		{"https://www.goofish.com/item/123?id=123", false},
		{"https://www.goofish.com.evil.test/item?id=123", false},
		{"https://me@www.goofish.com/item?id=123", false},
		{"https://www.goofish.com:8443/item?id=123", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isXianyuItem(u); got != tc.want {
			t.Errorf("isXianyuItem(%q)=%v", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"http://img.alicdn.com/bao/uploaded/a.jpg", "https://img.alicdn.com.evil.test/bao/uploaded/a.jpg",
		"https://a@img.alicdn.com/bao/uploaded/a.jpg", "https://img.alicdn.com:444/bao/uploaded/a.jpg",
		"https://127.0.0.1/bao/uploaded/a.jpg", "https://img.alicdn.com/imgextra/a.jpg",
		"https://img.alicdn.com/bao/uploaded/../avatar.jpg",
	} {
		if _, ok := xianyuImageURL(raw); ok {
			t.Errorf("accepted %q", raw)
		}
	}
	if got, ok := xianyuImageURL("//img.alicdn.com/bao/uploaded/a.jpg#ignored"); !ok || got != "https://img.alicdn.com/bao/uploaded/a.jpg" {
		t.Fatalf("image=%q ok=%v", got, ok)
	}
}

func TestXianyuGalleryAndPartial(t *testing.T) {
	root, _ := url.Parse(xianyuTestItem)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return xianyuResponse(raw, xianyuFixture("123", xianyuTestImage1, xianyuTestImage2)), nil
	})
	got, err := Discover(context.Background(), getter, root.String(), 1)
	if err == nil || !strings.Contains(err.Error(), "压缩图") || got.Title != "二手相机描述" || len(got.Items) != 2 {
		t.Fatalf("title=%q items=%+v err=%v", got.Title, got.Items, err)
	}
	if got.Items[0].URL != xianyuTestImage1 || got.Items[1].URL != xianyuTestImage2 {
		t.Fatalf("items=%+v", got.Items)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["xianyu_item_id"] != "123" || item.Headers["Referer"] != "https://www.goofish.com/" {
			t.Fatalf("unsafe item %+v", item)
		}
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return xianyuResponse(raw, xianyuFixture("123", xianyuTestImage1, "https://evil.test/bao/uploaded/x.jpg")), nil
	})
	got, err = discoverXianyu(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "未通过校验") || len(got.Items) != 1 || got.Items[0].URL != xianyuTestImage1 {
		t.Fatalf("partial=%+v err=%v", got.Items, err)
	}
}

func TestXianyuIdentityAndChallenge(t *testing.T) {
	root, _ := url.Parse(xianyuTestItem)
	for _, tc := range []struct {
		name, body string
		want       string
	}{
		{"wrong ID", xianyuFixture("999", xianyuTestImage1, xianyuTestImage2), "商品编号"},
		{"missing ID", strings.Replace(xianyuFixture("123", xianyuTestImage1, xianyuTestImage2), "/im?itemId=123", "/im?itemId=", 1), "商品编号"},
		{"external fake ID", strings.Replace(xianyuFixture("123", xianyuTestImage1, xianyuTestImage2), "/im?itemId=123", "https://evil.test/im?itemId=123", 1), "商品编号"},
		{"conflicting ID", strings.Replace(xianyuFixture("123", xianyuTestImage1, xianyuTestImage2), "</body>", `<div></div></body>`, 1), ""},
		{"block", `<html><div class="error-container--O03X5Fqd">非法访问 为了保障您的体验，请使用正常浏览器访问闲鱼</div>` + xianyuFixture("123", xianyuTestImage1, xianyuTestImage2) + `</html>`, "安全验证"},
		{"shell with recommendations", `<html><div class="item-feeds-container--hash"><img class="ant-image-img css-hash" src="` + xianyuTestImage1 + `"></div></html>`, "尚未加载"},
	} {
		if tc.name == "conflicting ID" {
			tc.body = strings.Replace(tc.body, "聊一聊</a>", `聊一聊</a><a href="/create-order?itemId=999">购买</a>`, 1)
			tc.want = "商品编号"
		}
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return xianyuResponse(raw, tc.body), nil
			})
			got, err := discoverXianyu(context.Background(), getter, root)
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(got.Items) != 0 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestXianyuRedirectAndCancel(t *testing.T) {
	root, _ := url.Parse(xianyuTestItem)
	for _, target := range []string{"https://www.goofish.com/login", "https://passport.goofish.com/mini_login.htm", "https://www.goofish.com/item?id=456", "https://evil.test/item?id=123"} {
		getter := GetterFunc(func(_ context.Context, _ string, _ func(*http.Request)) (*Response, error) {
			return xianyuResponse(target, `<html></html>`), nil
		})
		got, err := discoverXianyu(context.Background(), getter, root)
		if err == nil || len(got.Items) != 0 {
			t.Fatalf("target=%q items=%+v err=%v", target, got.Items, err)
		}
		if strings.Contains(target, "login") && !strings.Contains(err.Error(), "登录") {
			t.Fatalf("login=%v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(_ context.Context, _ string, _ func(*http.Request)) (*Response, error) {
		t.Fatal("cancelled request reached getter")
		return nil, nil
	})
	if _, err := discoverXianyu(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled=%v", err)
	}
}

func TestXianyuRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome")
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
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == xianyuTestItem {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(xianyuFixture("123", xianyuTestImage1, xianyuTestImage2))))
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
	start, _ := url.Parse(xianyuTestItem)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(xianyuTestItem)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err == nil || !strings.Contains(err.Error(), "压缩图") || len(got.Items) != 2 || got.Items[0].URL != xianyuTestImage1 {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
}

func TestXianyuPublicAccess(t *testing.T) {
	if os.Getenv("TULAI_XIANYU_LIVE") != "1" {
		t.Skip("set TULAI_XIANYU_LIVE=1 to check a public item")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "https://www.goofish.com/item?id=1070756032224", 1)
	if err == nil {
		t.Fatal("expected a partial-result notice")
	}
	t.Logf("public product: images=%d result=%v", len(got.Items), err)
}

func TestXianyuPublicCDN(t *testing.T) {
	if os.Getenv("TULAI_XIANYU_LIVE") != "1" {
		t.Skip("set TULAI_XIANYU_LIVE=1 to check a public CDN image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	image := "https://img.alicdn.com/bao/uploaded/i1/O1CN01tqGr9C1nQ480YQ6KQ_!!0-fleamarket.jpg"
	if _, ok := xianyuImageURL(image); !ok {
		t.Fatal("public image did not pass the CDN check")
	}
	dir := t.TempDir()
	item := Item{URL: image, RequireImage: true, PublicOnly: true, Filename: "sample.jpg"}
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("CDN download: entries=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Size() == 0 {
		t.Fatalf("CDN download: size=%v err=%v", info, err)
	}
	t.Logf("public CDN image downloaded: %d bytes", info.Size())
}
