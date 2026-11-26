import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

export const DialogRoot = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogPortal = DialogPrimitive.Portal;
export const DialogClose = DialogPrimitive.Close;

export interface DialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title?: ReactNode;
  description?: ReactNode;
  headerRight?: ReactNode;
  footer?: ReactNode;
  width?: number | string;
  maxWidth?: number | string;
  hideCloseButton?: boolean;
  closeOnEscape?: boolean;
  closeOnBackdrop?: boolean;
  className?: string;
  bodyClassName?: string;
  "data-testid"?: string;
  children?: ReactNode;
}

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  headerRight,
  footer,
  width,
  maxWidth,
  hideCloseButton = false,
  closeOnEscape = true,
  closeOnBackdrop = true,
  className,
  bodyClassName,
  "data-testid": testId,
  children,
}: DialogProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-slate-950/45 backdrop-blur-[1px] dark:bg-black/65 animate-tulai-fade" />
        <DialogPrimitive.Content
          data-testid={testId}
          onEscapeKeyDown={(e) => {
            if (!closeOnEscape) e.preventDefault();
          }}
          onPointerDownOutside={(e) => {
            if (!closeOnBackdrop) e.preventDefault();
          }}
          style={{
            width: typeof width === "number" ? `${width}px` : width,
            maxWidth:
              maxWidth !== undefined
                ? typeof maxWidth === "number"
                  ? `${maxWidth}px`
                  : maxWidth
                : "calc(100vw - 24px)",
          }}
          className={cn(
            "fixed left-1/2 top-1/2 z-50 flex max-h-[calc(100vh-24px)] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden",
            "rounded-[16px] border border-hairline bg-card text-ink float-shadow animate-tulai-dialog",
            "focus:outline-none",
            !width && "w-[min(560px,calc(100vw-24px))]",
            className,
          )}
        >
          {title !== undefined || description !== undefined || !hideCloseButton ? (
            <div className="flex shrink-0 items-start justify-between gap-3 border-b border-divider px-4 py-3">
              <div className="min-w-0 flex-1">
                {title !== undefined ? (
                  <DialogPrimitive.Title className="truncate text-[14px] font-semibold text-ink">
                    {title}
                  </DialogPrimitive.Title>
                ) : (
                  <DialogPrimitive.Title className="sr-only">对话框</DialogPrimitive.Title>
                )}
                {description !== undefined ? (
                  <DialogPrimitive.Description className="mt-0.5 text-[11.5px] text-muted">
                    {description}
                  </DialogPrimitive.Description>
                ) : (
                  <DialogPrimitive.Description className="sr-only">
                    对话框内容
                  </DialogPrimitive.Description>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                {headerRight}
                {!hideCloseButton ? (
                  <DialogPrimitive.Close
                    aria-label="关闭"
                    className="flex size-7 cursor-pointer items-center justify-center rounded-[8px] text-muted transition-colors hover:bg-sunken hover:text-ink"
                  >
                    <X size={14} />
                  </DialogPrimitive.Close>
                ) : null}
              </div>
            </div>
          ) : null}

          <div className={cn("min-h-0 flex-1 overflow-y-auto p-4", bodyClassName)}>
            {children}
          </div>

          {footer !== undefined ? (
            <div className="flex shrink-0 items-center justify-end gap-2 border-t border-divider bg-card-header px-4 py-2.5">
              {footer}
            </div>
          ) : null}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
