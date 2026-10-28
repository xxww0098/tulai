package cdp

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

//go:embed slider_probe.js
var sliderProbeSource string

// SliderRect 是 CSS 像素，相对即将接收鼠标事件的那个目标的视口。
type SliderRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// SliderImage 是拼图背景或拼图块。PNG 来自页面 canvas，跨域画布失败时只留 Src。
type SliderImage struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	NaturalW int     `json:"naturalW"`
	NaturalH int     `json:"naturalH"`
	PNG      []byte  `json:"-"`
	Src      string  `json:"src"`
}

// SliderHit 是当前页面上的一块阿里云滑块。TargetID 只用于在同一次连接里发鼠标事件。
type SliderHit struct {
	TargetID  string
	Text      string
	Handle    SliderRect
	Track     SliderRect
	PieceLeft float64
	Bg        *SliderImage
	Piece     *SliderImage
}

// SliderEvent 是一次 Input.dispatchMouseEvent。Wait 在发送前等待。
type SliderEvent struct {
	Type    string
	X       float64
	Y       float64
	Buttons int64
	Wait    time.Duration
}

// SliderLook 是一次探测的结果。
type SliderLook struct {
	Hit    SliderHit
	Found  bool
	Passed bool
}

// SliderConn 附着在已经打开远程调试的 Chrome 上。关闭时只 detach，不关掉用户的标签页。
type SliderConn struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[int64]chan cdpMsg
	next    atomic.Int64
	closed  chan struct{}
	fail    error
	sess    map[string]string
}

type cdpMsg struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type sliderProbe struct {
	Found     bool          `json:"found"`
	Passed    bool          `json:"passed"`
	Text      string        `json:"text"`
	Handle    SliderRect    `json:"handle"`
	Track     SliderRect    `json:"track"`
	PieceLeft float64       `json:"pieceLeft"`
	Bg        *sliderImage  `json:"bg"`
	Piece     *sliderImage  `json:"piece"`
	Iframes   []sliderFrame `json:"iframes"`
}

type sliderImage struct {
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	W        float64 `json:"w"`
	H        float64 `json:"h"`
	NaturalW int     `json:"naturalW"`
	NaturalH int     `json:"naturalH"`
	B64      string  `json:"b64"`
	Src      string  `json:"src"`
}

type sliderFrame struct {
	Src string  `json:"src"`
	X   float64 `json:"x"`
	Y   float64 `json:"y"`
	W   float64 `json:"w"`
	H   float64 `json:"h"`
}

type sliderTarget struct {
	ID   string
	Type string
	URL  string
}

// DialSlider 连接到浏览器级 CDP WebSocket。endpoint 可以是 http://127.0.0.1:9222 或 ws://...。
func DialSlider(ctx context.Context, endpoint string) (*SliderConn, error) {
	wsURL, err := browserWebsocket(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	origin := "http://127.0.0.1"
	if parsed, err := url.Parse(wsURL); err == nil && parsed.Host != "" {
		origin = "http://" + parsed.Host
	}
	dialCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	dialer := ws.Dialer{Timeout: 1500 * time.Millisecond, Header: ws.HandshakeHeaderHTTP(http.Header{
		"Origin": []string{origin},
	})}
	conn, br, _, err := dialer.Dial(dialCtx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("连接 CDP: %w", err)
	}
	reader := br
	if reader == nil {
		reader = bufio.NewReader(conn)
	}
	c := &SliderConn{
		conn:    conn,
		reader:  reader,
		pending: map[int64]chan cdpMsg{},
		closed:  make(chan struct{}),
		sess:    map[string]string{},
	}
	go c.readLoop()
	return c, nil
}

// Close 断开调试连接，不关闭页面。
func (c *SliderConn) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return
	default:
		close(c.closed)
	}
	c.mu.Unlock()
	_ = c.conn.Close()
}

