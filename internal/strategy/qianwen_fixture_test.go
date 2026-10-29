package strategy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/cdp"
)

func TestQianwenLiveFixtures(t *testing.T) {
	ws, shutdown := launchChromeArgs(t, []string{"--blink-settings=imagesEnabled=false"})
	defer shutdown()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	hook := mustRead(t, filepath.Join("js", "hook.js"))
	composer := mustRead(t, filepath.Join("js", "composer.js"))
	pages := map[string]string{}
	for _, name := range []string{"9338-0", "9339-0"} {
		pages[name] = staticFixture(mustRead(t, filepath.Join("testdata", "qianwen", "live", name+".html")))
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(r.URL.Path, "/")
		body, ok := pages[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	for _, name := range []string{"9338-0", "9339-0"} {
		t.Run(name, func(t *testing.T) {
			sess := openFixture(t, client, srv.URL+"/"+name, hook, composer)
			defer sess.Close()
			if !sess.EvaluateBool(`window.__tulaiPageHost='www.qianwen.com'; (() => { var st = __tulaiChatReady(); return st.ready === true && st.chars === 0 && st.messages === 0 && st.id === ''; })()`) {
				out, _ := sess.Evaluate(`JSON.stringify(__tulaiChatReady())`)
				t.Fatalf("占位文案不能挡住新对话: %s", out)
			}
			if !sess.EvaluateBool(`__tulaiFreshChat() === 'ready' && location.href.indexOf('doubao.com') < 0 && location.href.indexOf('qianwen.com') < 0`) {
				out, _ := sess.Evaluate(`location.href + ' ' + __tulaiFreshChat()`)
				t.Fatalf("空白新对话不该再开标签: %s", out)
			}
			if !sess.EvaluateBool(`(() => { var el = document.querySelector('[data-slate-editor="true"]'); el.getBoundingClientRect = function () { return { width: 0, height: 0, top: 0, left: 0 }; }; var st = __tulaiChatReady(); return st.ready === true && st.node !== ''; })()`) {
				t.Fatal("屏幕外布局盒为 0 时仍要认出千问输入框")
			}
		})
	}

	t.Run("send and original", func(t *testing.T) {
		prompt := "雪山下的小木屋，油画风格"
		sess := openFixture(t, client, srv.URL+"/9338-0", hook, composer)
		defer sess.Close()
		if !sess.EvaluateBool(`window.__tulaiPageHost='www.qianwen.com'; window.tulaiMarkComposer() && document.querySelector('[data-tulai-composer="1"]').getAttribute('data-slate-editor') === 'true'`) {
			t.Fatal("应写到 Slate 输入框，而不是别的可编辑节点")
		}
		if !sess.EvaluateBool(`window.tulaiReadComposer(document.querySelector('[data-tulai-composer="1"]')) === ''`) {
			out, _ := sess.Evaluate(`JSON.stringify(window.tulaiReadComposer(document.querySelector('[data-tulai-composer="1"]')))`)
			t.Fatalf("占位文案被读成了正文: %s", out)
		}
		sendBtn := `document.querySelector('[data-session-switch-target="send-query"]')`
		if !sess.EvaluateBool(sendBtn + ` && ` + sendBtn + `.getAttribute('aria-label') === '发送消息' && ` + sendBtn + `.disabled === true`) {
			t.Fatal("发送应是还没点亮的「发送消息」箭头")
		}
		if !sess.EvaluateBool(`window.tulaiSendAccepted(location.href, ` + quote(prompt) + `, 0) === false`) {
			t.Fatal("还没有用户气泡，不能确认发送")
		}
		if !sess.EvaluateBool(`(() => { var bubble = document.createElement('div'); bubble.className = 'chat-question-card-wrap'; bubble.textContent = ` + quote(prompt) + `; document.body.appendChild(bubble); return window.tulaiSendAccepted(location.href, ` + quote(prompt) + `, 0) === true; })()`) {
			t.Fatal("千问用户消息是 .chat-question-card-wrap，正文相符就要确认发送")
		}
		if !sess.EvaluateBool(`(() => {
			window.__tulaiMarkTask();
			var reply = document.createElement('div');
			reply.className = 'chat-answers-card-wrap';
			reply.innerHTML = '<div id="qk-markdown-react"><div id="card_card_ai_generate_image_1"><img alt="loading" width="32" height="32" src="https://cdn.example/loading.png"><a href="https://cdn.example/out.png"><img width="800" height="800" src="https://cdn.example/out.png?x-oss-process=image/resize,w_400"></a></div></div>';
			document.body.appendChild(reply);
			window.__reply = reply;
			return true;
		})()`) {
			t.Fatal("没有挂上生成卡")
		}
		busy := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if busy.Done || busy.Reason != "generating" {
			t.Fatalf("灰块还在不能收图 done=%t reason=%s images=%d", busy.Done, busy.Reason, len(busy.Images))
		}
		for _, image := range busy.Images {
			if strings.Contains(image.URL, "loading") || strings.Contains(image.URL, "x-oss-process") {
				t.Fatalf("灰块或缩略图不该留下: %s", image.URL)
			}
		}
		if !sess.EvaluateBool(`(() => { var img = window.__reply.querySelector('img[alt="loading"]'); img.remove(); return true; })()`) {
			t.Fatal("拿不掉灰块")
		}
		live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if !live.Done || live.Reason != "images-ready" || len(live.Images) != 1 || live.Images[0].URL != "https://cdn.example/out.png" {
			t.Fatalf("应下载无处理参数的原图 images=%v done=%t reason=%s", live.Images, live.Done, live.Reason)
		}
	})
}
