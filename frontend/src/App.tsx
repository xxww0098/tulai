import { useEffect, useState } from "react";
// Brand marks from @lobehub/icons (MIT).
import doubaoMark from "@/assets/providers/doubao-avatar.png";
import geminiMark from "@/assets/providers/gemini-color.svg";
import jimengMark from "@/assets/providers/jimeng-color.svg";
import qwenMark from "@/assets/providers/qwen-color.svg";
import qwenIntlMark from "@/assets/providers/qwen-intl-color.svg";
import { BrowserDrawer } from "@/components/BrowserDrawer";
import { HoverPreviewHost, clearHoverPreview } from "@/components/HoverPreview";
import { LogDrawer } from "@/components/LogDrawer";
import { useResolvedDotMatrixDark } from "@/components/dotmatrix/DotMatrixImageBox";
import { BrandLogo } from "@/components/primitives";
import {
  Button,
  Icon,
  IconButton,
  Progress,
  Select,
  Tabs,
  Toaster,
  Tooltip,
  TooltipGroup,
  TooltipProvider,
} from "@/components/ui";
import { cn } from "@/lib/cn";
import { Core, fireIpc, openExternalUrl } from "@/lib/ipc";
import { installGlobalShortcuts, modKeyLabel } from "@/lib/shortcuts";
import { describeUpdateProgress } from "@/lib/updateProgress";
import { BrowserPage } from "@/pages/BrowserPage";
import { DedupPage } from "@/pages/DedupPage";
import { DownloadPage } from "@/pages/DownloadPage";
import { ImageEditPage } from "@/pages/ImageEditPage";
import { PromptsPage } from "@/pages/PromptsPage";
import { RenamePage } from "@/pages/RenamePage";
import { SettingsPage } from "@/pages/SettingsPage";
import { WizardView } from "@/pages/WizardView";
import { WorkbenchPage } from "@/pages/WorkbenchPage";
import {
  DEFAULT_PLATFORMS,
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
  getLastWorkbenchTab,
  goTab,
  initStore,
  isWorkbenchTab,
  openPlatformWorkbench,
  selectWorkbenchPlatform,
  toggleLogDrawer,
  toggleNavCollapsed,
  updateSettingField,
  useAppStore,
  useBrowserDrawerOpen,
  useShell,
  type NavItem,
} from "@/store";

const GITHUB_REPO_URL = "https://github.com/xxww0098/tulai";

// Exact workbench mode order required by PRODUCT.md & ui_test.go
const WORKBENCH_MODE_OPTIONS = [
  { value: String(TAB_WASH), label: "批量洗图" },
  { value: String(TAB_GEN), label: "自由生图" },
  { value: String(TAB_FISSION), label: "裂变图片" },
];

const WORKBENCH_PLATFORM_OPTIONS = [
  { value: "豆包", label: "豆包" },
  { value: "千问", label: "千问" },
  { value: "千问国际", label: "千问国际" },
  { value: "Gemini", label: "Gemini" },
  { value: "即梦", label: "即梦" },
];

const PROVIDER_MARKS: Record<string, string> = {
  doubao: doubaoMark,
  豆包: doubaoMark,
  qianwen: qwenMark,
  千问: qwenMark,
  qwen_intl: qwenIntlMark,
  千问国际: qwenIntlMark,
  gemini: geminiMark,
  Gemini: geminiMark,
  jimeng: jimengMark,
  即梦: jimengMark,
};

const WORKBENCH_NAV: NavItem = {
  id: "workbench",
  tab: TAB_WASH,
  label: "图片生成",
  icon: "sparkles",
  shortcutIndex: 1,
  match: (t) => t <= TAB_FISSION,
};

const CORE_NAV: NavItem[] = [
  {
    id: "browser",
    tab: TAB_BROWSER,
    label: "账号池",
    icon: "users",
    shortcutIndex: 2,
    match: (t) => t === TAB_BROWSER,
  },
  {
    id: "prompts",
    tab: TAB_PROMPTS,
    label: "提示词",
    icon: "book-open",
    shortcutIndex: 3,
    match: (t) => t === TAB_PROMPTS,
  },
];

