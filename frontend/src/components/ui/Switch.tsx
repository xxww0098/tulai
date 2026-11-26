import * as SwitchPrimitive from "@radix-ui/react-switch";
import { cn } from "@/lib/cn";

export interface SwitchProps {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  size?: "small" | "default";
  label?: string;
  className?: string;
  "data-testid"?: string;
  "aria-label"?: string;
}

export function Switch({
  checked,
  onCheckedChange,
  disabled = false,
  size = "default",
  label,
  className,
  "data-testid": testId,
  "aria-label": ariaLabel,
}: SwitchProps) {
  const small = size === "small";
  const control = (
    <SwitchPrimitive.Root
      checked={checked}
      onCheckedChange={onCheckedChange}
      disabled={disabled}
      data-testid={testId}
      aria-label={ariaLabel ?? label}
      className={cn(
        "peer relative inline-flex shrink-0 cursor-pointer items-center rounded-full border border-transparent transition-colors duration-150",
        "bg-[var(--track-bg)] data-[state=checked]:bg-accent",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--focus-ring)]",
        "disabled:cursor-not-allowed disabled:opacity-45",
        small ? "h-[18px] w-[32px]" : "h-[20px] w-[36px]",
        !label && className,
      )}
    >
      <SwitchPrimitive.Thumb
        className={cn(
          "pointer-events-none block rounded-full bg-[var(--knob-bg)] shadow-xs transition-transform duration-150",
          small
            ? "size-[14px] translate-x-[1px] data-[state=checked]:translate-x-[15px]"
            : "size-[16px] translate-x-[1px] data-[state=checked]:translate-x-[17px]",
        )}
      />
    </SwitchPrimitive.Root>
  );

  if (!label) return control;

  return (
    <label
      className={cn(
        "inline-flex items-center gap-2 text-xs text-ink select-none",
        disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer",
        className,
      )}
    >
      {control}
      <span>{label}</span>
    </label>
  );
}
