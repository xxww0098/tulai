package download

import (
	"context"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

func photography35Doc(t *testing.T, source string) *html.Node {
	t.Helper()
	d, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func photography35Page(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func photography35SingleFixture(id, author string, buy any) string {
	v := map[string]any{"photo_id": id, "user_id": 42, "user_login": author, "user_name": "Test author", "photo_name": "Test work", "link": "https://35photo.pro/" + author + "/photo_" + id + "/", "src": "https://35photo.pro/photos_main/1/" + id + ".jpg", "src-mobile": "https://35photo.pro/photos_temp/sizes/1/" + id + "_1500n.jpg"}
	if buy != nil {
		v["buyAllow"] = buy
	}
	b, _ := json.Marshal(v)
	return `<html><script>var photoData = ` + string(b) + `;</script><div id="zonePhotoInScroll"><img id="mainPhoto" class="mainPhoto" src="https://35photo.pro/photos_temp/sizes/1/` + id + `_1500n.jpg"></div></html>`
}
func photography35SeriesFixture() string {
	return `<html><script>photoData = {"photo_id":123,"user_id":42,"user_login":"anna","photo_name":"Series","link":"https://35photo.pro/anna/photo_123/","buyAllow":0,"series":[{"id":"901","src":"https://35photo.pro/photos_series/9/901.jpg","src-mobile":"https://35photo.pro/photos_series_prev/9/901_1000n.jpg"},{"id":"902","src":"https://35photo.pro/photos_series/9/902.jpg","src-mobile":"https://35photo.pro/photos_series_prev/9/902_1000n.jpg"}]};</script><div id="zonePhotoInScroll"><div class="rowSer"><img class="mainPhoto" id="mainPhoto" photo-ids="901" src="https://35photo.pro/photos_series_prev/9/901_1000n.jpg"></div><div class="rowSer"><img class="mainPhoto" photo-ids="902" data-src="https://35photo.pro/photos_series_prev/9/902_1000n.jpg"></div></div><img class="mainPhoto" photo-ids="999" data-src="https://35photo.pro/photos_series_prev/9/999_1000n.jpg"></html>`
}
func photography35CardHTML(author, id string) string {
	return `<div class="item" itemscope itemtype="https://schema.org/Photograph"><meta itemprop="name" content="Work ` + id + `"><div><a class="item openPhoto" photo-id="` + id + `" href="https://35photo.pro/` + author + `/photo_` + id + `/" href-large="https://35photo.pro/photos_main/1/` + id + `.jpg" href-mobile="https://35photo.pro/photos_temp/sizes/1/` + id + `_1500n.jpg"></a></div></div>`
}
func photography35AuthorFixture(author, user, cursor string, ids ...string) string {
	var items strings.Builder
	for _, id := range ids {
		items.WriteString(photography35CardHTML(author, id))
	}
	return `<html><head><link rel="canonical" href="https://35photo.pro/` + author + `/"><script type="application/ld+json">{"@type":"Person","name":"Test author","url":"https://35photo.pro/` + author + `/"}</script></head><body><div class="profileTape grid">` + items.String() + `</div><script>var nextListMoreOp = "&user_id=` + user + `";var showNextListId=` + cursor + `;</script></body></html>`
}

func TestPhotography35RoutesAndAssets(t *testing.T) {
	for _, test := range []struct{ raw, author, id string }{
		{"https://35photo.pro/photo_123/", "", "123"}, {"https://35photo.pro/anna/photo_123", "anna", "123"},
		{"https://35photo.pro.evil.test/photo_123", "", ""}, {"https://x@35photo.pro/photo_123", "", ""},
		{"https://35photo.pro:444/photo_123", "", ""}, {"https://35photo.pro/anna/photo_123/other", "", ""},
		{"http://35photo.pro/photo_123", "", ""}, {"https://35photo.pro/a%2fb/photo_123", "", ""},
	} {
		ref := photography35PhotoRef(photography35Page(t, test.raw))
		if ref.author != test.author || ref.photo != test.id {
			t.Errorf("route %q = %+v", test.raw, ref)
		}
	}
	if !isPhotography35Author(photography35Page(t, "https://35photo.pro/anna/")) || isPhotography35Author(photography35Page(t, "https://35photo.pro/rating/")) {
		t.Fatal("author route mismatch")
	}
	for _, test := range []struct {
		raw, kind, id string
		ok            bool
	}{
		{"https://35photo.pro/photos_main/1/123.jpg", "main", "123", true},
		{"https://35photo.pro/photos_series/9/901.jpg", "series", "901", true},
		{"https://35photo.pro/photos_temp/sizes/1/123_1500n.jpg", "display", "123", true},
		{"https://35photo.pro/photos_series_prev/9/901_1000n.jpg", "series-display", "901", true},
		{"https://35photo.pro/photos_main/1/124.jpg", "main", "123", false},
		{"https://35photo.pro.evil.test/photos_main/1/123.jpg", "main", "123", false},
		{"https://x@35photo.pro/photos_main/1/123.jpg", "main", "123", false},
		{"https://35photo.pro:444/photos_main/1/123.jpg", "main", "123", false},
		{"https://35photo.pro/photos_main/%2e%2e/123.jpg", "main", "123", false},
		{"https://35photo.pro/photos_main/1/123.jpg?token=x", "main", "123", false},
	} {
		_, got := photography35Asset(test.raw, test.kind, test.id)
		if got != test.ok {
			t.Errorf("asset %q accepted=%v", test.raw, got)
		}
	}
}

func TestPhotography35PhotoAndSeries(t *testing.T) {
	page := photography35Page(t, "https://35photo.pro/anna/photo_123/")
	got, err := photography35PhotoPage(context.Background(), photography35Doc(t, photography35SingleFixture("123", "anna", 0)), page)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://35photo.pro/photos_main/1/123.jpg" || got.Items[0].Metadata["35photo_author"] != "anna" {
		t.Fatalf("single=%+v err=%v", got, err)
	}
	got, err = photography35PhotoPage(context.Background(), photography35Doc(t, photography35SingleFixture("123", "anna", 1)), page)
	if err == nil || len(got.Items) != 1 || !strings.Contains(got.Items[0].URL, "_1500n.jpg") || !strings.Contains(err.Error(), "展示图") {
		t.Fatalf("restricted=%+v err=%v", got, err)
	}
	got, err = photography35PhotoPage(context.Background(), photography35Doc(t, photography35SeriesFixture()), page)
	if err != nil || len(got.Items) != 2 || got.Items[1].URL != "https://35photo.pro/photos_series/9/902.jpg" {
		t.Fatalf("series=%+v err=%v", got, err)
	}
	for _, src := range []string{strings.Replace(photography35SingleFixture("123", "anna", 0), "var photoData", "adultText = true; var photoData", 1), photography35SingleFixture("124", "anna", 0), photography35SingleFixture("123", "bob", 0), `<script>var x='photoData = {"photo_id":123}';</script>`, strings.Replace(photography35SingleFixture("123", "anna", 0), "photos_main/1/123.jpg", "photos_main/1/999.jpg", 1)} {
		got, err = photography35PhotoPage(context.Background(), photography35Doc(t, src), page)
		if err == nil || len(got.Items) != 0 {
			t.Errorf("invalid photo accepted: %+v err=%v", got, err)
		}
	}
	partial := strings.Replace(photography35SeriesFixture(), `photo-ids="902"`, `photo-ids="999"`, 1)
	got, err = photography35PhotoPage(context.Background(), photography35Doc(t, partial), page)
	if err == nil || len(got.Items) != 1 {
		t.Fatalf("series mismatch=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := photography35PhotoPage(ctx, photography35Doc(t, photography35SeriesFixture()), page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

type photography35Getter struct {
	pages    map[string]string
	statuses map[string]int
	finals   map[string]string
	calls    atomic.Int32
}

func (g *photography35Getter) Get(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.calls.Add(1)
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if prepare != nil {
		prepare(req)
	}
	if u.Path != "/show_block.php" || u.Query().Get("user_id") != "42" || u.Query().Get("page") != "photoUser" || u.Query().Get("type") != "getNextPageData" || req.Header.Get("Referer") != "https://35photo.pro/" {
		return nil, errors.New("unexpected request")
	}
	cursor := u.Query().Get("lastId")
	status := g.statuses[cursor]
	if status == 0 {
		status = 200
	}
	body, ok := g.pages[cursor]
	if !ok {
		return nil, errors.New("missing page")
	}
	final := raw
	if g.finals[cursor] != "" {
		final = g.finals[cursor]
	}
	return &Response{StatusCode: status, FinalURL: final, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}
func photography35PageJSON(last string, html string) string {
	b, _ := json.Marshal(map[string]any{"page": "photoUser", "lastId": last, "data": html})
	return string(b)
}

func TestPhotography35AuthorPagingAndPartial(t *testing.T) {
	page := photography35Page(t, "https://35photo.pro/anna/")
	doc := photography35Doc(t, photography35AuthorFixture("anna", "42", "100", "100"))
	getter := &photography35Getter{pages: map[string]string{"100": photography35PageJSON("98", photography35CardHTML("anna", "99")+photography35CardHTML("anna", "98")), "98": photography35PageJSON("0", "")}}
	got, err := photography35AuthorCollect(context.Background(), getter, page, doc)
	if err == nil || len(got.Items) != 3 || getter.calls.Load() != 2 || !strings.Contains(err.Error(), "系列作品") || got.Items[0].Metadata["title"] != "Work 100" {
		t.Fatalf("author items=%d calls=%d err=%v", len(got.Items), getter.calls.Load(), err)
	}
	for _, item := range got.Items {
		if !strings.Contains(item.URL, "/photos_temp/sizes/") {
			t.Fatalf("author selected undisplayed image: %s", item.URL)
		}
		if item.Metadata["35photo_author_id"] != "42" || !item.RequireImage || !item.PublicOnly {
			t.Fatalf("bad item %+v", item)
		}
	}
	for _, test := range []struct {
		name, body, final string
		status            int
	}{
		{"wrong author", photography35PageJSON("99", photography35CardHTML("bob", "99")), "", 200},
		{"wrong cursor", photography35PageJSON("97", photography35CardHTML("anna", "99")), "", 200},
		{"redirect", photography35PageJSON("0", ""), "https://evil.test/show_block.php", 200},
		{"server error", `{}`, "", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := &photography35Getter{pages: map[string]string{"100": test.body}, statuses: map[string]int{"100": test.status}, finals: map[string]string{"100": test.final}}
			found, err := photography35AuthorCollect(context.Background(), g, page, doc)
			want := 1
			if test.name == "wrong cursor" {
				want = 2
			}
			if err == nil || len(found.Items) != want {
				t.Fatalf("partial items=%d err=%v", len(found.Items), err)
			}
		})
	}
	bad := photography35Doc(t, photography35AuthorFixture("bob", "42", "100", "100"))
	if found, err := photography35AuthorCollect(context.Background(), getter, page, bad); err == nil || len(found.Items) != 0 {
		t.Fatalf("wrong owner=%+v err=%v", found, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := photography35AuthorCollect(ctx, getter, page, doc); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestPhotography35RenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	series := photography35SeriesFixture()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "35photo.pro" {
			http.Error(w, "bad host", 400)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/anna/photo_123/" {
			fmt.Fprint(w, series)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page := photography35Page(t, "https://35photo.pro/anna/photo_123/")
	s := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := s.run(ctx, camoufox.Navigate(page.String())); err != nil {
		t.Fatal(err)
	}
	got, err := s.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[1].Metadata["35photo_author"] != "anna" {
		t.Fatalf("Capture count=%d err=%v", len(got.Items), err)
	}
}

func TestPhotography35PublicFixtures(t *testing.T) {
	dir := os.Getenv("TULAI_35PHOTO_FIXTURES")
	if dir == "" {
		t.Skip("set TULAI_35PHOTO_FIXTURES to public HTML fixture directory")
	}
	for _, test := range []struct {
		filename, url string
		count         int
	}{{"photo.html", "https://35photo.pro/photo_14083604/", 1}, {"series.html", "https://35photo.pro/photo_14340131/", 9}} {
		b, err := os.ReadFile(filepath.Join(dir, test.filename))
		if err != nil {
			t.Fatal(err)
		}
		got, err := photography35PhotoPage(context.Background(), photography35Doc(t, string(b)), photography35Page(t, test.url))
		if err != nil || len(got.Items) != test.count {
			t.Fatalf("%s count=%d err=%v", test.filename, len(got.Items), err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		err = New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil)
		cancel()
		if err != nil {
			t.Fatalf("%s download failed: %T", test.filename, err)
		}
		t.Logf("%s: %d photos, first download succeeded", test.filename, len(got.Items))
	}
}

func TestPhotography35PublicAuthor(t *testing.T) {
	if os.Getenv("TULAI_35PHOTO_LIVE") != "1" {
		t.Skip("set TULAI_35PHOTO_LIVE=1 for public author validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "https://35photo.pro/dmin/", 1)
	if err == nil || len(got.Items) < 100 {
		t.Fatalf("public author count=%d err=%v", len(got.Items), err)
	}
	t.Logf("public author count=%d warning=%v", len(got.Items), err)
}

func TestPhotography35PublicRendered(t *testing.T) {
	if os.Getenv("TULAI_35PHOTO_LIVE") != "1" {
		t.Skip("set TULAI_35PHOTO_LIVE=1 for public browser validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	raw := "https://35photo.pro/photo_14083604/"
	s, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), raw, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.run(ctx, chromedp.Sleep(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, warning := s.Capture(ctx)
	if len(got.Items) != 1 || warning != nil {
		t.Fatalf("public Capture count=%d warning=%v", len(got.Items), warning)
	}
	t.Logf("public browser Capture: %d photo", len(got.Items))
	if err := New(Options{Getter: s}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("browser download failed: %T %v", err, err)
	}
	t.Log("public browser download succeeded")
}

func TestPhotography35PublicAuthorRendered(t *testing.T) {
	if os.Getenv("TULAI_35PHOTO_LIVE") != "1" {
		t.Skip("set TULAI_35PHOTO_LIVE=1 for public browser validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	s, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), "https://35photo.pro/dmin/", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.run(ctx, chromedp.Sleep(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, warning := s.Capture(ctx)
	if len(got.Items) < 100 || warning == nil {
		t.Fatalf("public author Capture count=%d warning=%v", len(got.Items), warning)
	}
	t.Logf("public browser author Capture: %d images warning=%v", len(got.Items), warning)
	if err := New(Options{Getter: s}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("author browser download failed: %T %v", err, err)
	}
	t.Log("public browser author image download succeeded")
}

func TestPhotography35AuthorImageLimit(t *testing.T) {
	page := photography35Page(t, "https://35photo.pro/anna/")
	ids := make([]string, photography35MaxImages+1)
	for i := range ids {
		ids[i] = fmt.Sprint(i + 1)
	}
	doc := photography35Doc(t, photography35AuthorFixture("anna", "42", "0", ids...))
	got, err := photography35AuthorCollect(context.Background(), &photography35Getter{}, page, doc)
	if err == nil || len(got.Items) != photography35MaxImages || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("limit count=%d err=%v", len(got.Items), err)
	}
}
