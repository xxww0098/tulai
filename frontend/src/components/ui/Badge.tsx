import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "@/lib/cn";

export type BadgeVariant = "accent" | "success" | "warn" | "danger" | "neutral";

const VARIANT_CLASSES: Record<BadgeVariant, string> = {
  accent:
    "border-accent-border bg-accent-soft text-accent",
  success:
    "border-[var(--status-ok-border)] bg-[var(--status-ok-bg)] text-status-ok",
  warn:
    "border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] text-status-warn",
  danger:
    "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger",
  neutral:
    "border-hairline bg-sunken text-muted",
};

const DOT_CLASSES: Record<BadgeVariant, string> = {
  accent: "bg-accent",
  success: "bg-status-ok",
  warn: "bg-status-warn",
  danger: "bg-status-danger",
  neutral: "bg-subtle",
};

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  variant?: BadgeVariant;
  text?: string;
  dot?: boolean;
  pulseDot?: boolean;
  children?: ReactNode;
}

export function Badge({
  variant = "neutral",
  text,
  dot = false,
  pulseDot = false,
  className,
  children,
  ...rest
}: BadgeProps) {
  return (
    <span
      className={cn(
        "tnum inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-medium leading-4 whitespace-nowrap transition-colors duration-150 select-none",
        VARIANT_CLASSES[variant],
        className,
      )}
      {...rest}
    >
      {dot ? (
        <span className="relative inline-flex size-1.5 shrink-0 items-center justify-center">
          {pulseDot ? (
            <span
              aria-hidden="true"
              className={cn(
                "animate-tulai-live-ring absolute inset-0 rounded-full",
                DOT_CLASSES[variant],
              )}
            />
          ) : null}
          <span className={cn("relative size-1.5 rounded-full", DOT_CLASSES[variant])} />
        </span>
      ) : null}
      {text !== undefined ? text : children}
    </span>
  );
}

export interface OutlineBadgeProps extends HTMLAttributes<HTMLSpanElement> {
  text?: string;
  children?: ReactNode;
}

export function OutlineBadge({ text, className, children, ...rest }: OutlineBadgeProps) {
  return (
    <span
      className={cn(
        "tnum inline-flex shrink-0 items-center justify-center rounded-full border border-strong bg-transparent px-2 py-0.5 text-[11px] font-medium leading-4 text-ink whitespace-nowrap select-none",
        className,
      )}
      {...rest}
    >
      {text !== undefined ? text : children}
    </span>
  );
}
