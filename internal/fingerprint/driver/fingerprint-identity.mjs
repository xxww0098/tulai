import {createHash, randomBytes, randomInt} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

export const FINGERPRINT_IDENTITY_FILE = '.tulai-fingerprint.json';
// Real display as last seen by a headed window, shared by every profile next to it.
export const HOST_DISPLAY_FILE = '.tulai-host-display.json';

// The audio and client-rect noise layers only add what a page can prove wrong:
// an OfflineAudioContext that reports a sample rate other than the one it was
// created with, layout rectangles offset from their CSS position. Canvas noise
// stays on: it is what makes one account render differently from another, and
// it is stable per seed and consistent across readbacks.
const DISABLED_SPOOFING = 'audio,clientrects';
const DEFAULT_COLOR_DEPTH = 24;

const STALE_LOCK_MS = 5 * 60 * 1000;
const GEN_LOCK_STALE_MS = 30 * 1000;
const GEN_LOCK_WAIT_MS = 10 * 1000;
const SLEEP_VIEW = new Int32Array(new SharedArrayBuffer(4));
const MIN_SEED = 50000;
const MAX_SEED = 0x7fffffff;
const SUPPORTED_PLATFORMS = new Set(['macos', 'windows', 'linux']);
const VALID_DPR = new Set([1, 1.25, 1.5, 2]);
const VERSION_RE = /^\d+(?:\.\d+){1,3}$/;
const GPU_MODULUS = {
  macos: 2,
  windows: 10,
  linux: 2,
};
const CORE_CHOICES = {
  macos: [8, 10, 12, 14, 16],
  windows: [4, 6, 8, 12, 16, 20, 24],
  linux: [4, 6, 8, 12, 16, 24],
};
const MACOS_SEQUOIA_VERSIONS = ['15.0.1', '15.1.1', '15.2.0', '15.3.1', '15.3.2', '15.4.1', '15.5.0'];
const PLATFORM_VERSIONS = {
  macos: ['14.5.0', '14.6.1', '14.7.1', '14.7.2', '14.7.4', ...MACOS_SEQUOIA_VERSIONS],
  windows: ['10.0.0', '13.0.0', '14.0.0', '15.0.0', '19.0.0'],
  linux: ['6.1.0', '6.5.0', '6.6.0', '6.8.0', '6.11.0', '6.12.0', '6.14.0'],
};
// On macOS, fingerprint-chromium selects Apple M4 for even seeds (seed % 2 === 0)
// and Apple M2 for odd seeds (seed % 2 === 1). Match authentic core counts and
// minimum shipping OS versions (Apple M4 launched with macOS 15 Sequoia).
const MACOS_M4_CORES = [10, 14, 16];
// Apple M2 models by memory bucket, the bucket being {8, 16, 32}[seed % 3]:
// 8 GiB is the 8-core chip, 32 GiB only the 10/12-core Pro and Max chips.
const MACOS_M2_MEMORY_CORES = {
  0: [8],
  1: [8, 10, 12],
  2: [10, 12],
};
const MACOS_GPU_VERSIONS = {
  0: MACOS_SEQUOIA_VERSIONS,
  1: PLATFORM_VERSIONS.macos,
};
const SCREEN_PRESETS = {
  macos: [
    { width: 1440, height: 900, availTop: 25, availBottom: 60, availBottomMin: 56, availBottomMax: 68, dpr: 2, windowWidth: 1366, windowHeight: 800 },
    { width: 1470, height: 956, availTop: 37, availBottom: 64, availBottomMin: 58, availBottomMax: 72, dpr: 2, windowWidth: 1380, windowHeight: 836 },
    { width: 1512, height: 982, availTop: 37, availBottom: 70, availBottomMin: 60, availBottomMax: 76, dpr: 2, windowWidth: 1440, windowHeight: 860 },
    { width: 1680, height: 1050, availTop: 25, availBottom: 65, availBottomMin: 58, availBottomMax: 74, dpr: 2, windowWidth: 1440, windowHeight: 900 },
    { width: 1710, height: 1112, availTop: 37, availBottom: 68, availBottomMin: 60, availBottomMax: 76, dpr: 2, windowWidth: 1480, windowHeight: 920 },
    { width: 1728, height: 1117, availTop: 37, availBottom: 70, availBottomMin: 60, availBottomMax: 76, dpr: 2, windowWidth: 1512, windowHeight: 940 },
    { width: 1920, height: 1080, availTop: 25, availBottom: 64, availBottomMin: 56, availBottomMax: 72, dpr: 1, windowWidth: 1520, windowHeight: 920 },
    { width: 2048, height: 1280, availTop: 25, availBottom: 68, availBottomMin: 60, availBottomMax: 76, dpr: 2, windowWidth: 1600, windowHeight: 980 },
    { width: 2560, height: 1440, availTop: 25, availBottom: 70, availBottomMin: 60, availBottomMax: 78, dpr: 2, windowWidth: 1680, windowHeight: 1020 },
  ],
  windows: [
    { width: 1600, height: 900, availTop: 0, availBottom: 40, dpr: 1, windowWidth: 1366, windowHeight: 800 },
    { width: 1680, height: 1050, availTop: 0, availBottom: 48, dpr: 1, windowWidth: 1440, windowHeight: 900 },
    { width: 1707, height: 960, availTop: 0, availBottom: 48, dpr: 1.5, windowWidth: 1420, windowHeight: 840 },
    { width: 1920, height: 1080, availTop: 0, availBottom: 40, dpr: 1, windowWidth: 1440, windowHeight: 900 },
    { width: 1920, height: 1080, availTop: 0, availBottom: 48, dpr: 1.25, windowWidth: 1536, windowHeight: 920 },
    { width: 1920, height: 1080, availTop: 0, availBottom: 48, dpr: 2, windowWidth: 1520, windowHeight: 920 },
    { width: 1920, height: 1200, availTop: 0, availBottom: 48, dpr: 1, windowWidth: 1560, windowHeight: 960 },
    { width: 2048, height: 1152, availTop: 0, availBottom: 48, dpr: 1.25, windowWidth: 1600, windowHeight: 960 },
    { width: 2560, height: 1440, availTop: 0, availBottom: 48, dpr: 1, windowWidth: 1600, windowHeight: 960 },
    { width: 2560, height: 1600, availTop: 0, availBottom: 48, dpr: 1.5, windowWidth: 1660, windowHeight: 1000 },
  ],
  linux: [
    { width: 1440, height: 900, availTop: 27, availBottom: 0, dpr: 1, windowWidth: 1366, windowHeight: 820 },
    { width: 1600, height: 900, availTop: 27, availBottom: 0, dpr: 1, windowWidth: 1380, windowHeight: 820 },
    { width: 1680, height: 1050, availTop: 27, availBottom: 0, dpr: 1, windowWidth: 1440, windowHeight: 880 },
    { width: 1920, height: 1080, availTop: 27, availBottom: 0, dpr: 1, windowWidth: 1440, windowHeight: 900 },
    { width: 1920, height: 1080, availTop: 32, availBottom: 0, dpr: 2, windowWidth: 1500, windowHeight: 920 },
    { width: 1920, height: 1200, availTop: 27, availBottom: 0, dpr: 1, windowWidth: 1560, windowHeight: 960 },
    { width: 2560, height: 1440, availTop: 27, availBottom: 0, dpr: 1, windowWidth: 1640, windowHeight: 980 },
  ],
};

