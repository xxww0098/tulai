// tulai Gemini（gemini.google.com）聊天历史记录删除页面脚本：通过侧边栏对话条目菜单与确认弹层删除单条或批量清理。
(function () {
  if (typeof window !== 'undefined' && window.__tulaiGeminiHistory) return

  const RATE_LIMIT_RE = /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests|sending too many requests/i
  const CAPTCHA_RE = /滑动.*验证|安全验证|请完成验证|人机验证|verify you are human|unusual traffic|security verification/i
  const NON_CHAT_SLUGS = /^(new|images|download|gems|settings|mystuff)$/i

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

  function sleep(ms) {
    return new Promise(function (resolve) { setTimeout(resolve, ms) })
  }

  async function hoverEl(el) {
    if (!el || el.isConnected === false) return false
    if (typeof window !== 'undefined' && typeof window.tulaiHover === 'function') {
      try { if (await window.tulaiHover(el)) return true } catch (e) {}
    }
    try {
      const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 }
      const opts = { bubbles: true, cancelable: true, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 }
      el.dispatchEvent(new MouseEvent('mouseenter', opts))
      el.dispatchEvent(new MouseEvent('mouseover', opts))
      el.dispatchEvent(new MouseEvent('mousemove', opts))
      return true
    } catch (e) { return false }
  }

  async function press(el) {
    if (!el || el.isConnected === false) return false
    if (typeof window !== 'undefined' && typeof window.tulaiClick === 'function') {
      try { if (await window.tulaiClick(el)) return true } catch (e) {}
    }
    if (el.isConnected === false) return false
    try {
      if (typeof el.focus === 'function') el.focus()
      const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 }
      const opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 }
      if (typeof PointerEvent === 'function') {
        el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)))
      }
      el.dispatchEvent(new MouseEvent('mousedown', opts))
      if (typeof PointerEvent === 'function') {
        el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)))
      }
      el.dispatchEvent(new MouseEvent('mouseup', opts))
      el.click()
      return true
    } catch (e) { return false }
  }

  function conversationIdFromHref(href) {
    try {
      const u = new URL(String(href || ''), (typeof location !== 'undefined' && location && location.href) || 'https://gemini.google.com/app')
      const parts = u.pathname.split('/').filter(Boolean)
      const a = parts.indexOf('app')
      if (a >= 0 && parts[a + 1] && !NON_CHAT_SLUGS.test(parts[a + 1])) return parts[a + 1]
      return ''
    } catch (e) {
      return ''
    }
  }

  function conversationIdFromNode(node) {
    if (!node) return ''
    const link = (node.tagName === 'A' && node.getAttribute && node.getAttribute('href'))
      ? node
      : (node.querySelector ? node.querySelector('a[href*="/app/"]') : null)
    if (link) {
      const id = conversationIdFromHref(link.getAttribute('href') || link.href)
      if (id) return id
    }
    const hrefAttr = node.getAttribute && node.getAttribute('href')
    if (hrefAttr) {
      const id = conversationIdFromHref(hrefAttr)
      if (id) return id
    }
    const jslog = String((node.getAttribute && node.getAttribute('jslog')) || '')
    const m = jslog.match(/c_([a-f0-9]{8,})/i)
    if (m) return m[1]
    return ''
  }

  function sidebarRoot(doc) {
    return (doc && doc.querySelector && (doc.querySelector('side-navigation-v2') || doc.querySelector('bard-sidenav') || doc.querySelector('nav'))) || doc
  }

  function sidebarItems(doc) {
    const scope = sidebarRoot(doc)
    const rows = all(scope, '[data-test-id="conversation"],a[href*="/app/"]')
    const seen = new Set()
    const items = []
    for (let i = 0; i < rows.length; i++) {
      const el = rows[i]
      if (el.closest && el.closest('[data-test-id="conversation"]') && el.getAttribute && el.getAttribute('data-test-id') !== 'conversation') {
        continue
      }
      const id = conversationIdFromNode(el)
      if (!id || seen.has(id)) continue
      seen.add(id)
      const container = (el.closest && (el.closest('.conversation-items-container') || el.closest('gem-nav-list-item') || el.closest('li'))) || el.parentElement || el
      const title = normalize(el.innerText || el.textContent || '').slice(0, 80)
      items.push({ id, title, el, container })
    }
    return items
  }

  function hasVisibleLoginWall(doc) {
    if (sidebarItems(doc).length > 0) return false
    if (all(doc, '[gem-open-account-menu],a[href*="accounts.google.com/SignOutOptions"],user-profile-picture img[src]').some(visible)) return false
    if (all(doc, '[data-test-id="sidenav-error-action-link"],[data-test-id="sign-out-banner"]').some(visible)) return true
    return all(doc, 'button,[role="button"],a').some(function (el) {
      if (!visible(el)) return false
      return /^(Sign in|Log in|登录|立即登录)$/i.test(normalize(el.innerText || el.textContent))
    })
  }

  function checkBlocked(doc) {
    if (hasVisibleLoginWall(doc)) return { stopped: 'not_logged_in', error: '账号未登录' }
    const captchaNodes = all(doc, 'iframe[src*="recaptcha"],iframe[src*="captcha"],[class*="captcha"]')
    if (captchaNodes.some(function (el) {
      if (el.tagName === 'IFRAME') return visible(el)
      return visible(el) && CAPTCHA_RE.test(normalize(el.innerText || el.textContent))
    })) {
      return { stopped: 'captcha', error: '触发安全验证' }
    }
    const rate = all(doc, '[role="alert"],[role="dialog"],[class*="toast"],[class*="snackbar"],mat-snack-bar-container').some(function (el) {
      if (!visible(el)) return false
      const t = normalize(el.innerText || el.textContent)
      return t.length > 0 && t.length <= 240 && RATE_LIMIT_RE.test(t)
    })
    if (rate) return { stopped: 'rate_limit', error: '触发操作频率限制' }
    return null
  }

  async function ensureSidebarExpanded(doc) {
    if (sidebarItems(doc).length > 0) return
    const toggle = all(doc, 'chat-app-side-nav-menu-button button,button[aria-label*="Main menu" i],button[aria-label*="主菜单"],button[aria-label*="打开边栏"],button[aria-label*="Open sidebar" i],button[aria-label*="展开边栏"]').find(visible)
    if (toggle && toggle.getAttribute('aria-expanded') !== 'true') {
      await press(toggle)
      await sleep(250)
    }
  }

  function findRowActionButton(item) {
    const roots = [item.el, item.container, item.el && item.el.parentElement, item.container && item.container.parentElement].filter(Boolean)
    const sels = [
      'button[data-test-id="actions-menu-button"]',
      '[data-test-id="actions-menu-button"] button',
      '[data-test-id="actions-menu-button"]',
      '.gem-conversation-actions-menu-button button',
      'button.gem-conversation-actions-menu-button',
      'button.conversation-actions-menu-button',
      'button[aria-label*="More options" i]',
      'button[aria-label*="更多选项"]',
      'button[aria-haspopup="menu"]'
    ]
    for (let r = 0; r < roots.length; r++) {
      for (let s = 0; s < sels.length; s++) {
        const hit = all(roots[r], sels[s]).find(function (b) {
          return b && b.isConnected !== false && !b.disabled && b.getAttribute('aria-disabled') !== 'true'
        })
        if (hit) return hit
      }
    }
    return null
  }

  function findDeleteMenuItem(doc) {
    const exact = all(doc, '[data-test-id="delete-button"],button[data-test-id="delete-button"]').find(visible)
    if (exact) return exact
    const menus = all(doc, '.cdk-overlay-container [role="menu"],.mat-mdc-menu-panel,[role="menu"]').filter(visible)
    for (let i = menus.length - 1; i >= 0; i--) {
      const items = all(menus[i], 'button,[role="menuitem"]')
      const hit = items.find(function (el) {
        if (!visible(el)) return false
        const txt = normalize(el.innerText || el.textContent || el.getAttribute('aria-label'))
        const icon = el.querySelector && el.querySelector('mat-icon')
        const iconName = (icon && (icon.getAttribute('fonticon') || icon.getAttribute('data-mat-icon-name'))) || ''
        return iconName === 'delete' || /^(删除|Delete)$/i.test(txt)
      })
      if (hit) return hit
    }
    return null
  }

  function findConfirmButton(doc) {
    const exact = all(doc, '[data-test-id="confirm-button"] button,button[data-test-id="confirm-button"],[data-test-id="confirm-button"]').find(visible)
    if (exact) return exact
    const dialogs = all(doc, '[role="dialog"],mat-dialog-container,.cdk-overlay-container .mat-mdc-dialog-container').filter(visible)
    for (let i = dialogs.length - 1; i >= 0; i--) {
      const btns = all(dialogs[i], 'button,[role="button"]')
      const hit = btns.find(function (el) {
        if (!visible(el)) return false
        const txt = normalize(el.innerText || el.textContent || el.getAttribute('aria-label'))
        return /^(删除|确认删除|全部删除|确认|Delete|Confirm)$/i.test(txt)
      })
      if (hit) return hit
    }
    return null
  }

  async function deleteItemDOM(doc, item, cancelled, timeoutMs) {
    const deadline = Date.now() + (Number(timeoutMs) || 10000)
    await hoverEl(item.el)
    await sleep(60)
    let actionBtn = findRowActionButton(item)
    if (!actionBtn) {
      await hoverEl(item.container)
      await sleep(80)
      actionBtn = findRowActionButton(item)
    }
    if (!actionBtn) {
      return { ok: false, id: item.id, title: item.title, error: '未找到 Gemini 对话操作菜单按钮' }
    }
    if (!await press(actionBtn)) {
      return { ok: false, id: item.id, title: item.title, error: '无法打开 Gemini 对话操作菜单' }
    }

    let delBtn = null
    while (Date.now() < deadline && !delBtn) {
      if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
      delBtn = findDeleteMenuItem(doc)
      if (!delBtn) await sleep(30)
    }
    if (!delBtn) {
      return { ok: false, id: item.id, title: item.title, error: '未找到 Gemini 删除对话菜单项' }
    }
    if (!await press(delBtn)) {
      return { ok: false, id: item.id, title: item.title, error: '无法点击 Gemini 删除菜单项' }
    }

    let confirmBtn = null
    const confirmDeadline = Math.min(deadline, Date.now() + 2500)
    while (Date.now() < confirmDeadline && !confirmBtn) {
      if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
      confirmBtn = findConfirmButton(doc)
      if (!confirmBtn) {
        const stillThere = sidebarItems(doc).some(function (it) { return it.id === item.id })
        if (!stillThere) return { ok: true, deleted: true, id: item.id, title: item.title }
        await sleep(30)
      }
    }
    if (confirmBtn) {
      if (!await press(confirmBtn)) {
        return { ok: false, id: item.id, title: item.title, error: '无法确认删除 Gemini 对话' }
      }
    }

    while (Date.now() < deadline) {
      if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
      const blk = checkBlocked(doc)
      if (blk) return Object.assign({ ok: false, id: item.id, title: item.title }, blk)
      const stillThere = sidebarItems(doc).some(function (it) { return it.id === item.id })
      if (!stillThere) {
        return { ok: true, deleted: true, id: item.id, title: item.title }
      }
      await sleep(50)
    }
    return { ok: false, id: item.id, title: item.title, error: '等待 Gemini 对话从列表移除超时' }
  }

  async function deleteConversation(doc, convID, cancelled, timeoutMs) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false }, blk)
    if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
    await ensureSidebarExpanded(doc)
    const targetID = normalize(convID) || conversationIdFromHref(typeof location !== 'undefined' && location ? location.href : '')
    if (!targetID) return { ok: false, notFound: true, error: '当前页面没有可删除的对话 ID' }
    const items = sidebarItems(doc)
    const hit = items.find(function (it) { return it.id === targetID })
    if (!hit) return { ok: false, notFound: true, id: targetID, error: '侧边栏未找到目标对话' }
    const res = await deleteItemDOM(doc, hit, cancelled, timeoutMs)
    res.remaining = sidebarItems(doc).length
    return res
  }

  async function collectHistoryIds(doc, skipIds, keepRecent, maxCount) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false, ids: [], keepIds: [], titles: {} }, blk)
    await ensureSidebarExpanded(doc)
    const skip = new Set(Array.isArray(skipIds) ? skipIds.map(String) : [])
    const keep = Math.max(0, Number(keepRecent) || 0)
    const limit = Math.max(1, Number(maxCount) || 200)
    const items = sidebarItems(doc)
    const titles = {}
    const ordered = items.map(function (it) {
      titles[it.id] = it.title || ''
      return it.id
    })
    const keepIds = ordered.slice(0, keep)
    const keepSet = new Set(keepIds)
    const ids = ordered.filter(function (id) { return !keepSet.has(id) && !skip.has(id) }).slice(0, limit)
    return { ok: true, ids, keepIds, titles, totalSeen: ordered.length }
  }

  async function deleteNextHistoryItem(doc, skipIds, keepRecent, cancelled, timeoutMs) {
    const blk = checkBlocked(doc)
    if (blk) return Object.assign({ ok: false }, blk)
    if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
    await ensureSidebarExpanded(doc)
    const skip = new Set(Array.isArray(skipIds) ? skipIds.map(String) : [])
    const keep = Math.max(0, Number(keepRecent) || 0)
    const items = sidebarItems(doc)
    const candidates = items.slice(keep).filter(function (it) { return !skip.has(it.id) })
    if (!candidates.length) {
      return { ok: true, done: true, remaining: items.length }
    }
    const res = await deleteItemDOM(doc, candidates[0], cancelled, timeoutMs)
    res.remaining = sidebarItems(doc).length
    return res
  }

  const api = {
    conversationIdFromHref,
    sidebarItems,
    checkBlocked,
    deleteConversation,
    collectHistoryIds,
    deleteNextHistoryItem,
  }
  if (typeof window !== 'undefined') window.__tulaiGeminiHistory = api
  if (typeof module !== 'undefined' && module.exports) module.exports = api
})()
