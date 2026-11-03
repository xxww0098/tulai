// 只观察本轮发送请求；不读取请求体，也不把响应正文或凭证传给主机。
;(function () {
  const sessions = new Map()
  const pending = new Map()
  // shortcut: 超过 128 KiB 的响应不解析正文，需要长流诊断时再支持分块读取。
  const maxBody = 128 * 1024
  const quota = /额度不足|额度已用尽|额度已用完|额度用尽|次数已用完|次数已用尽|(?:今日|当日).{0,12}(?:额度|次数).{0,8}(?:用完|用尽|达到上限|已达上限)|free.{0,20}quota.{0,20}(?:exhausted|used up)/i
  const rate = /过于频繁|操作频繁|请求过于频繁|访问太频繁|rate limit|too many requests/i

  function target(platform, method, raw) {
    // 接口路径可能随平台更新；未匹配时继续依赖网页提示。
    if (method !== 'POST') return false
    try {
      const url = new URL(raw)
      if (url.protocol !== 'https:') return false
      if (platform === 'doubao') {
        return /(^|\.)doubao\.com$/.test(url.hostname) && /^(?:\/samantha)?\/chat\/completion\/?$/.test(url.pathname)
      }
      if (platform === 'qianwen') {
        return (url.hostname === 'chat2.qianwen.com' && url.pathname === '/api/v2/chat') ||
          (url.hostname === 'chat.qwen.ai' && url.pathname === '/api/v2/chat/completions') ||
          (url.hostname === 'qianwen.biz.aliyun.com' && url.pathname === '/dialog/conversation')
      }
    } catch (err) {}
    return false
  }

  function safeCode(value) {
    if (typeof value !== 'string' && typeof value !== 'number') return ''
    const code = String(value)
    // 限制文本业务码，避免将不明字段误记为凭证。
    if (/^\d{1,12}$/.test(code)) return code
    if (/^(?:OK|SUCCESS|QuotaExceeded|RateLimitExceeded|TooManyRequests|RESOURCE_EXHAUSTED|E_QUOTA|E_RATE_LIMIT)$/i.test(code)) return code
    return ''
  }

  function inspect(body, status) {
    let code = ''
    let kind = ''
    function visit(value, failed, depth) {
      if (depth > 3) return
      if (typeof value === 'string') {
        if (failed && /^[\s]*[\[{]/.test(value)) {
          try { visit(JSON.parse(value), true, depth + 1) } catch (err) {}
        }
        return
      }
      if (!value || typeof value !== 'object' || Array.isArray(value)) return
      const rawCode = value.error_code ?? value.errorCode ?? value.status_code ?? value.statusCode ?? value.code
      const candidate = safeCode(rawCode)
      const codeFailure = rawCode != null && !/^(?:0|200|OK|SUCCESS)$/i.test(String(rawCode))
      const rejected = failed || status >= 400 || Number(value.event_type) === 2005 || value.ok === false || value.success === false ||
        (value.error_code != null && String(value.error_code) !== '0') ||
        (value.errorCode != null && String(value.errorCode) !== '0') ||
        codeFailure || !!value.error
      if (candidate && (depth === 0 || rejected)) code = candidate
      if (rejected) {
        const text = [value.error_msg, value.error_message, value.message, value.msg, typeof value.error === 'string' ? value.error : ''].filter((v) => typeof v === 'string').join(' ')
        if (quota.test(text)) kind = 'quota'
        else if (!kind && rate.test(text)) kind = 'rate'
        visit(value.error, true, depth + 1)
        visit(value.event_data, true, depth + 1)
      }
    }
    try { visit(JSON.parse(body), false, 0) } catch (err) {
      for (const line of String(body).split(/\r?\n/)) {
        if (!line.startsWith('data:')) continue
        try { visit(JSON.parse(line.slice(5).trim()), false, 0) } catch (ignored) {}
      }
    }
    return { code, kind }
  }

  function armTimeout(state, ms) {
    clearTimeout(state.timer)
    state.timer = setTimeout(() => { stop(state.tabId, state.taskId) }, ms)
  }

  function serial(tabId, fn) {
    const next = (pending.get(tabId) || Promise.resolve()).catch(() => {}).then(fn)
    pending.set(tabId, next)
    return next.finally(() => { if (pending.get(tabId) === next) pending.delete(tabId) })
  }

  async function stopCurrent(tabId, taskId) {
    const state = sessions.get(tabId)
    if (!state || (taskId && state.taskId !== taskId)) return
    sessions.delete(tabId)
    clearTimeout(state.timer)
    await state.ready
    if (state.attached) {
      state.attached = false
      try { await chrome.debugger.detach({ tabId }) } catch (err) {}
    }
  }

  function stop(tabId, taskId) {
    return serial(tabId, () => stopCurrent(tabId, taskId))
  }

  function start(tabId, platform, taskId, report) {
    return serial(tabId, async () => {
      await stopCurrent(tabId)
      if (!taskId || !chrome.debugger) return false
      const state = { tabId, platform, taskId, report, armed: false, requestId: '', status: 0, attached: false, timer: null }
      sessions.set(tabId, state)
      state.ready = (async () => {
        try {
          await chrome.debugger.attach({ tabId }, '1.3')
          state.attached = true
          await chrome.debugger.sendCommand({ tabId }, 'Network.enable', {})
          return true
        } catch (err) {
          if (state.attached) {
            state.attached = false
            try { await chrome.debugger.detach({ tabId }) } catch (ignored) {}
          }
          if (sessions.get(tabId) === state) state.report({ unavailable: true })
          return false
        }
      })()
      const available = await state.ready
      if (sessions.get(tabId) === state) armTimeout(state, 30000)
      return available
    })
  }

  function mark(tabId, taskId) {
    const state = sessions.get(tabId)
    if (!state || state.taskId !== taskId || !state.attached) return
    state.armed = true
  }

  function extend(tabId, taskId, ms) {
    const state = sessions.get(tabId)
    if (!state || state.taskId !== taskId) return
    armTimeout(state, Math.min(Math.max(Number(ms) || 120000, 1000) + 10000, 900000))
  }

  if (chrome.debugger) {
    chrome.debugger.onEvent.addListener(async (source, method, params) => {
      const state = sessions.get(source.tabId)
      if (!state || !state.armed || !state.attached) return
      if (method === 'Network.requestWillBeSent') {
        if (!state.requestId && params.request && target(state.platform, params.request.method, params.request.url)) {
          state.requestId = params.requestId
        }
        return
      }
      if (!state.requestId || params.requestId !== state.requestId) return
      if (method === 'Network.responseReceived') {
        state.status = Number(params.response && params.response.status) || 0
        if (state.status >= 100 && state.status <= 599) state.report({ status: state.status })
        return
      }
      if (method === 'Network.loadingFailed') {
        state.report({ failed: true })
        return
      }
      if (method !== 'Network.loadingFinished' || Number(params.encodedDataLength) > maxBody) return
      try {
        const response = await chrome.debugger.sendCommand({ tabId: state.tabId }, 'Network.getResponseBody', { requestId: state.requestId })
        if (sessions.get(state.tabId) !== state || !response || response.base64Encoded || typeof response.body !== 'string' || response.body.length > maxBody) return
        const info = inspect(response.body, state.status)
        if (info.code || info.kind) state.report({ status: state.status, code: info.code, kind: info.kind })
      } catch (err) {}
    })
    chrome.debugger.onDetach.addListener((source) => {
      const state = sessions.get(source.tabId)
      if (!state || !state.attached) return
      state.attached = false
      state.report({ unavailable: true })
    })
  }

  globalThis.TULAINetwork = { start, stop, mark, extend }
})()
