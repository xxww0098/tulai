import {
  Children,
  cloneElement,
  createContext,
  isValidElement,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FocusEvent,
  type HTMLAttributes,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
  type ReactElement,
  type ReactNode,
  type Ref,
} from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/cn";
import {
  LABEL_EXIT_MS,
  LABEL_GROUP_GRACE_MS,
  LABEL_OPEN_DELAY_MS,
  OverlayTimerClock,
  TOUCH_LONG_PRESS_MS,
  hasHoverPointer,
} from "@/lib/hoverIntent";
import { isGlobalShortcutsInstalled, useEscapeHandler } from "@/lib/shortcuts";

export type TooltipSide = "top" | "right" | "bottom" | "left";
export type TooltipAlign = "start" | "center" | "end";

interface TooltipEntry {
  triggerId: string;
  anchor: HTMLElement;
  content: ReactNode;
  shortcut?: string;
  side: TooltipSide;
  align: TooltipAlign;
  groupId: string;
  delayDuration?: number;
  className?: string;
  openMode: "hover" | "focus" | "touch";
}

interface TooltipGroupRecord {
  id: string;
  element: HTMLElement | null;
}

interface TooltipController {
  domId: string;
  delayDuration: number;
  skipDelayDuration: number;
  activeTriggerId: string | null;
  enterTrigger: (entry: Omit<TooltipEntry, "openMode">, pointerType: string) => void;
  leaveTrigger: (triggerId: string, anchor: HTMLElement, relatedTarget: EventTarget | null) => void;
  pressTrigger: (triggerId: string, anchor: HTMLElement, pointerType: string, clientX: number, clientY: number, entry: Omit<TooltipEntry, "openMode">) => void;
  moveTouchTrigger: (triggerId: string, clientX: number, clientY: number) => void;
  endTouchTrigger: (triggerId: string, pointerType: string) => void;
  focusTrigger: (entry: Omit<TooltipEntry, "openMode">) => void;
  blurTrigger: (triggerId: string) => void;
  unmountTrigger: (triggerId: string) => void;
  registerGroup: (groupId: string, element: HTMLElement | null) => void;
  leaveGroup: (groupId: string, relatedTarget: EventTarget | null) => void;
  hideNow: () => void;
}

const TooltipContext = createContext<TooltipController | null>(null);
const TooltipGroupContext = createContext<string>("__default__");

function mergeRefs<T>(...refs: Array<Ref<T> | undefined>): (node: T | null) => void {
  return (node) => {
    for (const r of refs) {
      if (!r) continue;
      if (typeof r === "function") {
        r(node);
      } else {
        (r as { current: T | null }).current = node;
      }
    }
  };
}

function computeTooltipPosition(
  anchor: HTMLElement,
  tooltipEl: HTMLElement | null,
  preferredSide: TooltipSide,
  align: TooltipAlign,
): { left: number; top: number; side: TooltipSide } {
  const offset = 6;
  const margin = 8;
  const winW = typeof window !== "undefined" ? window.innerWidth || 1160 : 1160;
  const winH = typeof window !== "undefined" ? window.innerHeight || 740 : 740;
  const rect = anchor.getBoundingClientRect();
  const tipRect = tooltipEl?.getBoundingClientRect();
  const tipW = tipRect && tipRect.width > 0 ? tipRect.width : 120;
  const tipH = tipRect && tipRect.height > 0 ? tipRect.height : 28;

  let side = preferredSide;
  if (side === "top" && rect.top - offset - tipH < margin && rect.bottom + offset + tipH <= winH - margin) {
    side = "bottom";
  } else if (side === "bottom" && rect.bottom + offset + tipH > winH - margin && rect.top - offset - tipH >= margin) {
    side = "top";
  } else if (side === "right" && rect.right + offset + tipW > winW - margin && rect.left - offset - tipW >= margin) {
    side = "left";
  } else if (side === "left" && rect.left - offset - tipW < margin && rect.right + offset + tipW <= winW - margin) {
    side = "right";
  }

  let left = 0;
  let top = 0;

  if (side === "top" || side === "bottom") {
    top = side === "top" ? rect.top - tipH - offset : rect.bottom + offset;
    if (align === "start") {
      left = rect.left;
    } else if (align === "end") {
      left = rect.right - tipW;
    } else {
      left = rect.left + rect.width / 2 - tipW / 2;
    }
  } else {
    left = side === "left" ? rect.left - tipW - offset : rect.right + offset;
    if (align === "start") {
      top = rect.top;
    } else if (align === "end") {
      top = rect.bottom - tipH;
    } else {
      top = rect.top + rect.height / 2 - tipH / 2;
    }
  }

  left = Math.max(margin, Math.min(left, winW - tipW - margin));
  top = Math.max(margin, Math.min(top, winH - tipH - margin));

  return { left: Math.round(left), top: Math.round(top), side };
}

