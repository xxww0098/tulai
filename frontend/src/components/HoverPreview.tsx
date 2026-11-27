import {
  forwardRef,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type CSSProperties,
  type FocusEvent,
  type HTMLAttributes,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";
import { toThumbUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import {
  OverlayTimerClock,
  POINTER_WINDOW_MS,
  PREVIEW_CLOSE_DELAY_MS,
  PREVIEW_EXIT_MS,
  PREVIEW_OPEN_DELAY_MS,
  PREVIEW_SWAP_DWELL_MS,
  PointerVelocityTracker,
  TOUCH_LONG_PRESS_MS,
  hasHoverPointer,
  isPointInSafeCorridor,
  type Point,
  type RectBox,
} from "@/lib/hoverIntent";
import { isGlobalShortcutsInstalled, useEscapeHandler } from "@/lib/shortcuts";
import { Badge, type BadgeVariant } from "./ui/Badge";

export const HOVER_PREVIEW_DOM_ID = "tulai-hover-preview-card";

export interface HoverPreviewItem {
  key: string;
  anchor: HTMLElement;
  src: string;
  title: string;
  subtitle?: string;
  note?: string;
  badge?: string;
  badgeVariant?: BadgeVariant;
}

type PreviewPhase = "closed" | "entering" | "steady" | "exiting";
type OpenMode = "hover" | "focus" | "touch";

interface HoverPreviewSnapshot {
  item: HoverPreviewItem | null;
  phase: PreviewPhase;
  swapSeq: number;
  openMode: OpenMode;
}

const clock = new OverlayTimerClock();
const velocityTracker = new PointerVelocityTracker();

let state: HoverPreviewSnapshot = {
  item: null,
  phase: "closed",
  swapSeq: 0,
  openMode: "hover",
};

let pendingKey: string | null = null;
let pendingSwapItem: HoverPreviewItem | null = null;
let exitOrigin: Point | null = null;
let activePanelRect: RectBox | null = null;
let pointerOnPanel = false;
let pointerDownOnKey: string | null = null;
let touchStart: { key: string; x: number; y: number } | null = null;
let touchLongPressTimer: ReturnType<typeof setTimeout> | undefined;
let suppressNextClick = false;

const listeners = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot(): HoverPreviewSnapshot {
  return state;
}

function clearTouchLongPress() {
  if (touchLongPressTimer !== undefined) {
    clearTimeout(touchLongPressTimer);
    touchLongPressTimer = undefined;
  }
  touchStart = null;
}

function setState(next: HoverPreviewSnapshot) {
  state = next;
  emit();
}

export function clearHoverPreview(): void {
  clock.cancelAll();
  clearTouchLongPress();
  pendingKey = null;
  pendingSwapItem = null;
  exitOrigin = null;
  pointerOnPanel = false;
  if (state.item !== null || state.phase !== "closed") {
    setState({ item: null, phase: "closed", swapSeq: state.swapSeq, openMode: "hover" });
  }
}

function dismissWithFade(returnFocusToTrigger = false): void {
  clock.cancelOpen();
  clock.cancelClose();
  clock.cancelSwap();
  pendingKey = null;
  pendingSwapItem = null;
  exitOrigin = null;
  pointerOnPanel = false;

  const anchorToRestore =
    returnFocusToTrigger && state.openMode === "focus" ? state.item?.anchor : undefined;

  if (!state.item || state.phase === "exiting" || state.phase === "closed") {
    return;
  }

  setState({ ...state, phase: "exiting" });
  if (anchorToRestore && anchorToRestore.isConnected) {
    anchorToRestore.focus();
  }
  clock.scheduleExit(PREVIEW_EXIT_MS, () => {
    setState({ item: null, phase: "closed", swapSeq: state.swapSeq, openMode: "hover" });
  });
}

function openNow(item: HoverPreviewItem, openMode: OpenMode) {
  clock.cancelAll();
  pendingKey = null;
  pendingSwapItem = null;
  exitOrigin = null;
  setState({
    item,
    phase: "entering",
    swapSeq: state.swapSeq + 1,
    openMode,
  });
}

function swapInPlace(item: HoverPreviewItem, openMode: OpenMode) {
  clock.cancelClose();
  clock.cancelSwap();
  clock.cancelExit();
  pendingKey = null;
  pendingSwapItem = null;
  exitOrigin = null;
  setState({
    item,
    phase: "steady",
    swapSeq: state.swapSeq + 1,
    openMode,
  });
}

function attemptSwap(candidate: HoverPreviewItem) {
  if (!pendingSwapItem || pendingSwapItem.key !== candidate.key) return;

  const lastPt = velocityTracker.getLastPoint();
  const inSafeCorridor = Boolean(
    lastPt &&
      exitOrigin &&
      activePanelRect &&
      isPointInSafeCorridor(lastPt, exitOrigin, activePanelRect, 14),
  );

  // If moving fast or traversing the safe corridor toward the active panel, wait until settled
  if (velocityTracker.isFast() || inSafeCorridor) {
    clock.scheduleSwap(POINTER_WINDOW_MS, () => {
      if (!pendingSwapItem || pendingSwapItem.key !== candidate.key) return;
      if (velocityTracker.isFast()) {
        attemptSwap(candidate);
        return;
      }
      // Once settled outside fast movement, require 60ms slow dwell
      clock.scheduleSwap(PREVIEW_SWAP_DWELL_MS, () => {
        if (pendingSwapItem?.key === candidate.key && !velocityTracker.isFast()) {
          swapInPlace(candidate, "hover");
        }
      });
    });
    return;
  }

  clock.scheduleSwap(PREVIEW_SWAP_DWELL_MS, () => {
    if (pendingSwapItem?.key === candidate.key && !velocityTracker.isFast()) {
      swapInPlace(candidate, "hover");
    }
  });
}

function scheduleEnter(next: HoverPreviewItem, pointerType: string) {
  if (pointerType === "touch" || (!hasHoverPointer() && pointerType !== "mouse")) {
    return;
  }

  const isVisible = Boolean(state.item && state.phase !== "closed");
  if (isVisible && state.item?.key === next.key) {
    // Returned to current trigger within grace time
    clock.cancelClose();
    clock.cancelSwap();
    clock.cancelExit();
    pendingSwapItem = null;
    if (state.phase === "exiting") {
      setState({ ...state, phase: "steady" });
    }
    return;
  }

  if (pendingKey === next.key || pendingSwapItem?.key === next.key) {
    return;
  }

  if (isVisible) {
    pendingSwapItem = next;
    attemptSwap(next);
    return;
  }

  clock.cancelOpen();
  pendingKey = next.key;
  clock.scheduleOpen(PREVIEW_OPEN_DELAY_MS, () => {
    if (pendingKey !== next.key) return;
    openNow(next, "hover");
  });
}

function scheduleLeave(key: string, anchor: HTMLElement, relatedTarget: EventTarget | null) {
  if (relatedTarget instanceof Node && anchor.contains(relatedTarget)) {
    return;
  }
  if (pendingKey === key) {
    clock.cancelOpen();
    pendingKey = null;
  }
  if (pendingSwapItem?.key === key) {
    clock.cancelSwap();
    pendingSwapItem = null;
  }
  if (state.item?.key !== key || state.openMode === "focus") {
    return;
  }
  exitOrigin = velocityTracker.getLastPoint();
  clock.scheduleClose(PREVIEW_CLOSE_DELAY_MS, () => {
    if (state.item?.key === key && !pointerOnPanel) {
      dismissWithFade(false);
    }
  });
}

function recordPointerMove(key: string, clientX: number, clientY: number) {
  velocityTracker.record(clientX, clientY);
  if (pendingSwapItem?.key === key && velocityTracker.isFast()) {
    // Fast pass-through resets slow-dwell swap so crossed rows never hijack the panel
    attemptSwap(pendingSwapItem);
  }
}

export interface HoverPreviewTargetProps
  extends Omit<HTMLAttributes<HTMLDivElement>, "title"> {
  previewKey: string;
  src?: string;
  title: string;
  subtitle?: string;
  note?: string;
  badge?: string;
  badgeVariant?: BadgeVariant;
  openOnEnter?: boolean;
  openOnTouchTap?: boolean;
  className?: string;
  style?: CSSProperties;
  onMouseEnterExtra?: () => void;
  onMouseLeaveExtra?: () => void;
  children?: ReactNode;
}

export const HoverPreviewTarget = forwardRef<HTMLDivElement, HoverPreviewTargetProps>(
  function HoverPreviewTarget(
    {
      previewKey,
      src,
      title,
      subtitle,
      note,
      badge,
      badgeVariant,
      openOnEnter = false,
      openOnTouchTap = false,
      className,
      style,
      onMouseEnterExtra,
      onMouseLeaveExtra,
      onPointerEnter,
      onPointerLeave,
      onPointerMove,
      onPointerDown,
      onPointerUp,
      onPointerCancel,
      onMouseEnter,
      onMouseLeave,
      onMouseMove,
      onMouseDown,
      onFocus,
      onBlur,
      onKeyDown,
      onContextMenu,
      children,
      ...rest
    },
    forwardedRef,
  ) {
    const anchorRef = useRef<HTMLDivElement | null>(null);

    const setNodeRef = (node: HTMLDivElement | null) => {
      anchorRef.current = node;
      if (typeof forwardedRef === "function") {
        forwardedRef(node);
      } else if (forwardedRef) {
        forwardedRef.current = node;
      }
    };

    const snap = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
    const isDescribed = Boolean(
      snap.item?.key === previewKey && snap.phase !== "closed" && snap.phase !== "exiting",
    );

    useEffect(() => {
      return () => {
        if (pendingKey === previewKey) {
          clock.cancelOpen();
          pendingKey = null;
        }
        if (pendingSwapItem?.key === previewKey) {
          clock.cancelSwap();
          pendingSwapItem = null;
        }
        if (state.item?.key === previewKey) {
          clearHoverPreview();
        }
      };
    }, [previewKey]);

    const makeItem = (): HoverPreviewItem | null => {
      if (!src || !anchorRef.current) return null;
      return {
        key: previewKey,
        anchor: anchorRef.current,
        src,
        title,
        subtitle,
        note,
        badge,
        badgeVariant,
      };
    };

    const handleTouchDown = (e: PointerEvent<HTMLDivElement>) => {
      if (!src || !anchorRef.current) return;
      clearTouchLongPress();
      const item = makeItem();
      if (!item) return;

      if (openOnTouchTap) {
        touchStart = { key: previewKey, x: e.clientX, y: e.clientY };
        return;
      }

      touchStart = { key: previewKey, x: e.clientX, y: e.clientY };
      touchLongPressTimer = setTimeout(() => {
        touchLongPressTimer = undefined;
        suppressNextClick = true;
        openNow(item, "touch");
      }, TOUCH_LONG_PRESS_MS);
    };

    return (
      <div
        {...rest}
        ref={setNodeRef}
        aria-describedby={isDescribed ? HOVER_PREVIEW_DOM_ID : rest["aria-describedby"]}
        className={className}
        style={style}
        onPointerEnter={(e) => {
          onPointerEnter?.(e);
          onMouseEnterExtra?.();
          const item = makeItem();
          if (!item) return;
          scheduleEnter(item, e.pointerType || "mouse");
        }}
        onMouseEnter={(e) => {
          onMouseEnter?.(e);
          onMouseEnterExtra?.();
          const item = makeItem();
          if (!item) return;
          scheduleEnter(item, "mouse");
        }}
        onPointerMove={(e) => {
          onPointerMove?.(e);
          if (e.pointerType === "touch") {
            if (touchStart?.key === previewKey) {
              if (Math.hypot(e.clientX - touchStart.x, e.clientY - touchStart.y) > 8) {
                clearTouchLongPress();
              }
            }
            return;
          }
          recordPointerMove(previewKey, e.clientX, e.clientY);
        }}
        onMouseMove={(e) => {
          onMouseMove?.(e);
          recordPointerMove(previewKey, e.clientX, e.clientY);
        }}
        onPointerLeave={(e) => {
          onPointerLeave?.(e);
          onMouseLeaveExtra?.();
          if (e.pointerType === "touch") return;
          if (anchorRef.current) {
            scheduleLeave(previewKey, anchorRef.current, e.relatedTarget);
          }
        }}
        onMouseLeave={(e) => {
          onMouseLeave?.(e);
          onMouseLeaveExtra?.();
          if (anchorRef.current) {
            scheduleLeave(previewKey, anchorRef.current, e.relatedTarget);
          }
        }}
        onPointerDown={(e) => {
          onPointerDown?.(e);
          if (e.pointerType === "touch") {
            handleTouchDown(e);
            return;
          }
          pointerDownOnKey = previewKey;
          setTimeout(() => {
            if (pointerDownOnKey === previewKey) pointerDownOnKey = null;
          }, 0);
          clearHoverPreview();
        }}
        onPointerUp={(e) => {
          onPointerUp?.(e);
          if (e.pointerType !== "touch") return;
          const startedHere = touchStart?.key === previewKey;
          const movedTooFar =
            touchStart && Math.hypot(e.clientX - touchStart.x, e.clientY - touchStart.y) > 8;
          clearTouchLongPress();
          if (openOnTouchTap && startedHere && !movedTooFar) {
            const item = makeItem();
            if (!item) return;
            if (state.item?.key === previewKey && state.phase !== "closed" && state.phase !== "exiting") {
              dismissWithFade(false);
            } else {
              openNow(item, "touch");
            }
          }
        }}
        onPointerCancel={(e) => {
          onPointerCancel?.(e);
          if (e.pointerType === "touch") clearTouchLongPress();
        }}
        onMouseDown={(e) => {
          pointerDownOnKey = previewKey;
          setTimeout(() => {
            if (pointerDownOnKey === previewKey) pointerDownOnKey = null;
          }, 0);
          clearHoverPreview();
          onMouseDown?.(e);
        }}
        onFocus={(e: FocusEvent<HTMLDivElement>) => {
          onFocus?.(e);
          if (pointerDownOnKey === previewKey) return;
          const item = makeItem();
          if (!item) return;
          if (state.item?.key === previewKey && state.phase !== "closed") return;
          clock.cancelOpen();
          pendingKey = previewKey;
          clock.scheduleOpen(PREVIEW_OPEN_DELAY_MS, () => {
            if (pendingKey !== previewKey) return;
            openNow(item, "focus");
          });
        }}
        onBlur={(e: FocusEvent<HTMLDivElement>) => {
          onBlur?.(e);
          if (e.relatedTarget instanceof Node && anchorRef.current?.contains(e.relatedTarget)) {
            return;
          }
          if (pendingKey === previewKey) {
            clock.cancelOpen();
            pendingKey = null;
          }
          if (state.item?.key === previewKey && state.openMode === "focus") {
            dismissWithFade(false);
          }
        }}
        onKeyDown={(e: KeyboardEvent<HTMLDivElement>) => {
          onKeyDown?.(e);
          if (e.defaultPrevented) return;
          if (e.key === "Enter" && openOnEnter) {
            const item = makeItem();
            if (!item) return;
            e.preventDefault();
            if (state.item?.key === previewKey && state.phase !== "closed" && state.phase !== "exiting") {
              dismissWithFade(false);
            } else {
              openNow(item, "focus");
            }
          }
        }}
        onContextMenu={(e) => {
          clearHoverPreview();
          onContextMenu?.(e);
        }}
      >
        {children}
      </div>
    );
  },
);

const CARD_W = 280;
const GAP_OFFSET = 10;

function computePosition(
  anchor: HTMLElement,
  hasNote = false,
): { left: number; top: number; side: "right" | "left"; cardH: number } {
  const cardH = hasNote ? 318 : 290;
  const margin = 12;
  const winW = window.innerWidth || 1160;
  const winH = window.innerHeight || 740;
  const rect = anchor.getBoundingClientRect();

  // Prefer right side; flip left if overflowing right edge
  let side: "right" | "left" = "right";
  let left = rect.right + GAP_OFFSET;
  if (left + CARD_W + margin > winW) {
    const leftCandidate = rect.left - CARD_W - GAP_OFFSET;
    if (leftCandidate >= margin) {
      left = leftCandidate;
      side = "left";
    } else {
      left = Math.max(margin, winW - CARD_W - margin);
    }
  }

  // Vertically center relative to anchor and clamp within viewport
  const centeredTop = rect.top + rect.height / 2 - cardH / 2;
  const top = Math.max(margin, Math.min(centeredTop, winH - cardH - margin));

  return { left: Math.round(left), top: Math.round(top), side, cardH };
}

export function HoverPreviewHost() {
  const snap = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const bridgeRef = useRef<HTMLDivElement | null>(null);
  const { item, phase, swapSeq, openMode } = snap;
  const isOpen = Boolean(item && item.anchor.isConnected && phase !== "closed");

  const previewEscapeActive = Boolean(item && phase !== "closed" && phase !== "exiting");

  useEscapeHandler(
    "hover-preview",
    previewEscapeActive,
    () => {
      dismissWithFade(true);
    },
    60,
  );

  useEffect(() => {
    if (!previewEscapeActive) return;
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented && !isGlobalShortcutsInstalled()) {
        e.preventDefault();
        dismissWithFade(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [previewEscapeActive]);

  useEffect(() => {
    if (!isOpen || !item) {
      activePanelRect = null;
      return;
    }

    const onGlobalPointerMove = (e: globalThis.PointerEvent | globalThis.MouseEvent) => {
      if ("pointerType" in e && e.pointerType === "touch") return;
      velocityTracker.record(e.clientX, e.clientY);
    };

    const onOutsidePointerDown = (e: globalThis.PointerEvent | globalThis.MouseEvent) => {
      if (suppressNextClick) {
        suppressNextClick = false;
      }
      const target = e.target;
      if (
        target instanceof Node &&
        (cardRef.current?.contains(target) || item.anchor.contains(target))
      ) {
        return;
      }
      clearHoverPreview();
    };

    const onClickCapture = (e: globalThis.MouseEvent) => {
      if (suppressNextClick) {
        suppressNextClick = false;
        e.preventDefault();
        e.stopPropagation();
      }
    };

    const hide = () => clearHoverPreview();
    window.addEventListener("pointermove", onGlobalPointerMove, { passive: true });
    window.addEventListener("pointerdown", onOutsidePointerDown, true);
    window.addEventListener("click", onClickCapture, true);
    window.addEventListener("blur", hide);
    window.addEventListener("scroll", hide, true);
    return () => {
      window.removeEventListener("pointermove", onGlobalPointerMove);
      window.removeEventListener("pointerdown", onOutsidePointerDown, true);
      window.removeEventListener("click", onClickCapture, true);
      window.removeEventListener("blur", hide);
      window.removeEventListener("scroll", hide, true);
    };
  }, [isOpen, item]);

  if (!isOpen || !item) return null;

  const { left, top, side, cardH } = computePosition(item.anchor, Boolean(item.note));
  activePanelRect = { left, top, right: left + CARD_W, bottom: top + cardH };

  const bridgeStyle: CSSProperties =
    side === "right"
      ? { left: left - GAP_OFFSET - 2, top, width: GAP_OFFSET + 4, height: cardH }
      : { left: left + CARD_W - 2, top, width: GAP_OFFSET + 4, height: cardH };

  const handlePanelEnter = () => {
    pointerOnPanel = true;
    clock.cancelClose();
    clock.cancelExit();
    if (state.phase === "exiting") {
      setState({ ...state, phase: "steady" });
    }
  };

  const handlePanelLeave = (relatedTarget: EventTarget | null) => {
    if (
      relatedTarget instanceof Node &&
      (cardRef.current?.contains(relatedTarget) ||
        bridgeRef.current?.contains(relatedTarget) ||
        item.anchor.contains(relatedTarget))
    ) {
      return;
    }
    pointerOnPanel = false;
    if (openMode === "focus") return;
    clock.scheduleClose(PREVIEW_CLOSE_DELAY_MS, () => {
      if (state.item?.key === item.key && !pointerOnPanel) {
        dismissWithFade(false);
      }
    });
  };

  return createPortal(
    <>
      {/* Invisible gap bridge so crossing between trigger and panel never starts close grace */}
      {openMode === "hover" ? (
        <div
          ref={bridgeRef}
          aria-hidden="true"
          style={bridgeStyle}
          className="fixed z-[64] bg-transparent"
          onPointerEnter={handlePanelEnter}
          onMouseEnter={handlePanelEnter}
          onPointerLeave={(e) => handlePanelLeave(e.relatedTarget)}
          onMouseLeave={(e) => handlePanelLeave(e.relatedTarget)}
        />
      ) : null}

      <div
        ref={cardRef}
        id={HOVER_PREVIEW_DOM_ID}
        role="region"
        aria-label={item.title}
        data-testid="hover-preview-card"
        data-state={phase}
        style={{
          left,
          top,
          transition: "left 200ms cubic-bezier(0.2, 0, 0, 1), top 200ms cubic-bezier(0.2, 0, 0, 1)",
        }}
        onPointerEnter={(e) => {
          if (e.pointerType === "touch") return;
          handlePanelEnter();
        }}
        onMouseEnter={handlePanelEnter}
        onPointerLeave={(e) => {
          if (e.pointerType === "touch") return;
          handlePanelLeave(e.relatedTarget);
        }}
        onMouseLeave={(e) => handlePanelLeave(e.relatedTarget)}
        className={cn(
          "fixed z-[65] w-[280px] overflow-hidden rounded-[16px] border border-hairline bg-card float-shadow",
          "dark:border-[#2D3A52]",
          phase === "entering" &&
            (side === "right" ? "animate-tulai-preview-in-right" : "animate-tulai-preview-in-left"),
          phase === "exiting" && "animate-tulai-preview-out pointer-events-none",
        )}
      >
        <div key={swapSeq} className={cn(phase === "steady" && "animate-tulai-swap-fade")}>
          <div className="bg-checkerboard flex h-[220px] w-full items-center justify-center p-2.5">
            <HoverPreviewImage src={toThumbUrl(item.src, 560)} alt={item.title} />
          </div>
          <div className="flex flex-col gap-1 border-t border-divider px-3 py-2.5">
            <div className="flex items-center justify-between gap-2">
              <span className="min-w-0 flex-1 truncate text-xs font-semibold text-ink">
                {item.title}
              </span>
              {item.badge ? (
                <Badge text={item.badge} variant={item.badgeVariant ?? "neutral"} />
              ) : null}
            </div>
            {item.subtitle ? (
              <span className="tnum truncate text-[11px] text-muted">{item.subtitle}</span>
            ) : null}
            {item.note ? (
              <span
                data-testid="hover-preview-note"
                className="line-clamp-2 text-[11px] text-status-warn"
              >
                {item.note}
              </span>
            ) : null}
          </div>
        </div>
      </div>
    </>,
    document.body,
  );
}

function HoverPreviewImage({ src, alt }: { src: string; alt: string }) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [src]);

  if (failed || !src) {
    return (
      <div className="flex h-full w-full items-center justify-center text-[11px] text-subtle">
        无法加载预览
      </div>
    );
  }

  return (
    <img
      key={src}
      src={src}
      alt={alt}
      decoding="async"
      draggable={false}
      onError={() => setFailed(true)}
      className="h-full w-full rounded-[10px] object-contain"
    />
  );
}
