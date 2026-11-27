package fingerprint

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
)

type stubExecutor func(ctx context.Context, method string, params, result any) error

func (fn stubExecutor) Execute(ctx context.Context, method string, params, result any) error {
	return fn(ctx, method, params, result)
}

func TestPageEvaluateAndLocation(t *testing.T) {
	ctx := cdp.WithExecutor(t.Context(), stubExecutor(func(_ context.Context, method string, params, result any) error {
		if method != "Runtime.evaluate" {
			t.Fatalf("unexpected method: %s", method)
		}
		p, ok := params.(*runtime.EvaluateParams)
		if !ok {
			t.Fatalf("unexpected params type: %T", params)
		}
		var raw string
		switch p.Expression {
		case `document.location.toString()`:
			raw = `{"result":{"type":"string","value":"https://example.com/path"}}`
		case `({ok:true,n:3})`:
			raw = `{"result":{"type":"object","value":{"ok":true,"n":3}}}`
		case `void 0`:
			raw = `{"result":{"type":"undefined"}}`
		case `null`:
			raw = `{"result":{"type":"object","subtype":"null"}}`
		case `throw new Error("boom")`:
			raw = `{"result":{"type":"object"},"exceptionDetails":{"exceptionId":1,"text":"Uncaught Error: boom","lineNumber":0,"columnNumber":0}}`
		default:
			t.Fatalf("unexpected expression: %s", p.Expression)
		}
		return json.Unmarshal([]byte(raw), result)
	}))

	var loc string
	if err := Run(ctx, Location(&loc)); err != nil || loc != "https://example.com/path" {
		t.Fatalf("Location = %q, err = %v", loc, err)
	}

	var obj struct {
		OK bool `json:"ok"`
		N  int  `json:"n"`
	}
	if err := Run(ctx, Evaluate(`({ok:true,n:3})`, &obj)); err != nil || !obj.OK || obj.N != 3 {
		t.Fatalf("Evaluate struct = %+v, err = %v", obj, err)
	}

	if err := Run(ctx, Evaluate(`void 0`, nil)); err != nil {
		t.Fatalf("Evaluate with nil result failed: %v", err)
	}

	var raw []byte
	if err := Run(ctx, Evaluate(`({ok:true,n:3})`, &raw)); err != nil || string(raw) != `{"ok":true,"n":3}` {
		t.Fatalf("Evaluate *[]byte = %q, err = %v", raw, err)
	}

	var str string
	if err := Run(ctx, Evaluate(`void 0`, &str)); !errors.Is(err, ErrJSUndefined) {
		t.Fatalf("Evaluate undefined into string = %v, want ErrJSUndefined", err)
	}
	if err := Run(ctx, Evaluate(`null`, &str)); !errors.Is(err, ErrJSNull) {
		t.Fatalf("Evaluate null into string = %v, want ErrJSNull", err)
	}

	var slice []string
	if err := Run(ctx, Evaluate(`null`, &slice)); err != nil || slice != nil {
		t.Fatalf("Evaluate null into slice = %v, err = %v", slice, err)
	}

	if err := Run(ctx, Evaluate(`throw new Error("boom")`, &str)); err == nil {
		t.Fatal("expected JS exception error")
	}
}

func TestPageSleep(t *testing.T) {
	if err := Run(t.Context(), Sleep(5*time.Millisecond)); err != nil {
		t.Fatalf("Sleep failed: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Run(ctx, Sleep(time.Hour)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Sleep = %v, want context.Canceled", err)
	}
}
