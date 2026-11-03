package download

import (
	"context"
	"image"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFangAlbumBindsViewerToProperty(t *testing.T) {
	page := `<a id="xfptxq_B03_016" href="/loupan/123/photo/">album</a><div id="album-viewer"><img class="bigImg" src="//loupanw.soufunimg.com/viewimage/housepic/a/1/1400x1050.jpg?t=1"><img class="bigImg" src="//loupanw.soufunimg.com/viewimage/housepic/a/1/1400x1050.jpg?t=1"><img class="bigImg" src="https://evil.test/viewimage/housepic/a/2/x.jpg"></div><img class="bigImg" src="https://loupanw.soufunimg.com/viewimage/housepic/a/9/x.jpg">`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, 200, page), nil
	})
	result, err := Discover(context.Background(), getter, "https://xian.newhouse.fang.com/loupan/123/photo/", 1)
	if err == nil || len(result.Items) != 1 || result.Items[0].Metadata["fang_property_id"] != "123" || !strings.Contains(err.Error(), "部分图片") {
		t.Fatalf("found=%+v err=%v", result.Items, err)
	}
	page = strings.Replace(page, "/loupan/123/photo/", "/loupan/456/photo/", 1)
	result, err = Discover(context.Background(), getter, "https://xian.newhouse.fang.com/loupan/123/photo/", 1)
	if err == nil || len(result.Items) != 0 {
		t.Fatalf("accepted wrong property: %+v %v", result.Items, err)
	}
}

func TestFangAlbumRejectsUnsafeRoutes(t *testing.T) {
	for _, raw := range []string{"https://xian.newhouse.fang.com.evil.test/loupan/1/photo/", "https://xian.newhouse.fang.com:444/loupan/1/photo/", "https://xian.newhouse.fang.com/loupan/1/dongtai/"} {
		u, _ := url.Parse(raw)
		if isFangAlbum(u) {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestFangLive(t *testing.T) {
	if os.Getenv("TULAI_FANG_LIVE") != "1" {
		t.Skip("set TULAI_FANG_LIVE=1 for a public album")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	result, err := Discover(ctx, getter, "https://xian.newhouse.fang.com/loupan/3610207718/photo/list_903.htm", 1)
	if err != nil || len(result.Items) < 60 {
		t.Fatalf("%d images: %v", len(result.Items), err)
	}
	item := result.Items[0]
	item.Filename = "fang"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("downloaded files=%d", len(files))
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
	t.Logf("public album: %d images, first %dx%d %s", len(result.Items), img.Width, img.Height, format)
	for _, second := range result.Items {
		if !strings.Contains(second.URL, "imgwcs3.soufunimg.com") {
			continue
		}
		second.Filename = "second.png"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, second, nil); err != nil {
			t.Fatal(err)
		}
		files, _ := os.ReadDir(dir)
		if len(files) != 1 || filepath.Ext(files[0].Name()) != ".jpg" {
			t.Fatalf("expected JPEG served from PNG URL, got %v", files)
		}
		return
	}
	t.Fatal("public sample no longer contains its alternate image host")
}
