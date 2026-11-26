import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';

const driverFiles = ['bridge.mjs', 'fingerprint-identity.mjs', 'download-watch.mjs'];

function setupBridgeSandbox(t, { configurePage } = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-bridge-rpc-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const file of driverFiles) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const traceFile = path.join(root, 'trace.json');
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, { recursive: true });
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({ type: 'module', exports: './index.js' }));
  fs.writeFileSync(path.join(playwright, 'index.js'), `
    import { EventEmitter } from 'node:events';
    import fs from 'node:fs';
    const traceFile = process.env.TEST_TRACE_FILE;
    const trace = fs.existsSync(traceFile)
      ? JSON.parse(fs.readFileSync(traceFile, 'utf8'))
      : { launches: [], newContextCalls: [], contexts: [], closedContexts: 0, cdpCalls: [] };
    const save = () => fs.writeFileSync(traceFile, JSON.stringify(trace));

    function makePage(ctx, owner) {
      const page = new EventEmitter();
      page.closed = false;
      page.url = () => 'about:blank';
      page.isClosed = () => page.closed;
      page.context = () => ctx;
      page.mainFrame = () => ({ evaluate: async fn => { owner.evaluated.push(String(fn)); save(); return 'ok'; } });
      page.evaluate = async fn => { owner.evaluated.push(String(fn)); save(); return 'ok'; };
      page.goto = async url => { owner.visited.push(url); save(); };
      page.close = async () => {
        if (page.closed) return;
        page.closed = true;
        const idx = ctx.pagesList.indexOf(page);
        if (idx >= 0) ctx.pagesList.splice(idx, 1);
        page.emit('close');
      };
      ${configurePage ? `(${configurePage})(page, owner);` : ''}
      return page;
    }

    function makeContext(kind, options = {}) {
      const ctx = new EventEmitter();
      const record = { kind, options, initScripts: [], evaluated: [], visited: [], closed: false, defaultTimeout: null };
      trace.contexts.push(record);
      ctx.pagesList = [];
      ctx.pages = () => ctx.pagesList;
      ctx.setDefaultTimeout = ms => { record.defaultTimeout = ms; save(); };
      ctx.addInitScript = async ({ content }) => { record.initScripts.push(content); save(); };
      ctx.newPage = async () => {
        const page = makePage(ctx, record);
        ctx.pagesList.push(page);
        save();
        return page;
      };
      ctx.newCDPSession = async () => {
        const session = new EventEmitter();
        session.send = async (method, params) => {
          trace.cdpCalls.push({ method, params });
          save();
          if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'f1' }, childFrames: [] } };
          if (method === 'Page.createIsolatedWorld') return { executionContextId: params.worldName.startsWith('_dw') ? 84 : 42 };
          return {};
        };
        session.detach = async () => {};
        return session;
      };
      ctx.close = async () => {
        if (record.closed) return;
        record.closed = true;
        trace.closedContexts++;
        save();
      };
      return ctx;
    }

    export const request = {};
    export const chromium = {
      launchPersistentContext: async (profile, opts) => {
        trace.launches.push({ profile, opts });
        const rootCtx = makeContext('persistent', opts);
        await rootCtx.newPage();
        rootCtx.browser = () => ({
          newContext: async isoOpts => {
            trace.newContextCalls.push(isoOpts);
            save();
            return makeContext('isolated', isoOpts);
          },
        });
        save();
        return rootCtx;
      },
    };
  `);

  const runRpc = requests => new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(root, 'bridge.mjs')], {
      env: { ...process.env, TEST_TRACE_FILE: traceFile },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    const responses = [];
    let stdoutBuf = '';
    let stderrBuf = '';
    let index = 0;
    const sendNext = () => {
      if (index < requests.length) child.stdin.write(JSON.stringify(requests[index++]) + '\n');
    };
    child.stdout.on('data', chunk => {
      stdoutBuf += chunk.toString();
      const lines = stdoutBuf.split('\n');
      stdoutBuf = lines.pop();
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        responses.push(msg);
        if (msg.id !== undefined) sendNext();
      }
    });
    child.stderr.on('data', chunk => { stderrBuf += chunk.toString(); });
    child.on('error', reject);
    child.on('exit', code => {
      if (code !== 0) return reject(new Error(`bridge exited ${code}: ${stderrBuf}`));
      resolve({ responses, stderr: stderrBuf, trace: JSON.parse(fs.readFileSync(traceFile, 'utf8')) });
    });
    sendNext();
  });

  return { root, runRpc };
}

test('download navigation follows a committed replacement and only retries a transient interruption once', async t => {
  const fixture = 'https://fixture.test/';
  function configurePage(page, owner) {
    let current = 'about:blank', documentURI = current, requested = '';
    const frame = {};
    page.url = () => current;
    page.mainFrame = () => frame;
    function response(url, status, attachment = false) {
      page.emit('response', {
        request: () => ({ isNavigationRequest: () => true, resourceType: () => 'document' }),
        frame: () => frame, url: () => url, status: () => status,
        headers: () => attachment ? { 'content-disposition': 'attachment' } : {},
        serverAddr: async () => null, fromServiceWorker: () => false,
      });
    }
    page.goto = async url => {
      owner.visited.push(url);
      requested = new URL(url).pathname;
      if (requested === '/transient' && owner.visited.filter(x => x === url).length === 2) {
        current = documentURI = url;
        return;
      }
      if (requested === '/offline') throw new Error('page.goto: net::ERR_NAME_NOT_RESOLVED');
      if (requested === '/empty') response(url, 204);
      if (requested === '/file') page.emit('download', {});
      throw new Error('page.goto: net::ERR_ABORTED');
    };
    page.waitForURL = async matches => {
      if (['/replaced', '/error-page', '/history'].includes(requested)) {
        current = new URL(requested + '/landing', 'https://fixture.test/').href;
        if (requested !== '/history') response(current, 200);
        documentURI = requested === '/error-page' ? 'chrome-error://chromewebdata/' : current;
      }
      if (!matches(new URL(current))) {
        throw Object.assign(new Error('No replacement navigation'), { name: 'TimeoutError' });
      }
    };
    const evaluate = page.evaluate;
    page.evaluate = fn => typeof fn === 'function' ? documentURI : evaluate(fn);
  }
  const { root, runRpc } = setupBridgeSandbox(t, { configurePage });
  const paths = ['replaced', 'transient', 'unchanged', 'error-page', 'history', 'offline', 'empty', 'file', 'ordinary'];
  const { responses, trace } = await runRpc([
    { id: 1, method: 'start', params: { profile: path.join(root, 'profile'), executable: 'fixture', headless: true } },
    ...paths.map((p, i) => ({ id: i + 2, target: '1', method: 'Page.navigate', params: { url: fixture + p, recoverInterrupted: p !== 'ordinary' } })),
    { id: paths.length + 2, method: 'Browser.close' },
  ]);
  const reply = name => responses.find(r => r.id === paths.indexOf(name) + 2);
  assert.deepEqual(reply('replaced').result, { replacementURL: fixture + 'replaced/landing' });
  assert.deepEqual(reply('transient').result, {});
  assert.match(reply('unchanged').error, /连续两次打开中断/);
  for (const name of ['error-page', 'history', 'empty', 'file', 'ordinary']) {
    assert.match(reply(name).error, /net::ERR_ABORTED/, name);
  }
  assert.match(reply('offline').error, /ERR_NAME_NOT_RESOLVED/);
  assert.deepEqual(trace.contexts[0].visited, [
    'replaced', 'transient', 'transient', 'unchanged', 'unchanged', 'error-page', 'history', 'offline', 'empty', 'file', 'ordinary',
  ].map(p => fixture + p));
});

