import { useEffect, useRef, useState, type RefObject } from "react";
import { isCallError, isMyGo, onFileDrop as mygoOnFileDrop } from "mygo-runtime";
import {
  Browser,
  Core,
  Dedup,
  Download,
  ImageEdit,
  Prompts,
  Rename,
  Settings,
  Workbench,
  events,
  type FileDropEvent,
} from "@/mygo";

export {
  Browser,
  Core,
  Dedup,
  Download,
  ImageEdit,
  Prompts,
  Rename,
  Settings,
  Workbench,
  events,
};

export const backend = {
  core: Core,
  workbench: Workbench,
  browser: Browser,
  prompts: Prompts,
  download: Download,
  dedup: Dedup,
  rename: Rename,
  imageEdit: ImageEdit,
  settings: Settings,
} as const;

export function isMyGoRuntime(): boolean {
  try {
    return isMyGo();
  } catch {
    return false;
  }
}

export function formatCallError(err: unknown): string {
  if (isCallError(err)) return err.message;
  if (err instanceof Error) return err.message;
  return String(err ?? "未知错误");
}

export async function safeIpcCall<T>(fn: () => Promise<T>, fallback: T): Promise<T> {
  if (!isMyGoRuntime()) return fallback;
  try {
    return await fn();
  } catch (err) {
    console.warn("[tulai/ipc] call failed:", formatCallError(err));
    return fallback;
  }
}

/**
 * Fire-and-forget IPC wrapper that catches synchronous throws and async rejections
 * (e.g. `mygo: runtime not found` in standalone Vite preview) while still invoking
 * the underlying method so unit-test spies record calls.
 */
export function fireIpc(fn: () => unknown): void {
  try {
    void Promise.resolve(fn()).catch(() => {});
  } catch {
    // Ignore synchronous runtime errors outside MyGo
  }
}

export function openExternalUrl(url: string): void {
  fireIpc(() => Core.openExternal(url));
  if (
    !isMyGoRuntime() &&
    typeof window !== "undefined" &&
    import.meta.env.MODE !== "test"
  ) {
    window.open(url, "_blank", "noopener,noreferrer");
  }
}

export function subscribeTopics(handler: (topicsCsv: string) => void): () => void {
  if (!isMyGoRuntime()) return () => {};
  return events.tulaiTopics.on((payload) => {
    if (payload && typeof payload.topics === "string" && payload.topics.length > 0) {
      handler(payload.topics);
    }
  });
}

/**
 * Subscribes to native OS file/folder drops from both `tulai:file-drop` (Go `win.OnFileDrop`)
 * and MyGo's built-in `onFileDrop`, deduplicating identical drops fired within 120ms.
 */
export function subscribeFileDrop(handler: (drop: FileDropEvent) => void): () => void {
  if (!isMyGoRuntime()) return () => {};

  let lastSig = "";
  let lastAt = 0;

  const deliver = (drop: FileDropEvent) => {
    if (!drop || !Array.isArray(drop.paths) || drop.paths.length === 0) return;
    const now = Date.now();
    const sig = drop.paths.join("\u0000");
    if (sig === lastSig && now - lastAt < 120) return;
    lastSig = sig;
    lastAt = now;
    handler(drop);
  };

  const offCustom = events.tulaiFileDrop.on(deliver);
  let offBuiltin = () => {};
  try {
    offBuiltin = mygoOnFileDrop(deliver);
  } catch {
    // Ignore if built-in runtime listener is unavailable
  }

  return () => {
    offCustom();
    offBuiltin();
  };
}

export interface UseFileDropOptions {
  enabled?: boolean;
  targetRef?: RefObject<HTMLElement | null>;
  onDrop: (paths: string[], pos: { x: number; y: number }) => void;
}

/**
 * Hook for page-level or region-level native OS file/folder drag-and-drop.
 * Tracks `isDragging` for `<DropZoneOverlay />` and delivers real OS paths via MyGo.
 */
export function useFileDrop({
  enabled = true,
  targetRef,
  onDrop,
}: UseFileDropOptions): { isDragging: boolean } {
  const [isDragging, setIsDragging] = useState(false);
  const onDropRef = useRef(onDrop);
  onDropRef.current = onDrop;
  const depthRef = useRef(0);

  useEffect(() => {
    if (!enabled) {
      setIsDragging(false);
      depthRef.current = 0;
      return;
    }

    const hasFiles = (e: DragEvent): boolean => {
      const types = e.dataTransfer?.types;
      if (!types) return true;
      return Array.from(types).includes("Files");
    };

    const onDragEnter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depthRef.current += 1;
      setIsDragging(true);
    };

    const onDragOver = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "copy";
    };

    const onDragLeave = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depthRef.current = Math.max(0, depthRef.current - 1);
      if (depthRef.current === 0) setIsDragging(false);
    };

    const onDomDrop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depthRef.current = 0;
      setIsDragging(false);
    };

    const onWindowBlur = () => {
      depthRef.current = 0;
      setIsDragging(false);
    };

    const target: EventTarget = targetRef?.current ?? window;
    target.addEventListener("dragenter", onDragEnter as EventListener);
    target.addEventListener("dragover", onDragOver as EventListener);
    target.addEventListener("dragleave", onDragLeave as EventListener);
    target.addEventListener("drop", onDomDrop as EventListener);
    window.addEventListener("blur", onWindowBlur);

    const offNative = subscribeFileDrop((drop) => {
      depthRef.current = 0;
      setIsDragging(false);
      if (targetRef?.current && Number.isFinite(drop.x) && Number.isFinite(drop.y)) {
        const rect = targetRef.current.getBoundingClientRect();
        if (
          rect.width > 0 &&
          rect.height > 0 &&
          (drop.x < rect.left || drop.x > rect.right || drop.y < rect.top || drop.y > rect.bottom)
        ) {
          return;
        }
      }
      onDropRef.current(drop.paths, { x: drop.x, y: drop.y });
    });

    return () => {
      target.removeEventListener("dragenter", onDragEnter as EventListener);
      target.removeEventListener("dragover", onDragOver as EventListener);
      target.removeEventListener("dragleave", onDragLeave as EventListener);
      target.removeEventListener("drop", onDomDrop as EventListener);
      window.removeEventListener("blur", onWindowBlur);
      offNative();
    };
  }, [enabled, targetRef]);

  return { isDragging };
}
