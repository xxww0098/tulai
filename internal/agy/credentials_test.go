package agy

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryCredentials struct {
	mu       sync.Mutex
	data     []byte
	writeErr error
}

func (m *memoryCredentials) Read(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return bytes.Clone(m.data), nil
}

func (m *memoryCredentials) Write(_ context.Context, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.data = bytes.Clone(data)
	return nil
}

func useMemoryCredentials(t *testing.T, data []byte) *memoryCredentials {
	t.Helper()
	previous := Keyring
	memory := &memoryCredentials{data: bytes.Clone(data)}
	Keyring = memory
	t.Cleanup(func() { Keyring = previous })
	return memory
}

func TestNormalizeCredential(t *testing.T) {
	want := []byte(testCredential)
	for _, data := range [][]byte{want, []byte("go-keyring-base64:" + base64.StdEncoding.EncodeToString(want))} {
		got, err := normalizeCredential(data)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("OAuth payload was not preserved", err)
		}
	}
	for _, data := range []string{"", `{}`, `{"token":{"access_token":"sensitive-access"}}`, "go-keyring-base64:!", `{"token":{"refresh_token":42}}`} {
		_, err := normalizeCredential([]byte(data))
		if !errors.Is(err, errCredential) || strings.Contains(err.Error(), "sensitive-access") {
			t.Fatal("invalid credential was accepted or exposed")
		}
	}
}

func TestCLISwitchSavesRefreshedOAuthAndSerializes(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeToken(t, first)
	secondToken := []byte(strings.ReplaceAll(testCredential, "test-refresh", "second-refresh"))
	if err := writeCredential(second, secondToken); err != nil {
		t.Fatal(err)
	}
	keyring := useMemoryCredentials(t, secondToken)
	s, err := beginCLI(context.Background(), first, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.unlock()
		}
	}()
	active, _ := keyring.Read(context.Background())
	if !bytes.Equal(active, []byte(testCredential)) {
		t.Fatal("CLI did not switch to the first account")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := beginCLI(ctx, second, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overlapping switch = %v", err)
	}
	refreshed := []byte(strings.ReplaceAll(testCredential, "test-access", "refreshed-access"))
	if err := keyring.Write(context.Background(), refreshed); err != nil {
		t.Fatal(err)
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s.unlock()
	s = nil
	saved, err := os.ReadFile(TokenPath(first))
	if err != nil || !bytes.Equal(saved, refreshed) {
		t.Fatal("refreshed OAuth was not saved", err)
	}
	info, err := os.Stat(TokenPath(first))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("OAuth file must be private", err)
	}
	s, err = beginCLI(context.Background(), second, false)
	if err != nil {
		t.Fatal(err)
	}
	active, _ = keyring.Read(context.Background())
	if !bytes.Equal(active, secondToken) {
		t.Fatal("second account reused the first account's OAuth")
	}
	if data, _ := os.ReadFile(TokenPath(first)); !bytes.Equal(data, refreshed) {
		t.Fatal("switch changed the other account")
	}
}

func TestCLISavesFallbackRefreshAndRestoresCanceledLogin(t *testing.T) {
	home := t.TempDir()
	writeToken(t, home)
	previous := []byte(strings.ReplaceAll(testCredential, "test-refresh", "previous-refresh"))
	keyring := useMemoryCredentials(t, previous)
	s, err := beginCLI(context.Background(), home, false)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := []byte(strings.ReplaceAll(testCredential, "test-access", "file-refreshed-access"))
	if err := writeCredential(home, refreshed); err != nil {
		s.unlock()
		t.Fatal(err)
	}
	err = s.save()
	s.unlock()
	if err != nil {
		t.Fatal(err)
	}
	active, _ := keyring.Read(context.Background())
	if !bytes.Equal(active, refreshed) {
		t.Fatal("file refresh was not synchronized with the keyring")
	}
	if err := keyring.Write(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	s, err = beginCLI(context.Background(), home, true)
	if err != nil {
		t.Fatal(err)
	}
	if LoggedIn(home) {
		s.unlock()
		t.Fatal("login reused existing credentials")
	}
	err = s.restore()
	s.unlock()
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(TokenPath(home))
	active, _ = keyring.Read(context.Background())
	if !bytes.Equal(saved, refreshed) || !bytes.Equal(active, previous) {
		t.Fatal("canceled login lost the previous account")
	}
}

func TestCLISwitchFailureDoesNotStartProcess(t *testing.T) {
	home := t.TempDir()
	writeToken(t, home)
	keyring := useMemoryCredentials(t, []byte(testCredential))
	keyring.writeErr = errors.New("keyring denied")
	_, err := Run(context.Background(), Request{Bin: agyScript(t), Home: home, Prompt: "x"})
	if err == nil {
		t.Fatal("failed switch was accepted")
	}
	if _, err := os.Stat(filepath.Join(home, "last-prompt.txt")); !os.IsNotExist(err) {
		t.Fatal("CLI ran after credential switch failed")
	}
	if !LoggedIn(home) {
		t.Fatal("failed switch deleted the stored account")
	}
}

func TestImportKeyringCreatesIndependentSnapshot(t *testing.T) {
	src := t.TempDir()
	t.Setenv("HOME", src)
	t.Setenv("USERPROFILE", src)
	keyring := useMemoryCredentials(t, []byte(testCredential))
	found := DetectAt(src)
	if !found.CanImport() || !found.Keychain {
		t.Fatal("keyring-only account was not detected")
	}
	res, err := Import(context.Background(), ImportRequest{SourceHome: src, AccountRoot: t.TempDir(), Bin: agyScript(t), AllowMissingToken: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := keyring.Write(context.Background(), []byte(emptyCredential)); err != nil {
		t.Fatal(err)
	}
	if !LoggedIn(res.Home) {
		t.Fatal("import still depends on the shared system credential")
	}
	if LoggedIn(src) {
		t.Fatal("import wrote into the source home")
	}
}
