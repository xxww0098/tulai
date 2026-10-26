package update

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Options describes one install of an already verified download.
type Options struct {
	Mode       Mode
	Executable string
	AssetPath  string
	Version    string
	Home       string
	DataHome   string
	PID        int
}

// Outcome is what the caller should tell the user and whether to restart.
type Outcome struct {
	Message       string
	InstalledPath string
	Relaunch      bool
	Quit          bool
	Helper        string
	HelperArgs    []string
	ManualCommand string
	// OpenPath is a package the caller should hand to the operating system.
	OpenPath string
}

// Install applies a verified asset. Windows replacement is prepared and
// started by the caller with StartDetached, after the process is ready to exit.
func Install(opts Options) (Outcome, error) {
	if opts.AssetPath == "" {
		return Outcome{}, errors.New("没有安装包")
	}
	if opts.PID == 0 {
		opts.PID = os.Getpid()
	}
	switch opts.Mode {
	case ModeLinuxUser:
		return installLinuxUser(opts)
	case ModeLinuxBinary:
		return installLinuxBinary(opts)
	case ModeLinuxDeb:
		return installLinuxDeb(opts)
	case ModeWindowsExe:
		return installWindowsExe(opts)
	case ModeWindowsSetup:
		return installWindowsSetup(opts)
	case ModeMacBundle, ModeMacCopy:
		return installMac(opts)
	default:
		return Outcome{}, fmt.Errorf("无法安装（%s）", opts.Mode)
	}
}

func installLinuxUser(opts Options) (Outcome, error) {
	home, dataHome, err := homes(opts)
	if err != nil {
		return Outcome{}, err
	}
	appDir := filepath.Join(home, ".local", appName+".app")
	staging, err := os.MkdirTemp(filepath.Join(home, ".local"), "."+appName+".app.")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(filepath.Join(home, ".local"), 0o755); err != nil {
				return Outcome{}, err
			}
			staging, err = os.MkdirTemp(filepath.Join(home, ".local"), "."+appName+".app.")
		}
		if err != nil {
			return Outcome{}, err
		}
	}
	defer os.RemoveAll(staging)
	if err := extractTarGz(opts.AssetPath, staging); err != nil {
		return Outcome{}, err
	}
	bin := filepath.Join(staging, appName)
	info, err := os.Stat(bin)
	if err != nil || info.IsDir() {
		return Outcome{}, fmt.Errorf("压缩包里没有 %s", appName)
	}
	if err := os.Chmod(bin, info.Mode().Perm()|0o111); err != nil {
		return Outcome{}, err
	}
	if err := replaceDir(staging, appDir); err != nil {
		return Outcome{}, err
	}
	installed := filepath.Join(appDir, appName)
	link := filepath.Join(home, ".local", "bin", appName)
	linkNote := ensureSymlink(link, filepath.Join(appDir, appName))
	desktopNote := installDesktop(appDir, dataHome)
	if err := confirmAndKeep(installed); err != nil {
		return Outcome{}, err
	}
	relaunch := runsFrom(opts.Executable, appDir, link)
	msg := fmt.Sprintf("已安装 %s 到 %s。", opts.Version, appDir)
	if relaunch {
		msg += "重新打开后即可使用。"
	} else {
		msg += "请退出当前进程，从应用程序菜单或 " + link + " 启动。"
	}
	if linkNote != "" {
		msg += linkNote
	}
	if desktopNote != "" {
		msg += desktopNote
	}
	msg += "\n" + UnsignedWarning
	out := Outcome{Message: msg, InstalledPath: installed, Relaunch: relaunch}
	if !relaunch {
		return out, nil
	}
	script, err := writeRestartScript()
	if err != nil {
		return Outcome{}, err
	}
	out.Relaunch = false
	out.Quit = true
	out.Helper = "/bin/sh"
	out.HelperArgs = restartArgs(script, opts.PID, installed)
	out.Message += "即将重新打开。"
	return out, nil
}

