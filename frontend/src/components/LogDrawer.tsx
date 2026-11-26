import { useCallback, useEffect, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Icon } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Core, fireIpc } from "@/lib/ipc";
import { useEscapeHandler } from "@/lib/shortcuts";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import { showToast, toggleLogDrawer, useLogs, type LogsStateDTO } from "@/store";

interface LevelChipSpec {
  pick: number;
  label: string;
  count: (st: LogsStateDTO) => number;
  activeBg: string;
  activeBorder: string;
  activeText: string;
}

const LEVEL_CHIPS: LevelChipSpec[] = [
  {
    pick: 0,
    label: "全部",
    count: (st) => st.countAll,
    activeBg: "#F1F5F9",
    activeBorder: "#F1F5F9",
    activeText: "#090D16",
  },
  {
    pick: 1,
    label: "调试",
    count: (st) => st.countDebug,
    activeBg: "#242D3F",
    activeBorder: "#475569",
    activeText: "#F1F5F9",
  },
  {
    pick: 2,
    label: "信息",
    count: (st) => st.countInfo,
    activeBg: "#1E3A5F",
    activeBorder: "#3B82F6",
    activeText: "#93C5FD",
  },
  {
    pick: 3,
    label: "警告",
    count: (st) => st.countWarn,
    activeBg: "#451A03",
    activeBorder: "#D97706",
    activeText: "#FCD34D",
  },
  {
    pick: 4,
    label: "错误",
    count: (st) => st.countError,
    activeBg: "#450A0A",
    activeBorder: "#DC2626",
    activeText: "#FCA5A5",
  },
];

function levelTone(lvl?: string): { fg: string; bg: string; border: string } {
  switch (lvl) {
    case "DBG":
    case "DEBUG":
      return { fg: "#CBD5E1", bg: "#1B2230", border: "#334155" };
    case "WRN":
    case "WARN":
      return { fg: "#FCD34D", bg: "#451A03", border: "#D97706" };
    case "ERR":
    case "ERROR":
      return { fg: "#FCA5A5", bg: "#450A0A", border: "#DC2626" };
    case "OK":
      return { fg: "#86EFAC", bg: "#052E16", border: "#16A34A" };
    default:
      return { fg: "#93C5FD", bg: "#1E3A5F", border: "#3B82F6" };
  }
}

function LogToolButton({
  text,
  ariaLabel,
  testId,
  onClick,
}: {
  text: string;
  ariaLabel?: string;
  testId?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={ariaLabel || text}
      data-testid={testId}
      onClick={onClick}
      className="inline-flex h-6 shrink-0 cursor-pointer items-center justify-center rounded-[8px] border border-[#252F42] bg-[#1B2230] px-2.5 text-[11.5px] text-[#CBD5E1] transition-colors duration-150 hover:border-[#34425A] hover:bg-[#242D3F] hover:text-[#F1F5F9]"
    >
      {text}
    </button>
  );
}

