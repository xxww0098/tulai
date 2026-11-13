import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {patchMozglueManifest, prepareWindowsExecutable} from './windows-compat.mjs';

const dependency = `<dependency>\n        <dependentAssembly>\n                <assemblyIdentity\n                        type="win32"\n                        name="mozglue"\n                        version="1.0.0.0"\n                        language="*"\n                />\n        </dependentAssembly>\n</dependency>`;

test('only the mozglue dependency is removed without changing binary length', () => {
  const original = Buffer.from(`prefix${dependency}suffix`);
  const result = patchMozglueManifest(original);
  assert.equal(result.length, original.length);
  assert.equal(result.toString(), `prefix${' '.repeat(dependency.length)}suffix`);
  assert.match(original.toString(), /mozglue/);
  assert.throws(() => patchMozglueManifest(Buffer.from('no dependency')));
  assert.throws(() => patchMozglueManifest(Buffer.from(dependency + dependency)));
});

test('other binaries and non-Windows hosts pass through unchanged', t => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-compat-'));
  t.after(() => fs.rmSync(dir, {recursive: true, force: true}));
  const exe = path.join(dir, 'camoufox.exe');
  fs.writeFileSync(exe, dependency);
  assert.equal(prepareWindowsExecutable(exe, 'win32'), exe);
  assert.equal(prepareWindowsExecutable(exe, 'linux'), exe);
  assert.equal(fs.readFileSync(exe, 'utf8'), dependency);
  assert.deepEqual(fs.readdirSync(dir), ['camoufox.exe']);
});
