package tasks

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func washBatchUploadNames(prefix string, paths []string) []string {
	names := make([]string, len(paths))
	sum := sha256.Sum256([]byte(prefix))
	for i, path := range paths {
		if len(paths) > 1 {
			names[i] = fmt.Sprintf("tulai-%x-%02d%s", sum[:8], i+1, filepath.Ext(path))
		} else {
			names[i] = filepath.Base(path)
		}
	}
	return names
}

func washBatchCap(platform string) int {
	switch platform {
	case "豆包":
		return washChatTasks
	case "即梦":
		return jimengWashBatchMax
	default:
		return 1
	}
}

func freshWashItem(item Task) bool {
	return item.Attempts == 0 && !item.submitted && item.Error == "" && len(item.Images) == 1 && len(item.batch) == 0
}

func (e *Engine) washBatchLimit(t Task, limit int, now time.Time) int {
	if !e.opts.Wash || washBatchCap(t.Platform) <= 1 || t.maxBatch == 1 {
		return 1
	}
	row, ok := e.browserByID(t.BrowserID)
	return e.washBatchLimitRow(t, row, ok, limit, now)
}

func (e *Engine) washBatchLimitRow(t Task, row store.Browser, ok bool, limit int, now time.Time) int {
	platCap := washBatchCap(t.Platform)
	if !e.opts.Wash || platCap <= 1 || t.maxBatch == 1 {
		return 1
	}
	if t.maxBatch > 1 {
		limit = min(limit, t.maxBatch)
	}
	if cap := e.capFor(t.Platform); ok && cap > 0 {
		limit = min(limit, max(1, cap-row.UsedToday(now)))
	}
	// 即梦已同步剩余积分时，按剩余可生成张数收敛单批导入张数（例如余额仅剩 5 分且 1 分/张时最多导入 5 张），避免整批积分不足失败。
	if t.Platform == "即梦" && ok {
		if maxByCredits := e.jimengBatchMaxFor(row, ok, now); maxByCredits > 0 {
			limit = min(limit, maxByCredits)
		} else {
			return 1
		}
	}
	return min(limit, platCap)
}

// bundleWashTasks 只合并尚未发送、提示词相同的豆包洗图。
func (e *Engine) bundleWashTasks(pending []Task, index int, first Task, maxImages int, now time.Time) (Task, []int) {
	row, ok := e.browserByID(first.BrowserID)
	return e.bundleWashTasksWithRow(pending, index, first, row, ok, maxImages, now)
}

func (e *Engine) bundleWashTasksWithRow(pending []Task, index int, first Task, row store.Browser, ok bool, maxImages int, now time.Time) (Task, []int) {
	indices := []int{index}
	if !freshWashItem(first) {
		return first, indices
	}
	maxImages = e.washBatchLimitRow(first, row, ok, maxImages, now)
	if maxImages < 2 {
		return first, indices
	}
	batch := []Task{first}
	for i := index + 1; i < len(pending) && len(batch) < maxImages; i++ {
		item := pending[i]
		if item.Platform != first.Platform || item.Prompt != first.Prompt || item.maxBatch != first.maxBatch || !freshWashItem(item) {
			continue
		}
		batch = append(batch, item)
		indices = append(indices, i)
	}
	if len(batch) < 2 {
		return first, indices
	}
	first.batchSendOnly = !e.opts.WaitResult
	return washBatchOf(first, batch), indices
}

// Keep the submission state of an unsent batch when it moves to an account
// that has room for only some of its sources.
func washBatchOf(first Task, batch []Task) Task {
	first.Index = batch[0].Index
	if len(batch) == 1 {
		first.Images, first.batch = batch[0].Images, nil
		return first
	}
	first.Images = make([]string, len(batch))
	for i := range batch {
		first.Images[i] = batch[i].Images[0]
		if batch[i].splitModeration {
			first.splitModeration = true
		}
		first.sendSeq = max(first.sendSeq, batch[i].sendSeq)
	}
	first.batch = batch
	return first
}

