package ui

import (
	"os"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/images"
)

const (
	thumbPx   = 256
	previewPx = 640
)

// ponytail: 全局 4 路解码。一次拖进上百张把磁盘打满时，再改成按窗口排队。
var thumbGate = make(chan struct{}, 4)

// cachedThumb 只读缓存。没有就在后台解码，这一帧先空着。
func (a *App) cachedThumb(path string) *ui.Bitmap {
	if path == "" {
		return nil
	}
	for _, si := range a.sources {
		if si.Path == path && si.Thumb != nil {
			return si.Thumb
		}
	}
	a.mu.Lock()
	bmp, ok := a.thumbs[path]
	waiting := a.thumbWait[path]
	a.mu.Unlock()
	if ok {
		return bmp
	}
	if !waiting {
		a.scheduleThumb(path)
	}
	return nil
}

func (a *App) scheduleThumb(path string) {
	a.mu.Lock()
	if a.thumbs != nil {
		if _, ok := a.thumbs[path]; ok {
			a.mu.Unlock()
			return
		}
	}
	if a.thumbWait == nil {
		a.thumbWait = map[string]bool{}
	}
	if a.thumbWait[path] {
		a.mu.Unlock()
		return
	}
	a.thumbWait[path] = true
	win := a.window
	a.mu.Unlock()
	go func() {
		thumbGate <- struct{}{}
		defer func() { <-thumbGate }()
		img, err := images.CoverThumb(path, thumbPx)
		apply := func() {
			var bmp *ui.Bitmap
			if err == nil && img != nil {
				bmp = ui.NewBitmap(img)
			}
			a.mu.Lock()
			if a.thumbs == nil {
				a.thumbs = map[string]*ui.Bitmap{}
			}
			a.thumbs[path] = bmp
			delete(a.thumbWait, path)
			a.mu.Unlock()
		}
		if win != nil {
			win.Update(apply)
			return
		}
		apply()
	}()
}

// cachedPreview 按需返回保持原始宽高比的高清预览图。未解码完时先回退缩略图，避免悬停首帧闪烁。
func (a *App) cachedPreview(path string) *ui.Bitmap {
	if path == "" {
		return nil
	}
	a.mu.Lock()
	bmp, ok := a.previews[path]
	waiting := a.previewWait[path]
	win := a.window
	a.mu.Unlock()
	if ok {
		if bmp != nil {
			return bmp
		}
		return a.cachedThumb(path)
	}
	if win == nil {
		img, err := images.Thumb(path, previewPx)
		var decoded *ui.Bitmap
		if err == nil && img != nil {
			decoded = ui.NewBitmap(img)
		}
		a.mu.Lock()
		if a.previews == nil {
			a.previews = map[string]*ui.Bitmap{}
		}
		a.previews[path] = decoded
		a.mu.Unlock()
		if decoded != nil {
			return decoded
		}
		return a.cachedThumb(path)
	}
	if !waiting {
		a.schedulePreview(path)
	}
	return a.cachedThumb(path)
}

func (a *App) schedulePreview(path string) {
	a.mu.Lock()
	if a.previews != nil {
		if _, ok := a.previews[path]; ok {
			a.mu.Unlock()
			return
		}
	}
	if a.previewWait == nil {
		a.previewWait = map[string]bool{}
	}
	if a.previewWait[path] {
		a.mu.Unlock()
		return
	}
	a.previewWait[path] = true
	a.mu.Unlock()
	go func() {
		thumbGate <- struct{}{}
		defer func() { <-thumbGate }()
		img, err := images.Thumb(path, previewPx)
		apply := func() {
			var bmp *ui.Bitmap
			if err == nil && img != nil {
				bmp = ui.NewBitmap(img)
			}
			a.mu.Lock()
			if a.previews == nil {
				a.previews = map[string]*ui.Bitmap{}
			}
			a.previews[path] = bmp
			delete(a.previewWait, path)
			a.mu.Unlock()
		}
		a.postUI(apply)
	}()
}

// preloadThumbs 给截图用：等封面缩略图进缓存再画，避免第一帧还是灰块。
func (a *App) preloadThumbs(paths []string) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		img, err := images.CoverThumb(path, thumbPx)
		var bmp *ui.Bitmap
		if err == nil && img != nil {
			bmp = ui.NewBitmap(img)
		}
		a.mu.Lock()
		if a.thumbs == nil {
			a.thumbs = map[string]*ui.Bitmap{}
		}
		a.thumbs[path] = bmp
		a.mu.Unlock()
	}
	for i := range a.sources {
		if a.sources[i].Thumb != nil {
			continue
		}
		a.mu.Lock()
		a.sources[i].Thumb = a.thumbs[a.sources[i].Path]
		a.mu.Unlock()
	}
}

// ingestSources 把拖进来的文件收成源图。有窗口时解码在后台，测试没有窗口就当场收完。
func (a *App) ingestSources(paths []string) {
	if a.running || len(paths) == 0 {
		return
	}
	if a.window == nil {
		a.addLoaded(a.statePage, loadSourceFiles(paths))
		return
	}
	page := a.statePage
	go func() {
		loaded := loadSourceFiles(paths)
		a.window.Update(func() { a.addLoaded(page, loaded) })
	}()
}

func loadSourceFiles(paths []string) []sourceImage {
	var loaded []sourceImage
	for _, p := range expandImagePaths(paths) {
		data, err := os.ReadFile(p)
		if err != nil || !images.IsValidImageBytes(data) {
			continue
		}
		var bmp *ui.Bitmap
		if thumb, err := images.CoverThumb(p, thumbPx); err == nil && thumb != nil {
			bmp = ui.NewBitmap(thumb)
		}
		loaded = append(loaded, sourceImage{Path: p, Thumb: bmp, Hash: images.SHA256(data)})
	}
	return loaded
}
