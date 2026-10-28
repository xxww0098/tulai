//go:build !windows

package browser

import "testing"

func TestCmdlineUsesProfileDoesNotMatchAPrefix(t *testing.T) {
	raw := []byte("--user-data-dir=/tmp/Edge/账号2\x00--type=renderer")
	if !cmdlineUsesProfile(raw, "/tmp/Edge/账号2") {
		t.Fatal("exact profile argument should match")
	}
	if cmdlineUsesProfile(raw, "/tmp/Edge/账号") {
		t.Fatal("a shorter directory must not match a longer profile")
	}
}
