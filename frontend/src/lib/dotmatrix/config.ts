import { create } from "zustand";

export type LoadingPatternId =
  | "ripple"
  | "fbm"
  | "helix"
  | "grain"
  | "spiral"
  | "warp"
  | "plasma"
  | "band"
  | "snowflakes";

export type RevealEffectId =
  | "fisheye"
  | "halftone"
  | "unzip"
  | "denoise"
  | "spiral"
  | "dissolve"
  | "interference"
  | "stream"
  | "cellbloom";

export interface DotMatrixParams {
  breathing: number; // % (0-300)
  revealMs: number; // ms (400-3000)
  pitch: number; // px (12-36)
  dotSize: number; // % of pitch (5-30)
  noiseScale: number; // px (80-360)
  zoomIn: number; // % (0-30)
  blurIn: number; // % (0-100; 50% = 3.0 mip levels)
  dispersion: number; // % (0-100)
  edgeBend: number; // px (0-20)
  edgeGlow: number; // % (0-100, dark only)
  dotGlow: number; // % (0-100, dark only)
}

export interface PatternSpec {
  id: LoadingPatternId;
  index: number;
  name: string;
  label: string;
  description: string;
  pairedReveal: RevealEffectId;
}

export interface RevealSpec {
  id: RevealEffectId;
  index: number;
  name: string;
  label: string;
  hasShockwave: boolean;
  skipsGenericBlur: boolean;
}

export const LOADING_PATTERNS: readonly PatternSpec[] = [
  {
    id: "grain",
    index: 3,
    name: "Static grain",
    label: "静态噪粒",
    description: "微粒细闪呼吸 · 半调生长入场",
    pairedReveal: "halftone",
  },
  {
    id: "ripple",
    index: 0,
    name: "Ripple",
    label: "中心涟漪",
    description: "波纹扩散呼吸 · 鱼眼聚焦入场",
    pairedReveal: "fisheye",
  },
  {
    id: "fbm",
    index: 1,
    name: "fBm breath",
    label: "分形呼吸",
    description: "云雾明暗呼吸 · 半调生长入场",
    pairedReveal: "halftone",
  },
  {
    id: "helix",
    index: 2,
    name: "Double helix",
    label: "双螺旋",
    description: "双链波浪律动 · 波浪拉开入场",
    pairedReveal: "unzip",
  },
  {
    id: "spiral",
    index: 4,
    name: "Spiral arms",
    label: "三旋臂",
    description: "旋臂回旋脉动 · 旋臂扫开入场",
    pairedReveal: "spiral",
  },
  {
    id: "warp",
    index: 5,
    name: "Domain warp",
    label: "烟雾域扭曲",
    description: "流体扭曲呼吸 · 噪声溶解入场",
    pairedReveal: "dissolve",
  },
  {
    id: "plasma",
    index: 6,
    name: "Sine plasma",
    label: "正弦等离子",
    description: "正弦干涉波动 · 干涉擦除入场",
    pairedReveal: "interference",
  },
  {
    id: "band",
    index: 7,
    name: "Light band",
    label: "对角光带",
    description: "斜向光带扫掠 · 流式扫描入场",
    pairedReveal: "stream",
  },
  {
    id: "snowflakes",
    index: 8,
    name: "Snowflakes",
    label: "六角雪花",
    description: "雪花飘落点阵 · 胞元绽放入场",
    pairedReveal: "cellbloom",
  },
] as const;

