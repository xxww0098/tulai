package strategy

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/cdp"
)

// Doubao 豆包策略。
type Doubao struct {
	base
	washNames []string // 多图洗图当前消息内的唯一上传文件名，按源图顺序排列。
	washDir   string
}

// Platform 平台名。
func (Doubao) Platform() string { return "豆包" }

// HomeURL 豆包图像生成入口。
func (Doubao) HomeURL() string { return "https://www.doubao.com/chat/" }

func doubaoPagePrelude() string {
	pageJS := extbundle.DoubaoPageJS()
	rev := fmt.Sprintf("%x", sha256.Sum256([]byte(pageJS)))
	return `if (!window.__tulaiWashPage || window.__tulaiWashPageRev !== ` + fmt.Sprintf("%q", rev) + `) {` + pageJS + `
			window.__tulaiWashPage = window.TULAIPage;
			window.__tulaiWashPageRev = ` + fmt.Sprintf("%q", rev) + `;
		}`
}

// ensureDoubaoImageMode 确认输入区处于「图像生成」模式；已选中时不点击。
func ensureDoubaoImageMode(s *cdp.Session) error {
	if s == nil {
		return nil
	}
	s.EnsureBase()
	out, err := s.EvaluateAsync(`(async () => {
		` + doubaoPagePrelude() + `
		return JSON.stringify(await window.__tulaiWashPage.ensureImageMode(document));
	})()`)
	if err != nil {
		return err
	}
	var mode struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := cdp.ParseJSValue(out, &mode); err != nil {
		return err
	}
	if !mode.OK {
		return fmt.Errorf("进入豆包图像生成模式: %s", mode.Error)
	}
	return nil
}

// EnsureImageMode 确认豆包处于「图像生成」模式；已选中时零点击直接返回。
func (Doubao) EnsureImageMode(s *cdp.Session) error {
	return ensureDoubaoImageMode(s)
}

// CleanComposer 在新对话或复用对话中先进入图像模式，之后才上传或填词。
func (d Doubao) CleanComposer(s *cdp.Session) error {
	if err := d.base.CleanComposer(s); err != nil {
		return err
	}
	return ensureDoubaoImageMode(s)
}

// SetOutputRatio 在豆包图像模式下调用原生「比例」菜单。返回是否生效与失败原因。
func (Doubao) SetOutputRatio(s *cdp.Session, ratio string) (bool, string) {
	if s == nil || strings.TrimSpace(ratio) == "" {
		return false, ""
	}
	arg, err := json.Marshal(strings.TrimSpace(ratio))
	if err != nil {
		return false, err.Error()
	}
	s.EnsureBase()
	for attempt := 0; attempt < 2; attempt++ {
		out, err := s.EvaluateAsync(`(async () => {
			` + doubaoPagePrelude() + `
			if (!window.__tulaiWashPage || typeof window.__tulaiWashPage.setOutputRatio !== 'function') {
				return JSON.stringify({ ok: false, error: '页面脚本不支持原生比例' });
			}
			return JSON.stringify(await window.__tulaiWashPage.setOutputRatio(document, ` + string(arg) + `));
		})()`)
		if err != nil {
			return false, err.Error()
		}
		var res struct {
			OK       bool   `json:"ok"`
			Error    string `json:"error"`
			LostMode bool   `json:"lostMode"`
		}
		if err := cdp.ParseJSValue(out, &res); err != nil {
			return false, err.Error()
		}
		if res.OK || !res.LostMode || attempt > 0 {
			return res.OK, res.Error
		}
		if err := ensureDoubaoImageMode(s); err != nil {
			return false, err.Error()
		}
	}
	return false, "未找到「比例」入口"
}

// doubaoImageModeActive 快速同步检查豆包当前是否仍处于「图像生成」模式（用于写入提示词后发送前的最终防线）。
func doubaoImageModeActive(s *cdp.Session) (bool, error) {
	if s == nil {
		return true, nil
	}
	out, err := s.Evaluate(`(() => {
		` + doubaoPagePrelude() + `
		return !!(window.__tulaiWashPage && window.__tulaiWashPage.imageModeActive && window.__tulaiWashPage.imageModeActive(document));
	})()`)
	if err != nil {
		return false, err
	}
	var active bool
	if err := cdp.ParseJSValue(out, &active); err != nil {
		return false, err
	}
	return active, nil
}

// SendPrompt 发送前再确认图像模式：文生图只靠清理时切一次不够，
// 图生图上传源图后输入区可能被页面重绘掉已选技能，两种任务都以
// 发送瞬间处于图像生成模式为准，未选中就先补点再写词发送。
func (d Doubao) SendPrompt(s *cdp.Session, prompt string) error {
	if err := ensureDoubaoImageMode(s); err != nil {
		return err
	}
	if len(d.washNames) > 1 {
		prompt = DoubaoWashPrompt(prompt, d.washNames)
		if err := checkWashAttachmentsSet(s, d.washNames, false); err != nil {
			return err
		}
		if err := MarkDoubaoTurn(s, prompt); err != nil {
			return err
		}
	}
	return d.base.sendPromptWithPostInsertGuard(s, prompt, ensureDoubaoImageMode, doubaoImageModeActive)
}