export function claimFingerprint(locks, key, staleMs = STALE_LOCK_MS) {
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
      if (Date.now() - stat.mtimeMs <= staleMs) return null;
      try { fs.unlinkSync(file); }
      catch (unlinkError) {
        if (unlinkError.code !== 'ENOENT') throw unlinkError;
      }
    }
  }
}

function sleepSync(ms) {
  if (ms > 0) Atomics.wait(SLEEP_VIEW, 0, 0, ms);
}

function acquireGenerationLock(locks) {
  const deadline = Date.now() + GEN_LOCK_WAIT_MS;
  for (;;) {
    const release = claimFingerprint(locks, 'fingerprint-generate', GEN_LOCK_STALE_MS);
    if (release) return release;
    const remaining = deadline - Date.now();
    if (remaining <= 0) return null;
    sleepSync(Math.min(remaining, randomInt(10, 36)));
  }
}

function readExistingIdentityFile(identityPath) {
  try {
    const raw = JSON.parse(fs.readFileSync(identityPath, 'utf8'));
    return validateFingerprintIdentity(raw);
  } catch (error) {
    if (error.code === 'ENOENT') return null;
    throw new Error(`账号 Fingerprint 指纹文件无法读取：${error.message}`);
  }
}

function defaultScreenFor(platform, seed) {
  const presets = SCREEN_PRESETS[platform] ?? SCREEN_PRESETS.linux;
  const p = presets[Math.abs(seed) % presets.length];
  return {
    width: p.width,
    height: p.height,
    availTop: p.availTop,
    availBottom: p.availBottom,
    dpr: p.dpr,
    windowWidth: p.windowWidth,
    windowHeight: p.windowHeight,
  };
}

