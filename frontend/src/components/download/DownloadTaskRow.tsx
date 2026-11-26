import { OutlineBadge, StatusBadge } from "@/components/primitives";
import {
  Checkbox,
  ContextMenu,
  Icon,
  IconButton,
  Progress,
  Tooltip,
  TooltipGroup,
  type MenuActionItem,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { Download } from "@/lib/ipc";
import type { DownloadTaskDTO } from "@/store";
import {
  computeTaskETA,
  copyDownloadText,
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

export function DownloadTaskRow({
  task,
  selected,
  browserSupported = true,
  onSelect,
}: {
  task: DownloadTaskDTO;
  selected: boolean;
  browserSupported?: boolean;
  onSelect: (id: number) => void;
}) {
  const active = isTaskActive(task);
  const queued = isTaskQueued(task);
  const paused = isTaskPaused(task);
  const failed = isTaskFailed(task);
  const done = isTaskDone(task);
  const canceled = isTaskCanceled(task);

  const canPause = active || queued;
  const canResume = paused;
  const canRetry = failed || canceled || done;
  const hasPath = hasValidTaskPath(task);
  const canOpenInBrowser =
    browserSupported && isValidHttpUrl(task.url) && (task.rendered || task.isPage || failed);

  const fraction = taskFraction(task);
  const pct = Math.round(fraction * 100);
  const sizeLabel = formatTaskSize(task);
  const eta = computeTaskETA(task);

  const tileStyle = active
    ? "border-accent-border bg-accent-soft text-accent"
    : done
      ? "border-[var(--status-ok-border)] bg-[var(--status-ok-bg)] text-status-ok"
      : failed
        ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
        : paused
          ? "border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] text-status-warn"
          : "border-hairline bg-card text-muted";

  const speedPrimary = active
    ? task.speed > 0
      ? formatSpeed(task.speed)
      : "连接中..."
    : paused
      ? "已暂停"
      : queued
        ? "排队等待"
        : done
          ? "已完成"
          : "—";

  const speedSecondary = active
    ? eta
      ? `剩余 ${eta}`
      : `${pct}%`
    : paused
      ? `进度 ${pct}%`
      : done
        ? task.imagesFiltered > 0
          ? `已过滤 ${task.imagesFiltered} 张`
          : "100%"
        : "";

  const menuItems: MenuActionItem[] = [
    ...(canPause
      ? [
          {
            id: "pause",
            label: "暂停任务",
            icon: "pause",
            onSelect: () => runIpc(() => Download.downloadRowAction(task.id, "pause")),
          },
        ]
      : []),
    ...(canResume
      ? [
          {
            id: "resume",
            label: "继续下载",
            icon: "play",
            onSelect: () => runIpc(() => Download.downloadRowAction(task.id, "resume")),
          },
        ]
      : []),
    ...(canRetry
      ? [
          {
            id: "retry",
            label: done ? "重新下载" : "重试任务",
            icon: "refresh-cw",
            onSelect: () => runIpc(() => Download.downloadRowAction(task.id, "retry")),
          },
        ]
      : []),
    ...(canOpenInBrowser
      ? [
          {
            id: "open-browser",
            label: "在指纹浏览器打开",
            icon: "globe",
            onSelect: () => runIpc(() => Download.openDownloadBrowser(task.url)),
          },
        ]
      : []),
    ...(hasPath
      ? [
          {
            id: "open",
            label: "打开文件",
            icon: "external-link",
            onSelect: () => runIpc(() => Download.downloadRowAction(task.id, "open")),
          },
          {
            id: "reveal",
            label: "打开所在目录",
            icon: "folder-open",
            onSelect: () => runIpc(() => Download.downloadRowAction(task.id, "reveal")),
          },
        ]
      : []),
    { type: "separator" as const },
    {
      id: "copy-url",
      label: "复制下载链接",
      icon: "copy",
      onSelect: () => copyDownloadText(task.url),
    },
    {
      id: "inspect",
      label: selected ? "收起详情" : "查看任务详情",
      icon: "info",
      onSelect: () => onSelect(selected ? 0 : task.id),
    },
    { type: "separator" as const },
    ...(!task.terminal || failed
      ? [
          {
            id: "cancel",
            label: "移入回收站",
            icon: "trash-2",
            onSelect: () => runIpc(() => Download.downloadRowAction(task.id, "cancel")),
          },
        ]
      : []),
    {
      id: "remove",
      label: "从列表移除",
      icon: "x",
      danger: true,
      onSelect: () => runIpc(() => Download.downloadRowAction(task.id, "remove")),
    },
  ];

  return (
    <ContextMenu items={menuItems}>
      <div
        data-testid={`dl-task-${task.id}`}
        onClick={() => onSelect(selected ? 0 : task.id)}
        className={cn(
          "group flex cursor-pointer items-center gap-2.5 rounded-[12px] border px-3 py-2.5 transition-colors duration-150",
          selected
            ? "border-accent-border bg-accent-soft/75"
            : task.checked
              ? "border-accent-border/70 bg-accent-soft/35 hover:bg-accent-soft/55"
              : "border-hairline bg-sunken/70 hover:border-strong hover:bg-sunken",
        )}
      >
        {/* Column 1: Checkbox + File/Page Icon + Title, Progress & Inline Compact Meta */}
        <div className="flex min-w-0 flex-1 items-center gap-2.5 overflow-hidden">
          <div onClick={(e) => e.stopPropagation()} className="flex shrink-0 items-center">
            <Checkbox
              size="small"
              checked={Boolean(task.checked)}
              aria-label={`选择任务 ${task.name || task.id}`}
              onCheckedChange={(checked) =>
                runIpc(() => Download.toggleDownloadCheck(task.id, checked))
              }
            />
          </div>

          <div
            className={cn(
              "hidden size-8 shrink-0 items-center justify-center rounded-[10px] border transition-colors @[360px]:flex",
              tileStyle,
            )}
          >
            <Icon name={task.isPage || task.rendered ? "globe" : "file-image"} size={15} />
          </div>

          <div className="flex min-w-0 flex-1 flex-col gap-1 overflow-hidden">
            <div className="flex min-w-0 items-center gap-1.5 overflow-hidden">
              <Tooltip content={task.url || task.name} side="top">
                <span
                  tabIndex={0}
                  className="min-w-0 truncate rounded text-[12.5px] font-medium text-ink select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                >
                  {task.name || task.url || `任务 #${task.id}`}
                </span>
              </Tooltip>
              {task.site ? (
                <OutlineBadge
                  text={task.site}
                  className="hidden max-w-[104px] truncate py-0 text-[10px] @[420px]:inline-flex"
                />
              ) : null}
              {task.rendered ? (
                <span className="shrink-0 rounded-full border border-accent-border bg-accent-soft px-1.5 py-0 text-[10px] font-medium whitespace-nowrap text-accent">
                  浏览器
                </span>
              ) : null}
            </div>

            <div className="flex min-w-0 items-center gap-2">
              <div className="min-w-[36px] flex-1">
                <Progress value={fraction} tone={taskProgressTone(task)} size="sm" />
              </div>
              {/* Compact inline telemetry when Size/Speed columns collapse on narrower panes */}
              <span className="tnum flex shrink-0 items-center gap-1.5 text-[10.5px] whitespace-nowrap text-muted @[600px]:hidden">
                {sizeLabel !== "—" ? (
                  <span className="hidden max-w-[92px] truncate @[340px]:inline">{sizeLabel}</span>
                ) : null}
                <span>{pct}%</span>
                <span
                  className={cn(
                    "@[480px]:hidden",
                    active ? "font-medium text-accent" : failed ? "font-medium text-status-danger" : "",
                  )}
                >
                  · {task.phase || "等待"}
                </span>
              </span>
              {active ? (
                <span className="tnum hidden shrink-0 text-[10.5px] font-medium whitespace-nowrap text-accent @[600px]:inline @[740px]:hidden">
                  {speedPrimary}
                  {eta ? ` · 剩 ${eta}` : ""}
                </span>
              ) : null}
            </div>

            {task.err ? (
              <Tooltip content={task.err} side="top">
                <div
                  tabIndex={0}
                  className="truncate rounded text-[10.5px] leading-snug text-status-danger select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
                >
                  {task.err}
                </div>
              </Tooltip>
            ) : null}
          </div>
        </div>

        {/* Column 2: Size / Image Count */}
        <div className="tnum hidden w-[92px] shrink-0 flex-col items-end text-right @[600px]:flex">
          <span className="max-w-full truncate text-[11.5px] font-medium text-ink">
            {sizeLabel}
          </span>
          <span className="text-[10.5px] text-muted">{pct}%</span>
        </div>

        {/* Column 3: Speed & Xunlei ETA */}
        <div className="tnum hidden w-[96px] shrink-0 flex-col items-end text-right @[740px]:flex">
          <span
            className={cn(
              "max-w-full truncate text-[11.5px] font-medium",
              active ? "text-accent" : "text-muted",
            )}
          >
            {speedPrimary}
          </span>
          {speedSecondary ? (
            <span className="max-w-full truncate text-[10.5px] text-subtle">{speedSecondary}</span>
          ) : null}
        </div>

        {/* Column 4: Status Badge */}
        <div className="hidden w-[64px] shrink-0 justify-center @[480px]:flex">
          <StatusBadge
            text={task.phase || "等待"}
            tone={resolvedTaskTone(task)}
            dot={active}
            pulseDot={active}
          />
        </div>

        {/* Column 5: Contextual Quick Actions */}
        <TooltipGroup
          onClick={(e) => e.stopPropagation()}
          className="flex w-auto shrink-0 items-center justify-end gap-0.5 @[480px]:w-[104px]"
        >
          {canPause ? (
            <Tooltip content="暂停任务" side="top">
              <IconButton
                icon="pause"
                size="xs"
                variant="outline"
                aria-label="暂停"
                onClick={() => runIpc(() => Download.downloadRowAction(task.id, "pause"))}
              />
            </Tooltip>
          ) : null}
          {canResume ? (
            <Tooltip content="继续下载" side="top">
              <IconButton
                icon="play"
                size="xs"
                variant="primary"
                aria-label="继续"
                onClick={() => runIpc(() => Download.downloadRowAction(task.id, "resume"))}
              />
            </Tooltip>
          ) : null}
          {canRetry ? (
            <Tooltip content={done ? "重新下载" : "重试"} side="top">
              <IconButton
                icon="refresh-cw"
                size="xs"
                variant="outline"
                aria-label="重试"
                onClick={() => runIpc(() => Download.downloadRowAction(task.id, "retry"))}
              />
            </Tooltip>
          ) : null}
          {hasPath ? (
            <>
              <Tooltip content="打开文件" side="top">
                <IconButton
                  icon="external-link"
                  size="xs"
                  variant="ghost"
                  aria-label="打开"
                  onClick={() => runIpc(() => Download.downloadRowAction(task.id, "open"))}
                />
              </Tooltip>
              <Tooltip content="打开所在目录" side="top">
                <IconButton
                  icon="folder-open"
                  size="xs"
                  variant="ghost"
                  aria-label="定位"
                  onClick={() => runIpc(() => Download.downloadRowAction(task.id, "reveal"))}
                />
              </Tooltip>
            </>
          ) : null}
          <Tooltip content="从列表移除" side="top">
            <IconButton
              icon="trash-2"
              size="xs"
              variant="ghost"
              aria-label="移除"
              onClick={() => runIpc(() => Download.downloadRowAction(task.id, "remove"))}
              className="hover:text-status-danger"
            />
          </Tooltip>
        </TooltipGroup>
      </div>
    </ContextMenu>
  );
}
