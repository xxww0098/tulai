import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  BentoCard,
  FormRow,
  InsetPillRow,
  OutlineBadge,
  PageToolbar,
  StatusBadge,
} from "@/components/primitives";
import {
  Badge,
  Button,
  Checkbox,
  Icon,
  Input,
  Progress,
  Select,
  Switch,
  Tabs,
  Tooltip,
  type InputProps,
} from "@/components/ui";
import {
  DEFAULT_BROWSER_STATE,
  formatBytes,
  normalizeInstallProgress,
} from "@/lib/browserRuntime";
import { cn } from "@/lib/cn";
import { DotMatrixImageBox } from "@/components/dotmatrix/DotMatrixImageBox";
import previewPhoto from "@/assets/teton-preview.webp";
import {
  LOADING_PATTERNS,
  getDotMatrixEffectSpec,
  useDotMatrixStore,
  type LoadingPatternId,
  type PatternSpec,
} from "@/lib/dotmatrix/config";
import { Browser, Core, Settings } from "@/lib/ipc";
import { useSyncedDraft } from "@/lib/useSyncedDraft";
import {
  refreshBrowser,
  refreshSettings,
  toggleDarkMode,
  useAppStore,
  useBrowser,
  usePageLoading,
  useSettings,
  useShell,
  type SettingsStateDTO,
} from "@/store";

const SETTINGS_TABS = [
  { value: "0", label: "常规与输出", icon: "folder" },
  { value: "1", label: "账号与任务", icon: "sliders-horizontal" },
  { value: "2", label: "浏览器与网络", icon: "globe" },
  { value: "3", label: "关于与更新", icon: "refresh-cw" },
] as const;

// Exact Go backend contract (`internal/ui/bridge_settings.go:157`): "顺序循环" | "随机循环"
const STRATEGY_OPTIONS = [
  { value: "顺序循环", label: "顺序循环" },
  { value: "随机循环", label: "随机循环" },
];

// Exact Go backend contract (`internal/ui/bridge_settings.go:201`): "free" | "custom" | "ikunocr"
const CAPTCHA_BACKENDS = [
  { value: "free", label: "内置离线识别" },
  { value: "custom", label: "自定义视觉 API" },
  { value: "ikunocr", label: "iKunOCR 服务" },
];

// Matches `internal/update/policy.go` (`MirrorSetting`): "" = auto multi-source race, "off" = GitHub direct, or custom prefix(es)
const BASE_UPDATE_MIRRORS = [
  { value: "", label: "自动（多源测速择优）" },
  { value: "https://edgeone.gh-proxy.org/", label: "EdgeOne 加速 (gh-proxy.org)" },
  { value: "https://cdn.gh-proxy.org/", label: "CDN 加速 (gh-proxy.org)" },
  { value: "https://gh-proxy.org/", label: "镜像加速 (gh-proxy.org)" },
  { value: "https://ghfast.top/", label: "镜像加速 (ghfast.top)" },
  { value: "https://gh-proxy.com/", label: "镜像加速 (gh-proxy.com)" },
  { value: "https://ghproxy.net/", label: "镜像加速 (ghproxy.net)" },
  { value: "off", label: "仅 GitHub 直连" },
];
const CUSTOM_MIRROR_SENTINEL = "__custom__";

const DEFAULT_SETTINGS_STATE: SettingsStateDTO = {
  tab: 0,
  outputPath: "",
  resolvedOutput: "",
  genAutoNumber: false,
  washAutoNumber: false,
  numPrefix: "",
  numStart: 1,
  numWidth: 3,
  dlDir: "",
  resolvedDlDir: "",
  downloadConc: 4,
  downloadRetries: 1,
  downloadRateKb: 0,
  removeWatermark: false,
  autoDeleteHistory: false,
  concurrencyAuto: false,
  concurrencyMax: 2,
  sendStrategy: "顺序循环",
  retryCount: 0,
  sendWait: 0,
  dailyCapDoubao: 0,
  dailyCapQianwen: 0,
  dailyCapQwenIntl: 100,
  waitBefore: 0,
  waitResult: true,
  waitResultSeconds: 60,
  taskSwitch: false,
  switchCount: 1,
  startBrowserIdx: 1,
  taskBrowserVisible: false,
  riskyWords: "",
  proxyCustom: false,
  proxyUrl: "",
  captchaEnabled: false,
  captchaBackend: "free",
  customUrl: "",
  customKey: "",
  customModel: "",
  ikunocrKey: "",
  autoCheck: true,
  updateMirror: "",
  version: "",
  updateBusy: false,
  updateErr: "",
  updateNotice: "",
  dotMatrixEffect: "grain",
};

type FlushRegistrar = (fn: () => void) => () => void;

function invokeSafe(call: () => Promise<unknown>, after?: () => Promise<unknown>): Promise<boolean> {
  try {
    return Promise.resolve(call())
      .then(async () => {
        if (after) await after();
        return true;
      })
      .catch(() => {
        // Ignore runtime-unavailable rejections outside MyGo WebView (e.g. Vite preview)
        return false;
      });
  } catch {
    // Ignore synchronous runtime-unavailable errors
    return Promise.resolve(false);
  }
}

