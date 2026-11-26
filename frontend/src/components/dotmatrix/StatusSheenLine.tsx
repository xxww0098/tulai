import { useEffect, useRef, useState, type CSSProperties } from "react";
import { cn } from "@/lib/cn";

export interface StatusSheenLineProps {
  text: string;
  /** When true, sweeps a 96px soft-edged full-ink window across the text every 1.7s. */
  active?: boolean;
  /** Tone override for alert / error / settled states. */
  tone?: "default" | "done" | "warn" | "danger" | "paused";
  /** Typography scale: "lg" uses 20px type (Section 2), "sm" fits compact workbench cards. */
  size?: "sm" | "lg";
  className?: string;
  "data-testid"?: string;
}

const SHEEN_WINDOW_PX = 96;

/**
 * Status line with a moving 96px full-ink sheen window (Section 2).
 * Moves the window and an inner copy of the text in opposite directions via CSS
 * transforms so the glyphs remain visually stationary and never trigger repaints.
 */
export function StatusSheenLine({
  text,
  active = false,
  tone = "default",
  size = "sm",
  className,
  "data-testid": testId,
}: StatusSheenLineProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [widthPx, setWidthPx] = useState(240);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const measure = () => {
      const w = Math.max(80, Math.round(el.getBoundingClientRect().width || el.scrollWidth || 240));
      setWidthPx(w);
    };
    measure();
    if (typeof ResizeObserver === "function") {
      const ro = new ResizeObserver(measure);
      ro.observe(el);
      return () => ro.disconnect();
    }
    return undefined;
  }, [text]);

  const travelPx = widthPx + SHEEN_WINDOW_PX;
  const styleVars = {
    "--sheen-travel": `${travelPx}px`,
    "--sheen-window": `${SHEEN_WINDOW_PX}px`,
  } as CSSProperties;

  const sizeClass =
    size === "lg"
      ? "text-[20px] leading-[1.35] font-medium tracking-[-0.01em]"
      : "text-[12px] leading-[1.4] font-medium";

  const baseToneClass =
    tone === "danger"
      ? "text-status-danger"
      : tone === "warn"
        ? "text-status-warn"
        : tone === "done"
          ? "text-ink"
          : tone === "paused"
            ? "text-muted"
            : "text-ink/42";

  const showSheen = active && tone === "default";

  return (
    <div
      ref={containerRef}
      data-testid={testId}
      data-sheen-active={showSheen ? "true" : "false"}
      style={styleVars}
      className={cn(
        "relative inline-block max-w-full overflow-hidden align-middle select-none",
        sizeClass,
        className,
      )}
    >
      {/* Dim base text */}
      <span className={cn("block truncate whitespace-nowrap transition-colors duration-300", baseToneClass)}>
        {text}
      </span>

      {/* Moving 96px full-ink window with counter-translated copy (zero text repaint) */}
      {showSheen ? (
        <span
          aria-hidden="true"
          className="tulai-sheen-window pointer-events-none absolute inset-y-0 left-0 overflow-hidden"
          style={{
            width: `${SHEEN_WINDOW_PX}px`,
            maskImage:
              "linear-gradient(90deg, transparent 0%, #000 28%, #000 72%, transparent 100%)",
            WebkitMaskImage:
              "linear-gradient(90deg, transparent 0%, #000 28%, #000 72%, transparent 100%)",
          }}
        >
          <span
            className="tulai-sheen-counter block whitespace-nowrap text-ink"
            style={{ width: `${widthPx}px` }}
          >
            {text}
          </span>
        </span>
      ) : null}
    </div>
  );
}
