import { create } from "zustand";
import { pushToast, type ToastLevel } from "@/components/ui/Toaster";
import { resetResultGridArrivalForTests } from "@/components/workbench/ResultGrid";
import { setMediaOrigin } from "@/lib/assets";
import { useDotMatrixStore } from "@/lib/dotmatrix/config";
import { resetDotMatrixEngineForTests } from "@/lib/dotmatrix/engine";
import {
  Browser,
  Core,
  Dedup,
  Download,
  ImageEdit,
  Prompts,
  Rename,
  Settings,
  Workbench,
  fireIpc,
  isMyGoRuntime,
  subscribeTopics,
} from "@/lib/ipc";
import {
  DEFAULT_BROWSER_DRAWER,
  DEFAULT_LOGS,
  DEFAULT_SHELL,
  DEFAULT_WIZARD,
  TAB_BROWSER,
  TAB_DEDUP,
  TAB_DOWNLOAD,
  TAB_FISSION,
  TAB_GEN,
  TAB_IMAGE_EDIT,
  TAB_PROMPTS,
  TAB_RENAME,
  TAB_SETTINGS,
  TAB_WASH,
  type BrowserDrawerDTO,
  type BrowserStateDTO,
  type DedupStateDTO,
  type DownloadStateDTO,
  type ImageEditStateDTO,
  type LogsStateDTO,
  type PageSliceKey,
  type PromptPartsStateDTO,
  type PromptsStateDTO,
  type RenameStateDTO,
  type SettingsStateDTO,
  type SharedExitHintDTO,
  type ShellStateDTO,
  type WizardStateDTO,
  type WorkbenchStateDTO,
} from "./types";

export * from "./types";

export interface AppStoreState {
  initialized: boolean;
  shell: ShellStateDTO;
  workbench: WorkbenchStateDTO | null;
  promptParts: PromptPartsStateDTO | null;
  browser: BrowserStateDTO | null;
  prompts: PromptsStateDTO | null;
  dedup: DedupStateDTO | null;
  download: DownloadStateDTO | null;
  rename: RenameStateDTO | null;
  imageEdit: ImageEditStateDTO | null;
  settings: SettingsStateDTO | null;
  browserDrawer: BrowserDrawerDTO;
  logs: LogsStateDTO;
  wizard: WizardStateDTO;
  loadingPages: Partial<Record<PageSliceKey, boolean>>;
  lastWorkbenchTab: number;
  accountListLeft: number;
  dismissedSharedExitHints: Record<string, string>;
}

export const useAppStore = create<AppStoreState>(() => ({
  initialized: false,
  shell: DEFAULT_SHELL,
  workbench: null,
  promptParts: null,
  browser: null,
  prompts: null,
  dedup: null,
  download: null,
  rename: null,
  imageEdit: null,
  settings: null,
  browserDrawer: DEFAULT_BROWSER_DRAWER,
  logs: DEFAULT_LOGS,
  wizard: DEFAULT_WIZARD,
  loadingPages: {},
  lastWorkbenchTab: TAB_WASH,
  accountListLeft: 0,
  dismissedSharedExitHints: {},
}));

// ---- Toast helper ----

export function showToast(
  title: string,
  level: ToastLevel = "info",
  description?: string,
): void {
  if (!title) return;
  pushToast(title, level, description);
}

// ---- Theme & shell synchronization ----

let lastToastSeq = 0;
let lastDark: boolean | null = null;

function applyThemeToDom(dark: boolean): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
}

function applyShellSnapshot(s: ShellStateDTO): void {
  if (s.mediaOrigin) setMediaOrigin(s.mediaOrigin);
  useDotMatrixStore.getState().setEffect(s.dotMatrixEffect);

  const prev = useAppStore.getState();
  const nextLastWb = s.tab <= TAB_FISSION ? s.tab : prev.lastWorkbenchTab;
  useAppStore.setState({
    shell: s,
    lastWorkbenchTab: nextLastWb,
  });

  const dark = Boolean(s.dark);
  if (dark !== lastDark) {
    lastDark = dark;
    applyThemeToDom(dark);
  }

  if (s.toastSeq > lastToastSeq) {
    lastToastSeq = s.toastSeq;
    if (s.toastText) {
      const level: ToastLevel =
        s.toastLevel === "error" || s.toastLevel === "warning" || s.toastLevel === "success"
          ? s.toastLevel
          : "info";
      showToast(s.toastText, level);
    }
  }
}

