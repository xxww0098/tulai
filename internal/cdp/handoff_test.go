package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSessionHandOffPreservesPageUntilClose(t *testing.T) {
	transport, server, ctx := testEgoTransport(t)
	requests := make(chan string, 4)
	peerDone := make(chan struct{})
	go func() {
		defer close(peerDone)
		decoder := json.NewDecoder(server)
		for {
			var request struct {
				ID     uint64
				Method string
				Params json.RawMessage
			}
			if decoder.Decode(&request) != nil {
				return
			}
			requests <- request.Method
			response := egoResponse{ID: request.ID, Result: json.RawMessage("null")}
			if request.Method != "handoff" || string(request.Params) != "null" {
				response.Error = "unexpected request"
			}
			if json.NewEncoder(server).Encode(response) != nil {
				return
			}
		}
	}()
	processDone := make(chan struct{})
	transport.processDone = processDone
	transport.cmd = &exec.Cmd{Cancel: func() error { close(processDone); return nil }}
	s := &Session{ego: transport, ctx: ctx, cancel: func() {}}
	if err := s.HandOff(); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil || s.closed {
		t.Fatal("handoff canceled or closed the session")
	}
	select {
	case <-processDone:
		t.Fatal("handoff stopped the controller before Close")
	default:
	}
	s.Close()
	select {
	case <-processDone:
	default:
		t.Fatal("Close did not wait for controller cleanup")
	}
	select {
	case <-peerDone:
	case <-time.After(time.Second):
		t.Fatal("controller connection remained open")
	}
	if len(requests) != 1 || <-requests != "handoff" {
		t.Fatal("handoff/Close sent an unexpected page operation")
	}
}

func TestSessionHandOffChromeNoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Session{ctx: ctx}).HandOff(); err != nil {
		t.Fatalf("Chrome handoff: %v", err)
	}
}

func TestSessionHandOffFailures(t *testing.T) {
	for _, mode := range []string{"rejected", "deadline", "canceled", "already canceled"} {
		t.Run(mode, func(t *testing.T) {
			transport, server, base := testEgoTransport(t)
			ctx, cancel := context.WithCancel(base)
			defer cancel()
			if mode == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(base, 30*time.Millisecond)
				defer stop()
			}
			if mode == "already canceled" {
				cancel()
			}
			seen := make(chan struct{})
			go func() {
				var request struct {
					ID uint64
				}
				if json.NewDecoder(server).Decode(&request) != nil {
					return
				}
				close(seen)
				if mode == "rejected" {
					json.NewEncoder(server).Encode(egoResponse{ID: request.ID, Error: "handoff rejected"})
				}
			}()
			s := &Session{ego: transport, ctx: ctx}
			result := make(chan error, 1)
			go func() { result <- s.HandOff() }()
			if mode == "canceled" {
				select {
				case <-seen:
					cancel()
				case <-time.After(time.Second):
					t.Fatal("handoff request not received")
				}
			}
			var err error
			select {
			case err = <-result:
			case <-time.After(time.Second):
				t.Fatal("handoff did not respect parent deadline/cancellation")
			}
			switch mode {
			case "rejected":
				if err == nil || !strings.Contains(err.Error(), "handoff rejected") || ctx.Err() != nil {
					t.Fatalf("handoff error lost or session canceled: %v, %v", err, ctx.Err())
				}
			case "deadline":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline error: %v", err)
				}
			default:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation error: %v", err)
				}
			}
			if mode == "already canceled" {
				select {
				case <-seen:
					t.Fatal("canceled session sent handoff")
				default:
				}
			}
		})
	}
}
