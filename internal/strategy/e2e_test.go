package strategy

// 端到端测试：本地 mock 页面 + 无头 Chrome，验证真实链路
//   - TestChatFlowE2E：composer 打开上传 → 文件注入 → 发送 → hook 截获结果 → 抓字节
//   - TestCaptchaGridE2E：验证码检测 → 截图 → free CV 求解 → 拟人拖拽 → 验证通过
// 无 Chrome 或 -short 时跳过。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
)

// ---- 测试用平台（共享 base，指向 mock 页） ----

type mockChat struct{ base }

func (mockChat) Platform() string { return "mock" }
func (mockChat) HomeURL() string  { return "" } // 由测试注入 URL

// launchChrome 启动无头 Chrome，返回关闭函数。
func launchChrome(t *testing.T) (wsURL string, shutdown func()) {
	t.Helper()
	chromePath, err := findChrome()
	if err != nil {
		t.Skip("本机无 Chrome，跳过 E2E")
	}
	port := freePort(t)
	dir := t.TempDir()
	cmd := exec.Command(chromePath,
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+dir,
		"--headless=new", "--disable-gpu", "--no-first-run",
		"--remote-allow-origins=*", "--window-size=1280,900",
		"about:blank")
	if err := cmd.Start(); err != nil {
		t.Skipf("Chrome 启动失败: %v", err)
	}
	shutdown = func() {
		// 先正常退出，避免缓存子进程在 TempDir 清理时继续写入。
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			cmd.Process.Kill()
		}
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}
	// 轮询 CDP
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			var v struct {
				WS string `json:"webSocketDebuggerUrl"`
			}
			buf := &bytes.Buffer{}
			buf.ReadFrom(resp.Body)
			resp.Body.Close()
			if jsonDecode(buf.Bytes(), &v) == nil && v.WS != "" {
				return v.WS, shutdown
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	shutdown()
	t.Fatal("CDP 未就绪")
	return
}

func findChrome() (string, error) {
	for _, p := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"google-chrome", "chromium",
	} {
		if strings.Contains(p, "/") {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		} else if full, err := exec.LookPath(p); err == nil {
			return full, nil
		}
	}
	return "", fmt.Errorf("chrome 未找到")
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func jsonDecode(b []byte, v any) error { return json.Unmarshal(b, v) }

// noisePNG 生成一张 >20KB 的随机噪声图（触发 hook 的网络图片阈值）。
func noisePNG(size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	rnd := rand.New(rand.NewSource(42))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, color.RGBA{uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), uint8(rnd.Intn(256)), 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// makeSourcePNG 生成一张纯色小图作源图。
func makeSourcePNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.RGBA{90, 140, 220, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "源图A.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// chatMockHTML 豆包风格 mock 聊天页：composer + 附件按钮 + 发送 → 生成结果图。
const chatMockHTML = `<!doctype html><html><head><meta charset="utf-8"><style>
body{font-family:sans-serif;margin:20px}
#messages img{width:300px;height:300px;display:block;margin:6px 0}
#bar{margin-top:20px;display:flex;gap:8px;align-items:flex-start}
textarea{width:420px;height:60px}
</style></head><body>
<div id="messages"></div>
<div id="compose" data-chat-input-body="1">
<div id="bar">
  <button id="plus" aria-label="添加附件" style="width:36px;height:36px">+</button>
  <textarea id="composer" placeholder="输入消息"></textarea>
  <button id="send">发送</button>
</div>
<div id="preview"></div>
</div>
<script>
window.__mockFiles = 0; window.__mockSent = '';
document.getElementById('plus').addEventListener('click', function () {
  var input = document.getElementById('activeUpload');
  if (input) { input.click(); return; }
  input = document.createElement('input'); input.id='activeUpload';
  input.type = 'file'; input.accept = 'image/*'; input.multiple = true;
  input.style.display = 'none';
  input.addEventListener('change', function () {
    window.__mockFiles = input.files.length;
    var prev = document.getElementById('preview');
    prev.textContent = '已选择 ' + input.files.length + ' 个文件';
    Array.from(input.files).forEach(function(file){var img=document.createElement('img');img.width=120;img.height=120;img.src=URL.createObjectURL(file);prev.appendChild(img)});
  });
  document.body.appendChild(input);
  input.click();
});
document.getElementById('send').addEventListener('click', function () {
  var composer = document.getElementById('composer');
  var text = String(composer.value === undefined ? composer.textContent : composer.value).trim();
  if (!text || !window.__mockFiles) return;
  window.__mockSent = text;
  document.getElementById('composer').value = '';
  setTimeout(function () {
    fetch('/result.png').then(function (r) { return r.blob(); }).then(function (b) {
      var img = document.createElement('img');
      img.src = URL.createObjectURL(b);
      document.getElementById('messages').appendChild(img);
      window.__mockGenerated = true;
    });
  }, 400);
});
</script></body></html>`

func TestChatFlowE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()

	resultPNG := noisePNG(400)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, chatMockHTML)
		case "/result.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(resultPNG)
		}
	}))
	defer srv.Close()

	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	strat := mockChat{}
	if err := strat.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	src := makeSourcePNG(t)

	// 1) 上传：打开附件入口 → 注入文件
	if err := strat.UploadFiles(sess, []string{src}); err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if n := sess.EvaluateInt("window.__mockFiles"); n != 1 {
		t.Fatalf("mock 收到文件数 = %d", n)
	}
	// 2) 标记 + 发送
	if err := strat.MarkTask(sess); err != nil {
		t.Fatal(err)
	}
	if err := strat.SendPrompt(sess, "把这张图变成油画风格"); err != nil {
		t.Fatal(err)
	}
	got := sessEvaluateString(t, sess, "window.__mockSent")
	if !strings.Contains(got, "油画风格") {
		t.Fatalf("mock 发送内容: %q", got)
	}
	// 3) 等结果（hook 截获）
	results, err := strat.WaitForResult(sess, 20*time.Second)
	if err != nil {
		t.Fatalf("等待结果失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("结果为空")
	}
	// 4) 抓字节并校验
	fetched := 0
	for _, r := range results {
		data, err := strat.FetchResultBytes(sess, r.URL)
		if err != nil || !bytes.Equal(data, resultPNG) {
			continue
		}
		fetched++
	}
	if fetched == 0 {
		t.Fatalf("截获 %d 条结果但无一条与 mock 结果字节一致", len(results))
	}
	t.Logf("E2E 聊天链路通过：截获 %d 条结果", len(results))
}

