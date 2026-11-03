import assert from 'node:assert/strict';
import {execFile} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
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
