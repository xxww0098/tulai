package agy

import (
	"os"
	"runtime"
	"testing"

	"github.com/xxww0098/tulai/internal/agytest"
)

func TestMain(m *testing.M) {
	agytest.RunCommand()
	// Fake CLI tests must never read or replace a real account's system credential.
	Keyring = nil
	os.Exit(m.Run())
}

func assertCredentialFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("OAuth credential is not a regular file: %v", info.Mode())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("OAuth credential permissions are %o, want 600", info.Mode().Perm())
	}
}
