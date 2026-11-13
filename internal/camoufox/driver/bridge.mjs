import fs from 'node:fs';
import { createHash, randomUUID } from 'node:crypto';
import path from 'node:path';
import readline from 'node:readline';
import { request as playwrightRequest } from 'playwright-core';
import { createIdentity, prepareIdentity, prepareRendering, preserveVoices } from './identity.mjs';

// Camoufox resolves its cache at import time; restore the real environment before launching Firefox.
const cacheKey = { darwin: 'HOME', win32: 'LOCALAPPDATA', linux: 'XDG_CACHE_HOME' }[process.platform];
const originalCache = process.env[cacheKey];
let launcher, utils;
try {
  if (cacheKey && process.env.TULAI_CAMOUFOX_CACHE) process.env[cacheKey] = process.env.TULAI_CAMOUFOX_CACHE;
  launcher = await import('camoufox');
  utils = await import('camoufox/dist/utils.js');
} finally {
  if (cacheKey && originalCache === undefined) delete process.env[cacheKey];
  else if (cacheKey) process.env[cacheKey] = originalCache;
}
const { Camoufox, launchOptions, installedVerStr } = launcher;
const { utilsDeps } = utils;

// stdout is a private pipe to tulai. Launcher diagnostics must not enter the RPC stream.
const send = process.stdout.write.bind(process.stdout);
process.stdout.write = process.stderr.write.bind(process.stderr);
const emit = value => send(JSON.stringify(value) + '\n');
const hostOS = { darwin: 'macos', win32: 'windows', linux: 'linux' }[process.platform];
const maxBytes = 64 << 20;
// Keep per-document application state out of the website's global object.
const scopeEvent = randomUUID(), pipeEvent = randomUUID();
const scopePrefix = `_${randomUUID().replaceAll('-', '')}`;
const scopeBootstrap = `(()=>{
  const ${scopePrefix}check=new CustomEvent(${JSON.stringify(scopeEvent)});
  window.dispatchEvent(${scopePrefix}check);if(${scopePrefix}check.done)return;
  const ${scopePrefix}window=window, ${scopePrefix}state=Object.create(null);
  const ${scopePrefix}private=key=>typeof key==='string'&&/^(?:__tulai|tulai)/i.test(key);
  const ${scopePrefix}facade=new Proxy(Object.create(null),{
    get(_target,key){
      if(${scopePrefix}private(key))return ${scopePrefix}state[key];
      const value=Reflect.get(${scopePrefix}window,key,${scopePrefix}window);
      if(value===${scopePrefix}window)return ${scopePrefix}facade;
      return typeof value==='function'&&['addEventListener','removeEventListener','dispatchEvent','getSelection','scrollBy','scrollTo'].includes(key)?value.bind(${scopePrefix}window):value;
    },
    set(_target,key,value){if(${scopePrefix}private(key)){${scopePrefix}state[key]=value;return true}return Reflect.set(${scopePrefix}window,key,value,${scopePrefix}window)},
    has(_target,key){return Reflect.has(${scopePrefix}private(key)?${scopePrefix}state:${scopePrefix}window,key)},
    deleteProperty(_target,key){return Reflect.deleteProperty(${scopePrefix}private(key)?${scopePrefix}state:${scopePrefix}window,key)},
    ownKeys(){return [...new Set([...Reflect.ownKeys(${scopePrefix}window),...Reflect.ownKeys(${scopePrefix}state)])]},
    getOwnPropertyDescriptor(_target,key){
      const descriptor=Reflect.getOwnPropertyDescriptor(${scopePrefix}private(key)?${scopePrefix}state:${scopePrefix}window,key);
      return descriptor?{...descriptor,configurable:true}:undefined;
    },
    getPrototypeOf(){return Reflect.getPrototypeOf(${scopePrefix}window)}
  });
  const ${scopePrefix}scope=new Proxy(Object.create(null),{
    has(_target,key){return key==='window'||key==='globalThis'||${scopePrefix}private(key)},
    get(_target,key){if(key===Symbol.unscopables)return undefined;return key==='window'||key==='globalThis'?${scopePrefix}facade:${scopePrefix}state[key]},
    set(_target,key,value){${scopePrefix}state[key]=value;return true}
  });
  const ${scopePrefix}eval=eval;
  // The suspended generator retains top-level var bindings between evaluations.
  function* ${scopePrefix}run(){
    const eval=${scopePrefix}eval;
    while(true){
      const ${scopePrefix}source=yield;
      try{let ${scopePrefix}value;with(${scopePrefix}scope){${scopePrefix}value=eval(${scopePrefix}source)}yield {value:${scopePrefix}value}}
      catch(${scopePrefix}error){yield {failed:true,error:${scopePrefix}error}}
    }
  }
  const ${scopePrefix}runner=${scopePrefix}run();${scopePrefix}runner.next();
  ${scopePrefix}window.addEventListener(${JSON.stringify(scopeEvent)},event=>{
    if(event.detail===null&&!event.run){event.done=true;return}
    try{
      if(event.run)event.value=event.run(${scopePrefix}scope);
      else{const result=${scopePrefix}runner.next(event.detail).value;${scopePrefix}runner.next();if(result.failed)throw result.error;event.value=result.value}
    }catch(error){event.failed=true;event.error=error}event.done=true;
  });
})()`;
function scoped(source) {
  return `(()=>{const event=new CustomEvent(${JSON.stringify(scopeEvent)},{detail:${JSON.stringify(source)}});
    window.dispatchEvent(event);if(!event.done){${scopeBootstrap};window.dispatchEvent(event)}
    if(!event.done)throw Error('页面脚本环境不可用');if(event.failed)throw event.error;return event.value})()`;
}
function scopedInit(source) {
  // New-document scripts must run without dynamic eval under strict CSP.
  return `(()=>{const event=new CustomEvent(${JSON.stringify(scopeEvent)});
    event.run=function(${scopePrefix}arg){with(${scopePrefix}arg){${source}\n}};
    window.dispatchEvent(event);if(!event.done){${scopeBootstrap};window.dispatchEvent(event)}
    if(!event.done)throw Error('页面脚本环境不可用');if(event.failed)throw event.error})()`;
}

