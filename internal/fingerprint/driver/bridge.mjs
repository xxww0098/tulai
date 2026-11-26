import fs from 'node:fs';
import { randomInt, randomUUID } from 'node:crypto';
import path from 'node:path';
import readline from 'node:readline';
import { request as playwrightRequest } from 'playwright-core';
import {
  HOST_DISPLAY_EXPRESSION,
  buildFingerprintArgs,
  emulatedMedia,
  ensureFingerprintIdentity,
  readHostDisplay,
  validateHostDisplay,
  writeHostDisplay,
} from './fingerprint-identity.mjs';
import { downloadKernel } from './download-watch.mjs';

const mode = process.argv[2];
const downloadFingerprint = mode === 'download-fingerprint';

// stdout is a private pipe to tulai. Launcher diagnostics must not enter the RPC stream.
const send = process.stdout.write.bind(process.stdout);
process.stdout.write = process.stderr.write.bind(process.stderr);
const emit = value => send(JSON.stringify(value) + '\n');
const hostOS = { darwin: 'macos', win32: 'windows', linux: 'linux' }[process.platform];
const maxBytes = 64 << 20;

// Keep per-document application state out of the website's global object.
const scopeEvent = randomUUID(), pipeEvent = randomUUID(), armEvent = randomUUID();
const scopePrefix = `_${randomUUID().replaceAll('-', '')}`;
// A one-letter name reads like a minified bundle symbol in stack frames.
const frameFnName = String.fromCharCode(97 + randomInt(0, 26));
const scopePipeKey = `${scopePrefix}pipe`;
const scopeSlot = `_${randomUUID().replaceAll('-', '')}`;
const scopeRunnerGroup = `_rg${randomUUID().replaceAll('-', '')}`;

// fingerprint-chromium patches V8RuntimeAgentImpl::addBindings to a no-op to
// avoid Runtime.enable detection, breaking Playwright's exposeBinding across
// navigations. Attach a private CDP binding inside a dedicated isolated world
// whenever its execution context is created instead.
const fpWorld = `_w${randomUUID().replaceAll('-', '')}`;
const fpBind = `_b${randomUUID().replaceAll('-', '')}`;
const fpGuard = `_g${randomUUID().replaceAll('-', '')}`;
const fpForwarder = `(()=>{
  const g=${JSON.stringify(fpGuard)};
  if(globalThis[g])return;
  globalThis[g]=1;
  const k=${JSON.stringify(fpBind)};
  const q=[];
  let scheduled=false, tries=0;
  const flush=()=>{
    const fn=globalThis[k];
    if(typeof fn!=='function')return false;
    tries=0;
    while(q.length)fn(q.shift());
    return true;
  };
  const pump=()=>{
    scheduled=false;
    if(!q.length||flush())return;
    if(++tries>2000){q.length=0;tries=0;return;}
    scheduled=true;setTimeout(pump,2);
  };
  window.addEventListener(${JSON.stringify(pipeEvent)},event=>{
    event.stopImmediatePropagation?.();
    q.push(String(event.detail??''));
    tries=0;
    if(!flush()&&!scheduled){scheduled=true;setTimeout(pump,1);}
  },true);
})()`;

// Isolated-world download assistant host.
const dlWorld = `_dw${randomUUID().replaceAll('-', '')}`;
const dlBind = `_db${randomUUID().replaceAll('-', '')}`;
const dlResolve = `_dr${randomUUID().replaceAll('-', '')}`;
const dlGuard = `_dg${randomUUID().replaceAll('-', '')}`;

