// Package strategy 平台策略：豆包 / 千问。对应原软件 IPlatformStrategy /
// DoubaoStrategy / QianwenStrategy / PlatformStrategyFactory。
package strategy

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
)

//go:embed js/hook.js
var hookJS string

//go:embed js/composer.js
var composerJS string

// ErrSubmissionUncertain 表示发送已尝试，重发可能重复生成，需先检查网页。
var ErrSubmissionUncertain = errors.New("发送状态未确认，请检查网页后再开始任务")

// ErrAccountQuota 表示当前登录账号的免费额度用完了。
var ErrAccountQuota = errors.New("账号额度已用尽")

// ErrAccountRate 表示当前登录账号被限流。
var ErrAccountRate = errors.New("账号请求过于频繁")

// ErrModeration 表示豆包或千问拒绝了这条内容。页面话术在 extension/refuse.js。
var ErrModeration = errors.New("审核不通过")

// Result 一张生成结果。
type Result struct {
	URL  string `json:"url"`
	Src  string `json:"src"` // net/dom/stream 来源
	TS   int64  `json:"ts"`
	Data []byte `json:"-"` // Ego 官方下载入口返回的原始文件
	// WatermarkRemoved 为真表示 Data 已是扩展下载换成的去水印 PNG。
	// 任务设置里的另存开关不再对这张图算第二遍。
	WatermarkRemoved bool `json:"-"`
}

// Strategy 平台策略接口（与原 IPlatformStrategy 对齐并工程化）。
type Strategy interface {
	Platform() string
	HomeURL() string
	// InstallHook 注入结果截获 hook 与 composer 脚本
	InstallHook(s *cdp.Session) error
	// UploadFiles 打开上传入口并把文件交给真实选择器
	UploadFiles(s *cdp.Session, files []string) error
	// SendPrompt 写入提示词并点发送
	SendPrompt(s *cdp.Session, prompt string) error
	// MarkTask 标记本任务起点（结果收集只取此后新增）
	MarkTask(s *cdp.Session) error
	// WaitForResult 等待新结果出现
	WaitForResult(s *cdp.Session, timeout time.Duration) ([]Result, error)
	// FetchResultBytes 通过页面上下文抓取结果字节（带 cookie、免水印源图）
	FetchResultBytes(s *cdp.Session, url string) ([]byte, error)
	// CleanComposer 清理输入区供下个任务复用
	CleanComposer(s *cdp.Session) error
	// PageState 页面状态探测
	PageState(s *cdp.Session) map[string]any
}

// Factory 按平台名构造策略（PlatformStrategyFactory 等价）。
func Factory(platform string) Strategy {
	switch strings.TrimSpace(platform) {
	case "豆包":
		return &Doubao{}
	case "千问":
		return &Qianwen{}
	}
	return nil
}

// ---- 共享实现 ----

type base struct{}

// InstallHook 注入 hook + composer（平台无关）。
func (base) InstallHook(s *cdp.Session) error {
	if s.IsEgo() {
		if err := captcha.CheckVerificationRequired(s); err != nil {
			return err
		}
		var old bool
		if err := s.EgoEvaluate(`!!window.__tulaiHookInstalled`, &old); err != nil {
			return err
		}
		if old {
			if err := captcha.SetManual(s); err != nil {
				return err
			}
			if err := captcha.CheckVerificationRequired(s); err != nil {
				return err
			}
			if err := s.RenewPage(); err != nil {
				return fmt.Errorf("迁移旧结果脚本页面: %w", err)
			}
		}
		var state egoState
		if err := egoEvaluate(s, "init", nil, &state); err != nil {
			return err
		}
		logEgoDiagnostic("页面图片", state.Diagnostic)
		return nil
	}
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(hookJS)))
	out, err := s.Evaluate(`JSON.stringify({installed:!!window.__tulaiHookInstalled,revision:window.__tulaiHookRevision||''})`)
	if err != nil {
		return err
	}
	var current struct {
		Installed bool   `json:"installed"`
		Revision  string `json:"revision"`
	}
	if err := cdp.ParseJSValue(out, &current); err != nil {
		return err
	}
	if current.Installed && current.Revision != revision {
		if err := s.RenewPage(); err != nil {
			return fmt.Errorf("更新页面脚本: %w", err)
		}
	}
	s.AppendNavigateScript(fmt.Sprintf(`window.__tulaiHookRevision=%q;`, revision) + hookJS)
	s.AppendNavigateScript(extbundle.RefuseJS())
	s.AppendNavigateScript(composerJS)
	return nil
}