function validateVersionString(value) {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  if (!trimmed || trimmed.length > 32 || !VERSION_RE.test(trimmed)) return null;
  return trimmed;
}

function validateScreen(screen) {
  if (!screen || typeof screen !== 'object' || Array.isArray(screen) ||
      !Number.isInteger(screen.width) || screen.width < 1024 || screen.width > 7680 ||
      !Number.isInteger(screen.height) || screen.height < 720 || screen.height > 4320 ||
      !Number.isInteger(screen.availTop) || screen.availTop < 0 ||
      !Number.isInteger(screen.availBottom) || screen.availBottom < 0 ||
      screen.availTop + screen.availBottom >= screen.height ||
      !VALID_DPR.has(screen.dpr) ||
      !Number.isInteger(screen.windowWidth) || screen.windowWidth < 1024 || screen.windowWidth > screen.width ||
      !Number.isInteger(screen.windowHeight) || screen.windowHeight < 600 || screen.windowHeight > screen.height - screen.availTop - screen.availBottom) {
    throw new Error('账号 Fingerprint 指纹文件不完整，请恢复原文件后重试');
  }
  const hasPos = screen.windowX !== undefined || screen.windowY !== undefined;
  if (hasPos) {
    const maxX = screen.width - screen.windowWidth;
    const maxY = screen.height - screen.availBottom - screen.windowHeight;
    if (!Number.isInteger(screen.windowX) || screen.windowX < 0 || screen.windowX > maxX ||
        !Number.isInteger(screen.windowY) || screen.windowY < screen.availTop || screen.windowY > maxY) {
      throw new Error('账号 Fingerprint 指纹文件不完整，请恢复原文件后重试');
    }
  }
  const normalized = {
    width: screen.width,
    height: screen.height,
    availTop: screen.availTop,
    availBottom: screen.availBottom,
    dpr: screen.dpr,
    windowWidth: screen.windowWidth,
    windowHeight: screen.windowHeight,
  };
  if (hasPos) {
    normalized.windowX = screen.windowX;
    normalized.windowY = screen.windowY;
  }
  return normalized;
}

export function validateFingerprintIdentity(saved) {
  if (!saved || typeof saved !== 'object' || Array.isArray(saved) ||
      saved.version !== 1 ||
      !Number.isInteger(saved.seed) || saved.seed < 1 || saved.seed > MAX_SEED ||
      !SUPPORTED_PLATFORMS.has(saved.platform) ||
      typeof saved.brand !== 'string' || !saved.brand.trim() ||
      !Number.isInteger(saved.hardwareConcurrency) || saved.hardwareConcurrency < 2 || saved.hardwareConcurrency > 64) {
    throw new Error('账号 Fingerprint 指纹文件不完整，请恢复原文件后重试');
  }
  let platformVersion;
  if (saved.platformVersion !== undefined) {
    platformVersion = validateVersionString(saved.platformVersion);
    if (!platformVersion) {
      throw new Error('账号 Fingerprint 指纹文件不完整，请恢复原文件后重试');
    }
  }
  let brandVersion;
  if (saved.brandVersion !== undefined) {
    brandVersion = validateVersionString(saved.brandVersion);
    if (!brandVersion) {
      throw new Error('账号 Fingerprint 指纹文件不完整，请恢复原文件后重试');
    }
  }
  const screen = saved.screen === undefined
    ? defaultScreenFor(saved.platform, saved.seed)
    : validateScreen(saved.screen);
  const normalized = {
    version: 1,
    seed: saved.seed,
    platform: saved.platform,
    brand: saved.brand.trim(),
    hardwareConcurrency: saved.hardwareConcurrency,
    screen,
  };
  if (platformVersion !== undefined) normalized.platformVersion = platformVersion;
  if (brandVersion !== undefined) normalized.brandVersion = brandVersion;
  return normalized;
}