test('isolated contexts inherit proxy, scopeBootstrap, bindings, and close cleanly with their page', async t => {
  const { root, runRpc } = setupBridgeSandbox(t);
  const profile = path.join(root, 'accounts', 'acc-isolated');
  const { responses, trace } = await runRpc([
    { id: 1, method: 'start', params: { profile, executable: 'fake-exe', headless: true, proxy: 'http://user:pass@127.0.0.1:8080' } },
    { id: 2, method: 'Runtime.addBinding', target: '1', params: { name: 'tulaiPipe' } },
    { id: 3, method: 'Target.createTarget', params: { isolated: true, url: 'https://example.test/first' } },
    { id: 4, method: 'Target.createTarget', params: { isolated: true, proxy: 'http://127.0.0.1:9090', url: 'https://example.test/second' } },
    { id: 5, method: 'Runtime.addBinding', target: '1', params: { name: 'latePipe' } },
    { id: 6, method: 'Runtime.addBinding', target: '2', params: { name: 'privatePipe' } },
    { id: 7, method: 'Page.close', target: '2', params: {} },
    { id: 8, method: 'Target.closeTarget', params: { targetId: '3' } },
    { id: 9, method: 'Browser.close', params: {} },
  ]);
  for (const reqId of [1, 2, 3, 4, 5, 6, 7, 8, 9]) {
    const res = responses.find(r => r.id === reqId);
    assert.ok(res && !res.error, `request ${reqId} failed: ${res?.error}`);
  }
  assert.equal(trace.newContextCalls.length, 2);
  assert.deepEqual(trace.newContextCalls[0], {
    viewport: null, ignoreHTTPSErrors: false,
    proxy: { server: 'http://127.0.0.1:8080', username: 'user', password: 'pass' },
  });
  assert.deepEqual(trace.newContextCalls[1], {
    viewport: null, ignoreHTTPSErrors: false,
    proxy: { server: 'http://127.0.0.1:9090' },
  });

  const persistentContext = trace.contexts.find(c => c.kind === 'persistent');
  const isolatedContexts = trace.contexts.filter(c => c.kind === 'isolated');
  assert.equal(isolatedContexts.length, 2);
  assert.ok(persistentContext.initScripts.some(s => s.includes('tulaiPipe')));
  assert.ok(persistentContext.initScripts.some(s => s.includes('latePipe')));
  for (const [index, ctx] of isolatedContexts.entries()) {
    assert.equal(ctx.defaultTimeout, 25000);
    assert.equal(ctx.closed, true);
    assert.equal(ctx.initScripts[0], 'delete Element.prototype.fakeShadowRoot;', 'missing fakeShadowRoot cleanup init script');
    assert.ok(ctx.initScripts[1]?.includes('__tulai|tulai'), 'missing scopeBootstrap init script');
    assert.equal(ctx.initScripts[1], persistentContext.initScripts[1]);
    assert.ok(ctx.initScripts.some(s => s.includes('tulaiPipe')));
    assert.ok(ctx.initScripts.some(s => s.includes('latePipe')));
    assert.equal(ctx.initScripts.some(s => s.includes('privatePipe')), index === 0);
  }
});

