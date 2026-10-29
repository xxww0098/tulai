// tulai 结果截获 hook：网络层（fetch/XHR）+ DOM 层（聊天区新增大图）双通道，
// 把生成图片 URL/数据 记入 window.__tulaiResults，供 Go 侧等待与保存。
(function () {
  window.__tulaiAssistantQuery = '[data-message-author-role="assistant"],[data-message-role="assistant"],[data-role="assistant"],[data-testid="receive_message"],.chat-answers-card-wrap';
  if (window.__tulaiHookInstalled) return;
  window.__tulaiHookInstalled = true;
  var results = [];
  var existingImages = new WeakSet();
  var ignoredURLs = new Set();
  var baselineURLs = new Set();
  var baselineIDs = new Set();
  var baselineActions = new WeakSet();
  var baselineReplies = new WeakSet();
  var idSeq = 0;
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
    for (var i = 0; i < results.length; i++) if (results[i].url === url) return;
    results.push({ url: url, src: src || '', ts: Date.now() });
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
    window.fetch = function (input, init) {
      var url = (typeof input === 'string') ? input : (input && input.url) || '';
      var requestTask = taskNumber, relevant = generationURL(url), ticket = relevant ? beginGeneration() : null, p;
      try { p = origFetch.apply(this, arguments); } catch (e) { finishGeneration(ticket); throw e; }
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
    };
  }

  // ---- XHR 拦截 ----
  var origOpen = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (method, url) {
    this.__tulaiUrl = url;
    return origOpen.apply(this, arguments);
  };
  var origSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.send = function () {
    var xhr = this, requestTask = taskNumber, ticket = generationURL(xhr.__tulaiUrl || '') ? beginGeneration() : null;
    xhr.addEventListener('loadend', function () { finishGeneration(ticket); }, { once: true });
    xhr.addEventListener('load', function () {
      if (requestTask !== taskNumber) return;
      try {
        var mime = xhr.getResponseHeader('content-type') || '';
        if (looksLikeImage(xhr.__tulaiUrl, mime)) {
          if ((xhr.responseType === '' || xhr.responseType === 'blob') && xhr.response && xhr.response.size > 20480) {
            window.__tulaiAddResult(xhr.__tulaiUrl, 'net');
          }
        } else if (/json|text/i.test(mime)) {
          var txt = typeof xhr.responseText === 'string' ? xhr.responseText : '';
          var re = /https?:\/\/[^\s"'\\]+?(?:\.(?:png|jpe?g|webp)(?:\?[^\s"'\\]*)?|tosv[^"'\\]*|object_(?:basic|image)[^\s"'\\]*)/gi;
          var m;
          while ((m = re.exec(txt))) window.__tulaiAddResult(m[0], 'xhr');
        }
      } catch (e) {}
    });
    try { return origSend.apply(this, arguments); } catch (e) { finishGeneration(ticket); throw e; }
  };

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
  function imageID(img) {
    if (!img) return '';
    var id = img.getAttribute && (img.getAttribute('data-tulai-id') || img.id || img.getAttribute('data-id') || '');
    return String(id || '');
  }
  function rememberID(img) {
    var id = imageID(img);
    if (id || !img || typeof img.setAttribute !== 'function') return id;
    id = 'tulai-' + (++idSeq);
    try { img.setAttribute('data-tulai-id', id); } catch (e) {}
    return id;
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
    if (/cgen_lwm|\/orig(?:in)?\b/i.test(url)) rank += 80000;
    if (/cthumb|thumbnail|downsize/i.test(url)) rank -= 20000;
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
    return closestAny(img, ['[data-testid="receive_message"]', '[data-message-author-role="assistant"]', '[data-message-role="assistant"]', '[data-role="assistant"]', '.chat-answers-card-wrap']);
  }
  // 用户气泡里的 message_image_content 是上传的原图。助手回复里的同名容器才是生成图。
  function inUserBubble(img) {
    var node = closestAny(img, ['[data-testid="send_message"]', '[data-testid="receive_message"]', '[data-message-role="user"]', '[data-message-role="assistant"]', '[data-message-author-role="user"]', '[data-message-author-role="assistant"]']);
    if (!node || !node.getAttribute) return false;
    var tid = node.getAttribute('data-testid') || '';
    var role = node.getAttribute('data-message-role') || node.getAttribute('data-message-author-role') || '';
    if (tid === 'receive_message' || role === 'assistant') return false;
    return tid === 'send_message' || role === 'user';
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
      if (!/停止生成|^停止$|停止响应|Stop generating/i.test(labelOf(el))) continue;
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
    if (img.closest('nav') || img.closest('header') || img.closest('button') || img.closest('[role="button"]') || img.closest('svg')) return false;
    var host = pageHost();
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
    return /(^|\.)(tongyi|qianwen)\.com$/.test(host) || /(^|\.)qwen\.ai$/.test(host);
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
  window.__tulaiQwenOnChat = qwenOnChat;
  window.__tulaiQwenNote = function () {
    var url = window.__tulaiConversationKey(location.href);
    var nodes = document.querySelectorAll('.chat-question-card-wrap[data-chat-question-wrap]');
    var wrap = nodes.length ? String(nodes[nodes.length - 1].getAttribute('data-chat-question-wrap') || '') : '';
    if (!wrap) {
      var answers = document.querySelectorAll('.chat-answers-card-wrap[data-chat-answers-wrap]');
      wrap = answers.length ? String(answers[answers.length - 1].getAttribute('data-chat-answers-wrap') || '') : '';
    }
    return { url: url || '', wrap: wrap || '' };
  };
  function qwenReplies() {
    var wrap = qwenBound().wrap;
    var sel = '.chat-answers-card-wrap[data-chat-answers-wrap]';
    if (wrap) sel = '.chat-answers-card-wrap[data-chat-answers-wrap="' + wrap.replace(/["\\]/g, '') + '"]';
    var nodes = Array.prototype.slice.call(document.querySelectorAll(sel));
    if (wrap) return nodes;
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
    var reason = 'no-reply';
    if (!replies.length && !images.length) reason = 'no-reply';
    else if (busy) reason = 'generating';
    else if (!images.length) reason = 'no-new-image';
    else if (!done) reason = 'unfinished';
    else reason = 'images-ready';
    return { busy: busy, done: done, expect: expect, bar: false, replies: replies.length, reason: reason, here: window.__tulaiConversationKey(location.href), images: images };
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
  // 完成：这一轮每个图格 data-finished=true，并且每格都有实图。
  // 没有任何 data-finished 时，文字 data-streaming=false 或重新生成/复制也算。
  // 地址要再稳大约 3 秒才由调用方收下；这里只报告这一拍的判断。
  window.__tulaiLiveImages = function () {
    if (qwenPage()) return qwenLiveImages();
    var replies = replyNodes();
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
    document.querySelectorAll('[data-testid="mdbox_image"] img').forEach(function (img) {
      if (inUserBubble(img)) return;
      considerImage(groups, img);
    });
    var images = [];
    var seen = {};
    groups.forEach(function (group) {
      if (!group.url || seen[group.url]) return;
      seen[group.url] = true;
      images.push({ url: group.url, src: 'dom', w: group.img.naturalWidth || 0, h: group.img.naturalHeight || 0 });
    });
    // 千问生成卡没有 data-finished。灰块没了、实图还在，这一轮就停了。
    if (qwenPage() && !busy && images.length) finished = true;
    var reason = 'no-reply';
    if (!replies.length && !images.length) reason = 'no-reply';
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
      images: images,
    };
  };
  // /chat/<id> 是已经打开的对话。/chat 或 /chat/ 才是新对话。
  window.__tulaiConversationKey = function (href) {
    var path = '';
    try { path = new URL(String(href || ''), location.href).pathname; } catch (e) { return ''; }
    var parts = path.split('/').filter(Boolean);
    var i = parts.indexOf('chat');
    if (i < 0 || !parts[i + 1]) return '';
    return parts[i + 1];
  };
  function composerBox(el) {
    if (!el || el.readOnly || el.disabled) return false;
    if (el.getAttribute && el.getAttribute('aria-hidden') === 'true') return false;
    var box = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
    if (box.width > 5 && box.height > 5) return true;
    // 窗口在屏幕外时布局盒可以是 0。Slate 输入条和豆包输入框仍然算。
    var mark = el.getAttribute && (el.getAttribute('data-slate-editor') || el.getAttribute('data-testid') || '');
    return mark === 'true' || mark === 'chat_input_input';
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
    var why = [];
    if (!comp) why.push('no-composer');
    if (document.readyState !== 'complete') why.push('loading');
    if (chars) why.push('draft');
    if (qwen) {
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
    var cards = document.querySelectorAll('.chat-question-card-wrap,.chat-answers-card-wrap').length;
    var dirty = (qwen ? cards : assistantNodes().length) > 0 || composerText(composerNode()).length > 0;
    // 空白首页不要再导航。千问地址还挂着旧 /chat/<id>、页面却没有问答卡时也是空白首页。
    if (qwen && !dirty && composerNode()) return 'ready';
    if (!id && !dirty) return 'ready';
    // 点「新对话」会再开一个标签，Chrome 还把窗口拉到前面。同一张标签导航。
    var url = /(qianwen|tongyi|qwen)/i.test(host) ? 'https://www.qianwen.com/' : 'https://www.doubao.com/chat/';
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
    taskNumber++;
    pendingGeneration = 0;
    window.__tulaiMark = Date.now();
    window.__tulaiAssistantBaseline = assistantNodes().length;
    window.__tulaiQwenReplyBaseline = document.querySelectorAll('.chat-answers-card-wrap[data-chat-answers-wrap]').length;
    existingImages = new WeakSet();
    ignoredURLs = new Set();
    baselineURLs = new Set();
    baselineIDs = new Set();
    baselineActions = new WeakSet();
    baselineReplies = new WeakSet();
    assistantNodes().forEach(function (node) { baselineReplies.add(node); });
    document.querySelectorAll('button,[role="button"],a').forEach(function (el) { baselineActions.add(el); });
    Array.from(document.images || document.querySelectorAll('img')).forEach(function (img) {
      existingImages.add(img);
      var id = rememberID(img);
      if (id) baselineIDs.add(id);
      urlsOf(img).forEach(function (url) {
        baselineURLs.add(url);
        ignoredURLs.add(url);
      });
      checkImg(img);
    });
  };
  window.__tulaiResultsSince = function () {
    return results.filter(function (r) { return r.ts >= window.__tulaiMark && !ignoredURLs.has(r.url); });
  };
})();
