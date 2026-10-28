// 只转发软件脚本明确标记的 console，网站日志不进入任务日志。
function egoConsoleMessages(events) {
  const messages = new Set();
  for (const event of events) {
    if (event.method !== "Runtime.consoleAPICalled") continue;
    const message = (event.params?.args || []).map(arg => arg.value ?? arg.description ?? "").join(" ");
    if (message.startsWith("[验证码]")) messages.add(message);
  }
  return [...messages];
}

// 这些动作只调用公开 Ego Page/FileChooser/Download API。
async function egoCloseRetiredPages(task) {
  let closed = 0;
  for (const tab of await task.tabs()) {
    if (!tab.label || tab.openedBy !== "agent") continue;
    try {
      const candidate = task.page(tab.label);
      const empty = await candidate.evaluate(() => {
        if (sessionStorage.getItem("__tulaiRetiredHookPage") !== "1") return false;
        const editors = [...document.querySelectorAll('textarea,input,[contenteditable]:not([contenteditable="false" i])')].filter(editor =>
          editor.tagName !== "INPUT" || /^(text|search|email|url|tel|password|number)$/.test(editor.type)
        );
        if (!editors.length) return false;
        return editors.every(editor => {
          const nonempty = value => String(value).replace(/[\u200B-\u200D\uFEFF]/g, "").trim() !== "";
          if (editor.tagName === "TEXTAREA" || editor.tagName === "INPUT") {
            return typeof editor.value === "string" && !nonempty(editor.value);
          }
          if (editor.nodeType !== 1) return false;
          const hasText = node => {
            if (node.nodeType === 3) return nonempty(node.nodeValue || "");
            if (node.nodeType !== 1) return false;
            if (node !== editor && (node.getAttribute("contenteditable") === "false" || node.getAttribute("aria-hidden") === "true" || node.getAttribute("data-slate-placeholder") === "true")) return false;
            if (node.shadowRoot) throw new Error("无法确认编辑区内容");
            return [...node.childNodes].some(hasText);
          };
          return !hasText(editor);
        });
      });
      if (empty === true) {
        await candidate.close();
        closed++;
      }
    } catch { /* 无法确认无草稿或无法关闭的页面保留。 */ }
  }
  return closed;
}

async function egoNewPage(task, onCleanup) {
  const closed = await egoCloseRetiredPages(task);
  if (closed && onCleanup) onCleanup(closed);
  try {
    return await task.newPage();
  } catch (error) {
    let remaining = "未知";
    try { remaining = (await task.tabs()).length; } catch {}
    throw new Error(`${String(error.message || error)}；创建前安全回收 ${closed} 个旧页，空间剩余 ${remaining} 个页面；其他页面因非软件旧页、草稿或无法确认而保留`);
  }
}