test('fingerprint launch uses persisted seed, ignores --enable-automation, and relays isolated-world CDP bindings', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-bridge-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const file of driverFiles) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const traceFile = path.join(root, 'trace.json');
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, { recursive: true });
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({ type: 'module', exports: './index.js' }));
  fs.writeFileSync(path.join(playwright, 'index.js'), `
    import { EventEmitter } from 'node:events';
    import fs from 'node:fs';
    const traceFile = process.env.TEST_TRACE_FILE;
    const trace = { launches: [], cdpCalls: [], initScripts: [], evaluated: [] };
    const save = () => fs.writeFileSync(traceFile, JSON.stringify(trace));
    export const request = {};
    export const chromium = {
      launchPersistentContext: async (profile, opts) => {
        trace.launches.push({ profile, opts });
        save();
        const ctx = new EventEmitter();
        const pagesList = [];
        const makePage = () => {
          const p = new EventEmitter();
          p.closed = false;
          p.url = () => 'about:blank';
          p.isClosed = () => p.closed;
          p.context = () => ctx;
          p.mainFrame = () => ({ evaluate: async src => { trace.evaluated.push(src); save(); return 'ok'; } });
          p.evaluate = async src => { trace.evaluated.push(src); save(); return 'ok'; };
          p.close = async () => { if (!p.closed) { p.closed = true; p.emit('close'); } };
          return p;
        };
        pagesList.push(makePage());
        ctx.pages = () => pagesList;
        ctx.setDefaultTimeout = () => {};
        ctx.addInitScript = async ({ content }) => { trace.initScripts.push(content); save(); };
        ctx.newPage = async () => { const p = makePage(); pagesList.push(p); return p; };
        ctx.newCDPSession = async () => {
          const session = new EventEmitter();
          let bindName = '';
          session.send = async (method, params) => {
            trace.cdpCalls.push({ method, params });
            save();
            if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'f1' }, childFrames: [] } };
            if (method === 'Page.createIsolatedWorld') return { executionContextId: 42 };
            if (method === 'Runtime.addBinding') {
              bindName = params.name;
              setTimeout(() => session.emit('Runtime.bindingCalled', { name: bindName, payload: 'hello-from-fp' }), 10);
            }
            return {};
          };
          session.detach = async () => {};
          return session;
        };
        ctx.close = async () => {};
        return ctx;
      },
    };
  `);

  const profile = path.join(root, 'Fingerprint', 'account-1');
  const child = spawn(process.execPath, [path.join(root, 'bridge.mjs')], {
    env: { ...process.env, TEST_TRACE_FILE: traceFile },
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  const messages = [];
  let buf = '';
  let stderr = '';
  const requests = [
    { id: 1, method: 'start', params: { profile, executable: 'fake-fc', headless: true, directNetwork: true } },
    { id: 2, target: '1', method: 'Runtime.addBinding', params: { name: 'tulaiBridge' } },
    { id: 3, target: '1', method: 'Runtime.evaluate', params: { expression: '1+1' } },
    { id: 4, method: 'Browser.close', params: {} },
  ];
  let idx = 0;
  const sendNext = () => { if (idx < requests.length) child.stdin.write(JSON.stringify(requests[idx++]) + '\n'); };
  await new Promise((resolve, reject) => {
    child.stdout.on('data', chunk => {
      buf += chunk.toString();
      const lines = buf.split('\n');
      buf = lines.pop();
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        messages.push(msg);
        if (msg.id === 3) {
          setTimeout(sendNext, 40);
        } else if (msg.id !== undefined) {
          sendNext();
        }
      }
    });
    child.stderr.on('data', chunk => { stderr += chunk.toString(); });
    child.on('error', reject);
    child.on('exit', code => code === 0 ? resolve() : reject(new Error(`exit ${code}: ${stderr}`)));
    sendNext();
  });

  const trace = JSON.parse(fs.readFileSync(traceFile, 'utf8'));
  assert.equal(trace.launches.length, 1);
  // Playwright's defaults are dropped one by one: its sRGB profile flattens
  // wide-gamut displays, and blocking service workers rewrites
  // navigator.serviceWorker.register with a function that names Playwright.
  assert.deepEqual(trace.launches[0].opts.ignoreDefaultArgs, ['--enable-automation', '--hide-scrollbars', '--force-color-profile=srgb']);
  assert.equal('serviceWorkers' in trace.launches[0].opts, false);
  const args = trace.launches[0].opts.args;
  // The kernel already forces navigator.webdriver false, and any blink-feature
  // switch puts the unsupported-flag bar on the first tab of a headed window.
  assert.ok(!args.some(a => /^--(enable|disable)-blink-features/.test(a)), `unexpected blink-feature switch in ${args}`);
  assert.ok(args.includes('--disable-spoofing=audio,clientrects'), `missing --disable-spoofing in ${args}`);
  assert.ok(args.some(a => /^--fingerprint=\d+$/.test(a)), `missing --fingerprint in ${args}`);
  assert.ok(args.some(a => /^--fingerprint-platform-version=\d+\.\d+\.\d+$/.test(a)), `missing --fingerprint-platform-version in ${args}`);
  assert.ok(args.some(a => /^--window-size=\d+,\d+$/.test(a)), `missing --window-size in ${args}`);
  assert.ok(args.some(a => /^--window-position=\d+,\d+$/.test(a)), `missing --window-position in ${args}`);
  assert.ok(args.some(a => /^--screen-info=\{.+\}$/.test(a)), `missing --screen-info in ${args}`);
  assert.ok(args.includes('--fingerprint-brand=Chrome'));
  assert.ok(args.includes('--no-proxy-server'));
  assert.equal(args.some(a => a.startsWith('--webrtc-ip-handling-policy')), false);
  assert.ok(fs.existsSync(path.join(profile, '.tulai-fingerprint.json')));
  assert.equal(trace.initScripts[0], 'delete Element.prototype.fakeShadowRoot;', 'fingerprint-chromium adds this property; pages must not see it');
  assert.ok(trace.initScripts[1]?.includes('__tulai|tulai'), 'missing scopeBootstrap init script');
  const docScript = trace.cdpCalls.find(c => c.method === 'Page.addScriptToEvaluateOnNewDocument' && c.params.worldName);
  assert.ok(docScript, 'missing isolated-world init script');
  const bindCall = trace.cdpCalls.find(c => c.method === 'Runtime.addBinding' && c.params.executionContextId === 42);
  assert.ok(bindCall, 'missing Runtime.addBinding');
  {
    const vm = await import('node:vm');
    const listeners = [];
    const sent = [];
    const sandbox = {
      setTimeout,
      window: { addEventListener: (_type, fn) => listeners.push(fn) },
    };
    sandbox.globalThis = sandbox;
    sandbox[bindCall.params.name] = payload => sent.push(payload);
    vm.runInNewContext(docScript.params.source, sandbox);
    vm.runInNewContext(docScript.params.source, sandbox);
    assert.equal(listeners.length, 1, 'fpForwarder must not register duplicate listeners');
    listeners[0]({ detail: 'once' });
    assert.deepEqual(sent, ['once']);
  }
  assert.ok(messages.some(m => m.event === 'binding' && m.target === '1' && m.data === 'hello-from-fp'), `missing binding event: ${JSON.stringify(messages)}`);
});

// Runs bridge.mjs from a sandbox against a recorded fake Playwright, one request
// at a time, and returns the RPC responses together with the recorded trace.
function runBridge(root, requests) {
  const traceFile = path.join(root, 'trace.json');
  fs.rmSync(traceFile, { force: true });
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(root, 'bridge.mjs')], {
      env: { ...process.env, TEST_TRACE_FILE: traceFile },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    const responses = [];
    let stdoutBuf = '', stderrBuf = '', index = 0;
    const sendNext = () => {
      if (index < requests.length) child.stdin.write(JSON.stringify(requests[index++]) + '\n');
    };
    child.stdout.on('data', chunk => {
      stdoutBuf += chunk.toString();
      const lines = stdoutBuf.split('\n');
      stdoutBuf = lines.pop();
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        responses.push(msg);
        if (msg.id !== undefined) sendNext();
      }
    });
    child.stderr.on('data', chunk => { stderrBuf += chunk.toString(); });
    child.on('error', reject);
    child.on('exit', code => {
      if (code !== 0) return reject(new Error(`bridge exited ${code}: ${stderrBuf}`));
      resolve({ responses, stderr: stderrBuf, trace: JSON.parse(fs.readFileSync(traceFile, 'utf8')) });
    });
    sendNext();
  });
}

