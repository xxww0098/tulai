import { Upload } from "lucide-react";
import { cn } from "@/lib/cn";

export interface DropZoneOverlayProps {
  active: boolean;
  label?: string;
  sublabel?: string;
  className?: string;
}

export function DropZoneOverlay({
  active,
  label = "松开以导入文件",
  sublabel = "支持拖入图片或文件夹",
  className,
}: DropZoneOverlayProps) {
  if (!active) return null;

  return (
    <div
      data-testid="file-drop-overlay"
      className={cn(
        "pointer-events-none absolute inset-0 z-40 flex items-center justify-center rounded-[16px] bg-paper/80 p-4 backdrop-blur-[2px] animate-tulai-fade",
        className,
      )}
    >
      <div className="flex flex-col items-center justify-center gap-2 rounded-[16px] border-2 border-dashed border-accent bg-card/95 px-8 py-6 text-center float-shadow">
        <div className="flex size-11 items-center justify-center rounded-full bg-accent-soft text-accent">
          <Upload size={20} />
        </div>
        <div className="text-[13.5px] font-semibold text-ink">{label}</div>
        {sublabel ? <div className="text-xs text-muted">{sublabel}</div> : null}
      </div>
    </div>
  );
}
