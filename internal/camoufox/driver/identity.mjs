import {createHash, randomBytes} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {isDeepStrictEqual} from 'node:util';

const STALE_LOCK_MS = 5 * 60 * 1000;

// Compare page-visible properties, not incidental config fields.
function fingerprintValues(config) {
  return [
    config['navigator.platform'], config['navigator.oscpu'], config['navigator.hardwareConcurrency'],
    config['screen.width'], config['screen.height'],
    config['screen.availWidth'], config['screen.availHeight'],
    config['screen.colorDepth'],
  ];
}

function viewportValues(config) {
  const values = [
    config['screen.width'],
    config['screen.height'],
    config['window.innerWidth'],
    config['window.innerHeight'],
    config['window.devicePixelRatio'],
  ];
  return values.every(value => value != null) ? values : null;
}

export function claimFingerprint(locks, key) {
  const file = path.join(locks, createHash('sha256').update(key).digest('hex'));
  for (let retried = false; ; ) {
    let lock;
    try {
      lock = fs.openSync(file, 'wx', 0o600);
      return () => {
        try { fs.closeSync(lock); } finally { fs.rmSync(file, {force: true}); }
      };
    } catch (error) {
      if (error.code !== 'EEXIST') throw error;
      if (retried) return null;
      retried = true;
      let stat;
      try { stat = fs.statSync(file); }
      catch (statError) {
        if (statError.code === 'ENOENT') continue;
        throw statError;
      }
      if (Date.now() - stat.mtimeMs <= STALE_LOCK_MS) return null;
      try { fs.unlinkSync(file); }
      catch (unlinkError) {
        if (unlinkError.code !== 'ENOENT') throw unlinkError;
      }
    }
  }
}

function collectIdentityFiles(profileDir, collisionRoots) {
  const roots = [path.dirname(profileDir), ...(Array.isArray(collisionRoots) ? collisionRoots : [])]
    .filter(root => typeof root === 'string' && root.trim())
    .map(root => path.resolve(root));
  const files = [];
  const seenDirs = new Set();
  const addDir = (dir, label) => {
    const resolved = path.resolve(dir);
    if (resolved === profileDir || seenDirs.has(resolved)) return;
    seenDirs.add(resolved);
    files.push({file: path.join(resolved, '.tulai-identity.json'), label});
  };
  for (const root of new Set(roots)) {
    let entries;
    try { entries = fs.readdirSync(root, {withFileTypes: true}); }
    catch (error) {
      if (error.code === 'ENOENT' || error.code === 'ENOTDIR') continue;
      throw error;
    }
    // Support both single-profile roots (e.g. DownloadBrowser) and multi-account roots.
    addDir(root, path.basename(root) || root);
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      addDir(path.join(root, entry.name), entry.name);
    }
  }
  return files;
}

// Reserve a generated identity before another environment can choose the same device.
export async function createIdentity(profile, generate, options = {}) {
  const profileDir = path.resolve(profile);
  const parent = path.dirname(profileDir);
  const identityPath = path.join(profileDir, '.tulai-identity.json');
  const locks = path.join(parent, '.tulai-fingerprint-locks');
  fs.mkdirSync(locks, {recursive: true, mode: 0o700});
  for (let attempt = 0; attempt < 128; attempt++) {
    const result = await generate();
    const values = fingerprintValues(result.identity);
    if (values.some(value => value == null)) throw new Error('生成的浏览器指纹不完整，请重试');
    const deviceKey = JSON.stringify(values);
    const releaseDevice = claimFingerprint(locks, deviceKey);
    if (!releaseDevice) continue;
    let releaseViewport = null;
    try {
      const viewport = viewportValues(result.identity);
      if (viewport) {
        releaseViewport = claimFingerprint(locks, JSON.stringify(['viewport', ...viewport]));
        if (!releaseViewport) continue;
      }
      let duplicate = false;
      for (const {file, label} of collectIdentityFiles(profileDir, options?.collisionRoots)) {
        let saved;
        try {
          saved = JSON.parse(fs.readFileSync(file, 'utf8'));
          prepareIdentity(saved, '0', false);
        } catch (error) {
          if (error.code === 'ENOENT') continue;
          throw new Error(`读取已有环境指纹失败（${label}）：${error.message}`);
        }
        // An absent legacy field cannot establish that two devices differ.
        if (fingerprintValues(saved).every((value, index) => value == null || value === values[index])) {
          duplicate = true;
          break;
        }
        const savedViewport = viewportValues(saved);
        if (viewport && savedViewport && savedViewport.every((value, index) => value === viewport[index])) {
          duplicate = true;
          break;
        }
      }
      if (duplicate) continue;
      const temporary = path.join(profileDir, `.tulai-identity-${process.pid}-${randomBytes(8).toString('hex')}.tmp`);
      const file = fs.openSync(temporary, 'wx', 0o600);
      try {
        try { fs.writeFileSync(file, JSON.stringify(result.identity)); }
        finally { fs.closeSync(file); }
        fs.linkSync(temporary, identityPath);
      } finally { fs.rmSync(temporary, {force: true}); }
      return result;
    } finally {
      try { releaseViewport?.(); } finally { releaseDevice(); }
    }
  }
  throw new Error('无法生成与现有环境不同的浏览器指纹，请重试');
}

