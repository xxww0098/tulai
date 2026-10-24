// 千问页面动作。选择器沿用 internal/strategy/js/ego.js 里已经对着通义/千问页面验证过的锚点，
// 不另起一套 class 名。输入区旁边已有 input[type=file]（可以是隐藏的）时直接 DataTransfer 赋值。
// 当前千问菜单里没有这个 input：「上传图片」会临时创建 input 并调用 showPicker。
// 主世界 chooser.js 武装之后，点这项把源图交进去，不打开系统文件框。
// 本文件不碰 chrome.*，方便用 node 对锚点做夹具测试。
globalThis.XBSPage = (function () {
  const USER_Q = '[data-message-author-role="user"],[data-message-role="user"],[data-role="user"],[data-author="user"]'
  const ASSISTANT_Q = '[data-message-author-role="assistant"],[data-message-role="assistant"],[data-role="assistant"],[data-author="assistant"]'
  const CAPTCHA_TEXT = /人机验证|安全验证|滑动验证|滑块验证|请完成.{0,30}验证|请(?:输入|填写)(?:下方|图中|图片|图形)?验证码|完成下方验证|(?:complete|solve|enter).{0,20}\bcaptcha\b|verify (?:that )?you are human|human verification|security verification/i

  function hostOf(doc) {
    if (doc && doc.hostname) return String(doc.hostname)
    try {
      return String(location.hostname || '')
    } catch (e) {
      return ''
    }
  }

  // ego.js 只认 tongyi.com / qianwen.com。qwen.ai 是扩展原先匹配的国际站，卡片 class 未单独采过，
  // 角色属性和按钮文案仍适用；没有角色节点时沿用同一套 .chat-*-card-wrap 兜底。
  function qwenHost(host) {
    return /(^|\.)((tongyi|qianwen)\.com|qwen\.ai)$/.test(host || '')
  }

  function all(root, sel) {
    const scope = root || null
    if (!scope || !scope.querySelectorAll) return []
    try {
      return Array.from(scope.querySelectorAll(sel))
    } catch (e) {
      return []
    }
  }

  function styleOf(el) {
    try {
      return getComputedStyle(el)
    } catch (e) {
      return { display: '', visibility: '' }
    }
  }

  function visible(el) {
    if (!el || el.isConnected === false) return false
    const rect = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 }
    if (rect.width <= 5 || rect.height <= 5) return false
    const style = styleOf(el)
    return style.display !== 'none' && style.visibility !== 'hidden'
  }

  function enabled(el) {
    return visible(el) && !el.disabled && (!el.getAttribute || el.getAttribute('aria-disabled') !== 'true')
  }

  function labelOf(el) {
    if (!el || !el.getAttribute) return ''
    return ((el.getAttribute('aria-label') || '') + ' ' + (el.getAttribute('title') || '') + ' ' + (el.innerText || '')).trim()
  }

  function normalize(text) {
    return String(text || '').replace(/\uFEFF/g, '').replace(/\r\n/g, '\n').replace(/\s+/g, ' ').trim()
  }

  function queries(doc) {
    let assistant = ASSISTANT_Q
    let user = USER_Q
    const host = hostOf(doc)
    const qwen = qwenHost(host)
    if (qwen && all(doc, assistant + ',' + user).length === 0) {
      assistant += ',.chat-answers-card-wrap'
      user += ',.chat-question-card-wrap'
    }
    return { assistant, user, qwen, host }
  }

  function composer(doc) {
    return all(doc, 'textarea,[contenteditable="true"],input[type="text"]').filter(enabled).pop() || null
  }

  function readComposer(comp) {
    if (!comp) return ''
    if (comp.tagName === 'TEXTAREA' || comp.tagName === 'INPUT') return String(comp.value || '')
    function walk(node) {
      if (!node) return ''
      if (node.nodeType === 3) return String(node.nodeValue || '').replace(/\uFEFF/g, '')
      if (node.nodeType && node.nodeType !== 1) return ''
      if (node !== comp && node.getAttribute && (node.getAttribute('contenteditable') === 'false' || node.getAttribute('aria-hidden') === 'true')) return ''
      if (node.tagName === 'BR') return '\n'
      const children = node.childNodes || node.children || []
      let text = ''
      for (let i = 0; i < children.length; i++) text += walk(children[i])
      if (node !== comp && /^(DIV|P|LI)$/.test(node.tagName || '') && text) return text + '\n'
      return text
    }
    return walk(comp).replace(/\n$/, '')
  }

  function setComposer(comp, text) {
    if (!comp) return false
    if (typeof comp.focus === 'function') comp.focus()
    if (comp.tagName === 'TEXTAREA' || comp.tagName === 'INPUT') {
      const proto = comp.tagName === 'TEXTAREA'
        ? (typeof HTMLTextAreaElement !== 'undefined' ? HTMLTextAreaElement.prototype : null)
        : (typeof HTMLInputElement !== 'undefined' ? HTMLInputElement.prototype : null)
      const desc = proto ? Object.getOwnPropertyDescriptor(proto, 'value') : null
      if (desc && desc.set) desc.set.call(comp, text)
      else comp.value = text
      dispatchInput(comp, text)
      return normalize(readComposer(comp)) === normalize(text)
    }
    try {
      const sel = (comp.ownerDocument || document).getSelection()
      const range = (comp.ownerDocument || document).createRange()
      range.selectNodeContents(comp)
      sel.removeAllRanges()
      sel.addRange(range)
      const doc = comp.ownerDocument || document
      if (!doc.execCommand || !doc.execCommand('insertText', false, text)) comp.textContent = text
    } catch (e) {
      comp.textContent = text
    }
    dispatchInput(comp, text)
    return normalize(readComposer(comp)) === normalize(text)
  }

  function dispatchInput(el, text) {
    if (!el || typeof el.dispatchEvent !== 'function') return
    let ev
    try {
      ev = new InputEvent('input', { bubbles: true, inputType: 'insertText', data: text })
    } catch (e) {
      try { ev = new Event('input', { bubbles: true }) } catch (err) { return }
    }
    el.dispatchEvent(ev)
  }

  // 从输入框往上找，停在包含「添加附件」或上传按钮的那一层。不取整页第一个 file input。
  function uploadEntry(doc, comp) {
    for (let scope = comp && comp.parentElement; scope && scope !== doc.body; scope = scope.parentElement) {
      const found = all(scope, 'button[aria-label="添加附件"],button[data-testid="upload_file_button"]').filter(enabled)[0]
      if (found) return found
    }
    return null
  }

  function attachmentScope(comp, entry) {
    if (!comp) return null
    const surface = closest(comp, '.guidance-input-surface')
    if (surface && entry && surface.contains(entry)) return surface
    const body = closest(comp, '[data-chat-input-body]')
    if (body && entry && body.contains(entry)) return body
    for (let node = comp.parentElement; node && node !== (comp.ownerDocument || {}).body; node = node.parentElement) {
      if (entry && node.contains && node.contains(entry)) return node
    }
    return comp.parentElement || null
  }

  function closest(el, sel) {
    for (let node = el; node; node = node.parentElement) {
      if (node.matches && node.matches(sel)) return node
    }
    return null
  }

  function fileInputs(scope) {
    return all(scope, 'input[type="file"]').filter(function (input) {
      const accept = (input.getAttribute && input.getAttribute('accept')) || ''
      if (!accept) return true
      return /image|png|jpe?g|webp|gif|\*/i.test(accept)
    })
  }

  // 隐藏 input 也算数。优先用附件区内最后一个，避免点到历史消息里的 input。
  function fileInput(doc, comp) {
    const entry = uploadEntry(doc, comp)
    const scope = attachmentScope(comp, entry)
    const scoped = fileInputs(scope)
    if (scoped.length) return scoped[scoped.length - 1]
    if (!comp) return null
    for (let node = comp.parentElement; node; node = node.parentElement) {
      const found = fileInputs(node)
      if (found.length) return found[found.length - 1]
    }
    return null
  }

  // ego.js qwenImageItem：菜单项全文以「上传图片」开头，或某个子节点的文本恰好是「上传图片」。
  function uploadImageItem(doc) {
    return all(doc, '[role="menuitem"]').find(function (item) {
      if (!enabled(item)) return false
      const text = String(item.innerText || '').trim()
      if (/^上传图片(?:\s|$)/.test(text)) return true
      return all(item, 'span,p,div').some(function (label) { return String(label.innerText || '').trim() === '上传图片' })
    }) || null
  }

  function unsentBlocksUpload(doc, comp) {
    const entry = uploadEntry(doc, comp)
    if (!entry || !entry.getAttribute) return ''
    if (entry.getAttribute('aria-label') !== '添加附件') return ''
    if (!normalize(readComposer(comp))) return ''
    if (fileInput(doc, comp)) return ''
    return '当前输入区有未发送内容，未操作上传菜单'
  }

  function sendButton(doc, comp) {
    for (let scope = comp && comp.parentElement; scope && scope !== doc.body; scope = scope.parentElement) {
      const found = all(scope, 'button,[role="button"]').filter(function (el) {
        if (!enabled(el)) return false
        if (el.getAttribute && el.getAttribute('data-testid') === 'chat_input_send_button') return true
        return [el.getAttribute && el.getAttribute('aria-label'), el.getAttribute && el.getAttribute('title'), el.innerText].some(function (text) {
          return /^(发送|发送消息|Send|Send message)$/i.test(String(text || '').trim())
        })
      }).pop()
      if (found) return found
    }
    return null
  }

  function loginWall(doc) {
    if (composer(doc)) return false
    if (all(doc, 'input[type="password"]').some(visible)) return true
    return all(doc, 'button,[role="button"],a').some(function (el) {
      return enabled(el) && /^(登录|扫码登录|立即登录)$/.test(String(el.innerText || '').trim())
    })
  }

  function removeButtons(doc, comp) {
    const scope = attachmentScope(comp, uploadEntry(doc, comp))
    if (!scope) return []
    return all(scope, 'button,[role="button"]').filter(function (el) {
      return enabled(el) && /删除|移除|remove attachment/i.test(labelOf(el))
    })
  }

  function hash(text) {
    let value = 2166136261
    const s = String(text || '')
    for (let i = 0; i < s.length; i++) value = Math.imul(value ^ s.charCodeAt(i), 16777619)
    return (value >>> 0).toString(16)
  }

  function businessText(root) {
    function walk(node) {
      if (!node) return ''
      if (node.nodeType === 3) return node.nodeValue || ''
      if (node.nodeType && node.nodeType !== 1) return ''
      if (/^(BUTTON|NAV|TIME|SVG|SCRIPT|STYLE)$/.test(node.tagName || '')) return ''
      if (node.getAttribute && (node.getAttribute('role') === 'button' || node.getAttribute('role') === 'toolbar' || node.getAttribute('aria-hidden') === 'true')) return ''
      const style = styleOf(node)
      if (style.display === 'none' || style.visibility === 'hidden') return ''
      const children = node.childNodes || node.children || []
      let text = ''
      for (let i = 0; i < children.length; i++) text += walk(children[i])
      return text
    }
    return normalize(walk(root))
  }

  function matchesQuery(node, query) {
    return !!(node && node.matches && node.matches(query))
  }

  function orderedMessages(doc, user, assistant) {
    return all(doc, assistant + ',' + user).filter(function (node, index, nodes) {
      return !nodes.some(function (parent) { return parent !== node && parent.contains && parent.contains(node) })
    })
  }

  function messageKey(node, user) {
    const role = matchesQuery(node, user) ? 'user' : 'assistant'
    const id = (node.getAttribute && (node.getAttribute('data-message-id') || node.getAttribute('data-id'))) || node.id || ''
    if (id) return role + ':' + id
    return role + ':' + hash(businessText(node))
  }

  function userContents(root) {
    const nodes = [root].concat(all(root, 'p,div,span'))
    const out = []
    nodes.forEach(function (node) {
      const text = businessText(node)
      if (text && out.indexOf(text) === -1) out.push(text)
    })
    return out
  }

  function qwenLoadingImage(img, qwen) {
    if (!qwen || !img.getAttribute) return false
    if (String(img.getAttribute('alt') || '').trim().toLowerCase() !== 'loading') return false
    return !!closest(img, '[id^="card_card_ai_generate_image_"]')
  }

  function busy(root) {
    if (!root) return false
    if (all(root, '[aria-busy="true"],[role="progressbar"]').some(visible)) return true
    if (all(root, 'img').some(function (img) { return visible(img) && qwenLoadingImage(img, true) })) return true
    const text = root.innerText || ''
    return /正在生成|生成中|绘制中|\bGenerating\b/i.test(text)
  }

  function imageURL(img) {
    return String((img && (img.currentSrc || img.src)) || '')
  }

  function mark(doc, prompt) {
    const q = queries(doc)
    const comp = composer(doc)
    const entry = uploadEntry(doc, comp)
    const scope = attachmentScope(comp, entry)
    const messages = orderedMessages(doc, q.user, q.assistant)
    const images = new Set(all(doc, 'img'))
    const sourceURLs = new Set()
    if (scope) all(scope, 'img').forEach(function (img) { sourceURLs.add(imageURL(img)) })
    return {
      prompt: normalize(prompt),
      q,
      baseline: messages.map(function (node) { return messageKey(node, q.user) }),
      tail: messages[messages.length - 1] || null,
      userBodies: messages.filter(function (node) { return matchesQuery(node, q.user) }).map(userContents),
      images,
      sourceURLs,
      anchor: null,
      anchorConfirmed: false,
      scope,
    }
  }

  function newAssistants(doc, state) {
    if (!state) return []
    const q = state.q || queries(doc)
    const messages = orderedMessages(doc, q.user, q.assistant)
    const prompt = state.prompt
    let anchor = null
    if (prompt && state.anchorConfirmed && state.anchor && state.anchor.isConnected !== false && messages.indexOf(state.anchor) !== -1 && userContents(state.anchor).indexOf(prompt) !== -1) {
      anchor = state.anchor
    } else if (prompt) {
      const oldMatches = (state.userBodies || []).filter(function (bodies) { return bodies.indexOf(prompt) !== -1 }).length
      const matches = messages.filter(function (node) { return matchesQuery(node, q.user) && userContents(node).indexOf(prompt) !== -1 })
      if (matches.length > oldMatches) anchor = matches[oldMatches]
      if (!anchor && state.tail && state.tail.isConnected !== false) {
        const tailIndex = messages.indexOf(state.tail)
        if (tailIndex !== -1) anchor = matches.find(function (node) { return messages.indexOf(node) > tailIndex }) || null
      }
    }
    state.anchor = anchor
    if (!anchor) return []
    state.anchorConfirmed = true
    let following = messages.slice(messages.indexOf(anchor) + 1)
    const nextUser = following.findIndex(function (node) { return matchesQuery(node, q.user) })
    if (nextUser !== -1) following = following.slice(0, nextUser)
    return following.filter(function (node) { return matchesQuery(node, q.assistant) })
  }

  function resultImages(doc, state) {
    const q = (state && state.q) || queries(doc)
    const messages = newAssistants(doc, state)
    const images = []
    messages.forEach(function (message) {
      all(message, 'img').forEach(function (img) {
        if (qwenLoadingImage(img, q.qwen)) return
        if (q.qwen) {
          const markdown = closest(img, '#qk-markdown-react,.markdown-pc-special-class')
          if (!markdown || !message.contains(markdown)) return
        }
        if (!visible(img) || !img.complete || !(img.naturalWidth > 0)) return
        if (state.images && state.images.has(img)) return
        if (state.sourceURLs && state.sourceURLs.has(imageURL(img))) return
        const url = imageURL(img)
        if (!url) return
        images.push({ url, img })
      })
    })
    const seen = new Set()
    return images.filter(function (item) {
      if (seen.has(item.url)) return false
      seen.add(item.url)
      return true
    })
  }

  function resultsBusy(doc, state) {
    return newAssistants(doc, state).some(busy)
  }

  function composerCleared(doc) {
    const comp = composer(doc)
    return !!(comp && normalize(readComposer(comp)) === '')
  }

  // 与 egoSend 一样，输入被清空视作平台已接受。输入还在、但本轮用户消息已经出现时也算接受，避免误点第二次。
  function sendAccepted(doc, state) {
    if (composerCleared(doc)) return true
    if (!state || !state.prompt) return false
    const q = state.q || queries(doc)
    const messages = orderedMessages(doc, q.user, q.assistant)
    const oldMatches = (state.userBodies || []).filter(function (bodies) { return bodies.indexOf(state.prompt) !== -1 }).length
    const matches = messages.filter(function (node) { return matchesQuery(node, q.user) && userContents(node).indexOf(state.prompt) !== -1 })
    return matches.length > oldMatches
  }

  function assignFiles(input, files) {
    if (!input || typeof DataTransfer === 'undefined') return false
    const dt = new DataTransfer()
    for (let i = 0; i < files.length; i++) dt.items.add(files[i])
    input.files = dt.files
    if (typeof input.dispatchEvent === 'function') {
      try { input.dispatchEvent(new Event('change', { bubbles: true })) } catch (e) {}
      try { input.dispatchEvent(new Event('input', { bubbles: true })) } catch (e) {}
    }
    return !!(input.files && input.files.length)
  }

  // 与 qianwen/chooser.js 约定的事件名。源图只在派发 arm 的同步调用里留在 DOM 上。
  const CHOOSER_SLOT = 'xbs-qianwen-file-slot'

  function createChooserBridge(doc) {
    const root = doc && doc.documentElement
    if (!root || typeof root.getAttribute !== 'function' || root.getAttribute('data-xbs-chooser') !== 'qianwen') return null
    return {
      arm: function (file) { return armChooserSlot(doc, root, file) },
      result: function () { return root.getAttribute('data-xbs-chooser-result') || '' },
      disarm: function () {
        try { doc.dispatchEvent(new Event('xbs-qianwen-disarm')) } catch (e) {}
        const slot = doc.getElementById && doc.getElementById(CHOOSER_SLOT)
        if (slot && typeof slot.remove === 'function') {
          try { slot.remove() } catch (err) {}
        }
      },
    }
  }

  function ensureChooserSlot(doc) {
    if (!doc || typeof doc.createElement !== 'function') return null
    const existing = doc.getElementById && doc.getElementById(CHOOSER_SLOT)
    if (existing) return existing
    const slot = doc.createElement('pre')
    slot.id = CHOOSER_SLOT
    slot.textContent = ''
    if (typeof slot.setAttribute === 'function') {
      slot.setAttribute('hidden', '')
      slot.setAttribute('aria-hidden', 'true')
    }
    const parent = doc.documentElement || doc.body
    if (!parent || typeof parent.appendChild !== 'function') return null
    parent.appendChild(slot)
    return slot
  }

  async function armChooserSlot(doc, root, file) {
    root.setAttribute('data-xbs-chooser-result', '')
    root.setAttribute('data-xbs-chooser-armed', '0')
    let bytes
    try {
      bytes = new Uint8Array(await file.arrayBuffer())
    } catch (e) {
      return false
    }
    const slot = ensureChooserSlot(doc)
    if (!slot) return false
    try {
      slot.textContent = JSON.stringify({
        name: (file && file.name) || 'image',
        mime: (file && file.type) || 'application/octet-stream',
        b64: bytesToBase64(bytes),
      })
      doc.dispatchEvent(new Event('xbs-qianwen-arm'))
    } catch (e) {
      slot.textContent = ''
      return false
    }
    const armed = root.getAttribute('data-xbs-chooser-armed') === '1'
    slot.textContent = ''
    if (typeof slot.remove === 'function') {
      try { slot.remove() } catch (e) {}
    }
    return armed
  }

  function nestedFileInput(doc) {
    const item = uploadImageItem(doc)
    if (!item || typeof item.querySelector !== 'function') return null
    return item.querySelector('input[type="file"]')
  }

  function failedAssign() {
    return { error: '无法把文件写入 input[type=file]', code: 'E_UPLOAD_FAIL' }
  }

  // HTMLElement.click() 只有 click，没有 pointerdown。Radix 触发器在 pointerdown 上打开菜单，
  // 并 preventDefault，真实点击不会再补一次 click。菜单项则相反：pointerdown 只是拦住焦点，真正选中仍在 click。
  function dispatchUiEvent(el, type) {
    if (!el || typeof el.dispatchEvent !== 'function') return null
    const rect = el.getBoundingClientRect ? el.getBoundingClientRect() : null
    const x = rect ? rect.left + rect.width / 2 : 0
    const y = rect ? rect.top + rect.height / 2 : 0
    const point = {
      bubbles: true,
      cancelable: true,
      composed: true,
      clientX: x,
      clientY: y,
      screenX: x,
      screenY: y,
      button: 0,
      buttons: type === 'pointerdown' || type === 'mousedown' ? 1 : 0,
      pointerId: 1,
      pointerType: 'mouse',
      isPrimary: true,
    }
    const pointer = type === 'pointerdown' || type === 'pointerup'
    let ev = null
    try {
      if (pointer && typeof PointerEvent === 'function') ev = new PointerEvent(type, point)
    } catch (e) { ev = null }
    if (!ev) {
      try {
        if (typeof MouseEvent === 'function') ev = new MouseEvent(type, point)
      } catch (e) { ev = null }
    }
    if (!ev) {
      try { ev = new Event(type, { bubbles: true, cancelable: true, composed: true }) } catch (e) { return null }
    }
    try { el.dispatchEvent(ev) } catch (e) {}
    return ev
  }

  function poke(el) {
    const down = dispatchUiEvent(el, 'pointerdown')
    dispatchUiEvent(el, 'mousedown')
    dispatchUiEvent(el, 'pointerup')
    dispatchUiEvent(el, 'mouseup')
    return down
  }

  // 发送按钮和「上传图片」都要走完 pointer 再 click。pointerdown 若已 preventDefault，click 仍然要发：
  // Radix 菜单项的选中在 click 上。
  function activate(el) {
    if (!el) return false
    poke(el)
    try { el.click() } catch (e) { return false }
    return true
  }

  // 添加附件是菜单触发器。pointerdown 已经把菜单打开，或已经 preventDefault 时，再 click 会把菜单关掉。
  function openUploadEntry(doc, entry) {
    const down = poke(entry)
    if (uploadImageItem(doc)) return
    if (down && down.defaultPrevented) return
    try { entry.click() } catch (e) {}
  }

  // pointerup 已经把文件交给主世界桥时不要再 click，否则会落到没武装的 showPicker，弹出系统文件框。
  function activateChooserItem(item, bridge) {
    poke(item)
    let status = bridge && bridge.result ? bridge.result() : ''
    if (status === 'ok' || status === 'fail') return status
    try { item.click() } catch (e) {}
    return bridge && bridge.result ? bridge.result() : ''
  }

  // 先走已有 file input（旧千问、以及菜单项里嵌着 input 的情况）。
  // 没有 input、且主世界桥已就绪时，才点「上传图片」，让页面自己创建 input 并调用 showPicker。
  async function attachLocalImage(doc, file, opts) {
    opts = opts || {}
    const cancelled = opts.cancelled || function () { return false }
    const pause = opts.sleep || function (ms) { return new Promise(function (resolve) { setTimeout(resolve, ms) }) }
    const timeout = Number(opts.timeout) || 2000
    const bridge = opts.bridge || null
    const comp = composer(doc)
    const direct = fileInput(doc, comp)
    if (direct) {
      if (!assignFiles(direct, [file])) return failedAssign()
      return { input: direct, assigned: true }
    }
    const nestedNow = nestedFileInput(doc)
    if (nestedNow) {
      if (!assignFiles(nestedNow, [file])) return failedAssign()
      return { input: nestedNow, assigned: true }
    }
    const entry = uploadEntry(doc, comp)
    if (!entry) return { error: '未找到当前输入区上传入口', code: 'E_SELECTOR_MISS' }
    let armed = false
    try {
      if (cancelled()) return { error: '上传已取消', code: 'E_CANCELLED' }
      if (bridge && bridge.arm) {
        try {
          armed = !!(await bridge.arm(file))
        } catch (e) {
          armed = false
        }
        if (!armed) return { error: '页面上传桥没有就绪，请刷新千问页面后重试', code: 'E_UPLOAD_FAIL' }
      }
      if (!uploadImageItem(doc)) openUploadEntry(doc, entry)
      const deadline = Date.now() + timeout
      let clickedItem = false
      while (Date.now() < deadline) {
        if (cancelled()) return { error: '上传已取消', code: 'E_CANCELLED' }
        const status = bridge && bridge.result ? bridge.result() : ''
        if (status === 'ok') return { assigned: true, via: 'chooser' }
        if (status === 'fail') return failedAssign()
        const input = fileInput(doc, composer(doc))
        if (input) {
          if (!assignFiles(input, [file])) return failedAssign()
          return { input: input, assigned: true }
        }
        const nested = nestedFileInput(doc)
        if (nested) {
          if (!assignFiles(nested, [file])) return failedAssign()
          return { input: nested, assigned: true }
        }
        const item = uploadImageItem(doc)
        if (bridge && item && !clickedItem) {
          clickedItem = true
          const immediate = activateChooserItem(item, bridge)
          if (immediate === 'ok') return { assigned: true, via: 'chooser' }
          if (immediate === 'fail') return failedAssign()
          continue
        }
        await pause(50)
      }
      if (bridge && bridge.result && bridge.result() === 'ok') return { assigned: true, via: 'chooser' }
      if (clickedItem) return { error: '点击「上传图片」后没有接到文件选择（页面没有交出 input[type=file]）', code: 'E_SELECTOR_MISS' }
      return { error: '未找到输入区的文件选择框（上传入口旁没有 input[type=file]）', code: 'E_SELECTOR_MISS' }
    } finally {
      if (armed && bridge && bridge.disarm) {
        try { bridge.disarm() } catch (e) {}
      }
    }
  }

  // 千问附件区里的短标签。提示词在输入框的 value 里，不会被这层当成失败。
  function uploadFailureLabel(scope) {
    if (!scope) return false
    const nodes = all(scope, 'span,p,div,button,[role="status"],[role="alert"]')
    for (let i = 0; i < nodes.length; i++) {
      const el = nodes[i]
      const bits = [el.innerText, el.getAttribute && el.getAttribute('aria-label'), el.getAttribute && el.getAttribute('title')]
      for (let j = 0; j < bits.length; j++) {
        const text = normalize(bits[j] || '')
        if (!text || text.length > 24) continue
        if (text === '上传失败' || text.indexOf('上传失败') === 0) return true
      }
    }
    return false
  }

  function attachmentPreviewHost(url) {
    if (!/^https:\/\//i.test(url || '')) return false
    let host = ''
    try {
      if (typeof URL === 'function') host = new URL(url).hostname
    } catch (e) {
      host = ''
    }
    if (!host) {
      const matched = String(url).match(/^https:\/\/([^/?#]+)/i)
      host = matched ? matched[1] : ''
    }
    return /(^|\.)((qianwen|tongyi)\.com|alicdn\.com)$/i.test(host)
  }

  function attachmentRejected(doc) {
    const comp = composer(doc)
    return uploadFailureLabel(attachmentScope(comp, uploadEntry(doc, comp)))
  }

  // 解码成功的新预览仍是首选。workspace-zb-cdn.qianwen.com 有时图已挂上但 naturalWidth 停在 0，
  // 附件还在、页面也没写「上传失败」时算就绪。写了「上传失败」则整块不算就绪。
  function freshAttachment(doc, baseline) {
    const comp = composer(doc)
    const scope = attachmentScope(comp, uploadEntry(doc, comp))
    if (!scope || !baseline) return false
    if (uploadFailureLabel(scope)) return false
    const imgs = all(scope, 'img')
    const decoded = imgs.some(function (img) {
      return visible(img) && img.complete && img.naturalWidth > 0 && !baseline.has(img)
    })
    if (decoded) return !busy(scope)
    const undecoded = imgs.some(function (img) {
      if (!visible(img) || !img.complete || img.naturalWidth > 0 || baseline.has(img)) return false
      return attachmentPreviewHost(imageURL(img))
    })
    return undecoded && !busy(scope)
  }

  function documentURL(doc) {
    if (doc && typeof doc.URL === 'string' && doc.URL) return doc.URL
    try {
      if (doc && doc.location && doc.location.href) return String(doc.location.href)
    } catch (e) {}
    try { return String(location.href || '') } catch (e) { return '' }
  }

  // 阿里云 Baxia 滑块：整页或 iframe 会落到 _____tmd_____/punish，或带 baxia / nocaptcha。
  function isBaxiaURL(url) {
    const s = String(url || '')
    if (!s) return false
    if (s.indexOf('_____tmd_____') !== -1) return true
    return /\/punish(?:[/?#]|$)/i.test(s) && /baxia|x5secdata|nocaptcha/i.test(s)
  }

  function elementSrc(el) {
    if (!el) return ''
    const attr = el.getAttribute && el.getAttribute('src')
    return String(attr || el.src || '')
  }

  function elementIdentity(el) {
    if (!el) return ''
    return ((el.className || '') + ' ' + (el.id || '') + ' ' + ((el.getAttribute && el.getAttribute('data-testid')) || '') + ' ' + elementSrc(el)).toString()
  }

  function looksBaxia(el) {
    if (!el) return false
    if (/baxia|nocaptcha|_____tmd_____|nc-container/i.test(elementIdentity(el))) return true
    return isBaxiaURL(elementSrc(el))
  }

  // 检测逻辑缩自 internal/captcha/js/inject.js：验证码语义必须落在可见容器上，不能借整页其它文字。
  // iframe 仅因路径里有 punish 不够，还要带上 tmd / baxia / x5secdata，避免误伤普通地址。
  function captchaEvidence(el) {
    if (!el) return false
    const identity = elementIdentity(el)
    if (/captcha|geetest|yidun|turing|verification|baxia|nocaptcha|_____tmd_____|nc-container/i.test(identity)) return true
    if (el.tagName === 'IFRAME') {
      const src = elementSrc(el)
      if (/captcha|verify|geetest|turing|baxia|nocaptcha|_____tmd_____/i.test(src)) return true
      return isBaxiaURL(src)
    }
    return CAPTCHA_TEXT.test(String(el.innerText || '').slice(0, 1000))
  }

  const CAPTCHA_SEL = [
    '#captcha_verify_image',
    'iframe[src*="captcha"]', 'iframe[src*="verify"]', 'iframe[src*="geetest"]',
    'iframe[src*="baxia"]', 'iframe[src*="_____tmd_____"]', 'iframe[src*="punish"]', 'iframe[src*="nocaptcha"]',
    '[class*="captcha"]', '[class*="geetest"]', '[class*="verification"]',
    '[class*="baxia"]', '[id*="baxia"]', '[id*="nocaptcha"]', '[class*="nc-container"]',
  ]

  function detectCaptcha(doc) {
    if (isBaxiaURL(documentURL(doc))) {
      return describeCaptcha(doc.body || doc.documentElement || doc, true)
    }
    for (let i = 0; i < CAPTCHA_SEL.length; i++) {
      const candidates = all(doc, CAPTCHA_SEL[i])
      for (let j = 0; j < candidates.length; j++) {
        let el = candidates[j]
        if (!visible(el) && el.tagName !== 'IFRAME') continue
        if (el.tagName === 'CANVAS') el = el.parentElement
        if (!el || el === doc.body || !captchaEvidence(el)) continue
        return describeCaptcha(el)
      }
    }
    const canvases = all(doc, 'canvas')
    for (let k = 0; k < canvases.length; k++) {
      const cv = canvases[k]
      if (!visible(cv)) continue
      let pa = cv.parentElement
      for (let d = 0; d < 6 && pa && pa !== doc.body; d++) {
        if (captchaEvidence(pa)) return describeCaptcha(pa)
        pa = pa.parentElement
      }
    }
    return { blocked: false, kind: '', root: null }
  }

  function describeCaptcha(root, baxia) {
    const text = String((root && root.innerText) || '')
    const marked = !!baxia || looksBaxia(root)
    let kind = 'unknown'
    if (marked || /滑块|滑动|slider/i.test(text) || (root && all(root, '[class*="slider"]').length)) kind = 'slider'
    else if (root && (all(root, 'canvas').length > 1 || /九宫|点选/.test(text))) kind = 'grid'
    return { blocked: true, kind, root, baxia: marked }
  }

  function captchaCrop(root) {
    if (!root || !root.querySelector) return ''
    const canvas = root.querySelector('canvas')
    if (!canvas || typeof canvas.toDataURL !== 'function') return ''
    try {
      const url = canvas.toDataURL('image/png')
      if (typeof url === 'string' && url.length > 0 && url.length < 1500000) return url
    } catch (e) {}
    return ''
  }

  function bytesToBase64(bytes) {
    let binary = ''
    const chunk = 0x8000
    for (let i = 0; i < bytes.length; i += chunk) {
      binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunk))
    }
    return btoa(binary)
  }

  function base64ToBytes(data) {
    const binary = atob(data || '')
    const out = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
    return out
  }

  return {
    qwenHost,
    hostOf,
    composer,
    readComposer,
    setComposer,
    fileInput,
    uploadEntry,
    uploadImageItem,
    unsentBlocksUpload,
    sendButton,
    loginWall,
    removeButtons,
    mark,
    resultImages,
    resultsBusy,
    composerCleared,
    sendAccepted,
    assignFiles,
    activate,
    createChooserBridge,
    attachLocalImage,
    freshAttachment,
    attachmentRejected,
    attachmentScope,
    detectCaptcha,
    captchaCrop,
    bytesToBase64,
    base64ToBytes,
    normalize,
    imageURL,
    queries,
    USER_Q,
    ASSISTANT_Q,
  }
})()
