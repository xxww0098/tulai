// 额度 / 限流提示。只看短的提示条和对话框，不扫整页历史。
// 正则与 internal/strategy/js/composer.js、internal/tasks/fault.go 保持一致。
;(function () {
  const QUOTA = /额度不足|额度已用|次数已用完|次数已用尽|免费次数已|已达上限|超出.{0,8}限额|生成次数已|quota exceeded|daily limit/i
  const RATE = /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests/i

  function textOf(el) {
    return String((el && (el.innerText || el.textContent)) || '').replace(/\s+/g, ' ').trim()
  }

  function visible(el) {
    if (!el || el.nodeType !== 1) return false
    if (el.getAttribute && el.getAttribute('hidden') != null) return false
    if (typeof getComputedStyle === 'function') {
      const style = getComputedStyle(el)
      if (style && (style.display === 'none' || style.visibility === 'hidden')) return false
    }
    return true
  }

  function accountBlock(doc) {
    if (!doc || typeof doc.querySelectorAll !== 'function') return null
    const nodes = doc.querySelectorAll('[role="alert"],[role="dialog"],[class*="toast"],[class*="notice"],[class*="quota"],[class*="limit"]')
    for (let i = 0; i < nodes.length; i++) {
      const el = nodes[i]
      if (!visible(el)) continue
      const text = textOf(el)
      if (!text || text.length > 240) continue
      if (RATE.test(text)) return { kind: 'rate', message: text }
      if (QUOTA.test(text)) return { kind: 'quota', message: text }
    }
    return null
  }

  globalThis.XBSQuota = { accountBlock }
})()
