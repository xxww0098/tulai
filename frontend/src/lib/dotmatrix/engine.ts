import {
  DOTS_FADE_IN_MS,
  PATTERN_INDEX,
  LOADING_PATTERNS,
  REDUCED_MOTION_FADE_MS,
  REVEAL_INDEX,
  THEME_CROSSFADE_MS,
  easeRevealCubic,
  getActiveDotMatrixParams,
  getPatternDefaults,
  useDotMatrixStore,
  type DotMatrixParams,
  type LoadingPatternId,
  type RevealEffectId,
} from "./config";
import type { DecodedImageRecord } from "./imageLoader";
import {
  DISSOLVE_MASK_FRAG,
  FULLSCREEN_VERT,
  PASS1_DOTS_FRAG,
  PASS2_COMPOSITE_FRAG,
} from "./shaders";

export type DotMatrixBoxPhase = "loading" | "revealing" | "fastFade" | "settled" | "error";

export interface DotMatrixBoxRegistration {
  id: string;
  canvas: HTMLCanvasElement;
  container: HTMLElement;
  /** Logical box dimensions (updated on resize) */
  width: number;
  height: number;
  cornerRadius: number;
  fit: "cover" | "contain";
  phase: DotMatrixBoxPhase;
  paused: boolean;
  preview: boolean;
  /** When true, overrides store pattern/reveal for this specific box */
  patternOverride?: LoadingPatternId;
  revealOverride?: RevealEffectId;
  /** Decoded image to reveal once ready */
  imageRecord: DecodedImageRecord | null;
  /** Timestamps in performance.now() ms */
  createdAt: number;
  revealStartAt: number;
  staggerDelayMs: number;
  /** Frozen breathing clock used when paused, reduced-motion, or error */
  frozenClock: number | null;
  /** Marks box dirty so even static/frozen boxes paint once on param/theme/size change */
  dirty: boolean;
  visible: boolean;
  /** Invoked when reveal or fastFade finishes so component can settle to zero GPU work */
  onSettled?: () => void;
  /** Invoked when WebGL2 cannot render this image (e.g. non-CORS external image) */
  onFallbackChange?: (useCssFallback: boolean) => void;
}

interface TextureCacheEntry {
  tex: WebGLTexture;
  width: number;
  height: number;
}

const MAX_GL_TEXTURES = 48;
const MAX_DPR = 2;

// Theme colors aligned with tulai tokens.css (neutral dots per Section 7)
const BOX_BG_LIGHT: [number, number, number] = [0.957, 0.961, 0.969]; // #F4F5F7
const BOX_BG_DARK: [number, number, number] = [0.051, 0.067, 0.094]; // #0D1118
const DOT_COL_LIGHT: [number, number, number] = [0.110, 0.110, 0.110]; // #1C1C1C
const DOT_COL_DARK: [number, number, number] = [0.949, 0.949, 0.949]; // #F2F2F2

function compileShader(
  gl: WebGL2RenderingContext,
  type: number,
  source: string,
): WebGLShader | null {
  const sh = gl.createShader(type);
  if (!sh) return null;
  gl.shaderSource(sh, source);
  gl.compileShader(sh);
  if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
    console.warn("[tulai/dotmatrix] shader compile failed:", gl.getShaderInfoLog(sh));
    gl.deleteShader(sh);
    return null;
  }
  return sh;
}

function linkProgram(
  gl: WebGL2RenderingContext,
  vertSrc: string,
  fragSrc: string,
): WebGLProgram | null {
  const vs = compileShader(gl, gl.VERTEX_SHADER, vertSrc);
  const fs = compileShader(gl, gl.FRAGMENT_SHADER, fragSrc);
  if (!vs || !fs) {
    if (vs) gl.deleteShader(vs);
    if (fs) gl.deleteShader(fs);
    return null;
  }
  const prog = gl.createProgram();
  if (!prog) {
    gl.deleteShader(vs);
    gl.deleteShader(fs);
    return null;
  }
  gl.attachShader(prog, vs);
  gl.attachShader(prog, fs);
  gl.linkProgram(prog);
  gl.deleteShader(vs);
  gl.deleteShader(fs);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
    console.warn("[tulai/dotmatrix] program link failed:", gl.getProgramInfoLog(prog));
    gl.deleteProgram(prog);
    return null;
  }
  return prog;
}

// ---- Lightweight CPU pattern evaluator for 2D Canvas fallback (Section 10) ----

function fract(x: number): number {
  return x - Math.floor(x);
}

function clamp01(x: number): number {
  return x < 0 ? 0 : x > 1 ? 1 : x;
}

function smoothstep(e0: number, e1: number, x: number): number {
  const t = clamp01((x - e0) / (e1 - e0 || 1e-6));
  return t * t * (3 - 2 * t);
}

function hash21(x: number, y: number): number {
  let px = fract(x * 127.1);
  let py = fract(y * 311.7);
  const d = px * (px + 34.53) + py * (py + 34.53);
  px += d;
  py += d;
  return fract(px * py);
}

function hash31(x: number, y: number, z: number): number {
  let px = fract(x * 127.1);
  let py = fract(y * 311.7);
  let pz = fract(z * 74.7);
  const d = px * (py + 33.33) + py * (pz + 33.33) + pz * (px + 33.33);
  px += d;
  py += d;
  pz += d;
  return fract((px + py) * pz);
}

function vnoise2(x: number, y: number): number {
  const ix = Math.floor(x);
  const iy = Math.floor(y);
  const fx = x - ix;
  const fy = y - iy;
  const ux = fx * fx * (3 - 2 * fx);
  const uy = fy * fy * (3 - 2 * fy);
  const a = hash21(ix, iy);
  const b = hash21(ix + 1, iy);
  const c = hash21(ix, iy + 1);
  const d = hash21(ix + 1, iy + 1);
  return a + (b - a) * ux + (c - a + (a - b - c + d) * ux) * uy;
}

function fbm2Cpu(x: number, y: number): number {
  let sum = 0;
  let amp = 0.5;
  let px = x;
  let py = y;
  for (let i = 0; i < 4; i++) {
    sum += amp * vnoise2(px, py);
    px *= 2;
    py *= 2;
    amp *= 0.5;
  }
  return sum / 0.9375;
}