if (process.platform === 'darwin' || process.platform === 'win32') {
  // shortcut: matching headed/headless GPUs are verified on macOS and Windows; the
  // probe must always run headless or a fresh identity flashes a visible window
  // before the account browser opens. Validate before extending to other platforms.
  const hostGpu = utilsDeps.hostGpu;
  utilsDeps.hostGpu = (executable, _headless, env, prefs) => hostGpu(executable, true, env, prefs);
}

// The GPU probe launches a whole second Firefox; the launcher only caches it in
// memory, so every bridge start pays it again. Hardware rarely changes - keep
// one reading per executable (keyed by mtime+size, upgrades invalidate) in the
// tulai cache dir. Any cache miss or write failure falls back to the live probe.
function gpuCacheFile(executable) {
  const dir = process.env.TULAI_CAMOUFOX_CACHE;
  if (!dir) return '';
  let stat;
  try { stat = fs.statSync(executable); } catch { return ''; }
  const key = createHash('sha256').update(`${process.platform}|${executable}|${stat.mtimeMs}|${stat.size}`).digest('hex').slice(0, 24);
  return path.join(dir, `host-gpu-${key}.json`);
}
async function cachedHostGpu(executable) {
  const file = gpuCacheFile(executable);
  if (file) {
    try {
      const cached = JSON.parse(fs.readFileSync(file, 'utf8'));
      if (cached === null || Array.isArray(cached)) return cached;
    } catch { /* 没有缓存或已损坏就实测 */ }
  }
  const gpu = await utilsDeps.hostGpu(executable, true, process.env, {});
  if (file) {
    try {
      fs.mkdirSync(path.dirname(file), { recursive: true });
      const tmp = `${file}.${process.pid}.tmp`;
      fs.writeFileSync(tmp, JSON.stringify(gpu));
      fs.renameSync(tmp, file);
    } catch { /* 缓存写不上不影响启动 */ }
  }
  return gpu;
}

// GitHub 加速镜像 base（ghproxy 形式：base + '/' + 原 URL）。未设置或不是
// http(s) 地址时返回空串，镜像模式关闭。
function mirrorBase(value) {
  const base = String(value ?? '').trim().replace(/\/+$/, '');
  return /^https?:\/\//i.test(base) ? base : '';
}

