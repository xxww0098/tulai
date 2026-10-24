// Read-only identification/state for the Ego SDK path. Actions stay in the SDK.
(function (action, args) {
  var state = window.__xbsEgoState || (window.__xbsEgoState = {});
  var assistantQuery = '[data-message-author-role="assistant"],[data-message-role="assistant"],[data-role="assistant"],[data-author="assistant"]';
  var userQuery = '[data-message-author-role="user"],[data-message-role="user"],[data-role="user"],[data-author="user"]';
  if (/(^|\.)(tongyi|qianwen)\.com$/.test(location.hostname) && !document.querySelectorAll(assistantQuery + ',' + userQuery).length) {
    assistantQuery += ',.chat-answers-card-wrap';
    userQuery += ',.chat-question-card-wrap';
  }
  var panelQuery = '[role="dialog"],dialog[open],[aria-modal="true"]';
  function visible(el) {
    if (!el || !el.isConnected) return false;
    var r = el.getBoundingClientRect(), style = getComputedStyle(el);
    return r.width > 5 && r.height > 5 && style.display !== 'none' && style.visibility !== 'hidden';
  }
  function enabled(el) { return visible(el) && !el.disabled && el.getAttribute('aria-disabled') !== 'true'; }
  function all(query, root) { return Array.from((root || document).querySelectorAll(query)); }
  function composer() { return all('textarea,[contenteditable="true"],input[type="text"]').filter(enabled).pop() || null; }
  function label(el) { return ((el.getAttribute('aria-label') || '') + ' ' + (el.getAttribute('title') || '') + ' ' + (el.innerText || '')).trim(); }
  function stableSelector(el) {
    if (!el) return '';
    var tag = el.tagName.toLowerCase();
    var attributes = ['data-testid', 'data-test-id', 'aria-label', 'name', 'title'];
    for (var i = 0; i < attributes.length; i++) {
      var value = el.getAttribute(attributes[i]);
      if (!value) continue;
      var candidate = tag + '[' + attributes[i] + '=' + JSON.stringify(value) + ']';
      if (all(candidate).length === 1) return candidate;
    }
    if (el.id && all('#' + CSS.escape(el.id)).length === 1) return '#' + CSS.escape(el.id);
    return '';
  }
  function selector(el) {
    if (!el) return '';
    var stable = stableSelector(el);
    if (stable) return stable;
    var path = [];
    for (var node = el; node && node !== document.documentElement; node = node.parentElement) {
      if (node !== el && (stable = stableSelector(node))) return stable + '>' + path.join('>');
      var tag = node.tagName.toLowerCase(), index = 1;
      for (var prev = node.previousElementSibling; prev; prev = prev.previousElementSibling) if (prev.tagName === node.tagName) index++;
      path.unshift(tag + ':nth-of-type(' + index + ')');
    }
    return 'html>' + path.join('>');
  }
  function attrs(el) {
    if (!el) return 'none';
    return el.tagName + ' ' + ['id','class','data-testid','data-role','data-message-role','data-message-author-role','role','aria-label','title'].map(function (name) {
      var value = String(el.getAttribute(name) || '').replace(/https?:\/\/[^\s]+/g, '[url]').slice(0, name === 'class' ? 60 : 45);
      return value ? name + '=' + value : '';
    }).filter(Boolean).join(';');
  }
  function entryDiagnostic(entry) {
    if (!entry) return ['entry=none'];
    var rect = entry.getBoundingClientRect(), x = rect.left + rect.width / 2, y = rect.top + rect.height / 2;
    var hit = document.elementFromPoint(x, y);
    return [
      'entry=' + attrs(entry) + ';connected=' + entry.isConnected + ';enabled=' + enabled(entry),
      'entry rect=' + [rect.left, rect.top, rect.width, rect.height].map(function (value) { return Math.round(value); }).join(',') + ';centerHit=' + !!(hit && entry.contains(hit)) + ';hit=' + attrs(hit)
    ].map(function (line) { return line.slice(0, 320); });
  }
  function qwenImageItem(item) {
    var text = String(item.innerText || '').trim();
    return /^上传图片(?:\s|$)/.test(text) || all('span,p,div', item).some(function (label) { return String(label.innerText || '').trim() === '上传图片'; });
  }
  function uploadMenuDiagnostic() {
    var items = all('[role="menuitem"]').filter(function (item) { return visible(item) && /^上传(?:图片|文档)/.test(String(item.innerText || '').trim()); });
    return items.slice(0, 4).map(function (item, index) {
      var text = String(item.innerText || '').replace(/https?:\/\/[^\s]+/g, '[url]').replace(/\s+/g, ' ').trim().slice(0, 140);
      return ('uploadMenu#' + index + ' ' + attrs(item).slice(0, 150) + ';label=' + text).slice(0, 320);
    });
  }
  function controls(root) { return all('button,[role="button"],a[download]', root).filter(visible); }
  function diagnostics(root) { return controls(root).slice(-8).map(function (el) { return attrs(el).slice(0, 320); }); }
  function imageDiagnostic(images) {
    var lines = [];
    images.slice(-2).forEach(function (img, index) {
      lines = lines.concat(imageState(img, 'image#' + index));
      for (var node = img, depth = 0; node && node !== document.body && depth < 15; node = node.parentElement, depth++) {
        if (node.querySelector('nav,aside,[role="navigation"]')) break;
        lines.push('image#' + index + ' ancestor#' + depth + ' ' + attrs(node));
        if (node.matches(assistantQuery + ',' + userQuery) || /^(receive_message|send_message)$/.test(node.getAttribute('data-testid') || '')) {
          lines = lines.concat(diagnostics(node));
          break;
        }
      }
    });
    return lines.map(function (line) { return line.slice(0, 320); });
  }
  function imageState(img, prefix) {
    var role = img.closest(assistantQuery + ',' + userQuery), testid = img.closest('[data-testid]');
    var baseline = !!(state.images && state.images.has(img));
    var lastUser = all(userQuery).pop();
    return [
      prefix + ' visible=' + visible(img) + ';complete=' + img.complete + ';natural=' + img.naturalWidth + 'x' + img.naturalHeight + ';baselineNode=' + baseline + ';sourceURL=' + !!(state.sourceURLs && state.sourceURLs.has(img.currentSrc || img.src)) + ';followingLatestUser=' + !!(lastUser && (lastUser.compareDocumentPosition(img) & Node.DOCUMENT_POSITION_FOLLOWING)),
      prefix + ' role=' + attrs(role) + ';nearestTestid=' + (testid ? testid.getAttribute('data-testid') : '') + ';mdbox=' + !!img.closest('[data-testid="mdbox_image"]') + ';userImage=' + !!img.closest('[data-testid="message_image_content"]')
    ].map(function (line) { return line.slice(0, 320); });
  }
  function read(comp) {
    if (!comp) return '';
    if (comp.tagName === 'TEXTAREA' || comp.tagName === 'INPUT') return comp.value;
    function walk(node) {
      if (node.nodeType === 3) return String(node.nodeValue || '').replace(/\uFEFF/g, '');
      if (node.nodeType !== 1) return '';
      var style = getComputedStyle(node);
      if (node !== comp && (node.getAttribute('contenteditable') === 'false' || node.getAttribute('aria-hidden') === 'true' || style.display === 'none' || style.visibility === 'hidden')) return '';
      if (node.tagName === 'BR') return '\n';
      var text = Array.from(node.childNodes).map(walk).join('');
      return node !== comp && /^(DIV|P|LI)$/.test(node.tagName) && text ? text + '\n' : text;
    }
    return walk(comp).replace(/\n$/, '');
  }
  function uploadEntry(comp) {
    for (var scope = comp && comp.parentElement; scope && scope !== document.body; scope = scope.parentElement) {
      var found = all('button[data-testid="upload_file_button"],button[aria-label="添加附件"]', scope).filter(enabled)[0];
      if (found) return found;
    }
    return null;
  }
  function attachmentScope(comp, entry) {
    if (!comp) return null;
    var surface = comp.closest('.guidance-input-surface');
    if (surface && entry && surface.contains(entry)) return surface;
    surface = comp.closest('[data-chat-input-body]');
    if (surface && entry && surface.contains(entry)) return surface;
    for (var node = comp.parentElement; node && node !== document.body; node = node.parentElement) {
      if (entry && node.contains(entry)) return node;
    }
    return null;
  }
  function sendButton(comp) {
    for (var scope = comp && comp.parentElement; scope && scope !== document.body; scope = scope.parentElement) {
      var found = controls(scope).filter(function (el) { return enabled(el) && (el.getAttribute('data-testid') === 'chat_input_send_button' || [el.getAttribute('aria-label'), el.getAttribute('title'), el.innerText].some(function (text) { return /^(发送|发送消息|Send|Send message)$/i.test(String(text || '').trim()); })); }).pop();
      if (found) return found;
    }
    return null;
  }
  function newAssistants() {
    if (!state.marked) return [];
    var messages = orderedMessages();
    state.mismatch = state.baseline.findIndex(function (key, index) { return !messages[index] || messageKey(messages[index]) !== key; });
    var prompt = normalized(state.prompt), anchor = null;
    if (prompt && state.anchorConfirmed) {
      if (state.anchor && state.anchor.isConnected && messages.indexOf(state.anchor) !== -1 && userContents(state.anchor).indexOf(prompt) !== -1) {
        anchor = state.anchor;
      } else {
        var lastUser = messages.filter(function (node) { return node.matches(userQuery); }).pop();
        if (lastUser && userContents(lastUser).indexOf(prompt) !== -1) anchor = lastUser;
      }
    } else if (prompt) {
      var oldMatches = state.userBodies.filter(function (bodies) { return bodies.indexOf(prompt) !== -1; }).length;
      var matches = messages.filter(function (node) { return node.matches(userQuery) && userContents(node).indexOf(prompt) !== -1; });
      // Node replacement alone cannot add a new occurrence of this prompt.
      if (matches.length > oldMatches) anchor = matches[oldMatches];
      if (!anchor && state.tail && state.tail.isConnected) {
        var tailIndex = messages.indexOf(state.tail);
        if (tailIndex !== -1) anchor = matches.find(function (node) { return messages.indexOf(node) > tailIndex; }) || null;
      }
    }
    state.anchor = anchor;
    if (!anchor) return [];
    state.anchorConfirmed = true;
    var following = messages.slice(messages.indexOf(anchor) + 1), nextUser = following.findIndex(function (node) { return node.matches(userQuery); });
    if (nextUser !== -1) following = following.slice(0, nextUser);
    return following.filter(function (node) { return node.matches(assistantQuery); });
  }
  function orderedMessages() {
    return all(assistantQuery + ',' + userQuery).filter(function (node, index, nodes) {
      return !nodes.some(function (parent) { return parent !== node && parent.contains(node); });
    });
  }
  function messageKey(node) {
    var role = node.matches(userQuery) ? 'user' : 'assistant';
    var id = node.getAttribute('data-message-id') || node.getAttribute('data-id') || node.id;
    if (id) return role + ':' + id;
    return role + ':' + hash(businessText(node));
  }
  function normalized(text) { return String(text || '').replace(/\uFEFF/g, '').replace(/\s+/g, ' ').trim(); }
  function hash(text) {
    var value = 2166136261;
    for (var i = 0; i < text.length; i++) value = Math.imul(value ^ text.charCodeAt(i), 16777619);
    return (value >>> 0).toString(16);
  }
  function businessText(root) {
    function walk(node) {
      if (node.nodeType === 3) return node.nodeValue || '';
      if (node.nodeType !== 1) return '';
      if (/^(BUTTON|NAV|TIME|SVG|SCRIPT|STYLE)$/.test(node.tagName) || node.getAttribute('role') === 'button' || node.getAttribute('role') === 'toolbar' || node.getAttribute('aria-hidden') === 'true') return '';
      var style = getComputedStyle(node);
      if (style.display === 'none' || style.visibility === 'hidden') return '';
      return Array.from(node.childNodes).map(walk).join(/^(DIV|P|LI)$/.test(node.tagName) ? ' ' : '');
    }
    return normalized(walk(root));
  }
  function messageDiagnostic(node) {
    var names = node.getAttributeNames(), identity = names.filter(function (name) { return /(^id$|(?:^|[-_])(?:id|key|index)(?:$|[-_]))/i.test(name); });
    return 'message ' + attrs(node) + ';attrNames=' + names.join(',').slice(0, 100) + ';identityNames=' + identity.join(',').slice(0, 80) + ';bodyChars=' + businessText(node).length + ';keyHash=' + hash(messageKey(node));
  }
  function userContents(root) {
    var nodes = [root].concat(all('p,div,span,[data-testid]', root));
    return Array.from(new Set(nodes.filter(function (node) {
      for (var parent = node; parent && parent !== root; parent = parent.parentElement) {
        if (/^(BUTTON|NAV|TIME)$/.test(parent.tagName) || /^(button|toolbar)$/.test(parent.getAttribute('role') || '')) return false;
      }
      return true;
    }).map(businessText).filter(Boolean)));
  }
  function qwenLoadingImage(img) {
    return /(^|\.)(tongyi|qianwen)\.com$/.test(location.hostname) && String(img.getAttribute('alt') || '').trim().toLowerCase() === 'loading' && !!img.closest('[id^="card_card_ai_generate_image_"]');
  }
  function busy(root) { return all('[aria-busy="true"],[role="progressbar"]', root).some(visible) || all('img', root).some(function (img) { return visible(img) && qwenLoadingImage(img); }) || /正在生成|生成中|绘制中|\bGenerating\b/i.test(root.innerText || ''); }
  function downloadButtons(root) {
    return controls(root).filter(function (el) { return enabled(el) && !/全部|批量|\ball\b|\.zip\b/i.test(label(el) + ' ' + (el.getAttribute('download') || '')) && (el.hasAttribute('download') || /下载|保存图片|\bdownload\b/i.test(label(el))); });
  }
  function clientPromotionClose(comp) {
    if (!/(^|\.)doubao\.com$/.test(location.hostname)) return null;
    var cta = controls(document).filter(function (button) {
      return enabled(button) && [button.getAttribute('aria-label'), button.innerText].some(function (text) { return String(text || '').trim() === '立即下载电脑版'; });
    })[0];
    for (var scope = cta && cta.parentElement, depth = 0; scope && scope !== document.body && depth < 7; scope = scope.parentElement, depth++) {
      if ((comp && scope.contains(comp)) || scope.matches(assistantQuery + ',' + userQuery) || scope.querySelector('input[type="password"],[data-sitekey],iframe[src*="captcha"]')) break;
      var close = controls(scope).filter(function (button) {
        return enabled(button) && [button.getAttribute('aria-label'), button.getAttribute('title'), button.innerText].some(function (text) { return String(text || '').trim() === '关闭'; });
      })[0];
      if (close) return { button: close, scope: scope };
    }
    return null;
  }
  function imageEditor(comp) {
    if (/(^|\.)(tongyi|qianwen)\.com$/.test(location.hostname)) {
      var backs = controls(document).filter(function (button) { return button.tagName === 'BUTTON' && [button.getAttribute('aria-label'), button.getAttribute('title'), button.innerText].some(function (text) { return String(text || '').trim() === '回到对话'; }); });
      for (var i = 0; i < backs.length; i++) {
        for (var scope = backs[i].parentElement, depth = 0; scope && scope !== document.body && depth < 15; scope = scope.parentElement, depth++) {
          if ((comp && scope.contains(comp)) || (state.chatComposer && scope.contains(state.chatComposer)) || scope.matches(assistantQuery + ',' + userQuery)) break;
          var lists = all('[role="listbox"]', scope).filter(function (list) { return visible(list) && list.getAttribute('aria-label') === '图片列表'; });
          if (lists.length !== 1 || !all('img', scope).some(function (img) { return visible(img) && !lists[0].contains(img); })) continue;
          var nodes = all('*', scope).filter(function (node) { return enabled(node) && !lists[0].contains(node); });
          function leaves(text) { return nodes.filter(function (node) { return String(node.innerText || '').trim() === text && !all('*', node).some(function (child) { return visible(child) && String(child.innerText || '').trim() === text; }); }); }
          if (!leaves('添加到对话').length || !leaves('4K超清').length) continue;
          var downloads = leaves('下载');
          var closes = controls(scope).filter(function (button) { return button.tagName === 'BUTTON' && !lists[0].contains(button) && [button.getAttribute('aria-label'), button.getAttribute('title'), button.innerText].some(function (text) { return String(text || '').trim() === '关闭'; }); });
          return { scope: scope, save: downloads.length === 1 ? downloads[0] : null, close: closes.length === 1 ? closes[0] : null };
        }
      }
      return null;
    }
    var saves = controls(document).filter(function (button) {
      return [button.getAttribute('aria-label'), button.getAttribute('title'), button.innerText].some(function (text) { return String(text || '').trim() === '保存'; });
    });
    for (var i = 0; i < saves.length; i++) {
      for (var scope = saves[i].parentElement, depth = 0; scope && scope !== document.body && depth < 15; scope = scope.parentElement, depth++) {
        if ((comp && scope.contains(comp)) || (state.chatComposer && scope.contains(state.chatComposer)) || scope.matches(assistantQuery + ',' + userQuery)) break;
        var text = scope.innerText || '';
        if (text.indexOf('创建画布') !== -1 && text.indexOf('标记改图') !== -1) return { scope: scope, save: saves[i] };
      }
    }
    return null;
  }
  var comp = composer();
  if (action === 'init') {
    if (comp) state.chatComposer = comp;
    var recent = [];
    all(assistantQuery).slice(-2).forEach(function (message) { recent = recent.concat(all('img', message)); });
    var lines = ['existingAssistant=' + all(assistantQuery).length + ';recentImages=' + recent.length];
    recent.slice(-8).forEach(function (img, index) { lines = lines.concat(imageState(img, 'assistantIMG#' + index)); });
    var media = all('[data-testid="mdbox_image"] img').slice(-2);
    if (!/(^|\.)doubao\.com$/.test(location.hostname)) {
      media = all('img').filter(function (img) { return !img.closest('nav,aside,header,[role="navigation"],[contenteditable="true"]') && !(comp && comp.contains(img)); }).slice(-2);
    }
    media.forEach(function (img, index) { lines = lines.concat(imageState(img, 'mediaProbe#' + index)); });
    return { diagnostic: lines.concat(imageDiagnostic(recent), imageDiagnostic(media)) };
  }
  if (action === 'bindPrompt') {
    state.prompt = args;
    return {};
  }
  if (action === 'composer') {
    var button = sendButton(comp), value = read(comp);
    return { composer: selector(comp), value: value, send: selector(button), diagnostic: ['composer=' + attrs(comp) + ';chars=' + Array.from(value).length].concat(comp && comp.parentElement ? diagnostics(comp.parentElement) : []) };
  }
  if (action === 'uploadStart') {
    var entry = uploadEntry(comp);
    if (entry && entry.getAttribute('aria-label') === '添加附件' && String(read(comp) || '').trim()) {
      throw new Error('当前输入区有未发送内容，未操作上传菜单');
    }
    state.uploadScope = attachmentScope(comp, entry);
    state.uploadImages = new Map(all('img').map(function (img) { return [img, img.currentSrc || img.src]; }));
    state.uploadStable = 0;
    state.uploadEntry = entry;
    state.uploadEntryDiagnostic = entryDiagnostic(entry);
    var open = entry ? [selector(entry)] : [];
    var menuAlreadyOpen = false;
    var menuLabel = '';
    if (entry && entry.getAttribute('data-testid') === 'upload_file_button') {
      var menuSelector = 'text="上传文件或图片"';
      var menuOpen = all('[role="listbox"]').filter(visible).some(function (panel) {
        return all('*', panel).some(function (item) { return enabled(item) && String(item.innerText || '').trim() === '上传文件或图片'; });
      });
      open = menuOpen ? [menuSelector] : open.concat(menuSelector);
      menuAlreadyOpen = menuOpen;
    } else if (entry && entry.getAttribute('aria-label') === '添加附件') {
      var imageMenuOpen = all('[role="menuitem"]').some(function (item) { return enabled(item) && qwenImageItem(item); });
      open = imageMenuOpen ? [] : open;
      menuLabel = '上传图片';
      menuAlreadyOpen = imageMenuOpen;
    }
    var plan = 'plannedSteps=' + open.length + ';menuAlreadyOpen=' + menuAlreadyOpen + ';menuLabel=' + menuLabel + ';steps=' + open.join(' -> ');
    return { summary: plan, open: open, menuLabel: menuLabel, diagnostic: ['composer=' + attrs(comp), 'scope=' + attrs(state.uploadScope)].concat(state.uploadEntryDiagnostic, uploadMenuDiagnostic()) };
  }
  if (action === 'upload') {
    var scope = state.uploadScope;
    var fresh = scope && scope.isConnected ? all('img', scope).filter(function (img) { return visible(img) && img.complete && img.naturalWidth > 0 && (!state.uploadImages.has(img) || state.uploadImages.get(img) !== (img.currentSrc || img.src)); }) : [];
    var ready = !!scope && fresh.length >= args && !busy(scope);
    state.uploadStable = ready ? state.uploadStable + 1 : 0;
    return { ready: state.uploadStable >= 2, diagnostic: ['scope=' + attrs(scope) + ';attachments=' + fresh.length + ';expected=' + args].concat(state.uploadEntryDiagnostic || [], entryDiagnostic(uploadEntry(comp) || state.uploadEntry), uploadMenuDiagnostic(), imageDiagnostic(all('img').filter(function (img) { return state.uploadImages && !state.uploadImages.has(img); }))) };
  }
  if (action === 'clean') {
    var scope = attachmentScope(comp, uploadEntry(comp));
    var promotion = clientPromotionClose(comp);
    var editor = imageEditor(comp);
    return {
      preview: !!editor,
      promotionClose: promotion ? selector(promotion.button) : '',
      diagnostic: promotion ? ['clientPromotion=' + attrs(promotion.scope), 'close=' + attrs(promotion.button)] : [],
      remove: scope ? controls(scope).filter(function (el) { return enabled(el) && /删除|移除|remove attachment/i.test(label(el)); }).map(selector) : []
    };
  }
  if (action === 'mark') {
    state.marked = true;
    state.assistants = new Set(all(assistantQuery));
    state.users = new Set(all(userQuery));
    state.baseline = orderedMessages().map(messageKey);
    state.tail = orderedMessages().pop() || null;
    state.userBodies = orderedMessages().filter(function (node) { return node.matches(userQuery); }).map(userContents);
    state.prompt = '';
    state.anchor = null;
    state.anchorConfirmed = false;
    state.chatComposer = comp;
    state.activePreview = null;
    state.images = new Set(all('img'));
    state.sourceURLs = new Set();
    var scope = attachmentScope(comp, uploadEntry(comp));
    if (scope) all('img', scope).forEach(function (img) { state.sourceURLs.add(img.currentSrc || img.src); });
    state.dialogs = new Set(all(panelQuery).filter(visible));
    var currentImages = all('img').filter(function (img) { return visible(img) && !img.closest('nav,aside,header,[role="navigation"]') && !(scope && scope.contains(img)); });
    return { diagnostic: ['boundary assistant=' + state.assistants.size + ';user=' + state.users.size].concat(orderedMessages().slice(-4).map(messageDiagnostic), imageDiagnostic(currentImages)) };
  }
  if (action === 'preview') {
    var editor = imageEditor(comp), active = editor ? editor.scope : state.activePreview;
    return { preview: !!(active && active.isConnected && visible(active)), close: editor && editor.close ? selector(editor.close) : '', diagnostic: ['preview=' + attrs(active)] };
  }
  if (action === 'results' || action === 'state') {
    var messages = newAssistants(), images = [], downloads = [], probes = [], isBusy = messages.some(busy);
    messages.forEach(function (message) {
      all('img', message).forEach(function (img, index) { probes = probes.concat(imageState(img, 'taskIMG#' + index)); });
      var ownImages = all('img', message).filter(function (img) {
        if (qwenLoadingImage(img)) return false;
        if (/(^|\.)(tongyi|qianwen)\.com$/.test(location.hostname)) {
          var markdown = img.closest('#qk-markdown-react,.markdown-pc-special-class');
          if (!markdown || !message.contains(markdown)) return false;
        }
        if (/(^|\.)doubao\.com$/.test(location.hostname) && !img.closest('[data-testid="mdbox_image"]')) return false;
        return visible(img) && img.complete && img.naturalWidth > 0 && !state.images.has(img) && !state.sourceURLs.has(img.currentSrc || img.src);
      });
      images = images.concat(ownImages.map(function (img) { return img.closest('[data-testid="mdbox_image"]') || img; }));
      if (ownImages.length) downloads = downloads.concat(downloadButtons(message));
    });
    images = Array.from(new Set(images));
    var close = '';
    if (args && args.preview) {
      var editor = imageEditor(comp);
      var dialogs = all(panelQuery).filter(function (dialog) { return visible(dialog) && !state.dialogs.has(dialog); });
      downloads = [];
      if (editor) {
        state.activePreview = editor.scope;
        isBusy = isBusy || busy(editor.scope);
        if (enabled(editor.save)) downloads = [editor.save];
        if (editor.close) close = selector(editor.close);
        dialogs = [];
      }
      dialogs.forEach(function (dialog) {
        isBusy = isBusy || busy(dialog);
        var ownDownloads = downloadButtons(dialog);
        if (ownDownloads.length && all('img', dialog).some(visible)) state.activePreview = dialog;
        downloads = downloads.concat(ownDownloads);
        var button = controls(dialog).filter(function (el) { return enabled(el) && [el.getAttribute('aria-label'), el.getAttribute('title'), el.innerText].some(function (text) { return /^(关闭|关闭预览|Close|Close preview)$/i.test(String(text || '').trim()); }); })[0];
        if (button) close = selector(button);
      });
    }
    var newImages = state.images ? all('img').filter(function (img) { return !state.images.has(img) && !img.closest('nav,aside,header,[role="navigation"]'); }) : [];
    var summary = 'newAssistant=' + messages.length + ';anchor=' + !!state.anchor + ';images=' + images.length + ';newImages=' + newImages.length + ';downloads=' + downloads.length + ';busy=' + isBusy + ';firstMismatch=' + state.mismatch + ';imageState=' + hash(probes.join('|'));
    var lines = [summary].concat(probes);
    if (state.mismatch >= 0) {
      var currentRows = orderedMessages(), oldKey = state.baseline[state.mismatch], currentRow = currentRows[state.mismatch];
      lines.push('baselineMismatch index=' + state.mismatch + ';oldHash=' + hash(oldKey) + ';currentHash=' + (currentRow ? hash(messageKey(currentRow)) : 'missing'));
      if (currentRow) lines.push(messageDiagnostic(currentRow));
    }
    if (!state.anchor && state.marked) {
      var prompt = normalized(state.prompt);
      lines.push('anchor promptChars=' + prompt.length + ';oldMatches=' + state.userBodies.filter(function (bodies) { return bodies.indexOf(prompt) !== -1; }).length + ';currentMatches=' + orderedMessages().filter(function (user) { return user.matches(userQuery) && userContents(user).indexOf(prompt) !== -1; }).length);
      lines = lines.concat(orderedMessages().slice(-4).map(messageDiagnostic));
    }
    messages.slice(-2).forEach(function (message) { lines.push(attrs(message)); lines = lines.concat(diagnostics(message)); });
    if (!messages.length || !downloads.length) lines = lines.concat(imageDiagnostic(newImages));
    return { summary: summary, composer: selector(comp), busy: isBusy, images: images.map(function (img) { return { selector: selector(img), key: selector(img) }; }), downloads: downloads.map(function (button) { return { selector: selector(button), key: selector(button) }; }), close: close, diagnostic: lines };
  }
  throw new Error('Unknown Ego state action');
})
