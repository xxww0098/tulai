//go:build !windows

package agy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
)

type loginCredentials struct {
	memoryCredentials
	marker string
	token  []byte
}

func (m *loginCredentials) Read(ctx context.Context) ([]byte, error) {
	if _, err := os.Stat(m.marker); err == nil {
		if err := m.memoryCredentials.Write(ctx, m.token); err != nil {
			return nil, err
		}
		_ = os.Remove(m.marker)
	}
	return m.memoryCredentials.Read(ctx)
}

func TestLoginSavesKeyringOnlyOAuth(t *testing.T) {
	home := t.TempDir()
	script := filepath.Join(home, "agy-login.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
printf '%s\n' 'Authentication required. Please visit the URL to log in:' 'https://accounts.google.com/o/oauth2/auth?client_id=agy'
IFS= read -r code
unset code
: > "$HOME/keyring-ready"
printf '%s\n' 'OAuth: authenticated successfully as keyring@example.com'
exec sleep 30
`), 0o700); err != nil {
		t.Fatal(err)
	}
	previous := Keyring
	token := []byte(strings.ReplaceAll(testCredential, "test-refresh", "new-login-refresh"))
	Keyring = &loginCredentials{memoryCredentials: memoryCredentials{data: []byte(testCredential)}, marker: filepath.Join(home, "keyring-ready"), token: token}
	t.Cleanup(func() { Keyring = previous })
	sess, err := startLogin(context.Background(), script, home, "", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if LoggedIn(home) {
		t.Fatal("new login reused the previous keyring account")
	}
	if err := sess.Submit("test-code"); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(TokenPath(home))
	if err != nil || !bytes.Equal(data, token) || !LoggedIn(home) {
		t.Fatal("keyring-only OAuth was not saved to the account", err)
	}
}

func TestLoginCanCancelWhileWaitingForCredentialSwitch(t *testing.T) {
	home := t.TempDir()
	writeToken(t, home)
	session, err := beginCLI(context.Background(), home, false)
	if err != nil {
		t.Fatal(err)
	}
	defer session.unlock()
	next := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	script := agyScript(t)
	go func() { _, err := StartLogin(ctx, script, next, ""); result <- err }()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled login remained queued")
	}
	if _, err := os.Stat(TokenPath(next)); !os.IsNotExist(err) {
		t.Fatal("canceled login changed credentials")
	}
}

func TestLoginSubmitDoesNotKeepCode(t *testing.T) {
	script := agyScript(t)
	home := t.TempDir()
	writeFixture(t, home, "login")
	sess, err := startLogin(context.Background(), script, home, DefaultModel, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	deadline := time.Now().Add(3 * time.Second)
	for sess.URL() == "" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(sess.URL(), "https://accounts.google.com/") {
		t.Fatalf("url %q", sess.URL())
	}
	const code = "s3cret-code"
	if err := sess.Submit(code); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(); err != nil {
		t.Fatal(err)
	}
	if !LoggedIn(home) {
		t.Fatal("登录后应有凭证文件")
	}
	logData, err := os.ReadFile(LogPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logData), code) {
		t.Fatal("日志里出现了授权码")
	}
	if Email(home) != "someone@gmail.com" {
		t.Fatalf("email %q", Email(home))
	}
	for _, entry := range logging.Log.Entries() {
		if strings.Contains(entry.Text, code) {
			t.Fatal("运行日志里出现了授权码")
		}
	}
}

func TestLoginTimeout(t *testing.T) {
	script := agyScript(t)
	home := t.TempDir()
	writeFixture(t, home, "hang")
	sess, err := startLogin(context.Background(), script, home, "", 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	err = sess.Wait()
	if err == nil || !strings.Contains(err.Error(), "重试") {
		t.Fatalf("%v", err)
	}
	if LoggedIn(home) {
		t.Fatal("超时不应写成已登录")
	}
	if _, statErr := os.Stat(filepath.Join(home, "fixture.txt")); statErr != nil {
		t.Fatal(statErr)
	}
}
