import { computed, List, signal, Text, View } from "nsgo";
import { Badge, Button, Checkbox, Dialog, Input, Progress, Select, Tabs } from "nsgo/ui";
import {
  type BrowserRowDTO,
  agyAction,
  browserRowAction,
  commitBrowserRename,
  copyText,
  createBrowser,
  installCamoufox,
  refreshBrowsers,
  regenerateExtensionToken,
  runBrowserBatch,
  saveAccountProxy,
  selectAccountEngine,
  toggleAllBrowserChecks,
  toggleBrowserCheck,
} from "tulai/backend";
import { OutlineBadge } from "tulai/native";
import { AccountListFrame, AccountViewButton } from "../components/BrowserDrawer";
import { EmptyNotice, PageToolbar, PaneHeader, StatusBadge } from "../components/primitives";
import { browser, showToast } from "../store";

const PLATFORM_OPTIONS = [
  { value: "豆包", label: "豆包" },
  { value: "千问", label: "千问" },
  { value: "ChatGPT", label: "ChatGPT" },
  { value: "Antigravity", label: "Antigravity" },
];

// A virtual list cell can display different accounts as it scrolls.
function accountDrafts() {
  const drafts = signal(new Map<number, string>());
  return {
    get: (id: number) => drafts().get(id),
    set(id: number, value?: string) {
      const next = new Map(drafts());
      if (value === undefined) next.delete(id);
      else next.set(id, value);
      drafts.set(next);
    },
  };
}

function AccountRow(props: {
  row: BrowserRowDTO;
  names: ReturnType<typeof accountDrafts>;
  proxies: ReturnType<typeof accountDrafts>;
}) {
  const r = computed(() => props.row);
  const draftName = () => props.names.get(r().id);
  const draftProxy = () => props.proxies.get(r().id);
  const saveName = () => {
    commitBrowserRename(r().id, draftName() ?? "");
    props.names.set(r().id);
  };
  const isAgy = () => r().platform === "Antigravity";
  const isExt = () => Boolean(r().extension);

  return (
    <View
      class="gap-1 border-b border-slate-200/70 px-2.5 py-2 dark:border-[#222B3C]"
      classList={{
        "bg-white hover:bg-slate-50/80 dark:bg-[#131924] dark:hover:bg-[#1A2232]": !r().disabled,
        "bg-slate-50/70 opacity-75 dark:bg-[#0F141D]": Boolean(r().disabled),
      }}
    >
      {/* 主列表行：身份、状态、用量与操作 */}
      <View class="flex-row flex-wrap items-center justify-between gap-2">
        <View class="min-w-0 flex-1 flex-row flex-wrap items-center gap-2">
          <Checkbox
            checked={Boolean(r().checked)}
            accessibilityLabel={`选择 ${r().name || r().platform}`}
            onChange={({ checked }) => toggleBrowserCheck(r().id, checked)}
          />
          <If when={!isAgy() && !isExt()}>
            <AccountViewButton accountID={r().id} name={r().name} />
          </If>
          <OutlineBadge text={r().platform || "未知"} />

          <If
            when={draftName() !== undefined}
            else={
              <View
                onPress={() => props.names.set(r().id, r().name || "")}
                class="flex-row items-center gap-1"
              >
                <Text class="text-xs font-semibold text-slate-900 dark:text-slate-100">
                  {r().name || `账号 #${r().id}`}
                </Text>
                <If when={Boolean(r().email)}>
                  <Text class="text-xs text-slate-500 dark:text-slate-400">{`(${r().email})`}</Text>
                </If>
              </View>
            }
          >
            <View class="w-[210px] flex-row items-center gap-1">
              <Input
                testID={`browser-name-${r().id}`}
                value={draftName() ?? ""}
                onChange={({ value }) => props.names.set(r().id, value)}
                onSubmit={saveName}
                class="flex-1"
              />
              <Button
                text="保存"
                size="small"
                variant="primary"
                onPress={saveName}
              />
            </View>
          </If>

          <If when={Boolean(r().engine)}>
            <Badge text={r().engine} variant="neutral" />
          </If>
          <StatusBadge text={r().health || "未知"} tone={r().healthTone} />
          <Text
            class="text-[11px] text-slate-500 dark:text-slate-400"
            style={{ fontVariant: ["tabular-nums"] }}
          >
            {`今日使用 ${r().todayCount} 次${r().usageLabel ? ` · ${r().usageLabel}` : ""}${r().proxy ? ` · 代理 ${r().proxy}` : ""}${r().pendingOp ? ` · ${r().pendingOp}` : ""}`}
          </Text>
          <If when={r().usagePct > 0}>
            <View class="w-[72px]">
              <Progress value={Math.min(1, r().usagePct / 100)} />
            </View>
          </If>
        </View>

        <View class="shrink-0 flex-row flex-wrap items-center gap-1">
          <If when={!isExt()}>
            <If
              when={Boolean(r().isRunning)}
              else={
                <Button
                  text={isAgy() ? "授权/验证" : "打开登录"}
                  size="small"
                  variant="primary"
                  disabled={Boolean(r().pending)}
                  onPress={() => browserRowAction(r().id, "launch")}
                />
              }
            >
              <Button
                text="停止窗口"
                size="small"
                variant="outline"
                onPress={() => browserRowAction(r().id, "stop")}
              />
            </If>
          </If>

          <If when={isAgy()}>
            <Button
              text="查额度"
              size="small"
              variant="outline"
              onPress={() => browserRowAction(r().id, "refreshUsage")}
            />
          </If>

          <If when={Boolean(r().lastError)}>
            <Button
              text="清除冷却"
              size="small"
              variant="outline"
              onPress={() => browserRowAction(r().id, "restore")}
            />
          </If>
          <Button
            text={r().disabled ? "启用" : "禁用"}
            size="small"
            variant="ghost"
            onPress={() => browserRowAction(r().id, "toggleDisabled")}
          />
          <Button
            text="代理"
            size="small"
            variant="ghost"
            onPress={() => props.proxies.set(r().id, draftProxy() === undefined ? r().proxy || "" : undefined)}
          />
          <Button
            text="删除"
            size="small"
            variant="ghost"
            onPress={() => browserRowAction(r().id, "delete")}
          />
        </View>
      </View>

      <If when={Boolean(r().lastError)}>
        <Text class="pl-6 text-[11px] text-amber-600 dark:text-amber-400">{r().lastError}</Text>
      </If>

      <If when={draftProxy() !== undefined}>
        <View class="flex-row items-center gap-2 pl-6 pt-1">
          <Input
            value={draftProxy() ?? ""}
            placeholder="专属代理（留空跟随全局设置），如 http://127.0.0.1:7890"
            onChange={({ value }) => props.proxies.set(r().id, value)}
            class="flex-1"
          />
          <Button
            text="保存代理"
            size="small"
            variant="primary"
            onPress={() => {
              void saveAccountProxy(r().id, draftProxy() ?? "");
              props.proxies.set(r().id);
            }}
          />
          <Button text="取消" size="small" variant="ghost" onPress={() => props.proxies.set(r().id)} />
        </View>
      </If>
    </View>
  );
}

