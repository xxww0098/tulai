package strategy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
)

const (
	// JimengMaxUploadImages 是即梦 Seedream 4.7 单次任务最多支持导入的参考图数量。
	JimengMaxUploadImages = 6
	// JimengDefaultMode 是即梦底部工具栏创作类型默认选项。
	JimengDefaultMode = "图片生成"
	// JimengDefaultModel 是即梦底部工具栏模型默认选项。
	JimengDefaultModel = "Seedream 4.7"
	// JimengDefaultRatio 是即梦未显式配置时的回退画面比例（与 3:4 · 2K 一致）。
	JimengDefaultRatio = "3:4"
	// JimengAutoRatio 表示即梦网页原生「智能比例」选项。
	JimengAutoRatio = "智能"
	// JimengDefaultResolution 是即梦底部工具栏默认分辨率选项。
	JimengDefaultResolution = "2k"
	// JimengDefaultCount 是即梦底部工具栏默认生成数量（6张）。
	JimengDefaultCount = 6
)

// Jimeng 即梦（https://jimeng.jianying.com/ai-tool/generate/?type=image）策略。
type Jimeng struct {
	base
	targetRatio      string
	targetResolution string
	targetCount      int
}

// Platform 平台名。
func (Jimeng) Platform() string { return "即梦" }

// HomeURL 即梦图片生成页入口。
func (Jimeng) HomeURL() string { return "https://jimeng.jianying.com/ai-tool/generate/?type=image" }

// SetImageCount 设置本次即梦任务的生成张数（限制在 1..6）。
func (j *Jimeng) SetImageCount(n int) {
	if j == nil {
		return
	}
	if n < 1 {
		n = 1
	}
	if n > JimengMaxUploadImages {
		n = JimengMaxUploadImages
	}
	j.targetCount = n
}

// ImageCount 返回本次即梦任务当前目标生成张数（1..6）。
func (j *Jimeng) ImageCount() int {
	return j.desiredCount()
}

func (j *Jimeng) desiredCount() int {
	if j != nil && j.targetCount >= 1 && j.targetCount <= JimengMaxUploadImages {
		return j.targetCount
	}
	return JimengDefaultCount
}

func (j *Jimeng) desiredRatio() string {
	if j != nil {
		if r := strings.TrimSpace(j.targetRatio); r != "" {
			return r
		}
	}
	return JimengDefaultRatio
}

// SetOutputResolution 设置本次即梦任务的清晰度/分辨率（如 "1.5K"、"2K"、"4K"）。
func (j *Jimeng) SetOutputResolution(resolution string) {
	if j == nil {
		return
	}
	j.targetResolution = strings.ToLower(strings.TrimSpace(resolution))
}

func (j *Jimeng) desiredResolution() string {
	if j != nil {
		if r := strings.ToLower(strings.TrimSpace(j.targetResolution)); r != "" {
			return r
		}
	}
	return JimengDefaultResolution
}

type jimengConfigArgs struct {
	Mode       string `json:"mode"`
	Model      string `json:"model"`
	Ratio      string `json:"ratio"`
	Resolution string `json:"resolution"`
	Count      int    `json:"count"`
	TimeoutMs  int    `json:"timeoutMs"`
}

