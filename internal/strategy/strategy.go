// Package strategy 平台策略：豆包 / 千问。对应原软件 IPlatformStrategy /
// DoubaoStrategy / QianwenStrategy / PlatformStrategyFactory。
package strategy

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net/url"
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
	URL         string `json:"url"`
	RawURL      string `json:"rawUrl,omitempty"` // 豆包响应中的无水印原图地址
	Src         string `json:"src"`              // net/dom/stream 来源
	TS          int64  `json:"ts"`
	Width       int    `json:"w,omitempty"` // 页面上已解码的像素宽
	Height      int    `json:"h,omitempty"` // 页面上已解码的像素高
	SourceIndex int    `json:"-"`           // 豆包会话中已核对的源图序号，从 1 起。
	Data        []byte `json:"-"`           // 页面或下载通道直接返回的原始图片字节
	// WatermarkRemoved 为真表示扩展已下载无水印原图或完成局部修复。
	WatermarkRemoved bool        `json:"-"`
	Decoded          image.Image `json:"-"` // 预校验阶段已解码的像素，避免落盘阶段二次解码。
	Format           string      `json:"-"` // Decoded 对应的原始格式名（png/jpeg/webp/gif）。
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
	case "千问国际":
		return &QwenIntl{}
	}
	return nil
}

// ---- 共享实现 ----

type base struct{}

// InstallHook 注入 hook + composer（平台无关）。
func (base) InstallHook(s *cdp.Session) error {
	rawJS := extbundle.DoubaoRawJS()
	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(rawJS+hookJS)))
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
	s.AppendNavigateScript(fmt.Sprintf(`window.__tulaiHookRevision=%q;`, revision) + rawJS + hookJS)
	s.AppendNavigateScript(extbundle.RefuseJS())
	s.AppendNavigateScript(composerJS)
	return nil
}

// MarkTask 标记任务起点。
func (base) MarkTask(s *cdp.Session) error {
	_, err := s.Evaluate(`window.__tulaiMarkTask && window.__tulaiMarkTask(), 1`)
	return err
}

