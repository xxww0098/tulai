package ui

import (
	"os"
	"testing"

	"github.com/xxww0098/tulai/internal/agy"
)

func TestMain(m *testing.M) {
	// UI fixtures must not detect or modify the user's actual OAuth credential.
	agy.Keyring = nil
	os.Exit(m.Run())
}
