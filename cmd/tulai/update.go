package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/ui"
	"github.com/xxww0098/tulai/internal/update"
)

const updateUsage = `tulai
用法:
  tulai                 打开工作台
  tulai update          检查 GitHub Releases 并安装更新
  tulai update --check  只检查，不下载
  tulai version         打印桌面端版本`

// handleCommand runs a version or update command. It reports whether it
// handled the process and the caller should return.
func handleCommand(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[1] {
	case "version", "--version", "-version":
		fmt.Println(runningVersion())
		return true
	case "help", "--help", "-h":
		fmt.Println(updateUsage)
		return true
	case "update":
		os.Exit(runUpdate(args[2:]))
	}
	return false
}

func runningVersion() string {
	return runningVersionFrom(ui.PackagedVersion())
}

func runningVersionFrom(packaged string) string {
	if version := strings.TrimSpace(packaged); version != "" {
		return version
	}
	return ProductVersion
}

func runUpdate(args []string) int {
	return runUpdateWith(args, updateRun{
		Client: update.NewClient(runningVersion()),
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
}

type updateRun struct {
	Client     *update.Client
	Stdout     io.Writer
	Stderr     io.Writer
	Home       string
	DataHome   string
	Executable string
	GOOS       string
	GOARCH     string
	Start      func(path string, args ...string) error
}

func runUpdateWith(args []string, run updateRun) int {
	checkOnly := false
	for _, arg := range args {
		switch arg {
		case "--check", "-n":
			checkOnly = true
		case "-h", "--help":
			fmt.Fprintln(run.Stdout, updateUsage)
			return 0
		default:
			fmt.Fprintf(run.Stderr, "未知参数 %s\n%s\n", arg, updateUsage)
			return 2
		}
	}
	if run.Client == nil {
		run.Client = update.NewClient(runningVersion())
	}
	if run.Stdout == nil {
		run.Stdout = os.Stdout
	}
	if run.Stderr == nil {
		run.Stderr = os.Stderr
	}
	if run.GOOS == "" {
		run.GOOS = runtime.GOOS
	}
	if run.GOARCH == "" {
		run.GOARCH = runtime.GOARCH
	}
	if run.Executable == "" {
		exe, err := update.RunningExecutable()
		if err != nil {
			fmt.Fprintf(run.Stderr, "定位程序失败: %v\n", err)
			return 1
		}
		run.Executable = exe
	}
	mirror, proxy := configuredUpdateNetwork(run.GOOS, run.Executable)
	run.Client.ApplyMirrorSetting(mirror)
	if run.Client.ProxyURL == "" && proxy != "" {
		run.Client.ProxyURL = proxy
	}
	current := run.Client.Version
	if current == "" {
		current = runningVersion()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	mode := update.DetectMode(run.GOOS, run.Executable)
	rel, err := run.Client.Latest(ctx)
	if err != nil {
		fmt.Fprintf(run.Stderr, "检查更新失败: %v\n", err)
		return 1
	}
	latest, err := rel.Version()
	if err != nil {
		fmt.Fprintf(run.Stderr, "检查更新失败: %v\n", err)
		return 1
	}
	newer, err := update.Newer(current, latest)
	if err != nil {
		fmt.Fprintf(run.Stderr, "检查更新失败: %v\n", err)
		return 1
	}
	if !newer {
		return printRelease(run.Stdout, current, rel)
	}
	if checkOnly {
		return printRelease(run.Stdout, current, rel)
	}
	fmt.Fprintln(run.Stderr, update.UnsignedWarning)
	var finishCLIProgress func()
	if run.Client.OnProgress == nil && isTerminalWriter(run.Stderr) {
		var mu sync.Mutex
		var printed bool
		run.Client.OnProgress = func(p update.Progress) {
			line := formatCLIUpdateProgress(p)
			mu.Lock()
			printed = true
			_, _ = fmt.Fprintf(run.Stderr, "\r\x1b[K%s", line)
			mu.Unlock()
		}
		finishCLIProgress = func() {
			mu.Lock()
			if printed {
				_, _ = fmt.Fprintln(run.Stderr)
				printed = false
			}
			mu.Unlock()
		}
	}
	prepared, err := run.Client.Download(ctx, rel, current, mode, run.GOARCH)
	if finishCLIProgress != nil {
		finishCLIProgress()
	}
	if err != nil {
		fmt.Fprintf(run.Stderr, "下载更新失败: %v\n", err)
		return 1
	}
	if prepared.UpToDate {
		return printRelease(run.Stdout, current, prepared.Release)
	}
	version, err := prepared.Release.Version()
	if err != nil {
		fmt.Fprintf(run.Stderr, "检查更新失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(run.Stdout, "正在安装 %s（%s）\n", version, prepared.Asset.Name)
	out, err := update.Install(update.Options{
		Mode:       mode,
		Executable: run.Executable,
		AssetPath:  prepared.Path,
		Version:    version,
		Home:       run.Home,
		DataHome:   run.DataHome,
	})
	if err != nil {
		fmt.Fprintf(run.Stderr, "安装失败: %v\n", err)
		return 1
	}
	fmt.Fprintln(run.Stdout, out.Message)
	if out.ManualCommand != "" {
		fmt.Fprintln(run.Stdout, out.ManualCommand)
	}
	if out.OpenPath != "" {
		if err := update.Open(out.OpenPath); err != nil {
			fmt.Fprintf(run.Stderr, "未能打开安装包: %v\n", err)
		}
	}
	if out.Helper != "" {
		start := run.Start
		if start == nil {
			start = update.StartDetached
		}
		if err := start(out.Helper, out.HelperArgs...); err != nil {
			fmt.Fprintf(run.Stderr, "启动安装步骤失败: %v\n", err)
			return 1
		}
	}
	if out.Quit {
		os.Exit(0)
	}
	return 0
}

func configuredMirror(goos, executable string) string {
	mirror, _ := configuredUpdateNetwork(goos, executable)
	return mirror
}

func configuredUpdateNetwork(goos, executable string) (mirror, proxy string) {
	mirror = strings.TrimSpace(os.Getenv("TULAI_UPDATE_MIRROR"))
	path := filepath.Join("database", "config.db")
	if dir, err := packagedDataDir(goos, executable); err == nil && dir != "" {
		path = filepath.Join(dir, "database", "config.db")
	}
	if _, err := os.Stat(path); err != nil {
		return mirror, browser.ProxyServer("", os.Getenv)
	}
	st, err := store.Open(path)
	if err != nil {
		return mirror, browser.ProxyServer("", os.Getenv)
	}
	defer st.Close()
	if mirror == "" {
		mirror = st.Get(update.KeyMirror)
	}
	customProxy := ""
	if st.Get("proxy_mode") == "custom" {
		customProxy = st.Get("proxy_server")
	}
	return mirror, browser.ProxyServer(customProxy, os.Getenv)
}

func printRelease(w io.Writer, current string, rel update.Release) int {
	latest, err := rel.Version()
	if err != nil {
		fmt.Fprintf(w, "检查更新失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "当前版本: %s\n最新版本: %s\n", current, latest)
	newer, err := update.Newer(current, latest)
	if err != nil {
		fmt.Fprintf(w, "检查更新失败: %v\n", err)
		return 1
	}
	if newer {
		fmt.Fprintf(w, "有新版本: %s\n%s\n", rel.Tag, rel.HTMLURL)
		return 0
	}
	if strings.TrimSpace(rel.HTMLURL) != "" {
		fmt.Fprintln(w, rel.HTMLURL)
	}
	fmt.Fprintln(w, "已是最新版本")
	return 0
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func formatCLIUpdateProgress(p update.Progress) string {
	switch p.Phase {
	case update.PhaseChecksums:
		return "正在获取校验清单..."
	case update.PhaseProbing:
		return "正在测速选择最快的下载源..."
	case update.PhaseVerifying:
		return "下载完成，正在校验 SHA-256..."
	}
	const barWidth = 22
	pct := 0.0
	filled := 0
	if p.Total > 0 {
		pct = math.Min(100, math.Max(0, float64(p.Done)*100/float64(p.Total)))
		filled = int(math.Round(pct / 100 * barWidth))
		if filled > barWidth {
			filled = barWidth
		}
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	parts := []string{fmt.Sprintf("[%s] %3.0f%%", bar, pct)}
	if p.Done > 0 && p.Total > 0 {
		parts = append(parts, formatCLIBytes(p.Done)+" / "+formatCLIBytes(p.Total))
	} else if p.Done > 0 {
		parts = append(parts, formatCLIBytes(p.Done))
	}
	if p.BytesPerSec > 0 {
		parts = append(parts, formatCLIBytes(int64(p.BytesPerSec))+"/s")
		if p.Total > 0 && p.Done < p.Total && p.BytesPerSec >= 1024 {
			sec := int(math.Ceil(float64(p.Total-p.Done) / p.BytesPerSec))
			if sec < 1 {
				sec = 1
			}
			parts = append(parts, "剩余 "+formatCLIDuration(sec))
		}
	}
	if p.Source != "" {
		parts = append(parts, p.Source)
	}
	return strings.Join(parts, " · ")
}

func formatCLIBytes(n int64) string {
	const (
		kb = 1024.0
		mb = 1024.0 * 1024.0
	)
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	case n >= 1024:
		return fmt.Sprintf("%.0f KB", float64(n)/kb)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func formatCLIDuration(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%d 秒", sec)
	}
	m := sec / 60
	s := sec % 60
	if s == 0 {
		return fmt.Sprintf("%d 分", m)
	}
	return fmt.Sprintf("%d 分 %d 秒", m, s)
}
