package cdp

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// PageView 只附着到已有标签；关闭查看器不会关闭网页或清除登录信息。
type PageView struct {
	conn    *SliderConn
	target  string
	session string
	pressed *PageInput
}

type PageFrame struct {
	JPEG             []byte
	Width, Height    float64
	ScrollX, ScrollY float64
	Title, URL       string
}

type PageInput struct {
	Kind, Button, Key, Text string
	X, Y, DX, DY            float64
	Modifiers, Buttons      int64
	Clicks, VirtualKey      int
}

func AttachPageView(ctx context.Context, endpoint, home string) (*PageView, error) {
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	list, err := conn.targets(ctx)
	if err != nil {
		conn.Close()
		return nil, err
	}
	id := ""
	for id == "" {
		for _, page := range list {
			if page.Type == "page" && strings.TrimPrefix(hostOf(page.URL), "www.") == strings.TrimPrefix(hostOf(home), "www.") {
				id = page.ID
				break
			}
		}
		if id != "" {
			break
		}
		select {
		case <-ctx.Done():
			conn.Close()
			return nil, fmt.Errorf("没有找到该账号的平台页面，请先在浏览器中打开")
		case <-time.After(100 * time.Millisecond):
		}
		list, err = conn.targets(ctx)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}
	session, err := conn.session(ctx, id)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return &PageView{conn: conn, target: id, session: session}, nil
}

func (v *PageView) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if v.pressed != nil {
		in := *v.pressed
		in.Kind, in.Buttons = "mouseReleased", 0
		_, _ = v.Input(ctx, in)
	}
	// 等已发出的输入处理完，再让调度恢复这个账号。
	_ = v.conn.EvalJSON(ctx, v.target, `0`, nil)
	v.conn.Close()
}

func (v *PageView) Frame(ctx context.Context) (PageFrame, error) {
	var frame PageFrame
	if err := v.conn.EvalJSON(ctx, v.target, `({Width:innerWidth,Height:innerHeight,ScrollX:scrollX,ScrollY:scrollY,Title:document.title,URL:location.href})`, &frame); err != nil {
		return frame, err
	}
	var out struct{ Data string }
	err := v.conn.call(ctx, v.session, "Page.captureScreenshot", map[string]any{
		"format": "jpeg", "quality": 80, "captureBeyondViewport": false,
		"clip": map[string]any{"x": frame.ScrollX, "y": frame.ScrollY, "width": frame.Width, "height": frame.Height, "scale": 1},
	}, &out)
	if err != nil {
		return frame, err
	}
	frame.JPEG, err = base64.StdEncoding.DecodeString(out.Data)
	return frame, err
}

func (v *PageView) LoginState(ctx context.Context) (string, error) {
	var result loginProbeResult
	err := v.conn.EvalJSON(ctx, v.target, loginProbeJS, &result)
	return NormalizeLoginState(result.State), err
}

// Input 返回复制或剪切的文字，其它操作返回空串。文字作为协议参数发送，不拼成脚本。
func (v *PageView) Input(ctx context.Context, in PageInput) (string, error) {
	method := ""
	var params map[string]any
	switch in.Kind {
	case "mousePressed", "mouseReleased", "mouseMoved", "mouseWheel":
		method = "Input.dispatchMouseEvent"
		params = map[string]any{"type": in.Kind, "x": in.X, "y": in.Y, "modifiers": in.Modifiers, "buttons": in.Buttons}
		if in.Button != "" {
			params["button"] = in.Button
		}
		if in.Kind == "mouseWheel" {
			params["deltaX"], params["deltaY"] = in.DX, in.DY
		} else if in.Kind != "mouseMoved" {
			params["clickCount"] = max(in.Clicks, 1)
		}
	case "keyDown", "keyUp":
		method = "Input.dispatchKeyEvent"
		params = map[string]any{"type": in.Kind, "key": in.Key, "windowsVirtualKeyCode": in.VirtualKey, "modifiers": in.Modifiers}
	case "text":
		method, params = "Input.insertText", map[string]any{"text": in.Text}
	case "reload":
		method = "Page.reload"
	case "copy", "cut":
		var result struct{ Text string }
		err := v.conn.EvalJSON(ctx, v.target, `(()=>{const e=document.activeElement;return {text:e&&e.type==='password'?'':e&&typeof e.selectionStart==='number'?e.value.slice(e.selectionStart,e.selectionEnd):String(getSelection()||'')}})()`, &result)
		if err == nil && in.Kind == "cut" && result.Text != "" {
			err = v.conn.EvalJSON(ctx, v.target, `document.execCommand('delete')`, nil)
		}
		return result.Text, err
	case "selectAll", "undo", "redo":
		return "", v.conn.EvalJSON(ctx, v.target, `document.execCommand('`+in.Kind+`')`, nil)
	default:
		return "", fmt.Errorf("不支持的网页操作")
	}
	err := v.conn.call(ctx, v.session, method, params, nil)
	if err == nil {
		if in.Kind == "mousePressed" {
			v.pressed = &in
		} else if in.Kind == "mouseReleased" {
			v.pressed = nil
		}
	}
	return "", err
}
