package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEgoSDKLocalPage(t *testing.T) {
	if testing.Short() || os.Getenv("TULAI_EGO_E2E") != "1" {
		t.Skip("需显式 TULAI_EGO_E2E=1；仅官方 SDK localhost 验收")
	}
	scratch := egoTestTempDir(t)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewNRGBA(image.Rect(0, 0, 64, 40))); err != nil {
		t.Fatal(err)
	}
	slowDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/artifact" || r.URL.Path == "/slow" {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", `attachment; filename="fixture.png"`)
			w.Write(imageData.Bytes())
			if r.URL.Path == "/slow" {
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
				case <-slowDone:
				}
			}
			return
		}
		w.Write([]byte(`<!doctype html><html><body><h1>official SDK fixture</h1>
<textarea id="prompt"></textarea><div id="editor" contenteditable="true"></div>
<input type="file" id="old"><button id="click">click</button><button id="nothing">no chooser</button>
<button id="attach" onclick="window.attachClicks++;const menu=document.querySelector('#menu');menu.hidden=!menu.hidden;menu.style.display=menu.hidden?'none':'contents'">附件</button>
<div id="menu" role="menu" hidden><button role="menuitem" id="document" onclick="window.documentClicks++">上传文档</button><button role="menuitem" id="local" onclick="window.imageClicks++;document.querySelector('#active').click()"><span>上传图片</span><span>最多上传10张，每张不超过20MB</span></button></div><input type="file" id="active" hidden>
<a id="download" href="/artifact" download="fixture.png">下载图片</a><a id="slow" href="/slow" download="slow.png">slow download</a>
<script>window.clicks=0;window.changes=0;window.attachClicks=0;window.imageClicks=0;window.documentClicks=0;document.querySelector('#click').onclick=event=>{if(event.isTrusted)window.clicks++};document.querySelector('#active').onchange=()=>window.changes++;</script></body></html>`))
	}))
	defer func() { close(slowDone); server.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	s, err := NewEgoSession(ctx, server.URL, func(line string) { t.Log(line) })
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
			t.Errorf("清理 SDK 本地页: %v: %s", err, output)
		}
	}()
	if !s.IsEgo() {
		t.Fatal("Ego session not identified")
	}
	if snapshot, err := s.EgoSnapshot(); err != nil || !strings.Contains(snapshot, "official SDK fixture") {
		t.Fatalf("SDK snapshot: %s, %v", snapshot, err)
	}
	for _, selector := range []string{"#prompt", "#editor"} {
		if err := s.EgoFill(selector, "官方 SDK 中文提示词"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.EgoClick("#click"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(scratch, "中文 空格 %! 源图.png")
	if err := os.WriteFile(file, imageData.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.EgoUpload([]string{"#nothing"}, "", []string{file}); err == nil || s.Ctx().Err() != nil {
		t.Fatalf("chooser timeout should be recoverable: %v", err)
	}
	uploadSelector := `[role="menu"] [role="menuitem"]:has-text("上传图片")`
	t.Logf("selector compatibility: %s", uploadSelector)
	if err := s.EgoUpload([]string{"#attach", uploadSelector}, "", []string{file}); err != nil {
		t.Fatalf("SDK upload after timeout: %v", err)
	}
	var state struct {
		Prompt, Editor, File                                                       string
		Clicks, Changes, Old, AttachClicks, ImageClicks, DocumentClicks, MenuWidth int
	}
	if err := s.EgoEvaluate(`({prompt:document.querySelector('#prompt').value,editor:document.querySelector('#editor').textContent,file:document.querySelector('#active').files[0]?.name,clicks:window.clicks,changes:window.changes,old:document.querySelector('#old').files.length,attachClicks:window.attachClicks,imageClicks:window.imageClicks,documentClicks:window.documentClicks,menuWidth:document.querySelector('#menu').getBoundingClientRect().width})`, &state); err != nil || state.Prompt != "官方 SDK 中文提示词" || state.Editor != state.Prompt || state.File != filepath.Base(file) || state.Clicks != 1 || state.Changes != 1 || state.Old != 0 || state.AttachClicks != 1 || state.ImageClicks != 1 || state.DocumentClicks != 0 || state.MenuWidth != 0 {
		t.Fatalf("SDK interaction state: %+v, %v", state, err)
	}
	if data, err := s.EgoDownload("#download", 10*time.Second); err != nil || !bytes.Equal(data, imageData.Bytes()) {
		t.Fatalf("SDK attachment bytes: %d, %v", len(data), err)
	}
	old := s
	timer := time.AfterFunc(150*time.Millisecond, old.cancel)
	_, err = old.EgoDownload("#slow", 10*time.Second)
	timer.Stop()
	old.Close()
	if err == nil {
		t.Fatal("canceled saveAs succeeded")
	}
	resumed, err := NewEgoSession(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = resumed
	if data, err := s.EgoDownload("#download", 10*time.Second); err != nil || !bytes.Equal(data, imageData.Bytes()) {
		t.Fatalf("SDK download after canceled controller: %d, %v", len(data), err)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "tulai-ego-download-") {
			t.Fatalf("private SDK download directory remained: %s", entry.Name())
		}
	}
	t.Log("官方 SDK 正常、失败重试、取消重试与临时目录断言完成")
}

func TestEgoSDKDivMenuLocalPage(t *testing.T) {
	if testing.Short() || os.Getenv("TULAI_EGO_E2E") != "1" {
		t.Skip("需显式 TULAI_EGO_E2E=1；仅 DIV 菜单 localhost 验收")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><html><body><h1>DIV menu SDK fixture</h1>
<div id="editor" contenteditable="true" role="textbox">fixture</div>
<button id="attach" aria-label="添加附件">添加附件</button>
<div id="menu" role="menu" tabindex="-1" hidden>
<div id="document" role="menuitem" tabindex="-1"><span>上传文档</span><span>最多上传文件</span></div>
<div id="local" role="menuitem" tabindex="-1"><span>上传图片</span><span>最多上传10张，每张不超过20MB</span></div></div>
<input id="active" type="file" hidden><input id="old" type="file"><button id="outside">外部</button>
<script>
window.trace=[];window.changes=0;window.confirmations=0;const menu=document.querySelector('#menu'),attach=document.querySelector('#attach');
function record(event){trace.push(event.type+':'+(event.key||'')+':'+event.target.id+'->'+(event.relatedTarget?.id||'none'))}
for(const type of ['focusin','focusout','pointerdown','mousedown','click','keydown'])document.addEventListener(type,record,true);
attach.onclick=()=>{menu.hidden=!menu.hidden;if(!menu.hidden)menu.focus()};
attach.addEventListener('keydown',event=>{if(event.isTrusted&&event.key==='ArrowDown'){event.preventDefault();menu.hidden=false;document.querySelector('#document').focus()}});
menu.addEventListener('focusout',event=>{if(!menu.contains(event.relatedTarget)){trace.push('close:focusout');menu.hidden=true}});
document.addEventListener('pointerdown',event=>{if(!menu.hidden&&!menu.contains(event.target)&&event.target!==attach){trace.push('close:outside');menu.hidden=true}});
document.querySelector('#local').onclick=()=>document.querySelector('#active').click();
menu.addEventListener('keydown',event=>{
 if(!event.isTrusted)return;
 if(event.key==='ArrowDown'){const items=Array.from(menu.querySelectorAll('[role="menuitem"]'));items[(items.indexOf(document.activeElement)+1)%items.length].focus();event.preventDefault()}
 if(event.key==='Enter'&&document.activeElement.id==='local'){window.confirmations++;document.querySelector('#active').click();event.preventDefault()}
});
document.querySelector('#active').onchange=()=>window.changes++;
</script></body></html>`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var s *Session
	defer func() {
		if s != nil {
			s.Close()
		}
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
			t.Errorf("清理 DIV 菜单本地页: %v: %s", err, output)
		}
	}()
	var err error
	s, err = NewEgoSession(ctx, server.URL, func(line string) { t.Log(line) })
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(egoTestTempDir(t), "DIV 菜单源图.png")
	if err := os.WriteFile(file, []byte("localhost chooser fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Log("official keyboard compatibility: DIV tabindex=-1 ArrowDown → ArrowDown → Enter")
	err = s.egoSDKRequest("sdkUpload", map[string]any{"selectors": []string{"#attach"}, "menuLabel": "上传图片", "files": []string{file}, "timeout": 5000}, nil, 8*time.Second)
	var state struct {
		File, Active                string
		Changes, Old, Confirmations int
		Hidden                      bool
		Trace                       []string
	}
	stateErr := s.EgoEvaluate(`({file:document.querySelector('#active').files[0]?.name||'',active:document.activeElement.id,changes:window.changes,old:document.querySelector('#old').files.length,confirmations:window.confirmations,hidden:document.querySelector('#menu').hidden,trace:window.trace})`, &state)
	t.Logf("DIV menu state=%+v uploadError=%v readError=%v", state, err, stateErr)
	if err != nil || stateErr != nil || state.File != filepath.Base(file) || state.Changes != 1 || state.Old != 0 || state.Confirmations != 1 {
		if snapshot, snapshotErr := s.EgoSnapshot(); snapshotErr == nil {
			t.Logf("localhost-only snapshot: %s", snapshot)
		}
		t.Fatalf("DIV menu SDK upload failed: %v; state=%+v; read=%v", err, state, stateErr)
	}
}
