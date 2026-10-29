package cdp

import (
	"testing"

	"github.com/chromedp/cdproto/runtime"
	"github.com/go-json-experiment/json/jsontext"
)

func TestConsoleMessageDropsNull(t *testing.T) {
	noise := consoleMessage([]*runtime.RemoteObject{
		{Type: runtime.TypeObject},
		{Type: runtime.TypeObject, Subtype: runtime.SubtypeNull, Value: jsontext.Value("null")},
		{Type: runtime.TypeUndefined},
		{Type: runtime.TypeString, Value: jsontext.Value(`"null"`)},
		{Type: runtime.TypeString, Value: jsontext.Value(`"undefined"`)},
	})
	if noise != "" {
		t.Fatalf("null console args leaked: %q", noise)
	}
	got := consoleMessage([]*runtime.RemoteObject{
		{Type: runtime.TypeObject},
		{Type: runtime.TypeString, Value: jsontext.Value(`"验证码监听已启动"`)},
	})
	if got != "验证码监听已启动" {
		t.Fatalf("useful console text = %q", got)
	}
}
