import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
// Brand marks from @lobehub/icons (MIT).
import doubaoMark from "@/assets/providers/doubao-avatar.png";
import geminiMark from "@/assets/providers/gemini-color.svg";
import qwenMark from "@/assets/providers/qwen-color.svg";
import qwenIntlMark from "@/assets/providers/qwen-intl-color.svg";
import { AccountColumnHeader } from "@/components/browser/AccountColumnHeader";
import {
  DEFAULT_ACCOUNT_FILTERS,
  DEFAULT_ACCOUNT_SORT,
  filterAccountRows,
  hasActiveAccountFilters,
  isColumnFilterActive,
  resetColumnFilter,
  sortAccountRows,
  type AccountColumnKey,
  type AccountFiltersState,
  type AccountSortState,
  type SortDirection,
} from "@/components/browser/accountTableFilterSort";
import {
  AccountListFrame,
  AccountViewButton,
  EmptyNotice,
  FlushPane,
  PageToolbar,
  PaneHeader,
  StatusBadge,
} from "@/components/primitives";
import {
  Button,
  Checkbox,
  Dialog,
  Icon,
  IconButton,
  Input,
  Progress,
  Switch,
  Tabs,
  Tooltip,
  TooltipGroup,
} from "@/components/ui";
import { FingerprintDialog } from "@/components/browser/FingerprintDialog";
import { DEFAULT_BROWSER_STATE } from "@/lib/browserRuntime";
import { cn } from "@/lib/cn";
import { Browser, formatCallError } from "@/lib/ipc";
import {
  buildDoubaoStatusTooltip,
  buildGeminiStatusTooltip,
  buildQwenStatusTooltip,
  doubaoProgressTone,
  doubaoStatusTone,
  formatDoubaoMetaLine,
  formatDoubaoPrimary,
  formatGeminiMetaLine,
  formatGeminiQuotaPrimary,
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
} from "@/lib/qwenStatus";
import { useEscapeHandler } from "@/lib/shortcuts";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import {
  TAB_SETTINGS,
  dismissSharedExitHints,
  goTab,
  refreshBrowser,
  showToast,
  updateSettingField,
  useBrowser,
  useDismissedSharedExitHints,
  usePageLoading,
  type BrowserRowDTO,
} from "@/store";

const PLATFORM_OPTIONS = [
  { value: "豆包", label: "豆包" },
  { value: "千问", label: "千问" },
  { value: "千问国际", label: "千问国际" },
  { value: "Gemini", label: "Gemini" },
];

const PROVIDER_MARKS: Record<string, string> = {
  豆包: doubaoMark,
  千问: qwenMark,
  千问国际: qwenIntlMark,
  Gemini: geminiMark,
};

function invokeSafe(call: () => Promise<unknown>, after?: () => Promise<unknown>): void {
  try {
    void Promise.resolve(call())
      .then(() => (after ? after() : undefined))
      .catch(() => {
        // Ignore runtime-unavailable rejections outside MyGo WebView (e.g. Vite preview)
      });
  } catch {
    // Ignore synchronous runtime-unavailable errors
  }
}

function formatPendingOp(op: string): string {
  if (op === "starting") return "打开中";
  if (op === "stopping") return "关闭中";
  if (op === "clearing") return "清理中";
  if (op === "syncing") return "同步中";
  if (op === "syncing_status") return "同步状态中";
  return op;
}

const KEEP_RECENT_PRESETS = [
  { value: 0, label: "全部清空" },
  { value: 5, label: "保留 5 条" },
  { value: 10, label: "保留 10 条" },
  { value: 20, label: "保留 20 条" },
];

function RenameAccountDialog({
  target,
  openSeq,
  syncDisabled,
  onClose,
  onCommit,
  onSyncUsername,
}: {
  target: BrowserRowDTO | null;
  openSeq: number;
  syncDisabled?: boolean;
  onClose: () => void;
  onCommit: (id: number, value: string) => void;
  onSyncUsername?: (row: BrowserRowDTO) => void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const draft = useSyncedDraft(
    target?.name ?? "",
    target ? `${target.id}:${openSeq}` : "closed",
    () => {},
    { debounceMs: 0 },
  );

  useEffect(() => {
    if (!target) return;
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [target, openSeq]);

  const submit = () => {
    if (!target) return;
    draft.flush();
    onCommit(target.id, draft.value);
  };

  return (
    <Dialog
      open={Boolean(target)}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title="重命名账号"
      description={
        target
          ? `修改 ${target.platform} 账号「${target.name || `账号 #${target.id}`}」的备注名称`
          : undefined
      }
      width={420}
      closeOnBackdrop
      closeOnEscape
      data-testid="browser-rename-dialog"
      footer={
        <>
          <Button text="取消" size="small" variant="ghost" onClick={onClose} />
          <Button
            data-testid="browser-rename-save-btn"
            text="保存"
            size="small"
            variant="primary"
            onClick={submit}
          />
        </>
      }
    >
      {target ? (
        <div className="flex w-full min-w-0 flex-col gap-1.5">
          <div className="flex items-center justify-between gap-2">
            <label htmlFor="browser-rename-input" className="text-xs font-medium text-ink">
              账号备注名
            </label>
            {onSyncUsername ? (
              <Button
                data-testid="browser-rename-sync-btn"
                text="重新抓取用户名"
                size="xs"
                icon="refresh-cw"
                variant="ghost"
                disabled={Boolean(syncDisabled)}
                onClick={() => onSyncUsername(target)}
              />
            ) : null}
          </div>
          <Input
            id="browser-rename-input"
            ref={inputRef}
            data-testid={`browser-name-${target.id}`}
            autoFocus
            value={draft.value}
            placeholder="请输入账号备注名"
            aria-label="编辑账号备注名"
            onChange={(e) => draft.onChange(e.target.value)}
            onCompositionStart={draft.onCompositionStart}
            onCompositionEnd={(e) => draft.onCompositionEnd(e.currentTarget.value)}
            onBlur={draft.onBlur}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                e.preventDefault();
                submit();
              }
            }}
          />
        </div>
      ) : null}
    </Dialog>
  );
}

function ProxyAccountDialog({
  target,
  openSeq,
  errorText,
  saving,
  onClose,
  onSave,
}: {
  target: BrowserRowDTO | null;
  openSeq: number;
  errorText?: string;
  saving: boolean;
  onClose: () => void;
  onSave: (id: number, value: string) => void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const draft = useSyncedDraft(
    target?.proxy ?? "",
    target ? `${target.id}:${openSeq}` : "closed",
    () => {},
    { debounceMs: 0 },
  );

  useEffect(() => {
    if (!target) return;
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [target, openSeq]);

  const submit = () => {
    if (!target || saving) return;
    draft.flush();
    onSave(target.id, draft.value);
  };

  return (
    <Dialog
      open={Boolean(target)}
      onOpenChange={(open) => {
        if (!open && !saving) onClose();
      }}
      title="配置独立代理"
      description={
        target
          ? `为 ${target.platform} 账号「${target.name || `账号 #${target.id}`}」配置专属代理（留空跟随全局设置）`
          : undefined
      }
      width={460}
      closeOnBackdrop={!saving}
      closeOnEscape={!saving}
      data-testid="browser-proxy-dialog"
      footer={
        <>
          <Button text="取消" size="small" variant="ghost" disabled={saving} onClick={onClose} />
          <Button
            data-testid="browser-proxy-save-btn"
            text="保存代理"
            size="small"
            variant="primary"
            busy={saving}
            onClick={submit}
          />
        </>
      }
    >
      {target ? (
        <div className="flex w-full min-w-0 flex-col gap-1.5">
          <div className="flex items-center justify-between gap-2">
            <label htmlFor="browser-proxy-input" className="text-xs font-medium text-ink">
              代理地址
            </label>
            {draft.value.trim() ? (
              <Button
                text="清空（跟随全局）"
                size="xs"
                variant="ghost"
                disabled={saving}
                onClick={() => {
                  draft.onChange("");
                  inputRef.current?.focus();
                }}
              />
            ) : null}
          </div>
          <Input
            id="browser-proxy-input"
            ref={inputRef}
            data-testid={`browser-proxy-input-${target.id}`}
            autoFocus
            value={draft.value}
            disabled={saving}
            placeholder="专属代理（留空跟随全局设置），如 http://127.0.0.1:7890"
            aria-label="编辑独立代理"
            onChange={(e) => draft.onChange(e.target.value)}
            onCompositionStart={draft.onCompositionStart}
            onCompositionEnd={(e) => draft.onCompositionEnd(e.currentTarget.value)}
            onBlur={draft.onBlur}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                e.preventDefault();
                submit();
              }
            }}
            className="font-mono-console"
          />
          {errorText ? (
            <span
              data-testid={`browser-proxy-err-${target.id}`}
              className="select-text text-[11px] text-status-danger"
            >
              {errorText}
            </span>
          ) : null}
        </div>
      ) : null}
    </Dialog>
  );
}