// ---- Monotonic sequence guard so stale async IPC replies never overwrite newer state ----

const seqCounters: Record<string, number> = {};

function nextSeq(key: string): number {
  const n = (seqCounters[key] ?? 0) + 1;
  seqCounters[key] = n;
  return n;
}

function isLatest(key: string, seq: number): boolean {
  return seqCounters[key] === seq;
}

// ---- Individual slice fetchers ----

export async function refreshShell(): Promise<ShellStateDTO> {
  if (!isMyGoRuntime()) return useAppStore.getState().shell;
  const seq = nextSeq("shell");
  try {
    const s = await Core.getShellState();
    if (isLatest("shell", seq)) applyShellSnapshot(s);
    return s;
  } catch (err) {
    console.warn("[tulai/store] refreshShell failed:", err);
    return useAppStore.getState().shell;
  }
}

async function fetchPageSlice<K extends PageSliceKey>(
  key: K,
  fetcher: () => Promise<NonNullable<AppStoreState[K]>>,
): Promise<AppStoreState[K]> {
  if (!isMyGoRuntime()) return useAppStore.getState()[key];
  const seq = nextSeq(key);
  const hadData = useAppStore.getState()[key] !== null;
  if (!hadData) {
    useAppStore.setState((st) => ({
      loadingPages: { ...st.loadingPages, [key]: true },
    }));
  }
  try {
    const data = await fetcher();
    if (isLatest(key, seq)) {
      if (key === "settings") {
        useDotMatrixStore.getState().setEffect((data as SettingsStateDTO).dotMatrixEffect);
      }
      useAppStore.setState((st) => ({
        [key]: data,
        loadingPages: { ...st.loadingPages, [key]: false },
      }));
    }
    return data;
  } catch (err) {
    console.warn(`[tulai/store] refresh ${key} failed:`, err);
    if (isLatest(key, seq)) {
      useAppStore.setState((st) => ({
        loadingPages: { ...st.loadingPages, [key]: false },
      }));
    }
    return useAppStore.getState()[key];
  }
}

export const refreshWorkbench = () =>
  fetchPageSlice("workbench", () => Workbench.getWorkbenchState());
export const refreshPromptParts = () =>
  fetchPageSlice("promptParts", () => Workbench.getPromptPartsState());
export const refreshBrowser = () =>
  fetchPageSlice("browser", () => Browser.getBrowserState());
export const refreshPrompts = () =>
  fetchPageSlice("prompts", () => Prompts.getPromptsState());
export const refreshDedup = () =>
  fetchPageSlice("dedup", () => Dedup.getDedupState());
export const refreshDownload = () =>
  fetchPageSlice("download", () => Download.getDownloadState());
export const refreshRename = () =>
  fetchPageSlice("rename", () => Rename.getRenameState());
export const refreshImageEdit = () =>
  fetchPageSlice("imageEdit", () => ImageEdit.getImageEditState());
export const refreshSettings = () =>
  fetchPageSlice("settings", () => Settings.getSettingsState());

export async function refreshBrowserDrawer(): Promise<BrowserDrawerDTO> {
  if (!isMyGoRuntime()) return useAppStore.getState().browserDrawer;
  const seq = nextSeq("browserDrawer");
  try {
    const data = await Browser.getBrowserDrawerState();
    if (isLatest("browserDrawer", seq)) {
      useAppStore.setState({ browserDrawer: data });
    }
    return data;
  } catch (err) {
    console.warn("[tulai/store] refreshBrowserDrawer failed:", err);
    return useAppStore.getState().browserDrawer;
  }
}

