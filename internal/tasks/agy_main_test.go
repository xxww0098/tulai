package tasks

import (
	"os"
	"testing"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/agytest"
)

var agyFixtureBin string

func TestMain(m *testing.M) {
	agytest.RunCommand()
	var err error
	agyFixtureBin, err = os.Executable()
	if err != nil {
		panic(err)
	}
	// Account-switch integration fixtures use files, never the user's keychain.
	agy.Keyring = nil
	os.Exit(m.Run())
}