if (process.argv[2] === 'install') {
  delete process.env.CAMOUFOX_EXECUTABLE_PATH;
  // The official launcher downloads the kernel through its own package. Wrap
  // that one download so tulai can show bytes, percent and speed; the install
  // path itself stays the official one.
  const mirror = mirrorBase(process.env.TULAI_CAMOUFOX_MIRROR);
  try {
    const { CamoufoxFetcher } = await import('camoufox');
    const { webdl } = await import('camoufox/dist/pkgman.js');
    const getReleases = CamoufoxFetcher.prototype.getReleases;
    if (typeof getReleases === 'function') {
      const { effectivePin } = await import('camoufox/dist/browser-pin.js');
      const { loadConfig } = await import('camoufox/dist/multiversion.js');
      const pin = effectivePin(loadConfig());
      const version = '158.0.1-beta.41';
      if (pin?.repo === 'daijro/camoufox' && pin.repoName === 'official' &&
          pin.tag === `v${version}` && `${pin.version}-${pin.build}` === version) {
        // shortcut: hashes cover Camoufox 0.5.9's pinned build; update with the launcher.
        const hashes = {
          'mac.arm64':'9038f86d666c9780b6fff199d488737b8596c01f24e3e517845a0ecfb0cf8add',
          'mac.x86_64':'1afd6caa1e82caa41a4f56d8519f673e36b0835700b1c9c904e78a84c5711f3a',
          'win.x86_64':'2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f',
          'lin.x86_64':'3f2eb1d0451fa58e45e43e55f5e6181565069791ae60c0574f0ceb1f01341c33',
          'lin.arm64':'60e25f6eb95e13d9ec655bc02913d51057e669cb31542e6ace9d2190ab5380af',
        };
        const base = `https://github.com/${pin.repo}/releases/download/${pin.tag}`;
        // rewrite maps a kernel's GitHub URL to its download source (mirror or direct).
        const kernelAssets = rewrite => Object.entries(hashes).map(([platform, hash]) => {
          const name = `camoufox-${version}-${platform}.zip`;
          return {name, browser_download_url:rewrite(`${base}/${name}`), digest:`sha256:${hash}`};
        });
        const assets = kernelAssets(url => url);
        assets.push({name:'manifest.json', browser_download_url:`${base}/manifest.json`});
        // No manifest asset leaves the interface check at 1, the interface this
        // pinned release ships, so mirror mode never touches github.com at all.
        const mirrorAssets = kernelAssets(url => `${mirror}/${url}`);
        // The GitHub API does not publish asset digests; attach the pinned ones
        // so every kernel download is verified, direct or mirrored.
        const digests = new Map(kernelAssets(url => url).map(asset => [asset.name, asset.digest]));
        const withDigests = releases => {
          for (const release of releases ?? []) {
            for (const asset of release.assets ?? []) {
              const digest = digests.get(asset.name);
              if (digest) asset.digest = digest;
            }
          }
          return releases;
        };
        // Recover from rate limits (403), unreachable or hijacked API routes
        // (404 from DNS pollution, network failures), which otherwise fall
        // through to the launcher's fallback repo camoufox/camoufox — an org
        // that does not exist yet, whose genuine 404 then masks the real error.
        const recoverable = message => /^40[34]\b/.test(message) || message.startsWith('Could not download ');
        CamoufoxFetcher.prototype.getReleases = async function(repo) {
          // A configured mirror usually means api.github.com is unreachable too;
          // serve the pinned release straight from it instead of calling out.
          if (repo === pin.repo && mirror) {
            return [{tag_name:pin.tag, prerelease:false, assets:mirrorAssets}];
          }
          try {
            const releases = await getReleases.call(this, repo);
            return repo === pin.repo ? withDigests(releases) : releases;
          } catch (error) {
            if (repo !== pin.repo || !recoverable(error?.message || '')) throw error;
            return [{tag_name:pin.tag, prerelease:false, assets}];
          }
        };
      }
    }
    let last = 0;
    CamoufoxFetcher.downloadFile = async (file, url) => {
      await webdl(url, undefined, false, file, {
        progressCallback: (done, total) => {
          const now = Date.now();
          if (done !== total && now - last < 200) return;
          last = now;
          emit({ event: 'progress', stage: 'browser', done, total });
        },
      });
    };
  } catch {
    // A future launcher may move this seam; install without live progress.
  }
  const options = await launchOptions({ os: hostOS, headless: true });
  emit({ executable: options.executablePath });
  process.exit(0);
}

let context;
let contextOptions = {};
let requestOptions = {};
let closing = false;
let nextPage = 0;
let nextFrame = 0;
const pages = new Map();
const frames = new Map();
const bindings = new Set();
const controllers = new Map();
const networkOptions = new Map();
const responseBodies = new Map();
const routes = new Map();
const requestIDs = new WeakMap();
const privateContexts = new Map();
const activeRequests = new Map();
let nextRequest = 0;

