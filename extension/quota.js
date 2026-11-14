// 额度 / 限流提示。只看短的提示条和对话框，不扫整页历史。
// 正则与 internal/strategy/js/composer.js、internal/tasks/fault.go 保持一致。
;(function () {
  const QUOTA = /额度不足|额度已用尽|额度已用|额度用尽|额度用完|次数已用完|次数已用尽|次数用完|免费次数已|已达上限|超出.{0,8}限额|生成次数已|(?:今日|今天|当日|本周|本月|(?:最?近\s*)?\d+\s*天).{0,12}(?:额度|次数).{0,8}(?:用完|用尽|耗尽|达上限|已达上限)|quota exceeded|daily limit/i
  const REPLY_QUOTA = /^(?:(?:很)?抱歉[，,：:\s]*)?(?:(?:您|你)的?\s*)?(?:今日|今天|当日|本周|本月|(?:最?近\s*)?\d+\s*天)?\s*(?:图片生成|图像生成|生图|绘图)?\s*(?:免费)?\s*(?:生成)?(?:额度|次数)(?:已)?(?:用完|用尽|耗尽|达上限|已达上限)(?:[，,。.!！;；\s]|$)/
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

  function imageReplyBlock(text) {
    const lines = String(text || '').split('\n')
    const message = lines.map(function (line) { return line.replace(/\s+/g, ' ').trim() }).find(function (line) {
      return REPLY_QUOTA.test(line) && !/[?？]/.test(line.split(/[。.!！]/)[0])
    })
    return message ? { kind: 'quota', message } : null
  }

  globalThis.TULAIQuota = { accountBlock, imageReplyBlock }
})()
