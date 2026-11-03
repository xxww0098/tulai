package download

import (
	"os"
	"path/filepath"
)

// replace 把 source 改名覆盖到 target，再同步所在目录。
// 调用方要保证 source 和 target 在同一个目录，并且 source 已经落盘。
func replace(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return syncDir(filepath.Dir(target))
}

// syncDir 同步目录项，让改名在崩溃后仍然可见。Windows 不能同步目录。
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// writeAtomic 先写到同目录的临时文件，落盘后再改名替换目标。
func writeAtomic(target string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	if err := replace(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
