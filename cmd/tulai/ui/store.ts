import { batch, computed, setColorScheme, signal } from "nsgo";
import type { ToastsRef } from "nsgo/ui";
import {
  type BrowserDrawerDTO,
  type LogsStateDTO,
  type ShellStateDTO,
  type WizardStateDTO,
  getBrowserDrawerState,
  getBrowserState,
  getDedupState,
  getDownloadState,
  getImageEditState,
  getLogsState,
  getPromptPartsState,
  getPromptsState,
  getRenameState,
  getSettingsState,
  getShellState,
  getWorkbenchState,
  openWizard,
  requeueDoneTasks,
  setDarkTheme,
  setLogOpen,
  setNavFolded,
  setPlatformSelected,
  setSettingField,
  setTab,
  setWorkbenchField,
  undoRequeueTasks,
  finishWizard,
  wizardAction,
  watchEvents,
  wizardState,
} from "tulai/backend";

export const TAB_GEN = 0;
export const TAB_WASH = 1;
export const TAB_FISSION = 2;
export const TAB_DEDUP = 3;
export const TAB_BROWSER = 4;
export const TAB_PROMPTS = 5;
export const TAB_DOWNLOAD = 6;
export const TAB_RENAME = 7;
export const TAB_IMAGE_EDIT = 8;
export const TAB_SETTINGS = 9;

export interface NavItem {
  id: string;
  tab: number;
  label: string;
  icon: string;
  match: (t: number) => boolean;
}

export const NAV_ITEMS: NavItem[] = [
  { id: "workbench", tab: TAB_WASH, label: "图片生成", icon: "sparkles", match: (t) => t <= TAB_FISSION },
  { id: "dedup", tab: TAB_DEDUP, label: "去重", icon: "layers", match: (t) => t === TAB_DEDUP },
  { id: "browser", tab: TAB_BROWSER, label: "浏览器", icon: "globe", match: (t) => t === TAB_BROWSER },
  { id: "prompts", tab: TAB_PROMPTS, label: "提示词", icon: "book-open", match: (t) => t === TAB_PROMPTS },
  { id: "download", tab: TAB_DOWNLOAD, label: "下载", icon: "download", match: (t) => t === TAB_DOWNLOAD },
  { id: "rename", tab: TAB_RENAME, label: "批量重命名", icon: "pencil", match: (t) => t === TAB_RENAME },
  { id: "imageedit", tab: TAB_IMAGE_EDIT, label: "图片编辑", icon: "wand-sparkles", match: (t) => t === TAB_IMAGE_EDIT },
  { id: "settings", tab: TAB_SETTINGS, label: "设置", icon: "settings", match: (t) => t === TAB_SETTINGS },
];

export const [shellState, setShellState] = [
  signal<ShellStateDTO>({
    tab: TAB_WASH,
    runPage: 0,
    progress: 0,
    logTotal: 0,
    logWarnings: 0,
    logErrors: 0,
    pool: { ready: 0, busy: 0, issue: 0, total: 0 },
    update: { fraction: 0 },
    toastSeq: 0,
  }),
  undefined,
];

const shellSig = shellState;
// Page snapshots are read on first use. Background notifications only refresh
// the mounted page, and navigation reads the destination before it is shown.
function pageSnapshot<T>(read: () => T) {
  const value = signal<T>();
  const refresh = (): T => {
    const next = read();
    value.set(next);
    return next;
  };
  return [(): T => value() ?? refresh(), refresh] as const;
}

export const [workbench, refreshWorkbench] = pageSnapshot(getWorkbenchState);
export const [promptParts, refreshPromptParts] = pageSnapshot(getPromptPartsState);
export const [browser, refreshBrowser] = pageSnapshot(getBrowserState);
export const [promptsState, refreshPrompts] = pageSnapshot(getPromptsState);
export const [dedup, refreshDedup] = pageSnapshot(getDedupState);
export const [download, refreshDownload] = pageSnapshot(getDownloadState);
export const [renameState, refreshRename] = pageSnapshot(getRenameState);
export const [imageEdit, refreshImageEdit] = pageSnapshot(getImageEditState);
export const [settings, refreshSettings] = pageSnapshot(getSettingsState);

const browserDrawerSig = signal<BrowserDrawerDTO>(getBrowserDrawerState());
const logsSig = signal<LogsStateDTO>(getLogsState());
const wizardSig = signal<WizardStateDTO>(wizardState());

export const shell = shellSig;
export const browserDrawer = browserDrawerSig;
export const viewedAccountID = computed(() => browserDrawerSig().open ? browserDrawerSig().accountID : 0);
export const browserDrawerOpen = computed(() => Boolean(viewedAccountID()));
export const logs = logsSig;
export const wizard = wizardSig;

let toastsRef: ToastsRef | null = null;
let lastToastSeq = 0;
let lastDark: boolean | null = null;

export function setToastsRef(ref: ToastsRef | null) {
  toastsRef = ref;
}

