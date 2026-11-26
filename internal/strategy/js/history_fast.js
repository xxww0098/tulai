// tulai 极速历史删除助手：捕获首条 DOM 删除发出的真实请求模板，后续在页面上下文内批量并发回放。
// 外层包装在站点自身 fetch/XHR（如 secsdk/baxia 签名包装器）之外，回放时自动复用站点登录态与动态签名。
(function () {
  if (typeof window !== 'undefined' && window.__tulaiHistoryFast) return

  const ID_TOKEN = '__TULAI_CONV_ID__'
  const ID_ENC_TOKEN = '__TULAI_CONV_ID_ENC__'
  const IGNORE_URL_RE = /(?:^|[/?&._-])(log|logs|track|beacon|report|metric|metrics|stat|stats|event|events|aplus|slardar|telemetry|heartbeat|ping|unread|list|history|detail|get_)(?:$|[/?&._-])/i
  const DELETE_URL_RE = /(del|delete|remove|clear|trash|drop|destroy|clean)/i
  const DELETE_BODY_RE = /(delete|remove|is_deleted|del_)/i
  const RATE_LIMIT_RE = /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate\s*limit|too many requests|throttl/i
  const CAPTCHA_RE = /rgv587_flag|baxia|punish|captcha|安全验证|滑动验证|人机验证/i
  const FORBIDDEN_HEADER_RE = /^(host|origin|referer|cookie|content-length|user-agent|connection|accept-encoding|sec-.*|proxy-.*)$/i
  const TRANSIENT_SIGN_PARAMS = ['a_bogus', 'X-Bogus', 'x-bogus', '_signature']
  const CODE_FIELDS = ['code', 'err_no', 'errno', 'status_code', 'errorCode', 'ret', 'errCode']
  const SUCCESS_FIELDS = ['success', 'ok', 'isSuccess']

  let installed = false
  let pageFetch = null
  let origXHROpen = null
  let origXHRSetHeader = null
  let origXHRSend = null
  let replayingDepth = 0

  let armedTarget = ''
  let armedAt = 0
  let candidates = []
  let pendingCaptures = []

  function escapeRegExp(s) {
    return String(s).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  }

  function replaceAllLiteral(str, find, repl) {
    if (!find) return str
    return String(str).split(find).join(repl)
  }

  function normalizeHeaders(input) {
    const out = {}
    if (!input) return out
    if (typeof Headers !== 'undefined' && input instanceof Headers) {
      input.forEach(function (v, k) {
        if (!FORBIDDEN_HEADER_RE.test(k)) out[k] = String(v)
      })
      return out
    }
    if (Array.isArray(input)) {
      for (let i = 0; i < input.length; i++) {
        const pair = input[i]
        if (pair && pair.length >= 2 && !FORBIDDEN_HEADER_RE.test(String(pair[0]))) {
          out[String(pair[0])] = String(pair[1])
        }
      }
      return out
    }
    if (typeof input === 'object') {
      const keys = Object.keys(input)
      for (let i = 0; i < keys.length; i++) {
        const k = keys[i]
        if (!FORBIDDEN_HEADER_RE.test(k) && input[k] != null) out[k] = String(input[k])
      }
    }
    return out
  }

  function extractSuccessSig(status, text) {
    const sig = { status: status, isJSON: false }
    if (!text) return sig
    const trimmed = String(text).trim()
    if (!trimmed || (trimmed[0] !== '{' && trimmed[0] !== '[')) return sig
    try {
      const obj = JSON.parse(trimmed)
      if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return sig
      sig.isJSON = true
      for (let i = 0; i < CODE_FIELDS.length; i++) {
        const f = CODE_FIELDS[i]
        if (Object.prototype.hasOwnProperty.call(obj, f) && (typeof obj[f] === 'number' || typeof obj[f] === 'string')) {
          sig.codeField = f
          sig.codeValue = obj[f]
          break
        }
      }
      for (let j = 0; j < SUCCESS_FIELDS.length; j++) {
        const sf = SUCCESS_FIELDS[j]
        if (Object.prototype.hasOwnProperty.call(obj, sf) && typeof obj[sf] === 'boolean') {
          sig.successField = sf
          sig.successValue = obj[sf]
          break
        }
      }
    } catch (e) {}
    return sig
  }

  function responseLooksOK(status, text, expectedSig) {
    if (status === 429) return { ok: false, stopped: 'rate_limit', error: '触发接口频率限制(429)' }
    const raw = String(text || '')
    if (CAPTCHA_RE.test(raw)) return { ok: false, stopped: 'captcha', error: '触发安全验证' }
    if (RATE_LIMIT_RE.test(raw)) return { ok: false, stopped: 'rate_limit', error: '触发操作频率限制' }
    if (status === 401 || status === 403) return { ok: false, stopped: 'not_logged_in', error: '账号登录失效或无权限(' + status + ')' }
    if (status < 200 || status >= 300) return { ok: false, error: 'HTTP ' + status }
    const trimmed = raw.trim()
    if (trimmed && (trimmed[0] === '{' || trimmed[0] === '[')) {
      try {
        const obj = JSON.parse(trimmed)
        if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
          if (expectedSig && expectedSig.isJSON) {
            if (expectedSig.successField && obj[expectedSig.successField] !== expectedSig.successValue) {
              return { ok: false, error: '接口返回失败(' + expectedSig.successField + '=' + obj[expectedSig.successField] + ')' }
            }
            if (expectedSig.codeField && obj[expectedSig.codeField] !== expectedSig.codeValue) {
              return { ok: false, error: '接口返回错误码(' + expectedSig.codeField + '=' + obj[expectedSig.codeField] + ')' }
            }
          } else {
            for (let j = 0; j < SUCCESS_FIELDS.length; j++) {
              const sf = SUCCESS_FIELDS[j]
              if (Object.prototype.hasOwnProperty.call(obj, sf) && obj[sf] === false) {
                return { ok: false, error: '接口返回失败(' + sf + '=false)' }
              }
            }
            for (let i = 0; i < CODE_FIELDS.length; i++) {
              const f = CODE_FIELDS[i]
              if (Object.prototype.hasOwnProperty.call(obj, f)) {
                const v = obj[f]
                if (v !== 0 && v !== '0' && v !== 200 && v !== '200' && v !== 'OK' && v !== 'ok' && v !== '') {
                  return { ok: false, error: '接口返回错误码(' + f + '=' + v + ')' }
                }
              }
            }
          }
        }
      } catch (e) {}
    }
    return { ok: true }
  }

  function buildTemplate(transport, method, url, headers, bodyStr, targetID, status, respText) {
    const m = String(method || 'GET').toUpperCase()
    if (m !== 'POST' && m !== 'DELETE' && m !== 'PUT' && m !== 'PATCH') return null
    const rawURL = String(url || '')
    if (!rawURL || IGNORE_URL_RE.test(rawURL)) return null
    const encID = encodeURIComponent(targetID)
    const body = typeof bodyStr === 'string' ? bodyStr : ''
    const urlHas = rawURL.indexOf(targetID) >= 0 || (encID !== targetID && rawURL.indexOf(encID) >= 0)
    const bodyHas = body.indexOf(targetID) >= 0 || (encID !== targetID && body.indexOf(encID) >= 0)
    if (!urlHas && !bodyHas) return null

    const check = responseLooksOK(status, respText, null)
    if (!check.ok) return null

    let urlTemplate = rawURL
    if (encID !== targetID) urlTemplate = replaceAllLiteral(urlTemplate, encID, ID_ENC_TOKEN)
    urlTemplate = replaceAllLiteral(urlTemplate, targetID, ID_TOKEN)

    let bodyTemplate = body
    let bodyKind = 'raw'
    const trimmedBody = body.trim()
    if (trimmedBody && (trimmedBody[0] === '{' || trimmedBody[0] === '[')) {
      try { JSON.parse(trimmedBody); bodyKind = 'json' } catch (e) {}
    } else {
      const ct = Object.keys(headers || {}).find(function (k) { return k.toLowerCase() === 'content-type' })
      if (ct && /x-www-form-urlencoded/i.test(headers[ct])) bodyKind = 'form'
    }
    if (bodyTemplate) {
      if (encID !== targetID) bodyTemplate = replaceAllLiteral(bodyTemplate, encID, ID_ENC_TOKEN)
      bodyTemplate = replaceAllLiteral(bodyTemplate, targetID, ID_TOKEN)
    }

    let score = 0
    if (DELETE_URL_RE.test(rawURL)) score += 10
    if (m === 'DELETE') score += 5
    if (DELETE_BODY_RE.test(body)) score += 4
    if (bodyHas) score += 2
    if (urlHas) score += 1

    return {
      transport: transport,
      method: m,
      urlTemplate: urlTemplate,
      bodyTemplate: bodyTemplate,
      bodyKind: bodyKind,
      headers: normalizeHeaders(headers),
      successSig: extractSuccessSig(status, respText),
      score: score,
      capturedAt: Date.now(),
    }
  }

  function recordCandidate(tpl) {
    if (!tpl) return
    candidates.push(tpl)
    candidates.sort(function (a, b) { return b.score - a.score || b.capturedAt - a.capturedAt })
  }

  function serializeBody(body) {
    if (body == null) return ''
    if (typeof body === 'string') return body
    if (typeof URLSearchParams !== 'undefined' && body instanceof URLSearchParams) return body.toString()
    return null
  }

  function install() {
    if (installed || typeof window === 'undefined') return
    installed = true

    if (typeof window.fetch === 'function') {
      pageFetch = window.fetch
      window.fetch = function (input, init) {
        if (!armedTarget || replayingDepth > 0) {
          return pageFetch.apply(this, arguments)
        }
        const target = armedTarget
        let url = ''
        let method = (init && init.method) || 'GET'
        let headers = (init && init.headers) || {}
        let bodyStr = init && init.body !== undefined ? serializeBody(init.body) : ''
        let reqClone = null
        try {
          if (typeof input === 'string') {
            url = input
          } else if (input && typeof input.url === 'string') {
            url = input.url
            if (!init || !init.method) method = input.method || method
            if (!init || !init.headers) headers = input.headers || headers
            if ((!init || init.body === undefined) && typeof input.clone === 'function') {
              try { reqClone = input.clone() } catch (e) {}
            }
          } else if (input && typeof input.toString === 'function') {
            url = input.toString()
          }
        } catch (e) {}

        const p = pageFetch.apply(this, arguments)
        const mUpper = String(method || 'GET').toUpperCase()
        if (mUpper === 'POST' || mUpper === 'DELETE' || mUpper === 'PUT' || mUpper === 'PATCH') {
          const track = Promise.resolve(p).then(async function (res) {
            try {
              if (!res) return
              let actualBody = bodyStr
              if (actualBody === '' && reqClone) {
                try { actualBody = await reqClone.text() } catch (e) {}
              }
              if (actualBody === null) return
              let respText = ''
              if (typeof res.clone === 'function') {
                try { respText = await res.clone().text() } catch (e) {}
              }
              const tpl = buildTemplate('fetch', mUpper, url, headers, actualBody, target, res.status, respText)
              recordCandidate(tpl)
            } catch (e) {}
          }, function () {})
          pendingCaptures.push(track)
        }
        return p
      }
    }

    if (typeof XMLHttpRequest !== 'undefined' && XMLHttpRequest.prototype) {
      origXHROpen = XMLHttpRequest.prototype.open
      origXHRSetHeader = XMLHttpRequest.prototype.setRequestHeader
      origXHRSend = XMLHttpRequest.prototype.send

      XMLHttpRequest.prototype.open = function (method, url) {
        this.__tulaiCap = { method: String(method || 'GET').toUpperCase(), url: String(url || ''), headers: {} }
        return origXHROpen.apply(this, arguments)
      }
      XMLHttpRequest.prototype.setRequestHeader = function (k, v) {
        if (this.__tulaiCap && k) this.__tulaiCap.headers[String(k)] = String(v)
        return origXHRSetHeader.apply(this, arguments)
      }
      XMLHttpRequest.prototype.send = function (body) {
        if (!armedTarget || replayingDepth > 0 || !this.__tulaiCap) {
          return origXHRSend.apply(this, arguments)
        }
        const meta = this.__tulaiCap
        const target = armedTarget
        const bodyStr = serializeBody(body)
        if (bodyStr !== null && (meta.method === 'POST' || meta.method === 'DELETE' || meta.method === 'PUT' || meta.method === 'PATCH')) {
          const xhr = this
          const p = new Promise(function (resolve) {
            const done = function () {
              try {
                const respText = typeof xhr.responseText === 'string' ? xhr.responseText : ''
                const tpl = buildTemplate('xhr', meta.method, meta.url, meta.headers, bodyStr, target, xhr.status, respText)
                recordCandidate(tpl)
              } catch (e) {}
              resolve()
            }
            if (typeof xhr.addEventListener === 'function') {
              xhr.addEventListener('loadend', done, { once: true })
            } else {
              resolve()
            }
          })
          pendingCaptures.push(p)
        }
        return origXHRSend.apply(this, arguments)
      }
    }
  }

  function arm(targetID) {
    install()
    armedTarget = String(targetID || '').trim()
    armedAt = Date.now()
    candidates = []
    pendingCaptures = []
  }

  function disarm() {
    armedTarget = ''
  }

  async function takeTemplate(waitMs) {
    const ms = waitMs > 0 ? waitMs : 400
    if (pendingCaptures.length) {
      await Promise.race([
        Promise.all(pendingCaptures),
        new Promise(function (r) { setTimeout(r, ms) }),
      ])
    }
    const best = candidates[0] || null
    return best ? JSON.parse(JSON.stringify(best)) : null
  }

  function stripTransientSign(rawUrl) {
    try {
      const base = (typeof location !== 'undefined' && location && location.href) || 'https://localhost/'
      const u = new URL(rawUrl, base)
      let changed = false
      for (let i = 0; i < TRANSIENT_SIGN_PARAMS.length; i++) {
        if (u.searchParams.has(TRANSIENT_SIGN_PARAMS[i])) {
          u.searchParams.delete(TRANSIENT_SIGN_PARAMS[i])
          changed = true
        }
      }
      if (!changed) return ''
      return /^https?:\/\//i.test(rawUrl) ? u.toString() : (u.pathname + u.search + u.hash)
    } catch (e) {
      return ''
    }
  }

  function renderURL(tpl, id, stripSign) {
    const s = String(id || '')
    const enc = encodeURIComponent(s)
    let u = replaceAllLiteral(tpl.urlTemplate, ID_ENC_TOKEN, enc)
    u = replaceAllLiteral(u, ID_TOKEN, enc)
    if (stripSign) {
      const stripped = stripTransientSign(u)
      if (stripped) u = stripped
    }
    return u
  }

  function renderBody(tpl, id) {
    if (!tpl.bodyTemplate) return undefined
    const s = String(id || '')
    const enc = encodeURIComponent(s)
    let rawRepl = s
    if (tpl.bodyKind === 'json') rawRepl = JSON.stringify(s).slice(1, -1)
    else if (tpl.bodyKind === 'form') rawRepl = enc
    let b = replaceAllLiteral(tpl.bodyTemplate, ID_ENC_TOKEN, enc)
    b = replaceAllLiteral(b, ID_TOKEN, rawRepl)
    return b
  }

  function sendViaFetch(url, method, headers, body) {
    const fn = pageFetch || (typeof window !== 'undefined' && window.fetch) || (typeof fetch === 'function' ? fetch : null)
    if (!fn) return Promise.reject(new Error('fetch 不可用'))
    const opts = { method: method, headers: headers, credentials: 'include' }
    if (body !== undefined && method !== 'GET' && method !== 'HEAD') opts.body = body
    replayingDepth++
    let p
    try {
      p = fn(url, opts)
    } catch (e) {
      replayingDepth--
      return Promise.reject(e)
    }
    return Promise.resolve(p).then(async function (res) {
      replayingDepth--
      let text = ''
      try { text = await res.text() } catch (e) {}
      return { status: res.status, text: text }
    }, function (err) {
      replayingDepth--
      throw err
    })
  }

  function sendViaXHR(url, method, headers, body) {
    if (typeof XMLHttpRequest === 'undefined') return sendViaFetch(url, method, headers, body)
    return new Promise(function (resolve, reject) {
      replayingDepth++
      let finished = false
      const finish = function () {
        if (!finished) { finished = true; replayingDepth-- }
      }
      try {
        const xhr = new XMLHttpRequest()
        xhr.open(method, url, true)
        xhr.withCredentials = true
        const keys = Object.keys(headers || {})
        for (let i = 0; i < keys.length; i++) {
          try { xhr.setRequestHeader(keys[i], headers[keys[i]]) } catch (e) {}
        }
        xhr.onload = function () {
          finish()
          resolve({ status: xhr.status, text: typeof xhr.responseText === 'string' ? xhr.responseText : '' })
        }
        xhr.onerror = function () {
          finish()
          reject(new Error('网络请求失败'))
        }
        xhr.ontimeout = function () {
          finish()
          reject(new Error('请求超时'))
        }
        xhr.timeout = 12000
        xhr.send(body !== undefined ? body : null)
      } catch (e) {
        finish()
        reject(e)
      }
    })
  }

  async function replayOne(tpl, id, stripSign) {
    if (!tpl || !tpl.urlTemplate) return { ok: false, error: '缺少删除请求模板' }
    const url = renderURL(tpl, id, !!stripSign)
    const body = renderBody(tpl, id)
    const headers = Object.assign({}, tpl.headers || {})
    try {
      const resp = tpl.transport === 'xhr'
        ? await sendViaXHR(url, tpl.method, headers, body)
        : await sendViaFetch(url, tpl.method, headers, body)
      const verdict = responseLooksOK(resp.status, resp.text, tpl.successSig)
      if (!verdict.ok) {
        // 首次探测失败且 URL 带单次签名参数（如 a_bogus 过期引发 403/错误码）时，剥离旧签名让站点包装器重签重试一次。
        if (!stripSign && verdict.stopped !== 'rate_limit' && verdict.stopped !== 'captcha' && stripTransientSign(url)) {
          const retry = await replayOne(tpl, id, true)
          if (retry.ok) return Object.assign({ stripSign: true }, retry)
        }
        return Object.assign({ id: String(id), status: resp.status }, verdict)
      }
      return { ok: true, id: String(id), status: resp.status }
    } catch (e) {
      return { ok: false, id: String(id), error: String((e && e.message) || e || '回放请求失败') }
    }
  }

  async function replayWave(tpl, ids, concurrency, stripSign) {
    const list = Array.isArray(ids) ? ids.map(String).filter(Boolean) : []
    const conc = Math.max(1, Math.min(5, Number(concurrency) || 3))
    const deletedIds = []
    let stopped = ''
    let error = ''
    let failedId = ''
    let cursor = 0

    async function worker() {
      while (cursor < list.length && !stopped && !error) {
        const id = list[cursor++]
        const res = await replayOne(tpl, id, stripSign)
        if (res.ok) {
          deletedIds.push(id)
        } else {
          failedId = id
          stopped = res.stopped || ''
          error = res.error || '回放删除失败'
          return
        }
      }
    }

    const workers = []
    for (let i = 0; i < Math.min(conc, list.length); i++) workers.push(worker())
    await Promise.all(workers)
    return {
      ok: !stopped && !error,
      deletedIds: deletedIds,
      failedId: failedId,
      stopped: stopped,
      error: error,
    }
  }

  const api = {
    install,
    arm,
    disarm,
    takeTemplate,
    replayOne,
    replayWave,
    _stripTransientSign: stripTransientSign,
  }
  install()
  if (typeof window !== 'undefined') window.__tulaiHistoryFast = api
  if (typeof module !== 'undefined' && module.exports) module.exports = api
})()