function collectFingerprintIdentityFiles(profileDir, collisionRoots) {
  const roots = [path.dirname(profileDir), ...(Array.isArray(collisionRoots) ? collisionRoots : [])]
    .filter(root => typeof root === 'string' && root.trim())
    .map(root => path.resolve(root));
  const files = [];
  const seenDirs = new Set();
  const addDir = (dir, label) => {
    const resolved = path.resolve(dir);
    if (resolved === profileDir || seenDirs.has(resolved)) return;
    seenDirs.add(resolved);
    files.push({file: path.join(resolved, FINGERPRINT_IDENTITY_FILE), label});
  };
  for (const root of new Set(roots)) {
    let entries;
    try { entries = fs.readdirSync(root, {withFileTypes: true}); }
    catch (error) {
      if (error.code === 'ENOENT' || error.code === 'ENOTDIR') continue;
      throw error;
    }
    addDir(root, path.basename(root) || root);
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      addDir(path.join(root, entry.name), entry.name);
    }
  }
  return files;
}

function readExistingIdentities(profileDir, collisionRoots) {
  const existing = [];
  for (const {file, label} of collectFingerprintIdentityFiles(profileDir, collisionRoots)) {
    let saved;
    try {
      saved = validateFingerprintIdentity(JSON.parse(fs.readFileSync(file, 'utf8')));
    } catch (error) {
      if (error.code === 'ENOENT') continue;
      throw new Error(`读取已有 Fingerprint 环境指纹失败（${label}）：${error.message}`);
    }
    existing.push(saved);
  }
  return existing;
}

function pickLeastUsed(choices, counts) {
  let minCount = Infinity;
  for (const choice of choices) {
    const c = counts.get(choice) ?? 0;
    if (c < minCount) minCount = c;
  }
  const tied = choices.filter(choice => (counts.get(choice) ?? 0) === minCount);
  return tied[randomInt(0, tied.length)];
}

function randomStep(minVal, maxVal, step = 2) {
  if (maxVal <= minVal) return minVal;
  const span = Math.floor((maxVal - minVal) / step);
  return minVal + randomInt(0, span + 1) * step;
}

function screenPresetKey(screen) {
  return `${screen.width}x${screen.height}@${screen.dpr}`;
}

function viewportKey(platform, screen) {
  return JSON.stringify(['fingerprint-viewport', platform, screen.width, screen.height, screen.dpr, screen.windowWidth, screen.windowHeight]);
}

function gcd(a, b) {
  let x = a, y = b;
  while (y !== 0) [x, y] = [y, x % y];
  return x;
}

function lcm(a, b) {
  return (a / gcd(a, b)) * b;
}

