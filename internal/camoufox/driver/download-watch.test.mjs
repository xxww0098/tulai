import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import { PassThrough } from 'node:stream';
import test from 'node:test';
import { downloadKernel, watchDownload } from './download-watch.mjs';

test('stalled downloads fail so the installer can try another source', async () => {
  await assert.rejects(watchDownload(() => new Promise(() => {}), 25), /没有进度/);
});

test('progress extends the deadline and a completed transfer stops the timer', async () => {
  const result = await watchDownload(async progress => {
    for (let i = 0; i < 3; i++) {
      await new Promise(resolve => setTimeout(resolve, 15));
      progress();
    }
    return 'done';
  }, 80);
  assert.equal(result, 'done');
});

function collectWritable() {
  const stream = new PassThrough();
  const chunks = [];
  stream.on('data', chunk => chunks.push(chunk));
  const done = new Promise((resolve, reject) => {
    stream.on('finish', () => resolve(Buffer.concat(chunks)));
    stream.on('error', reject);
  });
  return { stream, done };
}

function makeRangeResponse(payload, start, end, { chunkSize = 4096, delayMs = 0, failAtByte = -1 } = {}) {
  const slice = payload.subarray(start, end + 1);
  let offset = 0;
  const body = new ReadableStream({
    async pull(controller) {
      if (delayMs > 0) await new Promise(r => setTimeout(r, delayMs));
      if (failAtByte >= 0 && offset >= failAtByte) {
        controller.error(new Error('simulated stream drop'));
        return;
      }
      if (offset >= slice.length) {
        controller.close();
        return;
      }
      const next = slice.subarray(offset, Math.min(slice.length, offset + chunkSize));
      offset += next.length;
      controller.enqueue(next);
    },
  });
  return new Response(body, {
    status: 206,
    headers: { 'content-range': `bytes ${start}-${end}/${payload.length}` },
  });
}

function makeFullResponse(payload, { chunkSize = 4096, delayMs = 0, failAtByte = -1 } = {}) {
  let offset = 0;
  const body = new ReadableStream({
    async pull(controller) {
      if (delayMs > 0) await new Promise(r => setTimeout(r, delayMs));
      if (failAtByte >= 0 && offset >= failAtByte) {
        controller.error(new Error('simulated 200 drop'));
        return;
      }
      if (offset >= payload.length) {
        controller.close();
        return;
      }
      const next = payload.subarray(offset, Math.min(payload.length, offset + chunkSize));
      offset += next.length;
      controller.enqueue(next);
    },
  });
  return new Response(body, {
    status: 200,
    headers: { 'content-length': String(payload.length) },
  });
}

test('downloadKernel picks fast Range mirror over throttled source and assembles chunks in order', async () => {
  const payload = crypto.randomBytes(200 * 1024 + 137);
  const counts = { slow: 0, fast: 0 };
  const fetchImpl = async (url, init = {}) => {
    const rangeHeader = init.headers?.Range ?? '';
    const match = /^bytes=(\d+)-(\d+)$/.exec(rangeHeader);
    const start = match ? Number(match[1]) : 0;
    const end = match ? Math.min(payload.length - 1, Number(match[2])) : payload.length - 1;
    if (url === 'https://slow.test/kernel.zip') {
      counts.slow++;
      return makeRangeResponse(payload, start, end, { chunkSize: 1024, delayMs: 25 });
    }
    if (url === 'https://fast.test/kernel.zip') {
      counts.fast++;
      return makeRangeResponse(payload, start, end, { chunkSize: 8192, delayMs: 0 });
    }
    throw new Error('unexpected url: ' + url);
  };

  const { stream, done } = collectWritable();
  const events = [];
  await downloadKernel(stream, ['https://slow.test/kernel.zip', 'https://fast.test/kernel.zip'], {
    probeBytes: 16 * 1024,
    chunkSize: 32 * 1024,
    concurrency: 4,
    probeWindowMs: 120,
    fetchImpl,
    onProgress: (d, t) => events.push([d, t]),
  });
  stream.end();
  const actual = await done;
  assert.equal(Buffer.compare(actual, payload), 0);
  assert.equal(counts.slow, 1, 'slow source should only be touched during initial probe');
  assert.ok(counts.fast >= 2, 'fast source should serve remaining parallel chunks');
  assert.deepEqual(events.at(-1), [payload.length, payload.length]);
  for (let i = 1; i < events.length; i++) {
    assert.ok(events[i][0] >= events[i - 1][0], 'progress must be monotonic');
  }
});

