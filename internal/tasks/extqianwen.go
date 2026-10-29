package tasks

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

const (
	// 3MiB 原文经 base64 后仍低于 extbridge.MaxMessageBytes。
	extChunkRaw    = 3 << 20
	extMaxDownload = 64 << 20
	extHelloWait   = 30 * time.Second
)

// errQianwenLogin / errDoubaoLogin 表示扩展打开的页面还没有输入框。整批停止，避免把未登录重试打满。
var errQianwenLogin = errors.New("千问页面未登录，请在扩展打开的网页登录后再开始任务")
var errDoubaoLogin = errors.New("豆包页面未登录，请在扩展打开的网页登录后再开始任务")

// QianwenJob 是一次扩展生图。Images 为空时不上传，只填词并发送。IdempotencyPrefix 在同一批次内稳定，换一批就换掉，避免发送键撞车。
type QianwenJob struct {
	TaskID            string
	IdempotencyPrefix string
	Prompt            string
	Images            []string
	Wait              time.Duration
	SkipSubmit        bool
}

// QianwenRun 是扩展跑完一轮后的结果。Submitted 为真表示发送已被页面确认。
type QianwenRun struct {
	Results   []strategy.Result
	Submitted bool
}

type extAck struct {
	Name           string `json:"name"`
	OK             *bool  `json:"ok"`
	Code           string `json:"code"`
	Message        string `json:"message"`
	CaptchaBlocked bool   `json:"captchaBlocked"`
	Kind           string `json:"kind"`
	Baxia          bool   `json:"baxia"`
	Crop           string `json:"crop"`
	Images         []struct {
		URL string `json:"url"`
	} `json:"images"`
	DataBase64 string `json:"dataBase64"`
	Total      int    `json:"total"`
	Offset     int    `json:"offset"`
	Done       bool   `json:"done"`
	Path       string `json:"path"`
}

func (e *Engine) useExtension(platform string) bool {
	if e == nil || e.store == nil || (platform != "千问" && platform != "豆包") {
		return false
	}
	return extbridge.NormalizeBrowserBackend(e.store.Get(extbridge.SettingBrowserBackend)) == "extension"
}

func (e *Engine) rotateExtBatch() {
	e.extMu.Lock()
	e.extBatch = strconv.FormatInt(time.Now().UnixNano(), 36)
	e.extMu.Unlock()
}

