package cdp

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/input"
)

func (s *Session) bridgeMouseClick(data string) string {
	var point struct{ X, Y *float64 }
	err := jsonUnmarshal([]byte(data), &point)
	if err == nil && (point.X == nil || point.Y == nil || *point.X < 0 || *point.Y < 0) {
		err = fmt.Errorf("点击坐标无效")
	}
	if err == nil {
		err = s.exec(func(ctx context.Context) error {
			if err := input.DispatchMouseEvent(input.MousePressed, *point.X, *point.Y).WithButton(input.Left).WithButtons(1).WithClickCount(1).Do(ctx); err != nil {
				return err
			}
			return input.DispatchMouseEvent(input.MouseReleased, *point.X, *point.Y).WithButton(input.Left).WithClickCount(1).Do(ctx)
		})
	}
	if err != nil {
		answer, _ := jsonMarshal(map[string]any{"success": false, "error": err.Error()})
		return string(answer)
	}
	s.StayBack()
	return `{"success":true}`
}

func (s *Session) bridgeKeyEnter() string {
	s.emulateFocus()
	err := s.exec(func(ctx context.Context) error {
		down := input.DispatchKeyEvent(input.KeyDown).
			WithKey("Enter").WithCode("Enter").WithText("\r").
			WithWindowsVirtualKeyCode(13).WithNativeVirtualKeyCode(13)
		if err := down.Do(ctx); err != nil {
			return err
		}
		up := input.DispatchKeyEvent(input.KeyUp).
			WithKey("Enter").WithCode("Enter").
			WithWindowsVirtualKeyCode(13).WithNativeVirtualKeyCode(13)
		return up.Do(ctx)
	})
	if err != nil {
		answer, _ := jsonMarshal(map[string]any{"success": false, "error": err.Error()})
		return string(answer)
	}
	s.StayBack()
	return `{"success":true}`
}
