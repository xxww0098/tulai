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
    var inDialog = false;
    var p = el.parentElement;
    for (var d = 0; d < 6 && p; d++, p = p.parentElement) {
      if (hint(p)) s += 2;
      var role = p.getAttribute && p.getAttribute('role');
      var cls = String(p.className || '');
      var pr = p.getBoundingClientRect && p.getBoundingClientRect();
      if (role === 'dialog' || /dialog|modal/i.test(cls)) inDialog = true;
      if (pr && /login/i.test(cls) && pr.width >= 200 && pr.height >= 160) inDialog = true;
      if (inDialog) s += 2;
    }
    if (s > score && s >= 3 && (hint(el) || (square(r) && inDialog))) {
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

// qrOpenJS 在还没有二维码时找出登录入口。
// 豆包是右上角「登录」。千问用同一组文案，点不开时再试 class / testid / aria 兜底。
// 弹层已经开了就改点「扫码登录」，避免再点一次「登录」把弹层关掉。
const qrOpenJS = `(function () {
  function visible(el) {
    if (!el || !el.getBoundingClientRect) return false;
    var r = el.getBoundingClientRect();
    if (r.width < 8 || r.height < 8) return false;
    var s = getComputedStyle(el);
    if (s.visibility === 'hidden' || s.display === 'none' || Number(s.opacity) === 0) return false;
    if (el.disabled || (el.getAttribute && el.getAttribute('aria-disabled') === 'true')) return false;
    return true;
  }
  function label(el) {
    var t = String(el.innerText || '').replace(/\s+/g, '');
    if (!t && el.getAttribute) t = String(el.getAttribute('aria-label') || el.getAttribute('title') || '').replace(/\s+/g, '');
    return t.slice(0, 32);
  }
  function classOf(el) {
    var cls = el.className;
    if (cls && typeof cls !== 'string') cls = cls.baseVal || '';
    return String(cls || '');
  }
  function blob(el) {
    var testid = el.getAttribute && (el.getAttribute('data-testid') || '');
    return String(el.id || '') + ' ' + classOf(el) + ' ' + String(testid || '');
  }
  function dialogOpen() {
    var keys = ['扫码登录', '二维码登录', '手机号登录', '短信登录', '验证码登录', '密码登录'];
    var nodes = document.querySelectorAll('[role="dialog"],[class*="dialog" i],[class*="modal" i],[class*="login" i]');
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (!visible(el)) continue;
      var r = el.getBoundingClientRect();
      if (r.width < 200 || r.height < 160) continue;
      var text = String(el.innerText || '').slice(0, 400);
      for (var k = 0; k < keys.length; k++) if (text.indexOf(keys[k]) >= 0) return true;
    }
    return false;
  }
  function boxOf(el, kind) {
    el.scrollIntoView({block:'center', inline:'center'});
    var r = el.getBoundingClientRect();
    return {ok:true, x:r.x, y:r.y, w:r.width, h:r.height, kind:kind};
  }
  var exact = {'登录':12, '立即登录':11, '登录/注册':11, '点击登录':10, '登入':9, '扫码登录':8};
  var dialog = dialogOpen();
  if (dialog) {
    var tabs = document.querySelectorAll('button,[role="tab"],[role="button"],a,span,div');
    var tab = null, tabScore = 0;
    for (var i = 0; i < tabs.length; i++) {
      var el = tabs[i];
      if (!visible(el)) continue;
      var t = label(el);
      if (t !== '扫码登录' && t !== '二维码登录') continue;
      if (el.getAttribute && el.getAttribute('aria-selected') === 'true') continue;
      var r = el.getBoundingClientRect();
      if (r.width > 240 || r.height > 80) continue;
      var s = 10;
      if (r.width < 160) s += 2;
      if (s > tabScore) { tabScore = s; tab = el; }
    }
    if (tab) return boxOf(tab, 'scan');
    return {ok:false, dialog:true};
  }
  var sels = [
    'button',
    '[role="button"]',
    'a',
    '[data-testid*="login" i]',
    '[data-testid*="signin" i]',
    '[class*="login-btn" i]',
    '[class*="LoginButton"]',
    '[class*="loginBtn"]',
    '[class*="header-login" i]',
    '[class*="btn-login" i]',
    '[aria-label="登录"]',
    '[aria-label="立即登录"]',
    '[title="登录"]'
  ];
  var nodes = document.querySelectorAll(sels.join(','));
  var best = null, score = 0;
  var vw = window.innerWidth || 1280;
  for (var n = 0; n < nodes.length; n++) {
    var el = nodes[n];
    if (!visible(el)) continue;
    var t = label(el);
    if (/退出|注销|登录后|已登录|注册新/.test(t)) continue;
    var s = exact[t] || 0;
    if (!s && el.getAttribute) {
      var aria = String(el.getAttribute('aria-label') || el.getAttribute('title') || '').replace(/\s+/g, '');
      if (aria === '登录' || aria === '立即登录' || aria === '点击登录' || aria === '登录/注册') s = 10;
    }
    if (!s && /login|signin|sign-in/i.test(blob(el)) && !/logout|signout/i.test(blob(el)) && t.length <= 8) s = 6;
    if (s <= 0) continue;
    var r = el.getBoundingClientRect();
    if (r.width > 320 || r.height > 120) continue;
    if (r.y < 140) s += 4;
    if (r.x > vw * 0.55) s += 3;
    var role = el.getAttribute && el.getAttribute('role');
    if (el.tagName === 'BUTTON' || role === 'button') s += 2;
    if (r.width > 180 || r.height > 64) s -= 5;
    if (s > score) { score = s; best = el; }
  }
  if (!best) return {ok:false, dialog:false};
  return boxOf(best, 'login');
})()`

// ErrLoginQR 是点了登录入口之后，限时内还是没有二维码。
// 界面把这句话直接写在账号卡片上。
var ErrLoginQR = errors.New("还没看到二维码，请在浏览器右上角点「登录」")

// LoginQRHint 把截图失败收成账号卡片上的一句中文。
func LoginQRHint(err error) string {
	if err == nil {
		return ""
	}
	return ErrLoginQR.Error()
}

const (
	qrAppearWait = 8 * time.Second
	qrPollGap    = 400 * time.Millisecond
)

// decideQR 决定这一轮是截图、刷新、点登录，还是继续等。
// opened 为真表示登录入口已经点过。到 limit 仍没有码就超时。
func decideQR(box qrBox, opened bool, elapsed, limit time.Duration) string {
	if box.OK && !box.Expired {
		return "capture"
	}
	if box.Expired {
		return "refresh"
	}
	if !opened {
		return "open"
	}
	if limit > 0 && elapsed >= limit {
		return "timeout"
	}
	return "wait"
}

// CaptureLoginQR 截登录弹层里的二维码。页面上还没有码时，先点站点的登录入口再等。
// 过期就点刷新。浏览器窗口可以留在后台。限时内没有二维码时返回 ErrLoginQR。
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
		if err := refreshLoginQR(ctx, conn, id); err != nil {
			return nil, true, err
		}
		if again, againErr := readQRBox(ctx, conn, id); againErr == nil {
			box = again
		}
	}
	if box.OK {
		return shootLoginQR(ctx, conn, id, box)
	}
	box, err = waitForLoginQR(ctx, conn, id)
	if err != nil {
		return nil, false, err
	}
	if !box.OK {
		if box.Expired {
			return nil, true, nil
		}
		return nil, false, ErrLoginQR
	}
	return shootLoginQR(ctx, conn, id, box)
}

