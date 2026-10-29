package cdp

import (
	"context"
	"encoding/base64"
	"errors"
	"time"
)

// qrFindJS 在登录弹层里找二维码，返回相对视口的 CSS 像素。
// 豆包和千问的节点类名不固定，所以认 qr / 二维码，以及弹层里接近正方形的图。
const qrFindJS = `(function () {
  function visible(el) {
    if (!el || !el.getBoundingClientRect) return false;
    var r = el.getBoundingClientRect();
    if (r.width < 48 || r.height < 48) return false;
    var s = getComputedStyle(el);
    if (s.visibility === 'hidden' || s.display === 'none' || Number(s.opacity) === 0) return false;
    return true;
  }
  function hint(el) {
    if (!el) return false;
    var blob = String(el.id || '') + ' ' + String(el.className || '') + ' ' +
      String((el.getAttribute && (el.getAttribute('alt') || el.getAttribute('aria-label') || el.getAttribute('src'))) || '');
    return /qr|qrcode|scan|二维码/i.test(blob);
  }
  function square(r) {
    var m = Math.max(r.width, r.height);
    return m >= 80 && m <= 460 && Math.abs(r.width - r.height) / m < 0.28;
  }
  var expired = /二维码已过期|二维码已失效|点击刷新|刷新二维码/.test(String((document.body && document.body.innerText) || '').slice(0, 2500));
  var nodes = document.querySelectorAll('canvas,img,svg,[class*="qr"],[class*="QR"],[id*="qr"]');
  var best = null, score = -1;
  for (var i = 0; i < nodes.length; i++) {
    var el = nodes[i];
    if (!visible(el)) continue;
    var r = el.getBoundingClientRect();
    var s = 0;
    if (hint(el)) s += 5;
    if (square(r)) s += 3;
    var p = el.parentElement;
    for (var d = 0; d < 6 && p; d++, p = p.parentElement) {
      if (hint(p)) s += 2;
      var role = p.getAttribute && p.getAttribute('role');
      var cls = String(p.className || '');
      if (role === 'dialog' || /dialog|modal|login/i.test(cls)) s += 2;
    }
    if (s > score && (hint(el) || square(r)) && s >= 3) {
      score = s;
      best = el;
    }
  }
  if (!best) return {ok:false, expired:expired};
  best.scrollIntoView({block:'center', inline:'center'});
  var box = best.getBoundingClientRect();
  return {ok:true, x:box.x, y:box.y, w:box.width, h:box.height, expired:expired};
})()`

// qrRefreshJS 点掉「二维码已失效」上的刷新。只点短文案，避免点到整页对话框。
const qrRefreshJS = `(function () {
  var nodes = document.querySelectorAll('button,[role="button"],div,span,a');
  for (var i = 0; i < nodes.length; i++) {
    var t = String(nodes[i].innerText || '').replace(/\s+/g, '');
    if (!t || t.length > 16) continue;
    if (t === '点击刷新' || t === '刷新' || t === '刷新二维码' || t === '重新获取' || /二维码已(过期|失效)/.test(t)) {
      nodes[i].click();
      return {clicked:true};
    }
  }
  return {clicked:false};
})()`

type qrBox struct {
	OK      bool    `json:"ok"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	Expired bool    `json:"expired"`
}

// CaptureLoginQR 截登录弹层里的二维码。过期时先点刷新再截一次。
// 浏览器窗口可以留在后台。没有二维码时 png 为空。
func CaptureLoginQR(ctx context.Context, endpoint string) (png []byte, expired bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return nil, false, err
	}
	id, ok := pickLoginTarget(targets)
	if !ok {
		return nil, false, errors.New("没有登录页")
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return nil, false, errors.New("地区限制")
	}
	box, err := readQRBox(ctx, conn, id)
	if err != nil {
		return nil, false, err
	}
	if box.Expired {
		expired = true
		var clicked struct {
			Clicked bool `json:"clicked"`
		}
		if err := conn.EvalJSON(ctx, id, qrRefreshJS, &clicked); err == nil && clicked.Clicked {
			if err := sleepCtx(ctx, 700*time.Millisecond); err != nil {
				return nil, true, err
			}
			if again, againErr := readQRBox(ctx, conn, id); againErr == nil {
				box = again
				expired = again.Expired
			}
		}
	}
	clip, ok := loginQRClip(box.X, box.Y, box.W, box.H)
	if !box.OK || !ok {
		if expired {
			return nil, true, nil
		}
		return nil, false, errors.New("没有二维码")
	}
	png, err = captureClip(ctx, conn, id, clip)
	return png, expired, err
}

func readQRBox(ctx context.Context, conn *SliderConn, id string) (qrBox, error) {
	var box qrBox
	err := conn.EvalJSON(ctx, id, qrFindJS, &box)
	return box, err
}

func loginQRClip(x, y, w, h float64) (map[string]any, bool) {
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if w < 40 || h < 40 {
		return nil, false
	}
	return map[string]any{
		"x": x, "y": y, "width": w, "height": h, "scale": 1,
	}, true
}

func captureClip(ctx context.Context, conn *SliderConn, id string, clip map[string]any) ([]byte, error) {
	session, err := conn.session(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = conn.call(ctx, session, "Page.enable", map[string]any{}, nil)
	var out struct {
		Data string `json:"data"`
	}
	if err := conn.call(ctx, session, "Page.captureScreenshot", map[string]any{
		"format":                "png",
		"clip":                  clip,
		"captureBeyondViewport": true,
	}, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Data)
}
