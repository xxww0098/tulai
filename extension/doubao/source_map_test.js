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

assert.deepStrictEqual(TULAIDoubaoSourceMap.match(state(attachments, creations), names, images.slice(0, 2)), [3, 1], '少返回结果时仍应准确关联已生成的图')
assert.strictEqual(TULAIDoubaoSourceMap.match(state(attachments, [creations[0], output(2, ['tos-input/1']), creations[2]]), names, [{ url: preview(1), rawUrl: raw(1) }]), null, '少返回结果时同源冲突仍不能猜测')

const resolved = TULAIDoubaoSourceMap.resolve(state(attachments, creations), names, images.slice(0, 2))
assert.ok(resolved && resolved.images.length === 3, 'resolve 应从会话状态补全屏外漏采的生成图')
assert.deepStrictEqual(resolved.indices, [3, 1, 2], 'resolve 补全后应返回完整源图索引')

const recovered = TULAIDoubaoSourceMap.recover(state(attachments, [creations[2], creations[0], creations[1]]), names)
assert.deepStrictEqual(recovered.indices, [1, 2, 3], '没有图卡时以会话附件名和引用 URI 恢复')
assert.deepStrictEqual(recovered.images.map(item => item.rawUrl), [raw(1), raw(2), raw(3)])
const recoverPartial = TULAIDoubaoSourceMap.recover(state(attachments, [creations[0], output(2, ['tos-input/1']), creations[2]]), names)
assert.deepStrictEqual(recoverPartial.indices, [3], '冲突的源图不能妨碍其他唯一关联')
assert.strictEqual(TULAIDoubaoSourceMap.recover(stale, names), null, '不能从后续附件消息之前恢复')
assert.strictEqual(TULAIDoubaoSourceMap.recover(unknownOrder, names), null, '不能恢复无法确定先后的批次')
assert.strictEqual(TULAIDoubaoSourceMap.recover(state([attachments[0], input(names[0], 'tos-input/2'), attachments[2]], creations), names), null, '重复附件名不能当成完整批次')
assert.strictEqual(TULAIDoubaoSourceMap.recover({ first: state(attachments, creations), second: state(attachments, creations) }, names), null, '多个消息列表命中时不能选其中一个')
const unsafeURL = output(1, ['tos-input/1'])
unsafeURL.image.image_ori_raw.url = 'https://other.example/output-1.png'
unsafeURL.image.image_ori = { url: preview(1) }
const safe = TULAIDoubaoSourceMap.recover(state(attachments, [unsafeURL]), names)
assert.deepStrictEqual(safe.images, [{ url: preview(1), rawUrl: undefined }], '仅返回已核对的图片域名')

const reusedState = {
  loaderData: {
    page: {
      messageList: {
        message_list: [
          { index: '1', content_block: [{ content: { attachment_block: { attachments } } }] },
          { index: '2', content_block: [{ content: { creation_block: { creations: [output(9, ['tos-input/1'])] } } }] },
          { index: '4', content_block: [{ content: { attachment_block: { attachments } } }] },
          { index: '5', content_block: [{ content: { creation_block: { creations: [output(10, ['tos-input/1']), output(11, ['tos-input/2']), output(12, ['tos-input/3'])] } } }] },
        ],
      },
    },
  },
}
assert.deepStrictEqual(
  TULAIDoubaoSourceMap.match(reusedState, names, [
    { url: preview(10), rawUrl: raw(10) },
    { url: preview(11), rawUrl: raw(11) },
    { url: preview(12), rawUrl: raw(12) },
  ]),
  [1, 2, 3],
  '复用会话中前几轮的源图 URI 不应误判为本轮冲突'
)

// SSE 结构化流直连映射：完整匹配、乱序匹配、以及缺失/歧义严格拒绝。
{
  const sseState = {
    attachments: names.map((name, i) => ({ name, uri: `tos-input/${i + 1}` })),
    creations: [3, 1, 2].map(i => ({
      key: `/tos-images/output-${i}`,
      url: preview(i),
      rawUrl: raw(i),
      imgUris: [`tos-input/${i}`],
      imgUri: `tos-input/${i}`,
    })),
  }
  const res = TULAIDoubaoSourceMap.resolveFromSSE(sseState, names, images)
  assert.ok(res && res.images.length === 3)
  assert.deepStrictEqual(res.indices, [3, 1, 2], 'SSE 完整映射应按参考 URI 正确关联')

  // DOM 仅见 2 张时 SSE 应补全屏外第 3 张并附带 rawUrl
  const sup = TULAIDoubaoSourceMap.resolveFromSSE(sseState, names, [{ url: preview(3) }, { url: preview(1) }])
  assert.ok(sup && sup.images.length === 3)
  assert.deepStrictEqual(sup.indices, [3, 1, 2])
  assert.strictEqual(sup.images[0].rawUrl, raw(3))

  const rec = TULAIDoubaoSourceMap.recoverFromSSE(sseState, names)
  assert.deepStrictEqual(rec.indices, [1, 2, 3])
  assert.deepStrictEqual(rec.images.map(x => x.rawUrl), [raw(1), raw(2), raw(3)])

  // 附件不完整或 URI 冲突必须返回 null，严禁猜测
  assert.strictEqual(TULAIDoubaoSourceMap.resolveFromSSE({ attachments: sseState.attachments.slice(0, 2), creations: sseState.creations }, names, images), null)
  const conflict = {
    attachments: sseState.attachments,
    creations: [
      sseState.creations[0],
      { key: '/tos-images/output-1', url: preview(1), rawUrl: raw(1), imgUris: ['tos-input/3'], imgUri: 'tos-input/3' },
      sseState.creations[2],
    ],
  }
  assert.strictEqual(TULAIDoubaoSourceMap.resolveFromSSE(conflict, names, images), null)
}