// FetchResultBytes 豆包覆盖：先要原图地址，再按页面 referer 和浏览器登录态去拉。
func (d Doubao) FetchResultBytes(s *cdp.Session, raw string) ([]byte, error) {
	tried := map[string]bool{}
	var last error
	for _, candidate := range []string{OriginalImageURL(raw), PreferUnwatermarkedURL(raw), raw} {
		if candidate == "" || tried[candidate] {
			continue
		}
		tried[candidate] = true
		body, err := d.base.FetchResultBytes(s, candidate)
		if err == nil {
			return body, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("缺少图片地址")
	}
	return nil, last
}

// FetchRawResultBytes 保留接口返回的原图签名，不改写 CDN 模板。
func (d Doubao) FetchRawResultBytes(s *cdp.Session, raw string) ([]byte, error) {
	return d.base.FetchResultBytes(s, raw)
}

// OriginalImageURL 去掉 imagex 的 ~tplv 缩略图模板，留下对象原图地址。
// 没有模板时原样返回。签名跟模板绑在一起时，调用方还会再试页面上的原地址。
func OriginalImageURL(raw string) string {
	clean := PreferUnwatermarkedURL(raw)
	i := strings.Index(clean, "~tplv-")
	if i <= 0 {
		return clean
	}
	tail := ""
	if q := strings.IndexAny(clean[i:], "?#"); q >= 0 {
		tail = clean[i+q:]
	}
	return clean[:i] + tail
}

// PreferUnwatermarkedURL 去掉豆包 CDN 用来带水印的查询。
// 查询里出现 watermark 或 wm=（忽略大小写）时丢掉整段查询，没有这些参数时原样返回。
// 千问结果地址不用这组参数，因此不会被改写；这里不识别别的站点参数。
func PreferUnwatermarkedURL(raw string) string {
	if i := strings.Index(raw, "?"); i > 0 {
		lower := strings.ToLower(raw[i:])
		if strings.Contains(lower, "watermark") || strings.Contains(lower, "wm=") {
			return raw[:i]
		}
	}
	return raw
}

// Qianwen 千问策略。
type Qianwen struct{ base }

// Platform 平台名。
func (Qianwen) Platform() string { return "千问" }

// HomeURL 千问入口（直连 qianwen.com，省去 tongyi.com 301 重定向与 ?ch=tongyi_redirect 参数）。
func (Qianwen) HomeURL() string { return "https://www.qianwen.com/" }

// qianwenImageModeActive 快速同步检查千问当前是否仍处于「AI生图」模式（兼容上传参考图后比例按钮变灰禁用态）。
func qianwenImageModeActive(s *cdp.Session) (bool, error) {
	if s == nil {
		return true, nil
	}
	out, err := s.Evaluate(`(() => {
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		var btns = Array.from(document.querySelectorAll('button,[role="button"]'));
		return btns.some(function (el) {
			if (!shown(el)) return false;
			var lbl = norm(el.getAttribute && el.getAttribute('aria-label'));
			var txt = norm(el.innerText);
			var comb = lbl || txt;
			if (/^比例\s*\d+:\d+$/.test(txt) || /^比例\s*\d+:\d+$/.test(lbl)) return true;
			if (lbl === '参考图' || txt === '参考图') return true;
			if (lbl === 'AI生图' || txt === 'AI生图') {
				if (el.getAttribute('aria-pressed') === 'true' || el.getAttribute('aria-checked') === 'true' || el.getAttribute('aria-selected') === 'true') return true;
				if (el.querySelector && el.querySelector('[data-icon-type*="close"],[class*="close"]')) return true;
			}
			return /^AI生图\s*[×✕]$/.test(comb);
		});
	})()`)
	if err != nil {
		return false, err
	}
	var active bool
	if err := cdp.ParseJSValue(out, &active); err != nil {
		return false, err
	}
	return active, nil
}

// ensureQianwenImageMode 在千问输入区切换并确认进入「AI生图」模式；
// 支持冷加载等待、退出冲突模式（如 PPT创作/AI生视频）及窄窗口溢出到「更多」菜单的场景。
func ensureQianwenImageMode(s *cdp.Session) error {
	return ensureQianwenImageModeWithTimeout(s, 10000)
}

func ensureQianwenImageModeWithTimeout(s *cdp.Session, timeoutMs int) error {
	if s == nil {
		return nil
	}
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	s.EnsureBase()
	out, err := s.EvaluateAsync(`(async () => {
		var timeoutMs = ` + fmt.Sprintf("%d", timeoutMs) + `;
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		function visible(el) {
			if (!shown(el)) return false;
			var st = getComputedStyle(el);
			return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
		}
		async function press(el) {
			if (!el || !el.isConnected) return false;
			if (typeof window.tulaiClick === 'function') {
				try { if (await window.tulaiClick(el)) return true; } catch (e) {}
			}
			if (!el.isConnected) return false;
			try {
				if (typeof el.focus === 'function') el.focus();
				var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
				var opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mousedown', opts));
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mouseup', opts));
				el.click();
				return true;
			} catch (e) { return false; }
		}
		function isRatioBtn(el) {
			if (!shown(el)) return false;
			var t = norm(el.innerText || el.getAttribute('aria-label') || '');
			return /^比例\s*\d+:\d+$/.test(t);
		}
		function isRefImageBtn(el) {
			if (!shown(el)) return false;
			var lbl = norm(el.getAttribute && el.getAttribute('aria-label'));
			var txt = norm(el.innerText);
			return lbl === '参考图' || txt === '参考图';
		}
		function isAIActive(el) {
			if (!shown(el)) return false;
			var lbl = norm(el.getAttribute && el.getAttribute('aria-label'));
			var txt = norm(el.innerText);
			if (lbl !== 'AI生图' && txt !== 'AI生图' && !/^AI生图\s*[×✕]$/.test(txt)) return false;
			if (el.getAttribute('aria-pressed') === 'true' || el.getAttribute('aria-checked') === 'true' || el.getAttribute('aria-selected') === 'true') return true;
			if (/^AI生图\s*[×✕]$/.test(txt)) return true;
			return !!(el.querySelector && el.querySelector('[data-icon-type*="close"],[class*="close"]'));
		}
		function isAIButton(el) {
			if (!visible(el) || isAIActive(el)) return false;
			var lbl = norm(el.getAttribute && el.getAttribute('aria-label'));
			var txt = norm(el.innerText);
			return lbl === 'AI生图' || txt === 'AI生图';
		}
		function isImageModeActive() {
			var btns = Array.from(document.querySelectorAll('button,[role="button"]'));
			return btns.some(function (el) {
				return isAIActive(el) || isRatioBtn(el) || isRefImageBtn(el);
			});
		}
		function openMenus() {
			return Array.from(document.querySelectorAll('[role="menu"],[role="listbox"]')).filter(function (m) {
				return m.getAttribute('data-state') !== 'closed' && visible(m);
			});
		}
		async function closeOpenMenus() {
			if (!openMenus().length) return;
			try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
			for (var i = 0; i < 6 && openMenus().length; i++) {
				await new Promise(function (r) { setTimeout(r, 25); });
			}
		}
		function findInlineAIButton() {
			var preferred = Array.from(document.querySelectorAll('[data-overflow-placement="inline"] button[aria-label="AI生图"],[data-role="visible-capsule"] button[aria-label="AI生图"]')).find(isAIButton);
			if (preferred) return preferred;
			var exact = Array.from(document.querySelectorAll('button[aria-label="AI生图"],[role="button"][aria-label="AI生图"]')).find(isAIButton);
			if (exact) return exact;
			return Array.from(document.querySelectorAll('button,[role="button"]')).find(isAIButton) || null;
		}
		function findMenuAIButton() {
			var menus = openMenus();
			for (var i = 0; i < menus.length; i++) {
				var hit = Array.from(menus[i].querySelectorAll('[role="menuitem"],[role="menuitemcheckbox"],[role="menuitemradio"],button,[role="button"]')).find(isAIButton);
				if (hit) return hit;
			}
			return null;
		}
		function findOtherActiveCapsule() {
			var candidates = Array.from(document.querySelectorAll('[data-overflow-placement="inline"] button[aria-pressed="true"],[data-role="visible-capsule"] button[aria-pressed="true"],button[aria-pressed="true"]'));
			var comp = document.querySelector('[data-slate-editor="true"],textarea,[contenteditable="true"]');
			var cr = comp && comp.getBoundingClientRect ? comp.getBoundingClientRect() : null;
			return candidates.find(function (el) {
				if (!visible(el)) return false;
				if (el.getAttribute('aria-haspopup') === 'menu') return false;
				if (isAIActive(el) || isAIButton(el) || isRatioBtn(el) || isRefImageBtn(el)) return false;
				var lbl = norm(el.getAttribute('aria-label') || el.innerText);
				if (!lbl || lbl === '参考图' || lbl === '更多') return false;
				if (el.closest && (el.closest('[data-overflow-placement]') || el.closest('[data-role="visible-capsule"]') || el.closest('[data-chat-input-body]'))) return true;
				if (cr && el.getBoundingClientRect) {
					var br = el.getBoundingClientRect();
					if (Math.abs(br.top - cr.bottom) <= 140) return true;
				}
				return false;
			}) || null;
		}
		function findMoreTrigger() {
			return Array.from(document.querySelectorAll('button[aria-label="更多"][aria-haspopup="menu"],[role="button"][aria-label="更多"][aria-haspopup="menu"]')).find(visible) || null;
		}
		function hasLoginButton() {
			return Array.from(document.querySelectorAll('button,[role="button"],a')).some(function (el) {
				return visible(el) && /^(登录|立即登录|扫码登录|登录\/注册)$/.test(norm(el.innerText));
			});
		}
		if (isImageModeActive()) return JSON.stringify({ ok: true, already: true });
		var deadline = Date.now() + timeoutMs;
		var clicks = 0, lastClick = 0;
		var exits = 0, lastExit = 0;
		var moreOpens = 0, lastMoreOpen = 0;
		while (Date.now() < deadline) {
			if (isImageModeActive()) {
				await closeOpenMenus();
				return JSON.stringify({ ok: true });
			}
			var btn = findInlineAIButton() || findMenuAIButton();
			if (btn) {
				if (clicks < 3 && (clicks === 0 || Date.now() - lastClick >= 1500)) {
					await press(btn);
					clicks++;
					lastClick = Date.now();
				}
			} else {
				var other = findOtherActiveCapsule();
				if (other && exits < 2 && (exits === 0 || Date.now() - lastExit >= 1200)) {
					await press(other);
					exits++;
					lastExit = Date.now();
				} else if (!openMenus().length && !Array.from(document.querySelectorAll('[role="menu"][data-state="closed"],[role="listbox"][data-state="closed"]')).some(shown)) {
					var more = findMoreTrigger();
					if (more && moreOpens < 2 && (moreOpens === 0 || Date.now() - lastMoreOpen >= 1500)) {
						await press(more);
						moreOpens++;
						lastMoreOpen = Date.now();
					}
				}
			}
			await new Promise(function (r) { setTimeout(r, 30); });
		}
		if (isImageModeActive()) {
			await closeOpenMenus();
			return JSON.stringify({ ok: true });
		}
		await closeOpenMenus();
		if (clicks === 0 && hasLoginButton() && !findMoreTrigger() && !findOtherActiveCapsule()) {
			return JSON.stringify({ ok: false, error: '请先登录千问账号' });
		}
		return JSON.stringify({ ok: false, error: clicks ? '点击千问「AI生图」后模式未生效' : '未找到千问「AI生图」入口' });
	})()`)
	if err != nil {
		return err
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := cdp.ParseJSValue(out, &res); err != nil {
		return err
	}
	if !res.OK {
		if res.Error == "" {
			res.Error = "模式未生效"
		}
		return fmt.Errorf("进入千问 AI生图模式: %s", res.Error)
	}
	return nil
}

// EnsureImageMode 确认千问处于「AI生图」模式；已激活时零点击直接返回。
func (Qianwen) EnsureImageMode(s *cdp.Session) error {
	return ensureQianwenImageMode(s)
}

// CleanComposer 清理输入区后自动切入千问「AI生图」模式，确保生图/洗图/裂变稳定走图像管线。
func (q Qianwen) CleanComposer(s *cdp.Session) error {
	if err := q.base.CleanComposer(s); err != nil {
		return err
	}
	return ensureQianwenImageMode(s)
}

// SetOutputRatio 在千问「AI生图」模式下切换原生「比例」菜单，避免方图/横图被默认 3:4 强行加边裁切。
// 上传参考图后千问会把比例按钮置为 disabled 锁定态：若已锁定为目标比例则立即零点击确认，避免无效等待与误判失活。
func (Qianwen) SetOutputRatio(s *cdp.Session, ratio string) (bool, string) {
	ratio = strings.TrimSpace(ratio)
	if s == nil || ratio == "" {
		return false, ""
	}
	arg, err := json.Marshal(ratio)
	if err != nil {
		return false, err.Error()
	}
	s.EnsureBase()
	for attempt := 0; attempt < 2; attempt++ {
		out, err := s.EvaluateAsync(`(async () => {
		var want = String(` + string(arg) + ` || '').trim();
		var wantAuto = /^(?:自动|自动比例|原比例|智能|智能比例)$/.test(want);
		if (!wantAuto && !/^\d+:\d+$/.test(want)) return JSON.stringify({ ok: false, error: '比例格式应为 4:3' });
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		function visible(el) {
			if (!shown(el)) return false;
			var st = getComputedStyle(el);
			return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
		}
		function norm(s) { return String(s || '').replace(/\s+/g, ' ').trim(); }
		function ratioOf(el) {
			if (!el) return '';
			var t = norm(el.innerText || el.getAttribute('aria-label') || '');
			var m = /^比例\s*(\d+:\d+|自动比例|自动|原比例|智能比例|智能)$/.exec(t);
			return m ? m[1] : '';
		}
		function findShownControl() {
			var all = Array.from(document.querySelectorAll('button,[role="button"]')).filter(function (el) {
				return shown(el) && ratioOf(el) !== '';
			});
			if (!all.length) return null;
			return all.find(function (el) { return el.getAttribute('aria-haspopup') === 'menu'; }) || all[0];
		}
		function findControl() {
			var all = Array.from(document.querySelectorAll('button,[role="button"]')).filter(function (el) {
				return visible(el) && ratioOf(el) !== '';
			});
			if (!all.length) return null;
			return all.find(function (el) { return el.getAttribute('aria-haspopup') === 'menu'; }) || all[0];
		}
		function findMenu() {
			return Array.from(document.querySelectorAll('[role="menu"],[role="listbox"]')).find(function (m) {
				if (m.getAttribute('data-state') === 'closed') return false;
				if (!visible(m)) return false;
				var txt = norm(m.innerText);
				var pairs = txt.match(/\b\d+:\d+\b/g) || [];
				return pairs.length >= 2;
			}) || null;
		}
		function hasClosingMenu() {
			return Array.from(document.querySelectorAll('[role="menu"][data-state="closed"],[role="listbox"][data-state="closed"]')).some(shown);
		}
		function findOption(menu) {
			if (!menu) return null;
			var roleItems = Array.from(menu.querySelectorAll('[role="menuitemcheckbox"],[role="menuitemradio"],[role="menuitem"],[role="option"]'));
			var hit = roleItems.find(function (el) { return visible(el) && norm(el.innerText) === want; });
			if (hit) return hit;
			var fallback = Array.from(menu.querySelectorAll('button,div'));
			return fallback.find(function (el) {
				return visible(el) && norm(el.innerText) === want && (el.tagName === 'BUTTON' || el.children.length <= 2);
			}) || null;
		}
		async function closeMenu(ctrl, waitNatural) {
			if (waitNatural) {
				// Radix 选中菜单项后会自动进入 300ms 退场动画，先等它自然收起，避免误点 ctrl 把菜单重新打开。
				for (var w = 0; w < 14 && (findMenu() || hasClosingMenu()); w++) {
					await new Promise(function (r) { setTimeout(r, 30); });
				}
				if (!findMenu()) return;
			}
			if (!findMenu()) return;
			try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
			for (var i = 0; i < 6 && findMenu(); i++) {
				await new Promise(function (r) { setTimeout(r, 30); });
			}
			if (findMenu() && ctrl && visible(ctrl)) {
				try { await press(ctrl); } catch (e) {}
				for (var j = 0; j < 6 && findMenu(); j++) {
					await new Promise(function (r) { setTimeout(r, 30); });
				}
			}
			for (var k = 0; k < 12 && hasClosingMenu(); k++) {
				await new Promise(function (r) { setTimeout(r, 30); });
			}
		}
		async function press(el) {
			if (!el || !el.isConnected) return false;
			if (typeof window.tulaiClick === 'function') {
				try { if (await window.tulaiClick(el)) return true; } catch (e) {}
			}
			if (!el.isConnected) return false;
			try {
				if (typeof el.focus === 'function') el.focus();
				var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
				var opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mousedown', opts));
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mouseup', opts));
				el.click();
				return true;
			} catch (e) { return false; }
		}
		var deadline = Date.now() + 8000;
		var ctrl = null;
		while (Date.now() < deadline) {
			ctrl = findShownControl();
			if (ctrl) break;
			await new Promise(function (r) { setTimeout(r, 30); });
		}
		if (!ctrl) return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
		if (ratioOf(ctrl) === want) {
			if (findMenu() || hasClosingMenu()) await closeMenu(ctrl, false);
			return JSON.stringify({ ok: true, already: true });
		}
		if (!visible(ctrl)) {
			var unlockUntil = Math.min(deadline, Date.now() + 600);
			while (Date.now() < unlockUntil) {
				var cur = findShownControl();
				if (!cur) return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
				ctrl = cur;
				if (ratioOf(ctrl) === want) return JSON.stringify({ ok: true, already: true });
				if (visible(ctrl)) break;
				await new Promise(function (r) { setTimeout(r, 30); });
			}
			if (!visible(ctrl)) {
				return JSON.stringify({ ok: false, error: '千问上传参考图后比例已锁定为 ' + ratioOf(ctrl) + '，无法改为 ' + want });
			}
		}
		// 若上一个 Radix 菜单正在 300ms fade-out 退场，先等退场结束再开新菜单。
		while (Date.now() < deadline && hasClosingMenu()) {
			await new Promise(function (r) { setTimeout(r, 25); });
		}
		var lastError = '千问比例 ' + want + ' 未生效';
		for (var round = 0; round < 3 && Date.now() < deadline; round++) {
			var liveCtrl = findControl();
			if (liveCtrl) ctrl = liveCtrl;
			else {
				var shownCtrl = findShownControl();
				if (shownCtrl && ratioOf(shownCtrl) === want) {
					await closeMenu(shownCtrl, true);
					return JSON.stringify({ ok: true });
				}
				if (!ctrl || !ctrl.isConnected) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
				}
			}
			if (ctrl && ratioOf(ctrl) === want) {
				await closeMenu(ctrl, true);
				return JSON.stringify({ ok: true });
			}
			var menu = findMenu();
			var openUntil = Math.min(deadline, Date.now() + 2400);
			var lastOpen = 0;
			while (!menu && Date.now() < openUntil) {
				liveCtrl = findControl();
				if (liveCtrl) ctrl = liveCtrl;
				else if (!ctrl || !ctrl.isConnected) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
				}
				if (ctrl && ratioOf(ctrl) === want) {
					await closeMenu(ctrl, true);
					return JSON.stringify({ ok: true });
				}
				if (lastOpen === 0 || Date.now() - lastOpen >= 1000) {
					if (!await press(ctrl)) {
						if (!findShownControl()) {
							await closeMenu(null, false);
							return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
						}
						lastError = '无法点击千问比例入口';
						break;
					}
					lastOpen = Date.now();
				}
				await new Promise(function (r) { setTimeout(r, 30); });
				menu = findMenu();
			}
			if (!menu) {
				if (!findShownControl()) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
				}
				lastError = '千问比例菜单未出现';
				continue;
			}
			await new Promise(function (r) { setTimeout(r, 25); });
			if (!findShownControl()) {
				await closeMenu(null, false);
				return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
			}
			if (!visible(menu)) {
				lastError = '千问比例菜单已关闭';
				continue;
			}
			var item = findOption(menu);
			if (!item) {
				if (!findShownControl()) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
				}
				await closeMenu(ctrl, false);
				return JSON.stringify({ ok: false, error: '千问比例菜单没有 ' + want });
			}
			if (!await press(item)) {
				await closeMenu(ctrl, false);
				lastError = '无法点击千问比例 ' + want;
				continue;
			}
			var confirmUntil = Math.min(deadline, Date.now() + 1800);
			while (Date.now() < confirmUntil) {
				var again = findShownControl();
				if (again && ratioOf(again) === want) {
					await closeMenu(again, true);
					return JSON.stringify({ ok: true });
				}
				if (!again && (!ctrl || !ctrl.isConnected)) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问比例入口' });
				}
				await new Promise(function (r) { setTimeout(r, 30); });
			}
			await closeMenu(findControl(), false);
		}
		await closeMenu(findControl(), false);
		return JSON.stringify({ ok: false, error: lastError });
	})()`)
		if err != nil {
			return false, err.Error()
		}
		var res struct {
			OK       bool   `json:"ok"`
			Error    string `json:"error"`
			LostMode bool   `json:"lostMode"`
		}
		if err := cdp.ParseJSValue(out, &res); err != nil {
			return false, err.Error()
		}
		if res.OK || !res.LostMode || attempt > 0 {
			return res.OK, res.Error
		}
		if err := ensureQianwenImageMode(s); err != nil {
			return false, err.Error()
		}
	}
	return false, "未找到千问比例入口"
}

// SendPrompt 发送前再次确认千问仍处于「AI生图」模式，防止上传附件或重绘切回普通对话。
func (q Qianwen) SendPrompt(s *cdp.Session, prompt string) error {
	if err := ensureQianwenImageMode(s); err != nil {
		return err
	}
	return q.base.sendPromptWithPostInsertGuard(s, prompt, ensureQianwenImageMode, qianwenImageModeActive)
}

// QwenIntl 千问国际（https://chat.qwen.ai/）策略。
type QwenIntl struct{ base }

// Platform 平台名。
func (QwenIntl) Platform() string { return "千问国际" }

// HomeURL 千问国际入口。
func (QwenIntl) HomeURL() string { return "https://chat.qwen.ai/" }

// qwenIntlImageModeActive 快速同步检查千问国际当前是否仍处于「Create Image / 生成图像」模式。
func qwenIntlImageModeActive(s *cdp.Session) (bool, error) {
	if s == nil {
		return true, nil
	}
	out, err := s.Evaluate(`(() => {
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		var badge = Array.from(document.querySelectorAll('.mode-select-current-mode')).find(shown);
		if (badge && /^(?:Create Image|Image Generation|生成图像|生成圖像)(?:\s|$)/i.test(norm(badge.innerText || badge.textContent))) return true;
		var sub = Array.from(document.querySelectorAll('.message-input-subconfig-visible-item .qwen-chat-v2-dropdown-menu-select,.message-input-column-footer .qwen-chat-v2-dropdown-menu-select')).filter(shown);
		return sub.some(function (el) { return /^\d+:\d+$/.test(norm(el.innerText || el.textContent)); });
	})()`)
	if err != nil {
		return false, err
	}
	var active bool
	if err := cdp.ParseJSValue(out, &active); err != nil {
		return false, err
	}
	return active, nil
}

// ensureQwenIntlImageMode 在千问国际输入区切换并确认进入「Create Image / 生成图像」模式。
func ensureQwenIntlImageMode(s *cdp.Session) error {
	return ensureQwenIntlImageModeWithTimeout(s, 10000)
}

func ensureQwenIntlImageModeWithTimeout(s *cdp.Session, timeoutMs int) error {
	if s == nil {
		return nil
	}
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	s.EnsureBase()
	out, err := s.EvaluateAsync(`(async () => {
		var timeoutMs = ` + fmt.Sprintf("%d", timeoutMs) + `;
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		function visible(el) {
			if (!shown(el)) return false;
			var st = getComputedStyle(el);
			return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
		}
		async function press(el) {
			if (!el || !el.isConnected) return false;
			if (typeof window.tulaiClick === 'function') {
				try { if (await window.tulaiClick(el)) return true; } catch (e) {}
			}
			if (!el.isConnected) return false;
			try {
				if (typeof el.focus === 'function') el.focus();
				var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
				var opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mousedown', opts));
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mouseup', opts));
				el.click();
				return true;
			} catch (e) { return false; }
		}
		function isCreateImageLabel(text) {
			return /^(?:Create Image|Image Generation|生成图像|生成圖像)(?:\s|$)/i.test(norm(text));
		}
		function activeModeBadge() {
			return Array.from(document.querySelectorAll('.mode-select-current-mode')).find(shown) || null;
		}
		function isImageModeActive() {
			var badge = activeModeBadge();
			if (badge && isCreateImageLabel(badge.innerText || badge.textContent)) return true;
			var sub = Array.from(document.querySelectorAll('.message-input-subconfig-visible-item .qwen-chat-v2-dropdown-menu-select,.message-input-column-footer .qwen-chat-v2-dropdown-menu-select')).filter(shown);
			return sub.some(function (el) { return /^\d+:\d+$/.test(norm(el.innerText || el.textContent)); });
		}
		function openMenus() {
			return Array.from(document.querySelectorAll('div[role="menu"].qwen-chat-v2-dropdown-menu-popup,[role="menu"],[role="listbox"]')).filter(function (m) {
				return m.getAttribute('data-state') !== 'closed' && visible(m);
			});
		}
		async function closeOpenMenus() {
			if (!openMenus().length) return;
			try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
			for (var i = 0; i < 6 && openMenus().length; i++) {
				await new Promise(function (r) { setTimeout(r, 25); });
			}
		}
		function findMenuCreateImageItem() {
			var menus = openMenus();
			for (var i = 0; i < menus.length; i++) {
				var items = Array.from(menus[i].querySelectorAll('[role="menuitem"],.qwen-chat-v2-dropdown-menu-item,button,[role="button"]'));
				var hit = items.find(function (el) { return visible(el) && isCreateImageLabel(el.innerText || el.textContent); });
				if (hit) return hit;
			}
			return null;
		}
		function findModeTrigger() {
			var exact = Array.from(document.querySelectorAll('.mode-select-open,[role="button"][aria-label="Select Mode"],[role="button"][aria-label="选择模式"],button[aria-label="Select Mode"],button[aria-label="选择模式"]')).find(visible);
			if (exact) return exact;
			return null;
		}
		function hasLoginWall() {
			if (Array.from(document.querySelectorAll('.user-menu-btn,.sidebar-user .user-profile-desktop,.user-img')).some(visible)) return false;
			if (Array.from(document.querySelectorAll('.header-right-auth-button')).some(visible)) return true;
			return Array.from(document.querySelectorAll('button,[role="button"],a')).some(function (el) {
				return visible(el) && /^(Log in|Sign in|登录|立即登录)$/i.test(norm(el.innerText));
			});
		}
		if (isImageModeActive()) return JSON.stringify({ ok: true, already: true });
		var deadline = Date.now() + timeoutMs;
		var clicks = 0, lastClick = 0;
		var opens = 0, lastOpen = 0;
		var closes = 0, lastClose = 0;
		while (Date.now() < deadline) {
			if (isImageModeActive()) {
				await closeOpenMenus();
				return JSON.stringify({ ok: true });
			}
			var item = findMenuCreateImageItem();
			if (item) {
				if (clicks < 3 && (clicks === 0 || Date.now() - lastClick >= 1400)) {
					await press(item);
					clicks++;
					lastClick = Date.now();
				}
			} else {
				var badge = activeModeBadge();
				var closeBtn = badge && !isCreateImageLabel(badge.innerText || badge.textContent)
					? badge.querySelector('.mode-select-current-mode-close,button,[role="button"]')
					: null;
				if (closeBtn && visible(closeBtn) && closes < 2 && (closes === 0 || Date.now() - lastClose >= 1100)) {
					await press(closeBtn);
					closes++;
					lastClose = Date.now();
				} else if (!openMenus().length) {
					var trigger = findModeTrigger();
					if (trigger && opens < 3 && (opens === 0 || Date.now() - lastOpen >= 1400)) {
						await press(trigger);
						opens++;
						lastOpen = Date.now();
					}
				}
			}
			await new Promise(function (r) { setTimeout(r, 30); });
		}
		if (isImageModeActive()) {
			await closeOpenMenus();
			return JSON.stringify({ ok: true });
		}
		await closeOpenMenus();
		if (clicks === 0 && opens === 0 && hasLoginWall()) {
			return JSON.stringify({ ok: false, error: '请先登录千问国际账号' });
		}
		return JSON.stringify({ ok: false, error: clicks ? '点击千问国际「生成图像」后模式未生效' : '未找到千问国际「生成图像」入口' });
	})()`)
	if err != nil {
		return err
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := cdp.ParseJSValue(out, &res); err != nil {
		return err
	}
	if !res.OK {
		if res.Error == "" {
			res.Error = "模式未生效"
		}
		return fmt.Errorf("进入千问国际生成图像模式: %s", res.Error)
	}
	return nil
}

// EnsureImageMode 确认千问国际处于「Create Image / 生成图像」模式。
func (QwenIntl) EnsureImageMode(s *cdp.Session) error {
	return ensureQwenIntlImageMode(s)
}

// CleanComposer 清理输入区后自动切入千问国际「Create Image / 生成图像」模式。
func (q QwenIntl) CleanComposer(s *cdp.Session) error {
	if err := q.base.CleanComposer(s); err != nil {
		return err
	}
	return ensureQwenIntlImageMode(s)
}

// SetOutputRatio 在千问国际「Create Image / 生成图像」模式下切换原生比例下拉菜单。
// 冷加载时页面异步水合可能短暂重置掉「生成图像」模式，检测到入口脱离 DOM 时会自动重进图像模式并重试。
func (QwenIntl) SetOutputRatio(s *cdp.Session, ratio string) (bool, string) {
	ratio = strings.TrimSpace(ratio)
	if s == nil || ratio == "" {
		return false, ""
	}
	arg, err := json.Marshal(ratio)
	if err != nil {
		return false, err.Error()
	}
	s.EnsureBase()
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := ensureQwenIntlImageMode(s); err != nil {
				return false, err.Error()
			}
		}
		out, err := s.EvaluateAsync(`(async () => {
		var rawWant = String(` + string(arg) + ` || '').trim();
		var wantAuto = /^(?:auto|自动|自动比例)$/i.test(rawWant);
		var want = wantAuto ? 'auto' : rawWant;
		var fastMiss = ` + fmt.Sprintf("%t", attempt == 0) + `;
		if (!wantAuto && !/^\d+:\d+$/.test(want)) return JSON.stringify({ ok: false, error: '比例格式应为 4:3' });
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		function visible(el) {
			if (!shown(el)) return false;
			var st = getComputedStyle(el);
			return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
		}
		function ratioOf(el) {
			if (!el) return '';
			var t = norm(el.innerText || el.textContent || '');
			var m = /^(\d+:\d+)$/.exec(t);
			if (m) return m[1];
			if (/^(?:auto|自动)$/i.test(t)) return 'auto';
			return '';
		}
		function hasImageBadge() {
			return Array.from(document.querySelectorAll('.mode-select-current-mode')).some(function (el) {
				return shown(el) && /^(?:Create Image|Image Generation|生成图像|生成圖像)(?:\s|$)/i.test(norm(el.innerText || el.textContent));
			});
		}
		function findShownControl() {
			var selectors = '.message-input-subconfig-visible-item .qwen-chat-v2-dropdown-menu-select,.message-input-column-footer .qwen-chat-v2-dropdown-menu-select';
			var all = Array.from(document.querySelectorAll(selectors)).filter(function (el) {
				return shown(el) && ratioOf(el) !== '';
			});
			return all[0] || null;
		}
		function findControl() {
			var selectors = '.message-input-subconfig-visible-item .qwen-chat-v2-dropdown-menu-select,.message-input-column-footer .qwen-chat-v2-dropdown-menu-select';
			var all = Array.from(document.querySelectorAll(selectors)).filter(function (el) {
				return visible(el) && ratioOf(el) !== '';
			});
			return all[0] || null;
		}
		function findMenu() {
			return Array.from(document.querySelectorAll('div[role="menu"].qwen-chat-v2-dropdown-menu-popup,[role="menu"],[role="listbox"]')).find(function (m) {
				if (m.getAttribute('data-state') === 'closed') return false;
				if (!visible(m)) return false;
				var txt = norm(m.innerText || m.textContent);
				var pairs = txt.match(/\b\d+:\d+\b/g) || [];
				return pairs.length >= 2;
			}) || null;
		}
		function hasClosingMenu() {
			return Array.from(document.querySelectorAll('[role="menu"][data-state="closed"],[role="listbox"][data-state="closed"]')).some(shown);
		}
		function findOption(menu) {
			if (!menu) return null;
			var items = Array.from(menu.querySelectorAll('[role="menuitem"],[role="option"],.qwen-chat-v2-dropdown-menu-item'));
			return items.find(function (el) {
				if (!visible(el)) return false;
				var txt = norm(el.innerText || el.textContent);
				return want === 'auto' ? /^(?:auto|自动|自动比例)$/i.test(txt) : txt === want;
			}) || null;
		}
		async function press(el) {
			if (!el || !el.isConnected) return false;
			if (typeof window.tulaiClick === 'function') {
				try { if (await window.tulaiClick(el)) return true; } catch (e) {}
			}
			if (!el.isConnected) return false;
			try {
				if (typeof el.focus === 'function') el.focus();
				var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
				var opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mousedown', opts));
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mouseup', opts));
				el.click();
				return true;
			} catch (e) { return false; }
		}
		async function closeMenu(ctrl, waitNatural) {
			if (waitNatural) {
				for (var w = 0; w < 12 && (findMenu() || hasClosingMenu()); w++) {
					await new Promise(function (r) { setTimeout(r, 25); });
				}
				if (!findMenu()) return;
			}
			if (!findMenu()) return;
			try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
			for (var i = 0; i < 6 && findMenu(); i++) {
				await new Promise(function (r) { setTimeout(r, 25); });
			}
			if (findMenu() && ctrl && visible(ctrl)) {
				try { await press(ctrl); } catch (e) {}
				await new Promise(function (r) { setTimeout(r, 40); });
			}
		}
		var ctrl = findShownControl();
		if (!ctrl && fastMiss && !hasImageBadge()) {
			return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
		}
		var deadline = Date.now() + 8000;
		while (!ctrl && Date.now() < deadline) {
			ctrl = findShownControl();
			if (ctrl) break;
			await new Promise(function (r) { setTimeout(r, 30); });
		}
		if (!ctrl) return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
		if (ratioOf(ctrl) === want) {
			if (findMenu()) await closeMenu(ctrl, false);
			return JSON.stringify({ ok: true, already: true });
		}
		if (!visible(ctrl)) {
			var unlockUntil = Math.min(deadline, Date.now() + 600);
			while (Date.now() < unlockUntil) {
				var cur = findShownControl();
				if (!cur) return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
				ctrl = cur;
				if (ratioOf(ctrl) === want) return JSON.stringify({ ok: true, already: true });
				if (visible(ctrl)) break;
				await new Promise(function (r) { setTimeout(r, 30); });
			}
			if (!visible(ctrl)) {
				return JSON.stringify({ ok: false, error: '千问国际上传参考图后比例已锁定为 ' + ratioOf(ctrl) + '，无法改为 ' + want });
			}
		}
		while (Date.now() < deadline && hasClosingMenu()) {
			await new Promise(function (r) { setTimeout(r, 25); });
		}
		var lastError = '千问国际比例 ' + want + ' 未生效';
		for (var round = 0; round < 3 && Date.now() < deadline; round++) {
			var liveCtrl = findControl();
			if (liveCtrl) ctrl = liveCtrl;
			else {
				var shownCtrl = findShownControl();
				if (shownCtrl && ratioOf(shownCtrl) === want) {
					await closeMenu(shownCtrl, true);
					return JSON.stringify({ ok: true });
				}
				if (!ctrl || !ctrl.isConnected) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
				}
			}
			if (ctrl && ratioOf(ctrl) === want) {
				await closeMenu(ctrl, true);
				return JSON.stringify({ ok: true });
			}
			var menu = findMenu();
			var openUntil = Math.min(deadline, Date.now() + 2400);
			var lastOpen = 0;
			while (!menu && Date.now() < openUntil) {
				liveCtrl = findControl();
				if (liveCtrl) ctrl = liveCtrl;
				else if (!ctrl || !ctrl.isConnected) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
				}
				if (ctrl && ratioOf(ctrl) === want) {
					await closeMenu(ctrl, true);
					return JSON.stringify({ ok: true });
				}
				if (lastOpen === 0 || Date.now() - lastOpen >= 1000) {
					if (!await press(ctrl)) {
						if (!findShownControl()) {
							await closeMenu(null, false);
							return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
						}
						lastError = '无法点击千问国际比例入口';
						break;
					}
					lastOpen = Date.now();
				}
				await new Promise(function (r) { setTimeout(r, 30); });
				menu = findMenu();
			}
			if (!menu) {
				if (!findShownControl()) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
				}
				lastError = '千问国际比例菜单未出现';
				continue;
			}
			await new Promise(function (r) { setTimeout(r, 25); });
			if (!findShownControl()) {
				await closeMenu(null, false);
				return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
			}
			if (!visible(menu)) {
				lastError = '千问国际比例菜单已关闭';
				continue;
			}
			var item = findOption(menu);
			if (!item) {
				if (!findShownControl()) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
				}
				await closeMenu(ctrl, false);
				return JSON.stringify({ ok: false, error: '千问国际比例菜单没有 ' + want });
			}
			if (!await press(item)) {
				await closeMenu(ctrl, false);
				lastError = '无法点击千问国际比例 ' + want;
				continue;
			}
			var confirmUntil = Math.min(deadline, Date.now() + 1800);
			while (Date.now() < confirmUntil) {
				var again = findShownControl();
				if (again && ratioOf(again) === want) {
					await closeMenu(again, true);
					return JSON.stringify({ ok: true });
				}
				if (!again && (!ctrl || !ctrl.isConnected)) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到千问国际比例入口' });
				}
				await new Promise(function (r) { setTimeout(r, 30); });
			}
			await closeMenu(findControl(), false);
		}
		await closeMenu(findControl(), false);
		return JSON.stringify({ ok: false, error: lastError });
	})()`)
		if err != nil {
			return false, err.Error()
		}
		var res struct {
			OK       bool   `json:"ok"`
			Error    string `json:"error"`
			LostMode bool   `json:"lostMode"`
		}
		if err := cdp.ParseJSValue(out, &res); err != nil {
			return false, err.Error()
		}
		if res.OK || !res.LostMode {
			return res.OK, res.Error
		}
	}
	return false, "未找到千问国际比例入口"
}

// SendPrompt 发送前确认千问国际仍处于「Create Image / 生成图像」模式。
func (q QwenIntl) SendPrompt(s *cdp.Session, prompt string) error {
	if err := ensureQwenIntlImageMode(s); err != nil {
		return err
	}
	return q.base.sendPromptWithPostInsertGuard(s, prompt, ensureQwenIntlImageMode, qwenIntlImageModeActive)
}

// WaitForResult 等待千问国际结果，断页重开时始终回到 https://chat.qwen.ai/c/<id>，不回退国内站。
func (QwenIntl) WaitForResult(s *cdp.Session, timeout time.Duration) ([]Result, error) {
	return waitForResultFor(s, "千问国际", timeout, nil)
}

// FetchResultBytes 千问国际优先去掉 OSS 缩略图处理参数拉取原图，签名绑定参数时回退原始地址。
func (q QwenIntl) FetchResultBytes(s *cdp.Session, raw string) ([]byte, error) {
	tried := map[string]bool{}
	var last error
	for _, candidate := range []string{stripOSSProcessURL(raw), raw} {
		if candidate == "" || tried[candidate] {
			continue
		}
		tried[candidate] = true
		body, err := q.base.FetchResultBytes(s, candidate)
		if err == nil {
			return body, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("缺少图片地址")
	}
	return nil, last
}

func stripOSSProcessURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.RawQuery == "" {
		return raw
	}
	q := u.Query()
	if !q.Has("x-oss-process") {
		return raw
	}
	q.Del("x-oss-process")
	u.RawQuery = q.Encode()
	return u.String()
}

// Gemini Google Gemini（https://gemini.google.com/）策略。
type Gemini struct{ base }

// Platform 平台名。
func (Gemini) Platform() string { return "Gemini" }

// HomeURL Gemini 入口。
func (Gemini) HomeURL() string { return "https://gemini.google.com/app" }

// geminiImageModeActive 快速同步检查 Gemini 当前是否仍处于「制作图片 / Create image」模式。
func geminiImageModeActive(s *cdp.Session) (bool, error) {
	if s == nil {
		return true, nil
	}
	out, err := s.Evaluate(`(() => {
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function cleanText(el) {
			if (!el) return '';
			if (document.createTreeWalker && typeof NodeFilter !== 'undefined') {
				var out = '', w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT), n;
				while ((n = w.nextNode())) {
					var skip = false;
					for (var p = n.parentElement; p && p !== el; p = p.parentElement) {
						var tag = String(p.tagName || '').toLowerCase();
						if (tag === 'mat-icon' || tag === 'svg' || p.getAttribute('aria-hidden') === 'true') { skip = true; break; }
					}
					if (!skip) out += ' ' + (n.nodeValue || '');
				}
				return norm(out);
			}
			return norm(el.innerText || el.textContent);
		}
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		var ed = document.querySelector('rich-textarea .ql-editor[contenteditable="true"],textarea');
		if (ed) {
			var ph = norm((ed.getAttribute && (ed.getAttribute('data-placeholder') || ed.getAttribute('aria-label') || ed.getAttribute('placeholder'))) || '');
			if (/描述你的图片|描述你的图像|Describe your image/i.test(ph)) return true;
		}
		var scope = document.querySelector('input-area-v2,input-container') || document;
		var btns = Array.from(scope.querySelectorAll('button,[role="button"]')).filter(shown);
		return btns.some(function (el) {
			var txt = cleanText(el);
			var lbl = norm((el.getAttribute && el.getAttribute('aria-label')) || '');
			var icon = el.querySelector && el.querySelector('mat-icon');
			var iconName = (icon && (icon.getAttribute('fonticon') || icon.getAttribute('data-mat-icon-name') || norm(icon.textContent))) || '';
			if (iconName === 'aspect_ratio' || /^(?:宽高比|Aspect ratio)(?:\s|$)/i.test(txt) || /^(?:宽高比|Aspect ratio)(?:\s|$)/i.test(lbl)) return true;
			if (el.closest && el.closest('toolbox-drawer') && !el.getAttribute('aria-haspopup')) {
				if (el.classList && (el.classList.contains('toolbox-drawer-item-deselect-button') || el.classList.contains('has-selected-item'))) {
					if (/图片|图像|image/i.test(txt + ' ' + lbl) || iconName === 'image_create' || iconName === 'photo_prints') return true;
				}
				if (iconName === 'image_create' || iconName === 'photo_prints') return true;
				if (/^(?:图片|制作图片|生成图片|Image|Create image)$/i.test(txt)) return true;
			}
			if (el.classList && el.classList.contains('toolbox-drawer-item-deselect-button')) {
				if (/图片|图像|image/i.test(txt + ' ' + lbl) || iconName === 'image_create' || iconName === 'photo_prints') return true;
			}
			return false;
		});
	})()`)
	if err != nil {
		return false, err
	}
	var active bool
	if err := cdp.ParseJSValue(out, &active); err != nil {
		return false, err
	}
	return active, nil
}

// ensureGeminiImageMode 在 Gemini 输入区通过「+ → 制作图片」切换并确认进入图像生成模式，并保证输出格式停留在「图片」（非 GIF）。
func ensureGeminiImageMode(s *cdp.Session) error {
	return ensureGeminiImageModeWithTimeout(s, 10000)
}

func ensureGeminiImageModeWithTimeout(s *cdp.Session, timeoutMs int) error {
	if s == nil {
		return nil
	}
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	s.EnsureBase()
	out, err := s.EvaluateAsync(`(async () => {
		var timeoutMs = ` + fmt.Sprintf("%d", timeoutMs) + `;
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function cleanText(el) {
			if (!el) return '';
			if (document.createTreeWalker && typeof NodeFilter !== 'undefined') {
				var out = '', w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT), n;
				while ((n = w.nextNode())) {
					var skip = false;
					for (var p = n.parentElement; p && p !== el; p = p.parentElement) {
						var tag = String(p.tagName || '').toLowerCase();
						if (tag === 'mat-icon' || tag === 'svg' || p.getAttribute('aria-hidden') === 'true') { skip = true; break; }
					}
					if (!skip) out += ' ' + (n.nodeValue || '');
				}
				return norm(out);
			}
			return norm(el.innerText || el.textContent);
		}
		function iconNameOf(el) {
			if (!el || !el.querySelector) return '';
			var icon = el.querySelector('mat-icon');
			if (!icon) return '';
			return norm(icon.getAttribute('fonticon') || icon.getAttribute('data-mat-icon-name') || icon.textContent || '');
		}
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		function visible(el) {
			if (!shown(el)) return false;
			var st = getComputedStyle(el);
			return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
		}
		async function press(el) {
			if (!el || !el.isConnected) return false;
			if (typeof window.tulaiClick === 'function') {
				try { if (await window.tulaiClick(el)) return true; } catch (e) {}
			}
			if (!el.isConnected) return false;
			try {
				if (typeof el.focus === 'function') el.focus();
				var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
				var opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mousedown', opts));
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mouseup', opts));
				el.click();
				return true;
			} catch (e) { return false; }
		}
		function composerScope() {
			return document.querySelector('input-area-v2,input-container') || document;
		}
		function isCreateImageLabel(text) {
			var t = norm(text);
			return /^(?:制作图片|生成图片|创建图片|Create image|Create images|Generate image)$/i.test(t);
		}
		function isImageModeActive() {
			var ed = document.querySelector('rich-textarea .ql-editor[contenteditable="true"],textarea');
			if (ed) {
				var ph = norm((ed.getAttribute && (ed.getAttribute('data-placeholder') || ed.getAttribute('aria-label') || ed.getAttribute('placeholder'))) || '');
				if (/描述你的图片|描述你的图像|Describe your image/i.test(ph)) return true;
			}
			var btns = Array.from(composerScope().querySelectorAll('button,[role="button"]')).filter(shown);
			return btns.some(function (el) {
				var txt = cleanText(el);
				var lbl = norm((el.getAttribute && el.getAttribute('aria-label')) || '');
				var ic = iconNameOf(el);
				if (ic === 'aspect_ratio' || /^(?:宽高比|Aspect ratio)(?:\s|$)/i.test(txt) || /^(?:宽高比|Aspect ratio)(?:\s|$)/i.test(lbl)) return true;
				if (el.closest && el.closest('toolbox-drawer') && !el.getAttribute('aria-haspopup')) {
					if (el.classList && (el.classList.contains('toolbox-drawer-item-deselect-button') || el.classList.contains('has-selected-item'))) {
						if (/图片|图像|image/i.test(txt + ' ' + lbl) || ic === 'image_create' || ic === 'photo_prints') return true;
					}
					if (ic === 'image_create' || ic === 'photo_prints') return true;
					if (/^(?:图片|制作图片|生成图片|Image|Create image)$/i.test(txt)) return true;
				}
				if (el.classList && el.classList.contains('toolbox-drawer-item-deselect-button')) {
					if (/图片|图像|image/i.test(txt + ' ' + lbl) || ic === 'image_create' || ic === 'photo_prints') return true;
				}
				return false;
			});
		}
		function openMenus() {
			return Array.from(document.querySelectorAll('.cdk-overlay-container [role="menu"],.cdk-overlay-container .mat-mdc-menu-panel,mat-action-list[role="menu"],[role="menu"],[role="listbox"]')).filter(function (m) {
				return m.getAttribute('data-state') !== 'closed' && visible(m);
			});
		}
		async function closeOpenMenus() {
			if (!openMenus().length) return;
			try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
			for (var i = 0; i < 6 && openMenus().length; i++) {
				await new Promise(function (r) { setTimeout(r, 25); });
			}
		}
		function findMenuCreateImageItem() {
			var menus = openMenus();
			for (var i = menus.length - 1; i >= 0; i--) {
				var items = Array.from(menus[i].querySelectorAll('button,[role="menuitem"],[role="menuitemcheckbox"],[role="menuitemradio"],[role="option"]'));
				var hit = items.find(function (el) {
					if (!visible(el)) return false;
					var ic = iconNameOf(el);
					if (ic === 'image_create' || ic === 'photo_prints') return true;
					var txt = cleanText(el);
					var lbl = norm((el.getAttribute && el.getAttribute('aria-label')) || '');
					return isCreateImageLabel(txt) || isCreateImageLabel(lbl);
				});
				if (hit) return hit;
			}
			return null;
		}
		function findMoreToolsMenuItem() {
			var menus = openMenus();
			for (var i = menus.length - 1; i >= 0; i--) {
				var items = Array.from(menus[i].querySelectorAll('button,[role="menuitem"],[role="menuitemcheckbox"]'));
				var hit = items.find(function (el) {
					if (!visible(el)) return false;
					var txt = cleanText(el);
					return /^(?:更多工具|More tools)$/i.test(txt);
				});
				if (hit) return hit;
			}
			return null;
		}
		function findPlusMenuTrigger() {
			var scope = composerScope();
			var preferred = [
				'simplified-input-menu button[aria-haspopup="menu"]',
				'gem-icon-button.gem-menu-button button',
				'button[aria-label="上传和工具"]',
				'button[aria-label="Upload & tools"]',
				'button[aria-label*="上传和工具"]',
				'button[aria-label*="Upload & tools" i]',
				'toolbox-drawer button[aria-label*="工具"]',
				'toolbox-drawer button[aria-label*="Tools" i]'
			];
			for (var i = 0; i < preferred.length; i++) {
				var hit = Array.from(scope.querySelectorAll(preferred[i])).find(visible);
				if (hit) return hit;
			}
			return Array.from(scope.querySelectorAll('button')).find(function (el) {
				if (!visible(el)) return false;
				var ic = iconNameOf(el);
				return ic === 'add' || ic === 'tune' || ic === 'build';
			}) || null;
		}
		function findConflictingChip() {
			var scope = composerScope();
			return Array.from(scope.querySelectorAll('.toolbox-drawer-item-deselect-button')).find(function (el) {
				if (!visible(el)) return false;
				var txt = cleanText(el);
				var ic = iconNameOf(el);
				return !/图片|图像|image/i.test(txt) && ic !== 'image_create' && ic !== 'photo_prints';
			}) || null;
		}
		// 若格式下拉意外停在 GIF，切回「图片 / Image」（当前为「图片」时不触碰，避免与模式胶囊混淆）。
		async function ensureStillImageFormat(deadline) {
			var scope = composerScope();
			var gifBtn = Array.from(scope.querySelectorAll('button,[role="button"]')).find(function (el) {
				if (!visible(el)) return false;
				var txt = cleanText(el);
				var ic = iconNameOf(el);
				return /^GIF$/i.test(txt) || ic === 'gif_box';
			});
			if (!gifBtn) return;
			await press(gifBtn);
			var waitUntil = Math.min(deadline, Date.now() + 1800);
			while (Date.now() < waitUntil) {
				var menus = openMenus();
				for (var i = menus.length - 1; i >= 0; i--) {
					var opt = Array.from(menus[i].querySelectorAll('button,[role="menuitem"],[role="menuitemradio"],[role="menuitemcheckbox"],[role="option"]')).find(function (el) {
						if (!visible(el)) return false;
						var t = cleanText(el);
						return /^(?:图片|图像|Image)$/i.test(t);
					});
					if (opt) {
						await press(opt);
						await new Promise(function (r) { setTimeout(r, 80); });
						await closeOpenMenus();
						return;
					}
				}
				await new Promise(function (r) { setTimeout(r, 30); });
			}
			await closeOpenMenus();
		}
		function hasLoginWall() {
			if (Array.from(document.querySelectorAll('[gem-open-account-menu],a[href*="accounts.google.com/SignOutOptions"],user-profile-picture img[src]')).some(visible)) return false;
			if (Array.from(document.querySelectorAll('[data-test-id="sidenav-error-action-link"],[data-test-id="sign-out-banner"]')).some(visible)) return true;
			return Array.from(document.querySelectorAll('button,[role="button"],a')).some(function (el) {
				return visible(el) && /^(Sign in|Log in|登录|立即登录)$/i.test(cleanText(el));
			});
		}
		var deadline = Date.now() + timeoutMs;
		if (isImageModeActive()) {
			await ensureStillImageFormat(deadline);
			return JSON.stringify({ ok: true, already: true });
		}
		var clicks = 0, lastClick = 0;
		var opens = 0, lastOpen = 0;
		var moreOpens = 0, lastMoreOpen = 0;
		var exits = 0, lastExit = 0;
		while (Date.now() < deadline) {
			if (isImageModeActive()) {
				await closeOpenMenus();
				await ensureStillImageFormat(deadline);
				return JSON.stringify({ ok: true });
			}
			var item = findMenuCreateImageItem();
			if (item) {
				if (clicks < 3 && (clicks === 0 || Date.now() - lastClick >= 1400)) {
					await press(item);
					clicks++;
					lastClick = Date.now();
				}
			} else if (openMenus().length) {
				var more = findMoreToolsMenuItem();
				if (more && moreOpens < 2 && (moreOpens === 0 || Date.now() - lastMoreOpen >= 1000)) {
					await press(more);
					moreOpens++;
					lastMoreOpen = Date.now();
				}
			} else {
				var conflict = findConflictingChip();
				if (conflict && exits < 2 && (exits === 0 || Date.now() - lastExit >= 1000)) {
					await press(conflict);
					exits++;
					lastExit = Date.now();
				} else {
					var trigger = findPlusMenuTrigger();
					if (trigger && opens < 3 && (opens === 0 || Date.now() - lastOpen >= 1400)) {
						await press(trigger);
						opens++;
						lastOpen = Date.now();
					}
				}
			}
			await new Promise(function (r) { setTimeout(r, 30); });
		}
		if (isImageModeActive()) {
			await closeOpenMenus();
			await ensureStillImageFormat(deadline);
			return JSON.stringify({ ok: true });
		}
		await closeOpenMenus();
		if (clicks === 0 && opens === 0 && hasLoginWall()) {
			return JSON.stringify({ ok: false, error: '请先登录 Gemini 账号' });
		}
		return JSON.stringify({ ok: false, error: clicks ? '点击 Gemini「制作图片」后模式未生效' : '未找到 Gemini「制作图片」入口' });
	})()`)
	if err != nil {
		return err
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := cdp.ParseJSValue(out, &res); err != nil {
		return err
	}
	if !res.OK {
		if res.Error == "" {
			res.Error = "模式未生效"
		}
		return fmt.Errorf("进入 Gemini 制作图片模式: %s", res.Error)
	}
	return nil
}

// EnsureImageMode 确认 Gemini 处于「制作图片 / Create image」模式。
func (Gemini) EnsureImageMode(s *cdp.Session) error {
	return ensureGeminiImageMode(s)
}

// CleanComposer 清理输入区后自动切入 Gemini「制作图片 / Create image」模式。
func (g Gemini) CleanComposer(s *cdp.Session) error {
	if err := g.base.CleanComposer(s); err != nil {
		return err
	}
	return ensureGeminiImageMode(s)
}

// SetOutputRatio 在 Gemini「制作图片」模式下切换原生「宽高比」下拉菜单。
// 不再写死 5 个比例，支持网页当前菜单中实际出现的任意 W:H 比例（或自动/默认项）。
func (Gemini) SetOutputRatio(s *cdp.Session, ratio string) (bool, string) {
	ratio = strings.TrimSpace(strings.ReplaceAll(ratio, "：", ":"))
	if s == nil || ratio == "" {
		return false, ""
	}
	arg, err := json.Marshal(ratio)
	if err != nil {
		return false, err.Error()
	}
	s.EnsureBase()
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := ensureGeminiImageMode(s); err != nil {
				return false, err.Error()
			}
		}
		out, err := s.EvaluateAsync(`(async () => {
		var want = String(` + string(arg) + ` || '').trim();
		var wantAuto = /^(?:自动|默认|默认比例|Auto|Default)$/i.test(want);
		if (!wantAuto && !/^\d+:\d+$/.test(want)) return JSON.stringify({ ok: false, error: 'Gemini 比例格式应为 4:3' });
		var fastMiss = ` + fmt.Sprintf("%t", attempt == 0) + `;
		function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
		function cleanText(el) {
			if (!el) return '';
			if (document.createTreeWalker && typeof NodeFilter !== 'undefined') {
				var out = '', w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT), n;
				while ((n = w.nextNode())) {
					var skip = false;
					for (var p = n.parentElement; p && p !== el; p = p.parentElement) {
						var tag = String(p.tagName || '').toLowerCase();
						if (tag === 'mat-icon' || tag === 'svg' || p.getAttribute('aria-hidden') === 'true') { skip = true; break; }
					}
					if (!skip) out += ' ' + (n.nodeValue || '');
				}
				return norm(out);
			}
			return norm(el.innerText || el.textContent);
		}
		function iconNameOf(el) {
			if (!el || !el.querySelector) return '';
			var icon = el.querySelector('mat-icon');
			if (!icon) return '';
			return norm(icon.getAttribute('fonticon') || icon.getAttribute('data-mat-icon-name') || icon.textContent || '');
		}
		function shown(el) {
			if (!el || !el.isConnected) return false;
			if (el.closest && el.closest('[aria-hidden="true"]')) return false;
			var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
			if (r.width <= 5 || r.height <= 5) return false;
			var st = getComputedStyle(el);
			return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
		}
		function visible(el) {
			if (!shown(el)) return false;
			var st = getComputedStyle(el);
			return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
		}
		function ratioOf(el) {
			if (!el) return '';
			var txt = cleanText(el);
			var lbl = norm((el.getAttribute && el.getAttribute('aria-label')) || '');
			var m = /\b(\d+:\d+)\b/.exec(txt) || /\b(\d+:\d+)\b/.exec(lbl);
			if (m) return m[1];
			var ic = iconNameOf(el);
			if (ic === 'aspect_ratio' || /^(?:宽高比|Aspect ratio|自动|默认|Auto|Default)$/i.test(txt) || /(?:宽高比|Aspect ratio)/i.test(lbl)) return 'default';
			return '';
		}
		function matchesWant(el) {
			var cur = ratioOf(el);
			return wantAuto ? cur === 'default' : cur === want;
		}
		function composerScope() {
			return document.querySelector('input-area-v2,input-container') || document;
		}
		function findShownControl() {
			var all = Array.from(composerScope().querySelectorAll('button,[role="button"]')).filter(function (el) {
				if (!shown(el)) return false;
				if (el.closest && el.closest('.cdk-overlay-container,[role="menu"],[role="listbox"]')) return false;
				return ratioOf(el) !== '';
			});
			if (!all.length) return null;
			return all.find(function (el) { return iconNameOf(el) === 'aspect_ratio' || el.getAttribute('aria-haspopup'); }) || all[0];
		}
		function findControl() {
			var all = Array.from(composerScope().querySelectorAll('button,[role="button"]')).filter(function (el) {
				if (!visible(el)) return false;
				if (el.closest && el.closest('.cdk-overlay-container,[role="menu"],[role="listbox"]')) return false;
				return ratioOf(el) !== '';
			});
			if (!all.length) return null;
			return all.find(function (el) { return iconNameOf(el) === 'aspect_ratio' || el.getAttribute('aria-haspopup'); }) || all[0];
		}
		function findMenu() {
			return Array.from(document.querySelectorAll('.cdk-overlay-container [role="menu"],.cdk-overlay-container [role="listbox"],.cdk-overlay-container .mat-mdc-menu-panel,[role="menu"],[role="listbox"]')).find(function (m) {
				if (m.getAttribute('data-state') === 'closed') return false;
				if (!visible(m)) return false;
				var txt = norm(m.innerText || m.textContent);
				var pairs = txt.match(/\b\d+:\d+\b/g) || [];
				return pairs.length >= 2;
			}) || null;
		}
		function findOption(menu) {
			if (!menu) return null;
			var items = Array.from(menu.querySelectorAll('[role="menuitem"],[role="menuitemradio"],[role="menuitemcheckbox"],[role="option"],button,.mat-mdc-menu-item'));
			if (wantAuto) {
				return items.find(function (el) {
					return visible(el) && /^(?:自动|默认|默认比例|Auto|Default)$/i.test(cleanText(el));
				}) || null;
			}
			var exact = items.find(function (el) { return visible(el) && cleanText(el) === want; });
			if (exact) return exact;
			return items.find(function (el) {
				if (!visible(el)) return false;
				var m = /\b(\d+:\d+)\b/.exec(cleanText(el));
				return !!(m && m[1] === want);
			}) || null;
		}
		async function press(el) {
			if (!el || !el.isConnected) return false;
			if (typeof window.tulaiClick === 'function') {
				try { if (await window.tulaiClick(el)) return true; } catch (e) {}
			}
			if (!el.isConnected) return false;
			try {
				if (typeof el.focus === 'function') el.focus();
				var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
				var opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerdown', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mousedown', opts));
				if (typeof PointerEvent === 'function') {
					el.dispatchEvent(new PointerEvent('pointerup', Object.assign({ pointerId: 1, pointerType: 'mouse', isPrimary: true }, opts)));
				}
				el.dispatchEvent(new MouseEvent('mouseup', opts));
				el.click();
				return true;
			} catch (e) { return false; }
		}
		async function closeMenu(ctrl, waitNatural) {
			if (waitNatural) {
				for (var w = 0; w < 10 && findMenu(); w++) {
					await new Promise(function (r) { setTimeout(r, 25); });
				}
				if (!findMenu()) return;
			}
			if (!findMenu()) return;
			try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
			for (var i = 0; i < 6 && findMenu(); i++) {
				await new Promise(function (r) { setTimeout(r, 25); });
			}
			if (findMenu() && ctrl && visible(ctrl)) {
				try { await press(ctrl); } catch (e) {}
				await new Promise(function (r) { setTimeout(r, 40); });
			}
		}
		var ctrl = findShownControl();
		if (!ctrl && fastMiss) {
			return JSON.stringify({ ok: false, lostMode: true, error: '未找到 Gemini 宽高比入口' });
		}
		var deadline = Date.now() + 8000;
		while (!ctrl && Date.now() < deadline) {
			ctrl = findShownControl();
			if (ctrl) break;
			await new Promise(function (r) { setTimeout(r, 30); });
		}
		if (!ctrl) return JSON.stringify({ ok: false, lostMode: true, error: '未找到 Gemini 宽高比入口' });
		if (matchesWant(ctrl)) {
			if (findMenu()) await closeMenu(ctrl, false);
			return JSON.stringify({ ok: true, already: true });
		}
		var lastError = 'Gemini 宽高比 ' + want + ' 未生效';
		for (var round = 0; round < 3 && Date.now() < deadline; round++) {
			var liveCtrl = findControl();
			if (liveCtrl) ctrl = liveCtrl;
			else if (!ctrl || !ctrl.isConnected) {
				await closeMenu(null, false);
				return JSON.stringify({ ok: false, lostMode: true, error: '未找到 Gemini 宽高比入口' });
			}
			if (ctrl && matchesWant(ctrl)) {
				await closeMenu(ctrl, true);
				return JSON.stringify({ ok: true });
			}
			var menu = findMenu();
			var openUntil = Math.min(deadline, Date.now() + 2400);
			var lastOpen = 0;
			while (!menu && Date.now() < openUntil) {
				liveCtrl = findControl();
				if (liveCtrl) ctrl = liveCtrl;
				else if (!ctrl || !ctrl.isConnected) {
					await closeMenu(null, false);
					return JSON.stringify({ ok: false, lostMode: true, error: '未找到 Gemini 宽高比入口' });
				}
				if (lastOpen === 0 || Date.now() - lastOpen >= 1000) {
					if (!await press(ctrl)) {
						lastError = '无法点击 Gemini 宽高比入口';
						break;
					}
					lastOpen = Date.now();
				}
				await new Promise(function (r) { setTimeout(r, 30); });
				menu = findMenu();
			}
			if (!menu) {
				lastError = 'Gemini 宽高比菜单未出现';
				continue;
			}
			await new Promise(function (r) { setTimeout(r, 25); });
			var item = findOption(menu);
			if (!item) {
				await closeMenu(ctrl, false);
				return JSON.stringify({ ok: false, error: 'Gemini 宽高比菜单没有 ' + want });
			}
			if (!await press(item)) {
				await closeMenu(ctrl, false);
				lastError = '无法点击 Gemini 宽高比 ' + want;
				continue;
			}
			var confirmUntil = Math.min(deadline, Date.now() + 1800);
			while (Date.now() < confirmUntil) {
				var again = findShownControl();
				if (again && matchesWant(again)) {
					await closeMenu(again, true);
					return JSON.stringify({ ok: true });
				}
				await new Promise(function (r) { setTimeout(r, 30); });
			}
			await closeMenu(findControl(), false);
		}
		await closeMenu(findControl(), false);
		return JSON.stringify({ ok: false, error: lastError });
	})()`)
		if err != nil {
			return false, err.Error()
		}
		var res struct {
			OK       bool   `json:"ok"`
			Error    string `json:"error"`
			LostMode bool   `json:"lostMode"`
		}
		if err := cdp.ParseJSValue(out, &res); err != nil {
			return false, err.Error()
		}
		if res.OK || !res.LostMode {
			return res.OK, res.Error
		}
	}
	return false, "未找到 Gemini 宽高比入口"
}

