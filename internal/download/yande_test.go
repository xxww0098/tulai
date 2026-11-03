package download

import (
	"context"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const yandeTestMD5 = "0123456789abcdef0123456789abcdef"

func yandeTestPost(id int, host string) string {
	return fmt.Sprintf(`{"id":%d,"md5":"%s","status":"active","file_url":"https://%s/image/%s/yande.re%%20%d.jpg"}`, id, yandeTestMD5, host, yandeTestMD5, id)
}

func TestYandePageAndImageValidation(t *testing.T) {
	for _, raw := range []string{"https://yande.re/post?tags=cat", "https://yande.re/post/show/123"} {
		u, _ := url.Parse(raw)
		if !isYandePage(u) {
			t.Fatalf("expected valid page %s", raw)
		}
	}
	for _, raw := range []string{"https://yande.re.evil.test/post", "https://yande.re:444/post", "https://yande.re/post/show/9999999999999999999", "https://yande.re/user/123"} {
		u, _ := url.Parse(raw)
		if isYandePage(u) {
			t.Fatalf("unexpected valid page %s", raw)
		}
	}
	post := yandePost{ID: 1, MD5: yandeTestMD5, Status: "active", FileURL: "https://files.yande.re/image/" + yandeTestMD5 + "/test.jpg"}
	if yandeImage(post) == "" {
		t.Fatal("rejected valid file")
	}
	post.FileURL = "https://files.yande.re.evil.test/image/" + yandeTestMD5 + "/test.jpg"
	if yandeImage(post) != "" {
		t.Fatal("accepted foreign host")
	}
	post.FileURL = "https://files.yande.re/image/ffffffffffffffffffffffffffffffff/test.jpg"
	if yandeImage(post) != "" {
		t.Fatal("accepted mismatched image digest")
	}
}

func TestYandeSingleBindsPostID(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<script>Post.register_resp({"posts":[` + yandeTestPost(123, "files.yande.re") + `,` + yandeTestPost(456, "files.yande.re") + `]});</script>`
		return tiebaTestResponse(raw, 200, body), nil
	})
	result, err := Discover(context.Background(), getter, "https://yande.re/post/show/123", 1)
	if err != nil || len(result.Items) != 1 || result.Items[0].Metadata["yande_post_id"] != "123" {
		t.Fatalf("single post = %+v, %v", result.Items, err)
	}
}

func TestYandeListPaginationKeepsFilterAndExcludesRecommendations(t *testing.T) {
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		if calls == 1 {
			body := `<a class="thumb" href="/post/show/1"></a><script>Post.register(` + yandeTestPost(1, "files.yande.re") + `);Post.register(` + yandeTestPost(999, "files.yande.re") + `);</script><a rel="next" href="/post?page=2&amp;tags=cat">2</a>`
			return tiebaTestResponse(raw, 200, body), nil
		}
		if raw != "https://yande.re/post?page=2&tags=cat" {
			t.Fatalf("unexpected next page %s", raw)
		}
		body := `<a class="thumb" href="/post/show/2"></a><script>Post.register(` + yandeTestPost(2, "files.yande.re") + `);</script>`
		return tiebaTestResponse(raw, 200, body), nil
	})
	result, err := Discover(context.Background(), getter, "https://yande.re/post?tags=cat", 1)
	if err != nil || calls != 2 || len(result.Items) != 2 || result.Items[1].Metadata["yande_post_id"] != "2" {
		t.Fatalf("pages=%d result=%+v err=%v", calls, result.Items, err)
	}
}

func TestYandeRejectsAlteredNextPage(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<a class="thumb" href="/post/show/1"></a><script>Post.register(` + yandeTestPost(1, "files.yande.re") + `);</script><a rel="next" href="/post?page=2&amp;tags=other">2</a>`
		return tiebaTestResponse(raw, 200, body), nil
	})
	result, err := Discover(context.Background(), getter, "https://yande.re/post?tags=cat", 1)
	if err == nil || len(result.Items) != 1 || !strings.Contains(err.Error(), "筛选") {
		t.Fatalf("result=%+v err=%v", result.Items, err)
	}
}

func TestYandeLive(t *testing.T) {
	if os.Getenv("TULAI_YANDE_LIVE") != "1" {
		t.Skip("set TULAI_YANDE_LIVE=1 for the public site")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	for _, raw := range []string{"https://yande.re/post/show/1271873", "https://yande.re/post?tags=id%3A1271873"} {
		result, err := Discover(ctx, getter, raw, 1)
		if err != nil || len(result.Items) != 1 || result.Items[0].Metadata["yande_post_id"] != "1271873" {
			t.Fatalf("%s: %d images: %v", raw, len(result.Items), err)
		}
		item := result.Items[0]
		item.Filename = "yande"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatal(err)
		}
		files, _ := os.ReadDir(dir)
		if len(files) != 1 {
			t.Fatalf("downloaded files: %d", len(files))
		}
		f, err := os.Open(filepath.Join(dir, files[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		img, format, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || img.Width < 1 || img.Height < 1 {
			t.Fatalf("image: %v", err)
		}
		t.Logf("%s: %dx%d %s", raw, img.Width, img.Height, format)
	}
}

func TestYandeLivePagination(t *testing.T) {
	if os.Getenv("TULAI_YANDE_LIVE") != "1" {
		t.Skip("set TULAI_YANDE_LIVE=1 for the public site")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	network := NewHTTPGetter("")
	pages := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Query().Get("page") == "3" {
			return nil, errors.New("test stops after two pages")
		}
		pages++
		return network.Get(ctx, raw, prepare)
	})
	result, err := Discover(ctx, getter, "https://yande.re/post?tags=rating%3Asafe", 1)
	if err == nil || pages != 2 || len(result.Items) < 60 {
		t.Fatalf("pages=%d images=%d err=%v", pages, len(result.Items), err)
	}
	t.Logf("two public search pages: %d images", len(result.Items))
}

func TestYandeRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/post/show/123" {
			_, _ = io.WriteString(w, `<script>Post.register_resp({"posts":[`+yandeTestPost(123, "files.yande.re")+`]});</script>`)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("http://yande.re/post/show/123")
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String())); err != nil {
		t.Fatal(err)
	}
	result, err := session.Capture(ctx)
	if err != nil || len(result.Items) != 1 || result.Items[0].Metadata["yande_post_id"] != "123" {
		t.Fatalf("captured=%+v err=%v", result.Items, err)
	}
	if err := session.run(ctx, camoufox.Navigate("http://yande.re/login")); err != nil {
		t.Fatal(err)
	}
	result, err = session.Capture(ctx)
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("login captured=%+v err=%v", result.Items, err)
	}
}
