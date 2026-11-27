package fingerprint

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
)

// Page implements tulai's existing typed command vocabulary over the launcher
// pipe. The cdproto types are data contracts, not a connection to Chromium.
type Page struct {
	Process *Process
	ID      string
}

func (p Page) Execute(ctx context.Context, method string, params, result any) error {
	return p.Process.Call(ctx, p.ID, method, params, result)
}

type pageKey struct{}

func PageContext(ctx context.Context, p *Process, id string) context.Context {
	page := Page{Process: p, ID: id}
	return context.WithValue(cdp.WithExecutor(ctx, page), pageKey{}, page)
}

var (
	ErrJSUndefined = errors.New("encountered undefined value")
	ErrJSNull      = errors.New("encountered null value")
)

type Action interface{ Do(context.Context) error }
type ActionFunc func(context.Context) error

func (fn ActionFunc) Do(ctx context.Context) error { return fn(ctx) }

func Run(ctx context.Context, actions ...Action) error {
	for _, item := range actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := item.Do(ctx); err != nil {
			return err
		}
	}
	return nil
}

func Navigate(url string) Action {
	return ActionFunc(func(ctx context.Context) error {
		return cdp.Execute(ctx, "Page.navigate", map[string]any{"url": url}, nil)
	})
}

func Evaluate(expression string, res any) Action {
	return ActionFunc(func(ctx context.Context) error {
		p := runtime.Evaluate(expression)
		if _, ok := res.(**runtime.RemoteObject); !ok {
			p = p.WithReturnByValue(true)
		}
		v, exp, err := p.Do(ctx)
		if err != nil {
			return err
		}
		if exp != nil {
			return exp
		}
		return parseRemoteObject(v, res)
	})
}

func parseRemoteObject(v *runtime.RemoteObject, res any) error {
	if res == nil {
		return nil
	}
	if v == nil {
		return ErrJSUndefined
	}
	switch x := res.(type) {
	case **runtime.RemoteObject:
		*x = v
		return nil
	case *[]byte:
		*x = v.Value
		return nil
	}
	value := v.Value
	if value == nil {
		rv := reflect.ValueOf(res)
		if rv.Kind() == reflect.Ptr {
			switch rv.Elem().Kind() {
			case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
			default:
				if v.Type == runtime.TypeUndefined {
					return ErrJSUndefined
				}
				return ErrJSNull
			}
		}
		value = []byte("null")
	}
	return json.Unmarshal(value, res)
}

func Location(urlstr *string) Action {
	if urlstr == nil {
		panic("urlstr cannot be nil")
	}
	return Evaluate(`document.location.toString()`, urlstr)
}

func Sleep(d time.Duration) Action {
	return ActionFunc(func(ctx context.Context) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
}

func NewPage(ctx, parent context.Context, isolated bool) (context.Context, context.CancelFunc, error) {
	owner, ok := parent.Value(pageKey{}).(Page)
	if !ok {
		return nil, nil, errors.New("没有浏览器页面")
	}
	var result struct {
		TargetID string `json:"targetId"`
	}
	if err := owner.Process.Call(ctx, "", "Target.createTarget", map[string]any{"isolated": isolated}, &result); err != nil {
		return nil, nil, err
	}
	life, cancel := context.WithCancel(parent)
	var once sync.Once
	closePage := func() {
		once.Do(func() {
			cancel()
			closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = owner.Process.Call(closeCtx, result.TargetID, "Page.close", nil, nil)
		})
	}
	context.AfterFunc(life, closePage)
	return PageContext(life, owner.Process, result.TargetID), closePage, nil
}

func ListenTarget(ctx context.Context, callback func(any)) {
	page, ok := ctx.Value(pageKey{}).(Page)
	if !ok {
		return
	}
	unsubscribe := page.Process.Subscribe(func(event Event) {
		if event.Target != page.ID || ctx.Err() != nil {
			return
		}
		var value any
		switch event.Event {
		case "Network.requestWillBeSent":
			value = &network.EventRequestWillBeSent{}
		case "Network.responseReceived":
			value = &network.EventResponseReceived{}
		case "Network.loadingFinished":
			value = &network.EventLoadingFinished{}
		case "Network.loadingFailed":
			value = &network.EventLoadingFailed{}
		case "Fetch.requestPaused":
			value = &fetch.EventRequestPaused{}
		default:
			return
		}
		if json.Unmarshal(event.Data, value) == nil {
			callback(value)
		}
	})
	context.AfterFunc(ctx, unsubscribe)
}
