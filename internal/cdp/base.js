// tulai 基础页面运行时：Go↔页面桥桩与 DOM 原语。
// 由 Go 端在会话建立后注入（并注册为新文档脚本）。
(function () {
  if (window.__tulaiBaseInstalled === 4) return;
  window.__tulaiBaseInstalled = 4;
  var _String = String;
  var _Promise = Promise;
  var _JSONParse = JSON.parse;
  var _JSONStringify = JSON.stringify;
  var _setTimeout = setTimeout;

  // ---- 桥：页面 → Go 异步调用 ----
  // 页面侧: await tulaiCall("cmd", dataString) → Go 处理结果字符串
  window.__tulaiSeq = window.__tulaiSeq || 0;
  window.__tulaiPending = window.__tulaiPending || {};
  window.__tulaiResolve = function (id, answerJson) {
    var obj;
    try { obj = _JSONParse(answerJson); } catch (e) { return; }
    var entry = window.__tulaiPending[id];
    if (entry) {
      delete window.__tulaiPending[id];
      // result 是处理程序返回的 JSON 字符串，解析成对象再交给调用方
      var res = obj.result;
      if (typeof res === 'string') {
        try { res = _JSONParse(res); } catch (e) { /* 保留原字符串 */ }
      }
      entry(res);
    }
  };
  function tulaiCall(cmd, data) {
    return new _Promise(function (resolve) {
      var id = ++window.__tulaiSeq;
      window.__tulaiPending[id] = resolve;
      try {
        tulaiBridge(_JSONStringify({ cmd: cmd, id: id, data: data == null ? '' : _String(data) }));
      } catch (e) { delete window.__tulaiPending[id]; resolve(null); }
      // 60s 超时兜底
      _setTimeout(function () {
        if (window.__tulaiPending[id]) { delete window.__tulaiPending[id]; resolve(null); }
      }, 60000);
    });
  }
  window.tulaiCall = tulaiCall;

  // ---- DOM 原语 ----

  // 单次浏览器点击，可信 pointerdown 可打开 Radix 等受控菜单。
  // 元素处于 CSS 位移/缩放过渡（如附件栏收起、Radix 下拉入场）时先等几何框稳定再点，避免点偏。
  window.tulaiClick = async function (sel) {
    var el = typeof sel === 'string' ? document.querySelector(sel) : sel;
    if (!el || !el.isConnected || el.disabled || el.getAttribute('aria-disabled') === 'true') return false;
    var r = el.getBoundingClientRect();
    if (r.top < 0 || r.bottom > innerHeight || r.left < 0 || r.right > innerWidth) {
      el.scrollIntoView({ block: 'center', inline: 'center' });
      r = el.getBoundingClientRect();
    }
    for (var settle = 0; settle < 16; settle++) {
      await new _Promise(function (resolve) { _setTimeout(resolve, 12); });
      if (!el.isConnected || el.disabled || el.getAttribute('aria-disabled') === 'true') return false;
      var cur = el.getBoundingClientRect();
      if (Math.abs(cur.left - r.left) < 1 && Math.abs(cur.top - r.top) < 1 &&
          Math.abs(cur.width - r.width) < 1 && Math.abs(cur.height - r.height) < 1) {
        r = cur;
        break;
      }
      r = cur;
    }
    var left = Math.max(0, r.left), right = Math.min(innerWidth, r.right);
    var top = Math.max(0, r.top), bottom = Math.min(innerHeight, r.bottom);
    if (right <= left || bottom <= top) return false;
    var cx = (left + right) / 2, cy = (top + bottom) / 2;
    var jx = (right - left) * 0.2, jy = (bottom - top) * 0.2;
    var x = cx + (Math.random() * 2 - 1) * jx;
    var y = cy + (Math.random() * 2 - 1) * jy;
    var target = document.elementFromPoint(x, y);
    if (!target || !el.contains(target)) {
      x = cx; y = cy;
      target = document.elementFromPoint(x, y);
    }
    if (!target || !el.contains(target)) return false;
    var answer = await tulaiCall('mouseClick', JSON.stringify({ x: x, y: y }));
    return !!(answer && answer.success);
  };

  // 真实浏览器鼠标悬停，触发 CSS :hover / group-hover 显现隐藏操作按钮。
  window.tulaiHover = async function (sel) {
    var el = typeof sel === 'string' ? document.querySelector(sel) : sel;
    if (!el || !el.isConnected) return false;
    var r = el.getBoundingClientRect();
    if (r.top < 0 || r.bottom > innerHeight || r.left < 0 || r.right > innerWidth) {
      el.scrollIntoView({ block: 'center', inline: 'center' });
      r = el.getBoundingClientRect();
    }
    for (var settle = 0; settle < 12; settle++) {
      await new _Promise(function (resolve) { _setTimeout(resolve, 12); });
      if (!el.isConnected) return false;
      var cur = el.getBoundingClientRect();
      if (Math.abs(cur.left - r.left) < 1 && Math.abs(cur.top - r.top) < 1 &&
          Math.abs(cur.width - r.width) < 1 && Math.abs(cur.height - r.height) < 1) {
        r = cur;
        break;
      }
      r = cur;
    }
    var left = Math.max(0, r.left), right = Math.min(innerWidth, r.right);
    var top = Math.max(0, r.top), bottom = Math.min(innerHeight, r.bottom);
    if (right <= left || bottom <= top) return false;
    var x = (left + right) / 2, y = (top + bottom) / 2;
    var answer = await tulaiCall('mouseMove', JSON.stringify({ x: x, y: y }));
    return !!(answer && answer.success);
  };

  // 后台窗口里按钮的 click() 不可信，豆包会丢掉。回车走浏览器按键。
  window.tulaiPressEnter = async function () {
    var answer = await tulaiCall('keyEnter', '');
    return !!(answer && answer.success);
  };

  // 向 composer（textarea / contenteditable / input）写入文本并派发输入事件
  window.tulaiInsertText = function (sel, text) {
    var el = document.querySelector(sel);
    if (!el) return false;
    el.focus();
    if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
      var setter = Object.getOwnPropertyDescriptor(
        el.tagName === 'INPUT' ? window.HTMLInputElement.prototype : window.HTMLTextAreaElement.prototype, 'value').set;
      setter.call(el, text);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    } else {
      // contenteditable：先清空再逐段写入（部分框架对 execCommand 有监听）
      el.textContent = '';
      document.execCommand('insertText', false, text);
      if (!el.textContent) el.textContent = text;
      el.dispatchEvent(new InputEvent('input', { bubbles: true, data: text, inputType: 'insertText' }));
    }
    return true;
  };

  // 标记第 n 个 file input 供 CDP 上传
  window.tulaiMarkFileInput = function (n, attr) {
    if (!attr || typeof attr !== 'string') return -1;
    var inputs = document.querySelectorAll('input[type=file]');
    var el = inputs[n];
    if (!el) return -1;
    for (var i = 0; i < inputs.length; i++) inputs[i].removeAttribute(attr);
    el.setAttribute(attr, '1');
    return inputs.length;
  };
  window.tulaiUnmarkFileInput = function (attr) {
    if (!attr || typeof attr !== 'string') return false;
    var inputs = document.querySelectorAll('input[type=file]');
    var removed = false;
    for (var i = 0; i < inputs.length; i++) {
      if (inputs[i].hasAttribute(attr)) {
        inputs[i].removeAttribute(attr);
        removed = true;
      }
    }
    return removed;
  };

  // 按文本找按钮并点击（发送按钮等）
  window.tulaiClickButtonText = async function (texts, scopeSel) {
    var scope = scopeSel ? (document.querySelector(scopeSel) || document) : document;
    var els = scope.querySelectorAll('button,[role=button],div,span,a');
    for (var i = els.length - 1; i >= 0; i--) {
      var el = els[i];
      if (el.children.length > 0) continue;
      var st;
      try { st = getComputedStyle(el); } catch (e) { continue; }
      if (st.display === 'none' || st.visibility === 'hidden' || el.offsetWidth < 2) continue;
      var t = ((el.textContent || '') + ' ' + (el.getAttribute('aria-label') || '')).trim();
      for (var k = 0; k < texts.length; k++) {
        if (t.indexOf(texts[k]) >= 0) {
          var cur = el;
          while (cur && cur !== document.body) {
            if (cur.tagName === 'BUTTON' || cur.getAttribute('role') === 'button' || cur.onclick) {
              try { cur.click(); } catch (e) {}
              return true;
            }
            cur = cur.parentElement;
          }
          try { el.click(); } catch (e) {}
          return true;
        }
      }
    }
    return false;
  };
})();