function requestID(request) {
  if (!requestIDs.has(request)) requestIDs.set(request, String(++nextRequest));
  return requestIDs.get(request);
}
function resourceType(request) {
  return { document:'Document', xhr:'XHR', fetch:'Fetch', image:'Image', script:'Script', stylesheet:'Stylesheet', font:'Font' }[request.resourceType()] || 'Other';
}
function requestInfo(request, maxPost = 1 << 20) {
  const body = request.postDataBuffer();
  return { url:request.url(), method:request.method(), headers:request.headers(),
    postDataEntries:body && body.length <= maxPost ? [{ bytes:body.toString('base64') }] : [] };
}

async function watchResponse(page, id, response) {
  const options = networkOptions.get(id);
  if (!options) return;
  const request = response.request();
  const requestId = requestID(request);
  emit({event:'Network.responseReceived',target:id,data:{requestId,type:resourceType(request),response:{
    url:response.url(),status:response.status(),fromServiceWorker:response.fromServiceWorker(),headers:response.headers(),
  }}});
  if (!['document','xhr','fetch'].includes(request.resourceType())) return;
  try {
    if (Number(response.headers()['content-length']) > options.maxResourceBufferSize) throw new Error('响应超过上限');
    const body = await response.body();
    if (body.length > options.maxResourceBufferSize) throw new Error('响应超过上限');
    responseBodies.set(requestId,{body,target:id});
    let total = 0;
    for (const value of responseBodies.values()) if (value.target === id) total += value.body.length;
    for (const [key,value] of responseBodies) {
      if (total <= options.maxTotalBufferSize) break;
      if (value.target === id) { total -= value.body.length; responseBodies.delete(key); }
    }
    emit({event:'Network.loadingFinished',target:id,data:{requestId}});
  } catch (error) {
    emit({event:'Network.loadingFailed',target:id,data:{requestId,errorText:error.message}});
  }
}

function pageID(page) {
  for (const [id, existing] of pages) if (page === existing) return id;
  const id = String(++nextPage);
  pages.set(id, page);
  page.on('close', () => {
    pages.delete(id);
    for (const [key, frame] of frames) if (frame.page() === page) frames.delete(key);
    networkOptions.delete(id);
    for (const [key,value] of responseBodies) if (value.target === id) responseBodies.delete(key);
    for (const [key,value] of routes) if (value.target === id) { clearTimeout(value.timer); value.route.abort().catch(() => {}); routes.delete(key); }
    for (const [client,target] of activeRequests) if (target === id) { activeRequests.delete(client); client.dispose().catch(() => {}); }
    const privateContext = privateContexts.get(id);
    privateContexts.delete(id);
    if (privateContext) privateContext.close().catch(() => {});
    emit({ event: 'closed', target: id });
  });
  page.on('console', msg => emit({ event: 'console', target: id, data: msg.text().slice(0, 4000) }));
  page.on('request', request => {
    const options = networkOptions.get(id);
    if (!options) return;
    emit({event:'Network.requestWillBeSent',target:id,data:{requestId:requestID(request),request:requestInfo(request,options.maxPostDataSize),
      redirectResponse:request.redirectedFrom() ? {url:request.redirectedFrom().url()} : undefined}});
  });
  page.on('requestfailed', request => {
    if (networkOptions.has(id)) emit({event:'Network.loadingFailed',target:id,data:{requestId:requestID(request),errorText:request.failure()?.errorText || '请求失败'}});
  });
  page.on('response', async response => {
    watchResponse(page,id,response).catch(() => {});
    try {
      if (response.request().resourceType() !== 'document' || response.frame() !== page.mainFrame()) return;
      const address = await response.serverAddr();
      emit({ event: 'document', target: id, data: {
        url: response.url(), ip: response.fromServiceWorker() ? '' : (address?.ipAddress || ''),
      }});
    } catch { /* A page may close while response metadata is being read. */ }
  });
  return id;
}

function pageFor(id) {
  const page = pages.get(String(id));
  if (!page || page.isClosed()) throw new Error('Camoufox 页面已关闭');
  return page;
}

function frameID(frame) {
  for (const [id, existing] of frames) if (existing === frame) return id;
  const id = ++nextFrame;
  frames.set(id, frame);
  return id;
}

function frameTree(frame) {
  return { frame: { id: String(frameID(frame)), url: frame.url() }, childFrames: frame.childFrames().map(frameTree) };
}

