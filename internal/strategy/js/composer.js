// tulai composer 流程：找输入框 → 打开上传入口 → 让 input[type=file] 出现。
// 浏览器先等待真实文件选择器，脚本只点击当前输入区入口与本次菜单。
(function () {
  window.__tulaiComposer = window.__tulaiComposer || {};

  function visible(el) {
    if (!el) return false;
    if (el.offsetParent === null && el.tagName !== 'BODY') return false;
    return el.clientWidth > 5 && el.clientHeight > 5 && getComputedStyle(el).visibility !== 'hidden' && !el.disabled &&
      (!el.getAttribute || el.getAttribute('aria-disabled') !== 'true');
  }
  function textOf(el) { return ((el.textContent || '') + ' ' + (el.innerText || '')).trim(); }
  function labelOf(el) {
    return ((el.getAttribute && el.getAttribute('aria-label')) || '') + ' ' +
      ((el.getAttribute && el.getAttribute('title')) || '');
  }
  function combined(el) { return (textOf(el) + ' ' + labelOf(el)).trim(); }

  // 最近的可编辑输入区（composer）
  function findComposer() {
    var tas = document.querySelectorAll('textarea,[contenteditable=true],input[type=text]');
    for (var i = tas.length - 1; i >= 0; i--) {
      if (visible(tas[i])) return tas[i];
    }
    return null;
  }
  // 仅有占位/隐藏文本时视为空；有可编辑正文时保留浏览器的多行 innerText。
  window.tulaiReadComposer = function (comp) {
    if (!comp) return null;
    if (comp.tagName === 'TEXTAREA' || comp.tagName === 'INPUT') return comp.value;
    var walker = document.createTreeWalker(comp, NodeFilter.SHOW_TEXT), text;
    while ((text = walker.nextNode())) {
      if (!String(text.nodeValue || '').trim()) continue;
      var excluded = false;
      for (var node = text.parentElement; node && node !== comp; node = node.parentElement) {
        var style = getComputedStyle(node);
        if (node.getAttribute('contenteditable') === 'false' || node.getAttribute('aria-hidden') === 'true' || style.display === 'none' || style.visibility === 'hidden') { excluded = true; break; }
      }
      if (!excluded) return comp.innerText;
    }
    return '';
  };

  window.tulaiComposerDiagnostic = function (comp) {
    if (!comp) return ['composer=none'];
    function stats(value) {
      var points = Array.from(String(value || ''));
      function codes(part) { return part.map(function(char){return 'U+'+char.codePointAt(0).toString(16).toUpperCase()}).join(','); }
      return 'chars=' + points.length + '; first=' + codes(points.slice(0,3)) + '; last=' + codes(points.slice(-3));
    }
    function describe(node) {
      return node.tagName + '#' + (node.id || '').slice(0,30) + '; class=' + cls(node).slice(0,40) + '; editable=' + (node.getAttribute('contenteditable') || '') + '; ariaHidden=' + (node.getAttribute('aria-hidden') || '') + '; data=' + node.getAttributeNames().filter(function(name){return name.indexOf('data-')===0 || name==='aria-placeholder'}).join(',').slice(0,70) + '; ' + stats(node.textContent);
    }
    var lines = ['value ' + stats(window.tulaiReadComposer(comp)), describe(comp)];
    var nodes = Array.from(comp.querySelectorAll('[contenteditable="false"],[aria-hidden="true"],[data-slate-placeholder],[data-placeholder],[aria-placeholder],[class*="placeholder" i]'));
    var walker = document.createTreeWalker(comp, NodeFilter.SHOW_TEXT), text;
    while ((text = walker.nextNode()) && nodes.length < 8) { if (String(text.nodeValue || '').trim() && text.parentElement !== comp && nodes.indexOf(text.parentElement) === -1) nodes.push(text.parentElement); }
    nodes.slice(0,8).forEach(function(node,index){lines.push('node#'+index+' '+describe(node))});
    return lines.map(function(line){return line.slice(0,320)});
  };

  // composer 作用域（含发送/上传按钮的容器）
  function findScope(comp) {
    if (!comp) return document;
    var p = comp;
    for (var d = 0; d < 8 && p; d++) {
      if (p.hasAttribute && p.hasAttribute('data-chat-input-body')) return p;
      p = p.parentElement;
    }
    var q = comp;
    for (var e = 0; e < 8 && q; e++) {
      var btns = q.querySelectorAll && q.querySelectorAll('button,[role=button]');
      if (btns) {
        for (var b = 0; b < btns.length; b++) {
          var tx = (btns[b].textContent || '') + (btns[b].getAttribute('aria-label') || '');
          if (/发送|更多|Send|More/i.test(tx)) return q;
        }
      }
      q = q.parentElement;
    }
    return document;
  }
  async function clickEl(el) {
    if (!el) return false;
    try { return await window.tulaiClick(el); } catch (e) { return false; }
  }

  var MODE_KW = ['AI生图', 'AI生视频', 'PPT创作', 'AI写作', '智能体', '模板', '海报', '电商', '人像', '视频生成', '音乐', '翻译', '画图', '代码', '千问高考', '办公助理', '研究'];

  function isMode(t) {
    for (var i = 0; i < MODE_KW.length; i++) if (t.indexOf(MODE_KW[i]) >= 0) return true;
    return false;
  }
  function cls(el) { return ((el && el.className) || '').toString(); }

  // 上传面板：弹出的菜单/浮层
  function findUploadPanel(comp, excluded) {
    var cr = comp ? comp.getBoundingClientRect() : null;
    var all = document.querySelectorAll('[role=menu],[role=listbox],[role=dialog],[data-radix-popper-content-wrapper],[popover]');
    for (var i = 0; i < all.length; i++) {
      var el = all[i];
      if (!visible(el) || (excluded && excluded.has(el))) continue;
      var r = el.getBoundingClientRect();
      if (r.width < 40 || r.height < 40) continue;
      if (cr && r.left < cr.left - 120) continue;
      var tx = el.textContent || '';
      if (!/参考图|上传|本地|从相册|选择|导入|我的文件|图片|文件|附件/i.test(tx)) continue;
      return el;
    }
    return null;
  }

  function fileCount() { return document.querySelectorAll('input[type=file]').length; }

  function attachmentScope(comp, uploadEntry) {
    var surface = comp.closest('.guidance-input-surface');
    if (surface && uploadEntry && surface.contains(uploadEntry)) return surface;
    var scope = findScope(comp), peer = uploadEntry || findSendButton(comp);
    if (peer) {
      for (var p = comp.parentElement; p && p !== document.body; p = p.parentElement) {
        if (p.contains(peer)) return scope !== document && scope !== document.body && scope.contains(p) ? scope : p;
      }
    }
    return scope === document || scope === document.body || scope === document.documentElement ? null : scope;
  }


  // 只打开当前编辑器的真实上传动作，文件由本轮浏览器 chooser 接收。
  window.tulaiOpenUpload = async function () {
    var comp = findComposer(), log = [];
    window.__tulaiUploadDiagnostic = log;
    if (!comp) throw new Error('未找到输入框');
    var entry = null;
    for (var scope = comp.parentElement; scope && scope !== document.body; scope = scope.parentElement) {
      var exact = scope.querySelector('button[data-testid="upload_file_button"]');
      if (visible(exact)) { entry = exact; break; }
    }
    for (scope = comp.parentElement; !entry && scope && scope !== document.body; scope = scope.parentElement) {
      var buttons = scope.querySelectorAll('button,[role=button]');
      for (var i = 0; i < buttons.length; i++) {
        if (visible(buttons[i]) && /添加附件/.test(combined(buttons[i]))) { entry = buttons[i]; break; }
      }
    }
    if (!entry) throw new Error('未找到当前输入区上传入口');
    var previewScope = attachmentScope(comp, entry), initialImages = new Map();
    Array.from(document.images).forEach(function (img) { initialImages.set(img, img.currentSrc || img.src); });
    window.__tulaiUploadState = { scope: previewScope, entry: entry, images: initialImages, files: new Map(Array.from(document.querySelectorAll('input[type=file]')).map(function(input){return [input,input.files]})) };
    log.push('scope=' + (previewScope ? previewScope.tagName + '#' + (previewScope.id || '').slice(0, 40) + '.' + cls(previewScope).slice(0, 60) + '; role=' + (previewScope.getAttribute('role') || '').slice(0, 40) : 'none'));
    log.push('entry=' + entry.tagName + '; testid=' + (entry.getAttribute('data-testid') || '').slice(0, 60) + '; id=' + (entry.id || '').slice(0, 50) + '; aria=' + (entry.getAttribute('aria-label') || '').slice(0, 60));
    var oldPanels = new Set(Array.from(document.querySelectorAll('[role=menu],[role=listbox],[role=dialog],[data-radix-popper-content-wrapper],[popover]')).filter(visible));
    if (!await clickEl(entry)) throw new Error('点击上传入口失败');
    // 直接打开 chooser 时没有菜单；有菜单时只处理本次新出现的面板。
    var panel = null;
    for (var deadline = Date.now() + 800; Date.now() < deadline;) {
      panel = findUploadPanel(comp, oldPanels);
      if (panel && !oldPanels.has(panel)) break;
      panel = null;
      await sleep(100);
    }
    if (panel) {
      var items = panel.querySelectorAll('[role=menuitem],[role=option],button,a');
      for (var j = 0; j < items.length; j++) {
        var item = items[j], text = combined(item);
        if (!visible(item) || isMode(text) || !/本地图片|上传图片|本地上传|上传文件|选择图片|选择文件/.test(text)) continue;
        log.push('menu-item=' + item.tagName + '#' + (item.id || '').slice(0, 50));
        if (!await clickEl(item)) throw new Error('点击上传菜单失败');
        break;
      }
    }
    return JSON.stringify({ opened: true });
  };

  window.tulaiWaitForUpload = async function (expected) {
    var deadline = Date.now() + 30000, stable = 0, count = 0, busy = false;
    var state = window.__tulaiUploadState;
    while (Date.now() < deadline) {
      var comp = findComposer(), scope = comp && attachmentScope(comp, state && state.entry);
      if (state && state.scope && state.scope.isConnected && state.scope.contains(comp)) scope = state.scope;
      if (!comp || !state || !scope) break;
      busy = Array.from(scope.querySelectorAll('[aria-busy="true"],[role="progressbar"]')).some(visible);
      count = Array.from(scope.querySelectorAll('img')).filter(function (img) {
        var src = img.currentSrc || img.src;
        return src && visible(img) && img.complete && img.naturalWidth > 0 && (!state.images.has(img) || state.images.get(img) !== src);
      }).length;
      if (!busy && count >= expected) {
        if (++stable >= 2) return JSON.stringify({ ready: true });
      } else stable = 0;
      await sleep(100);
    }
    var diagnostic = ['expected=' + expected + '; composerAttachments=' + count + '; busy=' + busy + '; scope=' + (state && state.scope ? state.scope.tagName : 'none')];
    Array.from(document.querySelectorAll('input[type=file]')).slice(-8).forEach(function (input, index) {
      diagnostic.push('file#' + index + ' id=' + (input.id || '').slice(0, 50) + '; accept=' + (input.accept || '').slice(0, 60) + '; files=' + input.files.length + '; currentScope=' + !!(state && state.scope && state.scope.contains(input)));
    });
    return JSON.stringify({ ready: false, diagnostic: diagnostic });
  };

  window.tulaiUploadAncestorDiagnostic = function () {
    var lines = [], comp = findComposer(), state = window.__tulaiUploadState;
    function attribute(node, name, length) { return String(node && node.getAttribute(name) || '').replace(/https?:\/\/[^/\s?#]+/gi, '[origin]').replace(/\?[^#\s]*/g, '?[redacted]').slice(0, length); }
    function describe(node) {
      if (!node) return 'none';
      return node.tagName + '#' + (node.id || '').slice(0, 35) + '; testid=' + attribute(node, 'data-testid', 50) + '; class=' + cls(node).slice(0, 55) + '; data=' + node.getAttributeNames().filter(function(name){return name.indexOf('data-')===0}).join(',').slice(0, 70);
    }
    var changedInputs = state && state.files ? Array.from(document.querySelectorAll('input[type=file]')).filter(function(input){return input.files.length && state.files.get(input)!==input.files}) : [];
    for (var node = comp, depth = 0; node && node !== document.body && depth < 12; depth++, node = node.parentElement) {
      lines.push('ancestor#' + depth + ' ' + describe(node) + '; imgs=' + node.querySelectorAll('img').length + '; containsChosenInput=' + changedInputs.some(function(input){return node.contains(input)}));
    }
    var added = state ? Array.from(document.images).filter(function(img){return !state.images.has(img)}) : [];
    lines.push('newIMGCount=' + added.length + '; changedFileInputs=' + changedInputs.length);
    added.slice(-12).forEach(function(img,index){
      lines.push('newIMG#' + index + ' ' + describe(img) + '; size=' + img.clientWidth + 'x' + img.clientHeight + '; loaded=' + (img.complete && img.naturalWidth>0));
      var button = img.closest('button,[role=button]');
      lines.push('newIMG#' + index + ' button=' + describe(button) + '; aria=' + attribute(button,'aria-label',45) + '; title=' + attribute(button,'title',45));
      for (var parent = img.parentElement, level=0; parent && parent !== document.body && level<4; level++,parent=parent.parentElement) lines.push('newIMG#'+index+' parent#'+level+' '+describe(parent));
      var common = comp;
      while(common && common !== document.body && !common.contains(img)) common=common.parentElement;
      lines.push('newIMG#'+index+' common='+describe(common === document.body ? null : common));
    });
    return lines.map(function(line){return line.slice(0, 320)});
  };

  window.tulaiMarkComposer = function () {
    var comp = findComposer();
    if (!comp || comp.readOnly) return false;
    document.querySelectorAll('[data-tulai-composer]').forEach(function (el) { el.removeAttribute('data-tulai-composer'); });
    comp.setAttribute('data-tulai-composer', '1');
    return true;
  };

  function findSendButton(comp) {
    // 上传区/“更多”可能只包住编辑器，发送按钮在其外层兄弟中。
    for (var scope = comp.parentElement; scope; scope = scope.parentElement) {
      var buttons = scope.querySelectorAll('button,[role=button]');
      for (var i = buttons.length - 1; i >= 0; i--) {
        var el = buttons[i], r = el.getBoundingClientRect();
        if (r.width > 5 && r.height > 5 && getComputedStyle(el).visibility !== 'hidden' &&
            (el.getAttribute('data-testid') === 'chat_input_send_button' || /发送|发消息|\bsend\b/i.test(combined(el)))) return el;
      }
    }
    return null;
  }

  function sendDiagnostic(comp) {
    var buttons = [];
    for (var scope = comp.parentElement; scope; scope = scope.parentElement) {
      buttons = Array.from(scope.querySelectorAll('button,[role=button]'));
      if (buttons.length >= 4 || scope === document.body) break;
    }
    function attribute(el, name) {
      return String(el.getAttribute(name) || '').replace(/https?:\/\/[^/\s?#]+/gi, '[origin]').replace(/\?[^#\s]*/g, '?[redacted]').slice(0, name === 'class' ? 40 : 60);
    }
    function structure(el, attrs) {
      return el.tagName + '[' + attrs.map(function (name) { var value = attribute(el, name); return value ? name + '=' + value : ''; }).filter(Boolean).join(',') + ']';
    }
    var extra = Array.from(scope.querySelectorAll('[data-testid*="send" i],[class*="send" i],svg use')).filter(function (el) { return buttons.indexOf(el) === -1; }).slice(-12);
    function describe(el, prefix) {
      return (prefix + structure(el, ['data-testid', 'id', 'aria-label', 'title', 'aria-labelledby', 'aria-describedby', 'href', 'xlink:href']) +
        Array.from(el.querySelectorAll('svg,use')).slice(0, 3).map(function (icon) { return structure(icon, ['href', 'xlink:href', 'class']); }).join('') + ' class=' + attribute(el, 'class')).slice(0, 320);
    }
    return ['composer=' + comp.tagName + '; buttons=' + buttons.length + '; extra=' + extra.length].concat(buttons.slice(-12).map(function (el, index) {
      return describe(el, '#' + (Math.max(0, buttons.length - 12) + index + 1) + ' ');
    }), extra.map(function (el, index) { return describe(el, 'icon#' + (index + 1) + ' '); }));
  }

  function conversationKey(href) {
    if (window.__tulaiConversationKey) return window.__tulaiConversationKey(href);
    try {
      var parts = new URL(String(href || ''), location.href).pathname.split('/').filter(Boolean);
      var i = parts.indexOf('chat');
      return i >= 0 && parts[i + 1] ? parts[i + 1] : '';
    } catch (e) { return ''; }
  }

  // 发送成功要看到我们这条用户气泡，或地址变成新的对话 id。输入框空了不算。
  window.tulaiSendAccepted = function (beforeHref, prompt) {
    var href = String(location.href || '');
    var nowID = conversationKey(href);
    var thenID = conversationKey(beforeHref);
    if (nowID && nowID !== thenID) return true;
    var want = String(prompt || '').replace(/\s+/g, '');
    if (!want) return false;
    var nodes = document.querySelectorAll('[data-message-author-role="user"],[data-message-role="user"],[data-testid="send_message"],[data-testid="user_message"],[data-testid="message_text_user"]');
    for (var i = 0; i < nodes.length; i++) {
      var text = String(nodes[i].innerText || nodes[i].textContent || '').replace(/\s+/g, '');
      if (text.indexOf(want) !== -1) return true;
    }
    return false;
  };

  window.tulaiSendPrompt = async function () {
    var comp = document.querySelector('[data-tulai-composer="1"]');
    if (!comp) return JSON.stringify({ attempted: false, error: '未找到已定位的输入框' });
    var deadline = Date.now() + 5000, button;
    while (Date.now() < deadline) {
      if (!comp.isConnected) comp = findComposer();
      if (!comp) return JSON.stringify({ attempted: false, error: '输入框已消失' });
      button = findSendButton(comp);
      if (!button) return JSON.stringify({ attempted: false, error: '未找到发送按钮', diagnostic: sendDiagnostic(comp) });
      if (visible(button)) break;
      await sleep(100);
    }
    if (!visible(button)) return JSON.stringify({ attempted: false, error: '发送按钮仍被禁用，平台尚未就绪' });
    var beforeHref = String(location.href || '');
    var promptText = window.tulaiReadComposer(comp);
    var clicked = false;
    if (window.tulaiClick) {
      try { clicked = await window.tulaiClick(button); } catch (e) { clicked = false; }
    }
    if (!clicked) {
      try { button.click(); clicked = true; } catch (e) { return JSON.stringify({ attempted: true, error: '点击发送失败: ' + String(e) }); }
    }
    deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      if (window.tulaiSendAccepted(beforeHref, promptText)) return JSON.stringify({ sent: true });
      await sleep(100);
    }
    var latest = findComposer();
    var remaining = window.tulaiReadComposer(latest);
    return JSON.stringify({ attempted: true, error: '平台未接受消息：发送后没有出现用户消息，地址也没有变成对话', diagnostic: ['accept composer=' + (latest ? latest.tagName : 'none') + '; chars=' + String(remaining || '').trim().length + '; nodeChanged=' + (latest !== comp) + '; oldConnected=' + comp.isConnected].concat(window.tulaiComposerDiagnostic(latest)) });
  };

  // 文本由可信 InsertText 全文替换，只清理输入区附件。
  window.tulaiCleanComposer = async function () {
    var comp = findComposer();
    if (!comp) return false;
    var scope = findScope(comp);
    if (scope === document || scope === document.body || scope === document.documentElement) return true;
    // 移除已上传的预览缩略图（关闭按钮）
    var dels = scope.querySelectorAll('[class*="preview"] [class*="delete"],[class*="preview"] [class*="remove"],[class*="image-container"] [class*="close"],[aria-label*="删除" i],[aria-label*="移除" i]');
    for (var i = 0; i < Math.min(dels.length, 9); i++) {
      try { await clickEl(dels[i]); } catch (e) {}
    }
    return true;
  };

  // 页面状态探测
  window.tulaiPageState = function () {
    var body = (document.body && document.body.innerText) || '';
    return {
      hasComposer: !!findComposer(),
      fileCount: fileCount(),
      generating: /生成中|正在生成|绘制中|Drawing|Generating/i.test(body),
      failed: /生成失败|请重试|敏感|违规/i.test(body),
      quota: accountNotice(document, /额度不足|额度已用尽|额度已用|额度用尽|次数已用完|次数已用尽|免费次数已|已达上限|超出.{0,8}限额|生成次数已|quota exceeded|daily limit/i),
      rateLimit: accountNotice(document, /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests/i),
      moderation: refusalText(),
    };
  };

  // 只看这一轮新出现的助手回复，加上短提示条。话术表在 extension/refuse.js。
  function refusalText() {
    if (!window.TULAIRefuse) return '';
    return window.TULAIRefuse.match(freshReplyText()) || window.TULAIRefuse.match(noticeBlob(document)) || '';
  }

  function freshReplyText() {
    var q = window.__tulaiAssistantQuery;
    if (!q) return '';
    var nodes = document.querySelectorAll(q);
    var start = window.__tulaiAssistantBaseline || 0;
    if (start < 0 || start > nodes.length) start = nodes.length;
    var parts = [];
    for (var i = start; i < nodes.length; i++) {
      var text = String(nodes[i].innerText || '').replace(/\s+/g, ' ').trim();
      if (text) parts.push(text.slice(0, 800));
    }
    return parts.join('\n');
  }

  function noticeBlob(doc) {
    var nodes = doc.querySelectorAll('[role="alert"],[role="dialog"],[class*="toast"],[class*="notice"]');
    var parts = [];
    for (var i = 0; i < nodes.length; i++) {
      var text = String(nodes[i].innerText || '').replace(/\s+/g, ' ').trim();
      if (text && text.length <= 240) parts.push(text);
    }
    return parts.join('\n');
  }

  // 只看提示条和对话框，避免历史对话里的「额度」字样把还可用的账号标掉。
  // 正则与 extension/quota.js、internal/tasks/fault.go 保持一致。
  function accountNotice(doc, pattern) {
    var nodes = doc.querySelectorAll('[role="alert"],[role="dialog"],[class*="toast"],[class*="notice"],[class*="quota"],[class*="limit"]');
    for (var i = 0; i < nodes.length; i++) {
      var text = String(nodes[i].innerText || '').replace(/\s+/g, ' ').trim();
      if (text && text.length <= 240 && pattern.test(text)) return true;
    }
    return false;
  }

  function sleep(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
})();
