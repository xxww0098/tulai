import { cn } from "@/lib/cn";

export type ProgressTone = "accent" | "success" | "warn" | "danger";

const TONE_CLASSES: Record<ProgressTone, string> = {
  accent: "bg-accent",
  success: "bg-status-ok",
  warn: "bg-status-warn",
  danger: "bg-status-danger",
};

export interface ProgressProps {
  /** Fraction between 0 and max (defaults to 0..1 to match legacy DTO progress values). */
  value: number;
  max?: number;
  tone?: ProgressTone;
  size?: "sm" | "md";
  className?: string;
  "aria-label"?: string;
  "data-testid"?: string;
}

export function Progress({
  value,
  max = 1,
  tone = "accent",
  size = "sm",
  className,
  "aria-label": ariaLabel,
  "data-testid": testId,
}: ProgressProps) {
  const safeMax = max > 0 ? max : 1;
  const pct = Math.max(0, Math.min(100, (value / safeMax) * 100));

  return (
    <div
      role="progressbar"
      aria-valuenow={Math.round(pct)}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={ariaLabel}
      data-testid={testId}
      className={cn(
        "w-full overflow-hidden rounded-full bg-sunken-hover",
        size === "sm" ? "h-1.5" : "h-2",
        className,
      )}
    >
      <div
        className={cn(
          "h-full rounded-full transition-[width] duration-200 ease-out",
          TONE_CLASSES[tone],
        )}
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}