// SendPrompt 发送前确认 Gemini 仍处于「制作图片 / Create image」模式。
func (g Gemini) SendPrompt(s *cdp.Session, prompt string) error {
	if err := ensureGeminiImageMode(s); err != nil {
		return err
	}
	return g.base.sendPromptWithPostInsertGuard(s, prompt, ensureGeminiImageMode, geminiImageModeActive)
}

// WaitForResult 等待 Gemini 生成结果，断页重开时始终回到 https://gemini.google.com/app/<id>。
func (Gemini) WaitForResult(s *cdp.Session, timeout time.Duration) ([]Result, error) {
	return waitForResultFor(s, "Gemini", timeout, nil)
}

// FetchResultBytes Gemini 优先尝试将 lh3.googleusercontent.com 尺寸后缀升级为 =s0 拉取全分辨率原图，签名拒绝时回退原始地址。
func (g Gemini) FetchResultBytes(s *cdp.Session, raw string) ([]byte, error) {
	tried := map[string]bool{}
	var last error
	for _, candidate := range []string{geminiOriginalImageURL(raw), raw} {
		if candidate == "" || tried[candidate] {
			continue
		}
		tried[candidate] = true
		body, err := g.base.FetchResultBytes(s, candidate)
		if err == nil {
			return body, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("缺少图片地址")
	}
	return nil, last
}

var geminiSizeModifierRe = regexp.MustCompile(`=(?:[swh]\d+)(?:-[a-zA-Z0-9]+)*$`)

func geminiOriginalImageURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || !strings.Contains(strings.ToLower(u.Host), "googleusercontent.com") {
		return trimmed
	}
	slash := strings.LastIndex(u.Path, "/")
	eq := strings.LastIndex(u.Path, "=")
	if eq <= slash+1 {
		return trimmed
	}
	suffix := u.Path[eq:]
	if strings.EqualFold(suffix, "=s0") || !geminiSizeModifierRe.MatchString(suffix) {
		return trimmed
	}
	u.Path = u.Path[:eq] + "=s0"
	return u.String()
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ","); i >= 0 && strings.HasPrefix(s, "data:") {
		s = s[i+1:]
	}
	return base64.StdEncoding.DecodeString(s)
}
