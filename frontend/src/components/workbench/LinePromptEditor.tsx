import { useCallback, useEffect, useMemo, useRef } from "react";
import { cn } from "@/lib/cn";

export interface LinePromptEditorProps {
  value: string;
  placeholder?: string;
  disabled?: boolean;
  onChange: (next: string) => void;
  onCompositionStart: () => void;
  onCompositionEnd: (finalValue?: string) => void;
  onBlur: () => void;
  className?: string;
}

interface LineRow {
  idx: number;
  text: string;
  label: string;
  isNonEmpty: boolean;
  promptOrder: number;
}

/**
 * One-prompt-per-line editor for text-to-image batch generation.
 * Uses a wrap-aligned, scroll-synced line-number mirror behind a native <textarea>
 * so wrapped prompts, CJK IME composition, Cmd+A, and multi-line paste stay exact.
 */
export function LinePromptEditor({
  value,
  placeholder,
  disabled = false,
  onChange,
  onCompositionStart,
  onCompositionEnd,
  onBlur,
  className,
}: LinePromptEditorProps) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const mirrorRef = useRef<HTMLDivElement | null>(null);

  const { rows, promptCount } = useMemo(() => {
    const rawLines = value === "" ? [""] : value.split(/\r?\n/);
    let order = 0;
    const mapped: LineRow[] = rawLines.map((text, idx) => {
      const isNonEmpty = text.trim().length > 0;
      if (isNonEmpty) {
        order += 1;
      }
      const label = isNonEmpty
        ? String(order)
        : value === "" && idx === 0
          ? "1"
          : "·";
      return { idx, text, label, isNonEmpty, promptOrder: order };
    });
    return { rows: mapped, promptCount: order };
  }, [value]);

  const gutterWidth = promptCount >= 1000 ? 42 : promptCount >= 100 ? 34 : 28;

  const syncScroll = useCallback(() => {
    const ta = textareaRef.current;
    const mirror = mirrorRef.current;
    if (ta && mirror) {
      mirror.scrollTop = ta.scrollTop;
    }
  }, []);

  useEffect(() => {
    syncScroll();
  }, [value, syncScroll]);

  return (
    <div
      className={cn(
        "relative flex flex-1 flex-col overflow-hidden rounded-[10px] border border-hairline bg-sunken/80 transition-[border-color,box-shadow,background-color] duration-150",
        "hover:border-[var(--text-subtle)] focus-within:border-accent focus-within:bg-field focus-within:ring-2 focus-within:ring-[var(--focus-ring)] dark:bg-[#0B0F17] dark:focus-within:bg-field",
        disabled && "cursor-not-allowed opacity-50",
        className,
      )}
    >
      <div
        aria-hidden="true"
        style={{ width: gutterWidth }}
        className="pointer-events-none absolute top-0 bottom-0 left-0 border-r border-divider/80 bg-card-header/60 dark:bg-[#0E1420]/85"
      />

      <div
        ref={mirrorRef}
        aria-hidden="true"
        data-testid="wb-prompt-line-gutter"
        style={{ scrollbarColor: "transparent transparent" }}
        className="scroll-gutter pointer-events-none absolute inset-0 overflow-x-hidden overflow-y-auto py-2 select-none [&::-webkit-scrollbar]:!bg-transparent [&::-webkit-scrollbar-thumb]:!bg-transparent"
      >
        {rows.map((row) => (
          <div
            key={row.idx}
            className={cn(
              "flex items-start",
              value !== "" &&
                row.isNonEmpty &&
                row.promptOrder % 2 === 0 &&
                "bg-ink/[0.018] dark:bg-white/[0.022]",
            )}
          >
            <span
              data-testid="wb-prompt-line-num"
              style={{ width: gutterWidth }}
              className={cn(
                "tnum shrink-0 pr-1.5 text-right text-[10px] leading-[20px] font-medium",
                row.isNonEmpty
                  ? "text-subtle"
                  : value === "" && row.idx === 0
                    ? "text-subtle/60"
                    : "text-subtle/35",
              )}
            >
              {row.label}
            </span>
            <div className="min-w-0 flex-1 pr-2.5 pl-2 font-sans text-[12px] leading-[20px] break-words whitespace-pre-wrap text-transparent">
              {row.text || "\u200B"}
            </div>
          </div>
        ))}
      </div>

      <textarea
        ref={textareaRef}
        data-testid="wb-prompt-editor"
        disabled={disabled}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        onCompositionStart={onCompositionStart}
        onCompositionEnd={(e) => onCompositionEnd(e.currentTarget.value)}
        onBlur={onBlur}
        onScroll={syncScroll}
        style={{ paddingLeft: gutterWidth + 8 }}
        className={cn(
          "scroll-gutter relative z-10 h-full w-full flex-1 resize-none overflow-x-hidden overflow-y-auto bg-transparent py-2 pr-2.5 font-sans text-[12px] leading-[20px] break-words whitespace-pre-wrap text-ink placeholder:text-subtle focus:outline-none disabled:cursor-not-allowed",
          className,
        )}
      />
    </div>
  );
}
