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

	"xbs/internal/browser"
)

func TestTrustedInputAndSingleUploadChange(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过 Chrome 输入 E2E")
	}
	chrome, err := browser.FindChromePath()
	if err != nil {
		t.Skip("本机无 Chrome")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<!doctype html><html><body>
<input id="text" value="old text"><input id="email" type="email" value="old@example.com"><input id="number" type="number" value="123"><textarea id="area">old text</textarea>
<div id="editor" contenteditable="true"><b>old</b> text</div>
<input id="disabled" disabled><textarea id="readonly" readonly>readonly</textarea>
<div id="aria-disabled" contenteditable="true" aria-disabled="true">disabled</div><div id="aria-readonly" contenteditable="true" aria-readonly="true">readonly</div><div id="plain">plain</div><input id="checkbox" type="checkbox">
<input id="upload" type="file"><button id="send" disabled>send</button>
<script>
window.inputEvents=[];window.changes=0;
document.addEventListener('input',event=>{window.inputEvents.push({id:event.target.id,trusted:event.isTrusted});if(event.target.id==='editor'&&event.isTrusted)document.querySelector('#send').disabled=false});
document.querySelector('#upload').addEventListener('change',()=>window.changes++);
</script></body></html>`))
	}))
	defer server.Close()
	alloc, stop := chromedp.NewExecAllocator(context.Background(), append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome))...)
	defer stop()
	ctx, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatal(err)
	}
	s := &Session{ctx: ctx}
	if _, err := s.Evaluate(`window.__xbsBaseInstalled=true;window.__xbsSeq=7;window.__keptPending=window.__xbsPending={marker:'retained'};window.xbsUnmarkFileInput=()=>{window.__oldUnmark=true}`); err != nil {
		t.Fatal(err)
	}
	if err := s.InjectBase(); err != nil {
		t.Fatal(err)
	}
	if !s.EvaluateBool(`window.__xbsBaseInstalled===3&&window.__xbsPending===window.__keptPending&&window.__xbsPending.marker==='retained'&&window.__xbsSeq===7`) {
		t.Fatal("base upgrade did not replace old revision while preserving pending calls")
	}
	if _, err := s.Evaluate(`window.__xbsBaseInstalled=true;window.xbsUnmarkFileInput=()=>{window.__oldUnmark=true}`); err != nil {
		t.Fatal(err)
	}
	s.EnsureBase()
	if !s.EvaluateBool(`window.__xbsBaseInstalled===3`) {
		t.Fatal("EnsureBase did not upgrade the old boolean marker")
	}
	for _, tc := range []struct{ selector, text string }{
		{"#text", "新的输入"},
		{"#email", "new@example.com"},
		{"#number", "456"},
		{"#area", "全文替换\n第二行"},
		{"#editor", "可信的提示词\n第二行"},
		{"#editor", ""},
	} {
		if err := s.InsertText(tc.selector, tc.text); err != nil {
			t.Fatal(err)
		}
		var state struct {
			Text    string
			Trusted bool
		}
		out, err := s.Evaluate(`(()=>{const e=document.activeElement;return {text:e.value??e.innerText,trusted:window.inputEvents.at(-1)?.trusted}})()`)
		if err != nil || ParseJSValue(out, &state) != nil || state.Text != tc.text || !state.Trusted {
			t.Fatalf("%s did not receive trusted full replacement: %s, %v", tc.selector, out, err)
		}
	}
	if !s.EvaluateBool(`!document.querySelector('#send').disabled`) {
		t.Fatal("trusted editor input did not enable send")
	}
	for _, selector := range []string{"#missing", "#disabled", "#readonly", "#aria-disabled", "#aria-readonly", "#plain", "#checkbox"} {
		if err := s.InsertText(selector, "must not insert"); err == nil {
			t.Fatalf("%s should return an input error", selector)
		}
	}
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte("upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUploadFilesNth(0, []string{path}); err != nil {
		t.Fatal(err)
	}
	if changes := s.EvaluateInt(`window.changes`); changes != 1 {
		t.Fatalf("one upload generated %d change events, want 1", changes)
	}
	if s.EvaluateBool(`window.__oldUnmark===true`) {
		t.Fatal("the stale upload helper survived base upgrade")
	}
}