export function TooltipProvider({
  children,
  delayDuration = LABEL_OPEN_DELAY_MS,
  skipDelayDuration = LABEL_GROUP_GRACE_MS,
}: {
  children: ReactNode;
  delayDuration?: number;
  skipDelayDuration?: number;
}) {
  const domId = useId();
  const clockRef = useRef<OverlayTimerClock | null>(null);
  if (!clockRef.current) clockRef.current = new OverlayTimerClock();

  const [active, setActive] = useState<TooltipEntry | null>(null);
  const [phase, setPhase] = useState<"closed" | "entering" | "steady" | "exiting">("closed");
  const activeRef = useRef<TooltipEntry | null>(null);
  const phaseRef = useRef<"closed" | "entering" | "steady" | "exiting">("closed");
  activeRef.current = active;
  phaseRef.current = phase;

  const pendingTriggerIdRef = useRef<string | null>(null);
  const suppressedTriggerIdRef = useRef<string | null>(null);
  const pointerDownTriggerIdRef = useRef<string | null>(null);
  const lastTouchStartRef = useRef<{ triggerId: string; x: number; y: number } | null>(null);
  const touchLongPressTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const touchAutoHideTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const suppressNextClickRef = useRef(false);
  const lastClosedRef = useRef<{ groupId: string; at: number } | null>(null);
  const groupsRef = useRef(new Map<string, TooltipGroupRecord>());
  const tooltipElRef = useRef<HTMLDivElement | null>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({ left: -9999, top: -9999 });

  const clearTouchTimers = useCallback(() => {
    if (touchLongPressTimerRef.current !== undefined) {
      clearTimeout(touchLongPressTimerRef.current);
      touchLongPressTimerRef.current = undefined;
    }
    if (touchAutoHideTimerRef.current !== undefined) {
      clearTimeout(touchAutoHideTimerRef.current);
      touchAutoHideTimerRef.current = undefined;
    }
  }, []);

  const showEntry = useCallback((entry: TooltipEntry, inPlace: boolean) => {
    const clock = clockRef.current!;
    clock.cancelExit();
    clock.cancelClose();
    pendingTriggerIdRef.current = null;
    setActive(entry);
    setPhase(inPlace ? "steady" : "entering");
  }, []);

  const hideWithFade = useCallback((recordGroup = true) => {
    const clock = clockRef.current!;
    clock.cancelOpen();
    clock.cancelClose();
    pendingTriggerIdRef.current = null;
    const current = activeRef.current;
    if (!current || phaseRef.current === "exiting" || phaseRef.current === "closed") {
      return;
    }
    if (recordGroup) {
      lastClosedRef.current = { groupId: current.groupId, at: Date.now() };
    }
    setPhase("exiting");
    clock.scheduleExit(LABEL_EXIT_MS, () => {
      setActive(null);
      setPhase("closed");
    });
  }, []);

  const hideNow = useCallback(() => {
    const clock = clockRef.current!;
    clock.cancelAll();
    clearTouchTimers();
    pendingTriggerIdRef.current = null;
    if (activeRef.current) {
      lastClosedRef.current = { groupId: activeRef.current.groupId, at: Date.now() };
    }
    setActive(null);
    setPhase("closed");
  }, [clearTouchTimers]);

  const tooltipEscapeActive = Boolean(active && phase !== "exiting");

  useEscapeHandler(
    `tooltip-${domId}`,
    tooltipEscapeActive,
    () => {
      hideWithFade(false);
    },
    70,
  );

  useEffect(() => {
    if (!tooltipEscapeActive) return;
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented && !isGlobalShortcutsInstalled()) {
        e.preventDefault();
        hideWithFade(false);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [tooltipEscapeActive, hideWithFade]);

  useEffect(() => {
    if (!active) return;
    const onScrollOrBlur = () => hideNow();
    window.addEventListener("blur", onScrollOrBlur);
    window.addEventListener("scroll", onScrollOrBlur, true);
    return () => {
      window.removeEventListener("blur", onScrollOrBlur);
      window.removeEventListener("scroll", onScrollOrBlur, true);
    };
  }, [active, hideNow]);

  useEffect(() => {
    if (!suppressNextClickRef.current) return;
    const handler = (e: globalThis.MouseEvent) => {
      if (suppressNextClickRef.current) {
        suppressNextClickRef.current = false;
        e.preventDefault();
        e.stopPropagation();
      }
    };
    document.addEventListener("click", handler, true);
    return () => document.removeEventListener("click", handler, true);
  }, [active]);

  useEffect(() => {
    const clock = clockRef.current!;
    return () => {
      clock.cancelAll();
      clearTouchTimers();
    };
  }, [clearTouchTimers]);

  useLayoutEffect(() => {
    if (!active || !active.anchor.isConnected) return;
    const next = computeTooltipPosition(active.anchor, tooltipElRef.current, active.side, active.align);
    setPos((prev) => (prev.left === next.left && prev.top === next.top ? prev : next));
  }, [active, phase]);

  const enterTrigger = useCallback(
    (entry: Omit<TooltipEntry, "openMode">, pointerType: string) => {
      if (pointerType === "touch" || (!hasHoverPointer() && pointerType !== "mouse")) {
        return;
      }
      if (suppressedTriggerIdRef.current === entry.triggerId) {
        return;
      }
      const clock = clockRef.current!;
      const current = activeRef.current;
      const isShowing = Boolean(current && phaseRef.current !== "exiting" && phaseRef.current !== "closed");

      // Idempotent guard: pointerenter + mouseenter on same trigger
      if (
        (isShowing && current?.triggerId === entry.triggerId) ||
        pendingTriggerIdRef.current === entry.triggerId
      ) {
        clock.cancelClose();
        return;
      }

      const sameGroupOpen = Boolean(
        current &&
          current.groupId === entry.groupId &&
          (phaseRef.current === "entering" || phaseRef.current === "steady" || phaseRef.current === "exiting"),
      );

      const recentInGroup = Boolean(
        lastClosedRef.current &&
          lastClosedRef.current.groupId === entry.groupId &&
          Date.now() - lastClosedRef.current.at <= skipDelayDuration,
      );

      if (sameGroupOpen) {
        // Move & change text in place without fade-out-and-back
        showEntry({ ...entry, openMode: "hover" }, true);
        return;
      }

      if (recentInGroup) {
        showEntry({ ...entry, openMode: "hover" }, false);
        return;
      }

      pendingTriggerIdRef.current = entry.triggerId;
      const wait = entry.delayDuration ?? delayDuration;
      clock.scheduleOpen(wait, () => {
        if (suppressedTriggerIdRef.current === entry.triggerId) return;
        showEntry({ ...entry, openMode: "hover" }, false);
      });
    },
    [delayDuration, showEntry, skipDelayDuration],
  );

  const leaveTrigger = useCallback(
    (triggerId: string, anchor: HTMLElement, relatedTarget: EventTarget | null) => {
      if (relatedTarget instanceof Node && anchor.contains(relatedTarget)) {
        return;
      }
      if (suppressedTriggerIdRef.current === triggerId) {
        suppressedTriggerIdRef.current = null;
      }
      const clock = clockRef.current!;
      if (pendingTriggerIdRef.current === triggerId) {
        clock.cancelOpen();
        pendingTriggerIdRef.current = null;
      }
      const current = activeRef.current;
      if (!current || current.triggerId !== triggerId || current.openMode === "focus") {
        return;
      }

      const groupRec = groupsRef.current.get(current.groupId);
      const stillInsideExplicitGroup = Boolean(
        groupRec?.element && relatedTarget instanceof Node && groupRec.element.contains(relatedTarget),
      );

      if (stillInsideExplicitGroup) {
        // Stay visible across small button gaps inside a TooltipGroup; hide if pointer lingers in dead space
        clock.scheduleClose(90, () => {
          if (activeRef.current?.triggerId === triggerId) {
            hideWithFade(true);
          }
        });
        return;
      }

      hideWithFade(true);
    },
    [hideWithFade],
  );

  const pressTrigger = useCallback(
    (
      triggerId: string,
      _anchor: HTMLElement,
      pointerType: string,
      clientX: number,
      clientY: number,
      entry: Omit<TooltipEntry, "openMode">,
    ) => {
      const clock = clockRef.current!;
      clock.cancelOpen();
      clock.cancelClose();
      pendingTriggerIdRef.current = null;

      if (pointerType === "touch") {
        clearTouchTimers();
        lastTouchStartRef.current = { triggerId, x: clientX, y: clientY };
        touchLongPressTimerRef.current = setTimeout(() => {
          touchLongPressTimerRef.current = undefined;
          suppressNextClickRef.current = true;
          showEntry({ ...entry, openMode: "touch" }, false);
        }, TOUCH_LONG_PRESS_MS);
        return;
      }

      // Pressing hides the label and keeps it hidden until the pointer leaves that trigger
      suppressedTriggerIdRef.current = triggerId;
      pointerDownTriggerIdRef.current = triggerId;
      setTimeout(() => {
        if (pointerDownTriggerIdRef.current === triggerId) {
          pointerDownTriggerIdRef.current = null;
        }
      }, 0);

      if (activeRef.current?.triggerId === triggerId) {
        hideNow();
      }
    },
    [clearTouchTimers, hideNow, showEntry],
  );

  const moveTouchTrigger = useCallback(
    (triggerId: string, clientX: number, clientY: number) => {
      const start = lastTouchStartRef.current;
      if (!start || start.triggerId !== triggerId) return;
      if (Math.hypot(clientX - start.x, clientY - start.y) > 8) {
        clearTouchTimers();
        lastTouchStartRef.current = null;
      }
    },
    [clearTouchTimers],
  );

  const endTouchTrigger = useCallback(
    (triggerId: string, pointerType: string) => {
      if (pointerType !== "touch") return;
      if (touchLongPressTimerRef.current !== undefined) {
        clearTimeout(touchLongPressTimerRef.current);
        touchLongPressTimerRef.current = undefined;
      }
      lastTouchStartRef.current = null;
      if (activeRef.current?.triggerId === triggerId && activeRef.current.openMode === "touch") {
        touchAutoHideTimerRef.current = setTimeout(() => {
          touchAutoHideTimerRef.current = undefined;
          hideWithFade(false);
        }, 1200);
      }
    },
    [hideWithFade],
  );

  const focusTrigger = useCallback(
    (entry: Omit<TooltipEntry, "openMode">) => {
      if (pointerDownTriggerIdRef.current === entry.triggerId) {
        return;
      }
      showEntry({ ...entry, openMode: "focus" }, false);
    },
    [showEntry],
  );

  const blurTrigger = useCallback(
    (triggerId: string) => {
      if (activeRef.current?.triggerId === triggerId && activeRef.current.openMode === "focus") {
        hideWithFade(false);
      }
    },
    [hideWithFade],
  );

  const unmountTrigger = useCallback((triggerId: string) => {
    if (pendingTriggerIdRef.current === triggerId) {
      clockRef.current?.cancelOpen();
      pendingTriggerIdRef.current = null;
    }
    if (suppressedTriggerIdRef.current === triggerId) {
      suppressedTriggerIdRef.current = null;
    }
    if (activeRef.current?.triggerId === triggerId) {
      clockRef.current?.cancelAll();
      setActive(null);
      setPhase("closed");
    }
  }, []);

  const registerGroup = useCallback((groupId: string, element: HTMLElement | null) => {
    if (!element) {
      groupsRef.current.delete(groupId);
    } else {
      groupsRef.current.set(groupId, { id: groupId, element });
    }
  }, []);

  const leaveGroup = useCallback(
    (groupId: string, relatedTarget: EventTarget | null) => {
      const groupRec = groupsRef.current.get(groupId);
      if (groupRec?.element && relatedTarget instanceof Node && groupRec.element.contains(relatedTarget)) {
        return;
      }
      const current = activeRef.current;
      if (current && current.groupId === groupId && current.openMode === "hover") {
        hideWithFade(true);
      }
    },
    [hideWithFade],
  );

  const controller = useMemo<TooltipController>(
    () => ({
      domId,
      delayDuration,
      skipDelayDuration,
      activeTriggerId: active && phase !== "exiting" ? active.triggerId : null,
      enterTrigger,
      leaveTrigger,
      pressTrigger,
      moveTouchTrigger,
      endTouchTrigger,
      focusTrigger,
      blurTrigger,
      unmountTrigger,
      registerGroup,
      leaveGroup,
      hideNow,
    }),
    [
      active,
      blurTrigger,
      delayDuration,
      domId,
      endTouchTrigger,
      enterTrigger,
      focusTrigger,
      hideNow,
      leaveGroup,
      leaveTrigger,
      moveTouchTrigger,
      phase,
      pressTrigger,
      registerGroup,
      skipDelayDuration,
      unmountTrigger,
    ],
  );

  const shouldRender = Boolean(active && active.anchor.isConnected && phase !== "closed");

  return (
    <TooltipContext.Provider value={controller}>
      {children}
      {shouldRender && typeof document !== "undefined"
        ? createPortal(
            <div
              ref={tooltipElRef}
              id={domId}
              role="tooltip"
              data-testid="tulai-tooltip"
              data-state={phase}
              style={{ left: pos.left, top: pos.top }}
              className={cn(
                "pointer-events-none fixed z-[70] inline-flex items-center gap-1.5 rounded-[8px] border border-[#252F42] bg-[#0F172A] px-2.5 py-1 text-[11px] font-medium text-[#F1F5F9] shadow-lg select-none",
                "dark:border-[#334155] dark:bg-[#1E293B]",
                phase === "entering" && "animate-tulai-label-in",
                phase === "exiting" && "animate-tulai-label-out",
                active?.className,
              )}
            >
              <span>{active?.content}</span>
              {active?.shortcut ? (
                <kbd className="tnum rounded-[4px] bg-white/12 px-1 py-0.5 font-sans text-[10px] leading-none text-slate-300">
                  {active.shortcut}
                </kbd>
              ) : null}
            </div>,
            document.body,
          )
        : null}
    </TooltipContext.Provider>
  );
}

/**
 * Groups neighboring labels (e.g. a toolbar or sidebar rail) so moving between
 * triggers across button gaps keeps the label open and swaps text in place.
 */
export function TooltipGroup({
  children,
  className,
  onPointerLeave,
  onMouseLeave,
  ...rest
}: HTMLAttributes<HTMLDivElement> & { children: ReactNode }) {
  const groupId = useId();
  const ctx = useContext(TooltipContext);
  const ctxRef = useRef(ctx);
  ctxRef.current = ctx;
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const controller = ctxRef.current;
    if (!controller) return;
    controller.registerGroup(groupId, ref.current);
    return () => controller.registerGroup(groupId, null);
  }, [groupId]);

  return (
    <TooltipGroupContext.Provider value={groupId}>
      <div
        ref={ref}
        data-tooltip-group={groupId}
        className={cn("inline-flex items-center", className)}
        onPointerLeave={(e) => {
          onPointerLeave?.(e);
          ctx?.leaveGroup(groupId, e.relatedTarget);
        }}
        onMouseLeave={(e) => {
          onMouseLeave?.(e);
          ctx?.leaveGroup(groupId, e.relatedTarget);
        }}
        {...rest}
      >
        {children}
      </div>
    </TooltipGroupContext.Provider>
  );
}

