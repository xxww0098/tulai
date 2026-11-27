import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { HoverPreviewTarget, clearHoverPreview } from "@/components/HoverPreview";
import { BentoCard, OutlineBadge } from "@/components/primitives";
import {
  Button,
  Checkbox,
  DropZoneOverlay,
  DropdownMenu,
  Icon,
  IconButton,
  Input,
  Select,
  Switch,
  Tabs,
  Textarea,
  Tooltip,
  TooltipGroup,
  type MenuActionItem,
} from "@/components/ui";
import { AccountLanes } from "@/components/workbench/AccountLanes";
import { LinePromptEditor } from "@/components/workbench/LinePromptEditor";
import { ResultGrid } from "@/components/workbench/ResultGrid";
import { WorkbenchDialogs } from "@/components/workbench/WorkbenchDialogs";
import { toThumbUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import { Prompts, Workbench, useFileDrop } from "@/lib/ipc";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import {
  TAB_FISSION,
  TAB_GEN,
  TAB_PROMPTS,
  TAB_WASH,
  goTab,
  refreshWorkbench,
  usePageLoading,
  useShell,
  useWorkbench,
  type PromptOptionDTO,
  type ShellStateDTO,
  type WorkbenchStateDTO,
} from "@/store";

function fireIpc(call: Promise<unknown>): void {
  void call.catch(() => {});
}

function buildFallbackWorkbench(shell: ShellStateDTO): WorkbenchStateDTO {
  const mode =
    shell.tab === TAB_GEN ? "free" : shell.tab === TAB_FISSION ? "fission" : "wash";
  const page = shell.tab <= TAB_FISSION ? shell.tab : TAB_WASH;
  const platform =
    shell.platform === "千问" || shell.platform === "千问国际" || shell.platform === "Gemini"
      ? shell.platform
      : "豆包";
  const qianwen = platform === "千问";
  const qwenIntl = platform === "千问国际";
  const gemini = platform === "Gemini";
  const startLabel =
    mode === "wash" ? "开始洗图" : mode === "fission" ? "开始裂变" : "开始文生图";

  return {
    mode,
    page,
    batchMode: 0,
    platform,
    platforms: ["豆包", "千问", "千问国际", "Gemini"],
    doubao: platform === "豆包",
    qianwen,
    qwenIntl,
    gemini,
    prompts: "",
    promptChoice: "",
    promptOptions: [],
    promptTemplates: [],
    promptSuffix: "",
    useSuffix: false,
    washStyle: "",
    washStyleOptions: [],
    outputPreset: "",
    outputPresets: [],
    outputMode: "",
    outputRatio: "",
    outputRatios: [],
    sources: [],
    sourcesTotal: 0,
    washedCount: 0,
    selectedSource: 0,
    skipImages: false,
    running: Boolean(shell.running),
    runPage: shell.runPage || page,
    progress: shell.progress || 0,
    progText: shell.progText || "",
    canStart: false,
    startLabel,
    planMissing: [],
    outputNote: "",
    activeRows: [],
    issueRows: [],
    pendingTiles: [],
    doneTiles: [],
    pendingCount: 0,
    doneCount: 0,
    doneTasks: 0,
    failedCount: 0,
    needsCount: 0,
    activeCount: 0,
    totalCount: 0,
    resultsDoneOpen: true,
    stageTab: 0,
    lanes: [],
    laneCollapsed: false,
    concurrencyAuto: true,
    concurrencyMax: 2,
    previewOn: false,
    previewPath: "",
    previewUrl: "",
    galleryOn: false,
    gallerySelected: 0,
    galleryAll: false,
    galleryItems: [],
    structuredOpen: false,
    structuredCount: 0,
    canUndoRewash: false,
    rewashUndoCount: 0,
    repeatCount: 1,
    imagePromptMode: "unified",
    plannedTasks: 0,
    promptUnitCount: 0,
    templateLocked: false,
  };
}

function splitNonEmptyLines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

const SOURCE_GRID_STYLE: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(66px, 1fr))",
  gap: 6,
};

function WorkbenchSkeleton() {
  return (
    <div
      data-testid="wb-skeleton"
      className="flex min-h-0 flex-1 gap-3 bg-paper px-3 pb-3"
    >
      <div className="flex h-full min-h-0 w-[26%] min-w-[284px] max-w-[356px] shrink-0 flex-col gap-3">
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          <div className="min-h-[188px] flex-1 animate-pulse rounded-[16px] border border-hairline bg-card p-3" />
          <div className="h-[212px] shrink-0 animate-pulse rounded-[16px] border border-hairline bg-card p-3" />
        </div>
        <div className="h-[56px] shrink-0 animate-pulse rounded-[16px] border border-hairline bg-card p-3" />
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3">
        <div className="h-[110px] animate-pulse rounded-[16px] border border-hairline bg-card p-3" />
        <div className="min-h-0 flex-1 animate-pulse rounded-[16px] border border-hairline bg-card p-3" />
      </div>
    </div>
  );
}

