import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { cn } from "@/lib/cn";
import { Icon } from "./Icon";

export interface TabOption {
  value: string;
  label: string;
  icon?: string;
  badge?: string | number;
  disabled?: boolean;
}

export interface TabsProps {
  options: TabOption[];
  value: string;
  onValueChange: (value: string) => void;
  variant?: "segmented" | "pills";
  size?: "small" | "default";
  fullWidth?: boolean;
  className?: string;
  "data-testid"?: string;
}

export function Tabs({
  options,
  value,
  onValueChange,
  variant = "segmented",
  size = "default",
  fullWidth = false,
  className,
  "data-testid": testId,
}: TabsProps) {
  const small = size === "small";
  const listRef = useRef<HTMLDivElement | null>(null);
  const tabRefs = useRef<Map<string, HTMLButtonElement>>(new Map());
  const [thumb, setThumb] = useState<{ left: number; width: number; ready: boolean }>({
    left: 0,
    width: 0,
    ready: false,
  });

  const measureThumb = useCallback(() => {
    if (variant !== "segmented") {
      setThumb((prev) => (prev.ready ? { left: 0, width: 0, ready: false } : prev));
      return;
    }
    const activeBtn = tabRefs.current.get(value);
    if (!activeBtn || activeBtn.offsetWidth <= 0) {
      setThumb((prev) => (prev.ready ? { left: 0, width: 0, ready: false } : prev));
      return;
    }
    const nextLeft = activeBtn.offsetLeft;
    const nextWidth = activeBtn.offsetWidth;
    setThumb((prev) =>
      prev.ready && prev.left === nextLeft && prev.width === nextWidth
        ? prev
        : { left: nextLeft, width: nextWidth, ready: true },
    );
  }, [value, variant]);

  useLayoutEffect(() => {
    measureThumb();
    const listEl = listRef.current;
    if (!listEl || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => measureThumb());
    ro.observe(listEl);
    return () => ro.disconnect();
  }, [measureThumb, options.length, size, fullWidth]);

  return (
    <div
      ref={listRef}
      role="tablist"
      data-testid={testId}
      className={cn(
        "relative",
        fullWidth ? "flex w-full items-center gap-0.5 select-none" : "inline-flex items-center gap-0.5 select-none",
        variant === "segmented"
          ? "rounded-[12px] border border-hairline/70 bg-sunken p-0.5"
          : "gap-1",
        className,
      )}
    >
      {variant === "segmented" && thumb.ready ? (
        <span
          aria-hidden="true"
          style={{
            width: `${thumb.width}px`,
            transform: `translate3d(${thumb.left}px, 0, 0)`,
          }}
          className={cn(
            "pointer-events-none absolute top-0.5 bottom-0.5 left-0 rounded-[9px] bg-accent shadow-[0_1px_2px_rgba(37,99,235,0.16)] transition-[transform,width] duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] dark:shadow-none",
          )}
        />
      ) : null}

      {options.map((opt) => {
        const active = opt.value === value;
        return (
          <button
            key={opt.value}
            ref={(el) => {
              if (el) tabRefs.current.set(opt.value, el);
              else tabRefs.current.delete(opt.value);
            }}
            type="button"
            role="tab"
            aria-selected={active}
            disabled={opt.disabled}
            onClick={() => {
              if (!opt.disabled && !active) onValueChange(opt.value);
            }}
            className={cn(
              "relative z-10 inline-flex items-center justify-center gap-1.5 whitespace-nowrap transition-[background-color,color,box-shadow] duration-150 cursor-pointer",
              "disabled:cursor-not-allowed disabled:opacity-45",
              fullWidth && "min-w-0 flex-1",
              variant === "segmented" ? "rounded-[9px]" : "rounded-full",
              small ? "h-[26px] px-2.5 text-xs" : "h-7 px-3 text-xs",
              active
                ? cn(
                    "font-semibold text-on-accent",
                    !(variant === "segmented" && thumb.ready) &&
                      "bg-accent shadow-[0_1px_2px_rgba(37,99,235,0.16)] dark:shadow-none",
                  )
                : "text-muted hover:text-ink hover:bg-sunken-hover/70 font-medium",
            )}
          >
            {opt.icon ? <Icon name={opt.icon} size={13} /> : null}
            <span>{opt.label}</span>
            {opt.badge !== undefined ? (
              <span
                className={cn(
                  "tnum rounded-full px-1.5 text-[10px] leading-4 transition-colors duration-150",
                  active ? "bg-white/20 text-on-accent dark:bg-black/20" : "bg-card text-muted",
                )}
              >
                {opt.badge}
              </span>
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