export function LogDrawer() {
  const st = useLogs();
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [viewport, setViewport] = useState(() => ({
    w: typeof window !== "undefined" ? window.innerWidth : 1280,
    h: typeof window !== "undefined" ? window.innerHeight : 820,
  }));

  useEffect(() => {
    const onResize = () =>
      setViewport({ w: window.innerWidth || 1280, h: window.innerHeight || 820 });
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const closeDrawer = useCallback(() => {
    void toggleLogDrawer(false);
  }, []);

  useEscapeHandler("log-drawer", Boolean(st.open), closeDrawer, 20);

  const commitQuery = useCallback(
    (nextQuery: string) => {
      fireIpc(() => Core.setLogFilter(st.pick, nextQuery, st.source || "", Boolean(st.paused)));
    },
    [st.pick, st.source, st.paused],
  );

  const queryDraft = useSyncedDraft(st.query || "", st.open ? "open" : "closed", commitQuery, {
    debounceMs: 120,
  });

  const entries = st.entries ?? [];

  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: (idx) => (entries[idx]?.expanded && entries[idx]?.detail ? 48 : 24),
    getItemKey: (idx) => entries[idx]?.seq ?? idx,
    overscan: 12,
  });

  // Auto-follow newest entries while unpaused
  useEffect(() => {
    if (!st.open || st.paused || entries.length === 0) return;
    const el = scrollRef.current;
    if (!el) return;
    const id = requestAnimationFrame(() => {
      el.scrollTop = el.scrollHeight;
    });
    return () => cancelAnimationFrame(id);
  }, [st.open, st.paused, entries.length, st.lastAt]);

  if (!st.open) return null;

  const width = Math.min(880, Math.max(320, viewport.w - 24));
  const height = Math.min(520, Math.max(260, viewport.h - 24));
  const left = Math.round(Math.max(12, (viewport.w - width) / 2));
  const top = Math.round(Math.max(12, (viewport.h - height) / 2));

  const cycleSource = () => {
    const options = ["", ...(st.sources ?? [])];
    const idx = options.indexOf(st.source || "");
    const next = options[(idx + 1) % options.length] ?? "";
    fireIpc(() => Core.setLogFilter(st.pick, queryDraft.value, next, Boolean(st.paused)));
  };

  const copyAllShown = () => {
    const lines = entries.map(
      (e) =>
        `[${e.time || ""}] ${e.level || ""} ${e.source || ""} ${e.text || ""}${e.detail ? `\n${e.detail}` : ""}`,
    );
    fireIpc(() => Core.copyText(lines.join("\n")));
    showToast("已复制当前日志", "success");
  };

  const hasFilter = Boolean(queryDraft.value.trim() || st.source || st.pick !== 0);

  return (
    <section
      data-testid="log-drawer"
      aria-label="运行日志区"
      style={{ left, top, width, height }}
      className="fixed z-40 flex flex-col overflow-hidden rounded-[16px] border border-[#252F42] bg-[#0B0E14] text-[#F1F5F9] shadow-[0_16px_40px_rgba(0,0,0,0.5)] animate-tulai-drawer"
    >
      {/* Top Chrome: Header + Level Chips + Search & Source */}
      <div className="flex shrink-0 flex-col gap-2 border-b border-[#252F42] bg-[#0F131B] px-4 py-3">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Icon name="terminal" size={14} color="#94A3B8" />
            <span className="text-[13.5px] font-semibold text-[#F1F5F9]">运行日志</span>
            <span className="inline-flex h-5 items-center gap-1.5 rounded-full border border-[#252F42] bg-[#141923] px-2.5">
              <span
                className={cn(
                  "size-1.5 rounded-full",
                  st.paused ? "bg-[#94A3B8]" : "bg-[#34D399]",
                )}
              />
              <span className="text-[11px] text-[#CBD5E1]">
                {st.paused ? "已暂停" : "实时接收"}
              </span>
            </span>
          </div>

          <div className="flex items-center gap-1.5">
            <LogToolButton
              text={st.paused ? "继续跟随" : "暂停跟随"}
              onClick={() =>
                fireIpc(() =>
                  Core.setLogFilter(st.pick, queryDraft.value, st.source || "", !st.paused),
                )
              }
            />
            <LogToolButton text="导出" onClick={() => fireIpc(() => Core.exportLogs())} />
            <LogToolButton text="复制" onClick={copyAllShown} />
            <LogToolButton text="清空" onClick={() => fireIpc(() => Core.clearLogs())} />
            <LogToolButton
              text="关闭"
              ariaLabel="关闭运行日志"
              testId="log-close-btn"
              onClick={() => void toggleLogDrawer(false)}
            />
          </div>
        </div>

        {/* Level Chips */}
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-[11.5px] text-[#94A3B8]">级别</span>
          {LEVEL_CHIPS.map((chip) => {
            const on = st.pick === chip.pick;
            const count = chip.count(st);
            return (
              <button
                key={chip.pick}
                type="button"
                aria-label={`${chip.label} ${count}`}
                onClick={() =>
                  fireIpc(() =>
                    Core.setLogFilter(
                      chip.pick,
                      queryDraft.value,
                      st.source || "",
                      Boolean(st.paused),
                    ),
                  )
                }
                style={{
                  backgroundColor: on ? chip.activeBg : "#141923",
                  borderColor: on ? chip.activeBorder : "#252F42",
                  color: on ? chip.activeText : "#CBD5E1",
                }}
                className={cn(
                  "tnum inline-flex h-[22px] cursor-pointer items-center justify-center rounded-full border px-2.5 text-[11.5px] transition-colors duration-150",
                  on ? "font-semibold" : "hover:border-[#34425A] hover:bg-[#1B2230]",
                )}
              >
                {`${chip.label} ${count}`}
              </button>
            );
          })}
        </div>

        {/* Search + Source Filter */}
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={queryDraft.value}
            placeholder="搜索事件或字段"
            aria-label="搜索事件或字段"
            onChange={(e) => queryDraft.onChange(e.target.value)}
            onCompositionStart={queryDraft.onCompositionStart}
            onCompositionEnd={(e) => queryDraft.onCompositionEnd(e.currentTarget.value)}
            onBlur={queryDraft.onBlur}
            className="h-7 min-w-[120px] flex-1 rounded-[10px] border border-[#252F42] bg-[#141923] px-2.5 text-xs text-[#F1F5F9] placeholder:text-[#64748B] focus:border-[#3B82F6] focus:outline-none"
          />
          <LogToolButton text={st.source || "全部来源"} onClick={cycleSource} />
        </div>
      </div>

      {/* Dense Virtualized Console Rows */}
      <div
        ref={scrollRef}
        className="font-mono-console min-h-0 flex-1 overflow-y-auto bg-[#0B0E14] px-3 py-2"
      >
        {entries.length === 0 ? (
          <div className="flex items-center justify-center py-10">
            <span className="font-sans text-xs text-[#94A3B8]">
              {hasFilter ? "没有匹配的日志" : "每个任务的开始、提交和结束会写在这里"}
            </span>
          </div>
        ) : (
          <div
            className="relative w-full"
            style={{ height: `${virtualizer.getTotalSize()}px` }}
          >
            {virtualizer.getVirtualItems().map((vRow) => {
              const entry = entries[vRow.index];
              const tone = levelTone(entry.level);
              return (
                <div
                  key={vRow.key}
                  data-index={vRow.index}
                  ref={virtualizer.measureElement}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${vRow.start}px)`,
                  }}
                >
                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() => fireIpc(() => Core.toggleLogItem(entry.seq))}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        fireIpc(() => Core.toggleLogItem(entry.seq));
                      }
                    }}
                    className={cn(
                      "cursor-pointer rounded-[8px] px-2 transition-colors hover:bg-[#141923]",
                      entry.expanded && "bg-[#141923]",
                    )}
                  >
                    <div className="flex h-[22px] items-center gap-2">
                      <span className="tnum w-[84px] shrink-0 text-[11px] text-[#94A3B8]">
                        {entry.time || ""}
                      </span>
                      <span
                        style={{
                          backgroundColor: tone.bg,
                          borderColor: tone.border,
                          color: tone.fg,
                        }}
                        className="inline-flex h-4 w-9 shrink-0 items-center justify-center rounded-full border text-[10px] font-bold"
                      >
                        {entry.level || "INF"}
                      </span>
                      <span className="w-[104px] shrink-0 truncate text-[11px] text-[#7EB6FF]">
                        {entry.source ? `[${entry.source}]` : ""}
                      </span>
                      <span className="select-text min-w-0 flex-1 truncate text-[11.5px] text-[#F1F5F9]">
                        {entry.text || ""}
                      </span>
                      {entry.elapsed ? (
                        <span className="tnum w-[60px] shrink-0 text-right text-[11px] text-[#93C5FD]">
                          {entry.elapsed}
                        </span>
                      ) : null}
                    </div>
                    {entry.expanded && entry.detail ? (
                      <pre className="select-text whitespace-pre-wrap break-all px-2 pb-1.5 pt-0.5 text-[11px] leading-relaxed text-[#94A3B8]">
                        {entry.detail}
                      </pre>
                    ) : null}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Footer */}
      <div className="tnum flex shrink-0 items-center justify-between gap-2.5 border-t border-[#252F42] bg-[#0F131B] px-4 py-2 text-[11.5px]">
        <div className="flex items-center gap-2.5">
          <span
            className={cn(
              "size-[7px] rounded-full",
              st.paused ? "bg-[#94A3B8]" : "bg-[#34D399]",
            )}
          />
          <span className="text-[#CBD5E1]">{st.paused ? "已暂停跟随" : "实时跟随"}</span>
          <span className="text-[#CBD5E1]">{`共 ${st.total} 条`}</span>
          <span className="text-[#94A3B8]">{`显示 ${st.shown} 条`}</span>
        </div>
        {st.lastAt ? <span className="text-[#94A3B8]">{`最近收到 ${st.lastAt}`}</span> : null}
      </div>
    </section>
  );
}