func TestUploadTrustedPointerDownE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	html := `<!doctype html><body><div data-chat-input-body="1"><textarea style="width:420px;height:60px"></textarea><button id="entry" aria-label="添加附件">+</button><button>发送</button><div id="menu" role="menu" style="display:none;width:160px;height:80px"><button id="upload">本地上传</button></div></div><script>
window.__trustedPointer=0;window.__entryClicks=0;window.__uploadClicks=0;
entry.onpointerdown=function(event){if(event.isTrusted){window.__trustedPointer++;menu.style.display='block'}};
entry.onclick=function(){window.__entryClicks++};upload.onclick=function(){window.__uploadClicks++};
</script>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, html) }))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := (mockChat{}).InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.EvaluateAsync(`tulaiOpenUpload()`); err != nil {
		t.Fatal(err)
	}
	if sess.EvaluateInt("window.__trustedPointer") != 1 || sess.EvaluateInt("window.__entryClicks") != 1 || sess.EvaluateInt("window.__uploadClicks") != 1 {
		t.Fatal("Radix风格入口需要一次可信pointerdown与click，随后点击本次上传菜单一次")
	}
}

func TestUploadMenuClicksOnceE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	html := `<!doctype html><body><div data-chat-input-body="1"><textarea style="width:420px;height:60px"></textarea><button id="entry" aria-label="添加附件">+</button><button>发送</button><div id="menu" role="menu" style="display:none;width:160px;height:80px"><button id="upload">本地上传</button></div></div><script>
window.__entryClicks=0;window.__uploadClicks=0;
entry.onclick=function(){window.__entryClicks++;menu.style.display=menu.style.display==='none'?'block':'none'};
upload.onclick=function(){window.__uploadClicks++;var input=document.createElement('input');input.type='file';input.style.display='none';document.body.appendChild(input);menu.style.display='none';input.click()};
</script>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, html) }))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := (mockChat{}).InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if err := sess.UploadFiles(`tulaiOpenUpload()`, []string{makeSourcePNG(t)}); err != nil {
		t.Fatal(err)
	}
	if sess.EvaluateInt("window.__entryClicks") != 1 || sess.EvaluateInt("window.__uploadClicks") != 1 || !sess.EvaluateBool(`document.querySelector('input[type=file]').files.length===1`) {
		t.Fatal("上传菜单入口和上传动作各只能点击一次，文件只交给本次chooser")
	}
}

func TestUploadDoesNotClickHistoryE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	html := `<!doctype html><body><button id="history">本地上传 私密历史会话</button><div data-chat-input-body="1"><textarea style="width:420px;height:60px"></textarea><button aria-label="添加附件">+</button><button>发送</button></div><script>window.__historyClicks=0;history.onclick=function(){window.__historyClicks++};</script>`
	html = strings.Replace(html, "history.onclick", "document.getElementById('history').onclick", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, html) }))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := (mockChat{}).InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	out, err := sess.EvaluateAsync(`tulaiOpenUpload()`)
	if err != nil {
		t.Fatal(err)
	}
	if sess.EvaluateInt("window.__historyClicks") != 0 || strings.Contains(out, "私密历史会话") {
		t.Fatalf("不能把聊天历史作为上传菜单点击或记录: %s", out)
	}
}

func TestUploadTargetsCurrentComposerE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Replace(strings.Replace(chatMockHTML, `data-chat-input-body="1"`, `data-chat-input-body=""`, 1), `<button id="send">发送</button>`, `<button id="send" aria-label="语音">麦克风</button>`, 1))
	}))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	strat := mockChat{}
	if err := strat.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Evaluate(`var stale=document.createElement('input');stale.type='file';stale.accept='.pdf,.txt';stale.style.display='none';stale.id='stale';document.body.prepend(stale);document.getElementById('plus').setAttribute('data-testid','upload_file_button');var comp=document.getElementById('composer'),inner=document.createElement('div');inner.className='guidance-input-surface';comp.before(inner);inner.appendChild(comp);var other=document.createElement('button');other.setAttribute('aria-label','添加附件');window.__otherUploadClicks=0;other.onclick=function(){window.__otherUploadClicks++};inner.appendChild(other);`); err != nil {
		t.Fatal(err)
	}
	if err := strat.UploadFiles(sess, []string{makeSourcePNG(t)}); err != nil {
		t.Fatal(err)
	}
	if !sess.EvaluateBool(`window.__mockFiles===1 && document.getElementById('stale').files.length===0 && window.__otherUploadClicks===0 && window.__tulaiUploadState.scope===document.getElementById('compose')`) {
		t.Fatal("不应将源图上传到旧的全局file input")
	}
}