const TOOL_NAV: NavItem[] = [
  {
    id: "download",
    tab: TAB_DOWNLOAD,
    label: "下载",
    icon: "download",
    shortcutIndex: 4,
    match: (t) => t === TAB_DOWNLOAD,
  },
  {
    id: "dedup",
    tab: TAB_DEDUP,
    label: "去重",
    icon: "layers",
    shortcutIndex: 5,
    match: (t) => t === TAB_DEDUP,
  },
  {
    id: "rename",
    tab: TAB_RENAME,
    label: "批量重命名",
    icon: "pencil",
    shortcutIndex: 6,
    match: (t) => t === TAB_RENAME,
  },
  {
    id: "imageedit",
    tab: TAB_IMAGE_EDIT,
    label: "图片编辑",
    icon: "wand-sparkles",
    shortcutIndex: 7,
    match: (t) => t === TAB_IMAGE_EDIT,
  },
];

const SETTINGS_NAV: NavItem = {
  id: "settings",
  tab: TAB_SETTINGS,
  label: "设置",
  icon: "settings",
  shortcutIndex: 8,
  match: (t) => t === TAB_SETTINGS,
};

const SHORTCUT_TAB_MAP: Record<number, () => number> = {
  1: () => getLastWorkbenchTab(),
  2: () => TAB_BROWSER,
  3: () => TAB_PROMPTS,
  4: () => TAB_DOWNLOAD,
  5: () => TAB_DEDUP,
  6: () => TAB_RENAME,
  7: () => TAB_IMAGE_EDIT,
  8: () => TAB_SETTINGS,
};

function RailNavButton({
  nav,
  active,
  folded,
  onClick,
}: {
  nav: NavItem;
  active: boolean;
  folded: boolean;
  onClick?: () => void;
}) {
  const shortcut = nav.shortcutIndex ? `${modKeyLabel()}${nav.shortcutIndex}` : undefined;

  const btn = (
    <button
      type="button"
      data-testid={`nav-${nav.id}`}
      aria-current={active ? "page" : undefined}
      onClick={() => {
        clearHoverPreview();
        if (onClick) {
          onClick();
        } else {
          void goTab(nav.tab);
        }
      }}
      className={cn(
        "group relative flex w-full cursor-pointer items-center gap-2 rounded-[10px] border transition-[background-color,border-color,color,transform] duration-150 select-none active:scale-[0.98]",
        folded ? "size-8 justify-center px-0" : "px-2 py-1.5",
        active
          ? "border-[#DFE3EA] bg-rail-active text-ink shadow-[0_1px_2px_rgba(15,23,42,0.04)] dark:border-[#263147] dark:shadow-none"
          : "border-transparent bg-transparent text-muted hover:bg-rail-hover hover:text-ink",
      )}
    >
      {!folded && active ? (
        <span className="animate-tulai-indicator-in -ml-0.5 h-3.5 w-[3px] shrink-0 rounded-full bg-accent" />
      ) : null}
      <Icon
        name={nav.icon}
        size={15}
        className={cn(
          "transition-colors",
          active ? "text-accent" : "text-muted group-hover:text-ink",
        )}
      />
      {!folded ? (
        <span
          className={cn(
            "min-w-0 flex-1 truncate text-left text-xs",
            active ? "font-semibold text-ink" : "font-medium text-muted group-hover:text-ink",
          )}
        >
          {nav.label}
        </span>
      ) : null}
    </button>
  );

  if (!folded) return btn;

  return (
    <Tooltip content={nav.label} shortcut={shortcut} side="right">
      {btn}
    </Tooltip>
  );
}

