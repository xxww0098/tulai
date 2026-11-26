// Reject a stalled transfer so the caller can try the next configured source.
// A progressing transfer has no fixed total-time limit.
export async function watchDownload(start, timeoutMs = 30000) {
  let timer;
  let rejectStalled;
  const stalled = new Promise((_, reject) => { rejectStalled = reject; });
  const progress = () => {
    clearTimeout(timer);
    timer = setTimeout(() => rejectStalled(new Error('Fingerprint 内核下载长时间没有进度')), timeoutMs);
  };
  progress();
  try {
    return await Promise.race([start(progress), stalled]);
  } finally {
    clearTimeout(timer);
  }
}

const DEFAULT_PROBE_BYTES = 2 * 1024 * 1024;
const DEFAULT_CHUNK_SIZE = 4 * 1024 * 1024;
const DEFAULT_CONCURRENCY = 6;
const DEFAULT_MAX_BUFFERED_CHUNKS = 12;
const DEFAULT_CONNECT_TIMEOUT_MS = 8000;
const DEFAULT_PROBE_WINDOW_MS = 2000;
const DEFAULT_CHUNK_STALL_MS = 12000;
const DEFAULT_SLOW_WINDOW_MS = 8000;
const DEFAULT_MIN_SPEED_BPS = 300 * 1024;
const FAST_RANGE_WIN_BPS = 3 * 1024 * 1024;

