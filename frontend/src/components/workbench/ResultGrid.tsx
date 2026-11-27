import {
  memo,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { HoverPreviewTarget, clearHoverPreview } from "@/components/HoverPreview";
import { BentoCard, EmptyNotice, FlushPane, PaneHeader } from "@/components/primitives";
import {
  Badge,
  Button,
  ContextMenu,
  Icon,
  Tabs,
  Tooltip,
  type MenuActionItem,
} from "@/components/ui";
import { toThumbUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import { Browser, Core, Workbench, isMyGoRuntime } from "@/lib/ipc";
import { useEscapeHandler } from "@/lib/shortcuts";
import {
  clearWorkbenchResults,
  deleteWorkbenchResult,
  openInImageEdit,
  requeueWorkbenchDone,
  showToast,
  undoWorkbenchRequeue,
  useWorkbench,
  type ResultTileDTO,
  type WorkbenchStateDTO,
} from "@/store";
import { DotMatrixImageBox } from "@/components/dotmatrix/DotMatrixImageBox";
import { getBatchStaggerDelay } from "@/lib/dotmatrix/config";
import { GenerationStageCard, areResultTilesEqual } from "./GenerationStageCard";

function fireIpc(call: Promise<unknown>): void {
  void call.catch(() => {});
}

async function copyTileImage(path: string): Promise<void> {
  if (!path || !isMyGoRuntime()) return;
  try {
    await Core.copyImage(path);
    showToast("已复制图片到剪贴板", "success");
  } catch (err) {
    showToast("复制图片失败", "error", err instanceof Error ? err.message : undefined);
  }
}

// Persisted across tab unmounts so switching pages never replays dot-matrix reveals on old tiles
const revealedDoneKeysGlobal = new Set<string>();

export function resetResultGridArrivalForTests(): void {
  revealedDoneKeysGlobal.clear();
}

const DENSITY_OPTIONS = [
  { value: "sm", label: "小图" },
  { value: "md", label: "中图" },
];

const PENDING_GRID_STYLE: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(68px, 1fr))",
  gap: 6,
};

const ISSUE_GRID_STYLE: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(112px, 1fr))",
  gap: 8,
};

const DONE_GRID_SM_STYLE: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(104px, 1fr))",
  gap: 8,
};

const DONE_GRID_MD_STYLE: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(156px, 1fr))",
  gap: 8,
};

function formatOrderNo(taskIndex: number): string {
  const num = taskIndex > 0 ? taskIndex : 1;
  return `#${String(num).padStart(2, "0")}`;
}

function tileBadgeVariant(status?: string): "success" | "warn" | "danger" | "accent" | "neutral" {
  switch (status) {
    case "完成":
    case "已完成":
      return "success";
    case "进行中":
    case "生成中":
    case "排队中":
    case "重试中":
      return "accent";
    case "需要人工验证":
    case "需核对":
    case "待验证":
      return "warn";
    case "失败":
    case "已取消":
      return "danger";
    default:
      return "neutral";
  }
}

interface TileMenuContext {
  running: boolean;
  mode: string;
  batchMode: number;
}

function buildTileMenuItems(tile: ResultTileDTO, ctx: TileMenuContext): MenuActionItem[] {
  const groups: MenuActionItem[][] = [];
  const promptText = (tile.prompt || "").trim();

  if (tile.path) {
    groups.push([
      {
        id: "open",
        label: "系统默认打开",
        icon: "external-link",
        onSelect: () => {
          if (tile.path) fireIpc(Core.openPath(tile.path));
        },
      },
      {
        id: "reveal",
        label: "在文件夹中显示",
        icon: "folder-open",
        onSelect: () => {
          if (tile.path) fireIpc(Core.revealPath(tile.path));
        },
      },
    ]);
  }

  const copyGroup: MenuActionItem[] = [];
  if (tile.path) {
    copyGroup.push(
      {
        id: "copy-image",
        label: "复制图片",
        icon: "image",
        onSelect: () => {
          clearHoverPreview();
          if (tile.path) void copyTileImage(tile.path);
        },
      },
      {
        id: "copy-path",
        label: "复制路径",
        icon: "copy",
        onSelect: () => {
          if (!tile.path) return;
          fireIpc(Core.copyText(tile.path));
          showToast("已复制图片路径", "success");
        },
      },
    );
  }
  if (promptText) {
    copyGroup.push({
      id: "copy-prompt",
      label: "复制提示词",
      icon: "file-text",
      onSelect: () => {
        fireIpc(Core.copyText(promptText));
        showToast("已复制提示词", "success");
      },
    });
  }
  if (copyGroup.length > 0) groups.push(copyGroup);

  const workflowGroup: MenuActionItem[] = [];
  if (tile.path && !ctx.running && ctx.mode !== "wash") {
    const isFission = ctx.mode === "fission";
    workflowGroup.push({
      id: "use-as-source",
      label: isFission ? "作为裂变母图" : "作为参考图",
      icon: "rotate-ccw",
      onSelect: () => {
        clearHoverPreview();
        if (!tile.path) return;
        fireIpc(Workbench.addWorkbenchSources([tile.path]));
        if (isFission || ctx.batchMode === 1) {
          showToast(isFission ? "已加入裂变母图" : "已加入参考图", "success");
        }
      },
    });
  }
  if (tile.canRewash) {
    workflowGroup.push({
      id: "rewash",
      label: "放回待洗 · 二次洗",
      icon: "rotate-ccw",
      onSelect: () => {
        clearHoverPreview();
        void requeueWorkbenchDone([tile.taskIndex]);
      },
    });
  }
  if (tile.path) {
    workflowGroup.push({
      id: "send-to-edit",
      label: "发送到图片编辑",
      icon: "sliders-horizontal",
      onSelect: () => {
        clearHoverPreview();
        if (tile.path) void openInImageEdit(tile.path);
      },
    });
  }
  if (tile.canReplace) {
    workflowGroup.push({
      id: "replace",
      label: "替换原图",
      icon: "replace",
      onSelect: () => {
        fireIpc(Workbench.replaceResultImage(tile.taskIndex));
      },
    });
  }
  if (tile.actionText) {
    workflowGroup.push({
      id: "retry",
      label: tile.actionText,
      icon: "refresh-cw",
      onSelect: () => {
        fireIpc(Workbench.retryTask(tile.taskIndex));
      },
    });
  }
  if (workflowGroup.length > 0) groups.push(workflowGroup);

  if (!ctx.running) {
    groups.push([
      {
        id: "remove-from-list",
        label: "移除",
        icon: "x",
        danger: true,
        onSelect: () => {
          clearHoverPreview();
          void deleteWorkbenchResult(tile.taskIndex, tile.path || "", false);
        },
      },
    ]);
  }

  const items: MenuActionItem[] = [];
  for (let i = 0; i < groups.length; i++) {
    if (i > 0) items.push({ type: "separator", id: `sep-${i}` });
    items.push(...groups[i]);
  }
  return items;
}