func (e *Engine) currentExtBatch() string {
	e.extMu.Lock()
	defer e.extMu.Unlock()
	if e.extBatch == "" {
		e.extBatch = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return e.extBatch
}

func (e *Engine) closeExtensionBridge() {
	e.extMu.Lock()
	srv := e.extSrv
	pair := e.pair
	e.extSrv = nil
	e.pair = nil
	e.extToken = ""
	e.extListen = ""
	e.extMu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
	if pair != nil {
		_ = pair.Close()
	}
}

// BridgeStatus 是页面桥当前能不能用。界面用它显示连接，不把令牌带出来。
type BridgeStatus struct {
	Listening bool
	Addr      string
	Err       string
	Detail    string
	Qianwen   bool
	Doubao    bool
}

// EnsureExtensionBridge 在应用打开时就开始听，扩展重启后可以自己连回来。
// onChange 在连上、断开或 hello 失败时调用，用来刷新界面。
func (e *Engine) EnsureExtensionBridge(onChange func()) {
	if e == nil {
		return
	}
	e.extMu.Lock()
	e.extOnChange = onChange
	e.extMu.Unlock()
	if _, err := e.extensionServer(); err != nil {
		logging.Log.Errorf("页面桥: %v", err)
	}
	e.fireBridgeChange()
}

// ReloadExtensionBridge 在桥已经监听时，按配置库里的令牌和端口重新监听。
// 令牌没变就不动。还没听过则不主动占端口，等任务或界面开始再听。
func (e *Engine) ReloadExtensionBridge() error {
	if e == nil {
		return nil
	}
	e.extMu.Lock()
	running := e.extSrv != nil
	e.extMu.Unlock()
	if !running {
		return nil
	}
	_, err := e.extensionServer()
	e.fireBridgeChange()
	return err
}

func (e *Engine) fireBridgeChange() {
	e.extMu.Lock()
	fn := e.extOnChange
	e.extMu.Unlock()
	if fn != nil {
		fn()
	}
}

// BridgeStatus 读取监听状态。没听过时 Listening 为 false。
func (e *Engine) BridgeStatus() BridgeStatus {
	if e == nil {
		return BridgeStatus{}
	}
	e.extMu.Lock()
	defer e.extMu.Unlock()
	st := BridgeStatus{Addr: e.extListen, Err: e.extErr}
	if e.extSrv == nil {
		return st
	}
	st.Listening = true
	qianwen, doubao, detail, addr := e.extSrv.Snapshot()
	st.Qianwen = qianwen
	st.Doubao = doubao
	st.Detail = detail
	if addr != "" {
		st.Addr = addr
	}
	return st
}

func (e *Engine) extensionServer() (*extbridge.Server, error) {
	token := extbridge.DefaultExtToken
	port := extbridge.DefaultExtPort
	if e.store != nil {
		token = extbridge.NormalizeExtToken(e.store.Get(extbridge.SettingExtToken))
		port = extbridge.NormalizeExtPort(e.store.Get(extbridge.SettingExtPort))
	}
	addr := net.JoinHostPort("127.0.0.1", port)
	e.extMu.Lock()
	if e.extSrv != nil && e.extToken == token && e.extListen == addr {
		srv := e.extSrv
		e.extMu.Unlock()
		return srv, nil
	}
	old := e.extSrv
	e.extSrv = nil
	e.extToken = ""
	e.extListen = ""
	e.extMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	srv, err := extbridge.Listen(extbridge.Config{
		Addr:    addr,
		Token:   token,
		OnHello: e.noteHello,
		OnEvent: e.noteExtensionLogin,
		OnChange: func() {
			e.fireBridgeChange()
		},
	})
	e.extMu.Lock()
	defer e.extMu.Unlock()
	if err != nil {
		e.extErr = fmt.Sprintf("页面桥没有在听 %s：%v。若端口被占用，关掉另一个 tulai，或修改 extBridgePort 后重新打开。", addr, err)
		return nil, errors.New(e.extErr)
	}
	if e.extSrv != nil {
		go srv.Close()
		return e.extSrv, nil
	}
	e.extSrv = srv
	e.extErr = ""
	e.extToken = token
	e.extListen = addr
	logging.Log.Info("扩展桥监听 %s", srv.Addr())
	return srv, nil
}

func (e *Engine) noteHello(sess *extbridge.Session) {
	e.rememberExtension(sess)
	e.poke()
}

func (e *Engine) rememberExtension(sess *extbridge.Session) {
	e.noteExtensionVersion(sess)
	if e == nil || e.store == nil || sess == nil || sess.Account() == "" {
		return
	}
	loggedIn, known := sess.Login()
	if !known {
		return
	}
	e.applyExtensionLogin(extensionPlatformLabel(sess.Platform()), sess.Account(), sess.AccountName(), loggedIn)
}

func (e *Engine) noteExtensionLogin(sess *extbridge.Session, env extbridge.Envelope) {
	if e == nil || e.store == nil || env.Type != extbridge.TypeEvent || len(env.Payload) == 0 {
		return
	}
	var body struct {
		Name     string `json:"name"`
		LoggedIn *bool  `json:"loggedIn"`
		Account  string `json:"account"`
	}
	if json.Unmarshal(env.Payload, &body) != nil || body.Name != "login" || body.LoggedIn == nil {
		return
	}
	account := strings.TrimSpace(body.Account)
	if account == "" && sess != nil {
		account = sess.Account()
	}
	if account == "" {
		return
	}
	platform := ""
	if sess != nil {
		platform = extensionPlatformLabel(sess.Platform())
	}
	if env.Platform != "" {
		platform = extensionPlatformLabel(env.Platform)
	}
	name := ""
	if sess != nil {
		name = sess.AccountName()
	}
	e.applyExtensionLogin(platform, account, name, *body.LoggedIn)
}

func (e *Engine) applyExtensionLogin(platform, account, name string, loggedIn bool) {
	if e == nil || e.store == nil {
		return
	}
	platform = strings.TrimSpace(platform)
	account = strings.TrimSpace(account)
	if platform == "" || account == "" {
		return
	}
	if !loggedIn {
		e.store.DropExtensionAccount(platform, account)
		return
	}
	if _, err := e.store.SaveLoggedInExtension(platform, account, name); err != nil {
		logging.Log.Warning("登记已登录扩展账号失败: %v", err)
	}
}

func extensionPlatformLabel(protocol string) string {
	if protocol == extbridge.PlatformDoubao || protocol == "豆包" {
		return "豆包"
	}
	return "千问"
}

func loggedInExtension(st *store.Store, platform, account string) (store.Browser, bool) {
	if st == nil {
		return store.Browser{}, false
	}
	for _, row := range st.Browsers() {
		if row.Platform == platform && row.ExtAccount == account && row.IsLoggedIn {
			return row, true
		}
	}
	return store.Browser{}, false
}

func (e *Engine) noteExtensionTaskLogin(platform string, sess *extbridge.Session, err error) {
	if e == nil || e.store == nil || sess == nil || sess.Account() == "" {
		return
	}
	if errors.Is(err, errQianwenLogin) || errors.Is(err, errDoubaoLogin) || extbridge.CodeOf(err) == extbridge.ENotLoggedIn {
		e.store.DropExtensionAccount(platform, sess.Account())
		return
	}
	if !extensionSawLogin(err) {
		return
	}
	if _, saveErr := e.store.SaveLoggedInExtension(platform, sess.Account(), sess.AccountName()); saveErr != nil {
		logging.Log.Warning("登记已登录扩展账号失败: %v", saveErr)
	}
}

func extensionSawLogin(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, errQianwenLogin) || errors.Is(err, errDoubaoLogin) || extbridge.CodeOf(err) == extbridge.ENotLoggedIn {
		return false
	}
	switch extbridge.CodeOf(err) {
	case extbridge.EQuota, extbridge.ERateLimit, extbridge.ECaptchaHuman, extbridge.EModeration, extbridge.ESendUncertain:
		return true
	}
	return errors.Is(err, strategy.ErrAccountQuota) || errors.Is(err, strategy.ErrAccountRate) || errors.Is(err, strategy.ErrModeration) || errors.Is(err, captcha.ErrVerificationRequired)
}

