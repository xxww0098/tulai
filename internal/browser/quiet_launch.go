package browser

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 出图冷启动后，窗口管理器可能晚一拍才把 Chrome 映到 tulai 上面。
// 端口就绪后再补这几下，把 tulai 自己的窗口拉回来。
var appFocusWaits = []time.Duration{
	300 * time.Millisecond,
	800 * time.Millisecond,
	1600 * time.Millisecond,
}

// KeepAppInFront 立刻把 tulai 放到前面，并在冷启动窗口真正出现后再补几次。
// 没有接上界面时什么都不做。登录路径不要调用。
func (m *Manager) KeepAppInFront() {
	if m == nil || m.focusFn() == nil {
		return
	}
	m.refocusApp()
	waits := append([]time.Duration(nil), appFocusWaits...)
	go func() {
		for _, wait := range waits {
			time.Sleep(wait)
			m.refocusApp()
		}
	}()
}

func (m *Manager) refocusApp() {
	fn := m.focusFn()
	if fn != nil {
		fn()
	}
}

func (m *Manager) focusFn() func() {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.focusApp
}

// launchCmd 在出图时尽量不把浏览器变成前台应用。
// macOS 且二进制在 .app 里时用 open -n -g：-n 新开一个实例，-g 不把应用带到前面。
// 不用 -j。-j 会把整个应用藏起来，之后登录没法把同一个进程的窗口显示出来。
// 其它系统直接启动。窗口留在屏幕内，再由 KeepAppInFront 把 tulai 拉回来。
func launchCmd(goos, bin string, args []string, background bool) (*exec.Cmd, bool) {
	if background && goos == "darwin" {
		if app := appBundle(bin); app != "" {
			argv := make([]string, 0, len(args)+5)
			argv = append(argv, "-n", "-g", "-a", app, "--args")
			argv = append(argv, args...)
			return exec.Command("open", argv...), true
		}
	}
	return exec.Command(bin, args...), false
}

// appBundle 从 .../Foo.app/Contents/MacOS/Foo 取出 Foo.app。不是这种布局就返回空。
func appBundle(bin string) string {
	slash := filepath.ToSlash(bin)
	const mark = ".app/Contents/MacOS/"
	i := strings.Index(slash, mark)
	if i <= 0 {
		return ""
	}
	return filepath.FromSlash(slash[:i+len(".app")])
}

// pidFromPS 在 ps 输出里找占用这个用户目录的浏览器主进程。
// 跳过 open 自己，也跳过带 --type= 的 gpu / renderer。
func pidFromPS(out, userData string) int {
	userData = strings.TrimSpace(userData)
	if userData == "" {
		return 0
	}
	needle := "--user-data-dir=" + userData
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "--type=") || !hasExactArg(line, needle) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid <= 0 {
			continue
		}
		if filepath.Base(fields[1]) == "open" {
			continue
		}
		return pid
	}
	return 0
}

func hasExactArg(line, arg string) bool {
	rest := line
	for {
		i := strings.Index(rest, arg)
		if i < 0 {
			return false
		}
		beforeOK := i == 0 || rest[i-1] == ' '
		end := i + len(arg)
		afterOK := end == len(rest) || rest[end] == ' '
		if beforeOK && afterOK {
			return true
		}
		rest = rest[i+1:]
	}
}
