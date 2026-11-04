const assert = require('assert')
const fs = require('fs')
const path = require('path')
const vm = require('vm')

vm.runInThisContext(fs.readFileSync(path.join(__dirname, 'source_map.js'), 'utf8'))

const names = ['01-a.png', '02-b.png', '03-c.png']
const raw = i => `https://p3.byteimg.com/tos-images/output-${i}~tplv-raw.png?sig=private`
const preview = i => `https://p6.byteimg.com/tos-images/output-${i}~tplv-preview.png?sig=other`
const input = (name, uri) => ({ image: { name, uri } })
const output = (i, uris) => ({ image: { image_ori_raw: { url: raw(i) }, gen_params: { img_uri: uris[0], img_uris: uris } } })
const state = (attachments, creations) => ({ loaderData: { page: { messageList: { message_list: [
  { index: '3', content_block: [{ content: { creation_block: { creations } } }] },
  { index: '1', content_block: [{ content: { attachment_block: { attachments } } }] },
] } } } })
const attachments = names.map((name, i) => input(name, `tos-input/${i + 1}`))
const creations = names.map((_, i) => output(i + 1, [`tos-input/${i + 1}`]))
const images = [3, 1, 2].map(i => ({ url: preview(i), rawUrl: raw(i) }))

assert.deepStrictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations), names, images), [3, 1, 2], '返回顺序变化时按参考 URI 关联')
assert.deepStrictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations), names, images.map(x => ({ url: x.url }))), [3, 1, 2], '没有原图地址时用预览路径匹配')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations.slice(0, 2)), names, images), null, '缺少元数据不能猜测')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, [creations[0], output(2, ['tos-input/1']), creations[2]]), names, images), null, '两个结果引用同一源图不能猜测')
assert.strictEqual(TULAIDoubaoSourceMap.match(state([attachments[0], input(names[1], 'tos-input/1'), attachments[2]], creations), names, images), null, '相同 URI 的源图不能按顺序猜测')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, [creations[0], output(2, ['tos-input/1', 'tos-input/2']), creations[2]]), names, images), null, '多个参考图不能当作唯一关联')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations.concat(output(2, ['tos-input/3']))), names, images), null, '同一结果存在冲突的来源时不能猜测')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations.concat(output(2, ['tos-input/1', 'tos-input/2']))), names, images), null, '同一结果存在模糊来源时不能忽略')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations), names, [images[0], images[0], images[2]]), null, '重复结果地址不能猜测')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations), [names[0], names[0], names[2]], images), null, '重复附件名不能猜测')
const stale = state(attachments, creations)
stale.loaderData.page.messageList.message_list.unshift({ index: '4', content_block: [{ content: { attachment_block: { attachments: names.map((name, i) => input(name, `new-input/${i}`)) } } }] })
assert.strictEqual(TULAIDoubaoSourceMap.match(stale, names, images), null, '不能用旧消息的同名附件匹配新任务')
const unknownOrder = state(attachments, creations)
delete unknownOrder.loaderData.page.messageList.message_list[1].index
assert.strictEqual(TULAIDoubaoSourceMap.match(unknownOrder, names, images), null, '不能确定消息先后时不能猜测')

console.log('doubao source mapping ok')