function makeScopeBootstrap(returnRunner) {
  return `(()=>{
  const ${scopePrefix}call=Function.prototype.call.bind(Function.prototype.call);
  const ${scopePrefix}dispatch=EventTarget.prototype.dispatchEvent;
  const ${scopePrefix}addListener=EventTarget.prototype.addEventListener;
  const ${scopePrefix}stopImm=Event.prototype.stopImmediatePropagation;
  const ${scopePrefix}CE=window.CustomEvent;
  const ${scopePrefix}create=Object.create;
  const ${scopePrefix}defineProp=Object.defineProperty;
  const ${scopePrefix}getOwnDesc=Object.getOwnPropertyDescriptor;
  const ${scopePrefix}getProto=Object.getPrototypeOf;
  const ${scopePrefix}keys=Object.keys;
  const ${scopePrefix}rGet=Reflect.get, ${scopePrefix}rSet=Reflect.set, ${scopePrefix}rHas=Reflect.has, ${scopePrefix}rDel=Reflect.deleteProperty, ${scopePrefix}rOwnKeys=Reflect.ownKeys, ${scopePrefix}rGetDesc=Reflect.getOwnPropertyDescriptor, ${scopePrefix}rGetProto=Reflect.getPrototypeOf;
  const ${scopePrefix}fnBind=Function.prototype.bind;
  const ${scopePrefix}reTest=RegExp.prototype.test;
  const ${scopePrefix}isArray=Array.isArray, ${scopePrefix}isFinite=Number.isFinite, ${scopePrefix}toStr=String;
  const ${scopePrefix}Set=Set, ${scopePrefix}setHas=Set.prototype.has, ${scopePrefix}setAdd=Set.prototype.add, ${scopePrefix}setDel=Set.prototype.delete;
  const ${scopePrefix}Date=Date, ${scopePrefix}dateGetTime=Date.prototype.getTime, ${scopePrefix}dateToISO=Date.prototype.toISOString;
  const ${scopePrefix}Promise=Promise, ${scopePrefix}promiseThen=Promise.prototype.then, ${scopePrefix}promiseResolve=Promise.resolve.bind(Promise);
  const ${scopePrefix}SyntaxError=SyntaxError, ${scopePrefix}perfEntries=Function.prototype.call.bind(performance.getEntriesByType,performance);
  const ${scopePrefix}Node=typeof Node==='function'?Node:null;
  const ${scopePrefix}Proxy=Proxy, ${scopePrefix}setTimer=window.setTimeout;
  const ${scopePrefix}window=window;
  const ${scopePrefix}def=(obj,key,value)=>{const d=${scopePrefix}create(null);d.value=value;d.enumerable=true;d.writable=true;d.configurable=true;${scopePrefix}defineProp(obj,key,d)};
  const ${scopePrefix}defHidden=(obj,key,value)=>{const d=${scopePrefix}create(null);d.value=value;d.enumerable=false;d.writable=true;d.configurable=true;${scopePrefix}defineProp(obj,key,d)};
  const ${scopePrefix}own=(obj,key)=>{const d=${scopePrefix}getOwnDesc(obj,key);return d?d.value:undefined};
  ${returnRunner ? '' : `const ${scopePrefix}checkOpts=${scopePrefix}create(null);${scopePrefix}checkOpts.bubbles=false;${scopePrefix}checkOpts.cancelable=false;${scopePrefix}checkOpts.composed=false;const ${scopePrefix}check=new ${scopePrefix}CE(${JSON.stringify(scopeEvent)},${scopePrefix}checkOpts);${scopePrefix}call(${scopePrefix}dispatch,${scopePrefix}window,${scopePrefix}check);if(${scopePrefix}own(${scopePrefix}check,'done'))return;`}
  const ${scopePrefix}state=${scopePrefix}create(null);
  const ${scopePrefix}privateRe=/^(?:__tulai|tulai)/i;
  const ${scopePrefix}private=key=>typeof key==='string'&&${scopePrefix}call(${scopePrefix}reTest,${scopePrefix}privateRe,key);
  const ${scopePrefix}bindMethods=${scopePrefix}create(null);
  ${scopePrefix}bindMethods.addEventListener=1;${scopePrefix}bindMethods.removeEventListener=1;${scopePrefix}bindMethods.dispatchEvent=1;${scopePrefix}bindMethods.getSelection=1;${scopePrefix}bindMethods.scrollBy=1;${scopePrefix}bindMethods.scrollTo=1;
  const ${scopePrefix}pipe=data=>{
    const opts=${scopePrefix}create(null);
    opts.bubbles=false;opts.cancelable=false;opts.composed=false;
    opts.detail=data==null?'':${scopePrefix}toStr(data);
    ${scopePrefix}call(${scopePrefix}dispatch,${scopePrefix}window,new ${scopePrefix}CE(${JSON.stringify(pipeEvent)},opts));
  };
  const ${scopePrefix}facade=new ${scopePrefix}Proxy(${scopePrefix}create(null),{
    get(_target,key){
      if(${scopePrefix}private(key))return ${scopePrefix}state[key];
      const value=${scopePrefix}rGet(${scopePrefix}window,key,${scopePrefix}window);
      if(value===${scopePrefix}window)return ${scopePrefix}facade;
      return typeof value==='function'&&typeof key==='string'&&${scopePrefix}bindMethods[key]?${scopePrefix}call(${scopePrefix}fnBind,value,${scopePrefix}window):value;
    },
    set(_target,key,value){if(${scopePrefix}private(key)){${scopePrefix}state[key]=value;return true}return ${scopePrefix}rSet(${scopePrefix}window,key,value,${scopePrefix}window)},
    has(_target,key){return ${scopePrefix}rHas(${scopePrefix}private(key)?${scopePrefix}state:${scopePrefix}window,key)},
    deleteProperty(_target,key){return ${scopePrefix}rDel(${scopePrefix}private(key)?${scopePrefix}state:${scopePrefix}window,key)},
    ownKeys(){
      const seen=new ${scopePrefix}Set(), out=[];
      const wk=${scopePrefix}rOwnKeys(${scopePrefix}window), sk=${scopePrefix}rOwnKeys(${scopePrefix}state);
      for(let i=0;i<wk.length;i++){const k=wk[i];if(!${scopePrefix}call(${scopePrefix}setHas,seen,k)){${scopePrefix}call(${scopePrefix}setAdd,seen,k);${scopePrefix}def(out,out.length,k)}}
      for(let i=0;i<sk.length;i++){const k=sk[i];if(!${scopePrefix}call(${scopePrefix}setHas,seen,k)){${scopePrefix}call(${scopePrefix}setAdd,seen,k);${scopePrefix}def(out,out.length,k)}}
      return out;
    },
    getOwnPropertyDescriptor(_target,key){
      const descriptor=${scopePrefix}rGetDesc(${scopePrefix}private(key)?${scopePrefix}state:${scopePrefix}window,key);
      if(!descriptor)return undefined;
      const copy=${scopePrefix}create(null), ks=${scopePrefix}keys(descriptor);
      for(let i=0;i<ks.length;i++){copy[ks[i]]=descriptor[ks[i]]}
      copy.configurable=true;
      return copy;
    },
    getPrototypeOf(){return ${scopePrefix}rGetProto(${scopePrefix}window)}
  });
  const ${scopePrefix}scope=new ${scopePrefix}Proxy(${scopePrefix}create(null),{
    has(_target,key){return key==='window'||key==='globalThis'||key===${JSON.stringify(scopePipeKey)}||${scopePrefix}private(key)},
    get(_target,key){if(key===Symbol.unscopables)return undefined;if(key===${JSON.stringify(scopePipeKey)})return ${scopePrefix}pipe;return key==='window'||key==='globalThis'?${scopePrefix}facade:${scopePrefix}state[key]},
    set(_target,key,value){${scopePrefix}state[key]=value;return true}
  });
  const ${scopePrefix}errText=err=>{
    try{
      if(err&&typeof err==='object'&&'message' in err&&err.message!=null)return ${scopePrefix}toStr(err.message);
      return ${scopePrefix}toStr(err);
    }catch{return '页面脚本执行失败'}
  };
  const ${scopePrefix}errObj=err=>{const r=${scopePrefix}create(null);r.ok=false;r.text=${scopePrefix}errText(err);return r};
  const ${scopePrefix}serialize=(val,seen,depth)=>{
    if(depth>64)return null;
    if(val===undefined||typeof val==='function'||typeof val==='symbol')return undefined;
    if(val===null||typeof val==='boolean'||typeof val==='string')return val;
    if(typeof val==='number')return ${scopePrefix}isFinite(val)?val:null;
    if(typeof val==='bigint')return ${scopePrefix}toStr(val);
    if(typeof val!=='object')return undefined;
    if(val===${scopePrefix}window||val===${scopePrefix}facade||(${scopePrefix}Node&&val instanceof ${scopePrefix}Node))return null;
    if(${scopePrefix}call(${scopePrefix}setHas,seen,val))return null;
    if(val instanceof ${scopePrefix}Date){
      const t=${scopePrefix}call(${scopePrefix}dateGetTime,val);
      return ${scopePrefix}isFinite(t)?${scopePrefix}call(${scopePrefix}dateToISO,val):null;
    }
    ${scopePrefix}call(${scopePrefix}setAdd,seen,val);
    try{
      let toJSON;
      try{toJSON=val.toJSON}catch{toJSON=null}
      if(typeof toJSON==='function'){
        let converted, ok=false;
        try{converted=${scopePrefix}call(toJSON,val,'');ok=true}catch{ok=false}
        if(ok&&converted!==val)return ${scopePrefix}serialize(converted,seen,depth+1);
      }
      if(${scopePrefix}isArray(val)){
        const arr=[];
        for(let i=0;i<val.length;i++){
          let elem;try{elem=val[i]}catch{elem=undefined}
          const item=${scopePrefix}serialize(elem,seen,depth+1);
          ${scopePrefix}def(arr,i,item===undefined?null:item);
        }
        return arr;
      }
      const out={}, ks=${scopePrefix}keys(val);
      for(let i=0;i<ks.length;i++){
        const k=ks[i];let prop;
        try{prop=val[k]}catch{continue}
        const item=${scopePrefix}serialize(prop,seen,depth+1);
        if(item!==undefined)${scopePrefix}def(out,k,item);
      }
      return out;
    }finally{${scopePrefix}call(${scopePrefix}setDel,seen,val)}
  };
  const ${scopePrefix}finish=val=>{
    const r=${scopePrefix}create(null);r.ok=true;
    const type=val===null?'object':typeof val;
    if(val===undefined||type==='function'||type==='symbol'){r.type='undefined';r.value=null;return r}
    const value=${scopePrefix}serialize(val,new ${scopePrefix}Set(),0);
    r.type=value===null?'object':typeof value;
    r.value=value===undefined?null:value;
    return r;
  };
  const ${scopePrefix}eval=eval;
  // The suspended generator retains top-level var bindings between evaluations,
  // so every source runs in its scope: an expression inside a function compiled
  // there, a statement script directly.
  const ${scopePrefix}runner=(function*(){
    const eval=${scopePrefix}eval;
    while(true){
      const ${scopePrefix}src=yield;
      try{
        let ${scopePrefix}fn=null;
        with(${scopePrefix}scope){
          try{${scopePrefix}fn=eval('const ${frameFnName}=function(){with(${scopePrefix}scope){return (\\n'+${scopePrefix}src+'\\n);\\n}};${frameFnName}'+${scopePrefix}frameTag())}
          catch(${scopePrefix}error){if(!(${scopePrefix}error instanceof ${scopePrefix}SyntaxError))throw ${scopePrefix}error}
        }
        const ok=${scopePrefix}create(null);
        if(${scopePrefix}fn)ok.fn=${scopePrefix}fn;
        else with(${scopePrefix}scope){ok.value=eval(${scopePrefix}tagged(${scopePrefix}src,${scopePrefix}frameTag()))}
        yield ok
      }
      catch(${scopePrefix}error){const fail=${scopePrefix}create(null);fail.failed=true;fail.error=${scopePrefix}error;yield fail}
    }
  })();${scopePrefix}runner.next();
  const ${scopePrefix}genNext=${scopePrefix}getProto(${scopePrefix}runner).next;
  // Script URL that page hooks read in stack traces: a bundle the page really
  // loaded, else its own document, as an inline script would. Automation frames
  // then look like the site's own code instead of anonymous injected scripts.
  let ${scopePrefix}frameURL='';
  const ${scopePrefix}url=()=>{
    if(${scopePrefix}frameURL)return ${scopePrefix}frameURL;
    try{
      if(/^https?:$/.test(location.protocol)){
        const own=location.origin, entries=${scopePrefix}perfEntries('resource');
        for(let i=0;i<entries.length;i++){
          const entry=entries[i];
          if(entry.initiatorType==='script'&&entry.name.startsWith(own+'/')){${scopePrefix}frameURL=entry.name;break}
        }
        ${scopePrefix}frameURL=${scopePrefix}frameURL||location.href.split('#')[0];
      }else{${scopePrefix}frameURL=location.href}
    }catch{${scopePrefix}frameURL=''}
    return ${scopePrefix}frameURL;
  };
  // The frames of an eval'd script name no URL unless its source ends with one.
  const ${scopePrefix}tagged=(${scopePrefix}src,${scopePrefix}tag)=>${scopePrefix}tag&&!/source(?:Mapping)?URL=/.test(${scopePrefix}src)?${scopePrefix}src+${scopePrefix}tag:${scopePrefix}src;
  const ${scopePrefix}frameTag=()=>{const ${scopePrefix}at=${scopePrefix}url();return ${scopePrefix}at?'\\n//# sourceURL='+${scopePrefix}at:''};
  // Runs a compiled function from a fresh microtask: page hooks then see only
  // that function's own frame, never the debugger frames that submitted it.
  const ${scopePrefix}hop=${scopePrefix}fn=>${scopePrefix}promiseThen.call(
    ${scopePrefix}promiseResolve(undefined),
    ${scopePrefix}fnBind.call(${scopePrefix}fn,undefined));
  const ${scopePrefix}exec=${scopePrefix}src=>{
    try{
      const result=${scopePrefix}call(${scopePrefix}genNext,${scopePrefix}runner,${scopePrefix}src).value;
      ${scopePrefix}call(${scopePrefix}genNext,${scopePrefix}runner);
      if(result.failed)return ${scopePrefix}errObj(result.error);
      return ${scopePrefix}promiseThen.call(result.fn?${scopePrefix}hop(result.fn):${scopePrefix}promiseResolve(result.value),${scopePrefix}finish,${scopePrefix}errObj);
    }catch(${scopePrefix}error){
      return ${scopePrefix}errObj(${scopePrefix}error);
    }
  };
  ${scopePrefix}call(${scopePrefix}addListener,${scopePrefix}window,${JSON.stringify(armEvent)},event=>{
    ${scopePrefix}call(${scopePrefix}stopImm,event);
    ${scopePrefix}defHidden(${scopePrefix}window,${JSON.stringify(scopeSlot)},${scopePrefix}exec);
    ${scopePrefix}call(${scopePrefix}setTimer,${scopePrefix}window,()=>{try{delete ${scopePrefix}window[${JSON.stringify(scopeSlot)}]}catch{}},1000);
  },true);
  ${scopePrefix}call(${scopePrefix}addListener,${scopePrefix}window,${JSON.stringify(scopeEvent)},event=>{
    const run=${scopePrefix}own(event,'run');
    if(event.detail===null&&!run){${scopePrefix}defHidden(event,'done',true);return}
    try{
      if(run)${scopePrefix}defHidden(event,'value',run(${scopePrefix}scope));
      else{
        const result=${scopePrefix}call(${scopePrefix}genNext,${scopePrefix}runner,event.detail).value;
        ${scopePrefix}call(${scopePrefix}genNext,${scopePrefix}runner);
        if(result.failed)throw result.error;
        ${scopePrefix}defHidden(event,'value',result.fn?${scopePrefix}hop(result.fn):result.value);
      }
    }catch(error){${scopePrefix}defHidden(event,'failed',true);${scopePrefix}defHidden(event,'error',error)}
    ${scopePrefix}defHidden(event,'done',true);
  });
  ${returnRunner ? `return ${scopePrefix}exec;` : ''}
})()`;
}

