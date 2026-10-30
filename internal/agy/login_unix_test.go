//go:build !windows

package agy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
)

func TestLoginSubmitDoesNotKeepCode(t *testing.T) {
	script := agyScript(t)
	home := t.TempDir()
	writeFixture(t, home, "login")
	sess, err := startLogin(script, home, DefaultModel, 5*time.Second)
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
	sess, err := startLogin(script, home, "", 300*time.Millisecond)
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
