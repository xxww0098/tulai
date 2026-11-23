package tasks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/strategy"
)

func freshWashItem(item Task) bool {
	return item.Attempts == 0 && !item.submitted && item.Error == "" && len(item.Images) == 1 && len(item.batch) == 0
}

func (e *Engine) washBatchLimit(t Task, limit int, now time.Time) int {
	if !e.opts.Wash || t.Platform != "豆包" || t.maxBatch == 1 {
		return 1
	}
	row, ok := e.browserByID(t.BrowserID)
	if !e.useBoundExtension(t.Platform) && (e.store == nil || !ok || row.ExtAccount != "" || e.useExtension(t.Platform)) {
		return 1
	}
	if t.maxBatch > 1 {
		limit = min(limit, t.maxBatch)
	}
	if cap := e.capFor(t.Platform); ok && cap > 0 {
		limit = min(limit, max(1, cap-row.UsedToday(now)))
	}
	return min(limit, washChatTasks)
}

// bundleWashTasks 只合并尚未发送、提示词相同的豆包洗图。
func (e *Engine) bundleWashTasks(pending []Task, index int, first Task, maxImages int, now time.Time) (Task, []int) {
	indices := []int{index}
	if !freshWashItem(first) {
		return first, indices
	}
	maxImages = e.washBatchLimit(first, maxImages, now)
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

// saveVerifiedWashBatch 按已校验的源图关联逐张保存豆包批量结果，允许少于发送张数但禁止无关联猜测。
// 返回尚未落盘的结果切片，供上层仅在真正未匹配或保存失败时写入「待核对」。
func (e *Engine) saveVerifiedWashBatch(ctx context.Context, sess *cdp.Session, t *Task, results []strategy.Result, indices []int) ([]strategy.Result, error) {
	if len(results) < 1 || len(results) > len(t.batch) {
		return results, fmt.Errorf("本批发送 %d 张，但取得 %d 张结果，未按源图保存，请检查豆包页面", len(t.batch), len(results))
	}
	if len(indices) != len(results) {
		return results, errors.New("豆包未提供完整的源图关联，未按源图保存")
	}
	seen := make([]bool, len(t.batch))
	for _, index := range indices {
		if index < 1 || index > len(t.batch) || seen[index-1] {
			return results, errors.New("豆包源图关联缺失或重复，未按源图保存")
		}
		seen[index-1] = true
	}
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
			resultErr = fmt.Errorf("本批发送 1 张，但取得 %d 张结果，未按源图保存，请检查豆包页面", len(results))
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
	backup := &Engine{opts: e.opts, srcHashes: e.srcHashes}
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
		item.BrowserID, item.Status, item.Attempts, item.Error = t.BrowserID, t.Status, t.Attempts, t.Error
		item.AccountName, item.StartedAt, item.Elapsed, item.Undispatched = t.AccountName, t.StartedAt, t.Elapsed, t.Undispatched
		item.slot, item.ranOn, item.soften = t.slot, t.ranOn, t.soften
		item.noUse = t.noUse
		item.Reassigned, item.ReassignWhy, item.Rewritten = t.Reassigned, t.ReassignWhy, t.Rewritten
		if len(item.Results) > 0 {
			item.Status, item.Error = StatusSuccess, ""
		} else if t.Status == StatusSuccess && !t.batchSendOnly {
			item.Status, item.Error = StatusFailed, "批量结果未保存"
		}
	}
	return out
}
