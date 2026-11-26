import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import { Loader2 } from "lucide-react";
import { cn } from "@/lib/cn";
import { Icon } from "./Icon";

export type ButtonVariant = "primary" | "secondary" | "outline" | "ghost" | "danger";
export type ButtonSize = "xs" | "small" | "default";

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary:
    "border-transparent bg-accent text-on-accent hover:bg-accent-hover active:bg-accent-pressed shadow-[0_1px_2px_rgba(37,99,235,0.18)] dark:shadow-none",
  secondary:
    "border-hairline bg-sunken text-ink hover:bg-sunken-hover active:bg-[var(--bg-surface-pressed)]",
  outline:
    "border-strong bg-card text-ink hover:bg-sunken active:bg-sunken-hover",
  ghost:
    "border-transparent bg-transparent text-muted hover:bg-sunken hover:text-ink active:bg-sunken-hover",
  danger:
    "border-transparent bg-[#DC2626] text-white hover:bg-[#B91C1C] active:bg-[#991B1B] dark:bg-[#EF4444] dark:text-[#090D16] dark:hover:bg-[#F87171]",
};

const SIZE_CLASSES: Record<ButtonSize, string> = {
  xs: "h-6 rounded-[8px] px-2 text-[11px] gap-1",
  small: "h-7 rounded-[9px] px-2.5 text-xs gap-1.5",
  default: "h-8 rounded-[10px] px-3 text-xs gap-1.5",
};

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  icon?: string | ReactNode;
  iconSize?: number;
  busy?: boolean;
  text?: string;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  {
    variant = "secondary",
    size = "default",
    icon,
    iconSize,
    busy = false,
    text,
    disabled,
    className,
    type = "button",
    children,
    ...rest
  },
  ref,
) {
  const isDisabled = Boolean(disabled || busy);
  const resolvedIconSize = iconSize ?? (size === "xs" ? 12 : 13.5);

  return (
    <button
      ref={ref}
      type={type}
      disabled={isDisabled}
      className={cn(
        "inline-flex shrink-0 items-center justify-center border font-medium whitespace-nowrap transition-[background-color,border-color,color,transform,opacity] duration-150 ease-out",
        "select-none cursor-pointer active:scale-[0.98]",
        "disabled:pointer-events-none disabled:opacity-45",
        VARIANT_CLASSES[variant],
        SIZE_CLASSES[size],
        className,
      )}
      {...rest}
    >
      {busy ? (
        <Loader2 size={resolvedIconSize} className="shrink-0 animate-spin" aria-hidden="true" />
      ) : typeof icon === "string" ? (
        <Icon name={icon} size={resolvedIconSize} />
      ) : (
        icon
      )}
      {text !== undefined ? <span className="truncate">{text}</span> : null}
      {children}
    </button>
  );
});

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon: string | ReactNode;
  variant?: ButtonVariant;
  size?: ButtonSize | number;
  iconSize?: number;
  busy?: boolean;
}

const ICON_BTN_SIZES: Record<ButtonSize, string> = {
  xs: "size-6 rounded-[7px]",
  small: "size-7 rounded-[8px]",
  default: "size-8 rounded-[9px]",
};

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  {
    icon,
    variant = "ghost",
    size = "small",
    iconSize,
    busy = false,
    disabled,
    className,
    style,
    type = "button",
    ...rest
  },
  ref,
) {
  const isDisabled = Boolean(disabled || busy);
  const numericSize = typeof size === "number" ? size : undefined;
  const resolvedIconSize =
    iconSize ?? (numericSize ? Math.max(12, Math.round(numericSize * 0.54)) : size === "xs" ? 13 : 14);

  return (
    <button
      ref={ref}
      type={type}
      disabled={isDisabled}
      style={
        numericSize
          ? { width: numericSize, height: numericSize, ...style }
          : style
      }
      className={cn(
        "inline-flex shrink-0 items-center justify-center border transition-[background-color,border-color,color,transform,opacity] duration-150 ease-out",
        "select-none cursor-pointer active:scale-[0.96]",
        "disabled:pointer-events-none disabled:opacity-45",
        VARIANT_CLASSES[variant],
        numericSize ? "rounded-[8px]" : ICON_BTN_SIZES[size as ButtonSize],
        className,
      )}
      {...rest}
    >
      {busy ? (
        <Loader2 size={resolvedIconSize} className="shrink-0 animate-spin" aria-hidden="true" />
      ) : typeof icon === "string" ? (
        <Icon name={icon} size={resolvedIconSize} />
      ) : (
        icon
      )}
    </button>
  );
});
