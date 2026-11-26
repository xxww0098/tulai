import assert from 'node:assert/strict';
import {spawn} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {fileURLToPath} from 'node:url';
import {
  FINGERPRINT_IDENTITY_FILE,
  HOST_DISPLAY_FILE,
  buildFingerprintArgs,
  claimFingerprint,
  emulatedMedia,
  ensureFingerprintIdentity,
  readHostDisplay,
  validateFingerprintIdentity,
  validateHostDisplay,
  writeHostDisplay,
} from './fingerprint-identity.mjs';

test('claimFingerprint rejects fresh locks and reclaims stale locks older than 5 minutes', t => {
  const locks = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-lock-'));
  t.after(() => fs.rmSync(locks, {recursive: true, force: true}));

  const release = claimFingerprint(locks, 'same-key');
  assert.equal(typeof release, 'function');
  assert.equal(claimFingerprint(locks, 'same-key'), null);

  const [entry] = fs.readdirSync(locks);
  const lockFile = path.join(locks, entry);
  const stale = new Date(Date.now() - 6 * 60 * 1000);
  fs.utimesSync(lockFile, stale, stale);

  const reclaimed = claimFingerprint(locks, 'same-key');
  assert.equal(typeof reclaimed, 'function');
  reclaimed();
  release();
  assert.equal(fs.existsSync(lockFile), false);
});

test('ensureFingerprintIdentity creates, persists, and avoids sibling seed collisions', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-id-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));

  const sampleScreen = {
    width: 1512,
    height: 982,
    availTop: 37,
    availBottom: 70,
    dpr: 2,
    windowWidth: 1440,
    windowHeight: 860,
  };
  const p1 = path.join(root, 'account-1');
  const first = ensureFingerprintIdentity(p1, {
    hostOS: 'macos',
    pickSeed: () => 200002,
    pickCores: () => 10,
    pickScreen: () => sampleScreen,
    pickPlatformVersion: () => '15.3.1',
  });
  assert.deepEqual(first, {
    version: 1,
    seed: 200002,
    platform: 'macos',
    platformVersion: '15.3.1',
    brand: 'Chrome',
    hardwareConcurrency: 10,
    screen: sampleScreen,
  });

  // Restart returns the exact saved identity without calling generators.
  const reloaded = ensureFingerprintIdentity(p1, {
    hostOS: 'windows',
    pickSeed: () => { throw new Error('must not regenerate'); },
  });
  assert.deepEqual(reloaded, first);

  // Sibling profile skips colliding seed and claims the next distinct seed.
  const p2 = path.join(root, 'account-2');
  const candidates = [200002, 200002, 400005];
  const second = ensureFingerprintIdentity(p2, {
    hostOS: 'macos',
    pickSeed: i => candidates[i] ?? 600007,
    pickCores: () => 12,
  });
  assert.equal(second.seed, 400005);
  assert.equal(second.hardwareConcurrency, 12);
  assert.match(second.platformVersion, /^\d+\.\d+\.\d+$/);
  assert.notEqual(second.platformVersion, first.platformVersion);

  // Cross-root collision avoidance via collisionRoots.
  const extraRoot = path.join(root, 'other-root');
  const p3 = path.join(extraRoot, 'account-3');
  const third = ensureFingerprintIdentity(p3, {
    hostOS: 'windows',
    collisionRoots: [root],
    pickSeed: i => (i === 0 ? 400005 : 800009),
    pickCores: () => 8,
  });
  assert.equal(third.seed, 800009);
  assert.equal(third.platform, 'windows');
  assert.match(third.platformVersion, /^(?:10|13|14|15|19)\.0\.0$/);
});