// Look 在已打开的页面和子 frame 里找滑块。坐标换算到接收事件的目标视口。
func (c *SliderConn) Look(ctx context.Context) (SliderLook, error) {
	targets, err := c.targets(ctx)
	if err != nil {
		return SliderLook{}, err
	}
	var pages []sliderPage
	type other struct {
		id    string
		url   string
		probe sliderProbe
	}
	var others []other
	for _, t := range targets {
		if t.Type != "page" && t.Type != "iframe" && t.Type != "other" {
			continue
		}
		probe, err := c.probeTarget(ctx, t.ID)
		if err != nil {
			continue
		}
		if t.Type == "page" {
			pages = append(pages, sliderPage{id: t.ID, probe: probe})
			continue
		}
		others = append(others, other{id: t.ID, url: t.URL, probe: probe})
	}
	for _, item := range pages {
		if item.probe.Passed {
			return SliderLook{Passed: true}, nil
		}
		if item.probe.Found {
			return SliderLook{Found: true, Hit: hitFromProbe(item.id, item.probe)}, nil
		}
	}
	for _, item := range others {
		if item.probe.Passed {
			return SliderLook{Passed: true}, nil
		}
		if !item.probe.Found {
			continue
		}
		if pageID, ox, oy, ok := parentFrameOffset(item.url, pages); ok {
			shiftProbe(&item.probe, ox, oy)
			return SliderLook{Found: true, Hit: hitFromProbe(pageID, item.probe)}, nil
		}
		return SliderLook{Found: true, Hit: hitFromProbe(item.id, item.probe)}, nil
	}
	// 跨域 punish / captcha iframe 常常没有独立 CDP target，只能从父页 frame tree 建隔离世界再探测。
	for _, item := range pages {
		look, err := c.lookInChildFrames(ctx, item)
		if err != nil {
			continue
		}
		if look.Found || look.Passed {
			return look, nil
		}
	}
	return SliderLook{}, nil
}

// Play 在指定目标上按顺序发送信任的鼠标事件。
func (c *SliderConn) Play(ctx context.Context, targetID string, events []SliderEvent) error {
	session, err := c.session(ctx, targetID)
	if err != nil {
		return err
	}
	for _, ev := range events {
		if ev.Wait > 0 {
			timer := time.NewTimer(ev.Wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		click := int64(0)
		button := "none"
		if ev.Type == "mousePressed" || ev.Type == "mouseReleased" {
			click = 1
			button = "left"
		}
		if ev.Type == "mouseMoved" && ev.Buttons != 0 {
			button = "left"
		}
		params := map[string]any{
			"type":        ev.Type,
			"x":           ev.X,
			"y":           ev.Y,
			"button":      button,
			"buttons":     ev.Buttons,
			"clickCount":  click,
			"pointerType": "mouse",
		}
		if err := c.call(ctx, session, "Input.dispatchMouseEvent", params, nil); err != nil {
			return err
		}
	}
	return nil
}

// EvalJSON 在目标页主世界执行 expression，把返回值解到 dest。
func (c *SliderConn) EvalJSON(ctx context.Context, targetID, expression string, dest any) error {
	session, err := c.session(ctx, targetID)
	if err != nil {
		return err
	}
	var eval struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
	}
	if err := c.call(ctx, session, "Runtime.evaluate", params, &eval); err != nil {
		return err
	}
	if len(eval.ExceptionDetails) > 0 && string(eval.ExceptionDetails) != "null" {
		return fmt.Errorf("页面脚本异常")
	}
	raw := eval.Result.Value
	if eval.Result.Type == "string" {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		raw = []byte(s)
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(raw, dest)
}

func (c *SliderConn) probeTarget(ctx context.Context, id string) (sliderProbe, error) {
	session, err := c.session(ctx, id)
	if err != nil {
		return sliderProbe{}, err
	}
	var eval struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	expr := "(" + sliderProbeSource + ")()"
	params := map[string]any{
		"expression":    expr,
		"returnByValue": true,
	}
	if err := c.call(ctx, session, "Runtime.evaluate", params, &eval); err != nil {
		return sliderProbe{}, err
	}
	if len(eval.ExceptionDetails) > 0 && string(eval.ExceptionDetails) != "null" {
		return sliderProbe{}, fmt.Errorf("探测脚本异常")
	}
	raw := eval.Result.Value
	if eval.Result.Type == "string" {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return sliderProbe{}, err
		}
		raw = []byte(s)
	}
	var probe sliderProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return sliderProbe{}, err
	}
	return probe, nil
}

func hitFromProbe(targetID string, probe sliderProbe) SliderHit {
	return SliderHit{
		TargetID:  targetID,
		Text:      probe.Text,
		Handle:    probe.Handle,
		Track:     probe.Track,
		PieceLeft: probe.PieceLeft,
		Bg:        imageFromProbe(probe.Bg),
		Piece:     imageFromProbe(probe.Piece),
	}
}

func imageFromProbe(in *sliderImage) *SliderImage {
	if in == nil {
		return nil
	}
	out := &SliderImage{
		X: in.X, Y: in.Y, W: in.W, H: in.H,
		NaturalW: in.NaturalW, NaturalH: in.NaturalH, Src: in.Src,
	}
	if in.B64 != "" {
		if raw, err := base64.StdEncoding.DecodeString(in.B64); err == nil {
			out.PNG = raw
		}
	}
	return out
}

