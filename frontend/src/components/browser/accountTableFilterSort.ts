import type { BrowserRowDTO } from "@/store";

export type AccountColumnKey =
  | "enabled"
  | "account"
  | "platform"
  | "health"
  | "usage"
  | "proxy";

export type SortDirection = "asc" | "desc";

export interface AccountSortState {
  key: AccountColumnKey;
  dir: SortDirection;
}

export type EnabledFilter = "all" | "enabled" | "disabled";
export type UsagePresetFilter = "all" | "unused" | "used" | "high";
export type ProxyModeFilter = "all" | "custom" | "global";

export interface AccountFiltersState {
  enabled: EnabledFilter;
  accountQuery: string;
  platforms: string[];
  healths: string[];
  usagePreset: UsagePresetFilter;
  usageMin: string;
  usageMax: string;
  proxyMode: ProxyModeFilter;
  proxyQuery: string;
}

export const DEFAULT_ACCOUNT_FILTERS: AccountFiltersState = {
  enabled: "all",
  accountQuery: "",
  platforms: [],
  healths: [],
  usagePreset: "all",
  usageMin: "",
  usageMax: "",
  proxyMode: "all",
  proxyQuery: "",
};

export const DEFAULT_ACCOUNT_SORT: AccountSortState = {
  key: "platform",
  dir: "asc",
};

export function isDefaultAccountSort(sort: AccountSortState | null): boolean {
  return (
    sort !== null &&
    sort.key === DEFAULT_ACCOUNT_SORT.key &&
    sort.dir === DEFAULT_ACCOUNT_SORT.dir
  );
}

const HEALTH_SORT_ORDER: Record<string, number> = {
  使用中: 0,
  运行中: 1,
  就绪: 2,
  冷却中: 3,
  需要人工验证: 4,
  未登录: 5,
  生图受限: 6,
  已用尽: 7,
  已禁用: 8,
};

const KNOWN_HEALTH_ORDER = [
  "就绪",
  "运行中",
  "使用中",
  "冷却中",
  "需要人工验证",
  "未登录",
  "生图受限",
  "已用尽",
  "已禁用",
];

function parseOptionalCount(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const parsed = Number.parseInt(trimmed, 10);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}

function accountDisplayName(row: BrowserRowDTO): string {
  return row.name || `账号 #${row.id}`;
}

function rowHealthLabel(row: BrowserRowDTO): string {
  return row.health || "未知";
}

export function isColumnFilterActive(
  filters: AccountFiltersState,
  col: AccountColumnKey,
): boolean {
  switch (col) {
    case "enabled":
      return filters.enabled !== "all";
    case "account":
      return filters.accountQuery.trim().length > 0;
    case "platform":
      return filters.platforms.length > 0;
    case "health":
      return filters.healths.length > 0;
    case "usage":
      return (
        filters.usagePreset !== "all" ||
        parseOptionalCount(filters.usageMin) !== null ||
        parseOptionalCount(filters.usageMax) !== null
      );
    case "proxy":
      return filters.proxyMode !== "all" || filters.proxyQuery.trim().length > 0;
  }
}

export function hasActiveAccountFilters(filters: AccountFiltersState): boolean {
  return (
    isColumnFilterActive(filters, "enabled") ||
    isColumnFilterActive(filters, "account") ||
    isColumnFilterActive(filters, "platform") ||
    isColumnFilterActive(filters, "health") ||
    isColumnFilterActive(filters, "usage") ||
    isColumnFilterActive(filters, "proxy")
  );
}

export function resetColumnFilter(
  filters: AccountFiltersState,
  col: AccountColumnKey,
): AccountFiltersState {
  switch (col) {
    case "enabled":
      return { ...filters, enabled: "all" };
    case "account":
      return { ...filters, accountQuery: "" };
    case "platform":
      return { ...filters, platforms: [] };
    case "health":
      return { ...filters, healths: [] };
    case "usage":
      return { ...filters, usagePreset: "all", usageMin: "", usageMax: "" };
    case "proxy":
      return { ...filters, proxyMode: "all", proxyQuery: "" };
  }
}

export function filterAccountRows(
  rows: BrowserRowDTO[],
  filters: AccountFiltersState,
): BrowserRowDTO[] {
  const accountKw = filters.accountQuery.trim().toLowerCase();
  const proxyKw = filters.proxyQuery.trim().toLowerCase();
  const minCount = parseOptionalCount(filters.usageMin);
  const maxCount = parseOptionalCount(filters.usageMax);
  const platformSet = filters.platforms.length > 0 ? new Set(filters.platforms) : null;
  const healthSet = filters.healths.length > 0 ? new Set(filters.healths) : null;

  return rows.filter((row) => {
    if (filters.enabled === "enabled" && row.disabled) return false;
    if (filters.enabled === "disabled" && !row.disabled) return false;

    if (accountKw) {
      const haystack = `${row.name} ${row.email} 账号 #${row.id} ${row.id}`.toLowerCase();
      if (!haystack.includes(accountKw)) return false;
    }

    if (platformSet && !platformSet.has(row.platform || "未知")) return false;
    if (healthSet && !healthSet.has(rowHealthLabel(row))) return false;

    if (filters.usagePreset === "unused" && row.todayCount !== 0) return false;
    if (filters.usagePreset === "used" && row.todayCount <= 0) return false;
    if (filters.usagePreset === "high" && row.usagePct < 80) return false;
    if (minCount !== null && row.todayCount < minCount) return false;
    if (maxCount !== null && row.todayCount > maxCount) return false;

    const hasCustomProxy = Boolean(row.proxy.trim());
    if (filters.proxyMode === "custom" && !hasCustomProxy) return false;
    if (filters.proxyMode === "global" && hasCustomProxy) return false;
    if (proxyKw && !(row.proxy || "跟随全局").toLowerCase().includes(proxyKw)) return false;

    return true;
  });
}