function proxyOptions(raw) {
  if (!raw) return undefined;
  const url = new URL(raw);
  const proxy = { server: `${url.protocol}//${url.host}` };
  if (url.protocol === 'socks5:' && (url.username || url.password)) throw new Error('Camoufox 不支持带认证的 SOCKS5 代理，请使用 HTTP 代理');
  if (url.username) proxy.username = decodeURIComponent(url.username);
  if (url.password) proxy.password = decodeURIComponent(url.password);
  return proxy;
}

async function start(params) {
  if (context) throw new Error('Camoufox 已经启动');
  fs.mkdirSync(params.profile, { recursive: true, mode: 0o700 });
  const identityPath = path.join(params.profile, '.tulai-identity.json');
  let saved;
  try { saved = JSON.parse(fs.readFileSync(identityPath, 'utf8')); }
  catch (error) { if (error.code !== 'ENOENT') throw new Error(`账号指纹文件无法读取：${error.message}`); }
  const config = prepareIdentity(saved, installedVerStr(path.dirname(params.executable)).split('.')[0], Boolean(params.proxy));
  let rendering = [true, null];
  if (config?.['webGl:vendor'] && config['webGl:renderer'] && (params.headless || process.platform === 'darwin')) {
    try {
      rendering = await utils.planRendering(utils.getTargetOs(config), undefined, false, false,
        () => cachedHostGpu(params.executable));
    } catch (error) {
      process.stderr.write(`无法确认 Canvas 渲染设备，保留 Camoufox 默认保护：${error.message}\n`);
    }
  }
  const generate = async (identityConfig = config) => {
    const prepared = prepareRendering(identityConfig, ...rendering);
    const options = await launchOptions({
      os: hostOS, headless: Boolean(params.headless), main_world_eval: true,
      config: prepared.config, canvas_noise: prepared.canvasNoise, i_know_what_im_doing: Boolean(saved),
      addons: params.addons || undefined,
      proxy: proxyOptions(params.proxy),
      geoip: params.proxy ? params.geoip !== false : undefined,
      executable_path: params.executable || undefined,
      firefox_user_prefs: {
        'browser.sessionstore.resume_from_crash': false,
        // An aborted operation closes its page; keep the account environment alive.
        'browser.tabs.closeWindowWithLastTab': false,
      },
    });
    const identityJSON = Object.keys(options.env).filter(key => /^CAMOU_CONFIG_\d+$/.test(key))
        .sort((a, b) => Number(a.split('_').at(-1)) - Number(b.split('_').at(-1)))
        .map(key => options.env[key]).join('');
    return {options, identity: JSON.parse(identityJSON)};
  };
  const {options, identity} = saved ? await preserveVoices(saved, config, generate) : await createIdentity(params.profile, generate);
  options.user_data_dir = params.profile;
  options.acceptDownloads = true;
  contextOptions = {viewport:null,serviceWorkers:'block',ignoreHTTPSErrors:Boolean(params.ignoreHTTPSErrors)};
  requestOptions = {proxy:proxyOptions(params.proxy),ignoreHTTPSErrors:contextOptions.ignoreHTTPSErrors};
  options.ignoreHTTPSErrors = contextOptions.ignoreHTTPSErrors;
  context = await Camoufox({ persistent_context: true, from_options: options });
  await context.addInitScript({content: mainWorld(scopeBootstrap)});
  for (const current of context.pages()) await current.evaluate(mainWorld(scopeBootstrap));
  if (saved) {
    const temporary = `${identityPath}.${process.pid}.tmp`;
    try { fs.writeFileSync(temporary, JSON.stringify(identity), {mode:0o600,flag:'wx'}); fs.renameSync(temporary,identityPath); }
    finally { fs.rmSync(temporary,{force:true}); }
  }
  context.setDefaultTimeout(25000);
  context.on('page', pageID);
  context.on('close', () => { if (!closing) process.exit(0); });
  for (const page of context.pages()) pageID(page);
  const page = context.pages()[0] || await context.newPage();
  return { targetId: pageID(page) };
}

async function close() {
  if (closing) return;
  closing = true;
  await context?.close();
}

function mainWorld(expression) { return `mw:${expression}`; }

async function insertText(page, text, signal) {
  if (text === '') { await page.keyboard.press('Backspace'); return; }
  if (!text.includes('\n') || !await page.evaluate(mainWorld('!!document.activeElement?.isContentEditable'))) {
    await page.keyboard.insertText(text); return;
  }
  // Firefox's native editing command retains newlines without a keydown that could submit a chat.
  signal.throwIfAborted();
  const inserted = await page.evaluate(mainWorld(`document.execCommand('insertText', false, ${JSON.stringify(text)})`));
  if (!inserted) throw new Error('富文本输入失败');
}

