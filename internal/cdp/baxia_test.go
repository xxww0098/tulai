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
