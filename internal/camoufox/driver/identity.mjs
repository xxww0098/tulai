import {createHash, randomBytes} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

// Compare page-visible properties, not incidental config fields.
function fingerprintValues(config) {
  return [
    config['navigator.platform'], config['navigator.oscpu'], config['navigator.hardwareConcurrency'],
    config['screen.width'], config['screen.height'],
    config['screen.availWidth'], config['screen.availHeight'],
    config['screen.colorDepth'],
  ];
}

// Reserve a generated identity before another environment can choose the same device.
export async function createIdentity(profile, generate) {
  const parent = path.dirname(profile);
  const identityPath = path.join(profile, '.tulai-identity.json');
  const locks = path.join(parent, '.tulai-fingerprint-locks');
  fs.mkdirSync(locks, {recursive: true, mode: 0o700});
  for (let attempt = 0; attempt < 128; attempt++) {
    const result = await generate();
    const values = fingerprintValues(result.identity);
    if (values.some(value => value == null)) throw new Error('生成的浏览器指纹不完整，请重试');
    const key = JSON.stringify(values);
    const lockPath = path.join(locks, createHash('sha256').update(key).digest('hex'));
    // shortcut: a force-killed launcher can leave one candidate locked; recover stale locks if failures recur.
    let lock;
    try { lock = fs.openSync(lockPath, 'wx', 0o600); }
    catch (error) { if (error.code === 'EEXIST') continue; throw error; }
    try {
      let duplicate = false;
      for (const entry of fs.readdirSync(parent, {withFileTypes: true})) {
        if (!entry.isDirectory() || entry.name === path.basename(profile)) continue;
        let saved;
        try {
          saved = JSON.parse(fs.readFileSync(path.join(parent, entry.name, '.tulai-identity.json'), 'utf8'));
          prepareIdentity(saved, '0', false);
        } catch (error) {
          if (error.code === 'ENOENT') continue;
          throw new Error(`读取已有环境指纹失败（${entry.name}）：${error.message}`);
        }
        // An absent legacy field cannot establish that two devices differ.
        if (fingerprintValues(saved).every((value, index) => value == null || value === values[index])) {
          duplicate = true; break;
        }
      }
      if (duplicate) continue;
      const temporary = path.join(profile, `.tulai-identity-${process.pid}-${randomBytes(8).toString('hex')}.tmp`);
      const file = fs.openSync(temporary, 'wx', 0o600);
      try {
        try { fs.writeFileSync(file, JSON.stringify(result.identity)); }
        finally { fs.closeSync(file); }
        fs.linkSync(temporary, identityPath);
      } finally { fs.rmSync(temporary, {force: true}); }
      return result;
    } finally {
      fs.closeSync(lock);
      fs.rmSync(lockPath, {force: true});
    }
  }
  throw new Error('无法生成与现有环境不同的浏览器指纹，请重试');
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
