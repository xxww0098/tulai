package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/nsgo/tsx"

	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/logging"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getDedupState":     getDedupStateBridge,
		"dedupAddFiles":     dedupAddFilesBridge,
		"dedupAddFolder":    dedupAddFolderBridge,
		"dedupIngestPaths":  dedupIngestPathsBridge,
		"dedupPickOutDir":   dedupPickOutDirBridge,
		"dedupSetLevel":     dedupSetLevelBridge,
		"dedupSetMore":      dedupSetMoreBridge,
		"dedupSetFlatColor": dedupSetFlatColorBridge,
		"dedupClear":        dedupClearBridge,
		"dedupStart":        dedupStartBridge,
		"dedupOpenOutDir":   dedupOpenOutDirBridge,
	})
}

type DedupFileItemDTO struct {
	Path    string
	Name    string
	FileURL string
}

type DedupSigDTO struct {
	Path    string
	Name    string
	FileURL string
	Width   int
	Height  int
	Size    string
	Meta    string
}

type DedupGroupDTO struct {
	Index    int
	Keep     DedupSigDTO
	Dupes    []DedupSigDTO
	Distance int
}

type DedupStateDTO struct {
	Items         []DedupFileItemDTO
	ItemCount     int
	Level         int
	LevelHint     string
	Threshold     int
	OutDir        string
	ResolvedOut   string
	More          bool
	FlatColorOK   bool
	Busy          bool
	Scanned       int
	Total         int
	Note          string
	Err           string
	HasReport     bool
	Unique        int
	Duplicates    int
	GroupCount    int
	SavedDir      string
	Groups        []DedupGroupDTO
}

func toDedupSigDTO(s dedup.Signature) DedupSigDTO {
	meta := humanSize(s.Bytes)
	if s.Width > 0 && s.Height > 0 {
		meta = fmt.Sprintf("%d×%d · %s", s.Width, s.Height, humanSize(s.Bytes))
	}
	return DedupSigDTO{
		Path:    s.Path,
		Name:    filepath.Base(s.Path),
		FileURL: fileURL(s.Path),
		Width:   s.Width,
		Height:  s.Height,
		Size:    humanSize(s.Bytes),
		Meta:    meta,
	}
}

func getDedupStateBridge() DedupStateDTO {
	a := getApp()
	if a == nil {
		return DedupStateDTO{}
	}
	items := make([]DedupFileItemDTO, 0, len(a.dedup.items))
	for _, p := range a.dedup.items {
		items = append(items, DedupFileItemDTO{
			Path:    p,
			Name:    filepath.Base(p),
			FileURL: fileURL(p),
		})
	}
	rep := a.dedup.report
	groups := make([]DedupGroupDTO, 0, len(rep.Groups))
	for i, g := range rep.Groups {
		if g.Keep < 0 || g.Keep >= len(rep.Signatures) {
			continue
		}
		dupes := make([]DedupSigDTO, 0, len(g.Dups))
		for _, idx := range g.Dups {
			if idx >= 0 && idx < len(rep.Signatures) {
				dupes = append(dupes, toDedupSigDTO(rep.Signatures[idx]))
			}
		}
		groups = append(groups, DedupGroupDTO{
			Index:    i + 1,
			Keep:     toDedupSigDTO(rep.Signatures[g.Keep]),
			Dupes:    dupes,
			Distance: g.Distance,
		})
	}
	return DedupStateDTO{
		Items:       items,
		ItemCount:   len(items),
		Level:       clampInt(a.dedup.level, 0, len(dedupLevelDistance)-1),
		LevelHint:   dedupLevelHint(a.dedup.level),
		Threshold:   a.dedupThreshold(),
		OutDir:      a.dedup.outDir,
		ResolvedOut: a.dedupOutDir(),
		More:        a.dedup.more,
		FlatColorOK: a.dedup.flatColorOK,
		Busy:        a.dedup.busy,
		Scanned:     a.dedup.scanned,
		Total:       a.dedup.total,
		Note:        a.dedup.note,
		Err:         a.dedup.err,
		HasReport:   rep.Scanned > 0,
		Unique:      rep.Unique,
		Duplicates:  rep.Duplicates,
		GroupCount:  len(rep.Groups),
		SavedDir:    rep.OutDir,
		Groups:      groups,
	}
}

func dedupAddFilesBridge(ctx context.Context) error {
	a := getApp()
	if a == nil || a.dedup.busy {
		return nil
	}
	paths, err := a.pickFilesAsync(ctx, "选择要去重的图片", true, []string{"png", "jpg", "jpeg", "webp", "gif"}, "图片")
	if err != nil {
		logging.Log.Errorf("选择图片失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		_ = a.onScope(ctx, func() {
			a.dedupIngest(paths)
			a.emit("dedup")
		})
	}
	return nil
}

func dedupAddFolderBridge(ctx context.Context) error {
	a := getApp()
	if a == nil || a.dedup.busy {
		return nil
	}
	paths, err := a.pickDirsAsync(ctx, "选择图片文件夹", true)
	if err != nil {
		logging.Log.Errorf("选择文件夹失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		_ = a.onScope(ctx, func() {
			a.dedupIngest(paths)
			a.emit("dedup")
		})
	}
	return nil
}

func dedupIngestPathsBridge(paths []string) {
	if a := getApp(); a != nil {
		a.dedupIngest(paths)
		a.emit("dedup")
	}
}

func dedupPickOutDirBridge(ctx context.Context) error {
	a := getApp()
	if a == nil || a.dedup.busy {
		return nil
	}
	paths, err := a.pickDirsAsync(ctx, "选择去重结果文件夹", false)
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		_ = a.onScope(ctx, func() {
			a.dedup.outDir = paths[0]
			a.dedup.folderErr = ""
			a.saveDedupSettings(a.store)
			a.emit("dedup")
		})
	}
	return nil
}

func dedupSetLevelBridge(level int) {
	if a := getApp(); a != nil && !a.dedup.busy {
		a.dedup.level = clampInt(level, 0, len(dedupLevelDistance)-1)
		a.saveDedupSettings(a.store)
		a.emit("dedup")
	}
}

func dedupSetMoreBridge(open bool) {
	if a := getApp(); a != nil {
		a.dedup.more = open
		a.emit("dedup")
	}
}

func dedupSetFlatColorBridge(on bool) {
	if a := getApp(); a != nil && !a.dedup.busy {
		a.dedup.flatColorOK = on
		a.saveDedupSettings(a.store)
		a.emit("dedup")
	}
}

func dedupClearBridge() {
	if a := getApp(); a != nil {
		a.dedupClear()
		a.emit("dedup")
	}
}

func dedupStartBridge() {
	if a := getApp(); a != nil {
		a.dedupStart()
	}
}

func dedupOpenOutDirBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	dir := strings.TrimSpace(a.dedup.report.OutDir)
	if dir == "" {
		dir = a.dedupOutDir()
	}
	return a.openPath(dir)
}