export function WorkbenchPage() {
  const rawWb = useWorkbench();
  const loading = usePageLoading("workbench");
  const shell = useShell();
  const [promptResetTick, setPromptResetTick] = useState(0);

  const wb = useMemo(
    () => rawWb ?? buildFallbackWorkbench(shell),
    [rawWb, shell],
  );

  const platform =
    wb.platform || (wb.gemini ? "Gemini" : wb.qwenIntl ? "千问国际" : wb.qianwen ? "千问" : "豆包");
  const mode = wb.mode || "wash";
  const isFree = mode === "free";
  const isWash = mode === "wash";
  const isFission = mode === "fission";
  const isFreeText = isFree && wb.batchMode === 0;
  const isFreeImage = isFree && wb.batchMode === 1;

  const commitPrompt = useCallback((next: string) => {
    fireIpc(Workbench.setWorkbenchField("prompts", next));
  }, []);

  const promptDraft = useSyncedDraft(
    wb.prompts || "",
    `${platform}-${mode}-${wb.page ?? 1}-${wb.batchMode ?? 0}-${promptResetTick}`,
    commitPrompt,
    { debounceMs: 90 },
  );

  const handleBatchModeChange = useCallback(
    (value: string) => {
      if (promptDraft.value !== (wb.prompts || "")) {
        promptDraft.flush();
      }
      setPromptResetTick((t) => t + 1);
      fireIpc(Workbench.setWorkbenchField("batchMode", value));
    },
    [promptDraft, wb.prompts],
  );

  const useSuffix = Boolean(wb.useSuffix);
  const commitSuffix = useCallback(
    (next: string) => {
      fireIpc(Prompts.setPromptSuffix(next, useSuffix));
    },
    [useSuffix],
  );

  const suffixDraft = useSyncedDraft(
    wb.promptSuffix || "",
    `suffix-${mode}`,
    commitSuffix,
    { debounceMs: 90 },
  );

  const flushDrafts = useCallback(() => {
    promptDraft.flush();
    suffixDraft.flush();
  }, [promptDraft, suffixDraft]);

  const { isDragging } = useFileDrop({
    enabled: Boolean(!loading && !wb.running),
    onDrop: (paths) => {
      if (paths.length > 0) {
        promptDraft.flush();
        fireIpc(Workbench.addWorkbenchSources(paths));
      }
    },
  });

  const [startingScope, setStartingScope] = useState<string | null>(null);
  const startPending = useRef(false);
  const lastStartedScope = useRef<string | null>(null);
  const start = useCallback(async () => {
    if (startPending.current || wb.running || !wb.canStart) return;
    startPending.current = true;
    lastStartedScope.current = `${platform}:${mode}`;
    setStartingScope(lastStartedScope.current);
    try {
      flushDrafts();
      await Workbench.startTasks();
      await refreshWorkbench();
    } finally {
      startPending.current = false;
      setStartingScope(null);
    }
  }, [wb.running, wb.canStart, platform, mode, flushDrafts]);

  // Cmd/Ctrl + Enter starts workbench tasks when ready
  useEffect(() => {
    if (!rawWb && loading) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.isComposing || e.altKey || e.shiftKey) return;
      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
        if (!wb.running && wb.canStart) {
          e.preventDefault();
          fireIpc(start());
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [rawWb, loading, wb, start]);

  const hasActiveTemplate = Boolean(wb.promptChoice && wb.promptChoice !== "选择提示词");
  const templateMenuItems = useMemo<MenuActionItem[]>(() => {
    const structured = wb.promptTemplates ?? [];
    const templates: PromptOptionDTO[] =
      structured.length > 0
        ? structured
        : (wb.promptOptions ?? [])
            .filter((opt) => Boolean(opt && opt !== "选择提示词"))
            .map((opt) => ({
              id: opt,
              label: opt,
              kindLbl: "",
              builtin: false,
              matched: true,
            }));

    const matched = templates.filter((item) => item.matched);
    const other = templates.filter((item) => !item.matched);
    const resetLabel = isWash
      ? "恢复默认洗图词"
      : isFission && !wb.skipImages
        ? "恢复默认裂变词"
        : "自由输入（不套用模板）";

    const applyTemplate = (label: string) => {
      setPromptResetTick((t) => t + 1);
      fireIpc(Workbench.applyWorkbenchPrompt(label));
    };

    const toMenuItem = (item: PromptOptionDTO, prefix: string, idx: number): MenuActionItem => {
      const selected = wb.promptChoice === item.label;
      const tag = item.builtin
        ? "内置"
        : item.matched
          ? item.kindLbl === "通用"
            ? "通用"
            : undefined
          : item.kindLbl || undefined;
      return {
        id: `${prefix}-${item.id || idx}`,
        label: item.label,
        icon: selected ? "check" : undefined,
        shortcut: tag,
        onSelect: () => applyTemplate(item.label),
      };
    };

    const hasDefaultReset = isWash || (isFission && !wb.skipImages);
    const items: MenuActionItem[] = [
      {
        id: "prompt-opt-reset",
        label: resetLabel,
        icon: hasDefaultReset ? "rotate-ccw" : !hasActiveTemplate ? "check" : undefined,
        shortcut: hasDefaultReset ? "默认词" : undefined,
        onSelect: () => applyTemplate("选择提示词"),
      },
      { type: "separator" },
    ];

    if (matched.length > 0) {
      if (other.length > 0) {
        items.push({
          type: "label",
          label: isWash ? "洗图与通用" : isFission ? "裂变与通用" : "生图与通用",
        });
      }
      matched.forEach((item, idx) => items.push(toMenuItem(item, "prompt-opt", idx)));
    } else if (other.length === 0) {
      items.push({
        id: "prompt-opt-empty",
        label: "暂无已保存模板",
        disabled: true,
      });
    }

    if (other.length > 0) {
      if (matched.length > 0) {
        items.push({ type: "separator" });
      }
      items.push({ type: "label", label: "其他分类" });
      other.forEach((item, idx) => items.push(toMenuItem(item, "prompt-other", idx)));
    }

    items.push(
      { type: "separator" },
      {
        id: "prompt-manage-library",
        label: templates.length > 0 ? "管理提示词模板..." : "前往提示词库新建...",
        icon: "book-open",
        onSelect: () => {
          void goTab(TAB_PROMPTS);
        },
      },
    );
    return items;
  }, [
    wb.promptTemplates,
    wb.promptOptions,
    wb.promptChoice,
    wb.skipImages,
    hasActiveTemplate,
    isWash,
    isFission,
  ]);

  const sources = wb.sources ?? [];
  const sourceCount = wb.sourcesTotal || sources.length;
  const repeatCount = Math.max(1, Math.min(10, wb.repeatCount || 1));
  const imagePromptMode = wb.imagePromptMode === "per_line" ? "per_line" : "unified";
  const draftLines = useMemo(() => splitNonEmptyLines(promptDraft.value), [promptDraft.value]);
  const hasDuplicateDraftLines = useMemo(() => {
    if (draftLines.length <= 1) return false;
    return new Set(draftLines).size < draftLines.length;
  }, [draftLines]);
  const effectiveTemplateLocked = Boolean(
    isFreeText &&
      wb.templateLocked &&
      !promptDraft.isDirty &&
      promptDraft.value === (wb.prompts || ""),
  );
  const livePromptUnits = useMemo(() => {
    if (isFreeText) {
      if (effectiveTemplateLocked) return promptDraft.value.trim() ? 1 : 0;
      return draftLines.length;
    }
    if (isFreeImage) {
      if (imagePromptMode === "per_line" && !hasActiveTemplate) return draftLines.length;
      return promptDraft.value.trim() ? 1 : 0;
    }
    return 0;
  }, [
    isFreeText,
    isFreeImage,
    effectiveTemplateLocked,
    promptDraft.value,
    draftLines.length,
    imagePromptMode,
    hasActiveTemplate,
  ]);
  const livePlannedTasks = useMemo(() => {
    if (isFreeImage) return sourceCount * repeatCount;
    if (isFreeText) {
      const units = livePromptUnits > 0 ? livePromptUnits : wb.structuredCount > 0 ? 1 : 0;
      return units * repeatCount;
    }
    return wb.plannedTasks || 0;
  }, [isFreeImage, isFreeText, sourceCount, repeatCount, livePromptUnits, wb.structuredCount, wb.plannedTasks]);

  const batchModeOptions = useMemo(
    () => [
      { value: "0", label: "文生图", icon: "file-text" },
      {
        value: "1",
        label: "图生图",
        icon: "images",
        badge: sourceCount > 0 ? sourceCount : undefined,
      },
    ],
    [sourceCount],
  );

  if (!rawWb && loading) return <WorkbenchSkeleton />;

  const washStyleList = (wb.washStyleOptions ?? []).map((s) => ({ value: s, label: s }));
  const isTextOnlyRatio = isFreeText || (isFission && Boolean(wb.skipImages));
  const originalRatioLabel = isTextOnlyRatio ? "自动比例" : "原比例";
  const presetList = (wb.outputPresets ?? []).map((p) => ({
    value: p,
    label: p === "原比例" ? originalRatioLabel : p,
  }));
  const showOutputSize = presetList.length > 0;

  const outputSizeSection = showOutputSize ? (
    <div className="flex flex-col gap-1.5">
      <div data-testid="wb-output-preset-wrap" className="flex items-center gap-2">
        <span className="w-[48px] shrink-0 text-[11px] font-medium text-muted">
          输出尺寸
        </span>
        <Select
          data-testid="wb-output-preset-select"
          aria-label="输出尺寸"
          options={presetList}
          value={wb.outputPreset || ""}
          placeholder={originalRatioLabel}
          disabled={Boolean(wb.running)}
          size="small"
          className="flex-1"
          onValueChange={(value) =>
            fireIpc(Workbench.setOutputSize(value, "", ""))
          }
        />
      </div>
    </div>
  ) : null;

  const hasUpperControls =
    isFree || isFission || washStyleList.length > 0 || Boolean(outputSizeSection);
  const promptPlaceholder = isWash
    ? "提取主画，去掉载体与边框，输出纯 2D 画面；可补充其他要求..."
    : isFission
      ? wb.skipImages
        ? "输入裂变主题（自动展开为 4 个方向），或每行 1 个方向共 4 行..."
        : "输入多角度/多场景裂变要求，留空按默认方向重构母图..."
      : isFreeImage
        ? imagePromptMode === "per_line"
          ? "每行一条改图指令，将按顺序循环搭配下方各张参考图..."
          : "输入统一改图描述（支持多行），将整段搭配下方每张参考图批量生成..."
        : effectiveTemplateLocked
          ? "当前已套用模板（整段作为 1 条发送），可直接微调或点击下方「转为逐行」..."
          : "一行输入一条提示词，回车换行即可批量文生图，也可导入或拖入 TXT...";
  const promptMinHeightClass = isWash
    ? "min-h-[132px]"
    : isFission
      ? "min-h-[116px]"
      : "min-h-[112px]";

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      {/* 模块化 Bento 双栏工作台：左侧三张独立圆角卡片 + 右侧账号与结果卡片 */}
      <div className="flex min-h-0 flex-1 flex-row gap-3 bg-paper px-3 pb-3">
        {/* 左栏：上部提示词与设置卡（随窗口拉伸） + 固定高度参考素材卡；底部锚定执行控制卡 */}
        <div
          data-testid="wb-composer"
          className="flex h-full min-h-0 w-[26%] min-w-[284px] max-w-[356px] shrink-0 flex-col gap-3"
        >
          <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pr-0.5 pb-0.5 -mr-0.5">
            {/* 卡片一：模式配置与提示词（上方功能控制 + 右上角模板下拉 + 底部提示词编辑） */}
            <BentoCard
              compact
              data-testid="wb-config-card"
              icon={isWash ? "sliders-horizontal" : isFission ? "copy" : "sparkles"}
              title="提示词"
              className="flex-1"
              right={
                <DropdownMenu
                  align="end"
                  side="bottom"
                  contentClassName="max-h-[300px] min-w-[196px] max-w-[260px] overflow-y-auto"
                  items={templateMenuItems}
                  trigger={
                    <Tooltip
                      content={
                        hasActiveTemplate
                          ? `当前模板：${wb.promptChoice}`
                          : "注入或套用提示词模板"
                      }
                      side="bottom"
                    >
                      <Button
                        data-testid="wb-prompt-template-btn"
                        text={hasActiveTemplate ? wb.promptChoice : "模板"}
                        size="xs"
                        icon="book-open"
                        variant={hasActiveTemplate ? "secondary" : "ghost"}
                        className="max-w-[108px]"
                      />
                    </Tooltip>
                  }
                />
              }
            >
              <div key={`${wb.mode || "wash"}-${wb.batchMode ?? 0}`} className="animate-tulai-panel-in flex flex-1 flex-col gap-2">
                {/* 自由生图：文生图 / 图生图切换 + 输出尺寸 + 每项次数 + 追加后缀/结构化 */}
                {isFree ? (
                  <>
                    <div className="shrink-0">
                      <Tabs
                        data-testid="wb-batch-mode-tabs"
                        variant="segmented"
                        size="small"
                        fullWidth
                        options={batchModeOptions}
                        value={String(wb.batchMode)}
                        onValueChange={handleBatchModeChange}
                      />
                    </div>
                    {outputSizeSection ? (
                      <div className="shrink-0">{outputSizeSection}</div>
                    ) : null}
                    <div
                      data-testid="wb-repeat-row"
                      className="flex shrink-0 items-center justify-between gap-2"
                    >
                      <div className="flex items-center gap-2">
                        <span className="w-[48px] shrink-0 text-[11px] font-medium text-muted">
                          {isFreeImage ? "每图次数" : "每条次数"}
                        </span>
                        <div className="inline-flex h-[26px] items-center rounded-[8px] border border-hairline bg-sunken p-0.5">
                          <button
                            type="button"
                            data-testid="wb-repeat-dec"
                            aria-label="减少生成次数"
                            disabled={Boolean(wb.running) || repeatCount <= 1}
                            onClick={() =>
                              fireIpc(
                                Workbench.setWorkbenchField(
                                  "repeatCount",
                                  String(Math.max(1, repeatCount - 1)),
                                ),
                              )
                            }
                            className="flex size-5 cursor-pointer items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-card hover:text-ink disabled:cursor-not-allowed disabled:opacity-35"
                          >
                            <Icon name="minus" size={11} />
                          </button>
                          <span
                            data-testid="wb-repeat-value"
                            className="tnum min-w-[38px] text-center text-xs font-medium text-ink select-none"
                          >
                            {`${repeatCount} 次`}
                          </span>
                          <button
                            type="button"
                            data-testid="wb-repeat-inc"
                            aria-label="增加生成次数"
                            disabled={Boolean(wb.running) || repeatCount >= 10}
                            onClick={() =>
                              fireIpc(
                                Workbench.setWorkbenchField(
                                  "repeatCount",
                                  String(Math.min(10, repeatCount + 1)),
                                ),
                              )
                            }
                            className="flex size-5 cursor-pointer items-center justify-center rounded-[6px] text-muted transition-colors hover:bg-card hover:text-ink disabled:cursor-not-allowed disabled:opacity-35"
                          >
                            <Icon name="plus" size={11} />
                          </button>
                        </div>
                      </div>
                      {livePlannedTasks > 0 ? (
                        <span
                          data-testid="wb-planned-tasks"
                          className="tnum text-[11px] font-medium text-muted"
                        >
                          {`共 ${livePlannedTasks} 个任务`}
                        </span>
                      ) : null}
                    </div>
                    <div className="flex shrink-0 flex-col gap-1.5">
                      <div className="flex items-center justify-between gap-2">
                        <Checkbox
                          data-testid="wb-suffix-checkbox"
                          size="small"
                          label="全局后缀"
                          checked={useSuffix}
                          onCheckedChange={(checked) => {
                            suffixDraft.flush();
                            fireIpc(Prompts.setPromptSuffix(suffixDraft.value, checked));
                          }}
                        />
                        <div className="flex min-w-0 flex-1 items-center justify-end gap-1.5">
                          {!useSuffix && suffixDraft.value.trim() ? (
                            <Tooltip content={suffixDraft.value} side="top">
                              <button
                                type="button"
                                onClick={() => {
                                  suffixDraft.flush();
                                  fireIpc(Prompts.setPromptSuffix(suffixDraft.value, true));
                                }}
                                className="min-w-0 flex-1 cursor-pointer truncate text-right text-[11px] text-muted transition-colors hover:text-ink"
                              >
                                {suffixDraft.value}
                              </button>
                            </Tooltip>
                          ) : null}
                          <Button
                            data-testid="wb-structured-btn"
                            text={
                              wb.structuredCount > 0
                                ? `结构化 (${wb.structuredCount})`
                                : "结构化"
                            }
                            size="xs"
                            icon="sliders-horizontal"
                            variant={wb.structuredCount > 0 ? "outline" : "ghost"}
                            className={cn(
                              "shrink-0",
                              wb.structuredCount > 0 &&
                                "border-accent-border bg-accent-soft text-accent hover:bg-accent-soft/80",
                            )}
                            onClick={() =>
                              fireIpc(Workbench.setWorkbenchField("structuredOpen", "true"))
                            }
                          />
                        </div>
                      </div>
                      {useSuffix ? (
                        <Input
                          data-testid="wb-suffix-input"
                          size="small"
                          value={suffixDraft.value}
                          placeholder="附加在自由生图末尾，如：高清细节、自然光"
                          onChange={(e) => suffixDraft.onChange(e.target.value)}
                          onCompositionStart={suffixDraft.onCompositionStart}
                          onCompositionEnd={(e) =>
                            suffixDraft.onCompositionEnd(e.currentTarget.value)
                          }
                          onBlur={suffixDraft.onBlur}
                          className="animate-tulai-banner-in"
                        />
                      ) : null}
                    </div>
                  </>
                ) : null}

                {/* 批量洗图：洗图风格 + 输出尺寸 */}
                {isWash ? (
                  <>
                    {washStyleList.length > 0 ? (
                      <div className="flex shrink-0 items-center gap-2">
                        <span className="w-[48px] shrink-0 text-[11px] font-medium text-muted">
                          洗图风格
                        </span>
                        <Select
                          data-testid="wb-wash-style-select"
                          aria-label="洗图风格"
                          options={washStyleList}
                          value={wb.washStyle || ""}
                          placeholder="选择洗图风格"
                          size="small"
                          className="flex-1"
                          onValueChange={(value) =>
                            fireIpc(Workbench.setWorkbenchField("washStyle", value))
                          }
                        />
                      </div>
                    ) : null}
                    {outputSizeSection ? (
                      <div className="shrink-0">{outputSizeSection}</div>
                    ) : null}
                  </>
                ) : null}

                {/* 裂变图片：输出尺寸 + 跳过参考图开关 */}
                {isFission ? (
                  <>
                    {outputSizeSection ? (
                      <div className="shrink-0">{outputSizeSection}</div>
                    ) : null}
                    <div className="flex shrink-0 items-center justify-between gap-2">
                      <span className="flex-1 text-xs text-ink">
                        跳过参考图直接按提示词裂变
                      </span>
                      <Switch
                        size="small"
                        checked={Boolean(wb.skipImages)}
                        aria-label="跳过参考图直接按提示词裂变"
                        onCheckedChange={(checked) => {
                          promptDraft.flush();
                          setPromptResetTick((t) => t + 1);
                          fireIpc(
                            Workbench.setWorkbenchField(
                              "skipImages",
                              checked ? "true" : "false",
                            ),
                          );
                        }}
                      />
                    </div>
                  </>
                ) : null}

                {/* 底部统一提示词编辑窗口 */}
                <div
                  className={cn(
                    "flex flex-1 flex-col gap-1.5",
                    hasUpperControls && "border-t border-divider pt-2",
                  )}
                >
                  {isFreeText && !effectiveTemplateLocked ? (
                    <LinePromptEditor
                      disabled={Boolean(wb.running)}
                      value={promptDraft.value}
                      placeholder={promptPlaceholder}
                      onChange={(next) => promptDraft.onChange(next)}
                      onCompositionStart={promptDraft.onCompositionStart}
                      onCompositionEnd={promptDraft.onCompositionEnd}
                      onBlur={promptDraft.onBlur}
                      className={promptMinHeightClass}
                    />
                  ) : (
                    <Textarea
                      data-testid="wb-prompt-editor"
                      disabled={Boolean(wb.running)}
                      value={promptDraft.value}
                      placeholder={promptPlaceholder}
                      onChange={(e) => promptDraft.onChange(e.target.value)}
                      onCompositionStart={promptDraft.onCompositionStart}
                      onCompositionEnd={(e) =>
                        promptDraft.onCompositionEnd(e.currentTarget.value)
                      }
                      onBlur={promptDraft.onBlur}
                      className={cn(
                        "flex-1 rounded-[10px] border-hairline bg-sunken/80 px-2.5 py-2 text-xs leading-relaxed text-ink focus:bg-field dark:bg-[#0B0F17]",
                        promptMinHeightClass,
                      )}
                    />
                  )}
                  {isFree ? (
                    <div
                      data-testid="wb-prompt-meta-bar"
                      className="flex shrink-0 items-center justify-between gap-2 text-[11px] text-muted"
                    >
                      <div className="flex min-w-0 items-center gap-1.5">
                        {isFreeText ? (
                          effectiveTemplateLocked ? (
                            <>
                              <span className="tnum truncate font-medium text-accent">
                                整段发送 · 1 条
                              </span>
                              <button
                                type="button"
                                data-testid="wb-detach-template-btn"
                                disabled={Boolean(wb.running)}
                                onClick={() => {
                                  promptDraft.flush();
                                  fireIpc(Workbench.setWorkbenchField("detachTemplate", ""));
                                }}
                                className="shrink-0 cursor-pointer rounded px-1 py-0.5 text-[11px] font-medium text-muted transition-colors hover:bg-sunken-hover hover:text-ink disabled:cursor-not-allowed disabled:opacity-45"
                              >
                                转为逐行
                              </button>
                            </>
                          ) : (
                            <span className="tnum truncate">
                              {livePromptUnits > 0
                                ? `${livePromptUnits} 条提示词`
                                : wb.structuredCount > 0
                                  ? "已启用结构化参数"
                                  : "每行 1 条 · 支持导入TXT"}
                            </span>
                          )
                        ) : (
                          <>
                            <Tooltip
                              content={
                                imagePromptMode === "per_line"
                                  ? "逐行配对：每行一条指令，按顺序循环匹配下方各张参考图（点击切换为统一描述）"
                                  : "统一描述：整段多行描述同时应用到下方每张参考图（点击切换为逐行配对）"
                              }
                              side="bottom"
                            >
                              <button
                                type="button"
                                data-testid="wb-img-prompt-mode-btn"
                                disabled={Boolean(wb.running)}
                                onClick={() =>
                                  fireIpc(
                                    Workbench.setWorkbenchField(
                                      "imagePromptMode",
                                      imagePromptMode === "per_line" ? "unified" : "per_line",
                                    ),
                                  )
                                }
                                className={cn(
                                  "inline-flex shrink-0 cursor-pointer items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-45",
                                  imagePromptMode === "per_line"
                                    ? "border-accent-border bg-accent-soft text-accent"
                                    : "border-hairline bg-sunken text-muted hover:text-ink",
                                )}
                              >
                                <Icon
                                  name={imagePromptMode === "per_line" ? "list-ordered" : "layers"}
                                  size={11}
                                />
                                <span>{imagePromptMode === "per_line" ? "逐行配对" : "统一描述"}</span>
                              </button>
                            </Tooltip>
                            {imagePromptMode === "per_line" && livePromptUnits > 0 ? (
                              <span className="tnum truncate">{`${livePromptUnits} 条循环`}</span>
                            ) : null}
                          </>
                        )}
                      </div>
                      <TooltipGroup className="flex shrink-0 items-center gap-1">
                        {isFreeText && !effectiveTemplateLocked && hasDuplicateDraftLines ? (
                          <button
                            type="button"
                            data-testid="wb-dedup-prompts-btn"
                            disabled={Boolean(wb.running)}
                            onClick={() => {
                              const unique = Array.from(new Set(draftLines)).join("\n");
                              promptDraft.setValue(unique, true);
                              commitPrompt(unique);
                            }}
                            className="cursor-pointer rounded px-1.5 py-0.5 text-[11px] font-medium text-muted transition-colors hover:bg-sunken-hover hover:text-ink disabled:cursor-not-allowed disabled:opacity-45"
                          >
                            去重
                          </button>
                        ) : null}
                        {isFreeText ? (
                          <Tooltip content="从 .txt / .md 文本文件批量导入提示词（每行一条），也支持直接拖入" side="bottom">
                            <button
                              type="button"
                              data-testid="wb-import-txt-btn"
                              disabled={Boolean(wb.running)}
                              onClick={() => {
                                promptDraft.flush();
                                fireIpc(Workbench.pickPromptTextFile());
                              }}
                              className="inline-flex cursor-pointer items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium text-muted transition-colors hover:bg-sunken-hover hover:text-ink disabled:cursor-not-allowed disabled:opacity-45"
                            >
                              <Icon name="upload" size={11} />
                              <span>导入TXT</span>
                            </button>
                          </Tooltip>
                        ) : null}
                        {promptDraft.value.trim() ? (
                          <button
                            type="button"
                            data-testid="wb-clear-prompts-btn"
                            disabled={Boolean(wb.running)}
                            onClick={() => {
                              promptDraft.setValue("", true);
                              commitPrompt("");
                            }}
                            className="cursor-pointer rounded px-1.5 py-0.5 text-[11px] font-medium text-muted transition-colors hover:bg-sunken-hover hover:text-ink disabled:cursor-not-allowed disabled:opacity-45"
                          >
                            清空
                          </button>
                        ) : null}
                      </TooltipGroup>
                    </div>
                  ) : null}
                </div>
              </div>
            </BentoCard>

            {/* 卡片二：固定高度参考素材（文生图模式隐藏以释放提示词编辑高度） */}
            {!isFreeText ? (
              <BentoCard
                compact
                data-testid="wb-sources-card"
                icon="images"
                title={isFreeImage ? `参考图 (${sourceCount})` : `参考素材 (${sourceCount})`}
                className="animate-tulai-panel-in h-[212px] shrink-0 overflow-hidden"
                right={
                  <div className="flex shrink-0 items-center gap-1">
                    {isFission && wb.skipImages ? <OutlineBadge text="已跳过" /> : null}
                    <Button
                      data-testid="wb-import-files"
                      text="导入文件"
                      size="xs"
                      variant="outline"
                      className="px-2"
                      aria-label={isFission ? "导入主图文件" : "导入图片文件"}
                      onClick={() => fireIpc(Workbench.pickWorkbenchFiles())}
                    />
                    {!isFission ? (
                      <Button
                        data-testid="wb-import-folder"
                        text="导入文件夹"
                        size="xs"
                        variant="outline"
                        className="px-2"
                        aria-label="导入图片文件夹"
                        onClick={() => fireIpc(Workbench.pickWorkbenchFolder())}
                      />
                    ) : null}
                    {sourceCount > 0 ? (
                      <Tooltip content="清空全部素材" side="bottom">
                        <IconButton
                          icon="trash"
                          size={24}
                          variant="ghost"
                          aria-label="清空素材"
                          onClick={() => fireIpc(Workbench.clearWorkbenchSources())}
                        />
                      </Tooltip>
                    ) : null}
                  </div>
                }
              >
                {isWash && wb.washedCount > 0 && !wb.running ? (
                  <div
                    data-testid="wb-washed-bar"
                    className="animate-tulai-banner-in mb-2 flex shrink-0 items-center justify-between gap-2 rounded-[10px] border border-[var(--status-ok-border)] bg-[var(--status-ok-bg)] px-2.5 py-1"
                  >
                    <div className="flex min-w-0 items-center gap-1.5">
                      <Icon name="check" size={11} className="text-status-ok" />
                      <span className="tnum truncate text-[11px] font-medium text-status-ok">
                        {`已洗 ${wb.washedCount} / ${sourceCount}`}
                      </span>
                    </div>
                    <Button
                      data-testid="wb-clear-washed-btn"
                      text="移除已洗"
                      size="xs"
                      variant="ghost"
                      onClick={() => fireIpc(Workbench.clearWashedWorkbenchSources())}
                    />
                  </div>
                ) : null}

                <div
                  className={cn(
                    "scroll-gutter min-h-0 flex-1 overflow-y-auto pr-0.5",
                    sourceCount === 0 && "flex flex-col",
                  )}
                >
                  {sourceCount === 0 ? (
                    <button
                      type="button"
                      data-testid="wb-sources-dropzone"
                      onClick={() => fireIpc(Workbench.pickWorkbenchFiles())}
                      className={cn(
                        "flex min-h-[120px] w-full flex-1 cursor-pointer flex-col items-center justify-center gap-1 rounded-[12px] border border-dashed border-strong/80 bg-sunken/70 px-3 text-center transition-colors duration-150 hover:border-accent",
                        isFission && wb.skipImages && "opacity-60",
                      )}
                    >
                      <Icon name="image" size={18} className="text-subtle" />
                      <span className="text-xs text-muted">
                        {isFission
                          ? "点击选择或直接拖入参考主图"
                          : isFreeImage
                            ? "点击选择或直接拖入参考图 / 文件夹"
                            : "点击选择或直接拖入待洗图片 / 文件夹"}
                      </span>
                      {isFission ? (
                        <span className="text-[11px] text-muted">
                          {wb.skipImages
                            ? "已跳过参考图：在上方输入裂变主题或 4 行方向即可开始"
                            : "单图裂变：支持拖入 1 张主图"}
                        </span>
                      ) : isFreeImage ? (
                        <span className="text-[11px] text-muted">
                          支持多张参考图批量图生图，可切换统一描述或逐行配对
                        </span>
                      ) : null}
                    </button>
                  ) : (
                    <div style={SOURCE_GRID_STYLE}>
                      {sources.slice(0, 24).map((item) => {
                        const srcStatus = isWash
                          ? item.washing
                            ? "洗图中"
                            : item.washed
                              ? "已洗完"
                              : "等待进入流水线"
                          : isFission
                            ? wb.skipImages
                              ? "当前已跳过母图"
                              : "当前裂变母图"
                            : "图生图参考图";
                      const srcBadge = isWash
                        ? item.washing
                          ? "洗图中"
                          : item.washed
                            ? "已洗"
                            : "待洗"
                        : isFission
                          ? "母图"
                          : "参考图";
                      const srcBadgeVariant = isWash
                        ? item.washing
                          ? "accent"
                          : item.washed
                            ? "success"
                            : "neutral"
                        : "neutral";
                      const thumbUrl = toThumbUrl(item.fileUrl, 360);
                      return (
                        <HoverPreviewTarget
                          key={`${platform}-${item.path || String(item.index)}`}
                          previewKey={`wb-src-${platform}-${item.index}-${item.path || ""}`}
                          src={item.fileUrl}
                          title={item.name || `素材 #${item.index + 1}`}
                          subtitle={srcStatus}
                          badge={srcBadge}
                          badgeVariant={srcBadgeVariant}
                          className="min-w-0"
                        >
                          <div
                            role="button"
                            tabIndex={0}
                            onClick={() => {
                              clearHoverPreview();
                              fireIpc(Workbench.selectWorkbenchSource(item.index));
                            }}
                            onKeyDown={(e) => {
                              if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                clearHoverPreview();
                                fireIpc(Workbench.selectWorkbenchSource(item.index));
                              }
                            }}
                            className={cn(
                              "group relative cursor-pointer overflow-hidden rounded-[10px] border transition-[border-color,background-color] duration-150",
                              item.washing
                                ? "border-accent bg-accent-soft"
                                : item.selected
                                  ? "border-accent bg-accent-soft/75"
                                  : item.washed
                                    ? "border-emerald-400/75 bg-card dark:border-emerald-500/45 dark:bg-sunken"
                                    : "border-hairline bg-card-header hover:border-strong dark:bg-sunken",
                            )}
                          >
                            <div className="relative flex aspect-square w-full items-center justify-center overflow-hidden bg-sunken dark:bg-[#0D1118]">
                              {thumbUrl ? (
                                <img
                                  src={thumbUrl}
                                  alt={item.name || `素材 #${item.index + 1}`}
                                  loading="lazy"
                                  decoding="async"
                                  className={cn(
                                    "h-full w-full object-contain transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:scale-[1.03]",
                                    !item.washing &&
                                      !item.washed &&
                                      (wb.running || wb.pendingCount > 0) &&
                                      "opacity-80",
                                  )}
                                  data-testid={`wb-source-thumb-${item.index}`}
                                />
                              ) : (
                                <Icon name="image" size={16} className="text-subtle" />
                              )}
                            </div>

                            {/* 左上角实时加工状态标签 */}
                            {item.washing ? (
                              <span className="absolute top-[3px] left-[3px] rounded-md bg-accent/95 px-1 py-0.5 text-[9px] font-semibold leading-none text-on-accent">
                                洗图中
                              </span>
                            ) : item.washed ? (
                              <span className="absolute top-[3px] left-[3px] inline-flex items-center gap-0.5 rounded-md bg-emerald-600/90 px-1 py-0.5 text-[9px] font-semibold leading-none text-white">
                                <Icon name="check" size={8} color="#FFFFFF" />
                                已洗
                              </span>
                            ) : wb.running || wb.pendingCount > 0 ? (
                              <span className="absolute top-[3px] left-[3px] rounded-md bg-slate-900/75 px-1 py-0.5 text-[9px] font-medium leading-none text-slate-100">
                                等待
                              </span>
                            ) : null}

                            {!wb.running ? (
                              <button
                                type="button"
                                aria-label={`移除素材 ${item.name || item.index + 1}`}
                                onClick={(e) => {
                                  e.stopPropagation();
                                  clearHoverPreview();
                                  fireIpc(Workbench.removeWorkbenchSource(item.index));
                                }}
                                className="absolute top-[3px] right-[3px] flex size-4 cursor-pointer items-center justify-center rounded-full bg-slate-900/65 text-white transition-colors hover:bg-rose-600"
                              >
                                <Icon name="x" size={10} color="#FFFFFF" />
                              </button>
                            ) : null}

                            <div className="truncate px-1.5 py-0.5 text-[10px] text-muted">
                              {item.name || `#${item.index + 1}`}
                            </div>
                          </div>
                        </HoverPreviewTarget>
                      );
                    })}
                  </div>
                )}
              </div>

              {sourceCount > 6 ? (
                <Button
                  text={`管理全部 ${sourceCount} 张素材`}
                  size="xs"
                  variant="outline"
                  onClick={() => fireIpc(Workbench.setGalleryOpen(true))}
                  className="mt-2 w-full shrink-0"
                />
              ) : null}
              </BentoCard>
            ) : null}
          </div>

          {/* 卡片三：主行动控制卡片（固定在左下角） */}
          <BentoCard compact data-testid="wb-action-card" className="shrink-0">
            {wb.running ? (
              <Button
                data-testid="wb-stop-btn"
                text="停止任务"
                size="default"
                icon="square"
                variant="secondary"
                onClick={() => fireIpc(Workbench.stopTasks())}
                className="w-full"
              />
            ) : (
              <Button
                data-testid="wb-start-btn"
                text={wb.startLabel || (isFreeImage ? "开始图生图" : "开始文生图")}
                size="default"
                icon="play"
                variant="primary"
                disabled={!wb.canStart || startingScope !== null}
                onClick={() => fireIpc(start())}
                className="tnum w-full"
              />
            )}
          </BentoCard>
        </div>

        {/* 右栏：账号车道卡片 + 流水线与出图结果卡片 */}
        <div
          data-testid="wb-stage"
          className="flex h-full min-h-0 min-w-0 flex-1 flex-col gap-3"
        >
          <AccountLanes wb={wb} />
          <ResultGrid wb={wb} starting={
            startingScope === `${platform}:${mode}` ||
            (wb.running && wb.runPage === wb.page && !wb.progText && lastStartedScope.current === `${platform}:${mode}`)
          } />
        </div>
      </div>

      <WorkbenchDialogs wb={wb} />
      <DropZoneOverlay
        active={isDragging}
        label={
          isFreeText
            ? "松开以导入 TXT 提示词，或导入图片切至图生图"
            : "松开以添加参考图片"
        }
        sublabel={
          isFission
            ? "单图裂变：支持拖入 1 张主图"
            : isFreeText
              ? "支持拖入 .txt 文件（每行一条提示词），或图片 / 文件夹"
              : "支持拖入多张图片或文件夹"
        }
      />
    </div>
  );
}
