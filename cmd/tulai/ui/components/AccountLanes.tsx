import { computed, List, signal, Text, View } from "nsgo";
import { Badge, Button, IconButton, Input, Progress, Switch } from "nsgo/ui";
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
import { goTab, TAB_BROWSER, workbench } from "../store";
import { AccountListFrame, AccountViewButton } from "./BrowserDrawer";

function laneTone(state?: string): "up" | "warn" | "danger" | "info" | "neutral" {
  switch (state) {
    case "出图中":
      return "info";
    case "空闲":
      return "up";
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

function laneStateText(ln: AccountLaneDTO): string {
  switch (ln.state) {
    case "出图中":
      return ln.hasJob ? `出图中 #${ln.job.taskIndex + 1}` : "出图中";
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
      class="gap-1 border-b border-slate-200/70 px-2.5 py-1.5 dark:border-[#222B3C]"
      classList={{
        "bg-white hover:bg-slate-50/80 dark:bg-[#131924] dark:hover:bg-[#1A2232]":
          !lane().disabled,
        "bg-slate-50/70 opacity-75 dark:bg-[#0F141D]":
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
            <Badge text="Chrome CDP" variant="outline" />
          </If>
          <Badge text={laneStateText(lane())} variant={laneTone(lane().state)} />
          <If when={Boolean(lane().disabled && lane().state === "出图中")}>
            <Text class="text-[11px] text-amber-600 dark:text-amber-400">
              本单完成后停用
            </Text>
          </If>
          <Text class="text-[11px] text-slate-500 dark:text-slate-400">
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
            <Text class="truncate text-[11px] text-blue-600 dark:text-blue-300">
              {`${lane().job.status || "执行中"} ${lane().job.elapsed || ""}`.trim()}
            </Text>
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
    <View class="gap-2 rounded-xl border border-slate-200 bg-white px-3 py-2 dark:border-[#222B3C] dark:bg-[#131924]">
      {/* Header Row */}
      <View class="flex-row flex-wrap items-center justify-between gap-2">
        <View class="min-w-0 flex-1 flex-row flex-wrap items-center gap-2">
          <Text class="text-[13px] font-semibold text-slate-900 dark:text-slate-100">
            {`账号 · ${platform()}`}
          </Text>
          <Text class="truncate text-[11px] text-slate-500 dark:text-slate-400">
            {platformLanes().length === 0
              ? "暂无账号"
              : `已启用 ${enabledCount()} / ${platformLanes().length} · 就绪 ${readyCount()}${platform() === "Antigravity" && wb().agyPauseLine ? ` · ${wb().agyPauseLine}` : ""}`}
          </Text>
        </View>

        <View class="flex-row flex-wrap items-center gap-1">
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
          <Button
            text="刷新"
            size="small"
            icon="refresh-cw"
            variant="ghost"
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
            size={22}
            accessibilityLabel="折叠账号车道"
            onPress={() => setWorkbenchField("laneCollapsed", collapsed() ? "false" : "true")}
          />
        </View>
      </View>

      {/* Inline Quick-Add Bar */}
      <If when={adding() && !wb().running && platform() !== "Antigravity"}>
        <View class="flex-row flex-wrap items-center gap-2 rounded-lg border border-slate-200 bg-slate-50/70 px-2.5 py-2 dark:border-[#222B3C] dark:bg-[#1A2232]">
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
        <View class="flex-row flex-wrap items-center justify-between gap-2 rounded-lg border border-dashed border-slate-200 bg-slate-50/70 px-3 py-2 dark:border-[#222B3C] dark:bg-[#1A2232]">
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

      {/* Collapsed Quick-Toggle Pills */}
      <If when={collapsed() && platformLanes().length > 0}>
        <View class="flex-row flex-wrap gap-1.5">
          <Each of={platformLanes()} key={(l) => String(l.id)}>
            {(lane) => {
              const dotClass = () =>
                lane().disabled
                  ? "bg-slate-400"
                  : lane().state === "空闲" || lane().state === "出图中"
                    ? "bg-emerald-600"
                    : "bg-amber-600";
              return (
                <View
                  testID={`lane-pill-${lane().id}`}
                  onPress={() => browserRowAction(lane().id, "toggleDisabled")}
                  class="flex-row items-center gap-1.5 rounded-full border px-2.5 py-1"
                  classList={{
                    "border-slate-200 bg-slate-50 hover:bg-slate-100 dark:border-[#334155] dark:bg-[#1A2232] dark:hover:bg-[#232D42]":
                      !lane().disabled,
                    "border-slate-200/70 bg-slate-100/70 opacity-75 hover:bg-slate-200/70 dark:border-[#222B3C] dark:bg-[#131924]":
                      Boolean(lane().disabled),
                  }}
                >
                  <View class={`size-1.5 rounded-full ${dotClass()}`} />
                  <Text
                    class="text-[11px] font-medium"
                    classList={{
                      "text-slate-800 dark:text-slate-100": !lane().disabled,
                      "text-slate-400 line-through dark:text-slate-500": Boolean(lane().disabled),
                    }}
                  >
                    {lane().title || `账号 #${lane().id}`}
                  </Text>
                  <Text
                    class="text-[10px]"
                    classList={{
                      "text-emerald-700 dark:text-emerald-400": !lane().disabled,
                      "text-slate-400 dark:text-slate-500": Boolean(lane().disabled),
                    }}
                  >
                    {lane().disabled ? "已停用" : "启用"}
                  </Text>
                </View>
              );
            }}
          </Each>
        </View>
      </If>

      {/* Account List */}
      <If when={!collapsed() && platformLanes().length > 0}>
        <AccountListFrame class="overflow-hidden rounded-lg border border-slate-200 dark:border-[#222B3C]">
          <View class="flex-row items-center justify-between border-b border-slate-200 bg-slate-50 px-2.5 py-1 dark:border-[#222B3C] dark:bg-[#1A2232]">
            <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
              账号 / 状态 / 今日用量
            </Text>
            <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
              操作
            </Text>
          </View>
          <List
            testID="wb-account-list"
            viewport="parent"
            each={platformLanes()}
            keyExtractor={(l) => String(l.id)}
            estimatedItemSize={42}
          >
            {(lane) => <AccountLaneRow lane={lane()} />}
          </List>
        </AccountListFrame>
      </If>
    </View>
  );
}
