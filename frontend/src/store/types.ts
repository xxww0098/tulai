import type {
  BrowserDrawerDTO,
  LogsStateDTO,
  PlatformNavDTO,
  ShellStateDTO,
  WizardStateDTO,
} from "@/mygo";

export type {
  AccountLaneDTO,
  BrowserDrawerDTO,
  BrowserInputDTO,
  BrowserRowDTO,
  BrowserStateDTO,
  DedupFileItemDTO,
  DedupGroupDTO,
  DedupSigDTO,
  DedupStateDTO,
  DownloadGuideEntryDTO,
  DownloadGuideGroupDTO,
  DownloadImportColDTO,
  DownloadImportDTO,
  DownloadStateDTO,
  DownloadTaskDTO,
  EditChipDTO,
  EditCropBoxDTO,
  EditFileDTO,
  EditRatioPresetDTO,
  EditRecipeDTO,
  EditSizePresetDTO,
  FileDropEvent,
  ImageEditStateDTO,
  LaneJobDTO,
  LogEntryDTO,
  LogsStateDTO,
  PlatformNavDTO,
  PoolSummaryDTO,
  PromptOptionDTO,
  PromptPartItemDTO,
  PromptPartsStateDTO,
  PromptTemplateDTO,
  PromptsStateDTO,
  RenameRowDTO,
  RenameRuleDTO,
  RenameStateDTO,
  ResultRowDTO,
  ResultTileDTO,
  SettingsStateDTO,
  SharedExitHintDTO,
  ShellStateDTO,
  SourceItemDTO,
  TopicsEvent,
  UpdateStateDTO,
  WizardStateDTO,
  WizardStepDTO,
  WorkbenchStateDTO,
} from "@/mygo";

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
  shortcutIndex?: number;
  match: (t: number) => boolean;
}

export type PageSliceKey =
  | "workbench"
  | "promptParts"
  | "browser"
  | "prompts"
  | "dedup"
  | "download"
  | "rename"
  | "imageEdit"
  | "settings";

export const DEFAULT_PLATFORMS: PlatformNavDTO[] = [
  { id: "doubao", name: "豆包", ready: 0, busy: 0, total: 0 },
  { id: "qianwen", name: "千问", ready: 0, busy: 0, total: 0 },
  { id: "qwen_intl", name: "千问国际", ready: 0, busy: 0, total: 0 },
  { id: "gemini", name: "Gemini", ready: 0, busy: 0, total: 0 },
  { id: "jimeng", name: "即梦", ready: 0, busy: 0, total: 0 },
];

export const DEFAULT_SHELL: ShellStateDTO = {
  tab: TAB_WASH,
  platform: "豆包",
  platforms: DEFAULT_PLATFORMS,
  dark: false,
  navCollapsed: false,
  navManual: false,
  logOpen: false,
  wizardOn: false,
  running: false,
  runPage: 0,
  progText: "",
  progress: 0,
  logTotal: 0,
  logWarnings: 0,
  logErrors: 0,
  pool: { ready: 0, busy: 0, issue: 0, total: 0, summary: "" },
  update: {
    visible: false,
    version: "",
    current: "",
    checking: false,
    downloading: false,
    phase: "",
    fraction: 0,
    doneBytes: 0,
    totalBytes: 0,
    speedBps: 0,
    etaSeconds: 0,
    source: "",
    assetName: "",
    progressText: "",
    notes: "",
    detailsOpen: false,
    notesOpen: false,
    err: "",
    installed: "",
    deferred: false,
  },
  toastSeq: 0,
  toastText: "",
  toastLevel: "",
  version: "",
  os: typeof navigator !== "undefined" && /mac/i.test(navigator.platform) ? "darwin" : "windows",
  mediaOrigin: "tulaimedia://localhost",
  dotMatrixEffect: "grain",
};

export const DEFAULT_BROWSER_DRAWER: BrowserDrawerDTO = {
  open: false,
  accountId: 0,
  name: "",
  platform: "",
  title: "",
  url: "",
  loading: false,
  manual: false,
  awaitingVerify: false,
  resuming: false,
  busy: false,
  err: "",
  width: 0,
  height: 0,
  copyText: "",
  copyReady: false,
  frameVersion: 0,
  frameUrl: "",
};

export const DEFAULT_LOGS: LogsStateDTO = {
  open: false,
  pick: 0,
  query: "",
  source: "",
  paused: false,
  sources: [],
  total: 0,
  shown: 0,
  countAll: 0,
  countDebug: 0,
  countInfo: 0,
  countWarn: 0,
  countError: 0,
  lastAt: "",
  entries: [],
};

export const DEFAULT_WIZARD: WizardStateDTO = {
  on: false,
  step: "platform",
  choice: "豆包",
  first: false,
  steps: [],
  phase: "",
  note: "",
  err: "",
  navFailed: false,
  accountId: 0,
  accountName: "",
  planBackend: "Fingerprint",
  planKind: "",
  planTitle: "",
  planDetail: "",
  planCanNext: false,
  planBlocked: "",
  planNeedsDl: false,
  runtimeBusy: false,
  runtimePercent: 0,
  runtimeStatus: "",
};
