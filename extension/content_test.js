// 内容脚本的等待循环：千问先滚再取样，豆包没有 revealResults 时取样逻辑不变。
const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

const GRAY = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'

function load(page) {
  const listeners = []
  const context = vm.createContext({
    console,
    setTimeout,
    clearTimeout,
    URL,
    Uint8Array,
    atob,
    btoa,
    location: { href: 'https://www.example.test/chat', hostname: 'www.example.test' },
    document: {},
    chrome: {
      runtime: {
        onMessage: { addListener(fn) { listeners.push(fn) } },
        sendMessage() { return { catch() {} } },
      },
    },
    XBSPage: page,
  })
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'protocol.js'), 'utf8'), context)
  vm.runInContext(fs.readFileSync(path.join(__dirname, 'content.js'), 'utf8'), context)
  assert.strictEqual(listeners.length, 1)
  return function command(payload) {
    const env = { v: 1, id: 't-' + payload.name, type: 'command', payload }
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('hung ' + payload.name)), 8000)
      listeners[0]({ type: 'COMMAND_FROM_HOST', envelope: env }, {}, (resp) => {
        clearTimeout(timer)
        resolve(resp)
      })
    })
  }
}

function composerPage(extra) {
  const box = { value: '水彩风格' }
  const button = { click() { box.value = '' }, dispatchEvent() {} }
  return Object.assign({
    normalize(text) { return String(text || '').replace(/\s+/g, ' ').trim() },
    composer() { return box },
    readComposer() { return box.value },
    sendButton() { return button },
    loginWall() { return false },
    mark() { return { id: 'mark' } },
    sendAccepted() { return box.value === '' },
    detectCaptcha() { return { blocked: false } },
  }, extra)
}

async function sendThenWait(page) {
  const call = load(page)
  const sent = await call({ name: 'send', prompt: '水彩风格', timeoutMs: 2000 })
  assert.strictEqual(sent.payload.ok, true, JSON.stringify(sent.payload))
  const started = Date.now()
  const waited = await call({ name: 'waitResult', timeoutMs: 3000 })
  return { waited, elapsed: Date.now() - started }
}

async function main() {
  const qianwen = composerPage({
    reveals: 0,
    ready: false,
    activate(el) { el.click(); return true },
    revealResults() { this.reveals++; this.ready = true },
    resultImages() {
      const gray = [{ url: GRAY }]
      if (!this.ready) return gray
      return gray.concat([
        { url: 'https://cdn.example/watercolor-a.png' },
        { url: 'https://cdn.example/watercolor-b.png' },
      ])
    },
    resultsBusy() { return !this.ready },
    isPlaceholderURL(url) { return String(url || '').startsWith('data:') || String(url || '').startsWith('blob:') },
  })
  const qianwenResult = await sendThenWait(qianwen)
  assert.ok(qianwen.reveals >= 1, '等待循环要先把千问生成卡滚进视口')
  assert.strictEqual(qianwenResult.waited.payload.ok, true, JSON.stringify(qianwenResult.waited.payload))
  assert.deepStrictEqual(qianwenResult.waited.payload.images.map((item) => item.url), [
    'https://cdn.example/watercolor-a.png',
    'https://cdn.example/watercolor-b.png',
  ])
  assert.ok(qianwenResult.elapsed >= 350, '两张实图要连续两次一样才返回，用时 ' + qianwenResult.elapsed)

  const doubao = composerPage({
    resultImages() {
      return [
        { url: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" width="1536" height="1536"/>' },
        { url: 'https://cdn.example/full.png' },
      ]
    },
    resultsBusy() { return false },
    isPlaceholderURL(url) { return /^data:image\/svg/i.test(String(url || '')) || /^blob:/i.test(String(url || '')) },
  })
  assert.strictEqual(typeof doubao.revealResults, 'undefined')
  const doubaoResult = await sendThenWait(doubao)
  assert.strictEqual(doubaoResult.waited.payload.ok, true, JSON.stringify(doubaoResult.waited.payload))
  assert.deepStrictEqual(doubaoResult.waited.payload.images.map((item) => item.url), ['https://cdn.example/full.png'])
  assert.ok(doubaoResult.elapsed >= 350, '豆包仍要两次稳定取样，用时 ' + doubaoResult.elapsed)

  const blocked = composerPage({
    detectCaptcha() { return { blocked: true, kind: 'slider', baxia: true, root: {} } },
    captchaCrop() { return '' },
  })
  const captchaCall = load(blocked)
  const captchaSent = await captchaCall({ name: 'send', prompt: '水彩风格', timeoutMs: 2000 })
  assert.strictEqual(captchaSent.payload.ok, false)
  assert.strictEqual(captchaSent.payload.code, 'E_CAPTCHA_HUMAN')
  assert.strictEqual(captchaSent.payload.kind, 'slider')
  assert.strictEqual(captchaSent.payload.baxia, true)
  assert.strictEqual(captchaSent.payload.captchaBlocked, true)
  console.log('content wait ok')
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
