package download

import (
	"context"
	"errors"
	"sync"
)

// BatchProgress 是批量下载的一条进展。Done 是已经处理完的条数，含失败和跳过。
type BatchProgress struct {
	Done, Total int
	Current     Progress
}

// BatchResult 是批量下载里的一项结果。
type BatchResult struct {
	URL     string
	Path    string
	Skipped bool
	Err     error
}

// RunBatch 用 workers 个协程把 items 下到 job 目录。
// 每个站点的并发上限由 Downloader 里的 HostLimiter 再收一次；
// ctx 取消后不再派新条目，正在传的会停下。
func RunBatch(ctx context.Context, dl *Downloader, job Job, items []Item, workers int, report func(BatchProgress)) []BatchResult {
	results := make([]BatchResult, len(items))
	if len(items) == 0 || dl == nil {
		return results
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(items) {
		workers = len(items)
	}

	var mu sync.Mutex
	done := 0
	finish := func(i int, res BatchResult) {
		mu.Lock()
		defer mu.Unlock()
		results[i] = res
		done++
		if report != nil {
			report(BatchProgress{Done: done, Total: len(items)})
		}
	}

	queue := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range queue {
				item := items[i]
				path := ""
				err := dl.Download(ctx, job, item, func(p Progress) {
					if p.Filename != "" && p.Filename != "[archived]" {
						path = p.Filename
					}
					if report != nil {
						mu.Lock()
						report(BatchProgress{Done: done, Total: len(items), Current: p})
						mu.Unlock()
					}
				})
				finish(i, BatchResult{
					URL:     item.URL,
					Path:    path,
					Skipped: errors.Is(err, ErrSkipped) || errors.Is(err, ErrFiltered),
					Err:     err,
				})
			}
		}()
	}

dispatch:
	for i := range items {
		select {
		case queue <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(queue)
	wg.Wait()

	// 取消后没派出去的条目也要有结果，界面才不会少算。
	for i := range results {
		if results[i].URL == "" {
			results[i] = BatchResult{URL: items[i].URL, Err: ctx.Err()}
			done++
		}
	}
	return results
}
