package browser

import (
	"context"
	"errors"
	"runtime"

	"github.com/xxww0098/tulai/internal/appdata"
	"github.com/xxww0098/tulai/internal/camoufox"
)

const KindCamoufox = "Camoufox"

type Runtime struct{ Kind, Path, Version string }

func DefaultBackend(string) string                 { return KindCamoufox }
func CanonicalBackend(string) string               { return KindCamoufox }
func NormalizeStoredBackend(string, string) string { return KindCamoufox }
func ChoiceLabel(string) string                    { return KindCamoufox }
func CamoufoxSupported(goos, goarch string) bool   { return camoufox.Supported(goos, goarch) }
func ConfigDir() string {
	dir, err := appdata.Dir()
	if err != nil {
		return ""
	}
	return dir
}

func Resolve(goos, backend, dataDir string) (Runtime, error) {
	if goos != runtime.GOOS {
		return Runtime{}, errors.New("只能启动当前系统的 Camoufox")
	}
	if rt, ok := InstalledCamoufox(dataDir); ok {
		return rt, nil
	}
	return Runtime{}, errors.New("Camoufox 尚未准备好，请先在账号页安装 Camoufox")
}
func Detect(dataDir string) []Runtime {
	if rt, ok := InstalledCamoufox(dataDir); ok {
		return []Runtime{rt}
	}
	return nil
}
func FormatDetected(list []Runtime) (string, string) {
	for _, rt := range list {
		if rt.Kind == KindCamoufox {
			return "Camoufox 已就绪", rt.Path
		}
	}
	return "Camoufox 尚未安装", ""
}
func InstalledCamoufox(dataDir string) (Runtime, bool) {
	rt, ok := camoufox.Installed(dataDir)
	return Runtime{Kind: KindCamoufox, Path: rt.Executable, Version: camoufox.Version}, ok
}
func EnsureCamoufox(ctx context.Context, dataDir string, progress func(camoufox.Progress)) (Runtime, error) {
	proxy, err := ResolveCurrentProxy()
	if err != nil {
		return Runtime{}, err
	}
	rt, err := camoufox.Ensure(ctx, dataDir, proxy, progress)
	return Runtime{Kind: KindCamoufox, Path: rt.Executable, Version: camoufox.Version}, err
}