// 服务端静默路由降级检测：区分 flow.agent.shell（纯文本代理）与 flow.agent.creation（生图代理）。
{
  const degradedBatch = {
    loaderData: {
      page: {
        messageList: {
          message_list: [
            { index: '1', user_type: 1, content_block: [{ content: { attachment_block: { attachments } } }] },
            {
              index: '2',
              user_type: 2,
              ext: {
                bot_state: JSON.stringify({ agent_id: '19457456070409730', agent_name: '豆包', agent_psm: 'flow.agent.shell' }),
                before_content_type: '1',
              },
              content_block: [{ content: { text_block: { text: '很抱歉，我目前没办法直接生成图片。' } } }],
            },
          ],
        },
      },
    },
  }
  const route = TULAIDoubaoSourceMap.inspectRoute(degradedBatch, names)
  assert.ok(route && route.degraded === true && route.agentPsm === 'flow.agent.shell', '多图洗图被路由至 flow.agent.shell 必须判定为降级')
  assert.ok(route.textPreview.includes('没办法直接生成图片'))

  const creationBatch = {
    loaderData: {
      page: {
        messageList: {
          message_list: [
            { index: '1', user_type: 1, content_block: [{ content: { attachment_block: { attachments } } }] },
            {
              index: '2',
              user_type: 2,
              ext: {
                bot_state: { agent_id: '747548995167490123', agent_name: 'Agent-Creation', agent_psm: 'flow.agent.creation' },
                intention: 'agent_creation',
                use_creation: '1',
              },
              content_block: [{ content: { creation_block: { creations } } }],
            },
          ],
        },
      },
    },
  }
  const okRoute = TULAIDoubaoSourceMap.inspectRoute(creationBatch, names)
  assert.ok(okRoute && okRoute.degraded === false && okRoute.agentPsm === 'flow.agent.creation' && okRoute.hasCreations === true)

  // 复用对话中前几轮正常、最新一轮翻转为 flow.agent.shell 时必须准确识别当前轮降级。
  const reusedFlipped = {
    loaderData: {
      page: {
        messageList: {
          message_list: [
            { index: '1', user_type: 1, content_block: [{ content: { attachment_block: { attachments } } }] },
            {
              index: '2',
              user_type: 2,
              ext: { bot_state: JSON.stringify({ agent_psm: 'flow.agent.creation' }), use_creation: '1' },
              content_block: [{ content: { creation_block: { creations } } }],
            },
            { index: '4', user_type: 1, content_block: [{ content: { attachment_block: { attachments } } }] },
            {
              index: '5',
              user_type: 2,
              ext: { bot_state: JSON.stringify({ agent_psm: 'flow.agent.shell', agent_name: '豆包' }) },
              content_block: [{ content: { code_block: { code: '<svg/>' } } }],
            },
          ],
        },
      },
    },
  }
  const flipped = TULAIDoubaoSourceMap.inspectRoute(reusedFlipped, names)
  assert.ok(flipped && flipped.degraded === true && flipped.hasCodeBlock === true, '复用对话最新轮翻转降级必须识别')

  // 单图/文生图（无多图 names）同样按最新一轮助手消息判定路由降级。
  const singleDegraded = {
    loaderData: {
      page: {
        messageList: {
          message_list: [
            { index: '1', user_type: 1, content_block: [{ content: { text_block: { text: '画一只橘猫' } } }] },
            {
              index: '2',
              user_type: 2,
              ext: { bot_state: JSON.stringify({ agent_psm: 'flow.agent.shell', agent_name: '豆包' }) },
              content_block: [{ content: { text_block: { text: '很抱歉，我目前没办法直接生成图片。' } } }],
            },
          ],
        },
      },
    },
  }
  const singleRes = TULAIDoubaoSourceMap.inspectRoute(singleDegraded, [])
  assert.ok(singleRes && singleRes.degraded === true && singleRes.agentPsm === 'flow.agent.shell')
  assert.strictEqual(TULAIDoubaoSourceMap.inspectRoute(creationBatch, []).degraded, false)
}

console.log('doubao source mapping ok')
