import * as ContextMenuPrimitive from "@radix-ui/react-context-menu";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";
import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { Icon } from "./Icon";

export interface MenuActionItem {
  type?: "item" | "separator" | "label";
  id?: string;
  label?: string;
  icon?: string;
  shortcut?: string;
  danger?: boolean;
  disabled?: boolean;
  "data-testid"?: string;
  onSelect?: () => void;
}

const CONTENT_CLASSES =
  "z-[60] min-w-[168px] overflow-hidden rounded-[12px] border border-hairline bg-card p-1 text-ink float-shadow animate-tulai-scale-in select-none";

const ITEM_CLASSES =
  "relative flex cursor-pointer items-center gap-2 rounded-[8px] px-2.5 py-1.5 text-xs outline-none transition-colors duration-100 data-[disabled]:pointer-events-none data-[disabled]:opacity-40";

export const DropdownMenuRoot = DropdownMenuPrimitive.Root;
export const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;
export const DropdownMenuPortal = DropdownMenuPrimitive.Portal;
export const DropdownMenuGroup = DropdownMenuPrimitive.Group;

export function DropdownMenuContent({
  className,
  sideOffset = 4,
  ...props
}: DropdownMenuPrimitive.DropdownMenuContentProps) {
  return (
    <DropdownMenuPrimitive.Portal>
      <DropdownMenuPrimitive.Content
        sideOffset={sideOffset}
        className={cn(CONTENT_CLASSES, className)}
        {...props}
      />
    </DropdownMenuPrimitive.Portal>
  );
}

export function DropdownMenuItem({
  className,
  danger,
  ...props
}: DropdownMenuPrimitive.DropdownMenuItemProps & { danger?: boolean }) {
  return (
    <DropdownMenuPrimitive.Item
      className={cn(
        ITEM_CLASSES,
        danger
          ? "text-status-danger data-[highlighted]:bg-[var(--status-danger-bg)]"
          : "text-ink data-[highlighted]:bg-accent-soft data-[highlighted]:text-accent",
        className,
      )}
      {...props}
    />
  );
}

export function DropdownMenuSeparator({
  className,
  ...props
}: DropdownMenuPrimitive.DropdownMenuSeparatorProps) {
  return (
    <DropdownMenuPrimitive.Separator
      className={cn("my-1 h-px bg-divider", className)}
      {...props}
    />
  );
}

export function DropdownMenuLabel({
  className,
  ...props
}: DropdownMenuPrimitive.DropdownMenuLabelProps) {
  return (
    <DropdownMenuPrimitive.Label
      className={cn("px-2.5 py-1 text-[10.5px] font-semibold text-subtle", className)}
      {...props}
    />
  );
}

export interface DropdownMenuProps {
  trigger: ReactNode;
  items: MenuActionItem[];
  align?: "start" | "center" | "end";
  side?: "top" | "right" | "bottom" | "left";
  contentClassName?: string;
}

export function DropdownMenu({
  trigger,
  items,
  align = "end",
  side = "bottom",
  contentClassName,
}: DropdownMenuProps) {
  return (
    <DropdownMenuPrimitive.Root>
      <DropdownMenuPrimitive.Trigger asChild>{trigger}</DropdownMenuPrimitive.Trigger>
      <DropdownMenuContent align={align} side={side} className={contentClassName}>
        {items.map((item, idx) => {
          const key = item.id ?? `${item.type ?? "item"}-${idx}`;
          if (item.type === "separator") {
            return <DropdownMenuSeparator key={key} />;
          }
          if (item.type === "label") {
            return <DropdownMenuLabel key={key}>{item.label}</DropdownMenuLabel>;
          }
          return (
            <DropdownMenuItem
              key={key}
              danger={item.danger}
              disabled={item.disabled}
              data-testid={item["data-testid"]}
              onSelect={() => item.onSelect?.()}
            >
              {item.icon ? <Icon name={item.icon} size={13} /> : null}
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              {item.shortcut ? (
                <span className="ml-2 shrink-0 text-[10.5px] text-subtle">{item.shortcut}</span>
              ) : null}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenuPrimitive.Root>
  );
}

export const ContextMenuRoot = ContextMenuPrimitive.Root;
export const ContextMenuTrigger = ContextMenuPrimitive.Trigger;

export function ContextMenuContent({
  className,
  ...props
}: ContextMenuPrimitive.ContextMenuContentProps) {
  return (
    <ContextMenuPrimitive.Portal>
      <ContextMenuPrimitive.Content
        className={cn(CONTENT_CLASSES, className)}
        {...props}
      />
    </ContextMenuPrimitive.Portal>
  );
}

export function ContextMenuItem({
  className,
  danger,
  ...props
}: ContextMenuPrimitive.ContextMenuItemProps & { danger?: boolean }) {
  return (
    <ContextMenuPrimitive.Item
      className={cn(
        ITEM_CLASSES,
        danger
          ? "text-status-danger data-[highlighted]:bg-[var(--status-danger-bg)]"
          : "text-ink data-[highlighted]:bg-accent-soft data-[highlighted]:text-accent",
        className,
      )}
      {...props}
    />
  );
}

export function ContextMenuSeparator({
  className,
  ...props
}: ContextMenuPrimitive.ContextMenuSeparatorProps) {
  return (
    <ContextMenuPrimitive.Separator
      className={cn("my-1 h-px bg-divider", className)}
      {...props}
    />
  );
}

export interface ContextMenuProps {
  children: ReactNode;
  items: MenuActionItem[];
  disabled?: boolean;
  contentClassName?: string;
}

export function ContextMenu({
  children,
  items,
  disabled = false,
  contentClassName,
}: ContextMenuProps) {
  if (disabled || items.length === 0) return <>{children}</>;
  return (
    <ContextMenuPrimitive.Root>
      <ContextMenuPrimitive.Trigger asChild>{children}</ContextMenuPrimitive.Trigger>
      <ContextMenuContent className={contentClassName}>
        {items.map((item, idx) => {
          const key = item.id ?? `${item.type ?? "item"}-${idx}`;
          if (item.type === "separator") {
            return <ContextMenuSeparator key={key} />;
          }
          if (item.type === "label") {
            return (
              <ContextMenuPrimitive.Label
                key={key}
                className="px-2.5 py-1 text-[10.5px] font-semibold text-subtle"
              >
                {item.label}
              </ContextMenuPrimitive.Label>
            );
          }
          return (
            <ContextMenuItem
              key={key}
              danger={item.danger}
              disabled={item.disabled}
              data-testid={item["data-testid"]}
              onSelect={() => item.onSelect?.()}
            >
              {item.icon ? <Icon name={item.icon} size={13} /> : null}
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              {item.shortcut ? (
                <span className="ml-2 text-[10.5px] text-subtle">{item.shortcut}</span>
              ) : null}
            </ContextMenuItem>
          );
        })}
      </ContextMenuContent>
    </ContextMenuPrimitive.Root>
  );
}
