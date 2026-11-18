import fs from 'node:fs';
import path from 'node:path';

// The launcher records a build as installed by writing version.json, and then
// never downloads it again: pkgman's install() reports "already installed" and
// launchPath() fails forever. A build folder whose executable is gone (an
// extraction interrupted before it finished, or a security scanner that
// quarantines the freshly unzipped camoufox.exe) therefore poisons every
// retry. These helpers find and remove exactly those folders.

// The executable the launcher looks for inside one build folder, mirroring
// pkgman.js launchPath(): macOS keeps the binary in the app bundle.
export function browserExecutable(dir, osName, launchFile) {
  if (osName === 'mac') {
    return path.resolve(dir, 'Camoufox.app', 'Contents', 'Resources', launchFile.mac ?? 'camoufox');
  }
  const name = launchFile[osName];
  return name ? path.join(dir, name) : '';
}

// Remove browsers/<repo>/<build>/ folders that hold version.json but no
// executable. Returns the removed paths relative to the cache root, which is
// how the launcher's config.json records active_version.
export function repairBrokenBuilds(browsersDir, osName, launchFile, io = fs) {
  const removed = [];
  if (!io.existsSync(browsersDir)) return removed;
  let repos;
  try {
    repos = io.readdirSync(browsersDir, {withFileTypes: true});
  } catch {
    return removed;
  }
  for (const repo of repos) {
    if (!repo.isDirectory() || repo.name.startsWith('.')) continue;
    const repoDir = path.join(browsersDir, repo.name);
    let builds;
    try {
      builds = io.readdirSync(repoDir, {withFileTypes: true});
    } catch {
      continue;
    }
    for (const build of builds) {
      if (!build.isDirectory()) continue;
      const dir = path.join(repoDir, build.name);
      // Only a folder the launcher counts as installed can block a download.
      // Partially extracted leftovers are overwritten by the next install.
      if (!io.existsSync(path.join(dir, 'version.json'))) continue;
      const executable = browserExecutable(dir, osName, launchFile);
      if (!executable || io.existsSync(executable)) continue;
      try {
        io.rmSync(dir, {recursive: true, force: true});
      } catch {
        continue;
      }
      removed.push(`browsers/${repo.name}/${build.name}`);
    }
  }
  return removed;
}

// Drop an active_version that points at a build the repair just removed, so
// the launcher resolves the next install instead of a missing folder.
export function forgetActiveVersion(configFile, removed, io = fs) {
  if (!removed.length || !io.existsSync(configFile)) return false;
  let config;
  try {
    config = JSON.parse(io.readFileSync(configFile, 'utf8'));
  } catch {
    return false;
  }
  if (!config || typeof config !== 'object' || !removed.includes(config.active_version)) return false;
  delete config.active_version;
  try {
    io.writeFileSync(configFile, JSON.stringify(config, null, 2));
  } catch {
    return false;
  }
  return true;
}

// Repair a launcher cache root (the directory holding browsers/ and
// config.json). Returns the builds that were removed.
export function repairKernel(installDir, osName, launchFile, io = fs) {
  const removed = repairBrokenBuilds(path.join(installDir, 'browsers'), osName, launchFile, io);
  if (removed.length) forgetActiveVersion(path.join(installDir, 'config.json'), removed, io);
  return removed;
}