// jimengEnsureConfigJS 在即梦生成页工具栏中自动选好：
// 1. 创作类型：图片生成
// 2. 模型：Seedream 4.7
// 3. 画面比例与分辨率：如 3:4 · 2K
// 4. 生成数量：1张..6张
// 已一致的项零点击跳过，操作完毕自动关闭浮层。
const jimengEnsureConfigJS = `(async (cfg) => {
	function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
	function sleep(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }
	function shown(el) {
		if (!el || !el.isConnected) return false;
		if (el.closest && el.closest('[aria-hidden="true"],[data-generator-preview-ignore="true"],[class*="prompt-editor-sizer"]')) return false;
		var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
		if (r.width <= 4 || r.height <= 4) return false;
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
		try { if (typeof el.scrollIntoView === 'function') el.scrollIntoView({ block: 'nearest', inline: 'nearest' }); } catch (e) {}
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
	function openPopups() {
		return Array.from(document.querySelectorAll('.lv-select-popup,.lv-popover-inner-content,[role="listbox"]')).filter(shown);
	}
	async function closePopups() {
		if (!openPopups().length) return;
		try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
		for (var i = 0; i < 8 && openPopups().length; i++) await sleep(25);
		if (openPopups().length) {
			var gen = document.querySelector('[data-testid="dreamina.generator"]');
			var neutral = (gen && gen.querySelector('[role="textbox"][aria-label="提示词"]')) || document.body;
			if (neutral) {
				try {
					neutral.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
					neutral.dispatchEvent(new MouseEvent('mouseup', { bubbles: true, cancelable: true }));
					neutral.click();
				} catch (e) {}
				for (var j = 0; j < 8 && openPopups().length; j++) await sleep(25);
			}
		}
	}
	function findGen() {
		return document.querySelector('[data-testid="dreamina.generator"]');
	}
	function findCombobox(gen, ariaLabel) {
		if (!gen) return null;
		var list = Array.from(gen.querySelectorAll('[role="combobox"]'));
		return list.find(function (el) { return shown(el) && norm(el.getAttribute('aria-label')) === ariaLabel; }) || null;
	}
	function findRatioButton(gen) {
		if (!gen) return null;
		var btns = Array.from(gen.querySelectorAll('button,[role="button"]'));
		return btns.find(function (el) {
			if (!shown(el)) return false;
			if (el.getAttribute('data-testid') === 'dreamina.generator.submit') return false;
			if (el.getAttribute('role') === 'combobox') return false;
			var aria = norm(el.getAttribute('aria-label'));
			if (/^选择生成数量/.test(aria) || /上传参考/.test(aria)) return false;
			var t = norm(el.innerText || el.textContent);
			if (!t || /^\d+\s*张$/.test(t)) return false;
			return /比例|\d+:\d+|1\.5K|2K|3K|4K/i.test(t);
		}) || null;
	}
	function findCountButton(gen) {
		if (!gen) return null;
		var exact = Array.from(gen.querySelectorAll('button[aria-label^="选择生成数量"],[role="button"][aria-label^="选择生成数量"]')).find(shown);
		if (exact) return exact;
		return Array.from(gen.querySelectorAll('button,[role="button"]')).find(function (el) {
			if (!shown(el) || el.getAttribute('role') === 'combobox') return false;
			var t = norm(el.innerText || el.textContent);
			return /^\d+\s*张$/.test(t);
		}) || null;
	}
	function currentCountOf(btn) {
		if (!btn) return 0;
		var aria = norm(btn.getAttribute('aria-label'));
		var m = aria.match(/(\d+)/);
		if (m) return parseInt(m[1], 10);
		var txt = norm(btn.innerText || btn.textContent);
		var m2 = txt.match(/^(\d+)\s*张$/);
		return m2 ? parseInt(m2[1], 10) : 0;
	}
	function isAutoRatio(v) {
		var s = norm(v);
		return s === '' || /^智能(?:比例)?$/i.test(s) || /^(?:smart|auto)$/i.test(s);
	}
	function ratioMatches(btn, wantRatio, wantRes) {
		if (!btn) return false;
		var t = norm(btn.innerText || btn.textContent);
		var hasRatio = isAutoRatio(wantRatio) ? /智能比例|智能/.test(t) : t.indexOf(wantRatio) >= 0;
		var hasRes = !wantRes || new RegExp('\\b' + wantRes.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\b', 'i').test(t);
		return hasRatio && hasRes;
	}
	function findSelectOption(wantText) {
		var wantLower = norm(wantText).toLowerCase();
		var popups = Array.from(document.querySelectorAll('.lv-select-popup,[role="listbox"]')).filter(shown);
		for (var p = popups.length - 1; p >= 0; p--) {
			var opts = Array.from(popups[p].querySelectorAll('li[role="option"],[role="option"]')).filter(visible);
			var exact = opts.find(function (opt) {
				var lblEl = opt.querySelector && opt.querySelector('[class*="option-label-"]');
				var lbl = norm(lblEl ? lblEl.textContent : '');
				var full = norm(opt.innerText || opt.textContent);
				return lbl.toLowerCase() === wantLower || full.toLowerCase() === wantLower;
			});
			if (exact) return exact;
			var partial = opts.find(function (opt) {
				var lblEl = opt.querySelector && opt.querySelector('[class*="option-label-"]');
				var lbl = norm(lblEl ? lblEl.textContent : (opt.innerText || opt.textContent));
				return lbl.toLowerCase().indexOf(wantLower) === 0 || lbl.toLowerCase().indexOf(wantLower) >= 0;
			});
			if (partial) return partial;
		}
		return null;
	}
	async function ensureCombobox(gen, ariaLabel, wantText, deadline) {
		var box = findCombobox(gen, ariaLabel);
		if (!box) return { ok: false, error: '未找到即梦「' + ariaLabel + '」下拉框' };
		var cur = norm(box.innerText || box.textContent);
		if (cur.toLowerCase().indexOf(norm(wantText).toLowerCase()) >= 0) {
			return { ok: true, already: true };
		}
		await closePopups();
		for (var round = 0; round < 3 && Date.now() < deadline; round++) {
			box = findCombobox(gen, ariaLabel);
			if (!box) return { ok: false, error: '未找到即梦「' + ariaLabel + '」下拉框' };
			cur = norm(box.innerText || box.textContent);
			if (cur.toLowerCase().indexOf(norm(wantText).toLowerCase()) >= 0) {
				await closePopups();
				return { ok: true };
			}
			await press(box);
			var opt = null;
			var openUntil = Math.min(deadline, Date.now() + 2000);
			while (Date.now() < openUntil) {
				opt = findSelectOption(wantText);
				if (opt) break;
				await sleep(30);
			}
			if (!opt) {
				await closePopups();
				continue;
			}
			await press(opt);
			var confirmUntil = Math.min(deadline, Date.now() + 1800);
			while (Date.now() < confirmUntil) {
				var again = findCombobox(gen, ariaLabel);
				if (again && norm(again.innerText || again.textContent).toLowerCase().indexOf(norm(wantText).toLowerCase()) >= 0) {
					await closePopups();
					return { ok: true };
				}
				await sleep(30);
			}
			await closePopups();
		}
		return { ok: false, error: '切换即梦「' + ariaLabel + '」为 ' + wantText + ' 未生效' };
	}
	function findRadioByValueOrLabel(popover, wantVal, wantLabel) {
		if (!popover) return null;
		var inputs = Array.from(popover.querySelectorAll('input[type="radio"]'));
		var wantValLow = String(wantVal).toLowerCase();
		var wantLblLow = norm(wantLabel).toLowerCase();
		for (var i = 0; i < inputs.length; i++) {
			var inp = inputs[i];
			var val = String(inp.value || inp.getAttribute('value') || '').toLowerCase();
			var labelEl = (inp.closest && inp.closest('label')) || inp.parentElement;
			var lbl = norm(labelEl ? (labelEl.innerText || labelEl.textContent) : '').toLowerCase();
			if (val === wantValLow || (wantLblLow && lbl === wantLblLow)) {
				return { input: inp, target: labelEl || inp };
			}
		}
		return null;
	}
	async function ensureRatioAndRes(gen, wantRatio, wantRes, deadline) {
		var btn = findRatioButton(gen);
		if (!btn) return { ok: false, error: '未找到即梦比例与分辨率入口' };
		if (ratioMatches(btn, wantRatio, wantRes)) return { ok: true, already: true };
		await closePopups();
		for (var round = 0; round < 3 && Date.now() < deadline; round++) {
			btn = findRatioButton(gen);
			if (!btn) return { ok: false, error: '未找到即梦比例与分辨率入口' };
			if (ratioMatches(btn, wantRatio, wantRes)) {
				await closePopups();
				return { ok: true };
			}
			await press(btn);
			var popover = null;
			var openUntil = Math.min(deadline, Date.now() + 2000);
			while (Date.now() < openUntil) {
				popover = Array.from(document.querySelectorAll('.lv-popover-inner-content,[role="dialog"]')).find(function (p) {
					if (!shown(p)) return false;
					var txt = norm(p.innerText || p.textContent);
					return /画面比例|分辨率|1:1|3:4|2K/i.test(txt) && p.querySelector('input[type="radio"]');
				}) || null;
				if (popover) break;
				await sleep(30);
			}
			if (!popover) {
				await closePopups();
				continue;
			}
			var wantAuto = isAutoRatio(wantRatio);
			var ratioPick = null;
			if (wantAuto) {
				var inputs = Array.from(popover.querySelectorAll('input[type="radio"]'));
				for (var ai = 0; ai < inputs.length; ai++) {
					var ainp = inputs[ai];
					var aval = norm(ainp.value || ainp.getAttribute('value') || '').toLowerCase();
					var alblEl = (ainp.closest && ainp.closest('label')) || ainp.parentElement;
					var albl = norm(alblEl ? (alblEl.innerText || alblEl.textContent) : '');
					if (/^智能(?:比例)?$/i.test(albl) || aval === '智能' || aval === '智能比例' || aval === 'smart' || aval === 'auto' || (aval === '' && /智能/.test(albl))) {
						ratioPick = { input: ainp, target: alblEl || ainp };
						break;
					}
				}
			} else {
				ratioPick = findRadioByValueOrLabel(popover, wantRatio, wantRatio);
			}
			if (!ratioPick) {
				await closePopups();
				return { ok: false, error: '即梦比例面板未找到 ' + (wantAuto ? '智能比例' : wantRatio) };
			}
			if (!ratioPick.input.checked) {
				await press(ratioPick.target);
				await sleep(60);
			}
			if (wantRes) {
				var resPick = findRadioByValueOrLabel(popover, wantRes, wantRes.toUpperCase());
				if (!resPick) {
					await closePopups();
					return { ok: false, error: '即梦分辨率面板未找到 ' + wantRes.toUpperCase() };
				}
				if (!resPick.input.checked) {
					await press(resPick.target);
					await sleep(60);
				}
			}
			var confirmUntil = Math.min(deadline, Date.now() + 1500);
			while (Date.now() < confirmUntil) {
				var again = findRatioButton(gen);
				if (again && ratioMatches(again, wantRatio, wantRes)) {
					await closePopups();
					return { ok: true };
				}
				await sleep(30);
			}
			await closePopups();
		}
		return { ok: false, error: '设置即梦比例 ' + wantRatio + ' · ' + String(wantRes || '').toUpperCase() + ' 未生效' };
	}
	async function ensureCount(gen, wantCount, deadline) {
		if (!wantCount || wantCount < 1) return { ok: true, already: true };
		var btn = findCountButton(gen);
		if (!btn) return { ok: false, error: '未找到即梦生成数量入口' };
		if (currentCountOf(btn) === wantCount) return { ok: true, already: true };
		await closePopups();
		for (var round = 0; round < 3 && Date.now() < deadline; round++) {
			btn = findCountButton(gen);
			if (!btn) return { ok: false, error: '未找到即梦生成数量入口' };
			if (currentCountOf(btn) === wantCount) {
				await closePopups();
				return { ok: true };
			}
			await press(btn);
			var popover = null;
			var openUntil = Math.min(deadline, Date.now() + 2000);
			while (Date.now() < openUntil) {
				popover = Array.from(document.querySelectorAll('.lv-popover-inner-content,[role="dialog"]')).find(function (p) {
					if (!shown(p)) return false;
					var txt = norm(p.innerText || p.textContent);
					return (/选择生成数量|生成数量/.test(txt) || p.querySelector('input[type="radio"][value="6"]')) && p.querySelector('input[type="radio"]');
				}) || null;
				if (popover) break;
				await sleep(30);
			}
			if (!popover) {
				await closePopups();
				continue;
			}
			var pick = findRadioByValueOrLabel(popover, String(wantCount), String(wantCount));
			if (!pick) {
				await closePopups();
				return { ok: false, error: '即梦生成数量面板未找到 ' + wantCount + '张' };
			}
			if (!pick.input.checked) {
				await press(pick.target);
				await sleep(60);
			}
			var confirmUntil = Math.min(deadline, Date.now() + 1500);
			while (Date.now() < confirmUntil) {
				var again = findCountButton(gen);
				if (again && currentCountOf(again) === wantCount) {
					await closePopups();
					return { ok: true };
				}
				await sleep(30);
			}
			await closePopups();
		}
		return { ok: false, error: '设置即梦生成数量 ' + wantCount + '张 未生效' };
	}

	var deadline = Date.now() + (cfg.timeoutMs || 10000);
	var gen = null;
	while (Date.now() < deadline) {
		gen = findGen();
		if (gen && findCombobox(gen, '创作类型') && findCombobox(gen, '模型')) break;
		await sleep(40);
	}
	if (!gen) return JSON.stringify({ ok: false, error: '未找到即梦生图工具栏' });

	var r1 = await ensureCombobox(gen, '创作类型', cfg.mode || '图片生成', deadline);
	if (!r1.ok) return JSON.stringify(r1);

	var r2 = await ensureCombobox(gen, '模型', cfg.model || 'Seedream 4.7', deadline);
	if (!r2.ok) return JSON.stringify(r2);

	var targetRatio = (cfg.ratio === undefined || cfg.ratio === null) ? '3:4' : String(cfg.ratio);
	var r3 = await ensureRatioAndRes(gen, targetRatio, cfg.resolution || '2k', deadline);
	if (!r3.ok) return JSON.stringify(r3);

	var r4 = await ensureCount(gen, cfg.count || 6, deadline);
	if (!r4.ok) return JSON.stringify(r4);

	window.__tulaiJimengExpectedCount = cfg.count || 6;
	return JSON.stringify({ ok: true });
})`

