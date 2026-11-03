package cdp

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufoxtest"
)

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