// MarkTask 标记任务起点。
func (base) MarkTask(s *cdp.Session) error {
	if s.IsEgo() {
		var state egoState
		if err := egoEvaluate(s, "mark", nil, &state); err != nil {
			return err
		}
		logEgoDiagnostic("任务边界", state.Diagnostic)
		return nil
	}
	_, err := s.Evaluate(`window.__tulaiMarkTask && window.__tulaiMarkTask(), 1`)
	return err
}

// UploadFiles 打开真实文件选择器并确认当前输入区附件已就绪。
func (b base) UploadFiles(s *cdp.Session, files []string) error {
	if s.IsEgo() {
		return egoUpload(s, files)
	}
	s.EnsureBase()
	logDiagnostic := func() {
		out, err := s.Evaluate(`JSON.stringify((window.__tulaiUploadDiagnostic || []).concat(window.tulaiUploadAncestorDiagnostic ? tulaiUploadAncestorDiagnostic() : []))`)
		if err != nil {
			return
		}
		var lines []string
		if cdp.ParseJSValue(out, &lines) == nil {
			for _, line := range lines {
				logging.Log.Debugf("上传入口诊断 %s", line)
			}
		}
	}
	if err := s.UploadFiles(`window.tulaiOpenUpload ? tulaiOpenUpload() : Promise.reject(new Error('上传脚本未注入'))`, files); err != nil {
		logDiagnostic()
		return fmt.Errorf("上传文件选择: %w", err)
	}
	out, err := s.EvaluateAsync(fmt.Sprintf(`window.tulaiWaitForUpload ? tulaiWaitForUpload(%d) : '{"ready":false}'`, len(files)))
	if err != nil {
		logDiagnostic()
		return fmt.Errorf("等待上传: %w", err)
	}
	var upload struct {
		Ready      bool     `json:"ready"`
		Diagnostic []string `json:"diagnostic"`
	}
	if err := cdp.ParseJSValue(out, &upload); err != nil {
		return fmt.Errorf("解析上传结果: %w", err)
	}
	if !upload.Ready {
		logDiagnostic()
		for _, diagnostic := range upload.Diagnostic {
			logging.Log.Debugf("上传诊断 %s", diagnostic)
		}
		return fmt.Errorf("未确认当前输入区源图附件已就绪")
	}
	return nil
}

// pageAccountFault 读取页面提示条里的额度或限流。没有提示时返回 nil。
func pageAccountFault(s *cdp.Session) error {
	if s == nil || s.IsEgo() {
		return nil
	}
	out, err := s.Evaluate(`JSON.stringify(window.tulaiPageState ? tulaiPageState() : {})`)
	if err != nil {
		return nil
	}
	var state struct {
		Quota      bool   `json:"quota"`
		RateLimit  bool   `json:"rateLimit"`
		Moderation string `json:"moderation"`
	}
	if cdp.ParseJSValue(out, &state) != nil {
		return nil
	}
	if state.RateLimit {
		return ErrAccountRate
	}
	if state.Quota {
		return ErrAccountQuota
	}
	if msg := strings.TrimSpace(state.Moderation); msg != "" {
		return fmt.Errorf("%w: %s", ErrModeration, msg)
	}
	return nil
}