func washPlatformLabel(platform string) string {
	if platform == "" {
		return "豆包"
	}
	return platform
}

// saveVerifiedWashBatch 按已校验的源图关联逐张保存批量结果，允许少于发送张数但禁止无关联猜测。
// 返回尚未落盘的结果切片，供上层仅在真正未匹配或保存失败时写入「待核对」。
func (e *Engine) saveVerifiedWashBatch(ctx context.Context, sess *cdp.Session, t *Task, results []strategy.Result, indices []int) ([]strategy.Result, error) {
	plat := washPlatformLabel(t.Platform)
	if len(results) < 1 || len(results) > len(t.batch) {
		return results, fmt.Errorf("本批发送 %d 张，但取得 %d 张结果，未按源图保存，请检查%s页面", len(t.batch), len(results), plat)
	}
	if len(indices) != len(results) {
		return results, fmt.Errorf("%s未提供完整的源图关联，未按源图保存", plat)
	}
	seen := make([]bool, len(t.batch))
	for _, index := range indices {
		if index < 1 || index > len(t.batch) || seen[index-1] {
			return results, fmt.Errorf("%s源图关联缺失或重复，未按源图保存", plat)
		}
		seen[index-1] = true
	}
	// 结果字节都已在内存时并行解码/缩放/落盘，保持原顺序与部分失败时的待核对语义。
	allInMemory := len(indices) > 1
	for i := range indices {
		if results[i].Data == nil {
			allInMemory = false
			break
		}
	}
	if allInMemory {
		type slotOut struct {
			saved []string
			err   error
		}
		outs := make([]slotOut, len(indices))
		jobs := make(chan int, len(indices))
		for i := range indices {
			jobs <- i
		}
		close(jobs)
		workers := min(4, max(1, runtime.NumCPU()), len(indices))
		var wg sync.WaitGroup
		strat := strategy.Factory(t.Platform)
		for range workers {
			wg.Go(func() {
				for i := range jobs {
					if ctx.Err() != nil {
						outs[i].err = ctx.Err()
						continue
					}
					item := &t.batch[indices[i]-1]
					item.BrowserID = t.BrowserID
					outs[i].saved, outs[i].err = e.saveResults(ctx, item, strat, sess, results[i:i+1])
				}
			})
		}
		wg.Wait()
		var unsaved []strategy.Result
		var firstErr error
		for i, index := range indices {
			item := &t.batch[index-1]
			saved, err := outs[i].saved, outs[i].err
			item.Results = append(item.Results, saved...)
			t.Results = append(t.Results, saved...)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				if len(saved) == 0 {
					unsaved = append(unsaved, results[i])
				}
				continue
			}
			if len(saved) != 1 {
				if firstErr == nil {
					firstErr = fmt.Errorf("第 %d 张结果未通过保存校验", index)
				}
				unsaved = append(unsaved, results[i])
			}
		}
		if firstErr != nil {
			return unsaved, firstErr
		}
	} else {
		for i, index := range indices {
			item := &t.batch[index-1]
			item.BrowserID = t.BrowserID
			saved, err := e.saveResults(ctx, item, strategy.Factory(t.Platform), sess, results[i:i+1])
			item.Results = append(item.Results, saved...)
			t.Results = append(t.Results, saved...)
			if err != nil {
				if len(saved) == 0 {
					return results[i:], err
				}
				return results[i+1:], err
			}
			if len(saved) != 1 {
				return results[i:], fmt.Errorf("第 %d 张结果未通过保存校验", index)
			}
		}
	}
	if len(results) < len(t.batch) {
		return nil, fmt.Errorf("本批发送 %d 张，已按源图关联保存 %d 张，缺少 %d 张结果，请重试未完成源图", len(t.batch), len(t.Results), len(t.batch)-len(t.Results))
	}
	return nil, nil
}

func (e *Engine) saveWashBatch(ctx context.Context, t *Task, results []strategy.Result, indices []int) error {
	_, err := e.saveVerifiedWashBatch(ctx, nil, t, results, indices)
	return err
}

