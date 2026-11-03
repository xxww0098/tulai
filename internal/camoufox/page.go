package camoufox

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
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

type Action interface{ Do(context.Context) error }
type action func(context.Context) error

func (fn action) Do(ctx context.Context) error { return fn(ctx) }

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
	return action(func(ctx context.Context) error {
		return cdp.Execute(ctx, "Page.navigate", map[string]any{"url": url}, nil)
	})
}

func NewPage(ctx, parent context.Context, isolated bool) (context.Context, context.CancelFunc, error) {
	owner, ok := parent.Value(pageKey{}).(Page)
	if !ok {
		return nil, nil, errors.New("没有 Camoufox 页面")
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