const displayFakePlaywright = `
  import { EventEmitter } from 'node:events';
  import fs from 'node:fs';
  const traceFile = process.env.TEST_TRACE_FILE;
  const trace = { launches: [], cdpCalls: [], initScripts: [], pageInitScripts: [], pageEvaluates: 0 };
  const save = () => fs.writeFileSync(traceFile, JSON.stringify(trace));
  export const request = {};
  export const chromium = {
    launchPersistentContext: async (profile, opts) => {
      trace.launches.push({ profile, opts });
      const ctx = new EventEmitter();
      const page = new EventEmitter();
      page.closed = false;
      page.url = () => 'https://site.test/chat?id=7#frag';
      page.isClosed = () => page.closed;
      page.context = () => ctx;
      page.mainFrame = () => ({ evaluate: async () => { trace.pageEvaluates++; save(); return 'fallback'; } });
      page.evaluate = async () => { trace.pageEvaluates++; save(); return 'fallback'; };
      page.addInitScript = async ({ content }) => { trace.pageInitScripts.push(content); save(); };
      page.close = async () => { if (!page.closed) { page.closed = true; page.emit('close'); } };
      ctx.pages = () => [page];
      ctx.setDefaultTimeout = () => {};
      ctx.addInitScript = async ({ content }) => { trace.initScripts.push(content); save(); };
      ctx.newCDPSession = async () => {
        const session = new EventEmitter();
        session.send = async (method, params) => {
          trace.cdpCalls.push({ method, params });
          save();
          if (method === 'Runtime.enable') {
            session.emit('Runtime.executionContextCreated', {
              context: { id: 101, name: '', auxData: { isDefault: true, type: 'default', frameId: 'main-f1' } },
            });
            return {};
          }
          if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'main-f1' }, childFrames: [] } };
          if (method === 'Page.createIsolatedWorld') return { executionContextId: 42 };
          if (method === 'Runtime.evaluate' && params?.contextId === 42) return { result: { type: 'boolean', value: true } };
          if (method === 'Runtime.evaluate' && params?.contextId === 101 && params?.returnByValue === false && params?.objectGroup) {
            return { result: { type: 'function', objectId: 'runner' } };
          }
          if (method === 'Runtime.evaluate' && params?.contextId === 101 && params?.returnByValue === true) {
            return { result: { type: 'object', value: {
              width: 3024, height: 1964, availLeft: 0, availTop: 74, availRight: 0, availBottom: 100,
              dpr: 2, colorDepth: 30, p3: true, hdr: true, extended: true,
            } } };
          }
          if (method === 'Runtime.callFunctionOn') return { result: { type: 'object', value: { ok: true, type: 'boolean', value: true } } };
          return {};
        };
        session.detach = async () => {};
        return session;
      };
      ctx.close = async () => {};
      return ctx;
    },
  };
`;

test('headed sessions remember the host display for later headless runs, and document scripts carry the page URL', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-display-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const file of driverFiles) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, { recursive: true });
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({ type: 'module', exports: './index.js' }));
  fs.writeFileSync(path.join(playwright, 'index.js'), displayFakePlaywright);
  const accounts = path.join(root, 'Fingerprint');
  const headed = path.join(accounts, 'account-1');

  const first = await runBridge(root, [
    { id: 1, method: 'start', params: { profile: headed, executable: 'fake-fc', headless: false, directNetwork: true } },
    { id: 2, target: '1', method: 'Runtime.addBinding', params: { name: 'tulaiBridge' } },
    { id: 3, target: '1', method: 'Page.addScriptToEvaluateOnNewDocument', params: { source: 'window.__x = 1;' } },
    { id: 4, method: 'Browser.close', params: {} },
  ]);
  for (const reqId of [1, 2, 3, 4]) {
    const res = first.responses.find(r => r.id === reqId);
    assert.ok(res && !res.error, `request ${reqId} failed: ${res?.error}`);
  }
  const display = {
    width: 3024, height: 1964, availLeft: 0, availTop: 74, availRight: 0, availBottom: 100,
    dpr: 2, colorDepth: 30, p3: true, hdr: true, extended: true,
  };
  assert.equal(first.trace.launches[0].opts.headless, false);
  assert.equal(first.trace.launches[0].opts.args.some(a => a.startsWith('--screen-info')), false, 'a headed window shows the real display');
  assert.equal(first.trace.pageEvaluates, 0, 'reading the display must not go through Playwright page.evaluate');
  const displayRead = first.trace.cdpCalls.find(c => c.method === 'Runtime.evaluate' && c.params?.returnByValue === true && c.params?.contextId === 101);
  assert.ok(displayRead?.params?.expression?.includes('screen.colorDepth'), 'missing host display read');
  assert.ok(displayRead.params.expression.includes('(dynamic-range: high)'), 'the HDR capability is part of the display');
  assert.equal(displayRead.params.userGesture, undefined);
  assert.deepEqual(JSON.parse(fs.readFileSync(path.join(accounts, '.tulai-host-display.json'), 'utf8')), display);

  // Document scripts are registered on a page: their frames then read as that page's inline code.
  const scriptURL = '\n//# sourceURL=https://site.test/chat?id=7';
  assert.ok(first.trace.initScripts.find(s => s.includes('tulaiBridge'))?.endsWith(scriptURL), 'binding init script missing page URL');
  assert.ok(first.trace.pageInitScripts[0]?.includes('window.__x = 1;'));
  assert.ok(first.trace.pageInitScripts[0].endsWith(scriptURL), 'navigate-injection script missing page URL');

  const second = await runBridge(root, [
    { id: 1, method: 'start', params: { profile: path.join(accounts, 'account-2'), executable: 'fake-fc', headless: true, directNetwork: true } },
    { id: 2, method: 'Browser.close', params: {} },
  ]);
  for (const reqId of [1, 2]) {
    const res = second.responses.find(r => r.id === reqId);
    assert.ok(res && !res.error, `request ${reqId} failed: ${res?.error}`);
  }
  const args = second.trace.launches[0].opts.args;
  assert.equal(second.trace.launches[0].opts.headless, true);
  assert.equal(
    second.trace.cdpCalls.some(c => c.method === 'Runtime.evaluate' && c.params?.returnByValue === true && c.params?.contextId === 101),
    false,
    'a headless launch has no real display to record',
  );
  assert.equal(second.trace.cdpCalls.some(c => c.method === 'Emulation.setEmulatedMedia'), false, 'an HDR host needs no emulated media');
  // The record is in CSS pixels; the switch takes physical ones. The extended
  // secondary display is kept.
  assert.ok(args.includes('--screen-info={6048x3928 workAreaTop=148 workAreaBottom=200 devicePixelRatio=2 colorDepth=30}{1920x1080 isInternal=false}'), `missing recorded --screen-info in ${args}`);
  assert.ok(args.includes('--force-color-profile=hdr10'), `missing HDR profile in ${args}`);
  const size = /^--window-size=(\d+),(\d+)$/.exec(args.find(a => a.startsWith('--window-size=')) || '');
  assert.ok(size, 'missing --window-size');
  assert.ok(Number(size[1]) <= 3024 && Number(size[2]) <= 1964 - 74 - 100, `window exceeds the recorded work area: ${size[0]}`);

  // A wide-gamut host without HDR: the only switch that reports P3 recolors
  // captures, so the media query is emulated on every page instead.
  fs.writeFileSync(path.join(accounts, '.tulai-host-display.json'), JSON.stringify({...display, hdr: false}));
  const third = await runBridge(root, [
    { id: 1, method: 'start', params: { profile: path.join(accounts, 'account-3'), executable: 'fake-fc', headless: true, directNetwork: true } },
    { id: 2, method: 'Browser.close', params: {} },
  ]);
  for (const reqId of [1, 2]) {
    const res = third.responses.find(r => r.id === reqId);
    assert.ok(res && !res.error, `request ${reqId} failed: ${res?.error}`);
  }
  assert.equal(third.trace.launches[0].opts.args.some(a => a.startsWith('--force-color-profile')), false);
  assert.deepEqual(
    third.trace.cdpCalls.filter(c => c.method === 'Emulation.setEmulatedMedia').map(c => c.params),
    [{ features: [{ name: 'color-gamut', value: 'p3' }] }],
  );
  assert.equal(first.trace.cdpCalls.some(c => c.method === 'Emulation.setEmulatedMedia'), false, 'a headed window shows the real display');
});