function fbm3Cpu(x: number, y: number, z: number): number {
  const iz = Math.floor(z);
  const fz = z - iz;
  const uz = fz * fz * (3 - 2 * fz);
  const a = fbm2Cpu(x + iz * 13.7, y + iz * 7.3);
  const b = fbm2Cpu(x + (iz + 1) * 13.7, y + (iz + 1) * 7.3);
  return a + (b - a) * uz;
}

export function evaluatePatternDotCpu(
  pattern: LoadingPatternId,
  ix: number,
  iy: number,
  px: number,
  py: number,
  boxW: number,
  boxH: number,
  pitch: number,
  noiseScale: number,
  t: number,
): number {
  const cx = boxW * 0.5;
  const cy = boxH * 0.5;
  const scale = Math.max(noiseScale, 20);
  const n = fbm3Cpu(px / scale, py / scale, t * 0.32);

  switch (pattern) {
    case "ripple": {
      const r = Math.hypot(px - cx, py - cy) / scale;
      const w = 0.5 + 0.5 * Math.sin(r * 5.2 * (Math.PI * 0.5) - 2.6 * t);
      return smoothstep(0.15, 0.85, (n + (w - n) * 0.6) * (0.55 + 0.6 * n));
    }
    case "fbm":
      return smoothstep(0.28, 0.74, n);
    case "helix": {
      const unit = Math.max(boxH, 48) / 306;
      const x = (px - cx) / unit;
      const y = (py - cy) / unit;
      const phase = x / 44 - 1.8 * t;
      const s = Math.sin(phase);
      const c = Math.cos(phase);
      const y1 = 72 * s;
      const y2 = -72 * s;
      const g1 = Math.exp(-0.5 * Math.pow((y - y1) / 10, 2)) * (0.62 + 0.38 * c);
      const g2 = Math.exp(-0.5 * Math.pow((y - y2) / 10, 2)) * (0.62 - 0.38 * c);
      let rung = 0;
      if (ix % 2 === 0) {
        const lo = Math.min(y1, y2);
        const hi = Math.max(y1, y2);
        if (y > lo + 4 && y < hi - 4) {
          rung = 0.5 * smoothstep(0.12, 0.55, Math.abs(s));
        }
      }
      return clamp01(Math.max(g1, g2, rung) + 0.08 * n);
    }
    case "grain": {
      const off = hash21(ix + 5.1, iy + 11.7);
      const gt = t * 2.5 + off * 17;
      const k = Math.floor(gt);
      const frac = smoothstep(0, 1, fract(gt));
      const g0 = hash31(ix, iy, k);
      const g1 = hash31(ix, iy, k + 1);
      const grain = g0 + (g1 - g0) * frac;
      return smoothstep(0.1, 0.9, n + (grain - n) * 0.42);
    }
    case "spiral": {
      const dx = px - cx;
      const dy = py - cy;
      const r = Math.hypot(dx, dy) / scale;
      const theta = Math.atan2(dy, dx);
      const s = 0.5 + 0.5 * Math.sin(3 * theta + 5.5 * r - 2.2 * t);
      return smoothstep(0.12, 0.85, (n + (s - n) * 0.55) * (0.6 + 0.5 * n));
    }
    case "warp": {
      const qx = px / scale;
      const qy = py / scale;
      const dx = 0.22 * t;
      const dy = -0.1 * t;
      const wx = fbm2Cpu(qx * 1.1 + dx, qy * 1.1 + dy);
      const wy = fbm2Cpu(qx * 1.1 + dx + 5.2, qy * 1.1 + dy + 5.2);
      return smoothstep(0.34, 0.7, fbm2Cpu(qx + 4 * (wx - 0.5) + 1.6 * dx, qy + 4 * (wy - 0.5) + 1.6 * dy));
    }
    case "plasma": {
      const qx = (px - cx) / scale;
      const qy = (py - cy) / scale;
      const r = Math.hypot(qx, qy);
      const sum =
        Math.sin(2.6 * qx + 0.9 * t) +
        Math.sin(3.1 * qy - 1.1 * t) +
        Math.sin(2.2 * (qx + qy) + 0.7 * t) +
        Math.sin(4.4 * r - 1.5 * t);
      const base = 0.5 + 0.125 * sum;
      return smoothstep(0.2, 0.8, base + (n - base) * 0.35);
    }
    case "band": {
      const diag = (px / Math.max(boxW, 1) + py / Math.max(boxH, 1)) * 0.5;
      const sweep = fract(t / 3.3) * 1.6 - 0.3;
      const band = Math.exp(-0.5 * Math.pow((diag - sweep) / 0.15, 2));
      return clamp01(0.5 * n + band * (1 - 0.3 * n));
    }
    case "snowflakes": {
      const z = Math.max(0.27 * boxH, 11 * pitch);
      const fallY = py - 18 * t;
      const bcx = Math.floor(px / z);
      const bcy = Math.floor(fallY / z);
      let flakeVal = 0;
      for (let oy = -1; oy <= 1; oy++) {
        for (let ox = -1; ox <= 1; ox++) {
          const cidX = bcx + ox;
          const cidY = bcy + oy;
          const r1 = hash21(cidX + 1.7, cidY + 3.1);
          const r2 = hash21(cidX + 9.2, cidY + 5.4);
          const r3 = hash21(cidX + 4.8, cidY + 8.6);
          const r4 = hash21(cidX + 13.1, cidY + 2.3);
          const sway = Math.sin(t * 0.85 + r4 * Math.PI * 2) * z * 0.07;
          const fcx = (cidX + 0.22 + 0.56 * r1) * z + sway;
          const fcy = (cidY + 0.22 + 0.56 * r2) * z;
          const arm = Math.max((0.37 + 0.13 * r3) * z, 4 * pitch);
          const rot = r4 * Math.PI * 2 + (r1 - 0.5) * 0.55 * t;
          const dx = px - fcx;
          const dy = fallY - fcy;
          const dist = Math.hypot(dx, dy);
          if (dist < arm + pitch * 1.4) {
            const sector = Math.PI / 3;
            let ang = Math.atan2(dy, dx) - rot;
            ang = (((ang + sector * 0.5) % sector) + sector) % sector - sector * 0.5;
            ang = Math.abs(ang);
            const fpx = Math.cos(ang) * dist;
            const fpy = Math.sin(ang) * dist;
            const h1 = clamp01(fpx / Math.max(arm, 1));
            const dArm = Math.hypot(fpx - arm * h1, fpy);
            const bx = arm * 0.5;
            const ex = Math.cos(Math.PI / 3) * arm * 0.36;
            const ey = Math.sin(Math.PI / 3) * arm * 0.36;
            const h2 = clamp01(((fpx - bx) * ex + fpy * ey) / Math.max(ex * ex + ey * ey, 1));
            const dBranch = Math.hypot(fpx - (bx + ex * h2), fpy - ey * h2);
            const dFlake = Math.min(dArm, dBranch);
            const sw = Math.max(pitch * 0.58, 5.5);
            flakeVal = Math.max(flakeVal, Math.exp(-0.5 * (dFlake / sw) * (dFlake / sw)));
          }
        }
      }
      return clamp01(Math.max(flakeVal, 0.12 * n));
    }
  }
}

