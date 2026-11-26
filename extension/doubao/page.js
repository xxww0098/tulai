// 豆包页面动作。选择器沿用 internal/strategy/js/ego.js 里已经对着 doubao.com 验证过的锚点：
// 上传入口 button[data-testid="upload_file_button"]，二级菜单全文「上传文件或图片」，
// 结果必须落在 [data-testid="mdbox_image"] 里。用户消息上的 [data-testid="message_image_content"]
// 不是生成结果。发送按钮与千问相同（chat_input_send_button / 发送）。
// 上传优先找输入区旁边的 input[type=file]（可以是隐藏的），用 DataTransfer 赋值。
// 不点击单独的「上传文件或图片」，否则会弹出系统文件框。
// 生成卡片里常有一张 1536×1536 的空 SVG（data:image/svg+xml 或 blob）占位，complete 且 naturalWidth 已经大于 0。
// 这层壳不是结果。要等 http(s) 实图，或同一张卡片上的 srcset / data-src / picture / 背景图 / 下载链接。
// 内容脚本读不到页面的 React fiber。本文件不碰 chrome.*，方便用 node 对锚点做夹具测试。
globalThis.TULAIPage = (function () {
  const USER_Q = '[data-message-author-role="user"],[data-message-role="user"],[data-role="user"],[data-author="user"],[data-testid="send_message"],[data-testid="message_text_user"]'
  const ASSISTANT_Q = '[data-message-author-role="assistant"],[data-message-role="assistant"],[data-role="assistant"],[data-author="assistant"],[data-testid="receive_message"]'
  const CAPTCHA_TEXT = /人机验证|安全验证|滑动验证|滑块验证|请完成.{0,30}验证|请(?:输入|填写)(?:下方|图中|图片|图形)?验证码|完成下方验证|(?:complete|solve|enter).{0,20}\bcaptcha\b|verify (?:that )?you are human|human verification|security verification/i
  const rawURLs = new Map()

  function rawImageKey(raw) {
    try {
      const url = new URL(raw)
      if (url.protocol !== 'https:' || !/(^|\.)byteimg\.com$/i.test(url.hostname)) return ''
      return url.pathname.split('~tplv-')[0]
    } catch (e) { return '' }
  }

  function acceptRawImages(pairs) {
    if (!Array.isArray(pairs)) return
    pairs.slice(0, 2048).forEach(function (item) {
      const key = rawImageKey(item && item.source)
      if (key && key === rawImageKey(item.raw)) {
        if (rawURLs.size >= 2048 && !rawURLs.has(key)) rawURLs.delete(rawURLs.keys().next().value)
        rawURLs.set(key, item.raw)
      }
    })
  }

  const RAW_EVENT = '__pb_raw_images__'

  function handleRawImagesEvent(event) {
    if (!event) return
    let pairs = event.detail
    if (typeof pairs === 'string') {
      try {
        pairs = JSON.parse(pairs)
      } catch (e) {
        return
      }
    }
    acceptRawImages(pairs)
  }

  if (typeof document !== 'undefined' && document && typeof document.addEventListener === 'function') {
    document.addEventListener(RAW_EVENT, handleRawImagesEvent)
  }

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
    if (style.display === 'none' || style.visibility === 'hidden') return false
    if (style.opacity !== '' && style.opacity != null && parseFloat(style.opacity) <= 0.05) return false
    return true
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
    const slots = all(doc, '[data-testid="chat_input_input"],[data-testid="chat_input"]')
    for (let i = slots.length - 1; i >= 0; i--) {
      const found = all(slots[i], 'textarea,[contenteditable="true"],input[type="text"]').filter(enabled).pop()
      if (found) return found
    }
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

  function isLockedChild(node) {
    if (!node || node.nodeType !== 1) return false
    if (node.getAttribute && (node.getAttribute('contenteditable') === 'false' || node.getAttribute('data-testid') === 'skill_input_exit_button')) return true
    return all(node, '[contenteditable="false"],[data-testid="skill_input_exit_button"]').length > 0
  }

  function setEditableWithChips(comp, doc, text) {
    const kids = Array.from(comp.childNodes || [])
    let lastLocked = null
    let lastLockedIdx = -1
    const unlocked = []
    for (let i = 0; i < kids.length; i++) {
      if (isLockedChild(kids[i])) {
        lastLocked = kids[i]
        lastLockedIdx = i
      } else {
        unlocked.push({ node: kids[i], index: i })
      }
    }
    const unlockedBefore = unlocked.some(function (item) { return item.index < lastLockedIdx })
    let inserted = false
    if (!unlockedBefore) {
      try {
        const sel = doc && doc.getSelection && doc.getSelection()
        const range = doc && doc.createRange && doc.createRange()
        if (sel && range) {
          if (lastLocked) range.setStartAfter(lastLocked)
          else range.selectNodeContents(comp)
          range.setEnd(comp, kids.length)
          sel.removeAllRanges()
          sel.addRange(range)
          inserted = !!(doc.execCommand && doc.execCommand('insertText', false, text))
        }
      } catch (e) {
        inserted = false
      }
    }
    if (!inserted) {
      for (let i = 0; i < unlocked.length; i++) {
        const node = unlocked[i].node
        if (typeof comp.removeChild === 'function') {
          try { comp.removeChild(node) } catch (e) {}
        } else if (Array.isArray(comp.childNodes)) {
          const idx = comp.childNodes.indexOf(node)
          if (idx !== -1) comp.childNodes.splice(idx, 1)
          if (Array.isArray(comp.children)) {
            const cidx = comp.children.indexOf(node)
            if (cidx !== -1) comp.children.splice(cidx, 1)
          }
        }
      }
      const textNode = doc && typeof doc.createTextNode === 'function'
        ? doc.createTextNode(text)
        : { nodeType: 3, nodeValue: text }
      if (typeof comp.appendChild === 'function') comp.appendChild(textNode)
      else if (Array.isArray(comp.childNodes)) comp.childNodes.push(textNode)
    }
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
    const doc = comp.ownerDocument || (typeof document !== 'undefined' ? document : null)
    if (all(comp, '[contenteditable="false"],[data-testid="skill_input_exit_button"]').length > 0) {
      setEditableWithChips(comp, doc, text)
    } else {
      try {
        const sel = doc && doc.getSelection && doc.getSelection()
        const range = doc && doc.createRange && doc.createRange()
        if (sel && range) {
          range.selectNodeContents(comp)
          sel.removeAllRanges()
          sel.addRange(range)
        }
        if (!doc || !doc.execCommand || !doc.execCommand('insertText', false, text)) comp.textContent = text
      } catch (e) {
        comp.textContent = text
      }
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
  // 2026-11 豆包图像生成模式移除 upload_file_button testid，改为与 upload-file-input 同父容器的圆按钮。
  function uploadEntry(doc, comp) {
    for (let scope = comp && comp.parentElement; scope && scope !== doc.body; scope = scope.parentElement) {
      const found = all(scope, 'button[data-testid="upload_file_button"],button[aria-label="添加附件"]').filter(enabled)[0]
      if (found) return found
    }
    for (let scope = comp && comp.parentElement; scope && scope !== doc.body; scope = scope.parentElement) {
      const inputs = fileInputs(scope)
      for (let i = 0; i < inputs.length; i++) {
        const wrap = inputs[i].parentElement
        if (!wrap || wrap === doc.body) continue
        const peer = all(wrap, 'button,[role="button"]').filter(enabled)[0]
        if (peer) return peer
      }
    }
    return null
  }

  function attachmentScope(comp, entry) {
    if (!comp) return null
    const surface = closest(comp, '.guidance-input-surface')
    if (surface && (!entry || surface.contains(entry))) return surface
    const chatInput = closest(comp, '[data-testid="chat_input"]')
    if (chatInput && (!entry || chatInput.contains(entry))) return chatInput
    const body = closest(comp, '[data-chat-input-body]')
    if (body && (!entry || body.contains(entry))) return body
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

  function imageModeScope(doc) {
    const comp = composer(doc)
    return closest(comp, '[data-testid="chat_input"]') || attachmentScope(comp, uploadEntry(doc, comp))
  }

  const SIBLING_SKILL_RE = /^(PPT\s*生成|帮我写作|视频生成|录音转写|更多技能|更多|技能|数据分析|AI\s*搜索|音乐生成|图像编辑)$/

  function hasSiblingSkillButton(scope, exclude) {
    return all(scope, 'button,[role="button"]').some(function (el) {
      return el !== exclude && visible(el) && SIBLING_SKILL_RE.test(normalize(el.innerText))
    })
  }

  function skillToolbarScope(doc) {
    const base = imageModeScope(doc)
    if (!base) return null
    for (let node = base, depth = 0; node && node !== doc.body && depth <= 3; node = node.parentElement, depth++) {
      if (depth > 0 && all(node, USER_Q + ',' + ASSISTANT_Q + ',nav,aside').length) break
      if (hasSiblingSkillButton(node, null)) return node
    }
    return base
  }

  const SKILL_TOOLBAR_SEL = '[data-testid="guidance-skill-bar"],[data-testid="skill_bar_button_3"],[data-skill-id="skill_bar_button_3"],[data-component-type="skill-item"],[data-input-engine-action-source="actionbar"],[class*="deterministic-overflow-list"],[class*="overflow-visible-item"]'

  function inSkillToolbar(el, comp) {
    if (closest(el, SKILL_TOOLBAR_SEL) || all(el, SKILL_TOOLBAR_SEL).length) {
      return true
    }
    const doc = (el && el.ownerDocument) || null
    const activeComp = comp !== undefined ? comp : (doc ? composer(doc) : null)
    const body = (doc && doc.body) || null
    for (let bar = el && el.parentElement, depth = 0; bar && bar !== body && depth <= 8; bar = bar.parentElement, depth++) {
      if (activeComp && bar.contains && bar.contains(activeComp)) break
      if (bar.matches && bar.matches('[data-testid="chat_input"],.guidance-input-surface,.guidance-input-content')) break
      if (all(bar, USER_Q + ',' + ASSISTANT_Q + ',nav,aside').length) break
      if (hasSiblingSkillButton(bar, el)) return true
    }
    return false
  }

  function openSkillPopups(doc) {
    return all(doc, '[role="menu"][data-state="open"],[role="listbox"][data-state="open"],[data-radix-menu-content],[data-slot="dropdown-menu-content"],[data-slot="popover-content"],[data-radix-popper-content-wrapper]').filter(function (m) {
      if (!visible(m)) return false
      if (m.getAttribute && (m.getAttribute('data-state') === 'closed' || m.getAttribute('role') === 'tooltip')) return false
      return true
    })
  }

  function imageModeControl(doc) {
    const scope = skillToolbarScope(doc) || imageModeScope(doc)
    const direct = all(scope, '[data-testid="skill_bar_button_3"],[data-skill-id="skill_bar_button_3"]').find(enabled)
    if (direct) return direct
    const bars = all(scope, '[data-testid="guidance-skill-bar"]')
    for (let i = 0; i < bars.length; i++) {
      const btn = all(bars[i], 'button').find(function (el) {
        return enabled(el) && exactText(el, '图像生成')
      })
      if (btn) return btn
    }
    const item = all(scope, '[data-component-type="skill-item"]').find(function (el) {
      return enabled(el) && exactText(el, '图像生成')
    })
    if (item) return item
    if (scope && hasSiblingSkillButton(scope, null)) {
      const btn = all(scope, 'button,[role="button"]').find(function (el) {
        return enabled(el) && exactText(el, '图像生成') && !closest(el, '[data-testid="skill_input_exit_button"]')
      })
      if (btn) return btn
    }
    const menus = openSkillPopups(doc)
    for (let i = 0; i < menus.length; i++) {
      const byId = all(menus[i], '[data-testid="skill_bar_button_3"],[data-skill-id="skill_bar_button_3"]').find(enabled)
      if (byId) return byId
      const entry = all(menus[i], 'button,[role="menuitem"],[role="option"],[data-component-type="skill-item"],div,span').find(function (el) {
        if (!enabled(el)) return false
        if (exactText(el, '图像生成')) {
          if ((el.tagName === 'DIV' || el.tagName === 'SPAN') && (!el.getAttribute || !el.getAttribute('role')) && el.children && el.children.length > 2) return false
          return true
        }
        return !!(el.matches && el.matches('button,[role="menuitem"],[role="option"],[data-component-type="skill-item"]')) &&
          all(el, 'span,div,p').some(function (child) { return visible(child) && exactText(child, '图像生成') })
      })
      if (entry) return entry
    }
    return null
  }

  function imageModeActive(doc) {
    const scope = imageModeScope(doc)
    const comp = composer(doc)
    // 2026-11 起已选技能在输入区挂固定 testid 的退出按钮，文本就是技能名。
    if (all(scope, '[data-testid="skill_input_exit_button"]').some(function (el) {
      return visible(el) && exactText(el, '图像生成')
    })) return true
    if (ratioControl(doc)) return true
    return all(scope, 'button,[role="button"],span,div').some(function (el) {
      if (!visible(el) || !exactText(el, '图像生成')) return false
      if (el.getAttribute('aria-pressed') === 'true' || el.getAttribute('aria-checked') === 'true' || el.getAttribute('aria-selected') === 'true') return true
      // 普通工具栏始终有同名入口，只有输入区里的已选模式标签才算切换成功。
      return !inSkillToolbar(el, comp)
    })
  }

  // 2026-11 实测：冷加载页面点击「图像生成」后，输入区要 2 秒多才重绘出已选标签；
  // 页面还没挂载完时第一次点击也会被吞掉。默认窗口 12 秒，未生效时每 3.5 秒补点
  // 一次（最多 3 次），每轮先查已选证据，已经生效就不再点。失败夹具传短 timeoutMs。
  function hasVisibleLoginButton(doc) {
    return all(doc, 'button,[role="button"],a').some(function (el) {
      return enabled(el) &&
        !closest(el, '[data-testid="send_message"],[data-testid="receive_message"]') &&
        /^(登录|扫码登录|立即登录|登录\/注册)$/.test(String(el.innerText || '').trim())
    })
  }

  // 2026-11 豆包新增「对话 / 工作（云电脑）」模式切换：处于「工作」模式时输入栏隐藏
  // 「图像生成」技能栏，需要先切回「对话」模式才能展示并点击「图像生成」。
  async function switchBackToChatMode(doc, allowOpen) {
    const scope = imageModeScope(doc)
    const modeBtn = all(scope, '[data-testid="chat_input_action_mode"]').find(enabled)
    if (!modeBtn) return ''
    const modeText = normalize(modeBtn.innerText)
    if (!modeText || modeText === '对话') return ''
    const menus = openSkillPopups(doc)
    for (let i = 0; i < menus.length; i++) {
      const chatItem = all(menus[i], '[role="menuitem"],[role="option"],button').find(function (el) {
        return enabled(el) && exactText(el, '对话')
      })
      if (chatItem) return (await pressMenuControl(chatItem)) ? 'selected' : ''
    }
    if (!allowOpen) return ''
    const topChatTab = all(doc, 'button,[role="tab"],[role="button"]').find(function (el) {
      if (el === modeBtn || !enabled(el) || !exactText(el, '对话')) return false
      if (closest(el, USER_Q + ',' + ASSISTANT_Q + ',nav,aside,[data-testid="chat_input"]')) return false
      const parent = el.parentElement
      if (!parent) return false
      return all(parent, 'button,[role="tab"],[role="button"]').some(function (sib) {
        return sib !== el && visible(sib) && exactText(sib, '工作')
      })
    })
    if (topChatTab) return (await pressMenuControl(topChatTab)) ? 'opened' : ''
    return (await pressMenuControl(modeBtn)) ? 'opened' : ''
  }

  async function ensureImageMode(doc, cancelled, timeoutMs) {
    const deadline = Date.now() + (timeoutMs > 0 ? timeoutMs : 12000)
    let clicks = 0
    let lastClick = 0
    let moreClicks = 0
    let lastMoreClick = 0
    let modeSwitchClicks = 0
    let lastModeSwitch = 0
    let promoDismissed = false
    while (Date.now() < deadline) {
      if (cancelled && cancelled()) return { error: '切换图像生成模式已取消', code: 'E_CANCELLED' }
      if (loginWall(doc)) return { error: '请先登录平台账号', code: 'E_NOT_LOGGED_IN' }
      if (imageModeActive(doc)) return { ok: true }
      if (!promoDismissed) {
        const promoClose = dismissClientPromo(doc, composer(doc))
        if (promoClose) {
          promoDismissed = true
          await pressMenuControl(promoClose)
          await new Promise(function (resolve) { setTimeout(resolve, 120) })
          continue
        }
      }
      const control = imageModeControl(doc)
      if (control && enabled(control) && clicks < 3 && (clicks === 0 || Date.now() - lastClick >= 3500)) {
        if (!await pressMenuControl(control)) return { error: '无法点击「图像生成」，未开始任务', code: 'E_SELECTOR_MISS' }
        clicks++
        lastClick = Date.now()
      } else if (!control) {
        const allowOpenMode = modeSwitchClicks < 2 && (modeSwitchClicks === 0 || Date.now() - lastModeSwitch >= 2500)
        const switched = await switchBackToChatMode(doc, allowOpenMode)
        if (switched) {
          if (switched === 'opened') {
            modeSwitchClicks++
            lastModeSwitch = Date.now()
          }
          await new Promise(function (resolve) { setTimeout(resolve, 120) })
          continue
        }
        if (moreClicks < 2 && (moreClicks === 0 || Date.now() - lastMoreClick >= 2500)) {
          const toolbar = skillToolbarScope(doc)
          const more = all(toolbar, '[data-testid="skill_bar_button_more"]').find(enabled) ||
            all(toolbar, 'button,[role="button"]').find(function (el) {
              return enabled(el) && /^(更多技能|更多|技能)$/.test(normalize(el.innerText)) &&
                (!el.getAttribute || el.getAttribute('data-testid') !== 'general-agent-skill-button')
            })
          if (more) {
            moreClicks++
            lastMoreClick = Date.now()
            await pressMenuControl(more)
          }
        }
      }
      await new Promise(function (resolve) { setTimeout(resolve, 40) })
    }
    if (!clicks && !all(doc, '[data-testid="chat_header_avatar_button"]').some(visible) && hasVisibleLoginButton(doc)) {
      return { error: '请先登录平台账号', code: 'E_NOT_LOGGED_IN' }
    }
    return { error: clicks ? '点击「图像生成」后模式未生效，未开始任务' : '未找到「图像生成」入口，未开始任务', code: 'E_SELECTOR_MISS' }
  }

  // 图像模式的「比例」入口（触发按钮文本形如「自动比例」「比例 自动」「比例 4:3」「3:4」）。
  function ratioLabelText(el) {
    if (!el) return ''
    return normalize(el.innerText || (el.getAttribute && el.getAttribute('aria-label')) || '')
  }

  function ratioControl(doc) {
    const scope = imageModeScope(doc)
    const candidates = all(scope, 'button,[role="button"],[aria-haspopup="menu"],[aria-haspopup="dialog"],[aria-haspopup="listbox"]').filter(function (el) {
      if (!enabled(el) || closest(el, USER_Q + ',' + ASSISTANT_Q)) return false
      const text = ratioLabelText(el)
      if (!text) return false
      if (/^比例/.test(text)) {
        const rest = text.slice(2).trim()
        return (el.getAttribute && el.getAttribute('aria-haspopup') !== null) || /^(自动|\d+:\d+)$/.test(rest)
      }
      if (/^(自动比例|比例\s*自动)$/.test(text)) return true
      const hasPopup = el.getAttribute && el.getAttribute('aria-haspopup') !== null
      const meta = (el.getAttribute && ((el.getAttribute('data-testid') || '') + ' ' + (el.getAttribute('aria-label') || ''))) || ''
      return (hasPopup || /ratio|比例/i.test(meta)) && /^\d+:\d+$/.test(text)
    })
    if (!candidates.length) return null
    return candidates.find(function (el) { return el.getAttribute && el.getAttribute('aria-haspopup') !== null }) || candidates[0]
  }

  // 当前已选比例，如 "4:3"；「自动」或读不到时返回 ""。
  function ratioValue(el) {
    const text = ratioLabelText(el)
    if (/^(自动比例|比例\s*自动|自动)$/.test(text)) return ''
    const m = /^(?:比例\s*)?(\d+:\d+)$/.exec(text)
    return m ? m[1] : ''
  }

  function isRatioAuto(el) {
    return /^(自动比例|比例\s*自动|自动)$/.test(ratioLabelText(el))
  }

  function ratioMatches(el, want) {
    return want === '自动' ? isRatioAuto(el) : ratioValue(el) === want
  }

  function isRatioOptionText(text) {
    return text === '自动' || text === '自动比例' || /^\d+:\d+$/.test(text)
  }

  function ratioOptionEnabled(el) {
    if (!enabled(el)) return false
    if (styleOf(el).pointerEvents === 'none') return false
    if (closest(el, '[data-state="closed"]')) return false
    return true
  }

  // 从比例浮层（传统下拉菜单或 2×4 卡片弹窗）里提取所有可选比例项（含 button / role / div 卡片）。
  function ratioMenuItems(menu) {
    if (!menu) return []
    const semantic = all(menu, 'button,[role="menuitemradio"],[role="menuitemcheckbox"],[role="menuitem"],[role="option"],[role="radio"],[role="button"]').filter(function (el) {
      return ratioOptionEnabled(el) && isRatioOptionText(normalize(el.innerText))
    })
    if (semantic.length >= 2) return semantic
    const leaves = all(menu, 'div,li,label,span').filter(function (el) {
      if (!ratioOptionEnabled(el)) return false
      const text = normalize(el.innerText)
      if (!isRatioOptionText(text)) return false
      return !all(el, 'div,li,label,span,button').some(function (child) {
        return child !== el && isRatioOptionText(normalize(child.innerText))
      })
    })
    const cards = []
    for (let i = 0; i < leaves.length; i++) {
      let target = leaves[i]
      const label = normalize(target.innerText)
      while (target.parentElement && target.parentElement !== menu) {
        const parent = target.parentElement
        if (!ratioOptionEnabled(parent) || normalize(parent.innerText) !== label) break
        target = parent
      }
      if (cards.indexOf(target) === -1) cards.push(target)
    }
    return cards.length ? cards : semantic
  }

  function isRatioMenuContainer(menu) {
    if (!menu || !visible(menu)) return false
    if (menu.getAttribute && (menu.getAttribute('data-state') === 'closed' || menu.getAttribute('role') === 'tooltip')) return false
    if (styleOf(menu).pointerEvents === 'none') return false
    if (closest(menu, USER_Q + ',' + ASSISTANT_Q + ',[data-state="closed"]')) return false
    const items = ratioMenuItems(menu)
    const auto = items.some(function (el) {
      const text = normalize(el.innerText)
      return text === '自动' || text === '自动比例'
    })
    const pairs = items.some(function (el) { return /^\d+:\d+$/.test(normalize(el.innerText)) })
    return auto && pairs
  }

  // 打开中的比例浮层：兼容 Radix DropdownMenu（role=menu/listbox）与 2026-11 新版 2×4 卡片 Popover（role=dialog / popover-content / popper wrapper / 浮动 div）。
  function ratioMenu(doc) {
    const primary = all(doc, '[role="menu"],[role="listbox"],[role="dialog"],[data-radix-menu-content],[data-slot="dropdown-menu-content"],[data-slot="popover-content"],[data-radix-popper-content-wrapper],[popover]').filter(isRatioMenuContainer)
    let matches = primary
    if (!matches.length) {
      matches = all(doc, 'div').filter(function (el) {
        const cls = String((el && el.className) || '')
        if (!/popover|popup|dropdown|portal|menu|ratio/i.test(cls)) return false
        return isRatioMenuContainer(el)
      })
    }
    if (!matches.length) return null
    // 嵌套包裹层（如 popper wrapper -> popover-content）优先取最内层面板。
    return matches.find(function (candidate) {
      return !matches.some(function (other) { return other !== candidate && candidate.contains && candidate.contains(other) })
    }) || matches[0]
  }

  function closeRatioMenu(doc, control) {
    try {
      (doc || document).dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    } catch (e) {}
    if (ratioMenu(doc) && control) {
      try { activate(control) } catch (e) {}
    }
  }

  async function pressMenuControl(el) {
    if (!el) return false
    if (typeof window !== 'undefined' && typeof window.tulaiClick === 'function') {
      try {
        if (await window.tulaiClick(el)) return true
      } catch (e) {}
    }
    return activate(el)
  }

  // 把图像模式的输出比例设成 ratio（如 "4:3" 或 "自动"）。浮层选项来自 2026-11 的豆包页面：
  // 自动 / 9:16 / 2:3 / 3:4 / 1:1 / 4:3 / 3:2 / 16:9。选中与否以触发按钮标签为准。
  // 工具栏随 React 重绘，节点可能被换掉：菜单没开就重取按钮再点；点完只看标签确认。
  async function setOutputRatio(doc, ratio, cancelled, timeoutMs) {
    const raw = normalize(ratio)
    const want = raw === '自动比例' ? '自动' : raw
    if (want !== '自动' && !/^\d+:\d+$/.test(want)) return { error: '比例格式应为 4:3', code: 'E_SELECTOR_MISS' }
    const deadline = Date.now() + (timeoutMs > 0 ? timeoutMs : 12000)
    let control = null
    while (Date.now() < deadline) {
      if (cancelled && cancelled()) return { error: '设置比例已取消', code: 'E_CANCELLED' }
      control = ratioControl(doc)
      if (control) break
      await new Promise(function (resolve) { setTimeout(resolve, 40) })
    }
    if (!control) return { error: '未找到「比例」入口', code: 'E_SELECTOR_MISS' }
    if (ratioMatches(control, want)) return { ok: true, already: true }
    let lastError = '比例 ' + want + ' 未生效'
    for (let round = 0; round < 3 && Date.now() < deadline; round++) {
      if (cancelled && cancelled()) { closeRatioMenu(doc, control); return { error: '设置比例已取消', code: 'E_CANCELLED' } }
      control = ratioControl(doc) || control
      if (control && ratioMatches(control, want)) {
        if (ratioMenu(doc)) closeRatioMenu(doc, control)
        return { ok: true }
      }
      let menu = ratioMenu(doc)
      let lastOpen = 0
      const openUntil = Math.min(deadline, Date.now() + 2600)
      while (!menu && Date.now() < openUntil) {
        if (cancelled && cancelled()) { closeRatioMenu(doc, control); return { error: '设置比例已取消', code: 'E_CANCELLED' } }
        control = ratioControl(doc) || control
        if (control && ratioMatches(control, want)) {
          if (ratioMenu(doc)) closeRatioMenu(doc, control)
          return { ok: true }
        }
        if (lastOpen === 0 || Date.now() - lastOpen >= 1200) {
          if (!await pressMenuControl(control)) return { error: '无法点击「比例」', code: 'E_SELECTOR_MISS' }
          lastOpen = Date.now()
        }
        await new Promise(function (resolve) { setTimeout(resolve, 40) })
        menu = ratioMenu(doc)
      }
      if (!menu) {
        lastError = '「比例」菜单未出现'
        continue
      }
      const readyUntil = Math.min(deadline, Date.now() + 150)
      while (Date.now() < readyUntil) {
        const op = parseFloat(styleOf(menu).opacity)
        if (isNaN(op) || op > 0.5) break
        await new Promise(function (resolve) { setTimeout(resolve, 20) })
      }
      const item = ratioMenuItems(menu).find(function (el) {
        const text = normalize(el.innerText)
        return want === '自动' ? (text === '自动' || text === '自动比例') : text === want
      })
      if (!item) {
        closeRatioMenu(doc, control)
        return { error: '「比例」菜单没有 ' + want, code: 'E_SELECTOR_MISS' }
      }
      if (!await pressMenuControl(item)) {
        closeRatioMenu(doc, control)
        return { error: '无法点击比例 ' + want, code: 'E_SELECTOR_MISS' }
      }
      const confirmUntil = Math.min(deadline, Date.now() + 1800)
      while (Date.now() < confirmUntil) {
        if (cancelled && cancelled()) { closeRatioMenu(doc, ratioControl(doc) || control); return { error: '设置比例已取消', code: 'E_CANCELLED' } }
        const again = ratioControl(doc)
        if (again && ratioMatches(again, want)) {
          if (ratioMenu(doc)) closeRatioMenu(doc, again)
          return { ok: true }
        }
        await new Promise(function (resolve) { setTimeout(resolve, 40) })
      }
      lastError = '比例 ' + want + ' 未生效'
      if (ratioMenu(doc)) closeRatioMenu(doc, ratioControl(doc) || control)
    }
    closeRatioMenu(doc, ratioControl(doc) || control)
    return { error: lastError, code: 'E_SELECTOR_MISS' }
  }

  function fileInputs(scope) {
    return all(scope, 'input[type="file"]').filter(function (input) {
      const accept = (input.getAttribute && input.getAttribute('accept')) || ''
      if (!accept) return true
      return /image|png|jpe?g|webp|gif|\*/i.test(accept)
    })
  }

  function pickFileInput(inputs) {
    if (!inputs.length) return null
    for (let i = inputs.length - 1; i >= 0; i--) {
      if (inputs[i].getAttribute && inputs[i].getAttribute('data-testid') === 'upload-file-input') return inputs[i]
    }
    return inputs[inputs.length - 1]
  }

  function fileInput(doc, comp) {
    const entry = uploadEntry(doc, comp)
    const scope = attachmentScope(comp, entry)
    const scoped = pickFileInput(fileInputs(scope))
    if (scoped) return scoped
    if (!comp) return null
    for (let node = comp.parentElement; node; node = node.parentElement) {
      const found = pickFileInput(fileInputs(node))
      if (found) return found
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
    if (all(doc, '[data-testid="to_login_button"]').some(enabled)) return true
    if (all(doc, 'input[type="password"]').some(visible)) return true
    if (all(doc, '[role="dialog"],[aria-modal="true"]').some(function (el) {
      return visible(el) && /扫码登录|手机号登录|短信登录|验证码登录|密码登录/.test(String(el.innerText || ''))
    })) return true
    if (composer(doc)) return false
    return all(doc, 'button,[role="button"],a').some(function (el) {
      return enabled(el) && /^(登录|扫码登录|立即登录)$/.test(String(el.innerText || '').trim())
    })
  }

  function removeButtons(doc, comp) {
    const scope = attachmentScope(comp, uploadEntry(doc, comp))
    if (!scope) return []
    return all(scope, '[data-testid="attachment-delete-btn"],[aria-label="delete" i],button,[role="button"]').filter(function (el) {
      if (!el || el.isConnected === false || el.disabled || (el.getAttribute && el.getAttribute('aria-disabled') === 'true')) return false
      if (el.getAttribute && (el.getAttribute('data-testid') === 'attachment-delete-btn' || /^delete$/i.test(String(el.getAttribute('aria-label') || '').trim()))) {
        // 豆包未悬停卡片时删除 <svg> 为 display:none，只要所属附件卡片可见即可派发点击移除。
        const card = closest(el, '[data-testid="attachment-image-card"]')
        return card ? visible(card) : visible(el)
      }
      return enabled(el) && /删除|移除|remove attachment/i.test(labelOf(el))
    })
  }

  // ego.js clientPromotionClose：豆包「立即下载电脑版」或「下载电脑版」弹窗旁唯一的「关闭」。
  // 范围里有输入框、消息、密码、登录或验证码时不关。
  function dismissClientPromo(doc, comp) {
    if (!doubaoHost(hostOf(doc))) return null
    function findClose(scope) {
      return all(scope, 'button,[role="button"]').find(function (button) {
        return enabled(button) && [button.getAttribute && button.getAttribute('aria-label'), button.getAttribute && button.getAttribute('title'), button.innerText].some(function (text) {
          return String(text || '').trim() === '关闭'
        })
      }) || null
    }
    const cta = all(doc, 'button,[role="button"],a').find(function (button) {
      return enabled(button) && [button.getAttribute && button.getAttribute('aria-label'), button.innerText].some(function (text) {
        return String(text || '').trim() === '立即下载电脑版'
      })
    })
    for (let scope = cta && cta.parentElement, depth = 0; scope && scope !== doc.body && depth < 7; scope = scope.parentElement, depth++) {
      if (comp && scope.contains && scope.contains(comp)) break
      if (matchesQuery(scope, USER_Q) || matchesQuery(scope, ASSISTANT_Q)) break
      if (all(scope, 'input[type="password"],[data-sitekey],iframe[src*="captcha"]').length) break
      const close = findClose(scope)
      if (close) return close
    }
    const dialogs = all(doc, '[role="dialog"],[aria-modal="true"]').filter(visible)
    for (let i = dialogs.length - 1; i >= 0; i--) {
      const dlg = dialogs[i]
      if (comp && dlg.contains && dlg.contains(comp)) continue
      if (matchesQuery(dlg, USER_Q) || matchesQuery(dlg, ASSISTANT_Q)) continue
      if (all(dlg, 'input[type="password"],[data-sitekey],iframe[src*="captcha"]').length) continue
      const text = String(dlg.innerText || '')
      if (/扫码登录|手机号登录|短信登录|验证码登录|密码登录|登录以解锁/.test(text)) continue
      if (!/下载电脑版|立即下载电脑版/.test(text)) continue
      const hasDownloadCTA = all(dlg, 'button,[role="button"],a').some(function (button) {
        return enabled(button) && /^(立即下载|立即下载电脑版|下载电脑版)$/.test(normalize(button.innerText))
      })
      if (!hasDownloadCTA) continue
      const close = findClose(dlg)
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
    const nodes = all(doc, assistant + ',' + user)
    if (nodes.length <= 1) return nodes
    const set = new Set(nodes)
    return nodes.filter(function (node) {
      let p = node && node.parentElement
      while (p) {
        if (set.has(p)) return false
        p = p.parentElement
      }
      return true
    })
  }

  function messageKey(node, user) {
    const role = matchesQuery(node, user) ? 'user' : 'assistant'
    const id = (node.getAttribute && (node.getAttribute('data-message-id') || node.getAttribute('data-id'))) || node.id || ''
    if (id) return role + ':' + id
    return role + ':' + hash(businessText(node))
  }

  function hasExpandAll(root) {
    if (!root) return false
    const raw = normalize((root.innerText || root.textContent) || '')
    if (/(?:^|\s)展开全部(?:\s|$)/.test(raw)) return true
    return all(root, 'button,[role="button"],span,div').some(function (el) {
      return normalize((el.innerText || el.textContent) || '') === '展开全部'
    })
  }

  function userContents(root) {
    const nodes = [root].concat(all(root, 'p,div,span'))
    const out = []
    nodes.forEach(function (node) {
      const text = businessText(node)
      if (text && out.indexOf(text) === -1) out.push(text)
      // 图像模式会给用户消息加「生成图片：」，回复仍须按原始提示词定位。
      const prompt = text.replace(/^生成图片[：:]\s*/, '')
      if (prompt && out.indexOf(prompt) === -1) out.push(prompt)
    })
    out.collapsed = hasExpandAll(root)
    return out
  }

  function bodiesMatchPrompt(bodies, prompt) {
    if (!Array.isArray(bodies) || !prompt) return false
    if (bodies.indexOf(prompt) !== -1) return true
    if (prompt.length < 40) return false
    const collapsed = Boolean(bodies.collapsed) || bodies.some(function (b) { return /展开全部$/.test(String(b || '').trim()) })
    if (!collapsed) return false
    return bodies.some(function (b) {
      const prefix = String(b || '')
        .replace(/\s*展开全部$/, '')
        .replace(/^生成图片[：:]\s*/, '')
        .replace(/(?:\.\.\.|…|\u2026)+$/, '')
        .trim()
      return prefix.length >= 40 && prompt.indexOf(prefix) === 0
    })
  }

  function userMatchesPrompt(node, prompt) {
    return bodiesMatchPrompt(userContents(node), prompt)
  }

  function busy(root) {
    if (!root) return false
    if (root.getAttribute && (root.getAttribute('data-streaming') === 'true' || root.getAttribute('data-finished') === 'false')) return true
    if (all(root, '[data-streaming="true"],[data-finished="false"]').length > 0) return true
    if (all(root, '[aria-busy="true"],[role="progressbar"]').some(visible)) return true
    return /正在生成|生成中|绘制中|\bGenerating\b/i.test(root.innerText || '')
  }

  function decodeURIComponentSafe(value) {
    try { return decodeURIComponent(value) } catch (e) { return String(value || '') }
  }

  // 空 SVG / blob 是豆包懒加载占位，naturalWidth 可以是 1536 但字节不是图片。
  // http(s) 实图、以及 data:image/(png|jpeg|webp|gif) 才算候选。
  function isPlaceholderURL(url) {
    const value = String(url || '').trim()
    if (!value) return true
    if (/^blob:/i.test(value)) return true
    const path = value.toLowerCase().split('#')[0].split('?')[0]
    if (/\.svg$/.test(path)) return true
    if (!/^data:/i.test(value)) return false
    if (/^data:image\/svg/i.test(value)) return true
    const decoded = decodeURIComponentSafe(value.slice(0, 2000))
    if (/<svg[\s>]/i.test(decoded)) return true
    return !/^data:image\/(png|jpe?g|webp|gif)(;|,)/i.test(value)
  }

  function urlsFromSrcset(value) {
    const out = []
    String(value || '').split(',').forEach(function (part) {
      const bits = part.trim().split(/\s+/)
      if (!bits[0]) return
      let width = 0
      const desc = bits[1] || ''
      if (/^\d+w$/i.test(desc)) width = parseInt(desc, 10) || 0
      else if (/^[\d.]+x$/i.test(desc)) width = Math.round((parseFloat(desc) || 0) * 1000)
      out.push({ url: bits[0], width: width })
    })
    return out
  }

  function pushCandidate(list, url, width) {
    const value = String(url || '').trim()
    if (!value) return
    const size = width > 0 ? width : 0
    for (let i = 0; i < list.length; i++) {
      if (list[i].url === value) {
        if (size > list[i].width) list[i].width = size
        return
      }
    }
    list.push({ url: value, width: size })
  }

  function urlsFromCSSValue(value) {
    const out = []
    const re = /url\(\s*(['"]?)(.*?)\1\s*\)/g
    const text = String(value || '')
    let match
    while ((match = re.exec(text))) {
      if (match[2]) out.push(match[2])
    }
    return out
  }

  function backgroundURLs(el) {
    const out = []
    function take(value) {
      urlsFromCSSValue(value).forEach(function (url) {
        if (out.indexOf(url) === -1) out.push(url)
      })
    }
    if (!el) return out
    if (el.style) take(el.style.backgroundImage || el.style.background || '')
    const computed = styleOf(el)
    take(computed && (computed.backgroundImage || computed.background))
    if (el.getAttribute) take(el.getAttribute('style'))
    return out
  }

  function imageCandidates(img) {
    const list = []
    if (!img) return list
    const displayed = String(img.currentSrc || img.src || (img.getAttribute && img.getAttribute('src')) || '')
    const natural = img.naturalWidth > 0 ? img.naturalWidth : 0
    function inherited(url) {
      if (!url || !(natural > 0) || isPlaceholderURL(url)) return 0
      if (url === displayed || isPlaceholderURL(displayed)) return natural
      return 0
    }
    function add(url, width) {
      pushCandidate(list, url, width > 0 ? width : inherited(url))
    }
    add(img.currentSrc, 0)
    add(img.src, 0)
    if (img.getAttribute) {
      add(img.getAttribute('src'), 0)
      urlsFromSrcset(img.getAttribute('srcset')).forEach(function (item) { add(item.url, item.width) })
      if (img.srcset) urlsFromSrcset(img.srcset).forEach(function (item) { add(item.url, item.width) })
      urlsFromSrcset(img.getAttribute('data-srcset')).forEach(function (item) { add(item.url, item.width) })
      ;['data-src', 'data-original', 'data-original-src', 'data-lazy-src', 'data-url', 'data-actualsrc', 'data-image-url', 'data-image'].forEach(function (name) {
        add(img.getAttribute(name), 0)
      })
    }
    const picture = closest(img, 'picture')
    if (picture) {
      all(picture, 'source').forEach(function (source) {
        urlsFromSrcset(source.getAttribute && source.getAttribute('srcset')).forEach(function (item) {
          add(item.url, item.width)
        })
        add(source.getAttribute && source.getAttribute('src'), 0)
      })
    }
    backgroundURLs(img).forEach(function (url) { add(url, 0) })
    return list
  }

  function imageURL(img) {
    const list = imageCandidates(img).filter(function (item) { return !isPlaceholderURL(item.url) })
    let best = null
    list.forEach(function (item) {
      if (!best || urlRank(item) > urlRank(best)) best = item
    })
    if (best) return best.url
    return String((img && (img.currentSrc || img.src)) || '')
  }

  function urlRank(item) {
    let rank = item.width || 0
    if (/^https?:/i.test(item.url)) rank += 1000000
    else if (/^data:image\/(png|jpe?g|webp|gif)/i.test(item.url)) rank += 1000
    else rank += 10000
    // 豆包缩略图 cthumb_lwm3 不如原图模板 cgen_lwm3。
    if (/cgen_lwm/i.test(item.url || '')) rank += 50000
    if (/cthumb_lwm/i.test(item.url || '')) rank -= 20000
    return rank
  }

  function isFetchableRasterURL(url) {
    if (isPlaceholderURL(url)) return false
    if (!/^https?:\/\//i.test(url || '')) return false
    if (/\.(png|jpe?g|webp|gif)(?:$|[?#])/i.test(url)) return true
    return /byteimg\.com|imagex|ocean-cloud-tos/i.test(url)
  }

  function imgsInCard(card) {
    const imgs = all(card, 'img')
    if (card && card.tagName === 'IMG' && imgs.indexOf(card) === -1) imgs.unshift(card)
    return imgs
  }

  function cardCandidates(card, state) {
    const out = []
    function add(url, width, img) {
      const value = String(url || '').trim()
      if (!value || isPlaceholderURL(value)) return
      if (state && state.sourceURLs && state.sourceURLs.has(value)) return
      for (let i = 0; i < out.length; i++) {
        if (out[i].url === value) {
          if (width > out[i].width) out[i].width = width
          if (!out[i].img && img) out[i].img = img
          return
        }
      }
      out.push({ url: value, width: width || 0, img: img || null })
    }
    const imgs = imgsInCard(card)
    imgs.forEach(function (img) {
      if (state && state.images && state.images.has(img)) return
      imageCandidates(img).forEach(function (item) { add(item.url, item.width, img) })
      if (img.parentElement && img.parentElement !== card) {
        backgroundURLs(img.parentElement).forEach(function (url) { add(url, 0, img) })
      }
    })
    backgroundURLs(card).forEach(function (url) { add(url, 0, imgs[0] || null) })
    all(card, 'a').forEach(function (el) {
      const href = el.getAttribute && (el.getAttribute('href') || el.getAttribute('data-href'))
      if (!isFetchableRasterURL(href)) return
      const wraps = el.contains && imgs.some(function (img) { return el.contains(img) })
      const label = ((el.getAttribute && el.getAttribute('aria-label')) || '') + ' ' + (el.innerText || '')
      const download = (el.getAttribute && el.getAttribute('download') != null) || /下载|原图/.test(label)
      if (wraps || download) add(href, 0, null)
    })
    return out
  }

  function chooseCardImage(card, state) {
    let chosen = null
    cardCandidates(card, state).forEach(function (item) {
      if (!chosen || urlRank(item) > urlRank(chosen)) chosen = item
    })
    return chosen
  }

  function mark(doc, prompt) {
    rawURLs.clear()
    const q = queries(doc)
    const comp = composer(doc)
    const entry = uploadEntry(doc, comp)
    const scope = attachmentScope(comp, entry)
    const messages = orderedMessages(doc, q.user, q.assistant)
    const images = new Set(all(doc, 'img'))
    const sourceURLs = new Set()
    if (scope) all(scope, 'img').forEach(function (img) {
      imageCandidates(img).forEach(function (item) { sourceURLs.add(item.url) })
    })
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
      anchorRawText: '',
      scope,
    }
  }

  function newAssistants(doc, state) {
    if (!state) return []
    const q = state.q || queries(doc)
    const messages = orderedMessages(doc, q.user, q.assistant)
    const prompt = state.prompt
    let anchor = null
    let anchorIdx = -1
    if (prompt && state.anchorConfirmed && state.anchor && state.anchor.isConnected !== false) {
      anchorIdx = messages.indexOf(state.anchor)
      if (anchorIdx !== -1) {
        const raw = String((state.anchor.innerText || state.anchor.textContent) || '')
        if (state.anchorRawText === raw || userMatchesPrompt(state.anchor, prompt)) {
          state.anchorRawText = raw
          anchor = state.anchor
        }
      }
    }
    if (!anchor && prompt) {
      const oldMatches = (state.userBodies || []).filter(function (bodies) { return bodiesMatchPrompt(bodies, prompt) }).length
      const matches = messages.filter(function (node) { return matchesQuery(node, q.user) && userMatchesPrompt(node, prompt) })
      if (matches.length > oldMatches) anchor = matches[oldMatches]
      if (!anchor && state.tail && state.tail.isConnected !== false) {
        const tailIndex = messages.indexOf(state.tail)
        if (tailIndex !== -1) anchor = matches.find(function (node) { return messages.indexOf(node) > tailIndex }) || null
      }
      if (anchor) {
        anchorIdx = messages.indexOf(anchor)
        state.anchorRawText = String((anchor.innerText || anchor.textContent) || '')
      }
    }
    state.anchor = anchor
    if (!anchor || anchorIdx === -1) return []
    state.anchorConfirmed = true
    let following = messages.slice(anchorIdx + 1)
    const nextUser = following.findIndex(function (node) { return matchesQuery(node, q.user) })
    if (nextUser !== -1) {
      state.followedByUser = true
      following = following.slice(0, nextUser)
    }
    return following.filter(function (node) { return matchesQuery(node, q.assistant) })
  }

  function scanAssistantCards(assistants, state) {
    const best = new Map()
    const pending = []
    const pendingSeen = new Set()
    assistants.forEach(function (message) {
      const seenCards = new Set()
      const chosenByCard = new Map()
      function chosenOf(card) {
        if (chosenByCard.has(card)) return chosenByCard.get(card)
        const chosen = chooseCardImage(card, state)
        chosenByCard.set(card, chosen)
        return chosen
      }
      function take(card) {
        if (!card || seenCards.has(card) || !message.contains(card)) return
        seenCards.add(card)
        const chosen = chosenOf(card)
        if (chosen) best.set(card, chosen)
      }
      all(message, 'img').forEach(function (img) {
        take(closest(img, '[data-testid="mdbox_image"]'))
      })
      const cards = all(message, '[data-testid="mdbox_image"]')
      cards.forEach(function (card) {
        take(card)
        if (!card || pendingSeen.has(card) || !message.contains(card)) return
        const chosen = chosenOf(card)
        if (chosen && chosen.url && !isPlaceholderURL(chosen.url) && isFetchableRasterURL(chosen.url)) return
        pendingSeen.add(card)
        pending.push(card)
      })
    })
    const seen = new Set()
    const images = []
    best.forEach(function (item) {
      if (seen.has(item.url)) return
      seen.add(item.url)
      images.push({ url: item.url, rawUrl: rawURLs.get(rawImageKey(item.url)) || '', img: item.img })
    })
    return { images, pending }
  }

  // 同一张 mdbox 里的大图和缩略图只留一张。空 SVG / blob 占位即使 naturalWidth 更大也丢掉，
  // 改留 http(s) 实图（或 srcset、data-src、picture、背景、下载链接上的地址）。
  // 不在 mdbox 里的图（包括用户消息上的 message_image_content）丢掉。
  function resultImages(doc, state) {
    return scanAssistantCards(newAssistants(doc, state), state).images
  }

  function isHiddenClass(el) {
    return !!(el && /(^|\s)!?hidden(\s|$)/.test(el.className || ''))
  }

  function isBusyForAssistants(doc, state, assistants) {
    if (assistants.some(busy)) return true
    if (state && state.anchorConfirmed) {
      const scope = imageModeScope(doc) || doc
      if (all(scope, '[data-testid="chat_input_local_break_button"]').some(function (el) {
        return visible(el) && !isHiddenClass(el)
      })) return true
    }
    return false
  }

  function resultsBusy(doc, state) {
    return isBusyForAssistants(doc, state, newAssistants(doc, state))
  }

  function computeReplyDone(isBusy, validImages, pending, state) {
    if (isBusy || !validImages.length) {
      if (state) {
        state.pendingSince = 0
        state.pendingSig = ''
      }
      return { done: false }
    }
    if (pending.length > 0 && state) {
      const sig = validImages.length + ':' + pending.length
      const now = Date.now()
      if (state.pendingSig !== sig || !state.pendingSince) {
        state.pendingSig = sig
        state.pendingSince = now
      }
      if (now - state.pendingSince < 15000) {
        return { done: false, pendingCards: pending.length }
      }
    } else if (state) {
      state.pendingSince = 0
      state.pendingSig = ''
    }
    return { done: true }
  }

  // 完成候选：这一轮回复里有实图，并且没有生成指示；若仍有待揭开的图片卡片，最多宽限 15 秒。
  // 重新生成/复制和声明张数在真实页面上经常没有。调用方再等地址稳住约 3 秒。
  function replyDone(doc, state) {
    const assistants = newAssistants(doc, state)
    const isBusy = isBusyForAssistants(doc, state, assistants)
    const scanned = scanAssistantCards(assistants, state)
    const images = scanned.images.filter(function (item) {
      return item && item.url && !isPlaceholderURL(item.url)
    })
    return computeReplyDone(isBusy, images, scanned.pending, state)
  }

  function assistantText(doc, state) {
    return newAssistants(doc, state).map(function (node) {
      return String((node && (node.innerText || node.textContent)) || '').replace(/\s+/g, ' ').trim()
    }).filter(Boolean).join('\n').slice(0, 800)
  }

  function scrollIntoView(el) {
    if (!el || typeof el.scrollIntoView !== 'function') return
    try {
      el.scrollIntoView({ block: 'center', inline: 'nearest' })
    } catch (e) {
      try { el.scrollIntoView() } catch (err) {}
    }
  }

  function nudgeImage(el) {
    const imgs = el && el.tagName === 'IMG' ? [el] : all(el, 'img')
    imgs.forEach(function (img) {
      if (!img) return
      const lazy = String((img.loading || (img.getAttribute && img.getAttribute('loading')) || '')).toLowerCase()
      if (lazy === 'lazy') {
        try {
          if (typeof img.setAttribute === 'function') img.setAttribute('loading', 'eager')
        } catch (e) {}
        try { img.loading = 'eager' } catch (e) {}
      }
      const url = imageURL(img)
      if (!url || isPlaceholderURL(url) || typeof img.decode !== 'function') return
      try {
        const job = img.decode()
        if (job && typeof job.catch === 'function') job.catch(function () {})
      } catch (e) {}
    })
  }

  // 豆包生成卡常先挂 1536 SVG 壳，真图 img 标着 loading=lazy。滚进视口并改成 eager 后才换成 http(s)。
  const REVEAL_DWELL_MS = 800
  function pendingRevealTargets(doc, state) {
    return scanAssistantCards(newAssistants(doc, state), state).pending
  }

  function revealPending(pending, state) {
    if (!state || !pending.length) return
    const now = Date.now()
    let idx = 0
    if (state.revealEl && now - (state.revealAt || 0) < REVEAL_DWELL_MS) {
      const held = pending.indexOf(state.revealEl)
      if (held !== -1) idx = held
    } else if (state.revealEl) {
      const held = pending.indexOf(state.revealEl)
      if (held !== -1) idx = (held + 1) % pending.length
    }
    const target = pending[idx]
    if (target !== state.revealEl) {
      state.revealEl = target
      state.revealAt = now
    }
    pending.forEach(nudgeImage)
    scrollIntoView(target)
  }

  function revealResults(doc, state) {
    if (!state) return
    revealPending(pendingRevealTargets(doc, state), state)
  }

  function pollResults(doc, state) {
    const assistants = newAssistants(doc, state)
    const scanned = scanAssistantCards(assistants, state)
    if (scanned.pending.length && state) {
      revealPending(scanned.pending, state)
    }
    const validImages = scanned.images.filter(function (item) {
      return item && item.url && !isPlaceholderURL(item.url)
    })
    const isBusy = isBusyForAssistants(doc, state, assistants)
    const signal = computeReplyDone(isBusy, validImages, scanned.pending, state)
    return {
      assistants,
      images: validImages,
      busy: isBusy,
      signal,
    }
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
    const oldMatches = (state.userBodies || []).filter(function (bodies) { return bodiesMatchPrompt(bodies, state.prompt) }).length
    const matches = messages.filter(function (node) { return matchesQuery(node, q.user) && userMatchesPrompt(node, state.prompt) })
    return matches.length > oldMatches
  }


  // HTMLElement.click() 只有 click，没有 pointerdown。豆包发送按钮和千问一样，需要走完 pointer 再 click，
  // 否则页面常常不收下这次发送，输入框也不清空。在元素中央 40% 范围内做微小拟人偏移。
  function pickClickPoint(el) {
    const rect = el && el.getBoundingClientRect ? el.getBoundingClientRect() : null
    if (!rect || !(rect.width > 0) || !(rect.height > 0)) return { x: 0, y: 0 }
    const jitterX = rect.width > 8 ? (Math.random() - 0.5) * rect.width * 0.4 : 0
    const jitterY = rect.height > 8 ? (Math.random() - 0.5) * rect.height * 0.4 : 0
    return {
      x: Math.round((rect.left + rect.width / 2 + jitterX) * 10) / 10,
      y: Math.round((rect.top + rect.height / 2 + jitterY) * 10) / 10,
    }
  }

  function dispatchUiEvent(el, type, coords) {
    if (!el || typeof el.dispatchEvent !== 'function') return null
    const pos = coords || pickClickPoint(el)
    const x = pos.x
    const y = pos.y
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
    const coords = pickClickPoint(el)
    const down = dispatchUiEvent(el, 'pointerdown', coords)
    dispatchUiEvent(el, 'mousedown', coords)
    dispatchUiEvent(el, 'pointerup', coords)
    dispatchUiEvent(el, 'mouseup', coords)
    return down
  }

  function activate(el) {
    if (!el) return false
    poke(el)
    try { el.click() } catch (e) { return false }
    return true
  }

  // SPA 页内点「新对话」，省去整页重载。只点非消息区、非新标签的显式新对话入口。
  function clickNewChat(doc) {
    const direct = all(doc, '[data-testid="create_conversation_button"],[data-testid*="new_chat"]').find(function (el) {
      return enabled(el) && !closest(el, USER_Q + ',' + ASSISTANT_Q) && el.getAttribute('target') !== '_blank'
    })
    if (direct) return activate(direct)
    const btn = all(doc, 'button,[role="button"],a').find(function (el) {
      if (!enabled(el) || closest(el, USER_Q + ',' + ASSISTANT_Q)) return false
      if (el.tagName === 'A' && el.getAttribute && el.getAttribute('target') === '_blank') return false
      const text = normalize(el.innerText || (el.getAttribute && el.getAttribute('aria-label')) || '')
      return text === '新对话'
    })
    return btn ? activate(btn) : false
  }

  function canMultiUpload(doc, comp) {
    const input = fileInput(doc, comp)
    return !!(input && input.multiple)
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
    if (busy(scope)) return false
    // 豆包收下文件后会先挂 attachment-image-card。卡内 blob 预览经常长期 naturalWidth=0，
    // 不能干等解码；可见的附件卡本身就表示源图已进输入区。
    const cards = all(scope, '[data-testid="attachment-image-card"]')
    if (cards.some(function (card) { return visible(card) && baseline && !baseline.has(card) })) return true
    const fresh = all(scope, 'img').filter(function (img) {
      return visible(img) && img.complete && img.naturalWidth > 0 && baseline && !baseline.has(img)
    })
    return fresh.length > 0
  }

  function attachmentNames(doc) {
    const comp = composer(doc)
    const scope = attachmentScope(comp, uploadEntry(doc, comp))
    return all(scope, '[data-testid="attachment-image-card"]').filter(visible).map(function (card) {
      return String(card.getAttribute('aria-label') || '')
    })
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

  // ---- 聊天历史记录删除 ----
  const RATE_LIMIT_RE = /过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests/i

  function conversationIdFromHref(href) {
    try {
      const u = new URL(String(href || ''), (typeof location !== 'undefined' && location && location.href) || 'https://www.doubao.com/chat/')
      const parts = u.pathname.split('/').filter(Boolean)
      const i = parts.indexOf('chat')
      return i >= 0 && parts[i + 1] ? parts[i + 1] : ''
    } catch (e) {
      return ''
    }
  }

  function hasRateLimitNotice(doc) {
    return all(doc, '[role="alert"],[role="dialog"],[class*="toast"],[class*="notice"]').some(function (el) {
      if (!visible(el)) return false
      const txt = normalize(el.innerText)
      return txt.length > 0 && txt.length <= 240 && RATE_LIMIT_RE.test(txt)
    })
  }

  function historyBlocked(doc) {
    if (loginWall(doc)) return { stopped: 'not_logged_in', error: '账号未登录' }
    if (detectCaptcha(doc).blocked) return { stopped: 'captcha', error: '触发安全验证' }
    if (hasRateLimitNotice(doc)) return { stopped: 'rate_limit', error: '触发操作频率限制' }
    return null
  }

  // 豆包真实 DOM 中 [data-testid="conversation-list-v2-project"] 是整个对话列表根容器
  // （同时包着 project-grouped-section「项目」与 conversation-section「最近」），
  // 因此只能排除 project-grouped-section，不能排除 conversation-list-v2-project。
  function inProjectSection(el) {
    return !!closest(el, '[data-testid="project-grouped-section"]')
  }

  function conversationItems(doc) {
    const root = (doc && doc.querySelector && doc.querySelector('[data-testid="chat_list_wrapper"]')) || doc
    const nodes = all(root, '[data-testid="conversation-list-v2-item"]')
    const seen = new Set()
    const out = []
    for (let i = 0; i < nodes.length; i++) {
      const el = nodes[i]
      if (!el || el.isConnected === false || inProjectSection(el)) continue
      const link = closest(el, 'a[href]')
      const id = normalize((el.getAttribute && el.getAttribute('data-conversation-id')) || '') ||
        conversationIdFromHref(link && link.getAttribute && link.getAttribute('href'))
      if (!id || seen.has(id)) continue
      seen.add(id)
      const moreBtn = (el.querySelector && (
        el.querySelector('button[data-testid="chat_list_item_more_button"]') ||
        el.querySelector('[data-testid="chat_list_item_more_button"]') ||
        el.querySelector('button[aria-haspopup="menu"]')
      )) || null
      const title = normalize(el.innerText || (el.getAttribute && el.getAttribute('title')) || '').slice(0, 80)
      out.push({ id, title, el, moreBtn, active: (el.getAttribute && el.getAttribute('data-conversation-active')) === 'true' })
    }
    return out
  }

  function sidebarVisible(doc) {
    const wrap = doc && doc.querySelector && (doc.querySelector('[data-testid="chat_list_wrapper"]') || doc.querySelector('[data-testid="flow_chat_sidebar"]'))
    return !!(wrap && visible(wrap))
  }

  async function ensureSidebarOpen(doc, timeoutMs) {
    if (sidebarVisible(doc)) return true
    const toggle = all(doc, 'button,[role="button"]').find(function (el) {
      if (!enabled(el)) return false
      const label = normalize(labelOf(el))
      return /展开侧边栏|打开侧边栏|显示侧边栏|侧边栏/.test(label) && !/收起|隐藏|关闭/.test(label)
    })
    if (!toggle) return sidebarVisible(doc)
    await pressMenuControl(toggle)
    const deadline = Date.now() + (timeoutMs > 0 ? timeoutMs : 2000)
    while (Date.now() < deadline) {
      if (sidebarVisible(doc)) return true
      await new Promise(function (resolve) { setTimeout(resolve, 40) })
    }
    return sidebarVisible(doc)
  }

  async function hoverEl(el) {
    if (!el) return false
    if (typeof window !== 'undefined' && typeof window.tulaiHover === 'function') {
      try {
        if (await window.tulaiHover(el)) return true
      } catch (e) {}
    }
    try {
      if (typeof el.dispatchEvent === 'function' && typeof MouseEvent === 'function') {
        el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, cancelable: true }))
        el.dispatchEvent(new MouseEvent('mouseenter', { bubbles: false, cancelable: false }))
      }
      if (typeof el.onmouseenter === 'function') el.onmouseenter()
      return true
    } catch (e) {
      return false
    }
  }

  function openActionMenus(doc) {
    return all(doc, '[role="menu"],[data-radix-menu-content],[data-slot="dropdown-menu-content"],[role="listbox"],[data-radix-popper-content-wrapper]').filter(function (m) {
      if (!visible(m)) return false
      if (m.getAttribute && (m.getAttribute('data-state') === 'closed' || m.getAttribute('role') === 'tooltip')) return false
      return true
    })
  }

  function findDeleteMenuItem(menu) {
    if (!menu) return null
    const candidates = all(menu, '[role="menuitem"],[role="option"],button,div,span').filter(function (el) {
      if (!enabled(el)) return false
      if ((el.tagName === 'DIV' || el.tagName === 'SPAN') && !el.getAttribute('role') && el.children && el.children.length > 2) return false
      const t = normalize(el.innerText)
      return /^(删除|删除对话|删除聊天|删除记录)$/.test(t) || (t.indexOf('删除') >= 0 && t.length <= 10 && !/取消|清除|上下文/.test(t))
    })
    return candidates.find(function (el) { return /^(删除|删除对话|删除聊天|删除记录)$/.test(normalize(el.innerText)) }) || candidates[0] || null
  }

  const DIALOG_SEL = '[role="dialog"],[role="alertdialog"],[data-radix-dialog-content],[data-slot="dialog-content"],[data-slot="alert-dialog-content"],.semi-modal,[class*="modal"]'

  function findConfirmDeleteButton(doc, oldDialogs) {
    const dialogs = all(doc, DIALOG_SEL).filter(function (d) {
      if (!visible(d)) return false
      if (d.getAttribute && d.getAttribute('data-state') === 'closed') return false
      return !oldDialogs || !oldDialogs.has(d)
    })
    for (let i = dialogs.length - 1; i >= 0; i--) {
      const dlg = dialogs[i]
      const btns = all(dlg, 'button,[role="button"]').filter(function (b) {
        if (!enabled(b)) return false
        const t = normalize(b.innerText)
        return /^(确认删除|删除对话|删除聊天|删除|确定|确认)$/.test(t)
      })
      const exact = btns.find(function (b) { return /^(确认删除|删除对话|删除聊天|删除)$/.test(normalize(b.innerText)) }) || btns[0]
      if (exact) return { dialog: dlg, button: exact }
    }
    return null
  }

  function closeMenus(doc) {
    try {
      (doc || document).dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    } catch (e) {}
  }

  async function waitRadixClosed(doc, ms) {
    const deadline = Date.now() + (ms > 0 ? ms : 500)
    while (Date.now() < deadline) {
      const closing = all(doc, '[role="menu"][data-state="closed"],[role="dialog"][data-state="closed"],[role="alertdialog"][data-state="closed"]')
      if (!closing.length) return
      await new Promise(function (resolve) { setTimeout(resolve, 25) })
    }
  }

  async function openDeleteItemFromTrigger(doc, trigger, deadline) {
    const oldMenus = new Set(openActionMenus(doc))
    if (!await pressMenuControl(trigger)) return { error: '无法点击更多菜单按钮' }
    const waitUntil = Math.min(deadline, Date.now() + 2500)
    while (Date.now() < waitUntil) {
      const menus = openActionMenus(doc).filter(function (m) { return !oldMenus.has(m) })
      for (let i = menus.length - 1; i >= 0; i--) {
        const delItem = findDeleteMenuItem(menus[i])
        if (delItem) return { menu: menus[i], item: delItem }
      }
      await new Promise(function (resolve) { setTimeout(resolve, 30) })
    }
    closeMenus(doc)
    return { error: '菜单中未找到删除项' }
  }

  async function deleteConversation(doc, convID, cancelled, timeoutMs) {
    const blocked = historyBlocked(doc)
    if (blocked) return Object.assign({ ok: false }, blocked)
    const currentID = conversationIdFromHref(typeof location !== 'undefined' && location ? location.href : '')
    const targetID = normalize(convID) || currentID
    if (!targetID) return { ok: false, notFound: true, error: '当前页面没有可删除的对话 ID' }
    const deadline = Date.now() + (timeoutMs > 0 ? timeoutMs : 10000)
    await waitRadixClosed(doc, 400)

    let delItem = null
    let title = ''
    let usedHeader = false
    const inSidebar = conversationItems(doc).find(function (it) { return it.id === targetID }) || null
    if (inSidebar) title = inSidebar.title

    // 当前页会话优先尝试顶部标题栏更多按钮，避免依赖侧边栏展开状态。
    if (targetID === currentID) {
      const headerMore = doc && doc.querySelector && doc.querySelector('button[data-testid="conversation_header_more_button"]')
      if (headerMore && enabled(headerMore)) {
        const opened = await openDeleteItemFromTrigger(doc, headerMore, deadline)
        if (opened.item) {
          delItem = opened.item
          usedHeader = true
        }
      }
    }

    if (!delItem) {
      await ensureSidebarOpen(doc, 2000)
      const item = conversationItems(doc).find(function (it) { return it.id === targetID }) || null
      if (!item) return { ok: false, notFound: true, error: '侧边栏未找到对话 ' + targetID }
      title = item.title || title
      if (!item.moreBtn) return { ok: false, error: '未找到对话更多按钮' }
      await hoverEl(item.el)
      await new Promise(function (resolve) { setTimeout(resolve, 70) })
      // 再将鼠标停到更多按钮中心，避免 tulaiClick 贝塞尔逼近路径上下偏移离开 32px 行导致失焦隐藏。
      await hoverEl(item.moreBtn)
      await new Promise(function (resolve) { setTimeout(resolve, 40) })
      let opened = await openDeleteItemFromTrigger(doc, item.moreBtn, deadline)
      if (!opened.item) {
        try { if (typeof item.moreBtn.focus === 'function') item.moreBtn.focus() } catch (e) {}
        await hoverEl(item.moreBtn)
        await new Promise(function (resolve) { setTimeout(resolve, 60) })
        opened = await openDeleteItemFromTrigger(doc, item.moreBtn, deadline)
      }
      if (!opened.item) return { ok: false, error: opened.error || '未找到删除菜单项' }
      delItem = opened.item
    }

    const oldDialogs = new Set(all(doc, DIALOG_SEL).filter(visible))
    const fastCap = typeof window !== 'undefined' ? window.__tulaiHistoryFast : null
    if (fastCap && typeof fastCap.arm === 'function') fastCap.arm(targetID)
    try {
      if (!await pressMenuControl(delItem)) {
        closeMenus(doc)
        return { ok: false, error: '无法点击删除菜单项' }
      }

      let confirmed = false
      while (Date.now() < deadline) {
        if (cancelled && cancelled()) {
          closeMenus(doc)
          return { ok: false, stopped: 'cancelled', error: '清理已取消' }
        }
        const blk = historyBlocked(doc)
        if (blk) return Object.assign({ ok: false }, blk)
        const dlg = findConfirmDeleteButton(doc, oldDialogs)
        if (dlg && !confirmed) {
          confirmed = true
          if (!await pressMenuControl(dlg.button)) {
            return { ok: false, error: '无法点击确认删除按钮' }
          }
          await new Promise(function (resolve) { setTimeout(resolve, 60) })
          continue
        }
        const stillInSidebar = conversationItems(doc).some(function (it) { return it.id === targetID })
        const stillDialog = !!findConfirmDeleteButton(doc, oldDialogs)
        const nowHrefID = conversationIdFromHref(typeof location !== 'undefined' && location ? location.href : '')
        const removed = inSidebar ? !stillInSidebar : (usedHeader ? (nowHrefID !== targetID && !stillInSidebar) : !stillInSidebar)
        if (removed && !stillDialog) {
          await waitRadixClosed(doc, 400)
          return { ok: true, id: targetID, title }
        }
        await new Promise(function (resolve) { setTimeout(resolve, 40) })
      }
      closeMenus(doc)
      return { ok: false, error: '删除对话超时未生效' }
    } finally {
      if (fastCap && typeof fastCap.disarm === 'function') fastCap.disarm()
    }
  }

  function findHistoryScroller(doc) {
    let cur = doc && doc.querySelector && doc.querySelector('[data-testid="chat_list_wrapper"]')
    for (let d = 0; cur && d < 8; d++, cur = cur.parentElement) {
      if (typeof cur.scrollHeight === 'number' && typeof cur.clientHeight === 'number' && cur.scrollHeight > cur.clientHeight + 20) {
        return cur
      }
    }
    return null
  }

  async function deleteNextHistoryItem(doc, skipIds, keepRecent, cancelled, timeoutMs) {
    const blocked = historyBlocked(doc)
    if (blocked) return Object.assign({ ok: false }, blocked)
    await ensureSidebarOpen(doc, 2000)
    const skip = new Set(Array.isArray(skipIds) ? skipIds.map(String) : [])
    const keep = Math.max(0, Number(keepRecent) || 0)

    function pickTarget() {
      const items = conversationItems(doc)
      const candidates = items.slice(keep).filter(function (it) { return !skip.has(it.id) })
      return { items, target: candidates[0] || null }
    }

    let picked = pickTarget()
    if (!picked.target) {
      const scroller = findHistoryScroller(doc)
      if (scroller) {
        const beforeTop = scroller.scrollTop
        scroller.scrollTop = scroller.scrollHeight
        if (scroller.scrollTop !== beforeTop) {
          await new Promise(function (resolve) { setTimeout(resolve, 450) })
          picked = pickTarget()
        }
      }
    }
    if (!picked.target) {
      return { ok: true, done: true, remaining: picked.items.length }
    }
    const res = await deleteConversation(doc, picked.target.id, cancelled, timeoutMs)
    const afterItems = conversationItems(doc)
    if (!res.ok) {
      return {
        ok: false,
        id: picked.target.id,
        title: picked.target.title,
        remaining: afterItems.length,
        stopped: res.stopped || '',
        error: res.error || '删除失败',
      }
    }
    return {
      ok: true,
      deleted: true,
      id: res.id || picked.target.id,
      title: res.title || picked.target.title,
      remaining: afterItems.length,
    }
  }

  async function collectHistoryIds(doc, skipIds, keepRecent, maxCount) {
    const blocked = historyBlocked(doc)
    if (blocked) return Object.assign({ ok: false, ids: [], keepIds: [], titles: {} }, blocked)
    await ensureSidebarOpen(doc, 2000)
    const skip = new Set(Array.isArray(skipIds) ? skipIds.map(String) : [])
    const keep = Math.max(0, Number(keepRecent) || 0)
    const limit = Math.max(1, Number(maxCount) || 200)
    const scroller = findHistoryScroller(doc)
    if (scroller && scroller.scrollTop > 0) {
      scroller.scrollTop = 0
      await new Promise(function (resolve) { setTimeout(resolve, 80) })
    }

    const ordered = []
    const seen = new Set()
    const titles = {}
    function scan() {
      let added = 0
      const items = conversationItems(doc)
      for (let i = 0; i < items.length; i++) {
        const it = items[i]
        if (!seen.has(it.id)) {
          seen.add(it.id)
          ordered.push(it.id)
          titles[it.id] = it.title || ''
          added++
        }
      }
      return added
    }

    scan()
    if (scroller && ordered.length < keep + limit) {
      let idleSteps = 0
      const step = Math.max(200, Math.floor((scroller.clientHeight || 400) * 0.8))
      for (let round = 0; round < 35 && ordered.length < keep + limit; round++) {
        const prevTop = scroller.scrollTop
        const prevHeight = scroller.scrollHeight
        scroller.scrollTop = Math.min(scroller.scrollHeight, prevTop + step)
        const atBottom = scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 20
        await new Promise(function (resolve) { setTimeout(resolve, atBottom ? 350 : 120) })
        const added = scan()
        if (added === 0 && scroller.scrollTop === prevTop && scroller.scrollHeight === prevHeight) {
          if (++idleSteps >= 2) break
        } else {
          idleSteps = 0
        }
      }
      scroller.scrollTop = 0
    }

    const keepIds = ordered.slice(0, keep)
    const keepSet = new Set(keepIds)
    const ids = ordered.filter(function (id) { return !keepSet.has(id) && !skip.has(id) }).slice(0, limit)
    return { ok: true, ids: ids, keepIds: keepIds, titles: titles, totalSeen: ordered.length }
  }

  // ---- 豆包「管理历史对话」页（/chat/thread/list）原生批量删除 ----

  function inSidebar(el) {
    return !!(closest(el, '#flow_chat_sidebar') || closest(el, '[data-testid="flow_chat_sidebar"]') || closest(el, '[data-testid="chat_route_layout_leftside_nav"]'))
  }

  function isCheckedBox(el) {
    if (!el) return false
    if (el.checked === true) return true
    if (el.getAttribute && (el.getAttribute('aria-checked') === 'true' || el.getAttribute('data-state') === 'checked')) return true
    const inner = el.querySelector && el.querySelector('input[type="checkbox"],[role="checkbox"]')
    if (inner && inner !== el) return isCheckedBox(inner)
    return false
  }

  function findThreadListMoreButton(doc) {
    const winW = (typeof window !== 'undefined' && window.innerWidth) || 1200
    const candidates = all(doc, 'button,[role="button"]').filter(function (el) {
      if (!enabled(el) || inSidebar(el)) return false
      if (closest(el, '[role="menu"],[role="dialog"],[role="alertdialog"]')) return false
      const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { top: 999, left: 0, width: 0, height: 0 }
      if (r.top < 0 || r.top > 120 || r.left < winW * 0.5) return false
      const hasMenu = el.getAttribute && (el.getAttribute('aria-haspopup') === 'menu' || el.getAttribute('data-slot') === 'dropdown-menu-trigger')
      const testid = String((el.getAttribute && el.getAttribute('data-testid')) || '')
      const label = normalize(labelOf(el))
      const text = normalize(el.innerText)
      const hasSvg = !!(el.querySelector && el.querySelector('svg'))
      return !!(hasMenu || /更多|more/i.test(label) || /more|menu/i.test(testid) || (hasSvg && text.length <= 2) || text === '...' || text === '···')
    })
    if (!candidates.length) return null
    candidates.sort(function (a, b) {
      const ra = a.getBoundingClientRect ? a.getBoundingClientRect() : { left: 0 }
      const rb = b.getBoundingClientRect ? b.getBoundingClientRect() : { left: 0 }
      return rb.left - ra.left
    })
    return candidates[0]
  }

  function findBatchDeleteMenuItem(menu) {
    if (!menu) return null
    const items = all(menu, '[role="menuitem"],[role="option"],button,div,span').filter(function (el) {
      if (!enabled(el)) return false
      if ((el.tagName === 'DIV' || el.tagName === 'SPAN') && !el.getAttribute('role') && el.children && el.children.length > 2) return false
      const t = normalize(el.innerText)
      return /批量删除对话|批量删除|清空对话|删除全部/.test(t)
    })
    return items.find(function (el) { return /批量删除对话|批量删除/.test(normalize(el.innerText)) }) || items[0] || null
  }

  function findSelectAllControl(doc) {
    const nodes = all(doc, 'label,button,[role="checkbox"],[role="button"],div,span').filter(function (el) {
      if (!enabled(el) || inSidebar(el)) return false
      if (closest(el, '[role="menu"],[role="dialog"],[role="alertdialog"]')) return false
      const t = normalize(el.innerText)
      if (!/^(全选|选择全部|全部选择)$/.test(t)) return false
      if ((el.tagName === 'DIV' || el.tagName === 'SPAN') && el.children && el.children.length > 3) return false
      return true
    })
    if (!nodes.length) return null
    const hit = nodes[0]
    const box = (hit.querySelector && hit.querySelector('input[type="checkbox"],[role="checkbox"]')) ||
      (hit.parentElement && hit.parentElement.querySelector && hit.parentElement.querySelector('input[type="checkbox"],[role="checkbox"]'))
    return box && enabled(box) ? box : hit
  }

  function findMainCheckboxes(doc, selectAllEl) {
    const allBoxes = all(doc, 'input[type="checkbox"],[role="checkbox"],[data-testid*="checkbox"]').filter(function (el) {
      if (!enabled(el) || inSidebar(el)) return false
      if (closest(el, '[role="menu"],[role="dialog"],[role="alertdialog"]')) return false
      if (selectAllEl && (el === selectAllEl || (selectAllEl.contains && selectAllEl.contains(el)) || (el.contains && el.contains(selectAllEl)))) return false
      const parentText = normalize(el.parentElement && el.parentElement.innerText)
      if (/^(全选|选择全部|全部选择)$/.test(parentText)) return false
      return true
    })
    // 去重（防止 wrapper 与内层 input 同时命中）
    const dedup = allBoxes.filter(function (el) {
      return !allBoxes.some(function (other) { return other !== el && el.contains && el.contains(other) })
    })
    dedup.sort(function (a, b) {
      const ra = a.getBoundingClientRect ? a.getBoundingClientRect() : { top: 0 }
      const rb = b.getBoundingClientRect ? b.getBoundingClientRect() : { top: 0 }
      return ra.top - rb.top
    })
    return dedup
  }

  function findBatchDeleteButton(doc) {
    const btns = all(doc, 'button,[role="button"]').filter(function (el) {
      if (!enabled(el) || inSidebar(el)) return false
      if (closest(el, '[role="menu"],[role="dialog"],[role="alertdialog"]')) return false
      const t = normalize(el.innerText)
      if (/批量删除/.test(t)) return false
      return /^(删除|删除对话|删除所选|删除选中|确认删除)(\s*[\(（]\s*\d+\s*[\)）]|\s+\d+)?$/.test(t)
    })
    return btns[btns.length - 1] || null
  }

  function parseCountFromText(text) {
    const m = /(\d+)/.exec(String(text || ''))
    return m ? parseInt(m[1], 10) : 0
  }

  function exitBatchMode(doc) {
    const cancelBtn = all(doc, 'button,[role="button"]').find(function (el) {
      if (!enabled(el) || inSidebar(el)) return false
      if (closest(el, '[role="menu"],[role="dialog"],[role="alertdialog"]')) return false
      return /^(取消|完成|退出)$/.test(normalize(el.innerText))
    })
    if (cancelBtn) activate(cancelBtn)
    else closeMenus(doc)
  }

  async function batchDeleteOnThreadList(doc, keepRecent, maxDelete, cancelled, timeoutMs) {
    const blocked = historyBlocked(doc)
    if (blocked) return Object.assign({ ok: false }, blocked)
    const keep = Math.max(0, Number(keepRecent) || 0)
    const limit = Math.max(1, Number(maxDelete) || 200)
    const deadline = Date.now() + (timeoutMs > 0 ? timeoutMs : 15000)
    await waitRadixClosed(doc, 400)

    const beforeSidebar = conversationItems(doc)
    const bodyText = normalize(doc && doc.body && doc.body.innerText)
    if (beforeSidebar.length === 0 && /暂无历史对话|暂无对话|还没有历史对话|暂无记录/.test(bodyText)) {
      return { ok: true, done: true, deletedCount: 0, remaining: 0 }
    }

    // 1. 若尚未进入批量选择模式，点右上角「...」→「批量删除对话」
    let selectAllEl = findSelectAllControl(doc)
    let rowBoxes = findMainCheckboxes(doc, selectAllEl)
    if (!rowBoxes.length && !selectAllEl) {
      const moreBtn = findThreadListMoreButton(doc)
      if (!moreBtn) {
        return { ok: false, unsupported: true, error: '未找到管理历史对话页右上角更多按钮' }
      }
      const oldMenus = new Set(openActionMenus(doc))
      if (!await pressMenuControl(moreBtn)) {
        return { ok: false, unsupported: true, error: '无法点击右上角更多按钮' }
      }
      let batchItem = null
      const menuUntil = Math.min(deadline, Date.now() + 2500)
      while (Date.now() < menuUntil) {
        const menus = openActionMenus(doc).filter(function (m) { return !oldMenus.has(m) })
        for (let i = menus.length - 1; i >= 0; i--) {
          batchItem = findBatchDeleteMenuItem(menus[i])
          if (batchItem) break
        }
        if (batchItem) break
        await new Promise(function (r) { setTimeout(r, 30) })
      }
      if (!batchItem) {
        closeMenus(doc)
        return { ok: false, unsupported: true, error: '管理历史对话菜单中未找到「批量删除对话」' }
      }
      const oldDialogs = new Set(all(doc, DIALOG_SEL).filter(visible))
      if (!await pressMenuControl(batchItem)) {
        closeMenus(doc)
        return { ok: false, unsupported: true, error: '无法点击「批量删除对话」' }
      }

      // 等待进入勾选模式或直接弹出清空确认框
      const modeUntil = Math.min(deadline, Date.now() + 3000)
      while (Date.now() < modeUntil) {
        if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
        const directDlg = findConfirmDeleteButton(doc, oldDialogs)
        if (directDlg) {
          if (keep > 0) {
            closeMenus(doc)
            return { ok: false, unsupported: true, error: '原生一键清空不支持保留最近条数' }
          }
          if (!await pressMenuControl(directDlg.button)) {
            return { ok: false, error: '无法点击批量删除确认按钮' }
          }
          while (Date.now() < deadline) {
            if (!findConfirmDeleteButton(doc, oldDialogs) && conversationItems(doc).length === 0) {
              return { ok: true, done: true, deletedCount: Math.max(1, beforeSidebar.length), remaining: 0 }
            }
            await new Promise(function (r) { setTimeout(r, 50) })
          }
          return { ok: true, done: conversationItems(doc).length === 0, deletedCount: Math.max(1, beforeSidebar.length), remaining: conversationItems(doc).length }
        }
        selectAllEl = findSelectAllControl(doc)
        rowBoxes = findMainCheckboxes(doc, selectAllEl)
        if (rowBoxes.length > 0 || selectAllEl) break
        await new Promise(function (r) { setTimeout(r, 40) })
      }
    }

    if (!rowBoxes.length && !selectAllEl) {
      if (conversationItems(doc).length <= keep) {
        return { ok: true, done: true, deletedCount: 0, remaining: conversationItems(doc).length }
      }
      return { ok: false, unsupported: true, error: '进入批量删除模式后未检测到复选框' }
    }

    // 2. 勾选目标对话（keep=0 且无小上限时优先点「全选」，否则精准勾选第 keep 条之后的复选框）
    let selectedCount = 0
    const useSelectAll = keep === 0 && selectAllEl && (rowBoxes.length === 0 || rowBoxes.length <= limit)
    if (useSelectAll) {
      if (!isCheckedBox(selectAllEl)) {
        await pressMenuControl(selectAllEl)
        await new Promise(function (r) { setTimeout(r, 120) })
      }
      rowBoxes = findMainCheckboxes(doc, selectAllEl)
      selectedCount = rowBoxes.filter(isCheckedBox).length
    }
    if (selectedCount === 0 && rowBoxes.length > 0) {
      const targets = rowBoxes.slice(keep, keep + limit)
      if (!targets.length) {
        exitBatchMode(doc)
        return { ok: true, done: true, deletedCount: 0, remaining: rowBoxes.length }
      }
      for (let i = 0; i < targets.length; i++) {
        if (cancelled && cancelled()) {
          exitBatchMode(doc)
          return { ok: false, stopped: 'cancelled', error: '清理已取消' }
        }
        if (!isCheckedBox(targets[i])) {
          await pressMenuControl(targets[i])
          await new Promise(function (r) { setTimeout(r, 15) })
        }
      }
      selectedCount = targets.filter(isCheckedBox).length || targets.length
    }

    // 3. 点击底部/顶部批量「删除」按钮 + 二次确认弹窗
    let delBtn = null
    const btnUntil = Math.min(deadline, Date.now() + 2000)
    while (Date.now() < btnUntil) {
      delBtn = findBatchDeleteButton(doc)
      if (delBtn) break
      await new Promise(function (r) { setTimeout(r, 40) })
    }
    if (!delBtn) {
      exitBatchMode(doc)
      return { ok: false, unsupported: true, error: '批量删除模式下未找到删除提交按钮' }
    }
    const btnCount = parseCountFromText(delBtn.innerText)
    const beforeCount = rowBoxes.length
    const sampleIDs = beforeSidebar.slice(keep, keep + 3).map(function (it) { return it.id })
    const oldDialogs = new Set(all(doc, DIALOG_SEL).filter(visible))

    if (!await pressMenuControl(delBtn)) {
      exitBatchMode(doc)
      return { ok: false, error: '无法点击批量删除按钮' }
    }

    let confirmed = false
    while (Date.now() < deadline) {
      if (cancelled && cancelled()) return { ok: false, stopped: 'cancelled', error: '清理已取消' }
      const blk = historyBlocked(doc)
      if (blk) return Object.assign({ ok: false }, blk)
      const dlg = findConfirmDeleteButton(doc, oldDialogs)
      if (dlg && !confirmed) {
        confirmed = true
        if (!await pressMenuControl(dlg.button)) {
          return { ok: false, error: '无法点击批量删除确认按钮' }
        }
        await new Promise(function (r) { setTimeout(r, 80) })
        continue
      }
      const stillDialog = !!findConfirmDeleteButton(doc, oldDialogs)
      const curSidebar = conversationItems(doc)
      const curSet = new Set(curSidebar.map(function (it) { return it.id }))
      const sampleRemoved = sampleIDs.length > 0 && sampleIDs.some(function (id) { return !curSet.has(id) })
      const curBoxes = findMainCheckboxes(doc, findSelectAllControl(doc))
      const boxesDropped = beforeCount > 0 && (curBoxes.length < beforeCount)
      const emptyNow = curSidebar.length === 0 && /暂无历史对话|暂无对话|还没有历史对话/.test(normalize(doc && doc.body && doc.body.innerText))
      if (!stillDialog && (sampleRemoved || boxesDropped || emptyNow)) {
        await waitRadixClosed(doc, 400)
        const afterSidebar = conversationItems(doc)
        const count = Math.max(1, btnCount || selectedCount || Math.max(0, beforeCount - curBoxes.length))
        const remaining = afterSidebar.length
        return { ok: true, done: remaining <= keep, deletedCount: count, remaining: remaining }
      }
      await new Promise(function (r) { setTimeout(r, 50) })
    }
    return { ok: false, error: '批量删除对话超时未生效' }
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
    imageModeActive,
    ensureImageMode,
    ratioControl,
    ratioValue,
    setOutputRatio,
    unsentBlocksUpload,
    sendButton,
    activate,
    clickNewChat,
    canMultiUpload,
    loginWall,
    removeButtons,
    dismissClientPromo,
    mark,
    acceptRawImages,
    newAssistants,
    resultImages,
    revealResults,
    pollResults,
    resultsBusy,
    replyDone,
    assistantText,
    composerCleared,
    sendAccepted,
    assignFiles,
    freshAttachment,
    attachmentNames,
    attachmentScope,
    detectCaptcha,
    captchaCrop,
    bytesToBase64,
    base64ToBytes,
    conversationItems,
    ensureSidebarOpen,
    deleteConversation,
    deleteNextHistoryItem,
    collectHistoryIds,
    batchDeleteOnThreadList,
    normalize,
    imageURL,
    isPlaceholderURL,
    queries,
    USER_Q,
    ASSISTANT_Q,
  }
})()
