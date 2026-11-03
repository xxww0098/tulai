// Package rename previews and applies local file renames.
package rename

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Change keeps a file in the same directory while changing its name.
type Change struct {
	From     string
	To       string
	Expected os.FileInfo
}

type stagedChange struct {
	Change
	info os.FileInfo
	temp string
}

func pathKey(path string) string {
	if runtime.GOOS == "darwin" {
		return norm.NFC.String(strings.ToLower(path))
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// Check verifies that the current sources and destinations can be renamed.
func Check(changes []Change) error {
	_, err := prepare(changes)
	return err
}

func prepare(changes []Change) ([]stagedChange, error) {
	items := make([]stagedChange, 0, len(changes))
	sources := make(map[string]int, len(changes))
	targets := make(map[string]bool, len(changes))
	for _, change := range changes {
		from, err := filepath.Abs(change.From)
		if err != nil || change.From == "" {
			return nil, fmt.Errorf("无效的源路径：%q", change.From)
		}
		to, err := filepath.Abs(change.To)
		if err != nil || change.To == "" || pathKey(filepath.Dir(from)) != pathKey(filepath.Dir(to)) {
			return nil, fmt.Errorf("目标必须在原文件夹内：%q", change.To)
		}
		if from == to {
			continue
		}
		if _, exists := sources[pathKey(from)]; exists {
			return nil, fmt.Errorf("重复的源路径：%s", from)
		}
		if targets[pathKey(to)] {
			return nil, fmt.Errorf("多个文件的目标名称相同：%s", to)
		}
		info, err := os.Lstat(from)
		if err != nil {
			return nil, fmt.Errorf("读取 %s：%w", from, err)
		}
		if change.Expected != nil && !os.SameFile(change.Expected, info) {
			return nil, fmt.Errorf("源文件已变化，无法重命名：%s", from)
		}
		sources[pathKey(from)] = len(items)
		targets[pathKey(to)] = true
		items = append(items, stagedChange{Change: Change{From: from, To: to, Expected: change.Expected}, info: info})
	}
	for _, item := range items {
		for parent := filepath.Dir(item.From); ; parent = filepath.Dir(parent) {
			if _, exists := sources[pathKey(parent)]; exists {
				return nil, fmt.Errorf("不能同时重命名文件夹及其内部项目：%s", item.From)
			}
			if parent == filepath.Dir(parent) {
				break
			}
		}
		info, err := os.Lstat(item.To)
		if err == nil {
			index, selected := sources[pathKey(item.To)]
			if !selected || !os.SameFile(info, items[index].info) {
				return nil, fmt.Errorf("目标已存在：%s", item.To)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("检查目标 %s：%w", item.To, err)
		}
	}
	return items, nil
}

// Apply never replaces an existing destination. It also supports swaps by
// moving each source into a private temporary directory before committing.
func Apply(changes []Change) error {
	return apply(changes, renameNoReplace)
}

func apply(changes []Change, move func(string, string) error) error {
	items, err := prepare(changes)
	if err != nil {
		return err
	}
	plans := make(map[string][]int)
	for i, item := range items {
		parent := filepath.Dir(item.From)
		plans[parent] = append(plans[parent], i)
	}
	dirs := make(map[string]string)
	keep := make(map[string]bool)
	defer func() {
		for _, dir := range dirs {
			if keep[dir] {
				continue
			}
			if err := os.Remove(filepath.Join(dir, "files")); err == nil || errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(filepath.Join(dir, "恢复说明.txt"))
				_ = os.Remove(dir)
			}
		}
	}()
	staged, committed := 0, 0
	rollback := func(cause error) error {
		var failures []error
		for i := committed - 1; i >= 0; i-- {
			info, err := os.Lstat(items[i].To)
			if err != nil || !os.SameFile(items[i].info, info) {
				keep[filepath.Dir(filepath.Dir(items[i].temp))] = true
				failures = append(failures, fmt.Errorf("目标已变化，无法恢复 %s：请检查 %s", items[i].From, items[i].To))
				continue
			}
			if err := move(items[i].To, items[i].temp); err != nil {
				keep[filepath.Dir(filepath.Dir(items[i].temp))] = true
				failures = append(failures, fmt.Errorf("无法将 %s 恢复为 %s：%w", items[i].To, items[i].From, err))
			}
		}
		for i := staged - 1; i >= 0; i-- {
			info, err := os.Lstat(items[i].temp)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || !os.SameFile(items[i].info, info) {
				keep[filepath.Dir(filepath.Dir(items[i].temp))] = true
				failures = append(failures, fmt.Errorf("临时文件已变化，请检查 %s", items[i].temp))
				continue
			}
			if err := move(items[i].temp, items[i].From); err != nil {
				keep[filepath.Dir(filepath.Dir(items[i].temp))] = true
				failures = append(failures, fmt.Errorf("请手动恢复 %s 到 %s：%w", items[i].temp, items[i].From, err))
			}
		}
		return errors.Join(append([]error{cause}, failures...)...)
	}
	for i := range items {
		item := &items[i]
		parent := filepath.Dir(item.From)
		dir := dirs[parent]
		if dir == "" {
			var err error
			dir, err = os.MkdirTemp(parent, ".tulai-rename-*")
			if err != nil {
				return rollback(fmt.Errorf("无法创建临时目录：%w", err))
			}
			dirs[parent] = dir
			if err := os.Mkdir(filepath.Join(dir, "files"), 0o700); err != nil {
				return rollback(fmt.Errorf("无法创建临时文件夹：%w", err))
			}
			var guide strings.Builder
			guide.WriteString("程序中断时，files 文件夹中的文件保留了原名。请将它们移回上一级目录：\n" + parent + "\n已完成的项目在上一级目录使用新名称。以下是本次计划（原名 -> 新名）：\n")
			for _, index := range plans[parent] {
				planned := items[index]
				fmt.Fprintf(&guide, "%q -> %q\n", filepath.Base(planned.From), filepath.Base(planned.To))
			}
			guide.WriteString("恢复时如果原名已存在，不要覆盖，请先检查并另存。\n")
			f, err := os.OpenFile(filepath.Join(dir, "恢复说明.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return rollback(fmt.Errorf("无法创建恢复说明：%w", err))
			}
			_, writeErr := f.WriteString(guide.String())
			if writeErr == nil {
				writeErr = f.Sync()
			}
			if closeErr := f.Close(); writeErr == nil {
				writeErr = closeErr
			}
			if writeErr != nil {
				return rollback(fmt.Errorf("无法写入恢复说明：%w", writeErr))
			}
		}
		item.temp = filepath.Join(dir, "files", filepath.Base(item.From))
		info, err := os.Lstat(item.From)
		if err != nil || !os.SameFile(item.info, info) {
			return rollback(fmt.Errorf("源文件已变化：%s", item.From))
		}
		if err := move(item.From, item.temp); err != nil {
			return rollback(fmt.Errorf("重命名 %s：%w", item.From, err))
		}
		staged++
	}
	for i := range items {
		info, err := os.Lstat(items[i].temp)
		if err != nil || !os.SameFile(items[i].info, info) {
			return rollback(fmt.Errorf("临时文件已变化：%s", items[i].temp))
		}
		if err := move(items[i].temp, items[i].To); err != nil {
			return rollback(fmt.Errorf("写入目标 %s：%w", items[i].To, err))
		}
		committed++
	}
	return nil
}