test('downloadKernel streams fast 200 OK mirror and resumes mid-transfer via 206 fallback on drop', async () => {
  const payload = crypto.randomBytes(128 * 1024 + 55);
  let fallbackUsed = false;
  const fetchImpl = async (url, init = {}) => {
    if (url === 'https://ghproxy.test/kernel.zip') {
      // Ignores Range and drops halfway through.
      return makeFullResponse(payload, { chunkSize: 8192, failAtByte: 48 * 1024 });
    }
    if (url === 'https://fallback.test/kernel.zip') {
      const match = /^bytes=(\d+)-(\d+)$/.exec(init.headers?.Range ?? '');
      const start = match ? Number(match[1]) : 0;
      const end = match ? Math.min(payload.length - 1, Number(match[2])) : payload.length - 1;
      if (start === 0) {
        // Make fallback slower on initial probe so 200 mirror wins first.
        return makeRangeResponse(payload, start, end, { chunkSize: 2048, delayMs: 30 });
      }
      fallbackUsed = true;
      return makeRangeResponse(payload, start, end, { chunkSize: 8192, delayMs: 0 });
    }
    throw new Error('unexpected url: ' + url);
  };

  const { stream, done } = collectWritable();
  await downloadKernel(stream, ['https://ghproxy.test/kernel.zip', 'https://fallback.test/kernel.zip'], {
    probeBytes: 16 * 1024,
    chunkSize: 32 * 1024,
    concurrency: 3,
    probeWindowMs: 80,
    fetchImpl,
  });
  stream.end();
  const actual = await done;
  assert.equal(fallbackUsed, true);
  assert.equal(Buffer.compare(actual, payload), 0);
});

test('downloadKernel fails over individual chunks when primary Range source starts failing', async () => {
  const payload = crypto.randomBytes(150 * 1024);
  let primaryCalls = 0;
  let backupCalls = 0;
  const fetchImpl = async (url, init = {}) => {
    const match = /^bytes=(\d+)-(\d+)$/.exec(init.headers?.Range ?? '');
    const start = Number(match[1]);
    const end = Math.min(payload.length - 1, Number(match[2]));
    if (url === 'https://primary.test/kernel.zip') {
      primaryCalls++;
      if (primaryCalls <= 2) {
        return makeRangeResponse(payload, start, end, { chunkSize: 8192 });
      }
      return new Response('', { status: 503, statusText: 'Service Unavailable' });
    }
    if (url === 'https://backup.test/kernel.zip') {
      backupCalls++;
      return makeRangeResponse(payload, start, end, { chunkSize: 8192, delayMs: start === 0 ? 30 : 0 });
    }
    throw new Error('unexpected url: ' + url);
  };

  const { stream, done } = collectWritable();
  await downloadKernel(stream, ['https://primary.test/kernel.zip', 'https://backup.test/kernel.zip'], {
    probeBytes: 16 * 1024,
    chunkSize: 32 * 1024,
    concurrency: 3,
    probeWindowMs: 80,
    fetchImpl,
  });
  stream.end();
  const actual = await done;
  assert.equal(Buffer.compare(actual, payload), 0);
  assert.ok(backupCalls >= 1);
});

test('downloadKernel completes tiny files smaller than probeBytes', async () => {
  const payload = Buffer.from('tiny-camoufox-payload');
  const fetchImpl = async () => makeFullResponse(payload, { chunkSize: 64 });
  const { stream, done } = collectWritable();
  const events = [];
  await downloadKernel(stream, ['https://single.test/kernel.zip'], {
    probeBytes: 64 * 1024,
    fetchImpl,
    onProgress: (d, t) => events.push([d, t]),
  });
  stream.end();
  assert.equal(Buffer.compare(await done, payload), 0);
  assert.deepEqual(events.at(-1), [payload.length, payload.length]);
});
