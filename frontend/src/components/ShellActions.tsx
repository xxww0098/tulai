import { Badge } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  getLastWorkbenchTab,
  goTab,
  isWorkbenchTab,
  useShell,
} from "@/store";
import { clearHoverPreview } from "./HoverPreview";

/**
 * Shared right side of utility-page floating toolbars:
 * - Renders the running-task badge when a batch job is active off-workbench.
 * (Strictly omits running badge on Workbench per design contract.)
 */
export function ShellActions({ className }: { className?: string }) {
  const s = useShell();
  const onWorkbench = isWorkbenchTab(s.tab);

  if (onWorkbench || !s.running) {
    return null;
  }

  return (
    <div className={cn("flex shrink-0 items-center gap-2", className)}>
      <button
        type="button"
        data-testid="topbar-running-badge"
        onClick={() => {
          clearHoverPreview();
          void goTab(getLastWorkbenchTab());
        }}
        className="inline-flex cursor-pointer items-center gap-1.5 rounded-full bg-accent-soft px-2.5 py-1 transition-colors duration-150 hover:bg-accent-border/40"
      >
        <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-accent" />
        <Badge
          text={s.progText || `${Math.round(s.progress * 100)}%`}
          variant="accent"
          className="border-0 bg-transparent px-0 py-0"
        />
      </button>
    </div>
  );
}
