// 豆包响应里的 image_ori_raw 与预览版本属于同一张图。
(function () {
  if (window.__tulaiDoubaoRaw) return;
  var urls = new Map();
  function key(raw) {
    try {
      var url = new URL(raw, location.href);
      if (url.protocol !== 'https:' || !/(^|\.)byteimg\.com$/i.test(url.hostname)) return '';
      return url.pathname.split('~tplv-')[0];
    } catch (e) { return ''; }
  }
  window.__tulaiDoubaoRaw = {
    get: function (url) { return urls.get(key(url)) || ''; },
    clear: function () { urls.clear(); }
  };
  var originalParse = JSON.parse;
  JSON.parse = new Proxy(originalParse, {apply: function (target, receiver, args) {
    var result = Reflect.apply(target, receiver, args);
    var host = window.__tulaiPageHost || location.hostname || '';
    if (/(^|\.)doubao\.com$/.test(host) && typeof args[0] === 'string' && args[0].indexOf('image_ori_raw') !== -1) {
      try {
        var stack = [result], seen = new WeakSet(), count = 0, pairs = [];
        while (stack.length && count++ < 10000) {
          var item = stack.pop();
          if (!item || typeof item !== 'object' || seen.has(item)) continue;
          seen.add(item);
          var raw = item.image_ori_raw && item.image_ori_raw.url;
          if (key(raw)) {
            ['image_ori_raw', 'image_ori', 'image_preview', 'image_thumb'].forEach(function (name) {
              var source = item[name] && item[name].url, path = key(source);
              if (path) {
                if (urls.size >= 2048 && !urls.has(path)) urls.delete(urls.keys().next().value);
                urls.set(path, raw);
                if (pairs.length < 2048) pairs.push({ source: source, raw: raw });
              }
            });
          }
          Object.keys(item).forEach(function (name) {
            if (item[name] && typeof item[name] === 'object') stack.push(item[name]);
          });
        }
        if (pairs.length && typeof window.postMessage === 'function') {
          window.postMessage({ type: 'TULAI_DOUBAO_RAW_IMAGES', pairs: pairs }, location.origin);
        }
      } catch (e) {}
    }
    return result;
  }});
})();
