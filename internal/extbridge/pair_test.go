package extbridge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPairServerReturnsTokenOnLoopback(t *testing.T) {
	p, err := StartPair(func() PairInfo { return PairInfo{Token: "abc123abc123abc123abc123abc123ab", Port: "18770"} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	resp, err := http.Get("http://" + p.Addr() + PairPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["product"] != "xbs" || body["token"] != "abc123abc123abc123abc123abc123ab" || body["port"] != "18770" {
		t.Fatalf("body %#v", body)
	}
	if p.LastHit().IsZero() || time.Since(p.LastHit()) > time.Minute {
		t.Fatal("pair hit was not recorded")
	}
}

func TestPairServerRejectsWeakTokenAndRemote(t *testing.T) {
	p := &PairServer{info: func() PairInfo { return PairInfo{Token: DefaultExtToken, Port: "18765"} }}
	req := httptest.NewRequest(http.MethodGet, PairPath, nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	p.serveHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("weak token status %d", rec.Code)
	}

	p.info = func() PairInfo { return PairInfo{Token: "abc123abc123abc123abc123abc123ab", Port: "18765"} }
	req = httptest.NewRequest(http.MethodGet, PairPath, nil)
	req.RemoteAddr = "8.8.8.8:9"
	rec = httptest.NewRecorder()
	p.serveHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("remote status %d body %s", rec.Code, body)
	}
}

func TestHelloAccountOptional(t *testing.T) {
	raw, err := json.Marshal(map[string]string{"token": "t", "account": " profile-1 ", "accountName": " 小号 "})
	if err != nil {
		t.Fatal(err)
	}
	id, name := HelloAccount(Envelope{Payload: raw})
	if id != "profile-1" || name != "小号" {
		t.Fatalf("account %q %q", id, name)
	}
	if id, name := HelloAccount(Envelope{Payload: []byte(`{"token":"t"}`)}); id != "" || name != "" {
		t.Fatalf("old hello should not invent an account: %q %q", id, name)
	}
	if !WeakToken(" dev-token ") || !WeakToken("  ") || WeakToken("abc") {
		t.Fatal("weak token")
	}
}
