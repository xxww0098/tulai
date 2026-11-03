package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/http/httpguts"
)

// Poster is optional; site adapters may use it only for verified POST endpoints.
// Callers remain responsible for checking response size and HTTP status.
type Poster interface {
	Post(ctx context.Context, rawURL string, body []byte, headers http.Header) (*Response, error)
}

// shortcut: POST 请求体最多 1 MiB；需要较大请求时按站点重新评估上限。
const maxPostBodyBytes = 1 << 20

func postTarget(raw string) (*url.URL, error) {
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil {
		return nil, errors.New("POST 地址必须是不含账号密码的 HTTP / HTTPS 地址")
	}
	return u, nil
}

func postHeaders(headers http.Header) (http.Header, error) {
	result := make(http.Header)
	total := 0
	for name, values := range headers {
		key := http.CanonicalHeaderKey(name)
		switch key {
		case "Accept", "Content-Type", "X-Requested-With":
		default:
			return nil, fmt.Errorf("POST 不支持自定义请求头 %s", name)
		}
		if _, duplicate := result[key]; duplicate || len(values) != 1 || !httpguts.ValidHeaderFieldValue(values[0]) {
			return nil, errors.New("POST 请求头无效")
		}
		total += len(key) + len(values[0])
		if total > 8192 {
			return nil, errors.New("POST 请求头超过 8 KiB 上限")
		}
		if key == "Content-Type" && values[0] != "" {
			mediaType, _, err := mime.ParseMediaType(values[0])
			if err != nil || (mediaType != "application/json" && mediaType != "application/x-www-form-urlencoded") {
				return nil, errors.New("POST 只支持 JSON 或表单请求体")
			}
		}
		result[key] = []string{values[0]}
	}
	return result, nil
}

func (g *HTTPGetter) Post(ctx context.Context, rawURL string, body []byte, headers http.Header) (*Response, error) {
	if len(body) > maxPostBodyBytes {
		return nil, errors.New("POST 请求体超过 1 MiB 上限")
	}
	target, err := postTarget(rawURL)
	if err != nil {
		return nil, err
	}
	clean, err := postHeaders(headers)
	if err != nil {
		return nil, err
	}
	return g.post(ctx, target, body, clean)
}

func (g *HTTPGetter) post(ctx context.Context, target *url.URL, body []byte, headers http.Header) (*Response, error) {
	if g == nil || g.client == nil {
		return nil, errors.New("没有配置 POST 下载客户端")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(bytes.Clone(body)))
	if err != nil {
		return nil, err
	}
	request.Header = headers.Clone()
	if request.Header.Get("Accept") == "" {
		request.Header.Set("Accept", "application/json, text/plain, */*")
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", g.userAgent)
	client := *g.client
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, errors.New("POST 请求不允许重定向")
	}
	return &Response{StatusCode: response.StatusCode, Header: response.Header, FinalURL: response.Request.URL.String(), Body: response.Body}, nil
}

// Post sends credentials only to the exact origin of the captured browser page.
func (s *RenderedSession) Post(ctx context.Context, rawURL string, body []byte, headers http.Header) (*Response, error) {
	if len(body) > maxPostBodyBytes {
		return nil, errors.New("POST 请求体超过 1 MiB 上限")
	}
	target, err := postTarget(rawURL)
	if err != nil {
		return nil, err
	}
	clean, err := postHeaders(headers)
	if err != nil {
		return nil, err
	}
	if s == nil || s.tab == nil || s.getter == nil {
		return nil, errors.New("浏览器下载会话不可用")
	}
	if err := s.tab.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	page, pageIP, startIP := s.pageURL, s.pageIP, s.startIP
	s.mu.RUnlock()
	if page == nil || !sameOrigin(page, target) {
		return nil, errors.New("POST 只支持浏览器当前网页的同源地址")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.tab, cancel)
	cleanup := func() { stop(); cancel() }
	currentPage := func() (*url.URL, error) {
		var currentRaw string
		if err := s.run(requestCtx, chromedp.Location(&currentRaw)); err != nil {
			return nil, err
		}
		current, err := postTarget(currentRaw)
		if err != nil || !sameOrigin(page, current) || !sameOrigin(current, target) {
			return nil, errors.New("浏览器当前网页已离开 POST 的来源站点")
		}
		return current, nil
	}
	if _, err := currentPage(); err != nil {
		cleanup()
		return nil, err
	}
	cookies, err := s.cookies(requestCtx, target)
	if err != nil {
		cleanup()
		return nil, err
	}
	current, err := currentPage()
	if err != nil {
		cleanup()
		return nil, err
	}
	clean.Set("Origin", current.Scheme+"://"+current.Host)
	if ref := imageReferer(current.String(), target.String()); ref != "" {
		clean.Set("Referer", ref)
	}
	cookieRequest := &http.Request{Header: clean}
	for _, cookie := range cookies {
		cookieRequest.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	policy := safeDownloadPolicy{}
	if pageIP.IsValid() && pageIP == startIP && sameOrigin(s.start, page) && !isPublicDownloadIP(pageIP) {
		policy.allowedLocal = pageIP
	}
	hopCtx := context.WithValue(requestCtx, safeDownloadPolicyKey{}, policy)
	response, err := s.getter.post(hopCtx, target, body, clean)
	if err != nil {
		cleanup()
		return nil, err
	}
	response.Body = &renderedBody{ReadCloser: response.Body, cleanup: cleanup}
	return response, nil
}