const extensionReloadNotice = "插件有新版本，点「重新加载」即可"

func (e *Engine) noteExtensionVersion(sess *extbridge.Session) {
	if e == nil || sess == nil {
		return
	}
	got := strings.TrimSpace(sess.ExtVersion())
	want := extbundle.Version()
	if got == "" || got == want {
		e.setExtNotice("")
		return
	}
	e.setExtNotice(extensionReloadNotice)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		cmd, err := extbridge.Command(extbridge.CmdReload, nil)
		if err != nil {
			return
		}
		_, _ = sess.Call(ctx, cmd)
	}()
}

func (e *Engine) setExtNotice(text string) {
	e.extMu.Lock()
	prev := e.extNotice
	e.extNotice = text
	e.extMu.Unlock()
	if text != "" && text != prev {
		logging.Log.Warning("%s", text)
	}
	if text != prev {
		e.fireBridgeChange()
	}
}

// ExtensionNotice 是扩展版本和磁盘上的副本不一致时给界面的那句话。
func (e *Engine) ExtensionNotice() string {
	if e == nil {
		return ""
	}
	e.extMu.Lock()
	defer e.extMu.Unlock()
	return e.extNotice
}

func (e *Engine) syncExtensionAccounts() {
	if e == nil || e.store == nil || !e.useExtension("千问") {
		return
	}
	e.extMu.Lock()
	srv := e.extSrv
	e.extMu.Unlock()
	if srv == nil {
		return
	}
	for _, sess := range srv.Sessions("") {
		// claimRun 持有 e.mu，这里不能 poke。
		e.rememberExtension(sess)
	}
}