function uniqueUrls(urls) {
  const seen = new Set();
  const out = [];
  for (const raw of urls ?? []) {
    const value = String(raw ?? '').trim();
    if (!value || seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}

function parseContentRange(header) {
  const match = /^bytes\s+(\d+)-(\d+)\/(\d+|\*)$/i.exec(String(header ?? '').trim());
  if (!match) return null;
  const start = Number(match[1]);
  const end = Number(match[2]);
  const total = match[3] === '*' ? 0 : Number(match[3]);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return null;
  if (match[3] !== '*' && (!Number.isFinite(total) || total <= end)) return null;
  return { start, end, total };
}

async function writeToStream(stream, chunk) {
  if (!chunk || chunk.length === 0) return;
  if (stream.errored) throw stream.errored;
  if (!stream.write(chunk)) {
    await new Promise((resolve, reject) => {
      const cleanup = () => {
        stream.off('drain', onDrain);
        stream.off('error', onError);
      };
      const onDrain = () => { cleanup(); resolve(); };
      const onError = err => { cleanup(); reject(err); };
      stream.once('drain', onDrain);
      stream.once('error', onError);
    });
  }
}

async function flushBuffers(stream, chunks) {
  let written = 0;
  for (const chunk of chunks) {
    await writeToStream(stream, chunk);
    written += chunk.length;
  }
  return written;
}

function createProgressReporter(onProgress) {
  let maxReported = 0;
  return (done, total, force = false) => {
    if (!onProgress) return;
    const clamped = total > 0 && !force ? Math.min(done, Math.max(0, total - 1)) : done;
    if (clamped > maxReported || force || (maxReported === 0 && done === 0)) {
      maxReported = Math.max(maxReported, clamped);
      onProgress(maxReported, total);
    }
  };
}

function abortQuietly(controller, reader) {
  try { controller.abort(); } catch {}
  if (reader) {
    Promise.resolve(reader.cancel()).catch(() => {});
  }
}

async function probeSources(urls, opts) {
  const {
    probeBytes,
    connectTimeoutMs,
    probeWindowMs,
    fetchImpl,
  } = opts;

  const probes = urls.map((url, index) => ({
    url,
    index,
    controller: new AbortController(),
    reader: null,
    chunks: [],
    received: 0,
    totalSize: 0,
    supportsRange: false,
    finished: false,
    eof: false,
    error: null,
    startedAt: Date.now(),
    firstByteAt: 0,
    doneAt: 0,
    settled: false,
  }));

  let settledCount = 0;
  let firstDataAt = 0;
  let resolveDecision;
  let decided = false;
  const decisionPromise = new Promise(resolve => { resolveDecision = resolve; });

  const probeSpeed = p => {
    if (p.received <= 0) return 0;
    const end = p.doneAt || Date.now();
    const elapsedSec = Math.max((end - p.startedAt) / 1000, 0.01);
    return p.received / elapsedSec;
  };

  let windowTimer;
  const maybeDecide = () => {
    if (decided) return;
    if (settledCount === probes.length) {
      decided = true;
      clearTimeout(windowTimer);
      resolveDecision();
      return;
    }
    const fastRange = probes.find(p => p.finished && p.supportsRange && !p.error && probeSpeed(p) >= FAST_RANGE_WIN_BPS);
    if (fastRange) {
      const fasterPriorityPending = probes.some(p => p.index < fastRange.index && !p.settled && !p.error);
      if (!fasterPriorityPending) {
        decided = true;
        clearTimeout(windowTimer);
        resolveDecision();
      }
    }
  };

  const armWindowTimer = () => {
    if (windowTimer || decided || probes.length <= 1) return;
    windowTimer = setTimeout(() => {
      if (!decided) {
        decided = true;
        resolveDecision();
      }
    }, probeWindowMs);
  };

  const runProbe = async probe => {
    let stallTimer;
    const armStallTimer = () => {
      clearTimeout(stallTimer);
      stallTimer = setTimeout(() => {
        if (!decided && !probe.finished) {
          probe.error = new Error(`读取超时 (${probe.url})`);
          abortQuietly(probe.controller, probe.reader);
        }
      }, DEFAULT_CHUNK_STALL_MS);
    };
    const connectTimer = setTimeout(() => {
      if (!probe.firstByteAt) {
        probe.error = new Error(`连接超时 (${probe.url})`);
        abortQuietly(probe.controller, probe.reader);
      }
    }, connectTimeoutMs);

    try {
      const res = await fetchImpl(probe.url, {
        headers: { Range: `bytes=0-${probeBytes - 1}` },
        signal: probe.controller.signal,
      });
      if (!res.ok) {
        throw new Error(`HTTP ${res.status} ${res.statusText} (${probe.url})`);
      }
      if (!res.body) {
        throw new Error(`响应体为空 (${probe.url})`);
      }

      if (res.status === 206) {
        const range = parseContentRange(res.headers.get('content-range'));
        if (!range || range.start !== 0 || range.total <= 0) {
          throw new Error(`无效 Content-Range (${probe.url})`);
        }
        probe.supportsRange = true;
        probe.totalSize = range.total;
      } else {
        probe.supportsRange = false;
        probe.totalSize = Number.parseInt(res.headers.get('content-length') || '0', 10) || 0;
      }

      const reader = res.body.getReader();
      probe.reader = reader;
      const expectedRangeEnd = probe.supportsRange
        ? Math.min(probeBytes, probe.totalSize)
        : probeBytes;

      while (!decided) {
        const { done, value } = await reader.read();
        if (done) {
          probe.eof = true;
          probe.finished = true;
          probe.doneAt = Date.now();
          break;
        }
        if (value && value.length > 0) {
          const now = Date.now();
          if (!probe.firstByteAt) {
            probe.firstByteAt = now;
            clearTimeout(connectTimer);
            if (!firstDataAt) {
              firstDataAt = now;
              armWindowTimer();
            }
          }
          armStallTimer();
          const buf = Buffer.from(value.buffer, value.byteOffset, value.byteLength);
          probe.chunks.push(buf);
          probe.received += buf.length;
          if (probe.received >= expectedRangeEnd) {
            probe.finished = true;
            probe.doneAt = now;
            if (probe.supportsRange) {
              // Range chunk complete; release reader cleanly.
              abortQuietly(probe.controller, reader);
              probe.reader = null;
            }
            break;
          }
        }
      }
      if (!probe.firstByteAt) {
        clearTimeout(connectTimer);
        if (probe.eof) {
          probe.firstByteAt = Date.now();
        }
      }
    } catch (err) {
      if (!probe.error && !decided) {
        probe.error = err;
      }
      abortQuietly(probe.controller, probe.reader);
      probe.reader = null;
    } finally {
      clearTimeout(connectTimer);
      clearTimeout(stallTimer);
      probe.settled = true;
      settledCount++;
      maybeDecide();
    }
  };

  for (const probe of probes) {
    probe.task = runProbe(probe);
  }
  await decisionPromise;

  const usable = probes.filter(p => !p.error && (p.received > 0 || p.eof));
  if (usable.length === 0) {
    await Promise.allSettled(probes.map(p => p.task));
    const errors = probes.map(p => p.error?.message).filter(Boolean);
    throw new Error(`${opts.label}下载失败: ${errors.join('; ') || '无可用下载源'}`);
  }

  // Pick the best usable probe: prefer Range-capable sources when reasonably fast,
  // because multi-connection Range download outscales single-stream and supports resume.
  let best = usable[0];
  let bestScore = -1;
  const max200Speed = usable.reduce((max, p) => (!p.supportsRange ? Math.max(max, probeSpeed(p)) : max), 0);
  for (const p of usable) {
    const spd = probeSpeed(p);
    let score = spd;
    if (p.supportsRange && (spd >= FAST_RANGE_WIN_BPS / 2 || spd >= max200Speed * 0.5)) {
      score = spd * 2.5;
    }
    if (p.eof && p.finished) {
      score += 1e12;
    }
    if (score > bestScore) {
      bestScore = score;
      best = p;
    }
  }

  for (const p of probes) {
    if (p !== best) {
      abortQuietly(p.controller, p.reader);
      p.reader = null;
    }
  }
  if (best.supportsRange && best.reader) {
    abortQuietly(best.controller, best.reader);
    best.reader = null;
  }
  // Ensure the winning probe's read loop has exited before handing its reader to streamFullResponse.
  await best.task;

  return best;
}

async function streamFullResponse(file, probe, fallbackUrls, opts) {
  const {
    chunkStallMs,
    slowWindowMs,
    minSpeedBytesPerSec,
    reportProgress,
  } = opts;

  let downloaded = await flushBuffers(file, probe.chunks);
  probe.chunks = [];
  const totalSize = probe.totalSize;
  reportProgress(downloaded, totalSize, probe.eof);

  if (probe.eof || (totalSize > 0 && downloaded >= totalSize)) {
    reportProgress(totalSize || downloaded, totalSize || downloaded, true);
    return { completed: true, downloaded };
  }

  if (!probe.reader) {
    return { completed: false, downloaded, totalSize };
  }

  const reader = probe.reader;
  let lastDataAt = Date.now();
  let windowStartAt = Date.now();
  let windowStartBytes = downloaded;

  while (true) {
    let stallTimer;
    const readPromise = reader.read();
    const timeoutPromise = new Promise((_, reject) => {
      stallTimer = setTimeout(() => reject(new Error(`${opts.label}下载长时间没有进度`)), chunkStallMs);
    });
    let result;
    try {
      result = await Promise.race([readPromise, timeoutPromise]);
    } catch (err) {
      abortQuietly(probe.controller, reader);
      return { completed: false, downloaded, totalSize, error: err };
    } finally {
      clearTimeout(stallTimer);
    }

    if (result.done) {
      if (totalSize > 0 && downloaded < totalSize) {
        return { completed: false, downloaded, totalSize, error: new Error(`下载不完整 (${downloaded}/${totalSize})`) };
      }
      reportProgress(totalSize || downloaded, totalSize || downloaded, true);
      return { completed: true, downloaded };
    }

    const value = result.value;
    if (value && value.length > 0) {
      const buf = Buffer.from(value.buffer, value.byteOffset, value.byteLength);
      await writeToStream(file, buf);
      downloaded += buf.length;
      const now = Date.now();
      lastDataAt = now;
      reportProgress(downloaded, totalSize);

      if (fallbackUrls.length > 0 && totalSize > 0 && now - windowStartAt >= slowWindowMs) {
        const bps = (downloaded - windowStartBytes) / ((now - windowStartAt) / 1000);
        windowStartAt = now;
        windowStartBytes = downloaded;
        if (bps < minSpeedBytesPerSec && await hasRangeFallback(fallbackUrls, downloaded, opts)) {
          abortQuietly(probe.controller, reader);
          return { completed: false, downloaded, totalSize, slow: true };
        }
      }
    }
    void lastDataAt;
  }
}

async function hasRangeFallback(urls, offset, opts) {
  for (const url of urls) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), opts.connectTimeoutMs);
    try {
      const res = await opts.fetchImpl(url, {
        headers: { Range: `bytes=${offset}-${offset}` },
        signal: controller.signal,
      });
      const range = res.status === 206 ? parseContentRange(res.headers.get('content-range')) : null;
      abortQuietly(controller, res.body?.getReader?.() ?? null);
      if (range && range.start === offset) return true;
    } catch {
      abortQuietly(controller, null);
    } finally {
      clearTimeout(timer);
    }
  }
  return false;
}