func TestUploadSiblingPreviewScopeE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	html := strings.Replace(chatMockHTML, `<div id="compose" data-chat-input-body="1">`, `<div id="surface" class="guidance-input-surface relative flex flex-col"><div id="preview"></div><div id="compose" data-chat-input-body="">`, 1)
	html = strings.Replace(html, "<div id=\"preview\"></div>\n</div>", "</div></div>", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, html) }))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	strat := mockChat{}
	if err := strat.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Evaluate(`var historyImage=document.createElement('img');historyImage.width=300;historyImage.height=300;historyImage.src='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aQ1sAAAAASUVORK5CYII=';document.getElementById('messages').appendChild(historyImage);var historyButton=document.createElement('button');historyButton.setAttribute('aria-label','删除历史图片');window.__historyClicks=0;historyButton.onclick=function(){window.__historyClicks++};document.getElementById('messages').appendChild(historyButton);`); err != nil {
		t.Fatal(err)
	}
	if err := sess.UploadFiles(`tulaiOpenUpload()`, []string{makeSourcePNG(t)}); err != nil {
		t.Fatal(err)
	}
	if !sess.EvaluateBool(`window.__tulaiUploadState.scope===document.getElementById('surface')&&!window.__tulaiUploadState.scope.contains(document.getElementById('messages'))`) {
		t.Fatal("附件应定位到包含当前入口的真实surface，不能扩到历史区域")
	}
	out, err := sess.EvaluateAsync(`tulaiWaitForUpload(1)`)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Ready bool `json:"ready"`
	}
	if err := cdp.ParseJSValue(out, &state); err != nil {
		t.Fatal(err)
	}
	if !state.Ready {
		t.Fatal("编辑器上层兄弟预览应计入当前上传附件")
	}
	if sess.EvaluateInt("window.__historyClicks") != 0 {
		t.Fatal("不能点击历史图片控件")
	}
}

func TestUploadRequiresNewComposerAttachmentE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Replace(chatMockHTML, "document.body.appendChild(input);", "input.addEventListener('change',function(){document.getElementById('preview').replaceChildren()});document.body.appendChild(input);", 1))
	}))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	strat := mockChat{}
	if err := strat.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Evaluate(`var historyImg=document.createElement('img');historyImg.src='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aQ1sAAAAASUVORK5CYII=';historyImg.width=200;historyImg.height=200;document.getElementById('messages').appendChild(historyImg);`); err != nil {
		t.Fatal(err)
	}
	if err := sess.UploadFiles(`tulaiOpenUpload()`, []string{makeSourcePNG(t)}); err != nil {
		t.Fatal(err)
	}
	out, err := sess.EvaluateAsync(`Promise.race([tulaiWaitForUpload(1).then(function(v){return JSON.stringify({finished:true,value:v})}),new Promise(function(resolve){setTimeout(function(){resolve(JSON.stringify({pending:true}))},300)})])`)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Pending bool `json:"pending"`
	}
	if err := cdp.ParseJSValue(out, &state); err != nil {
		t.Fatal(err)
	}
	if !state.Pending {
		t.Fatal("没有当前composer附件时不能因历史图片已加载而报上传成功")
	}
}

func TestFetchResultBytesCORSE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	imageData := noisePNG(220)
	asset := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("tulai-fixture-auth")
		if err != nil || cookie.Value != "allowed" || r.URL.RawQuery != "token=fixture%2Fsignature" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(imageData)
	}))
	defer asset.Close()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "tulai-fixture-auth", Value: "allowed", HttpOnly: true, Path: "/"})
		fmt.Fprint(w, "<!doctype html><body>fixture</body>")
	}))
	defer page.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession(page.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	data, err := (mockChat{}).FetchResultBytes(sess, asset.URL+"/output.png?token=fixture%2Fsignature")
	if err != nil || !bytes.Equal(data, imageData) {
		t.Fatalf("页面CORS失败应由当前浏览器登录态取回签名图: %v", err)
	}
	if _, err := (mockChat{}).FetchResultBytes(sess, asset.URL+"/output.png?token=invalid"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("浏览器资源读取不能隐藏真实HTTP403: %v", err)
	}
}

