package download

import (
	"context"
	"image"
	_ "image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJJsHouseProduct(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<meta property="og:url" content="https://www.jjshouse.com/au/product/sample-dress-g423570"><div class="base-image goods-gallery-thumbs__img"><img data-src="https://cdn-4.jjshouse.com/upimg/l/ca/e6/image.jpg"></div><img data-src="https://cdn-4.jjshouse.com/upimg/l/recommended.jpg">`
		return tiebaTestResponse(raw, http.StatusOK, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.jjshouse.com/au/product/sample-dress-g423570", 1)
	if err == nil || !strings.Contains(err.Error(), "颜色") || len(got.Items) != 1 || got.Items[0].URL != "https://cdn-4.jjshouse.com/upimg/jjshouse/s1140/ca/e6/image.jpg" || len(got.Items[0].AlternateURLs) != 1 {
		t.Fatalf("images=%+v err=%v", got.Items, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, http.StatusOK, `<meta property="og:url" content="https://www.jjshouse.com/au/product/wrong-g423571"><div class="base-image goods-gallery-thumbs__img"><img data-src="https://cdn-4.jjshouse.com/upimg/l/ca/e6/image.jpg"></div>`), nil
	})
	got, err = Discover(context.Background(), getter, "https://www.jjshouse.com/au/product/sample-dress-g423570", 1)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "不匹配") {
		t.Fatalf("mismatch=%+v err=%v", got.Items, err)
	}
}

func TestJJsHouseLiveCDN(t *testing.T) {
	if os.Getenv("TULAI_JJSHOUSE_CDN") != "1" {
		t.Skip("set TULAI_JJSHOUSE_CDN=1 to verify a public image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := crawlURL(nil, "https://www.jjshouse.com/au/product/sample-g423570")
	imageURL, alternate := jjshouseImage(page, "https://cdn-4.jjshouse.com/upimg/l/ca/e6/b7e2c859b4926fef3e805d6dd7c6cae6.jpg")
	if imageURL == "" || alternate == "" {
		t.Fatal("known image was rejected")
	}
	dir := t.TempDir()
	item := Item{URL: imageURL, AlternateURLs: []string{alternate}, Filename: "product", RequireImage: true}
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
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
	if err != nil || img.Width < 1000 || img.Height == 0 {
		t.Fatalf("image=%+v format=%q err=%v", img, format, err)
	}
	t.Logf("public CDN image: %dx%d %s", img.Width, img.Height, format)
}
