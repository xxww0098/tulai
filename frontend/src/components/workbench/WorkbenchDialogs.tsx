import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type WheelEvent as ReactWheelEvent,
} from "react";
import { Badge, Button, Checkbox, Dialog, Icon, IconButton, Input, Tooltip, TooltipGroup } from "@/components/ui";
import { toFullUrl, toThumbUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import { Core, Workbench } from "@/lib/ipc";
import { isEditableTarget, useEscapeHandler } from "@/lib/shortcuts";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import {
  requeueWorkbenchDone,
  showToast,
  usePromptParts,
  useWorkbench,
  type PromptPartItemDTO,
  type ResultTileDTO,
  type WorkbenchStateDTO,
} from "@/store";

function fireIpc(call: Promise<unknown>): void {
  void call.catch(() => {});
}

function PromptPartRow({
  item,
  openKey,
}: {
  item: PromptPartItemDTO;
  openKey: string;
}) {
  const partKey = item.key || "";
  const commit = useCallback(
    (nextVal: string) => {
      fireIpc(
        Workbench.setPromptPart(
          partKey,
          Boolean(item.enabled || nextVal.trim() !== ""),
          nextVal,
        ),
      );
    },
    [partKey, item.enabled],
  );

  const draft = useSyncedDraft(item.value || "", `${partKey}-${openKey}`, commit, {
    debounceMs: 90,
  });

  return (
    <div className="flex flex-col gap-2 rounded-[12px] border border-hairline bg-sunken p-3">
      <Checkbox
        label={item.name || item.key || ""}
        checked={Boolean(item.enabled)}
        onCheckedChange={(checked) => {
          draft.flush();
          fireIpc(Workbench.setPromptPart(partKey, checked, draft.value));
        }}
      />
      <Input
        value={draft.value}
        placeholder={`填写${item.name || ""}描述...`}
        onChange={(e) => draft.onChange(e.target.value)}
        onCompositionStart={draft.onCompositionStart}
        onCompositionEnd={(e) => draft.onCompositionEnd(e.currentTarget.value)}
        onBlur={draft.onBlur}
      />
    </div>
  );
}

export function WorkbenchDialogs({ wb: propWb }: { wb?: WorkbenchStateDTO } = {}) {
  const storeWb = useWorkbench();
  const wb = propWb ?? storeWb;
  const parts = usePromptParts();

  const [zoom, setZoom] = useState(1);
  const [pan, setPan] = useState({ x: 0, y: 0 });
  const [compareMode, setCompareMode] = useState(false);
  const [splitPct, setSplitPct] = useState(50);
  const [draggingPan, setDraggingPan] = useState(false);

  const stageRef = useRef<HTMLDivElement | null>(null);

  const previewTiles = useMemo(() => {
    if (!wb) return [] as ResultTileDTO[];
    const out: ResultTileDTO[] = [];
    const seen = new Set<string>();
    const push = (t: ResultTileDTO) => {
      if (!t.path || seen.has(t.path)) return;
      seen.add(t.path);
      out.push(t);
    };
    for (const r of wb.activeRows ?? []) {
      for (const t of r.tiles ?? []) push(t);
    }
    for (const r of wb.issueRows ?? []) {
      for (const t of r.tiles ?? []) push(t);
    }
    for (const t of wb.doneTiles ?? []) push(t);
    return out;
  }, [wb]);

  const previewPath = wb?.previewPath ?? "";
  const currentIndex = useMemo(() => {
    if (!previewPath) return -1;
    return previewTiles.findIndex((t) => t.path === previewPath);
  }, [previewPath, previewTiles]);

  const currentTile = currentIndex >= 0 ? previewTiles[currentIndex] : undefined;

  const fullOutputUrl = toFullUrl(wb?.previewUrl || currentTile?.fileUrl);
  const fullSourceUrl = toFullUrl(currentTile?.srcUrl);
  const canCompare = Boolean(fullSourceUrl && fullOutputUrl && fullSourceUrl !== fullOutputUrl);

  const closeStructured = useCallback(() => {
    fireIpc(Workbench.setWorkbenchField("structuredOpen", "false"));
  }, []);

  const closeGallery = useCallback(() => {
    fireIpc(Workbench.setGalleryOpen(false));
  }, []);

  const closePreview = useCallback(() => {
    fireIpc(Workbench.setPreviewPath(""));
  }, []);

  // Priority Escape Stack: 50 = Lightbox, 40 = Modal Dialogs
  useEscapeHandler("wb-lightbox", Boolean(wb?.previewOn), closePreview, 50);
  useEscapeHandler("wb-gallery", Boolean(wb?.galleryOn), closeGallery, 40);
  useEscapeHandler("wb-structured", Boolean(wb?.structuredOpen), closeStructured, 40);

  const stepPreview = useCallback(
    (delta: number) => {
      if (previewTiles.length <= 1) return;
      const base = currentIndex >= 0 ? currentIndex : 0;
      const nextIdx = (base + delta + previewTiles.length) % previewTiles.length;
      const nextPath = previewTiles[nextIdx]?.path;
      if (nextPath) fireIpc(Workbench.setPreviewPath(nextPath));
    },
    [currentIndex, previewTiles],
  );

  // Reset zoom/pan/compare whenever preview target changes
  useEffect(() => {
    setZoom(1);
    setPan({ x: 0, y: 0 });
    setCompareMode(false);
    setSplitPct(50);
  }, [previewPath, wb?.previewOn]);

  const clampZoom = useCallback((next: number) => {
    const clamped = Math.max(1, Math.min(4, Math.round(next * 100) / 100));
    setZoom(clamped);
    if (clamped <= 1) setPan({ x: 0, y: 0 });
  }, []);

  const toggleZoom = useCallback(() => {
    setZoom((prev) => {
      if (prev > 1.05) {
        setPan({ x: 0, y: 0 });
        return 1;
      }
      return 2;
    });
  }, []);

  // Lightbox keyboard shortcuts: Left/Right nav, +/-/0 zoom, C compare toggle
  useEffect(() => {
    if (!wb?.previewOn) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing || e.metaKey || e.ctrlKey || e.altKey) return;
      if (isEditableTarget(e.target)) return;

      if (e.key === "ArrowLeft") {
        e.preventDefault();
        stepPreview(-1);
      } else if (e.key === "ArrowRight") {
        e.preventDefault();
        stepPreview(1);
      } else if (e.key === "+" || e.key === "=") {
        e.preventDefault();
        setCompareMode(false);
        setZoom((z) => Math.min(4, Math.round((z + 0.5) * 100) / 100));
      } else if (e.key === "-") {
        e.preventDefault();
        setZoom((z) => {
          const next = Math.max(1, Math.round((z - 0.5) * 100) / 100);
          if (next <= 1) setPan({ x: 0, y: 0 });
          return next;
        });
      } else if (e.key === "0") {
        e.preventDefault();
        setZoom(1);
        setPan({ x: 0, y: 0 });
      } else if (e.key.toLowerCase() === "c" && canCompare) {
        e.preventDefault();
        setCompareMode((prev) => {
          const next = !prev;
          if (next) {
            setZoom(1);
            setPan({ x: 0, y: 0 });
          }
          return next;
        });
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [wb?.previewOn, stepPreview, canCompare]);

  if (!wb) return null;

  const handleStageWheel = (e: ReactWheelEvent<HTMLDivElement>) => {
    if (compareMode) return;
    e.preventDefault();
    const factor = e.deltaY < 0 ? 1.15 : 0.87;
    clampZoom(zoom * factor);
  };

  const handlePanStart = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (compareMode || zoom <= 1 || e.button !== 0) return;
    e.preventDefault();
    setDraggingPan(true);
    const startX = e.clientX;
    const startY = e.clientY;
    const origPan = { ...pan };

    const onMove = (ev: PointerEvent) => {
      setPan({
        x: origPan.x + (ev.clientX - startX),
        y: origPan.y + (ev.clientY - startY),
      });
    };
    const onUp = () => {
      setDraggingPan(false);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  const updateSplitFromClientX = (clientX: number) => {
    const el = stageRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0) return;
    const pct = ((clientX - rect.left) / rect.width) * 100;
    setSplitPct(Math.max(5, Math.min(95, Math.round(pct * 10) / 10)));
  };

  const handleCompareDragStart = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    updateSplitFromClientX(e.clientX);

    const onMove = (ev: PointerEvent) => {
      updateSplitFromClientX(ev.clientX);
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  };

  const galleryItems = wb.galleryItems ?? [];
  const promptItems = parts?.items ?? [];

  return (
    <>
      {/* 结构化提示词编辑器 */}
      <Dialog
        open={Boolean(wb.structuredOpen)}
        onOpenChange={(open) => {
          if (!open) closeStructured();
        }}
        title="结构化提示词拼装"
        description="按主体、背景、风格等模块填写，自动拼接并同步到输入框"
        width={580}
        className="h-[min(540px,calc(100vh-24px))]"
        bodyClassName="flex min-h-0 flex-1 flex-col gap-2.5 overflow-y-auto p-4"
        data-testid="structured-dialog"
        closeOnBackdrop
        closeOnEscape
      >
        {promptItems.map((item) => (
          <PromptPartRow
            key={item.key || item.name}
            item={item}
            openKey={wb.structuredOpen ? "open" : "closed"}
          />
        ))}
      </Dialog>

      {/* 素材画廊管理 */}
      <Dialog
        open={Boolean(wb.galleryOn)}
        onOpenChange={(open) => {
          if (!open) closeGallery();
        }}
        title="全部素材图片"
        description={`已勾选 ${wb.gallerySelected} / ${galleryItems.length} 张参与本次任务`}
        width={640}
        className="h-[min(540px,calc(100vh-24px))]"
        bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden p-4"
        data-testid="gallery-dialog"
        closeOnBackdrop
        closeOnEscape
      >
        <div
          data-testid="gallery-dialog-body"
          className="flex min-h-0 w-full min-w-0 flex-1 flex-col gap-3"
        >
          <div className="flex shrink-0 items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Button
                text={wb.galleryAll ? "全不选" : "全选"}
                size="small"
                variant="outline"
                onClick={() => fireIpc(Workbench.setAllGallerySelect(!wb.galleryAll))}
              />
              {(wb.mode === "wash" || !wb.mode) && wb.washedCount > 0 && !wb.running ? (
                <Button
                  data-testid="gallery-clear-washed-btn"
                  text={`移除已洗 (${wb.washedCount})`}
                  size="small"
                  variant="outline"
                  onClick={() => fireIpc(Workbench.clearWashedWorkbenchSources())}
                />
              ) : null}
            </div>
            <Button text="完成" size="small" variant="primary" onClick={closeGallery} />
          </div>

          <div className="scroll-gutter flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto pr-1">
            {galleryItems.map((item) => {
              const thumb = toThumbUrl(item.fileUrl, 360);
              return (
                <div
                  key={item.path || String(item.index)}
                  className="flex items-center gap-3 rounded-[12px] bg-sunken p-2.5 transition-colors duration-150"
                >
                  <div className="flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-[10px] bg-card dark:bg-[#0C1017]">
                    {thumb ? (
                      <img
                        src={thumb}
                        alt={item.name || `素材 #${item.index + 1}`}
                        loading="lazy"
                        decoding="async"
                        className="size-12 object-cover"
                        data-testid={`gallery-thumb-${item.index}`}
                      />
                    ) : (
                      <Icon name="image" size={16} className="text-subtle" />
                    )}
                  </div>
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="truncate text-xs font-medium text-ink">
                      {item.name || item.path}
                    </span>
                    <span className="select-text truncate text-[11px] text-muted">
                      {item.washing ? "洗图中..." : item.washed ? "已洗图" : item.path}
                    </span>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Checkbox
                      label="参与"
                      checked={Boolean(item.selected)}
                      onCheckedChange={(checked) =>
                        fireIpc(Workbench.toggleGallerySelect(item.path || "", checked))
                      }
                    />
                    <Button
                      text="移除"
                      size="small"
                      variant="ghost"
                      onClick={() =>
                        fireIpc(Workbench.removeGallerySourceItem(item.path || ""))
                      }
                    />
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </Dialog>

      {/* 大图预览 Lightbox（自适应视口 + 缩放/拖拽 + 原图/成图分屏对比 + 键盘导航） */}
      <Dialog
        open={Boolean(wb.previewOn)}
        onOpenChange={(open) => {
          if (!open) closePreview();
        }}
        title={currentTile?.title || "图片预览"}
        description={
          wb.previewPath ? (
            <span className="select-text font-mono-console text-[11px]">{wb.previewPath}</span>
          ) : undefined
        }
        headerRight={
          <div className="flex items-center gap-1">
            {canCompare ? (
              <Button
                data-testid="lightbox-compare-toggle"
                text={compareMode ? "退出对比" : "对比原图"}
                size="xs"
                icon="columns-2"
                variant={compareMode ? "primary" : "outline"}
                onClick={() => {
                  setCompareMode((prev) => {
                    const next = !prev;
                    if (next) {
                      setZoom(1);
                      setPan({ x: 0, y: 0 });
                    }
                    return next;
                  });
                }}
              />
            ) : null}
            {!compareMode ? (
              <TooltipGroup className="flex items-center gap-0.5 rounded-[8px] border border-hairline bg-sunken px-1 py-0.5">
                <Tooltip content="缩小" shortcut="-">
                  <IconButton
                    icon="zoom-out"
                    size={22}
                    iconSize={12}
                    variant="ghost"
                    aria-label="缩小"
                    disabled={zoom <= 1}
                    onClick={() => clampZoom(zoom - 0.5)}
                  />
                </Tooltip>
                <Tooltip content="点击切换 100% / 200% 缩放">
                  <button
                    type="button"
                    data-testid="lightbox-zoom-toggle"
                    onClick={toggleZoom}
                    className="tnum min-w-[42px] cursor-pointer rounded px-1 py-0.5 text-center text-[11px] font-medium text-ink hover:bg-sunken-hover"
                  >
                    {`${Math.round(zoom * 100)}%`}
                  </button>
                </Tooltip>
                <Tooltip content="放大" shortcut="+">
                  <IconButton
                    icon="zoom-in"
                    size={22}
                    iconSize={12}
                    variant="ghost"
                    aria-label="放大"
                    disabled={zoom >= 4}
                    onClick={() => clampZoom(zoom + 0.5)}
                  />
                </Tooltip>
              </TooltipGroup>
            ) : null}
          </div>
        }
        width="min(92vw, 1080px)"
        maxWidth={1080}
        className="h-[min(86vh,760px)]"
        bodyClassName="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden p-4"
        data-testid="lightbox-dialog"
        closeOnBackdrop
        closeOnEscape
      >
        <div
          ref={stageRef}
          onWheel={handleStageWheel}
          onPointerDown={compareMode ? handleCompareDragStart : handlePanStart}
          onDoubleClick={() => {
            if (!compareMode) toggleZoom();
          }}
          className={cn(
            "bg-checkerboard relative flex min-h-0 flex-1 items-center justify-center overflow-hidden rounded-[14px] border border-hairline select-none",
            compareMode
              ? "cursor-ew-resize"
              : zoom > 1
                ? draggingPan
                  ? "cursor-grabbing"
                  : "cursor-grab"
                : "cursor-zoom-in",
          )}
        >
          {fullOutputUrl ? (
            compareMode && fullSourceUrl ? (
              <>
                {/* Right layer: Output (成图) */}
                <img
                  src={fullOutputUrl}
                  alt={currentTile?.title || "成图预览"}
                  draggable={false}
                  className="pointer-events-none h-full w-full object-contain"
                  data-testid="lightbox-preview-image"
                />
                {/* Left layer: Original source (原图) clipped at splitPct */}
                <img
                  src={fullSourceUrl}
                  alt="原图对比"
                  draggable={false}
                  style={{ clipPath: `inset(0 ${100 - splitPct}% 0 0)` }}
                  className="pointer-events-none absolute inset-0 h-full w-full object-contain"
                  data-testid="lightbox-compare-source"
                />
                {/* Floating comparison tags */}
                <span className="pointer-events-none absolute top-3 left-3 rounded-full bg-slate-900/75 px-2.5 py-0.5 text-[11px] font-medium text-white">
                  原图
                </span>
                <span className="pointer-events-none absolute top-3 right-3 rounded-full bg-emerald-700/85 px-2.5 py-0.5 text-[11px] font-medium text-white">
                  成图
                </span>
                {/* Vertical split curtain + grab handle */}
                <div
                  style={{ left: `${splitPct}%` }}
                  className="pointer-events-none absolute top-0 bottom-0 w-[2px] -translate-x-1/2 bg-accent shadow-[0_0_8px_rgba(37,99,235,0.45)]"
                >
                  <div className="absolute top-1/2 left-1/2 flex size-7 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border border-white/80 bg-accent text-on-accent shadow-md">
                    <Icon name="flip-horizontal" size={14} />
                  </div>
                </div>
              </>
            ) : (
              <img
                key={fullOutputUrl}
                src={fullOutputUrl}
                alt={currentTile?.title || "图片预览"}
                decoding="async"
                draggable={false}
                data-testid="lightbox-preview-image"
                onClick={(e) => {
                  if (zoom <= 1) {
                    e.stopPropagation();
                    toggleZoom();
                  }
                }}
                style={{
                  transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})`,
                  transition: draggingPan
                    ? "none"
                    : "transform 160ms cubic-bezier(0.2, 0, 0, 1)",
                }}
                className="animate-tulai-fade h-full w-full object-contain"
              />
            )
          ) : (
            <div className="flex flex-col items-center gap-1.5 text-subtle">
              <Icon name="image" size={28} />
              <span className="text-xs">暂无预览图</span>
            </div>
          )}
        </div>

        {/* Footer Controls */}
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 pt-0.5">
          <div className="flex flex-wrap items-center gap-2">
            {previewTiles.length > 1 ? (
              <TooltipGroup className="flex items-center gap-1.5">
                <Tooltip content="上一张" shortcut="←">
                  <IconButton
                    icon="chevron-left"
                    size={26}
                    variant="outline"
                    aria-label="上一张"
                    onClick={() => stepPreview(-1)}
                  />
                </Tooltip>
                <span className="tnum px-1 text-xs font-medium text-ink">
                  {`${currentIndex >= 0 ? currentIndex + 1 : 1} / ${previewTiles.length}`}
                </span>
                <Tooltip content="下一张" shortcut="→">
                  <IconButton
                    icon="chevron-right"
                    size={26}
                    variant="outline"
                    aria-label="下一张"
                    onClick={() => stepPreview(1)}
                  />
                </Tooltip>
              </TooltipGroup>
            ) : null}
            {currentTile?.platform || currentTile?.accountName || currentTile?.elapsed ? (
              <span data-testid="lightbox-meta" className="tnum text-xs text-muted">
                {[
                  currentTile.platform,
                  currentTile.accountName && currentTile.accountName !== currentTile.platform
                    ? currentTile.accountName
                    : "",
                  currentTile.elapsed,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
            ) : null}
            {currentTile?.qualityNote ? (
              <div
                data-testid="lightbox-quality-note"
                className="flex items-center gap-1.5"
              >
                <Badge text="质检提醒" variant="warn" />
                <span className="select-text text-xs text-status-warn">
                  {currentTile.qualityNote}
                </span>
              </div>
            ) : null}
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            {currentTile?.canRewash ? (
              <Button
                data-testid="lightbox-rewash-btn"
                text="放回待洗（二次清洗）"
                size="small"
                icon="rotate-ccw"
                variant="outline"
                onClick={() => {
                  void requeueWorkbenchDone([currentTile.taskIndex]);
                  closePreview();
                }}
              />
            ) : null}
            {currentTile?.canReplace ? (
              <Button
                data-testid="lightbox-replace-btn"
                text="替换原图"
                size="small"
                icon="replace"
                variant="outline"
                onClick={() => fireIpc(Workbench.replaceResultImage(currentTile.taskIndex))}
              />
            ) : null}
            {currentTile?.actionText ? (
              <Button
                text={currentTile.actionText}
                size="small"
                icon="refresh-cw"
                variant="outline"
                onClick={() => fireIpc(Workbench.retryTask(currentTile.taskIndex))}
              />
            ) : null}
            {wb.previewPath ? (
              <Button
                text="复制路径"
                size="small"
                icon="copy"
                variant="ghost"
                onClick={() => {
                  fireIpc(Core.copyText(wb.previewPath));
                  showToast("已复制图片路径", "success");
                }}
              />
            ) : null}
            <Button
              text="在文件夹中显示"
              size="small"
              icon="folder-open"
              variant="outline"
              onClick={() => {
                if (wb.previewPath) fireIpc(Core.revealPath(wb.previewPath));
              }}
            />
            <Button
              text="系统默认打开"
              size="small"
              icon="external-link"
              variant="outline"
              onClick={() => {
                if (wb.previewPath) fireIpc(Core.openPath(wb.previewPath));
              }}
            />
            <Button text="关闭" size="small" variant="primary" onClick={closePreview} />
          </div>
        </div>
      </Dialog>
    </>
  );
}
