package browser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// KindChrome 是本机 Google Chrome。
	KindChrome = "Chrome"
	// KindEdge 是本机 Microsoft Edge。
	KindEdge = "Edge"
	// KindChromium 是本机 Chromium。配置库不单独存这个值，解析时当作 Chrome 的下一位。
	KindChromium = "Chromium"
	// KindBuiltin 是下载到应用数据目录的 Chrome for Testing。
	KindBuiltin = "Builtin"
)

// Runtime 是一次解析得到的浏览器可执行文件。
type Runtime struct {
	Kind    string
	Path    string
	Version string
}

// DefaultBackend 是新配置的网页方式。Windows 默认 Edge，其它系统默认 Chrome。
func DefaultBackend(goos string) string {
	if goos == "windows" {
		return KindEdge
	}
	return KindChrome
}

// CanonicalBackend 把界面文案和配置值收成 Chrome、Edge 或 Builtin。
// 空串和旧的 ego lite 收成 Chrome，调用方要先用 NormalizeStoredBackend 处理“尚未选择”。
func CanonicalBackend(v string) string {
	switch strings.TrimSpace(v) {
	case KindEdge, "edge":
		return KindEdge
	case KindBuiltin, "内置浏览器", "builtin":
		return KindBuiltin
	case KindChromium, "chromium":
		return KindChrome
	default:
		return KindChrome
	}
}

// NormalizeStoredBackend 读取配置库。空值和无法识别的旧值按系统给默认，已保存的 Chrome、Edge、Builtin 保持原样。
func NormalizeStoredBackend(goos, raw string) string {
	switch strings.TrimSpace(raw) {
	case KindEdge, "edge":
		return KindEdge
	case KindBuiltin, "内置浏览器", "builtin":
		return KindBuiltin
	case KindChrome, "chrome":
		return KindChrome
	default:
		return DefaultBackend(goos)
	}
}

// ChoiceLabel 是网页方式在界面上的中文或产品名。
func ChoiceLabel(backend string) string {
	switch strings.TrimSpace(backend) {
	case KindEdge, "edge":
		return "Edge"
	case KindBuiltin, "内置浏览器", "builtin":
		return "内置浏览器"
	case KindChromium, "chromium":
		return "Chromium"
	default:
		return "Chrome"
	}
}

// BuiltinSupported 报告这个系统有没有可下载的 Chrome for Testing。Windows arm64 没有。
func BuiltinSupported(goos, goarch string) bool {
	_, ok := cftPlatformID(goos, goarch)
	return ok
}

// ConfigDir 是应用数据根目录：<UserConfigDir>/xbs。
func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "xbs")
}

// Resolve 按系统顺序找可执行文件。backend 是用户的选择（Chrome、Edge、Builtin），空串表示按系统默认顺序。
// dataDir 是 <UserConfigDir>/xbs，用来找已经下载的内置浏览器。
func Resolve(goos, backend, dataDir string) (Runtime, error) {
	return resolve(hostLookup(goos, runtime.GOARCH), backend, dataDir)
}

// Detect 列出本机已经能用的浏览器，不含还没下载的内置浏览器。
func Detect(dataDir string) []Runtime {
	return detect(hostLookup(runtime.GOOS, runtime.GOARCH), dataDir)
}

// FormatDetected 给浏览器页一行摘要和一条带路径的说明。内置浏览器另有状态行。
func FormatDetected(list []Runtime) (summary, detail string) {
	var labels, lines []string
	for _, rt := range list {
		if rt.Kind == KindBuiltin {
			continue
		}
		labels = append(labels, ChoiceLabel(rt.Kind))
		if rt.Path != "" {
			lines = append(lines, ChoiceLabel(rt.Kind)+"： "+rt.Path)
		}
	}
	if len(labels) == 0 {
		return "未检测到 Chrome、Edge 或 Chromium", ""
	}
	return "已检测到：" + strings.Join(labels, "、"), strings.Join(lines, " · ")
}

type lookup struct {
	goos     string
	goarch   string
	home     string
	env      func(string) string
	stat     func(string) error
	lookPath func(string) (string, error)
}

func hostLookup(goos, goarch string) lookup {
	home, _ := os.UserHomeDir()
	return lookup{
		goos:   goos,
		goarch: goarch,
		home:   home,
		env:    os.Getenv,
		stat: func(path string) error {
			_, err := os.Stat(path)
			return err
		},
		lookPath: func(name string) (string, error) {
			if goos != runtime.GOOS {
				return "", exec.ErrNotFound
			}
			return exec.LookPath(name)
		},
	}
}

func (l lookup) find(look, files []string) (string, error) {
	if l.lookPath != nil {
		for _, name := range look {
			if name == "" {
				continue
			}
			if path, err := l.lookPath(name); err == nil && path != "" {
				return path, nil
			}
		}
	}
	if l.stat != nil {
		for _, path := range files {
			if path == "" {
				continue
			}
			if err := l.stat(path); err == nil {
				return path, nil
			}
		}
	}
	return "", os.ErrNotExist
}

