package cdp

import (
	"github.com/xxww0098/tulai/internal/camoufoxtest"
	"testing"
)

func newFixtureSession(t *testing.T, rawURL string) *Session {
	t.Helper()
	_, endpoint := camoufoxtest.Start(t, "about:blank")
	client, err := NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	session, err := client.NewSession(rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return session
}
