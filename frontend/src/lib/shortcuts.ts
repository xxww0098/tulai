import { useEffect } from "react";

export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (target.isContentEditable) return true;
  if (target.closest("[contenteditable='true']")) return true;
  return false;
}

export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return false;
  return /mac|darwin/i.test(navigator.platform || navigator.userAgent);
}

export function modKeyLabel(): string {
  return isMacPlatform() ? "⌘" : "Ctrl+";
}

interface EscapeEntry {
  id: string;
  priority: number;
  order: number;
  handler: () => boolean | void;
}

const escapeStack: EscapeEntry[] = [];
let escapeSeq = 0;
let globalShortcutsCount = 0;

export function isGlobalShortcutsInstalled(): boolean {
  return globalShortcutsCount > 0;
}

/**
 * Push a layer onto the priority Escape stack.
 * Higher priority closes first:
 *   50 = Lightbox
 *   40 = Modal Dialog
 *   30 = BrowserDrawer
 *   20 = LogDrawer
 *   10 = Page-level mode (e.g., Workbench multi-select)
 * Return `false` from handler to fall through to the next layer.
 */
export function pushEscapeHandler(
  id: string,
  handler: () => boolean | void,
  priority = 20,
): () => void {
  const entry: EscapeEntry = { id, priority, order: ++escapeSeq, handler };
  escapeStack.push(entry);
  return () => {
    const idx = escapeStack.indexOf(entry);
    if (idx >= 0) escapeStack.splice(idx, 1);
  };
}

export function useEscapeHandler(
  id: string,
  active: boolean,
  handler: () => boolean | void,
  priority = 20,
): void {
  useEffect(() => {
    if (!active) return;
    return pushEscapeHandler(id, handler, priority);
  }, [id, active, handler, priority]);
}

function hasOpenRadixPopover(): boolean {
  return Boolean(
    document.querySelector(
      "[data-radix-popper-content-wrapper], [role='menu'][data-state='open'], [role='listbox'][data-state='open']",
    ),
  );
}

export function dispatchTopEscape(): boolean {
  if (escapeStack.length === 0) return false;
  const sorted = [...escapeStack].sort(
    (a, b) => b.priority - a.priority || b.order - a.order,
  );
  for (const entry of sorted) {
    const res = entry.handler();
    if (res !== false) return true;
  }
  return false;
}

export interface GlobalShortcutHandlers {
  onSelectNavIndex: (index1Based: number) => void;
  onOpenSettings: () => void;
  onToggleSidebar: () => void;
  onToggleLogs: () => void;
}

export function installGlobalShortcuts(handlers: GlobalShortcutHandlers): () => void {
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.isComposing) return;

    if (e.key === "Escape") {
      if (e.defaultPrevented || hasOpenRadixPopover()) return;
      if (dispatchTopEscape()) {
        e.preventDefault();
      }
      return;
    }

    const mod = e.metaKey || e.ctrlKey;
    if (!mod || e.altKey) return;

    const key = e.key.toLowerCase();

    if (key === ",") {
      e.preventDefault();
      handlers.onOpenSettings();
      return;
    }

    if (key === "\\") {
      e.preventDefault();
      handlers.onToggleSidebar();
      return;
    }

    if (key === "j" && !e.shiftKey) {
      if (isEditableTarget(e.target)) return;
      e.preventDefault();
      handlers.onToggleLogs();
      return;
    }

    if (!e.shiftKey && /^[1-8]$/.test(key)) {
      if (isEditableTarget(e.target)) return;
      e.preventDefault();
      handlers.onSelectNavIndex(Number(key));
    }
  };

  globalShortcutsCount++;
  window.addEventListener("keydown", onKeyDown);
  return () => {
    globalShortcutsCount = Math.max(0, globalShortcutsCount - 1);
    window.removeEventListener("keydown", onKeyDown);
  };
}
