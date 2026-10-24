package tasks

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xbs/internal/captcha"
	"xbs/internal/extbridge"
	"xbs/internal/logging"
	"xbs/internal/strategy"
)

const (
	settingExtPort  = "extBridgePort"
	settingExtToken = "extBridgeToken"
	// 3MiB 原文经 base64 后仍低于 extbridge.MaxMessageBytes。
	extChunkRaw    = 3 << 20
	extMaxDownload = 64 << 20
	extHelloWait   = 30 * time.Second
)

// errQianwenLogin / errDoubaoLogin 表示扩展打开的页面还没有输入框。整批停止，避免把未登录重试打满。
var errQianwenLogin = errors.New("千问页面未登录，请在扩展打开的网页登录后再开始任务")
var errDoubaoLogin = errors.New("豆包页面未登录，请在扩展打开的网页登录后再开始任务")

// QianwenJob 是一次扩展图生图。IdempotencyPrefix 在同一批次内稳定，换一批就换掉，避免发送键撞车。
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
	e.extSrv = nil
	e.extToken = ""
	e.extListen = ""
	e.extMu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

func (e *Engine) extensionServer() (*extbridge.Server, error) {
	token := "dev-token"
	port := "18765"
	if e.store != nil {
		if got := strings.TrimSpace(e.store.Get(settingExtToken)); got != "" {
			token = got
		}
		if got := strings.TrimSpace(e.store.Get(settingExtPort)); got != "" {
			port = got
		}
	}
	addr := net.JoinHostPort("127.0.0.1", port)
	e.extMu.Lock()
	defer e.extMu.Unlock()
	if e.extSrv != nil && e.extToken == token && e.extListen == addr {
		return e.extSrv, nil
	}
	if e.extSrv != nil {
		_ = e.extSrv.Close()
		e.extSrv = nil
	}
	srv, err := extbridge.Listen(extbridge.Config{Addr: addr, Token: token})
	if err != nil {
		return nil, fmt.Errorf("启动扩展桥: %w", err)
	}
	e.extSrv = srv
	e.extToken = token
	e.extListen = addr
	logging.Log.Info("扩展桥监听 %s", srv.Addr())
	return srv, nil
}

func (e *Engine) extensionSession(ctx context.Context, platform string) (*extbridge.Session, error) {
	srv, err := e.extensionServer()
	if err != nil {
		return nil, err
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
		sess, err := srv.Next(wait)
		if err != nil {
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return nil, &extbridge.ProtoError{Code: extbridge.ENotConnected, Message: "等待" + label + "扩展 hello 超时，请加载扩展并在选项页保存同一令牌"}
			}
			return nil, err
		}
		if sess.Platform() == platform {
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
	sess, err := e.extensionSession(ctx, protocolPlatform)
	if err != nil {
		return err
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
	for _, image := range body.Images {
		data, err := extDownload(ctx, sess, job.TaskID, image.URL, platform)
		if err != nil {
			dlErr = errors.Join(dlErr, err)
			continue
		}
		if data == nil {
			dlErr = errors.Join(dlErr, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载没有图片数据"})
			continue
		}
		out.Results = append(out.Results, strategy.Result{
			URL:  image.URL,
			Src:  "extension",
			TS:   time.Now().UnixMilli(),
			Data: data,
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

func extDownload(ctx context.Context, sess *extbridge.Session, taskID, rawURL, platform string) ([]byte, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "缺少图片地址"}
	}
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
			return data, nil
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
			return buf, nil
		}
		if len(chunk) == 0 || offset > extMaxDownload {
			return nil, &extbridge.ProtoError{Code: extbridge.EDownloadFail, Message: "下载没有推进"}
		}
	}
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

func extCall(ctx context.Context, sess *extbridge.Session, name, taskID, idem string, fields map[string]any, timeout time.Duration, platform string) (extAck, error) {
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
		if body.Crop != "" {
			logging.Log.Info("%s验证码已拦截，已带回画面 %d 字符（未自动求解）", platform, len(body.Crop))
		} else {
			logging.Log.Info("%s验证码已拦截，页面没有可回传的画布", platform)
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
