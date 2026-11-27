package browser

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"

	fpbrowser "github.com/xxww0098/realbrowser/fingerprintkernel/browser"
	fpdriver "github.com/xxww0098/realbrowser/fingerprintkernel/driver"
	"github.com/xxww0098/realbrowser/fingerprintkernel/engine"

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

func runtimeUpdater(dataDir string) (*fpbrowser.ManagedBrowser, error) {
	rt, ok := fingerprint.InstalledFingerprint(dataDir)
	if !ok {
		return nil, nil
	}
	proxy, err := ResolveCurrentProxy()
	if err != nil {
		return nil, err
	}
	base := engine.InstalledRelease{Version: rt.Version, Executable: rt.Executable}
	browser, err := fpdriver.Browser(filepath.Join(rt.Dir, "kernels"), base, proxy)
	if err != nil {
		return nil, err
	}
	return &browser, nil
}

func CheckRuntimeUpdate(ctx context.Context, dataDir string) (string, error) {
	b, err := runtimeUpdater(dataDir)
	if err != nil || b == nil {
		return "", err
	}
	defer b.Manager.HTTPClient.CloseIdleConnections()
	return b.Manager.CheckUpdate(ctx, b.Base.Version)
}

func AvailableRuntimeUpdate(ctx context.Context, dataDir string) (*engine.ReleaseInfo, error) {
	b, err := runtimeUpdater(dataDir)
	if err != nil || b == nil {
		return nil, err
	}
	defer b.Manager.HTTPClient.CloseIdleConnections()
	return b.Manager.AvailableUpdate(ctx, b.Base.Version)
}

func UpdateRuntime(ctx context.Context, dataDir string, selected *engine.ReleaseInfo, progress func(fingerprint.Progress)) (Runtime, error) {
	browser, err := runtimeUpdater(dataDir)
	if err != nil {
		return Runtime{}, err
	}
	if browser == nil {
		return Runtime{}, fmt.Errorf("请先安装 %s", KindFingerprint)
	}
	defer browser.Manager.HTTPClient.CloseIdleConnections()
	report := func(done, total int64) {
		if progress != nil {
			progress(fingerprint.Progress{Stage: fingerprint.StageBrowser, Text: "正在更新浏览器内核…", Done: done, Total: total})
		}
	}
	var active engine.InstalledRelease
	if selected == nil {
		active, err = browser.Update(ctx, report)
	} else {
		active, err = browser.Install(ctx, selected, report)
	}
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{Kind: KindFingerprint, Path: active.Executable, Version: active.Version}, nil
}
