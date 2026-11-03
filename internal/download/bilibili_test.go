package download

import (
	"context"
	"encoding/base64"
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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

func TestBilibiliPostURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.bilibili.com/opus/123?share=1": "123",
		"https://bilibili.com/opus/123/":            "123",
		"https://m.bilibili.com/opus/123":           "123",
		"https://t.bilibili.com/123":                "123",
		"https://t.bilibili.com/123/":               "123",
		"https://www.bilibili.com/read/cv123":       "",
		"https://space.bilibili.com/123":            "",
		"https://bilibili.com.evil.test/opus/123":   "",
		"https://user@www.bilibili.com/opus/123":    "",
		"https://www.bilibili.com:8443/opus/123":    "",
		"https://t.bilibili.com/0":                  "",
		"https://t.bilibili.com/abc":                "",
	} {
		u, err := url.Parse(raw)
		if err != nil || bilibiliPostID(u) != want {
			t.Errorf("bilibiliPostID(%q) = %q, want %q (parse %v)", raw, bilibiliPostID(u), want, err)
		}
	}
}

func TestBilibiliPostExtractsOnlyOriginalPictures(t *testing.T) {
	root, _ := url.Parse("https://t.bilibili.com/123?private=1")
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		if raw != "https://www.bilibili.com/opus/123" {
			t.Fatalf("canonical URL = %q", raw)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Fatalf("credentials sent: %v", req.Header)
		}
		body := `<script>window.__INITIAL_STATE__ = {"detail":{"id_str":"123","basic":{"title":"图文标题"},"modules":[` +
			`{"module_top":{"display":{"album":{"pics":[{"url":"http://i0.hdslb.com/cover.jpg"}]}}}},` +
			`{"module_content":{"paragraphs":[{"pic":{"pics":[{"url":"https://i0.hdslb.com/a.jpg"}]}},` +
			`{"album":{"pics":[{"url":"//i1.hdslb.com/b.jpg"},{"url":"https://i0.hdslb.com/a.jpg"}]}}]}}],` +
			`"other":{"pic":{"pics":[{"url":"https://i0.hdslb.com/other-post.jpg"}]}}}};(function(){})();</script>`
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := discoverBilibiliPost(context.Background(), getter, root)
	if err != nil || !got.IsPage || got.Title != "图文标题" || len(got.Items) != 3 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	want := []string{"https://i0.hdslb.com/cover.jpg", "https://i0.hdslb.com/a.jpg", "https://i1.hdslb.com/b.jpg"}
	for i, item := range got.Items {
		if item.URL != want[i] || item.Headers["Referer"] != "https://www.bilibili.com/" {
			t.Errorf("image %d = %+v, want %q", i, item, want[i])
		}
	}
}

func TestBilibiliPostRejectsWrongStateAndKeepsPartial(t *testing.T) {
	for _, scenario := range []string{"wrong-id", "external-redirect", "partial", "missing-url", "unsafe-url", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			root, _ := url.Parse("https://www.bilibili.com/opus/123")
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				if scenario == "cancel" {
					cancel()
					return nil, ctx.Err()
				}
				id, pics, final := "123", `[{"url":"https://i0.hdslb.com/a.jpg"},{"url":7}]`, raw
				switch scenario {
				case "wrong-id":
					id = "999"
				case "external-redirect":
					final = "https://evil.test/opus/123"
				case "missing-url":
					pics = `[{"url":"https://i0.hdslb.com/a.jpg"},{}]`
				case "unsafe-url":
					pics = `[{"url":"https://i0.hdslb.com/a.jpg"},{"url":"https://evil.test/a.jpg"},{"url":"http://127.0.0.1/a.jpg"},{"url":"https://user:secret@i0.hdslb.com/a.jpg"},{"url":"https://i0.hdslb.com:444/a.jpg"},{"url":"https://i0.hdslb.com.evil.test/a.jpg"}]`
				}
				body := fmt.Sprintf(`<script>window.__INITIAL_STATE__={"detail":{"id_str":%q,"modules":[{"pic":{"pics":%s}}]}};</script>`, id, pics)
				return &Response{StatusCode: 200, FinalURL: final, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			got, err := discoverBilibiliPost(ctx, getter, root)
			if err == nil {
				t.Fatal("expected error")
			}
			if (scenario == "partial" || scenario == "missing-url" || scenario == "unsafe-url") && (len(got.Items) != 1 || got.Items[0].URL != "https://i0.hdslb.com/a.jpg") {
				t.Fatalf("partial images = %+v", got.Items)
			}
			if scenario == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation = %v", err)
			}
			if (scenario == "wrong-id" || scenario == "external-redirect") && len(got.Items) != 0 {
				t.Fatalf("unexpected images = %+v", got.Items)
			}
		})
	}
}

func TestBilibiliPostLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_BILIBILI_LIVE") != "1" {
		t.Skip("set TULAI_BILIBILI_LIVE=1 to check a public Bilibili opus")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, _ := url.Parse("https://t.bilibili.com/953619104940425225")
	got, err := discoverBilibiliPost(ctx, NewHTTPGetter(""), root)
	if err != nil || len(got.Items) != 1 || !strings.HasPrefix(got.Items[0].URL, "https://i0.hdslb.com/bfs/new_dyn/") {
		t.Fatalf("public opus: %+v, %v", got.Items, err)
	}
	dir := t.TempDir()
	got.Items[0].Filename, got.Items[0].RequireImage = "image.jpg", true
	err = New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, got.Items[0], nil)
	if stat, statErr := os.Stat(filepath.Join(dir, "image.jpg")); err != nil || statErr != nil || stat.Size() == 0 {
		t.Fatalf("public opus download: %v, stat=%v", err, statErr)
	}
	article, _ := url.Parse("https://www.bilibili.com/opus/947531371067211815")
	got, err = discoverBilibiliPost(ctx, NewHTTPGetter(""), article)
	if err != nil || len(got.Items) != 4 {
		t.Fatalf("public long opus: %d images, %v", len(got.Items), err)
	}
}

func TestBilibiliSpaceURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://space.bilibili.com/123":              "123",
		"https://space.bilibili.com/123/album":        "123",
		"https://space.bilibili.com/123/article":      "123",
		"https://space.bilibili.com/123/dynamic":      "123",
		"https://space.bilibili.com/123/upload/opus/": "123",
		"https://space.bilibili.com.evil.test/123":    "",
		"https://user@space.bilibili.com/123":         "",
		"https://space.bilibili.com:444/123":          "",
		"https://space.bilibili.com/123/video":        "",
	} {
		u, _ := url.Parse(raw)
		id, _ := bilibiliSpaceParts(u)
		if id != want {
			t.Errorf("space %s = %q, want %q", raw, id, want)
		}
	}
}

func TestBilibiliSpaceFeedPaginationAndPartial(t *testing.T) {
	for _, mode := range []string{"complete", "post-failure", "cancel", "repeated-offset", "invalid-jump"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var offsets, posts []string
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
				req.Header.Set("Cookie", "private=session")
				req.Header.Set("Authorization", "private")
				prepare(req)
				if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
					t.Fatalf("credentials sent: %v", req.Header)
				}
				if req.URL.Host == "api.bilibili.com" {
					if req.URL.Query().Get("host_mid") != "123" {
						t.Fatalf("API = %s", raw)
					}
					offset := req.URL.Query().Get("offset")
					offsets = append(offsets, offset)
					body := `{"code":0,"data":{"items":[{"opus_id":"101","jump_url":"//www.bilibili.com/opus/101"},{"opus_id":"102"}],"has_more":true,"offset":"cursor"}}`
					if offset != "" {
						body = `{"code":0,"data":{"items":[{"opus_id":"103"}],"has_more":false,"offset":""}}`
						if mode == "repeated-offset" {
							body = `{"code":0,"data":{"items":[{"opus_id":"103"}],"has_more":true,"offset":"cursor"}}`
						}
					}
					if mode == "invalid-jump" && offset == "" {
						body = `{"code":0,"data":{"items":[{"opus_id":"101","jump_url":"https://evil.test/opus/101"}],"has_more":false}}`
					}
					return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				id := strings.TrimPrefix(req.URL.Path, "/opus/")
				posts = append(posts, id)
				if mode == "cancel" && id == "102" {
					cancel()
					return nil, ctx.Err()
				}
				status := 200
				if mode == "post-failure" && id == "102" {
					status = 403
				}
				body := fmt.Sprintf(`<script>window.__INITIAL_STATE__={"detail":{"id_str":%q,"modules":[{"pic":{"pics":[{"url":"https://i0.hdslb.com/%s.jpg"}]}}]}};</script>`, id, id)
				return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			root, _ := url.Parse("https://space.bilibili.com/123/upload/opus?secret=private")
			got, err := discoverBilibiliSpace(ctx, getter, root)
			switch mode {
			case "complete":
				if err != nil || len(got.Items) != 3 || strings.Join(offsets, ",") != ",cursor" {
					t.Fatalf("complete: images=%d offsets=%v err=%v", len(got.Items), offsets, err)
				}
			case "post-failure":
				var status *HTTPStatusError
				if !errors.As(err, &status) || status.StatusCode != 403 || len(got.Items) != 2 {
					t.Fatalf("partial: images=%d err=%v", len(got.Items), err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || len(got.Items) != 1 || len(offsets) != 1 {
					t.Fatalf("cancel: images=%d offsets=%v err=%v", len(got.Items), offsets, err)
				}
			case "repeated-offset":
				if err == nil || len(got.Items) != 3 || len(offsets) != 2 {
					t.Fatalf("repeat: images=%d offsets=%v err=%v", len(got.Items), offsets, err)
				}
			case "invalid-jump":
				if err == nil || len(got.Items) != 0 || len(posts) != 0 {
					t.Fatalf("invalid jump: images=%d posts=%v err=%v", len(got.Items), posts, err)
				}
			}
		})
	}
}

