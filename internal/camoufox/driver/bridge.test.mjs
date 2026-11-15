import assert from 'node:assert/strict';
import {execFile, spawn} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {promisify} from 'node:util';

test('official fetcher matches the current system and architecture', async () => {
  const assets = [
    'camoufox-158.0.1-beta.41-mac.arm64.zip', 'camoufox-158.0.1-beta.41-mac.x86_64.zip',
    'camoufox-158.0.1-beta.41-win.x86_64.zip', 'camoufox-158.0.1-beta.41-lin.x86_64.zip',
    'camoufox-158.0.1-beta.41-lin.arm64.zip',
  ];
  const script = `
    import os from 'node:os';
    import {syncBuiltinESMExports} from 'node:module';
    Object.defineProperty(process, 'platform', {value:process.env.TEST_PLATFORM});
    os.arch = () => process.env.TEST_ARCH;
    syncBuiltinESMExports();
    const {CamoufoxFetcher} = await import('camoufox/dist/pkgman.js');
    const pattern = new CamoufoxFetcher().pattern;
    console.log(JSON.stringify(JSON.parse(process.env.TEST_ASSETS).filter(asset => pattern.test(asset))));
  `;
  for (const [platform, arch, index] of [
    ['darwin','arm64',0], ['darwin','x64',1], ['win32','x64',2], ['linux','x64',3], ['linux','arm64',4],
  ]) {
    const env = {...process.env, TEST_PLATFORM:platform, TEST_ARCH:arch, TEST_ASSETS:JSON.stringify(assets)};
    const {stdout} = await promisify(execFile)(process.execPath, ['--input-type=module', '-e', script],
      {cwd:new URL('.', import.meta.url), env});
    assert.deepEqual(JSON.parse(stdout), [assets[index]]);
  }
});

test('installer isolates each platform cache and restores the real environment', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-platform-'));
  t.after(() => fs.rmSync(root, {recursive:true, force:true}));
  for (const file of ['bridge.mjs', 'identity.mjs', 'download-watch.mjs', 'windows-compat.mjs']) fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  const pkg = path.join(root, 'node_modules', 'camoufox');
  fs.mkdirSync(path.join(pkg, 'dist'), {recursive:true});
  fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({type:'module', exports:{'.':'./index.js','./dist/utils.js':'./dist/utils.js','./dist/pkgman.js':'./dist/pkgman.js'}}));
  fs.writeFileSync(path.join(pkg, 'index.js'), `
    const key = {darwin:'HOME',win32:'LOCALAPPDATA',linux:'XDG_CACHE_HOME'}[process.platform];
    const cacheAtImport = process.env[key];
    export class CamoufoxFetcher {}
    export const Camoufox = () => {};
    export const installedVerStr = () => '';
    export const launchOptions = async options => ({executablePath:JSON.stringify({
      cacheAtImport, restored:process.env[key], os:options.os, arch:process.arch,
      override:process.env.CAMOUFOX_EXECUTABLE_PATH ?? null
    })});
  `);
  fs.writeFileSync(path.join(pkg, 'dist', 'utils.js'), 'export const utilsDeps = {hostGpu:()=>{}};');
  fs.writeFileSync(path.join(pkg, 'dist', 'pkgman.js'), 'export const webdl = async () => {};');
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, {recursive:true});
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({type:'module', exports:'./index.js'}));
  fs.writeFileSync(path.join(playwright, 'index.js'), 'export const request = {};');
  fs.writeFileSync(path.join(root, 'run.mjs'), `
    Object.defineProperty(process, 'platform', {value:process.env.TEST_PLATFORM});
    Object.defineProperty(process, 'arch', {value:process.env.TEST_ARCH});
    await import('./bridge.mjs');
  `);

  for (const [platform, arch, hostOS, key] of [
    ['darwin','arm64','macos','HOME'], ['darwin','x64','macos','HOME'],
    ['win32','x64','windows','LOCALAPPDATA'],
    ['linux','x64','linux','XDG_CACHE_HOME'], ['linux','arm64','linux','XDG_CACHE_HOME'],
  ]) {
    const cache = path.join(root, `${platform}-${arch}`);
    const original = path.join(root, `original-${key}`);
    const env = {...process.env, [key]:original, TULAI_CAMOUFOX_CACHE:cache,
      CAMOUFOX_EXECUTABLE_PATH:'wrong-browser', TEST_PLATFORM:platform, TEST_ARCH:arch};
    const {stdout} = await promisify(execFile)(process.execPath, [path.join(root, 'run.mjs'), 'install'], {env});
    const actual = JSON.parse(JSON.parse(stdout).executable);
    assert.deepEqual(actual, {cacheAtImport:cache, restored:original, os:hostOS, arch, override:null});
  }
});