export async function refreshLogs(): Promise<LogsStateDTO> {
  if (!isMyGoRuntime()) return useAppStore.getState().logs;
  const seq = nextSeq("logs");
  try {
    const data = await Core.getLogsState();
    if (isLatest("logs", seq)) {
      useAppStore.setState({ logs: data });
    }
    return data;
  } catch (err) {
    console.warn("[tulai/store] refreshLogs failed:", err);
    return useAppStore.getState().logs;
  }
}

export async function refreshWizard(): Promise<WizardStateDTO> {
  if (!isMyGoRuntime()) return useAppStore.getState().wizard;
  const seq = nextSeq("wizard");
  try {
    const data = await Core.wizardState();
    if (isLatest("wizard", seq)) {
      useAppStore.setState({ wizard: data });
    }
    return data;
  } catch (err) {
    console.warn("[tulai/store] refreshWizard failed:", err);
    return useAppStore.getState().wizard;
  }
}

export async function refreshActiveTabPage(tab: number): Promise<void> {
  if (isWorkbenchTab(tab)) {
    const wb = await refreshWorkbench();
    if (wb?.structuredOpen) await refreshPromptParts();
  } else if (tab === TAB_BROWSER) {
    await refreshBrowser();
  } else if (tab === TAB_PROMPTS) {
    await refreshPrompts();
  } else if (tab === TAB_DEDUP) {
    await refreshDedup();
  } else if (tab === TAB_DOWNLOAD) {
    await refreshDownload();
  } else if (tab === TAB_RENAME) {
    await refreshRename();
  } else if (tab === TAB_IMAGE_EDIT) {
    await refreshImageEdit();
  } else if (tab === TAB_SETTINGS) {
    await Promise.all([refreshSettings(), refreshBrowser()]);
  }
}

// ---- Coalesced topic invalidation router ----

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

function enqueueTabRefreshTasks(
  tab: number,
  force: boolean,
  has: (...names: string[]) => boolean,
  tasks: Promise<unknown>[],
  queued: Set<string>,
): void {
  const needs = (...names: string[]) => force || has(...names);
  if (
    isWorkbenchTab(tab) &&
    !queued.has("workbench") &&
    needs("workbench", "browser", "browsers", "prompts", "settings")
  ) {
    queued.add("workbench");
    tasks.push(
      refreshWorkbench().then((wb) => {
        if (wb?.structuredOpen) return refreshPromptParts();
      }),
    );
  } else if (tab === TAB_BROWSER && !queued.has("browser") && needs("browser", "browsers", "wizard")) {
    queued.add("browser");
    tasks.push(refreshBrowser());
  } else if (tab === TAB_PROMPTS && !queued.has("prompts") && needs("prompts")) {
    queued.add("prompts");
    tasks.push(refreshPrompts());
  } else if (tab === TAB_DEDUP && !queued.has("dedup") && needs("dedup")) {
    queued.add("dedup");
    tasks.push(refreshDedup());
  } else if (tab === TAB_DOWNLOAD && !queued.has("download") && needs("download")) {
    queued.add("download");
    tasks.push(refreshDownload());
  } else if (tab === TAB_RENAME && !queued.has("rename") && needs("rename")) {
    queued.add("rename");
    tasks.push(refreshRename());
  } else if (tab === TAB_IMAGE_EDIT && !queued.has("imageedit") && needs("imageedit")) {
    queued.add("imageedit");
    tasks.push(refreshImageEdit());
  } else if (tab === TAB_SETTINGS && needs("settings", "update", "browser", "browsers")) {
    if (!queued.has("settings") && needs("settings", "update")) {
      queued.add("settings");
      tasks.push(refreshSettings());
    }
    if (!queued.has("browser") && needs("browser", "browsers", "settings")) {
      queued.add("browser");
      tasks.push(refreshBrowser());
    }
  }
}