function compareAccountRows(
  a: BrowserRowDTO,
  b: BrowserRowDTO,
  key: AccountColumnKey,
  factor: 1 | -1,
): number {
  switch (key) {
    case "enabled": {
      if (a.disabled !== b.disabled) return (a.disabled ? 1 : -1) * factor;
      return accountDisplayName(a).localeCompare(accountDisplayName(b), "zh-CN", {
        numeric: true,
        sensitivity: "base",
      });
    }
    case "account": {
      const nameCmp = accountDisplayName(a).localeCompare(accountDisplayName(b), "zh-CN", {
        numeric: true,
        sensitivity: "base",
      });
      if (nameCmp !== 0) return nameCmp * factor;
      const emailCmp = a.email.localeCompare(b.email, "zh-CN", { sensitivity: "base" });
      if (emailCmp !== 0) return emailCmp * factor;
      return a.id - b.id;
    }
    case "platform": {
      const platCmp = (a.platform || "未知").localeCompare(b.platform || "未知", "zh-CN");
      if (platCmp !== 0) return platCmp * factor;
      return accountDisplayName(a).localeCompare(accountDisplayName(b), "zh-CN", {
        numeric: true,
        sensitivity: "base",
      });
    }
    case "health": {
      const ha = rowHealthLabel(a);
      const hb = rowHealthLabel(b);
      const oa = HEALTH_SORT_ORDER[ha] ?? 99;
      const ob = HEALTH_SORT_ORDER[hb] ?? 99;
      if (oa !== ob) return (oa - ob) * factor;
      const labelCmp = ha.localeCompare(hb, "zh-CN");
      if (labelCmp !== 0) return labelCmp * factor;
      return a.id - b.id;
    }
    case "usage": {
      if (a.todayCount !== b.todayCount) return (a.todayCount - b.todayCount) * factor;
      if (a.usagePct !== b.usagePct) return (a.usagePct - b.usagePct) * factor;
      return a.id - b.id;
    }
    case "proxy": {
      const pa = a.proxy.trim();
      const pb = b.proxy.trim();
      if (Boolean(pa) !== Boolean(pb)) return (pa ? -1 : 1) * factor;
      const proxyCmp = pa.localeCompare(pb, "en", { numeric: true, sensitivity: "base" });
      if (proxyCmp !== 0) return proxyCmp * factor;
      return a.id - b.id;
    }
  }
}

export function sortAccountRows(
  rows: BrowserRowDTO[],
  sort: AccountSortState | null,
): BrowserRowDTO[] {
  if (!sort) return rows;
  const factor: 1 | -1 = sort.dir === "asc" ? 1 : -1;
  return rows
    .map((row, idx) => ({ row, idx }))
    .sort((a, b) => {
      const cmp = compareAccountRows(a.row, b.row, sort.key, factor);
      if (cmp !== 0) return cmp;
      return a.idx - b.idx;
    })
    .map((item) => item.row);
}

export function getPlatformFilterOptions(
  rows: BrowserRowDTO[],
  basePlatforms: string[],
): { value: string; count: number }[] {
  const counts = new Map<string, number>();
  for (const p of basePlatforms) counts.set(p, 0);
  for (const r of rows) {
    const p = r.platform || "未知";
    counts.set(p, (counts.get(p) ?? 0) + 1);
  }
  return Array.from(counts.entries()).map(([value, count]) => ({ value, count }));
}

export function getHealthFilterOptions(
  rows: BrowserRowDTO[],
): { value: string; tone: string; count: number }[] {
  const counts = new Map<string, { tone: string; count: number }>();
  for (const r of rows) {
    const label = rowHealthLabel(r);
    const cur = counts.get(label);
    if (cur) {
      cur.count += 1;
    } else {
      counts.set(label, { tone: r.healthTone || "neutral", count: 1 });
    }
  }
  return Array.from(counts.entries())
    .sort(([a], [b]) => {
      const ia = KNOWN_HEALTH_ORDER.indexOf(a);
      const ib = KNOWN_HEALTH_ORDER.indexOf(b);
      if (ia >= 0 && ib >= 0) return ia - ib;
      if (ia >= 0) return -1;
      if (ib >= 0) return 1;
      return a.localeCompare(b, "zh-CN");
    })
    .map(([value, meta]) => ({ value, tone: meta.tone, count: meta.count }));
}
