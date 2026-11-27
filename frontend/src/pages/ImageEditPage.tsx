import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { clearHoverPreview } from "@/components/HoverPreview";
import {
  ImageEditBatchGrid,
  ImageEditFilmstrip,
  type BatchPickStatus,
  type BatchQueueFilter,
} from "@/components/imageedit/ImageEditBatchQueue";
import {
  ImageEditRecipeDialog,
  ImageEditResultDialog,
} from "@/components/imageedit/ImageEditDialogs";
import {
  ImageEditInspector,
  type AdjustSliderField,
  type ClearCustomCropsScope,
  type CropSyncScope,
  type EditSectionId,
} from "@/components/imageedit/ImageEditInspector";
import {
  ImageEditStage,
  clampSplit,
  clampZoom,
  type LiveAdjustValues,
} from "@/components/imageedit/ImageEditStage";
import { PageSkeleton, PageToolbar } from "@/components/primitives";
import {
  Badge,
  Button,
  DropZoneOverlay,
  Icon,
  IconButton,
  Progress,
  Slider,
  Switch,
  Tooltip,
  TooltipGroup,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { ImageEdit, fireIpc, isMyGoRuntime, useFileDrop } from "@/lib/ipc";
import { isEditableTarget, useEscapeHandler } from "@/lib/shortcuts";
import {
  refreshImageEdit,
  useImageEdit,
  usePageLoading,
  type ImageEditStateDTO,
} from "@/store";

type EditViewMode = "darkroom" | "grid";

const EMPTY_IMAGE_EDIT: ImageEditStateDTO = {
  files: [],
  selected: -1,
  pickedCount: 0,
  customCropCount: 0,
  hasSelection: false,
  selectedName: "",
  srcW: 0,
  srcH: 0,
  outW: 0,
  outH: 0,
  origVersion: 0,
  outVersion: 0,
  origUrl: "",
  previewUrl: "",
  showCropUi: false,
  cropAspect: 0,
  previewPending: false,
  zoom: 1,
  compare: false,
  split: 50,
  formatEnabled: false,
  cropEnabled: false,
  adjustEnabled: false,
  formatOpen: false,
  cropOpen: false,
  adjustOpen: false,
  format: "original",
  quality: "",
  uniformCrop: true,
  mode: "crop",
  dimTab: 0,
  ratioTab: 0,
  dimW: "",
  dimH: "",
  cropRatio: "",
  rotate: 0,
  flipH: false,
  flipV: false,
  brightness: 0,
  contrast: 0,
  saturation: 0,
  blur: 0,
  sharpen: false,
  grayscale: false,
  outDir: "",
  suffix: "",
  notice: "",
  running: false,
  progressDone: 0,
  progressTotal: 0,
  progressCurrent: "",
  okCount: 0,
  failCount: 0,
  resultOpen: false,
  recipeOpen: false,
  recipeErr: "",
  chips: [],
  summaryFormat: "转格式 / 压缩品质",
  summaryCrop: "裁剪 / 改尺寸 / 旋转",
  summaryAdjust: "亮度 / 对比 / 饱和 / 锐化",
  sizePresets: [],
  ratioPresets: [],
  myRatios: [],
  mySizes: [],
  recipes: [],
};

const ADJUST_FIELDS: readonly AdjustSliderField[] = [
  "brightness",
  "contrast",
  "saturation",
  "blur",
];

function isSliderOrSpinTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  return target.closest("[role='slider'], [role='spinbutton']") !== null;
}

