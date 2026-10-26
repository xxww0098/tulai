// Package extbridge 是桌面端与 MV3 扩展之间的本机 WebSocket 控制面。
// 桌面端保留队列、配置库、验证码和落盘；扩展负责驱动页面。
// 本包只定义协议、会话和 RPC，不操作网页 DOM。
package extbridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Version 是当前信封版本。
const Version = 1

// DefaultAddr 只监听本机回环地址。
const DefaultAddr = "127.0.0.1:18765"

// SettingBrowserBackend 是配置库里的控制面开关，取值 extension 或 ego。
// 它和 browser_backend（Chrome | Edge）不是同一个键。
// 任务引擎在千问或豆包且该值为 extension 时走扩展；否则打开 Chrome 或 Edge 实例。
const SettingBrowserBackend = "browserBackend"

// 页面桥令牌和端口。扩展弹出页必须填写同一组值。
// 空字符串按默认值处理，和任务引擎启动桥时的回退一致。
const (
	SettingExtToken = "extBridgeToken"
	SettingExtPort  = "extBridgePort"
	DefaultExtToken = "dev-token"
	DefaultExtPort  = "18765"
)

// NormalizeExtToken 去掉空白。空白令牌用默认值，避免界面上是空的、桥却在听另一枚令牌。
func NormalizeExtToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultExtToken
	}
	return v
}

// NormalizeExtPort 去掉空白。空白端口用默认值。
func NormalizeExtPort(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultExtPort
	}
	return v
}

// NewToken 生成一枚页面桥令牌，供桌面端重新生成后交给扩展。
func NewToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// MaxMessageBytes 是单条 WebSocket 消息的上限。图片以分片 base64 传递，单片必须小于这个值。
const MaxMessageBytes = 8 << 20

// 信封 type。
const (
	TypeCmd   = "cmd"
	TypeAck   = "ack"
	TypeEvent = "event"
	TypePing  = "ping"
	TypePong  = "pong"
	TypeHello = "hello"
	TypeError = "error"
)

// 平台标识。桌面端的「千问 / 豆包」在协议里用这两个值。
const (
	PlatformQianwen = "qianwen"
	PlatformDoubao  = "doubao"
)

// 命令名。页面自动化留到后续 PR，名称在这一版固定。
const (
	CmdPing          = "ping"
	CmdHelloAck      = "helloAck"
	CmdUpload        = "upload"
	CmdSetPrompt     = "setPrompt"
	CmdSend          = "send"
	CmdWaitResult    = "waitResult"
	CmdDownload      = "download"
	CmdCaptchaAssist = "captchaAssist"
	CmdCancel        = "cancel"
	CmdStatus        = "status"
)

// 稳定错误码。扩展和桌面端都只使用这些字符串。
const (
	ENotConnected     = "E_NOT_CONNECTED"
	ENoTab            = "E_NO_TAB"
	ENotLoggedIn      = "E_NOT_LOGGED_IN"
	ESelectorMiss     = "E_SELECTOR_MISS"
	EUploadFail       = "E_UPLOAD_FAIL"
	ESendUncertain    = "E_SEND_UNCERTAIN"
	ETimeout          = "E_TIMEOUT"
	ECaptchaHuman     = "E_CAPTCHA_HUMAN"
	ECaptchaSolveFail = "E_CAPTCHA_SOLVE_FAIL"
	EDownloadFail     = "E_DOWNLOAD_FAIL"
	ESWSleep          = "E_SW_SLEEP"
	EProtocol         = "E_PROTOCOL"
	EAuth             = "E_AUTH"
	ECancelled        = "E_CANCELLED"
	EQuota            = "E_QUOTA"
	ERateLimit        = "E_RATE_LIMIT"
)

