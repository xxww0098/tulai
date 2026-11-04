const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

async function main() {
  let before, headers, failed
  const streams = new Map()
  const chrome = { webRequest: {
    onBeforeRequest: { addListener(fn) { before = fn } },
    onHeadersReceived: { addListener(fn) { headers = fn } },
    onErrorOccurred: { addListener(fn) { failed = fn } },
    filterResponseData(id) {
      const stream = { written: [], closed: false, disconnected: false,
        write(bytes) { this.written.push(Buffer.from(bytes)) },
        close() { this.closed = true },
        disconnect() { this.disconnected = true },
      }
      streams.set(id, stream)
      return stream
    },
  } }
  const context = vm.createContext({ chrome, URL, TextDecoder, setTimeout, clearTimeout })
  context.globalThis = context
  vm.runInContext(fs.readFileSync(path.join(__dirname, '..', 'network.js'), 'utf8'), context)
  const net = context.TULAINetwork
  const seen = [], raw = []
  assert.strictEqual(await net.start(7, 'doubao', 'first', (x) => seen.push(x), (x) => raw.push(x)), true)
  before({ tabId: 7, requestId: 'early', method: 'POST', url: 'https://www.doubao.com/chat/completion' })
  assert.strictEqual(streams.size, 0)
  net.mark(7, 'first')
  before({ tabId: 7, requestId: 'unrelated', method: 'POST', url: 'https://www.doubao.com/other' })
  before({ tabId: 7, requestId: 'one', method: 'POST', url: 'https://www.doubao.com/samantha/chat/completion?private=secret' })
  const stream = streams.get('one')
  assert.ok(stream)
  headers({ tabId: 7, requestId: 'one', statusCode: 200 })
  const image = 'https://p3.byteimg.com/path/picture.jpeg'
  const source = image + '~tplv-preview.png?token=private'
  const original = image + '~tplv-image_raw.png?signature=private'
  const error = { event_type: 2005, event_data: JSON.stringify({ error_code: 123, error_msg: '请求过于频繁', prompt: 'private prompt' }) }
  const imageEvent = { event_data: JSON.stringify({ image: { image_preview: { url: source }, image_ori_raw: { url: original } } }) }
  const payload = Buffer.from(': heartbeat\nid: 123\nevent: message\nretry: 1000\ndata: ' + JSON.stringify(error) + '\n\nevent: message\ndata: {\ndata: "event_data": ' + JSON.stringify(imageEvent.event_data) + '\ndata: }\n\n')
  for (let pos = 0; pos < payload.length; pos += 17) {
    stream.ondata({ data: Uint8Array.from(payload.subarray(pos, pos + 17)).buffer })
  }
  stream.onstop()
  assert.deepStrictEqual(Buffer.concat(stream.written), payload, '监听不能修改响应字节')
  assert.strictEqual(stream.closed, true)
  assert.strictEqual(seen[0].status, 200)
  assert.strictEqual(seen[1].kind, 'rate')
  assert.strictEqual(seen[1].code, '123')
  assert.ok(!JSON.stringify(seen).includes('private'))
  assert.ok(raw.flat().some((pair) => pair.source === source && pair.raw === original))
  await net.stop(7, 'first')

  const later = []
  await net.start(7, 'doubao', 'second', (x) => later.push(x))
  net.mark(7, 'second')
  headers({ tabId: 7, requestId: 'one', statusCode: 429 })
  failed({ tabId: 7, requestId: 'one' })
  assert.strictEqual(later.length, 0, '旧任务不能影响新任务')
  before({ tabId: 7, requestId: 'two', method: 'POST', url: 'https://www.doubao.com/chat/completion' })
  const pending = streams.get('two')
  await net.stop(7, 'second')
  assert.strictEqual(pending.disconnected, true, '停止监听必须释放响应流')
  console.log('Firefox network test ok')
}

main().catch((err) => { console.error(err); process.exit(1) })