function SyncedSettingInput({
  field,
  serverValue,
  syncKey,
  onCommit,
  registerFlush,
  numeric = false,
  className,
  ...rest
}: {
  field: string;
  serverValue: string;
  syncKey: string | number;
  onCommit: (field: string, value: string) => void;
  registerFlush: FlushRegistrar;
  numeric?: boolean;
} & Omit<InputProps, "value" | "onChange" | "onBlur" | "onCompositionStart" | "onCompositionEnd">) {
  const draft = useSyncedDraft(
    serverValue,
    syncKey,
    (next) => onCommit(field, next),
    { debounceMs: 110 },
  );

  useEffect(() => registerFlush(draft.flush), [registerFlush, draft.flush]);

  return (
    <Input
      value={draft.value}
      onChange={(e) => draft.onChange(e.target.value)}
      onCompositionStart={draft.onCompositionStart}
      onCompositionEnd={(e) => draft.onCompositionEnd(e.currentTarget.value)}
      onBlur={draft.onBlur}
      inputMode={numeric ? "numeric" : rest.inputMode}
      className={cn(numeric && "tnum", className)}
      {...rest}
    />
  );
}

function SettingsDotMatrixOptionCard({
  spec,
  selected,
  onSelect,
  onKeyDown,
}: {
  spec: PatternSpec;
  selected: boolean;
  onSelect: () => void;
  onKeyDown: (e: React.KeyboardEvent<HTMLButtonElement>) => void;
}) {
  const [phase, setPhase] = useState<"waiting" | "revealing" | "shown">("waiting");

  useEffect(() => {
    if (phase === "revealing") return;
    if (phase === "shown") {
      const motion = window.matchMedia?.("(prefers-reduced-motion: reduce)");
      if (motion?.matches) {
        const resume = () => setPhase("waiting");
        motion.addEventListener("change", resume);
        return () => motion.removeEventListener("change", resume);
      }
    }
    const timer = window.setTimeout(
      () => setPhase(phase === "waiting" ? "revealing" : "waiting"),
      phase === "waiting" ? 900 + spec.index * 100 : 1400,
    );
    return () => window.clearTimeout(timer);
  }, [phase, spec.index]);

  return (
    <Tooltip content={`${spec.label} · ${spec.description}`} side="top">
      <button
        type="button"
        role="radio"
        aria-checked={selected}
        aria-label={spec.label}
        tabIndex={selected ? 0 : -1}
        data-testid={`settings-dotmatrix-option-${spec.id}`}
        onClick={onSelect}
        onKeyDown={onKeyDown}
      className={cn(
        "group flex cursor-pointer flex-col gap-1.5 rounded-[12px] border p-1.5 text-left transition-colors duration-150 select-none",
        selected
          ? "border-accent bg-accent-soft"
          : "border-hairline bg-sunken hover:border-strong hover:bg-sunken-hover",
      )}
    >
      <DotMatrixImageBox
        preview
        patternOverride={spec.id}
        revealOverride={spec.pairedReveal}
        src={phase === "waiting" ? "" : previewPhoto}
        alt="雪山与河谷"
        forceReveal
        onSettled={() => setPhase("shown")}
        cornerRadius={8}
        fit="cover"
        className="w-full"
        boxClassName="h-[clamp(96px,9vw,148px)] flex-none border border-hairline/75 dark:border-[#1E293B]"
      />
      <div className="flex items-center justify-between gap-1.5 px-1 pb-0.5">
        <span
          className={cn(
            "truncate text-xs font-semibold",
            selected ? "text-accent" : "text-ink",
          )}
        >
          {spec.label}
        </span>
        {selected ? (
          <Badge text="默认" variant="accent" />
        ) : (
          <span className="size-3.5 shrink-0 rounded-full border border-strong/70 transition-colors group-hover:border-accent/60" />
        )}
      </div>
    </button>
    </Tooltip>
  );
}

