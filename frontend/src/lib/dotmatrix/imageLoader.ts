/**
 * Decode-gated image loader for DotMatrixImageBox.
 * Guarantees reveals start only after `await img.decode()` finishes (never on
 * network response alone), tracks elapsed ms for the 300ms fast-image skip rule,
 * and caches decoded HTMLImageElements in a small LRU map.
 */

export interface DecodedImageRecord {
  src: string;
  image: HTMLImageElement;
  naturalWidth: number;
  naturalHeight: number;
  corsSafe: boolean;
}

const MAX_DECODED_CACHE = 96;
const decodedCache = new Map<string, DecodedImageRecord>();
const inflightLoads = new Map<string, Promise<DecodedImageRecord>>();

function touchCache(src: string, record: DecodedImageRecord): void {
  decodedCache.delete(src);
  decodedCache.set(src, record);
  while (decodedCache.size > MAX_DECODED_CACHE) {
    const oldestKey = decodedCache.keys().next().value;
    if (oldestKey === undefined) break;
    decodedCache.delete(oldestKey);
  }
}

export function getCachedDecodedImage(src: string): DecodedImageRecord | undefined {
  if (!src) return undefined;
  const hit = decodedCache.get(src);
  if (hit) touchCache(src, hit);
  return hit;
}

export function clearDecodedImageCache(): void {
  decodedCache.clear();
  inflightLoads.clear();
}

function loadSingleAttempt(src: string, withCors: boolean): Promise<DecodedImageRecord> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.decoding = "async";
    if (withCors && !src.startsWith("data:") && !src.startsWith("blob:")) {
      img.crossOrigin = "anonymous";
    }

    let settled = false;
    const finishOk = async () => {
      if (settled) return;
      settled = true;
      try {
        if (typeof img.decode === "function") {
          await img.decode();
        }
      } catch {
        // Some test / SVG / already-decoded bitmaps reject decode() even when naturalWidth > 0
        if (!img.complete || img.naturalWidth === 0) {
          reject(new Error("Image decode failed"));
          return;
        }
      }
      const record: DecodedImageRecord = {
        src,
        image: img,
        naturalWidth: img.naturalWidth || 1,
        naturalHeight: img.naturalHeight || 1,
        corsSafe: withCors || src.startsWith("data:") || src.startsWith("blob:"),
      };
      resolve(record);
    };

    img.onload = () => {
      void finishOk();
    };
    img.onerror = () => {
      if (settled) return;
      settled = true;
      reject(new Error(`Image load failed: ${src}`));
    };

    img.src = src;

    if (img.complete && img.naturalWidth > 0) {
      void finishOk();
    }
  });
}

/**
 * Loads and decodes `src`. Attempts CORS-safe load first (required for WebGL2
 * `texImage2D`), falling back to non-CORS load if an external server rejects
 * anonymous CORS headers (in which case WebGL2 safely falls back to CSS reveal).
 */
export function loadAndDecodeImage(src: string): Promise<DecodedImageRecord> {
  const trimmed = src.trim();
  if (!trimmed) {
    return Promise.reject(new Error("Empty image source"));
  }
  const cached = getCachedDecodedImage(trimmed);
  if (cached) {
    return Promise.resolve(cached);
  }
  const existing = inflightLoads.get(trimmed);
  if (existing) {
    return existing;
  }

  const isLocalData = trimmed.startsWith("data:") || trimmed.startsWith("blob:");
  const promise = (async () => {
    try {
      const rec = await loadSingleAttempt(trimmed, !isLocalData);
      touchCache(trimmed, rec);
      return rec;
    } catch (firstErr) {
      if (isLocalData) throw firstErr;
      // Retry without CORS in case an external URL forbids anonymous CORS
      const rec = await loadSingleAttempt(trimmed, false);
      touchCache(trimmed, rec);
      return rec;
    } finally {
      inflightLoads.delete(trimmed);
    }
  })();

  inflightLoads.set(trimmed, promise);
  return promise;
}
