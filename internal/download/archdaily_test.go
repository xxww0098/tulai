package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"sync/atomic"
	"testing"
	"time"

	stdhtml "html"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

const archDailyPublicArticle = "https://www.archdaily.com/1014145/the-gallery-house-wim-goes-architectuur"
const archDailyPublicGallery = archDailyPublicArticle + "/65e771b4da34c7017c51cabe-the-gallery-house-wim-goes-architectuur-photo"
const archDailyPublicCN = "https://www.archdaily.cn/cn/1039069/bm-zhu-zhai-errantearchitetture"
const archDailyTestArticle = "https://www.archdaily.com/1014145/sample"
const archDailyTestGallery = archDailyTestArticle + "/aabbccddeeff001122334455-photo"
const archDailyMedia1 = "aabbccddeeff001122334455"
const archDailyMedia2 = "00112233445566778899aabb"
const archDailyPreview = "https://images.adsttc.com/media/images/aabb/ccdd/eeff/0011/2233/4455/newsletter/photo.jpg?1"
const archDailyLarge1 = "https://images.adsttc.com/media/images/aabb/ccdd/eeff/0011/2233/4455/large_jpg/photo.jpg?1"
const archDailyLarge2 = "https://images.adsttc.com/media/images/0011/2233/4455/6677/8899/aabb/large_jpg/plan.jpg?2"
const archDailyCategoryURL = "https://www.archdaily.com/search/projects/categories/houses"

func archDailyArticleFixture(article, gallery string) string {
	return `<html><head><title>Project | ArchDaily</title><link rel="canonical" href="` + article + `"><meta property="og:url" content="` + article + `"></head><body>
<img src="https://images.adsttc.com/media/images/aabb/ccdd/eeff/0011/2233/4455/newsletter/recommendation.jpg?1">
<article id="single-content"><img src="` + archDailyPreview + `" data-nr-picture-id="` + archDailyMedia1 + `" longdesc="` + gallery + `" alt="Project exterior"></article>
</body></html>`
}

func archDailyGalleryFixture(gallery string, entries []map[string]string) string {
	blob, _ := json.Marshal(entries)
	return `<html><head><title>Gallery | ArchDaily</title><link rel="canonical" href="` + gallery + `"><meta property="og:url" content="` + gallery + `"></head><body><div id="gallery-items" data-images="` + stdhtml.EscapeString(string(blob)) + `"></div></body></html>`
}

func archDailyTestEntries() []map[string]string {
	return []map[string]string{
		{"link": archDailyMedia1 + "-photo", "url_large": archDailyLarge1, "image_alt": "Exterior", "type": "MediaPicture"},
		{"link": archDailyMedia2 + "-plan", "url_large": archDailyLarge2, "image_alt": "Floor plan", "type": "MediaPlan"},
	}
}

