import { PageSkeleton } from "@/components/primitives";
import { RenameFooterBar } from "@/components/rename/RenameFooterBar";
import { RenameModeTabs } from "@/components/rename/RenameModeTabs";
import { RenamePreviewTable } from "@/components/rename/RenamePreviewTable";
import { RenameRulePanels } from "@/components/rename/RenameRulePanels";
import { EMPTY_RENAME_STATE, runIpc } from "@/components/rename/renameHelpers";
import { DropZoneOverlay } from "@/components/ui";
import { Rename, useFileDrop } from "@/lib/ipc";
import { showToast, usePageLoading, useRename } from "@/store";

export function RenamePage() {
  const rawState = useRename();
  const loading = usePageLoading("rename");
  const st = rawState ?? EMPTY_RENAME_STATE;

  const { isDragging } = useFileDrop({
    enabled: !Boolean(st.busy),
    onDrop: (paths) => {
      if (paths.length > 0) {
        runIpc(() => Rename.renameIngestPaths(paths));
      }
    },
  });

  if (!rawState && loading) {
    return <PageSkeleton rows={6} />;
  }

  const rows = st.rows ?? [];
  const inlineEditable = st.mode === 1 || st.mode === 4;

  const handleCopyOriginalNames = () => {
    if (rows.length === 0) return;
    const text = rows.map((r) => r.oldName).join("\n");
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      void navigator.clipboard
        .writeText(text)
        .then(() => showToast(`已复制 ${rows.length} 个原文件名`, "success"))
        .catch(() => showToast("复制失败，请检查剪贴板权限", "error"));
    }
  };

  const handleContainerPaste = (e: React.ClipboardEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement | null;
    if (
      target &&
      (target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable)
    ) {
      return;
    }
    const items = e.clipboardData?.files;
    if (items && items.length > 0) {
      const paths = Array.from(items)
        .map((f) => (f as File & { path?: string }).path)
        .filter((p): p is string => Boolean(p));
      if (paths.length > 0) {
        e.preventDefault();
        runIpc(() => Rename.renameIngestPaths(paths));
        return;
      }
    }
    const text = e.clipboardData?.getData("text/plain")?.trim() ?? "";
    if (!text) return;
    const lines = text
      .split(/\r?\n/)
      .map((l) => l.trim().replace(/^["']|["']$/g, ""))
      .filter((l) => /^(\/|[A-Za-z]:[\\/]|\\\\)/.test(l));
    if (lines.length > 0) {
      e.preventDefault();
      runIpc(() => Rename.renameIngestPaths(lines));
    }
  };

  return (
    <div
      onPaste={handleContainerPaste}
      className="relative flex h-full min-h-0 flex-1 flex-col bg-paper px-3 pt-2.5 pb-3"
    >
      <DropZoneOverlay
        active={isDragging}
        label="松开以导入待重命名文件"
        sublabel="支持拖入多个文件或文件夹"
      />

      <div className="bento-shadow flex min-h-0 flex-1 flex-col overflow-hidden rounded-[16px] border border-hairline bg-card">
        <RenameModeTabs mode={st.mode} busy={Boolean(st.busy)} />
        <RenameRulePanels st={st} onCopyOriginalNames={handleCopyOriginalNames} />
        <RenamePreviewTable st={st} inlineEditable={inlineEditable} />
        <RenameFooterBar st={st} />
      </div>
    </div>
  );
}