interface Pass1Uniforms {
  uPattern: WebGLUniformLocation | null;
  uBoxSize: WebGLUniformLocation | null;
  uGridOrigin: WebGLUniformLocation | null;
  uGridDims: WebGLUniformLocation | null;
  uPitch: WebGLUniformLocation | null;
  uNoiseScale: WebGLUniformLocation | null;
  uClock: WebGLUniformLocation | null;
}

interface Pass2Uniforms {
  uDotField: WebGLUniformLocation | null;
  uImage: WebGLUniformLocation | null;
  uDissolveTex: WebGLUniformLocation | null;
  uViewportOrigin: WebGLUniformLocation | null;
  uBoxSizePx: WebGLUniformLocation | null;
  uBoxSize: WebGLUniformLocation | null;
  uDpr: WebGLUniformLocation | null;
  uCornerRadius: WebGLUniformLocation | null;
  uGridOrigin: WebGLUniformLocation | null;
  uGridDims: WebGLUniformLocation | null;
  uDotTexSize: WebGLUniformLocation | null;
  uPitch: WebGLUniformLocation | null;
  uDotSizePct: WebGLUniformLocation | null;
  uNoiseScale: WebGLUniformLocation | null;
  uReveal: WebGLUniformLocation | null;
  uHasImage: WebGLUniformLocation | null;
  uRevealR: WebGLUniformLocation | null;
  uDotsFade: WebGLUniformLocation | null;
  uSimpleFade: WebGLUniformLocation | null;
  uUseSimpleFade: WebGLUniformLocation | null;
  uClock: WebGLUniformLocation | null;
  uTime: WebGLUniformLocation | null;
  uTheme: WebGLUniformLocation | null;
  uZoomInPct: WebGLUniformLocation | null;
  uBlurInPct: WebGLUniformLocation | null;
  uDispersionPct: WebGLUniformLocation | null;
  uEdgeBendPx: WebGLUniformLocation | null;
  uEdgeGlowPct: WebGLUniformLocation | null;
  uDotGlowPct: WebGLUniformLocation | null;
  uImgSize: WebGLUniformLocation | null;
  uFitMode: WebGLUniformLocation | null;
  uBoxBgLight: WebGLUniformLocation | null;
  uBoxBgDark: WebGLUniformLocation | null;
  uDotColLight: WebGLUniformLocation | null;
  uDotColDark: WebGLUniformLocation | null;
}

function copyFrame(source: HTMLCanvasElement, target: HTMLCanvasElement, width: number, height: number): void {
  if (target.width !== width || target.height !== height) {
    target.width = width;
    target.height = height;
  }
  const ctx = target.getContext("2d");
  if (!ctx) return;
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, width, height);
  ctx.drawImage(source, 0, 0, width, height, 0, 0, width, height);
}

class DotMatrixEngine {
  private boxes = new Map<string, DotMatrixBoxRegistration>();
  private observer: IntersectionObserver | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private rafId: number | null = null;
  private lastFrameMs = 0;

  // Scaled breathing clock (never jumps when breathing slider moves)
  private breathingClock = 0;
  private realClock = 0;

  // Theme crossfade state (0 = light, 1 = dark, 480ms crossfade)
  private targetDark = false;
  private themeMix = 0;
  private themeInitialized = false;
  private reducedMotion = false;

  // Shared single offscreen WebGL 2 context & resources
  private glCanvas: HTMLCanvasElement | null = null;
  private gl: WebGL2RenderingContext | null = null;
  private glAttempted = false;
  private vao: WebGLVertexArrayObject | null = null;

  private pass1Prog: WebGLProgram | null = null;
  private pass2Prog: WebGLProgram | null = null;
  private pass1Loc: Pass1Uniforms | null = null;
  private pass2Loc: Pass2Uniforms | null = null;

  // Shared Pass 1 dot-field FBO + texture (1 texel per dot)
  private dotFbo: WebGLFramebuffer | null = null;
  private dotTex: WebGLTexture | null = null;
  private dotTexW = 0;
  private dotTexH = 0;

  // Static half-res 2-octave Noise dissolve texture drawn once at startup
  private dissolveTex: WebGLTexture | null = null;
  // 1x1 dummy texture bound when no image is loaded
  private dummyTex: WebGLTexture | null = null;

  // LRU cache of mipmapped image textures
  private imageTextures = new Map<string, TextureCacheEntry>();

  // Unsubscribe handle for store changes
  private storeUnsub: (() => void) | null = null;

