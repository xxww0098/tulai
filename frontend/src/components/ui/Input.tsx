import {
  forwardRef,
  useState,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from "react";
import { Eye, EyeOff } from "lucide-react";
import { cn } from "@/lib/cn";
import { Icon } from "./Icon";

export interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "size"> {
  size?: "small" | "default";
  masked?: boolean;
  leftIcon?: string | ReactNode;
  rightSlot?: ReactNode;
  wrapperClassName?: string;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  {
    size = "default",
    masked = false,
    leftIcon,
    rightSlot,
    type,
    className,
    wrapperClassName,
    disabled,
    ...rest
  },
  ref,
) {
  const [revealed, setRevealed] = useState(false);
  const inputType = masked ? (revealed ? "text" : "password") : (type ?? "text");
  const hasChrome = Boolean(leftIcon || rightSlot || masked);

  const inputClasses = cn(
    "w-full min-w-0 bg-field text-ink placeholder:text-subtle",
    "border border-strong rounded-[10px] transition-[border-color,box-shadow,background-color] duration-150",
    "hover:border-[var(--text-subtle)] focus:border-accent focus:outline-none focus:ring-2 focus:ring-[var(--focus-ring)]",
    "disabled:cursor-not-allowed disabled:opacity-50",
    size === "small" ? "h-7 px-2.5 text-xs" : "h-8 px-2.5 text-xs",
    leftIcon ? "pl-7" : "",
    masked || rightSlot ? "pr-7" : "",
    className,
  );

  if (!hasChrome) {
    return (
      <input
        ref={ref}
        type={inputType}
        disabled={disabled}
        className={inputClasses}
        {...rest}
      />
    );
  }

  return (
    <div className={cn("relative flex min-w-0 items-center", wrapperClassName)}>
      {leftIcon ? (
        <span className="pointer-events-none absolute left-2.5 flex items-center text-subtle">
          {typeof leftIcon === "string" ? <Icon name={leftIcon} size={13} /> : leftIcon}
        </span>
      ) : null}
      <input
        ref={ref}
        type={inputType}
        disabled={disabled}
        className={inputClasses}
        {...rest}
      />
      {masked ? (
        <button
          type="button"
          tabIndex={-1}
          disabled={disabled}
          aria-label={revealed ? "隐藏内容" : "显示内容"}
          onClick={() => setRevealed((v) => !v)}
          className="absolute right-1.5 flex size-5 items-center justify-center rounded-[6px] text-subtle hover:bg-sunken hover:text-ink"
        >
          {revealed ? <EyeOff size={13} /> : <Eye size={13} />}
        </button>
      ) : rightSlot ? (
        <span className="absolute right-1.5 flex items-center">{rightSlot}</span>
      ) : null}
    </div>
  );
});

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  mono?: boolean;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { mono = false, className, ...rest },
  ref,
) {
  return (
    <textarea
      ref={ref}
      className={cn(
        "w-full min-w-0 rounded-[10px] border border-strong bg-field px-2.5 py-2 text-xs text-ink placeholder:text-subtle",
        "transition-[border-color,box-shadow] duration-150 resize-none",
        "hover:border-[var(--text-subtle)] focus:border-accent focus:outline-none focus:ring-2 focus:ring-[var(--focus-ring)]",
        "disabled:cursor-not-allowed disabled:opacity-50",
        mono && "font-mono-console",
        className,
      )}
      {...rest}
    />
  );
});
