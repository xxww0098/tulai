const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')
const test = require('node:test')

const context = vm.createContext({ globalThis: {} })
context.globalThis = context
vm.runInContext(fs.readFileSync(path.join(__dirname, 'bridge_logic.js'), 'utf8'), context)
const bridge = context.TULAIBridge

assert.strictEqual(bridge.nextRetryMs(1000), 2000)
assert.strictEqual(bridge.nextRetryMs(8000), 15000)
assert.strictEqual(bridge.nextRetryMs(15000), 15000)
assert.strictEqual(bridge.pingTimedOut(0, false, 100000, 15000), false)
assert.strictEqual(bridge.pingTimedOut(0, true, 30001, 15000), true)
assert.strictEqual(bridge.pingTimedOut(1000, true, 20000, 15000), false)
assert.ok(bridge.refusedMessage(18765).includes('18765'))
assert.ok(bridge.refusedMessage(18765).includes('tulai'))
assert.strictEqual(bridge.authMessage('令牌不一致'), '令牌不一致')
assert.ok(bridge.authMessage('').includes('令牌'))
console.log('bridge_test ok')

test('download and release stay on the result tab after the user switches tabs', async () => {
  const other = { id: 23, url: 'https://www.doubao.com/chat/other' }
  const result = { id: 17, url: 'https://www.doubao.com/chat/result' }
  const open = new Map([[other.id, other], [result.id, result]])
  const delivered = [], focused = [], replies = []
  const listener = { addListener() {} }
  Object.assign(context, {
    URL, WebSocket: { OPEN: 1 },
    chrome: {
      runtime: { onMessage: listener, onInstalled: listener, onStartup: listener },
      alarms: { onAlarm: listener, create() {} },
      storage: { onChanged: listener, local: { get: () => new Promise(() => {}) } },
      tabs: {
        onRemoved: listener, onUpdated: listener,
        async get(id) { return open.get(id) },
        async query() { return [other] },
        async update(id) { focused.push(id) },
        async sendMessage(id) { delivered.push(id); return { payload: { ok: true } } },
      },
    },
  })
  for (const file of ['protocol.js', 'background.js']) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, file), 'utf8'), context)
  }
  const link = { platform: 'doubao', socket: { readyState: 1, send: raw => replies.push(JSON.parse(raw)) } }
  async function send(release) {
    await context.dispatchCommand(link, { taskId: 'local', payload: { name: 'download', tabId: result.id, release } })
    return replies.pop().payload
  }
  assert.equal((await send(false)).tabId, result.id)
  assert.equal((await send(true)).tabId, result.id)
  open.delete(result.id)
  assert.equal((await send(false)).code, 'E_NO_TAB')
  open.set(result.id, { id: result.id, url: 'https://www.qianwen.com/chat/other' })
  assert.equal((await send(true)).code, 'E_NO_TAB')
  assert.deepStrictEqual(delivered, [result.id, result.id])
  assert.deepStrictEqual(focused, [])
})
