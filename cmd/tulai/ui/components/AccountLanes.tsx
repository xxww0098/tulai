import { computed, Image, List, signal, Text, View } from "nsgo";
import { Badge, Button, IconButton, Input, Progress, Spinner, Switch } from "nsgo/ui";
import {
  type AccountLaneDTO,
  agyAction,
  browserRowAction,
  createBrowser,
  openBrowserDrawer,
  refreshBrowsers,
  setPlatformAccountsDisabled,
  setWorkbenchField,
} from "tulai/backend";
import { OutlineBadge } from "tulai/native";
import { goTab, TAB_BROWSER, workbench } from "../store";
import { AccountListFrame, AccountViewButton } from "./BrowserDrawer";
import { LivePulseDot } from "./MotionPrimitives";

function laneTone(state?: string): "success" | "warn" | "danger" | "accent" | "neutral" {
  switch (state) {
    case "出图中":
      return "accent";
    case "空闲":
      return "success";
    case "等待间隔":
    case "冷却中":
    case "需要登录":
    case "验证码":
      return "warn";
    case "今日已满":
    case "额度不足":
    case "离线":
      return "danger";
    case "已禁用":
    default:
      return "neutral";
  }
}

function formatJobOrder(taskIndex: number): string {
  const num = taskIndex > 0 ? taskIndex : 1;
  return `#${String(num).padStart(2, "0")}`;
}

function laneStateText(ln: AccountLaneDTO): string {
  switch (ln.state) {
    case "出图中":
      return ln.hasJob ? `出图中 ${formatJobOrder(ln.job.taskIndex)}` : "出图中";
    case "空闲":
      return ln.readyInSec > 0 ? `${ln.readyInSec}s 后就绪` : "空闲就绪";
    case "等待间隔":
      return ln.readyInSec > 0 ? `间隔 ${ln.readyInSec}s` : "等待间隔";
    case "冷却中":
      return ln.cooldownAt ? `冷却至 ${ln.cooldownAt}` : "冷却中";
    default:
      return ln.state || "未知";
  }
}

