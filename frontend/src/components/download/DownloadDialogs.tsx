import { useMemo, useState } from "react";
import { EmptyNotice, InsetPillRow } from "@/components/primitives";
import {
  Badge,
  Button,
  Checkbox,
  Dialog,
  Icon,
  IconButton,
  Input,
  Select,
  Switch,
  Textarea,
  Tooltip,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { Download } from "@/lib/ipc";
import { useSyncedDraft, type UseSyncedDraftResult } from "@/lib/useSyncedDraft";
import { TAB_SETTINGS, goTab, updateSettingField, type DownloadStateDTO } from "@/store";
import {
  copyDownloadText,
  countActiveFilterRules,
  countUrlLines,
  getInvalidFilterFields,
  runIpc,
  validateDownloadFilters,
  type DownloadFilterValues,
} from "./downloadFormat";
import { getSiteDomain, getSiteIconUrl } from "./siteIcons";

function requiresFingerprintGuide(pages: string): boolean {
  return /指纹浏览器|下载浏览器|浏览器模式/.test(pages);
}

function SiteGuideIcon({ domain, site }: { domain: string; site: string }) {
  const iconUrl = getSiteIconUrl(domain);
  if (!iconUrl) {
    return (
      <span
        data-testid="dl-guide-site-fallback"
        className="flex size-7 shrink-0 items-center justify-center rounded-[8px] border border-hairline bg-card text-muted"
        aria-hidden="true"
      >
        <Icon name="globe" size={14} />
      </span>
    );
  }
  return (
    <span className="flex size-7 shrink-0 items-center justify-center overflow-hidden rounded-[8px] border border-hairline bg-white p-0.5 shadow-xs">
      <img
        src={iconUrl}
        alt={site}
        loading="lazy"
        decoding="async"
        className="size-full rounded-[5px] object-contain"
      />
    </span>
  );
}

function parseFilterInt(raw: string): number {
  const trimmed = raw.trim();
  if (trimmed === "") return 0;
  const parsed = Number.parseInt(trimmed, 10);
  return Number.isFinite(parsed) ? parsed : 0;
}

function FilterNumberInput({
  label,
  draft,
  invalid = false,
  className,
}: {
  label: string;
  draft: UseSyncedDraftResult;
  invalid?: boolean;
  className?: string;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1", className)}>
      <span className={cn("text-[11px]", invalid ? "font-medium text-status-danger" : "text-muted")}>
        {label}
      </span>
      <Input
        size="small"
        inputMode="numeric"
        aria-label={label}
        aria-invalid={invalid || undefined}
        value={draft.value}
        onChange={(e) => draft.onChange(e.target.value)}
        onCompositionStart={draft.onCompositionStart}
        onCompositionEnd={(e) => draft.onCompositionEnd(e.currentTarget.value)}
        onBlur={draft.onBlur}
        wrapperClassName={
          invalid
            ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)]/40"
            : undefined
        }
        className="tnum"
      />
    </div>
  );
}

