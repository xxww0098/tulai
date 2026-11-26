import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { Button, Icon } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  FAST_IMAGE_SKIP_MS,
  REDUCED_MOTION_FADE_MS,
  STATUS_REFINE_AFTER_MS,
  getPatternDefaults,
  useDotMatrixStore,
  type LoadingPatternId,
  type RevealEffectId,
} from "@/lib/dotmatrix/config";
import {
  getDotMatrixEngine,
  type DotMatrixBoxPhase,
} from "@/lib/dotmatrix/engine";
import {
  getCachedDecodedImage,
  loadAndDecodeImage,
  type DecodedImageRecord,
} from "@/lib/dotmatrix/imageLoader";
import { useShell } from "@/store";
import { StatusSheenLine } from "./StatusSheenLine";

export type DotMatrixBoxStatus = "loading" | "ready" | "error" | "paused";

export interface DotMatrixImageBoxProps {
  /** Image URL to decode and reveal. Omit while still waiting for generation. */
  src?: string;
  alt?: string;
  /** External lifecycle status: "loading" shows dots, "ready" decodes & reveals src, "error" freezes dots. */
  status?: DotMatrixBoxStatus;
  /** Optional custom status line copy override (e.g., "需要人工验证"). */
  statusText?: string;
  /** Show the moving-sheen status line directly above the image box. */
  showStatusLine?: boolean | "overlay";
  statusLineSize?: "sm" | "lg";
  /** Error message displayed when status === "error" or decode fails. */
  errorText?: string;
  /** Retry handler shown alongside errorText when provided. */
  onRetry?: () => void;
  retryLabel?: string;
  /**
   * Skip dot-matrix wait & reveal immediately (for pre-existing historical images
   * loaded on mount/tab switch) so zero GPU work runs for already-settled tiles.
   */
  initialSettled?: boolean;
  /**
   * Always play the full dot-matrix reveal when `src` decodes (used for freshly
   * generated images & dev simulations, bypassing the 300ms fast-image skip).
   */
  forceReveal?: boolean;
  /** Uneven stagger delay (ms) before reveal begins across multi-image batches. */
  staggerDelayMs?: number;
  /** Rounded corner radius in logical px (concentric with enclosing card). */
  cornerRadius?: number;
  /** Image fit mode inside the box. */
  fit?: "cover" | "contain";
  /** Small, looping sample in the Settings gallery. */
  preview?: boolean;
  /** Optional per-box pattern / reveal override. */
  patternOverride?: LoadingPatternId;
  revealOverride?: RevealEffectId;
  /** Keep the box background transparent when settled (useful over checkerboard). */
  transparentWhenSettled?: boolean;
  className?: string;
  boxClassName?: string;
  imgClassName?: string;
  imgStyle?: CSSProperties;
  imgTestId?: string;
  draggable?: boolean;
  onImgClick?: (e: ReactMouseEvent<HTMLImageElement>) => void;
  onSettled?: () => void;
  "data-testid"?: string;
}

export function useResolvedDotMatrixDark(): boolean {
  const sh = useShell();
  const dark = Boolean(sh.dark);
  const dotMatrixEffect = sh.dotMatrixEffect;

  useEffect(() => {
    getDotMatrixEngine().setTargetDark(dark);
  }, [dark]);

  useEffect(() => {
    useDotMatrixStore.getState().setEffect(dotMatrixEffect);
  }, [dotMatrixEffect]);

  return dark;
}

