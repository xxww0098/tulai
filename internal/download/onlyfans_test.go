package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func onlyFansTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func onlyFansFixture(posts ...map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"list": posts, "hasMore": true})
	return b
}

func onlyFansPostFixture(post, author, media, asset string) map[string]any {
	return map[string]any{
		"id": post, "author": map[string]any{"id": author}, "isOpened": true, "canViewMedia": true,
		"text": "must not be stored", "media": []any{map[string]any{
			"id": media, "type": "photo", "canView": true,
			"files": map[string]any{"full": map[string]any{"url": asset}},
		}},
	}
}

func TestOnlyFansRoutesAndSources(t *testing.T) {
	for _, tc := range []struct {
		raw, profile string
		site         bool
	}{
		{"https://onlyfans.com/Alice/", "alice", true},
		{"https://onlyfans.com/alice?tab=photos", "alice", true},
		{"http://onlyfans.com/alice", "", true},
		{"https://onlyfans.com/login", "", true},
		{"https://onlyfans.com/my/chats", "", true},
		{"https://www.onlyfans.com/alice", "", true},
		{"https://evil@onlyfans.com/alice", "", false},
		{"https://onlyfans.com.evil.test/alice", "", false},
		{"https://onlyfans.com:444/alice", "", false},
		{"https://onlyfans.com/alice%2fother", "", true},
	} {
		u := onlyFansTestURL(t, tc.raw)
		if got := onlyFansProfile(u); got != tc.profile {
			t.Errorf("profile %q = %q", tc.raw, got)
		}
		if got := isOnlyFansSite(u); got != tc.site {
			t.Errorf("site %q = %v", tc.raw, got)
		}
	}
	origin := onlyFansTestURL(t, "https://onlyfans.com/alice")
	for _, tc := range []struct{ raw, kind, user string }{
		{"https://onlyfans.com/api2/v2/users/alice", "profile", "alice"},
		{"https://onlyfans.com/api2/v2/users/123/posts?limit=10", "posts", "123"},
		{"https://onlyfans.com/api2/v2/users/bob", "", ""},
		{"https://onlyfans.com/api2/v2/users/123/posts", "", ""},
		{"https://onlyfans.com/api2/v2/users/123/posts/medias?limit=10", "", ""},
		{"https://onlyfans.com/api2/v2/users/xyz/posts?limit=10", "", ""},
		{"https://evil@onlyfans.com/api2/v2/users/alice", "", ""},
		{"https://www.onlyfans.com/api2/v2/users/alice", "", ""},
		{"https://onlyfans.com:444/api2/v2/users/alice", "", ""},
		{"https://onlyfans.com/api2/v2/users/alice%2fposts", "", ""},
	} {
		kind, user := onlyFansAPI(tc.raw, origin, "alice")
		if kind != tc.kind || user != tc.user {
			t.Errorf("API %q = %q, %q", tc.raw, kind, user)
		}
	}
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"https://cdn2.onlyfans.com/files/x/example.jpg?Policy=synthetic&Signature=synthetic", true},
		{"https://cdn3.onlyfans.com/files/x/example", true},
		{"http://cdn2.onlyfans.com/files/x/example.jpg", false},
		{"https://evil@cdn2.onlyfans.com/files/x/example.jpg", false},
		{"https://cdn2.onlyfans.com:444/files/x/example.jpg", false},
		{"https://cdn2.onlyfans.com.evil.test/files/x/example.jpg", false},
		{"https://127.0.0.1/files/x/example.jpg", false},
		{"https://cdn2.onlyfans.com/files/%2e%2e/example.jpg", false},
		{"https://cdn2.onlyfans.com/files/x%2fexample.jpg", false},
		{"https://cdn2.onlyfans.com/profile/example.jpg", false},
	} {
		_, ok := onlyFansImage(tc.raw)
		if ok != tc.ok {
			t.Errorf("image %q = %v", tc.raw, ok)
		}
	}
}