function AccountLaneRow(props: { lane: AccountLaneDTO }) {
  const lane = () => props.lane;
  const showClearCool = () =>
    lane().state === "冷却中" ||
    lane().state === "今日已满" ||
    lane().state === "额度不足" ||
    Boolean(lane().lastError);
  const needsLogin = () => lane().state === "需要登录" || !lane().isLoggedIn;
  const canView = () => !lane().extension && lane().platform !== "Antigravity";
  return (
    <View
      class="gap-1 border-b border-[#E5E9F0] px-2.5 py-1.5 transition-colors duration-150 dark:border-[#222B3C]"
      classList={{
        "bg-white hover:bg-[#F1F5F9]/70 dark:bg-[#131924] dark:hover:bg-[#1A2232]":
          !lane().disabled,
        "bg-[#F1F5F9]/80 opacity-75 dark:bg-[#0F141D]":
          Boolean(lane().disabled),
      }}
    >
      {/* Primary Horizontal Row */}
      <View class="flex-row flex-wrap items-center justify-between gap-2">
        <View class="min-w-0 flex-1 flex-row flex-wrap items-center gap-2">
          <Switch
            testID={`lane-toggle-${lane().id}`}
            size="small"
            checked={!lane().disabled}
            onChange={() => browserRowAction(lane().id, "toggleDisabled")}
          />
          <If when={canView()}>
            <AccountViewButton accountID={lane().id} name={lane().title} />
          </If>
          <Text
            class="max-w-[180px] truncate text-xs font-medium"
            classList={{
              "text-slate-900 dark:text-slate-100": !lane().disabled,
              "text-slate-400 line-through dark:text-slate-500": Boolean(lane().disabled),
            }}
          >
            {lane().title || `账号 #${lane().id}`}
          </Text>
          <If when={lane().engine === "Chrome CDP"}>
            <OutlineBadge text="Chrome CDP" />
          </If>
          <Badge text={laneStateText(lane())} variant={laneTone(lane().state)} />
          <If when={Boolean(lane().disabled && lane().state === "出图中")}>
            <Text class="text-[11px] text-amber-600 dark:text-amber-400">
              本单完成后停用
            </Text>
          </If>
          <Text
            class="text-[11px] text-slate-500 dark:text-slate-400"
            style={{ fontVariant: ["tabular-nums"] }}
          >
            {lane().usageLabel
              ? lane().usageLabel
              : lane().cap > 0
                ? `今日 ${lane().usedToday}/${lane().cap}（成 ${lane().successToday}）`
                : `今日 ${lane().usedToday} 次（成 ${lane().successToday}）`}
          </Text>
          <If when={lane().usagePct > 0 || lane().cap > 0}>
            <View class="w-[64px]">
              <Progress
                value={
                  lane().usagePct > 0
                    ? Math.min(1, lane().usagePct / 100)
                    : lane().cap > 0
                      ? Math.min(1, lane().usedToday / lane().cap)
                      : 0
                }
              />
            </View>
          </If>
          <If when={Boolean(lane().hasJob)}>
            <View class="flex-row items-center gap-1.5">
              <If when={Boolean(lane().job.sourceURL)}>
                <View class="size-5 shrink-0 overflow-hidden rounded border border-blue-400 bg-slate-100 animate-pulse dark:border-blue-500/60 dark:bg-[#0B0F17]">
                  <Image
                    src={lane().job.sourceURL}
                    fadeDuration={150}
                    class="h-full w-full object-cover"
                  />
                </View>
              </If>
              <Text
                class="truncate text-[11px] font-medium text-blue-600 animate-pulse dark:text-blue-300"
                style={{ fontVariant: ["tabular-nums"] }}
              >
                {`${formatJobOrder(lane().job.taskIndex)} · ${lane().job.elapsed || lane().job.status || "加工中"}`}
              </Text>
            </View>
          </If>
        </View>

        {/* Quick Actions */}
        <View class="shrink-0 flex-row flex-wrap items-center gap-1">
          <If
            when={lane().state === "验证码" && canView()}
            else={
              <If
                when={lane().platform === "Antigravity"}
                else={
                  <View class="flex-row items-center gap-1">
                    <If
                      when={needsLogin()}
                      else={
                        <If
                          when={Boolean(lane().isRunning)}
                          else={
                            <Button
                              text="打开"
                              size="small"
                              variant="ghost"
                              onPress={() => browserRowAction(lane().id, "launch")}
                            />
                          }
                        >
                          <Button
                            text="停止"
                            size="small"
                            variant="ghost"
                            onPress={() => browserRowAction(lane().id, "stop")}
                          />
                        </If>
                      }
                    >
                      <Button
                        text="打开登录"
                        size="small"
                        variant="outline"
                        onPress={() => browserRowAction(lane().id, "launch")}
                      />
                    </If>
                  </View>
                }
              >
                <Button
                  text="打开"
                  size="small"
                  variant="ghost"
                  onPress={() => browserRowAction(lane().id, "launch")}
                />
                <Button
                  text="查额度"
                  size="small"
                  variant="ghost"
                  onPress={() => browserRowAction(lane().id, "refreshUsage")}
                />
              </If>
            }
          >
            <Button
              text="去验证"
              size="small"
              variant="primary"
              onPress={() => openBrowserDrawer(lane().id)}
            />
          </If>

          <If when={showClearCool()}>
            <Button
              text="清除冷却"
              size="small"
              variant="outline"
              onPress={() => browserRowAction(lane().id, "restore")}
            />
          </If>

          <If when={!workbench().running}>
            <Button
              text="删除"
              size="small"
              variant="ghost"
              onPress={() => browserRowAction(lane().id, "delete")}
            />
          </If>
        </View>
      </View>

      <If when={Boolean(lane().usageErr || lane().lastError)}>
        <Text class="truncate pl-9 text-[11px] text-amber-600 dark:text-amber-400">
          {lane().usageErr || lane().lastError}
        </Text>
      </If>
    </View>
  );
}

