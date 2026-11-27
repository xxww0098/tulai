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
	if err := confirmAndKeepVersion(installed, opts.Version); err != nil {
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
		dir, err := os.MkdirTemp("", "tulai-bin-")
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
	if err := confirmAndKeepVersion(dest, opts.Version); err != nil {
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
	dir = filepath.Join(dir, "tulai", "updates")
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
	msg := fmt.Sprintf("已下载 %s。退出后会替换 %s 并重新打开。新的程序没有代码签名，SmartScreen 可能报警。\n%s", opts.Version, opts.Executable, UnsignedWarning)
	return Outcome{
		Message:       msg,
		InstalledPath: opts.Executable,
		Quit:          true,
		Helper:        "powershell.exe",
		HelperArgs:    windowsHelperArgs(script, opts.PID, opts.AssetPath, opts.Executable, "", opts.Version),
	}, nil
}

func installWindowsSetup(opts Options) (Outcome, error) {
	script, err := writeWindowsScript()
	if err != nil {
		return Outcome{}, err
	}
	msg := fmt.Sprintf("已下载 %s 的安装器。退出后静默安装到 %s 并重新打开。安装器没有代码签名，若 SmartScreen 拦截，请选择仍要运行。\n%s", opts.Version, installDirOf(opts.Executable), UnsignedWarning)
	return Outcome{
		Message:       msg,
		InstalledPath: opts.AssetPath,
		Quit:          true,
		Helper:        "powershell.exe",
		HelperArgs:    windowsHelperArgs(script, opts.PID, "", opts.Executable, opts.AssetPath, opts.Version),
	}, nil
}

// installDirOf is the folder to name in messages and to install over.
func installDirOf(executable string) string {
	if executable == "" {
		return "默认位置"
	}
	return filepath.Dir(executable)
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
	staging := dest + ".new"
	if err := stageDMG(opts.AssetPath, staging); err != nil {
		return Outcome{}, err
	}
	stagedBin := filepath.Join(staging, "Contents", "MacOS", appName)
	if err := ConfirmStartVersion(stagedBin, opts.Version); err != nil {
		_ = os.RemoveAll(staging)
		return Outcome{}, fmt.Errorf("新版本无法启动，未替换现有安装: %w", err)
	}
	installed := filepath.Join(dest, "Contents", "MacOS", appName)
	relaunch := inPlace && runsFrom(opts.Executable, dest, "")
	msg := fmt.Sprintf("已安装 %s 到 %s。磁盘映像只做了 ad-hoc 签名，没有公证。若 Gatekeeper 拦截，请在访达里右键打开，或执行 xattr -dr com.apple.quarantine %s。\n%s", opts.Version, dest, shellQuote(dest), UnsignedWarning)
	if !relaunch {
		if err := replaceDir(staging, dest); err != nil {
			_ = os.RemoveAll(staging)
			return Outcome{}, err
		}
		return Outcome{Message: msg, InstalledPath: installed, Relaunch: false}, nil
	}
	// Stage the bundle swap until after the running app exits so macOS Dock and
	// LaunchServices do not follow the renamed .previous bundle.
	script, err := writeRestartScript()
	if err != nil {
		_ = os.RemoveAll(staging)
		return Outcome{}, err
	}
	return Outcome{
		Message:       msg + "即将重新打开。",
		InstalledPath: installed,
		Quit:          true,
		Helper:        "/bin/sh",
		HelperArgs:    restartArgsStaged(script, opts.PID, installed, staging, "open", 50),
	}, nil
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

// MacDestination is the bundle to replace, or ~/Applications/tulai.app.
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

func stageDMG(dmg, staging string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("磁盘映像只能在 macOS 上安装")
	}
	mount, err := os.MkdirTemp("", "tulai-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mount)
	attach := exec.Command("hdiutil", "attach", "-nobrowse", "-readonly", "-noverify", "-noautoopen", "-mountpoint", mount, dmg)
	if out, err := attach.CombinedOutput(); err != nil {
		return fmt.Errorf("挂载磁盘映像失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	defer func() {
		if err := exec.Command("hdiutil", "detach", mount).Run(); err != nil {
			_ = exec.Command("hdiutil", "detach", "-force", mount).Run()
		}
	}()
	app, err := FindBundle(mount, appName+".app")
	if err != nil {
		return err
	}
	_ = os.RemoveAll(staging)
	copyCmd := exec.Command("ditto", app, staging)
	if out, err := copyCmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("复制应用程序失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", staging).Run()
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
	return []string{"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", script}
}

// PrepareWindowsRelaunch prepares a detached helper that waits for pid to exit
// and starts executable cleanly from its install directory.
func PrepareWindowsRelaunch(executable string, pid int) (Outcome, error) {
	if strings.TrimSpace(executable) == "" {
		return Outcome{}, errors.New("找不到当前程序")
	}
	if pid == 0 {
		pid = os.Getpid()
	}
	script, err := writeWindowsScript()
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		InstalledPath: executable,
		Quit:          true,
		Helper:        "powershell.exe",
		HelperArgs:    WindowsHelperArgs(script, pid, "", executable, ""),
	}, nil
}

// WindowsScript is the PowerShell that runs after this process exits.
// Paths are parameters so the script itself stays ASCII.
func WindowsScript() string {
	return strings.Join([]string{
		"param([int]$ProcessId, [string]$Source, [string]$Destination, [string]$Installer, [string]$ExpectedVersion = '')",
		"$script:scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path",
		"$script:logFile = ''",
		"if ($Installer -ne '') {",
		"  $ld = Split-Path -Parent $Installer",
		"  if ($ld -and [System.IO.Directory]::Exists($ld)) { $script:logFile = [System.IO.Path]::Combine($ld, 'update-helper.log') }",
		"} elseif ($Source -ne '') {",
		"  $ld = Split-Path -Parent $Source",
		"  if ($ld -and [System.IO.Directory]::Exists($ld)) { $script:logFile = [System.IO.Path]::Combine($ld, 'update-helper.log') }",
		"}",
		"function Write-UpdateLog([string]$Msg) {",
		"  if ($script:logFile -eq '') { return }",
		"  try {",
		"    $ts = (Get-Date).ToString('yyyy-MM-dd HH:mm:ss.fff')",
		"    [System.IO.File]::AppendAllText($script:logFile, ('[' + $ts + '] ' + $Msg + [Environment]::NewLine), [System.Text.Encoding]::UTF8)",
		"  } catch {}",
		"}",
		"function Cleanup-Self {",
		"  if ($script:scriptDir -and (Test-Path -LiteralPath $script:scriptDir)) {",
		"    Remove-Item -LiteralPath $script:scriptDir -Recurse -Force -ErrorAction SilentlyContinue",
		"  }",
		"}",
		"function Normalize-Ver([string]$v) {",
		"  if ($null -eq $v) { return '' }",
		"  $v = $v.Trim()",
		"  if ($v.StartsWith('v') -or $v.StartsWith('V')) { $v = $v.Substring(1) }",
		"  return $v",
		"}",
		"function Version-Matches([string]$Got, [string]$Want) {",
		"  $g = Normalize-Ver $Got",
		"  $w = Normalize-Ver $Want",
		"  if ($g -eq '' -or $w -eq '') { return $false }",
		"  return ($g -eq $w -or $g -eq ($w + '.0'))",
		"}",
		"function Get-ExeVersion([string]$Path) {",
		"  if ($Path -eq '' -or -not [System.IO.File]::Exists($Path)) { return '' }",
		"  for ($i = 0; $i -lt 20; $i++) {",
		"    try {",
		"      $info = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($Path)",
		"      $pv = Normalize-Ver $info.ProductVersion",
		"      if ($pv -ne '') { return $pv }",
		"      $fv = Normalize-Ver $info.FileVersion",
		"      if ($fv -ne '') { return $fv }",
		"    } catch {",
		"      if ($i + 1 -lt 20) { Start-Sleep -Milliseconds 250 }",
		"      continue",
		"    }",
		"    $readable = $false",
		"    $fs = $null",
		"    try {",
		"      $fs = [System.IO.File]::Open($Path, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::Read)",
		"      $readable = $true",
		"    } catch {",
		"      $readable = $false",
		"    } finally {",
		"      if ($null -ne $fs) { $fs.Dispose() }",
		"    }",
		"    if ($readable) {",
		"      Start-Sleep -Milliseconds 50",
		"      try {",
		"        $info2 = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($Path)",
		"        $pv2 = Normalize-Ver $info2.ProductVersion",
		"        if ($pv2 -ne '') { return $pv2 }",
		"        $fv2 = Normalize-Ver $info2.FileVersion",
		"        if ($fv2 -ne '') { return $fv2 }",
		"        return ''",
		"      } catch {}",
		"    }",
		"    if ($i + 1 -lt 20) { Start-Sleep -Milliseconds 250 }",
		"  }",
		"  return ''",
		"}",
		"function Get-FileSha256([string]$Path) {",
		"  if ($Path -eq '' -or -not [System.IO.File]::Exists($Path)) { return '' }",
		"  $share = [System.IO.FileShare]::ReadWrite -bor [System.IO.FileShare]::Delete",
		"  for ($i = 0; $i -lt 30; $i++) {",
		"    $stream = $null",
		"    $sha = $null",
		"    try {",
		"      $stream = [System.IO.File]::Open($Path, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, $share)",
		"      $sha = [System.Security.Cryptography.SHA256]::Create()",
		"      $bytes = $sha.ComputeHash($stream)",
		"      return ([System.BitConverter]::ToString($bytes) -replace '-', '').ToLowerInvariant()",
		"    } catch {",
		"      if ($i + 1 -lt 30) { Start-Sleep -Milliseconds 250 }",
		"    } finally {",
		"      if ($null -ne $sha) { $sha.Dispose() }",
		"      if ($null -ne $stream) { $stream.Dispose() }",
		"    }",
		"  }",
		"  return ''",
		"}",
		"function Test-UpdatedTarget([string]$Path, [string]$OldHash, [string]$OldVer, [string]$ExpectedVer, [string]$WantHash, [bool]$MovedOld) {",
		"  if ($Path -eq '' -or -not [System.IO.File]::Exists($Path)) { return $false }",
		"  try {",
		"    $fi = New-Object System.IO.FileInfo($Path)",
		"    if ($fi.Length -le 0) { return $false }",
		"  } catch {",
		"    return $false",
		"  }",
		"  if ($WantHash -ne '') {",
		"    $gotHash = Get-FileSha256 $Path",
		"    if ($gotHash -eq '' -or $gotHash -ne $WantHash) { return $false }",
		"  } elseif ($OldHash -ne '') {",
		"    $gotHash = Get-FileSha256 $Path",
		"    if ($gotHash -eq '' -or $gotHash -eq $OldHash) { return $false }",
		"  } elseif (-not $MovedOld) {",
		"    $gotVerCheck = Get-ExeVersion $Path",
		"    if ($ExpectedVer -eq '' -or $gotVerCheck -eq '' -or -not (Version-Matches $gotVerCheck $ExpectedVer)) { return $false }",
		"  }",
		"  if ($ExpectedVer -ne '') {",
		"    $gotVer = Get-ExeVersion $Path",
		"    if ($gotVer -ne '') {",
		"      if (-not (Version-Matches $gotVer $ExpectedVer)) { return $false }",
		"    } elseif ($OldVer -ne '') {",
		"      return $false",
		"    }",
		"  }",
		"  return $true",
		"}",
		"function Remove-WithRetry([string]$Path, [int]$MaxTries) {",
		"  if ($Path -eq '' -or -not [System.IO.File]::Exists($Path)) { return }",
		"  for ($i = 0; $i -lt $MaxTries; $i++) {",
		"    try { [System.IO.File]::SetAttributes($Path, [System.IO.FileAttributes]::Normal) } catch {}",
		"    try {",
		"      [System.IO.File]::Delete($Path)",
		"      return",
		"    } catch {",
		"      if (-not [System.IO.File]::Exists($Path)) { return }",
		"      if ($i + 1 -lt $MaxTries) { Start-Sleep -Milliseconds 250 }",
		"    }",
		"  }",
		"}",
		"function Prepare-BackupPath([string]$Dest) {",
		"  $primary = \"$Dest.previous\"",
		"  if (-not [System.IO.File]::Exists($primary)) { return $primary }",
		"  Remove-WithRetry $primary 10",
		"  if (-not [System.IO.File]::Exists($primary)) { return $primary }",
		"  return \"$Dest.previous.$PID\"",
		"}",
		// Use System.IO.File directly instead of Move-Item/Copy-Item -Destination,
		// because PowerShell 5.1 interprets '[' and ']' in -Destination as wildcards.
		"function Move-FileWithRetry([string]$From, [string]$To, [int]$MaxTries) {",
		"  for ($i = 0; $i -lt $MaxTries; $i++) {",
		"    try {",
		"      if ([System.IO.File]::Exists($To)) {",
		"        try { [System.IO.File]::SetAttributes($To, [System.IO.FileAttributes]::Normal) } catch {}",
		"        [System.IO.File]::Delete($To)",
		"      }",
		"      [System.IO.File]::Move($From, $To)",
		"      return $true",
		"    } catch {",
		"      if ($i + 1 -lt $MaxTries) { Start-Sleep -Milliseconds 250 }",
		"    }",
		"  }",
		"  return $false",
		"}",
		"function Copy-FileWithRetry([string]$From, [string]$To, [int]$MaxTries) {",
		"  for ($i = 0; $i -lt $MaxTries; $i++) {",
		"    try {",
		"      if ([System.IO.File]::Exists($To)) {",
		"        try { [System.IO.File]::SetAttributes($To, [System.IO.FileAttributes]::Normal) } catch {}",
		"      }",
		"      [System.IO.File]::Copy($From, $To, $true)",
		"      return $true",
		"    } catch {",
		"      if ($i + 1 -lt $MaxTries) { Start-Sleep -Milliseconds 250 }",
		"    }",
		"  }",
		"  return $false",
		"}",
		"function Restore-Backup([string]$Backup, [string]$Dest) {",
		"  if ($Backup -eq '' -or -not [System.IO.File]::Exists($Backup)) { return $false }",
		"  if ($Dest -ne '' -and [System.IO.File]::Exists($Dest)) {",
		"    $failed = \"$Dest.failed.$PID\"",
		"    if (Move-FileWithRetry $Dest $failed 20) {",
		"      Remove-WithRetry $failed 4",
		"    } else {",
		"      Remove-WithRetry $Dest 4",
		"    }",
		"  }",
		"  if (Move-FileWithRetry $Backup $Dest 20) { return $true }",
		"  return (Copy-FileWithRetry $Backup $Dest 20)",
		"}",
		"function Test-ProcessExited($proc) {",
		"  if ($null -eq $proc) { return $true }",
		"  try { return [bool]$proc.HasExited } catch { return $false }",
		"}",
		"function Get-Win32ErrorCode($Err) {",
		"  $ex = $Err",
		"  for ($d = 0; $d -lt 10 -and $null -ne $ex; $d++) {",
		"    if ($ex -is [System.ComponentModel.Win32Exception]) { return [int]$ex.NativeErrorCode }",
		"    if ($null -ne $ex.InnerException) {",
		"      $ex = $ex.InnerException",
		"    } elseif ($ex.PSObject.Properties['ErrorRecord'] -and $null -ne $ex.ErrorRecord -and $null -ne $ex.ErrorRecord.Exception -and -not [object]::ReferenceEquals($ex.ErrorRecord.Exception, $ex)) {",
		"      $ex = $ex.ErrorRecord.Exception",
		"    } else {",
		"      break",
		"    }",
		"  }",
		"  return 0",
		"}",
		"function Start-Target([string]$Path, [int]$Retries = 40) {",
		"  if ($Path -eq '' -or -not [System.IO.File]::Exists($Path)) { return $null }",
		"  $wd = Split-Path -Parent $Path",
		"  $lastErr = ''",
		"  for ($i = 0; $i -lt $Retries; $i++) {",
		"    $directCode = 0",
		"    try {",
		"      $psi = New-Object System.Diagnostics.ProcessStartInfo",
		"      $psi.FileName = $Path",
		"      $psi.UseShellExecute = $false",
		"      if ($wd -and [System.IO.Directory]::Exists($wd)) { $psi.WorkingDirectory = $wd }",
		"      $proc = [System.Diagnostics.Process]::Start($psi)",
		"      if ($null -ne $proc) { return $proc }",
		"    } catch {",
		"      $lastErr = $_.Exception.Message",
		"      $directCode = Get-Win32ErrorCode $_.Exception",
		"    }",
		"    if ($directCode -ne 32 -and $directCode -ne 33) {",
		"      try {",
		"        if ($wd -and [System.IO.Directory]::Exists($wd)) {",
		"          return (Start-Process -FilePath $Path -WorkingDirectory $wd -PassThru -ErrorAction Stop)",
		"        }",
		"        return (Start-Process -FilePath $Path -PassThru -ErrorAction Stop)",
		"      } catch {",
		"        $lastErr = $_.Exception.Message",
		"        if ((Get-Win32ErrorCode $_.Exception) -eq 1223) { break }",
		"      }",
		"    }",
		"    if ($i + 1 -lt $Retries) { Start-Sleep -Milliseconds 500 }",
		"  }",
		"  if ($lastErr -ne '') { Write-UpdateLog ('failed to start target ' + $Path + ': ' + $lastErr) }",
		"  return $null",
		"}",
		"function Invoke-SilentInstaller([string]$Installer, [string]$InstallArgs) {",
		"  $p = $null",
		"  $lastErr = ''",
		"  $launched = $false",
		"  for ($i = 0; $i -lt 25; $i++) {",
		"    $directCode = 0",
		"    try {",
		"      $psi = New-Object System.Diagnostics.ProcessStartInfo",
		"      $psi.FileName = $Installer",
		"      $psi.Arguments = $InstallArgs",
		"      $psi.UseShellExecute = $false",
		"      $p = [System.Diagnostics.Process]::Start($psi)",
		"      if ($null -ne $p) {",
		"        $launched = $true",
		"        break",
		"      }",
		"    } catch {",
		"      $p = $null",
		"      $lastErr = $_.Exception.Message",
		"      $directCode = Get-Win32ErrorCode $_.Exception",
		"    }",
		"    if ($directCode -ne 32 -and $directCode -ne 33) {",
		"      try {",
		"        if ($InstallArgs -ne '') {",
		"          $p = Start-Process -FilePath $Installer -ArgumentList $InstallArgs -PassThru -ErrorAction Stop",
		"        } else {",
		"          $p = Start-Process -FilePath $Installer -PassThru -ErrorAction Stop",
		"        }",
		"        $launched = $true",
		"        break",
		"      } catch {",
		"        $p = $null",
		"        $lastErr = $_.Exception.Message",
		"        if ((Get-Win32ErrorCode $_.Exception) -eq 1223) {",
		"          Write-UpdateLog 'silent installer launch cancelled by user'",
		"          return -4",
		"        }",
		"      }",
		"    }",
		"    if ($i + 1 -lt 25) { Start-Sleep -Milliseconds 400 }",
		"  }",
		"  if (-not $launched -or $null -eq $p) {",
		"    if ($lastErr -ne '') { Write-UpdateLog ('failed to launch installer: ' + $lastErr) }",
		"    elseif ($launched) { Write-UpdateLog 'silent installer launched without a process handle' }",
		"    return -1",
		"  }",
		"  try {",
		"    # Cache Handle before WaitForExit so PowerShell 5.1 does not drop ExitCode.",
		"    try { $null = $p.Handle } catch {}",
		"    $exited = $false",
		"    try {",
		"      $exited = $p.WaitForExit(300000)",
		"    } catch {",
		"      Write-UpdateLog ('failed waiting for silent installer: ' + $_.Exception.Message)",
		"      return -1",
		"    }",
		"    if (-not $exited) {",
		"      Write-UpdateLog 'silent installer timed out after 300s; terminating'",
		"      try { $p.Kill() } catch {}",
		"      try { $null = $p.WaitForExit(5000) } catch {}",
		"      return -3",
		"    }",
		"    try { $p.WaitForExit() } catch {}",
		"    try {",
		"      if ($null -eq $p.ExitCode) { return -2 }",
		"      return [int]$p.ExitCode",
		"    } catch {",
		"      return -2",
		"    }",
		"  } finally {",
		"    try { $p.Dispose() } catch {}",
		"  }",
		"}",
		"function Confirm-Started($proc, [string]$stamp) {",
		"  try {",
		"    if ($stamp -eq '') { return ($null -ne $proc) }",
		"    if ($null -eq $proc) {",
		"      for ($j = 0; $j -lt 50; $j++) {",
		"        if ([System.IO.File]::Exists($stamp)) { return $true }",
		"        Start-Sleep -Milliseconds 200",
		"      }",
		"      return [System.IO.File]::Exists($stamp)",
		"    }",
		"    $ok = $false",
		"    $waitTries = 0",
		"    while ($waitTries -lt 50) {",
		"      if ([System.IO.File]::Exists($stamp)) { $ok = $true; break }",
		"      if (Test-ProcessExited $proc) { break }",
		"      Start-Sleep -Milliseconds 200",
		"      $waitTries = $waitTries + 1",
		"    }",
		"    if (-not $ok) { $ok = (-not (Test-ProcessExited $proc)) -or [System.IO.File]::Exists($stamp) }",
		"    return $ok",
		"  } finally {",
		"    if ($null -ne $proc) { try { $proc.Dispose() } catch {} }",
		"  }",
		"}",
		"$mode = 'relaunch'",
		"if ($Installer -ne '') { $mode = 'setup' } elseif ($Source -ne '') { $mode = 'portable' }",
		"Write-UpdateLog ('start mode=' + $mode + ' pid=' + $ProcessId + ' dest=' + $Destination + ' want=' + $ExpectedVersion)",
		"# Wait on the parent process handle first so Windows PID recycling cannot",
		"# stall the update for 2 minutes; fall back to bounded polling if needed.",
		"if ($ProcessId -gt 0) {",
		"  $parentProc = Get-Process -Id $ProcessId -ErrorAction SilentlyContinue",
		"  if ($null -ne $parentProc) {",
		"    $waitedByHandle = $false",
		"    try {",
		"      $parentExited = $parentProc.WaitForExit(120000)",
		"      $waitedByHandle = $true",
		"      if (-not $parentExited) { Write-UpdateLog 'parent process did not exit within 120s; continuing' }",
		"    } catch {",
		"      $waitedByHandle = $false",
		"    } finally {",
		"      try { $parentProc.Dispose() } catch {}",
		"    }",
		"    if (-not $waitedByHandle) {",
		"      $tries = 0",
		"      while ((Get-Process -Id $ProcessId -ErrorAction SilentlyContinue) -and ($tries -lt 300)) { Start-Sleep -Milliseconds 400; $tries = $tries + 1 }",
		"    }",
		"  }",
		"}",
		"if ($Installer -ne '') {",
		"  # /D installs over the copy the user runs, not wherever an older installer",
		"  # was last pointed at, and must stay the last argument, unquoted.",
		"  $installArgs = '/S'",
		"  $dir = ''",
		"  if ($Destination -ne '') { $dir = Split-Path -Parent $Destination }",
		"  if ($dir -and [System.IO.Directory]::Exists($dir)) { $installArgs = '/S /D=' + $dir }",
		"  $stamp = ''",
		"  if ($Destination -ne '') {",
		"    $stamp = \"$Destination.started\"",
		"    Remove-WithRetry $stamp 4",
		"  }",
		"  $oldHash = ''",
		"  $oldVer = ''",
		"  $backup = ''",
		"  $movedOld = $false",
		"  if ($Destination -ne '' -and [System.IO.File]::Exists($Destination)) {",
		"    $oldHash = Get-FileSha256 $Destination",
		"    $oldVer = Get-ExeVersion $Destination",
		"    $backup = Prepare-BackupPath $Destination",
		"    # Rename the old executable aside before running NSIS /S: Windows allows",
		"    # renaming a read-locked exe, whereas NSIS silent File extraction skips",
		"    # locked files and still exits 0.",
		"    if (Move-FileWithRetry $Destination $backup 25) {",
		"      $movedOld = $true",
		"      if ($oldHash -eq '') { $oldHash = Get-FileSha256 $backup }",
		"      if ($oldVer -eq '') { $oldVer = Get-ExeVersion $backup }",
		"    }",
		"  }",
		"  $installed = $false",
		"  for ($attempt = 0; $attempt -lt 2; $attempt++) {",
		"    $code = Invoke-SilentInstaller $Installer $installArgs",
		"    if ($code -eq 0 -or $code -eq -2) {",
		"      if ($Destination -eq '') {",
		"        if ($code -eq 0) { $installed = $true; break }",
		"      } elseif (Test-UpdatedTarget $Destination $oldHash $oldVer $ExpectedVersion '' $movedOld) {",
		"        $installed = $true",
		"        break",
		"      }",
		"    }",
		"    $gotVer = Get-ExeVersion $Destination",
		"    $destExists = ($Destination -ne '' -and [System.IO.File]::Exists($Destination))",
		"    Write-UpdateLog ('silent installer attempt ' + ($attempt + 1) + ' did not update target (exit=' + $code + ', exists=' + $destExists + ', ver=' + $gotVer + ', want=' + $ExpectedVersion + ')')",
		"    if ($code -eq -3 -or $code -eq -4) { break }",
		"    if ($attempt -eq 0) {",
		"      if ($movedOld -and $destExists) { Remove-WithRetry $Destination 10 }",
		"      Start-Sleep -Milliseconds 1000",
		"    }",
		"  }",
		"  if (-not $installed) {",
		"    $userCancelled = ($code -eq -4)",
		"    if ($userCancelled) {",
		"      Write-UpdateLog 'silent installer cancelled by user; restoring backup'",
		"    } else {",
		"      Write-UpdateLog 'silent installer failed verification; restoring backup and opening interactive installer'",
		"    }",
		"    if ($movedOld -and $backup -ne '' -and [System.IO.File]::Exists($backup)) {",
		"      Restore-Backup $backup $Destination | Out-Null",
		"    }",
		"    $opened = $false",
		"    if (-not $userCancelled) {",
		"      for ($k = 0; $k -lt 5; $k++) {",
		"        try {",
		"          if ($dir -and [System.IO.Directory]::Exists($dir)) {",
		"            Start-Process -FilePath $Installer -ArgumentList ('/D=' + $dir) -ErrorAction Stop | Out-Null",
		"          } else {",
		"            Start-Process -FilePath $Installer -ErrorAction Stop | Out-Null",
		"          }",
		"          $opened = $true",
		"          break",
		"        } catch {",
		"          if ((Get-Win32ErrorCode $_.Exception) -eq 1223) { $userCancelled = $true; break }",
		"          if ($k + 1 -lt 5) { Start-Sleep -Milliseconds 400 }",
		"        }",
		"      }",
		"      if (-not $opened -and -not $userCancelled) {",
		"        try {",
		"          Start-Process -FilePath $Installer -ErrorAction Stop | Out-Null",
		"          $opened = $true",
		"        } catch {",
		"          Write-UpdateLog ('failed to launch interactive installer: ' + $_.Exception.Message)",
		"        }",
		"      }",
		"    }",
		"    if (-not $opened -and $Destination -ne '' -and [System.IO.File]::Exists($Destination)) {",
		"      Start-Target $Destination 10 | Out-Null",
		"    }",
		"    if ($userCancelled) {",
		"      Write-UpdateLog 'result=cancelled mode=setup'",
		"    } else {",
		"      Write-UpdateLog ('result=fallback_interactive mode=setup opened=' + $opened)",
		"    }",
		"    Cleanup-Self",
		"    exit 1",
		"  }",
		"  # A silent install skips the finish page, so the app is opened here.",
		"  if ($Destination -ne '' -and [System.IO.File]::Exists($Destination)) {",
		"    $proc = Start-Target $Destination",
		"    $ok = Confirm-Started $proc $stamp",
		"    if (-not $ok -and $movedOld -and $backup -ne '' -and [System.IO.File]::Exists($backup)) {",
		"      Write-UpdateLog 'new setup binary failed to start; rolling back to previous version'",
		"      Restore-Backup $backup $Destination | Out-Null",
		"      Start-Target $Destination 15 | Out-Null",
		"      Write-UpdateLog 'result=rollback mode=setup'",
		"      Cleanup-Self",
		"      exit 1",
		"    }",
		"    if ($ok) {",
		"      Remove-WithRetry $backup 20",
		"      Remove-WithRetry $stamp 4",
		"      Remove-WithRetry $Installer 10",
		"      Write-UpdateLog ('result=ok mode=setup ver=' + (Get-ExeVersion $Destination))",
		"    } else {",
		"      Write-UpdateLog 'result=start_unconfirmed mode=setup'",
		"    }",
		"  } else {",
		"    Write-UpdateLog 'result=ok mode=setup'",
		"  }",
		"  Cleanup-Self",
		"  exit 0",
		"}",
		"if ($Source -eq '') {",
		"  if ($Destination -ne '' -and [System.IO.File]::Exists($Destination)) { Start-Target $Destination 15 | Out-Null }",
		"  Write-UpdateLog 'result=ok mode=relaunch'",
		"  Cleanup-Self",
		"  exit 0",
		"}",
		"$stamp = \"$Destination.started\"",
		"Remove-WithRetry $stamp 4",
		"$sourceHash = Get-FileSha256 $Source",
		"$oldHash = Get-FileSha256 $Destination",
		"$oldVer = Get-ExeVersion $Destination",
		"$staged = \"$Destination.new\"",
		"if (-not (Copy-FileWithRetry $Source $staged 25)) {",
		"  Write-UpdateLog 'failed to stage portable update from source'",
		"  Write-UpdateLog 'result=fail mode=portable stage=copy'",
		"  Remove-WithRetry $staged 4",
		"  Start-Target $Destination 15 | Out-Null",
		"  Cleanup-Self",
		"  exit 1",
		"}",
		"if ($sourceHash -ne '') {",
		"  $stagedHash = Get-FileSha256 $staged",
		"  if ($stagedHash -ne $sourceHash) {",
		"    Write-UpdateLog 'staged portable update hash mismatch'",
		"    Write-UpdateLog 'result=fail mode=portable stage=hash'",
		"    Remove-WithRetry $staged 4",
		"    Start-Target $Destination 15 | Out-Null",
		"    Cleanup-Self",
		"    exit 1",
		"  }",
		"}",
		"$backup = Prepare-BackupPath $Destination",
		"$movedOld = $false",
		"if ([System.IO.File]::Exists($Destination)) {",
		"  if (Move-FileWithRetry $Destination $backup 40) {",
		"    $movedOld = $true",
		"    if ($oldHash -eq '') { $oldHash = Get-FileSha256 $backup }",
		"    if ($oldVer -eq '') { $oldVer = Get-ExeVersion $backup }",
		"  } else {",
		"    Write-UpdateLog 'failed to move current executable aside before swap'",
		"    Write-UpdateLog 'result=fail mode=portable stage=move_old'",
		"    Remove-WithRetry $staged 4",
		"    Start-Target $Destination 15 | Out-Null",
		"    Cleanup-Self",
		"    exit 1",
		"  }",
		"}",
		"$replaced = Move-FileWithRetry $staged $Destination 80",
		"if ($replaced -and -not (Test-UpdatedTarget $Destination $oldHash $oldVer $ExpectedVersion $sourceHash $movedOld)) {",
		"  Write-UpdateLog 'portable target verification failed after swap'",
		"  $replaced = $false",
		"}",
		"if (-not $replaced) {",
		"  Write-UpdateLog 'failed to swap staged executable into place; restoring backup'",
		"  Write-UpdateLog 'result=fail mode=portable stage=swap'",
		"  Remove-WithRetry $staged 4",
		"  if ($movedOld -and [System.IO.File]::Exists($backup)) {",
		"    Restore-Backup $backup $Destination | Out-Null",
		"  }",
		"  Start-Target $Destination 15 | Out-Null",
		"  Cleanup-Self",
		"  exit 1",
		"}",
		"$proc = Start-Target $Destination",
		"$ok = Confirm-Started $proc $stamp",
		"if (-not $ok) {",
		"  Write-UpdateLog 'new portable binary failed to start; rolling back to previous version'",
		"  if ([System.IO.File]::Exists($backup)) {",
		"    Restore-Backup $backup $Destination | Out-Null",
		"    Start-Target $Destination 15 | Out-Null",
		"  }",
		"  Write-UpdateLog 'result=rollback mode=portable'",
		"  Cleanup-Self",
		"  exit 1",
		"}",
		"Remove-WithRetry $Source 10",
		// 杀毒软件扫描刚写入的 exe 时会短暂占用它，删一次失败就会留下 tulai.exe.previous。
		// 多试几十次，每次之间停 250ms，仍然失败也不影响新程序运行。
		"Remove-WithRetry $backup 20",
		"Remove-WithRetry $stamp 4",
		"Write-UpdateLog ('result=ok mode=portable ver=' + (Get-ExeVersion $Destination))",
		"Cleanup-Self",
		"exit 0",
		"",
	}, "\r\n")
}

func writeWindowsScript() (string, error) {
	dir, err := os.MkdirTemp("", "tulai-replace-")
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
	return windowsHelperArgs(script, pid, source, destination, installer, "")
}

func windowsHelperArgs(script string, pid int, source, destination, installer, expectedVersion string) []string {
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
	if v := strings.TrimSpace(expectedVersion); v != "" {
		args = append(args, "-ExpectedVersion", v)
	}
	return args
}