function AccountTableRows({
  row,
  platformConcurrencyLabel,
  pageRunning,
  clearingBusy,
  syncingStatusBusy,
  onRequestRename,
  onRequestProxy,
  onRequestFingerprint,
  onToggleCheck,
  onRowAction,
  onSyncQwenRow,
  onSyncDoubaoRow,
  onSyncGeminiRow,
  onRequestClearHistory,
  onRequestDelete,
}: {
  row: BrowserRowDTO;
  platformConcurrencyLabel?: string;
  pageRunning: boolean;
  clearingBusy: boolean;
  syncingStatusBusy: boolean;
  onRequestRename: (row: BrowserRowDTO) => void;
  onRequestProxy: (row: BrowserRowDTO) => void;
  onRequestFingerprint: (row: BrowserRowDTO) => void;
  onToggleCheck: (id: number, checked: boolean) => void;
  onRowAction: (id: number, action: string) => void;
  onSyncQwenRow: (row: BrowserRowDTO) => void;
  onSyncDoubaoRow: (row: BrowserRowDTO) => void;
  onSyncGeminiRow: (row: BrowserRowDTO) => void;
  onRequestClearHistory: (row: BrowserRowDTO) => void;
  onRequestDelete: (row: BrowserRowDTO) => void;
}) {
  const showErrorRow = Boolean(row.lastError);
  const hasSubRows = showErrorRow;
  const platMark = PROVIDER_MARKS[row.platform];
  const isQwen = isQwenAccount(row);
  const isDoubao = isDoubaoAccount(row);
  const isGemini = isGeminiAccount(row);
  const isQuotaTracked = isQwen || isGemini;
  const creditTone = isQwen
    ? qwenCreditTone(row)
    : isGemini
      ? geminiQuotaTone(row)
      : isDoubao
        ? doubaoStatusTone(row)
        : "muted";
  const usageText = row.usageLabel
    ? `${row.todayCount} 次 · ${row.usageLabel}`
    : `${row.todayCount} 次`;
  const qwenEstImages =
    isQwen && row.creditsSynced && (row.creditsRemain ?? -1) >= 0
      ? Math.floor((row.creditsRemain ?? 0) / Math.max(1, row.creditCost || 1))
      : null;

  return (
    <Fragment>
      <tr
        data-testid={`browser-row-${row.id}`}
        className={cn(
          "transition-colors duration-150",
          row.disabled ? "bg-sunken/40 text-muted" : "hover:bg-sunken/70",
          !hasSubRows && "border-b border-divider",
        )}
      >
        {/* 选择 */}
        <td className="w-[42px] py-2.5 pl-4 pr-2 align-middle">
          <Checkbox
            size="small"
            checked={Boolean(row.checked)}
            aria-label={`选择 ${row.name || row.platform}`}
            onCheckedChange={(checked) => onToggleCheck(row.id, checked)}
          />
        </td>

        {/* 启用/禁用 */}
        <td className="w-[72px] px-2.5 py-2.5 align-middle">
          <div className="flex items-center">
            <Switch
              data-testid={`browser-toggle-${row.id}`}
              size="small"
              checked={!row.disabled}
              aria-label={`启用或禁用 ${row.name || row.platform}`}
              onCheckedChange={() => onRowAction(row.id, "toggleDisabled")}
            />
          </div>
        </td>

        {/* 账号（点击弹窗重命名） */}
        <td className="w-[21%] min-w-[156px] px-2.5 py-2.5 align-middle">
          <div className="flex min-w-0 items-center gap-2">
            <Tooltip content="点击重命名" side="top">
              <button
                type="button"
                onClick={() => onRequestRename(row)}
                className="group/name inline-flex min-w-0 max-w-full cursor-pointer items-center gap-1 rounded-[6px] px-1.5 py-0.5 text-left transition-colors hover:bg-sunken-hover"
              >
                <span
                  className={cn(
                    "truncate text-xs font-semibold transition-colors group-hover/name:text-accent",
                    row.disabled ? "text-muted line-through" : "text-ink",
                  )}
                >
                  {row.name || `账号 #${row.id}`}
                </span>
                {row.email ? (
                  <span className="truncate text-[11px] text-muted">{`(${row.email})`}</span>
                ) : null}
                <Icon
                  name="pencil"
                  size={11}
                  className="touch-visible shrink-0 text-subtle opacity-0 transition-opacity group-focus-within/name:opacity-100 group-hover/name:opacity-100"
                />
              </button>
            </Tooltip>
          </div>
        </td>

        {/* 平台 */}
        <td className="w-[12%] min-w-[96px] px-2.5 py-2.5 align-middle whitespace-nowrap">
          <div className="flex flex-col gap-0.5">
            <div className="inline-flex items-center gap-1.5">
              {platMark ? (
                <img
                  src={platMark}
                  alt=""
                  aria-hidden="true"
                  className="size-3.5 shrink-0 object-contain"
                  draggable={false}
                />
              ) : null}
              <span className="text-xs font-medium text-ink">{row.platform || "未知"}</span>
            </div>
            {platformConcurrencyLabel ? (
              <span
                data-testid={`browser-platform-concurrency-${row.id}`}
                className="tnum text-[10.5px] leading-snug text-subtle"
              >
                {platformConcurrencyLabel}
              </span>
            ) : null}
          </div>
        </td>

        {/* 状态 */}
        <td className="w-[12%] min-w-[104px] px-2.5 py-2.5 align-middle whitespace-nowrap">
          <div className="inline-flex items-center gap-1.5">
            <StatusBadge
              text={row.health || "未知"}
              tone={row.healthTone}
              dot
              pulseDot={Boolean(row.active || row.pending)}
            />
            {row.pendingOp ? (
              <span className="text-[11px] text-muted">{formatPendingOp(row.pendingOp)}</span>
            ) : null}
          </div>
        </td>

        {/* 今日用量 / 千问与 Gemini 实时额度与刷新 */}
        <td className="w-[24%] min-w-[208px] px-2.5 py-2.5 align-middle">
          {isQuotaTracked ? (
            <Tooltip
              content={
                isQwen
                  ? buildQwenStatusTooltip(row, { todayCount: row.todayCount })
                  : buildGeminiStatusTooltip(row, { todayCount: row.todayCount })
              }
              side="top"
            >
              <div className="flex flex-col gap-1">
                <div className="flex flex-wrap items-center gap-1.5 whitespace-nowrap">
                  <span
                    data-testid={
                      isQwen ? `browser-qwen-credits-${row.id}` : `browser-gemini-quota-${row.id}`
                    }
                    className={cn(
                      "tnum text-xs",
                      creditTone === "danger"
                        ? "font-semibold text-status-danger"
                        : creditTone === "warn"
                          ? "font-semibold text-status-warn"
                          : creditTone === "ok"
                            ? "font-medium text-ink"
                            : "text-muted",
                    )}
                  >
                    {isQwen ? formatQwenCreditsPrimary(row) : formatGeminiQuotaPrimary(row)}
                  </span>
                  {qwenEstImages !== null ? (
                    <span className="tnum text-[11px] text-muted">{`约 ${qwenEstImages} 张`}</span>
                  ) : null}
                  {row.creditsSynced && row.memberLabel ? (
                    <span
                      className={cn(
                        "rounded-full border px-1.5 py-px text-[10px] font-medium leading-tight",
                        isPaidMemberTier(row.memberTier)
                          ? "border-accent-border bg-accent-soft text-accent"
                          : "border-hairline bg-sunken text-muted",
                      )}
                    >
                      {row.memberLabel}
                    </span>
                  ) : null}
                </div>
                {row.creditsSynced && (row.usagePct > 0 || row.creditsTotal > 0) ? (
                  <div className="w-[88px]">
                    <Progress
                      value={
                        row.creditsRemain === 0
                          ? 1
                          : Math.min(1, Math.max(0, row.usagePct) / 100)
                      }
                      tone={isQwen ? qwenProgressTone(row) : geminiProgressTone(row)}
                      aria-label={isQwen ? `积分已用 ${row.usagePct}%` : `额度已用 ${row.usagePct}%`}
                    />
                  </div>
                ) : null}
                <span
                  data-testid={
                    isQwen ? `browser-qwen-meta-${row.id}` : `browser-gemini-meta-${row.id}`
                  }
                  className="tnum text-[10.5px] leading-snug text-subtle"
                >
                  {isQwen
                    ? formatQwenMetaLine(row, row.todayCount)
                    : formatGeminiMetaLine(row, row.todayCount)}
                  {row.creditsSyncedLabel ? ` · ${row.creditsSyncedLabel}` : ""}
                </span>
              </div>
            </Tooltip>
          ) : isDoubao ? (
            <Tooltip content={buildDoubaoStatusTooltip(row, { todayCount: row.todayCount })} side="top">
              {row.creditsSynced ? (
                <div className="flex flex-col gap-1">
                  <div className="flex flex-wrap items-center gap-1.5 whitespace-nowrap">
                    <span
                      data-testid={`browser-doubao-status-${row.id}`}
                      className={cn(
                        "tnum text-xs",
                        creditTone === "danger"
                          ? "font-semibold text-status-danger"
                          : creditTone === "warn"
                            ? "font-semibold text-status-warn"
                            : creditTone === "ok"
                              ? "font-medium text-ink"
                              : "text-muted",
                      )}
                    >
                      {formatDoubaoPrimary(row)}
                    </span>
                    {row.memberLabel ? (
                      <span
                        className={cn(
                          "rounded-full border px-1.5 py-px text-[10px] font-medium leading-tight",
                          isPaidMemberTier(row.memberTier)
                            ? "border-accent-border bg-accent-soft text-accent"
                            : "border-hairline bg-sunken text-muted",
                        )}
                      >
                        {row.memberLabel}
                      </span>
                    ) : null}
                  </div>
                  {row.platformLimited || row.usagePct > 0 || row.creditsTotal > 0 ? (
                    <div className="w-[88px]">
                      <Progress
                        value={
                          row.platformLimited || row.creditsRemain === 0
                            ? 1
                            : Math.min(1, Math.max(0, row.usagePct) / 100)
                        }
                        tone={doubaoProgressTone(row)}
                        aria-label={`豆包额度已用 ${row.usagePct}%`}
                      />
                    </div>
                  ) : null}
                  <span
                    data-testid={`browser-doubao-meta-${row.id}`}
                    className="tnum text-[10.5px] leading-snug text-subtle"
                  >
                    {formatDoubaoMetaLine(row, row.todayCount)}
                    {(row.resetCardCount ?? 0) === 0 ? " · 重置卡 0 张" : ""}
                    {row.creditsSyncedLabel ? ` · ${row.creditsSyncedLabel}` : ""}
                  </span>
                </div>
              ) : (
                <div className="flex flex-col gap-1 whitespace-nowrap">
                  <span className="tnum text-xs text-ink">{usageText}</span>
                  {row.usagePct > 0 ? (
                    <div className="w-[72px]">
                      <Progress
                        value={Math.min(1, row.usagePct / 100)}
                        tone={row.usagePct >= 95 ? "danger" : row.usagePct >= 80 ? "warn" : "accent"}
                        aria-label={`今日用量 ${row.usagePct}%`}
                      />
                    </div>
                  ) : null}
                  <span
                    data-testid={`browser-doubao-meta-${row.id}`}
                    className="tnum text-[10.5px] leading-snug text-subtle"
                  >
                    免费动态限额 · 待同步
                  </span>
                </div>
              )}
            </Tooltip>
          ) : (
            <div className="flex flex-col gap-1 whitespace-nowrap">
              <span className="tnum text-xs text-ink">{usageText}</span>
              {row.usagePct > 0 ? (
                <div className="w-[72px]">
                  <Progress
                    value={Math.min(1, row.usagePct / 100)}
                    tone={row.usagePct >= 95 ? "danger" : row.usagePct >= 80 ? "warn" : "accent"}
                    aria-label={`今日用量 ${row.usagePct}%`}
                  />
                </div>
              ) : null}
            </div>
          )}
        </td>

        {/* 独立代理 */}
        <td className="w-[16%] min-w-[120px] max-w-[200px] px-2.5 py-2.5 align-middle">
          <Tooltip
            content={
              row.proxy
                ? `点击编辑独立代理：${row.proxy}`
                : "点击配置独立代理（当前跟随全局）"
            }
            side="top"
          >
            <button
              type="button"
              data-testid={`browser-proxy-btn-${row.id}`}
              aria-label={
                row.proxy
                  ? `独立代理 ${row.proxy}，点击修改`
                  : `配置 ${row.name || row.platform} 独立代理`
              }
              onClick={() => onRequestProxy(row)}
              className="group/proxy inline-flex max-w-full cursor-pointer items-center gap-1 rounded-[6px] px-1.5 py-0.5 text-left transition-colors hover:bg-sunken-hover"
            >
            <span
              className={cn(
                "truncate text-[11px]",
                row.proxy ? "font-mono-console text-ink" : "text-subtle",
              )}
            >
              {row.proxy || "跟随全局"}
            </span>
            <Icon
              name="pencil"
              size={11}
              className="shrink-0 text-subtle opacity-50 transition-opacity group-hover/proxy:opacity-100"
            />
            </button>
          </Tooltip>
        </td>

        {/* 操作 */}
        <td className="w-[1%] py-2.5 pl-2.5 pr-4 text-right align-middle whitespace-nowrap">
          <TooltipGroup className="inline-flex items-center justify-end gap-1">
            <Tooltip content="指纹设置" side="top">
              <IconButton icon="sliders-horizontal" size="xs" variant="ghost"
                aria-label={`编辑 ${row.name || row.platform} 指纹设置`}
                onClick={() => onRequestFingerprint(row)} />
            </Tooltip>
            {isQwen ? (
              <Tooltip
                content={
                  pageRunning
                    ? "任务运行中，完成后会自动刷新千问积分"
                    : "同步该千问账号的用户名、实时积分、刷新时间与会员并发上限"
                }
                side="top"
              >
                <IconButton
                  data-testid={`browser-sync-qwen-row-${row.id}`}
                  icon="refresh-cw"
                  size="xs"
                  variant="ghost"
                  aria-label={`同步 ${row.name || `账号 #${row.id}`} 千问状态`}
                  disabled={Boolean(row.pending || pageRunning || clearingBusy || syncingStatusBusy)}
                  className={cn(row.pending && row.pendingOp === "syncing_status" && "animate-spin text-accent")}
                  onClick={() => onSyncQwenRow(row)}
                />
              </Tooltip>
            ) : isDoubao ? (
              <Tooltip
                content={
                  pageRunning
                    ? "任务运行中，完成后会自动刷新豆包状态"
                    : "同步该豆包账号的用户名、订阅方案与实时限额状态"
                }
                side="top"
              >
                <IconButton
                  data-testid={`browser-sync-doubao-row-${row.id}`}
                  icon="refresh-cw"
                  size="xs"
                  variant="ghost"
                  aria-label={`同步 ${row.name || `账号 #${row.id}`} 豆包状态`}
                  disabled={Boolean(row.pending || pageRunning || clearingBusy || syncingStatusBusy)}
                  className={cn(row.pending && row.pendingOp === "syncing_status" && "animate-spin text-accent")}
                  onClick={() => onSyncDoubaoRow(row)}
                />
              </Tooltip>
            ) : isGemini ? (
              <Tooltip
                content={
                  pageRunning
                    ? "任务运行中，完成后会自动刷新 Gemini 额度"
                    : "同步该 Gemini 账号的实时剩余额度、刷新时间、套餐与用户名"
                }
                side="top"
              >
                <IconButton
                  data-testid={`browser-sync-gemini-row-${row.id}`}
                  icon="refresh-cw"
                  size="xs"
                  variant="ghost"
                  aria-label={`同步 ${row.name || `账号 #${row.id}`} Gemini 状态`}
                  disabled={Boolean(row.pending || pageRunning || clearingBusy || syncingStatusBusy)}
                  className={cn(row.pending && row.pendingOp === "syncing_status" && "animate-spin text-accent")}
                  onClick={() => onSyncGeminiRow(row)}
                />
              </Tooltip>
            ) : null}
            <AccountViewButton accountId={row.id} name={row.name} iconOnly size="xs" />
            {row.pending && row.pendingOp === "starting" ? (
              <Tooltip content="打开中..." side="top">
                <IconButton
                  icon="play"
                  size="xs"
                  variant={row.isLoggedIn ? "outline" : "primary"}
                  aria-label="打开中"
                  disabled
                  busy
                />
              </Tooltip>
            ) : row.isRunning ? (
              <Tooltip
                content={row.pending && row.pendingOp === "stopping" ? "关闭中..." : "关闭窗口"}
                side="top"
              >
                <IconButton
                  icon="square"
                  size="xs"
                  variant="outline"
                  aria-label={row.pending && row.pendingOp === "stopping" ? "关闭中" : "关闭"}
                  disabled={Boolean(row.pending)}
                  busy={Boolean(row.pending && row.pendingOp === "stopping")}
                  onClick={() => onRowAction(row.id, "stop")}
                />
              </Tooltip>
            ) : (
              <Tooltip content={row.isLoggedIn ? "打开窗口" : "打开登录"} side="top">
                <IconButton
                  icon="play"
                  size="xs"
                  variant={row.isLoggedIn ? "outline" : "primary"}
                  aria-label={row.isLoggedIn ? "打开" : "打开登录"}
                  disabled={Boolean(row.pending)}
                  onClick={() => onRowAction(row.id, "launch")}
                />
              </Tooltip>
            )}

            {row.lastError ? (
              <Tooltip content="清除冷却" side="top">
                <IconButton
                  icon="rotate-ccw"
                  size="xs"
                  variant="outline"
                  aria-label="清除冷却"
                  onClick={() => onRowAction(row.id, "restore")}
                />
              </Tooltip>
            ) : null}

            {row.pending && row.pendingOp === "clearing" ? (
              <Tooltip content="正在清理对话..." side="top">
                <IconButton
                  data-testid={`browser-clear-history-btn-${row.id}`}
                  icon="eraser"
                  size="xs"
                  variant="outline"
                  aria-label="清理中"
                  disabled
                  busy
                />
              </Tooltip>
            ) : (
              <Tooltip
                content={
                  pageRunning
                    ? "任务运行中，暂不可清理"
                    : clearingBusy
                      ? "正在清理其他账号对话，请稍候"
                      : `清理 ${row.platform} 账号「${row.name || `账号 #${row.id}`}」的网页端聊天记录`
                }
                side="top"
              >
                <IconButton
                  data-testid={`browser-clear-history-btn-${row.id}`}
                  icon="eraser"
                  size="xs"
                  variant="ghost"
                  aria-label="清理对话"
                  disabled={Boolean(row.pending || pageRunning || clearingBusy)}
                  onClick={() => onRequestClearHistory(row)}
                />
              </Tooltip>
            )}

            <Tooltip content={`删除账号「${row.name || `账号 #${row.id}`}」`} side="top">
              <IconButton
                data-testid={`browser-delete-btn-${row.id}`}
                icon="trash-2"
                size="xs"
                variant="ghost"
                aria-label="删除"
                className="hover:text-status-danger"
                disabled={Boolean(
                  row.pending &&
                    (row.pendingOp === "clearing" ||
                      row.pendingOp === "syncing" ||
                      row.pendingOp === "syncing_status"),
                )}
                onClick={() => onRequestDelete(row)}
              />
            </Tooltip>
          </TooltipGroup>
        </td>
      </tr>

      {showErrorRow ? (
        <tr className="border-b border-divider">
          <td colSpan={2} />
          <td colSpan={6} className="px-2.5 pb-2 pt-0">
            <div className="inline-flex items-center gap-1.5 rounded-[8px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] px-2 py-1 text-[11px] text-status-warn select-text">
              <Icon name="alert-triangle" size={11} className="shrink-0" />
              <span>{row.lastError}</span>
            </div>
          </td>
        </tr>
      ) : null}
    </Fragment>
  );
}

export function BrowserPage() {
  const browserSlice = useBrowser();
  const loading = usePageLoading("browser");
  const st = browserSlice ?? DEFAULT_BROWSER_STATE;

  const [newOpen, setNewOpen] = useState(false);
  const [newPlatform, setNewPlatform] = useState("豆包");
  const [renameTarget, setRenameTarget] = useState<BrowserRowDTO | null>(null);
  const [renameOpenSeq, setRenameOpenSeq] = useState(0);
  const [deleteTarget, setDeleteTarget] = useState<BrowserRowDTO | null>(null);
  const [clearHistoryTargets, setClearHistoryTargets] = useState<BrowserRowDTO[] | null>(null);
  const [clearKeepRecent, setClearKeepRecent] = useState("0");
  const [clearFastMode, setClearFastMode] = useState(true);
  const [clearSubmitting, setClearSubmitting] = useState(false);
  const [filters, setFilters] = useState<AccountFiltersState>(DEFAULT_ACCOUNT_FILTERS);
  const [sortState, setSortState] = useState<AccountSortState>(DEFAULT_ACCOUNT_SORT);
  const [openFilterCol, setOpenFilterCol] = useState<AccountColumnKey | null>(null);

  const filterPlat =
    filters.platforms.length === 1 &&
    PLATFORM_OPTIONS.some((opt) => opt.value === filters.platforms[0])
      ? filters.platforms[0]
      : "全部";

  const setFilterPlat = useCallback((nextPlat: string) => {
    setFilters((prev) => ({
      ...prev,
      platforms: nextPlat === "全部" ? [] : [nextPlat],
    }));
  }, []);

  const [fingerprintTarget, setFingerprintTarget] = useState<BrowserRowDTO | null>(null);
  const [proxyTarget, setProxyTarget] = useState<BrowserRowDTO | null>(null);
  const [proxyOpenSeq, setProxyOpenSeq] = useState(0);
  const [proxyError, setProxyError] = useState("");
  const [proxySaving, setProxySaving] = useState(false);

  const closeNewDialog = useCallback(() => {
    setNewOpen(false);
  }, []);

  const closeRenameDialog = useCallback(() => {
    setRenameTarget(null);
  }, []);

  const closeProxyDialog = useCallback(() => {
    if (proxySaving) return;
    setProxyTarget(null);
    setProxyError("");
  }, [proxySaving]);

  const closeDeleteDialog = useCallback(() => {
    setDeleteTarget(null);
  }, []);

  const closeClearHistoryDialog = useCallback(() => {
    setClearHistoryTargets(null);
  }, []);

  useEscapeHandler("browser-new-dialog", newOpen, closeNewDialog, 40);
  useEscapeHandler("browser-rename-dialog", Boolean(renameTarget), closeRenameDialog, 40);
  useEscapeHandler("browser-proxy-dialog", Boolean(proxyTarget), closeProxyDialog, 40);
  useEscapeHandler("browser-delete-dialog", Boolean(deleteTarget), closeDeleteDialog, 40);
  useEscapeHandler(
    "browser-clear-history-dialog",
    Boolean(clearHistoryTargets),
    closeClearHistoryDialog,
    40,
  );

  const openNewDialog = useCallback(() => {
    setNewPlatform(filterPlat !== "全部" ? filterPlat : "豆包");
    setNewOpen(true);
  }, [filterPlat]);

  const openRenameDialog = useCallback((row: BrowserRowDTO) => {
    setRenameOpenSeq((n) => n + 1);
    setRenameTarget(row);
  }, []);

  const openProxyDialog = useCallback((row: BrowserRowDTO) => {
    setProxyError("");
    setProxyOpenSeq((n) => n + 1);
    setProxyTarget(row);
  }, []);

  const allRows = st.rows ?? [];
  const filteredRows = useMemo(
    () => filterAccountRows(allRows, filters),
    [allRows, filters],
  );
  const rows = useMemo(
    () => sortAccountRows(filteredRows, sortState),
    [filteredRows, sortState],
  );
  const hasFilters = hasActiveAccountFilters(filters);

  const dismissedSharedExitHints = useDismissedSharedExitHints();

  const visibleSharedExitHints = useMemo(() => {
    const hints = st.sharedExitHints ?? [];
    return hints.filter((h) => {
      if (filters.platforms.length > 0 && !filters.platforms.includes(h.platform)) {
        return false;
      }
      return dismissedSharedExitHints[h.platform] !== h.message;
    });
  }, [st.sharedExitHints, filters.platforms, dismissedSharedExitHints]);

  const selectableRows = rows;
  const checkedVisibleCount = useMemo(
    () => selectableRows.filter((r) => r.checked).length,
    [selectableRows],
  );
  const headerCheckState: boolean | "indeterminate" =
    selectableRows.length > 0 && checkedVisibleCount === selectableRows.length
      ? true
      : checkedVisibleCount > 0
        ? "indeterminate"
        : false;

  const handleCycleSort = useCallback((col: AccountColumnKey) => {
    setSortState((prev) => {
      if (prev.key !== col) return { key: col, dir: "asc" };
      if (prev.dir === "asc") return { key: col, dir: "desc" };
      return DEFAULT_ACCOUNT_SORT;
    });
  }, []);

  const handleSetSort = useCallback((col: AccountColumnKey, dir: SortDirection | null) => {
    setSortState(dir ? { key: col, dir } : DEFAULT_ACCOUNT_SORT);
  }, []);

  const handleResetColumn = useCallback((col: AccountColumnKey) => {
    setFilters((prev) => resetColumnFilter(prev, col));
  }, []);

  const handleResetAllFilters = useCallback(() => {
    setFilters(DEFAULT_ACCOUNT_FILTERS);
    setSortState(DEFAULT_ACCOUNT_SORT);
  }, []);

  const handleOpenBrowserSettings = useCallback(() => {
    void goTab(TAB_SETTINGS);
    void updateSettingField("tab", "2");
  }, []);

  const handleOpenSendIntervalSettings = useCallback(() => {
    void goTab(TAB_SETTINGS);
    void updateSettingField("tab", "1");
  }, []);

  const handleDismissSharedExitHints = useCallback(() => {
    dismissSharedExitHints(visibleSharedExitHints);
  }, [visibleSharedExitHints]);

  const handleCreateBrowser = useCallback(() => {
    setNewOpen(false);
    invokeSafe(() => Browser.createBrowser(newPlatform, ""), refreshBrowser);
  }, [newPlatform]);

  const handleToggleAllVisible = useCallback(() => {
    const next = headerCheckState !== true;
    if (!hasFilters) {
      invokeSafe(() => Browser.toggleAllBrowserChecks(next), refreshBrowser);
      return;
    }
    const targets = selectableRows.filter((r) => Boolean(r.checked) !== next);
    if (targets.length === 0) return;
    invokeSafe(
      () => Promise.all(targets.map((r) => Browser.toggleBrowserCheck(r.id, next))),
      refreshBrowser,
    );
  }, [hasFilters, headerCheckState, selectableRows]);

  const handleCommitRename = useCallback(
    (id: number, value: string) => {
      setRenameTarget(null);
      invokeSafe(() => Browser.commitBrowserRename(id, value), refreshBrowser);
    },
    [],
  );

  const handleSaveProxy = useCallback(async (id: number, value: string) => {
    setProxySaving(true);
    setProxyError("");
    try {
      await Browser.saveAccountProxy(id, value);
      setProxyTarget(null);
      showToast(value.trim() ? "账号专属代理已保存" : "已清除专属代理（跟随全局）", "success");
      void refreshBrowser();
    } catch (err) {
      const msg = formatCallError(err);
      setProxyError(msg);
      showToast("保存代理失败", "error", msg);
    } finally {
      setProxySaving(false);
    }
  }, []);

  const handleToggleCheck = useCallback((id: number, checked: boolean) => {
    invokeSafe(() => Browser.toggleBrowserCheck(id, checked), refreshBrowser);
  }, []);

  const handleRowAction = useCallback((id: number, action: string) => {
    invokeSafe(() => Browser.browserRowAction(id, action), refreshBrowser);
  }, []);

  const handleConfirmDelete = useCallback(() => {
    if (!deleteTarget) return;
    const id = deleteTarget.id;
    setDeleteTarget(null);
    setRenameTarget((cur) => (cur?.id === id ? null : cur));
    setProxyTarget((cur) => {
      if (cur?.id === id) {
        setProxyError("");
        return null;
      }
      return cur;
    });
    invokeSafe(() => Browser.browserRowAction(id, "delete"), refreshBrowser);
  }, [deleteTarget]);

  const clearingActive = Boolean(st.batchActive && st.batchKind === "clearing");
  const syncingActive = Boolean(st.batchActive && st.batchKind === "syncing");
  const syncingStatusActive = Boolean(st.batchActive && st.batchKind === "syncing_status");
  const getPlatformConcurrencyLabel = useCallback(
    (row: BrowserRowDTO): string | undefined => {
      if (isDoubaoAccount(row)) {
        return st.doubaoConcurrencyAuto
          ? `调度上限 ${st.doubaoConcurrencyMax}`
          : "串行 1";
      }
      if (isQwenAccount(row)) {
        return st.qianwenConcurrencyAuto
          ? `调度上限 ${st.qianwenConcurrencyMax}`
          : "串行 1";
      }
      if (isGeminiAccount(row)) {
        return st.geminiConcurrencyAuto
          ? `调度上限 ${st.geminiConcurrencyMax}`
          : "串行 1";
      }
      return undefined;
    },
    [
      st.doubaoConcurrencyAuto,
      st.doubaoConcurrencyMax,
      st.qianwenConcurrencyAuto,
      st.qianwenConcurrencyMax,
      st.geminiConcurrencyAuto,
      st.geminiConcurrencyMax,
    ],
  );

  const openClearHistoryDialog = useCallback(
    (selection: BrowserRowDTO | BrowserRowDTO[] | null) => {
      const targets = Array.isArray(selection)
        ? selection
        : selection
          ? [selection]
          : allRows.filter((r) => Boolean(r.checked));
      if (targets.length === 0) return;
      setClearKeepRecent("0");
      setClearFastMode(true);
      setClearHistoryTargets(targets);
    },
    [allRows],
  );

  const handleConfirmClearHistory = useCallback(async () => {
    if (!clearHistoryTargets || clearHistoryTargets.length === 0) return;
    const ids = clearHistoryTargets.map((r) => r.id);
    const parsed = Number.parseInt(clearKeepRecent.trim(), 10);
    const keepRecent = Number.isFinite(parsed) && parsed > 0 ? Math.min(500, parsed) : 0;
    const fast = clearFastMode;
    setClearSubmitting(true);
    try {
      await Browser.clearBrowserHistory(ids, keepRecent, fast);
      setClearHistoryTargets(null);
      const modeLabel = fast ? "极速清理" : "清理";
      showToast(
        ids.length === 1
          ? `已开始${modeLabel}该账号聊天历史`
          : `已开始批量${modeLabel} ${ids.length} 个账号聊天历史`,
        "info",
      );
      void refreshBrowser();
    } catch (err) {
      showToast("无法开始清理聊天历史", "error", formatCallError(err));
    } finally {
      setClearSubmitting(false);
    }
  }, [clearHistoryTargets, clearKeepRecent, clearFastMode]);

  const handleStopClearHistory = useCallback(() => {
    invokeSafe(() => Browser.stopClearBrowserHistory(), refreshBrowser);
  }, []);

  const handleSyncUsernames = useCallback(async (targets: BrowserRowDTO[]) => {
    if (targets.length === 0) return;
    const ids = targets.map((r) => r.id);
    try {
      await Browser.syncAccountUsernames(ids);
      showToast(
        ids.length === 1
          ? `正在同步「${targets[0].name || `账号 #${targets[0].id}`}」的用户名与实时额度`
          : `已开始批量同步 ${ids.length} 个账号的用户名与实时额度`,
        "info",
      );
      void refreshBrowser();
    } catch (err) {
      showToast("无法同步账号", "error", formatCallError(err));
    }
  }, []);

  const handleStopSyncUsernames = useCallback(() => {
    invokeSafe(() => Browser.stopSyncAccountUsernames(), refreshBrowser);
  }, []);

  const handleSyncQwenStatus = useCallback(async (targets: BrowserRowDTO[]) => {
    const qwenTargets = targets.filter(isQwenAccount);
    if (qwenTargets.length === 0) return;
    const ids = qwenTargets.map((r) => r.id);
    try {
      await Browser.syncQwenStatus(ids);
      showToast(
        ids.length === 1
          ? `正在同步「${qwenTargets[0].name || `账号 #${qwenTargets[0].id}`}」的千问积分与刷新状态`
          : `已开始同步 ${ids.length} 个千问账号的积分、刷新时间与并发上限`,
        "info",
      );
      void refreshBrowser();
    } catch (err) {
      showToast("无法同步千问状态", "error", formatCallError(err));
    }
  }, []);

  const handleStopSyncQwenStatus = useCallback(() => {
    invokeSafe(() => Browser.stopSyncQwenStatus(), refreshBrowser);
  }, []);

  const handleSyncDoubaoStatus = useCallback(async (targets: BrowserRowDTO[]) => {
    const doubaoTargets = targets.filter(isDoubaoAccount);
    if (doubaoTargets.length === 0) return;
    const ids = doubaoTargets.map((r) => r.id);
    try {
      await Browser.syncAccountStatus(ids);
      showToast(
        ids.length === 1
          ? `正在同步「${doubaoTargets[0].name || `账号 #${doubaoTargets[0].id}`}」的豆包订阅与限额状态`
          : `已开始同步 ${ids.length} 个豆包账号的订阅方案与实时限额状态`,
        "info",
      );
      void refreshBrowser();
    } catch (err) {
      showToast("无法同步豆包状态", "error", formatCallError(err));
    }
  }, []);

  const handleStopSyncAccountStatus = useCallback(() => {
    invokeSafe(() => Browser.stopSyncAccountStatus(), refreshBrowser);
  }, []);

  const handleSyncGeminiStatus = useCallback(async (targets: BrowserRowDTO[]) => {
    const geminiTargets = targets.filter(isGeminiAccount);
    if (geminiTargets.length === 0) return;
    const ids = geminiTargets.map((r) => r.id);
    try {
      await Browser.syncGeminiStatus(ids);
      showToast(
        ids.length === 1
          ? `正在同步「${geminiTargets[0].name || `账号 #${geminiTargets[0].id}`}」的 Gemini 剩余额度与账号状态`
          : `已开始同步 ${ids.length} 个 Gemini 账号的剩余额度、刷新时间与套餐`,
        "info",
      );
      void refreshBrowser();
    } catch (err) {
      showToast("无法同步 Gemini 状态", "error", formatCallError(err));
    }
  }, []);

  const handleStopSyncGeminiStatus = useCallback(() => {
    invokeSafe(() => Browser.stopSyncGeminiStatus(), refreshBrowser);
  }, []);

  const showTopNotice = !st.ready || Boolean(st.missingHint);

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-paper">
      {showTopNotice ? (
        <PageToolbar
          left={
            <div className="flex flex-wrap items-center gap-2">
              {!st.ready ? (
                <div
                  data-testid="pool-runtime-notice"
                  className="inline-flex h-7 items-center gap-1.5 rounded-[10px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] px-2.5 text-[11px] font-medium text-status-warn"
                >
                  <Icon name="alert-triangle" size={12} className="shrink-0" />
                  <span>{st.busy ? "内置浏览器下载中..." : "尚未安装内置浏览器"}</span>
                  <button
                    type="button"
                    data-testid="browser-go-settings-btn"
                    onClick={handleOpenBrowserSettings}
                    className="cursor-pointer font-semibold underline underline-offset-2 hover:opacity-80"
                  >
                    {st.busy ? "查看进度" : "前往设置安装"}
                  </button>
                </div>
              ) : null}

              {st.missingHint ? (
                <div
                  data-testid="browser-missing-hint"
                  className="inline-flex h-7 items-center gap-1.5 rounded-[10px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] px-2.5 text-[11px] font-medium text-status-warn"
                >
                  <Icon name="info" size={12} className="shrink-0" />
                  <span>{st.missingHint}</span>
                </div>
              ) : null}
            </div>
          }
        />
      ) : null}

      {/* Bento 主画布：统一账号池表格卡 */}
      {loading && !browserSlice ? (
        <div className="flex min-h-0 flex-1 flex-col px-3 pb-3">
          {!showTopNotice ? <div className="app-drag-region h-2.5 shrink-0" /> : null}
          <div className="min-h-0 flex-1 animate-pulse rounded-[16px] border border-hairline bg-card" />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col px-3 pb-3">
          {!showTopNotice ? <div className="app-drag-region h-2.5 shrink-0" /> : null}
          <FlushPane className="min-h-0 flex-1">
            <PaneHeader
              dragRegion={!showTopNotice}
              icon="users"
              title="账号池"
              subtitle={
                st.batchActive
                  ? st.batchText
                  : "点击名称可重命名 · 点击代理可独立配置 · 眼睛图标查看实时画面"
              }
              right={
                <div className="flex flex-wrap items-center gap-2">
                  <Tabs
                    data-testid="browser-platform-filter"
                    variant="segmented"
                    size="small"
                    options={[
                      { value: "全部", label: "全部" },
                      ...PLATFORM_OPTIONS.map((opt) => ({
                        value: opt.value,
                        label: opt.label,
                        iconOnly: true,
                        tooltip: opt.label,
                        iconNode: (
                          <span
                            className={cn(
                              "inline-flex size-[18px] shrink-0 items-center justify-center rounded-[5px] transition-colors",
                              filterPlat === opt.value && "bg-white/95",
                            )}
                          >
                            <img
                              src={PROVIDER_MARKS[opt.value]}
                              alt=""
                              aria-hidden="true"
                              className="size-3.5 object-contain"
                              draggable={false}
                            />
                          </span>
                        ),
                      })),
                    ]}
                    value={filterPlat}
                    onValueChange={setFilterPlat}
                  />
                  {clearingActive ? (
                    <Button
                      data-testid="browser-stop-clear-history-btn"
                      text="停止清理"
                      size="small"
                      icon="square"
                      variant="danger"
                      onClick={handleStopClearHistory}
                    />
                  ) : syncingActive ? (
                    <Button
                      data-testid="browser-stop-sync-names-btn"
                      text="停止同步"
                      size="small"
                      icon="square"
                      variant="danger"
                      onClick={handleStopSyncUsernames}
                    />
                  ) : syncingStatusActive ? (
                    <Button
                      data-testid="browser-stop-sync-qwen-btn"
                      text="停止同步"
                      size="small"
                      icon="square"
                      variant="danger"
                      onClick={() => {
                        if (st.batchText.includes("豆包")) {
                          handleStopSyncAccountStatus();
                        } else if (st.batchText.includes("Gemini")) {
                          handleStopSyncGeminiStatus();
                        } else {
                          handleStopSyncQwenStatus();
                        }
                      }}
                    />
                  ) : st.checkedCount > 0 ? (
                    <>
                      {st.checkedCount > checkedVisibleCount ? (
                        <Button
                          text={`清空已选 (${st.checkedCount})`}
                          size="small"
                          variant="ghost"
                          onClick={() =>
                            invokeSafe(() => Browser.toggleAllBrowserChecks(false), refreshBrowser)
                          }
                        />
                      ) : null}
                      {!st.batchActive ? (
                        <>
                          <Tooltip
                            content={
                              st.running
                                ? "任务运行中，暂不可同步"
                                : "自动同步所选账号的用户名与实时额度"
                            }
                            side="bottom"
                          >
                            <Button
                              data-testid="browser-batch-sync-names-btn"
                              text={`同步 (${st.checkedCount})`}
                              size="small"
                              icon="refresh-cw"
                              variant="outline"
                              disabled={Boolean(st.running)}
                              onClick={() =>
                                void handleSyncUsernames(
                                  allRows.filter((r) => Boolean(r.checked)),
                                )
                              }
                            />
                          </Tooltip>
                          <Tooltip
                            content={
                              st.running
                                ? "任务运行中，暂不可清理"
                                : "批量清理所选账号的网页端历史对话"
                            }
                            side="bottom"
                          >
                            <Button
                              data-testid="browser-batch-clear-history-btn"
                              text={`批量清理对话 (${st.checkedCount})`}
                              size="small"
                              icon="trash-2"
                              variant="outline"
                              disabled={Boolean(st.running)}
                              onClick={() => openClearHistoryDialog(null)}
                            />
                          </Tooltip>
                        </>
                      ) : null}
                      <Button
                        data-testid="browser-batch-btn"
                        text={st.batchActive ? "停止批量" : `批量打开 (${st.checkedCount})`}
                        size="small"
                        icon={st.batchActive ? "square" : "play"}
                        variant={st.batchActive ? "danger" : "outline"}
                        onClick={() =>
                          invokeSafe(
                            () => Browser.runBrowserBatch(Boolean(st.batchActive)),
                            refreshBrowser,
                          )
                        }
                      />
                    </>
                  ) : !st.batchActive && rows.length > 0 ? (
                    <>
                      <Tooltip
                        content={
                          st.running
                            ? "任务运行中，暂不可同步"
                            : `自动同步当前列表全部 ${rows.length} 个账号的用户名与实时额度`
                        }
                        side="bottom"
                      >
                        <Button
                          data-testid="browser-quick-sync-names-btn"
                          text="同步"
                          size="small"
                          icon="refresh-cw"
                          variant="outline"
                          disabled={Boolean(st.running)}
                          onClick={() => void handleSyncUsernames(rows)}
                        />
                      </Tooltip>
                      <Tooltip
                        content={
                          st.running
                            ? "任务运行中，暂不可清理"
                            : `一键极速清理当前列表全部 ${rows.length} 个账号的网页端聊天历史`
                        }
                        side="bottom"
                      >
                        <Button
                          data-testid="browser-quick-clear-history-btn"
                          text="一键清历史"
                          size="small"
                          icon="trash-2"
                          variant="outline"
                          disabled={Boolean(st.running)}
                          onClick={() => openClearHistoryDialog(rows)}
                        />
                      </Tooltip>
                    </>
                  ) : null}
                  <Tooltip
                    content={st.running ? "任务运行中，暂不可新建账号" : "创建独立账号（登录态彼此隔离）"}
                    side="bottom"
                  >
                    <Button
                      data-testid="browser-new-btn"
                      text="新建账号"
                      size="small"
                      icon="plus"
                      variant="primary"
                      disabled={Boolean(st.running)}
                      onClick={openNewDialog}
                    />
                  </Tooltip>
                </div>
              }
            />

            <AccountListFrame className="flex min-h-0 flex-1 flex-col overflow-hidden">
              {allRows.length === 0 ? (
                <div className="flex flex-1 items-center justify-center p-6">
                  <EmptyNotice
                    icon="users"
                    title="账号池暂无账号"
                    subtitle="点击「新建账号」添加豆包、千问、千问国际或 Gemini 账号"
                    action={
                      <Button
                        data-testid="browser-empty-new-btn"
                        text="新建账号"
                        size="small"
                        icon="plus"
                        variant="primary"
                        disabled={Boolean(st.running)}
                        onClick={openNewDialog}
                      />
                    }
                  />
                </div>
              ) : (
                <>

                  {hasFilters ? (
                    <div
                      data-testid="browser-active-filters-bar"
                      className="animate-tulai-banner-in flex flex-wrap items-center justify-between gap-2 border-b border-divider bg-card-header/70 px-4 py-1.5 text-[11px]"
                    >
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="tnum font-medium text-muted">
                          {`已显示 ${rows.length} / ${allRows.length} 个账号`}
                        </span>
                        {(
                          [
                            { key: "enabled", label: "启用状态" },
                            { key: "account", label: "账号" },
                            { key: "platform", label: "平台" },
                            { key: "health", label: "状态" },
                            { key: "usage", label: "今日用量" },
                            { key: "proxy", label: "独立代理" },
                          ] as { key: AccountColumnKey; label: string }[]
                        )
                          .filter((item) => isColumnFilterActive(filters, item.key))
                          .map((item) => (
                            <button
                              key={item.key}
                              type="button"
                              data-testid={`browser-active-filter-chip-${item.key}`}
                              onClick={() => handleResetColumn(item.key)}
                              className="inline-flex cursor-pointer items-center gap-1 rounded-[6px] border border-hairline bg-sunken px-1.5 py-0.5 font-medium text-ink transition-colors hover:bg-sunken-hover"
                            >
                              <span>{`筛选：${item.label}`}</span>
                              <Icon name="x" size={10} className="text-muted" />
                            </button>
                          ))}
                      </div>
                      <Button
                        data-testid="browser-reset-filters-btn"
                        text="重置"
                        size="xs"
                        icon="rotate-ccw"
                        variant="ghost"
                        onClick={handleResetAllFilters}
                      />
                    </div>
                  ) : null}

                  <div
                    data-testid="browser-account-list"
                    className="min-h-0 flex-1 overflow-auto"
                  >
                    <table
                      data-testid="browser-account-table"
                      className="w-full border-collapse text-left"
                    >
                      <thead className="sticky top-0 z-10 border-b border-divider bg-card-header">
                        <tr className="h-9 text-[11px] font-semibold text-muted select-none">
                          <th scope="col" className="w-[42px] py-2 pl-4 pr-2 align-middle">
                            <Checkbox
                              data-testid="browser-select-all"
                              size="small"
                              checked={headerCheckState}
                              disabled={selectableRows.length === 0}
                              aria-label={headerCheckState === true ? "取消全选账号" : "全选账号"}
                              onCheckedChange={handleToggleAllVisible}
                            />
                          </th>
                          <AccountColumnHeader
                            colKey="enabled"
                            label="启用"
                            className="w-[72px] px-2.5 py-2 align-middle whitespace-nowrap"
                            sortState={sortState}
                            filters={filters}
                            allRows={allRows}
                            basePlatforms={PLATFORM_OPTIONS.map((o) => o.value)}
                            providerMarks={PROVIDER_MARKS}
                            openCol={openFilterCol}
                            onOpenColChange={setOpenFilterCol}
                            onCycleSort={handleCycleSort}
                            onSetSort={handleSetSort}
                            onUpdateFilters={setFilters}
                            onResetColumn={handleResetColumn}
                          />
                          <AccountColumnHeader
                            colKey="account"
                            label="账号"
                            className="w-[21%] min-w-[156px] px-2.5 py-2 align-middle whitespace-nowrap"
                            sortState={sortState}
                            filters={filters}
                            allRows={allRows}
                            basePlatforms={PLATFORM_OPTIONS.map((o) => o.value)}
                            providerMarks={PROVIDER_MARKS}
                            openCol={openFilterCol}
                            onOpenColChange={setOpenFilterCol}
                            onCycleSort={handleCycleSort}
                            onSetSort={handleSetSort}
                            onUpdateFilters={setFilters}
                            onResetColumn={handleResetColumn}
                          />
                          <AccountColumnHeader
                            colKey="platform"
                            label="平台"
                            className="w-[12%] min-w-[96px] px-2.5 py-2 align-middle whitespace-nowrap"
                            sortState={sortState}
                            filters={filters}
                            allRows={allRows}
                            basePlatforms={PLATFORM_OPTIONS.map((o) => o.value)}
                            providerMarks={PROVIDER_MARKS}
                            openCol={openFilterCol}
                            onOpenColChange={setOpenFilterCol}
                            onCycleSort={handleCycleSort}
                            onSetSort={handleSetSort}
                            onUpdateFilters={setFilters}
                            onResetColumn={handleResetColumn}
                          />
                          <AccountColumnHeader
                            colKey="health"
                            label="状态"
                            className="w-[12%] min-w-[104px] px-2.5 py-2 align-middle whitespace-nowrap"
                            sortState={sortState}
                            filters={filters}
                            allRows={allRows}
                            basePlatforms={PLATFORM_OPTIONS.map((o) => o.value)}
                            providerMarks={PROVIDER_MARKS}
                            openCol={openFilterCol}
                            onOpenColChange={setOpenFilterCol}
                            onCycleSort={handleCycleSort}
                            onSetSort={handleSetSort}
                            onUpdateFilters={setFilters}
                            onResetColumn={handleResetColumn}
                          />
                          <AccountColumnHeader
                            colKey="usage"
                            label="今日用量"
                            className="w-[24%] min-w-[208px] px-2.5 py-2 align-middle whitespace-nowrap"
                            sortState={sortState}
                            filters={filters}
                            allRows={allRows}
                            basePlatforms={PLATFORM_OPTIONS.map((o) => o.value)}
                            providerMarks={PROVIDER_MARKS}
                            openCol={openFilterCol}
                            onOpenColChange={setOpenFilterCol}
                            onCycleSort={handleCycleSort}
                            onSetSort={handleSetSort}
                            onUpdateFilters={setFilters}
                            onResetColumn={handleResetColumn}
                          />
                          <AccountColumnHeader
                            colKey="proxy"
                            label="独立代理"
                            className="w-[16%] min-w-[120px] max-w-[200px] px-2.5 py-2 align-middle whitespace-nowrap"
                            sortState={sortState}
                            filters={filters}
                            allRows={allRows}
                            basePlatforms={PLATFORM_OPTIONS.map((o) => o.value)}
                            providerMarks={PROVIDER_MARKS}
                            openCol={openFilterCol}
                            onOpenColChange={setOpenFilterCol}
                            onCycleSort={handleCycleSort}
                            onSetSort={handleSetSort}
                            onUpdateFilters={setFilters}
                            onResetColumn={handleResetColumn}
                          />
                          <th
                            scope="col"
                            className="w-[1%] py-2 pl-2.5 pr-4 text-right align-middle whitespace-nowrap"
                          >
                            操作
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {rows.length === 0 ? (
                          <tr>
                            <td colSpan={8} className="px-4 py-12">
                              <EmptyNotice
                                icon="search"
                                title={
                                  filterPlat !== "全部" && !isColumnFilterActive(filters, "account")
                                    ? `暂无${filterPlat}账号`
                                    : "没有符合筛选条件的账号"
                                }
                                subtitle="可调整列筛选条件，或一键重置恢复全部账号视图"
                                action={
                                  <div className="flex items-center gap-2">
                                    {hasFilters ? (
                                      <Button
                                        data-testid="browser-empty-reset-filters-btn"
                                        text="重置筛选"
                                        size="small"
                                        icon="rotate-ccw"
                                        variant="outline"
                                        onClick={handleResetAllFilters}
                                      />
                                    ) : null}
                                    <Button
                                      data-testid="browser-empty-new-btn"
                                      text="新建账号"
                                      size="small"
                                      icon="plus"
                                      variant="primary"
                                      disabled={Boolean(st.running)}
                                      onClick={openNewDialog}
                                    />
                                  </div>
                                }
                              />
                            </td>
                          </tr>
                        ) : (
                          rows.map((row) => (
                            <AccountTableRows
                              key={row.id}
                              row={row}
                              platformConcurrencyLabel={getPlatformConcurrencyLabel(row)}
                              pageRunning={Boolean(st.running)}
                              clearingBusy={clearingActive}
                              syncingStatusBusy={syncingStatusActive}
                              onRequestRename={openRenameDialog}
                              onRequestProxy={openProxyDialog}
                              onRequestFingerprint={setFingerprintTarget}
                              onToggleCheck={handleToggleCheck}
                              onRowAction={handleRowAction}
                              onSyncQwenRow={(target) => void handleSyncQwenStatus([target])}
                              onSyncDoubaoRow={(target) => void handleSyncDoubaoStatus([target])}
                              onSyncGeminiRow={(target) => void handleSyncGeminiStatus([target])}
                              onRequestClearHistory={openClearHistoryDialog}
                              onRequestDelete={setDeleteTarget}
                            />
                          ))
                        )}
                      </tbody>
                    </table>
                  </div>
                </>
              )}

              {visibleSharedExitHints.length > 0 ? (
                <div
                  data-testid="browser-shared-exit-hint"
                  className="animate-tulai-banner-in flex flex-wrap items-center justify-between gap-2 border-t border-[var(--status-warn-border)]/70 bg-[var(--status-warn-bg)]/65 px-3.5 py-1.5"
                >
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    {visibleSharedExitHints.map((hint) => (
                      <div
                        key={hint.platform}
                        data-testid={`browser-shared-exit-hint-${hint.platform}`}
                        className="flex min-w-0 items-center gap-1.5"
                      >
                        <Icon name="alert-triangle" size={12} className="shrink-0 text-status-warn" />
                        <Tooltip content={hint.message} side="top">
                          <span
                            tabIndex={0}
                            className="truncate rounded text-[11px] text-status-warn focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                          >
                            {hint.message}
                          </span>
                        </Tooltip>
                      </div>
                    ))}
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <Button
                      data-testid="browser-shared-exit-interval-btn"
                      text="调整间隔"
                      size="xs"
                      variant="outline"
                      onClick={handleOpenSendIntervalSettings}
                    />
                    <Tooltip content="关闭提示" side="top">
                      <IconButton
                        data-testid="browser-shared-exit-dismiss-btn"
                        icon="x"
                        size="xs"
                        variant="ghost"
                        aria-label="关闭提示"
                        className="text-status-warn/80 hover:bg-[var(--status-warn-border)]/35 hover:text-status-warn"
                        onClick={handleDismissSharedExitHints}
                      />
                    </Tooltip>
                  </div>
                </div>
              ) : null}
            </AccountListFrame>
          </FlushPane>
        </div>
      )}

      {/* 新建账号弹窗 */}
      <Dialog
        open={newOpen}
        onOpenChange={(open) => {
          if (!open) closeNewDialog();
          else setNewOpen(true);
        }}
        title="新建账号"
        description="选择平台创建独立浏览器环境，登录成功后自动抓取账号用户名"
        width={460}
        closeOnBackdrop
        closeOnEscape
        data-testid="browser-new-dialog"
        footer={
          <>
            <Button text="取消" size="small" variant="ghost" onClick={closeNewDialog} />
            <Button
              data-testid="browser-create-btn"
              text="创建账号"
              size="small"
              icon="plus"
              variant="primary"
              autoFocus
              disabled={Boolean(st.running)}
              onClick={handleCreateBrowser}
            />
          </>
        }
      >
        <div className="flex w-full min-w-0 flex-col gap-3.5">
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-medium text-ink">所属平台</span>
            <div
              role="radiogroup"
              aria-label="选择新建账号平台"
              className="grid grid-cols-2 gap-2"
            >
              {PLATFORM_OPTIONS.map((opt) => {
                const active = newPlatform === opt.value;
                const mark = PROVIDER_MARKS[opt.value];
                return (
                  <button
                    key={opt.value}
                    type="button"
                    role="radio"
                    aria-checked={active}
                    data-testid={`browser-new-platform-${opt.value}`}
                    onClick={() => setNewPlatform(opt.value)}
                    className={cn(
                      "flex cursor-pointer items-center gap-2 rounded-[10px] border px-3 py-2 text-xs transition-colors duration-150 select-none",
                      active
                        ? "border-accent-border bg-accent-soft font-semibold text-accent"
                        : "border-hairline bg-sunken font-medium text-ink hover:bg-sunken-hover",
                    )}
                  >
                    {mark ? (
                      <img
                        src={mark}
                        alt=""
                        aria-hidden="true"
                        className="size-4 shrink-0 object-contain"
                        draggable={false}
                      />
                    ) : null}
                    <span>{opt.label}</span>
                  </button>
                );
              })}
            </div>
            <span className="text-[11px] text-muted">
              创建后将自动打开内置浏览器窗口以便扫码或登录；登录成功后会自动读取账号用户名作为名称，也可随时点击账号名重命名。
            </span>
          </div>

          {!st.ready ? (
            <div className="flex items-center gap-1.5 rounded-[10px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] px-2.5 py-2 text-[11px] text-status-warn">
              <Icon name="alert-triangle" size={12} className="shrink-0" />
              <span>
                {st.busy
                  ? "内置浏览器正在安装中，安装完成后即可打开登录。"
                  : "尚未安装内置浏览器，请先前往设置完成安装。"}
              </span>
            </div>
          ) : null}
        </div>
      </Dialog>

      {/* 重命名账号弹窗 */}
      <RenameAccountDialog
        target={renameTarget}
        openSeq={renameOpenSeq}
        syncDisabled={Boolean(st.running || st.batchActive || renameTarget?.pending)}
        onClose={closeRenameDialog}
        onCommit={handleCommitRename}
        onSyncUsername={(row) => {
          closeRenameDialog();
          void handleSyncUsernames([row]);
        }}
      />

      {fingerprintTarget && <FingerprintDialog key={fingerprintTarget.id} id={fingerprintTarget.id}
        name={fingerprintTarget.name || fingerprintTarget.platform} onClose={() => setFingerprintTarget(null)} />}

      {/* 配置独立代理弹窗 */}
      <ProxyAccountDialog
        target={proxyTarget}
        openSeq={proxyOpenSeq}
        errorText={proxyError}
        saving={proxySaving}
        onClose={closeProxyDialog}
        onSave={(id, val) => void handleSaveProxy(id, val)}
      />

      {/* 删除账号警告确认弹窗 */}
      <Dialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open) closeDeleteDialog();
        }}
        title="删除账号"
        description="此操作不可撤销，请确认是否继续"
        width={440}
        closeOnBackdrop
        closeOnEscape
        data-testid="browser-delete-dialog"
        footer={
          <>
            <Button text="取消" size="small" variant="ghost" onClick={closeDeleteDialog} />
            <Button
              data-testid="browser-delete-confirm-btn"
              text="确认删除"
              size="small"
              icon="trash-2"
              variant="danger"
              onClick={handleConfirmDelete}
            />
          </>
        }
      >
        {deleteTarget ? (
          <div className="flex w-full min-w-0 flex-col gap-2.5">
            <div className="flex items-start gap-2.5 rounded-[12px] border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] p-3 text-xs text-status-danger">
              <Icon name="alert-triangle" size={16} className="mt-0.5 shrink-0" />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="font-semibold">
                  {`即将永久删除 ${deleteTarget.platform} 账号「${deleteTarget.name || `账号 #${deleteTarget.id}`}」`}
                </span>
                <span className="text-[11px] leading-relaxed">
                  删除后将移除该账号，并清理本地浏览器数据与登录状态，无法恢复。
                </span>
              </div>
            </div>

            {deleteTarget.isRunning || deleteTarget.active ? (
              <div className="flex items-center gap-2 rounded-[10px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] px-3 py-2 text-[11px] text-status-warn">
                <Icon name="info" size={13} className="shrink-0" />
                <span>
                  {`该账号当前${deleteTarget.active ? "正在执行任务" : "浏览器窗口运行中"}，确认删除将立即停止窗口。`}
                </span>
              </div>
            ) : null}
          </div>
        ) : null}
      </Dialog>

      {/* 清理平台聊天历史确认弹窗 */}
      <Dialog
        open={Boolean(clearHistoryTargets)}
        onOpenChange={(open) => {
          if (!open && !clearSubmitting) closeClearHistoryDialog();
        }}
        title="清理平台聊天历史"
        description="通过内置浏览器清理豆包、千问、千问国际或 Gemini 网页端的历史对话"
        width={460}
        closeOnBackdrop={!clearSubmitting}
        closeOnEscape={!clearSubmitting}
        data-testid="browser-clear-history-dialog"
        footer={
          <>
            <Button
              text="取消"
              size="small"
              variant="ghost"
              disabled={clearSubmitting}
              onClick={closeClearHistoryDialog}
            />
            <Button
              data-testid="browser-clear-history-confirm-btn"
              text={clearSubmitting ? "启动中..." : "开始清理"}
              size="small"
              icon="trash-2"
              variant="danger"
              disabled={clearSubmitting}
              busy={clearSubmitting}
              onClick={() => void handleConfirmClearHistory()}
            />
          </>
        }
      >
        {clearHistoryTargets ? (
          <div className="flex w-full min-w-0 flex-col gap-3">
            <div className="flex items-start gap-2.5 rounded-[12px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] p-3 text-xs text-status-warn">
              <Icon name="alert-triangle" size={16} className="mt-0.5 shrink-0" />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="font-semibold">
                  {clearHistoryTargets.length === 1
                    ? `即将清理 ${clearHistoryTargets[0].platform} 账号「${clearHistoryTargets[0].name || `账号 #${clearHistoryTargets[0].id}`}」的聊天历史`
                    : `即将批量清理 ${clearHistoryTargets.length} 个账号的平台聊天历史`}
                </span>
                <span className="text-[11px] leading-relaxed">
                  平台端历史对话删除后不可恢复；不会删除账号本身、登录状态或本地已保存图片。
                </span>
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <span className="text-xs font-medium text-ink">保留最近对话条数</span>
              <div className="flex flex-wrap items-center gap-1.5">
                {KEEP_RECENT_PRESETS.map((preset) => {
                  const active = clearKeepRecent.trim() === String(preset.value);
                  return (
                    <button
                      key={preset.value}
                      type="button"
                      data-testid={`clear-history-keep-${preset.value}`}
                      onClick={() => setClearKeepRecent(String(preset.value))}
                      className={cn(
                        "tnum cursor-pointer rounded-[8px] border px-2.5 py-1 text-xs transition-colors duration-150 select-none",
                        active
                          ? "border-accent-border bg-accent-soft font-semibold text-accent"
                          : "border-hairline bg-sunken font-medium text-ink hover:bg-sunken-hover",
                      )}
                    >
                      {preset.label}
                    </button>
                  );
                })}
                <div className="inline-flex items-center gap-1.5 pl-1">
                  <span className="text-[11px] text-muted">自定义</span>
                  <Input
                    data-testid="clear-history-keep-input"
                    value={clearKeepRecent}
                    onChange={(e) => setClearKeepRecent(e.target.value.replace(/[^\d]/g, ""))}
                    aria-label="保留最近对话条数"
                    className="tnum w-[68px]"
                  />
                </div>
              </div>
            </div>

            <div className="flex items-center justify-between gap-3 rounded-[10px] border border-hairline bg-sunken px-3 py-2.5">
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="text-xs font-medium text-ink">极速模式（推荐）</span>
                <span className="text-[11px] leading-relaxed text-muted">
                  首条删除自动捕获平台接口后并发快删，不同平台之间并行清理；遇风控或不支持时自动回退逐条处理。
                </span>
              </div>
              <Switch
                data-testid="clear-history-fast-switch"
                size="small"
                aria-label="极速模式"
                checked={clearFastMode}
                onCheckedChange={setClearFastMode}
              />
            </div>

            <div className="flex items-start gap-2 rounded-[10px] border border-hairline bg-sunken px-3 py-2 text-[11px] leading-relaxed text-muted">
              <Icon name="info" size={13} className="mt-0.5 shrink-0" />
              <span>
                豆包会自动跳过「项目」分组下的对话；如遇未登录或安全验证将自动暂停，可随时在顶部停止。
              </span>
            </div>
          </div>
        ) : null}
      </Dialog>
    </div>
  );
}