// Envelope 是 v1 信封。时间戳 ts 为 Unix 毫秒。
type Envelope struct {
	V              int             `json:"v"`
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	TS             int64           `json:"ts"`
	Platform       string          `json:"platform,omitempty"`
	SessionID      string          `json:"sessionId,omitempty"`
	TaskID         string          `json:"taskId,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// ProtoError 是协议错误。Code 为稳定错误码。
type ProtoError struct {
	Code    string
	Message string
}

func (e *ProtoError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

// Is 让 errors.Is(err, &ProtoError{Code: ETimeout}) 按错误码匹配。
func (e *ProtoError) Is(target error) bool {
	other, ok := target.(*ProtoError)
	if !ok || other == nil {
		return false
	}
	return other.Code == "" || other.Code == e.Code
}

// CodeOf 返回错误中的稳定错误码。没有时返回空字符串。
func CodeOf(err error) string {
	var pe *ProtoError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// NormalizeBrowserBackend 只把精确的 extension 当成扩展控制面，其余值保持 ego。
func NormalizeBrowserBackend(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), "extension") {
		return "extension"
	}
	return "ego"
}

// Commands 按协议固定顺序列出命令名。
func Commands() []string {
	return []string{
		CmdPing, CmdHelloAck, CmdUpload, CmdSetPrompt, CmdSend,
		CmdWaitResult, CmdDownload, CmdCaptchaAssist, CmdCancel, CmdStatus,
	}
}

// ErrorCodes 按协议固定顺序列出错误码。
func ErrorCodes() []string {
	return []string{
		ENotConnected, ENoTab, ENotLoggedIn, ESelectorMiss, EUploadFail,
		ESendUncertain, ETimeout, ECaptchaHuman, ECaptchaSolveFail, EDownloadFail,
		ESWSleep, EProtocol, EAuth, ECancelled, EQuota, ERateLimit,
	}
}

// Types 按协议固定顺序列出信封类型。
func Types() []string {
	return []string{TypeCmd, TypeAck, TypeEvent, TypePing, TypePong, TypeHello, TypeError}
}

// Encode 校验并序列化信封。
func Encode(e Envelope) ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(e)
}

// Decode 解析并校验信封。
func Decode(b []byte) (Envelope, error) {
	if len(bytesTrim(b)) == 0 {
		return Envelope{}, &ProtoError{Code: EProtocol, Message: "空消息"}
	}
	var env Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return Envelope{}, &ProtoError{Code: EProtocol, Message: "JSON 无法解析"}
	}
	if err := env.Validate(); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// Validate 检查版本、类型和命令名。
func (e Envelope) Validate() error {
	if e.V != Version {
		return &ProtoError{Code: EProtocol, Message: fmt.Sprintf("不支持的协议版本 %d", e.V)}
	}
	if e.ID == "" {
		return &ProtoError{Code: EProtocol, Message: "缺少 id"}
	}
	if !known(e.Type, Types()) {
		return &ProtoError{Code: EProtocol, Message: "未知 type " + e.Type}
	}
	if e.Platform != "" && e.Platform != PlatformQianwen && e.Platform != PlatformDoubao {
		return &ProtoError{Code: EProtocol, Message: "未知 platform " + e.Platform}
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return &ProtoError{Code: EProtocol, Message: "payload 不是 JSON"}
	}
	switch e.Type {
	case TypeCmd:
		name, err := e.Name()
		if err != nil || !known(name, Commands()) {
			return &ProtoError{Code: EProtocol, Message: "未知命令"}
		}
	case TypeHello:
		if e.Platform == "" {
			return &ProtoError{Code: EProtocol, Message: "hello 缺少 platform"}
		}
		if _, ok := helloToken(e); !ok {
			return &ProtoError{Code: EProtocol, Message: "hello 缺少 token"}
		}
	case TypeError:
		code, _ := e.ErrorFields()
		if code == "" {
			return &ProtoError{Code: EProtocol, Message: "error 缺少 code"}
		}
	}
	return nil
}

// Name 读取 payload.name。没有名称时返回空字符串。
func (e Envelope) Name() (string, error) {
	if len(e.Payload) == 0 {
		return "", nil
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(e.Payload, &body); err != nil {
		return "", err
	}
	return body.Name, nil
}

// ErrorFields 读取错误码和说明。
func (e Envelope) ErrorFields() (code, message string) {
	if len(e.Payload) == 0 {
		return "", ""
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(e.Payload, &body); err != nil {
		return "", ""
	}
	return body.Code, body.Message
}

// Command 组装一条尚未填写 id 的命令。Call 会补上 id、时间和会话。
func Command(name string, fields map[string]any) (Envelope, error) {
	body := map[string]any{"name": name}
	for k, v := range fields {
		if k == "name" {
			continue
		}
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{V: Version, Type: TypeCmd, Payload: raw}, nil
}

// WeakToken 是空令牌或只该用于本机冒烟的 dev-token。桌面端正式运行会换成随机令牌。
func WeakToken(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == DefaultExtToken
}

// HelloAccount 读取 hello payload 里的可选账号标识。旧扩展不带这个字段。
func HelloAccount(e Envelope) (id, name string) {
	if len(e.Payload) == 0 {
		return "", ""
	}
	var body struct {
		Account     string `json:"account"`
		AccountName string `json:"accountName"`
	}
	if err := json.Unmarshal(e.Payload, &body); err != nil {
		return "", ""
	}
	return strings.TrimSpace(body.Account), strings.TrimSpace(body.AccountName)
}

func helloToken(e Envelope) (string, bool) {
	if len(e.Payload) == 0 {
		return "", false
	}
	var body struct {
		Token *string `json:"token"`
	}
	if err := json.Unmarshal(e.Payload, &body); err != nil || body.Token == nil {
		return "", false
	}
	return *body.Token, true
}

func terminalError(env Envelope) error {
	switch env.Type {
	case TypeError:
		code, message := env.ErrorFields()
		if code == "" {
			code = EProtocol
		}
		return &ProtoError{Code: code, Message: message}
	case TypeAck:
		if len(env.Payload) == 0 {
			return nil
		}
		var body struct {
			OK      *bool  `json:"ok"`
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(env.Payload, &body); err != nil {
			return &ProtoError{Code: EProtocol, Message: "ack payload 无法解析"}
		}
		if body.OK != nil && !*body.OK {
			code := body.Code
			if code == "" {
				code = EProtocol
			}
			return &ProtoError{Code: code, Message: body.Message}
		}
		if body.Code != "" && body.OK == nil {
			return &ProtoError{Code: body.Code, Message: body.Message}
		}
	}
	return nil
}

func known(v string, list []string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("id-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func nowMS() int64 { return time.Now().UnixMilli() }

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