export function ImageEditPage() {
  const rawState = useImageEdit();
  const isLoading = usePageLoading("imageEdit");
  const st = rawState ?? EMPTY_IMAGE_EDIT;

  const [viewMode, setViewMode] = useState<EditViewMode>("darkroom");
  const [searchQuery, setSearchQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<BatchQueueFilter>("all");
  const [filmstripCompact, setFilmstripCompact] = useState(false);

  // Local 60fps zoom & split state synced with Go snapshot
  const [localZoom, setLocalZoom] = useState<number>(() => clampZoom(st.zoom || 1));
  const [localSplit, setLocalSplit] = useState<number | null>(null);

  // Local 60fps color adjustment state while dragging sliders
  const [liveAdjust, setLiveAdjust] = useState<LiveAdjustValues>({
    brightness: st.brightness,
    contrast: st.contrast,
    saturation: st.saturation,
    blur: st.blur,
    grayscale: Boolean(st.grayscale),
  });
  const [isLiveAdjusting, setIsLiveAdjusting] = useState(false);

  const sliderTimersRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const splitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingAdjustFieldsRef = useRef<Set<string>>(new Set());

  const files = st.files ?? [];
  const chips = st.chips ?? [];
  const recipes = st.recipes ?? [];
  const selectedFile =
    st.selected >= 0 && st.selected < files.length ? files[st.selected] : undefined;
  const selectedKey = selectedFile?.path || `${st.selected}:${st.origUrl}`;

  // Return to darkroom and clear filters when the queue becomes empty
  useEffect(() => {
    if (files.length === 0) {
      setViewMode((prev) => (prev === "grid" ? "darkroom" : prev));
      setSearchQuery("");
      setStatusFilter("all");
    }
  }, [files.length]);

  // Cleanup pending throttle timers on unmount
  useEffect(() => {
    return () => {
      for (const t of Object.values(sliderTimersRef.current)) {
        clearTimeout(t);
      }
      if (splitTimerRef.current !== null) {
        clearTimeout(splitTimerRef.current);
      }
    };
  }, []);

  // Sync zoom from server snapshot when selection or server zoom changes
  useEffect(() => {
    setLocalZoom(clampZoom(st.zoom || 1));
  }, [st.zoom, selectedKey]);

  // Sync split from server snapshot when idle
  useEffect(() => {
    if (splitTimerRef.current === null) {
      setLocalSplit(null);
    }
  }, [st.split]);

  // Sync color slider state from server snapshot when not actively dragging
  useEffect(() => {
    if (isLiveAdjusting) return;
    setLiveAdjust((prev) => ({
      brightness: pendingAdjustFieldsRef.current.has("brightness")
        ? prev.brightness
        : st.brightness,
      contrast: pendingAdjustFieldsRef.current.has("contrast")
        ? prev.contrast
        : st.contrast,
      saturation: pendingAdjustFieldsRef.current.has("saturation")
        ? prev.saturation
        : st.saturation,
      blur: pendingAdjustFieldsRef.current.has("blur") ? prev.blur : st.blur,
      grayscale: Boolean(st.grayscale),
    }));
  }, [
    isLiveAdjusting,
    st.brightness,
    st.contrast,
    st.saturation,
    st.blur,
    st.grayscale,
  ]);

  // Clear pending slider field markers once Go echoes the committed values
  useEffect(() => {
    if (
      pendingAdjustFieldsRef.current.has("brightness") &&
      st.brightness === liveAdjust.brightness
    ) {
      pendingAdjustFieldsRef.current.delete("brightness");
    }
    if (
      pendingAdjustFieldsRef.current.has("contrast") &&
      st.contrast === liveAdjust.contrast
    ) {
      pendingAdjustFieldsRef.current.delete("contrast");
    }
    if (
      pendingAdjustFieldsRef.current.has("saturation") &&
      st.saturation === liveAdjust.saturation
    ) {
      pendingAdjustFieldsRef.current.delete("saturation");
    }
    if (
      pendingAdjustFieldsRef.current.has("blur") &&
      Math.abs(st.blur - liveAdjust.blur) < 0.01
    ) {
      pendingAdjustFieldsRef.current.delete("blur");
    }
  }, [st.brightness, st.contrast, st.saturation, st.blur, liveAdjust]);

  const runAction = useCallback(async (fn: () => Promise<unknown>) => {
    try {
      await fn();
    } catch (err) {
      if (isMyGoRuntime()) {
        console.warn("[tulai/imageedit] action failed:", err);
      }
    } finally {
      void refreshImageEdit();
    }
  }, []);

  const clearAdjustTracking = useCallback(() => {
    for (const key of ADJUST_FIELDS) {
      const t = sliderTimersRef.current[key];
      if (t) {
        clearTimeout(t);
        delete sliderTimersRef.current[key];
      }
      pendingAdjustFieldsRef.current.delete(key);
    }
    setIsLiveAdjusting(false);
  }, []);

  // Full-page native file/folder drop
  const { isDragging } = useFileDrop({
    onDrop: (paths) => {
      if (paths.length > 0) {
        void runAction(() => ImageEdit.editAddPaths(paths));
      }
    },
  });

  // Dialog close handlers + priority Escape stack
  const closeRecipeDialog = useCallback(() => {
    void runAction(() => ImageEdit.editSetField("recipeOpen", "false"));
  }, [runAction]);

  const closeResultDialog = useCallback(() => {
    void runAction(() => ImageEdit.editSetField("resultOpen", "false"));
  }, [runAction]);

  const exitGridView = useCallback(() => {
    clearHoverPreview();
    setViewMode("darkroom");
  }, []);

  useEscapeHandler("imageedit-recipe-dialog", Boolean(st.recipeOpen), closeRecipeDialog, 40);
  useEscapeHandler("imageedit-result-dialog", Boolean(st.resultOpen), closeResultDialog, 40);
  useEscapeHandler(
    "imageedit-grid-mode",
    viewMode === "grid" && files.length > 0 && !st.recipeOpen && !st.resultOpen,
    exitGridView,
    10,
  );

  // Zoom controls
  const handleZoomStep = useCallback(
    (factor: number) => {
      setLocalZoom((prev) => clampZoom(prev * factor));
      void runAction(() => ImageEdit.editZoomStep(factor));
    },
    [runAction],
  );

  const handleResetView = useCallback(() => {
    setLocalZoom(1);
    void runAction(() => ImageEdit.editResetView());
  }, [runAction]);

  // Bidirectional split curtain handlers (normalized 5..95%)
  const effectiveSplit = clampSplit(localSplit ?? st.split ?? 50);

  const handleSplitLive = useCallback((val: number) => {
    const next = Math.round(clampSplit(val));
    setLocalSplit(next);
    if (splitTimerRef.current !== null) clearTimeout(splitTimerRef.current);
    splitTimerRef.current = setTimeout(() => {
      splitTimerRef.current = null;
      fireIpc(() => ImageEdit.editSetField("split", String(next)));
    }, 100);
  }, []);

  const handleSplitCommit = useCallback(
    (val: number) => {
      const next = Math.round(clampSplit(val));
      setLocalSplit(next);
      if (splitTimerRef.current !== null) {
        clearTimeout(splitTimerRef.current);
        splitTimerRef.current = null;
      }
      void runAction(() => ImageEdit.editSetField("split", String(next)));
    },
    [runAction],
  );

  // Color slider handlers: 60fps local preview + throttled Go commit
  const handleAdjustSliderLive = useCallback(
    (field: AdjustSliderField, rawValue: number) => {
      const val =
        field === "blur" ? Math.round(rawValue * 10) / 10 : Math.round(rawValue);
      setIsLiveAdjusting(true);
      pendingAdjustFieldsRef.current.add(field);
      setLiveAdjust((prev) => ({ ...prev, [field]: val }));

      const existing = sliderTimersRef.current[field];
      if (existing) clearTimeout(existing);
      sliderTimersRef.current[field] = setTimeout(() => {
        delete sliderTimersRef.current[field];
        fireIpc(() => ImageEdit.editSetField(field, String(val)));
      }, 120);
    },
    [],
  );

  const handleAdjustSliderCommit = useCallback(
    (field: AdjustSliderField, rawValue: number) => {
      const val =
        field === "blur" ? Math.round(rawValue * 10) / 10 : Math.round(rawValue);
      const existing = sliderTimersRef.current[field];
      if (existing) {
        clearTimeout(existing);
        delete sliderTimersRef.current[field];
      }
      pendingAdjustFieldsRef.current.add(field);
      setLiveAdjust((prev) => ({ ...prev, [field]: val }));
      setIsLiveAdjusting(false);
      void runAction(() => ImageEdit.editSetField(field, String(val)));
    },
    [runAction],
  );

  const handleResetSingleAdjust = useCallback(
    (field: AdjustSliderField) => {
      handleAdjustSliderCommit(field, 0);
    },
    [handleAdjustSliderCommit],
  );

  const handleResetAdjust = useCallback(() => {
    clearAdjustTracking();
    setLiveAdjust({
      brightness: 0,
      contrast: 0,
      saturation: 0,
      blur: 0,
      grayscale: false,
    });
    void runAction(async () => {
      await ImageEdit.editResetFeature("adjust");
      if (st.grayscale) {
        await ImageEdit.editSetField("grayscale", "false");
      }
    });
  }, [clearAdjustTracking, runAction, st.grayscale]);

  const handleClearAll = useCallback(() => {
    clearAdjustTracking();
    setLiveAdjust({
      brightness: 0,
      contrast: 0,
      saturation: 0,
      blur: 0,
      grayscale: false,
    });
    void runAction(() => ImageEdit.editClearAll());
  }, [clearAdjustTracking, runAction]);

  const handleCommitCropBox = useCallback(
    (x: number, y: number, w: number, h: number) => {
      void runAction(() => ImageEdit.editSetCropBox(x, y, w, h));
    },
    [runAction],
  );

  const handleClearCropBox = useCallback(() => {
    void runAction(() => ImageEdit.editClearCropBox());
  }, [runAction]);

  // Queue & batch pick IPC callbacks
  const handleSelectIndex = useCallback(
    (idx: number) => {
      void runAction(() => ImageEdit.editSelectIndex(idx));
    },
    [runAction],
  );

  const handleInspectFile = useCallback(
    (idx: number) => {
      void runAction(() => ImageEdit.editSelectIndex(idx));
      setViewMode("darkroom");
    },
    [runAction],
  );

  const handleTogglePick = useCallback(
    (idx: number, shift: boolean, cmd: boolean) => {
      void runAction(() => ImageEdit.editTogglePick(idx, shift, cmd));
    },
    [runAction],
  );

  const handleRemoveIndex = useCallback(
    (idx: number) => {
      void runAction(() => ImageEdit.editRemoveIndex(idx));
    },
    [runAction],
  );

  const handleSelectAllPicks = useCallback(() => {
    void runAction(() => ImageEdit.editSelectAllPicks());
  }, [runAction]);

  const handleInvertPicks = useCallback(() => {
    void runAction(() => ImageEdit.editInvertPicks());
  }, [runAction]);

  const handlePickByStatus = useCallback(
    (status: BatchPickStatus) => {
      void runAction(() => ImageEdit.editPickByStatus(status));
    },
    [runAction],
  );

  const handleClearPicks = useCallback(() => {
    void runAction(() => ImageEdit.editClearPicks());
  }, [runAction]);

  const handleRemovePicked = useCallback(() => {
    void runAction(() => ImageEdit.editRemovePicked());
  }, [runAction]);

  const handleApplyRecipe = useCallback(
    (idx: number) => {
      clearAdjustTracking();
      void runAction(() => ImageEdit.editApplyRecipe(idx));
    },
    [clearAdjustTracking, runAction],
  );

  const handlePickFailedFromResult = useCallback(() => {
    void runAction(() => ImageEdit.editPickByStatus("failed"));
    closeResultDialog();
  }, [closeResultDialog, runAction]);

  // Keyboard shortcuts (+/-/0 zoom/fit, C compare, [/] rotate, G grid toggle, P pick, arrows)
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing || e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) {
        return;
      }
      if (st.recipeOpen || st.resultOpen) return;
      if (isEditableTarget(e.target) || isSliderOrSpinTarget(e.target)) return;

      const lower = e.key.toLowerCase();

      if (lower === "g" && files.length > 0) {
        e.preventDefault();
        clearHoverPreview();
        setViewMode((prev) => (prev === "grid" ? "darkroom" : "grid"));
        return;
      }

      if (lower === "p" && st.hasSelection && st.selected >= 0 && st.selected < files.length) {
        e.preventDefault();
        handleTogglePick(st.selected, false, true);
        return;
      }

      if (viewMode === "darkroom") {
        if (e.key === "+" || e.key === "=") {
          e.preventDefault();
          handleZoomStep(1.25);
          return;
        }
        if (e.key === "-") {
          e.preventDefault();
          handleZoomStep(0.8);
          return;
        }
        if (e.key === "0") {
          e.preventDefault();
          handleResetView();
          return;
        }
        if (lower === "c" && st.hasSelection) {
          e.preventDefault();
          void runAction(() =>
            ImageEdit.editSetField("compare", st.compare ? "false" : "true"),
          );
          return;
        }
        if (e.key === "[" && st.hasSelection) {
          e.preventDefault();
          void runAction(() => ImageEdit.editSetField("rotateLeft", "1"));
          return;
        }
        if (e.key === "]" && st.hasSelection) {
          e.preventDefault();
          void runAction(() => ImageEdit.editSetField("rotateRight", "1"));
          return;
        }
        if (e.key === "ArrowLeft" && files.length > 1 && st.selected > 0) {
          e.preventDefault();
          handleSelectIndex(st.selected - 1);
          return;
        }
        if (e.key === "ArrowRight" && files.length > 1 && st.selected < files.length - 1) {
          e.preventDefault();
          handleSelectIndex(st.selected + 1);
          return;
        }
      }

      if (e.key === "ArrowUp" && files.length > 1 && st.selected > 0) {
        e.preventDefault();
        handleSelectIndex(st.selected - 1);
        return;
      }
      if (e.key === "ArrowDown" && files.length > 1 && st.selected < files.length - 1) {
        e.preventDefault();
        handleSelectIndex(st.selected + 1);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    viewMode,
    st.recipeOpen,
    st.resultOpen,
    st.hasSelection,
    st.compare,
    st.selected,
    files.length,
    handleZoomStep,
    handleResetView,
    handleTogglePick,
    handleSelectIndex,
    runAction,
  ]);

  const serverAdjust = useMemo<LiveAdjustValues>(
    () => ({
      brightness: st.brightness,
      contrast: st.contrast,
      saturation: st.saturation,
      blur: st.blur,
      grayscale: Boolean(st.grayscale),
    }),
    [st.brightness, st.contrast, st.saturation, st.blur, st.grayscale],
  );

  const hasGeometryTransform = Boolean(
    st.cropEnabled &&
      (st.rotate !== 0 ||
        st.flipH ||
        st.flipV ||
        Boolean(st.cropRatio) ||
        Boolean(st.cropBox) ||
        Number(st.dimW) > 0 ||
        Number(st.dimH) > 0),
  );

  if (rawState === null && isLoading) {
    return <PageSkeleton rows={6} />;
  }

  const compactFilm = filmstripCompact;
  const exportTargetCount = st.pickedCount > 0 ? st.pickedCount : files.length;
  const failedFiles = files.filter((f) => f.status === "failed");
  const isGridMode = viewMode === "grid" && files.length > 0;

  return (
    <div
      data-testid="imageedit-page"
      className="relative flex h-full min-h-0 flex-1 flex-col bg-paper"
    >
      <DropZoneOverlay
        active={isDragging}
        label="松开以导入待精修图片"
        sublabel="支持拖入 JPG / PNG / WebP / BMP / AVIF 图片或文件夹"
      />

      {/* 顶部悬浮操作栏：选图 / 视图切换 / 配方 / 批量导出 */}
      <PageToolbar
        left={
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              text="添加图片"
              size="small"
              icon="plus"
              variant="outline"
              onClick={() => void runAction(() => ImageEdit.editPickFiles())}
            />
            <Button
              text="添加目录"
              size="small"
              icon="folder"
              variant="outline"
              onClick={() => void runAction(() => ImageEdit.editPickFolder())}
            />
            <Button
              text="从生图导入"
              size="small"
              icon="folder-input"
              variant="outline"
              onClick={() => void runAction(() => ImageEdit.editAddFromSources())}
            />
            <Button
              text="精修配方"
              size="small"
              icon="save"
              variant="outline"
              onClick={() =>
                void runAction(() => ImageEdit.editSetField("recipeOpen", "true"))
              }
            />

            <TooltipGroup
              role="group"
              aria-label="视图模式"
              className="flex items-center rounded-[9px] border border-hairline bg-sunken p-0.5"
            >
              <Tooltip content="精修画布" shortcut="G" side="bottom">
                <button
                  type="button"
                  data-testid="edit-mode-darkroom"
                  aria-pressed={!isGridMode}
                  disabled={files.length === 0}
                  onClick={exitGridView}
                  className={cn(
                    "inline-flex h-6 cursor-pointer items-center gap-1 rounded-[7px] px-2 text-xs font-medium transition-colors select-none",
                    "disabled:pointer-events-none disabled:opacity-45",
                    !isGridMode
                      ? "bg-card text-ink shadow-xs"
                      : "text-muted hover:text-ink",
                  )}
                >
                  <Icon name="sliders-horizontal" size={12} />
                  <span>精修画布</span>
                </button>
              </Tooltip>
              <Tooltip content="网格批览" shortcut="G" side="bottom">
                <button
                  type="button"
                  data-testid="edit-mode-grid"
                  aria-pressed={isGridMode}
                  disabled={files.length === 0}
                  onClick={() => {
                    clearHoverPreview();
                    setViewMode("grid");
                  }}
                  className={cn(
                    "inline-flex h-6 cursor-pointer items-center gap-1 rounded-[7px] px-2 text-xs font-medium transition-colors select-none",
                    "disabled:pointer-events-none disabled:opacity-45",
                    isGridMode
                      ? "bg-card text-ink shadow-xs"
                      : "text-muted hover:text-ink",
                  )}
                >
                  <Icon name="layout-grid" size={12} />
                  <span>网格批览</span>
                </button>
              </Tooltip>
            </TooltipGroup>

            {files.length > 0 ? (
              <Button
                text="清空"
                size="small"
                variant="ghost"
                disabled={Boolean(st.running)}
                onClick={() => void runAction(() => ImageEdit.editClearFiles())}
              />
            ) : null}
          </div>
        }
        right={
          <div className="flex flex-wrap items-center gap-2">
            {st.pickedCount > 0 ? (
              <Badge text={`仅导出已选 ${st.pickedCount} 张`} variant="accent" dot />
            ) : null}
            {st.failCount > 0 ? (
              <Button
                text={`重试失败 (${st.failCount})`}
                size="small"
                variant="outline"
                disabled={Boolean(st.running)}
                onClick={() => void runAction(() => ImageEdit.editRetryFailed())}
                className="tnum"
              />
            ) : null}
            {st.running ? (
              <Button
                text="停止导出"
                size="small"
                icon="square"
                variant="secondary"
                onClick={() => void runAction(() => ImageEdit.editCancelBatch())}
              />
            ) : (
              <Button
                text={`批量导出 (${exportTargetCount})`}
                size="small"
                icon="play"
                variant="primary"
                disabled={files.length === 0}
                onClick={() => void runAction(() => ImageEdit.editRunBatch())}
                className="tnum"
              />
            )}
          </div>
        }
        bottom={
          st.running || st.notice || chips.length > 0 ? (
            <div
              data-testid="edit-status-bar"
              className="bento-shadow flex flex-wrap items-center justify-between gap-2 rounded-[14px] border border-hairline bg-card px-3.5 py-2"
            >
              <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
                {chips.map((chip) => (
                  <Badge
                    key={chip.id || chip.label}
                    text={chip.label || ""}
                    variant="accent"
                  />
                ))}
                {chips.length > 0 ? (
                  <Button
                    text="重置参数"
                    size="xs"
                    variant="ghost"
                    onClick={handleClearAll}
                  />
                ) : null}
                {st.notice ? (
                  <span className="truncate text-xs text-muted">{st.notice}</span>
                ) : null}
              </div>

              {st.running ? (
                <div className="flex w-[240px] shrink-0 flex-col gap-1">
                  <div className="tnum truncate text-[11px] text-muted">
                    {`处理中 ${st.progressDone}/${st.progressTotal} ${st.progressCurrent || ""}`}
                  </div>
                  <Progress
                    value={st.progressTotal > 0 ? st.progressDone / st.progressTotal : 0}
                  />
                </div>
              ) : null}
            </div>
          ) : null
        }
      />

      {/* 批量精修工作区：上方左侧命令面板 + 右侧精修画布或网格批览 / 底部全宽图片左右轮播 */}
      <div className="flex min-h-0 flex-1 flex-col gap-3 px-3 pb-3">
        <div className="flex min-h-0 min-w-0 flex-1 flex-row gap-3">
          <ImageEditInspector
          st={st}
          liveAdjust={liveAdjust}
          onSetField={(field, value) =>
            void runAction(() => ImageEdit.editSetField(field, value))
          }
          onSetFeature={(id: EditSectionId, checked: boolean) =>
            void runAction(() => ImageEdit.editSetFeature(id, checked))
          }
          onToggleExpanded={(id: EditSectionId) =>
            void runAction(() => ImageEdit.editToggleExpanded(id))
          }
          onResetFeature={(id: EditSectionId) => {
            if (id === "adjust") {
              handleResetAdjust();
              return;
            }
            void runAction(() => ImageEdit.editResetFeature(id));
          }}
          onClearAll={handleClearAll}
          onClearPicks={handleClearPicks}
          onApplyDims={(mode, w, h, ratio) =>
            void runAction(() => ImageEdit.editApplyDims(mode, w, h, ratio))
          }
          onPickRatio={(ratio) => void runAction(() => ImageEdit.editPickRatio(ratio))}
          onSaveMyRatio={() => void runAction(() => ImageEdit.editSaveMyRatio())}
          onSaveMySize={() => void runAction(() => ImageEdit.editSaveMySize())}
          onClearCropBox={handleClearCropBox}
          onSyncCropTo={(scope: CropSyncScope) =>
            void runAction(() => ImageEdit.editSyncCropTo(scope))
          }
          onClearCustomCrops={(scope: ClearCustomCropsScope) =>
            void runAction(() => ImageEdit.editClearCustomCrops(scope))
          }
          onAdjustLive={handleAdjustSliderLive}
          onAdjustCommit={handleAdjustSliderCommit}
          onResetSingleAdjust={handleResetSingleAdjust}
          onGrayscaleChange={(checked) => {
            setLiveAdjust((prev) => ({ ...prev, grayscale: checked }));
            void runAction(() =>
              ImageEdit.editSetField("grayscale", checked ? "true" : "false"),
            );
          }}
          onResetAdjust={handleResetAdjust}
          onPickOutDir={() => void runAction(() => ImageEdit.editPickOutDir())}
          onOpenOutDir={() => fireIpc(() => ImageEdit.editOpenOutDir())}
          onApplyRecipe={handleApplyRecipe}
          onOpenRecipeDialog={() =>
            void runAction(() => ImageEdit.editSetField("recipeOpen", "true"))
          }
        />

        <div
          data-testid="edit-workspace-main"
          className="flex min-h-0 min-w-0 flex-1 flex-col"
        >
          {isGridMode ? (
          <div key="grid" className="animate-tulai-panel-in flex min-h-0 min-w-0 flex-1 flex-col">
            <ImageEditBatchGrid
              files={files}
              selectedIndex={st.selected}
              pickedCount={st.pickedCount}
              failCount={st.failCount}
              customCropCount={st.customCropCount}
              running={Boolean(st.running)}
              onSelectIndex={handleSelectIndex}
              onInspectFile={handleInspectFile}
              onTogglePick={handleTogglePick}
              onRemoveIndex={handleRemoveIndex}
              onSelectAllPicks={handleSelectAllPicks}
              onInvertPicks={handleInvertPicks}
              onPickByStatus={handlePickByStatus}
              onClearPicks={handleClearPicks}
              onRemovePicked={handleRemovePicked}
              onExitGrid={exitGridView}
              onPickFiles={() => void runAction(() => ImageEdit.editPickFiles())}
              searchQuery={searchQuery}
              onSearchQueryChange={setSearchQuery}
              statusFilter={statusFilter}
              onStatusFilterChange={setStatusFilter}
            />
          </div>
        ) : (
          <section
            key="darkroom"
            data-testid="edit-canvas"
            className="animate-tulai-panel-in bento-shadow flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-[16px] border border-[#1E293B] bg-[#0F172A]"
          >
            {/* Darkroom Toolbar */}
            <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-[#1E293B] bg-[#131924] px-3.5 py-2">
              <TooltipGroup className="flex flex-wrap items-center gap-1.5">
                <Tooltip content="缩小" shortcut="-" side="bottom">
                  <IconButton
                    icon="zoom-out"
                    size={24}
                    variant="ghost"
                    aria-label="缩小"
                    disabled={!st.hasSelection}
                    onClick={() => handleZoomStep(0.8)}
                    className="text-slate-300 hover:bg-slate-800 hover:text-white"
                  />
                </Tooltip>
                <span
                  data-testid="edit-zoom-label"
                  className="tnum w-[44px] text-center text-xs font-medium text-slate-200"
                >
                  {`${Math.round(localZoom * 100)}%`}
                </span>
                <Tooltip content="放大" shortcut="+" side="bottom">
                  <IconButton
                    icon="zoom-in"
                    size={24}
                    variant="ghost"
                    aria-label="放大"
                    disabled={!st.hasSelection}
                    onClick={() => handleZoomStep(1.25)}
                    className="text-slate-300 hover:bg-slate-800 hover:text-white"
                  />
                </Tooltip>
                <Button
                  text="适应"
                  size="xs"
                  variant="ghost"
                  disabled={!st.hasSelection}
                  onClick={handleResetView}
                  className="text-slate-300 hover:bg-slate-800 hover:text-white"
                />

                {st.hasSelection ? (
                  <span
                    data-testid="edit-dims-pill"
                    className="tnum rounded-full border border-slate-700/70 bg-[#1E293B] px-2.5 py-0.5 text-[11px] text-slate-300"
                  >
                    {`${st.srcW}×${st.srcH} → ${st.outW || st.srcW}×${st.outH || st.srcH}`}
                  </span>
                ) : null}
              </TooltipGroup>

              <div className="flex items-center gap-2.5">
                <div className="flex items-center gap-1.5">
                  <Switch
                    size="small"
                    data-testid="edit-compare-switch"
                    aria-label="原图对比"
                    disabled={!st.hasSelection}
                    checked={Boolean(st.compare)}
                    onCheckedChange={(checked) =>
                      void runAction(() =>
                        ImageEdit.editSetField("compare", checked ? "true" : "false"),
                      )
                    }
                  />
                  <span className="text-xs text-slate-200 select-none">原图对比</span>
                </div>

                {st.compare ? (
                  <div className="animate-tulai-banner-in flex items-center gap-1.5">
                    <div className="w-[96px]">
                      <Slider
                        data-testid="edit-split-slider"
                        aria-label="原图对比比例"
                        min={5}
                        max={95}
                        step={1}
                        value={effectiveSplit}
                        onValueChange={handleSplitLive}
                        onValueCommit={handleSplitCommit}
                      />
                    </div>
                    <span
                      data-testid="edit-split-value"
                      className="tnum w-[30px] text-right text-[11px] text-slate-400"
                    >
                      {`${Math.round(effectiveSplit)}%`}
                    </span>
                  </div>
                ) : null}

                {files.length > 0 ? (
                  <Tooltip content="网格批览" shortcut="G" side="bottom">
                    <Button
                      text="网格批览"
                      size="xs"
                      icon="layout-grid"
                      variant="ghost"
                      data-testid="edit-canvas-grid-btn"
                      aria-label="进入网格批览"
                      onClick={() => {
                        clearHoverPreview();
                        setViewMode("grid");
                      }}
                      className="text-slate-300 hover:bg-slate-800 hover:text-white"
                    />
                  </Tooltip>
                ) : null}
              </div>
            </div>

            <ImageEditStage
              hasSelection={Boolean(st.hasSelection)}
              selectedKey={selectedKey}
              srcW={st.srcW}
              srcH={st.srcH}
              outW={st.outW}
              outH={st.outH}
              origUrl={st.origUrl}
              origVersion={st.origVersion}
              previewUrl={st.previewUrl}
              outVersion={st.outVersion}
              previewPending={Boolean(st.previewPending)}
              zoom={localZoom}
              compare={Boolean(st.compare)}
              split={effectiveSplit}
              showCropUi={Boolean(st.showCropUi)}
              cropAspect={st.cropAspect}
              cropRatio={st.cropRatio}
              cropBox={st.cropBox}
              hasGeometryTransform={hasGeometryTransform}
              adjustEnabled={Boolean(st.adjustEnabled)}
              serverAdjust={serverAdjust}
              liveAdjust={liveAdjust}
              isLiveAdjusting={isLiveAdjusting}
              onZoomChange={setLocalZoom}
              onResetView={handleResetView}
              onSplitChange={handleSplitLive}
              onSplitCommit={handleSplitCommit}
              onCommitCropBox={handleCommitCropBox}
              onClearCropBox={handleClearCropBox}
              onPickFiles={() => void runAction(() => ImageEdit.editPickFiles())}
              fileName={selectedFile?.name || st.selectedName || undefined}
              fileIndex={st.selected >= 0 ? st.selected : undefined}
              totalFiles={files.length}
              isPicked={Boolean(selectedFile?.picked)}
              hasCustomCrop={Boolean(selectedFile?.hasCustomCrop)}
              uniformCrop={Boolean(st.uniformCrop)}
              onTogglePickCurrent={
                st.hasSelection && st.selected >= 0 && st.selected < files.length
                  ? () => handleTogglePick(st.selected, false, true)
                  : undefined
              }
              onPrevFile={
                files.length > 1
                  ? () => {
                      if (st.selected > 0) handleSelectIndex(st.selected - 1);
                    }
                  : undefined
              }
              onNextFile={
                files.length > 1
                  ? () => {
                      if (st.selected < files.length - 1) handleSelectIndex(st.selected + 1);
                    }
                  : undefined
              }
            />
          </section>
        )}

        </div>
        </div>

        <ImageEditFilmstrip
          compact={compactFilm}
          onToggleCompact={() => setFilmstripCompact((v) => !v)}
          viewMode={isGridMode ? "grid" : "darkroom"}
          onViewModeChange={(mode) => {
            clearHoverPreview();
            setViewMode(files.length === 0 ? "darkroom" : mode);
          }}
          files={files}
          selectedIndex={st.selected}
          pickedCount={st.pickedCount}
          failCount={st.failCount}
          customCropCount={st.customCropCount}
          running={Boolean(st.running)}
          onSelectIndex={handleSelectIndex}
          onTogglePick={handleTogglePick}
          onRemoveIndex={handleRemoveIndex}
          onSelectAllPicks={handleSelectAllPicks}
          onInvertPicks={handleInvertPicks}
          onPickByStatus={handlePickByStatus}
          onClearPicks={handleClearPicks}
          onRemovePicked={handleRemovePicked}
          searchQuery={searchQuery}
          onSearchQueryChange={setSearchQuery}
          statusFilter={statusFilter}
          onStatusFilterChange={setStatusFilter}
        />
      </div>

      <ImageEditRecipeDialog
        open={Boolean(st.recipeOpen)}
        recipes={recipes}
        recipeErr={st.recipeErr}
        onClose={closeRecipeDialog}
        onSaveRecipe={(name) => void runAction(() => ImageEdit.editSaveRecipe(name))}
        onApplyRecipe={handleApplyRecipe}
        onDeleteRecipe={(idx) => void runAction(() => ImageEdit.editDeleteRecipe(idx))}
      />

      <ImageEditResultDialog
        open={Boolean(st.resultOpen)}
        okCount={st.okCount}
        failCount={st.failCount}
        outDir={st.outDir}
        failedFiles={failedFiles}
        running={Boolean(st.running)}
        onClose={closeResultDialog}
        onRetryFailed={() => void runAction(() => ImageEdit.editRetryFailed())}
        onPickFailed={handlePickFailedFromResult}
        onOpenOutDir={() => fireIpc(() => ImageEdit.editOpenOutDir())}
      />
    </div>
  );
}
