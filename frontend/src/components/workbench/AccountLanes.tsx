import { memo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
// Brand marks from @lobehub/icons (MIT).
import doubaoMark from "@/assets/providers/doubao-avatar.png";
import qwenMark from "@/assets/providers/qwen-color.svg";
import qwenIntlMark from "@/assets/providers/qwen-intl-color.svg";
import { AccountListFrame, AccountViewButton } from "@/components/primitives";
import { Badge, Button, Icon, IconButton, Progress, Switch, Tooltip, TooltipGroup } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Browser, Settings, Workbench, fireIpc } from "@/lib/ipc";
import {
  TAB_BROWSER,
  goTab,
  useAppStore,
  useWorkbench,
  type AccountLaneDTO,
  type WorkbenchStateDTO,
} from "@/store";

const PROVIDER_MARKS: Record<string, string> = {
  豆包: doubaoMark,
  千问: qwenMark,
  千问国际: qwenIntlMark,
};

const MIN_CONCURRENCY_MAX = 2;
const MAX_CONCURRENCY_MAX = 8;
const LANE_ROW_HEIGHT = 41;
const LANE_ROW_ERR_HEIGHT = 60;
const LANE_COLUMNS_HEADER_HEIGHT = 28;
const LANE_GRID_COLS =
  "@[560px]:grid @[560px]:grid-cols-[minmax(0,1fr)_minmax(0,0.85fr)_minmax(0,1.25fr)_minmax(154px,1fr)] @[560px]:items-center @[560px]:gap-3";

function isLaneWorking(state?: string): boolean {
  return state === "出图中";
}

function laneTone(state?: string): "success" | "accent" {
  return isLaneWorking(state) ? "accent" : "success";
}

function formatJobOrder(taskIndex: number): string {
  const num = taskIndex > 0 ? taskIndex : 1;
  return `#${String(num).padStart(2, "0")}`;
}

function laneStateText(ln: AccountLaneDTO): "工作" | "空闲" {
  return isLaneWorking(ln.state) ? "工作" : "空闲";
}

function areAccountLanesEqual(a: AccountLaneDTO, b: AccountLaneDTO): boolean {
  if (a === b) return true;
  if (
    a.id !== b.id ||
    a.platform !== b.platform ||
    a.title !== b.title ||
    a.state !== b.state ||
    a.action !== b.action ||
    a.usedToday !== b.usedToday ||
    a.successToday !== b.successToday ||
    a.failedToday !== b.failedToday ||
    a.cap !== b.cap ||
    a.readyInSec !== b.readyInSec ||
    a.cooldownAt !== b.cooldownAt ||
    a.coolUntil !== b.coolUntil ||
    a.usagePct !== b.usagePct ||
    a.usageLabel !== b.usageLabel ||
    a.usageErr !== b.usageErr ||
    a.hasJob !== b.hasJob ||
    a.disabled !== b.disabled ||
    a.isRunning !== b.isRunning ||
    a.isLoggedIn !== b.isLoggedIn ||
    a.pending !== b.pending ||
    a.pendingOp !== b.pendingOp ||
    a.engine !== b.engine ||
    a.lastError !== b.lastError
  ) {
    return false;
  }
  if (!a.hasJob && !b.hasJob) return true;
  const aj = a.job;
  const bj = b.job;
  if (aj === bj) return true;
  if (!aj || !bj) return false;
  return (
    aj.taskIndex === bj.taskIndex &&
    aj.prompt === bj.prompt &&
    aj.sourceUrl === bj.sourceUrl &&
    aj.status === bj.status &&
    aj.elapsed === bj.elapsed
  );
}

