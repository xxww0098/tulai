import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type CompositionEvent,
  type KeyboardEvent,
  type PointerEvent,
  type WheelEvent,
} from "react";
import { getDrawerFrameUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import { Browser, fireIpc } from "@/lib/ipc";
import { useBrowserDrawer, useShell, type BrowserInputDTO } from "@/store";

function pointerButtonName(button: number): string {
  if (button === 1) return "middle";
  if (button === 2) return "right";
  return "left";
}

function pointerButtonMask(button: number): number {
  if (button === 1) return 4;
  if (button === 2) return 2;
  return 1;
}

export interface BrowserStageProps {
  className?: string;
  "data-testid"?: string;
}

/**
 * Interactive live CDP viewport replacing legacy `tulai/native` BrowserStage.
 * Double-buffers frame URLs for zero-flicker streaming and maps pointer, wheel,
 * keyboard, CJK IME composition, and clipboard paste to `Browser.sendBrowserDrawerInput`.
 */
export function BrowserStage({ className, "data-testid": testId }: BrowserStageProps) {
  const st = useBrowserDrawer();
  const shell = useShell();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const inputSinkRef = useRef<HTMLTextAreaElement | null>(null);
  const [stageSize, setStageSize] = useState({ w: 0, h: 0 });
  const [displayedSrc, setDisplayedSrc] = useState("");
  const pendingMoveRef = useRef<BrowserInputDTO | null>(null);
  const moveRafRef = useRef<number | null>(null);
  const capturedPointerRef = useRef<{ id: number; button: string } | null>(null);

  const nextFrameSrc = getDrawerFrameUrl(st.frameUrl, st.frameVersion, shell.mediaOrigin);

  // Measure container accurately for letterboxed coordinate mapping
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const update = () => {
      const rect = el.getBoundingClientRect();
      setStageSize({ w: rect.width, h: rect.height });
    };
    update();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Double-buffer frame loading so live frames never flash blank between updates
  useEffect(() => {
    if (!nextFrameSrc) {
      setDisplayedSrc("");
      return;
    }
    let canceled = false;
    const img = new Image();
    img.onload = () => {
      if (!canceled) setDisplayedSrc(nextFrameSrc);
    };
    img.onerror = () => {
      if (!canceled && !displayedSrc) setDisplayedSrc(nextFrameSrc);
    };
    img.src = nextFrameSrc;
    return () => {
      canceled = true;
    };
  }, [nextFrameSrc]);

  useEffect(() => {
    return () => {
      if (moveRafRef.current !== null) cancelAnimationFrame(moveRafRef.current);
    };
  }, []);

  const toPageCoords = useCallback(
    (clientX: number, clientY: number, clamp = false): { x: number; y: number } | null => {
      const el = containerRef.current;
      const vw = st.width;
      const vh = st.height;
      const sw = stageSize.w || el?.clientWidth || 0;
      const sh = stageSize.h || el?.clientHeight || 0;
      if (!el || sw <= 0 || sh <= 0 || vw <= 0 || vh <= 0) return null;

      const rect = el.getBoundingClientRect();
      const localX = clientX - rect.left;
      const localY = clientY - rect.top;

      const scale = Math.min(sw / vw, sh / vh);
      const rw = vw * scale;
      const rh = vh * scale;
      const ix = (sw - rw) / 2;
      const iy = (sh - rh) / 2;

      const rawX = ((localX - ix) * vw) / rw;
      const rawY = ((localY - iy) * vh) / rh;
      if (!Number.isFinite(rawX) || !Number.isFinite(rawY)) return null;
      if (clamp) {
        return {
          x: Math.max(0, Math.min(vw, rawX)),
          y: Math.max(0, Math.min(vh, rawY)),
        };
      }
      if (rawX < 0 || rawY < 0 || rawX > vw || rawY > vh) return null;
      return { x: rawX, y: rawY };
    },
    [st.width, st.height, stageSize.w, stageSize.h],
  );

  const sendInput = useCallback((payload: BrowserInputDTO) => {
    fireIpc(() => Browser.sendBrowserDrawerInput(payload));
  }, []);

  const flushPendingMove = useCallback(() => {
    if (moveRafRef.current !== null) {
      cancelAnimationFrame(moveRafRef.current);
      moveRafRef.current = null;
    }
    const payload = pendingMoveRef.current;
    pendingMoveRef.current = null;
    if (payload) sendInput(payload);
  }, [sendInput]);

  const focusSink = useCallback(() => {
    inputSinkRef.current?.focus({ preventScroll: true });
  }, []);

  const handlePointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.stopPropagation();
    focusSink();
    const pt = toPageCoords(e.clientX, e.clientY);
    if (!pt) return;
    const button = pointerButtonName(e.button);
    try {
      e.currentTarget.setPointerCapture?.(e.pointerId);
      capturedPointerRef.current = { id: e.pointerId, button };
    } catch {
      capturedPointerRef.current = { id: e.pointerId, button };
    }
    sendInput({
      kind: "mousePressed",
      button,
      buttons: e.buttons || pointerButtonMask(e.button),
      x: pt.x,
      y: pt.y,
      clicks: Math.max(1, e.detail || 1),
      alt: e.altKey,
      control: e.ctrlKey,
      meta: e.metaKey,
      shift: e.shiftKey,
    });
  };

  const finishPointerDrag = (e: PointerEvent<HTMLDivElement>, canceled: boolean) => {
    const captured = capturedPointerRef.current?.id === e.pointerId;
    if (captured) {
      capturedPointerRef.current = null;
      try {
        if (e.currentTarget.hasPointerCapture?.(e.pointerId)) {
          e.currentTarget.releasePointerCapture?.(e.pointerId);
        }
      } catch {
        // Ignore release errors when element unmounts
      }
    }
    flushPendingMove();
    const pt = toPageCoords(e.clientX, e.clientY, captured);
    if (!pt) return;
    sendInput({
      kind: "mouseReleased",
      button: pointerButtonName(e.button),
      buttons: canceled ? 0 : e.buttons,
      x: pt.x,
      y: pt.y,
      clicks: Math.max(1, e.detail || 1),
      alt: e.altKey,
      control: e.ctrlKey,
      meta: e.metaKey,
      shift: e.shiftKey,
    });
  };

  const handlePointerUp = (e: PointerEvent<HTMLDivElement>) => {
    e.stopPropagation();
    finishPointerDrag(e, false);
  };

  const handlePointerCancel = (e: PointerEvent<HTMLDivElement>) => {
    if (capturedPointerRef.current?.id !== e.pointerId) return;
    e.stopPropagation();
    finishPointerDrag(e, true);
  };

  const handlePointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const captured = capturedPointerRef.current?.id === e.pointerId;
    const pt = toPageCoords(e.clientX, e.clientY, captured);
    if (!pt) return;
    pendingMoveRef.current = {
      kind: "mouseMoved",
      buttons: e.buttons,
      x: pt.x,
      y: pt.y,
      alt: e.altKey,
      control: e.ctrlKey,
      meta: e.metaKey,
      shift: e.shiftKey,
    };
    if (moveRafRef.current === null) {
      moveRafRef.current = requestAnimationFrame(() => {
        moveRafRef.current = null;
        const payload = pendingMoveRef.current;
        pendingMoveRef.current = null;
        if (payload) sendInput(payload);
      });
    }
  };

  const handleWheel = (e: WheelEvent<HTMLDivElement>) => {
    const pt = toPageCoords(e.clientX, e.clientY);
    if (!pt) return;
    e.preventDefault();
    sendInput({
      kind: "mouseWheel",
      x: pt.x,
      y: pt.y,
      dx: e.deltaX,
      dy: e.deltaY,
      alt: e.altKey,
      control: e.ctrlKey,
      meta: e.metaKey,
      shift: e.shiftKey,
    });
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.nativeEvent.isComposing || e.key === "Process") return;
    if (e.key === "Escape") {
      // Allow global Escape stack to close BrowserDrawer cleanly
      return;
    }

    const mod = e.ctrlKey || e.metaKey;
    const lower = e.key.toLowerCase();

    if (mod && lower === "c") {
      e.preventDefault();
      sendInput({ kind: "copy" });
      return;
    }
    if (mod && lower === "x") {
      e.preventDefault();
      sendInput({ kind: "cut" });
      return;
    }
    if (mod && lower === "v") {
      // Handled by native onPaste on the focused textarea so clipboard text arrives cleanly
      return;
    }

    // Single printable character without Ctrl/Meta/Alt
    if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault();
      sendInput({ kind: "text", text: e.key });
      return;
    }

    // Navigation & control keys
    e.preventDefault();
    sendInput({
      kind: "keyDown",
      key: e.key,
      alt: e.altKey,
      control: e.ctrlKey,
      meta: e.metaKey,
      shift: e.shiftKey,
    });
  };

  const handleCompositionEnd = (e: CompositionEvent<HTMLTextAreaElement>) => {
    const text = e.data || e.currentTarget.value;
    e.currentTarget.value = "";
    if (text) {
      sendInput({ kind: "text", text });
    }
  };

  const handlePaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    e.preventDefault();
    const text = e.clipboardData?.getData("text/plain");
    if (text) {
      sendInput({ kind: "text", text });
    }
  };

  return (
    <div
      ref={containerRef}
      data-testid={testId}
      aria-label="内置浏览器画面"
      onPointerDown={handlePointerDown}
      onPointerUp={handlePointerUp}
      onPointerCancel={handlePointerCancel}
      onPointerMove={handlePointerMove}
      onWheel={handleWheel}
      onContextMenu={(e) => e.preventDefault()}
      className={cn(
        "relative flex min-h-0 w-full flex-1 items-center justify-center overflow-hidden bg-slate-950 select-none outline-none",
        className,
      )}
    >
      {/* Hidden native input sink for reliable CJK IME & Cmd/Ctrl+V paste */}
      <textarea
        ref={inputSinkRef}
        aria-hidden="true"
        tabIndex={0}
        onKeyDown={handleKeyDown}
        onCompositionEnd={handleCompositionEnd}
        onPaste={handlePaste}
        onChange={(e) => {
          e.currentTarget.value = "";
        }}
        className="pointer-events-none fixed -left-[9999px] top-0 size-1 opacity-0"
      />

      {displayedSrc ? (
        <img
          src={displayedSrc}
          alt="内置浏览器画面"
          draggable={false}
          className="pointer-events-none h-full w-full object-contain select-none"
        />
      ) : (
        <div className="flex flex-col items-center gap-1.5 text-slate-400">
          <span className="text-xs">
            {st.err ? "无法显示页面画面" : "正在连接内置浏览器…"}
          </span>
        </div>
      )}
    </div>
  );
}