const ResultTileCard = memo(
  function ResultTileCard({
    tile,
    kind = "done",
    activePlatform,
    running = false,
    mode = "wash",
    batchMode = 0,
    selectMode = false,
    selected = false,
    initialSettled = true,
    staggerDelayMs = 0,
    onToggleSelect,
  }: {
    tile: ResultTileDTO;
    kind?: "done" | "issue" | "active" | "queued";
    activePlatform?: string;
    running?: boolean;
    mode?: string;
    batchMode?: number;
    selectMode?: boolean;
    selected?: boolean;
    initialSettled?: boolean;
    staggerDelayMs?: number;
    onToggleSelect?: (taskIndex: number, shiftKey: boolean) => void;
  }) {
    const isDoneTile = kind === "done";
    const live = kind === "active" || kind === "queued";
    const rawImgSrc = live || (kind === "issue" && !tile.fileUrl) ? tile.srcUrl : tile.fileUrl;
    const thumbSrc = toThumbUrl(rawImgSrc, 360);
    const isIssueWithoutOutput = kind === "issue" && !tile.fileUrl;
    const menuItems = useMemo(
      () => buildTileMenuItems(tile, { running, mode, batchMode }),
      [tile, running, mode, batchMode],
    );
    const hasContextActions = !live && menuItems.length > 0;
    const hasFooterActions = Boolean(tile.actionText || (kind === "active" && tile.accountId > 0));

    const subtitleText = [
      tile.accountName || tile.platform,
      tile.platform && tile.accountName && tile.platform !== activePlatform ? tile.platform : "",
      tile.elapsed,
    ]
      .filter(Boolean)
      .join(" · ");
    const metaText = [
      tile.accountName,
      tile.elapsed,
      tile.platform && tile.platform !== activePlatform ? tile.platform : "",
    ]
      .filter(Boolean)
      .join(" · ");

    const handleArtworkClick = (e: ReactMouseEvent<HTMLDivElement>) => {
      clearHoverPreview();
      if (kind === "done" && tile.canRewash && (selectMode || e.shiftKey) && onToggleSelect) {
        e.preventDefault();
        onToggleSelect(tile.taskIndex, e.shiftKey);
        return;
      }
      if (!live && tile.path) {
        fireIpc(Workbench.setPreviewPath(tile.path));
      }
    };

  const cardBody = (
    <HoverPreviewTarget
      previewKey={`tile-${kind}-${tile.taskIndex}-${tile.path || ""}`}
      src={rawImgSrc}
      title={tile.title || `任务 ${formatOrderNo(tile.taskIndex)}`}
      subtitle={subtitleText}
      note={isDoneTile ? tile.qualityNote : tile.reason || tile.qualityNote}
      badge={
        live ? "输入源图" : kind === "issue"
          ? tile.status || "异常"
          : tile.qualityNote
            ? "质检提醒"
            : "已出图"
      }
      badgeVariant={live ? "neutral" : kind === "issue" || tile.qualityNote ? "warn" : "success"}
      className="min-w-0"
    >
      <div
        data-testid={`result-tile-card-${tile.taskIndex}`}
        className={cn(
          "group overflow-hidden rounded-[12px] border transition-[border-color,background-color] duration-150",
          selected
            ? "border-accent bg-accent-soft"
            : "border-hairline bg-card-header hover:border-strong dark:bg-sunken",
        )}
      >
        {/* Square compact artwork stage */}
        <div className="relative aspect-square w-full overflow-hidden bg-sunken dark:bg-[#0D1118]">
          <div
            role={(!live && tile.path) || selectMode ? "button" : undefined}
            tabIndex={(!live && tile.path) || selectMode ? 0 : undefined}
            onClick={handleArtworkClick}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                clearHoverPreview();
                if (kind === "done" && tile.canRewash && selectMode && onToggleSelect) {
                  onToggleSelect(tile.taskIndex, e.shiftKey);
                } else if (!live && tile.path) {
                  fireIpc(Workbench.setPreviewPath(tile.path));
                }
              }
            }}
            className={cn(
              "flex h-full w-full items-center justify-center p-1",
              (!live && tile.path) || selectMode ? "cursor-pointer" : "cursor-default",
            )}
          >
            {live ? (
              <GenerationStageCard tile={tile} queued={kind === "queued"} />
            ) : isIssueWithoutOutput ? (
              <DotMatrixImageBox
                status="error"
                errorText={tile.reason || tile.status || "生成失败"}
                onRetry={
                  tile.actionText
                    ? () => fireIpc(Workbench.retryTask(tile.taskIndex))
                    : undefined
                }
                retryLabel={tile.actionText || "重试"}
                cornerRadius={10}
                className="h-full w-full"
                boxClassName="h-full w-full"
                data-testid={`issue-dotmatrix-${tile.taskIndex}`}
              />
            ) : thumbSrc ? (
              <DotMatrixImageBox
                src={thumbSrc}
                alt={tile.title || `任务 ${formatOrderNo(tile.taskIndex)}`}
                initialSettled={initialSettled}
                forceReveal={!initialSettled}
                staggerDelayMs={staggerDelayMs}
                cornerRadius={10}
                fit="contain"
                transparentWhenSettled
                className="h-full w-full"
                boxClassName="h-full w-full"
                imgTestId={`result-tile-image-${tile.taskIndex}`}
                data-testid={`done-dotmatrix-${tile.taskIndex}`}
              />
            ) : (
              <Icon name="image" size={22} className="text-subtle" />
            )}
          </div>

          {/* Top-left issue badge on issue tiles */}
          {kind === "issue" ? (
            <div className="absolute top-1 left-1 z-10">
              <Badge text={tile.status || "-"} variant={tileBadgeVariant(tile.status)} />
            </div>
          ) : null}
        </div>

        {/* Done thumbnails render image-only; live/issue cards keep compact status & action footer */}
        {!isDoneTile ? (
          <div className="flex items-center justify-between gap-1 border-t border-divider px-2.5 py-1.5">
            <div className="flex min-w-0 flex-1 flex-col gap-0.5">
              <span className="truncate text-[11px] font-medium text-ink">
                {live ? `${formatOrderNo(tile.taskIndex)} · ${tile.title || tile.prompt || "生成任务"}` : tile.title || `任务 ${formatOrderNo(tile.taskIndex)}`}
              </span>
              {metaText ? (
                <span className="tnum truncate text-[10px] text-subtle">{metaText}</span>
              ) : null}
              {tile.reason ? (
                <span className="select-text truncate text-[10px] text-status-danger">
                  {tile.reason}
                </span>
              ) : tile.qualityNote ? (
                <Tooltip content={tile.qualityNote} side="top">
                  <span
                    tabIndex={0}
                    data-testid={`quality-note-${tile.taskIndex}`}
                    className="select-text truncate rounded text-[10px] text-status-warn focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                  >
                    {tile.qualityNote}
                  </span>
                </Tooltip>
              ) : null}
            </div>

            {hasFooterActions ? (
              <div className="flex shrink-0 items-center gap-0.5">
                {tile.actionText ? (
                  <Button
                    text={tile.actionText}
                    size="xs"
                    variant="outline"
                    onClick={() => fireIpc(Workbench.retryTask(tile.taskIndex))}
                  />
                ) : null}
                {kind === "active" && tile.accountId > 0 ? (
                  <Button
                    text={tile.status === "需要人工验证" ? "去验证" : "查看画面"}
                    size="xs"
                    variant="ghost"
                    onClick={() => fireIpc(Browser.openBrowserDrawer(tile.accountId))}
                  />
                ) : null}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </HoverPreviewTarget>
  );

    return (
      <div className="min-w-0" data-testid={`result-slot-${tile.taskIndex}`}>
        <ContextMenu disabled={!hasContextActions} items={menuItems}>
          {cardBody}
        </ContextMenu>
      </div>
    );
  },
  (prev, next) =>
    prev.kind === next.kind &&
    prev.activePlatform === next.activePlatform &&
    prev.running === next.running &&
    prev.mode === next.mode &&
    prev.batchMode === next.batchMode &&
    prev.selectMode === next.selectMode &&
    prev.selected === next.selected &&
    prev.initialSettled === next.initialSettled &&
    prev.staggerDelayMs === next.staggerDelayMs &&
    prev.onToggleSelect === next.onToggleSelect &&
    areResultTilesEqual(prev.tile, next.tile),
);

const PendingQueueTile = memo(
  function PendingQueueTile({ tile }: { tile: ResultTileDTO }) {
  const thumbSrc = toThumbUrl(tile.srcUrl, 360);
  const rewashCount = tile.rewashCount ?? 0;

  return (
    <HoverPreviewTarget
      previewKey={`pending-${tile.taskIndex}-${tile.srcUrl || ""}`}
      src={tile.srcUrl}
      title={tile.title || `任务 ${formatOrderNo(tile.taskIndex)}`}
      subtitle={
        rewashCount > 0
          ? `二次清洗（第 ${rewashCount + 1} 次排队）`
          : "排队等待空闲账号"
      }
      badge={rewashCount > 0 ? "二次洗" : "排队中"}
      badgeVariant={rewashCount > 0 ? "warn" : "neutral"}
      className="min-w-0"
    >
      <div
        data-testid={`pending-tile-${tile.taskIndex}`}
        className="group overflow-hidden rounded-[12px] border border-dashed border-strong/80 bg-card-header transition-colors duration-150 hover:border-strong dark:bg-sunken"
      >
        <div className="relative aspect-square w-full overflow-hidden bg-sunken dark:bg-[#0D1118]">
          <div className="flex h-full w-full items-center justify-center p-1">
            {thumbSrc ? (
              <img
                src={thumbSrc}
                alt={tile.title || `任务 ${formatOrderNo(tile.taskIndex)}`}
                loading="lazy"
                decoding="async"
                className="h-full w-full object-contain opacity-90 transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:scale-[1.03]"
              />
            ) : (
              <div className="flex flex-col items-center gap-1 px-1">
                <Icon name="clock" size={14} className="text-subtle" />
                {tile.prompt ? (
                  <span className="line-clamp-2 text-center text-[9.5px] leading-tight text-muted">
                    {tile.prompt}
                  </span>
                ) : null}
              </div>
            )}
          </div>

          <span className="tnum absolute top-[3px] left-[3px] rounded bg-slate-900/75 px-1 py-0.5 text-[9.5px] font-semibold leading-none text-white">
            {formatOrderNo(tile.taskIndex)}
          </span>

          {rewashCount > 0 ? (
            <span className="absolute bottom-[3px] left-[3px] rounded bg-amber-600/95 px-1 py-0.5 text-[9px] font-semibold leading-none text-white">
              二次洗
            </span>
          ) : null}
        </div>

        <div className="truncate border-t border-divider px-1.5 py-0.5 text-[10px] text-muted">
          {tile.title || `任务 ${formatOrderNo(tile.taskIndex)}`}
        </div>
      </div>
    </HoverPreviewTarget>
  );
},
(prev, next) => areResultTilesEqual(prev.tile, next.tile),
);

export function ResultGrid({ wb: propWb, starting = false }: { wb?: WorkbenchStateDTO; starting?: boolean } = {}) {
  const storeWb = useWorkbench();
  const wb = propWb ?? storeWb;
  const [density, setDensity] = useState<"sm" | "md">("sm");
  const [doneLimit, setDoneLimit] = useState(30);
  const [pendingExpanded, setPendingExpanded] = useState(false);
  const [doneCollapsed, setDoneCollapsed] = useState(false);
  const [selectMode, setSelectMode] = useState(false);
  const [selectedDone, setSelectedDone] = useState<number[]>([]);

  const lastAnchorRef = useRef<number | null>(null);
  const lastContextKeyRef = useRef<string | null>(null);
  const seenDoneKeysRef = useRef<Set<string>>(new Set());
  const arrivalMetaRef = useRef<Map<string, { initialSettled: boolean; staggerDelayMs: number }>>(
    new Map(),
  );

  const activeRows = wb?.activeRows ?? [];
  const pendingTiles = wb?.pendingTiles ?? [];
  const issueRows = wb?.issueRows ?? [];
  const doneTiles = wb?.doneTiles ?? [];
  const stagedSources = wb?.sources ?? [];
  const total = wb?.totalCount ?? 0;

  const activeTiles = useMemo(() => {
    const out: ResultTileDTO[] = [];
    let rowAcct = "";
    for (const r of activeRows) {
      if (r.header && r.title) rowAcct = r.title;
      for (const t of r.tiles ?? []) {
        out.push(t.accountName ? t : { ...t, accountName: rowAcct });
      }
    }
    return out;
  }, [activeRows]);

  const issueTiles = useMemo(() => {
    const out: ResultTileDTO[] = [];
    for (const r of issueRows) {
      for (const t of r.tiles ?? []) out.push(t);
    }
    return out;
  }, [issueRows]);

  const mode = wb?.mode || "wash";
  const isWash = mode === "wash";
  const activePlatform =
    wb?.platform ||
    (wb?.jimeng ? "即梦" : wb?.gemini ? "Gemini" : wb?.qwenIntl ? "千问国际" : wb?.qianwen ? "千问" : "豆包");
  const scopeKey = `${activePlatform}:${mode}`;

  const runOnPage = Boolean(wb?.running && wb.runPage === wb.page);
  const liveRun = starting || runOnPage || activeTiles.length > 0;
  const previousLiveScope = useRef<string | null>(null);
  useEffect(() => {
    previousLiveScope.current = liveRun ? scopeKey : null;
  }, [liveRun, scopeKey]);

  // Only reveal tiles that just finished downloading live in the current view;
  // mounting, switching sidebar tabs, switching workbench mode or platform NEVER triggers dot-matrix reveal.
  const tileIdentity = (t: ResultTileDTO) => `${scopeKey}:${t.taskIndex}:${t.key}:${t.fileUrl}`;
  if (wb) {
    if (lastContextKeyRef.current !== scopeKey) {
      lastContextKeyRef.current = scopeKey;
      seenDoneKeysRef.current = new Set();
      arrivalMetaRef.current.clear();
      lastAnchorRef.current = null;
      for (const t of doneTiles) {
        const k = tileIdentity(t);
        seenDoneKeysRef.current.add(k);
        revealedDoneKeysGlobal.add(k);
        arrivalMetaRef.current.set(k, { initialSettled: true, staggerDelayMs: 0 });
      }
    } else if (doneTiles.length === 0) {
      for (const k of seenDoneKeysRef.current) {
        revealedDoneKeysGlobal.delete(k);
      }
      seenDoneKeysRef.current.clear();
      arrivalMetaRef.current.clear();
    } else {
      const newlyArrived: ResultTileDTO[] = [];
      for (const t of doneTiles) {
        const k = tileIdentity(t);
        if (!seenDoneKeysRef.current.has(k)) {
          seenDoneKeysRef.current.add(k);
          newlyArrived.push(t);
        }
      }
      if (newlyArrived.length > 0) {
        let revealIdx = 0;
        for (const t of newlyArrived) {
          const k = tileIdentity(t);
          const cameFromLiveGeneration =
            !revealedDoneKeysGlobal.has(k) &&
            (liveRun || previousLiveScope.current === scopeKey);
          revealedDoneKeysGlobal.add(k);
          arrivalMetaRef.current.set(k, {
            initialSettled: !cameFromLiveGeneration,
            staggerDelayMs:
              cameFromLiveGeneration && newlyArrived.length > 1
                ? getBatchStaggerDelay(revealIdx++)
                : 0,
          });
        }
      }
    }
  }

  // Reset per-view selection and pagination state when switching between Doubao / Qwen or modes
  useEffect(() => {
    setSelectMode(false);
    setSelectedDone([]);
    setDoneCollapsed(false);
    setDoneLimit(30);
    setPendingExpanded(false);
    lastAnchorRef.current = null;
  }, [scopeKey]);

  const selectedSet = useMemo(() => new Set(selectedDone), [selectedDone]);
  const selectableDoneIndices = useMemo(
    () => doneTiles.filter((t) => Boolean(t.canRewash)).map((t) => t.taskIndex),
    [doneTiles],
  );
  const selectableDoneSet = useMemo(() => new Set(selectableDoneIndices), [selectableDoneIndices]);

  // Keep selections valid when doneTiles or canRewash changes
  useEffect(() => {
    setSelectedDone((prev) => {
      const next = prev.filter((idx) => selectableDoneSet.has(idx));
      return next.length === prev.length ? prev : next;
    });
    if (doneTiles.length === 0) {
      setSelectMode(false);
      setDoneCollapsed(false);
      lastAnchorRef.current = null;
    }
  }, [doneTiles, selectableDoneSet]);

  useEffect(() => {
    if (pendingTiles.length === 0) {
      setPendingExpanded(false);
    }
  }, [pendingTiles.length]);

  // Priority 10 Escape closes multi-select mode
  const exitSelectMode = useCallback(() => {
    setSelectedDone([]);
    setSelectMode(false);
    lastAnchorRef.current = null;
  }, []);

  useEscapeHandler("wb-multi-select", selectMode || selectedDone.length > 0, exitSelectMode, 10);

  useEffect(() => {
    if (selectableDoneIndices.length === 0) {
      setSelectMode(false);
      setSelectedDone([]);
      lastAnchorRef.current = null;
    }
  }, [selectableDoneIndices.length]);

  const doneTilesRef = useRef(doneTiles);
  doneTilesRef.current = doneTiles;
  const selectableDoneSetRef = useRef(selectableDoneSet);
  selectableDoneSetRef.current = selectableDoneSet;

  const toggleDoneSelect = useCallback((taskIndex: number, shiftKey = false) => {
    if (!selectableDoneSetRef.current.has(taskIndex)) return;
    if (shiftKey && lastAnchorRef.current !== null) {
      const currentDoneTiles = doneTilesRef.current;
      const anchorPos = currentDoneTiles.findIndex((t) => t.taskIndex === lastAnchorRef.current);
      const targetPos = currentDoneTiles.findIndex((t) => t.taskIndex === taskIndex);
      if (anchorPos >= 0 && targetPos >= 0) {
        const start = Math.min(anchorPos, targetPos);
        const end = Math.max(anchorPos, targetPos);
        const rangeIndices = currentDoneTiles
          .slice(start, end + 1)
          .filter((t) => Boolean(t.canRewash))
          .map((t) => t.taskIndex);
        setSelectedDone((prev) => {
          const set = new Set(prev);
          for (const idx of rangeIndices) set.add(idx);
          return Array.from(set);
        });
        setSelectMode(true);
        lastAnchorRef.current = taskIndex;
        return;
      }
    }

    setSelectedDone((prev) => {
      const set = new Set(prev);
      if (set.has(taskIndex)) set.delete(taskIndex);
      else set.add(taskIndex);
      return Array.from(set);
    });
    lastAnchorRef.current = taskIndex;
  }, []);

  if (!wb) return null;

  const showDone =
    !doneCollapsed &&
    (Boolean(wb.resultsDoneOpen) ||
      (activeRows.length === 0 && pendingTiles.length === 0 && issueRows.length === 0));

  const queuedForRun = starting || runOnPage;
  const liveSlots = [
    ...activeTiles.map((tile) => ({ tile, kind: "active" as const })),
    ...(!isWash && queuedForRun
      ? pendingTiles.map((tile) => ({ tile, kind: "queued" as const }))
      : []),
  ];
  const waitingForQueue = liveSlots.length === 0 && starting;
  const showPendingQueueCard = !isWash && !queuedForRun && pendingTiles.length > 0;
  const showWashLiveSummary = isWash && (liveRun || pendingTiles.length > 0);
  // Keep one position per task even when an earlier task returns several images.
  type ResultSlot = { tile: ResultTileDTO; kind: "active" | "queued" | "done"; key: string };
  const slots: ResultSlot[] = liveSlots.map((slot) => ({
    ...slot,
    key: `${scopeKey}:task:${slot.tile.taskIndex}`,
  }));
  const extraOutputs: ResultSlot[] = [];
  const seenTasks = new Set<number>();
  if (showDone) {
    for (const tile of doneTiles) {
      if (seenTasks.has(tile.taskIndex)) {
        extraOutputs.push({ tile, kind: "done", key: tileIdentity(tile) });
      } else {
        slots.push({ tile, kind: "done", key: `${scopeKey}:task:${tile.taskIndex}` });
        seenTasks.add(tile.taskIndex);
      }
    }
  }
  slots.sort((a, b) => a.tile.taskIndex - b.tile.taskIndex);
  for (const slot of extraOutputs) slots.push(slot);
  const hasPipelineWork =
    waitingForQueue ||
    showWashLiveSummary ||
    total > 0 ||
    activeRows.length > 0 ||
    pendingTiles.length > 0 ||
    issueRows.length > 0 ||
    doneTiles.length > 0;

  const visiblePending = pendingExpanded ? pendingTiles : pendingTiles.slice(0, 18);

  const showTotalSubtitle =
    total > 0 &&
    doneTiles.length === 0 &&
    activeTiles.length === 0 &&
    pendingTiles.length === 0 &&
    issueTiles.length === 0;

  return (
    <FlushPane data-testid="wb-result-gallery">
      {/* Unified Flush Pane Header matching AccountLanes */}
      <PaneHeader
        icon="layout-grid"
        title={wb.mode === "wash" || !wb.mode ? "洗图结果" : "生成结果"}
        subtitle={showTotalSubtitle ? `共 ${total} 个任务` : undefined}
        className="bg-card-header px-3.5 py-2"
        right={
          <div className="flex shrink-0 flex-wrap items-center gap-1.5">
            {wb.canUndoRewash ? (
              <Button
                data-testid="wb-undo-rewash-btn"
                text={`撤销放回 (${wb.rewashUndoCount || 1})`}
                size="small"
                icon="undo-2"
                variant="outline"
                onClick={() => void undoWorkbenchRequeue()}
              />
            ) : null}
            {issueTiles.length === 0 && wb.failedCount + wb.needsCount > 0 ? (
              <Button
                text="重试失败"
                size="small"
                icon="refresh-cw"
                variant="outline"
                onClick={() => fireIpc(Workbench.retryAllFailed())}
              />
            ) : null}
            {hasPipelineWork && !wb.running && !starting ? (
              <Tooltip content="清空当前结果列表（不删除本地图片文件）" side="bottom">
                <Button
                  data-testid="wb-clear-results-btn"
                  text="清空结果"
                  size="small"
                  icon="trash"
                  variant="ghost"
                  onClick={() => {
                    clearHoverPreview();
                    exitSelectMode();
                    setDoneCollapsed(false);
                    setDoneLimit(30);
                    setPendingExpanded(false);
                    void clearWorkbenchResults();
                  }}
                />
              </Tooltip>
            ) : null}
            <Button
              text="打开输出目录"
              size="small"
              icon="folder-open"
              variant="outline"
              onClick={() => fireIpc(Workbench.openOutputFolder())}
            />
          </div>
        }
      />

      {/* Full-Height Pipeline & Completed Gallery Body */}
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto p-3">
        {!hasPipelineWork ? (
          wb.mode === "free" && wb.batchMode === 0 ? (
            wb.plannedTasks > 0 ? (
              <EmptyNotice
                icon="sparkles"
                title={`已就绪，共 ${wb.plannedTasks} 个文生图任务`}
                subtitle="确认提示词、输出尺寸与可用账号后，点击左下角「开始文生图」"
                className="min-h-full w-full flex-1"
              />
            ) : (
              <EmptyNotice
                icon="sparkles"
                title="暂无文生图结果"
                subtitle="在左侧输入提示词（每行一条）或导入 TXT，再点击「开始文生图」"
                className="min-h-full w-full flex-1"
                action={
                  <Button
                    text="导入 TXT"
                    size="small"
                    icon="upload"
                    variant="outline"
                    onClick={() => fireIpc(Workbench.pickPromptTextFile())}
                  />
                }
              />
            )
          ) : stagedSources.length > 0 ? (
            wb.mode === "wash" &&
            wb.washedCount > 0 &&
            wb.washedCount >= (wb.sourcesTotal || stagedSources.length) ? (
              <EmptyNotice
                icon="sparkles"
                title="当前素材已全部洗完"
                subtitle="结果列表为空，可移除已洗素材或导入新图片继续洗图"
                className="min-h-full w-full flex-1"
                action={
                  <div className="flex items-center gap-2">
                    {!wb.running ? (
                      <Button
                        data-testid="wb-empty-clear-washed-btn"
                        text="移除已洗素材"
                        size="small"
                        icon="trash"
                        variant="outline"
                        onClick={() => fireIpc(Workbench.clearWashedWorkbenchSources())}
                      />
                    ) : null}
                    <Button
                      text="选择图片"
                      size="small"
                      icon="plus"
                      variant="outline"
                      onClick={() => fireIpc(Workbench.pickWorkbenchFiles())}
                    />
                  </div>
                }
              />
            ) : (
              <EmptyNotice
                icon="sparkles"
                title={
                  wb.mode === "free" && wb.batchMode === 1
                    ? `已导入 ${wb.sourcesTotal || stagedSources.length} 张参考图，等待开始`
                    : `已导入 ${wb.sourcesTotal || stagedSources.length} 张素材，等待开始`
                }
                subtitle={
                  wb.mode === "free" && wb.batchMode === 1
                    ? "确认改图描述、输出尺寸与可用账号后，点击左下角「开始图生图」"
                    : "确认左侧参数与可用账号后，点击左下角开始按钮"
                }
                className="min-h-full w-full flex-1"
              />
            )
          ) : (
            <EmptyNotice
              icon="sparkles"
              title={wb.mode === "free" && wb.batchMode === 1 ? "暂无图生图结果" : "暂无生成结果"}
              subtitle={
                wb.mode === "free" && wb.batchMode === 1
                  ? "在左侧添加或拖入参考图并填写改图要求，再点击「开始图生图」"
                  : "在左侧配置提示词与参考图后，点击左下角开始按钮"
              }
              className="min-h-full w-full flex-1"
              action={
                <Button
                  text={wb.mode === "free" && wb.batchMode === 1 ? "选择参考图" : "选择图片"}
                  size="small"
                  icon="plus"
                  variant="outline"
                  onClick={() => fireIpc(Workbench.pickWorkbenchFiles())}
                />
              }
            />
          )
        ) : (
          <div className="flex flex-col gap-3">
            {/* Queued items before a run (hidden in wash mode) */}
            {showPendingQueueCard ? (
              <BentoCard
                data-testid="wb-pending-queue"
                compact
                className="rounded-[14px] shadow-none"
              >
                <div className="flex flex-col gap-2.5">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-full bg-subtle" />
                      <span className="tnum text-[13px] font-semibold text-ink">
                        {`排队中 (${pendingTiles.length})`}
                      </span>
                    </div>
                    {pendingTiles.length > 18 ? (
                      <Button
                        text={pendingExpanded ? "收起" : "展开全部"}
                        size="small"
                        variant="ghost"
                        onClick={() => setPendingExpanded((prev) => !prev)}
                      />
                    ) : null}
                  </div>

                  <div style={PENDING_GRID_STYLE}>
                    {visiblePending.map((tile) => (
                      <PendingQueueTile key={tile.key || String(tile.taskIndex)} tile={tile} />
                    ))}
                  </div>
                </div>
              </BentoCard>
            ) : null}

            {/* Live result slots and completed gallery */}
            {waitingForQueue || liveSlots.length > 0 || showWashLiveSummary || doneTiles.length > 0 ? (
              <BentoCard compact className="rounded-[14px] shadow-none">
                <div className="flex flex-col gap-2.5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="relative inline-flex size-2 shrink-0 items-center justify-center">
                        {liveSlots.length > 0 || waitingForQueue ? (
                          <span
                            aria-hidden="true"
                            className="animate-tulai-live-ring absolute inset-0 rounded-full bg-status-ok"
                          />
                        ) : null}
                        <span
                          className={cn(
                            "relative size-2 rounded-full",
                            isWash && !liveRun && doneTiles.length === 0
                              ? "bg-subtle"
                              : "bg-status-ok",
                          )}
                        />
                      </span>
                      <span
                        data-testid="wb-results-status-summary"
                        className={cn(
                          "tnum text-[13px] font-semibold",
                          isWash && !liveRun && doneTiles.length === 0
                            ? "text-muted"
                            : "text-status-ok",
                        )}
                      >
                        {liveSlots.length > 0 || waitingForQueue || (isWash && liveRun)
                          ? `进行中 (${activeTiles.length}) · 排队 (${isWash || queuedForRun ? pendingTiles.length : 0}) · 已完成 (${doneTiles.length})`
                          : isWash && pendingTiles.length > 0
                            ? `排队 (${pendingTiles.length}) · 已完成 (${doneTiles.length} 张)`
                            : wb.doneTasks > 0 && wb.doneTasks !== doneTiles.length
                              ? `已完成 (${doneTiles.length} 张 / ${wb.doneTasks} 任务)`
                              : `已完成 (${doneTiles.length} 张)`}
                      </span>
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5">
                      {slots.length > 0 ? (
                        <Tabs
                          data-testid="wb-density-tabs"
                          variant="segmented"
                          size="small"
                          options={DENSITY_OPTIONS}
                          value={density}
                          onValueChange={(value) => setDensity(value === "md" ? "md" : "sm")}
                        />
                      ) : null}
                      {showDone && selectableDoneIndices.length > 0 ? (
                        <Button
                          data-testid="wb-batch-select-toggle"
                          text={selectMode ? "退出多选" : "批量选择"}
                          size="small"
                          variant={selectMode ? "outline" : "ghost"}
                          onClick={() => {
                            const next = !selectMode;
                            setSelectMode(next);
                            if (!next) {
                              setSelectedDone([]);
                              lastAnchorRef.current = null;
                            }
                          }}
                        />
                      ) : null}
                      {doneTiles.length > 0 && <Button
                        text={showDone ? "收起已完成" : "展开已完成"}
                        size="small"
                        variant="ghost"
                        onClick={() => {
                          const collapse = showDone;
                          setDoneCollapsed(collapse);
                          fireIpc(Workbench.setResultsDoneOpen(!collapse));
                        }}
                      />}
                    </div>
                  </div>

                  {showDone && selectableDoneIndices.length > 0 && (selectMode || selectedDone.length > 0) ? (
                    <div className="animate-tulai-banner-in flex flex-wrap items-center justify-between gap-2 rounded-[12px] border border-accent-border bg-accent-soft px-3 py-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="tnum text-xs font-medium text-accent">
                          {`已选 ${selectedDone.length} 张成图`}
                        </span>
                        <span className="text-[11px] text-muted">
                          Shift+点击可连续范围选择
                        </span>
                      </div>
                      <div className="flex items-center gap-1.5">
                        <Button
                          data-testid="batch-rewash-btn"
                          text={`放回待洗 · 二次洗 (${selectedDone.length})`}
                          size="small"
                          icon="rotate-ccw"
                          variant="primary"
                          disabled={Boolean(wb.running) || selectedDone.length === 0}
                          onClick={() => {
                            const picks = [...selectedDone];
                            if (picks.length === 0) return;
                            void requeueWorkbenchDone(picks);
                            setSelectedDone([]);
                            setSelectMode(false);
                            lastAnchorRef.current = null;
                          }}
                        />
                        <Button
                          text="全选"
                          size="small"
                          variant="outline"
                          onClick={() => setSelectedDone(selectableDoneIndices)}
                        />
                        <Button
                          text="取消"
                          size="small"
                          variant="ghost"
                          onClick={exitSelectMode}
                        />
                      </div>
                    </div>
                  ) : null}

                  {slots.length > 0 || waitingForQueue ? (
                    <div className="flex flex-col gap-2.5">
                      <div style={density === "md" ? DONE_GRID_MD_STYLE : DONE_GRID_SM_STYLE}>
                        {waitingForQueue ? (
                          <DotMatrixImageBox
                            data-testid="wb-starting-animation"
                            status="loading"
                            statusText="正在准备任务…"
                            showStatusLine
                            boxClassName="aspect-square"
                          />
                        ) : null}
                        {slots.slice(0, doneLimit).map(({ tile, kind, key }) => {
                          const arrival = arrivalMetaRef.current.get(tileIdentity(tile));
                          return (
                            <ResultTileCard
                              key={key}
                              tile={tile}
                              kind={kind}
                              activePlatform={activePlatform}
                              running={Boolean(wb.running)}
                              mode={wb.mode || "wash"}
                              batchMode={wb.batchMode ?? 0}
                              selectMode={selectMode}
                              selected={selectedSet.has(tile.taskIndex)}
                              initialSettled={arrival ? arrival.initialSettled : true}
                              staggerDelayMs={arrival ? arrival.staggerDelayMs : 0}
                              onToggleSelect={toggleDoneSelect}
                            />
                          );
                        })}
                      </div>
                      {slots.length > doneLimit ? (
                        <div className="flex items-center gap-2 pt-1">
                          <Button
                            text="显示更多 (+30)"
                            size="small"
                            variant="outline"
                            onClick={() => setDoneLimit((n) => n + 30)}
                          />
                          <Button
                            text="展开全部"
                            size="small"
                            variant="ghost"
                            onClick={() => setDoneLimit(slots.length)}
                          />
                        </div>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              </BentoCard>
            ) : null}
            {/* Issues / Needs Attention */}
            {issueTiles.length > 0 ? (
              <BentoCard compact className="rounded-[14px] shadow-none">
                <div className="flex flex-col gap-2.5">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-full bg-status-warn" />
                      <span className="tnum text-[13px] font-semibold text-status-warn">
                        {`需处理 (${issueTiles.length})`}
                      </span>
                    </div>
                    <Button
                      text="重试失败"
                      size="small"
                      icon="refresh-cw"
                      variant="outline"
                      onClick={() => fireIpc(Workbench.retryAllFailed())}
                    />
                  </div>
                  <div style={ISSUE_GRID_STYLE}>
                    {issueTiles.map((tile) => (
                      <ResultTileCard
                        key={tile.key || String(tile.taskIndex)}
                        tile={tile}
                        kind="issue"
                        activePlatform={activePlatform}
                        running={Boolean(wb.running)}
                        mode={wb.mode || "wash"}
                        batchMode={wb.batchMode ?? 0}
                      />
                    ))}
                  </div>
                </div>
              </BentoCard>
            ) : null}
          </div>
        )}
      </div>
    </FlushPane>
  );
}