const AccountLaneRow = memo(
  function AccountLaneRow({
    lane,
    running,
    isLast = false,
  }: {
    lane: AccountLaneDTO;
    running: boolean;
    isLast?: boolean;
  }) {
  const showClearCool =
    lane.state === "冷却中" ||
    lane.state === "今日已满" ||
    lane.state === "额度不足" ||
    Boolean(lane.lastError);
  const needsLogin = lane.state === "需要登录" || !lane.isLoggedIn;
  const usageProgress =
    lane.usagePct > 0
      ? Math.min(1, lane.usagePct / 100)
      : lane.cap > 0
        ? Math.min(1, lane.usedToday / lane.cap)
        : 0;

  return (
    <div
      data-testid={`lane-row-${lane.id}`}
      className={cn(
        "flex flex-col gap-1 px-3.5 py-2 transition-colors duration-150",
        !isLast && "border-b border-divider",
        lane.disabled ? "bg-sunken/40 text-muted opacity-75" : "bg-card hover:bg-sunken/70",
      )}
    >
      {/* Primary Horizontal Row */}
      <div className={cn("flex flex-wrap items-center justify-between gap-2", LANE_GRID_COLS)}>
          {/* Account Toggle & Name */}
          <div className="flex min-w-0 items-center gap-2">
            <Switch
              data-testid={`lane-toggle-${lane.id}`}
              size="small"
              checked={!lane.disabled}
              aria-label={`启用或停用 ${lane.title || `账号 #${lane.id}`}`}
              onCheckedChange={() =>
                fireIpc(() => Browser.browserRowAction(lane.id, "toggleDisabled"))
              }
            />
            <span
              className={cn(
                "min-w-0 max-w-[180px] truncate text-xs font-medium @[560px]:max-w-none",
                lane.disabled ? "text-subtle line-through" : "text-ink",
              )}
            >
              {lane.title || `账号 #${lane.id}`}
            </span>
          </div>

          {/* Account Status */}
          <div className="flex min-w-0 flex-wrap items-center gap-1.5">
            <Badge
              text={laneStateText(lane)}
              variant={laneTone(lane.state)}
              dot={isLaneWorking(lane.state)}
              pulseDot={isLaneWorking(lane.state)}
            />
            {lane.disabled && lane.state === "出图中" ? (
              <span className="text-[11px] text-status-warn">本单完成后停用</span>
            ) : null}
          </div>

          {/* Today Usage, Progress & Active Job */}
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="tnum max-w-full truncate text-[11px] text-muted">
              {lane.usageLabel
                ? lane.usageLabel
                : lane.cap > 0
                  ? `今日 ${lane.usedToday}/${lane.cap} · 成功 ${lane.successToday}`
                  : `今日 ${lane.usedToday} 次 · 成功 ${lane.successToday}`}
            </span>
            {lane.usagePct > 0 || lane.cap > 0 ? (
              <div className="w-[64px] shrink-0">
                <Progress value={usageProgress} />
              </div>
            ) : null}
            {lane.hasJob ? (
              <span className="tnum shrink-0 truncate text-[11px] font-medium text-accent">
                {`${formatJobOrder(lane.job.taskIndex)}${lane.job.elapsed ? ` · ${lane.job.elapsed}` : ""}`}
              </span>
            ) : null}
          </div>

          {/* Quick Actions */}
          <div
            data-testid={`lane-actions-${lane.id}`}
            className="flex shrink-0 flex-wrap items-center justify-end gap-1"
          >
            <AccountViewButton accountId={lane.id} name={lane.title} iconOnly />
            {lane.state === "验证码" ? (
              <Button
                text="去验证"
                size="small"
                variant="primary"
                onClick={() => fireIpc(() => Browser.openBrowserDrawer(lane.id))}
              />
            ) : lane.pending && lane.pendingOp === "starting" ? (
              <Button text="打开中" size="small" variant="ghost" disabled busy />
            ) : lane.isRunning ? (
              <Button
                text={lane.pending && lane.pendingOp === "stopping" ? "关闭中" : "关闭"}
                size="small"
                variant="outline"
                disabled={Boolean(lane.pending) || running}
                busy={Boolean(lane.pending && lane.pendingOp === "stopping")}
                onClick={() => fireIpc(() => Browser.browserRowAction(lane.id, "stop"))}
              />
            ) : needsLogin ? (
              <Button
                text="打开登录"
                size="small"
                variant="outline"
                disabled={Boolean(lane.pending) || running}
                onClick={() => fireIpc(() => Browser.browserRowAction(lane.id, "launch"))}
              />
            ) : (
              <Button
                text="打开"
                size="small"
                variant="ghost"
                disabled={Boolean(lane.pending) || running}
                onClick={() => fireIpc(() => Browser.browserRowAction(lane.id, "launch"))}
              />
            )}

            {showClearCool ? (
              <Button
                text="清除冷却"
                size="small"
                variant="outline"
                onClick={() => fireIpc(() => Browser.browserRowAction(lane.id, "restore"))}
              />
            ) : null}

            {!running ? (
              <Button
                text="删除"
                size="small"
                variant="ghost"
                onClick={() => fireIpc(() => Browser.browserRowAction(lane.id, "delete"))}
              />
            ) : null}
          </div>
        </div>

      {lane.usageErr || lane.lastError ? (
        <div className="select-text truncate pl-9 text-[11px] text-status-warn">
          {lane.usageErr || lane.lastError}
        </div>
      ) : null}
    </div>
  );
},
(prev, next) =>
  prev.running === next.running &&
  prev.isLast === next.isLast &&
  areAccountLanesEqual(prev.lane, next.lane),
);

const CollapsedLanePill = memo(
  function CollapsedLanePill({ lane }: { lane: AccountLaneDTO }) {
  const isVerify = lane.state === "验证码";
  const isWorking = !lane.disabled && lane.state === "出图中";
  const dotClass = isVerify
    ? "bg-status-warn"
    : lane.disabled
      ? "bg-subtle"
      : isWorking
        ? "bg-accent"
        : lane.state === "空闲"
          ? "bg-status-ok"
          : lane.state === "今日已满" || lane.state === "额度不足" || lane.state === "离线"
            ? "bg-status-danger"
            : "bg-status-warn";

  return (
    <Tooltip content="点击打开内置浏览器完成验证" disabled={!isVerify} side="top">
      <button
        type="button"
        data-testid={`lane-pill-${lane.id}`}
        onClick={() =>
          isVerify
            ? fireIpc(() => Browser.openBrowserDrawer(lane.id))
            : fireIpc(() => Browser.browserRowAction(lane.id, "toggleDisabled"))
        }
      className={cn(
        "inline-flex cursor-pointer items-center gap-1.5 rounded-full border px-2 py-0.5 transition-[background-color,border-color,transform] duration-150 select-none active:scale-[0.97]",
        isVerify
          ? "border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] hover:brightness-95"
          : isWorking
            ? "border-accent-border bg-accent-soft hover:bg-accent-border/40"
            : !lane.disabled
              ? "border-hairline bg-sunken hover:bg-sunken-hover"
              : "border-hairline/70 bg-sunken/60 opacity-70 hover:bg-sunken-hover/70",
      )}
    >
      <span className={cn("size-1.5 shrink-0 rounded-full", dotClass)} />
      <span
        className={cn(
          "max-w-[140px] truncate text-[11px] font-medium",
          isVerify ? "text-status-warn" : lane.disabled ? "text-subtle line-through" : "text-ink",
        )}
      >
        {lane.title || `账号 #${lane.id}`}
      </span>
      {isVerify ? (
        <span className="text-[10px] font-semibold text-status-warn">去验证</span>
      ) : isWorking && lane.hasJob ? (
        <span className="tnum text-[10px] font-semibold text-accent">
          {`${formatJobOrder(lane.job.taskIndex)}${lane.job.elapsed ? ` ${lane.job.elapsed}` : ""}`}
        </span>
      ) : null}
        {!isVerify && lane.disabled ? <span className="text-[10px] text-subtle">已停用</span> : null}
      </button>
    </Tooltip>
  );
},
(prev, next) => areAccountLanesEqual(prev.lane, next.lane),
);

export function AccountLanes({ wb: propWb }: { wb?: WorkbenchStateDTO } = {}) {
  const storeWb = useWorkbench();
  const wb = propWb ?? storeWb;
  const listScrollRef = useRef<HTMLDivElement | null>(null);

  const platform = wb?.platform || (wb?.qwenIntl ? "千问国际" : wb?.qianwen ? "千问" : "豆包");
  const platformMark = PROVIDER_MARKS[platform];
  const platformLanes = (wb?.lanes ?? []).filter((l) => l.platform === platform);
  const enabledCount = platformLanes.filter((l) => !l.disabled).length;
  const disabledCount = platformLanes.length - enabledCount;
  const busyCount = platformLanes.filter((l) => !l.disabled && l.state === "出图中").length;
  const verifyCount = platformLanes.filter((l) => l.state === "验证码").length;
  const readyCount = platformLanes.filter((l) => l.state === "空闲").length;
  const collapsed = Boolean(wb?.laneCollapsed);
  const inlineCollapsedPills =
    collapsed && platformLanes.length > 0 && platformLanes.length <= 4;

  const listHeight = Math.min(Math.max(platformLanes.length, 1) * LANE_ROW_HEIGHT, 176);

  const useVirtualized = !collapsed && platformLanes.length > 24;

  const virtualizer = useVirtualizer({
    count: useVirtualized ? platformLanes.length : 0,
    getScrollElement: () => listScrollRef.current,
    estimateSize: (idx) => {
      const ln = platformLanes[idx];
      return ln && (ln.usageErr || ln.lastError) ? LANE_ROW_ERR_HEIGHT : LANE_ROW_HEIGHT;
    },
    getItemKey: (idx) => String(platformLanes[idx]?.id ?? idx),
    overscan: 6,
    initialRect: { width: 640, height: listHeight },
  });

  const virtualItems = useVirtualized ? virtualizer.getVirtualItems() : [];
  const renderVirtualized = useVirtualized && virtualItems.length > 0;

  if (!wb) return null;

  const concurrencyAuto = Boolean(wb.concurrencyAuto);
  const concurrencyMax = Math.min(
    MAX_CONCURRENCY_MAX,
    Math.max(MIN_CONCURRENCY_MAX, wb.concurrencyMax || MIN_CONCURRENCY_MAX),
  );
  const concurrencyAutoField =
    platform === "千问"
      ? "concurrencyAutoQianwen"
      : platform === "千问国际"
        ? "concurrencyAutoQwenIntl"
        : "concurrencyAutoDoubao";
  const concurrencyMaxField =
    platform === "千问"
      ? "concurrencyMaxQianwen"
      : platform === "千问国际"
        ? "concurrencyMaxQwenIntl"
        : "concurrencyMaxDoubao";

  const handleToggleConcurrency = (checked: boolean) => {
    useAppStore.setState((cur) => ({
      workbench: cur.workbench ? { ...cur.workbench, concurrencyAuto: checked } : cur.workbench,
      settings:
        platform === "豆包" && cur.settings
          ? { ...cur.settings, concurrencyAuto: checked }
          : cur.settings,
    }));
    fireIpc(() => Settings.setSettingField(concurrencyAutoField, checked ? "true" : "false"));
  };

  const handleSetConcurrencyMax = (nextRaw: number) => {
    const next = Math.min(MAX_CONCURRENCY_MAX, Math.max(MIN_CONCURRENCY_MAX, nextRaw));
    if (next === concurrencyMax) return;
    useAppStore.setState((cur) => ({
      workbench: cur.workbench ? { ...cur.workbench, concurrencyMax: next } : cur.workbench,
      settings:
        platform === "豆包" && cur.settings
          ? { ...cur.settings, concurrencyMax: next }
          : cur.settings,
    }));
    fireIpc(() => Settings.setSettingField(concurrencyMaxField, String(next)));
  };

  const summaryText = (() => {
    const total = platformLanes.length;
    if (total === 0) return "暂无账号";
    const parts = [`已启用 ${enabledCount} / ${total}`];
    if (busyCount > 0) parts.push(`出图中 ${busyCount}`);
    if (verifyCount > 0) parts.push(`待验证 ${verifyCount}`);
    parts.push(`就绪 ${readyCount}`);
    return parts.join(" · ");
  })();

  return (
    <div className="shrink-0">
      <AccountListFrame
        data-testid="lane-table"
        className="bento-shadow overflow-hidden rounded-[16px] border border-hairline bg-card"
      >
        {/* Header */}
        <div
          data-testid="lane-table-header"
          className="flex flex-wrap items-center justify-between gap-2 border-b border-divider bg-card-header px-3.5 py-2"
        >
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
            {platformMark ? (
              <img
                src={platformMark}
                alt={`${platform}账号`}
                data-testid="lane-platform-icon"
                className="size-3.5 shrink-0 object-contain"
                draggable={false}
              />
            ) : (
              <Icon name="users" size={14} className="text-subtle" />
            )}
            <span className="tnum shrink-0 text-[11px] text-muted">{summaryText}</span>
            {inlineCollapsedPills ? (
              <div className="flex flex-wrap items-center gap-1.5">
                {platformLanes.map((lane) => (
                  <CollapsedLanePill key={lane.id} lane={lane} />
                ))}
              </div>
            ) : null}
          </div>

          <TooltipGroup className="flex shrink-0 flex-wrap items-center justify-end gap-1">
            <div
              data-testid="lanes-concurrency-control"
              className={cn(
                "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-[9px] border px-2 transition-colors duration-150",
                concurrencyAuto
                  ? "border-accent-border bg-accent-soft/75"
                  : "border-hairline bg-sunken",
              )}
            >
              <Tooltip
                content={
                  concurrencyAuto
                    ? `${platform}已开启自动并发，最多同时运行 ${concurrencyMax} 个${platform}账号（与其他平台独立，运行中也可实时调整）`
                    : `${platform}已关闭自动并发，一次串行运行 1 批任务（与其他平台独立，运行中也可实时调整）`
                }
                side="bottom"
              >
                <div className="inline-flex items-center">
                  <Switch
                    data-testid="lanes-concurrency-switch"
                    size="small"
                    label="自动并发"
                    checked={concurrencyAuto}
                    onCheckedChange={handleToggleConcurrency}
                    className="gap-1.5 text-[11.5px] font-medium"
                  />
                </div>
              </Tooltip>
              {concurrencyAuto ? (
                <>
                  <span className="h-3.5 w-px bg-accent-border/80" aria-hidden="true" />
                  <Tooltip
                    content={`${platform}并发上限（2–8，与其他平台独立，运行中也可实时调整）`}
                    side="bottom"
                  >
                    <div
                      data-testid="lanes-concurrency-stepper"
                      className="inline-flex items-center gap-0.5"
                    >
                      <span className="mr-0.5 text-[11px] text-muted select-none">上限</span>
                      <button
                        type="button"
                        data-testid="lanes-concurrency-dec"
                        aria-label={`减少${platform}并发上限`}
                        disabled={concurrencyMax <= MIN_CONCURRENCY_MAX}
                        onClick={() => handleSetConcurrencyMax(concurrencyMax - 1)}
                        className="inline-flex size-5 cursor-pointer items-center justify-center rounded-[6px] text-xs font-medium text-ink transition-colors hover:bg-card/90 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)] active:scale-[0.95] disabled:pointer-events-none disabled:opacity-35"
                      >
                        −
                      </button>
                      <span
                        data-testid="lanes-concurrency-max"
                        className="tnum min-w-[14px] text-center text-xs font-semibold text-accent select-none"
                      >
                        {concurrencyMax}
                      </span>
                      <button
                        type="button"
                        data-testid="lanes-concurrency-inc"
                        aria-label={`增加${platform}并发上限`}
                        disabled={concurrencyMax >= MAX_CONCURRENCY_MAX}
                        onClick={() => handleSetConcurrencyMax(concurrencyMax + 1)}
                        className="inline-flex size-5 cursor-pointer items-center justify-center rounded-[6px] text-xs font-medium text-ink transition-colors hover:bg-card/90 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)] active:scale-[0.95] disabled:pointer-events-none disabled:opacity-35"
                      >
                        +
                      </button>
                    </div>
                  </Tooltip>
                </>
              ) : null}
            </div>
            <span className="mx-0.5 h-3.5 w-px bg-divider" aria-hidden="true" />
            {platformLanes.length > 0 && disabledCount > 0 ? (
              <Button
                data-testid="lanes-enable-all"
                text="全部启用"
                size="small"
                variant="outline"
                onClick={() => fireIpc(() => Browser.setPlatformAccountsDisabled(platform, false))}
              />
            ) : null}
            {platformLanes.length > 0 && enabledCount > 0 ? (
              <Button
                data-testid="lanes-disable-all"
                text="全部停用"
                size="small"
                variant="ghost"
                onClick={() => fireIpc(() => Browser.setPlatformAccountsDisabled(platform, true))}
              />
            ) : null}
            <Tooltip content="刷新账号状态" side="bottom">
              <IconButton
                icon="refresh-cw"
                size={24}
                variant="ghost"
                aria-label="刷新账号"
                onClick={() => fireIpc(() => Browser.refreshBrowsers())}
              />
            </Tooltip>
            <Button
              text="账号池"
              size="small"
              variant="ghost"
              onClick={() => void goTab(TAB_BROWSER)}
            />
            <IconButton
              icon={
                <Icon
                  name="chevron-up"
                  size={14}
                  className={cn(
                    "transition-transform duration-180 ease-[cubic-bezier(0.16,1,0.3,1)]",
                    collapsed && "rotate-180",
                  )}
                />
              }
              variant="ghost"
              size={24}
              aria-label={collapsed ? "展开账号列表" : "收起账号列表"}
              aria-expanded={!collapsed}
              onClick={() =>
                fireIpc(() =>
                  Workbench.setWorkbenchField("laneCollapsed", collapsed ? "false" : "true"),
                )
              }
            />
          </TooltipGroup>
        </div>

        {/* Empty State */}
        {!collapsed && platformLanes.length === 0 ? (
          <div className="animate-tulai-banner-in flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5">
            <span className="flex-1 text-xs text-muted">
              {`当前「${platform}」暂无可用账号，请前往账号池新建并登录`}
            </span>
            <Button
              data-testid="lanes-go-pool-btn"
              text="前往账号池添加"
              size="small"
              icon="users"
              variant="primary"
              onClick={() => void goTab(TAB_BROWSER)}
            />
          </div>
        ) : null}

        {/* Collapsed Pills (> 4 accounts) */}
        {collapsed && platformLanes.length > 4 ? (
          <div className="animate-tulai-banner-in flex flex-wrap gap-1.5 px-3.5 py-2.5">
            {platformLanes.map((lane) => (
              <CollapsedLanePill key={lane.id} lane={lane} />
            ))}
          </div>
        ) : null}

        {/* Hairline Table Account Rows */}
        {!collapsed && platformLanes.length > 0 ? (
          <div
            ref={listScrollRef}
            data-testid="wb-account-list"
            style={{
              maxHeight: 176 + LANE_COLUMNS_HEADER_HEIGHT,
              height: renderVirtualized ? listHeight + LANE_COLUMNS_HEADER_HEIGHT : undefined,
            }}
            className="animate-tulai-banner-in @container min-h-0 overflow-y-auto"
          >
            <div
              data-testid="lane-columns-header"
              className={cn(
                "sticky top-0 z-10 hidden border-b border-divider bg-card-header px-3.5 py-1.5 text-[11px] font-medium text-subtle select-none",
                LANE_GRID_COLS,
              )}
            >
              <span>账号</span>
              <span>状态</span>
              <span>今日用量</span>
              <span className="text-right">操作</span>
            </div>
            {renderVirtualized ? (
              <div
                className="relative w-full"
                style={{ height: `${virtualizer.getTotalSize()}px` }}
              >
                {virtualItems.map((vRow) => {
                  const lane = platformLanes[vRow.index];
                  if (!lane) return null;
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
                      <AccountLaneRow
                        lane={lane}
                        running={Boolean(wb.running)}
                        isLast={vRow.index === platformLanes.length - 1}
                      />
                    </div>
                  );
                })}
              </div>
            ) : (
              <div className="flex flex-col">
                {platformLanes.map((lane, idx) => (
                  <AccountLaneRow
                    key={lane.id}
                    lane={lane}
                    running={Boolean(wb.running)}
                    isLast={idx === platformLanes.length - 1}
                  />
                ))}
              </div>
            )}
          </div>
        ) : null}
      </AccountListFrame>
    </div>
  );
}
