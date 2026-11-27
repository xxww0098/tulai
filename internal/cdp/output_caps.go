package cdp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/store"
)

type rawOutputCapsProbe struct {
	OK                bool     `json:"ok"`
	Error             string   `json:"error,omitempty"`
	Platform          string   `json:"platform,omitempty"`
	Model             string   `json:"model,omitempty"`
	SupportsAuto      bool     `json:"supportsAuto"`
	AutoValue         string   `json:"autoValue,omitempty"`
	AutoLabel         string   `json:"autoLabel,omitempty"`
	Ratios            []string `json:"ratios"`
	DefaultRatio      string   `json:"defaultRatio,omitempty"`
	Resolutions       []string `json:"resolutions,omitempty"`
	DefaultResolution string   `json:"defaultResolution,omitempty"`
	CurrentRatio      string   `json:"currentRatio,omitempty"`
	CurrentResolution string   `json:"currentResolution,omitempty"`
}

func doubaoOutputCapsProbeJS() string {
	pageJS := extbundle.DoubaoPageJS()
	rev := fmt.Sprintf("%x", sha256.Sum256([]byte(pageJS)))
	return `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)doubao\.com$/.test(host)) {
		return { ok: false, error: '当前页面不是豆包' };
	}
	if (!window.__tulaiWashPage || window.__tulaiWashPageRev !== ` + fmt.Sprintf("%q", rev) + `) {
		` + pageJS + `
		window.__tulaiWashPage = window.TULAIPage;
		window.__tulaiWashPageRev = ` + fmt.Sprintf("%q", rev) + `;
	}
	if (!window.__tulaiWashPage || typeof window.__tulaiWashPage.probeOutputCaps !== 'function') {
		return { ok: false, error: '豆包页面脚本不支持输出规格探测' };
	}
	return await window.__tulaiWashPage.probeOutputCaps(document, 6500);
})()`
}

const qianwenOutputCapsProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)(qianwen|tongyi)\.com$/.test(host)) {
		return { ok: false, error: '当前页面不是千问' };
	}
	const norm = (v) => String(v || '').replace(/\s+/g, ' ').trim();
	const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
	const shown = (el) => {
		if (!el || !el.isConnected) return false;
		if (el.closest && el.closest('[aria-hidden="true"]')) return false;
		const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
		if (r.width <= 5 || r.height <= 5) return false;
		const st = getComputedStyle(el);
		return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
	};
	const visible = (el) => {
		if (!shown(el)) return false;
		const st = getComputedStyle(el);
		return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
	};
	const press = async (el) => {
		if (!el || !el.isConnected) return false;
		if (typeof window.tulaiClick === 'function') {
			try { if (await window.tulaiClick(el)) return true; } catch (e) {}
		}
		if (!el.isConnected) return false;
		try {
			if (typeof el.focus === 'function') el.focus();
			const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
			const opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
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
	};
	const ratioOf = (el) => {
		if (!el) return '';
		const t = norm(el.innerText || el.getAttribute('aria-label') || '');
		const m = /^比例\s*(\d+:\d+|自动比例|自动|智能比例|智能)$/.exec(t);
		return m ? m[1] : '';
	};
	const findRatioBtn = (requireEnabled) => {
		const all = Array.from(document.querySelectorAll('button,[role="button"]')).filter((el) => {
			return (requireEnabled ? visible(el) : shown(el)) && ratioOf(el) !== '';
		});
		if (!all.length) return null;
		return all.find((el) => el.getAttribute('aria-haspopup') === 'menu') || all[0];
	};
	const findMenu = () => {
		return Array.from(document.querySelectorAll('[role="menu"],[role="listbox"]')).find((m) => {
			if (m.getAttribute('data-state') === 'closed' || !visible(m)) return false;
			const txt = norm(m.innerText || m.textContent);
			const matches = txt.match(/\b\d+:\d+\b/g) || [];
			return matches.length >= 2;
		}) || null;
	};
	const hasClosingMenu = () => {
		return Array.from(document.querySelectorAll('[role="menu"][data-state="closed"],[role="listbox"][data-state="closed"]')).some(shown);
	};
	const closeMenu = async (ctrl) => {
		if (findMenu()) {
			try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
			for (let i = 0; i < 6 && findMenu(); i++) await sleep(25);
			if (findMenu() && ctrl && visible(ctrl)) {
				try { await press(ctrl); } catch (e) {}
				for (let j = 0; j < 6 && findMenu(); j++) await sleep(25);
			}
		}
		for (let k = 0; k < 12 && hasClosingMenu(); k++) await sleep(25);
	};
	const deadline = Date.now() + 6500;
	let ctrl = findRatioBtn(false);
	if (!ctrl) {
		const findAI = () => {
			const exact = Array.from(document.querySelectorAll('button[aria-label="AI生图"],[role="button"][aria-label="AI生图"],button,[role="button"]')).find((el) => {
				if (!visible(el)) return false;
				const lbl = norm(el.getAttribute('aria-label'));
				const txt = norm(el.innerText);
				return lbl === 'AI生图' || txt === 'AI生图';
			});
			return exact || null;
		};
		let clicked = 0;
		while (!ctrl && Date.now() < deadline) {
			const aiBtn = findAI();
			if (aiBtn && clicked < 2) {
				await press(aiBtn);
				clicked++;
				await sleep(180);
			} else {
				const more = Array.from(document.querySelectorAll('button[aria-label="更多"][aria-haspopup="menu"]')).find(visible);
				if (more && clicked === 0) {
					await press(more);
					clicked++;
					await sleep(150);
					const menuAI = Array.from(document.querySelectorAll('[role="menuitem"],[role="menuitemcheckbox"],[role="menuitemradio"]')).find((el) => {
						return visible(el) && norm(el.innerText) === 'AI生图';
					});
					if (menuAI) await press(menuAI);
				}
			}
			ctrl = findRatioBtn(false);
			if (ctrl) break;
			await sleep(60);
		}
	}
	if (!ctrl) return { ok: false, error: '未找到千问比例入口' };
	const currentRatio = ratioOf(ctrl);
	if (!visible(ctrl)) return { ok: false, error: '千问比例入口当前已锁定' };
	while (Date.now() < deadline && hasClosingMenu()) await sleep(25);
	let menu = findMenu();
	let opened = false;
	if (!menu) {
		const openUntil = Math.min(deadline, Date.now() + 2400);
		let lastOpen = 0;
		while (!menu && Date.now() < openUntil) {
			const live = findRatioBtn(true);
			if (live) ctrl = live;
			if (lastOpen === 0 || Date.now() - lastOpen >= 900) {
				if (!await press(ctrl)) break;
				opened = true;
				lastOpen = Date.now();
			}
			await sleep(30);
			menu = findMenu();
		}
	}
	if (!menu) return { ok: false, error: '千问比例菜单未出现' };
	await sleep(30);
	const rawItems = Array.from(menu.querySelectorAll('[role="menuitemcheckbox"],[role="menuitemradio"],[role="menuitem"],[role="option"],button')).filter(visible);
	let supportsAuto = false;
	let autoValue = '';
	let autoLabel = '';
	const ratios = [];
	const seen = {};
	for (const el of rawItems) {
		const txt = norm(el.innerText || el.textContent);
		if (/^(?:自动|自动比例|原比例|智能|智能比例)$/.test(txt)) {
			supportsAuto = true;
			autoValue = txt;
			autoLabel = txt;
		} else {
			const m = /^(\d+:\d+)$/.exec(txt);
			if (m && !seen[m[1]]) {
				seen[m[1]] = true;
				ratios.push(m[1]);
			}
		}
	}
	if (opened) await closeMenu(findRatioBtn(true) || ctrl);
	if (!ratios.length) return { ok: false, error: '千问比例菜单未读取到比例项' };
	return {
		ok: true,
		platform: '千问',
		supportsAuto,
		autoValue,
		autoLabel,
		ratios,
		currentRatio,
		defaultRatio: supportsAuto ? '' : (seen[currentRatio] ? currentRatio : (seen['3:4'] ? '3:4' : ratios[0])),
		resolutions: [],
	};
})()`

const qwenIntlOutputCapsProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)qwen\.ai$/.test(host)) {
		return { ok: false, error: '当前页面不是千问国际' };
	}
	const norm = (v) => String(v || '').replace(/\s+/g, ' ').trim();
	const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
	const shown = (el) => {
		if (!el || !el.isConnected) return false;
		if (el.closest && el.closest('[aria-hidden="true"]')) return false;
		const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
		if (r.width <= 5 || r.height <= 5) return false;
		const st = getComputedStyle(el);
		return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
	};
	const visible = (el) => {
		if (!shown(el)) return false;
		const st = getComputedStyle(el);
		return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
	};
	const press = async (el) => {
		if (!el || !el.isConnected) return false;
		if (typeof window.tulaiClick === 'function') {
			try { if (await window.tulaiClick(el)) return true; } catch (e) {}
		}
		if (!el.isConnected) return false;
		try {
			if (typeof el.focus === 'function') el.focus();
			const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
			const opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
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
	};
	const ratioOf = (el) => {
		if (!el) return '';
		const t = norm(el.innerText || el.textContent || '');
		const m = /^(\d+:\d+)$/.exec(t);
		if (m) return m[1];
		if (/^(?:auto|自动)$/i.test(t)) return 'auto';
		return '';
	};
	const selectors = '.message-input-subconfig-visible-item .qwen-chat-v2-dropdown-menu-select,.message-input-column-footer .qwen-chat-v2-dropdown-menu-select';
	const findCtrl = (requireEnabled) => {
		return Array.from(document.querySelectorAll(selectors)).find((el) => {
			return (requireEnabled ? visible(el) : shown(el)) && ratioOf(el) !== '';
		}) || null;
	};
	const findMenu = () => {
		return Array.from(document.querySelectorAll('div[role="menu"].qwen-chat-v2-dropdown-menu-popup,[role="menu"],[role="listbox"]')).find((m) => {
			if (m.getAttribute('data-state') === 'closed' || !visible(m)) return false;
			const txt = norm(m.innerText || m.textContent);
			const matches = txt.match(/\b\d+:\d+\b/g) || [];
			return matches.length >= 2;
		}) || null;
	};
	const closeMenu = async (ctrl) => {
		if (!findMenu()) return;
		try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
		for (let i = 0; i < 6 && findMenu(); i++) await sleep(25);
		if (findMenu() && ctrl && visible(ctrl)) {
			try { await press(ctrl); } catch (e) {}
			await sleep(40);
		}
	};
	const deadline = Date.now() + 6500;
	let ctrl = findCtrl(false);
	if (!ctrl) {
		const isImgMode = (el) => visible(el) && /^(?:Create Image|Image Generation|生成图像|生成圖像)(?:\s|$)/i.test(norm(el.innerText || el.textContent));
		const modeBtn = Array.from(document.querySelectorAll('.chat-input-feature-btn,.mode-select-item,button,[role="button"],[role="menuitem"]')).find(isImgMode);
		if (modeBtn) {
			await press(modeBtn);
			const waitUntil = Math.min(deadline, Date.now() + 2500);
			while (!ctrl && Date.now() < waitUntil) {
				ctrl = findCtrl(false);
				if (ctrl) break;
				await sleep(40);
			}
		}
	}
	if (!ctrl) return { ok: false, error: '未找到千问国际比例入口' };
	const currentRatio = ratioOf(ctrl);
	if (!visible(ctrl)) return { ok: false, error: '千问国际比例入口当前已锁定' };
	let menu = findMenu();
	let opened = false;
	if (!menu) {
		const openUntil = Math.min(deadline, Date.now() + 2400);
		let lastOpen = 0;
		while (!menu && Date.now() < openUntil) {
			const live = findCtrl(true);
			if (live) ctrl = live;
			if (lastOpen === 0 || Date.now() - lastOpen >= 900) {
				if (!await press(ctrl)) break;
				opened = true;
				lastOpen = Date.now();
			}
			await sleep(30);
			menu = findMenu();
		}
	}
	if (!menu) return { ok: false, error: '千问国际比例菜单未出现' };
	await sleep(30);
	const rawItems = Array.from(menu.querySelectorAll('[role="menuitem"],[role="option"],.qwen-chat-v2-dropdown-menu-item')).filter(visible);
	let supportsAuto = false;
	let autoValue = '';
	let autoLabel = '';
	const ratios = [];
	const seen = {};
	for (const el of rawItems) {
		const txt = norm(el.innerText || el.textContent);
		if (/^(?:auto|自动|自动比例)$/i.test(txt)) {
			supportsAuto = true;
			autoValue = 'auto';
			autoLabel = txt === 'auto' ? 'Auto' : txt;
		} else {
			const m = /^(\d+:\d+)$/.exec(txt);
			if (m && !seen[m[1]]) {
				seen[m[1]] = true;
				ratios.push(m[1]);
			}
		}
	}
	if (opened) await closeMenu(findCtrl(true) || ctrl);
	if (!ratios.length) return { ok: false, error: '千问国际比例菜单未读取到比例项' };
	return {
		ok: true,
		platform: '千问国际',
		supportsAuto,
		autoValue,
		autoLabel,
		ratios,
		currentRatio,
		defaultRatio: supportsAuto ? '' : (seen[currentRatio] ? currentRatio : (seen['16:9'] ? '16:9' : ratios[0])),
		resolutions: [],
	};
})()`

const geminiOutputCapsProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)gemini\.google\.com$/.test(host)) {
		return { ok: false, error: '当前页面不是 Gemini' };
	}
	const norm = (v) => String(v || '').replace(/\s+/g, ' ').trim();
	const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
	const cleanText = (el) => {
		if (!el) return '';
		if (document.createTreeWalker && typeof NodeFilter !== 'undefined') {
			let out = '';
			const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
			let n;
			while ((n = w.nextNode())) {
				let skip = false;
				for (let p = n.parentElement; p && p !== el; p = p.parentElement) {
					const tag = String(p.tagName || '').toLowerCase();
					if (tag === 'mat-icon' || tag === 'svg' || p.getAttribute('aria-hidden') === 'true') { skip = true; break; }
				}
				if (!skip) out += ' ' + (n.nodeValue || '');
			}
			return norm(out);
		}
		return norm(el.innerText || el.textContent);
	};
	const iconNameOf = (el) => {
		if (!el || !el.querySelector) return '';
		const icon = el.querySelector('mat-icon');
		if (!icon) return '';
		return norm(icon.getAttribute('fonticon') || icon.getAttribute('data-mat-icon-name') || icon.textContent || '');
	};
	const shown = (el) => {
		if (!el || !el.isConnected) return false;
		if (el.closest && el.closest('[aria-hidden="true"]')) return false;
		const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
		if (r.width <= 5 || r.height <= 5) return false;
		const st = getComputedStyle(el);
		return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
	};
	const visible = (el) => {
		if (!shown(el)) return false;
		const st = getComputedStyle(el);
		return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
	};
	const press = async (el) => {
		if (!el || !el.isConnected) return false;
		if (typeof window.tulaiClick === 'function') {
			try { if (await window.tulaiClick(el)) return true; } catch (e) {}
		}
		if (!el.isConnected) return false;
		try {
			if (typeof el.focus === 'function') el.focus();
			const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
			const opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
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
	};
	const ratioOf = (el) => {
		if (!el) return '';
		const txt = cleanText(el);
		const lbl = norm((el.getAttribute && el.getAttribute('aria-label')) || '');
		const m = /\b(\d+:\d+)\b/.exec(txt) || /\b(\d+:\d+)\b/.exec(lbl);
		if (m) return m[1];
		const ic = iconNameOf(el);
		if (ic === 'aspect_ratio' || /^(?:宽高比|Aspect ratio)$/i.test(txt) || /(?:宽高比|Aspect ratio)/i.test(lbl)) return 'default';
		return '';
	};
	const composerScope = () => document.querySelector('input-area-v2,input-container') || document;
	const findCtrl = (requireEnabled) => {
		const all = Array.from(composerScope().querySelectorAll('button,[role="button"]')).filter((el) => {
			if (!(requireEnabled ? visible(el) : shown(el))) return false;
			if (el.closest && el.closest('.cdk-overlay-container,[role="menu"],[role="listbox"]')) return false;
			return ratioOf(el) !== '';
		});
		if (!all.length) return null;
		return all.find((el) => iconNameOf(el) === 'aspect_ratio' || el.getAttribute('aria-haspopup')) || all[0];
	};
	const findMenu = () => {
		return Array.from(document.querySelectorAll('.cdk-overlay-container [role="menu"],.cdk-overlay-container [role="listbox"],.cdk-overlay-container .mat-mdc-menu-panel,[role="menu"],[role="listbox"]')).find((m) => {
			if (m.getAttribute('data-state') === 'closed' || !visible(m)) return false;
			const txt = norm(m.innerText || m.textContent);
			const matches = txt.match(/\b\d+:\d+\b/g) || [];
			return matches.length >= 2;
		}) || null;
	};
	const closeMenu = async (ctrl) => {
		if (!findMenu()) return;
		try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
		for (let i = 0; i < 6 && findMenu(); i++) await sleep(25);
		if (findMenu() && ctrl && visible(ctrl)) {
			try { await press(ctrl); } catch (e) {}
			await sleep(40);
		}
	};
	const deadline = Date.now() + 7000;
	let ctrl = findCtrl(false);
	if (!ctrl) {
		const findCreateImg = (root) => {
			return Array.from((root || document).querySelectorAll('[role="menuitem"],[role="menuitemradio"],[role="menuitemcheckbox"],[role="option"],button,.mat-mdc-menu-item')).find((el) => {
				if (!visible(el)) return false;
				const txt = cleanText(el);
				const lbl = norm(el.getAttribute('aria-label') || '');
				return /^(?:制作图片|创作图片|生成图片|Create image|Create images|Generate image)(?:\s|$)/i.test(txt) ||
					/^(?:制作图片|创作图片|生成图片|Create image|Create images|Generate image)(?:\s|$)/i.test(lbl);
			}) || null;
		};
		let direct = findCreateImg(composerScope());
		if (!direct) {
			const toolsBtn = Array.from(composerScope().querySelectorAll('button,[role="button"]')).find((el) => {
				if (!visible(el)) return false;
				const txt = cleanText(el);
				const lbl = norm(el.getAttribute('aria-label') || '');
				const ic = iconNameOf(el);
				return ic === 'page_info' || ic === 'tune' || /^(?:工具|Tools)$/i.test(txt) || /^(?:工具|Tools)$/i.test(lbl);
			});
			if (toolsBtn) {
				await press(toolsBtn);
				const toolUntil = Math.min(deadline, Date.now() + 1800);
				while (!direct && Date.now() < toolUntil) {
					direct = findCreateImg(document);
					if (direct) break;
					await sleep(30);
				}
			}
		}
		if (direct) {
			await press(direct);
			const modeUntil = Math.min(deadline, Date.now() + 2500);
			while (!ctrl && Date.now() < modeUntil) {
				ctrl = findCtrl(false);
				if (ctrl) break;
				await sleep(40);
			}
		}
	}
	if (!ctrl) return { ok: false, error: '未找到 Gemini 宽高比入口' };
	const curRaw = ratioOf(ctrl);
	const currentRatio = curRaw === 'default' ? '' : curRaw;
	if (!visible(ctrl)) return { ok: false, error: 'Gemini 宽高比入口不可展开' };
	let menu = findMenu();
	let opened = false;
	if (!menu) {
		const openUntil = Math.min(deadline, Date.now() + 2400);
		let lastOpen = 0;
		while (!menu && Date.now() < openUntil) {
			const live = findCtrl(true);
			if (live) ctrl = live;
			if (lastOpen === 0 || Date.now() - lastOpen >= 900) {
				if (!await press(ctrl)) break;
				opened = true;
				lastOpen = Date.now();
			}
			await sleep(30);
			menu = findMenu();
		}
	}
	if (!menu) return { ok: false, error: 'Gemini 宽高比菜单未出现' };
	await sleep(30);
	const rawItems = Array.from(menu.querySelectorAll('[role="menuitem"],[role="menuitemradio"],[role="menuitemcheckbox"],[role="option"],button,.mat-mdc-menu-item')).filter(visible);
	let autoValue = '';
	let autoLabel = '默认比例';
	const ratios = [];
	const seen = {};
	for (const el of rawItems) {
		const txt = cleanText(el);
		if (/^(?:自动|默认|默认比例|Auto|Default)$/i.test(txt)) {
			autoValue = txt;
			autoLabel = txt;
			continue;
		}
		const m = /\b(\d+:\d+)\b/.exec(txt);
		if (m && !seen[m[1]]) {
			seen[m[1]] = true;
			ratios.push(m[1]);
		}
	}
	if (opened) await closeMenu(findCtrl(true) || ctrl);
	if (!ratios.length) return { ok: false, error: 'Gemini 宽高比菜单未读取到比例项' };
	return {
		ok: true,
		platform: 'Gemini',
		supportsAuto: true,
		autoValue,
		autoLabel,
		ratios,
		currentRatio,
		defaultRatio: '',
		resolutions: [],
	};
})()`

const jimengOutputCapsProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)jimeng\.jianying\.com$/.test(host)) {
		return { ok: false, error: '当前页面不是即梦' };
	}
	const norm = (v) => String(v || '').replace(/\s+/g, ' ').trim();
	const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
	const shown = (el) => {
		if (!el || !el.isConnected) return false;
		if (el.closest && el.closest('[aria-hidden="true"],[data-generator-preview-ignore="true"],[class*="prompt-editor-sizer"]')) return false;
		const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
		if (r.width <= 4 || r.height <= 4) return false;
		const st = getComputedStyle(el);
		return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
	};
	const visible = (el) => {
		if (!shown(el)) return false;
		const st = getComputedStyle(el);
		return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
	};
	const press = async (el) => {
		if (!el || !el.isConnected) return false;
		if (typeof window.tulaiClick === 'function') {
			try { if (await window.tulaiClick(el)) return true; } catch (e) {}
		}
		if (!el.isConnected) return false;
		try {
			if (typeof el.focus === 'function') el.focus();
			const r = el.getBoundingClientRect ? el.getBoundingClientRect() : { left: 0, top: 0, width: 0, height: 0 };
			const opts = { bubbles: true, cancelable: true, button: 0, buttons: 1, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
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
	};
	const openPopups = () => Array.from(document.querySelectorAll('.lv-select-popup,.lv-popover-inner-content,[role="listbox"]')).filter(shown);
	const closePopups = async () => {
		if (!openPopups().length) return;
		try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
		for (let i = 0; i < 8 && openPopups().length; i++) await sleep(25);
		if (openPopups().length) {
			const gen = document.querySelector('[data-testid="dreamina.generator"]');
			const neutral = (gen && gen.querySelector('[role="textbox"][aria-label="提示词"]')) || document.body;
			if (neutral) {
				try {
					neutral.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
					neutral.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, cancelable: true }));
					neutral.click();
				} catch (e) {}
				for (let j = 0; j < 8 && openPopups().length; j++) await sleep(25);
			}
		}
	};
	const findGen = () => document.querySelector('[data-testid="dreamina.generator"]');
	const findCombobox = (gen, ariaLabel) => {
		if (!gen) return null;
		return Array.from(gen.querySelectorAll('[role="combobox"]')).find((el) => shown(el) && norm(el.getAttribute('aria-label')) === ariaLabel) || null;
	};
	const findRatioButton = (gen) => {
		if (!gen) return null;
		return Array.from(gen.querySelectorAll('button,[role="button"]')).find((el) => {
			if (!shown(el)) return false;
			if (el.getAttribute('data-testid') === 'dreamina.generator.submit') return false;
			if (el.getAttribute('role') === 'combobox') return false;
			const aria = norm(el.getAttribute('aria-label'));
			if (/^选择生成数量/.test(aria) || /上传参考/.test(aria)) return false;
			const t = norm(el.innerText || el.textContent);
			if (!t || /^\d+\s*张$/.test(t)) return false;
			return /比例|\d+:\d+|1\.5K|2K|3K|4K/i.test(t);
		}) || null;
	};
	const findRatioPopover = () => {
		return Array.from(document.querySelectorAll('.lv-popover-inner-content,[role="dialog"]')).find((p) => {
			if (!shown(p)) return false;
			const txt = norm(p.innerText || p.textContent);
			return /画面比例|分辨率|1:1|3:4|2K/i.test(txt) && p.querySelector('input[type="radio"]');
		}) || null;
	};
	const findSelectOption = (wantText) => {
		const wantLower = norm(wantText).toLowerCase();
		const popups = Array.from(document.querySelectorAll('.lv-select-popup,[role="listbox"]')).filter(shown);
		for (let p = popups.length - 1; p >= 0; p--) {
			const opts = Array.from(popups[p].querySelectorAll('li[role="option"],[role="option"]')).filter(visible);
			const exact = opts.find((opt) => {
				const lblEl = opt.querySelector && opt.querySelector('[class*="option-label-"]');
				const lbl = norm(lblEl ? lblEl.textContent : '');
				const full = norm(opt.innerText || opt.textContent);
				return lbl.toLowerCase() === wantLower || full.toLowerCase() === wantLower;
			});
			if (exact) return exact;
			const partial = opts.find((opt) => {
				const lblEl = opt.querySelector && opt.querySelector('[class*="option-label-"]');
				const lbl = norm(lblEl ? lblEl.textContent : (opt.innerText || opt.textContent));
				return lbl.toLowerCase().indexOf(wantLower) >= 0;
			});
			if (partial) return partial;
		}
		return null;
	};
	const ensureBox = async (ariaLabel, wantText) => {
		const box = findCombobox(gen, ariaLabel);
		if (!box) return;
		if (norm(box.innerText || box.textContent).toLowerCase().indexOf(norm(wantText).toLowerCase()) >= 0) return;
		await closePopups();
		await press(box);
		let opt = null;
		const until = Math.min(deadline, Date.now() + 1800);
		while (Date.now() < until) {
			opt = findSelectOption(wantText);
			if (opt) break;
			await sleep(30);
		}
		if (opt) {
			await press(opt);
			const cUntil = Math.min(deadline, Date.now() + 1500);
			while (Date.now() < cUntil) {
				const again = findCombobox(gen, ariaLabel);
				if (again && norm(again.innerText || again.textContent).toLowerCase().indexOf(norm(wantText).toLowerCase()) >= 0) break;
				await sleep(30);
			}
		}
		await closePopups();
	};
	const deadline = Date.now() + 8500;
	let gen = null;
	while (Date.now() < deadline) {
		gen = findGen();
		if (gen && findRatioButton(gen)) break;
		await sleep(40);
	}
	if (!gen) return { ok: false, error: '未找到即梦生图工具栏' };
	await ensureBox('创作类型', '图片生成');
	await ensureBox('模型', 'Seedream 4.7');
	const modelBox = findCombobox(gen, '模型');
	const model = modelBox ? norm(modelBox.innerText || modelBox.textContent) : '';
	const btn = findRatioButton(gen);
	if (!btn) return { ok: false, error: '未找到即梦比例与分辨率入口' };
	const btnText = norm(btn.innerText || btn.textContent);
	let currentRatio = '';
	const curRatioMatch = /\b(\d+:\d+)\b/.exec(btnText);
	if (curRatioMatch) currentRatio = curRatioMatch[1];
	else if (/智能比例|智能/.test(btnText)) currentRatio = '智能比例';
	let currentResolution = '';
	const curResMatch = /\b(\d+(?:\.\d+)?K)\b/i.exec(btnText);
	if (curResMatch) currentResolution = curResMatch[1].toUpperCase();

	let popover = findRatioPopover();
	let opened = false;
	if (!popover) {
		await closePopups();
		const openUntil = Math.min(deadline, Date.now() + 2400);
		let lastOpen = 0;
		while (!popover && Date.now() < openUntil) {
			if (lastOpen === 0 || Date.now() - lastOpen >= 900) {
				if (!await press(btn)) break;
				opened = true;
				lastOpen = Date.now();
			}
			await sleep(30);
			popover = findRatioPopover();
		}
	}
	if (!popover) return { ok: false, error: '即梦比例与分辨率面板未出现' };
	await sleep(30);

	const inputs = Array.from(popover.querySelectorAll('input[type="radio"]'));
	let supportsAuto = false;
	let autoValue = '';
	let autoLabel = '';
	const ratios = [];
	const seenRatio = {};
	const resolutions = [];
	const seenRes = {};
	for (const inp of inputs) {
		if (inp.disabled || inp.getAttribute('aria-disabled') === 'true') continue;
		const labelEl = (inp.closest && inp.closest('label')) || inp.parentElement;
		if (labelEl && labelEl.getAttribute && labelEl.getAttribute('aria-disabled') === 'true') continue;
		const val = norm(inp.value || inp.getAttribute('value') || '');
		const lbl = norm(labelEl ? (labelEl.innerText || labelEl.textContent) : '');
		if (/^智能(?:比例)?$/i.test(lbl) || /^智能(?:比例)?$/i.test(val) || /^(?:smart|auto)$/i.test(val)) {
			supportsAuto = true;
			autoValue = '智能';
			autoLabel = '智能比例';
			if (inp.checked) currentRatio = '智能比例';
			continue;
		}
		const rMatch = /^(\d+:\d+)$/.exec(val) || /^(\d+:\d+)$/.exec(lbl);
		if (rMatch) {
			const r = rMatch[1];
			if (!seenRatio[r]) {
				seenRatio[r] = true;
				ratios.push(r);
			}
			if (inp.checked) currentRatio = r;
			continue;
		}
		const resMatch = /^(\d+(?:\.\d+)?K|标清|高清|超清)$/i.exec(lbl) || /^(\d+(?:\.\d+)?K|标清|高清|超清)$/i.exec(val);
		if (resMatch) {
			const res = resMatch[1].toUpperCase();
			if (!seenRes[res]) {
				seenRes[res] = true;
				resolutions.push(res);
			}
			if (inp.checked) currentResolution = res;
		}
	}
	if (opened) await closePopups();
	if (!ratios.length) return { ok: false, error: '未从即梦面板读到比例选项' };
	return {
		ok: true,
		platform: '即梦',
		model,
		supportsAuto,
		autoValue,
		autoLabel,
		ratios,
		defaultRatio: supportsAuto ? '' : (seenRatio[currentRatio] ? currentRatio : ratios[0]),
		resolutions,
		defaultResolution: seenRes['2K'] ? '2K' : (currentResolution || (resolutions[0] || '')),
		currentRatio,
		currentResolution,
	};
})()`

