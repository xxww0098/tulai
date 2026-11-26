import { isMyGo, runtime } from "mygo-runtime";

let cachedMediaOrigin = "";

export function setMediaOrigin(origin: string | undefined): void {
  if (origin && origin.trim()) {
    cachedMediaOrigin = origin.replace(/\/+$/, "");
  }
}

export function getMediaOrigin(): string {
  if (cachedMediaOrigin) return cachedMediaOrigin;
  try {
    if (isMyGo() && runtime().platform === "win32") {
      return "http://tulaimedia.localhost";
    }
  } catch {
    // Running outside MyGo (e.g. Vite browser preview or Vitest)
  }
  return "tulaimedia://localhost";
}

function isMediaFileUrl(raw: string): boolean {
  return (
    (raw.startsWith("tulaimedia://") || raw.startsWith("http://tulaimedia.localhost")) &&
    raw.includes("/file?")
  );
}

export interface AssetUrlOptions {
  /** Requested thumbnail max width in px (<=0 or omitted keeps original / full-res). */
  width?: number;
  /** Optional cache-buster version param override. */
  version?: number | string;
}

/**
 * Normalizes a Go-returned media URL, optionally appending `&w=<px>` for cached
 * resized thumbnails (<150 KiB, safe under Windows WebView2's 1 MiB stream limit).
 */
export function toAssetUrl(
  raw?: string | null,
  options?: AssetUrlOptions,
): string {
  if (!raw) return "";
  const trimmed = raw.trim();
  if (!trimmed) return "";

  const width = options?.width;
  const version = options?.version;

  if ((width && width > 0) || version !== undefined) {
    if (isMediaFileUrl(trimmed)) {
      try {
        const u = new URL(trimmed);
        if (width && width > 0) {
          u.searchParams.set("w", String(Math.round(width)));
        }
        if (version !== undefined) {
          u.searchParams.set("v", String(version));
        }
        return u.toString();
      } catch {
        const sep = trimmed.includes("?") ? "&" : "?";
        const extra: string[] = [];
        if (width && width > 0) extra.push(`w=${Math.round(width)}`);
        if (version !== undefined) extra.push(`v=${encodeURIComponent(String(version))}`);
        return `${trimmed}${sep}${extra.join("&")}`;
      }
    }
  }

  return trimmed;
}

/** Thumbnail helper for grids, queues, filmstrips, and hover previews. */
export function toThumbUrl(raw?: string | null, width = 360): string {
  return toAssetUrl(raw, { width });
}

/** Full-resolution helper for Lightbox and ImageEdit (strips any `w` param). */
export function toFullUrl(raw?: string | null): string {
  if (!raw) return "";
  const trimmed = raw.trim();
  if (!trimmed) return "";
  if (isMediaFileUrl(trimmed) && trimmed.includes("w=")) {
    try {
      const u = new URL(trimmed);
      u.searchParams.delete("w");
      return u.toString();
    } catch {
      return trimmed;
    }
  }
  return trimmed;
}

/** Live CDP drawer frame URL (`<origin>/drawer/frame?v=<frameVersion>`). */
export function getDrawerFrameUrl(
  frameUrl?: string,
  frameVersion?: number,
  origin?: string,
): string {
  if (frameUrl && frameUrl.trim()) return frameUrl.trim();
  if (!frameVersion || frameVersion <= 0) return "";
  const base = (origin || getMediaOrigin()).replace(/\/+$/, "");
  return `${base}/drawer/frame?v=${frameVersion}`;
}

/** ImageEdit original stage URL (`<origin>/edit/orig?v=<origVersion>`). */
export function getEditOrigUrl(
  origUrl?: string,
  origVersion?: number,
  origin?: string,
): string {
  if (origUrl && origUrl.trim()) return origUrl.trim();
  if (!origVersion || origVersion <= 0) return "";
  const base = (origin || getMediaOrigin()).replace(/\/+$/, "");
  return `${base}/edit/orig?v=${origVersion}`;
}

/** ImageEdit processed preview URL (`<origin>/edit/preview?v=<outVersion>`). */
export function getEditPreviewUrl(
  previewUrl?: string,
  outVersion?: number,
  origin?: string,
): string {
  if (previewUrl && previewUrl.trim()) return previewUrl.trim();
  if (!outVersion || outVersion <= 0) return "";
  const base = (origin || getMediaOrigin()).replace(/\/+$/, "");
  return `${base}/edit/preview?v=${outVersion}`;
}

export function getBrandIconUrl(origin?: string): string {
  const base = (origin || getMediaOrigin()).replace(/\/+$/, "");
  return `${base}/brand-icon`;
}
