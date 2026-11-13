// Reject a stalled transfer so the caller can try the next configured source.
// A progressing transfer has no fixed total-time limit.
export async function watchDownload(start, timeoutMs = 30000) {
  let timer;
  let rejectStalled;
  const stalled = new Promise((_, reject) => { rejectStalled = reject; });
  const progress = () => {
    clearTimeout(timer);
    timer = setTimeout(() => rejectStalled(new Error('Camoufox 内核下载长时间没有进度')), timeoutMs);
  };
  progress();
  try {
    return await Promise.race([start(progress), stalled]);
  } finally {
    clearTimeout(timer);
  }
}
