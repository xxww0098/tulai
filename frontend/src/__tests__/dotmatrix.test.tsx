import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { DotMatrixImageBox } from "@/components/dotmatrix/DotMatrixImageBox";
import {
  BATCH_STAGGER_DELAYS_MS,
  DEFAULT_ACTIVE_PATTERN,
  DEFAULT_ACTIVE_REVEAL,
  LOADING_PATTERNS,
  REVEAL_EFFECTS,
  easeRevealCubic,
  getBatchStaggerDelay,
  getDotMatrixEffectSpec,
  getPatternDefaults,
  normalizeDotMatrixEffect,
  useDotMatrixStore,
} from "@/lib/dotmatrix/config";
import { evaluatePatternDotCpu, resetDotMatrixEngineForTests } from "@/lib/dotmatrix/engine";
import { clearDecodedImageCache } from "@/lib/dotmatrix/imageLoader";
import { resetStoreForTests, useAppStore } from "@/store";

function makeMockCanvas2D(): CanvasRenderingContext2D {
  const noop = () => {};
  return {
    clearRect: noop,
    fillRect: noop,
    drawImage: noop,
    setTransform: noop,
    beginPath: noop,
    arc: noop,
    fill: noop,
    globalAlpha: 1,
    fillStyle: "",
  } as unknown as CanvasRenderingContext2D;
}

beforeEach(() => {
  useDotMatrixStore.getState().resetAll();
  clearDecodedImageCache();
  resetDotMatrixEngineForTests();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation((kind: string) => {
    if (kind === "2d") return makeMockCanvas2D() as never;
    return null as never;
  });
});

afterEach(() => {
  cleanup();
  resetDotMatrixEngineForTests();
  clearDecodedImageCache();
  useDotMatrixStore.getState().resetAll();
  resetStoreForTests();
  vi.restoreAllMocks();
});

describe("DotMatrix config, 9 built-in effects, easing & CPU math", () => {
  it("exposes all 9 built-in dot matrix effects, default pair (Static grain + Halftone grow), and per-pattern defaults", () => {
    expect(LOADING_PATTERNS).toHaveLength(9);
    expect(REVEAL_EFFECTS).toHaveLength(9);
    expect(DEFAULT_ACTIVE_PATTERN).toBe("grain");
    expect(DEFAULT_ACTIVE_REVEAL).toBe("halftone");
    expect(getDotMatrixEffectSpec("grain").pairedReveal).toBe("halftone");

    const shockwaveReveals = REVEAL_EFFECTS.filter((r) => r.hasShockwave).map((r) => r.id);
    expect(shockwaveReveals).toEqual(["halftone", "denoise", "cellbloom"]);

    expect(getPatternDefaults("fbm")).toMatchObject({
      revealMs: 1600,
      zoomIn: 8,
      blurIn: 30,
      edgeBend: 8,
    });
    expect(getPatternDefaults("grain")).toMatchObject({
      pitch: 16,
      dotSize: 13,
      revealMs: 1800,
      zoomIn: 12,
      blurIn: 30,
      dispersion: 25,
      edgeBend: 8,
    });
    expect(getPatternDefaults("snowflakes")).toMatchObject({
      pitch: 15,
      dotSize: 14,
      revealMs: 1800,
      dispersion: 35,
      edgeBend: 8,
    });
    expect(getPatternDefaults("warp")).toMatchObject({
      noiseScale: 220,
      breathing: 120,
      revealMs: 1600,
      blurIn: 70,
    });
  });

  it("normalizes unknown effect IDs to grain and pairs each of the 9 effects with its reveal", () => {
    expect(normalizeDotMatrixEffect("unknown-effect")).toBe("grain");
    expect(normalizeDotMatrixEffect("")).toBe("grain");

    for (const spec of LOADING_PATTERNS) {
      act(() => {
        useDotMatrixStore.getState().setEffect(spec.id);
      });
      expect(useDotMatrixStore.getState().pattern).toBe(spec.id);
      expect(useDotMatrixStore.getState().reveal).toBe(spec.pairedReveal);
    }
  });

  it("solves cubic-bezier(0.16, 1, 0.3, 1) accurately (fast in, long settle) and uneven 3x3 stagger delays", () => {
    expect(easeRevealCubic(0)).toBe(0);
    expect(easeRevealCubic(1)).toBe(1);
    const early = easeRevealCubic(0.15);
    const mid = easeRevealCubic(0.5);
    const late = easeRevealCubic(0.85);
    expect(early).toBeGreaterThan(0.55);
    expect(mid).toBeGreaterThan(early);
    expect(late).toBeGreaterThan(mid);
    expect(late).toBeLessThan(1);

    expect(BATCH_STAGGER_DELAYS_MS).toEqual([0, 560, 220, 820, 380, 1040, 120, 700, 300]);
    expect(getBatchStaggerDelay(0)).toBe(0);
    expect(getBatchStaggerDelay(5)).toBe(1040);
    expect(getBatchStaggerDelay(9)).toBe(0);
  });

  it("produces valid bounded, spatially varying dot values for all 9 patterns on small and large boxes", () => {
    const sizes: Array<[number, number]> = [
      [88, 88],
      [600, 772],
    ];
    for (const spec of LOADING_PATTERNS) {
      const def = getPatternDefaults(spec.id);
      for (const [w, h] of sizes) {
        let minV = Infinity;
        let maxV = -Infinity;
        const cols = Math.max(4, Math.ceil(w / def.pitch));
        const rows = Math.max(4, Math.ceil(h / def.pitch));
        for (let iy = 0; iy < rows; iy++) {
          for (let ix = 0; ix < cols; ix++) {
            const px = (ix + 0.5) * def.pitch;
            const py = (iy + 0.5) * def.pitch;
            const v = evaluatePatternDotCpu(
              spec.id,
              ix,
              iy,
              px,
              py,
              w,
              h,
              def.pitch,
              def.noiseScale,
              1.25,
            );
            expect(Number.isFinite(v)).toBe(true);
            expect(v).toBeGreaterThanOrEqual(0);
            expect(v).toBeLessThanOrEqual(1);
            if (v < minV) minV = v;
            if (v > maxV) maxV = v;
          }
        }
        expect(maxV - minV).toBeGreaterThan(0.05);
      }
    }
  });
});

