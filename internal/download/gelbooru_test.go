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

const gelbooruTestDigest = "1b04f5bcafb1d5a7e9fedf89c971f526"

func gelbooruTestPost(id string) string {
	return `<meta property="og:image" content="https://img4.gelbooru.com//images/1b/04/` + gelbooruTestDigest + `.jpeg"><section class="image-container note-container" data-id="` + id + `" data-md5="` + gelbooruTestDigest + `"></section>`
}

func TestGelbooruPostChecksIDAndDigest(t *testing.T) {
	body := gelbooruTestPost("123")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, 200, body), nil
	})
	got, err := Discover(context.Background(), getter, "https://gelbooru.com/index.php?page=post&s=view&id=123", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://img4.gelbooru.com/images/1b/04/"+gelbooruTestDigest+".jpeg" {
		t.Fatalf("images=%+v err=%v", got.Items, err)
	}
	body = gelbooruTestPost("999")
	got, err = Discover(context.Background(), getter, "https://gelbooru.com/index.php?page=post&s=view&id=123", 1)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("accepted wrong post: %+v %v", got.Items, err)
	}
	if gelbooruOriginal("https://img4.gelbooru.com.evil.test/images/1b/04/"+gelbooruTestDigest+".jpeg", gelbooruTestDigest) != "" {
		t.Fatal("accepted foreign image host")
	}
}

func TestGelbooruListPaginationAndOwnership(t *testing.T) {
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		kind, id := gelbooruRoute(u)
		if kind == "view" {
			return tiebaTestResponse(raw, 200, gelbooruTestPost(id)), nil
		}
		calls++
		if calls == 1 {
			return tiebaTestResponse(raw, 200, `<article class="thumbnail-preview"><a id="p1" href="/index.php?page=post&amp;s=view&amp;id=1"></a></article><a id="p9" href="/index.php?page=post&amp;s=view&amp;id=9"></a><div id="paginator"><a href="/index.php?page=post&amp;s=list&amp;tags=cat&amp;pid=42">2</a></div>`), nil
		}
		return tiebaTestResponse(raw, 200, `<article class="thumbnail-preview"><a id="p2" href="/index.php?page=post&amp;s=view&amp;id=2"></a></article>`), nil
	})
	got, err := Discover(context.Background(), getter, "https://gelbooru.com/index.php?page=post&s=list&tags=cat", 1)
	if err != nil || calls != 2 || len(got.Items) != 2 || got.Items[1].Metadata["gelbooru_post_id"] != "2" {
		t.Fatalf("pages=%d images=%+v err=%v", calls, got.Items, err)
	}
}

func TestGelbooruRejectsForeignPage(t *testing.T) {
	for _, raw := range []string{"https://gelbooru.com.evil.test/index.php?page=post&s=view&id=1", "https://gelbooru.com:444/index.php?page=post&s=view&id=1", "https://gelbooru.com/index.php?page=post&s=view&id=1&id=2"} {
		u, _ := url.Parse(raw)
		if isGelbooruPage(u) {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestGelbooruLive(t *testing.T) {
	if os.Getenv("TULAI_GELBOORU_LIVE") != "1" {
		t.Skip("set TULAI_GELBOORU_LIVE=1 for a public search")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, "https://gelbooru.com/index.php?page=post&s=view&id=972129&tags=rating%3Asafe", 1)
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("single=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "gelbooru"
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
	cfg, format, err := image.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		t.Fatalf("image: %v", err)
	}
	list, err := Discover(ctx, getter, "https://gelbooru.com/index.php?page=post&s=list&tags=rating%3Asafe", 1)
	if err != nil || len(list.Items) == 0 {
		t.Fatalf("search=%d err=%v", len(list.Items), err)
	}
	for _, i := range list.Items {
		if !strings.Contains(i.URL, "gelbooru.com/images/") {
			t.Fatalf("unexpected image host for %s", i.Metadata["gelbooru_post_id"])
		}
	}
	t.Logf("public search: %d images; single %dx%d %s", len(list.Items), cfg.Width, cfg.Height, format)
}
