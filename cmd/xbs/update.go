package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"xbs/internal/store"
	"xbs/internal/update"
)

const updateUsage = `tulai
用法:
  xbs                 打开工作台
  xbs update          检查 GitHub Releases 并安装更新
  xbs update --check  只检查，不下载
  xbs version         打印桌面端版本`

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
	if mygo.App.Version() == "" {
		mygo.App.SetVersion(ProductVersion)
	}
	return mygo.App.Version()
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
	run.Client.ApplyMirrorSetting(configuredMirror())
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
	if checkOnly || !newer {
		return printRelease(run.Stdout, current, rel)
	}
	fmt.Fprintln(run.Stderr, update.UnsignedWarning)
	prepared, err := run.Client.Download(ctx, rel, current, mode, run.GOARCH)
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

func configuredMirror() string {
	if v := strings.TrimSpace(os.Getenv("XBS_UPDATE_MIRROR")); v != "" {
		return v
	}
	path := filepath.Join("database", "config.db")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	st, err := store.Open(path)
	if err != nil {
		return ""
	}
	defer st.Close()
	return st.Get(update.KeyMirror)
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
