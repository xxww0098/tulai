import { cn } from "@/lib/cn";

export type ProgressTone = "accent" | "success" | "warn" | "danger";

const TONE_CLASSES: Record<ProgressTone, string> = {
  accent: "bg-accent",
  success: "bg-status-ok",
  warn: "bg-status-warn",
  danger: "bg-status-danger",
};

const SIZE_CLASSES = {
  xs: "h-1",
  sm: "h-1.5",
  md: "h-2",
} as const;

export interface ProgressProps {
  /** Fraction between 0 and max (defaults to 0..1 to match legacy DTO progress values). */
  value: number;
  max?: number;
  tone?: ProgressTone;
  size?: "xs" | "sm" | "md";
  indeterminate?: boolean;
  active?: boolean;
  className?: string;
  "aria-label"?: string;
  "data-testid"?: string;
}

export function Progress({
  value,
  max = 1,
  tone = "accent",
  size = "sm",
  indeterminate = false,
  active = false,
  className,
  "aria-label": ariaLabel,
  "data-testid": testId,
}: ProgressProps) {
  const safeMax = max > 0 ? max : 1;
  const pct = indeterminate ? 0 : Math.max(0, Math.min(100, (value / safeMax) * 100));

  return (
    <div
      role="progressbar"
      aria-valuenow={indeterminate ? undefined : Math.round(pct)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={ariaLabel}
      data-testid={testId}
      data-indeterminate={indeterminate ? "true" : undefined}
      className={cn(
        "relative w-full overflow-hidden rounded-full bg-sunken-hover",
        SIZE_CLASSES[size],
        className,
      )}
    >
      {indeterminate ? (
        <div
          className={cn(
            "tulai-progress-indeterminate h-full w-[38%] rounded-full",
            TONE_CLASSES[tone],
          )}
        />
      ) : (
        <div
          className={cn(
            "relative h-full overflow-hidden rounded-full transition-[width] duration-150 ease-out",
            TONE_CLASSES[tone],
          )}
          style={{ width: `${pct}%` }}
        >
          {active && pct > 0 && pct < 100 ? (
            <span
              aria-hidden="true"
              className="tulai-progress-shimmer pointer-events-none absolute inset-y-0 left-0 w-1/2"
            />
          ) : null}
        </div>
      )}
    </div>
  );
}
