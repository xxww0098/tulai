package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"xbs/internal/browser"
)

func TestUploadFilesChooser(t *testing.T) {
	if testing.Short() {
		t.Skip("隔离文件选择器 E2E")
	}
	for _, backend := range []string{"Chrome", "Ego"} {
		t.Run(backend, func(t *testing.T) {
			if backend == "Ego" && os.Getenv("XBS_EGO_E2E") != "1" {
				t.Skip("需显式 XBS_EGO_E2E=1；仅访问隔离 localhost")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<!doctype html><html><body>
<input type="file" id="old-first"><div id="composer"><button id="open" onclick="document.querySelector('#active').click()">附件</button></div>
<input type="file" id="old-second"><div id="portal"><input type="file" id="active" style="display:none"></div>
<script>window.changes=[];document.querySelectorAll('input').forEach(input=>input.addEventListener('change',()=>window.changes.push(input.id)))</script>
</body></html>`))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			var s *Session
			if backend == "Ego" {
				var err error
				s, err = NewEgoSession(ctx, server.URL, nil)
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
					origin, _ := json.Marshal(server.URL)
					cmd, cleanup, err := egoCommand(cleanupCtx, path, `const task=await taskSpace("小帮手");for(const tab of await task.tabs()){if(tab.label&&new URL(tab.url).origin===`+string(origin)+`)await task.page(tab.label).close()}await task.handOff();`)
					if err != nil {
						t.Error(err)
						return
					}
					defer cleanup()
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Errorf("清理本地选择器页: %v: %s", err, output)
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
				if err := chromedp.Run(tab, chromedp.Navigate(server.URL)); err != nil {
					t.Fatal(err)
				}
				s = &Session{ctx: tab}
			}
			file := filepath.Join(t.TempDir(), "测试 图 % !.png")
			if err := os.WriteFile(file, []byte("local chooser fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := s.UploadFiles(`(()=>{throw new Error('fixture入口失败')})()`, []string{file}); err == nil {
				t.Fatal("broken entry action did not fail")
			}
			if backend == "Ego" {
				if err := s.ego.request(s.ctx, "chooseUpload", map[string]any{"openJS": "void 0", "files": []string{file}, "timeout": 100}, nil); err == nil || s.ctx.Err() != nil {
					t.Fatalf("chooser timeout should retain its controller: %v", err)
				}
			} else {
				shortCtx, stop := context.WithTimeout(s.ctx, 100*time.Millisecond)
				short := &Session{ctx: shortCtx}
				err := short.UploadFiles(`void 0`, []string{file})
				stop()
				if err == nil {
					t.Fatal("cancelled chooser succeeded")
				}
			}
			if err := s.UploadFiles(`document.querySelector('#open').click()`, []string{file}); err != nil {
				t.Fatalf("upload after failed entry: %v", err)
			}
			if !s.EvaluateBool(fmt.Sprintf(`document.querySelector('#old-first').files.length===0&&document.querySelector('#old-second').files.length===0&&document.querySelector('#active').files[0]?.name===%q&&JSON.stringify(window.changes)==='["active"]'`, filepath.Base(file))) {
				t.Fatal("chooser selected an old input, damaged the path, or emitted duplicate change")
			}
			if backend == "Ego" {
				old := s
				timer := time.AfterFunc(100*time.Millisecond, old.cancel)
				err := old.UploadFiles(`void 0`, []string{file})
				timer.Stop()
				old.Close()
				if err == nil {
					t.Fatal("cancelled chooser succeeded")
				}
				resumed, err := NewEgoSession(ctx, server.URL, nil)
				if err != nil {
					t.Fatal(err)
				}
				s = resumed
				if _, err := s.Evaluate(`document.querySelector('#active').value='';window.changes=[]`); err != nil {
					t.Fatal(err)
				}
				if err := s.UploadFiles(`document.querySelector('#open').click()`, []string{file}); err != nil || !s.EvaluateBool(`JSON.stringify(window.changes)==='["active"]'`) {
					t.Fatalf("upload after cancelled controller: %v", err)
				}
			}
		})
	}
}
