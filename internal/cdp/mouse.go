package cdp

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/chromedp/cdproto/input"
)

func mouseApproachPoints(startX, startY, targetX, targetY float64, hasStart bool) [][2]float64 {
	if !hasStart {
		angle := rand.Float64() * 2 * math.Pi
		dist := 70 + rand.Float64()*150
		startX = math.Max(4, targetX+math.Cos(angle)*dist)
		startY = math.Max(4, targetY+math.Sin(angle)*dist)
	}
	dx := targetX - startX
	dy := targetY - startY
	dist := math.Hypot(dx, dy)
	if dist < 2 {
		return [][2]float64{{targetX, targetY}}
	}
	steps := 2 + rand.IntN(2) // 2..3 smooth Bezier approach steps
	// Slight perpendicular quadratic Bezier bow so approach is not a rigid line.
	bow := (rand.Float64()*2 - 1) * math.Min(28, dist*0.14)
	ctrlX := (startX+targetX)/2 - (dy/dist)*bow
	ctrlY := (startY+targetY)/2 + (dx/dist)*bow
	pts := make([][2]float64, 0, steps)
	for i := 1; i <= steps; i++ {
		u := float64(i) / float64(steps)
		// Ease-out deceleration near target.
		t := 1 - (1-u)*(1-u)
		if i == steps {
			pts = append(pts, [2]float64{targetX, targetY})
			continue
		}
		om := 1 - t
		x := om*om*startX + 2*om*t*ctrlX + t*t*targetX
		y := om*om*startY + 2*om*t*ctrlY + t*t*targetY
		pts = append(pts, [2]float64{math.Max(0, x), math.Max(0, y)})
	}
	return pts
}

func (s *Session) bridgeMouseClick(data string) string {
	var point struct{ X, Y *float64 }
	err := jsonUnmarshal([]byte(data), &point)
	if err == nil && (point.X == nil || point.Y == nil || *point.X < 0 || *point.Y < 0) {
		err = fmt.Errorf("点击坐标无效")
	}
	if err == nil {
		s.mu.Lock()
		startX, startY, hasStart := s.mouseX, s.mouseY, s.hasMouse
		s.mu.Unlock()
		points := mouseApproachPoints(startX, startY, *point.X, *point.Y, hasStart)
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		err = s.client.proc.Call(ctx, s.target, "Input.mouseClick", map[string]any{
			"points":     points,
			"x":          *point.X,
			"y":          *point.Y,
			"stepMs":     3 + rand.IntN(4),
			"prePressMs": 6 + rand.IntN(7),
			"holdMs":     12 + rand.IntN(11),
		}, nil)
		cancel()
		if err == nil {
			s.mu.Lock()
			s.mouseX, s.mouseY, s.hasMouse = *point.X, *point.Y, true
			s.mu.Unlock()
		}
	}
	if err != nil {
		answer, _ := jsonMarshal(map[string]any{"success": false, "error": err.Error()})
		return string(answer)
	}
	s.StayBack()
	return `{"success":true}`
}

func (s *Session) bridgeMouseMove(data string) string {
	var point struct{ X, Y *float64 }
	err := jsonUnmarshal([]byte(data), &point)
	if err == nil && (point.X == nil || point.Y == nil || *point.X < 0 || *point.Y < 0) {
		err = fmt.Errorf("悬停坐标无效")
	}
	if err == nil {
		s.mu.Lock()
		startX, startY, hasStart := s.mouseX, s.mouseY, s.hasMouse
		s.mu.Unlock()
		points := mouseApproachPoints(startX, startY, *point.X, *point.Y, hasStart)
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		for i, pt := range points {
			if i > 0 {
				if sleepErr := pauseHuman(ctx, 3*time.Millisecond, 4*time.Millisecond); sleepErr != nil {
					err = sleepErr
					break
				}
			}
			if callErr := s.client.proc.Call(ctx, s.target, "Input.dispatchMouseEvent", map[string]any{
				"type": "mouseMoved",
				"x":    pt[0],
				"y":    pt[1],
			}, nil); callErr != nil {
				err = callErr
				break
			}
		}
		cancel()
		if err == nil {
			s.mu.Lock()
			s.mouseX, s.mouseY, s.hasMouse = *point.X, *point.Y, true
			s.mu.Unlock()
		}
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
		if err := pauseHuman(ctx, 20*time.Millisecond, 35*time.Millisecond); err != nil {
			return err
		}
		up := input.DispatchKeyEvent(input.KeyUp).
			WithKey("Enter").WithCode("Enter").WithText("\r").
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
