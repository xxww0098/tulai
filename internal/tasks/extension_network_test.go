package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

func TestExtensionNetworkEventsOnlyCoolMatchingAccount(t *testing.T) {
	st := openTaskStore(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	port := freePort(t)
	st.Set(extbridge.SettingExtPort, port)
	st.Set(extbridge.SettingExtToken, "test-token")
	e := NewEngine(st, nil, nil, t.TempDir())
	t.Cleanup(func() { e.CloseSessions() })
	if _, err := e.extensionServer(); err != nil {
		t.Fatal(err)
	}
	loggedIn := true
	addr := "127.0.0.1:" + port
	first := dialExtensionHello(t, addr, "test-token", extbridge.PlatformDoubao, "first", &loggedIn)
	defer first.Close()
	second := dialExtensionHello(t, addr, "test-token", extbridge.PlatformDoubao, "second", &loggedIn)
	defer second.Close()
	if rows := waitBrowserCount(st, 2, time.Second); len(rows) != 2 {
		t.Fatalf("accounts = %+v", rows)
	}
	send := func(body map[string]any) {
		t.Helper()
		body["name"] = "network"
		raw, _ := json.Marshal(body)
		if err := writeClient(first, extbridge.Envelope{V: extbridge.Version, ID: "network-" + time.Now().String(), Type: extbridge.TypeEvent, TS: time.Now().UnixMilli(), Platform: extbridge.PlatformDoubao, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	waitHealth := func(want string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			rows := st.Browsers()
			if len(rows) == 2 {
				var got, other store.Browser
				for _, row := range rows {
					if row.ExtAccount == "first" {
						got = row
					} else {
						other = row
					}
				}
				if got.Health == want {
					if other.Health != store.HealthReady || !other.IsLoggedIn {
						t.Fatalf("other account changed: %+v", other)
					}
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("health did not become %s: %+v", want, st.Browsers())
	}
	before := len(logging.Log.Entries())
	send(map[string]any{"status": 429, "code": "QuotaExceeded", "message": "private text"})
	deadline := time.Now().Add(time.Second)
	observed := false
	for time.Now().Before(deadline) && !observed {
		entries := logging.Log.Entries()
		for _, entry := range entries[before:] {
			if strings.Contains(entry.Text, "HTTP 429，业务码 QuotaExceeded") {
				observed = true
			}
			if strings.Contains(entry.Text, "private text") {
				t.Fatal("response text leaked to log")
			}
		}
		if !observed {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !observed {
		t.Fatal("network status was not logged")
	}
	waitHealth(store.HealthReady)
	send(map[string]any{"status": 429, "code": "42901", "kind": "rate"})
	waitHealth(store.HealthCooling)
	send(map[string]any{"status": 200, "code": "12345", "kind": "quota"})
	waitHealth(store.HealthExhausted)
}

func TestExtensionNetworkFaultAfterSubmitDoesNotResend(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, t.TempDir())
	calls := 0
	task := e.execute(context.Background(), Task{Index: 1, Platform: "豆包", BrowserID: first.ID}, func(_ context.Context, task *Task) error {
		calls++
		task.submitted = true
		return mapExtErr(&extbridge.ProtoError{Code: extbridge.ESendUncertain, Message: "平台响应提示账号受限，本次已发送，请检查网页结果"}, extAck{}, "豆包")
	})
	if calls != 1 || task.Status != StatusFailed || task.BrowserID != first.ID || !task.submitted {
		t.Fatalf("submitted task must not be replayed: calls=%d task=%+v", calls, task)
	}
}

func TestBoundExtensionNetworkFaultUsesOriginalAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	port := freePort(t)
	st.Set(extbridge.SettingExtPort, port)
	st.Set(extbridge.SettingExtToken, "test-token")
	e := NewEngine(st, nil, nil, t.TempDir())
	t.Cleanup(func() { e.CloseSessions() })
	if _, err := e.extensionServer(); err != nil {
		t.Fatal(err)
	}
	loggedIn := true
	conn := dialExtensionHello(t, "127.0.0.1:"+port, "test-token", extbridge.PlatformDoubao, e.extensionAccountKey(first.ID), &loggedIn)
	defer conn.Close()
	raw, _ := json.Marshal(map[string]any{"name": "network", "status": 200, "kind": "quota"})
	if err := writeClient(conn, extbridge.Envelope{V: extbridge.Version, ID: "quota", Type: extbridge.TypeEvent, TS: time.Now().UnixMilli(), Platform: extbridge.PlatformDoubao, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, _ := e.browserByID(first.ID)
		other, _ := e.browserByID(second.ID)
		if got.Health == store.HealthExhausted {
			if other.Health != store.HealthReady || len(st.Browsers()) != 2 {
				t.Fatalf("other account changed or duplicate created: %+v", st.Browsers())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("bound account was not marked exhausted: %+v", st.Browsers())
}
