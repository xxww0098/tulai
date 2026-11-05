package agy

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Fake CLI tests must never read or replace a real account's system credential.
	Keyring = nil
	os.Exit(m.Run())
}
