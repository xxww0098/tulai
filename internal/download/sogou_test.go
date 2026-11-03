package download

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sogouTestHTML(start, total int, images string) string {
	return fmt.Sprintf(`<title>花 - 搜狗图片</title><script>window.__INITIAL_STATE__={"route":{"query":{"query":"花","start":"%d"}},"searchList":{"query":"花","totalNum":%d,"xml_len":2,"searchList":[%s]}};</script>`, start, total, images)
}

func TestSogouSearchPagination(t *testing.T) {
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		calls = append(calls, u.Query().Get("start"))
		if u.Query().Get("query") != "花" {
			t.Fatalf("query lost: %s", raw)
		}
		if u.Query().Get("start") == "2" {
			return tiebaTestResponse(raw, http.StatusOK, sogouTestHTML(2, 4, `{"oriPicUrl":"https://images.example/second.jpg","url":"https://example.org/page"}`)), nil
		}
		return tiebaTestResponse(raw, http.StatusOK, sogouTestHTML(0, 4, `{"oriPicUrl":"https://images.example/first.jpg","url":"https://example.org/page"},{"oriPicUrl":"https://images.example/first.jpg"}`)), nil
	})
	got, err := Discover(context.Background(), getter, "https://pic.sogou.com/pics?query=%E8%8A%B1", 1)
	if err != nil || len(got.Items) != 2 || len(calls) != 2 || calls[1] != "2" || got.Items[0].URL != "https://images.example/first.jpg" {
		t.Fatalf("images=%+v calls=%v err=%v", got.Items, calls, err)
	}
}

func TestSogouSearchPartialAndBoundary(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Query().Get("start") != "" {
			return nil, errors.New("unavailable")
		}
		return tiebaTestResponse(raw, http.StatusOK, sogouTestHTML(0, 4, `{"oriPicUrl":"https://images.example/first.jpg"}`)), nil
	})
	got, err := Discover(context.Background(), getter, "https://pic.sogou.com/pics?query=%E8%8A%B1", 1)
	if err == nil || !strings.Contains(err.Error(), "不完整") || len(got.Items) != 1 {
		t.Fatalf("partial=%+v err=%v", got.Items, err)
	}
	for _, raw := range []string{"https://pic.sogou.com.evil.test/pics?query=a", "https://user@pic.sogou.com/pics?query=a", "https://pic.sogou.com:444/pics?query=a"} {
		u, _ := url.Parse(raw)
		if isSogouImageSearch(u) {
			t.Fatalf("unexpected search URL %s", raw)
		}
	}
}

func TestSogouLiveSearch(t *testing.T) {
	if os.Getenv("TULAI_SOGOU_LIVE") != "1" {
		t.Skip("set TULAI_SOGOU_LIVE=1 to verify public search pages")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	network := NewHTTPGetter("")
	pages := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Query().Get("start") == "96" {
			return nil, errors.New("test stops after two pages")
		}
		pages++
		return network.Get(ctx, raw, prepare)
	})
	got, err := Discover(ctx, getter, "https://pic.sogou.com/pics?query=cat", 1)
	if err == nil || pages != 2 || len(got.Items) < 50 {
		t.Fatalf("pages=%d images=%d err=%v", pages, len(got.Items), err)
	}
	for i, item := range got.Items {
		if i == 10 {
			break
		}
		dir := t.TempDir()
		item.Filename = "search"
		if err := New(Options{Getter: network}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			if i == 0 {
				t.Logf("first image download failed: %v", err)
			}
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			continue
		}
		f, err := os.Open(filepath.Join(dir, entries[0].Name()))
		if err != nil {
			continue
		}
		img, format, decodeErr := image.DecodeConfig(f)
		f.Close()
		if decodeErr == nil && img.Width > 0 && img.Height > 0 {
			t.Logf("public search: %d pages, %d images; one image %dx%d %s", pages, len(got.Items), img.Width, img.Height, format)
			return
		}
	}
	t.Fatalf("%d search results, but none of first ten images downloaded and decoded", len(got.Items))
}
