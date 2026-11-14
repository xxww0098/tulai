const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

globalThis.getComputedStyle = () => ({ display: 'block', visibility: 'visible' })
vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'quota.js'), 'utf8'))

function el(role, text, className) {
  return {
    nodeType: 1,
    attrs: { role: role || '', class: className || '' },
    className: className || '',
    innerText: text,
    textContent: text,
    getAttribute(name) {
      if (name === 'role') return role || null
      if (name === 'class') return className || null
      return null
    },
  }
}

function doc(nodes) {
  return {
    querySelectorAll() {
      return nodes.filter((node) => {
        const role = node.getAttribute && node.getAttribute('role')
        const className = node.className || ''
        return role === 'alert' || role === 'dialog' || /toast|notice|quota|limit/.test(className)
      })
    },
  }
}

const quota = TULAIQuota.accountBlock(doc([el('alert', '今日免费次数已用完，请明天再来')]))
assert.strictEqual(quota.kind, 'quota')
assert.match(quota.message, /免费次数已用完/)

const rate = TULAIQuota.accountBlock(doc([el('status', '请求过于频繁，请稍后再试', 'toast')]))
assert.strictEqual(rate.kind, 'rate')

const history = TULAIQuota.accountBlock(doc([
  { nodeType: 1, innerText: '刚才那条回复里写了额度不足，但这不是提示条', textContent: 'x', getAttribute() { return null }, className: 'message' },
]))
assert.strictEqual(history, null)

const hidden = TULAIQuota.accountBlock(doc([
  Object.assign(el('alert', '额度不足'), { getAttribute(name) { return name === 'hidden' ? '' : (name === 'role' ? 'alert' : null) } }),
]))
assert.strictEqual(hidden, null)

assert.strictEqual(TULAIQuota.imageReplyBlock('今日图片生成免费额度已用完。开通订阅畅享更多额度。').kind, 'quota')
assert.strictEqual(TULAIQuota.imageReplyBlock('为什么提示今日图片生成免费额度已用完？'), null)
assert.strictEqual(TULAIQuota.imageReplyBlock('今日图片生成免费额度已用完了怎么办？'), null)

assert.strictEqual(TULAIQuota.imageReplyBlock('近 7 天额度已用完，预计明天恢复。升级到标准套餐畅享更多额度。').kind, 'quota')
assert.strictEqual(TULAIQuota.imageReplyBlock('近 7 天额度已用完，预计明天恢复。升级到标准套餐畅享更多额度。 升级正式订阅 ->').kind, 'quota')
assert.strictEqual(TULAIQuota.imageReplyBlock('近7天图片生成额度已用完，预计明天恢复。').kind, 'quota')
assert.strictEqual(TULAIQuota.imageReplyBlock('为什么提示近 7 天额度已用完？'), null)
assert.strictEqual(TULAIQuota.imageReplyBlock('近 7 天额度已用完了怎么办？'), null)
assert.strictEqual(TULAIQuota.accountBlock(doc([el('alert', '近 7 天额度用完，预计明天恢复')])).kind, 'quota')

console.log('quota_test ok')
