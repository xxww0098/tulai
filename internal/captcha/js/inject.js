// tulai 验证码自动求解注入脚本（自研实现）
// 能力：验证窗口检测（九宫格/滑块）→ 截图 → 桥调 Go 求解 → 拟人拖拽 → 提交 → 重试/换题；
//      登录弹窗自动关闭、下载客户端弹窗关闭、阻塞状态上报。
(function () {
  if (window.__tulaiCaptchaInstalled) return;
  window.__tulaiCaptchaInstalled = true;
  window.__tulaiCaptchaRevision = '__TULAI_CAPTCHA_REVISION__';

  var CFG = {
    pollMs: 1000,     // 主轮询间隔
    maxRetries: 5,    // 失败重试上限（超限换题继续）
    reportMs: 1000,   // 阻塞状态上报间隔
  };
  var st = { running: false, retries: 0, manual: false, loginMode: false, kind: '' };

  function autoSolveEnabled() { return window.__tulaiCaptchaEnabled === true && !st.manual; }

  function log(msg) {
    console.log('[验证码] ' + msg);
    try { window.tulaiCall && window.tulaiCall('captchaLog', msg); } catch (e) {}
  }
  function sleep(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
  function rnd(a, b) { return a + Math.random() * (b - a); }
  function visible(el) {
    if (!el) return false;
    var s = getComputedStyle(el);
    if (s.display === 'none' || s.visibility === 'hidden' || s.opacity === '0') return false;
    var r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  }
  function cls(el) { return ((el && el.className) || '').toString(); }

  // ---------------- 拟人拖拽轨迹（6 种风格） ----------------
  function trackEaseOut(sx, sy, ex, ey) {
    var n = 30 + Math.floor(Math.random() * 50), pts = [];
    for (var i = 0; i < n; i++) {
      var t = i / (n - 1), e = 1 - Math.pow(1 - t, 3);
      var ns = 3 * (1 - t) + 0.5 * t;
      pts.push(p(sx + (ex - sx) * e + rnd(-ns, ns), sy + (ey - sy) * e + rnd(-ns, ns), 5 + rnd(0, 8) + t * rnd(0, 9)));
    }
    return pts;
  }
  function trackEaseInOut(sx, sy, ex, ey) {
    var n = 35 + Math.floor(Math.random() * 45), pts = [];
    for (var i = 0; i < n; i++) {
      var t = i / (n - 1), e = t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
      var ns = 3 * Math.sin(t * Math.PI) + 0.3;
      var sp = t < 0.5 ? 1 + t * 2 : 3 - t * 2;
      pts.push(p(sx + (ex - sx) * e + rnd(-ns, ns), sy + (ey - sy) * e + rnd(-ns, ns), Math.max(3, 15 / sp + rnd(0, 4))));
    }
    return pts;
  }
  function trackOvershoot(sx, sy, ex, ey) {
    var pts = [], ang = Math.atan2(ey - sy, ex - sx), od = 6 + rnd(0, 15);
    var ox = ex + Math.cos(ang) * od, oy = ey + Math.sin(ang) * od;
    var s1 = 20 + Math.floor(Math.random() * 15);
    for (var i = 0; i < s1; i++) {
      var t = (i / (s1 - 1)) * 0.7, e = 1 - Math.pow(1 - t, 2.5);
      pts.push(p(sx + (ox - sx) * e + rnd(-1.5, 1.5), sy + (oy - sy) * e + rnd(-1.5, 1.5), 4 + rnd(0, 8)));
    }
    pts.push(p(ox, oy, 20 + rnd(0, 60)));
    var s2 = 10 + Math.floor(Math.random() * 10);
    for (var j = 0; j < s2; j++) {
      var t2 = j / (s2 - 1);
      pts.push(p(ox + (ex - ox) * t2 * t2 + rnd(-1, 1), oy + (ey - oy) * t2 * t2 + rnd(-1, 1), 8 + rnd(0, 12)));
    }
    return pts;
  }
  function trackArc(sx, sy, ex, ey) {
    var n = 35 + Math.floor(Math.random() * 40), pts = [];
    var dx = ex - sx, dy = ey - sy, len = Math.sqrt(dx * dx + dy * dy) || 1;
    var off = rnd(-0.175, 0.175) * len;
    var cx = (sx + ex) / 2 - dy / len * off, cy = (sy + ey) / 2 + dx / len * off;
    for (var i = 0; i < n; i++) {
      var t = i / (n - 1), mt = 1 - t;
      var bx = mt * mt * sx + 2 * mt * t * cx + t * t * ex;
      var by = mt * mt * sy + 2 * mt * t * cy + t * t * ey;
      var ns = 2.5 * Math.sin(t * Math.PI) + 0.5;
      pts.push(p(bx + rnd(-ns, ns), by + rnd(-ns, ns), 5 + rnd(0, 7) + t * rnd(0, 7)));
    }
    return pts;
  }
  function trackStutter(sx, sy, ex, ey) {
    var n = 30 + Math.floor(Math.random() * 40), pts = [];
    var pauseAt = Math.floor(n * rnd(0.4, 0.7)), pauseMs = 60 + rnd(0, 160);
    for (var i = 0; i < n; i++) {
      if (i === pauseAt && pts.length) pts.push(p(pts[pts.length - 1].x, pts[pts.length - 1].y, pauseMs));
      var t = i / (n - 1), e = 1 - Math.pow(1 - t, 3), ns = 3 * (1 - t) + 0.5 * t;
      pts.push(p(sx + (ex - sx) * e + rnd(-ns, ns), sy + (ey - sy) * e + rnd(-ns, ns), 5 + rnd(0, 7) + t * rnd(0, 8)));
    }
    return pts;
  }
  function trackJittery(sx, sy, ex, ey) {
    var n = 45 + Math.floor(Math.random() * 50), pts = [];
    for (var i = 0; i < n; i++) {
      var t = i / (n - 1);
      var warp = Math.max(0, Math.min(1, t + Math.sin(t * Math.PI * 3) * 0.03 * Math.random()));
      var e = 1 - Math.pow(1 - warp, 3), j = 1 + Math.random() * 3;
      pts.push(p(sx + (ex - sx) * e + rnd(-j, j), sy + (ey - sy) * e + rnd(-j, j),
        3 + rnd(0, 5) + (Math.random() > 0.85 ? rnd(0, 20) : 0)));
    }
    return pts;
  }
  var STYLES = [trackEaseOut, trackEaseInOut, trackOvershoot, trackArc, trackStutter, trackJittery];
  function p(x, y, delay) { return { x: Math.round(x * 10) / 10, y: Math.round(y * 10) / 10, delay: delay }; }

  function fire(el, Ctor, type, x, y, opts) {
    try {
      var ev = new Ctor(type, Object.assign({
        bubbles: true, cancelable: true, composed: true,
        clientX: x, clientY: y, button: 0,
      }, opts || {}));
      el.dispatchEvent(ev);
    } catch (e) {}
  }
  function pointerTo(el, type, x, y) {
    fire(el, window.PointerEvent || MouseEvent, type, x, y, { pointerId: 1, pointerType: 'mouse', isPrimary: true });
  }

  async function humanDrag(sx, sy, ex, ey) {
    if (!autoSolveEnabled()) return;
    await sleep(50 + rnd(0, 250)); // 起手延迟
    if (!autoSolveEnabled()) return;
    var make = STYLES[Math.floor(Math.random() * STYLES.length)];
    var pts = make(sx, sy, ex, ey);
    var el = document.elementFromPoint(sx, sy) || document.body;
    pointerTo(el, 'pointerdown', sx, sy);
    fire(el, MouseEvent, 'mousedown', sx, sy);
    for (var i = 0; i < pts.length; i++) {
      var pt = pts[i];
      await sleep(pt.delay);
      if (!autoSolveEnabled()) return;
      var t = document.elementFromPoint(pt.x, pt.y) || document.body;
      pointerTo(t, 'pointermove', pt.x, pt.y);
      fire(t, MouseEvent, 'mousemove', pt.x, pt.y);
    }
    await sleep(30 + rnd(0, 50));
    if (!autoSolveEnabled()) return;
    // 终点小幅抖动后落定
    for (var j = 0; j < 1 + Math.floor(Math.random() * 3); j++) {
      var jx = ex + rnd(-2, 2), jy = ey + rnd(-2, 2);
      await sleep(6 + rnd(0, 10));
      if (!autoSolveEnabled()) return;
      var t2 = document.elementFromPoint(jx, jy) || document.body;
      pointerTo(t2, 'pointermove', jx, jy);
      fire(t2, MouseEvent, 'mousemove', jx, jy);
    }
    var end = document.elementFromPoint(ex, ey) || document.body;
    pointerTo(end, 'pointerup', ex, ey);
    fire(end, MouseEvent, 'mouseup', ex, ey);
  }

  // ---------------- 验证窗口检测 ----------------
  var CAPTCHA_SEL = [
    '#captcha_verify_image',
    'iframe[src*="captcha"]', 'iframe[src*="verify"]', 'iframe[src*="geetest"]', 'iframe[src*="turing"]',
    'div[class*="dialog"]', 'div[class*="modal"]', 'div[class*="popup"]',
    'div[class*="overlay"] canvas', '[class*="captcha"] canvas', '[class*="verify"] canvas',
    '[class*="puzzle"]', 'div[class*="mask"] canvas', 'div[class*="pop"] canvas',
    'div[class*="captcha"]', 'div[class*="verify"]', 'div[class*="geetest"]', 'div[class*="verification"]',
  ];
  var CAPTCHA_TEXT = /人机验证|安全验证|滑动验证|滑块验证|请完成.{0,30}验证|请(?:输入|填写)(?:下方|图中|图片|图形)?验证码|完成下方验证|(?:complete|solve|enter).{0,20}\bcaptcha\b|verify (?:that )?you are human|human verification|security verification/i;

  function captchaEvidence(el) {
    var identity = cls(el) + ' ' + (el.id || '') + ' ' + (el.getAttribute('data-testid') || '');
    if (/captcha|geetest|yidun|turing|verification/i.test(identity)) return true;
    if (el.tagName === 'IFRAME') return /captcha|verify|geetest|turing/i.test(el.getAttribute('src') || '');
    return CAPTCHA_TEXT.test((el.innerText || '').slice(0, 1000));
  }

  function detectCaptcha() {
    for (var i = 0; i < CAPTCHA_SEL.length; i++) {
      try {
        var candidates = document.querySelectorAll(CAPTCHA_SEL[i]);
        for (var j = 0; j < candidates.length; j++) {
          var el = candidates[j];
          if (!visible(el)) continue;
          if (el.tagName === 'CANVAS') el = el.parentElement;
          if (!el || el === document.body || !captchaEvidence(el)) continue;
          var c = cls(el);
          if (/(?:^|\s)(close|btn|button|bar|title|header)(?:[\s_-]|$)/i.test(c)) continue;
          return el;
        }
      } catch (e) {}
    }
    // 验证语义必须属于 canvas 的局部容器，不能借用整页其他位置的文本。
    try {
      var cs = document.querySelectorAll('canvas');
      for (var k = 0; k < cs.length; k++) {
        var cv = cs[k];
        if (!visible(cv) || cv.width < 30 || cv.height < 30) continue;
        var pa = cv.parentElement;
        for (var d = 0; d < 6 && pa && pa !== document.body; d++) {
          if (captchaEvidence(pa) && /dialog|modal|popup|overlay|mask|panel|box|wrapper|container|layer/i.test(cls(pa))) return pa;
          pa = pa.parentElement;
        }
      }
    } catch (e) {}
    return null;
  }

  function captchaType(container) {
    var txt = (container.innerText || '').slice(0, 500);
    if (/按住|拖动|滑动|拖拽|向右|slider|slide|handle/i.test(txt) || /slider|slide|handle|draggable|puzzle/i.test(cls(container))) {
      return 'slider';
    }
    var cv = container.querySelectorAll('canvas').length;
    var im = container.querySelectorAll('img').length;
    if (cv > 0 || im > 0) return 'grid';
    return null;
  }

  // ---------------- 九宫格求解 ----------------
  function findGrid(container) {
    // 豆包特化：容器自身即九宫格容器
    var direct = container.querySelectorAll('canvas');
    if (direct.length >= 9) return { items: Array.prototype.slice.call(direct), box: container };
    // 通用项选择器
    var generic = container.querySelectorAll(
      '[class*="grid-item"],[class*="piece"],[class*="tile"],[class*="cell"],[class*="image-item"],li,.item');
    if (generic.length === 9) return { items: Array.prototype.slice.call(generic), box: container };
    if (direct.length > 9) { // 同尺寸分组
      var bySize = {};
      direct.forEach(function (c) {
        var k = c.width + 'x' + c.height;
        (bySize[k] = bySize[k] || []).push(c);
      });
      var keys = Object.keys(bySize).sort(function (a, b) { return bySize[b].length - bySize[a].length; });
      if (bySize[keys[0]].length >= 9) return { items: bySize[keys[0]], box: container };
    }
    if (direct.length > 0) return { items: direct.length >= 9 ? Array.prototype.slice.call(direct, 0, 9) : Array.prototype.slice.call(direct), box: container };
    return null;
  }

  function findTargetArea(container) {
    var gridRect = container.getBoundingClientRect();
    var t = container.querySelectorAll('[class*="target"],[class*="drop"],[class*="zone"],[class*="slot"],[class*="placeholder"],[id*="drop"],[id*="zone"]');
    // 放置区是容器而非图片；且应在九宫格下方或侧方（跳过上方的提示参考图）
    for (var k = 0; k < t.length; k++) {
      var el0 = t[k];
      if (el0.tagName === 'IMG' || el0.tagName === 'PICTURE') continue;
      var r0 = el0.getBoundingClientRect();
      if (r0.top < gridRect.bottom - 4) continue;
      return el0;
    }
    // 兜底：在九宫格下方按文本特征找放置区（"拖到这里/放入/放置"等）
    var cands = document.querySelectorAll('div,section,p');
    var best = null, bestTop = 1e9;
    for (var i = 0; i < cands.length; i++) {
      var el = cands[i];
      if (el.children.length > 1) continue; // 容器性元素跳过，找提示性文本块/空槽
      if (el.tagName === 'IMG' || el.tagName === 'PICTURE') continue;
      var r = el.getBoundingClientRect();
      if (r.top < gridRect.bottom || r.width < 80 || r.height < 24 || r.height > 260) continue;
      var txt = (el.textContent || '').trim();
      if (!/拖|放置|放入|到这里|松手|drop/i.test(txt)) continue;
      if (r.top < bestTop) { bestTop = r.top; best = el; }
    }
    return best;
  }

  function extractPrompt(container) {
    var sels = ['.vc-captcha-verify', '[class*="vc-captcha"]', '[class*="prompt"]', '[class*="question"]', '[class*="tip"]', '[class*="desc"]'];
    for (var i = 0; i < sels.length; i++) {
      try {
        var el = document.querySelector(sels[i]);
        if (!el || !visible(el)) continue;
        var txt = (el.textContent || '').trim();
        if (txt.length > 3 && txt.length < 200 && /[\u4e00-\u9fff]/.test(txt)) {
          return txt.replace(/[,，\s]+请选择.*$/, '');
        }
      } catch (e) {}
    }
    return '';
  }

  function compositeGrid(box) {
    var r = box.getBoundingClientRect();
    var cv = document.createElement('canvas');
    cv.width = Math.max(1, Math.round(r.width));
    cv.height = Math.max(1, Math.round(r.height));
    var ctx = cv.getContext('2d');
    box.querySelectorAll('canvas').forEach(function (c) {
      try {
        var cr = c.getBoundingClientRect();
        if (c.width > 0 && c.height > 0) ctx.drawImage(c, cr.x - r.x, cr.y - r.y, cr.width, cr.height);
      } catch (e) {}
    });
    return cv.toDataURL('image/png');
  }

  function captureEl(el) {
    try {
      var r = el.getBoundingClientRect();
      var cv = document.createElement('canvas');
      cv.width = Math.max(1, Math.round(r.width));
      cv.height = Math.max(1, Math.round(r.height));
      var ctx = cv.getContext('2d');
      if (el.tagName === 'CANVAS' || el.tagName === 'IMG') {
        ctx.drawImage(el, 0, 0, cv.width, cv.height);
        return cv.toDataURL('image/png');
      }
    } catch (e) {}
    return '';
  }

  function findSubmitBtn() {
    var btns = document.querySelectorAll('button,[role=button],[class*="submit"],[class*="confirm"]');
    for (var i = 0; i < btns.length; i++) {
      var b = btns[i];
      if (!visible(b)) continue;
      var t = ((b.textContent || '') + (b.innerText || '')).trim();
      if (/^(提交|确认|确定|verify|submit|confirm|ok|下一步)$/i.test(t)) return b;
    }
    return null;
  }

  async function solveGrid(container) {
    if (!autoSolveEnabled()) return false;
    var grid = findGrid(container);
    if (!grid) { log('未找到九宫格结构'); return false; }
    var target = findTargetArea(container);
    var promptText = extractPrompt(container);
    log('九宫格 ' + grid.items.length + ' 项，提示: ' + (promptText || '(空)'));

    var targetB64 = '';
    var tImg = container.querySelector('img');
    if (tImg) targetB64 = captureEl(tImg);

    var answer = await window.tulaiCall('captchaSolve', JSON.stringify({
      type: 'grid',
      gridBase64: compositeGrid(grid.box),
      targetBase64: targetB64,
      promptText: promptText,
    }));
    if (!autoSolveEnabled()) return false;
    if (!answer || !answer.success) {
      log('求解失败: ' + (answer && answer.error));
      return false;
    }
    var indices = answer.indices && answer.indices.length ? answer.indices : [answer.bestIndex || 1];
    for (var i = 0; i < indices.length; i++) {
      if (!autoSolveEnabled()) return false;
      var item = grid.items[indices[i] - 1];
      if (!item) continue;
      var ir = item.getBoundingClientRect();
      var sx = ir.left + ir.width / 2, sy = ir.top + ir.height / 2;
      var ex, ey;
      if (target) {
        var tr = target.getBoundingClientRect();
        ex = tr.left + tr.width / 2 + rnd(-10, 10);
        ey = tr.top + tr.height / 2 + rnd(-5, 5);
      } else {
        ex = sx + rnd(-10, 10);
        ey = ir.bottom + 50;
      }
      log('拖拽 序号' + indices[i] + ' → (' + Math.round(ex) + ',' + Math.round(ey) + ')');
      await humanDrag(sx, sy, ex, ey);
      if (i < indices.length - 1) await sleep(500 + rnd(0, 500));
    }
    await sleep(800 + rnd(0, 400));
    if (!autoSolveEnabled()) return false;
    var btn = findSubmitBtn();
    if (btn) {
      await sleep(200 + rnd(0, 300));
      if (!autoSolveEnabled()) return false;
      var br = btn.getBoundingClientRect();
      var cx = br.left + br.width / 2, cy = br.top + br.height / 2;
      var t = document.elementFromPoint(cx, cy) || btn;
      pointerTo(t, 'pointerdown', cx, cy); fire(t, MouseEvent, 'mousedown', cx, cy);
      await sleep(40 + rnd(0, 60));
      if (!autoSolveEnabled()) return false;
      pointerTo(t, 'pointerup', cx, cy); fire(t, MouseEvent, 'mouseup', cx, cy); fire(t, MouseEvent, 'click', cx, cy);
      try { btn.click(); } catch (e) {}
      log('已点击提交');
    }
    await sleep(2000);
    return !detectCaptcha();
  }

  // ---------------- 滑块求解 ----------------
  async function solveSlider(container) {
    if (!autoSolveEnabled()) return false;
    var btn = null;
    var cands = container.querySelectorAll('[class*="slider"],[class*="handle"],[class*="draggable"],[class*="captcha-slider"],[class*="geetest_slider"],[class*="yidun_slider"],button');
    for (var i = 0; i < cands.length; i++) {
      var el = cands[i];
      if (!visible(el)) continue;
      var t = (el.textContent || '').trim(), c = cls(el);
      if (/按住|拖动|滑动|拖拽|>>|→|«/.test(t) || /(slider|slide|handle|arrow)/i.test(c)) { btn = el; break; }
    }
    if (!btn) { log('未找到滑块把手'); return false; }
    var br = btn.getBoundingClientRect();
    if (br.width > 150 || br.height > 150) { // 容器误判 → 找小尺寸把手
      var subs = btn.querySelectorAll('[class*="slider"],[class*="handle"],button,span,div');
      for (var k = 0; k < subs.length; k++) {
        var sr = subs[k].getBoundingClientRect();
        if (visible(subs[k]) && sr.width > 10 && sr.width <= 80 && sr.height > 10 && sr.height <= 80) {
          if (!/close|cancel/i.test(cls(subs[k]))) { btn = subs[k]; br = sr; break; }
        }
      }
    }
    // 轨道
    var track = btn.parentElement;
    for (var d = 0; d < 8 && track; d++) {
      if (/(track|bar|rail|progress|wrapper)/i.test(cls(track))) break;
      track = track.parentElement;
    }
    var tr2 = (track || btn.parentElement).getBoundingClientRect();
    var sx = br.left + br.width / 2, sy = br.top + br.height / 2;
    // 截图区域：轨道上方 120px 到轨道下方（覆盖缺口背景）
    var shot = await window.tulaiCall('captchaShot', JSON.stringify({
      x: tr2.left, y: Math.min(tr2.top, br.top) - 120, w: tr2.width, h: tr2.height + 160,
    }));
    if (!autoSolveEnabled()) return false;
    if (!shot || !shot.success) { log('滑块截图失败'); return false; }
    var answer = await window.tulaiCall('captchaSolve', JSON.stringify({ type: 'slider', gridBase64: shot.base64 }));
    if (!autoSolveEnabled()) return false;
    if (!answer || !answer.success) { log('滑块求解失败: ' + (answer && answer.error)); return false; }
    var maxDrag = tr2.left + tr2.width - br.width;
    var ex = answer.ratio > 0
      ? Math.min(tr2.left + tr2.width * answer.ratio, maxDrag)
      : Math.min(sx + (answer.offset || 0), maxDrag);
    log('滑块拖到 ' + Math.round(ex) + 'px (比例 ' + (answer.ratio || 0).toFixed(3) + ')');
    await humanDrag(sx, sy, ex, sy);
    await sleep(1500 + rnd(0, 500));
    return !detectCaptcha();
  }

  // ---------------- 登录弹窗 / 下载弹窗 ----------------
  var LOGIN_KW = ['手机号登录', '扫码登录', '短信登录', '验证码登录', '密码登录', '手机登录'];
  var CAPTCHA_KW = ['验证码', '拖动', '滑动', '拖拽', '九宫格', '人机验证', '安全验证', 'captcha', 'verify', 'challenge'];

  function detectLoginWindow() {
    var body = (document.body.innerText || '').slice(0, 500);
    if (!LOGIN_KW.some(function (k) { return body.indexOf(k) >= 0; })) return null;
    var sels = ['[class*="dialog"]', '[class*="modal"]', '[class*="popup"]', '[class*="login"]', '[class*="sign"]', '[class*="panel"]'];
    for (var i = 0; i < sels.length; i++) {
      try {
        var cs = document.querySelectorAll(sels[i]);
        for (var k = 0; k < cs.length; k++) {
          var c = cs[k];
          if (!visible(c)) continue;
          var txt = (c.innerText || '').slice(0, 300);
          if (CAPTCHA_KW.some(function (w) { return txt.indexOf(w) >= 0; })) continue;
          if (LOGIN_KW.some(function (w) { return txt.indexOf(w) >= 0; })) return c;
        }
      } catch (e) {}
    }
    return null;
  }

  function tryCloseLoginWindow() {
    var popup = detectLoginWindow();
    if (!popup) return true;
    // 1) 遮罩四角/中点点击（Semi Design maskClosable）
    var overlays = document.querySelectorAll('.semi-overlay,[class*="overlay"],[class*="mask"],[class*="backdrop"]');
    overlays.forEach(function (ov) {
      if (!visible(ov)) return;
      var r = ov.getBoundingClientRect();
      [{ x: r.left + 10, y: r.top + 10 }, { x: r.right - 10, y: r.top + 10 },
       { x: r.left + 10, y: r.bottom - 10 }, { x: r.right - 10, y: r.bottom - 10 },
       { x: (r.left + r.right) / 2, y: r.top + 10 }].forEach(function (pt) {
        var t = document.elementFromPoint(pt.x, pt.y);
        if (!t) return;
        pointerTo(t, 'pointerdown', pt.x, pt.y); fire(t, MouseEvent, 'mousedown', pt.x, pt.y);
        pointerTo(t, 'pointerup', pt.x, pt.y); fire(t, MouseEvent, 'mouseup', pt.x, pt.y); fire(t, MouseEvent, 'click', pt.x, pt.y);
      });
    });
    // 2) Escape
    [ 'keydown', 'keyup' ].forEach(function (ty) {
      document.dispatchEvent(new KeyboardEvent(ty, { key: 'Escape', code: 'Escape', keyCode: 27, which: 27, bubbles: true }));
    });
    // 3) React fiber onClick 直调关闭按钮
    try {
      var btns = (popup || document).querySelectorAll('button[aria-label*="close" i],[class*="close"],.semi-modal-close');
      for (var i = 0; i < btns.length; i++) {
        var b = btns[i];
        if (!visible(b)) continue;
        var fiber = b.__reactFiber || b.__reactInternalInstance$;
        var depth = 0;
        while (fiber && depth < 15) {
          if (fiber.memoizedProps && typeof fiber.memoizedProps.onClick === 'function') {
            fiber.memoizedProps.onClick({ stopPropagation: function () {}, preventDefault: function () {}, nativeEvent: new MouseEvent('click') });
            break;
          }
          fiber = fiber.return; depth++;
        }
      }
    } catch (e) {}
    // 4) 兜底隐藏
    if (detectLoginWindow()) {
      try { popup.style.setProperty('display', 'none', 'important'); } catch (e) {}
    }
    return !detectLoginWindow();
  }

  // 豆包下载客户端弹窗（dbx design token 关闭按钮）
  function findDbxClose() {
    try {
      var bs = document.querySelectorAll('button[aria-label="关闭"]');
      for (var i = 0; i < bs.length; i++) {
        if (visible(bs[i]) && /bg-dbx-static-black/i.test(cls(bs[i]))) return bs[i];
      }
    } catch (e) {}
    return null;
  }
  function closeDownloadPopup() {
    var btn = findDbxClose();
    if (!btn) return false;
    var el = btn.parentElement; // 排除验证码容器
    for (var i = 0; i < 6 && el; i++) {
      if (/验证码|拖动|滑动|拖拽|九宫格|人机验证|安全验证|captcha|challenge/i.test((el.innerText || '').slice(0, 300))) return false;
      el = el.parentElement;
    }
    var r = btn.getBoundingClientRect();
    window.tulaiClick && window.tulaiClick('button[aria-label="关闭"]');
    var cx = r.left + r.width / 2, cy = r.top + r.height / 2;
    var t = document.elementFromPoint(cx, cy) || btn;
    pointerTo(t, 'pointerdown', cx, cy); fire(t, MouseEvent, 'mousedown', cx, cy);
    pointerTo(t, 'pointerup', cx, cy); fire(t, MouseEvent, 'mouseup', cx, cy); fire(t, MouseEvent, 'click', cx, cy);
    setTimeout(function () {
      if (findDbxClose()) {
        var c = btn.parentElement;
        for (var d = 0; d < 8 && c; d++) {
          if (/dialog|modal|popup/i.test(c.getAttribute('role') || '') || /dialog|modal|popup|overlay/i.test(cls(c))) {
            c.style.setProperty('display', 'none', 'important');
            return;
          }
          c = c.parentElement;
        }
      }
    }, 800);
    log('关闭下载客户端弹窗');
    return true;
  }

  // ---------------- 阻塞状态上报 ----------------
  var blockingState = false;
  function reportBlocking() {
    var hasCaptcha = !!detectCaptcha();
    var hasLogin = st.loginMode ? false : !!detectLoginWindow();
    var hasDownload = st.loginMode ? false : !!findDbxClose();
    var now = hasCaptcha || hasLogin || hasDownload;
    if (now !== blockingState || now) {
      blockingState = now;
      var reason = hasCaptcha ? 'captcha' : (hasLogin ? 'login' : (hasDownload ? 'download' : ''));
      try { window.tulaiCall('captchaBlocking', JSON.stringify({ blocked: now, reason: reason })); } catch (e) {}
    }
  }

  // ---------------- 主循环 ----------------
  async function solveOnce() {
    if (!autoSolveEnabled()) return false;
    var el = detectCaptcha();
    if (!el) return false;
    var type = captchaType(el);
    if (!type) return false;
    return type === 'slider' ? solveSlider(el) : solveGrid(el);
  }

  function refreshCaptcha() { // 换题：点"换一批/刷新"
    if (!autoSolveEnabled()) return false;
    var btns = document.querySelectorAll('button,[role=button],a,span');
    for (var i = 0; i < btns.length; i++) {
      var t = (btns[i].textContent || '').trim();
      if (/^(换一批|换一换|刷新|看不清|重新加载)$/.test(t)) {
        try { btns[i].click(); log('已点击换题'); return true; } catch (e) {}
      }
    }
    return false;
  }

  async function startPolling() {
    if (st.running) return;
    st.running = true;
    log('验证码监听已启动');
    setInterval(reportBlocking, CFG.reportMs);
    while (st.running) {
      if (!st.loginMode) {
        closeDownloadPopup();
        if (detectLoginWindow()) {
          log('检测到登录弹窗，尝试关闭');
          var ok = false;
          for (var a = 0; a < 5 && !ok; a++) {
            ok = tryCloseLoginWindow();
            if (!ok) await sleep(2000);
          }
          if (!ok) {
            log('登录弹窗无法关闭，通知主程序');
            try { window.tulaiCall('loginDetected', ''); } catch (e) {}
            break;
          }
          await sleep(CFG.pollMs);
          continue;
        }
      }
      var detected = detectCaptcha();
      var kind = detected ? (captchaType(detected) || '') : '';
      if (kind !== st.kind) {
        st.kind = kind;
        if (kind) log('检测到验证码: ' + kind);
      }
      if (detected) {
        if (!autoSolveEnabled() || st.manual || !kind) {
          await sleep(CFG.pollMs);
          continue;
        }
        if (st.retries >= CFG.maxRetries) {
          await sleep(CFG.pollMs);
          continue;
        }
        var success = await solveOnce();
        if (!autoSolveEnabled()) { st.retries = 0; continue; }
        if (success) {
          st.retries = 0;
          log('验证通过');
          try { window.tulaiCall('captchaBlocking', '{"blocked":false,"reason":""}'); } catch (e) {}
        } else {
          st.retries++;
          log('求解失败，重试 ' + st.retries + '/' + CFG.maxRetries);
        }
      } else {
        st.retries = 0;
      }
      await sleep(CFG.pollMs);
    }
  }

  window.__tulaiCaptcha = {
    start: startPolling,
    stop: function () { st.running = false; },
    setManual: function (v) { st.manual = !!v; },
    setLoginMode: function (v) { st.loginMode = !!v; },
    solve: solveOnce,
    detect: detectCaptcha,
    status: function () {
      var el = detectCaptcha();
      return { running: st.running, retries: st.retries, blocked: blockingState,
        autoSolve: autoSolveEnabled() && st.retries < CFG.maxRetries && (!el || !!captchaType(el)) };
    },
  };
  if (document.readyState === 'complete') startPolling();
  else window.addEventListener('load', startPolling);
})();
