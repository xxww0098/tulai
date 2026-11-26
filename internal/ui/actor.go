package ui

import (
	"context"
	"strings"
	"time"
)

const browserSyncInterval = 2 * time.Second

// StartActor launches the single-goroutine controller actor that serializes all
// MyGo IPC state reads/writes and queued postUI callbacks without requiring
// fine-grained locks across *_ops.go.
func (a *App) StartActor() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.actorCh != nil {
		a.mu.Unlock()
		return
	}
	callCh := make(chan func())
	wakeCh := make(chan struct{}, 1)
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	a.actorCh = callCh
	a.actorWake = wakeCh
	a.actorStop = stopCh
	a.actorDone = doneCh
	pending := len(a.uiUpdates) > 0
	a.mu.Unlock()

	if pending {
		select {
		case wakeCh <- struct{}{}:
		default:
		}
	}

	go func() {
		defer close(doneCh)
		syncTicker := time.NewTicker(browserSyncInterval)
		defer syncTicker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case fn := <-callCh:
				if fn != nil {
					fn()
				}
			case <-wakeCh:
				a.flushUIUpdates()
			case <-syncTicker.C:
				a.syncRunningBrowsers()
			}
		}
	}()
}

// StopActor halts the controller actor goroutine and waits for it to drain/exit.
// Safe to call multiple times or when no actor was started.
func (a *App) StopActor() {
	if a == nil {
		return
	}
	a.mu.Lock()
	stopCh := a.actorStop
	doneCh := a.actorDone
	a.actorCh = nil
	a.actorWake = nil
	a.actorStop = nil
	a.actorDone = nil
	a.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (a *App) actorActive() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	active := a.actorCh != nil
	a.mu.Unlock()
	return active
}

// call executes fn on the controller actor goroutine when running, or directly
// on the caller goroutine in headless unit tests where StartActor was not called.
// NEVER invoke blocking OS dialogs inside fn.
func (a *App) call(ctx context.Context, fn func()) error {
	if a == nil || fn == nil {
		return nil
	}
	a.mu.Lock()
	callCh := a.actorCh
	stopCh := a.actorStop
	a.mu.Unlock()
	if callCh == nil {
		fn()
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	wrapped := func() {
		defer close(done)
		fn()
	}
	select {
	case callCh <- wrapped:
	case <-stopCh:
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-stopCh:
		return context.Canceled
	case <-ctx.Done():
		return ctx.Err()
	}
}

// callValue runs fn on the controller actor and returns its computed value.
func callValue[T any](a *App, ctx context.Context, fn func() T) (T, error) {
	var zero T
	if a == nil || fn == nil {
		return zero, nil
	}
	var out T
	err := a.call(ctx, func() {
		out = fn()
	})
	if err != nil {
		return zero, err
	}
	return out, nil
}

// startEventPump drains coalesced topic notifications from subscribeEvents and
// forwards each deduplicated batch to host.EmitTopics.
func (a *App) startEventPump(host DesktopHost) func() {
	if a == nil || host == nil {
		return func() {}
	}
	id, ch := a.subscribeEvents()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			first, ok := <-ch
			if !ok {
				return
			}
			seen := map[string]bool{first: true}
			order := []string{first}
		drain:
			for {
				select {
				case next, ok := <-ch:
					if !ok {
						break drain
					}
					if !seen[next] {
						seen[next] = true
						order = append(order, next)
					}
				default:
					break drain
				}
			}
			if !a.isQuitting() {
				host.EmitTopics(strings.Join(order, ","))
			}
		}
	}()
	return func() {
		a.unsubscribeEvents(id)
		<-done
	}
}
