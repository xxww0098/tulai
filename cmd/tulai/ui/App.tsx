import { computed, mount, Text, View } from "nsgo";
import { Badge, Button, Icon, IconButton, Toasts } from "nsgo/ui";
import { updateAction } from "tulai/backend";
import { BrandLogo } from "tulai/native";
import { BrowserDrawer } from "./components/BrowserDrawer";
import { LogDrawer } from "./components/LogDrawer";
import { LivePulseDot } from "./components/MotionPrimitives";
import { BrowserPage } from "./pages/BrowserPage";
import { DedupPage } from "./pages/DedupPage";
import { DownloadPage } from "./pages/DownloadPage";
import { ImageEditPage } from "./pages/ImageEditPage";
import { PromptsPage } from "./pages/PromptsPage";
import { RenamePage } from "./pages/RenamePage";
import { SettingsPage } from "./pages/SettingsPage";
import { WizardView } from "./pages/WizardView";
import { WorkbenchPage } from "./pages/WorkbenchPage";
import {
  type NavItem,
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
  browserDrawerOpen,
  goTab,
  initStore,
  isWorkbenchTab,
  setToastsRef,
  shell,
  toggleLogDrawer,
  toggleNavCollapsed,
  updateSettingField,
} from "./store";

const CORE_NAV: NavItem[] = [
  { id: "workbench", tab: TAB_WASH, label: "图片生成", icon: "sparkles", match: (t) => t <= TAB_FISSION },
  { id: "browser", tab: TAB_BROWSER, label: "浏览器", icon: "globe", match: (t) => t === TAB_BROWSER },
  { id: "prompts", tab: TAB_PROMPTS, label: "提示词", icon: "book-open", match: (t) => t === TAB_PROMPTS },
];

const TOOL_NAV: NavItem[] = [
  { id: "download", tab: TAB_DOWNLOAD, label: "下载", icon: "download", match: (t) => t === TAB_DOWNLOAD },
  { id: "dedup", tab: TAB_DEDUP, label: "去重", icon: "layers", match: (t) => t === TAB_DEDUP },
  { id: "rename", tab: TAB_RENAME, label: "批量重命名", icon: "pencil", match: (t) => t === TAB_RENAME },
  { id: "imageedit", tab: TAB_IMAGE_EDIT, label: "图片编辑", icon: "wand-sparkles", match: (t) => t === TAB_IMAGE_EDIT },
];

const SETTINGS_NAV: NavItem = {
  id: "settings",
  tab: TAB_SETTINGS,
  label: "设置",
  icon: "settings",
  match: (t) => t === TAB_SETTINGS,
};

function pageTitle(tab: number): string {
  switch (tab) {
    case TAB_GEN:
    case TAB_WASH:
    case TAB_FISSION:
      return "图片生成";
    case TAB_DEDUP:
      return "去重";
    case TAB_BROWSER:
      return "浏览器";
    case TAB_PROMPTS:
      return "提示词";
    case TAB_DOWNLOAD:
      return "下载";
    case TAB_RENAME:
      return "批量重命名";
    case TAB_IMAGE_EDIT:
      return "图片编辑";
    case TAB_SETTINGS:
      return "设置";
    default:
      return "图来";
  }
}

function RailNavButton(props: { nav: NavItem; active: boolean; folded: boolean; dark: boolean }) {
  const iconColor = () =>
    props.active
      ? props.dark
        ? "#60A5FA"
        : "#2563EB"
      : props.dark
        ? "#94A3B8"
        : "#64748B";

  return (
    <View
      testID={`nav-${props.nav.id}`}
      onPress={() => goTab(props.nav.tab)}
      style={{ cursor: "pointer" }}
      pressedStyle={{ transform: [{ scale: 0.98 }] }}
      class="flex-row items-center gap-2 rounded-lg border px-2.5 py-1.5 transition duration-150"
      classList={{
        "border-slate-200/90 bg-white dark:border-[#263248] dark:bg-[#161E2E]": props.active,
        "border-transparent hover:bg-slate-200/60 dark:hover:bg-[#131A27]": !props.active,
        "justify-center px-0 size-9": props.folded,
      }}
    >
      <Icon
        name={props.nav.icon}
        size={15}
        color={iconColor()}
      />
      <If when={!props.folded}>
        <Text
          class="flex-1 truncate text-xs font-medium"
          classList={{
            "font-semibold text-slate-900 dark:text-slate-100": props.active,
            "text-slate-600 dark:text-slate-400": !props.active,
          }}
        >
          {props.nav.label}
        </Text>
      </If>
    </View>
  );
}