test('ensureFingerprintIdentity balances GPU/memory/patch/cores/OS/screen diversity across sibling accounts', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-div-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));

  // macOS: 6 sibling accounts must cover both GPU parities (M4/M2), all 3 memory tiers,
  // all 4 Chrome patch buckets, distinct platformVersions, distinct screen presets, and unique viewports.
  const macProfiles = [];
  for (let i = 0; i < 6; i++) {
    const id = ensureFingerprintIdentity(path.join(root, 'mac', `acc-${i}`), {hostOS: 'macos'});
    macProfiles.push(id);
    assert.ok(id.seed >= 50000 && id.seed <= 0x7fffffff);
    assert.ok(id.screen.width >= 1440 && id.screen.height >= 900);
    assert.ok(id.screen.windowWidth >= 1320 && id.screen.windowWidth <= id.screen.width);
    assert.ok(id.screen.windowHeight >= 760 && id.screen.windowHeight <= id.screen.height - id.screen.availTop - id.screen.availBottom);
    assert.ok(Number.isInteger(id.screen.windowX) && id.screen.windowX >= 0 && id.screen.windowX <= id.screen.width - id.screen.windowWidth);
    assert.ok(Number.isInteger(id.screen.windowY) && id.screen.windowY >= id.screen.availTop && id.screen.windowY <= id.screen.height - id.screen.availBottom - id.screen.windowHeight);
    assertMacHardware(id);
  }
  assert.equal(new Set(macProfiles.map(p => p.seed)).size, 6);
  assert.equal(new Set(macProfiles.map(p => p.seed % 2)).size, 2, 'macOS accounts must span both odd and even seeds (Apple M4 + M2)');
  assert.equal(new Set(macProfiles.map(p => p.seed % 3)).size, 3, 'macOS accounts must span all 3 deviceMemory buckets');
  assert.equal(new Set(macProfiles.map(p => p.seed % 4)).size, 4, 'macOS accounts must span all 4 Chrome patch buckets');
  assert.equal(new Set(macProfiles.map(p => p.platformVersion)).size, 6, 'macOS accounts must diversify platformVersion');
  // Every M4 core count plus the 8-core M2 are always covered; the 10/12-core M2
  // only appears when memory diversity leaves room for it.
  assert.ok(new Set(macProfiles.map(p => p.hardwareConcurrency)).size >= 4, 'macOS accounts must cover M4 and M2 core counts');
  assert.equal(new Set(macProfiles.map(p => `${p.screen.width}x${p.screen.height}@${p.screen.dpr}`)).size, 6);
  assert.equal(
    new Set(macProfiles.map(p => `${p.screen.width}x${p.screen.height}@${p.screen.dpr}:${p.screen.windowWidth}x${p.screen.windowHeight}`)).size,
    6,
  );

  // Windows: 10 sibling accounts must cover all 10 D3D11 GPU slots, all 5 OS versions,
  // and match taskbar height (40px for Win10 10.0.0 vs 48px for Win11 13.0.0+).
  const winProfiles = [];
  for (let i = 0; i < 10; i++) {
    const id = ensureFingerprintIdentity(path.join(root, 'win', `acc-${i}`), {hostOS: 'windows'});
    winProfiles.push(id);
    const wantTaskbar = id.platformVersion === '10.0.0' ? 40 : 48;
    assert.equal(id.screen.availBottom, wantTaskbar, `Windows ${id.platformVersion} taskbar mismatch`);
  }
  assert.equal(new Set(winProfiles.map(p => p.seed)).size, 10);
  assert.equal(new Set(winProfiles.map(p => p.seed % 10)).size, 10, 'Windows accounts must cover all 10 GPU buckets');
  assert.equal(new Set(winProfiles.map(p => p.seed % 3)).size, 3, 'Windows accounts must cover all 3 deviceMemory buckets');
  assert.equal(new Set(winProfiles.map(p => p.seed % 4)).size, 4, 'Windows accounts must cover all 4 Chrome patch buckets');
  assert.equal(new Set(winProfiles.map(p => p.platformVersion)).size, 5, 'Windows accounts must cover all 5 OS versions');
});

test('ensureFingerprintIdentity loads legacy v1 files without platformVersion or window position', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-legacy-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));

  const p1 = path.join(root, 'legacy-account');
  fs.mkdirSync(p1, {recursive: true});
  const legacyJson = {
    version: 1,
    seed: 200002,
    platform: 'macos',
    brand: 'Chrome',
    hardwareConcurrency: 10,
    screen: {width: 1512, height: 982, availTop: 37, availBottom: 70, dpr: 2, windowWidth: 1440, windowHeight: 860},
  };
  fs.writeFileSync(path.join(p1, FINGERPRINT_IDENTITY_FILE), JSON.stringify(legacyJson));
  const loaded = ensureFingerprintIdentity(p1, {hostOS: 'macos'});
  assert.deepEqual(loaded, legacyJson);
});

