package tasks

import (
	"fmt"
	"math/bits"

	"github.com/xxww0098/tulai/internal/dedup"
)

func fissionTooSimilar(a, b uint64) bool {
	// 63 位中至少 26 位不同，才不超过 60%。
	return bits.OnesCount64(a^b) < 26
}

// ReviewFissionResults 按 63 位 pHash 两两检查成功结果。fresh 为本轮
// 重试的任务；先保留此前通过检查的结果，再检查新结果。
// pHash 只衡量低频视觉结构，并不代表语义相似度。
func ReviewFissionResults(items, fresh []Task) []Task {
	out := append([]Task(nil), items...)
	newPrompts := make(map[string]bool, len(fresh))
	for _, task := range fresh {
		newPrompts[task.Platform+"\x00"+task.Prompt] = true
	}
	var order []int
	if len(fresh) > 0 {
		for i, task := range out {
			if !newPrompts[task.Platform+"\x00"+task.Prompt] {
				order = append(order, i)
			}
		}
	}
	for i, task := range out {
		if len(fresh) == 0 || newPrompts[task.Platform+"\x00"+task.Prompt] {
			order = append(order, i)
		}
	}
	type checked struct {
		index int
		sig   dedup.Signature
	}
	var accepted []checked
	for _, i := range order {
		task := &out[i]
		if task.Status != StatusSuccess {
			continue
		}
		if len(task.Results) != 1 {
			task.Status = StatusNeedsReview
			task.Error = "裂变相似度检查失败：每个方向需要一张结果"
			continue
		}
		sig, err := dedup.Hash(task.Results[0])
		if err != nil {
			task.Status = StatusNeedsReview
			task.Error = "裂变相似度检查失败：无法读取结果图片"
			continue
		}
		for _, prior := range accepted {
			distance := bits.OnesCount64(sig.Hash ^ prior.sig.Hash)
			if fissionTooSimilar(sig.Hash, prior.sig.Hash) {
				task.Status = StatusNeedsReview
				task.Error = fmt.Sprintf("裂变相似度 %.1f%%，与第 %d 张超过 60%%（pHash）", 100*float64(63-distance)/63, out[prior.index].Index)
				break
			}
		}
		if task.Status == StatusSuccess {
			accepted = append(accepted, checked{index: i, sig: sig})
		}
	}
	return out
}
