import { useCallback, useEffect, useRef, useState } from "react";

export interface UseSyncedDraftOptions {
  /** Debounce delay in ms before committing changes to async IPC. Default: 90ms. */
  debounceMs?: number;
}

export interface UseSyncedDraftResult {
  value: string;
  setValue: (next: string, immediate?: boolean) => void;
  onChange: (next: string) => void;
  onCompositionStart: () => void;
  onCompositionEnd: (finalValue?: string) => void;
  onBlur: () => void;
  flush: () => void;
  isDirty: boolean;
}

/**
 * Async-IPC & CJK-IME safe text draft hook.
 *
 * Keeps local React state during fast typing and Chinese IME composition so
 * async Go IPC round-trips never drop characters or jump the caret, while
 * automatically resyncing when `syncKey` changes (tab/template/account switch)
 * or when `serverValue` changes externally (e.g. clicking "默认词" / "填入工作台").
 */
export function useSyncedDraft(
  serverValue: string,
  syncKey: string | number,
  onCommit: (next: string) => void,
  options?: UseSyncedDraftOptions,
): UseSyncedDraftResult {
  const debounceMs = options?.debounceMs ?? 90;
  const [draft, setDraft] = useState<string>(serverValue);
  const [isDirty, setIsDirty] = useState(false);

  const onCommitRef = useRef(onCommit);
  onCommitRef.current = onCommit;

  const draftRef = useRef(draft);
  draftRef.current = draft;

  const lastSyncKeyRef = useRef(syncKey);
  const lastServerRef = useRef(serverValue);
  const lastCommittedRef = useRef(serverValue);
  const composingRef = useRef(false);
  const dirtyRef = useRef(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const commitNow = useCallback(
    (next: string) => {
      clearTimer();
      lastCommittedRef.current = next;
      dirtyRef.current = false;
      setIsDirty(false);
      onCommitRef.current(next);
    },
    [clearTimer],
  );

  // Sync when syncKey changes or external serverValue diverges from our commits
  useEffect(() => {
    if (syncKey !== lastSyncKeyRef.current) {
      lastSyncKeyRef.current = syncKey;
      lastServerRef.current = serverValue;
      lastCommittedRef.current = serverValue;
      clearTimer();
      dirtyRef.current = false;
      setIsDirty(false);
      setDraft(serverValue);
      return;
    }

    if (serverValue === lastServerRef.current) return;
    lastServerRef.current = serverValue;

    // Our own async echo returned from Go
    if (serverValue === lastCommittedRef.current) {
      if (draftRef.current === serverValue) {
        dirtyRef.current = false;
        setIsDirty(false);
      }
      return;
    }

    // External update (e.g. reset prompt, apply template) — adopt when not mid-composition or pending local edit
    if (!composingRef.current && timerRef.current === null && !dirtyRef.current) {
      lastCommittedRef.current = serverValue;
      setDraft(serverValue);
    }
  }, [serverValue, syncKey, clearTimer]);

  // Flush pending dirty edit on unmount
  useEffect(() => {
    return () => {
      if (timerRef.current !== null) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
        if (dirtyRef.current && draftRef.current !== lastCommittedRef.current) {
          onCommitRef.current(draftRef.current);
        }
      }
    };
  }, []);

  const setValue = useCallback(
    (next: string, immediate = false) => {
      draftRef.current = next;
      setDraft(next);
      dirtyRef.current = true;
      setIsDirty(true);

      if (composingRef.current && !immediate) return;

      clearTimer();
      if (immediate || debounceMs <= 0) {
        commitNow(next);
        return;
      }
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        if (!composingRef.current) {
          commitNow(draftRef.current);
        }
      }, debounceMs);
    },
    [clearTimer, commitNow, debounceMs],
  );

  const onChange = useCallback((next: string) => setValue(next, false), [setValue]);

  const onCompositionStart = useCallback(() => {
    composingRef.current = true;
    clearTimer();
  }, [clearTimer]);

  const onCompositionEnd = useCallback(
    (finalValue?: string) => {
      composingRef.current = false;
      const val = finalValue !== undefined ? finalValue : draftRef.current;
      draftRef.current = val;
      setDraft(val);
      commitNow(val);
    },
    [commitNow],
  );

  const flush = useCallback(() => {
    clearTimer();
    if (dirtyRef.current && draftRef.current !== lastCommittedRef.current) {
      commitNow(draftRef.current);
    }
  }, [clearTimer, commitNow]);

  const onBlur = useCallback(() => {
    composingRef.current = false;
    flush();
  }, [flush]);

  return {
    value: draft,
    setValue,
    onChange,
    onCompositionStart,
    onCompositionEnd,
    onBlur,
    flush,
    isDirty,
  };
}