func (e *Engine) extensionAccountKey(id int64) string {
	if e == nil || e.store == nil || id <= 0 {
		return ""
	}
	for _, b := range e.store.Browsers() {
		if b.ID == id {
			return b.ExtAccount
		}
	}
	return ""
}

func (e *Engine) extensionSession(ctx context.Context, platform, account string) (*extbridge.Session, error) {
	srv, err := e.extensionServer()
	if err != nil {
		return nil, err
	}
	if account != "" {
		if sess := srv.SessionByAccount(platform, account); sess != nil {
			return sess, nil
		}
		wait, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		for {
			sess, err := srv.Next(wait)
			if err != nil {
				if errors.Is(err, context.Canceled) && ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, errExtOffline
			}
			e.noteHello(sess)
			if sess.Platform() == platform && sess.Account() == account {
				return sess, nil
			}
		}
	}
	if sess, err := srv.Session(platform); err == nil {
		return sess, nil
	}
	label := "千问"
	if platform == extbridge.PlatformDoubao {
		label = "豆包"
	}
	wait, cancel := context.WithTimeout(ctx, extHelloWait)
	defer cancel()
	for {
		if sess, err := srv.Session(platform); err == nil {
			return sess, nil
		}
		sess, err := srv.Next(wait)
		if err != nil {
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil, &extbridge.ProtoError{Code: extbridge.ENotConnected, Message: "等待" + label + "扩展 hello 超时。请打开对应网页，确认扩展弹出页的令牌和端口与 tulai「浏览器」页一致。"}
			}
			return nil, err
		}
		if sess.Platform() != platform {
			continue
		}
		select {
		case <-sess.Done():
			continue
		default:
			return sess, nil
		}
	}
}

func (e *Engine) attemptQianwenExtension(ctx context.Context, t *Task) error {
	return e.attemptPlatformExtension(ctx, t, extbridge.PlatformQianwen, "千问")
}

func (e *Engine) attemptDoubaoExtension(ctx context.Context, t *Task) error {
	return e.attemptPlatformExtension(ctx, t, extbridge.PlatformDoubao, "豆包")
}

func (e *Engine) attemptPlatformExtension(ctx context.Context, t *Task, protocolPlatform, label string) error {
	if !t.submitted && e.opts.WaitBeforeSend > 0 {
		select {
		case <-time.After(time.Duration(e.opts.WaitBeforeSend) * time.Second):
		case <-ctx.Done():
			return fmt.Errorf("已停止")
		}
	}
	account := ""
	if t.BrowserID > 0 {
		account = e.extensionAccountKey(t.BrowserID)
	}
	sess, err := e.extensionSession(ctx, protocolPlatform, account)
	if err != nil {
		return err
	}
	if sess != nil && sess.Account() != "" && e.store != nil {
		if row, ok := loggedInExtension(e.store, label, sess.Account()); ok {
			if t.BrowserID <= 0 {
				t.BrowserID = row.ID
			}
			if t.ranOn <= 0 {
				t.ranOn = row.ID
			}
		}
	}
	if t.ranOn <= 0 {
		t.ranOn = t.BrowserID
	}
	wait := time.Duration(0)
	if e.opts.WaitResult {
		secs := e.opts.WaitResultSeconds
		if secs <= 0 {
			secs = 120
		}
		wait = time.Duration(secs) * time.Second
	}
	out, err := runExtension(ctx, sess, QianwenJob{
		TaskID:            strconv.Itoa(t.Index),
		IdempotencyPrefix: e.currentExtBatch() + ":" + strconv.Itoa(t.Index),
		Prompt:            t.Prompt,
		Images:            t.Images,
		Wait:              wait,
		SkipSubmit:        t.submitted,
	}, label)
	e.noteExtensionTaskLogin(label, sess, err)
	if out.Submitted {
		t.submitted = true
		logging.Log.Info("#%d 已发送提示词（%.20s…）", t.Index, t.Prompt)
	}
	var saveErr error
	if len(out.Results) > 0 {
		var saved []string
		saved, saveErr = e.saveResults(ctx, t, strategy.Factory(t.Platform), nil, out.Results)
		t.Results = append(t.Results, saved...)
	}
	return errors.Join(err, saveErr)
}