test('installer recovers from GitHub API failures with verified pinned releases', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-release-fallback-'));
  t.after(() => fs.rmSync(root, {recursive:true, force:true}));
  for (const file of ['bridge.mjs', 'identity.mjs', 'download-watch.mjs', 'windows-compat.mjs']) fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  const realDist = fileURLToPath(new URL('./node_modules/camoufox/dist/', import.meta.url));
  const real = file => pathToFileURL(path.join(realDist, file)).href;
  const pin = JSON.parse(fs.readFileSync(path.join(realDist, 'data-files', 'browser-pin.json'), 'utf8'));
  assert.deepEqual(pin, {tag:'v158.0.1-beta.41', repo:'daijro/camoufox', repo_name:'Official', version:'158.0.1', build:'beta.41'});
  const pkg = path.join(root, 'node_modules', 'camoufox');
  fs.mkdirSync(path.join(pkg, 'dist'), {recursive:true});
  fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({type:'module', exports:{
    '.':'./index.js', './dist/utils.js':'./dist/utils.js', './dist/pkgman.js':'./dist/pkgman.js',
    './dist/browser-pin.js':'./dist/browser-pin.js', './dist/multiversion.js':'./dist/multiversion.js',
  }}));
  fs.writeFileSync(path.join(pkg, 'index.js'), `
    import {CamoufoxFetcher} from ${JSON.stringify(real('pkgman.js'))};
    export {CamoufoxFetcher};
    export const Camoufox = () => {};
    export const installedVerStr = () => '';
    export const launchOptions = async () => {
      const fetcher = await new CamoufoxFetcher().init();
      return {executablePath:JSON.stringify({url:fetcher.url, sha:fetcher.installedSha256})};
    };
  `);
  fs.writeFileSync(path.join(pkg, 'dist', 'utils.js'), 'export const utilsDeps = {hostGpu:()=>{}};');
  for (const file of ['pkgman.js', 'browser-pin.js', 'multiversion.js']) {
    fs.writeFileSync(path.join(pkg, 'dist', file), `export * from ${JSON.stringify(real(file))};`);
  }
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, {recursive:true});
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({type:'module', exports:'./index.js'}));
  fs.writeFileSync(path.join(playwright, 'index.js'), 'export const request = {};');
  fs.writeFileSync(path.join(root, 'run.mjs'), `
    import os from 'node:os';
    import {syncBuiltinESMExports} from 'node:module';
    Object.defineProperty(process, 'platform', {value:process.env.TEST_PLATFORM});
    Object.defineProperty(process, 'arch', {value:process.env.TEST_ARCH});
    os.arch = () => process.env.TEST_ARCH;
    syncBuiltinESMExports();
    globalThis.fetch = async url => {
      url = String(url);
      if (url.startsWith('https://api.github.com/')) {
        if (process.env.TEST_STATUS === 'network') throw new TypeError('fetch failed');
        if (process.env.TEST_STATUS === '200') return new Response(JSON.stringify([{assets:[{
          name:'camoufox-158.0.1-beta.41-win.x86_64.zip',
          browser_download_url:'https://example.invalid/from-api', digest:'sha256:'+'a'.repeat(64)
        }]}]), {status:200});
        return new Response('', {status:Number(process.env.TEST_STATUS), statusText:'rate limit exceeded'});
      }
      if (url === 'https://github.com/daijro/camoufox/releases/download/v158.0.1-beta.41/manifest.json') {
        return new Response('{"interface":1}', {status:200});
      }
      throw new Error('unexpected request: '+url);
    };
    if (process.env.TEST_EXPLICIT) {
      const {saveConfig} = await import(${JSON.stringify(real('multiversion.js'))});
      saveConfig({channel:'official/stable'});
    }
    await import('./bridge.mjs');
  `);
  const run = (platform, arch, status = '403', explicit = false) => {
    const cache = path.join(root, `${platform}-${arch}-${status}-${explicit}`);
    const env = {...process.env, HOME:cache, LOCALAPPDATA:cache, XDG_CACHE_HOME:cache, TULAI_CAMOUFOX_CACHE:cache,
      GITHUB_TOKEN:'', TEST_PLATFORM:platform, TEST_ARCH:arch, TEST_STATUS:status, TEST_EXPLICIT:explicit ? '1' : ''};
    return promisify(execFile)(process.execPath, [path.join(root, 'run.mjs'), 'install'], {env});
  };
  for (const [platform, arch, suffix, sha] of [
    ['darwin','arm64','mac.arm64','9038f86d666c9780b6fff199d488737b8596c01f24e3e517845a0ecfb0cf8add'],
    ['darwin','x64','mac.x86_64','1afd6caa1e82caa41a4f56d8519f673e36b0835700b1c9c904e78a84c5711f3a'],
    ['win32','x64','win.x86_64','2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f'],
    ['linux','x64','lin.x86_64','3f2eb1d0451fa58e45e43e55f5e6181565069791ae60c0574f0ceb1f01341c33'],
    ['linux','arm64','lin.arm64','60e25f6eb95e13d9ec655bc02913d51057e669cb31542e6ace9d2190ab5380af'],
  ]) {
    const {stdout} = await run(platform, arch);
    assert.deepEqual(JSON.parse(JSON.parse(stdout).executable), {
      url:`https://github.com/daijro/camoufox/releases/download/v158.0.1-beta.41/camoufox-158.0.1-beta.41-${suffix}.zip`, sha,
    });
  }
  const {verifySha256} = await import('camoufox/dist/pkgman.js');
  assert.throws(() => verifySha256(Buffer.from('tampered'), '2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f'));
  // A hijacked API route (404) and an unreachable one (network failure) fall
  // back to the same verified pinned release instead of surfacing the
  // launcher's fallback-repo 404 for camoufox/camoufox.
  const {stdout: notFound} = await run('win32', 'x64', '404');
  assert.deepEqual(JSON.parse(JSON.parse(notFound).executable), {
    url:'https://github.com/daijro/camoufox/releases/download/v158.0.1-beta.41/camoufox-158.0.1-beta.41-win.x86_64.zip',
    sha:'2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f',
  });
  const {stdout: offline} = await run('win32', 'x64', 'network');
  assert.deepEqual(JSON.parse(JSON.parse(offline).executable), {
    url:'https://github.com/daijro/camoufox/releases/download/v158.0.1-beta.41/camoufox-158.0.1-beta.41-win.x86_64.zip',
    sha:'2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f',
  });
  const {stdout} = await run('win32', 'x64', '200');
  assert.deepEqual(JSON.parse(JSON.parse(stdout).executable), {
    url:'https://example.invalid/from-api',
    // The GitHub API publishes no digests; the pinned one is attached anyway.
    sha:'2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f',
  });
  await assert.rejects(run('win32', 'x64', '500'), error => error.stderr.includes('500 Server Error'));
  await assert.rejects(run('win32', 'x64', '403', true), error => error.stderr.includes('403 Client Error'));
});

