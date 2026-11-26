import { formatBytes } from "@/lib/browserRuntime";
import type { ProgressTone } from "@/components/ui/Progress";
import type { UpdateStateDTO } from "@/store/types";

export type UpdateProgressPhase =
  | ""
  | "checksums"
  | "probing"
  | "downloading"
  | "verifying"
  | "installing"
  | "deferred";

export interface UpdateProgressView {
  active: boolean;
  phase: UpdateProgressPhase;
  indeterminate: boolean;
  fraction: number;
  percentLabel: string;
  compactPercent: string;
  stageLabel: string;
  hint: string;
  transferText: string;
  speedText: string;
  etaText: string;
  source: string;
  stalled: boolean;
  cancellable: boolean;
  shimmerActive: boolean;
  tone: ProgressTone;
}

export function formatUpdateDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "";
  const total = Math.max(1, Math.round(seconds));
  if (total < 60) return `${total} 秒`;
  const mins = Math.floor(total / 60);
  const secs = total % 60;
  if (mins < 60) {
    return secs === 0 ? `${mins} 分` : `${mins} 分 ${secs} 秒`;
  }
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  return remMins === 0 ? `${hours} 小时` : `${hours} 小时 ${remMins} 分`;
}

function resolvePhase(upd: UpdateStateDTO): UpdateProgressPhase {
  if (upd.deferred && !upd.downloading) return "deferred";
  switch (upd.phase) {
    case "checksums":
    case "probing":
    case "downloading":
    case "verifying":
    case "installing":
    case "deferred":
      return upd.phase;
    default:
      if (upd.downloading) return "downloading";
      if (upd.deferred) return "deferred";
      return "";
  }
}

export function describeUpdateProgress(upd: UpdateStateDTO): UpdateProgressView {
  const active = Boolean(upd.downloading || upd.deferred);
  const phase = resolvePhase(upd);
  const verSuffix = upd.version ? ` v${upd.version}` : "";

  const hasDeterminateBytes = upd.totalBytes > 0 && upd.doneBytes >= 0;
  const rawFrac =
    phase === "verifying" || phase === "installing"
      ? 1
      : upd.fraction >= 0 && Number.isFinite(upd.fraction)
        ? Math.min(1, Math.max(0, upd.fraction))
        : hasDeterminateBytes
          ? Math.min(1, Math.max(0, upd.doneBytes / upd.totalBytes))
          : -1;

  const indeterminate =
    phase === "checksums" ||
    phase === "probing" ||
    phase === "deferred" ||
    (phase === "downloading" && (upd.fraction < 0 || (upd.totalBytes <= 0 && rawFrac <= 0)));

  const fraction = indeterminate ? 0 : Math.min(1, Math.max(0, rawFrac < 0 ? 0 : rawFrac));
  const pct = fraction * 100;

  let percentLabel = "";
  let compactPercent = "";
  if (phase === "checksums") {
    percentLabel = "获取校验";
    compactPercent = "校验";
  } else if (phase === "probing") {
    percentLabel = "测速中";
    compactPercent = "测速";
  } else if (phase === "verifying") {
    percentLabel = "100%";
    compactPercent = "校验";
  } else if (phase === "installing") {
    percentLabel = "100%";
    compactPercent = "安装";
  } else if (phase === "deferred") {
    percentLabel = "已排队";
    compactPercent = "排队";
  } else if (!indeterminate) {
    if (pct <= 0 || pct >= 100) {
      percentLabel = `${Math.round(pct)}%`;
    } else {
      percentLabel = `${pct.toFixed(1)}%`;
    }
    compactPercent = `${Math.round(pct)}%`;
  } else {
    percentLabel = "下载中";
    compactPercent = "下载";
  }

  let stageLabel = `正在下载${verSuffix}`;
  let hint = upd.assetName || upd.progressText || "正在下载更新包...";
  let tone: ProgressTone = "accent";

  switch (phase) {
    case "checksums":
      stageLabel = "获取校验清单";
      hint = "正在获取官方 SHA256SUMS 校验清单...";
      break;
    case "probing":
      stageLabel = "测速选择下载源";
      hint = "正在并行测速多个加速镜像与直连线路...";
      break;
    case "verifying":
      stageLabel = "校验安装包完整性";
      hint = upd.assetName
        ? `正在核对 ${upd.assetName} 的 SHA-256 摘要...`
        : "下载已完成，正在核对 SHA-256 摘要...";
      tone = "success";
      break;
    case "installing":
      stageLabel = `正在准备安装${verSuffix}`;
      hint = "完整性校验通过，即将启动新版本安装...";
      tone = "success";
      break;
    case "deferred":
      stageLabel = `等待当前任务结束`;
      hint = upd.progressText || "任务进行中，结束后将自动更新";
      tone = "warn";
      break;
    case "downloading":
      stageLabel = `正在下载${verSuffix || "更新包"}`;
      hint = upd.assetName || upd.progressText || "分段并发下载中...";
      break;
  }

  let transferText = "";
  if (phase === "downloading" || phase === "verifying") {
    if (upd.doneBytes > 0 && upd.totalBytes > 0) {
      transferText = `${formatBytes(upd.doneBytes)} / ${formatBytes(upd.totalBytes)}`;
    } else if (upd.doneBytes > 0) {
      transferText = formatBytes(upd.doneBytes);
    }
  }

  const speedText =
    phase === "downloading" && upd.speedBps > 0 ? `${formatBytes(upd.speedBps)}/s` : "";
  const etaText =
    phase === "downloading" && upd.etaSeconds > 0
      ? `剩余 ${formatUpdateDuration(upd.etaSeconds)}`
      : "";
  const stalled =
    phase === "downloading" &&
    upd.doneBytes > 0 &&
    (upd.totalBytes <= 0 || upd.doneBytes < upd.totalBytes) &&
    upd.speedBps <= 0;
  const cancellable =
    phase === "checksums" ||
    phase === "probing" ||
    phase === "downloading" ||
    phase === "deferred";
  const shimmerActive =
    phase === "downloading" && !indeterminate && !stalled && fraction > 0 && fraction < 1;

  return {
    active,
    phase,
    indeterminate,
    fraction,
    percentLabel,
    compactPercent,
    stageLabel,
    hint,
    transferText,
    speedText,
    etaText,
    source: upd.source || "",
    stalled,
    cancellable,
    shimmerActive,
    tone,
  };
}
