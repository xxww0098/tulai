package strategy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/cdp"
)

// 千问2 真实页面：回复只有「请求异常，请检查网络连接」和重新生成图标，没有图卡。
// 检测要直接报 error-reply，而不是 no-new-image 等到超时。
// 豆包新对话：长连接让 readyState 停在 interactive，空对话加上输入框就该能打字。
func TestReplyFaultAndDoubaoReady(t *testing.T) {
	ws, shutdown := launchChromeArgs(t, []string{"--blink-settings=imagesEnabled=false"})
	defer shutdown()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	hook := mustRead(t, filepath.Join("js", "hook.js"))
	composer := mustRead(t, filepath.Join("js", "composer.js"))
	pages := map[string]string{
		"request-error": staticFixtureKeepJSON(mustRead(t, filepath.Join("testdata", "qianwen", "error", "request-error.html"))),
		"chat":          `<!doctype html><html><body><div id="root"><textarea data-testid="chat_input_input" style="width:300px;height:40px"></textarea></div></body></html>`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[strings.Trim(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	t.Run("qianwen request error", func(t *testing.T) {
		sess := openFixture(t, client, srv.URL+"/request-error", hook, composer)
		defer sess.Close()
		live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if live.Reason != "error-reply" || live.Busy || len(live.Images) != 0 || !strings.Contains(live.Fault, "请求异常") {
			t.Fatalf("报错回复要单独认出 reason=%s busy=%t images=%d fault=%q", live.Reason, live.Busy, len(live.Images), live.Fault)
		}
		ok := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var box = document.querySelector('[class*="retry-container"]');
			box.parentNode.removeChild(box);
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if ok.Reason == "error-reply" {
			t.Fatal("没有报错时不能判成回复异常")
		}
	})

	t.Run("doubao empty chat while loading", func(t *testing.T) {
		sess := openFixture(t, client, srv.URL+"/chat", hook, composer)
		defer sess.Close()
		if !sess.EvaluateBool(`window.__tulaiPageHost='www.doubao.com'; Object.defineProperty(document, 'readyState', { get: function () { return 'interactive'; } }); (() => { var st = __tulaiChatReady(); return st.ready === true && st.why === '' && st.messages === 0; })()`) {
			out, _ := sess.Evaluate(`JSON.stringify(__tulaiChatReady())`)
			t.Fatalf("空对话有输入框就该能打字: %s", out)
		}
		if !sess.EvaluateBool(`(() => { var el = document.querySelector('textarea'); el.parentNode.removeChild(el); var st = __tulaiChatReady(); return st.ready === false && st.why.indexOf('loading') >= 0; })()`) {
			out, _ := sess.Evaluate(`JSON.stringify(__tulaiChatReady())`)
			t.Fatalf("还没有输入框、文档没加载完不能算准备好: %s", out)
		}
	})
}
