const net = await import("node:net");
const readline = await import("node:readline");
const { randomUUID } = await import("node:crypto");
const socket = net.createConnection({ host: "127.0.0.1", port: bridgeConfig.port });
await new Promise((resolve, reject) => {
  socket.once("connect", resolve);
  socket.once("error", reject);
});
const send = value => {
  if (!socket.destroyed) socket.write(JSON.stringify(value) + "\n");
};
send({ token: bridgeConfig.token });
let task, page, polling = false, handedOff = false, fileChooser = null;
const logCleanup = closed => send({ event: "log", payload: `已回收 ${closed} 个无草稿的旧版页面` });
async function evaluateAsync(params) {
  // Ego 按页面串行处理 RPC；长 awaitPromise 会堵住 binding 事件与 Go 回执。
  const key = JSON.stringify("__tulaiEgoAsync_" + randomUUID());
  const evaluate = expression => page.cdp("Runtime.evaluate", { ...params, expression, awaitPromise: false, returnByValue: true }, { timeout: 60000 });
  await evaluate(`globalThis[${key}]={done:false};Promise.resolve().then(()=>(${params.expression})).then(value=>{globalThis[${key}]={done:true,value}},error=>{globalThis[${key}]={done:true,error:String(error?.message||error)}});void 0`);
  try {
    while (!socket.destroyed) {
      const response = await evaluate(`(()=>{const s=globalThis[${key}];return s?{done:s.done,error:s.error}:{done:true,error:'执行期间页面已导航'}})()`);
      const state = response.result?.value;
      if (state?.done) {
        if (state.error) return { result: { type: "undefined" }, exceptionDetails: { text: state.error } };
        return await evaluate(`globalThis[${key}].value`);
      }
      await new Promise(resolve => setTimeout(resolve, 50));
    }
    throw new Error("ego lite 控制会话已断开");
  } finally {
    if (!socket.destroyed) await evaluate(`delete globalThis[${key}]`);
  }
}
async function dispatch(method, params) {
  switch (method) {
    case "sdkEvaluate":
    case "sdkSnapshot":
    case "sdkClick":
    case "sdkPress":
    case "sdkFill":
    case "sdkUpload":
    case "sdkDownload":
      return await egoSDKAction(page, method, params, message => send({ event: "log", payload: message }));
    case "init": {
      const existingSpace = (await listTaskSpaces()).find(space => space.name === "小帮手");
      if (!existingSpace) {
        task = await taskSpace("小帮手");
      } else if (existingSpace.ownership === "agent") {
        task = await taskSpace(existingSpace.id);
      } else if (existingSpace.ownership === "agentDelegatedToUser") {
        task = await takeOverTaskSpace(existingSpace.id);
      } else {
        throw new Error("小帮手空间不由软件管理，请在 ego lite 中将其交还软件后重试");
      }
      const closed = await egoCloseRetiredPages(task);
      if (closed) logCleanup(closed);
      const tabs = await task.tabs();
      let existing;
      // 用户可能刚在另一个同域页面完成登录或验证，优先接回其当前页面。
      for (const tab of [...tabs].sort((a, b) => Number(b.active === true) - Number(a.active === true))) {
        let matches = false;
        try { matches = params.hosts.includes(new URL(tab.url).host.toLowerCase()); } catch { continue; }
        if (!matches) continue;
        const candidate = tab.label ? task.page(tab.label) : await task.adopt(tab.page);
        if (await candidate.evaluate(() => sessionStorage.getItem("__tulaiRetiredHookPage") === "1")) continue;
        existing = candidate;
        break;
      }
      if (existing) {
        page = existing;
      } else {
        const blank = tabs.find(tab => tab.label && tab.url === "about:blank");
        page = blank ? task.page(blank.label) : await egoNewPage(task, logCleanup);
        await page.goto(params.url, { waitUntil: "domcontentloaded", timeout: 60000 });
      }
      await page.cdp("Runtime.enable");
      await page.cdp("Runtime.addBinding", { name: "tulaiBridge" });
      return { label: page.label, url: await page.url() };
    }
    case "renew": {
      const previous = page;
      const currentURL = await previous.url();
      const fresh = await egoNewPage(task, logCleanup);
      try {
        await fresh.goto(currentURL, { waitUntil: "domcontentloaded", timeout: 60000 });
        await fresh.cdp("Runtime.enable");
        await fresh.cdp("Runtime.addBinding", { name: "tulaiBridge" });
        await previous.evaluate(() => sessionStorage.setItem("__tulaiRetiredHookPage", "1"));
      } catch (error) {
        await fresh.close();
        throw error;
      }
      page = fresh;
      return null;
    }
    case "cdp":
      if (params.method === "Runtime.evaluate" && params.params?.awaitPromise) {
        return await evaluateAsync(params.params);
      }
      return await page.cdp(params.method, params.params || {}, { timeout: 60000 });
    case "goto":
      return await page.goto(params.url, { waitUntil: "domcontentloaded", timeout: 60000 });
    case "wait":
      return await page.waitForSelector(params.selector, { state: "visible", timeout: params.timeout });
    case "upload":
      return await page.setInputFiles(params.selector, params.files);
    case "chooseUpload": {
      if (fileChooser) throw new Error("上传操作正在进行");
      const chooser = { node: null };
      const deadline = Date.now() + params.timeout;
      fileChooser = chooser;
      try {
        await page.cdp("Page.enable");
        await page.cdp("Page.setInterceptFileChooserDialog", { enabled: true });
        const result = await evaluateAsync({ expression: params.openJS, userGesture: true });
        if (result.exceptionDetails) throw new Error("打开上传入口失败: " + result.exceptionDetails.text);
        while (chooser.node === null) {
          if (socket.destroyed || Date.now() >= deadline) throw new Error("等待文件选择器超时或已取消");
          await new Promise(resolve => setTimeout(resolve, 50));
        }
        if (!chooser.node) throw new Error("文件选择器未提供文件输入节点");
        await page.cdp("DOM.setFileInputFiles", { files: params.files, backendNodeId: chooser.node });
        return null;
      } finally {
        fileChooser = null;
        await page.cdp("Page.setInterceptFileChooserDialog", { enabled: false }, { timeout: 3000 });
      }
    }
    case "closePage":
      return await page.close();
    case "handoff":
      handedOff = true;
      clearInterval(poll);
      return await task.handOff();
    default:
      throw new Error("未知 ego lite 控制命令: " + method);
  }
}
const poll = setInterval(async () => {
  if (!page || polling || socket.destroyed) return;
  polling = true;
  try {
    const events = await page.events();
    for (const message of egoConsoleMessages(events)) send({ event: "log", payload: message });
    for (const event of events) {
      if (event.method === "Runtime.bindingCalled" && event.params?.name === "tulaiBridge") {
        send({ event: "binding", payload: event.params.payload });
      } else if (event.method === "Page.fileChooserOpened" && fileChooser && fileChooser.node === null) {
        fileChooser.node = event.params?.backendNodeId || 0;
      }
    }
  } catch (error) {
    if (!handedOff) {
      send({ event: "error", error: String(error.message || error) });
      socket.end();
    }
  } finally {
    polling = false;
  }
}, 250);
try {
  const lines = readline.createInterface({ input: socket });
  for await (const line of lines) {
    const request = JSON.parse(line);
    dispatch(request.method, request.params).then(
      result => send({ id: request.id, result: result ?? null }),
      async error => {
        let fatal = false;
        if (task && request.method !== "handoff") {
          try { await task.waitForControl({ timeout: 1000, interval: 100 }); }
          catch { fatal = true; }
        }
        send({ id: request.id, error: String(error.message || error), fatal });
        if (fatal) socket.end();
      },
    );
  }
} finally {
  clearInterval(poll);
  socket.destroy();
}