func outputCapsProbeScript(platform string) string {
	switch strings.TrimSpace(platform) {
	case "豆包":
		return doubaoOutputCapsProbeJS()
	case "千问":
		return qianwenOutputCapsProbeJS
	case "千问国际":
		return qwenIntlOutputCapsProbeJS
	case "Gemini":
		return geminiOutputCapsProbeJS
	case "即梦":
		return jimengOutputCapsProbeJS
	default:
		return ""
	}
}

func pickPlatformTarget(list []sliderTarget, platform string) (string, bool) {
	switch strings.TrimSpace(platform) {
	case "豆包":
		return pickDoubaoTarget(list)
	case "千问":
		return pickQwenTarget(list)
	case "千问国际":
		for _, item := range list {
			if item.Type != "page" || item.ID == "" {
				continue
			}
			if strings.Contains(strings.ToLower(item.URL), "qwen.ai") {
				return item.ID, true
			}
		}
		return "", false
	case "Gemini":
		return pickGeminiTarget(list)
	case "即梦":
		return pickJimengTarget(list)
	default:
		return "", false
	}
}

func normalizeRawOutputCaps(platform string, raw rawOutputCapsProbe, now time.Time) (store.ProviderOutputCaps, error) {
	if !raw.OK {
		if strings.TrimSpace(raw.Error) != "" {
			return store.ProviderOutputCaps{}, fmt.Errorf("%s", strings.TrimSpace(raw.Error))
		}
		return store.ProviderOutputCaps{}, fmt.Errorf("未读取到%s网页输出规格", platform)
	}
	if now.IsZero() {
		now = time.Now()
	}
	caps, ok := store.NormalizeProviderOutputCaps(platform, store.ProviderOutputCaps{
		Platform:          platform,
		Source:            "live",
		SyncedAt:          now.Unix(),
		Model:             raw.Model,
		SupportsAuto:      raw.SupportsAuto,
		AutoValue:         raw.AutoValue,
		AutoLabel:         raw.AutoLabel,
		Ratios:            raw.Ratios,
		DefaultRatio:      raw.DefaultRatio,
		Resolutions:       raw.Resolutions,
		DefaultResolution: raw.DefaultResolution,
		CurrentRatio:      raw.CurrentRatio,
		CurrentResolution: raw.CurrentResolution,
	})
	if !ok {
		return store.ProviderOutputCaps{}, fmt.Errorf("读取到的%s网页输出规格不完整", platform)
	}
	return caps, nil
}