// RunQianwenExtension 通过已握手的千问扩展会话跑通上传、填词、发送、等待和下载。
// 发送使用稳定幂等键；点过发送但未确认时返回 ErrSubmissionUncertain，调用方不得再发一次。
func RunQianwenExtension(ctx context.Context, sess *extbridge.Session, job QianwenJob) (QianwenRun, error) {
	return runExtension(ctx, sess, job, "千问")
}

// RunDoubaoExtension 与千问走同一套命令。会话必须是 platform=doubao 的那条 hello。
func RunDoubaoExtension(ctx context.Context, sess *extbridge.Session, job QianwenJob) (QianwenRun, error) {
	return runExtension(ctx, sess, job, "豆包")
}

func runExtension(ctx context.Context, sess *extbridge.Session, job QianwenJob, platform string) (QianwenRun, error) {
	var out QianwenRun
	if sess == nil {
		return out, &extbridge.ProtoError{Code: extbridge.ENotConnected, Message: "扩展未连接"}
	}
	if job.TaskID == "" {
		if platform == "豆包" {
			job.TaskID = "doubao"
		} else {
			job.TaskID = "qianwen"
		}
	}
	if job.IdempotencyPrefix == "" {
		job.IdempotencyPrefix = job.TaskID
	}
	if _, err := extCall(ctx, sess, extbridge.CmdCaptchaAssist, job.TaskID, "", nil, 10*time.Second, platform); err != nil {
		return out, err
	}
	if !job.SkipSubmit {
		if err := extUpload(ctx, sess, job, platform); err != nil {
			return out, err
		}
		if len(job.Images) > 0 {
			logging.Log.Info("扩展任务 %s 已上传 %d 张源图", job.TaskID, len(job.Images))
		}
		if _, err := extCall(ctx, sess, extbridge.CmdSetPrompt, job.TaskID, "", map[string]any{"prompt": job.Prompt}, 10*time.Second, platform); err != nil {
			return out, err
		}
		// 键放在信封上：成功和 E_SEND_UNCERTAIN 会被桥缓存，同一次任务不会再把 send 发到扩展。
		// 超时不进缓存。内容脚本还会按这个键拒绝第二次点击。
		if _, err := extCall(ctx, sess, extbridge.CmdSend, job.TaskID, job.IdempotencyPrefix+":send", map[string]any{
			"prompt":    job.Prompt,
			"timeoutMs": 15000,
		}, 25*time.Second, platform); err != nil {
			return out, err
		}
		out.Submitted = true
	}
	if job.Wait <= 0 {
		return out, nil
	}
	body, err := extCall(ctx, sess, extbridge.CmdWaitResult, job.TaskID, "", map[string]any{
		"prompt":    job.Prompt,
		"timeoutMs": job.Wait.Milliseconds(),
	}, job.Wait+20*time.Second, platform)
	if err != nil {
		return out, err
	}
	if len(body.Images) == 0 {
		return out, fmt.Errorf("等待生成结果超时：未得到有效的新图片")
	}
	var dlErr error
	for _, item := range body.Images {
		data, usedURL, removed, err := extDownload(ctx, sess, job.TaskID, item.URL, platform)
		if err != nil {
			dlErr = errors.Join(dlErr, err)
			continue
		}
		if data == nil {
			dlErr = errors.Join(dlErr, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载没有图片数据"})
			continue
		}
		out.Results = append(out.Results, strategy.Result{
			URL:              usedURL,
			Src:              "extension",
			TS:               time.Now().UnixMilli(),
			Data:             data,
			WatermarkRemoved: removed,
		})
	}
	return out, dlErr
}

func extUpload(ctx context.Context, sess *extbridge.Session, job QianwenJob, platform string) error {
	for i, path := range job.Images {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取源图: %w", err)
		}
		if len(data) == 0 {
			return fmt.Errorf("源图是空文件")
		}
		uploadID := job.IdempotencyPrefix + ":upload:" + strconv.Itoa(i)
		for offset := 0; offset < len(data); {
			end := offset + extChunkRaw
			if end > len(data) {
				end = len(data)
			}
			_, err := extCall(ctx, sess, extbridge.CmdUpload, job.TaskID, "", map[string]any{
				"uploadId":   uploadID,
				"fileName":   filepath.Base(path),
				"mime":       imageMIME(path),
				"total":      len(data),
				"offset":     offset,
				"dataBase64": base64.StdEncoding.EncodeToString(data[offset:end]),
				"finish":     end == len(data),
				"clean":      i == 0 && offset == 0,
				"timeoutMs":  30000,
			}, 45*time.Second, platform)
			if err != nil {
				return fmt.Errorf("上传: %w", err)
			}
			offset = end
		}
	}
	return nil
}

