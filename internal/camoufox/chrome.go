package camoufox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The pinned Playwright package selects the matching Chrome for Testing build.
const chromeDriverVersion = "1.62.0"

func ChromeSupported(goos, goarch string) bool {
	// Playwright ships an unbranded Chromium build on Linux ARM64.
	return Supported(goos, goarch) && !(goos == "linux" && goarch == "arm64")
}

func chromeRuntimeDir(dataDir string) string {
	return filepath.Join(dataDir, "runtime", "chrome-cdp", chromeDriverVersion+"-"+nodeVersion+"-"+runtime.GOOS+"-"+runtime.GOARCH)
}

func InstalledChrome(dataDir string) (Runtime, bool) {
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, false
	}
	return chromeAt(chromeRuntimeDir(dataDir))
}

func chromeAt(dir string) (Runtime, bool) {
	marker, err := os.ReadFile(filepath.Join(dir, "browser-path"))
	rel := strings.TrimSpace(string(marker))
	if err != nil || !filepath.IsLocal(rel) || nodePackageVersion(dir, "playwright-core") != chromeDriverVersion {
		return Runtime{}, false
	}
	browsers := filepath.Join(dir, "browsers")
	exe := filepath.Join(browsers, rel)
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return Runtime{}, false
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return Runtime{}, false
	}
	within, err := filepath.Rel(filepath.Join(root, "browsers"), resolved)
	if err != nil || !filepath.IsLocal(within) {
		return Runtime{}, false
	}
	rt := Runtime{Dir: dir, Node: nodePath(dir), Executable: exe}
	for _, f := range []string{rt.Node, exe} {
		if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return Runtime{}, false
		}
	}
	return rt, true
}

// EnsureChrome builds an independent runtime. Chrome-only installations need
// neither a Firefox kernel nor the Camoufox fingerprint and native modules.
func EnsureChrome(ctx context.Context, dataDir, proxy string, progress func(Progress)) (Runtime, error) {
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, errors.New("没有应用数据目录")
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	if !ChromeSupported(runtime.GOOS, runtime.GOARCH) {
		return Runtime{}, fmt.Errorf("Chrome CDP 暂不支持 %s", platform)
	}
	artifact := nodeArtifacts[platform]
	final, err := filepath.Abs(chromeRuntimeDir(dataDir))
	if err != nil {
		return Runtime{}, err
	}
	root := filepath.Dir(final)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Runtime{}, err
	}
	note := func(stage, message string) {
		if progress != nil {
			progress(Progress{Stage: stage, Text: message})
		}
	}
	lock, err := acquireInstallLock(ctx, root, func() { note(StageWaiting, "正在等待 Chrome 安装完成…") })
	if err != nil {
		return Runtime{}, err
	}
	defer releaseInstallLock(lock)
	if rt, ok := chromeAt(final); ok {
		return rt, writeDriverFiles(final, bridgeScripts)
	}
	stage, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Runtime{}, err
	}
	defer os.RemoveAll(stage)
	note(StageRuntime, "正在准备 Chrome CDP 启动环境…")
	if source, size, bundled := embeddedRuntimeBundle(); bundled {
		err = unpackRuntimeBundle(ctx, source, size, stage, platform)
	} else if err = installNode(ctx, stage, proxy, artifact, progress); err == nil {
		note(StagePackages, "正在安装 Playwright…")
		if err = writeChromePackageLock(stage); err == nil {
			err = installLockedPackages(ctx, stage, proxy, progress)
		}
	}
	if err != nil {
		return Runtime{}, err
	}
	if err := writeDriverFiles(stage, bridgeScripts); err != nil {
		return Runtime{}, err
	}
	cache := filepath.Join(stage, "browsers")
	env := []string{"PLAYWRIGHT_BROWSERS_PATH=" + cache}
	note(StageBrowser, "正在下载 Chrome for Testing…")
	cli := filepath.Join(stage, "node_modules", "playwright-core", "cli.js")
	if _, err := runInstall(ctx, stage, proxy, env, nil, cli, "install", "--no-shell", "chromium"); err != nil {
		return Runtime{}, err
	}
	out, err := runInstall(ctx, stage, proxy, env, nil, "-p", "require('playwright-core').chromium.executablePath()")
	if err != nil {
		return Runtime{}, err
	}
	rel, err := filepath.Rel(cache, strings.TrimSpace(string(out)))
	if err != nil || !filepath.IsLocal(rel) {
		return Runtime{}, errors.New("Chrome 安装程序返回的路径超出专用目录")
	}
	if err := os.WriteFile(filepath.Join(stage, "browser-path"), []byte(rel), 0o600); err != nil {
		return Runtime{}, err
	}
	if _, ok := chromeAt(stage); !ok {
		return Runtime{}, errors.New("Chrome for Testing 安装不完整")
	}
	if err := publishRuntime(stage, final); err != nil {
		return Runtime{}, err
	}
	rt, ok := chromeAt(final)
	if !ok {
		return Runtime{}, errors.New("无法读取已安装的 Chrome for Testing")
	}
	return rt, nil
}

// playwright-core has no runtime dependencies. Reuse its exact npm integrity
// record from the shared driver lockfile when preparing a source installation.
func writeChromePackageLock(dir string) error {
	data, err := driver.ReadFile("driver/package-lock.json")
	if err != nil {
		return err
	}
	var lock struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}
	const key = "node_modules/playwright-core"
	pinned := lock.Packages[key]
	var dep struct{ Version, Integrity string }
	if json.Unmarshal(pinned, &dep) != nil || dep.Version != chromeDriverVersion || dep.Integrity == "" {
		return errors.New("Playwright 依赖锁和 Chrome 驱动版本不匹配")
	}
	pkg := map[string]any{"name": "tulai-chrome-cdp", "private": true, "dependencies": map[string]string{"playwright-core": dep.Version}}
	manifest, err := json.Marshal(pkg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), manifest, 0o600); err != nil {
		return err
	}
	data, err = json.Marshal(map[string]any{"name": pkg["name"], "lockfileVersion": 3, "packages": map[string]any{"": pkg, key: pinned}})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "package-lock.json"), data, 0o600)
}
