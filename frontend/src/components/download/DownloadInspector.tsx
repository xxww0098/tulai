import { BentoCard, InsetPillRow, OutlineBadge, StatusBadge } from "@/components/primitives";
import { Button, Icon, IconButton, Progress } from "@/components/ui";
import { Core, Download } from "@/lib/ipc";
import type { DownloadTaskDTO } from "@/store";
import {
  computeTaskETA,
  copyDownloadText,
  formatBytes,
  formatSpeed,
  formatTaskSize,
  hasValidTaskPath,
  isTaskActive,
  isTaskCanceled,
  isTaskDone,
  isTaskFailed,
  isTaskPaused,
  isTaskQueued,
  isValidHttpUrl,
  resolvedTaskTone,
  runIpc,
  taskFraction,
  taskProgressTone,
} from "./downloadFormat";

export function DownloadInspector({
  task,
  defaultDir,
  browserSupported = true,
  browserBusy = false,
  onClose,
}: {
  task: DownloadTaskDTO;
  defaultDir: string;
  browserSupported?: boolean;
  browserBusy?: boolean;
  onClose: () => void;
}) {
  const active = isTaskActive(task);
  const queued = isTaskQueued(task);
  const paused = isTaskPaused(task);
  const failed = isTaskFailed(task);
  const done = isTaskDone(task);
  const canceled = isTaskCanceled(task);

  const hasValidPath = hasValidTaskPath(task);
  const canOpenInBrowser =
    browserSupported &&
    isValidHttpUrl(task.url) &&
    (task.rendered ||
      (failed && (task.isPage || /指纹浏览器|下载浏览器|验证|登录/.test(task.err || ""))));

  const fraction = taskFraction(task);
  const pct = Math.round(fraction * 100);
  const eta = computeTaskETA(task);
  const saveDir = task.dir || defaultDir;
  const canOpenSaveDir = Boolean(saveDir && saveDir !== "默认下载目录");

  return (
    <BentoCard
      data-testid="dl-inspector"
      compact
      className="animate-tulai-drawer-right flex max-h-[42%] w-full shrink-0 flex-col gap-2.5 md:h-full md:max-h-none md:w-[252px] xl:w-[276px]"
      title="任务详情"
      icon="info"
      right={
        <IconButton
          icon="x"
          size="xs"
          variant="ghost"
          aria-label="关闭任务详情"
          onClick={onClose}
        />
      }
    >
      <div className="flex min-h-0 flex-1 flex-col gap-2.5 overflow-y-auto pr-0.5">
        {/* Name + Status */}
        <div className="flex flex-col gap-1.5 rounded-[12px] border border-hairline bg-sunken p-2.5">
          <div className="flex items-start justify-between gap-2">
            <span className="line-clamp-2 text-xs font-semibold text-ink select-text">
              {task.name || `任务 #${task.id}`}
            </span>
            <StatusBadge
              text={task.phase || "等待"}
              tone={resolvedTaskTone(task)}
              dot={active}
              pulseDot={active}
            />
          </div>

          <Progress value={fraction} tone={taskProgressTone(task)} size="md" />

          <div className="tnum flex items-center justify-between gap-2 text-[11px] text-muted">
            <span className="truncate">{formatTaskSize(task)}</span>
            <span className={active ? "shrink-0 font-medium text-accent" : "shrink-0"}>
              {active
                ? `${pct}% · ${formatSpeed(task.speed, "连接中")}${eta ? ` · 剩 ${eta}` : ""}`
                : `${pct}%`}
            </span>
          </div>
        </div>

        {/* Metadata rows */}
        <div className="flex flex-col gap-1.5 text-[11.5px]">
          <InsetPillRow className="gap-1 py-2">
            <span className="text-[10.5px] text-muted">来源与方式</span>
            <div className="flex flex-wrap items-center gap-1.5">
              {task.site ? <OutlineBadge text={task.site} /> : null}
              <span className="text-xs font-medium text-ink">
                {task.rendered ? "指纹浏览器" : "直接下载"} ·{" "}
                {task.isPage ? "网页抓图" : "单文件"}
              </span>
            </div>
          </InsetPillRow>

          {task.isPage ? (
            <InsetPillRow className="gap-0.5 py-2">
              <span className="text-[10.5px] text-muted">图片统计</span>
              <span className="tnum text-xs text-ink">
                已下 {task.imagesDone} / 总计 {task.imagesTotal || "—"} 张
                {task.imagesFiltered > 0 ? ` · 已过滤 ${task.imagesFiltered} 张` : ""}
                {task.written > 0 ? ` · ${formatBytes(task.written)}` : ""}
              </span>
            </InsetPillRow>
          ) : null}

          <InsetPillRow className="gap-1 py-2">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[10.5px] text-muted">保存目录</span>
              {canOpenSaveDir ? (
                <button
                  type="button"
                  onClick={() =>
                    runIpc(() =>
                      task.dir ? Core.openPath(task.dir) : Download.downloadOpenDir(),
                    )
                  }
                  className="inline-flex cursor-pointer items-center gap-1 text-[10.5px] font-medium text-accent hover:underline"
                >
                  <Icon name="folder-open" size={11} />
                  <span>打开目录</span>
                </button>
              ) : null}
            </div>
            <span className="font-mono-console break-all text-[11px] leading-snug text-ink select-text">
              {saveDir}
            </span>
          </InsetPillRow>

          {hasValidPath ? (
            <InsetPillRow className="gap-0.5 py-2">
              <span className="text-[10.5px] text-muted">本地路径</span>
              <span className="font-mono-console break-all text-[11px] leading-snug text-ink select-text">
                {task.path}
              </span>
            </InsetPillRow>
          ) : null}

          <InsetPillRow className="gap-1 py-2">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[10.5px] text-muted">下载链接</span>
              {task.url ? (
                <button
                  type="button"
                  onClick={() => copyDownloadText(task.url)}
                  className="inline-flex cursor-pointer items-center gap-1 text-[10.5px] font-medium text-accent hover:underline"
                >
                  <Icon name="copy" size={11} />
                  <span>复制</span>
                </button>
              ) : null}
            </div>
            <span className="font-mono-console block max-h-[76px] overflow-y-auto break-all text-[11px] leading-[1.45] text-muted select-text">
              {task.url}
            </span>
          </InsetPillRow>
        </div>

        {task.err ? (
          <div
            data-testid="dl-inspector-error"
            className="flex flex-col gap-2 rounded-[12px] border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] px-2.5 py-2 text-[11px] leading-relaxed break-words text-status-danger select-text"
          >
            <div>{task.err}</div>
            {failed && canOpenInBrowser ? (
              <div className="flex flex-wrap items-center gap-1.5 border-t border-[var(--status-danger-border)]/70 pt-2">
                <Button
                  data-testid="dl-inspector-open-browser"
                  text={browserBusy ? "正在打开..." : "在指纹浏览器打开"}
                  size="xs"
                  icon="globe"
                  variant="outline"
                  busy={browserBusy}
                  disabled={browserBusy}
                  onClick={() => runIpc(() => Download.openDownloadBrowser(task.url))}
                />
              </div>
            ) : null}
          </div>
        ) : null}
      </div>

      {/* Action footer */}
      <div className="flex flex-wrap items-center gap-1.5 border-t border-divider pt-2.5">
        {active || queued ? (
          <Button
            text="暂停"
            size="xs"
            icon="pause"
            variant="outline"
            onClick={() => runIpc(() => Download.downloadRowAction(task.id, "pause"))}
          />
        ) : null}
        {paused ? (
          <Button
            text="继续"
            size="xs"
            icon="play"
            variant="primary"
            onClick={() => runIpc(() => Download.downloadRowAction(task.id, "resume"))}
          />
        ) : null}
        {failed || canceled || done ? (
          <Button
            text={done ? "重新下载" : "重试"}
            size="xs"
            icon="refresh-cw"
            variant={failed ? "primary" : "outline"}
            onClick={() => runIpc(() => Download.downloadRowAction(task.id, "retry"))}
          />
        ) : null}
        {hasValidPath ? (
          <>
            <Button
              text="打开"
              size="xs"
              icon="external-link"
              variant="secondary"
              onClick={() => runIpc(() => Download.downloadRowAction(task.id, "open"))}
            />
            <Button
              text="定位"
              size="xs"
              icon="folder-open"
              variant="secondary"
              onClick={() => runIpc(() => Download.downloadRowAction(task.id, "reveal"))}
            />
          </>
        ) : null}
        {!task.terminal || failed ? (
          <Button
            text="移入回收站"
            size="xs"
            icon="ban"
            variant="ghost"
            onClick={() => runIpc(() => Download.downloadRowAction(task.id, "cancel"))}
          />
        ) : null}
        <Button
          text="移除"
          size="xs"
          icon="trash-2"
          variant="ghost"
          onClick={() => runIpc(() => Download.downloadRowAction(task.id, "remove"))}
          className="ml-auto hover:text-status-danger"
        />
      </div>
    </BentoCard>
  );
}