async function screenshot(page, params) {
  const type = params.format === 'jpeg' ? 'jpeg' : 'png';
  const options = { type, timeout: 15000, animations: 'allow' };
  if (type === 'jpeg') options.quality = params.quality || 80;
  if (params.clip) {
    const { x, y, width, height } = params.clip;
    options.clip = { x, y, width, height };
  }
  const data = await page.screenshot(options);
  if (data.length > maxBytes) throw new Error('截图超过 64 MiB 上限');
  return { data: data.toString('base64') };
}

async function continueRequest(page, target, route, signal) {
  // A shared BrowserContext request survives page.close(); own each guarded request so it can be cancelled.
  const client = await playwrightRequest.newContext({...requestOptions,storageState:{cookies:await page.context().cookies(),origins:[]}});
  activeRequests.set(client,target);
  const abort = () => client.dispose().catch(() => {});
  signal.addEventListener('abort',abort,{once:true});
  try {
    signal.throwIfAborted();
    if (page.isClosed()) throw new Error('Camoufox 页面已关闭');
    // Playwright does not intercept redirected requests. Guarded download pages must fail closed.
    const response = await client.fetch(route.request(),{maxRedirects:0,timeout:25000});
    signal.throwIfAborted();
    if (response.status() >= 300 && response.status() < 400) await route.abort('blockedbyclient');
    else await route.fulfill({response});
  } catch (error) {
    await route.abort('blockedbyclient').catch(() => {});
    throw error;
  } finally {
    signal.removeEventListener('abort',abort);
    activeRequests.delete(client);
    await client.dispose();
  }
}