// ProbeOutputCaps 附着到已打开的供应商网页，读取其原生支持的输出比例、自动项与分辨率。
// 只断开调试连接，不关闭标签页。
func ProbeOutputCaps(ctx context.Context, platform, endpoint string) (store.ProviderOutputCaps, error) {
	script := outputCapsProbeScript(platform)
	if script == "" {
		return store.ProviderOutputCaps{}, fmt.Errorf("不支持的平台: %s", platform)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return store.ProviderOutputCaps{}, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return store.ProviderOutputCaps{}, err
	}
	id, ok := pickPlatformTarget(targets, platform)
	if !ok {
		return store.ProviderOutputCaps{}, fmt.Errorf("未找到已打开的%s标签页", platform)
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return store.ProviderOutputCaps{}, fmt.Errorf("当前地区受限，无法读取%s网页输出规格", platform)
	}
	var raw rawOutputCapsProbe
	if err := conn.evalJSON(ctx, id, script, &raw, true, true); err != nil {
		return store.ProviderOutputCaps{}, err
	}
	return normalizeRawOutputCaps(platform, raw, time.Now())
}

// ProbeOutputCaps 在已有任务会话页面中读取当前供应商网页支持的输出比例与分辨率，不新开标签页。
func (s *Session) ProbeOutputCaps(platform string) (store.ProviderOutputCaps, error) {
	if s == nil {
		return store.ProviderOutputCaps{}, fmt.Errorf("缺少浏览器会话")
	}
	script := outputCapsProbeScript(platform)
	if script == "" {
		return store.ProviderOutputCaps{}, fmt.Errorf("不支持的平台: %s", platform)
	}
	s.EnsureBase()
	out, err := s.EvaluateAsync(script)
	if err != nil {
		return store.ProviderOutputCaps{}, err
	}
	var raw rawOutputCapsProbe
	if err := ParseJSValue(out, &raw); err != nil {
		return store.ProviderOutputCaps{}, err
	}
	return normalizeRawOutputCaps(platform, raw, time.Now())
}
