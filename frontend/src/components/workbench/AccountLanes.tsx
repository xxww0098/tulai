import { memo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
// Brand marks from @lobehub/icons (MIT).
import doubaoMark from "@/assets/providers/doubao-avatar.png";
import geminiMark from "@/assets/providers/gemini-color.svg";
import qwenMark from "@/assets/providers/qwen-color.svg";
import qwenIntlMark from "@/assets/providers/qwen-intl-color.svg";
import { AccountListFrame, AccountViewButton } from "@/components/primitives";
import { Badge, Button, Icon, IconButton, Progress, Switch, Tooltip, TooltipGroup } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Browser, Settings, Workbench, fireIpc } from "@/lib/ipc";
import {
  buildDoubaoStatusTooltip,
  buildGeminiStatusTooltip,
  buildQwenStatusTooltip,
  doubaoProgressTone,
  doubaoStatusTone,
  formatDoubaoCompactPill,
  formatDoubaoMetaLine,
  formatDoubaoPrimary,
  formatGeminiCompactPillQuota,
  formatGeminiMetaLine,
  formatGeminiQuotaPrimary,
  formatQwenCompactPillCredits,
  formatQwenCreditsPrimary,
  formatQwenMetaLine,
  geminiProgressTone,
  geminiQuotaTone,
  isDoubaoAccount,
  isGeminiAccount,
  isPaidMemberTier,
  isQwenAccount,
  qwenCreditTone,
  qwenProgressTone,
  summarizeDoubaoPool,
  summarizeGeminiPool,
  summarizeQwenPool,
} from "@/lib/qwenStatus";
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
  Gemini: geminiMark,
};

