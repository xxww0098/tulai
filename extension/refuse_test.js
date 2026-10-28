const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

const context = { console }
context.globalThis = context
vm.createContext(context)
vm.runInContext(fs.readFileSync(path.join(__dirname, 'refuse.js'), 'utf8'), context)
const match = context.TULAIRefuse.match

const hits = [
  '抱歉，我无法生成你描述的图片，内容不符合规范',
  '审核不通过，请修改提示词',
  '输入或者输出包含疑似敏感内容被绿网拦截',
  'Input data may contain inappropriate content.',
  'data_inspection_failed',
  'The image content does not comply with green network verification.',
  '换个话题聊聊吧',
  '我还没学会如何回答这个问题',
]
for (const text of hits) {
  assert.ok(match(text), 'should match: ' + text)
}

assert.strictEqual(match('画一只在窗台上的猫，水彩'), '')
assert.strictEqual(match(''), '')
assert.strictEqual(match('今天免费次数已用完'), '')
console.log('refuse_test ok')
