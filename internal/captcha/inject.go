package captcha

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"xbs/internal/cdp"
	"xbs/internal/logging"
)

//go:embed js/inject.js
var injectJS string

var injectRevision = fmt.Sprintf("%x", sha256.Sum256([]byte(injectJS)))

// InjectJS 返回验证码注入脚本。
func InjectJS() string {
	return strings.Replace(injectJS, "__XBS_CAPTCHA_REVISION__", injectRevision, 1)
}

func configuredInjectJS(enabled bool) string {
	return fmt.Sprintf("window.__xbsCaptchaEnabled = %t;\nwindow.__xbsCaptcha && window.__xbsCaptcha.setManual(false);\n", enabled) + InjectJS()
}

type egoPagePreparer interface {
	Ctx() context.Context
	IsEgo() bool
	EgoEvaluate(string, any) error
	RenewPage() error
}

// PreparePage 在安装其他脚本前迁移仍运行旧验证码监听的 Ego 文档。
func PreparePage(s egoPagePreparer) error {
	if !s.IsEgo() {
		return nil
	}
	var current struct {
		Installed bool   `json:"installed"`
		Revision  string `json:"revision"`
		Blocked   bool   `json:"blocked"`
		AutoSolve bool   `json:"autoSolve"`
	}
	if err := s.Ctx().Err(); err != nil {
		return err
	}
	err := s.EgoEvaluate(`({installed:!!window.__xbsCaptchaInstalled,revision:window.__xbsCaptchaRevision||'',blocked:`+verificationExpression+`,autoSolve:!!(window.__xbsCaptcha && window.__xbsCaptcha.status && window.__xbsCaptcha.status().autoSolve)})`, &current)
	if ctxErr := s.Ctx().Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("检查验证码监听版本: %w", err)
	}
	if current.Blocked && (!current.AutoSolve || current.Revision != injectRevision) {
		return errors.Join(ErrVerificationRequired, SetManual(s))
	}
	if current.Installed && current.Revision != injectRevision {
		if err := s.RenewPage(); err != nil {
			return fmt.Errorf("更新验证码监听页面: %w", err)
		}
	}
	return nil
}

// Attach 把验证码求解注入并挂到会话：
//   - 注入脚本（新文档自动执行）
//   - 桥命令：captchaSolve（求解）、captchaShot（区域截图）、
//     captchaBlocking（阻塞上报）、captchaLog（日志）、loginDetected（登录掉线）
func Attach(s *cdp.Session, solver Solver, enabled bool, onLoginLost func()) {
	s.AppendNavigateScript(configuredInjectJS(enabled))
	s.Handle("captchaSolve", func(data string) string {
		if !enabled {
			return mustJSON(Answer{Success: false, Error: "验证码自动求解未开启"})
		}
		var req Request
		if err := json.Unmarshal([]byte(data), &req); err != nil {
			return mustJSON(Answer{Success: false, Error: "请求解析失败: " + err.Error()})
		}
		ans := solver.Solve(s.Ctx(), req)
		if !ans.Success {
			logging.Log.Warning("验证码求解失败(%s): %s", solver.Name(), ans.Error)
		} else {
			logging.Log.Success("验证码求解命中: %v", ans.Indices)
		}
		return mustJSON(ans)
	})
	s.Handle("captchaShot", func(data string) string {
		var rect struct {
			X, Y, W, H float64
		}
		if err := json.Unmarshal([]byte(data), &rect); err != nil || rect.W <= 0 || rect.H <= 0 {
			return mustJSON(map[string]any{"success": false, "error": "参数错误"})
		}
		png, err := s.CaptureScreenshotRect(rect.X, rect.Y, rect.W, rect.H)
		if err != nil {
			return mustJSON(map[string]any{"success": false, "error": err.Error()})
		}
		return mustJSON(map[string]any{"success": true, "base64": b64encode(png)})
	})
	s.Handle("captchaBlocking", blockingHandler())
	s.Handle("captchaLog", func(data string) string {
		logging.Log.Debugf("[页面] %s", data)
		return "ok"
	})
	s.Handle("loginDetected", func(string) string {
		logging.Log.Warning("检测到登录掉线（自动关闭失败），请手动登录")
		if onLoginLost != nil {
			go onLoginLost()
		}
		return "ok"
	})
}

func blockingHandler() func(string) string {
	var mu sync.Mutex
	var blocked bool
	var reason string
	return func(data string) string {
		var st struct {
			Blocked bool   `json:"blocked"`
			Reason  string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(data), &st); err == nil {
			mu.Lock()
			changed := blocked != st.Blocked || reason != st.Reason
			blocked, reason = st.Blocked, st.Reason
			mu.Unlock()
			if st.Blocked && changed {
				logging.Log.Info("页面阻塞中: %s", st.Reason)
			}
		}
		return "ok"
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"success":false}`
	}
	return string(b)
}

func b64encode(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
