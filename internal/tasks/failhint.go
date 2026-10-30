package tasks

import (
	"errors"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/strategy"
)

// DefaultDailyCap 是每个账号每天最多出图的次数。到了就换号，不需要先打开设置。
const DefaultDailyCap = 100

const mildPhrase = "画面温和、健康，适合公开展示"

// FailClass 是失败卡片上的一类原因。额度用完和限流共用一个按钮。
type FailClass int

const (
	FailUnknown FailClass = iota
	FailModeration
	FailAuth
	FailQuota
	FailCaptcha
	FailNetwork
	FailUpload
)

// FailHint 是卡片上的一句话和一个按钮。
type FailHint struct {
	Class    FailClass
	Sentence string
	Button   string
}

// DefaultRiskyWords 是审核失败时从提示词里去掉的一小列词。设置里可以改。
func DefaultRiskyWords() []string {
	return []string{"血腥", "暴力", "裸体", "色情", "性感", "裸露", "枪支", "武器", "自杀", "毒品"}
}

// RiskySetting 是写进配置库的默认词表。
func RiskySetting() string {
	return strings.Join(DefaultRiskyWords(), ",")
}

// ParseWords 把逗号或换行分隔的词表拆开。
func ParseWords(raw string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == '\n'
	}) {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ClassifyFailure 给一条失败任务一句话和一个按钮。
// 额度、限流、验证码、登录沿用 classifyFault，不另写一套探测。
// 超时、额度不足、需登录、已停止要写出类别，不能收成一句「出图失败」。
func ClassifyFailure(status Status, raw string) FailHint {
	if status == StatusNeedsUser {
		return FailHint{FailCaptcha, "需要在网页里完成验证", "重试"}
	}
	text := strings.TrimSpace(raw)
	if status == StatusSkipped || strings.Contains(text, "已停止") {
		return FailHint{FailUnknown, "已停止", "重试"}
	}
	if strings.Contains(text, "出图超时") {
		return FailHint{FailUnknown, "出图超时，请重试", "重试"}
	}
	if strings.Contains(text, "需登录") {
		return FailHint{FailAuth, "需登录，请重试", "去重登"}
	}
	if text == "" {
		return FailHint{FailUnknown, "出图失败，请重试", "重试"}
	}
	if isModeration(errors.New(text)) {
		return FailHint{FailModeration, "这条内容没通过审核", "改提示词重试"}
	}
	switch classifyFault(errors.New(text)) {
	case faultQuota:
		sentence := "账号额度已用完"
		if strings.Contains(text, "额度不足") {
			sentence = "额度不足，请重试"
		}
		return FailHint{FailQuota, sentence, "换账号重试"}
	case faultRate:
		return FailHint{FailQuota, "请求太频繁，请稍后再试", "换账号重试"}
	case faultAuth:
		return FailHint{FailAuth, "登录失效了，请重新登录", "去重登"}
	case faultCaptcha:
		return FailHint{FailCaptcha, "需要在网页里完成验证", "重试"}
	}
	if isCaptchaText(text) {
		return FailHint{FailCaptcha, "需要在网页里完成验证", "重试"}
	}
	if isUploadText(text) {
		return FailHint{FailUpload, "图片上传失败", "换图"}
	}
	if isNetworkText(text) {
		return FailHint{FailNetwork, "网络中断了，请重试", "重试"}
	}
	return FailHint{FailUnknown, "出图失败，请重试", "重试"}
}

func isModeration(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, strategy.ErrModeration) || extbridge.CodeOf(err) == extbridge.EModeration {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "审核不通过") || strings.Contains(text, "E_MODERATION")
}

func isCaptchaText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "验证") || strings.Contains(text, "滑块") || strings.Contains(text, "E_CAPTCHA") || strings.Contains(lower, "captcha")
}

func isUploadText(text string) bool {
	return strings.Contains(text, "E_UPLOAD_FAIL") || strings.Contains(text, "上传失败") || strings.Contains(text, "上传:") || strings.Contains(text, "上传：")
}

func isNetworkText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(text, "网络") || strings.Contains(text, "未连接") || strings.Contains(text, "E_NOT_CONNECTED") ||
		strings.Contains(lower, "websocket") || strings.Contains(lower, "connection") || strings.Contains(lower, "target closed")
}

// SoftenPrompt 去掉风险词，并补上一句温和说法。已经改过的句子不会叠第二遍。
func SoftenPrompt(prompt string, words []string) string {
	if len(words) == 0 {
		words = DefaultRiskyWords()
	}
	out := prompt
	for _, word := range words {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		out = strings.ReplaceAll(out, word, "")
	}
	out = strings.Join(strings.Fields(out), " ")
	if strings.Contains(out, mildPhrase) {
		return out
	}
	out = strings.Trim(out, "，,。 ")
	if out == "" {
		out = "一张温和的插画"
	}
	return out + "。" + mildPhrase
}

func (e *Engine) capNow() int {
	if e != nil && e.opts.DailyCap > 0 {
		return e.opts.DailyCap
	}
	return DefaultDailyCap
}

func (e *Engine) softenModeration(t *Task, err error) (again, stop bool) {
	if t == nil || !isModeration(err) {
		return false, false
	}
	if t.soften >= 2 {
		t.Error = err.Error()
		t.Status = StatusFailed
		return false, true
	}
	t.Prompt = SoftenPrompt(t.Prompt, e.opts.RiskyWords)
	t.Rewritten = true
	t.soften++
	t.submitted = false
	t.sentURL, t.sentWrap = "", ""
	t.Error = ""
	if t.Attempts > 0 {
		t.Attempts--
	}
	return true, false
}

// noteUse 在这一单结束时记下次数。派出时先不记，避免正在跑的账号被当成已经用满，把并发名额收掉。
func (e *Engine) noteUse(t Task) {
	if e == nil || e.store == nil || t.Attempts == 0 {
		return
	}
	id := t.ranOn
	if id == 0 {
		id = t.slot
	}
	if id <= 0 {
		return
	}
	when := time.Now()
	for i := 0; i < 1+t.soften; i++ {
		e.store.AddUse(id, when)
	}
}

// JitterWait 把发送间隔散开到 [base/2, base*3/2]。base 为 0 时不等。
// unit 取 [0,1]。
func JitterWait(base time.Duration, unit float64) time.Duration {
	if base <= 0 {
		return 0
	}
	if unit < 0 {
		unit = 0
	}
	if unit > 1 {
		unit = 1
	}
	span := float64(base) / 2
	wait := time.Duration(float64(base) + (unit*2-1)*span)
	if wait < 0 {
		return 0
	}
	return wait
}