function CollapsedLanePill(props: { lane: AccountLaneDTO }) {
  const lane = () => props.lane;
  const isWorking = () => !lane().disabled && lane().state === "出图中";
  const dotClass = () =>
    lane().disabled
      ? "bg-slate-400"
      : lane().state === "空闲"
        ? "bg-emerald-500"
        : "bg-amber-500";
  return (
    <View
      testID={`lane-pill-${lane().id}`}
      onPress={() => browserRowAction(lane().id, "toggleDisabled")}
      style={{ cursor: "pointer" }}
      pressedStyle={{ transform: [{ scale: 0.97 }] }}
      class="flex-row items-center gap-1.5 rounded-full border px-2 py-0.5 transition duration-150"
      classList={{
        "border-blue-300/90 bg-blue-50/80 hover:bg-blue-100/80 dark:border-blue-500/45 dark:bg-blue-500/15":
          isWorking(),
        "border-slate-200/90 bg-slate-50/80 hover:bg-slate-100 dark:border-[#263248] dark:bg-[#161E2E] dark:hover:bg-[#1C2639]":
          !lane().disabled && !isWorking(),
        "border-slate-200/60 bg-slate-100/60 opacity-70 hover:bg-slate-200/60 dark:border-[#1E2738] dark:bg-[#0F141D]":
          Boolean(lane().disabled),
      }}
    >
      <If when={isWorking()} else={<View class={`size-1.5 rounded-full ${dotClass()}`} />}>
        <View class="flex-row items-center gap-1">
          <LivePulseDot color="blue" size={6} />
          <Spinner size={9} tint="#2563EB" />
        </View>
      </If>
      <Text
        class="text-[11px] font-medium"
        classList={{
          "text-slate-800 dark:text-slate-100": !lane().disabled,
          "text-slate-400 line-through dark:text-slate-500": Boolean(lane().disabled),
        }}
      >
        {lane().title || `账号 #${lane().id}`}
      </Text>
      <If when={isWorking() && Boolean(lane().hasJob)}>
        <Text
          class="text-[10px] font-semibold text-blue-700 animate-pulse dark:text-blue-300"
          style={{ fontVariant: ["tabular-nums"] }}
        >
          {`${formatJobOrder(lane().job.taskIndex)}${lane().job.elapsed ? ` ${lane().job.elapsed}` : ""}`}
        </Text>
      </If>
      <If when={Boolean(lane().disabled)}>
        <Text class="text-[10px] text-slate-400 dark:text-slate-500">已停用</Text>
      </If>
    </View>
  );
}