export async function refreshTopics(raw: string): Promise<void> {
  const topics = raw
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);
  if (topics.length === 0) return;

  const set = new Set(topics);
  const all = set.has("all") || topics.some((t) => !KNOWN_TOPICS.has(t));
  const has = (...names: string[]) => all || names.some((name) => set.has(name));

  const initialState = useAppStore.getState();
  const previousTab = initialState.shell.tab;
  const needShell = has(
    "shell",
    "update",
    "toast",
    "workbench",
    "browser",
    "browsers",
    "settings",
    "logs",
    "wizard",
  );

  // Run shell refresh and known active-tab/drawer refreshes in parallel to halve IPC round-trips.
  const tasks: Promise<unknown>[] = [];
  const queued = new Set<string>();
  const shellPromise = needShell ? refreshShell() : null;

  enqueueTabRefreshTasks(previousTab, false, has, tasks, queued);

  if (has("browserDrawer") || (has("browser", "browsers") && initialState.browserDrawer.open)) {
    queued.add("browserDrawer");
    tasks.push(refreshBrowserDrawer());
  }
  if (has("logs") && (initialState.logs.open || initialState.shell.logOpen)) {
    queued.add("logs");
    tasks.push(refreshLogs());
  }
  if (has("wizard")) {
    queued.add("wizard");
    tasks.push(refreshWizard());
  }

  if (shellPromise) {
    await shellPromise;
    const updatedState = useAppStore.getState();
    const activeTab = updatedState.shell.tab;
    if (activeTab !== previousTab) {
      enqueueTabRefreshTasks(activeTab, true, has, tasks, queued);
    }
    if (has("logs") && !queued.has("logs") && (updatedState.logs.open || updatedState.shell.logOpen)) {
      queued.add("logs");
      tasks.push(refreshLogs());
    }
  }

  if (tasks.length > 0) {
    await Promise.all(tasks);
  }
}

export function refreshTopic(topic: string): Promise<void> {
  return refreshTopics(topic);
}

// ---- Lifecycle initialization ----

const TOPIC_COALESCE_MS = 16;

let started = false;
let unsubscribeTopics: (() => void) | null = null;
const pendingTopicBatch = new Set<string>();
let topicFlushTimer: ReturnType<typeof setTimeout> | null = null;

function scheduleTopicRefresh(topicsCsv: string): void {
  for (const part of topicsCsv.split(",")) {
    const trimmed = part.trim();
    if (trimmed) pendingTopicBatch.add(trimmed);
  }
  if (pendingTopicBatch.size === 0 || topicFlushTimer !== null) return;
  topicFlushTimer = setTimeout(() => {
    topicFlushTimer = null;
    const batch = Array.from(pendingTopicBatch).join(",");
    pendingTopicBatch.clear();
    if (batch) {
      void refreshTopics(batch);
    }
  }, TOPIC_COALESCE_MS);
}

export function initStore(): () => void {
  if (!started) {
    started = true;
    if (isMyGoRuntime()) {
      unsubscribeTopics = subscribeTopics((topicsCsv) => {
        scheduleTopicRefresh(topicsCsv);
      });
    }

    void (async () => {
      const sh = await refreshShell();
      const jobs: Promise<unknown>[] = [
        refreshActiveTabPage(sh.tab),
        refreshBrowserDrawer(),
        refreshWizard(),
      ];
      if (sh.logOpen) jobs.push(refreshLogs());
      await Promise.all(jobs);
      useAppStore.setState({ initialized: true });
    })();
  }

  return () => {
    // Keep global store alive across StrictMode double-mount; cleaned up only via resetStoreForTests
  };
}

export function resetStoreForTests(): void {
  unsubscribeTopics?.();
  unsubscribeTopics = null;
  if (topicFlushTimer !== null) {
    clearTimeout(topicFlushTimer);
    topicFlushTimer = null;
  }
  pendingTopicBatch.clear();
  started = false;
  lastToastSeq = 0;
  lastDark = null;
  resetDotMatrixEngineForTests();
  resetResultGridArrivalForTests();
  useDotMatrixStore.getState().resetAll();
  for (const k of Object.keys(seqCounters)) delete seqCounters[k];
  useAppStore.setState({
    initialized: false,
    shell: DEFAULT_SHELL,
    workbench: null,
    promptParts: null,
    browser: null,
    prompts: null,
    dedup: null,
    download: null,
    rename: null,
    imageEdit: null,
    settings: null,
    browserDrawer: DEFAULT_BROWSER_DRAWER,
    logs: DEFAULT_LOGS,
    wizard: DEFAULT_WIZARD,
    loadingPages: {},
    lastWorkbenchTab: TAB_WASH,
    accountListLeft: 0,
    dismissedSharedExitHints: {},
  });
}

