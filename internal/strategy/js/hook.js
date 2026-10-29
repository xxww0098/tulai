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
    if (/cgen_lwm|\/orig(?:in)?\b/i.test(url)) rank += 50000;
    if (/cthumb|thumbnail|downsize/i.test(url)) rank -= 20000;
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
  function isBaselineImage(img) {
    var id = imageID(img);
    if (id && baselineIDs.has(id)) return true;
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
      if (/重新生成|再次生成|再试一次|复制|拷贝|^copy$/i.test(labelOf(buttons[i]))) return true;
    }
    return false;
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
    if (img.closest('nav,header,button,[role="button"],svg')) return false;
    var host = String(location.hostname || '');
    if (/(^|\.)doubao\.com$/.test(host)) return !!img.closest('[data-testid="mdbox_image"]');
    if (/(^|\.)(tongyi|qianwen)\.com$/.test(host) || /(^|\.)qwen\.ai$/.test(host)) {
      return !!img.closest('#qk-markdown-react,.markdown-pc-special-class,[data-message-author-role="assistant"],[data-message-role="assistant"]');
    }
    return true;
  }
  function realImg(img) {
    if (!img || !inResult(img)) return false;
    var src = img.currentSrc || img.src || '';
    if (junkURL(src)) return false;
    var nw = img.naturalWidth || 0, nh = img.naturalHeight || 0;
    if (nw && nh && (nw < 128 || nh < 128)) return false;
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
  // 完成只认这一轮回复自己的信号：停止/生成指示消失，并且出现重新生成或复制，
  // 或者图片格子已经达到页面声明的最终张数。张数稳定几百毫秒不算完成。
  window.__tulaiLiveImages = function () {
    var replies = replyNodes();
    var images = [];
    var seen = {};
    var busy = composerStopping();
    var bar = false;
    var expect = 0;
    replies.forEach(function (root) {
      if (replyBusy(root)) busy = true;
      if (actionBar(root)) bar = true;
      var declared = declaredCount(root);
      if (declared > expect) expect = declared;
      root.querySelectorAll('img').forEach(function (img) {
        if (!realImg(img) || isBaselineImage(img)) return;
        var src = bestURL(img);
        if (!src || seen[src] || baselineURLs.has(src)) return;
        seen[src] = true;
        images.push({ url: src, src: 'dom', w: img.naturalWidth || 0, h: img.naturalHeight || 0 });
      });
    });
    var gridFull = expect > 0 && images.length >= expect;
    return { busy: busy, done: !busy && images.length > 0 && (bar || gridFull), expect: expect, images: images };
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
    return box.width > 5 && box.height > 5;
  }
  // 新对话可以打字：有一个还在的输入框，并且不在 loading。
  window.__tulaiChatReady = function () {
    var href = String(location.href || '');
    var nodes = document.querySelectorAll('textarea,[contenteditable="true"]');
    var comp = null;
    for (var i = nodes.length - 1; i >= 0; i--) {
      if (!composerBox(nodes[i])) continue;
      comp = nodes[i];
      break;
    }
    if (!comp || document.readyState === 'loading') {
      return { ready: false, href: href, id: window.__tulaiConversationKey(href) };
    }
    if (!comp.__tulaiNode) comp.__tulaiNode = 'n' + Math.random().toString(36).slice(2);
    return { ready: true, href: href, id: window.__tulaiConversationKey(href), node: comp.__tulaiNode };
  };
  window.__tulaiFreshChat = function () {
    var host = String(location.hostname || '');
    // 已经在新对话地址上就别再点。点下去会换掉正在用的输入框。
    if (!window.__tulaiConversationKey(location.href)) return 'ready';
    var labels = /(^|\.)doubao\.com$/i.test(host) ? ['新对话', '新建对话'] : ['新建对话', '新对话', '新建聊天'];
    var nodes = document.querySelectorAll('a,button,[role="button"]');
    for (var i = 0; i < nodes.length; i++) {
      var text = labelOf(nodes[i]);
      var box = nodes[i].getBoundingClientRect ? nodes[i].getBoundingClientRect() : { width: 1, height: 1 };
      if (box.width <= 8 || box.height <= 8) continue;
      for (var k = 0; k < labels.length; k++) {
        if (text === labels[k] || (text.indexOf(labels[k]) === 0 && text.length <= labels[k].length + 12)) {
          try { nodes[i].click(); } catch (e) {}
          return 'button';
        }
      }
    }
    var url = /(qianwen|tongyi|qwen)/i.test(host) ? 'https://www.qianwen.com/' : 'https://www.doubao.com/chat/';
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
    existingImages = new WeakSet();
    ignoredURLs = new Set();
    baselineURLs = new Set();
    baselineIDs = new Set();
    baselineActions = new WeakSet();
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
