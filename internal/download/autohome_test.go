package download

import (
	"context"
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

func TestAutohomePhotoTraversal(t *testing.T) {
	var requests []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		requests = append(requests, u.Path)
		var body string
		switch u.Path {
		case "/photo/series/14975/15/2.html":
			body = `<title>汽车相册</title><div id="main"><img id="img" src="//car0.autoimg.cn/a.jpg"></div><script>var __CarModel={"CarSeriesId":2949,"CarSpec":14975,"CarPicTypeId":15,"CarImgId":2}; var prevurl = '/photo/series/18651/15/1.html'; var nexturl = '/photo/series/14975/16/3.html';</script>`
		case "/photo/series/18651/15/1.html":
			body = `<div id="main"><img id="img" src="//car1.autoimg.cn/b.jpg"></div><script>var __CarModel={"CarSeriesId":2949,"CarSpec":18651,"CarPicTypeId":15,"CarImgId":1}; var nexturl = '/photo/series/14975/15/2.html'; var prevurl = 'https://evil.test/photo/series/14975/15/3.html';</script>`
		default:
			t.Fatalf("unexpected request %s", raw)
		}
		return tiebaTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://car.autohome.com.cn/photo/series/14975/15/2.html", 1)
	if err != nil || got.Title != "汽车相册" || len(got.Items) != 2 || len(requests) != 2 || got.Items[0].URL != "https://car0.autoimg.cn/a.jpg" {
		t.Fatalf("images=%+v requests=%v err=%v", got.Items, requests, err)
	}
}

func TestAutohomePhotoPartialAndBoundary(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if strings.HasSuffix(raw, "/2.html") {
			return tiebaTestResponse(raw, http.StatusOK, `<div id="main"><img id="img" src="//car0.autoimg.cn/a.jpg"></div><script>var __CarModel={"CarSeriesId":2949,"CarSpec":14975,"CarPicTypeId":15,"CarImgId":2}; var prevurl='/photo/series/14975/15/1.html';</script>`), nil
		}
		return tiebaTestResponse(raw, http.StatusForbidden, "blocked"), nil
	})
	got, err := Discover(context.Background(), getter, "https://car.autohome.com.cn/photo/series/14975/15/2.html", 1)
	if err == nil || len(got.Items) != 1 || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("partial=%+v err=%v", got.Items, err)
	}
	for _, raw := range []string{"https://car.autohome.com.cn.evil.test/photo/series/14975/15/2.html", "https://user@car.autohome.com.cn/photo/series/14975/15/2.html", "https://car.autohome.com.cn:444/photo/series/14975/15/2.html"} {
		u, _ := url.Parse(raw)
		if isAutohomePhoto(u) {
			t.Fatalf("unexpected photo URL %s", raw)
		}
	}
	mismatch := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, http.StatusOK, `<div id="main"><img id="img" src="//car0.autoimg.cn/wrong.jpg"></div><script>var __CarModel={"CarSeriesId":2949,"CarSpec":14975,"CarPicTypeId":15,"CarImgId":3};</script>`), nil
	})
	got, err = Discover(context.Background(), mismatch, "https://car.autohome.com.cn/photo/series/14975/15/2.html", 1)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("mismatched image=%+v err=%v", got.Items, err)
	}
}

func TestAutohomeLivePhoto(t *testing.T) {
	if os.Getenv("TULAI_AUTOHOME_LIVE") != "1" {
		t.Skip("set TULAI_AUTOHOME_LIVE=1 to verify a public photo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, "https://car.autohome.com.cn/photo/series/14975/15/1843242.html", 1)
	if err != nil || len(got.Items) < 2 {
		t.Fatalf("public gallery images=%d err=%v", len(got.Items), err)
	}
	dir := t.TempDir()
	item := got.Items[0]
	item.Filename = "car"
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
	t.Logf("public gallery: %d images, first %dx%d %s", len(got.Items), img.Width, img.Height, format)
}
