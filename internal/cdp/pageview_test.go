package cdp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/camoufoxtest"
)

func TestPageViewKeepsBusyTaskPage(t *testing.T) {
	started, finished := make(chan struct{}, 1), make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/started":
			started <- struct{}{}
		case "/finished":
			finished <- struct{}{}
		default:
			fmt.Fprint(w, `<!doctype html><title>busy task</title>`)
		}
	}))
	defer server.Close()
	proc, endpoint := camoufoxtest.Start(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	view, err := AttachPageView(ctx, endpoint, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if view != nil {
			view.Close()
		}
	}()
	closed := make(chan struct{}, 1)
	unsubscribe := proc.Subscribe(func(event camoufox.Event) {
		if event.Event == "closed" && event.Target == proc.Page {
			closed <- struct{}{}
		}
	})
	defer unsubscribe()
	if err := proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{"expression": `(setTimeout(()=>{fetch('/started');const end=Date.now()+10000;while(Date.now()<end){};fetch('/finished')},100),true)`}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("task page did not become busy")
	}
	connectCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	err = OpenURLBackground(connectCtx, endpoint, server.URL)
	connectErr := connectCtx.Err()
	stop()
	if err != nil || connectErr != nil {
		t.Fatalf("connecting to an existing busy task page: %v, context: %v", err, connectErr)
	}
	frameCtx, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	_, err = view.Frame(frameCtx)
	stop()
	if err == nil {
		t.Fatal("busy page unexpectedly produced a frame")
	}
	probeCtx, cancelProbe := context.WithCancel(ctx)
	probeDone := make(chan error, 1)
	go func() {
		_, err := view.conn.pageHits(probeCtx)
		probeDone <- err
	}()
	time.Sleep(200 * time.Millisecond)
	cancelProbe()
	if err := <-probeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled page probe: %v", err)
	}
	loginCtx, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	_, err = view.LoginState(loginCtx)
	stop()
	if err == nil {
		t.Fatal("busy page unexpectedly returned login state")
	}
	view.Close()
	view = nil
	select {
	case <-finished:
	case <-closed:
		t.Fatal("viewer closed the busy task page")
	case <-time.After(15 * time.Second):
		t.Fatal("task page did not finish")
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	pages, err := conn.targets(ctx)
	if err != nil || len(pages) != 1 || pages[0].ID != proc.Page {
		t.Fatalf("task page was replaced: %+v, %v", pages, err)
	}
}

func TestPageViewControlsExistingPageAndDetaches(t *testing.T) {
	if testing.Short() {
		t.Skip("短测不启动浏览器")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>drawer fixture</title><body style="margin:0"><input style="position:absolute;left:20px;top:20px;width:300px;height:40px"><button style="position:absolute;left:350px;top:20px;width:100px;height:40px" onclick="document.title='clicked'">click</button><div style="height:350px;background:#ee2222"></div><div style="height:2000px;background:#2222ee"></div></body>`)
	}))
	defer srv.Close()
	_, endpoint := camoufoxtest.Start(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	view, err := AttachPageView(ctx, endpoint, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	before, err := view.conn.targets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	input := func(in PageInput) string {
		t.Helper()
		text, err := view.Input(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return text
	}
	input(PageInput{Kind: "mousePressed", X: 100, Y: 40, Button: "left", Buttons: 1})
	input(PageInput{Kind: "mouseReleased", X: 100, Y: 40, Button: "left"})
	input(PageInput{Kind: "text", Text: "你好，豆包 drawer"})
	input(PageInput{Kind: "selectAll"})
	if got := input(PageInput{Kind: "copy"}); got != "你好，豆包 drawer" {
		t.Fatalf("Chinese input/copy = %q", got)
	}
	input(PageInput{Kind: "keyDown", Key: "Backspace", VirtualKey: 8})
	input(PageInput{Kind: "keyUp", Key: "Backspace", VirtualKey: 8})
	input(PageInput{Kind: "text", Text: "再次输入"})
	input(PageInput{Kind: "selectAll"})
	if got := input(PageInput{Kind: "cut"}); got != "再次输入" {
		t.Fatalf("keyboard/cut = %q", got)
	}
	input(PageInput{Kind: "mousePressed", X: 400, Y: 40, Button: "left", Buttons: 1})
	input(PageInput{Kind: "mouseReleased", X: 400, Y: 40, Button: "left"})
	frame, err := view.Frame(ctx)
	if err != nil || frame.Title != "clicked" || frame.Width <= 0 || frame.Height <= 0 {
		t.Fatalf("click/frame = %+v %v", frame, err)
	}
	input(PageInput{Kind: "mouseWheel", X: 400, Y: 300, DY: 650})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		frame, err = view.Frame(ctx)
		if err == nil && frame.ScrollY > 500 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || frame.ScrollY <= 500 {
		t.Fatalf("scroll did not reach the page: y=%v err=%v", frame.ScrollY, err)
	}
	bmp, _, err := image.Decode(bytes.NewReader(frame.JPEG))
	if err != nil {
		t.Fatal(err)
	}
	r, _, b, _ := bmp.At(bmp.Bounds().Dx()/2, bmp.Bounds().Dy()/2).RGBA()
	if b <= r {
		t.Fatal("frame did not show the scrolled blue region")
	}
	id := view.target
	view.Close()
	reopened, err := AttachPageView(ctx, endpoint, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, _ := reopened.conn.targets(ctx)
	if reopened.target != id || len(after) != len(before) {
		t.Fatalf("viewer created/replaced a tab: %s -> %s, %d -> %d", id, reopened.target, len(before), len(after))
	}
}