async function downloadRanges(file, candidateUrls, startOffset, totalSize, opts) {
  const {
    chunkSize,
    concurrency,
    maxBufferedChunks,
    chunkStallMs,
    slowWindowMs,
    minSpeedBytesPerSec,
    fetchImpl,
    reportProgress,
  } = opts;

  const chunks = [];
  for (let start = startOffset, index = 0; start < totalSize; start += chunkSize, index++) {
    const end = Math.min(totalSize - 1, start + chunkSize - 1);
    chunks.push({ index, start, end, attempts: 0 });
  }
  if (chunks.length === 0) {
    reportProgress(totalSize, totalSize, true);
    return;
  }

  let activeUrlIndex = 0;
  const nonRangeUrls = new Set();
  const sourceFailures = new Map();
  let nextToAssign = 0;
  let nextToWrite = 0;
  const retryQueue = [];
  const completed = new Map();
  const inFlightBytes = new Map();
  let flushedBytes = startOffset;
  let flushWaiters = [];
  let flushing = false;
  let fatalError = null;
  let totalWindowBytes = 0;
  let windowStartAt = Date.now();

  const notifyFlush = () => {
    const waiters = flushWaiters;
    flushWaiters = [];
    for (const resolve of waiters) resolve();
  };

  const currentProgressBytes = () => {
    let sum = flushedBytes;
    for (const buf of completed.values()) sum += buf.length;
    for (const n of inFlightBytes.values()) sum += n;
    return sum;
  };

  const flushCompleted = async () => {
    if (flushing) return;
    flushing = true;
    try {
      while (!fatalError && completed.has(nextToWrite)) {
        const buf = completed.get(nextToWrite);
        completed.delete(nextToWrite);
        await writeToStream(file, buf);
        flushedBytes += buf.length;
        nextToWrite++;
        notifyFlush();
      }
    } catch (err) {
      fatalError = err;
      notifyFlush();
    } finally {
      flushing = false;
    }
  };

  const pickActiveUrl = () => {
    for (let offset = 0; offset < candidateUrls.length; offset++) {
      const idx = (activeUrlIndex + offset) % candidateUrls.length;
      const candidate = candidateUrls[idx];
      if (!nonRangeUrls.has(candidate)) {
        activeUrlIndex = idx;
        return candidate;
      }
    }
    return null;
  };

  const rotateUrl = (failedUrl, isNonRange = false) => {
    if (isNonRange) nonRangeUrls.add(failedUrl);
    if (candidateUrls[activeUrlIndex] === failedUrl) {
      for (let step = 1; step < candidateUrls.length; step++) {
        const nextIdx = (activeUrlIndex + step) % candidateUrls.length;
        if (!nonRangeUrls.has(candidateUrls[nextIdx])) {
          activeUrlIndex = nextIdx;
          return;
        }
      }
    }
  };

  const nextChunk = async () => {
    while (!fatalError) {
      if (retryQueue.length > 0) {
        const candidate = retryQueue[0];
        if (candidate.index - nextToWrite < maxBufferedChunks) {
          retryQueue.shift();
          return candidate;
        }
      } else if (nextToAssign < chunks.length) {
        const candidate = chunks[nextToAssign];
        if (candidate.index - nextToWrite < maxBufferedChunks) {
          nextToAssign++;
          return candidate;
        }
      } else {
        return null;
      }
      await new Promise(resolve => flushWaiters.push(resolve));
    }
    return null;
  };

  const requeueChunk = chunk => {
    let pos = retryQueue.length;
    for (let i = 0; i < retryQueue.length; i++) {
      if (chunk.index < retryQueue[i].index) {
        pos = i;
        break;
      }
    }
    retryQueue.splice(pos, 0, chunk);
    notifyFlush();
  };

  const fetchChunk = async (chunk, url) => {
    const controller = new AbortController();
    let stallTimer;
    let reader = null;
    const resetStall = () => {
      clearTimeout(stallTimer);
      stallTimer = setTimeout(() => abortQuietly(controller, reader), chunkStallMs);
    };
    resetStall();

    try {
      const res = await fetchImpl(url, {
        headers: { Range: `bytes=${chunk.start}-${chunk.end}` },
        signal: controller.signal,
      });
      if (res.status === 200) {
        const err = new Error(`下载源不支持分段续传 (${url})`);
        err.nonRange = true;
        throw err;
      }
      if (res.status !== 206) {
        throw new Error(`HTTP ${res.status} ${res.statusText} (${url})`);
      }
      const range = parseContentRange(res.headers.get('content-range'));
      if (!range || range.start !== chunk.start || range.end < chunk.end) {
        throw new Error(`分段范围不匹配 (${url})`);
      }
      if (!res.body) {
        throw new Error(`响应体为空 (${url})`);
      }

      reader = res.body.getReader();
      const expected = chunk.end - chunk.start + 1;
      const parts = [];
      let got = 0;

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        if (value && value.length > 0) {
          resetStall();
          const buf = Buffer.from(value.buffer, value.byteOffset, value.byteLength);
          parts.push(buf);
          got += buf.length;
          totalWindowBytes += buf.length;
          inFlightBytes.set(chunk.index, got);
          reportProgress(currentProgressBytes(), totalSize);

          const now = Date.now();
          if (now - windowStartAt >= slowWindowMs) {
            const bps = totalWindowBytes / ((now - windowStartAt) / 1000);
            windowStartAt = now;
            totalWindowBytes = 0;
            if (bps < minSpeedBytesPerSec && candidateUrls.some(u => u !== url && !nonRangeUrls.has(u))) {
              rotateUrl(url, false);
            }
          }
          if (got >= expected) break;
        }
      }

      if (got < expected) {
        throw new Error(`分段下载不完整 (${got}/${expected})`);
      }
      const assembled = parts.length === 1 ? parts[0] : Buffer.concat(parts, got);
      return assembled.length === expected ? assembled : assembled.subarray(0, expected);
    } catch (err) {
      abortQuietly(controller, reader);
      throw err;
    } finally {
      clearTimeout(stallTimer);
      abortQuietly(controller, reader);
      inFlightBytes.delete(chunk.index);
    }
  };

  const maxAttempts = Math.max(4, candidateUrls.length * 3);
  const workerCount = Math.min(concurrency, chunks.length);

  const worker = async () => {
    while (!fatalError) {
      const chunk = await nextChunk();
      if (!chunk) return;
      const url = pickActiveUrl();
      if (!url) {
        fatalError = new Error('所有下载源均不支持断点续传');
        notifyFlush();
        return;
      }
      try {
        const buf = await fetchChunk(chunk, url);
        sourceFailures.set(url, 0);
        completed.set(chunk.index, buf);
        reportProgress(currentProgressBytes(), totalSize);
        await flushCompleted();
      } catch (err) {
        chunk.attempts++;
        const fails = (sourceFailures.get(url) ?? 0) + 1;
        sourceFailures.set(url, fails);
        if (err.nonRange || fails >= 2) {
          rotateUrl(url, Boolean(err.nonRange));
        }
        if (chunk.attempts >= maxAttempts) {
          fatalError = err;
          notifyFlush();
          return;
        }
        requeueChunk(chunk);
      }
    }
  };

  await Promise.all(Array.from({ length: workerCount }, worker));
  await flushCompleted();
  if (fatalError) throw fatalError;
  if (nextToWrite !== chunks.length) {
    throw new Error(`${opts.label}下载未完整写入`);
  }
  reportProgress(totalSize, totalSize, true);
}