test('installer serves the pinned kernel from a configured mirror without touching GitHub', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-mirror-'));
  t.after(() => fs.rmSync(root, {recursive:true, force:true}));
  for (const file of ['bridge.mjs', 'identity.mjs', 'download-watch.mjs', 'windows-compat.mjs']) fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  const realDist = fileURLToPath(new URL('./node_modules/camoufox/dist/', import.meta.url));
  const real = file => pathToFileURL(path.join(realDist, file)).href;
  const pkg = path.join(root, 'node_modules', 'camoufox');
  fs.mkdirSync(path.join(pkg, 'dist'), {recursive:true});
  fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({type:'module', exports:{
    '.':'./index.js', './dist/utils.js':'./dist/utils.js', './dist/pkgman.js':'./dist/pkgman.js',
    './dist/browser-pin.js':'./dist/browser-pin.js', './dist/multiversion.js':'./dist/multiversion.js',
  }}));
  fs.writeFileSync(path.join(pkg, 'index.js'), `
    import {CamoufoxFetcher} from ${JSON.stringify(real('pkgman.js'))};
    export {CamoufoxFetcher};
    export const Camoufox = () => {};
    export const installedVerStr = () => '';
    export const launchOptions = async () => {
      const fetcher = await new CamoufoxFetcher().init();
      return {executablePath:JSON.stringify({url:fetcher.url, sha:fetcher.installedSha256})};
    };
  `);
  fs.writeFileSync(path.join(pkg, 'dist', 'utils.js'), 'export const utilsDeps = {hostGpu:()=>{}};');
  for (const file of ['pkgman.js', 'browser-pin.js', 'multiversion.js']) {
    fs.writeFileSync(path.join(pkg, 'dist', file), `export * from ${JSON.stringify(real(file))};`);
  }
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, {recursive:true});
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({type:'module', exports:'./index.js'}));
  fs.writeFileSync(path.join(playwright, 'index.js'), 'export const request = {};');
  fs.writeFileSync(path.join(root, 'run.mjs'), `
    import os from 'node:os';
    import {syncBuiltinESMExports} from 'node:module';
    Object.defineProperty(process, 'platform', {value:process.env.TEST_PLATFORM});
    Object.defineProperty(process, 'arch', {value:process.env.TEST_ARCH});
    os.arch = () => process.env.TEST_ARCH;
    syncBuiltinESMExports();
    // Mirror mode must resolve without any request: no API call, no manifest
    // probe. TEST_API=200 only serves the release API, for the invalid-mirror
    // case that falls back to the ordinary direct path.
    globalThis.fetch = async url => {
      url = String(url);
      if (process.env.TEST_API === '200' && url.startsWith('https://api.github.com/')) {
        return new Response(JSON.stringify([{assets:[{
          name:'camoufox-158.0.1-beta.41-win.x86_64.zip',
          browser_download_url:'https://example.invalid/from-api',
        }]}]), {status:200});
      }
      throw new Error('unexpected request: ' + url);
    };
    await import('./bridge.mjs');
  `);
  const run = (platform, arch, mirror, api = '') => {
    const cache = path.join(root, `${platform}-${arch}`);
    const env = {...process.env, HOME:cache, LOCALAPPDATA:cache, XDG_CACHE_HOME:cache, TULAI_CAMOUFOX_CACHE:cache,
      TULAI_CAMOUFOX_MIRROR:mirror, GITHUB_TOKEN:'', TEST_PLATFORM:platform, TEST_ARCH:arch, TEST_API:api};
    return promisify(execFile)(process.execPath, [path.join(root, 'run.mjs'), 'install'], {env});
  };
  const github = 'https://github.com/daijro/camoufox/releases/download/v158.0.1-beta.41';
  for (const [platform, arch, suffix, sha] of [
    ['darwin','arm64','mac.arm64','9038f86d666c9780b6fff199d488737b8596c01f24e3e517845a0ecfb0cf8add'],
    ['darwin','x64','mac.x86_64','1afd6caa1e82caa41a4f56d8519f673e36b0835700b1c9c904e78a84c5711f3a'],
    ['win32','x64','win.x86_64','2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f'],
    ['linux','x64','lin.x86_64','3f2eb1d0451fa58e45e43e55f5e6181565069791ae60c0574f0ceb1f01341c33'],
    ['linux','arm64','lin.arm64','60e25f6eb95e13d9ec655bc02913d51057e669cb31542e6ace9d2190ab5380af'],
  ]) {
    // The trailing slash is trimmed; ghproxy form is base + '/' + the GitHub URL.
    const {stdout} = await run(platform, arch, 'https://gh.test/');
    assert.deepEqual(JSON.parse(JSON.parse(stdout).executable), {
      url:`https://gh.test/${github}/camoufox-158.0.1-beta.41-${suffix}.zip`, sha,
    });
  }
  // A mirror value that is not an http(s) URL keeps mirror mode off; the API
  // path runs and still gets the pinned digest attached.
  const {stdout} = await run('win32', 'x64', 'not a mirror', '200');
  assert.deepEqual(JSON.parse(JSON.parse(stdout).executable), {
    url:'https://example.invalid/from-api',
    sha:'2644bd14f1cc345c768f6a70b576bcc87eb3d0bf0df54856d6fe0bbc3027515f',
  });
});

