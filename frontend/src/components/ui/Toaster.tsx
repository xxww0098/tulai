import { useSyncExternalStore } from "react";
import { AlertCircle, AlertTriangle, CheckCircle2, Info, X } from "lucide-react";
import { cn } from "@/lib/cn";

export type ToastLevel = "success" | "error" | "warning" | "info";

export interface ToastItem {
  id: number;
  title: string;
  description?: string;
  level: ToastLevel;
}

let toastSeq = 0;
let toastList: ToastItem[] = [];
const listeners = new Set<() => void>();
const timers = new Map<number, ReturnType<typeof setTimeout>>();

function emit() {
  for (const l of listeners) l();
}

export function dismissToast(id: number): void {
  const t = timers.get(id);
  if (t) {
    clearTimeout(t);
    timers.delete(id);
  }
  const next = toastList.filter((item) => item.id !== id);
  if (next.length !== toastList.length) {
    toastList = next;
    emit();
  }
}

export function pushToast(
  title: string,
  level: ToastLevel = "info",
  description?: string,
  durationMs?: number,
): number {
  const id = ++toastSeq;
  const item: ToastItem = { id, title, description, level };
  toastList = [...toastList.slice(-4), item];
  emit();

  const ms = durationMs ?? (level === "error" ? 5200 : 3400);
  if (ms > 0) {
    const timer = setTimeout(() => dismissToast(id), ms);
    timers.set(id, timer);
  }
  return id;
}

export function clearAllToasts(): void {
  for (const timer of timers.values()) clearTimeout(timer);
  timers.clear();
  if (toastList.length > 0) {
    toastList = [];
    emit();
  }
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot(): ToastItem[] {
  return toastList;
}

const LEVEL_STYLES: Record<ToastLevel, { border: string; iconColor: string; Icon: typeof Info }> = {
  success: {
    border: "border-[var(--status-ok-border)]",
    iconColor: "text-status-ok",
    Icon: CheckCircle2,
  },
  error: {
    border: "border-[var(--status-danger-border)]",
    iconColor: "text-status-danger",
    Icon: AlertCircle,
  },
  warning: {
    border: "border-[var(--status-warn-border)]",
    iconColor: "text-status-warn",
    Icon: AlertTriangle,
  },
  info: {
    border: "border-accent-border",
    iconColor: "text-accent",
    Icon: Info,
  },
};

export function Toaster() {
  const items = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  if (items.length === 0) return null;

  return (
    <div
      data-testid="toast-viewport"
      aria-live="polite"
      className="pointer-events-none fixed right-3.5 bottom-3.5 z-[70] flex max-w-[360px] flex-col gap-2"
    >
      {items.map((item) => {
        const meta = LEVEL_STYLES[item.level];
        const LevelIcon = meta.Icon;
        return (
          <div
            key={item.id}
            role="status"
            className={cn(
              "pointer-events-auto flex items-start gap-2.5 rounded-[12px] border bg-card px-3 py-2.5 text-ink float-shadow animate-tulai-toast",
              meta.border,
            )}
          >
            <LevelIcon size={15} className={cn("mt-0.5 shrink-0", meta.iconColor)} />
            <div className="min-w-0 flex-1">
              <div className="text-xs font-semibold leading-snug text-ink">{item.title}</div>
              {item.description ? (
                <div className="mt-0.5 text-[11px] leading-snug text-muted">{item.description}</div>
              ) : null}
            </div>
            <button
              type="button"
              aria-label="关闭通知"
              onClick={() => dismissToast(item.id)}
              className="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-[6px] text-subtle hover:bg-sunken hover:text-ink"
            >
              <X size={12} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