test('ensureFingerprintIdentity fails closed on corrupted identity files', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-corrupt-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));

  const p1 = path.join(root, 'account-1');
  fs.mkdirSync(p1, {recursive: true});
  fs.writeFileSync(path.join(p1, FINGERPRINT_IDENTITY_FILE), '{broken-json');
  assert.throws(() => ensureFingerprintIdentity(p1, {hostOS: 'macos'}), /指纹文件无法读取/);

  fs.writeFileSync(path.join(p1, FINGERPRINT_IDENTITY_FILE), JSON.stringify({version: 1, seed: -5, platform: 'macos', brand: 'Chrome', hardwareConcurrency: 8}));
  assert.throws(() => ensureFingerprintIdentity(p1, {hostOS: 'macos'}), /指纹文件不完整/);

  fs.writeFileSync(path.join(p1, FINGERPRINT_IDENTITY_FILE), JSON.stringify({
    version: 1, seed: 200002, platform: 'macos', brand: 'Chrome', hardwareConcurrency: 8,
    screen: {width: 800, height: 600, availTop: 0, availBottom: 0, dpr: 1, windowWidth: 1440, windowHeight: 900},
  }));
  assert.throws(() => ensureFingerprintIdentity(p1, {hostOS: 'macos'}), /指纹文件不完整/);

  fs.writeFileSync(path.join(p1, FINGERPRINT_IDENTITY_FILE), JSON.stringify({
    version: 1, seed: 200002, platform: 'macos', platformVersion: 'bad-ver', brand: 'Chrome', hardwareConcurrency: 8,
  }));
  assert.throws(() => ensureFingerprintIdentity(p1, {hostOS: 'macos'}), /指纹文件不完整/);

  fs.writeFileSync(path.join(p1, FINGERPRINT_IDENTITY_FILE), JSON.stringify({
    version: 1, seed: 200002, platform: 'macos', brand: 'Chrome', hardwareConcurrency: 8,
    screen: {width: 1512, height: 982, availTop: 37, availBottom: 70, dpr: 2, windowWidth: 1440, windowHeight: 860, windowX: 999, windowY: 40},
  }));
  assert.throws(() => ensureFingerprintIdentity(p1, {hostOS: 'macos'}), /指纹文件不完整/);

  fs.writeFileSync(path.join(p1, FINGERPRINT_IDENTITY_FILE), JSON.stringify({
    version: 1, seed: 200002, platform: 'macos', brand: 'Chrome', hardwareConcurrency: 8,
    screen: {width: 1512, height: 982, availTop: 37, availBottom: 70, dpr: 2, windowWidth: 1440, windowHeight: 860, windowX: 20},
  }));
  assert.throws(() => ensureFingerprintIdentity(p1, {hostOS: 'macos'}), /指纹文件不完整/);
});