export function BrowserPage() {
  const st = browser;
  const newPlatform = signal("豆包");
  const newName = signal("");
  const authCode = signal("");
  const filterPlat = signal("全部");
  const names = accountDrafts();
  const proxies = accountDrafts();

  const engineTabs = () =>
    (st().engines ?? []).map((eng) => ({
      value: eng,
      label: eng === "Chrome CDP" || eng === "chrome-cdp" ? "Chrome CDP" : "Camoufox (默认)",
    }));

  const rows = computed(() => {
    const list = st().rows ?? [];
    const f = filterPlat();
    if (f === "全部") return list;
    return list.filter((r) => r.platform === f);
  });

  return (
    <View class="min-h-0 flex-1">
      {/* 固定顶部托管浏览器与扩展连接条 */}
      <PageToolbar
        left={
          <View class="flex-row flex-wrap items-center gap-2">
            <If when={engineTabs().length > 0}>
              <Tabs
                variant="segmented"
                size="small"
                options={engineTabs()}
                value={st().backend || "Camoufox"}
                onChange={({ value }) => selectAccountEngine(value)}
              />
            </If>
            <StatusBadge text={st().badgeLabel || (st().ready ? "就绪" : "未就绪")} tone={st().badgeTone} />
            <If when={!st().ready && !st().busy}>
              <Button
                text={`下载安装 ${st().backendLabel || "Camoufox"}`}
                size="small"
                icon="download"
                variant="primary"
                onPress={() => installCamoufox()}
              />
            </If>
          </View>
        }
        right={
          <View class="flex-row flex-wrap items-center gap-1.5">
            <Text class="text-[11px] text-slate-500 dark:text-slate-400">
              {st().bridge.listening
                ? `扩展桥 ${st().bridge.addr || ""}`
                : st().bridge.err || "扩展桥未启动"}
            </Text>
            <If when={Boolean(st().bridge.token)}>
              <Button
                text="复制令牌"
                size="small"
                variant="ghost"
                onPress={() => {
                  copyText(st().bridge.token || "");
                  showToast("已复制扩展桥令牌", "success");
                }}
              />
            </If>
            <Button text="刷新令牌" size="small" variant="ghost" onPress={() => regenerateExtensionToken()} />
            <Button
              text="刷新状态"
              size="small"
              icon="refresh-cw"
              variant="outline"
              onPress={() => refreshBrowsers()}
            />
          </View>
        }
        bottom={
          <If when={Boolean(st().busy || st().missingHint)}>
            <View class="gap-1.5">
              <If when={Boolean(st().busy)}>
                <View class="gap-1 rounded-lg bg-slate-50 px-3 py-1.5 dark:bg-[#1A2232]">
                  <Text class="text-xs text-slate-700 dark:text-slate-200">
                    {`${st().status || "正在准备浏览器环境"} · ${Math.round(st().percent)}%`}
                  </Text>
                  <Progress value={Math.min(1, st().percent / 100)} />
                </View>
              </If>
              <If when={Boolean(st().missingHint)}>
                <Text class="text-xs text-amber-600 dark:text-amber-400">{st().missingHint}</Text>
              </If>
            </View>
          </If>
        }
      />

      {/* 添加条 + 全高平铺账号列表 */}
      <View class="min-h-0 flex-1 bg-white dark:bg-[#131924]">
        <View class="shrink-0 flex-row flex-wrap items-center gap-2 border-b border-[#E5E9F0] px-3 py-2 dark:border-[#222B3C]">
          <View class="w-[136px]">
            <Select
              options={PLATFORM_OPTIONS}
              value={newPlatform()}
              style={{ width: "100%", alignSelf: "stretch" }}
              onChange={({ value }) => newPlatform.set(value)}
            />
          </View>
          <Input
            value={newName()}
            placeholder="输入账号备注名（如：主力号）快速创建独立资料..."
            onChange={({ value }) => newName.set(value)}
            class="min-w-[180px] flex-1"
          />
          <Button
            text="新建账号"
            size="small"
            icon="plus"
            variant="primary"
            onPress={() => {
              createBrowser(newPlatform(), newName());
              newName.set("");
            }}
          />
          <Button
            text="Antigravity 导入 / 授权"
            size="small"
            icon="key"
            variant="outline"
            onPress={() => void agyAction("openChooser", "")}
          />
        </View>

        <PaneHeader
          title={`账号列表 (${rows().length})`}
          subtitle={st().batchActive ? st().batchText : st().status || "点击账号名可就地重命名"}
          right={
            <View class="flex-row flex-wrap items-center gap-2">
              <Tabs
                variant="segmented"
                size="small"
                options={[{ value: "全部", label: "全部" }, ...PLATFORM_OPTIONS]}
                value={filterPlat()}
                onChange={({ value }) => filterPlat.set(value)}
              />
              <Button
                text={st().checkedCount > 0 ? `已选 ${st().checkedCount}` : "全选"}
                size="small"
                variant="outline"
                onPress={() => toggleAllBrowserChecks(st().checkedCount === 0)}
              />
              <If when={st().checkedCount > 0}>
                <Button
                  text={st().batchActive ? "停止批量" : "批量打开"}
                  size="small"
                  variant="primary"
                  onPress={() => runBrowserBatch(Boolean(st().batchActive))}
                />
              </If>
            </View>
          }
        />

        <AccountListFrame class="min-h-0 flex-1 overflow-hidden">
          <If when={rows().length > 0}>
            <View class="flex-row items-center justify-between border-b border-[#E5E9F0] bg-[#F1F5F9]/80 px-3 py-1.5 dark:border-[#222B3C] dark:bg-[#1A2232]">
              <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
                账号 / 状态 / 今日用量
              </Text>
              <Text class="text-[11px] font-medium text-slate-500 dark:text-slate-400">
                操作
              </Text>
            </View>
          </If>
          <List
            testID="browser-account-list"
            class="min-h-0 flex-1"
            contentClass="pr-1"
            each={rows()}
            keyExtractor={(r) => String(r.id)}
            estimatedItemSize={48}
            empty={<EmptyNotice icon="globe" title="暂无账号" subtitle="选择上方平台并输入备注名即可创建独立账号资料" />}
          >
            {(row) => <AccountRow row={row()} names={names} proxies={proxies} />}
          </List>
        </AccountListFrame>
      </View>

      {/* Antigravity 导入/登录方式选择器 */}
      <Dialog
        open={Boolean(st().agyChooserOn)}
        title="添加 Antigravity 账号"
        description={st().agyChooser.hint || "可直接读取本机登录资料，或通过项目托管的 Camoufox 完成网页 OAuth 登录"}
        style={{ width: 540 }}
        onClose={() => void agyAction("closeChooser", "")}
        closeOnBackdropPress
        closeOnEscape
      >
        <View class="w-full min-w-0 gap-3">
          <If when={Boolean(st().agyChooser.status)}>
            <Text class="text-xs text-blue-600 dark:text-blue-400">{st().agyChooser.status}</Text>
          </If>
          <If when={Boolean(st().agyChooser.err)}>
            <Text class="text-xs text-rose-600 dark:text-rose-400">{st().agyChooser.err}</Text>
          </If>
          <If when={Boolean(st().agyChooser.home)}>
            <View class="rounded-lg border border-slate-200 bg-slate-50 p-2.5 dark:border-[#222B3C] dark:bg-[#1A2232]">
              <Text class="text-xs font-medium text-slate-900 dark:text-slate-100">
                {st().agyChooser.email || "检测到本机目录"}
              </Text>
              <Text class="truncate text-[11px] text-slate-500 dark:text-slate-400">
                {st().agyChooser.home}
              </Text>
            </View>
          </If>
          <View class="flex-row flex-wrap items-center justify-end gap-2">
            <Button
              text="选择本机数据目录"
              size="small"
              variant="outline"
              onPress={() => void agyAction("pickFolder", "")}
            />
            <If when={Boolean(st().agyChooser.canImport)}>
              <Button
                text="导入检测到的数据"
                size="small"
                variant="outline"
                busy={Boolean(st().agyChooser.busy)}
                onPress={() => void agyAction("startImport", "")}
              />
            </If>
            <Button
              text="通过 Camoufox 网页登录"
              size="small"
              variant="primary"
              onPress={() => void agyAction("startOAuth", "")}
            />
          </View>
        </View>
      </Dialog>

      {/* Antigravity OAuth 回调框 */}
      <Dialog
        open={Boolean(st().agyLoginOn)}
        title={`Antigravity 登录 · ${st().agyLogin.name || ""}`}
        description="请在托管的 Camoufox 窗口完成 Google 登录并复制授权链接或回调码粘贴到下方"
        style={{ width: 560 }}
        onClose={() => void agyAction("cancelLogin", "")}
        closeOnEscape
      >
        <View class="w-full min-w-0 gap-3">
          <View class="flex-row items-center gap-2">
            <Button
              text="打开授权网页"
              size="small"
              icon="external-link"
              variant="outline"
              busy={Boolean(st().agyLogin.opening)}
              onPress={() => void agyAction("openAuthURL", "")}
            />
            <Button
              text={st().agyLogin.copied ? "已复制链接" : "复制授权链接"}
              size="small"
              icon="copy"
              variant="outline"
              onPress={() => {
                void agyAction("copyAuthURL", "");
                if (st().agyLogin.url) copyText(st().agyLogin.url!);
                showToast("已复制授权链接", "success");
              }}
            />
            <If when={Boolean(st().agyLogin.retry)}>
              <Button
                text="重新获取链接"
                size="small"
                variant="ghost"
                onPress={() => void agyAction("retryLogin", "")}
              />
            </If>
          </View>
          <Input
            value={authCode()}
            placeholder="粘贴浏览器跳转后的回调 URL 或授权码..."
            onChange={({ value }) => authCode.set(value)}
          />
          <If when={Boolean(st().agyLogin.err)}>
            <Text class="text-xs text-rose-600 dark:text-rose-400">{st().agyLogin.err}</Text>
          </If>
          <View class="flex-row justify-end gap-2">
            <Button text="取消" size="small" variant="ghost" onPress={() => void agyAction("cancelLogin", "")} />
            <Button
              text="提交授权"
              size="small"
              variant="primary"
              busy={Boolean(st().agyLogin.busy)}
              disabled={authCode().trim() === ""}
              onPress={() => {
                void agyAction("submitCode", authCode());
                authCode.set("");
              }}
            />
          </View>
        </View>
      </Dialog>
    </View>
  );
}