func TestSignedStreamResultURLE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	resultPNG := noisePNG(220)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, "<!doctype html><body></body>")
		case "/chat/stream":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"url":%q}`, "http://"+r.Host+"/result.png?token=fixture%2Fsignature&transport="+r.URL.Query().Get("transport"))
		case "/result.png":
			if r.URL.Query().Get("token") != "fixture/signature" {
				http.Error(w, "missing signature", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(resultPNG)
		}
	}))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	strat := mockChat{}
	if err := strat.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Evaluate(`window.__installedFetch=window.fetch`); err != nil {
		t.Fatal(err)
	}
	if err := strat.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if !sess.EvaluateBool(`window.fetch === window.__installedFetch && !!window.__tulaiHookRevision`) {
		t.Fatal("同一版hook重复安装不应叠加fetch包装")
	}
	for _, transport := range []string{"fetch", "xhr"} {
		t.Run(transport, func(t *testing.T) {
			if err := strat.MarkTask(sess); err != nil {
				t.Fatal(err)
			}
			script := `fetch('/chat/stream?transport=fetch').then(function(r){return r.text()})`
			if transport == "xhr" {
				script = `new Promise(function(resolve){var x=new XMLHttpRequest(); x.open('GET','/chat/stream?transport=xhr'); x.onload=function(){resolve(true)}; x.send()})`
			}
			if _, err := sess.EvaluateAsync(script); err != nil {
				t.Fatal(err)
			}
			results, err := strat.WaitForResult(sess, 4*time.Second)
			want := srv.URL + "/result.png?token=fixture%2Fsignature&transport=" + transport
			if err != nil || len(results) != 1 || results[0].URL != want {
				t.Fatalf("签名URL丢失: %v, %v, want %s", results, err, want)
			}
			data, err := strat.FetchResultBytes(sess, results[0].URL)
			if err != nil || !bytes.Equal(data, resultPNG) {
				t.Fatalf("带签名图片抓取失败: %v", err)
			}
		})
	}
	if _, err := strat.FetchResultBytes(sess, srv.URL+"/result.png?token=invalid"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("无效签名应明确返回HTTP状态: %v", err)
	}
}

func TestWaitForGenerationStreamE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	preview := noisePNG(384)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, "<!doctype html><body></body>")
		case "/preview.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(preview)
		case "/chat/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, ": generating\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-time.After(2500 * time.Millisecond):
				fmt.Fprintf(w, `data: {"url":%q}`, "http://"+r.Host+"/full.png?transport="+r.URL.Query().Get("transport"))
			case <-r.Context().Done():
			}
		}
	}))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, transport := range []string{"fetch", "xhr"} {
		t.Run(transport, func(t *testing.T) {
			sess, err := client.NewSession(srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			strat := mockChat{}
			if err := strat.InstallHook(sess); err != nil {
				t.Fatal(err)
			}
			if err := strat.MarkTask(sess); err != nil {
				t.Fatal(err)
			}
			request := `fetch('/chat/stream?transport=fetch').catch(function(){})`
			if transport == "xhr" {
				request = `var xhr=new XMLHttpRequest();xhr.open('GET','/chat/stream?transport=xhr');xhr.send()`
			}
			if _, err := sess.Evaluate(request + `;var img=document.createElement('img');img.width=384;img.height=384;img.src='/preview.png';document.body.appendChild(img);true`); err != nil {
				t.Fatal(err)
			}
			logStart := len(logging.Log.Entries())
			results, err := strat.WaitForResult(sess, 6*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			want := srv.URL + "/full.png?transport=" + transport
			found := false
			for _, result := range results {
				if result.URL == want {
					found = true
				}
			}
			pendingLogs := 0
			for _, entry := range logging.Log.Entries()[logStart:] {
				if strings.Contains(entry.Text, "候选已出现") {
					pendingLogs++
				}
			}
			if pendingLogs != 1 {
				t.Fatalf("本轮pending状态应只记录一次: %d", pendingLogs)
			}
			if !found {
				t.Fatalf("DOM预览不能使未读完的生成流提前返回: %#v", results)
			}
		})
	}
}

func TestGenerationPendingLifecycleE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	small := noisePNG(32)
	oldImage := noisePNG(128)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, "<!doctype html><body></body>")
		case "/chat/hang":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, ": pending\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/chat/stale":
			w.Header().Set("Content-Type", "application/json")
			select {
			case <-time.After(250 * time.Millisecond):
				fmt.Fprintf(w, `{"url":%q}`, "http://"+r.Host+"/stale.png")
			case <-r.Context().Done():
			}
		case "/pictures/stale.png":
			w.Header().Set("Content-Type", "image/png")
			select {
			case <-time.After(250 * time.Millisecond):
				w.Write(oldImage)
			case <-r.Context().Done():
			}
		case "/small.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(small)
		}
	}))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, tc := range []struct{ name, action string }{
		{"fetch reject", `fetch('http://127.0.0.1:1/chat/rejected').catch(function(){})`},
		{"fetch abort", `var control=new AbortController();fetch('/chat/hang',{signal:control.signal}).catch(function(){});setTimeout(function(){control.abort()},50)`},
		{"xhr abort", `var xhr=new XMLHttpRequest();xhr.open('GET','/chat/hang');xhr.send();setTimeout(function(){xhr.abort()},50)`},
		{"xhr error", `var xhr=new XMLHttpRequest();xhr.open('GET','http://127.0.0.1:1/chat/rejected');xhr.send()`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess, err := client.NewSession(srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			strat := mockChat{}
			if err := strat.InstallHook(sess); err != nil {
				t.Fatal(err)
			}
			if err := strat.MarkTask(sess); err != nil {
				t.Fatal(err)
			}
			if _, err := sess.EvaluateAsync(`(async function(){` + tc.action + `;await new Promise(function(resolve){setTimeout(resolve,200)});return true})()`); err != nil {
				t.Fatal(err)
			}
			if sess.EvaluateInt(`window.__tulaiPendingGeneration()`) != 0 {
				t.Fatal("拒绝或中止必须释放本轮生成计数")
			}
		})
	}
	for _, transport := range []string{"fetch", "xhr", "image fetch", "image xhr"} {
		t.Run("old "+transport+" response", func(t *testing.T) {
			sess, err := client.NewSession(srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			strat := mockChat{}
			if err := strat.InstallHook(sess); err != nil {
				t.Fatal(err)
			}
			action := `window.__oldDone=fetch('/chat/stale').then(function(resp){return resp.text()})`
			if transport == "xhr" {
				action = `window.__oldDone=new Promise(function(resolve){var xhr=new XMLHttpRequest();xhr.open('GET','/chat/stale');xhr.onload=function(){resolve(true)};xhr.send()})`
			}
			if transport == "image fetch" {
				action = `window.__oldDone=fetch('/pictures/stale.png').then(function(resp){return resp.arrayBuffer()})`
			}
			if transport == "image xhr" {
				action = `window.__oldDone=new Promise(function(resolve){var xhr=new XMLHttpRequest();xhr.open('GET','/pictures/stale.png');xhr.responseType='blob';xhr.onload=function(){resolve(true)};xhr.send()})`
			}
			if _, err := sess.Evaluate(action); err != nil {
				t.Fatal(err)
			}
			if err := strat.MarkTask(sess); err != nil {
				t.Fatal(err)
			}
			if _, err := sess.EvaluateAsync(`window.__oldDone.then(function(){return new Promise(function(resolve){setTimeout(resolve,50)})})`); err != nil {
				t.Fatal(err)
			}
			if sess.EvaluateInt(`window.__tulaiResultsSince().length`) != 0 || sess.EvaluateInt(`window.__tulaiPendingGeneration()`) != 0 {
				t.Fatal("Mark前旧响应不能以完成时新时间戳进入本轮结果")
			}
		})
	}
	t.Run("generation reset", func(t *testing.T) {
		sess, err := client.NewSession(srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		strat := mockChat{}
		if err := strat.InstallHook(sess); err != nil {
			t.Fatal(err)
		}
		if err := strat.MarkTask(sess); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Evaluate(`var oldControl=new AbortController();fetch('/chat/hang',{signal:oldControl.signal}).catch(function(){});true`); err != nil {
			t.Fatal(err)
		}
		if err := strat.MarkTask(sess); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.EvaluateAsync(`(async function(){window.newControl=new AbortController();fetch('/chat/hang',{signal:newControl.signal}).catch(function(){});oldControl.abort();await new Promise(function(resolve){setTimeout(resolve,150)});return true})()`); err != nil {
			t.Fatal(err)
		}
		if sess.EvaluateInt(`__tulaiPendingGeneration()`) != 1 {
			t.Fatal("旧代次完成不能释放新任务的pending")
		}
		if _, err := sess.EvaluateAsync(`(async function(){newControl.abort();await new Promise(function(resolve){setTimeout(resolve,150)});return true})()`); err != nil {
			t.Fatal(err)
		}
		if sess.EvaluateInt(`__tulaiPendingGeneration()`) != 0 {
			t.Fatal("新任务中止应释放本轮pending")
		}
	})
	t.Run("old request and stable small DOM", func(t *testing.T) {
		sess, err := client.NewSession(srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		strat := mockChat{}
		if err := strat.InstallHook(sess); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Evaluate(`fetch('/chat/hang').catch(function(){});true`); err != nil {
			t.Fatal(err)
		}
		if err := strat.MarkTask(sess); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Evaluate(`var img=document.createElement('img');img.width=200;img.height=200;img.src='/small.png';document.body.appendChild(img);true`); err != nil {
			t.Fatal(err)
		}
		results, err := strat.WaitForResult(sess, 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != 1 || results[0].URL != srv.URL+"/small.png" {
			t.Fatal("任务前旧流不应阻塞稳定的小尺寸DOM-only结果")
		}
	})
	t.Run("total timeout", func(t *testing.T) {
		sess, err := client.NewSession(srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		strat := mockChat{}
		if err := strat.InstallHook(sess); err != nil {
			t.Fatal(err)
		}
		if err := strat.MarkTask(sess); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Evaluate(`fetch('/chat/hang').catch(function(){});var img=document.createElement('img');img.width=200;img.height=200;img.src='/small.png';document.body.appendChild(img);true`); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_, err = strat.WaitForResult(sess, 200*time.Millisecond)
		if err == nil || time.Since(start) > time.Second {
			t.Fatalf("未完成响应必须遵守总等待窗口: %v", err)
		}
	})
}

func TestWaitForResultStopsWithSessionE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession("about:blank", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	done := make(chan error, 1)
	go func() {
		_, err := (mockChat{}).WaitForResult(sess, time.Minute)
		done <- err
	}()
	sess.Close()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("等待结果取消错误 = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("会话结束后等待结果未及时退出")
	}
}

func TestSendPromptE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, chatMockHTML)
	}))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, setup string
		wantError   bool
	}{
		{"button once", `window.__sendCount=0; window.__enterCount=0;
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;document.getElementById('composer').value=''});
			document.getElementById('composer').addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, false},
		{"trusted editable", `window.__sendCount=0; window.__enterCount=0;
			var hidden=document.createElement('textarea'); hidden.style.display='none'; document.body.prepend(hidden);
			var old=document.getElementById('composer'), rich=document.createElement('div'); rich.id='composer'; rich.contentEditable='true'; rich.style.cssText='width:420px;height:60px;border:1px solid'; rich.innerHTML='<p>保留编辑器结构</p>'; old.replaceWith(rich);
			var send=document.getElementById('send'); send.disabled=true; send.setAttribute('aria-label','发送消息'); send.innerHTML='<svg width="20" height="20"></svg>';
			window.__historyDeleted=0; window.__attachmentRemoved=0;
			var historyButton=document.createElement('button'); historyButton.setAttribute('aria-label','删除历史'); historyButton.onclick=function(){window.__historyDeleted++}; document.body.prepend(historyButton);
			var attachment=document.createElement('button'); attachment.setAttribute('aria-label','移除附件'); attachment.onclick=function(){window.__attachmentRemoved++}; document.getElementById('bar').appendChild(attachment);
			rich.addEventListener('input',function(e){if(e.isTrusted)send.disabled=false});
			rich.addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});
			send.addEventListener('click',function(){window.__sendCount++;rich.textContent=''});`, false},
		{"outer send sibling", `window.__sendCount=0;window.__enterCount=0;
			var comp=document.getElementById('composer'), inner=document.createElement('div'); inner.setAttribute('data-chat-input-body','1'); comp.before(inner); inner.appendChild(comp);
			var more=document.createElement('button'); more.textContent='更多'; inner.appendChild(more);
			var send=document.getElementById('send'); send.setAttribute('aria-label','发送消息'); send.innerHTML='<svg width="20" height="20"></svg>';
			send.addEventListener('click',function(){window.__sendCount++;comp.value=''});
			comp.addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, false},
		{"doubao icon send", `window.__sendCount=0;window.__enterCount=0;
			var comp=document.getElementById('composer'), send=document.getElementById('send'); send.id='flow-end-msg-send'; send.setAttribute('data-testid','chat_input_send_button'); send.innerHTML='<svg class="size-18 !text-g-send-msg-btn-text" width="20" height="20"></svg>';
			send.addEventListener('click',function(){window.__sendCount++;comp.value=''});
			comp.addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, false},
		{"existing rich same prompt", `window.__sendCount=0;window.__enterCount=0;
			var old=document.getElementById('composer'), rich=document.createElement('div');rich.id='composer';rich.contentEditable='true';rich.style.cssText='width:420px;height:60px';rich.innerHTML='<p>只发送一次</p>';old.replaceWith(rich);
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;window.__observedText=rich.innerText;rich.textContent=''});
			rich.addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, false},
		{"hidden connected editor", `window.__sendCount=0;window.__enterCount=0;var old=document.getElementById('composer');
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;old.style.visibility='hidden';var fresh=document.createElement('div');fresh.contentEditable='true';fresh.style.cssText='width:420px;height:60px';fresh.textContent='\uFEFF';old.before(fresh)});
			old.addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, false},
		{"hidden placeholder", `window.__sendCount=0;window.__enterCount=0;var old=document.getElementById('composer'),rich=document.createElement('div');rich.id='composer';rich.contentEditable='true';rich.style.cssText='width:420px;height:60px';old.replaceWith(rich);
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;rich.innerHTML='\uFEFF<span style="display:none">隐藏占位文本</span>'});
			rich.addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, false},
		{"slow accepted", `window.__sendCount=0;window.__enterCount=0;var comp=document.getElementById('composer');
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;setTimeout(function(){comp.value=''},6000)});
			comp.addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, false},
		{"noneditable placeholder", `window.__sendCount=0;window.__enterCount=0;var old=document.getElementById('composer'),rich=document.createElement('div');rich.id='composer';rich.contentEditable='true';rich.style.cssText='width:420px;height:60px';old.replaceWith(rich);
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;rich.innerHTML='\uFEFF<span contenteditable="false" data-slate-placeholder="true">向千问提问</span>'});`, false},
		{"aria hidden placeholder", `window.__sendCount=0;window.__enterCount=0;var old=document.getElementById('composer'),rich=document.createElement('div');rich.id='composer';rich.contentEditable='true';rich.style.cssText='width:420px;height:60px';old.replaceWith(rich);
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;rich.innerHTML='\uFEFF<span aria-hidden="true">向千问提问</span>'});`, false},
		{"BOM empty", `window.__sendCount=0;window.__enterCount=0;var old=document.getElementById('composer'),rich=document.createElement('div');rich.id='composer';rich.contentEditable='true';rich.style.cssText='width:420px;height:60px';old.replaceWith(rich);
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;rich.textContent='\uFEFF'});`, false},
		{"user placeholder wording", `window.__sendCount=0;window.__enterCount=0;var old=document.getElementById('composer'),rich=document.createElement('div');rich.id='composer';rich.contentEditable='true';rich.style.cssText='width:420px;height:60px';old.replaceWith(rich);
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++});`, true},
		{"multiline rich value", `window.__sendCount=0;window.__enterCount=0;var old=document.getElementById('composer'),rich=document.createElement('div');rich.id='composer';rich.contentEditable='true';rich.style.cssText='width:420px;height:60px';old.replaceWith(rich);
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++;window.__observedText=rich.innerText;rich.textContent=''});`, false},
		{"duplicate editor content", `window.__sendCount=0;var comp=document.getElementById('composer');comp.addEventListener('input',function(){comp.value+=comp.value});document.getElementById('send').addEventListener('click',function(){window.__sendCount++});`, true},
		{"missing send", `document.getElementById('send').remove();
			var extra=document.createElement('button'); extra.setAttribute('aria-label','添加附件'); extra.setAttribute('data-testid','diagnostic-last'); extra.title='https://example.invalid/image?token=fixture-secret'; extra.innerHTML='<svg class=send-fixture><use href="/icon.svg?token=fixture-secret#send"></use></svg>'; document.getElementById('bar').appendChild(extra);
			var iconOnly=document.createElement('div'); iconOnly.className='send-extra'; iconOnly.id='non-button-send'; document.getElementById('bar').appendChild(iconOnly);
			var historyText=document.createElement('p'); historyText.textContent='不可输出的历史正文'; document.body.prepend(historyText);`, true},
		{"missing composer", `document.getElementById('composer').remove()`, true},
		{"input error", `document.getElementById('composer').readOnly=true`, true},
		{"button error", `document.getElementById('send').click=function(){throw new Error('send failed')}`, true},
		{"not accepted", `window.__sendCount=0;window.__enterCount=0;
			document.getElementById('send').addEventListener('click',function(){window.__sendCount++});
			document.getElementById('composer').addEventListener('keydown',function(e){if(e.key==='Enter')window.__enterCount++});`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess, err := client.NewSession(srv.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()
			if err := (mockChat{}).InstallHook(sess); err != nil {
				t.Fatal(err)
			}
			if _, err := sess.Evaluate(tc.setup); err != nil {
				t.Fatal(err)
			}
			if tc.name == "trusted editable" {
				if err := (mockChat{}).CleanComposer(sess); err != nil {
					t.Fatal(err)
				}
				if !sess.EvaluateBool(`!!document.querySelector('#composer p') && window.__historyDeleted===0 && window.__attachmentRemoved>0`) {
					t.Fatal("清理附件不应破坏编辑器或删除历史消息")
				}
			}
			logStart := len(logging.Log.Entries())
			prompt := "只发送一次"
			if tc.name == "user placeholder wording" {
				prompt = "向千问提问"
			}
			if tc.name == "multiline rich value" {
				prompt = "第一行\n第二行"
			}
			err = (mockChat{}).SendPrompt(sess, prompt)
			if (err != nil) != tc.wantError {
				t.Fatalf("SendPrompt error = %v, wantError %v", err, tc.wantError)
			}
			wantUncertain := tc.name == "button error" || tc.name == "not accepted" || tc.name == "user placeholder wording"
			if errors.Is(err, ErrSubmissionUncertain) != wantUncertain {
				t.Fatalf("只有发送已尝试后的错误应停止自动重发: err=%v wantUncertain=%t", err, wantUncertain)
			}
			if tc.name == "user placeholder wording" && (sess.EvaluateInt("window.__sendCount") != 1 || !strings.Contains(err.Error(), "平台未接受消息")) {
				t.Fatal("真实输入同名提示语时仍是正文，不能当空或跳过发送")
			}
			if tc.name == "multiline rich value" && !sess.EvaluateBool(`window.__observedText==='第一行\n第二行'`) {
				t.Fatal("语义读值不能丢失富文本换行")
			}
			if tc.name == "existing rich same prompt" && !sess.EvaluateBool(`window.__observedText==='只发送一次'`) {
				t.Fatal("已有相同提示词应被全文替换，不能重复")
			}
			if tc.name == "duplicate editor content" && sess.EvaluateInt("window.__sendCount") != 0 {
				t.Fatal("实际编辑器内容重复时必须停止，不能发送")
			}
			if tc.name == "missing send" {
				if err.Error() != "未找到发送按钮" {
					t.Fatalf("队列错误应简短: %v", err)
				}
				var diagnostic strings.Builder
				for _, entry := range logging.Log.Entries()[logStart:] {
					if entry.Level != logging.Debug || len([]rune(entry.Text)) >= 350 {
						t.Fatalf("诊断应逐条短调试日志: %v", entry)
					}
					diagnostic.WriteString(entry.Text)
				}
				text := diagnostic.String()
				if !strings.Contains(text, "composer=TEXTAREA") || !strings.Contains(text, "aria-label=添加附件") || !strings.Contains(text, "data-testid=diagnostic-last") || !strings.Contains(text, "class=send-fixture") || !strings.Contains(text, "id=non-button-send") || strings.Contains(text, "fixture-secret") || strings.Contains(text, "不可输出的历史正文") {
					t.Fatalf("发送诊断缺失或包含历史/签名数据: %s", text)
				}
			}
			if !tc.wantError && (sess.EvaluateInt("window.__sendCount") != 1 || sess.EvaluateInt("window.__enterCount") != 0) {
				t.Fatal("发送按钮成功后不应再触发回车")
			}
			if tc.name == "not accepted" && (sess.EvaluateInt("window.__sendCount") != 1 || sess.EvaluateInt("window.__enterCount") != 0) {
				t.Fatal("平台未接受消息时也不应重复发送或补发回车")
			}
		})
	}
}