export function SettingsPage() {
  const settingsSlice = useSettings();
  const browserSlice = useBrowser();
  const loading = usePageLoading("settings");
  const sh = useShell();
  const st = settingsSlice ?? DEFAULT_SETTINGS_STATE;
  const browserSt = browserSlice ?? DEFAULT_BROWSER_STATE;
  const upd = sh.update;

  const [winWidth, setWinWidth] = useState(() =>
    typeof window !== "undefined" ? window.innerWidth : 1160,
  );
  useEffect(() => {
    const onResize = () => setWinWidth(window.innerWidth || 1160);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const wide = winWidth >= 1120;

  // Flush registry ensures tab switches, unmount, and page hide commit any pending debounced drafts
  const flushersRef = useRef<Set<() => void>>(new Set());
  const registerFlush = useCallback<FlushRegistrar>((fn) => {
    flushersRef.current.add(fn);
    return () => {
      flushersRef.current.delete(fn);
    };
  }, []);

  const flushAllDrafts = useCallback(() => {
    for (const fn of flushersRef.current) {
      fn();
    }
  }, []);

  const [saveState, setSaveState] = useState<"idle" | "saving" | "saved">("idle");
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    const onPageHide = () => flushAllDrafts();
    window.addEventListener("pagehide", onPageHide);
    window.addEventListener("beforeunload", onPageHide);
    return () => {
      mountedRef.current = false;
      window.removeEventListener("pagehide", onPageHide);
      window.removeEventListener("beforeunload", onPageHide);
      if (saveTimerRef.current !== null) {
        clearTimeout(saveTimerRef.current);
        saveTimerRef.current = null;
      }
      flushAllDrafts();
    };
  }, [flushAllDrafts]);

  const commitSetting = useCallback((field: string, value: string) => {
    if (saveTimerRef.current !== null) {
      clearTimeout(saveTimerRef.current);
      saveTimerRef.current = null;
    }
    setSaveState("saving");
    void invokeSafe(() => Settings.setSettingField(field, value), refreshSettings).then((ok) => {
      if (!mountedRef.current) return;
      if (!ok) {
        setSaveState("idle");
        return;
      }
      setSaveState("saved");
      saveTimerRef.current = setTimeout(() => {
        saveTimerRef.current = null;
        if (mountedRef.current) {
          setSaveState("idle");
        }
      }, 1800);
    });
  }, []);

  const activeDotEffect = getDotMatrixEffectSpec(
    settingsSlice ? st.dotMatrixEffect : sh.dotMatrixEffect,
  );

  useEffect(() => {
    useDotMatrixStore.getState().setEffect(activeDotEffect.id);
  }, [activeDotEffect.id]);

  const handleSelectDotMatrixEffect = useCallback(
    (effectId: LoadingPatternId) => {
      useDotMatrixStore.getState().setEffect(effectId);
      useAppStore.setState((cur) => ({
        shell: { ...cur.shell, dotMatrixEffect: effectId },
        settings: cur.settings
          ? { ...cur.settings, dotMatrixEffect: effectId }
          : { ...DEFAULT_SETTINGS_STATE, dotMatrixEffect: effectId },
      }));
      commitSetting("dotMatrixEffect", effectId);
    },
    [commitSetting],
  );

  const handleTabChange = useCallback(
    (value: string) => {
      flushAllDrafts();
      const tabNum = Math.max(0, Math.min(3, Number(value) || 0));
      // Optimistic local tab switch for instant perceived response and non-MyGo preview/test parity
      useAppStore.setState((cur) => ({
        settings: cur.settings ? { ...cur.settings, tab: tabNum } : { ...DEFAULT_SETTINGS_STATE, tab: tabNum },
      }));
      void invokeSafe(() => Settings.setSettingField("tab", String(tabNum)), refreshSettings);
    },
    [flushAllDrafts],
  );

  const handlePickDir = useCallback((field: "outputPath" | "dlDir") => {
    void invokeSafe(() => Settings.pickSettingDir(field), refreshSettings);
  }, []);

  const strategyValue = STRATEGY_OPTIONS.some((o) => o.value === st.sendStrategy)
    ? st.sendStrategy
    : "顺序循环";

  const captchaValue = CAPTCHA_BACKENDS.some((o) => o.value === st.captchaBackend)
    ? st.captchaBackend
    : "free";

  const rawUpdateMirror = (st.updateMirror || "").trim();
  const matchedPresetMirror = useMemo(() => {
    if (!rawUpdateMirror) return "";
    const norm = rawUpdateMirror.replace(/\/+$/, "").toLowerCase();
    const match = BASE_UPDATE_MIRRORS.find(
      (o) => o.value !== "" && o.value.replace(/\/+$/, "").toLowerCase() === norm,
    );
    return match ? match.value : null;
  }, [rawUpdateMirror]);

  const [customMirrorDraft, setCustomMirrorDraft] = useState(false);
  const customMirrorActive = customMirrorDraft || matchedPresetMirror === null;
  const customMirrorActiveRef = useRef(customMirrorActive);
  customMirrorActiveRef.current = customMirrorActive;

  const updateMirrorOptions = useMemo(
    () => [...BASE_UPDATE_MIRRORS, { value: CUSTOM_MIRROR_SENTINEL, label: "自定义镜像前缀..." }],
    [],
  );

  const selectedMirrorValue = customMirrorActive
    ? CUSTOM_MIRROR_SENTINEL
    : (matchedPresetMirror ?? "");

  const handleMirrorSelect = useCallback(
    (value: string) => {
      if (value === CUSTOM_MIRROR_SENTINEL) {
        customMirrorActiveRef.current = true;
        setCustomMirrorDraft(true);
        return;
      }
      customMirrorActiveRef.current = false;
      setCustomMirrorDraft(false);
      commitSetting("updateMirror", value);
    },
    [commitSetting],
  );

  const handleCustomMirrorCommit = useCallback(
    (field: string, value: string) => {
      if (!customMirrorActiveRef.current) return;
      commitSetting(field, value);
    },
    [commitSetting],
  );

  const activeTab = Math.max(0, Math.min(3, st.tab ?? 0));

  const installProg = normalizeInstallProgress(browserSt.percent);
  const installTransferMeta = useMemo(() => {
    const parts: string[] = [];
    if (browserSt.done > 0 && browserSt.total > 0) {
      parts.push(`${formatBytes(browserSt.done)} / ${formatBytes(browserSt.total)}`);
    } else if (browserSt.done > 0) {
      parts.push(formatBytes(browserSt.done));
    }
    if (browserSt.speed > 0) {
      parts.push(`${formatBytes(browserSt.speed)}/s`);
    }
    return parts.join(" · ");
  }, [browserSt.done, browserSt.total, browserSt.speed]);

  const handleInstallRuntime = useCallback(() => {
    void invokeSafe(() => Browser.installBrowserRuntime(), refreshBrowser);
  }, []);

  const handleRefreshBrowser = useCallback(() => {
    void invokeSafe(() => Browser.refreshBrowsers(), refreshBrowser);
  }, []);

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-paper">
      <PageToolbar
        left={
          !wide ? (
            <Tabs
              data-testid="settings-tabs"
              variant="segmented"
              size="small"
              options={SETTINGS_TABS.map(({ value, label }) => ({ value, label }))}
              value={String(activeTab)}
              onValueChange={handleTabChange}
            />
          ) : (
            <span className="text-xs font-medium text-muted">
              全局输出、账号调度与网络设置 · 修改后自动保存
            </span>
          )
        }
        right={
          saveState !== "idle" ? (
            <span data-testid="settings-autosave-status" className="animate-tulai-banner-in inline-flex">
              <StatusBadge
                text={saveState === "saving" ? "保存中..." : "已自动保存"}
                tone={saveState === "saving" ? "info" : "ok"}
                dot
                pulseDot={saveState === "saving"}
              />
            </span>
          ) : undefined
        }
      />

      {loading && !settingsSlice ? (
        <div className="flex min-h-0 flex-1 gap-3 px-3 pb-3">
          {wide ? (
            <div className="h-full w-[196px] shrink-0 animate-pulse rounded-[16px] border border-hairline bg-card" />
          ) : null}
          <div className="flex min-h-0 flex-1 flex-col items-center gap-3">
            <div className="h-[110px] w-full max-w-[840px] animate-pulse rounded-[16px] border border-hairline bg-card" />
            <div className="h-[220px] w-full max-w-[840px] animate-pulse rounded-[16px] border border-hairline bg-card" />
          </div>
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 gap-3 px-3 pb-3">
          {/* 宽屏左侧独立 Bento 分类卡 */}
          {wide ? (
            <aside
              data-testid="settings-rail"
              className="bento-shadow flex h-full w-[196px] shrink-0 flex-col gap-1 rounded-[16px] border border-hairline bg-card p-2.5"
            >
              <div className="px-2.5 pt-1 pb-1.5 text-[11px] font-semibold text-subtle">
                设置分类
              </div>
              {SETTINGS_TABS.map((item) => {
                const active = String(activeTab) === item.value;
                return (
                  <button
                    key={item.value}
                    type="button"
                    data-testid={`settings-rail-tab-${item.value}`}
                    aria-current={active ? "page" : undefined}
                    onClick={() => handleTabChange(item.value)}
                    className={cn(
                      "flex w-full cursor-pointer items-center gap-2.5 rounded-[12px] border px-3 py-2.5 text-left transition-[background-color,border-color,color,transform] duration-150 select-none active:scale-[0.98]",
                      active
                        ? "border-accent-border bg-accent-soft text-accent"
                        : "border-transparent bg-transparent text-muted hover:bg-sunken hover:text-ink",
                    )}
                  >
                    {active ? (
                      <span className="animate-tulai-indicator-in -ml-1 h-3.5 w-[3px] shrink-0 rounded-full bg-accent" />
                    ) : null}
                    <Icon
                      name={item.icon}
                      size={14}
                      className={active ? "text-accent" : "text-subtle"}
                    />
                    <span className={cn("text-xs", active ? "font-semibold" : "font-medium")}>
                      {item.label}
                    </span>
                  </button>
                );
              })}
            </aside>
          ) : null}

          <div className="scroll-gutter flex min-h-0 min-w-0 flex-1 flex-col items-center overflow-y-auto pb-6">
            <div className="flex w-full max-w-[840px] flex-col gap-3">
              {/* Tab 0: 常规与输出 */}
              {activeTab === 0 ? (
                <div key="settings-tab-0" className="animate-tulai-panel-in flex flex-col gap-3">
                  <BentoCard
                    compact
                    icon="sparkles"
                    title="界面外观"
                    subtitle="浅色 / 深色主题与工作台视觉风格"
                  >
                    <InsetPillRow>
                      <FormRow label="主题模式" hint="即时切换浅色与深色工作台">
                        <Tabs
                          data-testid="settings-theme-tabs"
                          variant="segmented"
                          size="small"
                          options={[
                            { value: "light", label: "浅色模式" },
                            { value: "dark", label: "深色模式" },
                          ]}
                          value={sh.dark ? "dark" : "light"}
                          onValueChange={(value) =>
                            invokeSafe(() => toggleDarkMode(value === "dark"))
                          }
                        />
                      </FormRow>
                    </InsetPillRow>
                  </BentoCard>

                  <BentoCard
                    compact
                    data-testid="settings-dotmatrix-card"
                    icon="layout-grid"
                    title="生图点阵效果"
                    subtitle="点击卡片预览并切换出图等待时的点阵效果"
                    right={
                      <OutlineBadge
                        data-testid="settings-dotmatrix-active-badge"
                        text={`默认：${activeDotEffect.label}`}
                      />
                    }
                  >
                    <div
                      role="radiogroup"
                      aria-label="生图默认点阵效果"
                      className="grid grid-cols-1 gap-2 sm:grid-cols-3"
                    >
                      {LOADING_PATTERNS.map((spec, idx) => (
                        <SettingsDotMatrixOptionCard
                          key={spec.id}
                          spec={spec}
                          selected={spec.id === activeDotEffect.id}
                          onSelect={() => handleSelectDotMatrixEffect(spec.id)}
                          onKeyDown={(e) => {
                            const total = LOADING_PATTERNS.length;
                            const step = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key];
                            if (!step) return;
                            e.preventDefault();
                            const next = (idx + step + total) % total;
                            e.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus();
                            handleSelectDotMatrixEffect(LOADING_PATTERNS[next].id);
                          }}
                        />
                      ))}
                    </div>
                  </BentoCard>

                  <BentoCard
                    compact
                    icon="folder"
                    title="输出目录与命名序号"
                    subtitle={st.resolvedOutput || "控制生成与洗图结果保存位置与编号规范"}
                  >
                    <div className="flex flex-col gap-2">
                      <InsetPillRow>
                        <FormRow label="图片输出目录">
                          <div className="flex items-center gap-2">
                            <SyncedSettingInput
                              field="outputPath"
                              serverValue={st.outputPath || ""}
                              syncKey="outputPath"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              placeholder="留空使用默认「输出结果」文件夹"
                              aria-label="图片输出目录"
                              className="flex-1"
                            />
                            <Button
                              text="浏览"
                              size="small"
                              variant="outline"
                              onClick={() => handlePickDir("outputPath")}
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>

                      <InsetPillRow>
                        <FormRow label="自动编号开关">
                          <div className="flex flex-wrap items-center gap-4">
                            <Checkbox
                              label="自由生图自动编号"
                              checked={Boolean(st.genAutoNumber)}
                              onCheckedChange={(checked) =>
                                commitSetting("genAutoNumber", checked ? "true" : "false")
                              }
                            />
                            <Checkbox
                              label="批量洗图自动编号"
                              checked={Boolean(st.washAutoNumber)}
                              onCheckedChange={(checked) =>
                                commitSetting("washAutoNumber", checked ? "true" : "false")
                              }
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>

                      <InsetPillRow>
                        <FormRow label="编号格式">
                          <div className="flex flex-wrap items-center gap-2">
                            <SyncedSettingInput
                              field="numPrefix"
                              serverValue={st.numPrefix || ""}
                              syncKey="numPrefix"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              placeholder="前缀"
                              aria-label="编号前缀"
                              className="w-[150px]"
                            />
                            <span className="text-xs text-muted">起始</span>
                            <SyncedSettingInput
                              field="numStart"
                              serverValue={String(st.numStart)}
                              syncKey="numStart"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="编号起始"
                              className="w-[74px]"
                            />
                            <span className="text-xs text-muted">位数</span>
                            <SyncedSettingInput
                              field="numWidth"
                              serverValue={String(st.numWidth)}
                              syncKey="numWidth"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="编号位数"
                              className="w-[74px]"
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>
                    </div>
                  </BentoCard>

                  <BentoCard
                    compact
                    icon="download"
                    title="网页下载默认参数"
                    subtitle={st.resolvedDlDir || "控制网页抓图与表格批量下载默认策略"}
                  >
                    <div className="flex flex-col gap-2">
                      <InsetPillRow>
                        <FormRow label="下载保存目录">
                          <div className="flex items-center gap-2">
                            <SyncedSettingInput
                              field="dlDir"
                              serverValue={st.dlDir || ""}
                              syncKey="dlDir"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              placeholder="留空则使用默认下载目录"
                              aria-label="下载保存目录"
                              className="flex-1"
                            />
                            <Button
                              text="浏览"
                              size="small"
                              variant="outline"
                              onClick={() => handlePickDir("dlDir")}
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>

                      <InsetPillRow>
                        <FormRow label="速率与重试">
                          <div className="flex flex-wrap items-center gap-2.5">
                            <span className="text-xs text-muted">同时下载</span>
                            <SyncedSettingInput
                              field="downloadConc"
                              serverValue={String(st.downloadConc)}
                              syncKey="downloadConc"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="下载并发"
                              className="w-[68px]"
                            />
                            <span className="text-xs text-muted">失败重试</span>
                            <SyncedSettingInput
                              field="downloadRetries"
                              serverValue={String(st.downloadRetries)}
                              syncKey="downloadRetries"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="下载重试"
                              className="w-[68px]"
                            />
                            <span className="text-xs text-muted">限速(KB/s, 0不限)</span>
                            <SyncedSettingInput
                              field="downloadRateKb"
                              serverValue={String(st.downloadRateKb)}
                              syncKey="downloadRateKb"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="下载限速"
                              className="w-[82px]"
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>
                    </div>
                  </BentoCard>
                </div>
              ) : null}

              {/* Tab 1: 账号与任务 */}
              {activeTab === 1 ? (
                <div key="settings-tab-1" className="animate-tulai-panel-in flex flex-col gap-3">
                  <BentoCard compact icon="sliders-horizontal" title="队列与发送控制">
                    <div className="flex flex-col gap-2">
                      <InsetPillRow>
                        <FormRow
                          label="自动化开关"
                          hint="「成功后自动删对话」仅在图片保存成功后清理本次网页对话；发送未确认或失败时会保留供核对"
                        >
                          <div className="flex flex-wrap items-center gap-4">
                            <Switch
                              size="small"
                              label="去结果水印"
                              checked={Boolean(st.removeWatermark)}
                              onCheckedChange={(checked) =>
                                commitSetting("removeWatermark", checked ? "true" : "false")
                              }
                            />
                            <Switch
                              size="small"
                              label="执行时显示浏览器"
                              checked={Boolean(st.taskBrowserVisible)}
                              onCheckedChange={(checked) =>
                                commitSetting("taskBrowserVisible", checked ? "true" : "false")
                              }
                            />
                            <Switch
                              data-testid="settings-auto-delete-history-switch"
                              size="small"
                              label="成功后自动删对话"
                              checked={Boolean(st.autoDeleteHistory)}
                              onCheckedChange={(checked) =>
                                commitSetting("autoDeleteHistory", checked ? "true" : "false")
                              }
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>

                      <InsetPillRow>
                        <FormRow label="账号调度策略">
                          <div className="flex flex-wrap items-center gap-2.5">
                            <div className="w-[152px] shrink-0">
                              <Select
                                data-testid="settings-strategy-select"
                                aria-label="账号调度策略"
                                options={STRATEGY_OPTIONS}
                                value={strategyValue}
                                onValueChange={(value) => commitSetting("sendStrategy", value)}
                              />
                            </div>
                            <span className="text-xs text-muted">失败重试</span>
                            <SyncedSettingInput
                              field="retryCount"
                              serverValue={String(st.retryCount)}
                              syncKey="retryCount"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="任务重试次数"
                              className="w-[62px]"
                            />
                            <span className="text-xs text-muted">发送间隔(秒)</span>
                            <SyncedSettingInput
                              field="sendWait"
                              serverValue={String(st.sendWait)}
                              syncKey="sendWait"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="发送间隔秒数"
                              className="w-[62px]"
                            />
                            <span className="text-xs text-muted">等待超时(秒)</span>
                            <SyncedSettingInput
                              field="waitResultSeconds"
                              serverValue={String(st.waitResultSeconds)}
                              syncKey="waitResultSeconds"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="等待结果超时秒数"
                              className="w-[70px]"
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>

                      <InsetPillRow>
                        <FormRow label="风险过滤词" hint="命中关键词时跳过发送；每行或逗号分隔">
                          <div className="flex items-center gap-2">
                            <SyncedSettingInput
                              field="riskyWords"
                              serverValue={st.riskyWords || ""}
                              syncKey="riskyWords"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              aria-label="风险过滤词"
                              className="flex-1"
                            />
                            <Button
                              text="恢复默认"
                              size="small"
                              variant="ghost"
                              onClick={() => commitSetting("resetRiskyWords", "1")}
                            />
                          </div>
                        </FormRow>
                      </InsetPillRow>
                    </div>
                  </BentoCard>

                  <BentoCard
                    compact
                    icon="layers"
                    title="平台每日出图上限"
                    subtitle="防止单一账号单日高频触发风控"
                  >
                    <InsetPillRow>
                      <FormRow label="单日账号上限" hint="填 0 表示不限制">
                        <div className="grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-2">
                          <div className="flex flex-col gap-1 rounded-[10px] border border-hairline bg-card p-2">
                            <span className="text-[11px] font-medium text-muted">豆包</span>
                            <SyncedSettingInput
                              field="dailyCapDoubao"
                              serverValue={String(st.dailyCapDoubao)}
                              syncKey="dailyCapDoubao"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="豆包单日上限"
                            />
                          </div>
                          <div className="flex flex-col gap-1 rounded-[10px] border border-hairline bg-card p-2">
                            <span className="text-[11px] font-medium text-muted">千问</span>
                            <SyncedSettingInput
                              field="dailyCapQianwen"
                              serverValue={String(st.dailyCapQianwen)}
                              syncKey="dailyCapQianwen"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="千问单日上限"
                            />
                          </div>
                          <div className="flex flex-col gap-1 rounded-[10px] border border-hairline bg-card p-2">
                            <span className="text-[11px] font-medium text-muted">千问国际</span>
                            <SyncedSettingInput
                              field="dailyCapQwenIntl"
                              serverValue={String(st.dailyCapQwenIntl)}
                              syncKey="dailyCapQwenIntl"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              numeric
                              aria-label="千问国际单日上限"
                            />
                          </div>
                        </div>
                      </FormRow>
                    </InsetPillRow>
                  </BentoCard>
                </div>
              ) : null}

              {/* Tab 2: 浏览器与网络 */}
              {activeTab === 2 ? (
                <div key="settings-tab-2" className="animate-tulai-panel-in flex flex-col gap-3">
                  <BentoCard
                    compact
                    data-testid="settings-browser-runtime-card"
                    icon="download"
                    title="内置浏览器"
                    subtitle={
                      browserSt.detail || "内置独立指纹浏览器内核 (Fingerprint)，与系统浏览器完全隔离"
                    }
                    right={
                      <div className="flex flex-wrap items-center gap-1.5">
                        <OutlineBadge text={browserSt.backendLabel || "Fingerprint"} />
                        <StatusBadge
                          text={browserSt.badgeLabel || (browserSt.ready ? "已安装" : "未就绪")}
                          tone={browserSt.badgeTone}
                          dot
                        />
                        <Button
                          text="刷新状态"
                          size="small"
                          icon="refresh-cw"
                          variant="outline"
                          onClick={handleRefreshBrowser}
                        />
                        {!browserSt.ready && !browserSt.busy ? (
                          <Button
                            data-testid="browser-install-btn"
                            text={`下载安装 ${browserSt.backendLabel || "Fingerprint"}`}
                            size="small"
                            icon="download"
                            variant="primary"
                            disabled={!browserSt.supported}
                            onClick={handleInstallRuntime}
                          />
                        ) : null}
                      </div>
                    }
                  >
                    <div className="flex flex-col gap-2">
                      {browserSt.busy ? (
                        <InsetPillRow className="gap-1.5">
                          <div className="flex items-center justify-between gap-2">
                            <span className="tnum truncate text-xs text-ink">
                              {installProg.indeterminate
                                ? browserSt.status || "正在下载内置浏览器..."
                                : `${browserSt.status || "正在下载内置浏览器"} · ${installProg.labelPct}%`}
                            </span>
                            {installTransferMeta ? (
                              <span className="tnum shrink-0 text-[11px] text-muted">
                                {installTransferMeta}
                              </span>
                            ) : null}
                          </div>
                          <Progress
                            value={installProg.indeterminate ? 0.15 : installProg.fraction}
                            aria-label="内置浏览器下载进度"
                          />
                        </InsetPillRow>
                      ) : (
                        <InsetPillRow>
                          <FormRow
                            label="内核状态"
                            hint={
                              !browserSt.supported
                                ? "当前系统架构暂不支持自动下载 Fingerprint"
                                : undefined
                            }
                          >
                            <span className="text-xs text-ink">
                              {browserSt.status ||
                                (browserSt.ready ? "Fingerprint 已就绪" : "尚未安装内置浏览器")}
                            </span>
                          </FormRow>
                        </InsetPillRow>
                      )}
                    </div>
                  </BentoCard>

                  <BentoCard
                    compact
                    icon="globe"
                    title="代理设置"
                    subtitle="同时作用于内置浏览器与网页下载（关闭时直连）"
                  >
                    <div className="flex flex-col gap-2">
                      <InsetPillRow>
                        <FormRow label="自定义代理">
                          <Switch
                            size="small"
                            label="启用自定义代理"
                            checked={Boolean(st.proxyCustom)}
                            onCheckedChange={(checked) =>
                              commitSetting("proxyCustom", checked ? "true" : "false")
                            }
                          />
                        </FormRow>
                      </InsetPillRow>

                      <InsetPillRow>
                        <FormRow label="代理地址">
                          <SyncedSettingInput
                            field="proxyUrl"
                            serverValue={st.proxyUrl || ""}
                            syncKey="proxyUrl"
                            onCommit={commitSetting}
                            registerFlush={registerFlush}
                            placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:7891"
                            disabled={!st.proxyCustom}
                            aria-label="全局代理地址"
                            className="font-mono-console"
                          />
                        </FormRow>
                      </InsetPillRow>
                    </div>
                  </BentoCard>

                  <BentoCard
                    compact
                    icon="eye"
                    title="验证码自动识别（高级）"
                    subtitle="开启后可辅助识别滑块或点选验证码"
                  >
                    <div className="flex flex-col gap-2">
                      <InsetPillRow>
                        <FormRow label="识别方式">
                          <div className="flex flex-wrap items-center gap-3">
                            <Switch
                              size="small"
                              label="启用自动识别"
                              checked={Boolean(st.captchaEnabled)}
                              onCheckedChange={(checked) =>
                                commitSetting("captchaEnabled", checked ? "true" : "false")
                              }
                            />
                            <div className="w-[196px] shrink-0">
                              <Select
                                data-testid="settings-captcha-select"
                                aria-label="验证码识别后端"
                                options={CAPTCHA_BACKENDS}
                                value={captchaValue}
                                onValueChange={(value) =>
                                  commitSetting("captchaBackend", value)
                                }
                              />
                            </div>
                          </div>
                        </FormRow>
                      </InsetPillRow>

                      {captchaValue === "custom" ? (
                        <InsetPillRow data-testid="settings-captcha-custom-fields" className="animate-tulai-banner-in">
                          <FormRow label="自定义视觉接口">
                            <div className="flex flex-col gap-2">
                              <SyncedSettingInput
                                field="customUrl"
                                serverValue={st.customUrl || ""}
                                syncKey="customUrl"
                                onCommit={commitSetting}
                                registerFlush={registerFlush}
                                placeholder="视觉 API Base URL"
                                aria-label="视觉 API 地址"
                                className="font-mono-console"
                              />
                              <div className="flex flex-wrap items-center gap-2">
                                <SyncedSettingInput
                                  field="customKey"
                                  serverValue={st.customKey || ""}
                                  syncKey="customKey"
                                  onCommit={commitSetting}
                                  registerFlush={registerFlush}
                                  placeholder="API Key"
                                  aria-label="视觉 API 密钥"
                                  masked
                                  wrapperClassName="min-w-[160px] flex-1"
                                />
                                <SyncedSettingInput
                                  field="customModel"
                                  serverValue={st.customModel || ""}
                                  syncKey="customModel"
                                  onCommit={commitSetting}
                                  registerFlush={registerFlush}
                                  placeholder="模型名称"
                                  aria-label="视觉模型名称"
                                  className="w-[180px]"
                                />
                              </div>
                            </div>
                          </FormRow>
                        </InsetPillRow>
                      ) : null}

                      {captchaValue === "ikunocr" ? (
                        <InsetPillRow data-testid="settings-captcha-ikunocr-fields" className="animate-tulai-banner-in">
                          <FormRow label="iKunOCR 密钥">
                            <SyncedSettingInput
                              field="ikunocrKey"
                              serverValue={st.ikunocrKey || ""}
                              syncKey="ikunocrKey"
                              onCommit={commitSetting}
                              registerFlush={registerFlush}
                              placeholder="iKunOCR Token"
                              aria-label="iKunOCR 密钥"
                              masked
                            />
                          </FormRow>
                        </InsetPillRow>
                      ) : null}
                    </div>
                  </BentoCard>
                </div>
              ) : null}

              {/* Tab 3: 关于与更新 */}
              {activeTab === 3 ? (
                <div key="settings-tab-3" className="animate-tulai-panel-in flex flex-col gap-3">
                  <BentoCard
                    compact
                    icon="refresh-cw"
                    title="关于与版本更新"
                    subtitle={`当前版本 v${st.version || upd.current || "dev"} · GitHub Releases 自动校验安装`}
                    right={
                      <div className="flex items-center gap-2">
                        <Button
                          data-testid="settings-check-update-btn"
                          text={upd.checking ? "检查中..." : "检查更新"}
                          size="small"
                          icon="refresh-cw"
                          variant="outline"
                          busy={Boolean(upd.checking)}
                          onClick={() => invokeSafe(() => Core.updateAction("check"))}
                        />
                        {upd.version ? (
                          <Button
                            data-testid="settings-install-update-btn"
                            text={upd.downloading ? "下载更新中..." : `更新到 v${upd.version}`}
                            size="small"
                            icon="download"
                            variant="primary"
                            busy={Boolean(upd.downloading)}
                            onClick={() => invokeSafe(() => Core.updateAction("install"))}
                          />
                        ) : null}
                      </div>
                    }
                  >
                    <div className="flex flex-col gap-2">
                      <InsetPillRow>
                        <FormRow label="更新策略">
                          <div className="flex flex-wrap items-center gap-4">
                            <Checkbox
                              label="启动时自动检查更新"
                              checked={Boolean(st.autoCheck)}
                              onCheckedChange={(checked) =>
                                commitSetting("autoCheck", checked ? "true" : "false")
                              }
                            />
                            <div className="w-[248px] shrink-0">
                              <Select
                                data-testid="settings-mirror-select"
                                aria-label="更新下载线路"
                                options={updateMirrorOptions}
                                value={selectedMirrorValue}
                                onValueChange={handleMirrorSelect}
                              />
                            </div>
                          </div>
                        </FormRow>
                      </InsetPillRow>

                      {customMirrorActive ? (
                        <InsetPillRow data-testid="settings-custom-mirror-row" className="animate-tulai-banner-in">
                          <FormRow label="自定义镜像">
                            <SyncedSettingInput
                              field="updateMirror"
                              serverValue={matchedPresetMirror === null ? rawUpdateMirror : ""}
                              syncKey="updateMirror"
                              onCommit={handleCustomMirrorCommit}
                              registerFlush={registerFlush}
                              placeholder="https://your-mirror.example/（支持逗号分隔多个前缀，优先测速）"
                              aria-label="自定义更新镜像地址"
                              className="font-mono-console"
                            />
                          </FormRow>
                        </InsetPillRow>
                      ) : null}

                      {upd.downloading ? (
                        <InsetPillRow className="gap-1.5">
                          <span className="tnum text-xs text-accent">
                            {upd.progressText || "正在下载更新包并验证校验和..."}
                          </span>
                          <Progress
                            value={Math.min(1, Math.max(0, upd.fraction))}
                            aria-label="更新下载进度"
                          />
                        </InsetPillRow>
                      ) : null}

                      {st.updateNotice || upd.installed ? (
                        <div className="flex">
                          <Badge
                            text={st.updateNotice || `已准备更新 ${upd.installed}`}
                            variant="success"
                          />
                        </div>
                      ) : null}

                      {st.updateErr || upd.err ? (
                        <p className="select-text text-xs text-status-danger">
                          {st.updateErr || upd.err}
                        </p>
                      ) : null}
                    </div>
                  </BentoCard>

                  {upd.notes ? (
                    <BentoCard compact icon="book-open" title="更新日志">
                      <InsetPillRow>
                        <pre className="select-text whitespace-pre-wrap font-sans text-xs leading-relaxed text-ink">
                          {upd.notes}
                        </pre>
                      </InsetPillRow>
                    </BentoCard>
                  ) : null}
                </div>
              ) : null}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