const jimengConfigActiveJS = `((cfg) => {
	function norm(v) { return String(v || '').replace(/\s+/g, ' ').trim(); }
	function shown(el) {
		if (!el || !el.isConnected) return false;
		if (el.closest && el.closest('[aria-hidden="true"],[data-generator-preview-ignore="true"],[class*="prompt-editor-sizer"]')) return false;
		var r = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 0, height: 0 };
		if (r.width <= 4 || r.height <= 4) return false;
		var st = getComputedStyle(el);
		return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
	}
	var gen = document.querySelector('[data-testid="dreamina.generator"]');
	if (!gen) return false;
	var boxes = Array.from(gen.querySelectorAll('[role="combobox"]')).filter(shown);
	var modeBox = boxes.find(function (el) { return norm(el.getAttribute('aria-label')) === '创作类型'; });
	var modelBox = boxes.find(function (el) { return norm(el.getAttribute('aria-label')) === '模型'; });
	if (!modeBox || norm(modeBox.innerText || modeBox.textContent).indexOf(cfg.mode || '图片生成') === -1) return false;
	if (!modelBox || norm(modelBox.innerText || modelBox.textContent).toLowerCase().indexOf(norm(cfg.model || 'Seedream 4.7').toLowerCase()) === -1) return false;
	var btns = Array.from(gen.querySelectorAll('button,[role="button"]')).filter(shown);
	var ratioBtn = btns.find(function (el) {
		if (el.getAttribute('data-testid') === 'dreamina.generator.submit' || el.getAttribute('role') === 'combobox') return false;
		var aria = norm(el.getAttribute('aria-label'));
		if (/^选择生成数量/.test(aria) || /上传参考/.test(aria)) return false;
		var t = norm(el.innerText || el.textContent);
		return t && !/^\d+\s*张$/.test(t) && /比例|\d+:\d+|1\.5K|2K|3K|4K/i.test(t);
	});
	if (!ratioBtn) return false;
	var rt = norm(ratioBtn.innerText || ratioBtn.textContent);
	var wantAuto = cfg.ratio === '' || /^智能(?:比例)?$/i.test(norm(cfg.ratio)) || /^(?:smart|auto)$/i.test(norm(cfg.ratio));
	if (wantAuto) {
		if (!/智能比例|智能/.test(rt)) return false;
	} else if (cfg.ratio && rt.indexOf(cfg.ratio) === -1) {
		return false;
	}
	if (cfg.resolution && !new RegExp('\\b' + cfg.resolution.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\b', 'i').test(rt)) return false;
	if (cfg.count > 0) {
		var countBtn = btns.find(function (el) {
			var aria = norm(el.getAttribute('aria-label'));
			return /^选择生成数量/.test(aria) || /^\d+\s*张$/.test(norm(el.innerText || el.textContent));
		});
		if (!countBtn) return false;
		var m = (norm(countBtn.getAttribute('aria-label')) + ' ' + norm(countBtn.innerText || countBtn.textContent)).match(/(\d+)/);
		if (!m || parseInt(m[1], 10) !== cfg.count) return false;
	}
	return true;
})`