func refreshLoginQR(ctx context.Context, conn *SliderConn, id string) error {
	var clicked struct {
		Clicked bool `json:"clicked"`
	}
	if err := conn.EvalJSON(ctx, id, qrRefreshJS, &clicked); err != nil {
		return err
	}
	if !clicked.Clicked {
		return nil
	}
	return sleepCtx(ctx, 700*time.Millisecond)
}

type loginHit struct {
	OK   bool    `json:"ok"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Kind string  `json:"kind"`
}

func waitForLoginQR(ctx context.Context, conn *SliderConn, id string) (qrBox, error) {
	start := time.Now()
	var box qrBox
	clickedLogin, clickedScan, refreshed := false, false, false
	for {
		next, err := readQRBox(ctx, conn, id)
		if err != nil {
			return box, err
		}
		box = next
		switch decideQR(box, clickedLogin || clickedScan, time.Since(start), qrAppearWait) {
		case "capture":
			return box, nil
		case "timeout":
			if box.Expired {
				return box, nil
			}
			return box, ErrLoginQR
		case "refresh":
			if !refreshed {
				if err := refreshLoginQR(ctx, conn, id); err != nil {
					return box, err
				}
				refreshed = true
			} else if box.OK || time.Since(start) >= qrAppearWait {
				return box, nil
			}
		default:
			var hit loginHit
			if err := conn.EvalJSON(ctx, id, qrOpenJS, &hit); err != nil {
				return box, err
			}
			if shouldClickLogin(hit.Kind, clickedLogin, clickedScan) {
				if err := clickLoginHit(ctx, conn, id, hit); err != nil {
					return box, err
				}
				if hit.Kind == "scan" {
					clickedScan = true
				} else {
					clickedLogin = true
				}
			} else if time.Since(start) >= qrAppearWait {
				if box.Expired {
					return box, nil
				}
				return box, ErrLoginQR
			}
		}
		if err := sleepCtx(ctx, qrPollGap); err != nil {
			if box.Expired {
				return box, nil
			}
			return box, ErrLoginQR
		}
	}
}

func shouldClickLogin(kind string, clickedLogin, clickedScan bool) bool {
	switch kind {
	case "scan":
		return !clickedScan
	case "login":
		return !clickedLogin
	default:
		return false
	}
}

func loginClickPoint(x, y, w, h float64) (float64, float64, bool) {
	if w < 8 || h < 8 {
		return 0, 0, false
	}
	return x + w/2, y + h/2, true
}

func clickLoginHit(ctx context.Context, conn *SliderConn, id string, hit loginHit) error {
	x, y, ok := loginClickPoint(hit.X, hit.Y, hit.W, hit.H)
	if !ok {
		return nil
	}
	return conn.Play(ctx, id, []SliderEvent{
		{Type: "mouseMoved", X: x, Y: y},
		{Type: "mousePressed", X: x, Y: y, Buttons: 1},
		{Type: "mouseReleased", X: x, Y: y},
	})
}

func shootLoginQR(ctx context.Context, conn *SliderConn, id string, box qrBox) ([]byte, bool, error) {
	clip, ok := loginQRClip(box.X, box.Y, box.W, box.H)
	if !box.OK || !ok {
		if box.Expired {
			return nil, true, nil
		}
		return nil, false, ErrLoginQR
	}
	png, err := captureClip(ctx, conn, id, clip)
	return png, box.Expired, err
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
