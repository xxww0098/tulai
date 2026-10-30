package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

func (e *Engine) attemptAntigravity(ctx context.Context, t *Task) error {
	if t == nil {
		return fmt.Errorf("Antigravity 还没有账号，请先点「+ 添加账号」")
	}
	t.ranOn = t.BrowserID
	row, ok := e.browserByID(t.BrowserID)
	if !ok || strings.TrimSpace(row.UserDataDir) == "" {
		return fmt.Errorf("Antigravity 还没有账号，请先点「+ 添加账号」")
	}
	home := row.UserDataDir
	if !agy.LoggedIn(home) {
		agy.ForgetToken(home)
		if e.store != nil && row.ID > 0 {
			e.store.UpdateBrowser(row.ID, false, false)
		}
		return agy.ErrNeedLogin
	}
	ref := ""
	prompt := t.Prompt
	if e.opts.Wash {
		if len(t.Images) > 0 {
			ref = t.Images[0]
		}
		if strings.TrimSpace(prompt) == "" {
			prompt = "按洗图要求处理参考图，保留主体"
		}
	}
	path, err := agy.Run(ctx, agy.Request{
		Bin:    e.opts.AgyBin,
		Home:   home,
		Model:  e.opts.AgyModel,
		Prompt: agy.Instruction(prompt, ref),
		Ref:    ref,
	})
	if err != nil {
		if errors.Is(err, agy.ErrNeedLogin) && e.store != nil && row.ID > 0 {
			e.store.UpdateBrowser(row.ID, false, false)
		}
		return err
	}
	return e.saveAgyImage(ctx, t, path)
}

func (e *Engine) saveAgyImage(ctx context.Context, t *Task, path string) error {
	data, err := os.ReadFile(path)
	if err != nil || !images.AcceptGenerated(data) {
		return errors.New("没有找到生成的图片")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".jpeg" {
		ext = ".jpg"
	}
	if ext == "" {
		ext = ".png"
	}
	if e.opts.Wash && ext != ".png" {
		decoded, _, decErr := image.Decode(bytes.NewReader(data))
		if decErr != nil {
			return fmt.Errorf("洗图结果转 PNG 失败: %w", decErr)
		}
		png, encErr := images.EncodePNG(decoded)
		if encErr != nil {
			return fmt.Errorf("洗图结果转 PNG 失败: %w", encErr)
		}
		data, ext = png, ".png"
	}
	dir := e.opts.ResultDir(time.Now())
	saved, err := writeNumberedFile(ctx, dir, e.opts.resultStem(t.Prompt, resultSource(t)), ext, data)
	if err != nil {
		return err
	}
	t.Results = append(t.Results, saved)
	return nil
}

func (e *Engine) agyBudget() time.Duration {
	sec := int(agy.DefaultTimeout / time.Second)
	if e != nil && e.opts.AgyTimeoutSec > 0 {
		sec = e.opts.AgyTimeoutSec
	}
	if sec < 60 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

// holdAgyQuota 把额度用尽的任务留在队列里。还有账号就换；都用尽了就等到恢复，不标失败。
func (e *Engine) holdAgyQuota(task *Task, err error) bool {
	if task == nil || task.Platform != agy.Platform || classifyFault(err) != faultQuota {
		return false
	}
	if e.maybeRotate(task, err, time.Now()) {
		return true
	}
	task.rotated = true
	task.Status = StatusPending
	task.Error = ""
	if task.Attempts > 0 {
		task.Attempts--
	}
	e.noteAgyPause(time.Now())
	return true
}

func (e *Engine) noteAgyPause(now time.Time) {
	if e == nil {
		return
	}
	rows := e.accountsFor(agy.Platform)
	until, ok := agyFullyPaused(rows, now, e.capNow())
	if !ok {
		return
	}
	e.mu.Lock()
	if e.agyNoted {
		e.mu.Unlock()
		return
	}
	e.agyNoted = true
	e.mu.Unlock()
	logging.Log.Warning("%s", agy.PauseMessage(until, now))
}

func agyFullyPaused(rows []store.Browser, now time.Time, cap int) (time.Time, bool) {
	if len(rows) == 0 {
		return time.Time{}, false
	}
	var until time.Time
	saw := false
	for _, b := range rows {
		if !b.IsLoggedIn {
			return time.Time{}, false
		}
		if openAccount(b, now, cap) {
			return time.Time{}, false
		}
		msg := strings.ToLower(b.LastError)
		if !strings.Contains(b.LastError, "额度") && !strings.Contains(msg, "quota") {
			return time.Time{}, false
		}
		saw = true
		if b.CoolUntil <= 0 {
			continue
		}
		t := time.Unix(b.CoolUntil, 0)
		if until.IsZero() || t.Before(until) {
			until = t
		}
	}
	return until, saw
}

func (e *Engine) syncAgyAccounts() {
	if e == nil || e.store == nil {
		return
	}
	for _, b := range e.store.Browsers() {
		if b.Platform != agy.Platform || strings.TrimSpace(b.UserDataDir) == "" {
			continue
		}
		in := agy.LoggedIn(b.UserDataDir)
		if in == b.IsLoggedIn {
			continue
		}
		e.store.UpdateBrowser(b.ID, b.IsRunning, in)
		if !in {
			e.store.UpdateBrowserHealth(b.ID, store.HealthExhausted, 0, agy.ErrNeedLogin.Error())
		}
	}
}

func missingPlatform(platform string) error {
	if platform == agy.Platform {
		return fmt.Errorf("Antigravity 还没有账号，请先点「+ 添加账号」")
	}
	return fmt.Errorf("平台[%s]没有配置浏览器，请先在浏览器管理中添加", platform)
}

func needsAgyLogin(rows []store.Browser) bool {
	if len(rows) == 0 {
		return false
	}
	for _, b := range rows {
		if b.Platform != agy.Platform || b.IsLoggedIn {
			return false
		}
	}
	return true
}
