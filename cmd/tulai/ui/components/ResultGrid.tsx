import { createMotion, Image, Scroll, signal, Text, View } from "nsgo";
import {
  Badge,
  Button,
  ContextMenu,
  Icon,
  IconButton,
  Menu,
  MenuItem,
  MenuSeparator,
  Progress,
  Spinner,
} from "nsgo/ui";
import {
  type ResultTileDTO,
  type SourceItemDTO,
  openOutputFolder,
  openPath,
  replaceResultImage,
  retryAllFailed,
  retryTask,
  revealPath,
  setPreviewPath,
  setResultsDoneOpen,
} from "tulai/backend";
import { workbench } from "../store";
import { ActiveScanOverlay, ConveyorFlowTrack, LivePulseDot } from "./MotionPrimitives";
import { EmptyNotice } from "./primitives";

function formatOrderNo(taskIndex: number): string {
  const num = taskIndex > 0 ? taskIndex : 1;
  return `#${String(num).padStart(2, "0")}`;
}

function activeStatusLabel(status?: string): string {
  if (status === "需要人工验证") return "需人工验证";
  if (status === "重试中") return "重试加工中";
  return "正在洗图";
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
      <If when={Boolean(props.tile.canReplace)}>
        <If when={Boolean(props.tile.path)}>
          <MenuSeparator />
        </If>
        <MenuItem
          text="替换原图"
          icon="replace"
          onSelect={() => void replaceResultImage(props.tile.taskIndex)}
        />
      </If>
      <If when={Boolean(props.tile.actionText)}>
        <If when={Boolean(props.tile.path || props.tile.canReplace)}>
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

function ResultTileCard(props: { tile: ResultTileDTO; kind?: "done" | "active" | "issue" }) {
  const hovered = signal(false);
  const enter = createMotion({ enter: "fadeIn", duration: 220 });
  const isActive = () => props.kind === "active";
  const imgSrc = () => props.tile.fileURL || props.tile.srcURL || "";
  const hasMenu = () => Boolean(props.tile.path || props.tile.canReplace);
  const hasContextActions = () => Boolean(hasMenu() || props.tile.actionText);

  const cardBody = (
    <View
      testID={`result-tile-card-${props.tile.taskIndex}`}
      onMouseEnter={() => hovered.set(true)}
      onMouseLeave={() => hovered.set(false)}
      style={{ opacity: enter.opacity }}
      hoverStyle={isActive() ? undefined : { transform: [{ translateY: -2 }] }}
      pressedStyle={{ transform: [{ scale: 0.99 }] }}
      class="overflow-hidden rounded-xl border transition duration-200"
      classList={{
        "border-blue-400/90 bg-white dark:border-blue-500/60 dark:bg-[#131924]": isActive(),
        "border-slate-200/85 bg-white hover:border-slate-300 dark:border-[#1E2738] dark:bg-[#131924] dark:hover:border-[#334155]":
          !isActive(),
      }}
    >
      {/* Full-bleed artwork stage */}
      <View
        class="relative w-full overflow-hidden bg-slate-50/70 dark:bg-[#0D121C]"
        style={{ aspectRatio: 1.05 }}
      >
        <View
          onPress={() => {
            const p = props.tile.path;
            if (p) setPreviewPath(p);
          }}
          style={{ cursor: props.tile.path ? "pointer" : "default" }}
          class="h-full w-full items-center justify-center"
        >
          <If
            when={Boolean(imgSrc())}
            else={<Icon name="image" size={26} color="#94A3B8" />}
          >
            <Image
              src={imgSrc()}
              fadeDuration={220}
              class="h-full w-full object-contain"
              testID={`result-tile-image-${props.tile.taskIndex}`}
            />
          </If>
        </View>

        {/* Live laser scan & optical frame for active processing */}
        <If when={isActive()}>
          <ActiveScanOverlay
            testID={`active-scan-overlay-${props.tile.taskIndex}`}
            tone={props.tile.status === "需要人工验证" ? "amber" : "blue"}
          />
        </If>

        {/* Top-left live processing badge on active cards */}
        <If when={isActive()}>
          <View
            style={{ position: "absolute", top: 8, left: 8 }}
            class="flex-row items-center gap-1 rounded-full px-2 py-0.5"
            classList={{
              "bg-amber-600/95": props.tile.status === "需要人工验证",
              "bg-blue-600/95": props.tile.status !== "需要人工验证",
            }}
          >
            <Spinner size={10} tint="#FFFFFF" />
            <Text class="text-[11px] font-semibold text-white">
              {activeStatusLabel(props.tile.status)}
            </Text>
          </View>
        </If>

        {/* Top-left issue badge on异常 cards (completed art stays clean) */}
        <If when={props.kind === "issue"}>
          <View style={{ position: "absolute", top: 8, left: 8 }}>
            <Badge
              text={props.tile.status || "-"}
              variant={tileBadgeVariant(props.tile.status)}
            />
          </View>
        </If>

        {/* Top-right work-order number + timer pill for active cards */}
        <If when={isActive() && !(hovered() && Boolean(props.tile.path))}>
          <View
            style={{ position: "absolute", top: 8, right: 8 }}
            class="rounded-md bg-slate-900/75 px-1.5 py-0.5"
          >
            <Text
              class="text-[10px] font-semibold text-white"
              style={{ fontVariant: ["tabular-nums"] }}
            >
              {`${formatOrderNo(props.tile.taskIndex)}${props.tile.elapsed ? ` · ${props.tile.elapsed}` : ""}`}
            </Text>
          </View>
        </If>

        {/* Hover-revealed compact quick-action capsule */}
        <If when={hovered() && Boolean(props.tile.path)}>
          <View
            style={{ position: "absolute", top: 8, right: 8 }}
            class="flex-row items-center gap-0.5 rounded-lg bg-slate-900/80 px-1 py-0.5 transition duration-150"
          >
            <IconButton
              icon="eye"
              size={24}
              iconSize={13}
              variant="ghost"
              tint="#F8FAFC"
              tooltip="大图预览"
              accessibilityLabel="大图预览"
              onPress={() => {
                if (props.tile.path) setPreviewPath(props.tile.path);
              }}
            />
            <IconButton
              icon="folder-open"
              size={24}
              iconSize={13}
              variant="ghost"
              tint="#F8FAFC"
              tooltip="在文件夹中显示"
              accessibilityLabel="在文件夹中显示"
              onPress={() => {
                if (props.tile.path) void revealPath(props.tile.path);
              }}
            />
            <IconButton
              icon="external-link"
              size={24}
              iconSize={13}
              variant="ghost"
              tint="#F8FAFC"
              tooltip="系统默认打开"
              accessibilityLabel="系统默认打开"
              onPress={() => {
                if (props.tile.path) void openPath(props.tile.path);
              }}
            />
            <If when={Boolean(props.tile.canReplace)}>
              <IconButton
                icon="replace"
                size={24}
                iconSize={13}
                variant="ghost"
                tint="#F8FAFC"
                tooltip="替换原图"
                accessibilityLabel="替换原图"
                onPress={() => void replaceResultImage(props.tile.taskIndex)}
              />
            </If>
          </View>
        </If>

        {/* Bottom indeterminate scan line for active processing */}
        <If when={isActive()}>
          <View style={{ position: "absolute", left: 0, right: 0, bottom: 0 }}>
            <Progress indeterminate />
          </View>
        </If>
      </View>

      {/* Minimal footer: title + tabular metadata on left, single quiet menu / primary retry on right */}
      <View class="flex-row items-center justify-between gap-2 border-t border-slate-100 px-2.5 py-1.5 dark:border-[#1E2738]">
        <View class="min-w-0 flex-1 gap-0.5">
          <Text class="truncate text-xs font-medium text-slate-800 dark:text-slate-100">
            {props.tile.title || `任务 ${formatOrderNo(props.tile.taskIndex)}`}
          </Text>
          <If when={Boolean(props.tile.accountName || props.tile.platform || props.tile.elapsed)}>
            <Text
              class="truncate text-[11px] text-slate-400 dark:text-slate-500"
              style={{ fontVariant: ["tabular-nums"] }}
            >
              {[props.tile.accountName || props.tile.platform, props.tile.elapsed].filter(Boolean).join(" · ")}
            </Text>
          </If>
          <If when={Boolean(props.tile.reason)}>
            <Text class="truncate text-[11px] text-rose-600 dark:text-rose-400">
              {props.tile.reason}
            </Text>
          </If>
        </View>

        <View class="shrink-0 flex-row items-center gap-1">
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
    delay: Math.min((props.index ?? 0) * 28, 180),
  });
  return (
    <View class="min-w-0">
      <View
        testID={`pending-tile-${props.tile.taskIndex}`}
        style={{ opacity: enter.opacity }}
        hoverStyle={{ transform: [{ translateY: -1 }] }}
        class="overflow-hidden rounded-lg border border-dashed border-slate-300/90 bg-slate-50/85 transition duration-150 dark:border-[#263248] dark:bg-[#111722]"
      >
        <View
          class="relative w-full overflow-hidden bg-slate-100/70 dark:bg-[#0D121C]"
          style={{ aspectRatio: 1 }}
        >
          <View class="h-full w-full items-center justify-center p-1">
            <If
              when={Boolean(props.tile.srcURL)}
              else={
                <View class="items-center gap-1 px-1.5">
                  <Icon name="clock" size={16} color="#94A3B8" />
                  <If when={Boolean(props.tile.prompt)}>
                    <Text
                      style={{ numberOfLines: 2 }}
                      class="text-center text-[10px] text-slate-500 dark:text-slate-400"
                    >
                      {props.tile.prompt}
                    </Text>
                  </If>
                </View>
              }
            >
              <Image
                src={props.tile.srcURL!}
                fadeDuration={180}
                class="h-full w-full object-contain opacity-85"
              />
            </If>
          </View>

          <View
            style={{ position: "absolute", top: 5, left: 5 }}
            class="rounded bg-slate-900/75 px-1.5 py-0.5"
          >
            <Text
              class="text-[10px] font-semibold text-white"
              style={{ fontVariant: ["tabular-nums"] }}
            >
              {formatOrderNo(props.tile.taskIndex)}
            </Text>
          </View>

          <View
            style={{ position: "absolute", top: 5, right: 5 }}
            class="flex-row items-center gap-1 rounded bg-white/90 px-1.5 py-0.5 dark:bg-slate-800/90"
          >
            <View class="size-1.5 rounded-full bg-slate-400 animate-pulse" />
            <Text class="text-[10px] font-medium text-slate-600 dark:text-slate-300">
              等待
            </Text>
          </View>
        </View>

        <Text class="truncate border-t border-slate-200/70 px-1.5 py-1 text-[10px] text-slate-600 dark:border-[#1E2738] dark:text-slate-400">
          {props.tile.title || `任务 ${formatOrderNo(props.tile.taskIndex)}`}
        </Text>
      </View>
    </View>
  );
}

