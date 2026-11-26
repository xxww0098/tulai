import { Loader2 } from "lucide-react";
import { DropdownMenu, Icon, IconButton, Tooltip, TooltipGroup } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Rename } from "@/lib/ipc";
import type { RenameStateDTO } from "@/store";
import { SORT_OPTIONS, runIpc } from "./renameHelpers";

function AddFolderIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z" />
      <path d="M12 10v6" />
      <path d="m9 13 3-3 3 3" />
    </svg>
  );
}

function AddFileFilledIcon() {
  return (
    <svg width="17" height="17" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M6 2a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6H6zm7 1.5L18.5 9H13V3.5zM11 12h2v3h3v2h-3v3h-2v-3H8v-2h3v-3z" />
    </svg>
  );
}

function ClearSweepIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" aria-hidden="true">
      <path d="M4 7h16" />
      <path d="M5 12h11" />
      <path d="M4 17h7" />
    </svg>
  );
}

export function RenameFooterBar({ st }: { st: RenameStateDTO }) {
  const rows = st.rows ?? [];
  const busy = Boolean(st.busy);
  const sortValue = SORT_OPTIONS.some((o) => o.value === st.sortBy)
    ? st.sortBy
    : "名称升序";
  const applyDisabled = st.changedCount === 0 || busy;

  return (
    <footer className="flex h-[48px] shrink-0 items-center justify-between gap-2 border-t border-[#E5E7EB] bg-card px-3 dark:border-divider">
      <TooltipGroup className="flex min-w-0 items-center gap-1">
        <DropdownMenu
          align="start"
          side="top"
          trigger={
            <Tooltip content="添加文件夹" side="top">
              <button
                type="button"
                disabled={busy}
                aria-label="添加文件夹"
                className="flex size-8 cursor-pointer items-center justify-center rounded-[6px] text-[#5F5F5F] transition-colors hover:bg-sunken hover:text-ink disabled:pointer-events-none disabled:opacity-45 dark:text-muted"
              >
                <AddFolderIcon />
              </button>
            </Tooltip>
          }
          items={[
            {
              id: "pick-folder-files",
              label: "导入文件夹内全部文件",
              icon: "folder-tree",
              onSelect: () => runIpc(() => Rename.renamePickFolder(true)),
            },
            {
              id: "pick-folder-self",
              label: "添加文件夹本身",
              icon: "folder",
              onSelect: () => runIpc(() => Rename.renamePickFolder(false)),
            },
          ]}
        />

        <span aria-hidden="true" className="mx-1.5 h-4 w-px bg-[#E0E0E0] dark:bg-divider" />

        <Tooltip content="添加文件" side="top">
          <button
            type="button"
            disabled={busy}
            aria-label="添加文件"
            onClick={() => runIpc(() => Rename.renamePickFiles())}
            className="flex size-8 cursor-pointer items-center justify-center rounded-[6px] text-[#5F5F5F] transition-colors hover:bg-sunken hover:text-ink disabled:pointer-events-none disabled:opacity-45 dark:text-muted"
          >
            <AddFileFilledIcon />
          </button>
        </Tooltip>

        <span aria-hidden="true" className="mx-1.5 h-4 w-px bg-[#E0E0E0] dark:bg-divider" />

        <Tooltip content="清空文件列表" side="top">
          <button
            type="button"
            disabled={busy || rows.length === 0}
            aria-label="清空列表"
            onClick={() => runIpc(() => Rename.renameClear())}
            className="flex size-8 cursor-pointer items-center justify-center rounded-[6px] text-[#5F5F5F] transition-colors hover:bg-sunken hover:text-ink disabled:pointer-events-none disabled:opacity-45 dark:text-muted"
          >
            <ClearSweepIcon />
          </button>
        </Tooltip>

        {st.canUndo ? (
          <Tooltip content="撤销上次重命名" side="top">
            <div className="relative inline-flex">
              <IconButton
                icon="undo-2"
                variant="ghost"
                size={30}
                disabled={busy}
                aria-label="撤销上次"
                onClick={() => runIpc(() => Rename.renameUndo())}
              />
              <span
                aria-hidden="true"
                className="pointer-events-none absolute top-1 right-1 size-1.5 rounded-full bg-accent"
              />
            </div>
          </Tooltip>
        ) : null}

        <span aria-hidden="true" className="mx-1.5 h-4 w-px bg-[#E0E0E0] dark:bg-divider" />

        <DropdownMenu
          align="start"
          side="top"
          trigger={
            <button
              type="button"
              data-testid="rename-sort-select"
              disabled={busy}
              aria-label="排序方式"
              className="inline-flex h-[30px] cursor-pointer items-center gap-1.5 rounded-[4px] border border-dashed border-[#737373] bg-card px-2.5 text-[12.5px] text-[#374151] transition-colors hover:bg-[#F5F5F5] disabled:pointer-events-none disabled:opacity-45 dark:border-strong dark:text-ink dark:hover:bg-sunken"
            >
              <Icon name="arrow-up-down" size={13} className="text-[#595959] dark:text-subtle" />
              <span>排序 · {sortValue}</span>
            </button>
          }
          items={SORT_OPTIONS.map((opt) => ({
            id: `sort-${opt.value}`,
            label: opt.label,
            icon: sortValue === opt.value ? "check" : undefined,
            onSelect: () => runIpc(() => Rename.renameSetField("sortBy", opt.value)),
          }))}
        />

        <span className="tnum ml-2.5 truncate text-[12.5px] text-[#4B5563] select-none dark:text-muted">
          共 {rows.length} 个文件
          {st.changedCount > 0 ? (
            <span className="ml-1.5 font-medium text-accent">
              · 将重命名 {st.changedCount} 个
            </span>
          ) : null}
        </span>
      </TooltipGroup>

      <button
        type="button"
        data-testid="rename-apply-btn"
        disabled={applyDisabled}
        onClick={() => runIpc(() => Rename.renameApply())}
        className={cn(
          "tnum inline-flex h-[34px] shrink-0 items-center gap-2 rounded-[5px] px-4 text-[13px] font-medium transition-colors select-none",
          applyDisabled
            ? "cursor-not-allowed bg-[#D4D4D4] text-[#8C8C8C] dark:bg-sunken dark:text-subtle"
            : "cursor-pointer bg-accent text-on-accent hover:bg-accent-hover active:bg-accent-pressed shadow-[0_1px_2px_rgba(37,99,235,0.18)] dark:shadow-none",
        )}
      >
        {busy ? (
          <Loader2 size={15} className="shrink-0 animate-spin" aria-hidden="true" />
        ) : (
          <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" className="shrink-0">
            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 14.5v-9l6 4.5-6 4.5z" />
          </svg>
        )}
        <span>
          {busy
            ? "执行中..."
            : st.changedCount > 0
              ? `执行重命名 (${st.changedCount})`
              : "执行重命名"}
        </span>
      </button>
    </footer>
  );
}
