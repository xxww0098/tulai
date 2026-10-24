// 豆包页面动作。选择器沿用 internal/strategy/js/ego.js 里已经对着 doubao.com 验证过的锚点：
// 上传入口 button[data-testid="upload_file_button"]，二级菜单全文「上传文件或图片」，
// 结果必须落在 [data-testid="mdbox_image"] 里。用户消息上的 [data-testid="message_image_content"]
// 不是生成结果。发送按钮与千问相同（chat_input_send_button / 发送）。
// 上传优先找输入区旁边的 input[type=file]（可以是隐藏的），用 DataTransfer 赋值。
// 不点击单独的「上传文件或图片」，否则会弹出系统文件框。
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

  // ego.js：/(^|\.)doubao\.com$/. 桌面端首页是 https://www.doubao.com/chat/。
  function doubaoHost(host) {
    return /(^|\.)doubao\.com$/.test(host || '')
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
    return { assistant: ASSISTANT_Q, user: USER_Q, doubao: doubaoHost(hostOf(doc)), host: hostOf(doc) }
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

  // 从输入框往上找最近的 upload_file_button。页上另有一颗相同 testid 时，不会选到输入区外面的那颗。
  function uploadEntry(doc, comp) {
    for (let scope = comp && comp.parentElement; scope && scope !== doc.body; scope = scope.parentElement) {
      const found = all(scope, 'button[data-testid="upload_file_button"],button[aria-label="添加附件"]').filter(enabled)[0]
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

  function exactText(el, text) {
    return String(el && el.innerText || '').trim() === text
  }

  // ego.js uploadStart：可见 [role=listbox] 里全文恰好是「上传文件或图片」的项。
  // 隐藏的旧菜单不算已经打开。这里只把它交给内容脚本找嵌套的 file input，不点击它。
  function uploadImageItem(doc) {
    const panels = all(doc, '[role="listbox"]').filter(visible)
    for (let i = 0; i < panels.length; i++) {
      const labeled = all(panels[i], '[role="option"],[role="menuitem"],button,[role="button"]').find(function (item) {
        if (!enabled(item)) return false
        if (exactText(item, '上传文件或图片')) return true
        return all(item, 'span,p,div').some(function (label) { return exactText(label, '上传文件或图片') })
      })
      if (labeled) return labeled
      const plain = all(panels[i], '*').find(function (item) {
        return enabled(item) && exactText(item, '上传文件或图片')
      })
      if (plain) return plain
    }
    return null
  }

  // 未发送草稿只拦住千问的「添加附件」。豆包的 upload_file_button 在 ego.js 里没有这条拒绝。
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

  // ego.js clientPromotionClose：豆包「立即下载电脑版」旁边唯一的「关闭」。
  // 范围里有输入框、消息、密码或验证码时不关。
  function dismissClientPromo(doc, comp) {
    if (!doubaoHost(hostOf(doc))) return null
    const cta = all(doc, 'button,[role="button"],a').find(function (button) {
      return enabled(button) && [button.getAttribute && button.getAttribute('aria-label'), button.innerText].some(function (text) {
        return String(text || '').trim() === '立即下载电脑版'
      })
    })
    for (let scope = cta && cta.parentElement, depth = 0; scope && scope !== doc.body && depth < 7; scope = scope.parentElement, depth++) {
      if (comp && scope.contains && scope.contains(comp)) break
      if (matchesQuery(scope, USER_Q) || matchesQuery(scope, ASSISTANT_Q)) break
      if (all(scope, 'input[type="password"],[data-sitekey],iframe[src*="captcha"]').length) break
      const close = all(scope, 'button,[role="button"]').find(function (button) {
        return enabled(button) && [button.getAttribute && button.getAttribute('aria-label'), button.getAttribute && button.getAttribute('title'), button.innerText].some(function (text) {
          return String(text || '').trim() === '关闭'
        })
      })
      if (close) return close
    }
    return null
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

  function busy(root) {
    if (!root) return false
    if (all(root, '[aria-busy="true"],[role="progressbar"]').some(visible)) return true
    return /正在生成|生成中|绘制中|\bGenerating\b/i.test(root.innerText || '')
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

  // 同一张 mdbox 里的大图和缩略图只留一张，取 naturalWidth 更大的地址。
  // 不在 mdbox 里的图（包括用户消息上的 message_image_content）丢掉。
  function resultImages(doc, state) {
    const messages = newAssistants(doc, state)
    const best = new Map()
    messages.forEach(function (message) {
      all(message, 'img').forEach(function (img) {
        const card = closest(img, '[data-testid="mdbox_image"]')
        if (!card || !message.contains(card)) return
        if (!visible(img) || !img.complete || !(img.naturalWidth > 0)) return
        if (state.images && state.images.has(img)) return
        const url = imageURL(img)
        if (!url) return
        if (state.sourceURLs && state.sourceURLs.has(url)) return
        const prev = best.get(card)
        const width = img.naturalWidth || 0
        if (!prev || width > prev.width) best.set(card, { url, img, width })
      })
    })
    const seen = new Set()
    const images = []
    best.forEach(function (item) {
      if (seen.has(item.url)) return
      seen.add(item.url)
      images.push({ url: item.url, img: item.img })
    })
    return images
  }

  function resultsBusy(doc, state) {
    return newAssistants(doc, state).some(busy)
  }

  function composerCleared(doc) {
    const comp = composer(doc)
    return !!(comp && normalize(readComposer(comp)) === '')
  }

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

  function freshAttachment(doc, baseline) {
    const comp = composer(doc)
    const scope = attachmentScope(comp, uploadEntry(doc, comp))
    if (!scope) return false
    const fresh = all(scope, 'img').filter(function (img) {
      return visible(img) && img.complete && img.naturalWidth > 0 && baseline && !baseline.has(img)
    })
    return fresh.length > 0 && !busy(scope)
  }

  function captchaEvidence(el) {
    if (!el) return false
    const identity = ((el.className || '') + ' ' + (el.id || '') + ' ' + ((el.getAttribute && el.getAttribute('data-testid')) || '')).toString()
    if (/captcha|geetest|yidun|turing|verification/i.test(identity)) return true
    if (el.tagName === 'IFRAME') return /captcha|verify|geetest|turing/i.test((el.getAttribute && el.getAttribute('src')) || '')
    return CAPTCHA_TEXT.test(String(el.innerText || '').slice(0, 1000))
  }

  const CAPTCHA_SEL = [
    '#captcha_verify_image',
    'iframe[src*="captcha"]', 'iframe[src*="verify"]', 'iframe[src*="geetest"]',
    '[class*="captcha"]', '[class*="geetest"]', '[class*="verification"]',
  ]

  function detectCaptcha(doc) {
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

  function describeCaptcha(root) {
    const text = String(root.innerText || '')
    let kind = 'unknown'
    if (/滑块|滑动|slider/i.test(text) || all(root, '[class*="slider"]').length) kind = 'slider'
    else if (all(root, 'canvas').length > 1 || /九宫|点选/.test(text)) kind = 'grid'
    return { blocked: true, kind, root }
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
    doubaoHost,
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
    dismissClientPromo,
    mark,
    resultImages,
    resultsBusy,
    composerCleared,
    sendAccepted,
    assignFiles,
    freshAttachment,
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