function SidebarLogButton({
  open,
  total,
  errors,
  folded,
}: {
  open: boolean;
  total: number;
  errors: number;
  folded: boolean;
}) {
  const ariaLabel =
    errors > 0
      ? `运行日志 (${errors} 条错误)`
      : total > 0
        ? `运行日志 (${total})`
        : "运行日志";

  const countNode =
    errors > 0 ? (
      <span className="tnum shrink-0 rounded-full border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] px-1.5 text-[10px] leading-4 font-semibold text-status-danger">
        {errors}
      </span>
    ) : total > 0 ? (
      <span
        className={cn(
          "tnum shrink-0 rounded-full px-1.5 text-[10px] leading-4 font-medium",
          open ? "bg-accent-soft text-accent" : "bg-card/90 text-muted dark:bg-sunken",
        )}
      >
        {total}
      </span>
    ) : null;

  return (
    <Tooltip
      content={ariaLabel}
      shortcut={`${modKeyLabel()}J`}
      side="right"
    >
      <button
        type="button"
        data-testid="sidebar-log-btn"
        aria-label={ariaLabel}
        aria-pressed={open}
        onClick={() => {
          clearHoverPreview();
          void toggleLogDrawer(!open);
        }}
        className={cn(
          "group relative flex w-full cursor-pointer items-center gap-2 rounded-[10px] border transition-[background-color,border-color,color,transform] duration-150 select-none active:scale-[0.98]",
          folded ? "size-8 justify-center px-0" : "px-2 py-1.5",
          open
            ? "border-[#DFE3EA] bg-rail-active text-ink shadow-[0_1px_2px_rgba(15,23,42,0.04)] dark:border-[#263147] dark:shadow-none"
            : "border-transparent bg-transparent text-muted hover:bg-rail-hover hover:text-ink",
        )}
      >
        <Icon
          name="terminal"
          size={15}
          className={cn(
            "transition-colors",
            open
              ? "text-accent"
              : errors > 0
                ? "text-status-danger"
                : "text-muted group-hover:text-ink",
          )}
        />
        {!folded ? (
          <>
            <span
              className={cn(
                "min-w-0 flex-1 truncate text-left text-xs",
                open ? "font-semibold text-ink" : "font-medium text-muted group-hover:text-ink",
              )}
            >
              日志
            </span>
            {countNode}
          </>
        ) : errors > 0 ? (
          <span
            aria-hidden="true"
            className="absolute top-1 right-1 size-1.5 rounded-full bg-status-danger"
          />
        ) : null}
      </button>
    </Tooltip>
  );
}

