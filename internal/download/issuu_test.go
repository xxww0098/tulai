package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

const issuuTestPage = "https://issuu.com/sample/docs/book"
const issuuTestViewer = "https://issuu.com/rd4?p=1&d=book&u=sample"
const issuuTestPublication = "abcdef0123456789abcdef0123456789"
const issuuTestRevision = "250607033250"
const issuuTestSVG1 = "https://svg.issuu.com/250607033250-abcdef0123456789abcdef0123456789/page_1.svg"
const issuuTestSVG2 = "https://svg.issuu.com/250607033250-abcdef0123456789abcdef0123456789/page_2.svg"

func issuuFixture(gated any, access, user, name string, count int) string {
	dims := make([]any, count)
	for i := range dims {
		dims[i] = map[string]int{"width": 800, "height": 1000}
	}
	doc := map[string]any{"access": access, "isDocumentGated": gated, "path": map[string]string{"type": "user", "username": user, "documentName": name}, "fullPageImageDimensions": dims, "pageCount": count, "publicationId": issuuTestPublication, "revisionId": issuuTestRevision, "title": "Example"}
	root := []any{false, []any{"$", "$L2a", nil, map[string]any{"initialDocumentData": map[string]any{"document": doc}}}}
	v, _ := json.Marshal(root)
	wrapped, _ := json.Marshal([]any{1, "8:" + string(v)})
	return `<html><head><link rel="canonical" href="` + issuuTestPage + `"></head><body><script>self.__next_f.push(` + string(wrapped) + `)</script><iframe src="` + strings.ReplaceAll(issuuTestViewer, "&", "&amp;") + `"></iframe></body></html>`
}
func issuuTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func issuuParseFixture(t *testing.T, body string) (*url.URL, *html.Node) {
	t.Helper()
	page, _ := url.Parse(issuuTestPage)
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return page, doc
}

func TestIssuuURLBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{issuuTestPage, true}, {"https://issuu.com/sample/docs/book/5", true}, {"https://issuu.com/sample/docs/book?s=tracking", true},
		{"https://issuu.com/sample/docs/book/0", false}, {"https://issuu.com/sample/docs/book/1/2", false}, {"https://issuu.com/sample/docs/book/s/123", false},
		{"https://issuu.com.evil.test/sample/docs/book", false}, {"https://user@issuu.com/sample/docs/book", false}, {"https://issuu.com:444/sample/docs/book", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isIssuuDocument(u); got != tc.want {
			t.Errorf("isIssuuDocument(%q)=%v", tc.raw, got)
		}
	}
	page, doc := issuuParseFixture(t, issuuFixture(false, "PUBLIC", "sample", "book", 2))
	state, err := issuuState(context.Background(), page, doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"http://svg.issuu.com/" + issuuTestRevision + "-" + issuuTestPublication + "/page_1.svg",
		strings.Replace(issuuTestSVG1, "svg.issuu.com", "svg.issuu.com.evil.test", 1),
		strings.Replace(issuuTestSVG1, "svg.issuu.com", "u@svg.issuu.com", 1),
		strings.Replace(issuuTestSVG1, "svg.issuu.com", "svg.issuu.com:444", 1),
		strings.Replace(issuuTestSVG1, issuuTestPublication, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 1),
		strings.Replace(issuuTestSVG1, "page_1.svg", "page_3.svg", 1),
		issuuTestSVG1 + "?token=invalid", issuuTestSVG1 + "#fragment",
	} {
		if _, _, ok := issuuSVG(raw, state); ok {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestIssuuPublicAndPartial(t *testing.T) {
	body := strings.Replace(issuuFixture(false, "PUBLIC", "sample", "book", 3), "Example", "Example  title", 1)
	page, doc := issuuParseFixture(t, body)
	state, err := issuuState(context.Background(), page, doc)
	if err != nil {
		t.Fatal(err)
	}
	if state.Title != "Example  title" {
		t.Fatalf("title whitespace=%q", state.Title)
	}
	got, err := issuuItems(context.Background(), page, state, []string{issuuTestSVG2, issuuTestSVG1, issuuTestSVG1})
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != issuuTestSVG1 || got.Items[1].Metadata["issuu_page"] != "2" {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://issuu.com/" {
			t.Fatalf("item=%+v", item)
		}
	}
	got, err = issuuCollect(context.Background(), page, doc)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("static items=%+v err=%v", got.Items, err)
	}
}