export function App() {
  initStore();
  const s = shell;
  const tab = computed(() => s().tab);
  const folded = computed(() => Boolean(s().navCollapsed));
  const isDark = computed(() => Boolean(s().dark));

  return (
    <View
      testID="app-root"
      class="relative h-full w-full flex-row bg-[#F8FAFC] dark:bg-[#0B0F17]"
    >
      {/* 现代 SaaS 侧边导航栏 */}
      <View
        testID="sidebar-rail"
        class="h-full shrink-0 border-r border-slate-200/80 bg-[#F6F8FA] dark:border-[#1E2738] dark:bg-[#0B0F17]"
        classList={{
          "w-[48px]": folded(),
          "w-[148px]": !folded(),
        }}
      >
        {/* Brand header (aligned flush with 36px topbar) */}
        <View
          class="h-[36px] flex-row items-center border-b border-slate-200/70 dark:border-[#1E2738]"
          classList={{
            "justify-center px-1": folded(),
            "justify-between gap-1 px-2.5": !folded(),
          }}
        >
          <If
            when={!folded()}
            else={
              <IconButton
                icon="panel-left-open"
                size={24}
                variant="ghost"
                tint={isDark() ? "#94A3B8" : "#64748B"}
                accessibilityLabel="展开侧边栏"
                onPress={() => toggleNavCollapsed(false)}
              />
            }
          >
            <View class="min-w-0 flex-row items-center gap-1.5">
              <BrandLogo size={20} />
              <View class="min-w-0">
                <Text class="truncate text-xs font-semibold text-slate-900 dark:text-slate-100">
                  图来 tulai
                </Text>
                <Text
                  class="truncate text-[10px] text-slate-400 dark:text-slate-500"
                  style={{ fontVariant: ["tabular-nums"] }}
                >
                  {s().version ? `v${s().version}` : "工作台"}
                </Text>
              </View>
            </View>
            <IconButton
              icon="panel-left-close"
              size={22}
              variant="ghost"
              tint={isDark() ? "#94A3B8" : "#64748B"}
              accessibilityLabel="收起侧边栏"
              onPress={() => toggleNavCollapsed(true)}
            />
          </If>
        </View>

        {/* 分组导航 */}
        <View class="min-h-0 flex-1 justify-between gap-1.5 px-1.5 py-2" classList={{ "items-center px-1": folded() }}>
          <View class="gap-0.5">
            <If when={!folded()}>
              <Text class="px-2 pt-0.5 pb-0.5 text-[10px] font-semibold tracking-wider text-slate-400 dark:text-slate-500">
                工作区
              </Text>
            </If>
            <Each of={CORE_NAV} key={(n) => n.id}>
              {(item) => (
                <RailNavButton
                  nav={item()}
                  active={item().match(tab())}
                  folded={folded()}
                  dark={isDark()}
                />
              )}
            </Each>

            <View class="my-1.5 h-px bg-slate-200/80 dark:bg-[#1E2738]" />

            <If when={!folded()}>
              <Text class="px-2 pt-0.5 pb-0.5 text-[10px] font-semibold tracking-wider text-slate-400 dark:text-slate-500">
                工具箱
              </Text>
            </If>
            <Each of={TOOL_NAV} key={(n) => n.id}>
              {(item) => (
                <RailNavButton
                  nav={item()}
                  active={item().match(tab())}
                  folded={folded()}
                  dark={isDark()}
                />
              )}
            </Each>
          </View>

          <View class="gap-2 border-t border-slate-200/80 pt-2 dark:border-[#1E2738]">
            <If when={Boolean(s().update.visible && s().update.version)}>
              <View
                onPress={() => {
                  goTab(TAB_SETTINGS);
                  updateSettingField("tab", "3");
                }}
                style={{ cursor: "pointer" }}
                class="gap-1 rounded-lg border border-blue-200 bg-blue-50/90 p-2 dark:border-blue-500/30 dark:bg-blue-500/15"
              >
                <Text class="text-[11px] font-medium text-blue-700 dark:text-blue-300">
                  {folded() ? "更新" : `新版本 v${s().update.version}`}
                </Text>
                <If when={!folded()}>
                  <View class="flex-row gap-1">
                    <Button
                      text="查看"
                      size="small"
                      variant="ghost"
                      onPress={() => {
                        goTab(TAB_SETTINGS);
                        updateSettingField("tab", "3");
                      }}
                    />
                    <Button
                      text="忽略"
                      size="small"
                      variant="ghost"
                      onPress={() => updateAction("dismiss")}
                    />
                  </View>
                </If>
              </View>
            </If>

            <RailNavButton
              nav={SETTINGS_NAV}
              active={SETTINGS_NAV.match(tab())}
              folded={folded()}
              dark={isDark()}
            />
          </View>
        </View>
      </View>

      {/* 右侧主区：统一顶栏 + 当前页面 */}
      <View class="min-w-0 min-h-0 flex-1">
        <View
          testID="topbar"
          class="h-[36px] shrink-0 flex-row items-center justify-between gap-2.5 border-b border-slate-200/80 bg-white px-3 dark:border-[#1E2738] dark:bg-[#0F141F]"
        >
          <View class="min-w-0 flex-row items-center gap-2.5">
            <Text testID="page-title" class="truncate text-sm font-semibold text-slate-900 dark:text-slate-100">
              {pageTitle(tab())}
            </Text>
            <If when={Boolean(s().running)}>
              <View class="flex-row items-center gap-1.5">
                <LivePulseDot color="blue" size={7} />
                <Badge text={s().progText || `${Math.round(s().progress * 100)}%`} variant="accent" />
              </View>
            </If>
          </View>

          <View class="shrink-0 flex-row items-center gap-2">
            <If when={Boolean(s().pool.summary || s().pool.total > 0)}>
              <View
                onPress={() => goTab(TAB_BROWSER)}
                style={{ cursor: "pointer" }}
                class="flex-row items-center gap-1.5 rounded-md px-2 py-1 transition-colors duration-150 hover:bg-slate-100 dark:hover:bg-[#161E2E]"
              >
                <If
                  when={Boolean(s().running || s().pool.busy > 0)}
                  else={
                    <If
                      when={s().pool.issue > 0}
                      else={
                        <View
                          class="size-2 rounded-full"
                          classList={{
                            "bg-emerald-500": s().pool.ready > 0,
                            "bg-slate-400": s().pool.ready === 0,
                          }}
                        />
                      }
                    >
                      <LivePulseDot color="amber" size={8} />
                    </If>
                  }
                >
                  <LivePulseDot color="blue" size={8} />
                </If>
                <Text
                  class="text-[11px] font-medium text-slate-600 dark:text-slate-300"
                  style={{ fontVariant: ["tabular-nums"] }}
                >
                  {s().pool.summary || `账号 ${s().pool.ready}/${s().pool.total}`}
                </Text>
              </View>
            </If>

            <Button
              testID="topbar-log-btn"
              text={
                s().logErrors > 0
                  ? `日志 (${s().logErrors})`
                  : s().logTotal > 0
                    ? `日志 (${s().logTotal})`
                    : "日志"
              }
              size="small"
              icon="terminal"
              variant={s().logErrors > 0 ? "outline" : "ghost"}
              onPress={() => toggleLogDrawer(!s().logOpen)}
            />
          </View>
        </View>

        {/* 页面内容路由 */}
        <View class="min-h-0 flex-1">
          <If when={isWorkbenchTab(tab())}>
            <WorkbenchPage />
          </If>
          <If when={tab() === TAB_DEDUP}>
            <DedupPage />
          </If>
          <If when={tab() === TAB_BROWSER}>
            <BrowserPage />
          </If>
          <If when={tab() === TAB_PROMPTS}>
            <PromptsPage />
          </If>
          <If when={tab() === TAB_DOWNLOAD}>
            <DownloadPage />
          </If>
          <If when={tab() === TAB_RENAME}>
            <RenamePage />
          </If>
          <If when={tab() === TAB_IMAGE_EDIT}>
            <ImageEditPage />
          </If>
          <If when={tab() === TAB_SETTINGS}>
            <SettingsPage />
          </If>
        </View>
      </View>

      <If when={browserDrawerOpen()}>
        <BrowserDrawer />
      </If>

      {/* 弹窗与提示 */}
      <LogDrawer />
      <WizardView />
      <Toasts ref={(r) => setToastsRef(r)} placement="bottomEnd" />
    </View>
  );
}

mount(App);
