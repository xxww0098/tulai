// tulai 结果截获 hook：网络层（fetch/XHR）+ DOM 层（聊天区新增大图）双通道，
// 把生成图片 URL/数据 记入 window.__tulaiResults，供 Go 侧等待与保存。
(function () {
  window.__tulaiAssistantQuery = '[data-message-author-role="assistant"],[data-message-role="assistant"],[data-role="assistant"],[data-testid="receive_message"],.chat-answers-card-wrap,[id^="chat-response-message-"],.chat-response-message,model-response,[data-test-id="model-response"]';
  if (window.__tulaiHookInstalled) return;
  window.__tulaiHookInstalled = true;
  var results = [];
  var existingImages = new WeakSet();
  var ignoredURLs = new Set();
  var baselineURLs = new Set();
  var baselineActions = new WeakSet();
  var baselineReplies = new WeakSet();
  var doubaoTurn = null;
  var revealEl = null, revealAt = 0;
  var taskNumber = 0, pendingGeneration = 0;
  function generationURL(url) { return /sse|event-stream|chat|completion|stream|message|segment|generation/i.test(url); }
  function beginGeneration() {
    if (!taskNumber) return null;
    pendingGeneration++;
    return { task: taskNumber, done: false };
  }
  function finishGeneration(ticket) {
    if (!ticket || ticket.done) return;
    ticket.done = true;
    if (ticket.task === taskNumber) pendingGeneration = Math.max(0, pendingGeneration - 1);
  }
  window.__tulaiPendingGeneration = function () { return pendingGeneration; };

  window.__tulaiResults = results;
  window.__tulaiMark = Date.now();
  window.__tulaiAddResult = function (url, src) {
    if (!url || junkURL(url)) return;
    // 去重
    for (var i = 0; i < results.length; i++) if (results[i].task === taskNumber && results[i].url === url) return;
    results.push({ url: url, src: src || '', ts: Date.now(), task: taskNumber });
    if (results.length > 400) results.shift();
  };

  function looksLikeImage(url, mime) {
    if (mime && /image\//i.test(mime)) return true;
    if (!url) return false;
    if (/^data:image\//i.test(url)) return true;
    if (/^blob:/.test(url)) return true;
    return /\.(png|jpe?g|webp|gif)(\?|$)/i.test(url) ||
      (/object|image|photo|picture|img|media|download|stream/i.test(url) && /tosv|tos|obj|cdn|img|image/i.test(url));
  }

  // ---- fetch 拦截 ----
  var origFetch = window.fetch;
  if (origFetch) {
    // Preserve the original function metadata while observing responses.
    window.fetch = new Proxy(origFetch, {apply: function (target, receiver, args) {
      var input = args[0];
      var url = (typeof input === 'string') ? input : (input && input.url) || '';
      var requestTask = taskNumber, relevant = generationURL(url), ticket = relevant ? beginGeneration() : null, p;
      try { p = Reflect.apply(target, receiver, args); } catch (e) { finishGeneration(ticket); throw e; }
      if (relevant) {
        // 流式响应：读取文本里出现的图片 URL
        p.then(function (resp) {
          try {
            var ct = resp.headers.get('content-type') || '';
            if (/event-stream|json|text/i.test(ct) && !resp.bodyUsed) {
              var clone = resp.clone();
              clone.text().then(function (txt) {
                if (requestTask !== taskNumber) return;
                var re = /https?:\/\/[^\s"'\\]+?(?:\.(?:png|jpe?g|webp)(?:\?[^\s"'\\]*)?|tosv[^"'\\]*|object_(?:basic|image)[^\s"'\\]*)/gi;
                var m;
                while ((m = re.exec(txt))) window.__tulaiAddResult(m[0], 'stream');
              }).catch(function () {}).finally(function () { finishGeneration(ticket); });
              return resp;
            }
          } catch (e) {}
          finishGeneration(ticket);
          return resp;
        }).catch(function () { finishGeneration(ticket); });
      } else {
        p.then(function (resp) {
          try {
            var mime = resp.headers.get('content-type') || '';
            if (looksLikeImage(url, mime)) {
              resp.clone().blob().then(function (b) {
                if (requestTask !== taskNumber) return;
                if (b.size > 20480) window.__tulaiAddResult(url, 'net'); // 过滤小图标
              }).catch(function () {});
            }
          } catch (e) {}
          return resp;
        }).catch(function () {});
      }
      return p;
    }});
  }

  // ---- XHR 拦截 ----
  var xhrURLs = new WeakMap();
  var origOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = new Proxy(origOpen, {apply: function (target, receiver, args) {
    xhrURLs.set(receiver, args[1]);
    return Reflect.apply(target, receiver, args);
  }});
  var origSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.send = new Proxy(origSend, {apply: function (target, xhr, args) {
    var url = xhrURLs.get(xhr) || '', requestTask = taskNumber, ticket = generationURL(url) ? beginGeneration() : null;
    xhr.addEventListener('loadend', function () { finishGeneration(ticket); }, { once: true });
    xhr.addEventListener('load', function () {
      if (requestTask !== taskNumber) return;
      try {
        var mime = xhr.getResponseHeader('content-type') || '';
        if (looksLikeImage(url, mime)) {
          if ((xhr.responseType === '' || xhr.responseType === 'blob') && xhr.response && xhr.response.size > 20480) {
            window.__tulaiAddResult(url, 'net');
          }
        } else if (/json|text/i.test(mime)) {
          var txt = typeof xhr.responseText === 'string' ? xhr.responseText : '';
          var re = /https?:\/\/[^\s"'\\]+?(?:\.(?:png|jpe?g|webp)(?:\?[^\s"'\\]*)?|tosv[^"'\\]*|object_(?:basic|image)[^\s"'\\]*)/gi;
          var m;
          while ((m = re.exec(txt))) window.__tulaiAddResult(m[0], 'xhr');
        }
      } catch (e) {}
    });
    try { return Reflect.apply(target, xhr, args); } catch (e) { finishGeneration(ticket); throw e; }
  }});

  // ---- DOM 观察兜底：消息列表新增的图片 ----
  var mo = new MutationObserver(function (muts) {
    for (var i = 0; i < muts.length; i++) {
      if (muts[i].type === 'attributes' && muts[i].target.tagName === 'IMG') checkImg(muts[i].target);
      muts[i].addedNodes && Array.prototype.forEach.call(muts[i].addedNodes, function (n) {
        if (!n || n.nodeType !== 1) return;
        if (n.tagName === 'IMG') checkImg(n);
        else if (n.querySelectorAll) n.querySelectorAll('img').forEach(checkImg);
      });
    }
  });
  function junkURL(url) {
    var value = String(url || '');
    if (!value) return true;
    if (/^data:image\/svg/i.test(value) || /\.svg(\?|#|$)/i.test(value)) return true;
    if (/avatar|icon|logo|emoji|sprite/i.test(value)) return true;
    return false;
  }
  function pushURL(list, url) {
    var value = String(url || '').trim();
    if (!value || junkURL(value) || list.indexOf(value) !== -1) return;
    list.push(value);
  }
  function urlsOf(img) {
    var out = [];
    if (!img) return out;
    pushURL(out, img.currentSrc);
    pushURL(out, img.src);
    if (img.getAttribute) {
      pushURL(out, img.getAttribute('src'));
      ;['data-src', 'data-original', 'data-original-src', 'data-url', 'data-actualsrc'].forEach(function (name) {
        pushURL(out, img.getAttribute(name));
      });
    }
    var link = img.closest && img.closest('a');
    if (link && link.getAttribute) pushURL(out, link.getAttribute('href') || link.getAttribute('data-href'));
    return out;
  }
  function urlRank(url, width) {
    var rank = width || 0;
    if (/^https?:/i.test(url)) rank += 1000000;
    if (/rc_gen_image/i.test(url)) rank += 300000;
    if (/googleusercontent\.com\/(?:gg-(?:dl|mc)|rd-gg-dl)\b/i.test(url)) rank += 120000;
    if (/cgen_lwm|\/orig(?:in)?\b/i.test(url)) rank += 80000;
    if (/googleusercontent\.com\/.*=s0(?:[-?&#]|$)/i.test(url)) rank += 50000;
    if (/cthumb|thumbnail|downsize/i.test(url)) rank -= 20000;
    if (/googleusercontent\.com\/.*=s(?:64|128|256|320|512)(?:[-?&#]|$)/i.test(url)) rank -= 20000;
    // 千问缩略图带 x-oss-process=image/resize。同一张图上的无处理地址才是原图。
    if (/[?&]x-oss-process=/i.test(url)) rank -= 80000;
    if (/~tplv-/i.test(url)) rank -= 1000;
    return rank;
  }
  // 页面上同时有缩略图和原图时用原图。缩略图地址重新拉取经常 403。
  function bestURL(img) {
    var best = '', score = -1e15, width = img && img.naturalWidth > 0 ? img.naturalWidth : 0;
    urlsOf(img).forEach(function (url) {
      var next = urlRank(url, width);
      if (next > score) { score = next; best = url; }
    });
    return best;
  }
  function pageHost() {
    if (window.__tulaiPageHost) return String(window.__tulaiPageHost);
    return String(location.hostname || '');
  }
  function closestAny(node, selectors) {
    if (!node || !node.closest) return null;
    for (var i = 0; i < selectors.length; i++) {
      var hit = node.closest(selectors[i]);
      if (hit) return hit;
    }
    return null;
  }
  function replyOf(img) {
    return closestAny(img, ['[data-testid="receive_message"]', '[data-message-author-role="assistant"]', '[data-message-role="assistant"]', '[data-role="assistant"]', '.chat-answers-card-wrap', '[id^="chat-response-message-"]', '.chat-response-message', 'model-response', '[data-test-id="model-response"]']);
  }
  // 用户气泡里的 message_image_content 是上传的原图。助手回复里的同名容器才是生成图。
  function inUserBubble(img) {
    var node = closestAny(img, ['[data-testid="send_message"]', '[data-testid="receive_message"]', '[data-message-role="user"]', '[data-message-role="assistant"]', '[data-message-author-role="user"]', '[data-message-author-role="assistant"]', '.chat-user-message', '.chat-user-message-wrapper', '.chat-user-message-container', '[id^="chat-response-message-"]', '.chat-response-message', 'user-query', '[data-test-id="user-query"]', '.user-query-container', 'model-response', '[data-test-id="model-response"]']);
    if (!node || !node.getAttribute) return false;
    var tag = String(node.tagName || '').toLowerCase();
    var tid = node.getAttribute('data-testid') || '';
    var dtid = node.getAttribute('data-test-id') || '';
    var role = node.getAttribute('data-message-role') || node.getAttribute('data-message-author-role') || '';
    if (tid === 'receive_message' || role === 'assistant' || tag === 'model-response' || dtid === 'model-response') return false;
    if (tid === 'send_message' || role === 'user' || tag === 'user-query' || dtid === 'user-query') return true;
    var id = node.id || '';
    var cls = String((node.className && node.className.baseVal) || node.className || '');
    if (/^chat-response-message-/.test(id) || /(^|\s)chat-response-message(\s|$)/.test(cls)) return false;
    return /(^|\s)(?:chat-user-message(?:-wrapper|-container)?|user-query-container)(\s|$)/.test(cls);
  }
  // 基准只排除发送前已经在的回复。这一轮新出现的 receive_message 整段都算，
  // 即使占位壳的地址在标记时见过。旧回复里换上新地址的壳仍然算。
  function isBaselineImage(img) {
    var reply = replyOf(img);
    if (reply && !baselineReplies.has(reply)) return false;
    var urls = urlsOf(img);
    if (!urls.length) return false;
    for (var i = 0; i < urls.length; i++) if (!baselineURLs.has(urls[i])) return false;
    return true;
  }
  function assistantNodes() {
    return Array.prototype.slice.call(document.querySelectorAll(window.__tulaiAssistantQuery));
  }
  // 新助手节点是这一轮回复。豆包有时把新图追加进最后一条消息，那种情况只认基准之外的图。
  function replyNodes() {
    var nodes = assistantNodes();
    var floor = window.__tulaiAssistantBaseline || 0;
    if (floor < 0) floor = 0;
    var fresh = nodes.slice(floor);
    if (fresh.length) return fresh;
    if (nodes.length) return [nodes[nodes.length - 1]];
    return [];
  }
  function labelOf(el) {
    return String((el && (el.innerText || (el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('title'))))) || '').replace(/\s+/g, '');
  }
  function actionBar(root) {
    var buttons = root.querySelectorAll('button,[role="button"],a');
    for (var i = 0; i < buttons.length; i++) {
      if (baselineActions.has(buttons[i])) continue;
      var tid = buttons[i].getAttribute && (buttons[i].getAttribute('data-testid') || '');
      if (tid === 'message_action_regenerate' || tid === 'message_action_copy') return true;
      if (/重新生成|再次生成|再试一次|复制|拷贝|^copy$/i.test(labelOf(buttons[i]))) return true;
    }
    return false;
  }
  function imageSlots(root) {
    var items = root.querySelectorAll('[class*="image-box-grid-item"]');
    if (items.length) return Array.prototype.slice.call(items);
    var boxes = root.querySelectorAll('[data-testid="mdbox_image"]');
    if (boxes.length) return Array.prototype.slice.call(boxes);
    return [];
  }
  function hasFinishedAttr(root) {
    if (root.getAttribute && root.getAttribute('data-finished') != null) return true;
    return root.querySelectorAll('[data-finished]').length > 0;
  }
  function slotFinished(slot) {
    var node = slot;
    while (node && node.getAttribute) {
      var v = node.getAttribute('data-finished');
      if (v === 'true') return true;
      if (v === 'false') return false;
      node = node.parentElement;
    }
    return false;
  }
  function slotReady(slot) {
    if (!slotFinished(slot)) return false;
    var imgs = slot.querySelectorAll ? slot.querySelectorAll('img') : [];
    for (var i = 0; i < imgs.length; i++) if (realImg(imgs[i])) return true;
    return false;
  }
  function replyFinished(root) {
    if (!root) return false;
    var slots = imageSlots(root);
    // 豆包一格一个 data-finished。有这个属性时必须全部为 true，并且每格都有实图。
    // 只看第一格或复制按钮会在其余三张还没出来时就把这一轮收走。
    if (slots.length && hasFinishedAttr(root)) {
      for (var i = 0; i < slots.length; i++) if (!slotReady(slots[i])) return false;
      return true;
    }
    if (root.getAttribute && root.getAttribute('data-finished') === 'true') return true;
    if (root.getAttribute && root.getAttribute('data-streaming') === 'false') return true;
    var texts = root.querySelectorAll('[data-testid="message_text_content"]');
    for (var t = 0; t < texts.length; t++) if (texts[t].getAttribute('data-streaming') === 'false') return true;
    return actionBar(root);
  }
  function replyBusy(root) {
    var text = String((root && root.innerText) || '');
    if (/正在生成|生成中|绘制中|停止生成|\bGenerating\b|\bStop generating\b/.test(text)) return true;
    if (root.querySelector('[aria-busy="true"],[role="progressbar"]')) return true;
    var stops = root.querySelectorAll('[aria-label*="停止"],[data-testid*="stop"],[data-testid*="break"]');
    for (var i = 0; i < stops.length; i++) if (!baselineActions.has(stops[i])) return true;
    return false;
  }
  function composerStopping() {
    var nodes = document.querySelectorAll('button,[role="button"]');
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (baselineActions.has(el)) continue;
      var isStopBtn = !!(el.classList && (el.classList.contains('stop-button') || (el.classList.contains('send-button') && el.classList.contains('stop'))));
      if (!isStopBtn && geminiPage() && el.querySelector && el.querySelector('.stop-icon,mat-icon[fonticon="stop"]')) {
        isStopBtn = true;
      }
      if (!isStopBtn && !/停止生成|^停止$|停止响应|Stop generating|Stop response/i.test(labelOf(el))) {
        if (!(qwenIntlPage() && /^Stop$/i.test(labelOf(el)) && el.closest && el.closest('.message-input-container'))) continue;
      }
      var box = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 1, height: 1 };
      if (box.width > 8 && box.height > 8) return true;
    }
    return false;
  }
  function declaredCount(root) {
    var n = 0;
    function take(el) {
      if (!el || !el.getAttribute) return;
      ;['data-count', 'data-total', 'data-image-count', 'data-expect'].forEach(function (name) {
        var v = parseInt(el.getAttribute(name) || '', 10);
        if (v > n) n = v;
      });
    }
    take(root);
    root.querySelectorAll('[data-count],[data-total],[data-image-count],[data-expect]').forEach(take);
    return n;
  }
  function inResult(img) {
    if (!img || !img.closest) return false;
    if (img.closest('nav') || img.closest('header') || img.closest('svg')) return false;
    var host = pageHost();
    if (geminiPage()) {
      if (img.closest('discovery-images-page,.zero-state-container,modular-zero-state,zero-state-block-picker,zero-state-banners,.zero-state-suggestion-card,image-creation-zero-state')) return false;
      if (img.closest('user-query,[data-test-id="user-query"],.user-query-container,user-query-file-carousel,user-query-file-preview')) return false;
      if (img.closest('input-container,input-area-v2,uploader-file-preview-container,uploader-file-preview,file-preview-container')) return false;
      // 注意：<side-navigation-v2> 包裹整个主内容区（<bard-sidenav-content>），仅排除真正的侧栏 <bard-sidenav>。
      if (img.closest('bard-sidenav,user-profile-picture,[gem-open-account-menu],message-actions,response-footer')) return false;
      if (geminiNoise(bestURL(img) || img.currentSrc || img.src)) return false;
      // Gemini 会将每张生成图包在 <generated-image><single-image><button><img src="blob:..."></button> 中以支持点击放大预览。
      if (img.closest('generated-image,.generated-images-container,single-image')) {
        if (img.closest('button[data-test-id*="download"],button[aria-label*="下载"],button[aria-label*="Download" i],button[aria-label*="分享"],button[aria-label*="Share" i]')) return false;
        return true;
      }
      if (img.closest('button') || img.closest('[role="button"]')) return false;
      return !!img.closest('model-response,model-response-content,[data-test-id="model-response"]');
    }
    if (img.closest('button') || img.closest('[role="button"]')) return false;
    if (/(^|\.)doubao\.com$/.test(host)) {
      if (img.closest('[data-testid="attachment-image-card"]')) return false;
      // 生成图就在助手回复的 message_image_content 里。只丢掉用户消息上的那一层。
      if (img.closest('[data-testid="message_image_content"]') && inUserBubble(img)) return false;
      return !!(img.closest('[data-testid="mdbox_image"]') || img.closest('[data-testid="message_image_content"]') || img.closest('[data-testid="receive_message"]') || img.closest('[class*="image-box-grid-item"]'));
    }
    if (qwenPage()) {
      if (qwenLoadingImage(img)) return false;
      return !!img.closest('#qk-markdown-react,.markdown-pc-special-class,[id^="card_card_ai_generate_image_"],[data-message-author-role="assistant"],[data-message-role="assistant"],.chat-answers-card-wrap');
    }
    if (qwenIntlPage()) {
      if (img.closest('.chat-user-message,.chat-user-message-wrapper,.chat-user-message-container,.chat-user-message-files,.user-image-list,.user-image-item,.chat-recommend-image-list,.message-input-container,.response-message-footer,.response-message-footer-actions,.sidebar-user')) return false;
      if (img.matches && img.matches('.qwen-chat-package-comp-new-action-control-icon-img,.response-present-file-figure-img,.user-avatar,.user-img')) return false;
      return !!img.closest('[id^="chat-response-message-"],.chat-response-message,.chat-response-media-render,.qwen-image,.common-image-grid,.image-generation-preview');
    }
    return true;
  }
  function generatedSrc(src) {
    return /rc_gen_image|cgen_lwm/i.test(src || '');
  }
  function realImg(img) {
    if (!img || !inResult(img)) return false;
    var src = bestURL(img) || img.currentSrc || img.src || '';
    if (!src && img.getAttribute) src = img.getAttribute('src') || '';
    if (junkURL(src)) return false;
    var nw = img.naturalWidth || 0, nh = img.naturalHeight || 0;
    if (nw && nh && (nw < 128 || nh < 128)) return false;
    // 窗口在屏幕外时布局盒可以是 0。已解码的大图，或地址上的 rc_gen_image 原图，仍然算数。
    if (nw >= 128 && nh >= 128) return true;
    var aw = parseInt((img.getAttribute && img.getAttribute('width')) || '0', 10) || 0;
    var ah = parseInt((img.getAttribute && img.getAttribute('height')) || '0', 10) || 0;
    if (aw >= 128 && ah >= 128) return true;
    if (generatedSrc(src)) return true;
    var box = img.getBoundingClientRect();
    if (box.width < 64 || box.height < 64) return false;
    if (!(nw && nh) && (box.width < 128 || box.height < 128)) return false;
    return true;
  }
  function checkImg(img) {
    try {
      if (existingImages.has(img)) {
        ignoredURLs.add(img.src);
        ignoredURLs.add(img.currentSrc);
        return;
      }
      if (realImg(img) && !isBaselineImage(img)) window.__tulaiAddResult(bestURL(img) || img.currentSrc || img.src, 'dom');
    } catch (e) {}
  }
  window.__tulaiDismissPopups = function () {
    var prompts = ['下载电脑版', '立即下载电脑版', '每日签到', '签到领'];
    var closes = ['关闭', '取消', '我知道了', '稍后再说', '以后再说', '暂不', '×', '✕'];
    var n = 0;
    var nodes = document.querySelectorAll('[role="dialog"],[class*="dialog"],[class*="modal"],[class*="popup"],[class*="mask"]');
    for (var i = 0; i < nodes.length; i++) {
      var box = nodes[i];
      var text = String(box.innerText || '').slice(0, 400);
      var hit = false;
      for (var p = 0; p < prompts.length; p++) if (text.indexOf(prompts[p]) >= 0) hit = true;
      if (!hit) continue;
      var buttons = box.querySelectorAll('button,[role="button"],a');
      for (var b = 0; b < buttons.length; b++) {
        var label = String(buttons[b].innerText || buttons[b].getAttribute('aria-label') || '').trim();
        for (var c = 0; c < closes.length; c++) if (label === closes[c]) {
          try { buttons[b].click(); n++; } catch (e) {}
        }
      }
    }
    return n;
  };
  function qwenPage() {
    var host = pageHost();
    return /(^|\.)(tongyi|qianwen)\.com$/.test(host);
  }
  function qwenIntlPage() {
    var host = pageHost();
    return /(^|\.)qwen\.ai$/.test(host);
  }
  function geminiPage() {
    var host = pageHost();
    return /(^|\.)gemini\.google\.com$/.test(host);
  }
  function geminiNoise(url) {
    var v = String(url || '');
    return /\/\/(?:www\.gstatic\.com|ssl\.gstatic\.com|fonts\.gstatic\.com)(?:\/|$)/i.test(v) ||
      /googleusercontent\.com\/a(?:-|\b|\/)/i.test(v);
  }
  function qwenLoadingImage(img) {
    if (!qwenPage() || !img || !img.getAttribute) return false;
    if (String(img.getAttribute('alt') || '').trim().toLowerCase() !== 'loading') return false;
    return !!(img.closest && img.closest('[id^="card_card_ai_generate_image_"]'));
  }
  function qwenJSON(text) {
    try { return JSON.parse(text); } catch (e) { return null; }
  }
  // 完成看 data-exposure-extra 的 card_status。hydrate 脚本里的 status 会停在 processing。
  function qwenCardStatus(card) {
    var extra = qwenJSON(card.getAttribute('data-exposure-extra') || '');
    return String((extra && extra.card_status) || '');
  }
  function qwenImageNum(card) {
    var scripts = card.querySelectorAll('script[data-used-by="hydrate"]');
    for (var i = 0; i < scripts.length; i++) {
      var data = qwenJSON(scripts[i].textContent || '');
      var content = data && data.data && data.data.originalData && data.data.originalData.content;
      var n = content && parseInt(content.image_num, 10);
      if (n > 0) return n;
    }
    return 0;
  }
  function qwenNoise(url) {
    return /\/\/(?:gw\.alicdn\.com|images\.quark\.cn)(?:\/|$)/i.test(String(url || ''));
  }
  function qwenSrc(img) {
    var prop = String((img && img.src) || '');
    if (/^https?:/i.test(prop)) return prop;
    return String((img && img.getAttribute && img.getAttribute('src')) || '').trim();
  }
  function qwenBound() {
    return {
      url: String(window.__tulaiQwenURL || ''),
      wrap: String(window.__tulaiQwenChat || '')
    };
  }
  function qwenPair(id) {
    var safe = String(id || '').replace(/["\\]/g, '');
    return '.chat-question-card-wrap[data-chat-question-wrap="' + safe + '"],.chat-answers-card-wrap[data-chat-answers-wrap="' + safe + '"]';
  }
  // 发送时记下的对话还在当前文档里。地址对不上，或地址对了但首页壳把问答卡卸掉了，都不是。
  function qwenOnChat() {
    var bound = qwenBound();
    if (!bound.url && !bound.wrap) return true;
    var here = window.__tulaiConversationKey(location.href);
    if (bound.url && here !== bound.url) return false;
    if (bound.wrap) return !!document.querySelector(qwenPair(bound.wrap));
    return document.querySelectorAll('.chat-question-card-wrap,.chat-answers-card-wrap').length > 0;
  }
  function qwenIntlTopReplies() {
    var raw = Array.prototype.slice.call(document.querySelectorAll('[id^="chat-response-message-"],.chat-response-message'));
    return raw.filter(function (node) {
      for (var p = node.parentElement; p; p = p.parentElement) {
        if (raw.indexOf(p) !== -1) return false;
      }
      return true;
    });
  }
  function qwenIntlOnChat() {
    var bound = qwenBound();
    if (!bound.url && !bound.wrap) return true;
    var here = window.__tulaiConversationKey(location.href);
    if (bound.url && here !== bound.url) return false;
    if (bound.wrap && document.getElementById && document.getElementById(bound.wrap)) return true;
    if (bound.url && here === bound.url) {
      return document.querySelectorAll('[id^="chat-response-message-"],.chat-response-message,.chat-user-message').length > 0;
    }
    if (bound.wrap) return false;
    return document.querySelectorAll('[id^="chat-response-message-"],.chat-response-message,.chat-user-message').length > 0;
  }
  function geminiTopReplies() {
    var raw = Array.prototype.slice.call(document.querySelectorAll('model-response,[data-test-id="model-response"]'));
    return raw.filter(function (node) {
      for (var p = node.parentElement; p; p = p.parentElement) {
        if (raw.indexOf(p) !== -1) return false;
      }
      return true;
    });
  }
  function geminiOnChat() {
    var bound = qwenBound();
    if (!bound.url && !bound.wrap) return true;
    var here = window.__tulaiConversationKey(location.href);
    if (bound.url && here !== bound.url) return false;
    if (bound.wrap && document.getElementById && document.getElementById(bound.wrap)) return true;
    if (bound.url && here === bound.url) {
      return document.querySelectorAll('model-response,[data-test-id="model-response"],user-query,[data-test-id="user-query"]').length > 0;
    }
    if (bound.wrap) return false;
    return document.querySelectorAll('model-response,[data-test-id="model-response"],user-query,[data-test-id="user-query"]').length > 0;
  }
  window.__tulaiQwenOnChat = function () {
    if (geminiPage()) return geminiOnChat();
    return qwenIntlPage() ? qwenIntlOnChat() : qwenOnChat();
  };
  window.__tulaiQwenNote = function () {
    var url = window.__tulaiConversationKey(location.href);
    if (geminiPage()) {
      var gemNodes = document.querySelectorAll('model-response[id],[data-test-id="model-response"][id],[id^="model-response-message-content"],user-query[id],[data-test-id="user-query"][id]');
      var gemWrap = gemNodes.length ? String(gemNodes[gemNodes.length - 1].id || '') : '';
      return { url: url || '', wrap: gemWrap || '' };
    }
    if (qwenIntlPage()) {
      var intlNodes = document.querySelectorAll('[id^="chat-response-message-"],[id^="chat-user-message-"]');
      var intlWrap = intlNodes.length ? String(intlNodes[intlNodes.length - 1].id || '') : '';
      return { url: url || '', wrap: intlWrap || '' };
    }
    var nodes = document.querySelectorAll('.chat-question-card-wrap[data-chat-question-wrap]');
    var wrap = nodes.length ? String(nodes[nodes.length - 1].getAttribute('data-chat-question-wrap') || '') : '';
    if (!wrap) {
      var answers = document.querySelectorAll('.chat-answers-card-wrap[data-chat-answers-wrap]');
      wrap = answers.length ? String(answers[answers.length - 1].getAttribute('data-chat-answers-wrap') || '') : '';
    }
    return { url: url || '', wrap: wrap || '' };
  };
  function geminiReplies() {
    var all = geminiTopReplies();
    var fresh = all.filter(function (node) { return !baselineReplies.has(node); });
    if (fresh.length) return fresh;
    var floor = window.__tulaiGeminiReplyBaseline || 0;
    if (floor < 0) floor = 0;
    if (floor > all.length) floor = all.length;
    return all.slice(floor);
  }
  var geminiFaultPattern = /出了点问题|发生错误|请求异常|网络连接|网络异常|服务异常|服务繁忙|系统繁忙|生成失败|图片生成失败|无法生成图片|暂时无法生成|请稍后重试|请重试|Something went wrong|An error occurred|Failed to generate|Unable to generate|couldn'?t generate|can'?t generate images right now|Network error|Service unavailable|please retry|try again later/i;
  function geminiFault(root) {
    if (!root) return '';
    if (root.querySelector && root.querySelector('generated-image img,.generated-images-container img,single-image img')) return '';
    var box = root.querySelector('[role="alert"],.error-message,.error-container,[class*="error-message"]');
    var text = String(((box && (box.innerText || box.textContent)) || root.innerText || root.textContent) || '').replace(/\s+/g, ' ').trim();
    if (!text || text.length > 220) return '';
    if (window.TULAIRefuse && window.TULAIRefuse.match(text)) return '';
    if (/额度|次数已用|次数用完|免费次数|已达上限|已达到.*上限|image generation limit|reached your.*limit|daily limit|try again tomorrow/i.test(text)) return '';
    return box || geminiFaultPattern.test(text) ? text : '';
  }
  function geminiLiveImages() {
    if (!geminiOnChat()) {
      return { busy: false, done: false, expect: 0, bar: false, replies: 0, reason: 'off-chat', here: window.__tulaiConversationKey(location.href), images: [] };
    }
    var replies = geminiReplies();
    var images = [];
    var seen = {};
    var busy = composerStopping();
    if (!busy) {
      var stops = document.querySelectorAll('button.send-button.stop,.stop-icon,mat-icon[fonticon="stop"]');
      for (var si = 0; si < stops.length; si++) {
        if (qwenIntlElVisible(stops[si])) { busy = true; break; }
      }
    }
    var hasFooter = false;
    var pendingImgs = [];
    replies.forEach(function (root) {
      if (replyBusy(root)) busy = true;
      var loaders = root.querySelectorAll('mat-progress-spinner,mat-progress-bar,thinking-overlay,chat-loading-animation,.response-loading,[class*="image-loading"],[class*="generating"],[aria-busy="true"],[role="progressbar"]');
      for (var li = 0; li < loaders.length; li++) {
        if (qwenIntlElVisible(loaders[li])) { busy = true; break; }
      }
      if (root.querySelector('message-actions,button[data-test-id="download-generated-image-button"],button[aria-label*="下载"],button[aria-label*="Download" i],button[aria-label*="重新生成"],button[aria-label*="Regenerate" i],button[aria-label*="复制"],button[aria-label*="Copy" i]') || actionBar(root)) {
        hasFooter = true;
      }
      var rootImgs = 0;
      Array.prototype.forEach.call(root.querySelectorAll('img'), function (img) {
        if (!inResult(img) || isBaselineImage(img)) return;
        var src = bestURL(img);
        if (!src || junkURL(src) || geminiNoise(src)) return;
        var lazy = String((img.loading || (img.getAttribute && img.getAttribute('loading')) || '')).toLowerCase();
        if (lazy === 'lazy') {
          try { if (typeof img.setAttribute === 'function') img.setAttribute('loading', 'eager'); } catch (e) {}
          try { img.loading = 'eager'; } catch (e) {}
        }
        if (realImg(img)) {
          rootImgs++;
          if (seen[src]) return;
          seen[src] = true;
          var aw = parseInt((img.getAttribute && img.getAttribute('width')) || '0', 10) || 0;
          var ah = parseInt((img.getAttribute && img.getAttribute('height')) || '0', 10) || 0;
          images.push({
            url: src,
            src: 'dom',
            w: img.naturalWidth || aw || 0,
            h: img.naturalHeight || ah || 0
          });
        } else if (img.closest && img.closest('generated-image,.generated-images-container,single-image')) {
          pendingImgs.push(img);
          busy = true;
        }
      });
      if (rootImgs === 0) {
        var rText = String(root.innerText || root.textContent || '').replace(/\s+/g, ' ').trim();
        if (/正在生成|正在创建|生成中|绘制中|Generating|Creating image|Just a sec/i.test(rText)) busy = true;
      }
    });
    if (pendingImgs.length) {
      var target = pendingImgs[0];
      if (target && typeof target.scrollIntoView === 'function') {
        try { target.scrollIntoView({ block: 'center', inline: 'nearest' }); } catch (e) {
          try { target.scrollIntoView(); } catch (err) {}
        }
      }
    }
    var done = !busy && images.length > 0 && (hasFooter || pendingGeneration === 0);
    var fault = '';
    if (!busy && !images.length) {
      replies.forEach(function (root) { if (!fault) fault = geminiFault(root); });
    }
    var reason = 'no-reply';
    if (!replies.length && !images.length) reason = 'no-reply';
    else if (busy) reason = 'generating';
    else if (fault) reason = 'error-reply';
    else if (!images.length) reason = 'no-new-image';
    else if (!done) reason = 'unfinished';
    else reason = 'images-ready';
    return { busy: busy, done: done, expect: 0, bar: hasFooter, replies: replies.length, reason: reason, fault: fault, here: window.__tulaiConversationKey(location.href), images: images };
  }
  function qwenIntlReplies() {
    var all = qwenIntlTopReplies();
    var fresh = all.filter(function (node) { return !baselineReplies.has(node); });
    if (fresh.length) return fresh;
    var floor = window.__tulaiQwenIntlReplyBaseline || 0;
    if (floor < 0) floor = 0;
    if (floor > all.length) floor = all.length;
    return all.slice(floor);
  }
  var qwenIntlFaultPattern = /请求异常|网络连接|网络异常|服务异常|服务繁忙|系统繁忙|出错了|生成失败|图片生成失败|暂时无法生成|请稍后重试|请重试|Unknown error|Something went wrong|Failed to generate|Generation failed|Network error|Service unavailable|please retry|try again later/i;
  function qwenIntlFault(root) {
    if (!root) return '';
    var box = root.querySelector('.error-message,.green-net-error,[class*="error-message"]');
    var text = String(((box && (box.innerText || box.textContent)) || root.innerText || root.textContent) || '').replace(/\s+/g, ' ').trim();
    if (!text || text.length > 160) return '';
    if (window.TULAIRefuse && window.TULAIRefuse.match(text)) return '';
    if (/额度|次数已用|次数用完|免费次数|已达上限|not enough credits|insufficient credits|out of credits|daily free allowance|chat limit/i.test(text)) return '';
    return box || qwenIntlFaultPattern.test(text) ? text : '';
  }
  function qwenIntlElVisible(el) {
    if (!el || el.isConnected === false) return false;
    try {
      if (typeof getComputedStyle === 'function') {
        var st = getComputedStyle(el);
        if (st && (st.display === 'none' || st.visibility === 'hidden')) return false;
      }
    } catch (e) {}
    var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 1, height: 1 };
    return r.width > 2 && r.height > 2;
  }
  function qwenIntlLiveImages() {
    if (!qwenIntlOnChat()) {
      return { busy: false, done: false, expect: 0, bar: false, replies: 0, reason: 'off-chat', here: window.__tulaiConversationKey(location.href), images: [] };
    }
    var replies = qwenIntlReplies();
    var images = [];
    var seen = {};
    var busy = composerStopping();
    if (!busy) {
      var stops = document.querySelectorAll('button.stop-button');
      for (var si = 0; si < stops.length; si++) {
        if (qwenIntlElVisible(stops[si])) { busy = true; break; }
      }
    }
    var hasFooter = false;
    var pendingImgs = [];
    replies.forEach(function (root) {
      if (replyBusy(root)) busy = true;
      var loaders = root.querySelectorAll('.response-loading,.response-loading-lottie,.function-call-loading,.response-clarify-generating,.qwpcicon-loading2,.lower-center-item-loading-icon,[aria-busy="true"],[role="progressbar"]');
      for (var li = 0; li < loaders.length; li++) {
        if (qwenIntlElVisible(loaders[li])) { busy = true; break; }
      }
      var statusCards = root.querySelectorAll('.qwen-chat-status-card');
      for (var ci = 0; ci < statusCards.length; ci++) {
        var stText = String(statusCards[ci].innerText || statusCards[ci].textContent || '').trim();
        if (/Generating image|Editing image|正在生成|生成中|绘制中|编辑图像/i.test(stText)) busy = true;
      }
      if (root.querySelector('.response-message-footer,.response-message-footer-actions') || actionBar(root)) hasFooter = true;
      Array.prototype.forEach.call(root.querySelectorAll('img'), function (img) {
        if (!inResult(img) || isBaselineImage(img)) return;
        var src = bestURL(img);
        if (!src || junkURL(src) || qwenNoise(src)) return;
        var lazy = String((img.loading || (img.getAttribute && img.getAttribute('loading')) || '')).toLowerCase();
        if (lazy === 'lazy') {
          try { if (typeof img.setAttribute === 'function') img.setAttribute('loading', 'eager'); } catch (e) {}
          try { img.loading = 'eager'; } catch (e) {}
        }
        if (realImg(img)) {
          if (seen[src]) return;
          seen[src] = true;
          var aw = parseInt((img.getAttribute && img.getAttribute('width')) || '0', 10) || 0;
          var ah = parseInt((img.getAttribute && img.getAttribute('height')) || '0', 10) || 0;
          images.push({
            url: src,
            src: 'dom',
            w: img.naturalWidth || aw || 0,
            h: img.naturalHeight || ah || 0
          });
        } else if (img.matches && img.matches('img.common-image-grid-image,.chat-response-media-render img,.qwen-image img,.image-generation-preview img')) {
          pendingImgs.push(img);
          busy = true;
        }
      });
    });
    if (pendingImgs.length) {
      var target = pendingImgs[0];
      if (target && typeof target.scrollIntoView === 'function') {
        try { target.scrollIntoView({ block: 'center', inline: 'nearest' }); } catch (e) {
          try { target.scrollIntoView(); } catch (err) {}
        }
      }
    }
    var done = !busy && images.length > 0 && (hasFooter || pendingGeneration === 0);
    var fault = '';
    if (!busy && !images.length) {
      replies.forEach(function (root) { if (!fault) fault = qwenIntlFault(root); });
    }
    var reason = 'no-reply';
    if (!replies.length && !images.length) reason = 'no-reply';
    else if (busy) reason = 'generating';
    else if (fault) reason = 'error-reply';
    else if (!images.length) reason = 'no-new-image';
    else if (!done) reason = 'unfinished';
    else reason = 'images-ready';
    return { busy: busy, done: done, expect: 0, bar: hasFooter, replies: replies.length, reason: reason, fault: fault, here: window.__tulaiConversationKey(location.href), images: images };
  }
  function qwenReplies() {
    var wrap = String(qwenBound().wrap || '');
    var all = Array.prototype.slice.call(document.querySelectorAll('.chat-answers-card-wrap[data-chat-answers-wrap]'));
    var nodes = all;
    if (wrap) {
      nodes = all.filter(function (node) {
        return String(node.getAttribute('data-chat-answers-wrap') || '') === wrap;
      });
    }
    var fresh = nodes.filter(function (node) { return !baselineReplies.has(node); });
    if (wrap) {
      // 绑住的那张卡在这一轮标记前就在，是上一句的结果。改看标记之后新出来的回复。
      if (fresh.length || !nodes.length) return fresh;
      return all.filter(function (node) { return !baselineReplies.has(node); });
    }
    var floor = window.__tulaiQwenReplyBaseline || 0;
    if (floor < 0) floor = 0;
    if (floor > nodes.length) floor = nodes.length;
    return nodes.slice(floor);
  }
  // 千问没有停止按钮。生成中是 receiving 卡或 card_status=processing。
  // 原图是 img[data-image-resource-id] 的 src，张数要对上 image_num。
  function qwenLiveImages() {
    if (!qwenOnChat()) {
      return { busy: false, done: false, expect: 0, bar: false, replies: 0, reason: 'off-chat', here: window.__tulaiConversationKey(location.href), images: [] };
    }
    var replies = qwenReplies();
    var images = [];
    var seen = {};
    var busy = false;
    var expect = 0;
    var cards = 0;
    replies.forEach(function (root) {
      if (root.querySelector('.answer-receiving-card,[data-mt="custom/receiving"]')) busy = true;
      Array.prototype.forEach.call(root.querySelectorAll('[data-qianwen-card="ai_generate_image_list"]'), function (card) {
        cards++;
        var need = qwenImageNum(card);
        if (need > 0) expect += need;
        var got = 0;
        Array.prototype.forEach.call(card.querySelectorAll('img[data-image-resource-id]'), function (img) {
          var src = qwenSrc(img);
          if (!src || qwenNoise(src)) return;
          got++;
          if (seen[src]) return;
          seen[src] = true;
          images.push({
            url: src,
            src: 'dom',
            w: parseInt(img.getAttribute('data-image-resource-width') || '0', 10) || img.naturalWidth || 0,
            h: parseInt(img.getAttribute('data-image-resource-height') || '0', 10) || img.naturalHeight || 0
          });
        });
        var status = qwenCardStatus(card);
        if (status !== 'complete' || got < (need || 1)) busy = true;
      });
    });
    var done = !busy && cards > 0 && images.length > 0 && (expect === 0 || images.length >= expect);
    var fault = '';
    if (!busy && !cards && !images.length) {
      replies.forEach(function (root) { if (!fault) fault = qwenFault(root); });
    }
    var reason = 'no-reply';
    if (!replies.length && !images.length) reason = 'no-reply';
    else if (busy) reason = 'generating';
    else if (fault) reason = 'error-reply';
    else if (!images.length) reason = 'no-new-image';
    else if (!done) reason = 'unfinished';
    else reason = 'images-ready';
    return { busy: busy, done: done, expect: expect, bar: false, replies: replies.length, reason: reason, fault: fault, here: window.__tulaiConversationKey(location.href), images: images };
  }
  // 千问拒绝请求、积分不足、对话上限或命中会员专属拦截卡时，回复里没有图卡，必须立刻结束等待而不是死等到超时。
  var qwenFaultPattern = /请求异常|网络连接|网络异常|服务异常|服务繁忙|系统繁忙|出错了|生成失败|请稍后重试|请重试|会员专属功能|开通创作会员|订阅会员后可用|积分不足|今日对话次数已达上限/;
  function qwenFault(root) {
    var payCard = root.querySelector('[data-qianwen-card="zd_membership_payment_buttons_card"]');
    if (payCard) {
      var payText = String(payCard.innerText || payCard.textContent || '').replace(/\s+/g, ' ').trim();
      return (payText || '本次生成使用了会员专属功能，开通后立即可用').slice(0, 120);
    }
    var box = root.querySelector('[class*="retry-container"]');
    var text = String((box || root).textContent || '').replace(/\s+/g, ' ').trim();
    if (!text || text.length > 120) return '';
    return box || qwenFaultPattern.test(text) ? text : '';
  }
  // 豆包服务端偶发返回纯文字报错（「出了点问题，请稍后重试。」），不早停会死等到 3 分钟超时。
  var doubaoFaultPattern = /出了点问题|请求异常|网络连接|网络异常|服务异常|服务繁忙|系统繁忙|出错了|生成失败|图片生成失败|暂时无法生成|请稍后重试|请重试/;
  function doubaoFault(root) {
    if (!root || imageSlots(root).length > 0) return '';
    var text = String(root.innerText || root.textContent || '').replace(/\s+/g, ' ').trim();
    if (!text || text.length > 120) return '';
    if (window.TULAIRefuse && window.TULAIRefuse.match(text)) return '';
    if (/额度|次数已用|次数用完|免费次数|已达上限/.test(text)) return '';
    return doubaoFaultPattern.test(text) ? text : '';
  }
  function gridOf(img) {
    var card = img.closest && img.closest('[id^="card_card_ai_generate_image_"]');
    if (card) return card;
    return closestAny(img, ['[class*="image-box-grid-item"]', '[data-testid="mdbox_image"]']) || img;
  }
  // 同一格里的 svg 占位、缩略图和原图只留一张，优先 rc_gen_image 的 cgen 原图。
  function considerImage(groups, img) {
    if (!realImg(img) || isBaselineImage(img)) return;
    var src = bestURL(img);
    if (!src) return;
    var reply = replyOf(img);
    if ((!reply || baselineReplies.has(reply)) && baselineURLs.has(src)) return;
    var node = gridOf(img);
    var width = parseInt((img.getAttribute && img.getAttribute('width')) || '0', 10) || img.naturalWidth || 0;
    var score = urlRank(src, width);
    for (var i = 0; i < groups.length; i++) {
      if (groups[i].node === node) {
        if (score > groups[i].score) {
          groups[i].score = score;
          groups[i].url = src;
          groups[i].img = img;
        }
        return;
      }
    }
    groups.push({ node: node, url: src, score: score, img: img });
  }
  // 自由生图/单图洗图/裂变未启用 doubaoTurn 时，多图网格里落在视口外的卡片仍可能挂着 loading=lazy。
  // 主动把未就绪图格切成 eager 并轮流滚入视口，避免 4 图及以上卡在缩略图/占位状态。
  function nudgeUnscopedDoubaoSlots(replies) {
    if (!/(^|\.)doubao\.com$/.test(pageHost())) return;
    var pending = [];
    replies.forEach(function (root) {
      imageSlots(root).forEach(function (slot) {
        if (!slotReady(slot)) pending.push(slot);
      });
    });
    if (!pending.length) {
      revealEl = null;
      revealAt = 0;
      return;
    }
    pending.forEach(function (slot) {
      var imgs = slot.querySelectorAll ? slot.querySelectorAll('img') : [];
      for (var i = 0; i < imgs.length; i++) {
        var img = imgs[i];
        var lazy = String((img.loading || (img.getAttribute && img.getAttribute('loading')) || '')).toLowerCase();
        if (lazy === 'lazy') {
          try { if (typeof img.setAttribute === 'function') img.setAttribute('loading', 'eager'); } catch (e) {}
          try { img.loading = 'eager'; } catch (e) {}
        }
        if (typeof img.decode === 'function') {
          try { var p = img.decode(); if (p && typeof p.catch === 'function') p.catch(function () {}); } catch (e) {}
        }
      }
    });
    var now = Date.now();
    var idx = 0;
    if (revealEl && now - revealAt < 800) {
      var held = pending.indexOf(revealEl);
      if (held !== -1) idx = held;
    } else if (revealEl) {
      var prev = pending.indexOf(revealEl);
      if (prev !== -1) idx = (prev + 1) % pending.length;
    }
    var target = pending[idx];
    if (target !== revealEl) {
      revealEl = target;
      revealAt = now;
    }
    if (target && typeof target.scrollIntoView === 'function') {
      try { target.scrollIntoView({ block: 'center', inline: 'nearest' }); } catch (e) {
        try { target.scrollIntoView(); } catch (err) {}
      }
    }
  }
  // 完成：这一轮每个图格 data-finished=true，并且每格都有实图。
  // 没有任何 data-finished 时，文字 data-streaming=false 或重新生成/复制也算。
  // 调用方再看地址和 naturalWidth/naturalHeight：像素已够大就只再确认一拍，否则大约 3 秒。
  window.__tulaiLiveImages = function () {
    if (geminiPage()) return geminiLiveImages();
    if (qwenIntlPage()) return qwenIntlLiveImages();
    if (qwenPage()) return qwenLiveImages();
    var scoped = doubaoTurn && window.__tulaiWashPage;
    if (scoped) window.__tulaiWashPage.revealResults(document, doubaoTurn);
    var replies = scoped ? window.__tulaiWashPage.newAssistants(document, doubaoTurn) : replyNodes();
    if (!scoped) nudgeUnscopedDoubaoSlots(replies);
    var groups = [];
    var busy = composerStopping();
    var bar = false;
    var finished = false;
    var expect = 0;
    replies.forEach(function (root) {
      if (replyBusy(root)) busy = true;
      if (actionBar(root)) bar = true;
      if (replyFinished(root)) finished = true;
      var declared = declaredCount(root);
      if (declared > expect) expect = declared;
      root.querySelectorAll('img').forEach(function (img) {
        if (qwenLoadingImage(img)) busy = true;
        considerImage(groups, img);
      });
    });
    // 角色选择器对不上时，回复外的 mdbox 实图也算。用户消息上的图仍然排除。
    if (!scoped) {
      document.querySelectorAll('[data-testid="mdbox_image"] img').forEach(function (img) {
        if (inUserBubble(img)) return;
        considerImage(groups, img);
      });
    }
    var images = [];
    var seen = {};
    groups.forEach(function (group) {
      if (!group.url || seen[group.url]) return;
      seen[group.url] = true;
      images.push({ url: group.url, rawUrl: window.__tulaiDoubaoRaw ? window.__tulaiDoubaoRaw.get(group.url) : '', src: 'dom', w: group.img.naturalWidth || 0, h: group.img.naturalHeight || 0 });
    });
    // 千问生成卡没有 data-finished。灰块没了、实图还在，这一轮就停了。
    if (qwenPage() && !busy && images.length) finished = true;
    var fault = '';
    if (!busy && finished && !images.length) {
      replies.forEach(function (root) { if (!fault) fault = doubaoFault(root); });
    }
    var reason = 'no-reply';
    if (!replies.length && !images.length) reason = 'no-reply';
    else if (fault) reason = 'error-reply';
    else if (!images.length) reason = 'no-new-image';
    else if (busy) reason = 'generating';
    else if (!finished) reason = 'unfinished';
    else reason = 'images-ready';
    return {
      busy: busy,
      done: !busy && images.length > 0 && finished,
      expect: expect,
      bar: bar,
      replies: replies.length,
      reason: reason,
      fault: fault,
      images: images,
    };
  };
  // /chat/<id>、/c/<id>、/app/<id> 是已经打开的对话。/chat、/chat/、/、/c/new-chat、/app、/images 才是新对话。
  window.__tulaiConversationKey = function (href) {
    var path = '';
    try { path = new URL(String(href || ''), location.href).pathname; } catch (e) { return ''; }
    var parts = path.split('/').filter(Boolean);
    var i = parts.indexOf('chat');
    if (i >= 0 && parts[i + 1]) return parts[i + 1];
    var c = parts.indexOf('c');
    if (c >= 0 && parts[c + 1] && !/^(new-chat|new-branch|guest|local)$/i.test(parts[c + 1])) return parts[c + 1];
    var a = parts.indexOf('app');
    if (a >= 0 && parts[a + 1] && !/^(new|images|download|gems|settings|mystuff)$/i.test(parts[a + 1])) return parts[a + 1];
    return '';
  };
  function hiddenComposerAncestor(el) {
    for (var node = el, i = 0; node && i < 8; i++, node = node.parentElement) {
      var cls = String((node.className && node.className.baseVal) || node.className || '');
      if (/(^|\s)invisible(\s|$)/.test(cls)) return true;
      try {
        if (typeof getComputedStyle === 'function') {
          var st = getComputedStyle(node);
          if (st && (st.display === 'none' || st.visibility === 'hidden')) return true;
        }
      } catch (e) {}
    }
    return false;
  }
  function composerBox(el) {
    if (!el || el.readOnly || el.disabled) return false;
    if (el.classList && el.classList.contains('ql-clipboard')) return false;
    if (el.getAttribute && el.getAttribute('aria-hidden') === 'true') return false;
    if (hiddenComposerAncestor(el)) return false;
    var box = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
    if (box.width > 5 && box.height > 5) return true;
    // 窗口在屏幕外时布局盒可以是 0。Slate 输入条、豆包输入框和 Gemini Quill 输入框仍然算。
    var mark = el.getAttribute && (el.getAttribute('data-slate-editor') || el.getAttribute('data-testid') || '');
    if (mark === 'true' || mark === 'chat_input_input') return true;
    return !!(el.classList && el.classList.contains('ql-editor') && el.closest && el.closest('rich-textarea'));
  }
  // 占位「向千问提问」写在 contenteditable=false 里。innerText 会把它当成没发出去的草稿。
  function composerText(el) {
    if (!el) return '';
    if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') return String(el.value || '').replace(/\uFEFF/g, '').trim();
    var out = '';
    if (document.createTreeWalker && typeof NodeFilter !== 'undefined') {
      var walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT), text;
      while ((text = walker.nextNode())) {
        var skip = false;
        for (var node = text.parentElement; node && node !== el; node = node.parentElement) {
          if (!node.getAttribute) continue;
          if (node.getAttribute('contenteditable') === 'false' || node.getAttribute('aria-hidden') === 'true' || node.getAttribute('data-slate-placeholder') === 'true') {
            skip = true;
            break;
          }
        }
        if (!skip) out += text.nodeValue || '';
      }
    } else {
      out = String(el.innerText || el.textContent || '');
    }
    return out.replace(/\uFEFF/g, '').trim();
  }
  function composerNode() {
    if (geminiPage()) {
      var rich = document.querySelectorAll('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"]');
      for (var r = rich.length - 1; r >= 0; r--) {
        if (composerBox(rich[r])) return rich[r];
      }
    }
    var nodes = document.querySelectorAll('textarea,[contenteditable="true"]');
    for (var i = nodes.length - 1; i >= 0; i--) {
      if (composerBox(nodes[i])) return nodes[i];
    }
    return null;
  }
  // 能打字的新对话：文档完成、没有上一轮问答、输入框是空的。
  // 千问首页壳会把地址留在 /chat/<id> 上，正文仍是「你好，我是千问」。没有问答卡就能打字。
  // 豆包地址里还有旧 id 时不能打字。只看见一个 textarea 不够，草稿还在时点发送不会进对话。
  window.__tulaiChatReady = function () {
    var href = String(location.href || '');
    var id = window.__tulaiConversationKey(href);
    var comp = composerNode();
    var messages = assistantNodes().length;
    var cards = document.querySelectorAll('.chat-question-card-wrap,.chat-answers-card-wrap').length;
    var chars = composerText(comp).length;
    var qwen = qwenPage();
    var qwenIntl = qwenIntlPage();
    var gemini = geminiPage();
    var why = [];
    if (!comp) why.push('no-composer');
    // 豆包的长连接让 readyState 一直停在 interactive。输入框已在、对话是空的，就能打字。
    if (!comp && document.readyState !== 'complete') why.push('loading');
    if (chars) why.push('draft');
    if (gemini) {
      var gemMsgs = document.querySelectorAll('model-response,[data-test-id="model-response"],user-query,[data-test-id="user-query"]').length;
      var gemFiles = document.querySelectorAll('uploader-file-preview,uploader-file-preview-container img,file-preview-container img').length;
      if (gemMsgs) why.push('replies');
      if (gemFiles) why.push('files');
      if (id) why.push('url');
    } else if (qwenIntl) {
      var intlMsgs = document.querySelectorAll('[id^="chat-response-message-"],.chat-response-message,.chat-user-message').length;
      var intlFiles = document.querySelectorAll('.message-input-column-file .vision-item-container,.message-input-column-file img.vision-item-image').length;
      if (intlMsgs) why.push('replies');
      if (intlFiles) why.push('files');
      if (id) why.push('url');
    } else if (qwen) {
      if (cards) why.push('cards');
    } else {
      if (messages) why.push('replies');
      if (id) why.push('url');
    }
    var ready = why.length === 0;
    if (comp && !comp.__tulaiNode) comp.__tulaiNode = 'n' + Math.random().toString(36).slice(2);
    return { ready: ready, href: href, id: id, node: comp ? comp.__tulaiNode : '', messages: messages, chars: chars, cards: cards, why: why.join(',') };
  };
  window.__tulaiFreshChat = function () {
    var host = pageHost();
    var id = window.__tulaiConversationKey(location.href);
    var qwen = qwenPage();
    var qwenIntl = qwenIntlPage();
    if (geminiPage()) {
      var gemDirty = document.querySelectorAll('model-response,[data-test-id="model-response"],user-query,[data-test-id="user-query"],uploader-file-preview,uploader-file-preview-container img,file-preview-container img').length > 0 || composerText(composerNode()).length > 0;
      if (!id && !gemDirty && composerNode()) return 'ready';
      var uMatch = String(location.pathname || '').match(/^(\/u\/\d+)\//);
      var gemUrl = 'https://gemini.google.com' + (uMatch ? uMatch[1] : '') + '/app';
      var gemHere = String(location.href || '').split('?')[0].replace(/\/$/, '');
      if (gemHere === gemUrl.replace(/\/$/, '')) {
        try { location.reload(); } catch (e) {}
        return 'reload';
      }
      location.assign(gemUrl);
      return 'url';
    }
    if (qwenIntl) {
      var intlDirty = document.querySelectorAll('[id^="chat-response-message-"],.chat-response-message,.chat-user-message,.message-input-column-file .vision-item-container,.message-input-column-file img.vision-item-image').length > 0 || composerText(composerNode()).length > 0;
      if (!id && !intlDirty && composerNode()) return 'ready';
      var intlUrl = 'https://chat.qwen.ai/';
      var intlHere = String(location.href || '').split('?')[0].replace(/\/$/, '');
      if (intlHere === intlUrl.replace(/\/$/, '')) {
        try { location.reload(); } catch (e) {}
        return 'reload';
      }
      location.assign(intlUrl);
      return 'url';
    }
    var cards = document.querySelectorAll('.chat-question-card-wrap,.chat-answers-card-wrap').length;
    var dirty = (qwen ? cards : assistantNodes().length) > 0 || composerText(composerNode()).length > 0;
    // 空白首页不要再导航。千问地址还挂着旧 /chat/<id>、页面却没有问答卡时也是空白首页。
    if (qwen && !dirty && composerNode()) return 'ready';
    if (!id && !dirty) return 'ready';
    // 豆包优先走 SPA 页内「新对话」入口，省去整页重载；若未生效由 Go 侧超时兜底整页导航。
    if (!qwen && id && window.__tulaiWashPage && typeof window.__tulaiWashPage.clickNewChat === 'function') {
      if (window.__tulaiWashPage.clickNewChat(document)) return 'spa';
    }
    // 点「新对话」会再开一个标签，Chrome 还把窗口拉到前面。同一张标签导航。
    var url = /(qianwen|tongyi)/i.test(host) ? 'https://www.qianwen.com/' : 'https://www.doubao.com/chat/';
    var here = String(location.href || '').split('?')[0].replace(/\/$/, '');
    if (here === url.replace(/\/$/, '')) {
      try { location.reload() } catch (e) {}
      return 'reload';
    }
    location.assign(url);
    return 'url';
  };
  function start() {
    try { mo.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['src', 'srcset'] }); } catch (e) {}
  }
  document.addEventListener('load', function (event) {
    if (event.target.tagName === 'IMG') checkImg(event.target);
  }, true);
  if (document.readyState === 'complete') start();
  else window.addEventListener('load', start);

  // ---- 标记当前任务起点（MarkJs） ----
  window.__tulaiMarkTask = function () {
    doubaoTurn = null;
    window.__tulaiDoubaoTurn = null;
    taskNumber++;
    if (window.__tulaiDoubaoRaw) window.__tulaiDoubaoRaw.clear();
    pendingGeneration = 0;
    window.__tulaiMark = Date.now();
    window.__tulaiAssistantBaseline = assistantNodes().length;
    window.__tulaiQwenReplyBaseline = document.querySelectorAll('.chat-answers-card-wrap[data-chat-answers-wrap]').length;
    window.__tulaiQwenIntlReplyBaseline = qwenIntlTopReplies().length;
    window.__tulaiGeminiReplyBaseline = geminiTopReplies().length;
    existingImages = new WeakSet();
    ignoredURLs = new Set();
    baselineURLs = new Set();
    baselineActions = new WeakSet();
    baselineReplies = new WeakSet();
    assistantNodes().forEach(function (node) { baselineReplies.add(node); });
    document.querySelectorAll('button,[role="button"],a').forEach(function (el) { baselineActions.add(el); });
    Array.from(document.images || document.querySelectorAll('img')).forEach(function (img) {
      existingImages.add(img);
      urlsOf(img).forEach(function (url) {
        baselineURLs.add(url);
        ignoredURLs.add(url);
      });
      checkImg(img);
    });
  };
  window.__tulaiMarkDoubaoTurn = function (prompt) {
    var page = window.__tulaiWashPage;
    if (!page || !page.mark || !page.newAssistants || !page.revealResults || !String(prompt || '').trim()) return false;
    doubaoTurn = page.mark(document, prompt);
    window.__tulaiDoubaoTurn = doubaoTurn;
    return !!doubaoTurn;
  };
  window.__tulaiCurrentDoubaoTurn = function () {
    if (!doubaoTurn) return false;
    window.__tulaiWashPage.newAssistants(document, doubaoTurn);
    return !doubaoTurn.followedByUser;
  };
  window.__tulaiResultsSince = function () {
    return results.filter(function (r) { return r.task === taskNumber && !ignoredURLs.has(r.url); });
  };
})();