func (j *Jimeng) configArgs(timeoutMs int) jimengConfigArgs {
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	return jimengConfigArgs{
		Mode:       JimengDefaultMode,
		Model:      JimengDefaultModel,
		Ratio:      j.desiredRatio(),
		Resolution: j.desiredResolution(),
		Count:      j.desiredCount(),
		TimeoutMs:  timeoutMs,
	}
}

func (j *Jimeng) ensureConfig(s *cdp.Session, timeoutMs int) error {
	if s == nil {
		return nil
	}
	rawArg, err := json.Marshal(j.configArgs(timeoutMs))
	if err != nil {
		return err
	}
	s.EnsureBase()
	out, err := s.EvaluateAsync(jimengEnsureConfigJS + `(` + string(rawArg) + `)`)
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
			res.Error = "参数设置未生效"
		}
		return fmt.Errorf("设置即梦参数: %s", res.Error)
	}
	return nil
}

func (j *Jimeng) configActive(s *cdp.Session) (bool, error) {
	if s == nil {
		return true, nil
	}
	rawArg, err := json.Marshal(j.configArgs(0))
	if err != nil {
		return false, err
	}
	out, err := s.Evaluate(jimengConfigActiveJS + `(` + string(rawArg) + `)`)
	if err != nil {
		return false, err
	}
	var active bool
	if err := cdp.ParseJSValue(out, &active); err != nil {
		return false, err
	}
	return active, nil
}

