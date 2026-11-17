package browser

import (
	"sync"
	"time"
)

// foregroundSettle 是窗口启动后继续锁前台的时间：窗口管理器可能晚一拍才映射窗口。
var foregroundSettle = 5 * time.Second

// foregroundLock 测试里换成记录调用。
var foregroundLock = setForegroundLock

var foregroundHolds struct {
	sync.Mutex
	n int
}

// holdForeground 在显示窗口的任务浏览器启动期间锁住前台，返回的函数在稍后解锁。
// 多个账号同时启动时，最后一个结束才解锁。
func holdForeground() (release func()) {
	foregroundHolds.Lock()
	foregroundHolds.n++
	foregroundLock(true)
	foregroundHolds.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			time.AfterFunc(foregroundSettle, func() {
				foregroundHolds.Lock()
				defer foregroundHolds.Unlock()
				foregroundHolds.n--
				if foregroundHolds.n == 0 {
					foregroundLock(false)
				}
			})
		})
	}
}
