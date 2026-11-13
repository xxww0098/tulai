package camoufox

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The Windows build of impit dynamically links the Microsoft Visual C++ runtime.
// Keep the signed redistributable beside the native module: Node loads .node
// files with a DLL search path based on the module, not on node.exe.
//
//go:embed assets/windows-amd64/vcruntime140.dll
var vcruntime140 []byte

func ensureNodeRuntimeSupport(dir string) error {
	moduleDir := filepath.Join(dir, "node_modules", "impit-win32-x64-msvc")
	target := filepath.Join(moduleDir, "vcruntime140.dll")
	matches := func() bool {
		current, err := os.ReadFile(target)
		return err == nil && bytes.Equal(current, vcruntime140)
	}
	if matches() {
		return nil
	}
	temp, err := os.CreateTemp(moduleDir, ".vcruntime140-*.tmp")
	if err != nil {
		return fmt.Errorf("准备 Camoufox Windows 运行库: %w", err)
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(vcruntime140)
	err = errors.Join(err, temp.Close())
	if err != nil {
		return fmt.Errorf("写入 Camoufox Windows 运行库: %w", err)
	}
	for attempt := 0; attempt < 8; attempt++ {
		if err = os.Rename(temp.Name(), target); err == nil || matches() {
			return nil
		}
		if attempt < 7 {
			time.Sleep(25 * time.Millisecond)
		}
	}
	return fmt.Errorf("保存 Camoufox Windows 运行库: %w", err)
}
