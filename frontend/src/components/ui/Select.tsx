import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import { cn } from "@/lib/cn";

const EMPTY_SENTINEL = "__TULAI_SELECT_EMPTY__";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface SelectProps {
  value: string;
  onValueChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  size?: "small" | "default";
  className?: string;
  contentClassName?: string;
  "data-testid"?: string;
  "aria-label"?: string;
}

function encodeVal(v: string): string {
  return v === "" ? EMPTY_SENTINEL : v;
}

function decodeVal(v: string): string {
  return v === EMPTY_SENTINEL ? "" : v;
}

export function Select({
  value,
  onValueChange,
  options,
  placeholder,
  disabled = false,
  size = "default",
  className,
  contentClassName,
  "data-testid": testId,
  "aria-label": ariaLabel,
}: SelectProps) {
  const hasEmptyOption = options.some((o) => o.value === "");
  const encodedValue =
    value === ""
      ? hasEmptyOption
        ? EMPTY_SENTINEL
        : undefined
      : encodeVal(value);

  return (
    <SelectPrimitive.Root
      value={encodedValue}
      onValueChange={(v) => onValueChange(decodeVal(v))}
      disabled={disabled}
    >
      <SelectPrimitive.Trigger
        data-testid={testId}
        aria-label={ariaLabel}
        className={cn(
          "inline-flex w-full min-w-0 items-center justify-between gap-1.5 rounded-[10px] border border-strong bg-field text-ink",
          "transition-[border-color,box-shadow,background-color] duration-150 cursor-pointer select-none",
          "hover:border-[var(--text-subtle)] focus:border-accent focus:outline-none focus:ring-2 focus:ring-[var(--focus-ring)]",
          "disabled:cursor-not-allowed disabled:opacity-50",
          "data-[placeholder]:text-subtle",
          size === "small" ? "h-7 px-2.5 text-xs" : "h-8 px-2.5 text-xs",
          className,
        )}
      >
        <SelectPrimitive.Value placeholder={placeholder} className="truncate" />
        <SelectPrimitive.Icon asChild>
          <ChevronDown size={13} className="shrink-0 text-subtle" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>

      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={4}
          className={cn(
            "z-[60] max-h-[280px] min-w-[var(--radix-select-trigger-width)] overflow-hidden rounded-[12px] border border-hairline bg-card p-1 text-ink float-shadow",
            "animate-tulai-scale-in",
            contentClassName,
          )}
        >
          <SelectPrimitive.Viewport className="p-0.5">
            {options.map((opt) => (
              <SelectPrimitive.Item
                key={opt.value === "" ? EMPTY_SENTINEL : opt.value}
                value={encodeVal(opt.value)}
                disabled={opt.disabled}
                className={cn(
                  "relative flex w-full cursor-pointer items-center justify-between gap-2 rounded-[8px] py-1.5 pl-2.5 pr-7 text-xs text-ink outline-none select-none",
                  "data-[highlighted]:bg-accent-soft data-[highlighted]:text-accent",
                  "data-[state=checked]:font-semibold",
                  "data-[disabled]:pointer-events-none data-[disabled]:opacity-40",
                )}
              >
                <SelectPrimitive.ItemText className="truncate">{opt.label}</SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator className="absolute right-2 flex items-center text-accent">
                  <Check size={13} />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}