  constructor() {
    if (typeof window !== "undefined") {
      this.reducedMotion =
        typeof window.matchMedia === "function" &&
        window.matchMedia("(prefers-reduced-motion: reduce)").matches;

      if (typeof window.matchMedia === "function") {
        const mql = window.matchMedia("(prefers-reduced-motion: reduce)");
        const onChange = (e: MediaQueryListEvent) => {
          this.reducedMotion = e.matches;
          this.markAllDirty();
          this.wake();
        };
        if (typeof mql.addEventListener === "function") {
          mql.addEventListener("change", onChange);
        }
      }

      if (typeof ResizeObserver === "function") {
        this.resizeObserver = new ResizeObserver(entries => {
          for (const { target, contentRect } of entries) {
            const id = (target as HTMLElement).dataset.dotmatrixId;
            const box = id ? this.boxes.get(id) : undefined;
            if (!box || box.container !== target) continue;
            const width = Math.max(24, Math.round(contentRect.width));
            const height = Math.max(24, Math.round(contentRect.height));
            if (width !== box.width || height !== box.height) {
              this.updateBox(box.id, { width, height });
            }
          }
        });
      }

      if (typeof IntersectionObserver === "function") {
        this.observer = new IntersectionObserver(
          (entries) => {
            for (const entry of entries) {
              const id = (entry.target as HTMLElement).dataset.dotmatrixId;
              if (!id) continue;
              const box = this.boxes.get(id);
              if (box) {
                const nextVis = entry.isIntersecting;
                if (box.visible !== nextVis) {
                  box.visible = nextVis;
                  if (nextVis) {
                    box.dirty = true;
                    this.wake();
                  }
                }
              }
            }
          },
          { rootMargin: "48px" },
        );
      }

      this.storeUnsub = useDotMatrixStore.subscribe((next, prev) => {
        if (next.pattern !== prev.pattern || next.reveal !== prev.reveal) {
          this.markAllDirty();
          this.wake();
        }
      });
    }
  }

  public isWebGL2Ready(): boolean {
    this.ensureWebGL();
    return this.gl !== null;
  }

  public isReducedMotion(): boolean {
    return this.reducedMotion;
  }

  public setTargetDark(dark: boolean): void {
    if (!this.themeInitialized) {
      this.themeInitialized = true;
      this.targetDark = dark;
      this.themeMix = dark ? 1 : 0;
      return;
    }
    if (this.targetDark !== dark) {
      this.targetDark = dark;
      this.markAllDirty();
      this.wake();
    }
  }

  public getBreathingClock(): number {
    return this.breathingClock;
  }

  public registerBox(box: DotMatrixBoxRegistration): void {
    box.container.dataset.dotmatrixId = box.id;
    this.boxes.set(box.id, box);
    this.resizeObserver?.observe(box.container);
    if (this.observer) {
      this.observer.observe(box.container);
    } else {
      box.visible = true;
    }
    box.dirty = true;
    this.wake();
  }

  public updateBox(
    id: string,
    patch: Partial<Omit<DotMatrixBoxRegistration, "id" | "canvas" | "container">>,
  ): void {
    const box = this.boxes.get(id);
    if (!box) return;
    Object.assign(box, patch);
    box.dirty = true;
    this.wake();
  }

  public unregisterBox(id: string): void {
    const box = this.boxes.get(id);
    if (!box) return;
    if (this.observer) {
      this.observer.unobserve(box.container);
    }
    this.resizeObserver?.unobserve(box.container);
    this.boxes.delete(id);
  }

  public markAllDirty(): void {
    for (const box of this.boxes.values()) {
      box.dirty = true;
    }
  }

  public wake(): void {
    if (typeof window === "undefined" || typeof requestAnimationFrame !== "function") return;
    if (this.rafId !== null) return;
    this.lastFrameMs = performance.now();
    this.rafId = requestAnimationFrame(this.tick);
  }

  public dispose(): void {
    if (this.rafId !== null && typeof cancelAnimationFrame === "function") {
      cancelAnimationFrame(this.rafId);
      this.rafId = null;
    }
    this.resizeObserver?.disconnect();
    this.observer?.disconnect();
    this.observer = null;
    this.storeUnsub?.();
    this.storeUnsub = null;
    this.boxes.clear();
    if (this.gl) {
      for (const entry of this.imageTextures.values()) {
        this.gl.deleteTexture(entry.tex);
      }
      this.imageTextures.clear();
      if (this.dotTex) this.gl.deleteTexture(this.dotTex);
      if (this.dissolveTex) this.gl.deleteTexture(this.dissolveTex);
      if (this.dummyTex) this.gl.deleteTexture(this.dummyTex);
      if (this.dotFbo) this.gl.deleteFramebuffer(this.dotFbo);
      if (this.vao) this.gl.deleteVertexArray(this.vao);
      if (this.pass1Prog) this.gl.deleteProgram(this.pass1Prog);
      if (this.pass2Prog) this.gl.deleteProgram(this.pass2Prog);
    }
    this.gl = null;
    this.glCanvas = null;
    this.glAttempted = false;
  }

  private ensureWebGL(): WebGL2RenderingContext | null {
    if (this.glAttempted) return this.gl;
    this.glAttempted = true;
    if (typeof document === "undefined") return null;

    const canvas = document.createElement("canvas");
    canvas.width = 64;
    canvas.height = 64;

    let gl: WebGL2RenderingContext | null = null;
    try {
      gl = canvas.getContext("webgl2", {
        alpha: true,
        antialias: false,
        depth: false,
        stencil: false,
        premultipliedAlpha: true,
        preserveDrawingBuffer: true,
        powerPreference: "high-performance",
      });
    } catch {
      gl = null;
    }
    if (!gl || typeof gl.createVertexArray !== "function") {
      return null;
    }

    const pass1Prog = linkProgram(gl, FULLSCREEN_VERT, PASS1_DOTS_FRAG);
    const pass2Prog = linkProgram(gl, FULLSCREEN_VERT, PASS2_COMPOSITE_FRAG);
    const dissolveProg = linkProgram(gl, FULLSCREEN_VERT, DISSOLVE_MASK_FRAG);
    if (!pass1Prog || !pass2Prog || !dissolveProg) {
      if (pass1Prog) gl.deleteProgram(pass1Prog);
      if (pass2Prog) gl.deleteProgram(pass2Prog);
      if (dissolveProg) gl.deleteProgram(dissolveProg);
      return null;
    }

    const vao = gl.createVertexArray();
    gl.bindVertexArray(vao);

    // Precompute static half-res 2-octave Noise dissolve texture once at startup (256x256)
    const dissolveTex = gl.createTexture();
    const dissolveFbo = gl.createFramebuffer();
    if (dissolveTex && dissolveFbo) {
      gl.bindTexture(gl.TEXTURE_2D, dissolveTex);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, 256, 256, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

      gl.bindFramebuffer(gl.FRAMEBUFFER, dissolveFbo);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, dissolveTex, 0);
      gl.viewport(0, 0, 256, 256);
      gl.useProgram(dissolveProg);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      gl.deleteFramebuffer(dissolveFbo);
    }
    gl.deleteProgram(dissolveProg);

