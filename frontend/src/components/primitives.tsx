import type { ReactNode } from "react";
import iconPng from "@/assets/icon.png";
import { cn } from "@/lib/cn";
import { AccountListFrame, AccountViewButton } from "./BrowserDrawer";
import { ShellActions } from "./ShellActions";
import { Badge, OutlineBadge, type BadgeVariant } from "./ui/Badge";
import { Icon } from "./ui/Icon";

export { AccountListFrame, AccountViewButton, OutlineBadge };

export function BrandLogo({
  size = 18,
  className,
  "data-testid": testId,
}: {
  size?: number;
  className?: string;
  "data-testid"?: string;
}) {
  return (
    <img
      src={iconPng}
      alt="图来"
      data-testid={testId}
      width={size}
      height={size}
      draggable={false}
      style={{
        width: size,
        height: size,
        borderRadius: Math.max(4, Math.round(size * 0.22)),
      }}
      className={cn("shrink-0 object-contain select-none", className)}
    />
  );
}

export function PageToolbar({
  left,
  right,
  bottom,
  className,
}: {
  left?: ReactNode;
  right?: ReactNode;
  bottom?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("app-drag-region flex shrink-0 flex-col gap-2.5 px-3 pt-2.5 pb-2.5", className)}>
      <div
        className="app-drag-region bento-shadow flex min-h-[42px] flex-wrap items-center justify-between gap-2 rounded-[16px] border border-hairline bg-card py-1.5 pl-3.5"
        style={{ paddingRight: "calc(14px + var(--mygo-titlebar-inset-right, 0px))" }}
      >
        <div className="app-no-drag flex min-w-0 flex-initial flex-wrap items-center gap-2">
          {left}
        </div>
        <div className="app-no-drag flex shrink-0 items-center gap-1.5">
          {right}
          <ShellActions />
        </div>
      </div>
      {bottom ? <div className="app-no-drag">{bottom}</div> : null}
    </div>
  );
}

function ModuleHeader({
  title,
  subtitle,
  icon,
  iconColor,
  right,
  className,
}: {
  title: string;
  subtitle?: string;
  icon?: string;
  iconColor?: string;
  right?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex items-center justify-between gap-2", className)}>
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {icon ? (
          <Icon
            name={icon}
            size={14}
            color={iconColor}
            className={!iconColor ? "text-subtle" : undefined}
          />
        ) : null}
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="tnum truncate text-[13px] font-semibold text-ink">{title}</div>
          {subtitle ? (
            <div className="tnum truncate text-[11px] text-muted">{subtitle}</div>
          ) : null}
        </div>
      </div>
      {right ? <div className="flex shrink-0 items-center gap-1.5">{right}</div> : null}
    </div>
  );
}

export function BentoCard({
  "data-testid": testId,
  title,
  subtitle,
  icon,
  iconColor,
  right,
  compact = false,
  flex = false,
  className,
  children,
}: {
  "data-testid"?: string;
  title?: string;
  subtitle?: string;
  icon?: string;
  iconColor?: string;
  right?: ReactNode;
  compact?: boolean;
  flex?: boolean;
  className?: string;
  children?: ReactNode;
}) {
  const hasBody = children !== undefined && children !== null && children !== false;
  const hasHeader = Boolean(title || subtitle || icon || right);

  return (
    <section
      data-testid={testId}
      className={cn(
        "bento-shadow flex flex-col rounded-[16px] border border-hairline bg-card",
        flex && "min-h-0 min-w-0 flex-1",
        compact ? "p-3" : "p-4",
        className,
      )}
    >
      {hasHeader ? (
        <ModuleHeader
          title={title ?? ""}
          subtitle={subtitle}
          icon={icon}
          iconColor={iconColor}
          right={right}
          className={hasBody ? (compact ? "mb-2.5" : "mb-3") : undefined}
        />
      ) : null}
      {children}
    </section>
  );
}