export async function refreshIdentity(identityPath, saved, identity) {
  if (isDeepStrictEqual(saved, identity)) return;
  const temporary = `${identityPath}.${process.pid}-${randomBytes(8).toString('hex')}.tmp`;
  const file = fs.openSync(temporary, 'wx', 0o600);
  try {
    try { fs.writeFileSync(file, JSON.stringify(identity)); }
    finally { fs.closeSync(file); }
    for (let attempt = 0; ; attempt++) {
      try {
        fs.renameSync(temporary, identityPath);
        return;
      } catch (error) {
        const busy = ['EPERM', 'EACCES', 'EBUSY'].includes(error.code);
        if (process.platform !== 'win32' || !busy || attempt >= 7) throw error;
        await delay(50);
      }
    }
  } finally {
    fs.rmSync(temporary, {force: true});
  }
}

function voiceLocale(identity) {
  const language = identity['locale:language'];
  return String(language ? [language, identity['locale:region']].filter(Boolean).join('-') : identity['navigator.language'] || '').toLowerCase();
}

// Keep the device's voices unless the proxy changes their language.
export async function preserveVoices(saved, config, generate) {
  const result = await generate(config);
  if (!Array.isArray(saved.voices) || voiceLocale(saved) === voiceLocale(result.identity)) return result;
  const refreshed = {...config};
  delete refreshed.voices;
  return generate(refreshed);
}

// Persist device identity, but refresh browser version and proxy-derived location.
export function prepareIdentity(saved, firefoxVersion, hasProxy) {
  if (saved === undefined) return undefined;
  if (!saved || typeof saved !== 'object' || Array.isArray(saved) ||
      !/Firefox\/\d+/.test(saved['navigator.userAgent']) || typeof saved['navigator.platform'] !== 'string' ||
      !Number.isFinite(saved['screen.width']) || saved['screen.width'] <= 0 ||
      !Number.isFinite(saved['screen.height']) || saved['screen.height'] <= 0) {
    throw new Error('账号指纹文件不完整，请恢复原文件后重试');
  }
  const config = {...saved};
  delete config.addons;
  for (const key of ['navigator.userAgent', 'navigator.appVersion', 'headers.User-Agent']) {
    if (typeof config[key] === 'string') {
      config[key] = config[key].replace(/(Firefox\/|rv:)\d+(?:\.\d+)*/g, `$1${firefoxVersion}.0`);
    }
  }
  if (hasProxy || 'webrtc:ipv4' in config || 'webrtc:ipv6' in config || 'geolocation:latitude' in config) {
    // launchOptions preserves caller values, so stale exit metadata must be removed first.
    for (const key of Object.keys(config)) {
      if (/^(webrtc:|geolocation:|locale:)/.test(key) || ['timezone','network:ipv4Only','navigator.language','navigator.languages','headers.Accept-Language'].includes(key)) delete config[key];
    }
  }
  return config;
}
