package strategy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// 千问2 真实页面：回复只有「请求异常，请检查网络连接」和重新生成图标，没有图卡。
// 检测要直接报 error-reply，而不是 no-new-image 等到超时。
// 豆包新对话：长连接让 readyState 停在 interactive，空对话加上输入框就该能打字。
func TestReplyFaultAndDoubaoReady(t *testing.T) {
	client := newFixtureClient(t)
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
		w.Header().Set("Content-Security-Policy", fixtureCSP)
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
		vip := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var reply = document.querySelector('.chat-answers-card-wrap[data-chat-answers-wrap]');
			var card = document.createElement('div');
			card.setAttribute('data-qianwen-card', 'zd_membership_payment_buttons_card');
			card.textContent = '本次生成使用了会员专属功能，开通后立即可用 开通创作会员';
			reply.appendChild(card);
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if vip.Reason != "error-reply" || vip.Busy || len(vip.Images) != 0 || !strings.Contains(vip.Fault, "会员专属功能") {
			t.Fatalf("会员专属拦截卡应立即报 error-reply reason=%s busy=%t images=%d fault=%q", vip.Reason, vip.Busy, len(vip.Images), vip.Fault)
		}
		if isReplyQuotaFault(vip.Fault) {
			t.Fatalf("会员专属拦截卡不应误判为积分耗尽: %q", vip.Fault)
		}
		noCredit := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var card = document.querySelector('[data-qianwen-card="zd_membership_payment_buttons_card"]');
			card.textContent = '积分不足，请购买积分或开通创作会员 购买积分 开通创作会员';
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if noCredit.Reason != "error-reply" || !isReplyQuotaFault(noCredit.Fault) {
			t.Fatalf("积分不足拦截卡应识别为 quota fault: reason=%s fault=%q", noCredit.Reason, noCredit.Fault)
		}
		if !sess.EvaluateBool(`(() => {
			window.__tulaiAssistantBaseline = 0;
			var f = tulaiAccountFault();
			return f && f.quota === true;
		})()`) {
			t.Fatal("积分不足回复应令 tulaiAccountFault().quota = true")
		}
		chatCap := readLive(t, sess, `window.__tulaiPageHost='www.qianwen.com'; (() => {
			var reply = document.querySelector('.chat-answers-card-wrap[data-chat-answers-wrap]');
			reply.textContent = '今日对话次数已达上限，请明天再来。';
			return JSON.stringify(__tulaiLiveImages());
		})()`)
		if chatCap.Reason != "error-reply" || !isReplyQuotaFault(chatCap.Fault) {
			t.Fatalf("今日对话次数已达上限应立即识别为 quota fault: reason=%s fault=%q", chatCap.Reason, chatCap.Fault)
		}
		if !sess.EvaluateBool(`(() => {
			window.__tulaiAssistantBaseline = 0;
			var f = tulaiAccountFault();
			return f && f.quota === true;
		})()`) {
			t.Fatal("今日对话次数已达上限应令 tulaiAccountFault().quota = true")
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
