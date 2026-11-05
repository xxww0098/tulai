package chatgpt

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
)

//go:embed browser.js
var browserJS string

type browserTransport struct{ page *cdp.Session }

func (b browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !allowedURL(req.URL.String()) {
		return nil, errors.New("ChatGPT 资源地址不在允许的域名内")
	}
	var body []byte
	if req.Body != nil {
		defer req.Body.Close()
		var err error
		body, err = io.ReadAll(io.LimitReader(req.Body, maxBytes+1))
		if err != nil || len(body) > maxBytes {
			return nil, errors.New("ChatGPT 上传内容超过 64 MiB 或读取失败")
		}
	}
	headers := make(map[string]string, len(req.Header))
	for k := range req.Header {
		headers[k] = req.Header.Get(k)
	}
	data, err := json.Marshal(map[string]any{"url": req.URL.String(), "method": req.Method, "headers": headers, "body": base64.StdEncoding.EncodeToString(body)})
	if err != nil {
		return nil, err
	}
	id := uuid()
	r := &browserBody{transport: b, ctx: req.Context(), id: id}
	if err := b.page.EvaluateContext(req.Context(), fmt.Sprintf("(%s)(%q,%s)", browserJS, id, data), nil); err != nil {
		r.Close()
		return nil, err
	}
	for {
		state, err := r.poll()
		if err != nil {
			r.Close()
			return nil, err
		}
		if state.Status != 0 {
			h := make(http.Header)
			for k, v := range state.Headers {
				h.Set(k, v)
			}
			return &http.Response{StatusCode: state.Status, Header: h, Body: r, ContentLength: -1, Request: req}, nil
		}
		if err := r.pause(); err != nil {
			r.Close()
			return nil, err
		}
	}
}

type browserState struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Chunks  []string          `json:"chunks"`
	Done    bool              `json:"done"`
	Error   bool              `json:"error"`
}

type browserBody struct {
	transport browserTransport
	ctx       context.Context
	id        string
	buffer    []byte
	done      bool
	err       error
	closed    bool
}

func (r *browserBody) poll() (browserState, error) {
	var state browserState
	expr := fmt.Sprintf(`(() => {
 const s = window.__tulaiChatGPTRequests?.[%q];
 if (!s) return {done:true,error:true};
 const chunks = s.chunks.splice(0,8);
 s.buffered -= chunks.reduce((n,c) => n + atob(c).length, 0);
 return {status:s.status,headers:s.headers,chunks,done:s.done && !s.chunks.length,error:s.error};
})()`, r.id)
	if err := r.transport.page.EvaluateContext(r.ctx, expr, &state); err != nil {
		return state, err
	}
	for _, chunk := range state.Chunks {
		b, err := base64.StdEncoding.DecodeString(chunk)
		if err != nil {
			return state, err
		}
		r.buffer = append(r.buffer, b...)
	}
	r.done = state.Done
	if state.Error {
		r.err = errors.New("ChatGPT 页面连接中断，请查看账号浏览器")
		if state.Status == 0 {
			return state, r.err
		}
	}
	return state, nil
}

func (r *browserBody) pause() error {
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

func (r *browserBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.buffer) == 0 {
		if r.closed {
			return 0, io.ErrClosedPipe
		}
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if r.done {
			if r.err != nil {
				return 0, r.err
			}
			return 0, io.EOF
		}
		if _, err := r.poll(); err != nil {
			return 0, err
		}
		if len(r.buffer) == 0 && !r.done {
			if err := r.pause(); err != nil {
				return 0, err
			}
		}
	}
	n := copy(p, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}

func (r *browserBody) Close() error {
	if r.closed {
		return nil
	}
	r.closed, r.buffer = true, nil
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return r.transport.page.EvaluateContext(ctx, fmt.Sprintf(`(() => {
 const requests = window.__tulaiChatGPTRequests;
 const s = requests?.[%q];
 if (s) { s.controller.abort(); delete requests[%q]; }
})()`, r.id, r.id), nil)
}

func New(ctx context.Context, page *cdp.Session) (*Client, error) {
	transport := browserTransport{page: page}
	var fp fingerprint
	err := page.EvaluateContext(ctx, `(() => ({
	 pageOrigin:location.origin,
 userAgent:navigator.userAgent, deviceId:decodeURIComponent(document.cookie.match(/(?:^|;\s*)oai-did=([^;]*)/)?.[1] || ''),
 language:navigator.language, languages:navigator.languages.join(','),
 script:[...document.scripts].map(s => s.src).find(s => s.includes('sentinel')) || 'https://chatgpt.com/backend-api/sentinel/sdk.js',
 build:document.documentElement.dataset.build || '', date:new Date().toString(),
 timezone:Intl.DateTimeFormat().resolvedOptions().timeZone, offset:new Date().getTimezoneOffset(),
 screen:screen.width+screen.height, cores:navigator.hardwareConcurrency, now:performance.now(), origin:performance.timeOrigin
}))()`, &fp)
	if err != nil {
		return nil, err
	}
	if fp.PageOrigin != "https://chatgpt.com" {
		return nil, ErrNeedLogin
	}
	if fp.DeviceID == "" {
		fp.DeviceID = uuid()
	}
	c := &Client{http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, fp: fp, sessionID: uuid()}
	var session struct {
		AccessToken string `json:"accessToken"`
	}
	if err := c.json(ctx, http.MethodGet, "/api/auth/session", nil, nil, &session); err != nil {
		return nil, err
	}
	c.token = strings.TrimSpace(session.AccessToken)
	if c.token == "" {
		return nil, ErrNeedLogin
	}
	return c, nil
}
