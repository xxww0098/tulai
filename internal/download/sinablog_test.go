package download

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSinaBlogArticle(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<h2 id="t_517646d10100ecb7">测试文章</h2><img real_src="https://s11.sinaimg.cn/bmiddle/avatar"><div id="sina_keyword_ad_area2"><img real_src="http://s11.sinaimg.cn/bmiddle/abc&amp;690"><img real_src="http://s11.sinaimg.cn/bmiddle/abc&amp;690"><img real_src="https://images.example/photo.png"><img real_src="javascript:alert(1)"></div>`
		return tiebaTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := discoverSinaBlogArticle(context.Background(), getter, mustSinaBlogURL(t, "https://blog.sina.com.cn/s/blog_517646d10100ecb7.html"))
	if err != nil || got.Title != "测试文章" || len(got.Items) != 2 {
		t.Fatalf("article = %+v, %v", got, err)
	}
	if got.Items[0].URL != "https://s11.sinaimg.cn/orignal/abc" || len(got.Items[0].AlternateURLs) != 1 || got.Items[0].AlternateURLs[0] != "https://s11.sinaimg.cn/bmiddle/abc&690" {
		t.Fatalf("original and fallback = %+v", got.Items[0])
	}
	if got.Items[1].URL != "https://images.example/photo.png" || !got.Items[1].RequireImage {
		t.Fatalf("embedded image = %+v", got.Items[1])
	}
}

func TestSinaBlogBoundaries(t *testing.T) {
	for _, raw := range []string{
		"https://blog.sina.com.cn.evil.test/s/blog_517646d10100ecb7.html",
		"https://user@blog.sina.com.cn/s/blog_517646d10100ecb7.html",
		"https://blog.sina.com.cn:444/s/blog_517646d10100ecb7.html",
	} {
		if isSinaBlogArticle(mustSinaBlogURL(t, raw)) {
			t.Fatalf("unexpected article: %s", raw)
		}
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse("https://blog.sina.com.cn/s/blog_517646d10100ecb8.html", http.StatusOK, `<div id="sina_keyword_ad_area2"></div>`), nil
	})
	_, err := discoverSinaBlogArticle(context.Background(), getter, mustSinaBlogURL(t, "https://blog.sina.com.cn/s/blog_517646d10100ecb7.html"))
	if err == nil || !strings.Contains(err.Error(), "其他文章") {
		t.Fatalf("redirect error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = discoverSinaBlogArticle(ctx, getter, mustSinaBlogURL(t, "https://blog.sina.com.cn/s/blog_517646d10100ecb7.html"))
	if err != context.Canceled {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestSinaBlogDirectoryPagination(t *testing.T) {
	var paths []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		paths = append(paths, u.Path)
		var body string
		switch u.Path {
		case "/s/articlelist_1366705873_0_1.html":
			body = `<div class="articleList"><span class="atc_title"><a href="/s/blog_517646d10100ecb7.html">first</a></span></div><li class="SG_pgnext"><a href="/s/articlelist_1366705873_0_2.html">下一页</a></li>`
		case "/s/articlelist_1366705873_0_2.html":
			body = `<div class="articleList"><span class="atc_title"><a href="/s/blog_517646d10102w0gb.html">second</a><a href="https://evil.test/s/blog_517646d10100ecb7.html">wrong</a></span></div>`
		case "/s/blog_517646d10100ecb7.html":
			body = `<h2 id="t_517646d10100ecb7">文章一</h2><div id="sina_keyword_ad_area2"><img real_src="https://s11.sinaimg.cn/bmiddle/shared"></div>`
		case "/s/blog_517646d10102w0gb.html":
			body = `<h2 id="t_517646d10102w0gb">文章二</h2><div id="sina_keyword_ad_area2"><img real_src="https://s11.sinaimg.cn/bmiddle/shared"></div>`
		default:
			t.Fatalf("unexpected directory request %s", raw)
		}
		return tiebaTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://blog.sina.com.cn/u/1366705873", 1)
	if err == nil || !strings.Contains(err.Error(), "不属于当前用户") || len(got.Items) != 2 || len(paths) != 4 {
		t.Fatalf("directory = %+v, paths=%v, err=%v", got.Items, paths, err)
	}
	if got.Items[0].Metadata["album_title"] != "文章一" || got.Items[1].Metadata["album_title"] != "文章二" || got.Items[0].Metadata["album_id"] == got.Items[1].Metadata["album_id"] {
		t.Fatalf("article grouping = %+v", got.Items)
	}
}

func mustSinaBlogURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSinaBlogLiveArticle(t *testing.T) {
	if os.Getenv("TULAI_SINABLOG_LIVE") != "1" {
		t.Skip("set TULAI_SINABLOG_LIVE=1 to verify a public article")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, "https://blog.sina.com.cn/s/blog_517646d10100ecb7.html", 1)
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("public article images=%d err=%v", len(got.Items), err)
	}
	dir := t.TempDir()
	item := got.Items[0]
	item.Filename = "article"
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
	f, err := os.Open(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, format, err := image.DecodeConfig(f)
	if err != nil || img.Width == 0 || img.Height == 0 {
		t.Fatalf("image=%+v format=%q err=%v", img, format, err)
	}
	t.Logf("public article: %d images, first %dx%d %s", len(got.Items), img.Width, img.Height, format)
}

func TestSinaBlogLiveDirectoryPages(t *testing.T) {
	if os.Getenv("TULAI_SINABLOG_LIVE") != "1" {
		t.Skip("set TULAI_SINABLOG_LIVE=1 to verify public directory pages")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	network := NewHTTPGetter("")
	var lists int
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		if ref := sinaBlogListID(u); ref.page > 0 {
			if ref.page > 2 {
				return nil, errors.New("test stops after two public directory pages")
			}
			lists++
			return network.Get(ctx, raw, prepare)
		}
		id := sinaBlogArticleID(u)
		if id == "" {
			return nil, errors.New("unexpected article URL")
		}
		body := fmt.Sprintf(`<h2 id="t_%s">article</h2><div id="sina_keyword_ad_area2"><img real_src="https://s11.sinaimg.cn/bmiddle/example"></div>`, id)
		return tiebaTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := Discover(ctx, getter, "https://blog.sina.com.cn/u/1366705873", 1)
	if err == nil || lists != 2 || len(got.Items) < 2 {
		t.Fatalf("public directory pages=%d articles=%d err=%v", lists, len(got.Items), err)
	}
	t.Logf("public directory: %d pages and %d article links; article bodies were synthetic", lists, len(got.Items))
}