// UploadFiles 打开真实文件选择器并确认当前输入区附件已就绪。
func (b base) UploadFiles(s *cdp.Session, files []string) error {
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
	if s == nil {
		return nil
	}
	out, err := s.Evaluate(`JSON.stringify(window.tulaiAccountFault ? tulaiAccountFault() : (window.tulaiPageState ? tulaiPageState() : {}))`)
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
func (b base) SendPrompt(s *cdp.Session, prompt string) error {
	return b.sendPromptWithPostInsertGuard(s, prompt, nil, nil)
}

func (base) fillAndVerifyComposer(s *cdp.Session, prompt string) error {
	out, err := s.Evaluate(`window.tulaiMarkComposer ? tulaiMarkComposer() : false`)
	if err != nil {
		return fmt.Errorf("定位输入框: %w", err)
	}
	var found bool
	if err := cdp.ParseJSValue(out, &found); err != nil || !found {
		return fmt.Errorf("未找到可写入提示词的输入框")
	}
	if err := s.InsertTextElement(`window.__tulaiComposer`, prompt); err != nil {
		return fmt.Errorf("写入提示词: %w", err)
	}
	out, err = s.Evaluate(`window.tulaiReadComposer ? tulaiReadComposer(window.__tulaiComposer) : null`)
	if err != nil {
		return fmt.Errorf("校验提示词: %w", err)
	}
	var actual string
	if err := cdp.ParseJSValue(out, &actual); err != nil {
		return fmt.Errorf("解析提示词内容: %w", err)
	}
	if !composerTextMatches(prompt, actual) {
		points := []rune(actual)
		logging.Log.Debugf("提示词校验失败 expectedChars=%d; composerChars=%d; first=%U; last=%U", len([]rune(prompt)), len(points), points[:min(3, len(points))], points[max(0, len(points)-3):])
		if diagnostic, err := s.Evaluate(`JSON.stringify(window.tulaiComposerDiagnostic ? tulaiComposerDiagnostic(window.__tulaiComposer) : [])`); err == nil {
			var lines []string
			if cdp.ParseJSValue(diagnostic, &lines) == nil {
				for _, line := range lines {
					logging.Log.Debugf("输入区诊断 %s", line)
				}
			}
		}
		return fmt.Errorf("输入区内容与本次提示词不一致，未发送")
	}
	return nil
}

// sendPromptWithPostInsertGuard 在写入提示词之后、点击发送之前再次核对图像模式是否仍激活，
// 避免富文本编辑器全选替换时误删模式标签导致在普通对话中直接发图和提示词。
func (b base) sendPromptWithPostInsertGuard(s *cdp.Session, prompt string, ensure func(*cdp.Session) error, activeCheck func(*cdp.Session) (bool, error)) (sendErr error) {
	s.EnsureBase()
	if err := b.fillAndVerifyComposer(s, prompt); err != nil {
		return err
	}
	if activeCheck != nil {
		if active, err := activeCheck(s); err == nil && !active {
			if ensure != nil {
				if err := ensure(s); err != nil {
					return err
				}
				if err := b.fillAndVerifyComposer(s, prompt); err != nil {
					return err
				}
				if stillActive, checkErr := activeCheck(s); checkErr == nil && !stillActive {
					return fmt.Errorf("写入提示词后脱离图像模式，已中止发送")
				}
			} else {
				return fmt.Errorf("写入提示词后脱离图像模式，已中止发送")
			}
		}
	}
	if err := pageAccountFault(s); err != nil {
		return err
	}
	isQwen := pageIsQwen(s)
	beforeChat := ""
	if isQwen {
		beforeChat = chatKey(s)
	}
	// 点击与确认之间出错时，不能靠重发判断平台是否已接受消息。
	uncertain := true
	defer func() {
		if sendErr != nil && uncertain {
			sendErr = fmt.Errorf("%w: %w", ErrSubmissionUncertain, sendErr)
		}
	}()
	out, err := s.EvaluateAsync(`window.tulaiSendPrompt ? tulaiSendPrompt() : '{"error":"发送脚本未注入"}'`)
	if err != nil {
		return fmt.Errorf("点击发送: %w", err)
	}
	var result struct {
		Sent       bool     `json:"sent"`
		Attempted  *bool    `json:"attempted"`
		Error      string   `json:"error"`
		Diagnostic []string `json:"diagnostic"`
		Chat       string   `json:"chat"`
		Wrap       string   `json:"wrap"`
	}
	if err := cdp.ParseJSValue(out, &result); err != nil {
		return fmt.Errorf("解析发送结果: %w", err)
	}
	if result.Attempted != nil && !*result.Attempted {
		uncertain = false
	}
	if isQwen && result.Sent {
		chat := result.Chat
		if chat == "" {
			chat = beforeChat
		}
		s.BindChat(chat, result.Wrap)
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

// composerTextMatches 核对写入输入区的正文是否与期望一致。
// 富文本编辑器（ProseMirror / Slate）常将段落换行折成多空行或单换行，
// 因此在完全相等之外，也允许空行数量差异，但所有非空行内容与顺序必须逐字一致。
func composerTextMatches(expected, actual string) bool {
	norm := func(s string) string {
		return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"))
	}
	want, got := norm(expected), norm(actual)
	if want == got {
		return true
	}
	if want == "" || got == "" {
		return false
	}
	nonEmpty := func(s string) []string {
		var out []string
		for _, line := range strings.Split(s, "\n") {
			if t := strings.TrimSpace(line); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	wl, gl := nonEmpty(want), nonEmpty(got)
	if len(wl) == 0 || len(wl) != len(gl) {
		return false
	}
	for i := range wl {
		if wl[i] != gl[i] {
			return false
		}
	}
	return true
}

const imageStableFor = 3 * time.Second

// 实图像素已经稳定时，不必再空等整段 3 秒。边长与生成图下限一致。
const imagePixelStableFor = 400 * time.Millisecond

const minSettledSide = 128

// previewResultURL 是页面会先画出的缩略图地址。cgen 原图和 cthumb 缩略图都能超过 128 像素。
func previewResultURL(raw string) bool {
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "cthumb") || strings.Contains(lower, "thumbnail") || strings.Contains(lower, "downsize") {
		return true
	}
	return strings.Contains(lower, "x-oss-process=")
}

// resultSettleFor 原图已解码时只再确认一拍。缩略图地址、缺尺寸或小占位维持原来的 3 秒。
func resultSettleFor(images []Result) time.Duration {
	if len(images) == 0 {
		return imageStableFor
	}
	for _, image := range images {
		if image.Width < minSettledSide || image.Height < minSettledSide || previewResultURL(image.URL) {
			return imageStableFor
		}
	}
	return imagePixelStableFor
}

type liveShot struct {
	Busy    bool     `json:"busy"`
	Done    bool     `json:"done"`
	Expect  int      `json:"expect"`
	Bar     bool     `json:"bar"`
	Replies int      `json:"replies"`
	Reason  string   `json:"reason"`
	Here    string   `json:"here"`
	Fault   string   `json:"fault"`
	Images  []Result `json:"images"`
}

// ErrPlatformReply 是平台回了一行报错、没有图卡。等到超时也不会出图，换新对话或换号。
var ErrPlatformReply = errors.New("平台回复异常")

// replyFaultFor 让报错多停一拍再认，避免回复刚开始渲染时误判。
const replyFaultFor = 1500 * time.Millisecond

func detectKey(live liveShot) string {
	return fmt.Sprintf("%d %t %t %d %d %s", len(live.Images), live.Busy, live.Bar, live.Expect, live.Replies, live.Reason)
}

func detectLine(live liveShot, stable time.Duration) string {
	return fmt.Sprintf("检测 images=%d busy=%t bar=%t expect=%d replies=%d stable_ms=%d reason=%s",
		len(live.Images), live.Busy, live.Bar, live.Expect, live.Replies, stable.Milliseconds(), live.Reason)
}

func liveKey(images []Result) string {
	var b strings.Builder
	for _, image := range images {
		fmt.Fprintf(&b, "%s@%dx%d\n", image.URL, image.Width, image.Height)
	}
	return b.String()
}

// WaitForResult 等这一轮回复里的实图停住。
// 完成：标记之后的实图还在，生成指示没有了。地址和像素尺寸不变后再收。
// 每张都是已解码的原图时只再确认一拍。缩略图地址或还没有尺寸时维持约 3 秒，避免收下 cthumb 预览。
// 重新生成/复制和声明张数在真实豆包页面上经常没有，不能拿它们当门槛。
// 到点还有实图就交回去保存；一张都没有就明确失败。
func (base) WaitForResult(s *cdp.Session, timeout time.Duration) ([]Result, error) {
	return waitForResultFor(s, "", timeout, nil)
}

func waitForResult(s *cdp.Session, timeout time.Duration, washAttachments []string) ([]Result, error) {
	return waitForResultFor(s, "", timeout, washAttachments)
}

func waitForResultFor(s *cdp.Session, platform string, timeout time.Duration, washAttachments []string) ([]Result, error) {
	deadline := time.Now().Add(timeout)
	nextProbe := time.Now().Add(time.Minute)
	poll := 250 * time.Millisecond
	var since time.Time
	var key string
	var held []Result
	var lastDetect, lastFail string
	var offSince, faultSince time.Time
	reopened := false
	tick := 0
	for {
		if err := s.Ctx().Err(); err != nil {
			return nil, err
		}
		if blocked, vErr := captcha.Visible(s); vErr == nil && blocked {
			return nil, captcha.ErrVisible
		}
		urlID, wrapID := s.BoundChat()
		dismissPopups := tick%5 == 0
		tick++
		// 合并对话绑定、弹窗清理、账号故障探测与实时结果检测为单次 CDP 往返，降低并发场景开销。
		expr := fmt.Sprintf(`(() => {
			window.__tulaiQwenURL=%q;
			window.__tulaiQwenChat=%q;
			if (%t && window.__tulaiDismissPopups) { try { window.__tulaiDismissPopups(); } catch (e) {} }
			var fault = window.tulaiAccountFault ? tulaiAccountFault() : (window.tulaiPageState ? tulaiPageState() : {});
			var live = window.__tulaiLiveImages ? __tulaiLiveImages() : {images:[],busy:false,done:false,reason:'missing'};
			return JSON.stringify({fault: fault, live: live});
		})()`, urlID, wrapID, dismissPopups)
		out, err := s.Evaluate(expr)
		var snapshot struct {
			Fault struct {
				Quota      bool   `json:"quota"`
				RateLimit  bool   `json:"rateLimit"`
				Moderation string `json:"moderation"`
			} `json:"fault"`
			Live liveShot `json:"live"`
		}
		if err == nil && cdp.ParseJSValue(out, &snapshot) == nil {
			if snapshot.Fault.RateLimit {
				return nil, ErrAccountRate
			}
			if snapshot.Fault.Quota {
				return nil, ErrAccountQuota
			}
			if msg := strings.TrimSpace(snapshot.Fault.Moderation); msg != "" {
				return nil, fmt.Errorf("%w: %s", ErrModeration, msg)
			}
			live := snapshot.Live
			if live.Reason == "off-chat" {
				if offSince.IsZero() {
					offSince = time.Now()
				}
				away := urlID != "" && live.Here != urlID
				if urlID != "" && !reopened && (away || time.Since(offSince) >= 8*time.Second) {
					reopened = true
					targetURL := qwenChatURLFor(s, platform, urlID)
					logging.Log.Info("页面不在对话 %s，打开 %s", urlID, targetURL)
					if navErr := s.Navigate(targetURL); navErr != nil {
						logging.Log.Debugf("打开对话 %s", navErr.Error())
					}
					applyQwenBind(s)
					offSince = time.Time{}
					continue
				}
			} else {
				offSince = time.Time{}
			}
			if live.Reason == "error-reply" && len(live.Images) == 0 {
				if faultSince.IsZero() {
					faultSince = time.Now()
				}
				if time.Since(faultSince) >= replyFaultFor {
					logging.Log.Debugf("%s", detectLine(live, 0))
					return nil, fmt.Errorf("%w：%s", ErrPlatformReply, live.Fault)
				}
			} else {
				faultSince = time.Time{}
			}
			if len(live.Images) > 0 {
				held = live.Images
			}
			stable := time.Duration(0)
			urls := liveKey(live.Images)
			if live.Done && len(live.Images) > 0 {
				if urls != key {
					key = urls
					since = time.Now()
				}
				stable = time.Since(since)
				if stable >= resultSettleFor(live.Images) && len(live.Images) >= len(washAttachments) {
					if len(washAttachments) > 1 {
						if matched := resolveDoubaoWash(s, washAttachments, live.Images); len(matched) > 0 {
							return matched, nil
						}
					}
					return live.Images, nil
				}
			} else {
				since = time.Time{}
				key = ""
			}
			if sig := detectKey(live); sig != lastDetect {
				lastDetect = sig
				logging.Log.Debugf("%s", detectLine(live, stable))
			}
			if len(washAttachments) > 1 && !live.Busy && time.Now().After(nextProbe) && time.Until(deadline) > 5*time.Second {
				nextProbe = time.Now().Add(15 * time.Second)
				if matched := resolveDoubaoWash(s, washAttachments, nil); len(matched) == len(washAttachments) {
					return matched, nil
				}
			}
		} else if err != nil {
			if msg := err.Error(); msg != lastFail {
				lastFail = msg
				logging.Log.Debugf("检测失败 %s", msg)
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-s.Ctx().Done():
			return nil, s.Ctx().Err()
		case <-time.After(min(poll, time.Until(deadline))):
		}
	}
	if len(washAttachments) > 1 {
		if matched := resolveDoubaoWash(s, washAttachments, held); len(matched) > 0 {
			return matched, nil
		}
		if len(held) == 0 {
			return nil, ErrWashWaitTimeout
		}
	}
	if len(held) > 0 && timeout >= imageStableFor {
		logging.Log.Info("出图超时，先保存已有的 %d 张", len(held))
		return held, nil
	}
	return nil, fmt.Errorf("出图超时，没有可保存的图片")
}

type chatReady struct {
	Ready    bool   `json:"ready"`
	Href     string `json:"href"`
	ID       string `json:"id"`
	Node     string `json:"node"`
	Messages int    `json:"messages"`
	Chars    int    `json:"chars"`
	Cards    int    `json:"cards"`
	Why      string `json:"why"`
}

// FreshChat 每次任务都开一条新对话。复用标签页会留着上一轮的图，标记之前先换对话。
// 输入框出现而且地址还是上一条对话时不能打字：那是还没换完的旧编辑器。
func FreshChat(s *cdp.Session) error {
	if s == nil {
		return nil
	}
	// 同一标签接着出下一句。上一轮的对话 id 留着的话，检测会读旧图，重试也不再发送。
	s.ResetChat()
	defer s.StayBack()
	out, err := s.Evaluate(`window.__tulaiFreshChat ? __tulaiFreshChat() : "missing"`)
	if err != nil {
		return err
	}
	via := strings.Trim(out, `"`)
	if via == "missing" {
		return nil
	}
	originID := ""
	if via != "ready" {
		originID = strings.Trim(evalString(s, `window.__tulaiConversationKey ? __tulaiConversationKey(location.href) : ""`), `"`)
	}
	started := time.Now()
	deadline := started.Add(freshChatWait)
	stable := 0
	last := ""
	seen := chatReady{Why: "missing"}
	needStable := 3
	poll := 80 * time.Millisecond
	if via == "ready" {
		needStable = 2
		poll = 50 * time.Millisecond
	}
	for time.Now().Before(deadline) {
		if err := s.Ctx().Err(); err != nil {
			return err
		}
		if via == "spa" && time.Since(started) >= 3*time.Second && stable == 0 {
			_, _ = s.Evaluate(`location.assign('https://www.doubao.com/chat/'), 'url'`)
			via = "url"
		}
		raw, evalErr := s.Evaluate(`JSON.stringify(window.__tulaiChatReady ? __tulaiChatReady() : {ready:false,why:"missing"})`)
		var st chatReady
		if evalErr == nil && cdp.ParseJSValue(raw, &st) == nil {
			seen = st
			if st.Ready && freshLanded(via, originID, st) {
				key := st.Href + "\n" + st.Node
				if key == last {
					stable++
				} else {
					stable = 1
					last = key
				}
				if stable >= needStable {
					return nil
				}
			} else {
				stable = 0
				last = ""
			}
		}
		select {
		case <-s.Ctx().Done():
			return s.Ctx().Err()
		case <-time.After(poll):
		}
	}
	return freshChatFailure(seen)
}

// freshChatWait 盖住豆包冷启动和慢网络下新对话页的加载。
const freshChatWait = 20 * time.Second

func freshChatFailure(st chatReady) error {
	why := st.Why
	if st.Ready && why == "" {
		why = "unstable"
	}
	if why == "" {
		why = "unknown"
	}
	logging.Log.Info("新对话还没准备好 id=%s messages=%d chars=%d cards=%d why=%s href=%s", st.ID, st.Messages, st.Chars, st.Cards, why, st.Href)
	return fmt.Errorf("新对话还没准备好（%s）", why)
}

func freshLanded(via, originID string, st chatReady) bool {
	if !st.Ready {
		return false
	}
	// 千问首页壳：地址还挂着 /chat/<id>，但没有问答卡，输入框是空的。
	if st.Cards == 0 && st.Chars == 0 && st.Node != "" {
		return true
	}
	if via == "ready" || originID == "" {
		return st.ID == ""
	}
	return st.ID == "" || st.ID != originID
}

func pageIsQwen(s *cdp.Session) bool {
	host := strings.ToLower(strings.Trim(evalString(s, `window.__tulaiPageHost || location.hostname`), `"`))
	return strings.Contains(host, "qianwen") || strings.Contains(host, "tongyi") || strings.Contains(host, "qwen")
}

func chatKey(s *cdp.Session) string {
	return strings.Trim(evalString(s, `window.__tulaiConversationKey ? __tulaiConversationKey(location.href) : ""`), `"`)
}

func applyQwenBind(s *cdp.Session) {
	if s == nil {
		return
	}
	urlID, wrapID := s.BoundChat()
	_, _ = s.Evaluate(fmt.Sprintf(`window.__tulaiQwenURL=%q;window.__tulaiQwenChat=%q;`, urlID, wrapID))
}

func qwenChatURL(s *cdp.Session, id string) string {
	return qwenChatURLFor(s, "", id)
}

func qwenChatURLFor(s *cdp.Session, platform, id string) string {
	id = strings.TrimSpace(id)
	fallback := "https://www.qianwen.com/chat/" + id
	if platform == "千问国际" {
		fallback = "https://chat.qwen.ai/c/" + id
	}
	if s == nil || id == "" {
		return fallback
	}
	raw := strings.Trim(evalString(s, `location.href`), `"`)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fallback
	}
	if platform == "千问国际" || strings.Contains(strings.ToLower(u.Host), "qwen.ai") {
		if !strings.Contains(strings.ToLower(u.Host), "qwen.ai") {
			return "https://chat.qwen.ai/c/" + id
		}
		u.Path = "/c/" + id
	} else {
		u.Path = "/chat/" + id
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String()
}

// FocusQwenChat 回到发送时的那条对话。已经在这条对话上就不导航。
func FocusQwenChat(s *cdp.Session, urlID, wrapID string) {
	FocusQwenChatFor(s, "", urlID, wrapID)
}

// FocusQwenChatFor 按平台回到发送时的那条对话，避免国内站与国际站互相串跳。
func FocusQwenChatFor(s *cdp.Session, platform, urlID, wrapID string) {
	if s == nil || strings.TrimSpace(urlID) == "" {
		return
	}
	s.BindChat(urlID, wrapID)
	applyQwenBind(s)
	raw, err := s.Evaluate(`JSON.stringify({here: window.__tulaiConversationKey ? __tulaiConversationKey(location.href) : "", on: window.__tulaiQwenOnChat ? __tulaiQwenOnChat() : false})`)
	var st struct {
		Here string `json:"here"`
		On   bool   `json:"on"`
	}
	if err == nil && cdp.ParseJSValue(raw, &st) == nil && st.On && st.Here == urlID {
		return
	}
	targetURL := qwenChatURLFor(s, platform, urlID)
	logging.Log.Info("回到对话 %s", targetURL)
	if navErr := s.Navigate(targetURL); navErr != nil {
		logging.Log.Debugf("打开对话 %s", navErr.Error())
	}
	applyQwenBind(s)
}

func evalString(s *cdp.Session, expr string) string {
	out, err := s.Evaluate(expr)
	if err != nil {
		return ""
	}
	return out
}

// FetchResultBytes 页面内 fetch 结果 URL（带登录态），base64 回传。
func (base) FetchResultBytes(s *cdp.Session, url string) ([]byte, error) {
	data, pageErr := readPageImage(s, url)
	if pageErr == nil {
		return data, nil
	}
	if browserFetch(pageErr) {
		via, err := s.LoadResource(url)
		if err != nil {
			return nil, err
		}
		if len(via) < 1024 {
			return nil, fmt.Errorf("结果过小")
		}
		return via, nil
	}
	return nil, pageErr
}

func browserFetch(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "HTTP ") {
		return true
	}
	return strings.HasPrefix(msg, "TypeError:") && strings.Contains(strings.ToLower(msg), "fetch")
}

func readPageImage(s *cdp.Session, url string) ([]byte, error) {
	out, err := s.EvaluateAsync(fmt.Sprintf(`(async function(){
		async function fetchImage(u) {
			var sameOrigin = false;
			try { sameOrigin = new URL(u, location.href).origin === location.origin; } catch (e) {}
			var attempts = sameOrigin
				? [{credentials:'include', referrer:location.href, referrerPolicy:'unsafe-url', cache:'no-store'}]
				: [
					{credentials:'omit', mode:'cors', cache:'no-store'},
					{credentials:'include', referrer:location.href, referrerPolicy:'unsafe-url', cache:'no-store'}
				];
			var lastErr = null, lastResp = null;
			for (var i = 0; i < attempts.length; i++) {
				try {
					var resp = await fetch(u, attempts[i]);
					if (resp.ok) return resp;
					lastResp = resp;
				} catch (e) { lastErr = e; }
			}
			if (lastResp) return lastResp;
			throw lastErr || new Error('fetch failed');
		}
		try {
			var resp = await fetchImage(%q);
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
		return nil, fmt.Errorf("%s", r.Error)
	}
	return decodeB64(r.B64)
}

// FetchResultList 在页面里并行拉取多张结果。一张失败不影响其余。
func (base) FetchResultList(s *cdp.Session, urls []string) [][]byte {
	out := make([][]byte, len(urls))
	if s == nil || len(urls) == 0 {
		return out
	}
	payload, err := json.Marshal(urls)
	if err != nil {
		return out
	}
	raw, err := s.EvaluateAsync(fmt.Sprintf(`(async function(){
		async function fetchImage(u) {
			var sameOrigin = false;
			try { sameOrigin = new URL(u, location.href).origin === location.origin; } catch (e) {}
			var attempts = sameOrigin
				? [{credentials:'include', referrer:location.href, referrerPolicy:'unsafe-url', cache:'no-store'}]
				: [
					{credentials:'omit', mode:'cors', cache:'no-store'},
					{credentials:'include', referrer:location.href, referrerPolicy:'unsafe-url', cache:'no-store'}
				];
			var lastErr = null, lastResp = null;
			for (var i = 0; i < attempts.length; i++) {
				try {
					var resp = await fetch(u, attempts[i]);
					if (resp.ok) return resp;
					lastResp = resp;
				} catch (e) { lastErr = e; }
			}
			if (lastResp) return lastResp;
			throw lastErr || new Error('fetch failed');
		}
		var urls = %s;
		var items = await Promise.all(urls.map(async function(url){
			try {
				var resp = await fetchImage(url);
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
		if i < len(items) && items[i].Error == "" && items[i].B64 != "" {
			if data, err := decodeB64(items[i].B64); err == nil {
				out[i] = data
				continue
			}
		}
		if data, err := (base{}).FetchResultBytes(s, urls[i]); err == nil {
			out[i] = data
		}
	}
	return out
}

// CleanComposer 清理输入区。
func (base) CleanComposer(s *cdp.Session) error {
	if s != nil {
		s.EnsureBase()
	}
	_, err := s.EvaluateAsync(`window.tulaiCleanComposer ? tulaiCleanComposer() : false`)
	return err
}

// PageState 状态探测。
func (base) PageState(s *cdp.Session) map[string]any {
	out, err := s.Evaluate(`window.tulaiPageState ? JSON.stringify(tulaiPageState()) : '{}'`)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	_ = cdp.ParseJSValue(out, &m)
	return m
}