func archDailyTestResponse(raw string, status int, contentType, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestArchDailyURLs(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{archDailyPublicArticle, true},
		{archDailyPublicGallery, true},
		{archDailyPublicCN, true},
		{"https://www.archdaily.com/zh/1039069/bm-zhu-zhai", true},
		{archDailyCategoryURL + "?page=2", true},
		{"https://www.archdaily.com/projects", false},
		{"https://www.archdaily.com/search/projects/categories/UPPER", false},
		{"https://www.archdaily.com/1014145/sample/not-a-media-id", false},
		{"https://www.archdaily.com.evil.test/1014145/sample", false},
		{"https://user@www.archdaily.com/1014145/sample", false},
		{"https://www.archdaily.com:8443/1014145/sample", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isArchDailyPage(u) != tc.ok {
			t.Errorf("isArchDailyPage(%q)=%v", tc.raw, isArchDailyPage(u))
		}
	}
	cn, _ := url.Parse(archDailyPublicCN)
	newURL, _ := url.Parse("https://www.archdaily.com/zh/1039069/bm-zhu-zhai-errantearchitetture")
	if archDailyPageRef(cn) != archDailyPageRef(newURL) {
		t.Fatal("Chinese legacy redirect did not retain article identity")
	}
}

func TestArchDailyImageBoundary(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(archDailyLarge1, "images.adsttc.com", "images.adsttc.com.evil.test", 1),
		strings.Replace(archDailyLarge1, "images.adsttc.com", "user@images.adsttc.com", 1),
		strings.Replace(archDailyLarge1, "images.adsttc.com", "images.adsttc.com:8443", 1),
		strings.Replace(archDailyLarge1, "https://", "http://", 1),
		strings.Replace(archDailyLarge1, "/aabb/", "/dead/", 1),
		strings.Replace(archDailyLarge1, "/large_jpg/", "/medium_jpg/", 1),
	} {
		if _, ok := archDailyImageURL(raw, archDailyMedia1, true); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if image, ok := archDailyImageURL(archDailyLarge1, archDailyMedia1, true); !ok || image != archDailyLarge1 {
		t.Fatalf("large image=%q ok=%v", image, ok)
	}
}

func TestArchDailyArticleAndGallery(t *testing.T) {
	root, _ := url.Parse(archDailyTestArticle + "?tracking=synthetic")
	var calls int
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		switch raw {
		case archDailyTestArticle + "?tracking=synthetic":
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyArticleFixture(archDailyTestArticle, archDailyTestGallery)), nil
		case archDailyTestGallery:
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyGalleryFixture(archDailyTestGallery, archDailyTestEntries())), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	got, err := discoverArchDaily(context.Background(), getter, root)
	if err != nil || got.Title != "Project | ArchDaily" || len(got.Items) != 2 || calls != 2 {
		t.Fatalf("images=%+v title=%q calls=%d err=%v", got.Items, got.Title, calls, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://www.archdaily.com/" || item.Metadata["archdaily_article_id"] != "1014145" {
			t.Fatalf("unexpected item %+v", item)
		}
	}
	if got.Items[0].URL != archDailyLarge1 || got.Items[1].URL != archDailyLarge2 {
		t.Fatalf("not large gallery images: %+v", got.Items)
	}
}

func TestArchDailyGalleryFailurePreservesPreview(t *testing.T) {
	root, _ := url.Parse(archDailyTestArticle)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == archDailyTestArticle {
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyArticleFixture(archDailyTestArticle, archDailyTestGallery)), nil
		}
		return archDailyTestResponse(raw, http.StatusServiceUnavailable, "text/html", ""), nil
	})
	got, err := discoverArchDaily(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != archDailyPreview {
		t.Fatalf("partial images=%+v err=%v", got.Items, err)
	}
}

func TestArchDailyGalleryIdentity(t *testing.T) {
	root, _ := url.Parse(archDailyTestGallery)
	for _, entries := range [][]map[string]string{
		{{"link": archDailyMedia2 + "-plan", "url_large": archDailyLarge2, "type": "MediaPlan"}},
		{{"link": archDailyMedia1 + "-photo", "url_large": strings.Replace(archDailyLarge1, "images.adsttc.com", "evil.test", 1), "type": "MediaPicture"}},
		{{"link": archDailyMedia1 + "-photo", "url_large": archDailyLarge2, "type": "MediaPicture"}},
	} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyGalleryFixture(archDailyTestGallery, entries)), nil
		})
		if got, err := discoverArchDaily(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
			t.Fatalf("invalid gallery images=%+v err=%v", got.Items, err)
		}
	}
}

func TestArchDailyCategoryPaginationAndFilters(t *testing.T) {
	root, _ := url.Parse(archDailyCategoryURL)
	api1 := "https://www.archdaily.com/search/api/v1/us/projects/categories/houses?page=1"
	api2 := "https://www.archdaily.com/search/api/v1/us/projects/categories/houses?page=2"
	article2 := "https://www.archdaily.com/1014146/sample"
	gallery2 := article2 + "/" + archDailyMedia1 + "-photo"
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		switch raw {
		case api1:
			return archDailyTestResponse(raw, http.StatusOK, "application/json", `{"total_docs":20,"results":[{"document_id":"1014145","url":"https://www.archdaily.com/1014145/sample?tracking=synthetic"}]}`), nil
		case api2:
			return archDailyTestResponse(raw, http.StatusOK, "application/json", `{"total_docs":20,"results":[{"document_id":"1014146","url":"https://www.archdaily.com/1014146/sample"}]}`), nil
		case archDailyTestArticle:
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyArticleFixture(archDailyTestArticle, archDailyTestGallery)), nil
		case archDailyTestGallery:
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyGalleryFixture(archDailyTestGallery, archDailyTestEntries())), nil
		case article2:
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyArticleFixture(article2, gallery2)), nil
		case gallery2:
			return archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyGalleryFixture(gallery2, archDailyTestEntries())), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	got, err := discoverArchDaily(context.Background(), getter, root)
	if err != nil || len(got.Items) != 2 || len(calls) != 6 {
		t.Fatalf("category images=%d calls=%+v err=%v", len(got.Items), calls, err)
	}
	filtered, _ := url.Parse(archDailyCategoryURL + "?year=2025")
	if got, err := discoverArchDaily(context.Background(), getter, filtered); err == nil || len(got.Items) != 0 {
		t.Fatalf("ignored filter images=%d err=%v", len(got.Items), err)
	}
}

func TestArchDailyCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(archDailyTestArticle)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverArchDaily(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := archDailyTestResponse(raw, http.StatusOK, "text/html", archDailyArticleFixture(archDailyTestArticle, archDailyTestGallery))
		resp.FinalURL = "https://www.archdaily.com/1014146/sample"
		return resp, nil
	})
	if got, err := discoverArchDaily(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type archDailyRoundTripper func(*http.Request) (*http.Response, error)

func (fn archDailyRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestArchDailyRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = archDailyTestArticle + "?tracking=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = archDailyRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != archDailyTestGallery {
			return nil, errors.New("unexpected gallery request")
		}
		body := archDailyGalleryFixture(archDailyTestGallery, archDailyTestEntries())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(archDailyArticleFixture(archDailyTestArticle, archDailyTestGallery))))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case responses <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(target)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || calls.Load() != 1 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestArchDailyLiveArticlesDownload(t *testing.T) {
	if os.Getenv("TULAI_ARCHDAILY_LIVE") != "1" {
		t.Skip("set TULAI_ARCHDAILY_LIVE=1 to verify public galleries")
	}
	for _, tc := range []struct {
		name, raw string
		minimum   int
	}{
		{"article", archDailyPublicArticle, 20},
		{"gallery", archDailyPublicGallery, 20},
		{"chinese", archDailyPublicCN, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			getter := NewHTTPGetter("")
			got, err := Discover(ctx, getter, tc.raw, 1)
			if err != nil || len(got.Items) < tc.minimum {
				t.Fatalf("public gallery images=%d err=%v", len(got.Items), err)
			}
			if tc.name != "gallery" {
				item := got.Items[0]
				item.Filename = "gallery"
				dir := t.TempDir()
				if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
					t.Fatalf("download: %v", err)
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
				im, format, err := image.DecodeConfig(f)
				if err != nil || im.Width == 0 || im.Height == 0 {
					t.Fatalf("image=%+v format=%q err=%v", im, format, err)
				}
				t.Logf("public gallery images=%d first=%dx%d %s", len(got.Items), im.Width, im.Height, format)
			} else {
				t.Logf("public gallery images=%d", len(got.Items))
			}
		})
	}
}

func TestArchDailyCategoryBoundaries(t *testing.T) {
	page, _ := url.Parse(archDailyCategoryURL + "?page=1")
	ref := archDailyCategoryID(page)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := fmt.Sprintf(`{"total_docs":1,"results":[{"document_id":"1014145","url":%q}]}`, "https://evil.test/1014145/sample")
		return archDailyTestResponse(raw, http.StatusOK, "application/json", body), nil
	})
	if articles, _, err := archDailyCategoryPage(context.Background(), getter, ref); err == nil || len(articles) != 0 {
		t.Fatalf("unsafe category articles=%+v err=%v", articles, err)
	}
}