test('main-frame Runtime.evaluate uses direct CDP runner without Playwright frame.evaluate and recovers stale runner handles', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-cdp-runner-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const file of driverFiles) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const traceFile = path.join(root, 'trace.json');
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, { recursive: true });
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({ type: 'module', exports: './index.js' }));
  fs.writeFileSync(path.join(playwright, 'index.js'), `
    import { EventEmitter } from 'node:events';
    import fs from 'node:fs';
    const traceFile = process.env.TEST_TRACE_FILE;
    const trace = { cdpCalls: [], frameEvaluates: 0, pageEvaluates: 0 };
    const save = () => fs.writeFileSync(traceFile, JSON.stringify(trace));
    export const request = {};
    export const chromium = {
      launchPersistentContext: async () => {
        const ctx = new EventEmitter();
        const page = new EventEmitter();
        page.closed = false;
        page.url = () => 'https://example.test/';
        page.isClosed = () => page.closed;
        page.context = () => ctx;
        const mainFrame = {
          evaluate: async () => { trace.frameEvaluates++; save(); return 'fallback'; },
        };
        page.mainFrame = () => mainFrame;
        page.evaluate = async () => { trace.pageEvaluates++; save(); return 'fallback'; };
        page.close = async () => { if (!page.closed) { page.closed = true; page.emit('close'); } };
        ctx.pages = () => [page];
        ctx.setDefaultTimeout = () => {};
        ctx.addInitScript = async () => {};
        ctx.newCDPSession = async () => {
          const session = new EventEmitter();
          let runnerSeq = 0;
          let failStaleOnce = true;
          session.send = async (method, params) => {
            trace.cdpCalls.push({ method, params });
            save();
            if (method === 'Runtime.enable') {
              session.emit('Runtime.executionContextCreated', {
                context: { id: 101, name: '', auxData: { isDefault: true, type: 'default', frameId: 'main-f1' } },
              });
              return {};
            }
            if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'main-f1' }, childFrames: [] } };
            if (method === 'Page.createIsolatedWorld') return { executionContextId: 42 };
            if (method === 'Runtime.evaluate' && params?.contextId === 42 && params?.expression?.includes('dispatchEvent')) {
              return { result: { type: 'boolean', value: true } };
            }
            if (method === 'Runtime.evaluate' && params?.contextId === 101 && params?.returnByValue === false && params?.objectGroup) {
              return { result: { type: 'function', objectId: 'runner-' + (++runnerSeq) } };
            }
            if (method === 'Runtime.callFunctionOn') {
              const expr = params.arguments?.[0]?.value;
              if (expr === 'stale-trigger' && failStaleOnce) {
                failStaleOnce = false;
                throw new Error('Could not find object with given id');
              }
              return { result: { type: 'object', value: { ok: true, type: 'string', value: 'cdp:' + expr + '@' + params.objectId } } };
            }
            return {};
          };
          session.detach = async () => {};
          return session;
        };
        ctx.close = async () => {};
        return ctx;
      },
    };
  `);

  const child = spawn(process.execPath, [path.join(root, 'bridge.mjs')], {
    env: { ...process.env, TEST_TRACE_FILE: traceFile },
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  const requests = [
    { id: 1, method: 'start', params: { profile: path.join(root, 'p1'), executable: 'fake-fc', headless: true } },
    { id: 2, target: '1', method: 'Runtime.evaluate', params: { expression: 'first' } },
    { id: 3, target: '1', method: 'Runtime.evaluate', params: { expression: 'second' } },
    { id: 4, target: '1', method: 'Runtime.evaluate', params: { expression: 'stale-trigger' } },
    { id: 5, method: 'Browser.close', params: {} },
  ];
  const responses = [];
  let idx = 0, buf = '', stderr = '';
  const sendNext = () => { if (idx < requests.length) child.stdin.write(JSON.stringify(requests[idx++]) + '\n'); };
  await new Promise((resolve, reject) => {
    child.stdout.on('data', chunk => {
      buf += chunk.toString();
      const lines = buf.split('\n');
      buf = lines.pop();
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        responses.push(msg);
        if (msg.id !== undefined) sendNext();
      }
    });
    child.stderr.on('data', chunk => { stderr += chunk.toString(); });
    child.on('error', reject);
    child.on('exit', code => code === 0 ? resolve() : reject(new Error(`exit ${code}: ${stderr}`)));
    sendNext();
  });

  const trace = JSON.parse(fs.readFileSync(traceFile, 'utf8'));
  assert.equal(trace.frameEvaluates, 0, 'main-frame Runtime.evaluate must not call Playwright frame.evaluate');
  assert.equal(trace.pageEvaluates, 0, 'startup must not call Playwright page.evaluate when CDP runner is ready');
  assert.equal(responses.find(r => r.id === 2)?.result?.result?.value, 'cdp:first@runner-1');
  assert.equal(responses.find(r => r.id === 3)?.result?.result?.value, 'cdp:second@runner-1');
  assert.equal(responses.find(r => r.id === 4)?.result?.result?.value, 'cdp:stale-trigger@runner-2');
  assert.equal(
    trace.cdpCalls.some(c => c.params?.includeCommandLineAPI),
    false,
    'must never enable CommandLineAPI (avoids window.getEventListeners honeypot)',
  );
  const armCalls = trace.cdpCalls.filter(c => c.method === 'Runtime.evaluate' && c.params?.contextId === 42 && c.params?.expression?.includes('dispatchEvent'));
  assert.equal(armCalls.length, 2, 'runner should be armed from fpWorld once per acquisition and re-armed on stale handle');
  const acquireCalls = trace.cdpCalls.filter(c => c.method === 'Runtime.evaluate' && c.params?.contextId === 101 && c.params?.returnByValue === false && c.params?.objectGroup);
  assert.equal(acquireCalls.length, 2, 'runner should be cached across calls and re-acquired once on stale handle');
});

