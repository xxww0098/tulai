import { cleanup, computed, createMotion, effect, Image, Scroll, signal, Text, View } from "nsgo";
import {
  Badge,
  Button,
  ContextMenu,
  Icon,
  IconButton,
  Menu,
  MenuItem,
  MenuSeparator,
  Spinner,
  Tabs,
} from "nsgo/ui";
import {
  type ResultTileDTO,
  openOutputFolder,
  openPath,
  replaceResultImage,
  retryAllFailed,
  retryTask,
  revealPath,
  setPreviewPath,
  setResultsDoneOpen,
} from "tulai/backend";
import { requeueWorkbenchDone, undoWorkbenchRequeue, workbench } from "../store";
import { clearHoverPreview, HoverPreviewTarget } from "./HoverPreview";
import { ConveyorFlowTrack, LivePulseDot } from "./MotionPrimitives";
import { EmptyNotice } from "./primitives";
import { WashBotCard } from "./WashBotStation";

const DENSITY_OPTIONS = [
  { value: "sm", label: "小图" },
  { value: "md", label: "中图" },
];

const PENDING_GRID_STYLE = {
  display: "grid" as const,
  gridTemplateColumns: "repeat(auto-fill, minmax(68px, 1fr))",
  gap: 6,
};

const ISSUE_GRID_STYLE = {
  display: "grid" as const,
  gridTemplateColumns: "repeat(auto-fill, minmax(112px, 1fr))",
  gap: 8,
};

const WASHBOT_GRID_STYLE = {
  display: "grid" as const,
  gridTemplateColumns: "repeat(auto-fill, minmax(284px, 1fr))",
  gap: 8,
};

const DONE_GRID_SM_STYLE = {
  display: "grid" as const,
  gridTemplateColumns: "repeat(auto-fill, minmax(104px, 1fr))",
  gap: 8,
};

