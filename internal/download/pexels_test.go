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

func TestPexelsPhotoSelectsMatchingOriginal(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := `<link rel="canonical" href="https://www.pexels.com/photo/example-123/"><meta property="og:image" content="https://images.pexels.com/photos/123/pexels-photo-123.jpeg?auto=compress&amp;w=1200&amp;h=800"><meta property="og:image" content="https://images.pexels.com/photos/999/other.jpg">`
		return tiebaTestResponse(raw, 200, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.pexels.com/photo/example-123/", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://images.pexels.com/photos/123/pexels-photo-123.jpeg" || len(got.Items[0].AlternateURLs) != 1 {
		t.Fatalf("images=%+v err=%v", got.Items, err)
	}
}

func TestPexelsOfficialDownloadAndIdentity(t *testing.T) {
	body := `<meta property="og:image" content="https://images.pexels.com/photos/123/x.jpeg?w=800"><a href="https://images.pexels.com/photos/123/x.jpeg?dl=photo.jpg&amp;fm=jpg">Download</a>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, 200, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.pexels.com/photo/example-123/", 1)
	if err != nil || len(got.Items) != 1 || !strings.Contains(got.Items[0].URL, "dl=photo.jpg") {
		t.Fatalf("images=%+v err=%v", got.Items, err)
	}
	body += `<link rel="canonical" href="https://www.pexels.com/photo/other-456/">`
	got, err = Discover(context.Background(), getter, "https://www.pexels.com/photo/example-123/", 1)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("accepted wrong photo: %+v %v", got.Items, err)
	}
	for _, raw := range []string{"https://images.pexels.com.evil.test/photos/123/x.jpeg", "https://images.pexels.com/photos/999/x.jpeg", "https://images.pexels.com:444/photos/123/x.jpeg"} {
		if _, ok := pexelsImage(raw, "123"); ok {
			t.Fatalf("accepted %s", raw)
		}
	}
	bad, _ := url.Parse("https://www.pexels.com.evil.test/photo/example-123/")
	if isPexelsPhoto(bad) {
		t.Fatal("accepted foreign host")
	}
}

func TestPexelsObservedPublicImage(t *testing.T) {
	if os.Getenv("TULAI_PEXELS_LIVE") != "1" {
		t.Skip("set TULAI_PEXELS_LIVE=1 to test the observed public download")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	item := Item{URL: "https://images.pexels.com/photos/1465886/pexels-photo-1465886.jpeg?cs=srgb&dl=pexels-andiravsanjani-1465886.jpg&fm=jpg", Filename: "pexels.jpg", RequireImage: true, PublicOnly: true}
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
	t.Logf("public official download: %dx%d %s", img.Width, img.Height, format)
}
