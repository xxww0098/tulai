import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {browserExecutable, repairKernel} from './kernel-repair.mjs';

const LAUNCH_FILE = {win: 'camoufox.exe', mac: '../MacOS/camoufox', lin: 'camoufox-bin'};

function cacheRoot(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'tulai-kernel-repair-'));
  t.after(() => fs.rmSync(root, {recursive: true, force: true}));
  return root;
}

function writeBuild(root, relative, {version = true, executable = true, osName = 'win'} = {}) {
  const dir = path.join(root, relative);
  fs.mkdirSync(dir, {recursive: true});
  if (version) fs.writeFileSync(path.join(dir, 'version.json'), '{}');
  if (executable) {
    const target = browserExecutable(dir, osName, LAUNCH_FILE);
    fs.mkdirSync(path.dirname(target), {recursive: true});
    fs.writeFileSync(target, 'MZ');
  }
  return dir;
}

test('only builds the launcher counts as installed and cannot launch are removed', t => {
  const root = cacheRoot(t);
  const broken = writeBuild(root, 'browsers/official/158.0.1-beta.41-2644bd14', {executable: false});
  const healthy = writeBuild(root, 'browsers/official/158.0.1-beta.41-11111111');
  const partial = writeBuild(root, 'browsers/official/158.0.1-beta.41-22222222', {version: false, executable: false});
  const hidden = writeBuild(root, 'browsers/.staging', {executable: false});

  const removed = repairKernel(root, 'win', LAUNCH_FILE);

  assert.deepEqual(removed, ['browsers/official/158.0.1-beta.41-2644bd14']);
  assert.equal(fs.existsSync(broken), false);
  assert.equal(fs.existsSync(healthy), true);
  assert.equal(fs.existsSync(partial), true);
  assert.equal(fs.existsSync(hidden), true);
});

test('a dangling active_version is dropped and other settings survive', t => {
  const root = cacheRoot(t);
  writeBuild(root, 'browsers/official/158.0.1-beta.41-2644bd14', {executable: false});
  const configFile = path.join(root, 'config.json');
  fs.writeFileSync(configFile, JSON.stringify({
    active_version: 'browsers/official/158.0.1-beta.41-2644bd14',
    channel: 'official/beta.41',
  }, null, 2));

  repairKernel(root, 'win', LAUNCH_FILE);

  const config = JSON.parse(fs.readFileSync(configFile, 'utf8'));
  assert.equal(config.active_version, undefined);
  assert.equal(config.channel, 'official/beta.41');
});

test('an active_version that survives the repair is left alone', t => {
  const root = cacheRoot(t);
  writeBuild(root, 'browsers/official/158.0.1-beta.41-2644bd14', {executable: false});
  const configFile = path.join(root, 'config.json');
  fs.writeFileSync(configFile, JSON.stringify({active_version: 'browsers/official/other'}));

  repairKernel(root, 'win', LAUNCH_FILE);

  assert.equal(JSON.parse(fs.readFileSync(configFile, 'utf8')).active_version, 'browsers/official/other');
});

test('the macOS executable lives in the app bundle', t => {
  const root = cacheRoot(t);
  const broken = writeBuild(root, 'browsers/official/158.0.1-beta.41-33333333', {osName: 'mac', executable: false});
  writeBuild(root, 'browsers/official/158.0.1-beta.41-44444444', {osName: 'mac'});

  assert.deepEqual(repairKernel(root, 'mac', LAUNCH_FILE), ['browsers/official/158.0.1-beta.41-33333333']);
  assert.equal(fs.existsSync(broken), false);
});

test('a missing cache root is not an error', t => {
  const root = cacheRoot(t);
  assert.deepEqual(repairKernel(root, 'win', LAUNCH_FILE), []);
});
