// tulai 千问国际（chat.qwen.ai）聊天历史记录删除页面脚本：优先同源 /api/v2/chats/ 接口，支持单条删除与分波清理。
(function () {
  if (typeof window !== 'undefined' && window.__tulaiQwenIntlHistory) return

  const RATE_LIMIT_RE = /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests|operation is too frequent|chat limited|global limited/i
  const CAPTCHA_RE = /滑动.*验证|向右滑动|安全验证|请完成验证|人机验证|verify you are human|security verification|slide to verify/i
  const NON_CHAT_SLUGS = /^(new-chat|new-branch|guest|local)$/i

  function normalize(s) {
    return String(s || '').replace(/\uFEFF/g, '').replace(/\s+/g, ' ').trim()
  }

  function all(root, sel) {
    if (!root || !root.querySelectorAll) return []
    try { return Array.from(root.querySelectorAll(sel)) } catch (e) { return [] }
  }

  function styleOf(el) {
    try { return getComputedStyle(el) } catch (e) { return { display: '', visibility: '', opacity: '1' } }
  }

  function visible(el) {
    if (!el || el.isConnected === false) return false
    const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 }
    if (r.width <= 4 || r.height <= 4) return false
    const st = styleOf(el)
    return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity || '1') > 0.05
  }

  function conversationIdFromHref(href) {
    try {
      const u = new URL(String(href || ''), (typeof location !== 'undefined' && location && location.href) || 'https://chat.qwen.ai/')
      const parts = u.pathname.split('/').filter(Boolean)
      const c = parts.indexOf('c')
      if (c >= 0 && parts[c + 1] && !NON_CHAT_SLUGS.test(parts[c + 1])) return parts[c + 1]
      const i = parts.indexOf('chat')
      if (i >= 0 && parts[i + 1]) return parts[i + 1]
      return ''
    } catch (e) {
      return ''
    }
  }

  function sidebarItems(doc) {
    const links = all(doc, 'a.chat-item-drag-link[href],a[aria-label="chat-item"][href],a[href*="/c/"]')
    const seen = new Set()
    const items = []
    for (let i = 0; i < links.length; i++) {
      const el = links[i]
      const id = conversationIdFromHref(el.getAttribute ? el.getAttribute('href') : el.href)
      if (!id || seen.has(id)) continue
      seen.add(id)
      items.push({ id, title: normalize(el.innerText || el.textContent || '').slice(0, 80) })
    }
    return items
  }

  function hasVisibleLoginWall(doc) {
    if (sidebarItems(doc).length > 0) return false
    if (all(doc, '.user-menu-btn,.sidebar-user .user-profile-desktop,.user-img').some(visible)) return false
    if (all(doc, '.header-right-auth-button').some(visible)) return true
    return all(doc, 'button,[role="button"],a').some(function (el) {
      if (!visible(el)) return false
      return /^(Log in|Sign in|登录|立即登录|登录\/注册)$/i.test(normalize(el.innerText || el.textContent))
    })
  }

  function checkBlocked(doc) {
    if (hasVisibleLoginWall(doc)) return { stopped: 'not_logged_in', error: '账号未登录' }
    const captchaNodes = all(doc, '#nc_wrapper,#baxia-dialog-content,.baxia-dialog,iframe[src*="captcha"],iframe[src*="baxia"],[class*="captcha"],[class*="baxia"]')
    if (captchaNodes.some(function (el) {
      if (el.tagName === 'IFRAME') return visible(el)
      return visible(el) && (el.id === 'nc_wrapper' || el.id === 'baxia-dialog-content' || CAPTCHA_RE.test(normalize(el.innerText || el.textContent)))
    })) {
      return { stopped: 'captcha', error: '触发安全验证' }
    }
    const rate = all(doc, '[role="alert"],[role="dialog"],[class*="toast"],[class*="notice"],.ant-message-notice').some(function (el) {
      if (!visible(el)) return false
      const t = normalize(el.innerText || el.textContent)
      return t.length > 0 && t.length <= 240 && RATE_LIMIT_RE.test(t)
    })
    if (rate) return { stopped: 'rate_limit', error: '触发操作频率限制' }
    return null
  }

  function authHeaders(extra) {
    const headers = Object.assign({ Accept: 'application/json' }, extra || {})
    try {
      if (typeof localStorage !== 'undefined' && localStorage) {
        const token = String(localStorage.getItem('token') || '').trim()
        if (token) headers.Authorization = 'Bearer ' + token
      }
    } catch (e) {}
    return headers
  }

  function classifyPayload(status, text) {
    if (status === 401 || status === 403) {
      return { stopped: 'not_logged_in', error: '账号未登录' }
    }
    if (status === 429) {
      return { stopped: 'rate_limit', error: '触发操作频率限制' }
    }
    const body = String(text || '')
    if (/rgv587_flag|baxia|captcha|security verification/i.test(body)) {
      return { stopped: 'captcha', error: '触发安全验证' }
    }
    if (RATE_LIMIT_RE.test(body)) {
      return { stopped: 'rate_limit', error: '触发操作频率限制' }
    }
    return null
  }

  function parseChatList(json) {
    let arr = null
    if (Array.isArray(json)) arr = json
    else if (json && Array.isArray(json.data)) arr = json.data
    else if (json && json.data && Array.isArray(json.data.list)) arr = json.data.list
    else if (json && json.data && Array.isArray(json.data.chats)) arr = json.data.chats
    else if (json && Array.isArray(json.chats)) arr = json.chats
    if (!arr) return null
    const out = []
    for (let i = 0; i < arr.length; i++) {
      const row = arr[i]
      if (!row || typeof row !== 'object') continue
      const id = normalize(row.id || row.chat_id || row.chatId || '')
      if (!id) continue
      out.push({ id, title: normalize(row.title || '').slice(0, 80) })
    }
    return out
  }

  async function apiFetch(url, init) {
    const fn = typeof fetch === 'function' ? fetch : (typeof window !== 'undefined' && window.fetch)
    if (typeof fn !== 'function') throw new Error('fetch unavailable')
    const resp = await fn(url, Object.assign({ credentials: 'include' }, init || {}))
    const text = await resp.text().catch(function () { return '' })
    let json = null
    try { json = text ? JSON.parse(text) : null } catch (e) {}
    return { ok: resp.ok, status: resp.status, text, json }
  }

  async function listChatsPage(page) {
    const p = Math.max(1, Number(page) || 1)
    const urls = [
      '/api/v2/chats/?page=' + p + '&exclude_project=true',
      '/api/v1/chats/?page=' + p,
    ]
    let lastErr = '获取千问国际会话列表失败'
    for (let i = 0; i < urls.length; i++) {
      try {
        const res = await apiFetch(urls[i], { method: 'GET', headers: authHeaders() })
        const blk = classifyPayload(res.status, res.text)
        if (blk) return Object.assign({ ok: false, items: [] }, blk)
        if (res.status === 404 && i < urls.length - 1) continue
        if (!res.ok) {
          lastErr = 'HTTP ' + res.status
          continue
        }
        const items = parseChatList(res.json)
        if (items !== null) return { ok: true, items }
        lastErr = '会话列表格式异常'
      } catch (e) {
        lastErr = String((e && e.message) || e || lastErr)
      }
    }
    return { ok: false, items: [], error: lastErr }
  }

  async function apiDeleteOne(id) {
    const safeId = encodeURIComponent(id)
    const urls = [
      '/api/v2/chats/' + safeId,
      '/api/v1/chats/' + safeId,
    ]
    let lastErr = '删除千问国际对话失败'
    for (let i = 0; i < urls.length; i++) {
      try {
        const res = await apiFetch(urls[i], { method: 'DELETE', headers: authHeaders() })
        const blk = classifyPayload(res.status, res.text)
        if (blk) return Object.assign({ ok: false }, blk)
        if (res.status === 404 && i < urls.length - 1) continue
        if (res.ok) {
          if (res.json && res.json.success === false) {
            lastErr = normalize(res.json.message || res.json.error || res.json.detail || lastErr)
            continue
          }
          return { ok: true, id }
        }
        lastErr = 'HTTP ' + res.status
      } catch (e) {
        lastErr = String((e && e.message) || e || lastErr)
      }
    }
    return { ok: false, id, error: lastErr }
  }

  async function deleteConversation(doc, convID, cancelled) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false }, blk)
    if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
    const targetID = normalize(convID) || conversationIdFromHref(typeof location !== 'undefined' && location ? location.href : '')
    if (!targetID) return { ok: false, notFound: true, error: '当前页面没有可删除的对话 ID' }
    return await apiDeleteOne(targetID)
  }

  async function collectHistoryIds(doc, skipIds, keepRecent, maxCount) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false, ids: [], keepIds: [], titles: {} }, blk)
    const skip = new Set(Array.isArray(skipIds) ? skipIds.map(String) : [])
    const keep = Math.max(0, Number(keepRecent) || 0)
    const limit = Math.max(1, Number(maxCount) || 200)
    const ordered = []
    const seen = new Set()
    const titles = {}
    let apiWorked = false

    for (let page = 1; page <= 20 && ordered.length < keep + limit; page++) {
      const res = await listChatsPage(page)
      if (res.stopped) return Object.assign({ ok: false, ids: [], keepIds: [], titles: {} }, res)
      if (!res.ok) {
        if (page === 1) break
        break
      }
      apiWorked = true
      if (!res.items.length) break
      let added = 0
      for (let i = 0; i < res.items.length; i++) {
        const it = res.items[i]
        if (!seen.has(it.id)) {
          seen.add(it.id)
          ordered.push(it.id)
          titles[it.id] = it.title || ''
          added++
        }
      }
      if (added === 0) break
    }

    if (!apiWorked) {
      const domItems = sidebarItems(doc)
      for (let i = 0; i < domItems.length; i++) {
        const it = domItems[i]
        if (!seen.has(it.id)) {
          seen.add(it.id)
          ordered.push(it.id)
          titles[it.id] = it.title || ''
        }
      }
    }

    const keepIds = ordered.slice(0, keep)
    const keepSet = new Set(keepIds)
    const ids = ordered.filter(function (id) { return !keepSet.has(id) && !skip.has(id) }).slice(0, limit)
    return { ok: true, ids, keepIds, titles, totalSeen: ordered.length }
  }

  async function deleteNextHistoryItem(doc, skipIds, keepRecent, cancelled) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false }, blk)
    const col = await collectHistoryIds(doc, skipIds, keepRecent, 1)
    if (!col.ok) {
      return { ok: false, stopped: col.stopped || '', error: col.error || '读取千问国际历史列表失败' }
    }
    if (!col.ids.length) {
      return { ok: true, done: true, remaining: col.totalSeen }
    }
    const targetID = col.ids[0]
    const title = (col.titles && col.titles[targetID]) || ''
    const res = await deleteConversation(doc, targetID, cancelled)
    if (!res.ok) {
      return {
        ok: false,
        id: targetID,
        title,
        remaining: col.totalSeen,
        stopped: res.stopped || '',
        error: res.error || '删除失败',
      }
    }
    return {
      ok: true,
      deleted: true,
      id: targetID,
      title,
      remaining: Math.max(0, col.totalSeen - 1),
    }
  }

  async function deleteWave(doc, ids, concurrency) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false, deletedIds: [] }, blk)
    const list = Array.isArray(ids) ? ids.map(normalize).filter(Boolean) : []
    if (!list.length) return { ok: true, deletedIds: [] }
    const workers = Math.max(1, Math.min(Number(concurrency) || 3, list.length))
    const deletedIds = []
    let cursor = 0
    let failed = null

    async function runWorker() {
      while (!failed && cursor < list.length) {
        const id = list[cursor++]
        const res = await apiDeleteOne(id)
        if (res.ok) {
          deletedIds.push(id)
        } else {
          failed = {
            ok: false,
            deletedIds,
            failedId: id,
            stopped: res.stopped || '',
            error: res.error || '删除失败',
          }
          return
        }
      }
    }

    await Promise.all(Array.from({ length: workers }, runWorker))
    if (failed) return failed
    return { ok: true, deletedIds }
  }

  const api = {
    conversationIdFromHref,
    sidebarItems,
    checkBlocked,
    listChatsPage,
    deleteConversation,
    deleteNextHistoryItem,
    collectHistoryIds,
    deleteWave,
  }
  if (typeof window !== 'undefined') window.__tulaiQwenIntlHistory = api
  if (typeof module !== 'undefined' && module.exports) module.exports = api
})()