export const REVEAL_EFFECTS: readonly RevealSpec[] = [
  {
    id: "fisheye",
    index: 0,
    name: "Fisheye lens",
    label: "鱼眼镜头",
    hasShockwave: false,
    skipsGenericBlur: false,
  },
  {
    id: "halftone",
    index: 1,
    name: "Halftone grow",
    label: "半调生长",
    hasShockwave: true,
    skipsGenericBlur: true,
  },
  {
    id: "unzip",
    index: 2,
    name: "Unzip",
    label: "波浪拉开",
    hasShockwave: false,
    skipsGenericBlur: false,
  },
  {
    id: "denoise",
    index: 3,
    name: "Denoise",
    label: "逆向去噪",
    hasShockwave: true,
    skipsGenericBlur: true,
  },
  {
    id: "spiral",
    index: 4,
    name: "Spiral wipe",
    label: "旋臂扫开",
    hasShockwave: false,
    skipsGenericBlur: false,
  },
  {
    id: "dissolve",
    index: 5,
    name: "Noise dissolve",
    label: "噪声溶解",
    hasShockwave: false,
    skipsGenericBlur: false,
  },
  {
    id: "interference",
    index: 6,
    name: "Interference wipe",
    label: "干涉波擦除",
    hasShockwave: false,
    skipsGenericBlur: false,
  },
  {
    id: "stream",
    index: 7,
    name: "Top-down stream",
    label: "流式扫描",
    hasShockwave: false,
    skipsGenericBlur: false,
  },
  {
    id: "cellbloom",
    index: 8,
    name: "Cell bloom",
    label: "胞元绽放",
    hasShockwave: true,
    skipsGenericBlur: false,
  },
] as const;

export const PATTERN_INDEX: Record<LoadingPatternId, number> = Object.fromEntries(
  LOADING_PATTERNS.map((p) => [p.id, p.index]),
) as Record<LoadingPatternId, number>;

export const REVEAL_INDEX: Record<RevealEffectId, number> = Object.fromEntries(
  REVEAL_EFFECTS.map((r) => [r.id, r.index]),
) as Record<RevealEffectId, number>;

export const BASE_DOTMATRIX_DEFAULTS: DotMatrixParams = {
  breathing: 100,
  revealMs: 1400,
  pitch: 20,
  dotSize: 15,
  noiseScale: 180,
  zoomIn: 10,
  blurIn: 50,
  dispersion: 30,
  edgeBend: 6,
  edgeGlow: 30,
  dotGlow: 40,
};

const PER_PATTERN_OVERRIDES: Record<LoadingPatternId, Partial<DotMatrixParams>> = {
  fbm: { revealMs: 1600, zoomIn: 8, blurIn: 30, edgeBend: 8 },
  warp: { noiseScale: 220, breathing: 120, revealMs: 1600, blurIn: 70 },
  snowflakes: { pitch: 15, dotSize: 14, revealMs: 1800, dispersion: 35, edgeBend: 8 },
  ripple: { breathing: 90, revealMs: 1600, zoomIn: 6, dispersion: 35 },
  band: { breathing: 110, revealMs: 1300, zoomIn: 8, edgeBend: 8 },
  plasma: { noiseScale: 200, revealMs: 1600 },
  helix: { pitch: 18, breathing: 110, dispersion: 25 },
  grain: {
    pitch: 16,
    dotSize: 13,
    revealMs: 1800,
    zoomIn: 12,
    blurIn: 30,
    dispersion: 25,
    edgeBend: 8,
  },
  spiral: { breathing: 120, revealMs: 1500, dispersion: 35 },
};

export function getPatternDefaults(pattern: LoadingPatternId): DotMatrixParams {
  return {
    ...BASE_DOTMATRIX_DEFAULTS,
    ...PER_PATTERN_OVERRIDES[pattern],
  };
}

/** Default global pair selected by user: Static grain + Halftone grow. */
export const DEFAULT_ACTIVE_PATTERN: LoadingPatternId = "grain";
export const DEFAULT_ACTIVE_REVEAL: RevealEffectId = "halftone";

export function normalizeDotMatrixEffect(raw?: string | null): LoadingPatternId {
  const trimmed = (raw ?? "").trim() as LoadingPatternId;
  return LOADING_PATTERNS.some((p) => p.id === trimmed) ? trimmed : DEFAULT_ACTIVE_PATTERN;
}