    // Dummy 1x1 transparent texture
    const dummyTex = gl.createTexture();
    if (dummyTex) {
      gl.bindTexture(gl.TEXTURE_2D, dummyTex);
      gl.texImage2D(
        gl.TEXTURE_2D,
        0,
        gl.RGBA,
        1,
        1,
        0,
        gl.RGBA,
        gl.UNSIGNED_BYTE,
        new Uint8Array([0, 0, 0, 0]),
      );
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    }

    this.glCanvas = canvas;
    this.gl = gl;
    this.vao = vao;
    this.pass1Prog = pass1Prog;
    this.pass2Prog = pass2Prog;
    this.dissolveTex = dissolveTex;
    this.dummyTex = dummyTex;
    this.dotFbo = gl.createFramebuffer();

    this.pass1Loc = {
      uPattern: gl.getUniformLocation(pass1Prog, "uPattern"),
      uBoxSize: gl.getUniformLocation(pass1Prog, "uBoxSize"),
      uGridOrigin: gl.getUniformLocation(pass1Prog, "uGridOrigin"),
      uGridDims: gl.getUniformLocation(pass1Prog, "uGridDims"),
      uPitch: gl.getUniformLocation(pass1Prog, "uPitch"),
      uNoiseScale: gl.getUniformLocation(pass1Prog, "uNoiseScale"),
      uClock: gl.getUniformLocation(pass1Prog, "uClock"),
    };

    this.pass2Loc = {
      uDotField: gl.getUniformLocation(pass2Prog, "uDotField"),
      uImage: gl.getUniformLocation(pass2Prog, "uImage"),
      uDissolveTex: gl.getUniformLocation(pass2Prog, "uDissolveTex"),
      uViewportOrigin: gl.getUniformLocation(pass2Prog, "uViewportOrigin"),
      uBoxSizePx: gl.getUniformLocation(pass2Prog, "uBoxSizePx"),
      uBoxSize: gl.getUniformLocation(pass2Prog, "uBoxSize"),
      uDpr: gl.getUniformLocation(pass2Prog, "uDpr"),
      uCornerRadius: gl.getUniformLocation(pass2Prog, "uCornerRadius"),
      uGridOrigin: gl.getUniformLocation(pass2Prog, "uGridOrigin"),
      uGridDims: gl.getUniformLocation(pass2Prog, "uGridDims"),
      uDotTexSize: gl.getUniformLocation(pass2Prog, "uDotTexSize"),
      uPitch: gl.getUniformLocation(pass2Prog, "uPitch"),
      uDotSizePct: gl.getUniformLocation(pass2Prog, "uDotSizePct"),
      uNoiseScale: gl.getUniformLocation(pass2Prog, "uNoiseScale"),
      uReveal: gl.getUniformLocation(pass2Prog, "uReveal"),
      uHasImage: gl.getUniformLocation(pass2Prog, "uHasImage"),
      uRevealR: gl.getUniformLocation(pass2Prog, "uRevealR"),
      uDotsFade: gl.getUniformLocation(pass2Prog, "uDotsFade"),
      uSimpleFade: gl.getUniformLocation(pass2Prog, "uSimpleFade"),
      uUseSimpleFade: gl.getUniformLocation(pass2Prog, "uUseSimpleFade"),
      uClock: gl.getUniformLocation(pass2Prog, "uClock"),
      uTime: gl.getUniformLocation(pass2Prog, "uTime"),
      uTheme: gl.getUniformLocation(pass2Prog, "uTheme"),
      uZoomInPct: gl.getUniformLocation(pass2Prog, "uZoomInPct"),
      uBlurInPct: gl.getUniformLocation(pass2Prog, "uBlurInPct"),
      uDispersionPct: gl.getUniformLocation(pass2Prog, "uDispersionPct"),
      uEdgeBendPx: gl.getUniformLocation(pass2Prog, "uEdgeBendPx"),
      uEdgeGlowPct: gl.getUniformLocation(pass2Prog, "uEdgeGlowPct"),
      uDotGlowPct: gl.getUniformLocation(pass2Prog, "uDotGlowPct"),
      uImgSize: gl.getUniformLocation(pass2Prog, "uImgSize"),
      uFitMode: gl.getUniformLocation(pass2Prog, "uFitMode"),
      uBoxBgLight: gl.getUniformLocation(pass2Prog, "uBoxBgLight"),
      uBoxBgDark: gl.getUniformLocation(pass2Prog, "uBoxBgDark"),
      uDotColLight: gl.getUniformLocation(pass2Prog, "uDotColLight"),
      uDotColDark: gl.getUniformLocation(pass2Prog, "uDotColDark"),
    };

