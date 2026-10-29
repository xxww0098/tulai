package cdp

import (
	"strings"
	"testing"
)

func TestLoginQRScriptFindsDialogCode(t *testing.T) {
	for _, needle := range []string{"canvas", "二维码", "二维码已过期", "二维码已失效", "点击刷新", "scrollIntoView", "qrcode"} {
		if !strings.Contains(qrFindJS, needle) && !strings.Contains(qrRefreshJS, needle) {
			t.Fatalf("qr script missing %q", needle)
		}
	}
}

func TestLoginQRClip(t *testing.T) {
	clip, ok := loginQRClip(12, 40, 180, 180)
	if !ok || clip["x"] != 12.0 || clip["width"] != 180.0 || clip["scale"] != 1 {
		t.Fatalf("clip = %#v ok=%v", clip, ok)
	}
	clip, ok = loginQRClip(-10, -4, 100, 90)
	if !ok || clip["x"] != 0.0 || clip["y"] != 0.0 || clip["width"] != 90.0 || clip["height"] != 86.0 {
		t.Fatalf("clamped = %#v ok=%v", clip, ok)
	}
	if _, ok := loginQRClip(0, 0, 10, 10); ok {
		t.Fatal("tiny box should be dropped")
	}
}