func TestUploadPreviewE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()
	previewPNG, resultPNG := noisePNG(220), noisePNG(400)
	html := strings.Replace(chatMockHTML, "prev.textContent = '已选择 ' + input.files.length + ' 个文件';", `
		var busy=document.createElement('span'); busy.setAttribute('role','progressbar'); busy.textContent='上传中';
		document.getElementById('bar').appendChild(busy);
		var img=document.createElement('img'); img.id='sourcePreview'; img.width=220; img.height=220; img.src='/preview.png'; prev.appendChild(img);
		setTimeout(function(){busy.remove(); window.__uploadReady=true},250);`, 1)
	html = strings.Replace(html, "window.__mockSent = text;", `window.__mockSent = text;
		setTimeout(function(){var img=document.getElementById('sourcePreview'); img.src='/preview-variant.png';
		var attachment=document.createElement('img'); attachment.src=img.src; document.getElementById('messages').appendChild(attachment); fetch(img.src)},50);`, 1)
	html = strings.Replace(html, "fetch('/result.png').then(function (r) { return r.blob(); }).then(function (b) {", "Promise.resolve().then(function () {", 1)
	html = strings.Replace(html, "img.src = URL.createObjectURL(b);", "setTimeout(function(){img.src='/result.png'},50);", 1)
	html = strings.Replace(html, "}, 400);", "}, 2200);", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, html)
		case "/result.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(resultPNG)
		case "/preview.png", "/preview-variant.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(previewPNG)
		}
	}))
	defer srv.Close()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	strat := mockChat{}
	if err := strat.InstallHook(sess); err != nil {
		t.Fatal(err)
	}
	if err := strat.UploadFiles(sess, []string{makeSourcePNG(t)}); err != nil {
		t.Fatal(err)
	}
	if !sess.EvaluateBool("window.__uploadReady === true") {
		t.Error("上传尚未完成就返回了")
	}
	if err := strat.MarkTask(sess); err != nil {
		t.Fatal(err)
	}
	if err := strat.SendPrompt(sess, "把这张图变成油画风格"); err != nil {
		t.Fatal(err)
	}
	results, err := strat.WaitForResult(sess, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		data, err := strat.FetchResultBytes(sess, result.URL)
		if err != nil || !bytes.Equal(data, resultPNG) {
			t.Errorf("源图预览被当作生成结果: %s (%v)", result.URL, err)
		}
	}
}

