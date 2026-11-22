package ui

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	fileren "github.com/xxww0098/tulai/internal/rename"
)

type renameItem struct {
	path  string
	draft string
}

type renameRule struct {
	Find, With    string
	Template      int
	Count, Offset int
	Text          string
}

type renameState struct {
	ready, busy bool
	done        <-chan struct{}
	mode        int
	items       []renameItem
	rules       []renameRule
	includeExt  bool
	insertKind  string
	position    string
	index       float64
	text        string
	prefix      string
	suffix      string
	numFormat   string
	numStart    float64
	numWidth    float64
	numPrefix   string
	numSuffix   string
	sortBy      string
	aiPrompt    string
	aiSettings  bool
	editVersion int
	undo        []fileren.Change
	note, err   string
}

func (s *renameState) init() {
	if s.ready {
		return
	}
	s.ready = true
	s.rules = []renameRule{{Count: 1, Offset: 1}}
	s.insertKind, s.position = "文本", "末尾"
	s.index, s.numStart = 1, 1
	s.numFormat, s.sortBy = "阿拉伯数字", "名称升序"
}

func (a *App) renameOptions() fileren.Options {
	s := &a.fileRename
	s.init()
	opts := fileren.Options{
		IncludeExtension: s.includeExt,
		InsertKind:       s.insertKind, Text: s.text, Index: int(s.index), Prefix: s.prefix, Suffix: s.suffix,
		Numbering: fileren.Numbering{Start: int64(s.numStart), Width: int(s.numWidth), Format: s.numFormat, Prefix: s.numPrefix, Suffix: s.numSuffix},
	}
	for _, rule := range s.rules {
		opts.Replacements = append(opts.Replacements, fileren.Replacement{
			Find: rule.Find, With: rule.With, Template: rule.Template,
			Count: rule.Count, Offset: rule.Offset, Text: rule.Text,
		})
	}
	switch s.position {
	case "开头":
		opts.Position = 0
	case "指定位置":
		opts.Position = 1
	default:
		opts.Position = 2
	}
	switch s.mode {
	case 1, 4:
		opts.Mode = fileren.Manual
		for _, item := range s.items {
			opts.Names = append(opts.Names, item.draft)
		}
	case 2:
		opts.Mode = fileren.Insert
	case 3:
		opts.Mode = fileren.Number
	}
	return opts
}

func (a *App) renamePlan() ([]fileren.Change, error) {
	paths := make([]string, len(a.fileRename.items))
	for i, item := range a.fileRename.items {
		paths[i] = item.path
	}
	changes, err := fileren.Preview(paths, a.renameOptions())
	if err == nil {
		err = fileren.Check(changes)
	}
	return changes, err
}

func (a *App) renameIngest(paths []string) {
	s := &a.fileRename
	s.init()
	if s.busy {
		return
	}
	known := make(map[string]bool, len(s.items))
	for _, item := range s.items {
		known[item.path] = true
	}
	added := 0
	for _, path := range paths {
		path, err := filepath.Abs(path)
		if err != nil || known[path] {
			continue
		}
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		known[path] = true
		s.items = append(s.items, renameItem{path: path, draft: filepath.Base(path)})
		added++
	}
	if added > 0 {
		s.undo = nil
		s.err = ""
		s.note = ""
		a.renameSort()
	}
}

func renameReadFolders(dirs []string) ([]string, int) {
	var files []string
	failed := 0
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				failed++
				return nil
			}
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".tulai-rename-") {
				if _, err := os.Lstat(filepath.Join(path, "恢复说明.txt")); err == nil {
					failed++
					return filepath.SkipDir
				}
			}
			if !entry.IsDir() {
				files = append(files, path)
			}
			return nil
		})
	}
	return files, failed
}