func TestBilibiliSpaceAlbumPagination(t *testing.T) {
	var pages []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		req.Header.Set("Cookie", "session=private")
		prepare(req)
		if req.Header.Get("Cookie") != "" || req.URL.Host != "api.bilibili.com" {
			t.Fatalf("unsafe request: %s %v", raw, req.Header)
		}
		page := req.URL.Query().Get("page_num")
		pages = append(pages, page)
		var items []any
		count := 0
		switch page {
		case "0":
			count = 30
		case "1", "2":
			count = 1
		}
		for i := 0; i < count; i++ {
			id := len(items) + 1
			if page == "1" || page == "2" {
				id += 30
			}
			if page == "2" {
				id++
			}
			items = append(items, map[string]any{"dyn_id": fmt.Sprint(id), "count": 1, "pictures": []any{map[string]any{"img_src": fmt.Sprintf("http://i0.hdslb.com/%d.jpg", id)}}})
		}
		body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"items": items}})
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	root, _ := url.Parse("https://space.bilibili.com/123/album")
	got, err := discoverBilibiliSpace(context.Background(), getter, root)
	if err != nil || len(got.Items) != 32 || strings.Join(pages, ",") != "0,1,2,3" || got.Items[0].URL != "https://i0.hdslb.com/1.jpg" {
		t.Fatalf("album images=%d pages=%v err=%v", len(got.Items), pages, err)
	}
}

func TestBilibiliSpaceAlbumInvalidOrIncompletePage(t *testing.T) {
	for _, body := range []string{
		`{"code":0,"data":{"items":null,"has_more":true}}`,
		`{"code":0,"data":{}}`,
		`{"code":0,"data":{"items":{}}}`,
		`{"code":-101,"message":"login required"}`,
	} {
		getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
			return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		root, _ := url.Parse("https://space.bilibili.com/123/album")
		if _, err := discoverBilibiliSpace(context.Background(), getter, root); err == nil {
			t.Errorf("accepted incomplete page: %s", body)
		}
	}
}

