import { Icon, IconButton, Tooltip, VirtualList } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Rename } from "@/lib/ipc";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import type { RenameRowDTO, RenameStateDTO } from "@/store";
import { runIpc } from "./renameHelpers";

function RenamePreviewRow({
  row,
  totalRows,
  editable,
  busy,
}: {
  row: RenameRowDTO;
  totalRows: number;
  editable: boolean;
  busy: boolean;
}) {
  const draft = useSyncedDraft(
    row.draft || "",
    `rn-row-${row.path || row.index}`,
    (next) => runIpc(() => Rename.renameSetDraft(row.index, next)),
    { debounceMs: 90 },
  );

  return (
    <div
      data-testid={`rename-row-${row.index}`}
      className="group flex h-[38px] items-center gap-3 px-4 transition-colors hover:bg-sunken/45"
    >
      {/* Left column: Status icon + Original filename */}
      <div className="flex min-w-0 flex-1 items-center gap-2 pr-1">
        <Icon
          name={row.changed ? "check" : "file-text"}
          size={13}
          className={cn(
            "shrink-0",
            row.changed ? "text-status-ok" : "text-subtle",
          )}
        />
        <Tooltip content={row.oldName} side="top">
          <span
            tabIndex={0}
            className="min-w-0 flex-1 truncate rounded text-xs text-ink select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]"
          >
            {row.oldName}
          </span>
        </Tooltip>
      </div>

      {/* Right column: Change dot + Target filename (inline-editable or preview) */}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <span
          aria-hidden="true"
          className={cn(
            "size-1.5 shrink-0 rounded-full transition-[background-color,transform] duration-150",
            row.changed ? "scale-100 bg-[#EA580C] dark:bg-[#FB923C]" : "scale-0 bg-transparent",
          )}
        />

        {editable ? (
          <input
            data-testid={`rename-draft-${row.index}`}
            type="text"
            disabled={busy}
            value={draft.value}
            aria-label={`第 ${row.index + 1} 行新文件名`}
            onChange={(e) => draft.onChange(e.target.value)}
            onCompositionStart={draft.onCompositionStart}
            onCompositionEnd={(e) => draft.onCompositionEnd(e.currentTarget.value)}
            onBlur={draft.onBlur}
            onPaste={(e) => {
              const raw = e.clipboardData?.getData("text/plain") ?? "";
              const normalized = raw
                .replace(/\r\n/g, "\n")
                .replace(/\r/g, "\n")
                .replace(/\n$/, "");
              if (normalized.includes("\n")) {
                e.preventDefault();
                draft.flush();
                runIpc(() => Rename.renamePasteLines(row.index, raw));
              }
            }}
            className={cn(
              "h-7 min-w-0 flex-1 border-0 border-b border-hairline bg-transparent px-1 text-xs transition-colors",
              "focus:border-accent focus:outline-none disabled:cursor-not-allowed disabled:opacity-50",
              row.changed
                ? "font-medium text-[#C2410C] dark:text-[#FB923C]"
                : "text-muted",
            )}
          />
        ) : (
          <Tooltip content={row.newName || row.oldName} side="top">
            <div
              tabIndex={0}
              className={cn(
                "flex h-7 min-w-0 flex-1 items-center rounded border-b border-divider/80 px-1 text-xs select-text focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--focus-ring)]",
                row.changed
                  ? "font-medium text-[#C2410C] dark:text-[#FB923C]"
                  : "text-subtle",
              )}
            >
              <span className="truncate">{row.newName || row.oldName}</span>
            </div>
          </Tooltip>
        )}
      </div>

      {/* Far right: Hover move controls + permanent remove button */}
      <div className="flex shrink-0 items-center gap-0.5">
        <IconButton
          icon="arrow-up"
          variant="ghost"
          size={22}
          disabled={busy || row.index <= 0}
          aria-label="上移"
          onClick={() => {
            if (row.index > 0) runIpc(() => Rename.renameMoveRow(row.index, row.index - 1));
          }}
          className="touch-visible opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        />
        <IconButton
          icon="arrow-down"
          variant="ghost"
          size={22}
          disabled={busy || row.index >= totalRows - 1}
          aria-label="下移"
          onClick={() => {
            if (row.index < totalRows - 1) {
              runIpc(() => Rename.renameMoveRow(row.index, row.index + 1));
            }
          }}
          className="touch-visible opacity-0 transition-opacity group-hover:opacity-100 focus-visible:opacity-100"
        />
        <IconButton
          icon="x"
          variant="ghost"
          size={22}
          disabled={busy}
          aria-label="移除"
          onClick={() => runIpc(() => Rename.renameRemoveAt(row.index))}
          className="text-subtle hover:text-ink"
        />
      </div>
    </div>
  );
}

export function RenamePreviewTable({
  st,
  inlineEditable,
}: {
  st: RenameStateDTO;
  inlineEditable: boolean;
}) {
  const rows = st.rows ?? [];
  const busy = Boolean(st.busy);
  const bannerText = st.err || st.planErr || st.note || "";
  const isBannerError = Boolean(st.err || st.planErr);

  return (
    <section
      data-testid="rename-preview"
      className={cn(
        "flex min-h-0 flex-1 flex-col",
        st.mode !== 1 && "border-t border-[#E5E7EB] dark:border-divider",
        rows.length === 0 ? "bg-[#F5F5F5] dark:bg-paper/60" : "bg-card",
      )}
    >
      {bannerText ? (
        <div
          data-testid="rename-banner"
          className={cn(
            "animate-tulai-banner-in flex shrink-0 items-center gap-2 border-b px-4 py-1.5 text-xs select-text",
            isBannerError
              ? "border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-status-danger"
              : "border-divider bg-sunken/70 text-muted",
          )}
        >
          <Icon
            name={isBannerError ? "alert-circle" : "info"}
            size={13}
            className="shrink-0"
          />
          <span className="min-w-0 flex-1 truncate">{bannerText}</span>
        </div>
      ) : null}

      {rows.length === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center select-none">
          <svg
            width="36"
            height="44"
            viewBox="0 0 36 44"
            fill="none"
            aria-hidden="true"
            className="text-[#CBCBCB] dark:text-subtle/40"
          >
            <path
              d="M4 0h19l13 13v27a4 4 0 0 1-4 4H4a4 4 0 0 1-4-4V4a4 4 0 0 1 4-4z"
              fill="currentColor"
            />
            <path d="M23 0l13 13H27a4 4 0 0 1-4-4V0z" fill="currentColor" opacity="0.65" />
          </svg>
          <div className="text-[12.5px] text-[#9E9E9E] dark:text-subtle">
            拖入或粘贴文件 / 文件夹到此处
          </div>
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="flex h-7 shrink-0 items-center gap-3 border-b border-divider bg-card-header px-4 text-[11px] font-medium text-subtle">
            <span className="min-w-0 flex-1 pl-5">原文件名</span>
            <span className="min-w-0 flex-1 pl-3.5">新文件名预览</span>
            <span className="w-[70px] shrink-0 text-right">操作</span>
          </div>

          <VirtualList
            data-testid="rename-row-list"
            items={rows}
            estimateSize={38}
            className="min-h-0 flex-1 py-1"
            getItemKey={(r) => r.path || String(r.index)}
            renderItem={(row) => (
              <RenamePreviewRow
                row={row}
                totalRows={rows.length}
                editable={inlineEditable}
                busy={busy}
              />
            )}
          />
        </div>
      )}
    </section>
  );
}