const scopeBootstrap = makeScopeBootstrap(false);
const bootstrapAndReturnExpr = makeScopeBootstrap(true);
// Arm the main-world runner from the isolated world (fpWorld) using isolated
// native Event/dispatchEvent, avoiding CommandLineAPI (window.getEventListeners),
// main-world dispatchEvent/CustomEvent hooks, and Object.prototype traps.
const armRunnerExpr = `window.dispatchEvent(new Event(${JSON.stringify(armEvent)}))`;
const takeRunnerExpr = `(()=>{const k=${JSON.stringify(scopeSlot)},f=globalThis[k];if(f){delete globalThis[k];return f;}return null;})()`;

function scoped(source) {
  return `(()=>{const opts=Object.create(null);opts.bubbles=false;opts.cancelable=false;opts.composed=false;opts.detail=${JSON.stringify(source)};
    const event=new CustomEvent(${JSON.stringify(scopeEvent)},opts);
    const own=k=>{const d=Object.getOwnPropertyDescriptor(event,k);return d?d.value:undefined};
    window.dispatchEvent(event);if(!own('done')){${scopeBootstrap};window.dispatchEvent(event)}
    if(!own('done'))throw Error('页面脚本环境不可用');if(own('failed'))throw own('error');return own('value')})()`;
}

// A document script is compiled once for every navigation, so it carries the
// address of the page it was registered on: its stack frames then read as that
// page's inline code instead of an unnamed injected script.
function pageScriptURL(url) {
  const clean = String(url ?? '').split('#')[0];
  return /^https?:\/\//i.test(clean) ? clean : '';
}

function scopedInit(source, url = '') {
  // New-document scripts must run without dynamic eval under strict CSP.
  return `(()=>{const opts=Object.create(null);opts.bubbles=false;opts.cancelable=false;opts.composed=false;
    const event=new CustomEvent(${JSON.stringify(scopeEvent)},opts);
    const own=k=>{const d=Object.getOwnPropertyDescriptor(event,k);return d?d.value:undefined};
    const d=Object.create(null);d.value=function(${scopePrefix}arg){with(${scopePrefix}arg){${source}\n}};d.configurable=true;d.writable=true;Object.defineProperty(event,'run',d);
    window.dispatchEvent(event);if(!own('done')){${scopeBootstrap};window.dispatchEvent(event)}
    if(!own('done'))throw Error('页面脚本环境不可用');if(own('failed'))throw own('error')})()${url ? `\n//# sourceURL=${url}` : ''}`;
}

// GitHub 加速镜像 base（ghproxy 形式：base + '/' + 原 URL）。未设置或不是
// http(s) 地址时返回空串，镜像模式关闭。
function mirrorBase(value) {
  const base = String(value ?? '').trim().replace(/\/+$/, '');
  return /^https?:\/\//i.test(base) ? base : '';
}

function parseMirrorList(value) {
  const seen = new Set();
  const out = [];
  for (const part of String(value ?? '').split(',')) {
    const base = mirrorBase(part);
    if (!base || seen.has(base)) continue;
    seen.add(base);
    out.push(base);
  }
  return out;
}

function buildDownloadCandidates(url, { mirror, mirrors, directFirst }) {
  const allMirrors = [];
  const seenMirror = new Set();
  for (const item of [mirror, ...(mirrors ?? [])]) {
    const base = mirrorBase(item);
    if (!base || seenMirror.has(base)) continue;
    seenMirror.add(base);
    allMirrors.push(base);
  }
  let canonical = String(url ?? '').trim();
  for (const base of allMirrors) {
    const prefix = `${base}/`;
    if (canonical.startsWith(prefix)) {
      canonical = canonical.slice(prefix.length);
      break;
    }
  }
  const canMirror = /^https:\/\/github\.com\//i.test(canonical);
  const mirrorUrls = canMirror ? allMirrors.map(base => `${base}/${canonical}`) : [];
  if (directFirst) {
    return [canonical, ...mirrorUrls];
  }
  if (mirrorUrls.length > 0) {
    return [...mirrorUrls, canonical];
  }
  return [String(url ?? '').trim()];
}

if (downloadFingerprint) {
  const url = String(process.env.TULAI_FINGERPRINT_URL ?? '').trim();
  const dest = String(process.env.TULAI_FINGERPRINT_DEST ?? '').trim();
  if (!url || !dest) {
    throw new Error('缺少 Fingerprint 内核下载地址或保存路径');
  }
  const mirror = mirrorBase(process.env.TULAI_FINGERPRINT_MIRROR);
  const mirrors = parseMirrorList(process.env.TULAI_FINGERPRINT_MIRRORS);
  const directFirst = process.env.TULAI_FINGERPRINT_DIRECT_FIRST === '1';
  const candidates = buildDownloadCandidates(url, { mirror, mirrors, directFirst });
  let last = 0;
  const file = fs.createWriteStream(dest, { mode: 0o600 });
  const streamError = new Promise((_, reject) => file.once('error', reject));
  streamError.catch(() => {});
  try {
    await Promise.race([
      downloadKernel(file, candidates, {
        label: 'Fingerprint 内核',
        onProgress: (done, total) => {
          const now = Date.now();
          if (done !== total && now - last < 200) return;
          last = now;
          emit({ event: 'progress', stage: 'browser', done, total });
        },
      }),
      streamError,
    ]);
    await new Promise((resolve, reject) => {
      file.once('finish', resolve);
      file.once('error', reject);
      file.end();
    });
  } catch (error) {
    file.destroy();
    fs.rmSync(dest, { force: true });
    throw error;
  }
  emit({ downloaded: dest });
  process.exit(0);
}

let context;
let contextOptions = {};
let requestOptions = {};
let launchProxy;
let launchHeadless = false;
let launchMedia = [];
let startup;
let closing;
let emptyWindowTimer = null;
let nextPage = 0;
let nextFrame = 0;
let downloadAssistant = null;
const pages = new Map();
const programmaticCloses = new WeakSet();
const frames = new Map();
const isolatedWorlds = new Map();
const frameIsolatedWorlds = new WeakMap();
const mousePositions = new WeakMap();
const bindings = new Set();
const contextBindings = new WeakMap();
const controllers = new Map();
const networkOptions = new Map();
const responseBodies = new Map();
const routes = new Map();
const requestIDs = new WeakMap();
const privateContexts = new Map();
const privateClosing = new Map();
const activeRequests = new Map();
const fingerprintBridges = new Map();
const downloadTemuTabs = new Map();
const resourcePages = new WeakSet();
let pendingResourcePages = 0;
let nextRequest = 0;