// Two real category pages and one live article from each are followed; other
// articles and page three are deliberately bounded to keep this test small.
func TestArchDailyLiveTwoCategoryPages(t *testing.T) {
	if os.Getenv("TULAI_ARCHDAILY_LIVE") != "1" {
		t.Skip("set TULAI_ARCHDAILY_LIVE=1 to verify public category pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	pages := map[string]string{}
	chosen := map[string]bool{}
	for page := 1; page <= 2; page++ {
		endpoint := fmt.Sprintf("https://www.archdaily.com/search/api/v1/us/projects/categories/houses?page=%d", page)
		resp, err := getter.Get(ctx, endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
		resp.Body.Close()
		if err != nil || len(body) > maxPageBytes || resp.StatusCode != http.StatusOK || resp.FinalURL != endpoint {
			t.Fatalf("page %d status=%d bytes=%d err=%v", page, resp.StatusCode, len(body), err)
		}
		pages[endpoint] = string(body)
		var data struct {
			Total   int `json:"total_docs"`
			Results []struct {
				ID  string `json:"document_id"`
				URL string `json:"url"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &data); err != nil || len(data.Results) != 18 || data.Total < 36 {
			t.Fatalf("page %d results=%d total=%d err=%v", page, len(data.Results), data.Total, err)
		}
		u, err := url.Parse(data.Results[0].URL)
		if err != nil || archDailyPageRef(u).id != data.Results[0].ID {
			t.Fatalf("page %d first article identity invalid", page)
		}
		u.RawQuery, u.Fragment = "", ""
		chosen[u.String()] = true
		t.Logf("public category page %d has %d articles", page, len(data.Results))
	}
	var visitedSecond, visitedThird bool
	bounded := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		if body, ok := pages[raw]; ok {
			visitedSecond = visitedSecond || strings.HasSuffix(raw, "?page=2")
			return archDailyTestResponse(raw, http.StatusOK, "application/json", body), nil
		}
		if strings.Contains(raw, "/search/api/") && strings.HasSuffix(raw, "?page=3") {
			visitedThird = true
			return archDailyTestResponse(raw, http.StatusServiceUnavailable, "application/json", ""), nil
		}
		for article := range chosen {
			if raw == article || strings.HasPrefix(raw, article+"/") {
				return getter.Get(ctx, raw, prepare)
			}
		}
		return archDailyTestResponse(raw, http.StatusNotFound, "text/html", ""), nil
	})
	got, err := Discover(ctx, bounded, archDailyCategoryURL, 1)
	if err == nil || !visitedSecond || !visitedThird || len(got.Items) < 2 {
		t.Fatalf("bounded category images=%d page2=%v page3=%v err=%v", len(got.Items), visitedSecond, visitedThird, err)
	}
	ids := map[string]bool{}
	for _, item := range got.Items {
		ids[item.Metadata["archdaily_article_id"]] = true
	}
	if len(ids) != 2 {
		t.Fatalf("wanted one article from each page, got=%+v err=%v", ids, err)
	}
	t.Logf("bounded two-page category traversal extracted %d images from %d live articles", len(got.Items), len(ids))
}

func TestArchDailyRenderedChineseRedirect(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const oldURL = "https://www.archdaily.cn/cn/1014145/sample"
	const target = "https://www.archdaily.com/zh/1014145/sample"
	const gallery = target + "/" + archDailyMedia1 + "-photo"
	var calls atomic.Int32
	session.getter.client.Transport = archDailyRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != gallery {
			return nil, errors.New("unexpected request")
		}
		body := archDailyGalleryFixture(gallery, archDailyTestEntries())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(archDailyArticleFixture(target, gallery))))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case responses <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(oldURL)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || calls.Load() != 1 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestArchDailyRenderedLiveGallery(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" || os.Getenv("TULAI_ARCHDAILY_LIVE") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 and TULAI_ARCHDAILY_LIVE=1 for isolated Chrome and a live gallery")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const media = "65e771b4da34c7017c51cabe"
	const preview = "https://images.adsttc.com/media/images/65e7/71b4/da34/c701/7c51/cabe/newsletter/the-gallery-house-wim-goes-architectuur_2.jpg?1709666804"
	fixture := `<html><head><link rel="canonical" href="` + archDailyPublicArticle + `"></head><body><article id="single-content"><img src="` + preview + `" data-nr-picture-id="` + media + `" longdesc="` + archDailyPublicGallery + `"></article></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == archDailyPublicArticle {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(fixture)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case responses <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(archDailyPublicArticle)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(archDailyPublicArticle)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err != nil && strings.Contains(err.Error(), "网页图片不能访问其他内网地址") {
		if len(got.Items) != 1 {
			t.Fatalf("DNS rejection lost article preview: %d", len(got.Items))
		}
		t.Skipf("live gallery blocked by rendered DNS boundary: %v", err)
	}
	if err != nil || len(got.Items) < 20 {
		t.Fatalf("rendered live gallery images=%d err=%v", len(got.Items), err)
	}
	t.Logf("rendered live gallery images=%d", len(got.Items))
}
