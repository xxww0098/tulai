const XBS = globalThis.XBS

chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (!msg || msg.type !== 'COMMAND_FROM_HOST') return
  const env = msg.envelope || {}
  const name = env.payload && env.payload.name
  if (name === XBS.commands.cancel) {
    sendResponse(XBS.ack(env, { name, ok: true, message: 'PR1 没有页面内任务可取消' }))
    return true
  }
  if (!XBS.stubCommands.includes(name)) {
    sendResponse(XBS.fail(env, XBS.errors.E_PROTOCOL, '内容脚本未实现命令 ' + name))
    return true
  }
  // PR2 才会查找上传、输入、发送、下载和验证码节点。当前固定返回 E_SELECTOR_MISS。
  sendResponse(XBS.fail(env, XBS.errors.E_SELECTOR_MISS, 'stub: ' + name + ' selector is not implemented until PR2'))
  return true
})

chrome.runtime.sendMessage({ type: 'TAB_READY', href: location.href }).catch(() => {})
