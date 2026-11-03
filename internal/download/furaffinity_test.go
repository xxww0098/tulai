package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func faTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func faTestDoc(t *testing.T, raw string) *html.Node {
	t.Helper()
	d, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func faTestPhoto(id, author string) string {
	asset := "//d.furaffinity.net/art/" + author + "/123/" + id + ".png"
	return `<html><head><meta property="og:url" content="https://www.furaffinity.net/view/` + id + `/"></head><body><div class="submission-content">` +
		`<div class="submission-image"><img id="submissionImg" class="blocked-content" data-fullview-src="` + asset + `" src="` + asset + `"></div>` +
		`<div id="submission-options"><a href="` + asset + `">Download</a></div></div>` +
		`<div class="submission-description-artist"><a href="/user/` + author + `/">avatar</a><div class="submission-title"><h2>Title</h2></div><a href="/user/` + author + `/">artist</a></div>` +
		`<div id="minigallery"><img src="https://d.furaffinity.net/art/else/1/wrong.png"></div></body></html>`
}
func faTestGallery(author string, page int, ids []string, next bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<html><head><meta property="og:url" content="https://www.furaffinity.net/gallery/%s/"></head><body><div class="gallery-navigation"><div class="navigation-page-name">Page #%d</div>`, author, page)
	if next {
		fmt.Fprintf(&b, `<form method="get" action="/gallery/%s/%d/"><button>Next</button></form>`, author, page+1)
	}
	b.WriteString(`</div><section id="gallery-gallery">`)
	for _, id := range ids {
		fmt.Fprintf(&b, `<figure id="sid-%s" class="r-general t-image"><a href="/view/%s/"><img data-tags="u_%s c_digital"></a></figure>`, id, id, author)
	}
	b.WriteString(`</section></body></html>`)
	return b.String()
}

type faTestGetter struct {
	pages  map[string]string
	finals map[string]string
	calls  []string
	cancel context.CancelFunc
}

func (g *faTestGetter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.calls = append(g.calls, raw)
	if g.cancel != nil {
		g.cancel()
		return nil, ctx.Err()
	}
	body, ok := g.pages[raw]
	if !ok {
		return nil, errors.New("synthetic unavailable")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if prepare != nil {
		prepare(req)
	}
	if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
		return nil, errors.New("unexpected credentials")
	}
	final := raw
	if g.finals[raw] != "" {
		final = g.finals[raw]
	}
	return &Response{StatusCode: 200, FinalURL: final, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func TestFuraffinityRoutesAndAssets(t *testing.T) {
	for _, tc := range []struct{ raw, kind string }{
		{"https://www.furaffinity.net/view/123/", "photo"},
		{"https://www.furaffinity.net/gallery/anna/", "gallery"},
		{"https://www.furaffinity.net/gallery/anna/2/", "gallery"},
		{"https://www.furaffinity.net/user/anna/", "user"},
		{"https://www.furaffinity.net/gallery/anna/0/", ""},
		{"http://www.furaffinity.net/view/123/", "photo"},
		{"http://www.furaffinity.net:80/gallery/anna/", "gallery"},
		{"http://www.furaffinity.net:443/view/123/", ""},
		{"https://bad@www.furaffinity.net/view/123/", ""},
		{"https://www.furaffinity.net:444/view/123/", ""},
		{"https://www.furaffinity.net.evil/view/123/", ""},
		{"https://www.furaffinity.net/view%2f123/", ""},
	} {
		if got := faPageRef(faTestURL(t, tc.raw)).kind; got != tc.kind {
			t.Errorf("%s => %q", tc.raw, got)
		}
	}
	page := faTestURL(t, "https://www.furaffinity.net/view/123/")
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"//d.furaffinity.net/art/anna/123/123.png", true},
		{"https://d.furaffinity.net/art/anna/123/123.jpg?key=original", true},
		{"https://d.furaffinity.net/art/bob/123/123.png", false},
		{"https://d.furaffinity.net.evil/art/anna/123/123.png", false},
		{"https://x@d.furaffinity.net/art/anna/123/123.png", false},
		{"https://d.furaffinity.net:444/art/anna/123/123.png", false},
		{"https://d.furaffinity.net/art/anna/%2e%2e/123.png", false},
		{"https://d.furaffinity.net/art/anna/123%2f123.png", false},
		{"https://d.furaffinity.net/art/anna/123/123.mp4", false},
	} {
		_, got := faAsset(page, tc.raw, "anna")
		if got != tc.ok {
			t.Errorf("asset %q accepted=%v", tc.raw, got)
		}
	}
}

func TestFuraffinityLegacyHTTPUpgrade(t *testing.T) {
	getter := &faTestGetter{pages: map[string]string{
		"https://www.furaffinity.net/view/123/?source=legacy": faTestPhoto("123", "anna"),
	}}
	page := faTestURL(t, "http://www.furaffinity.net:80/view/123/?source=legacy")
	got, err := discoverFuraffinity(context.Background(), getter, page)
	if err != nil || len(got.Items) != 1 || len(getter.calls) != 1 || getter.calls[0] != "https://www.furaffinity.net/view/123/?source=legacy" {
		t.Fatalf("legacy=%+v calls=%v err=%v", got, getter.calls, err)
	}
}
func TestFuraffinityPhotoVerification(t *testing.T) {
	page := faTestURL(t, "https://www.furaffinity.net/view/123/")
	fixture := faTestPhoto("123", "anna")
	got, err := furaffinityPhotoPage(context.Background(), faTestDoc(t, fixture), page)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://d.furaffinity.net/art/anna/123/123.png" || got.Items[0].Metadata["furaffinity_author"] != "anna" || !got.Items[0].PublicOnly || !got.Items[0].RequireImage {
		t.Fatalf("photo=%+v err=%v", got, err)
	}
	for _, invalid := range []string{
		faTestPhoto("124", "anna"),
		strings.Replace(fixture, "/user/anna/", "/user/bob/", 1),
		strings.Replace(fixture, `>Download</a>`, `>View</a>`, 1),
		strings.Replace(fixture, `id="submissionImg"`, `id="otherImg"`, 1),
		strings.Replace(fixture, `href="//d.furaffinity.net/art/anna/123/123.png"`, `href="//d.furaffinity.net/art/anna/123/other.png"`, 1),
		strings.ReplaceAll(fixture, "d.furaffinity.net/art/anna/", "evil.test/art/anna/"),
		`<html><title>Sign in</title><a href="/login/">Login</a></html>`,
	} {
		got, err := furaffinityPhotoPage(context.Background(), faTestDoc(t, invalid), page)
		if err == nil || len(got.Items) != 0 {
			t.Errorf("invalid accepted: %+v %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := furaffinityPhotoPage(ctx, faTestDoc(t, fixture), page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
func TestFuraffinityGalleryPaginationAndPartial(t *testing.T) {
	base := "https://www.furaffinity.net"
	page := faTestURL(t, base+"/gallery/anna/")
	first := faTestGallery("anna", 1, []string{"1", "2"}, true)
	getter := &faTestGetter{pages: map[string]string{
		base + "/view/1/":         faTestPhoto("1", "anna"),
		base + "/view/2/":         faTestPhoto("2", "bob"),
		base + "/gallery/anna/2/": faTestGallery("anna", 2, []string{"3"}, false),
		base + "/view/3/":         faTestPhoto("3", "anna"),
	}}
	got, err := furaffinityCollect(context.Background(), getter, page, faTestDoc(t, first))
	if err == nil || !strings.Contains(err.Error(), "不完整") || len(got.Items) != 2 || got.Items[0].Metadata["furaffinity_submission_id"] != "1" || got.Items[1].Metadata["furaffinity_submission_id"] != "3" {
		t.Fatalf("gallery=%+v err=%v", got, err)
	}
	if len(getter.calls) != 4 {
		t.Fatalf("calls=%v", getter.calls)
	}
	getter = &faTestGetter{pages: map[string]string{base + "/view/1/": faTestPhoto("1", "anna")}}
	redirect := strings.Replace(first, `/gallery/anna/2/`, `https://attacker.test/gallery/anna/2/`, 1)
	got, err = furaffinityCollect(context.Background(), getter, page, faTestDoc(t, redirect))
	if err == nil || len(got.Items) != 1 || len(getter.calls) != 2 {
		t.Fatalf("unsafe next=%+v calls=%v err=%v", got, getter.calls, err)
	}
	getter = &faTestGetter{pages: map[string]string{base + "/view/1/": faTestPhoto("1", "anna")}, finals: map[string]string{base + "/view/1/": base + "/view/9/"}}
	got, err = furaffinityCollect(context.Background(), getter, page, faTestDoc(t, faTestGallery("anna", 1, []string{"1"}, false)))
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	getter = &faTestGetter{cancel: cancel}
	if _, err := furaffinityCollect(ctx, getter, page, faTestDoc(t, first)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
func TestFuraffinityGalleryLimitAndUser(t *testing.T) {
	base := "https://www.furaffinity.net"
	ids := make([]string, faMaxWorks+1)
	getter := &faTestGetter{pages: map[string]string{}}
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
		getter.pages[base+"/view/"+ids[i]+"/"] = faTestPhoto(ids[i], "anna")
	}
	got, err := furaffinityCollect(context.Background(), getter, faTestURL(t, base+"/gallery/anna/"), faTestDoc(t, faTestGallery("anna", 1, ids, false)))
	if len(got.Items) != faMaxWorks || err == nil || !strings.Contains(err.Error(), "1000") || len(getter.calls) != faMaxWorks {
		t.Fatalf("limit=%d calls=%d err=%v", len(got.Items), len(getter.calls), err)
	}
	user := `<meta property="og:url" content="https://www.furaffinity.net/user/anna/"><a href="/gallery/anna/">Gallery</a>`
	getter = &faTestGetter{pages: map[string]string{base + "/gallery/anna/": faTestGallery("anna", 1, []string{"1"}, false), base + "/view/1/": faTestPhoto("1", "anna")}}
	got, err = furaffinityCollect(context.Background(), getter, faTestURL(t, base+"/user/anna/"), faTestDoc(t, user))
	if err != nil || len(got.Items) != 1 || len(getter.calls) != 2 {
		t.Fatalf("user=%+v calls=%v err=%v", got, getter.calls, err)
	}
}

func TestFuraffinityGalleryPageLimit(t *testing.T) {
	base := "https://www.furaffinity.net"
	getter := &faTestGetter{pages: map[string]string{}}
	for page := 2; page <= faMaxGalleryPages; page++ {
		getter.pages[fmt.Sprintf("%s/gallery/anna/%d/", base, page)] = faTestGallery("anna", page, nil, true)
	}
	got, err := furaffinityCollect(context.Background(), getter, faTestURL(t, base+"/gallery/anna/"), faTestDoc(t, faTestGallery("anna", 1, nil, true)))
	if err == nil || !strings.Contains(err.Error(), "100 页") || !strings.Contains(err.Error(), "不完整") || len(got.Items) != 0 || len(getter.calls) != faMaxGalleryPages-1 {
		t.Fatalf("page limit items=%d calls=%d err=%v", len(got.Items), len(getter.calls), err)
	}
}
func TestFuraffinityPublicFixtures(t *testing.T) {
	dir := os.Getenv("TULAI_FA_FIXTURES")
	if dir == "" {
		t.Skip("set TULAI_FA_FIXTURES for publicly retrieved fixtures")
	}
	for _, tc := range []struct{ file, id string }{{"photo.html", "41844092"}, {"first.html", "55812901"}} {
		b, err := os.ReadFile(filepath.Join(dir, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		got, err := furaffinityPhotoPage(context.Background(), faTestDoc(t, string(b)), faTestURL(t, "https://www.furaffinity.net/view/"+tc.id+"/"))
		if err != nil || len(got.Items) != 1 {
			t.Fatalf("%s count=%d err=%v", tc.file, len(got.Items), err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		err = New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil)
		cancel()
		if err != nil {
			t.Fatalf("%s download: %v", tc.file, err)
		}
		t.Logf("%s parsed and downloaded 1 public image", tc.file)
	}
	for i, file := range []string{"gallery.html", "gallery2.html"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		url := "https://www.furaffinity.net/gallery/tsaoshin/"
		if i > 0 {
			url += "2/"
		}
		page := faTestURL(t, url)
		ref := faPageRef(page)
		doc := faTestDoc(t, string(b))
		ids, next, invalid := faGalleryItems(doc, page, ref)
		if !faCanonical(doc, page, ref) || invalid || len(ids) != 48 || next == nil {
			t.Fatalf("%s: ids=%d next=%v invalid=%v", file, len(ids), next, invalid)
		}
		t.Logf("%s: %d public cards, next page verified", file, len(ids))
	}
}

func TestFuraffinityPublicDiscover(t *testing.T) {
	if os.Getenv("TULAI_FA_LIVE") != "1" {
		t.Skip("set TULAI_FA_LIVE=1 for live public site validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "http://www.furaffinity.net/view/41844092/", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["furaffinity_submission_id"] != "41844092" {
		t.Fatalf("public Discover count=%d err=%v", len(got.Items), err)
	}
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("public image download failed: %v", err)
	}
	t.Log("public HTTP entry upgraded; one image discovered and downloaded")
}

func TestFuraffinityPublicRendered(t *testing.T) {
	if os.Getenv("TULAI_FA_BROWSER") != "1" {
		t.Skip("set TULAI_FA_BROWSER=1 for isolated public browser validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), "https://www.furaffinity.net/view/41844092/", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["furaffinity_submission_id"] != "41844092" {
		t.Fatalf("public browser Capture count=%d err=%v", len(got.Items), err)
	}
	if err := New(Options{Getter: s}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Logf("browser image transfer unavailable in this network: %v", err)
	} else {
		t.Log("public browser image download succeeded")
	}
}