export function getDotMatrixEffectSpec(raw?: string | null): PatternSpec {
  const id = normalizeDotMatrixEffect(raw);
  return LOADING_PATTERNS.find((p) => p.id === id) ?? LOADING_PATTERNS[0];
}

/** Timing constants. */
export const DOTS_FADE_IN_MS = 500;
export const STATUS_REFINE_AFTER_MS = 1300;
export const FAST_IMAGE_SKIP_MS = 300;
export const REDUCED_MOTION_FADE_MS = 200;
export const THEME_CROSSFADE_MS = 480;

/** Uneven batch arrival delays across a 3x3 grid (ms). */
export const BATCH_STAGGER_DELAYS_MS: readonly number[] = [
  0, 560, 220, 820, 380, 1040, 120, 700, 300,
];

export function getBatchStaggerDelay(batchIndex: number): number {
  if (batchIndex <= 0) return 0;
  return BATCH_STAGGER_DELAYS_MS[batchIndex % BATCH_STAGGER_DELAYS_MS.length];
}

/**
 * Exact cubic-bezier(0.16, 1, 0.3, 1) easing solver:
 * fast arrival with a long, graceful settle.
 */
export function easeRevealCubic(x: number): number {
  if (x <= 0) return 0;
  if (x >= 1) return 1;
  // P0=(0,0), P1=(0.16, 1.0), P2=(0.3, 1.0), P3=(1,1)
  const cx = 3 * 0.16;
  const bx = 3 * (0.3 - 0.16) - cx;
  const ax = 1 - cx - bx;

  const cy = 3 * 1.0;
  const by = 3 * (1.0 - 1.0) - cy;
  const ay = 1 - cy - by;

  const sampleX = (t: number) => ((ax * t + bx) * t + cx) * t;
  const sampleDX = (t: number) => (3 * ax * t + 2 * bx) * t + cx;
  const sampleY = (t: number) => ((ay * t + by) * t + cy) * t;

  let t = x;
  for (let i = 0; i < 6; i++) {
    const err = sampleX(t) - x;
    if (Math.abs(err) < 1e-6) break;
    const dx = sampleDX(t);
    if (Math.abs(dx) < 1e-6) break;
    t -= err / dx;
    if (t < 0) t = 0;
    if (t > 1) t = 1;
  }
  // Bisection fallback for extreme slopes
  let lo = 0;
  let hi = 1;
  if (Math.abs(sampleX(t) - x) > 1e-5) {
    t = x;
    for (let i = 0; i < 12; i++) {
      const mid = sampleX(t);
      if (Math.abs(mid - x) < 1e-6) break;
      if (mid < x) lo = t;
      else hi = t;
      t = (lo + hi) * 0.5;
    }
  }
  return Math.max(0, Math.min(1, sampleY(t)));
}

export interface DotMatrixStoreState {
  pattern: LoadingPatternId;
  reveal: RevealEffectId;
  setEffect: (raw?: string | null) => void;
  resetAll: () => void;
}

export const useDotMatrixStore = create<DotMatrixStoreState>((set) => ({
  pattern: DEFAULT_ACTIVE_PATTERN,
  reveal: DEFAULT_ACTIVE_REVEAL,

  setEffect: (raw) =>
    set((st) => {
      const spec = getDotMatrixEffectSpec(raw);
      if (st.pattern === spec.id && st.reveal === spec.pairedReveal) {
        return st;
      }
      return { pattern: spec.id, reveal: spec.pairedReveal };
    }),

  resetAll: () =>
    set({
      pattern: DEFAULT_ACTIVE_PATTERN,
      reveal: DEFAULT_ACTIVE_REVEAL,
    }),
}));

export function getActiveDotMatrixParams(): DotMatrixParams {
  return getPatternDefaults(useDotMatrixStore.getState().pattern);
}
