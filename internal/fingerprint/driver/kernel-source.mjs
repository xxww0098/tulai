// Resolve development assets from the same Go module the production binary embeds.
import {execFileSync} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';

const root = execFileSync('go', ['list', '-f', '{{.Module.Dir}}', 'github.com/xxww0098/realbrowser/fingerprintkernel/driver'], {
  cwd: fileURLToPath(new URL('../../../', import.meta.url)),
  encoding: 'utf8',
}).trim();

export const kernelFile = relative => pathToFileURL(path.join(root, relative));
