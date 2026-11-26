export interface PointerSample {
  x: number;
  y: number;
  t: number;
}

export interface Point {
  x: number;
  y: number;
}

export interface RectBox {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/**
 * Shared timing defaults from the overlay sizing rules:
 * - Label (tooltip): 500ms open, 400ms group grace, 140ms/60ms fade
 * - Card (interactive hover card): 700ms open, 300ms close, 200ms/120ms motion
 * - Preview (large panel): 900ms open, 400ms close, 60ms slow-dwell swap, 250ms/140ms motion
 */
export const LABEL_OPEN_DELAY_MS = 500;
export const LABEL_GROUP_GRACE_MS = 400;
export const LABEL_ENTER_MS = 140;
export const LABEL_EXIT_MS = 60;

export const CARD_OPEN_DELAY_MS = 700;
export const CARD_CLOSE_DELAY_MS = 300;
export const CARD_ENTER_MS = 200;
export const CARD_EXIT_MS = 120;

export const PREVIEW_OPEN_DELAY_MS = 900;
export const PREVIEW_CLOSE_DELAY_MS = 400;
export const PREVIEW_SWAP_DWELL_MS = 60;
export const PREVIEW_ENTER_MS = 250;
export const PREVIEW_EXIT_MS = 140;
export const PREVIEW_SWAP_CROSSFADE_MS = 200;

export const TOUCH_LONG_PRESS_MS = 480;
export const POINTER_WINDOW_MS = 90;
export const POINTER_FAST_SPEED_PX_PER_MS = 0.1;

/**
 * Tracks recent pointer positions over a rolling 90ms window to distinguish
 * fast pass-through sweeps (> 0.1 px/ms) from slowing/resting intent.
 */
export class PointerVelocityTracker {
  private samples: PointerSample[] = [];
  private readonly windowMs: number;

  constructor(windowMs = POINTER_WINDOW_MS) {
    this.windowMs = windowMs;
  }

  record(x: number, y: number, now = Date.now()): void {
    this.samples.push({ x, y, t: now });
    this.prune(now);
  }

  clear(): void {
    this.samples = [];
  }

  getLastPoint(): Point | null {
    if (this.samples.length === 0) return null;
    const last = this.samples[this.samples.length - 1];
    return { x: last.x, y: last.y };
  }

  /**
   * Returns pointer speed in px/ms over the last `windowMs` (90ms).
   * Returns 0 when fewer than 2 samples exist inside the window (resting).
   */
  getSpeed(now = Date.now()): number {
    this.prune(now);
    if (this.samples.length < 2) return 0;

    let pathDist = 0;
    for (let i = 1; i < this.samples.length; i++) {
      const a = this.samples[i - 1];
      const b = this.samples[i];
      pathDist += Math.hypot(b.x - a.x, b.y - a.y);
    }

    const first = this.samples[0];
    const last = this.samples[this.samples.length - 1];
    const dt = Math.max(1, last.t - first.t);
    return pathDist / dt;
  }

  isFast(now = Date.now()): boolean {
    return this.getSpeed(now) > POINTER_FAST_SPEED_PX_PER_MS;
  }

  private prune(now: number): void {
    const cutoff = now - this.windowMs;
    while (this.samples.length > 0 && this.samples[0].t < cutoff) {
      this.samples.shift();
    }
  }
}

/**
 * Single-clock timer coordinator for an overlay kind.
 * Cancels competing timers cleanly on state change and unmount.
 */
export class OverlayTimerClock {
  private openId: ReturnType<typeof setTimeout> | undefined;
  private closeId: ReturnType<typeof setTimeout> | undefined;
  private swapId: ReturnType<typeof setTimeout> | undefined;
  private exitId: ReturnType<typeof setTimeout> | undefined;

  scheduleOpen(delayMs: number, fn: () => void): void {
    this.cancelOpen();
    this.cancelSwap();
    if (delayMs <= 0) {
      fn();
      return;
    }
    this.openId = setTimeout(() => {
      this.openId = undefined;
      fn();
    }, delayMs);
  }

  scheduleClose(delayMs: number, fn: () => void): void {
    this.cancelClose();
    if (delayMs <= 0) {
      fn();
      return;
    }
    this.closeId = setTimeout(() => {
      this.closeId = undefined;
      fn();
    }, delayMs);
  }

  scheduleSwap(delayMs: number, fn: () => void): void {
    this.cancelSwap();
    if (delayMs <= 0) {
      fn();
      return;
    }
    this.swapId = setTimeout(() => {
      this.swapId = undefined;
      fn();
    }, delayMs);
  }

  scheduleExit(delayMs: number, fn: () => void): void {
    this.cancelExit();
    if (delayMs <= 0) {
      fn();
      return;
    }
    this.exitId = setTimeout(() => {
      this.exitId = undefined;
      fn();
    }, delayMs);
  }

  cancelOpen(): void {
    if (this.openId !== undefined) {
      clearTimeout(this.openId);
      this.openId = undefined;
    }
  }

  cancelClose(): void {
    if (this.closeId !== undefined) {
      clearTimeout(this.closeId);
      this.closeId = undefined;
    }
  }

  cancelSwap(): void {
    if (this.swapId !== undefined) {
      clearTimeout(this.swapId);
      this.swapId = undefined;
    }
  }

  cancelExit(): void {
    if (this.exitId !== undefined) {
      clearTimeout(this.exitId);
      this.exitId = undefined;
    }
  }

  cancelAll(): void {
    this.cancelOpen();
    this.cancelClose();
    this.cancelSwap();
    this.cancelExit();
  }
}

function sign(p1: Point, p2: Point, p3: Point): number {
  return (p1.x - p3.x) * (p2.y - p3.y) - (p2.x - p3.x) * (p1.y - p3.y);
}

export function isPointInTriangle(pt: Point, a: Point, b: Point, c: Point): boolean {
  const d1 = sign(pt, a, b);
  const d2 = sign(pt, b, c);
  const d3 = sign(pt, c, a);
  const hasNeg = d1 < 0 || d2 < 0 || d3 < 0;
  const hasPos = d1 > 0 || d2 > 0 || d3 > 0;
  return !(hasNeg && hasPos);
}

/**
 * Builds a safe triangle from the anchor pointer point to the near vertical edge
 * of a side-docked preview/card panel so diagonal traversal across neighbor rows
 * does not steal or close the active panel.
 */
export function isPointInSafeCorridor(
  pt: Point,
  origin: Point,
  panelRect: RectBox,
  padding = 12,
): boolean {
  const top = panelRect.top - padding;
  const bottom = panelRect.bottom + padding;

  // Determine which side of origin the panel sits on
  if (origin.x <= panelRect.left) {
    return isPointInTriangle(
      pt,
      origin,
      { x: panelRect.left, y: top },
      { x: panelRect.left, y: bottom },
    );
  }
  if (origin.x >= panelRect.right) {
    return isPointInTriangle(
      pt,
      origin,
      { x: panelRect.right, y: top },
      { x: panelRect.right, y: bottom },
    );
  }
  return false;
}

export function isPointInsideRect(pt: Point, rect: RectBox, margin = 0): boolean {
  return (
    pt.x >= rect.left - margin &&
    pt.x <= rect.right + margin &&
    pt.y >= rect.top - margin &&
    pt.y <= rect.bottom + margin
  );
}

export function hasHoverPointer(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return true;
  }
  try {
    return !window.matchMedia("(hover: none)").matches;
  } catch {
    return true;
  }
}

export function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return false;
  }
  try {
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  } catch {
    return false;
  }
}
