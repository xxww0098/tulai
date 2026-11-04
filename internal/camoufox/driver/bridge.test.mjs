import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
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
  for (const file of ['bridge.mjs', 'identity.mjs']) fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
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

test('installer recovers from GitHub API rate limits with verified pinned releases', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-release-fallback-'));
  t.after(() => fs.rmSync(root, {recursive:true, force:true}));
  for (const file of ['bridge.mjs', 'identity.mjs']) fs.copyFileSync(new URL(file, import.meta.url), path.join(root, file));
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
  const {stdout} = await run('win32', 'x64', '200');
  assert.equal(JSON.parse(JSON.parse(stdout).executable).url, 'https://example.invalid/from-api');
  await assert.rejects(run('win32', 'x64', '500'), error => error.stderr.includes('500 Server Error'));
  await assert.rejects(run('win32', 'x64', '403', true), error => error.stderr.includes('403 Client Error'));
});