function loadDownloadAssistant(addons) {
  if (!Array.isArray(addons) || addons.length === 0) return null;
  const dir = String(addons[0] ?? '').trim();
  if (!dir) return null;
  const settingsRaw = fs.readFileSync(path.join(dir, 'settings.js'), 'utf8');
  const eq = settingsRaw.indexOf('=');
  if (eq < 0) throw new Error('下载助手配置无效');
  const settings = JSON.parse(settingsRaw.slice(eq + 1).trim());
  if (!settings || typeof settings.endpoint !== 'string' || typeof settings.token !== 'string') {
    throw new Error('下载助手配置缺少接口信息');
  }
  const temuSrc = fs.readFileSync(path.join(dir, 'temu.js'), 'utf8');
  const contentSrc = fs.readFileSync(path.join(dir, 'content.js'), 'utf8');
  const script = `(()=>{
    if(window!==window.top)return;
    if(location.protocol!=='http:'&&location.protocol!=='https:')return;
    const g=${JSON.stringify(dlGuard)};
    if(globalThis[g])return;
    globalThis[g]=1;
    const bindKey=${JSON.stringify(dlBind)};
    const resolveKey=${JSON.stringify(dlResolve)};
    const pending=new Map();
    let seq=0;
    globalThis[resolveKey]=(id,payload)=>{
      const fn=pending.get(id);
      if(fn){pending.delete(id);fn(payload);}
    };
    const send=(message)=>new Promise((resolve,reject)=>{
      const id=String(++seq);
      pending.set(id,resolve);
      const raw=JSON.stringify({id,message});
      let tries=0;
      const attempt=()=>{
        const fn=globalThis[bindKey];
        if(typeof fn==='function'){fn(raw);return;}
        if(++tries>2000){pending.delete(id);reject(new Error('download assistant unavailable'));return;}
        setTimeout(attempt,2);
      };
      attempt();
    });
    const browser={runtime:{sendMessage:send}};
    const boot=()=>{
      if(!document.documentElement){setTimeout(boot,20);return;}
      ${temuSrc}
      ;
      ${contentSrc}
    };
    if(document.readyState==='loading'){
      document.addEventListener('DOMContentLoaded',boot,{once:true});
    }else{
      boot();
    }
  })()`;
  return { endpoint: settings.endpoint, token: settings.token, script };
}

async function handleDownloadAssistantMessage(page, pid, message, cdp = null, executionContextId = 0) {
  if (!downloadAssistant || !message || !['DOWNLOAD_PAGE', 'TEMU_CACHE_LOAD', 'TEMU_CACHE_SAVE'].includes(message.type)) {
    return { ok: false, message: '请在网页中点击下载按钮' };
  }
  try {
    let tabURL = page.url();
    if (cdp && executionContextId && (message.type === 'DOWNLOAD_PAGE' || tabURL !== message.url)) {
      try {
        const live = await cdp.send('Runtime.evaluate', {
          contextId: executionContextId,
          expression: 'location.href',
          returnByValue: true,
        });
        if (typeof live?.result?.value === 'string' && /^https?:\/\//i.test(live.result.value)) {
          tabURL = live.result.value;
        }
      } catch { /* Fall back to page.url() if context navigated or in mocks. */ }
    }
    const url = new URL(tabURL);
    if (message.type !== 'DOWNLOAD_PAGE') {
      if (tabURL !== message.url || url.protocol !== 'https:' || !['temu.com', 'www.temu.com'].includes(url.hostname) ||
          !/^\/(?:[a-z]{2,3}(?:-[a-z]{2,4})?\/)?search_result\.html$/i.test(url.pathname) ||
          typeof message.key !== 'string' || message.key.length > 8192) {
        return { ok: false };
      }
      if (message.type === 'TEMU_CACHE_LOAD') {
        const cached = downloadTemuTabs.get(pid);
        return {
          ok: true,
          rows: cached && cached.key === message.key ? cached.rows : [],
          truncated: !!(cached && cached.key === message.key && cached.truncated),
        };
      }
      if (!Array.isArray(message.rows) || message.rows.length > 10000 ||
          Buffer.byteLength(JSON.stringify(message.rows), 'utf8') > 8 * 1024 * 1024) {
        return { ok: false };
      }
      downloadTemuTabs.set(pid, { key: message.key, rows: message.rows, truncated: !!message.truncated });
      return { ok: true };
    }
    if (!/^[0-9a-f-]{36}$/i.test(message.marker || '')) {
      return { ok: false, message: '请在网页中点击下载按钮' };
    }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
      return { ok: false, message: '请在当前网页重试' };
    }
    const response = await fetch(downloadAssistant.endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Tulai-Token': downloadAssistant.token },
      body: JSON.stringify({ url: tabURL, marker: message.marker }),
      signal: AbortSignal.timeout(35000),
    });
    const text = await response.text();
    try {
      const parsed = JSON.parse(text);
      if (parsed && typeof parsed === 'object') return parsed;
    } catch { /* Non-JSON response body. */ }
    return { ok: false, message: response.ok ? '已加入 tulai 下载列表' : '下载请求被拒绝，请重试' };
  } catch {
    return { ok: false, message: '无法连接 tulai，请确认应用正在运行' };
  }
}

function isStaleCDPContextError(error) {
  const msg = String(error?.message || error || '');
  return /Cannot find context|Could not find object|Execution context was destroyed|Inspected target navigated|Object reference chain/i.test(msg);
}

function notifyMainContextWaiters(entry) {
  if (!entry?.contextWaiters?.size) return;
  const waiters = [...entry.contextWaiters];
  entry.contextWaiters.clear();
  for (const wake of waiters) wake();
}

function waitForMainContext(entry, page, timeoutMs = 500) {
  const current = entry.frameContexts.get(entry.mainFrameId);
  if (current || !entry.hasSeenDefaultContext || timeoutMs <= 0 || page.isClosed()) {
    return Promise.resolve(current || null);
  }
  return new Promise(resolve => {
    let done = false;
    const finish = () => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      entry.contextWaiters.delete(finish);
      resolve(entry.frameContexts.get(entry.mainFrameId) || null);
    };
    const timer = setTimeout(finish, timeoutMs);
    entry.contextWaiters.add(finish);
  });
}

async function ensureMainFpContext(entry) {
  let fpCtxId = entry.fpContexts.get(entry.mainFrameId) || null;
  if (fpCtxId) return fpCtxId;
  try {
    const iso = await entry.cdp.send('Page.createIsolatedWorld', { frameId: entry.mainFrameId, worldName: fpWorld });
    if (iso?.executionContextId) {
      fpCtxId = iso.executionContextId;
      entry.fpContexts.set(entry.mainFrameId, fpCtxId);
    }
  } catch { /* Frame may navigate or close concurrently. */ }
  return fpCtxId;
}

async function acquireMainRunner(entry, ctxState) {
  if (ctxState.runnerId) return ctxState.runnerId;
  if (ctxState.runnerPromise) return ctxState.runnerPromise;
  ctxState.runnerPromise = (async () => {
    let objectId = null;
    for (let armAttempt = 0; armAttempt < 2; armAttempt++) {
      const fpCtxId = await ensureMainFpContext(entry);
      if (!fpCtxId) break;
      try {
        const armP = entry.cdp.send('Runtime.evaluate', {
          contextId: fpCtxId,
          expression: armRunnerExpr,
        });
        const takeP = entry.cdp.send('Runtime.evaluate', {
          contextId: ctxState.contextId,
          returnByValue: false,
          objectGroup: scopeRunnerGroup,
          expression: takeRunnerExpr,
        });
        const [armRes, takeRes] = await Promise.all([armP, takeP]);
        if (armRes?.exceptionDetails) break;
        objectId = takeRes?.result?.objectId || null;
        if (objectId) break;
        // armEvent dispatched cleanly in fpWorld but scopeBootstrap was not yet
        // installed on this document (e.g. initial about:blank); install directly.
        const bootRes = await entry.cdp.send('Runtime.evaluate', {
          contextId: ctxState.contextId,
          returnByValue: false,
          objectGroup: scopeRunnerGroup,
          expression: bootstrapAndReturnExpr,
        });
        objectId = bootRes?.result?.objectId || null;
        break;
      } catch (error) {
        if (armAttempt === 0 && isStaleCDPContextError(error)) {
          entry.fpContexts.delete(entry.mainFrameId);
          if (entry.frameContexts.get(entry.mainFrameId) !== ctxState) throw error;
          continue;
        }
        throw error;
      }
    }
    if (!objectId && !entry.fpContexts.get(entry.mainFrameId)) {
      const bootRes = await entry.cdp.send('Runtime.evaluate', {
        contextId: ctxState.contextId,
        returnByValue: false,
        objectGroup: scopeRunnerGroup,
        expression: bootstrapAndReturnExpr,
      });
      objectId = bootRes?.result?.objectId || null;
    }
    if (objectId && entry.frameContexts.get(entry.mainFrameId) === ctxState) {
      ctxState.runnerId = objectId;
    }
    return objectId;
  })().finally(() => {
    ctxState.runnerPromise = null;
  });
  return ctxState.runnerPromise;
}

