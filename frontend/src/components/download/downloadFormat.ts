import type { ProgressTone } from "@/components/ui";
import { Core, formatCallError, isMyGoRuntime } from "@/lib/ipc";
import { showToast, type DownloadTaskDTO } from "@/store";

export interface DownloadCategoryMeta {
  value: 0 | 1 | 2 | 3;
  label: string;
  icon: string;
  emptyTitle: string;
  emptySubtitle: string;
}

/**
 * Matches Go backend `dlFilter` constants (`internal/ui/download_tasks.go`):
 * 0 = 正在下载 (dlFilterOpen), 1 = 已完成 (dlFilterDone),
 * 2 = 回收站 (dlFilterCanceled), 3 = 全部任务 (dlFilterAll).
 */
export const DOWNLOAD_CATEGORIES: readonly DownloadCategoryMeta[] = [
  {
    value: 0,
    label: "正在下载",
    icon: "download",
    emptyTitle: "暂无正在下载的任务",
    emptySubtitle: "点击左上角「新建任务」粘贴链接，或导入 CSV / XLSX 表格开始批量下载",
  },
  {
    value: 1,
    label: "已完成",
    icon: "check-circle-2",
    emptyTitle: "暂无已完成的任务",
    emptySubtitle: "下载完成的图片和网页任务会归档在这里，支持一键打开或定位文件",
  },
  {
    value: 2,
    label: "回收站",
    icon: "trash-2",
    emptyTitle: "回收站为空",
    emptySubtitle: "取消的任务会暂存在回收站，可随时重试恢复或一键清空",
  },
  {
    value: 3,
    label: "全部任务",
    icon: "layers",
    emptyTitle: "暂无下载记录",
    emptySubtitle: "支持图片直链、网页抓图与指纹浏览器登录抓取",
  },
] as const;

export type DownloadListEntry =
  | {
      kind: "group";
      key: string;
      site: string;
      total: number;
      active: number;
      done: number;
      failed: number;
    }
  | {
      kind: "task";
      key: number;
      task: DownloadTaskDTO;
    };

export interface UrlLineStats {
  total: number;
  valid: number;
  uniqueValid: number;
  duplicates: number;
  invalid: number;
  cleanLines: string[];
}

export interface DownloadFilterValues {
  depth: number;
  minKb: number;
  maxKb: number;
  minW: number;
  maxW: number;
  minH: number;
  maxH: number;
}

export function isValidHttpUrl(raw: string): boolean {
  const trimmed = raw.trim();
  if (!trimmed) return false;
  try {
    const u = new URL(trimmed);
    return (u.protocol === "http:" || u.protocol === "https:") && Boolean(u.hostname);
  } catch {
    return false;
  }
}

export function hasValidTaskPath(task: DownloadTaskDTO): boolean {
  return Boolean(task.path && task.path.trim().length > 0);
}

export function classifyDroppedSheetPaths(paths: string[]): {
  supported: string[];
  legacyXls: string[];
  other: string[];
} {
  const supported: string[] = [];
  const legacyXls: string[] = [];
  const other: string[] = [];
  for (const raw of paths) {
    const p = raw.trim();
    if (!p) continue;
    if (/\.(xlsx|csv)$/i.test(p)) {
      supported.push(p);
    } else if (/\.xls$/i.test(p)) {
      legacyXls.push(p);
    } else {
      other.push(p);
    }
  }
  return { supported, legacyXls, other };
}

export function validateDownloadFilters(f: DownloadFilterValues): string[] {
  const errs: string[] = [];
  if (!Number.isFinite(f.depth) || f.depth < 1) {
    errs.push("抓取深度至少为 1");
  }
  if (f.minKb < 0 || f.maxKb < 0 || f.minW < 0 || f.maxW < 0 || f.minH < 0 || f.maxH < 0) {
    errs.push("过滤阈值不能为负数");
  }
  if (f.maxKb > 0 && f.minKb > f.maxKb) {
    errs.push("最小体积 (KB) 不能大于最大体积 (KB)");
  }
  if (f.maxW > 0 && f.minW > f.maxW) {
    errs.push("最小宽度不能大于最大宽度");
  }
  if (f.maxH > 0 && f.minH > f.maxH) {
    errs.push("最小高度不能大于最大高度");
  }
  return errs;
}

export function getInvalidFilterFields(f: DownloadFilterValues): Set<string> {
  const bad = new Set<string>();
  if (!Number.isFinite(f.depth) || f.depth < 1) bad.add("depth");
  if (f.minKb < 0) bad.add("minKB");
  if (f.maxKb < 0) bad.add("maxKB");
  if (f.minW < 0) bad.add("minW");
  if (f.maxW < 0) bad.add("maxW");
  if (f.minH < 0) bad.add("minH");
  if (f.maxH < 0) bad.add("maxH");
  if (f.maxKb > 0 && f.minKb > f.maxKb) {
    bad.add("minKB");
    bad.add("maxKB");
  }
  if (f.maxW > 0 && f.minW > f.maxW) {
    bad.add("minW");
    bad.add("maxW");
  }
  if (f.maxH > 0 && f.minH > f.maxH) {
    bad.add("minH");
    bad.add("maxH");
  }
  return bad;
}

export function countActiveFilterRules(f: DownloadFilterValues): number {
  let count = 0;
  if ((f.minKb ?? 0) > 0) count += 1;
  if ((f.maxKb ?? 0) > 0) count += 1;
  if ((f.minW ?? 0) > 0) count += 1;
  if ((f.maxW ?? 0) > 0) count += 1;
  if ((f.minH ?? 0) > 0) count += 1;
  if ((f.maxH ?? 0) > 0) count += 1;
  return count;
}

