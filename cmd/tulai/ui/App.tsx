import { computed, mount, Text, View } from "nsgo";
import { Badge, Button, Icon, IconButton, Toasts } from "nsgo/ui";
import { updateAction } from "tulai/backend";
import { BrandLogo } from "tulai/native";
import { BrowserDrawer } from "./components/BrowserDrawer";
import { LogDrawer } from "./components/LogDrawer";
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

function RailNavButton(props: { nav: NavItem; active: boolean; folded: boolean }) {
  return (
    <View
      testID={`nav-${props.nav.id}`}
      onPress={() => goTab(props.nav.tab)}
      class="flex-row items-center gap-2 rounded-lg px-2 py-1.5"
      classList={{
        "bg-[#253047] dark:bg-[#1C2538]": props.active,
        "hover:bg-[#1B2333] dark:hover:bg-[#131926]": !props.active,
        "justify-center px-0 size-9": props.folded,
      }}
    >
      <Icon
        name={props.nav.icon}
        size={16}
        color={props.active ? "#60A5FA" : "#94A3B8"}
      />
      <If when={!props.folded}>
        <Text
          class="flex-1 truncate text-xs font-medium"
          classList={{
            "text-[#F8FAFC]": props.active,
            "text-[#94A3B8]": !props.active,
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

  return (
    <View
      testID="app-root"
      class="relative h-full w-full flex-row bg-[#F8FAFC] dark:bg-[#0D1118]"
    >
      {/* 深色可折叠侧边导航栏 */}
      <View
        testID="sidebar-rail"
        class="h-full border-r border-[#222B3C] bg-[#111622] py-2 dark:border-[#1A2232] dark:bg-[#090C12]"
        classList={{
          "w-[52px] items-center px-1.5": folded(),
          "w-[164px] px-2": !folded(),
        }}
      >
        {/* Brand header */}
        <View class="mb-2.5 flex-row items-center justify-between gap-1.5 px-0.5">
          <View class="flex-row items-center gap-2">
            <BrandLogo size={24} />
            <If when={!folded()}>
              <View>
                <Text class="text-sm font-bold text-[#F8FAFC]">图来 tulai</Text>
                <Text class="text-[10px] text-[#94A3B8]">
                  {s().version ? `v${s().version}` : "工作台"}
                </Text>
              </View>
            </If>
          </View>
          <If when={!folded()}>
            <IconButton
              icon="panel-left-close"
              size={24}
              variant="ghost"
              tint="#94A3B8"
              accessibilityLabel="收起侧边栏"
              onPress={() => toggleNavCollapsed(true)}
            />
          </If>
        </View>

        <If when={folded()}>
          <View class="mb-2">
            <IconButton
              icon="panel-left-open"
              size={26}
              variant="ghost"
              tint="#94A3B8"
              accessibilityLabel="展开侧边栏"
              onPress={() => toggleNavCollapsed(false)}
            />
          </View>
        </If>

        {/* 分组导航 */}
        <View class="min-h-0 flex-1 justify-between gap-2">
          <View class="gap-1">
            <Each of={CORE_NAV} key={(n) => n.id}>
              {(item) => (
                <RailNavButton
                  nav={item()}
                  active={item().match(tab())}
                  folded={folded()}
                />
              )}
            </Each>

            <View class="my-1.5 h-px bg-[#222B3C]" />

            <Each of={TOOL_NAV} key={(n) => n.id}>
              {(item) => (
                <RailNavButton
                  nav={item()}
                  active={item().match(tab())}
                  folded={folded()}
                />
              )}
            </Each>
          </View>

          <View class="gap-2 pt-2 border-t border-[#222B3C]">
            <If when={Boolean(s().update.visible && s().update.version)}>
              <View
                onPress={() => {
                  goTab(TAB_SETTINGS);
                  updateSettingField("tab", "3");
                }}
                class="gap-1 rounded-lg border border-blue-500/30 bg-blue-500/15 p-2"
              >
                <Text class="text-[11px] font-medium text-blue-300">
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
            />
          </View>
        </View>
      </View>

      {/* 右侧主区：统一顶栏 + 当前页面 */}
      <View class="min-w-0 min-h-0 flex-1">
        <View
          testID="topbar"
          class="h-[40px] flex-row items-center justify-between gap-3 border-b border-slate-200 bg-white px-3.5 dark:border-[#222B3C] dark:bg-[#131924]"
        >
          <View class="flex-row items-center gap-2.5">
            <Text testID="page-title" class="text-sm font-semibold text-slate-900 dark:text-slate-100">
              {pageTitle(tab())}
            </Text>
            <If when={Boolean(s().running)}>
              <Badge text={s().progText || `${Math.round(s().progress * 100)}%`} variant="info" />
            </If>
          </View>

          <View class="flex-row items-center gap-2">
            <If when={Boolean(s().pool.summary || s().pool.total > 0)}>
              <View
                onPress={() => goTab(TAB_BROWSER)}
                class="flex-row items-center gap-1.5 rounded-full border border-slate-200 bg-slate-50 px-2.5 py-1 dark:border-[#334155] dark:bg-[#1A2232]"
              >
                <Icon name="globe" size={12} color={s().pool.issue > 0 ? "#D97706" : "#16A34A"} />
                <Text class="text-[11px] text-slate-700 dark:text-slate-200">
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