test('buildFingerprintArgs emits desktop window-size, headless screen-info, platformVersion, and proxy WebRTC protection', () => {
  const legacy = validateFingerprintIdentity({
    version: 1,
    seed: 123456,
    platform: 'macos',
    brand: 'Chrome',
    hardwareConcurrency: 10,
  });
  assert.ok(legacy.screen.width >= 1440 && legacy.screen.windowWidth <= legacy.screen.width);

  const id = validateFingerprintIdentity({
    version: 1,
    seed: 123457,
    platform: 'macos',
    platformVersion: '15.3.1',
    brand: 'Chrome',
    hardwareConcurrency: 10,
    screen: {width: 1512, height: 982, availTop: 37, availBottom: 70, dpr: 2, windowWidth: 1440, windowHeight: 860, windowX: 36, windowY: 45},
  });

  // Chromium shows its "unsupported command-line flag" bar on the first tab of
  // a headed window that carries any blink-feature switch, and that tab loses
  // 52px of viewport height to it. The kernel already forces navigator.webdriver
  // false, so no such switch belongs on the command line.
  for (const options of [{headless: false}, {headless: true}]) {
    const args = buildFingerprintArgs(id, options);
    assert.deepEqual(args.filter(a => /^--(enable|disable)-blink-features/.test(a)), [], `blink-feature switches in ${args}`);
  }
  assert.deepEqual(
    buildFingerprintArgs(id, {headless: true, directNetwork: true, timezone: 'Asia/Shanghai', lang: 'zh-CN'}),
    [
      '--disable-spoofing=audio,clientrects',
      '--fingerprint=123457',
      '--fingerprint-platform=macos',
      '--fingerprint-platform-version=15.3.1',
      '--fingerprint-brand=Chrome',
      '--fingerprint-hardware-concurrency=10',
      '--window-size=1440,860',
      '--window-position=36,45',
      '--screen-info={3024x1964 workAreaTop=74 workAreaBottom=140 devicePixelRatio=2}',
      '--timezone=Asia/Shanghai',
      '--lang=zh-CN',
      '--no-proxy-server',
    ],
  );
  assert.deepEqual(
    buildFingerprintArgs(id, {headless: false, proxy: 'http://127.0.0.1:7890'}),
    [
      '--disable-spoofing=audio,clientrects',
      '--fingerprint=123457',
      '--fingerprint-platform=macos',
      '--fingerprint-platform-version=15.3.1',
      '--fingerprint-brand=Chrome',
      '--fingerprint-hardware-concurrency=10',
      '--window-size=1440,860',
      '--webrtc-ip-handling-policy=disable_non_proxied_udp',
    ],
  );
  assert.deepEqual(
    buildFingerprintArgs(id, {headless: false, proxy: '   '}),
    [
      '--disable-spoofing=audio,clientrects',
      '--fingerprint=123457',
      '--fingerprint-platform=macos',
      '--fingerprint-platform-version=15.3.1',
      '--fingerprint-brand=Chrome',
      '--fingerprint-hardware-concurrency=10',
      '--window-size=1440,860',
    ],
  );
});

test('ensureFingerprintIdentity handles concurrent same-profile and sibling-profile startup without EEXIST or diversity loss', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-conc-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));

  const identityModuleUrl = new URL('./fingerprint-identity.mjs', import.meta.url).href;
  const workerFile = path.join(root, 'worker.mjs');
  fs.writeFileSync(workerFile, `
    import {ensureFingerprintIdentity} from ${JSON.stringify(identityModuleUrl)};
    const [profile, hostOS, collisionRoot] = process.argv.slice(2);
    const id = ensureFingerprintIdentity(profile, {hostOS, collisionRoots: collisionRoot ? [collisionRoot] : []});
    process.stdout.write(JSON.stringify(id) + '\\n');
  `);
  const driverDir = path.dirname(fileURLToPath(import.meta.url));
  const runWorker = args => new Promise((resolve, reject) => {
    const child = spawn(process.execPath, [workerFile, ...args], {
      cwd: driverDir,
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    let out = '', err = '';
    child.stdout.on('data', d => { out += d; });
    child.stderr.on('data', d => { err += d; });
    child.on('error', reject);
    child.on('exit', code => {
      if (code !== 0) reject(new Error(`worker exit ${code}: ${err.trim()}`));
      else resolve(JSON.parse(out.trim()));
    });
  });

  // 6 concurrent processes racing to create the same fresh profile must all converge cleanly.
  const sameRoot = path.join(root, 'same');
  const sameProfile = path.join(sameRoot, 'account-1');
  const sameResults = await Promise.all(
    Array.from({length: 6}, () => runWorker([sameProfile, 'macos', sameRoot])),
  );
  for (const item of sameResults) {
    assert.deepEqual(item, sameResults[0]);
  }

  // 6 concurrent sibling processes must serialize diversity cleanly across all buckets.
  const sibRoot = path.join(root, 'siblings');
  const sibResults = await Promise.all(
    Array.from({length: 6}, (_, i) => runWorker([path.join(sibRoot, `acc-${i}`), 'macos', sibRoot])),
  );
  assert.equal(new Set(sibResults.map(p => p.seed)).size, 6);
  assert.equal(new Set(sibResults.map(p => p.seed % 2)).size, 2);
  assert.equal(new Set(sibResults.map(p => p.seed % 3)).size, 3);
  assert.equal(new Set(sibResults.map(p => p.seed % 4)).size, 4);
  assert.equal(new Set(sibResults.map(p => p.platformVersion)).size, 6);
  for (const item of sibResults) assertMacHardware(item);
  assert.ok(new Set(sibResults.map(p => p.hardwareConcurrency)).size >= 4, 'macOS accounts must cover M4 and M2 core counts');
  assert.equal(new Set(sibResults.map(p => `${p.screen.width}x${p.screen.height}@${p.screen.dpr}`)).size, 6);
});

