package tasks

import (
	"os"
	"testing"

	"github.com/xxww0098/tulai/internal/agy"
)

func TestMain(m *testing.M) {
	// Account-switch integration fixtures use files, never the user's keychain.
	agy.Keyring = nil
	os.Exit(m.Run())
}
