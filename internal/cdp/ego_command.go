package cdp

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func egoImportEntry(path string) string {
	path = filepath.ToSlash(path)
	file := &url.URL{Scheme: "file", Path: path}
	if len(path) > 1 && path[1] == ':' {
		file.Path = "/" + path
	} else if strings.HasPrefix(path, "//") {
		file.Host, file.Path, _ = strings.Cut(path[2:], "/")
		file.Path = "/" + file.Path
	}
	// Base64 keeps Unicode paths and URL % escapes out of cmd.exe's expansion rules.
	encoded := base64.StdEncoding.EncodeToString([]byte(file.String()))
	return "await import(Buffer.from('" + encoded + "','base64').toString())"
}

func egoCommand(ctx context.Context, path, source string) (*exec.Cmd, func(), error) {
	dir, err := os.MkdirTemp("", "tulai-ego-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	script, err := filepath.Abs(filepath.Join(dir, "bridge.mjs"))
	if err == nil {
		err = os.WriteFile(script, []byte(source), 0o600)
	}
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	cmd, err := egoPlatformCommand(ctx, path, egoImportEntry(script))
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return cmd, cleanup, nil
}

func egoBatchCommandLine(entry string) string {
	return `cmd.exe /d /v:off /s /c ""%TULAI_EGO_LAUNCHER%" nodejs -e "` + entry + `""`
}
