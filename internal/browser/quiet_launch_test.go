package browser

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAppBundle(t *testing.T) {
	bin := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if got := appBundle(bin); got != "/Applications/Google Chrome.app" {
		t.Fatalf("bundle = %q", got)
	}
	edge := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
	if got := appBundle(edge); got != "/Applications/Microsoft Edge.app" {
		t.Fatalf("edge bundle = %q", got)
	}
	if appBundle("/usr/bin/google-chrome") != "" || appBundle(`C:\Program Files\Google\Chrome\Application\chrome.exe`) != "" {
		t.Fatal("non-app binary looked like a bundle")
	}
}

func TestBackgroundLaunchUsesOpenOnDarwin(t *testing.T) {
	bin := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	args := []string{"--user-data-dir=/tmp/acct", "--window-position=-2400,-2400", "https://www.doubao.com/chat/"}
	cmd, quiet := launchCmd("darwin", bin, args, true)
	if !quiet || len(cmd.Args) == 0 || cmd.Args[0] != "open" {
		t.Fatalf("darwin background = quiet:%v args:%v", quiet, cmd.Args)
	}
	joined := strings.Join(cmd.Args, "\n")
	for _, want := range []string{"-n", "-g", "-a", "/Applications/Google Chrome.app", "--args", "--user-data-dir=/tmp/acct", "--window-position=-2400,-2400"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, cmd.Args)
		}
	}
	// -j 会藏起整个应用，登录时就没法把这个进程的窗口拿回来。
	for _, arg := range cmd.Args {
		if arg == "-j" {
			t.Fatal("open -j hides the app and blocks a later login window")
		}
	}
	login, loginQuiet := launchCmd("darwin", bin, args, false)
	if loginQuiet || login.Args[0] == "open" {
		t.Fatalf("login should launch Chrome directly: %v", login.Args)
	}
	linux, linuxQuiet := launchCmd("linux", "/usr/bin/google-chrome", args, true)
	if linuxQuiet || linux.Args[0] != "/usr/bin/google-chrome" {
		t.Fatalf("linux stays a direct launch: %v quiet=%v", linux.Args, linuxQuiet)
	}
	win, winQuiet := launchCmd("windows", `C:\Program Files\Google\Chrome\Application\chrome.exe`, args, true)
	if winQuiet || !strings.Contains(win.Args[0], "chrome.exe") {
		t.Fatalf("windows stays a direct launch: %v quiet=%v", win.Args, winQuiet)
	}
}

func TestPidFromPS(t *testing.T) {
	const profile = "/tmp/acct"
	out := `
  42 /usr/bin/open -n -g -a /Applications/Google Chrome.app --args --user-data-dir=/tmp/acct --remote-debugging-port=9333
  100 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --type=gpu-process --user-data-dir=/tmp/acct
  99 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir=/tmp/acct --remote-debugging-port=9333
  101 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --type=renderer --user-data-dir=/tmp/acct
  77 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir=/tmp/acct-extra
`
	if got := pidFromPS(out, profile); got != 99 {
		t.Fatalf("pid = %d", got)
	}
	if pidFromPS(out, "") != 0 || pidFromPS(out, "/tmp/missing") != 0 {
		t.Fatal("empty or other profile matched")
	}
}

func TestBackgroundLaunchRefocusesAndLoginDoesNot(t *testing.T) {
	old := appFocusWaits
	appFocusWaits = []time.Duration{20 * time.Millisecond}
	t.Cleanup(func() { appFocusWaits = old })

	background := NewManager()
	var n atomic.Int32
	background.SetAppFocus(func() { n.Add(1) })
	background.KeepAppInFront()
	if n.Load() < 1 {
		t.Fatal("background launch did not refocus")
	}
	deadline := time.Now().Add(time.Second)
	for n.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n.Load() < 2 {
		t.Fatalf("follow-up refocus = %d", n.Load())
	}

	login := NewManager()
	var stolen atomic.Int32
	login.SetAppFocus(func() { stolen.Add(1) })
	if login.focusFn() == nil {
		t.Fatal("login focus hook missing")
	}
	if stolen.Load() != 0 {
		t.Fatal("login launch refocused tulai")
	}
}
