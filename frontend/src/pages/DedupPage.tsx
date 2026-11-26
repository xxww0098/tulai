import { useState } from "react";
import { HoverPreviewTarget } from "@/components/HoverPreview";
import {
  BentoCard,
  EmptyNotice,
  FlushPane,
  InsetPillRow,
  OutlineBadge,
  PageSkeleton,
  PageToolbar,
  PaneHeader,
  SegmentedPill,
} from "@/components/primitives";
import { Badge, Button, DropZoneOverlay, Icon, IconButton, Progress, Switch, Tooltip } from "@/components/ui";
import { toThumbUrl } from "@/lib/assets";
import { cn } from "@/lib/cn";
import { Core, Dedup, formatCallError, isMyGoRuntime, useFileDrop } from "@/lib/ipc";
import { showToast, useDedup, usePageLoading, type DedupStateDTO } from "@/store";

const LEVEL_TABS = [
  { value: 0, label: "严格" },
  { value: 1, label: "均衡 (推荐)" },
  { value: 2, label: "宽松" },
];

const EMPTY_DEDUP_STATE: DedupStateDTO = {
  items: [],
  itemCount: 0,
  level: 1,
  levelHint: "",
  threshold: 0,
  outDir: "",
  resolvedOut: "",
  more: false,
  flatColorOk: false,
  busy: false,
  scanned: 0,
  total: 0,
  note: "",
  err: "",
  hasReport: false,
  unique: 0,
  duplicates: 0,
  groupCount: 0,
  savedDir: "",
  groups: [],
};

function runIpc(fn: () => unknown): void {
  try {
    void Promise.resolve(fn()).catch((err) => {
      if (isMyGoRuntime()) {
        showToast(formatCallError(err), "error");
      }
    });
  } catch (err) {
    if (isMyGoRuntime()) {
      showToast(formatCallError(err), "error");
    }
  }
}