describe("DotMatrixImageBox decode gating, fast skip, shell sync & error freeze", () => {
  it("sizes a batch from one observer without reading layout on mount", () => {
    let report!: ResizeObserverCallback;
    const observe = vi.fn();
    const unobserve = vi.fn();
    const disconnect = vi.fn();
    const observer = { observe, unobserve, disconnect };
    const resize = vi.fn(function (callback: ResizeObserverCallback) {
      report = callback;
      return observer;
    });
    vi.stubGlobal("ResizeObserver", resize);
    try {
      const measure = vi.spyOn(HTMLElement.prototype, "getBoundingClientRect");
      let frame!: FrameRequestCallback;
      vi.spyOn(window, "requestAnimationFrame").mockImplementation(cb => { frame = cb; return 1; });
      const { unmount } = render(<><DotMatrixImageBox /><DotMatrixImageBox /></>);
      expect(resize).toHaveBeenCalledTimes(1);
      expect(measure).not.toHaveBeenCalled();
      const targets = observe.mock.calls.map(([target]) => target as HTMLElement);
      expect(targets).toHaveLength(2);
      report(targets.map(target => ({
        target, contentRect: new DOMRectReadOnly(0, 0, 123, 87),
        borderBoxSize: [], contentBoxSize: [], devicePixelContentBoxSize: [],
      })), observer);
      frame(performance.now() + 40);
      for (const target of targets) {
        expect(target.querySelector("canvas")?.width).toBe(123 * Math.min(window.devicePixelRatio || 1, 2));
      }
      unmount();
      expect(unobserve).toHaveBeenCalledTimes(2);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("paces settings previews independently from live workbench canvases on a 120 Hz display", () => {
    let nextFrame: FrameRequestCallback;
    vi.spyOn(window, "requestAnimationFrame").mockImplementation(callback => {
      nextFrame = callback;
      return 1;
    });
    render(<>
      <DotMatrixImageBox data-testid="sample" preview patternOverride="ripple" />
      <DotMatrixImageBox data-testid="live" />
    </>);
    const [sample, live] = ["sample", "live"].map(id => {
      const canvas = screen.getByTestId(id).querySelector("canvas")!;
      const ctx = makeMockCanvas2D();
      Object.defineProperty(canvas, "getContext", { value: () => ctx });
      return vi.spyOn(ctx, "fillRect");
    });
    act(() => {
      const start = performance.now();
      for (let i = 1; i <= 120; i++) nextFrame(start + i * 1000 / 120);
    });
    expect(sample.mock.calls.length).toBeGreaterThanOrEqual(30);
    expect(sample.mock.calls.length).toBeLessThanOrEqual(31);
    expect(live).toHaveBeenCalledTimes(120);
  });

  it("reuses only matching waiting frames and freezes a paused card while its neighbors animate", () => {
    let nextFrame: FrameRequestCallback;
    vi.spyOn(window, "requestAnimationFrame").mockImplementation(callback => {
      nextFrame = callback;
      return 1;
    });
    render(<>
      <DotMatrixImageBox data-testid="first" />
      <DotMatrixImageBox data-testid="second" />
      <DotMatrixImageBox data-testid="round" cornerRadius={24} />
      <DotMatrixImageBox data-testid="paused" status="paused" />
    </>);
    const [first, second, round, paused] = ["first", "second", "round", "paused"].map(id => {
      const canvas = screen.getByTestId(id).querySelector("canvas")!;
      const ctx = makeMockCanvas2D();
      Object.defineProperty(canvas, "getContext", { value: () => ctx });
      return { canvas, draw: vi.spyOn(ctx, "fillRect"), copy: vi.spyOn(ctx, "drawImage") };
    });
    act(() => {
      const start = performance.now() + 1000;
      for (let i = 0; i < 120; i++) nextFrame(start + i * 1000 / 120);
    });
    expect(first.draw.mock.calls.length).toBeGreaterThanOrEqual(30);
    expect(first.draw.mock.calls.length).toBeLessThanOrEqual(31);
    expect(second.draw).not.toHaveBeenCalled();
    expect(second.copy.mock.calls.length).toBe(first.draw.mock.calls.length);
    expect(second.copy.mock.calls.every(call => call[0] === first.canvas)).toBe(true);
    expect(round.draw.mock.calls.length).toBe(first.draw.mock.calls.length);
    expect(paused.draw).toHaveBeenCalledTimes(1);
  });

  it("waits for img.decode() before starting reveal, skips dots for fast (<300ms) images, and freezes dots with retry on error", async () => {
    const decodeCalls: Array<() => void> = [];
    const OriginalImage = window.Image;

    class MockImage {
      src = "";
      crossOrigin: string | null = null;
      decoding = "async";
      complete = false;
      naturalWidth = 512;
      naturalHeight = 512;
      onload: (() => void) | null = null;
      onerror: (() => void) | null = null;

      constructor() {
        queueMicrotask(() => {
          this.complete = true;
          this.onload?.();
        });
      }

      decode(): Promise<void> {
        return new Promise((resolve) => {
          decodeCalls.push(resolve);
        });
      }
    }

    vi.stubGlobal("Image", MockImage);

    try {
      const { rerender } = render(
        <DotMatrixImageBox
          data-testid="box-decode"
          src="tulaimedia://localhost/file?t=slow1&v=1"
          forceReveal
          showStatusLine
        />,
      );

      // Flush onload microtask: decode() is still pending so phase MUST stay loading
      await act(async () => {
        await Promise.resolve();
      });
      expect(screen.getByTestId("box-decode")).toHaveAttribute("data-phase", "loading");
      expect(decodeCalls.length).toBeGreaterThanOrEqual(1);

      // Resolve decode() -> transitions into revealing + status switches to 已完成
      await act(async () => {
        for (const resolve of decodeCalls.splice(0)) resolve();
        await Promise.resolve();
      });
      expect(screen.getByTestId("box-decode")).toHaveAttribute("data-phase", "revealing");
      expect(screen.getByTestId("box-decode")).toHaveTextContent("已完成");

      // Fast (<300ms) non-forced image skips dots via fastFade
      rerender(
        <DotMatrixImageBox
          data-testid="box-decode"
          src="tulaimedia://localhost/file?t=fast2&v=1"
          forceReveal={false}
        />,
      );
      await act(async () => {
        await Promise.resolve();
        for (const resolve of decodeCalls.splice(0)) resolve();
        await Promise.resolve();
      });
      expect(screen.getByTestId("box-decode")).toHaveAttribute("data-phase", "fastFade");

      // Error state stops dots where they are, displays reason, and wires retry button
      const onRetry = vi.fn();
      rerender(
        <DotMatrixImageBox
          data-testid="box-decode"
          status="error"
          errorText="网络超时"
          onRetry={onRetry}
          retryLabel="重试"
        />,
      );
      expect(screen.getByTestId("box-decode")).toHaveAttribute("data-phase", "error");
      expect(screen.getByText("网络超时")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "重试" }));
      expect(onRetry).toHaveBeenCalledTimes(1);
    } finally {
      vi.stubGlobal("Image", OriginalImage);
    }
  });

  it("syncs global default effect from shell state while respecting per-box pattern/reveal overrides", () => {
    act(() => {
      useAppStore.setState((st) => ({
        shell: { ...st.shell, dotMatrixEffect: "ripple" },
      }));
    });

    render(
      <>
        <DotMatrixImageBox data-testid="box-global" />
        <DotMatrixImageBox
          data-testid="box-override"
          patternOverride="band"
          revealOverride="stream"
        />
      </>,
    );

    expect(screen.getByTestId("box-global")).toHaveAttribute("data-pattern", "ripple");
    expect(screen.getByTestId("box-global")).toHaveAttribute("data-reveal", "fisheye");
    expect(screen.getByTestId("box-override")).toHaveAttribute("data-pattern", "band");
    expect(screen.getByTestId("box-override")).toHaveAttribute("data-reveal", "stream");
  });
});