// SendPrompt 可信输入后点击一次发送，等待平台清空输入确认接收。
func (b base) SendPrompt(s *cdp.Session, prompt string) (sendErr error) {
	if s.IsEgo() {
		return egoSend(s, prompt)
	}
	s.EnsureBase()
	out, err := s.Evaluate(`window.tulaiMarkComposer ? tulaiMarkComposer() : false`)
	if err != nil {
		return fmt.Errorf("定位输入框: %w", err)
	}
	var found bool
	if err := cdp.ParseJSValue(out, &found); err != nil || !found {
		return fmt.Errorf("未找到可写入提示词的输入框")
	}
	if err := s.InsertText(`[data-tulai-composer="1"]`, prompt); err != nil {
		return fmt.Errorf("写入提示词: %w", err)
	}
	out, err = s.Evaluate(`window.tulaiReadComposer ? tulaiReadComposer(document.querySelector('[data-tulai-composer="1"]')) : null`)
	if err != nil {
		return fmt.Errorf("校验提示词: %w", err)
	}
	var actual string
	if err := cdp.ParseJSValue(out, &actual); err != nil {
		return fmt.Errorf("解析提示词内容: %w", err)
	}
	if strings.TrimSpace(strings.ReplaceAll(actual, "\r\n", "\n")) != strings.TrimSpace(strings.ReplaceAll(prompt, "\r\n", "\n")) {
		points := []rune(actual)
		logging.Log.Debugf("提示词校验失败 expectedChars=%d; composerChars=%d; first=%U; last=%U", len([]rune(prompt)), len(points), points[:min(3, len(points))], points[max(0, len(points)-3):])
		if diagnostic, err := s.Evaluate(`JSON.stringify(window.tulaiComposerDiagnostic ? tulaiComposerDiagnostic(document.querySelector('[data-tulai-composer="1"]')) : [])`); err == nil {
			var lines []string
			if cdp.ParseJSValue(diagnostic, &lines) == nil {
				for _, line := range lines {
					logging.Log.Debugf("输入区诊断 %s", line)
				}
			}
		}
		return fmt.Errorf("输入区内容与本次提示词不一致，未发送")
	}
	if err := pageAccountFault(s); err != nil {
		return err
	}
	// 点击与确认之间出错时，不能靠重发判断平台是否已接受消息。
	uncertain := true
	defer func() {
		if sendErr != nil && uncertain {
			sendErr = fmt.Errorf("%w: %w", ErrSubmissionUncertain, sendErr)
		}
	}()
	out, err = s.EvaluateAsync(`window.tulaiSendPrompt ? tulaiSendPrompt() : '{"error":"发送脚本未注入"}'`)
	if err != nil {
		return fmt.Errorf("点击发送: %w", err)
	}
	var result struct {
		Sent       bool     `json:"sent"`
		Attempted  *bool    `json:"attempted"`
		Error      string   `json:"error"`
		Diagnostic []string `json:"diagnostic"`
	}
	if err := cdp.ParseJSValue(out, &result); err != nil {
		return fmt.Errorf("解析发送结果: %w", err)
	}
	if result.Attempted != nil && !*result.Attempted {
		uncertain = false
	}
	if !result.Sent {
		for _, diagnostic := range result.Diagnostic {
			logging.Log.Debugf("发送诊断 %s", diagnostic)
		}
		if result.Error == "" {
			result.Error = "平台未接受消息"
		}
		return fmt.Errorf("%s", result.Error)
	}
	return nil
}

// WaitForResult 等本轮生成图出现在回复容器里。
// 页面流式连接经常在图已经画完后还挂着，不能拿 pending 把完成拖几分钟。
// 图标和 SVG 壳不算出图。没有真图就继续等，而不是拿空结果报成功。
func (base) WaitForResult(s *cdp.Session, timeout time.Duration) ([]Result, error) {
	if s.IsEgo() {
		return egoWaitForResult(s, timeout)
	}
	deadline := time.Now().Add(timeout)
	poll := 300 * time.Millisecond
	var stableKey string
	var stableSince time.Time
	for time.Now().Before(deadline) {
		if err := s.Ctx().Err(); err != nil {
			return nil, err
		}
		if err := pageAccountFault(s); err != nil {
			return nil, err
		}
		_, _ = s.Evaluate(`window.__tulaiDismissPopups ? String(__tulaiDismissPopups()) : '0'`)
		out, err := s.Evaluate(`JSON.stringify(window.__tulaiLiveImages ? __tulaiLiveImages() : {images:[],busy:false})`)
		if err == nil {
			var live struct {
				Busy   bool     `json:"busy"`
				Images []Result `json:"images"`
			}
			if cdp.ParseJSValue(out, &live) == nil && !live.Busy && len(live.Images) > 0 {
				key := liveKey(live.Images)
				if key != stableKey {
					stableKey = key
					stableSince = time.Now()
				}
				if time.Since(stableSince) >= 400*time.Millisecond {
					return live.Images, nil
				}
			} else if hooked, ok := hookedResults(s); ok && len(hooked) > 0 && !live.Busy {
				key := liveKey(hooked)
				if key != stableKey {
					stableKey = key
					stableSince = time.Now()
				}
				if time.Since(stableSince) >= 400*time.Millisecond {
					return hooked, nil
				}
			} else {
				stableKey = ""
				stableSince = time.Time{}
			}
		}
		select {
		case <-s.Ctx().Done():
			return nil, s.Ctx().Err()
		case <-time.After(min(poll, time.Until(deadline))):
		}
	}
	return nil, fmt.Errorf("等待生成结果超时")
}