// These names preserve tulai's existing page API. Every operation uses Playwright's
// Firefox transport over a pipe; no Chromium executable or CDP listener is used.
async function dispatch(method, params, target, signal) {
  signal.throwIfAborted();
  switch (method) {
    case 'start': return start(params);
    case 'ping': return {};
    case 'Browser.close': await close(); return {};
    case 'Target.setDiscoverTargets': return {};
    case 'Target.getTargets':
      return { targetInfos: [...pages].filter(([, page]) => !page.isClosed()).map(([id, page]) => ({ targetId: id, type: 'page', url: page.url() })) };
    case 'Target.attachToTarget': pageFor(params.targetId); return { sessionId: params.targetId };
    case 'Target.createTarget': {
      const parent = params.isolated ? await context.browser().newContext(contextOptions) : context;
      const page = await parent.newPage();
      if (params.isolated) privateContexts.set(pageID(page),parent);
      if (params.url && params.url !== 'about:blank') await page.goto(params.url, { waitUntil: 'domcontentloaded', timeout: 60000 });
      return { targetId: pageID(page) };
    }
    case 'Target.closeTarget': await pageFor(params.targetId).close(); return { success: true };
    case 'Target.activateTarget': await pageFor(params.targetId).bringToFront(); return {};
    case 'Network.getCookies': return { cookies: (await pageFor(target).context().cookies(params.urls)).map(cookie => ({...cookie,session:cookie.expires<0,partitionKey:cookie.partitionKey?{topLevelSite:cookie.partitionKey}:undefined})) };
  }
  const page = pageFor(target);
  switch (method) {
    case 'Network.enable':
      networkOptions.set(target,{maxResourceBufferSize:Math.min(params.maxResourceBufferSize || 8<<20,maxBytes),maxTotalBufferSize:Math.min(params.maxTotalBufferSize || 16<<20,maxBytes),maxPostDataSize:params.maxPostDataSize || 1<<20});
      return {};
    case 'Network.getResponseBody': {
      const value = responseBodies.get(params.requestId);
      responseBodies.delete(params.requestId);
      if (!value || value.target !== target) throw new Error('响应已超限或被回收');
      return {body:value.body.toString('base64'),base64Encoded:true};
    }
    case 'Fetch.enable': {
      const types = new Set((params.patterns || []).map(pattern => pattern.resourceType));
      await page.route('**/*', async route => {
        const request = route.request();
        if (types.size && !types.has(resourceType(request))) { await route.continue(); return; }
        const id = requestID(request);
        const timer = setTimeout(() => { routes.delete(id); route.abort('blockedbyclient').catch(() => {}); },30000);
        routes.set(id,{route,timer,target});
        emit({event:'Fetch.requestPaused',target,data:{requestId:id,request:requestInfo(request),resourceType:resourceType(request),
          redirectedRequestId:request.redirectedFrom() ? requestID(request.redirectedFrom()) : undefined}});
      });
      return {};
    }
    case 'Fetch.continueRequest':
    case 'Fetch.failRequest': {
      const value = routes.get(params.requestId);
      if (!value || value.target !== target) throw new Error('请求已结束');
      routes.delete(params.requestId);clearTimeout(value.timer);
      if (method === 'Fetch.failRequest') { await value.route.abort('blockedbyclient'); return {}; }
      await continueRequest(page,target,value.route,signal);
      return {};
    }
    case 'Fetch.fulfillRequest': {
      const value = routes.get(params.requestId);
      if (!value || value.target !== target) throw new Error('请求已结束');
      routes.delete(params.requestId); clearTimeout(value.timer);
      if (params.responseCode >= 300 && params.responseCode < 400) { await value.route.abort('blockedbyclient'); return {}; }
      await value.route.fulfill({status:params.responseCode,headers:Object.fromEntries((params.responseHeaders || []).map(h=>[h.name,h.value])),body:Buffer.from(params.body || '', 'base64')});
      return {};
    }
    case 'Network.setCookie':
      if (params.partitionKey) throw new Error('不支持通过页面适配接口设置分区 Cookie');
      await page.context().addCookies([{name:params.name,value:params.value,
        ...(params.domain || params.path ? {domain:params.domain || new URL(params.url).hostname,path:params.path || '/'} : {url:params.url}),
        ...(params.expires > 0 ? {expires:params.expires} : {}),
        secure:params.secure,httpOnly:params.httpOnly,sameSite:params.sameSite}]);
      return {success:true};
    case 'Runtime.evaluate': {
      const frame = params.contextId ? frames.get(params.contextId) : page.mainFrame();
      if (!frame) throw new Error('Camoufox 页面框架已关闭');
      try {
        const value = await frame.evaluate(mainWorld(scoped(params.expression)));
        return { result: { type: value === null ? 'object' : typeof value, value: value ?? null } };
      } catch (error) { return { exceptionDetails: { text: error.message } }; }
    }
    case 'Runtime.addBinding':
      if (!bindings.has(params.name)) {
        await context.exposeBinding(params.name, ({ page: source }, data) => {
          emit({ event: 'binding', target: pageID(source), data });
        });
        // Camoufox exposes Playwright bindings in its isolated world.
        const name = JSON.stringify(params.name);
        const forward = `window.addEventListener(${JSON.stringify(pipeEvent)}, event => globalThis[${name}](event.detail))`;
        const invoke = `window[${name}] = data => window.dispatchEvent(new CustomEvent(${JSON.stringify(pipeEvent)}, {detail: data}))`;
        await context.addInitScript({content: forward});
        await context.addInitScript({content: mainWorld(scopedInit(invoke))});
        for (const current of context.pages()) {
          await current.evaluate(forward);
          await current.evaluate(mainWorld(scoped(invoke)));
        }
        bindings.add(params.name);
      }
      return {};
    case 'Page.addScriptToEvaluateOnNewDocument':
      await page.addInitScript({ content: mainWorld(scopedInit(params.source)) }); return { identifier: 'tulai' };
    case 'Page.navigate': await page.goto(params.url, { waitUntil: params.wait === 'commit' ? 'commit' : 'domcontentloaded', timeout: 60000 }); return {};
    case 'Page.reload': await page.reload({ waitUntil: 'domcontentloaded' }); return {};
    case 'Page.close': await page.close(); return {};
    case 'Page.bringToFront': await page.bringToFront(); return {};
    case 'Page.getFrameTree': return { frameTree: frameTree(page.mainFrame()) };
    case 'Page.createIsolatedWorld': return { executionContextId: Number(params.frameId) };
    case 'Page.captureScreenshot': return screenshot(page, params);
    case 'wait': await page.locator(params.selector).first().waitFor({ state: 'visible', timeout: params.timeout }); return {};
    case 'upload': {
      if (params.index === undefined) { await page.locator(params.selector).setInputFiles(params.files); return {}; }
      if (!Number.isInteger(params.index) || params.index < 0) throw new Error('文件输入框序号无效');
      const input = await page.locator('input[type=file]').nth(params.index).elementHandle();
      if (!input) throw new Error('文件输入框不存在');
      try { await input.setInputFiles(params.files); } finally { await input.dispose(); }
      return {};
    }
    case 'chooseUpload': {
      const chooser = page.waitForEvent('filechooser', { timeout: 25000 });
      chooser.catch(() => {});
      await page.evaluate(mainWorld(scoped(params.openJS)));
      const opened = await chooser;
      signal.throwIfAborted();
      await opened.setFiles(params.files);
      return {};
    }
    case 'resource': {
      // A Playwright newPage opens a foreground window in Firefox; reuse this window's tabs.
      const marker = `about:blank#${randomUUID()}`;
      const opened = page.context().waitForEvent('page', {predicate: async candidate => {
        if (candidate.url() === marker) return true;
        if (candidate.url() && candidate.url() !== 'about:blank') return false;
        try { await candidate.waitForURL(marker, {waitUntil:'commit',timeout:3000}); return true; } catch { return false; }
      }, timeout: 10000});
      opened.catch(() => {});
      let resourcePage;
      const abort = () => (resourcePage ? resourcePage.close() : opened.then(candidate => candidate.close())).catch(() => {});
      signal.addEventListener('abort', abort, {once:true});
      try {
        signal.throwIfAborted();
        await page.evaluate(url => { window.open(url, '_blank', 'noopener'); }, marker);
        resourcePage = await opened;
        signal.throwIfAborted();
        const response = await resourcePage.goto(params.url, {waitUntil:'commit',timeout:45000,referer:page.url()});
        if (!response) throw new Error('浏览器未返回资源');
        if (!response.ok()) throw new Error(`HTTP ${response.status()}`);
        if (Number(response.headers()['content-length']) > maxBytes) throw new Error('资源超过 64 MiB 上限');
        const body = await response.body();
        if (body.length > maxBytes) throw new Error('资源超过 64 MiB 上限');
        return { data: body.toString('base64') };
      } finally {
        signal.removeEventListener('abort', abort);
        if (resourcePage) await resourcePage.close().catch(() => {});
        else opened.then(candidate => candidate.close()).catch(() => {});
      }
    }
    case 'Input.insertText': await insertText(page, params.text, signal); return {};
    case 'Input.dispatchKeyEvent': {
      if (params.type === 'char') { if (params.text) await page.keyboard.insertText(params.text); return {}; }
      const key = params.key || params.code;
      if (!key) throw new Error('缺少按键');
      if (params.type === 'keyUp') await page.keyboard.up(key);
      else await page.keyboard.down(key);
      return {};
    }
    case 'Input.dispatchMouseEvent': {
      await page.mouse.move(params.x, params.y);
      signal.throwIfAborted();
      const options = { button: params.button && params.button !== 'none' ? params.button : 'left', clickCount: params.clickCount || 1 };
      if (params.type === 'mousePressed') await page.mouse.down(options);
      else if (params.type === 'mouseReleased') await page.mouse.up(options);
      else if (params.type === 'mouseWheel') await page.mouse.wheel(params.deltaX || 0, params.deltaY || 0);
      return {};
    }
    default: throw new Error(`不支持的 Camoufox 操作：${method}`);
  }
}