export function DedupPage() {
  const rawState = useDedup();
  const loading = usePageLoading("dedup");
  const st = rawState ?? EMPTY_DEDUP_STATE;
  const [poolCollapsed, setPoolCollapsed] = useState(false);

  const { isDragging } = useFileDrop({
    enabled: !Boolean(st.busy),
    onDrop: (paths) => {
      if (paths.length > 0) {
        runIpc(() => Dedup.dedupIngestPaths(paths));
      }
    },
  });

  if (!rawState && loading) {
    return <PageSkeleton rows={5} />;
  }

  const items = st.items ?? [];
  const visibleItems = st.more ? items : items.slice(0, 24);
  const groups = st.groups ?? [];
  const outDirLabel = st.resolvedOut || st.outDir || "自动";

  return (
    <div className="relative flex h-full min-h-0 flex-1 flex-col bg-paper">
      <DropZoneOverlay
        active={isDragging}
        label="松开以导入待去重图片"
        sublabel="支持拖入多张图片或文件夹"
      />

      {/* 顶部悬浮操作栏：素材导入与启动去重 */}
      <PageToolbar
        left={
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              text="添加图片"
              size="small"
              icon="plus"
              variant="outline"
              disabled={Boolean(st.busy)}
              onClick={() => runIpc(() => Dedup.dedupAddFiles())}
            />
            <Button
              text="添加目录"
              size="small"
              icon="folder"
              variant="outline"
              disabled={Boolean(st.busy)}
              onClick={() => runIpc(() => Dedup.dedupAddFolder())}
            />
            {st.itemCount > 0 ? (
              <Button
                text="清空"
                size="small"
                variant="ghost"
                disabled={Boolean(st.busy)}
                onClick={() => runIpc(() => Dedup.dedupClear())}
              />
            ) : null}
          </div>
        }
        right={
          <Button
            data-testid="dedup-start-btn"
            text={st.busy ? "扫描中..." : "开始去重"}
            size="small"
            icon="layers"
            variant="primary"
            busy={Boolean(st.busy)}
            disabled={st.itemCount < 2 || Boolean(st.busy)}
            onClick={() => runIpc(() => Dedup.dedupStart())}
          />
        }
      />

      {/* 纵向 Bento 流水线：上方参数与样本池 / 下方全宽相似图对照报告 */}
      <div className="flex min-h-0 flex-1 flex-col gap-3 px-3 pb-3">
        {/* 模块 1：全宽紧凑型相似度设置、输出目录与扫描统计条 */}
        <BentoCard compact className="shrink-0">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <div className="flex items-center gap-1 rounded-[12px] bg-sunken p-1">
                {LEVEL_TABS.map((tab) => (
                  <SegmentedPill
                    key={tab.value}
                    data-testid={`dedup-level-${tab.value}`}
                    label={tab.label}
                    active={st.level === tab.value}
                    disabled={Boolean(st.busy)}
                    onClick={() => runIpc(() => Dedup.dedupSetLevel(tab.value))}
                  />
                ))}
              </div>

              <Tooltip
                content="开启后，纯白、纯黑等纯色图将不再比对色差，直接判为重复"
                side="top"
              >
                <div className="flex items-center gap-2 rounded-[12px] border border-hairline bg-sunken px-2.5 py-1.5">
                  <span className="text-xs text-ink select-none">纯色图不区分颜色</span>
                  <Switch
                    size="small"
                    checked={Boolean(st.flatColorOk)}
                    disabled={Boolean(st.busy)}
                    aria-label="纯色图不区分颜色"
                    onCheckedChange={(checked) =>
                      runIpc(() => Dedup.dedupSetFlatColor(checked))
                    }
                  />
                </div>
              </Tooltip>

              <div className="flex min-w-0 items-center gap-1.5 rounded-[12px] border border-hairline bg-sunken py-1 pr-1.5 pl-2.5">
                <Tooltip content={outDirLabel} side="top">
                  <span
                    tabIndex={0}
                    className="font-mono-console max-w-[200px] truncate rounded text-[11px] text-muted select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                  >
                    {`输出：${outDirLabel}`}
                  </span>
                </Tooltip>
                <Button
                  text="更改"
                  size="xs"
                  variant="outline"
                  disabled={Boolean(st.busy)}
                  onClick={() => runIpc(() => Dedup.dedupPickOutDir())}
                />
                <Button
                  text="打开"
                  size="xs"
                  icon="folder-open"
                  variant="ghost"
                  onClick={() => runIpc(() => Dedup.dedupOpenOutDir())}
                />
              </div>
            </div>

            <div className="flex shrink-0 flex-wrap items-center gap-1.5">
              <div className="flex items-center gap-1.5 rounded-[10px] border border-hairline bg-sunken px-2.5 py-1">
                <span className="text-[11px] text-muted">待比对</span>
                <span className="tnum text-xs font-semibold text-ink">{st.itemCount}</span>
              </div>
              <div className="flex items-center gap-1.5 rounded-[10px] border border-hairline bg-sunken px-2.5 py-1">
                <span className="text-[11px] text-muted">保留唯一</span>
                <span className="tnum text-xs font-semibold text-status-ok">
                  {st.hasReport ? st.unique : "-"}
                </span>
              </div>
              <div className="flex items-center gap-1.5 rounded-[10px] border border-hairline bg-sunken px-2.5 py-1">
                <span className="text-[11px] text-muted">发现重复</span>
                <span className="tnum text-xs font-semibold text-status-warn">
                  {st.hasReport ? st.duplicates : "-"}
                </span>
              </div>
            </div>
          </div>

          {st.levelHint || st.savedDir ? (
            <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-[11px] text-muted">
              <span className="leading-relaxed">{st.levelHint || ""}</span>
              {st.savedDir ? (
                <Tooltip content={st.savedDir} side="top">
                  <span
                    tabIndex={0}
                    className="font-mono-console max-w-[360px] truncate rounded select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                  >
                    {`结果输出：${st.savedDir}`}
                  </span>
                </Tooltip>
              ) : null}
            </div>
          ) : null}

          {st.busy || st.note || st.err ? (
            <InsetPillRow className="animate-tulai-banner-in mt-2 gap-1.5 py-2">
              <div className="flex items-center justify-between gap-2">
                <span
                  className={cn(
                    "min-w-0 flex-1 text-xs select-text",
                    st.err ? "text-status-danger" : "text-ink",
                  )}
                >
                  {st.err || st.note || (st.busy ? "正在扫描图片相似度..." : "")}
                </span>
                {st.total > 0 ? (
                  <span className="tnum shrink-0 text-xs font-medium text-accent">
                    {`${st.scanned} / ${st.total}`}
                  </span>
                ) : null}
              </div>
              {st.busy && st.total > 0 ? (
                <Progress value={Math.min(1, st.scanned / st.total)} />
              ) : null}
            </InsetPillRow>
          ) : null}
        </BentoCard>

        {/* 模块 2：全宽紧凑型待扫描图片池 */}
        <FlushPane data-testid="dedup-queue" className="flex-none shrink-0">
          <PaneHeader
            title={`待扫描图片 (${st.itemCount})`}
            subtitle={
              st.itemCount < 2
                ? "至少需要 2 张图片，支持直接拖入"
                : "悬停查看高清大图"
            }
            icon="layers"
            className="py-2"
            right={
              <div className="flex items-center gap-1">
                {st.itemCount > 24 ? (
                  <Button
                    data-testid="dedup-more-btn"
                    text={st.more ? "收起" : "展开"}
                    size="xs"
                    variant="ghost"
                    onClick={() => runIpc(() => Dedup.dedupSetMore(!st.more))}
                  />
                ) : null}
                {items.length > 0 ? (
                  <Tooltip content={poolCollapsed ? "展开待扫描图片池" : "收起待扫描图片池"} side="top">
                    <IconButton
                      icon={
                        <Icon
                          name="chevron-up"
                          size={13}
                          className={cn(
                            "transition-transform duration-180 ease-[cubic-bezier(0.16,1,0.3,1)]",
                            poolCollapsed && "rotate-180",
                          )}
                        />
                      }
                      size={22}
                      variant="ghost"
                      aria-label={poolCollapsed ? "展开待扫描图片池" : "收起待扫描图片池"}
                      onClick={() => setPoolCollapsed((v) => !v)}
                    />
                  </Tooltip>
                ) : null}
              </div>
            }
          />
          <div className="tulai-collapse" data-open={poolCollapsed ? "false" : "true"}>
            <div className="tulai-collapse-inner">
            {items.length === 0 ? (
              <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2.5">
                <div className="flex items-center gap-2 text-xs text-muted">
                  <Icon name="layers" size={14} className="text-subtle" />
                  <span className="font-medium text-ink">尚未添加图片</span>
                  <span className="text-[11px] text-muted">
                    添加图片或文件夹，也可以直接拖拽到这里
                  </span>
                </div>
                <Button
                  text="添加图片"
                  size="xs"
                  icon="plus"
                  variant="outline"
                  disabled={Boolean(st.busy)}
                  onClick={() => runIpc(() => Dedup.dedupAddFiles())}
                />
              </div>
            ) : (
              <div
                className={cn(
                  "overflow-y-auto p-2.5",
                  st.more ? "max-h-[176px]" : "max-h-[120px]",
                )}
              >
                <div
                  className="grid gap-1.5"
                  style={{ gridTemplateColumns: "repeat(auto-fill, minmax(68px, 1fr))" }}
                >
                  {visibleItems.map((item, idx) => {
                    const thumbSrc = toThumbUrl(item.fileUrl, 240);
                    return (
                      <HoverPreviewTarget
                        key={item.path || `${item.name}-${idx}`}
                        previewKey={`dedup-q-${item.path || item.name || idx}`}
                        src={item.fileUrl}
                        title={item.name || "待扫描图片"}
                        subtitle="待去重比对图片"
                        badge="待扫"
                        badgeVariant="neutral"
                        tabIndex={0}
                        openOnEnter
                        openOnTouchTap
                        className="min-w-0 rounded-[10px] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                      >
                        <div className="group overflow-hidden rounded-[10px] border border-hairline bg-sunken p-1 transition-colors hover:border-strong">
                          <div className="flex aspect-square w-full items-center justify-center overflow-hidden rounded-[7px] bg-card dark:bg-[#0D1118]">
                            {thumbSrc ? (
                              <img
                                src={thumbSrc}
                                alt={item.name}
                                loading="lazy"
                                decoding="async"
                                draggable={false}
                                className="h-full w-full object-contain transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:scale-[1.03]"
                              />
                            ) : null}
                          </div>
                          <div className="mt-0.5 truncate text-[10px] text-ink">{item.name}</div>
                        </div>
                      </HoverPreviewTarget>
                    );
                  })}
                </div>
              </div>
            )}
            </div>
          </div>
        </FlushPane>

        {/* 模块 3：下方全宽相似组与对照预览报告 */}
        <div data-testid="dedup-report" className="flex min-h-0 min-w-0 flex-1 flex-col">
          <FlushPane>
            <PaneHeader
              title={
                st.hasReport
                  ? `去重结果 · 保留 ${st.unique} 张 / 重复 ${st.duplicates} 张`
                  : "相似图对照结果"
              }
              subtitle={
                st.savedDir
                  ? `已将保留图片与 CSV 报告保存至：${st.savedDir}`
                  : "点击「开始去重」后在此展示分组对照"
              }
              icon="search"
              right={
                st.hasReport && groups.length > 0 ? (
                  <Badge variant="neutral" text={`共 ${groups.length} 组`} />
                ) : null
              }
            />
            <div className="min-h-0 flex-1 overflow-y-auto p-3">
              {!st.hasReport ? (
                <EmptyNotice
                  icon="search"
                  title={st.itemCount < 2 ? "等待添加图片" : "已就绪，点击「开始去重」"}
                  subtitle={
                    st.itemCount < 2
                      ? "导入至少 2 张图片后即可开始比对"
                      : "将按当前相似度分组，并复制每组最高清原图到输出目录"
                  }
                />
              ) : groups.length === 0 ? (
                <EmptyNotice
                  icon="circle-check"
                  title="未发现重复图片"
                  subtitle="当前相似度阈值下所有图片均为唯一样本"
                />
              ) : (
                <div className="flex flex-col gap-2.5">
                  {groups.map((grp, grpIdx) => {
                    const groupNum = grp.index > 0 ? grp.index : grpIdx + 1;
                    const keepThumb = toThumbUrl(grp.keep.fileUrl, 240);
                    return (
                      <div
                        key={`grp-${groupNum}-${grp.keep.path || grpIdx}`}
                        data-testid={`dedup-group-${groupNum}`}
                        style={{ animationDelay: `${Math.min(grpIdx * 30, 180)}ms` }}
                        className="animate-tulai-stagger-item flex flex-col gap-2.5 rounded-[14px] border border-hairline bg-sunken p-3"
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span className="tnum text-xs font-semibold text-ink">
                            {`相似组 #${groupNum}`}
                          </span>
                          <OutlineBadge text={`视觉差异度 ${grp.distance}`} />
                        </div>

                        <div
                          className="grid gap-2"
                          style={{ gridTemplateColumns: "repeat(auto-fill, minmax(128px, 1fr))" }}
                        >
                          {/* 保留主图 */}
                          <HoverPreviewTarget
                            previewKey={`dedup-keep-${groupNum}-${grp.keep.path || grp.keep.name}`}
                            src={grp.keep.fileUrl}
                            title={grp.keep.name || "保留主图"}
                            subtitle={`${grp.keep.width}×${grp.keep.height}${grp.keep.size ? ` · ${grp.keep.size}` : ""} · 视觉差异度 ${grp.distance}`}
                            badge="保留"
                            badgeVariant="success"
                            tabIndex={0}
                            openOnEnter
                            openOnTouchTap
                            className="min-w-0 rounded-[12px] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                          >
                            <div
                              data-testid={`dedup-keep-${groupNum}`}
                              className="group flex flex-col gap-1 rounded-[12px] border-2 border-emerald-500/80 bg-card p-1.5"
                            >
                              <div className="flex aspect-square w-full items-center justify-center overflow-hidden rounded-[8px] bg-sunken dark:bg-[#0D1118]">
                                {keepThumb ? (
                                  <img
                                    src={keepThumb}
                                    alt={grp.keep.name}
                                    loading="lazy"
                                    decoding="async"
                                    draggable={false}
                                    className="h-full w-full object-contain transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:scale-[1.03]"
                                  />
                                ) : null}
                              </div>
                              <div className="flex items-center justify-between gap-1">
                                <Badge text="保留" variant="success" />
                                <span className="tnum truncate text-[10px] text-muted">
                                  {`${grp.keep.width}×${grp.keep.height}`}
                                </span>
                              </div>
                              <span className="truncate text-[11px] font-medium text-ink">
                                {grp.keep.name}
                              </span>
                            </div>
                          </HoverPreviewTarget>

                          {/* 重复副本卡片（点击定位文件） */}
                          {(grp.dupes ?? []).map((dup, dupIdx) => {
                            const dupThumb = toThumbUrl(dup.fileUrl, 240);
                            const clickable = Boolean(dup.path);
                            return (
                              <HoverPreviewTarget
                                key={dup.path || `${dup.name}-${dupIdx}`}
                                previewKey={`dedup-dup-${groupNum}-${dup.path || dup.name || dupIdx}`}
                                src={dup.fileUrl}
                                title={dup.name || "重复图"}
                                subtitle={`${dup.width}×${dup.height}${dup.size ? ` · ${dup.size}` : ""} · 点击定位文件`}
                                badge="重复"
                                badgeVariant="warn"
                                className="min-w-0"
                              >
                                <button
                                  type="button"
                                  data-testid={`dedup-dupe-${groupNum}-${dupIdx}`}
                                  disabled={!clickable}
                                  onClick={() => {
                                    if (dup.path) {
                                      runIpc(() => Core.revealPath(dup.path));
                                    }
                                  }}
                                  className={cn(
                                    "group flex w-full flex-col gap-1 rounded-[12px] border border-[var(--status-warn-border)] bg-card p-1.5 text-left transition-colors duration-150",
                                    clickable
                                      ? "cursor-pointer hover:border-status-warn"
                                      : "cursor-default",
                                  )}
                                >
                                  <div className="flex aspect-square w-full items-center justify-center overflow-hidden rounded-[8px] bg-sunken dark:bg-[#0D1118]">
                                    {dupThumb ? (
                                      <img
                                        src={dupThumb}
                                        alt={dup.name}
                                        loading="lazy"
                                        decoding="async"
                                        draggable={false}
                                        className="h-full w-full object-contain transition-transform duration-200 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:scale-[1.03]"
                                      />
                                    ) : null}
                                  </div>
                                  <div className="flex items-center justify-between gap-1">
                                    <Badge text="重复" variant="warn" />
                                    <span className="tnum truncate text-[10px] text-muted">
                                      {`${dup.width}×${dup.height}`}
                                    </span>
                                  </div>
                                  <span className="truncate text-[11px] text-ink">
                                    {dup.name}
                                  </span>
                                </button>
                              </HoverPreviewTarget>
                            );
                          })}
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </FlushPane>
        </div>
      </div>
    </div>
  );
}
