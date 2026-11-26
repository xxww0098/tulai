import { useCallback, useMemo, useRef, useState } from "react";
import type { LiveAdjustValues } from "@/components/imageedit/ImageEditStage";
import { BentoCard, InsetPillRow } from "@/components/primitives";
import {
  Badge,
  Button,
  Checkbox,
  Icon,
  IconButton,
  Input,
  Select,
  Slider,
  Switch,
  Tooltip,
  TooltipGroup,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import type { EditFileDTO, ImageEditStateDTO } from "@/store";

export type BatchImageEditState = ImageEditStateDTO & {
  customCropCount?: number;
};

export type AdjustSliderField = "brightness" | "contrast" | "saturation" | "blur";
export type EditSectionId = "crop" | "adjust" | "format";
export type CropSyncScope = "picked" | "all";
export type ClearCustomCropsScope = "selected" | "picked" | "all";

export interface ImageEditInspectorProps {
  st: BatchImageEditState;
  liveAdjust: LiveAdjustValues;
  onSetField: (field: string, value: string) => void;
  onSetFeature: (id: EditSectionId, checked: boolean) => void;
  onToggleExpanded: (id: EditSectionId) => void;
  onResetFeature: (id: EditSectionId) => void;
  onClearAll: () => void;
  onClearPicks: () => void;
  onApplyDims: (mode: string, w: number, h: number, ratio: string) => void;
  onPickRatio: (ratio: string) => void;
  onSaveMyRatio: () => void;
  onSaveMySize: () => void;
  onClearCropBox: () => void;
  onSyncCropTo: (scope: CropSyncScope) => void;
  onClearCustomCrops: (scope: ClearCustomCropsScope) => void;
  onAdjustLive: (field: AdjustSliderField, value: number) => void;
  onAdjustCommit: (field: AdjustSliderField, value: number) => void;
  onResetSingleAdjust?: (field: AdjustSliderField) => void;
  onGrayscaleChange: (checked: boolean) => void;
  onResetAdjust: () => void;
  onPickOutDir: () => void;
  onOpenOutDir: () => void;
  onApplyRecipe: (idx: number) => void;
  onOpenRecipeDialog: () => void;
}

const FORMAT_OPTIONS = [
  { value: "original", label: "保持原格式" },
  { value: "jpg", label: "JPEG (.jpg)" },
  { value: "png", label: "PNG (.png)" },
  { value: "webp", label: "WebP (.webp)" },
];

const CROP_MODE_OPTIONS = [
  { value: "crop", label: "裁剪" },
  { value: "fit", label: "等比缩放" },
  { value: "pad", label: "留白补满" },
  { value: "stretch", label: "拉伸" },
];

interface ToneSliderSpec {
  field: AdjustSliderField;
  label: string;
  testId: string;
  min: number;
  max: number;
  step: number;
}

const TONE_SLIDERS: ToneSliderSpec[] = [
  { field: "brightness", label: "亮度", testId: "edit-slider-brightness", min: -100, max: 100, step: 1 },
  { field: "contrast", label: "对比度", testId: "edit-slider-contrast", min: -100, max: 100, step: 1 },
  { field: "saturation", label: "饱和度", testId: "edit-slider-saturation", min: -100, max: 100, step: 1 },
  { field: "blur", label: "模糊", testId: "edit-slider-blur", min: 0, max: 20, step: 0.5 },
];

function formatToneValue(field: AdjustSliderField, val: number): string {
  if (field === "blur") return val.toFixed(1);
  return val > 0 ? `+${val}` : String(val);
}

export function ImageEditInspector({
  st,
  liveAdjust,
  onSetField,
  onSetFeature,
  onToggleExpanded,
  onResetFeature,
  onClearAll,
  onClearPicks,
  onApplyDims,
  onPickRatio,
  onSaveMyRatio,
  onSaveMySize,
  onClearCropBox,
  onSyncCropTo,
  onClearCustomCrops,
  onAdjustLive,
  onAdjustCommit,
  onResetSingleAdjust,
  onGrayscaleChange,
  onResetAdjust,
  onPickOutDir,
  onOpenOutDir,
  onApplyRecipe,
  onOpenRecipeDialog,
}: ImageEditInspectorProps) {
  const [dimSyncEpoch, setDimSyncEpoch] = useState(0);
  const [formSyncEpoch, setFormSyncEpoch] = useState(0);

  const cropCardRef = useRef<HTMLDivElement | null>(null);
  const adjustCardRef = useRef<HTMLDivElement | null>(null);
  const formatCardRef = useRef<HTMLDivElement | null>(null);

  const files = st.files ?? [];
  const chips = st.chips ?? [];
  const ratioPresets = st.ratioPresets ?? [];
  const sizePresets = st.sizePresets ?? [];
  const mySizes = st.mySizes ?? [];
  const myRatios = st.myRatios ?? [];
  const recipes = st.recipes ?? [];

  const customCropCount = useMemo(() => {
    if (typeof st.customCropCount === "number") return st.customCropCount;
    return files.filter((f) =>
      Boolean((f as EditFileDTO & { hasCustomCrop?: boolean }).hasCustomCrop),
    ).length;
  }, [st.customCropCount, files]);

  const dimWDraft = useSyncedDraft(
    st.dimW || "",
    `edit-dimW-${dimSyncEpoch}`,
    (val) => onSetField("dimW", val),
    { debounceMs: 140 },
  );

  const dimHDraft = useSyncedDraft(
    st.dimH || "",
    `edit-dimH-${dimSyncEpoch}`,
    (val) => onSetField("dimH", val),
    { debounceMs: 140 },
  );

  const qualityDraft = useSyncedDraft(
    st.quality || "",
    `edit-quality-${formSyncEpoch}`,
    (val) => onSetField("quality", val),
    { debounceMs: 140 },
  );

  const suffixDraft = useSyncedDraft(
    st.suffix || "",
    `edit-suffix-${formSyncEpoch}`,
    (val) => onSetField("suffix", val),
    { debounceMs: 140 },
  );

  const handleApplyDims = useCallback(() => {
    dimWDraft.flush();
    dimHDraft.flush();
    const w = Math.max(0, Math.round(Number(dimWDraft.value) || 0));
    const h = Math.max(0, Math.round(Number(dimHDraft.value) || 0));
    onApplyDims(st.mode || "crop", w, h, "");
  }, [dimWDraft, dimHDraft, onApplyDims, st.mode]);

  const sizePresetOptions = useMemo(
    () =>
      sizePresets.map((p, idx) => ({
        value: `${idx}:${p.w}:${p.h}`,
        label: p.label || `${p.w}×${p.h}`,
      })),
    [sizePresets],
  );

  const selectedSizePresetValue = useMemo(() => {
    const w = Number(dimWDraft.value) || 0;
    const h = Number(dimHDraft.value) || 0;
    if (w <= 0 || h <= 0) return "";
    const idx = sizePresets.findIndex((p) => p.w === w && p.h === h);
    return idx >= 0 ? `${idx}:${sizePresets[idx].w}:${sizePresets[idx].h}` : "";
  }, [dimWDraft.value, dimHDraft.value, sizePresets]);

  const extraMyRatios = useMemo(
    () => myRatios.filter((r) => !ratioPresets.some((p) => p.ratio === r)),
    [myRatios, ratioPresets],
  );

  const canSaveRatio = Boolean(
    st.cropRatio ||
      (Number(dimWDraft.value) > 0 && Number(dimHDraft.value) > 0) ||
      (Number(st.dimW) > 0 && Number(st.dimH) > 0),
  );

  const transformSummary = useMemo(() => {
    const parts: string[] = [];
    if (st.rotate) parts.push(`${st.rotate}°`);
    if (st.flipH) parts.push("水平翻转");
    if (st.flipV) parts.push("垂直翻转");
    return parts.join(" ");
  }, [st.rotate, st.flipH, st.flipV]);

  const jumpToSection = useCallback(
    (id: EditSectionId) => {
      const isOpen =
        id === "crop" ? st.cropOpen : id === "adjust" ? st.adjustOpen : st.formatOpen;
      if (!isOpen) {
        onToggleExpanded(id);
      }
      const target =
        id === "crop"
          ? cropCardRef.current
          : id === "adjust"
            ? adjustCardRef.current
            : formatCardRef.current;
      target?.scrollIntoView?.({ block: "nearest", behavior: "smooth" });
    },
    [st.cropOpen, st.adjustOpen, st.formatOpen, onToggleExpanded],
  );

  const handleSingleSliderReset = useCallback(
    (field: AdjustSliderField) => {
      if (onResetSingleAdjust) {
        onResetSingleAdjust(field);
        return;
      }
      onAdjustCommit(field, 0);
    },
    [onResetSingleAdjust, onAdjustCommit],
  );

  const sectionPills: Array<{
    id: EditSectionId;
    label: string;
    ariaLabel: string;
    enabled: boolean;
  }> = [
    { id: "crop", label: "裁剪", ariaLabel: "定位裁剪面板", enabled: Boolean(st.cropEnabled) },
    { id: "adjust", label: "调色", ariaLabel: "定位调色面板", enabled: Boolean(st.adjustEnabled) },
    { id: "format", label: "格式", ariaLabel: "定位格式面板", enabled: Boolean(st.formatEnabled) },
  ];

  const currentMode = st.mode || "crop";

  return (
    <aside
      data-testid="edit-inspector"
      style={{ width: "26%", minWidth: 272, maxWidth: 336 }}
      className="flex h-full min-h-0 shrink-0 flex-col"
    >
      <div className="scroll-gutter flex min-h-0 flex-1 flex-col gap-2.5 overflow-y-auto pr-0.5 pb-4">
        {/* Top Batch Scope & Quick Recipe Bar */}
        <BentoCard data-testid="edit-inspector-batch-bar" compact className="shrink-0 gap-2">
          <div className="flex items-center justify-between gap-1.5">
            <div className="flex min-w-0 items-center gap-1">
              {st.pickedCount > 0 ? (
                <>
                  <Badge variant="accent" dot text={`已多选 ${st.pickedCount} 张`} />
                  <Button
                    text="清空选区"
                    size="xs"
                    variant="ghost"
                    onClick={onClearPicks}
                  />
                </>
              ) : (
                <Badge variant="neutral" text={`全部共 ${files.length} 张`} />
              )}
            </div>
            {chips.length > 0 ? (
              <Button
                text="重置全部"
                size="xs"
                variant="ghost"
                onClick={() => {
                  setDimSyncEpoch((e) => e + 1);
                  setFormSyncEpoch((e) => e + 1);
                  onClearAll();
                }}
              />
            ) : null}
          </div>

          <TooltipGroup className="flex items-center gap-1">
            {sectionPills.map((pill) => (
              <Tooltip
                key={pill.id}
                content={`${pill.label} (${pill.enabled ? "已启用" : "未启用"} · 点击展开定位)`}
                side="top"
              >
                <button
                  type="button"
                  aria-label={pill.ariaLabel}
                  onClick={() => jumpToSection(pill.id)}
                  className={cn(
                    "inline-flex flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-[8px] border px-2 py-1 text-[11px] font-medium transition-colors duration-150 select-none",
                    pill.enabled
                      ? "border-accent-border bg-accent-soft text-ink hover:bg-accent-soft/80"
                      : "border-hairline bg-sunken text-muted hover:bg-sunken-hover hover:text-ink",
                  )}
                >
                  <span
                    aria-hidden="true"
                    className={cn(
                      "size-1.5 shrink-0 rounded-full",
                      pill.enabled ? "bg-accent" : "bg-subtle/50",
                    )}
                  />
                  <span>{pill.label}</span>
                </button>
              </Tooltip>
            ))}
          </TooltipGroup>

          <div className="flex items-center justify-between gap-1.5 border-t border-divider pt-2">
            {recipes.length > 0 ? (
              <TooltipGroup className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto py-0.5">
                {recipes.map((recipe) => (
                  <Tooltip
                    key={recipe.index}
                    content={`一键套用配方：${recipe.name}`}
                    side="top"
                  >
                    <Button
                      text={recipe.name}
                      size="xs"
                      variant="secondary"
                      onClick={() => {
                        setDimSyncEpoch((e) => e + 1);
                        setFormSyncEpoch((e) => e + 1);
                        onApplyRecipe(recipe.index);
                      }}
                      className="max-w-[116px]"
                    />
                  </Tooltip>
                ))}
              </TooltipGroup>
            ) : (
              <span className="truncate text-[11px] text-subtle">暂无快捷配方</span>
            )}
            <Button
              text="配方管理"
              size="xs"
              variant="outline"
              onClick={onOpenRecipeDialog}
            />
          </div>
        </BentoCard>

        {/* Card 1: 裁剪、尺寸与旋转 */}
        <div ref={cropCardRef} className="shrink-0">
          <BentoCard
            data-testid="edit-card-crop"
            title="裁剪、尺寸与旋转"
            subtitle={st.summaryCrop || undefined}
            icon="wand-sparkles"
            compact
            right={
              <div className="flex items-center gap-1">
                <IconButton
                  icon={
                    <Icon
                      name="chevron-down"
                      size={13}
                      className={cn(
                        "transition-transform duration-180 ease-[cubic-bezier(0.16,1,0.3,1)]",
                        st.cropOpen && "rotate-180",
                      )}
                    />
                  }
                  size={22}
                  variant="ghost"
                  aria-label="展开或收起裁剪面板"
                  onClick={() => onToggleExpanded("crop")}
                />
                <Switch
                  size="small"
                  data-testid="edit-crop-switch"
                  aria-label="启用裁剪、尺寸与旋转"
                  checked={Boolean(st.cropEnabled)}
                  onCheckedChange={(checked) => onSetFeature("crop", checked)}
                />
              </div>
            }
          >
            {st.cropOpen ? (
              <div className="animate-tulai-banner-in flex flex-col gap-2">
                {/* Compact Transform Row */}
                <InsetPillRow className="flex-row items-center justify-between gap-1 px-2.5 py-1.5">
                  <TooltipGroup className="flex items-center gap-1">
                    <Tooltip content="逆时针旋转 90°" shortcut="[" side="top">
                      <IconButton
                        icon="rotate-ccw"
                        size={26}
                        variant="outline"
                        aria-label="逆时针旋转"
                        onClick={() => onSetField("rotateLeft", "1")}
                      />
                    </Tooltip>
                    <Tooltip content="顺时针旋转 90°" shortcut="]" side="top">
                      <IconButton
                        icon="rotate-cw"
                        size={26}
                        variant="outline"
                        aria-label="顺时针旋转"
                        onClick={() => onSetField("rotateRight", "1")}
                      />
                    </Tooltip>
                    <Tooltip content="水平翻转" side="top">
                      <IconButton
                        icon="flip-horizontal"
                        size={26}
                        variant={st.flipH ? "primary" : "outline"}
                        aria-label="水平翻转"
                        onClick={() => onSetField("flipH", "1")}
                      />
                    </Tooltip>
                    <Tooltip content="垂直翻转" side="top">
                      <IconButton
                        icon="flip-vertical"
                        size={26}
                        variant={st.flipV ? "primary" : "outline"}
                        aria-label="垂直翻转"
                        onClick={() => onSetField("flipV", "1")}
                      />
                    </Tooltip>
                  </TooltipGroup>
                  <div className="flex items-center gap-1">
                    {transformSummary ? (
                      <Badge
                        variant="accent"
                        text={transformSummary}
                        className="px-1.5 py-0 text-[10px]"
                      />
                    ) : null}
                    <Button
                      text="重置"
                      size="xs"
                      variant="ghost"
                      onClick={() => {
                        setDimSyncEpoch((e) => e + 1);
                        onResetFeature("crop");
                      }}
                    />
                  </div>
                </InsetPillRow>

                {/* 4-Mode Segmented Control */}
                <div className="grid grid-cols-4 gap-1 rounded-[10px] bg-sunken p-1">
                  {CROP_MODE_OPTIONS.map((opt) => {
                    const active = currentMode === opt.value;
                    return (
                      <button
                        key={opt.value}
                        type="button"
                        aria-pressed={active}
                        onClick={() => onSetField("mode", opt.value)}
                        className={cn(
                          "cursor-pointer rounded-[7px] px-1.5 py-1 text-[11px] font-medium transition-colors select-none",
                          active
                            ? "bg-card text-ink shadow-xs"
                            : "text-muted hover:text-ink",
                        )}
                      >
                        {opt.label}
                      </button>
                    );
                  })}
                </div>

                {/* Compact 5-Col Ratio Grid + Custom Saved Ratios */}
                {ratioPresets.length > 0 || extraMyRatios.length > 0 || canSaveRatio ? (
                  <div className="flex flex-col gap-1">
                    {ratioPresets.length > 0 ? (
                      <TooltipGroup className="grid grid-cols-5 gap-1">
                        {ratioPresets.map((preset) => {
                          const active = st.cropRatio === preset.ratio;
                          const label = preset.label || preset.ratio;
                          return (
                            <Tooltip
                              key={preset.ratio}
                              content={
                                preset.label ? `${preset.label} (${preset.ratio})` : preset.ratio
                              }
                              side="top"
                            >
                              <button
                                type="button"
                                aria-label={label}
                                aria-pressed={active}
                                onClick={() => onPickRatio(preset.ratio || "")}
                                className={cn(
                                  "tnum flex h-7 cursor-pointer items-center justify-center rounded-[8px] border px-1 text-[11px] font-medium transition-colors duration-150 select-none",
                                  active
                                    ? "border-accent bg-accent font-semibold text-on-accent"
                                    : "border-hairline bg-sunken text-ink hover:bg-sunken-hover",
                                )}
                              >
                                <span className="truncate">{preset.ratio}</span>
                              </button>
                            </Tooltip>
                          );
                        })}
                      </TooltipGroup>
                    ) : null}

                    {extraMyRatios.length > 0 || canSaveRatio ? (
                      <div className="flex flex-wrap items-center gap-1">
                        {extraMyRatios.map((ratio) => (
                          <Button
                            key={`my-${ratio}`}
                            text={ratio}
                            size="xs"
                            variant={st.cropRatio === ratio ? "primary" : "outline"}
                            onClick={() => onPickRatio(ratio)}
                            className="tnum"
                          />
                        ))}
                        {canSaveRatio ? (
                          <Tooltip content="保存当前比例为常用快捷项" side="top">
                            <Button
                              text="存为常用"
                              size="xs"
                              variant="ghost"
                              onClick={onSaveMyRatio}
                              className="text-accent"
                            />
                          </Tooltip>
                        ) : null}
                      </div>
                    ) : null}
                  </div>
                ) : null}

                {/* Width × Height Inputs + Apply + Save My Size */}
                <div className="flex items-center gap-1.5">
                  <Input
                    size="small"
                    inputMode="numeric"
                    aria-label="目标宽度"
                    placeholder="宽"
                    value={dimWDraft.value}
                    onChange={(e) => dimWDraft.onChange(e.target.value)}
                    onCompositionStart={dimWDraft.onCompositionStart}
                    onCompositionEnd={(e) => dimWDraft.onCompositionEnd(e.currentTarget.value)}
                    onBlur={dimWDraft.onBlur}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && !e.nativeEvent.isComposing) handleApplyDims();
                    }}
                    className="tnum flex-1"
                  />
                  <span className="text-xs text-subtle">×</span>
                  <Input
                    size="small"
                    inputMode="numeric"
                    aria-label="目标高度"
                    placeholder="高"
                    value={dimHDraft.value}
                    onChange={(e) => dimHDraft.onChange(e.target.value)}
                    onCompositionStart={dimHDraft.onCompositionStart}
                    onCompositionEnd={(e) => dimHDraft.onCompositionEnd(e.currentTarget.value)}
                    onBlur={dimHDraft.onBlur}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && !e.nativeEvent.isComposing) handleApplyDims();
                    }}
                    className="tnum flex-1"
                  />
                  <Button
                    text="应用"
                    size="small"
                    variant="outline"
                    onClick={handleApplyDims}
                  />
                  <Tooltip content="保存为我的常用尺寸" side="top">
                    <IconButton
                      icon="save"
                      size={26}
                      variant="ghost"
                      aria-label="保存为我的常用尺寸"
                      onClick={onSaveMySize}
                    />
                  </Tooltip>
                </div>

                {/* Recommended Size Presets Select */}
                {sizePresetOptions.length > 0 ? (
                  <Select
                    size="small"
                    data-testid="edit-size-preset-select"
                    aria-label="推荐尺寸预设"
                    placeholder="套用推荐尺寸预设..."
                    value={selectedSizePresetValue}
                    options={sizePresetOptions}
                    onValueChange={(val) => {
                      const idx = Number(val.split(":")[0]);
                      const preset = sizePresets[idx];
                      if (!preset) return;
                      const w = preset.w > 0 ? preset.w : 0;
                      const h = preset.h > 0 ? preset.h : 0;
                      setDimSyncEpoch((e) => e + 1);
                      onApplyDims(currentMode, w, h, "");
                    }}
                  />
                ) : null}

                {/* Saved Custom Sizes Pills */}
                {mySizes.length > 0 ? (
                  <div className="flex flex-col gap-1">
                    <span className="text-[10.5px] font-medium text-subtle">我的常用尺寸</span>
                    <div className="flex flex-wrap gap-1">
                      {mySizes.map((sz, idx) => (
                        <Button
                          key={`${sz.w}x${sz.h}-${idx}`}
                          text={sz.label || `${sz.w}×${sz.h}`}
                          size="xs"
                          variant={
                            Number(st.dimW) === sz.w && Number(st.dimH) === sz.h
                              ? "primary"
                              : "secondary"
                          }
                          onClick={() => {
                            setDimSyncEpoch((e) => e + 1);
                            onApplyDims(currentMode, sz.w, sz.h, "");
                          }}
                          className="tnum"
                        />
                      ))}
                    </div>
                  </div>
                ) : null}

                {/* Batch Crop Scope & Sync Controls */}
                {currentMode === "crop" ? (
                  <div className="flex flex-col gap-1.5 pt-0.5">
                    <div className="flex items-center justify-between gap-1">
                      <Checkbox
                        size="small"
                        label="全部图片统一裁剪位置"
                        checked={Boolean(st.uniformCrop)}
                        onCheckedChange={(checked) =>
                          onSetField("uniformCrop", checked ? "true" : "false")
                        }
                      />
                      {st.cropBox || st.cropRatio ? (
                        <Button
                          text="清除裁剪"
                          size="xs"
                          variant="ghost"
                          onClick={onClearCropBox}
                        />
                      ) : null}
                    </div>

                    {!st.uniformCrop ? (
                      <InsetPillRow
                        data-testid="edit-crop-sync-box"
                        className="gap-1.5 px-2.5 py-2"
                      >
                        <div className="flex items-center justify-between gap-1.5">
                          <span className="text-[11px] font-medium text-ink">批量同步裁剪框</span>
                          <Badge
                            variant={customCropCount > 0 ? "accent" : "neutral"}
                            text={`自定义裁剪 ${customCropCount} 张`}
                            className="px-1.5 py-0 text-[10px]"
                          />
                        </div>
                        <div className="flex flex-wrap items-center gap-1">
                          <Button
                            text="同步到已选"
                            size="xs"
                            variant="outline"
                            disabled={st.pickedCount === 0}
                            onClick={() => onSyncCropTo("picked")}
                          />
                          <Button
                            text="同步到全部"
                            size="xs"
                            variant="outline"
                            disabled={files.length < 2}
                            onClick={() => onSyncCropTo("all")}
                          />
                          <Button
                            text="恢复统一裁剪"
                            size="xs"
                            variant="ghost"
                            disabled={customCropCount === 0}
                            onClick={() => onClearCustomCrops("all")}
                          />
                        </div>
                      </InsetPillRow>
                    ) : null}
                  </div>
                ) : null}
              </div>
            ) : null}
          </BentoCard>
        </div>

        {/* Card 2: 色彩与清晰度 */}
        <div ref={adjustCardRef} className="shrink-0">
          <BentoCard
            data-testid="edit-card-adjust"
            title="色彩与清晰度"
            subtitle={st.summaryAdjust || undefined}
            icon="sliders-horizontal"
            compact
            right={
              <div className="flex items-center gap-1">
                <IconButton
                  icon={
                    <Icon
                      name="chevron-down"
                      size={13}
                      className={cn(
                        "transition-transform duration-180 ease-[cubic-bezier(0.16,1,0.3,1)]",
                        st.adjustOpen && "rotate-180",
                      )}
                    />
                  }
                  size={22}
                  variant="ghost"
                  aria-label="展开或收起色彩面板"
                  onClick={() => onToggleExpanded("adjust")}
                />
                <Switch
                  size="small"
                  data-testid="edit-adjust-switch"
                  aria-label="启用色彩与清晰度"
                  checked={Boolean(st.adjustEnabled)}
                  onCheckedChange={(checked) => onSetFeature("adjust", checked)}
                />
              </div>
            }
          >
            {st.adjustOpen ? (
              <div className="animate-tulai-banner-in flex flex-col gap-2">
                <InsetPillRow className="p-2.5">
                  <div className="grid grid-cols-2 gap-x-3 gap-y-2">
                    {TONE_SLIDERS.map((item) => {
                      const val = liveAdjust[item.field];
                      const modified = Math.abs(val) > 0.001;
                      return (
                        <div key={item.field} className="flex flex-col gap-1">
                          <div className="flex items-center justify-between text-[11px] text-muted">
                            <Tooltip content="双击重置为 0" side="top">
                              <span
                                tabIndex={0}
                                onDoubleClick={() => handleSingleSliderReset(item.field)}
                                className="cursor-pointer rounded select-none hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                              >
                                {item.label}
                              </span>
                            </Tooltip>
                            <span
                              className={cn(
                                "tnum font-medium",
                                modified ? "text-accent" : "text-ink",
                              )}
                            >
                              {formatToneValue(item.field, val)}
                            </span>
                          </div>
                          <Slider
                            data-testid={item.testId}
                            aria-label={item.label}
                            min={item.min}
                            max={item.max}
                            step={item.step}
                            value={val}
                            onValueChange={(v) => onAdjustLive(item.field, v)}
                            onValueCommit={(v) => onAdjustCommit(item.field, v)}
                          />
                        </div>
                      );
                    })}
                  </div>
                </InsetPillRow>

                <div className="flex flex-wrap items-center justify-between gap-1.5 pt-0.5">
                  <Checkbox
                    size="small"
                    label="轻度锐化"
                    checked={Boolean(st.sharpen)}
                    onCheckedChange={(checked) =>
                      onSetField("sharpen", checked ? "true" : "false")
                    }
                  />
                  <Checkbox
                    size="small"
                    label="黑白灰度"
                    checked={Boolean(st.grayscale)}
                    onCheckedChange={onGrayscaleChange}
                  />
                  <Button
                    text="重置"
                    size="xs"
                    variant="ghost"
                    onClick={onResetAdjust}
                  />
                </div>
              </div>
            ) : null}
          </BentoCard>
        </div>

        {/* Card 3: 输出格式与目录 */}
        <div ref={formatCardRef} className="shrink-0">
          <BentoCard
            data-testid="edit-card-format"
            title="输出格式与目录"
            subtitle={st.summaryFormat || undefined}
            icon="folder-open"
            compact
            right={
              <div className="flex items-center gap-1">
                <IconButton
                  icon={
                    <Icon
                      name="chevron-down"
                      size={13}
                      className={cn(
                        "transition-transform duration-180 ease-[cubic-bezier(0.16,1,0.3,1)]",
                        st.formatOpen && "rotate-180",
                      )}
                    />
                  }
                  size={22}
                  variant="ghost"
                  aria-label="展开或收起格式面板"
                  onClick={() => onToggleExpanded("format")}
                />
                <Switch
                  size="small"
                  data-testid="edit-format-switch"
                  aria-label="启用输出格式与目录"
                  checked={Boolean(st.formatEnabled)}
                  onCheckedChange={(checked) => onSetFeature("format", checked)}
                />
              </div>
            }
          >
            {st.formatOpen ? (
              <div className="animate-tulai-banner-in flex flex-col gap-2">
                <div className="flex items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <Select
                      size="small"
                      data-testid="edit-format-select"
                      aria-label="输出格式"
                      options={FORMAT_OPTIONS}
                      value={st.format || "original"}
                      onValueChange={(value) => onSetField("format", value)}
                    />
                  </div>
                  <Tooltip content="压缩品质 1–100，留空使用默认值" side="top">
                    <Input
                      size="small"
                      inputMode="numeric"
                      aria-label="输出品质 (1-100)"
                      placeholder="默认品质"
                      value={qualityDraft.value}
                      onChange={(e) => qualityDraft.onChange(e.target.value)}
                      onCompositionStart={qualityDraft.onCompositionStart}
                      onCompositionEnd={(e) => qualityDraft.onCompositionEnd(e.currentTarget.value)}
                      onBlur={qualityDraft.onBlur}
                      className="tnum w-[84px]"
                    />
                  </Tooltip>
                </div>

                <Input
                  size="small"
                  aria-label="输出文件名后缀"
                  placeholder="输出文件名后缀 (如 _edited)"
                  value={suffixDraft.value}
                  onChange={(e) => suffixDraft.onChange(e.target.value)}
                  onCompositionStart={suffixDraft.onCompositionStart}
                  onCompositionEnd={(e) => suffixDraft.onCompositionEnd(e.currentTarget.value)}
                  onBlur={suffixDraft.onBlur}
                />

                <InsetPillRow className="flex-row items-center gap-1.5 py-1.5">
                  <Tooltip content={st.outDir || "默认输出到原图同目录"} side="top">
                    <span
                      tabIndex={0}
                      className="font-mono-console min-w-0 flex-1 truncate rounded text-[11px] text-muted select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                    >
                      {st.outDir || "默认输出到原图同目录"}
                    </span>
                  </Tooltip>
                  <Button
                    text="更改"
                    size="xs"
                    variant="outline"
                    onClick={onPickOutDir}
                  />
                  <Button
                    text="打开"
                    size="xs"
                    variant="ghost"
                    disabled={!st.outDir}
                    onClick={onOpenOutDir}
                  />
                </InsetPillRow>
              </div>
            ) : null}
          </BentoCard>
        </div>
      </div>
    </aside>
  );
}