test('GPU probe never opens a visible window before the account browser', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-gpu-probe-'));
  t.after(() => fs.rmSync(root, {recursive:true, force:true}));
  for (const file of ['bridge.mjs', 'identity.mjs', 'download-watch.mjs', 'windows-compat.mjs']) fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  const pkg = path.join(root, 'node_modules', 'camoufox');
  fs.mkdirSync(path.join(pkg, 'dist'), {recursive:true});
  fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({type:'module', exports:{'.':'./index.js','./dist/utils.js':'./dist/utils.js'}}));
  fs.writeFileSync(path.join(pkg, 'index.js'), `
    export const Camoufox = () => {};
    export const installedVerStr = () => '';
    export const launchOptions = async () => ({});
  `);
  // A fresh identity carries no WebGL pair, so launchOptions probes the host
  // GPU through this hook; whatever headless flag the caller asked for must not
  // reach it as a visible launch.
  fs.writeFileSync(path.join(pkg, 'dist', 'utils.js'), `
    export const utilsDeps = {hostGpu: async (executable, headless) => ({executable, headless})};
  `);
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, {recursive:true});
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({type:'module', exports:'./index.js'}));
  fs.writeFileSync(path.join(playwright, 'index.js'), 'export const request = {};');
  fs.writeFileSync(path.join(root, 'run.mjs'), `
    Object.defineProperty(process, 'platform', {value:process.env.TEST_PLATFORM});
    // bridge.mjs redirects process.stdout to stderr; keep a real writer first.
    const emit = process.stdout.write.bind(process.stdout);
    await import('./bridge.mjs');
    const {utilsDeps} = await import('camoufox/dist/utils.js');
    const probe = await utilsDeps.hostGpu('probe-exe', false, {}, {});
    emit(JSON.stringify(probe) + '\\n');
    process.exit(0);
  `);
  for (const [platform, headless] of [['darwin', true], ['win32', true], ['linux', false]]) {
    const {stdout} = await promisify(execFile)(process.execPath, [path.join(root, 'run.mjs')],
      {env:{...process.env, TEST_PLATFORM:platform}});
    // darwin and windows force the probe headless; linux passes it through as-is.
    assert.deepEqual(JSON.parse(stdout), {executable:'probe-exe', headless});
  }
});