func installLinuxBinary(opts Options) (Outcome, error) {
	dest := opts.Executable
	if dest == "" {
		return Outcome{}, errors.New("找不到当前程序")
	}
	src := opts.AssetPath
	lower := strings.ToLower(src)
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		dir, err := os.MkdirTemp("", "xbs-bin-")
		if err != nil {
			return Outcome{}, err
		}
		defer os.RemoveAll(dir)
		if err := extractTarGz(src, dir); err != nil {
			return Outcome{}, err
		}
		src = filepath.Join(dir, appName)
	}
	if err := replaceFile(dest, src); err != nil {
		return Outcome{}, err
	}
	if err := confirmAndKeep(dest); err != nil {
		return Outcome{}, err
	}
	msg := fmt.Sprintf("已安装 %s。\n%s", opts.Version, UnsignedWarning)
	out := Outcome{Message: msg, InstalledPath: dest}
	if filepath.Clean(opts.Executable) == filepath.Clean(dest) {
		script, err := writeRestartScript()
		if err != nil {
			return Outcome{}, err
		}
		out.Quit = true
		out.Helper = "/bin/sh"
		out.HelperArgs = restartArgs(script, opts.PID, dest)
		out.Message = fmt.Sprintf("已安装 %s。即将重新打开。\n%s", opts.Version, UnsignedWarning)
	}
	return out, nil
}

func installLinuxDeb(opts Options) (Outcome, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "xbs", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Outcome{}, err
	}
	dest := filepath.Join(dir, filepath.Base(opts.AssetPath))
	if filepath.Clean(opts.AssetPath) != filepath.Clean(dest) {
		if err := copyFile(opts.AssetPath, dest, 0o644); err != nil {
			return Outcome{}, err
		}
	}
	command := "sudo dpkg -i " + shellQuote(dest)
	msg := "这是系统安装包，需要管理员权限，程序不会自己申请。已尝试打开下载的安装包。若没有弹出，请在终端执行：\n" + command + "\n\n" + UnsignedWarning
	return Outcome{Message: msg, InstalledPath: dest, ManualCommand: command, OpenPath: dest}, nil
}

func installWindowsExe(opts Options) (Outcome, error) {
	if opts.Executable == "" {
		return Outcome{}, errors.New("找不到当前程序")
	}
	script, err := writeWindowsScript()
	if err != nil {
		return Outcome{}, err
	}
	msg := fmt.Sprintf("已下载 %s。退出后会替换 %s。新的程序没有代码签名，SmartScreen 可能报警。\n%s", opts.Version, opts.Executable, UnsignedWarning)
	return Outcome{
		Message:       msg,
		InstalledPath: opts.Executable,
		Quit:          true,
		Helper:        "powershell.exe",
		HelperArgs:    WindowsHelperArgs(script, opts.PID, opts.AssetPath, opts.Executable, ""),
	}, nil
}

func installWindowsSetup(opts Options) (Outcome, error) {
	script, err := writeWindowsScript()
	if err != nil {
		return Outcome{}, err
	}
	msg := fmt.Sprintf("已下载 %s 的安装器。退出后会打开它。安装器没有代码签名，若 SmartScreen 拦截，请选择仍要运行。\n%s", opts.Version, UnsignedWarning)
	return Outcome{
		Message:       msg,
		InstalledPath: opts.AssetPath,
		Quit:          true,
		Helper:        "powershell.exe",
		HelperArgs:    WindowsHelperArgs(script, opts.PID, "", "", opts.AssetPath),
	}, nil
}

func installMac(opts Options) (Outcome, error) {
	home := opts.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return Outcome{}, err
		}
	}
	dest, inPlace := MacDestination(opts.Executable, home)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Outcome{}, err
	}
	if err := installDMG(opts.AssetPath, dest); err != nil {
		return Outcome{}, err
	}
	installed := filepath.Join(dest, "Contents", "MacOS", appName)
	if err := confirmAndKeep(installed); err != nil {
		return Outcome{}, err
	}
	msg := fmt.Sprintf("已安装 %s 到 %s。磁盘映像只做了 ad-hoc 签名，没有公证。若 Gatekeeper 拦截，请在访达里右键打开，或执行 xattr -dr com.apple.quarantine %s。\n%s", opts.Version, dest, shellQuote(dest), UnsignedWarning)
	out := Outcome{Message: msg, InstalledPath: installed, Relaunch: inPlace && runsFrom(opts.Executable, dest, "")}
	if !out.Relaunch {
		return out, nil
	}
	script, err := writeRestartScript()
	if err != nil {
		return Outcome{}, err
	}
	out.Relaunch = false
	out.Quit = true
	out.Helper = "/bin/sh"
	out.HelperArgs = restartArgs(script, opts.PID, installed)
	out.Message += "即将重新打开。"
	return out, nil
}