func TestOnlyFansIdentity(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"id":123,"username":"Alice"}`, "123"},
		{`{"id":"123","username":"alice"}`, "123"},
		{`{"id":123,"username":"bob"}`, ""},
		{`{"id":0.2,"username":"alice"}`, ""},
		{`{"username":"alice"}`, ""},
	} {
		got, err := onlyFansIdentity([]byte(tc.body), "alice")
		if got != tc.want || ((err == nil) != (tc.want != "")) {
			t.Errorf("identity %s = %q, %v", tc.body, got, err)
		}
	}
	if _, err := onlyFansIdentity(make([]byte, onlyFansMaxBody+1), "alice"); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestOnlyFansVisiblePhotosAndRestrictions(t *testing.T) {
	asset := "https://cdn2.onlyfans.com/files/x/valid.jpg?Policy=synthetic"
	valid := onlyFansPostFixture("10", "123", "99", asset)
	items, partial, err := onlyFansPostImages(onlyFansFixture(valid), "123", "alice")
	if err != nil || partial || len(items) != 1 || items[0].URL != asset || !items[0].RequireImage || !items[0].PublicOnly ||
		items[0].Metadata["onlyfans_post_id"] != "10" || items[0].Metadata["onlyfans_author"] != "alice" || len(items[0].Metadata) != 3 {
		t.Fatalf("visible items=%+v partial=%v err=%v", items, partial, err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"locked post":         func(p map[string]any) { p["isOpened"] = false },
		"unknown post state":  func(p map[string]any) { delete(p, "isOpened") },
		"locked media":        func(p map[string]any) { p["canViewMedia"] = false },
		"unknown media state": func(p map[string]any) { delete(p, "canViewMedia") },
		"other author":        func(p map[string]any) { p["author"] = map[string]any{"id": "456"} },
		"invalid post id":     func(p map[string]any) { p["id"] = "wrong" },
		"media locked":        func(p map[string]any) { p["media"].([]any)[0].(map[string]any)["canView"] = false },
		"media unknown":       func(p map[string]any) { delete(p["media"].([]any)[0].(map[string]any), "canView") },
		"invalid media id":    func(p map[string]any) { p["media"].([]any)[0].(map[string]any)["id"] = "wrong" },
		"other CDN": func(p map[string]any) {
			p["media"].([]any)[0].(map[string]any)["files"] = map[string]any{"full": map[string]any{"url": "https://evil.test/files/x.jpg"}}
		},
	} {
		p := onlyFansPostFixture("11", "123", "100", asset)
		mutate(p)
		items, partial, err := onlyFansPostImages(onlyFansFixture(valid, p), "123", "alice")
		if err != nil || !partial || len(items) != 1 || items[0].URL != asset {
			t.Errorf("%s items=%+v partial=%v err=%v", name, items, partial, err)
		}
	}
	video := onlyFansPostFixture("11", "123", "100", asset)
	video["media"].([]any)[0].(map[string]any)["type"] = "video"
	items, partial, err = onlyFansPostImages(onlyFansFixture(video), "123", "alice")
	if err != nil || partial || len(items) != 0 {
		t.Fatalf("video items=%+v partial=%v err=%v", items, partial, err)
	}
	if _, _, err := onlyFansPostImages([]byte(`{"error":"login"}`), "123", "alice"); err == nil {
		t.Fatal("login error accepted")
	}
	locked := onlyFansPostFixture("12", "123", "101", asset)
	locked["media"] = []any{}
	locked["mediaCount"] = 1
	locked["canViewMedia"] = false
	items, partial, err = onlyFansPostImages(onlyFansFixture(valid, locked), "123", "alice")
	if err != nil || !partial || len(items) != 1 {
		t.Fatalf("hidden locked media items=%d partial=%v err=%v", len(items), partial, err)
	}
	if _, _, err := onlyFansPostImages(make([]byte, onlyFansMaxBody+1), "123", "alice"); err == nil {
		t.Fatal("oversize accepted")
	}
}

func TestOnlyFansLimitAndDiscover(t *testing.T) {
	posts := make([]map[string]any, onlyFansMaxImages+1)
	for i := range posts {
		posts[i] = onlyFansPostFixture(fmt.Sprint(i+1), "123", fmt.Sprint(i+1), "https://cdn2.onlyfans.com/files/a/img.jpg")
	}
	items, partial, err := onlyFansPostImages(onlyFansFixture(posts...), "123", "alice")
	if err != nil || !partial || len(items) != onlyFansMaxImages {
		t.Fatalf("oversized list items=%d partial=%v err=%v", len(items), partial, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverOnlyFans(ctx, nil, onlyFansTestURL(t, "https://onlyfans.com/alice")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	got, err := discoverOnlyFans(context.Background(), nil, onlyFansTestURL(t, "https://onlyfans.com/alice"))
	if err == nil || !got.IsPage || !strings.Contains(err.Error(), "登录") {
		t.Fatalf("discover=%+v err=%v", got, err)
	}
}