// Generate a random 31-bit seed spanning both odd and even integers while
// balancing fingerprint-chromium's deterministic GPU (seed % gpuMod),
// deviceMemory (seed % 3), and brand patch version (seed % 4) buckets.
function generateBalancedSeed(platform, siblings) {
  const gpuMod = GPU_MODULUS[platform] ?? 2;
  const gpuCounts = new Map();
  const memCounts = new Map();
  const patchCounts = new Map();
  for (const item of siblings) {
    if (item.platform !== platform) continue;
    const g = item.seed % gpuMod;
    const m = item.seed % 3;
    const p = item.seed % 4;
    gpuCounts.set(g, (gpuCounts.get(g) ?? 0) + 1);
    memCounts.set(m, (memCounts.get(m) ?? 0) + 1);
    patchCounts.set(p, (patchCounts.get(p) ?? 0) + 1);
  }
  const gpuChoices = Array.from({length: gpuMod}, (_, i) => i);
  const targetGpu = pickLeastUsed(gpuChoices, gpuCounts);
  // deviceMemory is {8, 16, 32}[seed % 3]. No Apple M4 machine ships with
  // 8 GiB, so even (M4) seeds only draw the two larger buckets.
  const memoryChoices = platform === 'macos' && targetGpu % 2 === 0 ? [1, 2] : [0, 1, 2];
  const targetMem = pickLeastUsed(memoryChoices, memCounts);
  const patchChoices = [0, 1, 2, 3].filter(p => (p % 2) === (targetGpu % 2));
  const targetPatch = pickLeastUsed(patchChoices, patchCounts);

  const period = lcm(gpuMod, 12);
  let residue = 0;
  for (let r = 0; r < period; r++) {
    if (r % gpuMod === targetGpu && r % 3 === targetMem && r % 4 === targetPatch) {
      residue = r;
      break;
    }
  }
  const minK = Math.ceil((MIN_SEED - residue) / period);
  const maxK = Math.floor((MAX_SEED - residue) / period);
  return randomInt(minK, maxK + 1) * period + residue;
}

function generateDynamicScreen(platform, platformVersion, siblings) {
  const screenList = SCREEN_PRESETS[platform] ?? SCREEN_PRESETS.linux;
  const presetCounts = new Map();
  for (const item of siblings) {
    if (item.platform !== platform) continue;
    const key = screenPresetKey(item.screen);
    presetCounts.set(key, (presetCounts.get(key) ?? 0) + 1);
  }
  const presetKeys = screenList.map(screenPresetKey);
  const chosenKey = pickLeastUsed(presetKeys, presetCounts);
  const matchingPresets = screenList.filter(p => screenPresetKey(p) === chosenKey);
  const preset = matchingPresets[randomInt(0, matchingPresets.length)];

  const availTop = preset.availTop;
  let availBottom = preset.availBottom;
  if (platform === 'windows') {
    availBottom = platformVersion === '10.0.0' ? 40 : 48;
  } else if (platform === 'macos' && preset.availBottomMin !== undefined && preset.availBottomMax !== undefined) {
    availBottom = randomInt(preset.availBottomMin, preset.availBottomMax + 1);
  }
  const availH = preset.height - availTop - availBottom;
  const minWinW = Math.min(preset.width, 1320);
  const maxWinW = Math.min(preset.width, Math.max(minWinW, Math.min(1800, preset.width - 16)));
  const minWinH = Math.min(availH, 760);
  const maxWinH = Math.min(availH, Math.max(minWinH, Math.min(1080, availH - 12)));
  const windowWidth = randomStep(minWinW, maxWinW, 2);
  const windowHeight = randomStep(minWinH, maxWinH, 2);
  const maxX = Math.max(0, preset.width - windowWidth);
  const maxY = Math.max(availTop, preset.height - availBottom - windowHeight);
  const windowX = randomInt(0, maxX + 1);
  const windowY = randomInt(availTop, maxY + 1);
  return {
    width: preset.width,
    height: preset.height,
    availTop,
    availBottom,
    dpr: preset.dpr,
    windowWidth,
    windowHeight,
    windowX,
    windowY,
  };
}

