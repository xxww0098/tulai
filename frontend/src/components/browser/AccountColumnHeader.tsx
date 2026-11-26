import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { StatusBadge } from "@/components/primitives";
import { Button, Checkbox, Icon, Input, Tooltip, TooltipGroup } from "@/components/ui";
import { cn } from "@/lib/cn";
import { useEscapeHandler } from "@/lib/shortcuts";
import type { BrowserRowDTO } from "@/store";
import {
  getHealthFilterOptions,
  getPlatformFilterOptions,
  isColumnFilterActive,
  isDefaultAccountSort,
  type AccountColumnKey,
  type AccountFiltersState,
  type AccountSortState,
  type EnabledFilter,
  type ProxyModeFilter,
  type SortDirection,
  type UsagePresetFilter,
} from "./accountTableFilterSort";

export interface AccountColumnHeaderProps {
  colKey: AccountColumnKey;
  label: string;
  className?: string;
  sortState: AccountSortState | null;
  filters: AccountFiltersState;
  allRows: BrowserRowDTO[];
  basePlatforms: string[];
  providerMarks: Record<string, string>;
  openCol: AccountColumnKey | null;
  onOpenColChange: (col: AccountColumnKey | null) => void;
  onCycleSort: (col: AccountColumnKey) => void;
  onSetSort: (col: AccountColumnKey, dir: SortDirection | null) => void;
  onUpdateFilters: (updater: (prev: AccountFiltersState) => AccountFiltersState) => void;
  onResetColumn: (col: AccountColumnKey) => void;
}

const POPOVER_WIDTH = 248;

function OptionRow({
  active,
  label,
  count,
  testId,
  onClick,
}: {
  active: boolean;
  label: string;
  count?: number;
  testId?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      data-testid={testId}
      onClick={onClick}
      className={cn(
        "flex w-full cursor-pointer items-center justify-between gap-2 rounded-[8px] px-2 py-1.5 text-left text-xs transition-colors",
        active
          ? "bg-accent-soft font-semibold text-accent"
          : "text-ink hover:bg-sunken",
      )}
    >
      <span className="flex min-w-0 items-center gap-1.5 truncate">
        <Icon
          name="check"
          size={12}
          className={cn("shrink-0", active ? "text-accent opacity-100" : "opacity-0")}
        />
        <span className="truncate">{label}</span>
      </span>
      {count !== undefined ? (
        <span className="tnum shrink-0 text-[11px] text-muted">{count}</span>
      ) : null}
    </button>
  );
}

