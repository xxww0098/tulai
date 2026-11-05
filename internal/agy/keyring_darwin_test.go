package agy

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacKeyringUsesAgyServiceAndPrivateInput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGY_KEYRING_FIXTURE", dir)
	script := filepath.Join(dir, "security")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
set -eu
if [ "$1" = "-i" ]; then
  IFS= read -r line
  case "$line" in
    'add-generic-password -U -s gemini -a antigravity -w go-keyring-base64:'*) ;;
    *) exit 1 ;;
  esac
  if [ ! -f "$AGY_KEYRING_FIXTURE/deny" ]; then
    printf '%s' "${line##* }" > "$AGY_KEYRING_FIXTURE/value"
  fi
  exit 0
fi
if [ "$*" != 'find-generic-password -s gemini -a antigravity -w' ]; then exit 1; fi
if [ ! -f "$AGY_KEYRING_FIXTURE/value" ]; then exit 44; fi
cat "$AGY_KEYRING_FIXTURE/value"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	previous := securityCommand
	securityCommand = func(ctx context.Context, args ...string) *exec.Cmd {
		for _, arg := range args {
			if strings.Contains(arg, "test-access") || strings.Contains(arg, "go-keyring-base64:") {
				t.Error("OAuth leaked in process arguments")
			}
		}
		return exec.CommandContext(ctx, script, args...)
	}
	t.Cleanup(func() { securityCommand = previous })
	k := systemKeyring{}
	ctx := context.Background()
	if data, err := k.Read(ctx); err != nil || len(data) != 0 {
		t.Fatal("missing credential was not handled", err)
	}
	if err := k.Write(ctx, []byte(testCredential)); err != nil {
		t.Fatal(err)
	}
	if data, err := k.Read(ctx); err != nil || !bytes.Equal(data, []byte(testCredential)) {
		t.Fatal("go-keyring encoding did not round trip", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deny"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	changed := []byte(strings.ReplaceAll(testCredential, "test-refresh", "different-refresh"))
	if err := k.Write(ctx, changed); err == nil {
		t.Fatal("security's zero exit status hid a failed update")
	}
}
