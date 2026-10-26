const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

const context = vm.createContext({ globalThis: {} })
context.globalThis = context
vm.runInContext(fs.readFileSync(path.join(__dirname, 'bridge_logic.js'), 'utf8'), context)
const bridge = context.XBSBridge

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
