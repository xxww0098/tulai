// 按稳定键去重。同一个键只下载一次，下载失败时把占用退回去，成功才记入。
package download

import "sync"

// Archive 记录已经下载过的稳定键。
type Archive interface {
	// Acquire 占用 key。already 为真表示这个键已经下载过或正在下载。
	// complete 在结束时调用：commit 为真才保留记录，为假则释放占用。
	Acquire(key string) (already bool, complete func(commit bool))
	// Add 直接把 key 记为已下载。
	Add(key string)
}

// MemoryArchive 是进程内的归档，程序退出后不保留。
type MemoryArchive struct {
	mu    sync.Mutex
	keys  map[string]bool
	owned map[string]bool
}

// NewMemoryArchive 建一个空的内存归档。
func NewMemoryArchive() *MemoryArchive {
	return &MemoryArchive{keys: map[string]bool{}, owned: map[string]bool{}}
}

// Acquire 占用 key。
func (a *MemoryArchive) Acquire(key string) (bool, func(bool)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.keys[key] || a.owned[key] {
		return true, func(bool) {}
	}
	a.owned[key] = true
	return false, func(commit bool) {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.owned, key)
		if commit {
			a.keys[key] = true
		}
	}
}

// Add 直接记录 key。
func (a *MemoryArchive) Add(key string) {
	a.mu.Lock()
	a.keys[key] = true
	a.mu.Unlock()
}

// Has 报告 key 是否已经下载过。测试用。
func (a *MemoryArchive) Has(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.keys[key]
}