export function AccountLanes() {
  const wb = workbench;
  const adding = signal(false);
  const newName = signal("");

  const platform = computed(() =>
    wb().qianwen ? "千问" : wb().chatGPT ? "ChatGPT" : wb().agy ? "Antigravity" : "豆包",
  );

  const platformLanes = computed(() => (wb().lanes ?? []).filter((l) => l.platform === platform()));
  const enabledCount = computed(() => platformLanes().filter((l) => !l.disabled).length);
  const disabledCount = () => platformLanes().length - enabledCount();
  const readyCount = computed(() => platformLanes().filter((l) => l.state === "空闲").length);
  const collapsed = () => Boolean(wb().laneCollapsed);
  const inlineCollapsedPills = () =>
    collapsed() && platformLanes().length > 0 && platformLanes().length <= 4;

  const openAdd = () => {
    if (wb().running) return;
    if (platform() === "Antigravity") {
      goTab(TAB_BROWSER);
      void agyAction("openChooser", "");
      return;
    }
    newName.set("");
    adding.set(!adding());
  };

  const submitCreate = () => {
    if (wb().running || platform() === "Antigravity") return;
    createBrowser(platform(), newName().trim());
    newName.set("");
    adding.set(false);
  };

  return (
    <View class="shrink-0 gap-1.5 border-b border-[#E5E9F0] bg-white px-3 py-1.5 dark:border-[#222B3C] dark:bg-[#131924]">
      {/* Header Row */}
      <View class="flex-row items-center justify-between gap-2">
        <View class="min-w-0 flex-1 flex-row flex-wrap items-center gap-2">
          <Text class="shrink-0 text-xs font-semibold text-slate-900 dark:text-slate-100">
            {`账号 · ${platform()}`}
          </Text>
          <Text
            class="shrink-0 text-[11px] text-slate-500 dark:text-slate-400"
            style={{ fontVariant: ["tabular-nums"] }}
          >
            {platformLanes().length === 0
              ? "暂无账号"
              : `已启用 ${enabledCount()} / ${platformLanes().length} · 就绪 ${readyCount()}${platform() === "Antigravity" && wb().agyPauseLine ? ` · ${wb().agyPauseLine}` : ""}`}
          </Text>
          <If when={inlineCollapsedPills()}>
            <View class="flex-row flex-wrap items-center gap-1.5">
              <Each of={platformLanes()} key={(l) => String(l.id)}>
                {(lane) => <CollapsedLanePill lane={lane()} />}
              </Each>
            </View>
          </If>
        </View>

        <View class="shrink-0 flex-row items-center gap-1">
          <If when={platformLanes().length > 0 && disabledCount() > 0}>
            <Button
              testID="lanes-enable-all"
              text="全部启用"
              size="small"
              variant="outline"
              onPress={() => setPlatformAccountsDisabled(platform(), false)}
            />
          </If>
          <If when={platformLanes().length > 0 && enabledCount() > 0}>
            <Button
              testID="lanes-disable-all"
              text="全部停用"
              size="small"
              variant="ghost"
              onPress={() => setPlatformAccountsDisabled(platform(), true)}
            />
          </If>
          <If when={!wb().running}>
            <Button
              text={platform() === "Antigravity" ? "导入/授权" : "新建账号"}
              size="small"
              icon="plus"
              variant="outline"
              onPress={openAdd}
            />
          </If>
          <IconButton
            icon="refresh-cw"
            size={24}
            variant="ghost"
            tooltip="刷新账号状态"
            accessibilityLabel="刷新账号"
            onPress={() => refreshBrowsers()}
          />
          <Button
            text="管理"
            size="small"
            variant="ghost"
            onPress={() => goTab(TAB_BROWSER)}
          />
          <IconButton
            icon={collapsed() ? "chevron-down" : "chevron-up"}
            variant="ghost"
            size={24}
            accessibilityLabel="折叠账号车道"
            onPress={() => setWorkbenchField("laneCollapsed", collapsed() ? "false" : "true")}
          />
        </View>
      </View>

      {/* Inline Quick-Add Bar */}
      <If when={adding() && !wb().running && platform() !== "Antigravity"}>
        <View class="flex-row flex-wrap items-center gap-2 rounded-lg border border-slate-200/80 bg-slate-50/70 px-2.5 py-2 dark:border-[#1E2738] dark:bg-[#161E2E]">
          <Input
            value={newName()}
            placeholder={`输入 ${platform()} 账号备注名（可留空自动命名）`}
            onChange={({ value }) => newName.set(value)}
            onSubmit={submitCreate}
            class="min-w-[180px] flex-1"
          />
          <Button text="创建并登录" size="small" variant="primary" onPress={submitCreate} />
          <Button text="取消" size="small" variant="ghost" onPress={() => adding.set(false)} />
        </View>
      </If>

      {/* Empty State */}
      <If when={!collapsed() && platformLanes().length === 0}>
        <View class="flex-row flex-wrap items-center justify-between gap-2 rounded-lg border border-dashed border-slate-200/90 bg-slate-50/70 px-3 py-2 dark:border-[#1E2738] dark:bg-[#161E2E]">
          <Text class="flex-1 text-xs text-slate-500 dark:text-slate-400">
            {`当前平台「${platform()}」暂无可用账号`}
          </Text>
          <Button
            text={platform() === "Antigravity" ? "导入 / 授权 Antigravity" : `新建 ${platform()} 账号`}
            size="small"
            icon="plus"
            variant="primary"
            disabled={Boolean(wb().running)}
            onPress={openAdd}
          />
        </View>
      </If>

      {/* Collapsed Quick-Toggle Pills (overflow row when > 4 accounts) */}
      <If when={collapsed() && platformLanes().length > 4}>
        <View class="flex-row flex-wrap gap-1.5">
          <Each of={platformLanes()} key={(l) => String(l.id)}>
            {(lane) => <CollapsedLanePill lane={lane()} />}
          </Each>
        </View>
      </If>

      {/* Account List */}
      <If when={!collapsed() && platformLanes().length > 0}>
        <AccountListFrame class="overflow-hidden rounded-lg border border-slate-200/80 dark:border-[#1E2738]">
          <View class="flex-row items-center justify-between border-b border-slate-200/80 bg-slate-50/80 px-2.5 py-1 dark:border-[#1E2738] dark:bg-[#161E2E]">
            <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
              账号 / 状态 / 今日用量
            </Text>
            <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
              操作
            </Text>
          </View>
          <List
            testID="wb-account-list"
            style={{ height: Math.min(Math.max(platformLanes().length, 1) * 40, 160) }}
            each={platformLanes()}
            keyExtractor={(l) => String(l.id)}
            estimatedItemSize={40}
          >
            {(lane) => <AccountLaneRow lane={lane()} />}
          </List>
        </AccountListFrame>
      </If>
    </View>
  );
}
