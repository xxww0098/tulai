package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/realbrowser/browserkernel"

	"github.com/xxww0098/tulai/internal/appdata"
	"github.com/xxww0098/tulai/internal/fingerprint"
	"github.com/xxww0098/tulai/internal/store"
)

const KindFingerprint = store.EngineFingerprint

type Runtime struct{ Kind, Path, Version string }

func DefaultBackend(string) string                 { return KindFingerprint }
func CanonicalBackend(string) string               { return KindFingerprint }
func NormalizeStoredBackend(string, string) string { return KindFingerprint }
func ChoiceLabel(string) string                    { return KindFingerprint }

// AccountEngines reports the managed browser engine used by local accounts.
func AccountEngines(_ []store.Browser, _, _ string) []string {
	return []string{KindFingerprint}
}

func RuntimeSupported(goos, goarch, _ string) bool {
	return fingerprint.FingerprintSupported(goos, goarch)
}

func ConfigDir() string {
	dir, err := appdata.Dir()
	if err != nil {
		return ""
	}
	return dir
}

func Resolve(goos, _, dataDir string) (Runtime, error) {
	if goos != runtime.GOOS {
		return Runtime{}, errors.New("只能启动当前系统的受管浏览器")
	}
	if rt, ok := InstalledRuntime(dataDir, KindFingerprint); ok {
		return rt, nil
	}
	return Runtime{}, errors.New(KindFingerprint + " 尚未准备好，请先在「设置 → 浏览器与网络」安装 " + KindFingerprint)
}

func InstalledRuntime(dataDir, _ string) (Runtime, bool) {
	rt, ok := fingerprint.InstalledFingerprint(dataDir)
	return Runtime{Kind: KindFingerprint, Path: rt.Executable, Version: rt.Version}, ok
}

func Detect(dataDir string) []Runtime {
	if rt, ok := InstalledRuntime(dataDir, KindFingerprint); ok {
		return []Runtime{rt}
	}
	return nil
}

func FormatDetected(list []Runtime) (string, string) {
	for _, rt := range list {
		if rt.Kind == KindFingerprint {
			return rt.Kind + " 已就绪", rt.Path
		}
	}
	return KindFingerprint + " 尚未安装", ""
}

func EnsureRuntime(ctx context.Context, dataDir, _ string, progress func(fingerprint.Progress)) (Runtime, error) {
	proxy, err := ResolveCurrentProxy()
	if err != nil {
		return Runtime{}, err
	}
	rt, err := fingerprint.EnsureFingerprint(ctx, dataDir, proxy, progress)
	return Runtime{Kind: KindFingerprint, Path: rt.Executable, Version: rt.Version}, err
}

// runtimeUpdater uses the same per-application proxy as the installer.
func runtimeUpdater() (*browserkernel.KernelManager, error) {
	proxy, err := ResolveCurrentProxy()
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(u)
	}
	mgr := browserkernel.NewKernelManager("", "")
	mgr.HTTPClient = &http.Client{Transport: transport, Timeout: 20 * time.Second}
	// This upstream build crashes on canvas readback in image generation.
	mgr.AcceptRelease = func(tag string) bool { return strings.TrimPrefix(tag, "v") != "150.0.7871.186" }
	if runtime.GOOS == "linux" {
		mgr.ArchiveSuffix = ".tar.xz"
	}
	return mgr, nil
}

func CheckRuntimeUpdate(ctx context.Context, dataDir string) (string, error) {
	rt, ok := fingerprint.InstalledFingerprint(dataDir)
	if !ok {
		return "", nil
	}
	mgr, err := runtimeUpdater()
	if err != nil {
		return "", err
	}
	defer mgr.HTTPClient.CloseIdleConnections()
	return mgr.CheckUpdate(ctx, rt.Version)
}

func UpdateRuntime(ctx context.Context, dataDir, _ string, progress func(fingerprint.Progress)) (Runtime, error) {
	rt, ok := fingerprint.InstalledFingerprint(dataDir)
	if !ok {
		return Runtime{}, fmt.Errorf("请先安装 %s", KindFingerprint)
	}
	mgr, err := runtimeUpdater()
	if err != nil {
		return Runtime{}, err
	}
	defer mgr.HTTPClient.CloseIdleConnections()
	active, _, err := mgr.Update(ctx, filepath.Join(rt.Dir, "kernels"), rt.Version, func(done, total int64) {
		if progress != nil {
			progress(fingerprint.Progress{Stage: fingerprint.StageBrowser, Text: "正在更新浏览器内核…", Done: done, Total: total})
		}
	})
	if err != nil {
		return Runtime{}, err
	}
	if active.Executable != "" {
		rt.Executable, rt.Version = active.Executable, active.Version
	}
	return Runtime{Kind: KindFingerprint, Path: rt.Executable, Version: rt.Version}, nil
}
