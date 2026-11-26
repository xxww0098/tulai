package strategy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
)

// TestUploadAndPromptSpeedE2E verifies fast, trusted prompt filling and image uploading
// for Doubao (ProseMirror, Textarea, 1-img and 10-img batch) and Qwen (Slate.js, direct ref button,
// and Radix dropdown upload) inside the managed Fingerprint browser.
func TestUploadAndPromptSpeedE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skip real browser speed verification in -short mode")
	}
	client := newFixtureClient(t)

	longPrompt := DoubaoWashPrompt(
		"保持商品主体结构、材质纹理、logo与配色完全一致，重新构图拍摄棚拍商业主图，自然柔和侧逆光，浅灰微纹理背景，主体居中留白适中，高清锐利细节，去除原图杂乱背景与多余水印，增强布料质感与立体光影层次，避免畸变与额外文字。",
		[]string{
			"tulai-a1b2c3d4e5f60708-01.png",
			"tulai-a1b2c3d4e5f60708-02.png",
			"tulai-a1b2c3d4e5f60708-03.png",
			"tulai-a1b2c3d4e5f60708-04.png",
			"tulai-a1b2c3d4e5f60708-05.png",
			"tulai-a1b2c3d4e5f60708-06.png",
			"tulai-a1b2c3d4e5f60708-07.png",
			"tulai-a1b2c3d4e5f60708-08.png",
			"tulai-a1b2c3d4e5f60708-09.png",
			"tulai-a1b2c3d4e5f60708-10.png",
		},
	)
	medPrompt := "保持商品主体结构与配色一致，商业棚拍质感，柔和自然光，干净极简背景，高清细节。\n画面比例自然，避免畸变与多余文字。\n构图居中，主体突出。"

	html := `<!doctype html><html><head><meta charset="utf-8"><style>
	body { font-family: sans-serif; padding: 20px; }
	.composer-box { width: 520px; border: 1px solid #ccc; padding: 12px; }
	textarea, [contenteditable] { width: 480px; min-height: 80px; border: 1px solid #999; padding: 6px; display: block; white-space: pre-wrap; }
	.hidden { display: none; }
	#radix-menu { display: none; width: 160px; height: 80px; border: 1px solid #aaa; background: #fff; }
	</style></head><body>

	<div id="doubao-box" class="guidance-input-surface composer-box" data-testid="chat_input">
		<div id="doubao-previews"></div>
		<div data-testid="chat_input_input">
			<div id="pm-editor" class="tiptap ProseMirror" contenteditable="true"><p><br class="ProseMirror-trailingBreak"></p></div>
		</div>
		<div id="doubao-upload-wrap">
			<input id="doubao-file" data-testid="upload-file-input" class="hidden" multiple type="file" accept=".png,.jpeg,.jpg,.webp">
			<button id="doubao-upload-btn" data-testid="upload_file_button" type="button">上传</button>
		</div>
		<button id="doubao-send" data-testid="chat_input_send_button" type="button">发送</button>
	</div>

	<div id="ta-box" class="composer-box">
		<textarea id="ta-editor"></textarea>
	</div>

	<div id="qwen-box" class="composer-box" data-chat-input-body="1">
		<div id="qwen-previews"></div>
		<div id="slate-editor" data-slate-editor="true" data-slate-node="value" contenteditable="true"><p data-slate-node="element"><span data-slate-zero-width="n"><br></span></p></div>
		<button id="qwen-attach-btn" aria-label="添加附件" aria-haspopup="menu" type="button">+</button>
		<button id="qwen-ref-btn" aria-label="参考图" type="button">参考图</button>
		<div id="radix-menu" role="menu" data-state="closed"><button id="qwen-menu-upload" role="menuitem" type="button">上传图片</button></div>
		<button id="qwen-send" data-session-switch-target="send-query" type="button">发送</button>
	</div>

	<script>
	window.__events = { input: [], change: [], clicks: [] };
	function track(el, name) {
		el.addEventListener('input', e => window.__events.input.push({ name, trusted: e.isTrusted }));
	}
	const pm = document.getElementById('pm-editor');
	const ta = document.getElementById('ta-editor');
	const slate = document.getElementById('slate-editor');
	track(pm, 'pm');
	track(ta, 'ta');
	track(slate, 'slate');

	// Realistic Slate.js Shift+Enter block split with zero-width <br> anchor
	slate.addEventListener('keydown', e => {
		if (e.key === 'Enter' && e.shiftKey) {
			e.preventDefault();
			const p = document.createElement('p');
			p.setAttribute('data-slate-node', 'element');
			p.innerHTML = '<span data-slate-zero-width="n"><br></span>';
			slate.appendChild(p);
			const r = document.createRange();
			r.setStart(p, 0);
			r.collapse(true);
			const sel = window.getSelection();
			sel.removeAllRanges();
			sel.addRange(r);
		}
	});

	const doubaoFile = document.getElementById('doubao-file');
	document.getElementById('doubao-upload-btn').addEventListener('click', () => doubaoFile.click());
	doubaoFile.addEventListener('change', e => {
		window.__events.change.push({ name: 'doubao', trusted: e.isTrusted, count: doubaoFile.files.length });
		const prev = document.getElementById('doubao-previews');
		prev.innerHTML = '';
		for (const f of doubaoFile.files) {
			const card = document.createElement('div');
			card.setAttribute('data-testid', 'attachment-image-card');
			card.setAttribute('aria-label', f.name);
			card.style.cssText = 'width:48px;height:48px;display:inline-block;background:#eee;';
			const img = document.createElement('img');
			img.width = 48; img.height = 48;
			img.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aQ1sAAAAASUVORK5CYII=';
			card.appendChild(img);
			prev.appendChild(card);
		}
	});

	const menu = document.getElementById('radix-menu');
	document.getElementById('qwen-attach-btn').addEventListener('pointerdown', e => {
		if (!e.isTrusted) return;
		menu.style.display = 'block';
		menu.setAttribute('data-state', 'open');
	});
	function openQwenDynamicFile() {
		menu.style.display = 'none';
		menu.setAttribute('data-state', 'closed');
		const inp = document.createElement('input');
		inp.type = 'file';
		inp.multiple = true;
		inp.style.display = 'none';
		inp.addEventListener('change', e => {
			window.__events.change.push({ name: 'qwen', trusted: e.isTrusted, count: inp.files.length });
			const prev = document.getElementById('qwen-previews');
			prev.innerHTML = '';
			for (const f of inp.files) {
				const img = document.createElement('img');
				img.width = 48; img.height = 48;
				img.src = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aQ1sAAAAASUVORK5CYII=';
				prev.appendChild(img);
			}
			inp.remove();
		});
		document.body.appendChild(inp);
		inp.click();
	}
	document.getElementById('qwen-menu-upload').addEventListener('click', openQwenDynamicFile);
	document.getElementById('qwen-ref-btn').addEventListener('click', openQwenDynamicFile);
	</script></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	}))
	defer srv.Close()

	sess, err := client.NewIsolatedSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := (Doubao{}).InstallHook(sess); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	files := make([]string, 10)
	pngBytes := noisePNG(64)
	for i := range files {
		p := filepath.Join(tmpDir, fmt.Sprintf("img-%02d.png", i+1))
		if err := os.WriteFile(p, pngBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		files[i] = p
	}

	// 1. FreshChat ready page
	t0 := time.Now()
	if err := FreshChat(sess); err != nil {
		t.Fatal(err)
	}
	dFresh := time.Since(t0)
	t.Logf("[AFTER] FreshChat (already ready): %v", dFresh)

	// 2. Prompt filling: ProseMirror (Doubao)
	t0 = time.Now()
	if err := sess.InsertText("#pm-editor", longPrompt); err != nil {
		t.Fatal(err)
	}
	dPM := time.Since(t0)
	gotPM, _ := sess.Evaluate(`window.tulaiReadComposer(document.getElementById('pm-editor'))`)
	if !composerTextMatches(longPrompt, unquoteJS(gotPM)) {
		t.Fatalf("ProseMirror prompt mismatch: got=%q", unquoteJS(gotPM))
	}
	if !sess.EvaluateBool(`window.__events.input.filter(e=>e.name==='pm').at(-1)?.trusted === true`) {
		t.Fatal("ProseMirror input event was not trusted")
	}
	t.Logf("[AFTER] Doubao ProseMirror (584r, 13L): %v", dPM)

	// 3. Prompt filling: Textarea (Doubao)
	t0 = time.Now()
	if err := sess.InsertText("#ta-editor", longPrompt); err != nil {
		t.Fatal(err)
	}
	dTA := time.Since(t0)
	gotTA, _ := sess.Evaluate(`window.tulaiReadComposer(document.getElementById('ta-editor'))`)
	if !composerTextMatches(longPrompt, unquoteJS(gotTA)) {
		t.Fatalf("Textarea prompt mismatch")
	}
	t.Logf("[AFTER] Doubao Textarea (584r, 13L): %v", dTA)

	// 4. Prompt filling: Slate.js (Qwen)
	t0 = time.Now()
	if err := sess.InsertText("#slate-editor", medPrompt); err != nil {
		t.Fatal(err)
	}
	dSlateMed := time.Since(t0)
	gotSlateMed, _ := sess.Evaluate(`window.tulaiReadComposer(document.getElementById('slate-editor'))`)
	if !composerTextMatches(medPrompt, unquoteJS(gotSlateMed)) {
		t.Fatalf("Slate med prompt mismatch: got=%q", unquoteJS(gotSlateMed))
	}
	t0 = time.Now()
	if err := sess.InsertText("#slate-editor", longPrompt); err != nil {
		t.Fatal(err)
	}
	dSlateLong := time.Since(t0)
	gotSlateLong, _ := sess.Evaluate(`window.tulaiReadComposer(document.getElementById('slate-editor'))`)
	if !composerTextMatches(longPrompt, unquoteJS(gotSlateLong)) {
		t.Fatalf("Slate long prompt mismatch: got=%q", unquoteJS(gotSlateLong))
	}
	if !sess.EvaluateBool(`window.__events.input.filter(e=>e.name==='slate').at(-1)?.trusted === true`) {
		t.Fatal("Slate input event was not trusted")
	}
	t.Logf("[AFTER] Qwen Slate.js: med(68r,3L)=%v, long(584r,13L)=%v", dSlateMed, dSlateLong)

	// 5. Image upload: Doubao 1-img & 10-img (via base.UploadFiles — chooseUpload + tulaiOpenUpload + tulaiWaitForUpload)
	t0 = time.Now()
	if err := (base{}).UploadFiles(sess, files[:1]); err != nil {
		t.Fatal(err)
	}
	dDoubao1 := time.Since(t0)

	t0 = time.Now()
	if err := (base{}).UploadFiles(sess, files); err != nil {
		t.Fatal(err)
	}
	dDoubao10 := time.Since(t0)
	t.Logf("[AFTER] Doubao UploadFiles: 1-img=%v, 10-img=%v", dDoubao1, dDoubao10)

	// 6. Image upload: Qwen direct 参考图 & Radix 添加附件
	_, _ = sess.Evaluate(`document.getElementById('doubao-box').remove(); document.getElementById('ta-box').remove();`)
	t0 = time.Now()
	if err := (base{}).UploadFiles(sess, files[:1]); err != nil {
		t.Fatal(err)
	}
	dQwenRef := time.Since(t0)

	_, _ = sess.Evaluate(`document.getElementById('qwen-ref-btn').remove(); document.getElementById('qwen-previews').innerHTML='';`)
	t0 = time.Now()
	if err := (base{}).UploadFiles(sess, files[:1]); err != nil {
		t.Fatal(err)
	}
	dQwenMenu := time.Since(t0)
	t.Logf("[AFTER] Qwen UploadFiles: direct参考图=%v, Radix添加附件=%v", dQwenRef, dQwenMenu)

	// 7. Single tulaiClick latency
	t0 = time.Now()
	if err := sess.Click("#qwen-send"); err != nil {
		t.Fatal(err)
	}
	dClick := time.Since(t0)
	t.Logf("[AFTER] Single tulaiClick: %v", dClick)

	// Assert generous speed regression guards (well below old multi-second baseline)
	if dPM > 500*time.Millisecond || dTA > 500*time.Millisecond || dSlateLong > 500*time.Millisecond {
		t.Fatalf("prompt filling regressed: pm=%v ta=%v slate=%v", dPM, dTA, dSlateLong)
	}
	if dDoubao1 > 500*time.Millisecond || dDoubao10 > 500*time.Millisecond || dQwenRef > 500*time.Millisecond || dQwenMenu > 500*time.Millisecond {
		t.Fatalf("upload trigger regressed: d1=%v d10=%v qRef=%v qMenu=%v", dDoubao1, dDoubao10, dQwenRef, dQwenMenu)
	}
	if dClick > 200*time.Millisecond {
		t.Fatalf("tulaiClick regressed: %v", dClick)
	}
}

func unquoteJS(s string) string {
	var out string
	if err := cdp.ParseJSValue(s, &out); err == nil {
		return out
	}
	return s
}