// MacBundle returns the .app that contains executable.
func MacBundle(executable string) (string, bool) {
	macOS := filepath.Dir(executable)
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.EqualFold(filepath.Ext(bundle), ".app") {
		return "", false
	}
	return bundle, true
}

// MacDestination is the bundle to replace, or ~/Applications/xbs.app.
func MacDestination(executable, home string) (dest string, inPlace bool) {
	if bundle, ok := MacBundle(executable); ok {
		return bundle, true
	}
	return filepath.Join(home, "Applications", appName+".app"), false
}

// FindBundle looks for name under root.
func FindBundle(root, name string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && strings.EqualFold(d.Name(), name) {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("磁盘映像里没有 %s", name)
	}
	return found, nil
}

func installDMG(dmg, dest string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("磁盘映像只能在 macOS 上安装")
	}
	mount, err := os.MkdirTemp("", "xbs-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mount)
	attach := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mount, dmg)
	if out, err := attach.CombinedOutput(); err != nil {
		return fmt.Errorf("挂载磁盘映像失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	defer exec.Command("hdiutil", "detach", mount).Run()
	app, err := FindBundle(mount, appName+".app")
	if err != nil {
		return err
	}
	staging := dest + ".new"
	os.RemoveAll(staging)
	copy := exec.Command("ditto", app, staging)
	if out, err := copy.CombinedOutput(); err != nil {
		os.RemoveAll(staging)
		return fmt.Errorf("复制应用程序失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if err := replaceDir(staging, dest); err != nil {
		os.RemoveAll(staging)
		return err
	}
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", dest).Run()
	return nil
}

func extractTarGz(src, dest string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("不是 gzip 压缩包: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var written int64
	nfiles := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		nfiles++
		if nfiles > 100 {
			return errors.New("压缩包文件过多")
		}
		name, err := safeRel(hdr.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		rel, err := filepath.Rel(dest, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("压缩包路径不安全: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode) & 0o755
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			n, err := io.Copy(out, io.LimitReader(tr, maxDownload+1))
			closeErr := out.Close()
			written += n
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if n > maxDownload || written > maxDownload {
				return errors.New("压缩包超过大小限制")
			}
		default:
			return fmt.Errorf("压缩包含有不支持的条目 %s", hdr.Name)
		}
	}
	return nil
}

func safeRel(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("压缩包路径不安全: %s", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("压缩包路径不安全: %s", name)
	}
	return clean, nil
}

func replaceDir(staging, dest string) error {
	backup := dest + ".previous"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			return fmt.Errorf("无法移开现有安装: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, dest); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	// Keep dest.previous until the new process starts. CommitPrevious removes it.
	return nil
}

func ensureSymlink(link, target string) string {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return " 未能创建 " + link + "。"
	}
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return " 保留了已有的 " + link + "，它不是指向本程序的符号链接。"
		}
		current, err := os.Readlink(link)
		ours := err == nil && (filepath.Clean(current) == filepath.Clean(target) || strings.Contains(toSlash(current), "/.local/"+appName+".app/"))
		if !ours {
			return " 保留了已有的 " + link + "。"
		}
		if err := os.Remove(link); err != nil {
			return " 未能更新 " + link + "。"
		}
	}
	if err := os.Symlink(target, link); err != nil {
		return " 未能创建命令 " + link + "。"
	}
	return ""
}

func installDesktop(appDir, dataHome string) string {
	raw, err := os.ReadFile(filepath.Join(appDir, appName+".desktop"))
	if err != nil {
		return ""
	}
	entryPath := filepath.Join(dataHome, "applications", appName+".desktop")
	if existing, err := os.ReadFile(entryPath); err == nil && !desktopIsOurs(string(existing), appDir) {
		return " 保留了已有的桌面项 " + entryPath + "。"
	}
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
		return " 未能登记桌面项。"
	}
	entry := RewriteDesktop(string(raw), appDir, appName)
	if err := os.WriteFile(entryPath, []byte(entry), 0o644); err != nil {
		return " 未能登记桌面项。"
	}
	if tool, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(tool, filepath.Dir(entryPath)).Run()
	}
	return ""
}

func desktopIsOurs(content, appDir string) bool {
	exe := registeredExec(content)
	if exe == "" {
		return false
	}
	return filepath.Clean(filepath.Dir(exe)) == filepath.Clean(appDir) || strings.Contains(toSlash(exe), "/.local/"+appName+".app/")
}