func (a *App) renameSort() {
	s := &a.fileRename
	collator := collate.New(language.Chinese, collate.Numeric, collate.IgnoreCase)
	type metadata struct {
		size              int64
		modified, created int64
	}
	infos := make(map[string]metadata, len(s.items))
	for _, item := range s.items {
		info, err := os.Lstat(item.path)
		if err != nil {
			continue
		}
		m := metadata{size: info.Size(), modified: info.ModTime().UnixNano()}
		if created, err := fileren.Created(item.path); err == nil {
			m.created = created.UnixNano()
		}
		infos[item.path] = m
	}
	desc := strings.HasSuffix(s.sortBy, "降序")
	sort.SliceStable(s.items, func(i, j int) bool {
		left, right := s.items[i], s.items[j]
		cmp := 0
		switch {
		case strings.HasPrefix(s.sortBy, "大小"):
			cmp = compareInt64(infos[left.path].size, infos[right.path].size)
		case strings.HasPrefix(s.sortBy, "修改时间"):
			cmp = compareInt64(infos[left.path].modified, infos[right.path].modified)
		case strings.HasPrefix(s.sortBy, "创建时间"):
			cmp = compareInt64(infos[left.path].created, infos[right.path].created)
		}
		if cmp == 0 {
			cmp = collator.CompareString(filepath.Base(left.path), filepath.Base(right.path))
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
}

func compareInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (a *App) renameReorder(rows []int, to int) {
	s := &a.fileRename
	if s.busy {
		return
	}
	selected := make(map[int]bool, len(rows))
	var moving []renameItem
	for _, i := range rows {
		if i >= 0 && i < len(s.items) {
			selected[i] = true
			moving = append(moving, s.items[i])
		}
	}
	var rest []renameItem
	at := 0
	for i, item := range s.items {
		if i == to {
			at = len(rest)
		}
		if !selected[i] {
			rest = append(rest, item)
		}
	}
	if to >= len(s.items) {
		at = len(rest)
	}
	s.items = append(append(append([]renameItem{}, rest[:at]...), moving...), rest[at:]...)
}

func (a *App) renamePasteAt(start int, text string) bool {
	s := &a.fileRename
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if !strings.Contains(text, "\n") {
		return false
	}
	lines := strings.Split(text, "\n")
	if start < 0 || start >= len(s.items) || len(lines) > len(s.items)-start {
		s.err = fmt.Sprintf("从当前行起只能粘贴 %d 行名称", max(0, len(s.items)-start))
		return true
	}
	for i, line := range lines {
		s.items[start+i].draft = line
	}
	s.editVersion++
	s.err = ""
	return true
}

func (a *App) renameSuggest() {
	s := &a.fileRename
	if s.busy || len(s.items) == 0 {
		return
	}
	names := make([]string, len(s.items))
	for i, item := range s.items {
		names[i] = filepath.Base(item.path)
	}
	s.busy, s.err, s.note = true, "", "正在生成名称建议…"
	prompt := s.aiPrompt
	endpoint, key, model := a.customURL, a.customKey, a.customModel
	a.emit("rename")
	go func() {
		result, err := fileren.Suggest(context.Background(), endpoint, key, model, prompt, names)
		apply := func() {
			s.busy = false
			defer a.maybeInstallPending()
			if err != nil {
				s.err, s.note = err.Error(), ""
				a.emit("rename")
				return
			}
			for i := range s.items {
				if i < len(result) {
					s.items[i].draft = result[i]
				}
			}
			s.note = "名称建议已生成，可在列表中修改。"
			a.emit("rename")
		}
		a.postUI(apply)
	}()
}

func (a *App) saveRenameModel() {
	if a.store != nil {
		a.store.Set("captcha_custom_url", a.customURL)
		a.store.Set("captcha_custom_key", a.customKey)
		a.store.Set("captcha_custom_model", a.customModel)
	}
	a.fileRename.aiSettings = false
	a.fileRename.note = "模型设置已保存。"
}

func (a *App) renameApply(changes []fileren.Change, undo bool) {
	s := &a.fileRename
	if s.busy || a.renameUpdateActive() || len(changes) == 0 {
		return
	}
	changes = append([]fileren.Change(nil), changes...)
	if !undo {
		for i := range changes {
			if changes[i].From == changes[i].To {
				continue
			}
			info, err := os.Lstat(changes[i].From)
			if err != nil {
				s.err = fmt.Sprintf("读取 %s：%v", changes[i].From, err)
				return
			}
			changes[i].Expected = info
		}
	}
	s.busy, s.err, s.note = true, "", "正在重命名…"
	a.emit("rename")
	finish := func(err error) {
		s.busy = false
		s.done = nil
		if err != nil {
			s.err, s.note = err.Error(), ""
			a.emit("rename")
			return
		}
		byPath := make(map[string]string, len(changes))
		var reverse []fileren.Change
		for _, change := range changes {
			byPath[change.From] = change.To
			if change.From != change.To {
				reverse = append(reverse, fileren.Change{From: change.To, To: change.From, Expected: change.Expected})
			}
		}
		for i := range s.items {
			if target, ok := byPath[s.items[i].path]; ok {
				s.items[i].path, s.items[i].draft = target, filepath.Base(target)
			}
		}
		if undo {
			s.undo = nil
			s.note = "已撤销上次重命名。"
		} else {
			s.undo = reverse
			s.note = fmt.Sprintf("已重命名 %d 项。", len(reverse))
		}
		a.maybeInstallPending()
		a.emit("rename")
	}
	if !a.hasWindow() {
		finish(fileren.Apply(changes))
		return
	}
	done := make(chan struct{})
	s.done = done
	go func() {
		err := fileren.Apply(changes)
		close(done)
		a.postUI(func() { finish(err) })
	}()
}

func (a *App) renameUpdateActive() bool {
	return a.updateDownloading || a.updatePending && a.updateBusy && !a.updateDeferred
}

func (a *App) waitRename() {
	if done := a.fileRename.done; done != nil {
		<-done
	}
}