export function DotMatrixImageBox({
  src = "",
  alt = "",
  status,
  statusText,
  showStatusLine = false,
  statusLineSize = "sm",
  errorText,
  onRetry,
  retryLabel = "重试",
  initialSettled = false,
  forceReveal = false,
  staggerDelayMs = 0,
  cornerRadius = 12,
  fit = "contain",
  preview = false,
  patternOverride,
  revealOverride,
  transparentWhenSettled = false,
  className,
  boxClassName,
  imgClassName,
  imgStyle,
  imgTestId,
  draggable = false,
  onImgClick,
  onSettled,
  "data-testid": testId,
}: DotMatrixImageBoxProps) {
  useResolvedDotMatrixDark();

  const reactId = useId();
  const boxId = useMemo(() => `dm-${reactId.replace(/:/g, "")}`, [reactId]);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  const effectiveStatus: DotMatrixBoxStatus = status ?? (src ? "ready" : "loading");
  const trimmedSrc = src.trim();

  const [phase, setPhase] = useState<DotMatrixBoxPhase>(() => {
    if (effectiveStatus === "error") return "error";
    if (initialSettled && trimmedSrc) return "settled";
    return "loading";
  });
  const [stageStep, setStageStep] = useState<"draft" | "refine" | "done">(() =>
    initialSettled && trimmedSrc ? "done" : "draft",
  );
  const [decodeErr, setDecodeErr] = useState<string | null>(null);
  const [useCssFallback, setUseCssFallback] = useState(false);
  const [cssRevealOpen, setCssRevealOpen] = useState(initialSettled && Boolean(trimmedSrc));
  const [decodedRecord, setDecodedRecord] = useState<DecodedImageRecord | null>(() =>
    trimmedSrc ? getCachedDecodedImage(trimmedSrc) ?? null : null,
  );

  const requestStartRef = useRef<number>(
    typeof performance !== "undefined" ? performance.now() : Date.now(),
  );
  const revealedSrcRef = useRef<string>(initialSettled ? trimmedSrc : "");
  const registeredRef = useRef(false);
  const onSettledRef = useRef(onSettled);
  onSettledRef.current = onSettled;

  const activePattern = useDotMatrixStore((s) => patternOverride ?? s.pattern);
  const activeReveal = useDotMatrixStore((s) => revealOverride ?? s.reveal);
  const { revealMs, zoomIn: zoomInPct, blurIn: blurInPct } = useMemo(
    () => getPatternDefaults(activePattern),
    [activePattern],
  );

  // Status line 1.3s progression while waiting ("正在生成初稿…" -> "正在细化细节…")
  useEffect(() => {
    if (!showStatusLine || effectiveStatus === "error" || decodeErr) return;
    if (phase === "settled" || phase === "revealing" || phase === "fastFade") {
      setStageStep("done");
      return;
    }
    if (effectiveStatus === "paused") return;
    setStageStep("draft");
    const timer = setTimeout(() => {
      setStageStep((cur) => (cur === "draft" ? "refine" : cur));
    }, STATUS_REFINE_AFTER_MS);
    return () => clearTimeout(timer);
  }, [showStatusLine, effectiveStatus, phase, decodeErr]);

  // Decode-gated image loading: NEVER reveal on network response alone, always await img.decode()
  useEffect(() => {
    if (effectiveStatus === "error") {
      setPhase("error");
      setCssRevealOpen(false);
      return;
    }

    if (!trimmedSrc || effectiveStatus === "loading" || effectiveStatus === "paused") {
      if (!trimmedSrc) {
        if (revealedSrcRef.current !== "") {
          revealedSrcRef.current = "";
          requestStartRef.current = performance.now();
        }
        setDecodedRecord(null);
        setDecodeErr(null);
        setCssRevealOpen(false);
        setPhase("loading");
        if (registeredRef.current) {
          const engine = getDotMatrixEngine();
          engine.updateBox(boxId, {
            phase: "loading",
            imageRecord: null,
            createdAt: requestStartRef.current,
            frozenClock: effectiveStatus === "paused" ? engine.getBreathingClock() : null,
          });
        }
      }
      return;
    }

    if (initialSettled && revealedSrcRef.current === trimmedSrc) {
      setPhase("settled");
      setStageStep("done");
      setCssRevealOpen(true);
      return;
    }

    if (revealedSrcRef.current === trimmedSrc && phase === "settled") {
      return;
    }

    let cancelled = false;
    setDecodeErr(null);
    const loadStartedAt =
      revealedSrcRef.current === "" ? requestStartRef.current : performance.now();
    if (revealedSrcRef.current !== "") {
      requestStartRef.current = loadStartedAt;
      setPhase("loading");
      setCssRevealOpen(false);
    }

    void loadAndDecodeImage(trimmedSrc)
      .then((record) => {
        if (cancelled) return;
        const elapsed = performance.now() - loadStartedAt;
        revealedSrcRef.current = trimmedSrc;
        setDecodedRecord(record);
        setStageStep("done");

        const fastSkip = !forceReveal && elapsed <= FAST_IMAGE_SKIP_MS && staggerDelayMs <= 0;
        const nextPhase: DotMatrixBoxPhase = fastSkip ? "fastFade" : "revealing";
        setPhase(nextPhase);

        const engine = getDotMatrixEngine();
        engine.updateBox(boxId, {
          phase: nextPhase,
          imageRecord: record,
          revealStartAt: performance.now(),
          staggerDelayMs: fastSkip ? 0 : staggerDelayMs,
          frozenClock: null,
        });
      })
      .catch(() => {
        if (cancelled) return;
        setDecodeErr(errorText || "图片加载失败");
        setPhase("error");
        setCssRevealOpen(false);
        const engine = getDotMatrixEngine();
        engine.updateBox(boxId, {
          phase: "error",
          frozenClock: engine.getBreathingClock(),
        });
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [trimmedSrc, effectiveStatus, initialSettled, forceReveal, staggerDelayMs, boxId]);

  // Register box with single shared DotMatrixEngine & track container size
  useEffect(() => {
    const container = containerRef.current;
    const canvas = canvasRef.current;
    if (!container || !canvas) return;

    const engine = getDotMatrixEngine();
    // The shared resize observer supplies the initial dimensions after layout.
    const rect = typeof ResizeObserver === "function" ? null : container.getBoundingClientRect();
    engine.registerBox({
      id: boxId,
      canvas,
      container,
      width: rect ? Math.max(24, Math.round(rect.width || container.clientWidth || 160)) : 0,
      height: rect ? Math.max(24, Math.round(rect.height || container.clientHeight || 160)) : 0,
      cornerRadius,
      fit,
      phase,
      paused: effectiveStatus === "paused",
      preview,
      patternOverride,
      revealOverride,
      imageRecord: decodedRecord,
      createdAt: requestStartRef.current,
      revealStartAt: performance.now(),
      staggerDelayMs,
      frozenClock: effectiveStatus === "error" || effectiveStatus === "paused" ? engine.getBreathingClock() : null,
      dirty: true,
      visible: true,
      onSettled: () => {
        setPhase("settled");
        setStageStep("done");
        setCssRevealOpen(true);
        onSettledRef.current?.();
      },
      onFallbackChange: (fallback) => {
        setUseCssFallback((prev) => (prev === fallback ? prev : fallback));
      },
    });
    registeredRef.current = true;

    return () => {
      registeredRef.current = false;
      engine.unregisterBox(boxId);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [boxId]);

  // Keep engine box parameters synchronized on prop updates
  useEffect(() => {
    if (!registeredRef.current) return;
    const engine = getDotMatrixEngine();
    engine.updateBox(boxId, {
      cornerRadius,
      fit,
      paused: effectiveStatus === "paused",
      preview,
      patternOverride,
      revealOverride,
      ...(effectiveStatus === "error"
        ? { phase: "error" as const, frozenClock: engine.getBreathingClock() }
        : {}),
    });
  }, [boxId, cornerRadius, fit, effectiveStatus, preview, patternOverride, revealOverride]);

  // Open CSS reveal transition on the frame after entering fastFade or CSS fallback revealing
  useEffect(() => {
    const shouldAnimateCss =
      phase === "fastFade" || (useCssFallback && phase === "revealing");
    if (!shouldAnimateCss) return;
    setCssRevealOpen(false);
    if (typeof requestAnimationFrame !== "function") {
      setCssRevealOpen(true);
      return;
    }
    let innerRaf: number | null = null;
    const outerRaf = requestAnimationFrame(() => {
      innerRaf = requestAnimationFrame(() => {
        setCssRevealOpen(true);
      });
    });
    return () => {
      cancelAnimationFrame(outerRaf);
      if (innerRaf !== null) cancelAnimationFrame(innerRaf);
    };
  }, [phase, useCssFallback, trimmedSrc]);

  const isError = phase === "error" || effectiveStatus === "error" || Boolean(decodeErr);
  const isSettled = phase === "settled" && Boolean(trimmedSrc) && !isError;
  const isPaused = effectiveStatus === "paused" && !isSettled && !isError;
  const reducedMotion = getDotMatrixEngine().isReducedMotion();

  const resolvedStatusLine = (() => {
    if (isError) return errorText || decodeErr || "生成失败";
    if (statusText) return statusText;
    if (isPaused) return "动画已暂停";
    if (stageStep === "done" || isSettled) return "已完成";
    if (stageStep === "refine") return "正在细化细节…";
    return "正在生成初稿…";
  })();

  const statusTone = isError
    ? "danger"
    : statusText === "需要人工验证"
      ? "warn"
      : isPaused
        ? "paused"
        : isSettled || stageStep === "done"
          ? "done"
          : "default";

  // Show the canvas while loading, revealing, fast-fading, or frozen on error.
  // Once settled, stop drawing and present the native <img> (0 GPU work).
  const showCanvas = !isSettled;

  // CSS fallback animation when WebGL2 cannot render the image (jsdom, software GPU, or non-CORS image)
  const showNativeImg =
    Boolean(trimmedSrc) &&
    !isError &&
    (isSettled || useCssFallback || phase === "fastFade");

  const cssTransitionStyle: CSSProperties = (() => {
    if (isSettled) {
      return {
        opacity: 1,
        transform: imgStyle?.transform,
        filter: undefined,
        ...imgStyle,
      };
    }
    if (phase === "fastFade" || reducedMotion) {
      return {
        opacity: cssRevealOpen ? 1 : 0,
        transition: `opacity ${REDUCED_MOTION_FADE_MS}ms ease-out`,
        ...imgStyle,
      };
    }
    if (useCssFallback && phase === "revealing") {
      const startScale = 1 + zoomInPct / 100;
      const startBlur = Math.round((blurInPct / 100) * 14);
      return {
        opacity: cssRevealOpen ? 1 : 0,
        transform: cssRevealOpen ? "scale(1)" : `scale(${startScale})`,
        filter: cssRevealOpen ? "blur(0px)" : `blur(${startBlur}px)`,
        transition: [
          `opacity ${Math.round(revealMs * 0.65)}ms cubic-bezier(0.16, 1, 0.3, 1) ${staggerDelayMs}ms`,
          `transform ${revealMs}ms cubic-bezier(0.16, 1, 0.3, 1) ${staggerDelayMs}ms`,
          `filter ${revealMs}ms cubic-bezier(0.16, 1, 0.3, 1) ${staggerDelayMs}ms`,
        ].join(", "),
        ...imgStyle,
      };
    }
    return {
      opacity: 0,
      ...imgStyle,
    };
  })();

  return (
    <div
      data-testid={testId}
      data-phase={phase}
      data-pattern={activePattern}
      data-reveal={activeReveal}
      className={cn("flex min-h-0 min-w-0 flex-col", className)}
    >
      {showStatusLine ? (
        <div className={cn(
          "mb-1.5 flex min-w-0 items-center justify-between gap-2",
          showStatusLine === "overlay" && "absolute inset-x-1 bottom-0 z-10 rounded bg-card/90 px-1",
        )}>
          <StatusSheenLine
            text={resolvedStatusLine}
            active={!isError && !isPaused && !isSettled && stageStep !== "done"}
            tone={statusTone}
            size={statusLineSize}
          />
        </div>
      ) : null}

      <div
        ref={containerRef}
        style={{ borderRadius: `${cornerRadius}px` }}
        className={cn(
          "relative min-h-0 w-full flex-auto overflow-hidden transition-colors duration-[480ms]",
          isSettled && transparentWhenSettled
            ? "bg-transparent"
            : "bg-[#F4F5F7] dark:bg-[#0D1118]",
          boxClassName,
        )}
      >
        {/* Shared-engine procedural dot-matrix & WebGL2 reveal surface */}
        <canvas
          ref={canvasRef}
          aria-hidden="true"
          className={cn(
            "pointer-events-none absolute inset-0 h-full w-full transition-opacity duration-150",
            showCanvas ? "opacity-100" : "opacity-0",
          )}
        />

        {/* Accessible native <img>: takes over with 0 GPU work once settled (or during CSS fallback) */}
        {trimmedSrc ? (
          <img
            src={trimmedSrc}
            alt={alt}
            decoding="async"
            draggable={draggable}
            onClick={onImgClick}
            data-testid={imgTestId}
            style={cssTransitionStyle}
            className={cn(
              "relative z-[1] h-full w-full",
              fit === "cover" ? "object-cover" : "object-contain",
              showNativeImg ? "pointer-events-auto" : "pointer-events-none",
              imgClassName,
            )}
          />
        ) : null}

        {/* Error overlay on frozen dots: says what happened and offers retry (Section 1) */}
        {isError ? (
          <div className="absolute inset-0 z-[2] flex flex-col items-center justify-center gap-2 bg-card/75 px-3 text-center backdrop-blur-[1px] dark:bg-[#0D1118]/78">
            <Icon name="alert-triangle" size={18} className="text-status-danger" />
            <span className="line-clamp-2 max-w-full select-text text-[11px] font-medium text-ink">
              {errorText || decodeErr || "生成失败"}
            </span>
            {onRetry ? (
              <Button
                text={retryLabel}
                size="xs"
                icon="refresh-cw"
                variant="outline"
                onClick={(e) => {
                  e.stopPropagation();
                  onRetry();
                }}
              />
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
