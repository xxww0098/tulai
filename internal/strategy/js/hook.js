// tulai 结果截获 hook：网络层（fetch/XHR）+ DOM 层（聊天区新增大图）双通道，
// 把生成图片 URL/数据 记入 window.__tulaiResults，供 Go 侧等待与保存。
(function () {
  window.__tulaiAssistantQuery = '[data-message-author-role="assistant"],[data-message-role="assistant"],[data-role="assistant"],[data-testid="receive_message"],.chat-answers-card-wrap';
  if (window.__tulaiHookInstalled) return;
  window.__tulaiHookInstalled = true;
  var results = [];
  var existingImages = new WeakSet();
  var ignoredURLs = new Set();
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
      if (realImg(img)) window.__tulaiAddResult(img.currentSrc || img.src, 'dom');
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
  window.__tulaiLiveImages = function () {
    var out = [];
    var seen = {};
    var busy = false;
    var nodes = document.querySelectorAll('[data-testid="mdbox_image"],[data-testid="receive_message"],[data-message-author-role="assistant"],#qk-markdown-react');
    for (var i = 0; i < nodes.length; i++) {
      var text = String(nodes[i].innerText || '');
      if (/正在生成|生成中|绘制中/.test(text)) busy = true;
      var imgs = nodes[i].querySelectorAll('img');
      for (var j = 0; j < imgs.length; j++) {
        if (!realImg(imgs[j])) continue;
        var src = imgs[j].currentSrc || imgs[j].src;
        if (!src || seen[src]) continue;
        seen[src] = true;
        out.push({ url: src, src: 'dom', w: imgs[j].naturalWidth || 0, h: imgs[j].naturalHeight || 0 });
      }
    }
    return { busy: busy, images: out };
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
    window.__tulaiAssistantBaseline = document.querySelectorAll(window.__tulaiAssistantQuery).length;
    existingImages = new WeakSet();
    ignoredURLs = new Set();
    Array.from(document.images).forEach(function (img) {
      existingImages.add(img);
      checkImg(img);
    });
  };
  window.__tulaiResultsSince = function () {
    return results.filter(function (r) { return r.ts >= window.__tulaiMark && !ignoredURLs.has(r.url); });
  };
})();
