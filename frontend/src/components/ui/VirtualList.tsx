import { useRef, type ReactNode, type RefObject } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/lib/cn";

export interface VirtualListProps<T> {
  items: T[];
  estimateSize: number | ((index: number) => number);
  overscan?: number;
  gap?: number;
  className?: string;
  innerClassName?: string;
  scrollRef?: RefObject<HTMLDivElement | null>;
  getItemKey?: (item: T, index: number) => string | number;
  renderItem: (item: T, index: number) => ReactNode;
  empty?: ReactNode;
  "data-testid"?: string;
}

export function VirtualList<T>({
  items,
  estimateSize,
  overscan = 8,
  gap = 0,
  className,
  innerClassName,
  scrollRef,
  getItemKey,
  renderItem,
  empty,
  "data-testid": testId,
}: VirtualListProps<T>) {
  const internalRef = useRef<HTMLDivElement | null>(null);
  const parentRef = scrollRef ?? internalRef;

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: typeof estimateSize === "function" ? estimateSize : () => estimateSize,
    getItemKey: getItemKey
      ? (index) => getItemKey(items[index], index)
      : undefined,
    overscan,
    gap,
  });

  if (items.length === 0 && empty !== undefined) {
    return (
      <div ref={parentRef} data-testid={testId} className={cn("overflow-y-auto", className)}>
        {empty}
      </div>
    );
  }

  return (
    <div
      ref={parentRef}
      data-testid={testId}
      className={cn("min-h-0 overflow-y-auto", className)}
    >
      <div
        className={cn("relative w-full", innerClassName)}
        style={{ height: `${virtualizer.getTotalSize()}px` }}
      >
        {virtualizer.getVirtualItems().map((vRow) => {
          const item = items[vRow.index];
          return (
            <div
              key={vRow.key}
              data-index={vRow.index}
              ref={virtualizer.measureElement}
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                width: "100%",
                transform: `translateY(${vRow.start}px)`,
              }}
            >
              {renderItem(item, vRow.index)}
            </div>
          );
        })}
      </div>
    </div>
  );
}
