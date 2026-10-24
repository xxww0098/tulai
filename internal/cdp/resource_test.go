package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	protocol "github.com/chromedp/cdproto/cdp"
	cdpio "github.com/chromedp/cdproto/io"
	"github.com/chromedp/chromedp"

	"xbs/internal/browser"
)

type resourceStreamExecutor struct {
	chunks []cdpio.ReadReturns
}

func (e *resourceStreamExecutor) Execute(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if method != cdpio.CommandRead || len(e.chunks) == 0 {
		return fmt.Errorf("unexpected stream command %s", method)
	}
	*result.(*cdpio.ReadReturns) = e.chunks[0]
	e.chunks = e.chunks[1:]
	return nil
}

func TestResourceStreamEncodingLimitAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		chunks []cdpio.ReadReturns
		limit  int
		want   string
		err    bool
	}{
		{"binary and text", []cdpio.ReadReturns{{Data: "AP8=", Base64encoded: true}, {Data: "text", EOF: true}}, 6, "\x00\xfftext", false},
		{"limit", []cdpio.ReadReturns{{Data: "abc"}, {Data: "de", EOF: true}}, 4, "", true},
		{"bad encoding", []cdpio.ReadReturns{{Data: "!", Base64encoded: true, EOF: true}}, 10, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := protocol.WithExecutor(context.Background(), &resourceStreamExecutor{chunks: tc.chunks})
			data, err := readResourceStream(ctx, "stream", tc.limit)
			if (err != nil) != tc.err || string(data) != tc.want {
				t.Fatalf("data=%q error=%v", data, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(protocol.WithExecutor(context.Background(), &resourceStreamExecutor{}))
	cancel()
	if _, err := readResourceStream(ctx, "stream", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
	if _, err := (&Session{}).LoadResource("file:///tmp/private.png"); err == nil {
		t.Fatal("non-HTTP resource accepted")
	}
}

func TestLoadResourceCrossOrigin(t *testing.T) {
	if testing.Short() {
		t.Skip("隔离浏览器资源下载 E2E")
	}
	for _, backend := range []string{"Chrome", "Ego"} {
		t.Run(backend, func(t *testing.T) {
			if backend == "Ego" && os.Getenv("XBS_EGO_E2E") != "1" {
				t.Skip("需显式 XBS_EGO_E2E=1；仅访问隔离 localhost")
			}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 32, 24))); err != nil {
				t.Fatal(err)
			}
			const signedQuery = "sig=a%2Fb%2Bc%3D&expires=7"
			resource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				cookie, err := r.Cookie("xbs_resource_fixture")
				if r.URL.Path != "/result.png" || r.URL.RawQuery != signedQuery || err != nil || cookie.Value != "logged-in" {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Write(encoded.Bytes())
			}))
			defer resource.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "xbs_resource_fixture", Value: "logged-in", Path: "/", HttpOnly: true})
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
				chrome, err := browser.FindChromePath()
				if err != nil {
					t.Skip("无本地 Chrome")
				}
				alloc, stop := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome))...)
				defer stop()
				tab, closeTab := chromedp.NewContext(alloc)
				defer closeTab()
				if err := chromedp.Run(tab, chromedp.Navigate(source.URL)); err != nil {
					t.Fatal(err)
				}
				s = &Session{ctx: tab}
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
			if data, err := s.LoadResource(resource.URL + "/denied"); err == nil || !strings.Contains(err.Error(), "HTTP 403") || data != nil {
				t.Fatalf("HTTP failure hidden: bytes=%d error=%v", len(data), err)
			}
		})
	}
}