// deviceMemory is {8, 16, 32}[seed % 3] and the GPU follows seed parity, so the
// core count must be a model that ships with that much memory.
function assertMacHardware(id) {
  if (id.seed % 2 === 0) {
    assert.ok(id.platformVersion.startsWith('15.'), `Apple M4 seed must use macOS 15.x Sequoia, got ${id.platformVersion}`);
    assert.ok([10, 14, 16].includes(id.hardwareConcurrency), `Apple M4 seed cores mismatch: ${id.hardwareConcurrency}`);
    assert.notEqual(id.seed % 3, 0, `Apple M4 has no 8 GiB model, got memory bucket ${id.seed % 3}`);
    return;
  }
  assert.ok([8, 10, 12].includes(id.hardwareConcurrency), `Apple M2 seed cores mismatch: ${id.hardwareConcurrency}`);
  if (id.seed % 3 === 0) {
    assert.equal(id.hardwareConcurrency, 8, `an 8 GiB Apple M2 is the 8-core model, got ${id.hardwareConcurrency} cores`);
  } else if (id.seed % 3 === 2) {
    assert.ok([10, 12].includes(id.hardwareConcurrency), `a 32 GiB Apple M2 is a 10/12-core model, got ${id.hardwareConcurrency} cores`);
  }
}

test('ensureFingerprintIdentity generates macOS seeds whose GPU, memory and core counts describe a real Mac', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-mac-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));
  for (let i = 0; i < 24; i++) {
    assertMacHardware(ensureFingerprintIdentity(path.join(root, `acc-${i}`), {hostOS: 'macos'}));
  }
  // Existing identity files keep their own core count; only new accounts follow the rule.
  const legacyDir = path.join(root, 'legacy');
  fs.mkdirSync(legacyDir, {recursive: true});
  const legacy = {
    version: 1, seed: 6, platform: 'macos', brand: 'Chrome', hardwareConcurrency: 16,
    screen: {width: 1512, height: 982, availTop: 37, availBottom: 70, dpr: 2, windowWidth: 1440, windowHeight: 860},
  };
  fs.writeFileSync(path.join(legacyDir, FINGERPRINT_IDENTITY_FILE), JSON.stringify(legacy));
  assert.deepEqual(ensureFingerprintIdentity(legacyDir, {hostOS: 'macos'}), legacy);
});

test('host display records round-trip next to the account profiles and reject unusable values', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-fp-host-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));
  const profile = path.join(root, 'Fingerprint', 'account-1');
  fs.mkdirSync(profile, {recursive: true});

  assert.equal(readHostDisplay(profile), null, 'no record yet');
  const display = {
    width: 1728, height: 1117, availLeft: 0, availTop: 37, availRight: 0, availBottom: 70,
    dpr: 2, colorDepth: 30, p3: true, hdr: false, extended: false,
  };
  assert.equal(writeHostDisplay(profile, display), true);
  assert.equal(fs.existsSync(path.join(root, 'Fingerprint', HOST_DISPLAY_FILE)), true);
  assert.deepEqual(readHostDisplay(path.join(root, 'Fingerprint', 'account-2')), display, 'sibling accounts share the record');

  for (const broken of [
    null,
    {...display, width: 100},
    {...display, availTop: display.height},
    {...display, dpr: 0},
    {...display, dpr: '2'},
    {...display, colorDepth: 0},
    {...display, colorDepth: 24.5},
    {...display, availLeft: -1},
  ]) {
    assert.equal(validateHostDisplay(broken), null, `accepted unusable display: ${JSON.stringify(broken)}`);
  }
  assert.deepEqual(validateHostDisplay({...display, p3: 'yes', extended: 1}), {...display, p3: false, extended: false});
  assert.equal(writeHostDisplay(profile, {...display, width: 0}), false, 'an unusable display is never written');

  fs.writeFileSync(path.join(root, 'Fingerprint', HOST_DISPLAY_FILE), '{broken');
  assert.equal(readHostDisplay(profile), null, 'a damaged record falls back to the account preset');
});