func resolve(l lookup, backend, dataDir string) (Runtime, error) {
	var last error
	for _, kind := range fallbackOrder(l.goos, l.goarch, backend) {
		rt, err := l.lookup(kind, dataDir)
		if err == nil {
			return rt, nil
		}
		last = err
	}
	if last == nil {
		last = os.ErrNotExist
	}
	return Runtime{}, noBrowserError(l.goos, l.goarch)
}

func detect(l lookup, dataDir string) []Runtime {
	var out []Runtime
	for _, kind := range []string{KindChrome, KindChromium, KindEdge, KindBuiltin} {
		if kind == KindBuiltin && !BuiltinSupported(l.goos, l.goarch) {
			continue
		}
		rt, err := l.lookup(kind, dataDir)
		if err == nil {
			out = append(out, rt)
		}
	}
	return out
}

func (l lookup) lookup(kind, dataDir string) (Runtime, error) {
	switch kind {
	case KindEdge:
		path, err := l.find(edgeLocations(l.goos, l.home, l.env))
		if err != nil {
			return Runtime{}, err
		}
		return Runtime{Kind: KindEdge, Path: path}, nil
	case KindChromium:
		path, err := l.find(chromiumLocations(l.goos, l.home, l.env))
		if err != nil {
			return Runtime{}, err
		}
		return Runtime{Kind: KindChromium, Path: path}, nil
	case KindBuiltin:
		return l.builtin(dataDir)
	default:
		path, err := l.find(chromeLocations(l.goos, l.home, l.env))
		if err != nil {
			return Runtime{}, err
		}
		return Runtime{Kind: KindChrome, Path: path}, nil
	}
}

func fallbackOrder(goos, goarch, choice string) []string {
	chrome := []string{KindChrome, KindChromium}
	edge := []string{KindEdge}
	var slots [][]string
	if goos == "windows" {
		slots = [][]string{edge, chrome}
	} else {
		slots = [][]string{chrome, edge}
	}
	if BuiltinSupported(goos, goarch) {
		slots = append(slots, []string{KindBuiltin})
	}
	switch normalizeChoice(choice) {
	case KindEdge:
		slots = preferSlot(slots, KindEdge)
	case KindChrome:
		slots = preferSlot(slots, KindChrome)
	case KindBuiltin:
		slots = preferSlot(slots, KindBuiltin)
	}
	var out []string
	seen := map[string]bool{}
	for _, slot := range slots {
		for _, kind := range slot {
			if seen[kind] {
				continue
			}
			seen[kind] = true
			out = append(out, kind)
		}
	}
	return out
}

func preferSlot(slots [][]string, kind string) [][]string {
	var picked, rest [][]string
	for _, slot := range slots {
		hit := false
		for _, item := range slot {
			if item == kind {
				hit = true
				break
			}
		}
		if hit {
			picked = append(picked, slot)
		} else {
			rest = append(rest, slot)
		}
	}
	return append(picked, rest...)
}

func normalizeChoice(v string) string {
	switch strings.TrimSpace(v) {
	case KindEdge, "edge":
		return KindEdge
	case KindBuiltin, "内置浏览器", "builtin":
		return KindBuiltin
	case KindChrome, "chrome":
		return KindChrome
	default:
		return ""
	}
}

func noBrowserError(goos, goarch string) error {
	if BuiltinSupported(goos, goarch) {
		return errors.New("没有可用的浏览器，请安装 Chrome 或 Edge，或在浏览器页一键下载内置浏览器")
	}
	return errors.New("没有可用的浏览器，请安装 Chrome 或 Edge")
}

func chromeLocations(goos, home string, env func(string) string) (look, files []string) {
	if env == nil {
		env = func(string) string { return "" }
	}
	switch goos {
	case "darwin":
		files = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
		if home != "" {
			files = append(files, osJoin(goos, home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"))
		}
	case "windows":
		for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			base := env(key)
			if base == "" {
				continue
			}
			files = append(files,
				osJoin(goos, base, `Google\Chrome\Application\chrome.exe`),
				osJoin(goos, base, `Google\Chrome Beta\Application\chrome.exe`),
			)
		}
	default:
		look = []string{"google-chrome-stable", "google-chrome", "google-chrome-beta"}
		files = []string{
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/opt/google/chrome/chrome",
			"/opt/google/chrome/google-chrome",
		}
	}
	return look, files
}

func chromiumLocations(goos, home string, env func(string) string) (look, files []string) {
	if env == nil {
		env = func(string) string { return "" }
	}
	switch goos {
	case "darwin":
		files = []string{"/Applications/Chromium.app/Contents/MacOS/Chromium"}
		if home != "" {
			files = append(files, osJoin(goos, home, "Applications/Chromium.app/Contents/MacOS/Chromium"))
		}
	case "windows":
		for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			base := env(key)
			if base == "" {
				continue
			}
			files = append(files, osJoin(goos, base, `Chromium\Application\chrome.exe`))
		}
	default:
		look = []string{"chromium", "chromium-browser"}
		files = []string{
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/lib/chromium/chromium",
			"/snap/bin/chromium",
		}
	}
	return look, files
}