const MIN_CONCURRENCY_MAX = 2;
const MAX_CONCURRENCY_MAX = 8;
const LANE_ROW_HEIGHT = 41;
const LANE_ROW_QWEN_HEIGHT = 56;
const LANE_ROW_ERR_HEIGHT = 68;
const LANE_COLUMNS_HEADER_HEIGHT = 28;
const LANE_GRID_COLS =
  "@[560px]:grid @[560px]:grid-cols-[minmax(0,1fr)_minmax(0,0.75fr)_minmax(0,1.45fr)_minmax(164px,1fr)] @[560px]:items-center @[560px]:gap-3";

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
    a.lastError !== b.lastError ||
    a.creditsSynced !== b.creditsSynced ||
    a.creditsRemain !== b.creditsRemain ||
    a.creditsTotal !== b.creditsTotal ||
    a.creditCost !== b.creditCost ||
    a.creditsResetAt !== b.creditsResetAt ||
    a.creditsResetLabel !== b.creditsResetLabel ||
    a.creditsCycle !== b.creditsCycle ||
    a.creditsCycleLabel !== b.creditsCycleLabel ||
    a.memberTier !== b.memberTier ||
    a.memberLabel !== b.memberLabel ||
    a.concurrencyLimit !== b.concurrencyLimit ||
    a.concurrencyLabel !== b.concurrencyLabel ||
    a.watermarkFree !== b.watermarkFree ||
    a.usagePercent !== b.usagePercent ||
    a.platformLimited !== b.platformLimited ||
    a.limitTips !== b.limitTips ||
    a.resetCardCount !== b.resetCardCount ||
    a.creditsSyncedAt !== b.creditsSyncedAt ||
    a.creditsSyncedLabel !== b.creditsSyncedLabel
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
  const isQwen = isQwenAccount(lane);
  const isDoubao = isDoubaoAccount(lane);
  const isGemini = isGeminiAccount(lane);
  const isQuotaTracked = isQwen || isGemini;
  const creditTone = isQwen
    ? qwenCreditTone(lane)
    : isGemini
      ? geminiQuotaTone(lane)
      : isDoubao
        ? doubaoStatusTone(lane)
        : "muted";
  const usageProgress =
    lane.usagePct > 0
      ? Math.min(1, lane.usagePct / 100)
      : isQuotaTracked && lane.creditsSynced && lane.creditsRemain === 0
        ? 1
        : isDoubao && lane.creditsSynced && (lane.platformLimited || lane.creditsRemain === 0)
          ? 1
          : lane.cap > 0
            ? Math.min(1, lane.usedToday / lane.cap)
            : 0;
  const quotaTooltip = isQwen
    ? buildQwenStatusTooltip(lane, {
        todayCount: lane.usedToday,
        successToday: lane.successToday,
        failedToday: lane.failedToday,
        cooldownAt: lane.cooldownAt,
      })
    : isGemini
      ? buildGeminiStatusTooltip(lane, {
          todayCount: lane.usedToday,
          successToday: lane.successToday,
          failedToday: lane.failedToday,
          cooldownAt: lane.cooldownAt,
        })
      : isDoubao
        ? buildDoubaoStatusTooltip(lane, {
            todayCount: lane.usedToday,
            successToday: lane.successToday,
            failedToday: lane.failedToday,
            cooldownAt: lane.cooldownAt,
          })
        : "";
  const quotaMeta = isQwen
    ? formatQwenMetaLine(lane, lane.usedToday)
    : isGemini
      ? formatGeminiMetaLine(lane, lane.usedToday)
      : isDoubao
        ? formatDoubaoMetaLine(lane, lane.usedToday)
        : "";
  const isSyncingStatus = Boolean(lane.pending && lane.pendingOp === "syncing_status");

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
            {(isQuotaTracked || isDoubao) && lane.creditsSynced && lane.memberLabel ? (
              <span
                className={cn(
                  "shrink-0 rounded-full border px-1.5 py-px text-[10px] font-medium leading-tight",
                  isPaidMemberTier(lane.memberTier)
                    ? "border-accent-border bg-accent-soft text-accent"
                    : "border-hairline bg-sunken text-muted",
                )}
              >
                {lane.memberLabel}
              </span>
            ) : null}
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

          {/* Today Usage, Qwen/Gemini Credits/Refresh/Concurrency & Active Job */}
          {isQuotaTracked ? (
            <Tooltip content={quotaTooltip} side="top">
              <div className="flex min-w-0 flex-col gap-0.5">
                <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
                  <span
                    data-testid={
                      isQwen ? `lane-qwen-credits-${lane.id}` : `lane-gemini-quota-${lane.id}`
                    }
                    className={cn(
                      "tnum max-w-full truncate text-[11px]",
                      creditTone === "danger"
                        ? "font-semibold text-status-danger"
                        : creditTone === "warn"
                          ? "font-semibold text-status-warn"
                          : creditTone === "ok"
                            ? "font-medium text-ink"
                            : "text-muted",
                    )}
                  >
                    {isQwen ? formatQwenCreditsPrimary(lane) : formatGeminiQuotaPrimary(lane)}
                  </span>
                  {lane.creditsSynced && (lane.usagePct > 0 || lane.creditsTotal > 0) ? (
                    <div className="w-[56px] shrink-0">
                      <Progress
                        value={usageProgress}
                        tone={isQwen ? qwenProgressTone(lane) : geminiProgressTone(lane)}
                      />
                    </div>
                  ) : null}
                  {lane.hasJob ? (
                    <span className="tnum shrink-0 truncate text-[11px] font-medium text-accent">
                      {`${formatJobOrder(lane.job.taskIndex)}${lane.job.elapsed ? ` · ${lane.job.elapsed}` : ""}`}
                    </span>
                  ) : null}
                </div>
                <span
                  data-testid={
                    isQwen ? `lane-qwen-meta-${lane.id}` : `lane-gemini-meta-${lane.id}`
                  }
                  className="tnum truncate text-[10.5px] text-subtle"
                >
                  {quotaMeta}
                </span>
              </div>
            </Tooltip>
          ) : isDoubao && lane.creditsSynced ? (
            <Tooltip content={quotaTooltip} side="top">
              <div className="flex min-w-0 flex-col gap-0.5">
                <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
                  <span
                    data-testid={`lane-doubao-status-${lane.id}`}
                    className={cn(
                      "tnum max-w-full truncate text-[11px]",
                      creditTone === "danger"
                        ? "font-semibold text-status-danger"
                        : creditTone === "warn"
                          ? "font-semibold text-status-warn"
                          : creditTone === "ok"
                            ? "font-medium text-ink"
                            : "text-muted",
                    )}
                  >
                    {formatDoubaoPrimary(lane)}
                  </span>
                  {lane.platformLimited || lane.usagePct > 0 || lane.creditsTotal > 0 ? (
                    <div className="w-[56px] shrink-0">
                      <Progress value={usageProgress} tone={doubaoProgressTone(lane)} />
                    </div>
                  ) : null}
                  {lane.hasJob ? (
                    <span className="tnum shrink-0 truncate text-[11px] font-medium text-accent">
                      {`${formatJobOrder(lane.job.taskIndex)}${lane.job.elapsed ? ` · ${lane.job.elapsed}` : ""}`}
                    </span>
                  ) : null}
                </div>
                <span
                  data-testid={`lane-doubao-meta-${lane.id}`}
                  className="tnum truncate text-[10.5px] text-subtle"
                >
                  {quotaMeta}
                </span>
              </div>
            </Tooltip>
          ) : (
            <Tooltip content={quotaTooltip} disabled={!quotaTooltip} side="top">
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
            </Tooltip>
          )}

          {/* Quick Actions */}
          <div
            data-testid={`lane-actions-${lane.id}`}
            className="flex shrink-0 flex-wrap items-center justify-end gap-1"
          >
            {isQwen ? (
              <Tooltip content="同步该千问账号的用户名、实时积分、刷新时间与会员状态" side="top">
                <IconButton
                  data-testid={`lane-sync-qwen-${lane.id}`}
                  icon="refresh-cw"
                  size={24}
                  variant="ghost"
                  disabled={Boolean(lane.pending) || running}
                  aria-label={`同步 ${lane.title || `账号 #${lane.id}`} 千问状态`}
                  className={cn(isSyncingStatus && "animate-spin text-accent")}
                  onClick={() => fireIpc(() => Browser.syncQwenStatus([lane.id]))}
                />
              </Tooltip>
            ) : isDoubao ? (
              <Tooltip content="同步该豆包账号的用户名、订阅方案、剩余额度与限额状态" side="top">
                <IconButton
                  data-testid={`lane-sync-doubao-${lane.id}`}
                  icon="refresh-cw"
                  size={24}
                  variant="ghost"
                  disabled={Boolean(lane.pending) || running}
                  aria-label={`同步 ${lane.title || `账号 #${lane.id}`} 豆包状态`}
                  className={cn(isSyncingStatus && "animate-spin text-accent")}
                  onClick={() => fireIpc(() => Browser.syncAccountStatus([lane.id]))}
                />
              </Tooltip>
            ) : isGemini ? (
              <Tooltip content="同步该 Gemini 账号的实时剩余额度、刷新时间、套餐与用户名" side="top">
                <IconButton
                  data-testid={`lane-sync-gemini-${lane.id}`}
                  icon="refresh-cw"
                  size={24}
                  variant="ghost"
                  disabled={Boolean(lane.pending) || running}
                  aria-label={`同步 ${lane.title || `账号 #${lane.id}`} Gemini 状态`}
                  className={cn(isSyncingStatus && "animate-spin text-accent")}
                  onClick={() => fireIpc(() => Browser.syncGeminiStatus([lane.id]))}
                />
              </Tooltip>
            ) : null}
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
  const isQwen = isQwenAccount(lane);
  const isDoubao = isDoubaoAccount(lane);
  const isGemini = isGeminiAccount(lane);
  const isQuotaTracked = isQwen || isGemini;
  const pillCredits = isQwen
    ? formatQwenCompactPillCredits(lane)
    : isGemini
      ? formatGeminiCompactPillQuota(lane)
      : isDoubao
        ? formatDoubaoCompactPill(lane)
        : "";
  const creditTone = isQwen
    ? qwenCreditTone(lane)
    : isGemini
      ? geminiQuotaTone(lane)
      : isDoubao
        ? doubaoStatusTone(lane)
        : "muted";
  const dotClass = isVerify
    ? "bg-status-warn"
    : lane.disabled
      ? "bg-subtle"
      : isWorking
        ? "bg-accent"
        : isQuotaTracked && lane.creditsSynced && lane.creditsRemain === 0
          ? "bg-status-danger"
          : isDoubao && lane.creditsSynced && (lane.platformLimited || lane.creditsRemain === 0)
            ? "bg-status-danger"
            : lane.state === "空闲"
              ? "bg-status-ok"
              : lane.state === "今日已满" || lane.state === "额度不足" || lane.state === "离线"
                ? "bg-status-danger"
                : "bg-status-warn";

  const tooltipText = isVerify
    ? "点击打开内置浏览器完成验证"
    : isQwen
      ? buildQwenStatusTooltip(lane, { todayCount: lane.usedToday, successToday: lane.successToday })
      : isGemini
        ? buildGeminiStatusTooltip(lane, { todayCount: lane.usedToday, successToday: lane.successToday })
        : isDoubao
          ? buildDoubaoStatusTooltip(lane, { todayCount: lane.usedToday, successToday: lane.successToday })
          : "";

  return (
    <Tooltip content={tooltipText} disabled={!tooltipText} side="top">
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
      ) : pillCredits ? (
        <span
          className={cn(
            "tnum text-[10px] font-semibold",
            creditTone === "danger"
              ? "text-status-danger"
              : creditTone === "warn"
                ? "text-status-warn"
                : "text-muted",
          )}
        >
          {pillCredits}
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

  const platform =
    wb?.platform || (wb?.gemini ? "Gemini" : wb?.qwenIntl ? "千问国际" : wb?.qianwen ? "千问" : "豆包");
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

  const isDoubaoPlatform = platform === "豆包";
  const isQwenPlatform = platform === "千问";
  const isGeminiPlatform = platform === "Gemini";
  const doubaoSummary = isDoubaoPlatform ? summarizeDoubaoPool(platformLanes) : null;
  const qwenSummary = isQwenPlatform ? summarizeQwenPool(platformLanes) : null;
  const geminiSummary = isGeminiPlatform ? summarizeGeminiPool(platformLanes) : null;
  const rowBaseHeight =
    isQwenPlatform || isGeminiPlatform || (isDoubaoPlatform && (doubaoSummary?.syncedAccounts ?? 0) > 0)
      ? LANE_ROW_QWEN_HEIGHT
      : LANE_ROW_HEIGHT;
  const listHeight = Math.min(Math.max(platformLanes.length, 1) * rowBaseHeight, 196);

  const useVirtualized = !collapsed && platformLanes.length > 24;

  const virtualizer = useVirtualizer({
    count: useVirtualized ? platformLanes.length : 0,
    getScrollElement: () => listScrollRef.current,
    estimateSize: (idx) => {
      const ln = platformLanes[idx];
      if (!ln) return rowBaseHeight;
      if (ln.usageErr || ln.lastError) return LANE_ROW_ERR_HEIGHT;
      return isQwenAccount(ln) || isGeminiAccount(ln) || (isDoubaoAccount(ln) && ln.creditsSynced)
        ? LANE_ROW_QWEN_HEIGHT
        : LANE_ROW_HEIGHT;
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
        : platform === "Gemini"
          ? "concurrencyAutoGemini"
          : "concurrencyAutoDoubao";
  const concurrencyMaxField =
    platform === "千问"
      ? "concurrencyMaxQianwen"
      : platform === "千问国际"
        ? "concurrencyMaxQwenIntl"
        : platform === "Gemini"
          ? "concurrencyMaxGemini"
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
            {isDoubaoPlatform && doubaoSummary && doubaoSummary.totalAccounts > 0 ? (
              <Tooltip
                content={
                  doubaoSummary.syncedAccounts > 0
                    ? doubaoSummary.vipCount > 0
                      ? `已同步 ${doubaoSummary.syncedAccounts}/${doubaoSummary.enabledAccounts || doubaoSummary.totalAccounts} 个豆包账号 · 订阅余 ${doubaoSummary.paidRemainSum}${doubaoSummary.paidTotalSum > 0 ? `/${doubaoSummary.paidTotalSum}` : ""} 额度${doubaoSummary.limitedCount > 0 ? ` · ${doubaoSummary.limitedCount} 个限额` : ""}${doubaoSummary.resetCardSum > 0 ? ` · 重置卡 ${doubaoSummary.resetCardSum} 张` : ""}`
                      : `已同步 ${doubaoSummary.syncedAccounts}/${doubaoSummary.enabledAccounts || doubaoSummary.totalAccounts} 个豆包账号（免费动态限额）${doubaoSummary.limitedCount > 0 ? ` · ${doubaoSummary.limitedCount} 个已触发限额` : doubaoSummary.maxUsagePercent > 0 ? ` · 窗口最高已用 ${doubaoSummary.maxUsagePercent}%` : " · 额度正常"}${doubaoSummary.resetCardSum > 0 ? ` · 重置卡 ${doubaoSummary.resetCardSum} 张` : ""}`
                    : "豆包实时状态尚未同步，点击右侧「同步」读取用户名、订阅方案与限额状态"
                }
                side="bottom"
              >
                <span
                  data-testid="lanes-doubao-summary"
                  className={cn(
                    "tnum inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[10.5px] font-medium",
                    doubaoSummary.syncedAccounts === 0
                      ? "border-hairline bg-sunken text-muted"
                      : doubaoSummary.limitedCount > 0
                        ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
                        : doubaoSummary.maxUsagePercent >= 80
                          ? "border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] text-status-warn"
                          : "border-accent-border bg-accent-soft text-accent",
                  )}
                >
                  {doubaoSummary.syncedAccounts === 0
                    ? "豆包状态待同步"
                    : doubaoSummary.vipCount > 0
                      ? `余 ${doubaoSummary.paidRemainSum}${doubaoSummary.paidTotalSum > 0 ? `/${doubaoSummary.paidTotalSum}` : ""} 额度${doubaoSummary.earliestResetLabel ? ` · ${doubaoSummary.earliestResetLabel}` : ""}`
                      : doubaoSummary.limitedCount > 0
                        ? `已限额 ${doubaoSummary.limitedCount} 个${doubaoSummary.earliestResetLabel ? ` · ${doubaoSummary.earliestResetLabel}` : ""}`
                        : doubaoSummary.maxUsagePercent > 0
                          ? `窗口最高 ${doubaoSummary.maxUsagePercent}%${doubaoSummary.resetCardSum > 0 ? ` · 重置卡 ${doubaoSummary.resetCardSum}` : ""}`
                          : `额度正常${doubaoSummary.resetCardSum > 0 ? ` · 重置卡 ${doubaoSummary.resetCardSum}` : ""}`}
                </span>
              </Tooltip>
            ) : null}
            {isQwenPlatform && qwenSummary && qwenSummary.totalAccounts > 0 ? (
              <Tooltip
                content={
                  qwenSummary.syncedAccounts > 0
                    ? `已同步 ${qwenSummary.syncedAccounts}/${qwenSummary.enabledAccounts || qwenSummary.totalAccounts} 个千问账号 · 总剩余 ${qwenSummary.remainSum} 积分（约可生图 ${qwenSummary.estImages} 张，${qwenSummary.costPerImage} 积分/张）${qwenSummary.earliestResetLabel ? ` · 最近刷新 ${qwenSummary.earliestResetLabel}` : ""}`
                    : "千问积分尚未同步，点击右侧「同步」读取用户名、实时积分、刷新时间与会员并发上限"
                }
                side="bottom"
              >
                <span
                  data-testid="lanes-qwen-summary"
                  className={cn(
                    "tnum inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[10.5px] font-medium",
                    qwenSummary.syncedAccounts === 0
                      ? "border-hairline bg-sunken text-muted"
                      : qwenSummary.remainSum === 0
                        ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
                        : "border-accent-border bg-accent-soft text-accent",
                  )}
                >
                  {qwenSummary.syncedAccounts > 0
                    ? `余 ${qwenSummary.remainSum}${qwenSummary.totalSum > 0 ? `/${qwenSummary.totalSum}` : ""} 积分${qwenSummary.earliestResetLabel ? ` · ${qwenSummary.earliestResetLabel}` : ""}`
                    : "千问积分待同步"}
                </span>
              </Tooltip>
            ) : null}
            {isGeminiPlatform && geminiSummary && geminiSummary.totalAccounts > 0 ? (
              <Tooltip
                content={
                  geminiSummary.syncedAccounts > 0
                    ? geminiSummary.meteredAccounts > 0
                      ? `已同步 ${geminiSummary.syncedAccounts}/${geminiSummary.enabledAccounts || geminiSummary.totalAccounts} 个 Gemini 账号 · 剩余额度均值 ${geminiSummary.avgRemainPct}%${geminiSummary.exhaustedCount > 0 ? `（${geminiSummary.exhaustedCount} 个已耗尽）` : ""}${geminiSummary.earliestResetLabel ? ` · 最近刷新 ${geminiSummary.earliestResetLabel}` : ""}`
                      : geminiSummary.aiCreditsSum > 0
                        ? `已同步 ${geminiSummary.syncedAccounts} 个 Gemini 账号 · 剩余 ${geminiSummary.aiCreditsSum} AI积分`
                        : `已同步 ${geminiSummary.syncedAccounts} 个 Gemini 账号状态`
                    : "Gemini 额度尚未同步，点击右侧「同步」读取用户名、实时剩余额度、刷新时间与套餐"
                }
                side="bottom"
              >
                <span
                  data-testid="lanes-gemini-summary"
                  className={cn(
                    "tnum inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[10.5px] font-medium",
                    geminiSummary.syncedAccounts === 0
                      ? "border-hairline bg-sunken text-muted"
                      : geminiSummary.meteredAccounts > 0 && geminiSummary.avgRemainPct === 0
                        ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
                        : "border-accent-border bg-accent-soft text-accent",
                  )}
                >
                  {geminiSummary.meteredAccounts > 0
                    ? `${geminiSummary.meteredAccounts === 1 ? "余" : "均余"} ${geminiSummary.avgRemainPct}%${geminiSummary.earliestResetLabel ? ` · ${geminiSummary.earliestResetLabel}` : ""}`
                    : geminiSummary.aiCreditsSum > 0
                      ? `余 ${geminiSummary.aiCreditsSum} AI积分`
                      : geminiSummary.syncedAccounts > 0
                        ? `已同步 ${geminiSummary.syncedAccounts} 个`
                        : "Gemini 额度待同步"}
                </span>
              </Tooltip>
            ) : null}
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
                    ? isQwenPlatform
                      ? `千问已开启自动并发，最多同时运行 ${concurrencyMax} 个千问账号（免费单号突发上限 6、建议 ≤2；会员不限并发；运行中也可实时调整）`
                      : `${platform}已开启自动并发，最多同时运行 ${concurrencyMax} 个${platform}账号（与其他平台独立，运行中也可实时调整）`
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
                    content={
                      isQwenPlatform
                        ? "千问调度并发上限（2–8）；免费单号突发上限 6（建议 1–2），创作会员不限并发"
                        : `${platform}并发上限（2–8，与其他平台独立，运行中也可实时调整）`
                    }
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
            {isDoubaoPlatform && platformLanes.length > 0 ? (
              <Tooltip
                content={
                  wb.running
                    ? "任务运行中，完成后会自动刷新豆包状态"
                    : "同步豆包账号的用户名、订阅方案、剩余额度 / 窗口占用与限额状态"
                }
                side="bottom"
              >
                <Button
                  data-testid="lanes-sync-doubao-btn"
                  text="同步"
                  size="small"
                  variant="ghost"
                  disabled={Boolean(wb.running)}
                  onClick={() =>
                    fireIpc(() => Browser.syncAccountStatus(platformLanes.map((l) => l.id)))
                  }
                />
              </Tooltip>
            ) : isQwenPlatform && platformLanes.length > 0 ? (
              <Tooltip
                content={
                  wb.running
                    ? "任务运行中，完成后会自动刷新千问积分"
                    : "同步千问账号的用户名、实时积分、刷新时间、会员套餐与并发上限"
                }
                side="bottom"
              >
                <Button
                  data-testid="lanes-sync-qwen-btn"
                  text="同步"
                  size="small"
                  variant="ghost"
                  disabled={Boolean(wb.running)}
                  onClick={() =>
                    fireIpc(() => Browser.syncQwenStatus(platformLanes.map((l) => l.id)))
                  }
                />
              </Tooltip>
            ) : isGeminiPlatform && platformLanes.length > 0 ? (
              <Tooltip
                content={
                  wb.running
                    ? "任务运行中，完成后会自动刷新 Gemini 额度"
                    : "同步 Gemini 账号的用户名、实时剩余额度、刷新时间与订阅套餐"
                }
                side="bottom"
              >
                <Button
                  data-testid="lanes-sync-gemini-btn"
                  text="同步"
                  size="small"
                  variant="ghost"
                  disabled={Boolean(wb.running)}
                  onClick={() =>
                    fireIpc(() => Browser.syncGeminiStatus(platformLanes.map((l) => l.id)))
                  }
                />
              </Tooltip>
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
