import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import { Check, Minus } from "lucide-react";
import { cn } from "@/lib/cn";

export interface CheckboxProps {
  checked: boolean | "indeterminate";
  onCheckedChange: (checked: boolean) => void;
  label?: string;
  disabled?: boolean;
  size?: "small" | "default";
  className?: string;
  "data-testid"?: string;
  "aria-label"?: string;
}

export function Checkbox({
  checked,
  onCheckedChange,
  label,
  disabled = false,
  size = "default",
  className,
  "data-testid": testId,
  "aria-label": ariaLabel,
}: CheckboxProps) {
  const small = size === "small";
  const box = (
    <CheckboxPrimitive.Root
      checked={checked}
      onCheckedChange={(v) => onCheckedChange(v === true)}
      disabled={disabled}
      data-testid={testId}
      aria-label={ariaLabel ?? label}
      className={cn(
        "peer inline-flex shrink-0 cursor-pointer items-center justify-center rounded-[5px] border transition-colors duration-150",
        "border-strong bg-field text-on-accent",
        "hover:border-accent data-[state=checked]:border-accent data-[state=checked]:bg-accent",
        "data-[state=indeterminate]:border-accent data-[state=indeterminate]:bg-accent",
        "disabled:cursor-not-allowed disabled:opacity-45",
        small ? "size-3.5" : "size-4",
        !label && className,
      )}
    >
      <CheckboxPrimitive.Indicator className="flex items-center justify-center">
        {checked === "indeterminate" ? (
          <Minus size={small ? 10 : 11} strokeWidth={2.5} />
        ) : (
          <Check size={small ? 10 : 11} strokeWidth={2.6} />
        )}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );

  if (!label) return box;

  return (
    <label
      className={cn(
        "inline-flex items-center gap-1.5 text-xs text-ink select-none",
        disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer",
        className,
      )}
    >
      {box}
      <span>{label}</span>
    </label>
  );
}