func registeredExec(entry string) string {
	for _, line := range strings.Split(entry, "\n") {
		rest, ok := strings.CutPrefix(line, `Exec="`)
		if !ok {
			continue
		}
		exe, _, ok := strings.Cut(rest, `"`)
		if ok && filepath.IsAbs(exe) {
			return exe
		}
	}
	return ""
}

// RewriteDesktop points Exec and Icon at the installed app, as install.sh does.
// The launcher label becomes tulai. The command and desktop filename stay the binary name.
func RewriteDesktop(packaged, dir, name string) string {
	icon := dir + "/" + name + ".png"
	_, iconErr := os.Stat(icon)
	var b strings.Builder
	for _, line := range strings.Split(packaged, "\n") {
		if rest, ok := strings.CutPrefix(line, "Exec="+name); ok && (rest == "" || strings.HasPrefix(rest, " ")) {
			line = `Exec="` + dir + "/" + name + `"` + rest
		} else if line == "Icon="+name && iconErr == nil {
			line = "Icon=" + icon
		} else if line == "Name="+name {
			line = "Name=tulai"
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func runsFrom(executable, appDir, link string) bool {
	if executable == "" {
		return false
	}
	candidates := []string{executable}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		candidates = append(candidates, resolved)
	}
	for _, candidate := range candidates {
		if link != "" && samePath(candidate, link) {
			return true
		}
		rel, err := filepath.Rel(appDir, candidate)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ai, bi)
}

func homes(opts Options) (home, data string, err error) {
	home = opts.Home
	if home == "" {
		home, err = os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
	}
	data = opts.DataHome
	if data != "" {
		return home, data, nil
	}
	if opts.Home == "" {
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return home, v, nil
		}
	}
	return home, filepath.Join(home, ".local", "share"), nil
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func scriptArgs(script string) []string {
	return []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script}
}

// WindowsScript is the PowerShell that runs after this process exits.
// Paths are parameters so the script itself stays ASCII.
func WindowsScript() string {
	return strings.Join([]string{
		"param([int]$ProcessId, [string]$Source, [string]$Destination, [string]$Installer)",
		"while (Get-Process -Id $ProcessId -ErrorAction SilentlyContinue) { Start-Sleep -Milliseconds 400 }",
		"if ($Installer -ne '') {",
		"  $p = Start-Process -FilePath $Installer -ArgumentList '/S' -PassThru -Wait",
		"  if ($null -ne $p -and $p.ExitCode -ne 0) { Start-Process -FilePath $Installer }",
		"  exit 0",
		"}",
		"$backup = \"$Destination.previous\"",
		"if (Test-Path -LiteralPath $Destination) {",
		"  Copy-Item -LiteralPath $Destination -Destination $backup -Force",
		"}",
		"Move-Item -LiteralPath $Source -Destination $Destination -Force",
		"if (-not $?) {",
		"  if (Test-Path -LiteralPath $backup) { Copy-Item -LiteralPath $backup -Destination $Destination -Force }",
		"  exit 1",
		"}",
		"$proc = Start-Process -FilePath $Destination -PassThru",
		"Start-Sleep -Seconds 3",
		"if ($proc.HasExited) {",
		"  if (Test-Path -LiteralPath $backup) {",
		"    Copy-Item -LiteralPath $backup -Destination $Destination -Force",
		"    Start-Process -FilePath $Destination",
		"  }",
		"  exit 1",
		"}",
		"if (Test-Path -LiteralPath $backup) { Remove-Item -LiteralPath $backup -Force }",
		"",
	}, "\r\n")
}

func writeWindowsScript() (string, error) {
	dir, err := os.MkdirTemp("", "xbs-replace-")
	if err != nil {
		return "", err
	}
	script := filepath.Join(dir, "replace.ps1")
	if err := os.WriteFile(script, []byte(WindowsScript()), 0o644); err != nil {
		return "", err
	}
	return script, nil
}

// WindowsHelperArgs is the powershell invocation for a written script.
func WindowsHelperArgs(script string, pid int, source, destination, installer string) []string {
	args := scriptArgs(script)
	args = append(args, "-ProcessId", strconv.Itoa(pid))
	if source != "" {
		args = append(args, "-Source", source)
	}
	if destination != "" {
		args = append(args, "-Destination", destination)
	}
	if installer != "" {
		args = append(args, "-Installer", installer)
	}
	return args
}