func shiftProbe(probe *sliderProbe, ox, oy float64) {
	probe.Handle.X += ox
	probe.Handle.Y += oy
	probe.Track.X += ox
	probe.Track.Y += oy
	if probe.Bg != nil {
		probe.Bg.X += ox
		probe.Bg.Y += oy
	}
	if probe.Piece != nil {
		probe.Piece.X += ox
		probe.Piece.Y += oy
	}
}

func (c *SliderConn) targets(ctx context.Context) ([]sliderTarget, error) {
	_ = c.call(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true}, nil)
	var out struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := c.call(ctx, "", "Target.getTargets", map[string]any{}, &out); err != nil {
		return nil, err
	}
	list := make([]sliderTarget, 0, len(out.TargetInfos))
	for _, info := range out.TargetInfos {
		list = append(list, sliderTarget{ID: info.TargetID, Type: info.Type, URL: info.URL})
	}
	return list, nil
}

func (c *SliderConn) session(ctx context.Context, targetID string) (string, error) {
	c.mu.Lock()
	if id, ok := c.sess[targetID]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &out); err != nil {
		return "", err
	}
	if out.SessionID == "" {
		return "", fmt.Errorf("没有 CDP 会话")
	}
	c.mu.Lock()
	c.sess[targetID] = out.SessionID
	c.mu.Unlock()
	return out.SessionID, nil
}

// Activate 把已经打开的标签拉到前面，不创建新页。
func (c *SliderConn) Activate(ctx context.Context, targetID string) error {
	if c == nil || targetID == "" {
		return fmt.Errorf("没有可前置的页面")
	}
	return c.call(ctx, "", "Target.activateTarget", map[string]any{"targetId": targetID}, nil)
}