function egoUploadSnapshotLines(snapshot) {
  const lines = new Set();
  const roleOf = value => /^(menuitem|menuitemcheckbox|menuitemradio|button|option|menu|listbox|text|generic|paragraph)$/i.test(value || "") ? value.toLowerCase() : "other";
  const add = (text, role, ref) => {
    if (lines.size === 4 || typeof text !== "string") return;
    const label = text.includes("上传图片") ? "上传图片" : text.includes("上传文档") ? "上传文档" : "";
    if (!label) return;
    const safeRef = /^@?\d{1,12}$/.test(String(ref ?? "")) ? "@" + String(ref).replace(/^@/, "") : "none";
    lines.add(`label=${label} role=${roleOf(role)} ref=${safeRef}`);
  };
  const seen = new Set();
  const visit = value => {
    if (lines.size === 4) return;
    if (typeof value === "string") {
      for (const line of value.split(/\r?\n/)) {
        add(line, line.match(/\b(menuitemcheckbox|menuitemradio|menuitem|button|option|menu|listbox|text|generic|paragraph)\b/i)?.[1], line.match(/(?:@|ref[=:]\s*["']?)(\d+)\b/i)?.[1]);
      }
    } else if (value && typeof value === "object" && !seen.has(value)) {
      seen.add(value);
      if (!Array.isArray(value)) {
        for (const key of ["name", "label", "text", "accessibleName"]) add(value[key], value.role, value.ref ?? value.refId);
      }
      for (const [key, child] of Object.entries(value)) {
        if (typeof child === "object" || typeof child === "string" && (Array.isArray(value) || child.includes("\n") || ["snapshot", "tree", "content"].includes(key))) visit(child);
      }
    }
  };
  visit(snapshot);
  return [...lines];
}

async function egoUploadSnapshotDiagnostic(page, step, total, onLog) {
  if (!onLog) return;
  const prefix = `上传步骤 ${step}/${total} snapshot`;
  try {
    const lines = egoUploadSnapshotLines(await page.snapshot());
    if (!lines.length) onLog(`${prefix} 上传功能项未出现`);
    else for (const line of lines) onLog(`${prefix} ${line}`);
  } catch {
    try { onLog(`${prefix} 诊断不可用`); } catch {}
  }
}

async function egoUploadDiagnostic(page, step, total, settled, phase, onLog) {
  if (!onLog) return;
  const prefix = `上传步骤 ${step}/${total} ${phase} chooserSettled=${settled}`;
  try {
    const state = await page.evaluate(() => {
      const items = [...document.querySelectorAll('[role="menuitem"]')];
      const counts = { image: 0, document: 0, other: 0 };
      const shown = [];
      for (const item of items) {
        const rawText = String(item.innerText || "");
        const text = rawText.trim();
        const kind = /^上传图片(?:\s|$)/.test(text) ? "image" : /^上传文档(?:\s|$)/.test(text) ? "document" : "other";
        counts[kind]++;
        if (shown.length === 4) continue;
        const rect = item.getBoundingClientRect(), style = getComputedStyle(item);
        const number = value => Number.isFinite(value) ? Math.round(value) : 0;
        const display = /^(none|contents|block|inline|inline-block|flex|inline-flex|grid|inline-grid|table|table-row|table-cell|list-item)$/.test(style.display) ? style.display : "other";
        const visibility = /^(visible|hidden|collapse)$/.test(style.visibility) ? style.visibility : "other";
        const hidden = item.getAttribute("aria-hidden");
        const details = { kind, rect: [rect.x, rect.y, rect.width, rect.height].map(number), display, visibility, ariaHidden: hidden === "true" || hidden === "false" ? hidden : "unset" };
        const points = Array.from(rawText);
        if (rawText.startsWith("上传") && points.length <= 40 && !/https?:\/\/|www\./i.test(rawText)) details.labelCodePoints = points.map(char => char.codePointAt(0));
        shown.push(details);
      }
      const active = document.activeElement;
      const tag = active?.tagName || "NONE", role = active?.getAttribute("role") || "unset";
      const activeTag = /^(BODY|HTML|BUTTON|DIV|SPAN|INPUT|TEXTAREA|A|IFRAME|NONE)$/.test(tag) ? tag : "OTHER";
      const activeRole = /^(button|menuitem|menu|listbox|option|dialog|textbox|combobox|document|none|unset)$/.test(role) ? role : "other";
      return { counts, items: shown, activeTag, activeRole, hasFocus: document.hasFocus() === true };
    });
    onLog(`${prefix} menuItems=${state.counts.image + state.counts.document + state.counts.other} image=${state.counts.image} document=${state.counts.document} other=${state.counts.other} active=${state.activeTag}/${state.activeRole} hasFocus=${state.hasFocus}`);
    state.items.forEach(({ labelCodePoints, ...item }, index) => {
      onLog(`${prefix} item#${index}=${JSON.stringify(item)}`);
      if (labelCodePoints) {
        for (let offset = 0; offset < labelCodePoints.length; offset += 16) {
          onLog(`${prefix} item#${index} labelCodePoints#${offset / 16}=${JSON.stringify(labelCodePoints.slice(offset, offset + 16))}`);
        }
      }
    });
  } catch {
    try { onLog(`${prefix} 菜单诊断不可用`); } catch {}
  }
}

async function egoUploadMenu(page, label, stillWaiting) {
  const read = () => page.evaluate(expected => {
    const normalize = value => String(value || "").replace(/\s+/g, " ").trim();
    const items = [...document.querySelectorAll('[role="menuitem"]')].filter(item => {
      const rect = item.getBoundingClientRect(), style = getComputedStyle(item);
      return rect.width > 0 && rect.height > 0 && style.display !== "none" && style.visibility === "visible" && !item.disabled && !item.hasAttribute("disabled") && item.getAttribute("aria-disabled") !== "true" && !item.closest('[aria-hidden="true"]');
    });
    const wanted = normalize(expected);
    const matches = items.filter(item => normalize(item.innerText) === wanted || normalize(item.innerText).startsWith(wanted + " ") || [...item.querySelectorAll("span,p,div,strong")].some(node => normalize(node.innerText) === wanted));
    if (matches.length !== 1) throw new Error("上传菜单目标不存在或不唯一");
    const target = matches[0], menu = target.closest('[role="menu"]'), active = document.activeElement;
    if (!menu || !active || !menu.contains(active) || active !== menu && active.closest('[role="menu"]') !== menu) throw new Error("上传菜单焦点已离开，未执行确认");
    const activeIndex = items.findIndex(item => item === active || item.contains(active));
    return { count: items.length, activeIndex, atTarget: target === active || target.contains(active) };
  }, label);
  let state = await read();
  const limit = state.count + 1;
  for (let moves = 0; !state.atTarget && moves < limit; moves++) {
    if (!stillWaiting()) throw new Error("等待文件选择器已结束，未执行确认");
    await page.keyboard.press("ArrowDown");
    const next = await read();
    if (!next.atTarget && next.activeIndex === state.activeIndex) throw new Error("上传菜单键盘导航无效，未执行确认");
    state = next;
  }
  if (!state.atTarget) throw new Error("上传菜单键盘导航未到达目标，未执行确认");
  if (!stillWaiting()) throw new Error("等待文件选择器已结束，未执行确认");
  await page.keyboard.press("Enter");
}

async function egoSDKAction(page, method, params, onLog) {
  switch (method) {
    case "sdkEvaluate":
      return await page.evaluate(params.expression);
    case "sdkSnapshot": {
      const snapshot = await page.snapshot();
      return typeof snapshot === "string" ? snapshot : JSON.stringify(snapshot);
    }
    case "sdkClick":
      await page.click(params.selector, { timeout: params.timeout });
      return null;
    case "sdkPress":
      await page.keyboard.press(params.key);
      return null;
    case "sdkFill":
      await page.fill(params.selector, params.value, { timeout: params.timeout });
      return null;
    case "sdkUpload": {
      const deadline = Date.now() + params.timeout;
      let settled;
      const pending = page.waitForFileChooser({ timeout: params.timeout }).then(
        value => settled = { value }, error => settled = { error },
      );
      let actionError;
      try {
        const selectors = params.selectors || [];
        for (const [index, selector] of selectors.entries()) {
          if (settled) break;
          await egoUploadDiagnostic(page, index + 1, selectors.length, !!settled, "开始", onLog);
          if (index > 0) await egoUploadSnapshotDiagnostic(page, index + 1, selectors.length, onLog);
          if (settled) break;
          let succeeded = false;
          try {
            const options = { timeout: Math.max(1, deadline - Date.now()) };
            if (params.menuLabel && selectors.length === 1) {
              await page.press(selector, "ArrowDown", options);
            } else {
              await page.click(selector, options);
            }
            succeeded = true;
          } finally {
            await egoUploadDiagnostic(page, index + 1, selectors.length, !!settled, succeeded ? "成功" : "失败", onLog);
          }
        }
        if (params.menuLabel && !settled) await egoUploadMenu(page, params.menuLabel, () => !settled && Date.now() < deadline);
      } catch (error) {
        actionError = error;
      }
      // 官方等待没有 cancel 方法，必须消费它后再返回，才不会污染下一次上传。
      await pending;
      if (actionError) throw actionError;
      if (settled.error) throw new Error("等待文件选择器超时或已取消");
      await settled.value.setFiles(params.files);
      return null;
    }
    case "sdkDownload": {
      const pending = page.waitForEvent("download", { timeout: params.timeout }).then(
        value => ({ value }), error => ({ error }),
      );
      let actionError;
      try {
        await page.click(params.selector, { timeout: params.timeout });
      } catch (error) {
        actionError = error;
      }
      const download = await pending;
      if (download.error) {
        if (actionError) throw actionError;
        throw new Error("等待下载超时或已取消");
      }
      let saved = false;
      try {
        if (actionError) throw actionError;
        await download.value.saveAs(params.path);
        saved = true;
        return null;
      } finally {
        if (!saved) {
          try { await download.value.cancel(); } catch {}
        }
        try { await download.value.delete(); } catch {}
      }
    }
    default:
      throw new Error("未知 ego lite SDK 动作");
  }
}
