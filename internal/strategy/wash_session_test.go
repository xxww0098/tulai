package strategy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/camoufoxtest"
	"github.com/xxww0098/tulai/internal/cdp"
)

func TestWashSessionReusesPromptBoundary(t *testing.T) {
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", fixtureCSP)
		_, _ = w.Write([]byte("<!doctype html><html><body></body></html>"))
	}))
	defer fixture.Close()
	local, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			_, _ = w.Write([]byte("<!doctype html><html><body></body></html>"))
			return
		}
		upstream, err := net.Dial("tcp", local.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(upstream, buffer) }()
		go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(client, upstream) }()
	}))
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	geoip := false
	process, err := camoufox.Start(ctx, camoufoxtest.ConfigDir(t), camoufox.Options{Profile: t.TempDir(), Headless: true, Proxy: proxy.URL, GeoIP: &geoip, IgnoreHTTPSErrors: true}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	const port = 61274
	camoufox.Register(port, process)
	defer camoufox.Unregister(port, process)
	client, err := cdp.NewClient(camoufox.Endpoint(port))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	const chatURL = "https://www.doubao.com/chat/test"
	sess := openFixture(t, client, chatURL, hookJS, composerJS)
	defer sess.Close()
	if !sess.EvaluateBool(`(() => {
		window.__tulaiPageHost = 'www.doubao.com';
		window.makeTurn = function (name) {
			var user = document.createElement('div');
			user.setAttribute('data-role', 'user'); user.textContent = '重复的洗图提示词';
			var reply = document.createElement('div');
			reply.setAttribute('data-role', 'assistant'); reply.setAttribute('data-finished', 'true');
			var card = document.createElement('div'); card.setAttribute('data-testid', 'mdbox_image');
			var img = document.createElement('img'); img.width = 512; img.height = 512;
			img.src = 'data:image/png;base64,' + name;
			card.appendChild(img); reply.appendChild(card);
			document.body.appendChild(user); document.body.appendChild(reply);
		};
		makeTurn('b2xk'); return true;
	})()`) {
		t.Fatal("create prior turn")
	}
	if got := ReusableDoubaoChat(sess); got != chatURL {
		t.Fatalf("finished conversation should be reusable: %q", got)
	}
	if !sess.EvaluateBool(`(() => {
		var stop = document.createElement('button'); stop.textContent = '停止生成'; stop.id = 'stop';
		document.body.appendChild(stop); return true;
	})()`) {
		t.Fatal("create stop button")
	}
	if got := ReusableDoubaoChat(sess); got != "" {
		t.Fatalf("a visible stop button must block reuse: %q", got)
	}
	if !sess.EvaluateBool(`(() => { document.getElementById('stop').remove(); return true; })()`) {
		t.Fatal("remove stop button")
	}
	if err := PrepareDoubaoTurns(sess); err != nil {
		t.Fatal(err)
	}
	if err := (base{}).MarkTask(sess); err != nil {
		t.Fatal(err)
	}
	if err := MarkDoubaoTurn(sess, "重复的洗图提示词"); err != nil {
		t.Fatal(err)
	}
	if !sess.EvaluateBool(`(() => { document.body.innerHTML = ''; makeTurn('b2xk'); return true; })()`) {
		t.Fatal("redraw prior turn")
	}
	if live := readLive(t, sess, `JSON.stringify(__tulaiLiveImages())`); len(live.Images) != 0 {
		t.Fatalf("old reply was attributed to the new task: %+v", live)
	}
	if got := ReusableDoubaoChat(sess); got != "" {
		t.Fatalf("a missing reply must not permit reuse: %q", got)
	}
	if !sess.EvaluateBool(`(() => { makeTurn('bmV3'); return true; })()`) {
		t.Fatal("create new turn")
	}
	live := readLive(t, sess, `JSON.stringify(__tulaiLiveImages())`)
	if !live.Done || len(live.Images) != 1 || live.Images[0].URL != "data:image/png;base64,bmV3" {
		t.Fatalf("only the new reply should be collected: %+v", live)
	}
	if got := ReusableDoubaoChat(sess); got != chatURL {
		t.Fatalf("new completed turn should be reusable: %q", got)
	}
}