// extDownload 先按豆包 CDN 规则去掉水印查询（失败再退回页面原地址），
// 字节校验通过后对豆包和千问跑 images.RemoveWatermark。匹配时返回的是去水印 PNG。
func extDownload(ctx context.Context, sess *extbridge.Session, taskID, rawURL, platform string) ([]byte, string, bool, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, "", false, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "缺少图片地址"}
	}
	clean := strategy.PreferUnwatermarkedURL(rawURL)
	data, err := extDownloadURL(ctx, sess, taskID, clean, platform)
	used := clean
	if err != nil && clean != rawURL && ctx.Err() == nil {
		logging.Log.Warning("扩展结果去掉水印参数后下载失败，改用页面原地址")
		data, err = extDownloadURL(ctx, sess, taskID, rawURL, platform)
		used = rawURL
	}
	if err != nil {
		return nil, "", false, err
	}
	if data == nil {
		return nil, used, false, nil
	}
	data, removed, err := dewatermarkExtImage(ctx, data, platform)
	if err != nil {
		return nil, "", false, err
	}
	return data, used, removed, nil
}

func extDownloadURL(ctx context.Context, sess *extbridge.Session, taskID, rawURL, platform string) ([]byte, error) {
	var buf []byte
	offset := 0
	for {
		body, err := extCall(ctx, sess, extbridge.CmdDownload, taskID, "", map[string]any{
			"url":    rawURL,
			"offset": offset,
			"limit":  extChunkRaw,
		}, 45*time.Second, platform)
		if err != nil {
			return nil, err
		}
		if body.Path != "" && len(body.DataBase64) == 0 {
			data, err := readExtDownload(body.Path)
			if err != nil {
				return nil, err
			}
			return validateExtImage(data)
		}
		chunk, err := base64.StdEncoding.DecodeString(body.DataBase64)
		if err != nil {
			return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载数据不是 base64"}
		}
		buf = append(buf, chunk...)
		offset += len(chunk)
		if body.Done || (body.Total > 0 && offset >= body.Total) {
			if body.Total > 0 && offset != body.Total {
				return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载分片长度不符"}
			}
			return validateExtImage(buf)
		}
		if len(chunk) == 0 || offset > extMaxDownload {
			return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载没有推进"}
		}
	}
}

func validateExtImage(data []byte) ([]byte, error) {
	if !images.IsValidImageBytes(data) {
		return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载内容不是 PNG、JPEG、GIF 或 WebP"}
	}
	return data, nil
}

// dewatermarkExtImage 在扩展下载的静态图上复用 images.RemoveWatermark。
// 未匹配、非方图、动画或处理失败时保留下载字节；取消时不把未处理的图当成成功。
func dewatermarkExtImage(ctx context.Context, data []byte, platform string) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if platform != "豆包" && platform != "千问" {
		return data, false, nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || !watermarkStaticImage(data, format) {
		return data, false, nil
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, false, nil
	}
	repaired, changed, err := images.RemoveWatermark(ctx, decoded, platform)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		logging.Log.Warning("扩展结果去水印失败，保留下载原图: %s", err)
		return data, false, nil
	}
	if !changed {
		return data, false, nil
	}
	out, err := images.EncodePNG(repaired)
	if err != nil || !images.IsValidImageBytes(out) {
		if err != nil {
			logging.Log.Warning("扩展结果去水印编码失败，保留下载原图: %s", err)
		}
		return data, false, nil
	}
	logging.Log.Info("扩展下载结果已去掉匹配到的%s水印", platform)
	return out, true, nil
}

