import { useCallback, useMemo, useState } from "react";
import {
  DownloadComposerDialog,
  DownloadGuideDialog,
  DownloadImportDialog,
} from "@/components/download/DownloadDialogs";
import { DownloadInspector } from "@/components/download/DownloadInspector";
import { DownloadTaskRow } from "@/components/download/DownloadTaskRow";
import {
  DOWNLOAD_CATEGORIES,
  classifyDroppedSheetPaths,
  countActiveFilterRules,
  flattenSiteGroups,
  formatSpeed,
  isTaskActive,
  isTaskFailed,
  isTaskPaused,
  isTaskQueued,
  runIpc,
  type DownloadListEntry,
} from "@/components/download/downloadFormat";
import { getSiteIconUrl } from "@/components/download/siteIcons";
import {
  BentoCard,
  EmptyNotice,
  FlushPane,
  PageSkeleton,
  PageToolbar,
  PillToggle,
  SegmentedPill,
} from "@/components/primitives";
import {
  Badge,
  Button,
  Checkbox,
  DropZoneOverlay,
  Icon,
  IconButton,
  Input,
  Tooltip,
  VirtualList,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { Download, useFileDrop } from "@/lib/ipc";
import { useEscapeHandler } from "@/lib/shortcuts";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import {
  TAB_SETTINGS,
  goTab,
  showToast,
  updateSettingField,
  useDownload,
  usePageLoading,
  type DownloadStateDTO,
} from "@/store";

const EMPTY_DOWNLOAD_STATE: DownloadStateDTO = {
  filter: 0,
  search: "",
  searchOn: false,
  groupBySite: false,
  selectedId: 0,
  composerOn: false,
  composerErr: "",
  urls: "",
  dir: "",
  resolvedDir: "",
  browserMode: false,
  browserBusy: false,
  browserOpen: false,
  browserUrl: "",
  browserErr: "",
  browserReady: true,
  browserInstallBusy: false,
  browserSupported: true,
  guideOn: false,
  guideSearch: "",
  guideGroups: [],
  import: {
    open: false,
    fileName: "",
    sheets: [],
    sheet: "",
    columns: [],
    folders: false,
    imageCount: 0,
    invalid: 0,
    err: "",
  },
  importNotice: "",
  depth: 1,
  minKb: 0,
  maxKb: 0,
  minW: 0,
  maxW: 0,
  minH: 0,
  maxH: 0,
  tasks: [],
  countAll: 0,
  countOpen: 0,
  countDone: 0,
  countCanceled: 0,
  totalSpeed: 0,
  activeCount: 0,
  queuedCount: 0,
  pausedCount: 0,
  failedCount: 0,
};

export function DownloadPage() {
  const rawState = useDownload();
  const loading = usePageLoading("download");
  const st = rawState ?? EMPTY_DOWNLOAD_STATE;

  const [composerOverride, setComposerOverride] = useState<{
    base: DownloadStateDTO | null;
    open: boolean;
  } | null>(null);
  const [importOverride, setImportOverride] = useState<{
    base: DownloadStateDTO | null;
    open: boolean;
  } | null>(null);
  const [guideOverride, setGuideOverride] = useState<{
    base: DownloadStateDTO | null;
    open: boolean;
  } | null>(null);
  const [selectedOverride, setSelectedOverride] = useState<{
    base: DownloadStateDTO | null;
    id: number;
  } | null>(null);
  const [dismissedNotice, setDismissedNotice] = useState<{
    base: DownloadStateDTO | null;
    text: string;
  } | null>(null);
  const [filtersOpen, setFiltersOpen] = useState(false);

  const tasks = st.tasks ?? [];
  const importOpen =
    importOverride && importOverride.base === rawState
      ? importOverride.open
      : Boolean(st.import?.open);
  const guideOpen =
    guideOverride && guideOverride.base === rawState
      ? guideOverride.open
      : Boolean(st.guideOn);
  const composerOpen =
    composerOverride && composerOverride.base === rawState
      ? composerOverride.open
      : Boolean(st.composerOn);

  const activeCategory =
    DOWNLOAD_CATEGORIES.find((c) => c.value === st.filter) ?? DOWNLOAD_CATEGORIES[0];

  const categoryCounts: Record<0 | 1 | 2 | 3, number> = {
    0: st.countOpen,
    1: st.countDone,
    2: st.countCanceled,
    3: st.countAll,
  };

  const searchDraft = useSyncedDraft(
    st.search || "",
    "dl-search",
    (val) => runIpc(() => Download.setDownloadField("search", val)),
    { debounceMs: 90 },
  );

  const openComposer = useCallback(
    (withFilters = false) => {
      if (withFilters) setFiltersOpen(true);
      setComposerOverride({ base: rawState, open: true });
      runIpc(() => Download.setDownloadField("composerOn", "true"));
    },
    [rawState],
  );

  const closeComposer = useCallback(() => {
    setComposerOverride({ base: rawState, open: false });
    runIpc(() => Download.setDownloadField("composerOn", "false"));
  }, [rawState]);

  const selectTask = useCallback(
    (id: number) => {
      setSelectedOverride({ base: rawState, id });
      runIpc(() => Download.setDownloadField("selectedId", String(id)));
    },
    [rawState],
  );

  const closeImportDialog = useCallback(() => {
    setImportOverride({ base: rawState, open: false });
    runIpc(() => Download.downloadImportAction("close", "", 0));
  }, [rawState]);

  const closeGuideDialog = useCallback(() => {
    setGuideOverride({ base: rawState, open: false });
    runIpc(() => Download.openDownloadGuide(false));
  }, [rawState]);

  const activeSelectedId =
    selectedOverride && selectedOverride.base === rawState
      ? selectedOverride.id
      : st.selectedId ?? 0;
  const selectedTask = useMemo(
    () => (activeSelectedId > 0 ? tasks.find((t) => t.id === activeSelectedId) ?? null : null),
    [activeSelectedId, tasks],
  );

  useEscapeHandler("download-import-dialog", importOpen, closeImportDialog, 40);
  useEscapeHandler("download-guide-dialog", guideOpen, closeGuideDialog, 40);
  useEscapeHandler(
    "download-composer-dialog",
    composerOpen && !importOpen && !guideOpen,
    closeComposer,
    40,
  );
  useEscapeHandler(
    "download-inspector",
    Boolean(selectedTask) && !importOpen && !guideOpen && !composerOpen,
    () => selectTask(0),
    10,
  );

  const { isDragging } = useFileDrop({
    enabled: !importOpen && !guideOpen && !composerOpen,
    onDrop: (paths) => {
      if (paths.length === 0) return;
      const { supported, legacyXls } = classifyDroppedSheetPaths(paths);
      if (supported.length > 0) {
        runIpc(() => Download.importDownloadSheet(supported[0]));
        if (supported.length > 1) {
          const firstName = supported[0].split(/[\\/]/).pop() || supported[0];
          showToast(
            `已打开第一个表格「${firstName}」`,
            "info",
            `每次仅支持预览一个表格，已忽略其余 ${supported.length - 1} 个文件`,
          );
        }
      } else if (legacyXls.length > 0) {
        showToast(
          "暂不支持旧版 .xls 格式",
          "warning",
          "请先另存为 .xlsx 或 .csv 后再拖入导入",
        );
      } else {
        showToast(
          "请拖入 .csv 或 .xlsx 表格文件",
          "info",
          "也可点击左上角「新建任务」粘贴链接批量下载",
        );
      }
    },
  });

  const telemetry = useMemo(() => {
    let totalSpeed = 0;
    let activeCount = 0;
    let queuedCount = 0;
    let failedCount = 0;
    let checkedCount = 0;
    let checkedActiveOrQueued = 0;
    let checkedPaused = 0;
    let checkedTerminal = 0;
    let checkedCancelable = 0;

    for (const t of tasks) {
      if (isTaskActive(t)) {
        activeCount += 1;
        totalSpeed += Math.max(0, t.speed || 0);
      } else if (isTaskQueued(t)) {
        queuedCount += 1;
      } else if (isTaskFailed(t)) {
        failedCount += 1;
      }
      if (t.checked) {
        checkedCount += 1;
        if (isTaskActive(t) || isTaskQueued(t)) checkedActiveOrQueued += 1;
        if (isTaskPaused(t)) checkedPaused += 1;
        if (t.terminal) checkedTerminal += 1;
        if (!t.terminal || isTaskFailed(t)) checkedCancelable += 1;
      }
    }

    return {
      totalSpeed,
      activeCount,
      queuedCount,
      failedCount,
      checkedCount,
      checkedActiveOrQueued,
      checkedPaused,
      checkedTerminal,
      checkedCancelable,
    };
  }, [tasks]);

  const listEntries = useMemo<DownloadListEntry[]>(() => {
    if (st.groupBySite) {
      return flattenSiteGroups(tasks);
    }
    return tasks.map((task) => ({ kind: "task", key: task.id, task }));
  }, [st.groupBySite, tasks]);

  if (!rawState && loading) {
    return <PageSkeleton rows={6} />;
  }

  const saveDirLabel = st.resolvedDir || st.dir || "默认下载目录";
  const activeFilterRuleCount = countActiveFilterRules({
    depth: st.depth ?? 1,
    minKb: st.minKb ?? 0,
    maxKb: st.maxKb ?? 0,
    minW: st.minW ?? 0,
    maxW: st.maxW ?? 0,
    minH: st.minH ?? 0,
    maxH: st.maxH ?? 0,
  });
  const hasActiveFilters = (st.depth ?? 1) > 1 || activeFilterRuleCount > 0;

  const rawInlineNotice = !composerOpen
    ? st.composerErr || st.browserErr || st.importNotice || ""
    : st.importNotice || "";
  const inlineNotice =
    dismissedNotice &&
    dismissedNotice.base === rawState &&
    dismissedNotice.text === rawInlineNotice
      ? ""
      : rawInlineNotice;
  const isInlineError = !composerOpen && Boolean(st.composerErr || st.browserErr);
  const isInlineImportOk = !isInlineError && inlineNotice.startsWith("已加入");

  const dismissInlineNotice = () => {
    setDismissedNotice({ base: rawState, text: rawInlineNotice });
    if (isInlineError) {
      if (st.composerErr) {
        runIpc(() => Download.setDownloadField("dismissComposerErr", "1"));
      }
      if (st.browserErr) {
        runIpc(() => Download.setDownloadField("dismissBrowserErr", "1"));
      }
    } else if (st.importNotice) {
      runIpc(() => Download.setDownloadField("dismissImportNotice", "1"));
    }
  };

  const allChecked = tasks.length > 0 && telemetry.checkedCount === tasks.length;
  const masterChecked: boolean | "indeterminate" =
    allChecked ? true : telemetry.checkedCount > 0 ? "indeterminate" : false;

  const toggleAllVisible = (next: boolean) => {
    for (const t of tasks) {
      if (Boolean(t.checked) !== next) {
        runIpc(() => Download.toggleDownloadCheck(t.id, next));
      }
    }
  };

  const runBatchAction = (
    action: "pause" | "resume" | "retry" | "cancel" | "remove",
    predicate?: (t: (typeof tasks)[number]) => boolean,
  ) => {
    for (const t of tasks) {
      if (t.checked && (!predicate || predicate(t))) {
        runIpc(() => Download.downloadRowAction(t.id, action));
      }
    }
  };

  const isTrashTab = st.filter === 2;

  const globalTotalSpeed = Math.max(0, st.totalSpeed ?? telemetry.totalSpeed);
  const globalActiveCount = Math.max(0, st.activeCount ?? telemetry.activeCount);
  const globalQueuedCount = Math.max(0, st.queuedCount ?? telemetry.queuedCount);
  const globalPausedCount = Math.max(0, st.pausedCount ?? 0);
  const globalFailedCount = Math.max(0, st.failedCount ?? telemetry.failedCount);

  return (
    <div className="relative flex h-full min-h-0 flex-1 flex-col bg-paper">
      <DropZoneOverlay
        active={isDragging}
        label="导入 CSV / XLSX 表格"
        sublabel="松开即可直接打开表格预览并配置网址列与命名列"
      />

      {/* 迅雷式顶部悬浮操作栏：新建任务 + 全局队列控制 + 搜索 */}
      <PageToolbar
        left={
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              data-testid="dl-new-task-btn"
              text="新建任务"
              size="small"
              icon="plus"
              variant="primary"
              onClick={() => openComposer(false)}
            />
            <Button
              text="导入表格"
              size="small"
              icon="file-spreadsheet"
              variant="outline"
              onClick={() => runIpc(() => Download.pickDownloadSheet())}
            />
            <Button
              text={
                st.browserBusy
                  ? "启动中..."
                  : st.browserOpen
                    ? "指纹浏览器已打开"
                    : "指纹浏览器"
              }
              size="small"
              icon="globe"
              variant={st.browserOpen ? "secondary" : "outline"}
              busy={Boolean(st.browserBusy)}
              disabled={st.browserSupported === false}
              onClick={() => runIpc(() => Download.openDownloadBrowser(""))}
            />

            <div className="mx-0.5 hidden h-4 w-px bg-hairline sm:block" />

            <Button
              text="全部开始"
              size="small"
              icon="play"
              variant="outline"
              onClick={() => runIpc(() => Download.downloadPauseResumeAll(false))}
            />
            <Button
              text="全部暂停"
              size="small"
              icon="pause"
              variant="outline"
              onClick={() => runIpc(() => Download.downloadPauseResumeAll(true))}
            />
            <Tooltip
              content={
                isTrashTab
                  ? "彻底清空回收站中的任务记录"
                  : "移除已完成和失败的任务记录（不删除本地文件）"
              }
              side="bottom"
            >
              <Button
                text={isTrashTab ? "清空回收站" : "清理已结束"}
                size="small"
                icon="trash-2"
                variant="ghost"
                onClick={() =>
                  runIpc(() => Download.downloadClearTrash(isTrashTab ? "clear" : "prune"))
                }
              />
            </Tooltip>
          </div>
        }
        right={
          <div className="flex items-center gap-1.5">
            <Button
              text="站点说明"
              size="small"
              icon="book-open"
              variant="ghost"
              onClick={() => runIpc(() => Download.openDownloadGuide(true))}
            />
            <Input
              size="small"
              leftIcon="search"
              value={searchDraft.value}
              placeholder="搜索任务文件名或链接..."
              aria-label="搜索下载任务"
              onChange={(e) => searchDraft.onChange(e.target.value)}
              onCompositionStart={searchDraft.onCompositionStart}
              onCompositionEnd={(e) => searchDraft.onCompositionEnd(e.currentTarget.value)}
              onBlur={searchDraft.onBlur}
              wrapperClassName="w-[160px] xl:w-[200px]"
              rightSlot={
                searchDraft.value ? (
                  <IconButton
                    icon="x"
                    size={18}
                    variant="ghost"
                    aria-label="清空搜索"
                    onClick={() => searchDraft.setValue("", true)}
                  />
                ) : undefined
              }
            />
          </div>
        }
      />

      {/* 迅雷经典三栏工作区：左分类栏 + 中央任务队列 + 右侧详情检查器 */}
      <div className="flex min-h-0 flex-1 flex-col gap-3 px-3 pb-3 md:flex-row">
        {/* 窄屏折叠的横向分类条 */}
        <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 rounded-[16px] border border-hairline bg-card p-2 md:hidden bento-shadow">
          <div className="flex flex-wrap items-center gap-1">
            {DOWNLOAD_CATEGORIES.map((cat) => (
              <SegmentedPill
                key={cat.value}
                label={`${cat.label} (${categoryCounts[cat.value]})`}
                icon={cat.icon}
                active={st.filter === cat.value}
                onClick={() => runIpc(() => Download.setDownloadFilter(cat.value))}
              />
            ))}
          </div>
          <span className="tnum px-1.5 text-[11px] font-medium text-accent">
            {formatSpeed(globalTotalSpeed, "0 B/s")}
          </span>
        </div>

        {/* 桌面端左侧分类与速率侧栏 */}
        <BentoCard
          data-testid="dl-sidebar"
          compact
          className="hidden w-[172px] shrink-0 flex-col justify-between gap-3 overflow-y-auto md:flex xl:w-[184px]"
        >
          <div className="flex flex-col gap-1">
            <div className="px-2 pb-1 text-[10.5px] font-semibold tracking-wide text-subtle">
              传输列表
            </div>
            {DOWNLOAD_CATEGORIES.map((cat) => {
              const active = st.filter === cat.value;
              const count = categoryCounts[cat.value];
              return (
                <button
                  key={cat.value}
                  type="button"
                  data-testid={`dl-filter-${cat.value}`}
                  aria-pressed={active}
                  onClick={() => runIpc(() => Download.setDownloadFilter(cat.value))}
                  className={cn(
                    "flex w-full cursor-pointer items-center justify-between gap-2 rounded-[11px] border px-2.5 py-2 text-xs transition-colors duration-150 select-none",
                    active
                      ? "border-accent-border bg-accent-soft font-semibold text-accent"
                      : "border-transparent bg-transparent font-medium text-muted hover:bg-sunken hover:text-ink",
                  )}
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <Icon name={cat.icon} size={14} />
                    <span className="truncate">{cat.label}</span>
                  </span>
                  <span
                    className={cn(
                      "tnum rounded-full px-1.5 py-0.5 text-[10.5px] leading-none",
                      active ? "bg-accent text-on-accent" : "bg-sunken text-muted",
                    )}
                  >
                    {count}
                  </span>
                </button>
              );
            })}
          </div>

          {/* 底部实时速率 + 抓取规则 + 存储路径 */}
          <div className="flex flex-col gap-2 border-t border-divider pt-2.5">
            <div className="flex flex-col gap-1 rounded-[12px] border border-hairline bg-sunken p-2.5">
              <div className="flex items-center justify-between text-[10.5px] text-muted">
                <span>总下载速度</span>
                <span
                  className={cn(
                    "size-1.5 rounded-full",
                    globalActiveCount > 0 ? "animate-pulse bg-accent" : "bg-subtle/40",
                  )}
                />
              </div>
              <div
                data-testid="dl-global-speed"
                className={cn(
                  "tnum text-sm font-semibold",
                  globalTotalSpeed > 0 ? "text-accent" : "text-ink",
                )}
              >
                {formatSpeed(globalTotalSpeed, "0 B/s")}
              </div>
              <div data-testid="dl-global-counts" className="tnum text-[10.5px] text-subtle">
                下载中 {globalActiveCount} · 等待 {globalQueuedCount}
                {globalPausedCount > 0 ? ` · 暂停 ${globalPausedCount}` : ""}
                {globalFailedCount > 0 ? ` · 失败 ${globalFailedCount}` : ""}
              </div>
            </div>

            <button
              type="button"
              onClick={() => openComposer(true)}
              className="flex w-full cursor-pointer items-center justify-between gap-1.5 rounded-[11px] border border-hairline bg-sunken px-2.5 py-1.5 text-left transition-colors hover:bg-sunken-hover"
            >
              <span className="flex items-center gap-1.5 text-[11px] text-muted">
                <Icon name="sliders-horizontal" size={12} />
                <span>过滤规则</span>
              </span>
              <Badge
                text={
                  activeFilterRuleCount > 0
                    ? `深度 ${st.depth} · ${activeFilterRuleCount}项`
                    : `深度 ${st.depth}`
                }
                variant={hasActiveFilters ? "accent" : "neutral"}
                className="tnum px-1.5 py-0 text-[10px]"
              />
            </button>

            <div className="flex flex-col gap-1.5 rounded-[12px] border border-hairline bg-sunken p-2.5">
              <div className="flex items-center justify-between gap-1">
                <span className="text-[10.5px] font-medium text-muted">保存至</span>
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    onClick={() => runIpc(() => Download.downloadPickDir())}
                    className="cursor-pointer text-[10.5px] font-medium text-accent hover:underline"
                  >
                    更改
                  </button>
                  <span className="text-[10px] text-subtle">·</span>
                  <button
                    type="button"
                    onClick={() => runIpc(() => Download.downloadOpenDir())}
                    className="cursor-pointer text-[10.5px] font-medium text-muted hover:text-ink"
                  >
                    打开
                  </button>
                </div>
              </div>
              <Tooltip content={saveDirLabel} side="top">
                <div
                  tabIndex={0}
                  className="font-mono-console line-clamp-2 break-all rounded text-[10.5px] leading-snug text-ink select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                >
                  {saveDirLabel}
                </div>
              </Tooltip>
            </div>
          </div>
        </BentoCard>

        {/* 中央任务列表主体 */}
        <FlushPane data-testid="dl-queue" className="@container min-h-0 flex-1">
          {/* 列表头部：全选 + 当前视图标题 + 勾选批量操作 + 按站点分组 */}
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-divider bg-card px-4 py-2.5">
            <div className="flex min-w-0 flex-wrap items-center gap-2.5">
              <Checkbox
                data-testid="dl-select-all"
                size="small"
                checked={masterChecked}
                disabled={tasks.length === 0}
                aria-label="全选当前列表任务"
                onCheckedChange={toggleAllVisible}
              />

              <div className="flex items-baseline gap-2">
                <span className="text-[13px] font-semibold text-ink">
                  {activeCategory.label}
                </span>
                <span className="tnum text-[11px] text-muted">
                  共 {tasks.length} 项
                </span>
              </div>

              {telemetry.checkedCount > 0 ? (
                <div
                  data-testid="dl-batch-bar"
                  className="animate-tulai-banner-in flex flex-wrap items-center gap-1 rounded-[10px] border border-accent-border bg-accent-soft px-2 py-0.5"
                >
                  <span className="tnum pr-1 text-[11px] font-medium text-accent">
                    已选 {telemetry.checkedCount}
                  </span>
                  {telemetry.checkedPaused > 0 ? (
                    <Button
                      text="开始"
                      size="xs"
                      variant="primary"
                      onClick={() => runBatchAction("resume", isTaskPaused)}
                    />
                  ) : null}
                  {telemetry.checkedActiveOrQueued > 0 ? (
                    <Button
                      text="暂停"
                      size="xs"
                      variant="outline"
                      onClick={() =>
                        runBatchAction("pause", (t) => isTaskActive(t) || isTaskQueued(t))
                      }
                    />
                  ) : null}
                  {telemetry.checkedTerminal > 0 ? (
                    <Button
                      text="重试"
                      size="xs"
                      variant="outline"
                      onClick={() => runBatchAction("retry", (t) => t.terminal)}
                    />
                  ) : null}
                  {telemetry.checkedCancelable > 0 ? (
                    <Button
                      text="移入回收站"
                      size="xs"
                      variant="outline"
                      onClick={() =>
                        runBatchAction("cancel", (t) => !t.terminal || isTaskFailed(t))
                      }
                    />
                  ) : null}
                  <Button
                    text="移除"
                    size="xs"
                    variant="ghost"
                    onClick={() => runBatchAction("remove")}
                  />
                </div>
              ) : null}
            </div>

            <div className="flex items-center gap-1.5">
              <PillToggle
                data-testid="dl-group-by-site"
                label="按站点分组"
                active={Boolean(st.groupBySite)}
                onClick={() =>
                  runIpc(() =>
                    Download.setDownloadField("groupBySite", st.groupBySite ? "false" : "true"),
                  )
                }
                className="rounded-[9px] px-2.5 py-1 text-[11.5px]"
              />
            </div>
          </div>

          {/* 队列内联错误/导入提示条 */}
          {inlineNotice ? (
            <div
              data-testid="dl-composer-notice"
              className={cn(
                "animate-tulai-banner-in mx-3 mt-2.5 flex shrink-0 items-start justify-between gap-2 rounded-[12px] border px-3 py-2 text-xs select-text",
                isInlineError
                  ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
                  : isInlineImportOk
                    ? "border-[var(--status-ok-border)] bg-[var(--status-ok-bg)] text-status-ok"
                    : "border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] text-status-warn",
              )}
            >
              <span className="min-w-0 flex-1 break-words">{inlineNotice}</span>
              <div className="flex shrink-0 items-center gap-1">
                {isInlineError &&
                st.browserReady === false &&
                st.browserSupported !== false &&
                !st.browserInstallBusy ? (
                  <Button
                    text="前往设置安装"
                    size="xs"
                    variant="outline"
                    onClick={() => {
                      goTab(TAB_SETTINGS);
                      updateSettingField("tab", "2");
                    }}
                  />
                ) : isInlineError &&
                  st.browserErr &&
                  st.browserReady !== false &&
                  st.browserSupported !== false &&
                  !st.browserBusy ? (
                  <Button
                    text="重试打开"
                    size="xs"
                    variant="outline"
                    onClick={() => runIpc(() => Download.openDownloadBrowser(""))}
                  />
                ) : null}
                <IconButton
                  icon="x"
                  size={20}
                  variant="ghost"
                  aria-label="关闭提示"
                  onClick={dismissInlineNotice}
                />
              </div>
            </div>
          ) : null}

          {/* 迅雷式列头：基于队列容器宽度自适应，避免打开右侧详情栏时挤压折叠 */}
          {tasks.length > 0 ? (
            <div
              data-testid="dl-table-header"
              className="flex shrink-0 items-center gap-2.5 border-b border-divider bg-card-header px-6 py-2 text-[11px] font-medium text-muted"
            >
              <div className="min-w-0 flex-1 truncate whitespace-nowrap">任务名称 / 进度</div>
              <div className="hidden w-[92px] shrink-0 text-right whitespace-nowrap @[600px]:block">
                大小 / 数量
              </div>
              <div className="hidden w-[96px] shrink-0 text-right whitespace-nowrap @[740px]:block">
                速度 / 剩余
              </div>
              <div className="hidden w-[64px] shrink-0 text-center whitespace-nowrap @[480px]:block">
                状态
              </div>
              <div className="w-auto shrink-0 text-right whitespace-nowrap @[480px]:w-[104px]">
                操作
              </div>
            </div>
          ) : null}

          {/* 虚拟滚动任务列表 */}
          <VirtualList
            data-testid="download-task-list"
            items={listEntries}
            estimateSize={(idx) => (listEntries[idx]?.kind === "group" ? 32 : 70)}
            gap={6}
            className="min-h-0 flex-1 p-3"
            getItemKey={(entry) => entry.key}
            renderItem={(entry) => {
              if (entry.kind === "group") {
                const iconUrl = getSiteIconUrl(entry.site);
                return (
                  <div
                    data-testid={`dl-site-group-${entry.site}`}
                    className="flex items-center justify-between gap-2 rounded-[10px] border border-divider bg-card-header px-3 py-1.5 text-xs"
                  >
                    <div className="flex min-w-0 items-center gap-2 font-semibold text-ink">
                      {iconUrl ? (
                        <span className="flex size-4 shrink-0 items-center justify-center overflow-hidden rounded-[4px] bg-white p-px">
                          <img
                            src={iconUrl}
                            alt=""
                            loading="lazy"
                            decoding="async"
                            className="size-full object-contain"
                          />
                        </span>
                      ) : (
                        <Icon name="globe" size={13} className="shrink-0 text-accent" />
                      )}
                      <span className="truncate">{entry.site}</span>
                    </div>
                    <span className="tnum shrink-0 text-[11px] text-muted">
                      共 {entry.total} 项
                      {entry.active > 0 ? ` · 下载中 ${entry.active}` : ""}
                      {entry.done > 0 ? ` · 已完成 ${entry.done}` : ""}
                      {entry.failed > 0 ? (
                        <span className="text-status-danger"> · 失败 {entry.failed}</span>
                      ) : null}
                    </span>
                  </div>
                );
              }
              const t = entry.task;
              return (
                <DownloadTaskRow
                  task={t}
                  selected={selectedTask?.id === t.id}
                  browserSupported={st.browserSupported !== false}
                  onSelect={selectTask}
                />
              );
            }}
            empty={
              <EmptyNotice
                icon={activeCategory.icon}
                title={st.search ? "未找到匹配的下载任务" : activeCategory.emptyTitle}
                subtitle={
                  st.search
                    ? "尝试更换关键词，或点击右上角清空搜索"
                    : activeCategory.emptySubtitle
                }
                action={
                  st.search ? (
                    <Button
                      text="清空搜索"
                      size="small"
                      variant="outline"
                      onClick={() => searchDraft.setValue("", true)}
                    />
                  ) : st.filter === 0 || st.filter === 3 ? (
                    <div className="flex flex-wrap items-center justify-center gap-2">
                      <Button
                        text="新建任务"
                        size="small"
                        icon="plus"
                        variant="primary"
                        onClick={() => openComposer(false)}
                      />
                      <Button
                        text="导入表格"
                        size="small"
                        icon="file-spreadsheet"
                        variant="outline"
                        onClick={() => runIpc(() => Download.pickDownloadSheet())}
                      />
                    </div>
                  ) : undefined
                }
              />
            }
          />

          {/* 迅雷底部实时状态栏 */}
          <div className="tnum flex h-8 shrink-0 items-center justify-between gap-2 border-t border-divider bg-card-header px-4 text-[11px] text-muted">
            <span className="min-w-0 truncate">
              全部 {st.countAll} · 正在下载 {st.countOpen} · 已完成 {st.countDone} · 回收站{" "}
              {st.countCanceled}
            </span>
            <div className="flex shrink-0 items-center gap-3">
              {st.browserOpen ? (
                <span className="hidden items-center gap-1.5 whitespace-nowrap text-status-ok @[480px]:inline-flex">
                  <span className="size-1.5 rounded-full bg-status-ok" />
                  <span>指纹浏览器运行中</span>
                </span>
              ) : null}
              {st.browserMode ? (
                <span className="hidden items-center gap-1 whitespace-nowrap text-accent @[640px]:inline-flex">
                  <Icon name="globe" size={11} />
                  <span>指纹浏览器</span>
                </span>
              ) : null}
              <span className="font-medium whitespace-nowrap text-ink">
                总速度 {formatSpeed(globalTotalSpeed, "0 B/s")}
              </span>
            </div>
          </div>
        </FlushPane>

        {/* 选中任务时展开的迅雷详情面板 */}
        {selectedTask ? (
          <DownloadInspector
            task={selectedTask}
            defaultDir={saveDirLabel}
            browserSupported={st.browserSupported !== false}
            browserBusy={Boolean(st.browserBusy)}
            onClose={() => selectTask(0)}
          />
        ) : null}
      </div>

      {/* 弹窗 1：迅雷式新建下载任务 */}
      <DownloadComposerDialog
        open={composerOpen}
        st={st}
        saveDirLabel={saveDirLabel}
        filtersOpen={filtersOpen}
        hasActiveFilters={hasActiveFilters}
        closeOnEscape={!importOpen && !guideOpen}
        onToggleFilters={() => setFiltersOpen((v) => !v)}
        onClose={closeComposer}
      />

      {/* 弹窗 2：表格导入预览 */}
      <DownloadImportDialog open={importOpen} st={st} onClose={closeImportDialog} />

      {/* 弹窗 3：支持站点说明 */}
      <DownloadGuideDialog open={guideOpen} st={st} onClose={closeGuideDialog} />
    </div>
  );
}