export function App() {
  const s = useShell();
  const browserDrawerOpen = useBrowserDrawerOpen();
  useResolvedDotMatrixDark();
  const [providersOpen, setProvidersOpen] = useState(true);
  const [winWidth, setWinWidth] = useState(() =>
    typeof window !== "undefined" ? window.innerWidth : 1160,
  );

  useEffect(() => {
    const cleanup = initStore();
    return cleanup;
  }, []);

  useEffect(() => {
    const onResize = () => setWinWidth(window.innerWidth || 1160);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  useEffect(() => {
    const onContextMenu = (e: MouseEvent) => {
      e.preventDefault();
    };
    document.addEventListener("contextmenu", onContextMenu);
    return () => document.removeEventListener("contextmenu", onContextMenu);
  }, []);

  useEffect(() => {
    return installGlobalShortcuts({
      onSelectNavIndex: (idx) => {
        const resolver = SHORTCUT_TAB_MAP[idx];
        if (!resolver) return;
        clearHoverPreview();
        void goTab(resolver());
      },
      onOpenSettings: () => {
        clearHoverPreview();
        void goTab(TAB_SETTINGS);
      },
      onToggleSidebar: () => {
        const st = useAppStore.getState().shell;
        const currentlyFolded = Boolean(
          st.navCollapsed || (!st.navManual && (window.innerWidth || 1160) < 960),
        );
        void toggleNavCollapsed(!currentlyFolded);
      },
      onToggleLogs: () => {
        const open = useAppStore.getState().shell.logOpen;
        void toggleLogDrawer(!open);
      },
    });
  }, []);

  const tab = s.tab;
  const activePlatform = s.platform || "豆包";
  const navPlatforms = s.platforms && s.platforms.length > 0 ? s.platforms : DEFAULT_PLATFORMS;
  const folded = Boolean(s.navCollapsed || (!s.navManual && winWidth < 960));
  const isMac = s.os === "darwin";
  const updateProg = describeUpdateProgress(s.update);
  const showSidebarUpdate = Boolean(
    (s.update.visible && s.update.version) || s.update.deferred,
  );
  const sidebarUpdateTooltip = updateProg.active
    ? [
        updateProg.stageLabel,
        updateProg.percentLabel,
        updateProg.speedText,
        updateProg.etaText,
      ]
        .filter(Boolean)
        .join(" · ")
    : `发现新版本 v${s.update.version}`;
  const openUpdateSettings = () => {
    clearHoverPreview();
    void goTab(TAB_SETTINGS);
    void updateSettingField("tab", "3");
  };

  return (
    <TooltipProvider>
      <div
        data-testid="app-root"
        className="relative flex h-full w-full flex-row overflow-hidden bg-paper text-ink"
      >
        {/* Left Navigation Sidebar Rail */}
        <aside
          data-testid="sidebar-rail"
          className={cn(
            "flex h-full shrink-0 flex-col border-r border-rail-border bg-rail transition-[width] duration-180 ease-out",
            folded ? "w-[52px]" : "w-[160px]",
          )}
        >
          {/* macOS hiddenInset traffic-light safe drag strip */}
          {isMac ? (
            <div
              className="app-drag-region shrink-0"
              style={{ height: "calc(var(--mygo-titlebar-controls-bottom, 28px) + 4px)" }}
            />
          ) : null}

          {/* Brand & collapse header */}
          <div
            className={cn(
              "app-drag-region flex h-[44px] shrink-0 items-center",
              folded ? "justify-center px-1" : "justify-between gap-1 px-2",
            )}
          >
            {folded ? (
              <Tooltip content="展开侧边栏" shortcut={`${modKeyLabel()}\\`} side="right">
                <IconButton
                  icon="panel-left-open"
                  size={28}
                  variant="ghost"
                  aria-label="展开侧边栏"
                  onClick={() => void toggleNavCollapsed(false)}
                  className="app-no-drag"
                />
              </Tooltip>
            ) : (
              <>
                <span className="w-6 shrink-0" aria-hidden="true" />
                <div className="flex min-w-0 flex-1 items-center justify-center gap-2">
                  <Tooltip content="打开 GitHub 仓库" side="right">
                    <button
                      type="button"
                      data-testid="brand-repo-btn"
                      aria-label="打开 GitHub 仓库"
                      onClick={() => openExternalUrl(GITHUB_REPO_URL)}
                      className="app-no-drag inline-flex shrink-0 cursor-pointer items-center justify-center rounded-[5px] transition-transform duration-150 ease-out hover:scale-105 active:scale-95"
                    >
                      <BrandLogo size={18} />
                    </button>
                  </Tooltip>
                  <div className="flex min-w-0 flex-col leading-tight">
                    <span className="truncate text-xs font-semibold text-ink">图来</span>
                    <span className="tnum truncate text-[10px] text-muted">
                      {s.version ? `v${s.version}` : "工作台"}
                    </span>
                  </div>
                </div>
                <Tooltip content="收起侧边栏" shortcut={`${modKeyLabel()}\\`} side="right">
                  <IconButton
                    icon="panel-left-close"
                    size={24}
                    variant="ghost"
                    aria-label="收起侧边栏"
                    onClick={() => void toggleNavCollapsed(true)}
                    className="app-no-drag"
                  />
                </Tooltip>
              </>
            )}
          </div>

          {/* Grouped Navigation */}
          <nav
            aria-label="主导航"
            className={cn(
              "flex min-h-0 flex-1 flex-col justify-between gap-2 overflow-y-auto pt-1 pb-2.5",
              folded ? "items-center px-1.5" : "px-2",
            )}
          >
            <TooltipGroup className={cn("flex flex-col gap-1", folded ? "items-center" : "items-stretch")}>
              {!folded ? (
                <div className="px-2 pt-0.5 pb-0.5 text-[10px] font-semibold tracking-wider text-subtle">
                  工作区
                </div>
              ) : null}

              {/* Workbench item + provider submenu */}
              {folded ? (
                <RailNavButton
                  nav={WORKBENCH_NAV}
                  active={isWorkbenchTab(tab)}
                  folded
                  onClick={() => void goTab(getLastWorkbenchTab())}
                />
              ) : (
                <>
                  <div
                    className={cn(
                      "flex items-center rounded-[10px] border transition-colors duration-150",
                      isWorkbenchTab(tab)
                        ? "border-[#DFE3EA] bg-rail-active shadow-[0_1px_2px_rgba(15,23,42,0.04)] dark:border-[#263147] dark:shadow-none"
                        : "border-transparent bg-transparent hover:bg-rail-hover",
                    )}
                  >
                    <button
                      type="button"
                      data-testid="nav-workbench"
                      aria-current={isWorkbenchTab(tab) ? "page" : undefined}
                      onClick={() => {
                        clearHoverPreview();
                        void goTab(getLastWorkbenchTab());
                        if (!providersOpen) setProvidersOpen(true);
                      }}
                      className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 py-1.5 pr-1 pl-2 text-left select-none"
                    >
                      {isWorkbenchTab(tab) ? (
                        <span className="animate-tulai-indicator-in -ml-0.5 h-3.5 w-[3px] shrink-0 rounded-full bg-accent" />
                      ) : null}
                      <Icon
                        name={WORKBENCH_NAV.icon}
                        size={15}
                        className={isWorkbenchTab(tab) ? "text-accent" : "text-muted"}
                      />
                      <span
                        className={cn(
                          "min-w-0 flex-1 truncate text-xs",
                          isWorkbenchTab(tab)
                            ? "font-semibold text-ink"
                            : "font-medium text-muted",
                        )}
                      >
                        {WORKBENCH_NAV.label}
                      </span>
                    </button>
                    <button
                      type="button"
                      data-testid="nav-workbench-toggle"
                      aria-label={providersOpen ? "收起平台列表" : "展开平台列表"}
                      aria-expanded={providersOpen}
                      onClick={() => setProvidersOpen((v) => !v)}
                      className="flex shrink-0 cursor-pointer items-center justify-center rounded-[8px] p-1.5 text-muted transition-colors hover:bg-rail-hover hover:text-ink"
                    >
                      <Icon
                        name="chevron-down"
                        size={12}
                        className={cn(
                          "transition-transform duration-180 ease-[cubic-bezier(0.16,1,0.3,1)]",
                          !providersOpen && "-rotate-90",
                        )}
                      />
                    </button>
                  </div>

                  {providersOpen ? (
                    <div
                      data-testid="nav-platform-submenu"
                      className="animate-tulai-banner-in mt-0.5 ml-3.5 flex flex-col gap-0.5 border-l border-[#D8DDE6] pl-2 dark:border-[#1E273B]"
                    >
                      {navPlatforms.map((p) => {
                        const name = p.name || "";
                        const markSrc = PROVIDER_MARKS[p.id ?? ""] || PROVIDER_MARKS[name];
                        const isSelected = activePlatform === name;
                        const isActive = isWorkbenchTab(tab) && isSelected;
                        const locked = Boolean(s.running) && !isSelected;
                        return (
                          <button
                            key={p.id || name}
                            type="button"
                            data-testid={`nav-platform-${p.id || ""}`}
                            disabled={locked}
                            onClick={() => {
                              if (locked || !name) return;
                              clearHoverPreview();
                              void openPlatformWorkbench(name);
                            }}
                            className={cn(
                              "flex w-full items-center gap-1.5 rounded-[8px] px-2 py-1 text-left transition-colors duration-150 select-none",
                              locked ? "cursor-not-allowed opacity-50" : "cursor-pointer",
                              isActive
                                ? "bg-rail-active"
                                : !locked
                                  ? "bg-transparent hover:bg-rail-hover"
                                  : "bg-transparent",
                            )}
                          >
                            {markSrc ? (
                              <img
                                src={markSrc}
                                alt=""
                                draggable={false}
                                className="size-3.5 shrink-0 object-contain"
                              />
                            ) : (
                              <span
                                className={cn(
                                  "size-1.5 shrink-0 rounded-full",
                                  isActive ? "bg-accent" : "bg-subtle",
                                )}
                              />
                            )}
                            <span
                              className={cn(
                                "min-w-0 flex-1 truncate text-[11px]",
                                isActive
                                  ? "font-semibold text-accent"
                                  : "font-medium text-muted",
                              )}
                            >
                              {name}
                            </span>
                            {p.total > 0 ? (
                              <span
                                className={cn(
                                  "tnum shrink-0 text-[10px]",
                                  p.busy > 0
                                    ? "font-semibold text-accent"
                                    : p.ready > 0
                                      ? "font-medium text-status-ok"
                                      : "text-subtle",
                                )}
                              >
                                {String(p.busy > 0 ? p.busy : p.ready)}
                              </span>
                            ) : null}
                          </button>
                        );
                      })}
                    </div>
                  ) : null}
                </>
              )}

              {CORE_NAV.map((item) => (
                <RailNavButton
                  key={item.id}
                  nav={item}
                  active={item.match(tab)}
                  folded={folded}
                />
              ))}

              <div className="my-1.5 h-px w-full bg-rail-border" />

              {!folded ? (
                <div className="px-2 pt-0.5 pb-0.5 text-[10px] font-semibold tracking-wider text-subtle">
                  工具箱
                </div>
              ) : null}

              {TOOL_NAV.map((item) => (
                <RailNavButton
                  key={item.id}
                  nav={item}
                  active={item.match(tab)}
                  folded={folded}
                />
              ))}
            </TooltipGroup>

            {/* Footer: Update banner + Log toggle + Settings */}
            <TooltipGroup
              className={cn(
                "flex flex-col gap-1 border-t border-rail-border pt-2",
                folded ? "w-full items-center" : "items-stretch",
              )}
            >
              {showSidebarUpdate ? (() => {
                const card = (
                  <div
                    role="button"
                    tabIndex={0}
                    data-testid="sidebar-update-card"
                    onClick={openUpdateSettings}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        openUpdateSettings();
                      }
                    }}
                    className={cn(
                      "animate-tulai-banner-in mb-1 flex cursor-pointer flex-col gap-1.5 rounded-[12px] border p-2 transition-colors",
                      updateProg.tone === "warn"
                        ? "border-[var(--status-warn-border)] bg-[var(--status-warn-bg)] hover:opacity-90"
                        : updateProg.tone === "success"
                          ? "border-[var(--status-ok-border)] bg-[var(--status-ok-bg)] hover:opacity-90"
                          : "border-accent-border bg-accent-soft hover:bg-accent-border/35",
                      folded && "w-full items-center px-1.5 py-1.5",
                    )}
                  >
                    {updateProg.active ? (
                      folded ? (
                        <>
                          <span
                            className={cn(
                              "tnum text-[10px] leading-none font-semibold",
                              updateProg.tone === "warn"
                                ? "text-status-warn"
                                : updateProg.tone === "success"
                                  ? "text-status-ok"
                                  : "text-accent",
                            )}
                          >
                            {updateProg.compactPercent}
                          </span>
                          <Progress
                            size="xs"
                            value={updateProg.fraction}
                            indeterminate={updateProg.indeterminate}
                            active={updateProg.shimmerActive}
                            tone={updateProg.tone}
                            aria-label="侧边栏更新进度"
                          />
                        </>
                      ) : (
                        <>
                          <div className="flex items-center justify-between gap-1">
                            <span
                              className={cn(
                                "min-w-0 truncate text-[11px] font-medium",
                                updateProg.tone === "warn"
                                  ? "text-status-warn"
                                  : updateProg.tone === "success"
                                    ? "text-status-ok"
                                    : "text-accent",
                              )}
                            >
                              {updateProg.phase === "deferred"
                                ? "等待更新"
                                : s.update.version
                                  ? `更新 v${s.update.version}`
                                  : "正在更新"}
                            </span>
                            <span
                              data-testid="sidebar-update-percent"
                              className={cn(
                                "tnum shrink-0 text-[10px] font-semibold",
                                updateProg.tone === "warn"
                                  ? "text-status-warn"
                                  : updateProg.tone === "success"
                                    ? "text-status-ok"
                                    : "text-accent",
                              )}
                            >
                              {updateProg.compactPercent}
                            </span>
                          </div>
                          <Progress
                            data-testid="sidebar-update-progress"
                            size="xs"
                            value={updateProg.fraction}
                            indeterminate={updateProg.indeterminate}
                            active={updateProg.shimmerActive}
                            tone={updateProg.tone}
                            aria-label="侧边栏更新进度"
                          />
                          <span className="tnum truncate text-[10px] text-muted">
                            {updateProg.speedText
                              ? updateProg.etaText
                                ? `${updateProg.speedText} · ${updateProg.etaText}`
                                : updateProg.speedText
                              : updateProg.stalled
                                ? "等待响应中…"
                                : updateProg.stageLabel}
                          </span>
                        </>
                      )
                    ) : (
                      <>
                        <span className="tnum text-[11px] font-medium text-accent">
                          {folded ? "更新" : `新版本 v${s.update.version}`}
                        </span>
                        {!folded ? (
                          <div className="flex items-center gap-1">
                            <Button
                              text="查看"
                              size="xs"
                              variant="ghost"
                              onClick={(e) => {
                                e.stopPropagation();
                                openUpdateSettings();
                              }}
                            />
                            <Button
                              text="忽略"
                              size="xs"
                              variant="ghost"
                              onClick={(e) => {
                                e.stopPropagation();
                                fireIpc(() => Core.updateAction("dismiss"));
                              }}
                            />
                          </div>
                        ) : null}
                      </>
                    )}
                  </div>
                );

                return folded ? (
                  <Tooltip content={sidebarUpdateTooltip} side="right">
                    {card}
                  </Tooltip>
                ) : (
                  card
                );
              })() : null}

              <SidebarLogButton
                open={Boolean(s.logOpen)}
                total={s.logTotal}
                errors={s.logErrors}
                folded={folded}
              />

              <RailNavButton
                nav={SETTINGS_NAV}
                active={SETTINGS_NAV.match(tab)}
                folded={folded}
              />
            </TooltipGroup>
          </nav>
        </aside>

        {/* Right Bento Main Canvas */}
        <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-paper">
          {/* Floating Workbench-only Topbar */}
          {isWorkbenchTab(tab) ? (
            <header data-testid="topbar" className="app-drag-region shrink-0 px-3 pt-2.5 pb-3">
              <div
                className="app-drag-region bento-shadow flex min-h-[42px] items-center justify-between gap-2.5 rounded-[16px] border border-hairline bg-card py-1.5 pl-3.5"
                style={{ paddingRight: "calc(14px + var(--mygo-titlebar-inset-right, 0px))" }}
              >
                <div className="app-no-drag flex min-w-0 items-center gap-2.5">
                  <Tabs
                    data-testid="wb-mode-tabs"
                    variant="segmented"
                    size="small"
                    options={WORKBENCH_MODE_OPTIONS}
                    value={String(tab)}
                    onValueChange={(value) => void goTab(Number(value))}
                  />
                  {folded ? (
                    <>
                      <div className="h-4 w-px bg-hairline" />
                      <Select
                        data-testid="wb-platform-select"
                        options={WORKBENCH_PLATFORM_OPTIONS}
                        value={activePlatform}
                        disabled={Boolean(s.running)}
                        size="small"
                        className="w-[120px]"
                        onValueChange={(value) => void selectWorkbenchPlatform(value)}
                      />
                    </>
                  ) : null}
                </div>
              </div>
            </header>
          ) : null}

          {/* Page Route Slot */}
          <div
            key={isWorkbenchTab(tab) ? "workbench" : String(tab)}
            className="animate-tulai-page-in relative flex min-h-0 flex-1 flex-col overflow-hidden"
          >
            {isWorkbenchTab(tab) ? <WorkbenchPage /> : null}
            {tab === TAB_DEDUP ? <DedupPage /> : null}
            {tab === TAB_BROWSER ? <BrowserPage /> : null}
            {tab === TAB_PROMPTS ? <PromptsPage /> : null}
            {tab === TAB_DOWNLOAD ? <DownloadPage /> : null}
            {tab === TAB_RENAME ? <RenamePage /> : null}
            {tab === TAB_IMAGE_EDIT ? <ImageEditPage /> : null}
            {tab === TAB_SETTINGS ? <SettingsPage /> : null}
          </div>
        </main>

        {/* Global Floating Overlays & Portals */}
        {browserDrawerOpen ? <BrowserDrawer /> : null}
        <LogDrawer />
        <WizardView />
        <HoverPreviewHost />
        <Toaster />
      </div>
    </TooltipProvider>
  );
}
