// 进程内对目标路径的互斥。
//
// 两个任务要写同一个文件时，后一个等到前一个写完。锁的键把指向同一位置的不同写法
// 收成一个：已存在的父目录按真实路径解析，最后一个文件名不解析，这样替换一个符号链接
// 时写的是链接本身，而不是它指向的文件。大小写不敏感的文件系统上，键按小写比较。
package download

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type pathLock struct {
	held bool
	wait []chan struct{}
	refs int
}

// PathLocker 在进程内独占一个目标路径。
type PathLocker struct {
	mu    sync.Mutex
	locks map[string]*pathLock
}

// NewPathLocker 建一个空的路径锁。
func NewPathLocker() *PathLocker { return &PathLocker{locks: map[string]*pathLock{}} }

// Lock 独占 path，返回一个可重复调用的解锁函数。等待可以被取消，避免一个任务取消后
// 还占着名额，挡住同一个文件名的下一次传输。
func (p *PathLocker) Lock(ctx context.Context, path string) (func(), error) {
	key := pathLockKey(path)
	p.mu.Lock()
	entry := p.locks[key]
	if entry == nil {
		entry = &pathLock{}
		p.locks[key] = entry
	}
	entry.refs++
	p.mu.Unlock()

	for {
		if err := ctx.Err(); err != nil {
			p.releaseRef(key)
			return nil, err
		}
		p.mu.Lock()
		if !entry.held {
			entry.held = true
			p.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					p.mu.Lock()
					entry.held = false
					wait := entry.wait
					entry.wait = nil
					p.mu.Unlock()
					for _, ch := range wait {
						close(ch)
					}
					p.releaseRef(key)
				})
			}, nil
		}
		ch := make(chan struct{})
		entry.wait = append(entry.wait, ch)
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			p.releaseRef(key)
			return nil, ctx.Err()
		case <-ch:
		}
	}
}

func (p *PathLocker) releaseRef(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry := p.locks[key]
	if entry == nil {
		return
	}
	entry.refs--
	if entry.refs <= 0 && !entry.held {
		delete(p.locks, key)
	}
}

// pathLockKey 把指向同一目标的路径写法收成一个锁键。
func pathLockKey(p string) string {
	cleaned := resolveExistingParent(filepath.Clean(p))
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	return cleaned
}

// resolveExistingParent 解析最长的已存在父目录，再把还没创建的尾部接回去。
func resolveExistingParent(p string) string {
	parent := filepath.Dir(p)
	suffix := []string{filepath.Base(p)}
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			out := resolved
			for i := len(suffix) - 1; i >= 0; i-- {
				out = filepath.Join(out, suffix[i])
			}
			return filepath.Clean(out)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return p
		}
		suffix = append(suffix, filepath.Base(parent))
		parent = next
	}
}