export function AccountColumnHeader({
  colKey,
  label,
  className,
  sortState,
  filters,
  allRows,
  basePlatforms,
  providerMarks,
  openCol,
  onOpenColChange,
  onCycleSort,
  onSetSort,
  onUpdateFilters,
  onResetColumn,
}: AccountColumnHeaderProps) {
  const thRef = useRef<HTMLTableCellElement | null>(null);
  const filterBtnRef = useRef<HTMLButtonElement | null>(null);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  const [pos, setPos] = useState<{ top: number; left: number }>({ top: 0, left: 0 });

  const isOpen = openCol === colKey;
  const activeSortDir = sortState?.key === colKey ? sortState.dir : null;
  const canResetColSort = Boolean(activeSortDir) && !isDefaultAccountSort(sortState);
  const filterActive = isColumnFilterActive(filters, colKey);

  const closePopover = () => onOpenColChange(null);

  useEscapeHandler(`browser-col-filter-${colKey}`, isOpen, closePopover, 35);

  useLayoutEffect(() => {
    if (!isOpen) return;
    const anchor = thRef.current ?? filterBtnRef.current;
    if (!anchor) return;
    const updatePosition = () => {
      const rect = anchor.getBoundingClientRect();
      const vw = typeof window !== "undefined" ? window.innerWidth : 1024;
      const vh = typeof window !== "undefined" ? window.innerHeight : 768;
      const clampedLeft = Math.max(8, Math.min(rect.left, vw - POPOVER_WIDTH - 8));
      const estimatedMaxH = 340;
      const top =
        rect.bottom + 6 + estimatedMaxH > vh && rect.top > estimatedMaxH
          ? Math.max(8, rect.top - estimatedMaxH - 6)
          : Math.min(vh - 48, rect.bottom + 4);
      setPos({ top, left: clampedLeft });
    };
    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [isOpen]);

  useEffect(() => {
    if (!isOpen) return;
    const onPointerDown = (e: PointerEvent | MouseEvent) => {
      const target = e.target as Node | null;
      if (!target) return;
      if (popoverRef.current?.contains(target)) return;
      if (filterBtnRef.current?.contains(target)) return;
      onOpenColChange(null);
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [isOpen, onOpenColChange]);

  const ariaSort =
    activeSortDir === "asc"
      ? "ascending"
      : activeSortDir === "desc"
        ? "descending"
        : "none";

  const sortIcon =
    activeSortDir === "asc"
      ? "arrow-up"
      : activeSortDir === "desc"
        ? "arrow-down"
        : "arrow-up-down";

  const renderFilterBody = () => {
    switch (colKey) {
      case "enabled": {
        const enabledCount = allRows.filter((r) => !r.disabled).length;
        const disabledCount = allRows.length - enabledCount;
        const items: { value: EnabledFilter; label: string; count: number }[] = [
          { value: "all", label: "全部状态", count: allRows.length },
          { value: "enabled", label: "仅已启用", count: enabledCount },
          { value: "disabled", label: "仅已禁用", count: disabledCount },
        ];
        return (
          <div className="flex flex-col gap-0.5">
            {items.map((item) => (
              <OptionRow
                key={item.value}
                active={filters.enabled === item.value}
                label={item.label}
                count={item.count}
                testId={`browser-filter-enabled-${item.value}`}
                onClick={() =>
                  onUpdateFilters((prev) => ({ ...prev, enabled: item.value }))
                }
              />
            ))}
          </div>
        );
      }

      case "account":
        return (
          <div className="flex flex-col gap-1.5">
            <div className="relative">
              <Input
                autoFocus
                size="small"
                data-testid="browser-filter-account-input"
                value={filters.accountQuery}
                placeholder="搜索账号名 / 邮箱 / #ID"
                aria-label="搜索账号名、邮箱或编号"
                onChange={(e) => {
                  const val = e.target.value;
                  onUpdateFilters((prev) => ({ ...prev, accountQuery: val }));
                }}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    e.preventDefault();
                    e.stopPropagation();
                    closePopover();
                  } else if (e.key === "Enter" && !e.nativeEvent.isComposing) {
                    e.preventDefault();
                    closePopover();
                  }
                }}
                className="pr-7"
              />
              {filters.accountQuery ? (
                <button
                  type="button"
                  aria-label="清空账号搜索"
                  onClick={() =>
                    onUpdateFilters((prev) => ({ ...prev, accountQuery: "" }))
                  }
                  className="absolute right-1.5 top-1/2 -translate-y-1/2 cursor-pointer rounded p-0.5 text-subtle hover:bg-sunken hover:text-ink"
                >
                  <Icon name="x" size={12} />
                </button>
              ) : null}
            </div>
            <span className="text-[11px] text-subtle">
              输入关键词实时筛选备注名、邮箱或账号序号
            </span>
          </div>
        );

      case "platform": {
        const options = getPlatformFilterOptions(allRows, basePlatforms);
        const togglePlatform = (plat: string, nextChecked: boolean) => {
          onUpdateFilters((prev) => {
            const current =
              prev.platforms.length === 0
                ? options.map((o) => o.value)
                : prev.platforms;
            const nextSet = new Set(current);
            if (nextChecked) nextSet.add(plat);
            else nextSet.delete(plat);
            const allValues = options.map((o) => o.value);
            const normalized =
              nextSet.size === 0 || nextSet.size === allValues.length
                ? []
                : allValues.filter((v) => nextSet.has(v));
            return { ...prev, platforms: normalized };
          });
        };
        return (
          <div className="flex flex-col gap-1">
            {options.map((opt) => {
              const checked =
                filters.platforms.length === 0 ||
                filters.platforms.includes(opt.value);
              const mark = providerMarks[opt.value];
              return (
                <label
                  key={opt.value}
                  data-testid={`browser-filter-platform-${opt.value}`}
                  className="flex cursor-pointer items-center justify-between gap-2 rounded-[8px] px-2 py-1.5 text-xs text-ink transition-colors hover:bg-sunken select-none"
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <Checkbox
                      size="small"
                      checked={checked}
                      onCheckedChange={(v) => togglePlatform(opt.value, v)}
                    />
                    {mark ? (
                      <img
                        src={mark}
                        alt=""
                        aria-hidden="true"
                        className="size-3.5 shrink-0 object-contain"
                        draggable={false}
                      />
                    ) : null}
                    <span className="truncate font-medium">{opt.value}</span>
                  </span>
                  <span className="tnum shrink-0 text-[11px] text-muted">{opt.count}</span>
                </label>
              );
            })}
          </div>
        );
      }

      case "health": {
        const options = getHealthFilterOptions(allRows);
        const toggleHealth = (health: string, nextChecked: boolean) => {
          onUpdateFilters((prev) => {
            const current =
              prev.healths.length === 0
                ? options.map((o) => o.value)
                : prev.healths;
            const nextSet = new Set(current);
            if (nextChecked) nextSet.add(health);
            else nextSet.delete(health);
            const allValues = options.map((o) => o.value);
            const normalized =
              nextSet.size === 0 || nextSet.size === allValues.length
                ? []
                : allValues.filter((v) => nextSet.has(v));
            return { ...prev, healths: normalized };
          });
        };
        return options.length === 0 ? (
          <div className="py-2 text-center text-[11px] text-subtle">暂无状态数据</div>
        ) : (
          <div className="flex max-h-[210px] flex-col gap-1 overflow-y-auto pr-0.5">
            {options.map((opt) => {
              const checked =
                filters.healths.length === 0 ||
                filters.healths.includes(opt.value);
              return (
                <label
                  key={opt.value}
                  data-testid={`browser-filter-health-${opt.value}`}
                  className="flex cursor-pointer items-center justify-between gap-2 rounded-[8px] px-2 py-1 text-xs text-ink transition-colors hover:bg-sunken select-none"
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <Checkbox
                      size="small"
                      checked={checked}
                      onCheckedChange={(v) => toggleHealth(opt.value, v)}
                    />
                    <StatusBadge text={opt.value} tone={opt.tone} dot />
                  </span>
                  <span className="tnum shrink-0 text-[11px] text-muted">{opt.count}</span>
                </label>
              );
            })}
          </div>
        );
      }

      case "usage": {
        const presets: { value: UsagePresetFilter; label: string; count: number }[] = [
          { value: "all", label: "全部用量", count: allRows.length },
          {
            value: "unused",
            label: "未使用 (0 次)",
            count: allRows.filter((r) => r.todayCount === 0).length,
          },
          {
            value: "used",
            label: "已使用 (> 0 次)",
            count: allRows.filter((r) => r.todayCount > 0).length,
          },
          {
            value: "high",
            label: "高用量 (≥ 80%)",
            count: allRows.filter((r) => r.usagePct >= 80).length,
          },
        ];
        return (
          <div className="flex flex-col gap-2">
            <div className="flex flex-col gap-0.5">
              {presets.map((item) => (
                <OptionRow
                  key={item.value}
                  active={filters.usagePreset === item.value}
                  label={item.label}
                  count={item.count}
                  testId={`browser-filter-usage-${item.value}`}
                  onClick={() =>
                    onUpdateFilters((prev) => ({ ...prev, usagePreset: item.value }))
                  }
                />
              ))}
            </div>
            <div className="border-t border-divider pt-2">
              <span className="mb-1 block text-[10.5px] font-medium text-muted">
                今日次数范围
              </span>
              <div className="flex items-center gap-1.5">
                <Input
                  size="small"
                  data-testid="browser-filter-usage-min"
                  value={filters.usageMin}
                  placeholder="最少"
                  aria-label="今日用量最少次数"
                  onChange={(e) => {
                    const val = e.target.value.replace(/[^\d]/g, "");
                    onUpdateFilters((prev) => ({ ...prev, usageMin: val }));
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") {
                      e.preventDefault();
                      e.stopPropagation();
                      closePopover();
                    }
                  }}
                  className="tnum flex-1"
                />
                <span className="text-[11px] text-subtle">~</span>
                <Input
                  size="small"
                  data-testid="browser-filter-usage-max"
                  value={filters.usageMax}
                  placeholder="最多"
                  aria-label="今日用量最多次数"
                  onChange={(e) => {
                    const val = e.target.value.replace(/[^\d]/g, "");
                    onUpdateFilters((prev) => ({ ...prev, usageMax: val }));
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Escape") {
                      e.preventDefault();
                      e.stopPropagation();
                      closePopover();
                    }
                  }}
                  className="tnum flex-1"
                />
              </div>
            </div>
          </div>
        );
      }

      case "proxy": {
        const customCount = allRows.filter((r) => Boolean(r.proxy.trim())).length;
        const globalCount = allRows.length - customCount;
        const modes: { value: ProxyModeFilter; label: string; count: number }[] = [
          { value: "all", label: "全部代理配置", count: allRows.length },
          { value: "custom", label: "仅独立代理", count: customCount },
          { value: "global", label: "仅跟随全局", count: globalCount },
        ];
        return (
          <div className="flex flex-col gap-2">
            <div className="flex flex-col gap-0.5">
              {modes.map((m) => (
                <OptionRow
                  key={m.value}
                  active={filters.proxyMode === m.value}
                  label={m.label}
                  count={m.count}
                  testId={`browser-filter-proxy-${m.value}`}
                  onClick={() =>
                    onUpdateFilters((prev) => ({ ...prev, proxyMode: m.value }))
                  }
                />
              ))}
            </div>
            <div className="border-t border-divider pt-2">
              <Input
                size="small"
                data-testid="browser-filter-proxy-input"
                value={filters.proxyQuery}
                placeholder="筛选代理地址，如 7890"
                aria-label="筛选代理地址"
                onChange={(e) => {
                  const val = e.target.value;
                  onUpdateFilters((prev) => ({ ...prev, proxyQuery: val }));
                }}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    e.preventDefault();
                    e.stopPropagation();
                    closePopover();
                  }
                }}
                className="font-mono-console"
              />
            </div>
          </div>
        );
      }
    }
  };

  return (
    <th ref={thRef} scope="col" aria-sort={ariaSort} className={className}>
      <TooltipGroup className="group/th inline-flex max-w-full items-center gap-0.5">
        <Tooltip content={`按${label}排序`} side="top">
          <button
            type="button"
            data-testid={`browser-sort-${colKey}`}
            aria-label={`按${label}排序`}
            onClick={() => onCycleSort(colKey)}
            className={cn(
              "inline-flex cursor-pointer items-center gap-1 rounded-[6px] px-1 py-0.5 transition-colors hover:bg-sunken hover:text-ink",
              activeSortDir ? "font-semibold text-accent" : "text-muted",
            )}
          >
            <span>{label}</span>
            <Icon
              name={sortIcon}
              size={11}
              className={cn(
                "shrink-0 transition-opacity",
                activeSortDir
                  ? "text-accent opacity-100"
                  : "text-subtle opacity-35 group-hover/th:opacity-85",
              )}
            />
          </button>
        </Tooltip>

        <Tooltip content={`筛选${label}`} side="top">
          <button
            ref={filterBtnRef}
            type="button"
            data-testid={`browser-filter-btn-${colKey}`}
            aria-label={`筛选${label}`}
            aria-expanded={isOpen}
            onClick={() => onOpenColChange(isOpen ? null : colKey)}
            className={cn(
              "inline-flex size-5 shrink-0 cursor-pointer items-center justify-center rounded-[6px] transition-colors",
              filterActive || isOpen
                ? "bg-accent-soft text-accent opacity-100"
                : "text-subtle opacity-40 hover:bg-sunken hover:text-ink group-hover/th:opacity-90",
            )}
          >
            <Icon name="list-filter" size={11} />
          </button>
        </Tooltip>
      </TooltipGroup>

      {isOpen && typeof document !== "undefined"
        ? createPortal(
            <div
              ref={popoverRef}
              role="dialog"
              aria-label={`${label}排序与筛选`}
              data-testid={`browser-col-popover-${colKey}`}
              style={{ top: pos.top, left: pos.left, width: POPOVER_WIDTH }}
              className="fixed z-[60] rounded-[12px] border border-hairline bg-card p-2.5 text-ink float-shadow animate-tulai-scale-in select-none"
            >
              {/* 排序快捷操作 */}
              <div className="mb-2 flex items-center justify-between gap-1.5 border-b border-divider pb-2">
                <span className="text-[11px] font-semibold text-muted">{label}</span>
                <div className="inline-flex items-center gap-1">
                  <button
                    type="button"
                    data-testid={`browser-popover-sort-${colKey}-asc`}
                    onClick={() =>
                      onSetSort(colKey, activeSortDir === "asc" ? null : "asc")
                    }
                    className={cn(
                      "inline-flex cursor-pointer items-center gap-1 rounded-[7px] border px-2 py-0.5 text-[11px] font-medium transition-colors",
                      activeSortDir === "asc"
                        ? "border-accent-border bg-accent-soft text-accent"
                        : "border-hairline bg-sunken text-muted hover:bg-sunken-hover hover:text-ink",
                    )}
                  >
                    <Icon name="arrow-up" size={11} />
                    <span>升序</span>
                  </button>
                  <button
                    type="button"
                    data-testid={`browser-popover-sort-${colKey}-desc`}
                    onClick={() =>
                      onSetSort(colKey, activeSortDir === "desc" ? null : "desc")
                    }
                    className={cn(
                      "inline-flex cursor-pointer items-center gap-1 rounded-[7px] border px-2 py-0.5 text-[11px] font-medium transition-colors",
                      activeSortDir === "desc"
                        ? "border-accent-border bg-accent-soft text-accent"
                        : "border-hairline bg-sunken text-muted hover:bg-sunken-hover hover:text-ink",
                    )}
                  >
                    <Icon name="arrow-down" size={11} />
                    <span>降序</span>
                  </button>
                </div>
              </div>

              {/* 列筛选内容 */}
              {renderFilterBody()}

              {/* 底部重置操作 */}
              {filterActive || canResetColSort ? (
                <div className="mt-2 flex items-center justify-end border-t border-divider pt-2">
                  <Button
                    data-testid={`browser-col-reset-${colKey}`}
                    text="清除此列条件"
                    size="xs"
                    variant="ghost"
                    icon="rotate-ccw"
                    onClick={() => {
                      onResetColumn(colKey);
                      if (canResetColSort) onSetSort(colKey, null);
                    }}
                  />
                </div>
              ) : null}
            </div>,
            document.body,
          )
        : null}
    </th>
  );
}
