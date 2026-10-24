// xbs 结果截获 hook：网络层（fetch/XHR）+ DOM 层（聊天区新增大图）双通道，
// 把生成图片 URL/数据 记入 window.__xbsResults，供 Go 侧等待与保存。
(function () {
  if (window.__xbsHookInstalled) return;
  window.__xbsHookInstalled = true;
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
  window.__xbsPendingGeneration = function () { return pendingGeneration; };

  window.__xbsResults = results;
  window.__xbsMark = Date.now();
  window.__xbsAddResult = function (url, src) {
    if (!url) return;
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
                while ((m = re.exec(txt))) window.__xbsAddResult(m[0], 'stream');
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
                if (b.size > 20480) window.__xbsAddResult(url, 'net'); // 过滤小图标
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
    this.__xbsUrl = url;
    return origOpen.apply(this, arguments);
  };
  var origSend = XMLHttpRequest.prototype.send;
  XMLHttpRequest.prototype.send = function () {
    var xhr = this, requestTask = taskNumber, ticket = generationURL(xhr.__xbsUrl || '') ? beginGeneration() : null;
    xhr.addEventListener('loadend', function () { finishGeneration(ticket); }, { once: true });
    xhr.addEventListener('load', function () {
      if (requestTask !== taskNumber) return;
      try {
        var mime = xhr.getResponseHeader('content-type') || '';
        if (looksLikeImage(xhr.__xbsUrl, mime)) {
          if ((xhr.responseType === '' || xhr.responseType === 'blob') && xhr.response && xhr.response.size > 20480) {
            window.__xbsAddResult(xhr.__xbsUrl, 'net');
          }
        } else if (/json|text/i.test(mime)) {
          var txt = typeof xhr.responseText === 'string' ? xhr.responseText : '';
          var re = /https?:\/\/[^\s"'\\]+?(?:\.(?:png|jpe?g|webp)(?:\?[^\s"'\\]*)?|tosv[^"'\\]*|object_(?:basic|image)[^\s"'\\]*)/gi;
          var m;
          while ((m = re.exec(txt))) window.__xbsAddResult(m[0], 'xhr');
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
  function checkImg(img) {
    try {
      if (existingImages.has(img)) {
        ignoredURLs.add(img.src);
        ignoredURLs.add(img.currentSrc);
        return;
      }
      var r = img.getBoundingClientRect();
      if (r.width >= 180 && r.height >= 180) { // 生成结果是大图，排除头像/图标
        var src = img.currentSrc || img.src;
        if (src && !/avatar|icon|logo|emoji/i.test(src)) window.__xbsAddResult(src, 'dom');
      }
    } catch (e) {}
  }
  function start() {
    try { mo.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ['src', 'srcset'] }); } catch (e) {}
  }
  document.addEventListener('load', function (event) {
    if (event.target.tagName === 'IMG') checkImg(event.target);
  }, true);
  if (document.readyState === 'complete') start();
  else window.addEventListener('load', start);

  // ---- 标记当前任务起点（MarkJs） ----
  window.__xbsMarkTask = function () {
    taskNumber++;
    pendingGeneration = 0;
    window.__xbsMark = Date.now();
    existingImages = new WeakSet();
    ignoredURLs = new Set();
    Array.from(document.images).forEach(function (img) {
      existingImages.add(img);
      checkImg(img);
    });
  };
  window.__xbsResultsSince = function () {
    return results.filter(function (r) { return r.ts >= window.__xbsMark && !ignoredURLs.has(r.url); });
  };
})();