function StagingSourceTile(props: { item: SourceItemDTO; orderNo: number }) {
  return (
    <View class="min-w-0">
      <View
        hoverStyle={{ transform: [{ translateY: -1 }] }}
        class="overflow-hidden rounded-lg border border-dashed border-slate-300/90 bg-slate-50/85 transition duration-150 dark:border-[#263248] dark:bg-[#111722]"
      >
        <View
          class="relative w-full overflow-hidden bg-slate-100/70 dark:bg-[#0D121C]"
          style={{ aspectRatio: 1 }}
        >
          <View class="h-full w-full items-center justify-center p-1">
            <If
              when={Boolean(props.item.fileURL)}
              else={<Icon name="image" size={16} color="#94A3B8" />}
            >
              <Image
                src={props.item.fileURL!}
                fadeDuration={180}
                class="h-full w-full object-contain opacity-85"
              />
            </If>
          </View>

          <View
            style={{ position: "absolute", top: 5, left: 5 }}
            class="rounded bg-slate-900/75 px-1.5 py-0.5"
          >
            <Text
              class="text-[10px] font-semibold text-white"
              style={{ fontVariant: ["tabular-nums"] }}
            >
              {formatOrderNo(props.orderNo)}
            </Text>
          </View>

          <View
            style={{ position: "absolute", top: 5, right: 5 }}
            class="rounded bg-white/90 px-1.5 py-0.5 dark:bg-slate-800/90"
          >
            <Text class="text-[10px] font-medium text-slate-600 dark:text-slate-300">
              待启动
            </Text>
          </View>
        </View>

        <Text class="truncate border-t border-slate-200/70 px-1.5 py-1 text-[10px] text-slate-600 dark:border-[#1E2738] dark:text-slate-400">
          {props.item.name || `素材 ${formatOrderNo(props.orderNo)}`}
        </Text>
      </View>
    </View>
  );
}