export function runIpc(fn: () => unknown): void {
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

export function copyDownloadText(text: string, label = "已复制到剪贴板"): void {
  if (!text) return;
  if (isMyGoRuntime()) {
    runIpc(() => Core.copyText(text));
    showToast(label, "success");
    return;
  }
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    void navigator.clipboard.writeText(text).then(
      () => showToast(label, "success"),
      () => showToast("复制失败，请手动选中复制", "error"),
    );
  }
}

export function isTaskActive(task: DownloadTaskDTO): boolean {
  return task.phase === "下载中" || task.phase === "进行中";
}

export function isTaskQueued(task: DownloadTaskDTO): boolean {
  return task.phase === "等待中" || task.phase === "等待" || task.phase === "排队中";
}

export function isTaskPaused(task: DownloadTaskDTO): boolean {
  return task.phase === "已暂停";
}

export function isTaskFailed(task: DownloadTaskDTO): boolean {
  return task.phase === "失败";
}

export function isTaskDone(task: DownloadTaskDTO): boolean {
  return task.phase === "已完成";
}

export function isTaskCanceled(task: DownloadTaskDTO): boolean {
  return task.phase === "已取消";
}

export function resolvedTaskTone(task: DownloadTaskDTO): string {
  if (isTaskActive(task)) return "accent";
  if (isTaskPaused(task)) return "warn";
  if (task.tone === "transfer") return "accent";
  return task.tone || "neutral";
}

export function taskProgressTone(task: DownloadTaskDTO): ProgressTone {
  if (isTaskActive(task)) return "accent";
  if (isTaskPaused(task)) return "warn";
  switch (task.tone) {
    case "ok":
    case "success":
    case "ready":
      return "success";
    case "warn":
    case "warning":
      return "warn";
    case "danger":
    case "error":
    case "bad":
    case "failed":
      return "danger";
    default:
      return "accent";
  }
}

export function taskFraction(task: DownloadTaskDTO): number {
  if (isTaskDone(task)) return 1;
  return Math.min(1, Math.max(0, Number.isFinite(task.fraction) ? task.fraction : 0));
}

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  if (i === 0 || v >= 100) return `${Math.round(v)} ${units[i]}`;
  if (v >= 10) return `${v.toFixed(1)} ${units[i]}`;
  return `${v.toFixed(2)} ${units[i]}`;
}

export function formatSpeed(bytesPerSec: number, empty = "—"): string {
  if (!Number.isFinite(bytesPerSec) || bytesPerSec <= 0) return empty;
  return `${formatBytes(bytesPerSec)}/s`;
}

export function formatETA(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 1) return "即将完成";
  if (seconds < 60) return `${Math.floor(seconds)}秒`;
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    return s > 0 ? `${m}分${s}秒` : `${m}分钟`;
  }
  if (seconds < 86400) {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    return m > 0 ? `${h}小时${m}分` : `${h}小时`;
  }
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  return h > 0 ? `${d}天${h}小时` : `${d}天`;
}

export function computeTaskETA(task: DownloadTaskDTO): string {
  if (!isTaskActive(task)) return "";
  if (task.speed <= 0 || task.total <= task.written) return "";
  return formatETA((task.total - task.written) / task.speed);
}

export function formatTaskSize(task: DownloadTaskDTO): string {
  if (task.progressText) return task.progressText;
  if (task.isPage) {
    if (task.imagesTotal > 0) return `${task.imagesDone}/${task.imagesTotal} 张`;
    if (task.imagesDone > 0) return `${task.imagesDone} 张`;
    if (isTaskActive(task)) return "页面解析中";
    if (isTaskQueued(task)) return "等待解析";
    if (isTaskPaused(task)) return "已暂停";
    if (isTaskFailed(task)) return "未获取到图片";
    if (isTaskCanceled(task)) return "已取消";
    return "0 张";
  }
  if (task.total > 0) return `${formatBytes(task.written)} / ${formatBytes(task.total)}`;
  if (task.written > 0) return formatBytes(task.written);
  return "—";
}

export function countUrlLines(text: string): UrlLineStats {
  const lines = text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter(Boolean);
  const seen = new Set<string>();
  const cleanLines: string[] = [];
  let valid = 0;
  let duplicates = 0;
  let invalid = 0;
  for (const line of lines) {
    if (isValidHttpUrl(line)) {
      valid += 1;
      if (seen.has(line)) {
        duplicates += 1;
      } else {
        seen.add(line);
        cleanLines.push(line);
      }
    } else {
      invalid += 1;
    }
  }
  return {
    total: lines.length,
    valid,
    uniqueValid: cleanLines.length,
    duplicates,
    invalid,
    cleanLines,
  };
}

export function flattenSiteGroups(tasks: DownloadTaskDTO[]): DownloadListEntry[] {
  const map = new Map<string, DownloadTaskDTO[]>();
  for (const task of tasks) {
    const site = task.site?.trim() || "其他来源";
    const list = map.get(site);
    if (list) {
      list.push(task);
    } else {
      map.set(site, [task]);
    }
  }
  const sortedSites = Array.from(map.keys()).sort((a, b) =>
    a.toLowerCase().localeCompare(b.toLowerCase()),
  );
  const entries: DownloadListEntry[] = [];
  for (const site of sortedSites) {
    const groupTasks = map.get(site) ?? [];
    let active = 0;
    let done = 0;
    let failed = 0;
    for (const t of groupTasks) {
      if (isTaskActive(t)) active += 1;
      if (isTaskDone(t)) done += 1;
      if (isTaskFailed(t)) failed += 1;
    }
    entries.push({
      kind: "group",
      key: `group:${site}`,
      site,
      total: groupTasks.length,
      active,
      done,
      failed,
    });
    for (const task of groupTasks) {
      entries.push({ kind: "task", key: task.id, task });
    }
  }
  return entries;
}