// captchaMockHTML mock 九宫格验证码：9 canvas（第 5 格带条纹特征）+ 目标图 + 放置区。
// 拖第 5 格到放置区 → 通过（移除弹窗）；拖错 → 换题。
const captchaMockHTML = `<!doctype html><html><head><meta charset="utf-8"><style>
body{font-family:sans-serif;margin:30px}
#overlay{position:fixed;inset:0;background:rgba(0,0,0,.4)}
#dialog{position:fixed;left:100px;top:40px;width:260px;background:#fff;padding:16px;border-radius:8px}
#captcha_verify_image{position:relative;display:grid;grid-template-columns:repeat(3,60px);gap:2px;width:184px}
#captcha_verify_image canvas{width:60px;height:60px}
#target{position:absolute;left:-10px;top:-46px;width:40px;height:40px;border:1px solid #ccc}
#zone{margin-top:14px;width:240px;height:100px;border:2px dashed #888;line-height:100px;text-align:center;color:#666}
</style></head><body>
<div id="overlay"></div>
<div id="dialog" class="captcha-dialog">
  <p>请将符合的图案拖入下方区域</p>
  <div id="captcha_verify_image"><img id="target" alt=""></div>
  <div id="zone">拖到这里</div>
  <button id="submit" style="margin-top:10px">提交</button>
</div>
<script>
(function () {
  var answer = 4; // 第 5 格（0 基 4）
  var container = document.getElementById('captcha_verify_image');
  // 每格独立的横向渐变（模拟真实验证码的纹理照片，使边缘特征可区分）
  function cellFill(ctx, i, w, h) {
    var r1 = 20 + i * 22, g1 = 40 + i * 17, b1 = 60 + i * 13;
    var r2 = 230 - i * 15, g2 = 200 - i * 20, b2 = 180 - i * 12;
    for (var x = 0; x < w; x++) {
      var t = x / (w - 1);
      ctx.fillStyle = 'rgb(' + Math.round(r1 + (r2 - r1) * t) + ',' +
        Math.round(g1 + (g2 - g1) * t) + ',' + Math.round(b1 + (b2 - b1) * t) + ')';
      ctx.fillRect(x, 0, 1, h);
    }
  }
  function draw() {
    container.querySelectorAll('canvas').forEach(function (c) { c.remove(); });
    for (var i = 0; i < 9; i++) {
      var cv = document.createElement('canvas');
      cv.width = 60; cv.height = 60;
      var ctx = cv.getContext('2d');
      cellFill(ctx, i, 60, 60);
      // 答案格叠加答案纹理：横向细条纹（仅在答案格）
      if (i === answer) {
        ctx.fillStyle = 'rgba(255,255,255,0.5)';
        for (var y = 0; y < 60; y += 8) ctx.fillRect(0, y, 60, 2);
      }
      container.appendChild(cv);
    }
    // 目标图 = 答案格样式（同渐变 + 同条纹）
    var tcv = document.createElement('canvas'); tcv.width = 40; tcv.height = 40;
    var tctx = tcv.getContext('2d');
    cellFill(tctx, answer, 40, 40);
    tctx.fillStyle = 'rgba(255,255,255,0.5)';
    for (var y2 = 0; y2 < 40; y2 += 8) tctx.fillRect(0, y2, 40, 2);
    document.getElementById('target').src = tcv.toDataURL('image/png');
  }
  draw();
  window.__mockPassed = false;
  window.__mockEvents = [];
  var dragFrom = -1;
  container.addEventListener('pointerdown', function (e) {
    var cv = e.target.closest('canvas');
    dragFrom = cv ? Array.prototype.indexOf.call(container.querySelectorAll('canvas'), cv) : -1;
    window.__mockEvents.push({ t: 'down', from: dragFrom, x: e.clientX, y: e.clientY });
  });
  document.addEventListener('pointerup', function (e) {
    window.__mockEvents.push({ t: 'up', from: dragFrom, x: e.clientX, y: e.clientY });
    if (dragFrom < 0) return;
    var z = document.getElementById('zone').getBoundingClientRect();
    var inside = e.clientX >= z.left && e.clientX <= z.right && e.clientY >= z.top && e.clientY <= z.bottom;
    var from = dragFrom; dragFrom = -1;
    if (!inside) return;
    if (from === answer) {
      window.__mockPassed = true;
      document.getElementById('dialog').remove();
      document.getElementById('overlay').remove();
    } else {
      answer = Math.floor(Math.random() * 9); // 换题
      draw();
    }
  });
})();
</script></body></html>`