// Persist a stable per-account fingerprint-chromium seed and hardware profile.
export function ensureFingerprintIdentity(profile, options = {}) {
  const profileDir = path.resolve(profile);
  const identityPath = path.join(profileDir, FINGERPRINT_IDENTITY_FILE);
  const initial = readExistingIdentityFile(identityPath);
  if (initial) return initial;

  const hostOS = SUPPORTED_PLATFORMS.has(options.hostOS) ? options.hostOS : 'linux';
  const defaultCoresList = CORE_CHOICES[hostOS] ?? CORE_CHOICES.linux;
  const defaultVersionList = PLATFORM_VERSIONS[hostOS] ?? PLATFORM_VERSIONS.linux;
  const hasCustomSeed = typeof options.pickSeed === 'function';
  const hasCustomCores = typeof options.pickCores === 'function';
  const hasCustomScreen = typeof options.pickScreen === 'function';
  const hasCustomPlatformVersion = typeof options.pickPlatformVersion === 'function';

  const parent = path.dirname(profileDir);
  const locks = path.join(parent, '.tulai-fingerprint-locks');
  fs.mkdirSync(locks, {recursive: true, mode: 0o700});
  fs.mkdirSync(profileDir, {recursive: true, mode: 0o700});

  const releaseGen = acquireGenerationLock(locks);
  try {
    const raced = readExistingIdentityFile(identityPath);
    if (raced) return raced;

    let siblings = readExistingIdentities(profileDir, options.collisionRoots);

    for (let attempt = 0; attempt < 128; attempt++) {
      const seed = hasCustomSeed
        ? options.pickSeed(attempt)
        : generateBalancedSeed(hostOS, siblings);
      if (!Number.isInteger(seed) || seed < 1 || seed > MAX_SEED) {
        throw new Error('生成的 Fingerprint 种子无效，请重试');
      }
      const releaseSeed = claimFingerprint(locks, JSON.stringify(['fingerprint-seed', seed]));
      if (!releaseSeed) continue;
      let releaseViewport = null;
      try {
        siblings = readExistingIdentities(profileDir, options.collisionRoots);
        if (siblings.some(saved => saved.seed === seed)) continue;

        const versionList = hostOS === 'macos'
          ? (MACOS_GPU_VERSIONS[seed % 2] ?? defaultVersionList)
          : defaultVersionList;
        let platformVersion;
        if (hasCustomPlatformVersion) {
          platformVersion = options.pickPlatformVersion(attempt);
        } else {
          const versionCounts = new Map();
          for (const item of siblings) {
            if (item.platform === hostOS && item.platformVersion) {
              versionCounts.set(item.platformVersion, (versionCounts.get(item.platformVersion) ?? 0) + 1);
            }
          }
          platformVersion = pickLeastUsed(versionList, versionCounts);
        }

        // The core count follows the hardware each GPU ships with.
        const coresList = hostOS === 'macos'
          ? (seed % 2 === 0 ? MACOS_M4_CORES : MACOS_M2_MEMORY_CORES[seed % 3])
          : defaultCoresList;
        let hardwareConcurrency;
        if (hasCustomCores) {
          hardwareConcurrency = options.pickCores(attempt);
        } else {
          const coreCounts = new Map();
          for (const item of siblings) {
            if (item.platform === hostOS) {
              coreCounts.set(item.hardwareConcurrency, (coreCounts.get(item.hardwareConcurrency) ?? 0) + 1);
            }
          }
          hardwareConcurrency = pickLeastUsed(coresList, coreCounts);
        }

        const screen = hasCustomScreen
          ? options.pickScreen(attempt)
          : generateDynamicScreen(hostOS, platformVersion, siblings);

        const candidate = {
          version: 1,
          seed,
          platform: hostOS,
          brand: 'Chrome',
          hardwareConcurrency,
          screen,
        };
        if (platformVersion !== undefined) candidate.platformVersion = platformVersion;

        const identity = validateFingerprintIdentity(candidate);

        if (!hasCustomScreen && attempt < 96) {
          const vpKey = viewportKey(hostOS, identity.screen);
          const vpCollides = siblings.some(saved => viewportKey(saved.platform, saved.screen) === vpKey);
          if (vpCollides) continue;
          releaseViewport = claimFingerprint(locks, vpKey);
          if (!releaseViewport) continue;
        }

        const temporary = path.join(profileDir, `.tulai-fingerprint-${process.pid}-${randomBytes(8).toString('hex')}.tmp`);
        const fd = fs.openSync(temporary, 'wx', 0o600);
        try {
          try { fs.writeFileSync(fd, JSON.stringify(identity)); }
          finally { fs.closeSync(fd); }
          try {
            fs.linkSync(temporary, identityPath);
          } catch (linkError) {
            if (linkError.code === 'EEXIST') {
              const winner = readExistingIdentityFile(identityPath);
              if (winner) return winner;
            }
            throw linkError;
          }
        } finally {
          fs.rmSync(temporary, {force: true});
        }
        return identity;
      } finally {
        try { releaseViewport?.(); } finally { releaseSeed(); }
      }
    }
    throw new Error('无法生成与现有环境不同的 Fingerprint 指纹，请重试');
  } finally {
    releaseGen?.();
  }
}