function setupBridgeSandbox(t, {gpuResult = ['Apple', 'Apple M1'], gpuError = '', candidates = []}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-bridge-rpc-'));
  t.after(() => fs.rmSync(root, {recursive:true, force:true}));
  for (const file of ['bridge.mjs', 'identity.mjs', 'download-watch.mjs', 'windows-compat.mjs']) {
    fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
  }
  const traceFile = path.join(root, 'trace.json');
  const pkg = path.join(root, 'node_modules', 'camoufox');
  fs.mkdirSync(path.join(pkg, 'dist'), {recursive:true});
  fs.writeFileSync(path.join(pkg, 'package.json'), JSON.stringify({
    type:'module',
    exports:{'.':'./index.js', './dist/utils.js':'./dist/utils.js'},
  }));
  fs.writeFileSync(path.join(pkg, 'dist', 'utils.js'), `
    export const STOCK_MEDIA_DEFAULTS = {colorScheme:null, reducedMotion:null, forcedColors:null, contrast:null};
    export const utilsDeps = {
      hostGpu: async () => {
        if (process.env.TEST_GPU_ERROR) throw new Error(process.env.TEST_GPU_ERROR);
        return JSON.parse(process.env.TEST_GPU_RESULT);
      },
    };
  `);
  fs.writeFileSync(path.join(pkg, 'index.js'), `
    import {EventEmitter} from 'node:events';
    import fs from 'node:fs';
    const traceFile = process.env.TEST_TRACE_FILE;
    const candidates = JSON.parse(process.env.TEST_CANDIDATES || '[]');
    const trace = fs.existsSync(traceFile) ? JSON.parse(fs.readFileSync(traceFile, 'utf8')) : {launchCalls:[], newContextCalls:[], contexts:[], closedContexts:0};
    const save = () => fs.writeFileSync(traceFile, JSON.stringify(trace));
    let nextLaunch = 0;

    function makePage(owner) {
      const page = new EventEmitter();
      page.closed = false;
      page.url = () => 'about:blank';
      page.isClosed = () => page.closed;
      page.evaluate = async fn => {
        owner.evaluated.push(String(fn));
      };
      page.goto = async url => { owner.visited.push(url); };
      page.close = async () => {
        if (page.closed) return;
        page.closed = true;
        const idx = owner.pagesList.indexOf(page);
        if (idx >= 0) owner.pagesList.splice(idx, 1);
        page.emit('close');
      };
      return page;
    }

    function makeContext(kind, options = {}) {
      const ctx = new EventEmitter();
      const record = {kind, options, initScripts:[], bindings:[], evaluated:[], visited:[], closed:false, defaultTimeout:null};
      trace.contexts.push(record);
      ctx.pagesList = [];
      ctx.pages = () => ctx.pagesList;
      ctx.setDefaultTimeout = ms => { record.defaultTimeout = ms; save(); };
      ctx.addInitScript = async ({content}) => { record.initScripts.push(content); save(); };
      ctx.exposeBinding = async name => { record.bindings.push(name); save(); };
      ctx.newPage = async () => {
        const page = makePage(record);
        record.pagesList = ctx.pagesList;
        ctx.pagesList.push(page);
        save();
        return page;
      };
      ctx.close = async () => {
        if (record.closed) return;
        record.closed = true;
        trace.closedContexts++;
        save();
      };
      return ctx;
    }

    export class CamoufoxFetcher {}
    export const installedVerStr = () => '158.0.1-beta.41';
    export const launchOptions = async opts => {
      trace.launchCalls.push({
        os:opts.os, headless:opts.headless, canvas_noise:opts.canvas_noise,
        pin_cpu_cores:opts.pin_cpu_cores, i_know_what_im_doing:opts.i_know_what_im_doing,
        config:opts.config ?? null,
      });
      const generated = candidates[Math.min(nextLaunch++, Math.max(0, candidates.length - 1))] || opts.config || {};
      const merged = opts.config ? {...generated, ...opts.config} : generated;
      save();
      return {executablePath:'fake-exe', env:{CAMOU_CONFIG_1:JSON.stringify(merged)}};
    };
    export const Camoufox = async () => {
      const rootCtx = makeContext('persistent');
      const initial = await rootCtx.newPage();
      rootCtx.browser = () => ({
        newContext: async opts => {
          trace.newContextCalls.push(opts);
          save();
          return makeContext('isolated', opts);
        },
      });
      save();
      void initial;
      return rootCtx;
    };
  `);
  const playwright = path.join(root, 'node_modules', 'playwright-core');
  fs.mkdirSync(playwright, {recursive:true});
  fs.writeFileSync(path.join(playwright, 'package.json'), JSON.stringify({type:'module', exports:'./index.js'}));
  fs.writeFileSync(path.join(playwright, 'index.js'), 'export const request = {};');

  const runRpc = requests => new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [path.join(root, 'bridge.mjs')], {
      env:{
        ...process.env,
        TEST_TRACE_FILE:traceFile,
        TEST_GPU_RESULT:JSON.stringify(gpuResult),
        TEST_GPU_ERROR:gpuError,
        TEST_CANDIDATES:JSON.stringify(candidates),
      },
      stdio:['pipe', 'pipe', 'pipe'],
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
      resolve({responses, stderr:stderrBuf, trace:JSON.parse(fs.readFileSync(traceFile, 'utf8'))});
    });
    sendNext();
  });

  return {root, runRpc};
}

