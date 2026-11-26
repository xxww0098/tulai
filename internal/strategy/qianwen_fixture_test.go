package strategy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQianwenLiveFixtures(t *testing.T) {
	client := newFixtureClient(t)
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
		w.Header().Set("Content-Security-Policy", fixtureCSP)
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

	t.Run("stale chat url", func(t *testing.T) {
		sess := openFixture(t, client, srv.URL+"/9338-0", hook, composer)
		defer sess.Close()
		if !sess.EvaluateBool(`window.__tulaiPageHost='www.qianwen.com'; history.pushState({}, '', '/chat/5e6b3a5d14dd4f25996b2abb4203c942'); (() => { var st = __tulaiChatReady(); var before = location.href; var via = __tulaiFreshChat(); return st.ready === true && st.cards === 0 && st.why === '' && st.id === '5e6b3a5d14dd4f25996b2abb4203c942' && via === 'ready' && location.href === before; })()`) {
			out, _ := sess.Evaluate(`JSON.stringify(__tulaiChatReady()) + ' ' + location.href`)
			t.Fatalf("地址挂着对话、页面仍是首页时要能打字，并且不能再导航: %s", out)
		}
	})

	t.Run("send and original", func(t *testing.T) {
		prompt := "雪山下的小木屋，油画风格"
		sess := openFixture(t, client, srv.URL+"/9338-0", hook, composer)
		defer sess.Close()
		if !sess.EvaluateBool(`window.__tulaiPageHost='www.qianwen.com'; window.tulaiMarkComposer() && window.__tulaiComposer.getAttribute('data-slate-editor') === 'true'`) {
			t.Fatal("应写到 Slate 输入框，而不是别的可编辑节点")
		}
		if !sess.EvaluateBool(`window.tulaiReadComposer(window.__tulaiComposer) === ''`) {
			out, _ := sess.Evaluate(`JSON.stringify(window.tulaiReadComposer(window.__tulaiComposer))`)
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
	client := newFixtureClient(t)
	hook := mustRead(t, filepath.Join("js", "hook.js"))
	composer := mustRead(t, filepath.Join("js", "composer.js"))
	names := []string{"live-gen-3", "live-gen-15", "live-gen-40", "live-gen-45", "live-gen-98-final"}
	pages := map[string]string{}
	for _, name := range names {
		pages[name] = staticFixtureKeepJSON(mustRead(t, filepath.Join("testdata", "qianwen", "live2", name+".html")))
	}
	const sentChat = "78241e13b27e48a2868f7133437ca28f"
	const sentWrap = "9d6za8GJXYrgCGrzLZb49-Jt_cRKc9nn"
	pages["chat/"+sentChat] = pages["live-gen-98-final"]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(r.URL.Path, "/")
		body, ok := pages[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", fixtureCSP)
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

	t.Run("bound chat", func(t *testing.T) {
		sess := openFixture(t, client, srv.URL+"/live-gen-98-final", hook, composer)
		defer sess.Close()
		live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; history.pushState({}, '', '/chat/`+sentChat+`'); window.__tulaiQwenURL='`+sentChat+`'; window.__tulaiQwenChat='`+sentWrap+`'; JSON.stringify(__tulaiLiveImages())`)
		if !live.Done || live.Reason != "images-ready" || live.Here != sentChat || len(live.Images) != 1 {
			t.Fatalf("绑住发送时的问答卡后要读到这张图 here=%s images=%v done=%t reason=%s", live.Here, live.Images, live.Done, live.Reason)
		}
		decoy := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var other = document.createElement('div');
			other.className = 'chat-answers-card-wrap';
			other.setAttribute('data-chat-answers-wrap', 'other-wrap');
			var card = document.createElement('div');
			card.setAttribute('data-qianwen-card', 'ai_generate_image_list');
			card.setAttribute('data-exposure-extra', '{"card_status":"complete"}');
			var img = document.createElement('img');
			img.src = 'https://workspace-zb-cdn.qianwen.com/o/other.png';
			img.setAttribute('data-image-resource-id', 'other');
			card.appendChild(img);
			other.appendChild(card);
			document.body.appendChild(other);
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if !decoy.Done || len(decoy.Images) != 1 || strings.Contains(decoy.Images[0].URL, "other.png") {
			t.Fatalf("别的对话的图不能算这一轮 images=%v", decoy.Images)
		}
		away := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; history.pushState({}, '', '/chat/ffffffffffffffffffffffffffffffff'); JSON.stringify(__tulaiLiveImages())`)
		if away.Reason != "off-chat" || away.Replies != 0 || len(away.Images) != 0 || away.Here == sentChat {
			t.Fatalf("离开这条对话后不能在当前页继续读 here=%s replies=%d images=%v reason=%s", away.Here, away.Replies, away.Images, away.Reason)
		}
		shell := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; history.pushState({}, '', '/chat/`+sentChat+`'); document.querySelectorAll('.chat-question-card-wrap,.chat-answers-card-wrap').forEach(function (n) { n.remove(); }); JSON.stringify(__tulaiLiveImages())`)
		if shell.Reason != "off-chat" || len(shell.Images) != 0 {
			t.Fatalf("地址还在这条对话上、页面却是首页壳时要标成离开了对话 reason=%s images=%d", shell.Reason, len(shell.Images))
		}
	})

	t.Run("second prompt", func(t *testing.T) {
		sess := openFixture(t, client, srv.URL+"/live-gen-98-final", hook, composer)
		defer sess.Close()
		const cabin = "https://workspace-zb-cdn.qianwen.com/o/cabin.png"
		const prompt = "雪山下的小木屋，油画风格"
		if !sess.EvaluateBool(`window.__tulaiPageHost='www.qianwen.com'; history.pushState({}, '', '/chat/` + sentChat + `'); window.__tulaiQwenURL='` + sentChat + `'; window.__tulaiQwenChat='` + sentWrap + `'; __tulaiMarkTask(); (() => {
			var stale = __tulaiLiveImages();
			if (stale.done || stale.images.some(function (img) { return img.url.indexOf('1793285583682') >= 0; })) return false;
			var answer = document.createElement('div');
			answer.className = 'chat-answers-card-wrap';
			answer.setAttribute('data-chat-answers-wrap', 'cabin-wrap');
			var card = document.createElement('div');
			card.setAttribute('data-qianwen-card', 'ai_generate_image_list');
			card.setAttribute('data-exposure-extra', '{"card_status":"complete"}');
			var script = document.createElement('script');
			script.type = 'application/json';
			script.setAttribute('data-used-by', 'hydrate');
			script.textContent = '{"data":{"originalData":{"content":{"image_num":1}}}}';
			var img = document.createElement('img');
			img.src = '` + cabin + `';
			img.setAttribute('data-image-resource-id', 'cabin');
			card.appendChild(script);
			card.appendChild(img);
			answer.appendChild(card);
			document.body.appendChild(answer);
			return true;
		})()`) {
			out, _ := sess.Evaluate(`JSON.stringify(__tulaiLiveImages())`)
			t.Fatalf("标记后不能把上一轮橘猫当成这一轮: %s", out)
		}
		live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if !live.Done || live.Reason != "images-ready" || len(live.Images) != 1 || live.Images[0].URL != cabin {
			t.Fatalf("第二句要读新图，不能读上一轮的橘猫 images=%v done=%t reason=%s", live.Images, live.Done, live.Reason)
		}
		sess.ResetChat()
		applyQwenBind(sess)
		sess.BindChat(sentChat, "cabin-wrap")
		applyQwenBind(sess)
		bound := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if !bound.Done || len(bound.Images) != 1 || bound.Images[0].URL != cabin {
			t.Fatalf("换到这句的问答卡后仍只留新图 images=%v done=%t", bound.Images, bound.Done)
		}
		if !sess.EvaluateBool(`window.__tulaiPageHost='www.qianwen.com'; (() => {
			var prompt = ` + quote(prompt) + `;
			if (window.tulaiSendAccepted('https://www.qianwen.com/', prompt, 0)) return false;
			history.pushState({}, '', '/chat/cccccccccccccccccccccccccccccccc');
			if (window.tulaiSendAccepted('https://www.qianwen.com/', prompt, 0)) return false;
			var bubble = document.createElement('div');
			bubble.className = 'chat-question-card-wrap';
			bubble.setAttribute('data-chat-question-wrap', 'cabin-wrap');
			bubble.textContent = prompt;
			document.body.appendChild(bubble);
			return window.tulaiSendAccepted('https://www.qianwen.com/', prompt, 0) === true;
		})()`) {
			t.Fatal("上一轮问答卡还在时，地址变化不能当成这句已经发出")
		}
	})

	t.Run("reopen sent chat", func(t *testing.T) {
		sess := openFixture(t, client, srv.URL+"/live-gen-3", hook, composer)
		defer sess.Close()
		sess.AppendNavigateScript(hook)
		sess.AppendNavigateScript(composer)
		FocusQwenChat(sess, sentChat, sentWrap)
		href, err := sess.Evaluate(`location.pathname`)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Trim(href, `"`) != "/chat/"+sentChat {
			t.Fatalf("不在这条对话上就要打开它: %s", href)
		}
		live := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; JSON.stringify(__tulaiLiveImages())`)
		if !live.Done || live.Reason != "images-ready" || live.Replies == 0 || len(live.Images) != 1 || !strings.Contains(live.Images[0].URL, "workspace-zb-cdn.qianwen.com") {
			t.Fatalf("回到原对话后直接读结果图，不再发送 replies=%d images=%v done=%t reason=%s", live.Replies, live.Images, live.Done, live.Reason)
		}
	})
}

func TestQianwenShellCountsAsFresh(t *testing.T) {
	shell := chatReady{Ready: true, ID: "5e6b3a5d14dd4f25996b2abb4203c942", Cards: 0, Chars: 0, Node: "n1"}
	if !freshLanded("ready", "", shell) {
		t.Fatal("地址还挂着对话、页面没有问答卡，要当成能打字")
	}
	open := chatReady{Ready: false, ID: "still-old", Cards: 1, Why: "cards"}
	if freshLanded("url", "still-old", open) {
		t.Fatal("问答卡还在就不是新对话")
	}
	blank := chatReady{Ready: true, Node: "n", Cards: 0, Chars: 0}
	if !freshLanded("ready", "", blank) {
		t.Fatal("空白输入框要能开始")
	}
	err := freshChatFailure(chatReady{Why: "no-composer", ID: "abc", Href: "https://www.qianwen.com/chat/abc"})
	if err == nil || !strings.Contains(err.Error(), "no-composer") {
		t.Fatalf("失败要写明缺的是哪一条: %v", err)
	}
}

func TestQianwenSetOutputRatioFixture(t *testing.T) {
	client := newFixtureClient(t)
	html := `<!doctype html><html><head><meta charset="utf-8"><style>
button { width: 96px; height: 32px; }
[role="menu"] { position: fixed; left: 40px; top: 80px; width: 148px; background: #fff; }
[role="menuitemcheckbox"] { width: 138px; height: 36px; display: flex; align-items: center; }
[role="menu"][data-state="closed"] { opacity: 0.4; pointer-events: none; }
</style></head><body>
<button id="ratio-btn" type="button">比例 3:4</button>
<script>
const ratios = ['9:16', '2:3', '3:4', '1:1', '4:3', '3:2', '16:9'];
const btn = document.getElementById('ratio-btn');
let menu = null;
let closeTimer = null;
window.__menuCorrupted = false;

function removeMenu() {
  if (closeTimer) { clearTimeout(closeTimer); closeTimer = null; }
  if (menu && menu.parentNode) menu.parentNode.removeChild(menu);
  menu = null;
}

function closeMenuAnimated() {
  if (!menu) return;
  menu.setAttribute('data-state', 'closed');
  if (closeTimer) clearTimeout(closeTimer);
  closeTimer = setTimeout(removeMenu, 300);
}

function openMenu() {
  removeMenu();
  menu = document.createElement('div');
  menu.setAttribute('role', 'menu');
  menu.setAttribute('data-state', 'open');
  ratios.forEach(r => {
    const item = document.createElement('div');
    item.setAttribute('role', 'menuitemcheckbox');
    const inner = document.createElement('div');
    const icon = document.createElement('span');
    const label = document.createElement('span');
    label.textContent = r;
    inner.appendChild(icon);
    inner.appendChild(label);
    item.appendChild(inner);
    item.addEventListener('click', () => {
      if (menu && menu.getAttribute('data-state') === 'closed') return;
      btn.textContent = '比例 ' + r;
      closeMenuAnimated();
    });
    menu.appendChild(item);
  });
  document.body.appendChild(menu);
}

btn.addEventListener('click', () => {
  if (menu && menu.getAttribute('data-state') === 'closed') {
    window.__menuCorrupted = true;
    return;
  }
  if (menu && menu.getAttribute('data-state') === 'open') {
    closeMenuAnimated();
  } else {
    openMenu();
  }
});

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && menu && menu.getAttribute('data-state') === 'open') {
    closeMenuAnimated();
  }
});
</script></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	sess := openFixture(t, client, srv.URL+"/", "", "")
	defer sess.Close()
	qw := Qianwen{}

	// Already selected fast path
	if ok, why := qw.SetOutputRatio(sess, "3:4"); !ok || why != "" {
		t.Fatalf("already 3:4 failed: ok=%v why=%q", ok, why)
	}
	// Back-to-back switches across all Radix 300ms fade-out transitions
	for _, want := range []string{"3:2", "2:3", "4:3", "16:9", "1:1", "9:16", "3:4"} {
		if ok, why := qw.SetOutputRatio(sess, want); !ok {
			t.Fatalf("back-to-back SetOutputRatio(%q) failed: %q", want, why)
		}
		got, _ := sess.Evaluate(`document.getElementById('ratio-btn').textContent`)
		if strings.Trim(got, `"`) != "比例 "+want {
			t.Fatalf("after SetOutputRatio(%q), button=%s", want, got)
		}
	}
	if sess.EvaluateBool(`window.__menuCorrupted === true`) {
		t.Fatal("SetOutputRatio clicked trigger during Radix closing animation")
	}
	// Missing option closes menu cleanly and returns explicit error
	if ok, why := qw.SetOutputRatio(sess, "5:4"); ok || !strings.Contains(why, "5:4") {
		t.Fatalf("missing ratio 5:4: ok=%v why=%q", ok, why)
	}
	if sess.EvaluateBool(`!!document.querySelector('[role="menu"][data-state="open"]')`) {
		t.Fatal("menu remained open after missing ratio error")
	}
	// Invalid format rejected without opening menu
	if ok, why := qw.SetOutputRatio(sess, "invalid"); ok || why == "" {
		t.Fatalf("invalid ratio should fail: ok=%v why=%q", ok, why)
	}
	// After uploading a reference image, Qianwen disables the ratio button while keeping the chosen ratio visible.
	if _, err := sess.Evaluate(`(() => { const b = document.getElementById('ratio-btn'); b.textContent = '比例 16:9'; b.disabled = true; })()`); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	if ok, why := qw.SetOutputRatio(sess, "16:9"); !ok || why != "" {
		t.Fatalf("disabled matching ratio 16:9 must confirm immediately: ok=%v why=%q", ok, why)
	}
	if d := time.Since(t0); d > 1500*time.Millisecond {
		t.Fatalf("disabled matching ratio took too long (%v), expected instant confirmation", d)
	}
	if err := ensureQianwenImageModeWithTimeout(sess, 1500); err != nil {
		t.Fatalf("disabled ratio button must still count as active AI生图 mode: %v", err)
	}
	t1 := time.Now()
	if ok, why := qw.SetOutputRatio(sess, "4:3"); ok || !strings.Contains(why, "已锁定") {
		t.Fatalf("disabled mismatched ratio should fail fast with locked message: ok=%v why=%q", ok, why)
	}
	if d := time.Since(t1); d > 3*time.Second {
		t.Fatalf("disabled mismatched ratio stalled (%v), expected fast failure", d)
	}
}

func TestQianwenEnsureImageModeFixture(t *testing.T) {
	client := newFixtureClient(t)

	t.Run("delayed hydration and idempotent repeat", func(t *testing.T) {
		html := `<!doctype html><html><head><meta charset="utf-8"><style>
button { width: 96px; height: 32px; }
</style></head><body>
<div data-slate-editor="true" contenteditable="true"></div>
<div id="bar"></div>
<script>
window.__aiClicks = 0;
setTimeout(() => {
  const bar = document.getElementById('bar');
  const wrap = document.createElement('div');
  wrap.setAttribute('data-overflow-placement', 'inline');
  const btn = document.createElement('button');
  btn.id = 'ai-btn';
  btn.type = 'button';
  btn.setAttribute('aria-label', 'AI生图');
  btn.setAttribute('aria-pressed', 'false');
  btn.textContent = 'AI生图';
  btn.addEventListener('click', () => {
    window.__aiClicks++;
    if (window.__aiClicks === 1) return; // simulate swallowed first click during cold hydration
    const active = btn.getAttribute('aria-pressed') === 'true';
    btn.setAttribute('aria-pressed', active ? 'false' : 'true');
    const existing = document.getElementById('ratio-btn');
    if (!active && !existing) {
      const rb = document.createElement('button');
      rb.id = 'ratio-btn';
      rb.type = 'button';
      rb.setAttribute('aria-haspopup', 'menu');
      rb.textContent = '比例 16:9';
      bar.appendChild(rb);
    } else if (active && existing) {
      existing.remove();
    }
  });
  wrap.appendChild(btn);
  bar.appendChild(wrap);
}, 180);
</script></body></html>`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		}))
		defer srv.Close()

		sess := openFixture(t, client, srv.URL+"/", "", "")
		defer sess.Close()
		if err := ensureQianwenImageModeWithTimeout(sess, 6000); err != nil {
			t.Fatalf("expected delayed AI生图 activation to succeed: %v", err)
		}
		if !sess.EvaluateBool(`document.getElementById('ai-btn')?.getAttribute('aria-pressed') === 'true' && !!document.getElementById('ratio-btn')`) {
			t.Fatal("AI生图 mode not active after first ensure")
		}
		clicksBefore := sess.EvaluateBool(`window.__aiClicks === 2`)
		if !clicksBefore {
			out, _ := sess.Evaluate(`window.__aiClicks`)
			t.Fatalf("expected 2 clicks (1 swallowed + 1 retry), got %s", out)
		}
		// Repeat call must be an idempotent 0-click no-op and never toggle off
		if err := ensureQianwenImageModeWithTimeout(sess, 2000); err != nil {
			t.Fatalf("idempotent ensure failed: %v", err)
		}
		if !sess.EvaluateBool(`window.__aiClicks === 2 && document.getElementById('ai-btn')?.getAttribute('aria-pressed') === 'true' && document.getElementById('ratio-btn')?.textContent === '比例 16:9'`) {
			t.Fatal("repeat ensureQianwenImageMode clicked active chip or altered ratio")
		}
	})

	t.Run("exits conflicting mode before entering AI生图", func(t *testing.T) {
		html := `<!doctype html><html><head><meta charset="utf-8"><style>
button { width: 96px; height: 32px; }
</style></head><body>
<div data-slate-editor="true" contenteditable="true"></div>
<div id="bar">
  <div data-overflow-placement="inline"><button id="ppt-btn" type="button" aria-label="PPT创作" aria-pressed="true">PPT创作</button></div>
</div>
<script>
const bar = document.getElementById('bar');
document.getElementById('ppt-btn').addEventListener('click', () => {
  bar.innerHTML = '<div data-overflow-placement="inline"><button id="ai-btn" type="button" aria-label="AI生图" aria-pressed="false">AI生图</button></div>';
  const ai = document.getElementById('ai-btn');
  ai.addEventListener('click', () => {
    ai.setAttribute('aria-pressed', 'true');
    const rb = document.createElement('button');
    rb.type = 'button';
    rb.setAttribute('aria-haspopup', 'menu');
    rb.textContent = '比例 3:4';
    bar.appendChild(rb);
  });
});
</script></body></html>`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		}))
		defer srv.Close()

		sess := openFixture(t, client, srv.URL+"/", "", "")
		defer sess.Close()
		if err := ensureQianwenImageModeWithTimeout(sess, 4000); err != nil {
			t.Fatalf("failed to exit PPT创作 and enter AI生图: %v", err)
		}
		if !sess.EvaluateBool(`document.getElementById('ai-btn')?.getAttribute('aria-pressed') === 'true'`) {
			t.Fatal("AI生图 not active after exiting PPT创作")
		}
	})

	t.Run("opens overflow 更多 menu when AI生图 overflows", func(t *testing.T) {
		html := `<!doctype html><html><head><meta charset="utf-8"><style>
button { width: 96px; height: 32px; }
[role="menu"] { position: fixed; left: 40px; top: 80px; width: 140px; background: #fff; }
[role="menuitem"] { width: 120px; height: 32px; display: flex; align-items: center; }
</style></head><body>
<div data-slate-editor="true" contenteditable="true"></div>
<div id="bar">
  <button id="more-inert" type="button" aria-label="更多">更多</button>
  <button id="more-real" type="button" aria-label="更多" aria-haspopup="menu">更多</button>
</div>
<script>
const bar = document.getElementById('bar');
document.getElementById('more-real').addEventListener('click', () => {
  if (document.getElementById('more-menu')) return;
  const m = document.createElement('div');
  m.id = 'more-menu';
  m.setAttribute('role', 'menu');
  m.setAttribute('data-state', 'open');
  const item = document.createElement('div');
  item.setAttribute('role', 'menuitem');
  item.setAttribute('aria-label', 'AI生图');
  item.textContent = 'AI生图';
  item.addEventListener('click', () => {
    m.remove();
    bar.innerHTML = '<button id="ai-btn" type="button" aria-label="AI生图" aria-pressed="true">AI生图</button><button type="button" aria-haspopup="menu">比例 3:4</button>';
  });
  m.appendChild(item);
  document.body.appendChild(m);
});
</script></body></html>`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		}))
		defer srv.Close()

		sess := openFixture(t, client, srv.URL+"/", "", "")
		defer sess.Close()
		if err := ensureQianwenImageModeWithTimeout(sess, 4000); err != nil {
			t.Fatalf("failed to enter AI生图 via 更多 overflow menu: %v", err)
		}
		if !sess.EvaluateBool(`document.getElementById('ai-btn')?.getAttribute('aria-pressed') === 'true'`) {
			t.Fatal("AI生图 not active after selecting from 更多 menu")
		}
	})

	t.Run("missing entry returns explicit error", func(t *testing.T) {
		html := `<!doctype html><html><body><div data-slate-editor="true" contenteditable="true"></div></body></html>`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		}))
		defer srv.Close()

		sess := openFixture(t, client, srv.URL+"/", "", "")
		defer sess.Close()
		err := ensureQianwenImageModeWithTimeout(sess, 400)
		if err == nil || !strings.Contains(err.Error(), "AI生图") {
			t.Fatalf("expected explicit AI生图 missing error, got %v", err)
		}
	})

	t.Run("login wall without entry reports login error", func(t *testing.T) {
		html := `<!doctype html><html><head><style>button{width:80px;height:32px;}</style></head><body><button type="button">登录</button></body></html>`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		}))
		defer srv.Close()

		sess := openFixture(t, client, srv.URL+"/", "", "")
		defer sess.Close()
		err := ensureQianwenImageModeWithTimeout(sess, 400)
		if err == nil || !strings.Contains(err.Error(), "请先登录") {
			t.Fatalf("expected login error on login wall, got %v", err)
		}
	})
}

