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
			reply.setAttribute('data-chat-answers-wrap', 'cid');
			var card = document.createElement('div');
			card.setAttribute('data-qianwen-card', 'ai_generate_image_list');
			card.setAttribute('data-exposure-extra', '{"card_status":"processing"}');
			var script = document.createElement('script');
			script.type = 'application/json';
			script.setAttribute('data-used-by', 'hydrate');
			script.textContent = '{"data":{"originalData":{"content":{"status":"complete","image_num":1}}}}';
			var quark = document.createElement('img');
			quark.alt = 'loading';
			quark.src = 'https://images.quark.cn/s/deco.webp';
			var avatar = document.createElement('img');
			avatar.src = 'https://gw.alicdn.com/imgextra/a.jpg';
			card.appendChild(script);
			card.appendChild(quark);
			card.appendChild(avatar);
			reply.appendChild(card);
			document.body.appendChild(reply);
			window.__reply = reply;
			return true;
		})()`) {
			t.Fatal("没有挂上生成卡")
		}
		busy := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if busy.Done || busy.Reason != "generating" || busy.Replies == 0 || len(busy.Images) != 0 {
			t.Fatalf("生成中不能收图 done=%t reason=%s replies=%d images=%d", busy.Done, busy.Reason, busy.Replies, len(busy.Images))
		}
		if !sess.EvaluateBool(`(() => {
			var card = window.__reply.querySelector('[data-qianwen-card="ai_generate_image_list"]');
			card.setAttribute('data-exposure-extra', '{"card_status":"complete"}');
			var img = document.createElement('img');
			img.src = 'https://workspace-zb-cdn.qianwen.com/o/out.png?auth_key=abc';
			img.setAttribute('data-image-resource-id', 'rid');
			img.setAttribute('data-image-watermark-url', 'https://workspace-zb-cdn.qianwen.com/o/out.png?auth_key=abc&x-oss-process=image/resize,w_400');
			var decoy = document.createElement('img');
			decoy.src = 'https://images.quark.cn/s/deco.webp';
			decoy.setAttribute('data-image-resource-id', 'decoy');
			card.appendChild(img);
			card.appendChild(decoy);
			return true;
		})()`) {
			t.Fatal("没有换成完成态")
		}
		live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if !live.Done || live.Reason != "images-ready" || len(live.Images) != 1 || live.Images[0].URL != "https://workspace-zb-cdn.qianwen.com/o/out.png?auth_key=abc" {
			t.Fatalf("应下载 img.src 原图 images=%v done=%t reason=%s", live.Images, live.Done, live.Reason)
		}
	})
}

func TestQianwenGenerationFixtures(t *testing.T) {
	ws, shutdown := launchChromeArgs(t, []string{"--blink-settings=imagesEnabled=false"})
	defer shutdown()
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	hook := mustRead(t, filepath.Join("js", "hook.js"))
	composer := mustRead(t, filepath.Join("js", "composer.js"))
	names := []string{"live-gen-3", "live-gen-15", "live-gen-40", "live-gen-45", "live-gen-98-final"}
	pages := map[string]string{}
	for _, name := range names {
		pages[name] = staticFixtureKeepJSON(mustRead(t, filepath.Join("testdata", "qianwen", "live2", name+".html")))
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

	for _, name := range []string{"live-gen-3", "live-gen-15", "live-gen-40", "live-gen-45"} {
		t.Run(name, func(t *testing.T) {
			sess := openFixture(t, client, srv.URL+"/"+name, hook, composer)
			defer sess.Close()
			if !sess.EvaluateBool(`!!document.querySelector('.chat-answers-card-wrap[data-chat-answers-wrap]') && !!document.querySelector('img[src*="gw.alicdn.com"]')`) {
				t.Fatal("夹具里应有回复容器和头像")
			}
			if name != "live-gen-3" && !sess.EvaluateBool(`!!document.querySelector('[data-qianwen-card="ai_generate_image_list"]') && !!document.querySelector('img[src*="images.quark.cn"]') && !document.querySelector('img[data-image-resource-id]')`) {
				t.Fatal("进行中的图卡应还停在装饰图，没有结果图")
			}
			live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
			if live.Done || live.Replies == 0 || len(live.Images) != 0 || live.Reason != "generating" {
				t.Fatalf("进行中 replies=%d images=%d done=%t reason=%s", live.Replies, len(live.Images), live.Done, live.Reason)
			}
		})
	}

	t.Run("final", func(t *testing.T) {
		sess := openFixture(t, client, srv.URL+"/live-gen-98-final", hook, composer)
		defer sess.Close()
		if !sess.EvaluateBool(`(() => {
			var script = document.querySelector('script[data-used-by="hydrate"]');
			var card = document.querySelector('[data-qianwen-card="ai_generate_image_list"]');
			if (!script || !card || script.textContent.indexOf('"status":"processing"') < 0) return false;
			if (card.getAttribute('data-exposure-extra').indexOf('"card_status":"complete"') < 0) return false;
			var decoy = document.createElement('img');
			decoy.src = 'https://images.quark.cn/s/uae/deco.webp';
			decoy.setAttribute('data-image-resource-id', 'decoy');
			card.appendChild(decoy);
			var stop = document.createElement('button');
			stop.textContent = '停止';
			stop.style.cssText = 'width:40px;height:40px';
			document.body.appendChild(stop);
			return !!document.querySelector('img[src*="gw.alicdn.com"]');
		})()`) {
			t.Fatal("完成夹具应是 hydrate 仍 processing、卡片已 complete，并且页面上有头像")
		}
		live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		src, err := sess.Evaluate(`document.querySelector('[data-qianwen-card="ai_generate_image_list"] img[data-image-resource-id="8c7a305e657c4e3190d74059c11c5736"]').src`)
		if err != nil {
			t.Fatal(err)
		}
		src = strings.Trim(src, `"`)
		if !live.Done || live.Reason != "images-ready" || live.Replies == 0 || live.Expect != 1 || len(live.Images) != 1 || live.Images[0].URL != src {
			t.Fatalf("完成态应只留 img.src replies=%d expect=%d images=%v done=%t reason=%s src=%s", live.Replies, live.Expect, live.Images, live.Done, live.Reason, src)
		}
		if !strings.Contains(src, "workspace-zb-cdn.qianwen.com") || strings.Contains(src, "x-oss-process") || strings.Contains(src, "gw.alicdn.com") || strings.Contains(src, "images.quark.cn") {
			t.Fatalf("原图地址不对: %s", src)
		}
		short := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var script = document.querySelector('script[data-used-by="hydrate"]');
			var data = JSON.parse(script.textContent);
			data.data.originalData.content.image_num = 2;
			script.textContent = JSON.stringify(data);
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if short.Done || len(short.Images) != 1 {
			t.Fatalf("张数不够 image_num 不能结束 images=%d done=%t", len(short.Images), short.Done)
		}
		processing := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var card = document.querySelector('[data-qianwen-card="ai_generate_image_list"]');
			card.setAttribute('data-exposure-extra', '{"card_status":"processing"}');
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if processing.Done || processing.Reason != "generating" {
			t.Fatalf("card_status 回到 processing 不能当完成 done=%t reason=%s", processing.Done, processing.Reason)
		}
		kept := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var card = document.querySelector('[data-qianwen-card="ai_generate_image_list"]');
			card.setAttribute('data-exposure-extra', '{"card_status":"complete"}');
			var script = document.querySelector('script[data-used-by="hydrate"]');
			var data = JSON.parse(script.textContent);
			data.data.originalData.content.image_num = 1;
			script.textContent = JSON.stringify(data);
			var reply = document.querySelector('.chat-answers-card-wrap[data-chat-answers-wrap]');
			var parent = reply.parentNode, next = reply.nextSibling;
			parent.removeChild(reply);
			__tulaiMarkTask();
			parent.insertBefore(reply, next);
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if !kept.Done || kept.Replies == 0 || len(kept.Images) != 1 || kept.Images[0].URL != src {
			t.Fatalf("标记之后新出现的完成回复也要收下 images=%v replies=%d done=%t", kept.Images, kept.Replies, kept.Done)
		}
	})
}

func staticFixtureKeepJSON(html string) string {
	html = scriptTag.ReplaceAllStringFunc(html, func(tag string) string {
		head := tag
		if i := strings.Index(tag, ">"); i >= 0 {
			head = tag[:i]
		}
		lower := strings.ToLower(head)
		if strings.Contains(lower, "application/json") || strings.Contains(lower, "data-used-by") {
			return tag
		}
		return ""
	})
	return linkTag.ReplaceAllString(html, "")
}