async function evaluateScoped(page, frame, expression) {
  const isMain = !frame || (typeof page.mainFrame === 'function' && frame === page.mainFrame());
  if (isMain && !resourcePages.has(page)) {
    await ensureFingerprintPageBridge(page);
    const entry = fingerprintBridges.get(page);
    if (entry?.cdp && entry.mainFrameId) {
      for (let attempt = 0; attempt < 3; attempt++) {
        const ctxState = entry.frameContexts.get(entry.mainFrameId) || await waitForMainContext(entry, page, 500);
        if (!ctxState) break;
        try {
          const runnerId = await acquireMainRunner(entry, ctxState);
          if (!runnerId) break;
          const reply = await entry.cdp.send('Runtime.callFunctionOn', {
            objectId: runnerId,
            functionDeclaration: 'function(src) { return this(src); }',
            arguments: [{ value: expression }],
            awaitPromise: true,
            returnByValue: true,
            userGesture: true,
          });
          if (reply?.exceptionDetails) {
            const text = reply.exceptionDetails.exception?.description || reply.exceptionDetails.text || '页面脚本执行失败';
            return { exceptionDetails: { text: String(text).split('\n', 1)[0] } };
          }
          const out = reply?.result?.value;
          if (out && typeof out === 'object' && 'ok' in out) {
            if (!out.ok) return { exceptionDetails: { text: out.text || '页面脚本执行失败' } };
            return { result: { type: out.type ?? (out.value === null ? 'object' : typeof out.value), value: out.value ?? null } };
          }
          return { result: { type: out === null ? 'object' : typeof out, value: out ?? null } };
        } catch (error) {
          ctxState.runnerId = null;
          if (attempt < 2 && isStaleCDPContextError(error)) continue;
          if (isStaleCDPContextError(error)) break;
          return { exceptionDetails: { text: String(error?.message || error) } };
        }
      }
    }
  }
  const target = frame || (typeof page.mainFrame === 'function' ? page.mainFrame() : page);
  try {
    const value = await target.evaluate(scoped(expression));
    return { result: { type: value === null ? 'object' : typeof value, value: value ?? null } };
  } catch (error) {
    return { exceptionDetails: { text: error.message } };
  }
}

async function evaluateMainRaw(page, expression, fallbackFn) {
  if (!resourcePages.has(page)) {
    await ensureFingerprintPageBridge(page).catch(() => {});
  }
  const res = await evaluateScoped(page, null, expression);
  if (!res.exceptionDetails) return res.result.value;
  return fallbackFn ? page.evaluate(fallbackFn) : page.evaluate(expression);
}

// A headed window reports the real display, which the kernel leaves alone, while
// a headless one is built from switches. Remember what the headed page saw so
// later headless runs of this account present the same screen.
async function recordHostDisplay(page, profile, previous) {
  const entry = fingerprintBridges.get(page);
  const ctxState = entry?.mainFrameId ? entry.frameContexts.get(entry.mainFrameId) : null;
  if (!entry?.cdp || !ctxState) return;
  try {
    const reply = await entry.cdp.send('Runtime.evaluate', {
      contextId: ctxState.contextId,
      expression: HOST_DISPLAY_EXPRESSION,
      returnByValue: true,
    });
    const display = validateHostDisplay(reply?.result?.value);
    if (display && JSON.stringify(display) !== JSON.stringify(previous)) writeHostDisplay(profile, display);
  } catch { /* The page may navigate while its display is being read. */ }
}

function findMatchingCDPFrameId(node, frameURL, frameName) {
  if (!node?.frame) return null;
  for (const child of node.childFrames ?? []) {
    if (child?.frame?.url === frameURL && (!frameName || child.frame.name === frameName)) {
      return child.frame.id;
    }
    const nested = findMatchingCDPFrameId(child, frameURL, frameName);
    if (nested) return nested;
  }
  for (const child of node.childFrames ?? []) {
    if (child?.frame?.url === frameURL) return child.frame.id;
  }
  return null;
}

function releaseIsolatedWorld(worldId) {
  const iso = isolatedWorlds.get(worldId);
  if (!iso) return;
  isolatedWorlds.delete(worldId);
  if (iso.frame && frameIsolatedWorlds.get(iso.frame) === worldId) {
    frameIsolatedWorlds.delete(iso.frame);
  }
  if (iso.ownCdp) iso.cdp?.detach?.().catch(() => {});
}

async function createChildIsolatedWorld(page, frameIdNum) {
  const frame = frames.get(frameIdNum);
  if (!frame) return { executionContextId: frameIdNum };
  const cachedId = frameIsolatedWorlds.get(frame);
  if (cachedId && isolatedWorlds.has(cachedId)) {
    return { executionContextId: cachedId };
  }
  const opaqueWorld = `_iw${randomUUID().replaceAll('-', '')}`;
  const isMain = typeof page.mainFrame === 'function' && frame === page.mainFrame();
  let targetCdp = null;
  let ownCdp = false;
  let cdpFrameId = null;
  if (!isMain && typeof page.context === 'function') {
    try {
      const frameCdp = await page.context().newCDPSession(frame);
      try {
        const tree = await frameCdp.send('Page.getFrameTree');
        cdpFrameId = tree?.frameTree?.frame?.id || null;
        if (cdpFrameId) {
          targetCdp = frameCdp;
          ownCdp = true;
        } else {
          await frameCdp.detach().catch(() => {});
        }
      } catch {
        await frameCdp.detach().catch(() => {});
      }
    } catch { /* Same-origin child frame shares the page's CDP session. */ }
  }
  if (!targetCdp) {
    await ensureFingerprintPageBridge(page);
    const entry = fingerprintBridges.get(page);
    if (entry?.cdp) {
      targetCdp = entry.cdp;
      if (isMain) {
        cdpFrameId = entry.mainFrameId;
      } else {
        try {
          const tree = await entry.cdp.send('Page.getFrameTree');
          const url = typeof frame.url === 'function' ? frame.url() : '';
          const name = typeof frame.name === 'function' ? frame.name() : '';
          cdpFrameId = findMatchingCDPFrameId(tree?.frameTree, url, name);
        } catch { /* Frame may detach concurrently. */ }
      }
    }
  }
  if (targetCdp && cdpFrameId) {
    try {
      const iso = await targetCdp.send('Page.createIsolatedWorld', {
        frameId: cdpFrameId,
        worldName: opaqueWorld,
        grantUniveralAccess: true,
      });
      if (iso?.executionContextId) {
        const worldId = ++nextFrame;
        isolatedWorlds.set(worldId, { page, frame, cdp: targetCdp, ownCdp, contextId: iso.executionContextId });
        frameIsolatedWorlds.set(frame, worldId);
        return { executionContextId: worldId };
      }
    } catch {
      if (ownCdp) targetCdp.detach?.().catch(() => {});
    }
  } else if (ownCdp) {
    targetCdp.detach?.().catch(() => {});
  }
  return { executionContextId: frameIdNum };
}

async function evaluateIsolatedWorld(worldId, expression, awaitPromise) {
  const iso = isolatedWorlds.get(worldId);
  if (!iso) return null;
  try {
    const reply = await iso.cdp.send('Runtime.evaluate', {
      contextId: iso.contextId,
      expression,
      returnByValue: true,
      awaitPromise: Boolean(awaitPromise),
    });
    if (reply?.exceptionDetails) {
      const text = reply.exceptionDetails.exception?.description || reply.exceptionDetails.text || '页面脚本执行失败';
      return { exceptionDetails: { text: String(text).split('\n', 1)[0] } };
    }
    const result = reply?.result;
    if (!result || result.type === 'undefined') {
      return { result: { type: 'undefined', value: null } };
    }
    const val = result.value ?? null;
    return { result: { type: val === null ? 'object' : typeof val, value: val } };
  } catch (error) {
    if (isStaleCDPContextError(error)) {
      releaseIsolatedWorld(worldId);
    }
    return { exceptionDetails: { text: String(error?.message || error) } };
  }
}