// Evaluated in a headed page: the display a real window on this host reports.
export const HOST_DISPLAY_EXPRESSION = `({width:screen.width,height:screen.height,availLeft:screen.availLeft,
availTop:screen.availTop,availRight:screen.width-screen.availLeft-screen.availWidth,
availBottom:screen.height-screen.availTop-screen.availHeight,dpr:devicePixelRatio,
colorDepth:screen.colorDepth,p3:matchMedia('(color-gamut: p3)').matches,
hdr:matchMedia('(dynamic-range: high)').matches,extended:screen.isExtended===true})`;

// validateHostDisplay accepts the real display a headed page reported and
// returns a normalized copy, or null when the record is unusable. It is only a
// cache: an unusable record falls back to the account's own screen preset.
export function validateHostDisplay(saved) {
  if (!saved || typeof saved !== 'object' || Array.isArray(saved)) return null;
  const { width, height, availLeft, availTop, availRight, availBottom, dpr, colorDepth } = saved;
  if (![width, height, availLeft, availTop, availRight, availBottom].every(Number.isInteger) ||
      width < 320 || width > 16384 || height < 240 || height > 16384 ||
      availLeft < 0 || availTop < 0 || availRight < 0 || availBottom < 0 ||
      availLeft + availRight >= width || availTop + availBottom >= height ||
      typeof dpr !== 'number' || !(dpr >= 0.5 && dpr <= 8) ||
      !Number.isInteger(colorDepth) || colorDepth < 1 || colorDepth > 64) {
    return null;
  }
  return {
    width, height, availLeft, availTop, availRight, availBottom, dpr, colorDepth,
    p3: saved.p3 === true,
    hdr: saved.hdr === true,
    extended: saved.extended === true,
  };
}

export function readHostDisplay(profile) {
  try {
    const raw = JSON.parse(fs.readFileSync(path.join(path.dirname(path.resolve(profile)), HOST_DISPLAY_FILE), 'utf8'));
    return validateHostDisplay(raw);
  } catch {
    return null;
  }
}

// writeHostDisplay replaces the shared record atomically; concurrent accounts
// racing to record the same host each write their own temporary file.
export function writeHostDisplay(profile, display) {
  const valid = validateHostDisplay(display);
  if (!valid) return false;
  const parent = path.dirname(path.resolve(profile));
  const temporary = path.join(parent, `.tulai-host-display-${process.pid}-${randomBytes(8).toString('hex')}.tmp`);
  let fd;
  try {
    fd = fs.openSync(temporary, 'wx', 0o600);
  } catch {
    return false;
  }
  try {
    try {
      fs.writeFileSync(fd, JSON.stringify(valid));
    } finally {
      fs.closeSync(fd);
    }
    fs.renameSync(temporary, path.join(parent, HOST_DISPLAY_FILE));
  } catch {
    try { fs.rmSync(temporary, {force: true}); } catch { /* best effort */ }
    return false;
  }
  return true;
}

// The display pages see for this launch. A headed window always reports the
// real display because the kernel cannot spoof screen.*, so a headless launch
// replays the display recorded from the latest headed session. Without such a
// record it falls back to the account's preset.
function launchScreen(valid, display) {
  const { width, height, availTop, availBottom, dpr } = valid.screen;
  return display
    ? { ...display }
    : { width, height, availLeft: 0, availTop, availRight: 0, availBottom, dpr, colorDepth: DEFAULT_COLOR_DEPTH, p3: false, extended: false };
}

// Media features a headless launch emulates on every page. A wide-gamut host
// reports P3 in a headed window, but the only switch that makes a headless page
// say so, display-p3-d65, recolors every capture. Emulating the feature answers
// the query the way the real display does and leaves rendering alone. An HDR
// host needs nothing here: its hdr10 profile already reports P3.
export function emulatedMedia(display, headless) {
  if (!headless || !display) return [];
  const valid = validateHostDisplay(display);
  return valid && valid.p3 && !valid.hdr ? [{ name: 'color-gamut', value: 'p3' }] : [];
}

