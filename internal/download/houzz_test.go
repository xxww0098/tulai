package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func houzzFixture() map[string]any {
	return map[string]any{"data": map[string]any{"pageName": "viewProject", "stores": map[string]any{"data": map[string]any{
		"CurrentUserStore":     map[string]any{"data": map[string]any{"loggedIn": false}},
		"ViewProjectPageStore": map[string]any{"data": map[string]any{"current_project_id": 123, "profile_user_id": 42}},
		"ProjectStore":         map[string]any{"data": map[string]any{"123": map[string]any{"id": 123, "name": "Project", "userId": 42, "photoIds": []int{10, 11, 12}, "photoCount": 3, "coverSpaceId": 10}, "999": map[string]any{"id": 999, "userId": 77}}},
		"SpaceStore": map[string]any{"data": map[string]any{
			"10": map[string]any{"id": 10, "projectId": 123, "title": "Cover", "imageIds": []string{"aaaaaaaaaaaaaaaa"}},
			"11": map[string]any{"id": 11, "projectId": 123, "title": "Room", "imageIds": []string{"bbbbbbbbbbbbbbbb"}},
			"12": map[string]any{"id": 12, "projectId": 123, "title": "Room 2", "imageIds": []string{"cccccccccccccccc"}},
			"99": map[string]any{"id": 99, "projectId": 999, "imageIds": []string{"dddddddddddddddd"}},
		}},
		"ImageStore": map[string]any{"data": map[string]any{
			"aaaaaaaaaaaaaaaa": map[string]any{"externalId": "aaaaaaaaaaaaaaaa"}, "bbbbbbbbbbbbbbbb": map[string]any{"externalId": "bbbbbbbbbbbbbbbb"},
			"cccccccccccccccc": map[string]any{"externalId": "cccccccccccccccc"}, "dddddddddddddddd": map[string]any{"externalId": "dddddddddddddddd"},
		}},
	}}}}
}
func houzzFixtureHTML(t *testing.T, state map[string]any, extra string) string {
	t.Helper()
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return `<html><head><meta property="og:image" content="https://st.hzcdn.com/fimgs/aaaaaaaaaaaaaaaa_cover.jpg"></head><body><script id="hz-ctx" type="application/json">` + string(b) + `</script>` + extra + `</body></html>`
}
func houzzParseDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func houzzPage(t *testing.T) *url.URL {
	t.Helper()
	p, err := url.Parse("https://www.houzz.com/hznb/projects/example-pj-vj~123")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHouzzRoutesAndImageURLs(t *testing.T) {
	for _, test := range []struct{ raw, id string }{
		{"https://www.houzz.com/hznb/projects/example-pj-vj~123", "123"},
		{"https://houzz.com/hznb/projects/exterior-project-pj-vj~99/", "99"},
		{"https://www.houzz.com.evil.test/hznb/projects/x-pj-vj~123", ""},
		{"https://user@www.houzz.com/hznb/projects/x-pj-vj~123", ""},
		{"https://www.houzz.com:444/hznb/projects/x-pj-vj~123", ""},
		{"https://www.houzz.com/hznb/projects/x-pj-vj~123/other", ""},
		{"https://www.houzz.com/hznb/projects/x%2fy-pj-vj~123", ""},
		{"https://www.houzz.com/photos/x-vp~123", ""},
	} {
		p, err := url.Parse(test.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := houzzProjectID(p); got != test.id {
			t.Errorf("%q got %q", test.raw, got)
		}
	}
	for _, test := range []struct {
		raw string
		ok  bool
	}{
		{"https://st.hzcdn.com/fimgs/aaaaaaaaaaaaaaaa_0021-w1200.jpg", true},
		{"//st.hzcdn.com/simgs/bbbbbbbbbbbbbbbb_14-2301/home-design.jpg", true},
		{"http://st.hzcdn.com/fimgs/aaaaaaaaaaaaaaaa_0021.jpg", false},
		{"https://st.hzcdn.com.evil.test/fimgs/aaaaaaaaaaaaaaaa_0021.jpg", false},
		{"https://st.hzcdn.com@evil.test/fimgs/aaaaaaaaaaaaaaaa_0021.jpg", false},
		{"https://st.hzcdn.com:444/fimgs/aaaaaaaaaaaaaaaa_0021.jpg", false},
		{"https://st.hzcdn.com/fimgs/aaaaaaaaaaaaaaaa_%2e%2e/a.jpg", false},
		{"https://st.hzcdn.com/fimgs/aaaaaaaaaaaaaaaa_0021%2fa.jpg", false},
	} {
		_, _, ok := houzzDisplayedImage(test.raw)
		if ok != test.ok {
			t.Errorf("image %q accepted=%v", test.raw, ok)
		}
	}
}

func TestHouzzProjectIdentityAndDisplayedImages(t *testing.T) {
	page := houzzPage(t)
	source := houzzFixtureHTML(t, houzzFixture(), `<img id="11" src="https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_0027-w300.jpg"><img id="12" src="https://st.hzcdn.com/fimgs/aaaaaaaaaaaaaaaa_wrong.jpg"><img id="99" src="https://st.hzcdn.com/fimgs/dddddddddddddddd_recommendation.jpg"><img id="10" src="https://evil.test/fimgs/aaaaaaaaaaaaaaaa_bad.jpg">`)
	got, err := houzzProjectPage(context.Background(), houzzParseDoc(t, source), page)
	if err == nil || len(got.Items) != 2 || got.Title != "Project" || !strings.Contains(err.Error(), "2/3") || !strings.Contains(err.Error(), "展示图") {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if got.Items[0].URL != "https://st.hzcdn.com/fimgs/aaaaaaaaaaaaaaaa_cover.jpg" || got.Items[1].URL != "https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_0027-w300.jpg" {
		t.Fatalf("wrong images %+v", got.Items)
	}
	for _, it := range got.Items {
		if !it.PublicOnly || !it.RequireImage || it.Headers["Referer"] != "https://www.houzz.com/" || it.Metadata["houzz_project_id"] != "123" || it.Metadata["houzz_author_id"] != "42" {
			t.Fatalf("unsafe item %+v", it)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"other author", func(s map[string]any) {
			s["ViewProjectPageStore"].(map[string]any)["data"].(map[string]any)["profile_user_id"] = 99
		}},
		{"other project", func(s map[string]any) {
			s["ViewProjectPageStore"].(map[string]any)["data"].(map[string]any)["current_project_id"] = 999
		}},
		{"no photo count", func(s map[string]any) {
			delete(s["ProjectStore"].(map[string]any)["data"].(map[string]any)["123"].(map[string]any), "photoCount")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := houzzFixture()
			stores := state["data"].(map[string]any)["stores"].(map[string]any)["data"].(map[string]any)
			test.mutate(stores)
			got, err := houzzProjectPage(context.Background(), houzzParseDoc(t, houzzFixtureHTML(t, state, `<img id="11" src="https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_0027.jpg">`)), page)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("invalid identity got=%+v err=%v", got, err)
			}
		})
	}
	state := houzzFixture()
	stores := state["data"].(map[string]any)["stores"].(map[string]any)["data"].(map[string]any)
	spaces := stores["SpaceStore"].(map[string]any)["data"].(map[string]any)
	spaces["11"].(map[string]any)["projectId"] = 999
	got, err = houzzProjectPage(context.Background(), houzzParseDoc(t, houzzFixtureHTML(t, state, `<img id="11" src="https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_0027.jpg">`)), page)
	if err == nil || len(got.Items) != 1 || !strings.Contains(err.Error(), "无法确认") {
		t.Fatalf("cross-project got=%+v err=%v", got, err)
	}
	// Logged-in visitors can capture images actually displayed on the page.
	logged := houzzFixture()
	loggedStores := logged["data"].(map[string]any)["stores"].(map[string]any)["data"].(map[string]any)
	loggedStores["CurrentUserStore"].(map[string]any)["data"].(map[string]any)["loggedIn"] = true
	loggedDoc := houzzParseDoc(t, houzzFixtureHTML(t, logged, `<img id="11" src="https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_shown.jpg">`))
	loggedResult, loggedErr := houzzProjectPage(context.Background(), loggedDoc, page)
	if loggedErr == nil || len(loggedResult.Items) != 2 {
		t.Fatalf("logged in count=%d err=%v", len(loggedResult.Items), loggedErr)
	}
	delete(loggedStores, "CurrentUserStore")
	missingResult, missingErr := houzzProjectPage(context.Background(), houzzParseDoc(t, houzzFixtureHTML(t, logged, `<img id="11" src="https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_shown.jpg">`)), page)
	if missingErr == nil || len(missingResult.Items) != 2 {
		t.Fatalf("missing login marker count=%d err=%v", len(missingResult.Items), missingErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := houzzProjectPage(ctx, houzzParseDoc(t, source), page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

type houzzFixtureGetter struct{ final, body string }

func (g *houzzFixtureGetter) Get(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
	return tiebaTestResponse(firstNonEmpty(g.final, raw), http.StatusOK, g.body), nil
}

func TestHouzzDiscoverProjectRoutingAndRedirects(t *testing.T) {
	page := houzzPage(t)
	fixture := houzzFixtureHTML(t, houzzFixture(), `<img id="11" src="https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_shown.jpg">`)
	getter := &houzzFixtureGetter{body: fixture}
	got, err := Discover(context.Background(), getter, page.String(), 1)
	if err == nil || len(got.Items) != 2 || got.Items[0].Metadata["houzz_project_id"] != "123" {
		t.Fatalf("Discover count=%d err=%v", len(got.Items), err)
	}
	for _, final := range []string{
		"https://www.houzz.com/hznb/projects/other-pj-vj~124",
		"http://www.houzz.com/hznb/projects/example-pj-vj~123",
		"https://evil.test/hznb/projects/example-pj-vj~123",
	} {
		getter.final = final
		got, err = Discover(context.Background(), getter, page.String(), 1)
		if err == nil || len(got.Items) != 0 {
			t.Fatalf("accepted redirect to %s", final)
		}
	}
}

func TestHouzzPublicFixture(t *testing.T) {
	file := os.Getenv("TULAI_HOUZZ_PUBLIC_FIXTURE")
	if file == "" {
		t.Skip("set TULAI_HOUZZ_PUBLIC_FIXTURE to a previously obtained public project page")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://www.houzz.com/hznb/projects/chambers-complete-house-pj-vj~7964067")
	got, warning := houzzProjectPage(context.Background(), houzzParseDoc(t, string(b)), page)
	if len(got.Items) != 1 || warning == nil {
		t.Fatalf("public capture items=%d warning=%v", len(got.Items), warning)
	}
	t.Logf("public project capture: %d picture; warning=%v", len(got.Items), warning)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("public preview download failed: %T", err)
	}
	t.Log("public preview download succeeded")
}

func TestHouzzRenderedProject(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	fixture := houzzFixtureHTML(t, houzzFixture(), `<img id="11" src="https://st.hzcdn.com/fimgs/bbbbbbbbbbbbbbbb_shown.jpg"><img id="99" src="https://st.hzcdn.com/fimgs/dddddddddddddddd_other.jpg">`)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "www.houzz.com" {
			http.Error(w, "bad host", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/hznb/projects/example-pj-vj~123" {
			fmt.Fprint(w, fixture)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page := houzzPage(t)
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
	if err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "展示图") {
		t.Fatalf("Capture items=%d err=%v", len(got.Items), err)
	}
	for _, item := range got.Items {
		if item.Metadata["houzz_project_id"] != "123" || item.Metadata["houzz_author_id"] != "42" {
			t.Fatalf("bad item %+v", item)
		}
	}
}