test('buildFingerprintArgs replays the recorded host display and keeps the window on its work area', () => {
  const id = validateFingerprintIdentity({
    version: 1,
    seed: 123458,
    platform: 'macos',
    platformVersion: '15.3.1',
    brand: 'Chrome',
    hardwareConcurrency: 10,
    screen: {width: 2560, height: 1440, availTop: 25, availBottom: 70, dpr: 2, windowWidth: 1680, windowHeight: 1020, windowX: 700, windowY: 300},
  });
  const display = {
    width: 1440, height: 900, availLeft: 0, availTop: 25, availRight: 0, availBottom: 60,
    dpr: 2, colorDepth: 30, p3: true, hdr: true, extended: true,
  };
  // The host display is smaller than the account preset: the window shrinks to fit.
  const headless = buildFingerprintArgs(id, {headless: true, display});
  assert.ok(headless.includes('--window-size=1440,815'), `window not clamped to the work area: ${headless}`);
  assert.ok(headless.includes('--window-position=0,25'), `window position not clamped: ${headless}`);
  assert.ok(headless.includes('--screen-info={2880x1800 workAreaTop=50 workAreaBottom=120 devicePixelRatio=2 colorDepth=30}{1920x1080 isInternal=false}'), `missing host display: ${headless}`);
  assert.ok(headless.includes('--force-color-profile=hdr10'), `missing HDR profile: ${headless}`);

  const headed = buildFingerprintArgs(id, {headless: false, display});
  assert.ok(headed.includes('--window-size=1440,815'), `headed window not clamped: ${headed}`);
  assert.ok(headed.includes('--window-position=0,25'), `headed window position not clamped: ${headed}`);
  assert.equal(headed.some(a => a.startsWith('--screen-info')), false);
  assert.equal(headed.some(a => a.startsWith('--force-color-profile')), false);

  // Without a record the account preset is replayed with the defaults of a plain panel.
  const preset = buildFingerprintArgs(id, {headless: true});
  assert.ok(preset.includes('--window-size=1680,1020'), `preset window: ${preset}`);
  assert.ok(preset.includes('--window-position=700,300'), `preset position: ${preset}`);
  assert.ok(preset.includes('--screen-info={5120x2880 workAreaTop=50 workAreaBottom=140 devicePixelRatio=2}'), `preset display: ${preset}`);
  assert.equal(preset.some(a => a.startsWith('--force-color-profile')), false);

  // An unusable record is ignored rather than trusted.
  assert.deepEqual(buildFingerprintArgs(id, {headless: true, display: {width: -1}}), preset);
  // Only an HDR display gets a forced profile: the P3 one recolors screenshots.
  const p3Only = buildFingerprintArgs(id, {headless: true, display: {...display, hdr: false}}).filter(a => !a.includes('--screen-info'));
  const plain = buildFingerprintArgs(id, {headless: true, display: {...display, hdr: false, p3: false}}).filter(a => !a.includes('--screen-info'));
  assert.equal(p3Only.some(a => a.startsWith('--force-color-profile')), false);
  assert.deepEqual(p3Only, plain);
});

test('emulatedMedia answers the P3 query for a wide-gamut host that is not HDR, in headless pages only', () => {
  const hdr = {width: 1512, height: 982, availLeft: 0, availTop: 37, availRight: 0, availBottom: 70, dpr: 2, colorDepth: 30, p3: true, hdr: true};
  const p3 = {...hdr, hdr: false};
  const srgb = {...hdr, p3: false};
  // An HDR host is covered by its hdr10 profile; a plain display needs nothing.
  assert.deepEqual(emulatedMedia(p3, true), [{name: 'color-gamut', value: 'p3'}]);
  assert.deepEqual(emulatedMedia(hdr, true), []);
  assert.deepEqual(emulatedMedia(srgb, true), []);
  // A headed window shows the real display, and an unusable record is no record.
  assert.deepEqual(emulatedMedia(p3, false), []);
  assert.deepEqual(emulatedMedia(null, true), []);
  assert.deepEqual(emulatedMedia({p3: true}, true), []);
});
