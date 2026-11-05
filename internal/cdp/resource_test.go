package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResourceRejectsNonHTTP(t *testing.T) {
	for _, raw := range []string{"file:///tmp/private.png", "data:image/png;base64,AA==", "javascript:alert(1)", "http:///missing"} {
		if _, err := (&Session{}).LoadResource(raw); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestCanceledResourceClosesTemporaryPage(t *testing.T) {
	if testing.Short() {
		t.Skip("隔离浏览器资源下载 E2E")
	}
	started := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			once.Do(func() { close(started) })
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		fmt.Fprint(w, "<!doctype html><title>resource fixture</title>")
	}))
	defer srv.Close()
	s := newFixtureSession(t, srv.URL)
	pages := func() map[string]bool {
		t.Helper()
		var result struct {
			Targets []struct {
				ID string `json:"targetId"`
			} `json:"targetInfos"`
		}
		if err := s.client.proc.Call(s.ctx, "", "Target.getTargets", nil, &result); err != nil {
			t.Fatal(err)
		}
		ids := make(map[string]bool, len(result.Targets))
		for _, target := range result.Targets {
			ids[target.ID] = true
		}
		return ids
	}
	before := pages()
	ctx, cancel := context.WithCancel(s.ctx)
	finished := make(chan error, 1)
	go func() {
		_, err := s.LoadResourceContext(ctx, srv.URL+"/slow")
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("资源请求未开始")
	}
	extra := 0
	for id := range pages() {
		if before[id] {
			continue
		}
		extra++
		var result struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
		}
		check, stop := context.WithTimeout(s.ctx, 5*time.Second)
		err := s.client.proc.Call(check, id, "Runtime.evaluate", map[string]any{"expression": "window.opener === null"}, &result)
		stop()
		if err != nil || !result.Result.Value {
			cancel()
			t.Fatalf("资源页面不应访问原页面: %v", err)
		}
	}
	if extra != 1 {
		cancel()
		t.Fatalf("资源请求应只有一个临时页面，实际 %d", extra)
	}
	cancel()
	if err := <-finished; err == nil {
		t.Fatal("取消资源请求后没有返回错误")
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(pages()) != len(before) {
		if time.Now().After(deadline) {
			t.Fatal("取消后临时资源页面没有关闭")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got, err := s.Evaluate("document.title"); err != nil || !strings.Contains(got, "resource fixture") {
		t.Fatalf("取消资源请求影响了原页面: %q, %v", got, err)
	}
}

func TestLoadResourceCrossOrigin(t *testing.T) {
	if testing.Short() {
		t.Skip("隔离浏览器资源下载 E2E")
	}
	for _, backend := range []string{"Camoufox", "Ego"} {
		t.Run(backend, func(t *testing.T) {
			if backend == "Ego" && os.Getenv("TULAI_EGO_E2E") != "1" {
				t.Skip("需显式 TULAI_EGO_E2E=1；仅访问隔离 localhost")
			}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
				t.Fatal(err)
			}
			const signedQuery = "sig=a%2Fb%2Bc%3D&expires=7"
			resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect" {
					http.Redirect(w, r, "/result.png?"+signedQuery, http.StatusFound)
					return
				}
				cookie, err := r.Cookie("tulai_resource_fixture")
				if r.URL.Path != "/result.png" || r.URL.RawQuery != signedQuery || err != nil || cookie.Value != "logged-in" {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Write(encoded.Bytes())
			}))
			defer resource.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "tulai_resource_fixture", Value: "logged-in", Path: "/", HttpOnly: true})
				w.Write([]byte("<!doctype html><html><body>isolated cross-origin download fixture</body></html>"))
			}))
			defer source.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			var s *Session
			if backend == "Ego" {
				var err error
				s, err = NewEgoSession(ctx, source.URL, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					s.Close()
					cleanupCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
					defer stop()
					path, err := egoExecutable()
					if err != nil {
						t.Error(err)
						return
					}
					origin, _ := json.Marshal(source.URL)
					cmd, cleanup, err := egoCommand(cleanupCtx, path, `const task=await taskSpace("小帮手");for(const tab of await task.tabs()){if(tab.label&&new URL(tab.url).origin===`+string(origin)+`)await task.page(tab.label).close()}await task.handOff();`)
					if err != nil {
						t.Error(err)
						return
					}
					defer cleanup()
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Errorf("本地页清理/释放空间失败: %v: %s", err, output)
					}
				}()
			} else {
				s = newFixtureSession(t, source.URL)
			}
			url := resource.URL + "/result.png?" + signedQuery
			out, err := s.EvaluateAsync(fmt.Sprintf(`fetch(%q,{credentials:'include'}).then(()=>false,error=>error instanceof TypeError)`, url))
			if err != nil || out != "true" {
				t.Fatalf("fixture did not reproduce CORS TypeError: %s, %v", out, err)
			}
			data, err := s.LoadResource(url)
			if err != nil || !bytes.Equal(data, encoded.Bytes()) {
				t.Fatalf("browser resource bytes differ: bytes=%d error=%v", len(data), err)
			}
			if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err != nil || cfg.Width != 32 || cfg.Height != 24 {
				t.Fatalf("returned image did not decode: %+v, %v", cfg, err)
			}
			if data, err := s.LoadResource(resource.URL + "/redirect"); err != nil || !bytes.Equal(data, encoded.Bytes()) {
				t.Fatalf("redirected resource bytes differ: bytes=%d error=%v", len(data), err)
			}
			if backend == "Camoufox" {
				var wg sync.WaitGroup
				for range 2 {
					wg.Go(func() {
						if data, err := s.LoadResource(url); err != nil || !bytes.Equal(data, encoded.Bytes()) {
							t.Errorf("concurrent resource bytes differ: bytes=%d error=%v", len(data), err)
						}
					})
				}
				wg.Wait()
			}
			if data, err := s.LoadResource(resource.URL + "/denied"); err == nil || !strings.Contains(err.Error(), "HTTP 403") || data != nil {
				t.Fatalf("HTTP failure hidden: bytes=%d error=%v", len(data), err)
			}
		})
	}
}