func TestIssuuRestrictionsAndIdentity(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"gated", issuuFixture(true, "PUBLIC", "sample", "book", 2)},
		{"private", issuuFixture(false, "PRIVATE", "sample", "book", 2)},
		{"unknown gate", issuuFixture(nil, "PUBLIC", "sample", "book", 2)},
		{"wrong author", issuuFixture(false, "PUBLIC", "other", "book", 2)},
		{"wrong document", issuuFixture(false, "PUBLIC", "sample", "other", 2)},
		{"wrong canonical", strings.Replace(issuuFixture(false, "PUBLIC", "sample", "book", 2), issuuTestPage, "https://issuu.com/other/docs/other", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, doc := issuuParseFixture(t, tc.body)
			if got, err := issuuCollect(context.Background(), page, doc); err == nil || len(got.Items) != 0 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestIssuuRedirectAndCancel(t *testing.T) {
	root, _ := url.Parse(issuuTestPage)
	for _, dest := range []string{"https://issuu.com/login", "https://issuu.com/other/docs/book", "https://evil.test/sample/docs/book"} {
		getter := GetterFunc(func(_ context.Context, _ string, _ func(*http.Request)) (*Response, error) {
			return issuuTestResponse(dest, issuuFixture(false, "PUBLIC", "sample", "book", 2)), nil
		})
		if got, err := discoverIssuu(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
			t.Fatalf("redirect %s items=%+v err=%v", dest, got.Items, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("cancelled request")
		return nil, nil
	})
	if _, err := discoverIssuu(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestIssuuRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == issuuTestPage {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(issuuFixture(false, "PUBLIC", "sample", "book", 124))))
			} else if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == issuuTestPage+"?gated=1" {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(issuuFixture(true, "PUBLIC", "sample", "book", 124))))
			} else if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == issuuTestViewer {
				body := `<html><head><link rel="preload" as="image" href="` + issuuTestSVG2 + `"></head><body><section class="page" style="background-image:url('` + issuuTestSVG1 + `')"></section><svg data-src="https://evil.test/page_3.svg"></svg></body></html>`
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
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
	start, _ := url.Parse(issuuTestPage)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(issuuTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != issuuTestSVG1 {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
	if err := session.run(ctx, camoufox.Navigate(issuuTestPage+"?gated=1")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "访问授权") || len(got.Items) != 0 {
		t.Fatalf("gated capture=%+v err=%v", got.Items, err)
	}
}

func TestIssuuPublicLive(t *testing.T) {
	if os.Getenv("TULAI_ISSUU_LIVE") != "1" {
		t.Skip("set TULAI_ISSUU_LIVE=1 for public document and isolated browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := "https://issuu.com/carlyheltzel/docs/magazine_design"
	getter := NewHTTPGetter("")
	static, err := Discover(ctx, getter, root, 1)
	if err == nil || len(static.Items) != 0 || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("static=%+v err=%v", static.Items, err)
	}
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), root, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.run(ctx, chromedp.Sleep(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 5 {
		t.Fatalf("public reader images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[len(got.Items)-1]
	item.Filename = "public-page"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("SVG download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Size() == 0 {
		t.Fatalf("info=%v err=%v", info, err)
	}
	t.Logf("public reader: %d exposed pages; downloaded %s (%d bytes)", len(got.Items), fmt.Sprint(item.Metadata["issuu_page"]), info.Size())
}
