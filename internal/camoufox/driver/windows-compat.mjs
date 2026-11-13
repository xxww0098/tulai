import {createHash, randomUUID} from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

// Camoufox 158.0.1-beta.41 Windows x64, from the already verified pinned
// archive. Its manifest requests mozglue as an SxS assembly, which prevents
// startup on affected Windows machines even though mozglue.dll is present.
const originalHash = '4e3a3bbab88942b20b678bc1db07e635c369988b03ecd8c38f6db7da6306f050';
const compatHash = 'bb5bf32db8d41b4d86190d51a56c536da9474f1b2c1829158cd529c65b3b7ebf';
const digest = data => createHash('sha256').update(data).digest('hex');

export function patchMozglueManifest(data) {
  const text = data.toString('latin1');
  const pattern = /<dependency>\s*<dependentAssembly>\s*<assemblyIdentity\s+type="win32"\s+name="mozglue"\s+version="1\.0\.0\.0"\s+language="\*"\s*\/>\s*<\/dependentAssembly>\s*<\/dependency>/g;
  const matches = [...text.matchAll(pattern)];
  if (matches.length !== 1) throw new Error('Camoufox Windows 清单与已验证的版本不符');
  const result = Buffer.from(data);
  result.fill(0x20, matches[0].index, matches[0].index + matches[0][0].length);
  return result;
}

// Leave the official binary untouched. Only the exact pinned binary may produce
// a separate, deterministic copy; upgrades and user-supplied builds pass through.
export function prepareWindowsExecutable(executable, platform = process.platform) {
  if (platform !== 'win32' || path.basename(executable).toLowerCase() !== 'camoufox.exe') return executable;
  const data = fs.readFileSync(executable);
  if (digest(data) !== originalHash) return executable;
  const patched = patchMozglueManifest(data);
  if (digest(patched) !== compatHash) throw new Error('Camoufox Windows 兼容副本校验失败');
  const target = path.join(path.dirname(executable), `tulai-camoufox-${compatHash.slice(0, 12)}.exe`);
  try {
    if (digest(fs.readFileSync(target)) !== compatHash) throw new Error('已有 Camoufox Windows 兼容副本校验失败');
    return target;
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }
  const temporary = `${target}.${randomUUID()}.tmp`;
  try {
    fs.writeFileSync(temporary, patched, {mode: 0o700, flag: 'wx'});
    fs.renameSync(temporary, target);
  } finally {
    fs.rmSync(temporary, {force: true});
  }
  return target;
}