const sampleDevice = {
  'navigator.userAgent':'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:158.0) Gecko/20100101 Firefox/158.0',
  'navigator.platform':'MacIntel',
  'navigator.oscpu':'Intel Mac OS X 10.15',
  'navigator.hardwareConcurrency':10,
  'screen.width':1920,
  'screen.height':1080,
  'screen.availWidth':1920,
  'screen.availHeight':1050,
  'screen.colorDepth':24,
  'window.innerWidth':1440,
  'window.innerHeight':900,
  'window.devicePixelRatio':2,
  'webGl:vendor':'Apple',
  'webGl:renderer':'Apple M1',
  'webGl:parameters':{7937:'Apple M1'},
};

test('start activates canvas_noise, pin_cpu_cores, collisionRoots, and preserves WebGL while stripping legacy keys', async t => {
  const firstCandidate = {...sampleDevice};
  const uniqueCandidate = {...sampleDevice, 'navigator.hardwareConcurrency':12, 'window.innerWidth':1280};
  const {root, runRpc} = setupBridgeSandbox(t, {
    gpuResult:['Apple', 'Apple M1'],
    candidates:[firstCandidate, uniqueCandidate],
  });
  const downloadBrowser = path.join(root, 'DownloadBrowser');
  fs.mkdirSync(downloadBrowser, {recursive:true});
  fs.writeFileSync(path.join(downloadBrowser, '.tulai-identity.json'), JSON.stringify(firstCandidate));
  const profile = path.join(root, 'accounts', 'acc-1');

  const firstRun = await runRpc([
    {id:1, method:'start', params:{profile, executable:'fake-exe', headless:true, collisionRoots:[downloadBrowser]}},
    {id:2, method:'Browser.close', params:{}},
  ]);
  assert.equal(firstRun.responses.find(r => r.id === 1)?.result?.targetId, '1');
  assert.equal(firstRun.trace.launchCalls.length, 2);
  for (const call of firstRun.trace.launchCalls) {
    assert.equal(call.canvas_noise, true);
    assert.equal(call.pin_cpu_cores, true);
  }
  const identityPath = path.join(profile, '.tulai-identity.json');
  const savedAfterCreate = JSON.parse(fs.readFileSync(identityPath, 'utf8'));
  assert.equal(savedAfterCreate['navigator.hardwareConcurrency'], 12);

  // Inject legacy 156/158 removed keys on disk before restart.
  fs.writeFileSync(identityPath, JSON.stringify({
    ...savedAfterCreate,
    'canvas:seed':11, 'canvas:aaOffset':2, 'canvas:aaCapOffset':3, 'fonts:spacing_seed':4, 'audio:seed':5,
  }));

  const restartRun = await runRpc([
    {id:1, method:'start', params:{profile, executable:'fake-exe', headless:true}},
    {id:2, method:'Browser.close', params:{}},
  ]);
  assert.equal(restartRun.responses.find(r => r.id === 1)?.result?.targetId, '1');
  const restartLaunch = restartRun.trace.launchCalls.at(-1);
  assert.equal(restartLaunch.canvas_noise, true);
  assert.equal(restartLaunch.pin_cpu_cores, true);
  assert.equal(restartLaunch.config['webGl:vendor'], 'Apple');
  assert.equal(restartLaunch.config['webGl:renderer'], 'Apple M1');
  assert.deepEqual(restartLaunch.config['webGl:parameters'], {7937:'Apple M1'});
  for (const legacyKey of ['canvas:seed', 'canvas:aaOffset', 'canvas:aaCapOffset', 'fonts:spacing_seed', 'audio:seed']) {
    assert.equal(legacyKey in restartLaunch.config, false);
  }
});