export function showToast(title: string, type: "success" | "error" | "warning" | "info" = "info", description?: string) {
  if (!toastsRef) return;
  void toastsRef.show({ type, title, description });
}

function syncShell(s: ShellStateDTO) {
  shellSig.set(s);
  const dark = Boolean(s.dark);
  if (dark !== lastDark) {
    lastDark = dark;
    setColorScheme(dark ? "dark" : "light");
  }
  if (s.toastSeq > lastToastSeq) {
    lastToastSeq = s.toastSeq;
    if (s.toastText) {
      const level = s.toastLevel === "error" || s.toastLevel === "warning" || s.toastLevel === "success" ? s.toastLevel : "info";
      showToast(s.toastText, level);
    }
  }
}

const KNOWN_TOPICS = new Set([
  "all",
  "shell",
  "update",
  "toast",
  "workbench",
  "browser",
  "browsers",
  "browserDrawer",
  "prompts",
  "dedup",
  "download",
  "rename",
  "imageedit",
  "settings",
  "logs",
  "wizard",
]);

export function refreshTopics(raw: string) {
  const topics = raw
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
  if (topics.length === 0) return;

  const set = new Set(topics);
  const all = set.has("all") || topics.some((t) => !KNOWN_TOPICS.has(t));
  const has = (...names: string[]) => all || names.some((name) => set.has(name));

  batch(() => {
    const previousTab = shellSig().tab;
    if (has("shell", "update", "toast", "workbench", "browser", "browsers", "settings", "logs", "wizard")) {
      syncShell(getShellState());
    }

    const activeTab = shellSig().tab;
    const needsPage = (...names: string[]) => activeTab !== previousTab || has(...names);
    if (isWorkbenchTab(activeTab) && needsPage("workbench", "browser", "browsers", "prompts", "settings")) {
      if (refreshWorkbench().structuredOpen) refreshPromptParts();
    } else if (activeTab === TAB_BROWSER && needsPage("browser", "browsers", "wizard")) {
      refreshBrowser();
    } else if (activeTab === TAB_PROMPTS && needsPage("prompts")) {
      refreshPrompts();
    } else if (activeTab === TAB_DEDUP && needsPage("dedup")) {
      refreshDedup();
    } else if (activeTab === TAB_DOWNLOAD && needsPage("download")) {
      refreshDownload();
    } else if (activeTab === TAB_RENAME && needsPage("rename")) {
      refreshRename();
    } else if (activeTab === TAB_IMAGE_EDIT && needsPage("imageedit")) {
      refreshImageEdit();
    } else if (activeTab === TAB_SETTINGS && needsPage("settings", "update")) {
      refreshSettings();
    }
    if (has("browserDrawer") || (has("browser", "browsers") && browserDrawerSig().open)) {
      browserDrawerSig.set(getBrowserDrawerState());
    }
    if (has("logs") && (logsSig().open || shellSig().logOpen)) {
      logsSig.set(getLogsState());
    }
    if (has("wizard")) {
      wizardSig.set(wizardState());
    }
  });
}

export function refreshTopic(topic: string) {
  refreshTopics(topic);
}

export function refreshAll() {
  refreshTopics("all");
}

let started = false;
export function initStore() {
  syncShell(getShellState());
  if (started) return;
  started = true;
  void watchEvents((raw) => {
    refreshTopics(raw);
  });
}

export function isWorkbenchTab(tab: number): boolean {
  return tab <= TAB_FISSION;
}

export function goTab(tab: number) {
  if (tab === shellSig().tab) return;
  setTab(tab);
  refreshTopics("shell");
}

export function toggleDarkMode(dark: boolean) {
  setDarkTheme(dark);
  syncShell(getShellState());
}

export function toggleNavCollapsed(folded: boolean) {
  setNavFolded(folded);
  syncShell(getShellState());
}

export function toggleLogDrawer(open: boolean) {
  setLogOpen(open);
  batch(() => {
    logsSig.set(getLogsState());
    syncShell(getShellState());
  });
}

export function updateWorkbenchField(field: string, value: string) {
  setWorkbenchField(field, value);
  refreshTopic("workbench");
}

export function selectWorkbenchPlatform(platform: string) {
  setPlatformSelected(platform, true);
  refreshTopic("workbench");
}

export function updateSettingField(field: string, value: string) {
  setSettingField(field, value);
  refreshTopic("settings");
}

export function openWizardModal() {
  openWizard();
  refreshTopic("wizard");
}

export function closeWizardModal() {
  finishWizard();
  refreshTopic("wizard");
}

export async function runWizardAction(action: string, value: string) {
  await wizardAction(action, value);
  refreshTopic("wizard");
}

export function requeueWorkbenchDone(taskIndices: number[]) {
  if (taskIndices.length === 0) return;
  requeueDoneTasks(taskIndices);
  refreshTopics("workbench,shell");
}

export function undoWorkbenchRequeue() {
  undoRequeueTasks();
  refreshTopics("workbench,shell");
}
