package browser

import (
	"context"
	"errors"
	"runtime"
	"slices"

	"github.com/xxww0098/tulai/internal/appdata"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/store"
)

const KindCamoufox = "Camoufox"
const KindChromeCDP = store.EngineChromeCDP

type Runtime struct{ Kind, Path, Version string }

func DefaultBackend(string) string { return KindCamoufox }
func CanonicalBackend(kind string) string {
	if kind == KindChromeCDP {
		return KindChromeCDP
	}
	return KindCamoufox
}
func NormalizeStoredBackend(_ string, kind string) string { return CanonicalBackend(kind) }
func ChoiceLabel(kind string) string                      { return CanonicalBackend(kind) }

// AccountEngines reports the runtimes used by this platform's enabled local
// accounts. The default is used before the first account has been created.
func AccountEngines(rows []store.Browser, platform, fallback string) []string {
	var kinds []string
	for _, b := range rows {
		if b.Platform != platform || b.ExtAccount != "" || b.Disabled {
			continue
		}
		kind := CanonicalBackend(b.Engine)
		if !slices.Contains(kinds, kind) {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return []string{CanonicalBackend(fallback)}
	}
	return kinds
}
func CamoufoxSupported(goos, goarch string) bool { return camoufox.Supported(goos, goarch) }
func RuntimeSupported(goos, goarch, kind string) bool {
	if kind == KindChromeCDP {
		return camoufox.ChromeSupported(goos, goarch)
	}
	return CamoufoxSupported(goos, goarch)
}
func ConfigDir() string {
	dir, err := appdata.Dir()
	if err != nil {
		return ""
	}
	return dir
}

func Resolve(goos, backend, dataDir string) (Runtime, error) {
	if goos != runtime.GOOS {
		return Runtime{}, errors.New("只能启动当前系统的受管浏览器")
	}
	if rt, ok := InstalledRuntime(dataDir, backend); ok {
		return rt, nil
	}
	kind := CanonicalBackend(backend)
	return Runtime{}, errors.New(kind + " 尚未准备好，请先在账号页安装 " + kind)
}
func InstalledRuntime(dataDir, kind string) (Runtime, bool) {
	if kind == KindChromeCDP {
		rt, ok := camoufox.InstalledChrome(dataDir)
		return Runtime{Kind: kind, Path: rt.Executable}, ok
	}
	return InstalledCamoufox(dataDir)
}
func Detect(dataDir string) []Runtime {
	var installed []Runtime
	for _, kind := range []string{KindCamoufox, KindChromeCDP} {
		if rt, ok := InstalledRuntime(dataDir, kind); ok {
			installed = append(installed, rt)
		}
	}
	return installed
}
func FormatDetected(list []Runtime) (string, string) {
	for _, rt := range list {
		if rt.Kind == KindCamoufox || rt.Kind == KindChromeCDP {
			return rt.Kind + " 已就绪", rt.Path
		}
	}
	return "Camoufox 尚未安装", ""
}
func InstalledCamoufox(dataDir string) (Runtime, bool) {
	rt, ok := camoufox.Installed(dataDir)
	return Runtime{Kind: KindCamoufox, Path: rt.Executable, Version: camoufox.Version}, ok
}
func EnsureCamoufox(ctx context.Context, dataDir string, progress func(camoufox.Progress)) (Runtime, error) {
	return EnsureRuntime(ctx, dataDir, KindCamoufox, progress)
}

func EnsureRuntime(ctx context.Context, dataDir, kind string, progress func(camoufox.Progress)) (Runtime, error) {
	proxy, err := ResolveCurrentProxy()
	if err != nil {
		return Runtime{}, err
	}
	if kind == KindChromeCDP {
		rt, err := camoufox.EnsureChrome(ctx, dataDir, proxy, progress)
		return Runtime{Kind: kind, Path: rt.Executable}, err
	}
	rt, err := camoufox.Ensure(ctx, dataDir, proxy, progress)
	return Runtime{Kind: KindCamoufox, Path: rt.Executable, Version: camoufox.Version}, err
}
