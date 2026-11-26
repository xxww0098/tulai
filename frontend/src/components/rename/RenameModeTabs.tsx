import { ShellActions } from "@/components/ShellActions";
import { Icon } from "@/components/ui";
import { cn } from "@/lib/cn";
import { Rename } from "@/lib/ipc";
import { RENAME_MODES, runIpc } from "./renameHelpers";

function ModeTabIcon({ icon, active }: { icon: string; active: boolean }) {
  if (icon === "replace-loop") {
    return (
      <svg
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={active ? 2.1 : 1.8}
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        className="shrink-0"
      >
        <path d="M21 12a9 9 0 0 0-9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
        <path d="M3 3v5h5" />
        <path d="M3 12a9 9 0 0 0 9 9 9.75 9.75 0 0 0 6.74-2.74L21 16" />
        <path d="M16 16h5v5" />
        <path d="M9.5 10.5h5" />
        <path d="M9.5 13.5h5" />
      </svg>
    );
  }

  if (icon === "insert-square") {
    return active ? (
      <svg
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill="currentColor"
        aria-hidden="true"
        className="shrink-0"
      >
        <path d="M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zm6 5v3H8v2h3v3h2v-3h3v-2h-3V8h-2z" />
      </svg>
    ) : (
      <svg
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.9"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        className="shrink-0"
      >
        <rect x="3" y="3" width="18" height="18" rx="3" />
        <path d="M12 8v8" />
        <path d="M8 12h8" />
      </svg>
    );
  }

  return (
    <Icon
      name={icon}
      size={14.5}
      strokeWidth={active ? 2.1 : 1.8}
      className={active ? "text-[#111827] dark:text-ink" : "text-[#4B5563] dark:text-muted"}
    />
  );
}

export function RenameModeTabs({
  mode,
  busy,
}: {
  mode: number;
  busy: boolean;
}) {
  return (
    <header
      className="app-drag-region flex h-[40px] shrink-0 items-stretch justify-between bg-[#F4F5F6] select-none dark:bg-rail"
      style={{ paddingRight: "calc(8px + var(--mygo-titlebar-inset-right, 0px))" }}
    >
      <div
        role="tablist"
        aria-label="重命名模式"
        className="app-no-drag flex min-w-0 flex-1 items-stretch overflow-x-auto"
      >
        {RENAME_MODES.map((opt) => {
          const active = mode === opt.value;
          return (
            <button
              key={opt.value}
              type="button"
              role="tab"
              data-testid={`rename-mode-${opt.value}`}
              aria-selected={active}
              disabled={busy}
              onClick={() => runIpc(() => Rename.renameSetField("mode", String(opt.value)))}
              className={cn(
                "relative flex min-w-[96px] flex-1 shrink-0 cursor-pointer items-center justify-center gap-2 px-3 text-[13px] whitespace-nowrap transition-[background-color,color] duration-150",
                "focus-visible:z-20 focus-visible:outline-2 focus-visible:outline-offset-[-2px]",
                "disabled:cursor-not-allowed disabled:opacity-50",
                active
                  ? "z-10 bg-card font-semibold text-[#111827] dark:text-ink"
                  : "bg-transparent font-medium text-[#4B5563] hover:bg-black/[0.035] hover:text-[#111827] dark:text-muted dark:hover:bg-white/[0.04] dark:hover:text-ink",
              )}
            >
              <ModeTabIcon icon={opt.icon} active={active} />
              <span>{opt.label}</span>
              {active ? (
                <span
                  aria-hidden="true"
                  className="animate-tulai-fade absolute right-2 bottom-0 left-2 h-[2px] rounded-full bg-accent"
                />
              ) : null}
            </button>
          );
        })}
      </div>

      <ShellActions className="app-no-drag pl-2" />
    </header>
  );
}