/**
 * Download a kernel archive from the fastest working source in `urls`, using
 * parallel Range connections when supported and seamless failover across sources.
 */
export async function downloadKernel(file, urls, options = {}) {
  const label = options.label || 'Fingerprint 内核';
  const candidates = uniqueUrls(urls);
  if (candidates.length === 0) {
    throw new Error(`缺少${label}下载地址`);
  }

  const opts = {
    label,
    probeBytes: options.probeBytes ?? DEFAULT_PROBE_BYTES,
    chunkSize: options.chunkSize ?? DEFAULT_CHUNK_SIZE,
    concurrency: options.concurrency ?? DEFAULT_CONCURRENCY,
    maxBufferedChunks: options.maxBufferedChunks ?? DEFAULT_MAX_BUFFERED_CHUNKS,
    connectTimeoutMs: options.connectTimeoutMs ?? DEFAULT_CONNECT_TIMEOUT_MS,
    probeWindowMs: options.probeWindowMs ?? DEFAULT_PROBE_WINDOW_MS,
    chunkStallMs: options.chunkStallMs ?? DEFAULT_CHUNK_STALL_MS,
    slowWindowMs: options.slowWindowMs ?? DEFAULT_SLOW_WINDOW_MS,
    minSpeedBytesPerSec: options.minSpeedBytesPerSec ?? DEFAULT_MIN_SPEED_BPS,
    fetchImpl: options.fetchImpl ?? fetch,
    reportProgress: createProgressReporter(options.onProgress),
  };

  const winner = await probeSources(candidates, opts);

  if (winner.supportsRange) {
    const written = await flushBuffers(file, winner.chunks);
    winner.chunks = [];
    const totalSize = winner.totalSize;
    opts.reportProgress(written, totalSize, written >= totalSize);
    if (written >= totalSize) {
      opts.reportProgress(totalSize, totalSize, true);
      return;
    }
    const orderedUrls = [winner.url, ...candidates.filter(u => u !== winner.url)];
    await downloadRanges(file, orderedUrls, written, totalSize, opts);
    return;
  }

  const remainingUrls = candidates.filter(u => u !== winner.url);
  const outcome = await streamFullResponse(file, winner, remainingUrls, opts);
  if (outcome.completed) return;

  if (outcome.totalSize > 0 && outcome.downloaded > 0 && outcome.downloaded < outcome.totalSize && remainingUrls.length > 0) {
    await downloadRanges(file, remainingUrls, outcome.downloaded, outcome.totalSize, opts);
    return;
  }

  throw outcome.error ?? new Error(`${label}下载中断 (${winner.url})`);
}
