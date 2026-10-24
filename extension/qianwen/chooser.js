// 跑在页面主世界（manifest world: MAIN）。内容脚本改自己的 HTMLInputElement.prototype
// 拦不到千问页面里的 showPicker：两边不共享 JS 原型。
// 当前千问「上传图片」会临时创建 input，先 showPicker，失败再 click（qianwen-web 的 em()）。
// 内容脚本把源图放进隐藏节点并派发 xbs-qianwen-arm 之后，这里在那一次调用里写入文件并派发 change，
// 不打开系统文件框。没武装时走原来的 showPicker/click，用户自己选文件不受影响。
(function () {
  if (globalThis.__xbsQianwenChooser) return
  const ARM = 'xbs-qianwen-arm'
  const DISARM = 'xbs-qianwen-disarm'
  const SLOT = 'xbs-qianwen-file-slot'
  let pending = null

  function acceptsImage(input) {
    let accept = ''
    try { accept = String(input.accept || '') } catch (e) {}
    if (!accept && input.getAttribute) accept = String(input.getAttribute('accept') || '')
    if (!String(accept).trim()) return true
    return /image|png|jpe?g|webp|bmp|gif|\*/i.test(accept)
  }

  function assign(input, file) {
    let set = false
    try {
      const dt = new DataTransfer()
      dt.items.add(file)
      input.files = dt.files
      set = !!(input.files && input.files.length)
    } catch (e) {
      return false
    }
    if (!set) return false
    // 千问的 change 回调会先读 files，再把 value 清空。成功与否以派发前的文件列表为准。
    try { input.dispatchEvent(new Event('change', { bubbles: true })) } catch (e) {}
    try { input.dispatchEvent(new Event('input', { bubbles: true })) } catch (e) {}
    return true
  }

  function setAttr(name, value) {
    const root = document.documentElement
    if (root && root.setAttribute) root.setAttribute(name, value)
  }

  function consume(input) {
    if (!pending || !input) return false
    if (String(input.type || '').toLowerCase() !== 'file') return false
    if (!acceptsImage(input)) return false
    const file = pending
    pending = null
    const ok = assign(input, file)
    setAttr('data-xbs-chooser-result', ok ? 'ok' : 'fail')
    setAttr('data-xbs-chooser-armed', '0')
    return true
  }

  function patch(proto, name, wrap) {
    if (!proto) return false
    const desc = Object.getOwnPropertyDescriptor(proto, name)
    const original = desc && typeof desc.value === 'function' ? desc.value : proto[name]
    if (typeof original !== 'function') return false
    const wrapped = wrap(original)
    try {
      Object.defineProperty(proto, name, { configurable: true, writable: true, value: wrapped })
      return true
    } catch (e) {
      try {
        proto[name] = wrapped
        return true
      } catch (err) {
        return false
      }
    }
  }

  function decode(raw) {
    if (!raw) return null
    let payload
    try { payload = JSON.parse(raw) } catch (e) { return null }
    if (!payload || !payload.b64) return null
    try {
      const binary = atob(payload.b64)
      const bytes = new Uint8Array(binary.length)
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i) & 255
      return new File([bytes], payload.name || 'image', { type: payload.mime || 'application/octet-stream' })
    } catch (e) {
      return null
    }
  }

  function onArm() {
    const slot = document.getElementById(SLOT)
    const raw = slot ? String(slot.textContent || '') : ''
    if (slot) slot.textContent = ''
    pending = decode(raw)
    setAttr('data-xbs-chooser-armed', pending ? '1' : '0')
  }

  function onDisarm() {
    pending = null
    setAttr('data-xbs-chooser-armed', '0')
  }

  function install() {
    if (globalThis.__xbsQianwenChooser) return
    if (!globalThis.HTMLInputElement || !document || !document.documentElement) return
    let ok = false
    if (typeof HTMLInputElement.prototype.showPicker === 'function') {
      ok = patch(HTMLInputElement.prototype, 'showPicker', function (original) {
        return function () {
          if (consume(this)) return
          return original.apply(this, arguments)
        }
      }) || ok
    }
    const elementProto = globalThis.HTMLElement && HTMLElement.prototype
    if (elementProto && typeof elementProto.click === 'function') {
      ok = patch(elementProto, 'click', function (original) {
        return function () {
          if (this && String(this.type || '').toLowerCase() === 'file' && consume(this)) return
          return original.apply(this, arguments)
        }
      }) || ok
    }
    if (!ok) return
    document.addEventListener(ARM, onArm, true)
    document.addEventListener(DISARM, onDisarm, true)
    document.documentElement.setAttribute('data-xbs-chooser', 'qianwen')
    globalThis.__xbsQianwenChooser = true
  }

  install()
  if (!globalThis.__xbsQianwenChooser && document && typeof document.addEventListener === 'function') {
    document.addEventListener('DOMContentLoaded', install, { once: true })
  }
})()
