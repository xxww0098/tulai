const temuTabs = new Map()
browser.tabs.onRemoved.addListener(id => temuTabs.delete(id))

browser.runtime.onMessage.addListener(async (message, sender) => {
  if (!message || !['DOWNLOAD_PAGE', 'TEMU_CACHE_LOAD', 'TEMU_CACHE_SAVE'].includes(message.type)) return undefined
  if (!sender.tab || sender.frameId !== 0) return { ok: false, message: '请在网页中点击下载按钮' }
  try {
    const tab = await browser.tabs.get(sender.tab.id)
    const url = new URL(tab.url)
    if (message.type !== 'DOWNLOAD_PAGE') {
      if (tab.url !== message.url || url.protocol !== 'https:' || !['temu.com', 'www.temu.com'].includes(url.hostname) ||
          !/^\/(?:[a-z]{2}(?:-[a-z]{2})?\/)?search_result\.html$/i.test(url.pathname) ||
          typeof message.key !== 'string' || message.key.length > 8192) return { ok: false }
      if (message.type === 'TEMU_CACHE_LOAD') {
        const cached = temuTabs.get(tab.id)
        return { ok: true, rows: cached && cached.key === message.key ? cached.rows : [], truncated: !!(cached && cached.key === message.key && cached.truncated) }
      }
      if (!Array.isArray(message.rows) || message.rows.length > 10000 || new TextEncoder().encode(JSON.stringify(message.rows)).length > 8 * 1024 * 1024) return { ok: false }
      temuTabs.set(tab.id, { key: message.key, rows: message.rows, truncated: !!message.truncated })
      return { ok: true }
    }
    if (!/^[0-9a-f-]{36}$/i.test(message.marker || '')) return { ok: false, message: '请在网页中点击下载按钮' }
    if (!tab.active || !['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
      return { ok: false, message: '请在当前网页重试' }
    }
    const response = await fetch(TULAI_DOWNLOAD.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Tulai-Token': TULAI_DOWNLOAD.token },
      body: JSON.stringify({ url: tab.url, marker: message.marker }),
    })
    return await response.json()
  } catch (_) {
    return { ok: false, message: '无法连接 tulai，请确认应用正在运行' }
  }
})
