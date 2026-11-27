import type { BrowserStateDTO } from "@/store";

export const DEFAULT_BROWSER_STATE: BrowserStateDTO = {
  backend: "Fingerprint",
  backendLabel: "Fingerprint",
  engines: ["Fingerprint"],
  ready: false,
  busy: false,
  badgeLabel: "",
  badgeTone: "neutral",
  status: "",
  detail: "",
  percent: 0,
  done: 0,
  total: 0,
  speed: 0,
  supported: true,
  running: false,
  rows: [],
  selectedId: 0,
  checkedCount: 0,
  batchActive: false,
  batchKind: "",
  batchText: "",
  missingHint: "",
  sharedExitHints: [],
  dlBrowserBusy: false,
  dlBrowserOpen: false,
  dlBrowserUrl: "",
  doubaoConcurrencyAuto: true,
  doubaoConcurrencyMax: 2,
  qianwenConcurrencyAuto: true,
  qianwenConcurrencyMax: 2,
  geminiConcurrencyAuto: true,
  geminiConcurrencyMax: 2,
  jimengConcurrencyAuto: true,
  jimengConcurrencyMax: 2,
};

export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "";
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  if (n >= 1024) return `${Math.round(n / 1024)} KB`;
  return `${Math.round(n)} B`;
}

export function normalizeInstallProgress(percent: number): {
  indeterminate: boolean;
  fraction: number;
  labelPct: number;
} {
  if (!Number.isFinite(percent) || percent < 0) {
    return { indeterminate: true, fraction: 0, labelPct: 0 };
  }
  const fraction = percent > 1 ? Math.min(1, percent / 100) : Math.max(0, Math.min(1, percent));
  return {
    indeterminate: false,
    fraction,
    labelPct: Math.round(fraction * 100),
  };
}
