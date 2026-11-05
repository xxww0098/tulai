// Transport only: request payloads, proof, SSE interpretation and task state live in Go.
((id, request) => {
  const requests = window.__tulaiChatGPTRequests ||= Object.create(null);
  const state = requests[id] = {status: 0, headers: {}, chunks: [], buffered: 0, done: false, error: false, controller: new AbortController()};
  (async () => {
    let reader;
    try {
      const sameOrigin = new URL(request.url).origin === location.origin;
      const response = await fetch(request.url, {
        method: request.method, headers: request.headers,
        body: request.body ? Uint8Array.from(atob(request.body), c => c.charCodeAt(0)) : undefined,
        credentials: sameOrigin ? 'include' : 'omit', redirect: 'error', signal: state.controller.signal
      });
      state.status = response.status;
      state.headers = Object.fromEntries(response.headers);
      reader = response.body?.getReader();
      let total = 0;
      while (reader) {
        const {done, value} = await reader.read();
        if (done) break;
        total += value.byteLength;
        if (total > 64 * 1024 * 1024) throw Error('response too large');
        for (let start = 0; start < value.length; start += 32768) {
          while (state.buffered >= 1024 * 1024) {
            state.controller.signal.throwIfAborted();
            await new Promise(resolve => setTimeout(resolve, 25));
          }
          const part = value.subarray(start, start + 32768);
          state.chunks.push(btoa(String.fromCharCode(...part)));
          state.buffered += part.length;
        }
      }
    } catch (_) {
      state.error = true;
      await reader?.cancel().catch(() => {});
    } finally {
      reader?.releaseLock();
      state.done = true;
    }
  })();
  return true;
})
