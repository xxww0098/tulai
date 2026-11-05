// Package chatgpt implements ChatGPT's image protocol in Go over a bound Camoufox account.
package chatgpt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	Platform     = "ChatGPT"
	HomeURL      = "https://chatgpt.com/"
	DefaultModel = "gpt-image-2"
	maxBytes     = 64 << 20
)

var (
	ErrNeedLogin           = errors.New("ChatGPT 未登录，请在账号浏览器中登录")
	ErrVerification        = errors.New("ChatGPT 需要人工验证，请在账号浏览器中完成验证")
	ErrQuota               = errors.New("ChatGPT 生图额度已用尽")
	ErrRate                = errors.New("ChatGPT 请求过于频繁")
	ErrModeration          = errors.New("ChatGPT 审核不通过")
	ErrSubmissionUncertain = errors.New("ChatGPT 发送状态未确认，请检查账号网页后再开始任务")
)

type Client struct {
	http      *http.Client
	token     string
	sessionID string
	fp        fingerprint
}

type httpError struct {
	status int
	err    error
}

func (e *httpError) Error() string { return e.err.Error() }
func (e *httpError) Unwrap() error { return e.err }

func allowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "chatgpt.com" || strings.HasSuffix(host, ".oaiusercontent.com") || strings.HasSuffix(host, ".blob.core.windows.net")
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, headers http.Header) (*http.Response, error) {
	raw := path
	if strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
		raw = strings.TrimSuffix(HomeURL, "/") + path
	}
	if !allowedURL(raw) {
		return nil, errors.New("ChatGPT 资源地址不在允许的域名内")
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if req.URL.Hostname() == "chatgpt.com" {
		req.Header.Set("Accept", "application/json")
		if headers.Get("Accept") != "" {
			req.Header.Set("Accept", headers.Get("Accept"))
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		req.Header.Set("Oai-Device-Id", c.fp.DeviceID)
		req.Header.Set("Oai-Session-Id", c.sessionID)
		req.Header.Set("Oai-Language", c.fp.Language)
		req.Header.Set("Oai-Client-Version", "prod-a194cd50d4416d3c0b47c740f206b12ce60f5887")
		req.Header.Set("Oai-Client-Build-Number", "6708908")
		req.Header.Set("X-OpenAI-Target-Path", req.URL.Path)
		req.Header.Set("X-OpenAI-Target-Route", req.URL.Path)
	} else {
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// net/http's wrapped error includes signed URLs. Keep them out of logs.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("ChatGPT 网络请求失败，请检查账号浏览器和代理")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, &httpError{status: resp.StatusCode, err: responseError(resp.StatusCode, data)}
	}
	return resp, nil
}

func (c *Client) json(ctx context.Context, method, path string, body any, headers http.Header, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	resp, err := c.request(ctx, method, path, data, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil {
		return err
	}
	if len(data) > 8<<20 {
		return errors.New("ChatGPT 接口响应超过大小限制")
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		if bytes.Contains(bytes.ToLower(data), []byte("<html")) {
			return ErrVerification
		}
		return errors.New("ChatGPT 接口返回了无法解析的数据")
	}
	return nil
}

func responseError(status int, data []byte) error {
	text := strings.ToLower(string(data))
	switch {
	case status == 401:
		return ErrNeedLogin
	case status == 403:
		return ErrVerification
	case strings.Contains(text, "content_policy") || strings.Contains(text, "moderation_blocked"):
		return ErrModeration
	case (status == 400 || status == 429) && (strings.Contains(text, "quota") || strings.Contains(text, "image_generation_limit") || strings.Contains(text, "image_gen_limit") || strings.Contains(text, "limit_reached")):
		return ErrQuota
	case status == 429 || strings.Contains(text, "rate_limit"):
		return ErrRate
	default:
		return fmt.Errorf("ChatGPT 请求失败（HTTP %d）", status)
	}
}

type requirements struct{ token, proof string }

func (c *Client) requirements(ctx context.Context) (requirements, error) {
	const path = "/backend-api/sentinel/chat-requirements"
	var prepared struct {
		PrepareToken string `json:"prepare_token"`
		Proof        struct {
			Required   bool   `json:"required"`
			Seed       string `json:"seed"`
			Difficulty string `json:"difficulty"`
		} `json:"proofofwork"`
		Arkose struct {
			Required bool `json:"required"`
		} `json:"arkose"`
		Turnstile struct {
			Required bool `json:"required"`
		} `json:"turnstile"`
	}
	config := c.fp.proofConfig()
	if err := c.json(ctx, http.MethodPost, path+"/prepare", map[string]any{"p": requirementsToken(config)}, nil, &prepared); err != nil {
		return requirements{}, err
	}
	if prepared.Arkose.Required || prepared.Turnstile.Required {
		return requirements{}, ErrVerification
	}
	r := requirements{}
	if prepared.Proof.Required {
		var err error
		r.proof, err = proofToken(ctx, prepared.Proof.Seed, prepared.Proof.Difficulty, config)
		if err != nil {
			return requirements{}, err
		}
	}
	var finalized struct {
		Token string `json:"token"`
	}
	err := c.json(ctx, http.MethodPost, path+"/finalize", map[string]any{
		"prepare_token": prepared.PrepareToken, "proof_token": r.proof, "turnstile_token": "",
	}, nil, &finalized)
	if err != nil {
		return requirements{}, err
	}
	r.token = finalized.Token
	if r.token == "" {
		return requirements{}, errors.New("ChatGPT 未返回请求凭据，请重新登录后重试")
	}
	return r, nil
}
