package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/imageedit"
	"github.com/xxww0098/tulai/internal/logging"
)

// editBatchResult 是一张图跑完后的归属。
type editBatchResult struct {
	index  int
	output string
	err    error
}

// editRunAll 开始批量处理：勾过图就只处理勾选的，否则全部。
func (a *App) editRunAll() {
	a.editStartBatch(a.editTargetIndices())
}

// editRetryFailed 只重跑上一轮失败的图。
func (a *App) editRetryFailed() {
	if idxs := a.editFailedIndices(); len(idxs) > 0 {
		a.editStartBatch(idxs)
	}
}

// editRequestCancel 让正在跑的批次在下一张开工前停下。
func (a *App) editRequestCancel() {
	st := &a.edit
	if !st.running || st.runCancel == nil {
		return
	}
	close(st.runCancel)
	st.runCancel = nil
}

// editStartBatch 处理 indices 指定的这些图。没有输出目录时先弹目录选择。
func (a *App) editStartBatch(indices []int) {
	st := &a.edit
	// 弹目录选择期间列表可能变过，先滤掉越界的下标。
	valid := make([]int, 0, len(indices))
	for _, idx := range indices {
		if idx >= 0 && idx < len(st.files) {
			valid = append(valid, idx)
		}
	}
	indices = valid
	if st.running || a.renameUpdateActive() || len(indices) == 0 {
		return
	}
	if strings.TrimSpace(st.outDir) == "" {
		a.editPickOutDir(func() { a.editStartBatch(indices) })
		return
	}
	a.editEnsureMaps()
	total := len(indices)
	paths := make([]string, total)
	opts := make([]imageedit.Options, total)
	for i, idx := range indices {
		f := st.files[idx]
		paths[i] = f.Path
		opts[i] = a.editOptionsFor(f.Path)
		f.Status = "pending"
		f.Err = ""
		f.Output = ""
	}
	outDir := strings.TrimSpace(st.outDir)
	suffix := strings.ReplaceAll(strings.ReplaceAll(st.suffix, "\\", ""), "/", "")
	suffix = strings.TrimSpace(suffix)
	cancel := make(chan struct{})
	st.runCancel = cancel
	st.running = true
	st.progressDone = 0
	st.progressTotal = total
	st.progressCurrent = ""
	st.notice = ""
	go a.editBatchRun(indices, paths, opts, outDir, suffix, cancel)
}

func (a *App) editBatchRun(indices []int, paths []string, opts []imageedit.Options, outDir, suffix string, cancel <-chan struct{}) {
	total := len(paths)
	workers := 3
	if total < workers {
		workers = total
	}
	var claimedMu sync.Mutex
	claimed := map[string]bool{}
	jobs := make(chan int)
	results := make(chan editBatchResult, total)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				select {
				case <-cancel:
					return
				default:
				}
				results <- editProcessOne(paths[i], opts[i], outDir, suffix, claimed, &claimedMu, i)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range paths {
			select {
			case <-cancel:
				// 停止后不再派新图，已经开工的照常写完。
				return
			case jobs <- i:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	okN, failN := 0, 0
	for r := range results {
		if r.err == nil {
			okN++
		} else {
			failN++
		}
		done := okN + failN
		output, errText := r.output, ""
		if r.err != nil {
			errText = r.err.Error()
		}
		index := indices[r.index]
		name := filepath.Base(paths[r.index])
		a.postUI(func() {
			st := &a.edit
			if index >= 0 && index < len(st.files) && st.files[index].Path == paths[r.index] {
				f := st.files[index]
				if errText == "" {
					f.Status = "ok"
					f.Output = output
					f.Err = ""
				} else {
					f.Status = "failed"
					f.Err = errText
					f.Output = ""
				}
			}
			st.progressDone = done
			st.progressCurrent = name
		})
	}
	stopped := false
	select {
	case <-cancel:
		stopped = true
	default:
	}
	left := total - okN - failN
	a.postUI(func() {
		st := &a.edit
		st.running = false
		defer a.maybeInstallPending()
		st.runCancel = nil
		st.progressDone = okN + failN
		st.progressCurrent = ""
		switch {
		case stopped && left > 0 && failN > 0:
			st.notice = fmt.Sprintf("已停止：完成 %d 个，失败 %d 个，还有 %d 张没处理", okN, failN, left)
		case stopped && left > 0:
			st.notice = fmt.Sprintf("已停止：完成 %d 个，还有 %d 张没处理", okN, left)
		case failN > 0:
			st.notice = fmt.Sprintf("完成 %d 个，失败 %d 个 → %s", okN, failN, outDir)
		default:
			st.notice = fmt.Sprintf("完成 %d 个 → %s", okN, outDir)
		}
	})
}

func editProcessOne(path string, opts imageedit.Options, outDir, suffix string, claimed map[string]bool, claimedMu *sync.Mutex, index int) editBatchResult {
	res := editBatchResult{index: index}
	data, err := os.ReadFile(path)
	if err != nil {
		res.err = err
		return res
	}
	out, err := imageedit.Process(context.Background(), data, opts)
	if err != nil {
		res.err = err
		return res
	}
	ext := "." + out.Format
	if out.Format == "jpeg" {
		ext = ".jpg"
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + suffix
	target := editClaimOutput(outDir, stem, ext, claimed, claimedMu)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		res.err = err
		return res
	}
	if err := os.WriteFile(target, out.Data, 0o644); err != nil {
		res.err = err
		return res
	}
	res.output = target
	return res
}

// editClaimOutput 先在内存里占位再查盘，并行时同名的两张图不会抢一个路径。
func editClaimOutput(dir, stem, ext string, claimed map[string]bool, mu *sync.Mutex) string {
	mu.Lock()
	defer mu.Unlock()
	target := filepath.Join(dir, stem+ext)
	for i := 1; i <= 1000; i++ {
		if !claimed[target] {
			claimed[target] = true
			_, statErr := os.Stat(target)
			if os.IsNotExist(statErr) {
				return target
			}
			if statErr != nil {
				// 查不了盘就当它存在，换下一个序号，绝不覆盖。
				continue
			}
		}
		target = filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
	}
	// 序号全被占或目录一直查不动：退到纳秒时间戳，至少不会覆盖。
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext))
}

// editOpenOutDir 在系统文件管理器里打开输出目录。
func (a *App) editOpenOutDir() {
	dir := strings.TrimSpace(a.edit.outDir)
	if dir == "" {
		return
	}
	if err := a.openPath(dir); err != nil {
		logging.Log.Errorf("打开输出目录失败: %v", err)
	}
}

func (a *App) editPickOutDir(then func()) {
	go func() {
		paths, err := a.pickDirsAsync(context.Background(), "选择输出目录", false)
		if err != nil {
			logging.Log.Errorf("选择输出目录失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		a.postUI(func() {
			a.edit.outDir = paths[0]
			a.saveImageEditSettings()
			a.emit("imageedit")
			if then != nil {
				then()
			}
		})
	}()
}

func (a *App) editPickFolder() {
	go func() {
		paths, err := a.pickDirsAsync(context.Background(), "选择图片文件夹", false)
		if err != nil {
			logging.Log.Errorf("选择图片文件夹失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		a.postUI(func() {
			a.editAddPaths(paths)
			a.emit("imageedit")
		})
	}()
}

func (a *App) editPickFiles() {
	go func() {
		paths, err := a.pickFilesAsync(context.Background(), "打开图片", true, []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "avif"}, "图片")
		if err != nil {
			logging.Log.Errorf("打开图片失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		a.postUI(func() {
			a.editAddPaths(paths)
			a.emit("imageedit")
		})
	}()
}