func (e *Engine) saveWashResults(ctx context.Context, sess *cdp.Session, t *Task, results []strategy.Result, indices []int, resultErr error) error {
	want := max(1, len(t.batch))
	unsaved := results
	if resultErr == nil {
		if want > 1 {
			unsaved, resultErr = e.saveVerifiedWashBatch(ctx, sess, t, results, indices)
		} else if len(results) != 1 {
			resultErr = fmt.Errorf("本批发送 1 张，但取得 %d 张结果，未按源图保存，请检查%s页面", len(results), washPlatformLabel(t.Platform))
		} else {
			var saved []string
			saved, resultErr = e.saveResults(ctx, t, strategy.Factory(t.Platform), sess, results)
			t.Results = append(t.Results, saved...)
			if resultErr == nil && len(saved) != 1 {
				resultErr = errors.New("未保存到有效结果图片")
			} else if resultErr == nil && len(saved) == 1 {
				unsaved = nil
			}
		}
	}
	if resultErr != nil && ctx.Err() == nil && len(unsaved) > 0 {
		count, dir, keepErr := e.saveUnmatchedWash(ctx, sess, t, unsaved)
		if count > 0 {
			resultErr = fmt.Errorf("%w；%d 张未匹配结果保存在 %s", resultErr, count, dir)
		}
		if keepErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("保存待核对结果: %w", keepErr))
		}
	}
	return resultErr
}

// 不能确认对应关系时保留结果，但不使用任何源图文件名。
func (e *Engine) saveUnmatchedWash(ctx context.Context, sess *cdp.Session, t *Task, results []strategy.Result) (int, string, error) {
	hashes := make(map[string]string, len(t.Images))
	for _, img := range t.Images {
		if h := e.sourceHash(img); h != "" {
			hashes[img] = h
		}
	}
	backup := &Engine{opts: e.opts, srcHashes: hashes}
	backup.opts.OutputPath = filepath.Join(e.opts.OutputPath, "待核对")
	backup.opts.OutputSize = OutputSize{}
	// 待核对结果保留「批次N_未匹配」词根，不参与自动编号，方便人工核对。
	backup.opts.Numbering = Numbering{}
	dir := backup.opts.ResultDir(time.Now())
	count := 0
	var resultErr error
	for _, result := range results {
		if result.Data == nil && sess == nil {
			continue
		}
		item := &Task{Index: t.Index, Platform: t.Platform, Prompt: fmt.Sprintf("批次%d_未匹配", t.Index)}
		saved, err := backup.saveResults(ctx, item, strategy.Factory(t.Platform), sess, []strategy.Result{result})
		count += len(saved)
		resultErr = errors.Join(resultErr, err)
	}
	return count, dir, resultErr
}

// batchMembers 把一批的共同状态和各自的保存结果还原为单图任务。
func batchMembers(t Task) []Task {
	if len(t.batch) == 0 {
		return []Task{t}
	}
	out := append([]Task(nil), t.batch...)
	for i := range out {
		item := &out[i]
		item.BrowserID, item.Status, item.Phase, item.Attempts, item.Error = t.BrowserID, t.Status, t.Phase, t.Attempts, t.Error
		item.AccountName, item.StartedAt, item.Elapsed, item.Undispatched = t.AccountName, t.StartedAt, t.Elapsed, t.Undispatched
		item.slot, item.ranOn, item.soften = t.slot, t.ranOn, t.soften
		item.noUse = t.noUse
		item.Reassigned, item.ReassignWhy, item.Rewritten = t.Reassigned, t.ReassignWhy, t.Rewritten
		if len(item.Results) > 0 {
			item.Status, item.Phase, item.Error = StatusSuccess, "", ""
		} else if t.Status == StatusSuccess && !t.batchSendOnly {
			item.Status, item.Phase, item.Error = StatusFailed, "", "批量结果未保存"
		}
	}
	return out
}
