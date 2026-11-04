package tasks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/xxww0098/tulai/internal/strategy"
)

// bundleWashTasks 只合并尚未发送、提示词相同的豆包洗图。
func (e *Engine) bundleWashTasks(pending []Task, index int, first Task, maxImages int, now time.Time) (Task, []int) {
	indices := []int{index}
	if !e.opts.Wash || !e.opts.WaitResult || first.Platform != "豆包" || !e.useBoundExtension(first.Platform) || first.Attempts != 0 || first.submitted || first.Error != "" || len(first.Images) != 1 || len(first.batch) != 0 {
		return first, indices
	}
	if row, ok := e.browserByID(first.BrowserID); ok {
		maxImages = min(maxImages, max(1, e.capNow()-row.UsedToday(now)))
	}
	maxImages = min(maxImages, washChatTasks)
	batch := []Task{first}
	for i := index + 1; i < len(pending) && len(batch) < maxImages; i++ {
		item := pending[i]
		if item.Platform != first.Platform || item.Prompt != first.Prompt || item.Attempts != 0 || item.submitted || item.Error != "" || len(item.Images) != 1 || len(item.batch) != 0 {
			continue
		}
		batch = append(batch, item)
		indices = append(indices, i)
	}
	if len(batch) < 2 {
		return first, indices
	}
	first.Images = make([]string, len(batch))
	for i := range batch {
		first.Images[i] = batch[i].Images[0]
	}
	first.batch = batch
	return first, indices
}

func (e *Engine) saveWashBatch(ctx context.Context, t *Task, results []strategy.Result, indices []int) error {
	if len(results) != len(t.batch) {
		return fmt.Errorf("本批发送 %d 张，但只取得 %d 张结果，未按源图保存，请检查豆包页面", len(t.batch), len(results))
	}
	if len(indices) != len(results) {
		return errors.New("豆包未提供完整的源图关联，未按源图保存")
	}
	ordered := make([]strategy.Result, len(results))
	seen := make([]bool, len(results))
	for i, index := range indices {
		if index < 1 || index > len(results) || seen[index-1] {
			return errors.New("豆包源图关联缺失或重复，未按源图保存")
		}
		seen[index-1] = true
		ordered[index-1] = results[i]
	}
	// 调用方按这个顺序保留发生保存错误后尚未落盘的结果。
	copy(results, ordered)
	for i := range t.batch {
		item := &t.batch[i]
		item.BrowserID = t.BrowserID
		saved, err := e.saveResults(ctx, item, strategy.Factory(t.Platform), nil, results[i:i+1])
		item.Results = append(item.Results, saved...)
		t.Results = append(t.Results, saved...)
		if err != nil {
			return err
		}
		if len(saved) != 1 {
			return fmt.Errorf("第 %d 张结果未通过保存校验", i+1)
		}
	}
	return nil
}

func (e *Engine) saveExtensionWash(ctx context.Context, t *Task, results []strategy.Result, indices []int, resultErr error) error {
	want := max(1, len(t.batch))
	if resultErr == nil && len(results) != want {
		resultErr = fmt.Errorf("本批发送 %d 张，但取得 %d 张结果，未按源图保存，请检查豆包页面", want, len(results))
	}
	if resultErr == nil && want > 1 {
		resultErr = e.saveWashBatch(ctx, t, results, indices)
	} else if resultErr == nil {
		var saved []string
		saved, resultErr = e.saveResults(ctx, t, strategy.Factory(t.Platform), nil, results)
		t.Results = append(t.Results, saved...)
		if resultErr == nil && len(saved) != 1 {
			resultErr = errors.New("未保存到有效结果图片")
		}
	}
	if resultErr != nil && ctx.Err() == nil && len(results) > 0 {
		remaining := results
		if len(results) == want {
			remaining = results[min(len(t.Results), len(results)):]
		}
		count, dir, keepErr := e.saveUnmatchedWash(ctx, t, remaining)
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
func (e *Engine) saveUnmatchedWash(ctx context.Context, t *Task, results []strategy.Result) (int, string, error) {
	backup := &Engine{opts: e.opts, srcHashes: e.srcHashes}
	backup.opts.OutputPath = filepath.Join(e.opts.OutputPath, "待核对")
	backup.opts.OutputSize = OutputSize{}
	dir := backup.opts.ResultDir(time.Now())
	count := 0
	var resultErr error
	for _, result := range results {
		if result.Data == nil {
			continue
		}
		item := &Task{Index: t.Index, Platform: t.Platform, Prompt: fmt.Sprintf("批次%d_未匹配", t.Index)}
		saved, err := backup.saveResults(ctx, item, strategy.Factory(t.Platform), nil, []strategy.Result{result})
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
		item.slot, item.ranOn, item.soften = t.slot, t.ranOn, t.soften
		item.noUse = t.noUse
		item.Reassigned, item.ReassignWhy, item.Rewritten = t.Reassigned, t.ReassignWhy, t.Rewritten
		if len(item.Results) > 0 {
			item.Status, item.Error = StatusSuccess, ""
		} else if t.Status == StatusSuccess {
			item.Status, item.Error = StatusFailed, "批量结果未保存"
		}
	}
	return out
}