const DONE_GRID_MD_STYLE = {
  display: "grid" as const,
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

function TileMenuItems(props: { tile: ResultTileDTO }) {
  return (
    <>
      <If when={Boolean(props.tile.path)}>
        <MenuItem
          text="大图预览"
          icon="eye"
          onSelect={() => {
            clearHoverPreview();
            if (props.tile.path) setPreviewPath(props.tile.path);
          }}
        />
        <MenuItem
          text="系统默认打开"
          icon="external-link"
          onSelect={() => {
            if (props.tile.path) void openPath(props.tile.path);
          }}
        />
        <MenuItem
          text="在文件夹中显示"
          icon="folder-open"
          onSelect={() => {
            if (props.tile.path) void revealPath(props.tile.path);
          }}
        />
      </If>
      <If when={Boolean(props.tile.canRewash)}>
        <If when={Boolean(props.tile.path)}>
          <MenuSeparator />
        </If>
        <MenuItem
          text="放回待洗队列（二次清洗）"
          icon="rotate-ccw"
          onSelect={() => {
            clearHoverPreview();
            requeueWorkbenchDone([props.tile.taskIndex]);
          }}
        />
      </If>
      <If when={Boolean(props.tile.canReplace)}>
        <If when={Boolean(props.tile.path || props.tile.canRewash)}>
          <MenuSeparator />
        </If>
        <MenuItem
          text="替换原图"
          icon="replace"
          onSelect={() => void replaceResultImage(props.tile.taskIndex)}
        />
      </If>
      <If when={Boolean(props.tile.actionText)}>
        <If when={Boolean(props.tile.path || props.tile.canReplace || props.tile.canRewash)}>
          <MenuSeparator />
        </If>
        <MenuItem
          text={props.tile.actionText!}
          icon="refresh-cw"
          onSelect={() => retryTask(props.tile.taskIndex)}
        />
      </If>
    </>
  );
}

function ResultTileCard(props: {
  tile: ResultTileDTO;
  kind?: "done" | "issue";
  selectMode?: boolean;
  selected?: boolean;
  onToggleSelect?: () => void;
}) {
  const hovered = signal(false);
  const enter = createMotion({ enter: "fadeIn", duration: 200 });
  const imgSrc = () => props.tile.fileURL || props.tile.srcURL || "";
  const hasMenu = () => Boolean(props.tile.path || props.tile.canReplace || props.tile.canRewash);
  const hasContextActions = () => Boolean(hasMenu() || props.tile.actionText);
  const subtitleText = () =>
    [props.tile.accountName || props.tile.platform, props.tile.elapsed].filter(Boolean).join(" · ");

  const cardBody = (
    <HoverPreviewTarget
      previewKey={`tile-${props.kind || "done"}-${props.tile.taskIndex}-${props.tile.path || ""}`}
      src={imgSrc()}
      title={props.tile.title || `任务 ${formatOrderNo(props.tile.taskIndex)}`}
      subtitle={subtitleText()}
      badge={props.kind === "issue" ? props.tile.status || "异常" : "已出图"}
      badgeVariant={props.kind === "issue" ? "warn" : "success"}
      onMouseEnterExtra={() => hovered.set(true)}
      onMouseLeaveExtra={() => hovered.set(false)}
      class="min-w-0"
    >
      <View
        testID={`result-tile-card-${props.tile.taskIndex}`}
        style={{ opacity: enter.opacity }}
        hoverStyle={{ transform: [{ translateY: -1 }] }}
        pressedStyle={{ transform: [{ scale: 0.99 }] }}
        class="overflow-hidden rounded-md border transition duration-150"
        classList={{
          "border-blue-500 bg-blue-50/35 dark:border-blue-400 dark:bg-blue-500/10": Boolean(
            props.selected,
          ),
          "border-[#E5E9F0] bg-white hover:border-[#CBD5E1] dark:border-[#222B3C] dark:bg-[#131924] dark:hover:border-[#334155]":
            !props.selected,
        }}
      >
        {/* Square compact artwork stage */}
        <View
          class="relative w-full overflow-hidden bg-slate-50/70 dark:bg-[#0D1118]"
          style={{ aspectRatio: 1 }}
        >
          <View
            onPress={() => {
              clearHoverPreview();
              if (props.selectMode && props.kind === "done" && props.onToggleSelect) {
                props.onToggleSelect();
                return;
              }
              const p = props.tile.path;
              if (p) setPreviewPath(p);
            }}
            style={{ cursor: props.tile.path || props.selectMode ? "pointer" : "default" }}
            class="h-full w-full items-center justify-center p-1"
          >
            <If
              when={Boolean(imgSrc())}
              else={<Icon name="image" size={22} color="#94A3B8" />}
            >
              <Image
                src={imgSrc()}
                fadeDuration={180}
                class="h-full w-full object-contain"
                testID={`result-tile-image-${props.tile.taskIndex}`}
              />
            </If>
          </View>

          {/* Top-left corner selection checkbox (done tiles) */}
          <If when={props.kind === "done" && Boolean(props.selectMode || props.selected || hovered())}>
            <View
              testID={`done-select-${props.tile.taskIndex}`}
              role="checkbox"
              accessibilityState={{ checked: Boolean(props.selected) }}
              accessibilityLabel={`选择结果 ${props.tile.title || props.tile.taskIndex}`}
              onPress={() => {
                clearHoverPreview();
                props.onToggleSelect?.();
              }}
              style={{ position: "absolute", top: 4, left: 4, width: 18, height: 18, cursor: "pointer" }}
              class="items-center justify-center rounded border"
              classList={{
                "border-blue-600 bg-blue-600": Boolean(props.selected),
                "border-slate-300 bg-white/95 dark:border-slate-500 dark:bg-slate-800/95": !props.selected,
              }}
            >
              <If when={Boolean(props.selected)}>
                <Icon name="check" size={11} color="#FFFFFF" />
              </If>
            </View>
          </If>

          {/* Top-left issue badge on issue tiles */}
          <If when={props.kind === "issue"}>
            <View style={{ position: "absolute", top: 4, left: 4 }}>
              <Badge
                text={props.tile.status || "-"}
                variant={tileBadgeVariant(props.tile.status)}
              />
            </View>
          </If>

          {/* Top-right compact quick-rewash + preview trigger on done tiles */}
          <If when={props.kind === "done" && Boolean(props.tile.canRewash || (hovered() && props.tile.path))}>
            <View
              style={{
                position: "absolute",
                top: 4,
                right: 4,
                opacity: hovered() ? 1 : 0.8,
              }}
              class="flex-row items-center gap-0.5 rounded-md bg-slate-900/75 px-0.5 py-0.5"
            >
              <If when={Boolean(props.tile.canRewash)}>
                <IconButton
                  testID={`rewash-tile-${props.tile.taskIndex}`}
                  icon="rotate-ccw"
                  size={20}
                  iconSize={11}
                  variant="ghost"
                  tint="#F8FAFC"
                  tooltip="放回待洗队列（二次清洗）"
                  accessibilityLabel={`二次清洗 ${props.tile.title || props.tile.taskIndex}`}
                  onPress={() => {
                    clearHoverPreview();
                    requeueWorkbenchDone([props.tile.taskIndex]);
                  }}
                />
              </If>
              <If when={hovered() && Boolean(props.tile.path)}>
                <IconButton
                  icon="eye"
                  size={20}
                  iconSize={11}
                  variant="ghost"
                  tint="#F8FAFC"
                  tooltip="大图预览"
                  accessibilityLabel="大图预览"
                  onPress={() => {
                    clearHoverPreview();
                    if (props.tile.path) setPreviewPath(props.tile.path);
                  }}
                />
              </If>
            </View>
          </If>
        </View>

        {/* Minimal compact footer: filename + tabular meta on left, single menu / retry on right */}
        <View class="flex-row items-center justify-between gap-1 border-t border-[#E5E9F0] px-2 py-1 dark:border-[#222B3C]">
          <View class="min-w-0 flex-1 gap-0.5">
            <Text class="truncate text-[11px] font-medium text-slate-800 dark:text-slate-100">
              {props.tile.title || `任务 ${formatOrderNo(props.tile.taskIndex)}`}
            </Text>
            <If when={Boolean(props.tile.elapsed || props.tile.platform)}>
              <Text
                class="truncate text-[10px] text-slate-400 dark:text-slate-500"
                style={{ fontVariant: ["tabular-nums"] }}
              >
                {[props.tile.elapsed, props.tile.platform].filter(Boolean).join(" · ")}
              </Text>
            </If>
            <If when={Boolean(props.tile.reason)}>
              <Text class="truncate text-[10px] text-rose-600 dark:text-rose-400">
                {props.tile.reason}
              </Text>
            </If>
          </View>

          <View class="shrink-0 flex-row items-center gap-0.5">
            <If when={Boolean(props.tile.actionText)}>
              <Button
                text={props.tile.actionText}
                size="small"
                variant="outline"
                onPress={() => retryTask(props.tile.taskIndex)}
              />
            </If>
            <If when={hasMenu()}>
              <Menu
                testID={`result-tile-menu-${props.tile.taskIndex}`}
                placement="bottom"
                align="end"
                accessibilityLabel="图片操作"
                trigger={{ icon: "ellipsis", variant: "ghost", size: "small", label: "更多操作" }}
              >
                <TileMenuItems tile={props.tile} />
              </Menu>
            </If>
          </View>
        </View>
      </View>
    </HoverPreviewTarget>
  );

  return (
    <View class="min-w-0">
      <ContextMenu disabled={!hasContextActions()} trigger={cardBody}>
        <TileMenuItems tile={props.tile} />
      </ContextMenu>
    </View>
  );
}

function PendingQueueTile(props: { tile: ResultTileDTO; index?: number }) {
  const enter = createMotion({
    enter: "fadeIn",
    duration: 200,
    delay: Math.min((props.index ?? 0) * 24, 160),
  });
  return (
    <HoverPreviewTarget
      previewKey={`pending-${props.tile.taskIndex}-${props.tile.srcURL || ""}`}
      src={props.tile.srcURL}
      title={props.tile.title || `任务 ${formatOrderNo(props.tile.taskIndex)}`}
      subtitle={
        (props.tile.rewashCount ?? 0) > 0
          ? `二次清洗（第 ${(props.tile.rewashCount ?? 0) + 1} 次排队）`
          : "按序号派发至空闲洗图机器人"
      }
      badge={(props.tile.rewashCount ?? 0) > 0 ? "二次洗" : "等待中"}
      badgeVariant={(props.tile.rewashCount ?? 0) > 0 ? "warn" : "neutral"}
      class="min-w-0"
    >
      <View
        testID={`pending-tile-${props.tile.taskIndex}`}
        style={{ opacity: enter.opacity }}
        hoverStyle={{ transform: [{ translateY: -1 }] }}
        class="overflow-hidden rounded-md border border-dashed border-[#CBD5E1] bg-white transition duration-150 dark:border-[#334155] dark:bg-[#131924]"
      >
        <View
          class="relative w-full overflow-hidden bg-slate-100/70 dark:bg-[#0D1118]"
          style={{ aspectRatio: 1 }}
        >
          <View class="h-full w-full items-center justify-center p-1">
            <If
              when={Boolean(props.tile.srcURL)}
              else={
                <View class="items-center gap-1 px-1">
                  <Icon name="clock" size={14} color="#94A3B8" />
                  <If when={Boolean(props.tile.prompt)}>
                    <Text
                      style={{ numberOfLines: 2 }}
                      class="text-center text-[9.5px] text-slate-500 dark:text-slate-400"
                    >
                      {props.tile.prompt}
                    </Text>
                  </If>
                </View>
              }
            >
              <Image
                src={props.tile.srcURL!}
                fadeDuration={160}
                class="h-full w-full object-contain opacity-90"
              />
            </If>
          </View>

          <View
            style={{ position: "absolute", top: 3, left: 3 }}
            class="rounded bg-slate-900/75 px-1 py-0.5"
          >
            <Text
              class="text-[9.5px] font-semibold text-white"
              style={{ fontVariant: ["tabular-nums"] }}
            >
              {formatOrderNo(props.tile.taskIndex)}
            </Text>
          </View>

          <View
            style={{ position: "absolute", top: 3, right: 3 }}
            class="flex-row items-center gap-1 rounded bg-white/90 px-1 py-0.5 dark:bg-slate-800/90"
          >
            <View class="size-1.5 rounded-full bg-slate-400 animate-pulse" />
            <Text class="text-[9.5px] font-medium text-slate-600 dark:text-slate-300">
              等待
            </Text>
          </View>

          <If when={(props.tile.rewashCount ?? 0) > 0}>
            <View
              style={{ position: "absolute", bottom: 3, left: 3 }}
              class="rounded bg-amber-600/95 px-1 py-0.5"
            >
              <Text class="text-[9px] font-semibold text-white">二次洗</Text>
            </View>
          </If>
        </View>

        <Text class="truncate border-t border-[#E5E9F0] px-1.5 py-0.5 text-[10px] text-slate-600 dark:border-[#222B3C] dark:text-slate-400">
          {props.tile.title || `任务 ${formatOrderNo(props.tile.taskIndex)}`}
        </Text>
      </View>
    </HoverPreviewTarget>
  );
}

export function ResultGrid() {
  const wb = workbench;
  const density = signal<"sm" | "md">("sm");
  const botAnimPaused = signal(false);
  const doneLimit = signal(30);
  const pendingExpanded = signal(false);
  const doneCollapsed = signal(false);
  const selectMode = signal(false);
  const selectedDone = signal<number[]>([]);
  const celebrating = signal(false);

  let prevDoneCount = wb().doneTiles?.length ?? 0;
  let celebrateTimer: ReturnType<typeof setTimeout> | undefined;

  cleanup(() => {
    if (celebrateTimer !== undefined) {
      clearTimeout(celebrateTimer);
      celebrateTimer = undefined;
    }
  });

  const activeRows = () => wb().activeRows ?? [];
  const pendingTiles = () => wb().pendingTiles ?? [];
  const issueRows = () => wb().issueRows ?? [];
  const doneTiles = () => wb().doneTiles ?? [];
  const stagedSources = () => wb().sources ?? [];
  const total = () => wb().totalCount;

  const activeTiles = computed(() => {
    const out: ResultTileDTO[] = [];
    let rowAcct = "";
    for (const r of activeRows()) {
      if (r.header && r.title) rowAcct = r.title;
      for (const t of r.tiles ?? []) {
        out.push(t.accountName ? t : { ...t, accountName: rowAcct });
      }
    }
    return out;
  });

  const issueTiles = computed(() => {
    const out: ResultTileDTO[] = [];
    for (const r of issueRows()) {
      for (const t of r.tiles ?? []) out.push(t);
    }
    return out;
  });

  const recentDoneURL = computed(() => {
    for (const t of doneTiles()) {
      const u = t.fileURL || t.srcURL;
      if (u) return u;
    }
    return "";
  });

  effect(() => {
    const list = doneTiles();
    const validIndices = new Set(list.map((t) => t.taskIndex));
    const currentSel = selectedDone();
    const filtered = currentSel.filter((idx) => validIndices.has(idx));
    if (filtered.length !== currentSel.length) {
      selectedDone.set(filtered);
    }
    if (list.length === 0 && selectMode()) {
      selectMode.set(false);
    }
    if (list.length > prevDoneCount) {
      celebrating.set(true);
      if (celebrateTimer !== undefined) clearTimeout(celebrateTimer);
      celebrateTimer = setTimeout(() => {
        celebrateTimer = undefined;
        celebrating.set(false);
      }, 2200);
    }
    prevDoneCount = list.length;
  });

  const showDone = () =>
    !doneCollapsed() &&
    (Boolean(wb().resultsDoneOpen) ||
      (activeRows().length === 0 && pendingTiles().length === 0 && issueRows().length === 0));

  const hasPipelineWork = () =>
    total() > 0 ||
    activeRows().length > 0 ||
    pendingTiles().length > 0 ||
    issueRows().length > 0 ||
    doneTiles().length > 0;

  const visiblePending = () =>
    pendingExpanded() ? pendingTiles() : pendingTiles().slice(0, 18);

  const selectedSet = computed(() => new Set(selectedDone()));
  const selectableDoneIndices = computed(() =>
    doneTiles().filter((t) => Boolean(t.canRewash)).map((t) => t.taskIndex),
  );

  const toggleDoneSelect = (taskIndex: number) => {
    const set = new Set(selectedDone());
    if (set.has(taskIndex)) set.delete(taskIndex);
    else set.add(taskIndex);
    selectedDone.set(Array.from(set));
  };

  return (
    <View testID="wb-result-gallery" class="min-h-0 flex-1 bg-[#F8FAFC] dark:bg-[#0D1118]">
      {/* Pinned Engineering Pipeline Header */}
      <View class="shrink-0 flex-row flex-wrap items-center justify-between gap-2 px-3 pt-2 pb-1.5">
        <View class="min-w-0 flex-row flex-wrap items-center gap-1.5">
          <Text class="text-[13px] font-semibold text-slate-900 dark:text-slate-100">
            {wb().mode === "wash" || !wb().mode ? "洗图流水线" : "加工流水线"}
          </Text>
          <If when={wb().activeCount > 0}>
            <View class="flex-row items-center gap-1 rounded-full border border-blue-200/90 bg-blue-50 px-2 py-0.5 dark:border-blue-500/35 dark:bg-blue-500/15">
              <Spinner size={10} tint="#2563EB" />
              <Text
                class="text-[11px] font-semibold text-blue-700 dark:text-blue-300"
                style={{ fontVariant: ["tabular-nums"] }}
              >
                {`洗图中 ${wb().activeCount}`}
              </Text>
            </View>
          </If>
          <If when={pendingTiles().length > 0}>
            <View class="rounded-full border border-[#E5E9F0] bg-white px-2 py-0.5 dark:border-[#222B3C] dark:bg-[#131924]">
              <Text
                class="text-[11px] font-medium text-slate-600 dark:text-slate-300"
                style={{ fontVariant: ["tabular-nums"] }}
              >
                {`等待中 ${pendingTiles().length}`}
              </Text>
            </View>
          </If>
          <If when={wb().failedCount + wb().needsCount > 0}>
            <Badge
              text={`需处理 ${wb().failedCount + wb().needsCount}`}
              variant="warn"
            />
          </If>
          <If when={doneTiles().length > 0}>
            <Badge text={`已出图 ${doneTiles().length}`} variant="success" />
          </If>
          <If
            when={
              total() > 0 &&
              doneTiles().length === 0 &&
              wb().activeCount === 0 &&
              pendingTiles().length === 0 &&
              wb().failedCount + wb().needsCount === 0
            }
          >
            <Text
              class="text-xs text-slate-500 dark:text-slate-400"
              style={{ fontVariant: ["tabular-nums"] }}
            >
              {`共 ${total()} 个任务`}
            </Text>
          </If>
        </View>

        <View class="shrink-0 flex-row flex-wrap items-center gap-1.5">
          <If when={doneTiles().length > 0}>
            <Tabs
              testID="wb-density-tabs"
              variant="segmented"
              size="small"
              options={DENSITY_OPTIONS}
              value={density()}
              onChange={({ value }) => density.set(value === "md" ? "md" : "sm")}
            />
          </If>
          <If when={Boolean(wb().canUndoRewash)}>
            <Button
              testID="wb-undo-rewash-btn"
              text={`撤销放回 (${wb().rewashUndoCount || 1})`}
              size="small"
              icon="undo-2"
              variant="outline"
              onPress={() => undoWorkbenchRequeue()}
            />
          </If>
          <If when={wb().failedCount + wb().needsCount > 0}>
            <Button
              text="重试失败"
              size="small"
              icon="refresh-cw"
              variant="outline"
              onPress={() => retryAllFailed()}
            />
          </If>
          <Button
            text="打开输出目录"
            size="small"
            icon="folder-open"
            variant="outline"
            onPress={() => void openOutputFolder()}
          />
          <If when={doneTiles().length > 0}>
            <Button
              text={showDone() ? "收起已完成" : "展开已完成"}
              size="small"
              variant="ghost"
              onPress={() => {
                const collapse = showDone();
                doneCollapsed.set(collapse);
                setResultsDoneOpen(!collapse);
              }}
            />
          </If>
        </View>
      </View>

      {/* Live Conveyor Pipeline Rail */}
      <If when={Boolean(wb().running || wb().activeCount > 0 || pendingTiles().length > 0)}>
        <View class="shrink-0 px-3 pb-1.5">
          <ConveyorFlowTrack
            active={Boolean(wb().running)}
            activeCount={wb().activeCount}
            pendingCount={pendingTiles().length}
            doneCount={doneTiles().length}
          />
        </View>
      </If>

      <Scroll class="min-h-0 flex-1" contentClass="gap-3.5 px-3 pb-3.5">
        <If
          when={hasPipelineWork()}
          else={
            <If
              when={stagedSources().length > 0}
              else={
                <EmptyNotice
                  icon="sparkles"
                  title="暂无生成结果"
                  subtitle="在左侧配置提示词与参考图后，点击左下角开始按钮生成图片"
                />
              }
            >
              <View
                testID="wb-staging-queue"
                class="gap-2 rounded-md border border-dashed border-[#CBD5E1] bg-white p-3 dark:border-[#334155] dark:bg-[#131924]"
              >
                <View class="flex-row items-center justify-between gap-2">
                  <View class="flex-row items-center gap-2">
                    <View class="size-2 rounded-full bg-slate-400" />
                    <Text
                      class="text-xs font-semibold text-slate-700 dark:text-slate-200"
                      style={{ fontVariant: ["tabular-nums"] }}
                    >
                      {`待启动加工队列 (${wb().sourcesTotal || stagedSources().length} 张待洗)`}
                    </Text>
                  </View>
                  <Text class="text-[11px] text-slate-400 dark:text-slate-500">
                    点击左下角开始按钮进入洗图机器人流水线
                  </Text>
                </View>
                <View style={PENDING_GRID_STYLE}>
                  <Each
                    of={stagedSources().slice(0, 12)}
                    key={(s) => s.path || String(s.index)}
                  >
                    {(item) => (
                      <HoverPreviewTarget
                        previewKey={`staging-${item().index}-${item().path || ""}`}
                        src={item().fileURL}
                        title={item().name || `素材 ${formatOrderNo(item().index + 1)}`}
                        subtitle="待启动洗图"
                        badge="待启动"
                        badgeVariant="neutral"
                        class="min-w-0"
                      >
                        <View class="overflow-hidden rounded-md border border-dashed border-[#CBD5E1] bg-slate-50/70 dark:border-[#334155] dark:bg-[#1A2232]">
                          <View class="relative w-full items-center justify-center p-1" style={{ aspectRatio: 1 }}>
                            <If
                              when={Boolean(item().fileURL)}
                              else={<Icon name="image" size={14} color="#94A3B8" />}
                            >
                              <Image src={item().fileURL!} fadeDuration={150} class="h-full w-full object-contain" />
                            </If>
                            <View style={{ position: "absolute", top: 3, left: 3 }} class="rounded bg-slate-900/75 px-1 py-0.5">
                              <Text class="text-[9px] font-semibold text-white" style={{ fontVariant: ["tabular-nums"] }}>
                                {formatOrderNo(item().index + 1)}
                              </Text>
                            </View>
                          </View>
                          <Text class="truncate border-t border-[#E5E9F0] px-1 py-0.5 text-[10px] text-slate-600 dark:border-[#222B3C] dark:text-slate-300">
                            {item().name || `素材 #${item().index + 1}`}
                          </Text>
                        </View>
                      </HoverPreviewTarget>
                    )}
                  </Each>
                </View>
              </View>
            </If>
          }
        >
          <View class="gap-4">
            {/* Stage 1: Anthropomorphic Browser Wash-Bot Stations */}
            <If when={activeTiles().length > 0}>
              <View testID="wb-active-pipeline" class="gap-2">
                <View class="flex-row items-center justify-between gap-2">
                  <View class="flex-row items-center gap-1.5">
                    <LivePulseDot color="blue" size={7} ping={!botAnimPaused()} />
                    <Spinner size={11} tint="#2563EB" />
                    <Text
                      class="text-xs font-semibold text-blue-600 dark:text-blue-400"
                      style={{ fontVariant: ["tabular-nums"] }}
                    >
                      {`正在加工 (${wb().activeCount} 项)`}
                    </Text>
                  </View>
                  <View class="flex-row items-center gap-2">
                    <Text class="text-[11px] text-slate-400 dark:text-slate-500">
                      正在洗图 · 悬浮轨道缩略图可看大图
                    </Text>
                    <Button
                      testID="wb-bot-anim-toggle"
                      text={botAnimPaused() ? "继续动画" : "暂停动画"}
                      size="small"
                      variant="ghost"
                      onPress={() => botAnimPaused.set(!botAnimPaused())}
                    />
                  </View>
                </View>

                <View style={WASHBOT_GRID_STYLE}>
                  <Each of={activeTiles()} key={(t) => t.key || String(t.taskIndex)}>
                    {(tile) => (
                      <WashBotCard
                        tile={tile()}
                        recentOutURL={recentDoneURL()}
                        celebrating={celebrating()}
                        animPaused={botAnimPaused()}
                      />
                    )}
                  </Each>
                </View>
              </View>
            </If>

            {/* Stage 2: Compact Waiting Queue */}
            <If when={pendingTiles().length > 0}>
              <View testID="wb-pending-queue" class="gap-2">
                <View class="flex-row items-center justify-between gap-2">
                  <View class="flex-row items-center gap-2">
                    <LivePulseDot
                      color="slate"
                      size={7}
                      ping={wb().activeCount > 0 || Boolean(wb().running)}
                    />
                    <Text
                      class="text-xs font-semibold text-slate-700 dark:text-slate-200"
                      style={{ fontVariant: ["tabular-nums"] }}
                    >
                      {`等待加工 (${pendingTiles().length} 张排队中)`}
                    </Text>
                  </View>
                  <View class="flex-row items-center gap-2">
                    <Text class="text-[11px] text-slate-400 dark:text-slate-500">
                      按序号派发至空闲洗图机器人
                    </Text>
                    <If when={pendingTiles().length > 18}>
                      <Button
                        text={pendingExpanded() ? "收起等待" : "展开全部等待"}
                        size="small"
                        variant="ghost"
                        onPress={() => pendingExpanded.set(!pendingExpanded())}
                      />
                    </If>
                  </View>
                </View>

                <View style={PENDING_GRID_STYLE}>
                  <Each
                    of={visiblePending()}
                    key={(t) => t.key || String(t.taskIndex)}
                  >
                    {(tile, idx) => <PendingQueueTile tile={tile()} index={idx()} />}
                  </Each>
                </View>
              </View>
            </If>

            {/* Stage 3: Issues / Needs Attention */}
            <If when={issueTiles().length > 0}>
              <View class="gap-2">
                <Text class="text-xs font-semibold text-amber-600 dark:text-amber-400">
                  {`异常 / 需处理 (${issueTiles().length})`}
                </Text>
                <View style={ISSUE_GRID_STYLE}>
                  <Each of={issueTiles()} key={(t) => t.key || String(t.taskIndex)}>
                    {(tile) => <ResultTileCard tile={tile()} kind="issue" />}
                  </Each>
                </View>
              </View>
            </If>

            {/* Stage 4: Compact Completed Gallery with Hover Preview & Re-Wash */}
            <If when={doneTiles().length > 0}>
              <View class="gap-2">
                <View class="flex-row flex-wrap items-center justify-between gap-2">
                  <Text
                    class="text-xs font-semibold text-emerald-700 dark:text-emerald-400"
                    style={{ fontVariant: ["tabular-nums"] }}
                  >
                    {`已完成 (${doneTiles().length} 张 / ${wb().doneTasks} 任务)`}
                  </Text>
                  <View class="flex-row items-center gap-1.5">
                    <Text class="text-[11px] text-slate-400 dark:text-slate-500">
                      悬浮查看大图 · 支持放回二次清洗
                    </Text>
                    <Button
                      testID="wb-batch-select-toggle"
                      text={selectMode() ? "退出多选" : "批量选择"}
                      size="small"
                      variant={selectMode() ? "outline" : "ghost"}
                      onPress={() => {
                        const next = !selectMode();
                        selectMode.set(next);
                        if (!next) selectedDone.set([]);
                      }}
                    />
                  </View>
                </View>

                <If when={showDone() && (selectMode() || selectedDone().length > 0)}>
                  <View class="flex-row flex-wrap items-center justify-between gap-2 rounded-md border border-blue-200 bg-blue-50/70 px-2.5 py-1.5 dark:border-blue-500/35 dark:bg-blue-500/15">
                    <Text
                      class="text-xs font-medium text-blue-700 dark:text-blue-300"
                      style={{ fontVariant: ["tabular-nums"] }}
                    >
                      {`已选 ${selectedDone().length} 张成图`}
                    </Text>
                    <View class="flex-row items-center gap-1.5">
                      <Button
                        testID="batch-rewash-btn"
                        text={`放回待洗 · 二次洗 (${selectedDone().length})`}
                        size="small"
                        icon="rotate-ccw"
                        variant="primary"
                        disabled={Boolean(wb().running) || selectedDone().length === 0}
                        onPress={() => {
                          const picks = [...selectedDone()];
                          if (picks.length === 0) return;
                          requeueWorkbenchDone(picks);
                          selectedDone.set([]);
                          selectMode.set(false);
                        }}
                      />
                      <Button
                        text="全选"
                        size="small"
                        variant="outline"
                        onPress={() => selectedDone.set(selectableDoneIndices())}
                      />
                      <Button
                        text="取消"
                        size="small"
                        variant="ghost"
                        onPress={() => {
                          selectedDone.set([]);
                          selectMode.set(false);
                        }}
                      />
                    </View>
                  </View>
                </If>

                <If when={showDone()}>
                  <View class="gap-2.5">
                    <View style={density() === "md" ? DONE_GRID_MD_STYLE : DONE_GRID_SM_STYLE}>
                      <Each
                        of={doneTiles().slice(0, doneLimit())}
                        key={(t) => t.key || `${t.taskIndex}-${t.path || ""}`}
                      >
                        {(tile) => (
                          <ResultTileCard
                            tile={tile()}
                            kind="done"
                            selectMode={selectMode()}
                            selected={selectedSet().has(tile().taskIndex)}
                            onToggleSelect={() => toggleDoneSelect(tile().taskIndex)}
                          />
                        )}
                      </Each>
                    </View>
                    <If when={doneTiles().length > doneLimit()}>
                      <View class="flex-row items-center gap-2 pt-1">
                        <Button
                          text="显示更多 (+30)"
                          size="small"
                          variant="outline"
                          onPress={() => doneLimit.set(doneLimit() + 30)}
                        />
                        <Button
                          text="展开全部"
                          size="small"
                          variant="ghost"
                          onPress={() => doneLimit.set(doneTiles().length)}
                        />
                      </View>
                    </If>
                  </View>
                </If>
              </View>
            </If>
          </View>
        </If>
      </Scroll>
    </View>
  );
}
