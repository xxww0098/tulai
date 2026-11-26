import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
} from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { HoverPreviewTarget, clearHoverPreview } from "@/components/HoverPreview";
import { EmptyNotice, StatusBadge } from "@/components/primitives";
import {
  Badge,
  Button,
  Icon,
  IconButton,
  Input,
  Tooltip,
  TooltipGroup,
} from "@/components/ui";
import { toThumbUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import type { EditFileDTO } from "@/store";

export type BatchEditFile = EditFileDTO & { hasCustomCrop?: boolean };

export type BatchQueueFilter = "all" | "picked" | "failed" | "ok" | "customCrop";

export type BatchPickStatus = "pending" | "ok" | "failed" | "customCrop";

export interface ImageEditFilmstripProps {
  compact: boolean;
  onToggleCompact: () => void;
  viewMode: "darkroom" | "grid";
  onViewModeChange: (mode: "darkroom" | "grid") => void;
  files: BatchEditFile[];
  selectedIndex: number;
  pickedCount: number;
  failCount: number;
  customCropCount: number;
  running: boolean;
  onSelectIndex: (idx: number) => void;
  onTogglePick: (idx: number, shift: boolean, cmd: boolean) => void;
  onRemoveIndex: (idx: number) => void;
  onSelectAllPicks: () => void;
  onInvertPicks: () => void;
  onPickByStatus: (status: BatchPickStatus) => void;
  onClearPicks: () => void;
  onRemovePicked: () => void;
  searchQuery?: string;
  onSearchQueryChange?: (query: string) => void;
  statusFilter?: BatchQueueFilter;
  onStatusFilterChange?: (filter: BatchQueueFilter) => void;
  className?: string;
}

export interface ImageEditBatchGridProps {
  files: BatchEditFile[];
  selectedIndex: number;
  pickedCount: number;
  failCount: number;
  customCropCount: number;
  running: boolean;
  onSelectIndex: (idx: number) => void;
  onInspectFile: (idx: number) => void;
  onTogglePick: (idx: number, shift: boolean, cmd: boolean) => void;
  onRemoveIndex: (idx: number) => void;
  onSelectAllPicks: () => void;
  onInvertPicks: () => void;
  onPickByStatus: (status: BatchPickStatus) => void;
  onClearPicks: () => void;
  onRemovePicked: () => void;
  onExitGrid: () => void;
  onPickFiles?: () => void;
  searchQuery?: string;
  onSearchQueryChange?: (query: string) => void;
  statusFilter?: BatchQueueFilter;
  onStatusFilterChange?: (filter: BatchQueueFilter) => void;
  className?: string;
}

function fileStatusMeta(status?: string): { label: string; tone: string } | null {
  switch (status) {
    case "ok":
      return { label: "已导出", tone: "ok" };
    case "failed":
      return { label: "失败", tone: "failed" };
    case "pending":
      return null;
    default:
      return status ? { label: status, tone: "neutral" } : null;
  }
}

function useQueueFilters(
  files: BatchEditFile[],
  pickedCount: number,
  failCount: number,
  customCropCount: number,
  controlledQuery?: string,
  onControlledQueryChange?: (query: string) => void,
  controlledStatus?: BatchQueueFilter,
  onControlledStatusChange?: (filter: BatchQueueFilter) => void,
) {
  const [internalQuery, setInternalQuery] = useState("");
  const [internalStatus, setInternalStatus] = useState<BatchQueueFilter>("all");

  const query = controlledQuery !== undefined ? controlledQuery : internalQuery;
  const statusFilter = controlledStatus !== undefined ? controlledStatus : internalStatus;

  const setQuery = useCallback(
    (next: string) => {
      if (onControlledQueryChange) {
        onControlledQueryChange(next);
      } else {
        setInternalQuery(next);
      }
    },
    [onControlledQueryChange],
  );

  const setStatusFilter = useCallback(
    (next: BatchQueueFilter) => {
      if (onControlledStatusChange) {
        onControlledStatusChange(next);
      } else {
        setInternalStatus(next);
      }
    },
    [onControlledStatusChange],
  );

  const clearFilters = useCallback(() => {
    setQuery("");
    setStatusFilter("all");
  }, [setQuery, setStatusFilter]);

  const counts = useMemo(() => {
    let ok = 0;
    let failed = 0;
    let picked = 0;
    let customCrop = 0;
    for (const f of files) {
      if (f.status === "ok") ok++;
      else if (f.status === "failed") failed++;
      if (f.picked) picked++;
      if (f.hasCustomCrop) customCrop++;
    }
    return {
      all: files.length,
      ok,
      failed: failCount > 0 ? failCount : failed,
      picked: pickedCount > 0 ? pickedCount : picked,
      customCrop: customCropCount > 0 ? customCropCount : customCrop,
    };
  }, [files, failCount, pickedCount, customCropCount]);

  const filteredFiles = useMemo(() => {
    const q = query.trim().toLowerCase();
    const isIndexLookup = q.startsWith("#") && q.length > 1;
    const indexTarget = isIndexLookup ? q.slice(1) : "";

    if (!q && statusFilter === "all") return files;

    return files.filter((f) => {
      if (statusFilter === "picked" && !f.picked) return false;
      if (statusFilter === "failed" && f.status !== "failed") return false;
      if (statusFilter === "ok" && f.status !== "ok") return false;
      if (statusFilter === "customCrop" && !f.hasCustomCrop) return false;
      if (q) {
        if (isIndexLookup) {
          if (String(f.index + 1) !== indexTarget) return false;
        } else if (!f.name.toLowerCase().includes(q)) {
          return false;
        }
      }
      return true;
    });
  }, [files, query, statusFilter]);

  const filterPills = useMemo(() => {
    const items: Array<{ key: BatchQueueFilter; label: string; count: number }> = [
      { key: "all", label: "全部", count: counts.all },
      { key: "picked", label: "已选", count: counts.picked },
      { key: "failed", label: "失败", count: counts.failed },
      { key: "ok", label: "已导出", count: counts.ok },
      { key: "customCrop", label: "独立裁剪", count: counts.customCrop },
    ];
    return items.filter(
      (item) => item.key === "all" || item.count > 0 || statusFilter === item.key,
    );
  }, [counts, statusFilter]);

  return {
    query,
    setQuery,
    statusFilter,
    setStatusFilter,
    clearFilters,
    counts,
    filteredFiles,
    filterPills,
    hasActiveFilter: Boolean(query.trim() || statusFilter !== "all"),
  };
}

function handleFileClick(
  e: ReactMouseEvent<HTMLElement>,
  fileIndex: number,
  onSelectIndex: (idx: number) => void,
  onTogglePick: (idx: number, shift: boolean, cmd: boolean) => void,
) {
  clearHoverPreview();
  if (e.shiftKey || e.metaKey || e.ctrlKey) {
    onTogglePick(fileIndex, e.shiftKey, e.metaKey || e.ctrlKey);
  } else {
    onSelectIndex(fileIndex);
  }
}

function FilterPillButton({
  label,
  count,
  active,
  danger,
  onClick,
}: {
  label: string;
  count: number;
  active: boolean;
  danger?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "tnum inline-flex h-5 cursor-pointer items-center gap-1 rounded-full border px-1.5 text-[10.5px] font-medium transition-colors select-none",
        active
          ? danger
            ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
            : "border-accent-border bg-accent-soft text-accent"
          : "border-transparent bg-sunken text-muted hover:bg-sunken-hover hover:text-ink",
      )}
    >
      <span>{label}</span>
      <span className="opacity-80">{count}</span>
    </button>
  );
}