func hookedResults(s *cdp.Session) ([]Result, bool) {
	out, err := s.Evaluate(`JSON.stringify(window.__tulaiResultsSince ? __tulaiResultsSince() : [])`)
	if err != nil {
		return nil, false
	}
	var items []Result
	if cdp.ParseJSValue(out, &items) != nil {
		return nil, false
	}
	return items, true
}

func liveKey(items []Result) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.URL != "" {
			parts = append(parts, item.URL)
		}
	}
	return strings.Join(parts, "\n")
}

// FetchResultBytes 页面内 fetch 结果 URL（带登录态），base64 回传。
func (base) FetchResultBytes(s *cdp.Session, url string) ([]byte, error) {
	if s.IsEgo() {
		return nil, fmt.Errorf("Ego 结果必须通过网页下载入口获取")
	}
	out, err := s.EvaluateAsync(fmt.Sprintf(`(async function(){
		try {
			var resp = await fetch(%q, {credentials: 'include'});
			if (!resp.ok) return JSON.stringify({error: 'HTTP ' + resp.status});
			var blob = await resp.blob();
			if (blob.size < 1024) return JSON.stringify({error: '结果过小'});
			var b64 = await new Promise(function(resolve){
				var fr = new FileReader();
				fr.onload = function(){ resolve(fr.result.split(',')[1]); };
				fr.readAsDataURL(blob);
			});
			return JSON.stringify({b64: b64});
		} catch (e) { return JSON.stringify({error: String(e)}); }
	})()`, url))
	if err != nil {
		return nil, err
	}
	var r struct {
		B64   string `json:"b64"`
		Error string `json:"error"`
	}
	if err := cdp.ParseJSValue(out, &r); err != nil || r.Error != "" {
		if r.Error == "" {
			r.Error = "返回解析失败"
		}
		if strings.HasPrefix(r.Error, "TypeError:") && strings.Contains(strings.ToLower(r.Error), "fetch") {
			return s.LoadResource(url)
		}
		return nil, fmt.Errorf("%s", r.Error)
	}
	return decodeB64(r.B64)
}

// FetchResultList 在页面里并行拉取多张结果。一张失败不影响其余。
func (base) FetchResultList(s *cdp.Session, urls []string) [][]byte {
	out := make([][]byte, len(urls))
	if s == nil || len(urls) == 0 || s.IsEgo() {
		return out
	}
	payload, err := json.Marshal(urls)
	if err != nil {
		return out
	}
	raw, err := s.EvaluateAsync(fmt.Sprintf(`(async function(){
		var urls = %s;
		var items = await Promise.all(urls.map(async function(url){
			try {
				var resp = await fetch(url, {credentials:'include'});
				if (!resp.ok) return {error:'HTTP '+resp.status};
				var blob = await resp.blob();
				if (blob.size < 1536) return {error:'结果过小'};
				var b64 = await new Promise(function(resolve){
					var fr = new FileReader();
					fr.onload = function(){ resolve(String(fr.result||'').split(',')[1]||''); };
					fr.readAsDataURL(blob);
				});
				return {b64:b64};
			} catch (e) { return {error:String(e)}; }
		}));
		return JSON.stringify(items);
	})()`, string(payload)))
	if err != nil {
		return out
	}
	var items []struct {
		B64   string `json:"b64"`
		Error string `json:"error"`
	}
	if cdp.ParseJSValue(raw, &items) != nil {
		return out
	}
	for i := range urls {
		if i >= len(items) || items[i].Error != "" || items[i].B64 == "" {
			continue
		}
		if data, err := decodeB64(items[i].B64); err == nil {
			out[i] = data
		}
	}
	return out
}

// CleanComposer 清理输入区。
func (base) CleanComposer(s *cdp.Session) error {
	if s.IsEgo() {
		return egoClean(s)
	}
	_, err := s.EvaluateAsync(`window.tulaiCleanComposer ? tulaiCleanComposer() : false`)
	return err
}

// PageState 状态探测。
func (base) PageState(s *cdp.Session) map[string]any {
	if s.IsEgo() {
		var state egoState
		if egoEvaluate(s, "state", nil, &state) != nil {
			return map[string]any{}
		}
		return map[string]any{"hasComposer": state.Composer != "", "generating": state.Busy}
	}
	out, err := s.Evaluate(`window.tulaiPageState ? JSON.stringify(tulaiPageState()) : '{}'`)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	_ = cdp.ParseJSValue(out, &m)
	return m
}