func TestCaptchaGridE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过")
	}
	ws, shutdown := launchChrome(t)
	defer shutdown()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, captchaMockHTML)
	}))
	defer srv.Close()

	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := client.NewSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// 挂真实求解链：free 后端 + 注入脚本
	captcha.Attach(sess, captcha.NewFreeSolver(captcha.Config{Backend: "free"}), true, nil)

	deadline := time.Now().Add(45 * time.Second)
	var lastDiag string
	for time.Now().Before(deadline) {
		if sess.EvaluateBool("window.__mockPassed === true") {
			t.Log("E2E 验证码链路通过：检测→截图→CV 求解→拟人拖拽→验证放行")
			return
		}
		diag := sessEvaluateString(t, sess,
			`JSON.stringify({
				events: (window.__mockEvents||[]).slice(-6),
				canvasCount: document.querySelectorAll('#captcha_verify_image canvas').length,
				status: window.__tulaiCaptcha && window.__tulaiCaptcha.status(),
			})`)
		if diag != lastDiag {
			t.Logf("诊断: %s", diag)
			lastDiag = diag
		}
		time.Sleep(500 * time.Millisecond)
	}
	status := sessEvaluateString(t, sess, "JSON.stringify(window.__tulaiCaptcha && window.__tulaiCaptcha.status())")
	t.Fatalf("验证码未在期限内通过，注入状态: %s", status)
}

func sessEvaluateString(t *testing.T, s *cdp.Session, expr string) string {
	t.Helper()
	out, err := s.Evaluate(expr)
	if err != nil {
		t.Fatalf("evaluate %s: %v", expr, err)
	}
	var v string
	if err := cdp.ParseJSValue(out, &v); err != nil {
		return out
	}
	return v
}