test('start disables canvas_noise for software rasterizer or null GPU and falls back when probe fails', async t => {
  for (const [label, opts, expectedNoise] of [
    ['null GPU', {gpuResult:null}, false],
    ['llvmpipe software renderer', {gpuResult:['Mesa', 'llvmpipe (LLVM 15.0, 256 bits)']}, false],
    ['probe error fallback', {gpuError:'probe failed'}, undefined],
  ]) {
    const {root, runRpc} = setupBridgeSandbox(t, {...opts, candidates:[sampleDevice]});
    const profile = path.join(root, 'profile', label.replace(/\W+/g, '-'));
    const {trace} = await runRpc([
      {id:1, method:'start', params:{profile, executable:'fake-exe', headless:true}},
      {id:2, method:'Browser.close', params:{}},
    ]);
    assert.equal(trace.launchCalls.at(-1).canvas_noise, expectedNoise, label);
    assert.equal(trace.launchCalls.at(-1).pin_cpu_cores, true, label);
  }
});

test('isolated contexts inherit media defaults, proxy, scopeBootstrap, bindings, and close cleanly with their page', async t => {
  const {root, runRpc} = setupBridgeSandbox(t, {gpuResult:['Apple', 'Apple M1'], candidates:[sampleDevice]});
  const profile = path.join(root, 'accounts', 'acc-isolated');
  const {responses, trace} = await runRpc([
    {id:1, method:'start', params:{profile, executable:'fake-exe', headless:true, proxy:'http://user:pass@127.0.0.1:8080'}},
    {id:2, method:'Runtime.addBinding', target:'1', params:{name:'tulaiPipe'}},
    {id:3, method:'Target.createTarget', params:{isolated:true, url:'https://example.test/first'}},
    {id:4, method:'Target.createTarget', params:{isolated:true, proxy:'http://127.0.0.1:9090', url:'https://example.test/second'}},
    {id:5, method:'Runtime.addBinding', target:'1', params:{name:'latePipe'}},
    {id:6, method:'Page.close', target:'2', params:{}},
    {id:7, method:'Target.closeTarget', params:{targetId:'3'}},
    {id:8, method:'Browser.close', params:{}},
  ]);
  for (const reqId of [1, 2, 3, 4, 5, 6, 7, 8]) {
    const res = responses.find(r => r.id === reqId);
    assert.ok(res && !res.error, `request ${reqId} failed: ${res?.error}`);
  }
  assert.equal(trace.newContextCalls.length, 2);
  assert.deepEqual(trace.newContextCalls[0], {
    colorScheme:null, reducedMotion:null, forcedColors:null, contrast:null,
    viewport:null, serviceWorkers:'block', ignoreHTTPSErrors:false,
    proxy:{server:'http://127.0.0.1:8080', username:'user', password:'pass'},
  });
  assert.deepEqual(trace.newContextCalls[1], {
    colorScheme:null, reducedMotion:null, forcedColors:null, contrast:null,
    viewport:null, serviceWorkers:'block', ignoreHTTPSErrors:false,
    proxy:{server:'http://127.0.0.1:9090'},
  });

  const persistentContext = trace.contexts.find(c => c.kind === 'persistent');
  const isolatedContexts = trace.contexts.filter(c => c.kind === 'isolated');
  assert.equal(isolatedContexts.length, 2);
  for (const ctx of isolatedContexts) {
    assert.equal(ctx.defaultTimeout, 25000);
    assert.equal(ctx.closed, true);
    assert.ok(ctx.initScripts[0]?.includes('__tulai|tulai'), 'missing scopeBootstrap init script');
    assert.equal(ctx.initScripts[0], persistentContext.initScripts[0]);
    assert.deepEqual(ctx.bindings, ['tulaiPipe', 'latePipe']);
    assert.equal(ctx.evaluated.length, 2, 'latePipe must evaluate on active isolated pages');
  }
});
