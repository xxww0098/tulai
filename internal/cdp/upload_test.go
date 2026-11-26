package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadFilesChooser(t *testing.T) {
	if testing.Short() {
		t.Skip("隔离文件选择器 E2E")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><html><body>
<input type="file" id="old-first"><div id="composer"><button id="open" onclick="document.querySelector('#active').click()">附件</button></div>
<input type="file" id="old-second"><div id="portal"><input type="file" id="active" style="display:none"></div>
<script>window.changes=[];document.querySelectorAll('input').forEach(input=>input.addEventListener('change',()=>window.changes.push(input.id)))</script>
</body></html>`))
	}))
	defer server.Close()
	s := newFixtureSession(t, server.URL)
	file := filepath.Join(t.TempDir(), "测试 图 % !.png")
	if err := os.WriteFile(file, []byte("local chooser fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.UploadFiles(`(()=>{throw new Error('fixture入口失败')})()`, []string{file}); err == nil {
		t.Fatal("broken entry action did not fail")
	}
	shortCtx, stop := context.WithTimeout(s.ctx, 100*time.Millisecond)
	short := &Session{ctx: shortCtx, client: s.client, target: s.target}
	err := short.UploadFiles(`void 0`, []string{file})
	stop()
	if err == nil {
		t.Fatal("cancelled chooser succeeded")
	}
	if err := s.UploadFiles(`document.querySelector('#open').click()`, []string{file}); err != nil {
		t.Fatalf("upload after failed entry: %v", err)
	}
	if !s.EvaluateBool(fmt.Sprintf(`document.querySelector('#old-first').files.length===0&&document.querySelector('#old-second').files.length===0&&document.querySelector('#active').files[0]?.name===%q&&JSON.stringify(window.changes)==='["active"]'`, filepath.Base(file))) {
		t.Fatal("chooser selected an old input, damaged the path, or emitted duplicate change")
	}
}
