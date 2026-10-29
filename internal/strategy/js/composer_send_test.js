// 发送确认：用户气泡或新的对话 id。输入框被清空不算。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

function el(tag, text) {
  return {
    tagName: tag.toUpperCase(),
    innerText: text || '',
    textContent: text || '',
    attrs: {},
    getAttribute(name) { return this.attrs[name] || null },
    setAttribute(name, value) { this.attrs[name] = String(value) },
  }
}

const users = []
const questions = []
const document = {
  querySelector() { return null },
  querySelectorAll(selector) {
    const sel = String(selector)
    if (sel.includes('chat-question-card-wrap')) return questions.slice()
    if (sel.includes('user')) return users.slice()
    return []
  },
}
const location = { href: 'https://www.doubao.com/chat/', hostname: 'www.doubao.com' }
const context = vm.createContext({
  window: {},
  document,
  location,
  URL,
  NodeFilter: { SHOW_TEXT: 4 },
  getComputedStyle() { return { visibility: 'visible', display: 'block' } },
})
context.window = context
vm.runInContext(fs.readFileSync(path.join(__dirname, 'composer.js'), 'utf8'), context)

const accepted = (before, prompt) => context.tulaiSendAccepted(before, prompt)

assert.strictEqual(accepted('https://www.doubao.com/chat/', '雪山下的小木屋，油画风格'), false, '输入框空了但没有气泡、地址也没变，不能算发出去')
location.href = 'https://www.doubao.com/chat/'
assert.strictEqual(accepted('https://www.doubao.com/chat/old', '雪山下的小木屋，油画风格'), false, '从旧对话回到没有 id 的地址不算新对话已接受')

const bubble = el('div', '雪山下的小木屋，油画风格')
bubble.attrs['data-message-author-role'] = 'user'
users.push(bubble)
assert.strictEqual(accepted('https://www.doubao.com/chat/', '雪山下的小木屋，油画风格'), true)

users.length = 0
location.href = 'https://www.doubao.com/chat/1847291'
assert.strictEqual(accepted('https://www.doubao.com/chat/', '雪山下的小木屋，油画风格'), true, '地址出现新的对话 id 就是发送成功')
assert.strictEqual(accepted('https://www.doubao.com/chat/1847291', '雪山下的小木屋，油画风格'), false, '还停在同一个对话 id 上不算')

users.length = 0
questions.length = 0
location.hostname = 'www.qianwen.com'
context.__tulaiPageHost = 'www.qianwen.com'
location.href = 'https://www.qianwen.com/chat/78241e13b27e48a2868f7133437ca28f'
const previous = el('div', '画一只橘猫坐在窗台上，水彩风格')
previous.attrs.class = 'chat-question-card-wrap'
questions.push(previous)
assert.strictEqual(accepted('https://www.qianwen.com/', '雪山下的小木屋，油画风格'), false, '地址写回上一轮对话、气泡里没有这句提示词，不能算发出去')
questions.push(el('div', '雪山下的小木屋，油画风格'))
assert.strictEqual(accepted('https://www.qianwen.com/', '雪山下的小木屋，油画风格'), true, '新的用户气泡出现才算这句发出去了')
questions.length = 0
location.href = 'https://www.qianwen.com/chat/new-chat'
assert.strictEqual(accepted('https://www.qianwen.com/', '雪山下的小木屋，油画风格'), true, '空白页地址变成新对话才算发出去')

const src = fs.readFileSync(path.join(__dirname, 'composer.js'), 'utf8')
assert.ok(!src.includes("if (!String(text || '').trim()) return JSON.stringify({ sent: true })"), '不能再靠输入框变空确认发送')

console.log('composer send ok')