export function DownloadComposerDialog({
  open,
  st,
  saveDirLabel,
  filtersOpen,
  hasActiveFilters,
  closeOnEscape = true,
  onToggleFilters,
  onClose,
}: {
  open: boolean;
  st: DownloadStateDTO;
  saveDirLabel: string;
  filtersOpen: boolean;
  hasActiveFilters: boolean;
  closeOnEscape?: boolean;
  onToggleFilters: () => void;
  onClose: () => void;
}) {
  const urlsDraft = useSyncedDraft(
    st.urls || "",
    "dl-urls",
    (val) => runIpc(() => Download.setDownloadField("urls", val)),
    { debounceMs: 90 },
  );

  const depthDraft = useSyncedDraft(
    String(st.depth ?? 1),
    "dl-filter-depth",
    (val) => runIpc(() => Download.setDownloadField("depth", val)),
    { debounceMs: 120 },
  );
  const minKbDraft = useSyncedDraft(
    String(st.minKb ?? 0),
    "dl-filter-minKB",
    (val) => runIpc(() => Download.setDownloadField("minKB", val)),
    { debounceMs: 120 },
  );
  const maxKbDraft = useSyncedDraft(
    String(st.maxKb ?? 0),
    "dl-filter-maxKB",
    (val) => runIpc(() => Download.setDownloadField("maxKB", val)),
    { debounceMs: 120 },
  );
  const minWDraft = useSyncedDraft(
    String(st.minW ?? 0),
    "dl-filter-minW",
    (val) => runIpc(() => Download.setDownloadField("minW", val)),
    { debounceMs: 120 },
  );
  const maxWDraft = useSyncedDraft(
    String(st.maxW ?? 0),
    "dl-filter-maxW",
    (val) => runIpc(() => Download.setDownloadField("maxW", val)),
    { debounceMs: 120 },
  );
  const minHDraft = useSyncedDraft(
    String(st.minH ?? 0),
    "dl-filter-minH",
    (val) => runIpc(() => Download.setDownloadField("minH", val)),
    { debounceMs: 120 },
  );
  const maxHDraft = useSyncedDraft(
    String(st.maxH ?? 0),
    "dl-filter-maxH",
    (val) => runIpc(() => Download.setDownloadField("maxH", val)),
    { debounceMs: 120 },
  );

  const effectiveFilters: DownloadFilterValues = {
    depth: parseFilterInt(depthDraft.value),
    minKb: parseFilterInt(minKbDraft.value),
    maxKb: parseFilterInt(maxKbDraft.value),
    minW: parseFilterInt(minWDraft.value),
    maxW: parseFilterInt(maxWDraft.value),
    minH: parseFilterInt(minHDraft.value),
    maxH: parseFilterInt(maxHDraft.value),
  };

  const filterErrors = validateDownloadFilters(effectiveFilters);
  const invalidFilterFields = getInvalidFilterFields(effectiveFilters);
  const activeRuleCount = countActiveFilterRules(effectiveFilters);
  const effectiveHasActiveFilters =
    hasActiveFilters || effectiveFilters.depth > 1 || activeRuleCount > 0;

  const lineStats = countUrlLines(urlsDraft.value);
  const noticeText = st.composerErr || st.browserErr || "";
  const browserSupported = st.browserSupported !== false;
  const browserReady = st.browserReady !== false;
  const browserMultiUrlWarn = Boolean(st.browserMode && lineStats.uniqueValid > 1);

  const openBrowserSettings = () => {
    goTab(TAB_SETTINGS);
    updateSettingField("tab", "2");
    onClose();
  };

  const dismissAllErrors = () => {
    if (st.composerErr) {
      runIpc(() => Download.setDownloadField("dismissComposerErr", "1"));
    }
    if (st.browserErr) {
      runIpc(() => Download.setDownloadField("dismissBrowserErr", "1"));
    }
  };

  const flushFilterDrafts = () => {
    depthDraft.flush();
    minKbDraft.flush();
    maxKbDraft.flush();
    minWDraft.flush();
    maxWDraft.flush();
    minHDraft.flush();
    maxHDraft.flush();
  };

  const handleSubmit = () => {
    if (filterErrors.length > 0) {
      if (!filtersOpen) onToggleFilters();
      return;
    }
    flushFilterDrafts();
    urlsDraft.flush();
    if (st.browserErr) {
      runIpc(() => Download.setDownloadField("dismissBrowserErr", "1"));
    }
    runIpc(() => Download.submitDownloadURLs(urlsDraft.value));
  };

  const handleOpenBrowser = () => {
    flushFilterDrafts();
    urlsDraft.flush();
    if (st.composerErr) {
      runIpc(() => Download.setDownloadField("dismissComposerErr", "1"));
    }
    runIpc(() => Download.openDownloadBrowser(urlsDraft.value));
  };

  const handleResetFilters = () => {
    depthDraft.setValue("1", true);
    minKbDraft.setValue("0", true);
    maxKbDraft.setValue("0", true);
    minWDraft.setValue("0", true);
    maxWDraft.setValue("0", true);
    minHDraft.setValue("0", true);
    maxHDraft.setValue("0", true);
  };

  const showQuickBrowserAction =
    browserSupported &&
    browserReady &&
    !st.browserBusy &&
    Boolean(st.browserErr || /指纹浏览器|下载浏览器/.test(noticeText));

  return (
    <Dialog
      data-testid="dl-composer-dialog"
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      title="新建下载任务"
      description="每行一个 HTTP/HTTPS 图片直链或网页地址，支持批量粘贴（⌘/Ctrl + Enter 立即下载）"
      width={620}
      closeOnEscape={closeOnEscape}
      footer={
        <div className="flex w-full flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-1.5">
            <Button
              text="导入表格"
              size="small"
              icon="file-spreadsheet"
              variant="ghost"
              onClick={() => runIpc(() => Download.pickDownloadSheet())}
            />
            <Button
              text="站点说明"
              size="small"
              icon="book-open"
              variant="ghost"
              onClick={() => runIpc(() => Download.openDownloadGuide(true))}
            />
          </div>
          <div className="flex items-center gap-2">
            <Button
              text={
                st.browserBusy
                  ? "正在启动指纹浏览器..."
                  : st.browserOpen
                    ? "在指纹浏览器打开"
                    : "指纹浏览器抓取"
              }
              size="small"
              icon="globe"
              variant={st.browserOpen ? "secondary" : "outline"}
              busy={Boolean(st.browserBusy)}
              disabled={!browserSupported || Boolean(st.browserBusy)}
              onClick={handleOpenBrowser}
            />
            <Button text="取消" size="small" variant="ghost" onClick={onClose} />
            <Button
              text="立即下载"
              size="small"
              icon="download"
              variant="primary"
              disabled={Boolean(st.browserMode) && Boolean(st.browserBusy)}
              onClick={handleSubmit}
            />
          </div>
        </div>
      }
    >
      <div className="flex flex-col gap-3">
        {/* URLs Input */}
        <div className="flex flex-col gap-1.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-xs font-medium text-ink">下载链接</span>
            <div className="flex flex-wrap items-center gap-1.5">
              {lineStats.total > 0 ? (
                <Badge
                  text={
                    lineStats.uniqueValid === lineStats.total
                      ? `有效 ${lineStats.uniqueValid} 条`
                      : `有效 ${lineStats.uniqueValid} / ${lineStats.total} 行`
                  }
                  variant={lineStats.uniqueValid > 0 ? "accent" : "warn"}
                  className="tnum"
                />
              ) : null}
              {lineStats.duplicates > 0 ? (
                <Badge
                  data-testid="dl-urls-dup-badge"
                  text={`重复 ${lineStats.duplicates}（提交时去重）`}
                  variant="neutral"
                  className="tnum"
                />
              ) : null}
              {lineStats.invalid > 0 ? (
                <Badge
                  data-testid="dl-urls-invalid-badge"
                  text={`无效 ${lineStats.invalid}`}
                  variant="warn"
                  className="tnum"
                />
              ) : null}
              {lineStats.duplicates > 0 || lineStats.invalid > 0 ? (
                <button
                  type="button"
                  data-testid="dl-urls-clean-btn"
                  onClick={() => urlsDraft.setValue(lineStats.cleanLines.join("\n"), true)}
                  className="cursor-pointer text-[11px] font-medium text-accent hover:underline"
                >
                  去重清理
                </button>
              ) : null}
              {urlsDraft.value ? (
                <button
                  type="button"
                  onClick={() => urlsDraft.setValue("", true)}
                  className="cursor-pointer text-[11px] text-muted hover:text-ink"
                >
                  清空
                </button>
              ) : null}
            </div>
          </div>

          <Textarea
            data-testid="dl-urls-input"
            value={urlsDraft.value}
            placeholder="https://example.com/gallery&#10;https://cdn.example.com/image.jpg"
            onChange={(e) => urlsDraft.onChange(e.target.value)}
            onCompositionStart={urlsDraft.onCompositionStart}
            onCompositionEnd={(e) => urlsDraft.onCompositionEnd(e.currentTarget.value)}
            onBlur={urlsDraft.onBlur}
            onKeyDown={(e) => {
              if (!e.nativeEvent.isComposing && (e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault();
                handleSubmit();
              }
            }}
            className="h-[112px] rounded-[12px] border-hairline bg-sunken p-2.5 text-xs leading-relaxed"
          />

          {browserMultiUrlWarn ? (
            <div
              data-testid="dl-browser-multi-url-warn"
              className="tnum flex items-center gap-1.5 rounded-[10px] border border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] px-2.5 py-1.5 text-[11px] text-status-warn"
            >
              <Icon name="alert-triangle" size={12} />
              <span>
                指纹浏览器模式每次仅支持 1 个网页地址，当前输入了 {lineStats.uniqueValid} 个。
              </span>
            </div>
          ) : null}
        </div>

        {/* Save Directory Row */}
        <InsetPillRow className="flex-row items-center justify-between gap-2 py-2">
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <Icon name="folder" size={14} className="shrink-0 text-subtle" />
            <span className="shrink-0 text-xs text-muted">保存至</span>
            <Tooltip content={saveDirLabel} side="top">
              <span
                tabIndex={0}
                className="font-mono-console min-w-0 flex-1 truncate rounded text-xs text-ink select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
              >
                {saveDirLabel}
              </span>
            </Tooltip>
          </div>
          <Button
            text="更改目录"
            size="xs"
            variant="outline"
            onClick={() => runIpc(() => Download.downloadPickDir())}
          />
        </InsetPillRow>

        {/* Browser Mode & Depth/Filter Toggle */}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <label className="inline-flex h-8 cursor-pointer items-center gap-2 rounded-[10px] border border-hairline bg-sunken px-3 text-xs text-ink transition-colors hover:bg-sunken-hover select-none">
            <span>使用指纹浏览器登录抓取</span>
            <Switch
              size="small"
              checked={Boolean(st.browserMode)}
              aria-label="使用指纹浏览器登录抓取"
              onCheckedChange={(checked) =>
                runIpc(() =>
                  Download.setDownloadField("browserMode", checked ? "true" : "false"),
                )
              }
            />
          </label>

          <button
            type="button"
            data-testid="dl-filters-toggle"
            aria-expanded={filtersOpen}
            onClick={onToggleFilters}
            className={cn(
              "inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-[10px] border px-3 text-xs font-medium transition-colors select-none",
              filterErrors.length > 0
                ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
                : filtersOpen || effectiveHasActiveFilters
                  ? "border-accent-border bg-accent-soft text-accent"
                  : "border-hairline bg-sunken text-ink hover:bg-sunken-hover",
            )}
          >
            <Icon name="sliders-horizontal" size={13} />
            <span>抓取深度与过滤</span>
            <Badge
              text={
                filterErrors.length > 0
                  ? "规则异常"
                  : activeRuleCount > 0
                    ? `深度 ${effectiveFilters.depth} · ${activeRuleCount}项`
                    : `深度 ${effectiveFilters.depth}`
              }
              variant={
                filterErrors.length > 0
                  ? "danger"
                  : effectiveHasActiveFilters
                    ? "accent"
                    : "neutral"
              }
              className="tnum px-1.5 py-0 text-[10px]"
            />
            <Icon name={filtersOpen ? "chevron-up" : "chevron-down"} size={13} />
          </button>
        </div>

        {!browserSupported ? (
          <InsetPillRow className="flex-row items-center justify-between gap-2 py-2 text-xs text-muted">
            <span className="min-w-0 flex-1">
              当前系统暂不支持指纹浏览器 (Fingerprint)，可使用直连与静态网页抓取。
            </span>
          </InsetPillRow>
        ) : !browserReady ? (
          <InsetPillRow className="flex-row items-center justify-between gap-2 border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] py-2">
            <span className="min-w-0 flex-1 text-xs text-status-warn">
              {st.browserInstallBusy
                ? "正在安装指纹浏览器 (Fingerprint)，完成后即可用于登录或动态网页抓取。"
                : "指纹浏览器 (Fingerprint) 尚未安装，需登录或动态网页抓取时请先安装。"}
            </span>
            {st.browserInstallBusy ? (
              <Badge text="安装中" variant="accent" dot />
            ) : (
              <Button
                text="前往设置安装"
                size="xs"
                variant="outline"
                onClick={openBrowserSettings}
              />
            )}
          </InsetPillRow>
        ) : st.browserBusy ? (
          <InsetPillRow className="flex-row items-center gap-2 border-accent-border bg-accent-soft py-2 text-xs text-accent">
            <Icon name="loader-2" size={13} className="animate-spin" />
            <span className="min-w-0 flex-1">
              正在启动指纹浏览器，请稍候...
            </span>
          </InsetPillRow>
        ) : st.browserOpen ? (
          <InsetPillRow className="flex-row items-center justify-between gap-2 border-[var(--status-ok-border)] bg-[var(--status-ok-bg)] py-2">
            <div className="flex min-w-0 flex-1 items-center gap-2 text-xs text-status-ok">
              <span className="size-1.5 shrink-0 rounded-full bg-status-ok" />
              <span className="shrink-0 font-medium">指纹浏览器运行中</span>
              {st.browserUrl ? (
                <span className="font-mono-console min-w-0 truncate text-[11px] text-muted">
                  {st.browserUrl}
                </span>
              ) : (
                <span className="min-w-0 truncate text-[11px] text-muted">
                  可在指纹浏览器中登录账号，或点击页内浮窗「下载当前页」/「下载已加载」
                </span>
              )}
            </div>
          </InsetPillRow>
        ) : st.browserMode ? (
          <div className="rounded-[10px] border border-hairline bg-sunken px-3 py-1.5 text-[11px] text-muted">
            请先点击「指纹浏览器抓取」打开网页并确认页面已加载，再点击「立即下载」抓取当前标签页。
          </div>
        ) : null}

        {/* Expandable 7-field filter grid */}
        {filtersOpen ? (
          <div
            data-testid="dl-filters-grid"
            className="flex flex-col gap-2 rounded-[12px] border border-hairline bg-sunken p-2.5"
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-[11px] text-subtle">
                抓取深度最小为 1；体积与宽高填 0 表示不限制（单位 px）
              </span>
              {effectiveHasActiveFilters || filterErrors.length > 0 ? (
                <button
                  type="button"
                  data-testid="dl-filters-reset"
                  onClick={handleResetFilters}
                  className="cursor-pointer text-[11px] font-medium text-accent hover:underline"
                >
                  重置规则
                </button>
              ) : null}
            </div>

            <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
              <FilterNumberInput
                label="抓取深度"
                draft={depthDraft}
                invalid={invalidFilterFields.has("depth")}
              />
              <FilterNumberInput
                label="最小(KB)"
                draft={minKbDraft}
                invalid={invalidFilterFields.has("minKB")}
              />
              <FilterNumberInput
                label="最大(KB)"
                draft={maxKbDraft}
                invalid={invalidFilterFields.has("maxKB")}
              />
              <FilterNumberInput
                label="最小宽度"
                draft={minWDraft}
                invalid={invalidFilterFields.has("minW")}
              />
              <FilterNumberInput
                label="最大宽度"
                draft={maxWDraft}
                invalid={invalidFilterFields.has("maxW")}
              />
              <FilterNumberInput
                label="最小高度"
                draft={minHDraft}
                invalid={invalidFilterFields.has("minH")}
              />
              <FilterNumberInput
                label="最大高度"
                draft={maxHDraft}
                invalid={invalidFilterFields.has("maxH")}
              />
            </div>

            {filterErrors.length > 0 ? (
              <div
                data-testid="dl-filter-errors"
                className="flex items-start gap-1.5 rounded-[10px] border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] px-2.5 py-1.5 text-[11px] text-status-danger"
              >
                <Icon name="alert-circle" size={12} className="mt-0.5 shrink-0" />
                <span>{filterErrors.join("；")}</span>
              </div>
            ) : null}
          </div>
        ) : null}

        {noticeText ? (
          <div
            data-testid="dl-composer-notice"
            className="flex items-start justify-between gap-2 rounded-[12px] border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] px-3 py-2 text-xs text-status-danger select-text"
          >
            <span className="min-w-0 flex-1 break-words">{noticeText}</span>
            <div className="flex shrink-0 items-center gap-1">
              {!browserReady && browserSupported && !st.browserInstallBusy ? (
                <Button
                  text="前往安装"
                  size="xs"
                  variant="outline"
                  onClick={openBrowserSettings}
                />
              ) : showQuickBrowserAction ? (
                <Button
                  text={st.browserOpen ? "聚焦指纹浏览器" : "打开指纹浏览器"}
                  size="xs"
                  variant="outline"
                  onClick={handleOpenBrowser}
                />
              ) : null}
              <IconButton
                icon="x"
                size={18}
                variant="ghost"
                aria-label="关闭提示"
                onClick={dismissAllErrors}
              />
            </div>
          </div>
        ) : null}
      </div>
    </Dialog>
  );
}

export function DownloadImportDialog({
  open,
  st,
  onClose,
}: {
  open: boolean;
  st: DownloadStateDTO;
  onClose: () => void;
}) {
  const sheets = st.import?.sheets ?? [];
  const columns = st.import?.columns ?? [];
  const urlColCount = columns.filter((c) => c.isUrl).length;
  const nameColCount = columns.filter((c) => c.isName).length;
  const imageCount = st.import?.imageCount ?? 0;

  return (
    <Dialog
      data-testid="dl-import-dialog"
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      title={`导入表格 · ${st.import?.fileName || ""}`}
      description={`识别到 ${imageCount} 条链接（忽略无效 ${st.import?.invalid ?? 0} 条）`}
      width={580}
      closeOnEscape
      footer={
        <div className="flex w-full flex-wrap items-center justify-between gap-2">
          <div className="tnum flex flex-wrap items-center gap-1.5 text-[11px] text-muted">
            <Badge
              text={`网址列 ${urlColCount}`}
              variant={urlColCount > 0 ? "accent" : "warn"}
              className="tnum"
            />
            <Badge text={`命名列 ${nameColCount}`} variant="neutral" className="tnum" />
            <Badge
              text={`可导入 ${imageCount} 条`}
              variant={imageCount > 0 ? "success" : "neutral"}
              className="tnum"
            />
          </div>
          <div className="flex items-center gap-2">
            <Button text="取消" size="small" variant="ghost" onClick={onClose} />
            <Button
              text="确认导入"
              size="small"
              icon="download"
              variant="primary"
              disabled={imageCount === 0}
              onClick={() => runIpc(() => Download.downloadImportAction("submit", "", 0))}
            />
          </div>
        </div>
      }
    >
      <div className="flex min-h-0 flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2.5">
            {sheets.length > 1 ? (
              <div className="w-[168px]">
                <Select
                  data-testid="dl-import-sheet-select"
                  size="small"
                  aria-label="选择工作表"
                  value={st.import?.sheet || sheets[0]}
                  options={sheets.map((name) => ({ value: name, label: name }))}
                  onValueChange={(val) =>
                    runIpc(() => Download.downloadImportAction("sheet", val, 0))
                  }
                />
              </div>
            ) : sheets.length === 1 ? (
              <Badge text={`工作表：${sheets[0]}`} variant="neutral" />
            ) : null}
            <div className="flex items-center gap-1.5">
              <Checkbox
                label="按列创建子目录"
                checked={Boolean(st.import?.folders)}
                onCheckedChange={() =>
                  runIpc(() => Download.downloadImportAction("toggleFolders", "", 0))
                }
              />
              {st.import?.folders && urlColCount <= 1 ? (
                <span className="text-[11px] text-subtle">（多网址列时生效）</span>
              ) : null}
            </div>
          </div>
        </div>

        {st.import?.err ? (
          <div className="rounded-[10px] border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] px-3 py-2 text-xs text-status-danger select-text">
            {st.import.err}
          </div>
        ) : null}

        <div className="flex max-h-[340px] flex-col gap-2 overflow-y-auto pr-1">
          {columns.length === 0 ? (
            <EmptyNotice
              icon="file-spreadsheet"
              title="未解析到表格列"
              subtitle="请检查文件是否包含表头或切换其他工作表"
            />
          ) : (
            columns.map((col) => (
              <InsetPillRow
                key={col.index}
                active={Boolean(col.isUrl)}
                className="flex-row items-center justify-between gap-3"
              >
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="truncate text-xs font-medium text-ink">{col.name}</span>
                  <span className="truncate text-[11px] text-muted select-text">
                    {col.sample || "（空示例）"}
                  </span>
                </div>
                <div className="flex shrink-0 items-center gap-3">
                  <Checkbox
                    size="small"
                    label="网址列"
                    checked={Boolean(col.isUrl)}
                    onCheckedChange={() =>
                      runIpc(() => Download.downloadImportAction("toggleURL", "", col.index))
                    }
                  />
                  <Checkbox
                    size="small"
                    label="命名列"
                    checked={Boolean(col.isName)}
                    onCheckedChange={() =>
                      runIpc(() => Download.downloadImportAction("toggleName", "", col.index))
                    }
                  />
                </div>
              </InsetPillRow>
            ))
          )}
        </div>
      </div>
    </Dialog>
  );
}

export function DownloadGuideDialog({
  open,
  st,
  onClose,
}: {
  open: boolean;
  st: DownloadStateDTO;
  onClose: () => void;
}) {
  const [onlyFingerprint, setOnlyFingerprint] = useState(false);
  const guideGroups = st.guideGroups ?? [];
  const browserSupported = st.browserSupported !== false;
  const guideSearchDraft = useSyncedDraft(
    st.guideSearch || "",
    "dl-guide-search",
    (val) => runIpc(() => Download.setDownloadField("guideSearch", val)),
    { debounceMs: 90 },
  );

  const { totalEntries, fingerprintEntries, visibleGroups } = useMemo(() => {
    let total = 0;
    let fpCount = 0;
    const filtered = [];
    for (const grp of guideGroups) {
      const entries = grp.entries ?? [];
      total += entries.length;
      const fpEntries = entries.filter((entry) => requiresFingerprintGuide(entry.pages));
      fpCount += fpEntries.length;
      const shown = onlyFingerprint ? fpEntries : entries;
      if (shown.length > 0) {
        filtered.push({ ...grp, entries: shown });
      }
    }
    return {
      totalEntries: total,
      fingerprintEntries: fpCount,
      visibleGroups: filtered,
    };
  }, [guideGroups, onlyFingerprint]);

  const handleOpenSiteInBrowser = (domain: string) => {
    if (!domain || !browserSupported) return;
    runIpc(() => Download.openDownloadBrowser(`https://${domain}`));
    onClose();
  };

  return (
    <Dialog
      data-testid="dl-guide-dialog"
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      title="支持站点与抓取说明"
      description="点击域名可复制，或直接在指纹浏览器中打开"
      width={660}
      closeOnBackdrop
      closeOnEscape
    >
      <div className="flex min-h-0 flex-col gap-2.5">
        <div className="flex flex-wrap items-center gap-2">
          <div className="min-w-[200px] flex-1">
            <Input
              size="small"
              leftIcon="search"
              value={guideSearchDraft.value}
              placeholder="搜索站点名称、域名或抓取方式..."
              aria-label="搜索支持站点"
              onChange={(e) => guideSearchDraft.onChange(e.target.value)}
              onCompositionStart={guideSearchDraft.onCompositionStart}
              onCompositionEnd={(e) => guideSearchDraft.onCompositionEnd(e.currentTarget.value)}
              onBlur={guideSearchDraft.onBlur}
              rightSlot={
                guideSearchDraft.value ? (
                  <IconButton
                    icon="x"
                    size={18}
                    variant="ghost"
                    aria-label="清空站点搜索"
                    onClick={() => guideSearchDraft.setValue("", true)}
                  />
                ) : undefined
              }
            />
          </div>
          <button
            type="button"
            data-testid="dl-guide-fingerprint-filter"
            aria-pressed={onlyFingerprint}
            onClick={() => setOnlyFingerprint((prev) => !prev)}
            className={cn(
              "inline-flex h-8 shrink-0 cursor-pointer items-center gap-1.5 rounded-[10px] border px-2.5 text-xs font-medium transition-colors select-none",
              onlyFingerprint
                ? "border-accent-border bg-accent-soft text-accent"
                : "border-hairline bg-sunken text-ink hover:bg-sunken-hover",
            )}
          >
            <Icon name="globe" size={13} />
            <span>需指纹浏览器</span>
            <Badge
              text={`${fingerprintEntries}/${totalEntries}`}
              variant={onlyFingerprint ? "accent" : "neutral"}
              className="tnum px-1.5 py-0 text-[10px]"
            />
          </button>
        </div>

        <div className="flex max-h-[430px] flex-col gap-3 overflow-y-auto pr-1">
          {visibleGroups.length === 0 ? (
            <EmptyNotice
              icon="search"
              title="未匹配到站点"
              subtitle={
                onlyFingerprint
                  ? "当前筛选下无匹配站点，可取消「需指纹浏览器」或更换关键词"
                  : "尝试输入其他关键词或清空搜索"
              }
              action={
                onlyFingerprint || guideSearchDraft.value ? (
                  <Button
                    text="重置筛选"
                    size="small"
                    variant="outline"
                    onClick={() => {
                      setOnlyFingerprint(false);
                      if (guideSearchDraft.value) {
                        guideSearchDraft.setValue("", true);
                      }
                    }}
                  />
                ) : undefined
              }
            />
          ) : (
            visibleGroups.map((grp) => {
              const entries = grp.entries ?? [];
              return (
                <div key={grp.title || "group"} className="flex flex-col gap-1.5">
                  <div className="flex items-center justify-between gap-2 px-1">
                    <span className="text-xs font-semibold text-ink">{grp.title}</span>
                    <span className="tnum text-[11px] text-subtle">{entries.length} 项</span>
                  </div>
                  {entries.map((entry) => {
                    const domain = getSiteDomain(entry.icons);
                    const needsFingerprint = requiresFingerprintGuide(entry.pages);
                    return (
                      <InsetPillRow
                        key={`${grp.title}-${entry.site}-${domain}`}
                        className="flex-row items-start gap-2.5 py-2"
                      >
                        <SiteGuideIcon domain={domain} site={entry.site} />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <span className="text-[12.5px] font-medium text-ink">
                              {entry.site}
                            </span>
                            {entry.icons ? (
                              <Tooltip content={`点击复制域名 ${domain || entry.icons}`} side="top">
                                <button
                                  type="button"
                                  onClick={() =>
                                    copyDownloadText(
                                      domain || entry.icons,
                                      `已复制域名 ${domain || entry.icons}`,
                                    )
                                  }
                                  className="font-mono-console cursor-pointer rounded-[5px] border border-hairline bg-card px-1.5 py-px text-[10.5px] text-muted transition-colors hover:border-accent-border hover:text-accent"
                                >
                                  {entry.icons}
                                </button>
                              </Tooltip>
                            ) : null}
                            {needsFingerprint ? (
                              <Badge
                                text="指纹浏览器"
                                variant="accent"
                                className="px-1.5 py-0 text-[10px]"
                              />
                            ) : null}
                          </div>
                          {entry.pages ? (
                            <div className="mt-0.5 text-[11.5px] leading-snug text-muted">
                              {entry.pages}
                            </div>
                          ) : null}
                        </div>
                        {domain ? (
                          <Tooltip
                            content={
                              browserSupported
                                ? `在指纹浏览器中打开 https://${domain}`
                                : "当前系统暂不支持指纹浏览器"
                            }
                            side="left"
                          >
                            <IconButton
                              icon="external-link"
                              size="xs"
                              variant="ghost"
                              aria-label={`在指纹浏览器打开 ${entry.site}`}
                              disabled={!browserSupported || Boolean(st.browserBusy)}
                              onClick={() => handleOpenSiteInBrowser(domain)}
                              className="shrink-0"
                            />
                          </Tooltip>
                        ) : null}
                      </InsetPillRow>
                    );
                  })}
                </div>
              );
            })
          )}
        </div>
      </div>
    </Dialog>
  );
}