test('fingerprint launch enforces WebRTC non-proxied UDP blocking only when launched with a proxy', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-proxy-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const file of driverFiles) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const traceFile = path.join(root, 'trace.json');
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, { recursive: true });
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({ type: 'module', exports: './index.js' }));
  fs.writeFileSync(path.join(playwright, 'index.js'), `
    import { EventEmitter } from 'node:events';
    import fs from 'node:fs';
    const traceFile = process.env.TEST_TRACE_FILE;
    const readTrace = () => fs.existsSync(traceFile) ? JSON.parse(fs.readFileSync(traceFile, 'utf8')) : { launches: [] };
    export const request = {};
    export const chromium = {
      launchPersistentContext: async (profile, opts) => {
        const trace = readTrace();
        trace.launches.push({ profile, opts });
        fs.writeFileSync(traceFile, JSON.stringify(trace));
        const ctx = new EventEmitter();
        const page = new EventEmitter();
        page.closed = false;
        page.url = () => 'about:blank';
        page.isClosed = () => page.closed;
        page.context = () => ctx;
        page.evaluate = async () => 'ok';
        page.close = async () => { if (!page.closed) { page.closed = true; page.emit('close'); } };
        ctx.pages = () => [page];
        ctx.setDefaultTimeout = () => {};
        ctx.addInitScript = async () => {};
        ctx.newCDPSession = async () => {
          const session = new EventEmitter();
          session.send = async method => method === 'Page.getFrameTree' ? { frameTree: { frame: { id: 'f1' }, childFrames: [] } } : {};
          session.detach = async () => {};
          return session;
        };
        ctx.close = async () => {};
        return ctx;
      },
    };
  `);

  const runStart = params => new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(root, 'bridge.mjs')], {
      env: { ...process.env, TEST_TRACE_FILE: traceFile },
      stdio: ['pipe', 'pipe', 'pipe'],
    });
    const requests = [
      { id: 1, method: 'start', params },
      { id: 2, method: 'Browser.close', params: {} },
    ];
    let idx = 0;
    let buf = '';
    let stderr = '';
    const sendNext = () => { if (idx < requests.length) child.stdin.write(JSON.stringify(requests[idx++]) + '\n'); };
    let failed;
    child.stdout.on('data', chunk => {
      buf += chunk.toString();
      const lines = buf.split('\n');
      buf = lines.pop();
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        if (msg.error) {
          failed = new Error(msg.error);
          child.stdin.end();
          return;
        }
        if (msg.id !== undefined) sendNext();
      }
    });
    child.stderr.on('data', chunk => { stderr += chunk.toString(); });
    child.on('error', reject);
    child.on('exit', code => {
      if (failed) return reject(failed);
      code === 0 ? resolve() : reject(new Error(`exit ${code}: ${stderr}`));
    });
    sendNext();
  });

  await runStart({
    profile: path.join(root, 'Fingerprint', 'account-proxied'),
    executable: 'fake-fc',
    headless: true,
    proxy: 'http://user:pass@127.0.0.1:8080',
  });
  await runStart({
    profile: path.join(root, 'Fingerprint', 'account-socks5'),
    executable: 'fake-fc',
    headless: true,
    proxy: 'socks5://127.0.0.1:1080',
  });
  await runStart({
    profile: path.join(root, 'Fingerprint', 'account-system'),
    executable: 'fake-fc',
    headless: false,
  });
  await runStart({
    profile: path.join(root, 'Fingerprint', 'account-blank-proxy'),
    executable: 'fake-fc',
    headless: false,
    proxy: '   ',
  });
  await assert.rejects(
    runStart({
      profile: path.join(root, 'Fingerprint', 'account-direct-conflict'),
      executable: 'fake-fc',
      headless: true,
      directNetwork: true,
      proxy: 'http://127.0.0.1:8080',
    }),
    /浏览器直连模式不能同时指定代理/,
  );

  const trace = JSON.parse(fs.readFileSync(traceFile, 'utf8'));
  assert.equal(trace.launches.length, 4);

  const proxied = trace.launches[0].opts;
  assert.deepEqual(proxied.proxy, { server: 'http://127.0.0.1:8080', username: 'user', password: 'pass' });
  assert.ok(proxied.args.includes('--webrtc-ip-handling-policy=disable_non_proxied_udp'), `missing WebRTC flag in ${proxied.args}`);
  assert.equal(proxied.args.includes('--no-proxy-server'), false);

  const socks5 = trace.launches[1].opts;
  assert.deepEqual(socks5.proxy, { server: 'socks5://127.0.0.1:1080' });
  assert.ok(socks5.args.includes('--webrtc-ip-handling-policy=disable_non_proxied_udp'), `missing WebRTC flag in ${socks5.args}`);
  assert.equal(socks5.args.includes('--no-proxy-server'), false);

  for (const systemNet of [trace.launches[2].opts, trace.launches[3].opts]) {
    assert.equal(systemNet.proxy, undefined);
    assert.equal(systemNet.args.some(a => a.startsWith('--webrtc-ip-handling-policy')), false);
    assert.equal(systemNet.args.includes('--no-proxy-server'), false);
  }
});