function ensureFingerprintPageBridge(page) {
  if (!page || page.isClosed()) return Promise.resolve();
  const existing = fingerprintBridges.get(page);
  if (existing) return existing.promise;
  const entry = {
    cdp: null,
    promise: null,
    mainFrameId: null,
    frameContexts: new Map(),
    fpContexts: new Map(),
    contextWaiters: new Set(),
    hasSeenDefaultContext: false,
  };
  entry.promise = (async () => {
    const cdp = await page.context().newCDPSession(page);
    entry.cdp = cdp;
    if (page.isClosed()) {
      await cdp.detach().catch(() => {});
      return;
    }
    cdp.on('Runtime.bindingCalled', ev => {
      if (page.isClosed()) return;
      if (ev.name === fpBind) {
        emit({ event: 'binding', target: pageID(page), data: ev.payload });
        return;
      }
      if (ev.name === dlBind && downloadAssistant) {
        let req;
        try { req = JSON.parse(ev.payload); } catch { return; }
        const pid = pageID(page);
        void handleDownloadAssistantMessage(page, pid, req.message, cdp, ev.executionContextId).then(reply => {
          if (page.isClosed()) return;
          const expr = `globalThis[${JSON.stringify(dlResolve)}]?.(${JSON.stringify(req.id)},${JSON.stringify(reply)})`;
          return cdp.send('Runtime.evaluate', { contextId: ev.executionContextId, expression: expr }).catch(() => {});
        });
      }
    });
    const bindContext = (name, id) => cdp.send('Runtime.addBinding', { name, executionContextId: id }).catch(() => {});
    cdp.on('Runtime.executionContextCreated', ({ context: ec }) => {
      if (!ec?.id) return;
      if (ec.name === fpWorld) {
        if (ec.auxData?.frameId) entry.fpContexts.set(ec.auxData.frameId, ec.id);
        void bindContext(fpBind, ec.id);
      } else if (downloadAssistant && ec.name === dlWorld) {
        void bindContext(dlBind, ec.id);
      } else if (ec.auxData?.isDefault === true && (!ec.auxData.type || ec.auxData.type === 'default') && ec.auxData.frameId) {
        entry.hasSeenDefaultContext = true;
        entry.frameContexts.set(ec.auxData.frameId, { contextId: ec.id, runnerId: null, runnerPromise: null });
        if (ec.auxData.frameId === entry.mainFrameId) notifyMainContextWaiters(entry);
      }
    });
    cdp.on('Runtime.executionContextDestroyed', ({ executionContextId }) => {
      for (const [fid, state] of entry.frameContexts) {
        if (state.contextId === executionContextId) entry.frameContexts.delete(fid);
      }
      for (const [fid, ctxId] of entry.fpContexts) {
        if (ctxId === executionContextId) entry.fpContexts.delete(fid);
      }
    });
    cdp.on('Runtime.executionContextsCleared', () => {
      entry.frameContexts.clear();
      entry.fpContexts.clear();
    });
    await cdp.send('Page.enable');
    await cdp.send('Runtime.enable');
    if (launchMedia.length) await cdp.send('Emulation.setEmulatedMedia', { features: launchMedia });
    await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: fpForwarder, worldName: fpWorld });
    const injectDownloadAssistant = async frameId => {
      if (!downloadAssistant || page.isClosed() || resourcePages.has(page)) return;
      try {
        const targetFrameId = frameId || entry.mainFrameId || (await cdp.send('Page.getFrameTree'))?.frameTree?.frame?.id;
        if (!targetFrameId || page.isClosed()) return;
        entry.mainFrameId = targetFrameId;
        const dlIso = await cdp.send('Page.createIsolatedWorld', { frameId: targetFrameId, worldName: dlWorld });
        if (dlIso?.executionContextId) {
          await bindContext(dlBind, dlIso.executionContextId);
          await cdp.send('Runtime.evaluate', { contextId: dlIso.executionContextId, expression: downloadAssistant.script });
        }
      } catch { /* Frame may navigate or close concurrently. */ }
    };
    if (downloadAssistant) {
      await cdp.send('Page.addScriptToEvaluateOnNewDocument', { source: downloadAssistant.script, worldName: dlWorld });
      page.on('domcontentloaded', () => {
        if (!page.isClosed() && !resourcePages.has(page) && /^https?:\/\//i.test(page.url())) {
          void injectDownloadAssistant();
        }
      });
    }
    const tree = await cdp.send('Page.getFrameTree');
    entry.mainFrameId = tree?.frameTree?.frame?.id || null;
    if (entry.mainFrameId && entry.frameContexts.has(entry.mainFrameId)) {
      notifyMainContextWaiters(entry);
    }
    const attachFrame = async (node, isMain) => {
      const frameId = node?.frame?.id;
      if (frameId) {
        try {
          const iso = await cdp.send('Page.createIsolatedWorld', { frameId, worldName: fpWorld });
          if (iso?.executionContextId) {
            entry.fpContexts.set(frameId, iso.executionContextId);
            await bindContext(fpBind, iso.executionContextId);
            await cdp.send('Runtime.evaluate', { contextId: iso.executionContextId, expression: fpForwarder });
          }
        } catch { /* Frame may navigate or close concurrently. */ }
        if (isMain && downloadAssistant) {
          await injectDownloadAssistant(frameId);
        }
      }
      for (const child of node?.childFrames ?? []) await attachFrame(child, false);
    };
    await attachFrame(tree?.frameTree, true);
  })().catch(error => {
    if (fingerprintBridges.get(page) === entry) fingerprintBridges.delete(page);
    if (!page.isClosed()) throw error;
  });
  fingerprintBridges.set(page, entry);
  return entry.promise;
}

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
    const isPrivate = privateContexts.has(id);
    const isResource = resourcePages.has(page);
    const isProgrammatic = programmaticCloses.has(page);
    pages.delete(id);
    downloadTemuTabs.delete(id);
    const fpBridge = fingerprintBridges.get(page);
    if (fpBridge) {
      fingerprintBridges.delete(page);
      fpBridge.frameContexts?.clear();
      fpBridge.fpContexts?.clear();
      notifyMainContextWaiters(fpBridge);
      fpBridge.cdp?.detach?.().catch(() => {});
    }
    mousePositions.delete(page);
    for (const [worldId, iso] of [...isolatedWorlds]) {
      if (iso.page === page) releaseIsolatedWorld(worldId);
    }
    for (const [key, frame] of frames) if (frame.page() === page) frames.delete(key);
    networkOptions.delete(id);
    for (const [key,value] of responseBodies) if (value.target === id) responseBodies.delete(key);
    for (const [key,value] of routes) if (value.target === id) { clearTimeout(value.timer); value.route.abort().catch(() => {}); routes.delete(key); }
    for (const [client,target] of activeRequests) if (target === id) { activeRequests.delete(client); client.dispose().catch(() => {}); }
    const privateContext = privateContexts.get(id);
    privateContexts.delete(id);
    if (privateContext) {
      const done = privateContext.close().catch(() => {}).finally(() => {
        if (privateClosing.get(id) === done) privateClosing.delete(id);
      });
      privateClosing.set(id, done);
    }
    emit({ event: 'closed', target: id });
    if (!isPrivate && !isResource && !isProgrammatic) {
      scheduleEmptyWindowExit();
    }
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
  if (!page || page.isClosed()) throw new Error('账号浏览器页面已关闭');
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
  const trimmed = String(raw ?? '').trim();
  if (!trimmed) return undefined;
  const url = new URL(trimmed);
  const proxy = { server: `${url.protocol}//${url.host}` };
  if (url.protocol === 'socks5:' && (url.username || url.password)) throw new Error('账号浏览器不支持带认证的 SOCKS5 代理，请使用 HTTP 代理');
  if (url.username) proxy.username = decodeURIComponent(url.username);
  if (url.password) proxy.password = decodeURIComponent(url.password);
  return proxy;
}

function cancelEmptyWindowExit() {
  if (!emptyWindowTimer) return;
  clearTimeout(emptyWindowTimer);
  emptyWindowTimer = null;
}

function visibleWindowEmpty() {
  if (closing || launchHeadless || !context) return false;
  if (privateContexts.size > 0 || pendingResourcePages > 0) return false;
  return context.pages().every(page => page.isClosed());
}

function scheduleEmptyWindowExit() {
  if (!visibleWindowEmpty()) return;
  cancelEmptyWindowExit();
  // On macOS, closing the last Chromium window leaves the app alive with zero pages.
  emptyWindowTimer = setTimeout(() => {
    emptyWindowTimer = null;
    if (!visibleWindowEmpty()) return;
    close().finally(() => process.exit(0));
  }, 250);
  emptyWindowTimer.unref?.();
}

async function start(params) {
  if (context) throw new Error('账号浏览器已经启动');
  const rawProxy = String(params.proxy ?? '').trim();
  if (params.directNetwork && rawProxy) {
    throw new Error('浏览器直连模式不能同时指定代理');
  }
  fs.mkdirSync(params.profile, { recursive: true, mode: 0o700 });
  downloadAssistant = loadDownloadAssistant(params.addons);
  launchProxy = rawProxy;
  launchHeadless = Boolean(params.headless);
  const startProxy = proxyOptions(rawProxy);
  const display = readHostDisplay(params.profile);
  launchMedia = emulatedMedia(display, launchHeadless);
  // Service workers stay allowed: Playwright's "block" replaces
  // navigator.serviceWorker.register with a function that says so in its source.
  contextOptions = {
    viewport: null,
    ignoreHTTPSErrors: Boolean(params.ignoreHTTPSErrors),
    ...(startProxy ? { proxy: startProxy } : {}),
  };
  requestOptions = {proxy:startProxy, ignoreHTTPSErrors:contextOptions.ignoreHTTPSErrors};
  const { chromium } = await import('playwright-core');
  const identity = ensureFingerprintIdentity(params.profile, {
    hostOS,
    collisionRoots: params.collisionRoots,
  });
  context = await chromium.launchPersistentContext(params.profile, {
    ...contextOptions,
    executablePath: params.executable,
    headless: Boolean(params.headless),
    chromiumSandbox: true,
    acceptDownloads: true,
    // The default sRGB profile flattens wide-gamut displays, unlike a real window.
    ignoreDefaultArgs: ['--enable-automation', '--hide-scrollbars', '--force-color-profile=srgb'],
    args: buildFingerprintArgs(identity, {
      directNetwork: Boolean(params.directNetwork),
      proxy: Boolean(startProxy),
      headless: Boolean(params.headless),
      timezone: params.timezone,
      lang: params.lang,
      display,
    }),
  });
  // fingerprint-chromium adds this property to every page; stock Chrome has none.
  await context.addInitScript({ content: 'delete Element.prototype.fakeShadowRoot;'});
  await context.addInitScript({ content: scopeBootstrap });
  for (const current of context.pages()) {
    await ensureFingerprintPageBridge(current);
    await evaluateScoped(current, null, 'true');
  }
  if (!params.headless) {
    for (const current of context.pages()) await recordHostDisplay(current, params.profile, display);
  }
  context.setDefaultTimeout(25000);
  context.on('page', page => {
    cancelEmptyWindowExit();
    pageID(page);
    // Pages the site opens itself get the emulated display features too.
    if (launchMedia.length) void ensureFingerprintPageBridge(page).catch(() => {});
    if (!downloadAssistant) return;
    if (pendingResourcePages === 0) {
      void ensureFingerprintPageBridge(page).catch(() => {});
    } else {
      page.on('framenavigated', frame => {
        if (frame === page.mainFrame() && !resourcePages.has(page) && /^https?:\/\//i.test(frame.url())) {
          void ensureFingerprintPageBridge(page).catch(() => {});
        }
      });
    }
  });
  context.on('close', () => { if (!closing) process.exit(0); });
  context.browser?.()?.on?.('disconnected', () => { if (!closing) process.exit(0); });
  for (const page of context.pages()) pageID(page);
  const page = context.pages()[0] || await context.newPage();
  await ensureFingerprintPageBridge(page);
  return { targetId: pageID(page) };
}