export interface TooltipProps {
  content: ReactNode;
  shortcut?: string;
  side?: TooltipSide;
  align?: TooltipAlign;
  disabled?: boolean;
  delayDuration?: number;
  className?: string;
  children: ReactNode;
}

export function Tooltip({
  content,
  shortcut,
  side = "top",
  align = "center",
  disabled = false,
  delayDuration,
  className,
  children,
  ...rest
}: TooltipProps & Record<string, unknown>) {
  const ctx = useContext(TooltipContext);
  const ctxRef = useRef(ctx);
  ctxRef.current = ctx;
  const groupId = useContext(TooltipGroupContext);
  const triggerId = useId();
  const anchorRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    return () => {
      ctxRef.current?.unmountTrigger(triggerId);
    };
  }, [triggerId]);

  const child = Children.only(children);
  if (!isValidElement(child)) return <>{children}</>;

  if (disabled || !content || !ctx) {
    return Object.keys(rest).length > 0
      ? cloneElement(child as ReactElement<Record<string, unknown>>, rest)
      : <>{children}</>;
  }

  const childProps = child.props as Record<string, unknown> & {
    ref?: Ref<HTMLElement>;
    "aria-describedby"?: string;
    onPointerEnter?: (e: PointerEvent<HTMLElement>) => void;
    onPointerLeave?: (e: PointerEvent<HTMLElement>) => void;
    onPointerDown?: (e: PointerEvent<HTMLElement>) => void;
    onPointerMove?: (e: PointerEvent<HTMLElement>) => void;
    onPointerUp?: (e: PointerEvent<HTMLElement>) => void;
    onPointerCancel?: (e: PointerEvent<HTMLElement>) => void;
    onMouseEnter?: (e: MouseEvent<HTMLElement>) => void;
    onMouseLeave?: (e: MouseEvent<HTMLElement>) => void;
    onMouseDown?: (e: MouseEvent<HTMLElement>) => void;
    onClick?: (e: MouseEvent<HTMLElement>) => void;
    onKeyDown?: (e: KeyboardEvent<HTMLElement>) => void;
    onFocus?: (e: FocusEvent<HTMLElement>) => void;
    onBlur?: (e: FocusEvent<HTMLElement>) => void;
  };

  const restHandlers = rest as {
    ref?: Ref<HTMLElement>;
    "aria-describedby"?: string;
    onPointerEnter?: (e: PointerEvent<HTMLElement>) => void;
    onPointerLeave?: (e: PointerEvent<HTMLElement>) => void;
    onPointerDown?: (e: PointerEvent<HTMLElement>) => void;
    onPointerMove?: (e: PointerEvent<HTMLElement>) => void;
    onPointerUp?: (e: PointerEvent<HTMLElement>) => void;
    onPointerCancel?: (e: PointerEvent<HTMLElement>) => void;
    onMouseEnter?: (e: MouseEvent<HTMLElement>) => void;
    onMouseLeave?: (e: MouseEvent<HTMLElement>) => void;
    onMouseDown?: (e: MouseEvent<HTMLElement>) => void;
    onClick?: (e: MouseEvent<HTMLElement>) => void;
    onKeyDown?: (e: KeyboardEvent<HTMLElement>) => void;
    onFocus?: (e: FocusEvent<HTMLElement>) => void;
    onBlur?: (e: FocusEvent<HTMLElement>) => void;
  };

  const makeEntry = (anchor: HTMLElement): Omit<TooltipEntry, "openMode"> => ({
    triggerId,
    anchor,
    content,
    shortcut,
    side,
    align,
    groupId,
    delayDuration,
    className,
  });

  const isDescribed = ctx.activeTriggerId === triggerId;
  const existingDescribedBy = [childProps["aria-describedby"], restHandlers["aria-describedby"]]
    .filter(Boolean)
    .join(" ");
  const ariaDescribedBy = isDescribed
    ? [existingDescribedBy, ctx.domId].filter(Boolean).join(" ")
    : existingDescribedBy || undefined;

  return cloneElement(child as ReactElement<Record<string, unknown>>, {
    ...rest,
    ref: mergeRefs(anchorRef, childProps.ref, restHandlers.ref),
    "aria-describedby": ariaDescribedBy,
    onPointerEnter: (e: PointerEvent<HTMLElement>) => {
      childProps.onPointerEnter?.(e);
      restHandlers.onPointerEnter?.(e);
      const anchor = anchorRef.current ?? e.currentTarget;
      ctx.enterTrigger(makeEntry(anchor), e.pointerType || "mouse");
    },
    onMouseEnter: (e: MouseEvent<HTMLElement>) => {
      childProps.onMouseEnter?.(e);
      restHandlers.onMouseEnter?.(e);
      const anchor = anchorRef.current ?? e.currentTarget;
      ctx.enterTrigger(makeEntry(anchor), "mouse");
    },
    onPointerLeave: (e: PointerEvent<HTMLElement>) => {
      childProps.onPointerLeave?.(e);
      restHandlers.onPointerLeave?.(e);
      const anchor = anchorRef.current ?? e.currentTarget;
      ctx.leaveTrigger(triggerId, anchor, e.relatedTarget);
    },
    onMouseLeave: (e: MouseEvent<HTMLElement>) => {
      childProps.onMouseLeave?.(e);
      restHandlers.onMouseLeave?.(e);
      const anchor = anchorRef.current ?? e.currentTarget;
      ctx.leaveTrigger(triggerId, anchor, e.relatedTarget);
    },
    onPointerDown: (e: PointerEvent<HTMLElement>) => {
      childProps.onPointerDown?.(e);
      restHandlers.onPointerDown?.(e);
      const anchor = anchorRef.current ?? e.currentTarget;
      ctx.pressTrigger(
        triggerId,
        anchor,
        e.pointerType || "mouse",
        e.clientX,
        e.clientY,
        makeEntry(anchor),
      );
    },
    onMouseDown: (e: MouseEvent<HTMLElement>) => {
      childProps.onMouseDown?.(e);
      restHandlers.onMouseDown?.(e);
      const anchor = anchorRef.current ?? e.currentTarget;
      ctx.pressTrigger(triggerId, anchor, "mouse", e.clientX, e.clientY, makeEntry(anchor));
    },
    onClick: (e: MouseEvent<HTMLElement>) => {
      childProps.onClick?.(e);
      restHandlers.onClick?.(e);
    },
    onKeyDown: (e: KeyboardEvent<HTMLElement>) => {
      childProps.onKeyDown?.(e);
      restHandlers.onKeyDown?.(e);
    },
    onPointerMove: (e: PointerEvent<HTMLElement>) => {
      childProps.onPointerMove?.(e);
      restHandlers.onPointerMove?.(e);
      if (e.pointerType === "touch") {
        ctx.moveTouchTrigger(triggerId, e.clientX, e.clientY);
      }
    },
    onPointerUp: (e: PointerEvent<HTMLElement>) => {
      childProps.onPointerUp?.(e);
      restHandlers.onPointerUp?.(e);
      ctx.endTouchTrigger(triggerId, e.pointerType || "mouse");
    },
    onPointerCancel: (e: PointerEvent<HTMLElement>) => {
      childProps.onPointerCancel?.(e);
      restHandlers.onPointerCancel?.(e);
      ctx.endTouchTrigger(triggerId, e.pointerType || "mouse");
    },
    onFocus: (e: FocusEvent<HTMLElement>) => {
      childProps.onFocus?.(e);
      restHandlers.onFocus?.(e);
      const anchor = anchorRef.current ?? e.currentTarget;
      ctx.focusTrigger(makeEntry(anchor));
    },
    onBlur: (e: FocusEvent<HTMLElement>) => {
      childProps.onBlur?.(e);
      restHandlers.onBlur?.(e);
      ctx.blurTrigger(triggerId);
    },
  });
}