func (c *SliderConn) call(ctx context.Context, session, method string, params, dest any) error {
	select {
	case <-c.closed:
		return fmt.Errorf("CDP 已断开")
	default:
	}
	id := c.next.Add(1)
	ch := make(chan cdpMsg, 1)
	c.mu.Lock()
	if c.fail != nil {
		err := c.fail
		c.mu.Unlock()
		return err
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	body := map[string]any{"id": id, "method": method}
	if session != "" {
		body["sessionId"] = session
	}
	if params != nil {
		body["params"] = params
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err = wsutil.WriteClientMessage(c.conn, ws.OpText, raw)
	c.writeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return fmt.Errorf("CDP 已断开")
	case msg := <-ch:
		if msg.Error != nil {
			return fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		if dest != nil && len(msg.Result) > 0 && string(msg.Result) != "null" {
			if err := json.Unmarshal(msg.Result, dest); err != nil {
				return err
			}
		}
		return nil
	}
}

func (c *SliderConn) readLoop() {
	rw := sliderRW{r: c.reader, c: c}
	for {
		payload, err := wsutil.ReadServerText(rw)
		if err != nil {
			c.failAll(err)
			return
		}
		var msg cdpMsg
		if err := json.Unmarshal(payload, &msg); err != nil || msg.ID == 0 {
			continue
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

func (c *SliderConn) failAll(err error) {
	c.mu.Lock()
	if c.fail == nil {
		c.fail = err
	}
	c.mu.Unlock()
	select {
	case <-c.closed:
	default:
		_ = c.conn.Close()
	}
}

type sliderRW struct {
	r io.Reader
	c *SliderConn
}

func (s sliderRW) Read(p []byte) (int, error) { return s.r.Read(p) }

func (s sliderRW) Write(p []byte) (int, error) {
	s.c.writeMu.Lock()
	defer s.c.writeMu.Unlock()
	return s.c.conn.Write(p)
}

func browserWebsocket(ctx context.Context, endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("缺少 CDP 地址")
	}
	if strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://") {
		return endpoint, nil
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	endpoint = strings.TrimRight(endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/json/version", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	if body.WS == "" {
		return "", fmt.Errorf("CDP 没有 webSocketDebuggerUrl")
	}
	return body.WS, nil
}

func (c *SliderConn) lookInChildFrames(ctx context.Context, page sliderPage) (SliderLook, error) {
	session, err := c.session(ctx, page.id)
	if err != nil {
		return SliderLook{}, err
	}
	var tree struct {
		FrameTree frameTreeNode `json:"frameTree"`
	}
	if err := c.call(ctx, session, "Page.getFrameTree", map[string]any{}, &tree); err != nil {
		return SliderLook{}, err
	}
	for _, fr := range flattenFrameTree(tree.FrameTree) {
		if fr.ID == "" || !captchaFrameURL(fr.URL) {
			continue
		}
		var world struct {
			ExecutionContextID int `json:"executionContextId"`
		}
		if err := c.call(ctx, session, "Page.createIsolatedWorld", map[string]any{
			"frameId":              fr.ID,
			"worldName":            "tulai-baxia",
			"grantUniversalAccess": true,
		}, &world); err != nil || world.ExecutionContextID == 0 {
			continue
		}
		probe, err := c.probeContext(ctx, session, world.ExecutionContextID)
		if err != nil {
			continue
		}
		if probe.Passed {
			return SliderLook{Passed: true}, nil
		}
		if !probe.Found {
			continue
		}
		ox, oy := childFrameOffset(fr.URL, page)
		shiftProbe(&probe, ox, oy)
		return SliderLook{Found: true, Hit: hitFromProbe(page.id, probe)}, nil
	}
	return SliderLook{}, nil
}

type frameTreeNode struct {
	Frame       frameInfo       `json:"frame"`
	ChildFrames []frameTreeNode `json:"childFrames"`
}

type frameInfo struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func flattenFrameTree(root frameTreeNode) []frameInfo {
	out := []frameInfo{}
	var walk func(frameTreeNode)
	walk = func(n frameTreeNode) {
		if n.Frame.ID != "" {
			out = append(out, n.Frame)
		}
		for _, c := range n.ChildFrames {
			walk(c)
		}
	}
	walk(root)
	return out
}

func captchaFrameURL(raw string) bool {
	s := strings.ToLower(raw)
	return strings.Contains(s, "punish") ||
		strings.Contains(s, "_____tmd_____") ||
		strings.Contains(s, "nocaptcha") ||
		strings.Contains(s, "baxia") ||
		strings.Contains(s, "captcha") ||
		strings.Contains(s, "aliyun")
}

func childFrameOffset(frameURL string, page sliderPage) (float64, float64) {
	if _, x, y, ok := parentFrameOffset(frameURL, []sliderPage{page}); ok {
		return x, y
	}
	for _, listed := range page.probe.Iframes {
		if frameURLMatch(frameURL, listed.Src) || captchaFrameURL(listed.Src) {
			return listed.X, listed.Y
		}
	}
	if len(page.probe.Iframes) == 1 {
		return page.probe.Iframes[0].X, page.probe.Iframes[0].Y
	}
	return 0, 0
}

func (c *SliderConn) probeContext(ctx context.Context, session string, contextID int) (sliderProbe, error) {
	var eval struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	expr := "(" + sliderProbeSource + ")()"
	params := map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"contextId":     contextID,
	}
	if err := c.call(ctx, session, "Runtime.evaluate", params, &eval); err != nil {
		return sliderProbe{}, err
	}
	if len(eval.ExceptionDetails) > 0 && string(eval.ExceptionDetails) != "null" {
		return sliderProbe{}, fmt.Errorf("探测脚本异常")
	}
	raw := eval.Result.Value
	if eval.Result.Type == "string" {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return sliderProbe{}, err
		}
		raw = []byte(s)
	}
	var probe sliderProbe
	if err := json.Unmarshal(raw, &probe); err != nil {
		return sliderProbe{}, err
	}
	return probe, nil
}

// 下面这个函数补上 Look 里跨域 iframe 的偏移。pages 用具体类型，避免和 Look 的局部类型缠在一起。
type sliderPage struct {
	id    string
	probe sliderProbe
}

func parentFrameOffset(rawURL string, pages []sliderPage) (string, float64, float64, bool) {
	if len(pages) == 0 {
		return "", 0, 0, false
	}
	for _, page := range pages {
		for _, fr := range page.probe.Iframes {
			if frameURLMatch(rawURL, fr.Src) {
				return page.id, fr.X, fr.Y, true
			}
		}
	}
	if len(pages) == 1 && len(pages[0].probe.Iframes) == 1 {
		fr := pages[0].probe.Iframes[0]
		return pages[0].id, fr.X, fr.Y, true
	}
	return "", 0, 0, false
}

func frameURLMatch(targetURL, src string) bool {
	if src == "" || targetURL == "" {
		return false
	}
	if src == targetURL {
		return true
	}
	a, errA := url.Parse(targetURL)
	b, errB := url.Parse(src)
	if errA != nil || errB != nil {
		return strings.Contains(targetURL, src) || strings.Contains(src, targetURL)
	}
	return a.Host == b.Host && a.Path == b.Path && a.Path != ""
}
