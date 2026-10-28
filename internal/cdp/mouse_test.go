package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/xxww0098/tulai/internal/browser"
)

func TestTrustedPointerClickUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("隔离可信 pointerdown E2E")
	}
	chrome, err := browser.FindChromePath()
	if err != nil {
		t.Skip("无本地 Chrome")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><html><body>
<button id="open">添加附件</button><button id="disabled" disabled>禁用</button><input type="file" id="active" style="display:none">
<script>
window.events=[];window.changes=0;
document.querySelector('#open').addEventListener('pointerdown',event=>{
 if(!event.isTrusted)return;
 const menu=document.createElement('button');menu.id='menu-item';menu.textContent='上传图片';menu.onclick=()=>document.querySelector('#active').click();document.body.append(menu);
});
document.addEventListener('pointerdown',event=>{if(event.target.tagName==='BUTTON')window.events.push(['pointerdown',event.target.id,event.isTrusted])});
document.addEventListener('click',event=>{if(event.target.tagName==='BUTTON')window.events.push(['click',event.target.id,event.isTrusted])});
document.querySelector('#active').addEventListener('change',()=>window.changes++);
</script></body></html>`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	alloc, stop := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome))...)
	defer stop()
	client := &Client{alloc: alloc}
	s, err := client.NewSession(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Evaluate(`document.querySelector('#open').click()`); err != nil || s.EvaluateBool(`!!document.querySelector('#menu-item')`) {
		t.Fatal("fixture must require a trusted pointerdown")
	}
	if _, err := s.Evaluate(`window.events=[];window.__tulaiBaseInstalled=2;window.tulaiClick=async()=>{window.__oldClick=true;return true}`); err != nil {
		t.Fatal(err)
	}
	s.EnsureBase()
	file := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(file, []byte("trusted pointer fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.UploadFiles(`(async()=>{if(!await tulaiClick(document.querySelector('#open')))throw new Error('入口点击失败');if(!await tulaiClick('#menu-item'))throw new Error('菜单点击失败')})()`, []string{file}); err != nil {
		t.Fatal(err)
	}
	if !s.EvaluateBool(`!window.__oldClick&&window.__tulaiBaseInstalled===3&&window.changes===1&&document.querySelector('#active').files.length===1&&JSON.stringify(window.events)===JSON.stringify([['pointerdown','open',true],['click','open',true],['pointerdown','menu-item',true],['click','menu-item',true]])`) {
		out, _ := s.Evaluate(`JSON.stringify({events:window.events,changes:window.changes,old:window.__oldClick})`)
		t.Fatalf("trusted single-click/upload sequence failed: %s", out)
	}
	if err := s.Click("#missing"); err == nil {
		t.Fatal("missing click target succeeded")
	}
	if err := s.Click("#disabled"); err == nil {
		t.Fatal("disabled click target succeeded")
	}
}
