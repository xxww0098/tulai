package download

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const facebookTestPage = "https://www.facebook.com/NASA/photos"
const facebookTestImage = "https://scontent-lax3-2.xx.fbcdn.net/v/t39.99422-6/123_456_789_n.png?stp=c320.0.1280.1280a_dst-jpg_tt6&ctp=s552x414&oh=synthetic"
const facebookTestLink = "https://www.facebook.com/photo.php?fbid=1672152994280070&set=pb.100044561550831.-2207520000&type=3"

func facebookTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func facebookTestState() facebookState {
	return facebookState{Location: facebookTestPage, Canonical: "https://www.facebook.com/NASA/", ProfileID: "100044561550831", IOSProfileID: "100044561550831", Tiles: []facebookTile{{Link: facebookTestLink, URL: facebookTestImage}}}
}

func TestFacebookRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, kind, id string }{
		{facebookTestPage, "grid", "nasa"},
		{facebookTestPage + "?tab=album&album_id=123", "profile", "nasa"},
		{"https://www.facebook.com/NASA/photos_albums", "profile", "nasa"},
		{facebookTestLink, "photo", "1672152994280070"},
		{"https://www.facebook.com/photo/?fbid=123", "photo", "123"},
		{"https://www.facebook.com/NASA/photos/a.123/456/", "photo", "456"},
		{"https://www.facebook.com/media/set/?set=a.123", "album", "123"},
		{"https://www.facebook.com/profile.php?id=123&sk=photos", "profile", "123"},
		{"https://www.facebook.com/photo.php?fbid=123&fbid=456", "", ""},
		{"https://www.facebook.com/NASA/posts/123", "", ""},
		{"https://www.facebook.com.evil.test/NASA/photos", "", ""},
		{"https://user@www.facebook.com/NASA/photos", "", ""},
		{"https://www.facebook.com:8443/NASA/photos", "", ""},
	} {
		kind, id := facebookRoute(facebookTestURL(t, tc.raw))
		if kind != tc.kind || id != tc.id {
			t.Errorf("route %s: %s %s; want %s %s", tc.raw, kind, id, tc.kind, tc.id)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{facebookTestImage, facebookTestImage},
		{"http://scontent-lax3-2.xx.fbcdn.net/v/t39/a_n.jpg", ""},
		{"https://scontent-lax3-2.xx.fbcdn.net.evil.test/v/t39/a_n.jpg", ""},
		{"https://user@scontent-lax3-2.xx.fbcdn.net/v/t39/a_n.jpg", ""},
		{"https://scontent-lax3-2.xx.fbcdn.net:8443/v/t39/a_n.jpg", ""},
		{"https://scontent-lax3-2.xx.fbcdn.net/v/t39/../a_n.jpg", ""},
		{"https://static.xx.fbcdn.net/v/t39/a_n.jpg", ""},
		{"https://scontent-lax3-2.xx.fbcdn.net/v/t39/a_n.mp4", ""},
		{"https://scontent-lax3-2.xx.fbcdn.net/v/t39/a_s.jpg", ""},
	} {
		if got := facebookImage(tc.raw); got != tc.want {
			t.Errorf("image %s: got %q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestFacebookCollectIdentityAndLogin(t *testing.T) {
	page := facebookTestURL(t, facebookTestPage)
	state := facebookTestState()
	got, err := facebookCollectState(context.Background(), page, state)
	if err == nil || !strings.Contains(err.Error(), "预览") || len(got.Items) != 1 {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	item := got.Items[0]
	if item.URL != facebookTestImage || item.Metadata["facebook_photo_id"] != "1672152994280070" || item.Metadata["image_kind"] != "preview" || !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://www.facebook.com/" {
		t.Fatalf("unsafe item %+v", item)
	}
	for _, tc := range []struct {
		name string
		edit func(*facebookState)
	}{
		{"login wall", func(s *facebookState) { s.Blocked = true }},
		{"wrong page", func(s *facebookState) { s.Location = "https://www.facebook.com/other/photos" }},
		{"wrong canonical", func(s *facebookState) { s.Canonical = "https://www.facebook.com/other/" }},
		{"missing profile", func(s *facebookState) { s.ProfileID = "" }},
		{"wrong owner", func(s *facebookState) { s.ProfileID = "999" }},
		{"foreign anchor", func(s *facebookState) {
			s.Tiles[0].Link = "https://evil.test/photo.php?fbid=123&set=pb.100044561550831.1"
		}},
		{"foreign image", func(s *facebookState) { s.Tiles[0].URL = "https://evil.test/a_n.jpg" }},
		{"wrong set", func(s *facebookState) {
			s.Tiles[0].Link = strings.Replace(facebookTestLink, "pb.100044561550831", "pb.999", 1)
		}},
		{"album set", func(s *facebookState) {
			s.Tiles[0].Link = strings.Replace(facebookTestLink, "pb.100044561550831.-2207520000", "a.999", 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := facebookTestState()
			tc.edit(&state)
			got, err := facebookCollectState(context.Background(), page, state)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
	state = facebookTestState()
	state.Tiles = append(state.Tiles, state.Tiles[0])
	got, err = facebookCollectState(context.Background(), page, state)
	if err == nil || len(got.Items) != 1 {
		t.Fatalf("duplicate items=%+v err=%v", got.Items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = facebookCollectState(ctx, page, state)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestFacebookDirectMode(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("must not request undocumented APIs")
		return nil, nil
	})
	got, err := discoverFacebook(context.Background(), getter, facebookTestURL(t, facebookTestPage))
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "登录") {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = facebookCapture(ctx, nil, facebookTestURL(t, facebookTestPage))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("capture cancellation: %v", err)
	}
}

func TestFacebookPublicThumbnail(t *testing.T) {
	raw := os.Getenv("TULAI_FACEBOOK_CDN_URL")
	if raw == "" {
		t.Skip("set TULAI_FACEBOOK_CDN_URL to an observed public preview URL")
	}
	imageURL := facebookImage(raw)
	if imageURL == "" {
		t.Fatal("not a recognized Facebook image URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	item := Item{URL: imageURL, Filename: "facebook-preview", RequireImage: true, PublicOnly: true}
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	f, err := os.Open(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		t.Fatalf("image decode: %v", err)
	}
}

// Opens the public page using a fresh profile; it must stop if Facebook shows
// a login wall, and must never turn the blocked page's thumbnails into items.
func TestFacebookAnonymousLoginWall(t *testing.T) {
	if os.Getenv("TULAI_FACEBOOK_ANON_LIVE") != "1" {
		t.Skip("set TULAI_FACEBOOK_ANON_LIVE=1 to check the anonymous live page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), facebookTestPage, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.run(ctx, chromedp.Sleep(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "登录") {
		t.Fatalf("anonymous capture: items=%d err=%v", len(got.Items), err)
	}
}

type facebookRoundTrip func(*http.Request) (*http.Response, error)

func (f facebookRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func facebookFixture() string {
	return `<html><head><meta property="og:url" content="https://www.facebook.com/NASA/">` +
		`<meta property="al:android:url" content="fb://profile/100044561550831"><meta property="al:ios:url" content="fb://profile/100044561550831"></head><body>` +
		`<a style="display:inline-block;width:100px;height:100px" href="` + strings.ReplaceAll(facebookTestLink, "&", "&amp;") + `"><img width="100" height="100" src="` + strings.ReplaceAll(facebookTestImage, "&", "&amp;") + `"></a>` +
		`<a href="https://www.facebook.com/photo.php?fbid=888&amp;set=pb.999.-2207520000"><img src="` + strings.ReplaceAll(facebookTestImage, "&", "&amp;") + `"></a>` +
		`<img src="https://scontent-lax3-2.xx.fbcdn.net/v/t39/avatar_n.jpg"></body></html>`
}

func TestFacebookRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome integration")
	}
	for _, name := range []string{"preview", "login", "foreign-owner"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
			defer server.Close()
			session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			body := facebookFixture()
			switch name {
			case "login":
				body = strings.Replace(body, `</body>`, `<div role="dialog"><input type="password"></div></body>`, 1)
			case "foreign-owner":
				body = strings.Replace(body, `fb://profile/100044561550831`, `fb://profile/999`, 1)
			}
			interceptErrors := make(chan error, 8)
			camoufox.ListenTarget(session.tab, func(event any) {
				paused, ok := event.(*fetch.EventRequestPaused)
				if !ok {
					return
				}
				go func() {
					var action camoufox.Action
					if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == facebookTestPage {
						action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
					} else {
						action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
					}
					if err := camoufox.Run(session.tab, action); err != nil {
						select {
						case interceptErrors <- err:
						default:
						}
					}
				}()
			})
			if err := session.run(ctx, fetch.Enable()); err != nil {
				t.Fatal(err)
			}
			session.start = facebookTestURL(t, facebookTestPage)
			if err := session.run(ctx, camoufox.Navigate(facebookTestPage)); err != nil {
				t.Fatal(err)
			}
			if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
				return network.SetCookie("synthetic_session", "example").WithDomain(".facebook.com").WithURL("https://www.facebook.com/").WithHTTPOnly(true).Do(ec)
			})); err != nil {
				t.Fatal(err)
			}
			calls := 0
			session.getter.client.Transport = facebookRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "scontent-lax3-2.xx.fbcdn.net" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.facebook.com/" {
					t.Errorf("unsafe CDN request %v", r.Header)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic")), Request: r}, nil
			})
			got, err := session.Capture(ctx)
			switch name {
			case "login":
				if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "登录") {
					t.Fatalf("login=%+v err=%v", got.Items, err)
				}
			case "foreign-owner":
				if err == nil || len(got.Items) != 0 {
					t.Fatalf("foreign owner=%+v err=%v", got.Items, err)
				}
			default:
				if err == nil || !strings.Contains(err.Error(), "预览") || len(got.Items) != 1 || session.PageURL() != facebookTestPage {
					t.Fatalf("got=%+v page=%s err=%v", got.Items, session.PageURL(), err)
				}
				resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) {
					for k, v := range got.Items[0].Headers {
						r.Header.Set(k, v)
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if calls != 1 {
					t.Fatalf("CDN calls=%d", calls)
				}
			}
			select {
			case err := <-interceptErrors:
				t.Fatalf("intercept: %v", err)
			default:
			}
		})
	}
}
