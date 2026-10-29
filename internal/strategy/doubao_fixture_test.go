package strategy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/cdp"
)

func TestDoubaoFixturesDetectGeneratedImages(t *testing.T) {
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
	for _, name := range []string{"9336-0", "9336-1", "9337-5", "9337-1", "9337-3"} {
		pages[name] = staticFixture(mustRead(t, filepath.Join("testdata", "doubao", name, "page.html")))
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

	cases := []struct {
		name   string
		images int
	}{
		{"9336-0", 3},
		{"9336-1", 1},
		{"9337-5", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := openFixture(t, client, srv.URL+"/"+tc.name, hook, composer)
			defer sess.Close()
			live := readLive(t, sess, `window.__tulaiPageHost='www.doubao.com'; JSON.stringify(__tulaiLiveImages())`)
			if !live.Done || live.Reason != "images-ready" || len(live.Images) != tc.images {
				t.Fatalf("完成检测 images=%d done=%t reason=%s replies=%d", len(live.Images), live.Done, live.Reason, live.Replies)
			}
			for _, image := range live.Images {
				if !strings.Contains(image.URL, "rc_gen_image") || strings.Contains(image.URL, "cthumb") {
					t.Fatalf("应留下 rc_gen_image 原图: %s", image.URL)
				}
			}
			kept := readLive(t, sess, `(() => {
				window.__tulaiPageHost='www.doubao.com';
				var reply=document.querySelector('[data-testid="receive_message"]');
				var parent=reply.parentNode, next=reply.nextSibling;
				parent.removeChild(reply);
				__tulaiMarkTask();
				parent.insertBefore(reply, next);
				return JSON.stringify(__tulaiLiveImages());
			})()`)
			if !kept.Done || len(kept.Images) != tc.images {
				t.Fatalf("发送前不存在的回复不能被基准图丢掉 images=%d done=%t reason=%s", len(kept.Images), kept.Done, kept.Reason)
			}
		})
	}

	t.Run("send", func(t *testing.T) {
		prompt := "雪山下的小木屋，油画风格"
		sess := openFixture(t, client, srv.URL+"/9336-0", hook, composer)
		defer sess.Close()
		if !sess.EvaluateBool(`window.tulaiSendAccepted(location.href, ` + quote(prompt) + `, 0) === true`) {
			t.Fatal("已有 send_message[data-message-role=user] 且正文相符，应确认发送")
		}
		if !sess.EvaluateBool(`window.tulaiSendAccepted(location.href, ` + quote(prompt) + `, 1) === false`) {
			t.Fatal("这条用户消息在点击前就已经在，不能再算一次新发送")
		}
		fresh := openFixture(t, client, srv.URL+"/9337-1", hook, composer)
		defer fresh.Close()
		if !fresh.EvaluateBool(`window.tulaiMarkComposer() && document.querySelector('[data-tulai-composer="1"]').tagName==='TEXTAREA' && document.querySelector('[data-tulai-composer="1"]').getAttribute('data-testid')==='chat_input_input' && !document.querySelector('[data-tulai-composer="1"]').closest('.invisible')`) {
			t.Fatal("应写到可见的 chat_input_input textarea，而不是旁边那份 invisible 克隆")
		}
		if !fresh.EvaluateBool(`window.tulaiSendAccepted(location.href, ` + quote(prompt) + `, 0) === false`) {
			t.Fatal("还停在 /chat/ 且没有新的用户气泡，不能确认发送")
		}
		if !fresh.EvaluateBool(`(() => { var before=location.href; history.pushState({}, '', '/chat/38450053409309698'); return window.tulaiSendAccepted(before, ` + quote(prompt) + `, 0) === true; })()`) {
			t.Fatal("地址变成 /chat/<id> 就是发送已接受")
		}
	})

	t.Run("9337-3 send", func(t *testing.T) {
		prompt := "雪山下的小木屋，油画风格"
		sess := openFixture(t, client, srv.URL+"/9337-3", hook, composer)
		defer sess.Close()
		// 实页上按钮的 click() 不可信，豆包直接丢掉。盖住按钮后只能靠回车。
		if !sess.EvaluateBool(`(() => {
			var area = document.querySelector('textarea[data-testid="chat_input_input"]');
			var btn = document.querySelector('button[data-testid="chat_input_send_button"]');
			if (!area || !btn) return false;
			window.__hits = [];
			function accept(how) {
				if (window.__how) return;
				window.__how = how;
				var bubble = document.createElement('div');
				bubble.setAttribute('data-testid', 'send_message');
				bubble.setAttribute('data-message-role', 'user');
				bubble.textContent = area.value;
				document.body.appendChild(bubble);
			}
			btn.addEventListener('click', function (e) {
				window.__hits.push('click:' + e.isTrusted);
				if (e.isTrusted) accept('click');
			});
			area.addEventListener('keydown', function (e) {
				window.__hits.push('key:' + e.key + ':' + e.isTrusted);
				if (e.isTrusted && e.key === 'Enter') accept('enter');
			});
			var cover = document.createElement('div');
			cover.style.cssText = 'position:fixed;inset:0;z-index:2147483647';
			document.body.appendChild(cover);
			return true;
		})()`) {
			t.Fatal("9337-3 夹具没有挂上发送监听")
		}
		if !sess.EvaluateBool(`!!document.querySelector('textarea[data-testid="chat_input_input"]') && !!document.querySelector('button[data-testid="chat_input_send_button"]')`) {
			t.Fatal("9337-3 夹具里没有可见输入框或 chat_input_send_button")
		}
		if err := (base{}).SendPrompt(sess, prompt); err != nil {
			out, _ := sess.Evaluate(`JSON.stringify(window.__hits||[])`)
			t.Fatalf("发送: %v hits=%s", err, out)
		}
		if !sess.EvaluateBool(`(document.querySelector('[data-testid="send_message"][data-message-role="user"]')||{}).textContent.includes('雪山下的小木屋')`) {
			t.Fatal("发送后没有出现用户气泡")
		}
	})
}

func openFixture(t *testing.T, client *cdp.Client, url, hook, composer string) *cdp.Session {
	t.Helper()
	sess, err := client.NewSession(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Evaluate(hook); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Evaluate(composer); err != nil {
		t.Fatal(err)
	}
	return sess
}

func readLive(t *testing.T, sess *cdp.Session, expr string) liveShot {
	t.Helper()
	out, err := sess.Evaluate(expr)
	if err != nil {
		t.Fatal(err)
	}
	var live liveShot
	if err := cdp.ParseJSValue(out, &live); err != nil {
		t.Fatalf("解析检测结果: %v\n%s", err, out)
	}
	return live
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

var (
	scriptTag = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	linkTag   = regexp.MustCompile(`(?is)<link\b[^>]*>`)
)

func staticFixture(html string) string {
	html = scriptTag.ReplaceAllString(html, "")
	return linkTag.ReplaceAllString(html, "")
}