    return gl;
  }

  private ensureDotFieldTexture(gl: WebGL2RenderingContext, cols: number, rows: number): void {
    const needW = Math.max(32, nextPow2(cols));
    const needH = Math.max(32, nextPow2(rows));
    if (this.dotTex && this.dotTexW >= needW && this.dotTexH >= needH) {
      return;
    }
    if (this.dotTex) gl.deleteTexture(this.dotTex);
    const tex = gl.createTexture();
    if (!tex) return;
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, needW, needH, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    this.dotTex = tex;
    this.dotTexW = needW;
    this.dotTexH = needH;
  }

  private ensureImageTexture(
    gl: WebGL2RenderingContext,
    record: DecodedImageRecord,
  ): TextureCacheEntry | null {
    if (!record.corsSafe) return null;
    const hit = this.imageTextures.get(record.src);
    if (hit) {
      this.imageTextures.delete(record.src);
      this.imageTextures.set(record.src, hit);
      return hit;
    }

    const tex = gl.createTexture();
    if (!tex) return null;
    try {
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, 0);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, record.image);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      gl.generateMipmap(gl.TEXTURE_2D);
    } catch {
      gl.deleteTexture(tex);
      return null;
    }

    const entry: TextureCacheEntry = {
      tex,
      width: record.naturalWidth || 1,
      height: record.naturalHeight || 1,
    };
    this.imageTextures.set(record.src, entry);
    while (this.imageTextures.size > MAX_GL_TEXTURES) {
      const oldest = this.imageTextures.keys().next().value;
      if (oldest === undefined) break;
      const oldEntry = this.imageTextures.get(oldest);
      if (oldEntry) gl.deleteTexture(oldEntry.tex);
      this.imageTextures.delete(oldest);
    }
    return entry;
  }

  private tick = (now: number): void => {
    this.rafId = null;
    const previousFrame = this.lastFrameMs;
    const dtMs = Math.min(64, Math.max(0, now - previousFrame));
    this.lastFrameMs = now;
    const dtSec = dtMs / 1000;

    const store = useDotMatrixStore.getState();
    const globalParams = getActiveDotMatrixParams();

    // Advance scaled breathing clock incrementally so slider adjustments never jump
    if (!this.reducedMotion) {
      this.breathingClock += dtSec * (globalParams.breathing / 100);
      this.realClock += dtSec;
    }

    // 480ms theme crossfade
    const targetTheme = this.targetDark ? 1 : 0;
    let themeAnimating = false;
    if (Math.abs(this.themeMix - targetTheme) > 0.0005) {
      const step = this.reducedMotion ? 1 : dtMs / THEME_CROSSFADE_MS;
      if (this.themeMix < targetTheme) {
        this.themeMix = Math.min(targetTheme, this.themeMix + step);
      } else {
        this.themeMix = Math.max(targetTheme, this.themeMix - step);
      }
      themeAnimating = Math.abs(this.themeMix - targetTheme) > 0.0005;
      this.markAllDirty();
    } else {
      this.themeMix = targetTheme;
    }

    const gl = this.ensureWebGL();
    let anyNeedsNextFrame = themeAnimating;
    let activeWorkstations = 0;
    for (const box of this.boxes.values()) {
      if (!box.visible || box.preview || box.paused || box.phase === "error" || box.phase === "settled") continue;
      activeWorkstations++;
      if (activeWorkstations === 2) break;
    }
    const batch = activeWorkstations > 1;
    const waitingFrames = batch ? new Map<string, HTMLCanvasElement>() : null;

    for (const box of this.boxes.values()) {
      if (!box.visible || box.width < 4 || box.height < 4) continue;

      const pattern = box.patternOverride ?? store.pattern;
      const isSettled = box.phase === "settled";
      if (isSettled) {
        box.dirty = false;
        continue;
      }

      const elapsedFadeIn = Math.min(1, Math.max(0, (now - box.createdAt) / DOTS_FADE_IN_MS));
      const dotsFade =
        this.reducedMotion || box.phase === "revealing" || box.phase === "fastFade"
          ? 1
          : elapsedFadeIn;
      const isStaticPhase =
        box.phase === "error" || box.paused || (box.phase === "loading" && this.reducedMotion);

      const frozenDots = box.phase === "loading" && isStaticPhase && previousFrame >= box.createdAt + DOTS_FADE_IN_MS;
      if (!box.dirty && (box.phase === "error" || frozenDots)) continue;
      if ((box.preview || batch) && !box.dirty) {
        const interval = 1000 / (box.phase === "loading" ? 30 : 60);
        const offset = box.preview
          ? interval * PATTERN_INDEX[pattern] / LOADING_PATTERNS.length
          : box.phase === "revealing"
            ? -(box.revealStartAt + box.staggerDelayMs)
            : 0;
        if (Math.floor((previousFrame + offset) / interval) === Math.floor((now + offset) / interval)) {
          anyNeedsNextFrame = true;
          continue;
        }
      }

      const reveal = box.revealOverride ?? store.reveal;
      const params: DotMatrixParams = getPatternDefaults(pattern);

      // Fit the preset's dot field to thumbnail-sized canvases.
      const scale = Math.min(1, Math.min(box.width, box.height) / 360);
      params.pitch = Math.max(3, params.pitch * scale);
      params.noiseScale *= scale;

      let revealR = 0;
      let simpleFade = 0;
      let useSimpleFade = false;
      let hasImage = false;
      let justSettled = false;

      if (box.phase === "loading") {
        if (!isStaticPhase || dotsFade < 1) {
          anyNeedsNextFrame = true;
        }
      } else if (box.phase === "error") {
        if (box.frozenClock === null) {
          box.frozenClock = this.breathingClock;
        }
      } else if (box.phase === "fastFade" && box.imageRecord) {
        hasImage = true;
        useSimpleFade = true;
        const t = (now - box.revealStartAt) / REDUCED_MOTION_FADE_MS;
        simpleFade = clamp01(t);
        if (t >= 1) {
          box.phase = "settled";
          justSettled = true;
        } else {
          anyNeedsNextFrame = true;
        }
      } else if (box.phase === "revealing" && box.imageRecord) {
        const sinceStart = now - box.revealStartAt;
        if (sinceStart < box.staggerDelayMs) {
          // Waiting for uneven batch stagger delay — dots keep breathing
          anyNeedsNextFrame = true;
        } else if (this.reducedMotion) {
          hasImage = true;
          useSimpleFade = true;
          const t = (sinceStart - box.staggerDelayMs) / REDUCED_MOTION_FADE_MS;
          simpleFade = clamp01(t);
          if (t >= 1) {
            box.phase = "settled";
            justSettled = true;
          } else {
            anyNeedsNextFrame = true;
          }
        } else {
          hasImage = true;
          const lin = clamp01((sinceStart - box.staggerDelayMs) / Math.max(params.revealMs, 100));
          revealR = easeRevealCubic(lin);
          if (lin >= 1) {
            revealR = 1;
            box.phase = "settled";
            justSettled = true;
          } else {
            anyNeedsNextFrame = true;
          }
        }
      } else if (box.phase === "settled" && box.imageRecord) {
        hasImage = true;
        revealR = 1;
        simpleFade = 1;
        useSimpleFade = this.reducedMotion;
      }

      if (box.paused && box.frozenClock === null) {
        box.frozenClock = this.breathingClock;
      } else if (!box.paused && box.phase !== "error") {
        box.frozenClock = null;
      }

      const boxClock = box.frozenClock ?? (
        box.patternOverride ? this.realClock * params.breathing / 100 : this.breathingClock
      );

      const imageTexture = gl && hasImage && box.imageRecord
        ? this.ensureImageTexture(gl, box.imageRecord)
        : null;
      const usedCssFallback = !gl || (hasImage && !imageTexture);
      // During a CSS reveal the native image animates over the last painted dots.
      if (!usedCssFallback || !hasImage || box.dirty) {
        const bareDots = usedCssFallback || !hasImage;
        const shareDots = waitingFrames && bareDots && !box.preview && !box.patternOverride && !isStaticPhase;
        // Match fade-in at the batch's 30 Hz cadence so tiles mounted together share their first frames too.
        const fadeSteps = DOTS_FADE_IN_MS * 30 / 1000;
        const fade = shareDots ? Math.round(dotsFade * fadeSteps) / fadeSteps : dotsFade;
        const key = shareDots ? `${box.width}:${box.height}:${box.cornerRadius}:${fade}:${usedCssFallback}` : null;
        const shared = key ? waitingFrames?.get(key) : undefined;
        if (shared) {
          copyFrame(shared, box.canvas, shared.width, shared.height);
        } else {
          if (gl && !usedCssFallback) {
            this.renderBoxWebGL(gl, box, pattern, reveal, params, boxClock, fade, hasImage,
              revealR, simpleFade, useSimpleFade, imageTexture);
          } else {
            this.renderBoxCpu2D(box, pattern, params, boxClock, fade);
          }
          if (key) waitingFrames?.set(key, box.canvas);
        }
      }

      box.onFallbackChange?.(usedCssFallback);
      box.dirty = false;

      if (justSettled) {
        box.onSettled?.();
      }
    }

    if (anyNeedsNextFrame) {
      this.rafId = requestAnimationFrame(this.tick);
    }
  };

  private renderBoxWebGL(
    gl: WebGL2RenderingContext,
    box: DotMatrixBoxRegistration,
    pattern: LoadingPatternId,
    reveal: RevealEffectId,
    params: DotMatrixParams,
    clock: number,
    dotsFade: number,
    hasImage: boolean,
    revealR: number,
    simpleFade: number,
    useSimpleFade: boolean,
    imgEntry: TextureCacheEntry | null,
  ): void {
    const glCanvas = this.glCanvas!;
    const dpr = Math.min(MAX_DPR, Math.max(1, window.devicePixelRatio || 1));
    const fbW = Math.max(1, Math.round(box.width * dpr));
    const fbH = Math.max(1, Math.round(box.height * dpr));

    if (glCanvas.width < fbW || glCanvas.height < fbH) {
      glCanvas.width = Math.max(glCanvas.width, fbW);
      glCanvas.height = Math.max(glCanvas.height, fbH);
    }

    const pitch = params.pitch;
    const cols = Math.max(2, Math.ceil(box.width / pitch));
    const rows = Math.max(2, Math.ceil(box.height / pitch));
    const gridOriginX = (box.width - cols * pitch) * 0.5;
    const gridOriginY = (box.height - rows * pitch) * 0.5;

    gl.bindVertexArray(this.vao);

    // ---- Pass 1: 1 texel per dot into offscreen dotFbo ----
    this.ensureDotFieldTexture(gl, cols, rows);
    if (this.dotFbo && this.dotTex && this.pass1Loc) {
      // Unbind dotTex from TEXTURE0 before attaching as color target to satisfy ANGLE/Metal feedback checks
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, null);
      gl.bindFramebuffer(gl.FRAMEBUFFER, this.dotFbo);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, this.dotTex, 0);
      gl.disable(gl.SCISSOR_TEST);
      gl.viewport(0, 0, cols, rows);
      gl.useProgram(this.pass1Prog);
      gl.uniform1i(this.pass1Loc.uPattern, PATTERN_INDEX[pattern]);
      gl.uniform2f(this.pass1Loc.uBoxSize, box.width, box.height);
      gl.uniform2f(this.pass1Loc.uGridOrigin, gridOriginX, gridOriginY);
      gl.uniform2f(this.pass1Loc.uGridDims, cols, rows);
      gl.uniform1f(this.pass1Loc.uPitch, pitch);
      gl.uniform1f(this.pass1Loc.uNoiseScale, params.noiseScale);
      gl.uniform1f(this.pass1Loc.uClock, clock);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
    }

    // ---- Pass 2: Full-res rounded box into TOP-LEFT (0, glH - fbH, fbW, fbH) of shared canvas ----
    const vpY = glCanvas.height - fbH;
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    gl.enable(gl.SCISSOR_TEST);
    gl.viewport(0, vpY, fbW, fbH);
    gl.scissor(0, vpY, fbW, fbH);
    gl.clearColor(0, 0, 0, 0);
    gl.clear(gl.COLOR_BUFFER_BIT);

    if (this.pass2Loc) {
      gl.useProgram(this.pass2Prog);

      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, this.dotTex);
      gl.uniform1i(this.pass2Loc.uDotField, 0);

      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, imgEntry ? imgEntry.tex : this.dummyTex);
      gl.uniform1i(this.pass2Loc.uImage, 1);

      gl.activeTexture(gl.TEXTURE2);
      gl.bindTexture(gl.TEXTURE_2D, this.dissolveTex);
      gl.uniform1i(this.pass2Loc.uDissolveTex, 2);

      gl.uniform2f(this.pass2Loc.uViewportOrigin, 0, vpY);
      gl.uniform2f(this.pass2Loc.uBoxSizePx, fbW, fbH);
      gl.uniform2f(this.pass2Loc.uBoxSize, box.width, box.height);
      gl.uniform1f(this.pass2Loc.uDpr, dpr);
      gl.uniform1f(this.pass2Loc.uCornerRadius, box.cornerRadius);

      gl.uniform2f(this.pass2Loc.uGridOrigin, gridOriginX, gridOriginY);
      gl.uniform2f(this.pass2Loc.uGridDims, cols, rows);
      gl.uniform2f(this.pass2Loc.uDotTexSize, Math.max(this.dotTexW, 1), Math.max(this.dotTexH, 1));
      gl.uniform1f(this.pass2Loc.uPitch, pitch);
      gl.uniform1f(this.pass2Loc.uDotSizePct, params.dotSize);
      gl.uniform1f(this.pass2Loc.uNoiseScale, params.noiseScale);

      gl.uniform1i(this.pass2Loc.uReveal, REVEAL_INDEX[reveal]);
      gl.uniform1f(this.pass2Loc.uHasImage, hasImage && imgEntry ? 1 : 0);
      gl.uniform1f(this.pass2Loc.uRevealR, revealR);
      gl.uniform1f(this.pass2Loc.uDotsFade, dotsFade);
      gl.uniform1f(this.pass2Loc.uSimpleFade, simpleFade);
      gl.uniform1f(this.pass2Loc.uUseSimpleFade, useSimpleFade ? 1 : 0);

      gl.uniform1f(this.pass2Loc.uClock, clock);
      gl.uniform1f(this.pass2Loc.uTime, this.realClock);
      gl.uniform1f(this.pass2Loc.uTheme, this.themeMix);

      gl.uniform1f(this.pass2Loc.uZoomInPct, params.zoomIn);
      gl.uniform1f(this.pass2Loc.uBlurInPct, params.blurIn);
      gl.uniform1f(this.pass2Loc.uDispersionPct, params.dispersion);
      gl.uniform1f(this.pass2Loc.uEdgeBendPx, params.edgeBend);
      gl.uniform1f(this.pass2Loc.uEdgeGlowPct, params.edgeGlow);
      gl.uniform1f(this.pass2Loc.uDotGlowPct, params.dotGlow);

      gl.uniform2f(
        this.pass2Loc.uImgSize,
        imgEntry ? imgEntry.width : box.width,
        imgEntry ? imgEntry.height : box.height,
      );
      gl.uniform1i(this.pass2Loc.uFitMode, box.fit === "contain" ? 1 : 0);

      gl.uniform3f(this.pass2Loc.uBoxBgLight, BOX_BG_LIGHT[0], BOX_BG_LIGHT[1], BOX_BG_LIGHT[2]);
      gl.uniform3f(this.pass2Loc.uBoxBgDark, BOX_BG_DARK[0], BOX_BG_DARK[1], BOX_BG_DARK[2]);
      gl.uniform3f(this.pass2Loc.uDotColLight, DOT_COL_LIGHT[0], DOT_COL_LIGHT[1], DOT_COL_LIGHT[2]);
      gl.uniform3f(this.pass2Loc.uDotColDark, DOT_COL_DARK[0], DOT_COL_DARK[1], DOT_COL_DARK[2]);

      gl.drawArrays(gl.TRIANGLES, 0, 3);
    }

    // Blit synchronously from top-left (0, 0, fbW, fbH) into the box's lightweight display canvas
    copyFrame(glCanvas, box.canvas, fbW, fbH);
  }

  private renderBoxCpu2D(
    box: DotMatrixBoxRegistration,
    pattern: LoadingPatternId,
    params: DotMatrixParams,
    clock: number,
    dotsFade: number,
  ): void {
    const target = box.canvas;
    const dpr = Math.min(
      MAX_DPR,
      Math.max(1, typeof window !== "undefined" ? window.devicePixelRatio || 1 : 1),
    );
    const fbW = Math.max(1, Math.round(box.width * dpr));
    const fbH = Math.max(1, Math.round(box.height * dpr));
    if (target.width !== fbW || target.height !== fbH) {
      target.width = fbW;
      target.height = fbH;
    }
    let ctx: CanvasRenderingContext2D | null = null;
    try {
      ctx = target.getContext("2d");
    } catch {
      ctx = null;
    }
    if (!ctx) return;

    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, box.width, box.height);

    const tTheme = this.themeMix;
    const bgR = Math.round((BOX_BG_LIGHT[0] + (BOX_BG_DARK[0] - BOX_BG_LIGHT[0]) * tTheme) * 255);
    const bgG = Math.round((BOX_BG_LIGHT[1] + (BOX_BG_DARK[1] - BOX_BG_LIGHT[1]) * tTheme) * 255);
    const bgB = Math.round((BOX_BG_LIGHT[2] + (BOX_BG_DARK[2] - BOX_BG_LIGHT[2]) * tTheme) * 255);
    ctx.fillStyle = `rgb(${bgR}, ${bgG}, ${bgB})`;
    ctx.fillRect(0, 0, box.width, box.height);

    const pitch = params.pitch;
    const cols = Math.max(2, Math.ceil(box.width / pitch));
    const rows = Math.max(2, Math.ceil(box.height / pitch));
    const originX = (box.width - cols * pitch) * 0.5;
    const originY = (box.height - rows * pitch) * 0.5;
    const maxRad = pitch * (params.dotSize * 0.01);

    const dotR = Math.round((DOT_COL_LIGHT[0] + (DOT_COL_DARK[0] - DOT_COL_LIGHT[0]) * tTheme) * 255);
    const dotG = Math.round((DOT_COL_LIGHT[1] + (DOT_COL_DARK[1] - DOT_COL_LIGHT[1]) * tTheme) * 255);
    const dotB = Math.round((DOT_COL_LIGHT[2] + (DOT_COL_DARK[2] - DOT_COL_LIGHT[2]) * tTheme) * 255);
    ctx.fillStyle = `rgb(${dotR}, ${dotG}, ${dotB})`;

    for (let iy = 0; iy < rows; iy++) {
      const py = originY + (iy + 0.5) * pitch;
      for (let ix = 0; ix < cols; ix++) {
        const px = originX + (ix + 0.5) * pitch;
        const v = evaluatePatternDotCpu(
          pattern,
          ix,
          iy,
          px,
          py,
          box.width,
          box.height,
          pitch,
          params.noiseScale,
          clock,
        );
        const dEdge = Math.min(px, py, box.width - px, box.height - py);
        const vig = 0.45 + 0.55 * smoothstep(8, 72, Math.max(0, dEdge));
        const rad = 0.55 + (maxRad - 0.55) * v;
        ctx.globalAlpha = (0.1 + 0.9 * v) * vig * dotsFade;
        ctx.beginPath();
        ctx.arc(px, py, rad, 0, Math.PI * 2);
        ctx.fill();
      }
    }
    ctx.globalAlpha = 1;
  }
}

function nextPow2(n: number): number {
  let p = 1;
  while (p < n) p <<= 1;
  return p;
}

let singletonEngine: DotMatrixEngine | null = null;

export function getDotMatrixEngine(): DotMatrixEngine {
  if (!singletonEngine) {
    singletonEngine = new DotMatrixEngine();
  }
  return singletonEngine;
}

export function resetDotMatrixEngineForTests(): void {
  singletonEngine?.dispose();
  singletonEngine = null;
}