test('download assistant isolated world relays TEMU_CACHE_* and DOWNLOAD_PAGE via private CDP binding', async t => {
  const received = [];
  const srv = http.createServer((req, res) => {
    let body = '';
    req.on('data', chunk => { body += chunk; });
    req.on('end', () => {
      received.push({ token: req.headers['x-tulai-token'], body: JSON.parse(body) });
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ ok: true, message: '已加入下载队列' }));
    });
  });
  await new Promise(resolve => srv.listen(0, '127.0.0.1', resolve));
  t.after(() => srv.close());
  const endpoint = `http://127.0.0.1:${srv.address().port}/download`;

  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-dl-world-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const file of driverFiles) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const addonDir = path.join(root, 'addon');
  fs.mkdirSync(addonDir, { recursive: true });
  fs.writeFileSync(path.join(addonDir, 'settings.js'), `const TULAI_DOWNLOAD = ${JSON.stringify({ endpoint, token: 'secret-token' })}`);
  fs.writeFileSync(path.join(addonDir, 'temu.js'), 'function tulaiTemuSnapshot() { return { rows: [] }; }');
  fs.writeFileSync(path.join(addonDir, 'content.js'), 'void browser.runtime.sendMessage;');

  const traceFile = path.join(root, 'trace.json');
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, { recursive: true });
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({ type: 'module', exports: './index.js' }));
  fs.writeFileSync(path.join(playwright, 'index.js'), `
    import { EventEmitter } from 'node:events';
    import fs from 'node:fs';
    const traceFile = process.env.TEST_TRACE_FILE;
    const trace = { replies: [], worlds: [] };
    const save = () => fs.writeFileSync(traceFile, JSON.stringify(trace));
    export const request = {};
    export const chromium = {
      launchPersistentContext: async () => {
        const ctx = new EventEmitter();
        const page = new EventEmitter();
        page.closed = false;
        page.url = () => 'https://www.temu.com/search_result.html?search_key=bag';
        page.isClosed = () => page.closed;
        page.context = () => ctx;
        page.evaluate = async () => 'ok';
        page.close = async () => { if (!page.closed) { page.closed = true; page.emit('close'); } };
        ctx.pages = () => [page];
        ctx.setDefaultTimeout = () => {};
        ctx.addInitScript = async () => {};
        ctx.newCDPSession = async () => {
          const session = new EventEmitter();
          let dlBindName = '';
          session.send = async (method, params) => {
            if (method === 'Page.getFrameTree') return { frameTree: { frame: { id: 'f1' }, childFrames: [] } };
            if (method === 'Page.createIsolatedWorld') {
              trace.worlds.push(params.worldName);
              save();
              return { executionContextId: params.worldName.startsWith('_dw') ? 84 : 42 };
            }
            if (method === 'Runtime.addBinding' && params.executionContextId === 84) {
              dlBindName = params.name;
              const emitMsg = (id, message) => session.emit('Runtime.bindingCalled', {
                name: dlBindName,
                executionContextId: 84,
                payload: JSON.stringify({ id, message }),
              });
              setTimeout(() => {
                emitMsg('1', {
                  type: 'TEMU_CACHE_SAVE',
                  key: 'https://temu.com/search_result.html?search_key=bag',
                  url: 'https://www.temu.com/search_result.html?search_key=bag',
                  rows: [{ id: '123456789012345', image: 'https://img.example/1.jpg', link: 'https://www.temu.com/a-g-123456789012345.html', title: 'Bag' }],
                  truncated: false,
                });
              }, 10);
              setTimeout(() => {
                emitMsg('2', {
                  type: 'TEMU_CACHE_LOAD',
                  key: 'https://temu.com/search_result.html?search_key=bag',
                  url: 'https://www.temu.com/search_result.html?search_key=bag',
                });
              }, 30);
              setTimeout(() => {
                emitMsg('3', {
                  type: 'DOWNLOAD_PAGE',
                  marker: '11111111-2222-3333-4444-555555555555',
                });
              }, 50);
            }
            if (method === 'Runtime.evaluate' && params.contextId === 84 && params.expression.includes('globalThis[')) {
              trace.replies.push(params.expression);
              save();
            }
            return {};
          };
          session.detach = async () => {};
          return session;
        };
        ctx.close = async () => {};
        return ctx;
      },
    };
  `);

  const child = spawn(process.execPath, [path.join(root, 'bridge.mjs')], {
    env: { ...process.env, TEST_TRACE_FILE: traceFile },
    stdio: ['pipe', 'pipe', 'pipe'],
  });
  let buf = '';
  let stderr = '';
  await new Promise((resolve, reject) => {
    child.stdout.on('data', chunk => {
      buf += chunk.toString();
      const lines = buf.split('\n');
      buf = lines.pop();
      for (const line of lines) {
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        if (msg.id === 1) {
          setTimeout(() => child.stdin.write(JSON.stringify({ id: 2, method: 'Browser.close', params: {} }) + '\n'), 180);
        }
      }
    });
    child.stderr.on('data', chunk => { stderr += chunk.toString(); });
    child.on('error', reject);
    child.on('exit', code => code === 0 ? resolve() : reject(new Error(`exit ${code}: ${stderr}`)));
    child.stdin.write(JSON.stringify({
      id: 1,
      method: 'start',
      params: { profile: path.join(root, 'DownloadBrowser'), executable: 'fake-fc', headless: true, addons: [addonDir] },
    }) + '\n');
  });

  const trace = JSON.parse(fs.readFileSync(traceFile, 'utf8'));
  assert.equal(trace.worlds.length, 2, 'expected both fpWorld and dlWorld');
  assert.ok(trace.replies.some(r => r.includes('"1"') && r.includes('"ok":true')), `missing save reply: ${trace.replies}`);
  assert.ok(trace.replies.some(r => r.includes('"2"') && r.includes('123456789012345')), `missing load reply: ${trace.replies}`);
  assert.ok(trace.replies.some(r => r.includes('"3"') && r.includes('已加入下载队列')), `missing download reply: ${trace.replies}`);
  assert.deepEqual(received, [{
    token: 'secret-token',
    body: { url: 'https://www.temu.com/search_result.html?search_key=bag', marker: '11111111-2222-3333-4444-555555555555' },
  }]);
});

test('download-fingerprint streams through downloadKernel with progress events and cleans up on error', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-dl-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const file of driverFiles) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, { recursive: true });
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({ type: 'module', exports: './index.js' }));
  fs.writeFileSync(path.join(playwright, 'index.js'), 'export const request = {};');

  const payload = Buffer.alloc(128 * 1024, 0x5a);
  const srv = http.createServer((req, res) => {
    if (req.url === '/fail') {
      res.writeHead(404);
      res.end('missing');
      return;
    }
    const range = req.headers.range;
    if (range) {
      const m = /^bytes=(\d+)-(\d+)$/.exec(range);
      if (m) {
        const start = Number(m[1]);
        const end = Math.min(Number(m[2]), payload.length - 1);
        res.writeHead(206, {
          'content-range': `bytes ${start}-${end}/${payload.length}`,
          'content-length': String(end - start + 1),
        });
        res.end(payload.subarray(start, end + 1));
        return;
      }
    }
    res.writeHead(200, { 'content-length': String(payload.length) });
    res.end(payload);
  });
  await new Promise(resolve => srv.listen(0, '127.0.0.1', resolve));
  t.after(() => srv.close());
  const base = `http://127.0.0.1:${srv.address().port}`;

  const dest = path.join(root, 'archive.bin');
  const { stdout } = await promisify(execFile)(process.execPath, [path.join(root, 'bridge.mjs'), 'download-fingerprint'], {
    env: { ...process.env, TULAI_FINGERPRINT_URL: `${base}/ok.bin`, TULAI_FINGERPRINT_DEST: dest },
  });
  const lines = stdout.trim().split('\n').map(l => JSON.parse(l));
  assert.ok(lines.some(l => l.event === 'progress' && l.stage === 'browser' && l.done === payload.length && l.total === payload.length));
  assert.ok(lines.some(l => l.downloaded === dest));
  assert.deepEqual(fs.readFileSync(dest), payload);

  const failDest = path.join(root, 'fail.bin');
  await assert.rejects(
    promisify(execFile)(process.execPath, [path.join(root, 'bridge.mjs'), 'download-fingerprint'], {
      env: { ...process.env, TULAI_FINGERPRINT_URL: `${base}/fail`, TULAI_FINGERPRINT_DEST: failDest },
    }),
    err => /Fingerprint 内核下载失败/.test(String(err.stderr)),
  );
  assert.equal(fs.existsSync(failDest), false, 'failed download must not leave a partial file behind');
});

