import { memo } from "react";
import { DotMatrixImageBox } from "@/components/dotmatrix/DotMatrixImageBox";
import type { ResultTileDTO } from "@/store";

export function areResultTilesEqual(a: ResultTileDTO, b: ResultTileDTO): boolean {
  return (
    a === b ||
    (a.key === b.key &&
      a.taskIndex === b.taskIndex &&
      a.title === b.title &&
      a.status === b.status &&
      (a.phase ?? "") === (b.phase ?? "") &&
      a.path === b.path &&
      a.fileUrl === b.fileUrl &&
      a.srcUrl === b.srcUrl &&
      a.platform === b.platform &&
      a.accountId === b.accountId &&
      a.accountName === b.accountName &&
      a.engine === b.engine &&
      a.elapsed === b.elapsed &&
      a.prompt === b.prompt &&
      a.rewritten === b.rewritten &&
      a.reason === b.reason &&
      a.qualityNote === b.qualityNote &&
      a.actionText === b.actionText &&
      a.canReplace === b.canReplace &&
      a.canRewash === b.canRewash &&
      a.rewashCount === b.rewashCount)
  );
}

export const GenerationStageCard = memo(
  function GenerationStageCard({
    tile,
    animPaused = false,
    queued = false,
  }: {
    tile: ResultTileDTO;
    animPaused?: boolean;
    queued?: boolean;
  }) {
    const isAlert = tile.status === "需要人工验证";
    const isError = tile.status === "失败" || tile.status === "需核对";
    const isPaused = Boolean(animPaused && !isAlert && !isError);

    const phase = tile.phase?.trim();
    const statusText = isAlert ? "需要人工验证"
      : isPaused ? "点阵已暂停"
      : queued ? "排队中"
      : phase === "downloading" ? "已出图，正在保存…"
      : phase === "uploading" ? "正在上传原图…"
      : phase === "generating" ? undefined
      : "正在准备账号会话…";

    return (
      <div
        data-testid={`dotmatrix-stage-${tile.taskIndex}`}
        className="relative h-full w-full"
      >
        <DotMatrixImageBox
          status={isError ? "error" : isPaused || isAlert ? "paused" : "loading"}
          statusText={statusText}
          showStatusLine="overlay"
          statusLineSize="sm"
          errorText={tile.reason || "生成失败"}
          cornerRadius={10}
          className="relative h-full w-full"
          boxClassName="h-full w-full"
        />
      </div>
    );
  },
  (prev, next) =>
    prev.queued === next.queued &&
    prev.animPaused === next.animPaused &&
    areResultTilesEqual(prev.tile, next.tile),
);