function BatchActionChip({
  label,
  disabled = false,
  danger = false,
  onClick,
}: {
  label: string;
  disabled?: boolean;
  danger?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "inline-flex h-5 cursor-pointer items-center rounded-[6px] border px-1.5 text-[10.5px] font-medium transition-colors select-none",
        "disabled:pointer-events-none disabled:opacity-45",
        danger
          ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger hover:opacity-90"
          : "border-hairline bg-card text-muted hover:border-strong hover:bg-sunken hover:text-ink",
      )}
    >
      {label}
    </button>
  );
}

const CAROUSEL_CARD_W = 120;
const CAROUSEL_CARD_GAP = 8;

/**
 * Collapsible horizontal virtualized bottom carousel (`data-testid="edit-files"`)
 * with left/right scrolling controls, filename/status filtering, direct pick checkboxes,
 * and grid view switcher.
 */
export function ImageEditFilmstrip({
  compact,
  onToggleCompact,
  viewMode,
  onViewModeChange,
  files,
  selectedIndex,
  pickedCount,
  failCount,
  customCropCount,
  running,
  onSelectIndex,
  onTogglePick,
  onRemoveIndex,
  onSelectAllPicks,
  onInvertPicks,
  onPickByStatus,
  onClearPicks,
  onRemovePicked,
  searchQuery,
  onSearchQueryChange,
  statusFilter: controlledStatusFilter,
  onStatusFilterChange,
  className,
}: ImageEditFilmstripProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [scrollMetrics, setScrollMetrics] = useState({
    canScrollLeft: false,
    canScrollRight: false,
    hasOverflow: false,
  });

  const {
    query,
    setQuery,
    statusFilter,
    setStatusFilter,
    clearFilters,
    counts,
    filteredFiles,
    filterPills,
    hasActiveFilter,
  } = useQueueFilters(
    files,
    pickedCount,
    failCount,
    customCropCount,
    searchQuery,
    onSearchQueryChange,
    controlledStatusFilter,
    onStatusFilterChange,
  );

  const colVirtualizer = useVirtualizer({
    horizontal: true,
    count: filteredFiles.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => CAROUSEL_CARD_W,
    getItemKey: (index) =>
      filteredFiles[index]?.path || String(filteredFiles[index]?.index ?? index),
    overscan: 6,
    gap: CAROUSEL_CARD_GAP,
    initialRect: { width: 720, height: 100 },
  });

  const updateScrollButtons = useCallback(() => {
    const el = scrollRef.current;
    if (!el) {
      setScrollMetrics({ canScrollLeft: false, canScrollRight: false, hasOverflow: false });
      return;
    }
    const maxScroll = Math.max(0, el.scrollWidth - el.clientWidth);
    const hasOverflow = maxScroll > 2;
    const canScrollLeft = hasOverflow && el.scrollLeft > 2;
    const canScrollRight = hasOverflow && el.scrollLeft < maxScroll - 2;
    setScrollMetrics((prev) =>
      prev.canScrollLeft === canScrollLeft &&
      prev.canScrollRight === canScrollRight &&
      prev.hasOverflow === hasOverflow
        ? prev
        : { canScrollLeft, canScrollRight, hasOverflow },
    );
  }, []);

  useEffect(() => {
    if (compact) return;
    updateScrollButtons();
    const el = scrollRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => updateScrollButtons());
    ro.observe(el);
    return () => ro.disconnect();
  }, [compact, filteredFiles.length, updateScrollButtons]);

  // Keep selected thumbnail scrolled into view on keyboard or stage navigation
  useEffect(() => {
    if (compact || selectedIndex < 0) return;
    const visualIdx = filteredFiles.findIndex((f) => f.index === selectedIndex);
    if (visualIdx < 0) return;
    const el = scrollRef.current;
    if (el && typeof el.scrollTo === "function") {
      colVirtualizer.scrollToIndex(visualIdx, { align: "auto" });
    }
  }, [compact, selectedIndex, filteredFiles, colVirtualizer]);

  const curVisualIdx = useMemo(
    () => filteredFiles.findIndex((f) => f.index === selectedIndex),
    [filteredFiles, selectedIndex],
  );

  const prevDisabled = scrollMetrics.hasOverflow
    ? !scrollMetrics.canScrollLeft
    : filteredFiles.length <= 1 || curVisualIdx <= 0;
  const nextDisabled = scrollMetrics.hasOverflow
    ? !scrollMetrics.canScrollRight
    : filteredFiles.length <= 1 || curVisualIdx < 0 || curVisualIdx >= filteredFiles.length - 1;

  const handleCarouselStep = useCallback(
    (direction: -1 | 1) => {
      clearHoverPreview();
      const el = scrollRef.current;
      if (el && el.scrollWidth > el.clientWidth + 2) {
        const step = Math.max(240, Math.round((el.clientWidth || 480) * 0.72));
        if (typeof el.scrollBy === "function") {
          el.scrollBy({ left: direction * step, behavior: "smooth" });
        } else {
          el.scrollLeft += direction * step;
          updateScrollButtons();
        }
        return;
      }
      if (filteredFiles.length > 1) {
        const base = curVisualIdx >= 0 ? curVisualIdx : 0;
        const targetIdx = Math.max(0, Math.min(filteredFiles.length - 1, base + direction));
        const target = filteredFiles[targetIdx];
        if (target && target.index !== selectedIndex) {
          onSelectIndex(target.index);
        }
      }
    },
    [curVisualIdx, filteredFiles, onSelectIndex, selectedIndex, updateScrollButtons],
  );

  const nextViewMode = viewMode === "grid" ? "darkroom" : "grid";

  return (
    <aside
      data-testid="edit-files"
      className={cn(
        "bento-shadow flex w-full shrink-0 flex-col overflow-hidden rounded-[16px] border border-hairline bg-card",
        className,
      )}
    >
      {/* Header & Filter Strip */}
      <div
        className={cn(
          "flex shrink-0 flex-wrap items-center justify-between gap-2 px-3 py-2",
          !compact && "border-b border-divider",
        )}
      >
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
          <span className="tnum shrink-0 text-xs font-semibold text-ink">
            {`图片 (${files.length})`}
          </span>

          {!compact && files.length > 0 ? (
            <>
              <Input
                size="small"
                leftIcon="search"
                aria-label="筛选文件名"
                placeholder="筛选文件名..."
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                wrapperClassName="w-[144px] shrink-0"
                rightSlot={
                  query ? (
                    <button
                      type="button"
                      aria-label="清空筛选关键词"
                      onClick={() => setQuery("")}
                      className="flex size-4 cursor-pointer items-center justify-center rounded text-subtle hover:bg-sunken hover:text-ink"
                    >
                      <Icon name="x" size={11} />
                    </button>
                  ) : undefined
                }
              />

              {filterPills.length > 1 ? (
                <div className="flex flex-wrap items-center gap-1">
                  {filterPills.map((pill) => (
                    <FilterPillButton
                      key={pill.key}
                      label={pill.label}
                      count={pill.count}
                      active={statusFilter === pill.key}
                      danger={pill.key === "failed"}
                      onClick={() => setStatusFilter(pill.key)}
                    />
                  ))}
                </div>
              ) : null}
            </>
          ) : null}
        </div>

        <div className="flex shrink-0 flex-wrap items-center gap-1">
          {!compact && files.length > 0 ? (
            <>
              <BatchActionChip label="全选" onClick={onSelectAllPicks} />
              <BatchActionChip label="反选" onClick={onInvertPicks} />
              {counts.failed > 0 ? (
                <BatchActionChip
                  label="选中失败"
                  onClick={() => onPickByStatus("failed")}
                />
              ) : null}
              {counts.picked > 0 ? (
                <BatchActionChip label="清空选区" onClick={onClearPicks} />
              ) : null}
              {counts.picked > 0 && !running ? (
                <BatchActionChip
                  label="移除已选"
                  danger
                  onClick={onRemovePicked}
                />
              ) : null}
            </>
          ) : null}

          <TooltipGroup className="flex items-center gap-1">
            <Tooltip content={viewMode === "grid" ? "返回精修画布" : "网格批览"} side="top">
              <IconButton
                data-testid="edit-view-mode-toggle"
                icon="layout-grid"
                size={22}
                variant={viewMode === "grid" ? "primary" : "ghost"}
                aria-label="批览"
                onClick={() => onViewModeChange(nextViewMode)}
              />
            </Tooltip>
            <Tooltip content={compact ? "展开胶片列表" : "折叠胶片列表"} side="top">
              <IconButton
                icon={
                  <Icon
                    name="chevron-down"
                    size={13}
                    className={cn(
                      "transition-transform duration-180 ease-[cubic-bezier(0.16,1,0.3,1)]",
                      compact && "rotate-180",
                    )}
                  />
                }
                size={22}
                variant="ghost"
                aria-label={compact ? "展开胶片列表" : "折叠胶片列表"}
                onClick={onToggleCompact}
              />
            </Tooltip>
          </TooltipGroup>
        </div>
      </div>

      {/* Horizontal Carousel Body */}
      {!compact ? (
        files.length === 0 ? (
          <div className="flex items-center justify-center gap-2.5 px-3 py-3.5 text-center">
            <div className="flex size-8 items-center justify-center rounded-[10px] border border-hairline bg-sunken text-subtle">
              <Icon name="image" size={16} />
            </div>
            <div className="text-left">
              <div className="text-xs font-medium text-muted">暂无图片</div>
              <div className="text-[11px] text-subtle">点击上方「添加图片」或直接拖入文件</div>
            </div>
          </div>
        ) : filteredFiles.length === 0 && hasActiveFilter ? (
          <div className="flex h-[104px] items-center justify-center gap-2.5 px-3">
            <Icon name="search" size={15} className="text-subtle" />
            <span className="text-xs font-medium text-muted">无匹配图片</span>
            <span className="text-[11px] text-subtle">调整关键词或状态筛选</span>
            <Button
              size="xs"
              variant="outline"
              text="清空筛选"
              onClick={clearFilters}
            />
          </div>
        ) : (
          <div className="flex h-[108px] items-center gap-1.5 px-2 py-2">
            <Tooltip content="向左轮播" side="top">
              <IconButton
                data-testid="edit-carousel-prev"
                icon="chevron-left"
                size={24}
                variant="outline"
                disabled={prevDisabled}
                aria-label="向左轮播"
                onClick={() => handleCarouselStep(-1)}
                className="h-[74px] w-7 shrink-0 rounded-[10px]"
              />
            </Tooltip>

            <div
              ref={scrollRef}
              data-testid="edit-filmstrip-list"
              onScroll={() => {
                clearHoverPreview();
                updateScrollButtons();
              }}
              onWheel={(e) => {
                const el = scrollRef.current;
                if (!el || el.scrollWidth <= el.clientWidth + 2) return;
                if (Math.abs(e.deltaY) > Math.abs(e.deltaX) && e.deltaY !== 0) {
                  el.scrollLeft += e.deltaY;
                  updateScrollButtons();
                }
              }}
              className="h-full min-w-0 flex-1 overflow-x-auto overflow-y-hidden"
            >
              <div
                className="relative h-full"
                style={{ width: `${colVirtualizer.getTotalSize()}px` }}
              >
                {colVirtualizer.getVirtualItems().map((vCol) => {
                  const file = filteredFiles[vCol.index];
                  if (!file) return null;
                  const active = file.index === selectedIndex;
                  const statusMeta = fileStatusMeta(file.status);
                  const thumbSrc = toThumbUrl(file.fileUrl, 180);

                  return (
                    <div
                      key={vCol.key}
                      data-index={vCol.index}
                      style={{
                        position: "absolute",
                        top: 0,
                        bottom: 0,
                        left: 0,
                        width: `${CAROUSEL_CARD_W}px`,
                        transform: `translateX(${vCol.start}px)`,
                      }}
                    >
                      <HoverPreviewTarget
                        previewKey={`edit-file-${file.index}-${file.path}`}
                        src={file.fileUrl}
                        title={file.name}
                        subtitle={
                          file.err ||
                          (file.output ? `已输出至 ${file.output}` : undefined)
                        }
                        badge={statusMeta?.label}
                        badgeVariant={
                          statusMeta?.tone === "ok"
                            ? "success"
                            : statusMeta?.tone === "failed"
                              ? "danger"
                              : "neutral"
                        }
                        className="h-full w-full"
                      >
                        <div
                          role="button"
                          tabIndex={0}
                          data-testid={`edit-file-item-${file.index}`}
                          aria-selected={active}
                          onClick={(e) =>
                            handleFileClick(e, file.index, onSelectIndex, onTogglePick)
                          }
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault();
                              onSelectIndex(file.index);
                            }
                          }}
                          className={cn(
                            "group relative flex h-full w-full cursor-pointer flex-col overflow-hidden rounded-[11px] border p-1 transition-colors duration-150 select-none",
                            active
                              ? "border-accent bg-accent-soft ring-1 ring-accent/25"
                              : file.picked
                                ? "border-accent-border bg-accent-soft/45 hover:bg-accent-soft/70"
                                : "border-hairline/70 bg-sunken hover:border-strong hover:bg-sunken-hover",
                          )}
                        >
                          {/* Thumbnail */}
                          <div className="bg-checkerboard relative min-h-0 w-full flex-1 overflow-hidden rounded-[7px] ring-1 ring-black/5 dark:ring-white/10">
                            {thumbSrc ? (
                              <img
                                src={thumbSrc}
                                alt={file.name}
                                loading="lazy"
                                decoding="async"
                                className="h-full w-full object-cover transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:scale-[1.03]"
                              />
                            ) : null}

                            <button
                              type="button"
                              aria-label={file.picked ? "取消多选" : "多选此图"}
                              aria-pressed={Boolean(file.picked)}
                              onClick={(e) => {
                                e.stopPropagation();
                                clearHoverPreview();
                                onTogglePick(file.index, e.shiftKey, true);
                              }}
                              className={cn(
                                "absolute top-1 left-1 flex size-4 cursor-pointer items-center justify-center rounded-[5px] border shadow-xs transition-colors",
                                file.picked
                                  ? "border-accent bg-accent text-on-accent"
                                  : "border-strong/80 bg-card/85 text-transparent hover:border-accent",
                              )}
                            >
                              <Icon name="check" size={10} strokeWidth={2.6} />
                            </button>

                            <Tooltip content="移除" side="top">
                              <IconButton
                                icon="x"
                                variant="ghost"
                                size={18}
                                disabled={running}
                                aria-label="移除"
                                onClick={(e) => {
                                  e.stopPropagation();
                                  clearHoverPreview();
                                  onRemoveIndex(file.index);
                                }}
                                className="absolute top-1 right-1 bg-card/80 opacity-75 shadow-xs hover:bg-card hover:opacity-100"
                              />
                            </Tooltip>

                            {statusMeta || file.hasCustomCrop ? (
                              <div
                                className="pointer-events-none absolute right-1 bottom-1 left-1 flex flex-wrap items-center gap-1"
                              >
                                {statusMeta ? (
                                  <StatusBadge
                                    text={statusMeta.label}
                                    tone={statusMeta.tone}
                                    className="px-1 py-0 text-[9.5px] shadow-xs"
                                  />
                                ) : null}
                                {file.hasCustomCrop ? (
                                  <Badge
                                    text="独立"
                                    variant="accent"
                                    className="px-1 py-0 text-[9.5px] shadow-xs"
                                  />
                                ) : null}
                              </div>
                            ) : null}
                          </div>

                          {/* Compact Index & Name Footer */}
                          <div className="mt-1 flex shrink-0 items-center gap-1 px-0.5">
                            <span className="tnum shrink-0 text-[10px] font-medium text-subtle">
                              {`#${file.index + 1}`}
                            </span>
                            <span className="min-w-0 flex-1 truncate text-[10.5px] font-medium text-ink">
                              {file.name}
                            </span>
                          </div>
                        </div>
                      </HoverPreviewTarget>
                    </div>
                  );
                })}
              </div>
            </div>

            <Tooltip content="向右轮播" side="top">
              <IconButton
                data-testid="edit-carousel-next"
                icon="chevron-right"
                size={24}
                variant="outline"
                disabled={nextDisabled}
                aria-label="向右轮播"
                onClick={() => handleCarouselStep(1)}
                className="h-[74px] w-7 shrink-0 rounded-[10px]"
              />
            </Tooltip>
          </div>
        )
      ) : null}
    </aside>
  );
}