const input = readline.createInterface({ input: process.stdin });
input.on('line', line => {
  let request;
  try { request = JSON.parse(line); } catch { return; }
  if (request.method === 'cancel') { controllers.get(request.id)?.abort(); return; }
  const controller = new AbortController();
  controllers.set(request.id, controller);
  const timer = setTimeout(() => controller.abort(), Math.max(1, request.timeout || 60000));
  const aborted = new Promise((_, reject) => controller.signal.addEventListener('abort', () => {
    // Playwright promises alone are not cancellable; close this page to stop late input or script effects.
    if ((request.method === 'Runtime.evaluate' && !request.params?.readOnly) ||
        ['Page.navigate','Page.reload','upload'].includes(request.method) || request.method.startsWith('Input.')) {
      pages.get(String(request.target))?.close().catch(() => {});
    }
    reject(new Error('操作已取消或超时'));
  }, { once: true }));
  Promise.race([dispatch(request.method, request.params || {}, request.target, controller.signal), aborted])
    .then(result => emit({ id: request.id, result }), error => emit({ id: request.id, error: String(error.message || error) }))
    .finally(() => {
      clearTimeout(timer);
      controllers.delete(request.id);
      if (request.method === 'Browser.close') process.exit(0);
    });
});
input.on('close', () => close().finally(() => process.exit(0)));
process.on('SIGTERM', () => close().finally(() => process.exit(0)));