func readExtDownload(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载路径不是绝对路径"}
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > extMaxDownload {
		return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载文件不可读"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "读取下载文件失败"}
	}
	return data, nil
}

// solveQianwenBaxia 在千问验证码挡住发送或等待时，用已打开的 Chrome 调试端口做一次信任拖动。
// 测试替换这个函数。豆包不调用它。
var solveQianwenBaxia = captcha.SolveQianwenSlider

func extCall(ctx context.Context, sess *extbridge.Session, name, taskID, idem string, fields map[string]any, timeout time.Duration, platform string) (extAck, error) {
	body, err := extCallOnce(ctx, sess, name, taskID, idem, fields, timeout, platform)
	if platform != "千问" || !qianwenSliderBlock(err, body) {
		return body, err
	}
	if sErr := solveQianwenBaxia(ctx); sErr != nil {
		logging.Log.Warning("千问滑块未自动通过，仍需人工验证: %v", sErr)
		return body, err
	}
	logging.Log.Success("千问滑块已通过，继续当前步骤")
	return extCallOnce(ctx, sess, name, taskID, idem, fields, timeout, platform)
}

func qianwenSliderBlock(err error, body extAck) bool {
	if !(body.CaptchaBlocked || extbridge.CodeOf(err) == extbridge.ECaptchaHuman) {
		return false
	}
	return body.Kind != "grid"
}

func extCallOnce(ctx context.Context, sess *extbridge.Session, name, taskID, idem string, fields map[string]any, timeout time.Duration, platform string) (extAck, error) {
	cmd, err := extbridge.Command(name, fields)
	if err != nil {
		return extAck{}, err
	}
	cmd.TaskID = taskID
	cmd.IdempotencyKey = idem
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	env, callErr := sess.Call(callCtx, cmd)
	var body extAck
	if len(env.Payload) > 0 {
		_ = json.Unmarshal(env.Payload, &body)
	}
	if mapped := mapExtErr(callErr, body, platform); mapped != nil {
		return body, mapped
	}
	return body, nil
}

func mapExtErr(err error, body extAck, platform string) error {
	if body.CaptchaBlocked || extbridge.CodeOf(err) == extbridge.ECaptchaHuman {
		// 千问滑块交给 extCall 再试一次 CDP。这里只对不会自动拖的情况记一笔。
		if platform != "千问" || body.Kind == "grid" {
			if body.Crop != "" {
				logging.Log.Info("%s验证码已拦截，已带回画面 %d 字符（未自动求解）", platform, len(body.Crop))
			} else {
				logging.Log.Info("%s验证码已拦截，页面没有可回传的画布", platform)
			}
		}
		if err != nil {
			return fmt.Errorf("%w: %s", captcha.ErrVerificationRequired, err.Error())
		}
		return captcha.ErrVerificationRequired
	}
	if err == nil {
		return nil
	}
	switch extbridge.CodeOf(err) {
	case extbridge.ESendUncertain:
		return fmt.Errorf("%w: %s", strategy.ErrSubmissionUncertain, err.Error())
	case extbridge.EQuota:
		return fmt.Errorf("%w: %s", strategy.ErrAccountQuota, err.Error())
	case extbridge.ERateLimit:
		return fmt.Errorf("%w: %s", strategy.ErrAccountRate, err.Error())
	case extbridge.EModeration:
		return fmt.Errorf("%w: %s", strategy.ErrModeration, err.Error())
	case extbridge.ENotLoggedIn:
		loginErr := errQianwenLogin
		if platform == "豆包" {
			loginErr = errDoubaoLogin
		}
		return fmt.Errorf("%w: %s", loginErr, err.Error())
	default:
		return err
	}
}

func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}