// EnsureImageMode 确认即梦已选中「图片生成」「Seedream 4.7」「比例 · 2K」与目标张数。
func (j *Jimeng) EnsureImageMode(s *cdp.Session) error {
	return j.ensureConfig(s, 10000)
}

// CleanComposer 清理上一次参考图后再确认即梦生图参数。
func (j *Jimeng) CleanComposer(s *cdp.Session) error {
	if err := j.base.CleanComposer(s); err != nil {
		return err
	}
	return j.ensureConfig(s, 10000)
}

// SetOutputSize 设置即梦画面比例（支持具体比例或「智能」）与分辨率（如 1.5K/2K/4K）。
func (j *Jimeng) SetOutputSize(s *cdp.Session, ratio, resolution string) (bool, string) {
	ratio = strings.TrimSpace(strings.ReplaceAll(ratio, "：", ":"))
	if ratio == "" || ratio == "智能比例" || strings.EqualFold(ratio, "auto") || strings.EqualFold(ratio, "smart") {
		ratio = JimengAutoRatio
	}
	j.targetRatio = ratio
	if res := strings.ToLower(strings.TrimSpace(resolution)); res != "" {
		j.targetResolution = res
	}
	if s == nil {
		return false, ""
	}
	if err := j.ensureConfig(s, 10000); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// SetOutputRatio 设置即梦画面比例并保持「图片生成」「Seedream 4.7」、目标分辨率与目标张数。
func (j *Jimeng) SetOutputRatio(s *cdp.Session, ratio string) (bool, string) {
	return j.SetOutputSize(s, ratio, "")
}

// UploadFiles 上传参考图到即梦输入区，单次最多支持 6 张。
func (j *Jimeng) UploadFiles(s *cdp.Session, files []string) error {
	if len(files) > JimengMaxUploadImages {
		return fmt.Errorf("即梦单次最多支持导入 %d 张图片（当前 %d 张）", JimengMaxUploadImages, len(files))
	}
	return j.base.UploadFiles(s, files)
}

// SendPrompt 发送前与写词后双次确认即梦参数（图片生成 / Seedream 4.7 / 比例·2K / 张数）均已正确选中。
func (j *Jimeng) SendPrompt(s *cdp.Session, prompt string) error {
	if err := j.ensureConfig(s, 10000); err != nil {
		return err
	}
	return j.base.sendPromptWithPostInsertGuard(s, prompt, j.EnsureImageMode, j.configActive)
}

// WaitForResult 等待即梦生成结果就绪，并在返回前尝试通过页内只读接口解析无水印原图地址。
func (j *Jimeng) WaitForResult(s *cdp.Session, timeout time.Duration) ([]Result, error) {
	if s != nil {
		_, _ = s.Evaluate(fmt.Sprintf(`window.__tulaiJimengExpectedCount=%d;`, j.desiredCount()))
	}
	results, err := waitForResultFor(s, "即梦", timeout, nil)
	if err != nil || len(results) == 0 || s == nil {
		return results, err
	}
	j.resolveCleanURLs(s, results)
	return results, nil
}

func (j *Jimeng) resolveCleanURLs(s *cdp.Session, results []Result) {
	if s == nil || len(results) == 0 {
		return
	}
	out, err := s.EvaluateAsync(`window.__tulaiJimengResolveClean ? window.__tulaiJimengResolveClean() : '{"byUrl":{},"byIndex":[]}'`)
	if err != nil {
		return
	}
	var resolved struct {
		ByURL   map[string]string `json:"byUrl"`
		ByIndex []string          `json:"byIndex"`
	}
	if err := cdp.ParseJSValue(out, &resolved); err != nil {
		return
	}
	for i := range results {
		candidate := ""
		if resolved.ByURL != nil {
			candidate = strings.TrimSpace(resolved.ByURL[results[i].URL])
		}
		if candidate == "" && i < len(resolved.ByIndex) {
			candidate = strings.TrimSpace(resolved.ByIndex[i])
		}
		if candidate != "" && candidate != results[i].URL {
			results[i].RawURL = candidate
		}
	}
}

func jimengFetchCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lower := strings.ToLower(raw)
	// 即梦签名地址把 ~tplv 模板一并纳入 x-signature 校验，剥模板必 403；优先保留完整签名地址。
	if strings.Contains(lower, "x-signature=") && strings.Contains(lower, "~tplv-") {
		return []string{raw}
	}
	out := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, candidate := range []string{OriginalImageURL(raw), PreferUnwatermarkedURL(raw), raw} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

// FetchRawResultBytes 保留即梦无水印原图地址的完整签名与模板参数，不做改写。
func (j *Jimeng) FetchRawResultBytes(s *cdp.Session, raw string) ([]byte, error) {
	return j.base.FetchResultBytes(s, raw)
}

// FetchResultBytes 拉取即梦图片字节：签名地址直接原样请求避免无谓 403，非签名地址优先尝试去模板原图。
func (j *Jimeng) FetchResultBytes(s *cdp.Session, raw string) ([]byte, error) {
	var last error
	for _, candidate := range jimengFetchCandidates(raw) {
		body, err := j.base.FetchResultBytes(s, candidate)
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