test('Input.dispatchMouseEvent avoids redundant move before press/release at identical coordinates', async t => {
  function configurePage(page, owner) {
    owner.mouseLog = [];
    page.mouse = {
      move: async (x, y) => { owner.mouseLog.push(['move', x, y]); },
      down: async opts => { owner.mouseLog.push(['down', opts.button]); },
      up: async opts => { owner.mouseLog.push(['up', opts.button]); },
      wheel: async (dx, dy) => { owner.mouseLog.push(['wheel', dx, dy]); },
    };
  }
  const { root, runRpc } = setupBridgeSandbox(t, { configurePage });
  const { responses, trace } = await runRpc([
    { id: 1, method: 'start', params: { profile: path.join(root, 'p-mouse'), executable: 'fake-fc', headless: true } },
    { id: 2, target: '1', method: 'Input.dispatchMouseEvent', params: { type: 'mouseMoved', x: 100, y: 120 } },
    { id: 3, target: '1', method: 'Input.dispatchMouseEvent', params: { type: 'mouseMoved', x: 150, y: 160 } },
    { id: 4, target: '1', method: 'Input.dispatchMouseEvent', params: { type: 'mousePressed', x: 150, y: 160, button: 'left', clickCount: 1 } },
    { id: 5, target: '1', method: 'Input.dispatchMouseEvent', params: { type: 'mouseReleased', x: 150, y: 160, button: 'left', clickCount: 1 } },
    { id: 6, method: 'Browser.close', params: {} },
  ]);
  for (const id of [1, 2, 3, 4, 5, 6]) {
    const res = responses.find(r => r.id === id);
    assert.ok(res && !res.error, `request ${id} failed: ${res?.error}`);
  }
  assert.deepEqual(trace.contexts[0].mouseLog, [
    ['move', 100, 120],
    ['move', 150, 160],
    ['down', 'left'],
    ['up', 'left'],
  ]);
});

test('closing the last visible user page closes context and exits bridge without waiting for context.close', async t => {
  function configurePage(page) {
    const origGoto = page.goto;
    page.goto = async url => {
      await origGoto(url);
      if (url === 'https://close-me.test/') {
        // Simulate macOS user clicking the red window close button: page closes, Chromium stays alive.
        setTimeout(() => { void page.close(); }, 20);
      }
    };
  }
  const { root, runRpc } = setupBridgeSandbox(t, { configurePage });
  const { responses, trace } = await runRpc([
    { id: 1, method: 'start', params: { profile: path.join(root, 'p-visible-close'), executable: 'fake-fc', headless: false } },
    { id: 2, target: '1', method: 'Page.navigate', params: { url: 'https://close-me.test/', wait: 'commit' } },
  ]);
  assert.ok(responses.some(r => r.id === 1 && !r.error));
  assert.ok(responses.some(r => r.id === 2 && !r.error));
  assert.ok(responses.some(r => r.event === 'closed' && r.target === '1'));
  assert.equal(trace.closedContexts, 1, 'visible browser should flush and close context once all user pages close');
});

test('programmatic Page.close on a visible browser does not prematurely exit bridge', async t => {
  const { root, runRpc } = setupBridgeSandbox(t);
  const { responses, trace } = await runRpc([
    { id: 1, method: 'start', params: { profile: path.join(root, 'p-prog-close'), executable: 'fake-fc', headless: false } },
    { id: 2, target: '1', method: 'Page.close', params: {} },
    { id: 3, method: 'Target.createTarget', params: { url: 'about:blank' } },
    { id: 4, method: 'Browser.close', params: {} },
  ]);
  for (const id of [1, 2, 3, 4]) {
    const res = responses.find(r => r.id === id);
    assert.ok(res && !res.error, `request ${id} failed: ${res?.error}`);
  }
  assert.equal(trace.closedContexts, 1);
});

test('Input.mouseClick batches approach moves and press/release into one RPC', async t => {
  function configurePage(page, owner) {
    owner.mouseLog = [];
    page.mouse = {
      move: async (x, y) => { owner.mouseLog.push(['move', x, y]); },
      down: async opts => { owner.mouseLog.push(['down', opts.button]); },
      up: async opts => { owner.mouseLog.push(['up', opts.button]); },
    };
  }
  const { root, runRpc } = setupBridgeSandbox(t, { configurePage });
  const { responses, trace } = await runRpc([
    { id: 1, method: 'start', params: { profile: path.join(root, 'p-mclick'), executable: 'fake-fc', headless: true } },
    { id: 2, target: '1', method: 'Input.mouseClick', params: { points: [[10, 20], [50, 60]], x: 50, y: 60, stepMs: 2, prePressMs: 2, holdMs: 2 } },
    { id: 3, method: 'Browser.close', params: {} },
  ]);
  for (const id of [1, 2, 3]) {
    const res = responses.find(r => r.id === id);
    assert.ok(res && !res.error, `request ${id} failed: ${res?.error}`);
  }
  assert.deepEqual(trace.contexts[0].mouseLog, [
    ['move', 10, 20],
    ['move', 50, 60],
    ['down', 'left'],
    ['up', 'left'],
  ]);
});

test('chooseUpload completes as soon as filechooser fires and guards single-file choosers', async t => {
  function configurePage(page, owner) {
    owner.setFilesCalls = [];
    let callNum = 0;
    const origCdp = page.context().newCDPSession.bind(page.context());
    page.context().newCDPSession = async target => {
      const cdp = await origCdp(target);
      const origSend = cdp.send.bind(cdp);
      cdp.send = async (method, params) => {
        if (method === 'Runtime.callFunctionOn' && String(params?.arguments?.[0]?.value || '').includes('slowOpen')) {
          await new Promise(r => setTimeout(r, 800));
          return { result: { value: { ok: true, type: 'object', value: { opened: true } } } };
        }
        return origSend(method, params);
      };
      return cdp;
    };
    page.waitForEvent = async ev => {
      if (ev !== 'filechooser') throw new Error(`unexpected event ${ev}`);
      const n = ++callNum;
      await new Promise(r => setTimeout(r, 20));
      return {
        isMultiple: () => n === 1,
        setFiles: async files => { owner.setFilesCalls.push(files); },
      };
    };
  }
  const { root, runRpc } = setupBridgeSandbox(t, { configurePage });
  const t0 = Date.now();
  const { responses, trace } = await runRpc([
    { id: 1, method: 'start', params: { profile: path.join(root, 'p-choose'), executable: 'fake-fc', headless: true } },
    { id: 2, target: '1', method: 'chooseUpload', params: { openJS: 'slowOpen()', files: ['/tmp/a.png', '/tmp/b.png'] } },
    { id: 3, target: '1', method: 'chooseUpload', params: { openJS: 'singleOpen()', files: ['/tmp/a.png', '/tmp/b.png'] } },
    { id: 4, method: 'Browser.close', params: {} },
  ]);
  const elapsed = Date.now() - t0;
  assert.ok(responses.find(r => r.id === 2 && !r.error), `multi chooseUpload failed: ${JSON.stringify(responses)}`);
  assert.ok(elapsed < 700, `chooseUpload waited for slow openJS instead of racing filechooser (${elapsed}ms)`);
  const singleRes = responses.find(r => r.id === 3);
  assert.ok(singleRes && /不支持多选/.test(singleRes.error || ''), `single-file chooser must reject multi-file batch cleanly: ${JSON.stringify(singleRes)}`);
  assert.deepEqual(trace.contexts[0].setFilesCalls, [['/tmp/a.png', '/tmp/b.png'], []]);
});
