// 豆包响应里的 image_ori_raw 与预览版本属于同一张图。
(function () {
  var RAW_EVENT = '__pb_raw_images__';
  if (typeof window.__tulaiAddResult === 'function' && window.__tulaiDoubaoRaw) return;
  var urls = new Map();
  var originals = new WeakMap();
  var origToString = Function.prototype.toString;
  function unwrap(fn) {
    var cur = fn;
    while (cur && (typeof cur === 'object' || typeof cur === 'function') && originals.has(cur)) {
      cur = originals.get(cur);
    }
    return cur;
  }
  var toStringDesc = Object.getOwnPropertyDescriptor(Function.prototype, 'toString');
  if (!toStringDesc || toStringDesc.configurable) {
    var wrappedToString = new Proxy(origToString, {
      apply: function (target, thisArg, args) {
        return Reflect.apply(target, unwrap(thisArg), args);
      }
    });
    originals.set(wrappedToString, origToString);
    try {
      Object.defineProperty(Function.prototype, 'toString', {
        value: wrappedToString,
        writable: toStringDesc ? toStringDesc.writable : true,
        enumerable: toStringDesc ? toStringDesc.enumerable : false,
        configurable: true
      });
    } catch (e) {}
  }
  function key(raw) {
    try {
      var url = new URL(raw, location.href);
      if (url.protocol !== 'https:' || !/(^|\.)byteimg\.com$/i.test(url.hostname)) return '';
      return url.pathname.split('~tplv-')[0];
    } catch (e) { return ''; }
  }
  var store = {
    get: function (url) { return urls.get(key(url)) || ''; },
    clear: function () { urls.clear(); }
  };
  function attachStore() {
    if (typeof window.__tulaiAddResult === 'function' && !window.__tulaiDoubaoRaw) {
      window.__tulaiDoubaoRaw = store;
    }
  }
  attachStore();
  var originalParse = JSON.parse;
  var parseDesc = Object.getOwnPropertyDescriptor(JSON, 'parse');
  var wrappedParse = new Proxy(originalParse, {apply: function (target, receiver, args) {
    var result = Reflect.apply(target, receiver, args);
    attachStore();
    var host = (typeof window.__tulaiAddResult === 'function' ? window.__tulaiPageHost : '') || location.hostname || '';
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
        if (pairs.length && typeof document !== 'undefined' && document && typeof document.dispatchEvent === 'function' && typeof CustomEvent === 'function') {
          document.dispatchEvent(new CustomEvent(RAW_EVENT, { detail: JSON.stringify(pairs) }));
        }
      } catch (e) {}
    }
    return result;
  }});
  originals.set(wrappedParse, originalParse);
  if (!parseDesc || parseDesc.configurable) {
    Object.defineProperty(JSON, 'parse', {
      value: wrappedParse,
      writable: parseDesc ? parseDesc.writable : true,
      enumerable: parseDesc ? parseDesc.enumerable : false,
      configurable: true
    });
  } else {
    JSON.parse = wrappedParse;
  }
})();