const GRID_HORIZONTAL_PAD = 24;
const GRID_GAP = 10;
const GRID_MIN_CARD_W = 152;
const GRID_CARD_FOOTER_H = 54;

/**
 * Row-virtualized multi-column Batch Contact Sheet (`data-testid="edit-batch-grid"`)
 * built for smooth 60fps browsing and multi-selection across 50-5000 images.
 */
export function ImageEditBatchGrid({
  files,
  selectedIndex,
  pickedCount,
  failCount,
  customCropCount,
  running,
  onSelectIndex,
  onInspectFile,
  onTogglePick,
  onRemoveIndex,
  onSelectAllPicks,
  onInvertPicks,
  onPickByStatus,
  onClearPicks,
  onRemovePicked,
  onExitGrid,
  onPickFiles,
  searchQuery,
  onSearchQueryChange,
  statusFilter: controlledStatusFilter,
  onStatusFilterChange,
  className,
}: ImageEditBatchGridProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [gridWidth, setGridWidth] = useState(720);

  const {
    query,
    setQuery,
    statusFilter,
    setStatusFilter,
    clearFilters,
    counts,
    filteredFiles,
    filterPills,
    hasActiveFilter,
  } = useQueueFilters(
    files,
    pickedCount,
    failCount,
    customCropCount,
    searchQuery,
    onSearchQueryChange,
    controlledStatusFilter,
    onStatusFilterChange,
  );

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;

    const update = () => {
      const next = el.clientWidth || el.getBoundingClientRect().width;
      if (next > 0) {
        setGridWidth((prev) => (Math.abs(prev - next) > 1 ? next : prev));
      }
    };
    update();

    if (typeof ResizeObserver !== "undefined") {
      const ro = new ResizeObserver(() => update());
      ro.observe(el);
      return () => ro.disconnect();
    }
    window.addEventListener("resize", update);
    return () => window.removeEventListener("resize", update);
  }, []);

  const innerWidth = Math.max(240, gridWidth - GRID_HORIZONTAL_PAD);
  const cols = Math.max(
    2,
    Math.min(6, Math.floor((innerWidth + GRID_GAP) / (GRID_MIN_CARD_W + GRID_GAP))),
  );
  const cardWidth = Math.max(120, (innerWidth - GRID_GAP * (cols - 1)) / cols);
  const previewHeight = Math.round(cardWidth * 0.78);
  const rowHeight = previewHeight + GRID_CARD_FOOTER_H;
  const rowCount = Math.ceil(filteredFiles.length / cols);

  const rowVirtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 3,
    gap: GRID_GAP,
    initialRect: { width: 720, height: 520 },
  });

  useEffect(() => {
    rowVirtualizer.measure();
  }, [rowVirtualizer, rowHeight, cols]);

  return (
    <section
      data-testid="edit-batch-grid"
      className={cn(
        "bento-shadow flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-[16px] border border-hairline bg-card",
        className,
      )}
    >
      {/* Top Bar */}
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-divider bg-card-header px-3.5 py-2.5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <Icon name="layout-grid" size={14} className="text-accent" />
          <span className="text-[13px] font-semibold text-ink">网格批览</span>
          <span className="tnum text-xs text-muted">
            {hasActiveFilter
              ? `显示 ${filteredFiles.length} / ${files.length} 张`
              : `共 ${files.length} 张`}
          </span>
          {counts.picked > 0 ? (
            <Badge text={`已选 ${counts.picked}`} variant="accent" />
          ) : null}
          {counts.customCrop > 0 ? (
            <Badge text={`独立裁剪 ${counts.customCrop}`} variant="neutral" />
          ) : null}
          {counts.failed > 0 ? (
            <Badge text={`失败 ${counts.failed}`} variant="danger" />
          ) : null}
        </div>

        <Button
          text="返回精修画布"
          size="small"
          variant="outline"
          icon="sliders-horizontal"
          onClick={() => {
            clearHoverPreview();
            onExitGrid();
          }}
        />
      </div>

      {/* Filter & Batch Operation Bar */}
      {files.length > 0 ? (
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-divider bg-card px-3.5 py-2">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
            <Input
              size="small"
              leftIcon="search"
              aria-label="批览筛选文件名"
              placeholder="筛选文件名..."
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              wrapperClassName="w-[180px] shrink-0"
              rightSlot={
                query ? (
                  <button
                    type="button"
                    aria-label="清空批览搜索"
                    onClick={() => setQuery("")}
                    className="flex size-4 cursor-pointer items-center justify-center rounded text-subtle hover:bg-sunken hover:text-ink"
                  >
                    <Icon name="x" size={11} />
                  </button>
                ) : undefined
              }
            />

            <div className="flex flex-wrap items-center gap-1">
              {filterPills.map((pill) => (
                <FilterPillButton
                  key={pill.key}
                  label={pill.label}
                  count={pill.count}
                  active={statusFilter === pill.key}
                  danger={pill.key === "failed"}
                  onClick={() => setStatusFilter(pill.key)}
                />
              ))}
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              text="全选"
              size="xs"
              variant="outline"
              onClick={onSelectAllPicks}
            />
            <Button
              text="反选"
              size="xs"
              variant="outline"
              onClick={onInvertPicks}
            />
            {counts.failed > 0 ? (
              <Button
                text="选中失败"
                size="xs"
                variant="outline"
                onClick={() => onPickByStatus("failed")}
              />
            ) : null}
            {counts.customCrop > 0 ? (
              <Button
                text="选中独立裁剪"
                size="xs"
                variant="outline"
                onClick={() => onPickByStatus("customCrop")}
              />
            ) : null}
            {counts.picked > 0 ? (
              <Button
                text="清空选区"
                size="xs"
                variant="ghost"
                onClick={onClearPicks}
              />
            ) : null}
            {counts.picked > 0 ? (
              <Button
                text="移除已选"
                size="xs"
                variant="danger"
                disabled={running}
                onClick={onRemovePicked}
              />
            ) : null}
          </div>
        </div>
      ) : null}

      {/* Virtualized Grid Viewport */}
      {files.length === 0 ? (
        <div className="flex min-h-0 flex-1 items-center justify-center p-6">
          <EmptyNotice
            icon="images"
            title="暂无待编辑图片"
            subtitle="点击上方「添加图片」「添加目录」或直接拖入图片与文件夹（单次最多 5000 张）"
            action={
              onPickFiles ? (
                <Button
                  size="small"
                  variant="primary"
                  icon="plus"
                  text="选择图片"
                  onClick={onPickFiles}
                />
              ) : undefined
            }
          />
        </div>
      ) : filteredFiles.length === 0 ? (
        <div className="flex min-h-0 flex-1 items-center justify-center p-6">
          <EmptyNotice
            icon="search"
            title="没有符合筛选条件的图片"
            subtitle="请调整文件名关键词或切换状态筛选"
            action={
              <Button
                size="small"
                variant="outline"
                text="清空筛选"
                onClick={clearFilters}
              />
            }
          />
        </div>
      ) : (
        <div
          ref={scrollRef}
          data-testid="edit-batch-grid-scroll"
          className="scroll-gutter min-h-0 flex-1 overflow-y-auto p-3"
        >
          <div
            className="relative w-full"
            style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
          >
            {rowVirtualizer.getVirtualItems().map((vRow) => {
              const startIdx = vRow.index * cols;
              const rowFiles = filteredFiles.slice(startIdx, startIdx + cols);
              return (
                <div
                  key={vRow.key}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    height: `${rowHeight}px`,
                    transform: `translateY(${vRow.start}px)`,
                    display: "grid",
                    gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
                    gridTemplateRows: "minmax(0, 1fr)",
                    gap: `${GRID_GAP}px`,
                    overflow: "hidden",
                  }}
                >
                  {rowFiles.map((file) => {
                    const active = file.index === selectedIndex;
                    const statusMeta = fileStatusMeta(file.status);
                    const thumbSrc = toThumbUrl(file.fileUrl, 240);

                    return (
                      <HoverPreviewTarget
                        key={file.path || file.index}
                        previewKey={`edit-grid-${file.index}-${file.path}`}
                        src={file.fileUrl}
                        title={file.name}
                        subtitle={
                          file.err ||
                          (file.output ? `已输出至 ${file.output}` : undefined)
                        }
                        badge={statusMeta?.label}
                        badgeVariant={
                          statusMeta?.tone === "ok"
                            ? "success"
                            : statusMeta?.tone === "failed"
                              ? "danger"
                              : "neutral"
                        }
                        className="h-full min-h-0 min-w-0"
                      >
                        <div
                          role="button"
                          tabIndex={0}
                          data-testid={`edit-grid-item-${file.index}`}
                          aria-selected={active}
                          onClick={(e) =>
                            handleFileClick(
                              e,
                              file.index,
                              onSelectIndex,
                              onTogglePick,
                            )
                          }
                          onDoubleClick={(e) => {
                            e.stopPropagation();
                            clearHoverPreview();
                            onInspectFile(file.index);
                          }}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              e.preventDefault();
                              clearHoverPreview();
                              onInspectFile(file.index);
                            } else if (e.key === " ") {
                              e.preventDefault();
                              onSelectIndex(file.index);
                            }
                          }}
                          className={cn(
                            "group flex h-full min-h-0 cursor-pointer flex-col overflow-hidden rounded-[12px] border transition-[border-color,background-color] duration-150 select-none",
                            active
                              ? "border-accent bg-accent-soft ring-1 ring-accent/30"
                              : file.picked
                                ? "border-accent-border bg-accent-soft/40 hover:bg-accent-soft/65"
                                : "border-hairline bg-sunken/70 hover:border-strong hover:bg-sunken",
                          )}
                        >
                          {/* Preview Thumbnail */}
                          <div className="bg-checkerboard relative min-h-0 flex-1 overflow-hidden">
                            {thumbSrc ? (
                              <img
                                src={thumbSrc}
                                alt={file.name}
                                loading="lazy"
                                decoding="async"
                                draggable={false}
                                className="absolute inset-0 h-full w-full object-contain p-1 transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:scale-[1.03] select-none"
                              />
                            ) : null}

                            {/* Top-left Pick Toggle Checkbox */}
                            <button
                              type="button"
                              aria-label={file.picked ? "取消多选" : "多选此图"}
                              aria-pressed={Boolean(file.picked)}
                              onClick={(e) => {
                                e.stopPropagation();
                                clearHoverPreview();
                                onTogglePick(file.index, e.shiftKey, true);
                              }}
                              className={cn(
                                "absolute top-1.5 left-1.5 z-10 flex size-5 cursor-pointer items-center justify-center rounded-[6px] border shadow-xs transition-all",
                                file.picked
                                  ? "border-accent bg-accent text-on-accent opacity-100"
                                  : "border-strong bg-card/90 text-transparent opacity-85 hover:border-accent hover:opacity-100",
                              )}
                            >
                              <Icon name="check" size={12} strokeWidth={2.6} />
                            </button>

                            {/* Top-right Status & Custom Crop Badges */}
                            {statusMeta || file.hasCustomCrop ? (
                              <div className="pointer-events-none absolute top-1.5 right-1.5 z-10 flex items-center gap-1">
                                {file.hasCustomCrop ? (
                                  <span className="inline-flex rounded-full bg-card/95 shadow-xs backdrop-blur-[2px]">
                                    <Badge
                                      text="独立"
                                      variant="accent"
                                      className="px-1.5 py-0 text-[10px]"
                                    />
                                  </span>
                                ) : null}
                                {statusMeta ? (
                                  <span className="inline-flex rounded-full bg-card/95 shadow-xs backdrop-blur-[2px]">
                                    <StatusBadge
                                      text={statusMeta.label}
                                      tone={statusMeta.tone}
                                      className="px-1.5 py-0 text-[10px]"
                                    />
                                  </span>
                                ) : null}
                              </div>
                            ) : null}
                          </div>

                          {/* Card Footer */}
                          <div className="flex h-[54px] shrink-0 flex-col justify-center gap-1 border-t border-divider bg-card px-2 py-1.5">
                            <div className="flex min-w-0 items-center gap-1">
                              <span className="tnum shrink-0 text-[10.5px] font-medium text-subtle">
                                {`#${file.index + 1}`}
                              </span>
                              <span className="min-w-0 flex-1 truncate text-[11px] font-medium text-ink">
                                {file.name}
                              </span>
                            </div>

                            <div className="flex items-center justify-between gap-1">
                              <span
                                className={cn(
                                  "min-w-0 flex-1 truncate text-[10px]",
                                  file.err
                                    ? "text-status-danger"
                                    : active
                                      ? "font-medium text-accent"
                                      : "text-subtle",
                                )}
                              >
                                {file.err
                                  ? file.err
                                  : active
                                    ? "当前精修"
                                    : "双击精修"}
                              </span>

                              <TooltipGroup className="touch-visible flex shrink-0 items-center gap-1 opacity-85 transition-opacity group-focus-within:opacity-100 group-hover:opacity-100">
                                <Tooltip content="进入精修画布" side="top">
                                  <button
                                    type="button"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      clearHoverPreview();
                                      onInspectFile(file.index);
                                    }}
                                    className="cursor-pointer rounded-[6px] border border-hairline bg-sunken px-1.5 py-0.5 text-[10.5px] font-medium text-ink transition-colors hover:border-accent hover:bg-accent-soft hover:text-accent"
                                  >
                                    精修
                                  </button>
                                </Tooltip>
                                <Tooltip content="移除" side="top">
                                  <IconButton
                                    icon="x"
                                    size={18}
                                    variant="ghost"
                                    disabled={running}
                                    aria-label="移除"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      clearHoverPreview();
                                      onRemoveIndex(file.index);
                                    }}
                                  />
                                </Tooltip>
                              </TooltipGroup>
                            </div>
                          </div>
                        </div>
                      </HoverPreviewTarget>
                    );
                  })}
                </div>
              );
            })}
          </div>
        </div>
      )}
    </section>
  );
}
