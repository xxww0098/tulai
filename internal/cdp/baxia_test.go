package cdp

import "testing"

func TestParentFrameOffsetMatchesPunishURL(t *testing.T) {
	pages := []sliderPage{{
		id: "page-1",
		probe: sliderProbe{Iframes: []sliderFrame{{
			Src: "https://www.qianwen.com/_____tmd_____/punish?x5secdata=abc",
			X:   18,
			Y:   26,
			W:   320,
			H:   240,
		}}},
	}}
	id, x, y, ok := parentFrameOffset("https://www.qianwen.com/_____tmd_____/punish?x5secdata=abc", pages)
	if !ok || id != "page-1" || x != 18 || y != 26 {
		t.Fatalf("offset id=%s x=%v y=%v ok=%v", id, x, y, ok)
	}
}

func TestParentFrameOffsetSingleIframeFallback(t *testing.T) {
	pages := []sliderPage{{
		id:    "page-1",
		probe: sliderProbe{Iframes: []sliderFrame{{Src: "https://g.alicdn.com/captcha.html", X: 4, Y: 9}}},
	}}
	id, x, y, ok := parentFrameOffset("https://other.example/widget", pages)
	if !ok || id != "page-1" || x != 4 || y != 9 {
		t.Fatalf("fallback id=%s x=%v y=%v ok=%v", id, x, y, ok)
	}
}

func TestCaptchaFrameURL(t *testing.T) {
	if !captchaFrameURL("https://chat2.qianwen.com/api/v2/chat/_____tmd_____/punish?x=1") {
		t.Fatal("punish")
	}
	if captchaFrameURL("https://www.qianwen.com/chat/abc") {
		t.Fatal("plain chat should not match")
	}
}

func TestChildFrameOffsetUsesListedIframe(t *testing.T) {
	page := sliderPage{
		id: "p",
		probe: sliderProbe{Iframes: []sliderFrame{{
			Src: "https://chat2.qianwen.com/api/v2/chat/_____tmd_____/punish?x=1",
			X:   10, Y: 20,
		}}},
	}
	x, y := childFrameOffset("https://chat2.qianwen.com/api/v2/chat/_____tmd_____/punish?x=1", page)
	if x != 10 || y != 20 {
		t.Fatalf("got %v %v", x, y)
	}
}
