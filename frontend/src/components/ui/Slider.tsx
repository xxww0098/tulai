import * as SliderPrimitive from "@radix-ui/react-slider";
import { cn } from "@/lib/cn";

export interface SliderProps {
  value: number;
  onValueChange: (value: number) => void;
  onValueCommit?: (value: number) => void;
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
  className?: string;
  "aria-label"?: string;
  "data-testid"?: string;
}

export function Slider({
  value,
  onValueChange,
  onValueCommit,
  min = 0,
  max = 100,
  step = 1,
  disabled = false,
  className,
  "aria-label": ariaLabel,
  "data-testid": testId,
}: SliderProps) {
  const clamped = Number.isFinite(value) ? Math.max(min, Math.min(max, value)) : min;
  return (
    <SliderPrimitive.Root
      value={[clamped]}
      min={min}
      max={max}
      step={step}
      disabled={disabled}
      data-testid={testId}
      aria-label={ariaLabel}
      onValueChange={(vals) => {
        if (vals.length > 0) onValueChange(vals[0]);
      }}
      onValueCommit={(vals) => {
        if (vals.length > 0) onValueCommit?.(vals[0]);
      }}
      className={cn(
        "relative flex h-5 w-full touch-none items-center select-none",
        disabled ? "cursor-not-allowed opacity-45" : "cursor-pointer",
        className,
      )}
    >
      <SliderPrimitive.Track className="relative h-1.5 w-full grow overflow-hidden rounded-full bg-sunken-hover">
        <SliderPrimitive.Range className="absolute h-full rounded-full bg-accent" />
      </SliderPrimitive.Track>
      <SliderPrimitive.Thumb
        className={cn(
          "block size-3.5 rounded-full border-2 border-accent bg-card shadow-xs transition-transform duration-100",
          "hover:scale-110 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--focus-ring)]",
        )}
      />
    </SliderPrimitive.Root>
  );
}