func TestQwenIntlSetOutputRatioFixture(t *testing.T) {
	client := newFixtureClient(t)
	html := `<!doctype html><html><head><meta charset="utf-8"><style>
.bar { position: relative; display: flex; gap: 12px; padding: 40px; }
.mode-select-open, .mode-select-current-mode, .qwen-chat-v2-dropdown-menu-select {
  display: inline-flex; align-items: center; justify-content: center;
  min-width: 68px; height: 32px; padding: 0 10px; border: 1px solid #ddd; cursor: pointer;
}
.chat-prompt-recommend-detail-container {
  position: fixed; left: 0; top: 52px; width: 420px; height: 24px; z-index: 1000;
  background: rgba(255,255,255,0.6);
}
.qwen-chat-v2-dropdown-menu-popup {
  position: fixed; left: 160px; top: 96px; width: 120px; background: #fff; border: 1px solid #ccc; z-index: 1200;
}
.qwen-chat-v2-dropdown-menu-item { height: 28px; display: flex; align-items: center; padding: 0 8px; cursor: pointer; }
</style></head><body>
<div class="bar" id="bar">
  <div class="mode-select-open" role="button" aria-label="选择模式">选择模式</div>
  <div class="message-input-subconfig-visible-item" id="subconfig"></div>
</div>
<div class="chat-prompt-recommend-detail-container" id="recommend-overlay">推荐浮层</div>
<script>
const ratios = ['3:4', '9:16', '2:3', '1:1', '3:2', '4:3', '16:9'];
const bar = document.getElementById('bar');
const sub = document.getElementById('subconfig');
const modeTrigger = bar.querySelector('.mode-select-open');
let currentRatio = '16:9';
let modeMenu = null;
let ratioMenu = null;
let dropNextOpen = false;
let dropAfterMenuOpen = false;
window.__modeEnters = 0;

function closeAllMenus() {
  if (modeMenu) { modeMenu.remove(); modeMenu = null; }
  if (ratioMenu) { ratioMenu.remove(); ratioMenu = null; }
}

function exitImageMode() {
  closeAllMenus();
  const badge = bar.querySelector('.mode-select-current-mode');
  if (badge) badge.remove();
  sub.innerHTML = '';
}

function enterImageMode() {
  closeAllMenus();
  exitImageMode();
  window.__modeEnters++;
  const badge = document.createElement('div');
  badge.className = 'mode-select-current-mode';
  badge.textContent = '生成图像';
  bar.insertBefore(badge, sub);

  const sel = document.createElement('div');
  sel.id = 'qwen-ratio-select';
  sel.className = 'qwen-chat-v2-dropdown-menu-select';
  sel.textContent = currentRatio;
  sel.addEventListener('click', () => {
    if (dropNextOpen) {
      dropNextOpen = false;
      exitImageMode();
      return;
    }
    if (ratioMenu) { ratioMenu.remove(); ratioMenu = null; return; }
    ratioMenu = document.createElement('div');
    ratioMenu.setAttribute('role', 'menu');
    ratioMenu.className = 'qwen-chat-v2-dropdown-menu-popup';
    ratios.forEach(r => {
      const item = document.createElement('div');
      item.setAttribute('role', 'menuitem');
      item.className = 'qwen-chat-v2-dropdown-menu-item';
      item.textContent = r;
      item.addEventListener('click', () => {
        currentRatio = r;
        sel.textContent = r;
        closeAllMenus();
      });
      ratioMenu.appendChild(item);
    });
    document.body.appendChild(ratioMenu);
    if (dropAfterMenuOpen) {
      dropAfterMenuOpen = false;
      setTimeout(exitImageMode, 10);
    }
  });
  sub.appendChild(sel);
}

modeTrigger.addEventListener('click', () => {
  if (modeMenu) { modeMenu.remove(); modeMenu = null; return; }
  modeMenu = document.createElement('div');
  modeMenu.setAttribute('role', 'menu');
  modeMenu.className = 'qwen-chat-v2-dropdown-menu-popup';
  const item = document.createElement('div');
  item.setAttribute('role', 'menuitem');
  item.className = 'qwen-chat-v2-dropdown-menu-item';
  item.textContent = '生成图像';
  item.addEventListener('click', enterImageMode);
  modeMenu.appendChild(item);
  document.body.appendChild(modeMenu);
});

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') closeAllMenus();
});

window.__armModeDrop = () => { dropNextOpen = true; };
window.__armMenuOpenModeDrop = () => { dropAfterMenuOpen = true; };
</script></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	sess := openFixture(t, client, srv.URL+"/", "", "")
	defer sess.Close()
	qi := QwenIntl{}

	// 1. Entering image mode + switching even when recommend overlay covers ratio trigger center
	for _, want := range []string{"16:9", "1:1", "3:4", "4:3", "9:16", "3:2", "2:3"} {
		if ok, why := qi.SetOutputRatio(sess, want); !ok {
			t.Fatalf("SetOutputRatio(%q) failed: %q", want, why)
		}
		got, _ := sess.Evaluate(`document.getElementById('qwen-ratio-select')?.textContent`)
		if strings.Trim(got, `"`) != want {
			t.Fatalf("after SetOutputRatio(%q), select=%s", want, got)
		}
	}

	// 2. Simulate cold-hydration reset detaching image mode on ratio click; SetOutputRatio must recover automatically.
	if _, err := sess.Evaluate(`window.__armModeDrop()`); err != nil {
		t.Fatal(err)
	}
	if ok, why := qi.SetOutputRatio(sess, "4:3"); !ok {
		t.Fatalf("SetOutputRatio after mid-click mode drop failed: %q", why)
	}
	if got, _ := sess.Evaluate(`document.getElementById('qwen-ratio-select')?.textContent`); strings.Trim(got, `"`) != "4:3" {
		t.Fatalf("after recovery select=%s want 4:3", got)
	}
	if !sess.EvaluateBool(`window.__modeEnters === 2`) {
		out, _ := sess.Evaluate(`window.__modeEnters`)
		t.Fatalf("expected exactly 2 mode enters (initial + 1 recovery), got %s", out)
	}

	// 2b. Simulate cold-hydration reset detaching image mode 10ms AFTER ratio menu opens (during the 30ms settle).
	if _, err := sess.Evaluate(`window.__armMenuOpenModeDrop()`); err != nil {
		t.Fatal(err)
	}
	if ok, why := qi.SetOutputRatio(sess, "1:1"); !ok {
		t.Fatalf("SetOutputRatio after post-menu-open mode drop failed: %q", why)
	}
	if got, _ := sess.Evaluate(`document.getElementById('qwen-ratio-select')?.textContent`); strings.Trim(got, `"`) != "1:1" {
		t.Fatalf("after post-menu-open recovery select=%s want 1:1", got)
	}
	if !sess.EvaluateBool(`window.__modeEnters === 3`) {
		out, _ := sess.Evaluate(`window.__modeEnters`)
		t.Fatalf("expected 3 mode enters after post-menu-open recovery, got %s", out)
	}

	// 3. Missing option closes menu cleanly and returns explicit error.
	if ok, why := qi.SetOutputRatio(sess, "5:4"); ok || !strings.Contains(why, "5:4") {
		t.Fatalf("missing ratio 5:4: ok=%v why=%q", ok, why)
	}
	if sess.EvaluateBool(`!!document.querySelector('[role="menu"]')`) {
		t.Fatal("menu remained open after missing ratio error")
	}
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