function close() {
  cancelEmptyWindowExit();
  closing ??= Promise.resolve(startup).catch(() => {}).then(async () => {
    for (const privateContext of new Set(privateContexts.values())) {
      await privateContext.close().catch(() => {});
    }
    privateContexts.clear();
    await Promise.all(privateClosing.values());
    await context?.close();
  });
  return closing;
}

function ensureBinding(targetContext, bindingName, applyExisting = true, url = '') {
  let registered = contextBindings.get(targetContext);
  if (!registered) {
    registered = new Map();
    contextBindings.set(targetContext, registered);
  }
  if (!registered.has(bindingName)) {
    registered.set(bindingName, installBinding(targetContext, bindingName, applyExisting, url));
  }
  return registered.get(bindingName);
}

async function installBinding(targetContext, bindingName, applyExisting = true, url = '') {
  const name = JSON.stringify(bindingName);
  const invoke = `window[${name}] = data => ${scopePipeKey}(data)`;
  await targetContext.addInitScript({ content: scopedInit(invoke, url) });
  if (!applyExisting) return;
  for (const current of targetContext.pages()) {
    if (current.isClosed()) continue;
    await ensureFingerprintPageBridge(current);
    const res = await evaluateScoped(current, null, invoke);
    if (res.exceptionDetails) throw new Error(res.exceptionDetails.text || '页面绑定失败');
  }
}

async function closeTargetPage(id, targetPage) {
  const key = String(id);
  programmaticCloses.add(targetPage);
  await targetPage.close();
  const pending = privateClosing.get(key);
  if (pending) { await pending; return; }
  const privateContext = privateContexts.get(key);
  if (privateContext) {
    privateContexts.delete(key);
    await privateContext.close().catch(() => {});
  }
}