export function FormRow({
  label,
  hint,
  className,
  children,
}: {
  label: string;
  hint?: string;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <div className={cn("flex flex-col gap-1.5 md:flex-row md:items-center md:gap-4", className)}>
      <div className="flex shrink-0 flex-col gap-0.5 md:w-[152px]">
        <div className="text-xs font-medium text-ink">{label}</div>
        {hint ? <div className="text-[11px] text-muted">{hint}</div> : null}
      </div>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

export function FlushPane({
  "data-testid": testId,
  className,
  children,
}: {
  "data-testid"?: string;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <div
      data-testid={testId}
      className={cn(
        "bento-shadow flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-[16px] border border-hairline bg-card",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function PaneHeader({
  title,
  subtitle,
  icon,
  right,
  dragRegion = false,
  className,
}: {
  title: string;
  subtitle?: string;
  icon?: string;
  right?: ReactNode;
  dragRegion?: boolean;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex shrink-0 items-center justify-between gap-2 border-b border-divider bg-card px-4 py-3",
        dragRegion && "app-drag-region",
        className,
      )}
    >
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {icon ? <Icon name={icon} size={14} className="text-subtle" /> : null}
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="tnum truncate text-[13px] font-semibold text-ink">{title}</div>
          {subtitle ? (
            <div className="tnum truncate text-[11px] text-muted">{subtitle}</div>
          ) : null}
        </div>
      </div>
      {right ? (
        <div className={cn("flex shrink-0 items-center gap-1.5", dragRegion && "app-no-drag")}>
          {right}
        </div>
      ) : null}
    </div>
  );
}

export function InsetPillRow({
  "data-testid": testId,
  active = false,
  onClick,
  className,
  children,
}: {
  "data-testid"?: string;
  active?: boolean;
  onClick?: () => void;
  className?: string;
  children?: ReactNode;
}) {
  const interactive = Boolean(onClick);
  return (
    <div
      data-testid={testId}
      role={interactive ? "button" : undefined}
      tabIndex={interactive ? 0 : undefined}
      onClick={onClick}
      onKeyDown={
        interactive
          ? (e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                onClick?.();
              }
            }
          : undefined
      }
      className={cn(
        "flex flex-col rounded-[12px] border px-3 py-2.5 transition-colors duration-150",
        active
          ? "border-accent-border bg-accent-soft"
          : "border-transparent bg-sunken",
        interactive && "cursor-pointer",
        interactive && !active && "hover:bg-sunken-hover",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function SegmentedPill({
  "data-testid": testId,
  label,
  icon,
  active,
  disabled = false,
  onClick,
  className,
}: {
  "data-testid"?: string;
  label: string;
  icon?: string;
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  className?: string;
}) {
  return (
    <button
      type="button"
      data-testid={testId}
      disabled={disabled}
      aria-pressed={active}
      onClick={() => {
        if (!disabled) onClick();
      }}
      className={cn(
        "tnum inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs transition-colors duration-150 select-none",
        disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer",
        active
          ? "bg-accent font-semibold text-on-accent hover:bg-accent-hover"
          : "bg-transparent font-medium text-muted hover:bg-sunken hover:text-ink",
        className,
      )}
    >
      {icon ? <Icon name={icon} size={13} /> : null}
      <span>{label}</span>
    </button>
  );
}

export function PillToggle({
  label,
  active,
  onClick,
  disabled = false,
  "data-testid": testId,
  className,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
  disabled?: boolean;
  "data-testid"?: string;
  className?: string;
}) {
  return (
    <button
      type="button"
      data-testid={testId}
      disabled={disabled}
      aria-pressed={active}
      onClick={() => {
        if (!disabled) onClick();
      }}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-[12px] border px-3 py-1.5 text-xs transition-colors duration-150 select-none",
        disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer",
        active
          ? "border-accent bg-accent font-semibold text-on-accent hover:bg-accent-hover"
          : "border-hairline bg-sunken font-medium text-muted hover:bg-sunken-hover hover:text-ink",
        className,
      )}
    >
      <span>{label}</span>
    </button>
  );
}

export function EmptyNotice({
  icon,
  title,
  subtitle,
  action,
  className,
}: {
  icon: string;
  title: string;
  subtitle?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-2.5 rounded-[16px] border border-dashed border-[#D6DCE6] bg-[#F7F9FC] px-4 py-7 text-center dark:border-[#263147] dark:bg-[#151C29]",
        className,
      )}
    >
      <div className="animate-tulai-scale-in flex size-10 items-center justify-center rounded-full bg-accent-soft text-accent">
        <Icon name={icon} size={18} />
      </div>
      <div className="text-xs font-semibold text-ink">{title}</div>
      {subtitle ? (
        <div className="max-w-[320px] text-[11px] leading-relaxed text-muted">{subtitle}</div>
      ) : null}
      {action ? <div className="mt-0.5">{action}</div> : null}
    </div>
  );
}

export function toneVariant(tone?: string): BadgeVariant {
  switch (tone) {
    case "ok":
    case "ready":
    case "success":
    case "up":
      return "success";
    case "warn":
    case "warning":
    case "cooling":
    case "verify":
      return "warn";
    case "danger":
    case "error":
    case "bad":
    case "failed":
      return "danger";
    case "info":
    case "running":
    case "busy":
    case "accent":
      return "accent";
    default:
      return "neutral";
  }
}

export function StatusBadge({
  text,
  tone,
  dot = false,
  pulseDot = false,
  className,
}: {
  text?: string;
  tone?: string;
  dot?: boolean;
  pulseDot?: boolean;
  className?: string;
}) {
  return (
    <Badge
      text={text || "-"}
      variant={toneVariant(tone)}
      dot={dot}
      pulseDot={pulseDot}
      className={className}
    />
  );
}

export function PageSkeleton({
  rows = 5,
  className,
}: {
  rows?: number;
  className?: string;
}) {
  return (
    <div className={cn("flex min-h-0 flex-1 flex-col gap-3 p-3", className)}>
      <div className="h-[42px] w-full animate-pulse rounded-[16px] border border-hairline bg-card" />
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 md:grid-cols-3">
        <div className="flex flex-col gap-2.5 rounded-[16px] border border-hairline bg-card p-4">
          <div className="h-4 w-28 animate-pulse rounded bg-sunken" />
          <div className="h-24 w-full animate-pulse rounded-[12px] bg-sunken" />
          <div className="h-16 w-full animate-pulse rounded-[12px] bg-sunken" />
        </div>
        <div className="flex flex-col gap-2.5 rounded-[16px] border border-hairline bg-card p-4 md:col-span-2">
          <div className="h-4 w-36 animate-pulse rounded bg-sunken" />
          {Array.from({ length: rows }).map((_, idx) => (
            <div
              key={idx}
              className="h-10 w-full animate-pulse rounded-[12px] bg-sunken"
            />
          ))}
        </div>
      </div>
    </div>
  );
}