const GALLERY_GRID_STYLE = {
  display: "grid" as const,
  gridTemplateColumns: "repeat(3, 1fr)",
  gap: 10,
};

const PENDING_GRID_STYLE = {
  display: "grid" as const,
  gridTemplateColumns: "repeat(5, 1fr)",
  gap: 8,
};

export function ResultGrid() {
  const wb = workbench;
  const doneLimit = signal(24);
  const pendingExpanded = signal(false);
  const doneCollapsed = signal(false);
  const activeRows = () => wb().activeRows ?? [];
  const pendingTiles = () => wb().pendingTiles ?? [];
  const issueRows = () => wb().issueRows ?? [];
  const doneTiles = () => wb().doneTiles ?? [];
  const stagedSources = () => wb().sources ?? [];
  const total = () => wb().totalCount;
  const showDone = () =>
    !doneCollapsed() &&
    (Boolean(wb().resultsDoneOpen) ||
      (activeRows().length === 0 && pendingTiles().length === 0 && issueRows().length === 0));
  const hasMultipleSections = () =>
    activeRows().length > 0 || pendingTiles().length > 0 || issueRows().length > 0;
  const hasPipelineWork = () =>
    total() > 0 ||
    activeRows().length > 0 ||
    pendingTiles().length > 0 ||
    issueRows().length > 0 ||
    doneTiles().length > 0;
  const visiblePending = () =>
    pendingExpanded() ? pendingTiles() : pendingTiles().slice(0, 15);

  return (
    <View testID="wb-result-gallery" class="min-h-0 flex-1">
      {/* Pinned Engineering Pipeline Header */}
      <View class="shrink-0 flex-row items-center justify-between gap-2 px-3.5 pt-2.5 pb-2">
        <View class="min-w-0 flex-row items-center gap-2">
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
            <View class="rounded-full border border-slate-200/90 bg-slate-100/90 px-2 py-0.5 dark:border-[#263248] dark:bg-[#161E2E]">
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

        <View class="shrink-0 flex-row items-center gap-1.5">
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
        <View class="shrink-0 px-3.5 pb-2">
          <ConveyorFlowTrack
            active={Boolean(wb().running)}
            activeCount={wb().activeCount}
            pendingCount={pendingTiles().length}
            doneCount={doneTiles().length}
          />
        </View>
      </If>

      <Scroll class="min-h-0 flex-1" contentClass="gap-3.5 px-3.5 pb-3.5">
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
                class="gap-2.5 rounded-xl border border-dashed border-slate-200/90 bg-white/75 p-3 dark:border-[#1E2738] dark:bg-[#111722]/75"
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
                    点击左下角开始按钮进入流水线
                  </Text>
                </View>
                <View style={PENDING_GRID_STYLE}>
                  <Each
                    of={stagedSources().slice(0, 10)}
                    key={(s) => s.path || String(s.index)}
                  >
                    {(item) => (
                      <StagingSourceTile item={item()} orderNo={item().index + 1} />
                    )}
                  </Each>
                </View>
              </View>
            </If>
          }
        >
          <View class="gap-5">
            {/* Stage 1: Currently processing / washing */}
            <If when={activeRows().length > 0}>
              <View testID="wb-active-pipeline" class="gap-2.5">
                <View class="flex-row items-center justify-between gap-2">
                  <View class="flex-row items-center gap-1.5">
                    <LivePulseDot color="blue" size={7} />
                    <Spinner size={11} tint="#2563EB" />
                    <Text
                      class="text-xs font-semibold text-blue-600 dark:text-blue-400"
                      style={{ fontVariant: ["tabular-nums"] }}
                    >
                      {`正在加工 (${wb().activeCount} 项)`}
                    </Text>
                  </View>
                  <Text class="text-[11px] text-slate-400 dark:text-slate-500">
                    当前工位实时状态
                  </Text>
                </View>

                <Each of={activeRows()} key={(r) => r.key || r.title || ""}>
                  {(row) => (
                    <View class="gap-2">
                      <If when={Boolean(row().header)}>
                        <View class="flex-row items-center gap-2 border-l-2 border-blue-500 pl-2">
                          <Text class="text-xs font-semibold text-slate-700 dark:text-slate-200">
                            {`工位 · ${row().title}`}
                          </Text>
                          <If when={Boolean(row().summary)}>
                            <Text class="text-[11px] text-slate-400 dark:text-slate-500">
                              {row().summary}
                            </Text>
                          </If>
                        </View>
                      </If>
                      <If when={(row().tiles ?? []).length > 0}>
                        <View style={GALLERY_GRID_STYLE}>
                          <Each of={row().tiles ?? []} key={(t) => t.key || String(t.taskIndex)}>
                            {(tile) => <ResultTileCard tile={tile()} kind="active" />}
                          </Each>
                        </View>
                      </If>
                    </View>
                  )}
                </Each>
              </View>
            </If>

            {/* Stage 2: Waiting queue */}
            <If when={pendingTiles().length > 0}>
              <View testID="wb-pending-queue" class="gap-2.5">
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
                      按序号派发至空闲工位
                    </Text>
                    <If when={pendingTiles().length > 15}>
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

            {/* Stage 3: Issues / needs attention */}
            <If when={issueRows().length > 0}>
              <View class="gap-2.5">
                <Text class="text-xs font-semibold text-amber-600 dark:text-amber-400">
                  异常 / 需处理
                </Text>
                <Each of={issueRows()} key={(r) => r.key || r.title || ""}>
                  {(row) => (
                    <View class="gap-2">
                      <If when={Boolean(row().header)}>
                        <Text class="text-xs font-medium text-slate-600 dark:text-slate-300">
                          {row().title} {row().summary ? `· ${row().summary}` : ""}
                        </Text>
                      </If>
                      <If when={(row().tiles ?? []).length > 0}>
                        <View style={GALLERY_GRID_STYLE}>
                          <Each of={row().tiles ?? []} key={(t) => t.key || String(t.taskIndex)}>
                            {(tile) => <ResultTileCard tile={tile()} kind="issue" />}
                          </Each>
                        </View>
                      </If>
                    </View>
                  )}
                </Each>
              </View>
            </If>

            {/* Stage 4: Completed gallery */}
            <If when={doneTiles().length > 0}>
              <View class="gap-2.5">
                <If when={hasMultipleSections()}>
                  <Text
                    class="text-xs font-semibold text-emerald-700 dark:text-emerald-400"
                    style={{ fontVariant: ["tabular-nums"] }}
                  >
                    {`已完成 (${doneTiles().length} 张 / ${wb().doneTasks} 任务)`}
                  </Text>
                </If>
                <If when={showDone()}>
                  <View class="gap-3">
                    <View style={GALLERY_GRID_STYLE}>
                      <Each
                        of={doneTiles().slice(0, doneLimit())}
                        key={(t) => t.key || `${t.taskIndex}-${t.path || ""}`}
                      >
                        {(tile) => <ResultTileCard tile={tile()} kind="done" />}
                      </Each>
                    </View>
                    <If when={doneTiles().length > doneLimit()}>
                      <View class="flex-row items-center gap-2 pt-1">
                        <Button
                          text="显示更多 (+24)"
                          size="small"
                          variant="outline"
                          onPress={() => doneLimit.set(doneLimit() + 24)}
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
