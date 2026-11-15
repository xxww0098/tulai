// 跑在页面主世界（manifest world: MAIN）。内容脚本改自己的 HTMLInputElement.prototype
// 拦不到千问页面里的 showPicker：两边不共享 JS 原型。
// 当前千问「上传图片」会临时创建 input，先 showPicker，失败再 click（qianwen-web 的 em()）。
// 内容脚本通过同步可取消事件把源图传入闭包状态，这里在那一次调用里写入文件并派发 change，
// 不打开系统文件框。没武装时走原来的 showPicker/click，用户自己选文件不受影响。
(function () {
  const PROBE = '__PB_PROBE__'
  const ARM = '__PB_ARM__'
  const DISARM = '__PB_DISARM__'
  const OK = '__PB_OK__'
  const FAIL = '__PB_FAIL__'
  const ARM_TTL_MS = 30000
  const originals = new WeakMap()
  let installed = false
  let pending = null
  let result = ''

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

  function consume(input) {
    if (!pending || !input) return false
    if (Date.now() > pending.expiresAt) {
      pending = null
      return false
    }
    if (String(input.type || '').toLowerCase() !== 'file') return false
    if (!acceptsImage(input)) return false
    const file = pending.file
    pending = null
    result = assign(input, file) ? 'ok' : 'fail'
    return true
  }

  function unwrap(fn) {
    let cur = fn
    while (cur && (typeof cur === 'object' || typeof cur === 'function') && originals.has(cur)) {
      cur = originals.get(cur)
    }
    return cur
  }

  function installToStringHook() {
    const origToString = Function.prototype.toString
    const desc = Object.getOwnPropertyDescriptor(Function.prototype, 'toString')
    if (desc && !desc.configurable && !desc.writable) return
    const wrapped = new Proxy(origToString, {
      apply: function (target, thisArg, args) {
        return Reflect.apply(target, unwrap(thisArg), args)
      },
    })
    originals.set(wrapped, origToString)
    try {
      Object.defineProperty(Function.prototype, 'toString', {
        value: wrapped,
        writable: desc ? desc.writable : true,
        enumerable: desc ? desc.enumerable : false,
        configurable: desc ? desc.configurable : true,
      })
    } catch (e) {}
  }

  function patch(proto, name, onCall) {
    if (!proto) return false
    const desc = Object.getOwnPropertyDescriptor(proto, name)
    const original = desc && typeof desc.value === 'function' ? desc.value : proto[name]
    if (typeof original !== 'function') return false
    const wrapped = new Proxy(original, {
      apply: function (target, thisArg, args) {
        if (onCall(thisArg, args)) return undefined
        return Reflect.apply(target, thisArg, args)
      },
    })
    originals.set(wrapped, original)
    try {
      Object.defineProperty(proto, name, {
        configurable: desc ? desc.configurable : true,
        enumerable: desc ? desc.enumerable : true,
        writable: desc ? desc.writable : true,
        value: wrapped,
      })
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
    if (!raw || typeof raw !== 'string') return null
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

  function isInstalled() {
    if (installed) return true
    if (!document || typeof document.dispatchEvent !== 'function' || typeof CustomEvent !== 'function') return false
    try {
      return document.dispatchEvent(new CustomEvent(PROBE, { cancelable: true })) === false
    } catch (e) {
      return false
    }
  }

  function onProbe(e) {
    if (e && typeof e.preventDefault === 'function') e.preventDefault()
  }

  function onArm(e) {
    const raw = e && typeof e.detail === 'string' ? e.detail : ''
    const file = decode(raw)
    if (!file) {
      pending = null
      result = ''
      return
    }
    pending = { file: file, expiresAt: Date.now() + ARM_TTL_MS }
    result = ''
    if (typeof e.preventDefault === 'function') e.preventDefault()
  }

  function onDisarm() {
    pending = null
    result = ''
  }

  function onOk(e) {
    if (result !== 'ok') return
    result = ''
    if (e && typeof e.preventDefault === 'function') e.preventDefault()
  }

  function onFail(e) {
    if (result !== 'fail') return
    result = ''
    if (e && typeof e.preventDefault === 'function') e.preventDefault()
  }

  function install() {
    if (isInstalled()) return
    if (!globalThis.HTMLInputElement || !document) return
    installToStringHook()
    let ok = false
    if (typeof HTMLInputElement.prototype.showPicker === 'function') {
      ok = patch(HTMLInputElement.prototype, 'showPicker', function (self) {
        return consume(self)
      }) || ok
    }
    const elementProto = globalThis.HTMLElement && HTMLElement.prototype
    if (elementProto && typeof elementProto.click === 'function') {
      ok = patch(elementProto, 'click', function (self) {
        return !!(self && String(self.type || '').toLowerCase() === 'file' && consume(self))
      }) || ok
    }
    if (!ok) return
    document.addEventListener(PROBE, onProbe, true)
    document.addEventListener(ARM, onArm, true)
    document.addEventListener(DISARM, onDisarm, true)
    document.addEventListener(OK, onOk, true)
    document.addEventListener(FAIL, onFail, true)
    installed = true
  }

  install()
  if (!isInstalled() && document && typeof document.addEventListener === 'function') {
    document.addEventListener('DOMContentLoaded', install, { once: true })
  }
})()