func TestBilibiliSpaceRootMergesSourcesAndDynamicWarns(t *testing.T) {
	for _, mode := range []string{"root", "root-login", "dynamic"} {
		t.Run(mode, func(t *testing.T) {
			var apiPaths []string
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
				prepare(req)
				body := ""
				if req.URL.Host == "api.bilibili.com" {
					apiPaths = append(apiPaths, req.URL.Path)
					switch req.URL.Path {
					case "/x/polymer/web-dynamic/v1/opus/feed/space":
						body = `{"code":0,"data":{"items":[{"opus_id":"101"}],"has_more":false}}`
						if mode == "root-login" {
							body = `{"code":-101,"message":"login required"}`
						}
					case "/x/dynamic/feed/draw/doc_list":
						body = `{"code":0,"data":{"items":null}}`
						if req.URL.Query().Get("page_num") == "0" {
							body = `{"code":0,"data":{"items":[{"dyn_id":"101","count":1,"pictures":[{"img_src":"https://i0.hdslb.com/101.jpg"}]},{"dyn_id":"102","count":1,"pictures":[{"img_src":"https://i2.hdslb.com/102.jpg"}]}]}}`
						}
					}
				} else {
					body = `<script>window.__INITIAL_STATE__={"detail":{"id_str":"101","modules":[{"pic":{"pics":[{"url":"https://i0.hdslb.com/101.jpg"}]}}]}};</script>`
				}
				return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			raw := "https://space.bilibili.com/123"
			if mode == "dynamic" {
				raw += "/dynamic"
			}
			root, _ := url.Parse(raw)
			got, err := discoverBilibiliSpace(context.Background(), getter, root)
			switch mode {
			case "root":
				if err != nil || len(apiPaths) != 3 || len(got.Items) != 2 || got.Items[1].URL != "https://i2.hdslb.com/102.jpg" {
					t.Fatalf("merged: paths=%v images=%+v err=%v", apiPaths, got.Items, err)
				}
			case "root-login":
				if err == nil || !strings.Contains(err.Error(), "-101") || len(apiPaths) != 3 || len(got.Items) != 2 {
					t.Fatalf("login: paths=%v images=%+v err=%v", apiPaths, got.Items, err)
				}
			case "dynamic":
				if err == nil || !strings.Contains(err.Error(), "其他动态类型") || len(apiPaths) != 1 || len(got.Items) != 1 {
					t.Fatalf("dynamic: paths=%v images=%+v err=%v", apiPaths, got.Items, err)
				}
			}
		})
	}
}

func TestBilibiliCaptureCurrentPostOnly(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><title>placeholder</title></html>")
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://www.bilibili.com/opus/123"
	const document = `<html><head><title>页面标题</title><script>window.__INITIAL_STATE__={"detail":{"id_str":"123","basic":{"title":"本篇图文"},"modules":[{"pic":{"pics":[{"url":"http://i0.hdslb.com/one.jpg"},{"url":"https://i1.hdslb.com/two.jpg"}]}}],"related":{"pic":{"pics":[{"url":"https://i0.hdslb.com/other.jpg"}]}}}};</script></head><body><img src="https://i0.hdslb.com/avatar.jpg"></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(document)))
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
	session.start, _ = url.Parse(target)
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || got.Title != "本篇图文" || len(got.Items) != 2 || got.Items[0].URL != "https://i0.hdslb.com/one.jpg" || got.Items[1].URL != "https://i1.hdslb.com/two.jpg" {
		t.Fatalf("captured post = %+v, err = %v", got, err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestBilibiliSpaceLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_BILIBILI_LIVE") != "1" {
		t.Skip("set TULAI_BILIBILI_LIVE=1 to check public Bilibili space endpoints")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	var first, second struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				ID string `json:"opus_id"`
			} `json:"items"`
			Offset  string `json:"offset"`
			HasMore bool   `json:"has_more"`
		} `json:"data"`
	}
	u, _ := url.Parse("https://api.bilibili.com/x/polymer/web-dynamic/v1/opus/feed/space?host_mid=9670054")
	if err := bilibiliAPI(ctx, getter, u, &first); err != nil || first.Code != 0 || !first.Data.HasMore || first.Data.Offset == "" || len(first.Data.Items) == 0 {
		t.Fatalf("first opus page: %+v %v", first.Data, err)
	}
	q := u.Query()
	q.Set("offset", first.Data.Offset)
	u.RawQuery = q.Encode()
	if err := bilibiliAPI(ctx, getter, u, &second); err != nil || second.Code != 0 || len(second.Data.Items) == 0 || second.Data.Items[0].ID == first.Data.Items[0].ID {
		t.Fatalf("second opus page: %+v %v", second.Data, err)
	}
	var albums [2]struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				ID       string `json:"dyn_id"`
				Pictures []struct {
					URL string `json:"img_src"`
				} `json:"pictures"`
			} `json:"items"`
		} `json:"data"`
	}
	for page := range albums {
		u, _ = url.Parse(fmt.Sprintf("https://api.bilibili.com/x/dynamic/feed/draw/doc_list?uid=9670054&page_num=%d&page_size=30", page))
		if err := bilibiliAPI(ctx, getter, u, &albums[page]); err != nil || albums[page].Code != 0 || len(albums[page].Data.Items) == 0 || len(albums[page].Data.Items[0].Pictures) == 0 {
			t.Fatalf("album page %d: %+v %v", page, albums[page].Data, err)
		}
	}
	if albums[0].Data.Items[0].ID == albums[1].Data.Items[0].ID {
		t.Fatal("album pagination returned the same first item")
	}
}