// ---- Domain helpers & actions ----

export function isWorkbenchTab(tab: number): boolean {
  return tab <= TAB_FISSION;
}

export function getLastWorkbenchTab(): number {
  return useAppStore.getState().lastWorkbenchTab;
}

export function setAccountListLeft(x: number): void {
  if (Number.isFinite(x) && useAppStore.getState().accountListLeft !== x) {
    useAppStore.setState({ accountListLeft: x });
  }
}

export async function goTab(tab: number): Promise<void> {
  const cur = useAppStore.getState().shell.tab;
  if (tab === cur) return;
  // Optimistic local tab switch for instant perceived navigation
  useAppStore.setState((st) => ({
    shell: { ...st.shell, tab },
    lastWorkbenchTab: tab <= TAB_FISSION ? tab : st.lastWorkbenchTab,
  }));
  if (!isMyGoRuntime()) return;
  await Core.setTab(tab);
  await Promise.all([refreshShell(), refreshActiveTabPage(tab)]);
}

export async function toggleDarkMode(dark: boolean): Promise<void> {
  lastDark = dark;
  applyThemeToDom(dark);
  useAppStore.setState((st) => ({
    shell: { ...st.shell, dark },
  }));
  if (!isMyGoRuntime()) return;
  await Core.setDarkTheme(dark);
  await refreshShell();
}

export async function toggleNavCollapsed(folded: boolean): Promise<void> {
  useAppStore.setState((st) => ({
    shell: { ...st.shell, navCollapsed: folded, navManual: true },
  }));
  if (!isMyGoRuntime()) return;
  await Core.setNavFolded(folded);
  await refreshShell();
}

export async function toggleLogDrawer(open: boolean): Promise<void> {
  useAppStore.setState((st) => ({
    shell: { ...st.shell, logOpen: open },
    logs: { ...st.logs, open },
  }));
  if (!isMyGoRuntime()) return;
  await Core.setLogOpen(open);
  await Promise.all([refreshLogs(), refreshShell()]);
}

export async function updateWorkbenchField(field: string, value: string): Promise<void> {
  if (!isMyGoRuntime()) return;
  await Workbench.setWorkbenchField(field, value);
  await refreshTopic("workbench");
}

export async function selectWorkbenchPlatform(platform: string): Promise<void> {
  useAppStore.setState((st) => ({
    shell: { ...st.shell, platform },
  }));
  if (!isMyGoRuntime()) return;
  await Workbench.setPlatformSelected(platform, true);
  await refreshTopics("workbench,shell");
}

export async function openPlatformWorkbench(platform: string): Promise<void> {
  const st = useAppStore.getState();
  const onWorkbench = isWorkbenchTab(st.shell.tab);
  const targetTab = onWorkbench ? st.shell.tab : st.lastWorkbenchTab;
  useAppStore.setState({
    shell: { ...st.shell, platform, tab: targetTab },
  });
  if (!isMyGoRuntime()) return;
  await Workbench.setPlatformSelected(platform, true);
  if (!onWorkbench) {
    await Core.setTab(targetTab);
  }
  await refreshTopics("workbench,shell");
}

export async function updateSettingField(field: string, value: string): Promise<void> {
  if (!isMyGoRuntime()) return;
  await Settings.setSettingField(field, value);
  await refreshTopic("settings");
}

export async function closeWizardModal(): Promise<void> {
  useAppStore.setState((st) => ({
    wizard: { ...st.wizard, on: false },
    shell: { ...st.shell, wizardOn: false },
  }));
  if (!isMyGoRuntime()) return;
  await Core.finishWizard();
  await refreshTopics("wizard,shell");
}

