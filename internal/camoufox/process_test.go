package camoufox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func TestCallReplyCancellationAndExit(t *testing.T) {
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	p := &Process{in: inputWriter, ctx: life, cancel: cancel, pending: map[int64]chan reply{}, listeners: map[int64]func(Event){}}
	t.Cleanup(func() { inputReader.Close(); inputWriter.Close(); outputReader.Close(); outputWriter.Close() })
	requests := make(chan map[string]any, 4)
	go func() {
		scanner := bufio.NewScanner(inputReader)
		for scanner.Scan() {
			var r map[string]any
			if json.Unmarshal(scanner.Bytes(), &r) == nil {
				requests <- r
			}
		}
	}()
	go p.read(outputReader)
	result := make(chan error, 1)
	var got struct{ Value int }
	go func() { result <- p.Call(context.Background(), "page", "ping", nil, &got) }()
	request := <-requests
	data, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]int{"value": 42}})
	_, _ = outputWriter.Write(append(data, '\n'))
	if err := <-result; err != nil || got.Value != 42 {
		t.Fatalf("reply: %+v %v", got, err)
	}
	ctx, stop := context.WithCancel(context.Background())
	go func() { result <- p.Call(ctx, "page", "wait", nil, nil) }()
	<-requests
	stop()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if request := <-requests; request["method"] != "cancel" {
		t.Fatal(request)
	}
	go func() { result <- p.Call(context.Background(), "page", "ping", nil, nil) }()
	<-requests
	outputWriter.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("EOF succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("exit left a pending command")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pending) != 0 {
		t.Fatal("pending calls leaked")
	}
}
