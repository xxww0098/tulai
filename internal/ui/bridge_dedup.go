package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/logging"
)

// DedupService exposes perceptual image deduplication IPCs.
type DedupService struct {
	app *App
}

// DedupFileItemDTO represents one queued input image for deduplication.
type DedupFileItemDTO struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	FileURL string `json:"fileUrl"`
}

// DedupSigDTO describes one analyzed image in a duplicate cluster.
type DedupSigDTO struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	FileURL string `json:"fileUrl"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Size    string `json:"size"`
	Meta    string `json:"meta"`
}

// DedupGroupDTO describes a kept image alongside its near-duplicates.
type DedupGroupDTO struct {
	Index    int           `json:"index"`
	Keep     DedupSigDTO   `json:"keep"`
	Dupes    []DedupSigDTO `json:"dupes"`
	Distance int           `json:"distance"`
}

// DedupStateDTO represents the deduplication page snapshot.
type DedupStateDTO struct {
	Items       []DedupFileItemDTO `json:"items"`
	ItemCount   int                `json:"itemCount"`
	Level       int                `json:"level"`
	LevelHint   string             `json:"levelHint"`
	Threshold   int                `json:"threshold"`
	OutDir      string             `json:"outDir"`
	ResolvedOut string             `json:"resolvedOut"`
	More        bool               `json:"more"`
	FlatColorOK bool               `json:"flatColorOk"`
	Busy        bool               `json:"busy"`
	Scanned     int                `json:"scanned"`
	Total       int                `json:"total"`
	Note        string             `json:"note"`
	Err         string             `json:"err"`
	HasReport   bool               `json:"hasReport"`
	Unique      int                `json:"unique"`
	Duplicates  int                `json:"duplicates"`
	GroupCount  int                `json:"groupCount"`
	SavedDir    string             `json:"savedDir"`
	Groups      []DedupGroupDTO    `json:"groups"`
}

func (a *App) toDedupSigDTO(s dedup.Signature) DedupSigDTO {
	meta := humanSize(s.Bytes)
	if s.Width > 0 && s.Height > 0 {
		meta = fmt.Sprintf("%d×%d · %s", s.Width, s.Height, humanSize(s.Bytes))
	}
	return DedupSigDTO{
		Path:    s.Path,
		Name:    filepath.Base(s.Path),
		FileURL: a.mediaURL(s.Path),
		Width:   s.Width,
		Height:  s.Height,
		Size:    humanSize(s.Bytes),
		Meta:    meta,
	}
}

// GetDedupState returns the current deduplication input list, settings, progress, and report.
func (s *DedupService) GetDedupState(ctx context.Context) DedupStateDTO {
	if s.app == nil {
		return DedupStateDTO{Items: []DedupFileItemDTO{}, Groups: []DedupGroupDTO{}}
	}
	out, err := callValue(s.app, ctx, func() DedupStateDTO {
		a := s.app
		items := make([]DedupFileItemDTO, 0, len(a.dedup.items))
		for _, p := range a.dedup.items {
			items = append(items, DedupFileItemDTO{
				Path:    p,
				Name:    filepath.Base(p),
				FileURL: a.mediaURL(p),
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
					dupes = append(dupes, a.toDedupSigDTO(rep.Signatures[idx]))
				}
			}
			groups = append(groups, DedupGroupDTO{
				Index:    i + 1,
				Keep:     a.toDedupSigDTO(rep.Signatures[g.Keep]),
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
	})
	if err != nil {
		return DedupStateDTO{Items: []DedupFileItemDTO{}, Groups: []DedupGroupDTO{}}
	}
	return out
}

// DedupAddFiles opens a multi-file picker and adds selected images to the dedup queue.
func (s *DedupService) DedupAddFiles(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	busy, err := callValue(s.app, ctx, func() bool { return s.app.dedup.busy })
	if err != nil || busy {
		return err
	}
	paths, err := s.app.pickFilesAsync(ctx, "选择要去重的图片", true, []string{"png", "jpg", "jpeg", "webp", "gif"}, "图片")
	if err != nil {
		logging.Log.Errorf("选择图片失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		_ = s.app.call(ctx, func() {
			s.app.dedupIngest(paths)
			s.app.emit("dedup")
		})
	}
	return nil
}

// DedupAddFolder opens a folder picker and adds images inside selected folders.
func (s *DedupService) DedupAddFolder(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	busy, err := callValue(s.app, ctx, func() bool { return s.app.dedup.busy })
	if err != nil || busy {
		return err
	}
	paths, err := s.app.pickDirsAsync(ctx, "选择图片文件夹", true)
	if err != nil {
		logging.Log.Errorf("选择文件夹失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		_ = s.app.call(ctx, func() {
			s.app.dedupIngest(paths)
			s.app.emit("dedup")
		})
	}
	return nil
}

// DedupIngestPaths adds dropped or supplied file/folder paths to the dedup queue.
func (s *DedupService) DedupIngestPaths(ctx context.Context, paths []string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dedupIngest(paths)
		s.app.emit("dedup")
	})
}

// DedupPickOutDir opens a directory picker for the deduplication output folder.
func (s *DedupService) DedupPickOutDir(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	busy, err := callValue(s.app, ctx, func() bool { return s.app.dedup.busy })
	if err != nil || busy {
		return err
	}
	paths, err := s.app.pickDirsAsync(ctx, "选择去重结果文件夹", false)
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		_ = s.app.call(ctx, func() {
			s.app.dedup.outDir = paths[0]
			s.app.dedup.folderErr = ""
			s.app.saveDedupSettings(s.app.store)
			s.app.emit("dedup")
		})
	}
	return nil
}

// DedupSetLevel updates the similarity sensitivity level (0..3).
func (s *DedupService) DedupSetLevel(ctx context.Context, level int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if !s.app.dedup.busy {
			s.app.dedup.level = clampInt(level, 0, len(dedupLevelDistance)-1)
			s.app.saveDedupSettings(s.app.store)
			s.app.emit("dedup")
		}
	})
}

// DedupSetMore expands or collapses advanced dedup options.
func (s *DedupService) DedupSetMore(ctx context.Context, open bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dedup.more = open
		s.app.emit("dedup")
	})
}

// DedupSetFlatColor toggles whether near-flat-color images are treated as comparable.
func (s *DedupService) DedupSetFlatColor(ctx context.Context, on bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if !s.app.dedup.busy {
			s.app.dedup.flatColorOK = on
			s.app.saveDedupSettings(s.app.store)
			s.app.emit("dedup")
		}
	})
}

// DedupClear clears queued input images and results.
func (s *DedupService) DedupClear(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dedupClear()
		s.app.emit("dedup")
	})
}

// DedupStart starts a background perceptual deduplication scan.
func (s *DedupService) DedupStart(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dedupStart()
	})
}

// DedupOpenOutDir opens the deduplication result folder in the OS file manager.
func (s *DedupService) DedupOpenOutDir(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	dir, err := callValue(s.app, ctx, func() string {
		d := strings.TrimSpace(s.app.dedup.report.OutDir)
		if d == "" {
			d = s.app.dedupOutDir()
		}
		return d
	})
	if err != nil {
		return err
	}
	return s.app.openPath(dir)
}