export async function runWizardAction(action: string, value: string): Promise<void> {
  if (!isMyGoRuntime()) return;
  await Core.wizardAction(action, value);
  await refreshTopic("wizard");
}

export async function requeueWorkbenchDone(taskIndices: number[]): Promise<void> {
  if (taskIndices.length === 0 || !isMyGoRuntime()) return;
  await Workbench.requeueDoneTasks(taskIndices);
  await refreshTopics("workbench,shell");
}

export async function undoWorkbenchRequeue(): Promise<void> {
  if (!isMyGoRuntime()) return;
  await Workbench.undoRequeueTasks();
  await refreshTopics("workbench,shell");
}

export async function clearWorkbenchResults(): Promise<void> {
  if (!isMyGoRuntime()) return;
  try {
    await Workbench.clearWorkbenchResults();
    await refreshTopics("workbench,shell");
  } catch (err) {
    console.warn("[tulai/store] clearWorkbenchResults failed:", err);
  }
}

export async function deleteWorkbenchResult(
  taskIndex: number,
  path: string,
  trashFile: boolean,
): Promise<void> {
  if (!isMyGoRuntime()) return;
  try {
    await Workbench.deleteWorkbenchResult(taskIndex, path, trashFile);
    await refreshTopics("workbench,shell");
  } catch (err) {
    console.warn("[tulai/store] deleteWorkbenchResult failed:", err);
  }
}

export async function openInImageEdit(path: string): Promise<void> {
  if (!path || !isMyGoRuntime()) return;
  try {
    await ImageEdit.editOpenPath(path);
    await goTab(TAB_IMAGE_EDIT);
  } catch (err) {
    console.warn("[tulai/store] openInImageEdit failed:", err);
  }
}

export function dismissSharedExitHints(hints: SharedExitHintDTO[]): void {
  if (hints.length === 0) return;
  const platforms: string[] = [];
  useAppStore.setState((st) => {
    const next = { ...st.dismissedSharedExitHints };
    let changed = false;
    for (const hint of hints) {
      if (!hint.platform || !hint.message) continue;
      if (!platforms.includes(hint.platform)) {
        platforms.push(hint.platform);
      }
      if (next[hint.platform] !== hint.message) {
        next[hint.platform] = hint.message;
        changed = true;
      }
    }
    return changed ? { dismissedSharedExitHints: next } : st;
  });
  if (platforms.length > 0) {
    fireIpc(() => Browser.dismissSharedExitHints(platforms));
  }
}

// ---- React selector hooks ----

export const useShell = () => useAppStore((s) => s.shell);
export const useWorkbench = () => useAppStore((s) => s.workbench);
export const usePromptParts = () => useAppStore((s) => s.promptParts);
export const useBrowser = () => useAppStore((s) => s.browser);
export const usePrompts = () => useAppStore((s) => s.prompts);
export const useDedup = () => useAppStore((s) => s.dedup);
export const useDownload = () => useAppStore((s) => s.download);
export const useRename = () => useAppStore((s) => s.rename);
export const useImageEdit = () => useAppStore((s) => s.imageEdit);
export const useSettings = () => useAppStore((s) => s.settings);
export const useBrowserDrawer = () => useAppStore((s) => s.browserDrawer);
export const useViewedAccountId = () =>
  useAppStore((s) => (s.browserDrawer.open ? s.browserDrawer.accountId : 0));
export const useBrowserDrawerOpen = () =>
  useAppStore((s) => Boolean(s.browserDrawer.open && s.browserDrawer.accountId));
export const useLogs = () => useAppStore((s) => s.logs);
export const useWizard = () => useAppStore((s) => s.wizard);
export const useAccountListLeft = () => useAppStore((s) => s.accountListLeft);
export const useDismissedSharedExitHints = () =>
  useAppStore((s) => s.dismissedSharedExitHints);
export const usePageLoading = (slice: PageSliceKey) =>
  useAppStore((s) => Boolean(s.loadingPages[slice]));
