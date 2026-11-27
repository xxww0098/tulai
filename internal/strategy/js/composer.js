// tulai composer 流程：找输入框 → 打开上传入口 → 让 input[type=file] 出现。
// 浏览器先等待真实文件选择器，脚本只点击当前输入区入口与本次菜单。
(function () {
  window.__tulaiComposer = window.__tulaiComposer || null;

  function visible(el) {
    if (!el) return false;
    if (el.offsetParent === null && el.tagName !== 'BODY') return false;
    return el.clientWidth > 5 && el.clientHeight > 5 && getComputedStyle(el).visibility !== 'hidden' && !el.disabled &&
      (!el.getAttribute || el.getAttribute('aria-disabled') !== 'true');
  }
  function textOf(el) { return ((el.textContent || '') + ' ' + (el.innerText || '')).trim(); }
  function normText(el) { return String((el && (el.innerText || el.textContent)) || '').replace(/\s+/g, ' ').trim(); }
  function labelOf(el) {
    return ((el.getAttribute && el.getAttribute('aria-label')) || '') + ' ' +
      ((el.getAttribute && el.getAttribute('title')) || '');
  }
  function combined(el) { return (textOf(el) + ' ' + labelOf(el)).trim(); }

  function classNameOf(el) {
    return String((el && ((el.className && el.className.baseVal) || el.className)) || '');
  }
  // 豆包会在可见输入框旁边放一份 invisible 的 ProseMirror 克隆，testid 相同；
  // Gemini 的 Quill 编辑器会附带一个隐藏的 .ql-clipboard contenteditable 元素。
  function hiddenComposer(el) {
    if (el && el.classList && el.classList.contains('ql-clipboard')) return true;
    for (var node = el, i = 0; node && i < 8; i++, node = node.parentElement) {
      if (/(^|\s)invisible(\s|$)/.test(classNameOf(node))) return true;
    }
    return false;
  }
  function editorInside(slot) {
    if (!slot || hiddenComposer(slot)) return null;
    if (slot.tagName === 'TEXTAREA' || slot.tagName === 'INPUT' || (slot.getAttribute && slot.getAttribute('contenteditable') === 'true')) return slot;
    return slot.querySelector ? slot.querySelector('.ql-editor[contenteditable="true"],textarea,[contenteditable="true"],input[type="text"]') : null;
  }
  // 最近的可编辑输入区。豆包实页是 [data-testid="chat_input_input"]，Gemini 实页是 rich-textarea .ql-editor。
  function findComposer() {
    var slots = document.querySelectorAll('[data-testid="chat_input_input"],rich-textarea .ql-editor[contenteditable="true"],rich-textarea');
    var fallback = null;
    for (var i = slots.length - 1; i >= 0; i--) {
      var editor = editorInside(slots[i]);
      if (!editor || hiddenComposer(editor)) continue;
      if (!fallback) fallback = editor;
      if (visible(editor)) return editor;
    }
    if (fallback) return fallback;
    var tas = document.querySelectorAll('textarea,[contenteditable=true],input[type=text]');
    for (var j = tas.length - 1; j >= 0; j--) {
      if (hiddenComposer(tas[j])) continue;
      if (!fallback) fallback = tas[j];
      if (tas[j].getAttribute && tas[j].getAttribute('data-slate-editor') === 'true') fallback = tas[j];
      if (visible(tas[j])) return tas[j];
    }
    return fallback;
  }
  // 仅有占位/隐藏文本时视为空；有可编辑正文时按块级节点读取，避免 ProseMirror/Slate 的 innerText 把单换行翻倍。
  window.tulaiReadComposer = function (comp) {
    if (!comp) return null;
    if (comp.tagName === 'TEXTAREA' || comp.tagName === 'INPUT') return comp.value;
    var hasVisibleText = false;
    var walker = document.createTreeWalker(comp, NodeFilter.SHOW_TEXT), text;
    while ((text = walker.nextNode())) {
      if (!String(text.nodeValue || '').replace(/\uFEFF/g, '').trim()) continue;
      var excluded = false;
      for (var node = text.parentElement; node && node !== comp; node = node.parentElement) {
        var style = getComputedStyle(node);
        if (node.getAttribute('contenteditable') === 'false' || node.getAttribute('data-slate-placeholder') === 'true' || node.getAttribute('aria-hidden') === 'true' || style.display === 'none' || style.visibility === 'hidden') { excluded = true; break; }
      }
      if (!excluded) { hasVisibleText = true; break; }
    }
    if (!hasVisibleText) return '';
    // Tiptap / ProseMirror / Slate puts each line in a block child; innerText doubles newlines between <p>s.
    var isSlateEditor = comp.getAttribute && comp.getAttribute('data-slate-editor') === 'true';
    var blockChildren = comp.children && Array.from(comp.children).filter(function (child) {
      return /^(P|DIV|LI)$/.test(child.tagName || '');
    });
    if (blockChildren && blockChildren.length > 0 && (isSlateEditor || (comp.classList && comp.classList.contains('ProseMirror')) || comp.querySelector('p'))) {
      var readNode = function (el) {
        if (!el) return '';
        if (el.nodeType === 3) return String(el.nodeValue || '').replace(/\uFEFF/g, '');
        if (el.nodeType !== 1) return '';
        if (el !== comp) {
          var st = getComputedStyle(el);
          if (el.getAttribute('contenteditable') === 'false' || el.getAttribute('data-slate-placeholder') === 'true' || el.getAttribute('aria-hidden') === 'true' || st.display === 'none' || st.visibility === 'hidden') return '';
        }
        if (el.tagName === 'BR') {
          if (el.classList && el.classList.contains('ProseMirror-trailingBreak')) return '';
          if (el.parentElement && el.parentElement.hasAttribute && el.parentElement.hasAttribute('data-slate-zero-width')) return '';
          return '\n';
        }
        if (el !== comp && el.children && Array.from(el.children).some(function (c) { return /^(P|DIV|LI)$/.test(c.tagName || ''); })) {
          var sub = [];
          for (var c2 = el.firstChild; c2; c2 = c2.nextSibling) {
            if (c2.nodeType === 1 && /^(P|DIV|LI)$/.test(c2.tagName || '')) {
              sub.push(readNode(c2));
            } else {
              var inlinePart = readNode(c2);
              if (inlinePart) sub.push(inlinePart);
            }
          }
          return sub.join('\n');
        }
        var s = '';
        for (var c = el.firstChild; c; c = c.nextSibling) s += readNode(c);
        return s;
      };
      var lines = [];
      for (var k = 0; k < comp.childNodes.length; k++) {
        var child = comp.childNodes[k];
        if (child.nodeType === 1 && /^(P|DIV|LI)$/.test(child.tagName || '')) {
          lines.push(readNode(child));
        } else {
          var chunk = readNode(child);
          if (chunk) lines.push(chunk);
        }
      }
      return lines.join('\n').replace(/\uFEFF/g, '');
    }
    var slate = comp.querySelector && comp.querySelector('[data-slate-placeholder]');
    if (!slate) return comp.innerText;
    var out = '', seen = document.createTreeWalker(comp, NodeFilter.SHOW_TEXT), leaf;
    while ((leaf = seen.nextNode())) {
      var skip = false;
      for (var parent = leaf.parentElement; parent && parent !== comp; parent = parent.parentElement) {
        if (parent.getAttribute && (parent.getAttribute('contenteditable') === 'false' || parent.getAttribute('data-slate-placeholder') === 'true' || parent.getAttribute('aria-hidden') === 'true')) {
          skip = true;
          break;
        }
      }
      if (!skip) out += leaf.nodeValue || '';
    }
    return out.replace(/\uFEFF/g, '');
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
    for (var d = 0; d < 10 && p; d++) {
      if (p.hasAttribute && p.hasAttribute('data-chat-input-body')) return p;
      if (p.classList && p.classList.contains('message-input-container')) return p;
      if (p.tagName === 'INPUT-AREA-V2' || p.tagName === 'INPUT-CONTAINER') return p;
      p = p.parentElement;
    }
    var q = comp;
    for (var e = 0; e < 8 && q; e++) {
      var btns = q.querySelectorAll && q.querySelectorAll('button,[role=button]');
      if (btns) {
        for (var b = 0; b < btns.length; b++) {
          var tx = (btns[b].textContent || '') + (btns[b].getAttribute('aria-label') || '');
          if (/发送|更多|上传|工具|Send|More|Upload|tools/i.test(tx)) return q;
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

  function syntheticPress(el) {
    if (!el || el.isConnected === false) return false;
    try {
      if (typeof el.focus === 'function') el.focus();
      var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
      var opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
      if (typeof PointerEvent === 'function') {
        el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
      }
      if (typeof MouseEvent === 'function') el.dispatchEvent(new MouseEvent('mousedown', opts));
      if (typeof PointerEvent === 'function') {
        el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
      }
      if (typeof MouseEvent === 'function') el.dispatchEvent(new MouseEvent('mouseup', opts));
      if (typeof el.click === 'function') el.click();
      return true;
    } catch (e) { return false; }
  }

  function coveringElement(el) {
    if (!el || !el.getBoundingClientRect || typeof document.elementFromPoint !== 'function') return null;
    var r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return null;
    var hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    if (!hit || el.contains(hit)) return null;
    return hit;
  }

  function hasAnimatingClosingMenu() {
    if (!document.querySelectorAll) return false;
    var nodes = document.querySelectorAll('[role="menu"][data-state="closed"],[role="listbox"][data-state="closed"]');
    for (var i = 0; i < nodes.length; i++) {
      var m = nodes[i];
      if (!m || m.isConnected === false) continue;
      var st = getComputedStyle(m);
      if (st.display === 'none' || st.visibility === 'hidden' || parseFloat(st.opacity) <= 0.05) continue;
      var r = m.getBoundingClientRect ? m.getBoundingClientRect() : { width: 0, height: 0 };
      if (r.width > 5 && r.height > 5) return true;
    }
    return false;
  }

  async function waitClosingMenus(maxMs) {
    var deadline = Date.now() + (maxMs || 400);
    while (Date.now() < deadline) {
      if (!hasAnimatingClosingMenu()) return;
      await sleep(25);
    }
  }

  async function openUploadEntry(el, directFileButton) {
    if (!el) return false;
    await waitClosingMenus(380);
    if (await clickEl(el)) return true;
    if (el.isConnected === false) return false;
    var cover = coveringElement(el);
    // 若被上一个未收起的 Radix 菜单或千问国际推荐浮层挡住，先尝试收起浮层再重走可信点击。
    if (cover) {
      var recOverlay = cover.closest && cover.closest('.chat-prompt-recommend-detail-container');
      if (recOverlay) {
        var recClose = recOverlay.querySelector && recOverlay.querySelector('button,[role="button"],[class*="close"]');
        if (recClose && visible(recClose)) {
          await clickEl(recClose);
          await sleep(60);
        }
      } else if (cover.closest && cover.closest('[role="menu"],[role="listbox"],[data-radix-popper-content-wrapper]')) {
        try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
        await sleep(120);
        await waitClosingMenus(300);
      }
      if (await clickEl(el)) return true;
    }
    // 仅对弹出二级菜单的入口（如千问国际 .mode-select-open）允许合成派发，二级菜单项本身仍走可信 tulaiClick 以触发文件选择器。
    if (!directFileButton && el.isConnected !== false && visible(el)) {
      return syntheticPress(el);
    }
    return false;
  }

  var MODE_KW = ['AI生图', 'AI生视频', 'PPT创作', 'AI写作', '智能体', '模板', '海报', '电商', '人像', '视频生成', '音乐', '翻译', '画图', '代码', '千问高考', '办公助理', '研究', '生成图像', '生成圖像', 'Create Image', 'Create image', '制作图片', '生成图片', '创建视频', 'Create Video', 'Create video', '制作视频', '制作音乐', 'Create music', '网页开发', 'Web Dev', 'Agent 模式', 'Deep Research', 'Canvas', '在 Google 上搜索', '从云端硬盘添加', 'Add from Drive'];

  function isMode(t) {
    for (var i = 0; i < MODE_KW.length; i++) if (t.indexOf(MODE_KW[i]) >= 0) return true;
    return false;
  }
  function cls(el) { return ((el && el.className) || '').toString(); }
  function isDisabledItem(el) {
    if (!el) return true;
    if (el.disabled || (el.getAttribute && el.getAttribute('aria-disabled') === 'true')) return true;
    // Angular Material 在可用菜单项上也会挂 mat-mdc-tooltip-disabled，需剔除后再判断禁用态。
    var c = cls(el).replace(/\b[\w-]*tooltip-disabled\b/gi, '');
    return /(?:^|[\s_-])disabled(?:$|[\s_-])/i.test(c);
  }

  // 上传面板：弹出的菜单/浮层
  function findUploadPanel(comp, excluded) {
    var cr = comp ? comp.getBoundingClientRect() : null;
    var all = document.querySelectorAll('[role=menu],[role=listbox],[role=dialog],[data-radix-popper-content-wrapper],[popover],.qwen-chat-v2-dropdown-menu-popup,.mat-mdc-menu-panel');
    for (var i = 0; i < all.length; i++) {
      var el = all[i];
      if (!visible(el) || (excluded && excluded.has(el))) continue;
      var r = el.getBoundingClientRect();
      if (r.width < 40 || r.height < 40) continue;
      var isMatMenu = el.tagName === 'MAT-ACTION-LIST' || (el.classList && el.classList.contains('mat-mdc-menu-panel')) || !!(el.closest && el.closest('.cdk-overlay-container'));
      if (!isMatMenu && cr && r.left < cr.left - 120) continue;
      var tx = el.textContent || '';
      if (!/参考图|上传|本地|从相册|选择|导入|我的文件|图片|文件|附件|upload|attachment|add files|photo|image/i.test(tx)) continue;
      return el;
    }
    return null;
  }

  function fileCount() { return document.querySelectorAll('input[type=file]').length; }

  function attachmentScope(comp, uploadEntry) {
    var surface = comp.closest('.guidance-input-surface') || comp.closest('.message-input-container') || comp.closest('input-area-v2') || comp.closest('input-container');
    if (surface && (!uploadEntry || surface.contains(uploadEntry))) return surface;
    var scope = findScope(comp), peer = uploadEntry || findSendButton(comp);
    if (peer) {
      for (var p = comp.parentElement; p && p !== document.body; p = p.parentElement) {
        if (p.contains(peer)) return scope !== document && scope !== document.body && scope.contains(p) ? scope : p;
      }
    }
    return scope === document || scope === document.body || scope === document.documentElement ? null : scope;
  }


  function findUploadConsentConfirm() {
    var dialogs = document.querySelectorAll('mat-dialog-container,.mat-mdc-dialog-container,[role="dialog"],[role="alertdialog"]');
    for (var i = dialogs.length - 1; i >= 0; i--) {
      var d = dialogs[i];
      if (!visible(d)) continue;
      var txt = combined(d);
      if (!/creating content from images|prohibited use policy|rights to any images|使用图片和文件|生成内容.*权利|禁止使用政策|图片和文件创作/i.test(txt)) continue;
      var btns = d.querySelectorAll('button,[role="button"]');
      for (var j = btns.length - 1; j >= 0; j--) {
        var b = btns[j];
        if (!visible(b) || isDisabledItem(b)) continue;
        var bt = normText(b) || labelOf(b).replace(/\s+/g, ' ').trim();
        if (/^(?:agree|accept|continue|i agree|got it|ok|同意|接受|继续|确认|知道了)$/i.test(bt)) return b;
      }
    }
    return null;
  }

  async function maybeAcceptUploadConsent(log) {
    for (var deadline = Date.now() + 900; Date.now() < deadline;) {
      if (window.__tulaiChooserOpened) return false;
      var confirmBtn = findUploadConsentConfirm();
      if (confirmBtn) {
        log.push('consent-agree=' + (textOf(confirmBtn) || 'Agree'));
        await clickEl(confirmBtn);
        for (var after = Date.now() + 1200; Date.now() < after;) {
          if (window.__tulaiChooserOpened) return true;
          if (!findUploadConsentConfirm()) break;
          await sleep(30);
        }
        return true;
      }
      await sleep(30);
    }
    return false;
  }

  // 只打开当前编辑器的真实上传动作，文件由本轮浏览器 chooser 接收。
  window.tulaiOpenUpload = async function () {
    window.__tulaiChooserOpened = false;
    var comp = findComposer(), log = [];
    window.__tulaiUploadDiagnostic = log;
    if (!comp) throw new Error('未找到输入框');
    var entry = null, directFileButton = false;
    for (var scope = comp.parentElement; scope && scope !== document.body; scope = scope.parentElement) {
      var exact = scope.querySelector('button[data-testid="upload_file_button"]');
      if (visible(exact)) { entry = exact; break; }
    }
    // Gemini（gemini.google.com）的「+」按钮位于 simplified-input-menu 内，点击弹出上传与工具菜单。
    for (scope = comp.parentElement; !entry && scope && scope !== document.body; scope = scope.parentElement) {
      var gemBtn = scope.querySelector('simplified-input-menu button[aria-haspopup="menu"],gem-icon-button.gem-menu-button button,button[aria-label*="Upload & tools" i],button[aria-label*="上传和工具"],button[aria-label*="Add files" i],button[aria-label*="添加文件"]');
      if (visible(gemBtn)) {
        entry = gemBtn;
        directFileButton = false;
        break;
      }
    }
    // 千问「AI生图」模式提供直出文件选择器的「参考图」按钮，避开 Radix 下拉菜单动画竞态。
    for (scope = comp.parentElement; !entry && scope && scope !== document.body; scope = scope.parentElement) {
      var refBtn = scope.querySelector('button[aria-label="参考图"],[role="button"][aria-label="参考图"]');
      if (visible(refBtn) && !refBtn.getAttribute('aria-haspopup')) {
        entry = refBtn;
        directFileButton = true;
        break;
      }
      var refCandidates = scope.querySelectorAll('button,[role=button]');
      for (var ri = 0; ri < refCandidates.length; ri++) {
        var rc = refCandidates[ri];
        if (visible(rc) && !rc.getAttribute('aria-haspopup') && String(rc.innerText || rc.textContent || '').replace(/\s+/g, ' ').trim() === '参考图') {
          entry = rc;
          directFileButton = true;
          break;
        }
      }
    }
    for (scope = comp.parentElement; !entry && scope && scope !== document.body; scope = scope.parentElement) {
      var buttons = scope.querySelectorAll('button,[role=button]');
      for (var i = 0; i < buttons.length; i++) {
        if (visible(buttons[i]) && /添加附件/.test(combined(buttons[i]))) { entry = buttons[i]; break; }
      }
    }
    // 千问国际（chat.qwen.ai）的「+」按钮 .mode-select-open 位于 #filesUpload 同级容器内但没有 aria-haspopup，
    // 点击后会弹出 .qwen-chat-v2-dropdown-menu-popup 菜单，再点「上传附件 / Upload attachment」才触发文件选择器。
    for (scope = comp.parentElement; !entry && scope && scope !== document.body; scope = scope.parentElement) {
      var modeOpen = scope.querySelector('.mode-select-open,[role="button"][aria-label="Select Mode"],[role="button"][aria-label="选择模式"],button[aria-label="Select Mode"],button[aria-label="选择模式"]');
      if (visible(modeOpen)) {
        entry = modeOpen;
        directFileButton = false;
        break;
      }
    }
    // 豆包图像生成模式去掉了 upload_file_button testid，把按钮与 upload-file-input 放在同一 36×36 容器内。
    for (scope = comp.parentElement; !entry && scope && scope !== document.body; scope = scope.parentElement) {
      var fileInputs = scope.querySelectorAll('input[type="file"][data-testid="upload-file-input"],input[type="file"]');
      for (var f = 0; f < fileInputs.length && !entry; f++) {
        var inp = fileInputs[f];
        var accept = String(inp.getAttribute('accept') || '');
        if (accept && !/image|png|jpe?g|webp|gif|\*/i.test(accept)) continue;
        var wrap = inp.parentElement;
        if (!wrap || wrap === document.body) continue;
        var peers = wrap.querySelectorAll('button,[role=button]');
        for (var p = 0; p < peers.length; p++) {
          if (visible(peers[p])) {
            entry = peers[p];
            var isModeMenu = (entry.classList && entry.classList.contains('mode-select-open')) || /select mode|选择模式/i.test(labelOf(entry));
            directFileButton = !isModeMenu && !entry.getAttribute('aria-haspopup');
            break;
          }
        }
      }
    }
    if (!entry) throw new Error('未找到当前输入区上传入口');
    var previewScope = attachmentScope(comp, entry), initialImages = new Map();
    Array.from(document.images).forEach(function (img) { initialImages.set(img, img.currentSrc || img.src); });
    var doubaoBaseline = new Set(document.querySelectorAll('img,[data-testid="attachment-image-card"],uploader-file-preview'));
    window.__tulaiUploadState = { scope: previewScope, entry: entry, images: initialImages, doubaoBaseline: doubaoBaseline, files: new Map(Array.from(document.querySelectorAll('input[type=file]')).map(function(input){return [input,input.files]})) };
    log.push('scope=' + (previewScope ? previewScope.tagName + '#' + (previewScope.id || '').slice(0, 40) + '.' + cls(previewScope).slice(0, 60) + '; role=' + (previewScope.getAttribute('role') || '').slice(0, 40) : 'none'));
    log.push('entry=' + entry.tagName + '; testid=' + (entry.getAttribute('data-testid') || '').slice(0, 60) + '; id=' + (entry.id || '').slice(0, 50) + '; aria=' + (entry.getAttribute('aria-label') || '').slice(0, 60) + '; direct=' + directFileButton);
    var oldPanels = new Set(Array.from(document.querySelectorAll('[role=menu],[role=listbox],[role=dialog],[data-radix-popper-content-wrapper],[popover],.qwen-chat-v2-dropdown-menu-popup,.mat-mdc-menu-panel')).filter(visible));
    if (!await openUploadEntry(entry, directFileButton)) throw new Error('点击上传入口失败');
    if (directFileButton || window.__tulaiChooserOpened) {
      return JSON.stringify({ opened: true });
    }
    // 直接打开 chooser 时没有菜单；有菜单时只处理本次新出现的面板。
    var panel = null;
    for (var deadline = Date.now() + 1800; Date.now() < deadline;) {
      if (window.__tulaiChooserOpened) return JSON.stringify({ opened: true });
      panel = findUploadPanel(comp, oldPanels);
      if (panel && !oldPanels.has(panel)) break;
      panel = null;
      await sleep(20);
    }
    var menuClicked = false;
    if (panel && !window.__tulaiChooserOpened) {
      if (panel.tagName === 'GEM-MENU' || (panel.getAttribute && (panel.getAttribute('data-state') === 'open' || panel.hasAttribute('data-radix-popper-content-wrapper') || (panel.closest && panel.closest('[data-radix-popper-content-wrapper]'))))) {
        for (var readyUntil = Date.now() + 260; Date.now() < readyUntil;) {
          if (window.__tulaiChooserOpened) return JSON.stringify({ opened: true });
          var op = parseFloat(getComputedStyle(panel).opacity || '1');
          var pr = panel.getBoundingClientRect ? panel.getBoundingClientRect() : { width: 100, height: 100 };
          if ((isNaN(op) || op > 0.5) && pr.height > 40) break;
          await sleep(15);
        }
      }
      var gemMenuUpload = Array.from(panel.querySelectorAll('gem-menu-item[role="menuitem"],gem-menu-item')).find(function (el) {
        if (!visible(el) || isDisabledItem(el)) return false;
        if (el.querySelector && el.querySelector('mat-icon[fonticon="attach_file"],mat-icon[data-mat-icon-name="attach_file"]')) return true;
        return /^(?:上传文件|上传图片|Upload files?|Upload images?)$/i.test(normText(el));
      });
      if (gemMenuUpload && !window.__tulaiChooserOpened) {
        log.push('menu-item=gemini-menu-upload');
        if (!await clickEl(gemMenuUpload)) throw new Error('点击上传菜单失败');
        menuClicked = true;
      }
      var gemUploadItem = !menuClicked && panel.querySelector && panel.querySelector('button[data-test-id="local-images-files-uploader-button"]');
      if (gemUploadItem && visible(gemUploadItem) && !isDisabledItem(gemUploadItem) && !window.__tulaiChooserOpened) {
        var gr = gemUploadItem.getBoundingClientRect ? gemUploadItem.getBoundingClientRect() : { width: 0, height: 0 };
        var hiddenWrap = gemUploadItem.closest && gemUploadItem.closest('.hidden-uploader');
        if (!hiddenWrap && gr.width > 24 && gr.height > 16) {
          log.push('menu-item=gemini-local-upload');
          if (!await clickEl(gemUploadItem)) throw new Error('点击上传菜单失败');
          menuClicked = true;
        }
      }
      if (!menuClicked) {
        var items = panel.querySelectorAll('[role=menuitem],[role=option],.qwen-chat-v2-dropdown-menu-item,button,a');
        for (var j = 0; j < items.length; j++) {
          var item = items[j], label = textOf(item), text = combined(item);
          if (!visible(item) || isDisabledItem(item)) continue;
          if (isMode(label || text) || !/本地图片|上传图片|本地上传|上传文件|上传附件|选择图片|选择文件|upload attachment|upload image|upload files?|add files?|attach file/i.test(text)) continue;
          if (window.__tulaiChooserOpened) return JSON.stringify({ opened: true });
          log.push('menu-item=' + item.tagName + '#' + (item.id || '').slice(0, 50));
          if (!await clickEl(item)) throw new Error('点击上传菜单失败');
          menuClicked = true;
          break;
        }
      }
    }
    // Gemini 首次上传会先弹出「Creating content from images and files / 同意」确认框，点同意后若仍未弹 chooser 则再点一次上传项。
    if (menuClicked && !window.__tulaiChooserOpened) {
      var agreed = await maybeAcceptUploadConsent(log);
      if (agreed && !window.__tulaiChooserOpened) {
        await sleep(150);
        if (!window.__tulaiChooserOpened) {
          var retryPanel = findUploadPanel(comp, oldPanels);
          if (!retryPanel && visible(entry)) {
            await openUploadEntry(entry, directFileButton);
            for (var rd = Date.now() + 800; Date.now() < rd && !window.__tulaiChooserOpened;) {
              retryPanel = findUploadPanel(comp, oldPanels);
              if (retryPanel && !oldPanels.has(retryPanel)) break;
              retryPanel = null;
              await sleep(20);
            }
          }
          if (retryPanel && !window.__tulaiChooserOpened) {
            var retryGem = Array.from(retryPanel.querySelectorAll('gem-menu-item[role="menuitem"],gem-menu-item,button[data-test-id="local-images-files-uploader-button"]')).find(function (el) {
              if (!visible(el) || isDisabledItem(el)) return false;
              if (el.closest && el.closest('.hidden-uploader')) return false;
              if (el.querySelector && el.querySelector('mat-icon[fonticon="attach_file"],mat-icon[data-mat-icon-name="attach_file"]')) return true;
              return /^(?:上传文件|上传图片|Upload files?|Upload images?)$/i.test(normText(el));
            });
            if (retryGem) {
              log.push('menu-item=gemini-menu-upload-after-consent');
              await clickEl(retryGem);
            }
          }
        }
      }
    }
    return JSON.stringify({ opened: true });
  };

  function isSvgPlaceholderSrc(src) {
    var value = String(src || '').trim();
    return /^data:image\/svg/i.test(value) || /\.svg(?:[?#]|$)/i.test(value);
  }

  window.tulaiWaitForUpload = async function (expected) {
    var deadline = Date.now() + 30000, stable = 0, count = 0, busy = false;
    var state = window.__tulaiUploadState;
    while (Date.now() < deadline) {
      var comp = findComposer(), scope = comp && attachmentScope(comp, state && state.entry);
      if (state && state.scope && state.scope.isConnected && state.scope.contains(comp)) scope = state.scope;
      if (!comp || !state || !scope) break;
      busy = Array.from(scope.querySelectorAll('[aria-busy="true"],[role="progressbar"],.qwpcicon-loading2,.vision-item-loading,mat-progress-spinner,mat-progress-bar,.gem-attachment-content.loading')).some(visible);
      var newCards = Array.from(scope.querySelectorAll('[data-testid="attachment-image-card"],uploader-file-preview')).filter(function (card) {
        return visible(card) && (!state.doubaoBaseline || !state.doubaoBaseline.has(card)) && !(card.querySelector && card.querySelector('mat-progress-spinner,mat-progress-bar,.loading'));
      }).length;
      count = Array.from(scope.querySelectorAll('img')).filter(function (img) {
        var src = img.currentSrc || img.src;
        return src && !isSvgPlaceholderSrc(src) && visible(img) && img.complete && img.naturalWidth > 0 && (!state.images.has(img) || state.images.get(img) !== src);
      }).length;
      if (newCards > count) count = newCards;
      if (expected === 1 && !count && state.doubaoBaseline && window.__tulaiWashPage && window.__tulaiWashPage.freshAttachment(document, state.doubaoBaseline)) count = 1;
      if (!busy && count >= expected) {
        if (++stable >= 2) return JSON.stringify({ ready: true });
      } else stable = 0;
      await sleep(30);
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
    window.__tulaiComposer = comp;
    return true;
  };

  function isStopButton(el) {
    if (!el) return false;
    if (el.classList && el.classList.contains('stop')) return true;
    if (el.querySelector && el.querySelector('mat-icon[fonticon="stop"],[data-mat-icon-name="stop"],.stop-icon')) return true;
    return /停止响应|停止生成|Stop response|Stop generating/i.test(labelOf(el));
  }
  function shown(el) {
    if (!el) return false;
    if (visible(el)) return true;
    if (el.classList && el.classList.contains('send-button') && !isStopButton(el)) return true;
    var target = el.getAttribute && el.getAttribute('data-session-switch-target');
    if (target === 'send-query') return true;
    return !!(el.getAttribute && el.getAttribute('data-testid') === 'chat_input_send_button');
  }
  function sendEnabled(el) {
    return shown(el) && !isStopButton(el) && !el.disabled && (!el.getAttribute || el.getAttribute('aria-disabled') !== 'true');
  }
  function findSendButton(comp) {
    // 上传区/“更多”可能只包住编辑器，发送按钮在其外层兄弟中。
    // 千问箭头是 data-session-switch-target=send-query，千问国际是 button.send-button，Gemini 是 arrow_upward / aria-label="发送"/"Send message"。
    for (var scope = comp.parentElement; scope; scope = scope.parentElement) {
      var buttons = scope.querySelectorAll('button,[role=button]');
      for (var i = buttons.length - 1; i >= 0; i--) {
        var el = buttons[i];
        if (!shown(el) || isStopButton(el)) continue;
        var hasSendIcon = !!(el.querySelector && el.querySelector('mat-icon[fonticon="arrow_upward"],mat-icon[fonticon="send"],[data-mat-icon-name="arrow_upward"],[data-mat-icon-name="send"]'));
        if ((el.classList && el.classList.contains('send-button')) || el.getAttribute('data-session-switch-target') === 'send-query' || el.getAttribute('data-testid') === 'chat_input_send_button' || hasSendIcon || /发送|发消息|\bsend\b/i.test(combined(el))) return el;
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
      if (i >= 0 && parts[i + 1]) return parts[i + 1];
      var c = parts.indexOf('c');
      if (c >= 0 && parts[c + 1] && !/^(new-chat|new-branch|guest|local)$/i.test(parts[c + 1])) return parts[c + 1];
      var a = parts.indexOf('app');
      if (a >= 0 && parts[a + 1] && !/^(new|images|download|gems|settings|mystuff)$/i.test(parts[a + 1])) return parts[a + 1];
      return '';
    } catch (e) { return ''; }
  }

  // 实页用户气泡是 send_message[data-message-role=user] / user-query。旧夹具只写了 role。
  function userBubbles() {
    var nodes = document.querySelectorAll('[data-testid="send_message"][data-message-role="user"]');
    if (nodes.length) return nodes;
    return document.querySelectorAll('[data-message-author-role="user"],[data-message-role="user"],.chat-question-card-wrap,.chat-user-message,user-query,[data-test-id="user-query"],.user-query-container');
  }
  function promptMatches(prompt) {
    var want = String(prompt || '').replace(/\s+/g, '');
    if (!want) return 0;
    var nodes = userBubbles();
    var n = 0;
    for (var i = 0; i < nodes.length; i++) {
      var text = String(nodes[i].innerText || nodes[i].textContent || '').replace(/\s+/g, '');
      if (text.indexOf(want) !== -1) n++;
    }
    return n;
  }
  // 发送成功要看到一条新的用户气泡（正文含这次提示词），或地址变成新的 /chat/<id> 或 /c/<id>。输入框空了不算。
  // 国内千问复用标签时，首页壳会把地址写回上一轮的 /chat/<id>，问答卡里却没有这次提示词。那不是发出去了。
  window.tulaiSendAccepted = function (beforeHref, prompt, beforeMatches) {
    var href = String(location.href || '');
    var nowID = conversationKey(href);
    var thenID = conversationKey(beforeHref);
    var floor = typeof beforeMatches === 'number' ? beforeMatches : 0;
    if (promptMatches(prompt) > floor) return true;
    if (!(nowID && nowID !== thenID)) return false;
    var host = String((window.__tulaiPageHost || location.hostname) || '');
    if (!/(^|\.)(qianwen|tongyi)\.com$/i.test(host)) return true;
    return document.querySelectorAll('.chat-question-card-wrap').length === 0;
  };

  // InsertText 在失焦窗口里可能只改了 DOM。再用原生 setter 派发 input，React 才会打开发送。
  function commitComposer(comp) {
    if (!comp) return;
    try { comp.focus(); } catch (e) {}
    if (comp.tagName !== 'TEXTAREA' && comp.tagName !== 'INPUT') return;
    var proto = comp.tagName === 'TEXTAREA' ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    var desc = Object.getOwnPropertyDescriptor(proto, 'value');
    if (desc && desc.set) desc.set.call(comp, comp.value);
    try {
      comp.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: comp.value }));
    } catch (e) {
      comp.dispatchEvent(new Event('input', { bubbles: true }));
    }
  }

  function realSendButton(comp) {
    var exact = document.querySelector('button[data-testid="chat_input_send_button"]');
    if (exact && !hiddenComposer(exact) && !isStopButton(exact)) return exact;
    var qwenIntlSend = document.querySelector('.chat-prompt-send-button button.send-button:not(.stop),button.send-button:not(.stop)');
    if (qwenIntlSend && !hiddenComposer(qwenIntlSend) && !isStopButton(qwenIntlSend)) return qwenIntlSend;
    return comp ? findSendButton(comp) : null;
  }

  window.tulaiSendPrompt = async function () {
    var comp = window.__tulaiComposer;
    if (!comp || !comp.isConnected) return JSON.stringify({ attempted: false, error: '未找到已定位的输入框' });
    commitComposer(comp);
    var deadline = Date.now() + 5000, button;
    while (Date.now() < deadline) {
      if (!comp.isConnected) comp = findComposer();
      if (!comp) return JSON.stringify({ attempted: false, error: '输入框已消失' });
      button = realSendButton(comp);
      if (sendEnabled(button)) break;
      button = null;
      await sleep(30);
    }
    var beforeHref = String(location.href || '');
    var promptText = window.tulaiReadComposer(comp);
    var beforeMatches = promptMatches(promptText);
    var attempted = false;
    if (button) {
      button.scrollIntoView({ block: 'center', inline: 'center' });
      var rect = button.getBoundingClientRect();
      var hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
      // Choose one trusted action before sending; never retry an uncertain click with Enter.
      if ((!hit || !button.contains(hit)) && window.tulaiPressEnter) {
        comp.focus();
        if (document.activeElement !== comp) return JSON.stringify({ attempted: false, error: '发送按钮被遮挡，输入框无法获得焦点' });
        attempted = true;
        try { await window.tulaiPressEnter(); } catch (e) {}
      } else {
        attempted = true;
        try {
          if (window.tulaiClick) await window.tulaiClick(button);
          else button.click();
        } catch (e) {}
      }
    }
    if (!attempted && !button) return JSON.stringify({ attempted: false, error: '未找到发送按钮', diagnostic: sendDiagnostic(comp) });
    deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      if (window.tulaiSendAccepted(beforeHref, promptText, beforeMatches)) {
        var note = window.__tulaiQwenNote ? __tulaiQwenNote() : {};
        return JSON.stringify({ sent: true, chat: (note && note.url) || '', wrap: (note && note.wrap) || '' });
      }
      await sleep(30);
    }
    var latest = findComposer();
    var remaining = window.tulaiReadComposer(latest);
    var missed = window.__tulaiQwenNote ? __tulaiQwenNote() : {};
    return JSON.stringify({ attempted: true, error: '平台未接受消息：发送后没有出现用户消息，地址也没有变成对话', chat: (missed && missed.url) || '', wrap: (missed && missed.wrap) || '', diagnostic: ['accept composer=' + (latest ? latest.tagName : 'none') + '; chars=' + String(remaining || '').trim().length + '; nodeChanged=' + (latest !== comp) + '; oldConnected=' + comp.isConnected].concat(window.tulaiComposerDiagnostic(latest)) });
  };

  function cleanScope(comp) {
    if (!comp) return null;
    if (comp.closest) {
      var surface = comp.closest('.guidance-input-surface') || comp.closest('[data-testid="chat_input"]') || comp.closest('.message-input-container') || comp.closest('input-area-v2') || comp.closest('input-container');
      if (surface) return surface;
    }
    var scope = findScope(comp);
    if (scope && scope !== document && scope !== document.body && scope !== document.documentElement) {
      for (var p = scope, d = 0; p && p !== document.body && p !== document.documentElement && d < 3; p = p.parentElement, d++) {
        if (p.querySelector && p.querySelector('[class*="imageBoxWrapper"],[data-testid="attachment-image-card"],.vision-item-container,.message-input-column-file,uploader-file-preview')) {
          return p;
        }
      }
      return scope;
    }
    return null;
  }

  var CLEAN_DEL_SEL = '[data-testid="attachment-delete-btn"],[aria-label="delete" i],[class*="imageBoxWrapper"] [data-icon-type="qwpcicon-close2"],[class*="imageBoxWrapper"] [class*="closeIcon"],.message-input-column-file .icon-close,.vision-item-container .icon-close,.file-card-list [class*="close"],[class*="preview"] [class*="delete"],[class*="preview"] [class*="remove"],[class*="image-container"] [class*="close"],uploader-file-preview .cancel-button,uploader-file-preview button[aria-label*="remove" i],uploader-file-preview button[aria-label*="cancel" i],uploader-file-preview button[aria-label*="取消"],uploader-file-preview-container button[data-test-id*="cancel"],uploader-file-preview-container button[data-test-id*="remove"],[aria-label*="删除" i],[aria-label*="移除" i]';

  // 文本由可信 InsertText 全文替换，只清理输入区附件。
  window.tulaiCleanComposer = async function () {
    var comp = findComposer();
    if (!comp) return false;
    var scope = cleanScope(comp);
    if (!scope || scope === document || scope === document.body || scope === document.documentElement) return true;
    // 移除已上传的预览缩略图（关闭按钮）；逐张点击并重新查询以适配 React 异步重绘列表。
    var clicked = typeof WeakSet === 'function' ? new WeakSet() : null;
    var prevCount = -1;
    for (var step = 0; step < 20; step++) {
      var dels = Array.from(scope.querySelectorAll(CLEAN_DEL_SEL));
      if (!dels.length) break;
      if (prevCount >= 0 && dels.length < prevCount && clicked) clicked = new WeakSet();
      prevCount = dels.length;
      var target = null;
      for (var i = 0; i < dels.length; i++) {
        if (!clicked || !clicked.has(dels[i])) { target = dels[i]; break; }
      }
      if (!target) break;
      if (clicked) clicked.add(target);
      try {
        if (!await clickEl(target) && typeof target.dispatchEvent === 'function') {
          target.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
        }
      } catch (e) {}
      if (typeof target.isConnected === 'boolean') await sleep(30);
    }
    return true;
  };

  // 正则与 extension/quota.js、internal/tasks/fault.go 保持一致。
  var QUOTA_RE = /额度不足|额度已用尽|额度已用|额度用尽|额度用完|次数已用完|次数已用尽|次数用完|免费次数已|已达上限|已达到?.{0,8}(?:生成|创作)?上限|超出.{0,8}限额|生成次数已|积分不足|免费额度已用完|今日对话次数已达上限|(?:今日|今天|当日|本周|本月|(?:最?近\s*)?\d+\s*天).{0,12}(?:额度|次数).{0,8}(?:用完|用尽|耗尽|达上限|已达上限)|quota exceeded|daily limit|not enough credits|insufficient credits|out of credits|daily free allowance|today['’]s chat limit has been reached|already use(?:d)? up .{0,24}credits|reached (?:your |the )?(?:daily |image )?(?:generation |creation )?limit|image generation limit/i;
  var REPLY_QUOTA_RE = /^(?:(?:很)?抱歉[，,：:\s]*)?(?:(?:您|你)的?\s*)?(?:(?:当前)?(?:创作)?积分不足|(?:今日|今天|当日|本周|本月|(?:最?近\s*)?\d+\s*天)?\s*(?:图片生成|图像生成|生图|绘图|对话)?\s*(?:免费)?\s*(?:生成)?(?:额度|次数)(?:已)?(?:用完|用尽|耗尽|达上限|已达上限)|已达到?(?:图片|图像)?(?:生成|创作)?上限)(?:[，,。.!！;；\s]|$)/;
  var REPLY_QUOTA_EN_RE = /^(?:sorry[,:\s]*)?(?:not enough credits|insufficient credits|out of credits|today['’]s chat limit has been reached|you(?:'ve| have)?\s+(?:already\s+)?(?:use(?:d)?\s+up\b.{0,40}(?:credits|daily free allowance)|reached (?:your |the )?(?:daily |image )?(?:generation |creation )?limit)|daily free allowance\b.{0,24}(?:reached|used up|exhausted))/i;
  var RATE_LIMIT_RE = /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests|sending too many requests|operation is too frequent|too many files uploaded|too many requests in the queue|chat limited|global limited|file upload limited/i;

  function replyQuota(text) {
    var lines = String(text || '').split('\n');
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].replace(/\s+/g, ' ').trim();
      if (line && (REPLY_QUOTA_RE.test(line) || REPLY_QUOTA_EN_RE.test(line)) && !/[?？]/.test(line.split(/[。.!！]/)[0])) return true;
    }
    return false;
  }

  function doubaoTurnReplyText() {
    try {
      if (window.__tulaiWashPage && typeof window.__tulaiWashPage.assistantText === 'function' && window.__tulaiDoubaoTurn) {
        return window.__tulaiWashPage.assistantText(document, window.__tulaiDoubaoTurn) || '';
      }
    } catch (e) {}
    return '';
  }

  // 仅读取额度/限流/审核信号，避免发送前读取整页 document.body.innerText 触发同步回流。
  window.tulaiAccountFault = function () {
    var rateLimit = accountNotice(document, RATE_LIMIT_RE);
    var quota = accountNotice(document, QUOTA_RE);
    if (!quota && (replyQuota(freshReplyText()) || replyQuota(doubaoTurnReplyText()))) {
      var body = (document.body && document.body.innerText) || '';
      if (!/生成中|正在生成|绘制中|Drawing|Generating/i.test(body)) quota = true;
    }
    return { quota: quota, rateLimit: rateLimit, moderation: refusalText() };
  };

  // 页面状态探测
  window.tulaiPageState = function () {
    var body = (document.body && document.body.innerText) || '';
    var generating = /生成中|正在生成|绘制中|Drawing|Generating/i.test(body);
    return {
      hasComposer: !!findComposer(),
      fileCount: fileCount(),
      generating: generating,
      failed: /生成失败|请重试|敏感|违规/i.test(body),
      quota: accountNotice(document, QUOTA_RE) || (!generating && (replyQuota(freshReplyText()) || replyQuota(doubaoTurnReplyText()))),
      rateLimit: accountNotice(document, RATE_LIMIT_RE),
      moderation: refusalText(),
    };
  };

  // 只看这一轮新出现的助手回复，加上短提示条。话术表在 extension/refuse.js。
  function refusalText() {
    if (!window.TULAIRefuse) return '';
    return window.TULAIRefuse.match(doubaoTurnReplyText()) || window.TULAIRefuse.match(freshReplyText()) || window.TULAIRefuse.match(noticeBlob(document)) || '';
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