export function buildFingerprintArgs(identity, options = {}) {
  const valid = validateFingerprintIdentity(identity);
  const display = options.display === undefined ? null : validateHostDisplay(options.display);
  const screen = launchScreen(valid, display);
  // No --disable-blink-features=AutomationControlled. The kernel's webdriver
  // patch already removed the only code that reads that feature, while the
  // switch puts Chromium's "unsupported command-line flag" bar on the first
  // tab of every headed window and costs that tab 52px of viewport height.
  const args = [
    `--disable-spoofing=${DISABLED_SPOOFING}`,
    `--fingerprint=${valid.seed}`,
    `--fingerprint-platform=${valid.platform}`,
  ];
  if (valid.platformVersion) {
    args.push(`--fingerprint-platform-version=${valid.platformVersion}`);
  }
  args.push(`--fingerprint-brand=${valid.brand}`);
  if (valid.brandVersion) {
    args.push(`--fingerprint-brand-version=${valid.brandVersion}`);
  }
  // Keep the window inside the work area of the display pages see. Headed and
  // headless sessions of one account then report the same screen.
  const workWidth = screen.width - screen.availLeft - screen.availRight;
  const workHeight = screen.height - screen.availTop - screen.availBottom;
  const windowWidth = Math.min(valid.screen.windowWidth, workWidth);
  const windowHeight = Math.min(valid.screen.windowHeight, workHeight);
  args.push(
    `--fingerprint-hardware-concurrency=${valid.hardwareConcurrency}`,
    `--window-size=${windowWidth},${windowHeight}`,
  );
  if (options.headless || display) {
    const x = Math.min(Math.max(valid.screen.windowX ?? 0, 0), workWidth - windowWidth) + screen.availLeft;
    const y = Math.min(Math.max(valid.screen.windowY ?? screen.availTop, screen.availTop), screen.availTop + workHeight - windowHeight);
    args.push(`--window-position=${x},${y}`);
  }
  if (options.headless) {
    // Headless Chromium defaults to an 800x600 virtual screen; describe the
    // display instead. Sizes and insets are physical pixels.
    const scale = value => Math.round(value * screen.dpr);
    const parts = [`${scale(screen.width)}x${scale(screen.height)}`];
    for (const [key, value] of [['workAreaLeft', screen.availLeft], ['workAreaTop', screen.availTop], ['workAreaRight', screen.availRight], ['workAreaBottom', screen.availBottom]]) {
      if (value > 0) parts.push(`${key}=${scale(value)}`);
    }
    if (screen.dpr !== 1) parts.push(`devicePixelRatio=${Math.round(screen.dpr * 1000) / 1000}`);
    if (screen.colorDepth !== DEFAULT_COLOR_DEPTH) parts.push(`colorDepth=${screen.colorDepth}`);
    // isExtended is readable without the Window Management permission, so keep it.
    args.push(`--screen-info={${parts.join(' ')}}${screen.extended ? '{1920x1080 isInternal=false}' : ''}`);
    // The virtual display is plain sRGB. An HDR host reports P3 and HDR in a
    // headed window; hdr10 reproduces both without shifting capture colors.
    if (screen.hdr) args.push('--force-color-profile=hdr10');
  }
  if (typeof options.timezone === 'string' && options.timezone.trim()) {
    args.push(`--timezone=${options.timezone.trim()}`);
  }
  if (typeof options.lang === 'string' && options.lang.trim()) {
    args.push(`--lang=${options.lang.trim()}`);
  }
  const hasProxy = typeof options.proxy === 'string'
    ? options.proxy.trim().length > 0
    : Boolean(options.proxy);
  if (options.directNetwork) {
    args.push('--no-proxy-server');
  } else if (hasProxy) {
    // Enforce ungoogled-chromium's WebRTC IP handling switch on proxied accounts
    // so direct non-proxied UDP cannot bypass the proxy even if profile prefs drift.
    args.push('--webrtc-ip-handling-policy=disable_non_proxied_udp');
  }
  return args;
}