function sleepSignal(ms, signal) {
  if (!(ms > 0)) return Promise.resolve();
  if (signal?.aborted) return Promise.reject(signal.reason || new Error('操作已取消'));
  return new Promise((resolve, reject) => {
    const onAbort = () => { clearTimeout(timer); reject(signal.reason || new Error('操作已取消')); };
    const timer = setTimeout(() => { signal?.removeEventListener('abort', onAbort); resolve(); }, ms);
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

async function mouseClick(page, params, signal) {
  const x = Number(params.x);
  const y = Number(params.y);
  const stepMs = Number(params.stepMs) || 0;
  const prePressMs = Number(params.prePressMs) || 0;
  const holdMs = Number(params.holdMs) || 0;
  const points = Array.isArray(params.points) && params.points.length ? params.points : [[x, y]];
  for (let i = 0; i < points.length; i++) {
    signal.throwIfAborted();
    const px = Number(points[i][0]);
    const py = Number(points[i][1]);
    const pos = mousePositions.get(page);
    if (!pos || pos.x !== px || pos.y !== py) {
      await page.mouse.move(px, py);
      mousePositions.set(page, { x: px, y: py });
    }
    if (stepMs > 0 && i < points.length - 1) await sleepSignal(stepMs, signal);
  }
  const pos = mousePositions.get(page);
  if (!pos || pos.x !== x || pos.y !== y) {
    signal.throwIfAborted();
    await page.mouse.move(x, y);
    mousePositions.set(page, { x, y });
  }
  if (prePressMs > 0) await sleepSignal(prePressMs, signal);
  signal.throwIfAborted();
  await page.mouse.down({ button: 'left', clickCount: 1 });
  if (holdMs > 0) await sleepSignal(holdMs, signal);
  signal.throwIfAborted();
  await page.mouse.up({ button: 'left', clickCount: 1 });
}

async function insertText(page, text, signal) {
  if (text === '') { await page.keyboard.press('Backspace'); return; }
  const normalized = text.includes('\r') ? text.replace(/\r\n/g, '\n').replace(/\r/g, '\n') : text;
  if (!normalized.includes('\n')) {
    await page.keyboard.insertText(normalized); return;
  }
  // Single CDP probe: 0 = input/textarea, 1 = ProseMirror/contenteditable, 2 = Slate.js.
  const editorKind = await evaluateMainRaw(page, '(()=>{const a=document.activeElement;if(!a?.isContentEditable)return 0;return a.closest?.("[data-slate-editor=\\"true\\"]")?2:1;})()');
  if (!editorKind) {
    await page.keyboard.insertText(normalized); return;
  }
  signal.throwIfAborted();
  // Slate.js corrupts multiline document.execCommand('insertText'); insert full lines with Shift+Enter.
  if (editorKind === 2) {
    const lines = normalized.split('\n');
    for (let i = 0; i < lines.length; i++) {
      signal.throwIfAborted();
      if (i === 0 && lines[0].length === 0) await page.keyboard.press('Backspace');
      if (i > 0) await page.keyboard.press('Shift+Enter');
      if (lines[i].length > 0) await page.keyboard.insertText(lines[i]);
    }
    return;
  }
  const inserted = await evaluateMainRaw(page, `document.execCommand('insertText', false, ${JSON.stringify(normalized)})`);
  if (!inserted) throw new Error('富文本输入失败');
}

async function screenshot(page, params) {
  const type = params.format === 'jpeg' ? 'jpeg' : 'png';
  const options = { type, timeout: 15000, animations: 'allow' };
  if (type === 'jpeg') options.quality = params.quality || 80;
  if (params.clip) {
    const { x, y, width, height } = params.clip;
    options.clip = { x, y, width, height };
    if (params.clip.scale === 1) options.scale = 'css';
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
    if (page.isClosed()) throw new Error('浏览器页面已关闭');
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

async function gotoDownloadPage(page, goto, signal) {
  const previous = page.url().split('#')[0];
  const documents = new Set();
  let noDocument = false;
  const onDownload = () => { noDocument = true; };
  const onResponse = response => {
    if (!response.request().isNavigationRequest() || response.frame() !== page.mainFrame()) return;
    if ([204, 205].includes(response.status()) || /^attachment\b/i.test(response.headers()['content-disposition'] || '')) {
      noDocument = true;
    } else if (response.status() < 300 || response.status() >= 400) {
      documents.add(response.url().split('#')[0]);
    }
  };
  page.on('download', onDownload);
  page.on('response', onResponse);
  try {
    for (let attempt = 0; ; attempt++) {
      try {
        return { response: await goto() };
      } catch (error) {
        signal.throwIfAborted();
        const reason = error.message.split('\n', 1)[0].replace(/^page\.goto: /, '');
        const interrupted = /^(?:NS_BINDING_ABORTED\b|net::ERR_ABORTED\b|Navigation to ".*" is interrupted by another navigation to ")/.test(reason);
        if (!interrupted || page.isClosed() || noDocument) throw error;
        try {
          await page.waitForURL(url => /^https?:$/.test(url.protocol) && url.href.split('#')[0] !== previous,
            { waitUntil: 'commit', timeout: 1500 });
        } catch (waitError) {
          signal.throwIfAborted();
          if (waitError.name !== 'TimeoutError' || noDocument || page.isClosed()) throw error;
          if (attempt === 1) throw new Error('网页连续两次打开中断，请检查网络连接后重试', {cause:error});
          continue;
        }
        signal.throwIfAborted();
        const replacementURL = await evaluateMainRaw(page, 'document.documentURI', () => document.documentURI);
        if (noDocument || !/^https?:\/\//i.test(replacementURL) || !documents.has(replacementURL.split('#')[0]) ||
            replacementURL.split('#')[0] === previous) throw error;
        return { replacementURL };
      }
    }
  } finally {
    page.off('response', onResponse);
    page.off('download', onDownload);
  }
}

// These names preserve tulai's existing page API. Every operation uses Playwright's
// private CDP pipe to the managed fingerprint-chromium browser.
async function dispatch(method, params, target, signal) {
  signal.throwIfAborted();
  switch (method) {
    case 'start': return startup ??= start(params);
    case 'ping':
      if (visibleWindowEmpty()) {
        scheduleEmptyWindowExit();
        return { alive: false };
      }
      return { alive: !closing };
    case 'Target.setDiscoverTargets': return {};
    case 'Target.getTargets':
      return { targetInfos: [...pages].filter(([, page]) => !page.isClosed()).map(([id, page]) => ({ targetId: id, type: 'page', url: page.url() })) };
    case 'Target.attachToTarget': pageFor(params.targetId); return { sessionId: params.targetId };
    case 'Target.createTarget': {
      cancelEmptyWindowExit();
      let isolatedContext;
      let page;
      if (params.isolated) {
        const requestedProxy = String(params.proxy ?? '').trim();
        const isolatedProxy = proxyOptions(requestedProxy || launchProxy);
        const isolatedOptions = { ...contextOptions };
        if (isolatedProxy) isolatedOptions.proxy = isolatedProxy;
        else delete isolatedOptions.proxy;
        isolatedContext = await context.browser().newContext(isolatedOptions);
        try {
          isolatedContext.setDefaultTimeout?.(25000);
          await isolatedContext.addInitScript({ content: 'delete Element.prototype.fakeShadowRoot;'});
          await isolatedContext.addInitScript({ content: scopeBootstrap });
          const targetURL = pageScriptURL(params.url);
          for (const name of bindings) await ensureBinding(isolatedContext, name, false, targetURL);
          page = await isolatedContext.newPage();
        } catch (error) {
          await isolatedContext.close().catch(() => {});
          throw error;
        }
      } else {
        page = await context.newPage();
      }
      const id = pageID(page);
      if (isolatedContext) privateContexts.set(id, isolatedContext);
      try {
        await ensureFingerprintPageBridge(page);
        if (params.url && params.url !== 'about:blank') {
          await page.goto(params.url, { waitUntil: 'domcontentloaded', timeout: 60000 });
        }
      } catch (error) {
        await closeTargetPage(id, page).catch(() => {});
        throw error;
      }
      return { targetId: id };
    }
    case 'Target.closeTarget': await closeTargetPage(params.targetId, pageFor(params.targetId)); return { success: true };
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
      if (params.contextId && isolatedWorlds.has(params.contextId)) {
        return evaluateIsolatedWorld(params.contextId, params.expression, params.awaitPromise);
      }
      const frame = params.contextId ? frames.get(params.contextId) : page.mainFrame();
      if (!frame) throw new Error('账号浏览器页面框架已关闭');
      return evaluateScoped(page, frame, params.expression);
    }
    case 'Runtime.addBinding': {
      const owner = page.context();
      if (owner === context) bindings.add(params.name);
      const url = pageScriptURL(page.url());
      await ensureBinding(owner, params.name, true, url);
      if (owner === context) {
        for (const privateContext of new Set(privateContexts.values())) {
          await ensureBinding(privateContext, params.name, true, url);
        }
      }
      return {};
    }
    case 'Page.addScriptToEvaluateOnNewDocument':
      await page.addInitScript({ content: scopedInit(params.source, pageScriptURL(page.url())) }); return { identifier: 'tulai' };
    case 'Page.navigate': {
      const deadline = Date.now() + 60000;
      const goto = () => page.goto(params.url, {waitUntil: 'commit', timeout: Math.max(1, deadline - Date.now())});
      const { response, replacementURL } = params.recoverInterrupted
        ? await gotoDownloadPage(page, goto, signal) : { response: await goto() };
      if (replacementURL) return { replacementURL };
      const contentType = response?.headers()['content-type']?.toLowerCase();
      if (params.wait !== 'commit' && response && !contentType?.startsWith('image/')) {
        signal.throwIfAborted();
        await page.waitForLoadState('domcontentloaded', {timeout: Math.max(1, deadline - Date.now())});
      }
      return {};
    }
    case 'Page.reload': await page.reload({ waitUntil: 'domcontentloaded' }); return {};
    case 'Page.close': await closeTargetPage(target, page); return {};
    case 'Page.bringToFront': await page.bringToFront(); return {};
    case 'Page.getFrameTree': return { frameTree: frameTree(page.mainFrame()) };
    case 'Page.createIsolatedWorld': return createChildIsolatedWorld(page, Number(params.frameId));
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
      const openP = evaluateScoped(page, null, params.openJS).then(res => {
        if (res?.exceptionDetails) throw new Error(res.exceptionDetails.text || '页面脚本执行失败');
        return res;
      });
      // Race filechooser against openJS so direct upload buttons don't wait out menu polling.
      const first = await Promise.race([
        chooser.then(fc => ({ kind: 'chooser', fc })),
        openP.then(() => ({ kind: 'open' })),
      ]);
      let opened;
      if (first.kind === 'chooser') {
        opened = first.fc;
        evaluateScoped(page, null, 'window.__tulaiChooserOpened = true').catch(() => {});
        openP.catch(() => {});
      } else {
        opened = await chooser;
      }
      signal.throwIfAborted();
      if (Array.isArray(params.files) && params.files.length > 1 && typeof opened.isMultiple === 'function' && !opened.isMultiple()) {
        await opened.setFiles([]).catch(() => {});
        throw new Error('文件选择器不支持多选');
      }
      await opened.setFiles(params.files);
      return {};
    }
    case 'resource': {
      let resourcePage;
      let closedByAbort = false;
      const abort = () => {
        closedByAbort = true;
        if (resourcePage) {
          programmaticCloses.add(resourcePage);
          resourcePage.close().catch(() => {});
        }
      };
      signal.addEventListener('abort', abort, {once:true});
      pendingResourcePages++;
      try {
        signal.throwIfAborted();
        const referer = page.url();
        resourcePage = await page.context().newPage();
        resourcePages.add(resourcePage);
        if (closedByAbort || signal.aborted) signal.throwIfAborted();
        const response = await resourcePage.goto(params.url, {waitUntil:'commit',timeout:45000,referer});
        if (!response) throw new Error('浏览器未返回资源');
        if (!response.ok()) throw new Error(`HTTP ${response.status()}`);
        if (Number(response.headers()['content-length']) > maxBytes) throw new Error('资源超过 64 MiB 上限');
        const body = await response.body();
        if (body.length > maxBytes) throw new Error('资源超过 64 MiB 上限');
        return { data: body.toString('base64') };
      } finally {
        pendingResourcePages--;
        signal.removeEventListener('abort', abort);
        if (resourcePage) {
          programmaticCloses.add(resourcePage);
          await resourcePage.close().catch(() => {});
        }
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
    case 'Input.mouseClick': await mouseClick(page, params, signal); return {};
    case 'Input.dispatchMouseEvent': {
      const pos = mousePositions.get(page);
      const moved = !pos || pos.x !== params.x || pos.y !== params.y;
      if (params.type === 'mouseMoved' || moved) {
        await page.mouse.move(params.x, params.y);
        mousePositions.set(page, { x: params.x, y: params.y });
        signal.throwIfAborted();
      }
      const options = { button: params.button && params.button !== 'none' ? params.button : 'left', clickCount: params.clickCount || 1 };
      if (params.type === 'mousePressed') await page.mouse.down(options);
      else if (params.type === 'mouseReleased') await page.mouse.up(options);
      else if (params.type === 'mouseWheel') await page.mouse.wheel(params.deltaX || 0, params.deltaY || 0);
      return {};
    }
    default: throw new Error(`不支持的浏览器操作：${method}`);
  }
}

const input = readline.createInterface({ input: process.stdin });
input.on('line', line => {
  let request;
  try { request = JSON.parse(line); } catch { return; }
  if (request.method === 'cancel') { controllers.get(request.id)?.abort(); return; }
  if (request.method === 'Browser.close') {
    close()
      .then(() => emit({ id: request.id, result: {} }), error => emit({ id: request.id, error: String(error.message || error) }))
      .finally(() => process.exit(0));
    return;
  }
  const controller = new AbortController();
  controllers.set(request.id, controller);
  const timer = setTimeout(() => controller.abort(), Math.max(1, request.timeout || 60000));
  const aborted = new Promise((_, reject) => controller.signal.addEventListener('abort', () => {
    // Playwright promises alone are not cancellable; close this page to stop late input or script effects.
    if ((request.method === 'Runtime.evaluate' && !request.params?.readOnly) ||
        ['Page.navigate','Page.reload','upload'].includes(request.method) || request.method.startsWith('Input.')) {
      const targetPage = pages.get(String(request.target));
      if (targetPage) {
        programmaticCloses.add(targetPage);
        targetPage.close().catch(() => {});
      }
    }
    reject(new Error('操作已取消或超时'));
  }, { once: true }));
  Promise.race([dispatch(request.method, request.params || {}, request.target, controller.signal), aborted])
    .then(result => emit({ id: request.id, result }), error => emit({ id: request.id, error: String(error.message || error) }))
    .finally(() => {
      clearTimeout(timer);
      controllers.delete(request.id);
    });
});
input.on('close', () => close().finally(() => process.exit(0)));
process.on('SIGTERM', () => close().finally(() => process.exit(0)));
