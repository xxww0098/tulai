package ui

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

const (
	dedupStrict = iota
	dedupStandard
	dedupLoose
)

var dedupLevelDistance = [...]int{3, 6, 12}

const dedupDefaultLevel = dedupStandard

const (
	dedupKeyLevel     = "dedup_level"
	dedupKeyOutDir    = "dedup_out_dir"
	dedupKeyFlatColor = "dedup_flat_color"
)

type dedupState struct {
	items       []string
	level       int
	outDir      string
	more        bool
	flatColorOK bool
	busy        bool
	scanned     int
	total       int
	report      dedup.Report
	note        string
	err         string
	folderErr   string
}

func (a *App) dedupThreshold() int {
	level := clampInt(a.dedup.level, 0, len(dedupLevelDistance)-1)
	return dedupLevelDistance[level]
}

func (a *App) dedupOptions() dedup.Options {
	return dedup.Options{MaxDistance: a.dedupThreshold(), IgnoreFlatColor: a.dedup.flatColorOK}
}

func dedupLevelHint(level int) string {
	switch clampInt(level, 0, len(dedupLevelDistance)-1) {
	case dedupStrict:
		return "只去几乎一样的图：同一张图重存、缩放、换了编码。"
	case dedupLoose:
		return "更松：轻微裁剪、换了角标的也算同一张，同款不同图也可能并到一组。"
	default:
		return "默认。同一张图重存、缩放、轻微裁剪都算重复。"
	}
}

func (a *App) loadDedupSettings(s *store.Store) {
	if s == nil {
		return
	}
	if s.Get(dedupKeyLevel) != "" {
		a.dedup.level = clampInt(s.GetInt(dedupKeyLevel), 0, len(dedupLevelDistance)-1)
	}
	a.dedup.outDir = strings.TrimSpace(s.Get(dedupKeyOutDir))
	a.dedup.flatColorOK = s.GetBool(dedupKeyFlatColor)
}

func (a *App) saveDedupSettings(s *store.Store) {
	if s == nil {
		return
	}
	s.SetInt(dedupKeyLevel, clampInt(a.dedup.level, 0, len(dedupLevelDistance)-1))
	s.Set(dedupKeyOutDir, strings.TrimSpace(a.dedup.outDir))
	s.SetInt(dedupKeyFlatColor, boolInt(a.dedup.flatColorOK))
}

func (a *App) dedupOutDir() string {
	if dir := strings.TrimSpace(a.dedup.outDir); dir != "" {
		return dir
	}
	return filepath.Join(a.resolvedOutputDir(), "去重", time.Now().Format("2006-01-02"))
}

func (a *App) dedupClear() {
	if a.dedup.busy {
		return
	}
	a.dedup.items = nil
	a.dedup.report = dedup.Report{}
	a.dedup.scanned, a.dedup.total = 0, 0
	a.dedup.note, a.dedup.err = "", ""
}

func openableImage(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	_, _, err = image.DecodeConfig(f)
	return err == nil
}

func (a *App) dedupIngest(paths []string) {
	if a.dedup.busy || len(paths) == 0 {
		return
	}
	have := make(map[string]bool, len(a.dedup.items))
	for _, path := range a.dedup.items {
		have[path] = true
	}
	added := 0
	for _, path := range expandImagePaths(paths) {
		if have[path] || !openableImage(path) {
			continue
		}
		a.dedup.items = append(a.dedup.items, path)
		have[path] = true
		added++
	}
	if added > 0 {
		a.dedup.note = fmt.Sprintf("新加 %d 张", added)
		a.dedup.err = ""
		a.dedup.report = dedup.Report{}
		a.dedup.scanned, a.dedup.total = 0, 0
		return
	}
	a.dedup.note = "没有新的图片"
}

func (a *App) dedupStart() {
	if a.dedup.busy || a.renameUpdateActive() || len(a.dedup.items) == 0 {
		return
	}
	paths := append([]string(nil), a.dedup.items...)
	out := a.dedupOutDir()
	opts := a.dedupOptions()
	a.dedup.busy = true
	a.dedup.err = ""
	a.dedup.note = ""
	a.dedup.report = dedup.Report{}
	a.dedup.scanned = 0
	a.dedup.total = len(paths)
	logging.Log.Info("开始去重：%d 张，阈值 %d，纯色底也算重复=%v，输出 %s",
		len(paths), opts.MaxDistance, opts.IgnoreFlatColor, out)
	a.emit("dedup")
	if !a.hasWindow() {
		rep, err := dedup.Run(paths, out, opts, nil)
		a.finishDedup(rep, err)
		return
	}
	go func() {
		rep, err := dedup.Run(paths, out, opts, func(done, total int) {
			a.postUI(func() {
				a.dedup.scanned = done
				a.dedup.total = total
				a.emit("dedup")
			})
		})
		a.postUI(func() {
			a.finishDedup(rep, err)
			a.emit("dedup")
		})
	}()
}

func (a *App) finishDedup(rep dedup.Report, err error) {
	a.dedup.busy = false
	defer a.maybeInstallPending()
	a.dedup.report = rep
	a.dedup.scanned = rep.Scanned
	a.dedup.total = rep.Scanned
	if err != nil {
		a.dedup.err = "去重失败：" + err.Error()
		logging.Log.Errorf("去重失败: %v", err)
		return
	}
	logging.Log.Info("去重完成：%d 组，重复 %d 张，留下 %d 张", len(rep.Groups), rep.Duplicates, rep.Unique)
}
